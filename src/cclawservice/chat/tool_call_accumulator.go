/**
* LLM 工具调用(Function Calling)支持
*
* 职责:
*	- 管理执行分片（LLM一次只能表达一步意图）
*	- agent loop支持多次调用，必须对请求和响应的分片进行封装
*
* 工具执行不在本包:调用方拿到 ToolCall 后调用执行器,再把结果以 tool 消息回填后继续循环。
**/

package chat

import (
	"strings"

	"my.favorite/cclawservice/common/llmproto"
)

// FunctionCallAccumulator 流式工具调用累积器:按 index 保存调用,并拼接分片的 function.arguments。
type FunctionCallAccumulator struct {
	calls map[int]*llmproto.ToolCall
	args  map[int]*strings.Builder
	order []int
}

func NewToolCallAccumulator() *FunctionCallAccumulator {
	return &FunctionCallAccumulator{
		calls: make(map[int]*llmproto.ToolCall),
		args:  make(map[int]*strings.Builder),
	}
}

func (a *FunctionCallAccumulator) Add(delta llmproto.ToolCallDelta) {
	if _, ok := a.calls[delta.Index]; !ok {
		a.calls[delta.Index] = &llmproto.ToolCall{}
		a.args[delta.Index] = &strings.Builder{}
		a.order = append(a.order, delta.Index)
	}
	call := a.calls[delta.Index]
	if delta.ID != "" {
		call.ID = delta.ID
	}
	if delta.Type != "" {
		call.Type = delta.Type
	}
	if delta.Function.Name != "" {
		call.Function.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		a.args[delta.Index].WriteString(delta.Function.Arguments)
	}
}

// Result 按出现顺序返回完整的工具调用列表(arguments 已拼接完整)。
func (a *FunctionCallAccumulator) Result() []llmproto.ToolCall {
	out := make([]llmproto.ToolCall, 0, len(a.order))
	for _, idx := range a.order {
		call := a.calls[idx]
		call.Function.Arguments = a.args[idx].String()
		call.Index = idx // <- 关键代码:不在丢弃
		out = append(out, *call)
	}
	return out
}
