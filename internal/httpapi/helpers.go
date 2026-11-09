package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/joacomedel/agentbank-go/internal/audit"
	"github.com/joacomedel/agentbank-go/internal/domain"
	"github.com/joacomedel/agentbank-go/internal/service"
)

type authedHandler func(http.ResponseWriter, *http.Request, service.Identity)

func (s *Server) authenticated(next authedHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			s.auditRejection(r, audit.Actor{Type: domain.AuditActorAnonimo}, "rechazo.auth.sin_token", "falta Authorization: Bearer", "", "")
			s.writeError(w, http.StatusUnauthorized, "unauthorized", "autenticación requerida")
			return
		}
		identity, err := s.svc.Authenticate(r.Context(), token)
		if errors.Is(err, service.ErrInvalidToken) {
			s.auditRejection(r, audit.Actor{Type: domain.AuditActorAnonimo}, "rechazo.auth.token_invalido", "token desconocido o revocado", "", "")
			s.writeError(w, http.StatusUnauthorized, "unauthorized", "credencial inválida")
			return
		}
		if err != nil {
			s.log.Error("autenticar", "err", err)
			s.writeError(w, http.StatusInternalServerError, "internal_error", "error interno")
			return
		}
		sessionID, err := s.svc.OpenSession(r.Context(), identity, r.Header.Get("X-Session-Id"), originOf(r))
		if errors.Is(err, service.ErrInvalidSession) {
			s.auditRejection(r, audit.Actor{Type: identity.Participant.Type, ID: identity.Participant.ID, Role: identity.Participant.AgentRole, SessionID: identity.SessionID, Origin: originOf(r)}, "rechazo.auth.sesion_invalida", "la sesión no pertenece al participante", "", "")
			s.writeError(w, http.StatusUnauthorized, "invalid_session", "sesión inválida")
			return
		}
		if err != nil {
			s.log.Error("abrir sesión", "err", err)
			s.writeError(w, http.StatusInternalServerError, "internal_error", "error interno")
			return
		}
		if !s.limiter.Allow(identity.Participant.ID) {
			s.auditRejection(r, audit.Actor{Type: identity.Participant.Type, ID: identity.Participant.ID, Role: identity.Participant.AgentRole, SessionID: identity.SessionID, Origin: originOf(r)}, "rechazo.rate_limit", "límite de requests alcanzado", "", "")
			w.Header().Set("Retry-After", "60")
			s.writeError(w, http.StatusTooManyRequests, "rate_limited", "demasiados requests")
			return
		}
		identity.SessionID = sessionID
		identity.Origin = originOf(r)
		w.Header().Set("X-Session-Id", sessionID)
		if r.Method == http.MethodPost || r.Method == http.MethodPatch || r.Method == http.MethodPut || r.Method == http.MethodDelete {
			if key := strings.TrimSpace(r.Header.Get("Idempotency-Key")); key != "" {
				s.withIdempotency(w, r, identity, key, next)
				return
			}
		}
		next(w, r, identity)
	})
}

