package controller

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	cclawcommon "my.favorite/cclawservice/common"
	exception2 "my.favorite/cclawservice/exception"
	model "my.favorite/cclawservice/request"
	"my.favorite/cclawservice/service"
	"my.favorite/cclawservice/service/ccc"
	"my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/webbase/middleware/basic"
	baseController "my.favorite/platformutility/webbase/middleware/controller"
	pService "my.favorite/platformutility/webbase/middleware/service"
)

type ChatController struct {
	baseController.BaseController

	streamGate *ccc.ConcurrencyGate // SSE 流式在飞连接闸
	chatGate   *ccc.ConcurrencyGate // 全局聊天在飞请求闸
}

var chatController = common.GetSingleton(func() *ChatController {
	return &ChatController{
		BaseController: baseController.BaseController{
			Type:    basic.APIGroupRequire,
			Name:    "chatController",
			Service: nil,
		},
		streamGate: nil,
		chatGate:   nil,
	}
}).Instance()

func GetChatController(svc pService.IService, sGate, cGate *ccc.ConcurrencyGate) *ChatController {
	chatController.Service = svc
	chatController.streamGate = sGate
	chatController.chatGate = cGate
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
//	@Param			body		body		model.ChatRequest	true	"聊天内容"
//	@Success		200			{object}	model.ChatResponseEx
//	@Failure		400			{object}	basic.CommonResponse
//	@Router			/api/chat [post]
func (cc *ChatController) Chat(c *gin.Context) {
	chatService, ok := cc.getChatService(c)
	if !ok {
		return
	}

	loginSession, ok := GetAuthentication(c)
	if !ok {
		return
	}

	var req model.ChatRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorChatParameter, cclawcommon.ConstErrorChatParameterMsg, nil)
		return
	}

	// 全局聊天并发闸:同步/流式共用总预算,满则 503(快速拒绝,不排队)
	if !cc.chatGate.TryAcquire() {
		basic.GetBaseResponser().Failure(c, http.StatusServiceUnavailable, cclawcommon.ConstErrorChatServerBusy, cclawcommon.ConstErrorChatServerBusyMsg, nil)
		return
	}
	defer cc.chatGate.Release()

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

	loginSession, ok := GetAuthentication(c)
	if !ok {
		return
	}

	var req model.ChatRequest
	if err := c.ShouldBindBodyWithJSON(&req); err != nil {
		basic.GetBaseResponser().Failure(c, http.StatusBadRequest, cclawcommon.ConstErrorChatParameter, cclawcommon.ConstErrorChatParameterMsg, nil)
		return
	}

	// 全局聊天并发闸:同步/流式共用总预算,满则 503(此时尚未写 SSE 头,可回普通 JSON)
	if !cc.chatGate.TryAcquire() {
		basic.GetBaseResponser().Failure(c, http.StatusServiceUnavailable, cclawcommon.ConstErrorChatServerBusy, cclawcommon.ConstErrorChatServerBusyMsg, nil)
		return
	}
	defer cc.chatGate.Release()

	// SSE 流并发闸:达到每实例在飞流式上限直接 503(此时尚未写 SSE 头,可回普通 JSON)
	if !cc.streamGate.TryAcquire() {
		basic.GetBaseResponser().Failure(c, http.StatusServiceUnavailable, cclawcommon.ConstErrorChatServerBusy, cclawcommon.ConstErrorChatServerBusyMsg, nil)
		return
	}
	defer cc.streamGate.Release()

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

	// SSE 写端安全(单 goroutine 泵内同步调用,无并发写竞争):
	// 每次写入前检查请求上下文;一旦客户端断开(ctx 取消),立即停止继续向死连接
	// 写/Flush,并主动取消上游流式读取(与中断同源),避免"写错误被忽略后仍空转转发"。
	disconnected := false
	emit := func(name string, payload gin.H) {
		if disconnected {
			return
		}
		if c.Request.Context().Err() != nil {
			disconnected = true
			// 提前取消上游:命中断开立即退流(会话号在 ChatStream 内已生成/规整)
			if strings.TrimSpace(req.SessionId) != "" {
				_ = chatService.InterruptChat(sender, req.SessionId)
			}
			return
		}
		c.SSEvent(name, payload)
		flusher.Flush()
	}

	// 逐块转发上游增量给浏览器;内容开始前先以 status 事件报告 Agent 阶段(解析任务/思考等)
	streamed := false
	_, err := chatService.ChatStream(c.Request.Context(), &req, sender, comCode,
		func(delta string) {
			streamed = true
			emit("message", gin.H{"reply": delta})
		},
		func(status, text string) {
			emit("status", gin.H{"status": status, "text": text})
		},
		func(delta string) {
			// 思考过程增量:独立事件转发,供前端实时展示思考过程(不并入正式回复)
			emit("thinking", gin.H{"delta": delta})
		},
		func(evt service.ToolEvent) {
			// 工具调用进度(intent/running/done/error):实时转发,供前端渲染工具调用卡片
			emit("tool", gin.H{"phase": evt.Phase, "seq": evt.Seq, "name": evt.Name, "detail": evt.Detail})
		})

	if err != nil {
		// 客户端已断开:响应写不出去,静默结束(不再尝试向死连接写任何事件)
		if c.Request.Context().Err() != nil {
			return
		}
		if errors.Is(err, exception2.ChatCanceledErr) {
			// 服务端主动中断(用户点击中断按钮):浏览器仍保持 SSE 连接,
			// 发送结束标记让前端正常收尾,避免误判为"引擎不可用"。
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

	// 正常结束标记(客户端已断开则跳过)
	if c.Request.Context().Err() == nil {
		c.SSEvent("message", "[DONE]")
		flusher.Flush()
	}
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

	loginSession, ok := GetAuthentication(c)
	if !ok {
		return
	}

	var req model.ChatInterruptRequest
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
	case errors.Is(err, exception2.ChatBusyErr):
		// 目标 Agent 已达并发上限(每 Agent 闸)
		basic.GetBaseResponser().Failure(c, http.StatusServiceUnavailable, cclawcommon.ConstErrorChatBusy, cclawcommon.ConstErrorChatBusyMsg, nil)
	default:
		basic.GetBaseResponser().Failure(c, http.StatusInternalServerError, basic.ConstInternalServerError, basic.ConstInternalServerErrorMsg, nil)
	}
}
