package httpapi

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestOpenAPICoversRegisteredRoutes(t *testing.T) {
	raw, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatalf("leer server.go: %v", err)
	}
	pattern := regexp.MustCompile(`mux\.Handle\("([A-Z]+) ([^"]+)"`)
	matches := pattern.FindAllStringSubmatch(string(raw), -1)
	if len(matches) < 40 {
		t.Fatalf("rutas encontradas=%d, esperaba muchas más", len(matches))
	}
	doc := openAPIDocument()
	paths, _ := doc["paths"].(map[string]any)
	for _, match := range matches {
		method := strings.ToLower(match[1])
		path := match[2]
		item, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatalf("falta path %s en OpenAPI", path)
		}
		if _, ok := item[method]; !ok {
			t.Fatalf("falta método %s %s en OpenAPI", match[1], path)
		}
	}
}
