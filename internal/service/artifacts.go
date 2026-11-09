package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mime"
	"path"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// maxArtifactBytes es el máximo de contenido de un artefacto de texto.
const maxArtifactBytes = 1 << 20

// ArtifactInput publica (crea o versiona) un artefacto de proyecto.
type ArtifactInput struct {
	ProjectID    string
	Path         string
	Content      string
	ContentType  string
	SourceCommit string
}

// ArtifactSyncInput es una entrada de sincronización de artefactos.
type ArtifactSyncInput struct {
	Path         string
	Content      string
	ContentType  string
	SourceCommit string
}

// ArtifactView es un artefacto con la versión visible.
type ArtifactView struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	Path           string    `json:"path"`
	ContentType    string    `json:"content_type"`
	CurrentVersion int64     `json:"current_version"`
	Version        int64     `json:"version"`
	Hash           string    `json:"hash"`
	SizeBytes      int64     `json:"size_bytes"`
	SourceCommit   string    `json:"source_commit,omitempty"`
	AuthorID       string    `json:"author_id,omitempty"`
	AuthorName     string    `json:"author_name,omitempty"`
	AuthorRole     string    `json:"author_role,omitempty"`
	CreatedByRole  string    `json:"created_by_role,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Content        string    `json:"content,omitempty"`
	Action         string    `json:"action,omitempty"`
}

// ArtifactSyncResult es el resultado por archivo.
type ArtifactSyncResult struct {
	Path       string `json:"path"`
	Action     string `json:"action"`
	ArtifactID string `json:"artifact_id"`
	Version    int64  `json:"version"`
	Hash       string `json:"hash"`
}

// PublishArtifact crea o versiona un artefacto si el contenido cambió.
func (s *Service) PublishArtifact(ctx context.Context, actor Identity, input ArtifactInput) (ArtifactView, error) {
	project, err := resolveProject(ctx, s.db, input.ProjectID)
	if err != nil {
		return ArtifactView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return ArtifactView{}, err
	}
	if !rights.CanWrite(project.ID) {
		return ArtifactView{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "artefacto.publicar", "")
	}
	artifactPath, err := normalizeArtifactPath(input.Path)
	if err != nil {
		return ArtifactView{}, err
	}
	contentType, err := normalizeArtifactContentType(input.ContentType, artifactPath)
	if err != nil {
		return ArtifactView{}, err
	}
	if err := validateArtifactContent(input.Content); err != nil {
		return ArtifactView{}, err
	}
	sourceCommit, err := normalizeSourceCommit(input.SourceCommit)
	if err != nil {
		return ArtifactView{}, err
	}
	var view ArtifactView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		view, err = s.publishArtifactTx(ctx, tx, actor, project.ID, artifactPath, contentType, sourceCommit, input.Content)
		return err
	})
	if err != nil {
		return ArtifactView{}, err
	}
	return view, nil
}

// SyncArtifacts publica un lote de artefactos en una sola transacción.
func (s *Service) SyncArtifacts(ctx context.Context, actor Identity, projectRef string, inputs []ArtifactSyncInput) ([]ArtifactSyncResult, error) {
	if len(inputs) == 0 || len(inputs) > 200 {
		return nil, validation("sync admite entre 1 y 200 artefactos")
	}
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !rights.CanWrite(project.ID) {
		return nil, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "artefacto.publicar", "")
	}
	results := make([]ArtifactSyncResult, 0, len(inputs))
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		for _, input := range inputs {
			artifactPath, err := normalizeArtifactPath(input.Path)
			if err != nil {
				return err
			}
			contentType, err := normalizeArtifactContentType(input.ContentType, artifactPath)
			if err != nil {
				return err
			}
			if err := validateArtifactContent(input.Content); err != nil {
				return err
			}
			sourceCommit, err := normalizeSourceCommit(input.SourceCommit)
			if err != nil {
				return err
			}
			view, err := s.publishArtifactTx(ctx, tx, actor, project.ID, artifactPath, contentType, sourceCommit, input.Content)
			if err != nil {
				return err
			}
			results = append(results, ArtifactSyncResult{
				Path: artifactPath, Action: view.Action, ArtifactID: view.ID,
				Version: view.Version, Hash: view.Hash,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (s *Service) publishArtifactTx(ctx context.Context, tx *sql.Tx, actor Identity, projectID, artifactPath, contentType, sourceCommit, content string) (ArtifactView, error) {
	now := s.now().UTC()
	hash := hashContent(content)
	size := int64(len(content))
	var artifactID string
	var currentVersion int64
	var currentHash string
	err := tx.QueryRowContext(ctx, `
		SELECT a.id, a.current_version, v.hash
		FROM artifacts a
		JOIN artifact_versions v ON v.artifact_id = a.id AND v.version = a.current_version
		WHERE a.project_id = ? AND a.path = ?`, projectID, artifactPath,
	).Scan(&artifactID, &currentVersion, &currentHash)
	if errors.Is(err, sql.ErrNoRows) {
		artifactID = id.New()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO artifacts(id, project_id, path, content_type, current_version, source_commit,
			                      created_by, created_by_role, created_at, updated_at)
			VALUES (?, ?, ?, ?, 1, ?, ?, ?, ?, ?)`,
			artifactID, projectID, artifactPath, contentType, sourceCommit,
			actor.Participant.ID, actor.Participant.AgentRole,
			timefmt.Format(now), timefmt.Format(now),
		); err != nil {
			if isUniqueConstraint(err) {
				return ArtifactView{}, conflict("artifact_path_taken", "ya existe un artefacto con ese path")
			}
			return ArtifactView{}, fmt.Errorf("crear artefacto: %w", err)
		}
		if err := insertArtifactVersion(ctx, tx, artifactID, 1, content, hash, size, contentType, sourceCommit, actor, now); err != nil {
			return ArtifactView{}, err
		}
		if _, err := audit.Append(ctx, tx, audit.Input{
			ChainID: chainForProject(projectID), Actor: auditActor(actor),
			Action: "artefacto.publicar", ObjectType: "artefacto", ObjectID: artifactID, ProjectID: projectID,
			Payload: map[string]any{
				"artifact_id": artifactID, "path": artifactPath, "version": 1,
				"hash": hash, "size_bytes": size, "content_type": contentType, "source_commit": sourceCommit,
			},
			CreatedAt: now,
		}); err != nil {
			return ArtifactView{}, err
		}
		view, err := s.artifactView(ctx, tx, artifactID, 0)
		if err != nil {
			return ArtifactView{}, err
		}
		view.Action = "creado"
		return view, nil
	}
	if err != nil {
		return ArtifactView{}, fmt.Errorf("buscar artefacto: %w", err)
	}
	if hash == currentHash {
		view, err := s.artifactView(ctx, tx, artifactID, 0)
		if err != nil {
			return ArtifactView{}, err
		}
		view.Action = "sin_cambios"
		return view, nil
	}
	version := currentVersion + 1
	if err := insertArtifactVersion(ctx, tx, artifactID, version, content, hash, size, contentType, sourceCommit, actor, now); err != nil {
		return ArtifactView{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE artifacts
		SET current_version = ?, content_type = ?, source_commit = ?, updated_at = ?
		WHERE id = ?`,
		version, contentType, sourceCommit, timefmt.Format(now), artifactID,
	); err != nil {
		return ArtifactView{}, fmt.Errorf("actualizar artefacto: %w", err)
	}
	if _, err := audit.Append(ctx, tx, audit.Input{
		ChainID: chainForProject(projectID), Actor: auditActor(actor),
		Action: "artefacto.publicar", ObjectType: "artefacto", ObjectID: artifactID, ProjectID: projectID,
		Payload: map[string]any{
			"artifact_id": artifactID, "path": artifactPath, "version": version,
			"hash": hash, "size_bytes": size, "content_type": contentType, "source_commit": sourceCommit,
		},
		CreatedAt: now,
	}); err != nil {
		return ArtifactView{}, err
	}
	view, err := s.artifactView(ctx, tx, artifactID, 0)
	if err != nil {
		return ArtifactView{}, err
	}
	view.Action = "actualizado"
	return view, nil
}

func insertArtifactVersion(ctx context.Context, tx *sql.Tx, artifactID string, version int64, content, hash string, size int64, contentType, sourceCommit string, actor Identity, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO artifact_versions(id, artifact_id, version, content, hash, size_bytes, content_type,
		                              source_commit, author_participant_id, author_role, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id.New(), artifactID, version, content, hash, size, contentType,
		sourceCommit, actor.Participant.ID, actor.Participant.AgentRole, timefmt.Format(now),
	)
	if err != nil {
		return fmt.Errorf("crear versión de artefacto: %w", err)
	}
	return nil
}

// GetArtifactByID devuelve un artefacto (con contenido) por id.
func (s *Service) GetArtifactByID(ctx context.Context, actor Identity, artifactID string, version int64) (ArtifactView, error) {
	return s.artifactView(ctx, s.db, strings.TrimSpace(artifactID), version)
}

// GetArtifactByPath devuelve un artefacto (con contenido) por proyecto y path.
func (s *Service) GetArtifactByPath(ctx context.Context, actor Identity, projectRef, artifactPath string, version int64) (ArtifactView, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return ArtifactView{}, err
	}
	normalized, err := normalizeArtifactPath(artifactPath)
	if err != nil {
		return ArtifactView{}, err
	}
	var artifactID string
	err = s.db.QueryRowContext(ctx,
		`SELECT id FROM artifacts WHERE project_id = ? AND path = ?`,
		project.ID, normalized,
	).Scan(&artifactID)
	if errors.Is(err, sql.ErrNoRows) {
		return ArtifactView{}, notFound("artifact_not_found", "artefacto no encontrado")
	}
	if err != nil {
		return ArtifactView{}, fmt.Errorf("buscar artefacto: %w", err)
	}
	return s.artifactView(ctx, s.db, artifactID, version)
}

// ListArtifacts lista metadatos. Con projectRef vacío lista toda la instancia.
func (s *Service) ListArtifacts(ctx context.Context, actor Identity, projectRef string, limit int, cursor, updatedSince, pathFilter string) ([]ArtifactView, string, error) {
	limit, err := normalizeLimit(limit)
	if err != nil {
		return nil, "", err
	}
	conditions := []string{"1 = 1"}
	args := []any{}
	if strings.TrimSpace(projectRef) != "" {
		project, err := resolveProject(ctx, s.db, projectRef)
		if err != nil {
			return nil, "", err
		}
		conditions = append(conditions, "a.project_id = ?")
		args = append(args, project.ID)
	}
	if filter := strings.TrimSpace(pathFilter); filter != "" {
		normalized, err := normalizeArtifactPath(filter)
		if err != nil {
			return nil, "", err
		}
		conditions = append(conditions, "a.path = ?")
		args = append(args, normalized)
	}
	incremental := strings.TrimSpace(updatedSince) != ""
	if incremental {
		since, err := timefmt.ParseFlexible(updatedSince)
		if err != nil {
			return nil, "", validation("updated_since inválido")
		}
		if cursor != "" {
			updatedAt, lastID, err := decodeAuditCursor(cursor)
			if err != nil {
				return nil, "", validation("cursor inválido")
			}
			conditions = append(conditions, "(a.updated_at > ? OR (a.updated_at = ? AND a.id > ?))")
			args = append(args, updatedAt, updatedAt, lastID)
		} else {
			conditions = append(conditions, "a.updated_at >= ?")
			args = append(args, timefmt.Format(since))
		}
	} else if cursor != "" {
		conditions = append(conditions, "a.updated_at < ?")
		args = append(args, cursor)
	}
	args = append(args, limit+1)
	order := "a.updated_at DESC, a.id DESC"
	if incremental {
		order = "a.updated_at ASC, a.id ASC"
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.project_id, a.path, a.content_type, a.current_version, a.source_commit,
		       COALESCE(a.created_by, ''), a.created_by_role, a.created_at, a.updated_at,
		       v.hash, v.size_bytes, COALESCE(v.author_participant_id, ''), COALESCE(p.name, ''),
		       v.author_role, v.created_at, '' AS content
		FROM artifacts a
		JOIN artifact_versions v ON v.artifact_id = a.id AND v.version = a.current_version
		LEFT JOIN participants p ON p.id = v.author_participant_id
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY `+order+`
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar artefactos: %w", err)
	}
	defer rows.Close()
	views := make([]ArtifactView, 0, limit+1)
	for rows.Next() {
		view, err := scanArtifact(rows)
		if err != nil {
			return nil, "", err
		}
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer artefactos: %w", err)
	}
	next := ""
	if len(views) > limit {
		views = views[:limit]
		last := views[len(views)-1]
		if incremental {
			next = encodeAuditCursor(timefmt.Format(last.UpdatedAt), last.ID)
		} else {
			next = timefmt.Format(last.UpdatedAt)
		}
	}
	return views, next, nil
}

// DeleteArtifact elimina un artefacto y sus versiones.
func (s *Service) DeleteArtifact(ctx context.Context, actor Identity, projectRef, artifactID string) error {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return err
	}
	if !rights.CanWrite(project.ID) {
		return forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "artefacto.eliminar", artifactID)
	}
	now := s.now().UTC()
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var artifactPath string
		err := tx.QueryRowContext(ctx,
			`SELECT path FROM artifacts WHERE id = ? AND project_id = ?`,
			artifactID, project.ID,
		).Scan(&artifactPath)
		if errors.Is(err, sql.ErrNoRows) {
			return notFound("artifact_not_found", "artefacto no encontrado")
		}
		if err != nil {
			return fmt.Errorf("buscar artefacto: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, artifactID); err != nil {
			return fmt.Errorf("eliminar artefacto: %w", err)
		}
		_, err = audit.Append(ctx, tx, audit.Input{
			ChainID: chainForProject(project.ID), Actor: auditActor(actor),
			Action: "artefacto.eliminar", ObjectType: "artefacto", ObjectID: artifactID, ProjectID: project.ID,
			Payload:   map[string]any{"artifact_id": artifactID, "path": artifactPath},
			CreatedAt: now,
		})
		return err
	})
}

func (s *Service) artifactView(ctx context.Context, q queryer, artifactID string, version int64) (ArtifactView, error) {
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return ArtifactView{}, notFound("artifact_not_found", "artefacto no encontrado")
	}
	var currentVersion int64
	err := q.QueryRowContext(ctx, `SELECT current_version FROM artifacts WHERE id = ?`, artifactID).Scan(&currentVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return ArtifactView{}, notFound("artifact_not_found", "artefacto no encontrado")
	}
	if err != nil {
		return ArtifactView{}, fmt.Errorf("buscar artefacto: %w", err)
	}
	if version <= 0 {
		version = currentVersion
	}
	row := q.QueryRowContext(ctx, `
		SELECT a.id, a.project_id, a.path, a.content_type, a.current_version, a.source_commit,
		       COALESCE(a.created_by, ''), a.created_by_role, a.created_at, a.updated_at,
		       v.hash, v.size_bytes, COALESCE(v.author_participant_id, ''), COALESCE(p.name, ''),
		       v.author_role, v.created_at, v.content
		FROM artifacts a
		JOIN artifact_versions v ON v.artifact_id = a.id AND v.version = ?
		LEFT JOIN participants p ON p.id = v.author_participant_id
		WHERE a.id = ?`, version, artifactID)
	view, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ArtifactView{}, notFound("artifact_version_not_found", "versión de artefacto no encontrada")
	}
	if err != nil {
		return ArtifactView{}, err
	}
	return view, nil
}

func scanArtifact(row interface{ Scan(...any) error }) (ArtifactView, error) {
	var view ArtifactView
	var sourceCommit, createdBy, authorID, authorName, authorRole, createdAt, updatedAt, versionCreatedAt string
	if err := row.Scan(&view.ID, &view.ProjectID, &view.Path, &view.ContentType, &view.CurrentVersion,
		&sourceCommit, &createdBy, &view.CreatedByRole, &createdAt, &updatedAt,
		&view.Hash, &view.SizeBytes, &authorID, &authorName, &authorRole,
		&versionCreatedAt, &view.Content); err != nil {
		return ArtifactView{}, err
	}
	view.SourceCommit = sourceCommit
	view.AuthorID = authorID
	view.AuthorName = authorName
	view.AuthorRole = authorRole
	view.Version = view.CurrentVersion
	parsedCreated, err := timefmt.Parse(createdAt)
	if err != nil {
		return ArtifactView{}, fmt.Errorf("created_at de artefacto inválido: %w", err)
	}
	parsedUpdated, err := timefmt.Parse(updatedAt)
	if err != nil {
		return ArtifactView{}, fmt.Errorf("updated_at de artefacto inválido: %w", err)
	}
	view.CreatedAt = parsedCreated
	view.UpdatedAt = parsedUpdated
	return view, nil
}

func normalizeArtifactPath(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", validation("path no puede estar vacío")
	}
	if len(value) > 300 {
		return "", validation("path demasiado largo (máximo 300)")
	}
	if strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return "", validation("path inválido: no empieza ni termina con / ni tiene //")
	}
	if strings.ContainsAny(value, "\\?#\x00\r\n\t") {
		return "", validation("path inválido: usa solo letras, números, '.', '_', '-' y '/'")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", validation("path inválido: no se permiten segmentos vacíos, '.' ni '..'")
		}
	}
	for _, r := range value {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-' || r == '/'
		if !allowed {
			return "", validation("path inválido: usa solo letras, números, '.', '_', '-' y '/'")
		}
	}
	return value, nil
}

func normalizeArtifactContentType(raw, artifactPath string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = inferArtifactContentType(artifactPath)
	}
	if len(value) > 120 {
		return "", validation("content_type demasiado largo")
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return "", validation("content_type inválido")
	}
	switch {
	case mediaType == "application/json",
		mediaType == "application/xml",
		mediaType == "application/x-yaml",
		mediaType == "application/yaml",
		mediaType == "application/toml",
		mediaType == "application/javascript",
		mediaType == "application/x-ndjson",
		mediaType == "image/svg+xml",
		strings.HasPrefix(mediaType, "text/"):
		return mediaType, nil
	default:
		return "", validation("artefacto debe ser texto (JSON, YAML, Markdown, texto plano, etc.)")
	}
}

func inferArtifactContentType(artifactPath string) string {
	switch strings.ToLower(path.Ext(artifactPath)) {
	case ".json":
		return "application/json"
	case ".ndjson", ".jsonl":
		return "application/x-ndjson"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".toml":
		return "application/toml"
	case ".md", ".markdown":
		return "text/markdown"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".csv":
		return "text/csv"
	case ".xml":
		return "application/xml"
	case ".svg":
		return "image/svg+xml"
	case ".js":
		return "application/javascript"
	default:
		return "text/plain"
	}
}

func normalizeSourceCommit(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if len(value) > 64 {
		return "", validation("source_commit demasiado largo (máximo 64)")
	}
	for _, r := range value {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-'
		if !allowed {
			return "", validation("source_commit inválido")
		}
	}
	return value, nil
}

func validateArtifactContent(content string) error {
	if content == "" {
		return validation("content no puede estar vacío")
	}
	if len(content) > maxArtifactBytes {
		return validation("content supera el máximo de 1 MiB")
	}
	return validateNoSecrets(content)
}
