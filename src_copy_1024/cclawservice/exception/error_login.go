/*
* 用户登录 / 会话鉴权的业务错误定义(单一事实来源)。
*
* 每类错误只声明一行 Kind 并注册到 apperr 统一管理;业务码为前端契约(前端按 code 分支),
* 基址 ConstErrorMax + 1000 = 3000,域内自 +1 起连续分配(3001..3005),
* 数值由 error_codes_test.go 锁定。
*
* 账号不存在 / 账号被禁用**不单独区分**:登录失败统一回"用户名或密码错误",避免账号枚举。
*
* 兼容层:UserPasswordUnmatchErr/TokenInvalidErr 及其工厂保持原签名不变。
 */

package exception

import (
	"net/http"

	"my.favorite/cclawservice/apperr"
)

const (
	codeUserUnAuthorized        = codeErrorUserBase + 1 + iota // 用户未认证
	codeLoginParameter                                         // 登录参数错误
	codeUserNamePasswordUnmatch                                // 用户名或密码错误
	codeNonToken                                               // 未携带 Token
	codeInvalidToken                                           // Token 无效
)

var (
	// UserUnAuthorizedKind 用户未认证(缺少登录态)。
	UserUnAuthorizedKind = apperr.Register(apperr.Kind{
		Name:       "login.un_authorized",
		Code:       codeUserUnAuthorized,
		HTTPStatus: http.StatusUnauthorized,
		Msg:        "用户未认证,请登录",
	})

	// LoginParameterKind 登录参数错误(请求体不合法)。
	LoginParameterKind = apperr.Register(apperr.Kind{
		Name:       "login.parameter",
		Code:       codeLoginParameter,
		HTTPStatus: http.StatusBadRequest,
		Msg:        "登录参数错误",
	})

	// UserPasswordUnmatchKind 用户名或密码不匹配。
	// 账号不存在/被禁用也统一由它应答(防账号枚举);沿用 200 + 业务码的既有约定。
	UserPasswordUnmatchKind = apperr.Register(apperr.Kind{
		Name:       "login.user_password_unmatch",
		Code:       codeUserNamePasswordUnmatch,
		HTTPStatus: http.StatusOK,
		Msg:        "用户名或密码错误",
	})

	// NonTokenKind 请求未携带 Token。
	NonTokenKind = apperr.Register(apperr.Kind{
		Name:       "login.non_token",
		Code:       codeNonToken,
		HTTPStatus: http.StatusUnauthorized,
		Msg:        "Token不存在,请登录",
	})

	// InvalidTokenKind Token 无效/过期/已被替换。
	InvalidTokenKind = apperr.Register(apperr.Kind{
		Name:       "login.invalid_token",
		Code:       codeInvalidToken,
		HTTPStatus: http.StatusUnauthorized,
		Msg:        "Token无效,请登录",
	})
)

// UserPasswordUnmatchErr 用户名密码不匹配的哨兵(errors.Is 用)。
var UserPasswordUnmatchErr = UserPasswordUnmatchKind.Sentinel()

func UserPasswordUnmatchEx(errTxt string) *apperr.Error {
	return UserPasswordUnmatchKind.New(errTxt)
}

func UserPasswordUnmatchEx2(errTxt string, a ...interface{}) *apperr.Error {
	return UserPasswordUnmatchKind.Newf(errTxt, a...)
}

// TokenInvalidErr Token 无效的哨兵(errors.Is 用)。
var TokenInvalidErr = InvalidTokenKind.Sentinel()

func TokenInvalidErrEx(errTxt string) *apperr.Error {
	return InvalidTokenKind.New(errTxt)
}

func TokenInvalidErrEx2(errTxt string, a ...interface{}) *apperr.Error {
	return InvalidTokenKind.Newf(errTxt, a...)
}
