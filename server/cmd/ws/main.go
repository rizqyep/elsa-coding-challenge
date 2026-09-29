// Command ws runs the WebSocket gateway (TRD §7).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/config"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/health"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/logging"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/metrics"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/postgres"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/realtime"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

// TRD §9.2 and §7.3.
const (
	answerTimeout    = 250 * time.Millisecond
	setLoadAttempt   = 2 * time.Second
	clockSyncEvery   = 30 * time.Second
	readinessTimeout = time.Second
	drainShare       = 3 // sockets close across the first third of SHUTDOWN_TIMEOUT (TRD §7.9)
)

// Histogram buckets around the latency targets (non-functional §2).
var (
	answerBuckets = []float64{.002, .005, .01, .025, .05, .1, .25, .5, 1}
	fanoutBuckets = []float64{.0005, .001, .005, .01, .025, .05, .1, .25, .5}
)

func main() {
	health.Main(os.Args)
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadGateway(config.FromEnv)
	if err != nil {
		return err
	}
	log := logging.New(os.Stdout, cfg.LogLevel, "ws")
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

	gw, err := newGateway(ctx, cfg, rdb, pool, log, metrics.NewRegistry())
	if err != nil {
		return err
	}
	bgCtx, stopBackground := context.WithCancel(context.Background())
	background := gw.start(bgCtx)
	defer func() { stopBackground(); background.Wait() }()

	// No read or write timeouts: upgraded connections keep their own deadlines (TRD §7.2).
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: gw.handler, ReadHeaderTimeout: 5 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	log.Info("listening", "addr", cfg.HTTPAddr)

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	var shutdownErr error
	gw.ready.Shutdown(
		func() {
			if left := gw.ws.Drain(shutdownCtx, cfg.ShutdownTimeout/drainShare); left > 0 {
				log.Warn("connections still open after the drain", "open", left)
			}
		},
		func() { shutdownErr = srv.Shutdown(shutdownCtx) },
	)
	if shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", shutdownErr)
	}
	return nil
}

// gateway is the wired service: its HTTP handler and the loops that run beside it.
type gateway struct {
	handler  http.Handler
	ws       *realtime.Gateway
	ready    *health.Readiness
	sub      *realtime.Subscriber
	hub      *realtime.Hub
	sessions *session.RedisRepository
	clock    *redisx.Clock
	presence time.Duration
	log      *slog.Logger
}

// newGateway wires the gateway over Redis and PostgreSQL.
func newGateway(ctx context.Context, cfg config.Gateway, rdb redis.UniversalClient, pool *pgxpool.Pool, log *slog.Logger, reg *prometheus.Registry) (*gateway, error) {
	if err := redisx.LoadScripts(ctx, rdb, append(session.Scripts(), scoring.Scripts()...)...); err != nil {
		return nil, err
	}
	cache := quiz.NewCache(quiz.NewPostgresStore(pool), quiz.CacheOptions{MaxSets: cfg.QuestionCacheMaxSets,
		Retry: retry.New(cfg.DBRetry, nil), AttemptTimeout: setLoadAttempt, Log: log})
	sessions := session.NewRedisRepository(rdb)
	fanout := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "broadcast_fanout_seconds",
		Help: "Time to queue one room event for every local connection (NFR-6, NFR-8).", Buckets: fanoutBuckets})
	answerSeconds := prometheus.NewHistogram(prometheus.HistogramOpts{Name: "answer_duration_seconds",
		Help: "Time to record one answer in Redis, WAIT included (NFR-7).", Buckets: answerBuckets})
	reg.MustRegister(fanout, answerSeconds)
	hub := realtime.NewHub(cache, sessions, realtime.HubOptions{Shards: cfg.RegistryShards, Log: log,
		OnBroadcast: func(d time.Duration) { fanout.Observe(d.Seconds()) }})
	sub := realtime.NewSubscriber(rdb, hub, realtime.SubscriberOptions{Log: log})
	hub.Attach(sub)

	clock := redisx.ClockFor(rdb)
	if err := clock.Sync(ctx); err != nil {
		return nil, fmt.Errorf("redis time: %w", err)
	}
	answers := scoring.NewService(scoring.NewRedisRepository(rdb, scoring.RedisOptions{OnlineWindow: cfg.PresenceTTL,
		TTL: cfg.QuizDataTTL, WaitReplicas: cfg.RedisWaitReplicas, WaitTimeout: cfg.RedisWaitTimeout}), answerTimeout)
	timed := timedAnswers{answers, answerSeconds}
	handler := realtime.NewMessageHandler(realtime.HandlerDeps{Hub: hub, Joiner: sessions, Rooms: quiz.NewRedisRepository(rdb),
		Answers: timed, Finals: leaderboard.NewPostgresStore(pool), Clock: clock, DataTTL: cfg.QuizDataTTL})

	errorsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "errors_total", Help: "Errors sent to clients, by code (TRD §9.6)."},
		[]string{"service", "code"})
	reg.MustRegister(errorsTotal)
	opts := realtime.OptionsFrom(cfg)
	opts.OnError = func(code protocol.ErrorCode) { errorsTotal.WithLabelValues("ws", string(code)).Inc() }
	ws := realtime.New(opts, &auth.Tokens{Key: cfg.AuthSigningKey}, handler, log)
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "ws_connections", Help: "Open WebSocket connections on this gateway."},
		func() float64 { return float64(ws.Active()) }))

	mux := http.NewServeMux()
	mux.Handle("GET /ws", ws)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	ready := health.NewReadiness(func(ctx context.Context) error {
		return errors.Join(redisx.Ping(ctx, rdb, readinessTimeout), postgres.Ping(ctx, pool, readinessTimeout))
	})
	mux.Handle("GET /readyz", ready.Handler())
	mux.Handle("GET /metrics", metrics.Handler(reg))
	if cfg.AppEnv == "local" { // profiles for the load runs; the port isn't published outside the stack
		mux.HandleFunc("GET /debug/pprof/", pprof.Index)
		mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	}
	return &gateway{handler: mux, ws: ws, ready: ready, sub: sub, hub: hub, sessions: sessions, clock: clock, presence: cfg.PresenceRefresh, log: log}, nil
}

// start runs the subscriber, presence refresh, and clock sync until ctx ends; Wait blocks until they stop.
func (g *gateway) start(ctx context.Context) *sync.WaitGroup {
	var wg sync.WaitGroup
	wg.Go(func() { g.sub.Run(ctx) })
	wg.Go(func() { g.hub.RunPresence(ctx, g.sessions, g.presence) })
	wg.Go(func() {
		g.clock.Run(ctx, clockSyncEvery, func(err error) { g.log.Warn("redis time sync failed", slog.Any("error", err)) })
	})
	return &wg
}

// timedAnswers observes how long each answer takes to record.
type timedAnswers struct {
	inner realtime.AnswerSubmitter
	h     prometheus.Histogram
}

func (t timedAnswers) SubmitAnswer(ctx context.Context, in scoring.AnswerInput) (scoring.AnswerResult, error) {
	start := time.Now()
	res, err := t.inner.SubmitAnswer(ctx, in)
	t.h.Observe(time.Since(start).Seconds())
	return res, err
}
