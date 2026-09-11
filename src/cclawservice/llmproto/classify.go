/**
* 上游错误体的语义化分类:特征词表 + 分类器 + 错误体解析 + 类别枚举。
*
* 纯协议层:只回答"上游说了什么",不含任何业务码/用户文案/HTTP 响应语义。
* 类别 → 业务 Kind 的映射在 exception/error_utility.go(llmCategoryKinds)。
**/

package llmproto

import (
	"encoding/json"
	"net/http"
	"strings"

	pCommon "my.favorite/platformutility/common"
)

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

// ClassifyLlmError 依据 HTTP 状态码与上游错误体判定语义化类别。
// 优先级:内容安全 > 额度 > HTTP 状态码 > 文案兜底(流中错误无状态码时)。
func ClassifyLlmError(statusCode int, api LlmAPIError) LlmErrorCategory {
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

// IsQuotaError 判定错误是否为"LLM 额度耗尽/欠费"(裸错误兜底;结构化错误请用 LlmStatusError.Category)。
// 上游可能用非标准状态码返回欠费(如 dashscope/深度),因此需要文案特征兜底。
func IsQuotaError(err error) bool {
	if err == nil {
		return false
	}
	return pCommon.ContainsString(strings.ToLower(err.Error()), quotaSignals)
}
