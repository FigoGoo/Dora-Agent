package apperror

import (
	"errors"
	"net/http"
)

type Code string

const (
	CodeInvalidArgument     Code = "INVALID_ARGUMENT"
	CodeUnauthenticated     Code = "UNAUTHENTICATED"
	CodePermissionDenied    Code = "PERMISSION_DENIED"
	CodeResourceNotFound    Code = "RESOURCE_NOT_FOUND"
	CodeProjectNotFound     Code = "PROJECT_NOT_FOUND"
	CodeProjectArchived     Code = "PROJECT_ARCHIVED"
	CodeStateConflict       Code = "STATE_CONFLICT"
	CodeIdempotencyConflict Code = "IDEMPOTENCY_CONFLICT"
	CodeInternal            Code = "INTERNAL_ERROR"
	CodeNotImplemented      Code = "NOT_IMPLEMENTED"
)

type AgentError struct {
	Code      Code
	Message   string
	TraceID   string
	Retryable bool
	Cause     error
}

func New(code Code, message string) *AgentError {
	return &AgentError{Code: code, Message: message}
}

func (e *AgentError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code) + ": " + e.Message
}

func (e *AgentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *AgentError) HTTPStatus() int {
	if e == nil {
		return http.StatusInternalServerError
	}
	switch e.Code {
	case CodeInvalidArgument:
		return http.StatusBadRequest
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodePermissionDenied:
		return http.StatusForbidden
	case CodeResourceNotFound, CodeProjectNotFound:
		return http.StatusNotFound
	case CodeStateConflict, CodeIdempotencyConflict, CodeProjectArchived:
		return http.StatusConflict
	case CodeNotImplemented:
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

func FromError(err error) *AgentError {
	var appErr *AgentError
	if errors.As(err, &appErr) {
		return appErr
	}
	return &AgentError{Code: CodeInternal, Message: "internal error", Cause: err}
}
