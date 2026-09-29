-- create_room (TRD §4.4). KEYS: room, qids, sched:transitions
-- ARGV: code, set_id, host_id, window_ms, reveal_ms, lobby_timeout_ms, ttl_s, question ids...
if redis.call('EXISTS', KEYS[1]) == 1 then
  return {'rejected', 'code_in_use'}
end
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local expires = now + tonumber(ARGV[6])
redis.call('HSET', KEYS[1],
  'set_id', ARGV[2], 'host_id', ARGV[3], 'status', 'lobby', 'q_index', -1, 'q_id', '',
  'q_count', #ARGV - 7, 'window_ms', ARGV[4], 'reveal_ms', ARGV[5],
  'opened_at', 0, 'deadline', 0, 'close_at', 0, 'next_at', expires,
  'start_requested', 0, 'lobby_expires_at', expires,
  'state_ver', 1, 'lb_ver', 0, 'pending_flush', 0, 'created_at', now)
redis.call('RPUSH', KEYS[2], unpack(ARGV, 8))
redis.call('EXPIRE', KEYS[1], ARGV[7])
redis.call('EXPIRE', KEYS[2], ARGV[7])
redis.call('ZADD', KEYS[3], expires, ARGV[1])
return {'ok', now}
