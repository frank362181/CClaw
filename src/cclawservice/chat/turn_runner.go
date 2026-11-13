package chat

import (
	"context"
	"errors"
	"time"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	selfException "my.favorite/cclawservice/exception"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/observatory"
	"my.favorite/cclawservice/harness/toolexecutor"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/concurrency"
)

// TurnRunnerParams 执行单元所需的参数依赖,全部有调用方注入
type TurnRunnerParams struct {
	Assembler  IContextAssembler
	Journal    IMemoryJournal
	Maintainer IMemoryMaintainer
	ConGate    concurrency.IConcurrencyController
	Register   ITurnRegistrator
	Transport  ITransportClient

	SyncLooper  IToolLooper
	AsyncLooper IToolLooper

	Rules toolexecutor.ToolConfigRules
}

type TurnRunner struct {
	assembler       IContextAssembler
	journal         IMemoryJournal
	maintainer      IMemoryMaintainer
	conGate         concurrency.IConcurrencyController
	turnRegistrator ITurnRegistrator
	transport       ITransportClient

	syncLooper  IToolLooper
	asyncLooper IToolLooper

	rules toolexecutor.ToolConfigRules
}

func NewTurnRunner(turnParams TurnRunnerParams) *TurnRunner {
	return &TurnRunner{
		assembler:       turnParams.Assembler,
		journal:         turnParams.Journal,
		maintainer:      turnParams.Maintainer,
		conGate:         turnParams.ConGate,
		turnRegistrator: turnParams.Register,
		transport:       turnParams.Transport,

		syncLooper:  turnParams.SyncLooper,
		asyncLooper: turnParams.AsyncLooper,

		rules: turnParams.Rules,
	}
}

var _ ITurnRunner = (*TurnRunner)(nil)

func (t *TurnRunner) Run(ctx context.Context, req TurnRequest) (TurnOutcome, error) {
	turn := req.Turn
	if turn == nil {
		return TurnOutcome{}, selfException.ChatParameterKind.Newf("turn runner:nil turn state")
	}

	sink := req.Sink
	if sink == nil {
		sink = NopSink{}
	}

	// 唯一真相源:sink决定流式与否,写进TurnState
	turn.SetStreamed(sink.IsStreamed())

	if req.DisableTool {
		// 评估者不需要工具:走无工具分支,可强制关闭(兼容旧版本调用)
		turn.VisibleTools = nil
		turn.TurnTool = nil
	}

	// 每agent并发控制
	gateStart := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp
	release, err := t.conGate.TryAcquire(ctx, turn.Agent.AgentId, turn.Agent.MaxConcurrents)
	if turn.Timing != nil {
		turn.Timing.ConcurrencyMs = pCommon.MilliDiff2(gateStart)
	}
	if err != nil {
		return TurnOutcome{}, err
	}
	defer release()

	// 3 流式才需要"可被中断"的运行上下文
	streamCtx := ctx
	if sink.IsStreamed() && t.turnRegistrator != nil {
		var done func()
		streamCtx, done = t.turnRegistrator.Register(ctx, turn.Cmd)
		defer done()
	}

	// 4. 应用级运行超时
	timeout := time.Duration(pCommon.GetEnvIntDefault(hCommon.ChatRunTimeout, hCommon.AgentRunTimeout)) * time.Second
	runCtx, runCancel := context.WithTimeout(streamCtx, timeout)
	defer runCancel()

	runCtx = observatory.WithTurn(runCtx, turn.Cmd.TurnID, req.StartAt)

	// 5.会话建档
	sess, jErr := t.journal.Begin(runCtx, turn)
	if errors.Is(jErr, selfException.SessionOwnershipConflictKind) {
		sink.OnNotice(cCommon.ChatNoticeNotRecorded, cCommon.ConstChatNoticeNotRecordedMsg)
	}
	sink.OnJournal(jErr)
	turn.Session = sess

	// 6 装配上下文
	asm, err := t.assembler.Assemble(runCtx, turn, turn.Cmd.Message)
	if err != nil {
		return TurnOutcome{}, err
	}
	turn.Seed(asm.Messages)

	// 7. 生成(有工具走ToolLoop,无工具则一次调用)
	capSink := NewCapturingSink(sink)
	llmReply, genErr := t.invoke(runCtx, turn, capSink)
	out := TurnOutcome{
		Llm:            llmReply,
		FirstVisibleMs: capSink.FirstVisibleMs(req.StartAt),
		ElapseMs:       time.Since(req.StartAt).Milliseconds(),
	}
	if turn.Timing != nil {
		out.Rounds = turn.Timing.Rounds
		out.ToolCalls = turn.Timing.ToolCalls
	}

	// 8.收尾记账
	if genErr != nil {
		partial := capSink.Streamed()
		if partial == pCommon.BlankString {
			partial = llmReply.Content // 同步路径
		}

		seq := 0
		if req.AllowRecord && sess.Recordable() {
			seq = t.journal.FinishPartial(ctx, turn, partial, cCommon.ClassifyInterrupt(ctx, runCtx, streamCtx, genErr))
			t.maintainer.Maintain(sess, turn, asm, seq) // 中断轮同样进行维护
		}

		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return out, selfException.ChatRunTimeoutKind.Newf(
				"turn runner:run timeout after %d seconds", int(timeout.Seconds()))
		}

		return out, selfException.LlmError(genErr, "turn runner:runtime chat error")
	}

	// finish reason收尾提示
	if code, msg := cCommon.FinishReasonNotice(llmReply.FinishReason); code != pCommon.BlankString {
		if llmReply.Content == pCommon.BlankString && llmReply.FinishReason == llmproto.FinishReasonContentFilter {
			return out, selfException.ChatLLMContentFilterKind.New("turn runner:output blocked by content filter")
		}

		sink.OnNotice(code, msg)
	}

	seq := 0
	if req.AllowRecord && sess.Recordable() {
		seq = t.journal.Finish(ctx, turn, llmReply.Content)
		t.maintainer.Maintain(sess, turn, asm, seq)
	}

	out.TurnReply = Reply{
		SessionID:    turn.Cmd.SessionId,
		TurnID:       turn.Cmd.TurnID,
		Content:      llmReply.Content,
		FinishReason: llmReply.FinishReason,
		At:           pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
	}

	return out, nil
}

func (t *TurnRunner) invoke(ctx context.Context, turn *TurnState, sink ISink) (llmproto.LlmReply, error) {
	if len(turn.VisibleTools) == 0 {
		// 无工具调用
		req := turn.Request(nil, pCommon.BlankString)
		req.Messages = t.assembler.Fit(req.Messages, turn.Policy)

		if turn.IsStreamed() {
			return t.transport.Stream(ctx, turn.Provider, req, sink)
		}

		return t.transport.Complete(ctx, turn.Provider, req)
	}

	// 有工具调用
	if turn.IsStreamed() {
		return t.asyncLooper.Run(ctx, turn, sink, t.rules)
	}

	return t.syncLooper.Run(ctx, turn, sink, t.rules)
}
