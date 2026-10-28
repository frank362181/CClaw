package common

const (
	MemoryDbTypeUnknown = iota
	MemoryDbTypeRedis
	MemoryDbTypeMongoDb
	MemoryDbTypeMemcache
)

const (
	DefaultMemoryDbAddress = "127.0.0.1"
	DefaultMemoryDbPort    = 6379

	DefaultMemoryDbName = 0

	DefaultMaxPoolSize       = 20
	DefaultMinIdleConnection = 5
	DefaultMaxRetries        = 3

	DefaultDialTimeout  = 5
	DefaultReadTimeout  = 3
	DefaultWriteTimeout = 3

	HealthCheckInterval   = 10
	ReconnectionBaseDelay = 500
)
