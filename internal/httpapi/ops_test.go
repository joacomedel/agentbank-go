package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func (e *testEnv) doHeaders(t *testing.T, method, path, token string, body any, headers map[string]string) (int, map[string]any, http.Header) {
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
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var decoded map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	return response.StatusCode, decoded, response.Header
}

func TestTicketHTTPFlow(t *testing.T) {
	env := newIdentityServer(t)
	project := env.createProject(t, "tickets-http", "Tickets HTTP")
	projectID, _ := project["id"].(string)

	status, body, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/tickets", env.humanToken, map[string]any{
		"title": "Ticket HTTP", "priority": "alta", "role": "backend",
		"paths": []string{"internal/httpapi"},
	})
	if status != http.StatusCreated {
		t.Fatalf("crear ticket: status=%d body=%v", status, body)
	}
	ref, _ := body["ref"].(string)
	ticketID, _ := body["id"].(string)
	if ref == "" || ticketID == "" {
		t.Fatalf("ticket inesperado: %v", body)
	}
	status, body, _ = env.do(t, http.MethodGet, "/v1/tickets/"+ref, env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("GET ticket: status=%d body=%v", status, body)
	}
	version := body["version"].(float64)
	status, body, _ = env.do(t, http.MethodPatch, "/v1/tickets/"+ref, env.humanToken, map[string]any{
		"version": version, "status": "en revisión",
	})
	if status != http.StatusOK {
		t.Fatalf("PATCH ticket: status=%d body=%v", status, body)
	}
	status, body, _ = env.do(t, http.MethodPost, "/v1/tickets/"+ref+"/claims", env.humanToken, nil)
	if status != http.StatusConflict {
		t.Fatalf("claim de ticket en revisión debía fallar: status=%d body=%v", status, body)
	}
	status, comment, _ := env.do(t, http.MethodPost, "/v1/tickets/"+ref+"/comments", env.humanToken, map[string]any{
		"body": "Comentario de prueba", "type": "nota",
	})
	if status != http.StatusCreated {
		t.Fatalf("comentario: status=%d body=%v", status, comment)
	}
	status, timeline, _ := env.do(t, http.MethodGet, "/v1/tickets/"+ref+"/timeline", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("timeline: status=%d body=%v", status, timeline)
	}
	if items, _ := timeline["items"].([]any); len(items) == 0 {
		t.Fatalf("timeline vacía: %v", timeline)
	}

	status, agentBody, _ := env.do(t, http.MethodPost, "/v1/participants", env.humanToken, map[string]any{
		"name": "agente-tickets-http", "type": "agente",
		"projects": []map[string]any{{"project_id": projectID, "permission": "colaborador", "roles": []string{"backend"}}},
	})
	if status != http.StatusCreated {
		t.Fatalf("crear agente: status=%d body=%v", status, agentBody)
	}
	agentToken, _ := agentBody["token"].(string)
	if status, body, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/tickets", env.humanToken, map[string]any{
		"title": "Ticket para cola", "role": "backend",
	}); status != http.StatusCreated {
		t.Fatalf("crear ticket para cola: status=%d body=%v", status, body)
	}
	status, next, _ := env.do(t, http.MethodPost, "/v1/tickets/siguiente", agentToken, map[string]any{
		"project_id": projectID, "role": "backend", "claim": true,
	})
	if status != http.StatusOK {
		t.Fatalf("ticket_siguiente: status=%d body=%v", status, next)
	}
	if next["ticket"] == nil {
		t.Fatalf("sin ticket siguiente: %v", next)
	}
}

