package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

const generationBridgeKind = "plan_dispatch"

// GenerationBridge maps the plan runtime's dispatch vocabulary onto the
// existing durable generation workflow aggregate.
type GenerationBridge struct {
	commands *generation.CommandService
}

func NewGenerationBridge(commands *generation.CommandService) *GenerationBridge {
	return &GenerationBridge{commands: commands}
}

// CancelBatch adapts the durable generation cancellation command to the
// scheduler's narrow cancellation dependency.
func (b *GenerationBridge) CancelBatch(ctx context.Context, request BatchCancelRequest) error {
	if b == nil || b.commands == nil {
		return errors.New("generation bridge command service is required")
	}
	_, err := b.commands.CancelBatchOwned(ctx, generation.CancelBatchOwnedRequest{
		BatchID: request.BatchID, SessionID: request.SessionID, UserID: request.UserID,
		WorkflowRunID: request.PlanRunID, StageRunID: request.NodeID,
	})
	return err
}

func (b *GenerationBridge) Dispatch(ctx context.Context, request vocabulary.GenerationDispatchRequest) (vocabulary.GenerationDispatchResult, error) {
	if ctx == nil {
		return vocabulary.GenerationDispatchResult{}, errors.New("generation bridge context is required")
	}
	if err := ctx.Err(); err != nil {
		return vocabulary.GenerationDispatchResult{}, err
	}
	if b == nil || b.commands == nil {
		return vocabulary.GenerationDispatchResult{}, errors.New("generation bridge command service is required")
	}
	if strings.TrimSpace(request.SessionID) == "" || strings.TrimSpace(request.UserID) == "" ||
		strings.TrimSpace(request.PlanRunID) == "" || strings.TrimSpace(request.NodeID) == "" ||
		request.Attempt <= 0 || strings.TrimSpace(request.IdempotencyKey) == "" {
		return vocabulary.GenerationDispatchResult{}, errors.New("generation bridge dispatch context is incomplete")
	}
	inputs, err := cloneGenerationInputs(request.Inputs)
	if err != nil {
		return vocabulary.GenerationDispatchResult{}, err
	}
	targets, ok := inputs["targets"].([]any)
	if !ok || len(targets) == 0 {
		return vocabulary.GenerationDispatchResult{}, errors.New("generation bridge targets must be a non-empty array")
	}

	policy := generation.DeliveryPolicy{
		BindingMode: generation.BindingModeActive, ApprovalPolicy: generation.ApprovalAutoApprove,
		ChargePolicy: generation.ChargePostpaidNoReservation,
	}
	jobs := make([]generation.GenerationJob, 0, len(targets))
	for index, raw := range targets {
		target, ok := raw.(map[string]any)
		if !ok {
			return vocabulary.GenerationDispatchResult{}, fmt.Errorf("generation bridge target %d must be an object", index)
		}
		prompt, _ := target["prompt"].(string)
		if strings.TrimSpace(prompt) == "" {
			return vocabulary.GenerationDispatchResult{}, fmt.Errorf("generation bridge target %d prompt is required", index)
		}
		mediaKind := targetString(target, "media_kind", inputs)
		if mediaKind == "" {
			mediaKind = "image"
		}
		provider := generationProvider(mediaKind)
		if provider == "" {
			return vocabulary.GenerationDispatchResult{}, fmt.Errorf("generation bridge target %d has unsupported media_kind %q", index, mediaKind)
		}
		targetID := strings.TrimSpace(stringValue(target["target_id"]))
		if targetID == "" {
			targetID = fmt.Sprintf("deliverable:%s:%s:%d", request.PlanRunID, request.NodeID, index)
		}
		assetSlot := strings.TrimSpace(stringValue(target["asset_slot"]))
		if assetSlot == "" {
			assetSlot = "primary"
		}
		fingerprint, err := generationTargetFingerprint(request, target)
		if err != nil {
			return vocabulary.GenerationDispatchResult{}, fmt.Errorf("generation bridge target %d fingerprint: %w", index, err)
		}
		payload, err := cloneGenerationInputs(target)
		if err != nil {
			return vocabulary.GenerationDispatchResult{}, fmt.Errorf("generation bridge target %d payload: %w", index, err)
		}
		payload["prompt"] = strings.TrimSpace(prompt)
		payload["media_kind"] = mediaKind
		payload["user_id"] = request.UserID
		payload["plan_run_id"] = request.PlanRunID
		payload["node_id"] = request.NodeID
		payload["plan_attempt"] = request.Attempt
		if _, exists := payload["ratio"]; !exists {
			if ratio, ok := inputs["ratio"]; ok {
				payload["ratio"] = ratio
			}
		}
		jobs = append(jobs, generation.GenerationJob{
			SessionID: request.SessionID, UserID: request.UserID,
			WorkflowRunID: request.PlanRunID, StageRunID: request.NodeID,
			IdempotencyKey: fmt.Sprintf("%s:target:%d", request.IdempotencyKey, index),
			Provider:       provider, MediaKind: mediaKind,
			TargetType: generation.TargetKindSessionDeliverable, TargetID: targetID, AssetSlot: assetSlot,
			Required: true,
			BindingToken: generation.BindingToken{
				TargetKind: generation.TargetKindSessionDeliverable, TargetID: targetID, AssetSlot: assetSlot,
				TargetRevision: 1, InputFingerprint: fingerprint,
			},
			DeliveryPolicy: policy, MaxAttempts: 4, Payload: payload,
		})
	}

	workflow, _, err := b.commands.Create(ctx, generation.CreateWorkflowCommand{
		Operation: generation.GenerationOperation{
			SessionID: request.SessionID, UserID: request.UserID,
			WorkflowRunID: request.PlanRunID, StageRunID: request.NodeID,
			IdempotencyKey: request.IdempotencyKey, Kind: generationBridgeKind,
		},
		Batch: generation.GenerationBatch{
			SessionID: request.SessionID, UserID: request.UserID,
			WorkflowRunID: request.PlanRunID, StageRunID: request.NodeID,
			Kind:             generation.TargetKindSessionDeliverable,
			CompletionPolicy: generation.CompletionAllowPartial, WakePolicy: generation.WakeOnTerminal,
			DeliveryPolicy: policy,
		},
		Jobs: jobs,
	})
	if err != nil {
		return vocabulary.GenerationDispatchResult{}, fmt.Errorf("create generation workflow: %w", err)
	}
	jobIDs := make([]string, len(workflow.Jobs))
	for index := range workflow.Jobs {
		jobIDs[index] = workflow.Jobs[index].ID
	}
	return vocabulary.GenerationDispatchResult{OperationID: workflow.Operation.ID, BatchID: workflow.Batch.ID, JobIDs: jobIDs}, nil
}

