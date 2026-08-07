package m_redis

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/exception"
	"my.favorite/platformutility/logging"

	"my.favorite/platformutility/common"
	redisdb "my.favorite/platformutility/memory_db/common"
)

type RedisClient struct {
	redisdb.MemoryDbClient

	mu     sync.RWMutex
	Ctx    context.Context
	Cancel context.CancelFunc

	closeOnce sync.Once //保证Close仅仅执行一次
	isClosed  bool      //标记是否已经关闭

	mClient *redis.Client
}

var memoryDbClient = common.GetSingleton(func() *RedisClient {
	return &RedisClient{
		mClient: nil,
	}
}).Instance()

func GetRedisClient(config *config.ConfigurationHelper) redisdb.IMemoryDB {
	var memoryConfig redisdb.MemoryDbConfig
	if config == nil {
		memoryConfig = *redisdb.GetDefaultMemoryDbConfig()
	} else {
		memoryConfig.Address, _ = config.GetStringValueDefault("memoryDb", "address", "127.0.0.1")
		memoryConfig.Port, _ = config.GetIntValueDefault("memoryDb", "port", 6379)
		memoryConfig.DatabaseName, _ = config.GetIntValueDefault("memoryDb", "databaseName", 0)
		memoryConfig.Username, _ = config.GetStringValueDefault("memoryDb", "username", "")
		memoryConfig.Password, _ = config.GetStringValueDefault("memoryDb", "password", "")

		memoryConfig.PoolSize, _ = config.GetIntValueDefault("memoryDb", "poolSize", 20)
		memoryConfig.MinIdleConnections, _ = config.GetIntValueDefault("memoryDb", "minIdleConnections", 5)
		memoryConfig.MaxRetries, _ = config.GetIntValueDefault("memoryDb", "maxRetries", 3)

		memoryConfig.DialTimeout, _ = config.GetIntValueDefault("memoryDb", "dialTimeout", 5)
		memoryConfig.ReadTimeout, _ = config.GetIntValueDefault("memoryDb", "readTimeout", 3)
		memoryConfig.WriteTimeout, _ = config.GetIntValueDefault("memoryDb", "writeTimeout", 3)

		memoryConfig.HealthCheckInterval, _ = config.GetIntValueDefault("memoryDb", "healthCheckInterval", 10)

		memoryConfig.ReconnectBaseDelay, _ = config.GetIntValueDefault("memoryDb", "reconnectBaseDelay", 500)
	}

	memoryDbClient.MConfig = memoryConfig //设置配置信息
	ctx, cancel := context.WithCancel(context.Background())
	memoryDbClient.Ctx = ctx
	memoryDbClient.Cancel = cancel

	client := memoryDbClient

	// 链接数据库
	if err := client.connectMemoryDb(); err != nil {
		cancel()
		logging.LogInfof("%s", err.Error())
		return nil
	}

	// 启动后台的健康检查
	go client.HealthCheck()

	return client
}

func (rdb *RedisClient) HealthCheck() {
	ticker := time.NewTicker(time.Duration(rdb.MConfig.HealthCheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-rdb.Ctx.Done():
			logging.LogInfof("[Redis] health check done")
			return
		case <-ticker.C:
			rdb.mu.Lock()
			client := rdb.mClient
			isClosed := rdb.IsClosed()
			rdb.mu.Unlock()

			if isClosed || client == nil {
				logging.LogInfof("[Redis] health check skipped: client is closed!")
				continue
			}

			ctx, cancel := context.WithTimeout(rdb.Ctx, 3*time.Second)
			err := client.Ping(ctx).Err()
			cancel()

			if err != nil {
				logging.LogInfof("[Redis] health check ping failed:%s,starting reconnect...", err.Error())
				if reconErr := rdb.reconnect(); reconErr != nil {
					logging.LogInfof("[redis] reconnect failed:%s:%s", rdb.MConfig.Address, reconErr.Error())
				}
			}
		}
	}
}

