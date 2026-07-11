package generation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type JobTerminalResult struct {
	JobID             string   `json:"job_id"`
	TargetID          string   `json:"target_id,omitempty"`
	AssetSlot         string   `json:"asset_slot,omitempty"`
	Status            string   `json:"status"`
	ResultDisposition string   `json:"result_disposition,omitempty"`
	ResultAssetIDs    []string `json:"result_asset_ids,omitempty"`
	ErrorCode         string   `json:"error_code,omitempty"`
	GrossPoints       int64    `json:"gross_points,omitempty"`
	RefundedPoints    int64    `json:"refunded_points,omitempty"`
	NetPoints         int64    `json:"net_points,omitempty"`
}

// PostBatchPayload is the immutable, trusted snapshot emitted by the Batch
// Barrier. Session runtime can run deterministic PostBatchContinuation from it
// without inspecting provider payloads.
type PostBatchPayload struct {
	SessionID             string              `json:"session_id"`
	WorkflowRunID         string              `json:"workflow_run_id,omitempty"`
	StageRunID            string              `json:"stage_run_id,omitempty"`
	OperationID           string              `json:"operation_id"`
	ToolCallID            string              `json:"tool_call_id,omitempty"`
	BatchID               string              `json:"batch_id"`
	BatchVersion          int                 `json:"batch_version"`
	Status                string              `json:"status"`
	Cost                  CostSummary         `json:"cost"`
	Jobs                  []JobTerminalResult `json:"jobs"`
	NeedsAgentExplanation bool                `json:"needs_agent_explanation"`
	CreatedAt             time.Time           `json:"created_at"`
}

type BarrierResult struct {
	Terminal     bool             `json:"terminal"`
	EventCreated bool             `json:"event_created"`
	Payload      PostBatchPayload `json:"payload"`
}

type BatchBarrier struct {
	store WorkflowStore
	clock func() time.Time
}

func NewBatchBarrier(store WorkflowStore, clock ...func() time.Time) *BatchBarrier {
	now := time.Now
	if len(clock) > 0 && clock[0] != nil {
		now = clock[0]
	}
	return &BatchBarrier{store: store, clock: now}
}

// tryFinalizeBatchBestEffort is only a low-latency optimization for callers
// that already committed a batch.finalize_requested Outbox row together with
// the state change that may unblock the Barrier. A transient failure must not
// turn an already committed terminal/settlement mutation into a business
// failure; the durable Outbox relay will retry until acknowledged.
func tryFinalizeBatchBestEffort(ctx context.Context, barrier *BatchBarrier, batchID string) {
	if barrier != nil && strings.TrimSpace(batchID) != "" {
		_, _ = barrier.TryFinalize(ctx, batchID)
	}
}

