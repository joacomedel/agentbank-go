package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/cli"
)

func TestAPICommand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/me" {
			t.Errorf("path=%q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"participant":{"id":"p1","name":"humano"},"memberships":[]}`)
	}))
	defer server.Close()
	t.Setenv("AGENTBANK_URL", server.URL)
	t.Setenv("AGENTBANK_TOKEN", "test-token")
	t.Setenv("AGENTBANK_PROJECT", "")
	var out bytes.Buffer
	err := cli.Run(context.Background(), []string{"api", "--method", "GET", "--path", "/v1/me"}, &out, io.Discard)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), `"participant"`) {
		t.Fatalf("salida inesperada: %s", out.String())
	}
}

func TestNextAndAdvanceCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tickets/siguiente":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["claim"] != true {
				t.Errorf("claim=%v", body["claim"])
			}
			_, _ = io.WriteString(w, `{"ticket":{"id":"t1","ref":"API-1"},"claims_activos":1}`)
		case strings.HasSuffix(r.URL.Path, "/avanzar"):
			_, _ = io.WriteString(w, `{"ticket":{"id":"t1","status":"hecho"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("AGENTBANK_URL", server.URL)
	t.Setenv("AGENTBANK_TOKEN", "test-token")
	t.Setenv("AGENTBANK_PROJECT", "API")
	var out bytes.Buffer
	if err := cli.Run(context.Background(), []string{"next"}, &out, io.Discard); err != nil {
		t.Fatalf("next: %v", err)
	}
	if !strings.Contains(out.String(), `"API-1"`) {
		t.Fatalf("next salida: %s", out.String())
	}
	out.Reset()
	if err := cli.Run(context.Background(), []string{"advance", "--id", "API-1", "--status", "hecho"}, &out, io.Discard); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if !strings.Contains(out.String(), `"hecho"`) {
		t.Fatalf("advance salida: %s", out.String())
	}
}

func TestInitWritesConfigWithoutSecrets(t *testing.T) {
	dir := t.TempDir()
	configDir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	t.Setenv("XDG_CONFIG_HOME", configDir)
	var out bytes.Buffer
	if err := cli.Run(context.Background(), []string{"init", "--url", "http://127.0.0.1:9999", "--project", "API", "--agent", "orquestador-1", "--role", "orquestador", "--token", "ab_secreto_de_prueba_0123456789abcdef"}, &out, io.Discard); err != nil {
		t.Fatalf("init: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".agentbank.json"))
	if err != nil {
		t.Fatalf("leer .agentbank.json: %v", err)
	}
	if strings.Contains(string(raw), "ab_secreto") {
		t.Fatal(".agentbank.json contiene el token")
	}
	if !strings.Contains(string(raw), `"project": "API"`) || !strings.Contains(string(raw), `"agent": "orquestador-1"`) || !strings.Contains(string(raw), `"agent_role": "orquestador"`) {
		t.Fatalf(".agentbank.json inesperado: %s", raw)
	}
	tokenPath := filepath.Join(configDir, "agentbank", "human.token")
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("leer token: %v", err)
	}
	if !strings.Contains(string(token), "ab_secreto") {
		t.Fatalf("token no guardado: %s", token)
	}
}

func TestLoginStoresHumanToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer human-token" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"participant":{"id":"h1","name":"humano","type":"humano"},"memberships":[]}`)
	}))
	defer server.Close()
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("AGENTBANK_TOKEN", "")
	w := io.Discard
	if err := cli.Run(context.Background(), []string{"login", "--url", server.URL, "--token", "human-token", "--project", "API"}, w, io.Discard); err != nil {
		t.Fatalf("login: %v", err)
	}
	token, err := os.ReadFile(filepath.Join(configDir, "agentbank", "human.token"))
	if err != nil {
		t.Fatalf("leer human.token: %v", err)
	}
	if !strings.Contains(string(token), "human-token") {
		t.Fatalf("token inesperado: %s", token)
	}
}
