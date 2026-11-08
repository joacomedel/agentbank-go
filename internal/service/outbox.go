package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// WebhookInput es el alta de un webhook.
type WebhookInput struct {
	URL    string
	Secret string
	Events []string
}

// WebhookView es un webhook registrado.
type WebhookView struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	URL       string    `json:"url"`
	Events    []string  `json:"events"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// OutboxItem es una entrega pendiente o histórica.
type OutboxItem struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Destination   string    `json:"destination"`
	Payload       any       `json:"payload"`
	Attempts      int       `json:"attempts"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	Status        string    `json:"status"`
	LastError     string    `json:"last_error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// CreateWebhook registra un webhook de proyecto.
func (s *Service) CreateWebhook(ctx context.Context, actor Identity, projectRef string, input WebhookInput) (WebhookView, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return WebhookView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return WebhookView{}, err
	}
	if !rights.CanAdmin(project.ID) {
		return WebhookView{}, forbidden("requires_project_admin", "requiere admin del proyecto", project.ID, "webhook.crear", project.ID)
	}
	rawURL := strings.TrimSpace(input.URL)
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return WebhookView{}, validation("url inválida: usá http(s)://host/path")
	}
	events := input.Events
	if len(events) == 0 {
		events = []string{"*"}
	}
	eventsJSON, _ := json.Marshal(events)
	now := s.now().UTC()
	webhookID := id.New()
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO webhooks(id, project_id, url, secret, events, active, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
			webhookID, project.ID, rawURL, nullable(input.Secret), string(eventsJSON),
			actor.Participant.ID, timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("crear webhook: %w", err)
		}
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID: project.ID, Actor: auditActor(actor),
			Action: "webhook.crear", ObjectType: "webhook", ObjectID: webhookID, ProjectID: project.ID,
			Payload:   map[string]any{"webhook_id": webhookID, "url": rawURL, "events": events},
			CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return WebhookView{}, err
	}
	return WebhookView{ID: webhookID, ProjectID: project.ID, URL: rawURL, Events: events, Active: true, CreatedAt: now}, nil
}

// ListWebhooks lista webhooks de un proyecto.
func (s *Service) ListWebhooks(ctx context.Context, actor Identity, projectRef string) ([]WebhookView, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !rights.CanRead(project.ID) {
		return nil, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "webhook.listar", project.ID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, url, events, active, created_at
		FROM webhooks WHERE project_id = ? ORDER BY created_at DESC`, project.ID)
	if err != nil {
		return nil, fmt.Errorf("listar webhooks: %w", err)
	}
	defer rows.Close()
	items := make([]WebhookView, 0, 4)
	for rows.Next() {
		var item WebhookView
		var events, createdAt string
		var active int
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.URL, &events, &active, &createdAt); err != nil {
			return nil, fmt.Errorf("escanear webhook: %w", err)
		}
		item.Active = active == 1
		_ = json.Unmarshal([]byte(events), &item.Events)
		parsed, err := timefmt.Parse(createdAt)
		if err != nil {
			return nil, fmt.Errorf("created_at de webhook inválido: %w", err)
		}
		item.CreatedAt = parsed
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer webhooks: %w", err)
	}
	return items, nil
}

// RemoveWebhook elimina un webhook.
func (s *Service) RemoveWebhook(ctx context.Context, actor Identity, webhookID string) error {
	var projectID string
	if err := s.db.QueryRowContext(ctx,
		`SELECT project_id FROM webhooks WHERE id = ?`, webhookID,
	).Scan(&projectID); err != nil {
		if err == sql.ErrNoRows {
			return notFound("webhook_not_found", "webhook no encontrado")
		}
		return fmt.Errorf("buscar webhook: %w", err)
	}
	project, err := resolveProject(ctx, s.db, projectID)
	if err != nil {
		return err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return err
	}
	if !rights.CanAdmin(project.ID) {
		return forbidden("requires_project_admin", "requiere admin del proyecto", project.ID, "webhook.quitar", webhookID)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, webhookID); err != nil {
			return fmt.Errorf("quitar webhook: %w", err)
		}
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID: project.ID, Actor: auditActor(actor),
			Action: "webhook.quitar", ObjectType: "webhook", ObjectID: webhookID, ProjectID: project.ID,
			Payload: map[string]any{"webhook_id": webhookID}, CreatedAt: s.now().UTC(),
		})
		return err
	})
}

func (s *Service) enqueueWebhooks(ctx context.Context, tx *sql.Tx, projectID, event string, payload any) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, url, secret, events FROM webhooks WHERE project_id = ? AND active = 1`, projectID)
	if err != nil {
		return fmt.Errorf("buscar webhooks: %w", err)
	}
	type hook struct {
		id, url, secret, events string
	}
	hooks := make([]hook, 0, 4)
	for rows.Next() {
		var item hook
		var secret sql.NullString
		if err := rows.Scan(&item.id, &item.url, &secret, &item.events); err != nil {
			rows.Close()
			return fmt.Errorf("escanear webhook: %w", err)
		}
		item.secret = secret.String
		hooks = append(hooks, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("recorrer webhooks: %w", err)
	}
	_ = rows.Close()
	now := s.now().UTC()
	for _, hook := range hooks {
		if !webhookMatches(hook.events, event) {
			continue
		}
		body, _ := json.Marshal(map[string]any{
			"webhook_id": hook.id, "event": event, "data": payload,
		})
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO outbox(id, kind, destination, payload, attempts, next_attempt_at, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, 0, ?, 'pendiente', ?, ?)`,
			id.New(), event, hook.url, string(body), timefmt.Format(now),
			timefmt.Format(now), timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("encolar webhook: %w", err)
		}
	}
	return nil
}

