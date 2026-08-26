package controller

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	cclawcommon "my.favorite/cclawservice/common"
	model2 "my.favorite/cclawservice/controller/model"
	exception2 "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/service"
	"my.favorite/cclawservice/utility"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/webbase/middleware/basic"
	baseController "my.favorite/platformutility/webbase/middleware/controller"
	pService "my.favorite/platformutility/webbase/middleware/service"
)

type ChatController struct {
	baseController.BaseController
}

var chatController = common.GetSingleton(func() *ChatController {
	return &ChatController{
		BaseController: baseController.BaseController{
			Type:    basic.APIGroupRequire,
			Name:    "chatController",
			Service: nil,
		},
	}
}).Instance()

func GetChatController(svc pService.IService) *ChatController {
	chatController.Service = svc
	return chatController
}

// getChatService 获取当前注入的 ChatService
func (cc *ChatController) getChatService(c *gin.Context) (*service.ChatService, bool) {
	svc, ok := cc.GetService().(*service.ChatService)
	if !ok || svc == nil {
		//内部服务器错误
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return nil, false
	}
	return svc, true
}

// respondChatError 将聊天业务错误映射为 HTTP 状态码
func (cc *ChatController) respondChatError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, exception2.ChatParameterErr):
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorChatParameter, cclawcommon.ConstErrorChatParameterMsg, nil)
	default:
		basic.GetBaseResponser().Failure(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
	}
}

// CreateSession
//
//	@Summary		创建新的会话
//	@Description	创建全新的聊天会话
//	@Tags			聊天管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Param			body		body		model.SessionRequest	true	"聊天内容"
//	@Success		200			{object}	model.SessionResponseEx
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/createSession [post]
func (cc *ChatController) CreateSession(c *gin.Context) {

}

// GetSessions
//
//	@Summary		获取会话历史列表
//	@Description	获取会话历史列表
//	@Tags			聊天管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Param			body		body		model.SessionsRequest	true	"聊天内容"
//	@Success		200			{object}	model.SessionsResponseEx
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/getSessions [post]
func (cc *ChatController) GetSessions(c *gin.Context) {}

// DeleteSession
//
//	@Summary		获取会话历史
//	@Description	获取会话历史
//	@Tags			聊天管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Param			body		body		model.SessionsRequest	true	"聊天内容"
//	@Success		200			{object}	model.SessionsResponseEx
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/deleteSession [post]
func (cc *ChatController) DeleteSession(c *gin.Context) {}

// Chat 接收一条聊天消息并返回应答
//
//	@Summary		发送聊天消息
//	@Description	接收 cclawchat 聊天界面发来的消息并返回 AI 应答
//	@Tags			聊天管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string				true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string				true	"盐"
//	@Param			TIMESTAMP		query		string				true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string				true	"对请求内容的签名秒数"
//	@Param			body		body		model.ChatRequest	true	"聊天内容"
//	@Success		200			{object}	model.ChatResponseEx
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/chat [post]
func (cc *ChatController) Chat(c *gin.Context) {
	chatService, ok := cc.getChatService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c)
	if !ok {
		return
	}

	var req model2.ChatRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorChatParameter, cclawcommon.ConstErrorChatParameterMsg, nil)
		return
	}

	// 发送者以服务端认证信息为准(认证 interceptor 已写入上下文,防止客户端伪造身份)
	sender := loginSession.LoginName
	resp, err := chatService.SendMessage(&req, sender)
	if err != nil {
		cc.respondChatError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, resp)
}

// DeleteChat
//
//	@Summary		删除聊天
//	@Description	删除聊天
//	@Tags			聊天管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Param			body		body		model.ChatsRequest	true	"聊天内容"
//	@Success		200			{object}	model.ChatsResponseEx
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/deleteChat [post]
func (cc *ChatController) DeleteChat(c *gin.Context) {

}
