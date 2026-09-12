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
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cCommon "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/harness"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	"my.favorite/cclawservice/request"
	core2 "my.favorite/cclawservice/service/core"
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

// AgentRunTimeoutSecs 单次聊天任务(工具循环 + LLM 生成整段)的应用级最长时长(秒)。
// 它与传输层 writeTimeout 解耦:任务能跑多久由这里(而非 http.Server 的写超时)界定,
// 超时返回可读的"任务执行超时",不会再把长任务误报成"request canceled by client"。
// 可用环境变量 CCLAW_CHAT_RUN_TIMEOUT_SEC(秒)覆盖。
const AgentRunTimeoutSecs = 600

// chatRunTimeout 返回应用级运行超时:优先取环境变量 CCLAW_CHAT_RUN_TIMEOUT_SEC,
// 否则用 AgentRunTimeoutSecs 默认值。
func chatRunTimeout() time.Duration {
	if v := strings.TrimSpace(pCommon.GetEnvString("CCLAW_CHAT_RUN_TIMEOUT_SEC")); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return AgentRunTimeoutSecs * time.Second
}

// ChatService 处理聊天消息:校验 → 解析 Agent/Provider → 组装 prompt → 调用 LLM。
type ChatService struct {
	baseService.BaseService

	agentRepo     repo.IAgentRepository
	promptService *harness.PromptService
	llmClient     *harness.Client
	agentRouter   *harness.AgentRouter

	// sessionService 会话管理(阶段1双写:只写不读的旁路)。
	// 为 nil 或未启用时全部记录动作静默跳过,不影响聊天主流程。
	sessionService *ChatSessionService

	functionCalling bool // 是否启用 function-calling(由 ini [sandBox] functionCalling 控制)
}

var chatService = pCommon.GetSingleton(func() *ChatService {
	return &ChatService{
		BaseService: baseService.BaseService{
			Name:        "chatService",
			Description: "receive chat messages and reply via the LLM engine",
		},
		agentRepo:     nil,
		promptService: harness.GetPromptService(),
		llmClient:     harness.NewClient(),
		agentRouter:   nil,
	}
}).Instance()

