/**
* sop 事件调用
**/

package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/harness/toolexecutor"
	chatCommon "my.favorite/cclawservice/runtime/common"
	"my.favorite/cclawservice/runtime/plan_mode"
	pCommon "my.favorite/platformutility/common"
)

const (
	// 错误时返回的最大的错误长度
	planToolInvokerErrorDetailMaxRunes = 512
)

type SopTurnToolInvoker struct {
	root         *chatCommon.TurnState
	sink         cCommon.ISink
	rules        toolexecutor.ToolConfigRules
	toolExecutor toolexecutor.IToolExecutedWrapper
	seq          atomic.Int64
}

var _ plan_mode.ISopToolInvoker = (*SopTurnToolInvoker)(nil)

func NewSOPTurnToolInvoker(root *chatCommon.TurnState, sink cCommon.ISink, rules toolexecutor.ToolConfigRules) *SopTurnToolInvoker {
	if sink == nil {
		sink = cCommon.NopSink{}
	}
	return &SopTurnToolInvoker{
		root:         root,
		sink:         sink,
		rules:        rules,
		toolExecutor: toolexecutor.NewRoundExecutorSerial(toolexecutor.GetSimpleScriptExecutor()),
	}
}

func (v *SopTurnToolInvoker) InvokeRaw(ctx context.Context, toolName, argsJson string) (string, error) {
	seq := int(v.seq.Add(1))
	outcomes := v.toolExecutor.RunAll(ctx, v.root.NewToolEnv(v.rules), []toolexecutor.ToolInvocation{{
		Seq: seq,
		Call: llmproto.ToolCall{
			ID:   fmt.Sprintf("plan-%d", seq),
			Type: "function",
			Function: llmproto.ToolCallFunction{
				Name:      toolName,
				Arguments: argsJson,
			},
		},
	}}, v.sink, v.root.Ledger)

	if len(outcomes) == 0 {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return pCommon.BlankString, fmt.Errorf("tool invoker:%s not executed: %w", toolName, ctxErr)
		}
		return pCommon.BlankString, fmt.Errorf("tool invoker: no outcomes: %v", outcomes)
	}

	if outcomes[0].Failed() {
		return pCommon.BlankString, truncateFailure(outcomes[0])
	}

	if strings.TrimSpace(outcomes[0].Full) == pCommon.BlankString {
		return pCommon.BlankString, fmt.Errorf("tool invoker: %s produce no outcome", toolName)
	}

	return outcomes[0].Full, nil
}

func (v *SopTurnToolInvoker) Invoke(ctx context.Context, toolName string, args map[string]string) (string, error) {
	payload, err := pCommon.MarshalJson(args)
	if err != nil {
		return pCommon.BlankString, fmt.Errorf("tool invoker: failed to marshal args: %w", err)
	}

	return v.InvokeRaw(ctx, toolName, payload)
}

func truncateFailure(outcome toolexecutor.ToolOutcome) error {
	if outcome.Reason == nil {
		return nil
	}
	detail := strings.TrimSpace(outcome.Full)
	if detail == pCommon.BlankString {
		return outcome.Reason
	}

	return fmt.Errorf("%w | script output: %s", outcome.Reason,
		pCommon.TruncateRunes(detail, planToolInvokerErrorDetailMaxRunes, cCommon.TruncatedMarker))
}
