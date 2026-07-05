package tools

import "github.com/FigoGoo/Dora-Agent/internal/aigc/patch"

// JSONPatchOp is kept as a compatibility alias for existing tool callers.
type JSONPatchOp = patch.JSONPatchOp

const (
	ToolStatusOK     = "ok"
	ToolStatusError  = "error"
	ToolStatusQueued = "queued"

	ErrCodeValidation      = "validation_error"
	ErrCodeDependency      = "dependency_not_ready"
	ErrCodeVersionConflict = "version_conflict"
	ErrCodeAssetNotFound   = "asset_not_found"
	ErrCodeProviderLimit   = "provider_rate_limit"
	ErrCodeProviderFailed  = "provider_failed"
	ErrCodeTimeout         = "timeout"
	ErrCodeFatal           = "fatal"
)

// ToolInvocationEnvelope is the stable input shape all business tools should accept.
type ToolInvocationEnvelope[T any] struct {
	SessionID                 string `json:"session_id" jsonschema:"required"`
	SkillID                   string `json:"skill_id,omitempty"`
	StageKey                  string `json:"stage_key,omitempty"`
	RequestID                 string `json:"request_id" jsonschema:"required"`
	IdempotencyKey            string `json:"idempotency_key" jsonschema:"required"`
	ExpectedSpecVersion       int    `json:"expected_spec_version,omitempty"`
	ExpectedStoryboardVersion int    `json:"expected_storyboard_version,omitempty"`
	Action                    string `json:"action" jsonschema:"required"`
	Payload                   T      `json:"payload" jsonschema:"required"`
}

// ToolResultEnvelope is the stable result shape returned by business tools.
type ToolResultEnvelope[T any] struct {
	Status             string             `json:"status"`
	RequestID          string             `json:"request_id,omitempty"`
	IdempotencyKey     string             `json:"idempotency_key,omitempty"`
	SpecVersion        int                `json:"spec_version,omitempty"`
	StoryboardVersion  int                `json:"storyboard_version,omitempty"`
	ArtifactIDs        []string           `json:"artifact_ids,omitempty"`
	PatchEventIDs      []string           `json:"patch_event_ids,omitempty"`
	NextConfirmationID string             `json:"next_confirmation_id,omitempty"`
	Data               T                  `json:"data,omitempty"`
	Error              *ToolErrorEnvelope `json:"error,omitempty"`
}

type ToolErrorEnvelope struct {
	ToolKey           string `json:"tool_key"`
	Code              string `json:"code"`
	UserMessage       string `json:"user_message"`
	TechnicalMessage  string `json:"technical_message,omitempty"`
	Retryable         bool   `json:"retryable"`
	SuggestedAction   string `json:"suggested_action,omitempty"`
	Provider          string `json:"provider,omitempty"`
	ProviderRequestID string `json:"provider_request_id,omitempty"`
	TraceID           string `json:"trace_id,omitempty"`
}

func ErrorResult[T any](err ToolErrorEnvelope) ToolResultEnvelope[T] {
	return ToolResultEnvelope[T]{
		Status: ToolStatusError,
		Error:  &err,
	}
}
