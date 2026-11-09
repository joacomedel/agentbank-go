package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// PhaseInput es el alta de una fase.
type PhaseInput struct {
	Key         string
	Name        string
	Description string
	OrderIndex  *int64
}

// PhaseUpdate son los campos editables de una fase.
type PhaseUpdate struct {
	Key         *string
	Name        *string
	Description *string
	OrderIndex  *int64
	Status      *string
}

// PhaseView es una fase con progreso y contenido opcional.
type PhaseView struct {
	ID            string        `json:"id"`
	ProjectID     string        `json:"project_id"`
	Key           string        `json:"key,omitempty"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	OrderIndex    int64         `json:"order_index"`
	Status        string        `json:"status"`
	CreatedByRole string        `json:"created_by_role,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	ClosedAt      *time.Time    `json:"closed_at,omitempty"`
	Progress      PhaseProgress `json:"progress"`
	Epics         []EpicView    `json:"epics,omitempty"`
	Tickets       []TicketView  `json:"tickets,omitempty"`
}

// PhaseProgress cuenta tickets por estado efectivo en la fase.
type PhaseProgress struct {
	Total      int `json:"total"`
	Open       int `json:"open"`
	InProgress int `json:"in_progress"`
	Done       int `json:"done"`
	Cancelled  int `json:"cancelled"`
}

// EpicInput es el alta de una épica.
type EpicInput struct {
	Key         string
	Title       string
	Description string
	PhaseID     string
	OrderIndex  *int64
}

// EpicUpdate son los campos editables de una épica.
type EpicUpdate struct {
	Key         *string
	Title       *string
	Description *string
	PhaseID     *string
	OrderIndex  *int64
	Status      *string
}

// EpicView es una épica con sus tickets.
type EpicView struct {
	ID            string       `json:"id"`
	ProjectID     string       `json:"project_id"`
	PhaseID       string       `json:"phase_id,omitempty"`
	Key           string       `json:"key,omitempty"`
	Title         string       `json:"title"`
	Description   string       `json:"description"`
	OrderIndex    int64        `json:"order_index"`
	Status        string       `json:"status"`
	CreatedByRole string       `json:"created_by_role,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	ClosedAt      *time.Time   `json:"closed_at,omitempty"`
	Tickets       []TicketView `json:"tickets,omitempty"`
}

// CreatePhase crea una fase.
func (s *Service) CreatePhase(ctx context.Context, actor Identity, projectRef string, input PhaseInput) (PhaseView, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return PhaseView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return PhaseView{}, err
	}
	if !rights.CanWrite(project.ID) {
		return PhaseView{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "fase.crear", "")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 200 {
		return PhaseView{}, validation("name debe tener entre 1 y 200 caracteres")
	}
	key := strings.TrimSpace(input.Key)
	if key != "" && !ticketKeyPattern.MatchString(key) {
		return PhaseView{}, validation("key inválida")
	}
	var view PhaseView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		order := int64(0)
		if input.OrderIndex != nil {
			order = *input.OrderIndex
		} else if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(order_index), 0) + 1 FROM phases WHERE project_id = ?`,
			project.ID,
		).Scan(&order); err != nil {
			return fmt.Errorf("calcular orden de fase: %w", err)
		}
		phase := domain.Phase{
			ID: id.New(), ProjectID: project.ID, Key: key, Name: name,
			Description: input.Description, OrderIndex: order, Status: domain.PhaseOpen,
			CreatedBy: actor.Participant.ID, CreatedByRole: actor.Participant.AgentRole,
			CreatedAt: now,
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO phases(id, project_id, key, name, description, order_index, status, created_by, created_by_role, created_at)
			VALUES (?, ?, ?, ?, ?, ?, 'abierta', ?, ?, ?)`,
			phase.ID, phase.ProjectID, nullable(phase.Key), phase.Name, phase.Description,
			phase.OrderIndex, nullable(phase.CreatedBy), phase.CreatedByRole, timefmt.Format(now),
		); err != nil {
			if isUniqueConstraint(err) {
				return conflict("phase_key_taken", "ya existe una fase con esa key")
			}
			return fmt.Errorf("crear fase: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: project.ID, Actor: auditActor(actor),
			Action: "fase.crear", ObjectType: "fase", ObjectID: phase.ID, ProjectID: project.ID,
			Payload:   map[string]any{"phase_id": phase.ID, "name": phase.Name, "order": phase.OrderIndex},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		view = phaseView(phase, PhaseProgress{})
		return nil
	})
	if err != nil {
		return PhaseView{}, err
	}
	return view, nil
}

// ListPhases lista fases de un proyecto.
func (s *Service) ListPhases(ctx context.Context, actor Identity, projectRef string) ([]PhaseView, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !rights.CanRead(project.ID) {
		return nil, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "fase.listar", project.ID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, COALESCE(key, ''), name, description, order_index, status,
		       COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM phases WHERE project_id = ? ORDER BY order_index, name LIMIT 200`, project.ID)
	if err != nil {
		return nil, fmt.Errorf("listar fases: %w", err)
	}
	defer rows.Close()
	phases := make([]domain.Phase, 0, 8)
	for rows.Next() {
		phase, err := scanPhase(rows)
		if err != nil {
			return nil, err
		}
		phases = append(phases, phase)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer fases: %w", err)
	}
	views := make([]PhaseView, 0, len(phases))
	for _, phase := range phases {
		progress, err := s.phaseProgress(ctx, s.db, phase.ID)
		if err != nil {
			return nil, err
		}
		views = append(views, phaseView(phase, progress))
	}
	return views, nil
}

