/*
* ToolInvocation的目的:
*	取代 calls[] + seq[] 两个并排切片,原因:
*		- 因为两个数组的下标语义并不相同 --
*		  calls 是模型声明顺序(FunctionCallAccumulator的a.order);
*		  seq的分配键是"上游的delta index"
*	合并后配对只在Invocation一处发生,下游Executor/Sink/Journal等再也看不到两处下标
**/

package chat

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

// 将模型声明的调用与序号配对
func bindToolInvocation(calls []llmproto.ToolCall, seq *seqSink) []ToolInvocation {
	out := make([]ToolInvocation, 0, len(calls))
	seen := make(map[int]struct{}, len(calls))

	for i := range calls {
		key := calls[i].Index
		if _, dup := seen[key]; dup {
			key = -1 - i //负数 -> SeqOf 走独立分配
		}
		seen[key] = struct{}{}

		out = append(out, ToolInvocation{Seq: seq.SeqOf(key), Call: calls[i]})
	}

	return out
}
