// Package security /*
/**************************************************************
* 职责：
*	工具执行的权限控制
* 权限分为三个等级:
*	Tier1: 自动审批,限于读取文件等只读的操作
*	Tier2: 等待用户确认,编辑文件
*	Tier3: 等待用户审批,创建文件、覆盖文件、删除文件、修改环境变量、访问网络等高危操作
**************************************************************/

package security

type PermissionMode int

const (
	PermissionModeRead    PermissionMode = iota //自动审批,如读取文件内容
	PermissionModeWrite                         // 等待用户确认,修改文件等
	PermissionModeApprove                       // 等待用户审批,如创建、覆盖、删除文件、修改环境变量、访问网络等
)
