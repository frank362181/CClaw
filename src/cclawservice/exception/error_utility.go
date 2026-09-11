/**
* 业务错误映射层:把上游协议错误 / 底层平台错误映射为业务 Kind,并统一聊天链路的错误归一化。
*
* 分工:
*   - 上游协议细节(错误体解析、语义化分类、request_id、重试判定)→ llmproto 包;
*   - 业务错误定义(码/HTTP 状态/文案)→ 本包各 error_*.go 的 Kind 表;
*   - 本文件只负责"协议错误 → 业务 Kind"的映射与聊天链路归一化(LlmError)。
**/

package exception

import (
	"errors"

	"my.favorite/cclawservice/apperr"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

// loginKinds 登录/鉴权域 Kind 集合。
//
// 供 MapLoginError 与 controller 判定"该错误是否可直接对客户端呈现":
// 只有登录域 Kind 自带对客户端的呈现语义,其它域的(或平台)错误在登录链路上
// 一律回落为通用内部错误,避免内部文案/业务码外泄。
var loginKinds = map[*apperr.Kind]struct{}{
	UserUnAuthorizedKind:    {},
	LoginParameterKind:      {},
	UserPasswordUnmatchKind: {},
	NonTokenKind:            {},
	InvalidTokenKind:        {},
}

// IsLoginKind 判定 Kind 是否属于登录/鉴权域。
func IsLoginKind(kind *apperr.Kind) bool {
	if kind == nil {
		return false
	}
	_, ok := loginKinds[kind]
	return ok
}

// MapLoginError 把登录链路的底层错误映射为登录域业务错误,使 controller 只处理业务错误
// (与 MapStartupFileError 同一模式)。
//   - 参数为空(平台 ParameterEmptyError)→ 登录参数错误(400);
//   - 凭据不匹配(平台 UnMatchError)→ 用户名或密码错误(沿用 200 + 业务码的既有约定);
//   - 其余(含 service 层已 Kind 化的错误与内部故障)原样返回,由 controller 决定兜底。
//
// 注意:登录失败不得区分"账号不存在/被禁用",否则会形成账号枚举通道。
func MapLoginError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, exception.ParameterEmptyError):
		return LoginParameterKind.WrapF(err, "login: parameter empty")
	case errors.Is(err, exception.UnMatchError):
		return UserPasswordUnmatchKind.WrapF(err, "login: credential unmatch")
	default:
		return err
	}
}

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

// chatKinds 聊天域 Kind 集合。
//
// 供 LlmError 判定"已是聊天业务错误"而直接透传:只有聊天域错误自带对前端的呈现语义;
// 其它域(agent/login/database/harness)的应用错误进入聊天链路时仍应归一化为 ChatLLMKind,
// 避免内部错误的业务码/文案泄漏到聊天响应里。
var chatKinds = map[*apperr.Kind]struct{}{
	ChatParameterKind:         {},
	ChatAgentNotFoundKind:     {},
	ChatServerBusyKind:        {},
	ChatLLMKind:               {},
	ChatLLMQuotaKind:          {},
	ChatRunTimeoutKind:        {},
	ChatToolLoopKind:          {},
	ChatCanceledKind:          {},
	ChatNotActiveKind:         {},
	ChatBusyKind:              {},
	ChatLLMAuthKind:           {},
	ChatLLMForbiddenKind:      {},
	ChatLLMNotFoundKind:       {},
	ChatLLMInvalidRequestKind: {},
	ChatLLMRateLimitKind:      {},
	ChatLLMContentFilterKind:  {},
}

// IsChatKind 判定 Kind 是否属于聊天域(供 LlmError 与调用方判断是否可直接透传)。
func IsChatKind(kind *apperr.Kind) bool {
	if kind == nil {
		return false
	}
	_, ok := chatKinds[kind]
	return ok
}

// llmCategoryKinds 上游结构化错误类别 → 聊天错误 Kind 的唯一映射表。
// 增删上游类别时只需改这里,文案与 HTTP 状态自动跟随 Kind 定义。
var llmCategoryKinds = map[llmproto.LlmErrorCategory]*apperr.Kind{
	llmproto.LlmCategoryAuth:           ChatLLMAuthKind,
	llmproto.LlmCategoryForbidden:      ChatLLMForbiddenKind,
	llmproto.LlmCategoryNotFound:       ChatLLMNotFoundKind,
	llmproto.LlmCategoryInvalidRequest: ChatLLMInvalidRequestKind,
	llmproto.LlmCategoryRateLimit:      ChatLLMRateLimitKind,
	llmproto.LlmCategoryContentFilter:  ChatLLMContentFilterKind,
	llmproto.LlmCategoryQuota:          ChatLLMQuotaKind,
	// server/unknown:临时性故障,沿用"引擎暂时不可用"文案
	llmproto.LlmCategoryServer:  ChatLLMKind,
	llmproto.LlmCategoryUnknown: ChatLLMKind,
}

// KindOfLlmCategory 返回上游类别对应的聊天错误 Kind(未知类别回退到 ChatLLMKind)。
func KindOfLlmCategory(category llmproto.LlmErrorCategory) *apperr.Kind {
	if kind, ok := llmCategoryKinds[category]; ok {
		return kind
	}
	return ChatLLMKind
}

// MapLlmStatusError 把结构化上游错误按语义化类别映射为聊天业务错误。
// 类别→Kind 的对应关系集中在 llmCategoryKinds 一张表里,
// 用户文案与 HTTP 状态由 Kind 定义提供,调用方不再单独维护映射。
func MapLlmStatusError(e *llmproto.LlmStatusError, logPrefix string) error {
	// 挂上底层原因(cause),保留 request_id/状态码等排障信息可被 errors.Is/As 命中。
	return KindOfLlmCategory(e.Category).WrapF(e, "%s:%s", logPrefix, e.Error())
}

// LlmError 统一转换 LLM 调用错误:客户端取消 → ChatCanceledKind(静默),其余按语义映射。
//   - 已是应用错误(带语义的聊天业务错误,如工具循环/额度/运行超时)原样透传,
//     避免二次包装丢失原始细节与类别;
//   - 结构化上游错误(*llmproto.LlmStatusError)按 Category 映射到对应 Kind;
//   - 裸错误若命中额度特征则以 ChatLLMQuotaKind 兜底,否则统一归入 ChatLLMKind。
func LlmError(err error, logPrefix string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, llmproto.ErrRequestCanceled) {
		logging.LogDebugF("%s: request canceled by client", logPrefix)
		return ChatCanceledKind.New("chat request canceled")
	}
	logging.LogErrorF("%s:%s", logPrefix, err.Error())
	// 已是聊天域应用错误 → 原样透传(保留类别与细节)。
	// 其它域的应用错误(agent/login/database/harness)继续按上游故障归一化,
	// 否则内部错误的业务码/文案会直接泄漏到聊天响应。
	if kind := apperr.Of(err); IsChatKind(kind) {
		return err
	}
	if statusErr, ok := errors.AsType[*llmproto.LlmStatusError](err); ok {
		return MapLlmStatusError(statusErr, logPrefix)
	}
	if llmproto.IsQuotaError(err) {
		return ChatLLMQuotaKind.Newf("%s:%s", logPrefix, err.Error())
	}
	return ChatLLMKind.Newf("%s:%s", logPrefix, err.Error())
}
