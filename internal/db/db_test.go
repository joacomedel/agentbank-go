package db_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/db"
	"github.com/joacomedel/agentbank-go/internal/id"
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

func TestOnlyOneActiveTokenPerParticipant(t *testing.T) {
	dir := t.TempDir()
	handle, err := db.Open(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer handle.Close()
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	participantID := id.New()
	if _, err := handle.Exec(`
		INSERT INTO participants(id, type, name, admin, created_at)
		VALUES (?, 'agente', 'agente-token', 0, '2026-11-07T20:00:00.000000000Z')`,
		participantID,
	); err != nil {
		t.Fatalf("insertar participante: %v", err)
	}
	if _, err := handle.Exec(`
		INSERT INTO tokens(id, participant_id, token_hash, created_at)
		VALUES (?, ?, 'hash-uno', '2026-11-07T20:00:00.000000000Z')`,
		id.New(), participantID,
	); err != nil {
		t.Fatalf("insertar token uno: %v", err)
	}
	if _, err := handle.Exec(`
		INSERT INTO tokens(id, participant_id, token_hash, created_at)
		VALUES (?, ?, 'hash-dos', '2026-11-07T20:00:01.000000000Z')`,
		id.New(), participantID,
	); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("se permitieron dos tokens activos: %v", err)
	}
	if _, err := handle.Exec(`
		UPDATE tokens SET revoked_at = '2026-11-07T20:00:02.000000000Z'
		WHERE participant_id = ?`, participantID,
	); err != nil {
		t.Fatalf("revocar token: %v", err)
	}
	if _, err := handle.Exec(`
		INSERT INTO tokens(id, participant_id, token_hash, created_at)
		VALUES (?, ?, 'hash-tres', '2026-11-07T20:00:03.000000000Z')`,
		id.New(), participantID,
	); err != nil {
		t.Fatalf("insertar token después de revocar: %v", err)
	}
}

func TestRevisionMigrationColumnsAndCommentType(t *testing.T) {
	dir := t.TempDir()
	handle, err := db.Open(context.Background(), filepath.Join(dir, "revision.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer handle.Close()
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	columns := map[string]bool{}
	rows, err := handle.Query(`PRAGMA table_info(tickets)`)
	if err != nil {
		t.Fatalf("table_info: %v", err)
	}
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("cerrar rows: %v", err)
	}
	for _, name := range []string{"requiere_revision", "commit_sha", "reaperturas"} {
		if !columns[name] {
			t.Fatalf("falta la columna %s", name)
		}
	}

	var schema string
	if err := handle.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'comments'`,
	).Scan(&schema); err != nil {
		t.Fatalf("schema comments: %v", err)
	}
	if !strings.Contains(schema, "revisión") {
		t.Fatalf("comments no admite el tipo revisión: %s", schema)
	}

	// Migrar de nuevo sobre la base existente debe ser idempotente.
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("Migrate segunda vez: %v", err)
	}
}
