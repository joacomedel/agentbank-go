package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/id"
	"github.com/joacomedel/agentbank-go/internal/timefmt"
)

// DocumentInput es el alta de un documento.
type DocumentInput struct {
	ProjectID string
	Title     string
	Route     string
	Type      string
	Pinned    bool
	Content   string
}

// DocumentView es un documento con su versión visible.
type DocumentView struct {
	ID             string                `json:"id"`
	ProjectID      string                `json:"project_id,omitempty"`
	Title          string                `json:"title"`
	Route          string                `json:"route,omitempty"`
	Type           string                `json:"type"`
	Pinned         bool                  `json:"pinned"`
	CurrentVersion int64                 `json:"current_version"`
	Version        int64                 `json:"version"`
	Hash           string                `json:"hash"`
	Content        string                `json:"content"`
	ContentContent domain.ForeignContent `json:"content_content"`
	AuthorID       string                `json:"author_id,omitempty"`
	AuthorName     string                `json:"author_name,omitempty"`
	CreatedAt      time.Time             `json:"created_at"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

// DocumentVersionView es una versión inmutable.
type DocumentVersionView struct {
	ID             string                `json:"id"`
	DocumentID     string                `json:"document_id"`
	Version        int64                 `json:"version"`
	Hash           string                `json:"hash"`
	Content        string                `json:"content"`
	ContentContent domain.ForeignContent `json:"content_content"`
	AuthorID       string                `json:"author_id,omitempty"`
	AuthorName     string                `json:"author_name,omitempty"`
	CreatedAt      time.Time             `json:"created_at"`
}

// DiffLine es una línea de diff.
type DiffLine struct {
	Op   string `json:"op"`
	Line string `json:"line"`
}

// DocumentSyncInput es una entrada de sincronización.
type DocumentSyncInput struct {
	Route   string
	Title   string
	Type    string
	Content string
}

// DocumentSyncResult es el resultado por archivo.
type DocumentSyncResult struct {
	Route      string `json:"route"`
	Action     string `json:"action"`
	DocumentID string `json:"document_id"`
	Version    int64  `json:"version"`
}

// CreateDocument crea un documento con su primera versión.
func (s *Service) CreateDocument(ctx context.Context, actor Identity, input DocumentInput) (DocumentView, error) {
	projectID := ""
	if strings.TrimSpace(input.ProjectID) != "" {
		project, err := resolveProject(ctx, s.db, input.ProjectID)
		if err != nil {
			return DocumentView{}, err
		}
		projectID = project.ID
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return DocumentView{}, err
	}
	if projectID == "" {
		if !rights.Admin {
			return DocumentView{}, forbidden("requires_instance_admin", "solo el admin de instancia escribe documentos globales", "", "documento.crear", "")
		}
	} else if !rights.CanWrite(projectID) {
		return DocumentView{}, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", projectID, "documento.crear", "")
	}
	title := strings.TrimSpace(input.Title)
	if title == "" || len([]rune(title)) > 200 {
		return DocumentView{}, validation("title debe tener entre 1 y 200 caracteres")
	}
	route := strings.TrimSpace(input.Route)
	if len(route) > 300 {
		return DocumentView{}, validation("route demasiado larga")
	}
	docType := strings.TrimSpace(input.Type)
	if docType == "" {
		docType = "doc"
	}
	if docType != "doc" && docType != "glosario" {
		return DocumentView{}, validation("type debe ser doc o glosario")
	}
	if input.Content == "" {
		return DocumentView{}, validation("content no puede estar vacío")
	}
	if err := validateNoSecrets(title, input.Content); err != nil {
		return DocumentView{}, err
	}
	var view DocumentView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		view, err = s.createDocumentTx(ctx, tx, actor, projectID, title, route, docType, input.Pinned, input.Content)
		return err
	})
	if err != nil {
		return DocumentView{}, err
	}
	return view, nil
}

func (s *Service) createDocumentTx(ctx context.Context, tx *sql.Tx, actor Identity, projectID, title, route, docType string, pinned bool, content string) (DocumentView, error) {
	now := s.now().UTC()
	doc := domain.Document{
		ID: id.New(), ProjectID: projectID, Title: title, Route: route, Type: docType,
		Pinned: pinned, CurrentVersion: 1, CreatedBy: actor.Participant.ID,
		CreatedAt: now, UpdatedAt: now,
	}
	hash := hashContent(content)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO documents(id, project_id, title, route, type, pinned, current_version, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
		doc.ID, nullable(projectID), title, nullable(route), docType, boolInt(pinned),
		actor.Participant.ID, timefmt.Format(now), timefmt.Format(now),
	); err != nil {
		if isUniqueConstraint(err) {
			return DocumentView{}, conflict("document_route_taken", "ya existe un documento con esa route")
		}
		return DocumentView{}, fmt.Errorf("crear documento: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO document_versions(id, document_id, version, content, hash, author_participant_id, created_at)
		VALUES (?, ?, 1, ?, ?, ?, ?)`,
		id.New(), doc.ID, content, hash, actor.Participant.ID, timefmt.Format(now),
	); err != nil {
		return DocumentView{}, fmt.Errorf("crear versión inicial: %w", err)
	}
	chainID := chainForProject(projectID)
	if _, err := audit.Append(ctx, tx, audit.Input{
		ChainID: chainID, Actor: auditActor(actor),
		Action: "documento.crear", ObjectType: "documento", ObjectID: doc.ID, ProjectID: projectID,
		Payload:   map[string]any{"document_id": doc.ID, "title": title, "route": route, "version": 1, "hash": hash},
		CreatedAt: now,
	}); err != nil {
		return DocumentView{}, err
	}
	return DocumentView{
		ID: doc.ID, ProjectID: projectID, Title: title, Route: route, Type: docType,
		Pinned: pinned, CurrentVersion: 1, Version: 1, Hash: hash, Content: content,
		ContentContent: domain.WrapForeign(content, "document"), AuthorID: actor.Participant.ID,
		AuthorName: actor.Participant.Name, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// GetDocument devuelve la versión actual o una concreta.
func (s *Service) GetDocument(ctx context.Context, actor Identity, ref string, version int64) (DocumentView, error) {
	doc, err := resolveDocument(ctx, s.db, ref)
	if err != nil {
		return DocumentView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return DocumentView{}, err
	}
	if !canReadDocument(rights, doc) {
		return DocumentView{}, forbidden("forbidden", "sin permiso para leer el documento", doc.ProjectID, "documento.leer", doc.ID)
	}
	if version == 0 {
		version = doc.CurrentVersion
	}
	return s.documentVersionView(ctx, s.db, doc, version)
}

// ListDocuments lista documentos de proyecto + globales, o solo globales.
func (s *Service) ListDocuments(ctx context.Context, actor Identity, projectRef string, limit int, cursor string) ([]DocumentView, string, error) {
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, "", err
	}
	limit, err = normalizeLimit(limit)
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
		if !rights.CanRead(project.ID) {
			return nil, "", forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "documento.listar", project.ID)
		}
		conditions = append(conditions, "(d.project_id = ? OR d.project_id IS NULL)")
		args = append(args, project.ID)
	} else {
		conditions = append(conditions, "d.project_id IS NULL")
	}
	if cursor != "" {
		conditions = append(conditions, "d.updated_at < ?")
		args = append(args, cursor)
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, COALESCE(d.project_id, ''), d.title, COALESCE(d.route, ''), d.type,
		       d.pinned, d.current_version, COALESCE(d.created_by, ''), d.created_at, d.updated_at
		FROM documents d
		WHERE `+strings.Join(conditions, " AND ")+`
		ORDER BY d.updated_at DESC, d.id DESC
		LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("listar documentos: %w", err)
	}
	defer rows.Close()
	docs := make([]domain.Document, 0, limit)
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, "", err
		}
		if doc.ProjectID != "" && !rights.CanRead(doc.ProjectID) {
			continue
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("recorrer documentos: %w", err)
	}
	views := make([]DocumentView, 0, len(docs))
	for _, doc := range docs {
		view, err := s.documentVersionView(ctx, s.db, doc, doc.CurrentVersion)
		if err != nil {
			return nil, "", err
		}
		view.Content = ""
		view.ContentContent = domain.ForeignContent{}
		views = append(views, view)
	}
	next := ""
	if len(views) > limit {
		views = views[:limit]
		next = timefmt.Format(views[len(views)-1].UpdatedAt)
	}
	return views, next, nil
}

