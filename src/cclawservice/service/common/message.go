package common

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

	// EnableThinking 是否开启 thinking(Qwen3 等思考型模型专用;omitempty 为 nil 时不发送)。
	// 约束:该类模型非流式调用必须显式 enable_thinking=false,否则网关返回 400。
	EnableThinking *bool `json:"enable_thinking,omitempty"`
}
