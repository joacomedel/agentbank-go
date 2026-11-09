package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/joacomedel/agentbank-go/internal/security"
	"github.com/joacomedel/agentbank-go/internal/service"
)

type Options struct {
	MaxBodyBytes int64
	Version      string
	CORSOrigins  []string
	// Now permite inyectar reloj en tests. nil usa time.Now.
	Now func() time.Time
}

type Server struct {
	db             *sql.DB
	log            *slog.Logger
	opts           Options
	svc            *service.Service
	allowedOrigins map[string]struct{}
	limiter        *security.Limiter
}

type contextKey int

const requestIDKey contextKey = 0

func New(database *sql.DB, logger *slog.Logger, opts Options) http.Handler {
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = 1 << 20
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	s := &Server{
		db:             database,
		log:            logger,
		opts:           opts,
		svc:            service.New(database, opts.Now),
		allowedOrigins: make(map[string]struct{}, len(opts.CORSOrigins)),
		limiter:        security.NewLimiter(600),
	}
	for _, origin := range opts.CORSOrigins {
		s.allowedOrigins[origin] = struct{}{}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /v1/meta", s.meta)
	mux.HandleFunc("GET /openapi.json", s.openapi)
	mux.HandleFunc("GET /v1/openapi.json", s.openapi)
	mux.HandleFunc("GET /swagger", s.swagger)
	mux.HandleFunc("GET /docs", s.swagger)

	mux.Handle("GET /v1/me", s.authenticated(s.handleMe))
	mux.Handle("GET /v1/projects", s.authenticated(s.handleListProjects))
	mux.Handle("POST /v1/projects", s.authenticated(s.handleCreateProject))
	mux.Handle("GET /v1/projects/{id}", s.authenticated(s.handleGetProject))
	mux.Handle("PATCH /v1/projects/{id}", s.authenticated(s.handleUpdateProject))
	mux.Handle("GET /v1/projects/{id}/roles", s.authenticated(s.handleListProjectRoles))
	mux.Handle("GET /v1/projects/{id}/members", s.authenticated(s.handleListMembers))
	mux.Handle("PUT /v1/projects/{id}/members/{participant_id}", s.authenticated(s.handleSetMember))
	mux.Handle("DELETE /v1/projects/{id}/members/{participant_id}", s.authenticated(s.handleRemoveMember))
	mux.Handle("GET /v1/participants", s.authenticated(s.handleListParticipants))
	mux.Handle("POST /v1/participants", s.authenticated(s.handleCreateParticipant))
	mux.Handle("PATCH /v1/participants/{id}", s.authenticated(s.handleUpdateParticipant))
	mux.Handle("POST /v1/participants/{id}/tokens", s.authenticated(s.handleRotateToken))
	mux.Handle("POST /v1/participants/{id}/tokens/revoke", s.authenticated(s.handleRevokeParticipantToken))
	mux.Handle("POST /v1/participants/{id}/baja", s.authenticated(s.handleDisableParticipant))
	mux.Handle("POST /v1/tokens/{id}/revoke", s.authenticated(s.handleRevokeToken))
	mux.Handle("GET /v1/audit/chains", s.authenticated(s.handleListChains))
	mux.Handle("GET /v1/audit", s.authenticated(s.handleListAudit))
	mux.Handle("GET /v1/audit/verify", s.authenticated(s.handleVerifyChain))

	mux.Handle("GET /v1/projects/{id}/tickets", s.authenticated(s.handleListTickets))
	mux.Handle("POST /v1/projects/{id}/tickets", s.authenticated(s.handleCreateTicket))
	mux.Handle("GET /v1/tickets/{id}", s.authenticated(s.handleGetTicket))
	mux.Handle("PATCH /v1/tickets/{id}", s.authenticated(s.handleUpdateTicket))
	mux.Handle("POST /v1/tickets/{id}/avanzar", s.authenticated(s.handleAdvanceTicket))
	mux.Handle("POST /v1/tickets/{id}/revision", s.authenticated(s.handleReviewTicket))
	mux.Handle("POST /v1/tickets/{id}/claims", s.authenticated(s.handleClaimTicket))
	mux.Handle("POST /v1/tickets/{id}/claims/release", s.authenticated(s.handleReleaseClaim))
	mux.Handle("POST /v1/tickets/{id}/comments", s.authenticated(s.handleAddComment))
	mux.Handle("GET /v1/tickets/{id}/comments", s.authenticated(s.handleListComments))
	mux.Handle("GET /v1/tickets/{id}/timeline", s.authenticated(s.handleTimeline))
	mux.Handle("POST /v1/tickets/{id}/dependencies", s.authenticated(s.handleAddDependency))
	mux.Handle("DELETE /v1/tickets/{id}/dependencies/{depends_on}", s.authenticated(s.handleRemoveDependency))
	mux.Handle("POST /v1/tickets/siguiente", s.authenticated(s.handleTicketNext))
	mux.Handle("GET /v1/tickets/listas", s.authenticated(s.handleTicketListas))

	mux.Handle("GET /v1/projects/{id}/phases", s.authenticated(s.handleListPhases))
	mux.Handle("POST /v1/projects/{id}/phases", s.authenticated(s.handleCreatePhase))
	mux.Handle("GET /v1/phases/{id}", s.authenticated(s.handleGetPhase))
	mux.Handle("PATCH /v1/phases/{id}", s.authenticated(s.handleUpdatePhase))
	mux.Handle("GET /v1/projects/{id}/epics", s.authenticated(s.handleListEpics))
	mux.Handle("POST /v1/projects/{id}/epics", s.authenticated(s.handleCreateEpic))
	mux.Handle("GET /v1/epics/{id}", s.authenticated(s.handleGetEpic))
	mux.Handle("PATCH /v1/epics/{id}", s.authenticated(s.handleUpdateEpic))

	mux.Handle("POST /v1/projects/{id}/documents", s.authenticated(s.handleCreateProjectDocument))
	mux.Handle("GET /v1/projects/{id}/documents", s.authenticated(s.handleListProjectDocuments))
	mux.Handle("POST /v1/projects/{id}/documents/sync", s.authenticated(s.handleSyncProjectDocuments))
	mux.Handle("POST /v1/documents", s.authenticated(s.handleCreateGlobalDocument))
	mux.Handle("GET /v1/documents", s.authenticated(s.handleListGlobalDocuments))
	mux.Handle("POST /v1/documents/sync", s.authenticated(s.handleSyncGlobalDocuments))
	mux.Handle("GET /v1/documents/{id}", s.authenticated(s.handleGetDocument))
	mux.Handle("POST /v1/documents/{id}/versions", s.authenticated(s.handleCreateDocumentVersion))
	mux.Handle("GET /v1/documents/{id}/versions", s.authenticated(s.handleListDocumentVersions))
	mux.Handle("GET /v1/documents/{id}/diff", s.authenticated(s.handleDiffDocument))
	mux.Handle("POST /v1/projects/{id}/decisions", s.authenticated(s.handleCreateDecision))
	mux.Handle("GET /v1/projects/{id}/decisions", s.authenticated(s.handleListDecisions))
	mux.Handle("POST /v1/tickets/{id}/documents", s.authenticated(s.handleAttachDocument))
	mux.Handle("GET /v1/tickets/{id}/documents", s.authenticated(s.handleListTicketDocuments))

	mux.Handle("POST /v1/projects/{id}/artifacts", s.authenticated(s.handlePublishArtifact))
	mux.Handle("GET /v1/projects/{id}/artifacts", s.authenticated(s.handleListProjectArtifacts))
	mux.Handle("POST /v1/projects/{id}/artifacts/sync", s.authenticated(s.handleSyncArtifacts))
	mux.Handle("GET /v1/projects/{id}/artifacts/raw/{path...}", s.authenticated(s.handleRawArtifact))
	mux.Handle("DELETE /v1/projects/{id}/artifacts/{artifact_id}", s.authenticated(s.handleDeleteArtifact))
	mux.Handle("GET /v1/artifacts", s.authenticated(s.handleListArtifacts))
	mux.Handle("GET /v1/artifacts/{id}", s.authenticated(s.handleGetArtifact))
	mux.Handle("GET /v1/search", s.authenticated(s.handleSearch))
	mux.Handle("GET /v1/projects/{id}/briefing", s.authenticated(s.handleBriefing))
	mux.Handle("GET /v1/projects/{id}/digest", s.authenticated(s.handleDigest))
	mux.Handle("GET /v1/projects/{id}/tablero", s.authenticated(s.handleTablero))

	mux.Handle("POST /v1/messages", s.authenticated(s.handleCreateMessage))
	mux.Handle("GET /v1/messages", s.authenticated(s.handleListMessages))
	mux.Handle("GET /v1/messages/{id}", s.authenticated(s.handleGetMessage))
	mux.Handle("GET /v1/me/bandeja", s.authenticated(s.handleInbox))
	mux.Handle("POST /v1/messages/{id}/estado", s.authenticated(s.handleMessageState))
	mux.Handle("POST /v1/messages/{id}/visto", s.authenticated(s.handleMessageSeen))
	mux.Handle("POST /v1/messages/{id}/acusar", s.authenticated(s.handleMessageAck))
	mux.Handle("POST /v1/messages/{id}/responder", s.authenticated(s.handleResolveMessage))
	mux.Handle("POST /v1/messages/{id}/handoff/aceptar", s.authenticated(s.handleHandoffAccept))
	mux.Handle("POST /v1/messages/{id}/handoff/rechazar", s.authenticated(s.handleHandoffReject))
	mux.Handle("POST /v1/messages/{id}/tomar", s.authenticated(s.handleTakeMessage))
	mux.Handle("POST /v1/messages/{id}/resolver", s.authenticated(s.handleResolveMessage))
	mux.Handle("POST /v1/messages/{id}/descartar", s.authenticated(s.handleDiscardMessage))
	mux.Handle("POST /v1/messages/{id}/convertir", s.authenticated(s.handleConvertMessage))

	mux.Handle("POST /v1/me/heartbeat", s.authenticated(s.handleHeartbeat))
	mux.Handle("POST /v1/admin/barridos", s.authenticated(s.handleSweeps))
	mux.Handle("POST /v1/admin/claims/sin-actividad", s.authenticated(s.handleSweeps))
	mux.Handle("GET /v1/admin/metrics", s.authenticated(s.handleMetrics))
	mux.Handle("POST /v1/admin/verify-all", s.authenticated(s.handleVerifyAll))
	mux.Handle("GET /v1/admin/outbox", s.authenticated(s.handleListOutbox))
	mux.Handle("POST /v1/admin/outbox/despachar", s.authenticated(s.handleDispatchOutbox))
	mux.Handle("POST /v1/projects/{id}/import", s.authenticated(s.handleImport))
	mux.Handle("POST /v1/projects/{id}/webhooks", s.authenticated(s.handleCreateWebhook))
	mux.Handle("GET /v1/projects/{id}/webhooks", s.authenticated(s.handleListWebhooks))
	mux.Handle("DELETE /v1/webhooks/{id}", s.authenticated(s.handleRemoveWebhook))
	mux.Handle("GET /v1/novedades", s.authenticated(s.handleNovedades))
	mux.Handle("GET /v1/changes", s.authenticated(s.handleChanges))
	mux.Handle("POST /v1/projects/{id}/pausa", s.authenticated(s.handlePauseProject))
	mux.Handle("POST /v1/projects/{id}/seguir", s.authenticated(s.handleResumeProject))

	return s.recoverer(
		s.requestID(
			s.logging(
				s.securityHeaders(
					s.cors(
						s.bodyLimit(mux),
					),
				),
			),
		),
	)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		s.log.Error("health: base no disponible", "err", err)
		s.writeError(w, http.StatusServiceUnavailable, "db_unavailable", "base no disponible")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "db": "ok"})
}