func (rdb *RedisClient) PingMemoryDb() error {
	rdb.mu.RLock()
	client := rdb.mClient
	isClosed := rdb.isClosed
	rdb.mu.RUnlock()

	if isClosed || client == nil {
		return exception.MemoryDbErrEx("redis client is closed")
	}

	ctx, cancel := context.WithTimeout(rdb.Ctx, time.Duration(rdb.MConfig.DialTimeout)*time.Second)
	defer cancel()
	return client.Ping(ctx).Err()
}

func (rdb *RedisClient) IsClosed() bool {
	return rdb.mClient == nil || rdb.isClosed
}

// Set /*
func (rdb *RedisClient) Set(key string, value interface{}, expiration time.Duration) error {
	rdb.mu.RLock()
	client := rdb.mClient
	isClosed := rdb.IsClosed()
	rdb.mu.RUnlock()

	if isClosed || client == nil {
		return exception.MemoryDbErrEx("redis client is closed")
	}

	return rdb.executeWithRetry(func(client *redis.Client) error {
		ctx, cancel := context.WithTimeout(rdb.Ctx, time.Duration(rdb.MConfig.WriteTimeout)*time.Second)
		defer cancel()

		return client.Set(ctx, key, value, expiration).Err()
	})
}

func (rdb *RedisClient) Get(key string) (string, error) {
	rdb.mu.RLock()
	client := rdb.mClient
	isClosed := rdb.IsClosed()
	rdb.mu.RUnlock()

	if isClosed || client == nil {
		return "", exception.MemoryDbErrEx("redis client is closed")
	}

	var result string
	err := rdb.executeWithRetry(func(client *redis.Client) error {
		ctx, cancel := context.WithTimeout(rdb.Ctx, time.Duration(rdb.MConfig.ReadTimeout)*time.Second)
		defer cancel()
		val, err := client.Get(ctx, key).Result()
		if err != nil {
			return err
		}

		result = val
		return nil
	})

	return result, err
}

func (rdb *RedisClient) GetIntWithDefault(key string, defValue int) (int, error) {
	str, err := rdb.Get(key)
	if err != nil {
		return defValue, err
	}

	var result int
	if result, err = strconv.Atoi(str); err == nil {
		return result, nil
	}

	return defValue, err
}

func (rdb *RedisClient) Delete(keys ...string) (int64, error) {
	rdb.mu.RLock()
	client := rdb.mClient
	isClosed := rdb.IsClosed()
	rdb.mu.RUnlock()

	if isClosed || client == nil {
		return -1, exception.MemoryDbErrEx("redis client is closed")
	}

	var deleted int64
	err := rdb.executeWithRetry(func(client *redis.Client) error {
		ctx, cancel := context.WithTimeout(rdb.Ctx, time.Duration(rdb.MConfig.WriteTimeout)*time.Second)
		defer cancel()
		val, err := client.Del(ctx, keys...).Result()
		if err != nil {
			return err
		}

		deleted = val
		return nil
	})

	return deleted, err
}

func (rdb *RedisClient) CloseMemoryDb() error {
	var closeErr error
	rdb.closeOnce.Do(func() {
		//先取消context
		rdb.Cancel()

		rdb.mu.Lock()
		defer rdb.mu.Unlock()

		rdb.isClosed = true
		if rdb.mClient != nil {
			closeErr = rdb.mClient.Close()
			rdb.mClient = nil
		}

		logging.LogInfof("[Redis] close success")
	})

	return closeErr
}

// ============================= private functions & methods ====================================
func (rdb *RedisClient) connectMemoryDb() error {
	if !rdb.IsClosed() {
		return nil
	}

	rClient, err := rdb.connecting(rdb.onConnected)
	if err != nil {
		return exception.MemoryDbErrEx2("failed to connect redis db:%s,ERROR=%s", rdb.MConfig.Address, err.Error())
	}

	// 替换客户端，原子性
	rdb.mu.Lock()
	if rdb.mClient != nil {
		_ = rdb.mClient.Close()
	}
	rdb.mClient = rClient
	rdb.isClosed = false
	rdb.mu.Unlock()

	logging.LogInfof("Redis connect success:%s", rdb.MConfig.Address)
	return nil
}

