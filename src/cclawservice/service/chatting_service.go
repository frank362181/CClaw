package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"my.favorite/cclawservice/biz"
	"my.favorite/cclawservice/chat"
	chatCommon "my.favorite/cclawservice/chat/common"
	"my.favorite/cclawservice/chat/plan_mode"
	"my.favorite/cclawservice/common"
	"my.favorite/cclawservice/config"
	myException "my.favorite/cclawservice/exception"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/toolexecutor"
	"my.favorite/cclawservice/model/dataaccessor"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/model/repo"
	core2 "my.favorite/cclawservice/service/core"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/concurrency"
	"my.favorite/platformutility/database/core"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// negotiation 的三种揭露
type planModeDecision int

const (
	// 未进入plan mode:技能未激活 / mode = off/协商判定不适用
	planModeDecisionSkip planModeDecision = iota

	// 计划就绪,将进入plan mode
	planModeDecisionRun

	// 已由协商给出结论(追问/参数不可用),答复文本在answer中
	planModeDecisionHandled
)

// 用于service的判断
type planModeResult struct {
	Decision planModeDecision
	Plan     plan_mode.Plan  // planModeRun时有效
	Spec     chatCommon.Spec //npa.ReportSpec // planModeRun时有效
	Answer   string          // planModeHandled 时作为本轮文本
}

// ChattingService 只做编排
type ChattingService struct {
	baseService.BaseService

	chattingConfig  config.ChatConfig
	sessionAccessor dataaccessor.ISessionAccessor

	memoryService *ctxmgr.MemoryService

	preparer  chatCommon.ITurnPreparer
	assembler chatCommon.IContextAssembler

	transport   chatCommon.ITransportClient
	syncLooper  *chat.ToolLoop
	asyncLooper *chat.ToolLoop

	journal          chatCommon.IMemoryJournal
	memoryMaintainer chatCommon.IMemoryMaintainer
	agentConcurrent  concurrency.IConcurrencyController // 按照Agent维度的并发控制器
	turnRegistrator  chatCommon.ITurnRegistrator
	turnReporter     chatCommon.ITurnReporter

	toolConfiguredRules toolexecutor.ToolConfigRules

	planModeEngine *plan_mode.PlanModeEngine
}

var chattingService = pCommon.GetSingleton(func() *ChattingService {
	return &ChattingService{
		turnRegistrator: chat.GetChatTurnRegistrator(), //流式聊天的轮次注册器,用户用户在UI界面上的中断使用
	}
}).Instance()

func GetChattingService(db core.IBaseDatabase, chattingConfig config.ChatConfig, agentRepo *repo.AgentRepository, toolRules toolexecutor.ToolConfigRules) *ChattingService {
	if db == nil || agentRepo == nil {
		panic("service.GetChattingService: db / agentRepo must not be nil")
	}

	// 初始化session数据库访问类
	chattingService.sessionAccessor, _ = dataaccessor.GetSessionAccessor(db)
	orchestratorAccessor, _ := dataaccessor.GetOrchestratorAccessor(db)
	if orchestratorAccessor != nil {
		if schemaErr := orchestratorAccessor.CheckSchema(); schemaErr != nil {
			logging.LogErrorF("chatting service: orchestration tables NOT ready, "+
				"plan persistence/audit will fail; run data: %s", schemaErr.Error())
		}
	}

	chattingService.chattingConfig = chattingConfig // 设置chatting configurations

	// 1. 传输层
	transport := chat.NewTransportClient(hCommon.NonStreamResponseTimeout)

	// 2. 装配器
	chattingService.memoryService = ctxmgr.GetMemoryService(db, chattingService.chattingConfig.Chat.SessionRecord)
	assembler := chat.GetAssembler(chattingService.memoryService)

	chattingService.preparer = chat.GetChatPreparer(agentRepo, chattingService.chattingConfig.ToolRule.FunctionCalling, transport.SimpleComplete)
	chattingService.agentConcurrent = concurrency.GetConcurrentAdvance()
	chattingService.assembler = assembler
	chattingService.transport = transport
	chattingService.syncLooper = chat.NewToolLoop(transport, assembler, false) // 同步执行
	chattingService.asyncLooper = chat.NewToolLoop(transport, assembler, true) // 异步执行
	chattingService.memoryMaintainer = chat.GetMemoryMaintainer(chattingService.memoryService, transport)
	chattingService.journal = chat.GetMemoryJournal(chattingService.memoryService)
	chattingService.turnRegistrator = chat.GetChatTurnRegistrator()
	chattingService.turnReporter = chat.GetTurnReporter()

	chattingService.toolConfiguredRules = toolRules

	// 创建一个plan mode聊天执行器
	extractors := plan_mode.NewExtractors()
	barriers := plan_mode.NewBarrierRepository()
	biz.RegistryNPABuiltin(extractors, barriers)

	chattingService.planModeEngine = plan_mode.NewPlanModeEngine(plan_mode.PlanModeEngineParams{
		Config:     chattingService.chattingConfig,
		Negotiator: plan_mode.NewIntentionNegotiator(chattingService.transport, chattingService.chattingConfig),
		Storage:    core2.GetOrchestratorStorage(orchestratorAccessor),
		ConGate:    chattingService.agentConcurrent,
		Extractors: extractors,
		Barriers:   barriers,
	})

	return chattingService
}

