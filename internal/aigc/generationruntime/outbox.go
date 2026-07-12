package generationruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
)

type SessionRuntime interface {
	Enqueue(context.Context, string, sessionruntime.SessionInput) (sessionruntime.EnqueueResult, error)
	EnsureSession(context.Context, string) (bool, error)
}

type FinalizedJobPublisher interface {
	PublishFinalized(context.Context, generation.GenerationJob) error
}

type SessionSignalPublisher struct {
	Store        generation.WorkflowStore
	Runtime      SessionRuntime
	Events       a2ui.EventPublisher
	Compensation *generation.CompensationService
	Barrier      *generation.BatchBarrier
	Finalized    FinalizedJobPublisher
	Now          func() time.Time
}

func (p SessionSignalPublisher) PublishOutbox(ctx context.Context, event generation.OutboxEvent) error {
	if event.Destination == generation.DestinationBilling {
		if p.Compensation == nil {
			return fmt.Errorf("generation compensation service is required")
		}
		jobID, _ := event.Payload["job_id"].(string)
		updated, err := p.Compensation.Run(ctx, jobID)
		// Run persists retry_wait/permanent-failure state together with the
		// next outbox event before returning the business error. ACK this
		// consumed request so the newly scheduled event owns the retry.
		if err != nil && strings.TrimSpace(updated.ID) != "" {
			if updated.CompensationStatus == generation.CompensationRetryWait ||
				(updated.CompensationStatus == generation.CompensationPending && !updated.Retryable && updated.ErrorCode == "compensation_failed") {
				return nil
			}
		}
		return err
	}
	if event.Destination != generation.DestinationSessionSignal {
		return nil
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	if event.EventType == generation.EventBatchFinalizeRequested {
		if p.Barrier == nil {
			return fmt.Errorf("generation batch barrier is required")
		}
		batchID := strings.TrimSpace(event.BatchID)
		if batchID == "" {
			batchID, _ = event.Payload["batch_id"].(string)
		}
		_, err := p.Barrier.TryFinalize(ctx, batchID)
		return err
	}
	if event.EventType == generation.EventOperationAccepted || event.EventType == generation.EventOperationCancelRequested {
		return p.publishCurrentOperation(ctx, event)
	}
	if strings.HasPrefix(event.EventType, "batch.") {
		payload, err := decodePostBatch(event.Payload)
		if err != nil {
			return err
		}
		if p.Runtime != nil {
			input := sessionruntime.NewBatchContinuationResult(payload.BatchID, payload.BatchVersion, event.ID)
			input.OperationID, input.StageStatus, input.NeedsAgentExplanation = payload.OperationID, payload.Status, payload.NeedsAgentExplanation
			input.Result, _ = json.Marshal(payload)
			if _, err := p.Runtime.Enqueue(ctx, payload.SessionID, input); err != nil {
				return err
			}
			if _, err := p.Runtime.EnsureSession(context.Background(), payload.SessionID); err != nil {
				return err
			}
		}
		return p.publishOperation(ctx, event, payload.SessionID, payload.OperationID, payload.Status, payload.Cost, event.Payload)
	}
	if strings.HasPrefix(event.EventType, "job.") && p.Store != nil {
		job, err := p.Store.GetJob(ctx, event.JobID)
		if err != nil {
			return err
		}
		if event.EventType == "job.succeeded" && p.Finalized != nil {
			if err := p.Finalized.PublishFinalized(ctx, job); err != nil {
				return err
			}
		}
		return p.publishJob(ctx, event, job)
	}
	if strings.HasPrefix(event.EventType, "billing.") && p.Store != nil {
		jobID, _ := event.Payload["job_id"].(string)
		if strings.TrimSpace(jobID) != "" {
			job, err := p.Store.GetJob(ctx, jobID)
			if err != nil {
				return err
			}
			return p.publishJob(ctx, event, job)
		}
	}
	return nil
}

func (p SessionSignalPublisher) publishCurrentOperation(ctx context.Context, event generation.OutboxEvent) error {
	if p.Store == nil {
		return nil
	}
	operationID := strings.TrimSpace(event.OperationID)
	if operationID == "" && event.AggregateType == "operation" {
		operationID = strings.TrimSpace(event.AggregateID)
	}
	operation, err := p.Store.GetOperation(ctx, operationID)
	if err != nil {
		return err
	}
	status := operation.Status
	cost := generation.CostSummary{}
	if batch, batchErr := p.Store.GetBatch(ctx, operation.BatchID); batchErr == nil {
		status, cost = batch.Status, batch.Cost
	}
	status = projectionString(event.Payload, "status", status)
	return p.publishOperation(ctx, event, operation.SessionID, operation.ID, status, cost, operation.Result)
}

func (p SessionSignalPublisher) publishJob(_ context.Context, _ generation.OutboxEvent, _ generation.GenerationJob) error {
	// Job lifecycle remains durable in the Generation store and is exposed by
	// the jobs read model. It is deliberately not projected as a chat ToolRun:
	// generated candidates belong to the left Storyboard's unified review UI,
	// while the enclosing Operation/Batch owns the single public capability card.
	return nil
}

func (p SessionSignalPublisher) publishOperation(ctx context.Context, event generation.OutboxEvent, sessionID, operationID, status string, cost generation.CostSummary, result map[string]any) error {
	if p.Events == nil {
		return nil
	}
	view := map[string]any{"operation_id": operationID, "status": status, "cost": cost}
	var operation generation.GenerationOperation
	var batch generation.GenerationBatch
	if p.Store != nil {
		if loaded, err := p.Store.GetOperation(ctx, operationID); err == nil {
			operation = loaded
			view["tool_key"] = operation.Kind
			view["display_name"] = generationDisplayName(operation.Kind)
			view["tool_call_id"] = operation.ToolCallID
			view["stage_run_id"] = operation.StageRunID
			view["operation_version"] = operation.Version
			if loadedBatch, batchErr := p.Store.GetBatch(ctx, operation.BatchID); batchErr == nil {
				batch = loadedBatch
				view["batch_id"] = batch.ID
				view["batch_version"] = batch.Version
			}
		}
	}
	if version := projectionInt(event.Payload, "operation_version"); version > 0 {
		view["operation_version"] = version
	}
	if version := projectionInt(event.Payload, "batch_version"); version > 0 {
		view["batch_version"] = version
	}
	if visible := publicProjectionResult(result); len(visible) > 0 {
		view["result"] = visible
	}
	cardID, toolKey := publicCapabilityCard(operation.Kind)
	if cardID == "" {
		return nil
	}
	view["tool_key"] = toolKey
	view["display_name"] = generationDisplayName(toolKey)
	view["summary"] = capabilityProgressSummary(toolKey, status)
	if batch.ID != "" {
		view["batch_version"] = batch.Version
	}
	if version := projectionInt(event.Payload, "batch_version"); version > 0 {
		view["batch_version"] = version
	}
	// The stable high-level card intentionally has no Job nodes, target IDs,
	// asset slots, or result_asset_ids. Those details are rendered only by the
	// Storyboard/read models, never as one chat card per generated asset.
	action := toolRunUpdateAction(cardID, view)
	if generation.IsTerminalBatchStatus(status) || generation.IsTerminalOperationStatus(status) {
		action.Payload.(map[string]any)["refresh_resources"] = []string{"storyboard", "assets", "jobs"}
	}
	envelope := a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: []a2ui.Action{action}}
	return p.Events.Publish(ctx, a2ui.SSEEvent{ID: event.ID + ":projection", SessionID: sessionID, Event: a2ui.EventAction, Payload: envelope, CreatedAt: p.Now()})
}

