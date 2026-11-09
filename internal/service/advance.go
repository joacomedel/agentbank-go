package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// AdvanceQuestion es la pregunta opcional de ticket_avanzar.
type AdvanceQuestion struct {
	Subject                  string
	Body                     string
	Options                  []string
	DefaultOption            *int
	Mode                     string
	Motivo                   string
	DestinationType          string
	DestinationRole          string
	DestinationParticipantID string
}

// AdvanceInput es el body de ticket_avanzar.
type AdvanceInput struct {
	Version     *int64
	Status      *string
	Comment     string
	ClosingNote string
	Question    *AdvanceQuestion
	YSiguiente  bool
}

// AdvanceResult devuelve el ticket, el mensaje y el siguiente ticket si se pidió.
type AdvanceResult struct {
	Ticket  TicketView        `json:"ticket"`
	Message *MessageView      `json:"message,omitempty"`
	Next    *TicketNextResult `json:"next,omitempty"`
}

// AdvanceTicket cierra o avanza un ticket con nota y pregunta opcional.
func (s *Service) AdvanceTicket(ctx context.Context, actor Identity, ref string, input AdvanceInput) (AdvanceResult, error) {
	tc, err := resolveTicket(ctx, s.db, ref)
	if err != nil {
		return AdvanceResult{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return AdvanceResult{}, err
	}
	if !rights.CanWrite(tc.Project.ID) {
		return AdvanceResult{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", tc.Project.ID, "ticket.avanzar", tc.Ticket.ID)
	}
	if input.Status == nil && input.Comment == "" && input.ClosingNote == "" && input.Question == nil {
		return AdvanceResult{}, validation("ticket_avanzar necesita status, comment, nota_cierre o pregunta")
	}
	if input.Status != nil {
		if err := validateTicketStatus(strings.TrimSpace(*input.Status)); err != nil {
			return AdvanceResult{}, err
		}
	}
	if input.Question != nil {
		if err := validateAdvanceQuestion(*input.Question); err != nil {
			return AdvanceResult{}, err
		}
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
		previous := ticketAuditPayload(current)
		now := s.now().UTC()
		commentIDs := make([]string, 0, 2)
		if input.ClosingNote != "" {
			commentID, err := insertTicketCommentTx(ctx, tx, actor, current.ID, domain.CommentClose, input.ClosingNote, now, tc.Project.ID)
			if err != nil {
				return err
			}
			commentIDs = append(commentIDs, commentID)
		}
		if input.Comment != "" {
			commentID, err := insertTicketCommentTx(ctx, tx, actor, current.ID, domain.CommentNota, input.Comment, now, tc.Project.ID)
			if err != nil {
				return err
			}
			commentIDs = append(commentIDs, commentID)
		}
		if input.Status != nil {
			current.Status = strings.TrimSpace(*input.Status)
		}
		if input.Question != nil && questionMode(input.Question) == "espero" && input.Status == nil {
			current.Status = domain.TicketOpen
		}
		if current.Status != domain.TicketInProgress {
			if err := releaseClaimsForTicket(ctx, tx, current.ID, now, "ticket avanzado", actor, tc.Project.ID); err != nil {
				return err
			}
		}
		current.Version++
		current.UpdatedAt = now
		if _, err := tx.ExecContext(ctx,
			`UPDATE tickets SET status = ?, version = ?, updated_by_role = ?, updated_at = ? WHERE id = ?`,
			current.Status, current.Version, actor.Participant.AgentRole,
			timefmt.Format(now), current.ID,
		); err != nil {
			return fmt.Errorf("avanzar ticket: %w", err)
		}
		if input.Question != nil {
			message, err := s.createAdvanceMessageTx(ctx, tx, actor, tc.Project, current, *input.Question, now)
			if err != nil {
				return err
			}
			result.Message = &message
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: tc.Project.ID, Actor: auditActor(actor),
			Action: "ticket.avanzar", ObjectType: "ticket", ObjectID: current.ID, ProjectID: tc.Project.ID,
			Payload: map[string]any{
				"ticket_id": current.ID, "previous": previous,
				"new": ticketAuditPayload(current), "comments": commentIDs,
				"question_mode": questionMode(input.Question),
			},
			CreatedAt: now,
		}); err != nil {
			return err
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
	if input.YSiguiente {
		next, err := s.TicketNext(ctx, actor, TicketNextInput{ProjectID: tc.Project.ID, Claim: true})
		if err != nil {
			return AdvanceResult{}, err
		}
		result.Next = &next
	}
	return result, nil
}

func insertTicketCommentTx(ctx context.Context, tx *sql.Tx, actor Identity, ticketID, commentType, body string, now time.Time, projectID string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", validation("el comentario no puede estar vacío")
	}
	if err := validateNoSecrets(body); err != nil {
		return "", err
	}
	commentID := id.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO comments(id, ticket_id, author_participant_id, author_role, type, body, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		commentID, ticketID, actor.Participant.ID, actor.Participant.AgentRole,
		commentType, body, timefmt.Format(now),
	); err != nil {
		return "", fmt.Errorf("crear comentario: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Input{
		ChainID: projectID, Actor: auditActor(actor), Action: "ticket.comentar",
		ObjectType: "ticket", ObjectID: ticketID, ProjectID: projectID,
		Payload:   map[string]any{"ticket_id": ticketID, "comment_id": commentID, "type": commentType, "body": body},
		CreatedAt: now,
	}); err != nil {
		return "", err
	}
	return commentID, nil
}

func (s *Service) createAdvanceMessageTx(ctx context.Context, tx *sql.Tx, actor Identity, project domain.Project, ticket domain.Ticket, question AdvanceQuestion, now time.Time) (MessageView, error) {
	subject := strings.TrimSpace(question.Subject)
	body := strings.TrimSpace(question.Body)
	if subject == "" || body == "" {
		return MessageView{}, validation("la pregunta necesita asunto y cuerpo")
	}
	if err := validateNoSecrets(subject, body); err != nil {
		return MessageView{}, err
	}
	destinationType := strings.TrimSpace(question.DestinationType)
	if destinationType == "" {
		destinationType = "rol"
	}
	destinationRole := strings.TrimSpace(question.DestinationRole)
	if destinationType == "rol" && destinationRole == "" {
		destinationRole = "dueño"
	}
	var exists int
	if destinationType == "rol" {
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM project_roles WHERE project_id = ? AND name = ?`,
			project.ID, destinationRole,
		).Scan(&exists); err != nil {
			return MessageView{}, fmt.Errorf("consultar rol destino: %w", err)
		}
		if exists == 0 {
			return MessageView{}, validation("el rol destino no existe en el catálogo")
		}
	}
	options, err := normalizeMessageOptions(question.Options)
	if err != nil {
		return MessageView{}, err
	}
	defaultOption := question.DefaultOption
	if defaultOption != nil && (len(options) == 0 || *defaultOption < 0 || *defaultOption >= len(options)) {
		return MessageView{}, validation("default_option inválido")
	}
	messageID := id.New()
	optionsJSON, _ := json.Marshal(options)
	message := domain.Message{
		ID: messageID, ProjectID: project.ID, FromParticipantID: actor.Participant.ID,
		FromRole: actor.Participant.AgentRole,
		Type:     "solicitud", DestinationType: destinationType,
		DestinationRole: destinationRole, Subject: subject, Body: body,
		Options: options, DefaultOption: defaultOption, CreatedAt: now,
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages
			(id, project_id, from_participant_id, from_role, type, destination_type,
			 destination_participant_id, destination_role, subject, body, options,
			 default_option, created_at)
		VALUES (?, ?, ?, ?, 'solicitud', ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID, project.ID, actor.Participant.ID, actor.Participant.AgentRole,
		destinationType,
		nullable(question.DestinationParticipantID), nullable(destinationRole),
		message.Subject, message.Body, string(optionsJSON), nullableInt(defaultOption),
		timefmt.Format(now),
	); err != nil {
		return MessageView{}, fmt.Errorf("crear pregunta: %w", err)
	}
	if strings.TrimSpace(question.Mode) == "espero" {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO message_affects(message_id, ticket_id) VALUES (?, ?)`,
			message.ID, ticket.ID,
		); err != nil {
			return MessageView{}, fmt.Errorf("afectar ticket: %w", err)
		}
	}
	if _, err := audit.Append(ctx, tx, audit.Input{
		ChainID: project.ID, Actor: auditActor(actor), Action: "mensaje.enviar",
		ObjectType: "mensaje", ObjectID: message.ID, ProjectID: project.ID,
		Payload:   map[string]any{"message_id": message.ID, "subject": message.Subject, "mode": question.Mode},
		CreatedAt: now,
	}); err != nil {
		return MessageView{}, err
	}
	return messageView(message, actor.Participant.Name, "nuevo"), nil
}

func validateAdvanceQuestion(question AdvanceQuestion) error {
	mode := strings.TrimSpace(question.Mode)
	if mode == "" {
		mode = "sigo"
	}
	if mode != "sigo" && mode != "divido" && mode != "espero" {
		return validation("mode debe ser sigo, divido o espero")
	}
	if mode == "espero" && strings.TrimSpace(question.Motivo) == "" {
		return validation("mode espero requiere motivo: irreversible, sin_default o consultar_siempre")
	}
	if strings.TrimSpace(question.Subject) == "" || strings.TrimSpace(question.Body) == "" {
		return validation("la pregunta necesita asunto y cuerpo")
	}
	if _, err := normalizeMessageOptions(question.Options); err != nil {
		return err
	}
	return nil
}

func questionMode(question *AdvanceQuestion) string {
	if question == nil {
		return ""
	}
	if strings.TrimSpace(question.Mode) == "" {
		return "sigo"
	}
	return strings.TrimSpace(question.Mode)
}
