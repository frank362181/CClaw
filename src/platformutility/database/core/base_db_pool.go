package core

import (
	"database/sql"

	"my.favorite/platformutility/config"
)

type BaseDBPool struct {
	DbTypeName    string //数据据名称,MySQL,PostgreSQL等
	StorageConfig config.DataStorageConfig
}

type IBaseDBPool interface {
	GetDbTypeName() string
	GetDbConfig() config.DataStorageConfig

	IsOpened() bool

	OpenPool() error
	GetDatabase() (*sql.DB, error)
	Ping() error
	ClosePool()
}

func (pool *BaseDBPool) GetDbTypeName() string {
	return pool.DbTypeName
}

func (pool *BaseDBPool) GetDbConfig() config.DataStorageConfig {
	return pool.StorageConfig
}

// ========================
