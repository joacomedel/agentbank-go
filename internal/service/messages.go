package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// MessageRef referencia un objeto desde un mensaje.
type MessageRef struct {
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
}

// MessageInput es el alta de un mensaje.
type MessageInput struct {
	ProjectID                string
	Type                     string
	DestinationType          string
	DestinationParticipantID string
	DestinationRole          string
	Subject                  string
	Body                     string
	Options                  []string
	DefaultOption            *int
	AssumeDefaultAt          string
	Affects                  []string
	Refs                     []MessageRef
	ReplyTo                  string
}

// MessageView es un mensaje resuelto para la API.
type MessageView struct {
	ID                       string                `json:"id"`
	ProjectID                string                `json:"project_id,omitempty"`
	FromParticipantID        string                `json:"from_participant_id"`
	FromName                 string                `json:"from_name"`
	Type                     string                `json:"type"`
	DestinationType          string                `json:"destination_type"`
	DestinationParticipantID string                `json:"destination_participant_id,omitempty"`
	DestinationRole          string                `json:"destination_role,omitempty"`
	Subject                  string                `json:"subject"`
	Body                     string                `json:"body"`
	BodyContent              domain.ForeignContent `json:"body_content"`
	Options                  []string              `json:"options"`
	DefaultOption            *int                  `json:"default_option,omitempty"`
	AssumeDefaultAt          *time.Time            `json:"assume_default_at,omitempty"`
	Resolution               string                `json:"resolution,omitempty"`
	ResolvedBy               string                `json:"resolved_by,omitempty"`
	ResolvedAt               *time.Time            `json:"resolved_at,omitempty"`
	ResponseOption           *int                  `json:"response_option,omitempty"`
	ResponseText             string                `json:"response_text,omitempty"`
	TakenBy                  string                `json:"taken_by,omitempty"`
	TakenAt                  *time.Time            `json:"taken_at,omitempty"`
	ReplyTo                  string                `json:"reply_to,omitempty"`
	CorrelationID            string                `json:"correlation_id,omitempty"`
	State                    string                `json:"state"`
	Pending                  bool                  `json:"pending"`
	Affects                  []string              `json:"affects,omitempty"`
	Refs                     []MessageRef          `json:"refs,omitempty"`
	CreatedAt                time.Time             `json:"created_at"`
}

