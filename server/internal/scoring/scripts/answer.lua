-- answer (TRD §4.4). KEYS: room, ans:{qid}, lb, roster, online, sched:lbdirty, sched:transitions, room channel
-- ARGV: code, participant_id, qid, option_id, correct (0/1), online_window_ms, ttl_s
local r = redis.call('HMGET', KEYS[1], 'status', 'q_id', 'close_at', 'opened_at', 'deadline', 'state_ver', 'q_index', 'q_count')
if r[1] ~= 'question_open' then return {'rejected', 'question_closed'} end
if r[2] ~= ARGV[3] then return {'rejected', 'wrong_question'} end
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
if now >= tonumber(r[3]) then return {'rejected', 'question_closed'} end
local id = ARGV[2]
if redis.call('HEXISTS', KEYS[4], id) == 0 then return {'rejected', 'not_joined'} end

local stored = redis.call('HGET', KEYS[2], id)
if stored then
  local opt, cor, pts, at = string.match(stored, '^([^|]*)|([^|]*)|([^|]*)|([^|]*)$')
  return {'duplicate', opt, tonumber(cor), tonumber(pts), tonumber(redis.call('ZSCORE', KEYS[3], id)), tonumber(at)}
end

local correct = ARGV[5] == '1'
local p = points(correct, tonumber(r[4]), tonumber(r[5]), now)
redis.call('HSET', KEYS[2], id, ARGV[4] .. '|' .. (correct and 1 or 0) .. '|' .. p .. '|' .. now)
if redis.call('TTL', KEYS[2]) < 0 then redis.call('EXPIRE', KEYS[2], ARGV[7]) end
local total = tonumber(redis.call('ZINCRBY', KEYS[3], p, id))
redis.call('SADD', KEYS[6], ARGV[1])

-- Early close (FR-5): same condition as quiz.EarlyClose; only close_at/next_at move.
local online = redis.call('ZCOUNT', KEYS[5], now - tonumber(ARGV[6]), '+inf')
if online > 0 and redis.call('HLEN', KEYS[2]) >= online then
  redis.call('HSET', KEYS[1], 'close_at', now, 'next_at', now)
  redis.call('ZADD', KEYS[7], now, ARGV[1])
  redis.call('PUBLISH', KEYS[8], cjson.encode({t = 'state', v = tonumber(r[6]), s = 'question_open',
    i = tonumber(r[7]), n = tonumber(r[8]), q = r[2], o = tonumber(r[4]), d = tonumber(r[5]), c = now, x = now}))
end
return {'accepted', ARGV[4], correct and 1 or 0, p, total, now}
