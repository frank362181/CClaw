package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"my.favorite/cclawservice/controller/model"
	myException "my.favorite/cclawservice/exception"
	"my.favorite/cclawservice/model/dataobject"
)

// capturedRequest 记录 mock 服务收到的请求,用于断言请求构造是否正确。
type capturedRequest struct {
	Method  string
	Path    string
	Auth    string
	Payload ChatCompletionRequest
}

// newMockServer 构造一个 chat/completions mock 服务。
// handler 为空时返回一个标准的成功应答。
func newMockServer(t *testing.T, status int, body string, capture *capturedRequest) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture.Method = r.Method
			capture.Path = r.URL.Path
			capture.Auth = r.Header.Get("Authorization")
			_ = json.NewDecoder(r.Body).Decode(&capture.Payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != "" {
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// provider 构造一个用于测试的 LLM Provider(上下文窗口来自 DB 配置)。
func provider(baseURL, apiKey string, contextWindow uint32) dataobject.TLlm {
	return dataobject.TLlm{
		ProviderId:    "p",
		BaseUrl:       baseURL,
		ApiKey:        apiKey,
		ContextWindow: contextWindow,
	}
}

// providerModel 构造带 Model 的 Provider(用于断言请求中的模型名)。
func providerModel(baseURL, apiKey, modelName string, contextWindow uint32) dataobject.TLlm {
	p := provider(baseURL, apiKey, contextWindow)
	p.Model = modelName
	return p
}

// systemPrompt 构造测试用的 system prompt。
func systemPrompt(content string) *dataobject.SystemPrompt {
	return &dataobject.SystemPrompt{Prompt: content}
}

// chatReq 构造测试用的聊天请求。
func chatReq(msg string) *model.ChatRequest {
	return &model.ChatRequest{ReqMsg: msg}
}

const okResponse = `{"id":"chatcmpl-test","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"你好,我是智能助手。"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`

func TestChatCompletion_Success(t *testing.T) {
	var cap capturedRequest
	srv := newMockServer(t, http.StatusOK, okResponse, &cap)

	client := NewClient()
	reply, err := client.ChatCompletion(context.Background(), providerModel(srv.URL, "sk-test", "deep_seek_v4_pro", 8192), systemPrompt("你是助手"), chatReq("你好"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "你好,我是智能助手。" {
		t.Fatalf("unexpected reply: %q", reply)
	}

	// 请求构造断言
	if cap.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", cap.Method)
	}
	if cap.Path != "/chat/completions" {
		t.Errorf("path = %s, want /chat/completions", cap.Path)
	}
	if cap.Auth != "Bearer sk-test" {
		t.Errorf("auth = %q, want Bearer sk-test", cap.Auth)
	}
	if cap.Payload.Model != "deep_seek_v4_pro" {
		t.Errorf("model = %q, want deep_seek_v4_pro", cap.Payload.Model)
	}
	if cap.Payload.Stream {
		t.Error("stream should be false")
	}
	// max_tokens 由 context_window 动态推导,并受供应商输出上限约束:
	// 应为 min(窗口 - 估算输入, DefaultMaxOutputTokens),且小于窗口
	if cap.Payload.MaxTokens == nil {
		t.Fatal("max_tokens should be set when ContextWindow > 0")
	}
	if *cap.Payload.MaxTokens >= 8192 {
		t.Errorf("max_tokens = %d, want < context_window 8192", *cap.Payload.MaxTokens)
	}
	want := 8192 - estimateInputTokens(cap.Payload.Messages)
	if want > DefaultMaxOutputTokens {
		want = DefaultMaxOutputTokens
	}
	if *cap.Payload.MaxTokens != want {
		t.Errorf("max_tokens = %d, want %d (min(window - input, DefaultMaxOutputTokens))", *cap.Payload.MaxTokens, want)
	}
	if len(cap.Payload.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(cap.Payload.Messages))
	}
	if cap.Payload.Messages[0].Role != "system" || cap.Payload.Messages[0].Content != "你是助手" ||
		cap.Payload.Messages[1].Role != "user" || cap.Payload.Messages[1].Content != "你好" {
		t.Errorf("unexpected messages: %+v", cap.Payload.Messages)
	}
}

func TestChatCompletion_TrailingSlashBaseURL(t *testing.T) {
	var cap capturedRequest
	srv := newMockServer(t, http.StatusOK, okResponse, &cap)

	client := NewClient()
	_, err := client.ChatCompletion(context.Background(), provider(srv.URL+"/", "", 0), systemPrompt("sys"), chatReq("hi"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.Path != "/chat/completions" {
		t.Errorf("path = %s, want /chat/completions (trailing slash trimmed)", cap.Path)
	}
}

func TestChatCompletion_NoAuthHeaderWhenKeyEmpty(t *testing.T) {
	var cap capturedRequest
	srv := newMockServer(t, http.StatusOK, okResponse, &cap)

	client := NewClient()
	_, err := client.ChatCompletion(context.Background(), provider(srv.URL, "", 0), systemPrompt("sys"), chatReq("hi"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cap.Auth != "" {
		t.Errorf("auth = %q, want empty when apiKey is empty", cap.Auth)
	}
}

func TestChatCompletion_Non2xx(t *testing.T) {
	srv := newMockServer(t, http.StatusUnauthorized,
		`{"error":{"message":"Invalid API key","type":"authentication_error","code":"invalid_api_key"}}`, nil)

	client := NewClient()
	_, err := client.ChatCompletion(context.Background(), provider(srv.URL, "bad", 0), systemPrompt("sys"), chatReq("hi"))
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("error should contain status and message, got: %v", err)
	}
}

func TestChatCompletion_Non2xxPlainBody(t *testing.T) {
	srv := newMockServer(t, http.StatusInternalServerError, "boom", nil)

	client := NewClient()
	_, err := client.ChatCompletion(context.Background(), provider(srv.URL, "k", 0), systemPrompt("sys"), chatReq("hi"))
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error should contain raw body, got: %v", err)
	}
}

func TestChatCompletion_UpstreamErrorField(t *testing.T) {
	srv := newMockServer(t, http.StatusOK,
		`{"error":{"message":"upstream exploded","type":"server_error"}}`, nil)

	client := NewClient()
	_, err := client.ChatCompletion(context.Background(), provider(srv.URL, "k", 0), systemPrompt("sys"), chatReq("hi"))
	if err == nil {
		t.Fatal("expected error from error field, got nil")
	}
	if !strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("error should mention upstream message, got: %v", err)
	}
}

func TestChatCompletion_EmptyChoices(t *testing.T) {
	srv := newMockServer(t, http.StatusOK, `{"id":"x","choices":[]}`, nil)

	client := NewClient()
	_, err := client.ChatCompletion(context.Background(), provider(srv.URL, "k", 0), systemPrompt("sys"), chatReq("hi"))
	if err == nil {
		t.Fatal("expected error for empty choices, got nil")
	}
}

func TestChatCompletion_EmptyContent(t *testing.T) {
	srv := newMockServer(t, http.StatusOK,
		`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"  "}}]}`, nil)

	client := NewClient()
	_, err := client.ChatCompletion(context.Background(), provider(srv.URL, "k", 0), systemPrompt("sys"), chatReq("hi"))
	if err == nil {
		t.Fatal("expected error for empty content, got nil")
	}
}

func TestChatCompletion_ContextCanceled(t *testing.T) {
	srv := newMockServer(t, http.StatusOK, okResponse, nil)

	client := NewClient()
	// 请求发出前上下文已取消,模拟客户端断开 → 应返回 ErrRequestCanceled 而非通用错误
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.ChatCompletion(ctx, provider(srv.URL, "k", 0), systemPrompt("sys"), chatReq("hi"))
	if !errors.Is(err, myException.ErrRequestCanceled) {
		t.Fatalf("expected ErrRequestCanceled, got: %v", err)
	}
}

func TestChatCompletionStream_Success(t *testing.T) {
	streamBody := "" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"你\"}}]}\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"}}]}\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"世界\"}}]}\n" +
		"data: [DONE]\n"
	var cap capturedRequest
	srv := newMockServer(t, http.StatusOK, streamBody, &cap)

	client := NewClient()
	var got []string
	full, err := client.ChatCompletionStream(context.Background(), provider(srv.URL, "k", 0), systemPrompt("sys"), chatReq("hi"), func(delta string) { got = append(got, delta) }, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if full != "你好世界" {
		t.Errorf("full = %q, want 你好世界", full)
	}
	if len(got) != 3 || got[0] != "你" || got[1] != "好" || got[2] != "世界" {
		t.Errorf("chunks = %v, want [你 好 世界]", got)
	}
	if !cap.Payload.Stream {
		t.Error("request stream should be true")
	}
}