// GetPhase devuelve la fase con sus épicas y tickets sueltos.
func (s *Service) GetPhase(ctx context.Context, actor Identity, ref string) (PhaseView, error) {
	phase, project, err := resolvePhaseWithProject(ctx, s.db, ref)
	if err != nil {
		return PhaseView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return PhaseView{}, err
	}
	if !rights.CanRead(project.ID) {
		return PhaseView{}, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "fase.leer", phase.ID)
	}
	progress, err := s.phaseProgress(ctx, s.db, phase.ID)
	if err != nil {
		return PhaseView{}, err
	}
	view := phaseView(phase, progress)
	epics, err := s.listEpicsForPhase(ctx, project, phase.ID)
	if err != nil {
		return PhaseView{}, err
	}
	view.Epics = epics
	tickets, err := s.listPhaseLooseTickets(ctx, actor, project, phase.ID)
	if err != nil {
		return PhaseView{}, err
	}
	view.Tickets = tickets
	return view, nil
}

// UpdatePhase edita o cierra una fase.
func (s *Service) UpdatePhase(ctx context.Context, actor Identity, ref string, update PhaseUpdate) (PhaseView, error) {
	phase, project, err := resolvePhaseWithProject(ctx, s.db, ref)
	if err != nil {
		return PhaseView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return PhaseView{}, err
	}
	if !rights.CanWrite(project.ID) {
		return PhaseView{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "fase.editar", phase.ID)
	}
	var view PhaseView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		current, err := phaseByID(ctx, tx, phase.ID)
		if err != nil {
			return err
		}
		previous := map[string]any{"name": current.Name, "description": current.Description, "order": current.OrderIndex, "status": current.Status}
		if update.Key != nil {
			key := strings.TrimSpace(*update.Key)
			if key != "" && !ticketKeyPattern.MatchString(key) {
				return validation("key inválida")
			}
			current.Key = key
		}
		if update.Name != nil {
			name := strings.TrimSpace(*update.Name)
			if name == "" || len([]rune(name)) > 200 {
				return validation("name debe tener entre 1 y 200 caracteres")
			}
			current.Name = name
		}
		if update.Description != nil {
			current.Description = *update.Description
		}
		if update.OrderIndex != nil {
			current.OrderIndex = *update.OrderIndex
		}
		action := "fase.editar"
		var pending []map[string]any
		if update.Status != nil {
			status := strings.TrimSpace(*update.Status)
			if status != domain.PhaseOpen && status != domain.PhaseClosed {
				return validation("status debe ser abierta o cerrada")
			}
			if status == domain.PhaseClosed && current.Status != domain.PhaseClosed {
				pending, err = s.pendingTicketsForPhase(ctx, tx, current.ID)
				if err != nil {
					return err
				}
				now := s.now().UTC()
				current.ClosedAt = &now
				action = "fase.cerrar"
			}
			if status == domain.PhaseOpen {
				current.ClosedAt = nil
			}
			current.Status = status
		}
		now := s.now().UTC()
		if _, err := tx.ExecContext(ctx, `
			UPDATE phases SET key = ?, name = ?, description = ?, order_index = ?, status = ?, closed_at = ?
			WHERE id = ?`,
			nullable(current.Key), current.Name, current.Description, current.OrderIndex,
			current.Status, nullableTime(current.ClosedAt), current.ID,
		); err != nil {
			if isUniqueConstraint(err) {
				return conflict("phase_key_taken", "ya existe una fase con esa key")
			}
			return fmt.Errorf("actualizar fase: %w", err)
		}
		payload := map[string]any{"phase_id": current.ID, "previous": previous, "new": map[string]any{
			"name": current.Name, "description": current.Description, "order": current.OrderIndex, "status": current.Status,
		}}
		if len(pending) > 0 {
			payload["pendientes"] = pending
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: project.ID, Actor: auditActor(actor),
			Action: action, ObjectType: "fase", ObjectID: current.ID, ProjectID: project.ID,
			Payload: payload, CreatedAt: now,
		}); err != nil {
			return err
		}
		progress, err := s.phaseProgress(ctx, tx, current.ID)
		if err != nil {
			return err
		}
		view = phaseView(current, progress)
		return nil
	})
	if err != nil {
		return PhaseView{}, err
	}
	return view, nil
}

