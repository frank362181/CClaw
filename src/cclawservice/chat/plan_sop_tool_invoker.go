/**
* sop 事件调用
**/

package chat

import (
	"context"
	"fmt"
	"sync/atomic"

	"my.favorite/cclawservice/chat/sop"
	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/harness/toolexecutor"
	pCommon "my.favorite/platformutility/common"
)

type SopTurnToolInvoker struct {
	root         *TurnState
	sink         ISink
	rules        toolexecutor.ToolConfigRules
	toolExecutor toolexecutor.IToolRoundExecutor
	seq          atomic.Int64
}

var _ sop.ISopToolInvoker = (*SopTurnToolInvoker)(nil)

func NewSOPTurnToolInvoker(root *TurnState, sink ISink, rules toolexecutor.ToolConfigRules) *SopTurnToolInvoker {
	if sink == nil {
		sink = NopSink{}
	}
	return &SopTurnToolInvoker{
		root:         root,
		sink:         sink,
		rules:        rules,
		toolExecutor: toolexecutor.NewRoundExecutorSerial(toolexecutor.GetSimpleScriptExecutor()),
	}
}

func (v *SopTurnToolInvoker) Invoke(ctx context.Context, toolName string, args map[string]string) (string, error) {
	payload, err := pCommon.MarshalJson(args)
	if err != nil {
		return pCommon.BlankString, fmt.Errorf("tool invoker: marshal json payload fail: %w", err)
	}

	seq := int(v.seq.Add(1))
	outcomes := v.toolExecutor.RunAll(ctx, v.root.NewToolEnv(v.rules), []toolexecutor.ToolInvocation{{
		Seq: seq,
		Call: llmproto.ToolCall{
			ID:   fmt.Sprintf("plan-%d", seq),
			Type: "function",
			Function: llmproto.ToolCallFunction{
				Name:      toolName,
				Arguments: payload,
			},
		},
	}}, v.sink, v.root.Ledger)

	if len(outcomes) == 0 {
		return pCommon.BlankString, fmt.Errorf("tool invoker: no outcomes: %v", outcomes)
	}

	if outcomes[0].Failed() {
		return pCommon.BlankString, outcomes[0].Reason
	}

	return outcomes[0].Full, nil
}
