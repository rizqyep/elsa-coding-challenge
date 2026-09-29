package testkit

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// RoomSpec describes one simulated room (TRD §10.7). The API has no per-quiz question count, so
// the question set decides how many questions a room has.
type RoomSpec struct {
	QuestionSet   string
	Participants  int
	JoinRamp      time.Duration // joins spread evenly over this; 0 means all at once
	Window        time.Duration // answer window per question
	Reveal        time.Duration
	AnswerWithin  time.Duration // each participant answers at a random point in this after the question arrives
	CorrectRatio  float64       // share of answers that are right
	NoAnswerRatio float64       // share of questions a participant leaves unanswered
	SlowRatio     float64       // share of participants that read slowly
	SlowReadDelay time.Duration // their pause before each read; 200 ms when zero
	Seed          uint64
	Live          *Live // optional counters updated while the run is in progress
}

// Live counts progress while rooms run, for a live summary; share one across rooms.
type Live struct{ Joined, Answers, Accepted, Errors, Finished atomic.Int64 }

// Step is a timed action during a run, such as a fault. Question 0 anchors it to the start.
type Step struct {
	Name     string
	Question int           // 1-based question whose opening anchors the step
	After    time.Duration // delay after the anchor
	Do       func(ctx context.Context) error
}

// Result is what a run measured and checked.
type Result struct {
	Code                                  string
	Participants, Joined, Finished        int
	NFR6, NFR7, NFR8, NFR9, TransitionLag Summary
	Rejoin                                Summary // dropped connection → next snapshot
	Raw                                   Recorders
	AnswersSent, Accepted, Duplicates     int64
	Reconnects                            int64
	Stale                                 int
	Errors                                map[string]int // error message codes
	Closes                                map[int]int    // unexpected close codes
	Violations, PointMismatches           []string
	TotalMismatches, StepErrors           []string
	Elapsed                               time.Duration
}

// Recorders are a run's raw latencies, for merging percentiles across rooms.
type Recorders struct{ NFR6, NFR7, NFR8, NFR9, TransitionLag, Rejoin *Recorder }

// OK reports whether nothing went wrong that a correct system would never do.
func (r *Result) OK() bool {
	return len(r.Violations) == 0 && len(r.PointMismatches) == 0 && len(r.TotalMismatches) == 0 && len(r.StepErrors) == 0 &&
		r.Finished == r.Joined && r.Joined == r.Participants
}

// run holds one room's shared state while it runs.
type run struct {
	env     *Compose
	spec    RoomSpec
	code    string
	keys    map[string]string
	nfr6    []time.Time           // accepted answers' server receive times
	lbSeen  map[int64][]time.Time // leaderboard version → arrival at each fast client
	nfr7    Recorder
	nfr8    Recorder
	nfr9    Recorder
	lag     Recorder
	rejoin  Recorder
	mu      sync.Mutex
	errs    map[string]int
	closes  map[int]int
	notes   []string // violations, capped
	points  []string // per-answer point mismatches, capped
	sent    atomic.Int64
	accepts atomic.Int64
	dups    atomic.Int64
	reconn  atomic.Int64
	stale   atomic.Int64
	joined  atomic.Int64
	done    atomic.Int64
	allDone chan struct{}
}

const maxNotes = 20

func (r *run) live(f func(*Live)) {
	if r.spec.Live != nil {
		f(r.spec.Live)
	}
}

func (r *run) note(list *[]string, s string) {
	r.mu.Lock()
	if len(*list) < maxNotes {
		*list = append(*list, s)
	}
	r.mu.Unlock()
}