// CreateEpic crea una épica.
func (s *Service) CreateEpic(ctx context.Context, actor Identity, projectRef string, input EpicInput) (EpicView, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return EpicView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return EpicView{}, err
	}
	if !rights.CanWrite(project.ID) {
		return EpicView{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "epica.crear", "")
	}
	title := strings.TrimSpace(input.Title)
	if title == "" || len([]rune(title)) > 200 {
		return EpicView{}, validation("title debe tener entre 1 y 200 caracteres")
	}
	key := strings.TrimSpace(input.Key)
	if key != "" && !ticketKeyPattern.MatchString(key) {
		return EpicView{}, validation("key inválida")
	}
	var view EpicView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		phaseID := ""
		if strings.TrimSpace(input.PhaseID) != "" {
			phase, err := resolvePhase(ctx, tx, project.ID, input.PhaseID)
			if err != nil {
				return err
			}
			phaseID = phase.ID
		}
		order := int64(0)
		if input.OrderIndex != nil {
			order = *input.OrderIndex
		} else if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(order_index), 0) + 1 FROM epics WHERE project_id = ?`,
			project.ID,
		).Scan(&order); err != nil {
			return fmt.Errorf("calcular orden de épica: %w", err)
		}
		epic := domain.Epic{
			ID: id.New(), ProjectID: project.ID, PhaseID: phaseID, Key: key, Title: title,
			Description: input.Description, OrderIndex: order, Status: domain.PhaseOpen,
			CreatedBy: actor.Participant.ID, CreatedByRole: actor.Participant.AgentRole,
			CreatedAt: now,
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO epics(id, project_id, phase_id, key, title, description, order_index, status, created_by, created_by_role, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'abierta', ?, ?, ?)`,
			epic.ID, epic.ProjectID, nullable(epic.PhaseID), nullable(epic.Key), epic.Title,
			epic.Description, epic.OrderIndex, nullable(epic.CreatedBy),
			epic.CreatedByRole, timefmt.Format(now),
		); err != nil {
			if isUniqueConstraint(err) {
				return conflict("epic_key_taken", "ya existe una épica con esa key")
			}
			return fmt.Errorf("crear épica: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: project.ID, Actor: auditActor(actor),
			Action: "epica.crear", ObjectType: "epica", ObjectID: epic.ID, ProjectID: project.ID,
			Payload:   map[string]any{"epic_id": epic.ID, "title": epic.Title, "phase_id": epic.PhaseID},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		view = epicView(epic)
		return nil
	})
	if err != nil {
		return EpicView{}, err
	}
	return view, nil
}

