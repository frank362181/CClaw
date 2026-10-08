/*
* 设计目的:
*	专门用于面向上游供应商调用时的Token使用量的观察者
 */

package observatory

import (
	"sync"

	"my.favorite/cclawservice/harness/api/llmproto"
	"my.favorite/cclawservice/model/dataobject"
)

// 注册token观察者(nil为关闭),Agent启动时注册
// 全局钩子函数,多调用点,为横切关注点
var (
	tokenObserverLock sync.Mutex
	tokenObserver     func(event TokenUsageEvent)
)

func RegisterTokenObserver(fn func(TokenUsageEvent)) {
	tokenObserverLock.Lock()
	defer tokenObserverLock.Unlock()

	tokenObserver = fn //观察者函数
}

// ReportTokenUsage 尽在 **成功** 返回时调用(失败/重试 的尝试不计数,否则会出现重复统计)
func ReportTokenUsage(provider dataobject.TLlm, stream bool, u *llmproto.ChatCompletionUsage) {
	if u == nil {
		return
	}

	tokenObserverLock.Lock()
	fn := tokenObserver
	tokenObserverLock.Unlock()

	if fn == nil {
		return
	}

	// 调用观察者函数
	fn(TokenUsageEvent{
		ProviderId: provider.ProviderId, Model: provider.GetModelName(), Stream: stream,
		Prompt: u.PromptTokens, Cached: u.CachedPromptTokens(),
		HasCacheReport: u.HasCacheReport(),
		InputCost:      provider.InputCost, CachedReadCost: provider.CachedReadCost,
	})
}