func cloneGenerationInputs(value map[string]any) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("generation bridge input is not serializable: %w", err)
	}
	var cloned map[string]any
	if err := decodeSingleJSONValue(raw, &cloned); err != nil {
		return nil, fmt.Errorf("generation bridge input is not serializable: %w", err)
	}
	return cloned, nil
}

func generationTargetFingerprint(request vocabulary.GenerationDispatchRequest, target map[string]any) (string, error) {
	shape := struct {
		PlanRunID string         `json:"plan_run_id"`
		NodeID    string         `json:"node_id"`
		Attempt   int            `json:"attempt"`
		Target    map[string]any `json:"target"`
	}{request.PlanRunID, request.NodeID, request.Attempt, target}
	raw, err := json.Marshal(shape)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func targetString(target map[string]any, key string, inputs map[string]any) string {
	if value := strings.TrimSpace(stringValue(target[key])); value != "" {
		return strings.ToLower(value)
	}
	return strings.ToLower(strings.TrimSpace(stringValue(inputs[key])))
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func generationProvider(mediaKind string) string {
	switch mediaKind {
	case "image", "illustration", "keyframe":
		return generation.ProviderImage2
	case "video":
		return generation.ProviderSeedance
	case "audio", "music":
		return generation.ProviderAudio
	default:
		return ""
	}
}
