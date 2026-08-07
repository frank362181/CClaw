package common

import (
	"time"
)

type IMemoryDB interface {
	HealthCheck()
	PingMemoryDb() error

	IsClosed() bool

	Set(key string, value interface{}, expiration time.Duration) error
	Get(key string) (string, error)
	GetIntWithDefault(key string, defValue int) (int, error)
	Delete(keys ...string) (int64, error)

	// RunScript 原子地执行一段 Lua 脚本（Redis EVALSHA/EVAL，go-redis 自动缓存与回退）。
	// 用于需要跨进程原子性的复合操作（如会话双向索引的 Create/Replace/Refresh/Revoke）。
	// keys/args 对应脚本的 KEYS/ARGV。返回脚本返回值（类型由脚本决定）。
	RunScript(script string, keys []string, args ...interface{}) (interface{}, error)

	CloseMemoryDb() error
}
