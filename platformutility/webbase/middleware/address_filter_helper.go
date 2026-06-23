package middleware

import (
	"my.favorite/platformutility/common"
)

func GetAddressFilter(order int) *AddressFilterFilter {
	addressFilter := common.GetSingleton(func() *AddressFilterFilter {
		return &AddressFilterFilter{
			BaseFilter: BaseFilter{
				Order: order,
			},
		}
	}).Instance()

	return addressFilter
}
