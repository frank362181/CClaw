package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

func GetSecurityFilter(order int) *SecurityFilter {
	securityFilter := common.GetSingleton(func() *SecurityFilter {
		return &SecurityFilter{
			BaseFilter: basic.BaseFilter{
				Order:      order,
				FilterName: fmt.Sprintf("securityFilter_%d", order),
			},
		}
	}).Instance()

	return securityFilter
}
