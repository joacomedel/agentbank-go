package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/auth"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

const bootstrapTokenMinLength = 32

var rolePattern = regexp.MustCompile(`^[\p{L}\p{N}_-]+$`)

// BootstrapResult informa si el primer arranque creó al humano.
type BootstrapResult struct {
	Created       bool
	ParticipantID string
}

// Bootstrap crea el participante humano admin de instancia si no existe.
// Si ya hay un humano, la variable de entorno se ignora. Nunca se loguea ni
// se persiste el token en claro.
func (s *Service) Bootstrap(ctx context.Context, bootstrapToken string) (BootstrapResult, error) {
	var result BootstrapResult
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var humans int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM participants WHERE type = 'humano'`,
		).Scan(&humans); err != nil {
			return fmt.Errorf("contar humanos: %w", err)
		}
		if humans > 0 {
			return nil
		}
		token := strings.TrimSpace(bootstrapToken)
		if token == "" {
			return errors.New("falta AGENTBANK_BOOTSTRAP_TOKEN: no existe participante humano y es el primer arranque")
		}
		if len(token) < bootstrapTokenMinLength {
			return fmt.Errorf("AGENTBANK_BOOTSTRAP_TOKEN debe tener al menos %d caracteres", bootstrapTokenMinLength)
		}

		result.Created = true
		result.ParticipantID = id.New()
		now := s.now().UTC()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO participants(id, type, name, admin, created_at)
			VALUES (?, 'humano', 'humano', 1, ?)`,
			result.ParticipantID, timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("crear humano: %w", err)
		}
		tokenID := id.New()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tokens(id, participant_id, token_hash, created_at)
			VALUES (?, ?, ?, ?)`,
			tokenID, result.ParticipantID, auth.HashToken(token), timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("crear token de bootstrap: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    domain.SystemChain,
			Actor:      audit.Actor{Type: domain.AuditActorHuman, ID: result.ParticipantID},
			Action:     "bootstrap.humano",
			ObjectType: "participante",
			ObjectID:   result.ParticipantID,
			Payload: map[string]any{
				"participant_id": result.ParticipantID,
				"token_id":       tokenID,
				"name":           "humano",
				"admin":          true,
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    domain.SystemChain,
			Actor:      audit.Actor{Type: domain.AuditActorHuman, ID: result.ParticipantID},
			Action:     "token.emitir",
			ObjectType: "token",
			ObjectID:   tokenID,
			Payload: map[string]any{
				"participant_id": result.ParticipantID,
				"token_id":       tokenID,
				"purpose":        "bootstrap",
			},
			CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return BootstrapResult{}, err
	}
	return result, nil
}

// MembershipView es una membresía con datos de proyecto para /v1/me.
type MembershipView struct {
	ProjectID   string    `json:"project_id"`
	ProjectSlug string    `json:"project_slug"`
	ProjectName string    `json:"project_name"`
	Permission  string    `json:"permission"`
	Roles       []string  `json:"roles"`
	CreatedAt   time.Time `json:"created_at"`
}

// Me devuelve el participante autenticado y sus membresías.
func (s *Service) Me(ctx context.Context, identity Identity) ([]MembershipView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.project_id, p.slug, p.name, m.permission, m.roles, m.created_at
		FROM memberships m
		JOIN projects p ON p.id = m.project_id
		WHERE m.participant_id = ?
		ORDER BY p.slug
		LIMIT 200`, identity.Participant.ID)
	if err != nil {
		return nil, fmt.Errorf("listar membresías: %w", err)
	}
	defer rows.Close()
	views := make([]MembershipView, 0, 4)
	for rows.Next() {
		var view MembershipView
		var rolesJSON, createdAt string
		if err := rows.Scan(&view.ProjectID, &view.ProjectSlug, &view.ProjectName,
			&view.Permission, &rolesJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("escanear membresía: %w", err)
		}
		if err := json.Unmarshal([]byte(rolesJSON), &view.Roles); err != nil {
			return nil, fmt.Errorf("roles inválidos: %w", err)
		}
		if view.Roles == nil {
			view.Roles = []string{}
		}
		parsed, err := timefmt.Parse(createdAt)
		if err != nil {
			return nil, fmt.Errorf("created_at de membresía inválido: %w", err)
		}
		view.CreatedAt = parsed
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer membresías: %w", err)
	}
	return views, nil
}

