// Package domain contiene los tipos de negocio compartidos.
package domain

import "time"

const (
	ParticipantHuman  = "humano"
	ParticipantAgent  = "agente"
	PermissionReader  = "lector"
	PermissionWriter  = "colaborador"
	PermissionAdmin   = "admin"
	SystemChain       = "system"
	AuditActorHuman   = "humano"
	AuditActorAgent   = "agente"
	AuditActorAnonimo = "anonimo"
)

// Participant es un humano o agente. Admin significa admin de instancia.
type Participant struct {
	ID        string
	Type      string
	Name      string
	Status    string
	Admin     bool
	CreatedAt time.Time
}

// Project es un proyecto con slug y prefijo únicos.
type Project struct {
	ID        string
	Slug      string
	Name      string
	Prefix    string
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Membership representa el permiso y los roles de un participante en un
// proyecto. Permission es lector, colaborador o admin.
type Membership struct {
	ParticipantID string
	ProjectID     string
	Permission    string
	Roles         []string
	CreatedAt     time.Time
}

// ProjectAccess agrega los datos de proyecto y la membresía efectiva del actor.
type ProjectAccess struct {
	Project    Project
	Permission string
	Roles      []string
}

// Member es la vista de un participante dentro de un proyecto.
type Member struct {
	ParticipantID string
	Name          string
	Type          string
	Admin         bool
	Permission    string
	Roles         []string
	CreatedAt     time.Time
}

// Token es un token hasheado. Nunca contiene el texto plano.
type Token struct {
	ID            string
	ParticipantID string
	TokenHash     string
	CreatedAt     time.Time
	RevokedAt     *time.Time
}

// Session es una sesión de auditoría asociada a un participante.
type Session struct {
	ID            string
	ParticipantID string
	TokenID       string
	Origin        string
	StartedAt     time.Time
	LastSeenAt    time.Time
}

// Rights es la autorización efectiva de un participante.
type Rights struct {
	Memberships map[string]Membership
	Admin       bool
}

// NewRights construye los derechos a partir de las membresías explícitas.
func NewRights(admin bool, memberships []Membership) Rights {
	byProject := make(map[string]Membership, len(memberships))
	for _, membership := range memberships {
		byProject[membership.ProjectID] = membership
	}
	return Rights{Memberships: byProject, Admin: admin}
}

// CanRead indica si el participante puede ver el proyecto.
func (r Rights) CanRead(projectID string) bool {
	if r.Admin {
		return true
	}
	_, ok := r.Memberships[projectID]
	return ok
}

// CanWrite indica si el participante puede modificar el proyecto.
func (r Rights) CanWrite(projectID string) bool {
	if r.Admin {
		return true
	}
	permission := r.Memberships[projectID].Permission
	return permission == PermissionWriter || permission == PermissionAdmin
}

// CanAdmin indica si el participante administra el proyecto.
func (r Rights) CanAdmin(projectID string) bool {
	if r.Admin {
		return true
	}
	return r.Memberships[projectID].Permission == PermissionAdmin
}

// PermissionFor devuelve el permiso efectivo para mostrar en respuestas.
func (r Rights) PermissionFor(projectID string) (string, []string) {
	if r.Admin {
		if membership, ok := r.Memberships[projectID]; ok {
			return PermissionAdmin, membership.Roles
		}
		return PermissionAdmin, []string{}
	}
	membership, ok := r.Memberships[projectID]
	if !ok {
		return "", []string{}
	}
	return membership.Permission, membership.Roles
}

// ProjectIDs devuelve los proyectos con membresía explícita.
func (r Rights) ProjectIDs() []string {
	ids := make([]string, 0, len(r.Memberships))
	for id := range r.Memberships {
		ids = append(ids, id)
	}
	return ids
}
