local count = tonumber(redis.call("GET", KEYS[1]) or "0")
local ttl = redis.call("PTTL", KEYS[1])
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])

if count >= limit then
    if ttl == -1 then
        redis.call("PEXPIRE", KEYS[1], window)
        ttl = window
    end
    return {0, count, ttl}
end

count = redis.call("INCR", KEYS[1])
if count == 1 or ttl == -1 then
    redis.call("PEXPIRE", KEYS[1], window)
    ttl = window
else
    ttl = redis.call("PTTL", KEYS[1])
end

return {1, count, ttl}
