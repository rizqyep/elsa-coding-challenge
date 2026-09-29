// Package migrations embeds the SQL migrations, so binaries don't depend on files on disk.
package migrations

import (
	"embed"
	"io/fs"
)

var (
	//go:embed *.sql
	schemaFS embed.FS

	//go:embed seed/*.sql
	seedFS embed.FS
)

// Schema returns the schema migrations, applied in every environment.
func Schema() fs.FS { return schemaFS }

// Seed returns the seed data migrations, applied only when APP_ENV=local (TRD §5.5).
func Seed() (fs.FS, error) { return fs.Sub(seedFS, "seed") }
