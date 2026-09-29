-- claim (TRD §4.4). KEYS: sched:flush. ARGV: batch, visibility_ms
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local ids = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now, 'LIMIT', 0, tonumber(ARGV[1]))
for _, id in ipairs(ids) do redis.call('ZADD', KEYS[1], now + tonumber(ARGV[2]), id) end
return ids
