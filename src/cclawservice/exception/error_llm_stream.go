/**
* LLM 上游错误的基础设施:供应商错误体解析、语义化分类、结构化错误类型。
*
* 定位:这里只关心"上游说了什么"(HTTP 状态码 / 错误体 / request_id / 是否可重试),
 * 不关心"业务上如何呈现给用户"——后者由 exception 的 Kind 表负责。
* 因此本文件不依赖任何业务错误定义,可被 harness 内部与上层共同复用。
*
* 历史:原分散在 exception/error_harness.go(协议类型)与 exception/error_utility.go(解析/分类),
* 因属于基础设施而非业务错误,统一收敛到 harness。
**/

package exception

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// ======================== 1. 供应商错误体解析与分类 ========================

// llmAPIError 供应商返回的错误体(OpenAI 风格);仅包内使用,不把供应商协议细节暴露给调用方。
// Param 指出出错的具体字段(OpenAI 风格),便于定位请求配置问题。
type llmAPIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param"`
	Code    string `json:"code"`
}

// LLM 上游错误的特征词(统一转小写后做子串匹配)。
// 说明:上游错误文案千差万别,这里只做"尽力而为"的粗分类;能拿到 HTTP 状态码时以状态码为准。
var (
	// contentFilterSignals 内容安全/合规拦截特征
	contentFilterSignals = []string{
		"data_inspection", "data inspection", "content_filter", "content filter",
		"content policy", "inappropriate content", "safety system", "responsible ai",
		"content management policy", "prohibited", "ip infringement",
		"敏感", "违规", "内容安全", "绿网", "不合规", "侵权",
	}
	// quotaSignals 额度耗尽/欠费特征(可能出现在 400/402/429)
	quotaSignals = []string{
		"402",                  // HTTP 402 Payment Required(如 deepseek 余额不足)
		"payment required",     // OpenAI 风格描述
		"insufficient_quota",   // OpenAI 风格 error.code
		"insufficient balance", // deepseek 余额不足
		"insufficient funds",   // 通用余额不足
		"out of quota",         // dashscope: Throttling.User.OutOfQuota
		"quota exceeded",       // 通用配额用尽
		"allocationquota",      // dashscope: Throttling.AllocationQuota
		"arrearage",            // dashscope: 欠费
		"out_of_service",       // dashscope: isv.OUT_OF_SERVICE(余额不足暂停)
		"prepaidbilloverdue",   // dashscope: 预付账单逾期
		"postpaidbilloverdue",  // dashscope: 后付账单逾期
		"billing",              // 计费相关
		"欠费", "余额不足", "额度不足", "额度耗尽", "无可用额度", "请充值",
	}
	// rateLimitSignals 限流特征(非额度型)
	rateLimitSignals = []string{
		"rate limit", "rate_limit", "ratelimit", "throttling", "too many requests",
		"limit_requests", "resourceexhausted", "resource exhausted", "burst_rate",
		"requests rate", "slow down", "limit reached", "限流", "请求过于频繁",
	}
	// authSignals 鉴权失败特征
	authSignals = []string{
		"invalid_api_key", "invalid api key", "incorrect api key", "unauthorized",
		"authentication", "no api key", "api key not valid", "unauthenticated",
		"invalid access token", "鉴权",
	}
	// forbiddenSignals 无权限特征
	forbiddenSignals = []string{
		"access_denied", "access denied", "permission", "not authorized",
		"forbidden", "unpurchased", "not activated", "no permission", "无权",
	}
	// notFoundSignals 模型不存在特征
	notFoundSignals = []string{
		"model_not_found", "model not found", "model not exist", "no such model",
		"does not exist", "not_found", "model_not_supported", "模型不存在",
	}
	// serverSignals 服务端故障特征
	serverSignals = []string{
		"internal error", "internal_error", "server error", "server_error",
		"overloaded", "service unavailable", "bad gateway", "modelunavailable",
		"system error", "inference internal",
	}
	// invalidRequestSignals 请求非法特征
	invalidRequestSignals = []string{
		"invalid_request", "invalid request", "invalid_parameter", "invalid parameter",
		"bad request", "badrequest", "deserialize", "context_length", "context length",
		"too long", "missing required", "invalid value",
	}
)

