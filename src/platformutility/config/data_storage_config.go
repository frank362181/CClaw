/**
* 数据存储所需的配置信息,来源于INI/JSON等配置文件
* 包含redis等内存数据库,关系型数据库等
**/

package config

import cCommon "my.favorite/cclawservice/common"

type DataStorageConfig struct {
	DBConfig struct {
		DBType       int
		DBHost       string //数据所在服务器名IP或域名
		DBPort       int
		DataDir      string // just valid for sqlite
		DatabaseName string
		UserName     string
		UserPass     string

		SSLMode        bool
		MaxConnections int
		MinConnections int

		ConnectionTimeout int //最大链接超时时间

		ConnectionIdle int //最大空闲时间, 15 min
		ConnectionTTl  int //最大生存时间 30 min

		MaxRetries     int
		RetryBaseDelay float64
		RetryMaxDelay  float64
		RetryJitter    float64

		ReloadCacheTTL int // 重新从数据库加载数据的超时时间,second

		LogLevel string //数据库的日志级别,silent/error/warn/info,默认是warn级别
	}

	MemoryDBConfig struct {
		MemoryType int //内存数据库类型，如redis,memo-cache mongodb

		Address string //内存数据库地址，如redis的地址
		Port    int

		DatabaseName int //数据库的名称
		Username     string
		Password     string

		PoolSize           int //连接池最大连接数
		MinIdleConnections int //最小空闲链接数量
		MaxRetries         int //最大的链接尝试次数

		DialTimeout  int // 链接超时
		ReadTimeout  int
		WriteTimeout int

		// 健康检查与重连参数
		HealthCheckInterval int
		ReconnectBaseDelay  int //重连等待的基础时间（backoff算法,ms）
	}
}

func LoadDataStorageConfig(helper *ConfigurationHelper) (DataStorageConfig, error) {
	var config DataStorageConfig
	if helper == nil {
		return config, nil
	}

	// 数据库配置信息
	config.DBConfig.DBType, _ = helper.GetIntValueDefault("database", "dbType", ConstDbTypeUnknown)
	config.DBConfig.DBHost, _ = helper.GetStringValueDefault("database", "server", "")
	config.DBConfig.DBPort, _ = helper.GetIntValueDefault("database", "port", 5432)
	config.DBConfig.DatabaseName, _ = helper.GetStringValueDefault("database", "dbname", "")
	config.DBConfig.DataDir, _ = helper.GetStringValueDefault("database", "dataDir", "")
	config.DBConfig.SSLMode, _ = helper.GetBoolValueDefault("database", "sslMode", false)
	config.DBConfig.UserName, _ = helper.GetStringValueDefault("database", "username", "")
	config.DBConfig.UserPass, _ = helper.GetStringValueDefault("database", "password", "")
	config.DBConfig.MinConnections, _ = helper.GetIntValueDefault("database", "min_connections", 5)
	config.DBConfig.MaxConnections, _ = helper.GetIntValueDefault("database", "max_connections", 10)
	config.DBConfig.ConnectionTimeout, _ = helper.GetIntValueDefault("database", "connection_timeout", 30)
	config.DBConfig.ConnectionIdle, _ = helper.GetIntValueDefault("database", "connection_idle", 900)
	config.DBConfig.ConnectionTTl, _ = helper.GetIntValueDefault("database", "connection_ttl", 1800)
	config.DBConfig.MaxRetries, _ = helper.GetIntValueDefault("database", "max_retries", 5)
	config.DBConfig.RetryBaseDelay, _ = helper.GetFloat64ValueDefault("database", "retry_base_delay", 1.0)
	config.DBConfig.RetryMaxDelay, _ = helper.GetFloat64ValueDefault("database", "retry_max_delay", 60.0)
	config.DBConfig.RetryJitter, _ = helper.GetFloat64ValueDefault("database", "retry_jitter", 0.1)
	config.DBConfig.ReloadCacheTTL, _ = helper.GetIntValueDefault("database", "reloadCacheTTL", cCommon.DefaultReloadDatabaseCacheTTLSeconds)

	config.DBConfig.LogLevel, _ = helper.GetStringValueDefault("database", "log_level", "warn")

	// load redis configuration
	config.MemoryDBConfig.MemoryType, _ = helper.GetIntValueDefault("memoryDb", "memoryDbType", MemoryDbTypeRedis)
	config.MemoryDBConfig.Address, _ = helper.GetStringValueDefault("memoryDb", "address", DefaultMemoryDbAddress)
	config.MemoryDBConfig.Port, _ = helper.GetIntValueDefault("memoryDb", "port", DefaultMemoryDbPort)
	config.MemoryDBConfig.DatabaseName, _ = helper.GetIntValueDefault("memoryDb", "databaseName", DefaultMemoryDbName)
	config.MemoryDBConfig.Username, _ = helper.GetStringValueDefault("memoryDb", "username", "")
	config.MemoryDBConfig.Password, _ = helper.GetStringValueDefault("memoryDb", "password", "")

	config.MemoryDBConfig.PoolSize, _ = helper.GetIntValueDefault("memoryDb", "poolSize", DefaultMaxPoolSize)
	config.MemoryDBConfig.MinIdleConnections, _ = helper.GetIntValueDefault("memoryDb", "minIdleConnections", DefaultMinIdleConnection)
	config.MemoryDBConfig.MaxRetries, _ = helper.GetIntValueDefault("memoryDb", "maxRetries", DefaultMaxRetries)

	config.MemoryDBConfig.DialTimeout, _ = helper.GetIntValueDefault("memoryDb", "dialTimeout", DefaultDialTimeout)
	config.MemoryDBConfig.ReadTimeout, _ = helper.GetIntValueDefault("memoryDb", "readTimeout", DefaultReadTimeout)
	config.MemoryDBConfig.WriteTimeout, _ = helper.GetIntValueDefault("memoryDb", "writeTimeout", DefaultWriteTimeout)

	config.MemoryDBConfig.HealthCheckInterval, _ = helper.GetIntValueDefault("memoryDb", "healthCheckInterval", HealthCheckInterval)

	config.MemoryDBConfig.ReconnectBaseDelay, _ = helper.GetIntValueDefault("memoryDb", "reconnectBaseDelay", ReconnectionBaseDelay)

	return config, nil
}