// CreateMessage crea un mensaje info o solicitud.
func (s *Service) CreateMessage(ctx context.Context, actor Identity, input MessageInput) (MessageView, error) {
	messageType := strings.TrimSpace(input.Type)
	if messageType == "" {
		messageType = "info"
	}
	if messageType != "info" && messageType != "solicitud" {
		return MessageView{}, validation("type debe ser info o solicitud")
	}
	subject := strings.TrimSpace(input.Subject)
	if subject == "" || len([]rune(subject)) > 200 {
		return MessageView{}, validation("subject debe tener entre 1 y 200 caracteres")
	}
	body := strings.TrimSpace(input.Body)
	if body == "" || len(body) > 20000 {
		return MessageView{}, validation("body debe tener entre 1 y 20000 caracteres")
	}
	if err := validateNoSecrets(subject, body); err != nil {
		return MessageView{}, err
	}
	projectID := ""
	var project domain.Project
	if strings.TrimSpace(input.ProjectID) != "" {
		var err error
		project, err = resolveProject(ctx, s.db, input.ProjectID)
		if err != nil {
			return MessageView{}, err
		}
		rights, err := s.loadRights(ctx, s.db, actor.Participant)
		if err != nil {
			return MessageView{}, err
		}
		if !rights.CanRead(project.ID) {
			return MessageView{}, forbidden("requires_project_read", "sin permiso para enviar mensajes en el proyecto", project.ID, "mensaje.enviar", "")
		}
		projectID = project.ID
	}
	destinationType := strings.TrimSpace(input.DestinationType)
	if destinationType == "" {
		destinationType = "proyecto"
	}
	if destinationType != "participante" && destinationType != "rol" && destinationType != "proyecto" {
		return MessageView{}, validation("destination_type debe ser participante, rol o proyecto")
	}
	destinationParticipant := strings.TrimSpace(input.DestinationParticipantID)
	destinationRole := strings.TrimSpace(input.DestinationRole)
	switch destinationType {
	case "participante":
		if destinationParticipant == "" {
			return MessageView{}, validation("destination_participant_id es obligatorio")
		}
		if _, err := getParticipant(ctx, s.db, destinationParticipant); err != nil {
			return MessageView{}, err
		}
	case "rol":
		if projectID == "" {
			return MessageView{}, validation("un mensaje a rol requiere project_id")
		}
		if destinationRole == "" {
			return MessageView{}, validation("destination_role es obligatorio")
		}
		var exists int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM project_roles WHERE project_id = ? AND name = ?`,
			projectID, destinationRole,
		).Scan(&exists); err != nil {
			return MessageView{}, fmt.Errorf("consultar rol: %w", err)
		}
		if exists == 0 {
			return MessageView{}, validation("el rol no existe en el catálogo del proyecto")
		}
	case "proyecto":
		if projectID == "" {
			return MessageView{}, validation("un mensaje a proyecto requiere project_id")
		}
	}
	options, err := normalizeMessageOptions(input.Options)
	if err != nil {
		return MessageView{}, err
	}
	if messageType == "info" && len(options) > 0 {
		return MessageView{}, validation("un mensaje info no acepta opciones")
	}
	defaultOption := input.DefaultOption
	if defaultOption != nil {
		if len(options) == 0 {
			return MessageView{}, validation("default_option requiere options")
		}
		if *defaultOption < 0 || *defaultOption >= len(options) {
			return MessageView{}, validation("default_option fuera de rango")
		}
	}
	var assumeDefaultAt *time.Time
	if strings.TrimSpace(input.AssumeDefaultAt) != "" {
		if messageType != "solicitud" {
			return MessageView{}, validation("assume_default_at solo aplica a solicitud")
		}
		if defaultOption == nil {
			return MessageView{}, validation("assume_default_at requiere default_option")
		}
		parsed, err := timefmt.ParseFlexible(input.AssumeDefaultAt)
		if err != nil {
			return MessageView{}, validation(err.Error())
		}
		assumeDefaultAt = &parsed
	}
	if messageType == "info" && len(input.Affects) > 0 {
		return MessageView{}, validation("un mensaje info no puede afectar tickets")
	}

	var view MessageView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		message := domain.Message{
			ID: id.New(), ProjectID: projectID, FromParticipantID: actor.Participant.ID,
			Type: messageType, DestinationType: destinationType,
			DestinationParticipantID: destinationParticipant, DestinationRole: destinationRole,
			Subject: subject, Body: body, Options: options,
			DefaultOption: defaultOption, AssumeDefaultAt: assumeDefaultAt,
			CreatedAt: now,
		}
		replyTo := ""
		correlationID := message.ID
		if strings.TrimSpace(input.ReplyTo) != "" {
			parent, err := resolveMessage(ctx, tx, input.ReplyTo)
			if err != nil {
				return err
			}
			rights, err := s.loadRights(ctx, tx, actor.Participant)
			if err != nil {
				return err
			}
			if !canSeeMessage(ctx, tx, actor, rights, parent) {
				return forbidden("forbidden", "sin permiso para responder ese mensaje", parent.ProjectID, "mensaje.enviar", parent.ID)
			}
			replyTo = parent.ID
			if parent.CorrelationID != "" {
				correlationID = parent.CorrelationID
			} else {
				correlationID = parent.ID
			}
		}
		message.ReplyTo = replyTo
		message.CorrelationID = correlationID
		optionsJSON, _ := json.Marshal(options)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO messages
				(id, project_id, from_participant_id, type, destination_type,
				 destination_participant_id, destination_role, subject, body, options,
				 default_option, assume_default_at, reply_to, correlation_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			message.ID, nullable(message.ProjectID), message.FromParticipantID, message.Type,
			message.DestinationType, nullable(message.DestinationParticipantID),
			nullable(message.DestinationRole), message.Subject, message.Body, string(optionsJSON),
			nullableInt(message.DefaultOption), nullableTime(message.AssumeDefaultAt),
			nullable(replyTo), correlationID, timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("crear mensaje: %w", err)
		}
		affects := make([]string, 0, len(input.Affects))
		for _, ref := range input.Affects {
			ticket, err := resolveTicket(ctx, tx, ref)
			if err != nil {
				return err
			}
			rights, err := s.loadRights(ctx, tx, actor.Participant)
			if err != nil {
				return err
			}
			if !rights.CanRead(ticket.Project.ID) {
				return forbidden("requires_project_read", "sin permiso para afectar el ticket", ticket.Project.ID, "mensaje.enviar", ticket.Ticket.ID)
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO message_affects(message_id, ticket_id) VALUES (?, ?)`,
				message.ID, ticket.Ticket.ID,
			); err != nil {
				return fmt.Errorf("afectar ticket: %w", err)
			}
			affects = append(affects, ticket.Ticket.Ref(ticket.Project.Prefix))
		}
		for _, ref := range input.Refs {
			if err := validateMessageRef(ctx, tx, actor, ref); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO message_refs(message_id, object_type, object_id)
				VALUES (?, ?, ?)`, message.ID, ref.ObjectType, ref.ObjectID,
			); err != nil {
				return fmt.Errorf("guardar referencia: %w", err)
			}
		}
		chainID := projectID
		if chainID == "" {
			chainID = domain.SystemChain
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: chainID, Actor: auditActor(actor),
			Action: "mensaje.enviar", ObjectType: "mensaje", ObjectID: message.ID, ProjectID: projectID,
			Payload: map[string]any{
				"message_id": message.ID, "type": message.Type,
				"destination_type": message.DestinationType, "subject": message.Subject,
				"affects": affects,
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		if projectID != "" {
			if err := s.enqueueWebhooks(ctx, tx, projectID, "mensaje.enviar", map[string]any{
				"message_id": message.ID, "type": message.Type, "subject": message.Subject,
			}); err != nil {
				return err
			}
		}
		view = messageView(message, actor.Participant.Name, "nuevo")
		view.Affects = affects
		view.Refs = input.Refs
		return nil
	})
	if err != nil {
		return MessageView{}, err
	}
	return view, nil
}

// ListInbox devuelve la bandeja del participante.
func (s *Service) ListInbox(ctx context.Context, actor Identity, projectRef string, pendingOnly bool, limit int, cursor string) ([]MessageView, string, error) {
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, "", err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return nil, "", err
	}
	conditions := []string{"m.from_participant_id <> ?"}
	args := []any{actor.Participant.ID}
	recipient := `(m.destination_participant_id = ?`
	args = append(args, actor.Participant.ID)
	if len(rights.Memberships) > 0 {
		recipient += ` OR (m.destination_type = 'rol' AND EXISTS (
			SELECT 1 FROM memberships mm, json_each(mm.roles) j
			WHERE mm.participant_id = ? AND mm.project_id = m.project_id AND j.value = m.destination_role))`
		args = append(args, actor.Participant.ID)
		placeholders := make([]string, 0, len(rights.Memberships))
		for id := range rights.Memberships {
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}
		recipient += ` OR (m.destination_type = 'proyecto' AND m.project_id IN (` + strings.Join(placeholders, ",") + `))`
	}
	if rights.Admin {
		recipient += ` OR m.destination_type = 'proyecto'`
	}
	recipient += `)`
	conditions = append(conditions, recipient)
	if strings.TrimSpace(projectRef) != "" {
		project, err := resolveProject(ctx, s.db, projectRef)
		if err != nil {
			return nil, "", err
		}
		if !rights.CanRead(project.ID) {
			return nil, "", forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "bandeja.leer", project.ID)
		}
		conditions = append(conditions, "m.project_id = ?")
		args = append(args, project.ID)
	}
	if pendingOnly {
		conditions = append(conditions, "m.resolution IS NULL")
	}
	if cursor != "" {
		createdAt, id, err := decodeAuditCursor(cursor)
		if err != nil {
			return nil, "", validation("cursor inválido")
		}
		conditions = append(conditions, "(m.created_at < ? OR (m.created_at = ? AND m.id < ?))")
		args = append(args, createdAt, createdAt, id)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, COALESCE(m.project_id, ''), m.from_participant_id, m.type,
		       m.destination_type, COALESCE(m.destination_participant_id, ''),
		       COALESCE(m.destination_role, ''), m.subject, m.body, m.options,
		       m.default_option, m.assume_default_at, COALESCE(m.resolution, ''),
		       COALESCE(m.resolved_by, ''), m.resolved_at, m.response_option,
		       COALESCE(m.response_text, ''), COALESCE(m.taken_by, ''), m.taken_at,
		       COALESCE(m.reply_to, ''), COALESCE(m.correlation_id, ''), m.created_at
		FROM messages m
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar bandeja: %w", err)
	}
	defer rows.Close()
	messages := make([]domain.Message, 0, limit)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, "", err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer bandeja: %w", err)
	}
	views := make([]MessageView, 0, len(messages))
	for _, message := range messages {
		view, err := s.messageViewFor(ctx, s.db, actor, message)
		if err != nil {
			return nil, "", err
		}
		views = append(views, view)
	}
	next := ""
	if len(views) > limit {
		views = views[:limit]
		last := views[len(views)-1]
		next = encodeAuditCursor(timefmt.Format(last.CreatedAt), last.ID)
	}
	return views, next, nil
}

// GetMessage devuelve un mensaje visible para el actor.
func (s *Service) GetMessage(ctx context.Context, actor Identity, messageID string) (MessageView, error) {
	message, err := resolveMessage(ctx, s.db, messageID)
	if err != nil {
		return MessageView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return MessageView{}, err
	}
	if !canSeeMessage(ctx, s.db, actor, rights, message) {
		return MessageView{}, forbidden("forbidden", "sin permiso para leer el mensaje", message.ProjectID, "mensaje.leer", message.ID)
	}
	return s.messageViewFor(ctx, s.db, actor, message)
}

// SetMessageState marca visto/atendido para el participante.
func (s *Service) SetMessageState(ctx context.Context, actor Identity, messageID, state string) (MessageView, error) {
	if state != "visto" && state != "atendido" {
		return MessageView{}, validation("state debe ser visto o atendido")
	}
	message, err := resolveMessage(ctx, s.db, messageID)
	if err != nil {
		return MessageView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return MessageView{}, err
	}
	if !canSeeMessage(ctx, s.db, actor, rights, message) {
		return MessageView{}, forbidden("forbidden", "sin permiso para cambiar el estado del mensaje", message.ProjectID, "mensaje.estado", message.ID)
	}
	now := s.now().UTC()
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO message_state(message_id, participant_id, state, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(message_id, participant_id) DO UPDATE SET state = excluded.state, updated_at = excluded.updated_at`,
			message.ID, actor.Participant.ID, state, timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("guardar estado de mensaje: %w", err)
		}
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID: chainForMessage(message), Actor: auditActor(actor),
			Action: "mensaje.estado", ObjectType: "mensaje", ObjectID: message.ID, ProjectID: message.ProjectID,
			Payload: map[string]any{"message_id": message.ID, "state": state}, CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return MessageView{}, err
	}
	return s.GetMessage(ctx, actor, message.ID)
}

