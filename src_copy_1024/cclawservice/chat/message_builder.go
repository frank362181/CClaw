/*
* 定义消息构建函数
**/

package chat

import (
	"my.favorite/cclawservice/harness/api/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
)

func buildAssistantMessage(content string, calls []llmproto.ToolCall) llmproto.ChatMessage {
	return llmproto.ChatMessage{
		Role:      hCommon.RoleAssistant,
		Content:   content,
		ToolCalls: calls,
	}
}

func buildToolMessage(callID, content string) llmproto.ChatMessage {
	return llmproto.ChatMessage{
		Role:       hCommon.RoleTool,
		ToolCallID: callID,
		Content:    content,
	}
}
