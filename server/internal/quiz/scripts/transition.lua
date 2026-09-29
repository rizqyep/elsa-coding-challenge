-- transition (TRD §4.4); next_state and top_entries are prepended.
-- KEYS: room, qids, sched:transitions, sched:flush, sched:lbdirty, lb, roster, room channel
-- ARGV: code, expected_state_ver, top_n
local code = ARGV[1]
if redis.call('EXISTS', KEYS[1]) == 0 then
  redis.call('ZREM', KEYS[3], code)
  return {'stale'}
end
local h = redis.call('HMGET', KEYS[1], 'status', 'q_index', 'q_count', 'window_ms', 'reveal_ms', 'opened_at', 'deadline',
  'close_at', 'next_at', 'start_requested', 'lobby_expires_at', 'state_ver', 'q_id')
local r = {status = h[1], q_index = tonumber(h[2]), q_count = tonumber(h[3]), window_ms = tonumber(h[4]),
  reveal_ms = tonumber(h[5]), opened_at = tonumber(h[6]), deadline = tonumber(h[7]), close_at = tonumber(h[8]),
  next_at = tonumber(h[9]), start_requested = h[10] == '1', lobby_expires_at = tonumber(h[11]), state_ver = tonumber(h[12])}
if r.state_ver ~= tonumber(ARGV[2]) then return {'stale'} end

local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local outcome, ev = next_state(r, now)
if outcome == 'terminal' then
  redis.call('ZREM', KEYS[3], code)
  return {'terminal'}
end
if outcome == 'not_due' then
  redis.call('ZADD', KEYS[3], r.next_at, code)
  return {'not_due'}
end

local qid = h[13]
if ev == 'question_opened' then qid = redis.call('LINDEX', KEYS[2], r.q_index) end
redis.call('HSET', KEYS[1], 'status', r.status, 'q_index', r.q_index, 'q_id', qid, 'opened_at', r.opened_at,
  'deadline', r.deadline, 'close_at', r.close_at, 'next_at', r.next_at, 'state_ver', r.state_ver)
if r.next_at > 0 then redis.call('ZADD', KEYS[3], r.next_at, code) else redis.call('ZREM', KEYS[3], code) end

if ev == 'question_closed' then
  redis.call('ZADD', KEYS[4], now, 'q|' .. code .. '|' .. qid)
  redis.call('HINCRBY', KEYS[1], 'pending_flush', 1)
  redis.call('SADD', KEYS[5], code)
elseif ev == 'quiz_finished' or ev == 'quiz_expired' then
  redis.call('ZADD', KEYS[4], now, 'final|' .. code)
end

if ev == 'quiz_finished' then
  local entries, count = top_entries(KEYS[6], KEYS[7], tonumber(ARGV[3]))
  redis.call('PUBLISH', KEYS[8], cjson.encode({t = 'finished', v = r.state_ver, n = count, top = entries}))
else
  redis.call('PUBLISH', KEYS[8], cjson.encode({t = 'state', v = r.state_ver, s = r.status, i = r.q_index,
    n = r.q_count, q = qid, o = r.opened_at, d = r.deadline, c = r.close_at}))
end
return {'applied', r.status, r.state_ver}
