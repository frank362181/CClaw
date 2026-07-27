package interceptor

import (
	"fmt"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var interceptors = common.GetSingleton(func() *Interceptors {
	return &Interceptors{
		BaseFilter: basic.BaseFilter{
			Order:      99,
			FilterName: fmt.Sprintf("Interceptors_%d", 99),
		},
		chains: make(map[string]basic.IInterceptor),
	}
}).Instance()

func GetInterceptors() *Interceptors {
	return interceptors
}
