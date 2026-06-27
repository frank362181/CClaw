package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var securityFilter = common.GetSingleton(func() *SecurityFilter {
	return &SecurityFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderSecurity,
			FilterName: fmt.Sprintf("securityFilter_%d", basic.FilterOrderSecurity),
		},
	}
}).Instance()

func GetSecurityFilter() *SecurityFilter {
	return securityFilter
}
