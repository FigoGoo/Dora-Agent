package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/patch"
)

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

// decodeToolInvocationEnvelope 只接受新工具 envelope；旧版直接 payload 不再兼容。
func decodeToolInvocationEnvelope[T any](toolKey string, argumentsInJSON string, payloadReady func(T) bool) (ToolInvocationEnvelope[T], error) {
	var envelope ToolInvocationEnvelope[T]
	if err := json.Unmarshal([]byte(argumentsInJSON), &envelope); err != nil {
		return ToolInvocationEnvelope[T]{}, fmt.Errorf("decode %s input envelope: %w", toolKey, err)
	}
	if strings.TrimSpace(envelope.SessionID) == "" {
		return ToolInvocationEnvelope[T]{}, fmt.Errorf("%s session_id is required", toolKey)
	}
	if strings.TrimSpace(envelope.RequestID) == "" {
		return ToolInvocationEnvelope[T]{}, fmt.Errorf("%s request_id is required", toolKey)
	}
	if strings.TrimSpace(envelope.IdempotencyKey) == "" {
		return ToolInvocationEnvelope[T]{}, fmt.Errorf("%s idempotency_key is required", toolKey)
	}
	if strings.TrimSpace(envelope.Action) == "" {
		return ToolInvocationEnvelope[T]{}, fmt.Errorf("%s action is required", toolKey)
	}
	if payloadReady != nil && !payloadReady(envelope.Payload) {
		return ToolInvocationEnvelope[T]{}, fmt.Errorf("%s payload is required", toolKey)
	}
	return envelope, nil
}

// toolInvocationEnvelopeParams 生成统一工具入参 schema，约束 Agent 只按 envelope 调用工具。
func toolInvocationEnvelopeParams(payload map[string]*schema.ParameterInfo) map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"session_id": {
			Type:     schema.String,
			Desc:     "Current AIGC session id.",
			Required: true,
		},
		"skill_id": {
			Type: schema.String,
			Desc: "Current skill id when the call is bound to a skill.",
		},
		"stage_key": {
			Type: schema.String,
			Desc: "Current workflow stage key.",
		},
		"request_id": {
			Type:     schema.String,
			Desc:     "Stable request id for this tool call.",
			Required: true,
		},
		"idempotency_key": {
			Type:     schema.String,
			Desc:     "Stable key used to deduplicate retries and correlate UI state.",
			Required: true,
		},
		"expected_spec_version": {
			Type: schema.Integer,
			Desc: "Expected Final Video Spec version for optimistic concurrency.",
		},
		"expected_storyboard_version": {
			Type: schema.Integer,
			Desc: "Expected storyboard version for optimistic concurrency.",
		},
		"action": {
			Type:     schema.String,
			Desc:     "Business action name for this tool call.",
			Required: true,
		},
		"payload": {
			Type:      schema.Object,
			Desc:      "Business payload for the tool. Put all tool-specific fields here.",
			Required:  true,
			SubParams: payload,
		},
	}
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
