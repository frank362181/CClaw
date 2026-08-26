/**
* Chat 业务服务
*
* 职责:接收 cclawchat 侧边栏聊天按钮发来的聊天消息。
* 当前阶段(先接通 API 通道):
*   - 校验消息内容并做长度限制,解析/生成会话标识
*   - 发送者以服务端认证信息为准(不信任客户端传入的 senderId,防伪造)
*   - 返回应答(LLM 引擎接入前的演示应答),后续可平滑扩展为真实模型调用
*
* 与 LoginService/AgentService 保持一致:组合 baseService.BaseService + 工厂 GetChatService()。
**/

package service

import (
	"strings"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/controller/model"
	myException "my.favorite/cclawservice/exception"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/exception"
	baseService "my.favorite/platformutility/webbase/middleware/service"
)

// MaxChatMessageLen 单条聊天消息的字符上限(防超大请求体滥用)
const MaxChatMessageLen = 4096

// ChatService 处理聊天消息
type ChatService struct {
	baseService.BaseService
}

// GetChatService 构建聊天服务
func GetChatService() *ChatService {
	return &ChatService{
		BaseService: baseService.BaseService{
			Name:        "chatService",
			Description: "receive and respond chat messages",
		},
	}
}

// SendMessage 接收并应答一条聊天消息。
// sender 为服务端认证得到的登录名(由认证 interceptor 写入请求上下文),
// 用于覆盖请求体中的 senderId,防止客户端伪造身份。
func (s *ChatService) SendMessage(req *model.ChatRequest, sender string) (*model.ChatResponse, error) {
	if req == nil {
		return nil, exception.ParameterEmptyErrorEx("chat request is nil")
	}

	// 1. 校验消息内容
	req.ReqMsg = strings.TrimSpace(req.ReqMsg)
	if req.ReqMsg == "" {
		return nil, myException.ChatParameterErrEx("chat message is empty")
	}
	if len([]rune(req.ReqMsg)) > MaxChatMessageLen {
		return nil, myException.ChatParameterErrEx("chat message is too long")
	}

	// 2. 发送者以服务端认证信息为准
	if strings.TrimSpace(sender) != "" {
		req.SenderId = strings.TrimSpace(sender)
	}

	// 3. 会话标识:客户端可传入;为空则由服务端生成(未来持久化到会话表)
	if strings.TrimSpace(req.SessionId) == "" {
		req.SessionId = pCommon.GenerateUUID()
	}

	// 4. 缺省类型
	if req.ChatType == 0 {
		req.ChatType = cCommon.ChatTypePrivate
	}
	if req.ChatChannel == 0 {
		req.ChatChannel = cCommon.ChatChannelWeb
	}

	// 5. 组装应答(演示应答:LLM 引擎接入后在此调用模型并替换 Reply)
	now := pCommon.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	resp := &model.ChatResponse{
		SenderId:    req.SenderId,
		SessionId:   req.SessionId,
		ChatType:    req.ChatType,
		ChatChannel: req.ChatChannel,
		ChatId:      now,
		Reply:       "已收到您的消息。智能回复引擎接入中,当前为演示应答。",
	}

	return resp, nil
}
