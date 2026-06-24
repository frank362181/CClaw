package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type AddressFilterFilter struct {
	BaseFilter // 继承基础struct

	URLPatterns  []string //URL的模式
	URLBlackList []string //URL黑名单
	IPBlackList  []string //IP 黑名单
}

func (af *AddressFilterFilter) SetupAddress() error {
	return nil
}

// DoFilter filter的核心方法
func (af *AddressFilterFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {

		requestInfo := af.getRequestInfo(c)

		// 1. 检查URL模式是否匹配
		if af.isUrlPatternMatched(requestInfo.RelativeURL, requestInfo.RequestMethod) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":   ConstUrlPatternUnMatched,
				"url":    requestInfo.RelativeURL,
				"method": requestInfo.RequestMethod,
				"data":   nil,
			})
			return
		}

		// 2. 检查URL黑名单
		if af.isBlackHit(requestInfo.RelativeURL) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":   ConstBlackUrl,
				"url":    requestInfo.RelativeURL,
				"method": requestInfo.RequestMethod,
				"data":   nil,
			})
			return
		}

		// 3. 检查IP黑名单
		clientIP := c.ClientIP()
		if af.isIpHit(clientIP) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":   ConstBlackIp,
				"url":    requestInfo.RelativeURL,
				"method": requestInfo.RequestMethod,
				"data":   nil,
			})
			return
		}

		// 放行
		c.Next()
	}
}

// ========================== private functions & methods ======================================

func (af *AddressFilterFilter) isUrlPatternMatched(relativeUrl string, method string) bool {
	// OPTIONS 直接放行(预检请求)
	if strings.EqualFold(method, http.MethodOptions) {
		return false
	}

	return true
}

func (af *AddressFilterFilter) isBlackHit(relativeUrl string) bool {
	return false
}

func (af *AddressFilterFilter) isIpHit(ip string) bool {
	return false
}
