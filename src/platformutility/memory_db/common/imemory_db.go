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
	Delete(keys ...string) (int64, error)

	CloseMemoryDb() error
}
