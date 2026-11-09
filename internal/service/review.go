package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

var commitSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

var evidenceSections = []string{
	"Solución:",
	"Cambios:",
	"Verificación:",
	"Artefactos:",
	"Riesgos:",
}

// ReviewInput es el body de POST /v1/tickets/{id}/revision.
type ReviewInput struct {
	Veredicto string
	Comment   string
	Version   *int64
}

// validateTransition aplica la máquina de estados del ADR 0007.
// No incluye el veredicto de auditoría: ese camino lo maneja ReviewTicket.
func validateTransition(from, to string, requiereRevision bool) error {
	if from == to {
		return nil
	}
	allowed := false
	switch from {
	case domain.TicketOpen:
		allowed = to == domain.TicketBlocked || to == domain.TicketCancelled
	case domain.TicketInProgress:
		allowed = to == domain.TicketInReview || to == domain.TicketOpen ||
			to == domain.TicketBlocked || to == domain.TicketCancelled ||
			(to == domain.TicketDone && !requiereRevision)
	case domain.TicketInReview:
		allowed = to == domain.TicketOpen || to == domain.TicketCancelled
	case domain.TicketBlocked:
		allowed = to == domain.TicketOpen || to == domain.TicketInProgress ||
			to == domain.TicketCancelled
	}
	if allowed {
		return nil
	}
	message := fmt.Sprintf("transición inválida: %s → %s", from, to)
	if to == domain.TicketDone && from == domain.TicketInProgress {
		message += " (pasá por en revisión)"
	}
	if to == domain.TicketDone && from == domain.TicketInReview {
		message += " (usá POST /v1/tickets/{id}/revision)"
	}
	return conflict("transicion_invalida", message)
}

// requiresEvidence indica si la transición exige nota de cierre y SHA.
func requiresEvidence(from, to string, requiereRevision bool) bool {
	if from == to {
		return false
	}
	if to == domain.TicketInReview {
		return true
	}
	return to == domain.TicketDone && from == domain.TicketInProgress && !requiereRevision
}

// validateClosingEvidence valida las cinco secciones y, si corresponde, el SHA.
func validateClosingEvidence(note string, requireSHA bool, sha string) error {
	content := map[string]string{}
	current := ""
	for _, raw := range strings.Split(note, "\n") {
		line := strings.TrimSpace(raw)
		matched := false
		for _, label := range evidenceSections {
			if strings.HasPrefix(line, label) {
				current = label
				content[label] += strings.TrimSpace(strings.TrimPrefix(line, label)) + "\n"
				matched = true
				break
			}
		}
		if !matched && current != "" {
			content[current] += line + "\n"
		}
	}
	missing := make([]string, 0, len(evidenceSections)+1)
	for _, label := range evidenceSections {
		if strings.TrimSpace(content[label]) == "" {
			missing = append(missing, strings.TrimSuffix(label, ":"))
		}
	}
	sha = strings.TrimSpace(sha)
	if requireSHA && sha == "" {
		missing = append(missing, "commit_sha")
	} else if sha != "" && !commitSHAPattern.MatchString(sha) {
		missing = append(missing, "commit_sha (7-40 hex)")
	}
	if len(missing) > 0 {
		return validationCode("evidencia_incompleta",
			"evidencia incompleta; falta: "+strings.Join(missing, ", "))
	}
	return nil
}

func maxReaperturas() int {
	raw := strings.TrimSpace(os.Getenv("AGENTBANK_MAX_REAPERTURAS"))
	if raw == "" {
		return 2
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 2
	}
	return value
}

