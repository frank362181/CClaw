package middleware

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type FilterInterceptorMgr struct {
	globalFilters map[string]basic.IFilter //global filter

	interceptors map[string]basic.IFilter //interceptor
}

// RegisterFilter 注册filter将会按照调用该函数的顺序被注册进gin.Engine中

func (filterMgr *FilterInterceptorMgr) RegisterFilter(filter basic.IFilter, engine *gin.Engine) error {
	if filter == nil || engine == nil {
		return exception.ParameterEmptyErrorEx("parameter empty")
	}

	if _, ok := filterMgr.globalFilters[filter.GetFilterName()]; ok {
		return exception.MatchErrorEx("filter already registered")
	}

	filterMgr.globalFilters[filter.GetFilterName()] = filter
	engine.Use(filter.DoFilter())

	return nil
}
