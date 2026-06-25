package middleware

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/webbase/middleware/basic"
)

func (filterMgr *FilterInterceptorMgr) RegisterFilter(name string, filter basic.IFilter) error {
	if _, ok := filterMgr.filters[name]; ok {
		return exception.MatchErrorEx("filter already registered")
	}

	filterMgr.filters[name] = filter
	return nil
}

func (filterMgr *FilterInterceptorMgr) ApplyFilter(engine *gin.Engine) bool {
	return true
}
