/*
* 设计目的：
*	可观测性的入口类
 */

package observatory

import (
	"sort"
	"sync"

	"my.favorite/platformutility/logging"
)

type promptCacheModel struct {
	Calls          int
	Prompt         int
	Cached         int
	PromptReported int
	Completion     int
	NoReport       int
	SavedMoney     float64 //估算节省:cached/le6*(InputCost - CachedReadCost)
}

var promptCache = struct {
	mu sync.Mutex

	calls    int
	models   map[string]*promptCacheModel //按模型统计token用量
	logEvery int
}{
	models:   make(map[string]*promptCacheModel),
	logEvery: 50, //统计的频次,节拍
}

// ObservePromptCache 记录一次采用,并按节拍输出
func ObservePromptCache(s TokenUsageEvent) {
	if s.Prompt <= 0 {
		return // 无输入用量(异常响应):不参与统计,避免无人命中率
	}

	promptCache.mu.Lock()

	promptCache.calls++ //节拍计数器

	m := promptCache.models[s.Model]
	if m == nil {
		m = &promptCacheModel{}
		promptCache.models[s.Model] = m
	}
	m.Calls++
	m.Prompt += s.Prompt
	m.Cached += s.Cached

	if !s.HasCacheReport {
		m.NoReport++
	} else {
		m.PromptReported += s.Prompt //只有上报的调用才进分母
		if s.Cached > 0 {
			m.SavedMoney += float64(s.Cached) / 1e6 * float64(s.InputCost-s.CachedReadCost)
		}
	}

	calls := promptCache.calls
	logEvery := promptCache.logEvery

	promptCache.mu.Unlock()

	if logEvery > 0 && calls%logEvery == 0 {
		LogPromptCacheSummary()
	}
}

// SnapShot 返回当前统计数据的快照
func SnapShot() PromptCacheSummary {
	promptCache.mu.Lock()
	defer promptCache.mu.Unlock()

	out := PromptCacheSummary{}
	reported := 0
	for _, m := range promptCache.models {
		out.Calls += m.Calls
		out.Prompt += m.Prompt
		out.Cached += m.Cached
		out.SavedMoney += m.SavedMoney
		out.NoReport += m.NoReport

		reported += m.PromptReported
	}

	if reported > 0 {
		out.HitRatePercent = float64(out.Cached) / float64(reported) * 100
	}

	return out
}

// LogPromptCacheSummary 输出一行info汇总(按模型细分,便于判断 有些模型不支持缓存)
func LogPromptCacheSummary() {
	s := SnapShot()
	logging.LogInfoF("llm prompt cache: calls=%d prompt_tokens=%d cached_tokens=%d hit_rate=%.1f%% saved≈$%.4f no_report=%d",
		s.Calls, s.Prompt, s.Cached, s.HitRatePercent, s.SavedMoney, s.NoReport)

	// for debug the cache report
	promptCache.mu.Lock()
	defer promptCache.mu.Unlock()

	tempModels := make([]string, 0, len(promptCache.models))
	for model := range promptCache.models {
		tempModels = append(tempModels, model)
	}
	sort.Strings(tempModels)

	for _, k := range tempModels {
		m := promptCache.models[k]
		rate := 0.0
		if m.Prompt > 0 {
			rate = float64(m.Cached) / float64(m.Prompt) * 100
		}

		logging.LogDebugF("llm prompt cache: model=%s calls=%d prompt=%d cached=%d hit_rate=%.1f%% saved≈$%.4f no_report=%d",
			k, m.Calls, m.Prompt, m.Cached, rate, m.SavedMoney, m.NoReport)
	}
}
