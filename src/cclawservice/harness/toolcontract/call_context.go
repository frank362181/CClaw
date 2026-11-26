package toolcontract

import "context"

// SandboxConfig 会话级的沙箱显式配置,定义的是每个工具函数的沙箱配置
type SandboxConfig struct {
	Enabled         *bool    // 启动/禁用沙箱,nil表示未指定由模式推导
	AllowedNetwork  *bool    // 启用/禁用网络,nil表示未指定
	AllowedDomains  []string // 允许访问的域名白名单(仅在AllowedNetwork为true时生效)
	AllowedDirWrite []string //允许写入的额外路径
	DeniedDirRead   []string //禁止读取的路径(追加到系统默认之上)
	Backend         string   // 强制指定沙箱环境,空则自动选择
}

// CallContext 封装工具调用所需的会话级信息
type CallContext struct {
	SessionId  string
	WorkingDir string
	Permission PermissionMode

	AllowedTools []string //会话级 allow 规则
	DeniedTools  []string //会话级 deny 规则
	AskTools     []string //会话级 ask 规则

	PreToolHooks []IToolHook // hook 列表

	UserConfirmFunc ConfirmFunc //交互确认回调

	// 根据会话启动时根据配置信息设置的,nil表示未设置,由模式推导
	SandboxConfig *SandboxConfig
}

// ToolCallRequest 沿着chain of responsibility传递的请求对象
// 携带工具调用所需的一切上下文,且在链中可被修改(如Hook修改Input)
type ToolCallRequest struct {
	ToolName string
	Input    map[string]any // 工具输入参数,可被Hook的updatedInput替换
	Context  CallContext    // 会话上下文
}

// IToolHook Hook函数的接口
type IToolHook interface {
	// Run 接收工具调用,返回决策
	// 若返回Modify,则request的Input 已被替换
	Run(ctx context.Context, req *ToolCallRequest) (*Decision, error)
}

// ConfirmFunc 交互确认的回调签名
type ConfirmFunc func(ctx context.Context, req *ToolCallRequest) (bool, error)
