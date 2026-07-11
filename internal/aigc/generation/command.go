package generation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type CommandServiceConfig struct {
	Store WorkflowStore
	NewID func() string
	Clock func() time.Time
}

// CommandService owns atomic workflow creation and cancellation intent.
type CommandService struct {
	store WorkflowStore
	newID func() string
	clock func() time.Time
}

func NewCommandService(config CommandServiceConfig) *CommandService {
	if config.NewID == nil {
		config.NewID = defaultID
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &CommandService{store: config.Store, newID: config.NewID, clock: config.Clock}
}

// Create atomically persists Operation, Batch, Jobs and their dispatch Outbox.
// It returns created=false for an existing operation idempotency key.
func (s *CommandService) Create(ctx context.Context, command CreateWorkflowCommand) (WorkflowAggregate, bool, error) {
	if s == nil || s.store == nil {
		return WorkflowAggregate{}, false, fmt.Errorf("generation command store is required")
	}
	if strings.TrimSpace(command.Operation.ID) == "" {
		command.Operation.ID = s.newID()
	}
	if strings.TrimSpace(command.Batch.ID) == "" {
		command.Batch.ID = s.newID()
	}
	for i := range command.Jobs {
		if strings.TrimSpace(command.Jobs[i].ID) == "" {
			command.Jobs[i].ID = s.newID()
		}
		if strings.TrimSpace(command.Jobs[i].IdempotencyKey) == "" {
			command.Jobs[i].IdempotencyKey = "generation:job:" + command.Jobs[i].ID
		}
	}
	return s.store.CreateWorkflow(ctx, command)
}

// CancelBatch freezes cancel intent independently from the transient batch
// status. Queued jobs are cancelled atomically; submitted jobs observe the
// flag and perform cooperative provider cancellation/final settlement.
func (s *CommandService) CancelBatch(ctx context.Context, batchID string) (WorkflowAggregate, error) {
	if s == nil || s.store == nil {
		return WorkflowAggregate{}, fmt.Errorf("generation command store is required")
	}
	return s.store.TransactBatch(ctx, batchID, func(operation *GenerationOperation, batch *GenerationBatch, jobs []*GenerationJob) ([]OutboxEvent, error) {
		if IsTerminalBatchStatus(batch.Status) {
			return nil, nil
		}
		if batch.CancelRequested {
			return nil, nil
		}
		now := s.clock()
		batch.CancelRequested = true
		batch.CancelRequestedAt = &now
		batch.Status = BatchStatusCancelling
		events := []OutboxEvent{{
			IdempotencyKey: "operation:" + operation.ID + ":cancel-requested",
			EventType:      EventOperationCancelRequested,
			Destination:    DestinationSessionSignal,
			Payload:        map[string]any{"batch_id": batch.ID},
		}}
		for _, job := range jobs {
			if IsTerminalJobStatus(job.Status) {
				continue
			}
			job.CancelRequested = true
			events = append(events, OutboxEvent{
				IdempotencyKey: "job:" + job.ID + ":cancel-requested",
				EventType:      EventJobCancelRequested,
				Destination:    DestinationMediaJobs,
				AggregateType:  "job",
				AggregateID:    job.ID,
				JobID:          job.ID,
				Payload:        map[string]any{"job_id": job.ID},
			})
			if job.Status == StatusQueued && strings.TrimSpace(job.ProviderTaskID) == "" {
				job.Status = StatusCancelled
				job.ErrorCode = "cancelled_by_user"
				job.ErrorMessage = "generation was cancelled before provider submission"
			}
		}
		events = append(events, OutboxEvent{
			IdempotencyKey: "batch:" + batch.ID + ":finalize-requested:cancel",
			EventType:      EventBatchFinalizeRequested,
			Destination:    DestinationSessionSignal,
			Payload:        map[string]any{"batch_id": batch.ID},
		})
		return events, nil
	})
}
