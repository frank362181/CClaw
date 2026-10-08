/**
* 上游 usage 采集的端到端(离线)测试:用假 SSE/JSON 服务端验证
*  1) 流式 usage 只出现在 choices 为空的末包 —— 必须仍被采到;
*  2) 请求体必须注入 stream_options.include_usage(否则供应商压根不下发 usage);
*  3) 未上报 usage 的流不产生脏样本;
*  4) 两口径(DeepSeek 顶层 / OpenAI prompt_tokens_details)都能归一。
**/

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"my.favorite/cclawservice/harness/api/llmproto"
	"my.favorite/cclawservice/harness/observatory"
	"my.favorite/cclawservice/model/dataobject"
)

// captureUsage 安装 token 观察者并返回快照函数与还原函数。
func captureUsage(t *testing.T) (func() []observatory.TokenUsageEvent, func()) {
	t.Helper()

	var mu sync.Mutex
	events := make([]observatory.TokenUsageEvent, 0, 4)

	observatory.RegisterTokenUsageObserver(func(e observatory.TokenUsageEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	})

	snapshot := func() []observatory.TokenUsageEvent {
		mu.Lock()
		defer mu.Unlock()
		out := make([]observatory.TokenUsageEvent, len(events))
		copy(out, events)
		return out
	}
	return snapshot, func() { observatory.RegisterTokenUsageObserver(nil) }
}

// sseServer 起一个假 SSE 服务端,按行回放 lines,并记录请求体。
// 返回的第二个函数用于读取服务端收到的请求体(延迟读取)。
func sseServer(t *testing.T, lines []string) (*httptest.Server, func() string) {
	t.Helper()

	var mu sync.Mutex
	body := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(raw)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, l := range lines {
			_, _ = io.WriteString(w, l+"\n")
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	t.Cleanup(srv.Close)

	return srv, func() string {
		mu.Lock()
		defer mu.Unlock()
		return body
	}
}

func testProvider(url string, usageInStreaming bool) dataobject.TLlm {
	return dataobject.TLlm{
		ProviderId:       "deepseek_v4_pro",
		Model:            "deepseek-v4-pro",
		BaseUrl:          url,
		ApiKey:           "test-key",
		UsageInStreaming: usageInStreaming,
		InputCost:        1,
		CachedReadCost:   0.1,
	}
}

// TestStreamUsageOnlyChunkIsCaptured 回归护栏:usage 末包的 choices 为空,
// 采集必须早于"空 choices 跳过"分支,否则流式永远统计不到命中。
func TestStreamUsageOnlyChunkIsCaptured(t *testing.T) {
	srv, bodyRef := sseServer(t, []string{
		`data: {"choices":[{"index":0,"delta":{"content":"你好"}}]}`,
		``,
		`data: {"choices":[],"usage":{"prompt_tokens":1000,"completion_tokens":10,"total_tokens":1010,"prompt_cache_hit_tokens":800,"prompt_cache_miss_tokens":200}}`,
		``,
		`data: [DONE]`,
		``,
	})

	snapshot, restore := captureUsage(t)
	defer restore()

	c := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	content, _, err := c.ChatCompletionStreamMessages(ctx, testProvider(srv.URL, true),
		[]llmproto.ChatMessage{{Role: "user", Content: "hi"}}, nil, nil)
	if err != nil {
		t.Fatalf("流式调用失败: %v", err)
	}
	if content != "你好" {
		t.Fatalf("内容拼接错误: %q", content)
	}

	events := snapshot()
	if len(events) != 1 {
		t.Fatalf("应上报 1 次用量,实际 %d 次", len(events))
	}
	e := events[0]
	if e.Prompt != 1000 || e.Cached != 800 {
		t.Fatalf("用量解析错误: %+v", e)
	}
	if !e.Stream {
		t.Fatal("流式调用应标记 Stream=true")
	}
	if !e.HasCacheReport {
		t.Fatal("DeepSeek 下发了缓存字段,应判定为已上报")
	}
	if e.Model != "deepseek-v4-pro" {
		t.Fatalf("模型名错误: %s", e.Model)
	}

	// 请求体必须带 stream_options.include_usage
	var sent llmproto.ChatCompletionRequest
	if err := json.Unmarshal([]byte(bodyRef()), &sent); err != nil {
		t.Fatalf("请求体解析失败: %v", err)
	}
	if !sent.Stream {
		t.Fatalf("应请求流式: %s", bodyRef())
	}
	if sent.StreamOptions == nil || !sent.StreamOptions.IncludeUsage {
		t.Fatalf("缺少 stream_options.include_usage,供应商将不下发 usage: %s", bodyRef())
	}
}

// TestStreamUsageNotInjectedWhenDisabled 供应商不支持时不得注入该参数。
func TestStreamUsageNotInjectedWhenDisabled(t *testing.T) {
	srv, bodyRef := sseServer(t, []string{
		`data: {"choices":[{"index":0,"delta":{"content":"ok"}}]}`,
		``,
		`data: [DONE]`,
		``,
	})

	snapshot, restore := captureUsage(t)
	defer restore()

	c := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, _, err := c.ChatCompletionStreamMessages(ctx, testProvider(srv.URL, false),
		[]llmproto.ChatMessage{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatalf("流式调用失败: %v", err)
	}

	if events := snapshot(); len(events) != 0 {
		t.Fatalf("上游未下发 usage 时不应产生样本: %+v", events)
	}

	var sent llmproto.ChatCompletionRequest
	if err := json.Unmarshal([]byte(bodyRef()), &sent); err != nil {
		t.Fatalf("请求体解析失败: %v", err)
	}
	if sent.StreamOptions != nil {
		t.Fatalf("UsageInStreaming=false 时不应注入 stream_options: %s", bodyRef())
	}
}

// TestSyncCompletionReportsUsage 非流式路径同样要上报,并归一 OpenAI 口径。
func TestSyncCompletionReportsUsage(t *testing.T) {
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],
			"usage":{"prompt_tokens":1000,"completion_tokens":5,"total_tokens":1005,
			         "prompt_tokens_details":{"cached_tokens":640}}}`)
	}))
	defer srv.Close()

	snapshot, restore := captureUsage(t)
	defer restore()

	c := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.ChatCompletionMessages(ctx, testProvider(srv.URL, false),
		[]llmproto.ChatMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("同步调用失败: %v", err)
	}
	_ = raw

	events := snapshot()
	if len(events) != 1 {
		t.Fatalf("应上报 1 次用量,实际 %d 次", len(events))
	}
	e := events[0]
	if e.Stream {
		t.Fatal("非流式调用不应标记 Stream=true")
	}
	if e.Cached != 640 {
		t.Fatalf("OpenAI 口径命中 token 未归一: cached=%d(检查 prompt_tokens_details 的 json tag)", e.Cached)
	}
}
