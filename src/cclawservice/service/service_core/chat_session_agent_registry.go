/**
* 会话-Agent 绑定注册表(内存实现)
*
* 职责:记录"会话(sessionId)已被路由到哪个 Agent",保证同一会话内后续消息沿用同一 Agent,
* 从而保持上下文一致,并避免每次消息都重复执行内容路由(关键词/LLM 分类)。
*
* 设计说明:
*   - key 形如 "<comCode>:<sessionId>",以单位编码为前缀,隔离不同单位;
*   - 条目带 TTL(秒),过期后按惰性方式在读取/写入时清理;
*   - 有容量上限,超出时先清理过期项,仍超出则整体清空(防内存膨胀的安全阀);
*   - 目前为内存实现:进程重启后绑定丢失、多实例部署不共享,
*     后续可平滑替换为 Redis/DB 实现,对外接口保持不变。
**/

package service_core

import (
	"sync"

	myCommon "my.favorite/cclawservice/common"
	myUtility "my.favorite/cclawservice/utility"
	"my.favorite/platformutility/common"
)

// sessionAgentBinding 一条会话绑定记录。
type sessionAgentBinding struct {
	agentId string
	boundAt int64 // 绑定时刻(Unix 秒)
}

// ChatSessionAgentRegistry 会话→Agent 的绑定注册表(线程安全)。
type ChatSessionAgentRegistry struct {
	mu         sync.RWMutex
	bindings   map[string]sessionAgentBinding
	ttlSeconds int64 // 条目有效期;<=0 表示永不过期
	maxEntries int   // 容量上限;<=0 表示不限制
}

// defaultSessionBindingMaxEntries 会话绑定的容量上限,防止内存无限膨胀。
const defaultSessionBindingMaxEntries = 100000

var chatSessionAgentRegistry = common.GetSingleton(func() *ChatSessionAgentRegistry {
	return &ChatSessionAgentRegistry{
		bindings:   make(map[string]sessionAgentBinding),
		ttlSeconds: myCommon.SessionAgentBindingTTLSeconds,
		maxEntries: defaultSessionBindingMaxEntries,
	}
}).Instance()

// GetChatSessionAgentRegistry 返回全局唯一的会话-Agent 绑定注册表。
func GetChatSessionAgentRegistry() *ChatSessionAgentRegistry {
	return chatSessionAgentRegistry
}

// Get 查询指定会话绑定的 Agent;不存在或已过期返回 ok=false(过期条目惰性删除)。
func (r *ChatSessionAgentRegistry) Get(comCode, sessionId string) (string, bool) {
	if sessionId == "" {
		return "", false
	}
	key := myUtility.BuildSessionKey(comCode, sessionId)

	r.mu.RLock()
	b, ok := r.bindings[key]
	r.mu.RUnlock()
	if !ok {
		return "", false
	}

	// 惰性过期:命中后判断 TTL,过期则删除并视为未绑定
	nowTimestamp := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp
	if r.ttlSeconds > 0 && nowTimestamp-b.boundAt > r.ttlSeconds {
		r.mu.Lock()
		delete(r.bindings, key)
		r.mu.Unlock()
		return "", false
	}
	return b.agentId, true
}

// Set 记录"会话→Agent"绑定;超出容量时先清理过期项,仍不足则整体清空(防内存膨胀)。
func (r *ChatSessionAgentRegistry) Set(comCode, sessionId, agentId string) {
	if sessionId == "" || agentId == "" {
		return
	}
	key := myUtility.BuildSessionKey(comCode, sessionId)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.maxEntries > 0 && len(r.bindings) >= r.maxEntries {
		r.evictExpiredLocked()
		if len(r.bindings) >= r.maxEntries {
			// 过期清理仍无法腾出空间:整体清空,保证容量有界
			r.bindings = make(map[string]sessionAgentBinding)
		}
	}
	r.bindings[key] = sessionAgentBinding{agentId: agentId, boundAt: common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp}
}

// ClearKey 移除指定会话的绑定(调用方需持有写锁的场景:绑定目标 Agent 已失效时)。
func (r *ChatSessionAgentRegistry) ClearKey(comCode, sessionId string) {
	if sessionId == "" {
		return
	}
	key := myUtility.BuildSessionKey(comCode, sessionId)
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bindings, key)
}

// Clear 清空全部绑定(测试/重启清理用)。
func (r *ChatSessionAgentRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bindings = make(map[string]sessionAgentBinding)
}

// ======================= private function & methods =========================

// evictExpiredLocked 清理全部过期条目(调用方需持有写锁)。
func (r *ChatSessionAgentRegistry) evictExpiredLocked() {
	if r.ttlSeconds <= 0 {
		return
	}
	now := common.GetDatetimeUtilityInstance().GetCurrentDatetime().Timestamp //time.Now().Unix()
	for k, b := range r.bindings {
		if now-b.boundAt > r.ttlSeconds {
			delete(r.bindings, k)
		}
	}
}