func (s *Server) meta(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{
		"name":        "agentbank-go",
		"version":     s.opts.Version,
		"api_version": "v1",
		"database":    "sqlite",
		"features": []string{
			"identity", "projects", "participants", "memberships", "tokens", "sessions",
			"tickets", "claims", "phases", "epics", "messages", "documents", "artifacts", "search",
			"import", "outbox", "webhooks", "audit", "openapi",
		},
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		s.log.Error("json encode", "err", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message string) {
	s.writeJSON(w, status, map[string]string{
		"error": message,
		"code":  code,
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.log.Error("panic", "err", recovered, "path", r.URL.Path)
				s.writeError(w, http.StatusInternalServerError, "internal_error", "error interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw [8]byte
		_, _ = rand.Read(raw[:])
		id := hex.EncodeToString(raw[:])
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		requestID, _ := r.Context().Value(requestIDKey).(string)
		args := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
			"request_id", requestID,
			"remote_addr", r.RemoteAddr,
		}
		if r.URL.Path == "/healthz" {
			s.log.Debug("http", args...)
			return
		}
		s.log.Info("http", args...)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := s.allowedOrigins[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Session-Id")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Expose-Headers", "X-Session-Id, X-Request-Id")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, s.opts.MaxBodyBytes)
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wrote {
		r.status = status
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(data)
}
