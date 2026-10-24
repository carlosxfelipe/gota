package core

import (
	"fmt"
	"net/http"
)

// ErrorResponse encapsulates standard JSON error responses.
type ErrorResponse struct {
	Error  string `json:"error"`
	Status int    `json:"status"`
}

// HTTPError is a typed error containing an HTTP status code and message.
type HTTPError struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
}

// NewHTTPError creates a new HTTPError with status and custom message.
func NewHTTPError(status int, message string) *HTTPError {
	return &HTTPError{Status: status, Message: message}
}

// BadRequest creates a 400 Bad Request error.
func BadRequest(message string) *HTTPError {
	return NewHTTPError(http.StatusBadRequest, message)
}

// Unauthorized creates a 401 Unauthorized error.
func Unauthorized(message string) *HTTPError {
	return NewHTTPError(http.StatusUnauthorized, message)
}

// Forbidden creates a 403 Forbidden error.
func Forbidden(message string) *HTTPError {
	return NewHTTPError(http.StatusForbidden, message)
}

// NotFound creates a 404 Not Found error.
func NotFound(message string) *HTTPError {
	return NewHTTPError(http.StatusNotFound, message)
}

// UnprocessableEntity creates a 422 Unprocessable Entity error.
func UnprocessableEntity(message string) *HTTPError {
	return NewHTTPError(http.StatusUnprocessableEntity, message)
}

// InternalServerError creates a 500 Internal Server Error.
func InternalServerError(message string) *HTTPError {
	return NewHTTPError(http.StatusInternalServerError, message)
}
