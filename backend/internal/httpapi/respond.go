package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"github.com/solvinx/opsflow/internal/apperr"
)

// APIError is the uniform error body; services return the same type.
type APIError = apperr.Error

var (
	errBadRequest   = apperr.BadRequest
	errValidation   = apperr.Validation
	errUnauthorized = apperr.Unauthorized
	errForbidden    = apperr.Forbidden
	errNotFound     = apperr.NotFound
	errConflict     = apperr.Conflict
	errTooMany      = apperr.TooMany
)

func errorHandler(c fiber.Ctx, err error) error {
	var apiErr *APIError
	var fErr *fiber.Error
	switch {
	case errors.As(err, &apiErr):
	case errors.As(err, &fErr):
		apiErr = &APIError{Status: fErr.Code, Code: "http_error", Message: fErr.Message}
	default:
		slog.Error("unhandled error", "err", err, "method", c.Method(), "path", c.Path())
		apiErr = &APIError{Status: fiber.StatusInternalServerError, Code: "internal", Message: "Something went wrong"}
	}
	return c.Status(apiErr.Status).JSON(fiber.Map{"error": apiErr})
}

// decodeJSON strictly decodes the request body: unknown fields and trailing data are rejected.
func decodeJSON(c fiber.Ctx, v any) error {
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errBadRequest("Invalid JSON body: " + err.Error())
	}
	if dec.More() {
		return errBadRequest("Invalid JSON body: trailing data")
	}
	return nil
}

func paramUUID(c fiber.Ctx, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.Nil, errBadRequest("Invalid " + name)
	}
	return id, nil
}
