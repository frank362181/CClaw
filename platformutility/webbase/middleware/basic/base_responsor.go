package basic

import (
	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
)

type GlobalResponsor struct{}

var globalResponsor = common.GetSingleton(func() *GlobalResponsor {
	return &GlobalResponsor{}
}).Instance()

func GetBaseResponser() *GlobalResponsor {
	return globalResponsor
}

func (resp *GlobalResponsor) Success(c *gin.Context, httpStatus int, code int, msg string, data interface{}) {
	jsonObj := resp.createJsonObject(c, code, msg, data)
	c.AbortWithStatusJSON(httpStatus, jsonObj)
}

func (resp *GlobalResponsor) AbortWithError(c *gin.Context, httpStatus int, code int, msg string) {
	jsonObj := resp.createJsonObject(c, code, msg, nil)
	c.AbortWithStatusJSON(httpStatus, jsonObj)
}

// ========================================private functions & methods ======================================

/*
* 构造响应包函数
* params:
*	code:业务层的表示返回的代码
*	msg:业务层与code对应的消息
*	data:表示返回的数据
 */
func (resp *GlobalResponsor) createJsonObject(c *gin.Context, code int, msg string, data interface{}) interface{} {
	reqInfo, err := c.Get(ConstRequestId)
	reqId := ""

	if !err {
		reqId = "-1" //表示无效的ID
	}

	reqId = reqInfo.(*RequestInfo).RequestId
	reqReceivedTimestamp := reqInfo.(*RequestInfo).QueryStringTimestamp
	reqMethod := reqInfo.(*RequestInfo).RequestMethod

	respMsg := CommonResponseMsg{
		ReqID:                reqId,
		ReqMethod:            reqMethod,
		RespCode:             code,
		RespMsg:              msg,
		ReqReceivedTimestamp: reqReceivedTimestamp,
		ResponseTimestamp:    common.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp,
		Data:                 data,
	}

	return respMsg
}
