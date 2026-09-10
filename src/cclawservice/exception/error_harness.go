/**
* harness 调用侧的错误(runtime client:构造请求/解析响应/空响应等),以及"请求被取消"哨兵。
*
* 说明:上游 LLM 协议类型与解析/分类基础设施在 exception/error_llm_stream.go
*
* RuntimeClientKind 属**内部成因**类错误:在聊天链路上会被 harness.LlmError 归一化为
* ChatLLMKind(502 + 可读文案),因此不对外暴露业务码(Code=0)。
**/

package exception

import (
	"errors"
	"net/http"

	"my.favorite/cclawservice/apperr"
	"my.favorite/platformutility/webbase/middleware/basic"
)

// codeHarnessBase harness(含聊天域)业务码基址:ConstErrorMax + 2500 = 4500。
const codeHarnessBase = basic.ConstErrorMax + 2500

// codeRuntimeClientError harness 调用侧内部错误(域内单独占 1 个码位 → 4501)。
const codeRuntimeClientError = codeHarnessBase + 1

// 聊天域业务码:自 +2 起按声明顺序连续分配 → 4502..4517。
//
// 这里用 iota 而不是逐个手写偏移:手写偏移易出现"两类错误写成同一个偏移"
// (业务码重复 → 启动期 panic),而 iota 让重复在语法上不可能;顺序变化导致的
// 数值漂移由 exception/error_codes_test.go 的契约测试兜底。
const (
	codeChatParameter = codeHarnessBase + 2 + iota
	codeChatAgentNotFound
	codeChatLLM
	codeChatCanceled
	codeChatNotActive
	codeChatBusy
	codeChatServerBusy
	codeChatToolLoop
	codeChatLLMQuota
	codeChatRunTimeout
	codeChatLLMAuth
	codeChatLLMForbidden
	codeChatLLMNotFound
	codeChatLLMInvalidRequest
	codeChatLLMRateLimit
	codeChatLLMContentFilter
)

// RuntimeClientKind harness 调用上游失败(构造请求/解析响应/空响应等)。
var RuntimeClientKind = apperr.Register(apperr.Kind{
	Name:       "harness.runtime_client",
	Code:       codeRuntimeClientError,
	HTTPStatus: http.StatusInternalServerError,
	Msg:        basic.ConstInternalServerErrorMsg,
})

// RuntimeClientErr 运行时客户端错误的哨兵(errors.Is 用)。
var RuntimeClientErr = RuntimeClientKind.Sentinel()

func RuntimeClientErrEx(errTxt string) *apperr.Error {
	return RuntimeClientKind.New(errTxt)
}

func RuntimeClientErrEx2(errTxt string, a ...interface{}) *apperr.Error {
	return RuntimeClientKind.Newf(errTxt, a...)
}

