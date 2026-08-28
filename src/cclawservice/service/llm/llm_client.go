/**
* LLM 引擎客户端(OpenAI 兼容协议)
*
* 职责:向 LLM Provider 的 chat/completions 端点发送对话请求并取回回复文本。
* 兼容所有 OpenAI 风格协议(deepseek / anthropic-compatible / openai / 各类代理网关),
* 仅在 HTTP 层做最小假设:
*   - 请求体:{model, messages[], max_tokens, stream:false}
*   - 认证:Authorization: Bearer <apiKey>(apiKey 为空时省略该头)
*   - 应答:choices[0].message.content
*
* 设计说明:
*   - baseURL 按"每次调用"传入,因为 LLM Provider(端点)从数据库按 agent 动态解析,
*     客户端实例本身不绑定具体供应商。
*   - 超时由调用方注入的 http.Client 控制(与 server writeTimeout 解耦)。
**/

package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	myCommon "my.favorite/cclawservice/common"
	"my.favorite/cclawservice/controller/model"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
	pException "my.favorite/platformutility/exception"
)

// ChatCompletionChoice 一条候选回复
type ChatCompletionChoice struct {
	Index   int         `json:"index"`
	Message ChatMessage `json:"message"`
}

// ChatCompletionUsage token 用量(可选,部分供应商流式返回时可能为空)
type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`     // 本次请求的全部token,=请求的token+历史消息+工具调用结果+prompt token
	CompletionTokens int `json:"completion_tokens"` // 本次请求的输出token
	TotalTokens      int `json:"total_tokens"`      //prompt token + completion tokens
}

// ChatCompletionResponse chat/completions 应答体
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   *ChatCompletionUsage   `json:"usage,omitempty"`
	Error   *APIError              `json:"error,omitempty"`
}

// APIError 供应商返回的错误结构(OpenAI 风格)
type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

// ChatCompletionChunk 流式应答的单个数据块(stream=true 时上游逐块返回,OpenAI 兼容)
type ChatCompletionChunk struct {
	ID      string                      `json:"id"`
	Model   string                      `json:"model"`
	Choices []ChatCompletionChunkChoice `json:"choices"`
	Error   *APIError                   `json:"error,omitempty"`
}

// ChatCompletionChunkDelta 流式块中的增量(delta)。
// reasoning_content 是推理模型的思考过程增量(如 DeepSeek reasoner),仅在流式响应中出现、
// 不参与请求体;这里把它实时转发给浏览器展示,不并入正式回复。
type ChatCompletionChunkDelta struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// ChatCompletionChunkChoice 流式块中的单个候选(增量文本在 delta.content / delta.reasoning_content)
type ChatCompletionChunkChoice struct {
	Index        int                      `json:"index"`
	Delta        ChatCompletionChunkDelta `json:"delta"`
	FinishReason *string                  `json:"finish_reason"`
}

// Client 一个可复用的 LLM chat/completions 客户端(线程安全)。
type Client struct {
	// 同步请求客户端:固定整体超时,防止单次同步调用无限挂起。
	httpClient *http.Client
	// 流式请求客户端:无整体超时;长推理/长输出会合法地超过任何固定上限,
	// 若沿用同步的整体超时,会在内容已输出后把整次调用判为失败(见 A13)。
	streamClient *http.Client
}

// newClient 构造 LLM 客户端。
//   - timeout: 同步请求的整体超时,同时也是流式请求的首字节等待上限
//   - 流式客户端刻意不设整体超时(Timeout=0):流式输出时长由模型推理决定,
//     整体超时会在内容已送达后触发,浏览器端把已输出内容覆盖成"引擎不可用"。
//     断开/取消由调用方 ctx 负责(浏览器断开 → gin 取消请求 ctx → 上游读失败 →
//     ErrRequestCanceled → 静默结束),不会无限挂起。
func newClient(timeout time.Duration) *Client {
	streamTransport := http.DefaultTransport.(*http.Transport).Clone()
	streamTransport.ResponseHeaderTimeout = timeout // 仅限制"等待首个字节",不影响已开始的流
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		streamClient: &http.Client{
			Timeout:   0,
			Transport: streamTransport,
		},
	}
}

// NewClient 构造 LLM 客户端。
// 鉴权密钥来自每次调用传入的 TLlm(数据库配置);max_tokens 由
// BuildChatCompletionRequest 按 context_window 动态推导,客户端本身不持有任何供应商特定配置。
func NewClient() *Client {
	return newClient(myCommon.DefaultTimeout)
}

// ChatCompletion 调用指定 baseURL 的 chat/completions 端点,返回回复文本(已去首尾空白)。
// 同步模式:等待上游完整返回后再解析。
//   - req: 模型与消息;max_tokens 已按 context_window 动态推导(BuildChatCompletionRequest)
//
// 错误语义:网络/HTTP/协议错误均包装后返回,由上层映射为业务错误。
func (c *Client) ChatCompletion(ctx context.Context, llmProvider dataobject.TLlm, systemPrompt *dataobject.SystemPrompt, chatReq *model.ChatRequest) (string, error) {
	req := BuildChatCompletionRequest(llmProvider, systemPrompt, chatReq, false)
	httpReq, err := c.prepareRequest(ctx, llmProvider, req)
	if err != nil {
		return "", err
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return "", myException.ErrRequestCanceled
		}
		return "", fmt.Errorf("llm client: request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return "", myException.ParseLlmResponseError(httpResp, myCommon.MaxResponseBytes)
	}

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, myCommon.MaxResponseBytes))
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return "", myException.ErrRequestCanceled
		}
		return "", fmt.Errorf("llm client: read response: %w", err)
	}

	var resp ChatCompletionResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("llm client: unmarshal response: %w", err)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", fmt.Errorf("llm client: upstream error: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("llm client: empty completion choices")
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == "" {
		return "", errors.New("llm client: empty completion content")
	}

	return content, nil
}

// ChatCompletionStream 以流式(SSE)方式调用 chat/completions。
//   - onChunk:    每个正式回复增量到达时被调用,参数为该次增量 delta
//   - onReasoning: 思考过程增量(reasoning_content)到达时被调用(如推理模型);不并入返回的正式回复
//   - 返回值:     正式回复文本(累计,不含思考过程)
func (c *Client) ChatCompletionStream(ctx context.Context, llmProvider dataobject.TLlm, systemPrompt *dataobject.SystemPrompt, chatReq *model.ChatRequest, onChunk func(delta string), onReasoning func(delta string)) (string, error) {
	req := BuildChatCompletionRequest(llmProvider, systemPrompt, chatReq, true) // 已强制 stream=true
	httpReq, err := c.prepareRequest(ctx, llmProvider, req)
	if err != nil {
		return "", err
	}

	// 流式使用独立客户端:无整体超时,长流不会被固定上限(如 60s)中断
	httpResp, err := c.streamClient.Do(httpReq)
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return "", myException.ErrRequestCanceled
		}
		return "", fmt.Errorf("llm client: request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return "", myException.ParseLlmResponseError(httpResp, myCommon.MaxResponseBytes)
	}

	// 按行读取 SSE;每行形如 "data: {json}"。
	// 单行上限与 MaxResponseBytes 对齐,防止恶意超长行耗尽内存。
	var sb strings.Builder
	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), myCommon.MaxResponseBytes)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return "", myException.ErrRequestCanceled // 客户端已断开
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue // 忽略空行/注释行
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break // 上游结束标记
		}

		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // 跳过无法解析的行
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return "", fmt.Errorf("llm client: upstream stream error: %s", chunk.Error.Message)
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		// 思考过程增量(reasoning_content):只转发给调用方(前端实时展示),不并入正式回复
		if delta.ReasoningContent != "" && onReasoning != nil {
			onReasoning(delta.ReasoningContent)
		}
		if delta.Content == "" {
			continue
		}
		sb.WriteString(delta.Content)
		if onChunk != nil {
			onChunk(delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return "", myException.ErrRequestCanceled
		}
		return "", fmt.Errorf("llm client: read stream: %w", err)
	}

	content := strings.TrimSpace(sb.String())
	if content == "" {
		return "", errors.New("llm client: empty stream content")
	}
	return content, nil
}

// prepareRequest 校验入参并构造请求(不发起网络调用)。
// 同步(ChatCompletion)与流式(ChatCompletionStream)两条路径共用,保证请求构造一致。
func (c *Client) prepareRequest(ctx context.Context, llmProvider dataobject.TLlm, req ChatCompletionRequest) (*http.Request, error) {
	if strings.TrimSpace(req.Model) == "" {
		return nil, errors.New("llm client: model is empty")
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("llm client: messages is empty")
	}
	baseURL := strings.TrimSpace(llmProvider.BaseUrl)
	if baseURL == "" {
		return nil, errors.New("llm client: base url is empty")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("llm client: marshal request: %w", err)
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("llm client: new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(llmProvider.ApiKey); key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	return httpReq, nil
}
