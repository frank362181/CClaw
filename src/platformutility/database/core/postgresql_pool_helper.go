package core

import (
	"my.favorite/platformutility/common"
)

var pgPool = common.GetSingleton(func() *PostgresqlPool {
	return &PostgresqlPool{
		BaseDBPool: BaseDBPool{
			DbTypeName: "postgres",
			DBConfig:   nil,
		},

		pgxPool: nil,
	}
}).Instance()

func GetPGPool(dbConfig *DBConfig) IBaseDBPool {
	pgPool.DBConfig = dbConfig
	return pgPool
}
