package basic

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
)

type GlobalRequester struct {
	trustForwarded bool //是否可信代理来的转发头
}

var globalRequester = common.GetSingleton(func() *GlobalRequester {
	return &GlobalRequester{
		trustForwarded: false, //默认为false
	}
}).Instance()

func GetBaseRequester() *GlobalRequester {
	return globalRequester
}

func (req *GlobalRequester) SetTrustedForwardedHeaders(trustedForwardedHeaders bool) {
	req.trustForwarded = trustedForwardedHeaders
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
			RemoteAddr:    c.Request.RemoteAddr,
			RemoteIp:      c.ClientIP(),
			RequestURL:    GetFullUrl(c, req.trustForwarded),
			RequestMethod: c.Request.Method,
			RelativeURL:   c.Request.URL.String(),
			RequestPath:   c.Request.URL.Path,

			ReqAtdTimestamp: common.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp,
			QueryString:     c.Request.URL.RawQuery,
			QueryParams:     c.Request.URL.Query(),

			Nonce:     c.Query(ConstQueryNonce),
			Signature: c.Query(ConstSignature),
		}

		u64, err := strconv.ParseInt(c.Query(ConstQueryTimestamp), 10, 64)
		if err != nil {
			requestInfo.QueryTimestamp = 0 //此时没有设置,未必有问题,有些API 不需要设置
		} else {
			requestInfo.QueryTimestamp = u64
		}

		//存入到Gin上下文
		c.Set(ConstRequestTotal, requestInfo)
		return requestInfo
	}

	return requestInfo.(*RequestInfo)
}
