/*
* 工具调用循环类,设计策略: template method + strategy
**/

package chat

import (
	"context"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	myException "my.favorite/cclawservice/exception"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/security"
	"my.favorite/cclawservice/harness/toolexecutor"
	pCommon "my.favorite/platformutility/common"
)

// ToolLoop 一轮对话的工具编排
// 骨架固定: 调模型 -> 判守卫 -> 回填assistant -> 执行工具 -> 回填tool
//
//	-> 下一轮
//
// 变体全部外置依赖:LlmGateway(同步/异步)、ToolExecutor(串行/并行)、Sink(空/SSE)
type ToolLoop struct {
	transport ITransportClient

	toolExecutor IToolExecutor
	assembler    IContextAssembler //上下文组装器

	loopGuardFn func() ILoopGuard

	maxRounds          int
	maxToolResultRunes int //工具调用结果回填给模型的最大runes上限
}

func NewToolLoop(transport ITransportClient, asm IContextAssembler, streaming bool) *ToolLoop {
	var exec IToolExecutor
	if streaming {
		//并行
		exec = NewToolExecutorParallel(toolexecutor.NewToolExecutor())
	} else {
		//串行
		exec = NewToolExecutorSerial(toolexecutor.NewToolExecutor())
	}

	return &ToolLoop{
		transport:          transport,
		toolExecutor:       exec,
		assembler:          asm,
		loopGuardFn:        func() ILoopGuard { return security.NewTurnLoopGuard() },
		maxRounds:          hCommon.AgentToolLoopMax,
		maxToolResultRunes: ctxmgr.TurnToolResultMaxRunes,
	}
}

// Run 执行本轮工具调用;模型不再请求工具时返回结果
func (l *ToolLoop) Run(ctx context.Context, st *TurnState, sink ISink) (llmproto.LlmReply, error) {
	if sink == nil {
		sink = NopSink{}
	}

	guard := l.loopGuardFn()
	seq := newSeqSink(sink) // 序号分配

	for round := 0; round < l.maxRounds; round++ {
		roundTools, choice := st.Plan.RoundTools(st.VisibleTools, round)

		seq.beginRound() //只清本轮映射,next 不清
		reply, err := l.invoke(ctx, st, roundTools, choice, seq)
		if err != nil {
			// 返回partial:上层要把它写入历史(否则"问题在、回答不在")
			return reply, err
		}

		st.Plan.Verify(reply.ToolCalls, choice)

		if len(reply.ToolCalls) == 0 {
			return reply, nil // 唯一正常结束
		}

		if guard.ShouldStop() {
			// 已经提示过且再次命中
			return reply, guard.StopError(st.Scoped())
		}

		st.AppendAssistant(reply)
		toolInvocations := bindToolInvocation(reply.ToolCalls, seq)
		outcomes := l.toolExecutor.RunAll(ctx, st.Env(), toolInvocations, sink, st.Ledger)

		// 计时汇总,单协程完成
		if st.Timing != nil {
			for _, oc := range outcomes {
				st.Timing.ToolCalls++
				st.Timing.ToolsMs += oc.ElapseMs
			}
		}

		// 中断:不再进入下一轮(已执行的仍要进历史台账)
		if err := ctx.Err(); err != nil {
			st.RecordOutcomes(outcomes)                             // 已执行的仍要进历史台账
			return llmproto.LlmReply{}, llmproto.ErrRequestCanceled // 中断加速:不再进入到下一轮
		}

		// 未取消时必须 1:1,否则下一轮assistant声明N个、只回填M个 -> 上游 400
		if len(outcomes) != len(reply.ToolCalls) {
			st.RecordOutcomes(outcomes)
			return llmproto.LlmReply{}, myException.ToolExecutionMisMatchedKind.Newf("outcomes unmatched: want %d,got %d", len(reply.ToolCalls), len(outcomes))
		}

		for _, oc := range outcomes {
			if oc.Failed() {
				// done/error先发,让UI先看到工具执行结果
				sink.OnTool(ToolEvent{Phase: cCommon.ChatToolEventError, Seq: oc.Seq, Name: oc.Name, Detail: oc.Reason.Error()})
			} else {
				sink.OnTool(ToolEvent{Phase: cCommon.ChatToolEventDone, Seq: oc.Seq, Name: oc.Name})
			}

			// 回填给模型的文本:截断的副本 + 守卫提示(可能)
			body := guard.ApplyLoopGuard(oc.Name, oc.Arg, !oc.Failed(), oc.ForModel(l.maxToolResultRunes), sink.OnNotice)
			st.AppendToolResult(oc.CallID, body)
		}

		st.RecordOutcomes(outcomes)
	}

	return llmproto.LlmReply{}, myException.ToolLoopExceededKind.Newf("tool loop exceeded:%d rounds", l.maxRounds)
}

// ================================= private  functions & methods ===============================
func (l *ToolLoop) invoke(ctx context.Context, st *TurnState, tools []llmproto.Tool, choice string, ob IStreamObserver) (llmproto.LlmReply, error) {
	req := st.Request(tools, choice)

	// 预算贴合:工具结果不断回填会持续挤压窗口,长工具链必须随时裁掉最老的历史
	// 口径必须与装配时一致(含OutputReserve/ToolsReserve),所以走ContextAssembler.Fit
	req.Messages = l.assembler.Fit(req.Messages, st.Policy)

	start := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp
	defer func() {
		if st.Timing != nil {
			st.Timing.LlmMs += pCommon.MilliDiff2(start)
		}
	}()

	var (
		reply llmproto.LlmReply
		err   error
	)

	if req.Streamed {
		reply, err = l.transport.Stream(ctx, st.Provider, req, ob)
	} else {
		reply, err = l.transport.Complete(ctx, st.Provider, req)
	}

	if err != nil {
		return reply, err
	}

	if st.Timing != nil {
		st.Timing.Rounds++
	}

	return reply, nil
}