func webhookMatches(eventsJSON, event string) bool {
	var events []string
	if err := json.Unmarshal([]byte(eventsJSON), &events); err != nil || len(events) == 0 {
		return true
	}
	for _, candidate := range events {
		if candidate == "*" || candidate == event || (strings.HasSuffix(candidate, ".*") && strings.HasPrefix(event, strings.TrimSuffix(candidate, "*"))) {
			return true
		}
	}
	return false
}

// ListOutbox lista entregas. Solo admin de instancia.
func (s *Service) ListOutbox(ctx context.Context, actor Identity, status string, limit int) ([]OutboxItem, error) {
	if !actor.Participant.Admin {
		return nil, forbidden("requires_instance_admin", "solo el admin de instancia ve el outbox", "", "outbox.listar", "")
	}
	limit, err := normalizeLimit(limit)
	if err != nil {
		return nil, err
	}
	conditions := []string{"1 = 1"}
	args := []any{}
	if strings.TrimSpace(status) != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, destination, payload, attempts, next_attempt_at, status,
		       COALESCE(last_error, ''), created_at, updated_at
		FROM outbox WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY created_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("listar outbox: %w", err)
	}
	defer rows.Close()
	items := make([]OutboxItem, 0, limit)
	for rows.Next() {
		var item OutboxItem
		var payload, next, created, updated string
		if err := rows.Scan(&item.ID, &item.Kind, &item.Destination, &payload, &item.Attempts,
			&next, &item.Status, &item.LastError, &created, &updated); err != nil {
			return nil, fmt.Errorf("escanear outbox: %w", err)
		}
		_ = json.Unmarshal([]byte(payload), &item.Payload)
		item.NextAttemptAt, err = timefmt.Parse(next)
		if err != nil {
			return nil, fmt.Errorf("next_attempt_at inválido: %w", err)
		}
		item.CreatedAt, err = timefmt.Parse(created)
		if err != nil {
			return nil, fmt.Errorf("created_at de outbox inválido: %w", err)
		}
		item.UpdatedAt, err = timefmt.Parse(updated)
		if err != nil {
			return nil, fmt.Errorf("updated_at de outbox inválido: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer outbox: %w", err)
	}
	return items, nil
}

// DispatchOutbox intenta entregar pendientes con backoff y dead-letter.
func (s *Service) DispatchOutbox(ctx context.Context) (int, error) {
	now := s.now().UTC()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, destination, payload, attempts
		FROM outbox WHERE status = 'pendiente' AND next_attempt_at <= ?
		ORDER BY next_attempt_at LIMIT 50`, timefmt.Format(now))
	if err != nil {
		return 0, fmt.Errorf("buscar outbox: %w", err)
	}
	type pending struct {
		id, kind, destination, payload string
		attempts                       int
	}
	items := make([]pending, 0, 8)
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.id, &item.kind, &item.destination, &item.payload, &item.attempts); err != nil {
			rows.Close()
			return 0, fmt.Errorf("escanear outbox: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("recorrer outbox: %w", err)
	}
	_ = rows.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	delivered := 0
	for _, item := range items {
		secret := s.webhookSecret(ctx, item.payload)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, item.destination, bytes.NewReader([]byte(item.payload)))
		if err != nil {
			s.outboxFailure(ctx, item.id, item.attempts, err)
			continue
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-AgentBank-Event", item.kind)
		if secret != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(item.payload))
			request.Header.Set("X-AgentBank-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		response, err := client.Do(request)
		if err != nil {
			s.outboxFailure(ctx, item.id, item.attempts, err)
			continue
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE outbox SET status = 'entregado', attempts = attempts + 1, updated_at = ? WHERE id = ?`,
				timefmt.Format(now), item.id,
			); err != nil {
				return delivered, fmt.Errorf("marcar entregado: %w", err)
			}
			delivered++
			continue
		}
		s.outboxFailure(ctx, item.id, item.attempts, fmt.Errorf("status %d", response.StatusCode))
	}
	return delivered, nil
}

func (s *Service) outboxFailure(ctx context.Context, outboxID string, attempts int, cause error) {
	attempts++
	status := "pendiente"
	if attempts >= 10 {
		status = "dead"
	}
	next := s.now().UTC().Add(time.Duration(1<<minInt(attempts, 6)) * time.Minute)
	if _, err := s.db.ExecContext(ctx, `
		UPDATE outbox SET attempts = ?, status = ?, next_attempt_at = ?, last_error = ?, updated_at = ?
		WHERE id = ?`,
		attempts, status, timefmt.Format(next), truncate(cause.Error(), 300),
		timefmt.Format(s.now().UTC()), outboxID,
	); err != nil {
		_ = err
	}
}

func (s *Service) webhookSecret(ctx context.Context, payload string) string {
	var envelope struct {
		WebhookID string `json:"webhook_id"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil || envelope.WebhookID == "" {
		return ""
	}
	var secret sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT secret FROM webhooks WHERE id = ?`, envelope.WebhookID,
	).Scan(&secret); err != nil {
		return ""
	}
	return secret.String
}

// RunOutboxLoop procesa el outbox periódicamente.
func (s *Service) RunOutboxLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = s.DispatchOutbox(ctx)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
