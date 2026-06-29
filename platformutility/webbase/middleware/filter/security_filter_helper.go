package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var securityFilter = common.GetSingleton(func() *SecurityFilter {
	securityFilter := &SecurityFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderSecurity,
			FilterName: fmt.Sprintf("securityFilter_%d", basic.FilterOrderSecurity),
		},
	}

	deSecurityUrls := []string{
		webbase.HealthUrl,
		webbase.LogoutUrl,
	}

	securityFilter.SetupSecurityFilter(deSecurityUrls)
	return securityFilter
}).Instance()

func GetSecurityFilter() *SecurityFilter {
	return securityFilter
}
