local time = redis.call("TIME")
local now = tonumber(time[1]) * 1000000 + tonumber(time[2])
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])

redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", now - window)
local count = redis.call("ZCARD", KEYS[1])
local first = redis.call("ZRANGE", KEYS[1], 0, 0, "WITHSCORES")

if count >= limit then
    local retry = math.max(1, tonumber(first[2]) + window - now)
    redis.call("PEXPIRE", KEYS[1], ttl)
    return {0, count, retry}
end

local sequence = redis.call("INCR", KEYS[2])
redis.call("PEXPIRE", KEYS[2], ttl)
redis.call("ZADD", KEYS[1], now, tostring(now) .. ":" .. tostring(sequence))
redis.call("PEXPIRE", KEYS[1], ttl)
return {1, count + 1, 0}