func (rdb *RedisClient) reconnect() error {
	rdb.mu.Lock()
	defer rdb.mu.Unlock()

	if rdb.IsClosed() {
		return exception.MemoryDbErrEx("redis client is closed")
	}

	// 关闭旧链接
	if rdb.mClient != nil {
		_ = rdb.mClient.Close()
		rdb.mClient = nil
	}

	var lastErr error
	for i := 0; i < rdb.MConfig.MaxRetries; i++ {
		// 指数退避算法
		exponent := int(math.Pow(2, float64(i))) // 转换为Int
		wait := time.Duration(exponent*rdb.MConfig.ReconnectBaseDelay) * time.Millisecond
		if wait > 30*time.Second {
			wait = 30 * time.Second
		}

		logging.LogInfof("[Redis] reconnecting in %v (attempt %d:%d)", wait, i+1, rdb.MConfig.MaxRetries)

		select {
		case <-rdb.Ctx.Done():
			return rdb.Ctx.Err()
		case <-time.After(wait):
		}

		//尝试新建链接
		rClient, err := rdb.connecting(nil)
		if err != nil {
			lastErr = err
			continue
		}

		rdb.mClient = rClient
		rdb.isClosed = false
		return nil
	}

	return exception.MemoryDbErrEx2("reconnect failed after %d attempts,Error=%s", rdb.MConfig.MaxRetries, lastErr.Error())
}

func (rdb *RedisClient) connecting(fn func(ctx context.Context, cn *redis.Conn) error) (*redis.Client, error) {
	opts := &redis.Options{
		Addr:         fmt.Sprintf("%s:%d", rdb.MConfig.Address, rdb.MConfig.Port),
		Password:     rdb.MConfig.Password,
		DB:           rdb.MConfig.DatabaseName,
		PoolSize:     rdb.MConfig.PoolSize,
		MinIdleConns: rdb.MConfig.MinIdleConnections,
		MaxRetries:   rdb.MConfig.MaxRetries,
		DialTimeout:  time.Duration(rdb.MConfig.DialTimeout) * time.Second,
		ReadTimeout:  time.Duration(rdb.MConfig.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(rdb.MConfig.WriteTimeout) * time.Second,
		OnConnect:    fn,
	}

	rClient := redis.NewClient(opts)

	// 验证链接
	ctx, cancel := context.WithTimeout(rdb.Ctx, 5*time.Second)
	defer cancel()
	if err := rClient.Ping(ctx).Err(); err != nil {
		_ = rdb.mClient.Close()
		return nil, err
	}

	return rClient, nil
}

func (rdb *RedisClient) executeWithRetry(operation func(client *redis.Client) error) error {
	err := operation(rdb.mClient)
	if err == nil {
		return nil
	}

	// 判断是否需要重连(链接拒绝、EOF、连接池耗尽等)
	if isNetworkError(err) {
		logging.LogInfof("[Redis] network error detected:%s,%s,trying reconnect.", rdb.MConfig.Address, err.Error())

		if reconErr := rdb.reconnect(); reconErr != nil {
			logging.LogInfof("[redis] reconnect failed:%s:%s", rdb.MConfig.Address, reconErr.Error())
			return reconErr
		}

		//重试操作
		rdb.mu.RLock()
		rClient := rdb.mClient
		isClosed := rdb.IsClosed()
		rdb.mu.RUnlock()

		if rClient == nil || isClosed {
			return exception.MemoryDbErrEx("redis client not available after  reconnect")
		}

		return operation(rClient)
	}

	return err
}

func (rdb *RedisClient) onConnected(ctx context.Context, cn *redis.Conn) error {
	logging.LogInfof("[Redis] connection established:%s", rdb.MConfig.Address)
	return nil
}
