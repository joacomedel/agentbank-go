package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// TicketNextInput filtra la cola.
type TicketNextInput struct {
	ProjectID string
	Role      string
	PhaseID   string
	EpicID    string
	Status    string
	Claim     bool
}

// TicketNextResult es la respuesta de /v1/tickets/siguiente.
type TicketNextResult struct {
	Ticket       *TicketView    `json:"ticket"`
	Candidates   []TicketView   `json:"candidates,omitempty"`
	Motive       string         `json:"motivo"`
	MotiveCounts map[string]int `json:"motivos"`
	ClaimsActive int            `json:"claims_activos"`
	Context      map[string]any `json:"contexto"`
}

// TicketNext elige el próximo ticket listo y, opcionalmente, lo reclama.
func (s *Service) TicketNext(ctx context.Context, actor Identity, input TicketNextInput) (TicketNextResult, error) {
	input.Status = strings.TrimSpace(input.Status)
	if input.Status != "" && input.Status != domain.TicketOpen && input.Status != domain.TicketInReview {
		return TicketNextResult{}, validation("status debe ser abierto o en revisión")
	}
	reviewQueue := input.Status == domain.TicketInReview
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return TicketNextResult{}, err
	}
	projectIDs, pausedAll, err := s.scopeProjects(ctx, actor, rights, input.ProjectID)
	if err != nil {
		return TicketNextResult{}, err
	}
	result := TicketNextResult{MotiveCounts: map[string]int{}, Context: map[string]any{}}
	if len(projectIDs) == 0 {
		result.Motive = "sin_proyectos"
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM claims WHERE participant_id = ? AND released_at IS NULL`,
			actor.Participant.ID,
		).Scan(&result.ClaimsActive); err != nil {
			return TicketNextResult{}, fmt.Errorf("contar claims activos: %w", err)
		}
		return result, nil
	}
	var selected ticketContext
	var selectedPrefix string
	claimLimit := 5
	if input.Claim && !reviewQueue {
		claimLimit = 1
	}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		query, args, err := candidateQuery(projectIDs, actor, rights, input, claimLimit)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("buscar candidatos: %w", err)
		}
		candidates := make([]ticketContext, 0, claimLimit)
		prefixes := make([]string, 0, claimLimit)
		for rows.Next() {
			ticket, prefix, err := scanTicketWithPrefix(rows)
			if err != nil {
				rows.Close()
				return err
			}
			project, err := resolveProject(ctx, tx, ticket.ProjectID)
			if err != nil {
				rows.Close()
				return err
			}
			candidates = append(candidates, ticketContext{Ticket: ticket, Project: project})
			prefixes = append(prefixes, prefix)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("recorrer candidatos: %w", err)
		}
		_ = rows.Close()
		if len(candidates) == 0 {
			if reviewQueue {
				result.Motive = "sin_candidatos"
				result.MotiveCounts = map[string]int{}
				return nil
			}
			result.MotiveCounts, err = s.queueMotives(ctx, tx, projectIDs, actor, rights, input)
			if err != nil {
				return err
			}
			if pausedAll {
				result.Motive = "pausado"
			} else if result.MotiveCounts["esperando_dependencias"] > 0 {
				result.Motive = "esperando_dependencias"
			} else if result.MotiveCounts["esperando_respuesta"] > 0 {
				result.Motive = "esperando_respuesta"
			} else if result.MotiveCounts["reclamados"] > 0 {
				result.Motive = "reclamados"
			} else {
				result.Motive = "sin_candidatos"
			}
			return nil
		}
		selected = candidates[0]
		selectedPrefix = prefixes[0]
		if reviewQueue {
			result.Candidates = make([]TicketView, 0, len(candidates))
			for _, candidate := range candidates {
				effective, err := s.effectiveStatus(ctx, tx, candidate.Ticket)
				if err != nil {
					return err
				}
				result.Candidates = append(result.Candidates, baseTicketView(candidate.Ticket, candidate.Project, effective))
			}
			result.Ticket = &result.Candidates[0]
			return nil
		}
		if !input.Claim {
			result.Candidates = make([]TicketView, 0, len(candidates))
			for _, candidate := range candidates {
				effective, err := s.effectiveStatus(ctx, tx, candidate.Ticket)
				if err != nil {
					return err
				}
				result.Candidates = append(result.Candidates, baseTicketView(candidate.Ticket, candidate.Project, effective))
			}
			return nil
		}
		var active int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM claims WHERE ticket_id = ? AND released_at IS NULL`,
			selected.Ticket.ID,
		).Scan(&active); err != nil {
			return fmt.Errorf("consultar claim: %w", err)
		}
		if active > 0 {
			return conflict("claim_conflict", "el ticket ya tiene un claim activo")
		}
		claim := domain.Claim{
			ID: id.New(), TicketID: selected.Ticket.ID, ParticipantID: actor.Participant.ID,
			SessionID: actor.SessionID, StatusPrevious: selected.Ticket.Status,
			LeaseUntil: now.Add(s.claimTTL), LastActivityAt: now, CreatedAt: now,
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO claims(id, ticket_id, participant_id, session_id, status_previo,
			                   lease_until, last_activity_at, created_by_role, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			claim.ID, claim.TicketID, claim.ParticipantID, nullable(claim.SessionID),
			claim.StatusPrevious, timefmt.Format(claim.LeaseUntil),
			timefmt.Format(claim.LastActivityAt), actor.Participant.AgentRole,
			timefmt.Format(claim.CreatedAt),
		); err != nil {
			if isUniqueConstraint(err) {
				return conflict("claim_conflict", "el ticket ya tiene un claim activo")
			}
			return fmt.Errorf("crear claim: %w", err)
		}
		selected.Ticket.Status = domain.TicketInProgress
		selected.Ticket.Version++
		selected.Ticket.UpdatedAt = now
		if _, err := tx.ExecContext(ctx,
			`UPDATE tickets SET status = ?, version = ?, updated_by_role = ?, updated_at = ? WHERE id = ?`,
			selected.Ticket.Status, selected.Ticket.Version, actor.Participant.AgentRole,
			timefmt.Format(now), selected.Ticket.ID,
		); err != nil {
			return fmt.Errorf("marcar ticket en curso: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: selected.Project.ID, Actor: auditActor(actor),
			Action: "ticket.claim", ObjectType: "ticket", ObjectID: selected.Ticket.ID, ProjectID: selected.Project.ID,
			Payload: map[string]any{
				"ticket_id": selected.Ticket.ID, "claim_id": claim.ID,
				"participant_id": actor.Participant.ID, "lease_until": timefmt.Format(claim.LeaseUntil),
				"source": "ticket_siguiente",
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		view, err := s.ticketView(ctx, tx, selected.Ticket, selected.Project, true)
		if err != nil {
			return err
		}
		result.Ticket = &view
		result.Context, err = buildTicketContext(ctx, tx, selected.Ticket)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return TicketNextResult{}, err
	}
	if result.Ticket != nil {
		result.Ticket.Ref = selected.Ticket.Ref(selectedPrefix)
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM claims WHERE participant_id = ? AND released_at IS NULL`,
		actor.Participant.ID,
	).Scan(&result.ClaimsActive); err != nil {
		return TicketNextResult{}, fmt.Errorf("contar claims activos: %w", err)
	}
	return result, nil
}

func (s *Service) scopeProjects(ctx context.Context, actor Identity, rights domain.Rights, projectRef string) ([]string, bool, error) {
	if strings.TrimSpace(projectRef) != "" {
		project, err := resolveProject(ctx, s.db, projectRef)
		if err != nil {
			return nil, false, err
		}
		if !rights.CanWrite(project.ID) {
			return nil, false, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "ticket.siguiente", project.ID)
		}
		var paused int
		if err := s.db.QueryRowContext(ctx,
			`SELECT paused FROM projects WHERE id = ?`, project.ID,
		).Scan(&paused); err != nil {
			return nil, false, fmt.Errorf("consultar pausa: %w", err)
		}
		return []string{project.ID}, paused == 1, nil
	}
	if rights.Admin {
		rows, err := s.db.QueryContext(ctx, `SELECT id, paused FROM projects ORDER BY slug LIMIT 200`)
		if err != nil {
			return nil, false, fmt.Errorf("listar proyectos: %w", err)
		}
		defer rows.Close()
		ids := make([]string, 0, 8)
		allPaused := true
		for rows.Next() {
			var id string
			var paused int
			if err := rows.Scan(&id, &paused); err != nil {
				return nil, false, fmt.Errorf("escanear proyecto: %w", err)
			}
			ids = append(ids, id)
			if paused == 0 {
				allPaused = false
			}
		}
		if err := rows.Err(); err != nil {
			return nil, false, fmt.Errorf("recorrer proyectos: %w", err)
		}
		if len(ids) == 0 {
			allPaused = false
		}
		return ids, allPaused, nil
	}
	ids := make([]string, 0, len(rights.Memberships))
	for id, membership := range rights.Memberships {
		if membership.Permission == domain.PermissionWriter || membership.Permission == domain.PermissionAdmin {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return ids, false, nil
	}
	allPaused := true
	for _, id := range ids {
		var paused int
		if err := s.db.QueryRowContext(ctx, `SELECT paused FROM projects WHERE id = ?`, id).Scan(&paused); err != nil {
			return nil, false, fmt.Errorf("consultar pausa: %w", err)
		}
		if paused == 0 {
			allPaused = false
		}
	}
	_ = actor
	return ids, allPaused, nil
}

func candidateQuery(projectIDs []string, actor Identity, rights domain.Rights, input TicketNextInput, limit int) (string, []any, error) {
	if len(projectIDs) == 0 {
		return "", nil, nil
	}
	placeholders := make([]string, len(projectIDs))
	args := make([]any, 0, len(projectIDs)+8)
	for i, id := range projectIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	if strings.TrimSpace(input.Status) == domain.TicketInReview {
		conditions := []string{
			"t.project_id IN (" + strings.Join(placeholders, ",") + ")",
			"t.status = 'en revisión'",
			"pr.paused = 0",
			"NOT EXISTS (SELECT 1 FROM claims c WHERE c.ticket_id = t.id AND c.released_at IS NULL)",
		}
		if strings.TrimSpace(input.PhaseID) != "" {
			conditions = append(conditions, "COALESCE(e.phase_id, t.phase_id) = ?")
			args = append(args, input.PhaseID)
		}
		if strings.TrimSpace(input.EpicID) != "" {
			conditions = append(conditions, "t.epic_id = ?")
			args = append(args, input.EpicID)
		}
		args = append(args, limit)
		query := `
		SELECT t.id, t.project_id, t.number, COALESCE(t.key, ''), t.title, t.description,
		       t.status, t.priority, COALESCE(t.assignee_id, ''), COALESCE(t.assigned_role, ''),
		       COALESCE(t.phase_id, ''), COALESCE(t.epic_id, ''), t.order_index, t.version,
		       t.paths, COALESCE(t.discovered_in, ''), COALESCE(t.created_by, ''), COALESCE(t.created_by_role, ''), COALESCE(t.updated_by_role, ''), t.requiere_revision, t.commit_sha, t.reaperturas,
		       t.created_at, t.updated_at, pr.prefix
		FROM tickets t
		JOIN projects pr ON pr.id = t.project_id
		LEFT JOIN epics e ON e.id = t.epic_id
		LEFT JOIN phases ph ON ph.id = COALESCE(e.phase_id, t.phase_id)
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY t.updated_at ASC, t.number ASC
		LIMIT ?`
		return query, args, nil
	}
	conditions := []string{
		"t.project_id IN (" + strings.Join(placeholders, ",") + ")",
		"t.status = 'abierto'",
		"pr.paused = 0",
		"NOT EXISTS (SELECT 1 FROM claims c WHERE c.ticket_id = t.id AND c.released_at IS NULL)",
		"(t.assignee_id IS NULL OR t.assignee_id = ?)",
		"NOT EXISTS (SELECT 1 FROM ticket_edges e JOIN tickets d ON d.id = e.depends_on_id WHERE e.ticket_id = t.id AND d.status NOT IN ('hecho','cancelado'))",
		"NOT EXISTS (SELECT 1 FROM message_affects ma JOIN messages m ON m.id = ma.message_id WHERE ma.ticket_id = t.id AND m.resolution IS NULL)",
	}
	args = append(args, actor.Participant.ID)
	role := strings.TrimSpace(input.Role)
	if role != "" {
		conditions = append(conditions, "(t.assigned_role IS NULL OR t.assigned_role = '' OR t.assigned_role = ?)")
		args = append(args, role)
		if !rights.Admin {
			conditions = append(conditions, `EXISTS (
				SELECT 1 FROM memberships m, json_each(m.roles) j
				WHERE m.participant_id = ? AND m.project_id = t.project_id AND j.value = ?)`)
			args = append(args, actor.Participant.ID, role)
		}
	} else if !rights.Admin {
		conditions = append(conditions, `(t.assigned_role IS NULL OR t.assigned_role = '' OR EXISTS (
			SELECT 1 FROM memberships m, json_each(m.roles) j
			WHERE m.participant_id = ? AND m.project_id = t.project_id AND j.value = t.assigned_role))`)
		args = append(args, actor.Participant.ID)
	}
	if strings.TrimSpace(input.PhaseID) != "" {
		conditions = append(conditions, "COALESCE(e.phase_id, t.phase_id) = ?")
		args = append(args, input.PhaseID)
	}
	if strings.TrimSpace(input.EpicID) != "" {
		conditions = append(conditions, "t.epic_id = ?")
		args = append(args, input.EpicID)
	}
	args = append(args, limit)
	query := `
		SELECT t.id, t.project_id, t.number, COALESCE(t.key, ''), t.title, t.description,
		       t.status, t.priority, COALESCE(t.assignee_id, ''), COALESCE(t.assigned_role, ''),
		       COALESCE(t.phase_id, ''), COALESCE(t.epic_id, ''), t.order_index, t.version,
		       t.paths, COALESCE(t.discovered_in, ''), COALESCE(t.created_by, ''), COALESCE(t.created_by_role, ''), COALESCE(t.updated_by_role, ''), t.requiere_revision, t.commit_sha, t.reaperturas,
		       t.created_at, t.updated_at, pr.prefix
		FROM tickets t
		JOIN projects pr ON pr.id = t.project_id
		LEFT JOIN epics e ON e.id = t.epic_id
		LEFT JOIN phases ph ON ph.id = COALESCE(e.phase_id, t.phase_id)
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY COALESCE(ph.order_index, 9223372036854775807),
		         COALESCE(e.order_index, 9223372036854775807),
		         t.order_index,
		         CASE t.priority WHEN 'urgente' THEN 0 WHEN 'alta' THEN 1 WHEN 'media' THEN 2 ELSE 3 END,
		         t.created_at ASC,
		         t.number ASC
		LIMIT ?`
	return query, args, nil
}

func (s *Service) queueMotives(ctx context.Context, tx *sql.Tx, projectIDs []string, actor Identity, rights domain.Rights, input TicketNextInput) (map[string]int, error) {
	counts := map[string]int{
		"esperando_dependencias": 0,
		"esperando_respuesta":    0,
		"reclamados":             0,
	}
	if len(projectIDs) == 0 {
		return counts, nil
	}
	placeholders := make([]string, len(projectIDs))
	args := make([]any, 0, len(projectIDs)+2)
	for i, id := range projectIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	base := `FROM tickets t JOIN projects pr ON pr.id = t.project_id WHERE t.status = 'abierto' AND pr.paused = 0 AND t.project_id IN (` + strings.Join(placeholders, ",") + `)`
	args = append(args, actor.Participant.ID)
	base += ` AND (t.assignee_id IS NULL OR t.assignee_id = ?)`
	queries := map[string]string{
		"esperando_dependencias": base + ` AND EXISTS (SELECT 1 FROM ticket_edges e JOIN tickets d ON d.id = e.depends_on_id WHERE e.ticket_id = t.id AND d.status NOT IN ('hecho','cancelado'))`,
		"esperando_respuesta":    base + ` AND EXISTS (SELECT 1 FROM message_affects ma JOIN messages m ON m.id = ma.message_id WHERE ma.ticket_id = t.id AND m.resolution IS NULL)`,
		"reclamados":             base + ` AND EXISTS (SELECT 1 FROM claims c WHERE c.ticket_id = t.id AND c.released_at IS NULL)`,
	}
	for key, query := range queries {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) `+query, args...).Scan(&count); err != nil {
			return nil, fmt.Errorf("contar motivo %s: %w", key, err)
		}
		counts[key] = count
	}
	_ = rights
	_ = input
	return counts, nil
}

func scanTicketWithPrefix(row interface{ Scan(...any) error }) (domain.Ticket, string, error) {
	var ticket domain.Ticket
	var paths, createdAt, updatedAt, prefix string
	var requiereRevision int
	if err := row.Scan(
		&ticket.ID, &ticket.ProjectID, &ticket.Number, &ticket.Key, &ticket.Title,
		&ticket.Description, &ticket.Status, &ticket.Priority, &ticket.AssigneeID,
		&ticket.AssignedRole, &ticket.PhaseID, &ticket.EpicID, &ticket.OrderIndex,
		&ticket.Version, &paths, &ticket.DiscoveredIn, &ticket.CreatedBy,
		&ticket.CreatedByRole, &ticket.UpdatedByRole, &requiereRevision,
		&ticket.CommitSHA, &ticket.Reaperturas, &createdAt, &updatedAt, &prefix,
	); err != nil {
		return domain.Ticket{}, "", err
	}
	ticket.RequiereRevision = requiereRevision != 0
	if err := json.Unmarshal([]byte(paths), &ticket.Paths); err != nil {
		return domain.Ticket{}, "", fmt.Errorf("paths inválidos: %w", err)
	}
	if ticket.Paths == nil {
		ticket.Paths = []string{}
	}
	parsedCreated, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Ticket{}, "", fmt.Errorf("created_at de ticket inválido: %w", err)
	}
	parsedUpdated, err := timefmt.Parse(updatedAt)
	if err != nil {
		return domain.Ticket{}, "", fmt.Errorf("updated_at de ticket inválido: %w", err)
	}
	ticket.CreatedAt = parsedCreated
	ticket.UpdatedAt = parsedUpdated
	return ticket, prefix, nil
}