func TestPhasesEpicsMessagesDocsHTTP(t *testing.T) {
	env := newIdentityServer(t)
	project := env.createProject(t, "fases-http", "Fases HTTP")
	projectID, _ := project["id"].(string)
	status, phase, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/phases", env.humanToken, map[string]any{
		"key": "f1", "name": "Fase 1",
	})
	if status != http.StatusCreated {
		t.Fatalf("crear fase: status=%d body=%v", status, phase)
	}
	phaseID, _ := phase["id"].(string)
	status, epic, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/epics", env.humanToken, map[string]any{
		"key": "e1", "title": "Épica 1", "phase_id": phaseID,
	})
	if status != http.StatusCreated {
		t.Fatalf("crear épica: status=%d body=%v", status, epic)
	}
	epicID, _ := epic["id"].(string)
	status, ticket, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/tickets", env.humanToken, map[string]any{
		"title": "Ticket de épica", "epic_id": epicID,
	})
	if status != http.StatusCreated {
		t.Fatalf("crear ticket: status=%d body=%v", status, ticket)
	}
	ticketRef, _ := ticket["ref"].(string)
	status, board, _ := env.do(t, http.MethodGet, "/v1/projects/"+projectID+"/tablero", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("tablero: status=%d body=%v", status, board)
	}
	status, briefing, _ := env.do(t, http.MethodGet, "/v1/projects/"+projectID+"/briefing", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("briefing: status=%d body=%v", status, briefing)
	}

	status, message, _ := env.do(t, http.MethodPost, "/v1/messages", env.humanToken, map[string]any{
		"project_id": projectID, "type": "solicitud", "destination_type": "proyecto",
		"subject": "Pregunta HTTP", "body": "¿Seguimos?", "options": []string{"sí", "no"},
		"default_option": 0, "affects": []string{ticketRef},
	})
	if status != http.StatusCreated {
		t.Fatalf("crear mensaje: status=%d body=%v", status, message)
	}
	messageID, _ := message["id"].(string)
	status, inbox, _ := env.do(t, http.MethodGet, "/v1/me/bandeja?pendientes=1", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("bandeja: status=%d body=%v", status, inbox)
	}
	status, resolved, _ := env.do(t, http.MethodPost, "/v1/messages/"+messageID+"/resolver", env.humanToken, map[string]any{
		"option": 1,
	})
	if status != http.StatusOK {
		t.Fatalf("resolver mensaje: status=%d body=%v", status, resolved)
	}

	status, doc, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/documents", env.humanToken, map[string]any{
		"title": "Doc HTTP", "route": "docs/http.md", "content": "# Doc\n\nContenido inicial",
	})
	if status != http.StatusCreated {
		t.Fatalf("crear documento: status=%d body=%v", status, doc)
	}
	docID, _ := doc["id"].(string)
	status, _, _ = env.do(t, http.MethodPost, "/v1/documents/"+docID+"/versions", env.humanToken, map[string]any{
		"content": "# Doc\n\nContenido actualizado",
	})
	if status != http.StatusCreated {
		t.Fatalf("versionar documento: status=%d", status)
	}
	status, diff, _ := env.do(t, http.MethodGet, "/v1/documents/"+docID+"/diff?from=1&to=2", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("diff: status=%d body=%v", status, diff)
	}
	status, search, _ := env.do(t, http.MethodGet, "/v1/search?q=Contenido", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("search: status=%d body=%v", status, search)
	}
}

func TestIdempotencyAndAdminHTTP(t *testing.T) {
	env := newIdentityServer(t)
	headers := map[string]string{"Idempotency-Key": "crear-proyecto-1"}
	status, first, _ := env.doHeaders(t, http.MethodPost, "/v1/projects", env.humanToken, map[string]any{
		"slug": "idempotente", "name": "Idempotente",
	}, headers)
	if status != http.StatusCreated {
		t.Fatalf("primer POST: status=%d body=%v", status, first)
	}
	status, second, _ := env.doHeaders(t, http.MethodPost, "/v1/projects", env.humanToken, map[string]any{
		"slug": "idempotente", "name": "Idempotente",
	}, headers)
	if status != http.StatusCreated || second["id"] != first["id"] {
		t.Fatalf("replay idempotente: status=%d first=%v second=%v", status, first, second)
	}
	status, conflict, _ := env.doHeaders(t, http.MethodPost, "/v1/projects", env.humanToken, map[string]any{
		"slug": "otro", "name": "Otro",
	}, headers)
	if status != http.StatusConflict || conflict["code"] != "idempotency_conflict" {
		t.Fatalf("conflicto idempotente: status=%d body=%v", status, conflict)
	}
	status, metrics, _ := env.do(t, http.MethodGet, "/v1/admin/metrics", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("metrics: status=%d body=%v", status, metrics)
	}
	status, verify, _ := env.do(t, http.MethodPost, "/v1/admin/verify-all", env.humanToken, nil)
	if status != http.StatusOK || verify["ok"] != true {
		t.Fatalf("verify-all: status=%d body=%v", status, verify)
	}
	status, sweeps, _ := env.do(t, http.MethodPost, "/v1/admin/barridos", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("barridos: status=%d body=%v", status, sweeps)
	}
	status, novedades, _ := env.do(t, http.MethodGet, "/v1/novedades", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("novedades: status=%d body=%v", status, novedades)
	}
}
