-- lua allows scripts to be executed atomically
-- it reduces the number of round trips to redis and avoids race conditions
-- round trips to redis means with example : user sends a request to redis, redis processes it and sends back the response to user. This is one round trip. If user sends multiple requests, then it will be multiple round trips.
-- by using lua script, we can send multiple commands to redis in a single request and get the response back in a single response. This reduces the number of round trips to redis and avoids race conditions.
local time = redis.call("TIME")
local now = tonumber(time[1]) * 1000000 + tonumber(time[2])
local interval = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local ttl = tonumber(ARGV[3])

redis.call("ZREMRANGEBYSCORE", KEYS[1], "-inf", now)
local count = redis.call("ZCARD", KEYS[1])
local first = redis.call("ZRANGE", KEYS[1], 0, 0, "WITHSCORES")

if count >= capacity then
    local retry = math.max(1, tonumber(first[2]) - now)
    local last = redis.call("ZREVRANGE", KEYS[1], 0, 0, "WITHSCORES")
    return {0, count, retry, math.max(1, tonumber(last[2]) - now)}
end

local last = redis.call("ZREVRANGE", KEYS[1], 0, 0, "WITHSCORES")
local processAt = now + interval
if #last > 0 then
    processAt = math.max(processAt, tonumber(last[2]) + interval)
end
local sequence = redis.call("INCR", KEYS[2])
redis.call("PEXPIRE", KEYS[2], ttl)
redis.call("ZADD", KEYS[1], processAt, tostring(processAt) .. ":" .. tostring(sequence))
redis.call("PEXPIRE", KEYS[1], ttl)

local lastPending = redis.call("ZREVRANGE", KEYS[1], 0, 0, "WITHSCORES")
return {1, count + 1, 0, math.max(1, tonumber(lastPending[2]) - now)}
