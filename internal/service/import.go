package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

var errImportDryRun = errors.New("import dry run")

// ImportPhase es una fase del archivo de importación.
type ImportPhase struct {
	Key         string
	Name        string
	Description string
	OrderIndex  *int64
}

// ImportEpic es una épica del archivo.
type ImportEpic struct {
	Key         string
	Title       string
	Description string
	Phase       string
	OrderIndex  *int64
}

// ImportTicket es un ticket del archivo.
type ImportTicket struct {
	Key         string
	Title       string
	Description string
	Priority    string
	Status      string
	Role        string
	Epic        string
	Phase       string
	OrderIndex  *int64
	Paths       []string
	DependsOn   []string
}

// ImportInput es el body de importación.
type ImportInput struct {
	Phases  []ImportPhase
	Epics   []ImportEpic
	Tickets []ImportTicket
	DryRun  bool
	Mode    string
}

// ImportSummary es el resultado del import.
type ImportSummary struct {
	PhasesCreated  int  `json:"phases_created"`
	EpicsCreated   int  `json:"epics_created"`
	TicketsCreated int  `json:"tickets_created"`
	TicketsUpdated int  `json:"tickets_updated"`
	TicketsSkipped int  `json:"tickets_skipped"`
	TicketsTotal   int  `json:"tickets_total"`
	DryRun         bool `json:"dry_run"`
}

