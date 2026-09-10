package exception

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	cclawcommon "my.favorite/cclawservice/common"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

// MapStartupFileError 把启动文件相关的仓库层/平台错误转换为 agent 管理业务错误,
// 使 controller 只处理业务错误,不感知底层平台异常。
func MapStartupFileError(err error) error {
	switch {
	case errors.Is(err, exception.FilePathError):
		// 启动文件不在白名单/路径越界:视为参数错误,不暴露具体路径
		return exception.ParameterEmptyErrorEx("illegal startup file")
	case errors.Is(err, exception.ParameterEmptyError):
		return AgentInternalErrEx("agent service internal error")
	default:
		return err
	}
}

// MapLlmStatusError 把结构化上游错误按语义化类别映射为聊天业务错误,
// 供控制器选择用户文案与 HTTP 状态(配置类错误不再笼统提示为"引擎暂时不可用")。
func MapLlmStatusError(e *LlmStatusError, logPrefix string) error {
	detail := e.Error()
	switch e.Category {
	case LlmCategoryAuth:
		return ChatLLMAuthErrEx2("%s:%s", logPrefix, detail)
	case LlmCategoryForbidden:
		return ChatLLMForbiddenErrEx2("%s:%s", logPrefix, detail)
	case LlmCategoryNotFound:
		return ChatLLMNotFoundErrEx2("%s:%s", logPrefix, detail)
	case LlmCategoryInvalidRequest:
		return ChatLLMInvalidRequestErrEx2("%s:%s", logPrefix, detail)
	case LlmCategoryRateLimit:
		return ChatLLMRateLimitErrEx2("%s:%s", logPrefix, detail)
	case LlmCategoryContentFilter:
		return ChatLLMContentFilterErrEx2("%s:%s", logPrefix, detail)
	case LlmCategoryQuota:
		return ChatLLMQuotaErrEx2("%s:%s", logPrefix, detail)
	default:
		// server/unknown:临时性故障,沿用"引擎暂时不可用"文案
		return ChatLLMErrEx2("%s:%s", logPrefix, detail)
	}
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

// ClassifyLlmError 依据 HTTP 状态码与上游错误体判定语义化类别。
// 优先级:内容安全 > 额度 > HTTP 状态码 > 文案兜底(流中错误无状态码时)。
func ClassifyLlmError(statusCode int, api LlmAPIError) LlmErrorCategory {
	code := strings.ToLower(strings.TrimSpace(api.Code))
	typ := strings.ToLower(strings.TrimSpace(api.Type))
	msg := strings.ToLower(api.Message)
	head := code + " " + typ
	all := head + " " + msg

	// 1. 内容安全/合规:与状态码无关(常见 400,或流中 finish_reason=content_filter)
	if common.ContainsString(all, contentFilterSignals) {
		return LlmCategoryContentFilter
	}
	// 2. 额度/欠费:可能以 400/402/429 返回,须先于通用限流判定
	if common.ContainsString(head, quotaSignals) || common.ContainsString(msg, quotaSignals) {
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
	if common.ContainsString(all, rateLimitSignals) {
		return LlmCategoryRateLimit
	}
	if common.ContainsString(all, authSignals) {
		return LlmCategoryAuth
	}
	if common.ContainsString(all, forbiddenSignals) {
		return LlmCategoryForbidden
	}
	if common.ContainsString(all, notFoundSignals) {
		return LlmCategoryNotFound
	}
	if common.ContainsString(all, serverSignals) {
		return LlmCategoryServer
	}
	if common.ContainsString(all, invalidRequestSignals) {
		return LlmCategoryInvalidRequest
	}
	return LlmCategoryUnknown
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

// isQuotaError 判定错误是否为"LLM 额度耗尽/欠费"(裸错误兜底;结构化错误请用 LlmStatusError.Category)。
func isQuotaError(err error) bool {
	if err == nil {
		return false
	}
	return common.ContainsString(strings.ToLower(err.Error()), quotaSignals)
}

// LlmError 统一转换 LLM 调用错误:客户端取消 → ChatCanceledErr(静默),其余按语义映射。
// 已是带语义的聊天业务错误(工具循环/额度/运行超时)原样透传;
// 结构化上游错误(*LlmStatusError)按类别映射,把配置类错误与瞬时故障分开;
// 裸错误若命中额度特征则以 ChatLLMQuotaErr 兜底,否则统一 ChatLLMErr。
func LlmError(err error, logPrefix string) error {
	if errors.Is(err, ErrRequestCanceled) {
		logging.LogDebugF("%s: request canceled by client", logPrefix)
		return ChatCanceledErrEx("chat request canceled")
	}
	logging.LogErrorF("%s:%s", logPrefix, err.Error())
	if _, ok := errors.AsType[*ErrorChatToolLoop](err); ok {
		return err
	}
	if _, ok := errors.AsType[*ErrorChatLLMQuota](err); ok {
		return err
	}
	if _, ok := errors.AsType[*ErrorChatRunTimeout](err); ok {
		return err
	}
	if statusErr, ok := errors.AsType[*LlmStatusError](err); ok {
		return MapLlmStatusError(statusErr, logPrefix)
	}

	if isQuotaError(err) {
		return ChatLLMQuotaErrEx2("%s:%s", logPrefix, err.Error())
	}
	return ChatLLMErrEx2("%s:%s", logPrefix, err.Error())
}

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

	apiErr := ParseLlmErrorBody(raw)
	return &LlmStatusError{
		StatusCode: statusCode,
		RequestID:  requestID,
		Stage:      llmStageResponse,
		Category:   ClassifyLlmError(statusCode, apiErr),
		API:        apiErr,
		Raw:        raw,
	}
}

// ParseLlmErrorBody 解析上游错误体:优先 OpenAI 风格 {"error":{...}},回退扁平结构 {message,type,code,param}。
// 导出供流式 SSE 解析复用(如 event: error 形态)。
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

// GetUserMsgFromError 把聊天业务错误映射为展示给前端的错误文案(SSE error 事件用)。
// 让"工具循环超限"等可引导的任务错误给出真实原因,而不是笼统的"引擎不可用"。
func GetUserMsgFromError(err error) string {
	switch {
	case errors.Is(err, ChatRunTimeoutErr):
		return cclawcommon.ConstErrorChatRunTimeoutMsg
	case errors.Is(err, ChatLLMQuotaErr):
		return cclawcommon.ConstErrorChatLLMQuotaMsg
	case errors.Is(err, ChatToolLoopErr):
		return cclawcommon.ConstErrorChatToolLoopMsg
	case errors.Is(err, ChatLLMAuthErr):
		return cclawcommon.ConstErrorChatLLMAuthMsg
	case errors.Is(err, ChatLLMForbiddenErr):
		return cclawcommon.ConstErrorChatLLMForbiddenMsg
	case errors.Is(err, ChatLLMNotFoundErr):
		return cclawcommon.ConstErrorChatLLMNotFoundMsg
	case errors.Is(err, ChatLLMInvalidRequestErr):
		return cclawcommon.ConstErrorChatLLMInvalidRequestMsg
	case errors.Is(err, ChatLLMRateLimitErr):
		return cclawcommon.ConstErrorChatLLMRateLimitMsg
	case errors.Is(err, ChatLLMContentFilterErr):
		return cclawcommon.ConstErrorChatLLMContentFilterMsg
	case errors.Is(err, ChatLLMErr):
		return cclawcommon.ConstErrorChatLLMMsg
	default:
		return cclawcommon.ConstErrorChatLLMMsg
	}
}