func (s *Server) withIdempotency(w http.ResponseWriter, r *http.Request, identity service.Identity, key string, next authedHandler) {
	if len(key) > 200 {
		s.writeError(w, http.StatusBadRequest, "invalid_idempotency_key", "Idempotency-Key demasiado larga")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_json", "no se pudo leer el body")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	sum := sha256.Sum256(body)
	requestHash := hex.EncodeToString(sum[:])
	var statusCode int
	var responseBody, method, path, storedHash string
	err = s.db.QueryRowContext(r.Context(), `
		SELECT status_code, response_body, method, path, request_hash
		FROM idempotency_keys WHERE participant_id = ? AND key = ?`,
		identity.Participant.ID, key,
	).Scan(&statusCode, &responseBody, &method, &path, &storedHash)
	if err == nil {
		if method != r.Method || path != r.URL.RequestURI() || storedHash != requestHash {
			s.writeError(w, http.StatusConflict, "idempotency_conflict", "la Idempotency-Key ya se usó con otro request")
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Idempotency-Replayed", "true")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(responseBody))
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		s.log.Error("idempotencia: consultar", "err", err)
		s.writeError(w, http.StatusInternalServerError, "internal_error", "error interno")
		return
	}
	capture := &captureWriter{ResponseWriter: w, status: http.StatusOK, body: &bytes.Buffer{}}
	next(capture, r, identity)
	if capture.status >= 500 {
		return
	}
	if _, err := s.db.ExecContext(context.WithoutCancel(r.Context()), `
		INSERT INTO idempotency_keys(participant_id, key, method, path, request_hash, status_code, response_body, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(participant_id, key) DO NOTHING`,
		identity.Participant.ID, key, r.Method, r.URL.RequestURI(), requestHash,
		capture.status, capture.body.String(), time.Now().UTC().Format(time.RFC3339Nano),
	); err != nil {
		s.log.Error("idempotencia: guardar", "err", err)
	}
}

type captureWriter struct {
	http.ResponseWriter
	status int
	body   *bytes.Buffer
	wrote  bool
}

func (c *captureWriter) WriteHeader(status int) {
	if !c.wrote {
		c.status = status
		c.wrote = true
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *captureWriter) Write(data []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	c.body.Write(data)
	return c.ResponseWriter.Write(data)
}

func bearerToken(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], true
}

func originOf(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) auditRejection(r *http.Request, actor audit.Actor, action, reason, projectID, objectID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
	defer cancel()
	if err := s.svc.AuditRejection(ctx, actor, action, reason, originOf(r), projectID, "auth", objectID); err != nil {
		s.log.Error("auditar rechazo", "action", action, "err", err)
	}
}

func (s *Server) respondError(w http.ResponseWriter, r *http.Request, identity service.Identity, err error) {
	var forbidden *service.ForbiddenError
	if errors.As(err, &forbidden) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
		defer cancel()
		reason := forbidden.Message
		if forbidden.Action != "" {
			reason = forbidden.Action + ": " + reason
		}
		if auditErr := s.svc.AuditRejection(ctx,
			audit.Actor{Type: identity.Participant.Type, ID: identity.Participant.ID, Role: identity.Participant.AgentRole, SessionID: identity.SessionID, Origin: originOf(r)},
			"rechazo.permiso", reason, originOf(r), forbidden.ProjectID, "permiso", forbidden.ObjectID,
		); auditErr != nil {
			s.log.Error("auditar permiso rechazado", "err", auditErr)
		}
		s.writeError(w, http.StatusForbidden, forbidden.Code, forbidden.Message)
		return
	}
	var notFound *service.NotFoundError
	if errors.As(err, &notFound) {
		s.writeError(w, http.StatusNotFound, notFound.Code, notFound.Message)
		return
	}
	var conflict *service.ConflictError
	if errors.As(err, &conflict) {
		s.writeError(w, http.StatusConflict, conflict.Code, conflict.Message)
		return
	}
	var invalid *service.ValidationError
	if errors.As(err, &invalid) {
		s.writeError(w, http.StatusUnprocessableEntity, "validation_error", invalid.Message)
		return
	}
	if errors.Is(err, service.ErrInvalidToken) || errors.Is(err, service.ErrInvalidSession) {
		s.writeError(w, http.StatusUnauthorized, "unauthorized", "credencial inválida")
		return
	}
	s.log.Error("handler", "err", err, "path", r.URL.Path)
	s.writeError(w, http.StatusInternalServerError, "internal_error", "error interno")
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("body vacío")
		}
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("body contiene datos extra")
	}
	return nil
}

func parseLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &service.ValidationError{Message: "limit debe ser un número"}
	}
	if value < 1 || value > 200 {
		return 0, &service.ValidationError{Message: "limit debe estar entre 1 y 200"}
	}
	return value, nil
}

func listResponse(items any, next string) map[string]any {
	var cursor any
	if next != "" {
		cursor = next
	}
	return map[string]any{
		"items":       items,
		"next_cursor": cursor,
	}
}
