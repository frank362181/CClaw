/*
* 定义多条redis命令合并成原子型语句
**/

package m_redis

const (
	LuaIncrementString = `
		-- 检查键是否已存在
		local exists = redis.call('EXISTS', KEYS[1])
	
		-- 使用 pcall 执行 INCRBY，它只返回一个值
		local result = redis.pcall('INCRBY', KEYS[1], ARGV[1])
	
		-- 检查返回值是否是一个包含 'err' 字段的 table
		if type(result) == 'table' and result['err'] then
			-- 若执行失败，返回清晰的错误信息
			return 'ERROR: ' .. tostring(result['err'])
		end
	
		-- 仅当键在本次操作前不存在时，设置过期时间（首次使用）
		if exists == 0 then
			redis.call('EXPIRE', KEYS[1], ARGV[2])
		end
	
		-- 返回递增后的新值
		return result`
)
