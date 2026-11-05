local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local previous = tonumber(redis.call("GET", KEYS[2]) or "0")
local window = tonumber(ARGV[1])
local elapsed = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
local weight = 1 - elapsed / window
local estimate = current + previous * weight

if estimate >= limit then
    local retry
    if current >= limit then
        retry = window - elapsed + window * (1 - limit / current)
    elseif previous > 0 then
        retry = window * (1 - (limit - current) / previous) - elapsed
    else
        retry = window - elapsed
    end
    redis.call("PEXPIRE", KEYS[1], ttl)
    return {0, math.floor(estimate * 1000000), math.max(1, math.ceil(retry))}
end

current = redis.call("INCR", KEYS[1])
redis.call("PEXPIRE", KEYS[1], ttl)
estimate = current + previous * weight
return {1, math.floor(estimate * 1000000), 0}
