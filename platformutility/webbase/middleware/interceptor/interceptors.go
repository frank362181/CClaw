/*
* interceptor串,本质上一个filter,里面管理遗传interceptor
 */

package interceptor

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type Interceptors struct {
	basic.BaseFilter

	chains map[string]basic.IInterceptor
}

func (interceptors *Interceptors) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 利用defer 确保无论如何退出路径，所有的AfterComplete 都会执行
		defer func() {
			//遍历 chains

		}()

		// 1. 顺序执行前置拦截

		// 2. 执行整整的逻辑
		c.Next()

		// 3. 逆序执行 后置拦截
	}
}
