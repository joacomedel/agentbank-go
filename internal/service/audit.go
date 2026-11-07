package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// ChainHead es la cabeza visible de una cadena.
type ChainHead struct {
	ChainID  string `json:"chain_id"`
	LastSeq  int64  `json:"last_seq"`
	LastHash string `json:"last_hash"`
}

// AuditEventView es un evento expuesto por la API.
type AuditEventView struct {
	ID         string    `json:"id"`
	ChainID    string    `json:"chain_id"`
	Seq        int64     `json:"seq"`
	ActorType  string    `json:"actor_type"`
	ActorID    string    `json:"actor_id,omitempty"`
	Action     string    `json:"action"`
	ObjectType string    `json:"object_type"`
	ObjectID   string    `json:"object_id,omitempty"`
	ProjectID  string    `json:"project_id,omitempty"`
	Payload    string    `json:"-"`
	PayloadRaw any       `json:"payload"`
	PrevHash   string    `json:"prev_hash"`
	Hash       string    `json:"hash"`
	CreatedAt  time.Time `json:"created_at"`
}

// AuditFilter filtra /v1/audit.
type AuditFilter struct {
	ChainID    string
	ActorID    string
	Action     string
	ObjectType string
	ObjectID   string
	ProjectID  string
	From       string
	To         string
}

// ListChains lista las cadenas legibles por el actor.
func (s *Service) ListChains(ctx context.Context, actor Identity, limit int, cursor string) ([]ChainHead, string, error) {
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, "", err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return nil, "", err
	}
	args := []any{}
	where := "WHERE 1 = 1"
	if !rights.Admin {
		ids := rights.ProjectIDs()
		if len(ids) == 0 {
			return []ChainHead{}, "", nil
		}
		placeholders := make([]string, len(ids))
		for i, id := range ids {
			placeholders[i] = "?"
			args = append(args, id)
		}
		where += " AND chain_id IN (" + strings.Join(placeholders, ",") + ")"
	}
	if cursor != "" {
		where += " AND chain_id > ?"
		args = append(args, cursor)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT chain_id, last_seq, last_hash
		FROM audit_chain_heads `+where+`
		ORDER BY chain_id ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar cadenas: %w", err)
	}
	defer rows.Close()
	heads := make([]ChainHead, 0, limit)
	for rows.Next() {
		var head ChainHead
		if err := rows.Scan(&head.ChainID, &head.LastSeq, &head.LastHash); err != nil {
			return nil, "", fmt.Errorf("escanear cadena: %w", err)
		}
		heads = append(heads, head)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer cadenas: %w", err)
	}
	next := ""
	if len(heads) > limit {
		heads = heads[:limit]
		next = heads[len(heads)-1].ChainID
	}
	return heads, next, nil
}

