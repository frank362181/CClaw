/*
* 定义构建http request函数
**/

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"my.favorite/cclawservice/chat"
	"my.favorite/cclawservice/harness/api/llmproto"
	"my.favorite/cclawservice/harness/security"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

const (
	LlmProviderEndpoint = "/chat/completions" // LLM API 请求时的访问端点URL
)

// 将 chat 的请求翻译成协议请求
// 职责边界:消息序列的 **内容** 由 chat/装配器决定,该函数只补 “协议必需品”
func buildRequest(p chat.Provider, req chat.LlmRequest, stream bool) llmproto.ChatCompletionRequest {
	r := buildChatCompletionRequestWithToolChoice(p.Llm, req.Messages, req.Tools, req.ToolChoice, stream)

	// 调用方显式指定时覆盖(维护调用/路由分类需要固定预算)
	// 不能让随历史体积漂移 1 个token
	if req.MaxTokens > 0 {
		r.MaxTokens = new(req.MaxTokens)
	}

	// 流式 Usage按provider能力开启
	if stream && p.Llm.UsageInStreaming {
		r.StreamOptions = &llmproto.StreamOptions{IncludeUsage: true}
	}
	return r
}

func buildHttpRequest(ctx context.Context, p chat.Provider, req llmproto.ChatCompletionRequest) (*http.Request, error) {
	if strings.TrimSpace(req.Model) == pCommon.BlankString {
		return nil, fmt.Errorf("blank model")
	}

	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("no messages")
	}

	baseUrl := strings.TrimSpace(p.Llm.BaseUrl)
	if baseUrl == pCommon.BlankString {
		return nil, fmt.Errorf("blank url")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimRight(baseUrl, "/") + LlmProviderEndpoint
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(p.Llm.ApiKey); key != pCommon.BlankString {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}

	return httpReq, nil
}

func buildChatCompletionRequestWithToolChoice(llm dataobject.TLlm, messages []llmproto.ChatMessage, tools []llmproto.Tool,
	toolChoice string, stream bool) llmproto.ChatCompletionRequest {
	req := llmproto.ChatCompletionRequest{
		Model:      llm.GetModelName(),
		Messages:   messages,
		Stream:     stream,
		Tools:      tools,
		ToolChoice: strings.TrimSpace(toolChoice),
	}

	// 非流式请求按provider配置发送 enable_thinking;流式保持nil(不关闭thinking)
	if !stream {
		req.EnableThinking = new(llm.ThkNonStreaming)
	}

	// 例外:强制调用(tool_choice=required)与 thinking 互斥,必须显式关闭思考(流式也一样)
	if req.ToolChoice == llmproto.ToolChoiceRequired {
		req.EnableThinking = new(false)
	}

	mt := security.DynamicMaxTokens(llm, messages)
	if mt == 0 {
		req.MaxTokens = nil // 应用LLM厂家默认
	} else {
		req.MaxTokens = &mt
	}

	return req
}
