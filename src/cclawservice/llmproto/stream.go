/**
* 从上游流式数据块中提取信息:错误载荷的判定与提取。
*
* 纯协议层:只回答"上游这一块说了什么",不做任何业务映射。
* 与 http.go 的 HasError / ParseLlmResponseError 同族(区别:本文件针对 SSE 流内数据块,
* 后者针对非 2xx 响应体)。
**/

package llmproto

import (
	"encoding/json"
	"strings"
)

// StreamErrorPayload 取该 SSE 数据块中的错误载荷:
//   - 数据块含 error 字段(OpenAI 风格)→ 取该字段
//   - 事件类型为 error(Anthropic / 网关自定义)→ 取整个数据块
//
// 注意:这里的 "error" 是**上游协议**的事件名/字段名,与我们对前端下发的事件名无关。
// 无错误时返回 ok=false。载荷的解析与分类见 NewLlmStreamError 与 classify.go。
func StreamErrorPayload(eventName string, chunk ChatCompletionChunk, data string) (json.RawMessage, bool) {
	if HasError(chunk.Error) {
		return chunk.Error, true
	}

	if strings.EqualFold(eventName, "error") {
		return json.RawMessage(data), true
	}

	return nil, false
}