// ListEpics lista épicas de un proyecto, opcionalmente por fase.
func (s *Service) ListEpics(ctx context.Context, actor Identity, projectRef, phaseRef string) ([]EpicView, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !rights.CanRead(project.ID) {
		return nil, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "epica.listar", project.ID)
	}
	args := []any{project.ID}
	where := ""
	if strings.TrimSpace(phaseRef) != "" {
		phase, err := resolvePhase(ctx, s.db, project.ID, phaseRef)
		if err != nil {
			return nil, err
		}
		where = " AND phase_id = ?"
		args = append(args, phase.ID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, COALESCE(phase_id, ''), COALESCE(key, ''), title, description,
		       order_index, status, COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM epics WHERE project_id = ?`+where+`
		ORDER BY order_index, title LIMIT 200`, args...)
	if err != nil {
		return nil, fmt.Errorf("listar épicas: %w", err)
	}
	defer rows.Close()
	views := make([]EpicView, 0, 8)
	for rows.Next() {
		epic, err := scanEpic(rows)
		if err != nil {
			return nil, err
		}
		views = append(views, epicView(epic))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer épicas: %w", err)
	}
	return views, nil
}

// GetEpic devuelve la épica con sus tickets.
func (s *Service) GetEpic(ctx context.Context, actor Identity, ref string) (EpicView, error) {
	epic, project, err := resolveEpicWithProject(ctx, s.db, ref)
	if err != nil {
		return EpicView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return EpicView{}, err
	}
	if !rights.CanRead(project.ID) {
		return EpicView{}, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "epica.leer", epic.ID)
	}
	view := epicView(epic)
	tickets, err := s.listEpicTickets(ctx, actor, project, epic.ID)
	if err != nil {
		return EpicView{}, err
	}
	view.Tickets = tickets
	return view, nil
}

// UpdateEpic edita, mueve de fase o cierra una épica.
func (s *Service) UpdateEpic(ctx context.Context, actor Identity, ref string, update EpicUpdate) (EpicView, error) {
	epic, project, err := resolveEpicWithProject(ctx, s.db, ref)
	if err != nil {
		return EpicView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return EpicView{}, err
	}
	if !rights.CanWrite(project.ID) {
		return EpicView{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "epica.editar", epic.ID)
	}
	var view EpicView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		current, err := epicByID(ctx, tx, epic.ID)
		if err != nil {
			return err
		}
		previous := map[string]any{"title": current.Title, "description": current.Description, "phase_id": current.PhaseID, "status": current.Status, "order": current.OrderIndex}
		if update.Key != nil {
			key := strings.TrimSpace(*update.Key)
			if key != "" && !ticketKeyPattern.MatchString(key) {
				return validation("key inválida")
			}
			current.Key = key
		}
		if update.Title != nil {
			title := strings.TrimSpace(*update.Title)
			if title == "" || len([]rune(title)) > 200 {
				return validation("title debe tener entre 1 y 200 caracteres")
			}
			current.Title = title
		}
		if update.Description != nil {
			current.Description = *update.Description
		}
		if update.OrderIndex != nil {
			current.OrderIndex = *update.OrderIndex
		}
		action := "epica.editar"
		if update.PhaseID != nil {
			phaseRef := strings.TrimSpace(*update.PhaseID)
			if phaseRef == "" {
				current.PhaseID = ""
			} else {
				phase, err := resolvePhase(ctx, tx, project.ID, phaseRef)
				if err != nil {
					return err
				}
				current.PhaseID = phase.ID
			}
			action = "epic.mover_fase"
		}
		var pending []map[string]any
		if update.Status != nil {
			status := strings.TrimSpace(*update.Status)
			if status != domain.PhaseOpen && status != domain.PhaseClosed {
				return validation("status debe ser abierta o cerrada")
			}
			if status == domain.PhaseClosed && current.Status != domain.PhaseClosed {
				pending, err = s.pendingTicketsForEpic(ctx, tx, current.ID)
				if err != nil {
					return err
				}
				now := s.now().UTC()
				current.ClosedAt = &now
				action = "epica.cerrar"
			}
			if status == domain.PhaseOpen {
				current.ClosedAt = nil
			}
			current.Status = status
		}
		now := s.now().UTC()
		if _, err := tx.ExecContext(ctx, `
			UPDATE epics SET key = ?, title = ?, description = ?, phase_id = ?, order_index = ?, status = ?, closed_at = ?
			WHERE id = ?`,
			nullable(current.Key), current.Title, current.Description, nullable(current.PhaseID),
			current.OrderIndex, current.Status, nullableTime(current.ClosedAt), current.ID,
		); err != nil {
			if isUniqueConstraint(err) {
				return conflict("epic_key_taken", "ya existe una épica con esa key")
			}
			return fmt.Errorf("actualizar épica: %w", err)
		}
		payload := map[string]any{"epic_id": current.ID, "previous": previous, "new": map[string]any{
			"title": current.Title, "description": current.Description, "phase_id": current.PhaseID,
			"status": current.Status, "order": current.OrderIndex,
		}}
		if len(pending) > 0 {
			payload["tickets_no_hechos"] = pending
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: project.ID, Actor: auditActor(actor),
			Action: action, ObjectType: "epica", ObjectID: current.ID, ProjectID: project.ID,
			Payload: payload, CreatedAt: now,
		}); err != nil {
			return err
		}
		view = epicView(current)
		return nil
	})
	if err != nil {
		return EpicView{}, err
	}
	return view, nil
}

func (s *Service) phaseProgress(ctx context.Context, q queryer, phaseID string) (PhaseProgress, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT t.status, COUNT(1)
		FROM tickets t
		LEFT JOIN epics e ON e.id = t.epic_id
		WHERE COALESCE(e.phase_id, t.phase_id) = ?
		GROUP BY t.status`, phaseID)
	if err != nil {
		return PhaseProgress{}, fmt.Errorf("calcular progreso de fase: %w", err)
	}
	defer rows.Close()
	var progress PhaseProgress
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return PhaseProgress{}, fmt.Errorf("escanear progreso: %w", err)
		}
		progress.Total += count
		switch status {
		case domain.TicketDone:
			progress.Done += count
		case domain.TicketCancelled:
			progress.Cancelled += count
		case domain.TicketInProgress, domain.TicketInReview:
			progress.InProgress += count
		default:
			progress.Open += count
		}
	}
	if err := rows.Err(); err != nil {
		return PhaseProgress{}, fmt.Errorf("recorrer progreso: %w", err)
	}
	return progress, nil
}

