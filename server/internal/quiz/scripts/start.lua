-- start (TRD §4.4); same checks, in the same order, as quiz.Start.
-- KEYS: room, lb, sched:transitions. ARGV: code, caller_id
if redis.call('EXISTS', KEYS[1]) == 0 then
  return {'rejected', 'unknown_quiz'}
end
local r = redis.call('HMGET', KEYS[1], 'host_id', 'status', 'start_requested')
if r[1] ~= ARGV[2] then return {'rejected', 'not_host'} end
local running = r[2] == 'lobby' or r[2] == 'question_open' or r[2] == 'question_closed'
if r[3] == '1' and running then return {'ok'} end
if r[2] ~= 'lobby' then return {'rejected', 'not_in_lobby'} end
if redis.call('ZCARD', KEYS[2]) < 1 then return {'rejected', 'no_participants'} end
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
redis.call('HSET', KEYS[1], 'start_requested', 1, 'next_at', now)
redis.call('ZADD', KEYS[3], now, ARGV[1])
return {'ok'}
