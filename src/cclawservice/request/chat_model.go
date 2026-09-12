package request

// ChatRequest =================聊天所需的信令==================
type ChatRequest struct {
	SenderId    string `json:"senderId"`    //发送者,以服务端认证信息为准
	SessionId   string `json:"sessionId"`   //表示会话的session，在一个会话中的就属于同一个session
	TurnId      string `json:"turnId"`      //turn id
	ReqMsg      string `json:"requestMsg"`  //聊天的内容
	ChatType    int    `json:"chatType"`    //聊天类型，0/1:私聊/群聊
	ChatChannel int    `json:"chatChannel"` //聊天的频道,1=WEB,2=FEISHU 等
	AgentId     string `json:"agentId"`     //可选:会话对应的目标Agent(暂未启用)
	GenAt       int64  `json:"genAt"`       // 消息的发送时间
}

// ChatInterruptRequest 中断进行中的流式聊天所需参数
// 仅需会话标识(turnId),对应发起聊天时 ChatRequest.turnId;发送者由服务端认证决定。
type ChatInterruptRequest struct {
	TurnId string `json:"turnId"` //会话标识,对应发起聊天时的 sessionId
}

type ChatResponse struct {
	SenderId    string `json:"senderId"` //发送者,以服务端认证信息为准
	SessionId   string `json:"sessionId"`
	ChatType    int    `json:"chatType"`    //聊天类型，0/1:私聊/群聊
	ChatChannel int    `json:"chatChannel"` //聊天的频道,1=WEB,2,FEISHU 等
	TurnId      string `json:"turnId"`      //本次聊天的消息ID,用于标识本次聊天的位置
	Reply       string `json:"reply"`       //AI 回复内容(由 LLM 引擎返回的真实回复)
	RespTime    int64  `json:"respTime"`    //消息响应的时间
}
