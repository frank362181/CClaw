/**
* Chat 业务服务
*
* 职责:接收 cclawchat 聊天界面发来的消息,调用 LLM 引擎返回真实回复。
*
* 处理流程:
*   1. 校验消息内容并做长度限制,解析/生成会话标识
*   2. 发送者以服务端认证信息为准(不信任客户端传入的 senderId,防伪造)
*   3. 解析目标 Agent(方案C 混合路由:显式→会话绑定→关键词→LLM分类→默认)
*   4. 解析 LLM Provider(Agent.PrimaryModel 优先,失败回退 Fallbacks)
*   5. 基于 Agent 工作区启动文档组装 system prompt
*   6. 调用 LLM chat/completions 取回真实回复
*
* 与 LoginService/AgentService 保持一致:组合 baseService.BaseService + 工厂 GetChatService()。
**/

package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/controller/model"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	"my.favorite/cclawservice/service/runtime"
	myUtility "my.favorite/cclawservice/utility"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/database/core"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// MaxChatMessageLen 单条聊天消息的字符上限(防超大请求体滥用)
const MaxChatMessageLen = 4096

// ToolCallingHeartbeatInterval 工具执行静默心跳间隔:脚本超过该时长无新输出时,对于长执行的tool
// 向用户推送心跳 progress 行,用于区分"仍在运行"与"挂起"(对长任务工具体验友好)。
const ToolCallingHeartbeatInterval = 3 * time.Second

// ChatService 处理聊天消息:校验 → 解析 Agent/Provider → 组装 prompt → 调用 LLM。
type ChatService struct {
	baseService.BaseService

	agentRepo     repo.IAgentRepository
	promptService *PromptService
	llmClient     *runtime.Client
	agentRouter   *runtime.AgentRouter

	functionCalling bool // 是否启用 function-calling(由 ini [sandBox] functionCalling 控制)
}

var chatService = pCommon.GetSingleton(func() *ChatService {
	return &ChatService{
		BaseService: baseService.BaseService{
			Name:        "chatService",
			Description: "receive chat messages and reply via the LLM engine",
		},
		agentRepo:     nil,
		promptService: GetPromptService(),
		llmClient:     runtime.NewClient(),
		agentRouter:   nil,
	}
}).Instance()

// GetChatService 构建聊天服务。
//   - db:             用于读取 Agent / LLM Provider / Skill 配置(与 AgentService 共用同一仓库单例)
//   - functionCalling:是否启用 function-calling(工具调用),来源于 [sandBox] functionCalling 配置
func GetChatService(db core.IBaseDatabase, functionCalling bool) baseService.IService {
	chatService.agentRepo = repo.GetAgentRepository(db)
	chatService.agentRouter = runtime.GetAgentRouter(chatService.agentRepo)
	chatService.functionCalling = functionCalling

	return chatService
}

// ToolEvent 流式路径下工具调用的事件,由 streamToolLoop 实时推送给控制器/前端:
//   - Phase: intent(模型声明调用)/running(开始执行)/done(成功)/error(失败)
//   - Seq:   本回合工具调用序号(自增)
//   - Detail: done=结果摘要; error=错误信息; intent/running 为空
//
// 底层仍为多次上游请求(OpenAI 兼容协议不支持服务端工具循环),此处把多轮编排为一条连续事件流。
type ToolEvent struct {
	Phase  string
	Seq    int
	Name   string
	Detail string
}

// SendMessage 接收并应答一条聊天消息(同步模式:等待完整回复后一次性返回)。
//   - sender: 服务端认证得到的登录名(由认证 interceptor 写入请求上下文),覆盖请求体 senderId 防伪造
//   - comCode: 服务端认证得到的单位编码,用于 Agent/Provider 的归属隔离(owner_id)
func (s *ChatService) SendMessage(ctx context.Context, req *model.ChatRequest, sender, comCode string) (*model.ChatResponse, error) {
	req, _, systemPrompt, llmProvider, toolSet, err := s.prepareChat(ctx, req, sender, comCode)
	if err != nil {
		return nil, err
	}

	// 调用 LLM 引擎取回完整回复(function-calling 开启且该 Agent 有工具时可能经过工具循环)
	reply, err := s.completeChat(ctx, llmProvider, systemPrompt, req, toolSet)
	if err != nil {
		return nil, myException.LlmError(err, "chat service: runtime call error")
	}

	// 组装应答
	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	resp := &model.ChatResponse{
		SenderId:    req.SenderId,
		SessionId:   req.SessionId,
		ChatType:    req.ChatType,
		ChatChannel: req.ChatChannel,
		ChatId:      now,
		Reply:       reply,
	}
	return resp, nil
}

