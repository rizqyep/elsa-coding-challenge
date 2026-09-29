// Command migrate applies the database schema and, when APP_ENV=local, the seed data.
// It runs as a one-shot service before the API, gateways, and workers start.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/migrations"
)

const (
	schemaTable = "goose_db_version"
	seedTable   = "goose_seed_version"
	timeout     = 60 * time.Second
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
	appEnv := os.Getenv("APP_ENV")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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

	if err := apply(ctx, logger, db, "schema", migrations.Schema(), schemaTable); err != nil {
		return err
	}
	if appEnv != "local" {
		logger.Info("seed skipped", "app_env", appEnv)
		return nil
	}
	seed, err := migrations.Seed()
	if err != nil {
		return fmt.Errorf("load seed migrations: %w", err)
	}
	return apply(ctx, logger, db, "seed", seed, seedTable)
}

func apply(ctx context.Context, logger *slog.Logger, db *sql.DB, name string, fsys fs.FS, table string) error {
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithTableName(table))
	if err != nil {
		return fmt.Errorf("%s: create provider: %w", name, err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("%s: apply migrations: %w", name, err)
	}
	for _, r := range results {
		logger.Info("migration applied", "set", name, "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration.String())
	}
	logger.Info("migrations up to date", "set", name, "applied_now", len(results))
	return nil
}
