package errors

import (
	stderrors "errors"
	"net/http"
)

type Code string

const (
	CodeInvalidArgument     Code = "INVALID_ARGUMENT"
	CodeUnauthenticated     Code = "UNAUTHENTICATED"
	CodePermissionDenied    Code = "PERMISSION_DENIED"
	CodeResourceNotFound    Code = "RESOURCE_NOT_FOUND"
	CodeStateConflict       Code = "STATE_CONFLICT"
	CodeIdempotencyConflict Code = "IDEMPOTENCY_CONFLICT"
	CodeInternal            Code = "INTERNAL_ERROR"
	CodeNotImplemented      Code = "NOT_IMPLEMENTED"
)

type BusinessError struct {
	Code      Code
	Message   string
	TraceID   string
	Retryable bool
	Details   map[string]string
	Cause     error
}

func New(code Code, message string) *BusinessError {
	return &BusinessError{Code: code, Message: message}
}

func NotImplemented(method string) *BusinessError {
	return &BusinessError{
		Code:    CodeNotImplemented,
		Message: method + " is not implemented in M1 infrastructure baseline",
		Details: map[string]string{"method": method, "milestone": "M1"},
	}
}

func (e *BusinessError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code) + ": " + e.Message
}

func (e *BusinessError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *BusinessError) HTTPStatus() int {
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
	case CodeResourceNotFound:
		return http.StatusNotFound
	case CodeStateConflict, CodeIdempotencyConflict:
		return http.StatusConflict
	case CodeNotImplemented:
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

func FromError(err error) *BusinessError {
	var businessErr *BusinessError
	if stderrors.As(err, &businessErr) {
		return businessErr
	}
	return &BusinessError{Code: CodeInternal, Message: "internal error", Cause: err}
}
