package runtime

import (
	"my.favorite/cclawservice/controller/model"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// ChatMessage 一条对话消息(OpenAI 兼容;工具调用场景支持 tool_calls / tool_call_id)
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolFunction 一个可调用的函数定义(OpenAI tools.function)
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Tool 一个工具定义(OpenAI tools)
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolCallFunction 模型请求调用的函数(参数为 JSON 字符串)
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall 模型发出的一次工具调用
// 工具循环中会把它(以 assistant 消息携带)与执行结果(tool 消息)一起回填到对话历史。
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallDelta 流式工具调用的增量片段(按 index 累积拼接 id/name/arguments)
type ToolCallDelta struct {
	Index    int                   `json:"index"`
	ID       string                `json:"id"`
	Type     string                `json:"type"`
	Function ToolCallFunctionDelta `json:"function"`
}

// ToolCallFunctionDelta 流式函数增量
// 模型流式返回工具调用时,arguments 会分片下发,需逐片拼接成完整 JSON。
type ToolCallFunctionDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionRequest chat/completions 请求体(OpenAI 兼容)
type ChatCompletionRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	MaxTokens *int          `json:"max_tokens,omitempty"`
	Stream    bool          `json:"stream"`
	Tools     []Tool        `json:"tools,omitempty"`
}

// BuildSimpleChatMessage 以system/user 文本构建简单的prompt
func BuildSimpleChatMessage(sysMsg, userMsg string) []ChatMessage {
	return []ChatMessage{
		{Role: "system", Content: sysMsg},
		{Role: "user", Content: userMsg},
	}
}

func BuildChatMessage(systemPrompt *dataobject.SystemPrompt, req *model.ChatRequest) []ChatMessage {
	return []ChatMessage{
		{Role: "system", Content: systemPrompt.Prompt},
		{Role: "user", Content: req.ReqMsg},
	}
}

// BuildChatCompletionRequest 构造 chat/completions 请求体。
// max_tokens 按输入 token 估算做动态计算,保证"估算输入 + max_tokens ≤ context_window",
// 避免长输入时因超出供应商上下文窗口而报错(同步/流式两条路径共用)。
// 未配置 ContextWindow(0)时不发送 max_tokens,由供应商使用其默认输出上限。
func BuildChatCompletionRequest(llmProvider dataobject.TLlm, systemPrompt *dataobject.SystemPrompt, chatReq *model.ChatRequest, stream bool) ChatCompletionRequest {
	messages := BuildChatMessage(systemPrompt, chatReq)
	req := ChatCompletionRequest{
		Model:    llmProvider.GetModelName(),
		Messages: messages,
		Stream:   stream,
	}

	// 动态裁剪 max_tokens:仅当 ContextWindow 已配置时才计算,否则返回 nil
	maxTokens := dynamicMaxTokens(llmProvider, messages)
	if maxTokens == 0 {
		req.MaxTokens = nil //应用供应商的默认配置
	} else {
		req.MaxTokens = new(maxTokens)
	}

	return req
}

// BuildChatCompletionRequestWithTool 构造带工具的 chat/completions 请求体。
// max_tokens 沿用与普通请求一致的动态推导(dynamicMaxTokens)。
func BuildChatCompletionRequestWithTool(llmProvider dataobject.TLlm, messages []ChatMessage, tools []Tool, stream bool) ChatCompletionRequest {
	req := ChatCompletionRequest{
		Model:    llmProvider.GetModelName(),
		Messages: messages,
		Stream:   stream,
		Tools:    tools,
	}
	mt := dynamicMaxTokens(llmProvider, messages)
	if mt == 0 {
		req.MaxTokens = nil // 应用供应商默认
	} else {
		req.MaxTokens = &mt
	}
	return req
}

// BuildToolCallMessage 构造携带工具调用的 assistant 消息(工具循环回填历史用)。
func BuildToolCallMessage(calls []ToolCall) ChatMessage {
	return ChatMessage{Role: "assistant", Content: "", ToolCalls: calls}
}

// BuildToolResultMessage 构造工具执行结果消息(role=tool,按 tool_call_id 关联)。
func BuildToolResultMessage(toolCallID, content string) ChatMessage {
	return ChatMessage{Role: "tool", ToolCallID: toolCallID, Content: content}
}

// dynamicMaxTokens 计算应发送的 max_tokens。由 context_window 与供应商输出上限共同约束:
//   - 取值为 min(context_window - 估算输入, llmProvider.MaxTokens);
//   - 剩余空间 ≤ 0 时取 1(此时请求大概率仍会被供应商拒绝,但保持确定性);
//   - 未配置 ContextWindow 时返回 0(不发送 max_tokens,由供应商用其默认值)。
//
// 修复背景:早期版本仅按 context_window 推导(如 1M 窗口 + 小输入 → max_tokens ≈ 1M),
func dynamicMaxTokens(llmProvider dataobject.TLlm, messages []ChatMessage) int {
	if llmProvider.ContextWindow == 0 {
		return 0 // 未配置返回 0 表示应用供应商的默认值
	}

	budget := int(llmProvider.ContextWindow) - estimateInputTokens(messages)
	if budget < 1 {
		budget = 1
	}
	if budget > llmProvider.MaxTokens {
		//确保不会超过LLM供应商单次输出的上限
		budget = llmProvider.MaxTokens
	}
	return budget
}

// estimateInputTokens 粗略估算 messages 的输入 token 数(非精确,仅用于动态裁剪)。
// 估算规则:
//   - 中文/日文/韩文(CJK)字符按 ~1.5 字符/token;
//   - 其他字符(如英文/数字/标点)按 ~4 字符/token;
//   - 每条消息附加 ~4 token 的角色/结构开销;
//   - 结果上浮 20% 作为安全余量,避免估算偏低导致"输入 + max_tokens"仍越界。
func estimateInputTokens(messages []ChatMessage) int {
	const (
		charsPerCJKToken   = 1.5
		charsPerAsciiToken = 4.0
		perMessageOverhead = 4
		safetyFactor       = 0.2
	)

	total := 0
	for _, m := range messages {
		cjk, other := 0, 0
		for _, r := range m.Content {
			if pCommon.IsCJK(r) {
				cjk++
			} else {
				other++
			}
		}
		total += perMessageOverhead + int(float64(cjk)/charsPerCJKToken) + int(float64(other)/charsPerAsciiToken)
	}
	return int(float64(total) * (1 + safetyFactor))
}
