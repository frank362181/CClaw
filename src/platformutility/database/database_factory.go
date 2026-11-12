package database

import (
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/database/core"
)

/*
* 该函数调用多次会创建多个Database实例,但是其底层的pool永远只有一个
 */

func GetCommonDatabase(storageCfg config.DataStorageConfig) core.IBaseDatabase {
	return core.GetCommonDatabase(storageCfg)
}

func GetGormDatabase(storageCfg config.DataStorageConfig) core.IBaseDatabase {
	return core.GetGormDatabase(storageCfg)
}
