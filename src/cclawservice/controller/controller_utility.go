/*
* controller层的工具函数
 */

package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"my.favorite/cclawservice/apperr"
	cclawcommon "my.favorite/cclawservice/common"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/request"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/webbase/middleware/basic"
)

func getAuthentication(c *gin.Context) (*request.LoginSession, bool) {
	tempValue, exists := c.Get(cclawcommon.ContextAuthenticatedUser)
	if !exists {
		respondKindAbort(c, myException.UserUnAuthorizedKind)
		return nil, false
	}
	loginSession, ok := tempValue.(*request.LoginSession)
	if !ok {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return nil, false
	}

	return loginSession, true
}

// ===== Kind 驱动的统一错误响应(controller 层共用)=====

// 平台层错误(参数为空/未匹配)在此归一为聊天语义;其余全部交给错误自带的 Kind,
// 由 Kind 决定 HTTP 状态码 / 业务码 / 用户文案,不再维护"错误 → 状态码"的长 switch。
func respondChatError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, pException.ParameterEmptyError):
		respondKind(c, myException.ChatParameterKind)
	case errors.Is(err, pException.UnMatchError):
		respondKind(c, myException.ChatAgentNotFoundKind)
	default:
		respondErrorKind(c, err)
	}
}

// respondAgentError 业务层的错误表现为 http 正常,但业务层异常。
// 具体呈现(HTTP 状态 / 业务码 / 文案)全部来自错误自带的 Kind;仅平台层错误在此归一为 agent 语义。
func respondAgentError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, pException.ParameterEmptyError):
		respondKind(c, myException.AgentParameterKind)
	case errors.Is(err, pException.UnMatchError):
		respondKind(c, myException.AgentNotFoundKind)
	case errors.Is(err, pException.MatchError):
		respondKind(c, myException.AgentConflictKind)
	default:
		// 带 Kind 的业务错误(工作空间不可用/内部错误等)统一按 Kind 渲染
		if kind := apperr.Of(err); kind != nil {
			respondKind(c, kind)
			return
		}
		basic.GetBaseResponser().Failure(c, http.StatusOK, basic.ConstErrorUnknow, basic.ConstErrorUnknowMsg, nil)
	}
}

// respondErrorKind 按错误自带的 Kind 渲染响应。
// 非应用错误(平台层/未知错误)回退为 500;Silent 类错误(客户端已断开/主动取消)不再输出。
// respondLoginError 登录/鉴权域的统一错误响应:
//   - 登录域 Kind → 按 Kind 渲染业务码/文案/HTTP 状态;
//   - 其它域的应用错误(数据库/运行时等内部故障)→ 通用 500,不外泄内部文案与业务码;
//   - 非应用错误 → 由 respondErrorKind 兜底为通用 500。
func respondLoginError(c *gin.Context, err error) {
	mapped := myException.MapLoginError(err)
	if kind := apperr.Of(mapped); kind != nil && !myException.IsLoginKind(kind) {
		basic.GetBaseResponser().Failure(c, http.StatusInternalServerError,
			basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return
	}
	respondErrorKind(c, mapped)
}

func respondErrorKind(c *gin.Context, err error) {
	kind := apperr.Of(err)
	if kind == nil {
		basic.GetBaseResponser().Failure(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return
	}
	if kind.Silent || kind.HTTPStatus <= 0 {
		// 静默类:响应已无接收方,直接结束(不向前端输出任何内容)。
		return
	}
	respondKind(c, kind)
}

// respondKind 按 Kind 定义输出统一错误响应(HTTP 状态 / 业务码 / 用户文案同源)。
// 业务码为 0 表示"无业务码"的内部错误,此时回退平台通用内部错误码。
func respondKind(c *gin.Context, kind *apperr.Kind) {
	basic.GetBaseResponser().Failure(c, kind.HTTPStatus, getResponseCodeOf(kind), kind.Msg, nil)
}

// respondKindStatus 按 Kind 的业务码/文案输出错误响应,但 HTTP 状态由调用方指定。
// 用于同一业务码在既有约定下使用不同 HTTP 状态的场景(如 agent 参数错误:
// 请求体绑定失败用 400,业务层失败用 200)。
func respondKindStatus(c *gin.Context, httpStatus int, kind *apperr.Kind) {
	basic.GetBaseResponser().Failure(c, httpStatus, getResponseCodeOf(kind), kind.Msg, nil)
}

// respondKindAbort 同 respondKind,但中断后续处理链(认证/参数校验等前置失败用)。
func respondKindAbort(c *gin.Context, kind *apperr.Kind) {
	basic.GetBaseResponser().WriteWithErrorAndAbort(c, kind.HTTPStatus, getResponseCodeOf(kind), kind.Msg, nil)
}

// getResponseCodeOf 取 Kind 的业务码:Code==0(内部错误)回退平台通用内部错误码,
// 避免向前端下发 0(与成功码相同)。
func getResponseCodeOf(kind *apperr.Kind) int {
	if kind.Code != 0 {
		return kind.Code
	}
	return basic.ConstInternalServerError
}
