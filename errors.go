package repr

import (
	"fmt"
	"net/http"
	"strings"
)

// ErrorResponse encapsulates standard JSON error responses.
type ErrorResponse struct {
	Error     string `json:"error"`
	Status    int    `json:"status"`
	RequestID string `json:"requestId,omitempty"`
}

// HTTPError is a typed error containing an HTTP status code and message.
type HTTPError struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
}

// HTTPStatus returns the HTTP status code for this error.
func (e *HTTPError) HTTPStatus() int {
	return e.Status
}

// NewHTTPError creates a new HTTPError with status and message.
func NewHTTPError(status int, message string) *HTTPError {
	return &HTTPError{Status: status, Message: message}
}

func BadRequest(message string) *HTTPError   { return NewHTTPError(http.StatusBadRequest, message) }
func Unauthorized(message string) *HTTPError { return NewHTTPError(http.StatusUnauthorized, message) }
func Forbidden(message string) *HTTPError    { return NewHTTPError(http.StatusForbidden, message) }
func NotFound(message string) *HTTPError     { return NewHTTPError(http.StatusNotFound, message) }
func Conflict(message string) *HTTPError     { return NewHTTPError(http.StatusConflict, message) }
func UnprocessableEntity(message string) *HTTPError {
	return NewHTTPError(http.StatusUnprocessableEntity, message)
}
func InternalServerError(message string) *HTTPError {
	return NewHTTPError(http.StatusInternalServerError, message)
}

// ValidationError holds structured per-field validation errors.
type ValidationError struct {
	Message string              `json:"message"`
	Errors  map[string][]string `json:"errors"`
	Status  int                 `json:"status"`
}

func (v *ValidationError) Error() string {
	parts := make([]string, 0, len(v.Errors))
	for field, msgs := range v.Errors {
		parts = append(parts, fmt.Sprintf("%s: %s", field, strings.Join(msgs, ", ")))
	}
	return fmt.Sprintf("validation failed: %s", strings.Join(parts, "; "))
}

// HTTPStatus returns the HTTP status used when this error is rendered (422 by default).
func (v *ValidationError) HTTPStatus() int {
	if v.Status == 0 {
		return http.StatusUnprocessableEntity
	}
	return v.Status
}
