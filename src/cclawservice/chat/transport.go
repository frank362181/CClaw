/*
* http请求和解析类
**/

package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"my.favorite/cclawservice/common"
	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/exception"
	hCommon "my.favorite/cclawservice/harness/common"
	"my.favorite/cclawservice/harness/observatory"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
	pException "my.favorite/platformutility/exception"
)

type TransportPool struct {
	maxIdleConnections        int   // 全局空闲链接总数上限
	maxIdleConnectionsPerHost int   // 同一个LLM的峰值并发
	maxIdleConnectionTimeout  int64 // 链接的最大空闲时间
	maxTLSHandleShakeTimeout  int64 // 链接最大的握手时间长度
	maxResponseHeaderTimeout  int64 // 首字节等待的最大超时时间

	httpClientPool *http.Transport
}

var transportPoolInstance = pCommon.GetSingleton(func() *TransportPool {
	pool := &TransportPool{
		maxIdleConnections:        common.DefaultMaxChatConcurrency,
		maxIdleConnectionsPerHost: hCommon.MaxIdleConnectionsPerHost, // default
		maxIdleConnectionTimeout:  hCommon.MaxIdleConnectionTimeout,
		maxTLSHandleShakeTimeout:  hCommon.MaxTLSHandleShakeTimeout,
		maxResponseHeaderTimeout:  hCommon.NonStreamResponseTimeout,

		httpClientPool: http.DefaultTransport.(*http.Transport).Clone(),
	}

	pool.httpClientPool.MaxIdleConns = pool.maxIdleConnections
	pool.httpClientPool.MaxIdleConnsPerHost = pool.maxIdleConnectionsPerHost
	pool.httpClientPool.IdleConnTimeout = time.Duration(pool.maxIdleConnectionTimeout) * time.Second
	pool.httpClientPool.TLSHandshakeTimeout = time.Duration(pool.maxTLSHandleShakeTimeout) * time.Second
	pool.httpClientPool.ResponseHeaderTimeout = time.Duration(pool.maxResponseHeaderTimeout) * time.Second

	return pool
}).Instance()

func GetTransportPool() *TransportPool {
	return transportPoolInstance
}

// Close 关闭池内所有空闲链接
func (pool *TransportPool) Close() {
	if pool.httpClientPool != nil {
		pool.httpClientPool.CloseIdleConnections()
	}
}

// TransportClient 是chat.ITransport的 **唯一**实现类:只有两个方法
// 进程级单例子:http.Client内部有链接池
type TransportClient struct {
	syncTransport  *http.Client
	asyncTransport *http.Client

	streamIdle  time.Duration //空闲的时间
	maxResponse int64
	retryPolicy RetryPolicy
}

func NewTransportClient(responseTimeout int) ITransportClient {
	if responseTimeout <= 0 {
		responseTimeout = hCommon.NonStreamResponseTimeout
	}

	pool := GetTransportPool()

	return &TransportClient{
		syncTransport:  &http.Client{Timeout: time.Duration(responseTimeout) * time.Second, Transport: pool.httpClientPool},
		asyncTransport: &http.Client{Timeout: time.Duration(hCommon.MaxStreamTotalTimeout) * time.Second, Transport: pool.httpClientPool},
		maxResponse:    hCommon.MaxResponseBytes,
		streamIdle:     time.Duration(hCommon.StreamResponseTimeout) * time.Second,
		retryPolicy: DefaultRetryPolicy(hCommon.LlmRetryMaxAttempts, hCommon.LlmRetryBaseDelayMs,
			hCommon.LlmRetryMaxDelayMs, hCommon.LlmRetryJitterRatio),
	}
}

// SimpleComplete 在多agent、多skill等场景下,专门用来支持通过LLM来实现选取合适的agent或skill
func (t *TransportClient) SimpleComplete(ctx context.Context, llm dataobject.TLlm, system, user string, maxTokens int) (llmproto.LlmReply, error) {
	messages := buildSimpleChatMessage(system, user)
	builtReq := buildChatCompletionRequestWithString(llm, messages, maxTokens, false)

	reply, err := t.retryAndDegrade(ctx, &builtReq, NewDegrader(), func(ctx context.Context) (llmproto.LlmReply, bool, error) {
		return t.completeOnce(ctx, Provider{Llm: llm}, builtReq)
	})

	return reply, err
}

