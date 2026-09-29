-- Quiz state machine (TRD §3.3). Mirrors quiz.Next; both are tested against
-- testdata/transition_cases.json. Mutates r; returns outcome and event type ('' for none).
local function next_state(r, now)
  if r.status == 'finished' or r.status == 'expired' then return 'terminal', '' end
  if now < r.next_at then return 'not_due', '' end
  local function open_question(i)
    r.status, r.q_index, r.opened_at = 'question_open', i, now
    r.deadline = now + r.window_ms
    r.close_at, r.next_at = r.deadline, r.deadline
    return 'question_opened'
  end
  local ev
  if r.status == 'lobby' then
    if r.start_requested then
      ev = open_question(0)
    elseif now >= r.lobby_expires_at then
      r.status, r.next_at = 'expired', 0
      ev = 'quiz_expired'
    else
      return 'not_due', ''
    end
  elseif r.status == 'question_open' then
    if now < r.close_at then return 'not_due', '' end
    r.status, r.next_at = 'question_closed', now + r.reveal_ms
    ev = 'question_closed'
  elseif r.status == 'question_closed' then
    if r.q_index < r.q_count - 1 then
      ev = open_question(r.q_index + 1)
    else
      r.status, r.next_at = 'finished', 0
      ev = 'quiz_finished'
    end
  else
    return 'not_due', ''
  end
  r.state_ver = r.state_ver + 1
  return 'applied', ev
end
