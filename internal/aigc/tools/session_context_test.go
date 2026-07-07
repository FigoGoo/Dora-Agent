package tools

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/turncontext"
)

// SessionIDContextKey must match the key turncontext injects into adk session values,
// otherwise tools would silently fail to resolve the runtime session id.
func TestSessionIDContextKeyMatchesTurncontext(t *testing.T) {
	if SessionIDContextKey != turncontext.SessionIDValueKey {
		t.Fatalf("SessionIDContextKey = %q, must match turncontext.SessionIDValueKey = %q",
			SessionIDContextKey, turncontext.SessionIDValueKey)
	}
}

// Without a runtime session (plain context), the fallback resolves to empty so the
// existing "session_id is required" validation still fires when the model omits it.
func TestSessionIDFromContextEmptyWithoutSession(t *testing.T) {
	if got := sessionIDFromContext(context.Background()); got != "" {
		t.Fatalf("sessionIDFromContext(Background) = %q, want empty", got)
	}
}
