/*
* 对请求进行预处理的filter
 */

package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
)

type RequestPreprocessFilter struct {
	BaseFilter
}

func (rf *RequestPreprocessFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestId := common.GenerateUUID()
		milliTimestamp := common.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp

		// 设置属性
		ctx := context.WithValue(c.Request.Context(), "requestId", requestId)
		ctx = context.WithValue(ctx, "reqArrivedTime", milliTimestamp)
		c.Request = c.Request.WithContext(ctx)

		// 放行
		c.Next()
	}
}
