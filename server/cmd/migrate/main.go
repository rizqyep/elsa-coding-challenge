// Command migrate applies the database schema and, when APP_ENV=local, the seed data.
package main

// AI-assisted: AI-017 (docs/ai-collaboration/log.md).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/rizqyep/rizqyep-elsa-assignment/server/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("migrate failed", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		return errors.New("POSTGRES_DSN is required")
	}
	seed := os.Getenv("APP_ENV") == "local"

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			logger.Warn("close database", "err", cerr)
		}
	}()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	results, err := migrations.Apply(ctx, db, seed)
	for _, r := range results {
		logger.Info("migration applied", "set", r.Set, "version", r.Version, "file", r.File, "duration", r.Duration.String())
	}
	if err != nil {
		return err
	}
	logger.Info("migrations up to date", "applied_now", len(results), "seed", seed)
	return nil
}
