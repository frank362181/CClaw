package llm

import (
	"my.favorite/cclawservice/controller/model"
	"my.favorite/cclawservice/model/dataobject"
)

// ChatMessage 一条对话消息
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest chat/completions 请求体(OpenAI 兼容)
type ChatCompletionRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	MaxTokens *int          `json:"max_tokens,omitempty"`
	Stream    bool          `json:"stream"`
}

func BuildChatMessage(systemPrompt *dataobject.SystemPrompt, req *model.ChatRequest) []ChatMessage {
	return []ChatMessage{
		{Role: "system", Content: systemPrompt.Prompt},
		{Role: "user", Content: req.ReqMsg},
	}
}

func BuildChatCompletionRequest(llmProvider dataobject.TLlm, systemPrompt *dataobject.SystemPrompt, chatReq *model.ChatRequest, stream bool) ChatCompletionRequest {
	messages := BuildChatMessage(systemPrompt, chatReq)
	req := ChatCompletionRequest{
		Model:    llmProvider.GetModelName(),
		Messages: messages,
		Stream:   stream,
	}

	return req
}
