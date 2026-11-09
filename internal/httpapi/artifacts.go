package httpapi

import (
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/joacomedel/agentbank-go/internal/service"
)

func (s *Server) handlePublishArtifact(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Path         string `json:"path"`
		Content      string `json:"content"`
		ContentType  string `json:"content_type"`
		SourceCommit string `json:"source_commit"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	view, err := s.svc.PublishArtifact(r.Context(), identity, service.ArtifactInput{
		ProjectID: r.PathValue("id"), Path: body.Path, Content: body.Content,
		ContentType: body.ContentType, SourceCommit: body.SourceCommit,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	status := http.StatusOK
	if view.Action == "creado" {
		status = http.StatusCreated
	}
	s.writeJSON(w, status, view)
}

func (s *Server) handleSyncArtifacts(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Artifacts []struct {
			Path         string `json:"path"`
			Content      string `json:"content"`
			ContentType  string `json:"content_type"`
			SourceCommit string `json:"source_commit"`
		} `json:"artifacts"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	inputs := make([]service.ArtifactSyncInput, 0, len(body.Artifacts))
	for _, item := range body.Artifacts {
		inputs = append(inputs, service.ArtifactSyncInput{
			Path: item.Path, Content: item.Content,
			ContentType: item.ContentType, SourceCommit: item.SourceCommit,
		})
	}
	results, err := s.svc.SyncArtifacts(r.Context(), identity, r.PathValue("id"), inputs)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(results, ""))
}

func (s *Server) handleListProjectArtifacts(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	s.listArtifacts(w, r, identity, r.PathValue("id"))
}

func (s *Server) handleListArtifacts(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	s.listArtifacts(w, r, identity, r.URL.Query().Get("project_id"))
}

func (s *Server) listArtifacts(w http.ResponseWriter, r *http.Request, identity service.Identity, projectRef string) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	items, next, err := s.svc.ListArtifacts(r.Context(), identity, projectRef, limit,
		r.URL.Query().Get("cursor"), r.URL.Query().Get("updated_since"), r.URL.Query().Get("path"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(items, next))
}

func (s *Server) handleGetArtifact(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	version, err := parseArtifactVersion(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	artifact, err := s.svc.GetArtifactByID(r.Context(), identity, r.PathValue("id"), version)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, artifact)
}

// handleRawArtifact devuelve el contenido crudo con ETag para caching.
func (s *Server) handleRawArtifact(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	version, err := parseArtifactVersion(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	artifact, err := s.svc.GetArtifactByPath(r.Context(), identity, r.PathValue("id"), r.PathValue("path"), version)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	w.Header().Set("ETag", `"`+artifact.Hash+`"`)
	w.Header().Set("Cache-Control", "private, no-cache")
	contentType := artifact.ContentType
	if strings.HasPrefix(contentType, "text/") && !strings.Contains(contentType, "charset") {
		contentType += "; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(artifact.Path)+`"`)
	}
	if etagMatches(r.Header.Get("If-None-Match"), artifact.Hash) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, artifact.Content)
}

func (s *Server) handleDeleteArtifact(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	if err := s.svc.DeleteArtifact(r.Context(), identity, r.PathValue("id"), r.PathValue("artifact_id")); err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseArtifactVersion(r *http.Request) (int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("version"))
	if raw == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed < 1 {
		return 0, &service.ValidationError{Message: "version inválida"}
	}
	return parsed, nil
}

func etagMatches(header, hash string) bool {
	for _, candidate := range strings.Split(header, ",") {
		value := strings.TrimSpace(candidate)
		value = strings.TrimPrefix(value, "W/")
		value = strings.Trim(value, `"`)
		if value == hash || value == "*" {
			return true
		}
	}
	return false
}
