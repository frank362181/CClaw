/**
* TTFT 与单次上游调用耗时的测试。
*
* 为什么必须在这里测:TTFT 只在 llm_client 的 SSE 读流循环里可得,
* 上层无论怎么写都伪造不出来 —— 所以这是唯一能锁住该口径的地方。
*
* 三条口径:
*  1) TTFT = 请求发出 → 上游首个 data: 行(不是首个字节);总时长 = 请求发出 → 本轮结束;
*  2) 失败路径必须也上报(慢失败=最该被看见的样本),且此时 TTFT 应为 0;
*  3) 非流式没有"首包"概念,TTFT ≈ TotalMs(同一时刻,允许毫秒级误差)。
**/

package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"my.favorite/cclawservice/harness/api/llmproto"
	"my.favorite/cclawservice/harness/observatory"
)

// captureTiming 注入计时观察者(通过公开注入点,不越层访问私有状态)。
func captureTiming(t *testing.T) func() []observatory.LlmCallTiming {
	t.Helper()

	var mu sync.Mutex
	var out []observatory.LlmCallTiming

	observatory.RegisterLlmTimingObserver(func(s observatory.LlmCallTiming) {
		mu.Lock()
		defer mu.Unlock()
		out = append(out, s)
	})
	t.Cleanup(func() { observatory.RegisterLlmTimingObserver(nil) })

	return func() []observatory.LlmCallTiming {
		mu.Lock()
		defer mu.Unlock()
		return append([]observatory.LlmCallTiming(nil), out...)
	}
}

// TestStreamTtftMeasured 首包延迟 150ms → TTFT 必须落在合理区间,且总时长 ≥ TTFT。
func TestStreamTtftMeasured(t *testing.T) {
	const preDelay = 150 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		fl.Flush()

		time.Sleep(preDelay) // 模拟"连接已建立但上游迟迟不出内容"(prefill/排队)

		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"hi"}}]}`+"\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer srv.Close()

	snapshot := captureTiming(t)

	c := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, _, err := c.ChatCompletionStreamMessages(ctx, testProvider(srv.URL, false),
		[]llmproto.ChatMessage{{Role: "user", Content: "hi"}}, nil, nil); err != nil {
		t.Fatalf("流式调用失败: %v", err)
	}

	got := snapshot()
	if len(got) != 1 {
		t.Fatalf("应上报 1 条计时样本,实际 %d 条: %+v", len(got), got)
	}
	s := got[0]

	if !s.Stream {
		t.Fatal("流式调用应标记 Stream=true")
	}
	if s.TtftMs < 100 {
		t.Fatalf("TTFT 应 ≥100ms(上游首包延迟 150ms),实际 %dms", s.TtftMs)
	}
	if s.TotalMs < s.TtftMs {
		t.Fatalf("总时长(%dms)不应小于 TTFT(%dms)", s.TotalMs, s.TtftMs)
	}
	if s.Err != "" {
		t.Fatalf("成功路径不应带 Err: %s", s.Err)
	}
	if s.TurnScope != "" {
		t.Fatalf("未挂回合归因时 TurnScope 应为空,实际 %q", s.TurnScope)
	}
}

// TestStreamTimingReportedOnError 上游 4xx(不可重试)→ 仍要有样本,且 TTFT=0(一个字节都没收到)。
func TestStreamTimingReportedOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"bad request","type":"invalid_request_error"}}`)
	}))
	defer srv.Close()

	snapshot := captureTiming(t)

	c := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, _, err := c.ChatCompletionStreamMessages(ctx, testProvider(srv.URL, false),
		[]llmproto.ChatMessage{{Role: "user", Content: "hi"}}, nil, nil); err == nil {
		t.Fatal("4xx 应返回错误")
	}

	got := snapshot()
	if len(got) != 1 {
		t.Fatalf("失败路径也必须留痕(应 1 条),实际 %d 条", len(got))
	}
	if got[0].Err == "" {
		t.Fatal("失败样本必须带 Err,否则最该被看见的那次永远查不到")
	}
	if got[0].TtftMs != 0 {
		t.Fatalf("未收到任何 data 行时 TTFT 应为 0,实际 %dms", got[0].TtftMs)
	}
}

// TestSyncTimingTtftEqualsTotal 非流式:没有"首包"概念,TTFT 与总时长取同一时刻。
func TestSyncTimingTtftEqualsTotal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		// 让整次调用 >1ms:Milliseconds() 会截断,本地 httptest 常常算成 0ms
		time.Sleep(120 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],
			"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101}}`)
	}))
	defer srv.Close()

	snapshot := captureTiming(t)

	c := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := c.ChatCompletionMessages(ctx, testProvider(srv.URL, false),
		[]llmproto.ChatMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("同步调用失败: %v", err)
	}

	got := snapshot()
	if len(got) != 1 {
		t.Fatalf("应上报 1 条计时样本,实际 %d 条", len(got))
	}
	s := got[0]
	if s.Stream {
		t.Fatal("非流式不应标记 Stream=true")
	}
	if s.TtftMs < 100 {
		t.Fatalf("非流式 TTFT 应 ≈ 服务端延迟(120ms),实际 %dms", s.TtftMs)
	}
	if diff := s.TotalMs - s.TtftMs; diff < 0 || diff > 5 {
		t.Fatalf("非流式 TTFT 应≈TotalMs,实际 ttft=%dms total=%dms", s.TtftMs, s.TotalMs)
	}
}
