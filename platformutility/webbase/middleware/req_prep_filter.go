/*
* 对请求进行预处理的filter
 */

package middleware

import (
	"github.com/gin-gonic/gin"
)

type RequestPreprocessFilter struct {
	BaseFilter
}

func (rf *RequestPreprocessFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		rf.getRequestInfo(c)

		// 放行
		c.Next()
	}
}