// Complete non-stream的聊天函数,等待LLM全部完成再返回
func (t *TransportClient) Complete(ctx context.Context, p Provider, req llmproto.LlmRequest) (llmproto.LlmReply, error) {
	builtReq := buildChatCompletionRequestWithToolChoice(p, req, false)

	return t.retryAndDegrade(ctx, &builtReq, NewDegrader(), func(ctx context.Context) (llmproto.LlmReply, bool, error) {
		return t.completeOnce(ctx, p, builtReq)
	})
}

func (t *TransportClient) Stream(ctx context.Context, p Provider, req llmproto.LlmRequest, ob IStreamObserver) (llmproto.LlmReply, error) {
	if ob == nil {
		ob = NopObserver{}
	}

	builtReq := buildChatCompletionRequestWithToolChoice(p, req, true)

	return t.retryAndDegrade(ctx, &builtReq, NewDegrader(), func(ctx context.Context) (llmproto.LlmReply, bool, error) {
		return t.streamOnce(ctx, p, builtReq, ob)
	})
}

// =============================== private functions =================================

// exec "重试 + 参数降级" 的唯一入口(流式与非流式共用)
//
// 语义约定:
//
//  1. 内层 Retry:只负责瞬时故障(限流/5xx/连接失败)重试;
//  2. 降级:把“请求写错了”和“服务不稳定”彻底分开,降级成功后不受可重试性限制再跑一轮;
//  3. 已经对外产出(emitted)时绝不重跑,否则客户端会看到重复/错乱的内容。
func (t *TransportClient) retryAndDegrade(ctx context.Context, builtReq *llmproto.ChatCompletionRequest, degrader *Degrader,
	call func(context.Context) (llmproto.LlmReply, bool, error)) (llmproto.LlmReply, error) {

	var (
		reply   llmproto.LlmReply
		err     error
		emitted bool
	)

	for round := 0; round < hCommon.MaxDegradeRounds; round++ { // 这里仅2次循环和降级
		emitted = false
		reply, err = Retry(ctx, t.retryPolicy, func(ctx context.Context) (llmproto.LlmReply, bool, error) {
			rep, streamed, callErr := call(ctx)
			if streamed {
				emitted = true
			}

			return rep, streamed, callErr
		})

		if err == nil || emitted || !degrader.apply(builtReq, err) {
			return reply, err
		}
	}

	return reply, err
}

// 发出请求并校验状态码,返回的resp由调用方负责close
func (t *TransportClient) doRequest(ctx context.Context, c *http.Client, p Provider, req llmproto.ChatCompletionRequest) (*http.Response, error) {
	httpReq, err := buildHttpRequest(ctx, p, req)
	if err != nil {
		return nil, err
	}

	resp, err := c.Do(httpReq)
	if err != nil {
		if pException.IsContextCanceled(ctx, err) {
			return nil, llmproto.ErrRequestCanceled
		}

		return nil, llmproto.GetLlmResponseTransportError(err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer func(body io.ReadCloser) {
			_ = body.Close()
		}(resp.Body)
		return nil, llmproto.ParseLlmResponseError(resp, t.maxResponse)
	}

	return resp, nil
}

func (t *TransportClient) completeOnce(ctx context.Context, p Provider, req llmproto.ChatCompletionRequest) (reply llmproto.LlmReply, emitted bool, outErr error) {
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
		return llmproto.LlmReply{}, false, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)
	requestId := llmproto.GetRequestIDFromLlmResponse(resp.Header)

	body, err := io.ReadAll(io.LimitReader(resp.Body, t.maxResponse))
	if err != nil {
		if errors.Is(err, exception.ChatResponseTooLargeKind) {
			return llmproto.LlmReply{}, false, err
		}

		if pException.IsContextCanceled(ctx, err) {
			return llmproto.LlmReply{}, false, llmproto.ErrRequestCanceled
		}

		return llmproto.LlmReply{}, false, llmproto.GetLlmResponseTransportError(err)
	}

	var parsed llmproto.ChatCompletionResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return llmproto.LlmReply{}, false, fmt.Errorf("transport: unmarshal response: %w", err)
	}
	if llmproto.HasError(parsed.Error) {
		return llmproto.LlmReply{}, false, llmproto.GetLlmResponseError(parsed.Error)
	}

	if len(parsed.Choices) == 0 {
		return llmproto.LlmReply{}, false, fmt.Errorf("transport: empty choice")
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
		return llmproto.LlmReply{FinishReason: finish}, false, llmproto.GetLlmEmptyResponseError(requestId)
	}

	return llmproto.LlmReply{Content: content, ToolCalls: msg.ToolCalls, FinishReason: finish}, false, nil
}

