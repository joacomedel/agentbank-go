package httpapi

import (
	"net/http"

	"github.com/joacomedel/agentbank-go/internal/service"
)

func (s *Server) handleBriefing(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	briefing, err := s.svc.Briefing(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, briefing)
}

func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	digest, err := s.svc.Digest(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, digest)
}

func (s *Server) handleTablero(w http.ResponseWriter, r *http.Request, identity service.Identity) {
	board, err := s.svc.Tablero(r.Context(), identity, r.PathValue("id"))
	if err != nil {
		s.respondError(w, r, identity, err)
		return
	}
	s.writeJSON(w, http.StatusOK, board)
}
