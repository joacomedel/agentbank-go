// Package timefmt fija el formato de fechas de la API y de la auditoría.
package timefmt

import (
	"fmt"
	"strings"
	"time"
)

// Layout es RFC3339 UTC con 9 dígitos de fracción. El ancho fijo hace que el
// orden lexicográfico coincida con el orden cronológico en SQLite.
const Layout = "2006-01-02T15:04:05.000000000Z07:00"

// Format normaliza a UTC y aplica Layout.
func Format(t time.Time) string {
	return t.UTC().Format(Layout)
}

// Parse lee Layout y devuelve la hora en UTC.
func Parse(value string) (time.Time, error) {
	parsed, err := time.Parse(Layout, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

// ParseFlexible acepta las variantes RFC3339 que puede mandar un cliente y
// normaliza a UTC.
func ParseFlexible(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{Layout, time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("fecha RFC3339 inválida: %q", value)
}
