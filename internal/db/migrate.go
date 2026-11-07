package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate aplica las migraciones embebidas en orden. Es idempotente.
func Migrate(ctx context.Context, handle *sql.DB) error {
	if _, err := handle.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("crear schema_migrations: %w", err)
	}
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("leer migraciones: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version := entry.Name()
		var applied int
		if err := handle.QueryRowContext(ctx,
			"SELECT COUNT(1) FROM schema_migrations WHERE version = ?", version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("consultar migración %s: %w", version, err)
		}
		if applied > 0 {
			continue
		}
		raw, err := migrationFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("leer migración %s: %w", version, err)
		}
		tx, err := handle.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin %s: %w", version, err)
		}
		for _, statement := range splitSQL(string(raw)) {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("migración %s: %w", version, err)
			}
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)",
			version, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("registrar migración %s: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", version, err)
		}
	}
	return nil
}

// splitSQL separa statements por ';' y descarta comentarios de línea.
// Las migraciones del proyecto no usan ';' dentro de strings.
func splitSQL(raw string) []string {
	var clean strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		if index := strings.Index(line, "--"); index >= 0 {
			line = line[:index]
		}
		clean.WriteString(line)
		clean.WriteByte('\n')
	}
	var statements []string
	for _, part := range strings.Split(clean.String(), ";") {
		if statement := strings.TrimSpace(part); statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}
