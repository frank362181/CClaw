package middleware

import (
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type FilterInterceptorMgr struct {
	filters map[string]basic.IFilter //filter

	interceptors map[string]basic.IFilter //interceptor
}

var filterMgr = common.GetSingleton(func() *FilterInterceptorMgr {
	return &FilterInterceptorMgr{
		filters:      make(map[string]basic.IFilter),
		interceptors: make(map[string]basic.IFilter),
	}
}).Instance()

func GetFilterMgr() *FilterInterceptorMgr {
	return filterMgr
}
