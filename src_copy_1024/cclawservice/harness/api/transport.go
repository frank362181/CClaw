/*
* http请求和解析类
**/

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"my.favorite/cclawservice/chat"
	"my.favorite/cclawservice/harness/api/llmproto"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/observatory"
	"my.favorite/cclawservice/harness/tool"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
)

// Transport 是chat.ITransport的 **唯一**实现类:只有两个方法
// 进程级单例子:http.Client内部有链接池
type Transport struct {
	syncTransport  *http.Client
	asyncTransport *http.Client

	idle        time.Duration //空闲的时间
	maxResponse int64
	retryPolicy RetryPolicy
}

type Options struct {
	SyncTimeout time.Duration //同步整体超时,同时作为流式"首字节等待"上限,unit = second
	AsyncIdle   time.Duration //流式空闲超时

	MaxResponse int64       // 单次响应体 / 单SSE行的字节上限
	retryPolicy RetryPolicy // default为DefaultRetryPolicy
}

func NewTransport(opt Options) chat.ITransport {
	if opt.SyncTimeout == 0 {
		opt.SyncTimeout = time.Duration(time.Second) * 60
	}

	if opt.MaxResponse == 0 {
		opt.MaxResponse = 16 << 20
	}

	if opt.retryPolicy.Attempts() <= 1 {
		opt.retryPolicy = *DefaultRetryPolicy()
	}

	streamTransport := http.DefaultTransport.(*http.Transport).Clone()
	streamTransport.ResponseHeaderTimeout = opt.SyncTimeout

	return &Transport{
		syncTransport:  &http.Client{Timeout: opt.SyncTimeout},
		asyncTransport: &http.Client{Timeout: 0, Transport: streamTransport},
		maxResponse:    opt.MaxResponse,
		idle:           opt.AsyncIdle,
		retryPolicy:    opt.retryPolicy,
	}
}

func (t *Transport) Complete(ctx context.Context, p chat.Provider, req chat.LlmRequest) (chat.LlmReply, error) {
	builtReq := buildRequest(p, req, false)

	reply, err := Retry(ctx, t.retryPolicy, func(ctx context.Context) (chat.LlmReply, bool, error) {
		return t.completeOnce(ctx, p, builtReq)
	})

	return reply, err
}

func (t *Transport) Stream(ctx context.Context, p chat.Provider, req chat.LlmRequest, ob chat.IStreamObserver) (chat.LlmReply, error) {
	if ob == nil {
		ob = chat.NopObserver{}
	}

	builtReq := buildRequest(p, req, true)
	degrader := NewDegrader()

	reply, err := Retry(ctx, t.retryPolicy, func(ctx context.Context) (chat.LlmReply, bool, error) {
		rep, emitted, callErr := t.streamOnce(ctx, p, builtReq, ob)
		if callErr != nil && !emitted && degrader.apply(&builtReq, callErr) {
			// 供应商不接受强制调用参数 (tool_choice . enable_thinking):
			// 改写请求立刻重试。 **不计入** "可重试错误"语义
			// 这是参数兼容性降级,不是瞬时故障
			return rep, false, callErr
		}

		return rep, emitted, callErr
	})

	return reply, err
}

// =============================== private functions =================================

// 发出请求并校验状态码,返回的resp由调用方负责close
func (t *Transport) doRequest(ctx context.Context, c *http.Client, p chat.Provider, req llmproto.ChatCompletionRequest) (*http.Response, error) {
	httpReq, err := buildHttpRequest(ctx, p, req)
	if err != nil {
		return nil, err
	}

	resp, err := c.Do(httpReq)
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return nil, llmproto.ErrRequestCanceled
		}

		return nil, llmproto.NewLlmResponseTransportError(err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer func(body io.ReadCloser) {
			_ = body.Close()
		}(resp.Body)
		return nil, llmproto.ParseLlmResponseError(resp, t.maxResponse)
	}

	return resp, nil
}

func (t *Transport) completeOnce(ctx context.Context, p chat.Provider, req llmproto.ChatCompletionRequest) (reply chat.LlmReply, emitted bool, outErr error) {
	start := pCommon.GetDatetimeUtilityInstance().GetNow()

	var (
		ttft  int64 // 第一个token到达时间
		usage *llmproto.ChatCompletionUsage
	)

	defer func() {
		observatory.OnLlmTiming(ctx, p.Llm, false, usage, start, ttft, outErr)
	}()

	resp, err := t.doRequest(ctx, t.syncTransport, p, req)
	if err != nil {
		return chat.LlmReply{}, false, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	body, err := io.ReadAll(io.LimitReader(resp.Body, t.maxResponse))
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return chat.LlmReply{}, false, llmproto.ErrRequestCanceled
		}

		return chat.LlmReply{}, false, llmproto.NewLlmResponseTransportError(err)
	}

	var parsed llmproto.ChatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return chat.LlmReply{}, false, fmt.Errorf("transport: unmarshal response: %w", err)
	}
	if llmproto.HasError(parsed.Error) {
		return chat.LlmReply{}, false, llmproto.NewLlmResponseError(parsed.Error)
	}

	if len(parsed.Choices) == 0 {
		return chat.LlmReply{}, false, fmt.Errorf("transport: empty choice")
	}

	ttft = pCommon.MilliDiff(start)
	usage = parsed.Usage
	observatory.OnTokenUsage(p.Llm, false, usage)

	choice := parsed.Choices[0]
	msg := choice.Message

	// 非流式时响应包中没有Index字段,按照下标补齐让ToolCall.Index恒有效
	for i := range msg.ToolCalls {
		msg.ToolCalls[i].Index = i
	}

	finish := ""
	if choice.FinishReason != nil {
		finish = strings.TrimSpace(*choice.FinishReason)
	}

	content := strings.TrimSpace(msg.Content)
	if len(content) == 0 && len(msg.ToolCalls) == 0 {
		// 既无内容又无工具调用才是空响应
		return chat.LlmReply{FinishReason: finish}, false, fmt.Errorf("transport: empty stream")
	}

	return chat.LlmReply{Content: content, ToolCalls: msg.ToolCalls, FinishReason: finish}, false, nil
}