// GetChatService 构建聊天服务。
//   - db:             用于读取 Agent / LLM Provider / Skill 配置(与 AgentService 共用同一仓库单例)
//   - functionCalling:是否启用 function-calling(工具调用),来源于 [sandBox] functionCalling 配置
//   - sessionRecord:  是否启用会话记录(阶段1双写),来源于 [chat] sessionRecord 配置
func GetChatService(db core.IBaseDatabase, functionCalling bool, sessionRecord bool) baseService.IService {
	chatService.agentRepo = repo.GetAgentRepository(db)
	chatService.agentRouter = harness.GetAgentRouter(chatService.agentRepo)
	chatService.functionCalling = functionCalling
	chatService.sessionService = GetChatSessionService(db, sessionRecord)

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
func (s *ChatService) SendMessage(ctx context.Context, req *request.ChatRequest, sender, comCode string) (*request.ChatResponse, error) {
	req, agent, systemPrompt, llmProvider, toolSet, err := s.prepareChat(ctx, req, sender, comCode)
	if err != nil {
		return nil, err
	}

	// 每 Agent 并发闸:同一 agent 同时生成数不超过 max_concurrent,覆盖整段生成(含工具循环)
	release, err := s.acquireAgentGate(ctx, agent)
	if err != nil {
		return nil, err
	}
	defer release()

	// 会话建档(阶段1双写:旁路,只写不读;失败不影响聊天)
	session, recorded := s.sessionService.Begin(sender, agent, req)

	// 调用 LLM 引擎取回完整回复(function-calling 开启且该 Agent 有工具时可能经过工具循环)
	recorder := newTurnRecorder()
	reply, err := s.completeChat(ctx, llmProvider, systemPrompt, req, toolSet, recorder)
	if err != nil {
		return nil, myException.LlmError(err, "chat service: runtime call chat")
	}

	// 轮次落库:问题/工具/回复写入历史文件并登记 t_turn / t_turn_tool
	if recorded {
		s.sessionService.AppendTurn(session, req, reply, recorder.Snapshot())
	}

	// 组装应答
	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	resp := &request.ChatResponse{
		SenderId:    req.SenderId,
		SessionId:   req.SessionId,
		ChatType:    req.ChatType,
		ChatChannel: req.ChatChannel,
		TurnId:      req.TurnId, // 回显客户端生成的 turnId,保证客户端能定位本轮
		Reply:       reply,
		RespTime:    now,
	}
	return resp, nil
}

// ChatStream 以流式(SSE)方式应答聊天消息。
//   - onChunk:  每个增量文本到达时被调用(参数为该次增量 delta,由控制器转发给浏览器)
//   - onStatus: 内容开始前报告 Agent 所处阶段(如解析任务/思考),由控制器以 status 事件转发
//   - onTool:   工具调用进度事件(intent/running/done/error),由控制器以 tool 事件转发
//   - 返回值:   完整回复文本
//   - 与 SendMessage 共用 prepareChat,保证同步/流式两条路径的校验与解析完全一致
func (s *ChatService) ChatStream(ctx context.Context, req *request.ChatRequest, sender, comCode string, onChunk func(string), onStatus func(status, text string), onReasoning func(delta string), onTool func(evt ToolEvent), onNotice func(code, msg string)) (string, error) {
	if onStatus != nil {
		onStatus(cCommon.ChatStatusPreparing, "正在解析任务…")
	}
	req, agent, systemPrompt, llmProvider, toolSet, err := s.prepareChat(ctx, req, sender, comCode)
	if err != nil {
		return "", err
	}

	// 每 Agent 并发闸:同一 agent 同时生成数不超过 max_concurrent,覆盖整段流式生成(含工具循环)
	release, err := s.acquireAgentGate(ctx, agent)
	if err != nil {
		return "", err
	}
	defer release()

	// 注册为"进行中的轮次",供中断 API 按轮次取消(不同用户/轮次相互隔离)。
	// 派生可取消上下文:客户端断开或用户点击中断都会终止 LLM 流式读取。
	// 注册键必须是 <sender>:<turnId>:中断 API 只收到 turnId,用同一规则构造才能命中;
	// 以发送者为前缀则避免跨用户冒用 turnId 中断他人流。
	streamCtx, cancel := context.WithCancel(ctx)
	key := cCommon.BuildTurnStreamKey(sender, req.TurnId)
	ctxmgr.GetTurnStreamMgr().Register(key, cancel)
	defer ctxmgr.GetTurnStreamMgr().Unregister(key)

	// 应用级运行超时:界定"这一轮任务最多跑多久"(工具循环 + LLM 生成整段)。
	// 与传输层 writeTimeout 解耦——任务时长由 chatRunTimeout()(可 env 覆盖)决定,
	// 超时返回可读的"任务超时"错误,而非依赖 http.Server 写超时去掐断 SSE。
	runCtx, runCancel := context.WithTimeout(streamCtx, chatRunTimeout())
	defer runCancel()

	if onStatus != nil {
		onStatus(cCommon.ChatStatusThinking, "正在思考…")
	}

	// 会话建档(阶段1双写:旁路,只写不读;失败不影响聊天)
	session, recorded := s.sessionService.Begin(sender, agent, req)

	// function-calling 开启且该 Agent 有可用工具时,走工具调用循环(工具阶段状态提示,最终内容流式转发);
	// 否则保持原有的纯流式转发。
	recorder := newTurnRecorder()
	var reply, finishReason string
	if s.functionCalling && toolSet != nil && len(toolSet.LLMTools()) > 0 {
		reply, finishReason, err = s.streamToolLoop(runCtx, llmProvider, harness.BuildChatMessage(systemPrompt, req), toolSet, onChunk, onStatus, onReasoning, onTool, recorder)
	} else {
		reply, finishReason, err = s.llmClient.ChatCompletionStream(runCtx, llmProvider, systemPrompt, req, onChunk, onReasoning)
	}
	if err != nil {
		// 应用级运行超时:给可读错误,而不是笼统的 "request canceled by client"
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return pCommon.BlankString, myException.ChatRunTimeoutKind.Newf("chat service: run timeout after %s", chatRunTimeout())
		}
		return pCommon.BlankString, myException.LlmError(err, "chat service: runtime stream error")
	}

	// finish_reason 收尾:长度截断/内容合规 → 以独立 notice 事件告知前端(不污染回答正文)。
	// 无任何正式内容且被内容安全策略拦截时,按内容合规业务错误返回(交由控制器提示)。
	if code, msg := finishReasonNotice(finishReason); code != "" {
		if strings.TrimSpace(reply) == pCommon.BlankString && finishReason == llmproto.FinishReasonContentFilter {
			return pCommon.BlankString, myException.ChatLLMContentFilterKind.Newf("chat service: output blocked by content filter")
		}
		if onNotice != nil {
			onNotice(code, msg)
		}
	}

	// 轮次落库(阶段1双写:旁路,只写不读;失败不影响回复)
	if recorded {
		s.sessionService.AppendTurn(session, req, reply, recorder.Snapshot())
	}
	return reply, nil
}