func (s *ChattingService) GetSessions(loginName string, limit int) ([]dataobject.TSession, error) {
	loginName = strings.TrimSpace(loginName)
	if len(loginName) == 0 {
		return nil, pException.ParameterEmptyErrorEx2("chattingService.GetSessions: empty loginName")
	}
	return s.sessionAccessor.ListSessions(loginName, limit)
}

func (s *ChattingService) GetSessionTurns(sessionId string, loginName string, limit int) ([]dataobject.SessionTurnView, error) {
	sessionId = strings.TrimSpace(sessionId)
	loginName = strings.TrimSpace(loginName)
	if len(sessionId) == 0 || len(loginName) == 0 {
		return nil, pException.ParameterEmptyErrorEx2("sessionId or LoginName must not be empty")
	}

	session, err := s.sessionAccessor.GetSession(sessionId)
	if err != nil {
		return nil, err
	}

	if session == nil {
		return []dataobject.SessionTurnView{}, nil
	}

	// 越权保护:只允许读取当前登录用户自己的会话
	if sender := strings.TrimSpace(session.Sender); len(sender) > 0 && !strings.EqualFold(sender, strings.TrimSpace(loginName)) {
		return nil, myException.PermissionDeniedKind.Newf("session %s does not belong to current user", sessionId)
	}

	turns, err := s.sessionAccessor.GetTurns(sessionId, limit)
	if err != nil {
		return nil, err
	}

	if len(turns) == 0 {
		return []dataobject.SessionTurnView{}, nil
	}

	records := s.memoryService.LoadHistoryTurnRecords(session.ChatData, sessionId, loginName)
	views := make([]dataobject.SessionTurnView, 0, len(turns))
	for _, turn := range turns {
		view := dataobject.SessionTurnView{
			TurnId:          turn.TurnId,
			Seq:             turn.Seq,
			ChatTime:        turn.ChatTime,
			RespTime:        turn.RespTime,
			Status:          turn.Status,
			InterruptReason: turn.InterruptReason,
		}

		// 问题记录:chat_index -> user 记录正文
		if record, ok := records[turn.ChatIndex]; ok && record.RecordType == ctxmgr.HistoryRecordTypeUser {
			view.Question = record.Content
		}

		// 首轮提问在落盘前就被中断时 chat_index=0,正文永远读不到;
		// 用会话主题兜底(主题由首条消息生成),否则该轮在 UI 上只剩中断提示、看不到提问。
		if len(view.Question) == 0 && turn.Seq == 1 {
			view.Question = strings.TrimSpace(session.SessionName)
		}

		// 回复记录:resp_index = 0 表示未落盘(中断无产出/写失败)
		if turn.RespIndex > 0 {
			if record, ok := records[turn.RespIndex]; ok && record.RecordType == ctxmgr.HistoryRecordTypeAssistant {
				view.Answer = record.Content
				view.AnswerInterrupted = record.IsInterrupted()
			}
		}

		views = append(views, view)
	}

	return views, nil
}

func (s *ChattingService) Send(ctx context.Context, cmd chatCommon.Command) (chatCommon.Reply, error) {
	return s.chat(ctx, cmd, common.NopSink{})
}

func (s *ChattingService) Stream(ctx context.Context, cmd chatCommon.Command, sink common.ISink) (out chatCommon.Reply, err error) {
	if sink == nil {
		sink = common.NopSink{}
	}

	return s.chat(ctx, cmd, sink)
}

