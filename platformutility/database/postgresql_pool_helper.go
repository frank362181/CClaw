package database

import "my.favorite/platformutility/common"

var pool = common.GetSingleton(func() *PostgresqlPool {
	return &PostgresqlPool{
		BaseDBPool: BaseDBPool{
			dbTypeName: "postgres",
			dbConfig:   nil,
		},

		pgxPool: nil,
	}
}).Instance()

func GetPGPool() *PostgresqlPool {
	return pool
}