// finishReasonNotice 把上游 finish_reason 映射为独立的收尾提示(code + 文案;无需提示返回空串)。
//   - length:         回答达到输出长度上限,可能被截断
//   - content_filter: 输出被内容安全策略拦截,可能不完整
func finishReasonNotice(finishReason string) (code, msg string) {
	switch strings.TrimSpace(finishReason) {
	case llmproto.FinishReasonLength:
		return cCommon.ChatNoticeTruncated, cCommon.ConstChatNoticeTruncatedMsg
	case llmproto.FinishReasonContentFilter:
		return cCommon.ChatNoticeContentFilter, cCommon.ConstChatNoticeContentFilterMsg
	default:
		return "", ""
	}
}

// InterruptChat 中断指定轮次进行中的流式聊天(用户点击中断按钮)。
// 仅在聊天已开始(流式进行中)时支持;该轮次不在聊天中则返回"未在聊天"错误。
// 键与 ChatStream 注册时一致(<sender>:<turnId>),并以发送者隔离不同用户。
func (s *ChatService) InterruptChat(sender, turnId string) error {
	key := cCommon.BuildTurnStreamKey(sender, cCommon.NormalizeCompactId(turnId))
	if !ctxmgr.GetTurnStreamMgr().Cancel(key) {
		return myException.ChatNotActiveKind.Newf("chat stream not active: %s", turnId)
	}
	logging.LogInfoF("chat stream: interrupt chat key:%s", key)
	return nil
}

// =============================== private functions & methods===========================

func (s *ChatService) normalizeRequest(comCode, sender string, req *request.ChatRequest) *request.ChatRequest {
	// 会话标识:统一带单位编码前缀 —— 无论客户端是否提供。
	clientSessionId := cCommon.NormalizeCompactId(req.SessionId)
	if clientSessionId == pCommon.BlankString {
		req.SessionId = cCommon.NewCompactId(comCode)
	} else {
		// 幂等:客户端**回传服务端签发的 session_id**时不能重复叠加单位前缀。
		// 否则 comCode + (comCode + uuid) 会算出另一个 session_id(且超长被截断),
		// 结果是:续写/恢复会话时被拆成新会话,历史永远接不上。
		// 触发场景:Phase2 的"会话列表/恢复会话"、飞书/微信等渠道适配器回传会话标识。
		prefix := cCommon.NormalizeCompactId(comCode)
		clientSessionId = strings.TrimPrefix(clientSessionId, prefix)
		req.SessionId = cCommon.NormalizeCompactId(comCode + clientSessionId)
	}

	req.TurnId = cCommon.NormalizeCompactId(req.TurnId)
	if req.TurnId == pCommon.BlankString {
		// 兜底生成时**不**带 sender 前缀:turnId 只保证"单用户内唯一"
		req.TurnId = cCommon.NewCompactId(pCommon.BlankString)
	}

	// 提问时间:客户端未提供时,以"服务端收到请求的时刻"兜底(Web 端目前不发送 genAt)。
	if req.GenAt <= 0 {
		req.GenAt = pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	}

	// chat type,默认为私聊
	if req.ChatType == 0 {
		req.ChatType = cCommon.ChatTypePrivate
	}

	// chat channel default = web
	if req.ChatChannel == 0 {
		req.ChatChannel = cCommon.ChatChannelWeb
	}

	return req
}

// acquireAgentGate 获取目标 Agent 的并发许可(每 Agent 闸)。
//   - 上限取 t_agent.max_concurrent;未配置(<=0)用 DefaultAgentConcurrency;
//   - 等待超过 AgentGateWaitTimeout 或请求 ctx 取消则失败:取消→按取消语义,超时→"Agent 忙"。
func (s *ChatService) acquireAgentGate(ctx context.Context, agent dataobject.TAgent) (func(), error) {
	limit := agent.MaxConcurrents
	if limit <= 0 {
		limit = cCommon.DefaultAgentConcurrency
	}
	if ctx == nil {
		ctx = context.Background()
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(cCommon.AgentGateWaitTimeout)*time.Second)
	defer cancel()

	release, err := core2.GetAgentGateRegistry().Acquire(waitCtx, agent.AgentId, limit)
	if err != nil {
		if ctx.Err() != nil {
			// 客户端已断开/超时:按取消语义静默处理
			return nil, myException.ChatCanceledKind.Newf("chat service: acquire agent gate canceled for agent %s", agent.AgentId)
		}
		return nil, myException.ChatBusyKind.Newf("chat service: agent %s busy, max concurrent %d", agent.AgentId, limit)
	}
	return release, nil
}

