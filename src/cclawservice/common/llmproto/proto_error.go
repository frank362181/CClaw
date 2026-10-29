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
	"fmt"
	"io"
	"net/http"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

const (
	llmStageResponse = "response" // 首字节前:HTTP 非 2xx 或响应体携带 error
	llmStageStream   = "stream"   // 流中:SSE 已建连(HTTP 200)后发生错误
)

// requestIDHeaders 上游 request_id 可能出现的响应头(按优先级;http.Header.Get 大小写不敏感)。
var requestIDHeaders = []string{
	"X-Request-Id",
	"X-Dashscope-Request-Id",
	"Request-Id",
	"X-Trace-Id",
	"Trace-Id",
}

// ErrRequestCanceled 表示请求上下文被取消(客户端断开/超时),而非上游故障。
var ErrRequestCanceled = errors.New("runtime client: request context canceled")

// ======================== 错误体 DTO 与结构化错误类型 ========================

// LlmAPIError 供应商返回的错误体(OpenAI 风格)。
// 供本包分类与上层诊断使用;对业务层仍以 LlmStatusError.Error() 的文本呈现,不泄漏协议字段。
// Param 指出出错的具体字段(OpenAI 风格),便于定位请求配置问题。
type LlmAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

// LlmStatusError 结构化的 LLM 上游错误。
// 统一携带 HTTP 状态码、上游 request_id 与语义化类别,是 ParseLlmResponseError 与流中错误的
// 返回类型,便于上层按类别映射用户文案与 HTTP 状态。
// 注:不携带内部错误码 —— 业务码/HTTP 响应码统一由 exception 包的 Kind 承载(apperr 注册表)。
type LlmStatusError struct {
	StatusCode int              // 上游 HTTP 状态码;流中错误(连接建立时已 200)记 200
	RequestID  string           // 上游 request_id(响应头),便于排障/提交工单
	Stage      string           // 错误发生阶段:response(首字节前)/ stream(流中)
	Category   LlmErrorCategory // 语义化类别

	api LlmAPIError // 解析出的错误体(包内;可能为空)
	raw string      // 原始响应体(无法结构化解析时的兜底)
}

// Is 支持按"阶段 + 类别"匹配,例如:
//
//	errors.Is(err, &LlmStatusError{Category: LlmCategoryQuota})
//
// 不再依赖内部错误码(业务码统一由 exception 的 Kind 承载)。
func (e *LlmStatusError) Is(target error) bool {
	ed, ok := errors.AsType[*LlmStatusError](target)
	if !ok {
		return false
	}
	return ed.Stage == e.Stage && ed.Category == e.Category
}

// Error 实现 error 接口;文本包含阶段/状态码/类别/上游消息/request_id,便于日志诊断。
func (e *LlmStatusError) Error() string {
	msg := strings.TrimSpace(e.api.Message)
	if msg == "" {
		msg = strings.TrimSpace(e.raw)
	}
	var b strings.Builder
	b.WriteString("runtime client: ")
	switch {
	case e.Stage == llmStageStream:
		b.WriteString("stream error")
	case e.StatusCode > 0:
		_, _ = fmt.Fprintf(&b, "http %d", e.StatusCode)
	default:
		b.WriteString("transport error")
	}
	if e.Category != pCommon.BlankString && e.Category != LlmCategoryUnknown {
		_, _ = fmt.Fprintf(&b, " (%s)", e.Category)
	}
	if msg != pCommon.BlankString {
		b.WriteString(": ")
		b.WriteString(msg)
	}
	if e.RequestID != pCommon.BlankString {
		_, _ = fmt.Fprintf(&b, " [request_id=%s]", e.RequestID)
	}
	return b.String()
}

// GetLlmResponseError 构造"HTTP 200 但响应体携带 error 字段"的结构化错误(同步路径)。
// payload 为错误体原始 JSON(即 error 字段的内容),由本包负责解析与分类。
func GetLlmResponseError(payload json.RawMessage) *LlmStatusError {
	api := ParseLlmErrorBody(string(payload))
	return &LlmStatusError{
		StatusCode: http.StatusOK,
		Stage:      llmStageResponse,
		Category:   classifyLlmError(http.StatusOK, api),
		api:        api,
	}
}

func GetLlmEmptyResponseError(requestId string) *LlmStatusError {
	return &LlmStatusError{
		StatusCode: 0,
		RequestID:  requestId,
		Stage:      llmStageResponse,
		Category:   LlmCategoryServer,
		raw:        "empty response: no content and no tool_call",
	}
}

// GetLlmResponseTransportError 构造同步路径(首字节前)的传输层错误(连接失败/读响应失败)。
// 归类为 server(瞬时故障),可被上层重试。
func GetLlmResponseTransportError(err error) *LlmStatusError {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return &LlmStatusError{
		Stage:    llmStageResponse,
		Category: LlmCategoryServer,
		api:      LlmAPIError{Message: detail, Type: "transport_error"},
		raw:      detail,
	}
}

// GetLlmStreamError 构造流中错误(连接已建立,HTTP 200)的结构化错误。
// payload 为错误载荷原始 JSON/文本:能解析出结构则按结构分类,否则回退为纯文本消息。
func GetLlmStreamError(payload json.RawMessage, requestID string) *LlmStatusError {
	api := ParseLlmErrorBody(string(payload))
	if api.Message == "" && api.Code == "" && api.Type == "" {
		api = LlmAPIError{Message: strings.TrimSpace(string(payload))}
	}
	return &LlmStatusError{
		StatusCode: http.StatusOK,
		RequestID:  requestID,
		Stage:      llmStageStream,
		Category:   classifyLlmError(http.StatusOK, api),
		api:        api,
	}
}

func GetLlmEmptyStreamError(requestID string) *LlmStatusError {
	return &LlmStatusError{
		StatusCode: 0,
		RequestID:  requestID,
		Stage:      llmStageStream,
		Category:   LlmCategoryServer,
		raw:        "empty stream: no content and no tool_call",
	}
}

// GetLlmStreamTransportError 构造流中传输层错误(断流/读流失败/空闲超时)。
// 归类为 server(瞬时故障);携带上游 request_id 便于排障。
// 是否重试由调用方按"是否已向客户端输出内容"决定(见 IsRetryableLlmError)。
func GetLlmStreamTransportError(err error, requestID string) *LlmStatusError {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	return &LlmStatusError{
		RequestID: requestID,
		Stage:     llmStageStream,
		Category:  LlmCategoryServer,
		api:       LlmAPIError{Message: detail, Type: "transport_error"},
		raw:       detail,
	}
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

	apiErr := ParseLlmErrorBody(raw)
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
// 无错误时返回 ok=false。载荷的解析与分类见 GetLlmStreamError 与 classify_error.go。
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

// IsQuotaError 判断错误是否为 "LLM额度耗尽 / 欠费"(裸错误兜底;结构化错误用 LlmStatueErrorCategory)
// 上游可能用非标准状态码返回欠费(如 dashscope/深度),因此需要文案特征兜底
func IsQuotaError(err error) bool {
	if err == nil {
		return false
	}

	return pCommon.ContainsString(strings.TrimSpace(err.Error()), quotaSignals)
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

// ParseLlmErrorBody 解析上游错误体:优先 OpenAI 风格 {"error":{...}},回退扁平结构
// {message,type,code,param}。
func ParseLlmErrorBody(raw string) LlmAPIError {
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
