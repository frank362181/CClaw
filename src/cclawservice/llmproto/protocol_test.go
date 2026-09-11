/**
* 协议层纯逻辑的表驱动测试:分类、错误体解析、SSE 载荷提取、重试判定、request_id 提取。
*
* 覆盖重点(此前仅由 harness 间接覆盖或完全未覆盖):
*   - ClassifyLlmError 的优先级链:内容安全 > 额度 > HTTP 状态码 > 文案兜底;
*   - parseLlmErrorBody 的三种形态(OpenAI 包裹体 / 扁平体 / 非法 JSON);
*   - IsRetryableLlmError 的可重试与不可重试边界(传输层/429/5xx/取消/配置类);
*   - StreamErrorPayload 的两种取载荷方式与大小写不敏感的事件名。
**/

package llmproto

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// ==================== ClassifyLlmError ====================

func TestClassifyLlmError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		api    LlmAPIError
		want   LlmErrorCategory
	}{
		// 1) HTTP 状态码(最可靠)
		{"401→auth", http.StatusUnauthorized, LlmAPIError{}, LlmCategoryAuth},
		{"402→quota", http.StatusPaymentRequired, LlmAPIError{}, LlmCategoryQuota},
		{"403→forbidden", http.StatusForbidden, LlmAPIError{}, LlmCategoryForbidden},
		{"404→not_found", http.StatusNotFound, LlmAPIError{}, LlmCategoryNotFound},
		{"429→rate_limit", http.StatusTooManyRequests, LlmAPIError{}, LlmCategoryRateLimit},
		{"500→server", http.StatusInternalServerError, LlmAPIError{}, LlmCategoryServer},
		{"503→server", http.StatusServiceUnavailable, LlmAPIError{}, LlmCategoryServer},
		{"400→invalid_request", http.StatusBadRequest, LlmAPIError{}, LlmCategoryInvalidRequest},
		{"422→invalid_request", http.StatusUnprocessableEntity, LlmAPIError{}, LlmCategoryInvalidRequest},

		// 2) 优先级:内容安全 > 额度 > 状态码
		{"429+content_filter→content_filter(内容安全优先)", http.StatusTooManyRequests,
			LlmAPIError{Code: "content_filter"}, LlmCategoryContentFilter},
		{"400+敏感→content_filter", http.StatusBadRequest,
			LlmAPIError{Message: "输入包含敏感内容"}, LlmCategoryContentFilter},
		{"429+insufficient_quota→quota(额度优先于限流)", http.StatusTooManyRequests,
			LlmAPIError{Code: "insufficient_quota"}, LlmCategoryQuota},
		{"400+余额不足→quota", http.StatusBadRequest,
			LlmAPIError{Message: "账户余额不足,请充值"}, LlmCategoryQuota},

		// 3) 无可用状态码(流中错误 statusCode=200)→ 文案兜底
		{"200+rate limit→rate_limit", http.StatusOK,
			LlmAPIError{Message: "Requests rate limit exceeded"}, LlmCategoryRateLimit},
		{"200+invalid api key→auth", http.StatusOK,
			LlmAPIError{Message: "Invalid API key provided"}, LlmCategoryAuth},
		{"200+does not exist→not_found", http.StatusOK,
			LlmAPIError{Message: "The model does not exist"}, LlmCategoryNotFound},
		{"200+internal error→server", http.StatusOK,
			LlmAPIError{Message: "internal error, please retry"}, LlmCategoryServer},
		{"200+bad request→invalid_request", http.StatusOK,
			LlmAPIError{Message: "bad request: context_length exceeded"}, LlmCategoryInvalidRequest},
		{"200+无关文案→unknown", http.StatusOK,
			LlmAPIError{Message: "something weird happened"}, LlmCategoryUnknown},

		// 4) 大小写与空格不敏感
		{"大小写不敏感→auth", 0, LlmAPIError{Code: "  INVALID_API_KEY  "}, LlmCategoryAuth},

		// 5) 空输入
		{"0+空→unknown", 0, LlmAPIError{}, LlmCategoryUnknown},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyLlmError(c.status, c.api); got != c.want {
				t.Fatalf("ClassifyLlmError(%d, %+v) = %q, 期望 %q", c.status, c.api, got, c.want)
			}
		})
	}
}

// ==================== parseLlmErrorBody ====================