// prepareChat 执行同步/流式两条聊天路径的公共前置步骤:
// 校验/规整入参 → 覆盖 sender → 生成 sessionId → 缺省类型 → 解析 Agent/Provider → 组装 system prompt。
// 返回规范化后的请求、目标 Agent、系统提示词与解析出的 Provider,供 SendMessage/ChatStream 复用。
func (s *ChatService) prepareChat(ctx context.Context, req *request.ChatRequest, sender, comCode string) (*request.ChatRequest, dataobject.TAgent, *dataobject.SystemPrompt, dataobject.TLlm, *harness.ToolSet, error) {
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

	// 3. 会话/轮次标识:客户端可传入;为空则由服务端生成。
	//    统一规整为 <=40 字符的紧凑形式(t_session.session_id 与 t_turn.turn_id 均为 VARCHAR(40)):
	//    前端 crypto.randomUUID() 与服务端 GenerateUUID() 产出的是 36 字符标准 UUID,
	//    去分隔符后为 32 字符;服务端生成的带前缀 ID 最长 48 → 截断到 40。
	//    该值会被用作历史文件目录名,规整中的字符过滤顺带杜绝了路径穿越。
	s.normalizeRequest(comCode, sender, req)

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
		return nil, dataobject.TAgent{}, nil, dataobject.TLlm{}, nil, myException.ChatLLMKind.Newf("chat service: build system prompt error:%s", err.Error())
	}

	return req, agent, systemPrompt, llmProvider, harness.LoadToolSet(agentExtensionRoot, skillFiles), nil
}

// resolveAgent 解析本次会话的目标 Agent(混合路由)。
//   - 显式 agentId 优先:非空时精确查找,未命中/被禁用返回错误;
//   - 未显式指定时由 AgentRouter 按 会话绑定 → 关键词规则 → LLM 分类 → 默认 Agent 逐级回落;
//   - 会话绑定保证同一会话内上下文一致,并避免重复路由计算。
func (s *ChatService) resolveAgent(ctx context.Context, comCode, sessionId, reqMsg, agentId string) (dataobject.TAgent, error) {
	if s.agentRouter == nil {
		return dataobject.TAgent{}, pException.RuntimeErr("chat service: agent router not initialized")
	}
	return s.agentRouter.Resolve(ctx, comCode, sessionId, reqMsg, agentId)
}

