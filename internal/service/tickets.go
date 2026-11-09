package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

var (
	ticketRefPattern = regexp.MustCompile(`^([A-Za-z0-9]{2,10})-([0-9]+)$`)
	ticketKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,80}$`)
)

// TicketInput es el alta de un ticket.
type TicketInput struct {
	Title            string
	Description      string
	Status           string
	Priority         string
	AssigneeID       string
	AssignedRole     string
	PhaseID          string
	EpicID           string
	OrderIndex       int64
	Paths            []string
	DiscoveredIn     string
	Key              string
	DependsOn        []string
	RequiereRevision *bool
}

// TicketUpdate son los campos editables. Los punteros distinguen ausencia.
type TicketUpdate struct {
	Title        *string
	Description  *string
	Status       *string
	Priority     *string
	AssigneeID   *string
	AssignedRole *string
	PhaseID      *string
	EpicID       *string
	OrderIndex   *int64
	Paths        *[]string
	DiscoveredIn *string
	Version      *int64
}

// TicketView es la representación de ticket para la API.
type TicketView struct {
	ID                 string                `json:"id"`
	ProjectID          string                `json:"project_id"`
	Ref                string                `json:"ref"`
	Number             int64                 `json:"number"`
	Key                string                `json:"key,omitempty"`
	Title              string                `json:"title"`
	Description        string                `json:"description"`
	DescriptionContent domain.ForeignContent `json:"description_content"`
	Status             string                `json:"status"`
	EffectiveStatus    string                `json:"effective_status"`
	Priority           string                `json:"priority"`
	AssigneeID         string                `json:"assignee_id,omitempty"`
	AssignedRole       string                `json:"role,omitempty"`
	PhaseID            string                `json:"phase_id,omitempty"`
	EpicID             string                `json:"epic_id,omitempty"`
	OrderIndex         int64                 `json:"order_index"`
	Version            int64                 `json:"version"`
	Paths              []string              `json:"paths"`
	DiscoveredIn       string                `json:"discovered_in,omitempty"`
	CreatedBy          string                `json:"created_by,omitempty"`
	CreatedByRole      string                `json:"created_by_role,omitempty"`
	UpdatedByRole      string                `json:"updated_by_role,omitempty"`
	RequiereRevision   bool                  `json:"requiere_revision"`
	CommitSHA          string                `json:"commit_sha,omitempty"`
	Reaperturas        int64                 `json:"reaperturas"`
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
	Dependencies       []DependencyView      `json:"dependencies,omitempty"`
	Dependents         []DependencyView      `json:"dependents,omitempty"`
	Claims             []ClaimView           `json:"claims,omitempty"`
	CommentsCount      int                   `json:"comments_count"`
}

// DependencyView es una arista resuelta para mostrar.
type DependencyView struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	Ref           string `json:"ref"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	CreatedByRole string `json:"created_by_role,omitempty"`
}

// ClaimView es un claim activo visible.
type ClaimView struct {
	ID            string    `json:"id"`
	ParticipantID string    `json:"participant_id"`
	Name          string    `json:"name"`
	CreatedByRole string    `json:"created_by_role,omitempty"`
	LeaseUntil    time.Time `json:"lease_until"`
	CreatedAt     time.Time `json:"created_at"`
}

// ClaimResult es la respuesta de reclamar un ticket.
type ClaimResult struct {
	Ticket TicketView `json:"ticket"`
	Claim  ClaimView  `json:"claim"`
}

// CommentView es un comentario con su autor.
type CommentView struct {
	ID          string                `json:"id"`
	TicketID    string                `json:"ticket_id"`
	AuthorID    string                `json:"author_id"`
	AuthorName  string                `json:"author_name"`
	AuthorRole  string                `json:"author_role,omitempty"`
	Type        string                `json:"type"`
	Body        string                `json:"body"`
	BodyContent domain.ForeignContent `json:"body_content"`
	CreatedAt   time.Time             `json:"created_at"`
}

// TimelineItem mezcla comentarios y eventos de auditoría.
type TimelineItem struct {
	ID          string                `json:"id"`
	Type        string                `json:"type"`
	AuthorID    string                `json:"author_id,omitempty"`
	AuthorName  string                `json:"author_name,omitempty"`
	ActorRole   string                `json:"actor_role,omitempty"`
	SessionID   string                `json:"session_id,omitempty"`
	Action      string                `json:"action,omitempty"`
	Body        string                `json:"body,omitempty"`
	BodyContent domain.ForeignContent `json:"body_content,omitempty"`
	Payload     any                   `json:"payload,omitempty"`
	CreatedAt   time.Time             `json:"created_at"`
}

// TicketFilter son filtros de listado.
type TicketFilter struct {
	Status       string
	Priority     string
	Role         string
	AssigneeID   string
	PhaseID      string
	EpicID       string
	Key          string
	UpdatedSince string
}

type ticketContext struct {
	Ticket  domain.Ticket
	Project domain.Project
}

