package chat

import (
	"context"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
)

// 同步执行工具
type serialExecutor struct {
	core executorCore
}

func NewToolExecutorSerial(runner llmproto.IToolScriptRunner) IToolExecutor {
	if runner == nil {
		panic("function calling: serial executor requires a non-nil IToolScriptRunner")
	}

	return &serialExecutor{
		core: executorCore{
			runner: runner,
		},
	}
}

func (se *serialExecutor) RunAll(ctx context.Context, env ToolEnv, invocations []ToolInvocation, sink ISink, ledger IMemoryLedger) []ToolOutcome {
	out := make([]ToolOutcome, 0, len(invocations))
	for _, invocation := range invocations {
		// 中断加速:已取消就不再发起新的工具调用
		if ctx.Err() != nil {
			break
		}

		// 同步路径的sink,通知UI工具调用
		sink.OnTool(ToolEvent{
			Phase: cCommon.ChatToolEventRunning,
			Seq:   invocation.Seq,
			Name:  invocation.Name(),
		})

		out = append(out, se.core.executeOne(ctx, env, invocation, ledger, nil))
	}

	return out
}
