package model

// GenerateSessionRequest 创建一个新的Session 请求/*
func GenerateSessionRequest(senderId, sessionName string, chatType, chatChannel int) *SessionRequest {
	if len(senderId) == 0 || len(sessionName) == 0 {
		return nil
	}

	return nil
}

// GenerateSessionResponse 创建针对创建Session的响应/*
func GenerateSessionResponse(senderId, sessionName string, chatType, chatChannel int, sessionData string) *SessionResponse {
	return nil
}

// GenerateChatRequest 创建一个聊天请求/*
func GenerateChatRequest(senderId, sessionId, chatTxt string) *ChatRequest {
	return nil
}

func GenerateChatResponse(senderId, sessionId, chatTxt string) *ChatResponse {
	return nil
}
