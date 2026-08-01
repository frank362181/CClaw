package basic

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
)

type GlobalResponsor struct{}

var globalResponsor = common.GetSingleton(func() *GlobalResponsor {
	return &GlobalResponsor{}
}).Instance()

func GetBaseResponser() *GlobalResponsor {
	return globalResponsor
}

// Success 用于表示业务成功
func (resp *GlobalResponsor) Success(c *gin.Context, httpStatus int, code int, msg string, data interface{}) {
	jsonObj := resp.createJsonObject(c, code, msg, data)
	c.PureJSON(httpStatus, jsonObj)
}

// WriteWithErrorAndAbort 用于表示业务执行失败,但并不影响系统正常运行
func (resp *GlobalResponsor) WriteWithErrorAndAbort(c *gin.Context, httpStatus int, code int, msg string, data interface{}) {
	jsonObj := resp.createJsonObject(c, code, msg, data)
	c.PureJSON(httpStatus, jsonObj)
	c.Abort() //不能忽略
}

// AbortWithError 一般表示系统级错误,如http的系统错误等
func (resp *GlobalResponsor) AbortWithError(c *gin.Context, httpStatus int, code int, msg string, data interface{}) {
	jsonObj := resp.createJsonObject(c, code, msg, data)
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
	reqInfo, err := c.Get(ConstRequestTotal)
	if err {
		r := reqInfo.(*RequestInfo)
		respMsg := &CommonResponse{
			ReqID:             r.RequestId,
			ReqPath:           r.RequestPath,
			ReqMethod:         r.RequestMethod,
			RespCode:          code,
			RespMsg:           msg,
			ReqAtTimestamp:    r.ReqAtdTimestamp,
			ResponseTimestamp: common.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp,
			Data:              data,
		}

		resp.printLog(respMsg)
		return respMsg
	}

	respMsg := &CommonResponse{
		ReqID:             "",
		ReqPath:           "",
		ReqMethod:         "",
		RespCode:          code,
		RespMsg:           msg,
		ReqAtTimestamp:    -1,
		ResponseTimestamp: common.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp,
		Data:              data,
	}

	resp.printLog(respMsg)
	return respMsg
}

func (resp *GlobalResponsor) printLog(msg interface{}) {
	if jsonBytes, err := json.Marshal(msg); err == nil {
		logging.LogInfof(string(jsonBytes))
	} else {
		logging.LogInfof("%v", msg)
	}
}
