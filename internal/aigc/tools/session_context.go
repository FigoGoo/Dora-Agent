package tools

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
)

// SessionIDContextKey mirrors turncontext.SessionIDValueKey. The runtime injects the
// current session id into adk session values, so tools can resolve it from context
// instead of requiring the model to echo session_id in every tool call (DeepSeek
// frequently omits it on the first attempt, which otherwise costs one failed retry
// per tool call). Keep this in sync with turncontext.SessionIDValueKey — a test guards
// against drift.
const SessionIDContextKey = "aigc.session.id"

// sessionIDFromContext returns the runtime-injected session id, or "" when absent.
func sessionIDFromContext(ctx context.Context) string {
	value, ok := adk.GetSessionValue(ctx, SessionIDContextKey)
	if !ok || value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
