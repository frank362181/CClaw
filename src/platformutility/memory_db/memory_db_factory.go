package memory_db

import (
	"my.favorite/platformutility/config"
	"my.favorite/platformutility/memory_db/common"
	"my.favorite/platformutility/memory_db/m_redis"
)

func CreateMemoryDb(config *config.ConfigurationHelper) common.IMemoryDB {
	var memoryDbType = common.MemoryDbTypeUnknown
	if config != nil {
		memoryDbType, _ = config.GetIntValueDefault("memoryDb", "memoryDbType", common.MemoryDbTypeRedis)
	}

	switch memoryDbType {
	case common.MemoryDbTypeRedis:
		return createRedisDb(config)
	default:
	}
	return nil
}

// ================================ private functions & methods ===============================
func createRedisDb(config *config.ConfigurationHelper) common.IMemoryDB {
	return m_redis.GetRedisClient(config)
}
