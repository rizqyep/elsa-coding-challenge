-- view (TRD §7.10): a room's shared snapshot part, read-only. KEYS: room, lb, roster. ARGV: top_n
if redis.call('EXISTS', KEYS[1]) == 0 then return {'missing'} end
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local top = redis.call('ZREVRANGE', KEYS[2], 0, tonumber(ARGV[1]) - 1, 'WITHSCORES')
local ids = {}
for i = 1, #top, 2 do ids[#ids + 1] = top[i] end
local names = {}
if #ids > 0 then names = redis.call('HMGET', KEYS[3], unpack(ids)) end
return {'ok', redis.call('HGETALL', KEYS[1]), redis.call('ZCARD', KEYS[2]), top, names, now}
