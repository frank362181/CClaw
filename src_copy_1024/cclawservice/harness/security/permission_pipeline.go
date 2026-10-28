/*
* 责任链管线,将调用工具时的前中后的执行逻辑定义为一个pipeline
**/

package security

import pCommon "my.favorite/platformutility/common"

type PermissionPipeline struct {
	Head   IPermissionHandler
	Length int
}

// NewDefaultPermissionPipeline 按照指定的顺序组装管线,基于责任链
//  1. ValidateInputHandler   —— 语义校验，失败直接返回错误给模型
//  2. HookHandler            —— ToolUse hooks，可修改/拒绝
//  3. DenyRuleHandler        —— 硬拒绝规则，不可覆盖
//  4. AskRuleHandler         —— 强制确认规则
//  5. ToolPermissionHandler  —— 工具自身的 checkPermissions()
//  6. SafetyGuardrailHandler —— 抗绕过的安全护栏
//  7. PermissionModeHandler  —— 权限模式策略
//  8. AllowRuleHandler       —— 自动放行规则 + 只读工具
//  9. InteractiveConfirmHandler —— 用户交互确认（链尾）
var defaultPermissionPipeline = pCommon.GetSingleton(func() *PermissionPipeline {
	head := &ValidateInputHandler{}
	prev := head.SetNext(&PreToolUseHook{})
	prev = prev.SetNext(&DenyRuleHandler{})
	prev = prev.SetNext(&AskRuleHandler{})
	prev = prev.SetNext(&ToolSelfCheckHandler{})
	prev = prev.SetNext(&SafetyGuardrailHandler{})
	prev = prev.SetNext(&PermissionModeHandler{})
	prev = prev.SetNext(&AllowRuleHandler{})
	prev = prev.SetNext(&InteractiveConfirmHandler{})

	return &PermissionPipeline{Head: head}
}).Instance()

func GetPermissionPipeline() *PermissionPipeline {
	return defaultPermissionPipeline
}
