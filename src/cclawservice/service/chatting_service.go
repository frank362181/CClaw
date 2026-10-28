package service

import (
	"context"
	"errors"
	"time"

	"my.favorite/cclawservice/chat"
	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	myException "my.favorite/cclawservice/exception"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/observatory"
	"my.favorite/cclawservice/model/repo"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/concurrency"
	"my.favorite/platformutility/database/core"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// ChattingService 只做编排
type ChattingService struct {
	baseService.BaseService

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
}

var chattingService = pCommon.GetSingleton(func() *ChattingService {
	return &ChattingService{
		turnRegistrator: chat.GetChatTurnRegistrator(), //流失聊天的轮次注册器
	}
}).Instance()

func GetChattingService(db core.IBaseDatabase, agentRepo *repo.AgentRepository, functionCalling bool, sessionRecord bool) *ChattingService {
	if db == nil || agentRepo == nil {
		panic("service.GetChattingService: db / agentRepo must not be nil")
	}

	// 1. 传输层
	transport := chat.NewTransportClient(hCommon.NonStreamResponseTimeout)

	// 2. 装配器
	memoryService := ctxmgr.GetMemoryService(db, sessionRecord)
	assembler := chat.GetAssembler(memoryService)

	chattingService.preparer = chat.GetChatPreparer(agentRepo, functionCalling, transport.SimpleComplete)
	chattingService.agentConcurrent = concurrency.GetConcurrentAdvance()
	chattingService.assembler = assembler
	chattingService.transport = transport
	chattingService.syncLooper = chat.NewToolLoop(transport, assembler, false) // 同步执行
	chattingService.asyncLooper = chat.NewToolLoop(transport, assembler, true) // 异步执行
	chattingService.memoryMaintainer = chat.GetMemoryMaintainer(memoryService, transport)
	chattingService.journal = chat.GetMemoryJournal(memoryService)
	chattingService.turnRegistrator = chat.GetChatTurnRegistrator()
	chattingService.turnReporter = chat.GetTurnReporter()

	return chattingService
}

func (s *ChattingService) Send(ctx context.Context, cmd chat.Command) (chat.Reply, error) {
	return s.execute(ctx, cmd, chat.NopSink{})
}

func (s *ChattingService) Stream(ctx context.Context, cmd chat.Command, sink chat.ISink) (out chat.Reply, err error) {
	if sink == nil {
		sink = chat.NopSink{}
	}

	return s.execute(ctx, cmd, sink)
}

func (s *ChattingService) Interrupt(sender, turnId string) error {
	if !s.turnRegistrator.Cancel(sender, turnId) {
		return myException.ChatNotActiveKind.Newf("chat stream not active: %s", turnId)
	}

	return nil
}

// ============================= private functions ===================================

// 编排模版
func (s *ChattingService) execute(ctx context.Context, cmd chat.Command, sink chat.ISink) (out chat.Reply, err error) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	tmpCtx, tTiming := s.turnReporter.Begin(ctx, cmd)
	var firstVisibleMs int64
	defer func() { s.turnReporter.End(tmpCtx, tTiming, firstVisibleMs, out, err, start) }()

	// 1. 准备(路由/provider/技能/prompt/工具预算)
	turn, err := s.preparer.Prepare(tmpCtx, cmd)
	if err != nil {
		return chat.Reply{}, err
	}
	cmd = turn.Cmd
	turn.SetStreamed(sink.IsStreamed()) //唯一真相源:sink决定流式与否,写进TurnState
	if pt := turn.Timing; pt != nil {
		tTiming.RouteMs, tTiming.SkillMs, tTiming.PromptMs = pt.RouteMs, pt.SkillMs, pt.PromptMs
	}
	tTiming.AgentId = turn.Agent.AgentId // 路由后的权威值,覆盖 0 步的显式值
	turn.Timing = tTiming

	// 2. 每Agent 并发闸
	gateStart := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp
	release, err := s.agentConcurrent.TryAcquire(tmpCtx, turn.Agent.AgentId, turn.Agent.MaxConcurrents)
	tTiming.ConcurrencyMs = pCommon.MilliDiff2(gateStart)
	if err != nil {
		return chat.Reply{}, err
	}
	defer release()

	// 3. 流式才需要"可被中断"的运行上下文
	streamCtx := tmpCtx
	if sink.IsStreamed() {
		var done func()
		streamCtx, done = s.turnRegistrator.Register(tmpCtx, cmd)
		defer done()
	}

	// 4. 应用级运行超时
	timeout := time.Duration(pCommon.GetEnvIntDefault(hCommon.ChatRunTimeout, hCommon.AgentRunTimeout)) * time.Second
	runCtx, runCancel := context.WithTimeout(streamCtx, timeout)
	defer runCancel()
	runCtx = observatory.WithTurn(runCtx, cmd.TurnID, start)

	// 5.会话建档(旁路)
	sess, jErr := s.journal.Begin(runCtx, turn)
	if errors.Is(jErr, ctxmgr.ErrSessionOwnershipConflict) {
		sink.OnNotice(cCommon.ChatNoticeNotRecorded, cCommon.ConstChatNoticeNotRecordedMsg)
	}
	sink.OnJournal(jErr)
	turn.Session = sess

	// 6. 上下文装配
	asm, err := s.assembler.Assemble(runCtx, turn, cmd.Message)
	if err != nil {
		return chat.Reply{}, err
	}
	turn.Seed(asm.Messages)

	// 7.生成(有工具走 ToolLoop,无工具一次调用)
	capSink := chat.NewCapturingSink(sink)
	llmReply, genErr := s.invoke(runCtx, turn, capSink)
	firstVisibleMs = capSink.FirstVisibleMs(start) //在defer执行前赋值

	// 8.收尾记账(成功/失败共用一条路径)
	if genErr != nil {
		partial := capSink.Streamed()
		if partial == pCommon.BlankString {
			partial = llmReply.Content // 同步路径:内容在llmReply里
		}

		seq := 0
		if sess.Recordable() {
			seq = s.journal.FinishPartial(ctx, turn, partial, cCommon.ClassifyInterrupt(ctx, runCtx, streamCtx, genErr))
			s.memoryMaintainer.Maintain(sess, turn, asm, seq) //中断轮同样进行维护
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return chat.Reply{}, myException.ChatRunTimeoutKind.Newf("chatting service: run timeout after %d seconds", int(timeout.Seconds()))
		}

		return chat.Reply{}, myException.LlmError(genErr, "chatting service:runtime chat error")
	}

	// finish reason 收尾提示
	if code, msg := cCommon.FinishReasonNotice(llmReply.FinishReason); code != pCommon.BlankString {
		if llmReply.Content == pCommon.BlankString && llmReply.FinishReason == llmproto.FinishReasonContentFilter {
			return chat.Reply{}, myException.ChatLLMContentFilterKind.New("chatting service: output blocked by content filter")
		}
		sink.OnNotice(code, msg)
	}

	seq := 0
	if sess.Recordable() {
		seq = s.journal.Finish(ctx, turn, llmReply.Content)
		s.memoryMaintainer.Maintain(sess, turn, asm, seq)
	}

	return chat.Reply{
		SessionID: cmd.SessionId, TurnID: cmd.TurnID,
		Content: llmReply.Content, FinishReason: llmReply.FinishReason, At: pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
	}, nil
}

func (s *ChattingService) invoke(ctx context.Context, turnState *chat.TurnState, sink chat.ISink) (llmproto.LlmReply, error) {
	if len(turnState.VisibleTools) == 0 {
		// 无工具调用
		req := turnState.Request(nil, pCommon.BlankString)
		req.Messages = s.assembler.Fit(req.Messages, turnState.Policy)

		if turnState.IsStreamed() {
			return s.transport.Stream(ctx, turnState.Provider, req, sink)
		}

		return s.transport.Complete(ctx, turnState.Provider, req)
	}

	// 有工具调用
	if turnState.IsStreamed() {
		return s.asyncLooper.Run(ctx, turnState, sink)
	}

	return s.syncLooper.Run(ctx, turnState, sink)
}
