package basic

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
)

type GlobalRequester struct{}

var globalRequester = common.GetSingleton(func() *GlobalRequester {
	return &GlobalRequester{}
}).Instance()

func GetBaseRequester() *GlobalRequester {
	return globalRequester
}

func (req *GlobalRequester) GetOrCreateRequestInfo(c *gin.Context) *RequestInfo {
	return req.getRequestInfo(c)
}

// =================================== private functions & methods ====================================
func (req *GlobalRequester) getRequestInfo(c *gin.Context) *RequestInfo {
	requestInfo, exists := c.Get(ConstRequestTotal)
	if !exists {
		requestInfo := &RequestInfo{
			RequestId:     common.GenerateUUID(),
			RemoteIp:      c.ClientIP(),
			RequestURL:    c.Request.URL.String(),
			RequestMethod: c.Request.Method,
			RelativeURL:   c.Request.URL.Path,

			ArrivedTimestamp: common.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp,
			QueryString:      c.Request.URL.RawQuery,
			QueryParams:      c.Request.URL.Query(),

			Nonce:     c.Query(ConstNonce),
			Signature: c.Query(ConstSignature),
		}

		u64, err := strconv.ParseInt(c.Query(ConstQueryStringTimestamp), 10, 64)
		if err != nil {
			requestInfo.QueryStringTimestamp = 1 //此时一定有问题,即请求端必须在query string中设置 TIMESTAMP=? 且为整数
		} else {
			requestInfo.QueryStringTimestamp = u64
		}

		//存入到Gin上下文
		c.Set(ConstRequestTotal, requestInfo)
		return requestInfo
	}

	return requestInfo.(*RequestInfo)
}
