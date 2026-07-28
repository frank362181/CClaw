/**
* 定义所有的内存数据库所需的类型定义
**/

package common

type MemoryDbConfig struct {
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

// MemoryDbClient 内存数据库链接的客户端
type MemoryDbClient struct {
	MConfig MemoryDbConfig
}

func GetDefaultMemoryDbConfig() *MemoryDbConfig {
	return &MemoryDbConfig{
		Address:      "127.0.0.1",
		Port:         6379,
		DatabaseName: 0,
		Username:     "",
		Password:     "",

		PoolSize:           DefaultMaxPoolSize,
		MinIdleConnections: DefaultMinIdleConnection,
		MaxRetries:         DefaultMaxRetries,

		DialTimeout:  DefaultDialTimeout,
		ReadTimeout:  DefaultReadTimeout,
		WriteTimeout: DefaultWriteTimeout,

		HealthCheckInterval: HealthCheckInterval,
		ReconnectBaseDelay:  ReconnectionBaseDelay,
	}
}
