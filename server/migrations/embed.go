// Package migrations holds the embedded SQL migrations and applies them (TRD §5.5).
package migrations

// AI-assisted: AI-017 (docs/ai-collaboration/log.md).

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"time"

	"github.com/pressly/goose/v3"
)

var (
	//go:embed *.sql
	schemaFS embed.FS

	//go:embed seed/*.sql
	seedFS embed.FS
)

// Version tables: schema and seed are tracked separately so seed can be skipped outside local.
const (
	SchemaTable = "goose_db_version"
	SeedTable   = "goose_seed_version"
)

// Result is one migration applied by Apply.
type Result struct {
	Set      string // "schema" or "seed"
	Version  int64
	File     string
	Duration time.Duration
}

// Apply runs pending schema migrations and, when seed is true, pending seed data.
func Apply(ctx context.Context, db *sql.DB, seed bool) ([]Result, error) {
	results, err := up(ctx, db, "schema", schemaFS, SchemaTable)
	if err != nil || !seed {
		return results, err
	}
	sfs, err := fs.Sub(seedFS, "seed")
	if err != nil {
		return results, fmt.Errorf("seed: %w", err)
	}
	more, err := up(ctx, db, "seed", sfs, SeedTable)
	return append(results, more...), err
}

func up(ctx context.Context, db *sql.DB, set string, fsys fs.FS, table string) ([]Result, error) {
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithTableName(table))
	if err != nil {
		return nil, fmt.Errorf("%s: create provider: %w", set, err)
	}
	applied, err := p.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: apply migrations: %w", set, err)
	}
	out := make([]Result, 0, len(applied))
	for _, r := range applied {
		out = append(out, Result{Set: set, Version: r.Source.Version, File: r.Source.Path, Duration: r.Duration})
	}
	return out, nil
}
