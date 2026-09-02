package controller

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	cclawcommon "my.favorite/cclawservice/common"
	model2 "my.favorite/cclawservice/controller/model"
	exception2 "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/service"
	"my.favorite/cclawservice/utility"
	"my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
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

	// 发送者与单位编码以服务端认证信息为准(认证 interceptor 已写入上下文,防止客户端伪造身份)
	sender := loginSession.LoginName
	comCode := loginSession.ComCode
	resp, err := chatService.SendMessage(c.Request.Context(), &req, sender, comCode)
	if err != nil {
		cc.respondChatError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, resp)
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
//	@Param			body		body		model.ChatRequest	true	"聊天内容"
//	@Success		200			{object}	model.ChatResponseEx
//	@Router			/api/chat/stream [post]
func (cc *ChatController) ChatStream(c *gin.Context) {
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

	// 先确认支持 flush,再设置 SSE 头(头一旦按 SSE 发送,后续错误只能走事件通道)
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		basic.GetBaseResponser().Failure(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
		return
	}

	// 设置 SSE 响应头(必须在任何写入之前)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // 关闭反向代理缓冲,保证逐块到达浏览器

	sender := loginSession.LoginName
	comCode := loginSession.ComCode

	// 逐块转发上游增量给浏览器(仅在 LLM 流式读取循环内同步调用,无并发写竞争);
	// 内容开始前先以 status 事件报告 Agent 阶段(解析任务/思考等)
	streamed := false
	_, err := chatService.ChatStream(c.Request.Context(), &req, sender, comCode,
		func(delta string) {
			streamed = true
			c.SSEvent("message", gin.H{"reply": delta})
			flusher.Flush()
		},
		func(status, text string) {
			c.SSEvent("status", gin.H{"status": status, "text": text})
			flusher.Flush()
		},
		func(delta string) {
			// 思考过程增量:独立事件转发,供前端实时展示思考过程(不并入正式回复)
			c.SSEvent("thinking", gin.H{"delta": delta})
			flusher.Flush()
		},
		func(evt service.ToolEvent) {
			// 工具调用进度(intent/running/done/error):实时转发,供前端渲染工具调用卡片
			c.SSEvent("tool", gin.H{"phase": evt.Phase, "seq": evt.Seq, "name": evt.Name, "detail": evt.Detail})
			flusher.Flush()
		})

	if err != nil {
		if errors.Is(err, exception2.ChatCanceledErr) {
			// 区分"客户端断开"与"服务端主动中断(用户点击中断按钮)":
			//   - 客户端断开:请求上下文已取消,响应写不出去,静默结束;
			//   - 服务端中断:浏览器仍保持 SSE 连接,发送结束标记让前端正常收尾,
			//     避免前端误判为"引擎不可用"或一直等待。
			if c.Request.Context().Err() != nil {
				return
			}
			if streamed {
				c.SSEvent("message", "[DONE]")
			} else {
				// 思考/推理阶段被中断(尚无正式回复):发 interrupt 事件告知前端
				c.SSEvent("interrupt", gin.H{"msg": cclawcommon.ConstErrorChatCanceledMsg})
			}
			flusher.Flush()
			return
		}
		if streamed {
			// 已向浏览器输出过内容:保留已送达部分,以 [DONE] 正常收尾,
			// 避免 error 事件把已输出的回复覆盖成"引擎不可用"
			// (上游错误已在 chat_service.LlmError 记录日志,不影响诊断)。
			c.SSEvent("message", "[DONE]")
			flusher.Flush()
			return
		}
		// 头部已作为 SSE 发送,无法再返回普通 JSON,改以 error 事件告知前端
		c.SSEvent("error", gin.H{"msg": cclawcommon.ConstErrorChatLLMMsg})
		flusher.Flush()
		return
	}

	// 正常结束标记
	c.SSEvent("message", "[DONE]")
	flusher.Flush()
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
//	@Param			body		body		model.ChatInterruptRequest	true	"中断参数"
//	@Success		200			{object}	basic.CommonResponse
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/chat/interrupt [post]
func (cc *ChatController) InterruptChat(c *gin.Context) {
	chatService, ok := cc.getChatService(c)
	if !ok {
		return
	}

	loginSession, ok := utility.GetAuthentication(c)
	if !ok {
		return
	}

	var req model2.ChatInterruptRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil || strings.TrimSpace(req.SessionId) == "" {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorChatParameter, cclawcommon.ConstErrorChatParameterMsg, nil)
		return
	}

	// 发送者以服务端认证信息为准(认证 interceptor 已写入上下文),确保只能中断自己的聊天
	sender := loginSession.LoginName
	if err := chatService.InterruptChat(sender, req.SessionId); err != nil {
		cc.respondChatError(c, err)
		return
	}

	basic.GetBaseResponser().Success(c, http.StatusOK, basic.ConstSuccessCode, basic.ConstSuccess, nil)
}

// ======================== private functions & methods =======================

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
	case errors.Is(err, pException.ParameterEmptyError):
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorChatParameter, cclawcommon.ConstErrorChatParameterMsg, nil)
	case errors.Is(err, pException.UnMatchError):
		basic.GetBaseResponser().Failure(c, http.StatusNotFound, cclawcommon.ConstErrorChatAgentNotFound, cclawcommon.ConstErrorChatAgentNotFoundMsg, nil)
	case errors.Is(err, exception2.ChatLLMErr):
		basic.GetBaseResponser().Failure(c, http.StatusBadGateway, cclawcommon.ConstErrorChatLLM, cclawcommon.ConstErrorChatLLMMsg, nil)
	case errors.Is(err, exception2.ChatCanceledErr):
		// 客户端已断开/取消请求:响应写不出去,静默结束即可
		return
	case errors.Is(err, exception2.ChatNotActiveErr):
		// 中断请求时目标会话没有进行中的聊天(聊天未开始或已结束)
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorChatNotActive, cclawcommon.ConstErrorChatNotActiveMsg, nil)
	default:
		basic.GetBaseResponser().Failure(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
	}
}
