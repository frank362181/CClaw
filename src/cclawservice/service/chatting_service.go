package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"my.favorite/cclawservice/biz"
	"my.favorite/cclawservice/biz/npa"
	"my.favorite/cclawservice/chat"
	"my.favorite/cclawservice/chat/sop"
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

// ChattingService 只做编排
type ChattingService struct {
	baseService.BaseService

	chattingConfig      config.ChatConfig
	sessionAccessor     dataaccessor.ISessionAccessor
	extractors          *sop.Extractors
	barriers            *sop.BarrierRepository
	orchestratorStorage *core2.OrchestratorStorage

	memoryService *ctxmgr.MemoryService

	preparer  chat.ITurnPreparer
	assembler chat.IContextAssembler

	transport   chat.ITransportClient
	syncLooper  *chat.ToolLoop
	asyncLooper *chat.ToolLoop

	journal          chat.IMemoryJournal
	memoryMaintainer chat.IMemoryMaintainer
	agentConcurrent  concurrency.IConcurrencyController // 按照Agent维度的并发控制器
	turnRegistrator  chat.ITurnRegistrator
	turnReporter     chat.ITurnReporter

	toolConfiguredRules toolexecutor.ToolConfigRules
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

	chattingService.orchestratorStorage = core2.GetOrchestratorStorage(orchestratorAccessor)

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
	chattingService.extractors = sop.NewExtractors()
	chattingService.barriers = sop.NewBarrierRepository()
	biz.RegistryNPABuiltin(chattingService.extractors, chattingService.barriers)

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
	if sender := strings.TrimSpace(session.Sender); len(sender) > 0 && sender != loginName {
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

func (s *ChattingService) Send(ctx context.Context, cmd chat.Command) (chat.Reply, error) {
	return s.chat(ctx, cmd, common.NopSink{})
}

func (s *ChattingService) Stream(ctx context.Context, cmd chat.Command, sink common.ISink) (out chat.Reply, err error) {
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
func (s *ChattingService) chat(ctx context.Context, cmd chat.Command, sink common.ISink) (out chat.Reply, err error) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	tmpCtx, tTiming := s.turnReporter.Begin(ctx, cmd)
	var firstVisibleMs int64
	defer func() { s.turnReporter.End(tmpCtx, tTiming, firstVisibleMs, out, err, start) }()

	// 1. 准备(路由/provider/技能/prompt/工具预算)
	turn, err := s.preparer.Prepare(tmpCtx, cmd)
	if err != nil {
		return chat.Reply{}, err
	}

	if pt := turn.Timing; pt != nil {
		tTiming.RouteMs, tTiming.SkillMs, tTiming.PromptMs = pt.RouteMs, pt.SkillMs, pt.PromptMs
	}
	tTiming.AgentId = turn.Agent.AgentId // 路由后的权威值,覆盖 0 步的显式值
	turn.Timing = tTiming

	// 四道闸门:技能激活 -> mode -> 协商 -> 校验; 任一不通过都落回下面的单轮路径
	if plan, arg, ok := s.isPlanMode(tmpCtx, turn, sink); ok {
		// 执行plan mode模式
		return s.runPlanMode(tmpCtx, turn, plan, arg, sink, start, &firstVisibleMs)
	}

	// 执行一轮对话,即简单聊天
	return s.runSimpleMode(tmpCtx, turn, sink, start, &firstVisibleMs)
}

// 根据用户的问题判断是否执行plan mode模式:技能激活 -> mode -> 协商 -> spec校验
func (s *ChattingService) isPlanMode(ctx context.Context, turn *chat.TurnState, sink common.ISink) (sop.Plan, npa.ReportSpec, bool) {
	if turn == nil || s.chattingConfig.Chat.Mode == common.PlanModeOff {
		return sop.Plan{}, npa.ReportSpec{}, false
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
		return sop.Plan{}, npa.ReportSpec{}, false
	}

	// 2. plan mode 协商，得到协商的结果,即what
	arg, ok := s.negotiateArgument(ctx, turn, activeSkillId, activeSkillDoc, sink)
	if !ok {
		return sop.Plan{}, npa.ReportSpec{}, false
	}

	arg.ArchiveNo = s.resolveArchiveNo(ctx, turn.Cmd.TurnID, arg.ProjectDir, arg.ArchiveNo)

	// 根据协商的结果形成 Plan,即 HOW
	planArguments := sop.DefaultPlanArguments()
	planArguments.KnownTools = hCommon.ConvertBasicTools2Map(activeSkillDoc.FrontMatter)
	plan, err := sop.BuildPlanFromSkillSOPOutline(activeSkillId, activeSkillDoc.FrontMatter.SOPOutLines, arg, planArguments)
	if err != nil {
		logging.LogWarnF("plan mode:build plan failed,skill=%s,turn=%s,error=%s", activeSkillId, turn.Cmd.TurnID, err.Error())
		sink.OnNotice(common.ChatNoticeIncomplete, "流程定义有误,已改为直接回答:"+err.Error())
		return sop.Plan{}, npa.ReportSpec{}, false
	}

	// plan mode 第4点：计划必须对人可见(),用户才能判断“该不该跑”
	sink.OnStatus("plan.preview", fmt.Sprintf(
		"计划就绪:目录=%s,档案号=%s, %d 个步骤 / %d 个波次 / 预计 %d tokens", arg.ProjectDir, common.FallbackText(arg.ArchiveNo, "(空)"), plan.TaskCount, plan.WaveCount, plan.EstTokens))

	return plan, arg, true
}

func (s *ChattingService) resolveArchiveNo(ctx context.Context, turnID, projectDir, archiveNo string) string {
	if no := strings.TrimSpace(archiveNo); no != "" {
		return no
	}

	if s.orchestratorStorage != nil && strings.TrimSpace(turnID) != pCommon.BlankString {
		resumer, err := s.orchestratorStorage.Load(ctx, turnID)
		if err != nil {
			logging.LogWarnF("plan mode:load resumed spec failed,turn=%s,err=%s", turnID, err.Error())
		} else if resumer != nil {
			if no := strings.TrimSpace(resumer.Spec.ArchiveNo); no != pCommon.BlankString {
				return no
			}
		}
	}

	return npa.GenerateDocumentNo(projectDir, "NPA")
}

// 产出并校验ReportSpec
func (s *ChattingService) negotiateArgument(ctx context.Context, turn *chat.TurnState, skillId string, skillDoc hCommon.SkillDocument, sink common.ISink) (npa.ReportSpec, bool) {
	if s.chattingConfig.Chat.Mode == common.PlanModeForce {
		return s.forceArgument(ctx, turn, skillId, skillDoc, sink)
	}

	sink.OnStatus("plan.negotiating", "正在确认需求...")
	retry := s.chattingConfig.Negotiation.ArgRetry
	if retry < 0 {
		retry = 0
	}
	feedback := pCommon.BlankString

	intentionNegotiator := chat.NewIntentionNegotiator(s.transport, s.chattingConfig)
	for attempt := 0; attempt <= retry; attempt++ {
		result, err := intentionNegotiator.Negotiate(ctx, chat.NegotiationRequest{
			Provider:  turn.Provider,
			Question:  turn.Cmd.Message,
			SkillId:   skillId,
			SkillName: skillDoc.FrontMatter.Name,
			SkillDesc: skillDoc.FrontMatter.Description,
			SOPSteps:  chat.GetSOPStepSummary(skillDoc.FrontMatter.SOPOutLines),
			WorkRoot:  turn.WorkRoot,
			Rules:     s.toolConfiguredRules,
			NegSpec:   npa.NewReportSpecNegotiation(skillDoc.FrontMatter),
			Feedback:  feedback,
		})

		if err != nil {
			result = chat.NegotiationResult{Decision: chat.DecisionFailed, Reason: err.Error()}
		}

		switch result.Decision {
		case chat.DecisionProceed:
			decoded, ok := result.Arg.(npa.ReportSpec)
			if !ok {
				// proceed 却没有可用参数:按协商失败处理,不要带着空 spec 去建计划
				logging.LogWarnF("plan mode:proceed without spec,turn=%s,attempt=%d,type=%T",
					turn.Cmd.TurnID, attempt+1, result.Arg)
				feedback = "已判定为需要执行,但缺少必要参数;请用 " + chat.IntentionToolStart + " 重新提交。"
				continue
			}
			candidate := npa.NormalizeReportSpec(decoded, skillDoc.FrontMatter)
			issues := npa.ValidateReportSpec(candidate, skillDoc.FrontMatter)
			if len(issues) == 0 {
				s.reportDecision(ctx, turn, skillId, string(chat.DecisionProceed), result)
				return candidate, true
			}

			feedback = npa.FormatSpecIssues(issues)
			logging.LogWarnF("plan mode:spec rejected,turn=%s,attempt=%d,issues=%d", turn.Cmd.TurnID, attempt+1, len(issues))
			continue
		case chat.DecisionNeedMoreInfo:
			s.reportDecision(ctx, turn, skillId, string(chat.DecisionNeedMoreInfo), result)
			sink.OnNotice(common.ChatNoticeIncomplete, chat.NeedMoreInfoText(result.Missing))

			return npa.ReportSpec{}, false
		case chat.DecisionNotApplicable:
			s.reportDecision(ctx, turn, skillId, string(chat.DecisionNotApplicable), result)
			logging.LogInfoF("plan mode:not applicable,turn=%s,reason=%s",
				turn.Cmd.TurnID, result.Reason)
			return npa.ReportSpec{}, false
		default:
			s.reportDecision(ctx, turn, skillId, string(chat.DecisionFailed), result)
			logging.LogInfoF("plan mode:negotiate failed,turn=%s,reason=%s",
				turn.Cmd.TurnID, result.Reason)
			return npa.ReportSpec{}, false
		}
	}

	// 重试用尽:明确告诉用户缺了什么,而不是静默退回
	s.reportDecision(ctx, turn, skillId, string(chat.DecisionValidateFailed), chat.NegotiationResult{Decision: chat.DecisionValidateFailed, Reason: feedback})
	sink.OnNotice(common.ChatNoticeIncomplete, "材料信息不完善,改为直接回答。\n"+feedback)
	return npa.ReportSpec{}, false
}

func (s *ChattingService) forceArgument(ctx context.Context, turn *chat.TurnState, skillId string, skillDoc hCommon.SkillDocument, sink common.ISink) (npa.ReportSpec, bool) {
	dftProjectDir := s.chattingConfig.Negotiation.ProjectDir
	candidate := npa.NormalizeReportSpec(npa.ReportSpec{
		ProjectDir: dftProjectDir,
		ArchiveNo:  "",
		ReportKind: npa.DefaultReportKind,
		OCR:        npa.DefaultOCRMode,
	}, skillDoc.FrontMatter)

	if issues := npa.ValidateReportSpec(candidate, skillDoc.FrontMatter); len(issues) > 0 {
		s.reportDecision(ctx, turn, skillId, string(chat.DecisionValidateFailed), chat.NegotiationResult{Reason: npa.FormatSpecIssues(issues)})
		// force语义:不静默回退,明确报错
		sink.OnNotice(common.ChatNoticeError, "force模式参数不可用(请检查[negotiation.projectDir]配置项:\n)"+
			npa.FormatSpecIssuesForUser(issues))

		return npa.ReportSpec{}, false
	}

	s.reportDecision(ctx, turn, skillId, string(chat.DecisionProceed), chat.NegotiationResult{Decision: "force mode"})
	return candidate, true
}

// 记录决策日志,不影响主流程
func (s *ChattingService) reportDecision(ctx context.Context, turn *chat.TurnState, skillId, decision string, result chat.NegotiationResult) {
	if turn == nil || s.orchestratorStorage == nil {
		return
	}

	s.orchestratorStorage.SaveDecision(ctx, dataobject.TIntentionDecision{
		TurnId:    turn.Cmd.TurnID,
		SkillId:   skillId,
		Decision:  decision,
		Reason:    pCommon.TruncateRunes(result.Reason, 480, common.TruncatedMarker),
		Missing:   pCommon.TruncateRunes(strings.Join(result.Missing, ","), 480, common.TruncatedMarker),
		Rounds:    result.Rounds,
		ElapsedMs: result.ElapsedMs,
	})
}

// 读回落库的状态:指纹不一致则表示作废(失败即从头跑,不阻断)
func (s *ChattingService) loadResumedState(ctx context.Context, turnID string, plan sop.Plan) *sop.PlanState {
	if s.orchestratorStorage == nil {
		return nil
	}

	loaded, err := s.orchestratorStorage.Load(ctx, turnID)
	if err != nil {
		logging.LogWarnF("plan mode:load resume state failed,turn=%s,error=%s", turnID, err.Error())
		return nil
	}

	if loaded == nil {
		return nil
	}

	if loaded.PlanDigest != sop.GetFingerprintOfPlan(plan) {
		logging.LogWarnF("plan mode: fingerprint changed, turn=%s, drop resume state", turnID)
		return nil
	}

	return loaded.State
}

func (s *ChattingService) runSimpleMode(ctx context.Context, turn *chat.TurnState, sink common.ISink, start time.Time, firstVisibleMs *int64) (chat.Reply, error) {
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

	res, err := simpleChatBot.Run(ctx, chat.TurnRequest{
		Cmd:         turn.Cmd,
		Turn:        turn,
		Role:        chat.TurnRoleRoot,
		Sink:        sink,
		AllowRecord: true,
		StartAt:     start,
	})

	*firstVisibleMs = res.FirstVisibleMs

	return res.TurnReply, err
}

// 执行计划
func (s *ChattingService) runPlanMode(ctx context.Context, turn *chat.TurnState, plan sop.Plan, arg npa.ReportSpec,
	sink common.ISink, start time.Time, firstVisibleMs *int64) (chat.Reply, error) {
	if sink == nil {
		sink = common.NopSink{}
	}

	release, err := s.agentConcurrent.TryAcquire(ctx, turn.Agent.AgentId, turn.Agent.MaxConcurrents)
	if err != nil {
		return chat.Reply{}, err
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

	// 获得暂停掉的State
	resumedState := s.loadResumedState(runCtx, turn.Cmd.TurnID, plan)

	sopChatBot := sop.NewPlanChatBotRunner(sop.PlanChatBotParams{
		ConGate:    chattingService.agentConcurrent,
		Sink:       sink,
		Extractors: s.extractors,
		Barriers:   s.barriers,
		PlanStorge: s.orchestratorStorage,
	})
	result, runErr := sopChatBot.Run(runCtx, sop.PlanRequest{
		AgentId:      turn.Agent.AgentId,
		TurnID:       turn.Cmd.TurnID,
		SessionID:    turn.Cmd.SessionId,
		PlanMem:      plan,
		Spec:         arg,
		Sink:         chat.NewSinkIntention(sink, len(plan.Tasks)),
		Tool:         chat.NewSOPTurnToolInvoker(turn, sink, s.toolConfiguredRules),
		ResumedState: resumedState,
		StartAt:      start,
	})

	if runErr != nil {
		return chat.Reply{}, runErr
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return chat.Reply{}, myException.ChatRunTimeoutKind.Newf("plan executor run after %d seconds", int(timeout.Seconds()))
	}

	answer := sop.GetSOPChatResultSummary(result)
	if answer != pCommon.BlankString {
		sink.OnDelta(answer)
	}

	*firstVisibleMs = time.Since(start).Milliseconds()
	return chat.Reply{
		SessionID:    turn.Cmd.SessionId,
		TurnID:       turn.Cmd.TurnID,
		Content:      answer,
		FinishReason: "stop",
		At:           pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
	}, nil
}
