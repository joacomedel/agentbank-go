package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

var (
	slugPattern   = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	prefixPattern = regexp.MustCompile(`^[A-Z0-9]{2,10}$`)
)

// ProjectInput es el alta de un proyecto.
type ProjectInput struct {
	Slug   string
	Name   string
	Prefix string
}

// ProjectView agrega el permiso efectivo del actor.
type ProjectView struct {
	Project    domain.Project
	Permission string
	Roles      []string
}

// CreateProject crea un proyecto y le da membresía admin al creador.
func (s *Service) CreateProject(ctx context.Context, actor Identity, input ProjectInput) (ProjectView, error) {
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return ProjectView{}, err
	}
	if !rights.Admin {
		return ProjectView{}, forbidden("requires_instance_admin", "solo el admin de instancia crea proyectos", "", "proyecto.crear", "")
	}
	slug := strings.TrimSpace(input.Slug)
	if !slugPattern.MatchString(slug) {
		return ProjectView{}, validation("slug inválido: minúsculas, números y guiones, sin guion final")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len([]rune(name)) > 200 {
		return ProjectView{}, validation("name debe tener entre 1 y 200 caracteres")
	}
	prefix := strings.ToUpper(strings.TrimSpace(input.Prefix))
	if prefix != "" && !prefixPattern.MatchString(prefix) {
		return ProjectView{}, validation("prefix debe tener 2..10 caracteres A-Z o 0-9")
	}
	if prefix == "" {
		prefix = derivePrefix(slug)
	}

	var view ProjectView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		finalPrefix, err := uniquePrefix(ctx, tx, prefix, input.Prefix != "")
		if err != nil {
			return err
		}
		view.Project = domain.Project{
			ID:        id.New(),
			Slug:      slug,
			Name:      name,
			Prefix:    finalPrefix,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO projects(id, slug, name, prefix, version, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1, ?, ?)`,
			view.Project.ID, slug, name, finalPrefix, timefmt.Format(now), timefmt.Format(now),
		); err != nil {
			if isUniqueConstraint(err) {
				if strings.Contains(strings.ToLower(err.Error()), "projects.slug") {
					return conflict("project_slug_taken", "ya existe un proyecto con ese slug")
				}
				return conflict("project_prefix_taken", "ya existe un proyecto con ese prefix")
			}
			return fmt.Errorf("crear proyecto: %w", err)
		}
		roles := []string{"dueño"}
		rolesJSON, err := encodeRoles(roles)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memberships(participant_id, project_id, permission, roles, created_at)
			VALUES (?, ?, 'admin', ?, ?)`,
			actor.Participant.ID, view.Project.ID, rolesJSON, timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("crear membresía del creador: %w", err)
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    domain.SystemChain,
			Actor:      audit.Actor{Type: actor.Participant.Type, ID: actor.Participant.ID},
			Action:     "proyecto.crear",
			ObjectType: "proyecto",
			ObjectID:   view.Project.ID,
			ProjectID:  view.Project.ID,
			Payload: map[string]any{
				"project_id": view.Project.ID,
				"slug":       slug,
				"name":       name,
				"prefix":     finalPrefix,
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    view.Project.ID,
			Actor:      audit.Actor{Type: actor.Participant.Type, ID: actor.Participant.ID},
			Action:     "membresia.cambiar",
			ObjectType: "membresia",
			ObjectID:   actor.Participant.ID,
			ProjectID:  view.Project.ID,
			Payload: map[string]any{
				"participant_id": actor.Participant.ID,
				"permission":     domain.PermissionAdmin,
				"roles":          roles,
				"created":        true,
			},
			CreatedAt: now,
		}); err != nil {
			return err
		}
		view.Permission = domain.PermissionAdmin
		view.Roles = roles
		return nil
	})
	if err != nil {
		return ProjectView{}, err
	}
	return view, nil
}

