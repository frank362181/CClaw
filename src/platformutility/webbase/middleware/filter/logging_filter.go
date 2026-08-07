package filter

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type LoggingFilter struct {
	basic.BaseFilter
}

var loggingFilter = common.GetSingleton(func() *LoggingFilter {
	return &LoggingFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderLogging,
			FilterName: fmt.Sprintf("loggingFilter_%d", basic.FilterOrderLogging),
		},
	}
}).Instance()

// GetLoggingFilter 全局单例模式
func GetLoggingFilter() *LoggingFilter {
	return loggingFilter
}

func NewLoggingFilter(order int) *LoggingFilter {
	return &LoggingFilter{
		BaseFilter: basic.BaseFilter{
			Order:      order,
			FilterName: fmt.Sprintf("loggingFilter_%d", order),
		},
	}
}

func (lf *LoggingFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		r := basic.GetBaseRequester().GetOrCreateRequestInfo(c)

		//打印日志
		logging.LogDebugf("remote IP:%s: access URL:%s at :%d", r.RemoteIp, r.RelativeURL, r.ReqAtdTimestamp)

		c.Next()
	}
}