// TryFinalize is the only producer of a Batch terminal event. It waits for all
// jobs and all charged-job compensation settlements, persists the immutable
// gross/refund/net snapshot, and appends exactly one terminal signal.
func (b *BatchBarrier) TryFinalize(ctx context.Context, batchID string) (BarrierResult, error) {
	if b == nil || b.store == nil {
		return BarrierResult{}, fmt.Errorf("batch barrier store is required")
	}
	result := BarrierResult{}
	aggregate, err := b.store.TransactBatch(ctx, batchID, func(operation *GenerationOperation, batch *GenerationBatch, jobs []*GenerationJob) ([]OutboxEvent, error) {
		if IsTerminalBatchStatus(batch.Status) {
			result.Terminal = true
			result.Payload = buildPostBatchPayload(*operation, *batch, jobs, b.clock())
			return nil, nil
		}

		recountBatch(batch, jobs)
		for _, job := range jobs {
			if !IsTerminalJobStatus(job.Status) {
				if batch.CancelRequested {
					batch.Status = BatchStatusCancelling
				}
				return nil, nil
			}
		}

		for _, job := range jobs {
			if strings.TrimSpace(job.BillingTransactionID) == "" {
				continue
			}
			status := normalizedCompensationStatus(job.CompensationStatus)
			switch status {
			case CompensationPending, CompensationRunning, CompensationRetryWait:
				if batch.CancelRequested {
					batch.Status = BatchStatusCancelling
				} else {
					batch.Status = BatchStatusFinalizing
				}
				return nil, nil
			case CompensationNotRequired, CompensationCompleted, CompensationManualFinal:
			default:
				return nil, fmt.Errorf("job %s has invalid compensation status %q", job.ID, job.CompensationStatus)
			}
		}

		batch.Cost = aggregateCost(jobs)
		if batch.CancelRequested {
			switch {
			case batch.SucceededJobs == len(jobs):
				// A durable domain commit may have won just before cancellation and
				// been recovered from its receipt. Delivered/charged work is complete,
				// even though a later cancel intent was recorded.
				batch.Status = BatchStatusCompleted
			case batch.SucceededJobs > 0:
				batch.Status = BatchStatusPartialFailed
			default:
				batch.Status = BatchStatusCancelled
			}
		} else {
			batch.Status = evaluateCompletionPolicy(*batch, jobs)
		}
		operation.Status = operationStatusForBatch(batch.Status)
		operation.ErrorCode = batch.ErrorCode
		operation.ErrorMessage = batch.ErrorMessage
		result.Terminal = true
		result.EventCreated = true
		result.Payload = buildPostBatchPayload(*operation, *batch, jobs, b.clock())
		result.Payload.BatchVersion = batch.Version + 1
		// Persist the deterministic Tool result in the Operation aggregate before
		// emitting the terminal outbox signal. Consumers can replay or hydrate the
		// result without asking the Agent to reconstruct provider outcomes.
		operation.Result = mapFromPostBatchPayload(result.Payload)

		eventType := terminalEventType(batch.Status)
		payload := mapFromPostBatchPayload(result.Payload)
		return []OutboxEvent{{
			IdempotencyKey:   "batch:" + batch.ID + ":terminal",
			EventType:        eventType,
			Destination:      DestinationSessionSignal,
			AggregateType:    "batch",
			AggregateID:      batch.ID,
			AggregateVersion: batch.Version + 1,
			Payload:          payload,
		}}, nil
	})
	if err != nil {
		return BarrierResult{}, err
	}
	if result.Terminal && result.Payload.BatchID == "" {
		jobs := make([]*GenerationJob, len(aggregate.Jobs))
		for i := range aggregate.Jobs {
			jobs[i] = &aggregate.Jobs[i]
		}
		result.Payload = buildPostBatchPayload(aggregate.Operation, aggregate.Batch, jobs, b.clock())
	}
	return result, nil
}

func recountBatch(batch *GenerationBatch, jobs []*GenerationJob) {
	batch.RequiredJobs = 0
	batch.OptionalJobs = 0
	batch.SucceededJobs = 0
	batch.FailedJobs = 0
	batch.CancelledJobs = 0
	for _, job := range jobs {
		if job.Required {
			batch.RequiredJobs++
		} else {
			batch.OptionalJobs++
		}
		switch job.Status {
		case StatusSucceeded:
			batch.SucceededJobs++
		case StatusFailed:
			batch.FailedJobs++
		case StatusCancelled:
			batch.CancelledJobs++
		}
	}
}

func evaluateCompletionPolicy(batch GenerationBatch, jobs []*GenerationJob) string {
	switch batch.CompletionPolicy {
	case CompletionAllowPartial:
		if batch.SucceededJobs == len(jobs) {
			return BatchStatusCompleted
		}
		if batch.SucceededJobs > 0 {
			return BatchStatusPartialFailed
		}
		return BatchStatusFailed
	case CompletionMinSuccess:
		minimum := batch.MinSuccess
		if minimum <= 0 {
			minimum = 1
		}
		if batch.SucceededJobs >= minimum {
			return BatchStatusCompleted
		}
		return BatchStatusFailed
	default:
		for _, job := range jobs {
			if job.Required && job.Status != StatusSucceeded {
				return BatchStatusFailed
			}
		}
		return BatchStatusCompleted
	}
}

