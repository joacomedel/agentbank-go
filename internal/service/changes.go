package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// ChangeItem es un objeto cambiado desde un cursor.
type ChangeItem struct {
	ObjectType string         `json:"object_type"`
	ObjectID   string         `json:"object_id"`
	Action     string         `json:"action"`
	ProjectID  string         `json:"project_id,omitempty"`
	ChangedAt  time.Time      `json:"changed_at"`
	Summary    map[string]any `json:"summary,omitempty"`
}

// Changes devuelve un feed incremental y deduplicado de objetos cambiados.
// Una sola llamada alcanza para saber qué tickets/mensajes/docs/fases/épicas
// cambiaron desde el cursor.
func (s *Service) Changes(ctx context.Context, actor Identity, since, projectRef string, limit int) (map[string]any, error) {
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return nil, err
	}
	chainIDs := rights.ProjectIDs()
	if rights.Admin {
		chainIDs = nil
	}
	if strings.TrimSpace(projectRef) != "" {
		project, err := resolveProject(ctx, s.db, projectRef)
		if err != nil {
			return nil, err
		}
		if !rights.CanRead(project.ID) {
			return nil, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "changes", project.ID)
		}
		chainIDs = []string{project.ID}
	}
	if !rights.Admin && len(chainIDs) == 0 {
		return map[string]any{"items": []ChangeItem{}, "next_cursor": since}, nil
	}

	conditions := []string{"1 = 1"}
	args := []any{}
	if len(chainIDs) > 0 {
		placeholders := make([]string, len(chainIDs))
		for i, chainID := range chainIDs {
			placeholders[i] = "?"
			args = append(args, chainID)
		}
		conditions = append(conditions, "chain_id IN ("+strings.Join(placeholders, ",")+")")
	}
	initial := strings.TrimSpace(since) == ""
	if !initial {
		createdAt, id, err := decodeAuditCursor(since)
		if err != nil {
			parsed, parseErr := timefmt.ParseFlexible(since)
			if parseErr != nil {
				return nil, validation("since inválido")
			}
			createdAt = timefmt.Format(parsed)
			id = ""
		}
		if id == "" {
			conditions = append(conditions, "created_at >= ?")
			args = append(args, createdAt)
		} else {
			conditions = append(conditions, "(created_at > ? OR (created_at = ? AND id > ?))")
			args = append(args, createdAt, createdAt, id)
		}
	}
	order := "created_at ASC, id ASC"
	args = append(args, limit)
	if initial {
		order = "created_at DESC, id DESC"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, chain_id, seq, actor_type, COALESCE(actor_id, ''), action,
		       object_type, COALESCE(object_id, ''), COALESCE(project_id, ''),
		       COALESCE(actor_role, ''), COALESCE(session_id, ''), COALESCE(origin, ''),
		       COALESCE(correlation_id, ''), COALESCE(parent_event_id, ''), payload,
		       prev_hash, hash, created_at
		FROM audit_events
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY `+order+`
		LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("listar cambios: %w", err)
	}
	defer rows.Close()
	events := make([]AuditEventView, 0, limit)
	for rows.Next() {
		event, err := scanAuditView(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer cambios: %w", err)
	}
	if initial {
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
	}
	nextCursor := since
	if len(events) > 0 {
		last := events[len(events)-1]
		nextCursor = encodeAuditCursor(timefmt.Format(last.CreatedAt), last.ID)
	}

	// Dedupe por objeto, quedándose con el último cambio.
	byObject := make(map[string]ChangeItem, len(events))
	idsByType := map[string]map[string]struct{}{}
	for _, event := range events {
		if event.ObjectID == "" {
			continue
		}
		key := event.ObjectType + "|" + event.ObjectID
		byObject[key] = ChangeItem{
			ObjectType: event.ObjectType, ObjectID: event.ObjectID,
			Action: event.Action, ProjectID: event.ProjectID,
			ChangedAt: event.CreatedAt,
		}
		if idsByType[event.ObjectType] == nil {
			idsByType[event.ObjectType] = map[string]struct{}{}
		}
		idsByType[event.ObjectType][event.ObjectID] = struct{}{}
	}
	if err := s.enrichChanges(ctx, byObject); err != nil {
		return nil, err
	}
	items := make([]ChangeItem, 0, len(byObject))
	for _, item := range byObject {
		items = append(items, item)
	}
	sortChangeItems(items)
	return map[string]any{"items": items, "next_cursor": nextCursor}, nil
}

func sortChangeItems(items []ChangeItem) {
	// insertion sort estable por ChangedAt; los volúmenes son chicos.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0; j-- {
			left := items[j-1]
			right := items[j]
			if left.ChangedAt.Before(right.ChangedAt) || (left.ChangedAt.Equal(right.ChangedAt) && left.ObjectID <= right.ObjectID) {
				break
			}
			items[j-1], items[j] = right, left
		}
	}
}

