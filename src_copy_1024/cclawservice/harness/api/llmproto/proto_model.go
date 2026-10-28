/**
* 上游 LLM 协议 DTO(OpenAI 兼容):请求体 / 应答体 / 流式块 / 消息与工具结构。
*
* 纯协议层,不依赖任何业务定义;供 harness(组装请求/解析应答)、service(拼装对话历史)
* 共同使用。同包文件:proto_error.go/classify_error.go/proto_utility.go。
**/

package llmproto

import "encoding/json"

// ==================== 请求侧(chat/completions 请求体) ====================

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

	// 本地工具调用在 **本轮** 上游应用中的序号,每轮从0开始
	// 		- 流式:取 delta.index(跨分片不变)
	//		- 非流式:取ToolCalls数组下标
	Index int `json:"-"`
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

	// ToolChoice 工具选择策略(OpenAI 兼容:auto / required / none;空串则不发送该字段)。
	// required 用于"必须实时取数"的工具(技能 front matter 的 force-call:true):
	// 模型看不到技能正文,若不给硬约束,它可能直接用上下文里的历史工具结果作答
	// (实测:天气技能复述了两天前的旧结果)。仅首轮强制,后续轮次交回 auto。
	ToolChoice string `json:"tool_choice,omitempty"`

	// EnableThinking 是否开启 thinking(Qwen3 等思考型模型专用;omitempty 为 nil 时不发送)。
	// 约束:该类模型非流式调用必须显式 enable_thinking=false,否则网关返回 400。
	EnableThinking *bool `json:"enable_thinking,omitempty"`

	//仅流式有意义
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
}

// 流式/同步应答的 finish_reason 取值(OpenAI 兼容):决定回答结束的原因。
const (
	FinishReasonStop          = "stop"           // 正常结束
	FinishReasonLength        = "length"         // 达到 max_tokens/上下文上限,内容被截断
	FinishReasonToolCalls     = "tool_calls"     // 模型请求调用工具(工具循环续跑)
	FinishReasonContentFilter = "content_filter" // 输出被内容安全策略过滤
)

// 工具选择策略(tool_choice)取值(OpenAI 兼容)
const (
	ToolChoiceAuto     = "auto"     // 模型自行决定(不发送该字段时的默认行为)
	ToolChoiceRequired = "required" // 必须调用工具(至少调用一个)
	ToolChoiceNone     = "none"     // 禁止调用工具
)

// ==================== 应答侧(chat/completions 应答体 / 流式块) ====================

// ChatCompletionChoice 一条候选回复
type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason *string     `json:"finish_reason,omitempty"`
}

// PromptTokensDetail OpenAI口径的输入token细分
type PromptTokensDetail struct {
	CachedTokens int `json:"cached_tokens"` //命中前缀缓存的输入的token数
}

// StreamOptions 流式可选参数:开启include_usage后上游在**最后一个 chunk**返回 usage
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// ChatCompletionUsage token 用量(可选,部分供应商流式返回时可能为空)
//
//	对于Agent向LLM发起请求时的prompt,遵循"静态在前,动态在后",那么token到底省了多少
//	唯一权威的数据是来自上游返回的缓存命中的token
type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`     // 本次请求的全部token,=请求的token+历史消息+工具调用结果+prompt token
	CompletionTokens int `json:"completion_tokens"` // 本次请求的输出token
	TotalTokens      int `json:"total_tokens"`      //prompt token + completion tokens

	// 前缀缓存命中DeepSeek 口径
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens"`

	// 前缀缓存命中 OpenAI口径
	PromptTokensDetails *PromptTokensDetail `json:"prompt_tokens_details"`
}

// CachedPromptTokens 归一化命中前缀缓存的输入token
func (u *ChatCompletionUsage) CachedPromptTokens() int {
	if u == nil {
		return 0
	}

	n := u.PromptCacheHitTokens
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > n {
		n = u.PromptTokensDetails.CachedTokens
	}

	return n
}

func (u *ChatCompletionUsage) HasCacheReport() bool {
	if u == nil {
		return false
	}

	return u.PromptCacheHitTokens > 0 || u.PromptCacheMissTokens > 0 || u.PromptTokensDetails != nil
}

// ChatCompletionResponse chat/completions 应答体
// Error 保留原始 JSON(error 字段内容),其结构与分类由本包(classify_error.go)负责。
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   *ChatCompletionUsage   `json:"usage,omitempty"`
	Error   json.RawMessage        `json:"error,omitempty"`
}

// ChatCompletionChunk 流式应答的单个数据块(stream=true 时上游逐块返回,OpenAI 兼容)
// Error 同上:保留原始 JSON。
type ChatCompletionChunk struct {
	ID      string                      `json:"id"`
	Model   string                      `json:"model"`
	Choices []ChatCompletionChunkChoice `json:"choices"`
	Usage   *ChatCompletionUsage        `json:"usage,omitempty"` //仅最后一个包(需设置LLM的include_usage属性)
	Error   json.RawMessage             `json:"error,omitempty"`
}

// ChatCompletionChunkDelta 流式块中的增量(delta)。
// reasoning_content 是推理模型的思考过程增量(如 DeepSeek reasoner),仅在流式响应中出现、
// 不参与请求体;由 harness 实时转发给浏览器展示,不并入正式回复。
type ChatCompletionChunkDelta struct {
	Role             string          `json:"role"`
	Content          string          `json:"content"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCallDelta `json:"tool_calls,omitempty"`
}

// ChatCompletionChunkChoice 流式块中的单个候选(增量文本在 delta.content / delta.reasoning_content)
type ChatCompletionChunkChoice struct {
	Index        int                      `json:"index"`
	Delta        ChatCompletionChunkDelta `json:"delta"`
	FinishReason *string                  `json:"finish_reason"`
}
