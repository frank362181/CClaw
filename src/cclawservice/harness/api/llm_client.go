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

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/harness/api/llmproto"
	"my.favorite/cclawservice/harness/common"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/ctxmgr"
	"my.favorite/cclawservice/harness/observatory"
	"my.favorite/cclawservice/harness/tool"
	"my.favorite/cclawservice/model/dataobject"
	"my.favorite/cclawservice/request"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"
)

// Client 一个可复用的 LLM chat/completions 客户端(线程安全)。
type Client struct {
	// 同步请求客户端:固定整体超时,防止单次同步调用无限挂起。
	httpClient *http.Client
	// 流式请求客户端:无整体超时;长推理/长输出会合法地超过任何固定上限,
	// 若沿用同步的整体超时,会在内容已输出后把整次调用判为失败(见 A13)。
	streamClient *http.Client

	// 可重试错误(429/5xx/断流)的指数退避策略;默认取 common 常量,可按实例覆盖。
	retryMaxAttempts int
	retryBaseDelay   time.Duration
	retryMaxDelay    time.Duration
	// 退避抖动比例(±),用于打散并发重试;0 表示不抖动。
	retryJitter float64
	// 流式读取空闲超时:超过该时长无任何数据到达则中止本次流(0 表示禁用)。
	streamIdle time.Duration
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
		retryMaxAttempts: hCommon.LlmRetryMaxAttempts,
		retryBaseDelay:   time.Duration(hCommon.LlmRetryBaseDelayMs) * time.Millisecond,
		retryMaxDelay:    time.Duration(hCommon.LlmRetryMaxDelayMs) * time.Millisecond,
		retryJitter:      hCommon.LlmRetryJitterRatio,

		streamIdle: time.Duration(hCommon.StreamResponseTimeout) * time.Second,
	}
}

// NewClient 构造 LLM 客户端。
func NewClient() *Client {
	return newClient(time.Duration(hCommon.NonStreamResponseTimeout) * time.Second)
}

// Complete 轻量同步调用:以 system/user 文本调用 chat/completions 并返回回复文本。
// 用来根据用户问题的意图匹配最适合的agent的函数
// 用于"Agent 路由分类"等短输出场景;maxTokens 限制输出上限(<=0 时由供应商默认,仍受 context_window 约束)。
func (c *Client) Complete(ctx context.Context, llmProvider dataobject.TLlm, system, user string, maxTokens int) (string, string, error) {
	req := ctxmgr.BuildChatCompletionRequestWithString(llmProvider, system, user, maxTokens, false)
	content, finishReason, err := c.doCompletion(ctx, llmProvider, req)
	return content, finishReason, err
}

// ChatCompletion 调用指定 baseURL 的 chat/completions 端点,返回回复文本(已去首尾空白)。
// 同步模式:等待上游完整返回后再解析。
//   - req: 模型与消息;max_tokens 已按 context_window 动态推导(BuildChatCompletionRequest)
//
// 错误语义:网络/HTTP/协议错误均包装后返回,由上层映射为业务错误。
func (c *Client) ChatCompletion(ctx context.Context, llmProvider dataobject.TLlm, systemPrompt *hCommon.SystemPrompt, chatReq *request.ChatRequest) (string, error) {
	req := ctxmgr.BuildChatCompletionRequest(llmProvider, systemPrompt, chatReq, false)
	content, _, err := c.doCompletion(ctx, llmProvider, req)
	return content, err
}

