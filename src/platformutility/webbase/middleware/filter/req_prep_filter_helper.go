package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

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