// 聊天错误的 Kind 表。HTTPStatus 与既有 controller 行为保持一致(前端契约不变)。
var (
	// ChatParameterKind 聊天请求参数错误(请求体缺失/解析失败)。HTTP 400。
	ChatParameterKind = apperr.Register(apperr.Kind{
		Name:       "chat.parameter",
		Code:       codeChatParameter,
		HTTPStatus: http.StatusBadRequest,
		Msg:        "聊天参数错误",
	})

	// ChatAgentNotFoundKind 聊天目标 Agent 不存在或已禁用。HTTP 404。
	ChatAgentNotFoundKind = apperr.Register(apperr.Kind{
		Name:       "chat.agent_not_found",
		Code:       codeChatAgentNotFound,
		HTTPStatus: http.StatusNotFound,
		Msg:        "聊天目标Agent不存在或已禁用",
	})

	// ChatServerBusyKind 本实例整体繁忙(全局聊天闸/流式闸已满,快速拒绝)。HTTP 503。
	ChatServerBusyKind = apperr.Register(apperr.Kind{
		Name:       "chat.server_busy",
		Code:       codeChatServerBusy,
		HTTPStatus: http.StatusServiceUnavailable,
		Msg:        "服务繁忙,请稍后再试",
		Retryable:  true,
	})

	// ChatLLMKind 上游智能体引擎不可用(未归类的引擎故障/瞬时故障)。HTTP 502。
	ChatLLMKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm",
		Code:       codeChatLLM,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "智能体引擎暂时不可用,请稍后再试",
		Retryable:  true,
	})

	// ChatLLMQuotaKind 上游额度耗尽/欠费:需充值或提升配额,与瞬时故障区分。HTTP 502。
	ChatLLMQuotaKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.quota",
		Code:       codeChatLLMQuota,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "模型服务额度不足或已耗尽(可能欠费或套餐用尽),请充值/提升配额后重试",
	})

	// ChatRunTimeoutKind 应用级任务超时(工具循环 + 生成的总时长超出预算)。HTTP 504。
	ChatRunTimeoutKind = apperr.Register(apperr.Kind{
		Name:       "chat.run_timeout",
		Code:       codeChatRunTimeout,
		HTTPStatus: http.StatusGatewayTimeout,
		Msg:        "任务执行超时,请缩小范围或分批执行后重试",
	})

	// ChatToolLoopKind 工具调用循环超过最大轮次仍未产出最终答复。
	// 与 ChatLLMKind 区分:不是引擎不可用,而是任务规模超出单回合工具循环预算。HTTP 502。
	ChatToolLoopKind = apperr.Register(apperr.Kind{
		Name:       "chat.tool_loop",
		Code:       codeChatToolLoop,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "任务步骤过多未能完成,请缩小范围或分批执行后重试",
	})

	// ChatCanceledKind 请求上下文被取消(客户端断开/服务端主动中断)。
	// 静默:响应已无接收方,调用方不应再向前端输出任何内容。
	ChatCanceledKind = apperr.Register(apperr.Kind{
		Name:   "chat.canceled",
		Code:   codeChatCanceled,
		Msg:    "请求已被取消",
		Silent: true,
	})

	// ChatNotActiveKind 中断请求时目标会话没有进行中的聊天。HTTP 400。
	ChatNotActiveKind = apperr.Register(apperr.Kind{
		Name:       "chat.not_active",
		Code:       codeChatNotActive,
		HTTPStatus: http.StatusBadRequest,
		Msg:        "当前会话没有进行中的聊天",
	})

	// ChatBusyKind 目标 Agent 已达并发上限。HTTP 503。
	ChatBusyKind = apperr.Register(apperr.Kind{
		Name:       "chat.busy",
		Code:       codeChatBusy,
		HTTPStatus: http.StatusServiceUnavailable,
		Msg:        "该智能体当前繁忙,请稍后再试",
		Retryable:  true,
	})

	// ChatLLMAuthKind 上游鉴权失败(API Key 无效或未配置),属可自行修正的配置类错误。HTTP 502。
	ChatLLMAuthKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.auth",
		Code:       codeChatLLMAuth,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "模型服务鉴权失败(API Key 无效或未配置),请检查模型密钥配置后重试",
	})

	// ChatLLMForbiddenKind 无权访问该模型(未开通/未授权/已下线),属配置类错误。HTTP 502。
	ChatLLMForbiddenKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.forbidden",
		Code:       codeChatLLMForbidden,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "无权访问该模型(未开通或未授权),请在模型服务侧开通或授权后重试",
	})

	// ChatLLMNotFoundKind 模型不存在或名称配置错误,属配置类错误。HTTP 502。
	ChatLLMNotFoundKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.not_found",
		Code:       codeChatLLMNotFound,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "模型不存在或名称配置错误,请检查模型名(model)配置后重试",
	})

	// ChatLLMInvalidRequestKind 请求被模型服务拒绝(参数/上下文不合法),属配置类错误。HTTP 502。
	ChatLLMInvalidRequestKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.invalid_request",
		Code:       codeChatLLMInvalidRequest,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "请求被模型服务拒绝(参数或上下文不合法),请检查模型与上下文配置后重试",
	})

	// ChatLLMRateLimitKind 触发上游限流:可稍后重试。HTTP 429。
	ChatLLMRateLimitKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.rate_limit",
		Code:       codeChatLLMRateLimit,
		HTTPStatus: http.StatusTooManyRequests,
		Msg:        "模型服务请求过于频繁,请稍后重试",
		Retryable:  true,
	})

	// ChatLLMContentFilterKind 输入/输出被模型内容安全策略拦截(需修改输入)。HTTP 502。
	ChatLLMContentFilterKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.content_filter",
		Code:       codeChatLLMContentFilter,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "内容未通过模型安全审核,请修改输入后重试",
	})
)

// GetChatErrorUserMsg 取错误的用户文案(SSE error 事件用):优先错误自带 Kind 的文案,
// 非应用错误回退到通用的"引擎暂时不可用"提示。
func GetChatErrorUserMsg(err error) string {
	if msg := apperr.UserMsg(err); msg != "" {
		return msg
	}
	return ChatLLMKind.Msg
}

// ErrRequestCanceled 表示请求上下文被取消(客户端断开/超时),而非上游故障。
// 上层可用 errors.Is(err, ErrRequestCanceled) 区分"客户端主动取消"与"引擎异常"。
var ErrRequestCanceled = errors.New("runtime client: request context canceled")
