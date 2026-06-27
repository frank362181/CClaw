package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var addressFilter = common.GetSingleton(func() *AddressFilter {
	return &AddressFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderAddress,
			FilterName: fmt.Sprintf("addressFilter_%d", basic.FilterOrderAddress),
		},
	}
}).Instance()

func GetAddressFilter() *AddressFilter {
	return addressFilter
}
