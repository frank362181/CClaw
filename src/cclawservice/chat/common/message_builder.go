/*
* 定义消息构建函数
**/

package common

import (
	"my.favorite/cclawservice/common/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
)

func BuildUserMessage(content string) llmproto.ChatMessage {
	return llmproto.ChatMessage{
		Role:    "user",
		Content: content,
	}
}

func BuildSimpleAssistantMessage(content string) llmproto.ChatMessage {
	return llmproto.ChatMessage{
		Role:    hCommon.RoleAssistant,
		Content: content,
	}
}

func BuildAssistantMessage(content string, calls []llmproto.ToolCall) llmproto.ChatMessage {
	return llmproto.ChatMessage{
		Role:      hCommon.RoleAssistant,
		Content:   content,
		ToolCalls: calls,
	}
}

func BuildToolMessage(callID, content string) llmproto.ChatMessage {
	return llmproto.ChatMessage{
		Role:       hCommon.RoleTool,
		ToolCallID: callID,
		Content:    content,
	}
}

// BuildSimpleChatMessage 构建基于文本的ChatMessage
func BuildSimpleChatMessage(sysMsg, userMsg string) []llmproto.ChatMessage {
	return []llmproto.ChatMessage{
		{Role: hCommon.RoleSystem, Content: sysMsg},
		{Role: hCommon.RoleUser, Content: userMsg},
	}
}
