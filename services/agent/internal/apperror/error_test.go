package apperror

import "testing"

func TestAgentErrorCategoryForCode(t *testing.T) {
	cases := map[Code]Category{
		CodeInvalidArgument:     CategoryValidation,
		CodeUnauthenticated:     CategoryAuth,
		CodePermissionDenied:    CategoryPermission,
		CodeResourceNotFound:    CategoryNotFound,
		CodeProjectArchived:     CategoryState,
		CodeIdempotencyConflict: CategoryIdempotency,
		CodeInternal:            CategoryInternal,
		CodeNotImplemented:      CategoryInternal,
	}
	for code, want := range cases {
		if got := CategoryForCode(code); got != want {
			t.Fatalf("CategoryForCode(%s)=%s want %s", code, got, want)
		}
		if got := New(code, "test").Category(); got != want {
			t.Fatalf("AgentError.Category(%s)=%s want %s", code, got, want)
		}
	}
}
