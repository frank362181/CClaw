/*
* 设计目的:
*	专门用于"时间维度"的可观测性:单次调用上游(TTFT)和整回合分阶段耗时
*	设计 **setupTimingObserver**函数的目的是为了是否支持可观测性进行设置
 */

package observatory

import (
	"context"
	"sync"

	"my.favorite/platformutility/logging"
)

// 用私有空结构体做key:类型即身份,避免与其他包的ctx value的键冲突
type turnInfoKey struct{}

// TurnInfo 挂在ctx上的回合归因信息
// 只承载 **只读**数据:埋点不得通过它回写状态(并发下必然出错)
type TurnInfo struct {
	Scope   string // 归因标识,即turnId
	StartMs int64  // 回合开始timestamp
}

func withTurn(ctx context.Context, scope string, startMs int64) context.Context {
	return context.WithValue(ctx, turnInfoKey{}, &TurnInfo{Scope: scope, StartMs: startMs})
}

func turnFrom(ctx context.Context) (*TurnInfo, bool) {
	if ctx == nil {
		return nil, false
	}

	info, ok := ctx.Value(turnInfoKey{}).(*TurnInfo)
	return info, ok
}

func callStartOffset(ctx context.Context, at int64) int64 {
	if info, ok := turnFrom(ctx); ok && info.StartMs > 0 {
		return at - info.StartMs
	}

	return 0
}

// =============================== observer ==================================

// 时间维度阈值:只"超阈值才告警",不做每轮刷屏 —— 可观测性要能被看见,不是越多越好。
const (
	TimingWarnTtftMs  = 3000  // 单次上游首包 ≥3s
	TimingWarnTotalMs = 60000 // 单回合 ≥60s
	TimingWarnGapMs   = 2000  // 分阶段之和与总时长差值 ≥2s(说明还有没埋到的地方)
)

var (
	timingObserverLock sync.Mutex

	llmTimingObserver  func(timing LlmCallTiming)
	turnTimingObserver func(turn TurnTiming)
)

func reportLlmTiming(ctx context.Context, timing LlmCallTiming) {
	timingObserverLock.Lock()
	fn := llmTimingObserver
	timingObserverLock.Unlock()

	if fn == nil {
		return
	}

	if timing.TurnScope == "" {
		if info, ok := turnFrom(ctx); ok {
			timing.TurnScope = info.Scope
		}
	}

	fn(timing)
}

func reportTurnTiming(ctx context.Context, turn TurnTiming) {
	timingObserverLock.Lock()
	fn := turnTimingObserver
	timingObserverLock.Unlock()

	if fn == nil {
		return
	}

	fn(turn)
}

func setupTimingObserver() {
	timingObserverLock.Lock()
	defer timingObserverLock.Unlock()

	llmTimingObserver = observerLlmTiming
	turnTimingObserver = observerTurnTiming
}

// getTurnTimeDiff 返回 一回合中分阶段与总时长的差值
func getTurnTimeDiff(t TurnTiming) int64 {
	covered := t.GateMs + t.RouteMs + t.SkillMs + t.PromptMs + t.LlmMs + t.ToolsMs
	return t.TotalMs - covered
}

func observerLlmTiming(t LlmCallTiming) {
	stream := "sync"
	if t.Stream {
		stream = "stream"
	}

	if t.Err != "" {
		logging.LogWarnF("llm timing: scope=%s provider=%s stream=%s offset=%dms ttft=%dms total=%dms prompt=%d cached=%d err=%s",
			t.TurnScope, t.ProviderId, stream, t.StartOffsetMs, t.TtftMs, t.TotalMs, t.PromptTokens, t.CachedTokens, t.Err)
		return
	}

	logging.LogDebugF("llm timing: scope=%s provider=%s stream=%s offset=%dms ttft=%dms total=%dms prompt=%d cached=%d",
		t.TurnScope, t.ProviderId, stream, t.StartOffsetMs, t.TtftMs, t.TotalMs, t.PromptTokens, t.CachedTokens)

	switch {
	case t.Stream && t.TtftMs >= TimingWarnTtftMs:
		logging.LogWarnF("llm timing slow first byte: scope=%s provider=%s stream=%s ttft=%dms total=%dms prompt=%d",
			t.TurnScope, t.ProviderId, stream, t.TtftMs, t.TotalMs, t.PromptTokens)
	case !t.Stream && t.TotalMs >= TimingWarnTotalMs:
		logging.LogWarnF("llm timing slow sync call: scope=%s provider=%s total=%dms prompt=%d",
			t.TurnScope, t.ProviderId, t.TotalMs, t.PromptTokens)
	}
}

func observerTurnTiming(t TurnTiming) {
	logging.LogInfoF("turn timing: turn=%s agent=%s total=%dms gate=%dms route=%dms skill=%dms prompt=%dms llm=%dms(%d轮) tool=%dms(%d次) first_visible=%dms reply=%drune err=%s",
		t.TurnScope, t.AgentId, t.TotalMs, t.GateMs, t.RouteMs, t.SkillMs, t.PromptMs,
		t.LlmMs, t.Rounds, t.ToolsMs, t.ToolCalls, t.FirstVisibleMs, t.ReplyRunes, t.Err)

	if t.TotalMs >= TimingWarnTotalMs {
		logging.LogWarnF("turn timing slow turn: turn=%s total=%dms route=%dms llm=%dms(%d轮) tool=%dms first_visible=%dms",
			t.TurnScope, t.TotalMs, t.RouteMs, t.LlmMs, t.Rounds, t.ToolsMs, t.FirstVisibleMs)
	}

	// 自检:埋点是否覆盖完整。缺口大 = 还有阶段没打点,别在这种状态下做优化决策。
	if gap := getTurnTimeDiff(t); gap >= TimingWarnGapMs {
		logging.LogWarnF("turn timing unaccounted: turn=%s total=%dms covered=%dms gap=%dms",
			t.TurnScope, t.TotalMs, t.TotalMs-gap, gap)
	}
}