// resolveProvider 解析 Agent 使用的 LLM Provider:优先 PrimaryModel,再按 Fallbacks 逐个回退。
// 均不可用时返回 ChatAgentNotFoundErr(视为"目标 Agent 不可用")。
func (s *ChatService) resolveProvider(comCode string, agent dataobject.TAgent) (dataobject.TLlm, error) {
	if s.agentRepo == nil {
		return dataobject.TLlm{}, pException.RuntimeErr("chat service: agent repository not initialized")
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

	agentExtensionRoot, _ := cCommon.BuildAgentExtensionRoot(agent)

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
// recorder 用于旁路记录本轮调用过的工具(可为 nil)。
func (s *ChatService) completeChat(ctx context.Context, llmProvider dataobject.TLlm, systemPrompt *dataobject.SystemPrompt, req *request.ChatRequest, toolSet *harness.ToolSet, recorder *turnRecorder) (string, error) {
	if s.functionCalling && toolSet != nil && len(toolSet.LLMTools()) > 0 {
		return s.runToolLoop(ctx, llmProvider, harness.BuildChatMessage(systemPrompt, req), toolSet, recorder)
	}
	return s.llmClient.ChatCompletion(ctx, llmProvider, systemPrompt, req)
}

// runToolLoop 同步路径的工具调用循环:调用LLM → 执行工具 → 结果回填 → 再次调用,直到模型给出最终文本。
func (s *ChatService) runToolLoop(ctx context.Context, llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, toolSet *harness.ToolSet, recorder *turnRecorder) (string, error) {
	for i := 0; i < cCommon.AgentToolLoopMax; i++ {
		content, calls, err := s.llmClient.CompleteWithTools(ctx, llmProvider, messages, toolSet.LLMTools())
		if err != nil {
			return "", myException.LlmError(err, "chat service: runtime tool call chat")
		}
		if len(calls) == 0 {
			return content, nil
		}
		messages = append(messages, harness.BuildToolCallWithContentMessage(content, calls)) // 缺口1:工具轮文本一并回填
		for _, call := range calls {
			callTime := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
			result, err := toolSet.Exec(ctx, call.Function.Name, call.Function.Arguments)
			respTime := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
			if err != nil {
				result = fmt.Sprintf(`{"error":%q}`, err.Error())
			}
			recorder.Record(call.Function.Name, callTime, respTime, result) // 旁路记录(recorder 为 nil 时静默)
			messages = append(messages, harness.BuildToolResultMessage(call.ID, result))
		}
	}
	return "", myException.ChatToolLoopKind.Newf("chat service: tool loop exceeded max iterations(%d)", cCommon.AgentToolLoopMax)
}

// streamToolLoop 流式路径的工具调用循环:
// 工具调用阶段实时推送给用户:模型声明调用(intent)→开始执行(running)→结果(done/error),
// 最终内容增量通过 onChunk 流式转发。底层仍为多次上游请求(OpenAI 兼容协议不支持服务端工具循环),
// 此处把多轮编排为一条面向客户端的事件流。
func (s *ChatService) streamToolLoop(ctx context.Context, llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, toolSet *harness.ToolSet, onChunk func(string), onStatus func(status, text string), onReasoning func(delta string), onTool func(evt ToolEvent), recorder *turnRecorder) (string, string, error) {
	seq := 0 // 本回合工具调用序号(跨轮自增,前端按 seq 追踪同一工具调用)
	for i := 0; i < cCommon.AgentToolLoopMax; i++ {
		if i > 0 && onStatus != nil {
			onStatus(cCommon.ChatStatusThinking, "正在思考…")
		}
		var roundIntents []int // 本轮模型声明的工具调用序号(与返回的 calls 一一对应)
		content, calls, finishReason, err := s.llmClient.StreamWithTools(ctx, llmProvider, messages, toolSet.LLMTools(), onChunk, onReasoning,
			func(name string) { // onToolIntent:模型流式声明将调用某工具,立即告知前端
				if onTool != nil {
					seq++
					roundIntents = append(roundIntents, seq)
					onTool(ToolEvent{Phase: cCommon.ChatToolEventIntent, Seq: seq, Name: name})
				}
			})
		if err != nil {
			return "", "", myException.LlmError(err, "chat service: runtime tool stream error")
		}
		if len(calls) == 0 {
			// 本轮无工具调用:流式内容(早已通过 onChunk 转发)即为最终结果
			return content, finishReason, nil
		}
		messages = append(messages, harness.BuildToolCallWithContentMessage(content, calls)) // 工具调用+文本一并回填

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
			item     toolRunItem
			result   string
			err      error
			callTime int64 // 工具调用时间(Unix 秒,旁路记录用)
			respTime int64 // 工具返回时间(Unix 秒,旁路记录用)
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

				//执行具体的工具
				res, err := toolSet.ExecStream(ctx, run.name, run.arg, func(line string) {
					lastOut.Store(pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().NanoTimestamp)
					// 脚本 stdout 行:仅投递到通道,由唯一转发 goroutine 推送(不在此处直接写 SSE)
					lineCh <- ToolEvent{Phase: cCommon.ChatToolEventProgress, Seq: run.seq, Name: run.name, Detail: line}
				})
				respTime := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
				close(stopHeart)
				<-heartDone
				results[k] = toolResultItem{item: run, result: res, err: err, callTime: startTime.Timestamp, respTime: respTime}
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
			return "", "", llmproto.ErrRequestCanceled
		}

		// 全部执行完成:按原始顺序回填 tool 消息并派发 done/error 事件
		for _, r := range results {
			if r.err != nil {
				if onTool != nil {
					onTool(ToolEvent{Phase: cCommon.ChatToolEventError, Seq: r.item.seq, Name: r.item.name, Detail: r.err.Error()})
				}
				recorder.Record(r.item.name, r.callTime, r.respTime, r.err.Error()) // 旁路记录(recorder 为 nil 时静默)
				messages = append(messages, harness.BuildToolResultMessage(r.item.callID, fmt.Sprintf(`{"error":%q}`, r.err.Error())))
				continue
			}
			if onTool != nil {
				onTool(ToolEvent{Phase: cCommon.ChatToolEventDone, Seq: r.item.seq, Name: r.item.name, Detail: pCommon.TruncateRunes(r.result, 600)})
			}
			recorder.Record(r.item.name, r.callTime, r.respTime, r.result) // 旁路记录(recorder 为 nil 时静默)
			messages = append(messages, harness.BuildToolResultMessage(r.item.callID, r.result))
		}
	}
	return "", "", myException.ChatToolLoopKind.Newf("chat service: tool stream loop exceeded max iterations(%d)", cCommon.AgentToolLoopMax)
}