// ReviewTicket aprueba o rechaza un ticket en revisión.
func (s *Service) ReviewTicket(ctx context.Context, actor Identity, ref string, input ReviewInput) (AdvanceResult, error) {
	tc, err := resolveTicket(ctx, s.db, ref)
	if err != nil {
		return AdvanceResult{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return AdvanceResult{}, err
	}
	if !rights.CanWrite(tc.Project.ID) {
		return AdvanceResult{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", tc.Project.ID, "ticket.revisar", tc.Ticket.ID)
	}
	veredicto := strings.ToLower(strings.TrimSpace(input.Veredicto))
	if veredicto != "aprobado" && veredicto != "rechazado" {
		return AdvanceResult{}, validation("veredicto debe ser aprobado o rechazado")
	}
	comment := strings.TrimSpace(input.Comment)
	if comment == "" {
		return AdvanceResult{}, validation("el veredicto necesita un comentario")
	}
	if err := validateNoSecrets(comment); err != nil {
		return AdvanceResult{}, err
	}

	var result AdvanceResult
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		current, err := ticketByID(ctx, tx, tc.Ticket.ID)
		if err != nil {
			return err
		}
		if input.Version != nil && *input.Version != current.Version {
			return conflict("version_conflict", "versión desactualizada")
		}
		if current.Status != domain.TicketInReview {
			return conflict("transicion_invalida",
				fmt.Sprintf("el ticket no está en revisión (estado actual: %s)", current.Status))
		}
		now := s.now().UTC()
		previous := ticketAuditPayload(current)
		body := "Aprobado: " + comment
		action := "ticket.revision.aprobar"
		newStatus := domain.TicketDone
		if veredicto == "rechazado" {
			body = "Rechazado: " + comment
			action = "ticket.revision.rechazar"
			newStatus = domain.TicketOpen
			current.Reaperturas++
			if limit := maxReaperturas(); limit > 0 && current.Reaperturas >= int64(limit) {
				newStatus = domain.TicketBlocked
			}
		}
		commentID, err := insertTicketCommentTx(ctx, tx, actor, current.ID, domain.CommentReview, body, now, tc.Project.ID)
		if err != nil {
			return err
		}
		if err := releaseClaimsForTicket(ctx, tx, current.ID, now, "revisión emitida", actor, tc.Project.ID); err != nil {
			return err
		}
		current.Status = newStatus
		current.Version++
		current.UpdatedAt = now
		current.UpdatedByRole = actor.Participant.AgentRole
		if _, err := tx.ExecContext(ctx, `
			UPDATE tickets SET status = ?, version = ?, reaperturas = ?, updated_by_role = ?, updated_at = ?
			WHERE id = ?`,
			current.Status, current.Version, current.Reaperturas,
			actor.Participant.AgentRole, timefmt.Format(now), current.ID,
		); err != nil {
			return fmt.Errorf("emitir veredicto: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: tc.Project.ID, Actor: auditActor(actor),
			Action: action, ObjectType: "ticket", ObjectID: current.ID, ProjectID: tc.Project.ID,
			Payload: map[string]any{
				"ticket_id": current.ID, "previous": previous,
				"new": ticketAuditPayload(current), "veredicto": veredicto,
				"comment_id": commentID, "reaperturas": current.Reaperturas,
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		if current.Status == domain.TicketBlocked {
			if err := s.escalateReopeningsTx(ctx, tx, actor, tc.Project, current, comment, now); err != nil {
				return err
			}
		}
		view, err := s.ticketView(ctx, tx, current, tc.Project, true)
		if err != nil {
			return err
		}
		result.Ticket = view
		return nil
	})
	if err != nil {
		return AdvanceResult{}, err
	}
	return result, nil
}

// escalateReopeningsTx crea una solicitud al rol dueño cuando se supera el tope.
func (s *Service) escalateReopeningsTx(ctx context.Context, tx *sql.Tx, actor Identity, project domain.Project, ticket domain.Ticket, lastComment string, now time.Time) error {
	destinationType := "rol"
	destinationRole := "dueño"
	var exists int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM project_roles WHERE project_id = ? AND name = ?`,
		project.ID, destinationRole,
	).Scan(&exists); err != nil {
		return fmt.Errorf("consultar rol dueño: %w", err)
	}
	if exists == 0 {
		destinationType = "proyecto"
		destinationRole = ""
	}
	question := AdvanceQuestion{
		Subject:         fmt.Sprintf("Ticket %s bloqueado tras %d reaperturas", ticket.Ref(project.Prefix), ticket.Reaperturas),
		Body:            fmt.Sprintf("El ticket %s se bloqueó tras %d reaperturas.\nÚltimo rechazo: %s", ticket.Ref(project.Prefix), ticket.Reaperturas, lastComment),
		Mode:            "sigo",
		DestinationType: destinationType,
		DestinationRole: destinationRole,
	}
	_, err := s.createAdvanceMessageTx(ctx, tx, actor, project, ticket, question, now)
	return err
}
