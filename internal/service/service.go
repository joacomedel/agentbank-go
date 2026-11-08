// Package service implementa las reglas de negocio de identidad y auditoría.
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
	"github.com/joacomedel/agentbank-go/internal/auth"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/security"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// Service agrupa la base y el reloj inyectable.
type Service struct {
	db       *sql.DB
	now      func() time.Time
	claimTTL time.Duration
}

// New crea el servicio. now nil usa time.Now.
func New(database *sql.DB, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{db: database, now: now, claimTTL: 30 * time.Minute}
}

// Identity es el resultado de autenticar un token.
type Identity struct {
	Participant domain.Participant
	TokenID     string
	SessionID   string
	Origin      string
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func auditActor(identity Identity) audit.Actor {
	return audit.Actor{
		Type: identity.Participant.Type, ID: identity.Participant.ID,
		SessionID: identity.SessionID, Origin: identity.Origin,
	}
}

func validateNoSecrets(fields ...string) error {
	for _, field := range fields {
		if err := security.ScanSecrets(field); err != nil {
			return validation("el contenido parece contener un secreto; quitalo antes de guardar")
		}
	}
	return nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Service) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("abrir transacción: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s *Service) loadRights(ctx context.Context, q queryer, participant domain.Participant) (domain.Rights, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT project_id, permission, roles, created_at
		FROM memberships
		WHERE participant_id = ?
		ORDER BY project_id`, participant.ID)
	if err != nil {
		return domain.Rights{}, fmt.Errorf("listar membresías: %w", err)
	}
	defer rows.Close()
	memberships := make([]domain.Membership, 0, 4)
	for rows.Next() {
		membership, err := scanMembership(rows)
		if err != nil {
			return domain.Rights{}, err
		}
		membership.ParticipantID = participant.ID
		memberships = append(memberships, membership)
	}
	if err := rows.Err(); err != nil {
		return domain.Rights{}, fmt.Errorf("recorrer membresías: %w", err)
	}
	return domain.NewRights(participant.Admin, memberships), nil
}

func scanMembership(row interface{ Scan(...any) error }) (domain.Membership, error) {
	var membership domain.Membership
	var roles string
	var createdAt string
	if err := row.Scan(&membership.ProjectID, &membership.Permission, &roles, &createdAt); err != nil {
		return domain.Membership{}, fmt.Errorf("escanear membresía: %w", err)
	}
	if err := json.Unmarshal([]byte(roles), &membership.Roles); err != nil {
		return domain.Membership{}, fmt.Errorf("roles inválidos: %w", err)
	}
	if membership.Roles == nil {
		membership.Roles = []string{}
	}
	parsed, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Membership{}, fmt.Errorf("created_at de membresía inválido: %w", err)
	}
	membership.CreatedAt = parsed
	return membership, nil
}

func encodeRoles(roles []string) (string, error) {
	if roles == nil {
		roles = []string{}
	}
	encoded, err := json.Marshal(roles)
	if err != nil {
		return "", fmt.Errorf("serializar roles: %w", err)
	}
	return string(encoded), nil
}

func scanParticipant(row interface{ Scan(...any) error }) (domain.Participant, error) {
	var participant domain.Participant
	var admin int
	var createdAt string
	if err := row.Scan(&participant.ID, &participant.Type, &participant.Name, &participant.Status, &admin, &createdAt); err != nil {
		return domain.Participant{}, fmt.Errorf("escanear participante: %w", err)
	}
	participant.Admin = admin == 1
	parsed, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Participant{}, fmt.Errorf("created_at de participante inválido: %w", err)
	}
	participant.CreatedAt = parsed
	return participant, nil
}

func scanProject(row interface{ Scan(...any) error }) (domain.Project, error) {
	var project domain.Project
	var createdAt, updatedAt string
	if err := row.Scan(&project.ID, &project.Slug, &project.Name, &project.Prefix,
		&project.Version, &createdAt, &updatedAt); err != nil {
		return domain.Project{}, fmt.Errorf("escanear proyecto: %w", err)
	}
	parsedCreated, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Project{}, fmt.Errorf("created_at de proyecto inválido: %w", err)
	}
	parsedUpdated, err := timefmt.Parse(updatedAt)
	if err != nil {
		return domain.Project{}, fmt.Errorf("updated_at de proyecto inválido: %w", err)
	}
	project.CreatedAt = parsedCreated
	project.UpdatedAt = parsedUpdated
	return project, nil
}

func getParticipant(ctx context.Context, q queryer, participantID string) (domain.Participant, error) {
	participant, err := scanParticipant(q.QueryRowContext(ctx, `
		SELECT id, type, name, status, admin, created_at FROM participants WHERE id = ?`,
		participantID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Participant{}, notFound("participant_not_found", "participante no encontrado")
	}
	if err != nil {
		return domain.Participant{}, err
	}
	return participant, nil
}

// Authenticate valida el token bearer y devuelve su participante. No crea
// sesión ni audita; eso lo hacen OpenSession y el handler de rechazo.
func (s *Service) Authenticate(ctx context.Context, plainToken string) (Identity, error) {
	if plainToken == "" || len(plainToken) > 512 {
		return Identity{}, ErrInvalidToken
	}
	computed := auth.HashToken(plainToken)
	var identity Identity
	var hash string
	var admin int
	var status string
	var revokedAt sql.NullString
	var createdAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT t.id, t.token_hash, t.revoked_at,
		       p.id, p.type, p.name, p.admin, p.status, p.created_at
		FROM tokens t
		JOIN participants p ON p.id = t.participant_id
		WHERE t.token_hash = ?`, computed,
	).Scan(
		&identity.TokenID, &hash, &revokedAt,
		&identity.Participant.ID, &identity.Participant.Type, &identity.Participant.Name,
		&admin, &status, &createdAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, ErrInvalidToken
	}
	if err != nil {
		return Identity{}, fmt.Errorf("buscar token: %w", err)
	}
	if !auth.EqualHash(hash, computed) {
		return Identity{}, ErrInvalidToken
	}
	if revokedAt.Valid || status != "activo" {
		return Identity{}, ErrInvalidToken
	}
	identity.Participant.Admin = admin == 1
	parsed, err := timefmt.Parse(createdAt)
	if err != nil {
		return Identity{}, fmt.Errorf("created_at de participante inválido: %w", err)
	}
	identity.Participant.CreatedAt = parsed
	return identity, nil
}

