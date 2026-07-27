package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var addressFilter = common.GetSingleton(func() *AddressFilter {
	addressFilter := &AddressFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderAddress,
			FilterName: fmt.Sprintf("addressFilter_%d", basic.FilterOrderAddress),
		},
	}

	//allowedUrls := []string{
	//	webbase.HealthUrl,
	//	webbase.LoginUrl,
	//	webbase.LogoutUrl,
	//}
	//addressFilter.SetupAllowedUrls(allowedUrls)
	return addressFilter
}).Instance()

func GetAddressFilter() *AddressFilter {
	return addressFilter
}