// CompleteWithTools 执行一次同步 chat/completions(带工具),返回正式文本与工具调用(可能为空)。
// 不执行工具:由调用方(工具循环)解析并执行返回的工具调用。
// 429/5xx/断流(传输层)按指数退避重试(同步调用无副作用,可安全重试)。
//   - toolChoice: llmproto.ToolChoice* 常量;空串表示不发送 tool_choice(默认 auto)
func (c *Client) CompleteWithTools(ctx context.Context, llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, tools []llmproto.Tool, toolChoice string) (string, []llmproto.ToolCall, error) {
	req := ctxmgr.BuildChatCompletionRequestWithToolChoice(llmProvider, messages, tools, toolChoice, false)
	var lastErr error
	for attempt := 1; attempt <= c.retryMaxAttempts; attempt++ {
		if attempt > 1 {
			if err := c.retryWait(ctx, attempt, lastErr); err != nil {
				return pCommon.BlankString, nil, err
			}
		}
		content, calls, err := c.completeWithToolsOnce(ctx, llmProvider, req)
		if err == nil {
			return content, calls, nil
		}
		lastErr = err
		// 供应商不接受强制调用相关参数(tool_choice / enable_thinking):降级重试一次
		if dropped := ctxmgr.DegradeChatRequest(&req, err); dropped {
			continue
		}
		if !llmproto.IsRetryableLlmError(err) {
			return pCommon.BlankString, nil, err
		}
	}
	return pCommon.BlankString, nil, lastErr
}

// ChatCompletionStream 以流式(SSE)方式调用 chat/completions。
//   - onChunk:    每个正式回复增量到达时被调用,参数为该次增量 delta
//   - onReasoning: 思考过程增量(reasoning_content)到达时被调用(如推理模型);不并入返回的正式回复
//   - 返回值:     正式回复文本(累计,不含思考过程) + finish_reason(上游结束原因,可能为空)
//   - 重试:       429/5xx/断流(含空闲超时)在"尚未向客户端输出任何增量"时按指数退避重试;
//     一旦有增量输出则不再重试(避免重复内容)。
//   - 说明:       内容安全拦截(finish_reason=content_filter)可能无任何正式内容,此时不报"空流"错误,
//     而是原样透出 finish_reason,由上层给出"内容合规"提示。
func (c *Client) ChatCompletionStream(ctx context.Context, llmProvider dataobject.TLlm, systemPrompt *hCommon.SystemPrompt, chatReq *request.ChatRequest, onChunk func(delta string), onReasoning func(delta string)) (string, string, error) {
	req := ctxmgr.BuildChatCompletionRequest(llmProvider, systemPrompt, chatReq, true) // 已强制 stream=true
	return c.streamRequest(ctx, llmProvider, req, onChunk, onReasoning)
}

// ChatCompletionMessages 以**完整消息序列**做同步 chat/completions。
//
// 与 ChatCompletion 的区别:后者固定按 [system, 本轮用户消息] 两段构建,
// 无法携带历史;上下文读回装配后必须走本方法(max_tokens 的动态推导不变,见
// BuildChatCompletionRequestWithTool,它会按整段消息估算输入)。
func (c *Client) ChatCompletionMessages(ctx context.Context, llmProvider dataobject.TLlm, messages []llmproto.ChatMessage) (string, error) {
	req := ctxmgr.BuildChatCompletionRequestWithTool(llmProvider, messages, nil, false)
	content, _, err := c.doCompletion(ctx, llmProvider, req)
	return content, err
}

// ChatCompletionStreamMessages 以**完整消息序列**做流式 chat/completions。
// 语义与 ChatCompletionStream 完全一致(含"已输出增量即不重试"),仅消息来源不同。
func (c *Client) ChatCompletionStreamMessages(ctx context.Context, llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, onChunk func(delta string), onReasoning func(delta string)) (string, string, error) {
	req := ctxmgr.BuildChatCompletionRequestWithTool(llmProvider, messages, nil, true)
	return c.streamRequest(ctx, llmProvider, req, onChunk, onReasoning)
}

