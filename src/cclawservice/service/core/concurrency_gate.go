/**
* 并发计数闸(每实例在飞请求/连接上限,非阻塞)
*
* 目标:限制同一时刻“进行中”的数量,防止无上限占用 goroutine/上游流,制造背压。
* 现两个实例:
*   - 流式闸(GetStreamConcurrencyGate): 同时在飞 SSE 流式连接 ≤ DefaultMaxConcurrentStreams;
*   - 全局聊天闸(GetChatConcurrencyGate): 同时在飞聊天请求(同步+流式) ≤ DefaultMaxChatConcurrency。
*
* 语义:
*   - TryAcquire 非阻塞:达到上限立即失败(调用方在写 SSE 头前回 503 JSON);
*   - Release 与一次成功 TryAcquire 配对(defer 保证含 panic/错误路径);
*   - SetMax 运行期可调(<=0 回退默认);
*   - 进程内实现(single-instance),与会话绑定/流注册一致。
**/

package core

import (
	"sync"

	cCommon "my.favorite/cclawservice/common"
	"my.favorite/platformutility/common"
)

// ConcurrencyGate 计数式并发闸(线程安全)。
type ConcurrencyGate struct {
	mu     sync.Mutex
	max    int
	active int
}

var (
	streamConcurrencyGate = common.GetSingleton(func() *ConcurrencyGate {
		return &ConcurrencyGate{max: cCommon.DefaultMaxStreamConcurrency}
	}).Instance()
	chatConcurrencyGate = common.GetSingleton(func() *ConcurrencyGate {
		return &ConcurrencyGate{max: cCommon.DefaultMaxChatConcurrency}
	}).Instance()
)

// GetStreamConcurrencyGate 返回全局唯一的 SSE 流并发闸,并按 max 更新上限。
func GetStreamConcurrencyGate(max int) *ConcurrencyGate {
	streamConcurrencyGate.setMax(max)
	return streamConcurrencyGate
}

// GetChatConcurrencyGate 返回全局唯一的聊天并发闸,并按 max 更新上限。
func GetChatConcurrencyGate(max int) *ConcurrencyGate {
	chatConcurrencyGate.setMax(max)
	return chatConcurrencyGate
}

// TryAcquire 尝试占用一个名额;达到上限返回 false(不排队)。
func (g *ConcurrencyGate) TryAcquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active >= g.max {
		return false
	}
	g.active++
	return true
}

// Release 释放一个名额(与一次成功 TryAcquire 配对)。
func (g *ConcurrencyGate) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active > 0 {
		g.active--
	}
}

// Active 当前在飞数(观测用)。
func (g *ConcurrencyGate) Active() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active
}

// ============================== private functions ===============================

// SetMax 更新上限;<=0 时回退默认值(流式闸/聊天闸各用各自默认)。已占用数超过新上限时,
// 仅阻止后续新增,直至释放回落到上限内。
func (g *ConcurrencyGate) setMax(max int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if max > 0 {
		g.max = max
	}
}
