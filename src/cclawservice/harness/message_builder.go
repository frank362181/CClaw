package harness

import (
	"fmt"
	"strings"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
)

// BuildSimpleChatMessage 以system/user 文本构建简单的prompt
func BuildSimpleChatMessage(sysMsg, userMsg string) []llmproto.ChatMessage {
	return []llmproto.ChatMessage{
		{Role: hCommon.RoleSystem, Content: sysMsg},
		{Role: hCommon.RoleUser, Content: userMsg},
	}
}

func BuildChatMessage(systemPrompt *dataobject.SystemPrompt, req *request.ChatRequest) []llmproto.ChatMessage {
	return []llmproto.ChatMessage{
		{Role: hCommon.RoleSystem, Content: systemPrompt.Prompt},
		{Role: hCommon.RoleUser, Content: req.ReqMsg},
	}

}

// BuildClassifySystemPrompt 构建用来通过用户问题的意图询问LLM返回合适Agent的 prompt。
// 简介优先使用各 Agent 工作区 AGENTS.md 的摘要(已由调用方解析进 ClassifyAgent.Label)。
func BuildClassifySystemPrompt(agents []dataobject.ClassifyAgent) string {
	var sb strings.Builder
	sb.WriteString("你是一个智能体路由器。请根据用户的问题,从下面的智能体列表中选择最合适的一个。\n")
	sb.WriteString("智能体列表(格式: 编号|名称|简介):\n")
	for _, a := range agents {
		label := strings.TrimSpace(a.Label)
		if label == "" {
			label = strings.TrimSpace(a.AgentName)
		}
		_, _ = fmt.Fprintf(&sb, "- %s|%s|%s\n", a.AgentId, a.AgentName, label)
	}
	sb.WriteString("只回答最合适的智能体编号(如 agent_001),不要输出任何其他内容;如果不确定,回答 default。")
	return sb.String()
}

func BuildChatCompletionRequestWithString(llmProvider dataobject.TLlm, sysMsg string, userMsg string, maxTokens int, stream bool) llmproto.ChatCompletionRequest {
	messages := BuildSimpleChatMessage(sysMsg, userMsg)
	req := llmproto.ChatCompletionRequest{
		Model:     llmProvider.GetModelName(),
		Messages:  messages,
		Stream:    stream,
		MaxTokens: nil,
	}
	if maxTokens > 0 {
		req.MaxTokens = &maxTokens
	}

	// 仅非流式请求按 provider 配置发送 enable_thinking(非流模式下是否思考);
	// 流式请求保持 nil:thinking 由流式默认开启并经 reasoning_content 返回,不在此关闭。
	if !stream {
		req.EnableThinking = new(llmProvider.ThkNonStreaming)
	}
	return req
}

// BuildChatCompletionRequest 构造 chat/completions 请求体。
// max_tokens 按输入 token 估算做动态计算,保证"估算输入 + max_tokens ≤ context_window",
// 避免长输入时因超出供应商上下文窗口而报错(同步/流式两条路径共用)。
// 未配置 ContextWindow(0)时不发送 max_tokens,由供应商使用其默认输出上限。
func BuildChatCompletionRequest(llmProvider dataobject.TLlm, systemPrompt *dataobject.SystemPrompt, chatReq *request.ChatRequest, stream bool) llmproto.ChatCompletionRequest {
	messages := BuildChatMessage(systemPrompt, chatReq)
	req := llmproto.ChatCompletionRequest{
		Model:    llmProvider.GetModelName(),
		Messages: messages,
		Stream:   stream,
	}
	// 仅非流式请求按 provider 配置发送 enable_thinking;流式保持 nil(不关闭 thinking)
	if !stream {
		req.EnableThinking = new(llmProvider.ThkNonStreaming)
	}

	// 动态裁剪 max_tokens:仅当 ContextWindow 已配置时才计算,否则返回 nil
	maxTokens := ctxmgr.DynamicMaxTokens(llmProvider, messages)
	if maxTokens == 0 {
		req.MaxTokens = nil //应用供应商的默认配置
	} else {
		req.MaxTokens = new(maxTokens)
	}

	return req
}

// BuildChatCompletionRequestWithTool 构造带工具的 chat/completions 请求体。
// max_tokens 沿用与普通请求一致的动态推导(dynamicMaxTokens)。
func BuildChatCompletionRequestWithTool(llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, tools []llmproto.Tool, stream bool) llmproto.ChatCompletionRequest {
	req := llmproto.ChatCompletionRequest{
		Model:    llmProvider.GetModelName(),
		Messages: messages,
		Stream:   stream,
		Tools:    tools,
	}
	// 仅非流式请求按 provider 配置发送 enable_thinking;流式保持 nil(不关闭 thinking)
	if !stream {
		req.EnableThinking = new(llmProvider.ThkNonStreaming)
	}
	mt := ctxmgr.DynamicMaxTokens(llmProvider, messages)
	if mt == 0 {
		req.MaxTokens = nil // 应用供应商默认
	} else {
		req.MaxTokens = &mt
	}
	return req
}

// BuildToolCallWithContentMessage 构造携带文本与工具调用的 assistant 消息(工具循环回填历史用)。
// 模型可能在一轮里既输出文本又请求工具(OpenAI 规范允许 content 与 tool_calls 并存),
// 文本须一并回填,否则下一轮 LLM 看不到自己刚说的话(上下文缺失)。
func BuildToolCallWithContentMessage(content string, calls []llmproto.ToolCall) llmproto.ChatMessage {
	return llmproto.ChatMessage{Role: hCommon.RoleAssistant, Content: content, ToolCalls: calls}
}

// BuildToolCallMessage 构造纯工具调用(无文本)的 assistant 消息(向后兼容入口)。
func BuildToolCallMessage(calls []llmproto.ToolCall) llmproto.ChatMessage {
	return BuildToolCallWithContentMessage("", calls)
}

// BuildToolResultMessage 构造工具执行结果消息(role=tool,按 tool_call_id 关联)。
func BuildToolResultMessage(toolCallID, content string) llmproto.ChatMessage {
	return llmproto.ChatMessage{Role: hCommon.RoleTool, ToolCallID: toolCallID, Content: content}
}
