package httpapi_test

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

func (e *testEnv) rawRequest(t *testing.T, method, path, token string, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	request, err := http.NewRequest(method, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
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
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("leer body: %v", err)
	}
	return response.StatusCode, body, response.Header
}

func TestArtifactsPublishReadSyncAndETag(t *testing.T) {
	env := newIdentityServer(t)
	project := env.createProject(t, "artefactos", "Artefactos")
	projectID, _ := project["id"].(string)

	content := `{"openapi":"3.0.3","info":{"title":"demo"}}`
	status, created, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/artifacts", env.humanToken, map[string]any{
		"path": "openapi.json", "content": content, "source_commit": "abc1234",
	})
	if status != http.StatusCreated {
		t.Fatalf("publicar: status=%d body=%v", status, created)
	}
	if created["action"] != "creado" || created["version"].(float64) != 1 {
		t.Fatalf("publicación inesperada: %v", created)
	}
	artifactID, _ := created["id"].(string)
	hash, _ := created["hash"].(string)
	if artifactID == "" || hash == "" {
		t.Fatalf("sin id/hash: %v", created)
	}
	if created["content_type"] != "application/json" {
		t.Fatalf("content_type inferido: %v", created["content_type"])
	}

	status, same, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/artifacts", env.humanToken, map[string]any{
		"path": "openapi.json", "content": content, "source_commit": "abc1234",
	})
	if status != http.StatusOK || same["action"] != "sin_cambios" || same["version"].(float64) != 1 {
		t.Fatalf("mismo contenido: status=%d body=%v", status, same)
	}

	status, raw, headers := env.rawRequest(t, http.MethodGet, "/v1/projects/"+projectID+"/artifacts/raw/openapi.json", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("raw: status=%d body=%s", status, raw)
	}
	if string(raw) != content {
		t.Fatalf("contenido crudo distinto: %q", raw)
	}
	if got := headers.Get("ETag"); got != `"`+hash+`"` {
		t.Fatalf("ETag=%q want %q", got, `"`+hash+`"`)
	}
	if !strings.HasPrefix(headers.Get("Content-Type"), "application/json") {
		t.Fatalf("content-type=%q", headers.Get("Content-Type"))
	}
	status, body, _ := env.rawRequest(t, http.MethodGet, "/v1/projects/"+projectID+"/artifacts/raw/openapi.json", env.humanToken,
		map[string]string{"If-None-Match": `"` + hash + `"`})
	if status != http.StatusNotModified || len(body) != 0 {
		t.Fatalf("304: status=%d body=%q", status, body)
	}

	status, listed, _ := env.do(t, http.MethodGet, "/v1/projects/"+projectID+"/artifacts", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("listar: status=%d body=%v", status, listed)
	}
	items, _ := listed["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items=%v", listed)
	}
	if items[0].(map[string]any)["content"] != nil {
		t.Fatalf("el listado no debe traer contenido: %v", items[0])
	}

	status, fetched, _ := env.do(t, http.MethodGet, "/v1/artifacts/"+artifactID, env.humanToken, nil)
	if status != http.StatusOK || fetched["content"] != content {
		t.Fatalf("get por id: status=%d body=%v", status, fetched)
	}

	updatedContent := `{"openapi":"3.0.3","info":{"title":"demo v2"}}`
	status, updated, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/artifacts", env.humanToken, map[string]any{
		"path": "openapi.json", "content": updatedContent, "source_commit": "def5678",
	})
	if status != http.StatusOK || updated["action"] != "actualizado" || updated["version"].(float64) != 2 {
		t.Fatalf("actualizar: status=%d body=%v", status, updated)
	}

	status, synced, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/artifacts/sync", env.humanToken, map[string]any{
		"artifacts": []map[string]any{
			{"path": "docs/API.md", "content": "# API\n", "source_commit": "def5678"},
			{"path": "schema.sql", "content": "CREATE TABLE t (id TEXT);\n"},
			{"path": "openapi.json", "content": updatedContent},
		},
	})
	if status != http.StatusOK {
		t.Fatalf("sync: status=%d body=%v", status, synced)
	}
	syncItems, _ := synced["items"].([]any)
	if len(syncItems) != 3 {
		t.Fatalf("sync items=%v", synced)
	}
	actions := map[string]string{}
	for _, item := range syncItems {
		entry := item.(map[string]any)
		actions[entry["path"].(string)] = entry["action"].(string)
	}
	if actions["docs/API.md"] != "creado" || actions["schema.sql"] != "creado" || actions["openapi.json"] != "sin_cambios" {
		t.Fatalf("acciones de sync: %v", actions)
	}

	status, all, _ := env.do(t, http.MethodGet, "/v1/artifacts", env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("listar global: status=%d body=%v", status, all)
	}
	allItems, _ := all["items"].([]any)
	if len(allItems) != 3 {
		t.Fatalf("artefactos globales=%v", all)
	}

	status, _, _ = env.do(t, http.MethodGet, "/v1/audit?chain_id="+projectID, env.humanToken, nil)
	if status != http.StatusOK {
		t.Fatalf("auditoría: status=%d", status)
	}

	status, deleted, _ := env.do(t, http.MethodDelete, "/v1/projects/"+projectID+"/artifacts/"+artifactID, env.humanToken, nil)
	if status != http.StatusNoContent && status != http.StatusOK {
		t.Fatalf("delete: status=%d body=%v", status, deleted)
	}
	status, missing, _ := env.do(t, http.MethodGet, "/v1/artifacts/"+artifactID, env.humanToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("get tras delete: status=%d body=%v", status, missing)
	}
}

