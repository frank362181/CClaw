// Package webbase
// 采用OOD/OOP 思维模式,当前文件相当于包公开函数创建一对象
// webbase_router.go则是对象内的函数

package webbase

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware"
)

// ================================= public functions & methods ===============================

var baseRouter = common.GetSingleton(func() *BaseRouter {
	gin.SetMode(gin.ReleaseMode) //设置release模式

	router := &BaseRouter{
		GinEngine:            gin.New(),
		Server:               nil,
		RGroup:               make(map[string]*gin.RouterGroup),
		filterInterceptorMgr: middleware.GetFilterInterceptorMgr(),
		certFile:             "",
		keyFile:              "",
	}
	return router
}).Instance()

func GetBaseRouter() *BaseRouter {
	return baseRouter
}