func TestParseLlmErrorBody(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want LlmAPIError
	}{
		{"OpenAI 包裹体", `{"error":{"message":"boom","type":"invalid_request_error","param":"model","code":"bad_model"}}`,
			LlmAPIError{Message: "boom", Type: "invalid_request_error", Param: "model", Code: "bad_model"}},
		{"扁平体", `{"message":"boom","code":"bad_model"}`,
			LlmAPIError{Message: "boom", Code: "bad_model"}},
		{"error 为 null 时回退扁平体", `{"error":null,"code":"bad_model"}`,
			LlmAPIError{Code: "bad_model"}},
		{"error 为空对象时回退扁平体", `{"error":{},"message":"boom"}`,
			LlmAPIError{Message: "boom"}},
		{"非法 JSON 返回零值", `not-json`, LlmAPIError{}},
		{"空串返回零值", ``, LlmAPIError{}},
		{"纯文本返回零值(由上层回退为 Message)", `upstream exploded`, LlmAPIError{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseLlmErrorBody(c.raw); got != c.want {
				t.Fatalf("parseLlmErrorBody(%q) = %+v, 期望 %+v", c.raw, got, c.want)
			}
		})
	}
}

// ==================== HasError ====================

func TestHasError(t *testing.T) {
	cases := []struct {
		name    string
		payload json.RawMessage
		want    bool
	}{
		{"nil", nil, false},
		{"空", json.RawMessage(""), false},
		{"null", json.RawMessage("null"), false},
		{"带空格的 null", json.RawMessage("  null  "), false},
		{"空对象", json.RawMessage(`{}`), true},
		{"有内容", json.RawMessage(`{"message":"boom"}`), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HasError(c.payload); got != c.want {
				t.Fatalf("HasError(%q) = %v, 期望 %v", string(c.payload), got, c.want)
			}
		})
	}
}

// ==================== StreamErrorPayload ====================

func TestStreamErrorPayload(t *testing.T) {
	withErrorField := ChatCompletionChunk{Error: json.RawMessage(`{"message":"boom"}`)}
	nullErrorField := ChatCompletionChunk{Error: json.RawMessage("null")}
	emptyChunk := ChatCompletionChunk{}

	cases := []struct {
		name      string
		eventName string
		chunk     ChatCompletionChunk
		data      string
		want      string
		wantOK    bool
	}{
		{"error 字段优先(事件名任意)", "message", withErrorField, `{"message":"x"}`, `{"message":"boom"}`, true},
		{"event: error 时取整块数据", "error", emptyChunk, `{"type":"overloaded_error"}`, `{"type":"overloaded_error"}`, true},
		{"事件名大小写不敏感", "ERROR", emptyChunk, `boom`, `boom`, true},
		{"error 字段为 null 且事件名非 error→无错误", "message", nullErrorField, `{"choices":[]}`, "", false},
		{"无错误字段且事件名为 message→无错误", "message", emptyChunk, `{"choices":[]}`, "", false},
		{"空 chunk + 空 data 但事件名为 error→仍视为错误载荷(分类交给上层)", "error", emptyChunk, "", "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := StreamErrorPayload(c.eventName, c.chunk, c.data)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, 期望 %v", ok, c.wantOK)
			}
			if string(got) != c.want {
				t.Fatalf("payload = %q, 期望 %q", string(got), c.want)
			}
		})
	}
}

// ==================== IsRetryableLlmError ====================