func TestChatCompletionStream_UpstreamErrorEvent(t *testing.T) {
	streamBody := "data: {\"error\":{\"message\":\"boom during stream\",\"type\":\"server_error\"}}\n"
	srv := newMockServer(t, http.StatusOK, streamBody, nil)

	client := NewClient()
	_, err := client.ChatCompletionStream(context.Background(), provider(srv.URL, "k", 0), systemPrompt("sys"), chatReq("hi"), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "boom during stream") {
		t.Fatalf("expected upstream stream error, got: %v", err)
	}
}

func TestChatCompletionStream_EmptyContent(t *testing.T) {
	srv := newMockServer(t, http.StatusOK, "data: [DONE]\n", nil)

	client := NewClient()
	_, err := client.ChatCompletionStream(context.Background(), provider(srv.URL, "k", 0), systemPrompt("sys"), chatReq("hi"), nil, nil)
	if err == nil {
		t.Fatal("expected empty stream content error, got nil")
	}
}

// TestChatCompletionStream_ReasoningContent 验证思考过程增量(reasoning_content)单独转发、
// 且不并入正式回复:content 只累计正式内容,reasoning 回调只收到思考增量。
func TestChatCompletionStream_ReasoningContent(t *testing.T) {
	streamBody := "" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"让我\",\"content\":\"\"}}]}\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"想想\",\"content\":\"\"}}]}\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"reasoning_content\":\"\",\"content\":\"你好\"}}]}\n" +
		"data: [DONE]\n"
	srv := newMockServer(t, http.StatusOK, streamBody, nil)

	client := NewClient()
	var content []string
	var reasoning []string
	full, err := client.ChatCompletionStream(context.Background(), provider(srv.URL, "k", 0), systemPrompt("sys"), chatReq("hi"),
		func(delta string) { content = append(content, delta) },
		func(delta string) { reasoning = append(reasoning, delta) })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if full != "你好" {
		t.Errorf("full = %q, want 你好(不含思考过程)", full)
	}
	if len(content) != 1 || content[0] != "你好" {
		t.Errorf("content chunks = %v, want [你好]", content)
	}
	if len(reasoning) != 2 || reasoning[0] != "让我" || reasoning[1] != "想想" {
		t.Errorf("reasoning chunks = %v, want [让我 想想]", reasoning)
	}
}

