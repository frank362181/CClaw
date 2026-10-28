/**
*
* 职责:按"会话键"串行化临界区,供上下文模块和记忆层模块使用。
*
* 为什么需要:同一会话的两条消息可能并发到达(用户连点、多标签页、重试),
* 而会话内 seq(轮次序号)与历史文件 index(记录序号)都是"读最大值 + 1"的分配逻辑,
* 不加锁会分配重复值,进而写坏 t_turn.seq 与历史文件的记录序号。
*
* 实现要点:
*   - 按 key 惰性创建互斥锁,key 的粒度即会话标识(与并发会话数同阶);
*   - 引用计数:释放后最后一个持有者负责从表中删除,避免 map 随会话数无限膨胀;
*   - 全局锁只用于保护 entries 表本身,不在持锁期间做 IO。
**/

package ctxmgr

import (
	"sync"

	pCommon "my.favorite/platformutility/common"
)

// sessionLockEntry 一条会话锁(引用计数用于回收)。
type sessionLockEntry struct {
	mu   sync.Mutex
	refs int
}

// StorageLocks 会话锁管理器(线程安全)。
type StorageLocks struct {
	mu      sync.Mutex
	entries map[string]*sessionLockEntry
}

var storageLocks = pCommon.GetSingleton(func() *StorageLocks {
	return &StorageLocks{entries: make(map[string]*sessionLockEntry)}
}).Instance()

// GetStorageLocks 返回全局唯一的会话锁管理器。
func GetStorageLocks() *StorageLocks {
	return storageLocks
}

// Lock 获取指定键的独占锁,返回释放函数。调用方必须释放,惯用法:defer unlock()。
// key 为空时退化为全局锁(等价于串行化所有会话),仅作为异常入参的兜底。
func (l *StorageLocks) Lock(key string) func() {
	l.mu.Lock()
	entry, ok := l.entries[key]
	if !ok {
		entry = &sessionLockEntry{}
		l.entries[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()

	return func() {
		entry.mu.Unlock()

		l.mu.Lock()
		entry.refs--
		if entry.refs <= 0 {
			delete(l.entries, key)
		}
		l.mu.Unlock()
	}
}
