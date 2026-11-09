package domain

import (
	"fmt"
	"time"
)

const (
	TicketOpen       = "abierto"
	TicketInProgress = "en curso"
	TicketInReview   = "en revisión"
	TicketDone       = "hecho"
	TicketBlocked    = "bloqueado"
	TicketCancelled  = "cancelado"

	PriorityLow    = "baja"
	PriorityMedium = "media"
	PriorityHigh   = "alta"
	PriorityUrgent = "urgente"

	PhaseOpen   = "abierta"
	PhaseClosed = "cerrada"

	CommentNote   = "comentario"
	CommentNota   = "nota"
	CommentClose  = "cierre"
	CommentReview = "revisión"
)

// Ticket es la unidad de trabajo. Number es correlativo por proyecto y Ref se
// compone con el prefijo del proyecto.
type Ticket struct {
	ID               string
	ProjectID        string
	Number           int64
	Key              string
	Title            string
	Description      string
	Status           string
	Priority         string
	AssigneeID       string
	AssignedRole     string
	PhaseID          string
	EpicID           string
	OrderIndex       int64
	Version          int64
	Paths            []string
	DiscoveredIn     string
	CreatedBy        string
	CreatedByRole    string
	UpdatedByRole    string
	RequiereRevision bool
	CommitSHA        string
	Reaperturas      int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Ref devuelve PREFIX-N.
func (t Ticket) Ref(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, t.Number)
}

// PriorityRank ordena urgente primero.
func PriorityRank(priority string) int {
	switch priority {
	case PriorityUrgent:
		return 0
	case PriorityHigh:
		return 1
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 3
	default:
		return 4
	}
}

// Phase agrupa trabajo por proyecto.
type Phase struct {
	ID            string
	ProjectID     string
	Key           string
	Name          string
	Description   string
	OrderIndex    int64
	Status        string
	CreatedBy     string
	CreatedByRole string
	CreatedAt     time.Time
	ClosedAt      *time.Time
}

// Epic agrupa tickets dentro de una fase.
type Epic struct {
	ID            string
	ProjectID     string
	PhaseID       string
	Key           string
	Title         string
	Description   string
	OrderIndex    int64
	Status        string
	CreatedBy     string
	CreatedByRole string
	CreatedAt     time.Time
	ClosedAt      *time.Time
}

// Claim es un lock con lease sobre un ticket.
type Claim struct {
	ID             string
	TicketID       string
	ParticipantID  string
	SessionID      string
	StatusPrevious string
	LeaseUntil     time.Time
	LastActivityAt time.Time
	CreatedByRole  string
	CreatedAt      time.Time
	ReleasedAt     *time.Time
	Reason         string
}

// Comment es una nota, comentario o cierre de ticket.
type Comment struct {
	ID         string
	TicketID   string
	AuthorID   string
	AuthorRole string
	Type       string
	Body       string
	CreatedAt  time.Time
}

// Dependency es una arista "ticket depende de depends_on".
type Dependency struct {
	TicketID    string
	DependsOnID string
	Ref         string
	Title       string
	Status      string
}
