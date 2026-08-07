package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var corsFilter = common.GetSingleton(func() *CorsFilter {
	return &CorsFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderCors,
			FilterName: fmt.Sprintf("corsFilter_%d", basic.FilterOrderCors),
		},
	}
}).Instance()

// GetCorsFilter 单例模式
func GetCorsFilter() *CorsFilter {
	return corsFilter
}

// NewCorsFilter 每次创建一个
func NewCorsFilter(filterOrder int) *CorsFilter {
	return &CorsFilter{
		BaseFilter: basic.BaseFilter{
			Order:      filterOrder,
			FilterName: fmt.Sprintf("corsFilter_%d", filterOrder),
		},
	}
}
