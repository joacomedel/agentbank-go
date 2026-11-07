package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Open abre SQLite con WAL y un único writer. El servidor es de baja
// concurrencia; una conexión evita sorpresas con SQLite.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("AGENTBANK_DB vacío")
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("crear directorio de base: %w", err)
		}
	}
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(ON)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=cache_size(-4096)" +
		"&_pragma=temp_store(MEMORY)"
	handle, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir sqlite: %w", err)
	}
	handle.SetMaxOpenConns(1)
	handle.SetMaxIdleConns(1)
	handle.SetConnMaxLifetime(0)
	if err := handle.PingContext(ctx); err != nil {
		handle.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	var mode string
	if err := handle.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		handle.Close()
		return nil, fmt.Errorf("leer journal_mode: %w", err)
	}
	if strings.ToLower(mode) != "wal" {
		handle.Close()
		return nil, fmt.Errorf("journal_mode=%q, se esperaba wal", mode)
	}
	var foreignKeys int
	if err := handle.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		handle.Close()
		return nil, fmt.Errorf("leer foreign_keys: %w", err)
	}
	if foreignKeys != 1 {
		handle.Close()
		return nil, fmt.Errorf("foreign_keys=%d, se esperaba 1", foreignKeys)
	}
	return handle, nil
}
