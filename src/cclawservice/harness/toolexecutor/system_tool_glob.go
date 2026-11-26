package toolexecutor

import (
	"context"
	"strings"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	hException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/harness/toolcontract"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
)

type GlobSystemTool struct {
	toolcontract.BaseTool

	toolEnv      ToolEnv
	toolExecutor IToolExecutedWrapper
}

func NewGlobSystemTool(toolSpec ToolSpec, workRoot, scope string, rules ToolConfigRules) (toolcontract.ISystemTool, error) {
	if strings.EqualFold(toolSpec.Name, SystemToolGlob.String()) {
		return nil, exception.UnMatchErrorEx2("system tool: tool mismatch,want %s,got %s", SystemToolGlob.String(), toolSpec.Name)
	}

	// 获取系统工具的系统配置信息
	toolProfile := LookupSystemToolProfile(SystemToolGlob.String())
	env := NewToolEnv(GetScriptsResolver(map[string]ToolSpec{SystemToolGlob.String(): toolSpec}), workRoot, scope, rules)
	return &GlobSystemTool{
		BaseTool: toolcontract.BaseTool{
			ToolName: toolSpec.Name,
			Profile:  toolProfile,
		},
		toolEnv:      env,
		toolExecutor: NewRoundExecutorSerial(GetSimpleScriptExecutor()),
	}, nil
}

func (t *GlobSystemTool) ValidateInput(ctx context.Context, input map[string]any, cc toolcontract.CallContext) error {
	return nil
}

// CheckPermission 执行工具特定的授权逻辑
func (t *GlobSystemTool) CheckPermission(ctx context.Context, input map[string]any, cc toolcontract.CallContext) *toolcontract.Decision {
	return &toolcontract.Decision{
		Behavior: toolcontract.Allow,
		Reason:   "",
	}
}

// IsReadOnly 决定该工具是否可以在plan模式下放行
func (t *GlobSystemTool) IsReadOnly() bool {
	return t.Profile.ReadOnly
}

// Call 是实际执行逻辑,管线到达此处时,所有安全检查已通过
func (t *GlobSystemTool) Call(ctx context.Context, input map[string]any, cc toolcontract.CallContext) (any, error) {
	payload, err := pCommon.MarshalJson(input)
	if err != nil {
		return nil, err
	}

	outcomes := t.toolExecutor.RunAll(ctx, t.toolEnv, []ToolInvocation{{
		Seq: 1,
		Call: llmproto.ToolCall{
			ID:   "locate-glob-1",
			Type: "function",
			Function: llmproto.ToolCallFunction{
				Name:      "",
				Arguments: payload,
			},
		},
	}}, cCommon.NopSink{}, NopToolLedger{})

	if len(outcomes) == 0 {
		return nil, hException.ToolExecutionNoResultKind
	}

	if outcomes[0].Failed() {
		return nil, outcomes[0].Reason
	}

	return outcomes[0].Full, nil
}
