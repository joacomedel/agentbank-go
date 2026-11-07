package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joacomedel/agentbank-go/internal/db"
	"github.com/joacomedel/agentbank-go/internal/httpapi"
	"github.com/joacomedel/agentbank-go/internal/service"
)

const bootstrapToken = "ab_bootstrap_token_de_prueba_0123456789abcdef"

type testEnv struct {
	server     *httptest.Server
	handle     *sql.DB
	humanToken string
	humanID    string
}

func newIdentityServer(t *testing.T) *testEnv {
	t.Helper()
	handle, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	now := func() time.Time { return time.Date(2026, 11, 7, 20, 0, 0, 0, time.UTC) }
	svc := service.New(handle, now)
	if _, err := svc.Bootstrap(context.Background(), bootstrapToken); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	identity, err := svc.Authenticate(context.Background(), bootstrapToken)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := httpapi.New(handle, logger, httpapi.Options{
		MaxBodyBytes: 1 << 20,
		Version:      "test",
		Now:          now,
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &testEnv{
		server:     server,
		handle:     handle,
		humanToken: bootstrapToken,
		humanID:    identity.Participant.ID,
	}
}

func (e *testEnv) do(t *testing.T, method, path, token string, body any) (int, map[string]any, http.Header) {
	t.Helper()
	return e.doWithSession(t, method, path, token, body, "")
}

func (e *testEnv) doWithSession(t *testing.T, method, path, token string, body any, sessionID string) (int, map[string]any, http.Header) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, e.server.URL+path, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if sessionID != "" {
		request.Header.Set("X-Session-Id", sessionID)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("leer respuesta: %v", err)
	}
	var decoded map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("respuesta no JSON (%s): %s", path, raw)
		}
	}
	return response.StatusCode, decoded, response.Header
}

func (e *testEnv) createProject(t *testing.T, slug, name string) map[string]any {
	t.Helper()
	status, body, _ := e.do(t, http.MethodPost, "/v1/projects", e.humanToken, map[string]any{
		"slug": slug, "name": name,
	})
	if status != http.StatusCreated {
		t.Fatalf("POST project status=%d body=%v", status, body)
	}
	return body
}

func TestAuthRequiredAndRejected(t *testing.T) {
	env := newIdentityServer(t)

	status, body, _ := env.do(t, http.MethodGet, "/v1/me", "", nil)
	if status != http.StatusUnauthorized || body["code"] != "unauthorized" {
		t.Fatalf("sin token: status=%d body=%v", status, body)
	}
	status, body, _ = env.do(t, http.MethodGet, "/v1/me", "no-es-un-token", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("token inválido: status=%d body=%v", status, body)
	}
	var rejections int
	if err := env.handle.QueryRow(`
		SELECT COUNT(1) FROM audit_events
		WHERE action IN ('rechazo.auth.sin_token', 'rechazo.auth.token_invalido')`,
	).Scan(&rejections); err != nil {
		t.Fatalf("contar rechazos: %v", err)
	}
	if rejections < 2 {
		t.Fatalf("rechazos auditados=%d, want >=2", rejections)
	}
}

func TestMeAndSession(t *testing.T) {
	env := newIdentityServer(t)
	status, body, headers := env.do(t, http.MethodGet, "/v1/me", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /v1/me status=%d body=%v", status, body)
	}
	participant, ok := body["participant"].(map[string]any)
	if !ok || participant["type"] != "humano" || participant["admin"] != true {
		t.Fatalf("participante inesperado: %v", body)
	}
	sessionID := headers.Get("X-Session-Id")
	if sessionID == "" {
		t.Fatal("falta X-Session-Id en la respuesta")
	}
	status, body, headers2 := env.doWithSession(t, http.MethodGet, "/v1/me", env.humanToken, nil, sessionID)
	if status != http.StatusOK || headers2.Get("X-Session-Id") != sessionID {
		t.Fatalf("reutilizar sesión: status=%d headers=%v body=%v", status, headers2, body)
	}
	status, body, _ = env.doWithSession(t, http.MethodGet, "/v1/me", env.humanToken, nil, "00000000-0000-0000-0000-000000000099")
	if status != http.StatusUnauthorized || body["code"] != "invalid_session" {
		t.Fatalf("sesión ajena: status=%d body=%v", status, body)
	}
	var invalidSessions int
	if err := env.handle.QueryRow(`SELECT COUNT(1) FROM audit_events WHERE action = 'rechazo.auth.sesion_invalida'`).Scan(&invalidSessions); err != nil {
		t.Fatalf("contar sesiones inválidas: %v", err)
	}
	if invalidSessions == 0 {
		t.Fatal("no se auditó la sesión inválida")
	}
}

