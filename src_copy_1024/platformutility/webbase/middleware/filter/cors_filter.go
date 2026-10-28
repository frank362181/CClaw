package filter

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type CorsFilter struct {
	basic.BaseFilter

	allowedOrigins []string //允许的来源白名单
}

var corsFilter = common.GetSingleton(func() *CorsFilter {
	return &CorsFilter{
		BaseFilter: basic.BaseFilter{
			Order:      basic.FilterOrderCors,
			FilterName: fmt.Sprintf("corsFilter_%d", basic.FilterOrderCors),
		},
	}
}).Instance()

// GetCorsFilter 单例模式
func GetCorsFilter() *CorsFilter {
	return corsFilter
}

// NewCorsFilter 每次创建一个
func NewCorsFilter(filterOrder int) *CorsFilter {
	return &CorsFilter{
		BaseFilter: basic.BaseFilter{
			Order:      filterOrder,
			FilterName: fmt.Sprintf("corsFilter_%d", filterOrder),
		},
	}
}

func (cf *CorsFilter) SetupAllowedOrigins2(origins []string) {
	if len(origins) == 0 {
		return
	}

	for _, origin := range origins {
		cf.allowedOrigins = append(cf.allowedOrigins, origin)
	}
}

func (cf *CorsFilter) DoFilter() gin.HandlerFunc {
	origins := cf.allowedOrigins
	if len(origins) == 0 {
		origins = []string{"http://localhost:5500", "http://127.0.0.1:5500"}
		//origins = []string{"*"}
	}

	return cors.New(
		cors.Config{
			AllowOrigins: origins,

			//允许的HTTP方法
			AllowMethods: []string{
				http.MethodGet,
				http.MethodPost,
				http.MethodPut,
				http.MethodPatch,
				http.MethodDelete,
				http.MethodOptions,
			},

			AllowHeaders: []string{
				"Origin",
				"Content-Type",
				"Authorization",
				"X-Requested-With",
				"TIMESTAMP",
				"SIGNATURE",
				"NONCE",
			},

			//暴露给前端的响应头
			ExposeHeaders: []string{
				"Content-Length",
				"X-Custom-Header",
			},

			//是否允许携带cookie
			AllowCredentials: false, //前端使用token，不使用cookie

			//预检请求缓存时间(秒)
			MaxAge: 12 * time.Hour,
		})
}