func (s *ChattingService) Interrupt(sender, turnId string) error {
	if !s.turnRegistrator.Cancel(sender, turnId) {
		return myException.ChatNotActiveKind.Newf("chat stream not active: %s", turnId)
	}

	return nil
}

// ============================= private functions ===================================

// 编排模版
func (s *ChattingService) chat(ctx context.Context, cmd chatCommon.Command, sink common.ISink) (out chatCommon.Reply, err error) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	tmpCtx, tTiming := s.turnReporter.Begin(ctx, cmd)
	var firstVisibleMs int64
	defer func() { s.turnReporter.End(tmpCtx, tTiming, firstVisibleMs, out, err, start) }()

	// 1. 准备(路由/provider/技能/prompt/工具预算)
	turn, err := s.preparer.Prepare(tmpCtx, cmd)
	if err != nil {
		return chatCommon.Reply{}, err
	}

	if pt := turn.Timing; pt != nil {
		tTiming.RouteMs, tTiming.SkillMs, tTiming.PromptMs = pt.RouteMs, pt.SkillMs, pt.PromptMs
	}
	tTiming.AgentId = turn.Agent.AgentId // 路由后的权威值,覆盖 0 步的显式值
	turn.Timing = tTiming

	// 四道闸门:技能激活 -> mode -> 协商 -> 校验; 任一不通过都落回下面的单轮路径
	switch result := s.isPlanMode(tmpCtx, turn, sink); result.Decision {
	case planModeDecisionRun:
		// 执行plan mode模式
		return s.runPlanMode(tmpCtx, turn, result.Plan, result.Spec, sink, start, &firstVisibleMs)
	case planModeDecisionHandled:
		// 协商已经给出结论(追问 / 参数不可用):直接作为本轮答复,不再多跑一轮 LLM
		return s.finishNegotiation(tmpCtx, turn, sink, result.Answer, start, &firstVisibleMs)
	default:
		// 执行一轮对话,即简单聊天
		return s.runSimpleMode(tmpCtx, turn, sink, start, &firstVisibleMs)
	}
}

// 根据用户的问题判断是否执行plan mode模式:技能激活 -> mode -> 协商 -> spec校验
func (s *ChattingService) isPlanMode(ctx context.Context, turn *chatCommon.TurnState, sink common.ISink) planModeResult {
	if turn == nil || s.chattingConfig.Chat.Mode == common.PlanModeOff {
		return planModeResult{Decision: planModeDecisionSkip}
	}

	// 1. 获取当前活跃且有sop的skill document
	var activeSkillId string
	var activeSkillDoc hCommon.SkillDocument
	for _, skillId := range turn.ActiveSkills {
		for _, doc := range turn.SkillDocuments {
			if doc.SkillId != skillId {
				continue
			}

			if len(doc.FrontMatter.SOPOutLines) > 0 {
				activeSkillId = skillId
				activeSkillDoc = doc
			}
		}
	}

	if activeSkillId == pCommon.BlankString {
		return planModeResult{Decision: planModeDecisionSkip}
	}

	// 闸门 3:协商 + spec校验(force 分支、重试、回灌机制在 plan_mode内)
	outcome := s.planModeEngine.NegotiateArgument(ctx, plan_mode.NegotiationInput{
		SessionID: turn.Cmd.SessionId,
		TurnID:    turn.Cmd.TurnID,
		Message:   turn.Cmd.Message,
		Provider:  turn.Provider.Llm,
		WorkRoot:  turn.WorkRoot,
		SkillID:   activeSkillId,
		SkillDoc:  activeSkillDoc,
		NegSpec:   biz.GetNegotiationSpec(activeSkillId, activeSkillDoc.FrontMatter, s.chattingConfig),
		Sink:      sink,
		Rules:     s.toolConfiguredRules,
	})

	if outcome.End {
		return planModeResult{Decision: planModeDecisionHandled, Answer: outcome.Answer}
	}

	if !outcome.Ready {
		return planModeResult{Decision: planModeDecisionSkip}
	}

	// 闸门 4:计划展开 + 预览(展开失败降级单轮)
	plan, ok := s.planModeEngine.PreviewPlan(turn.Cmd.TurnID, activeSkillId, activeSkillDoc, outcome.Spec, sink)
	if !ok {
		return planModeResult{Decision: planModeDecisionSkip}
	}

	return planModeResult{Decision: planModeDecisionRun, Plan: plan, Spec: outcome.Spec}
}

