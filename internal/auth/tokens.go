// Package auth implementa tokens bearer y su verificación.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const tokenPrefix = "ab_"

// GenerateToken devuelve un token de 32 bytes aleatorios en base64url con el
// prefijo ab_. El texto plano solo se muestra al cliente una vez.
func GenerateToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generar token: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// HashToken guarda solo sha256(token) en hexadecimal.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// EqualHash compara dos hashes en tiempo constante.
func EqualHash(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// LooksLikeToken valida de forma gruesa el formato de un token bearer. No
// reemplaza la verificación contra la base.
func LooksLikeToken(token string) bool {
	if !strings.HasPrefix(token, tokenPrefix) {
		return false
	}
	raw := strings.TrimPrefix(token, tokenPrefix)
	if len(raw) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	return err == nil && len(decoded) == 32
}
