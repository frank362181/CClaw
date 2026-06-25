package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

func GetAddressFilter(order int) *AddressFilter {
	addressFilter := common.GetSingleton(func() *AddressFilter {
		tmpFilter := &AddressFilter{
			BaseFilter: basic.BaseFilter{
				Order:      order,
				FilterName: fmt.Sprintf("addressFilter_%d", order),
			},
		}
		return tmpFilter
	}).Instance()

	return addressFilter
}
