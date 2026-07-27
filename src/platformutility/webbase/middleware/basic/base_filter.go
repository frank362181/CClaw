package basic

import (
	"github.com/gin-gonic/gin"
)

type BaseFilter struct {
	Order      int    //the order of the middleware
	FilterName string // the name of the filter
}

type IFilter interface {
	GetOrder() int
	GetFilterName() string
	DoFilter() gin.HandlerFunc
}

func (filter *BaseFilter) GetOrder() int {
	return filter.Order
}

func (filter *BaseFilter) GetFilterName() string {
	return filter.FilterName
}
