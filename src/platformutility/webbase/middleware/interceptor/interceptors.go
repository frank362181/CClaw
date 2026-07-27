/*
* interceptor串,本质上一个filter,里面管理遗传interceptor
 */

package interceptor

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/webbase/middleware/basic"
)

type Interceptors struct {
	basic.BaseFilter

	interceptorsKey []string
	chains          map[string]basic.IInterceptor
}

func (in *Interceptors) RegistryInterceptor(inter basic.IInterceptor) error {
	if inter == nil {
		return exception.ParameterEmptyErrorEx("parameter interface is nil")
	}

	if _, ok := in.chains[inter.GetInterceptorName()]; ok {
		return exception.MatchErrorEx("interceptor already exist: " + inter.GetInterceptorName())
	}

	in.interceptorsKey = append(in.interceptorsKey, inter.GetInterceptorName())
	in.chains[inter.GetInterceptorName()] = inter
	return nil
}

func (in *Interceptors) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			for _, inter := range in.chains {
				inter.AfterCompletion(c)
			}
		}()

		// 1. 顺序执行前置拦截
		for _, inter := range in.chains {
			if !inter.PreHandle(c) {
				return
			}
		}

		// 2. 执行正正的逻辑
		c.Next()

		// 3. 逆序执行 后置拦截
		for i := len(in.interceptorsKey) - 1; i >= 0; i-- {
			in.chains[in.interceptorsKey[i]].PostHandle(c)
		}
	}
}
