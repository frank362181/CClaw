package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

func GetRecoveryFilter(order int) *GlobalRecoveryFilter {
	recoveryFilter := common.GetSingleton(func() *GlobalRecoveryFilter {
		return &GlobalRecoveryFilter{
			BaseFilter: basic.BaseFilter{
				Order:      order,
				FilterName: fmt.Sprintf("recoveryFilter_%d", order),
			},
		}
	}).Instance()

	return recoveryFilter
}
