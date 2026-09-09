/**
* Agent 并发许可闸(每 Agent 并发上限)
*
* 目标:限制同一 Agent 同一时刻"进行中生成"的数量(≤ max_concurrent),
*      避免热门 Agent 打满 SQLite/Redis/上游 LLM 额度并饿死其它 Agent。
*
* 语义:
*   - 上限来自 t_agent.max_concurrent(未配置时由服务层换算为默认值),可动态调整:
*     空闲(无运行者/无等待者)时新上限立即生效;使用中沿用旧上限直至清空;
*   - 许可"移交"式排队(FIFO):释放方直接把许可交给队首等待者,不额外计数;
*   - 支持 ctx 取消排队(请求断开/超时),取消时若已取得许可则归还,避免许可泄漏;
*   - 进程内实现(single-instance 部署,与会话绑定/流注册一致);接口可平滑换 Redis 分布式实现。
**/

package core

import (
	"context"
	"sync"

	"my.favorite/platformutility/common"
)

// AgentGateRegistry 按 AgentId 管理并发许可(线程安全)。
type AgentGateRegistry struct {
	mu      sync.Mutex
	entries map[string]*agentGateEntry
}

var agentGateRegistry = common.GetSingleton(func() *AgentGateRegistry {
	return &AgentGateRegistry{entries: make(map[string]*agentGateEntry)}
}).Instance()

// GetAgentGateRegistry 返回全局唯一的 Agent 并发闸。
func GetAgentGateRegistry() *AgentGateRegistry {
	return agentGateRegistry
}

// Acquire 获取 agentId 的一个并发许可,等待 ctx 超时/取消时返回错误。
// 成功返回 release(恰调用一次);limit<=0 时退化为 1(服务层通常已换算为正数默认)。
func (r *AgentGateRegistry) Acquire(ctx context.Context, agentId string, limit int) (func(), error) {
	if limit < 1 {
		limit = 1
	}

	r.mu.Lock()
	entry := r.entries[agentId]
	if entry == nil {
		entry = &agentGateEntry{limit: limit, free: limit}
		r.entries[agentId] = entry
	}
	r.mu.Unlock()

	return entry.acquire(ctx, limit)
}

// Destroy 清空全部许可(进程关闭)。
func (r *AgentGateRegistry) Destroy() {
	r.mu.Lock()
	r.entries = make(map[string]*agentGateEntry)
	r.mu.Unlock()
}

// agentGateEntry 单个 Agent 的并发许可队列。
type agentGateEntry struct {
	mu      sync.Mutex
	limit   int             // 当前上限
	free    int             // 空闲许可数(limit - 进行中)
	waiters []chan struct{} // 等待者队列(FIFO),关闭 ch 即表示获得许可
}

// acquire 获取一个许可(带 ctx 取消)。
func (e *agentGateEntry) acquire(ctx context.Context, limit int) (func(), error) {
	e.mu.Lock()
	// 空闲且上限变化 → 直接应用新上限,避免旧上限长时间残留
	if e.limit != limit && len(e.waiters) == 0 && e.free == e.limit {
		e.limit = limit
		e.free = limit
	}
	if e.free > 0 {
		e.free--
		e.mu.Unlock()
		return e.release, nil
	}

	ch := make(chan struct{})
	e.waiters = append(e.waiters, ch)
	e.mu.Unlock()

	select {
	case <-ctx.Done():
		e.mu.Lock()
		// 仍排队 → 取消排队(未取得许可,free 不变)
		for i, w := range e.waiters {
			if w == ch {
				e.waiters = append(e.waiters[:i], e.waiters[i+1:]...)
				e.mu.Unlock()
				return nil, ctx.Err()
			}
		}
		// 已不在队列(释放方刚移交许可)→ 归还许可后以取消收场,避免许可泄漏
		e.mu.Unlock()
		e.release()
		return nil, ctx.Err()
	case <-ch:
		// 释放方已把许可移交给本等待者(free 不变),直接返回 release
		return e.release, nil
	}
}

// release 归还一个许可:优先移交给队首等待者,否则 free++。
func (e *agentGateEntry) release() {
	e.mu.Lock()
	if len(e.waiters) > 0 {
		w := e.waiters[0]
		e.waiters = e.waiters[1:]
		e.mu.Unlock()
		close(w)
		return
	}
	e.free++
	e.mu.Unlock()
}
