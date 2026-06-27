package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var globalRecoveryFilter = common.GetSingleton(func() *GlobalRecoveryFilter {
	return &GlobalRecoveryFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderRecovery,
			FilterName: fmt.Sprintf("recoveryFilter_%d", basic.FilterOrderRecovery),
		},
	}
}).Instance()

func GetRecoveryFilter() *GlobalRecoveryFilter {
	return globalRecoveryFilter
}