func TestProjectsAndMemberships(t *testing.T) {
	env := newIdentityServer(t)
	project := env.createProject(t, "proyecto-http", "Proyecto HTTP")
	projectID, _ := project["id"].(string)
	if projectID == "" {
		t.Fatalf("proyecto sin id: %v", project)
	}
	if project["prefix"] != "PROYEC" {
		t.Fatalf("prefix inesperado: %v", project)
	}

	status, body, _ := env.do(t, http.MethodGet, "/v1/projects", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET projects status=%d body=%v", status, body)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items=%v", body)
	}

	status, agentBody, _ := env.do(t, http.MethodPost, "/v1/participants", env.humanToken, map[string]any{
		"name": "agente-http",
		"type": "agente",
		"projects": []map[string]any{{
			"project_id": projectID, "permission": "lector", "roles": []string{"backend"},
		}},
	})
	if status != http.StatusCreated {
		t.Fatalf("POST participant status=%d body=%v", status, agentBody)
	}
	agentParticipant, _ := agentBody["participant"].(map[string]any)
	agentID, _ := agentParticipant["id"].(string)
	agentToken, _ := agentBody["token"].(string)
	if agentID == "" || !strings.HasPrefix(agentToken, "ab_") {
		t.Fatalf("participante/token inesperado: %v", agentBody)
	}

	status, me, _ := env.do(t, http.MethodGet, "/v1/me", agentToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /v1/me agente status=%d body=%v", status, me)
	}
	memberships, _ := me["memberships"].([]any)
	if len(memberships) != 1 {
		t.Fatalf("membresías del agente: %v", me)
	}

	status, members, _ := env.do(t, http.MethodGet, "/v1/projects/"+projectID+"/members", agentToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET members como lector status=%d body=%v", status, members)
	}

	status, body, _ = env.do(t, http.MethodPut, "/v1/projects/"+projectID+"/members/"+env.humanID, agentToken, map[string]any{
		"permission": "lector", "roles": []string{"x"},
	})
	if status != http.StatusForbidden || body["code"] != "requires_project_admin" {
		t.Fatalf("lector no debía cambiar membresía: status=%d body=%v", status, body)
	}
	var rejection int
	if err := env.handle.QueryRow(`
		SELECT COUNT(1) FROM audit_events
		WHERE action = 'rechazo.permiso' AND actor_id = ?`, agentID,
	).Scan(&rejection); err != nil {
		t.Fatalf("contar rechazo: %v", err)
	}
	if rejection == 0 {
		t.Fatal("no se auditó el rechazo de permiso")
	}

	status, body, _ = env.do(t, http.MethodPut, "/v1/projects/"+projectID+"/members/"+agentID, env.humanToken, map[string]any{
		"permission": "admin", "roles": []string{"backend", "owner"},
	})
	if status != http.StatusOK {
		t.Fatalf("PUT member admin status=%d body=%v", status, body)
	}
	if body["permission"] != "admin" {
		t.Fatalf("miembro inesperado: %v", body)
	}

	status, body, _ = env.do(t, http.MethodPut, "/v1/projects/"+projectID+"/members/"+env.humanID, agentToken, map[string]any{
		"permission": "colaborador", "roles": []string{"revisor"},
	})
	if status != http.StatusOK {
		t.Fatalf("admin del proyecto debía cambiar membresía: status=%d body=%v", status, body)
	}

	status, body, _ = env.do(t, http.MethodDelete, "/v1/projects/"+projectID+"/members/"+env.humanID, agentToken, nil)
	if status != http.StatusOK || body["removed"] != true {
		t.Fatalf("DELETE member status=%d body=%v", status, body)
	}
}

