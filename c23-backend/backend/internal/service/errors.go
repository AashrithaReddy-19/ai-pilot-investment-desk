package service

import (
	"errors"
	"net/http"

	"c23desk/internal/domain"
	"c23desk/internal/store"
)

// Error is a typed, client-safe error with an HTTP status and a stable code.
// Cause is logged server-side and never serialised to clients.
type Error struct {
	Status  int                 `json:"-"`
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Fields  []domain.FieldError `json:"fields,omitempty"`
	Details any                 `json:"details,omitempty"`
	Cause   error               `json:"-"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func validation(fields []domain.FieldError) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "VALIDATION_FAILED", Message: "input failed validation", Fields: fields}
}

func fieldErr(field, code, msg string) *Error {
	return validation([]domain.FieldError{{Field: field, Code: code, Message: msg}})
}

func notFound(what string) *Error {
	return &Error{Status: http.StatusNotFound, Code: "NOT_FOUND", Message: what + " not found"}
}

// fromStore maps repository errors to API errors. Unknown errors become DATABASE_UNAVAILABLE (cause kept for logs).
func fromStore(err error, what string) error {
	if err == nil {
		return nil
	}
	var se *Error
	switch {
	case errors.As(err, &se):
		return se
	case errors.Is(err, store.ErrNotFound):
		return notFound(what)
	case errors.Is(err, store.ErrConflict):
		return &Error{Status: http.StatusConflict, Code: "CONFLICT", Message: what + " already exists"}
	case errors.Is(err, store.ErrVersionConflict):
		return &Error{Status: http.StatusConflict, Code: "VERSION_CONFLICT", Message: "the " + what + " was modified by someone else; reload and retry with the current version"}
	case errors.Is(err, store.ErrInUse):
		return &Error{Status: http.StatusConflict, Code: "IN_USE", Message: what + " is referenced by another record"}
	}
	return &Error{Status: http.StatusServiceUnavailable, Code: "DATABASE_UNAVAILABLE",
		Message: "database unavailable; stateless endpoints (/api/v1/evaluate, /api/v1/optimize) still work", Cause: err}
}
