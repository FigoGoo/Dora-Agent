package errors

import "testing"

func TestBusinessErrorCategoryForCode(t *testing.T) {
	cases := map[Code]Category{
		CodeInvalidArgument:          CategoryValidation,
		CodeUnauthenticated:          CategoryAuth,
		CodePermissionDenied:         CategoryPermission,
		CodeCrossSpaceDenied:         CategoryPermission,
		CodeResourceNotFound:         CategoryNotFound,
		CodeProjectArchived:          CategoryState,
		CodeIdempotencyConflict:      CategoryIdempotency,
		CodeProcessing:               CategoryIdempotency,
		CodeAssetSaveFailed:          CategoryDependency,
		CodeRedeemCodeTargetMismatch: CategoryPermission,
		CodeInternal:                 CategoryInternal,
	}
	for code, want := range cases {
		if got := CategoryForCode(code); got != want {
			t.Fatalf("CategoryForCode(%s)=%s want %s", code, got, want)
		}
		if got := New(code, "test").Category(); got != want {
			t.Fatalf("BusinessError.Category(%s)=%s want %s", code, got, want)
		}
	}
}