func buildTicketContext(ctx context.Context, tx *sql.Tx, ticket domain.Ticket) (map[string]any, error) {
	contextMap := map[string]any{}
	rows, err := tx.QueryContext(ctx, `
		SELECT d_ref.ref, c.body
		FROM ticket_edges e
		JOIN tickets d ON d.id = e.depends_on_id
		JOIN projects p ON p.id = d.project_id
		JOIN comments c ON c.ticket_id = d.id AND c.type = 'cierre'
		JOIN (SELECT d.id AS ticket_id, p.prefix || '-' || d.number AS ref
		      FROM tickets d JOIN projects p ON p.id = d.project_id) d_ref ON d_ref.ticket_id = d.id
		WHERE e.ticket_id = ?
		ORDER BY c.created_at DESC LIMIT 5`, ticket.ID)
	if err != nil {
		return nil, fmt.Errorf("contexto de dependencias: %w", err)
	}
	closing := make([]map[string]any, 0, 4)
	for rows.Next() {
		var ref, body string
		if err := rows.Scan(&ref, &body); err != nil {
			rows.Close()
			return nil, fmt.Errorf("escanear cierre: %w", err)
		}
		closing = append(closing, map[string]any{"ref": ref, "nota": truncate(body, 400)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("recorrer cierres: %w", err)
	}
	_ = rows.Close()
	if len(closing) > 0 {
		contextMap["notas_cierre"] = closing
	}

	rows, err = tx.QueryContext(ctx, `
		SELECT m.subject, m.response_option, COALESCE(m.response_text, '')
		FROM message_affects ma
		JOIN messages m ON m.id = ma.message_id
		WHERE ma.ticket_id = ? AND m.resolution IS NOT NULL
		ORDER BY m.resolved_at DESC LIMIT 5`, ticket.ID)
	if err != nil {
		return nil, fmt.Errorf("contexto de mensajes: %w", err)
	}
	resolutions := make([]map[string]any, 0, 4)
	for rows.Next() {
		var subject, text string
		var option sql.NullInt64
		if err := rows.Scan(&subject, &option, &text); err != nil {
			rows.Close()
			return nil, fmt.Errorf("escanear resolución: %w", err)
		}
		item := map[string]any{"subject": subject, "text": truncate(text, 400)}
		if option.Valid {
			item["option"] = option.Int64
		}
		resolutions = append(resolutions, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("recorrer resoluciones: %w", err)
	}
	_ = rows.Close()
	if len(resolutions) > 0 {
		contextMap["resoluciones"] = resolutions
	}

	rows, err = tx.QueryContext(ctx, `
		SELECT d.title, COALESCE(d.route, '')
		FROM ticket_documents td
		JOIN documents d ON d.id = td.document_id
		WHERE td.ticket_id = ?
		ORDER BY d.title LIMIT 5`, ticket.ID)
	if err != nil {
		return nil, fmt.Errorf("contexto de documentos: %w", err)
	}
	docs := make([]map[string]any, 0, 4)
	for rows.Next() {
		var title, route string
		if err := rows.Scan(&title, &route); err != nil {
			rows.Close()
			return nil, fmt.Errorf("escanear documento: %w", err)
		}
		docs = append(docs, map[string]any{"title": title, "route": route})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("recorrer documentos: %w", err)
	}
	_ = rows.Close()
	if len(docs) > 0 {
		contextMap["docs"] = docs
	}
	return contextMap, nil
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}
