package toolexecutor

import (
	"context"

	"my.favorite/cclawservice/harness/toolcontract"
)

type GrepSystemTool struct{}

func NewGrepSystemTool() *GrepSystemTool {
	return &GrepSystemTool{}
}

func (t *GrepSystemTool) Name() string {
	return SystemToolGrep.String()
}

func (t *GrepSystemTool) ValidateInput(ctx context.Context, input map[string]any, cc toolcontract.CallContext) error {
	return nil
}

// CheckPermission 执行工具特定的授权逻辑
func (t *GrepSystemTool) CheckPermission(ctx context.Context, input map[string]any, cc toolcontract.CallContext) *toolcontract.Decision {
	return &toolcontract.Decision{
		Behavior: toolcontract.Allow,
		Reason:   "",
	}
}

// IsReadOnly 决定该工具是否可以在plan模式下放行
func (t *GrepSystemTool) IsReadOnly() bool {
	return true
}

// Call 是实际执行逻辑,管线到达此处时,所有安全检查已通过
func (t *GrepSystemTool) Call(ctx context.Context, input map[string]any, cc toolcontract.CallContext) (any, error) {
	return nil, nil
}