// TakeMessage toma un mensaje dirigido a un rol.
func (s *Service) TakeMessage(ctx context.Context, actor Identity, messageID string) (MessageView, error) {
	message, err := resolveMessage(ctx, s.db, messageID)
	if err != nil {
		return MessageView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return MessageView{}, err
	}
	if !canSeeMessage(ctx, s.db, actor, rights, message) {
		return MessageView{}, forbidden("forbidden", "sin permiso para tomar el mensaje", message.ProjectID, "mensaje.tomar", message.ID)
	}
	if message.DestinationType != "rol" {
		return MessageView{}, validation("solo se pueden tomar mensajes dirigidos a un rol")
	}
	if !hasProjectRole(ctx, s.db, actor.Participant.ID, message.ProjectID, message.DestinationRole) && !rights.Admin {
		return MessageView{}, forbidden("forbidden", "no tenés el rol destino", message.ProjectID, "mensaje.tomar", message.ID)
	}
	if message.TakenBy != "" && message.TakenBy != actor.Participant.ID {
		return MessageView{}, conflict("message_taken", "el mensaje ya fue tomado por otro participante")
	}
	now := s.now().UTC()
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE messages SET taken_by = ?, taken_at = ? WHERE id = ? AND (taken_by IS NULL OR taken_by = ?)`,
			actor.Participant.ID, timefmt.Format(now), message.ID, actor.Participant.ID,
		); err != nil {
			return fmt.Errorf("tomar mensaje: %w", err)
		}
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID: chainForMessage(message), Actor: auditActor(actor),
			Action: "mensaje.tomar", ObjectType: "mensaje", ObjectID: message.ID, ProjectID: message.ProjectID,
			Payload: map[string]any{"message_id": message.ID}, CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return MessageView{}, err
	}
	return s.GetMessage(ctx, actor, message.ID)
}

// ResolveMessage resuelve una solicitud con una opción o texto.
func (s *Service) ResolveMessage(ctx context.Context, actor Identity, messageID string, option *int, text string) (MessageView, error) {
	message, err := resolveMessage(ctx, s.db, messageID)
	if err != nil {
		return MessageView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return MessageView{}, err
	}
	if !canRespondMessage(ctx, s.db, actor, rights, message) {
		return MessageView{}, forbidden("forbidden", "no podés responder ese mensaje", message.ProjectID, "mensaje.resolver", message.ID)
	}
	resultText := strings.TrimSpace(text)
	if option != nil {
		if len(message.Options) == 0 {
			return MessageView{}, validation("el mensaje no tiene opciones")
		}
		if *option < 0 || *option >= len(message.Options) {
			return MessageView{}, validation("option fuera de rango")
		}
	}
	if option == nil && resultText == "" {
		return MessageView{}, validation("enviá option o text")
	}
	if message.Resolution == "respondida" || message.Resolution == "descartada" {
		return MessageView{}, conflict("message_already_resolved", "el mensaje ya está resuelto")
	}
	correction := message.Resolution == "default_asumido" && option != nil && message.ResponseOption != nil && *option != *message.ResponseOption
	now := s.now().UTC()
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			UPDATE messages SET resolution = 'respondida', resolved_by = ?, resolved_at = ?,
			                   response_option = ?, response_text = ?, taken_by = COALESCE(taken_by, ?)
			WHERE id = ?`,
			actor.Participant.ID, timefmt.Format(now), nullableInt(option), nullable(resultText),
			actor.Participant.ID, message.ID,
		); err != nil {
			return fmt.Errorf("resolver mensaje: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: chainForMessage(message), Actor: auditActor(actor),
			Action: "mensaje.resolver", ObjectType: "mensaje", ObjectID: message.ID, ProjectID: message.ProjectID,
			Payload: map[string]any{"message_id": message.ID, "option": option, "text": resultText}, CreatedAt: now,
		}); err != nil {
			return err
		}
		if correction {
			return s.createCorrectionTicketTx(ctx, tx, actor, message)
		}
		return nil
	})
	if err != nil {
		return MessageView{}, err
	}
	return s.GetMessage(ctx, actor, message.ID)
}

