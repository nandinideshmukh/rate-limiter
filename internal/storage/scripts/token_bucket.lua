local time = redis.call("TIME")
local now = tonumber(time[1]) * 1000000 + tonumber(time[2])
local limit = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local window = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local scale = 1000000
local capacityScaled = capacity * scale

local tokens = tonumber(redis.call("HGET", KEYS[1], "tokens"))
local updatedAt = tonumber(redis.call("HGET", KEYS[1], "updatedAt"))
if tokens == nil or updatedAt == nil then
    tokens = capacityScaled
    updatedAt = now
else
    local elapsed = math.max(0, now - updatedAt)
    tokens = math.min(capacityScaled, tokens + elapsed * limit * scale / window)
end

local allowed = 0
local retry = 0
if tokens >= scale then
    tokens = tokens - scale
    allowed = 1
else
    retry = math.ceil((scale - tokens) * window / (limit * scale))
end

local reset = math.ceil((capacityScaled - tokens) * window / (limit * scale))
redis.call("HSET", KEYS[1], "tokens", tokens, "updatedAt", now)
redis.call("PEXPIRE", KEYS[1], ttl)
return {allowed, math.floor(tokens / scale), math.max(1, retry), math.max(1, reset)}
