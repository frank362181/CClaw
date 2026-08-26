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

// 单例模式
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
		memoryConfig.Address, _ = config.GetStringValueDefault("memoryDb", "address", redisdb.DefaultMemoryDbAddress)
		memoryConfig.Port, _ = config.GetIntValueDefault("memoryDb", "port", redisdb.DefaultMemoryDbPort)
		memoryConfig.DatabaseName, _ = config.GetIntValueDefault("memoryDb", "databaseName", redisdb.DefaultMemoryDbName)
		memoryConfig.Username, _ = config.GetStringValueDefault("memoryDb", "username", "")
		memoryConfig.Password, _ = config.GetStringValueDefault("memoryDb", "password", "")

		memoryConfig.PoolSize, _ = config.GetIntValueDefault("memoryDb", "poolSize", redisdb.DefaultMaxPoolSize)
		memoryConfig.MinIdleConnections, _ = config.GetIntValueDefault("memoryDb", "minIdleConnections", redisdb.DefaultMinIdleConnection)
		memoryConfig.MaxRetries, _ = config.GetIntValueDefault("memoryDb", "maxRetries", redisdb.DefaultMaxRetries)

		memoryConfig.DialTimeout, _ = config.GetIntValueDefault("memoryDb", "dialTimeout", redisdb.DefaultDialTimeout)
		memoryConfig.ReadTimeout, _ = config.GetIntValueDefault("memoryDb", "readTimeout", redisdb.DefaultReadTimeout)
		memoryConfig.WriteTimeout, _ = config.GetIntValueDefault("memoryDb", "writeTimeout", redisdb.DefaultWriteTimeout)

		memoryConfig.HealthCheckInterval, _ = config.GetIntValueDefault("memoryDb", "healthCheckInterval", redisdb.HealthCheckInterval)

		memoryConfig.ReconnectBaseDelay, _ = config.GetIntValueDefault("memoryDb", "reconnectBaseDelay", redisdb.ReconnectionBaseDelay)
	}

	memoryDbClient.MConfig = memoryConfig //设置配置信息
	ctx, cancel := context.WithCancel(context.Background())
	memoryDbClient.Ctx = ctx
	memoryDbClient.Cancel = cancel

	client := memoryDbClient

	// 链接数据库
	if err := client.connectMemoryDb(); err != nil {
		cancel()
		logging.LogInfoF("%s", err.Error())
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
			logging.LogInfoF("[Redis] health check done")
			return
		case <-ticker.C:
			rdb.mu.Lock()
			client := rdb.mClient
			isClosed := rdb.IsClosed()
			rdb.mu.Unlock()

			if isClosed || client == nil {
				logging.LogInfoF("[Redis] health check skipped: client is closed!")
				continue
			}

			ctx, cancel := context.WithTimeout(rdb.Ctx, 3*time.Second)
			err := client.Ping(ctx).Err()
			cancel()

			if err != nil {
				logging.LogInfoF("[Redis] health check ping failed:%s,starting reconnect...", err.Error())
				if reconErr := rdb.reconnect(); reconErr != nil {
					logging.LogInfoF("[redis] reconnect failed:%s:%s", rdb.MConfig.Address, reconErr.Error())
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

// SetNX 原子写入：仅当 key 不存在时设置成功（Redis `SET NX EX`），等价「检查并写入」。
// 返回 true=写入成功（key 此前不存在）；false=key 已存在。
func (rdb *RedisClient) SetNX(key string, value interface{}, expiration time.Duration) (bool, error) {
	rdb.mu.RLock()
	client := rdb.mClient
	isClosed := rdb.IsClosed()
	rdb.mu.RUnlock()

	if isClosed || client == nil {
		return false, exception.MemoryDbErrEx("redis client is closed")
	}

	var ok bool
	err := rdb.executeWithRetry(func(client *redis.Client) error {
		ctx, cancel := context.WithTimeout(rdb.Ctx, time.Duration(rdb.MConfig.WriteTimeout)*time.Second)
		defer cancel()

		b, err := client.SetNX(ctx, key, value, expiration).Result()
		if err != nil {
			return err
		}
		ok = b
		return nil
	})

	return ok, err
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

func (rdb *RedisClient) Increment(key string, inc int64) (int64, error) {
	rdb.mu.RLock()
	client := rdb.mClient
	isClosed := rdb.IsClosed()
	rdb.mu.RUnlock()

	if isClosed || client == nil {
		return -1, exception.MemoryDbErrEx("redis client is closed")
	}

	var val int64
	err := rdb.executeWithRetry(func(client *redis.Client) error {
		ctx, cancel := context.WithTimeout(rdb.Ctx, time.Duration(rdb.MConfig.WriteTimeout)*time.Second)
		defer cancel()

		result, err := client.IncrBy(ctx, key, inc).Result()
		if err != nil {
			return err
		}

		val = result
		return nil
	})

	return val, err
}

func (rdb *RedisClient) IncrementWithExpiration(key string, inc int64, expiration time.Duration) (int64, error) {
	rdb.mu.RLock()
	client := rdb.mClient
	isClosed := rdb.IsClosed()
	rdb.mu.RUnlock()

	if isClosed || client == nil {
		return -1, exception.MemoryDbErrEx("redis client is closed")
	}

	var val int64 //返回的值
	err := rdb.executeWithRetry(func(client *redis.Client) error {
		ctx, cancel := context.WithTimeout(rdb.Ctx, time.Duration(rdb.MConfig.WriteTimeout)*time.Second)
		defer cancel()

		v, err := redis.NewScript(LuaIncrementString).Run(ctx, client, []string{key}, inc, int64(expiration/time.Second)).Int64()
		if err != nil {
			return err
		}
		val = v
		return nil
	})

	return val, err
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

// RunScript 原子执行 Lua 脚本；go-redis 的 NewScript 会自动缓存 SHA，
// 遇 NOSCRIPT 自动回退 EVAL，兼顾带宽与健壮性。
func (rdb *RedisClient) RunScript(script string, keys []string, args ...interface{}) (interface{}, error) {
	rdb.mu.RLock()
	client := rdb.mClient
	isClosed := rdb.IsClosed()
	rdb.mu.RUnlock()

	if isClosed || client == nil {
		return nil, exception.MemoryDbErrEx("redis client is closed")
	}

	var result interface{}
	err := rdb.executeWithRetry(func(client *redis.Client) error {
		ctx, cancel := context.WithTimeout(rdb.Ctx, time.Duration(rdb.MConfig.WriteTimeout)*time.Second)
		defer cancel()

		val, err := redis.NewScript(script).Run(ctx, client, keys, args...).Result()
		if err != nil {
			return err
		}
		result = val
		return nil
	})

	return result, err
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

		logging.LogInfoF("[Redis] close success")
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

	logging.LogInfoF("Redis connect success:%s", rdb.MConfig.Address)
	return nil
}

// reconnect 后台断线重建(由健康检查 goroutine 调用),带指数退避。
// 注意:不在请求路径调用;退避与拨号期间不持有写锁、不清空 mClient,
// 旧连接继续服务,避免重连窗口期误伤在线请求。
func (rdb *RedisClient) reconnect() error {
	var lastErr error
	for i := 0; i < rdb.MConfig.MaxRetries; i++ {
		// 指数退避算法
		exponent := int(math.Pow(2, float64(i)))
		wait := time.Duration(exponent*rdb.MConfig.ReconnectBaseDelay) * time.Millisecond
		if wait > 30*time.Second {
			wait = 30 * time.Second
		}

		logging.LogInfoF("[Redis] reconnecting in %v (attempt %d:%d)", wait, i+1, rdb.MConfig.MaxRetries)

		select {
		case <-rdb.Ctx.Done():
			return rdb.Ctx.Err()
		case <-time.After(wait):
		}

		// 先拨号并验证,成功后再原子替换旧连接
		rClient, err := rdb.connecting(nil)
		if err != nil {
			lastErr = err
			continue
		}

		rdb.mu.Lock()
		if rdb.IsClosed() {
			rdb.mu.Unlock()
			_ = rClient.Close()
			return exception.MemoryDbErrEx("redis client is closed")
		}
		old := rdb.mClient
		rdb.mClient = rClient
		rdb.isClosed = false
		rdb.mu.Unlock()
		if old != nil {
			_ = old.Close()
		}
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
		// 关闭新建的失败客户端(勿关旧 mClient:可能在重连中为 nil 或仍被使用)
		_ = rClient.Close()
		return nil, err
	}

	return rClient, nil
}

func (rdb *RedisClient) executeWithRetry(operation func(client *redis.Client) error) error {
	err := operation(rdb.mClient)
	if err == nil {
		return nil
	}

	// 非网络类错误(业务错误等)直接返回,不重试
	if !isNetworkError(err) {
		return err
	}

	// A1:网络错误时,请求路径只做一次"快速重连"(无指数退避),失败立即返回,
	// 避免请求被后台式重连的退避阻塞数秒;真正的断线重建由健康检查 goroutine 负责。
	logging.LogInfoF("[Redis] network error detected:%s,%s,trying fast reconnect.", rdb.MConfig.Address, err.Error())
	if reconErr := rdb.fastReconnect(); reconErr != nil {
		logging.LogInfoF("[redis] fast reconnect failed:%s:%s", rdb.MConfig.Address, reconErr.Error())
		return reconErr
	}

	rdb.mu.RLock()
	rClient := rdb.mClient
	isClosed := rdb.IsClosed()
	rdb.mu.RUnlock()
	if rClient == nil || isClosed {
		return exception.MemoryDbErrEx("redis client not available after fast reconnect")
	}

	// 在新连接上重试一次;仍失败则快速返回
	return operation(rClient)
}

// fastReconnect 请求路径使用的轻量重连:立即新建并验证连接(无指数退避),失败立即返回。
// 与后台 reconnect() 的区别:不做退避;连接替换原子化,不会出现
// "重连窗口期 mClient 为 nil 导致在线请求被误伤"的问题。
func (rdb *RedisClient) fastReconnect() error {
	rClient, err := rdb.connecting(nil)
	if err != nil {
		return exception.MemoryDbErrEx2("fast reconnect failed:%s", err.Error())
	}

	rdb.mu.Lock()
	defer rdb.mu.Unlock()
	if rdb.IsClosed() {
		_ = rClient.Close()
		return exception.MemoryDbErrEx("redis client is closed")
	}

	old := rdb.mClient
	rdb.mClient = rClient
	rdb.isClosed = false
	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (rdb *RedisClient) onConnected(ctx context.Context, cn *redis.Conn) error {
	logging.LogInfoF("[Redis] connection established:%s", rdb.MConfig.Address)
	return nil
}
