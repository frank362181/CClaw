/**
* 上游 usage(前缀缓存命中)协议解析测试。
*
* 锁住三件事:
*  1) 两口径(DeepSeek / OpenAI)都能归一;
*  2) "未上报"必须与"0 命中"区分(HasCacheReport);
*  3) JSON 字段名必须与供应商实际下发一致(踩过 prompt_tokens_details 的坑)。
**/

package llmproto

import (
	"encoding/json"
	"testing"
)

func TestCachedPromptTokens(t *testing.T) {
	if got := (*ChatCompletionUsage)(nil).CachedPromptTokens(); got != 0 {
		t.Fatalf("nil 应返回 0, got %d", got)
	}
	if got := (&ChatCompletionUsage{PromptCacheHitTokens: 30}).CachedPromptTokens(); got != 30 {
		t.Fatalf("DeepSeek 口径解析错误: %d", got)
	}
	u := &ChatCompletionUsage{
		PromptCacheHitTokens: 10,
		PromptTokensDetails:  &PromptTokensDetail{CachedTokens: 42},
	}
	if got := u.CachedPromptTokens(); got != 42 {
		t.Fatalf("两口径应取较大值: %d", got)
	}
}

// TestHasCacheReport 区分"上游上报了缓存字段",这条是防止把"不支持上报"误算成"0% 命中"的唯一手段。
func TestHasCacheReport(t *testing.T) {
	if (*ChatCompletionUsage)(nil).HasCacheReport() {
		t.Fatal("nil 不应视为已上报")
	}

	// 关键用例:只有 prompt/completion/total(供应商未上报任何缓存字段)→ 未上报
	plain := &ChatCompletionUsage{PromptTokens: 1000, CompletionTokens: 10, TotalTokens: 1010}
	if plain.HasCacheReport() {
		t.Fatal("没有缓存字段的 usage 不应视为已上报(否则会把'不支持'误读为 0% 命中)")
	}

	if !(&ChatCompletionUsage{PromptCacheHitTokens: 5}).HasCacheReport() {
		t.Fatal("DeepSeek hit>0 应视为已上报")
	}
	if !(&ChatCompletionUsage{PromptCacheMissTokens: 5}).HasCacheReport() {
		t.Fatal("DeepSeek miss>0 应视为已上报")
	}
	if !(&ChatCompletionUsage{PromptTokensDetails: &PromptTokensDetail{CachedTokens: 0}}).HasCacheReport() {
		t.Fatal("OpenAI 出现明细结构即视为已上报(即使恒为 0)")
	}
}

// TestUsageUnmarshalDeepSeek 校验 DeepSeek 顶层字段名。
func TestUsageUnmarshalDeepSeek(t *testing.T) {
	raw := `{"prompt_tokens":1000,"completion_tokens":10,"total_tokens":1010,
	         "prompt_cache_hit_tokens":800,"prompt_cache_miss_tokens":200}`
	var u ChatCompletionUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if u.PromptCacheHitTokens != 800 || u.PromptCacheMissTokens != 200 {
		t.Fatalf("DeepSeek 字段解析错误: %+v", u)
	}
	if u.CachedPromptTokens() != 800 || !u.HasCacheReport() {
		t.Fatalf("归一/上报判定错误: cached=%d report=%v", u.CachedPromptTokens(), u.HasCacheReport())
	}
}

// TestUsageUnmarshalOpenAI 校验 OpenAI 口径字段名:**必须是 prompt_tokens_details(复数)**。
func TestUsageUnmarshalOpenAI(t *testing.T) {
	raw := `{"prompt_tokens":1000,"completion_tokens":10,"total_tokens":1010,
	         "prompt_tokens_details":{"cached_tokens":640}}`
	var u ChatCompletionUsage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if u.PromptTokensDetails == nil {
		t.Fatalf("prompt_tokens_details 未被解析(检查 json tag 拼写): %s", raw)
	}
	if got := u.CachedPromptTokens(); got != 640 {
		t.Fatalf("OpenAI 口径命中 token 解析错误: %d", got)
	}
}
