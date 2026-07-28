package common

import (
	"context"
	"time"
)

type IMemoryDB interface {
	HealthCheck()
	PingMemoryDb(ctx context.Context) error

	IsClosed() bool

	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, keys ...string) (int64, error)

	CloseMemoryDb() error
}
