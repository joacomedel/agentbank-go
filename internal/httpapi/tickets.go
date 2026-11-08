package httpapi

import (
	"net/http"

	"github.com/joacomedel/agentbank-go/internal/service"
)

func (s *Server) handleCreateTicket(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Title        string   `json:"title"`
		Description  string   `json:"description"`
		Status       string   `json:"status"`
		Priority     string   `json:"priority"`
		AssigneeID   string   `json:"assignee_id"`
		Role         string   `json:"role"`
		PhaseID      string   `json:"phase_id"`
		EpicID       string   `json:"epic_id"`
		OrderIndex   int64    `json:"order_index"`
		Paths        []string `json:"paths"`
		DiscoveredIn string   `json:"discovered_in"`
		Key          string   `json:"key"`
		DependsOn    []string `json:"depends_on"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	ticket, err := s.svc.CreateTicket(r.Context(), identity, r.PathValue("id"), service.TicketInput{
		Title: body.Title, Description: body.Description, Status: body.Status,
		Priority: body.Priority, AssigneeID: body.AssigneeID, AssignedRole: body.Role,
		PhaseID: body.PhaseID, EpicID: body.EpicID, OrderIndex: body.OrderIndex,
		Paths: body.Paths, DiscoveredIn: body.DiscoveredIn, Key: body.Key,
		DependsOn: body.DependsOn,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, ticket)
}

func (s *Server) handleListTickets(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	query := r.URL.Query()
	tickets, next, err := s.svc.ListTickets(r.Context(), identity, r.PathValue("id"), service.TicketFilter{
		Status: query.Get("status"), Priority: query.Get("priority"),
		Role: query.Get("role"), AssigneeID: query.Get("assignee_id"),
		PhaseID: query.Get("phase_id"), EpicID: query.Get("epic_id"), Key: query.Get("key"),
		UpdatedSince: query.Get("updated_since"),
	}, limit, query.Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(tickets, next))
}

func (s *Server) handleGetTicket(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	ticket, err := s.svc.GetTicket(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, ticket)
}

func (s *Server) handleUpdateTicket(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Title        *string   `json:"title"`
		Description  *string   `json:"description"`
		Status       *string   `json:"status"`
		Priority     *string   `json:"priority"`
		AssigneeID   *string   `json:"assignee_id"`
		Role         *string   `json:"role"`
		PhaseID      *string   `json:"phase_id"`
		EpicID       *string   `json:"epic_id"`
		OrderIndex   *int64    `json:"order_index"`
		Paths        *[]string `json:"paths"`
		DiscoveredIn *string   `json:"discovered_in"`
		Version      *int64    `json:"version"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	ticket, err := s.svc.UpdateTicket(r.Context(), identity, r.PathValue("id"), service.TicketUpdate{
		Title: body.Title, Description: body.Description, Status: body.Status,
		Priority: body.Priority, AssigneeID: body.AssigneeID, AssignedRole: body.Role,
		PhaseID: body.PhaseID, EpicID: body.EpicID, OrderIndex: body.OrderIndex,
		Paths: body.Paths, DiscoveredIn: body.DiscoveredIn, Version: body.Version,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, ticket)
}

func (s *Server) handleAdvanceTicket(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Version     *int64  `json:"version"`
		Status      *string `json:"status"`
		Comment     string  `json:"comment"`
		ClosingNote string  `json:"nota_cierre"`
		YSiguiente  bool    `json:"y_siguiente"`
		Question    *struct {
			Subject                  string   `json:"asunto"`
			Body                     string   `json:"cuerpo"`
			Options                  []string `json:"opciones"`
			DefaultOption            *int     `json:"default_opcion"`
			Mode                     string   `json:"modo"`
			Motivo                   string   `json:"motivo"`
			DestinationType          string   `json:"destino_tipo"`
			DestinationRole          string   `json:"destino_rol"`
			DestinationParticipantID string   `json:"destino_participante_id"`
		} `json:"pregunta"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	input := service.AdvanceInput{
		Version: body.Version, Status: body.Status, Comment: body.Comment,
		ClosingNote: body.ClosingNote, YSiguiente: body.YSiguiente,
	}
	if body.Question != nil {
		input.Question = &service.AdvanceQuestion{
			Subject: body.Question.Subject, Body: body.Question.Body,
			Options: body.Question.Options, DefaultOption: body.Question.DefaultOption,
			Mode: body.Question.Mode, Motivo: body.Question.Motivo,
			DestinationType: body.Question.DestinationType, DestinationRole: body.Question.DestinationRole,
			DestinationParticipantID: body.Question.DestinationParticipantID,
		}
	}
	result, err := s.svc.AdvanceTicket(r.Context(), identity, r.PathValue("id"), input)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleClaimTicket(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	result, err := s.svc.ClaimTicket(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleReleaseClaim(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	ticket, err := s.svc.ReleaseClaim(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, ticket)
}

func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Body string `json:"body"`
		Type string `json:"type"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	comment, err := s.svc.AddComment(r.Context(), identity, r.PathValue("id"), body.Body, body.Type)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, comment)
}

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	comments, next, err := s.svc.ListComments(r.Context(), identity, r.PathValue("id"), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(comments, next))
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	items, err := s.svc.Timeline(r.Context(), identity, r.PathValue("id"), limit)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(items, ""))
}