// ParticipantProjectInput asigna una membresía al crear un participante.
type ParticipantProjectInput struct {
	ProjectID  string
	Permission string
	Roles      []string
}

// ParticipantInput es el alta de un humano o agente.
type ParticipantInput struct {
	Name     string
	Type     string
	Admin    bool
	Projects []ParticipantProjectInput
}

// ParticipantCreation devuelve el token en claro una sola vez.
type ParticipantCreation struct {
	Participant domain.Participant
	Token       string
	TokenID     string
}

// CreateParticipant crea un participante y emite su primer token.
func (s *Service) CreateParticipant(ctx context.Context, actor Identity, input ParticipantInput) (ParticipantCreation, error) {
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return ParticipantCreation{}, err
	}
	if !rights.Admin {
		return ParticipantCreation{}, forbidden("requires_instance_admin", "solo el admin de instancia crea participantes", "", "participante.crear", "")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 100 {
		return ParticipantCreation{}, validation("name debe tener entre 1 y 100 caracteres")
	}
	participantType := strings.TrimSpace(strings.ToLower(input.Type))
	if participantType != domain.ParticipantHuman && participantType != domain.ParticipantAgent {
		return ParticipantCreation{}, validation("type debe ser humano o agente")
	}
	projects := make([]ParticipantProjectInput, 0, len(input.Projects))
	seenProjects := make(map[string]struct{}, len(input.Projects))
	for _, project := range input.Projects {
		projectID := strings.TrimSpace(project.ProjectID)
		if projectID == "" {
			return ParticipantCreation{}, validation("project_id no puede estar vacío")
		}
		if _, duplicated := seenProjects[projectID]; duplicated {
			return ParticipantCreation{}, validation("project_id repetido en projects")
		}
		seenProjects[projectID] = struct{}{}
		permission := strings.TrimSpace(project.Permission)
		if permission == "" {
			permission = domain.PermissionWriter
		}
		if err := validatePermission(permission); err != nil {
			return ParticipantCreation{}, err
		}
		roles, err := normalizeRoles(project.Roles)
		if err != nil {
			return ParticipantCreation{}, err
		}
		projects = append(projects, ParticipantProjectInput{
			ProjectID:  projectID,
			Permission: permission,
			Roles:      roles,
		})
	}

	var created ParticipantCreation
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		created.Participant = domain.Participant{
			ID:        id.New(),
			Type:      participantType,
			Name:      name,
			Admin:     input.Admin,
			CreatedAt: now,
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO participants(id, type, name, admin, created_at)
			VALUES (?, ?, ?, ?, ?)`,
			created.Participant.ID, participantType, name, boolInt(input.Admin), timefmt.Format(now),
		); err != nil {
			if isUniqueConstraint(err) {
				return conflict("participant_name_taken", "ya existe un participante con ese name")
			}
			return fmt.Errorf("crear participante: %w", err)
		}
		token, err := auth.GenerateToken()
		if err != nil {
			return err
		}
		created.Token = token
		created.TokenID = id.New()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tokens(id, participant_id, token_hash, created_at)
			VALUES (?, ?, ?, ?)`,
			created.TokenID, created.Participant.ID, auth.HashToken(token), timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("crear token: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    domain.SystemChain,
			Actor:      audit.Actor{Type: actor.Participant.Type, ID: actor.Participant.ID},
			Action:     "participante.crear",
			ObjectType: "participante",
			ObjectID:   created.Participant.ID,
			Payload: map[string]any{
				"participant_id": created.Participant.ID,
				"name":           name,
				"type":           participantType,
				"admin":          input.Admin,
				"token_id":       created.TokenID,
				"projects":       projectIDs(projects),
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    domain.SystemChain,
			Actor:      audit.Actor{Type: actor.Participant.Type, ID: actor.Participant.ID},
			Action:     "token.emitir",
			ObjectType: "token",
			ObjectID:   created.TokenID,
			Payload: map[string]any{
				"participant_id": created.Participant.ID,
				"token_id":       created.TokenID,
				"purpose":        "initial",
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		for _, project := range projects {
			resolved, err := resolveProjectTx(ctx, tx, project.ProjectID)
			if err != nil {
				return err
			}
			rolesJSON, err := encodeRoles(project.Roles)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO memberships(participant_id, project_id, permission, roles, created_at)
				VALUES (?, ?, ?, ?, ?)`,
				created.Participant.ID, resolved.ID, project.Permission, rolesJSON, timefmt.Format(now),
			); err != nil {
				if isUniqueConstraint(err) {
					return conflict("membership_exists", "la membresía ya existe")
				}
				return fmt.Errorf("crear membresía: %w", err)
			}
			if _, err := audit.Append(ctx, tx, audit.Input{
				ChainID:    resolved.ID,
				Actor:      audit.Actor{Type: actor.Participant.Type, ID: actor.Participant.ID},
				Action:     "membresia.cambiar",
				ObjectType: "membresia",
				ObjectID:   created.Participant.ID,
				ProjectID:  resolved.ID,
				Payload: map[string]any{
					"participant_id": created.Participant.ID,
					"project_id":     resolved.ID,
					"permission":     project.Permission,
					"roles":          project.Roles,
					"created":        true,
				},
				CreatedAt: now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ParticipantCreation{}, err
	}
	return created, nil
}

// ListParticipants devuelve participantes para usuarios autenticados.
func (s *Service) ListParticipants(ctx context.Context, limit int, cursor string) ([]domain.Participant, string, error) {
	limit, err := normalizeLimit(limit)
	if err != nil {
		return nil, "", err
	}
	args := []any{}
	where := ""
	if cursor != "" {
		where = "WHERE name > ?"
		args = append(args, cursor)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, name, admin, created_at
		FROM participants `+where+`
		ORDER BY name ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar participantes: %w", err)
	}
	defer rows.Close()
	participants := make([]domain.Participant, 0, limit)
	for rows.Next() {
		participant, err := scanParticipant(rows)
		if err != nil {
			return nil, "", err
		}
		participants = append(participants, participant)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer participantes: %w", err)
	}
	next := ""
	if len(participants) > limit {
		participants = participants[:limit]
		next = participants[len(participants)-1].Name
	}
	return participants, next, nil
}

// RotateToken revoca el token activo del participante y emite uno nuevo.
func (s *Service) RotateToken(ctx context.Context, actor Identity, participantID string) (string, string, error) {
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return "", "", err
	}
	if !rights.Admin && actor.Participant.ID != participantID {
		return "", "", forbidden("forbidden", "solo el admin de instancia o el propio participante rota su token", "", "token.emitir", participantID)
	}
	var token, tokenID string
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		target, err := getParticipant(ctx, tx, participantID)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		rows, err := tx.QueryContext(ctx,
			`SELECT id FROM tokens WHERE participant_id = ? AND revoked_at IS NULL`,
			target.ID)
		if err != nil {
			return fmt.Errorf("buscar tokens activos: %w", err)
		}
		previous := make([]string, 0, 1)
		for rows.Next() {
			var previousID string
			if err := rows.Scan(&previousID); err != nil {
				rows.Close()
				return fmt.Errorf("escanear token activo: %w", err)
			}
			previous = append(previous, previousID)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("recorrer tokens activos: %w", err)
		}
		_ = rows.Close()
		if _, err := tx.ExecContext(ctx,
			`UPDATE tokens SET revoked_at = ? WHERE participant_id = ? AND revoked_at IS NULL`,
			timefmt.Format(now), target.ID,
		); err != nil {
			return fmt.Errorf("revocar token anterior: %w", err)
		}
		for _, previousID := range previous {
			if _, err := audit.Append(ctx, tx, audit.Input{
				ChainID:    domain.SystemChain,
				Actor:      audit.Actor{Type: actor.Participant.Type, ID: actor.Participant.ID},
				Action:     "token.revocar",
				ObjectType: "token",
				ObjectID:   previousID,
				Payload: map[string]any{
					"token_id":       previousID,
					"participant_id": target.ID,
					"revoked_at":     timefmt.Format(now),
					"rotated":        true,
				},
				CreatedAt: now,
			}); err != nil {
				return err
			}
		}
		token, err = auth.GenerateToken()
		if err != nil {
			return err
		}
		tokenID = id.New()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tokens(id, participant_id, token_hash, created_at)
			VALUES (?, ?, ?, ?)`,
			tokenID, target.ID, auth.HashToken(token), timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("crear token: %w", err)
		}
		_, err = audit.Append(ctx, tx, audit.Input{
			ChainID:    domain.SystemChain,
			Actor:      audit.Actor{Type: actor.Participant.Type, ID: actor.Participant.ID},
			Action:     "token.emitir",
			ObjectType: "token",
			ObjectID:   tokenID,
			Payload: map[string]any{
				"participant_id": target.ID,
				"token_id":       tokenID,
				"rotated":        true,
				"previous":       previous,
			},
			CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return "", "", err
	}
	return token, tokenID, nil
}

// RevokeToken revoca un token por id.
func (s *Service) RevokeToken(ctx context.Context, actor Identity, tokenID string) (time.Time, error) {
	var participantID string
	var revokedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT participant_id, revoked_at FROM tokens WHERE id = ?`, tokenID,
	).Scan(&participantID, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, notFound("token_not_found", "token no encontrado")
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("buscar token: %w", err)
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return time.Time{}, err
	}
	if !rights.Admin && actor.Participant.ID != participantID {
		return time.Time{}, forbidden("forbidden", "solo el admin de instancia o el propio participante revoca su token", "", "token.revocar", tokenID)
	}
	if revokedAt.Valid {
		return time.Time{}, conflict("token_already_revoked", "el token ya estaba revocado")
	}
	now := s.now().UTC()
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx,
			`UPDATE tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
			timefmt.Format(now), tokenID,
		)
		if err != nil {
			return fmt.Errorf("revocar token: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("verificar revocación: %w", err)
		}
		if affected == 0 {
			return conflict("token_already_revoked", "el token ya estaba revocado")
		}
		_, err = audit.Append(ctx, tx, audit.Input{
			ChainID:    domain.SystemChain,
			Actor:      audit.Actor{Type: actor.Participant.Type, ID: actor.Participant.ID},
			Action:     "token.revocar",
			ObjectType: "token",
			ObjectID:   tokenID,
			Payload: map[string]any{
				"token_id":       tokenID,
				"participant_id": participantID,
				"revoked_at":     timefmt.Format(now),
			},
			CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return time.Time{}, err
	}
	return now, nil
}

func validatePermission(permission string) error {
	switch permission {
	case domain.PermissionReader, domain.PermissionWriter, domain.PermissionAdmin:
		return nil
	default:
		return validation("permission debe ser lector, colaborador o admin")
	}
}

func normalizeRoles(roles []string) ([]string, error) {
	if len(roles) > 20 {
		return nil, validation("roles admite como máximo 20 elementos")
	}
	normalized := make([]string, 0, len(roles))
	seen := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" || len([]rune(role)) > 32 || !rolePattern.MatchString(role) {
			return nil, validation("cada rol debe tener 1..32 caracteres alfanuméricos, guion o guion bajo")
		}
		if _, duplicated := seen[role]; duplicated {
			continue
		}
		seen[role] = struct{}{}
		normalized = append(normalized, role)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func projectIDs(projects []ParticipantProjectInput) []string {
	ids := make([]string, 0, len(projects))
	for _, project := range projects {
		ids = append(ids, project.ProjectID)
	}
	return ids
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func isUniqueConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
