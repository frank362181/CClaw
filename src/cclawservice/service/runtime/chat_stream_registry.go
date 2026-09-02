/**
* 流式聊天注册表
*
* 职责:管理"进行中的流式聊天"会话,支持按会话键中断(取消上下文),供聊天中断 API 使用。
*
*   - key 形如 "<sender>:<sessionId>",以发送者(服务端认证)为前缀,
*     保证不同用户、不同会话的流式聊天相互隔离,互不影响。
*   - 注册:ChatStream 在调用 LLM 前注册取消函数;流式结束(成功/出错/取消)后自动清理。
*   - 中断:中断 API 按 key 查找并调用取消函数,使 LLM 流式读取停止。
**/

package runtime

import (
	"context"
	"sync"

	"my.favorite/platformutility/common"
)

// ChatStreamRegistry 管理进行中的流式聊天,支持按会话键中断(取消上下文)。
// 线程安全:注册/取消/清理在多 goroutine(各请求)并发下安全。
type ChatStreamRegistry struct {
	mu     sync.RWMutex
	active map[string]context.CancelFunc
}

var chatStreamRegistry = common.GetSingleton(func() *ChatStreamRegistry {
	return &ChatStreamRegistry{active: make(map[string]context.CancelFunc)}
}).Instance()

// GetChatStreamRegistry 返回全局唯一的流式聊天注册表。
func GetChatStreamRegistry() *ChatStreamRegistry {
	return chatStreamRegistry
}

// Register 注册一个进行中的流式聊天的取消函数;同键已存在时覆盖(旧取消函数自然失效)。
func (r *ChatStreamRegistry) Register(key string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[key] = cancel
}

// Cancel 取消指定会话的流式聊天。
//   - 找到并已取消:返回 true;
//   - 找不到(聊天未开始或已结束):返回 false,调用方据此返回"未在聊天"错误。
//
// 取消函数幂等(可重复调用),删除与调用顺序不敏感。
func (r *ChatStreamRegistry) Cancel(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.active[key]
	if !ok {
		return false
	}
	delete(r.active, key)
	cancel()
	return true
}

// Unregister 移除指定会话的注册(流式结束/出错/取消后清理,防内存泄漏)。
// 幂等:不存在的键静默忽略。
func (r *ChatStreamRegistry) Unregister(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, key)
}
