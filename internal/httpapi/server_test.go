package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/db"
	"github.com/joacomedel/agentbank-go/internal/httpapi"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	handle, err := db.Open(context.Background(), filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	if err := db.Migrate(context.Background(), handle); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := httpapi.New(handle, logger, httpapi.Options{
		MaxBodyBytes: 1024,
		Version:      "test",
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func getJSON(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return response.StatusCode, body
}

func TestHealth(t *testing.T) {
	server := newTestServer(t)
	status, body := getJSON(t, server.URL+"/healthz")
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if body["ok"] != true {
		t.Fatalf("body=%v", body)
	}
}

func TestMeta(t *testing.T) {
	server := newTestServer(t)
	status, body := getJSON(t, server.URL+"/v1/meta")
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if body["name"] != "agentbank-go" || body["version"] != "test" {
		t.Fatalf("body=%v", body)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	server := newTestServer(t)
	request, err := http.NewRequest(http.MethodPost, server.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want 405", response.StatusCode)
	}
}
