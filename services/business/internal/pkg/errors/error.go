package errors

import (
	stderrors "errors"
	"net/http"
)

type Code string

const (
	CodeInvalidArgument       Code = "INVALID_ARGUMENT"
	CodeUnauthenticated       Code = "UNAUTHENTICATED"
	CodePermissionDenied      Code = "PERMISSION_DENIED"
	CodeCrossSpaceDenied      Code = "CROSS_SPACE_DENIED"
	CodeResourceNotFound      Code = "RESOURCE_NOT_FOUND"
	CodeProjectNotFound       Code = "PROJECT_NOT_FOUND"
	CodeProjectArchived       Code = "PROJECT_ARCHIVED"
	CodeStateConflict         Code = "STATE_CONFLICT"
	CodeIdempotencyConflict   Code = "IDEMPOTENCY_CONFLICT"
	CodeProcessing            Code = "IDEMPOTENCY_PROCESSING"
	CodeSafetyEvidenceInvalid Code = "SAFETY_EVIDENCE_INVALID"
	CodeInternal              Code = "INTERNAL_ERROR"
	CodeNotImplemented        Code = "NOT_IMPLEMENTED"
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
	case CodeInvalidArgument, CodeSafetyEvidenceInvalid:
		return http.StatusBadRequest
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodePermissionDenied:
		return http.StatusForbidden
	case CodeCrossSpaceDenied:
		return http.StatusForbidden
	case CodeResourceNotFound, CodeProjectNotFound:
		return http.StatusNotFound
	case CodeStateConflict, CodeIdempotencyConflict, CodeProcessing, CodeProjectArchived:
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
