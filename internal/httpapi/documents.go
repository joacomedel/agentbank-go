package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/joacomedel/agentbank-go/internal/service"
)

func (s *Server) handleCreateProjectDocument(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	s.createDocument(w, r, identity, r.PathValue("id"))
}

func (s *Server) handleCreateGlobalDocument(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	s.createDocument(w, r, identity, "")
}

func (s *Server) createDocument(w http.ResponseWriter, r *http.Request, identity service.Identity, projectID string) {
	var body struct {
		Title   string `json:"title"`
		Route   string `json:"route"`
		Type    string `json:"type"`
		Pinned  bool   `json:"pinned"`
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	document, err := s.svc.CreateDocument(r.Context(), identity, service.DocumentInput{
		ProjectID: projectID, Title: body.Title, Route: body.Route, Type: body.Type,
		Pinned: body.Pinned, Content: body.Content,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, document)
}

func (s *Server) handleListProjectDocuments(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	s.listDocuments(w, r, identity, r.PathValue("id"))
}

func (s *Server) handleListGlobalDocuments(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	s.listDocuments(w, r, identity, "")
}

func (s *Server) listDocuments(w http.ResponseWriter, r *http.Request, identity service.Identity, projectID string) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	documents, next, err := s.svc.ListDocuments(r.Context(), identity, projectID, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(documents, next))
}

func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var version int64
	if raw := strings.TrimSpace(r.URL.Query().Get("version")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 {
			s.writeError(w, http.StatusUnprocessableEntity, "validation_error", "version inválida")
			return
		}
		version = parsed
	}
	document, err := s.svc.GetDocument(r.Context(), identity, r.PathValue("id"), version)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, document)
}

func (s *Server) handleCreateDocumentVersion(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	document, err := s.svc.CreateDocumentVersion(r.Context(), identity, r.PathValue("id"), body.Content)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, document)
}

func (s *Server) handleListDocumentVersions(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	versions, err := s.svc.ListDocumentVersions(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(versions, ""))
}

func (s *Server) handleDiffDocument(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	from, _ := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	to, _ := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64)
	result, err := s.svc.DiffDocument(r.Context(), identity, r.PathValue("id"), from, to)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSyncProjectDocuments(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	s.syncDocuments(w, r, identity, r.PathValue("id"))
}

func (s *Server) handleSyncGlobalDocuments(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	s.syncDocuments(w, r, identity, "")
}

func (s *Server) syncDocuments(w http.ResponseWriter, r *http.Request, identity service.Identity, projectID string) {
	var body struct {
		Documents []struct {
			Route   string `json:"route"`
			Title   string `json:"title"`
			Type    string `json:"type"`
			Content string `json:"content"`
		} `json:"documents"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	inputs := make([]service.DocumentSyncInput, 0, len(body.Documents))
	for _, doc := range body.Documents {
		inputs = append(inputs, service.DocumentSyncInput{
			Route: doc.Route, Title: doc.Title, Type: doc.Type, Content: doc.Content,
		})
	}
	results, err := s.svc.SyncDocuments(r.Context(), identity, projectID, inputs)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": results})
}

func (s *Server) handleCreateDecision(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Title           string `json:"title"`
		Body            string `json:"body"`
		SourceMessageID string `json:"source_message_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	decision, err := s.svc.CreateDecision(r.Context(), identity, r.PathValue("id"), body.Title, body.Body, body.SourceMessageID)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, decision)
}

func (s *Server) handleListDecisions(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	decisions, err := s.svc.ListDecisions(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(decisions, ""))
}

func (s *Server) handleAttachDocument(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		DocumentID string `json:"document_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	if err := s.svc.AttachDocumentToTicket(r.Context(), identity, r.PathValue("id"), body.DocumentID); err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"attached": true})
}

func (s *Server) handleListTicketDocuments(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	documents, err := s.svc.ListTicketDocuments(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(documents, ""))
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	items, err := s.svc.Search(r.Context(), identity, r.URL.Query().Get("q"),
		r.URL.Query().Get("type"), r.URL.Query().Get("project_id"), limit)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(items, ""))
}
