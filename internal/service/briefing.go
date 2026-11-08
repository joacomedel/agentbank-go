package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// Briefing es el contexto de arranque de un proyecto.
type Briefing struct {
	ProjectID           string           `json:"project_id"`
	ActivePhaseID       string           `json:"active_phase_id,omitempty"`
	Phases              []PhaseView      `json:"phases"`
	Decisions           []map[string]any `json:"decisions"`
	PinnedDocs          []DocumentView   `json:"pinned_docs"`
	ActiveClaims        []map[string]any `json:"active_claims"`
	NextTickets         []TicketView     `json:"next_tickets"`
	InboxCount          int              `json:"inbox_count"`
	Inbox               []MessageView    `json:"inbox"`
	RolesWithoutMembers []string         `json:"roles_without_members"`
	SizeBytes           int              `json:"size_bytes"`
}

// Briefing arma el contexto de arranque sin mutar nada.
func (s *Service) Briefing(ctx context.Context, actor Identity, projectRef string) (Briefing, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return Briefing{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return Briefing{}, err
	}
	if !rights.CanRead(project.ID) {
		return Briefing{}, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "briefing", project.ID)
	}
	briefing := Briefing{ProjectID: project.ID, Decisions: []map[string]any{}, PinnedDocs: []DocumentView{}, ActiveClaims: []map[string]any{}, NextTickets: []TicketView{}, Inbox: []MessageView{}, RolesWithoutMembers: []string{}}
	phases, err := s.ListPhases(ctx, actor, project.ID)
	if err != nil {
		return Briefing{}, err
	}
	briefing.Phases = phases
	for _, phase := range phases {
		if phase.Status == domain.PhaseOpen && briefing.ActivePhaseID == "" {
			briefing.ActivePhaseID = phase.ID
		}
	}
	decisions, err := s.ListDecisions(ctx, actor, project.ID)
	if err != nil {
		return Briefing{}, err
	}
	if len(decisions) > 5 {
		decisions = decisions[:5]
	}
	briefing.Decisions = decisions
	docs, err := s.pinnedDocuments(ctx, actor, project.ID)
	if err != nil {
		return Briefing{}, err
	}
	briefing.PinnedDocs = docs
	claims, err := s.activeClaimsForParticipant(ctx, actor.Participant.ID, project.ID)
	if err != nil {
		return Briefing{}, err
	}
	briefing.ActiveClaims = claims
	if rights.CanWrite(project.ID) {
		next, err := s.TicketNext(ctx, actor, TicketNextInput{ProjectID: project.ID, Claim: false})
		if err != nil {
			return Briefing{}, err
		}
		if len(next.Candidates) > 5 {
			next.Candidates = next.Candidates[:5]
		}
		briefing.NextTickets = next.Candidates
	}
	inbox, _, err := s.ListInbox(ctx, actor, project.ID, true, 5, "")
	if err != nil {
		return Briefing{}, err
	}
	briefing.Inbox = inbox
	count, err := s.pendingInboxCount(ctx, actor, project.ID)
	if err != nil {
		return Briefing{}, err
	}
	briefing.InboxCount = count
	roles, err := s.rolesWithoutMembers(ctx, project.ID)
	if err != nil {
		return Briefing{}, err
	}
	briefing.RolesWithoutMembers = roles
	return shrinkBriefing(briefing), nil
}

func (s *Service) pinnedDocuments(ctx context.Context, actor Identity, projectID string) ([]DocumentView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(project_id, ''), title, COALESCE(route, ''), type, pinned,
		       current_version, COALESCE(created_by, ''), created_at, updated_at
		FROM documents
		WHERE pinned = 1 AND (project_id = ? OR project_id IS NULL)
		ORDER BY project_id DESC, title LIMIT 20`, projectID)
	if err != nil {
		return nil, fmt.Errorf("listar docs fijados: %w", err)
	}
	defer rows.Close()
	docs := make([]domain.Document, 0, 8)
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer docs fijados: %w", err)
	}
	views := make([]DocumentView, 0, len(docs))
	for _, doc := range docs {
		views = append(views, DocumentView{
			ID: doc.ID, ProjectID: doc.ProjectID, Title: doc.Title, Route: doc.Route,
			Type: doc.Type, Pinned: doc.Pinned, CurrentVersion: doc.CurrentVersion,
			Version: doc.CurrentVersion, CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
		})
	}
	return views, nil
}

func (s *Service) activeClaimsForParticipant(ctx context.Context, participantID, projectID string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, t.id, p.prefix || '-' || t.number, t.title, c.lease_until, c.created_at
		FROM claims c
		JOIN tickets t ON t.id = c.ticket_id
		JOIN projects p ON p.id = t.project_id
		WHERE c.participant_id = ? AND c.released_at IS NULL AND t.project_id = ?
		ORDER BY c.created_at`, participantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("listar claims del briefing: %w", err)
	}
	defer rows.Close()
	items := make([]map[string]any, 0, 4)
	for rows.Next() {
		var claimID, ticketID, ref, title, lease, created string
		if err := rows.Scan(&claimID, &ticketID, &ref, &title, &lease, &created); err != nil {
			return nil, fmt.Errorf("escanear claim del briefing: %w", err)
		}
		parsedLease, err := timefmt.Parse(lease)
		if err != nil {
			return nil, err
		}
		parsedCreated, err := timefmt.Parse(created)
		if err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"claim_id": claimID, "ticket_id": ticketID, "ref": ref, "title": title,
			"lease_until": parsedLease, "created_at": parsedCreated,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer claims del briefing: %w", err)
	}
	return items, nil
}

