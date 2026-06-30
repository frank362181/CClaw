package database

import "my.favorite/platformutility/config"

type DBConfig struct {
	DBHost       string //数据所在服务器名IP或域名
	DBPort       int
	DatabaseName string
	UserName     string
	UserPass     string

	SSLMode        string
	MaxConnections int
	MinConnections int

	ConnectionTimeout int //最大链接超时时间

	ConnectionIdle int //最大空闲时间, 15 min
	ConnectionTTl  int //最大生存时间 30 min
}

type BaseDBPool struct {
	dbTypeName string //数据据名称,MySQL,PostgreSQL等

	dbConfig *DBConfig
}

type IBaseDBPool interface {
	GetDbTypeName() string
	GetDbConfig() *DBConfig
	InitDbPool(config *config.ConfigurationHelper) error
	CloseDbPool()
}

func (pool *BaseDBPool) GetDbTypeName() string {
	return pool.dbTypeName
}

func (pool *BaseDBPool) GetDbConfig() *DBConfig {
	return pool.dbConfig
}