// publicCapabilityCard collapses all Operation/Batch instances of the same
// user-visible generation capability into one chat progress card per session.
func publicCapabilityCard(operationKind string) (cardID string, toolKey string) {
	kind := strings.ToLower(strings.TrimSpace(operationKind))
	kind = strings.TrimSuffix(kind, "_recovery")
	switch kind {
	case "generate_media", "target_regeneration", "auto_next":
		return "tool_run:generate_media", "generate_media"
	case "assemble_preview", "assemble_export", "assembly", "assemble_output":
		return "tool_run:assemble_output", "assemble_output"
	default:
		return "", ""
	}
}

func capabilityProgressSummary(toolKey, status string) string {
	terminal := generation.IsTerminalBatchStatus(status) || generation.IsTerminalOperationStatus(status)
	if toolKey == "generate_media" {
		if status == generation.BatchStatusCompleted || status == generation.OperationStatusCompleted {
			return "素材已生成，请在左侧故事板统一确认"
		}
		if terminal {
			return "本轮素材生成已结束，请在左侧故事板查看结果"
		}
		return "正在生成素材，完成后请在左侧故事板统一确认"
	}
	if status == generation.BatchStatusCompleted || status == generation.OperationStatusCompleted {
		return "成片输出已完成"
	}
	if terminal {
		return "成片输出已结束"
	}
	return "正在生成成片输出"
}

func toolRunUpdateAction(cardID string, view map[string]any) a2ui.Action {
	return a2ui.Action{Type: a2ui.ActionUpdateCard, Surface: "tool_runs", Target: &a2ui.ActionTarget{Surface: "tool_runs", CardID: cardID}, Payload: map[string]any{"tool_run": view}}
}

func projectionString(values map[string]any, key, fallback string) string {
	if value, ok := values[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func projectionInt(values map[string]any, key string) int {
	value, _ := projectionIntValue(values, key)
	return value
}

func projectionIntValue(values map[string]any, key string) (int, bool) {
	switch value := values[key].(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case json.Number:
		parsed, err := value.Int64()
		return int(parsed), err == nil
	default:
		return 0, false
	}
}

func publicProjectionResult(result map[string]any) map[string]any {
	visible := map[string]any{}
	for _, key := range []string{"status", "estimated_points", "cost", "assembly_revision_id", "recovery_of_operation_id"} {
		if value, exists := result[key]; exists {
			visible[key] = value
		}
	}
	return visible
}

func generationDisplayName(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "image", "illustration", "keyframe", "element_images", "keyframes":
		return "图片素材生成"
	case "video", "videos":
		return "视频素材生成"
	case "audio", "music", "voice":
		return "音频素材生成"
	case "assembly", "assemble_preview", "assemble_export", "assemble_output":
		return "成片合成"
	case "target_regeneration":
		return "局部素材重生成"
	case "generate_media", "auto_next":
		return "素材生成"
	default:
		if strings.TrimSpace(kind) == "" {
			return "生成任务"
		}
		return kind
	}
}

func decodePostBatch(values map[string]any) (generation.PostBatchPayload, error) {
	raw, err := json.Marshal(values)
	if err != nil {
		return generation.PostBatchPayload{}, err
	}
	var payload generation.PostBatchPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, err
	}
	if payload.BatchID == "" || payload.SessionID == "" {
		return payload, fmt.Errorf("invalid post-batch payload")
	}
	return payload, nil
}

var _ generation.OutboxPublisher = SessionSignalPublisher{}
