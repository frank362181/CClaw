package filter

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/logging"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type LoggingFilter struct {
	basic.BaseFilter
}

func (lf *LoggingFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		r := basic.GetBaseRequester().GetOrCreateRequestInfo(c)

		//打印日志
		logging.LogDebugf("remote IP:%s: access URL:%s at :%d", r.RemoteIp, r.RelativeURL, r.ReqAtdTimestamp)

		c.Next()
	}
}
