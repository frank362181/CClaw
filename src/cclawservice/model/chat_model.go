package model

import "my.favorite/platformutility/webbase/middleware/basic"

// SessionRequest ================== session 管理命令=============================
type SessionRequest struct {
	SessionId   string `json:"sessionId"` //标识会话session
	SessionName string `json:"sessionName"`

	SenderId string `json:"senderId"` //发起新会话的userName

	ChatType    int `json:"chatType"`    //聊天类型
	ChatChannel int `json:"chatChannel"` //聊天渠道

	AgentId string `json:"agentId"` //session对应的调用目标Agent
}

type SessionResponse struct {
	SessionId   string `json:"sessionId"`
	SessionName string `json:"sessionName"` //session name
	SenderId    string `json:"senderId"`    //发起新会话的userName
	ChatType    int    `json:"chatType"`
	ChatChannel int    `json:"chatChannel"`

	GenAt int `json:"genAt"` // session创建的时间

	SessionData string `json:"sessionData"` //用来存放session所有交互历史的文件名,格式为JSON
}

type SessionResponseEx struct {
	basic.CommonResponse
	Data SessionResponse `json:"data"`
}

type SessionsRequest struct {
	SenderId string `json:"senderId"` //userName

	sessions []string //session list

	StartTime int64 `json:"startTime"`
	EndTime   int64 `json:"endTime"`
}
type SessionsResponse struct {
	SenderId string `json:"senderId"`
	sessions []string
}

type SessionsResponseEx struct {
	basic.CommonResponse
	Data SessionsResponse `json:"data"`
}

// ChatRequest =================聊天所需的信令==================
type ChatRequest struct {
	SenderId  string `json:"senderId"`  //发送者,如果为空,则采用系统默认的
	SessionId string `json:"sessionId"` //表示会话的session，在一个会话中的就属于同一个session

	ReqMsg string `json:"requestMsg"` //聊天的内容
	GenAt  int64  `json:"genAt"`      // 消息的发送时间
}

type ChatResponse struct {
	SenderId  string `json:"senderId"` //发送者,如果为空,则采用系统默认的
	SessionId string `json:"sessionId"`
	ChatType  int    `json:"chatType"` //聊天类型，0/1:私聊/群聊

	ChatChannel int   `json:"chatChannel"` //聊天的频道,1=WEB,2,FEISHU 等
	ChatId      int64 `json:"chatId"`      //本次聊天的消息ID,用于标识本次聊天的位置
}

type ChatResponseEx struct {
	basic.CommonResponse
	Data ChatResponse `json:"data"`
}

// ChatsRequest =================== 消息管理命令========================
type ChatsRequest struct {
	SenderId string   `json:"senderId"`
	ChatIds  []string `json:"chatIds"`
}

type ChatsResponse struct {
	SenderId string   `json:"senderId"`
	ChatIds  []string `json:"chatIds"`
}
type ChatsResponseEx struct {
	basic.CommonResponse
	Data ChatsRequest `json:"data"`
}
