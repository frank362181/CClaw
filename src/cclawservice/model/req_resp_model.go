package model

import "my.favorite/platformutility/webbase/middleware/basic"

// ================ login request and response model ================

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	UserName      string `json:"userName"`
	DisplayName   string `json:"displayName"`
	Token         string `json:"token"`         // 用户登录成功后得到的token
	GenAt         int64  `json:"genAt"`         //token产生的时间
	ExpiredLength int64  `json:"expiredLength"` //token失效时长,unit = second
}

type LoginResponseEx struct {
	basic.CommonResponse
	Data LoginResponse `json:"data"`
}

// ================= chat request and response model =================

type NewChatRequest struct {
	SenderId    string `json:"sender_id"`    //谁创建
	ChatType    int    `json:"chat_type"`    //聊天类型
	ChatChannel int    `json:"chat_channel"` //聊天渠道

	AgentId string `json:"agent_id"` //与哪个Agent对话
}

type NewChatResponse struct {
}

type ChatRequest struct {
	SenderId   string `json:"sender_id"`   //发送者,如果为空,则采用系统默认的
	SenderName string `json:"sender_name"` //发送者名称,如果为空,则采用系统默认的
	ChatType   int    `json:"chat_type"`   //聊天类型，0/1:私聊/群聊

	ChatChannel int `json:"chat_channel"` //聊天的频道,1=WEB,2,FEISHU 等

	RequestMsg string `json:"request_msg"` //聊天的内容

	AgentId string `json:"agent_id"` //AgentID,如果为空,则采用系统默认的

	SessionId string `json:"session_id"` //表示会话的session，在一个会话中的就属于同一个session
}

type ChatResponse struct {
	SenderId string `json:"sender_id"` //发送者,如果为空,则采用系统默认的
	ChatType int    `json:"chat_type"` //聊天类型，0/1:私聊/群聊

	ChatChannel int    `json:"chat_channel"` //聊天的频道,1=WEB,2,FEISHU 等
	ChatID      string `json:"chat_id"`      //聊天的ID,用于标识一次聊天的唯一性

	AgentID string `json:"agent_id"` //AgentID,如果为空,则采用系统默认的

	StartMsgId int64 `json:"start_msg_id"` //本次聊天的起始消息ID,用于标识本次聊天的起始位置
}

type ChatResponseEx struct {
	basic.CommonResponse
	Data ChatResponse `json:"data"`
}
