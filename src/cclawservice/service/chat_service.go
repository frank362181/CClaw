/**
* Chat 业务服务
*
* 职责:接收 聊天界面发来的消息,调用 LLM 引擎返回真实回复。
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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cCommon "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/harness"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/execbox"
	"my.favorite/cclawservice/harness/mstorage"
	"my.favorite/cclawservice/harness/security"
	"my.favorite/cclawservice/harness/tool"
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

// ChatService 处理聊天消息:校验 → 解析 Agent/Provider → 组装 prompt → 调用 LLM。
type ChatService struct {
	baseService.BaseService

	agentRepo   repo.IAgentRepository
	llmClient   *harness.Client
	agentRouter *ctxmgr.AgentRouter

	// memoryService 会话管理(阶段1双写:只写不读的旁路)。
	// 为 nil 或未启用时全部记录动作静默跳过,不影响聊天主流程。
	memoryService *mstorage.MemoryService

	functionCalling bool // 是否启用 function-calling(由 ini [sandBox] functionCalling 控制)

	// policyWarned 已报告过的"上下文预算配置异味"(按 agentId):
	// 配置问题只报一次,避免每轮请求刷屏(改配置后重启生效)。
	policyWarned sync.Map // map[string]bool
}

var chatService = pCommon.GetSingleton(func() *ChatService {
	return &ChatService{
		BaseService: baseService.BaseService{
			Name:        "chatService",
			Description: "receive chat messages and reply via the LLM engine",
		},
		agentRepo:   nil,
		llmClient:   harness.NewClient(),
		agentRouter: nil,
	}
}).Instance()

// GetChatService 构建聊天服务。
//   - db:             用于读取 Agent / LLM Provider / Skill 配置(与 AgentService 共用同一仓库单例)
//   - functionCalling:是否启用 function-calling(工具调用),来源于 [sandBox] functionCalling 配置
//   - sessionRecord:  是否启用会话记录(阶段1双写),来源于 [chat] sessionRecord 配置
func GetChatService(db core.IBaseDatabase, functionCalling bool, sessionRecord bool) baseService.IService {
	chatService.agentRepo = repo.GetAgentRepository(db)
	chatService.agentRouter = ctxmgr.GetAgentRouter(chatService.agentRepo)
	chatService.functionCalling = functionCalling
	chatService.memoryService = mstorage.GetMemoryService(db, sessionRecord)

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

// 一轮对话的准备产物(readonly;每turn新建,禁止缓存/池化)
type chatTurn struct {
	req          *request.ChatRequest
	agent        dataobject.TAgent
	systemPrompt *dataobject.SystemPrompt
	llm          dataobject.TLlm

	tools        tool.FunctionBundle //与skill.md文件绑定的，以及系统工具合成的实际工具列表
	visibleTools []llmproto.Tool     //本轮实际下发的工具列表
	toolsReserve int                 //本轮工具schema的token预算(每轮现算,不能缓存)

	plan  tool.FirstRoundToolPlan //首轮策略(由tools立刻算出)
	gate  tool.Activation         //意图门结果(命中技能/强制工具/被抑制的强制)
	scope string                  // "agent = xxx turn = yyy" ,日志与哨兵共用,只拼接一次
}

// scriptPathOf 取"工具名 → 脚本绝对路径",取不到时返回可读错误。
//
// 为什么要收在一处:同步与流式两条执行路径都要用它,而取值口径一旦分叉就会出现
// "一处传名字、一处传路径"的事故 —— 实测 2026-09-20 流式路径把工具名当脚本路径交给
// execbox,报 `script tool: script not found: get_weather`。
//
// 键与值都按 TrimSpace 归一化:键要和发给模型的 function.name / 模型回传的
// tool_calls[].function.name 严格同源;值必须是绝对路径(Scripts 已在 FunctionBundle
// 构建时解析成绝对路径,相对路径会被 execbox 当成相对进程 CWD 而 Stat 失败)。
func (t *chatTurn) scriptPathOf(toolName string) (string, error) {
	name := strings.TrimSpace(toolName)
	if t == nil || len(t.tools.Scripts) == 0 {
		return "", fmt.Errorf("tool %s: no script registered", name)
	}

	path := strings.TrimSpace(t.tools.Scripts[name])
	if path == pCommon.BlankString {
		return "", fmt.Errorf("tool %s: no script registered", name)
	}

	return path, nil
}

// SendMessage 接收并应答一条聊天消息(同步模式:等待完整回复后一次性返回)。
//   - sender: 服务端认证得到的登录名(由认证 interceptor 写入请求上下文),覆盖请求体 senderId 防伪造
//   - comCode: 服务端认证得到的单位编码,用于 Agent/Provider 的归属隔离(owner_id)
func (s *ChatService) SendMessage(ctx context.Context, req *request.ChatRequest, sender, comCode string) (*request.ChatResponse, error) {
	turn, err := s.prepareChat(ctx, req, sender, comCode)
	if err != nil {
		return nil, err
	}

	// 每 Agent 并发闸:同一 agent 同时生成数不超过 max_concurrent,覆盖整段生成(含工具循环)
	release, err := s.acquireAgentGate(ctx, turn.agent)
	if err != nil {
		return nil, err
	}
	defer release()

	// 会话建档(阶段1双写:旁路,只写不读;失败不影响聊天)
	session, recorded := s.memoryService.Begin(sender, turn.agent, req)

	// 上下文读回装配
	// (从历史文件读回本会话既有内容,拼成 system + 历史 + 本轮用户消息)
	assembled, records := s.assembleChatContext(session, turn.agent, turn.llm, turn.systemPrompt, req, turn.toolsReserve)

	// 调用 LLM 引擎取回完整回复(function-calling 开启且该 Agent 有工具时可能经过工具循环)
	recorder := mstorage.NewTurnRecorder()
	reply, err := s.completeChat(ctx, turn, assembled.Messages, recorder)
	if err != nil {
		// 中断轮次记录(旁路):同步路径没有向客户端输出内容,
		// 因此只在"本次失败确属中断/超时"时留痕(提问已发生,审计需要)。
		if recorded && cCommon.IsInterrupted(ctx, err) {
			s.memoryService.AppendInterruptedTurn(session, req, pCommon.BlankString, recorder.Snapshot(), cCommon.InterruptionReason(ctx, err))
		}
		// 工具循环被守卫中止时可能已有产出文本:同步接口只返回 err,不会把它交给客户端,
		// 但必须留下痕迹 —— 否则"模型到底说了什么"在日志与历史里都查不到(流式路径靠已下发的增量自然留痕)。
		if strings.TrimSpace(reply) != pCommon.BlankString {
			logging.LogWarnF("chat service: tool loop stopped with partial reply: %s", pCommon.TruncateRunes(reply, 200, "…"))
		}
		return nil, myException.LlmError(err, "chat service: runtime call chat")
	}

	// 轮次落库:问题/工具/回复写入历史文件并登记 t_turn / t_turn_tool
	turnSeq := 0
	if recorded {
		turnSeq = s.memoryService.AppendTurn(session, req, reply, recorder.Snapshot())
	}

	// 记忆维护(旁路、异步):按需压缩历史、按需蒸馏长期记忆
	s.memoryService.TriggerMemoryMaintenance(mstorage.MaintenanceRequest{
		Session:       &session,
		Agent:         &turn.agent,
		Assembled:     &assembled,
		Records:       records,
		TurnSeq:       turnSeq,
		ContextWindow: int(turn.llm.ContextWindow),
	}, s.summarizer(turn.llm))

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
func (s *ChatService) ChatStream(ctx context.Context, req *request.ChatRequest, sender, comCode string,
	onChunk func(string), onStatus func(status, text string), onReasoning func(delta string), onTool func(evt ToolEvent), onNotice func(code, msg string)) (string, error) {
	if onStatus != nil {
		onStatus(cCommon.ChatStatusPreparing, "正在解析任务…")
	}
	turn, err := s.prepareChat(ctx, req, sender, comCode)
	if err != nil {
		return "", err
	}

	// 每 Agent 并发闸:同一 agent 同时生成数不超过 max_concurrent,覆盖整段流式生成(含工具循环)
	release, err := s.acquireAgentGate(ctx, turn.agent)
	if err != nil {
		return "", err
	}
	defer release()

	// 注册为"进行中的轮次",供中断 API 按轮次取消(不同用户/轮次相互隔离)。
	// 派生可取消上下文:客户端断开或用户点击中断都会终止 LLM 流式读取。
	// 注册键必须是 <sender>:<turnId>:中断 API 只收到 turnId,用同一规则构造才能命中;
	// 以发送者为前缀则避免跨用户冒用 turnId 中断他人流。
	streamCtx, cancel := context.WithCancel(ctx)
	key := hCommon.BuildTurnStreamKey(sender, req.TurnId)
	ctxmgr.GetTurnStreamMgr().Register(key, cancel)
	defer ctxmgr.GetTurnStreamMgr().Unregister(key)

	// 应用级运行超时:界定"这一轮任务最多跑多久"(工具循环 + LLM 生成整段)。
	timeout := time.Duration(pCommon.GetEnvIntDefault(hCommon.ChatRunTimeout, hCommon.AgentRunTimeout)) * time.Second
	runCtx, runCancel := context.WithTimeout(streamCtx, timeout)
	defer runCancel()

	if onStatus != nil {
		onStatus(cCommon.ChatStatusThinking, "正在思考…")
	}

	// 会话建档(阶段1双写:旁路,只写不读;失败不影响聊天)
	session, recorded := s.memoryService.Begin(sender, turn.agent, req)

	// 将system,user,memory等信息进行装配
	assembled, records := s.assembleChatContext(session, turn.agent, turn.llm, turn.systemPrompt, req, turn.toolsReserve)

	// 累计已产出的回复:中断/失败时也要把"用户已经看到的内容"记入历史,
	// 否则下一轮读回会出现"问题在、回答不在"的断裂。
	// 回调由读流循环同步调用,与下面的读取处于同一协程,无需加锁。
	var streamed strings.Builder
	forwardChunk := onChunk
	if onChunk != nil {
		forwardChunk = func(delta string) {
			streamed.WriteString(delta)
			onChunk(delta)
		}
	}

	// function-calling 开启且该 Agent 有可用工具时,走工具调用循环(工具阶段状态提示,最终内容流式转发);
	// 否则保持原有的纯流式转发。
	recorder := mstorage.NewTurnRecorder()
	var reply, finishReason string
	if s.functionCalling && len(turn.tools.Tools) > 0 {
		reply, finishReason, err = s.streamToolLoop(runCtx, turn, assembled.Messages, forwardChunk, onStatus, onReasoning, onTool, onNotice, recorder)
	} else {
		reply, finishReason, err = s.llmClient.ChatCompletionStreamMessages(runCtx, turn.llm, assembled.Messages, forwardChunk, onReasoning)
	}
	if err != nil {
		// 中断轮次记录(旁路):已产出部分内容,或本次失败属中断/超时,都要留痕
		if recorded && (strings.TrimSpace(streamed.String()) != pCommon.BlankString || cCommon.IsInterrupted(runCtx, err)) {
			s.memoryService.AppendInterruptedTurn(session, req, streamed.String(), recorder.Snapshot(), cCommon.InterruptionReason(runCtx, err))
		}

		// 应用级运行超时:给可读错误,而不是笼统的 "request canceled by client"
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return pCommon.BlankString, myException.ChatRunTimeoutKind.Newf("chat service: run timeout after %d seconds", pCommon.GetEnvIntDefault(hCommon.ChatRunTimeout, hCommon.AgentRunTimeout))
		}
		return pCommon.BlankString, myException.LlmError(err, "chat service: runtime stream error")
	}

	// finish_reason 收尾:长度截断/内容合规 → 以独立 notice 事件告知前端(不污染回答正文)。
	// 无任何正式内容且被内容安全策略拦截时,按内容合规业务错误返回(交由控制器提示)。
	if code, msg := cCommon.FinishReasonNotice(finishReason); code != "" {
		if strings.TrimSpace(reply) == pCommon.BlankString && finishReason == llmproto.FinishReasonContentFilter {
			return pCommon.BlankString, myException.ChatLLMContentFilterKind.Newf("chat service: output blocked by content filter")
		}
		if onNotice != nil {
			onNotice(code, msg)
		}
	}

	// 轮次落库(阶段1双写:旁路,只写不读;失败不影响回复)
	turnSeq := 0
	if recorded {
		turnSeq = s.memoryService.AppendTurn(session, req, reply, recorder.Snapshot())
	}

	// 记忆维护(旁路、异步):与同步路径共用同一触发条件
	s.memoryService.TriggerMemoryMaintenance(mstorage.MaintenanceRequest{
		Session:       &session,
		Agent:         &turn.agent,
		Assembled:     &assembled,
		Records:       records,
		TurnSeq:       turnSeq,
		ContextWindow: int(turn.llm.ContextWindow),
	}, s.summarizer(turn.llm))
	return reply, nil
}

// EndChatSession 结束会话:写结束标记,并做一次**收尾蒸馏**(把尾巴带走)。
//
// 为什么收尾必须蒸一次:蒸馏节拍是每 DistillIntervalTurns 轮一次,而会话在哪一轮停下是随机的 ——
// 最后不足一个节拍窗口的那几轮如果不在这里带走,就永远进不了长期记忆。
// 真实数据:22 轮会话只在第 10/20 轮蒸过(第 21-22 轮没蒸);只聊到 8 轮的会话一次都没蒸过。
//
// 异步执行(与其它维护一致):收尾蒸馏要调一次 LLM,不该阻塞结束会话的调用方。
// 调用方:空闲超时扫描 / 手动结束会话的接口(目前没有生产调用点,先把编排与判定做好)。
func (s *ChatService) EndChatSession(session dataobject.TSession, agent dataobject.TAgent,
	llmProvider dataobject.TLlm, reason int) error {
	if s.memoryService == nil || !s.memoryService.Enabled() {
		return nil
	}

	if err := s.memoryService.EndSession(session.SessionId, reason); err != nil {
		logging.LogWarnF("chat service: end session %s error:%s", session.SessionId, err.Error())
		return err
	}

	records, err := s.memoryService.ReadRecords(session)
	if err != nil {
		logging.LogWarnF("chat service: read records before closing %s error:%s", session.SessionId, err.Error())
	}

	// closing=true:不受节拍约束,直接把水位之后的尾巴蒸掉(NeedDistill 的收尾维度)。
	// 没有可蒸内容时 Distill 内部自己会跳过,不会白烧调用。
	// Assembled=nil:收尾路径没有"本轮装配结果",由 TriggerMemoryMaintenance 归一(zero 值)。
	s.memoryService.TriggerMemoryMaintenance(mstorage.MaintenanceRequest{
		Session:       &session,
		Agent:         &agent,
		Assembled:     nil,
		Records:       records,
		Closing:       true,
		ContextWindow: int(llmProvider.ContextWindow),
	}, s.summarizer(llmProvider))
	return nil
}

// InterruptChat 中断指定轮次进行中的流式聊天(用户点击中断按钮)。
// 仅在聊天已开始(流式进行中)时支持;该轮次不在聊天中则返回"未在聊天"错误。
// 键与 ChatStream 注册时一致(<sender>:<turnId>),并以发送者隔离不同用户。
func (s *ChatService) InterruptChat(sender, turnId string) error {
	key := hCommon.BuildTurnStreamKey(sender, cCommon.NormalizeCompactId(turnId))
	if !ctxmgr.GetTurnStreamMgr().Cancel(key) {
		return myException.ChatNotActiveKind.Newf("chat stream not active: %s", turnId)
	}
	logging.LogInfoF("chat stream: interrupt chat key:%s", key)
	return nil
}

// =============================== private functions & methods===========================

// 会话/轮次标识:客户端可传入;为空则由服务端生成。
// 统一规整为 <=40 字符的紧凑形式(t_session.session_id 与 t_turn.turn_id 均为 VARCHAR(40)):
// 前端 crypto.randomUUID() 与服务端 GenerateUUID() 产出的是 36 字符标准 UUID,
// 去分隔符后为 32 字符;服务端生成的带前缀 ID 最长 48 → 截断到 40。
// 该值会被用作历史文件目录名,规整中的字符过滤顺带杜绝了路径穿越。
func (s *ChatService) normalizeRequest(comCode, sender string, req *request.ChatRequest) *request.ChatRequest {
	if strings.TrimSpace(sender) != pCommon.BlankString {
		req.SenderId = sender
	}

	// 会话标识:统一带单位编码前缀 —— 无论客户端是否提供。
	clientSessionId := cCommon.NormalizeCompactId(req.SessionId)
	if clientSessionId == pCommon.BlankString {
		req.SessionId = cCommon.NewCompactId(comCode)
	} else {
		// 幂等:客户端**回传服务端签发的 session_id**时不能重复叠加单位前缀。
		// 否则 comCode + (comCode + uuid) 会算出另一个 session_id(且超长被截断),
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
func (s *ChatService) prepareChat(ctx context.Context, req *request.ChatRequest, sender, comCode string) (*chatTurn, error) {
	if req == nil {
		return nil, pException.ParameterEmptyErrorEx("chat request is nil")
	}

	// 1. 校验消息内容
	req.ReqMsg = strings.TrimSpace(req.ReqMsg)
	if req.ReqMsg == "" {
		return nil, pException.ParameterEmptyErrorEx("chat message is empty")
	}
	if len([]rune(req.ReqMsg)) > MaxChatMessageLen {
		return nil, pException.ParameterEmptyErrorEx("chat message is too long")
	}

	// 2. 对请求进行规整
	s.normalizeRequest(comCode, sender, req)

	// 3. 解析目标 Agent(混合路由:显式 → 会话绑定 → 关键词 → LLM 分类 → 默认)
	agent, err := s.resolveAgent(ctx, comCode, req.SessionId, req.ReqMsg, req.AgentId)
	if err != nil {
		return nil, err
	}

	// 4. 解析 LLM Provider(PrimaryModel 优先,Fallbacks 兜底)
	llmProvider, err := s.resolveProvider(comCode, agent)
	if err != nil {
		return nil, err
	}

	// 5. 基于 Agent 工作区启动文档组装 system prompt,并解析其 skill 中声明的可用工具
	skillFiles, skillRoot := s.collectSkillFiles(agent)

	// 5.1 先解析工具与“服务它们的技能”(技能级元数据:触发词/正文/强制工具)
	bundle := tool.GetFunctionRepository().GetFunctionBundle(skillRoot, skillFiles)

	// 5.2 意图门:按**本轮问题**判定哪些技能相关。根据用户的请求命中关键词，或force-call决定哪些skill文件注入system prompt
	// 命中才允许该技能的 force-call 生效、才注入其正文(SOP)。
	gate := bundle.Activate(req.ReqMsg)

	// 5.3 组装 system prompt(命中技能的正文一并注入;未命中不注入,避免稀释注意力)
	systemPrompt, err := ctxmgr.BuildSystemPromptExWithActiveSkills(agent, skillFiles, dataobject.BootstrapScopeNormal, gate.ActiveFiles)
	if err != nil {
		logging.LogErrorF("chat service: build system prompt error:%s", err.Error())
		return nil, myException.ChatLLMKind.Newf("chat service: build system prompt error:%s", err.Error())
	}

	// 5.4 首轮策略:只对“命中技能内的 force-call 工具”强制调用(工具列表保持全量)
	visibleTools := bundle.VisibleTools(gate)
	plan := tool.BuildFirstRoundToolPlan(visibleTools, gate.ForcedTools) //近对本轮可见的工具有效

	// 工具 schema 占窗口但与历史竞争同一份额度:按**本轮实际下发的工具**现算,
	// 不做按 agentId 的跨轮缓存(可见工具会随门/配置变化,旧实现无失效键 + 空值写死)。
	toolsReserveTokens := security.EstimateToolReserveTokens(agent.AgentId, visibleTools)

	// 上下文预算的配置异味(预留吃光窗口 / 把系统预留当历史旋钮)只报一次
	s.warnPolicyIssues(agent, llmProvider, systemPrompt, toolsReserveTokens)

	turn := &chatTurn{
		req:          req,
		agent:        agent,
		systemPrompt: systemPrompt,
		llm:          llmProvider,
		tools:        bundle,
		visibleTools: visibleTools,
		toolsReserve: toolsReserveTokens,
		plan:         plan,
		gate:         gate,
		scope:        fmt.Sprintf("agent=%s,turn=%s", agent.AgentId, req.TurnId),
	}

	// active=命中的技能,keyword=命中的触发词,visible tools=本轮下发数/总工具数,reserve=占用的窗口额度
	logging.LogInfoF("chat service: %s gate=%s visible_tools=%d/%d reserve=%dt plan=%s",
		turn.scope, gate.String(), len(visibleTools), len(bundle.Tools), toolsReserveTokens, plan.String())
	return turn, nil
}

// warnPolicyIssues 报告"上下文预算"上明显不合理的配置(每个 agent 只报一次)。
//
// 典型两类错误:
//   - 系统提示预留 + 输出预留 + 工具预留 ≥ 窗口 ⇒ 历史永不回放且压缩被禁用(静默失效);
//   - 系统提示预留远大于实测系统提示 ⇒ 多半是想调"历史回放多少",应该用 max_history_tokens。
func (s *ChatService) warnPolicyIssues(agent dataobject.TAgent, llmProvider dataobject.TLlm, systemPrompt *dataobject.SystemPrompt, toolsReserve int) {
	window := int(llmProvider.ContextWindow)
	if window <= 0 {
		return
	}
	if _, warned := s.policyWarned.Load(agent.AgentId); warned {
		return
	}

	policy := hCommon.GetContextPolicy(agent, window)
	policy.ToolsReserveTokens = toolsReserve

	systemText := pCommon.BlankString
	if systemPrompt != nil {
		systemText = systemPrompt.Prompt
	}
	systemTokens := security.EstimateInputTokens([]llmproto.ChatMessage{{Role: hCommon.RoleSystem, Content: systemText}})

	for _, issue := range security.PolicyWarnings(policy, systemTokens) {
		logging.LogWarnF("chat service: agent=%s context budget issue: %s", agent.AgentId, issue)
	}
	s.policyWarned.Store(agent.AgentId, true)
}

// resolveAgent 解析本次会话的目标 Agent(混合路由)。
//   - 显式 agentId 优先:非空时精确查找,未命中/被禁用返回错误;
//   - 未显式指定时由 AgentRouter 按 会话绑定 → 关键词规则 → LLM 分类 → 默认 Agent 逐级回落;
//   - 会话绑定保证同一会话内上下文一致,并避免重复路由计算。
func (s *ChatService) resolveAgent(ctx context.Context, comCode, sessionId, reqMsg, agentId string) (dataobject.TAgent, error) {
	if s.agentRouter == nil {
		return dataobject.TAgent{}, pException.RuntimeErr("chat service: agent router not initialized")
	}

	agentRouter := harness.NewClient()
	return s.agentRouter.Resolve(ctx, comCode, sessionId, reqMsg, agentId, agentRouter.Complete)
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

// collectSkillFiles 收集当前 Agent 启用的 skill 文档路径(SKILL.md 绝对路径),并返回 skills 根目录。
//
// 布局约定:所有技能统一位于 <exeDir>/cclaw/skills/<skill_dir>/SKILL.md,
// 由 t_skill.skill_dir 定位(相对 skills 根的安全相对路径,一个目录一个技能)。
// 目录非法/文件缺失都不影响主流程:非法目录告警后跳过,文件缺失由仓库告警后跳过。
// 返回值:SKILL.md 路径列表 + skills 根目录(供 ToolSet 解析脚本工具时做安全校验)。
func (s *ChatService) collectSkillFiles(agent dataobject.TAgent) ([]string, string) {
	skillRoot, _ := hCommon.BuildSkillRoot()

	if s.agentRepo == nil {
		return nil, skillRoot
	}

	skills := s.agentRepo.GetSkillsByAgent(agent)
	if len(skills) == 0 {
		return nil, skillRoot
	}

	files := make([]string, 0, len(skills))
	for i := range skills {
		docPath, err := hCommon.ResolveSkillDocPath(skills[i].SkillDir)
		if err != nil {
			logging.LogWarnF("chat service: agent=%s skill=%s has an illegal skill dir[%s]:%s",
				agent.AgentId, skills[i].SkillId, skills[i].SkillDir, err.Error())
			continue
		}
		files = append(files, docPath)
	}

	return files, skillRoot
}

// assembleChatContext 读回会话历史并装配成本轮完整的 LLM 消息序列。
//
// 装配形态:[system(系统提示 + 历史摘要)] + [历史消息(预算内)] + [本轮用户消息]。
//
// 旁路语义:读回失败一律降级为"仅系统提示 + 本轮问题"(与阶段1行为一致),只记日志,
// 绝不影响本轮对话 —— 记忆能力是增强,不是聊天链路的硬依赖。
//
// 返回结果同时给出"是否因预算裁掉了历史"等判定,供记忆压缩编排使用;
// 并回传本次读回的记录 —— 维护触发要按"水位之后还剩多少"决定要不要蒸,
// 直接复用这份读回结果,避免在请求路径上再读一次。
func (s *ChatService) assembleChatContext(session dataobject.TSession, agent dataobject.TAgent, llmProvider dataobject.TLlm,
	systemPrompt *dataobject.SystemPrompt, req *request.ChatRequest, toolsReserveTokens int) (hCommon.AssembleResult, []hCommon.MemoryRecord) {
	policy := hCommon.GetContextPolicy(agent, int(llmProvider.ContextWindow))
	policy.ToolsReserveTokens = toolsReserveTokens

	systemText := pCommon.BlankString
	if systemPrompt != nil {
		systemText = systemPrompt.Prompt
	}

	records, err := s.memoryService.ReadRecords(session) //读取历史记录
	if err != nil {
		logging.LogWarnF("chat service: read memory records of %s error:%s", session.SessionId, err.Error())
		records = nil
	}

	return ctxmgr.AssembleContext(systemText, records, req.ReqMsg, policy), records
}

// completeChat 取回完整回复:function-calling 开启且该 Agent 有可用工具时走工具循环,否则走普通同步调用。
//   - messages: 已装配好的完整消息序列(system + 历史 + 本轮用户消息)
//   - recorder: 旁路记录本轮调用过的工具(可为 nil)
func (s *ChatService) completeChat(ctx context.Context, turn *chatTurn, messages []llmproto.ChatMessage, recorder *mstorage.TurnRecorder) (string, error) {
	if s.functionCalling && len(turn.visibleTools) > 0 {
		return s.runToolLoop(ctx, turn, messages, recorder)
	}
	return s.llmClient.ChatCompletionMessages(ctx, turn.llm, messages)
}

// runToolLoop 同步路径的工具调用循环:调用LLM → 执行工具 → 结果回填 → 再次调用,直到模型给出最终文本。
//
// 每轮请求前都做一次预算贴合(FitMessages):工具结果不断回填会持续挤压上下文窗口,
// 长工具链必须随时把最老的历史裁掉,否则会遇到上游"超窗口"报错。
func (s *ChatService) runToolLoop(ctx context.Context, turn *chatTurn, messages []llmproto.ChatMessage, recorder *mstorage.TurnRecorder) (string, error) {
	policy := hCommon.ContextPolicy{ContextWindow: int(turn.llm.ContextWindow)}
	// 工具死循环守卫:一轮对话一个实例(局部变量),禁止做成服务字段/单例,原因见 chat_tool_guard.go。
	guard := security.NewTurnLoopGuard()

	for i := 0; i < hCommon.AgentToolLoopMax; i++ {
		roundTools, toolChoice := turn.plan.RoundTools(turn.visibleTools, i) //根据轮次决定required参数
		content, calls, err := s.llmClient.CompleteWithTools(ctx, turn.llm, ctxmgr.FitMessages(turn.llm, messages, policy), roundTools, toolChoice)
		tool.VerifyForcedCall(turn.plan, calls, toolChoice, turn.scope) //哨兵:按本轮实际发送的 choice 判断

		if err != nil {
			return "", myException.LlmError(err, "chat service: runtime tool call chat")
		}
		if len(calls) == 0 {
			return content, nil
		}
		if guard.ShouldStop() {
			// 已提示过且**再次命中**守卫(模型没换策略)→ 终止本轮,不再执行工具;
			// 这一轮已产出的文本仍带回调用方(用于留痕/日志,避免"内容全丢")。
			return content, guard.StopError("chat service: tool loop")
		}
		messages = append(messages, ctxmgr.BuildToolCallWithContentMessage(content, calls)) // 工具轮文本一并回填
		for _, call := range calls {
			callTime := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
			result, err := execbox.RunScript(ctx, turn.tools.Scripts[call.Function.Name], call.Function.Arguments)
			respTime := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
			if err != nil {
				result = fmt.Sprintf(`{"error":%q}`, err.Error())
			}
			recorder.Record(call.Function.Name, callTime, respTime, result) // 旁路记录(recorder 为 nil 时静默)
			// 死循环判定:同步路径本身串行执行,顺序天然等于"模型声明的顺序"(同步接口无 notice 通道)
			result = guard.ApplyLoopGuard(call.Function.Name, call.Function.Arguments, err == nil, result, nil)
			messages = append(messages, ctxmgr.BuildToolResultMessage(call.ID, result))
		}
	}
	return "", myException.ChatToolLoopKind.Newf("chat service: tool loop exceeded max iterations(%d)", hCommon.AgentToolLoopMax)
}

// streamToolLoop 流式路径的工具调用循环:
// 工具调用阶段实时推送给用户:模型声明调用(intent)→开始执行(running)→结果(done/error),
// 最终内容增量通过 onChunk 流式转发。底层仍为多次上游请求(OpenAI 兼容协议不支持服务端工具循环),
// 此处把多轮编排为一条面向客户端的事件流。
func (s *ChatService) streamToolLoop(ctx context.Context, turn *chatTurn, messages []llmproto.ChatMessage,
	onChunk func(string), onStatus func(status, text string), onReasoning func(delta string), onTool func(evt ToolEvent), onNotice func(code, msg string), recorder *mstorage.TurnRecorder) (string, string, error) {
	seq := 0 // 本回合工具调用序号(跨轮自增,前端按 seq 追踪同一工具调用)
	policy := hCommon.ContextPolicy{ContextWindow: int(turn.llm.ContextWindow)}

	// 工具死循环守卫:一轮对话一个实例
	guard := security.NewTurnLoopGuard()
	// 首轮:含 force-call 工具(取实时数据)的技能强制模型调用工具;之后交回 auto(见 forcedToolChoice)
	//toolChoice := toolSet.ForceToolChoice() //forcedToolChoice(toolSet)
	for i := 0; i < hCommon.AgentToolLoopMax; i++ {
		if i > 0 && onStatus != nil {
			onStatus(cCommon.ChatStatusThinking, "正在思考…")
		}

		roundTools, toolChoice := turn.plan.RoundTools(turn.visibleTools, i)

		var roundIntents []int // 本轮模型声明的工具调用序号(与返回的 calls 一一对应)
		content, calls, finishReason, err := s.llmClient.StreamWithTools(ctx, turn.llm, ctxmgr.FitMessages(turn.llm, messages, policy), roundTools, toolChoice, onChunk, onReasoning,
			func(name string) { // onToolIntent:模型流式声明将调用某工具,立即告知前端
				if onTool != nil {
					seq++
					roundIntents = append(roundIntents, seq)
					onTool(ToolEvent{Phase: cCommon.ChatToolEventIntent, Seq: seq, Name: name})
				}
			})
		tool.VerifyForcedCall(turn.plan, calls, toolChoice, turn.scope) //哨兵:按本轮实际发送的 choice 判断

		if err != nil {
			return "", "", myException.LlmError(err, "chat service: runtime tool stream error")
		}
		if len(calls) == 0 {
			// 本轮工具调用结束，唯一正常结束
			return content, finishReason, nil
		}
		if guard.ShouldStop() {
			// 已提示过且**再次命中**守卫(模型没换策略)→ 终止本轮,不再执行工具。
			return content, finishReason, guard.StopError("chat service: stream tool loop")
		}
		messages = append(messages, ctxmgr.BuildToolCallWithContentMessage(content, calls)) // 工具调用+文本一并回填

		// 同轮多个工具调用并行执行(goroutine 各自跑脚本);
		// 脚本 stdout 经 lineCh 逐行汇集,由唯一转发 goroutine 串行推送 progress 事件(保证 SSE 无并发写)。
		type toolRunItem struct {
			seq    int
			name   string
			callID string
			arg    string
			script string // 脚本绝对路径(派发前解析好;解析失败时填可读占位,见下)
		}
		runs := make([]toolRunItem, len(calls))
		for k, call := range calls {
			callSeq := seq
			if k < len(roundIntents) {
				callSeq = roundIntents[k]
			}

			runs[k] = toolRunItem{seq: callSeq, name: call.Function.Name, callID: call.ID, arg: call.Function.Arguments, script: turn.tools.Scripts[call.Function.Name]}
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
					timeout := time.Duration(hCommon.ToolCallingHeartbeatInterval) * time.Second
					ticker := time.NewTicker(timeout)
					defer ticker.Stop()
					for {
						select {
						case <-stopHeart:
							return
						case now := <-ticker.C: //此时返回的当前的时间（超时）
							if time.Duration(now.UnixNano()-lastOut.Load()) < (timeout) {
								continue // 期间有新输出(未静默),不打扰用户
							}
							lastOut.Store(now.UnixNano())
							lineCh <- ToolEvent{Phase: cCommon.ChatToolEventProgress, Seq: run.seq, Name: run.name,
								Detail: fmt.Sprintf("… 工具运行中(%d 秒,无新输出) …", int(now.Unix()-startTime.Timestamp))}
						}
					}
				}()

				//执行具体的工具(run.script 已在派发前解析为绝对路径)
				res, err := execbox.RunScriptStream(ctx, run.script, run.arg, func(line string) {
					lastOut.Store(pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().NanoTimestamp)
					// json串不需要输出到UI界面上
					if pCommon.LooksLikeJson(line) {
						return
					}
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
				result := fmt.Sprintf(`{"error":%q}`, r.err.Error())

				// 死循环判定必须落在串行回填点(同轮工具是并发执行的,这里才是"模型声明的顺序")
				loopGuardResult := guard.ApplyLoopGuard(r.item.name, r.item.arg, false, result, onNotice)
				messages = append(messages, ctxmgr.BuildToolResultMessage(r.item.callID, loopGuardResult))
				continue
			}
			if onTool != nil {
				// done 事件只报"完成",不回传结果 JSON:结果是给模型的,聊天界面对它没有价值
				// (前端按 phase 显示 "✓ 完成";失败仍走 error 事件携带错误文本,便于排查)
				onTool(ToolEvent{Phase: cCommon.ChatToolEventDone, Seq: r.item.seq, Name: r.item.name})
			}
			recorder.Record(r.item.name, r.callTime, r.respTime, r.result) // 旁路记录(recorder 为 nil 时静默)
			loopGuardResult := guard.ApplyLoopGuard(r.item.name, r.item.arg, true, r.result, onNotice)
			messages = append(messages, ctxmgr.BuildToolResultMessage(r.item.callID, loopGuardResult))
		}
	}
	return "", "", myException.ChatToolLoopKind.Newf("chat service: tool stream loop exceeded max iterations(%d)", hCommon.AgentToolLoopMax)
}

// summarizer 把 LLM 客户端包装为"摘要生成器"注入 mstorage。
func (s *ChatService) summarizer(llmProvider dataobject.TLlm) hCommon.Summarizer {
	return func(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
		// 输出额度按**窗口**收口(见 security.CalcMaintenanceOutputTokens):维护调用的输入同样占窗口,
		// 若无脑发 SummaryMaxTokens,小窗模型上会变成"输入 + 输出 > 窗口"的必然失败 ——
		// 而且失败只发生在后台日志里。装不下时直接报错,不发起注定失败的调用。
		maxTokens := security.CalcMaintenanceOutputTokens(int(llmProvider.ContextWindow), hCommon.SummaryMaxTokens, systemPrompt, userPrompt)
		if maxTokens <= 0 {
			return pCommon.BlankString, myException.RuntimeClientErrEx(fmt.Sprintf(
				"maintenance call does not fit the context window: window=%d", llmProvider.ContextWindow))
		}

		text, finishReason, err := s.llmClient.Complete(ctx, llmProvider, systemPrompt, userPrompt, maxTokens)
		if err != nil {
			return text, err
		}
		if finishReason == llmproto.FinishReasonLength {
			return pCommon.BlankString, myException.RuntimeClientErrEx(
				"summarizer output was truncated by max_tokens (finish_reason=length)")
		}
		return text, nil
	}
}
