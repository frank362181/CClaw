/**
* 上游 HTTP 层面的协议工具:request_id 提取、错误体判定、非 2xx 响应解析。
*
* 纯协议层:不引用任何业务 Kind。
**/

package llmproto

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

// requestIDHeaders 上游 request_id 可能出现的响应头(按优先级;http.Header.Get 大小写不敏感)。
var requestIDHeaders = []string{
	"X-Request-Id",
	"X-Dashscope-Request-Id",
	"Request-Id",
	"X-Trace-Id",
	"Trace-Id",
}

// GetRequestIDFromLlmResponse 从上游响应头提取 request_id,便于排障/提交工单。
func GetRequestIDFromLlmResponse(header http.Header) string {
	if header == nil {
		return pCommon.BlankString
	}
	for _, h := range requestIDHeaders {
		if v := strings.TrimSpace(header.Get(h)); v != pCommon.BlankString {
			return v
		}
	}
	return pCommon.BlankString
}

// ParseLlmResponseError 解析上游非 2xx 响应,返回带 HTTP 状态码、request_id 与语义化类别的
// 结构化错误(*LlmStatusError),供上层按类别映射用户文案。
func ParseLlmResponseError(resp *http.Response, readBytes int64) error {
	statusCode := 0
	requestID := pCommon.BlankString
	if resp != nil {
		statusCode = resp.StatusCode
		requestID = GetRequestIDFromLlmResponse(resp.Header)
	}

	raw := pCommon.BlankString
	if resp != nil && resp.Body != nil {
		if data, err := io.ReadAll(io.LimitReader(resp.Body, readBytes)); err == nil {
			raw = strings.TrimSpace(string(data))
		}
	}

	apiErr := parseLlmErrorBody(raw)
	return &LlmStatusError{
		StatusCode: statusCode,
		RequestID:  requestID,
		Stage:      llmStageResponse,
		Category:   classifyLlmError(statusCode, apiErr),
		api:        apiErr,
		raw:        raw,
	}
}

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

// HasError 判定 JSON 载荷中是否携带错误体(error 字段存在且非 null)。
// 供调用方判断上游是否返回错误,而无需感知错误体的内部结构。
func HasError(payload json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(payload))
	return trimmed != pCommon.BlankString && trimmed != "null"
}

// IsQuotaError 判定错误是否为"LLM 额度耗尽/欠费"(裸错误兜底;结构化错误请用 LlmStatusError.Category)。
// 上游可能用非标准状态码返回欠费(如 dashscope/深度),因此需要文案特征兜底。
func IsQuotaError(err error) bool {
	if err == nil {
		return false
	}
	return pCommon.ContainsString(strings.ToLower(err.Error()), quotaSignals)
}

// ErrRequestCanceled 表示请求上下文被取消(客户端断开/超时),而非上游故障。
//var ErrRequestCanceled = errors.New("runtime client: request context canceled")

// IsRetryableLlmError 判定错误是否可安全重试(瞬时故障)。
//   - 传输层故障(连接失败/断流;StatusCode==0)可重试;
//   - 真限流(429)与真服务端故障(5xx)可重试;
//   - HTTP 200 携带 error 体的应用级错误、客户端取消、配置类错误(鉴权/模型名/参数/欠费/内容合规)不重试。
func IsRetryableLlmError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRequestCanceled) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if statusErr, ok := errors.AsType[*LlmStatusError](err); ok {
		switch statusErr.Category {
		case LlmCategoryRateLimit:
			return true
		case LlmCategoryServer:
			return statusErr.StatusCode == 0 || statusErr.StatusCode >= 500
		}
	}
	return false
}

// ===================== private functions & methods =======================

// parseLlmErrorBody 解析上游错误体:优先 OpenAI 风格 {"error":{...}},回退扁平结构
// {message,type,code,param}。
func parseLlmErrorBody(raw string) LlmAPIError {
	if strings.TrimSpace(raw) == "" {
		return LlmAPIError{}
	}
	var wrapped struct {
		Error *LlmAPIError `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &wrapped) == nil && wrapped.Error != nil &&
		(wrapped.Error.Message != "" || wrapped.Error.Code != "" || wrapped.Error.Type != "") {
		return *wrapped.Error
	}
	var flat LlmAPIError
	if json.Unmarshal([]byte(raw), &flat) == nil &&
		(flat.Message != "" || flat.Code != "" || flat.Type != "") {
		return flat
	}
	return LlmAPIError{}
}
