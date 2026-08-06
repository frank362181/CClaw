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

// Failure 表示业务执行失败
// /*此时httpStatus一般为HttpStatus.OK
func (resp *GlobalResponsor) Failure(c *gin.Context, httpStatus int, code int, msg string, data interface{}) {
	jsonObj := resp.createJsonObject(c, code, msg, data)
	c.PureJSON(httpStatus, jsonObj)
}

// WriteWithErrorAndAbort
// /* 此方法被调用后，后续的filter,interceptor等处理将会被终止,此时httpStatus一般为非HttpStatus.OK
func (resp *GlobalResponsor) WriteWithErrorAndAbort(c *gin.Context, httpStatus int, code int, msg string, data interface{}) {
	jsonObj := resp.createJsonObject(c, code, msg, data)
	c.PureJSON(httpStatus, jsonObj)
	c.Abort() //不能忽略
}

// AbortWithError
// /* AbortWithError 一般表示系统级错误,如http的系统错误等,此时httpStatus一般为非HttpStatus.OK
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
			ReqID:         r.RequestId,
			ReqPath:       r.RequestPath,
			ReqMethod:     r.RequestMethod,
			ReqTimestamp:  r.ReqAtdTimestamp,
			RespTimestamp: common.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp,

			RespCode: code,
			RespMsg:  msg,

			Data: data,
		}

		resp.printLog(respMsg)
		return respMsg
	}

	respMsg := &CommonResponse{
		ReqID:         "",
		ReqPath:       "",
		ReqMethod:     "",
		ReqTimestamp:  -1,
		RespTimestamp: common.GetDatetimeUtilityInstance().GetCurrentDatetime().MilliTimestamp,

		RespCode: code,
		RespMsg:  msg,

		Data: data,
	}

	resp.printLog(respMsg)
	return respMsg
}

/*
* 只是记录响应元数据
 */
func (resp *GlobalResponsor) printLog(msg interface{}) {
	if r, ok := msg.(*CommonResponse); ok {
		//只是打印响应数据的元数据，不打印具体内容
		logging.LogInfof("response reqId=%s,path=%s,method=%s,timestamp=%d,code=%d,msg=%s",
			r.ReqID, r.ReqPath, r.ReqMethod, r.ReqTimestamp, r.RespCode, r.RespMsg)
		return
	}

	if jsonBytes, err := json.MarshalIndent(msg, "", " "); err == nil {
		logging.LogInfof(string(jsonBytes))
	}
}
