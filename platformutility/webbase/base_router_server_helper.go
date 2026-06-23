// Package webbase
// 采用棉线对象的思维模式,当前文件相当于包公开函数创建一对象
// webbase_router.go则是对象内的函数

package webbase

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
)

// ================================= public functions & methods ===============================

func GetBaseRouter() *BaseRouter {
	webBaseRouter := common.GetSingleton(func() *BaseRouter {
		return &BaseRouter{
			GinEngine: gin.New(),
		}
	}).Instance()

	return webBaseRouter
}
