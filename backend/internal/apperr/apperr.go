// Package apperr defines the error type services return and the HTTP layer renders.
package apperr

import (
	"net/http"

	"github.com/solvinx/opsflow/internal/validate"
)

// Error is rendered as {"error": {"code", "message", "fields"}} with Status as the HTTP status.
type Error struct {
	Status  int             `json:"-"`
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Fields  validate.Errors `json:"fields,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func BadRequest(msg string) *Error {
	return &Error{Status: http.StatusBadRequest, Code: "bad_request", Message: msg}
}

func Validation(f validate.Errors) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "Some fields are invalid", Fields: f}
}

func Unauthorized() *Error {
	return &Error{Status: http.StatusUnauthorized, Code: "unauthenticated", Message: "Please sign in"}
}

func Forbidden() *Error {
	return &Error{Status: http.StatusForbidden, Code: "forbidden", Message: "You do not have permission to do this"}
}

func NotFound(what string) *Error {
	return &Error{Status: http.StatusNotFound, Code: "not_found", Message: what + " not found"}
}

func Conflict(msg string) *Error {
	return &Error{Status: http.StatusConflict, Code: "conflict", Message: msg}
}

// InvalidTransition is a workflow move that does not exist from the current status.
func InvalidTransition(msg string) *Error {
	return &Error{Status: http.StatusConflict, Code: "invalid_transition", Message: msg}
}

func TooMany(msg string) *Error {
	return &Error{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: msg}
}
