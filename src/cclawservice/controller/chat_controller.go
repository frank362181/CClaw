package controller

import (
	"github.com/gin-gonic/gin"
	_ "my.favorite/platformutility/webbase/middleware/basic"
	baseController "my.favorite/platformutility/webbase/middleware/controller"
)

type ChatController struct {
	baseController.BaseController
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

// Chat
//
//	@Summary		新的聊天
//	@Description	新的聊天
//	@Tags			聊天管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Param			body		body		model.ChatRequest	true	"聊天内容"
//	@Success		200			{object}	model.ChatResponseEx
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/chat [post]
func (cc *ChatController) Chat(c *gin.Context) {

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
