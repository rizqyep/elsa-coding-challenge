-- leave (TRD §4.4). KEYS: room, roster, lb, online, sched:lbdirty. ARGV: code, participant_id
-- Lobby: remove the player. After start: keep the score, only stop counting them as online.
local status = redis.call('HGET', KEYS[1], 'status')
if not status then return 'gone' end
local id = ARGV[2]
redis.call('ZREM', KEYS[4], id)
if status ~= 'lobby' then return 'offline' end
redis.call('HDEL', KEYS[2], id)
redis.call('ZREM', KEYS[3], id)
redis.call('SADD', KEYS[5], ARGV[1])
return 'removed'
