-- presence (TRD §7.7). KEYS: room, online. ARGV: participant_id…
-- Stamps Redis TIME, so gateway clocks never matter; a released room is not recreated.
local ttl = redis.call('PTTL', KEYS[1])
if ttl <= 0 then return 0 end
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local args = {}
for i = 1, #ARGV do
  args[#args + 1] = now
  args[#args + 1] = ARGV[i]
end
redis.call('ZADD', KEYS[2], unpack(args))
redis.call('PEXPIRE', KEYS[2], ttl)
return #ARGV
