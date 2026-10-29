/**
* 一次待执行的工具调用 = 协议载荷 + 前端关联序号
*
* 取代 calls[] + seq[] 两个并排切片:
*   - calls 的下标是模型声明顺序(FunctionCallAccumulator 的 a.order)
*   - seq 的分配键是"上游的 delta index",两者语义并不相同
* 合并后配对只发生在绑定处,下游 Executor/Sink/Journal 再也看不到两处下标。
**/

package toolexecutor

import "my.favorite/cclawservice/common/llmproto"

// ToolInvocation 一次待执行的工具调用 = 协议载荷 + 前端关联序号
type ToolInvocation struct {
	Seq  int               // 全局单调的前端关联序号(一次对话内唯一)
	Call llmproto.ToolCall // 协议载荷(Index保留,便于日志排障)
}

func (ti ToolInvocation) ID() string {
	return ti.Call.ID
}

func (ti ToolInvocation) Name() string {
	return ti.Call.Function.Name
}

func (ti ToolInvocation) Args() string {
	return ti.Call.Function.Arguments
}
