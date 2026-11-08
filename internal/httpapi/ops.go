package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/joacomedel/agentbank-go/internal/service"
)

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		Fases []struct {
			Key         string `json:"key"`
			Nombre      string `json:"nombre"`
			Descripcion string `json:"descripcion"`
			Orden       *int64 `json:"orden"`
		} `json:"fases"`
		Epicas []struct {
			Key         string `json:"key"`
			Titulo      string `json:"titulo"`
			Descripcion string `json:"descripcion"`
			Fase        string `json:"fase"`
			Orden       *int64 `json:"orden"`
		} `json:"epicas"`
		Tickets []struct {
			Key         string   `json:"key"`
			Titulo      string   `json:"titulo"`
			Descripcion string   `json:"descripcion"`
			Prioridad   string   `json:"prioridad"`
			Estado      string   `json:"estado"`
			Rol         string   `json:"rol"`
			Epica       string   `json:"epica"`
			Fase        string   `json:"fase"`
			Orden       *int64   `json:"orden"`
			Paths       []string `json:"paths"`
			DependsOn   []string `json:"depende_de"`
		} `json:"tickets"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	input := service.ImportInput{
		DryRun: r.URL.Query().Get("dry_run") == "1" || r.URL.Query().Get("dry_run") == "true",
		Mode:   r.URL.Query().Get("modo"),
	}
	for _, phase := range body.Fases {
		input.Phases = append(input.Phases, service.ImportPhase{
			Key: phase.Key, Name: phase.Nombre, Description: phase.Descripcion, OrderIndex: phase.Orden,
		})
	}
	for _, epic := range body.Epicas {
		input.Epics = append(input.Epics, service.ImportEpic{
			Key: epic.Key, Title: epic.Titulo, Description: epic.Descripcion,
			Phase: epic.Fase, OrderIndex: epic.Orden,
		})
	}
	for _, ticket := range body.Tickets {
		input.Tickets = append(input.Tickets, service.ImportTicket{
			Key: ticket.Key, Title: ticket.Titulo, Description: ticket.Descripcion,
			Priority: ticket.Prioridad, Status: ticket.Estado, Role: ticket.Rol,
			Epic: ticket.Epica, Phase: ticket.Fase, OrderIndex: ticket.Orden,
			Paths: ticket.Paths, DependsOn: ticket.DependsOn,
		})
	}
	summary, err := s.svc.ImportPlan(r.Context(), identity, r.PathValue("id"), input)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	var body struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "body JSON inválido: "+err.Error())
		return
	}
	webhook, err := s.svc.CreateWebhook(r.Context(), identity, r.PathValue("id"), service.WebhookInput{
		URL: body.URL, Secret: body.Secret, Events: body.Events,
	})
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, webhook)
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	webhooks, err := s.svc.ListWebhooks(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(webhooks, ""))
}

func (s *Server) handleRemoveWebhook(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	if err := s.svc.RemoveWebhook(r.Context(), identity, r.PathValue("id")); err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"removed": true})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	metrics, err := s.svc.Metrics(r.Context(), identity)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) handleVerifyAll(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	result, err := s.svc.VerifyAll(r.Context(), identity)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListOutbox(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	limit, err := parseLimit(r)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	items, err := s.svc.ListOutbox(r.Context(), identity, r.URL.Query().Get("status"), limit)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listResponse(items, ""))
}

func (s *Server) handleDispatchOutbox(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	if !identity.Participant.Admin {
		s.respondError(w, r, identity, &service.ForbiddenError{Code: "requires_instance_admin", Message: "solo el admin de instancia despacha el outbox", Action: "admin.outbox"})
		return
	}
	delivered, err := s.svc.DispatchOutbox(r.Context())
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"entregados": delivered})
}

func (s *Server) handleNovedades(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	wait, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("wait")))
	result, err := s.svc.Novedades(r.Context(), identity, r.URL.Query().Get("since"), wait)
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}
