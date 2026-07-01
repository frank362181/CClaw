package pgdatabase

import (
	"my.favorite/platformutility/common"
	"my.favorite/platformutility/database"
)

var pool = common.GetSingleton(func() *PostgresqlPool {
	return &PostgresqlPool{
		BaseDBPool: database.BaseDBPool{
			DbTypeName: "postgres",
			DBConfig:   nil,
			Opened:     false,
		},

		pgxPool: nil,
	}
}).Instance()

func GetPGPool() database.IBaseDBPool {
	return pool
}
