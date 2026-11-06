package common

import (
	"context"
	"time"
)

type IMemoryDB interface {
	HealthCheck(ctx context.Context)
	PingMemoryDb() error

	IsClosed() bool

	Set(key string, value interface{}, expiration time.Duration) error
	Get(key string) (string, error)
	GetIntWithDefault(key string, defValue int) (int, error)

	// SetNX 原子写入：仅当 key 不存在时设置（等价 Redis `SET key val NX EX`）。
	// 返回 true 表示本次写入成功（key 此前不存在）；false 表示 key 已存在。
	// 用于 NONCE 防重放等「检查并写入」场景：把 GET+SET 两次往返合并为一次，且天然原子。
	SetNX(key string, value interface{}, expiration time.Duration) (bool, error)

	// Increment 专门针对int类型的自增操作
	Increment(key string, inc int64) (int64, error)

	// IncrementWithExpiration 带有过期时间的增加/减少操作
	IncrementWithExpiration(key string, inc int64, expiration time.Duration) (int64, error)

	Delete(keys ...string) (int64, error)

	// RunScript 原子地执行一段 Lua 脚本（Redis EVALSHA/EVAL，go-redis 自动缓存与回退）。
	// 用于需要跨进程原子性的复合操作（如会话双向索引的 Create/Replace/Refresh/Revoke）。
	// keys/args 对应脚本的 KEYS/ARGV。返回脚本返回值（类型由脚本决定）。
	RunScript(script string, keys []string, args ...interface{}) (interface{}, error)

	CloseMemoryDb() error
}