// StreamWithTools 流式执行一次 chat/completions(带工具):
//   - onChunk:      正式内容增量
//   - onReasoning:  思考增量(reasoning_content)
//   - onToolIntent: 模型流式声明将调用某工具时立即回调(仅首次辨识到工具名触发一次),
//     供调用方把工具意图实时推给前端(真正执行仍需等流结束拿到完整 arguments)。
//   - 返回值:       累计正式文本 + 模型请求的工具调用(若有;arguments 已按 index 拼接完整) + finish_reason
//   - 重试:         429/5xx/断流在"尚未输出任何内容/思考/工具意图"时按指数退避重试。
//   - toolChoice:   llmproto.ToolChoice* 常量;空串表示不发送 tool_choice(默认 auto)
func (c *Client) StreamWithTools(ctx context.Context, llmProvider dataobject.TLlm, messages []llmproto.ChatMessage, tools []llmproto.Tool, toolChoice string, onChunk func(string), onReasoning func(string), onToolIntent func(name string)) (string, []llmproto.ToolCall, string, error) {
	req := ctxmgr.BuildChatCompletionRequestWithToolChoice(llmProvider, messages, tools, toolChoice, true)
	lastFinish := ""
	var lastErr error
	for attempt := 1; attempt <= c.retryMaxAttempts; attempt++ {
		if attempt > 1 {
			if err := c.retryWait(ctx, attempt, lastErr); err != nil {
				return pCommon.BlankString, nil, lastFinish, err
			}
		}
		content, calls, finishReason, emitted, err := c.streamWithToolsOnce(ctx, llmProvider, req, onChunk, onReasoning, onToolIntent)
		if err == nil {
			return content, calls, finishReason, nil
		}
		lastErr, lastFinish = err, finishReason
		// 供应商不接受强制调用相关参数:降级重试一次(已向客户端输出增量时不可重试)
		if !emitted && ctxmgr.DegradeChatRequest(&req, err) {
			continue
		}
		if emitted || !llmproto.IsRetryableLlmError(err) {
			return pCommon.BlankString, nil, finishReason, err
		}
	}
	return pCommon.BlankString, nil, lastFinish, lastErr
}

// streamRequest 执行一次流式请求的重试编排(已构建好请求体)。
// 重试约定:429/5xx/断流在"尚未向客户端输出任何增量"时按指数退避重试;
// 一旦有增量输出则不再重试(避免重复内容)。
func (c *Client) streamRequest(ctx context.Context, llmProvider dataobject.TLlm, req llmproto.ChatCompletionRequest, onChunk func(delta string), onReasoning func(delta string)) (string, string, error) {
	lastFinish := ""
	var lastErr error
	for attempt := 1; attempt <= c.retryMaxAttempts; attempt++ {
		if attempt > 1 {
			if err := c.retryWait(ctx, attempt, lastErr); err != nil {
				return pCommon.BlankString, lastFinish, err
			}
		}
		content, finishReason, emitted, err := c.chatCompletionStreamOnce(ctx, llmProvider, req, onChunk, onReasoning)
		if err == nil {
			return content, finishReason, nil
		}
		lastErr, lastFinish = err, finishReason
		// 已输出过任何增量 → 不可重试(避免重复内容)
		if emitted || !llmproto.IsRetryableLlmError(err) {
			return pCommon.BlankString, finishReason, err
		}
	}
	return pCommon.BlankString, lastFinish, lastErr
}

// ================================== private functions & methods ========================================
// completeWithToolsOnce 执行一次同步(带工具)chat/completions(不含重试)
func (c *Client) completeWithToolsOnce(ctx context.Context, llmProvider dataobject.TLlm, req llmproto.ChatCompletionRequest) (outContent string, outCalls []llmproto.ToolCall, outErr error) {
	var (
		callStart      time.Time
		ttftMs         int64
		lastTokenUsage *llmproto.ChatCompletionUsage //同步路径也有 usage,之前硬传 nil → prompt/cached 永远为 0
		err            error
	)

	// 返回值具名(名字刻不与局部变量冲突,免得连带改一堆 :=):
	// 只有具名返回值才能让 `return ..., 新构造的错误` 也落到 outErr 上 ——
	// 否则像 ParseLlmResponseError 这类返回值不会写回任何局部 err,报表里的 err 永远是空的。
	defer func() {
		observatory.OnLlmTiming(ctx, llmProvider, false, lastTokenUsage, callStart, ttftMs, outErr)
	}()

	httpReq, err := c.prepareRequest(ctx, llmProvider, req)
	if err != nil {
		return pCommon.BlankString, nil, err
	}

	callStart = time.Now() //TTFT 调用开始时间
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return pCommon.BlankString, nil, llmproto.ErrRequestCanceled
		}
		return pCommon.BlankString, nil, llmproto.NewLlmResponseTransportError(err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return pCommon.BlankString, nil, llmproto.ParseLlmResponseError(httpResp, hCommon.MaxResponseBytes)
	}

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, hCommon.MaxResponseBytes))
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return pCommon.BlankString, nil, llmproto.ErrRequestCanceled
		}
		return pCommon.BlankString, nil, llmproto.NewLlmResponseTransportError(err)
	}

	var resp llmproto.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return pCommon.BlankString, nil, myException.RuntimeClientErrEx2("runtime client: unmarshal response: %v", err)
	}
	if llmproto.HasError(resp.Error) {
		return pCommon.BlankString, nil, llmproto.NewLlmResponseError(resp.Error)
	}
	if len(resp.Choices) == 0 {
		return pCommon.BlankString, nil, myException.RuntimeClientErrEx("runtime client: empty completion choices")
	}

	//回调统计token用量
	ttftMs = time.Since(callStart).Milliseconds() //登记结束时检长度
	lastTokenUsage = resp.Usage                   //同步路径同样要把用量带进 llm timing 行
	observatory.ReportTokenUsage(llmProvider, false, resp.Usage)

	msg := resp.Choices[0].Message
	return strings.TrimSpace(msg.Content), msg.ToolCalls, nil
}

