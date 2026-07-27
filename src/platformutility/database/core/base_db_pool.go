package core

import (
	"database/sql"
)

type BaseDBPool struct {
	DbTypeName string //数据据名称,MySQL,PostgreSQL等
	DBConfig   *DBConfig
}

type IBaseDBPool interface {
	GetDbTypeName() string
	GetDbConfig() DBConfig

	IsOpened() bool

	OpenPool() error
	GetDatabase() (*sql.DB, error)
	Ping() error
	ClosePool()
}

func (pool *BaseDBPool) GetDbTypeName() string {
	return pool.DbTypeName
}

func (pool *BaseDBPool) GetDbConfig() DBConfig {
	return *pool.DBConfig
}

// ========================