// DiscardMessage descarta una solicitud sin resolverla. Solo el emisor.
func (s *Service) DiscardMessage(ctx context.Context, actor Identity, messageID string) (MessageView, error) {
	message, err := resolveMessage(ctx, s.db, messageID)
	if err != nil {
		return MessageView{}, err
	}
	if message.FromParticipantID != actor.Participant.ID {
		return MessageView{}, forbidden("forbidden", "solo el emisor puede descartar el mensaje", message.ProjectID, "mensaje.descartar", message.ID)
	}
	if message.Resolution != "" {
		return MessageView{}, conflict("message_already_resolved", "el mensaje ya está resuelto")
	}
	now := s.now().UTC()
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE messages SET resolution = 'descartada', resolved_at = ? WHERE id = ?`,
			timefmt.Format(now), message.ID,
		); err != nil {
			return fmt.Errorf("descartar mensaje: %w", err)
		}
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID: chainForMessage(message), Actor: auditActor(actor),
			Action: "mensaje.descartar", ObjectType: "mensaje", ObjectID: message.ID, ProjectID: message.ProjectID,
			Payload: map[string]any{"message_id": message.ID}, CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return MessageView{}, err
	}
	return s.GetMessage(ctx, actor, message.ID)
}

// SweepMessageDefaults asume defaults vencidos. Devuelve cuántos asumió.
func (s *Service) SweepMessageDefaults(ctx context.Context) (int, error) {
	now := s.now().UTC()
	assumed := 0
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `
			SELECT id, COALESCE(project_id, ''), default_option
			FROM messages
			WHERE resolution IS NULL AND type = 'solicitud'
			  AND default_option IS NOT NULL AND assume_default_at IS NOT NULL
			  AND assume_default_at <= ?
			ORDER BY assume_default_at`, timefmt.Format(now))
		if err != nil {
			return fmt.Errorf("buscar defaults vencidos: %w", err)
		}
		type pending struct {
			id, projectID string
			option        int
		}
		items := make([]pending, 0, 8)
		for rows.Next() {
			var item pending
			if err := rows.Scan(&item.id, &item.projectID, &item.option); err != nil {
				rows.Close()
				return fmt.Errorf("escanear default: %w", err)
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("recorrer defaults: %w", err)
		}
		_ = rows.Close()
		for _, item := range items {
			if _, err := tx.ExecContext(ctx, `
				UPDATE messages SET resolution = 'default_asumido', resolved_at = ?, response_option = ?
				WHERE id = ? AND resolution IS NULL`, timefmt.Format(now), item.option, item.id,
			); err != nil {
				return fmt.Errorf("asumir default: %w", err)
			}
			chainID := item.projectID
			if chainID == "" {
				chainID = domain.SystemChain
			}
			if _, err := audit.Append(ctx, tx, audit.Input{
				ChainID: chainID, Actor: audit.Actor{Type: domain.AuditActorAnonimo},
				Action: "mensaje.default_asumido", ObjectType: "mensaje", ObjectID: item.id, ProjectID: item.projectID,
				Payload: map[string]any{"message_id": item.id, "option": item.option}, CreatedAt: now,
			}); err != nil {
				return err
			}
			assumed++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return assumed, nil
}

// ConvertMessage convierte un mensaje en decisión o ticket.
func (s *Service) ConvertMessage(ctx context.Context, actor Identity, messageID, to, title, body string) (map[string]any, error) {
	message, err := resolveMessage(ctx, s.db, messageID)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !canSeeMessage(ctx, s.db, actor, rights, message) {
		return nil, forbidden("forbidden", "sin permiso para convertir el mensaje", message.ProjectID, "mensaje.convertir", message.ID)
	}
	if message.ProjectID == "" {
		return nil, validation("el mensaje no pertenece a un proyecto")
	}
	if !rights.CanWrite(message.ProjectID) {
		return nil, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", message.ProjectID, "mensaje.convertir", message.ID)
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = message.Subject
	}
	if body == "" {
		body = message.Body
	}
	now := s.now().UTC()
	switch to {
	case "decision":
		decisionID := id.New()
		err = s.withTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO decisions(id, project_id, title, status, body, source_message_id, created_by, created_at)
				VALUES (?, ?, ?, 'vigente', ?, ?, ?, ?)`,
				decisionID, message.ProjectID, title, body, message.ID, actor.Participant.ID, timefmt.Format(now),
			); err != nil {
				return fmt.Errorf("crear decisión: %w", err)
			}
			_, err := audit.Append(ctx, tx, audit.Input{
				ChainID: message.ProjectID, Actor: auditActor(actor),
				Action: "decision.crear", ObjectType: "decision", ObjectID: decisionID, ProjectID: message.ProjectID,
				Payload:   map[string]any{"decision_id": decisionID, "message_id": message.ID, "title": title},
				CreatedAt: now,
			})
			return err
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"decision_id": decisionID, "title": title}, nil
	case "ticket":
		var ticketID, ref string
		err = s.withTx(ctx, func(tx *sql.Tx) error {
			project, err := resolveProject(ctx, tx, message.ProjectID)
			if err != nil {
				return err
			}
			var number int64
			if err := tx.QueryRowContext(ctx,
				`SELECT COALESCE(MAX(number), 0) + 1 FROM tickets WHERE project_id = ?`,
				message.ProjectID,
			).Scan(&number); err != nil {
				return fmt.Errorf("asignar número: %w", err)
			}
			ticketID = id.New()
			ref = fmt.Sprintf("%s-%d", project.Prefix, number)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO tickets
					(id, project_id, number, title, description, status, priority, created_by, created_by_role, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, 'abierto', 'media', ?, ?, ?, ?)`,
				ticketID, message.ProjectID, number, title, body, actor.Participant.ID,
				actor.Participant.AgentRole, timefmt.Format(now), timefmt.Format(now),
			); err != nil {
				return fmt.Errorf("crear ticket desde mensaje: %w", err)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT OR IGNORE INTO message_refs(message_id, object_type, object_id)
				VALUES (?, 'ticket', ?)`, message.ID, ticketID,
			); err != nil {
				return fmt.Errorf("referenciar ticket: %w", err)
			}
			if _, err := audit.Append(ctx, tx, audit.Input{
				ChainID: message.ProjectID, Actor: auditActor(actor),
				Action: "ticket.crear", ObjectType: "ticket", ObjectID: ticketID, ProjectID: message.ProjectID,
				Payload:   map[string]any{"ticket_id": ticketID, "ref": ref, "title": title, "from_message": message.ID},
				CreatedAt: now,
			}); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ticket_id": ticketID, "ref": ref, "title": title}, nil
	default:
		return nil, validation("to debe ser decision o ticket")
	}
}

