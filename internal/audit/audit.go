// Package audit implementa la cadena de hashes append-only.
package audit

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// Actor es quién produjo el evento. El ID sale del token, nunca del body.
type Actor struct {
	Type string
	ID   string
}

// Event es un evento ya persistido.
type Event struct {
	ID         string
	ChainID    string
	Seq        int64
	ActorType  string
	ActorID    string
	Action     string
	ObjectType string
	ObjectID   string
	ProjectID  string
	Payload    string
	PrevHash   string
	Hash       string
	CreatedAt  time.Time
}

// Input describe un evento a agregar. Payload se serializa a JSON canónico.
type Input struct {
	ChainID    string
	Actor      Actor
	Action     string
	ObjectType string
	ObjectID   string
	ProjectID  string
	Payload    any
	CreatedAt  time.Time
}

// VerifyResult es el resultado de verificar una cadena.
type VerifyResult struct {
	OK         bool         `json:"ok"`
	ChainID    string       `json:"chain_id"`
	Events     int          `json:"events"`
	FirstError *VerifyError `json:"first_error"`
}

// VerifyError apunta a la primera inconsistencia encontrada.
type VerifyError struct {
	Seq    int64  `json:"seq"`
	Reason string `json:"reason"`
}

const hashSeparator = "\u2016"

