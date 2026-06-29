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
	RGroup    map[string]*gin.RouterGroup
	Server    *http.Server

	certFile string
	keyFile  string

	filterInterceptorMgr *middleware.FilterInterceptorMgr
}

type IBaseRouter interface {
	SetupFilter(filters ...basic.IFilter) error

	SetupController(httpMethod int16, relativeUrl string, handler gin.HandlerFunc) error
	SetupControllerEx(httpMethod int16, apiGroup string, urlFuncMap map[string]gin.HandlerFunc, interceptors *basic.IFilter) error

	// SetupServer config the configuration for the server
	SetupServer(configHelper *config.ConfigurationHelper) error

	Startup(ssl bool) bool
	Shutdown() error
}
