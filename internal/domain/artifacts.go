package domain

import "time"

// Artifact es un archivo de texto expuesto por un proyecto. Los artefactos
// son de instancia: cualquier participante autenticado puede leerlos, pero
// solo quien escribe en el proyecto puede publicarlos.
type Artifact struct {
	ID             string
	ProjectID      string
	Path           string
	ContentType    string
	CurrentVersion int64
	SourceCommit   string
	CreatedBy      string
	CreatedByRole  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
