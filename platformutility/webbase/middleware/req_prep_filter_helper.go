package middleware

import (
	"my.favorite/platformutility/common"
)

func GetRequestPreprocessFilter(order int) *RequestPreprocessFilter {
	reqPreprocessFilter := common.GetSingleton(func() *RequestPreprocessFilter {
		return &RequestPreprocessFilter{
			BaseFilter: BaseFilter{
				Order: order,
			},
		}
	}).Instance()

	return reqPreprocessFilter
}