// ListAudit lista eventos con control de acceso por cadena.
func (s *Service) ListAudit(ctx context.Context, actor Identity, filter AuditFilter, limit int, cursor string) ([]AuditEventView, string, error) {
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, "", err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return nil, "", err
	}
	if filter.ChainID != "" && !rights.Admin && !rights.CanRead(filter.ChainID) {
		return nil, "", forbidden("requires_audit_access", "sin permiso para leer esa cadena", "", "audit.listar", filter.ChainID)
	}

	conditions := []string{"1 = 1"}
	args := []any{}
	add := func(condition string, value any) {
		conditions = append(conditions, condition)
		args = append(args, value)
	}
	if filter.ChainID != "" {
		add("chain_id = ?", filter.ChainID)
	} else if !rights.Admin {
		ids := rights.ProjectIDs()
		if len(ids) == 0 {
			return []AuditEventView{}, "", nil
		}
		placeholders := make([]string, len(ids))
		for i, id := range ids {
			placeholders[i] = "?"
			args = append(args, id)
		}
		conditions = append(conditions, "chain_id IN ("+strings.Join(placeholders, ",")+")")
	}
	if filter.ActorID != "" {
		add("actor_id = ?", filter.ActorID)
	}
	if filter.Action != "" {
		add("action = ?", filter.Action)
	}
	if filter.ObjectType != "" {
		add("object_type = ?", filter.ObjectType)
	}
	if filter.ObjectID != "" {
		add("object_id = ?", filter.ObjectID)
	}
	if filter.ProjectID != "" {
		add("project_id = ?", filter.ProjectID)
	}
	if filter.From != "" {
		from, err := timefmt.ParseFlexible(filter.From)
		if err != nil {
			return nil, "", validation(err.Error())
		}
		add("created_at >= ?", timefmt.Format(from))
	}
	if filter.To != "" {
		to, err := timefmt.ParseFlexible(filter.To)
		if err != nil {
			return nil, "", validation(err.Error())
		}
		add("created_at <= ?", timefmt.Format(to))
	}
	if cursor != "" {
		createdAt, id, err := decodeAuditCursor(cursor)
		if err != nil {
			return nil, "", validation("cursor inválido")
		}
		conditions = append(conditions, "(created_at < ? OR (created_at = ? AND id < ?))")
		args = append(args, createdAt, createdAt, id)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, chain_id, seq, actor_type, COALESCE(actor_id, ''), action,
		       object_type, COALESCE(object_id, ''), COALESCE(project_id, ''),
		       payload, prev_hash, hash, created_at
		FROM audit_events
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar auditoría: %w", err)
	}
	defer rows.Close()
	events := make([]AuditEventView, 0, limit)
	for rows.Next() {
		event, err := scanAuditView(rows)
		if err != nil {
			return nil, "", err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer auditoría: %w", err)
	}
	next := ""
	if len(events) > limit {
		events = events[:limit]
		last := events[len(events)-1]
		next = encodeAuditCursor(timefmt.Format(last.CreatedAt), last.ID)
	}
	return events, next, nil
}

// VerifyChain verifica la integridad de una cadena.
func (s *Service) VerifyChain(ctx context.Context, actor Identity, chainID string) (audit.VerifyResult, error) {
	chainID = strings.TrimSpace(chainID)
	if chainID == "" {
		return audit.VerifyResult{}, validation("falta chain_id")
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return audit.VerifyResult{}, err
	}
	if !rights.Admin && (chainID == domain.SystemChain || !rights.CanRead(chainID)) {
		return audit.VerifyResult{}, forbidden("requires_audit_access", "sin permiso para verificar esa cadena", "", "audit.verificar", chainID)
	}
	exists, err := audit.ChainExists(ctx, s.db, chainID)
	if err != nil {
		return audit.VerifyResult{}, err
	}
	if !exists {
		return audit.VerifyResult{}, notFound("chain_not_found", "cadena no encontrada")
	}
	return audit.Verify(ctx, s.db, chainID)
}

func scanAuditView(row interface{ Scan(...any) error }) (AuditEventView, error) {
	var event AuditEventView
	var createdAt string
	if err := row.Scan(&event.ID, &event.ChainID, &event.Seq, &event.ActorType,
		&event.ActorID, &event.Action, &event.ObjectType, &event.ObjectID,
		&event.ProjectID, &event.Payload, &event.PrevHash, &event.Hash, &createdAt); err != nil {
		return AuditEventView{}, fmt.Errorf("escanear evento de auditoría: %w", err)
	}
	if event.Payload == "" {
		event.Payload = "{}"
	}
	if err := json.Unmarshal([]byte(event.Payload), &event.PayloadRaw); err != nil {
		event.PayloadRaw = event.Payload
	}
	parsed, err := timefmt.Parse(createdAt)
	if err != nil {
		return AuditEventView{}, fmt.Errorf("created_at de auditoría inválido: %w", err)
	}
	event.CreatedAt = parsed
	return event, nil
}

func encodeAuditCursor(createdAt, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(createdAt + "|" + id))
}

func decodeAuditCursor(cursor string) (string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("cursor inválido")
	}
	return parts[0], parts[1], nil
}