// chatCompletionStreamOnce 执行一次流式 chat/completions(不含重试;含空闲超时与 SSE 事件解析)。
// 返回值 emitted 表示是否已向调用方输出过增量(决定上层能否安全重试)。
func (c *Client) chatCompletionStreamOnce(ctx context.Context, llmProvider dataobject.TLlm, req llmproto.ChatCompletionRequest, onChunk func(string), onReasoning func(string)) (outContent string, outFinish string, outEmitted bool, outErr error) {
	var (
		lastTokenUsage *llmproto.ChatCompletionUsage //流式下的token用量统计变量
		callStart      time.Time
		ttftMs         int64
		err            error
	)
	// 返回值具名:让每个 `return ..., 新构造的错误` 都能被 defer 读到(否则报表 err 恒空)。
	defer func() {
		observatory.OnLlmTiming(ctx, llmProvider, true, lastTokenUsage, callStart, ttftMs, outErr)
	}()

	// 空闲超时:上游长时间无任何数据 → 取消本次流(归类为可重试的传输层错误)
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	idle := c.streamIdle
	idleTimer := pCommon.CreateTimerWithContext(idle, cancel)
	if idleTimer != nil {
		defer idleTimer.Stop()
	}

	httpReq, err := c.prepareRequest(streamCtx, llmProvider, req)
	if err != nil {
		return pCommon.BlankString, pCommon.BlankString, false, err
	}

	// 流式使用独立客户端:无整体超时,长流不会被固定上限(如 60s)中断
	callStart = time.Now() //登记TTFT开始的时间
	httpResp, err := c.streamClient.Do(httpReq)
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return pCommon.BlankString, pCommon.BlankString, false, llmproto.ErrRequestCanceled
		}
		return pCommon.BlankString, pCommon.BlankString, false, llmproto.NewLlmResponseTransportError(err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return pCommon.BlankString, pCommon.BlankString, false, llmproto.ParseLlmResponseError(httpResp, hCommon.MaxResponseBytes)
	}
	requestID := llmproto.GetRequestIDFromLlmResponse(httpResp.Header) // 流中错误带上上游 request_id,便于排障
	if idleTimer != nil {
		idleTimer.Reset(idle) // 已收到响应头,重新计时
	}

	// 按行读取 SSE;每行形如 "data: {json}" 或 "event: <type>"。
	// 单行上限与 MaxResponseBytes 对齐,防止恶意超长行耗尽内存。
	var sb strings.Builder
	finishReason := ""
	emitted := false
	eventName := ""

	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), hCommon.MaxResponseBytes)
	for scanner.Scan() {
		if idleTimer != nil {
			idleTimer.Reset(idle) // 收到数据 → 重置空闲计时
		}
		if ctx.Err() != nil {
			return pCommon.BlankString, finishReason, emitted, llmproto.ErrRequestCanceled // 客户端已断开
		}

		line := strings.TrimSpace(scanner.Text())
		if line == pCommon.BlankString { // SSE 事件块结束:重置事件类型
			eventName = pCommon.BlankString
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue // 忽略注释/其它字段
		}

		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break // 上游结束标记
		}

		// TTFT 打点:首个data行 = 上游开始产出,不能是首个字节(避免keep-alive会让首包时间虚低,失去诊断价值)
		if ttftMs == 0 {
			ttftMs = time.Since(callStart).Milliseconds()
		}

		var chunk llmproto.ChatCompletionChunk
		chunkOK := json.Unmarshal([]byte(data), &chunk) == nil
		if payload, isErr := llmproto.StreamErrorPayload(eventName, chunk, data); isErr {
			return pCommon.BlankString, finishReason, emitted, llmproto.NewLlmStreamError(payload, requestID)
		}

		//登记使用量,usage末包chunk.Choice为空,因此必须早于chunk.Choice是否为空的判断
		if chunkOK && chunk.Usage != nil {
			lastTokenUsage = chunk.Usage
		}

		if !chunkOK || len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			finishReason = strings.TrimSpace(*choice.FinishReason) // 记录结束原因(length/content_filter/tool_calls/stop)
		}

		delta := choice.Delta
		// 思考过程增量(reasoning_content):只转发给调用方(前端实时展示),不并入正式回复
		if delta.ReasoningContent != pCommon.BlankString && onReasoning != nil {
			emitted = true
			onReasoning(delta.ReasoningContent)
		}
		if delta.Content == pCommon.BlankString {
			continue
		}
		sb.WriteString(delta.Content)
		emitted = true
		if onChunk != nil {
			onChunk(delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return pCommon.BlankString, finishReason, emitted, common.StreamReadError(ctx, streamCtx, err, requestID, idle)
	}

	// 回掉统计token是用量
	observatory.ReportTokenUsage(llmProvider, true, lastTokenUsage)

	content := strings.TrimSpace(sb.String())
	// 内容安全拦截可能无任何正式内容:透出 finish_reason 交由上层提示,不误报为"空流"
	if content == pCommon.BlankString && finishReason != llmproto.FinishReasonContentFilter {
		return pCommon.BlankString, finishReason, emitted, myException.RuntimeClientErrEx("runtime client: empty stream content")
	}
	return content, finishReason, emitted, nil
}