func aggregateCost(jobs []*GenerationJob) CostSummary {
	grossSeen := make(map[string]struct{})
	refundSeen := make(map[string]struct{})
	result := CostSummary{Breakdown: map[string]int64{}}
	var balanceUpdatedAt time.Time
	for _, job := range jobs {
		if transactionID := strings.TrimSpace(job.BillingTransactionID); transactionID != "" {
			if _, exists := grossSeen[transactionID]; !exists {
				grossSeen[transactionID] = struct{}{}
				result.GrossChargedPoints += job.ChargedPoints
				for key, value := range job.CostBreakdown {
					result.Breakdown[key] += value
				}
			}
		}
		if refundID := strings.TrimSpace(job.RefundTransactionID); refundID != "" {
			if _, exists := refundSeen[refundID]; !exists {
				refundSeen[refundID] = struct{}{}
				result.RefundedPoints += job.CompensatedPoints
			}
		}
		if job.BalanceAfter != nil && (result.BalanceAfter == nil || job.UpdatedAt.After(balanceUpdatedAt)) {
			balance := *job.BalanceAfter
			result.BalanceAfter = &balance
			balanceUpdatedAt = job.UpdatedAt
		}
	}
	result.NetChargedPoints = result.GrossChargedPoints - result.RefundedPoints
	if result.RefundedPoints != 0 {
		result.Breakdown["refund"] -= result.RefundedPoints
	}
	if len(result.Breakdown) == 0 {
		result.Breakdown = nil
	}
	return result
}

func buildPostBatchPayload(operation GenerationOperation, batch GenerationBatch, jobs []*GenerationJob, now time.Time) PostBatchPayload {
	items := make([]JobTerminalResult, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, JobTerminalResult{
			JobID: job.ID, TargetID: job.TargetID, AssetSlot: job.AssetSlot,
			Status: job.Status, ResultDisposition: job.ResultDisposition,
			ResultAssetIDs: append([]string(nil), job.ResultAssetIDs...), ErrorCode: job.ErrorCode,
			GrossPoints: job.ChargedPoints, RefundedPoints: job.CompensatedPoints,
			NetPoints: job.ChargedPoints - job.CompensatedPoints,
		})
	}
	createdAt := now
	if batch.TerminalAt != nil {
		createdAt = *batch.TerminalAt
	}
	return PostBatchPayload{
		SessionID: batch.SessionID, WorkflowRunID: batch.WorkflowRunID, StageRunID: batch.StageRunID,
		OperationID: operation.ID, ToolCallID: batch.ToolCallID, BatchID: batch.ID,
		BatchVersion: batch.Version, Status: batch.Status, Cost: batch.Cost, Jobs: items,
		NeedsAgentExplanation: needsAgentExplanation(batch.WakePolicy, batch.Status), CreatedAt: createdAt,
	}
}

func needsAgentExplanation(policy, status string) bool {
	switch policy {
	case WakeNever:
		return false
	case WakeOnFailure:
		return status == BatchStatusPartialFailed || status == BatchStatusFailed || status == BatchStatusCancelled
	default:
		return true
	}
}

func operationStatusForBatch(status string) string {
	switch status {
	case BatchStatusCompleted:
		return OperationStatusCompleted
	case BatchStatusPartialFailed:
		return OperationStatusPartialFailed
	case BatchStatusCancelled:
		return OperationStatusCancelled
	default:
		return OperationStatusFailed
	}
}

func terminalEventType(status string) string {
	return "batch." + status
}

func normalizedCompensationStatus(status string) string {
	if strings.TrimSpace(status) == "" {
		return CompensationNotRequired
	}
	return status
}

func mapFromPostBatchPayload(payload PostBatchPayload) map[string]any {
	return cloneMap(map[string]any{
		"session_id":              payload.SessionID,
		"workflow_run_id":         payload.WorkflowRunID,
		"stage_run_id":            payload.StageRunID,
		"operation_id":            payload.OperationID,
		"tool_call_id":            payload.ToolCallID,
		"batch_id":                payload.BatchID,
		"batch_version":           payload.BatchVersion,
		"status":                  payload.Status,
		"cost":                    payload.Cost,
		"jobs":                    payload.Jobs,
		"needs_agent_explanation": payload.NeedsAgentExplanation,
		"created_at":              payload.CreatedAt,
	})
}
