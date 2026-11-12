package memory_db

import (
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/memory_db/m_redis"
)

func CreateMemoryDb(storageCfg config.DataStorageConfig) common.IMemoryDB {
	//storageConfig, _ := config.LoadDataStorageConfig(helper)

	switch storageCfg.MemoryDBConfig.MemoryType {
	case config.MemoryDbTypeRedis:
		return createRedisDb(storageCfg)
	default:
	}
	return nil
}

// ================================ private functions & methods ===============================
func createRedisDb(storageCfg config.DataStorageConfig) common.IMemoryDB {
	return m_redis.GetRedisClient(storageCfg)
}
