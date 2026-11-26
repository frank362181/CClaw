package toolexecutor

import (
	"context"

	cCommon "my.favorite/cclawservice/common"
)

// serialExecutor 同步执行一轮内的工具调用(非流式接口走这里)
type serialExecutor struct {
	core executorCore
}

func NewRoundExecutorSerial(runner IScriptRunner) IToolExecutedWrapper {
	if runner == nil {
		panic("function calling: serial executor requires a non-nil IToolScriptRunner")
	}

	return &serialExecutor{
		core: executorCore{
			runner: runner,
		},
	}
}

func (se *serialExecutor) RunAll(ctx context.Context, env ToolEnv, invocations []ToolInvocation,
	sink cCommon.IToolEventSink, ledger IToolLedger) []ToolOutcome {

	out := make([]ToolOutcome, 0, len(invocations))
	for _, invocation := range invocations {
		// 中断加速:已取消就不再发起新的工具调用
		if ctx.Err() != nil {
			break
		}

		// 同步路径的sink,通知UI工具调用
		sink.OnTool(cCommon.ToolEvent{
			Phase: cCommon.ChatToolEventRunning,
			Seq:   invocation.Seq,
			Name:  invocation.Name(),
		})

		out = append(out, se.core.executeOne(ctx, env, invocation, ledger, nil))
	}

	return out
}
