// Command sim runs a load or chaos scenario against the running stack and checks it against the NFRs (TRD §11.4).
//
//	sim [flags] loadtest/scenarios/big-room.yaml [participants=10000 window=15s chaos=off ...]
package main

// AI-assisted: AI-039, AI-040 (docs/ai-collaboration/log.md).

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/testkit"
)

func main() {
	base := flag.String("base", "http://localhost:8080", "stack URL (nginx)")
	origin := flag.String("origin", "", "Origin header for WebSockets when -base isn't an allowed origin (e.g. nginx's container IP)")
	dsn := flag.String("dsn", "postgres://quiz:quiz-local-only@localhost:15432/quiz?sslmode=disable", "PostgreSQL, for answer keys and archived results")
	out := flag.String("out", "", "directory for the JSON report (default: results/ beside the scenarios directory)")
	timeout := flag.Duration("timeout", 20*time.Minute, "give up after this long")
	cpuProfile := flag.String("cpuprofile", "", "write the simulator's own CPU profile here")
	mutexProfile := flag.String("mutexprofile", "", "write the simulator's lock-contention profile here")
	flag.Parse()
	if *mutexProfile != "" {
		runtime.SetMutexProfileFraction(5)
		defer writeProfile("mutex", *mutexProfile)
	}
	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err == nil && pprof.StartCPUProfile(f) == nil {
			defer pprof.StopCPUProfile()
		}
	}
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: sim [flags] <scenario.yaml> [key=value ...]")
		os.Exit(2)
	}
	dir := *out
	if dir == "" { // loadtest/scenarios/x.yaml → loadtest/results, whatever the working directory
		dir = filepath.Join(filepath.Dir(flag.Arg(0)), "..", "results")
	}
	code, err := run(*base, *origin, *dsn, dir, *timeout, flag.Arg(0), flag.Args()[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "sim:", err)
	}
	pprof.StopCPUProfile()
	if *mutexProfile != "" {
		writeProfile("mutex", *mutexProfile)
	}
	os.Exit(code)
}