// CreateTicket crea un ticket, asigna número y detecta ciclos.
func (s *Service) CreateTicket(ctx context.Context, actor Identity, projectRef string, input TicketInput) (TicketView, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return TicketView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return TicketView{}, err
	}
	if !rights.CanWrite(project.ID) {
		return TicketView{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "ticket.crear", "")
	}
	title := strings.TrimSpace(input.Title)
	if title == "" || len([]rune(title)) > 500 {
		return TicketView{}, validation("title debe tener entre 1 y 500 caracteres")
	}
	if err := validateNoSecrets(title, input.Description); err != nil {
		return TicketView{}, err
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = domain.TicketOpen
	}
	if err := validateTicketStatus(status); err != nil {
		return TicketView{}, err
	}
	priority := strings.TrimSpace(input.Priority)
	if priority == "" {
		priority = domain.PriorityMedium
	}
	if err := validatePriority(priority); err != nil {
		return TicketView{}, err
	}
	key := strings.TrimSpace(input.Key)
	if key != "" && !ticketKeyPattern.MatchString(key) {
		return TicketView{}, validation("key inválida: 1..80 caracteres alfanuméricos, punto, guion o guion bajo")
	}
	paths, err := normalizePaths(input.Paths)
	if err != nil {
		return TicketView{}, err
	}
	role := strings.TrimSpace(input.AssignedRole)
	if role != "" {
		if _, err := normalizeRoles([]string{role}); err != nil {
			return TicketView{}, err
		}
	}

	var view TicketView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		ticket := domain.Ticket{
			ID:               id.New(),
			ProjectID:        project.ID,
			Key:              key,
			Title:            title,
			Description:      input.Description,
			Status:           status,
			Priority:         priority,
			AssigneeID:       strings.TrimSpace(input.AssigneeID),
			AssignedRole:     role,
			OrderIndex:       input.OrderIndex,
			Version:          1,
			Paths:            paths,
			RequiereRevision: input.RequiereRevision == nil || *input.RequiereRevision,
			CreatedBy:        actor.Participant.ID,
			CreatedByRole:    actor.Participant.AgentRole,
			UpdatedByRole:    actor.Participant.AgentRole,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if ticket.AssigneeID != "" {
			if _, err := getParticipant(ctx, tx, ticket.AssigneeID); err != nil {
				return err
			}
		}
		if role != "" {
			if err := registerRoles(ctx, tx, project.ID, []string{role}, now); err != nil {
				return err
			}
		}
		if strings.TrimSpace(input.EpicID) != "" {
			if strings.TrimSpace(input.PhaseID) != "" {
				return validation("un ticket con épica no acepta phase_id")
			}
			epic, err := resolveEpic(ctx, tx, project.ID, input.EpicID)
			if err != nil {
				return err
			}
			ticket.EpicID = epic.ID
		} else if strings.TrimSpace(input.PhaseID) != "" {
			phase, err := resolvePhase(ctx, tx, project.ID, input.PhaseID)
			if err != nil {
				return err
			}
			ticket.PhaseID = phase.ID
		}
		if strings.TrimSpace(input.DiscoveredIn) != "" {
			discovered, err := resolveTicket(ctx, tx, input.DiscoveredIn)
			if err != nil {
				return err
			}
			if !rights.CanRead(discovered.Project.ID) {
				return forbidden("requires_project_read", "sin permiso para referenciar el ticket de origen", discovered.Project.ID, "ticket.crear", discovered.Ticket.ID)
			}
			ticket.DiscoveredIn = discovered.Ticket.ID
		}
		dependencies := make([]ticketContext, 0, len(input.DependsOn))
		seenDeps := make(map[string]struct{}, len(input.DependsOn))
		for _, ref := range input.DependsOn {
			dep, err := resolveTicket(ctx, tx, ref)
			if err != nil {
				return err
			}
			if dep.Ticket.ID == ticket.ID {
				return validation("un ticket no puede depender de sí mismo")
			}
			if _, duplicated := seenDeps[dep.Ticket.ID]; duplicated {
				continue
			}
			if !rights.CanRead(dep.Project.ID) {
				return forbidden("requires_project_read", "sin permiso para referenciar la dependencia", dep.Project.ID, "ticket.crear", dep.Ticket.ID)
			}
			seenDeps[dep.Ticket.ID] = struct{}{}
			dependencies = append(dependencies, dep)
		}
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(number), 0) + 1 FROM tickets WHERE project_id = ?`,
			project.ID,
		).Scan(&ticket.Number); err != nil {
			return fmt.Errorf("asignar número de ticket: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tickets
				(id, project_id, number, key, title, description, status, priority,
				 assignee_id, assigned_role, phase_id, epic_id, order_index, version,
				 paths, discovered_in, created_by, created_by_role, updated_by_role,
				 requiere_revision, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ticket.ID, ticket.ProjectID, ticket.Number, nullable(ticket.Key), ticket.Title,
			ticket.Description, ticket.Status, ticket.Priority, nullable(ticket.AssigneeID),
			nullable(ticket.AssignedRole), nullable(ticket.PhaseID), nullable(ticket.EpicID),
			ticket.OrderIndex, mustJSON(ticket.Paths), nullable(ticket.DiscoveredIn),
			nullable(ticket.CreatedBy), ticket.CreatedByRole, ticket.UpdatedByRole,
			boolInt(ticket.RequiereRevision), timefmt.Format(now), timefmt.Format(now),
		); err != nil {
			if isUniqueConstraint(err) {
				return conflict("ticket_key_taken", "ya existe un ticket con esa key")
			}
			return fmt.Errorf("crear ticket: %w", err)
		}
		for _, dep := range dependencies {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO ticket_edges(ticket_id, depends_on_id, created_by, created_by_role, created_at)
				VALUES (?, ?, ?, ?, ?)`,
				ticket.ID, dep.Ticket.ID, actor.Participant.ID,
				actor.Participant.AgentRole, timefmt.Format(now),
			); err != nil {
				return fmt.Errorf("crear dependencia: %w", err)
			}
		}
		if err := ensureNoCycle(ctx, tx, ticket.ID); err != nil {
			return err
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    project.ID,
			Actor:      auditActor(actor),
			Action:     "ticket.crear",
			ObjectType: "ticket",
			ObjectID:   ticket.ID,
			ProjectID:  project.ID,
			Payload: map[string]any{
				"ticket_id": ticket.ID,
				"ref":       ticket.Ref(project.Prefix),
				"title":     ticket.Title,
				"status":    ticket.Status,
				"priority":  ticket.Priority,
				"deps":      dependencyIDs(dependencies),
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := s.enqueueWebhooks(ctx, tx, project.ID, "ticket.crear", map[string]any{
			"ticket_id": ticket.ID, "ref": ticket.Ref(project.Prefix), "title": ticket.Title,
		}); err != nil {
			return err
		}
		view, err = s.ticketView(ctx, tx, ticket, project, true)
		return err
	})
	if err != nil {
		return TicketView{}, err
	}
	return view, nil
}

