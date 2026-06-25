package webbase

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type BaseRouter struct {
	GinEngine *gin.Engine
	Server    *http.Server
}

type IBaseRouter interface {
	SetupFilter(filter basic.IFilter) error
	StartupServer(configFileName string) error
	ShutdownServer() error
}
