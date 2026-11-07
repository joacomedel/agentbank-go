package auth_test

import (
	"strings"
	"testing"

	"github.com/joacomedel/agentbank-go/internal/auth"
)

func TestGenerateTokenFormatAndHash(t *testing.T) {
	first, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	second, err := auth.GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if first == second {
		t.Fatal("dos tokens generados son iguales")
	}
	if !auth.LooksLikeToken(first) {
		t.Fatalf("token no tiene el formato esperado: %q", first)
	}
	if strings.Contains(auth.HashToken(first), first) {
		t.Fatal("el hash contiene el token en claro")
	}
	if len(auth.HashToken(first)) != 64 {
		t.Fatalf("hash debe ser sha256 hex de 64 caracteres")
	}
	if !auth.EqualHash(auth.HashToken(first), auth.HashToken(first)) {
		t.Fatal("EqualHash debía aceptar el mismo hash")
	}
	if auth.EqualHash(auth.HashToken(first), auth.HashToken(second)) {
		t.Fatal("EqualHash aceptó hashes distintos")
	}
}

func TestLooksLikeTokenRejectsInvalid(t *testing.T) {
	cases := []string{"", "abc", "ab_", "ab_corto", "ab_" + strings.Repeat("!", 43)}
	for _, candidate := range cases {
		if auth.LooksLikeToken(candidate) {
			t.Fatalf("LooksLikeToken aceptó %q", candidate)
		}
	}
}