// ListTickets lista tickets de un proyecto, más nuevos primero.
func (s *Service) ListTickets(ctx context.Context, actor Identity, projectRef string, filter TicketFilter, limit int, cursor string) ([]TicketView, string, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return nil, "", err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, "", err
	}
	if !rights.CanRead(project.ID) {
		return nil, "", forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "ticket.listar", project.ID)
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return nil, "", err
	}
	conditions := []string{"t.project_id = ?"}
	args := []any{project.ID}
	if filter.Status != "" {
		if err := validateTicketStatus(filter.Status); err != nil {
			return nil, "", err
		}
		conditions = append(conditions, "t.status = ?")
		args = append(args, filter.Status)
	}
	if filter.Priority != "" {
		if err := validatePriority(filter.Priority); err != nil {
			return nil, "", err
		}
		conditions = append(conditions, "t.priority = ?")
		args = append(args, filter.Priority)
	}
	if filter.Role != "" {
		conditions = append(conditions, "t.assigned_role = ?")
		args = append(args, filter.Role)
	}
	if filter.AssigneeID != "" {
		conditions = append(conditions, "t.assignee_id = ?")
		args = append(args, filter.AssigneeID)
	}
	if filter.PhaseID != "" {
		conditions = append(conditions, "COALESCE(e.phase_id, t.phase_id) = ?")
		args = append(args, filter.PhaseID)
	}
	if filter.EpicID != "" {
		conditions = append(conditions, "t.epic_id = ?")
		args = append(args, filter.EpicID)
	}
	if filter.Key != "" {
		conditions = append(conditions, "t.key = ?")
		args = append(args, filter.Key)
	}
	incremental := strings.TrimSpace(filter.UpdatedSince) != ""
	if incremental {
		since, err := timefmt.ParseFlexible(filter.UpdatedSince)
		if err != nil {
			return nil, "", validation("updated_since inválido")
		}
		if cursor != "" {
			createdAt, lastID, err := decodeAuditCursor(cursor)
			if err != nil {
				return nil, "", validation("cursor inválido")
			}
			conditions = append(conditions, "(t.updated_at > ? OR (t.updated_at = ? AND t.id > ?))")
			args = append(args, createdAt, createdAt, lastID)
		} else {
			conditions = append(conditions, "t.updated_at >= ?")
			args = append(args, timefmt.Format(since))
		}
	} else if cursor != "" {
		number, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return nil, "", validation("cursor inválido")
		}
		conditions = append(conditions, "t.number < ?")
		args = append(args, number)
	}
	args = append(args, limit+1)
	order := "t.number DESC"
	if incremental {
		order = "t.updated_at ASC, t.id ASC"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, t.project_id, t.number, COALESCE(t.key, ''), t.title, t.description,
		       t.status, t.priority, COALESCE(t.assignee_id, ''), COALESCE(t.assigned_role, ''),
		       COALESCE(t.phase_id, ''), COALESCE(t.epic_id, ''), t.order_index, t.version,
		       t.paths, COALESCE(t.discovered_in, ''), COALESCE(t.created_by, ''), COALESCE(t.created_by_role, ''), COALESCE(t.updated_by_role, ''), t.requiere_revision, t.commit_sha, t.reaperturas,
		       t.created_at, t.updated_at
		FROM tickets t
		LEFT JOIN epics e ON e.id = t.epic_id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY `+order+`
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar tickets: %w", err)
	}
	defer rows.Close()
	tickets := make([]domain.Ticket, 0, limit)
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			return nil, "", err
		}
		tickets = append(tickets, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer tickets: %w", err)
	}
	views := make([]TicketView, 0, len(tickets))
	for _, ticket := range tickets {
		status, err := s.effectiveStatus(ctx, s.db, ticket)
		if err != nil {
			return nil, "", err
		}
		views = append(views, baseTicketView(ticket, project, status))
	}
	next := ""
	if len(views) > limit {
		views = views[:limit]
		last := views[len(views)-1]
		if incremental {
			next = encodeAuditCursor(timefmt.Format(last.UpdatedAt), last.ID)
		} else {
			next = strconv.FormatInt(last.Number, 10)
		}
	}
	return views, next, nil
}

// GetTicket devuelve el detalle completo.
func (s *Service) GetTicket(ctx context.Context, actor Identity, ref string) (TicketView, error) {
	tc, err := resolveTicket(ctx, s.db, ref)
	if err != nil {
		return TicketView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return TicketView{}, err
	}
	if !rights.CanRead(tc.Project.ID) {
		return TicketView{}, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", tc.Project.ID, "ticket.leer", tc.Ticket.ID)
	}
	return s.ticketView(ctx, s.db, tc.Ticket, tc.Project, true)
}

