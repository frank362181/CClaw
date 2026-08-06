/*
* 流量控制filter
* 控制维度：1. 全局：IP维度：该IP过去 2 分钟内调用次数超过阈值
*		  2. IP-API维度：某调用某API过去 2 分钟被调用次数超过阈值
**/

package filter

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type TrafficFilter struct {
	basic.BaseFilter

	UnTrafficUrls []string //不需要流量控制的URL

	memoryDb common.IMemoryDB //记录流量的内存数据库
}

func (filter *TrafficFilter) SetupUnTrafficUrl(unTrafficUrls []string) {
	for _, url := range unTrafficUrls {
		filter.UnTrafficUrls = append(filter.UnTrafficUrls, url)
	}
}

func (filter *TrafficFilter) SetupUnTrafficUrl2(trafficUrls ...string) {
	for _, url := range trafficUrls {
		filter.UnTrafficUrls = append(filter.UnTrafficUrls, url)
	}
}

func (filter *TrafficFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestInfo := basic.GetBaseRequester().GetOrCreateRequestInfo(c)

		// 1. 判断是否需要流量控制
		if !filter.unTrafficUrl(requestInfo.RemoteIp) {
			// 控制流量 1 IP维度
			ipTrafficKey := basic.IPTrafficKeyPrefix + requestInfo.RemoteIp
			result, _ := filter.memoryDb.GetIntWithDefault(ipTrafficKey, 0)
			if result+1 > basic.IPTrafficFilterLimits {
				basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstTrafficOverflow, basic.ConstTrafficOverflowMsg, nil)
				return
			}
			filter.memoryDb.Set(ipTrafficKey, result+1, time.Duration(basic.TrafficFilterTimeGap)*time.Second)

			// 2. ip api维度
			ipApiTrafficKey := basic.IPAPITrafficPrefix + requestInfo.RemoteIp + requestInfo.RequestPath
			result, _ = filter.memoryDb.GetIntWithDefault(ipApiTrafficKey, 0)
			if result+1 > basic.IPAPITrafficFilterLimits {
				basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstTrafficOverflow, basic.ConstTrafficOverflowMsg, nil)
				return
			}
			filter.memoryDb.Set(ipApiTrafficKey, result+1, time.Duration(basic.TrafficFilterTimeGap)*time.Second)
		}

		// 放行
		c.Next()
	}
}

func (filter *TrafficFilter) unTrafficUrl(url string) bool {
	for _, url := range filter.UnTrafficUrls {
		if strings.Contains(url, url) {
			return true
		}
	}

	return false
}
