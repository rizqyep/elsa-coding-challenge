-- flush_ack (TRD §4.4). KEYS: room, answers, sched:flush. ARGV: job_id
redis.call('DEL', KEYS[2])
if redis.call('ZREM', KEYS[3], ARGV[1]) == 1 and redis.call('EXISTS', KEYS[1]) == 1 then
  redis.call('HINCRBY', KEYS[1], 'pending_flush', -1)
end
return {'ok'}
