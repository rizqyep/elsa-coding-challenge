// Command worker runs the quiz clock, leaderboard ticks, and persistence jobs (TRD §8).
package main

// AI-assisted: AI-032, AI-036 (docs/ai-collaboration/log.md).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/history"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/config"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/health"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/logging"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/metrics"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/postgres"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scheduler"
)

// TRD §4.5 and §7.3.
const (
	finalRetryDelay  = time.Second
	clockSyncEvery   = 30 * time.Second
	readinessTimeout = time.Second
)

func main() {
	health.Main(os.Args)
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadWorker(config.FromEnv)
	if err != nil {
		return err
	}
	log := logging.New(os.Stdout, cfg.LogLevel, "worker")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	rdb := redisx.NewClient(cfg.RedisAddr, cfg.RedisPassword)
	defer func() { _ = rdb.Close() }()
	if err := redisx.Ping(ctx, rdb, 5*time.Second); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	pool, err := postgres.NewPool(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	w, err := newWorker(ctx, cfg, rdb, pool, log, metrics.NewRegistry())
	if err != nil {
		return err
	}
	loopCtx, stopLoops := context.WithCancel(context.Background())
	loops := w.start(loopCtx)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: w.handler, ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.HTTPAddr)

	select {
	case err = <-serveErr:
		err = fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	// Stop claiming; claimed work finishes. A job still claimed at exit comes back after its visibility timeout (TRD §8.4).
	log.Info("shutting down")
	w.ready.Shutdown(
		func() {
			stopLoops()
			drained := make(chan struct{})
			go func() { loops.Wait(); close(drained) }()
			select {
			case <-drained:
			case <-time.After(cfg.ShutdownTimeout):
				log.Warn("in-flight work still running at the shutdown timeout")
			}
		},
		func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), readinessTimeout)
			defer cancel()
			_ = srv.Shutdown(shutdownCtx)
		},
	)
	return err
}

// worker is the wired service: its loops and its HTTP handler.
type worker struct {
	handler http.Handler
	ready   *health.Readiness
	runner  *scheduler.Runner
	clock   *redisx.Clock
	log     *slog.Logger
}

// newWorker wires the loops over Redis and PostgreSQL.
func newWorker(ctx context.Context, cfg config.Worker, rdb redis.UniversalClient, pool *pgxpool.Pool, log *slog.Logger, reg *prometheus.Registry) (*worker, error) {
	scripts := append(append(quiz.Scripts(), leaderboard.Scripts()...), history.Scripts()...)
	if err := redisx.LoadScripts(ctx, rdb, scripts...); err != nil {
		return nil, err
	}
	clock := redisx.ClockFor(rdb)
	if err := clock.Sync(ctx); err != nil {
		return nil, fmt.Errorf("redis time: %w", err)
	}
	mismatches := prometheus.NewCounter(prometheus.CounterOpts{Name: "score_reconciliation_mismatches_total",
		Help: "Participants whose live total differed from the total recomputed from saved answers (FR-35)."})
	transitions := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "transitions_total",
		Help: "Quiz transitions this worker applied, by the status entered."}, []string{"to"})
	for _, st := range []quiz.Status{quiz.StatusQuestionOpen, quiz.StatusQuestionClosed, quiz.StatusFinished, quiz.StatusExpired} {
		transitions.WithLabelValues(string(st))
	}
	reg.MustRegister(mismatches, transitions)
	live := history.NewRedisLiveStore(rdb)
	jobs := history.NewService(live, history.NewPostgresStore(pool), history.Options{
		TTL: cfg.QuizDataTTL, FinalRetryDelay: finalRetryDelay,
		OnMismatch: func(code quiz.Code, m []history.Mismatch) {
			mismatches.Add(float64(len(m)))
			log.Error("final totals differ from saved answers", slog.String("quiz_code", string(code)), slog.Any("mismatches", m))
		},
	})
	runner := scheduler.NewRunner(log, scheduler.Loops(scheduler.Deps{
		Transitions: countedTransitions{quiz.NewRedisRepository(rdb), transitions}, Leaderboards: leaderboard.NewRedisRepository(rdb),
		Jobs: live, Processor: jobs, NowMs: clock.NowMs,
	}, scheduler.Settings{
		TransitionPoll: cfg.TransitionPoll, LeaderboardTick: cfg.LeaderboardTick, FlushPoll: cfg.FlushPoll,
		ClaimBatch: cfg.ClaimBatch, FlushVisibility: cfg.FlushVisibilityTimeout,
	})...)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if err := runner.Healthy(); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	ready := health.NewReadiness(func(ctx context.Context) error {
		return errors.Join(redisx.Ping(ctx, rdb, readinessTimeout), postgres.Ping(ctx, pool, readinessTimeout))
	})
	mux.Handle("GET /readyz", ready.Handler())
	mux.Handle("GET /metrics", metrics.Handler(reg))
	return &worker{handler: mux, ready: ready, runner: runner, clock: clock, log: log}, nil
}

// start runs the loops and the clock sync until ctx ends; Wait returns once in-flight work is done.
func (w *worker) start(ctx context.Context) *sync.WaitGroup {
	var wg sync.WaitGroup
	wg.Go(func() { w.runner.Run(ctx) })
	wg.Go(func() {
		w.clock.Run(ctx, clockSyncEvery, func(err error) { w.log.Warn("redis time sync failed", slog.Any("error", err)) })
	})
	return &wg
}

// countedTransitions counts applied transitions by the status they entered.
type countedTransitions struct {
	*quiz.RedisRepository
	n *prometheus.CounterVec
}

func (c countedTransitions) ApplyTransition(ctx context.Context, code quiz.Code) (quiz.TransitionResult, error) {
	res, err := c.RedisRepository.ApplyTransition(ctx, code)
	if err == nil && res.Outcome == quiz.Applied {
		c.n.WithLabelValues(string(res.Status)).Inc()
	}
	return res, err
}