// RunRoom creates a quiz, joins the participants, starts it, lets them answer, and checks the outcome
// against what they sent: every answer's points and every archived total are recomputed independently.
func (e *Compose) RunRoom(ctx context.Context, spec RoomSpec, steps ...Step) (*Result, error) {
	if spec.SlowReadDelay == 0 {
		spec.SlowReadDelay = 200 * time.Millisecond
	}
	began := time.Now()
	host, err := e.DevToken(ctx, "host")
	if err != nil {
		return nil, err
	}
	code, err := e.CreateQuiz(ctx, host, spec.QuestionSet, spec.Window, spec.Reveal)
	if err != nil {
		return nil, err
	}
	keys, err := e.AnswerKeys(ctx, spec.QuestionSet)
	if err != nil {
		return nil, err
	}
	r := &run{env: e, spec: spec, code: code, keys: keys, lbSeen: map[int64][]time.Time{},
		errs: map[string]int{}, closes: map[int]int{}, allDone: make(chan struct{})}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stepErrs []string
	var stepMu sync.Mutex
	fire := func(s Step) {
		time.AfterFunc(s.After, func() {
			if err := s.Do(runCtx); err != nil {
				stepMu.Lock()
				stepErrs = append(stepErrs, fmt.Sprintf("%s: %v", s.Name, err))
				stepMu.Unlock()
			}
		})
	}
	w := &watcher{r: r, host: host, steps: steps, fire: fire}
	go w.loop(runCtx)

	players := make([]*player, spec.Participants)
	var wg sync.WaitGroup
	for i := range players {
		rng := rand.New(rand.NewPCG(spec.Seed, uint64(i)))
		p := &player{r: r, idx: i, rng: rng, slow: rng.Float64() < spec.SlowRatio,
			questions: map[string]qinfo{}, decided: map[string]bool{}, pending: map[string]pendingAnswer{}, counted: map[string]bool{}}
		players[i] = p
		var delay time.Duration
		if spec.JoinRamp > 0 && spec.Participants > 1 {
			delay = spec.JoinRamp * time.Duration(i) / time.Duration(spec.Participants-1)
		}
		wg.Go(func() { p.loop(runCtx, delay) })
	}

	// Start once everyone joined, or when the ramp plus a grace period has passed.
	joinWait := time.NewTimer(spec.JoinRamp + 30*time.Second)
	for int(r.joined.Load()) < spec.Participants {
		select {
		case <-joinWait.C:
			goto start
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
start:
	joinWait.Stop()
	if err := e.Start(ctx, host, code); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}
	for _, s := range steps {
		if s.Question == 0 {
			fire(s)
		}
	}

	select {
	case <-r.allDone:
	case <-ctx.Done():
	}
	cancel()
	wg.Wait()

	res := &Result{Code: code, Participants: spec.Participants, Joined: int(r.joined.Load()), Finished: int(r.done.Load()),
		NFR7: r.nfr7.Summary(), NFR8: r.nfr8.Summary(), NFR9: r.nfr9.Summary(), TransitionLag: r.lag.Summary(),
		AnswersSent: r.sent.Load(), Accepted: r.accepts.Load(), Duplicates: r.dups.Load(), Reconnects: r.reconn.Load(),
		Stale: int(r.stale.Load()), Errors: r.errs, Closes: r.closes, Violations: r.notes, PointMismatches: r.points}
	nfr6 := &Recorder{}
	for _, d := range leaderboardLatencies(r.nfr6, r.lbSeen) {
		nfr6.Add(d)
	}
	res.NFR6, res.Rejoin = nfr6.Summary(), r.rejoin.Summary()
	res.Raw = Recorders{NFR6: nfr6, NFR7: &r.nfr7, NFR8: &r.nfr8, NFR9: &r.nfr9, TransitionLag: &r.lag, Rejoin: &r.rejoin}
	stepMu.Lock()
	res.StepErrors = stepErrs
	stepMu.Unlock()

	// Every archived total must equal the sum of the points recomputed from what the participant sent.
	archiveCtx, cancelArchive := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancelArchive()
	totals, err := e.ArchivedTotals(archiveCtx, code, res.Joined)
	if err != nil {
		res.TotalMismatches = append(res.TotalMismatches, "archive: "+err.Error())
	}
	for _, p := range players {
		if !p.joined {
			continue
		}
		if got, ok := totals[p.id]; !ok || got != p.expected {
			if len(res.TotalMismatches) < maxNotes {
				res.TotalMismatches = append(res.TotalMismatches, fmt.Sprintf("%s: archived %d (found %v), expected %d", p.id, got, ok, p.expected))
			}
		}
	}
	res.Elapsed = time.Since(began)
	return res, nil
}

// watcher is the host's connection: it anchors steps to questions and measures transition lag.
type watcher struct {
	r      *run
	host   Token
	steps  []Step
	fire   func(Step)
	seen   map[string]bool
	nextAt int64
}

func (w *watcher) loop(ctx context.Context) {
	w.seen = map[string]bool{}
	var mu sync.Mutex
	for ctx.Err() == nil {
		c, err := DialWith(ctx, w.r.env.WSURL(), w.host.Token, DialOptions{Origin: w.r.env.BaseURL, NoLog: true, OnMessage: func(m Message) {
			mu.Lock()
			defer mu.Unlock()
			switch m.Type {
			case "question":
				q := m.Data["question"].(map[string]any)
				id := q["questionId"].(string)
				if w.seen[id] {
					return // an early close re-sends the question with an earlier closeAt
				}
				w.seen[id] = true
				if opened := num(q["openedAt"]); w.nextAt > 0 && opened >= w.nextAt {
					w.r.lag.Add(time.Duration(opened-w.nextAt) * time.Millisecond)
				}
				for _, s := range w.steps {
					if s.Question == int(num(q["index"]))+1 {
						w.fire(s)
					}
				}
			case "question_closed":
				w.nextAt = num(m.Data["nextTransitionAt"])
			}
		}})
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		_, _ = c.Send("watch", map[string]any{"quizCode": w.r.code})
		select {
		case <-c.Done():
		case <-ctx.Done():
			c.Close()
			return
		}
	}
}

type qinfo struct{ openedAt, deadline, closeAt int64 }

type pendingAnswer struct {
	qid, option string
	sentAt      time.Time
}

// player is one simulated participant; it reconnects and resends unanswered answers until the quiz ends.
type player struct {
	r        *run
	idx      int
	rng      *rand.Rand
	slow     bool
	id       string
	joined   bool
	expected int

	mu        sync.Mutex
	c         *Client
	joinSent  time.Time
	droppedAt time.Time // when the last connection dropped, until the next snapshot
	finished  bool
	questions map[string]qinfo
	decided   map[string]bool
	pending   map[string]pendingAnswer // request id → answer
	counted   map[string]bool          // questions whose result was counted
}

func (p *player) loop(ctx context.Context, delay time.Duration) {
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return
	}
	tok, err := p.r.env.DevToken(ctx, "participant")
	if err != nil {
		p.r.note(&p.r.notes, "token: "+err.Error())
		return
	}
	p.id = tok.ParticipantID
	for attempt := 0; ctx.Err() == nil; attempt++ {
		opts := DialOptions{Origin: p.r.env.BaseURL, NoLog: true, OnMessage: p.onMessage}
		if p.slow {
			opts.ReadDelay = p.r.spec.SlowReadDelay
		}
		c, err := DialWith(ctx, p.r.env.WSURL(), tok.Token, opts)
		if err != nil {
			time.Sleep(time.Duration(200+p.rng.IntN(400)) * time.Millisecond)
			continue
		}
		p.mu.Lock()
		p.c, p.joinSent = c, time.Now()
		p.mu.Unlock()
		_, _ = c.Send("join", map[string]any{"quizCode": p.r.code, "displayName": fmt.Sprintf("P%d", p.idx+1)})
		select {
		case <-c.Done():
		case <-ctx.Done():
			c.Close()
			return
		}
		p.r.stale.Add(int64(c.Stale()))
		for _, v := range c.Violations() {
			p.r.note(&p.r.notes, p.id+": "+v)
		}
		p.mu.Lock()
		finished := p.finished
		p.mu.Unlock()
		if finished || ctx.Err() != nil {
			return
		}
		code := c.CloseCode()
		p.r.mu.Lock()
		p.r.closes[code]++
		p.r.mu.Unlock()
		if code == 4000 { // replaced by a newer connection: never reconnect (TRD §9.5)
			return
		}
		p.r.reconn.Add(1)
		p.mu.Lock()
		p.droppedAt = time.Now()
		p.mu.Unlock()
		time.Sleep(time.Duration(100+p.rng.IntN(500)) * time.Millisecond)
	}
}

