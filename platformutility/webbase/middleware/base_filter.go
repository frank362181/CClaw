package middleware

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
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
		return exception.ParameterEmptyErrorEx("filter order can not negative number:" + fmt.Sprintf("%d", filter.Order))
	}

	if order == filter.Order {
		return exception.MatchErrorEx("filter order exists: " + strconv.Itoa(filter.Order))
	}

	filter.Order = order
	return nil
}

// =============================== private functions & methods =======================================

func (filter *BaseFilter) getRequestInfo(c *gin.Context) *RequestInfo {
	requestInfo, exists := c.Get(ConstRequestTotal)
	if !exists {
		requestInfo := &RequestInfo{
			RequestId:     common.GenerateUUID(),
			RemoteIp:      c.ClientIP(),
			RequestURL:    c.Request.URL.String(),
			RequestMethod: c.Request.Method,
			RelativeURL:   c.Request.URL.Path,

			ArrivedTimestamp: time.Now(),
			QueryString:      c.Request.URL.RawQuery,
			QueryParams:      c.Request.URL.Query(),

			Nonce:     c.Query(ConstNonce),
			Signature: c.Query(ConstSignature),
		}

		u32, err := strconv.ParseUint(c.Query(ConstQueryStringTimestamp), 10, 32)
		if err != nil {
			requestInfo.QueryStringTimestamp = 0 //此时一定会有问题,即请求段必须设置
		} else {
			requestInfo.QueryStringTimestamp = u32
		}

		//存入到Gin上下文
		c.Set(ConstRequestTotal, requestInfo)
		return requestInfo
	}

	return requestInfo.(*RequestInfo)
}