// CreateDocumentVersion crea una versión si el contenido cambió.
func (s *Service) CreateDocumentVersion(ctx context.Context, actor Identity, ref, content string) (DocumentView, error) {
	doc, err := resolveDocument(ctx, s.db, ref)
	if err != nil {
		return DocumentView{}, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return DocumentView{}, err
	}
	if !canWriteDocument(rights, doc) {
		return DocumentView{}, forbidden("requires_project_write", "sin permiso para versionar el documento", doc.ProjectID, "documento.version", doc.ID)
	}
	if content == "" {
		return DocumentView{}, validation("content no puede estar vacío")
	}
	if err := validateNoSecrets(content); err != nil {
		return DocumentView{}, err
	}
	var view DocumentView
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		view, err = s.createDocumentVersionTx(ctx, tx, actor, doc, content)
		return err
	})
	if err != nil {
		return DocumentView{}, err
	}
	return view, nil
}

func (s *Service) createDocumentVersionTx(ctx context.Context, tx *sql.Tx, actor Identity, doc domain.Document, content string) (DocumentView, error) {
	hash := hashContent(content)
	var currentHash string
	if err := tx.QueryRowContext(ctx, `
		SELECT hash FROM document_versions WHERE document_id = ? AND version = ?`,
		doc.ID, doc.CurrentVersion,
	).Scan(&currentHash); err != nil {
		return DocumentView{}, fmt.Errorf("leer hash actual: %w", err)
	}
	if hash == currentHash {
		return s.documentVersionView(ctx, tx, doc, doc.CurrentVersion)
	}
	now := s.now().UTC()
	version := doc.CurrentVersion + 1
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO document_versions(id, document_id, version, content, hash, author_participant_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id.New(), doc.ID, version, content, hash, actor.Participant.ID, timefmt.Format(now),
	); err != nil {
		return DocumentView{}, fmt.Errorf("crear versión: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE documents SET current_version = ?, updated_at = ? WHERE id = ?`,
		version, timefmt.Format(now), doc.ID,
	); err != nil {
		return DocumentView{}, fmt.Errorf("actualizar documento: %w", err)
	}
	chainID := chainForProject(doc.ProjectID)
	if _, err := audit.Append(ctx, tx, audit.Input{
		ChainID: chainID, Actor: auditActor(actor),
		Action: "documento.version", ObjectType: "documento", ObjectID: doc.ID, ProjectID: doc.ProjectID,
		Payload:   map[string]any{"document_id": doc.ID, "version": version, "hash": hash},
		CreatedAt: now,
	}); err != nil {
		return DocumentView{}, err
	}
	doc.CurrentVersion = version
	doc.UpdatedAt = now
	return DocumentView{
		ID: doc.ID, ProjectID: doc.ProjectID, Title: doc.Title, Route: doc.Route, Type: doc.Type,
		Pinned: doc.Pinned, CurrentVersion: version, Version: version, Hash: hash, Content: content,
		ContentContent: domain.WrapForeign(content, "document"), AuthorID: actor.Participant.ID,
		AuthorName: actor.Participant.Name, CreatedAt: doc.CreatedAt, UpdatedAt: now,
	}, nil
}

// ListDocumentVersions lista metadatos de versiones.
func (s *Service) ListDocumentVersions(ctx context.Context, actor Identity, ref string) ([]DocumentVersionView, error) {
	doc, err := resolveDocument(ctx, s.db, ref)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !canReadDocument(rights, doc) {
		return nil, forbidden("forbidden", "sin permiso para leer el documento", doc.ProjectID, "documento.versiones", doc.ID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.id, v.document_id, v.version, v.hash, v.content,
		       v.author_participant_id, COALESCE(p.name, ''), v.created_at
		FROM document_versions v
		LEFT JOIN participants p ON p.id = v.author_participant_id
		WHERE v.document_id = ?
		ORDER BY v.version DESC`, doc.ID)
	if err != nil {
		return nil, fmt.Errorf("listar versiones: %w", err)
	}
	defer rows.Close()
	views := make([]DocumentVersionView, 0, 8)
	for rows.Next() {
		var view DocumentVersionView
		var createdAt string
		if err := rows.Scan(&view.ID, &view.DocumentID, &view.Version, &view.Hash, &view.Content,
			&view.AuthorID, &view.AuthorName, &createdAt); err != nil {
			return nil, fmt.Errorf("escanear versión: %w", err)
		}
		parsed, err := timefmt.Parse(createdAt)
		if err != nil {
			return nil, fmt.Errorf("created_at de versión inválido: %w", err)
		}
		view.CreatedAt = parsed
		view.ContentContent = domain.WrapForeign(view.Content, "document")
		views = append(views, view)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer versiones: %w", err)
	}
	return views, nil
}

// DiffDocument devuelve un diff por líneas entre dos versiones.
func (s *Service) DiffDocument(ctx context.Context, actor Identity, ref string, from, to int64) (map[string]any, error) {
	doc, err := resolveDocument(ctx, s.db, ref)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !canReadDocument(rights, doc) {
		return nil, forbidden("forbidden", "sin permiso para leer el documento", doc.ProjectID, "documento.diff", doc.ID)
	}
	if to == 0 {
		to = doc.CurrentVersion
	}
	if from == 0 {
		from = to - 1
	}
	if from < 0 || to < 1 || from == to {
		return nil, validation("from/to inválidos")
	}
	fromText, err := documentContent(ctx, s.db, doc.ID, from)
	if err != nil {
		return nil, err
	}
	toText, err := documentContent(ctx, s.db, doc.ID, to)
	if err != nil {
		return nil, err
	}
	lines := lineDiff(fromText, toText)
	return map[string]any{
		"document_id": doc.ID,
		"from":        from,
		"to":          to,
		"lines":       lines,
	}, nil
}

// SyncDocuments crea o versiona documentos por ruta.
func (s *Service) SyncDocuments(ctx context.Context, actor Identity, projectRef string, inputs []DocumentSyncInput) ([]DocumentSyncResult, error) {
	if len(inputs) == 0 || len(inputs) > 200 {
		return nil, validation("sync admite entre 1 y 200 documentos")
	}
	projectID := ""
	if strings.TrimSpace(projectRef) != "" {
		project, err := resolveProject(ctx, s.db, projectRef)
		if err != nil {
			return nil, err
		}
		projectID = project.ID
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		if !rights.Admin {
			return nil, forbidden("requires_instance_admin", "solo el admin de instancia sincroniza documentos globales", "", "documento.sync", "")
		}
	} else if !rights.CanWrite(projectID) {
		return nil, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", projectID, "documento.sync", "")
	}
	results := make([]DocumentSyncResult, 0, len(inputs))
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		for _, input := range inputs {
			route := strings.TrimSpace(input.Route)
			if route == "" {
				return validation("route no puede estar vacía en sync")
			}
			existing, err := documentByRoute(ctx, tx, projectID, route)
			if errors.Is(err, sql.ErrNoRows) {
				title := strings.TrimSpace(input.Title)
				if title == "" {
					title = route
				}
				view, err := s.createDocumentTx(ctx, tx, actor, projectID, title, route, input.Type, false, input.Content)
				if err != nil {
					return err
				}
				results = append(results, DocumentSyncResult{Route: route, Action: "creado", DocumentID: view.ID, Version: 1})
				continue
			}
			if err != nil {
				return err
			}
			view, err := s.createDocumentVersionTx(ctx, tx, actor, existing, input.Content)
			if err != nil {
				return err
			}
			action := "actualizado"
			if view.Version == existing.CurrentVersion {
				action = "sin_cambios"
			}
			results = append(results, DocumentSyncResult{Route: route, Action: action, DocumentID: existing.ID, Version: view.Version})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// CreateDecision crea un ADR.
func (s *Service) CreateDecision(ctx context.Context, actor Identity, projectRef, title, body, sourceMessageID string) (map[string]any, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !rights.CanWrite(project.ID) {
		return nil, forbidden("requires_project_write", "sin permiso de escritura en el proyecto", project.ID, "decision.crear", project.ID)
	}
	title = strings.TrimSpace(title)
	if title == "" || len([]rune(title)) > 200 {
		return nil, validation("title debe tener entre 1 y 200 caracteres")
	}
	if strings.TrimSpace(body) == "" {
		return nil, validation("body no puede estar vacío")
	}
	if err := validateNoSecrets(title, body); err != nil {
		return nil, err
	}
	decisionID := id.New()
	now := s.now().UTC()
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		if sourceMessageID != "" {
			if _, err := resolveMessage(ctx, tx, sourceMessageID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO decisions(id, project_id, title, status, body, source_message_id, created_by, created_at)
			VALUES (?, ?, ?, 'vigente', ?, ?, ?, ?)`,
			decisionID, project.ID, title, body, nullable(sourceMessageID), actor.Participant.ID, timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("crear decisión: %w", err)
		}
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID: project.ID, Actor: auditActor(actor),
			Action: "decision.crear", ObjectType: "decision", ObjectID: decisionID, ProjectID: project.ID,
			Payload:   map[string]any{"decision_id": decisionID, "title": title, "source_message_id": sourceMessageID},
			CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"decision_id": decisionID, "title": title, "status": "vigente"}, nil
}

// ListDecisions lista decisiones de un proyecto.
func (s *Service) ListDecisions(ctx context.Context, actor Identity, projectRef string) ([]map[string]any, error) {
	project, err := resolveProject(ctx, s.db, projectRef)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !rights.CanRead(project.ID) {
		return nil, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "decision.listar", project.ID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, status, body, COALESCE(source_message_id, ''), COALESCE(created_by, ''), created_at
		FROM decisions WHERE project_id = ? ORDER BY created_at DESC LIMIT 200`, project.ID)
	if err != nil {
		return nil, fmt.Errorf("listar decisiones: %w", err)
	}
	defer rows.Close()
	items := make([]map[string]any, 0, 8)
	for rows.Next() {
		var decisionID, title, status, body, source, createdBy, createdAt string
		if err := rows.Scan(&decisionID, &title, &status, &body, &source, &createdBy, &createdAt); err != nil {
			return nil, fmt.Errorf("escanear decisión: %w", err)
		}
		parsed, err := timefmt.Parse(createdAt)
		if err != nil {
			return nil, fmt.Errorf("created_at de decisión inválido: %w", err)
		}
		items = append(items, map[string]any{
			"id": decisionID, "title": title, "status": status, "body": body,
			"body_content":      domain.WrapForeign(body, "decision"),
			"source_message_id": source, "created_by": createdBy, "created_at": parsed,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer decisiones: %w", err)
	}
	return items, nil
}

// AttachDocumentToTicket adjunta un documento a un ticket.
func (s *Service) AttachDocumentToTicket(ctx context.Context, actor Identity, ticketRef, documentRef string) error {
	tc, err := resolveTicket(ctx, s.db, ticketRef)
	if err != nil {
		return err
	}
	doc, err := resolveDocument(ctx, s.db, documentRef)
	if err != nil {
		return err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return err
	}
	if !rights.CanWrite(tc.Project.ID) {
		return forbidden("requires_project_write", "sin permiso de escritura en el proyecto", tc.Project.ID, "ticket.documento.adjuntar", tc.Ticket.ID)
	}
	if !canReadDocument(rights, doc) {
		return forbidden("forbidden", "sin permiso para leer el documento", doc.ProjectID, "ticket.documento.adjuntar", doc.ID)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		now := s.now().UTC()
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO ticket_documents(ticket_id, document_id, created_at)
			VALUES (?, ?, ?)`, tc.Ticket.ID, doc.ID, timefmt.Format(now),
		); err != nil {
			return fmt.Errorf("adjuntar documento: %w", err)
		}
		_, err := audit.Append(ctx, tx, audit.Input{
			ChainID: tc.Project.ID, Actor: auditActor(actor),
			Action: "ticket.documento.adjuntar", ObjectType: "ticket", ObjectID: tc.Ticket.ID, ProjectID: tc.Project.ID,
			Payload: map[string]any{"ticket_id": tc.Ticket.ID, "document_id": doc.ID}, CreatedAt: now,
		})
		return err
	})
}

