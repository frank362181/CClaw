// Package security /*
/**************************************************************
* 职责：
*	工具执行的权限控制
* 设计原则:
*	1. 采用chain of responsibility模式,每个权限检查器只负责一类权限,可组合成链。
**************************************************************/

package security

type InteractiveApprovalType string

const (
	InteractiveApprovalLocate InteractiveApprovalType = "locate" // 从用户的question中先识别时的确认
	InteractiveApprovalPlan   InteractiveApprovalType = "plan"
)

const (
	maxSignatureForRepeatToolCalling = 512 //是否重复调用时对工具结果进行签名的前缀token数量
)