// UpdateTicket edita un ticket con control optimista de versión.
func (s *Service) UpdateTicket(ctx context.Context, actor Identity, ref string, update TicketUpdate) (TicketView, error) {
	tc, err := resolveTicket(ctx, s.db, ref)
	if err != nil {
		return TicketView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return TicketView{}, err
	}
	if !rights.CanWrite(tc.Project.ID) {
		return TicketView{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", tc.Project.ID, "ticket.editar", tc.Ticket.ID)
	}
	if update.Version == nil {
		return TicketView{}, validation("version es obligatoria para editar un ticket")
	}
	var view TicketView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		current, err := ticketByID(ctx, tx, tc.Ticket.ID)
		if err != nil {
			return err
		}
		if *update.Version != current.Version {
			return conflict("version_conflict", "versión desactualizada")
		}
		previous := ticketAuditPayload(current)
		if update.Title != nil {
			title := strings.TrimSpace(*update.Title)
			if title == "" || len([]rune(title)) > 500 {
				return validation("title debe tener entre 1 y 500 caracteres")
			}
			current.Title = title
		}
		if update.Description != nil {
			current.Description = *update.Description
		}
		if err := validateNoSecrets(current.Title, current.Description); err != nil {
			return err
		}
		if update.Status != nil {
			status := strings.TrimSpace(*update.Status)
			if err := validateTicketStatus(status); err != nil {
				return err
			}
			if status != current.Status {
				return conflict("transicion_invalida", "usá /avanzar o /revision para cambiar de estado")
			}
		}
		if update.Priority != nil {
			priority := strings.TrimSpace(*update.Priority)
			if err := validatePriority(priority); err != nil {
				return err
			}
			current.Priority = priority
		}
		if update.AssigneeID != nil {
			assignee := strings.TrimSpace(*update.AssigneeID)
			if assignee != "" {
				if _, err := getParticipant(ctx, tx, assignee); err != nil {
					return err
				}
			}
			current.AssigneeID = assignee
		}
		if update.AssignedRole != nil {
			role := strings.TrimSpace(*update.AssignedRole)
			if role != "" {
				if _, err := normalizeRoles([]string{role}); err != nil {
					return err
				}
				if err := registerRoles(ctx, tx, tc.Project.ID, []string{role}, s.now().UTC()); err != nil {
					return err
				}
			}
			current.AssignedRole = role
		}
		if update.EpicID != nil {
			epicRef := strings.TrimSpace(*update.EpicID)
			if epicRef == "" {
				current.EpicID = ""
			} else {
				epic, err := resolveEpic(ctx, tx, tc.Project.ID, epicRef)
				if err != nil {
					return err
				}
				current.EpicID = epic.ID
				current.PhaseID = ""
			}
		}
		if update.PhaseID != nil {
			phaseRef := strings.TrimSpace(*update.PhaseID)
			if phaseRef == "" {
				current.PhaseID = ""
			} else {
				if current.EpicID != "" {
					return validation("un ticket con épica no acepta phase_id")
				}
				phase, err := resolvePhase(ctx, tx, tc.Project.ID, phaseRef)
				if err != nil {
					return err
				}
				current.PhaseID = phase.ID
			}
		}
		if update.OrderIndex != nil {
			current.OrderIndex = *update.OrderIndex
		}
		if update.Paths != nil {
			paths, err := normalizePaths(*update.Paths)
			if err != nil {
				return err
			}
			current.Paths = paths
		}
		if update.DiscoveredIn != nil {
			discoveredRef := strings.TrimSpace(*update.DiscoveredIn)
			if discoveredRef == "" {
				current.DiscoveredIn = ""
			} else {
				discovered, err := resolveTicket(ctx, tx, discoveredRef)
				if err != nil {
					return err
				}
				if !rights.CanRead(discovered.Project.ID) {
					return forbidden("requires_project_read", "sin permiso para referenciar el ticket de origen", discovered.Project.ID, "ticket.editar", discovered.Ticket.ID)
				}
				current.DiscoveredIn = discovered.Ticket.ID
			}
		}
		now := s.now().UTC()
		current.Version++
		current.UpdatedAt = now
		if current.Status != domain.TicketInProgress {
			if err := releaseClaimsForTicket(ctx, tx, current.ID, now, "estado cambiado", actor, tc.Project.ID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tickets SET
				title = ?, description = ?, status = ?, priority = ?,
				assignee_id = ?, assigned_role = ?, phase_id = ?, epic_id = ?,
				order_index = ?, version = ?, paths = ?, discovered_in = ?,
				updated_by_role = ?, updated_at = ?
			WHERE id = ?`,
			current.Title, current.Description, current.Status, current.Priority,
			nullable(current.AssigneeID), nullable(current.AssignedRole), nullable(current.PhaseID),
			nullable(current.EpicID), current.OrderIndex, current.Version, mustJSON(current.Paths),
			nullable(current.DiscoveredIn), actor.Participant.AgentRole,
			timefmt.Format(now), current.ID,
		); err != nil {
			return fmt.Errorf("actualizar ticket: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    tc.Project.ID,
			Actor:      auditActor(actor),
			Action:     "ticket.editar",
			ObjectType: "ticket",
			ObjectID:   current.ID,
			ProjectID:  tc.Project.ID,
			Payload: map[string]any{
				"previous": previous,
				"new":      ticketAuditPayload(current),
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := s.enqueueWebhooks(ctx, tx, tc.Project.ID, "ticket.editar", map[string]any{
			"ticket_id": current.ID, "ref": current.Ref(tc.Project.Prefix), "status": current.Status,
		}); err != nil {
			return err
		}
		view, err = s.ticketView(ctx, tx, current, tc.Project, true)
		return err
	})
	if err != nil {
		return TicketView{}, err
	}
	return view, nil
}

// AddDependency agrega "ticket depende de depRef".
func (s *Service) AddDependency(ctx context.Context, actor Identity, ticketRef, depRef string) (DependencyView, error) {
	tc, err := resolveTicket(ctx, s.db, ticketRef)
	if err != nil {
		return DependencyView{}, err
	}
	dep, err := resolveTicket(ctx, s.db, depRef)
	if err != nil {
		return DependencyView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return DependencyView{}, err
	}
	if !rights.CanWrite(tc.Project.ID) {
		return DependencyView{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", tc.Project.ID, "ticket.dependencia.crear", tc.Ticket.ID)
	}
	if !rights.CanRead(dep.Project.ID) {
		return DependencyView{}, forbidden("requires_project_read", "sin permiso para leer la dependencia", dep.Project.ID, "ticket.dependencia.crear", dep.Ticket.ID)
	}
	if tc.Ticket.ID == dep.Ticket.ID {
		return DependencyView{}, validation("un ticket no puede depender de sí mismo")
	}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO ticket_edges(ticket_id, depends_on_id, created_by, created_by_role, created_at)
			VALUES (?, ?, ?, ?, ?)`,
			tc.Ticket.ID, dep.Ticket.ID, actor.Participant.ID,
			actor.Participant.AgentRole, timefmt.Format(now),
		); err != nil {
			if isUniqueConstraint(err) {
				return conflict("dependency_exists", "la dependencia ya existe")
			}
			return fmt.Errorf("crear dependencia: %w", err)
		}
		if err := ensureNoCycle(ctx, tx, tc.Ticket.ID); err != nil {
			return err
		}
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    tc.Project.ID,
			Actor:      auditActor(actor),
			Action:     "ticket.dependencia.crear",
			ObjectType: "ticket",
			ObjectID:   tc.Ticket.ID,
			ProjectID:  tc.Project.ID,
			Payload: map[string]any{
				"ticket_id":     tc.Ticket.ID,
				"depends_on_id": dep.Ticket.ID,
				"ref":           dep.Ticket.Ref(dep.Project.Prefix),
			},
			CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return DependencyView{}, err
	}
	return DependencyView{
		ID: dep.Ticket.ID, ProjectID: dep.Ticket.ProjectID,
		Ref: dep.Ticket.Ref(dep.Project.Prefix), Title: dep.Ticket.Title, Status: dep.Ticket.Status,
	}, nil
}

// RemoveDependency quita "ticket depende de depRef".
func (s *Service) RemoveDependency(ctx context.Context, actor Identity, ticketRef, depRef string) error {
	tc, err := resolveTicket(ctx, s.db, ticketRef)
	if err != nil {
		return err
	}
	dep, err := resolveTicket(ctx, s.db, depRef)
	if err != nil {
		return err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return err
	}
	if !rights.CanWrite(tc.Project.ID) {
		return forbidden("requires_project_write", "sin permiso de escritura en el proyecto", tc.Project.ID, "ticket.dependencia.quitar", tc.Ticket.ID)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx,
			`DELETE FROM ticket_edges WHERE ticket_id = ? AND depends_on_id = ?`,
			tc.Ticket.ID, dep.Ticket.ID)
		if err != nil {
			return fmt.Errorf("quitar dependencia: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("verificar dependencia: %w", err)
		}
		if affected == 0 {
			return notFound("dependency_not_found", "dependencia no encontrada")
		}
		_, err = audit.Append(ctx, tx, audit.Input{
			ChainID:    tc.Project.ID,
			Actor:      auditActor(actor),
			Action:     "ticket.dependencia.quitar",
			ObjectType: "ticket",
			ObjectID:   tc.Ticket.ID,
			ProjectID:  tc.Project.ID,
			Payload: map[string]any{
				"ticket_id":     tc.Ticket.ID,
				"depends_on_id": dep.Ticket.ID,
			},
			CreatedAt: s.now().UTC(),
		})
		return err
	})
}

