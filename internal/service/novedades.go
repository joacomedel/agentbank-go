package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// Novedades devuelve eventos posteriores a un cursor; si wait > 0, hace
// long-poll hasta que aparezcan o venza el plazo (máximo 30 s).
func (s *Service) Novedades(ctx context.Context, actor Identity, since string, waitSeconds int) (map[string]any, error) {
	if waitSeconds < 0 {
		waitSeconds = 0
	}
	if waitSeconds > 30 {
		waitSeconds = 30
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	chainIDs := rights.ProjectIDs()
	if rights.Admin {
		chainIDs = nil
	}
	if !rights.Admin && len(chainIDs) == 0 {
		return map[string]any{"items": []AuditEventView{}, "next_cursor": since, "waited": false}, nil
	}
	createdAt, id := parseNovedadesCursor(since, s.now().UTC())
	deadline := s.now().Add(time.Duration(waitSeconds) * time.Second)
	for {
		items, next, err := s.fetchNovedades(ctx, chainIDs, createdAt, id)
		if err != nil {
			return nil, err
		}
		if len(items) > 0 || waitSeconds == 0 || s.now().After(deadline) {
			waited := len(items) == 0 && waitSeconds > 0
			return map[string]any{"items": items, "next_cursor": next, "waited": waited}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *Service) fetchNovedades(ctx context.Context, chainIDs []string, createdAt, id string) ([]AuditEventView, string, error) {
	conditions := []string{"(created_at > ? OR (created_at = ? AND id > ?))"}
	args := []any{createdAt, createdAt, id}
	if len(chainIDs) > 0 {
		placeholders := make([]string, len(chainIDs))
		for i, chainID := range chainIDs {
			placeholders[i] = "?"
			args = append(args, chainID)
		}
		conditions = append(conditions, "chain_id IN ("+strings.Join(placeholders, ",")+")")
	}
	args = append(args, 100)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, chain_id, seq, actor_type, COALESCE(actor_id, ''), action,
		       object_type, COALESCE(object_id, ''), COALESCE(project_id, ''),
		       COALESCE(actor_role, ''), COALESCE(session_id, ''), COALESCE(origin, ''),
		       COALESCE(correlation_id, ''), COALESCE(parent_event_id, ''), payload,
		       prev_hash, hash, created_at
		FROM audit_events
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY created_at ASC, id ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar novedades: %w", err)
	}
	defer rows.Close()
	items := make([]AuditEventView, 0, 16)
	for rows.Next() {
		event, err := scanAuditView(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer novedades: %w", err)
	}
	next := encodeAuditCursor(createdAt, id)
	if len(items) > 0 {
		last := items[len(items)-1]
		next = encodeAuditCursor(timefmt.Format(last.CreatedAt), last.ID)
	}
	return items, next, nil
}

func parseNovedadesCursor(cursor string, now time.Time) (string, string) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return timefmt.Format(now), ""
	}
	if createdAt, id, err := decodeAuditCursor(cursor); err == nil {
		return createdAt, id
	}
	if parsed, err := timefmt.ParseFlexible(cursor); err == nil {
		return timefmt.Format(parsed), ""
	}
	return timefmt.Format(now), ""
}