func (s *ChattingService) finishNegotiation(ctx context.Context, turn *chatCommon.TurnState, sink common.ISink,
	answer string, start time.Time, firstVisibleMs *int64) (chatCommon.Reply, error) {
	_ = ctx

	if strings.TrimSpace(answer) == pCommon.BlankString {
		answer = "需要补充更多信息才能开始..."
	}

	sink.OnDelta(answer)
	*firstVisibleMs = time.Since(start).Milliseconds()

	return chatCommon.Reply{
		SessionID:    turn.Cmd.SessionId,
		TurnID:       turn.Cmd.TurnID,
		Content:      answer,
		FinishReason: "stop",
		At:           pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
	}, nil
}

// 协商已经给出结论时的收尾:把答复作为本轮正文发出,不再调用LLM
// 为何用OnDelta 而非 OnNotice:
//   - OnNotice 是 "收尾提示"(banner),不进入消息正文
//   - 追问需要作为一条正常回答出现在会话里,前端才会渲染成气泡
func (s *ChattingService) runSimpleMode(ctx context.Context, turn *chatCommon.TurnState, sink common.ISink, start time.Time, firstVisibleMs *int64) (chatCommon.Reply, error) {
	// 执行单元:并发控制 -> 中断登记 -> 超时 -> 建档 -> 装配 -> 生成 -> 收尾记账
	simpleChatBot := chat.NewTurnRunner(chat.TurnRunnerParams{
		Assembler:   chattingService.assembler,
		Journal:     chattingService.journal,
		Maintainer:  chattingService.memoryMaintainer,
		ConGate:     chattingService.agentConcurrent,
		Register:    chattingService.turnRegistrator,
		Transport:   chattingService.transport,
		SyncLooper:  chattingService.syncLooper,
		AsyncLooper: chattingService.asyncLooper,
		Rules:       chattingService.toolConfiguredRules,
	})

	res, err := simpleChatBot.Run(ctx, chatCommon.TurnRequest{
		Cmd:         turn.Cmd,
		Turn:        turn,
		Role:        chatCommon.TurnRoleRoot,
		Sink:        sink,
		AllowRecord: true,
		StartAt:     start,
	})

	*firstVisibleMs = res.FirstVisibleMs

	return res.TurnReply, err
}

// 执行计划
func (s *ChattingService) runPlanMode(ctx context.Context, turn *chatCommon.TurnState, plan plan_mode.Plan, arg chatCommon.Spec,
	sink common.ISink, start time.Time, firstVisibleMs *int64) (chatCommon.Reply, error) {
	if sink == nil {
		sink = common.NopSink{}
	}

	release, err := s.agentConcurrent.TryAcquire(ctx, turn.Agent.AgentId, turn.Agent.MaxConcurrents)
	if err != nil {
		return chatCommon.Reply{}, err
	}
	defer release()

	// 中断登记
	streamCtx := ctx
	if sink.IsStreamed() {
		var done func()
		streamCtx, done = s.turnRegistrator.Register(streamCtx, turn.Cmd)
		defer done()
	}

	timeout := time.Duration(pCommon.GetEnvIntDefault(hCommon.ChatRunTimeout, hCommon.AgentRunTimeout)) * time.Second
	runCtx, cancel := context.WithTimeout(streamCtx, timeout)
	defer cancel()

	// 执行
	outcome, runErr := s.planModeEngine.Run(runCtx, plan_mode.PlanModeEngineInput{
		AgentID:   turn.Agent.AgentId,
		TurnID:    turn.Cmd.TurnID,
		SessionID: turn.Cmd.SessionId,
		Plan:      plan,
		Spec:      arg,
		Sink:      sink,
		Tool:      chat.NewSOPTurnToolInvoker(turn, sink, s.toolConfiguredRules),
		StartAt:   start,
	})
	if runErr != nil {
		return chatCommon.Reply{}, runErr
	}

	// 收尾 + 结果映射
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return chatCommon.Reply{}, myException.ChatRunTimeoutKind.Newf("plan executor run after %d seconds", int(timeout.Seconds()))
	}

	if outcome.Answer != pCommon.BlankString {
		sink.OnDelta(outcome.Answer)
	}

	*firstVisibleMs = time.Since(start).Milliseconds()
	return chatCommon.Reply{
		SessionID:    turn.Cmd.SessionId,
		TurnID:       turn.Cmd.TurnID,
		Content:      outcome.Answer,
		FinishReason: "stop",
		At:           pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
	}, nil
}
