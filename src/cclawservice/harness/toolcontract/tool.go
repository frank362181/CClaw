package toolcontract

import (
	"context"
)

type SystemToolProfile struct {
	ReadOnly   bool
	Sandboxed  bool
	PathFields []string
	Shelled    bool
}

type BaseTool struct {
	ToolName string
	Profile  SystemToolProfile
}

func (t *BaseTool) Name() string {
	return t.ToolName
}

// IBaseTool 为所有的工具必须实现的接口契约
// 这里的方法尽量不要设计为Handler,Handler是管线的方法,实现调度
// ITool的方法为不同工具不同的方法
type IBaseTool interface {
	Name() string

	// ValidateInput 在权限检查前执行语义检查
	// 失败时将结果作为 tool_result 返回给模型,不弹出权限对话框
	ValidateInput(ctx context.Context, input map[string]any, cc CallContext) error

	// CheckPermission 执行工具特定的授权逻辑
	CheckPermission(ctx context.Context, input map[string]any, cc CallContext) *Decision

	// IsReadOnly 决定该工具是否可以在plan模式下放行
	IsReadOnly() bool

	// Call 是实际执行逻辑,管线到达此处时,所有安全检查已通过
	Call(ctx context.Context, input map[string]any, cc CallContext) (any, error)
}

type ISystemTool interface {
	IBaseTool
}
