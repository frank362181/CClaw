/*
* 对请求进行预处理的filter
* 此时如果要对requestInfo进行处理,一定要记得将其放回到context中
 */

package filter

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type RequestPreprocessFilter struct {
	basic.BaseFilter
}

var requestPreprocessFilter = common.GetSingleton(func() *RequestPreprocessFilter {
	return &RequestPreprocessFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderReqPre,
			FilterName: fmt.Sprintf("reqPreprocessFilter_%d", basic.FilterOrderReqPre),
		},
	}
}).Instance()

func GetRequestPreprocessFilter() *RequestPreprocessFilter {
	return requestPreprocessFilter
}

func NewRequestPreprocessFilter(order int) *RequestPreprocessFilter {
	return &RequestPreprocessFilter{
		BaseFilter: basic.BaseFilter{
			Order:      order,
			FilterName: fmt.Sprintf("reqPreprocessFilter_%d", order),
		},
	}
}

func (rf *RequestPreprocessFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		basic.GetBaseRequester().GetOrCreateRequestInfo(c)

		// 放行
		c.Next()
	}
}