func TestTokenEndpoints(t *testing.T) {
	env := newIdentityServer(t)
	project := env.createProject(t, "tokens", "Tokens")
	projectID, _ := project["id"].(string)

	status, body, _ := env.do(t, http.MethodPost, "/v1/participants", env.humanToken, map[string]any{
		"name": "agente-token", "type": "agente",
		"projects": []map[string]any{{"project_id": projectID, "permission": "lector"}},
	})
	if status != http.StatusCreated {
		t.Fatalf("crear agente: status=%d body=%v", status, body)
	}
	participant, _ := body["participant"].(map[string]any)
	agentID, _ := participant["id"].(string)
	oldToken, _ := body["token"].(string)

	status, body, _ = env.do(t, http.MethodPost, "/v1/participants/"+agentID+"/tokens", env.humanToken, nil)
	if status != http.StatusCreated {
		t.Fatalf("rotar token: status=%d body=%v", status, body)
	}
	newToken, _ := body["token"].(string)
	newTokenID, _ := body["token_id"].(string)
	if newToken == oldToken || newTokenID == "" {
		t.Fatalf("rotación inesperada: %v", body)
	}
	if status, _, _ := env.do(t, http.MethodGet, "/v1/me", oldToken, nil); status != http.StatusUnauthorized {
		t.Fatalf("el token viejo seguía válido: status=%d", status)
	}
	if status, _, _ := env.do(t, http.MethodGet, "/v1/me", newToken, nil); status != http.StatusOK {
		t.Fatalf("el token nuevo no funciona: status=%d", status)
	}

	status, body, _ = env.do(t, http.MethodPost, "/v1/tokens/"+newTokenID+"/revoke", env.humanToken, nil)
	if status != http.StatusOK || body["revoked"] != true {
		t.Fatalf("revocar: status=%d body=%v", status, body)
	}
	if status, _, _ := env.do(t, http.MethodGet, "/v1/me", newToken, nil); status != http.StatusUnauthorized {
		t.Fatalf("el token revocado seguía válido: status=%d", status)
	}
	status, body, _ = env.do(t, http.MethodPost, "/v1/tokens/"+newTokenID+"/revoke", env.humanToken, nil)
	if status != http.StatusConflict || body["code"] != "token_already_revoked" {
		t.Fatalf("revocar dos veces: status=%d body=%v", status, body)
	}
}

func TestStrictJSONAndValidation(t *testing.T) {
	env := newIdentityServer(t)
	status, body, _ := env.do(t, http.MethodPost, "/v1/projects", env.humanToken, map[string]any{
		"slug": "x", "name": "x", "actor_id": env.humanID,
	})
	if status != http.StatusBadRequest || body["code"] != "invalid_json" {
		t.Fatalf("campo desconocido: status=%d body=%v", status, body)
	}
	status, body, _ = env.do(t, http.MethodPost, "/v1/projects", env.humanToken, map[string]any{
		"slug": "Slug Invalido", "name": "x",
	})
	if status != http.StatusUnprocessableEntity || body["code"] != "validation_error" {
		t.Fatalf("slug inválido: status=%d body=%v", status, body)
	}
	status, body, _ = env.do(t, http.MethodGet, "/v1/projects?limit=999", env.humanToken, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("limit inválido: status=%d body=%v", status, body)
	}
}

