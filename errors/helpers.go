package errors

import (
	"errors"
	"net/http"
	"strings"
)

// HandleError is a helper function that handles DxError responses.
// It writes appropriate status codes and JSON responses.
func HandleError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}

	var dxErr DxError
	if errors.As(err, &dxErr) {
		WriteError(w, dxErr)
		return
	}

	WriteError(w, NewInternal("internal server error", err.Error()))
}

// WriteServerError is the standard handler-layer "translate or 500" helper.
// If err is a DxError it is written as-is (its status, URN and detail are the
// intended client response). Otherwise the error is treated as unexpected:
// logUnexpected (when non-nil) is invoked so the caller can log it with its own
// logger, and a generic 500 with no internal detail is written — unexpected
// errors must not leak their message to clients.
//
// This centralises the branching that was previously copy-pasted as a local
// writeErr/fail helper in every service's handler package. Callers keep a thin
// one-line wrapper that supplies the log closure, e.g.:
//
//	func (h *Handler) fail(w http.ResponseWriter, op string, err error) {
//		errors.WriteServerError(w, err, func(e error) {
//			h.logger.Error(op+" failed", zap.Error(e))
//		})
//	}
func WriteServerError(w http.ResponseWriter, err error, logUnexpected func(error)) {
	if dxErr, ok := err.(DxError); ok {
		WriteError(w, dxErr)
		return
	}
	if logUnexpected != nil {
		logUnexpected(err)
	}
	WriteError(w, NewInternal("internal error"))
}

// HandleValidationError handles validation errors from custom validators
func HandleValidationError(w http.ResponseWriter, details ...string) {
	WriteError(w, NewValidation("validation failed", details...))
}

// HandleAuthorizationError handles authorization failures
func HandleAuthorizationError(w http.ResponseWriter, resource string, operation string) {
	message := "unauthorized to " + operation + " " + resource
	WriteError(w, NewForbidden(message))
}

// HandleNotFoundError handles resource not found
func HandleNotFoundError(w http.ResponseWriter, resource string, id string) {
	message := resource + " not found"
	if id != "" {
		message += ": " + id
	}
	WriteError(w, NewNotFound(message))
}

// HandleDatabaseError handles database-specific errors
func HandleDatabaseError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}

	// Check for specific database error patterns
	errMsg := err.Error()
	if strings.Contains(errMsg, "no rows") || strings.Contains(errMsg, "not found") {
		WriteError(w, NewNotFound("requested resource not found"))
		return
	}

	if strings.Contains(errMsg, "unique constraint") {
		WriteError(w, NewConflict("resource already exists"))
		return
	}

	if strings.Contains(errMsg, "foreign key constraint") {
		WriteError(w, NewConflict("invalid reference to related resource"))
		return
	}

	// Generic database error
	WriteError(w, NewDatabase("database operation failed", err.Error()))
}

// HandleStatusCodeError converts HTTP status codes to DxError
func HandleStatusCodeError(w http.ResponseWriter, statusCode int, message string) {
	var dxErr DxError

	switch statusCode {
	case http.StatusBadRequest:
		dxErr = NewValidation(message)
	case http.StatusUnauthorized:
		dxErr = NewUnauthorized(message)
	case http.StatusForbidden:
		dxErr = NewForbidden(message)
	case http.StatusNotFound:
		dxErr = NewNotFound(message)
	case http.StatusConflict:
		dxErr = NewConflict(message)
	case http.StatusTooManyRequests:
		dxErr = NewTooManyRequests(message)
	case http.StatusInternalServerError:
		dxErr = NewInternal(message)
	case http.StatusBadGateway:
		dxErr = NewBadGateway(message)
	default:
		dxErr = NewInternal("unexpected error")
	}

	WriteError(w, dxErr)
}

// IsNotFoundError checks if an error (or any wrapped cause) is a not-found error.
func IsNotFoundError(err error) bool {
	var dxErr DxError
	if errors.As(err, &dxErr) {
		return dxErr.HTTPStatus() == http.StatusNotFound
	}
	return false
}

// IsValidationError checks if an error (or any wrapped cause) is a validation error.
func IsValidationError(err error) bool {
	var dxErr DxError
	if errors.As(err, &dxErr) {
		return dxErr.HTTPStatus() == http.StatusBadRequest
	}
	return false
}

// IsAuthorizationError checks if an error (or any wrapped cause) is an authorization error.
func IsAuthorizationError(err error) bool {
	var dxErr DxError
	if errors.As(err, &dxErr) {
		statusCode := dxErr.HTTPStatus()
		return statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden
	}
	return false
}

// ErrorDetail provides a detailed view of an error for logging
type ErrorDetail struct {
	Code       string   `json:"code"`
	Message    string   `json:"message"`
	Details    []string `json:"details,omitempty"`
	StatusCode int      `json:"status_code"`
}

// GetErrorDetail extracts error details for logging.
func GetErrorDetail(err error) ErrorDetail {
	detail := ErrorDetail{
		Code:       "INTERNAL_ERROR",
		Message:    "internal server error",
		StatusCode: http.StatusInternalServerError,
	}

	var dxErr DxError
	if errors.As(err, &dxErr) {
		detail.Code = string(dxErr.Code())
		detail.Message = dxErr.Message()
		detail.Details = dxErr.Details()
		detail.StatusCode = dxErr.HTTPStatus()
	}

	return detail
}
