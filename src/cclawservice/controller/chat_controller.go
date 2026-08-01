package controller

import (
	"github.com/gin-gonic/gin"
	"my.favorite/cclawservice/model"
	_ "my.favorite/platformutility/webbase/middleware/basic"
	baseController "my.favorite/platformutility/webbase/middleware/controller"
)

type ChatController struct {
	baseController.BaseController
}

// Chat (聊天)
//
//	@Summary		聊天API
//	@Description	实现用户与系统的聊天
//	@Tags			用户聊天
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Param			body		body		model.ChatRequest	true	"聊天内容"
//	@Success		200			{object}	basic.CommonResponse
//	@Failure		400			{object}	model.ChatResponseEx
//	@Router			/chat [post]
func (cc *ChatController) Chat(c *gin.Context) {
	var chatRequest model.ChatRequest
	c.ShouldBindBodyWithJSON(&chatRequest)
}
