package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	common2 "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var securityFilter = common.GetSingleton(func() *SecurityFilter {
	securityFilter := &SecurityFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderSecurity,
			FilterName: fmt.Sprintf("securityFilter_%d", basic.FilterOrderSecurity),
		},
	}

	//deSecurityUrls := []string{
	//	webbase.HealthUrl,
	//	webbase.LogoutUrl,
	//}
	//
	//securityFilter.SetupSecurityFilter(deSecurityUrls)
	return securityFilter
}).Instance()

func GetSecurityFilter(db common2.IMemoryDB) *SecurityFilter {
	securityFilter.memoryDb = db //设置内存数据库
	return securityFilter
}