// Append agrega un evento dentro de la transacción del llamador. La
// transacción debe haberse abierto en modo IMMEDIATE; con una sola conexión
// SQLite queda serializada.
func Append(ctx context.Context, tx *sql.Tx, input Input) (Event, error) {
	if input.ChainID == "" {
		return Event{}, fmt.Errorf("audit: chain_id vacío")
	}
	if input.Actor.Type == "" {
		return Event{}, fmt.Errorf("audit: actor_type vacío")
	}
	if input.CreatedAt.IsZero() {
		return Event{}, fmt.Errorf("audit: created_at vacío")
	}
	payload, err := canonicalPayload(input.Payload)
	if err != nil {
		return Event{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO audit_chain_heads(chain_id, last_seq, last_hash)
		 VALUES (?, 0, '') ON CONFLICT(chain_id) DO NOTHING`,
		input.ChainID,
	); err != nil {
		return Event{}, fmt.Errorf("audit: crear cabeza de cadena: %w", err)
	}
	var lastSeq int64
	var lastHash string
	if err := tx.QueryRowContext(ctx,
		`SELECT last_seq, last_hash FROM audit_chain_heads WHERE chain_id = ?`,
		input.ChainID,
	).Scan(&lastSeq, &lastHash); err != nil {
		return Event{}, fmt.Errorf("audit: leer cabeza de cadena: %w", err)
	}
	event := Event{
		ID:         id.New(),
		ChainID:    input.ChainID,
		Seq:        lastSeq + 1,
		ActorType:  input.Actor.Type,
		ActorID:    input.Actor.ID,
		Action:     input.Action,
		ObjectType: input.ObjectType,
		ObjectID:   input.ObjectID,
		ProjectID:  input.ProjectID,
		Payload:    payload,
		PrevHash:   lastHash,
		CreatedAt:  input.CreatedAt.UTC(),
	}
	event.Hash = HashEvent(event)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_events
			(id, chain_id, seq, actor_id, actor_type, action, object_type,
			 object_id, project_id, payload, prev_hash, hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.ChainID, event.Seq, nullable(event.ActorID), event.ActorType,
		event.Action, event.ObjectType, nullable(event.ObjectID), nullable(event.ProjectID),
		event.Payload, event.PrevHash, event.Hash, timefmt.Format(event.CreatedAt),
	); err != nil {
		return Event{}, fmt.Errorf("audit: insertar evento: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE audit_chain_heads SET last_seq = ?, last_hash = ? WHERE chain_id = ?`,
		event.Seq, event.Hash, event.ChainID,
	); err != nil {
		return Event{}, fmt.Errorf("audit: actualizar cabeza de cadena: %w", err)
	}
	return event, nil
}

// HashEvent calcula SHA-256(hash_previo ‖ campos canónicos). Los campos llevan
// longitud en base 36 para que no pueda haber ambigüedad entre concatenaciones.
func HashEvent(event Event) string {
	fields := []string{
		field(strconv.FormatInt(event.Seq, 10)),
		field(event.ID),
		field(event.ChainID),
		field(event.ActorType),
		field(event.ActorID),
		field(event.Action),
		field(event.ObjectType),
		field(event.ObjectID),
		field(event.ProjectID),
		field(event.Payload),
		field(timefmt.Format(event.CreatedAt)),
	}
	sum := sha256.Sum256([]byte(event.PrevHash + hashSeparator + strings.Join(fields, "|")))
	return hex.EncodeToString(sum[:])
}

// Verify recorre la cadena en orden y reporta la primera inconsistencia.
func Verify(ctx context.Context, database *sql.DB, chainID string) (VerifyResult, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT id, chain_id, seq, COALESCE(actor_id, ''), actor_type, action,
		       object_type, COALESCE(object_id, ''), COALESCE(project_id, ''),
		       payload, prev_hash, hash, created_at
		FROM audit_events
		WHERE chain_id = ?
		ORDER BY seq ASC`, chainID)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("audit: listar eventos: %w", err)
	}
	defer rows.Close()

	result := VerifyResult{OK: true, ChainID: chainID}
	var previous string
	var expectedSeq int64 = 1
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return VerifyResult{}, err
		}
		result.Events++
		if event.Seq != expectedSeq {
			result.OK = false
			result.FirstError = &VerifyError{Seq: event.Seq, Reason: "seq fuera de orden"}
			return result, nil
		}
		if event.PrevHash != previous {
			result.OK = false
			result.FirstError = &VerifyError{Seq: event.Seq, Reason: "prev_hash no encadena con el evento anterior"}
			return result, nil
		}
		if recomputed := HashEvent(event); recomputed != event.Hash {
			result.OK = false
			result.FirstError = &VerifyError{Seq: event.Seq, Reason: "hash no coincide con el contenido"}
			return result, nil
		}
		previous = event.Hash
		expectedSeq++
	}
	if err := rows.Err(); err != nil {
		return VerifyResult{}, fmt.Errorf("audit: recorrer eventos: %w", err)
	}

	var lastSeq int64
	var lastHash string
	err = database.QueryRowContext(ctx,
		`SELECT last_seq, last_hash FROM audit_chain_heads WHERE chain_id = ?`, chainID,
	).Scan(&lastSeq, &lastHash)
	if err != nil && err != sql.ErrNoRows {
		return VerifyResult{}, fmt.Errorf("audit: leer cabeza: %w", err)
	}
	if err == nil {
		if result.Events == 0 && lastSeq != 0 {
			result.OK = false
			result.FirstError = &VerifyError{Seq: 0, Reason: "la cabeza apunta a eventos inexistentes"}
			return result, nil
		}
		if result.Events > 0 && (lastSeq != int64(result.Events) || lastHash != previous) {
			result.OK = false
			result.FirstError = &VerifyError{Seq: lastSeq, Reason: "cabeza de cadena desincronizada"}
			return result, nil
		}
	}
	return result, nil
}

// ChainExists indica si hay una cabeza de cadena registrada.
func ChainExists(ctx context.Context, database *sql.DB, chainID string) (bool, error) {
	var count int
	if err := database.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM audit_chain_heads WHERE chain_id = ?`, chainID,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("audit: consultar cadena: %w", err)
	}
	return count > 0, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(row scanner) (Event, error) {
	var event Event
	var createdAt string
	if err := row.Scan(
		&event.ID, &event.ChainID, &event.Seq, &event.ActorID, &event.ActorType,
		&event.Action, &event.ObjectType, &event.ObjectID, &event.ProjectID,
		&event.Payload, &event.PrevHash, &event.Hash, &createdAt,
	); err != nil {
		return Event{}, fmt.Errorf("audit: escanear evento: %w", err)
	}
	parsed, err := timefmt.Parse(createdAt)
	if err != nil {
		return Event{}, fmt.Errorf("audit: created_at inválido: %w", err)
	}
	event.CreatedAt = parsed
	return event, nil
}

func canonicalPayload(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("audit: serializar payload: %w", err)
	}
	if string(encoded) == "null" {
		return "{}", nil
	}
	return string(encoded), nil
}

func field(value string) string {
	return strconv.FormatInt(int64(len(value)), 36) + ":" + value
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
