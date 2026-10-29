/**
* 工具台账的 **写侧** 契约
*
* 只保留执行器真正需要的两个动作:执行前 Start、执行后 Finish。
* 读侧(Snapshot 落库/回填历史)属于调用方的记账职责,刻意不放在这里,
* 否则会把 ctxmgr 的领域类型下沉进执行层。
**/

package toolexecutor

// ToolLedger 两阶段留痕:先登记"已发起",执行完再回填结果。
// 用户中断时后续代码可能走不到,所以 Start 必须早于任何可能失败的动作。
type ToolLedger interface {
	Start(name string, at int64) int
	Finish(index int, at int64, content string)
}
