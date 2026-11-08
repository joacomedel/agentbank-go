package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/client"
	"github.com/joacomedel/agentbank-go/internal/mcp"
)

func TestMCPInitializeListAndCall(t *testing.T) {
	var calledPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledPath = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/me":
			_, _ = io.WriteString(w, `{"participant":{"id":"p1","name":"humano"},"memberships":[]}`)
		case "/v1/tickets/siguiente":
			_, _ = io.WriteString(w, `{"ticket":{"id":"t1","ref":"API-1"},"claims_activos":1}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"me","arguments":{"proyecto":"API"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"ticket_next","arguments":{"project_id":"API","role":"backend","claim":true}}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	api := client.New(server.URL, "test-token", "API")
	if err := mcp.Run(context.Background(), api, strings.NewReader(input), &out, io.Discard, "test"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("respuestas=%d: %s", len(lines), out.String())
	}
	var initialize map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &initialize); err != nil {
		t.Fatalf("initialize JSON: %v", err)
	}
	result, _ := initialize["result"].(map[string]any)
	if result["protocolVersion"] == nil {
		t.Fatalf("initialize sin protocolVersion: %v", initialize)
	}
	var list map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatalf("tools/list JSON: %v", err)
	}
	listResult, _ := list["result"].(map[string]any)
	tools, _ := listResult["tools"].([]any)
	if len(tools) < 50 {
		t.Fatalf("tools=%d, esperaba muchas más", len(tools))
	}
	if !strings.Contains(out.String(), `"ticket_next"`) || !strings.Contains(out.String(), `"changes"`) {
		t.Fatal("faltan tools clave")
	}
	if !strings.Contains(out.String(), "API-1") || !strings.Contains(out.String(), "p1") {
		t.Fatalf("respuestas de tools inesperadas: %s", out.String())
	}
	if calledPath != "/v1/tickets/siguiente" {
		t.Fatalf("último path=%q", calledPath)
	}
}