// ChatStream 以流式(SSE)方式应答聊天消息。
//   - onChunk:  每个增量文本到达时被调用(参数为该次增量 delta,由控制器转发给浏览器)
//   - onStatus: 内容开始前报告 Agent 所处阶段(如解析任务/思考),由控制器以 status 事件转发
//   - onTool:   工具调用进度事件(intent/running/done/error),由控制器以 tool 事件转发
//   - 返回值:   完整回复文本
//   - 与 SendMessage 共用 prepareChat,保证同步/流式两条路径的校验与解析完全一致
func (s *ChatService) ChatStream(ctx context.Context, req *model.ChatRequest, sender, comCode string, onChunk func(string), onStatus func(status, text string), onReasoning func(delta string), onTool func(evt ToolEvent)) (string, error) {
	if onStatus != nil {
		onStatus(cCommon.ChatStatusPreparing, "正在解析任务…")
	}
	req, _, systemPrompt, llmProvider, toolSet, err := s.prepareChat(ctx, req, sender, comCode)
	if err != nil {
		return "", err
	}

	// 注册为"进行中的流式聊天",供中断 API 按会话键取消(不同用户/会话相互隔离)。
	// 派生可取消上下文:客户端断开或用户点击中断都会终止 LLM 流式读取。
	streamCtx, cancel := context.WithCancel(ctx)
	key := myUtility.BuildChatStreamKey(sender, req.SessionId)
	runtime.GetChatStreamRegistry().Register(key, cancel)
	defer runtime.GetChatStreamRegistry().Unregister(key)

	if onStatus != nil {
		onStatus(cCommon.ChatStatusThinking, "正在思考…")
	}

	// function-calling 开启且该 Agent 有可用工具时,走工具调用循环(工具阶段状态提示,最终内容流式转发);
	// 否则保持原有的纯流式转发。
	var reply string
	if s.functionCalling && toolSet != nil && len(toolSet.LLMTools()) > 0 {
		reply, err = s.streamToolLoop(streamCtx, llmProvider, runtime.BuildChatMessage(systemPrompt, req), toolSet, onChunk, onStatus, onReasoning, onTool)
	} else {
		reply, err = s.llmClient.ChatCompletionStream(streamCtx, llmProvider, systemPrompt, req, onChunk, onReasoning)
	}
	if err != nil {
		return "", myException.LlmError(err, "chat service: runtime stream error")
	}
	return reply, nil
}

// InterruptChat 中断指定会话进行中的流式聊天(用户点击中断按钮)。
// 仅在聊天已开始(流式进行中)时支持;会话不存在或已结束时返回"未在聊天"错误。
// key 以发送者(服务端认证)为前缀,保证不同用户、不同会话的聊天相互隔离、互不影响。
func (s *ChatService) InterruptChat(sender, sessionId string) error {
	key := myUtility.BuildChatStreamKey(sender, sessionId)
	if !runtime.GetChatStreamRegistry().Cancel(key) {
		return myException.ChatNotActiveErrEx2("chat stream not active: %s", sessionId)
	}
	logging.LogDebugF("chat service: interrupt chat key:%s", key)
	return nil
}

