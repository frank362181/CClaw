/*
* 是否需要用户校验的interceptor
 */

package interceptor

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"my.favorite/cclawservice/common"
	cclawException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/service"
)

type AuthenticationInterceptor struct {
	basic.BaseInterceptor

	UnWantedTokenUrls []string //不需要token的URL列表

	// 业务处理的Service
	userService service.IService
}

func NewAuthenticationInterceptor(svc service.IService) *AuthenticationInterceptor {
	authInterceptor := &AuthenticationInterceptor{
		BaseInterceptor: basic.BaseInterceptor{
			Order:           1,
			InterceptorName: "authentication_interceptor",
		},
		userService: svc,
	}
	return authInterceptor
}

func (i *AuthenticationInterceptor) SetupUnwantedTokenUrls(urls []string) {
	for _, url := range urls {
		i.UnWantedTokenUrls = append(i.UnWantedTokenUrls, url)
	}
}

func (i *AuthenticationInterceptor) GetService() service.IService {
	return i.userService
}

func (i *AuthenticationInterceptor) PreHandle(c *gin.Context) bool {
	reqInfo := basic.GetBaseRequester().GetOrCreateRequestInfo(c)

	if i.unwantedTokenUrl(reqInfo.RequestPath) {
		return true
	}

	//此时需要先判断token的情况,为空、过期、不存在等
	token := c.GetHeader(basic.ConstHeaderToken)
	token = strings.TrimSpace(token)
	if len(token) == 0 {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusUnauthorized, common.ConstErrorNonToken, common.ConstErrorNonTokenMsg, nil)
		return false
	}

	// 此时要从数据库中获取指定的token，此时是i必须要经过登录的API
	userService, ok := i.userService.(interface {
		GetUserByToken(string) (*dataobject.TAccount, error)
	})
	if !ok {
		//服务器内部出错
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return false
	}
	userInfo, err1 := userService.GetUserByToken(token)
	if err1 != nil {
		if _, ok2 := errors.AsType[*cclawException.ErrorInvalidToken](err1); !ok2 {
			basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
			return false
		}

		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusUnauthorized, common.ConstErrorInvalidToken, common.ConstErrorInvalidTokenMsg, nil)
		return false
	}
	if userInfo == nil {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusUnauthorized, common.ConstErrorInvalidToken, common.ConstErrorInvalidTokenMsg, nil)
		return false
	}

	c.Set(common.ContextAuthenticatedUser, userInfo)

	return true
}

func (i *AuthenticationInterceptor) PostHandle(c *gin.Context) {}

func (i *AuthenticationInterceptor) AfterCompletion(c *gin.Context) {

}

// ========================== private functions & methods ==================================

/*
* 对是否是不需要token的request path进行判断
 */
func (i *AuthenticationInterceptor) unwantedTokenUrl(requestPath string) bool {
	for _, url := range i.UnWantedTokenUrls {
		tempStr := strings.ToLower(url)
		if strings.EqualFold(tempStr, requestPath) {
			return true
		}
	}

	return false
}
