// Package credentials resuelve y guarda tokens locales de AgentBank.
package credentials

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigDir devuelve ~/.config/agentbank.
func ConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".config", "agentbank")
	}
	return filepath.Join(dir, "agentbank")
}

// HumanTokenPath es el token humano (solo administración/enrolamiento).
func HumanTokenPath() string {
	return filepath.Join(ConfigDir(), "human.token")
}

// TokenPath devuelve el archivo de token de un agente.
func TokenPath(agent string) string {
	name := safeName(agent)
	return filepath.Join(ConfigDir(), "tokens", name+".token")
}

// ReadToken lee y limpia un token. Un archivo ausente devuelve "".
func ReadToken(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// WriteToken guarda un token con permisos 0600 de forma atómica.
func WriteToken(path, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token vacío")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("crear directorio de tokens: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return fmt.Errorf("crear token temporal: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("permisos de token: %w", err)
	}
	if _, err := tmp.WriteString(token + "\n"); err != nil {
		tmp.Close()
		return fmt.Errorf("escribir token: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cerrar token: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("publicar token: %w", err)
	}
	return nil
}

// RemoveToken borra el token local de un agente.
func RemoveToken(agent string) error {
	err := os.Remove(TokenPath(agent))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ResolveAgentName elige nombre explícito, o <host>-<rol>.
func ResolveAgentName(explicit, role string) string {
	name := strings.TrimSpace(explicit)
	if name != "" {
		return safeName(name)
	}
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "agent"
	}
	if role = strings.TrimSpace(role); role != "" {
		return safeName(host + "-" + role)
	}
	return safeName(host + "-agent")
}

func safeName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" || result == "." || result == ".." {
		return "agent"
	}
	return result
}