// =============================== private functions & methods========================
// prepareChat 执行同步/流式两条聊天路径的公共前置步骤:
// 校验/规整入参 → 覆盖 sender → 生成 sessionId → 缺省类型 → 解析 Agent/Provider → 组装 system prompt。
// 返回规范化后的请求、目标 Agent、系统提示词与解析出的 Provider,供 SendMessage/ChatStream 复用。
func (s *ChatService) prepareChat(ctx context.Context, req *model.ChatRequest, sender, comCode string) (*model.ChatRequest, dataobject.TAgent, *dataobject.SystemPrompt, dataobject.TLlm, *runtime.ToolSet, error) {
	if req == nil {
		return nil, dataobject.TAgent{}, nil, dataobject.TLlm{}, nil, pException.ParameterEmptyErrorEx("chat request is nil")
	}

	// 1. 校验消息内容
	req.ReqMsg = strings.TrimSpace(req.ReqMsg)
	if req.ReqMsg == "" {
		return nil, dataobject.TAgent{}, nil, dataobject.TLlm{}, nil, pException.ParameterEmptyErrorEx("chat message is empty")
	}
	if len([]rune(req.ReqMsg)) > MaxChatMessageLen {
		return nil, dataobject.TAgent{}, nil, dataobject.TLlm{}, nil, pException.ParameterEmptyErrorEx("chat message is too long")
	}

	// 2. 发送者以服务端认证信息为准
	if strings.TrimSpace(sender) != "" {
		req.SenderId = strings.TrimSpace(sender)
	}

	// 3. 会话标识:客户端可传入;为空则由服务端生成
	if strings.TrimSpace(req.SessionId) == "" {
		req.SessionId = pCommon.GenerateUUID()
	}

	// 4. 缺省类型
	if req.ChatType == 0 {
		req.ChatType = cCommon.ChatTypePrivate
	}
	if req.ChatChannel == 0 {
		req.ChatChannel = cCommon.ChatChannelWeb
	}

	// 5. 解析目标 Agent(混合路由:显式 → 会话绑定 → 关键词 → LLM 分类 → 默认)
	agent, err := s.resolveAgent(ctx, comCode, req.SessionId, req.ReqMsg, req.AgentId)
	if err != nil {
		return nil, dataobject.TAgent{}, nil, dataobject.TLlm{}, nil, err
	}

	// 6. 解析 LLM Provider(PrimaryModel 优先,Fallbacks 兜底)
	llmProvider, err := s.resolveProvider(comCode, agent)
	if err != nil {
		return nil, dataobject.TAgent{}, nil, dataobject.TLlm{}, nil, err
	}

	// 7. 基于 Agent 工作区启动文档组装 system prompt,并解析其 skill 中声明的可用工具
	skillFiles, agentExtensionRoot := s.collectSkillFiles(comCode, agent)
	systemPrompt, err := s.promptService.BuildSystemPromptEx(agent, skillFiles, dataobject.BootstrapScopeNormal)
	if err != nil {
		logging.LogErrorF("chat service: build system prompt error:%s", err.Error())
		return nil, dataobject.TAgent{}, nil, dataobject.TLlm{}, nil, myException.ChatLLMErrEx2("chat service: build system prompt error:%s", err.Error())
	}

	return req, agent, systemPrompt, llmProvider, runtime.LoadToolSet(agentExtensionRoot, skillFiles), nil
}

// resolveAgent 解析本次会话的目标 Agent(混合路由)。
//   - 显式 agentId 优先:非空时精确查找,未命中/被禁用返回错误;
//   - 未显式指定时由 AgentRouter 按 会话绑定 → 关键词规则 → LLM 分类 → 默认 Agent 逐级回落;
//   - 会话绑定保证同一会话内上下文一致,并避免重复路由计算。
func (s *ChatService) resolveAgent(ctx context.Context, comCode, sessionId, reqMsg, agentId string) (dataobject.TAgent, error) {
	if s.agentRouter == nil {
		return dataobject.TAgent{}, myException.ChatLLMErrEx("chat service: agent router not initialized")
	}
	return s.agentRouter.Resolve(ctx, comCode, sessionId, reqMsg, agentId)
}

// resolveProvider 解析 Agent 使用的 LLM Provider:优先 PrimaryModel,再按 Fallbacks 逐个回退。
// 均不可用时返回 ChatAgentNotFoundErr(视为"目标 Agent 不可用")。
func (s *ChatService) resolveProvider(comCode string, agent dataobject.TAgent) (dataobject.TLlm, error) {
	if s.agentRepo == nil {
		return dataobject.TLlm{}, myException.ChatLLMErrEx("chat service: agent repository not initialized")
	}

	modelIds := make([]string, 0, 4)
	if id := strings.TrimSpace(agent.PrimaryModel); id != "" {
		modelIds = append(modelIds, id)
	}
	for _, fb := range strings.Split(agent.Fallbacks, ",") {
		if id := strings.TrimSpace(fb); id != "" {
			modelIds = append(modelIds, id)
		}
	}

	providers := s.agentRepo.GetLLMProviders(comCode)
	for _, modelId := range modelIds {
		for _, p := range providers {
			if p.ProviderId == modelId && !p.Disabled && !p.Deleted {
				return p, nil
			}
		}
	}

	return dataobject.TLlm{}, pException.UnMatchErrorEx2("no available runtime provider for agent: %s", agent.AgentId)
}

