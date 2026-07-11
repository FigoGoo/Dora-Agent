package generation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type workflowRequestShape struct {
	SessionID string             `json:"session_id"`
	UserID    string             `json:"user_id,omitempty"`
	Kind      string             `json:"kind"`
	Batch     workflowBatchShape `json:"batch"`
	Jobs      []workflowJobShape `json:"jobs"`
}

type workflowBatchShape struct {
	Kind                      string         `json:"kind,omitempty"`
	CompletionPolicy          string         `json:"completion_policy"`
	MinSuccess                int            `json:"min_success,omitempty"`
	WakePolicy                string         `json:"wake_policy,omitempty"`
	DeliveryPolicy            DeliveryPolicy `json:"delivery_policy"`
	ExpectedSpecVersion       int            `json:"expected_spec_version,omitempty"`
	ExpectedStoryboardVersion int            `json:"expected_storyboard_version,omitempty"`
}

type workflowJobShape struct {
	Provider                    string         `json:"provider"`
	MediaKind                   string         `json:"media_kind"`
	TargetType                  string         `json:"target_type,omitempty"`
	TargetID                    string         `json:"target_id,omitempty"`
	AssetSlot                   string         `json:"asset_slot,omitempty"`
	VariantKey                  string         `json:"variant_key,omitempty"`
	Required                    bool           `json:"required"`
	StoryboardID                string         `json:"storyboard_id,omitempty"`
	StoryboardVersionAtDispatch int            `json:"storyboard_version_at_dispatch,omitempty"`
	BindingToken                BindingToken   `json:"binding_token"`
	DeliveryPolicy              DeliveryPolicy `json:"delivery_policy"`
	MaxAttempts                 int            `json:"max_attempts,omitempty"`
	MaxProviderPollAttempts     int            `json:"max_provider_poll_attempts,omitempty"`
	Payload                     map[string]any `json:"payload,omitempty"`
}

func freezeWorkflowRequest(command *CreateWorkflowCommand) error {
	if command == nil {
		return fmt.Errorf("generation workflow command is required")
	}
	batchPolicy := command.Batch.DeliveryPolicy.Normalize()
	shape := workflowRequestShape{
		SessionID: strings.TrimSpace(command.Operation.SessionID),
		UserID:    strings.TrimSpace(command.Operation.UserID),
		Kind:      strings.TrimSpace(command.Operation.Kind),
		Batch: workflowBatchShape{
			Kind: command.Batch.Kind, CompletionPolicy: command.Batch.CompletionPolicy,
			MinSuccess: command.Batch.MinSuccess, WakePolicy: command.Batch.WakePolicy,
			DeliveryPolicy:            batchPolicy,
			ExpectedSpecVersion:       command.Batch.ExpectedSpecVersion,
			ExpectedStoryboardVersion: command.Batch.ExpectedStoryboardVersion,
		},
		Jobs: make([]workflowJobShape, 0, len(command.Jobs)),
	}
	for _, job := range command.Jobs {
		shape.Jobs = append(shape.Jobs, workflowJobShape{
			Provider: job.Provider, MediaKind: job.MediaKind,
			TargetType: job.TargetType, TargetID: job.TargetID, AssetSlot: job.AssetSlot,
			VariantKey: job.VariantKey, Required: job.Required, StoryboardID: job.StoryboardID,
			StoryboardVersionAtDispatch: job.StoryboardVersionAtDispatch,
			BindingToken:                job.BindingToken, DeliveryPolicy: deliveryPolicyOr(job.DeliveryPolicy, batchPolicy),
			MaxAttempts:             job.MaxAttempts,
			MaxProviderPollAttempts: normalizedMaxProviderPollAttempts(job.MaxProviderPollAttempts),
			Payload:                 cloneMap(job.Payload),
		})
	}
	sort.Slice(shape.Jobs, func(i, j int) bool {
		left := shape.Jobs[i].TargetID + "\x00" + shape.Jobs[i].AssetSlot + "\x00" + shape.Jobs[i].VariantKey + "\x00" + shape.Jobs[i].Provider
		right := shape.Jobs[j].TargetID + "\x00" + shape.Jobs[j].AssetSlot + "\x00" + shape.Jobs[j].VariantKey + "\x00" + shape.Jobs[j].Provider
		return left < right
	})
	raw, err := json.Marshal(shape)
	if err != nil {
		return fmt.Errorf("marshal generation request fingerprint: %w", err)
	}
	sum := sha256.Sum256(raw)
	command.Operation.RequestFingerprint = hex.EncodeToString(sum[:])
	return nil
}

func validateWorkflowReplay(existing GenerationOperation, requested GenerationOperation) error {
	if existing.SessionID != strings.TrimSpace(requested.SessionID) ||
		existing.UserID != strings.TrimSpace(requested.UserID) ||
		existing.Kind != strings.TrimSpace(requested.Kind) ||
		(existing.RequestFingerprint != "" && existing.RequestFingerprint != requested.RequestFingerprint) {
		return fmt.Errorf("%w: key %s belongs to another immutable request", ErrIdempotencyConflict, requested.IdempotencyKey)
	}
	return nil
}
