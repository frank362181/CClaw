/**
* LLM 上游协议适配层:错误体 DTO、结构化错误类型与构造器。
*
* 定位:只关心"上游说了什么"(HTTP 状态码 / 错误体 / request_id / 是否可重试),
* 不关心"业务上如何呈现给用户"——后者由 exception 包的 Kind 表与 MapLlmStatusError 负责。
*
* 分层约定:apperr ← llmproto ← exception ← harness ← service ← controller。
* 严禁本包 import my.favorite/cclawservice 下的其它包,否则会与 exception ↔ harness 形成 import cycle。
* 同包文件:classify.go(特征词/分类/错误体解析)、http.go(request_id、非 2xx 响应解析)、retry.go(重试判定)。
**/

package llmproto

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

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

const (
	llmStageResponse = "response" // 首字节前:HTTP 非 2xx 或响应体携带 error
	llmStageStream   = "stream"   // 流中:SSE 已建连(HTTP 200)后发生错误
)

// NewLlmResponseError 构造"HTTP 200 但响应体携带 error 字段"的结构化错误(同步路径)。
// payload 为错误体原始 JSON(即 error 字段的内容),由本包负责解析与分类。
func NewLlmResponseError(payload json.RawMessage) *LlmStatusError {
	api := parseLlmErrorBody(string(payload))
	return &LlmStatusError{
		StatusCode: http.StatusOK,
		Stage:      llmStageResponse,
		Category:   ClassifyLlmError(http.StatusOK, api),
		api:        api,
	}
}

// NewLlmResponseTransportError 构造同步路径(首字节前)的传输层错误(连接失败/读响应失败)。
// 归类为 server(瞬时故障),可被上层重试。
func NewLlmResponseTransportError(err error) *LlmStatusError {
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

// NewLlmStreamError 构造流中错误(连接已建立,HTTP 200)的结构化错误。
// payload 为错误载荷原始 JSON/文本:能解析出结构则按结构分类,否则回退为纯文本消息。
func NewLlmStreamError(payload json.RawMessage, requestID string) *LlmStatusError {
	api := parseLlmErrorBody(string(payload))
	if api.Message == "" && api.Code == "" && api.Type == "" {
		api = LlmAPIError{Message: strings.TrimSpace(string(payload))}
	}
	return &LlmStatusError{
		StatusCode: http.StatusOK,
		RequestID:  requestID,
		Stage:      llmStageStream,
		Category:   ClassifyLlmError(http.StatusOK, api),
		api:        api,
	}
}

// NewLlmStreamTransportError 构造流中传输层错误(断流/读流失败/空闲超时)。
// 归类为 server(瞬时故障);携带上游 request_id 便于排障。
// 是否重试由调用方按"是否已向客户端输出内容"决定(见 IsRetryableLlmError)。
func NewLlmStreamTransportError(err error, requestID string) *LlmStatusError {
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
	if e.Category != "" && e.Category != LlmCategoryUnknown {
		_, _ = fmt.Fprintf(&b, " (%s)", e.Category)
	}
	if msg != "" {
		b.WriteString(": ")
		b.WriteString(msg)
	}
	if e.RequestID != "" {
		_, _ = fmt.Fprintf(&b, " [request_id=%s]", e.RequestID)
	}
	return b.String()
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