// ListTicketDocuments lista documentos adjuntos a un ticket.
func (s *Service) ListTicketDocuments(ctx context.Context, actor Identity, ticketRef string) ([]DocumentView, error) {
	tc, err := resolveTicket(ctx, s.db, ticketRef)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	if !rights.CanRead(tc.Project.ID) {
		return nil, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", tc.Project.ID, "ticket.documentos", tc.Ticket.ID)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, COALESCE(d.project_id, ''), d.title, COALESCE(d.route, ''), d.type,
		       d.pinned, d.current_version, COALESCE(d.created_by, ''), d.created_at, d.updated_at
		FROM ticket_documents td
		JOIN documents d ON d.id = td.document_id
		WHERE td.ticket_id = ? ORDER BY d.title`, tc.Ticket.ID)
	if err != nil {
		return nil, fmt.Errorf("listar documentos del ticket: %w", err)
	}
	defer rows.Close()
	docs := make([]domain.Document, 0, 4)
	for rows.Next() {
		doc, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer documentos del ticket: %w", err)
	}
	views := make([]DocumentView, 0, len(docs))
	for _, doc := range docs {
		view, err := s.documentVersionView(ctx, s.db, doc, doc.CurrentVersion)
		if err != nil {
			return nil, err
		}
		view.Content = ""
		view.ContentContent = domain.ForeignContent{}
		views = append(views, view)
	}
	return views, nil
}

// Search busca tickets, documentos y mensajes.
func (s *Service) Search(ctx context.Context, actor Identity, query, types, projectRef string, limit int) ([]map[string]any, error) {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < 2 {
		return nil, validation("q debe tener al menos 2 caracteres")
	}
	types = strings.TrimSpace(types)
	if types == "" {
		types = "ticket,document,message"
	}
	allowed := map[string]bool{}
	for _, t := range strings.Split(types, ",") {
		t = strings.TrimSpace(t)
		if t != "ticket" && t != "document" && t != "message" {
			return nil, validation("type debe ser ticket, document o message")
		}
		allowed[t] = true
	}
	limit, err := normalizeLimit(limit)
	if err != nil {
		return nil, err
	}
	rights, err := s.loadRights(ctx, s.db, actor.Participant)
	if err != nil {
		return nil, err
	}
	projectIDs := rights.ProjectIDs()
	projectFilter := ""
	argsPrefix := []any{}
	if strings.TrimSpace(projectRef) != "" {
		project, err := resolveProject(ctx, s.db, projectRef)
		if err != nil {
			return nil, err
		}
		if !rights.CanRead(project.ID) {
			return nil, forbidden("requires_project_read", "sin permiso de lectura en el proyecto", project.ID, "search", project.ID)
		}
		projectFilter = " AND t.project_id = ?"
		argsPrefix = append(argsPrefix, project.ID)
	} else if !rights.Admin {
		if len(projectIDs) == 0 {
			return []map[string]any{}, nil
		}
		placeholders := make([]string, len(projectIDs))
		for i, id := range projectIDs {
			placeholders[i] = "?"
			argsPrefix = append(argsPrefix, id)
		}
		projectFilter = " AND t.project_id IN (" + strings.Join(placeholders, ",") + ")"
	}
	like := "%" + query + "%"
	items := make([]map[string]any, 0, limit)
	if allowed["ticket"] {
		args := append([]any{}, argsPrefix...)
		args = append(args, like, like)
		rows, err := s.db.QueryContext(ctx, `
			SELECT t.id, t.project_id, p.prefix || '-' || t.number, t.title, t.description, t.status
			FROM tickets t JOIN projects p ON p.id = t.project_id
			WHERE (t.title LIKE ? OR t.description LIKE ?)`+projectFilter+`
			ORDER BY t.updated_at DESC LIMIT ?`,
			append(args, limit)...)
		if err != nil {
			return nil, fmt.Errorf("buscar tickets: %w", err)
		}
		for rows.Next() {
			var ticketID, projectID, ref, title, description, status string
			if err := rows.Scan(&ticketID, &projectID, &ref, &title, &description, &status); err != nil {
				rows.Close()
				return nil, fmt.Errorf("escanear ticket: %w", err)
			}
			items = append(items, map[string]any{
				"type": "ticket", "id": ticketID, "project_id": projectID, "ref": ref,
				"title": title, "snippet": domain.WrapForeign(truncate(description, 300), "search"),
				"status": status,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("recorrer tickets: %w", err)
		}
		_ = rows.Close()
	}
	if allowed["document"] {
		args := []any{like, like}
		docFilter := ""
		if strings.TrimSpace(projectRef) != "" {
			docFilter = " AND (d.project_id = ? OR d.project_id IS NULL)"
			args = append(args, argsPrefix[0])
		} else if !rights.Admin {
			if len(projectIDs) > 0 {
				placeholders := make([]string, len(projectIDs))
				for i, id := range projectIDs {
					placeholders[i] = "?"
					args = append(args, id)
				}
				docFilter = " AND (d.project_id IN (" + strings.Join(placeholders, ",") + ") OR d.project_id IS NULL)"
			} else {
				docFilter = " AND d.project_id IS NULL"
			}
		}
		args = append(args, limit)
		rows, err := s.db.QueryContext(ctx, `
			SELECT d.id, COALESCE(d.project_id, ''), d.title, COALESCE(d.route, ''), d.type
			FROM documents d
			WHERE (d.title LIKE ? OR EXISTS (
				SELECT 1 FROM document_versions v WHERE v.document_id = d.id AND v.version = d.current_version AND v.content LIKE ?
			))`+docFilter+`
			ORDER BY d.updated_at DESC LIMIT ?`, args...)
		if err != nil {
			return nil, fmt.Errorf("buscar documentos: %w", err)
		}
		for rows.Next() {
			var docID, projectID, title, route, docType string
			if err := rows.Scan(&docID, &projectID, &title, &route, &docType); err != nil {
				rows.Close()
				return nil, fmt.Errorf("escanear documento: %w", err)
			}
			items = append(items, map[string]any{
				"type": "document", "id": docID, "project_id": projectID,
				"title": title, "route": route, "document_type": docType,
				"snippet": domain.WrapForeign(title, "search"),
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("recorrer documentos: %w", err)
		}
		_ = rows.Close()
	}
	if allowed["message"] {
		args := []any{like, like}
		msgFilter := ""
		if strings.TrimSpace(projectRef) != "" {
			msgFilter = " AND m.project_id = ?"
			args = append(args, argsPrefix[0])
		} else if !rights.Admin {
			if len(projectIDs) > 0 {
				placeholders := make([]string, len(projectIDs))
				for i, id := range projectIDs {
					placeholders[i] = "?"
					args = append(args, id)
				}
				msgFilter = " AND (m.project_id IN (" + strings.Join(placeholders, ",") + ") OR m.destination_participant_id = ?)"
				args = append(args, actor.Participant.ID)
			} else {
				msgFilter = " AND m.destination_participant_id = ?"
				args = append(args, actor.Participant.ID)
			}
		}
		args = append(args, limit)
		rows, err := s.db.QueryContext(ctx, `
			SELECT m.id, COALESCE(m.project_id, ''), m.subject, m.body, m.type, COALESCE(m.resolution, '')
			FROM messages m
			WHERE (m.subject LIKE ? OR m.body LIKE ?)`+msgFilter+`
			ORDER BY m.created_at DESC LIMIT ?`, args...)
		if err != nil {
			return nil, fmt.Errorf("buscar mensajes: %w", err)
		}
		for rows.Next() {
			var messageID, projectID, subject, body, messageType, resolution string
			if err := rows.Scan(&messageID, &projectID, &subject, &body, &messageType, &resolution); err != nil {
				rows.Close()
				return nil, fmt.Errorf("escanear mensaje: %w", err)
			}
			items = append(items, map[string]any{
				"type": "message", "id": messageID, "project_id": projectID,
				"title": subject, "message_type": messageType, "resolution": resolution,
				"snippet": domain.WrapForeign(truncate(body, 300), "search"),
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("recorrer mensajes: %w", err)
		}
		_ = rows.Close()
	}
	sort.Slice(items, func(i, j int) bool {
		ti, _ := items[i]["type"].(string)
		tj, _ := items[j]["type"].(string)
		if ti == tj {
			return items[i]["title"].(string) < items[j]["title"].(string)
		}
		return ti < tj
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Service) documentVersionView(ctx context.Context, q queryer, doc domain.Document, version int64) (DocumentView, error) {
	var view DocumentView
	var versionID, docCreatedBy, docCreatedAt, docUpdatedAt, versionCreatedAt string
	if err := q.QueryRowContext(ctx, `
		SELECT d.id, COALESCE(d.project_id, ''), d.title, COALESCE(d.route, ''), d.type,
		       d.pinned, d.current_version, COALESCE(d.created_by, ''), d.created_at, d.updated_at,
		       v.id, v.version, v.content, v.hash, v.author_participant_id, COALESCE(p.name, ''), v.created_at
		FROM documents d
		JOIN document_versions v ON v.document_id = d.id AND v.version = ?
		LEFT JOIN participants p ON p.id = v.author_participant_id
		WHERE d.id = ?`, version, doc.ID,
	).Scan(&view.ID, &view.ProjectID, &view.Title, &view.Route, &view.Type,
		&view.Pinned, &view.CurrentVersion, &docCreatedBy, &docCreatedAt, &docUpdatedAt,
		&versionID, &view.Version, &view.Content, &view.Hash, &view.AuthorID, &view.AuthorName, &versionCreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DocumentView{}, notFound("document_version_not_found", "versión no encontrada")
		}
		return DocumentView{}, fmt.Errorf("leer versión: %w", err)
	}
	parsedCreated, err := timefmt.Parse(docCreatedAt)
	if err != nil {
		return DocumentView{}, fmt.Errorf("created_at inválido: %w", err)
	}
	parsedUpdated, err := timefmt.Parse(docUpdatedAt)
	if err != nil {
		return DocumentView{}, fmt.Errorf("updated_at inválido: %w", err)
	}
	view.CreatedAt = parsedCreated
	view.UpdatedAt = parsedUpdated
	view.ContentContent = domain.WrapForeign(view.Content, "document")
	return view, nil
}

func resolveDocument(ctx context.Context, q queryer, ref string) (domain.Document, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return domain.Document{}, notFound("document_not_found", "documento no encontrado")
	}
	doc, err := scanDocument(q.QueryRowContext(ctx, `
		SELECT id, COALESCE(project_id, ''), title, COALESCE(route, ''), type, pinned,
		       current_version, COALESCE(created_by, ''), created_at, updated_at
		FROM documents WHERE id = ?`, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Document{}, notFound("document_not_found", "documento no encontrado")
	}
	if err != nil {
		return domain.Document{}, err
	}
	return doc, nil
}

func documentByRoute(ctx context.Context, q queryer, projectID, route string) (domain.Document, error) {
	var query string
	var args []any
	if projectID == "" {
		query = `SELECT id, COALESCE(project_id, ''), title, COALESCE(route, ''), type, pinned,
		                current_version, COALESCE(created_by, ''), created_at, updated_at
		         FROM documents WHERE project_id IS NULL AND route = ?`
		args = []any{route}
	} else {
		query = `SELECT id, COALESCE(project_id, ''), title, COALESCE(route, ''), type, pinned,
		                current_version, COALESCE(created_by, ''), created_at, updated_at
		         FROM documents WHERE project_id = ? AND route = ?`
		args = []any{projectID, route}
	}
	return scanDocument(q.QueryRowContext(ctx, query, args...))
}

func documentContent(ctx context.Context, q queryer, documentID string, version int64) (string, error) {
	var content string
	if err := q.QueryRowContext(ctx,
		`SELECT content FROM document_versions WHERE document_id = ? AND version = ?`,
		documentID, version,
	).Scan(&content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", notFound("document_version_not_found", "versión no encontrada")
		}
		return "", fmt.Errorf("leer contenido: %w", err)
	}
	return content, nil
}

func scanDocument(row interface{ Scan(...any) error }) (domain.Document, error) {
	var doc domain.Document
	var pinned int
	var createdAt, updatedAt string
	if err := row.Scan(&doc.ID, &doc.ProjectID, &doc.Title, &doc.Route, &doc.Type,
		&pinned, &doc.CurrentVersion, &doc.CreatedBy, &createdAt, &updatedAt); err != nil {
		return domain.Document{}, err
	}
	doc.Pinned = pinned == 1
	parsedCreated, err := timefmt.Parse(createdAt)
	if err != nil {
		return domain.Document{}, fmt.Errorf("created_at de documento inválido: %w", err)
	}
	parsedUpdated, err := timefmt.Parse(updatedAt)
	if err != nil {
		return domain.Document{}, fmt.Errorf("updated_at de documento inválido: %w", err)
	}
	doc.CreatedAt = parsedCreated
	doc.UpdatedAt = parsedUpdated
	return doc, nil
}

func canReadDocument(rights domain.Rights, doc domain.Document) bool {
	if doc.ProjectID == "" {
		return true
	}
	return rights.CanRead(doc.ProjectID)
}

func canWriteDocument(rights domain.Rights, doc domain.Document) bool {
	if doc.ProjectID == "" {
		return rights.Admin
	}
	return rights.CanWrite(doc.ProjectID)
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func chainForProject(projectID string) string {
	if projectID == "" {
		return domain.SystemChain
	}
	return projectID
}

func lineDiff(from, to string) []DiffLine {
	fromLines := strings.Split(from, "\n")
	toLines := strings.Split(to, "\n")
	n, m := len(fromLines), len(toLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if fromLines[i] == toLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	lines := make([]DiffLine, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		if fromLines[i] == toLines[j] {
			lines = append(lines, DiffLine{Op: " ", Line: fromLines[i]})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			lines = append(lines, DiffLine{Op: "-", Line: fromLines[i]})
			i++
		} else {
			lines = append(lines, DiffLine{Op: "+", Line: toLines[j]})
			j++
		}
	}
	for ; i < n; i++ {
		lines = append(lines, DiffLine{Op: "-", Line: fromLines[i]})
	}
	for ; j < m; j++ {
		lines = append(lines, DiffLine{Op: "+", Line: toLines[j]})
	}
	return lines
}