// 一次流式调用(不含重试)
func (t *Transport) streamOnce(ctx context.Context, p chat.Provider, req llmproto.ChatCompletionRequest, ob chat.IStreamObserver) (reply chat.LlmReply, emitted bool, outErr error) {
	// 空闲超时:上游长时间无任何数据 -> 取消本次流(归类为可重试的传输层错误)
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	idleTimer := pCommon.CreateTimerWithContext(t.idle, cancel)
	if idleTimer != nil {
		defer idleTimer.Stop()
	}

	start := pCommon.GetDatetimeUtilityInstance().GetNow()
	var (
		ttft  int64
		usage *llmproto.ChatCompletionUsage
	)
	defer func() { observatory.OnLlmTiming(ctx, p.Llm, true, usage, start, ttft, outErr) }()

	resp, err := t.doRequest(streamCtx, t.asyncTransport, p, req)
	if err != nil {
		return chat.LlmReply{}, false, err
	}
	defer func(b io.ReadCloser) { _ = b.Close() }(resp.Body)

	requestId := llmproto.GetRequestIDFromLlmResponse(resp.Header)
	if idleTimer != nil {
		idleTimer.Reset(t.idle)
	}

	var (
		sb       strings.Builder
		finish   string
		reported = make(map[int]bool, 2)
	)
	acc := tool.NewToolCallAccumulator()
	sse := newSSEDecoder(resp.Body, int(t.maxResponse), func() {
		if idleTimer != nil {
			idleTimer.Reset(t.idle) // 收到数据 -> 重置空闲计时器
		}
	})

	for {
		name, data, ok := sse.Next() // 流式取下一行数据
		if !ok || data == "[DONE]" {
			break
		}
		if ctx.Err() != nil {
			return partial(sb.String(), acc.Result(), finish), emitted, llmproto.ErrRequestCanceled
		}

		//TTFT:首个 data 行,非首字节(keep-alive会让首包时间虚低)
		if ttft == 0 {
			ttft = pCommon.MilliDiff(start)
		}

		var chunk llmproto.ChatCompletionChunk
		chunkOK := json.Unmarshal([]byte(data), &chunk) == nil
		if payload, isErr := llmproto.StreamErrorPayload(name, chunk, data); isErr {
			return partial(sb.String(), acc.Result(), finish), emitted, llmproto.NewLlmStreamError(payload, requestId)
		}

		// usage 末包 Choice 为空,必须早于该判断
		if chunkOK && chunk.Usage != nil {
			usage = chunk.Usage
		}

		if !chunkOK || len(chunk.Choices) == 0 {
			continue
		}

		choice := chunk.Choices[0]
		if choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != pCommon.BlankString {
			finish = strings.TrimSpace(*choice.FinishReason)
		}

		d := choice.Delta
		if d.ReasoningContent != pCommon.BlankString {
			emitted = true
			ob.OnReasoning(d.ReasoningContent)
		}

		if d.Content != pCommon.BlankString {
			sb.WriteString(d.Content)
			emitted = true
			ob.OnDelta(d.Content)
		}

		for _, tc := range d.ToolCalls {
			acc.Add(tc)
			if tc.Function.Name != pCommon.BlankString && !reported[tc.Index] {
				reported[tc.Index] = true
				emitted = true
				ob.OnToolIntent(tc.Index, tc.Function.Name)
			}
		}
	}

	if err := sse.Error(); err != nil {
		return partial(sb.String(), acc.Result(), finish), emitted, hCommon.StreamReadError(ctx, streamCtx, err, requestId, t.idle)
	}

	observatory.OnTokenUsage(p.Llm, true, usage)

	reply = partial(sb.String(), acc.Result(), finish)
	// 内容安全拦截可能无任何正式内容: 透出finish_reason 交由上层提示,不误报 "空流"
	if reply.Content == pCommon.BlankString && finish != llmproto.FinishReasonContentFilter {
		return reply, emitted, fmt.Errorf("empty stream")
	}

	return reply, emitted, nil
}

func partial(content string, calls []llmproto.ToolCall, finish string) chat.LlmReply {
	return chat.LlmReply{Content: strings.TrimSpace(content), ToolCalls: calls, FinishReason: finish}
}

// 编译器断言,实现接口
var _ chat.ITransport = (*Transport)(nil)