func (s *Service) messageViewFor(ctx context.Context, q queryer, actor Identity, message domain.Message) (MessageView, error) {
	view := messageView(message, "", messageStateFor(ctx, q, message.ID, actor.Participant.ID))
	var fromName string
	if err := q.QueryRowContext(ctx, `SELECT name FROM participants WHERE id = ?`, message.FromParticipantID).Scan(&fromName); err == nil {
		view.FromName = fromName
	}
	affects, err := messageAffects(ctx, q, message.ID)
	if err != nil {
		return MessageView{}, err
	}
	refs, err := messageRefs(ctx, q, message.ID)
	if err != nil {
		return MessageView{}, err
	}
	view.Affects = affects
	view.Refs = refs
	return view, nil
}

func messageView(message domain.Message, fromName, state string) MessageView {
	options := message.Options
	if options == nil {
		options = []string{}
	}
	return MessageView{
		ID: message.ID, ProjectID: message.ProjectID, FromParticipantID: message.FromParticipantID,
		FromName: fromName, Type: message.Type, DestinationType: message.DestinationType,
		DestinationParticipantID: message.DestinationParticipantID, DestinationRole: message.DestinationRole,
		Subject: message.Subject, Body: message.Body,
		BodyContent: domain.WrapForeign(message.Body, "message"),
		Options:     options, DefaultOption: message.DefaultOption, AssumeDefaultAt: message.AssumeDefaultAt,
		Resolution: message.Resolution, ResolvedBy: message.ResolvedBy, ResolvedAt: message.ResolvedAt,
		ResponseOption: message.ResponseOption, ResponseText: message.ResponseText,
		TakenBy: message.TakenBy, TakenAt: message.TakenAt, ReplyTo: message.ReplyTo,
		CorrelationID: message.CorrelationID, State: state, Pending: message.Resolution == "",
		CreatedAt: message.CreatedAt,
	}
}

