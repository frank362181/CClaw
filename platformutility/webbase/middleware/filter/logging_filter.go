package filter

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type LoggingFilter struct {
	basic.BaseFilter
}

func (lf *LoggingFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		basic.GetBaseRequester().GetOrCreateRequestInfo(c)
		c.Next()
	}
}
