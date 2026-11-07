package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/service"
)

type participantJSON struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	Admin     bool      `json:"admin"`
	CreatedAt time.Time `json:"created_at"`
}

func toParticipant(participant domain.Participant) participantJSON {
	return participantJSON{
		ID:        participant.ID,
		Type:      participant.Type,
		Name:      participant.Name,
		Admin:     participant.Admin,
		CreatedAt: participant.CreatedAt,
	}
}

type projectJSON struct {
	ID         string    `json:"id"`
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	Prefix     string    `json:"prefix"`
	Version    int64     `json:"version"`
	Permission string    `json:"permission,omitempty"`
	Roles      []string  `json:"roles"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func toProject(view service.ProjectView) projectJSON {
	roles := view.Roles
	if roles == nil {
		roles = []string{}
	}
	return projectJSON{
		ID:         view.Project.ID,
		Slug:       view.Project.Slug,
		Name:       view.Project.Name,
		Prefix:     view.Project.Prefix,
		Version:    view.Project.Version,
		Permission: view.Permission,
		Roles:      roles,
		CreatedAt:  view.Project.CreatedAt,
		UpdatedAt:  view.Project.UpdatedAt,
	}
}

type memberJSON struct {
	ParticipantID string    `json:"participant_id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	Admin         bool      `json:"admin"`
	Permission    string    `json:"permission"`
	Roles         []string  `json:"roles"`
	CreatedAt     time.Time `json:"created_at"`
}

func toMember(member domain.Member) memberJSON {
	return memberJSON{
		ParticipantID: member.ParticipantID,
		Name:          member.Name,
		Type:          member.Type,
		Admin:         member.Admin,
		Permission:    member.Permission,
		Roles:         member.Roles,
		CreatedAt:     member.CreatedAt,
	}
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	memberships, err := s.svc.Me(r.Context(), identity)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"participant": toParticipant(identity.Participant),
		"memberships": memberships,
		"session_id":  identity.SessionID,
	})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Slug   string `json:"slug"`
		Name   string `json:"name"`
		Prefix string `json:"prefix"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	project, err := s.svc.CreateProject(r.Context(), identity, service.ProjectInput{
		Slug: body.Slug, Name: body.Name, Prefix: body.Prefix,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, toProject(project))
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	projects, next, err := s.svc.ListProjects(r.Context(), identity, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	items := make([]projectJSON, 0, len(projects))
	for _, project := range projects {
		items = append(items, toProject(project))
	}
	s.writeJSON(w, http.StatusOK, listResponse(items, next))
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	project, err := s.svc.GetProject(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, toProject(project))
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Name    *string `json:"name"`
		Slug    *string `json:"slug"`
		Prefix  *string `json:"prefix"`
		Version *int64  `json:"version"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	project, err := s.svc.UpdateProject(r.Context(), identity, r.PathValue("id"), service.ProjectUpdate{
		Name: body.Name, Slug: body.Slug, Prefix: body.Prefix, Version: body.Version,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, toProject(project))
}

func (s *Server) handleListProjectRoles(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	roles, next, err := s.svc.ListProjectRoles(r.Context(), identity, r.PathValue("id"), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(roles, next))
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	members, next, err := s.svc.ListMembers(r.Context(), identity, r.PathValue("id"), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	items := make([]memberJSON, 0, len(members))
	for _, member := range members {
		items = append(items, toMember(member))
	}
	s.writeJSON(w, http.StatusOK, listResponse(items, next))
}

func (s *Server) handleSetMember(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Permission string   `json:"permission"`
		Roles      []string `json:"roles"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	member, err := s.svc.SetMember(r.Context(), identity, r.PathValue("id"), r.PathValue("participant_id"), body.Permission, body.Roles)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, toMember(member))
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	if err := s.svc.RemoveMember(r.Context(), identity, r.PathValue("id"), r.PathValue("participant_id")); err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"removed": true})
}

func (s *Server) handleListParticipants(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	participants, next, err := s.svc.ListParticipants(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	items := make([]participantJSON, 0, len(participants))
	for _, participant := range participants {
		items = append(items, toParticipant(participant))
	}
	s.writeJSON(w, http.StatusOK, listResponse(items, next))
}

func (s *Server) handleCreateParticipant(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Admin    bool   `json:"admin"`
		Projects []struct {
			ProjectID  string   `json:"project_id"`
			Permission string   `json:"permission"`
			Roles      []string `json:"roles"`
		} `json:"projects"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	input := service.ParticipantInput{Name: body.Name, Type: body.Type, Admin: body.Admin}
	for _, project := range body.Projects {
		input.Projects = append(input.Projects, service.ParticipantProjectInput{
			ProjectID:  project.ProjectID,
			Permission: project.Permission,
			Roles:      project.Roles,
		})
	}
	created, err := s.svc.CreateParticipant(r.Context(), identity, input)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{
		"participant": toParticipant(created.Participant),
		"token":       created.Token,
		"token_id":    created.TokenID,
	})
}

func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	token, tokenID, err := s.svc.RotateToken(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{
		"token":    token,
		"token_id": tokenID,
	})
}

func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	revokedAt, err := s.svc.RevokeToken(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"id":         r.PathValue("id"),
		"revoked":    true,
		"revoked_at": revokedAt,
	})
}

func (s *Server) handleListChains(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	chains, next, err := s.svc.ListChains(r.Context(), identity, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(chains, next))
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	query := r.URL.Query()
	events, next, err := s.svc.ListAudit(r.Context(), identity, service.AuditFilter{
		ChainID:    strings.TrimSpace(query.Get("chain_id")),
		ActorID:    strings.TrimSpace(query.Get("actor_id")),
		Action:     strings.TrimSpace(query.Get("action")),
		ObjectType: strings.TrimSpace(query.Get("object_type")),
		ObjectID:   strings.TrimSpace(query.Get("object_id")),
		ProjectID:  strings.TrimSpace(query.Get("project_id")),
		From:       strings.TrimSpace(query.Get("from")),
		To:         strings.TrimSpace(query.Get("to")),
	}, limit, query.Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(events, next))
}

func (s *Server) handleVerifyChain(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	result, err := s.svc.VerifyChain(r.Context(), identity, r.URL.Query().Get("chain_id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}
