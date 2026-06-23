/*
* 安全 filter
 */

package middleware

import "github.com/gin-gonic/gin"

type SecurityFilter struct {
	BaseFilter
}

func (sf *SecurityFilter) DoFilter() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1 replay attack

		// 2 replay attach

		// 放行
		c.Next()
	}
}

// ================================= private functions & methods ====================================
