-- page (TRD §6.2): one consistent page of the live leaderboard; ranks are shared across pages (FR-24, FR-27).
-- KEYS: room, lb, roster. ARGV: offset, limit
local status = redis.call('HGET', KEYS[1], 'status')
if not status then return {'rejected', 'unknown_quiz'} end
local offset, limit = tonumber(ARGV[1]), tonumber(ARGV[2])
local rows = redis.call('ZREVRANGE', KEYS[2], offset, offset + limit - 1, 'WITHSCORES')
local ids = {}
for i = 1, #rows, 2 do ids[#ids + 1] = rows[i] end
local names = {}
if #ids > 0 then names = redis.call('HMGET', KEYS[3], unpack(ids)) end
local entries, rank_of = {}, {}
for i, id in ipairs(ids) do
  local score = rows[i * 2]
  local rank = rank_of[score]
  if not rank then
    rank = redis.call('ZCOUNT', KEYS[2], '(' .. score, '+inf') + 1
    rank_of[score] = rank
  end
  entries[i] = {id, names[i] or '', tonumber(score), rank}
end
return {'ok', status, redis.call('ZCARD', KEYS[2]), entries}
