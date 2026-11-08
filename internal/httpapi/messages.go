package httpapi

import (
	"net/http"

	"github.com/joacomedel/agentbank-go/internal/service"
)

func (s *Server) handleCreateMessage(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		ProjectID                string               `json:"project_id"`
		Type                     string               `json:"type"`
		DestinationType          string               `json:"destination_type"`
		DestinationParticipantID string               `json:"destination_participant_id"`
		DestinationRole          string               `json:"destination_role"`
		Subject                  string               `json:"subject"`
		Body                     string               `json:"body"`
		Options                  []string             `json:"options"`
		DefaultOption            *int                 `json:"default_option"`
		AssumeDefaultAt          string               `json:"assume_default_at"`
		Affects                  []string             `json:"affects"`
		Refs                     []service.MessageRef `json:"refs"`
		ReplyTo                  string               `json:"reply_to"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	message, err := s.svc.CreateMessage(r.Context(), identity, service.MessageInput{
		ProjectID: body.ProjectID, Type: body.Type, DestinationType: body.DestinationType,
		DestinationParticipantID: body.DestinationParticipantID, DestinationRole: body.DestinationRole,
		Subject: body.Subject, Body: body.Body, Options: body.Options,
		DefaultOption: body.DefaultOption, AssumeDefaultAt: body.AssumeDefaultAt,
		Affects: body.Affects, Refs: body.Refs, ReplyTo: body.ReplyTo,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, message)
}

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	messages, next, err := s.svc.ListSentMessages(r.Context(), identity, r.URL.Query().Get("project_id"), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(messages, next))
}

func (s *Server) handleMessageSeen(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	message, err := s.svc.SetMessageState(r.Context(), identity, r.PathValue("id"), "visto")
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, message)
}

func (s *Server) handleMessageAck(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	message, err := s.svc.SetMessageState(r.Context(), identity, r.PathValue("id"), "atendido")
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, message)
}

func (s *Server) handleHandoffAccept(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	message, err := s.svc.ResolveMessage(r.Context(), identity, r.PathValue("id"), nil, "aceptar")
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, message)
}

func (s *Server) handleHandoffReject(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	message, err := s.svc.ResolveMessage(r.Context(), identity, r.PathValue("id"), nil, "rechazar")
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, message)
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	message, err := s.svc.GetMessage(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, message)
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	pending := r.URL.Query().Get("pendientes") == "1"
	messages, next, err := s.svc.ListInbox(r.Context(), identity, r.URL.Query().Get("project_id"), pending, limit, r.URL.Query().Get("cursor"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(messages, next))
}

func (s *Server) handleMessageState(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		State string `json:"state"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	message, err := s.svc.SetMessageState(r.Context(), identity, r.PathValue("id"), body.State)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, message)
}

func (s *Server) handleTakeMessage(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	message, err := s.svc.TakeMessage(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, message)
}

func (s *Server) handleResolveMessage(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Option *int   `json:"option"`
		Text   string `json:"text"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	message, err := s.svc.ResolveMessage(r.Context(), identity, r.PathValue("id"), body.Option, body.Text)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, message)
}

func (s *Server) handleDiscardMessage(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	message, err := s.svc.DiscardMessage(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, message)
}

func (s *Server) handleConvertMessage(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		To    string `json:"to"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	result, err := s.svc.ConvertMessage(r.Context(), identity, r.PathValue("id"), body.To, body.Title, body.Body)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, result)
}