// AddComment agrega un comentario, nota o cierre.
func (s *Service) AddComment(ctx context.Context, actor Identity, ref, body, commentType string) (CommentView, error) {
	tc, err := resolveTicket(ctx, s.db, ref)
	if err != nil {
		return CommentView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return CommentView{}, err
	}
	if !rights.CanWrite(tc.Project.ID) {
		return CommentView{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", tc.Project.ID, "ticket.comentar", tc.Ticket.ID)
	}
	body = strings.TrimSpace(body)
	if body == "" || len(body) > 20000 {
		return CommentView{}, validation("body debe tener entre 1 y 20000 caracteres")
	}
	if err := validateNoSecrets(body); err != nil {
		return CommentView{}, err
	}
	if commentType == "" {
		commentType = domain.CommentNote
	}
	if commentType != domain.CommentNote && commentType != domain.CommentNota && commentType != domain.CommentClose {
		return CommentView{}, validation("type debe ser comentario, nota o cierre")
	}
	var view CommentView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		comment := domain.Comment{
			ID:         id.New(),
			TicketID:   tc.Ticket.ID,
			AuthorID:   actor.Participant.ID,
			AuthorRole: actor.Participant.AgentRole,
			Type:       commentType,
			Body:       body,
			CreatedAt:  now,
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO comments(id, ticket_id, author_participant_id, author_role, type, body, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			comment.ID, comment.TicketID, comment.AuthorID, actor.Participant.AgentRole,
			comment.Type, comment.Body, timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("crear comentario: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    tc.Project.ID,
			Actor:      auditActor(actor),
			Action:     "ticket.comentar",
			ObjectType: "ticket",
			ObjectID:   tc.Ticket.ID,
			ProjectID:  tc.Project.ID,
			Payload: map[string]any{
				"ticket_id":  tc.Ticket.ID,
				"comment_id": comment.ID,
				"type":       comment.Type,
				"body":       comment.Body,
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := s.enqueueWebhooks(ctx, tx, tc.Project.ID, "ticket.comentar", map[string]any{
			"ticket_id": tc.Ticket.ID, "comment_id": comment.ID, "type": comment.Type,
		}); err != nil {
			return err
		}
		view = CommentView{
			ID: comment.ID, TicketID: comment.TicketID, AuthorID: comment.AuthorID,
			AuthorName: actor.Participant.Name, AuthorRole: actor.Participant.AgentRole,
			Type: comment.Type, Body: comment.Body,
			BodyContent: domain.WrapForeign(comment.Body, "comment"), CreatedAt: comment.CreatedAt,
		}
		return nil
	})
	if err != nil {
		return CommentView{}, err
	}
	return view, nil
}

// ListComments lista comentarios de un ticket.
func (s *Service) ListComments(ctx context.Context, actor Identity, ref string, limit int, cursor string) ([]CommentView, string, error) {
	tc, err := resolveTicket(ctx, s.db, ref)
	if err != nil {
		return nil, "", err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, "", err
	}
	if !rights.CanRead(tc.Project.ID) {
		return nil, "", forbidden("requires_project_read", "sin permiso de lectura en el proyecto", tc.Project.ID, "ticket.comentarios", tc.Ticket.ID)
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return nil, "", err
	}
	args := []any{tc.Ticket.ID}
	where := ""
	if cursor != "" {
		where = " AND c.created_at > ?"
		args = append(args, cursor)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.ticket_id, c.author_participant_id, p.name, c.author_role,
		       c.type, c.body, c.created_at
		FROM comments c
		JOIN participants p ON p.id = c.author_participant_id
		WHERE c.ticket_id = ?`+where+`
		ORDER BY c.created_at ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar comentarios: %w", err)
	}
	defer rows.Close()
	comments := make([]CommentView, 0, limit)
	for rows.Next() {
		var view CommentView
		var createdAt string
		if err := rows.Scan(&view.ID, &view.TicketID, &view.AuthorID, &view.AuthorName,
			&view.AuthorRole, &view.Type, &view.Body, &createdAt); err != nil {
			return nil, "", fmt.Errorf("escanear comentario: %w", err)
		}
		parsed, err := timefmt.Parse(createdAt)
		if err != nil {
			return nil, "", fmt.Errorf("created_at de comentario inválido: %w", err)
		}
		view.CreatedAt = parsed
		view.BodyContent = domain.WrapForeign(view.Body, "comment")
		comments = append(comments, view)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer comentarios: %w", err)
	}
	next := ""
	if len(comments) > limit {
		comments = comments[:limit]
		next = timefmt.Format(comments[len(comments)-1].CreatedAt)
	}
	return comments, next, nil
}

// Timeline mezcla comentarios y eventos de auditoría del ticket.
func (s *Service) Timeline(ctx context.Context, actor Identity, ref string, limit int) ([]TimelineItem, error) {
	tc, err := resolveTicket(ctx, s.db, ref)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !rights.CanRead(tc.Project.ID) {
		return nil, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", tc.Project.ID, "ticket.timeline", tc.Ticket.ID)
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return nil, err
	}
	comments, _, err := s.ListComments(ctx, actor, tc.Ticket.ID, 200, "")
	if err != nil {
		return nil, err
	}
	events, err := s.ticketAuditEvents(ctx, tc.Ticket.ID, 200)
	if err != nil {
		return nil, err
	}
	items := make([]TimelineItem, 0, len(comments)+len(events))
	for _, comment := range comments {
		items = append(items, TimelineItem{
			ID: comment.ID, Type: "comentario", AuthorID: comment.AuthorID,
			AuthorName: comment.AuthorName, ActorRole: comment.AuthorRole,
			Body: comment.Body, BodyContent: comment.BodyContent,
			CreatedAt: comment.CreatedAt,
		})
	}
	for _, event := range events {
		if event.Action == "ticket.comentar" {
			continue
		}
		items = append(items, event)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	return items, nil
}

func (s *Service) ticketAuditEvents(ctx context.Context, ticketID string, limit int) ([]TimelineItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, COALESCE(a.actor_id, ''), COALESCE(p.name, ''),
		       COALESCE(a.actor_role, ''), COALESCE(a.session_id, ''),
		       a.action, a.payload, a.created_at
		FROM audit_events a
		LEFT JOIN participants p ON p.id = a.actor_id
		WHERE a.object_type = 'ticket' AND a.object_id = ?
		ORDER BY a.created_at ASC
		LIMIT ?`, ticketID, limit)
	if err != nil {
		return nil, fmt.Errorf("listar timeline: %w", err)
	}
	defer rows.Close()
	items := make([]TimelineItem, 0, limit)
	for rows.Next() {
		var item TimelineItem
		var payload, createdAt string
		if err := rows.Scan(&item.ID, &item.AuthorID, &item.AuthorName,
			&item.ActorRole, &item.SessionID, &item.Action, &payload,
			&createdAt); err != nil {
			return nil, fmt.Errorf("escanear timeline: %w", err)
		}
		_ = json.Unmarshal([]byte(payload), &item.Payload)
		parsed, err := timefmt.Parse(createdAt)
		if err != nil {
			return nil, fmt.Errorf("created_at de timeline inválido: %w", err)
		}
		item.Type = "evento"
		item.CreatedAt = parsed
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer timeline: %w", err)
	}
	return items, nil
}

func (s *Service) ticketView(ctx context.Context, q queryer, ticket domain.Ticket, project domain.Project, detailed bool) (TicketView, error) {
	status, err := s.effectiveStatus(ctx, q, ticket)
	if err != nil {
		return TicketView{}, err
	}
	view := baseTicketView(ticket, project, status)
	if !detailed {
		return view, nil
	}
	deps, err := loadDependencies(ctx, q, ticket.ID, "out")
	if err != nil {
		return TicketView{}, err
	}
	dependents, err := loadDependencies(ctx, q, ticket.ID, "in")
	if err != nil {
		return TicketView{}, err
	}
	claims, err := loadActiveClaims(ctx, q, ticket.ID)
	if err != nil {
		return TicketView{}, err
	}
	var comments int
	if err := q.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM comments WHERE ticket_id = ?`, ticket.ID,
	).Scan(&comments); err != nil {
		return TicketView{}, fmt.Errorf("contar comentarios: %w", err)
	}
	view.Dependencies = deps
	view.Dependents = dependents
	view.Claims = claims
	view.CommentsCount = comments
	return view, nil
}

func baseTicketView(ticket domain.Ticket, project domain.Project, effective string) TicketView {
	paths := ticket.Paths
	if paths == nil {
		paths = []string{}
	}
	return TicketView{
		ID: ticket.ID, ProjectID: ticket.ProjectID, Ref: ticket.Ref(project.Prefix),
		Number: ticket.Number, Key: ticket.Key, Title: ticket.Title,
		Description:        ticket.Description,
		DescriptionContent: domain.WrapForeign(ticket.Description, "ticket_description"),
		Status:             ticket.Status, EffectiveStatus: effective, Priority: ticket.Priority,
		AssigneeID: ticket.AssigneeID, AssignedRole: ticket.AssignedRole,
		PhaseID: ticket.PhaseID, EpicID: ticket.EpicID, OrderIndex: ticket.OrderIndex,
		Version: ticket.Version, Paths: paths, DiscoveredIn: ticket.DiscoveredIn,
		CreatedBy: ticket.CreatedBy, CreatedByRole: ticket.CreatedByRole,
		UpdatedByRole: ticket.UpdatedByRole, RequiereRevision: ticket.RequiereRevision,
		CommitSHA: ticket.CommitSHA, Reaperturas: ticket.Reaperturas,
		CreatedAt: ticket.CreatedAt, UpdatedAt: ticket.UpdatedAt,
	}
}

func (s *Service) effectiveStatus(ctx context.Context, q queryer, ticket domain.Ticket) (string, error) {
	if ticket.Status != domain.TicketOpen {
		return ticket.Status, nil
	}
	pendingDeps, err := pendingDependencies(ctx, q, ticket.ID)
	if err != nil {
		return "", err
	}
	if pendingDeps > 0 {
		return "esperando_dependencias", nil
	}
	pendingMessages, err := pendingMessageAffects(ctx, q, ticket.ID)
	if err != nil {
		return "", err
	}
	if pendingMessages > 0 {
		return "esperando_respuesta", nil
	}
	return "listo", nil
}

func pendingDependencies(ctx context.Context, q queryer, ticketID string) (int, error) {
	var count int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM ticket_edges e
		JOIN tickets d ON d.id = e.depends_on_id
		WHERE e.ticket_id = ? AND d.status NOT IN ('hecho', 'cancelado')`,
		ticketID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("contar dependencias pendientes: %w", err)
	}
	return count, nil
}

func pendingMessageAffects(ctx context.Context, q queryer, ticketID string) (int, error) {
	var count int
	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM message_affects ma
		JOIN messages m ON m.id = ma.message_id
		WHERE ma.ticket_id = ? AND m.resolution IS NULL`,
		ticketID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("contar mensajes pendientes: %w", err)
	}
	return count, nil
}

func ensureNoCycle(ctx context.Context, tx *sql.Tx, ticketID string) error {
	var cycle int
	err := tx.QueryRowContext(ctx, `
		WITH RECURSIVE reach(id) AS (
			SELECT depends_on_id FROM ticket_edges WHERE ticket_id = ?
			UNION
			SELECT e.depends_on_id
			FROM ticket_edges e
			JOIN reach r ON e.ticket_id = r.id
		)
		SELECT COUNT(1) FROM reach WHERE id = ?`, ticketID, ticketID,
	).Scan(&cycle)
	if err != nil {
		return fmt.Errorf("detectar ciclo: %w", err)
	}
	if cycle > 0 {
		return validation("la dependencia cierra un ciclo")
	}
	return nil
}

func ticketByID(ctx context.Context, q queryer, ticketID string) (domain.Ticket, error) {
	ticket, err := scanTicket(q.QueryRowContext(ctx, `
		SELECT id, project_id, number, COALESCE(key, ''), title, description,
		       status, priority, COALESCE(assignee_id, ''), COALESCE(assigned_role, ''),
		       COALESCE(phase_id, ''), COALESCE(epic_id, ''), order_index, version,
		       paths, COALESCE(discovered_in, ''), COALESCE(created_by, ''), COALESCE(created_by_role, ''), COALESCE(updated_by_role, ''), requiere_revision, commit_sha, reaperturas,
		       created_at, updated_at
		FROM tickets WHERE id = ?`, ticketID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Ticket{}, notFound("ticket_not_found", "ticket no encontrado")
	}
	if err != nil {
		return domain.Ticket{}, err
	}
	return ticket, nil
}

func resolveTicket(ctx context.Context, q queryer, ref string) (ticketContext, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ticketContext{}, notFound("ticket_not_found", "ticket no encontrado")
	}
	var ticket domain.Ticket
	var err error
	if match := ticketRefPattern.FindStringSubmatch(ref); match != nil {
		prefix := strings.ToUpper(match[1])
		number, _ := strconv.ParseInt(match[2], 10, 64)
		ticket, err = scanTicket(q.QueryRowContext(ctx, `
			SELECT t.id, t.project_id, t.number, COALESCE(t.key, ''), t.title, t.description,
			       t.status, t.priority, COALESCE(t.assignee_id, ''), COALESCE(t.assigned_role, ''),
			       COALESCE(t.phase_id, ''), COALESCE(t.epic_id, ''), t.order_index, t.version,
			       t.paths, COALESCE(t.discovered_in, ''), COALESCE(t.created_by, ''), COALESCE(t.created_by_role, ''), COALESCE(t.updated_by_role, ''), t.requiere_revision, t.commit_sha, t.reaperturas,
			       t.created_at, t.updated_at
			FROM tickets t
			JOIN projects p ON p.id = t.project_id
			WHERE p.prefix = ? AND t.number = ?`, prefix, number))
	} else {
		ticket, err = ticketByID(ctx, q, ref)
		if err != nil {
			var notFoundErr *NotFoundError
			if !errors.As(err, &notFoundErr) {
				return ticketContext{}, err
			}
			ticket, err = scanTicket(q.QueryRowContext(ctx, `
				SELECT id, project_id, number, COALESCE(key, ''), title, description,
				       status, priority, COALESCE(assignee_id, ''), COALESCE(assigned_role, ''),
				       COALESCE(phase_id, ''), COALESCE(epic_id, ''), order_index, version,
				       paths, COALESCE(discovered_in, ''), COALESCE(created_by, ''), COALESCE(created_by_role, ''), COALESCE(updated_by_role, ''), requiere_revision, commit_sha, reaperturas,
				       created_at, updated_at
				FROM tickets WHERE key = ? LIMIT 2`, ref))
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ticketContext{}, notFound("ticket_not_found", "ticket no encontrado")
	}
	if err != nil {
		return ticketContext{}, err
	}
	project, err := resolveProject(ctx, q, ticket.ProjectID)
	if err != nil {
		return ticketContext{}, err
	}
	return ticketContext{Ticket: ticket, Project: project}, nil
}

func scanTicket(row interface{ Scan(...any) error }) (domain.Ticket, error) {
	var ticket domain.Ticket
	var paths, createdAt, updatedAt string
	var requiereRevision int
	if err := row.Scan(
		&ticket.ID, &ticket.ProjectID, &ticket.Number, &ticket.Key, &ticket.Title,
		&ticket.Description, &ticket.Status, &ticket.Priority, &ticket.AssigneeID,
		&ticket.AssignedRole, &ticket.PhaseID, &ticket.EpicID, &ticket.OrderIndex,
		&ticket.Version, &paths, &ticket.DiscoveredIn, &ticket.CreatedBy,
		&ticket.CreatedByRole, &ticket.UpdatedByRole, &requiereRevision,
		&ticket.CommitSHA, &ticket.Reaperturas, &createdAt, &updatedAt,
	); err != nil {
		return domain.Ticket{}, err
	}
	ticket.RequiereRevision = requiereRevision != 0
	if err := json.Unmarshal([]byte(paths), &ticket.Paths); err != nil {
		return domain.Ticket{}, fmt.Errorf("paths inválidos: %w", err)
	}
	if ticket.Paths == nil {
		ticket.Paths = []string{}
	}
	parsedCreated, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Ticket{}, fmt.Errorf("created_at de ticket inválido: %w", err)
	}
	parsedUpdated, err := timefmt.Parse(updatedAt)
	if err != nil {
		return domain.Ticket{}, fmt.Errorf("updated_at de ticket inválido: %w", err)
	}
	ticket.CreatedAt = parsedCreated
	ticket.UpdatedAt = parsedUpdated
	return ticket, nil
}

func loadDependencies(ctx context.Context, q queryer, ticketID, direction string) ([]DependencyView, error) {
	var query string
	if direction == "out" {
		query = `
			SELECT t.id, t.project_id, p.prefix, t.number, t.title, t.status, e.created_by_role
			FROM ticket_edges e
			JOIN tickets t ON t.id = e.depends_on_id
			JOIN projects p ON p.id = t.project_id
			WHERE e.ticket_id = ?
			ORDER BY p.prefix, t.number`
	} else {
		query = `
			SELECT t.id, t.project_id, p.prefix, t.number, t.title, t.status, e.created_by_role
			FROM ticket_edges e
			JOIN tickets t ON t.id = e.ticket_id
			JOIN projects p ON p.id = t.project_id
			WHERE e.depends_on_id = ?
			ORDER BY p.prefix, t.number`
	}
	rows, err := q.QueryContext(ctx, query, ticketID)
	if err != nil {
		return nil, fmt.Errorf("listar dependencias: %w", err)
	}
	defer rows.Close()
	items := make([]DependencyView, 0, 4)
	for rows.Next() {
		var item DependencyView
		var prefix string
		var number int64
		if err := rows.Scan(&item.ID, &item.ProjectID, &prefix, &number, &item.Title,
			&item.Status, &item.CreatedByRole); err != nil {
			return nil, fmt.Errorf("escanear dependencia: %w", err)
		}
		item.Ref = fmt.Sprintf("%s-%d", prefix, number)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer dependencias: %w", err)
	}
	return items, nil
}

func loadActiveClaims(ctx context.Context, q queryer, ticketID string) ([]ClaimView, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT c.id, c.participant_id, p.name, c.created_by_role, c.lease_until, c.created_at
		FROM claims c
		JOIN participants p ON p.id = c.participant_id
		WHERE c.ticket_id = ? AND c.released_at IS NULL
		ORDER BY c.created_at`, ticketID)
	if err != nil {
		return nil, fmt.Errorf("listar claims: %w", err)
	}
	defer rows.Close()
	items := make([]ClaimView, 0, 1)
	for rows.Next() {
		var item ClaimView
		var lease, created string
		if err := rows.Scan(&item.ID, &item.ParticipantID, &item.Name,
			&item.CreatedByRole, &lease, &created); err != nil {
			return nil, fmt.Errorf("escanear claim: %w", err)
		}
		parsedLease, err := timefmt.Parse(lease)
		if err != nil {
			return nil, fmt.Errorf("lease inválido: %w", err)
		}
		parsedCreated, err := timefmt.Parse(created)
		if err != nil {
			return nil, fmt.Errorf("created_at de claim inválido: %w", err)
		}
		item.LeaseUntil = parsedLease
		item.CreatedAt = parsedCreated
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer claims: %w", err)
	}
	return items, nil
}

func validateTicketStatus(status string) error {
	switch status {
	case domain.TicketOpen, domain.TicketInProgress, domain.TicketInReview,
		domain.TicketDone, domain.TicketBlocked, domain.TicketCancelled:
		return nil
	default:
		return validation("status inválido")
	}
}

func validatePriority(priority string) error {
	switch priority {
	case domain.PriorityLow, domain.PriorityMedium, domain.PriorityHigh, domain.PriorityUrgent:
		return nil
	default:
		return validation("priority debe ser baja, media, alta o urgente")
	}
}

func normalizePaths(paths []string) ([]string, error) {
	if len(paths) > 50 {
		return nil, validation("paths admite como máximo 50 elementos")
	}
	normalized := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || len(path) > 300 {
			return nil, validation("cada path debe tener 1..300 caracteres")
		}
		if _, duplicated := seen[path]; duplicated {
			continue
		}
		seen[path] = struct{}{}
		normalized = append(normalized, path)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func dependencyIDs(deps []ticketContext) []string {
	ids := make([]string, 0, len(deps))
	for _, dep := range deps {
		ids = append(ids, dep.Ticket.ID)
	}
	return ids
}

func ticketAuditPayload(ticket domain.Ticket) map[string]any {
	return map[string]any{
		"title":             ticket.Title,
		"description":       ticket.Description,
		"status":            ticket.Status,
		"priority":          ticket.Priority,
		"assignee_id":       ticket.AssigneeID,
		"role":              ticket.AssignedRole,
		"phase_id":          ticket.PhaseID,
		"epic_id":           ticket.EpicID,
		"order_index":       ticket.OrderIndex,
		"version":           ticket.Version,
		"paths":             ticket.Paths,
		"created_by":        ticket.CreatedBy,
		"created_by_role":   ticket.CreatedByRole,
		"updated_by_role":   ticket.UpdatedByRole,
		"requiere_revision": ticket.RequiereRevision,
		"commit_sha":        ticket.CommitSHA,
		"reaperturas":       ticket.Reaperturas,
	}
}

func mustJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(encoded)
}
