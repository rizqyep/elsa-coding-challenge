-- join (TRD §4.4). KEYS: room, roster, lb, online, sched:lbdirty, channel
-- ARGV: code, participant_id, display_name, top_n, ttl_s, conn_id (empty: no kick)
if redis.call('EXISTS', KEYS[1]) == 0 then return {'rejected', 'unknown_quiz'} end
local status = redis.call('HGET', KEYS[1], 'status')
if status == 'expired' then return {'rejected', 'quiz_expired'} end
if status == 'finished' then return {'finished'} end

local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local id = ARGV[2]
redis.call('HSET', KEYS[2], id, ARGV[3])
redis.call('ZADD', KEYS[3], 'NX', 0, id)
redis.call('ZADD', KEYS[4], now, id)
redis.call('SADD', KEYS[5], ARGV[1])
for i = 2, 4 do redis.call('EXPIRE', KEYS[i], ARGV[5]) end
if ARGV[6] ~= '' then redis.call('PUBLISH', KEYS[6], cjson.encode({t = 'kick', p = id, k = ARGV[6]})) end

local score = tonumber(redis.call('ZSCORE', KEYS[3], id))
local higher = redis.call('ZCOUNT', KEYS[3], '(' .. score, '+inf')
local top = redis.call('ZREVRANGE', KEYS[3], 0, tonumber(ARGV[4]) - 1, 'WITHSCORES')
local ids = {}
for i = 1, #top, 2 do ids[#ids + 1] = top[i] end
local names = {}
if #ids > 0 then names = redis.call('HMGET', KEYS[2], unpack(ids)) end
return {'ok', redis.call('HGETALL', KEYS[1]), score, higher, redis.call('ZCARD', KEYS[3]), top, names, now}
