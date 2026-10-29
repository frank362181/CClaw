package toolexecutor

import (
	"context"

	"my.favorite/cclawservice/harness/security"
)

type GlobSystemTool struct{}

func NewGlobSystemTool() *GlobSystemTool {
	return &GlobSystemTool{}
}

func (t *GlobSystemTool) Name() string {
	return SystemToolGlob.String()
}

func (t *GlobSystemTool) ValidateInput(ctx context.Context, input map[string]any, cc security.CallContext) error {
	return nil
}

// CheckPermission 执行工具特定的授权逻辑
func (t *GlobSystemTool) CheckPermission(ctx context.Context, input map[string]any, cc security.CallContext) *security.Decision {
	return &security.Decision{
		Behavior: security.Allow,
		Reason:   "",
	}
}

// IsReadOnly 决定该工具是否可以在plan模式下放行
func (t *GlobSystemTool) IsReadOnly(input map[string]any) bool {
	return true
}

// Call 是实际执行逻辑,管线到达此处时,所有安全检查已通过
func (t *GlobSystemTool) Call(ctx context.Context, input map[string]any, cc security.CallContext) (any, error) {
	return nil, nil
}