// 一次流式调用(不含重试)
func (t *TransportClient) streamOnce(ctx context.Context, p Provider, req llmproto.ChatCompletionRequest, ob IStreamObserver) (reply llmproto.LlmReply, committed bool, outErr error) {
	// 空闲超时:上游长时间无任何数据 -> 取消本次流(归类为可重试的传输层错误)
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	idleTimer := pCommon.CreateTimerWithContext(t.streamIdle, cancel)
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
		return llmproto.LlmReply{}, false, err
	}
	defer func(b io.ReadCloser) { _ = b.Close() }(resp.Body)

	// 取消立刻关闭 -> 打断阻塞中的 sse.Next()(底层是 bufio.Scanner.Scan)
	stopCancelWatch := context.AfterFunc(streamCtx, func() { _ = resp.Body.Close() })
	defer stopCancelWatch()

	requestId := llmproto.GetRequestIDFromLlmResponse(resp.Header)
	if idleTimer != nil {
		idleTimer.Reset(t.streamIdle)
	}

	var (
		sb       strings.Builder
		finish   string
		reported = make(map[int]bool, 2)
	)
	acc := NewToolCallAccumulator()
	body := newLimitedReader(resp.Body, t.maxResponse) // 流式每次读取的上限
	sse := newSSEDecoder(body, hCommon.MaxSSELineBytes, func() {
		if idleTimer != nil {
			idleTimer.Reset(t.streamIdle) // 收到数据 -> 重置空闲计时器
		}
	})

	for {
		name, data, ok := sse.Next() // 流式取下一行数据
		if !ok || data == "[DONE]" {
			break
		}
		if ctx.Err() != nil {
			return buildPartial(sb.String(), acc.Result(), finish), committed, llmproto.ErrRequestCanceled
		}

		//TTFT:首个 data 行,非首字节(keep-alive会让首包时间虚低)
		if ttft == 0 {
			ttft = pCommon.MilliDiff(start)
		}

		var chunk llmproto.ChatCompletionChunk
		chunkOK := json.Unmarshal([]byte(data), &chunk) == nil
		if payload, isErr := llmproto.StreamErrorPayload(name, chunk, data); isErr {
			return buildPartial(sb.String(), acc.Result(), finish), committed, llmproto.GetLlmStreamError(payload, requestId)
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
			//reasoned = true // 只做观测:可撤回,不阻止重试
			ob.OnReasoning(d.ReasoningContent)
		}

		if d.Content != pCommon.BlankString {
			sb.WriteString(d.Content)
			committed = true
			ob.OnDelta(d.Content)
		}

		for _, tc := range d.ToolCalls {
			acc.Add(tc)
			if tc.Function.Name != pCommon.BlankString && !reported[tc.Index] {
				reported[tc.Index] = true
				committed = true
				ob.OnToolIntent(tc.Index, tc.Function.Name)
			}
		}
	}

	if err := sse.Error(); err != nil {
		if ctx.Err() != nil {
			return buildPartial(sb.String(), acc.Result(), finish), committed, llmproto.ErrRequestCanceled
		}
		return buildPartial(sb.String(), acc.Result(), finish), committed, hCommon.StreamReadError(ctx, streamCtx, err, requestId, t.streamIdle)
	}

	observatory.OnTokenUsage(p.Llm, true, usage)

	reply = buildPartial(sb.String(), acc.Result(), finish)
	// 内容安全拦截可能无任何正式内容: 透出finish_reason 交由上层提示,不误报 "空流"
	if reply.Content == pCommon.BlankString && len(reply.ToolCalls) == 0 && finish != llmproto.FinishReasonContentFilter {
		return reply, committed, llmproto.GetLlmEmptyStreamError(requestId)
	}

	return reply, committed, nil
}

// 编译器断言,实现接口
var _ ITransportClient = (*TransportClient)(nil)