// collectSkillFiles 收集该 comCode 下可用的 skill 文件路径,并返回 Agent 的 extension 根目录。
// 相对路径显式拼接 Agent 工作区根(与 PromptRepository.resolveSkillPathLocked 语义一致);
// 文件缺失由仓库告警后跳过,不影响主流程。
// 返回值:skill 文件路径列表 + extension 根目录(供 ToolSet 解析脚本工具时做安全校验)。
func (s *ChatService) collectSkillFiles(comCode string, agent dataobject.TAgent) ([]string, string) {
	if s.agentRepo == nil {
		return nil, ""
	}

	agentExtensionRoot, _ := myUtility.BuildAgentExtensionRoot(agent)

	skills := s.agentRepo.GetSkills(comCode)
	if len(skills) == 0 {
		return nil, agentExtensionRoot
	}

	files := make([]string, 0, len(skills))
	for i := range skills {
		dir := strings.TrimSpace(skills[i].SkillDir)
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) && agentExtensionRoot != "" {
			dir = filepath.Join(agentExtensionRoot, dir)
		}
		files = append(files, dir)
	}

	return files, agentExtensionRoot
}

// completeChat 取回完整回复:function-calling 开启且该 Agent 有可用工具时走工具循环,否则走普通同步调用。
func (s *ChatService) completeChat(ctx context.Context, llmProvider dataobject.TLlm, systemPrompt *dataobject.SystemPrompt, req *model.ChatRequest, toolSet *runtime.ToolSet) (string, error) {
	if s.functionCalling && toolSet != nil && len(toolSet.LLMTools()) > 0 {
		return s.runToolLoop(ctx, llmProvider, runtime.BuildChatMessage(systemPrompt, req), toolSet)
	}
	return s.llmClient.ChatCompletion(ctx, llmProvider, systemPrompt, req)
}

// runToolLoop 同步路径的工具调用循环:调用LLM → 执行工具 → 结果回填 → 再次调用,直到模型给出最终文本。
func (s *ChatService) runToolLoop(ctx context.Context, llmProvider dataobject.TLlm, messages []runtime.ChatMessage, toolSet *runtime.ToolSet) (string, error) {
	for i := 0; i < cCommon.AgentToolLoopMax; i++ {
		content, calls, err := s.llmClient.CompleteWithTools(ctx, llmProvider, messages, toolSet.LLMTools())
		if err != nil {
			return "", myException.LlmError(err, "chat service: runtime tool call error")
		}
		if len(calls) == 0 {
			return content, nil
		}
		messages = append(messages, runtime.BuildToolCallWithContentMessage(content, calls)) // 缺口1:工具轮文本一并回填
		for _, call := range calls {
			result, err := toolSet.Exec(ctx, call.Function.Name, call.Function.Arguments)
			if err != nil {
				result = fmt.Sprintf(`{"error":%q}`, err.Error())
			}
			messages = append(messages, runtime.BuildToolResultMessage(call.ID, result))
		}
	}
	return "", myException.ChatLLMErrEx2("chat service: tool loop exceeded max iterations(%d)", cCommon.AgentToolLoopMax)
}

