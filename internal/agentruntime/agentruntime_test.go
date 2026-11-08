package agentruntime_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/agentruntime"
	"github.com/joacomedel/agentbank-go/internal/credentials"
)

func TestEnsureCreatesReusesAndRotatesAgentToken(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)

	created := false
	membership := false
	rotated := false
	validAgentToken := "agent-token-1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		auth := r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/v1/me":
			if auth == "Bearer agent-token-1" && validAgentToken == "agent-token-1" {
				_, _ = w.Write([]byte(`{"participant":{"id":"a1","name":"auditor-1","type":"agente","agent_role":"auditor"}}`))
				return
			}
			if auth == "Bearer agent-token-2" {
				_, _ = w.Write([]byte(`{"participant":{"id":"a1","name":"auditor-1","type":"agente","agent_role":"auditor"}}`))
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"credencial inválida","code":"unauthorized"}`))
		case r.URL.Path == "/v1/participants" && r.Method == http.MethodGet:
			if created {
				_, _ = w.Write([]byte(`{"items":[{"id":"a1","name":"auditor-1","type":"agente","agent_role":"auditor"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"items":[]}`))
			}
		case r.URL.Path == "/v1/participants" && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["agent_role"] != "auditor" {
				t.Errorf("agent_role=%v", body["agent_role"])
			}
			created = true
			_, _ = w.Write([]byte(`{"participant":{"id":"a1","name":"auditor-1","type":"agente","agent_role":"auditor"},"token":"agent-token-1","token_id":"t1"}`))
		case r.URL.Path == "/v1/participants/a1" && r.Method == http.MethodPatch:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["agent_role"] != "auditor" {
				t.Errorf("patch agent_role=%v", body["agent_role"])
			}
			_, _ = w.Write([]byte(`{"id":"a1","name":"auditor-1","type":"agente","agent_role":"auditor"}`))
		case r.URL.Path == "/v1/projects/P1/members" && r.Method == http.MethodGet:
			if membership {
				_, _ = w.Write([]byte(`{"items":[{"participant_id":"a1"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"items":[]}`))
			}
		case r.URL.Path == "/v1/projects/P1/members/a1" && r.Method == http.MethodPut:
			membership = true
			_, _ = w.Write([]byte(`{"participant_id":"a1","permission":"colaborador"}`))
		case r.URL.Path == "/v1/participants/a1/tokens" && r.Method == http.MethodPost:
			rotated = true
			_, _ = w.Write([]byte(`{"token":"agent-token-2","token_id":"t2"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	opts := agentruntime.Options{
		BaseURL: server.URL, HumanToken: "human-token", Project: "P1",
		Agent: "auditor-1", Role: "auditor",
	}
	result, err := agentruntime.Ensure(context.Background(), opts)
	if err != nil {
		t.Fatalf("Ensure inicial: %v", err)
	}
	if !result.Created || result.Token != "agent-token-1" {
		t.Fatalf("resultado inicial: %+v", result)
	}
	tokenPath := credentials.TokenPath("auditor-1")
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("token no guardado: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permisos=%o", info.Mode().Perm())
	}
	if filepath.Dir(tokenPath) == configDir {
		t.Fatalf("token en el directorio raíz: %s", tokenPath)
	}

	result, err = agentruntime.Ensure(context.Background(), opts)
	if err != nil {
		t.Fatalf("Ensure reutilización: %v", err)
	}
	if result.Token != "agent-token-1" || rotated {
		t.Fatalf("no debía rotar: %+v rotated=%v", result, rotated)
	}

	validAgentToken = "invalid"
	result, err = agentruntime.Ensure(context.Background(), opts)
	if err != nil {
		t.Fatalf("Ensure rotación: %v", err)
	}
	if !rotated || result.Token != "agent-token-2" || !result.Rotated {
		t.Fatalf("rotación inesperada: %+v rotated=%v", result, rotated)
	}
	stored := credentials.ReadToken(tokenPath)
	if stored != "agent-token-2" {
		t.Fatalf("token guardado=%q", stored)
	}
}