func TestAuthorComesFromToken(t *testing.T) {
	env := newIdentityServer(t)
	project := env.createProject(t, "autor", "Autor")
	projectID, _ := project["id"].(string)
	var actorID string
	if err := env.handle.QueryRow(`
		SELECT actor_id FROM audit_events
		WHERE chain_id = 'system' AND action = 'proyecto.crear' AND project_id = ?`,
		projectID,
	).Scan(&actorID); err != nil {
		t.Fatalf("leer auditoría: %v", err)
	}
	if actorID != env.humanID {
		t.Fatalf("actor=%q, want %q", actorID, env.humanID)
	}
}

func TestAuditEndpointsAndTamperDetection(t *testing.T) {
	env := newIdentityServer(t)
	project := env.createProject(t, "auditoria-http", "Auditoría HTTP")
	projectID, _ := project["id"].(string)

	status, body, _ := env.do(t, http.MethodGet, "/v1/audit/chains", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("chains: status=%d body=%v", status, body)
	}
	chains, _ := body["items"].([]any)
	if len(chains) < 2 {
		t.Fatalf("cadenas esperadas >=2: %v", body)
	}

	status, body, _ = env.do(t, http.MethodGet, "/v1/audit/verify?chain_id=system", env.humanToken, nil)
	if status != http.StatusOK || body["ok"] != true {
		t.Fatalf("verify system: status=%d body=%v", status, body)
	}

	status, body, _ = env.do(t, http.MethodGet, "/v1/audit?chain_id="+projectID, env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("audit list: status=%d body=%v", status, body)
	}
	events, _ := body["items"].([]any)
	if len(events) == 0 {
		t.Fatalf("sin eventos: %v", body)
	}

	if _, err := env.handle.Exec(`
		UPDATE audit_events SET payload = '{"tampered":true}'
		WHERE chain_id = ? AND seq = 1`, projectID); err != nil {
		t.Fatalf("alterar evento: %v", err)
	}
	status, body, _ = env.do(t, http.MethodGet, "/v1/audit/verify?chain_id="+projectID, env.humanToken, nil)
	if status != http.StatusOK || body["ok"] != false {
		t.Fatalf("verify alterado: status=%d body=%v", status, body)
	}
	first, _ := body["first_error"].(map[string]any)
	if first == nil || first["seq"] != float64(1) {
		t.Fatalf("primer error inesperado: %v", body)
	}
}

func TestAuditAccessForAgent(t *testing.T) {
	env := newIdentityServer(t)
	project := env.createProject(t, "audit-agent", "Audit agent")
	projectID, _ := project["id"].(string)
	status, body, _ := env.do(t, http.MethodPost, "/v1/participants", env.humanToken, map[string]any{
		"name": "agente-auditoria", "type": "agente",
		"projects": []map[string]any{{"project_id": projectID, "permission": "lector"}},
	})
	if status != http.StatusCreated {
		t.Fatalf("crear agente: status=%d body=%v", status, body)
	}
	participant, _ := body["participant"].(map[string]any)
	agentID, _ := participant["id"].(string)
	agentToken, _ := body["token"].(string)

	status, body, _ = env.do(t, http.MethodGet, "/v1/audit?chain_id="+projectID, agentToken, nil)
	if status != http.StatusOK {
		t.Fatalf("agente miembro debía leer su cadena: status=%d body=%v", status, body)
	}
	status, body, _ = env.do(t, http.MethodGet, "/v1/audit?chain_id=system", agentToken, nil)
	if status != http.StatusForbidden {
		t.Fatalf("agente no debía leer system: status=%d body=%v", status, body)
	}
	var rejections int
	if err := env.handle.QueryRow(`
		SELECT COUNT(1) FROM audit_events
		WHERE action = 'rechazo.permiso' AND actor_id = ?`, agentID,
	).Scan(&rejections); err != nil {
		t.Fatalf("contar rechazos: %v", err)
	}
	if rejections == 0 {
		t.Fatal("no se auditó el rechazo de auditoría")
	}
}