func (s *Service) enrichChanges(ctx context.Context, byObject map[string]ChangeItem) error {
	type summaryQuery struct {
		objectType string
		query      string
		scan       func(*sql.Rows) (string, map[string]any, error)
	}
	queries := []summaryQuery{
		{"ticket", `SELECT t.id, t.project_id, p.prefix || '-' || t.number, t.title, t.status, t.version, t.updated_at
		            FROM tickets t JOIN projects p ON p.id = t.project_id WHERE t.id IN (%s)`,
			func(rows *sql.Rows) (string, map[string]any, error) {
				var id, projectID, ref, title, status, updated string
				var version int64
				if err := rows.Scan(&id, &projectID, &ref, &title, &status, &version, &updated); err != nil {
					return "", nil, err
				}
				return id, map[string]any{"project_id": projectID, "ref": ref, "title": title, "status": status, "version": version, "updated_at": updated}, nil
			}},
		{"mensaje", `SELECT id, COALESCE(project_id, ''), subject, type, COALESCE(resolution, ''), created_at FROM messages WHERE id IN (%s)`,
			func(rows *sql.Rows) (string, map[string]any, error) {
				var id, projectID, subject, messageType, resolution, created string
				if err := rows.Scan(&id, &projectID, &subject, &messageType, &resolution, &created); err != nil {
					return "", nil, err
				}
				return id, map[string]any{"project_id": projectID, "subject": subject, "type": messageType, "resolution": resolution, "created_at": created}, nil
			}},
		{"documento", `SELECT id, COALESCE(project_id, ''), title, COALESCE(route, ''), current_version, updated_at FROM documents WHERE id IN (%s)`,
			func(rows *sql.Rows) (string, map[string]any, error) {
				var id, projectID, title, route, updated string
				var current int64
				if err := rows.Scan(&id, &projectID, &title, &route, &current, &updated); err != nil {
					return "", nil, err
				}
				return id, map[string]any{"project_id": projectID, "title": title, "route": route, "current_version": current, "updated_at": updated}, nil
			}},
		{"fase", `SELECT id, project_id, name, status FROM phases WHERE id IN (%s)`,
			func(rows *sql.Rows) (string, map[string]any, error) {
				var id, projectID, name, status string
				if err := rows.Scan(&id, &projectID, &name, &status); err != nil {
					return "", nil, err
				}
				return id, map[string]any{"project_id": projectID, "name": name, "status": status}, nil
			}},
		{"epica", `SELECT id, project_id, title, status FROM epics WHERE id IN (%s)`,
			func(rows *sql.Rows) (string, map[string]any, error) {
				var id, projectID, title, status string
				if err := rows.Scan(&id, &projectID, &title, &status); err != nil {
					return "", nil, err
				}
				return id, map[string]any{"project_id": projectID, "title": title, "status": status}, nil
			}},
		{"decision", `SELECT id, project_id, title, status FROM decisions WHERE id IN (%s)`,
			func(rows *sql.Rows) (string, map[string]any, error) {
				var id, projectID, title, status string
				if err := rows.Scan(&id, &projectID, &title, &status); err != nil {
					return "", nil, err
				}
				return id, map[string]any{"project_id": projectID, "title": title, "status": status}, nil
			}},
		{"participante", `SELECT id, name, type, status FROM participants WHERE id IN (%s)`,
			func(rows *sql.Rows) (string, map[string]any, error) {
				var id, name, participantType, status string
				if err := rows.Scan(&id, &name, &participantType, &status); err != nil {
					return "", nil, err
				}
				return id, map[string]any{"name": name, "type": participantType, "status": status}, nil
			}},
	}
	for _, entry := range queries {
		ids := make([]string, 0, 8)
		for key, item := range byObject {
			if item.ObjectType != entry.objectType {
				continue
			}
			_ = key
			ids = append(ids, item.ObjectID)
		}
		if len(ids) == 0 {
			continue
		}
		placeholders := make([]string, len(ids))
		args := make([]any, len(ids))
		for i, id := range ids {
			placeholders[i] = "?"
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(entry.query, strings.Join(placeholders, ",")), args...)
		if err != nil {
			return fmt.Errorf("enriquecer %s: %w", entry.objectType, err)
		}
		for rows.Next() {
			id, summary, err := entry.scan(rows)
			if err != nil {
				rows.Close()
				return fmt.Errorf("escanear %s: %w", entry.objectType, err)
			}
			key := entry.objectType + "|" + id
			item := byObject[key]
			item.Summary = summary
			byObject[key] = item
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("recorrer %s: %w", entry.objectType, err)
		}
		_ = rows.Close()
	}
	return nil
}
