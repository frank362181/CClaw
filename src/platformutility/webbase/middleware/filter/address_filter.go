package filter

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type AddressFilter struct {
	basic.BaseFilter // 继承基础struct

	AllowedUrls  []string //定义的允许的URL
	URLBlackList []string //URL黑名单
	IPBlackList  []string //IP 黑名单
}

var addressFilter = common.GetSingleton(func() *AddressFilter {
	addressFilter := &AddressFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderAddress,
			FilterName: fmt.Sprintf("addressFilter_%d", basic.FilterOrderAddress),
		},
	}

	//allowedUrls := []string{
	//	webbase.HealthUrl,
	//	webbase.LoginUrl,
	//	webbase.LogoutUrl,
	//}
	//addressFilter.SetupAllowedUrls(allowedUrls)
	return addressFilter
}).Instance()

func GetAddressFilter() *AddressFilter {
	return addressFilter
}

func NewAddressFilter(order int) *AddressFilter {
	addressFilter := &AddressFilter{
		BaseFilter: basic.BaseFilter{
			Order:      order,
			FilterName: fmt.Sprintf("addressFilter_%d", order),
		},
	}
	return addressFilter
}

func (af *AddressFilter) SetupAllowedUrls(allowedUrls []string) {
	for _, url := range allowedUrls {
		af.AllowedUrls = append(af.AllowedUrls, url)
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
		if !af.isAllowedUrl(requestInfo.RequestURL, requestInfo.RequestPath, requestInfo.RequestMethod) {
			basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstUrlInvalid, basic.ConstUrlInvalidMsg, nil)
			return
		}

		// 2. 检查URL黑名单
		if af.isBlackHit(requestInfo.RelativeURL) {
			basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstBlackUrl, basic.ConstBlackUrlMsg, nil)
			return
		}

		// 3. 检查IP黑名单
		clientIP := requestInfo.RemoteIp
		if af.isIpHit(clientIP) {
			basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstBlackIp, basic.ConstBlackIpMsg, nil)
			return
		}

		// 放行
		c.Next()
	}
}

// ========================== private functions & methods ======================================

func (af *AddressFilter) isAllowedUrl(requestUrl string, relativeUrl string, method string) bool {
	if basic.IsGlobalAllowed(requestUrl, relativeUrl, method) {
		return true
	}

	// 允许的url
	for _, allowedUrl := range af.AllowedUrls {
		tempAllowedUrl := webbase.BasePath + "/" + allowedUrl
		if strings.EqualFold(relativeUrl, tempAllowedUrl) {
			return true
		}
	}

	return false
}

func (af *AddressFilter) isBlackHit(relativeUrl string) bool {
	for _, blackUrl := range af.URLBlackList {
		tempBlackUrl := webbase.BasePath + "/" + blackUrl
		if strings.EqualFold(relativeUrl, tempBlackUrl) {
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
