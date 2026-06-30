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
)

type AuthenticationInterceptor struct {
	basic.BaseInterceptor

	UnWantedTokenUrls []string //不需要token的URL列表
}

func (i *AuthenticationInterceptor) SetupUnwantedTokenUrls(urls []string) {
	for _, url := range urls {
		i.UnWantedTokenUrls = append(i.UnWantedTokenUrls, url)
	}
}

func (i *AuthenticationInterceptor) PreHandle(c *gin.Context) bool {
	reqInfo := basic.GetBaseRequester().GetOrCreateRequestInfo(c)

	for _, url := range i.UnWantedTokenUrls {
		tempStr := webbase.BasePath + "/" + url
		tempStr = strings.ToLower(tempStr)
		if strings.EqualFold(tempStr, reqInfo.RequestPath) {
			return true
		}
	}

	//此时需要先判断token的情况,为空、过期、不存在等
	token := c.GetHeader(basic.ConstHeaderToken)
	if len(token) == 0 {
		basic.GetBaseResponser().WriteWithError(c, http.StatusOK, agentoscommon.ConstErrorInvalidToken, agentoscommon.ConstErrorInvalidTokenMsg, nil)
		return false
	}

	return true
}

func (i *AuthenticationInterceptor) PostHandle(c *gin.Context) {}

func (i *AuthenticationInterceptor) AfterCompletion(c *gin.Context) {

}