func TestArtifactsCrossProjectReadAndPermissions(t *testing.T) {
	env := newIdentityServer(t)
	projectA := env.createProject(t, "art-a", "Artefactos A")
	projectAID, _ := projectA["id"].(string)
	projectB := env.createProject(t, "art-b", "Artefactos B")
	projectBID, _ := projectB["id"].(string)

	status, _, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectAID+"/artifacts", env.humanToken, map[string]any{
		"path": "contracts/openapi.json", "content": `{"ok":true}`,
	})
	if status != http.StatusCreated {
		t.Fatalf("publicar A: status=%d", status)
	}

	status, agent, _ := env.do(t, http.MethodPost, "/v1/participants", env.humanToken, map[string]any{
		"name": "agente-art-b", "type": "agente",
		"projects": []map[string]any{{"project_id": projectBID, "permission": "lector"}},
	})
	if status != http.StatusCreated {
		t.Fatalf("crear agente: status=%d body=%v", status, agent)
	}
	agentToken, _ := agent["token"].(string)

	status, raw, _ := env.rawRequest(t, http.MethodGet, "/v1/projects/"+projectAID+"/artifacts/raw/contracts/openapi.json", agentToken, nil)
	if status != http.StatusOK || string(raw) != `{"ok":true}` {
		t.Fatalf("lectura cross-project: status=%d body=%s", status, raw)
	}

	status, body, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectAID+"/artifacts", agentToken, map[string]any{
		"path": "hack.json", "content": "{}",
	})
	if status != http.StatusForbidden {
		t.Fatalf("lector no debía publicar: status=%d body=%v", status, body)
	}

	status, body, _ = env.do(t, http.MethodPost, "/v1/projects/"+projectAID+"/artifacts", env.humanToken, map[string]any{
		"path": "../escape.json", "content": "{}",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("path inválido debía fallar: status=%d body=%v", status, body)
	}

	status, body, _ = env.do(t, http.MethodPost, "/v1/projects/"+projectAID+"/artifacts", env.humanToken, map[string]any{
		"path": "secreto.txt", "content": "token ab_0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("secreto debía rechazarse: status=%d body=%v", status, body)
	}
}

func TestArtifactRawConditionalGetDoesNotLeakBody(t *testing.T) {
	env := newIdentityServer(t)
	project := env.createProject(t, "art-etag", "Artefactos ETag")
	projectID, _ := project["id"].(string)
	status, _, _ := env.do(t, http.MethodPost, "/v1/projects/"+projectID+"/artifacts", env.humanToken, map[string]any{
		"path": "v.txt", "content": "hola", "content_type": "text/plain",
	})
	if status != http.StatusCreated {
		t.Fatalf("publicar: status=%d", status)
	}
	status, body, _ := env.rawRequest(t, http.MethodGet, "/v1/projects/"+projectID+"/artifacts/raw/v.txt", env.humanToken,
		map[string]string{"If-None-Match": "*"})
	if status != http.StatusNotModified {
		t.Fatalf("status=%d", status)
	}
	if !bytes.Equal(body, nil) {
		t.Fatalf("304 con body: %q", body)
	}
	if invalidStatus, _, _ := env.rawRequest(t, http.MethodGet, "/v1/projects/"+projectID+"/artifacts/raw/v.txt?version=9", env.humanToken, nil); invalidStatus == http.StatusOK {
		t.Fatalf("versión inexistente devolvió 200")
	}
}
