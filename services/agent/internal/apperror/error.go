package apperror

import (
	"errors"
	"net/http"
)

type Code string
type Category string

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

const (
	CategoryValidation  Category = "validation"
	CategoryAuth        Category = "auth"
	CategoryPermission  Category = "permission"
	CategoryNotFound    Category = "not_found"
	CategoryState       Category = "state"
	CategoryIdempotency Category = "idempotency"
	CategoryInternal    Category = "internal"
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

func (e *AgentError) Category() Category {
	if e == nil {
		return CategoryInternal
	}
	return CategoryForCode(e.Code)
}

func CategoryForCode(code Code) Category {
	switch code {
	case CodeInvalidArgument:
		return CategoryValidation
	case CodeUnauthenticated:
		return CategoryAuth
	case CodePermissionDenied:
		return CategoryPermission
	case CodeResourceNotFound, CodeProjectNotFound:
		return CategoryNotFound
	case CodeStateConflict, CodeProjectArchived:
		return CategoryState
	case CodeIdempotencyConflict:
		return CategoryIdempotency
	default:
		return CategoryInternal
	}
}

func FromError(err error) *AgentError {
	var appErr *AgentError
	if errors.As(err, &appErr) {
		return appErr
	}
	return &AgentError{Code: CodeInternal, Message: "internal error", Cause: err}
}
