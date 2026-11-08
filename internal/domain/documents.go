package domain

import "time"

const (
	DocumentTypeDoc      = "doc"
	DocumentTypeGlossary = "glosario"
)

// Document es un documento versionado. ProjectID vacío significa global.
type Document struct {
	ID             string
	ProjectID      string
	Title          string
	Route          string
	Type           string
	Pinned         bool
	CurrentVersion int64
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