// ListProjects lista solo proyectos legibles por el actor.
func (s *Service) ListProjects(ctx context.Context, actor Identity, limit int, cursor string) ([]ProjectView, string, error) {
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, "", err
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return nil, "", err
	}
	args := []any{actor.Participant.ID}
	where := ""
	if cursor != "" {
		where = " AND p.slug > ?"
		args = append(args, cursor)
	}
	join := "JOIN memberships m ON m.project_id = p.id AND m.participant_id = ?"
	if rights.Admin {
		join = "LEFT JOIN memberships m ON m.project_id = p.id AND m.participant_id = ?"
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.slug, p.name, p.prefix, p.version, p.created_at, p.updated_at,
		       COALESCE(m.permission, ''), COALESCE(m.roles, '[]')
		FROM projects p `+join+`
		WHERE 1 = 1`+where+`
		ORDER BY p.slug ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar proyectos: %w", err)
	}
	defer rows.Close()
	views := make([]ProjectView, 0, limit)
	for rows.Next() {
		view, err := scanProjectView(rows)
		if err != nil {
			return nil, "", err
		}
		if rights.Admin {
			view.Permission = domain.PermissionAdmin
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer proyectos: %w", err)
	}
	next := ""
	if len(views) > limit {
		views = views[:limit]
		next = views[len(views)-1].Project.Slug
	}
	return views, next, nil
}

// GetProject obtiene un proyecto por id o slug si el actor puede leerlo.
func (s *Service) GetProject(ctx context.Context, actor Identity, ref string) (ProjectView, error) {
	project, err := resolveProject(ctx, s.db, ref)
	if err != nil {
		return ProjectView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return ProjectView{}, err
	}
	if !rights.CanRead(project.ID) {
		return ProjectView{}, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "proyecto.leer", project.ID)
	}
	permission, roles := rights.PermissionFor(project.ID)
	return ProjectView{Project: project, Permission: permission, Roles: roles}, nil
}

// ListMembers lista las membresías de un proyecto.
func (s *Service) ListMembers(ctx context.Context, actor Identity, ref string, limit int, cursor string) ([]domain.Member, string, error) {
	project, err := resolveProject(ctx, s.db, ref)
	if err != nil {
		return nil, "", err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, "", err
	}
	if !rights.CanRead(project.ID) {
		return nil, "", forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "membresia.listar", project.ID)
	}
	limit, err = normalizeLimit(limit)
	if err != nil {
		return nil, "", err
	}
	args := []any{project.ID}
	where := ""
	if cursor != "" {
		where = " AND p.name > ?"
		args = append(args, cursor)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.type, p.admin, m.permission, m.roles, m.created_at
		FROM memberships m
		JOIN participants p ON p.id = m.participant_id
		WHERE m.project_id = ?`+where+`
		ORDER BY p.name ASC
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar miembros: %w", err)
	}
	defer rows.Close()
	members := make([]domain.Member, 0, limit)
	for rows.Next() {
		member, err := scanMember(rows)
		if err != nil {
			return nil, "", err
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer miembros: %w", err)
	}
	next := ""
	if len(members) > limit {
		members = members[:limit]
		next = members[len(members)-1].Name
	}
	return members, next, nil
}

