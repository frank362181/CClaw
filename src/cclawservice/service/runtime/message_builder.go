package runtime

import (
	"fmt"
	"strings"

	"my.favorite/cclawservice/controller/model"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/service/ccc"
	"my.favorite/cclawservice/service/common"
)

// BuildSimpleChatMessage 以system/user 文本构建简单的prompt
func BuildSimpleChatMessage(sysMsg, userMsg string) []common.ChatMessage {
	return []common.ChatMessage{
		{Role: "system", Content: sysMsg},
		{Role: "user", Content: userMsg},
	}
}

func BuildChatMessage(systemPrompt *dataobject.SystemPrompt, req *model.ChatRequest) []common.ChatMessage {
	return []common.ChatMessage{
		{Role: "system", Content: systemPrompt.Prompt},
		{Role: "user", Content: req.ReqMsg},
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

func BuildChatCompletionRequestWithString(llmProvider dataobject.TLlm, sysMsg string, userMsg string, maxTokens int, stream bool) common.ChatCompletionRequest {
	messages := BuildSimpleChatMessage(sysMsg, userMsg)
	req := common.ChatCompletionRequest{
		Model:     llmProvider.GetModelName(),
		Messages:  messages,
		Stream:    stream,
		MaxTokens: nil,
	}
	if maxTokens > 0 {
		req.MaxTokens = &maxTokens
	}

	req.EnableThinking = nonStreamThinkingFlag(llmProvider, stream)
	return req
}

// nonStreamThinkingFlag 判定非流式请求是否需要显式关闭 thinking(enable_thinking=false)。
//
// Qwen3 等思考型模型在 OpenAI 兼容网关上的约束:非流式调用不允许 thinking 模式(未显式关闭会返回
// "parameter.enable_thinking must be set to false for non-streaming calls");thinking 只在流式调用
// 中经 reasoning_content 增量返回。因此:仅当 stream=false 且目标为 Qwen 系(厂商为 qwen/dashscope/
// alibaba/aliyun 等通义系,或模型名含 qwen)时附加 enable_thinking=false;流式请求与其他厂商不附加,
// 避免未知参数被部分网关拒绝。
func nonStreamThinkingFlag(llmProvider dataobject.TLlm, stream bool) *bool {
	if stream {
		return nil
	}
	name := strings.ToLower(llmProvider.GetModelName())
	vendor := strings.ToLower(strings.TrimSpace(llmProvider.Vendor))
	if strings.Contains(name, "qwen") || strings.Contains(vendor, "qwen") ||
		strings.Contains(vendor, "dashscope") || strings.Contains(vendor, "alibaba") || strings.Contains(vendor, "aliyun") {
		flag := false
		return &flag
	}
	return nil
}

// BuildChatCompletionRequest 构造 chat/completions 请求体。
// max_tokens 按输入 token 估算做动态计算,保证"估算输入 + max_tokens ≤ context_window",
// 避免长输入时因超出供应商上下文窗口而报错(同步/流式两条路径共用)。
// 未配置 ContextWindow(0)时不发送 max_tokens,由供应商使用其默认输出上限。
func BuildChatCompletionRequest(llmProvider dataobject.TLlm, systemPrompt *dataobject.SystemPrompt, chatReq *model.ChatRequest, stream bool) common.ChatCompletionRequest {
	messages := BuildChatMessage(systemPrompt, chatReq)
	req := common.ChatCompletionRequest{
		Model:    llmProvider.GetModelName(),
		Messages: messages,
		Stream:   stream,
	}
	req.EnableThinking = nonStreamThinkingFlag(llmProvider, stream)

	// 动态裁剪 max_tokens:仅当 ContextWindow 已配置时才计算,否则返回 nil
	maxTokens := ccc.DynamicMaxTokens(llmProvider, messages)
	if maxTokens == 0 {
		req.MaxTokens = nil //应用供应商的默认配置
	} else {
		req.MaxTokens = new(maxTokens)
	}

	return req
}

// BuildChatCompletionRequestWithTool 构造带工具的 chat/completions 请求体。
// max_tokens 沿用与普通请求一致的动态推导(dynamicMaxTokens)。
func BuildChatCompletionRequestWithTool(llmProvider dataobject.TLlm, messages []common.ChatMessage, tools []common.Tool, stream bool) common.ChatCompletionRequest {
	req := common.ChatCompletionRequest{
		Model:    llmProvider.GetModelName(),
		Messages: messages,
		Stream:   stream,
		Tools:    tools,
	}
	req.EnableThinking = nonStreamThinkingFlag(llmProvider, stream)
	mt := ccc.DynamicMaxTokens(llmProvider, messages)
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
func BuildToolCallWithContentMessage(content string, calls []common.ToolCall) common.ChatMessage {
	return common.ChatMessage{Role: "assistant", Content: content, ToolCalls: calls}
}

// BuildToolCallMessage 构造纯工具调用(无文本)的 assistant 消息(向后兼容入口)。
func BuildToolCallMessage(calls []common.ToolCall) common.ChatMessage {
	return BuildToolCallWithContentMessage("", calls)
}

// BuildToolResultMessage 构造工具执行结果消息(role=tool,按 tool_call_id 关联)。
func BuildToolResultMessage(toolCallID, content string) common.ChatMessage {
	return common.ChatMessage{Role: "tool", ToolCallID: toolCallID, Content: content}
}
