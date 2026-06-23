package webbase

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/webbase/middleware"
)

type BaseRouter struct {
	GinEngine *gin.Engine
	Server    *http.Server
}

type BaseRouterInterface interface {
	SetupFilter(filter middleware.BaseFilter) error
	StartupServer(configFileName string) error
	ShutdownServer() error
}
