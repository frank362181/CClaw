package interceptor

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

func GetInterceptors(order int) *Interceptors {
	interceptors := common.GetSingleton(func() *Interceptors {
		return &Interceptors{
			BaseFilter: basic.BaseFilter{
				Order:      order,
				FilterName: fmt.Sprintf("Interceptors_%d", order),
			},
		}
	}).Instance()

	return interceptors
}
