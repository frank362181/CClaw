/*
* 流量控制filter
* 控制维度：1. 全局：IP维度：该IP过去 2 分钟内调用次数超过阈值
*		  2. IP-API维度：某调用某API过去 2 分钟被调用次数超过阈值
*		  3. LoginName维度，过去2分钟内 调用次数阈值
*		  4. LoginName-API维度，过去2分钟内调用次数阈值
**/

package filter

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
	memoryCommon "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type TrafficFilter struct {
	basic.BaseFilter

	UnTrafficUrls []string //不需要流量控制的URL

	memoryDb memoryCommon.IMemoryDB //记录流量的内存数据库
}

var trafficFilter = common.GetSingleton(func() *TrafficFilter {
	return &TrafficFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderTraffic,
			FilterName: fmt.Sprintf("trafficFilter_%d", basic.FilterOrderTraffic),
		},
	}
}).Instance()

func GetTrafficFilter(db memoryCommon.IMemoryDB) *TrafficFilter {
	trafficFilter.memoryDb = db
	return trafficFilter
}

func NewTrafficFilter(order int, db memoryCommon.IMemoryDB) *TrafficFilter {
	trafficFilter := &TrafficFilter{
		BaseFilter: basic.BaseFilter{
			Order:      order,
			FilterName: fmt.Sprintf("trafficFilter_%d", order),
		},
	}

	trafficFilter.memoryDb = db
	return trafficFilter
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
		if !filter.unTrafficUrl(requestInfo.RequestPath) {
			if filter.memoryDb == nil {
				// redis抖动时开放限流保可用
				logging.LogWarnF("[Traffic] memory db unavailable,skip traffic limit.")
				c.Next()
				return
			}

			// 控制流量 1 IP维度
			ipTrafficKey := basic.IPTrafficKeyPrefix + requestInfo.RemoteIp
			result, _ := filter.memoryDb.IncrementWithExpiration(ipTrafficKey, 1, time.Duration(basic.TrafficFilterTimeGap)*time.Second)
			if result+1 > basic.IPTrafficFilterLimits {
				basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstTrafficOverflow, basic.ConstTrafficOverflowMsg, nil)
				return
			}

			// 2. ip api维度
			ipApiTrafficKey := basic.IPAPITrafficPrefix + requestInfo.RemoteIp + requestInfo.RequestPath
			result, _ = filter.memoryDb.IncrementWithExpiration(ipApiTrafficKey, 1, time.Duration(basic.TrafficFilterTimeGap)*time.Second)
			if result+1 > basic.IPAPITrafficFilterLimits {
				basic.GetBaseResponser().AbortWithError(c, http.StatusForbidden, basic.ConstTrafficOverflow, basic.ConstTrafficOverflowMsg, nil)
				return
			}
		}

		// 放行
		c.Next()
	}
}

func (filter *TrafficFilter) unTrafficUrl(url string) bool {
	for _, u := range filter.UnTrafficUrls {
		if strings.EqualFold(u, url) {
			//不需要限流
			return true
		}
	}

	return false //需要限流
}
