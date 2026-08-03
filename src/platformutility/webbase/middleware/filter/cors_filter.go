package filter

import (
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type CorsFilter struct {
	basic.BaseFilter
}

func (cf *CorsFilter) DoFilter() gin.HandlerFunc {
	return cors.New(
		cors.Config{
			AllowOrigins: []string{"*"},

			//允许的HTTP方法
			AllowMethods: []string{
				http.MethodGet,
				http.MethodPost,
				http.MethodPut,
				http.MethodPatch,
				http.MethodDelete,
				http.MethodConnect,
				http.MethodOptions,
			},

			AllowHeaders: []string{
				"Origin",
				"Content-Type",
				"Authorization",
				"X-Requested-With",
				"TIMESTAMP",
				"SIGNATURE",
			},

			//暴露给前端的响应头
			ExposeHeaders: []string{
				"Content-Length",
				"X-Custom-Header",
			},

			//是否允许携带cookie
			AllowCredentials: true,

			//预检请求缓存时间(秒)
			MaxAge: 12 * time.Hour,
		})
}
