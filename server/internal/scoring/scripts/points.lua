-- Speed-bonus scoring (FR-19, TRD §3.4). Mirrors scoring.Points; both are tested against
-- testdata/points_cases.json. Prepended to answer.lua.
local function points(correct, opened, deadline, received)
  if not correct then return 0 end
  local window = deadline - opened
  if window <= 0 then return 100 end
  local remaining = math.min(math.max(deadline - received, 0), window)
  return 100 + math.floor(100 * remaining / window)
end