func (s *Service) pendingTicketsForPhase(ctx context.Context, q queryer, phaseID string) ([]map[string]any, error) {
	return s.pendingTickets(ctx, q, `
		SELECT t.id, t.number, p.prefix, t.title, t.status
		FROM tickets t
		JOIN projects p ON p.id = t.project_id
		LEFT JOIN epics e ON e.id = t.epic_id
		WHERE COALESCE(e.phase_id, t.phase_id) = ? AND t.status NOT IN ('hecho', 'cancelado')
		ORDER BY p.prefix, t.number`, phaseID)
}

func (s *Service) pendingTicketsForEpic(ctx context.Context, q queryer, epicID string) ([]map[string]any, error) {
	return s.pendingTickets(ctx, q, `
		SELECT t.id, t.number, p.prefix, t.title, t.status
		FROM tickets t
		JOIN projects p ON p.id = t.project_id
		WHERE t.epic_id = ? AND t.status NOT IN ('hecho', 'cancelado')
		ORDER BY p.prefix, t.number`, epicID)
}

func (s *Service) pendingTickets(ctx context.Context, q queryer, query string, id string) ([]map[string]any, error) {
	rows, err := q.QueryContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("listar pendientes: %w", err)
	}
	defer rows.Close()
	items := make([]map[string]any, 0, 4)
	for rows.Next() {
		var ticketID, prefix, title, status string
		var number int64
		if err := rows.Scan(&ticketID, &number, &prefix, &title, &status); err != nil {
			return nil, fmt.Errorf("escanear pendiente: %w", err)
		}
		items = append(items, map[string]any{
			"ticket_id": ticketID, "ref": fmt.Sprintf("%s-%d", prefix, number),
			"title": title, "status": status,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer pendientes: %w", err)
	}
	return items, nil
}

func (s *Service) listEpicsForPhase(ctx context.Context, project domain.Project, phaseID string) ([]EpicView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, COALESCE(phase_id, ''), COALESCE(key, ''), title, description,
		       order_index, status, COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM epics WHERE project_id = ? AND phase_id = ? ORDER BY order_index, title`, project.ID, phaseID)
	if err != nil {
		return nil, fmt.Errorf("listar épicas de fase: %w", err)
	}
	defer rows.Close()
	views := make([]EpicView, 0, 4)
	for rows.Next() {
		epic, err := scanEpic(rows)
		if err != nil {
			return nil, err
		}
		views = append(views, epicView(epic))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer épicas de fase: %w", err)
	}
	return views, nil
}

func (s *Service) listPhaseLooseTickets(ctx context.Context, actor Identity, project domain.Project, phaseID string) ([]TicketView, error) {
	return s.listTicketsByQuery(ctx, project, `
		SELECT t.id, t.project_id, t.number, COALESCE(t.key, ''), t.title, t.description,
		       t.status, t.priority, COALESCE(t.assignee_id, ''), COALESCE(t.assigned_role, ''),
		       COALESCE(t.phase_id, ''), COALESCE(t.epic_id, ''), t.order_index, t.version,
		       t.paths, COALESCE(t.discovered_in, ''), COALESCE(t.created_by, ''), COALESCE(t.created_by_role, ''), COALESCE(t.updated_by_role, ''), t.requiere_revision, t.commit_sha, t.reaperturas,
		       t.created_at, t.updated_at
		FROM tickets t
		WHERE t.phase_id = ? AND t.epic_id IS NULL
		ORDER BY t.order_index, t.created_at`,
		phaseID)
}

func (s *Service) listEpicTickets(ctx context.Context, actor Identity, project domain.Project, epicID string) ([]TicketView, error) {
	return s.listTicketsByQuery(ctx, project, `
		SELECT id, project_id, number, COALESCE(key, ''), title, description,
		       status, priority, COALESCE(assignee_id, ''), COALESCE(assigned_role, ''),
		       COALESCE(phase_id, ''), COALESCE(epic_id, ''), order_index, version,
		       paths, COALESCE(discovered_in, ''), COALESCE(created_by, ''), COALESCE(created_by_role, ''), COALESCE(updated_by_role, ''), requiere_revision, commit_sha, reaperturas,
		       created_at, updated_at
		FROM tickets WHERE epic_id = ?
		ORDER BY order_index, created_at`,
		epicID)
}

func (s *Service) listTicketsByQuery(ctx context.Context, project domain.Project, query string, arg any) ([]TicketView, error) {
	rows, err := s.db.QueryContext(ctx, query, arg)
	if err != nil {
		return nil, fmt.Errorf("listar tickets: %w", err)
	}
	defer rows.Close()
	tickets := make([]domain.Ticket, 0, 8)
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer tickets: %w", err)
	}
	views := make([]TicketView, 0, len(tickets))
	for _, ticket := range tickets {
		status, err := s.effectiveStatus(ctx, s.db, ticket)
		if err != nil {
			return nil, err
		}
		views = append(views, baseTicketView(ticket, project, status))
	}
	return views, nil
}

func resolvePhase(ctx context.Context, q queryer, projectID, ref string) (domain.Phase, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return domain.Phase{}, notFound("phase_not_found", "fase no encontrada")
	}
	phase, err := scanPhase(q.QueryRowContext(ctx, `
		SELECT id, project_id, COALESCE(key, ''), name, description, order_index, status,
		       COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM phases WHERE project_id = ? AND (id = ? OR key = ?)
		ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END LIMIT 1`,
		projectID, ref, ref, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Phase{}, notFound("phase_not_found", "fase no encontrada")
	}
	if err != nil {
		return domain.Phase{}, err
	}
	return phase, nil
}

func resolvePhaseWithProject(ctx context.Context, q queryer, ref string) (domain.Phase, domain.Project, error) {
	phase, err := scanPhase(q.QueryRowContext(ctx, `
		SELECT id, project_id, COALESCE(key, ''), name, description, order_index, status,
		       COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM phases WHERE id = ? OR key = ? LIMIT 1`, ref, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Phase{}, domain.Project{}, notFound("phase_not_found", "fase no encontrada")
	}
	if err != nil {
		return domain.Phase{}, domain.Project{}, err
	}
	project, err := resolveProject(ctx, q, phase.ProjectID)
	if err != nil {
		return domain.Phase{}, domain.Project{}, err
	}
	return phase, project, nil
}

func phaseByID(ctx context.Context, q queryer, phaseID string) (domain.Phase, error) {
	phase, err := scanPhase(q.QueryRowContext(ctx, `
		SELECT id, project_id, COALESCE(key, ''), name, description, order_index, status,
		       COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM phases WHERE id = ?`, phaseID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Phase{}, notFound("phase_not_found", "fase no encontrada")
	}
	if err != nil {
		return domain.Phase{}, err
	}
	return phase, nil
}

func resolveEpic(ctx context.Context, q queryer, projectID, ref string) (domain.Epic, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return domain.Epic{}, notFound("epic_not_found", "épica no encontrada")
	}
	epic, err := scanEpic(q.QueryRowContext(ctx, `
		SELECT id, project_id, COALESCE(phase_id, ''), COALESCE(key, ''), title, description,
		       order_index, status, COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM epics WHERE project_id = ? AND (id = ? OR key = ?)
		ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END LIMIT 1`,
		projectID, ref, ref, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Epic{}, notFound("epic_not_found", "épica no encontrada")
	}
	if err != nil {
		return domain.Epic{}, err
	}
	return epic, nil
}

func resolveEpicWithProject(ctx context.Context, q queryer, ref string) (domain.Epic, domain.Project, error) {
	epic, err := scanEpic(q.QueryRowContext(ctx, `
		SELECT id, project_id, COALESCE(phase_id, ''), COALESCE(key, ''), title, description,
		       order_index, status, COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM epics WHERE id = ? OR key = ? LIMIT 1`, ref, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Epic{}, domain.Project{}, notFound("epic_not_found", "épica no encontrada")
	}
	if err != nil {
		return domain.Epic{}, domain.Project{}, err
	}
	project, err := resolveProject(ctx, q, epic.ProjectID)
	if err != nil {
		return domain.Epic{}, domain.Project{}, err
	}
	return epic, project, nil
}

func epicByID(ctx context.Context, q queryer, epicID string) (domain.Epic, error) {
	epic, err := scanEpic(q.QueryRowContext(ctx, `
		SELECT id, project_id, COALESCE(phase_id, ''), COALESCE(key, ''), title, description,
		       order_index, status, COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM epics WHERE id = ?`, epicID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Epic{}, notFound("epic_not_found", "épica no encontrada")
	}
	if err != nil {
		return domain.Epic{}, err
	}
	return epic, nil
}

func scanPhase(row interface{ Scan(...any) error }) (domain.Phase, error) {
	var phase domain.Phase
	var createdAt string
	var closedAt sql.NullString
	if err := row.Scan(&phase.ID, &phase.ProjectID, &phase.Key, &phase.Name, &phase.Description,
		&phase.OrderIndex, &phase.Status, &phase.CreatedBy, &phase.CreatedByRole,
		&createdAt, &closedAt); err != nil {
		return domain.Phase{}, err
	}
	parsed, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Phase{}, fmt.Errorf("created_at de fase inválido: %w", err)
	}
	phase.CreatedAt = parsed
	phase.ClosedAt = parseNullableTime(closedAt)
	return phase, nil
}

func scanEpic(row interface{ Scan(...any) error }) (domain.Epic, error) {
	var epic domain.Epic
	var createdAt string
	var closedAt sql.NullString
	if err := row.Scan(&epic.ID, &epic.ProjectID, &epic.PhaseID, &epic.Key, &epic.Title,
		&epic.Description, &epic.OrderIndex, &epic.Status, &epic.CreatedBy,
		&epic.CreatedByRole, &createdAt, &closedAt); err != nil {
		return domain.Epic{}, err
	}
	parsed, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Epic{}, fmt.Errorf("created_at de épica inválido: %w", err)
	}
	epic.CreatedAt = parsed
	epic.ClosedAt = parseNullableTime(closedAt)
	return epic, nil
}

func phaseView(phase domain.Phase, progress PhaseProgress) PhaseView {
	return PhaseView{
		ID: phase.ID, ProjectID: phase.ProjectID, Key: phase.Key, Name: phase.Name,
		Description: phase.Description, OrderIndex: phase.OrderIndex, Status: phase.Status,
		CreatedByRole: phase.CreatedByRole,
		CreatedAt:     phase.CreatedAt, ClosedAt: phase.ClosedAt, Progress: progress,
	}
}

func epicView(epic domain.Epic) EpicView {
	return EpicView{
		ID: epic.ID, ProjectID: epic.ProjectID, PhaseID: epic.PhaseID, Key: epic.Key,
		Title: epic.Title, Description: epic.Description, OrderIndex: epic.OrderIndex,
		Status: epic.Status, CreatedByRole: epic.CreatedByRole,
		CreatedAt: epic.CreatedAt, ClosedAt: epic.ClosedAt,
	}
}

func parseNullableTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed, err := timefmt.Parse(value.String)
	if err != nil {
		return nil
	}
	return &parsed
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return timefmt.Format(*value)
}