func (s *Server) handleAddDependency(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		DependsOn string `json:"depends_on"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	dependency, err := s.svc.AddDependency(r.Context(), identity, r.PathValue("id"), body.DependsOn)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, dependency)
}

func (s *Server) handleRemoveDependency(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	if err := s.svc.RemoveDependency(r.Context(), identity, r.PathValue("id"), r.PathValue("depends_on")); err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"removed": true})
}

func (s *Server) handleTicketNext(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		ProjectID string `json:"project_id"`
		Role      string `json:"role"`
		PhaseID   string `json:"phase_id"`
		EpicID    string `json:"epic_id"`
		Claim     *bool  `json:"claim"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	claim := true
	if body.Claim != nil {
		claim = *body.Claim
	}
	result, err := s.svc.TicketNext(r.Context(), identity, service.TicketNextInput{
		ProjectID: body.ProjectID, Role: body.Role, PhaseID: body.PhaseID,
		EpicID: body.EpicID, Claim: claim,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleTicketListas(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	result, err := s.svc.TicketNext(r.Context(), identity, service.TicketNextInput{
		ProjectID: r.URL.Query().Get("project_id"),
		Role:      r.URL.Query().Get("role"),
		PhaseID:   r.URL.Query().Get("phase_id"),
		EpicID:    r.URL.Query().Get("epic_id"),
		Claim:     false,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(result.Candidates, ""))
}

func (s *Server) handleListPhases(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	phases, err := s.svc.ListPhases(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(phases, ""))
}

func (s *Server) handleCreatePhase(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Key         string `json:"key"`
		Name        string `json:"name"`
		Description string `json:"description"`
		OrderIndex  *int64 `json:"order_index"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	phase, err := s.svc.CreatePhase(r.Context(), identity, r.PathValue("id"), service.PhaseInput{
		Key: body.Key, Name: body.Name, Description: body.Description, OrderIndex: body.OrderIndex,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, phase)
}

func (s *Server) handleGetPhase(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	phase, err := s.svc.GetPhase(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, phase)
}

func (s *Server) handleUpdatePhase(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Key         *string `json:"key"`
		Name        *string `json:"name"`
		Description *string `json:"description"`
		OrderIndex  *int64  `json:"order_index"`
		Status      *string `json:"status"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	phase, err := s.svc.UpdatePhase(r.Context(), identity, r.PathValue("id"), service.PhaseUpdate{
		Key: body.Key, Name: body.Name, Description: body.Description,
		OrderIndex: body.OrderIndex, Status: body.Status,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, phase)
}

func (s *Server) handleListEpics(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	epics, err := s.svc.ListEpics(r.Context(), identity, r.PathValue("id"), r.URL.Query().Get("phase_id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(epics, ""))
}

func (s *Server) handleCreateEpic(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Key         string `json:"key"`
		Title       string `json:"title"`
		Description string `json:"description"`
		PhaseID     string `json:"phase_id"`
		OrderIndex  *int64 `json:"order_index"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	epic, err := s.svc.CreateEpic(r.Context(), identity, r.PathValue("id"), service.EpicInput{
		Key: body.Key, Title: body.Title, Description: body.Description,
		PhaseID: body.PhaseID, OrderIndex: body.OrderIndex,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, epic)
}

func (s *Server) handleGetEpic(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	epic, err := s.svc.GetEpic(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, epic)
}

func (s *Server) handleUpdateEpic(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Key         *string `json:"key"`
		Title       *string `json:"title"`
		Description *string `json:"description"`
		PhaseID     *string `json:"phase_id"`
		OrderIndex  *int64  `json:"order_index"`
		Status      *string `json:"status"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	epic, err := s.svc.UpdateEpic(r.Context(), identity, r.PathValue("id"), service.EpicUpdate{
		Key: body.Key, Title: body.Title, Description: body.Description,
		PhaseID: body.PhaseID, OrderIndex: body.OrderIndex, Status: body.Status,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, epic)
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	if err := s.svc.Heartbeat(r.Context(), identity); err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSweeps(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	counts, err := s.svc.SweepAll(r.Context(), identity)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	if err := s.svc.RecordAdminSweep(r.Context(), identity, counts); err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, counts)
}

func (s *Server) handlePauseProject(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	paused, err := s.svc.SetProjectPaused(r.Context(), identity, r.PathValue("id"), true)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"project_id": r.PathValue("id"), "paused": paused})
}

func (s *Server) handleResumeProject(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	paused, err := s.svc.SetProjectPaused(r.Context(), identity, r.PathValue("id"), false)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"project_id": r.PathValue("id"), "paused": paused})
}
