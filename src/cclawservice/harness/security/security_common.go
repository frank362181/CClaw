// Package security /*
/**************************************************************
* 职责：
*	工具执行的权限控制
* 设计原则:
*	1. 采用chain of responsibility模式,每个权限检查器只负责一类权限,可组合成链。
**************************************************************/

package security

import (
	"context"
)

const (
	maxSignatureForRepeatToolCalling = 512 //是否重复调用时对工具结果进行签名的前缀token数量
)

type PermissionMode int //权限模式
type Behavior string    //权限模式下的动作

const (
	PermissionModeDefault PermissionMode = iota //自动审批,如读取文件内容
	PermissionModePlan                          // 只读,不执行修改
	PermissionModeAsk                           // 高风险,需确认
	PermissionModeAccept                        // 低风险直接放行;高风险等待用户审批,如创建、覆盖、删除文件、修改环境变量、访问网络等
	PermissionModeBypass                        // 直接旁路,允许
)

const (
	Allow  Behavior = "allow"
	Deny   Behavior = "deny"
	Ask    Behavior = "ask"    //需要用户确认
	Next   Behavior = "next"   // 继续到下一个Handler
	Modify Behavior = "modify" // Hook 修改了输入,继续
)

// IPermissionHandler 是责任链上的一个节点
// 每个Handler检查一个特定的权限维度
// 返回Next表示放行到下一个Handler
// 返回Allow/Deny/Ask 表示段路整条链
type IPermissionHandler interface {
	Handle(ctx context.Context, req *ToolCallRequest, tool IBaseTool) (*Decision, error)
	SetNext(next IPermissionHandler) IPermissionHandler
	Next() IPermissionHandler
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
	IsReadOnly(input map[string]any) bool

	// Call 是实际执行逻辑,管线到达此处时,所有安全检查已通过
	Call(ctx context.Context, input map[string]any, cc CallContext) (any, error)
}

type Decision struct {
	Behavior Behavior
	Reason   string
	Approved bool // 仅当 Behavior== Ask 且用户确认后,此字段指示是否允许
}

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

// IToolHook Hook函数的接口
type IToolHook interface {
	// Run 接收工具调用,返回决策
	// 若返回Modify,则request的Input 已被替换
	Run(ctx context.Context, req *ToolCallRequest) (*Decision, error)
}

// ConfirmFunc 交互确认的回调签名
type ConfirmFunc func(ctx context.Context, req *ToolCallRequest) (bool, error)

// ToolCallRequest 沿着chain of responsibility传递的请求对象
// 携带工具调用所需的一切上下文,且在链中可被修改(如Hook修改Input)
type ToolCallRequest struct {
	ToolName string
	Input    map[string]any // 工具输入参数,可被Hook的updatedInput替换
	Context  CallContext    // 会话上下文
}

type BaseHandler struct {
	next IPermissionHandler
}