func run(base, origin, dsn, outDir string, timeout time.Duration, path string, overrides []string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 2, err
	}
	sc, err := LoadScenario(raw)
	if err != nil {
		return 2, err
	}
	if err := sc.ApplyOverrides(overrides); err != nil {
		return 2, err
	}
	raiseFileLimit()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	env, err := testkit.NewCompose(ctx, base, dsn)
	if err != nil {
		return 2, err
	}
	defer env.Close()
	defer restore(env)
	if origin != "" {
		env.Origin = origin
	}
	before := reconciliation(ctx, env)

	fmt.Printf("%s: %d room(s) × %d participants, set %s, window %v, reveal %v, %d chaos step(s)\n",
		sc.Name, sc.Rooms, sc.ParticipantsPerRoom, sc.QuestionSet, sc.Window, sc.Reveal, len(sc.Chaos))
	live := &testkit.Live{}
	started := time.Now()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	go func() {
		total := sc.Rooms * sc.ParticipantsPerRoom
		for range ticker.C {
			fmt.Printf("  %5.0fs  joined %d/%d · answers %d · accepted %d · errors %d · finished %d\n", time.Since(started).Seconds(),
				live.Joined.Load(), total, live.Answers.Load(), live.Accepted.Load(), live.Errors.Load(), live.Finished.Load())
		}
	}()

	results := make([]*testkit.Result, sc.Rooms)
	errs := make([]error, sc.Rooms)
	var wg sync.WaitGroup
	for i := range sc.Rooms {
		spec := testkit.RoomSpec{QuestionSet: sc.QuestionSet, Participants: sc.ParticipantsPerRoom, JoinRamp: sc.JoinRampUp,
			Window: sc.Window, Reveal: sc.Reveal, AnswerWithin: sc.Answers.Within, CorrectRatio: sc.Answers.CorrectRatio,
			NoAnswerRatio: sc.Answers.NoAnswerRatio, SlowRatio: sc.SlowClients, Seed: sc.Seed + uint64(i), Live: live,
			ValidateEvery: validateEvery}
		var steps []testkit.Step
		if i == 0 { // faults are anchored to the first room's questions
			for _, c := range sc.Chaos {
				steps = append(steps, testkit.Step{Name: c.Text, Question: c.Question, After: c.After, Do: chaosDo(env, c.Action)})
			}
		}
		wg.Go(func() {
			select {
			case <-time.After(time.Duration(i) * sc.RoomStagger):
			case <-ctx.Done():
				return
			}
			results[i], errs[i] = env.RunRoom(ctx, spec, steps...)
		})
	}
	wg.Wait()
	ticker.Stop()
	var done []*testkit.Result
	for i, r := range results {
		if errs[i] != nil {
			return 1, fmt.Errorf("room %d: %w", i, errs[i])
		}
		done = append(done, r)
	}

	agg := AggregateResults(done)
	agg.ReconciliationMetric = reconciliation(context.WithoutCancel(ctx), env) - before
	checks := Evaluate(agg, sc.Assert)
	pass := true
	fmt.Printf("\n%s finished in %.1fs\n", sc.Name, time.Since(started).Seconds())
	fmt.Printf("  answers %d (accepted %d, duplicate %d) · reconnects %d · stale %d · errors %v · closes %v\n",
		agg.AnswersSent, agg.Accepted, agg.Duplicates, agg.Reconnects, agg.Stale, agg.Errors, agg.Closes)
	for _, c := range checks {
		mark := "PASS"
		if !c.Pass {
			mark, pass = "FAIL", false
		}
		fmt.Printf("  %s  %-15s %s  (target %s)\n", mark, c.Name, c.Actual, c.Target)
	}
	for _, list := range [][]string{agg.Violations, agg.PointMismatches, agg.TotalMismatches, agg.StepErrors} {
		for _, s := range list {
			fmt.Println("   ", s)
		}
	}
	file, err := writeReport(ctx, env, outDir, sc, overrides, started, agg, checks, pass, done)
	if err != nil {
		return 1, err
	}
	fmt.Println("  report:", file)
	if !pass {
		return 1, nil
	}
	return 0, nil
}

func writeProfile(name, path string) {
	if f, err := os.Create(path); err == nil {
		_ = pprof.Lookup(name).WriteTo(f, 0)
		_ = f.Close()
	}
}

// validateEvery samples schema validation in the simulator: every client checks the first message of each
// type, then every 100th. Checking all of them made the load generator the bottleneck (task-29).
const validateEvery = 100

// chaosDo turns a scenario action into a step (TRD §11.5).
func chaosDo(env *testkit.Compose, a Action) func(context.Context) error {
	wait := func(ctx context.Context, d time.Duration) {
		select {
		case <-time.After(d):
		case <-ctx.Done():
		}
	}
	return func(ctx context.Context) error {
		switch a.Kind {
		case "kill":
			return env.Kill(ctx, env.Project+"-"+a.Arg)
		case "stop":
			return env.StopService(ctx, a.Arg)
		case "redis-latency":
			return env.RedisLatency(ctx, a.N)
		case "redis-down":
			if err := env.SetRedisEnabled(ctx, false); err != nil {
				return err
			}
			wait(ctx, a.D)
			return env.SetRedisEnabled(context.WithoutCancel(ctx), true)
		case "pg-down":
			if err := env.PausePostgres(ctx); err != nil {
				return err
			}
			wait(ctx, a.D)
			return env.UnpausePostgres(context.WithoutCancel(ctx))
		}
		return fmt.Errorf("unknown action %q", a.Kind)
	}
}

// restore removes injected faults and brings every container back.
func restore(env *testkit.Compose) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if env.ToxiproxyUp(ctx) {
		_ = env.ResetFaults(ctx)
	}
	_ = env.StartAll(ctx)
	_ = env.WaitHealthy(ctx)
}

