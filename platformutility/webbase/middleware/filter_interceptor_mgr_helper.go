package middleware

import (
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

var filterInterceptorMgr = common.GetSingleton(func() *FilterInterceptorMgr {
	filterInterceptorMgr := &FilterInterceptorMgr{
		filters:      make(map[string]basic.IFilter),
		interceptors: make(map[string]basic.IFilter),
	}
	return filterInterceptorMgr
}).Instance()

func GetFilterInterceptorMgr() *FilterInterceptorMgr {
	return filterInterceptorMgr
}
