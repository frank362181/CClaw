/**
* 定义安全场景下工具执行的结果类型定义
**/

package toolcontract

type PermissionMode int //权限模式

const (
	PermissionModeDefault PermissionMode = iota //自动审批,如读取文件内容
	PermissionModePlan                          // 只读,不执行修改
	PermissionModeAsk                           // 高风险,需确认
	PermissionModeAccept                        // 低风险直接放行;高风险等待用户审批,如创建、覆盖、删除文件、修改环境变量、访问网络等
	PermissionModeBypass                        // 直接旁路,允许
)

// Behavior 权限管线对一次工具调用给出的动作
type Behavior string //权限模式下的动作
const (
	Allow  Behavior = "allow"
	Deny   Behavior = "deny"
	Ask    Behavior = "ask"    //需要用户确认
	Next   Behavior = "next"   // 继续到下一个Handler
	Modify Behavior = "modify" // Hook 修改了输入,继续
)

// Decision 一次权限判断的结果
type Decision struct {
	Behavior Behavior
	Reason   string
	Approved bool // 仅当 Behavior== Ask 且用户确认后,此字段指示是否允许
}
