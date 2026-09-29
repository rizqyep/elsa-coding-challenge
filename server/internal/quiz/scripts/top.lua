-- Top entries for room events (TRD §4.3): {{id, name, score}, ...} and the participant count.
local function top_entries(lb_key, roster_key, n)
  local top = redis.call('ZREVRANGE', lb_key, 0, n - 1, 'WITHSCORES')
  local ids = {}
  for i = 1, #top, 2 do ids[#ids + 1] = top[i] end
  local names = {}
  if #ids > 0 then names = redis.call('HMGET', roster_key, unpack(ids)) end
  local entries = {}
  for i, id in ipairs(ids) do entries[i] = {id, names[i] or '', tonumber(top[i * 2])} end
  return entries, redis.call('ZCARD', lb_key)
end
