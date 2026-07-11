package capability

import (
	"context"
	"fmt"
	"strings"
)

// CommandContext contains trusted identity, correlation, concurrency and
// idempotency fields. It is injected by the server/middleware and is never part
// of an Agent-facing Tool schema.
type CommandContext struct {
	TenantID                  string `json:"tenant_id,omitempty"`
	UserID                    string `json:"user_id,omitempty"`
	SessionID                 string `json:"session_id"`
	RunID                     string `json:"run_id,omitempty"`
	ToolCallID                string `json:"tool_call_id,omitempty"`
	WorkflowID                string `json:"workflow_id,omitempty"`
	StageRunID                string `json:"stage_run_id,omitempty"`
	OperationID               string `json:"operation_id,omitempty"`
	RequestID                 string `json:"request_id"`
	IdempotencyKey            string `json:"idempotency_key"`
	ExpectedSpecVersion       int    `json:"expected_spec_version,omitempty"`
	ExpectedStoryboardVersion int    `json:"expected_storyboard_version,omitempty"`
	TraceID                   string `json:"trace_id,omitempty"`
}

type commandContextKey struct{}

func WithCommandContext(ctx context.Context, command CommandContext) context.Context {
	return context.WithValue(ctx, commandContextKey{}, command)
}

func CommandContextFrom(ctx context.Context) (CommandContext, bool) {
	if ctx == nil {
		return CommandContext{}, false
	}
	command, ok := ctx.Value(commandContextKey{}).(CommandContext)
	return command, ok
}

func RequireCommandContext(ctx context.Context) (CommandContext, error) {
	command, ok := CommandContextFrom(ctx)
	if !ok {
		return CommandContext{}, fmt.Errorf("trusted capability command context is required")
	}
	command.SessionID = strings.TrimSpace(command.SessionID)
	command.RequestID = strings.TrimSpace(command.RequestID)
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.SessionID == "" {
		return CommandContext{}, fmt.Errorf("trusted capability session id is required")
	}
	if command.RequestID == "" {
		return CommandContext{}, fmt.Errorf("trusted capability request id is required")
	}
	if command.IdempotencyKey == "" {
		return CommandContext{}, fmt.Errorf("trusted capability idempotency key is required")
	}
	return command, nil
}

type Request[I any] struct {
	Command CommandContext `json:"command"`
	Intent  I              `json:"intent"`
}
