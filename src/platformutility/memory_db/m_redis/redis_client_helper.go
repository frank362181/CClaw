package m_redis

import (
	"context"

	"my.favorite/platformutility/common"
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/logging"
	redisdb "my.favorite/platformutility/memory_db/common"
)

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
		logging.LogInfo("%s", err.Error())
		return nil
	}

	// 启动后台的健康检查
	go client.HealthCheck()

	return client
}