// ImportPlan carga fases, épicas y tickets en una sola transacción.
func (s *Service) ImportPlan(ctx context.Context, actor Identity, projectRef string, input ImportInput) (ImportSummary, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return ImportSummary{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return ImportSummary{}, err
	}
	if !rights.CanWrite(project.ID) {
		return ImportSummary{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "proyecto.importar", project.ID)
	}
	if len(input.Tickets) > 500 {
		return ImportSummary{}, validation("el import admite como máximo 500 tickets")
	}
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "omitir"
	}
	if mode != "omitir" && mode != "actualizar" {
		return ImportSummary{}, validation("modo debe ser omitir o actualizar")
	}
	summary := ImportSummary{DryRun: input.DryRun, TicketsTotal: len(input.Tickets)}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		phaseIDs := map[string]string{}
		epicIDs := map[string]string{}
		ticketIDs := map[string]string{}

		for _, phase := range input.Phases {
			key := strings.TrimSpace(phase.Key)
			name := strings.TrimSpace(phase.Name)
			if name == "" {
				return validation("cada fase necesita name")
			}
			if err := validateNoSecrets(name, phase.Description); err != nil {
				return err
			}
			if key != "" {
				if _, ok := phaseIDs[key]; ok {
					return validation("fase repetida en el import: " + key)
				}
				if existing, err := phaseByKey(ctx, tx, project.ID, key); err == nil {
					phaseIDs[key] = existing.ID
					continue
				} else if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
			order := int64(0)
			if phase.OrderIndex != nil {
				order = *phase.OrderIndex
			} else if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(MAX(order_index), 0) + 1 FROM phases WHERE project_id = ?`,
				project.ID,
			).Scan(&order); err != nil {
				return fmt.Errorf("orden de fase: %w", err)
			}
			phaseID := id.New()
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO phases(id, project_id, key, name, description, order_index, status, created_by, created_by_role, created_at)
				VALUES (?, ?, ?, ?, ?, ?, 'abierta', ?, ?, ?)`,
				phaseID, project.ID, nullable(key), name, phase.Description, order,
				actor.Participant.ID, actor.Participant.AgentRole, timefmt.Format(now),
			); err != nil {
				return fmt.Errorf("importar fase: %w", err)
			}
			if key != "" {
				phaseIDs[key] = phaseID
			}
			summary.PhasesCreated++
			if _, err := audit.Append(ctx, tx, audit.Input{
				ChainID: project.ID, Actor: auditActor(actor),
				Action: "fase.crear", ObjectType: "fase", ObjectID: phaseID, ProjectID: project.ID,
				Payload:   map[string]any{"phase_id": phaseID, "key": key, "name": name, "source": "import"},
				CreatedAt: now,
			}); err != nil {
				return err
			}
		}

		for _, epic := range input.Epics {
			key := strings.TrimSpace(epic.Key)
			title := strings.TrimSpace(epic.Title)
			if title == "" {
				return validation("cada épica necesita title")
			}
			if err := validateNoSecrets(title, epic.Description); err != nil {
				return err
			}
			if key != "" {
				if _, ok := epicIDs[key]; ok {
					return validation("épica repetida en el import: " + key)
				}
				if existing, err := epicByKey(ctx, tx, project.ID, key); err == nil {
					epicIDs[key] = existing.ID
					continue
				} else if !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
			phaseID := ""
			if strings.TrimSpace(epic.Phase) != "" {
				resolved, ok := phaseIDs[epic.Phase]
				if !ok {
					phase, err := resolvePhase(ctx, tx, project.ID, epic.Phase)
					if err != nil {
						return err
					}
					resolved = phase.ID
				}
				phaseID = resolved
			}
			order := int64(0)
			if epic.OrderIndex != nil {
				order = *epic.OrderIndex
			} else if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(MAX(order_index), 0) + 1 FROM epics WHERE project_id = ?`,
				project.ID,
			).Scan(&order); err != nil {
				return fmt.Errorf("orden de épica: %w", err)
			}
			epicID := id.New()
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO epics(id, project_id, phase_id, key, title, description, order_index, status, created_by, created_by_role, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, 'abierta', ?, ?, ?)`,
				epicID, project.ID, nullable(phaseID), nullable(key), title, epic.Description,
				order, actor.Participant.ID, actor.Participant.AgentRole, timefmt.Format(now),
			); err != nil {
				return fmt.Errorf("importar épica: %w", err)
			}
			if key != "" {
				epicIDs[key] = epicID
			}
			summary.EpicsCreated++
			if _, err := audit.Append(ctx, tx, audit.Input{
				ChainID: project.ID, Actor: auditActor(actor),
				Action: "epica.crear", ObjectType: "epica", ObjectID: epicID, ProjectID: project.ID,
				Payload:   map[string]any{"epic_id": epicID, "key": key, "title": title, "source": "import"},
				CreatedAt: now,
			}); err != nil {
				return err
			}
		}

		nextNumber := int64(0)
		if err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(MAX(number), 0) + 1 FROM tickets WHERE project_id = ?`, project.ID,
		).Scan(&nextNumber); err != nil {
			return fmt.Errorf("número inicial de import: %w", err)
		}
		type dependency struct {
			ticketKey string
			dependsOn string
		}
		dependencies := make([]dependency, 0, len(input.Tickets))
		seenTicketKeys := make(map[string]struct{}, len(input.Tickets))
		for _, ticket := range input.Tickets {
			key := strings.TrimSpace(ticket.Key)
			title := strings.TrimSpace(ticket.Title)
			if title == "" {
				return validation("cada ticket necesita title")
			}
			if err := validateNoSecrets(title, ticket.Description); err != nil {
				return err
			}
			if key != "" {
				if !ticketKeyPattern.MatchString(key) {
					return validation("key de ticket inválida: " + key)
				}
				if _, duplicated := seenTicketKeys[key]; duplicated {
					return validation("ticket repetido en el import: " + key)
				}
				seenTicketKeys[key] = struct{}{}
			}
			priority := strings.TrimSpace(ticket.Priority)
			if priority == "" {
				priority = domain.PriorityMedium
			}
			if err := validatePriority(priority); err != nil {
				return err
			}
			status := strings.TrimSpace(ticket.Status)
			if status == "" {
				status = domain.TicketOpen
			}
			if err := validateTicketStatus(status); err != nil {
				return err
			}
			epicID := ""
			if strings.TrimSpace(ticket.Epic) != "" {
				resolved, ok := epicIDs[ticket.Epic]
				if !ok {
					epic, err := resolveEpic(ctx, tx, project.ID, ticket.Epic)
					if err != nil {
						return err
					}
					resolved = epic.ID
				}
				epicID = resolved
			}
			phaseID := ""
			if epicID == "" && strings.TrimSpace(ticket.Phase) != "" {
				resolved, ok := phaseIDs[ticket.Phase]
				if !ok {
					phase, err := resolvePhase(ctx, tx, project.ID, ticket.Phase)
					if err != nil {
						return err
					}
					resolved = phase.ID
				}
				phaseID = resolved
			}
			paths, err := normalizePaths(ticket.Paths)
			if err != nil {
				return err
			}
			role := strings.TrimSpace(ticket.Role)
			if role != "" {
				if err := registerRoles(ctx, tx, project.ID, []string{role}, now); err != nil {
					return err
				}
			}
			var existing domain.Ticket
			if key != "" {
				existing, err = ticketByKey(ctx, tx, project.ID, key)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
			}
			if existing.ID != "" {
				if mode == "omitir" {
					summary.TicketsSkipped++
					ticketIDs[key] = existing.ID
					if len(ticket.DependsOn) > 0 {
						dependencies = append(dependencies, dependency{ticketKey: key, dependsOn: strings.Join(ticket.DependsOn, "\x00")})
					}
					continue
				}
				existing.Title = title
				existing.Description = ticket.Description
				existing.Priority = priority
				existing.Status = status
				existing.AssignedRole = role
				existing.EpicID = epicID
				existing.PhaseID = phaseID
				if ticket.OrderIndex != nil {
					existing.OrderIndex = *ticket.OrderIndex
				}
				existing.Paths = paths
				existing.Version++
				existing.UpdatedAt = now
				if _, err := tx.ExecContext(ctx, `
					UPDATE tickets SET title = ?, description = ?, status = ?, priority = ?,
					       assigned_role = ?, phase_id = ?, epic_id = ?, order_index = ?,
					       version = ?, paths = ?, updated_by_role = ?, updated_at = ?
					WHERE id = ?`,
					existing.Title, existing.Description, existing.Status, existing.Priority,
					nullable(existing.AssignedRole), nullable(existing.PhaseID), nullable(existing.EpicID),
					existing.OrderIndex, existing.Version, mustJSON(existing.Paths),
					actor.Participant.AgentRole, timefmt.Format(now), existing.ID,
				); err != nil {
					return fmt.Errorf("actualizar ticket importado: %w", err)
				}
				summary.TicketsUpdated++
				ticketIDs[key] = existing.ID
				if _, err := audit.Append(ctx, tx, audit.Input{
					ChainID: project.ID, Actor: auditActor(actor),
					Action: "ticket.editar", ObjectType: "ticket", ObjectID: existing.ID, ProjectID: project.ID,
					Payload:   map[string]any{"ticket_id": existing.ID, "key": key, "source": "import"},
					CreatedAt: now,
				}); err != nil {
					return err
				}
			} else {
				ticketID := id.New()
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO tickets
						(id, project_id, number, key, title, description, status, priority,
						 assigned_role, phase_id, epic_id, order_index, version, paths, created_by, created_by_role, updated_by_role, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?)`,
					ticketID, project.ID, nextNumber, nullable(key), title, ticket.Description,
					status, priority, nullable(role), nullable(phaseID), nullable(epicID),
					int64Value(ticket.OrderIndex), mustJSON(paths), actor.Participant.ID,
					actor.Participant.AgentRole, actor.Participant.AgentRole,
					timefmt.Format(now), timefmt.Format(now),
				); err != nil {
					return fmt.Errorf("importar ticket: %w", err)
				}
				nextNumber++
				summary.TicketsCreated++
				ticketIDs[key] = ticketID
				if _, err := audit.Append(ctx, tx, audit.Input{
					ChainID: project.ID, Actor: auditActor(actor),
					Action: "ticket.crear", ObjectType: "ticket", ObjectID: ticketID, ProjectID: project.ID,
					Payload:   map[string]any{"ticket_id": ticketID, "key": key, "title": title, "source": "import"},
					CreatedAt: now,
				}); err != nil {
					return err
				}
			}
			if len(ticket.DependsOn) > 0 {
				dependencies = append(dependencies, dependency{ticketKey: key, dependsOn: strings.Join(ticket.DependsOn, "\x00")})
			}
		}

		for _, dep := range dependencies {
			ticketID := ticketIDs[dep.ticketKey]
			if ticketID == "" {
				if existing, err := ticketByKey(ctx, tx, project.ID, dep.ticketKey); err == nil {
					ticketID = existing.ID
				} else {
					return validation("ticket de dependencia no encontrado: " + dep.ticketKey)
				}
			}
			for _, ref := range strings.Split(dep.dependsOn, "\x00") {
				ref = strings.TrimSpace(ref)
				if ref == "" {
					continue
				}
				depID := ticketIDs[ref]
				var depContext ticketContext
				if depID == "" {
					resolved, err := resolveTicket(ctx, tx, ref)
					if err != nil {
						return err
					}
					depID = resolved.Ticket.ID
					depContext = resolved
				} else {
					ticket, err := ticketByID(ctx, tx, depID)
					if err != nil {
						return err
					}
					project, err := resolveProject(ctx, tx, ticket.ProjectID)
					if err != nil {
						return err
					}
					depContext = ticketContext{Ticket: ticket, Project: project}
				}
				if depID == ticketID {
					return validation("un ticket no puede depender de sí mismo")
				}
				if _, err := tx.ExecContext(ctx, `
					INSERT OR IGNORE INTO ticket_edges(ticket_id, depends_on_id, created_by, created_by_role, created_at)
					VALUES (?, ?, ?, ?, ?)`, ticketID, depID, actor.Participant.ID,
					actor.Participant.AgentRole, timefmt.Format(now),
				); err != nil {
					return fmt.Errorf("importar dependencia: %w", err)
				}
				if _, err := audit.Append(ctx, tx, audit.Input{
					ChainID: project.ID, Actor: auditActor(actor),
					Action: "ticket.dependencia.crear", ObjectType: "ticket", ObjectID: ticketID, ProjectID: project.ID,
					Payload:   map[string]any{"ticket_id": ticketID, "depends_on_id": depID, "source": "import"},
					CreatedAt: now,
				}); err != nil {
					return err
				}
				_ = depContext
			}
		}
		for _, ticketID := range ticketIDs {
			if err := ensureNoCycle(ctx, tx, ticketID); err != nil {
				return err
			}
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: project.ID, Actor: auditActor(actor),
			Action: "proyecto.importar", ObjectType: "proyecto", ObjectID: project.ID, ProjectID: project.ID,
			Payload: map[string]any{
				"phases_created": summary.PhasesCreated, "epics_created": summary.EpicsCreated,
				"tickets_created": summary.TicketsCreated, "tickets_updated": summary.TicketsUpdated,
				"tickets_skipped": summary.TicketsSkipped, "dry_run": summary.DryRun, "mode": mode,
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		if input.DryRun {
			return errImportDryRun
		}
		return nil
	})
	if errors.Is(err, errImportDryRun) {
		return summary, nil
	}
	if err != nil {
		return ImportSummary{}, err
	}
	return summary, nil
}

