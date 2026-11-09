package service

import "errors"

var (
	// ErrInvalidToken indica token desconocido o revocado. No distingue para
	// no filtrar si un token existió.
	ErrInvalidToken = errors.New("token inválido")
	// ErrInvalidSession indica una sesión que no pertenece al participante.
	ErrInvalidSession = errors.New("sesión inválida")
)

// ValidationError representa un body semánticamente inválido (HTTP 422).
// Code permite códigos específicos (por ejemplo evidencia_incompleta).
type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// NotFoundError representa un recurso inexistente (HTTP 404).
type NotFoundError struct {
	Code    string
	Message string
}

func (e *NotFoundError) Error() string { return e.Message }

// ConflictError representa una violación de unicidad o estado (HTTP 409).
type ConflictError struct {
	Code    string
	Message string
}

func (e *ConflictError) Error() string { return e.Message }

// ForbiddenError representa un rechazo de permisos (HTTP 403). El handler la
// audita antes de responder.
type ForbiddenError struct {
	Code      string
	Message   string
	ProjectID string
	Action    string
	ObjectID  string
}

func (e *ForbiddenError) Error() string { return e.Message }

func validation(message string) error {
	return &ValidationError{Message: message}
}

func validationCode(code, message string) error {
	return &ValidationError{Code: code, Message: message}
}

func notFound(code, message string) error {
	return &NotFoundError{Code: code, Message: message}
}

func conflict(code, message string) error {
	return &ConflictError{Code: code, Message: message}
}

func forbidden(code, message, projectID, action, objectID string) error {
	return &ForbiddenError{
		Code:      code,
		Message:   message,
		ProjectID: projectID,
		Action:    action,
		ObjectID:  objectID,
	}
}
