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

	"my.favorite/cclawservice/controller/model"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
)

// MaxResponseBytes 单次应答读取上限(8MB),防止超大响应耗尽内存。
const MaxResponseBytes = 8 << 20

// DefaultTimeout 单次 LLM 请求的默认超时。
// 真实模型推理(大上下文 + 大 max_tokens)可能耗时较长,10s 过短会在读响应时因
// 上下文被取消(客户端断开/超时)而中断;这里放宽到 60s(需与服务端 writeTimeout、前端超时对齐)。
const DefaultTimeout = 60 * time.Second

// ChatCompletionChoice 一条候选回复
type ChatCompletionChoice struct {
	Index   int         `json:"index"`
	Message ChatMessage `json:"message"`
}

// ChatCompletionUsage token 用量(可选,部分供应商流式返回时可能为空)
type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
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

// ChatCompletionChunkChoice 流式块中的单个候选(增量文本在 delta.content)
type ChatCompletionChunkChoice struct {
	Index        int         `json:"index"`
	Delta        ChatMessage `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

// Client 一个可复用的 LLM chat/completions 客户端(线程安全)。
type Client struct {
	httpClient *http.Client
}

// NewClient 构造 LLM 客户端。
// 鉴权密钥与 max_tokens 均来自每次调用传入的 TLlm(数据库配置),
// 客户端本身不持有任何供应商特定配置。
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}
}

// responseError 非 2xx 时把响应体解析为可读的错误信息返回。
func responseError(resp *http.Response) error {
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	var apiErr APIError
	if json.Unmarshal(respBody, &apiErr) == nil && apiErr.Message != "" {
		return fmt.Errorf("llm client: http %d: %s", resp.StatusCode, apiErr.Message)
	}
	return fmt.Errorf("llm client: http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
}

// ChatCompletion 调用指定 baseURL 的 chat/completions 端点,返回回复文本(已去首尾空白)。
// 同步模式:等待上游完整返回后再解析。
//   - req: 模型与消息;maxTokens 为空时自动填充客户端默认值
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
		if isContextCanceled(ctx, err) {
			return "", myException.ErrRequestCanceled
		}
		return "", fmt.Errorf("llm client: request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return "", responseError(httpResp)
	}

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, MaxResponseBytes))
	if err != nil {
		if isContextCanceled(ctx, err) {
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
//   - onChunk: 每个增量文本到达时被调用,参数为该次增量 delta(非累计)
//   - 返回值: 完整回复文本(累计)
//   - 客户端断开/取消:返回 myException.ErrRequestCanceled,由上层区分处理
func (c *Client) ChatCompletionStream(ctx context.Context, llmProvider dataobject.TLlm, systemPrompt *dataobject.SystemPrompt, chatReq *model.ChatRequest, onChunk func(delta string)) (string, error) {
	req := BuildChatCompletionRequest(llmProvider, systemPrompt, chatReq, true) // 已强制 stream=true
	httpReq, err := c.prepareRequest(ctx, llmProvider, req)
	if err != nil {
		return "", err
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if isContextCanceled(ctx, err) {
			return "", myException.ErrRequestCanceled
		}
		return "", fmt.Errorf("llm client: request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return "", responseError(httpResp)
	}

	// 按行读取 SSE;每行形如 "data: {json}"。
	// 单行上限与 MaxResponseBytes 对齐,防止恶意超长行耗尽内存。
	var sb strings.Builder
	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxResponseBytes)
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
		if len(chunk.Choices) == 0 || chunk.Choices[0].Delta.Content == "" {
			continue
		}

		delta := chunk.Choices[0].Delta.Content
		sb.WriteString(delta)
		if onChunk != nil {
			onChunk(delta)
		}
	}
	if err := scanner.Err(); err != nil {
		if isContextCanceled(ctx, err) {
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

	// 填充默认 max_tokens(取 provider 数据库配置;未配置则省略该字段)
	if req.MaxTokens == nil && llmProvider.MaxTokens > 0 {
		req.MaxTokens = new(int(llmProvider.MaxTokens))
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

// isContextCanceled 判断错误是否源于请求上下文取消,以便上层区分处理。
func isContextCanceled(ctx context.Context, err error) bool {
	return errors.Is(err, context.Canceled) || ctx.Err() != nil
}
