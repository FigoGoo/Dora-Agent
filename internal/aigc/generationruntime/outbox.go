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

func (p SessionSignalPublisher) publishJob(ctx context.Context, event generation.OutboxEvent, job generation.GenerationJob) error {
	if p.Events == nil {
		return nil
	}
	view := jobProjectionView(event, job)
	actions := []a2ui.Action{toolRunUpdateAction("job:"+job.ID, view)}
	if stageAction, ok, err := p.jobStageProgressAction(ctx, event, job, view); err != nil {
		return err
	} else if ok {
		actions = append(actions, stageAction)
	}
	envelope := a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: actions}
	return p.Events.Publish(ctx, a2ui.SSEEvent{ID: event.ID + ":projection", SessionID: job.SessionID, Event: a2ui.EventAction, Payload: envelope, CreatedAt: p.Now()})
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
	statusVersion := operationProjectionVersion(event, operation, batch)
	view["status_version"] = statusVersion
	if visible := publicProjectionResult(result); len(visible) > 0 {
		view["result"] = visible
	}
	actions := []a2ui.Action{{Type: a2ui.ActionUpdateCard, Surface: "tool_runs", Target: &a2ui.ActionTarget{Surface: "tool_runs", CardID: "operation:" + operationID}, Payload: map[string]any{"operation": view}}}
	if cardID := publicStageCardID(operation.ToolCallID, operation.StageRunID); cardID != "" {
		stageView := map[string]any{
			"operation_id": operationID, "batch_id": operation.BatchID,
			"tool_call_id": operation.ToolCallID, "stage_run_id": operation.StageRunID,
			"tool_key": operation.Kind, "display_name": generationDisplayName(operation.Kind),
			"status": status, "status_version": statusVersion, "cost": cost,
		}
		if batch.ID != "" {
			stageView["batch_version"] = batch.Version
		}
		if version := projectionInt(event.Payload, "batch_version"); version > 0 {
			stageView["batch_version"] = version
		}
		actions = append(actions, toolRunUpdateAction(cardID, stageView))
	}
	envelope := a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: actions}
	return p.Events.Publish(ctx, a2ui.SSEEvent{ID: event.ID + ":projection", SessionID: sessionID, Event: a2ui.EventAction, Payload: envelope, CreatedAt: p.Now()})
}

func operationProjectionVersion(event generation.OutboxEvent, operation generation.GenerationOperation, batch generation.GenerationBatch) int {
	if version := projectionInt(event.Payload, "batch_version"); version > 0 {
		return version
	}
	if batch.Version > 0 {
		return batch.Version
	}
	if version := projectionInt(event.Payload, "operation_version"); version > 0 {
		return version
	}
	if operation.Version > 0 {
		return operation.Version
	}
	return event.AggregateVersion
}

func (p SessionSignalPublisher) jobStageProgressAction(ctx context.Context, event generation.OutboxEvent, job generation.GenerationJob, jobView map[string]any) (a2ui.Action, bool, error) {
	toolCallID := projectionString(event.Payload, "tool_call_id", job.ToolCallID)
	stageRunID := projectionString(event.Payload, "stage_run_id", job.StageRunID)
	cardID := publicStageCardID(toolCallID, stageRunID)
	if cardID == "" {
		return a2ui.Action{}, false, nil
	}
	operationID := projectionString(event.Payload, "operation_id", job.OperationID)
	batchID := projectionString(event.Payload, "batch_id", job.BatchID)
	toolKey := ""
	if p.Store != nil && operationID != "" {
		operation, err := p.Store.GetOperation(ctx, operationID)
		if err != nil {
			return a2ui.Action{}, false, err
		}
		toolKey = operation.Kind
	}
	node := map[string]any{
		"node_key": "job:" + job.ID, "job_id": job.ID,
		"display_name": jobView["display_name"], "status": jobView["status"],
		"phase": jobView["phase"], "status_version": jobView["status_version"],
	}
	if summary, _ := jobView["summary"].(string); summary != "" {
		node["description"] = summary
	}
	if code, _ := jobView["error_code"].(string); code != "" {
		node["message"] = code
	}
	stageView := map[string]any{
		"operation_id": operationID, "batch_id": batchID,
		"tool_call_id": toolCallID, "stage_run_id": stageRunID,
		"tool_key": toolKey, "display_name": generationDisplayName(toolKey),
		"nodes": []map[string]any{node},
	}
	return toolRunUpdateAction(cardID, stageView), true, nil
}

