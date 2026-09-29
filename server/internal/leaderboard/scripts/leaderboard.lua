-- leaderboard (TRD §4.4); top_entries is prepended. KEYS: room, lb, roster, room channel. ARGV: code, top_n
if redis.call('EXISTS', KEYS[1]) == 0 then return {'stale'} end
local entries, count = top_entries(KEYS[2], KEYS[3], tonumber(ARGV[2]))
local ver = redis.call('HINCRBY', KEYS[1], 'lb_ver', 1)
redis.call('PUBLISH', KEYS[4], cjson.encode({t = 'lb', v = ver, n = count, top = entries}))
return {'ok', ver}
