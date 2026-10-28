package tool

import (
	"context"

	"my.favorite/cclawservice/harness/security"
)

type WriteSystemTool struct{}

func NewWriteSystemTool() *WriteSystemTool {
	return &WriteSystemTool{}
}

func (t *WriteSystemTool) Name() string {
	return SystemToolWrite.String()
}

func (t *WriteSystemTool) ValidateInput(ctx context.Context, input map[string]any, cc security.CallContext) error {
	return nil
}

// CheckPermission 执行工具特定的授权逻辑
func (t *WriteSystemTool) CheckPermission(ctx context.Context, input map[string]any, cc security.CallContext) *security.Decision {
	return &security.Decision{
		Behavior: security.Allow,
		Reason:   "",
	}
}

// IsReadOnly 决定该工具是否可以在plan模式下放行
func (t *WriteSystemTool) IsReadOnly(input map[string]any) bool {
	return true
}

// Call 是实际执行逻辑,管线到达此处时,所有安全检查已通过
func (t *WriteSystemTool) Call(ctx context.Context, input map[string]any, cc security.CallContext) (any, error) {
	return nil, nil
}
