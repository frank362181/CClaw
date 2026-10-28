/*
* 设计目的：
*	可观测性的入口类
 */

package observatory

import (
	"context"
	"time"

	"my.favorite/cclawservice/common/llmproto"
	"my.favorite/cclawservice/model/dataobject"
	pCommon "my.favorite/platformutility/common"
)

// =========================codes block for token usage ====================

func RegisterTokenObserver() {
	setupTokenObserver()
}

// TokenSnapShot 返回当前统计数据的快照
func TokenSnapShot() PromptCacheSummary {
	return tokenSnapShot()
}

// LogPromptCacheSummary 输出一行info汇总(按模型细分,便于判断 有些模型不支持缓存)
func LogPromptCacheSummary() {
	logPromptCacheSummary()
}

func OnTokenUsage(provider dataobject.TLlm, stream bool, u *llmproto.ChatCompletionUsage) {
	reportTokenUsage(provider, stream, u)
}

// ======================== block are for calling timing =================

func RegisterTimingObserver() {
	setupTimingObserver()
}

// WithTurn 把回合归因挂到 ctx(返回新 ctx,不修改入参),供底层埋点回填 TurnScope 与 StartOffsetMs
func WithTurn(ctx context.Context, scope string, start time.Time) context.Context {
	return withTurn(ctx, scope, start.UnixMilli())
}

// OnLlmTiming 流式下登记llm 调用时间
func OnLlmTiming(ctx context.Context, llm dataobject.TLlm, stream bool, lastTokenUsage *llmproto.ChatCompletionUsage, callStart time.Time, ttftMs int64, err error) {
	var (
		timing = LlmCallTiming{ProviderId: llm.ProviderId, Model: llm.GetModelName(), Stream: stream}
	)

	if callStart.IsZero() {
		return // 请求没发出去:不产生样本,避免污染
	}

	timing.StartOffsetMs = callStartOffset(ctx, callStart.UnixMilli())
	timing.TtftMs = ttftMs
	timing.TotalMs = time.Since(callStart).Milliseconds()
	if lastTokenUsage != nil {
		timing.PromptTokens = lastTokenUsage.PromptTokens
		timing.CachedTokens = lastTokenUsage.CachedPromptTokens()
	}

	if err != nil {
		timing.Err = err.Error()
	}

	reportLlmTiming(ctx, timing)
}

func OnTurnTiming(ctx context.Context, turnTiming TurnTiming, turnStart time.Time, firstVisibleMs int64, reply string, err error) {
	turnTiming.TotalMs = pCommon.MilliDiff(turnStart)
	turnTiming.FirstVisibleMs = firstVisibleMs
	turnTiming.ReplyRunes = len([]rune(reply))

	if err != nil {
		turnTiming.Err = err.Error()
	}

	reportTurnTiming(ctx, turnTiming)
}
