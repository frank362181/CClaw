package middleware

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type FilterInterceptorMgr struct {
	filters map[string]basic.IFilter //filter

	interceptors map[string]basic.IFilter //interceptor
}

// RegisterFilter 注册filter将会按照调用该函数的顺序被注册进gin.Engine中

func (filterMgr *FilterInterceptorMgr) RegisterFilter(filter basic.IFilter, engine *gin.Engine) error {
	if filter == nil || engine == nil {
		return exception.ParameterEmptyErrorEx("parameter empty")
	}

	if _, ok := filterMgr.filters[filter.GetFilterName()]; ok {
		return exception.MatchErrorEx("filter already registered")
	}

	filterMgr.filters[filter.GetFilterName()] = filter
	engine.Use(filter.DoFilter())

	return nil
}