func messageStateFor(ctx context.Context, q queryer, messageID, participantID string) string {
	var state string
	if err := q.QueryRowContext(ctx,
		`SELECT state FROM message_state WHERE message_id = ? AND participant_id = ?`,
		messageID, participantID,
	).Scan(&state); err != nil {
		return "nuevo"
	}
	return state
}

func messageAffects(ctx context.Context, q queryer, messageID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT p.prefix || '-' || t.number
		FROM message_affects ma
		JOIN tickets t ON t.id = ma.ticket_id
		JOIN projects p ON p.id = t.project_id
		WHERE ma.message_id = ?
		ORDER BY p.prefix, t.number`, messageID)
	if err != nil {
		return nil, fmt.Errorf("listar afectados: %w", err)
	}
	defer rows.Close()
	refs := make([]string, 0, 4)
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, fmt.Errorf("escanear afectado: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer afectados: %w", err)
	}
	return refs, nil
}

func messageRefs(ctx context.Context, q queryer, messageID string) ([]MessageRef, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT object_type, object_id FROM message_refs WHERE message_id = ?
		ORDER BY object_type, object_id`, messageID)
	if err != nil {
		return nil, fmt.Errorf("listar referencias: %w", err)
	}
	defer rows.Close()
	refs := make([]MessageRef, 0, 4)
	for rows.Next() {
		var ref MessageRef
		if err := rows.Scan(&ref.ObjectType, &ref.ObjectID); err != nil {
			return nil, fmt.Errorf("escanear referencia: %w", err)
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer referencias: %w", err)
	}
	return refs, nil
}