// classifyLlmError 依据 HTTP 状态码与上游错误体判定语义化类别(包内使用)。
// 优先级:内容安全 > 额度 > HTTP 状态码 > 文案兜底(流中错误无状态码时)。
func classifyLlmError(statusCode int, api llmAPIError) LlmErrorCategory {
	code := strings.ToLower(strings.TrimSpace(api.Code))
	typ := strings.ToLower(strings.TrimSpace(api.Type))
	msg := strings.ToLower(api.Message)
	head := code + " " + typ
	all := head + " " + msg

	// 1. 内容安全/合规:与状态码无关(常见 400,或流中 finish_reason=content_filter)
	if pCommon.ContainsString(all, contentFilterSignals) {
		return LlmCategoryContentFilter
	}
	// 2. 额度/欠费:可能以 400/402/429 返回,须先于通用限流判定
	if pCommon.ContainsString(head, quotaSignals) || pCommon.ContainsString(msg, quotaSignals) {
		return LlmCategoryQuota
	}
	// 3. HTTP 状态码(最可靠)
	switch statusCode {
	case http.StatusUnauthorized:
		return LlmCategoryAuth
	case http.StatusPaymentRequired:
		return LlmCategoryQuota
	case http.StatusForbidden:
		return LlmCategoryForbidden
	case http.StatusNotFound:
		return LlmCategoryNotFound
	case http.StatusTooManyRequests:
		return LlmCategoryRateLimit
	}
	if statusCode >= 500 {
		return LlmCategoryServer
	}
	if statusCode == http.StatusBadRequest || statusCode == http.StatusUnprocessableEntity {
		return LlmCategoryInvalidRequest
	}
	// 4. 无可用状态码(如流中错误,statusCode=200)时按文案兜底
	if pCommon.ContainsString(all, rateLimitSignals) {
		return LlmCategoryRateLimit
	}
	if pCommon.ContainsString(all, authSignals) {
		return LlmCategoryAuth
	}
	if pCommon.ContainsString(all, forbiddenSignals) {
		return LlmCategoryForbidden
	}
	if pCommon.ContainsString(all, notFoundSignals) {
		return LlmCategoryNotFound
	}
	if pCommon.ContainsString(all, serverSignals) {
		return LlmCategoryServer
	}
	if pCommon.ContainsString(all, invalidRequestSignals) {
		return LlmCategoryInvalidRequest
	}
	return LlmCategoryUnknown
}

// IsQuotaError 判定错误是否为"LLM 额度耗尽/欠费"(裸错误兜底;结构化错误请用 LlmStatusError.Category)。
// 上游可能用非标准状态码返回欠费(如 dashscope/深度),因此需要文案特征兜底。
func IsQuotaError(err error) bool {
	if err == nil {
		return false
	}
	return pCommon.ContainsString(strings.ToLower(err.Error()), quotaSignals)
}

