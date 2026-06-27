package filter

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type AddressFilter struct {
	basic.BaseFilter // 继承基础struct

	AllowedUrl   []string // 允许被放行的URl
	URLBlackList []string //URL黑名单
	IPBlackList  []string //IP 黑名单
}

func (af *AddressFilter) SetupAllowedUrl(allowedUrls ...string) {
	for _, url := range allowedUrls {
		af.AllowedUrl = append(af.AllowedUrl, url)
	}
}

func (af *AddressFilter) SetupUrlBlackList(blackUrls ...string) {
	for _, url := range blackUrls {
		af.URLBlackList = append(af.URLBlackList, url)
	}
}

func (af *AddressFilter) SetupIpBlackList(blackIps ...string) {
	for _, ip := range blackIps {
		af.IPBlackList = append(af.IPBlackList, ip)
	}
}

// DoFilter filter的核心方法
func (af *AddressFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {

		requestInfo := basic.GetBaseRequester().GetOrCreateRequestInfo(c)

		// 1. 检查URL模式是否匹配
		if !af.isAllowedUrl(requestInfo.RelativeURL, requestInfo.RequestMethod) {
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

// 命中这些url pattern直接放行
var allowedUrlPatterns = [...]string{
	"/heartbeat",
	"/v2/api-docs.*",
	"/doc.html",
	"/index.html",
	"/manifest.json",
	"/robots.txt",
	"/swagger-resources/.*",
	"/configuration/security",
	"/service-worker.js",
	"/favicon.ico",
	"/swagger-resources",
	"/precache-manifest.eea302037a9c2783bdf341d6c2dd2ca2.js",
	"/swagger-ui.html/.*",
	"/webjars/.*",
	"/file/.*", // 文件访问过滤
}

func (af *AddressFilter) isAllowedUrl(relativeUrl string, method string) bool {
	// OPTIONS 直接放行(预检请求)
	if strings.EqualFold(method, http.MethodOptions) {
		return true
	}

	// 允许的url
	for _, allowedUrl := range af.AllowedUrl {
		if strings.EqualFold(relativeUrl, allowedUrl) {
			return true
		}
	}

	// 模式匹配,放行
	for _, pattern := range allowedUrlPatterns {
		if strings.EqualFold(relativeUrl, pattern) {
			return true
		}
	}

	return false
}

func (af *AddressFilter) isBlackHit(relativeUrl string) bool {
	for _, blackUrl := range af.URLBlackList {
		if strings.EqualFold(relativeUrl, blackUrl) {
			return true
		}
	}

	return false
}

func (af *AddressFilter) isIpHit(ip string) bool {
	for _, blackIp := range af.IPBlackList {
		if strings.EqualFold(ip, blackIp) {
			return true
		}
	}

	return false
}