// OpenSession valida X-Session-Id o crea una sesión nueva, auditada en la
// cadena system.
func (s *Service) OpenSession(ctx context.Context, identity Identity, sessionID, origin string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	origin = strings.TrimSpace(origin)
	if sessionID != "" {
		var owner string
		err := s.db.QueryRowContext(ctx,
			`SELECT participant_id FROM sessions WHERE id = ?`, sessionID,
		).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != identity.Participant.ID) {
			return "", ErrInvalidSession
		}
		if err != nil {
			return "", fmt.Errorf("buscar sesión: %w", err)
		}
		if err := s.touchActivity(ctx, identity.Participant.ID, sessionID, s.now().UTC()); err != nil {
			return "", err
		}
		return sessionID, nil
	}

	created := ""
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		created = id.New()
		now := s.now().UTC()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sessions(id, participant_id, token_id, origin, started_at, last_seen_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			created, identity.Participant.ID, identity.TokenID, origin,
			timefmt.Format(now), timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("crear sesión: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE participants SET last_heartbeat_at = ? WHERE id = ?`,
			timefmt.Format(now), identity.Participant.ID,
		); err != nil {
			return fmt.Errorf("actualizar heartbeat: %w", err)
		}
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    domain.SystemChain,
			Actor:      auditActor(identity),
			Action:     "sesion.abrir",
			ObjectType: "sesion",
			ObjectID:   created,
			Payload: map[string]any{
				"participant_id": identity.Participant.ID,
				"origin":         origin,
			},
			CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return "", err
	}
	return created, nil
}

func (s *Service) touchActivity(ctx context.Context, participantID, sessionID string, now time.Time) error {
	cutoff := timefmt.Format(now.Add(-time.Minute))
	if _, err := s.db.ExecContext(ctx, `
		UPDATE participants SET last_heartbeat_at = ?
		WHERE id = ? AND (last_heartbeat_at IS NULL OR last_heartbeat_at < ?)`,
		timefmt.Format(now), participantID, cutoff,
	); err != nil {
		return fmt.Errorf("actualizar heartbeat: %w", err)
	}
	if sessionID != "" {
		if _, err := s.db.ExecContext(ctx, `
			UPDATE sessions SET last_seen_at = ?
			WHERE id = ? AND last_seen_at < ?`,
			timefmt.Format(now), sessionID, cutoff,
		); err != nil {
			return fmt.Errorf("actualizar sesión: %w", err)
		}
	}
	return nil
}

// AuditRejection registra un rechazo de autenticación o permisos. Nunca
// guarda tokens, hashes ni cuerpos.
func (s *Service) AuditRejection(ctx context.Context, actor audit.Actor, action, reason, origin, projectID, objectType, objectID string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    domain.SystemChain,
			Actor:      actor,
			Action:     action,
			ObjectType: objectType,
			ObjectID:   objectID,
			ProjectID:  projectID,
			Payload: map[string]any{
				"reason": reason,
				"origin": origin,
			},
			CreatedAt: s.now().UTC(),
		})
		return err
	})
}