func jobProjectionView(event generation.OutboxEvent, job generation.GenerationJob) map[string]any {
	status := projectionString(event.Payload, "status", job.Status)
	phase := projectionString(event.Payload, "phase", job.Phase)
	mediaKind := projectionString(event.Payload, "media_kind", job.MediaKind)
	targetID := projectionString(event.Payload, "target_id", job.TargetID)
	assetSlot := projectionString(event.Payload, "asset_slot", job.AssetSlot)
	view := map[string]any{
		"job_id": job.ID, "session_id": projectionString(event.Payload, "session_id", job.SessionID),
		"operation_id": projectionString(event.Payload, "operation_id", job.OperationID),
		"batch_id":     projectionString(event.Payload, "batch_id", job.BatchID),
		"tool_call_id": projectionString(event.Payload, "tool_call_id", job.ToolCallID),
		"stage_run_id": projectionString(event.Payload, "stage_run_id", job.StageRunID),
		"status":       status, "phase": phase, "status_version": projectionVersion(event, job),
		"provider":        projectionString(event.Payload, "provider", job.Provider),
		"provider_status": projectionString(event.Payload, "provider_status", job.ProviderStatus),
		"target_id":       targetID, "asset_slot": assetSlot,
		"retry_count":            projectionIntFallback(event.Payload, "retry_count", job.RetryCount),
		"provider_poll_attempts": projectionIntFallback(event.Payload, "provider_poll_attempts", job.ProviderPollAttempts),
		"error_code":             projectionString(event.Payload, "error_code", job.ErrorCode),
		"display_name":           generationDisplayName(mediaKind),
		"summary":                strings.Trim(strings.TrimSpace(targetID)+" · "+strings.TrimSpace(assetSlot), " ·"),
	}
	if status == generation.StatusSucceeded {
		if assetIDs := projectionStringSlice(event.Payload, "result_asset_ids"); len(assetIDs) > 0 {
			view["result_asset_ids"] = assetIDs
		} else {
			view["result_asset_ids"] = append([]string(nil), job.ResultAssetIDs...)
		}
	}
	if nextRunAt, ok := event.Payload["next_run_at"]; ok {
		view["next_run_at"] = nextRunAt
	}
	return view
}

func toolRunUpdateAction(cardID string, view map[string]any) a2ui.Action {
	return a2ui.Action{Type: a2ui.ActionUpdateCard, Surface: "tool_runs", Target: &a2ui.ActionTarget{Surface: "tool_runs", CardID: cardID}, Payload: map[string]any{"tool_run": view}}
}

func publicStageCardID(toolCallID, stageRunID string) string {
	if value := strings.TrimSpace(toolCallID); value != "" {
		return "tool_run:" + value
	}
	if value := strings.TrimSpace(stageRunID); value != "" {
		return "tool_run:" + value
	}
	return ""
}

func projectionVersion(event generation.OutboxEvent, job generation.GenerationJob) int {
	if version := projectionInt(event.Payload, "status_version"); version > 0 {
		return version
	}
	if event.AggregateVersion > 0 {
		return event.AggregateVersion
	}
	return job.StatusVersion
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

func projectionIntFallback(values map[string]any, key string, fallback int) int {
	if value, ok := projectionIntValue(values, key); ok {
		return value
	}
	return fallback
}

func projectionStringSlice(values map[string]any, key string) []string {
	switch value := values[key].(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		result := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, text)
			}
		}
		return result
	default:
		return nil
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
	case "assembly", "assemble_preview", "assemble_export":
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