// TestChatCompletionStream_NoOverallTimeout 回归测试(A13):流式客户端不得有整体超时。
// 同步客户端被配置为极短超时(150ms),mock 上游却以 3×100ms 缓慢输出(总计约 300ms)。
// 修复前流式复用同步客户端 → 整体超时在内容输出中触发并报错;
// 修复后流式使用独立客户端(Timeout=0) → 完整收到全部增量。
func TestChatCompletionStream_NoOverallTimeout(t *testing.T) {
	client := newClient(150 * time.Millisecond)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, ch := range []string{"a", "b", "c"} {
			time.Sleep(100 * time.Millisecond)
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"%s\"}}]}\n\n", ch)
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer srv.Close()

	var got []string
	full, err := client.ChatCompletionStream(context.Background(), provider(srv.URL, "k", 0), systemPrompt("sys"), chatReq("hi"), func(delta string) { got = append(got, delta) }, nil)
	if err != nil {
		t.Fatalf("stream longer than sync timeout should succeed, got error: %v", err)
	}
	if full != "abc" {
		t.Errorf("full = %q, want %q", full, "abc")
	}
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("chunks = %v, want [a b c]", got)
	}
}

// ==================== 构造器测试 ====================

func TestBuildChatMessage(t *testing.T) {
	messages := BuildChatMessage(systemPrompt("系统提示"), chatReq("用户消息"))
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}
	if messages[0].Role != "system" || messages[0].Content != "系统提示" {
		t.Errorf("system message = %+v", messages[0])
	}
	if messages[1].Role != "user" || messages[1].Content != "用户消息" {
		t.Errorf("user message = %+v", messages[1])
	}
}

