package middleware

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/exception"
)

type BaseFilter struct {
	Order int //the order of the middleware
}

type FilterInterface interface {
	GetOrder() int
	SetOrder(int) error
	DoFilter() gin.HandlerFunc
}

func (filter *BaseFilter) GetOrder() int {
	return filter.Order
}

func (filter *BaseFilter) SetOrder(order int) error {
	if order < 0 {
		return exception.ParameterEmptyErrorEx("filter order can not negative number:" + string(filter.Order))
	}

	if order == filter.Order {
		return exception.MatchErrorEx("filter order exists: " + strconv.Itoa(filter.Order))
	}

	filter.Order = order
	return nil
}