// streamWithToolsOnce 执行一次带工具的流式 chat/completions(不含重试)。
func (c *Client) streamWithToolsOnce(ctx context.Context, llmProvider dataobject.TLlm, req llmproto.ChatCompletionRequest, onChunk func(string), onReasoning func(string), onToolIntent func(name string)) (outContent string, outCalls []llmproto.ToolCall, outFinish string, outEmitted bool, outErr error) {
	var (
		callStart      time.Time
		ttftMs         int64
		lastTokenUsage *llmproto.ChatCompletionUsage
		err            error
	)
	// 返回值具名:让每个 `return ..., 新构造的错误` 都能被 defer 读到(否则报表 err 恒空)。
	defer func() {
		observatory.OnLlmTiming(ctx, llmProvider, true, lastTokenUsage, callStart, ttftMs, outErr)
	}()

	callStart = time.Now() //TTFT 调用开始的时间

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	idle := c.streamIdle
	idleTimer := pCommon.CreateTimerWithContext(idle, cancel)
	if idleTimer != nil {
		defer idleTimer.Stop()
	}

	httpReq, err := c.prepareRequest(streamCtx, llmProvider, req)
	if err != nil {
		return "", nil, "", false, err
	}

	httpResp, err := c.streamClient.Do(httpReq)
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return "", nil, "", false, llmproto.ErrRequestCanceled
		}
		return "", nil, "", false, llmproto.NewLlmResponseTransportError(err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return "", nil, "", false, llmproto.ParseLlmResponseError(httpResp, hCommon.MaxResponseBytes)
	}
	requestID := llmproto.GetRequestIDFromLlmResponse(httpResp.Header) // 流中错误带上上游 request_id,便于排障
	if idleTimer != nil {
		idleTimer.Reset(idle)
	}

	var sb strings.Builder
	finishReason := ""
	emitted := false
	eventName := ""

	acc := tool.NewToolCallAccumulator()
	reportedIntent := make(map[int]bool) // 已上报过工具意图的 index,避免同 index 分片重复上报
	scanner := bufio.NewScanner(httpResp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), hCommon.MaxResponseBytes)
	for scanner.Scan() {
		if idleTimer != nil {
			idleTimer.Reset(idle)
		}
		if ctx.Err() != nil {
			return pCommon.BlankString, nil, finishReason, emitted, llmproto.ErrRequestCanceled
		}

		line := strings.TrimSpace(scanner.Text())
		if line == pCommon.BlankString { // SSE 事件块结束:重置事件类型
			eventName = pCommon.BlankString
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		if ttftMs == 0 {
			ttftMs = time.Since(callStart).Milliseconds()
		}

		var chunk llmproto.ChatCompletionChunk
		chunkOK := json.Unmarshal([]byte(data), &chunk) == nil
		if payload, isErr := llmproto.StreamErrorPayload(eventName, chunk, data); isErr {
			return "", nil, finishReason, emitted, llmproto.NewLlmStreamError(payload, requestID)
		}

		//必须早于Choice是否为空的判断,因为末包Choice为空
		if chunkOK && chunk.Usage != nil {
			lastTokenUsage = chunk.Usage
		}

		if !chunkOK || len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
			finishReason = strings.TrimSpace(*choice.FinishReason) // 记录结束原因(length/content_filter/tool_calls/stop)
		}

		delta := choice.Delta
		if delta.ReasoningContent != pCommon.BlankString && onReasoning != nil {
			emitted = true
			onReasoning(delta.ReasoningContent)
		}
		if delta.Content != pCommon.BlankString {
			sb.WriteString(delta.Content)
			emitted = true
			if onChunk != nil {
				onChunk(delta.Content)
			}
		}
		for _, tc := range delta.ToolCalls {
			acc.Add(tc)
			if onToolIntent != nil && tc.Function.Name != "" && !reportedIntent[tc.Index] {
				reportedIntent[tc.Index] = true
				emitted = true
				onToolIntent(tc.Function.Name)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", nil, finishReason, emitted, common.StreamReadError(ctx, streamCtx, err, requestID, idle)
	}

	//回调登记 token使用量
	observatory.ReportTokenUsage(llmProvider, true, lastTokenUsage)

	return strings.TrimSpace(sb.String()), acc.Result(), finishReason, emitted, nil
}

// retryWait 退避等待后继续重试(nextAttempt 为即将执行的尝试序号,从 2 开始);
// 等待期间客户端断开 → 返回 ErrRequestCanceled(不重试)。
func (c *Client) retryWait(ctx context.Context, nextAttempt int, lastErr error) error {
	delay := pCommon.RetryBackPolicy(c.retryBaseDelay, c.retryMaxDelay, c.retryJitter).Delay(nextAttempt - 1) //c.retryDelay(nextAttempt - 1)
	logging.LogWarnF("runtime client: retry %d/%d after %s, last error: %v",
		nextAttempt, c.retryMaxAttempts, delay, lastErr)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return llmproto.ErrRequestCanceled
	case <-timer.C:
		return nil
	}
}

