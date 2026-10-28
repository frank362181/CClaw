package chat

import (
	"context"
	"errors"
	"time"

	cCommon "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/harness/api/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/observatory"
	pCommon "my.favorite/platformutility/common"
)

// ChattingService 只做编排
type ChattingService struct {
	preparer  ITurnPreparer
	assembler IContextAssembler

	transport   ITransport
	syncLooper  *ToolLoop
	asyncLooper *ToolLoop

	journal          IMemoryJournal
	memoryMaintainer IMemoryMaintainer
	concurrencyGate  IConcurrencyGate
	turnRegistrator  ITurnRegistrator
	turnReporter     ITurnReporter
}

var chattingService = pCommon.GetSingleton(func() *ChattingService {
	return &ChattingService{}
}).Instance()

func (s *ChattingService) Send(ctx context.Context, cmd Command) (Reply, error) {
	return s.execute(ctx, cmd, NopSink{})
}

func (s *ChattingService) Stream(ctx context.Context, cmd Command, sink ISink) (out Reply, err error) {
	if sink == nil {
		sink = NopSink{}
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
func (s *ChattingService) execute(ctx context.Context, cmd Command, sink ISink) (out Reply, err error) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	tmpCtx, tTiming := s.turnReporter.Begin(ctx, cmd)
	var firstVisibleMs int64
	defer func() { s.turnReporter.End(tmpCtx, tTiming, firstVisibleMs, out, err, start) }()

	// 1. 准备(路由/provider/技能/prompt/工具预算)
	turn, err := s.preparer.Prepare(tmpCtx, cmd)
	if err != nil {
		return Reply{}, err
	}
	cmd = turn.Cmd
	turn.SetStreamed(sink.IsStreamed()) //唯一真相源:sink决定流式与否,写进TurnState
	turn.Timing = tTiming

	// 2. 每Agent 并发闸
	gateStart := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp
	release, err := s.concurrencyGate.Acquire(tmpCtx, turn.AgentRef)
	tTiming.ConcurrencyMs = pCommon.MilliDiff2(gateStart)
	if err != nil {
		return Reply{}, err
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
	sink.OnJournal(jErr)
	turn.Session = sess

	// 6. 上下文装配
	asm, err := s.assembler.Assemble(runCtx, turn, cmd.Message)
	if err != nil {
		return Reply{}, err
	}
	turn.Seed(asm.Messages)

	// 7.生成(有工具走 ToolLoop,无工具一次调用)
	capSink := newCapturingSink(sink)
	llmReply, genErr := s.invoke(runCtx, turn, capSink)

	// 8.收尾记账(成功/失败共用一条路径)
	if genErr != nil {
		partial := capSink.Streamed()
		if partial == pCommon.BlankString {
			partial = llmReply.Content // 同步路径:内容在llmReply里
		}

		seq := 0
		if sess.Recorded {
			seq = s.journal.FinishPartial(ctx, turn, partial, cCommon.ClassifyInterrupt(ctx, runCtx, streamCtx, genErr))
			s.memoryMaintainer.Maintain(sess, turn, asm, seq) //中断轮同样进行维护
		}
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return Reply{}, myException.ChatRunTimeoutKind.Newf("chatting service: run timeout after %d seconds", int(timeout.Seconds()))
		}

		return Reply{}, myException.LlmError(genErr, "chatting service:runtime chat error")
	}

	// finish reason 收尾提示
	if code, msg := cCommon.FinishReasonNotice(llmReply.FinishReason); code != pCommon.BlankString {
		if llmReply.Content == pCommon.BlankString && llmReply.FinishReason == llmproto.FinishReasonContentFilter {
			return Reply{}, myException.ChatLLMContentFilterKind.New("chatting service: output blocked by content filter")
		}
		sink.OnNotice(code, msg)
	}

	seq := 0
	if sess.Recordable() {
		seq = s.journal.Finish(ctx, turn, llmReply.Content)
		s.memoryMaintainer.Maintain(sess, turn, asm, seq)
	}

	return Reply{
		SessionID: cmd.SessionId, TurnID: cmd.TurnID,
		Content: llmReply.Content, FinishReason: llmReply.FinishReason, At: pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp,
	}, nil
}

func (s *ChattingService) invoke(ctx context.Context, turnState *TurnState, sink ISink) (LlmReply, error) {
	// 无工具调用
	if len(turnState.VisibleTools) == 0 {
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
