package middleware

import "github.com/gin-gonic/gin"

type Filter interface {
	doFilter() gin.HandlerFunc
}

type Interceptor interface {
	doIntercept() gin.HandlerFunc
}
