package domain

import "time"

const (
	MessageInfo        = "info"
	MessageRequest     = "solicitud"
	DestinationUser    = "participante"
	DestinationRole    = "rol"
	DestinationProject = "proyecto"
)

// Message es un mensaje info o solicitud.
type Message struct {
	ID                       string
	ProjectID                string
	FromParticipantID        string
	FromRole                 string
	Type                     string
	DestinationType          string
	DestinationParticipantID string
	DestinationRole          string
	Subject                  string
	Body                     string
	Options                  []string
	DefaultOption            *int
	AssumeDefaultAt          *time.Time
	Resolution               string
	ResolvedBy               string
	ResolvedByRole           string
	ResolvedAt               *time.Time
	ResponseOption           *int
	ResponseText             string
	TakenBy                  string
	TakenByRole              string
	TakenAt                  *time.Time
	ReplyTo                  string
	CorrelationID            string
	CreatedAt                time.Time
}
