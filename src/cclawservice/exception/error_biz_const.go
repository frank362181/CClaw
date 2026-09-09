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
	ErrorIdBizAgentParameter
	ErrorIdBizAgentNotFound
	ErrorIdBizAgentConflict
	ErrorIdBizAgentWorkspace
	ErrorIdBizAgentInternal
	ErrorIdBizChatParameter
	ErrorIdBizChatAgentNotFound
	ErrorIdBizChatLLM
	ErrorIdBizChatCanceled
	ErrorIdBizChatNotActive
	ErrorIdBizChatBusy
	ErrorIdBizChatToolLoop
	ErrorIdBizChatLLMQuota
	ErrorIdBizChatRunTimeout
)
