package leaderboard

const redisCreateScript = `
if redis.call("EXISTS", KEYS[1]) == 1 then
	return -1
end
redis.call("SET", KEYS[1], ARGV[2])
redis.call("SADD", KEYS[2], ARGV[1])
local limit = tonumber(ARGV[4])
if limit > 0 then
	redis.call("RPUSH", KEYS[3], ARGV[3])
	redis.call("LTRIM", KEYS[3], -limit, -1)
end
return 1
`

const redisDeleteScript = `
if redis.call("EXISTS", KEYS[1]) == 0 then
	return -1
end
redis.call("DEL", KEYS[1], KEYS[2], KEYS[3])
redis.call("SREM", KEYS[4], ARGV[1])
local limit = tonumber(ARGV[3])
if limit > 0 then
	redis.call("RPUSH", KEYS[5], ARGV[2])
	redis.call("LTRIM", KEYS[5], -limit, -1)
end
return 1
`

const redisSaveRecord = `
redis.call("HSET", KEYS[1], ARGV[1], ARGV[2])
redis.call("ZADD", KEYS[2], ARGV[3], ARGV[1])
local limit = tonumber(ARGV[5])
if limit > 0 then
	redis.call("RPUSH", KEYS[3], ARGV[4])
	redis.call("LTRIM", KEYS[3], -limit, -1)
end
return 1
`

const redisDeleteRecords = `
local count = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local offset = 3
for i = 1, count do
	local owner_id = ARGV[offset]
	local history = ARGV[offset + 1]
	redis.call("HDEL", KEYS[1], owner_id)
	redis.call("ZREM", KEYS[2], owner_id)
	if limit > 0 then
		redis.call("RPUSH", KEYS[3], history)
	end
	offset = offset + 2
end
if limit > 0 then
	redis.call("LTRIM", KEYS[3], -limit, -1)
end
return count
`

const redisResetScript = `
if redis.call("EXISTS", KEYS[1]) == 0 then
	return -1
end
redis.call("SET", KEYS[1], ARGV[1])
redis.call("DEL", KEYS[2], KEYS[3])
local limit = tonumber(ARGV[3])
if limit > 0 then
	redis.call("RPUSH", KEYS[4], ARGV[2])
	redis.call("LTRIM", KEYS[4], -limit, -1)
end
return 1
`
