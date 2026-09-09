/* 约定：脚本通过存储在 token key 的 JSON 再在脚本内拼出 user key。
*  注意：脚本内计算 key 仅适用于单节点 Redis；若启用 Redis Cluster 需改为显式声明
*  全部 KEYS，见“关键决策”一节。
*  返回 status: 0=不存在 1=有效 2=已被替换 3=已过期
*
*  定义用来原子性执行支持的lua脚本，被用于在Redis中执行
*
**/

package core

const (
	// LuaCreateOrReplace KEYS[1]=userKey(loginName已知) KEYS[2]=tokenKey
	// ARGV[1]=newToken ARGV[2]=sessionJSON ARGV[3]=ttlSec ARGV[4]=userCurrentTokenPrefix
	LuaCreateOrReplace = `
		local oldToken = redis.call('GET', KEYS[1])
		redis.call('SET', KEYS[2], ARGV[2], 'EX', ARGV[3])
		redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[3])
		if oldToken and oldToken ~= ARGV[1] then
		  redis.call('DEL', ARGV[4] .. oldToken)
		end
		return 1`

	// LuaGetEx LuaGet KEYS[1]=tokenKey
	// ARGV[1]=token ARGV[2]=nowSec ARGV[3]=userCurrentTokenPrefix
	LuaGetEx = `
		local value = redis.call('GET', KEYS[1])
		if not value then return {0, ''} end
		local s = cjson.decode(value)
		local userKey = ARGV[3] .. s.loginName
		local current = redis.call('GET', userKey)
		if not current or current ~= ARGV[1] then
		  redis.call('DEL', KEYS[1])
		  return {2, ''}
		end
		if tonumber(s.absoluteExpireAt) <= tonumber(ARGV[2]) then
		  redis.call('DEL', KEYS[1])
		  redis.call('DEL', userKey)
		  return {3, ''}
		end
		return {1, value}`

	// LuaRefresh KEYS[1]=tokenKey
	// ARGV[1]=token ARGV[2]=nowSec ARGV[3]=idleTTLSec ARGV[4]=userCurrentTokenPrefix
	LuaRefresh = `
		local value = redis.call('GET', KEYS[1])
		if not value then return {3, ''} end
		local s = cjson.decode(value)
		local userKey = ARGV[4] .. s.loginName
		local current = redis.call('GET', userKey)
		if not current or current ~= ARGV[1] then
		  return {2, ''}
		end
		local now = tonumber(ARGV[2])
		local remaining = tonumber(s.absoluteExpireAt) - now
		if remaining <= 0 then
		  redis.call('DEL', KEYS[1])
		  redis.call('DEL', userKey)
		  return {3, ''}
		end
		local ttl = remaining
		if ttl > tonumber(ARGV[3]) then ttl = tonumber(ARGV[3]) end
		s.lastActiveAt = now
		redis.call('SET', KEYS[1], cjson.encode(s), 'EX', ttl)
		redis.call('SET', userKey, ARGV[1], 'EX', ttl)
		return {1, cjson.encode(s)}`

	// LuaRevoke KEYS[1]=tokenKey
	// ARGV[1]=token ARGV[2]=userCurrentTokenPrefix
	// 返回 1=已删除 0=原本不存在
	LuaRevoke = `
		local value = redis.call('GET', KEYS[1])
		if not value then return 0 end
		local s = cjson.decode(value)
		local userKey = ARGV[2] .. s.loginName
		local current = redis.call('GET', userKey)
		if current == ARGV[1] then
		  redis.call('DEL', userKey)
		end
		redis.call('DEL', KEYS[1])
		return 1`
)
