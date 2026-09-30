// Command api runs the REST API (TRD §6.2).
package main

// AI-assisted: AI-027 (docs/ai-collaboration/log.md).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/auth"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/httpapi"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/leaderboard"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/config"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/health"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/logging"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/metrics"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/postgres"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

const requestTimeout = 5 * time.Second

func main() {
	health.Main(os.Args)
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadAPI(config.FromEnv)
	if err != nil {
		return err
	}
	log := logging.New(os.Stdout, cfg.LogLevel, "api")
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

	ready := health.NewReadiness(func(ctx context.Context) error {
		return errors.Join(redisx.Ping(ctx, rdb, time.Second), postgres.Ping(ctx, pool, time.Second))
	})
	handler, err := newHandler(ctx, cfg, rdb, pool, log, metrics.NewRegistry(), ready)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr: cfg.HTTPAddr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
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
	ready.Shutdown(func() { shutdownErr = srv.Shutdown(shutdownCtx) }) // in-flight requests finish
	if shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed) {
		return fmt.Errorf("shutdown: %w", shutdownErr)
	}
	return nil
}

// newHandler wires the REST API over Redis and PostgreSQL.
func newHandler(ctx context.Context, cfg config.API, rdb redis.UniversalClient, pool *pgxpool.Pool, log *slog.Logger, reg *prometheus.Registry, ready *health.Readiness) (http.Handler, error) {
	if err := redisx.LoadScripts(ctx, rdb, append(quiz.Scripts(), leaderboard.Scripts()...)...); err != nil {
		return nil, err
	}
	quizzes := quiz.NewService(quiz.NewRedisRepository(rdb), quiz.NewPostgresStore(pool), quiz.Settings{
		DefaultWindow: cfg.QuestionWindowDefault, DefaultReveal: cfg.RevealDefault, LobbyTimeout: cfg.LobbyTimeout,
		DataTTL: cfg.QuizDataTTL, CodeAttempts: cfg.QuizCodeMaxAttempts,
	}, log)
	boards := leaderboard.NewService(leaderboard.NewRedisRepository(rdb), leaderboard.NewPostgresStore(pool))
	return httpapi.New(httpapi.Config{
		Quizzes: quizzes, Leaderboards: boards, DevTokens: cfg.DevTokensEnabled,
		Tokens: &auth.Tokens{Key: cfg.AuthSigningKey, TTL: cfg.AuthTokenTTL},
		Ready:  ready.Check,
		Log:    log, Metrics: reg, RequestTimeout: requestTimeout,
	})
}
