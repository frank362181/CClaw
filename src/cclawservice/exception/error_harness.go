/**
* 聊天域业务错误 Kind 表(业务码 4601..4616)+ 用户文案取值。
*
* HTTPStatus 与既有 controller 行为保持一致(前端契约不变)。
* 码值布局:4600 为本域基址(ConstErrorMax + 2600),域内自 +1 连续分配 → 4601..4617;
* harness.runtime_client 独占 4501(见 error_runtime.go) —— 由 exception/error_codes_test.go 的契约测试锁定。
**/

package exception

import (
	"net/http"

	"my.favorite/cclawservice/apperr"
)

const (
	codeChatParameter = codeErrorHarnessChatBase + 1 + iota
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
	codeAgentFileUnavailable
	codeSandboxExecutorError
	codeChatToolLoopGuard
	codeChatToolLoopExceeded
	codeChatToolExecutionMisMatched
	codeChatResponseTooLarge
)

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
		Notify:     true,
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
		Notify:     true,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "模型服务额度不足或已耗尽(可能欠费或套餐用尽),请充值/提升配额后重试",
	})

	// ChatRunTimeoutKind 应用级任务超时(工具循环 + 生成的总时长超出预算)。HTTP 504。
	ChatRunTimeoutKind = apperr.Register(apperr.Kind{
		Name:       "chat.run_timeout",
		Code:       codeChatRunTimeout,
		Notify:     true,
		HTTPStatus: http.StatusGatewayTimeout,
		Msg:        "任务执行超时,请缩小范围或分批执行后重试",
	})

	// ChatToolLoopKind 工具调用循环超过最大轮次仍未产出最终答复。
	// 与 ChatLLMKind 区分:不是引擎不可用,而是任务规模超出单回合工具循环预算。HTTP 502。
	ChatToolLoopKind = apperr.Register(apperr.Kind{
		Name:       "chat.tool_loop",
		Code:       codeChatToolLoop,
		Notify:     true,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "任务步骤过多未能完成,请缩小范围或分批执行后重试",
	})

	// ChatToolLoopGuardKind 工具死循环守卫命中:模型在被提示"换策略/停止重试"后仍继续请求工具。
	// 与 ChatToolLoopKind 区分:不是"任务步骤太多",而是同一批调用在重复
	// (完全相同的调用 / 同一工具连打 / 二元交替调用)。HTTP 502(与工具循环同族)。
	ChatToolLoopGuardKind = apperr.Register(apperr.Kind{
		Name:       "chat.tool_loop_guard",
		Code:       codeChatToolLoopGuard,
		Notify:     true,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "检测到重复调用工具已中止,请换一种方式描述需求或缩小范围",
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
		Notify:     true,
		HTTPStatus: http.StatusServiceUnavailable,
		Msg:        "该智能体当前繁忙,请稍后再试",
		Retryable:  true,
	})

	// ChatLLMAuthKind 上游鉴权失败(API Key 无效或未配置),属可自行修正的配置类错误。HTTP 502。
	ChatLLMAuthKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.auth",
		Code:       codeChatLLMAuth,
		Notify:     true,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "模型服务鉴权失败(API Key 无效或未配置),请检查模型密钥配置后重试",
	})

	// ChatLLMForbiddenKind 无权访问该模型(未开通/未授权/已下线),属配置类错误。HTTP 502。
	ChatLLMForbiddenKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.forbidden",
		Code:       codeChatLLMForbidden,
		Notify:     true,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "无权访问该模型(未开通或未授权),请在模型服务侧开通或授权后重试",
	})

	// ChatLLMNotFoundKind 模型不存在或名称配置错误,属配置类错误。HTTP 502。
	ChatLLMNotFoundKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.not_found",
		Code:       codeChatLLMNotFound,
		Notify:     true,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "模型不存在或名称配置错误,请检查模型名(model)配置后重试",
	})

	// ChatLLMInvalidRequestKind 请求被模型服务拒绝(参数/上下文不合法),属配置类错误。HTTP 502。
	ChatLLMInvalidRequestKind = apperr.Register(apperr.Kind{
		Name:       "chat.llm.invalid_request",
		Code:       codeChatLLMInvalidRequest,
		Notify:     true,
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
		Notify:     true,
		HTTPStatus: http.StatusBadGateway,
		Msg:        "内容未通过模型安全审核,请修改输入后重试",
	})

	AgentFileUnavailableKind = apperr.Register(apperr.Kind{
		Name:       "harness.agent_file_unavailable",
		Code:       codeAgentFileUnavailable,
		HTTPStatus: http.StatusOK,
		Msg:        "启动文件不可用(文件过大或读取失败)",
	})

	SandboxExecutorKind = apperr.Register(apperr.Kind{
		Name:       "harness.sandbox_executor",
		Code:       codeSandboxExecutorError,
		HTTPStatus: http.StatusOK,
		Msg:        "沙箱执行环境",
	})

	ToolLoopExceededKind = apperr.Register(apperr.Kind{
		Name:       "harness.tool_loop_exceeded",
		Code:       codeChatToolLoopExceeded,
		HTTPStatus: http.StatusOK,
		Msg:        "工具执行超过上限",
	})

	ToolExecutionMisMatchedKind = apperr.Register(apperr.Kind{
		Name:       "harness.tool_execution_mis_matched",
		Code:       codeChatToolExecutionMisMatched,
		HTTPStatus: http.StatusOK,
		Msg:        "工具调用返回结果不匹配",
	})

	ChatResponseTooLargeKind = apperr.Register(apperr.Kind{
		Name:       "harness.chat_response_too_large",
		Code:       codeChatResponseTooLarge,
		HTTPStatus: http.StatusOK,
		Msg:        "响应包超过指定大小",
		Notify:     true,
	})
)

var SandboxExecutorErr = PermissionDeniedKind.Sentinel()

func SandboxExecutorErrEx(errTxt string) *apperr.Error {
	return SandboxExecutorKind.New(errTxt)
}

func SandboxExecutorErrEx2(errTxt string, a ...interface{}) *apperr.Error {
	return SandboxExecutorKind.Newf(errTxt, a...)
}

var AgentFileUnavailableErr = AgentFileUnavailableKind.Sentinel()

func AgentFileUnavailableErrEx(errTxt string) *apperr.Error {
	return AgentFileUnavailableKind.New(errTxt)
}

func AgentFileUnavailableErrEx2(errTxt string, a ...interface{}) *apperr.Error {
	return AgentFileUnavailableKind.Newf(errTxt, a...)
}

// GetChatErrorUserMsg 取错误的用户文案(SSE error 事件用):优先错误自带 Kind 的文案,
// 非应用错误回退到通用的"引擎暂时不可用"提示。
func GetChatErrorUserMsg(err error) string {
	if msg := apperr.UserMsg(err); msg != "" {
		return msg
	}
	return ChatLLMKind.Msg
}
