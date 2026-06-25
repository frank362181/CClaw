/*
* 对请求进行预处理的filter
 */

package filter

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type RequestPreprocessFilter struct {
	basic.BaseFilter
}

func (rf *RequestPreprocessFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		basic.GetBaseRequester().GetOrCreateRequestInfo(c)

		// 放行
		c.Next()
	}
}
