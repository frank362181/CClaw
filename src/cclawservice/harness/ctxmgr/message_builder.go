package ctxmgr

import (
	"fmt"
	"strings"

	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/security"
	"my.favorite/cclawservice/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
	pCommon "my.favorite/platformutility/common"
	"my.favorite/platformutility/logging"
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

// BuildToolCallWithContentMessage 构造携带文本与工具调用的 assistant 消息(工具循环回填历史用)。
// 模型可能在一轮里既输出文本又请求工具(OpenAI 规范允许 content 与 tool_calls 并存),
// 文本须一并回填,否则下一轮 LLM 看不到自己刚说的话(上下文缺失)。
func BuildToolCallWithContentMessage(content string, calls []llmproto.ToolCall) llmproto.ChatMessage {
	return llmproto.ChatMessage{Role: hCommon.RoleAssistant, Content: content, ToolCalls: calls}
}

// BuildToolResultMessage 构造工具执行结果消息(role=tool,按 tool_call_id 关联)。
func BuildToolResultMessage(toolCallID, content string) llmproto.ChatMessage {
	return llmproto.ChatMessage{Role: hCommon.RoleTool, ToolCallID: toolCallID, Content: content}
}

// ComposeSystemMessage 组装 system 消息:系统提示 + 历史摘要(摘要追加在系统提示之后)。
//
// 摘要刻意不单独成一条 system 消息:部分供应商对"非首条 system"支持不一致,
// 折进同一条 system 消息可以完全规避该类兼容问题。
func ComposeSystemMessage(systemPrompt, summaryText, summaryTitle string) llmproto.ChatMessage {
	content := strings.TrimSpace(systemPrompt)
	if summary := strings.TrimSpace(summaryText); summary != pCommon.BlankString {
		content = strings.TrimSpace(content + "\n\n" + summaryTitle + "\n" + summary)
	}

	return llmproto.ChatMessage{Role: hCommon.RoleSystem, Content: content}
}

// BuildClassifySystemPrompt 构建用来通过用户问题的意图询问LLM返回合适Agent的 prompt。
// 简介优先使用各 Agent 工作区 AGENTS.md 的摘要(已由调用方解析进 ClassifyAgent.Label)。
func BuildClassifySystemPrompt(agents []hCommon.ClassificationAgent) string {
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
	maxTokens := security.DynamicMaxTokens(llmProvider, messages)
	if maxTokens == 0 {
		req.MaxTokens = nil //应用供应商的默认配置
	} else {
		req.MaxTokens = new(maxTokens)
	}

	return req
}

// BuildChatCompletionRequestWithTool 构造带工具的 chat/completions 请求体(tool_choice 不发送)。
// max_tokens 沿用与普通请求一致的动态推导(dynamicMaxTokens)。
func BuildChatCompletionRequestWithTool(llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, tools []llmproto.Tool /*[]llmproto.Tool*/, stream bool) llmproto.ChatCompletionRequest {
	return BuildChatCompletionRequestWithToolChoice(llmProvider, messages, tools, pCommon.BlankString, stream)
}

// BuildChatCompletionRequestWithToolChoice 在带工具请求的基础上附上工具选择策略。
//   - toolChoice: llmproto.ToolChoice* 常量;空串则不发送 tool_choice 字段(等价 auto)
//
// 为什么需要:技能正文不注入 system prompt,模型看不到"必须调用工具"的流程要求,
// 可能直接用上下文里的历史工具结果作答 —— 对实时类工具(force-call)首轮必须强制调用。
func BuildChatCompletionRequestWithToolChoice(llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, tools []llmproto.Tool /*[]llmproto.Tool*/, toolChoice string, stream bool) llmproto.ChatCompletionRequest {
	req := llmproto.ChatCompletionRequest{
		Model:      llmProvider.GetModelName(),
		Messages:   messages,
		Stream:     stream,
		Tools:      tools,
		ToolChoice: strings.TrimSpace(toolChoice),
	}
	// 仅非流式请求按 provider 配置发送 enable_thinking;流式保持 nil(不关闭 thinking)
	if !stream {
		req.EnableThinking = new(llmProvider.ThkNonStreaming)
	}
	// 例外:强制调用(tool_choice=required)与 thinking 互斥,必须显式关闭思考(流式也要发)。
	if req.ToolChoice == llmproto.ToolChoiceRequired {
		req.EnableThinking = new(false)
	}

	mt := security.DynamicMaxTokens(llmProvider, messages)
	if mt == 0 {
		req.MaxTokens = nil // 应用供应商默认
	} else {
		req.MaxTokens = &mt
	}
	return req
}

// DegradeChatRequest 错误信息中包含的信息对chat request降级
func DegradeChatRequest(req *llmproto.ChatCompletionRequest, err error) bool {
	if req == nil || err == nil || req.ToolChoice == pCommon.BlankString {
		return false
	}

	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "tool_choice") && !strings.Contains(msg, "enable_thinking") {
		return false
	}

	logging.LogWarnF("llm client: provider rejected tool forced-call params (clear tool_choice=%s/enable_thinking), retry: %s",
		req.ToolChoice, err.Error())

	//对强制工具调用进行降级
	req.ToolChoice = pCommon.BlankString
	req.EnableThinking = nil

	return true
}
