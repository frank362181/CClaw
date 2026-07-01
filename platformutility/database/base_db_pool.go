package database

import (
	"my.favorite/platformutility/config"
)

type BaseDBPool struct {
	DbTypeName string //数据据名称,MySQL,PostgreSQL等

	DBConfig *DBConfig
	Opened   bool
}

type IBaseDBPool interface {
	GetDbTypeName() string
	GetDbConfig() DBConfig

	InitPool(config *config.ConfigurationHelper) error
	OpenPool() error
	Ping() error
	ClosePool()
}

func (pool *BaseDBPool) GetDbTypeName() string {
	return pool.DbTypeName
}

func (pool *BaseDBPool) GetDbConfig() DBConfig {
	return *pool.DBConfig
}
