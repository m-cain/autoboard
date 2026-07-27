package app

import "fmt"

type ErrorKind string

const (
	ErrorUnauthorized        ErrorKind = "unauthorized"
	ErrorNotFound            ErrorKind = "not_found"
	ErrorValidationFailed    ErrorKind = "validation_failed"
	ErrorRevisionConflict    ErrorKind = "revision_conflict"
	ErrorInvalidTransition   ErrorKind = "invalid_transition"
	ErrorBlockedByDependency ErrorKind = "blocked_by_dependency"
	ErrorDependencyCycle     ErrorKind = "dependency_cycle"
	ErrorAttachmentFailed    ErrorKind = "attachment_failed"
)

type Error struct {
	Kind           ErrorKind
	Message        string
	Fields         map[string][]string
	CurrentProject *Project
	CurrentTicket  *Ticket
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func domainError(kind ErrorKind, message string) *Error {
	return &Error{
		Kind:    kind,
		Message: message,
		Fields:  map[string][]string{},
	}
}