// doCompletion 执行一次同步 chat/completions,返回回复文本与 finish_reason
// (429/5xx/断流按指数退避重试)。
func (c *Client) doCompletion(ctx context.Context, llmProvider dataobject.TLlm, req llmproto.ChatCompletionRequest) (string, string, error) {
	var lastErr error
	var lastFinish string
	for attempt := 1; attempt <= c.retryMaxAttempts; attempt++ {
		if attempt > 1 {
			if err := c.retryWait(ctx, attempt, lastErr); err != nil {
				return pCommon.BlankString, lastFinish, err
			}
		}
		content, finishReason, err := c.doCompletionOnce(ctx, llmProvider, req)
		if err == nil {
			return content, finishReason, nil
		}
		lastErr, lastFinish = err, finishReason
		if !llmproto.IsRetryableLlmError(err) {
			return pCommon.BlankString, lastFinish, err
		}
	}
	return pCommon.BlankString, lastFinish, lastErr
}

// doCompletionOnce 执行一次同步 chat/completions 请求并解析回复文本(同步/轻量调用共用,不含重试)。
// 同时透出 finish_reason:调用方据此判断内容是否被上游截断(length)。
func (c *Client) doCompletionOnce(ctx context.Context, llmProvider dataobject.TLlm, req llmproto.ChatCompletionRequest) (outContent string, outFinish string, outErr error) {
	var (
		callStart      time.Time
		ttftMs         int64
		lastTokenUsage *llmproto.ChatCompletionUsage //同步路径也有 usage,之前硬传 nil → prompt/cached 永远为 0
		err            error
	)

	// 返回值具名:让每个 `return ..., 新构造的错误` 都能被 defer 读到(否则报表 err 恒空)。
	defer func() {
		observatory.OnLlmTiming(ctx, llmProvider, false, lastTokenUsage, callStart, ttftMs, outErr)
	}()

	httpReq, err := c.prepareRequest(ctx, llmProvider, req)
	if err != nil {
		return pCommon.BlankString, pCommon.BlankString, err
	}

	callStart = time.Now() // TTFT起点,请求发出时间
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return pCommon.BlankString, pCommon.BlankString, llmproto.ErrRequestCanceled
		}
		return pCommon.BlankString, pCommon.BlankString, llmproto.NewLlmResponseTransportError(err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(httpResp.Body)

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return pCommon.BlankString, pCommon.BlankString, llmproto.ParseLlmResponseError(httpResp, hCommon.MaxResponseBytes)
	}

	respBody, err := io.ReadAll(io.LimitReader(httpResp.Body, hCommon.MaxResponseBytes))
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return pCommon.BlankString, pCommon.BlankString, llmproto.ErrRequestCanceled
		}
		return pCommon.BlankString, pCommon.BlankString, llmproto.NewLlmResponseTransportError(err)
	}

	var resp llmproto.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return pCommon.BlankString, pCommon.BlankString, myException.RuntimeClientErrEx2("runtime client: unmarshal response: %v", err)
	}
	if llmproto.HasError(resp.Error) {
		return pCommon.BlankString, pCommon.BlankString, llmproto.NewLlmResponseError(resp.Error)
	}
	if len(resp.Choices) == 0 {
		return pCommon.BlankString, pCommon.BlankString, myException.RuntimeClientErrEx("runtime client: empty completion choices")
	}

	finishReason := pCommon.BlankString
	if resp.Choices[0].FinishReason != nil {
		finishReason = strings.TrimSpace(*resp.Choices[0].FinishReason)
	}

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	if content == pCommon.BlankString {
		return pCommon.BlankString, finishReason, myException.RuntimeClientErrEx("runtime client: empty completion content")
	}

	// 回调token用量
	ttftMs = time.Since(callStart).Milliseconds() //记录完成的时间长度
	lastTokenUsage = resp.Usage                   //同步路径同样要把用量带进 llm timing 行
	observatory.ReportTokenUsage(llmProvider, false, resp.Usage)

	return content, finishReason, nil
}

