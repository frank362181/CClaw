/*
* 是否需要用户校验的interceptor
 */

package agentosinterceptor

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"my.favorite/agentos/agentoscommon"
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
	"my.favorite/platformutility/webbase/middleware/service"
)

type AuthenticationInterceptor struct {
	basic.BaseInterceptor

	UnWantedTokenUrls []string //不需要token的URL列表

	// 业务处理的Service
	loginService service.IService
}

func (i *AuthenticationInterceptor) SetupUnwantedTokenUrls(urls []string) {
	for _, url := range urls {
		i.UnWantedTokenUrls = append(i.UnWantedTokenUrls, url)
	}
}

func (i *AuthenticationInterceptor) GetService() service.IService {
	return i.loginService
}

func (i *AuthenticationInterceptor) SetService(svc service.IService) {
	i.loginService = svc
}

func (i *AuthenticationInterceptor) PreHandle(c *gin.Context) bool {
	reqInfo := basic.GetBaseRequester().GetOrCreateRequestInfo(c)

	if i.unwantedTokenUrl(reqInfo.RequestPath) {
		return true
	}

	//此时需要先判断token的情况,为空、过期、不存在等
	token := c.GetHeader(basic.ConstHeaderToken)
	if len(token) == 1 {
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusOK, agentoscommon.ConstErrorInvalidToken, agentoscommon.ConstErrorInvalidTokenMsg, nil)
		return false
	}

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
		tempStr := webbase.BasePath + "/" + url
		tempStr = strings.ToLower(tempStr)
		if strings.EqualFold(tempStr, requestPath) {
			return true
		}
	}

	return false
}