func (p *player) onMessage(m Message) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch m.Type {
	case "snapshot":
		p.r.nfr9.Add(m.At.Sub(p.joinSent))
		if !p.droppedAt.IsZero() {
			p.r.rejoin.Add(m.At.Sub(p.droppedAt))
			p.droppedAt = time.Time{}
		}
		if !p.joined {
			p.joined = true
			p.r.joined.Add(1)
			p.r.live(func(l *Live) { l.Joined.Add(1) })
		}
		if q, ok := m.Data["quiz"].(map[string]any); ok && q["status"] == "finished" {
			p.finish()
			return
		}
		for id, a := range p.pending { // resend with the original ids: the server returns the original result (FR-30)
			_ = p.c.SendID(id, "submit_answer", map[string]any{"questionId": a.qid, "optionId": a.option})
		}
		if q, ok := m.Data["question"].(map[string]any); ok {
			p.question(q, m.At, false)
		}
	case "question":
		p.question(m.Data["question"].(map[string]any), m.At, true)
	case "answer_result":
		p.result(m)
	case "error":
		code, _ := m.Data["code"].(string)
		p.r.mu.Lock()
		p.r.errs[code]++
		p.r.mu.Unlock()
		p.r.live(func(l *Live) { l.Errors.Add(1) })
		a, ok := p.pending[m.ID]
		if !ok {
			return
		}
		if retry, _ := m.Data["retryable"].(bool); retry {
			wait := time.Duration(num(m.Data["retryAfterMs"])) * time.Millisecond
			if wait == 0 {
				wait = 500 * time.Millisecond
			}
			id, c := m.ID, p.c
			time.AfterFunc(wait, func() { _ = c.SendID(id, "submit_answer", map[string]any{"questionId": a.qid, "optionId": a.option}) })
			return
		}
		delete(p.pending, m.ID)
	case "leaderboard":
		if !p.slow {
			p.r.mu.Lock()
			v := num(m.Data["version"])
			p.r.lbSeen[v] = append(p.r.lbSeen[v], m.At)
			p.r.mu.Unlock()
		}
	case "quiz_finished":
		p.finish()
	}
}

