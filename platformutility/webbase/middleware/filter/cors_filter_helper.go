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

func GetCorsFilter() *CorsFilter {
	return corsFilter
}