func reconciliation(ctx context.Context, env *testkit.Compose) float64 {
	m, err := env.Metrics(ctx, "worker")
	if err != nil {
		return 0
	}
	return m["score_reconciliation_mismatches_total"]
}

func raiseFileLimit() {
	var l syscall.Rlimit
	if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &l) == nil && l.Cur < l.Max {
		l.Cur = l.Max
		_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &l)
	}
}

func ms(s testkit.Summary) map[string]any {
	f := func(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }
	return map[string]any{"count": s.Count, "p50_ms": f(s.P50), "p95_ms": f(s.P95), "p99_ms": f(s.P99), "max_ms": f(s.Max)}
}

func writeReport(ctx context.Context, env *testkit.Compose, dir string, sc *Scenario, overrides []string, started time.Time,
	a Aggregate, checks []Check, pass bool, rooms []*testkit.Result) (string, error) {
	gateways, _ := env.Containers(ctx, "ws", false)
	workers, _ := env.Containers(ctx, "worker", false)
	var codes []string
	for _, r := range rooms {
		codes = append(codes, r.Code)
	}
	report := map[string]any{
		"scenario": sc.Name, "overrides": overrides, "startedAt": started.UTC(), "seconds": time.Since(started).Seconds(), "pass": pass,
		"config": map[string]any{"questionSet": sc.QuestionSet, "rooms": sc.Rooms, "participantsPerRoom": sc.ParticipantsPerRoom,
			"window": sc.Window.String(), "reveal": sc.Reveal.String(), "joinRampUp": sc.JoinRampUp.String(), "roomStagger": sc.RoomStagger.String(),
			"answerWithin": sc.Answers.Within.String(), "correctRatio": sc.Answers.CorrectRatio, "noAnswerRatio": sc.Answers.NoAnswerRatio,
			"slowClients": sc.SlowClients, "chaos": chaosText(sc), "seed": sc.Seed},
		"machine": machine(), "stack": map[string]any{"gateways": len(gateways), "workers": len(workers), "baseURL": env.BaseURL},
		"results": map[string]any{"participants": a.Participants, "joined": a.Joined, "finished": a.Finished,
			"answersSent": a.AnswersSent, "accepted": a.Accepted, "duplicates": a.Duplicates, "reconnects": a.Reconnects, "stale": a.Stale,
			"errors": a.Errors, "closes": a.Closes, "nfr6": ms(a.NFR6), "nfr7": ms(a.NFR7), "nfr8": ms(a.NFR8), "nfr9": ms(a.NFR9),
			"transitionLag": ms(a.TransitionLag), "rejoin": ms(a.Rejoin), "reconciliationMetric": a.ReconciliationMetric,
			"violations": a.Violations, "pointMismatches": a.PointMismatches, "totalMismatches": a.TotalMismatches, "stepErrors": a.StepErrors,
			"quizCodes": codes},
		"checks": checks,
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	name := filepath.Join(dir, fmt.Sprintf("%s-%s.json", sc.Name, started.UTC().Format("20060102-150405Z")))
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return name, os.WriteFile(name, append(b, '\n'), 0o600)
}

func chaosText(sc *Scenario) []string {
	var out []string
	for _, c := range sc.Chaos {
		out = append(out, c.Text)
	}
	return out
}

// machine describes where the run happened, for docs/testing.md.
func machine() map[string]any {
	m := map[string]any{"cpus": runtime.NumCPU(), "goos": runtime.GOOS, "go": runtime.Version()}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "model name") {
				m["cpu"] = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
				break
			}
		}
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		if line, _, ok := strings.Cut(string(b), "\n"); ok {
			m["memory"] = strings.Join(strings.Fields(line)[1:], " ")
		}
	}
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		m["kernel"] = strings.TrimSpace(string(b))
	}
	return m
}