func TestIsRetryableLlmError(t *testing.T) {
	stream := func(status int, body string) error {
		return ParseLlmResponseError(newResponse(status, body), 4096)
	}

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil 不可重试", nil, false},
		{"客户端取消不可重试", ErrRequestCanceled, false},
		{"context.Canceled 不可重试", context.Canceled, false},
		{"context.DeadlineExceeded 不可重试", context.DeadlineExceeded, false},
		{"传输层(StatusCode=0)可重试", NewLlmStreamTransportError(errors.New("connection reset"), ""), true},
		{"同步传输层可重试", NewLlmResponseTransportError(errors.New("dial tcp: refused")), true},
		{"429 可重试", stream(http.StatusTooManyRequests, `{"error":{"message":"rate limit"}}`), true},
		{"503 可重试", stream(http.StatusServiceUnavailable, `{"error":{"message":"overloaded"}}`), true},
		{"500 可重试", stream(http.StatusInternalServerError, ``), true},
		{"401 不可重试(配置类)", stream(http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`), false},
		{"404 模型不存在不可重试", stream(http.StatusNotFound, `{"error":{"message":"model not found"}}`), false},
		{"欠费不可重试", NewLlmResponseError(json.RawMessage(`{"error":{"code":"insufficient_quota"}}`)), false},
		{"内容安全不可重试", NewLlmStreamError(json.RawMessage(`content_filter`), ""), false},
		{"限流文案的流中错误可重试", NewLlmStreamError(json.RawMessage(`Requests rate limit exceeded`), ""), true},
		{"普通错误不可重试", errors.New("boom"), false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsRetryableLlmError(c.err); got != c.want {
				t.Fatalf("IsRetryableLlmError(%v) = %v, 期望 %v", c.err, got, c.want)
			}
		})
	}
}

// ==================== IsQuotaError / GetRequestIDFromLlmResponse ====================

func TestIsQuotaError(t *testing.T) {
	if IsQuotaError(nil) {
		t.Fatalf("nil 不应判为额度错误")
	}
	for _, s := range []string{"insufficient_quota", "账户余额不足", "Throttling.AllocationQuota"} {
		if !IsQuotaError(errors.New(s)) {
			t.Fatalf("%q 应判为额度错误", s)
		}
	}
	if IsQuotaError(errors.New("connection reset by peer")) {
		t.Fatalf("普通网络错误不应判为额度错误")
	}
}

func TestGetRequestIDFromLlmResponse(t *testing.T) {
	if got := GetRequestIDFromLlmResponse(nil); got != "" {
		t.Fatalf("nil header 应返回空串,得到 %q", got)
	}
	h := http.Header{}
	h.Set("X-Trace-Id", "trace-1")
	if got := GetRequestIDFromLlmResponse(h); got != "trace-1" {
		t.Fatalf("应回退到 X-Trace-Id,得到 %q", got)
	}
	h.Set("X-Request-Id", "req-1")
	if got := GetRequestIDFromLlmResponse(h); got != "req-1" {
		t.Fatalf("X-Request-Id 优先级更高,得到 %q", got)
	}
	h.Set("X-Request-Id", "  ")
	if got := GetRequestIDFromLlmResponse(h); got != "trace-1" {
		t.Fatalf("空白值应跳过并回退,得到 %q", got)
	}
}

// ==================== ParseLlmResponseError / LlmStatusError ====================

func TestParseLlmResponseError(t *testing.T) {
	resp := newResponse(http.StatusTooManyRequests, `{"error":{"message":"rate limit exceeded","type":"rate_limit_error"}}`)
	resp.Header.Set("X-Request-Id", "req-42")

	err := ParseLlmResponseError(resp, 4096)
	var statusErr *LlmStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("应返回 *LlmStatusError,得到 %T", err)
	}
	if statusErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("StatusCode = %d, 期望 %d", statusErr.StatusCode, http.StatusTooManyRequests)
	}
	if statusErr.RequestID != "req-42" {
		t.Fatalf("RequestID = %q, 期望 req-42", statusErr.RequestID)
	}
	if statusErr.Category != LlmCategoryRateLimit {
		t.Fatalf("Category = %q, 期望 %q", statusErr.Category, LlmCategoryRateLimit)
	}
	if statusErr.Stage != "response" {
		t.Fatalf("Stage = %q, 期望 response", statusErr.Stage)
	}
	text := err.Error()
	for _, frag := range []string{"runtime client:", "http 429", "rate_limit", "rate limit exceeded", "request_id=req-42"} {
		if !strings.Contains(text, frag) {
			t.Fatalf("Error() 文本缺少 %q,实际: %s", frag, text)
		}
	}
}

func TestLlmStatusErrorIsMatchesStageAndCategory(t *testing.T) {
	err := NewLlmStreamError(json.RawMessage(`Requests rate limit exceeded`), "req-1")

	if !errors.Is(err, &LlmStatusError{Stage: "stream", Category: LlmCategoryRateLimit}) {
		t.Fatalf("同阶段+同类别应匹配")
	}
	if errors.Is(err, &LlmStatusError{Stage: "stream", Category: LlmCategoryQuota}) {
		t.Fatalf("类别不同不应匹配")
	}
	if errors.Is(err, &LlmStatusError{Stage: "response", Category: LlmCategoryRateLimit}) {
		t.Fatalf("阶段不同不应匹配")
	}
	if errors.Is(err, errors.New("unrelated")) {
		t.Fatalf("非 LlmStatusError 目标不应匹配")
	}
}

// newResponse 构造带 body 的 http.Response(供 ParseLlmResponseError 读取)。
func newResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
