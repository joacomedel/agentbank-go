package httpapi_test

import (
	"net/http"
	"testing"
)

func TestTicketReviewHTTPFlow(t *testing.T) {
	env := newIdentityServer(t)
	project := env.createProject(t, "revision-http", "Revisión HTTP")
	projectID, _ := project["id"].(string)

	status, body, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/tickets", env.humanToken, map[string]any{
		"title": "Ticket con revisión", "role": "backend",
	})
	if status != http.StatusCreated {
		t.Fatalf("crear ticket: status=%d body=%v", status, body)
	}
	ref, _ := body["ref"].(string)
	if requires, ok := body["requiere_revision"].(bool); !ok || !requires {
		t.Fatalf("requiere_revision inesperado: %v", body["requiere_revision"])
	}

	status, body, _ = env.do(t, http.MethodPost, "/v1/tickets/"+ref+"/avanzar", env.humanToken, map[string]any{
		"status": "hecho", "nota_cierre": "Solución: x\nCambios: y\nVerificación: z\nArtefactos: abc1234\nRiesgos: ninguno",
		"commit_sha": "abc1234",
	})
	if status != http.StatusConflict || body["code"] != "transicion_invalida" {
		t.Fatalf("transición inválida: status=%d body=%v", status, body)
	}

	status, body, _ = env.do(t, http.MethodPost, "/v1/tickets/"+ref+"/claims", env.humanToken, nil)
	if status != http.StatusCreated {
		t.Fatalf("claim: status=%d body=%v", status, body)
	}
	ticket, _ := body["ticket"].(map[string]any)
	version := ticket["version"].(float64)

	status, body, _ = env.do(t, http.MethodPost, "/v1/tickets/"+ref+"/avanzar", env.humanToken, map[string]any{
		"version": version, "status": "en revisión", "nota_cierre": "Solución: x",
		"commit_sha": "abc1234",
	})
	if status != http.StatusUnprocessableEntity || body["code"] != "evidencia_incompleta" {
		t.Fatalf("evidencia incompleta: status=%d body=%v", status, body)
	}

	status, body, _ = env.do(t, http.MethodPost, "/v1/tickets/"+ref+"/avanzar", env.humanToken, map[string]any{
		"version": version, "status": "en revisión",
		"nota_cierre": "Solución: x\nCambios: y\nVerificación: z\nArtefactos: abc1234\nRiesgos: ninguno",
		"commit_sha":  "abc1234",
	})
	if status != http.StatusOK {
		t.Fatalf("avanzar a revisión: status=%d body=%v", status, body)
	}
	ticket, _ = body["ticket"].(map[string]any)
	version = ticket["version"].(float64)
	if ticket["status"] != "en revisión" {
		t.Fatalf("status=%v", ticket["status"])
	}

	status, body, _ = env.do(t, http.MethodPost, "/v1/tickets/"+ref+"/avanzar", env.humanToken, map[string]any{
		"version": version, "status": "hecho",
	})
	if status != http.StatusConflict || body["code"] != "transicion_invalida" {
		t.Fatalf("hecho sin veredicto: status=%d body=%v", status, body)
	}

	status, body, _ = env.do(t, http.MethodPost, "/v1/tickets/"+ref+"/revision", env.humanToken, map[string]any{
		"veredicto": "aprobado", "comment": "verificado", "version": version,
	})
	if status != http.StatusOK {
		t.Fatalf("revisión: status=%d body=%v", status, body)
	}
	ticket, _ = body["ticket"].(map[string]any)
	if ticket["status"] != "hecho" {
		t.Fatalf("status tras revisión=%v", ticket["status"])
	}
	if ticket["commit_sha"] != "abc1234" {
		t.Fatalf("commit_sha=%v", ticket["commit_sha"])
	}

	status, body, _ = env.do(t, http.MethodPost, "/v1/tickets/siguiente", env.humanToken, map[string]any{
		"project_id": projectID, "status": "en revisión", "claim": true,
	})
	if status != http.StatusOK {
		t.Fatalf("cola de revisión: status=%d body=%v", status, body)
	}
	if candidates, ok := body["candidates"].([]any); ok && len(candidates) != 0 {
		t.Fatalf("la cola de revisión debería estar vacía: %v", candidates)
	}
}
