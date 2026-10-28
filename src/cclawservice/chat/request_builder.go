/*
* 定义构建http request函数
**/

package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/harness/security"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

const (
	LlmProviderEndpoint = "/chat/completions" // LLM API 请求时的访问端点URL
)

// 将 chat 的请求翻译成协议请求
// 职责边界:消息序列的 **内容** 由 chat/装配器决定,该函数只补 “协议必需品”
func buildChatCompletionRequestWithToolChoice(p Provider, req llmproto.LlmRequest, stream bool) llmproto.ChatCompletionRequest {
	r := llmproto.ChatCompletionRequest{
		Model:      p.Llm.GetModelName(),
		Messages:   req.Messages,
		Stream:     stream,
		Tools:      req.Tools,
		ToolChoice: strings.TrimSpace(req.ToolChoice),
	}

	// 非流式请求按provider配置发送 enable_thinking;流式保持nil(不关闭thinking)
	if !stream {
		r.EnableThinking = new(p.Llm.ThkNonStreaming)
	}

	// 例外:强制调用(tool_choice=required)与 thinking 互斥,必须显式关闭思考(流式也一样)
	// 这是唯一的think-off落脚点
	if r.ToolChoice == llmproto.ToolChoiceRequired {
		r.EnableThinking = new(false)
	}

	mt := security.DynamicMaxTokens(p.Llm, req.Messages)
	if mt == 0 {
		r.MaxTokens = nil // 应用LLM厂家默认
	} else {
		r.MaxTokens = &mt
	}

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

func buildChatCompletionRequestWithString(llm dataobject.TLlm, messages []llmproto.ChatMessage, maxTokens int, stream bool) llmproto.ChatCompletionRequest {
	req := llmproto.ChatCompletionRequest{
		Model:     llm.GetModelName(),
		Messages:  messages,
		Stream:    stream,
		MaxTokens: nil,
	}

	if maxTokens > 0 {
		req.MaxTokens = &(maxTokens)
	}

	// 仅非流失请求按 llm配置发送 enable_thinking(非流式模式下)
	// 流式请求保持 nil:thinking 由流式默认开启并经reasoning_content返回,不在此关闭
	if !stream {
		req.EnableThinking = &(llm.ThkNonStreaming)
	}

	return req
}

func buildHttpRequest(ctx context.Context, p Provider, req llmproto.ChatCompletionRequest) (*http.Request, error) {
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