// SetMember crea o actualiza una membresía.
func (s *Service) SetMember(ctx context.Context, actor Identity, ref, participantID string, permission string, roles []string) (domain.Member, error) {
	project, err := resolveProject(ctx, s.db, ref)
	if err != nil {
		return domain.Member{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return domain.Member{}, err
	}
	if !rights.CanAdmin(project.ID) {
		return domain.Member{}, forbidden("requires_project_admin", "requiere admin del proyecto", project.ID, "membresia.cambiar", participantID)
	}
	permission = strings.TrimSpace(permission)
	if err := validatePermission(permission); err != nil {
		return domain.Member{}, err
	}
	roles, err = normalizeRoles(roles)
	if err != nil {
		return domain.Member{}, err
	}

	var member domain.Member
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		target, err := getParticipant(ctx, tx, participantID)
		if err != nil {
			return err
		}
		previous, hasPrevious, err := membershipPayload(ctx, tx, participantID, project.ID)
		if err != nil {
			return err
		}
		now := s.now().UTC()
		rolesJSON, err := encodeRoles(roles)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO memberships(participant_id, project_id, permission, roles, created_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(participant_id, project_id) DO UPDATE
			  SET permission = excluded.permission, roles = excluded.roles`,
			participantID, project.ID, permission, rolesJSON, timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("guardar membresía: %w", err)
		}
		payload := map[string]any{
			"participant_id": participantID,
			"permission":     permission,
			"roles":          roles,
			"created":        !hasPrevious,
		}
		if hasPrevious {
			payload["previous"] = previous
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID:    project.ID,
			Actor:      audit.Actor{Type: actor.Participant.Type, ID: actor.Participant.ID},
			Action:     "membresia.cambiar",
			ObjectType: "membresia",
			ObjectID:   participantID,
			ProjectID:  project.ID,
			Payload:    payload,
			CreatedAt:  now,
		}); err != nil {
			return err
		}
		var createdAt string
		if err := tx.QueryRowContext(ctx, `
			SELECT created_at FROM memberships
			WHERE participant_id = ? AND project_id = ?`,
			participantID, project.ID,
		).Scan(&createdAt); err != nil {
			return fmt.Errorf("leer membresía guardada: %w", err)
		}
		parsedCreated, err := timefmt.Parse(createdAt)
		if err != nil {
			return fmt.Errorf("created_at de membresía inválido: %w", err)
		}
		member = domain.Member{
			ParticipantID: target.ID,
			Name:          target.Name,
			Type:          target.Type,
			Admin:         target.Admin,
			Permission:    permission,
			Roles:         roles,
			CreatedAt:     parsedCreated,
		}
		return nil
	})
	if err != nil {
		return domain.Member{}, err
	}
	return member, nil
}

// RemoveMember elimina una membresía.
func (s *Service) RemoveMember(ctx context.Context, actor Identity, ref, participantID string) error {
	project, err := resolveProject(ctx, s.db, ref)
	if err != nil {
		return err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return err
	}
	if !rights.CanAdmin(project.ID) {
		return forbidden("requires_project_admin", "requiere admin del proyecto", project.ID, "membresia.quitar", participantID)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		previous, hasPrevious, err := membershipPayload(ctx, tx, participantID, project.ID)
		if err != nil {
			return err
		}
		if !hasPrevious {
			return notFound("membership_not_found", "membresía no encontrada")
		}
		result, err := tx.ExecContext(ctx,
			`DELETE FROM memberships WHERE participant_id = ? AND project_id = ?`,
			participantID, project.ID,
		)
		if err != nil {
			return fmt.Errorf("quitar membresía: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("verificar membresía: %w", err)
		}
		if affected == 0 {
			return notFound("membership_not_found", "membresía no encontrada")
		}
		_, err = audit.Append(ctx, tx, audit.Input{
			ChainID:    project.ID,
			Actor:      audit.Actor{Type: actor.Participant.Type, ID: actor.Participant.ID},
			Action:     "membresia.quitar",
			ObjectType: "membresia",
			ObjectID:   participantID,
			ProjectID:  project.ID,
			Payload: map[string]any{
				"participant_id": participantID,
				"previous":       previous,
			},
			CreatedAt: s.now().UTC(),
		})
		return err
	})
}

func resolveProject(ctx context.Context, q queryer, ref string) (domain.Project, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return domain.Project{}, notFound("project_not_found", "proyecto no encontrado")
	}
	project, err := scanProject(q.QueryRowContext(ctx, `
		SELECT id, slug, name, prefix, version, created_at, updated_at
		FROM projects
		WHERE id = ? OR slug = ?
		ORDER BY CASE WHEN id = ? THEN 0 ELSE 1 END
		LIMIT 1`, ref, ref, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Project{}, notFound("project_not_found", "proyecto no encontrado")
	}
	if err != nil {
		return domain.Project{}, err
	}
	return project, nil
}

// resolveProjectTx es igual que resolveProject pero sobre una transacción.
func resolveProjectTx(ctx context.Context, tx *sql.Tx, ref string) (domain.Project, error) {
	return resolveProject(ctx, tx, ref)
}

func uniquePrefix(ctx context.Context, tx *sql.Tx, base string, explicit bool) (string, error) {
	candidate := base
	for attempt := 0; attempt < 100; attempt++ {
		var count int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM projects WHERE prefix = ?`, candidate,
		).Scan(&count); err != nil {
			return "", fmt.Errorf("consultar prefix: %w", err)
		}
		if count == 0 {
			return candidate, nil
		}
		if explicit {
			return "", conflict("project_prefix_taken", "ya existe un proyecto con ese prefix")
		}
		attemptNumber := attempt + 2
		suffix := fmt.Sprintf("%d", attemptNumber)
		candidate = base
		if len(candidate)+len(suffix) > 10 {
			candidate = base[:10-len(suffix)]
		}
		candidate += suffix
	}
	return "", conflict("project_prefix_taken", "no se pudo generar un prefix único")
}