// question decides once per question whether and what to answer, and schedules the answer.
func (p *player) question(q map[string]any, arrived time.Time, live bool) {
	id := q["questionId"].(string)
	if p.decided[id] {
		if info, ok := p.questions[id]; ok { // an early close re-sends it with an earlier closeAt (asyncapi send_question)
			info.closeAt = min(info.closeAt, num(q["closeAt"]))
			p.questions[id] = info
		}
		return
	}
	p.decided[id] = true
	info := qinfo{openedAt: num(q["openedAt"]), deadline: num(q["deadline"]), closeAt: num(q["closeAt"])}
	p.questions[id] = info
	if live && !p.slow {
		p.r.nfr8.Add(arrived.Sub(time.UnixMilli(info.openedAt)))
	}
	var opts []string
	for _, o := range q["options"].([]any) {
		opts = append(opts, o.(map[string]any)["id"].(string))
	}
	choice := chooseOption(p.rng, opts, p.r.keys[id], p.r.spec.CorrectRatio, p.r.spec.NoAnswerRatio)
	if choice == "" {
		return
	}
	var delay time.Duration
	if p.r.spec.AnswerWithin > 0 {
		delay = time.Duration(p.rng.Int64N(int64(p.r.spec.AnswerWithin)))
	}
	time.AfterFunc(delay, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.finished {
			return
		}
		reqID, err := p.c.Send("submit_answer", map[string]any{"questionId": id, "optionId": choice})
		p.pending[reqID] = pendingAnswer{qid: id, option: choice, sentAt: time.Now()}
		p.r.sent.Add(1)
		p.r.live(func(l *Live) { l.Answers.Add(1) })
		_ = err // a failed send stays pending and is resent after the rejoin
	})
}