func TestBuildChatCompletionRequest(t *testing.T) {
	p := providerModel("http://x", "k", "deep_seek_v4_pro", 0)
	req := BuildChatCompletionRequest(p, systemPrompt("sys"), chatReq("hi"), true)
	if req.Model != "deep_seek_v4_pro" {
		t.Errorf("model = %q, want deep_seek_v4_pro", req.Model)
	}
	if !req.Stream {
		t.Error("stream should be true when stream=true")
	}
	if len(req.Messages) != 2 {
		t.Errorf("messages len = %d, want 2", len(req.Messages))
	}

	// 未配置 Model 时回退 ProviderId
	req2 := BuildChatCompletionRequest(provider("http://x", "k", 0), systemPrompt("sys"), chatReq("hi"), false)
	if req2.Model != "p" {
		t.Errorf("model = %q, want fallback ProviderId p", req2.Model)
	}
	if req2.Stream {
		t.Error("stream should be false when stream=false")
	}
}

// TestBuildChatCompletionRequest_DynamicMaxTokens 验证 max_tokens 由 context_window 动态推导:
// 超长输入时,max_tokens 应被压到"窗口剩余空间"(窗口 - 估算输入)以内。
func TestBuildChatCompletionRequest_DynamicMaxTokens(t *testing.T) {
	// 窗口 4096、超长中文输入(3000 字):估算输入 ≈ (3000/1.5 + 8) * 1.2 ≈ 2410
	// → max_tokens = 4096 - 2410 = 1687,远小于窗口本身
	p := dataobject.TLlm{
		ProviderId:    "p",
		Model:         "m",
		BaseUrl:       "http://x",
		ContextWindow: 4096,
	}
	longInput := strings.Repeat("中", 3000)
	req := BuildChatCompletionRequest(p, systemPrompt(longInput), chatReq("hi"), false)
	if req.MaxTokens == nil {
		t.Fatal("max_tokens should be set when ContextWindow > 0")
	}
	if *req.MaxTokens >= 4096 {
		t.Errorf("max_tokens = %d, want < context_window 4096 (should account for input)", *req.MaxTokens)
	}

	// 不变量:估算输入 + max_tokens = context_window(推导自窗口剩余空间)
	if est := estimateInputTokens(req.Messages); est+*req.MaxTokens != int(p.ContextWindow) {
		t.Errorf("invariant violated: estimateInput(%d) + max_tokens(%d) = %d, want context_window(%d)",
			est, *req.MaxTokens, est+*req.MaxTokens, p.ContextWindow)
	}
}

