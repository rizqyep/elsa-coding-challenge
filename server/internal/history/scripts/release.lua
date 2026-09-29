-- release (TRD §4.4). KEYS: room, qids, roster, online, lb, sched:flush, sched:transitions. ARGV: code, job_id
redis.call('DEL', KEYS[1], KEYS[2], KEYS[3], KEYS[4], KEYS[5])
redis.call('ZREM', KEYS[6], ARGV[2])
redis.call('ZREM', KEYS[7], ARGV[1])
return {'ok'}
