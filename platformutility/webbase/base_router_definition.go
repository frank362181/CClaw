package webbase

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/webbase/middleware"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type BaseRouter struct {
	GinEngine *gin.Engine
	Server    *http.Server

	certFile string
	keyFile  string

	filterInterceptorMgr *middleware.FilterInterceptorMgr
}

type IBaseRouter interface {
	SetupFilter(filters ...basic.IFilter) error
	SetupServer(configHelper *config.ConfigurationHelper) error
	Startup(ssl bool) bool
	Shutdown() error
}