func phaseByKey(ctx context.Context, q queryer, projectID, key string) (domain.Phase, error) {
	return scanPhase(q.QueryRowContext(ctx, `
		SELECT id, project_id, COALESCE(key, ''), name, description, order_index, status,
		       COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM phases WHERE project_id = ? AND key = ?`, projectID, key))
}

func epicByKey(ctx context.Context, q queryer, projectID, key string) (domain.Epic, error) {
	return scanEpic(q.QueryRowContext(ctx, `
		SELECT id, project_id, COALESCE(phase_id, ''), COALESCE(key, ''), title, description,
		       order_index, status, COALESCE(created_by, ''), created_by_role, created_at, closed_at
		FROM epics WHERE project_id = ? AND key = ?`, projectID, key))
}

func ticketByKey(ctx context.Context, q queryer, projectID, key string) (domain.Ticket, error) {
	return scanTicket(q.QueryRowContext(ctx, `
		SELECT id, project_id, number, COALESCE(key, ''), title, description,
		       status, priority, COALESCE(assignee_id, ''), COALESCE(assigned_role, ''),
		       COALESCE(phase_id, ''), COALESCE(epic_id, ''), order_index, version,
		       paths, COALESCE(discovered_in, ''), COALESCE(created_by, ''), COALESCE(created_by_role, ''), COALESCE(updated_by_role, ''),
		       created_at, updated_at
		FROM tickets WHERE project_id = ? AND key = ?`, projectID, key))
}

func int64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}
