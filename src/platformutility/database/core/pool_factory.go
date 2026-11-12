package core

import (
	"my.favorite/platformutility/config"
)

func GetDBPool(storageCfg config.DataStorageConfig) IBaseDBPool {
	switch storageCfg.DBConfig.DBType {
	case config.ConstDbTypePostgresSQL:
		return GetPGPool(storageCfg)
	case config.ConstDbTypeSqlite:
		return GetSqliteDBPool(storageCfg)
	default:
		break
	}

	return nil
}
