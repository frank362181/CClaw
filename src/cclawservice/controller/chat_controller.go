package controller

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"my.favorite/cclawservice/chat"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/request"
	"my.favorite/cclawservice/service"
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/concurrency"
	"my.favorite/platformutility/webbase/middleware/basic"
	baseController "my.favorite/platformutility/webbase/middleware/controller"
	pService "my.favorite/platformutility/webbase/middleware/service"
)

type ChatController struct {
	baseController.BaseController

	concurrent concurrency.IConcurrencyController //全局并发控制器
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

func GetChatController(svc pService.IService, cc concurrency.IConcurrencyController) *ChatController {
	if svc == nil || cc == nil {
		panic("parameters of chatting service cannot be nil")
	}

	chatController.Service = svc
	chatController.concurrent = cc

	return chatController
}

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
//	@Param			body		body			request.ChatRequest	true	"聊天内容"
//	@Success		200			{object}		request.ChatResponse
//	@Failure		400			{object}		basic.CommonResponse
//	@Router			/api/chat [post]
func (cc *ChatController) Chat(c *gin.Context) {
	chatService, ok := cc.getChattingService(c)
	if !ok {
		return
	}

	// 全局聊天并发闸:同步/流式共用总预算,满则 503(快速拒绝,不排队)
	var release func()
	var err error
	if release, err = cc.concurrent.TryAcquire(context.Background(), common.BlankString, 0); err != nil {
		respondKind(c, myException.ChatServerBusyKind)
		return
	}
	defer release()

	// 构建command
	cmd, ok := cc.bind(c)
	if !ok {
		return
	}

	reply, err := chatService.Send(c.Request.Context(), *cmd)
	if err != nil {
		respondChatError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess,
		&request.ChatResponse{
			SenderId:    cmd.Sender,
			SessionId:   cmd.SessionId,
			ChatType:    cmd.ChatType,
			ChatChannel: cmd.ChatChannel,
			TurnId:      cmd.TurnID, // 回显规范化后的值,保证客户端定位本轮
			Reply:       reply.Content,
			RespTime:    reply.At,
		},
	)
}

// ChatStream 接收一条聊天消息并以 SSE 流式返回 AI 应答
//
//	@Summary		流式发送聊天消息
//	@Description	接收 cclawchat 聊天界面发来的消息,以 SSE(Server-Sent Events)流式返回 AI 应答;增量在 data 的 {"reply":"..."} 中,[DONE] 表示结束,error 事件表示失败
//	@Tags			聊天管理
//	@Accept			json
//	@Produce		text/event-stream
//	@Param			AUTHORIZATION	header		string			true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string			true	"盐"
//	@Param			TIMESTAMP		query		string			true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string			true	"对请求内容的签名秒数"
//	@Param			body			body		request.ChatRequest	true	"聊天内容"
//	@Failure		400			{object} 		basic.CommonResponse
//	@Success		200			{object}		request.ChatResponse
//	@Router			/api/chat/stream [post]
func (cc *ChatController) ChatStream(c *gin.Context) {
	chatService, ok := cc.getChattingService(c)
	if !ok {
		return
	}

	// 全局聊天并发闸:同步/流式共用总预算,满则 503(此时尚未写 SSE 头,可回普通 JSON)
	var release func()
	var err error
	if release, err = cc.concurrent.TryAcquire(c.Request.Context(), common.BlankString, 0); err != nil {
		respondKind(c, myException.ChatServerBusyKind)
		return
	}
	defer release()

	cmd, ok := cc.bind(c)
	if !ok {
		return
	}

	// cmd.TurnID 已由NewCommand 保证非空
	presenter, ok := NewSSEPresenter(c, cmd.TurnID, func(turnID string) {
		_ = chatService.Interrupt(cmd.Sender, turnID)
	})
	if !ok {
		return
	}
	defer presenter.Close()

	if _, err := chatService.Stream(c.Request.Context(), *cmd, presenter); err != nil {
		presenter.Fail(err)
		return
	}

	presenter.Done()
}

// InterruptChat 中断进行中的流式聊天
//
//	@Summary		中断流式聊天
//	@Description	用户点击中断按钮后,立即停止指定会话正在进行的思考/推理/流式输出,并通知大模型停止。仅在聊天已开始(流式进行中)时支持,否则返回"未在聊天"错误。
//	@Tags			聊天管理
//	@Accept			json
//	@Produce		json
//	@Param			AUTHORIZATION	header		string	true	"Login成功返回后得到的token"
//	@Param			NONCE			query		string	true	"盐"
//	@Param			TIMESTAMP		query		string	true	"数值,表示请求端请求的时间(秒数)"
//	@Param			SIGNATURE		query		string	true	"对请求内容的签名秒数"
//	@Param			body		body		request.ChatInterruptRequest	true	"中断参数"
//	@Success		200			{object}	basic.CommonResponse
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/chat/interrupt [post]
func (cc *ChatController) InterruptChat(c *gin.Context) {
	chatService, ok := cc.getChattingService(c)
	if !ok {
		return
	}

	// 发送者以服务端认证信息为准(认证 interceptor 已写入上下文),确保只能中断自己的轮次
	loginSession, ok := getAuthentication(c)
	if !ok {
		return
	}

	var req request.ChatInterruptRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil || strings.TrimSpace(req.TurnId) == "" {
		respondKind(c, myException.ChatParameterKind)
		return
	}

	if err := chatService.Interrupt(loginSession.LoginName, req.TurnId); err != nil {
		respondChatError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// ================================ private functions & methods ===============================

// 将一次HTTP请求转换为chat.Command
func (cc *ChatController) bind(c *gin.Context) (*chat.Command, bool) {
	login, ok := getAuthentication(c)
	if !ok {
		return nil, false
	}

	var req request.ChatRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		respondKind(c, myException.ChatParameterKind)
		return nil, false
	}

	cmd, err := chat.NewCommand(login.ComCode, login.LoginName, &req)
	if err != nil {
		respondChatError(c, myException.ChatParameterKind)
		return nil, false
	}

	return cmd, true
}

// getChatService 获取当前注入的 ChatService
func (cc *ChatController) getChattingService(c *gin.Context) (*service.ChattingService, bool) {
	svc, ok := cc.GetService().(*service.ChattingService)
	if !ok || svc == nil {
		//内部服务器错误
		basic.GetBaseResponser().WriteWithErrorAndAbort(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return nil, false
	}
	return svc, true
}