// prepareRequest 校验入参并构造请求(不发起网络调用)。
// 同步(ChatCompletion)与流式(ChatCompletionStream)两条路径共用,保证请求构造一致。
func (c *Client) prepareRequest(ctx context.Context, llmProvider dataobject.TLlm, req llmproto.ChatCompletionRequest) (*http.Request, error) {
	if strings.TrimSpace(req.Model) == pCommon.BlankString {
		return nil, myException.RuntimeClientErrEx("runtime client: model is empty")
	}
	if len(req.Messages) == 0 {
		return nil, myException.RuntimeClientErrEx("runtime client: messages is empty")
	}

	// 流式:按provider配置开启usage上报
	if req.Stream && req.StreamOptions == nil && llmProvider.UsageInStreaming {
		req.StreamOptions = &llmproto.StreamOptions{IncludeUsage: true}
	}

	baseURL := strings.TrimSpace(llmProvider.BaseUrl)
	if baseURL == pCommon.BlankString {
		return nil, myException.RuntimeClientErrEx("runtime client: base url is empty")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, myException.RuntimeClientErrEx2("runtime client: marshal request: %v", err)
	}

	endpoint := strings.TrimRight(baseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, myException.RuntimeClientErrEx2("runtime client: new request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key := strings.TrimSpace(llmProvider.ApiKey); key != pCommon.BlankString {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	return httpReq, nil
}
