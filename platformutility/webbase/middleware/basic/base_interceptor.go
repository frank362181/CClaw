package basic

import "github.com/gin-gonic/gin"

type BaseInterceptor struct {
	Order           int
	InterceptorName string
}

type IInterceptor interface {
	// PreHandle  前置处理
	PreHandle(c *gin.Context) bool

	// PostHandle 后置处理
	PostHandle(c *gin.Context)

	// AfterCompletion 执行完成之后的处理
	AfterCompletion(c *gin.Context)
}

func (interceptor *BaseInterceptor) GetOrder() int {
	return interceptor.Order
}

func (interceptor *BaseInterceptor) GetInterceptorName() string {
	return interceptor.InterceptorName
}
