package toolexecutor

import (
	"context"

	"my.favorite/cclawservice/harness/toolcontract"
)

type BashSystemTool struct{}

func NewBashSystemTool() *BashSystemTool {
	return &BashSystemTool{}
}

func (t *BashSystemTool) Name() string {
	return SystemToolBash.String()
}

func (t *BashSystemTool) ValidateInput(ctx context.Context, input map[string]any, cc toolcontract.CallContext) error {
	return nil
}

// CheckPermission 执行工具特定的授权逻辑
func (t *BashSystemTool) CheckPermission(ctx context.Context, input map[string]any, cc toolcontract.CallContext) *toolcontract.Decision {
	return &toolcontract.Decision{
		Behavior: toolcontract.Allow,
		Reason:   "",
	}
}

// IsReadOnly 决定该工具是否可以在plan模式下放行
func (t *BashSystemTool) IsReadOnly() bool {
	return true
}

// Call 是实际执行逻辑,管线到达此处时,所有安全检查已通过
func (t *BashSystemTool) Call(ctx context.Context, input map[string]any, cc toolcontract.CallContext) (any, error) {
	return nil, nil
}
