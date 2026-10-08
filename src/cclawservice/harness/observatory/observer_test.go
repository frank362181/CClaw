/**
* 上游前缀缓存观测(聚合/节拍/未上报隔离)测试。
*
* 这些用例把"可观测性"的口径固定下来:
*  1) 聚合正确(调用数/prompt/命中/省钱);
*  2) 未上报缓存字段的调用要能被识别,且不参与命中率分母;
*  3) 节拍计数器必须自增,否则汇总会每次都输出。
**/

package observatory

import (
	"math"
	"testing"
)

// resetPromptCache 清理全局聚合状态,保证用例之间互不影响。
func resetPromptCache(t *testing.T) {
	t.Helper()
	promptCache.mu.Lock()
	defer promptCache.mu.Unlock()
	promptCache.calls = 0
	promptCache.models = make(map[string]*promptCacheModel)
	promptCache.logEvery = 0 //测试默认关闭汇总输出
}

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s 期望 %.9f,实际 %.9f", name, want, got)
	}
}

func TestObservePromptCacheAggregates(t *testing.T) {
	resetPromptCache(t)

	ObservePromptCache(TokenUsageEvent{ProviderId: "p", Model: "m1", Prompt: 1000, Cached: 800,
		HasCacheReport: true, InputCost: 1, CachedReadCost: 0})
	ObservePromptCache(TokenUsageEvent{ProviderId: "p", Model: "m1", Prompt: 2000, Cached: 500,
		HasCacheReport: true, InputCost: 1, CachedReadCost: 0})

	s := SnapShot()
	if s.Calls != 2 || s.Prompt != 3000 || s.Cached != 1300 {
		t.Fatalf("聚合结果错误: %+v", s)
	}
	approx(t, "hit_rate", s.HitRatePercent, 1300.0/3000.0*100)
	approx(t, "saved_money", s.SavedMoney, 0.0008+0.0005)
	if s.NoReport != 0 {
		t.Fatalf("已上报却计入 no_report: %+v", s)
	}
}

// TestNoReportExcludedFromHitRate 未上报缓存字段的模型不能污染命中率分母。
func TestNoReportExcludedFromHitRate(t *testing.T) {
	resetPromptCache(t)

	// 支持上报且命中
	ObservePromptCache(TokenUsageEvent{Model: "deepseek", Prompt: 1000, Cached: 900, HasCacheReport: true})
	// 不支持上报(供应商不返回任何缓存字段)
	ObservePromptCache(TokenUsageEvent{Model: "qwen", Prompt: 1000, Cached: 0, HasCacheReport: false})

	s := SnapShot()
	if s.NoReport != 1 {
		t.Fatalf("no_report 应为 1,实际 %d", s.NoReport)
	}
	approx(t, "hit_rate", s.HitRatePercent, 90.0)
}

// TestZeroPromptIgnored 0 输入用量的异常样本不应进入统计(否则拉低命中率)。
func TestZeroPromptIgnored(t *testing.T) {
	resetPromptCache(t)

	ObservePromptCache(TokenUsageEvent{Model: "m1", Prompt: 0, Cached: 0, HasCacheReport: true})

	s := SnapShot()
	if s.Calls != 0 || s.Prompt != 0 {
		t.Fatalf("prompt=0 的样本应被丢弃,实际 %+v", s)
	}
}

// TestThrottleCounterIncrements 节拍计数器必须自增,否则 logEvery 形同虚设、每调用一次就输出一次汇总。
func TestThrottleCounterIncrements(t *testing.T) {
	resetPromptCache(t)
	promptCache.logEvery = 2

	for i := 0; i < 4; i++ {
		ObservePromptCache(TokenUsageEvent{Model: "m1", Prompt: 100, Cached: 50, HasCacheReport: true})
	}

	promptCache.mu.Lock()
	calls := promptCache.calls
	promptCache.mu.Unlock()
	if calls != 4 {
		t.Fatalf("节拍计数器未自增:期望 4,实际 %d(会导致每次都输出汇总)", calls)
	}
}