func resolveMessage(ctx context.Context, q queryer, ref string) (domain.Message, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return domain.Message{}, notFound("message_not_found", "mensaje no encontrado")
	}
	message, err := scanMessage(q.QueryRowContext(ctx, `
		SELECT id, COALESCE(project_id, ''), from_participant_id, type, destination_type,
		       COALESCE(destination_participant_id, ''), COALESCE(destination_role, ''),
		       subject, body, options, default_option, assume_default_at,
		       COALESCE(resolution, ''), COALESCE(resolved_by, ''), resolved_at,
		       response_option, COALESCE(response_text, ''), COALESCE(taken_by, ''),
		       taken_at, COALESCE(reply_to, ''), COALESCE(correlation_id, ''), created_at
		FROM messages WHERE id = ?`, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Message{}, notFound("message_not_found", "mensaje no encontrado")
	}
	if err != nil {
		return domain.Message{}, err
	}
	return message, nil
}

func scanMessage(row interface{ Scan(...any) error }) (domain.Message, error) {
	var message domain.Message
	var options, createdAt string
	var defaultOption, responseOption sql.NullInt64
	var assumeDefaultAt, resolvedAt, takenAt sql.NullString
	if err := row.Scan(
		&message.ID, &message.ProjectID, &message.FromParticipantID, &message.Type,
		&message.DestinationType, &message.DestinationParticipantID, &message.DestinationRole,
		&message.Subject, &message.Body, &options, &defaultOption, &assumeDefaultAt,
		&message.Resolution, &message.ResolvedBy, &resolvedAt, &responseOption,
		&message.ResponseText, &message.TakenBy, &takenAt, &message.ReplyTo,
		&message.CorrelationID, &createdAt,
	); err != nil {
		return domain.Message{}, err
	}
	if err := json.Unmarshal([]byte(options), &message.Options); err != nil {
		return domain.Message{}, fmt.Errorf("opciones inválidas: %w", err)
	}
	if message.Options == nil {
		message.Options = []string{}
	}
	if defaultOption.Valid {
		value := int(defaultOption.Int64)
		message.DefaultOption = &value
	}
	if responseOption.Valid {
		value := int(responseOption.Int64)
		message.ResponseOption = &value
	}
	message.AssumeDefaultAt = parseNullableTime(assumeDefaultAt)
	message.ResolvedAt = parseNullableTime(resolvedAt)
	message.TakenAt = parseNullableTime(takenAt)
	parsed, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Message{}, fmt.Errorf("created_at de mensaje inválido: %w", err)
	}
	message.CreatedAt = parsed
	return message, nil
}

func canSeeMessage(ctx context.Context, q queryer, actor Identity, rights domain.Rights, message domain.Message) bool {
	if rights.Admin || message.FromParticipantID == actor.Participant.ID || message.DestinationParticipantID == actor.Participant.ID {
		return true
	}
	if message.ProjectID != "" && rights.CanRead(message.ProjectID) {
		return true
	}
	if message.DestinationType == "rol" && message.ProjectID != "" {
		return hasProjectRole(ctx, q, actor.Participant.ID, message.ProjectID, message.DestinationRole)
	}
	return false
}

func canRespondMessage(ctx context.Context, q queryer, actor Identity, rights domain.Rights, message domain.Message) bool {
	if message.Resolution == "respondida" || message.Resolution == "descartada" {
		return false
	}
	if rights.Admin {
		return true
	}
	switch message.DestinationType {
	case "participante":
		return message.DestinationParticipantID == actor.Participant.ID
	case "rol":
		return hasProjectRole(ctx, q, actor.Participant.ID, message.ProjectID, message.DestinationRole)
	case "proyecto":
		return message.ProjectID != "" && rights.CanRead(message.ProjectID)
	default:
		return false
	}
}

func hasProjectRole(ctx context.Context, q queryer, participantID, projectID, role string) bool {
	if role == "" || projectID == "" {
		return false
	}
	var count int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM memberships m, json_each(m.roles) j
		WHERE m.participant_id = ? AND m.project_id = ? AND j.value = ?`,
		participantID, projectID, role,
	).Scan(&count)
	return err == nil && count > 0
}

func validateMessageRef(ctx context.Context, q queryer, actor Identity, ref MessageRef) error {
	ref.ObjectType = strings.TrimSpace(ref.ObjectType)
	ref.ObjectID = strings.TrimSpace(ref.ObjectID)
	if ref.ObjectID == "" {
		return validation("object_id de referencia vacío")
	}
	switch ref.ObjectType {
	case "ticket":
		_, err := resolveTicket(ctx, q, ref.ObjectID)
		return err
	case "message":
		_, err := resolveMessage(ctx, q, ref.ObjectID)
		return err
	case "document":
		var count int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(1) FROM documents WHERE id = ?`, ref.ObjectID).Scan(&count); err != nil {
			return fmt.Errorf("consultar documento: %w", err)
		}
		if count == 0 {
			return notFound("document_not_found", "documento no encontrado")
		}
		return nil
	default:
		return validation("object_type debe ser ticket, document o message")
	}
}

