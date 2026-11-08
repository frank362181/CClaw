/**
* 登录域错误的映射契约:MapLoginError / IsLoginKind。
*
* 重点:
*   - 平台错误(ParameterEmptyError / UnMatchError)必须被映射为登录域 Kind ——
*     否则 controller 只能回落成通用 500,客户端看不到可读原因;
*   - 已 Kind 化的错误必须原样透传(不重复包装);
*   - 登录失败不得区分"账号不存在/被禁用"(防账号枚举),一律 UserPasswordUnmatchKind;
*   - HTTP 状态与业务码锁定:参数错(400/3002)、凭据错(200/3003)。
**/

package exception

import (
	"errors"
	"net/http"
	"testing"

	"my.favorite/platformutility/apperr"
	platformException "my.favorite/platformutility/exception"
)

func TestMapLoginError(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		want  *apperr.Kind
		wantH int
		wantC int
	}{
		{"平台参数为空→登录参数错误(400/3002)", platformException.ParameterEmptyErrorEx("param empty"),
			LoginParameterKind, http.StatusBadRequest, codeLoginParameter},
		{"平台凭据不匹配→用户名或密码错误(200/3003)", platformException.UnMatchErrorEx("unmatch"),
			UserPasswordUnmatchKind, http.StatusOK, codeUserNamePasswordUnmatch},
		{"已 Kind 化(参数)→原样透传", LoginParameterKind.New("empty"),
			LoginParameterKind, http.StatusBadRequest, codeLoginParameter},
		{"已 Kind 化(凭据)→原样透传", UserPasswordUnmatchKind.New("unmatch"),
			UserPasswordUnmatchKind, http.StatusOK, codeUserNamePasswordUnmatch},
		{"Token 无效→原样透传", InvalidTokenKind.New("token empty"),
			InvalidTokenKind, http.StatusUnauthorized, codeInvalidToken},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MapLoginError(c.err)
			kind := apperr.Of(got)
			if kind != c.want {
				t.Fatalf("Kind = %v, 期望 %v", kind, c.want)
			}
			if kind.HTTPStatus != c.wantH {
				t.Errorf("HTTPStatus = %d, 期望 %d", kind.HTTPStatus, c.wantH)
			}
			if kind.Code != c.wantC {
				t.Errorf("Code = %d, 期望 %d", kind.Code, c.wantC)
			}
		})
	}

	// 非应用错误原样返回(由 controller 兜底通用 500),且不应被误判成登录域错误。
	t.Run("普通错误原样返回", func(t *testing.T) {
		plain := errors.New("db connection refused")
		got := MapLoginError(plain)
		if got != plain {
			t.Fatalf("普通错误应原样返回,得到 %v", got)
		}
		if apperr.Of(got) != nil {
			t.Fatalf("普通错误不应带 Kind")
		}
	})

	if MapLoginError(nil) != nil {
		t.Fatalf("nil 应返回 nil")
	}
}

func TestIsLoginKind(t *testing.T) {
	loginKinds := []*apperr.Kind{
		UserUnAuthorizedKind,
		LoginParameterKind,
		UserPasswordUnmatchKind,
		NonTokenKind,
		InvalidTokenKind,
	}
	for _, kind := range loginKinds {
		if !IsLoginKind(kind) {
			t.Errorf("%s 应属登录域", kind.Name)
		}
	}

	otherKinds := []*apperr.Kind{
		ChatLLMKind,
		AgentParameterKind,
		DatabaseQueryKind,
		RuntimeClientKind,
	}
	for _, kind := range otherKinds {
		if IsLoginKind(kind) {
			t.Errorf("%s 不应属登录域(登录链路会因此外泄内部文案)", kind.Name)
		}
	}

	if IsLoginKind(nil) {
		t.Fatalf("nil 不应属登录域")
	}
}
