-- standings (TRD §7.5): score and number of higher scores per participant, read-only. KEYS: lb. ARGV: ids…
local out = {}
for i = 1, #ARGV do
  local s = redis.call('ZSCORE', KEYS[1], ARGV[i])
  if s then
    out[#out + 1] = tonumber(s)
    out[#out + 1] = redis.call('ZCOUNT', KEYS[1], '(' .. s, '+inf')
  else
    out[#out + 1] = -1
    out[#out + 1] = 0
  end
end
return out