func normalizeMessageOptions(options []string) ([]string, error) {
	if len(options) > 10 {
		return nil, validation("options admite como máximo 10 elementos")
	}
	normalized := make([]string, 0, len(options))
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" || len([]rune(option)) > 100 {
			return nil, validation("cada opción debe tener 1..100 caracteres")
		}
		if _, duplicated := seen[option]; duplicated {
			return nil, validation("las opciones no pueden repetirse")
		}
		seen[option] = struct{}{}
		normalized = append(normalized, option)
	}
	return normalized, nil
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func chainForMessage(message domain.Message) string {
	if message.ProjectID != "" {
		return message.ProjectID
	}
	return domain.SystemChain
}

func (s *Service) createCorrectionTicketTx(ctx context.Context, tx *sql.Tx, actor Identity, message domain.Message) error {
	if message.ProjectID == "" || message.FromParticipantID == "" {
		return nil
	}
	project, err := resolveProject(ctx, tx, message.ProjectID)
	if err != nil {
		return err
	}
	var number int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(number), 0) + 1 FROM tickets WHERE project_id = ?`,
		message.ProjectID,
	).Scan(&number); err != nil {
		return fmt.Errorf("asignar número de corrección: %w", err)
	}
	ticketID := id.New()
	title := "Corregir: " + message.Subject
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tickets
			(id, project_id, number, title, description, status, priority, assignee_id, created_by, created_by_role, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'abierto', 'alta', ?, ?, ?, ?, ?)`,
		ticketID, message.ProjectID, number, title,
		"La respuesta difiere del default asumido para el mensaje "+message.ID,
		message.FromParticipantID, actor.Participant.ID, actor.Participant.AgentRole,
		timefmt.Format(now), timefmt.Format(now),
	); err != nil {
		return fmt.Errorf("crear ticket de corrección: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Input{
		ChainID: message.ProjectID, Actor: auditActor(actor),
		Action: "ticket.crear", ObjectType: "ticket", ObjectID: ticketID, ProjectID: message.ProjectID,
		Payload: map[string]any{
			"ticket_id": ticketID, "ref": fmt.Sprintf("%s-%d", project.Prefix, number),
			"title": title, "correction_for_message": message.ID,
		},
		CreatedAt: now,
	}); err != nil {
		return err
	}
	return nil
}

// ListSentMessages lista mensajes enviados por el participante.
func (s *Service) ListSentMessages(ctx context.Context, actor Identity, projectRef string, limit int, cursor string) ([]MessageView, string, error) {
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, "", err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return nil, "", err
	}
	conditions := []string{"m.from_participant_id = ?"}
	args := []any{actor.Participant.ID}
	if strings.TrimSpace(projectRef) != "" {
		project, err := resolveProject(ctx, s.db, projectRef)
		if err != nil {
			return nil, "", err
		}
		if !rights.CanRead(project.ID) {
			return nil, "", forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "mensaje.listar", project.ID)
		}
		conditions = append(conditions, "m.project_id = ?")
		args = append(args, project.ID)
	}
	if cursor != "" {
		createdAt, id, err := decodeAuditCursor(cursor)
		if err != nil {
			return nil, "", validation("cursor inválido")
		}
		conditions = append(conditions, "(m.created_at < ? OR (m.created_at = ? AND m.id < ?))")
		args = append(args, createdAt, createdAt, id)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, COALESCE(m.project_id, ''), m.from_participant_id, m.type,
		       m.destination_type, COALESCE(m.destination_participant_id, ''),
		       COALESCE(m.destination_role, ''), m.subject, m.body, m.options,
		       m.default_option, m.assume_default_at, COALESCE(m.resolution, ''),
		       COALESCE(m.resolved_by, ''), m.resolved_at, m.response_option,
		       COALESCE(m.response_text, ''), COALESCE(m.taken_by, ''), m.taken_at,
		       COALESCE(m.reply_to, ''), COALESCE(m.correlation_id, ''), m.created_at
		FROM messages m WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY m.created_at DESC, m.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar mensajes enviados: %w", err)
	}
	defer rows.Close()
	messages := make([]domain.Message, 0, limit)
	for rows.Next() {
		message, err := scanMessage(rows)
		if err != nil {
			return nil, "", err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer mensajes enviados: %w", err)
	}
	views := make([]MessageView, 0, len(messages))
	for _, message := range messages {
		view, err := s.messageViewFor(ctx, s.db, actor, message)
		if err != nil {
			return nil, "", err
		}
		views = append(views, view)
	}
	next := ""
	if len(views) > limit {
		views = views[:limit]
		last := views[len(views)-1]
		next = encodeAuditCursor(timefmt.Format(last.CreatedAt), last.ID)
	}
	return views, next, nil
}