func (s *Service) pendingInboxCount(ctx context.Context, actor Identity, projectID string) (int, error) {
	conditions := []string{"m.resolution IS NULL", "m.project_id = ?", "m.from_participant_id <> ?"}
	args := []any{projectID, actor.Participant.ID}
	recipient := "(m.destination_participant_id = ?"
	args = append(args, actor.Participant.ID)
	recipient += " OR (m.destination_type = 'rol' AND EXISTS (SELECT 1 FROM memberships mm, json_each(mm.roles) j WHERE mm.participant_id = ? AND mm.project_id = m.project_id AND j.value = m.destination_role))"
	args = append(args, actor.Participant.ID)
	recipient += " OR m.destination_type = 'proyecto')"
	conditions = append(conditions, recipient)
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM messages m WHERE `+strings.Join(conditions, " AND "), args...,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("contar bandeja: %w", err)
	}
	return count, nil
}

func (s *Service) rolesWithoutMembers(ctx context.Context, projectID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pr.name
		FROM project_roles pr
		WHERE pr.project_id = ?
		  AND NOT EXISTS (
			SELECT 1 FROM memberships m, json_each(m.roles) j
			JOIN participants p ON p.id = m.participant_id
			WHERE m.project_id = pr.project_id AND j.value = pr.name AND p.status = 'activo'
		  )
		ORDER BY pr.name`, projectID)
	if err != nil {
		return nil, fmt.Errorf("listar roles sin miembros: %w", err)
	}
	defer rows.Close()
	roles := make([]string, 0, 4)
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("escanear rol sin miembros: %w", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer roles sin miembros: %w", err)
	}
	return roles, nil
}

func shrinkBriefing(briefing Briefing) Briefing {
	for {
		encoded, _ := json.Marshal(briefing)
		briefing.SizeBytes = len(encoded)
		if len(encoded) <= 4096 {
			return briefing
		}
		switch {
		case len(briefing.Inbox) > 0:
			briefing.Inbox = briefing.Inbox[:len(briefing.Inbox)-1]
		case len(briefing.NextTickets) > 0:
			briefing.NextTickets = briefing.NextTickets[:len(briefing.NextTickets)-1]
		case len(briefing.PinnedDocs) > 0:
			briefing.PinnedDocs = briefing.PinnedDocs[:len(briefing.PinnedDocs)-1]
		case len(briefing.ActiveClaims) > 0:
			briefing.ActiveClaims = briefing.ActiveClaims[:len(briefing.ActiveClaims)-1]
		case len(briefing.Decisions) > 0:
			briefing.Decisions = briefing.Decisions[:len(briefing.Decisions)-1]
		case len(briefing.Phases) > 0:
			briefing.Phases = briefing.Phases[:len(briefing.Phases)-1]
		default:
			return briefing
		}
	}
}

// Digest devuelve contadores y eventos recientes.
func (s *Service) Digest(ctx context.Context, actor Identity, projectRef string) (map[string]any, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !rights.CanRead(project.ID) {
		return nil, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "digest", project.ID)
	}
	counts := map[string]int{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT status, COUNT(1) FROM tickets WHERE project_id = ? GROUP BY status`, project.ID)
	if err != nil {
		return nil, fmt.Errorf("contar tickets: %w", err)
	}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			rows.Close()
			return nil, fmt.Errorf("escanear conteo: %w", err)
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("recorrer conteos: %w", err)
	}
	_ = rows.Close()
	var pendingMessages, activeClaims int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM messages WHERE project_id = ? AND resolution IS NULL`, project.ID,
	).Scan(&pendingMessages); err != nil {
		return nil, fmt.Errorf("contar mensajes: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM claims c JOIN tickets t ON t.id = c.ticket_id
		WHERE t.project_id = ? AND c.released_at IS NULL`, project.ID,
	).Scan(&activeClaims); err != nil {
		return nil, fmt.Errorf("contar claims: %w", err)
	}
	events := make([]AuditEventView, 0, 10)
	recent, _, err := s.ListAudit(ctx, actor, AuditFilter{ChainID: project.ID}, 10, "")
	if err != nil {
		return nil, err
	}
	events = append(events, recent...)
	return map[string]any{
		"project_id":       project.ID,
		"tickets":          counts,
		"pending_messages": pendingMessages,
		"active_claims":    activeClaims,
		"recent_events":    events,
	}, nil
}

// Tablero devuelve fases, épicas y tickets en una sola lectura.
func (s *Service) Tablero(ctx context.Context, actor Identity, projectRef string) (map[string]any, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !rights.CanRead(project.ID) {
		return nil, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "tablero", project.ID)
	}
	phases, err := s.ListPhases(ctx, actor, project.ID)
	if err != nil {
		return nil, err
	}
	detailed := make([]PhaseView, 0, len(phases))
	activePhaseID := ""
	for _, phase := range phases {
		got, err := s.GetPhase(ctx, actor, phase.ID)
		if err != nil {
			return nil, err
		}
		detailed = append(detailed, got)
		if activePhaseID == "" && phase.Status == domain.PhaseOpen {
			activePhaseID = phase.ID
		}
	}
	loose, err := s.listTicketsByQuery(ctx, project, `
		SELECT id, project_id, number, COALESCE(key, ''), title, description,
		       status, priority, COALESCE(assignee_id, ''), COALESCE(assigned_role, ''),
		       COALESCE(phase_id, ''), COALESCE(epic_id, ''), order_index, version,
		       paths, COALESCE(discovered_in, ''), COALESCE(created_by, ''),
		       created_at, updated_at
		FROM tickets WHERE project_id = ? AND phase_id IS NULL AND epic_id IS NULL
		ORDER BY order_index, created_at`, project.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"project_id":      project.ID,
		"active_phase_id": activePhaseID,
		"phases":          detailed,
		"loose_tickets":   loose,
	}, nil
}