func derivePrefix(slug string) string {
	var builder strings.Builder
	for _, r := range strings.ToUpper(slug) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
		}
	}
	prefix := builder.String()
	if prefix == "" {
		return "PRY"
	}
	if len(prefix) > 6 {
		prefix = prefix[:6]
	}
	if len(prefix) < 2 {
		prefix += "1"
	}
	return prefix
}

func scanProjectView(row interface{ Scan(...any) error }) (ProjectView, error) {
	var view ProjectView
	var createdAt, updatedAt, rolesJSON string
	if err := row.Scan(&view.Project.ID, &view.Project.Slug, &view.Project.Name,
		&view.Project.Prefix, &view.Project.Version, &createdAt, &updatedAt,
		&view.Permission, &rolesJSON); err != nil {
		return ProjectView{}, fmt.Errorf("escanear proyecto: %w", err)
	}
	parsedCreated, err := timefmt.Parse(createdAt)
	if err != nil {
		return ProjectView{}, fmt.Errorf("created_at de proyecto inválido: %w", err)
	}
	parsedUpdated, err := timefmt.Parse(updatedAt)
	if err != nil {
		return ProjectView{}, fmt.Errorf("updated_at de proyecto inválido: %w", err)
	}
	view.Project.CreatedAt = parsedCreated
	view.Project.UpdatedAt = parsedUpdated
	if err := json.Unmarshal([]byte(rolesJSON), &view.Roles); err != nil {
		return ProjectView{}, fmt.Errorf("roles inválidos: %w", err)
	}
	if view.Roles == nil {
		view.Roles = []string{}
	}
	return view, nil
}

func scanMember(row interface{ Scan(...any) error }) (domain.Member, error) {
	var member domain.Member
	var admin int
	var rolesJSON, createdAt string
	if err := row.Scan(&member.ParticipantID, &member.Name, &member.Type, &admin,
		&member.Permission, &rolesJSON, &createdAt); err != nil {
		return domain.Member{}, fmt.Errorf("escanear miembro: %w", err)
	}
	member.Admin = admin == 1
	if err := json.Unmarshal([]byte(rolesJSON), &member.Roles); err != nil {
		return domain.Member{}, fmt.Errorf("roles inválidos: %w", err)
	}
	if member.Roles == nil {
		member.Roles = []string{}
	}
	parsed, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Member{}, fmt.Errorf("created_at de membresía inválido: %w", err)
	}
	member.CreatedAt = parsed
	return member, nil
}

func membershipPayload(ctx context.Context, tx *sql.Tx, participantID, projectID string) (map[string]any, bool, error) {
	var permission, rolesJSON string
	var createdAt string
	err := tx.QueryRowContext(ctx, `
		SELECT permission, roles, created_at FROM memberships
		WHERE participant_id = ? AND project_id = ?`,
		participantID, projectID,
	).Scan(&permission, &rolesJSON, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("leer membresía: %w", err)
	}
	var roles []string
	if err := json.Unmarshal([]byte(rolesJSON), &roles); err != nil {
		return nil, false, fmt.Errorf("roles inválidos: %w", err)
	}
	if roles == nil {
		roles = []string{}
	}
	parsed, err := timefmt.Parse(createdAt)
	if err != nil {
		return nil, false, fmt.Errorf("created_at de membresía inválido: %w", err)
	}
	return map[string]any{
		"permission": permission,
		"roles":      roles,
		"created_at": timefmt.Format(parsed),
	}, true, nil
}

func normalizeLimit(limit int) (int, error) {
	if limit == 0 {
		return 50, nil
	}
	if limit < 1 || limit > 200 {
		return 0, validation("limit debe estar entre 1 y 200")
	}
	return limit, nil
}
