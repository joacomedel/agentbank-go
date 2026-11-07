package db_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/db"
)

func TestOpenMigrate(t *testing.T) {
	dir := t.TempDir()
	handle, err := db.Open(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer handle.Close()

	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("Migrate segunda vez: %v", err)
	}

	var mode string
	if err := handle.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode=%q", mode)
	}
	var foreignKeys int
	if err := handle.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatal(err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys=%d", foreignKeys)
	}
	var tables int
	if err := handle.QueryRow(`
		SELECT COUNT(1) FROM sqlite_master
		WHERE type='table' AND name IN ('projects','participants','tokens','audit_events')
	`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 4 {
		t.Fatalf("tablas base=%d, want 4", tables)
	}
}