// TestBuildChatCompletionRequest_DynamicMaxTokens_Room 验证窗口充足时,
// max_tokens 仍受供应商输出上限约束:取 DefaultMaxOutputTokens,而非整个窗口。
// (回归:早期按窗口推导,1M 窗口 + 小输入 → max_tokens ≈ 1M,会被供应商以
//
//	"max tokens is too long" 拒绝。)
func TestBuildChatCompletionRequest_DynamicMaxTokens_Room(t *testing.T) {
	p := dataobject.TLlm{
		ProviderId:    "p",
		Model:         "m",
		BaseUrl:       "http://x",
		ContextWindow: 1_000_000,
	}
	req := BuildChatCompletionRequest(p, systemPrompt("你是助手"), chatReq("你好"), false)
	if req.MaxTokens == nil {
		t.Fatal("max_tokens should be set when ContextWindow > 0")
	}
	// 窗口剩余空间远大于默认上限 → 被钳制到 DefaultMaxOutputTokens
	if *req.MaxTokens != DefaultMaxOutputTokens {
		t.Errorf("max_tokens = %d, want DefaultMaxOutputTokens %d (ceiling caps huge window)",
			*req.MaxTokens, DefaultMaxOutputTokens)
	}
	// 且不得超过默认上限(供应商侧安全兜底)
	if *req.MaxTokens > DefaultMaxOutputTokens {
		t.Errorf("max_tokens = %d, should not exceed %d", *req.MaxTokens, DefaultMaxOutputTokens)
	}
}

// TestBuildChatCompletionRequest_DynamicMaxTokens_NoWindow 验证未配置 ContextWindow(0)时,
// max_tokens 保持 nil(不发送该字段,由供应商使用其默认输出上限)。
func TestBuildChatCompletionRequest_DynamicMaxTokens_NoWindow(t *testing.T) {
	p := dataobject.TLlm{ProviderId: "p", Model: "m", BaseUrl: "http://x"}
	req := BuildChatCompletionRequest(p, systemPrompt("sys"), chatReq("hi"), false)
	if req.MaxTokens != nil {
		t.Errorf("max_tokens = %v, want nil when ContextWindow==0", req.MaxTokens)
	}
}

// TestEstimateInputTokens 验证估算函数的基本行为:CJK 密集文本比 ASCII 估算出更多 token。
func TestEstimateInputTokens(t *testing.T) {
	cjk := estimateInputTokens([]ChatMessage{{Role: "user", Content: strings.Repeat("中", 1000)}})
	ascii := estimateInputTokens([]ChatMessage{{Role: "user", Content: strings.Repeat("a", 1000)}})
	if cjk <= ascii {
		t.Errorf("CJK estimate (%d) should exceed ASCII estimate (%d)", cjk, ascii)
	}
}

func TestChatCompletion_Validation(t *testing.T) {
	client := NewClient()
	cases := []struct {
		name     string
		provider dataobject.TLlm
	}{
		// ProviderId 与 Model 均为空 → GetModelName() 返回空 → model is empty
		{"empty model", dataobject.TLlm{BaseUrl: "http://x"}},
		// BaseUrl 为空
		{"empty base url", dataobject.TLlm{ProviderId: "p"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := client.ChatCompletion(context.Background(), tc.provider, systemPrompt("sys"), chatReq("hi")); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

// TestPrepareRequest_Validation 直接覆盖 prepareRequest 的三个入参护栏
// (empty messages 经 BuildChatCompletionRequest 后不可达,故走低层单测)。
func TestPrepareRequest_Validation(t *testing.T) {
	client := NewClient()
	ctx := context.Background()
	base := dataobject.TLlm{ProviderId: "p", BaseUrl: "http://x"}

	cases := []struct {
		name string
		req  ChatCompletionRequest
	}{
		{"empty model", ChatCompletionRequest{Messages: []ChatMessage{{Role: "user", Content: "hi"}}}},
		{"empty messages", ChatCompletionRequest{Model: "m"}},
		{"empty base url", ChatCompletionRequest{Model: "m", Messages: []ChatMessage{{Role: "user", Content: "hi"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			if tc.name == "empty base url" {
				p.BaseUrl = ""
			}
			if _, err := client.prepareRequest(ctx, p, tc.req); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}
