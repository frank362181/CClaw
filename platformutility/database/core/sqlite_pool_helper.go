package core

import (
	"my.favorite/platformutility/common"
)

var sqlitePool = common.GetSingleton(func() *SqliteDBPool {
	return &SqliteDBPool{
		BaseDBPool: BaseDBPool{
			DbTypeName: "sqlite",
			DBConfig:   nil,
		},
	}
}).Instance()

func GetSqliteDBPool(dbConfig *DBConfig) IBaseDBPool {
	sqlitePool.DBConfig = dbConfig
	return sqlitePool
}