// streamToolLoop 流式路径的工具调用循环:
// 工具调用阶段实时推送给用户:模型声明调用(intent)→开始执行(running)→结果(done/error),
// 最终内容增量通过 onChunk 流式转发。底层仍为多次上游请求(OpenAI 兼容协议不支持服务端工具循环),
// 此处把多轮编排为一条面向客户端的事件流。
func (s *ChatService) streamToolLoop(ctx context.Context, llmProvider dataobject.TLlm, messages []runtime.ChatMessage, toolSet *runtime.ToolSet, onChunk func(string), onStatus func(status, text string), onReasoning func(delta string), onTool func(evt ToolEvent)) (string, error) {
	seq := 0 // 本回合工具调用序号(跨轮自增,前端按 seq 追踪同一工具调用)
	for i := 0; i < cCommon.AgentToolLoopMax; i++ {
		if i > 0 && onStatus != nil {
			onStatus(cCommon.ChatStatusThinking, "正在思考…")
		}
		var roundIntents []int // 本轮模型声明的工具调用序号(与返回的 calls 一一对应)
		content, calls, err := s.llmClient.StreamWithTools(ctx, llmProvider, messages, toolSet.LLMTools(), onChunk, onReasoning,
			func(name string) { // onToolIntent:模型流式声明将调用某工具,立即告知前端
				if onTool != nil {
					seq++
					roundIntents = append(roundIntents, seq)
					onTool(ToolEvent{Phase: cCommon.ChatToolEventIntent, Seq: seq, Name: name})
				}
			})
		if err != nil {
			return "", myException.LlmError(err, "chat service: runtime tool stream error")
		}
		if len(calls) == 0 {
			// 本轮无工具调用:流式内容(早已通过 onChunk 转发)即为最终结果
			return content, nil
		}
		messages = append(messages, runtime.BuildToolCallWithContentMessage(content, calls)) // 工具调用+文本一并回填

		// 同轮多个工具调用并行执行(goroutine 各自跑脚本);
		// 脚本 stdout 经 lineCh 逐行汇集,由唯一转发 goroutine 串行推送 progress 事件(保证 SSE 无并发写)。
		type toolRunItem struct {
			seq    int
			name   string
			callID string
			arg    string
		}
		runs := make([]toolRunItem, len(calls))
		for k, call := range calls {
			callSeq := seq
			if k < len(roundIntents) {
				callSeq = roundIntents[k]
			}
			runs[k] = toolRunItem{seq: callSeq, name: call.Function.Name, callID: call.ID, arg: call.Function.Arguments}
			if onTool != nil {
				onTool(ToolEvent{Phase: cCommon.ChatToolEventRunning, Seq: callSeq, Name: call.Function.Name})
			}
		}

		type toolResultItem struct {
			item   toolRunItem
			result string
			err    error
		}
		results := make([]toolResultItem, len(runs))

		lineCh := make(chan ToolEvent, 16)
		var wg sync.WaitGroup
		for k, run := range runs {
			wg.Add(1)
			go func(k int, run toolRunItem) {
				defer wg.Done()
				// 静默心跳:脚本超过 toolHeartbeatInterval 无新输出时推送心跳 progress 行,
				// 让 UI 区分"仍在运行"与"挂起";close(stopHeart) 后 <-heartDone 保证心跳协程先退出再收尾。
				startTime := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime() //time.Now()
				stopHeart := make(chan struct{})
				heartDone := make(chan struct{})
				var lastOut atomic.Int64
				lastOut.Store(startTime.NanoTimestamp)
				go func() {
					defer close(heartDone)
					ticker := time.NewTicker(ToolCallingHeartbeatInterval)
					defer ticker.Stop()
					for {
						select {
						case <-stopHeart:
							return
						case now := <-ticker.C: //此时返回的当前的时间（超时）
							if time.Duration(now.UnixNano()-lastOut.Load()) < ToolCallingHeartbeatInterval {
								continue // 期间有新输出(未静默),不打扰用户
							}
							lastOut.Store(now.UnixNano())
							lineCh <- ToolEvent{Phase: cCommon.ChatToolEventProgress, Seq: run.seq, Name: run.name,
								Detail: fmt.Sprintf("… 工具运行中(%d 秒,无新输出) …", int(now.Unix()-startTime.Timestamp))}
						}
					}
				}()

				res, err := toolSet.ExecStream(ctx, run.name, run.arg, func(line string) {
					lastOut.Store(pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().NanoTimestamp)
					// 脚本 stdout 行:仅投递到通道,由唯一转发 goroutine 推送(不在此处直接写 SSE)
					lineCh <- ToolEvent{Phase: cCommon.ChatToolEventProgress, Seq: run.seq, Name: run.name, Detail: line}
				})
				close(stopHeart)
				<-heartDone
				results[k] = toolResultItem{item: run, result: res, err: err}
			}(k, run)
		}

		// 唯一转发 goroutine:串行消费工具输出行,避免多 goroutine 并发写 SSE
		fwdDone := make(chan struct{})
		go func() {
			defer close(fwdDone)
			for evt := range lineCh {
				if onTool != nil {
					onTool(evt)
				}
			}
		}()
		wg.Wait()
		close(lineCh)
		<-fwdDone

		if err := ctx.Err(); err != nil { // 中断加速:工具执行中用户取消,不再继续后续轮次
			return "", myException.ErrRequestCanceled
		}

		// 全部执行完成:按原始顺序回填 tool 消息并派发 done/error 事件
		for _, r := range results {
			if r.err != nil {
				if onTool != nil {
					onTool(ToolEvent{Phase: cCommon.ChatToolEventError, Seq: r.item.seq, Name: r.item.name, Detail: r.err.Error()})
				}
				messages = append(messages, runtime.BuildToolResultMessage(r.item.callID, fmt.Sprintf(`{"error":%q}`, r.err.Error())))
				continue
			}
			if onTool != nil {
				onTool(ToolEvent{Phase: cCommon.ChatToolEventDone, Seq: r.item.seq, Name: r.item.name, Detail: pCommon.TruncateRunes(r.result, 600)})
			}
			messages = append(messages, runtime.BuildToolResultMessage(r.item.callID, r.result))
		}
	}
	return "", myException.ChatLLMErrEx2("chat service: tool stream loop exceeded max iterations(%d)", cCommon.AgentToolLoopMax)
}