// parseLlmErrorBody 解析上游错误体(包内):优先 OpenAI 风格 {"error":{...}},回退扁平结构 {message,type,code,param}。
func parseLlmErrorBody(raw string) llmAPIError {
	if strings.TrimSpace(raw) == "" {
		return llmAPIError{}
	}
	var wrapped struct {
		Error *llmAPIError `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &wrapped) == nil && wrapped.Error != nil &&
		(wrapped.Error.Message != "" || wrapped.Error.Code != "" || wrapped.Error.Type != "") {
		return *wrapped.Error
	}
	var flat llmAPIError
	if json.Unmarshal([]byte(raw), &flat) == nil &&
		(flat.Message != "" || flat.Code != "" || flat.Type != "") {
		return flat
	}
	return llmAPIError{}
}

// ======================== 2. 结构化错误类型 ========================

// LlmErrorCategory LLM 上游错误的语义化类别。
// 目的:把"可自行修正的配置类错误"(鉴权/模型名/参数/欠费)与"临时性故障"(限流/服务端)
// 分开,从而给出可操作的用户提示,而不是千篇一律的"引擎暂时不可用"。
type LlmErrorCategory string

const (
	// LlmCategoryInvalidRequest 请求非法(参数/上下文/请求体格式)
	LlmCategoryInvalidRequest LlmErrorCategory = "invalid_request"
	// LlmCategoryAuth 鉴权失败(API Key 缺失/无效)
	LlmCategoryAuth LlmErrorCategory = "auth"
	// LlmCategoryForbidden 无权限(未开通/未授权/模型下线)
	LlmCategoryForbidden LlmErrorCategory = "forbidden"
	// LlmCategoryNotFound 模型不存在或名称配置错误
	LlmCategoryNotFound LlmErrorCategory = "not_found"
	// LlmCategoryQuota 额度耗尽/欠费/账单逾期
	LlmCategoryQuota LlmErrorCategory = "quota"
	// LlmCategoryRateLimit 触发限流(可稍后重试)
	LlmCategoryRateLimit LlmErrorCategory = "rate_limit"
	// LlmCategoryServer 上游服务端故障(5xx/过载,可稍后重试)
	LlmCategoryServer LlmErrorCategory = "server"
	// LlmCategoryContentFilter 输入/输出被内容安全策略拦截
	LlmCategoryContentFilter LlmErrorCategory = "content_filter"
	// LlmCategoryUnknown 无法归类
	LlmCategoryUnknown LlmErrorCategory = "unknown"
)

const (
	llmStageResponse = "response" // 首字节前:HTTP 非 2xx 或响应体携带 error
	llmStageStream   = "stream"   // 流中:SSE 已建连(HTTP 200)后发生错误
)

const codeStreamBase = basic.ConstErrorMax + 2700
const (
	codeLlmTransportError = codeStreamBase + 1 + iota
	codeLlmStreamError
)

// LlmStatusError 结构化的 LLM 上游错误。
// 统一携带 HTTP 状态码、上游 request_id 与语义化类别,是 ParseLlmResponseError 与流中错误的
// 返回类型,便于上层按类别映射用户文案与 HTTP 状态。
type LlmStatusError struct {
	pException.ErrorDescription

	StatusCode int              // 上游 HTTP 状态码;流中错误(连接建立时已 200)记 200
	RequestID  string           // 上游 request_id(响应头),便于排障/提交工单
	Stage      string           // 错误发生阶段:response(首字节前)/ stream(流中)
	Category   LlmErrorCategory // 语义化类别

	api llmAPIError // 解析出的错误体(包内;可能为空)
	raw string      // 原始响应体(无法结构化解析时的兜底)
}

// HasLlmError 判定 JSON 载荷中是否携带错误体(error 字段存在且非 null)。
// 供调用方判断上游是否返回错误,而无需感知错误体的内部结构。
func HasLlmError(payload json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(payload))
	return trimmed != "" && trimmed != "null"
}

// NewLlmResponseError 构造"HTTP 200 但响应体携带 error 字段"的结构化错误(同步路径)。
// payload 为错误体原始 JSON(即 error 字段的内容),由本包负责解析与分类。
func NewLlmResponseError(payload json.RawMessage) *LlmStatusError {
	api := parseLlmErrorBody(string(payload))
	return &LlmStatusError{
		ErrorDescription: pException.ErrorDescription{
			ErrId: codeLlmTransportError,
		},
		StatusCode: http.StatusOK,
		Stage:      llmStageResponse,
		Category:   classifyLlmError(http.StatusOK, api),
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
		ErrorDescription: pException.ErrorDescription{
			ErrId: codeLlmTransportError,
		},
		Stage:    llmStageResponse,
		Category: LlmCategoryServer,
		api:      llmAPIError{Message: detail, Type: "transport_error"},
		raw:      detail,
	}
}

// NewLlmStreamError 构造流中错误(连接已建立,HTTP 200)的结构化错误。
// payload 为错误载荷原始 JSON/文本:能解析出结构则按结构分类,否则回退为纯文本消息。
func NewLlmStreamError(payload json.RawMessage, requestID string) *LlmStatusError {
	api := parseLlmErrorBody(string(payload))
	if api.Message == "" && api.Code == "" && api.Type == "" {
		api = llmAPIError{Message: strings.TrimSpace(string(payload))}
	}
	return &LlmStatusError{
		ErrorDescription: pException.ErrorDescription{
			ErrId: codeLlmStreamError,
		},
		StatusCode: http.StatusOK,
		RequestID:  requestID,
		Stage:      llmStageStream,
		Category:   classifyLlmError(http.StatusOK, api),
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
		ErrorDescription: pException.ErrorDescription{
			ErrId: codeLlmStreamError,
		},
		RequestID: requestID,
		Stage:     llmStageStream,
		Category:  LlmCategoryServer,
		api:       llmAPIError{Message: detail, Type: "transport_error"},
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

func (e *LlmStatusError) Is(target error) bool {
	if ed, ok := errors.AsType[*LlmStatusError](target); ok {
		return ed.ErrId == e.ErrId
	}
	return false
}

// ======================== 3. 传输层解析与重试判定 ========================

// ParseLlmResponseError 解析上游非 2xx 响应,返回带 HTTP 状态码、request_id 与语义化类别的
// 结构化错误(*LlmStatusError),供上层按类别映射用户文案。
func ParseLlmResponseError(resp *http.Response, readBytes int64) error {
	statusCode := 0
	requestID := ""
	if resp != nil {
		statusCode = resp.StatusCode
		requestID = LlmRequestID(resp.Header)
	}

	raw := ""
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

// requestIDHeaders 上游 request_id 可能出现的响应头(按优先级;http.Header.Get 大小写不敏感)。
var requestIDHeaders = []string{
	"X-Request-Id",
	"X-Dashscope-Request-Id",
	"Request-Id",
	"X-Trace-Id",
	"Trace-Id",
}

// LlmRequestID 从响应头读取上游 request_id(便于排障/提交工单);无则返回空串。
func LlmRequestID(header http.Header) string {
	if header == nil {
		return ""
	}
	for _, h := range requestIDHeaders {
		if v := strings.TrimSpace(header.Get(h)); v != "" {
			return v
		}
	}
	return ""
}

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
