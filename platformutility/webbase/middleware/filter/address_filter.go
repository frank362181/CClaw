package filter

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type AddressFilter struct {
	basic.BaseFilter // 继承基础struct

	URLPatterns  []string //URL的模式
	URLBlackList []string //URL黑名单
	IPBlackList  []string //IP 黑名单
}

func (af *AddressFilter) SetupAddress() error {
	return nil
}

// DoFilter filter的核心方法
func (af *AddressFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {

		requestInfo := basic.GetBaseRequester().GetOrCreateRequestInfo(c)

		// 1. 检查URL模式是否匹配
		if af.isUrlPatternMatched(requestInfo.RelativeURL, requestInfo.RequestMethod) {
			basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstUrlPatternUnMatched, basic.ConstUrlPatternUnMatchedMsg)
			return
		}

		// 2. 检查URL黑名单
		if af.isBlackHit(requestInfo.RelativeURL) {
			basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstBlackUrl, basic.ConstBlackUrlMsg)
			return
		}

		// 3. 检查IP黑名单
		clientIP := c.ClientIP()
		if af.isIpHit(clientIP) {
			basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstBlackIp, basic.ConstBlackIpMsg)
			return
		}

		// 放行
		c.Next()
	}
}

// ========================== private functions & methods ======================================

func (af *AddressFilter) isUrlPatternMatched(relativeUrl string, method string) bool {
	// OPTIONS 直接放行(预检请求)
	if strings.EqualFold(method, http.MethodOptions) {
		return false
	}

	return true
}

func (af *AddressFilter) isBlackHit(relativeUrl string) bool {
	return false
}

func (af *AddressFilter) isIpHit(ip string) bool {
	return false
}
