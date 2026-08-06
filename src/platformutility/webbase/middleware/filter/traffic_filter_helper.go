package filter

import (
	"fmt"

	"my.favorite/platformutility/common"
	common2 "my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var trafficFilter = common.GetSingleton(func() *TrafficFilter {
	return &TrafficFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderTraffic,
			FilterName: fmt.Sprintf("trafficFilter_%s", basic.FilterOrderTraffic),
		},
	}
}).Instance()

func GetTrafficFilter(db common2.IMemoryDB) *TrafficFilter {
	trafficFilter.memoryDb = db
	return trafficFilter
}