func (p *player) result(m Message) {
	a, ok := p.pending[m.ID]
	if !ok {
		return
	}
	delete(p.pending, m.ID)
	p.r.nfr7.Add(m.At.Sub(a.sentAt))
	if m.Data["status"] == "duplicate" {
		p.r.dups.Add(1)
	} else {
		p.r.accepts.Add(1)
		p.r.live(func(l *Live) { l.Accepted.Add(1) })
	}
	if p.counted[a.qid] {
		return
	}
	p.counted[a.qid] = true
	q := p.questions[a.qid]
	received := num(m.Data["receivedAt"])
	if received >= q.closeAt { // acceptance is received < closeAt (TRD §3.4), even with no worker to close the question
		p.r.note(&p.r.notes, fmt.Sprintf("%s %s: accepted at %d, after closeAt %d", p.id, a.qid, received, q.closeAt))
	}
	want := ExpectedPoints(p.r.keys[a.qid] == a.option, q.openedAt, q.deadline, received)
	if got := int(num(m.Data["points"])); got != want {
		p.r.note(&p.r.points, fmt.Sprintf("%s %s: server %d, expected %d", p.id, a.qid, got, want))
	}
	p.expected += want
	p.r.mu.Lock()
	p.r.nfr6 = append(p.r.nfr6, time.UnixMilli(received))
	p.r.mu.Unlock()
}

// finish marks the quiz over for this player and hangs up. Holds p.mu.
func (p *player) finish() {
	if p.finished {
		return
	}
	p.finished = true
	p.r.live(func(l *Live) { l.Finished.Add(1) })
	if n := p.r.done.Add(1); int(n) == p.r.spec.Participants {
		close(p.r.allDone)
	}
	p.c.Close()
}

// chooseOption returns "" (no answer) with probability none, else the correct option with probability
// right, else a wrong one.
func chooseOption(rng *rand.Rand, options []string, correct string, right, none float64) string {
	if rng.Float64() < none {
		return ""
	}
	if rng.Float64() < right {
		return correct
	}
	var wrong []string
	for _, o := range options {
		if o != correct {
			wrong = append(wrong, o)
		}
	}
	if len(wrong) == 0 {
		return correct
	}
	return wrong[rng.IntN(len(wrong))]
}

// leaderboardLatencies measures NFR-6 as an approximation the kit can observe: an answer accepted at t is
// covered by the first leaderboard version that began arriving after t, and its latency is when that version
// reached the last fast client. Answers after the final update are not counted.
func leaderboardLatencies(accepted []time.Time, arrivals map[int64][]time.Time) []time.Duration {
	type version struct{ first, last time.Time }
	var vs []version
	for _, ts := range arrivals {
		if len(ts) == 0 {
			continue
		}
		v := version{first: ts[0], last: ts[0]}
		for _, t := range ts {
			if t.Before(v.first) {
				v.first = t
			}
			if t.After(v.last) {
				v.last = t
			}
		}
		vs = append(vs, v)
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i].first.Before(vs[j].first) })
	out := make([]time.Duration, 0, len(accepted))
	for _, t := range slices.Clone(accepted) {
		i := sort.Search(len(vs), func(i int) bool { return vs[i].first.After(t) })
		if i < len(vs) {
			out = append(out, vs[i].last.Sub(t))
		}
	}
	return out
}
