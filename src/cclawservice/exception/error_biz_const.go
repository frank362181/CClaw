package exception

import "my.favorite/platformutility/exception"

const (
	ErrorIdBizBase = iota + exception.ErrorIdSystemBase
)

const (
	ErrorIdBizUserNamePasswordUnmatched = iota + ErrorIdBizBase + 1
	ErrorIdBizInvalidToken
	ErrorIdBizDatabase
	ErrorIdBizPrompt
	ErrorIdBizAgentWorkspace
	ErrorIdBizAgentInternal
	ErrorIdBizChatLLM
	ErrorIdBizChatCanceled
	ErrorIdBizChatNotActive
	ErrorIdBizChatBusy
	ErrorIdBizChatToolLoop
	ErrorIdBizChatLLMQuota
	ErrorIdBizChatRunTimeout
	ErrorIdBizChatLLMAuth
	ErrorIdBizChatLLMForbidden
	ErrorIdBizChatLLMNotFound
	ErrorIdBizChatLLMInvalidRequest
	ErrorIdBizChatLLMRateLimit
	ErrorIdBizChatLLMContentFilter
	ErrorIdBizHarness
	ErrorIdBizLlmStatus //流式聊天过程中LLM的状态异常
)
