package generation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	OutboxPending   = "pending"
	OutboxPublished = "published"
	OutboxDead      = "dead"

	DestinationMediaJobs     = "media.jobs"
	DestinationSessionSignal = "session.signals"
	DestinationBilling       = "billing.compensation"

	EventOperationAccepted            = "operation.accepted"
	EventJobDispatch                  = "job.dispatch"
	EventJobCancelRequested           = "job.cancel_requested"
	EventOperationCancelRequested     = "operation.cancel_requested"
	EventBatchFinalizeRequested       = "batch.finalize_requested"
	EventJobLifecycleChanged          = "job.lifecycle_changed"
	EventBillingCompensationRequested = "billing.compensation_requested"
	EventBillingCompensationCompleted = "billing.compensation_completed"
	EventBillingCompensationFailed    = "billing.compensation_failed"
)

// OutboxEvent is saved in the same transaction as its aggregate mutation.
// IdempotencyKey is the stable producer key and must be globally unique.
type OutboxEvent struct {
	ID               string         `json:"id"`
	IdempotencyKey   string         `json:"idempotency_key"`
	EventType        string         `json:"event_type"`
	Destination      string         `json:"destination"`
	AggregateType    string         `json:"aggregate_type"`
	AggregateID      string         `json:"aggregate_id"`
	AggregateVersion int            `json:"aggregate_version"`
	SessionID        string         `json:"session_id,omitempty"`
	WorkflowRunID    string         `json:"workflow_run_id,omitempty"`
	StageRunID       string         `json:"stage_run_id,omitempty"`
	OperationID      string         `json:"operation_id,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
	BatchID          string         `json:"batch_id,omitempty"`
	JobID            string         `json:"job_id,omitempty"`
	Payload          map[string]any `json:"payload,omitempty"`
	Status           string         `json:"status"`
	Attempts         int            `json:"attempts"`
	AvailableAt      time.Time      `json:"available_at"`
	CreatedAt        time.Time      `json:"created_at"`
	PublishedAt      *time.Time     `json:"published_at,omitempty"`
}

type WorkflowAggregate struct {
	Operation GenerationOperation `json:"operation"`
	Batch     GenerationBatch     `json:"batch"`
	Jobs      []GenerationJob     `json:"jobs"`
}

type CreateWorkflowCommand struct {
	Operation GenerationOperation
	Batch     GenerationBatch
	Jobs      []GenerationJob
}

type JobMutation func(job *GenerationJob) ([]OutboxEvent, error)
type BatchTransaction func(operation *GenerationOperation, batch *GenerationBatch, jobs []*GenerationJob) ([]OutboxEvent, error)

// WorkflowStore is deliberately transaction-oriented. Implementations must
// atomically commit aggregate changes and returned outbox events.
type WorkflowStore interface {
	CreateWorkflow(ctx context.Context, command CreateWorkflowCommand) (WorkflowAggregate, bool, error)
	GetOperation(ctx context.Context, operationID string) (GenerationOperation, error)
	GetOperationByIdempotencyKey(ctx context.Context, key string) (GenerationOperation, error)
	GetBatch(ctx context.Context, batchID string) (GenerationBatch, error)
	GetJob(ctx context.Context, jobID string) (GenerationJob, error)
	ListJobsByBatch(ctx context.Context, batchID string) ([]GenerationJob, error)
	ListRunnableJobs(ctx context.Context, now time.Time, limit int) ([]GenerationJob, error)
	RenewJobLease(ctx context.Context, jobID, leaseOwner string, leaseUntil time.Time) error
	MutateJob(ctx context.Context, jobID string, expectedVersion int, mutation JobMutation) (GenerationJob, error)
	TransactBatch(ctx context.Context, batchID string, transaction BatchTransaction) (WorkflowAggregate, error)
	ListOutbox(ctx context.Context, status string, limit int) ([]OutboxEvent, error)
	MarkOutboxPublished(ctx context.Context, eventID string, at time.Time) error
	MarkOutboxFailed(ctx context.Context, eventID string, at time.Time, maxAttempts int) error
}

type MemoryStore struct {
	mu sync.Mutex

	operations     map[string]GenerationOperation
	operationByKey map[string]string
	batches        map[string]GenerationBatch
	jobs           map[string]GenerationJob
	jobIDsByBatch  map[string][]string
	outbox         map[string]OutboxEvent
	outboxByKey    map[string]string
	clock          func() time.Time
	newID          func() string
}

type MemoryStoreOption func(*MemoryStore)

func WithMemoryClock(clock func() time.Time) MemoryStoreOption {
	return func(store *MemoryStore) {
		if clock != nil {
			store.clock = clock
		}
	}
}

func WithMemoryIDGenerator(newID func() string) MemoryStoreOption {
	return func(store *MemoryStore) {
		if newID != nil {
			store.newID = newID
		}
	}
}

func NewMemoryStore(options ...MemoryStoreOption) *MemoryStore {
	store := &MemoryStore{
		operations:     make(map[string]GenerationOperation),
		operationByKey: make(map[string]string),
		batches:        make(map[string]GenerationBatch),
		jobs:           make(map[string]GenerationJob),
		jobIDsByBatch:  make(map[string][]string),
		outbox:         make(map[string]OutboxEvent),
		outboxByKey:    make(map[string]string),
		clock:          time.Now,
		newID:          defaultID,
	}
	for _, option := range options {
		option(store)
	}
	return store
}

func (s *MemoryStore) CreateWorkflow(_ context.Context, command CreateWorkflowCommand) (WorkflowAggregate, bool, error) {
	if s == nil {
		return WorkflowAggregate{}, false, fmt.Errorf("generation memory store is required")
	}
	if err := freezeWorkflowRequest(&command); err != nil {
		return WorkflowAggregate{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	command.Operation.ID = strings.TrimSpace(command.Operation.ID)
	command.Operation.SessionID = strings.TrimSpace(command.Operation.SessionID)
	command.Operation.IdempotencyKey = strings.TrimSpace(command.Operation.IdempotencyKey)
	command.Batch.ID = strings.TrimSpace(command.Batch.ID)
	if command.Operation.ID == "" || command.Operation.SessionID == "" || command.Operation.IdempotencyKey == "" || command.Batch.ID == "" {
		return WorkflowAggregate{}, false, fmt.Errorf("operation id, session id, idempotency key and batch id are required")
	}
	if existingID, ok := s.operationByKey[command.Operation.IdempotencyKey]; ok {
		existing := s.operations[existingID]
		if err := validateWorkflowReplay(existing, command.Operation); err != nil {
			return WorkflowAggregate{}, false, err
		}
		return s.aggregateLocked(existing.BatchID), false, nil
	}
	if _, ok := s.operations[command.Operation.ID]; ok {
		return WorkflowAggregate{}, false, fmt.Errorf("%w: operation %s", ErrDuplicate, command.Operation.ID)
	}
	if _, ok := s.batches[command.Batch.ID]; ok {
		return WorkflowAggregate{}, false, fmt.Errorf("%w: batch %s", ErrDuplicate, command.Batch.ID)
	}
	if len(command.Jobs) == 0 {
		return WorkflowAggregate{}, false, fmt.Errorf("at least one generation job is required")
	}

	now := s.clock()
	operation := cloneOperation(command.Operation)
	operation.BatchID = command.Batch.ID
	operation.Status = OperationStatusWaitingJobs
	operation.Version = normalizeVersion(operation.Version)
	operation.CreatedAt = timeOr(operation.CreatedAt, now)
	operation.UpdatedAt = now

	batch := cloneBatch(command.Batch)
	batch.OperationID = operation.ID
	batch.SessionID = operation.SessionID
	batch.UserID = valueOrDefault(batch.UserID, operation.UserID)
	batch.WorkflowRunID = valueOrDefault(batch.WorkflowRunID, operation.WorkflowRunID)
	batch.StageRunID = valueOrDefault(batch.StageRunID, operation.StageRunID)
	batch.ToolCallID = valueOrDefault(batch.ToolCallID, operation.ToolCallID)
	batch.Status = BatchStatusWaitingJobs
	batch.CompletionPolicy = normalizeCompletionPolicy(batch.CompletionPolicy)
	batch.WakePolicy = normalizeWakePolicy(batch.WakePolicy)
	batch.DeliveryPolicy = batch.DeliveryPolicy.Normalize()
	if err := batch.DeliveryPolicy.Validate(); err != nil {
		return WorkflowAggregate{}, false, err
	}
	batch.Version = normalizeVersion(batch.Version)
	batch.CreatedAt = timeOr(batch.CreatedAt, now)
	batch.UpdatedAt = now

	jobs := make([]GenerationJob, 0, len(command.Jobs))
	seenJobs := make(map[string]struct{}, len(command.Jobs))
	defaultAllRequired := batch.CompletionPolicy == CompletionAllRequired
	for _, input := range command.Jobs {
		if input.Required {
			defaultAllRequired = false
			break
		}
	}
	for _, input := range command.Jobs {
		job := cloneJob(input)
		if defaultAllRequired {
			job.Required = true
		}
		job.ID = strings.TrimSpace(job.ID)
		job.IdempotencyKey = strings.TrimSpace(job.IdempotencyKey)
		if job.ID == "" || job.IdempotencyKey == "" {
			return WorkflowAggregate{}, false, fmt.Errorf("job id and idempotency key are required")
		}
		if _, ok := seenJobs[job.ID]; ok {
			return WorkflowAggregate{}, false, fmt.Errorf("%w: job %s", ErrDuplicate, job.ID)
		}
		if _, ok := s.jobs[job.ID]; ok {
			return WorkflowAggregate{}, false, fmt.Errorf("%w: job %s", ErrDuplicate, job.ID)
		}
		seenJobs[job.ID] = struct{}{}
		job.BatchID = batch.ID
		job.OperationID = operation.ID
		job.SessionID = operation.SessionID
		job.UserID = valueOrDefault(job.UserID, operation.UserID)
		job.WorkflowRunID = valueOrDefault(job.WorkflowRunID, operation.WorkflowRunID)
		job.StageRunID = valueOrDefault(job.StageRunID, operation.StageRunID)
		job.ToolCallID = valueOrDefault(job.ToolCallID, operation.ToolCallID)
		job.Status = StatusQueued
		job.Phase = PhaseProviderSubmit
		job.StatusVersion = normalizeVersion(job.StatusVersion)
		job.DeliveryPolicy = deliveryPolicyOr(job.DeliveryPolicy, batch.DeliveryPolicy)
		if err := job.DeliveryPolicy.Validate(); err != nil {
			return WorkflowAggregate{}, false, fmt.Errorf("job %s: %w", job.ID, err)
		}
		if job.BindingToken.StoryboardID == "" {
			job.BindingToken.StoryboardID = job.StoryboardID
		}
		if job.BindingToken.TargetID == "" {
			job.BindingToken.TargetID = job.TargetID
		}
		if job.BindingToken.AssetSlot == "" {
			job.BindingToken.AssetSlot = job.AssetSlot
		}
		if job.StoryboardID == "" {
			job.StoryboardID = job.BindingToken.StoryboardID
		}
		if job.TargetID == "" {
			job.TargetID = job.BindingToken.TargetID
		}
		if job.AssetSlot == "" {
			job.AssetSlot = job.BindingToken.AssetSlot
		}
		if strings.TrimSpace(job.StoryboardID) != "" {
			if err := job.BindingToken.Validate(); err != nil {
				return WorkflowAggregate{}, false, fmt.Errorf("job %s: %w", job.ID, err)
			}
		}
		if job.MaxAttempts <= 0 {
			job.MaxAttempts = max(1, job.MaxRetries+1)
		}
		if job.MaxProviderPollAttempts <= 0 {
			job.MaxProviderPollAttempts = DefaultProviderMaxPollAttempts
		}
		if job.BillingStatus == "" {
			job.BillingStatus = BillingNotStarted
		}
		if job.CompensationStatus == "" {
			job.CompensationStatus = CompensationNotRequired
		}
		job.CreatedAt = timeOr(job.CreatedAt, now)
		job.UpdatedAt = now
		if job.Required {
			batch.RequiredJobs++
		} else {
			batch.OptionalJobs++
		}
		jobs = append(jobs, job)
	}

	events := []OutboxEvent{{
		IdempotencyKey: "operation:" + operation.ID + ":accepted",
		EventType:      EventOperationAccepted, Destination: DestinationSessionSignal,
		AggregateType: "operation", AggregateID: operation.ID, AggregateVersion: operation.Version,
		SessionID: operation.SessionID, WorkflowRunID: operation.WorkflowRunID, StageRunID: operation.StageRunID,
		OperationID: operation.ID, ToolCallID: operation.ToolCallID,
		Payload: map[string]any{
			"batch_id": batch.ID, "status": batch.Status,
			"operation_status": operation.Status, "operation_version": operation.Version,
			"batch_version": batch.Version,
		},
	}}
	for _, job := range jobs {
		events = append(events, newJobDispatchEvent(job))
	}
	if err := s.appendOutboxBatchLocked(events); err != nil {
		return WorkflowAggregate{}, false, err
	}

	s.operations[operation.ID] = operation
	s.operationByKey[operation.IdempotencyKey] = operation.ID
	s.batches[batch.ID] = batch
	for _, job := range jobs {
		s.jobs[job.ID] = job
		s.jobIDsByBatch[batch.ID] = append(s.jobIDsByBatch[batch.ID], job.ID)
	}
	return s.aggregateLocked(batch.ID), true, nil
}

func (s *MemoryStore) GetOperation(_ context.Context, operationID string) (GenerationOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operation, ok := s.operations[strings.TrimSpace(operationID)]
	if !ok {
		return GenerationOperation{}, fmt.Errorf("%w: operation %s", ErrNotFound, operationID)
	}
	return cloneOperation(operation), nil
}

func (s *MemoryStore) GetOperationByIdempotencyKey(_ context.Context, key string) (GenerationOperation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.operationByKey[strings.TrimSpace(key)]
	if !ok {
		return GenerationOperation{}, fmt.Errorf("%w: operation idempotency key %s", ErrNotFound, key)
	}
	return cloneOperation(s.operations[id]), nil
}

func (s *MemoryStore) GetBatch(_ context.Context, batchID string) (GenerationBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, ok := s.batches[strings.TrimSpace(batchID)]
	if !ok {
		return GenerationBatch{}, fmt.Errorf("%w: batch %s", ErrNotFound, batchID)
	}
	return cloneBatch(batch), nil
}

func (s *MemoryStore) GetJob(_ context.Context, jobID string) (GenerationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[strings.TrimSpace(jobID)]
	if !ok {
		return GenerationJob{}, fmt.Errorf("%w: job %s", ErrNotFound, jobID)
	}
	return cloneJob(job), nil
}

func (s *MemoryStore) RenewJobLease(_ context.Context, jobID, leaseOwner string, leaseUntil time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[strings.TrimSpace(jobID)]
	if !ok {
		return fmt.Errorf("%w: job %s", ErrNotFound, jobID)
	}
	if job.LeaseOwner != strings.TrimSpace(leaseOwner) || (job.Status != StatusRunning && job.Status != StatusFinalizing) {
		return fmt.Errorf("%w: generation job lease is not owned by %s", ErrVersionConflict, leaseOwner)
	}
	until := leaseUntil
	job.LeaseUntil = &until
	job.UpdatedAt = s.clock()
	s.jobs[job.ID] = job
	return nil
}

func (s *MemoryStore) ListJobsByBatch(_ context.Context, batchID string) ([]GenerationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.batches[strings.TrimSpace(batchID)]; !ok {
		return nil, fmt.Errorf("%w: batch %s", ErrNotFound, batchID)
	}
	return s.jobsByBatchLocked(batchID), nil
}

// ListBySession mirrors the read model exposed by PostgresWorkflowStore. It is
// intentionally not part of WorkflowStore because lifecycle workers do not
// need a session-wide scan; HTTP/capability projections do.
func (s *MemoryStore) ListBySession(_ context.Context, sessionID string) ([]GenerationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessionID = strings.TrimSpace(sessionID)
	jobs := make([]GenerationJob, 0)
	for _, job := range s.jobs {
		if job.SessionID == sessionID {
			jobs = append(jobs, cloneJob(job))
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].UpdatedAt.Equal(jobs[j].UpdatedAt) {
			return jobs[i].ID < jobs[j].ID
		}
		return jobs[i].UpdatedAt.After(jobs[j].UpdatedAt)
	})
	return jobs, nil
}

func (s *MemoryStore) ListRunnableJobs(_ context.Context, now time.Time, limit int) ([]GenerationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now.IsZero() {
		now = s.clock()
	}
	jobs := make([]GenerationJob, 0)
	for _, stored := range s.jobs {
		if !jobRunnableAt(stored, now) {
			continue
		}
		jobs = append(jobs, cloneJob(stored))
	}
	sort.Slice(jobs, func(i, j int) bool {
		left := runnableTime(jobs[i])
		right := runnableTime(jobs[j])
		if left.Equal(right) {
			return jobs[i].ID < jobs[j].ID
		}
		return left.Before(right)
	})
	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

func (s *MemoryStore) MutateJob(_ context.Context, jobID string, expectedVersion int, mutation JobMutation) (GenerationJob, error) {
	if mutation == nil {
		return GenerationJob{}, fmt.Errorf("job mutation is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobID = strings.TrimSpace(jobID)
	job, ok := s.jobs[jobID]
	if !ok {
		return GenerationJob{}, fmt.Errorf("%w: job %s", ErrNotFound, jobID)
	}
	if expectedVersion > 0 && job.StatusVersion != expectedVersion {
		return GenerationJob{}, fmt.Errorf("%w: job %s expected %d got %d", ErrVersionConflict, jobID, expectedVersion, job.StatusVersion)
	}
	before := cloneJob(job)
	events, err := mutation(&job)
	if err != nil {
		return GenerationJob{}, err
	}
	events = ensureJobLifecycleEvent(events, before, job, true)
	if before.Status != job.Status {
		if err := ValidateJobTransition(before.Status, job.Status); err != nil {
			return GenerationJob{}, err
		}
	}
	now := s.clock()
	job.StatusVersion++
	job.UpdatedAt = now
	if before.StartedAt == nil && job.Status == StatusRunning {
		job.StartedAt = &now
	}
	if IsTerminalJobStatus(job.Status) && job.TerminalAt == nil {
		job.TerminalAt = &now
	}
	for i := range events {
		fillJobEvent(&events[i], job)
	}
	if err := s.appendOutboxBatchLocked(events); err != nil {
		return GenerationJob{}, err
	}
	s.jobs[jobID] = cloneJob(job)
	return cloneJob(job), nil
}

func (s *MemoryStore) TransactBatch(_ context.Context, batchID string, transaction BatchTransaction) (WorkflowAggregate, error) {
	if transaction == nil {
		return WorkflowAggregate{}, fmt.Errorf("batch transaction is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	batchID = strings.TrimSpace(batchID)
	batch, ok := s.batches[batchID]
	if !ok {
		return WorkflowAggregate{}, fmt.Errorf("%w: batch %s", ErrNotFound, batchID)
	}
	operation, ok := s.operations[batch.OperationID]
	if !ok {
		return WorkflowAggregate{}, fmt.Errorf("%w: operation %s", ErrNotFound, batch.OperationID)
	}
	jobIDs := append([]string(nil), s.jobIDsByBatch[batchID]...)
	jobs := make([]GenerationJob, len(jobIDs))
	jobPtrs := make([]*GenerationJob, len(jobIDs))
	for i, id := range jobIDs {
		jobs[i] = cloneJob(s.jobs[id])
		jobPtrs[i] = &jobs[i]
	}
	beforeBatch := cloneBatch(batch)
	beforeOperation := cloneOperation(operation)
	beforeJobs := make([]GenerationJob, len(jobs))
	for i := range jobs {
		beforeJobs[i] = cloneJob(jobs[i])
	}
	events, err := transaction(&operation, &batch, jobPtrs)
	if err != nil {
		return WorkflowAggregate{}, err
	}
	for i := range jobs {
		events = ensureJobLifecycleEvent(events, beforeJobs[i], jobs[i], false)
	}
	if beforeBatch.Status != batch.Status {
		if err := ValidateBatchTransition(beforeBatch.Status, batch.Status); err != nil {
			return WorkflowAggregate{}, err
		}
	}
	if beforeOperation.Status != operation.Status {
		if err := ValidateOperationTransition(beforeOperation.Status, operation.Status); err != nil {
			return WorkflowAggregate{}, err
		}
	}
	for i := range jobs {
		if beforeJobs[i].Status != jobs[i].Status {
			if err := ValidateJobTransition(beforeJobs[i].Status, jobs[i].Status); err != nil {
				return WorkflowAggregate{}, err
			}
		}
	}
	now := s.clock()
	if !equalJSON(beforeBatch, batch) {
		batch.Version++
		batch.UpdatedAt = now
		if IsTerminalBatchStatus(batch.Status) && batch.TerminalAt == nil {
			batch.TerminalAt = &now
		}
	}
	if !equalJSON(beforeOperation, operation) {
		operation.Version++
		operation.UpdatedAt = now
		if IsTerminalOperationStatus(operation.Status) && operation.TerminalAt == nil {
			operation.TerminalAt = &now
		}
	}
	for i := range jobs {
		if equalJSON(beforeJobs[i], jobs[i]) {
			continue
		}
		jobs[i].StatusVersion++
		jobs[i].UpdatedAt = now
		if IsTerminalJobStatus(jobs[i].Status) && jobs[i].TerminalAt == nil {
			jobs[i].TerminalAt = &now
		}
	}
	for i := range events {
		fillWorkflowEvent(&events[i], operation, batch, jobs)
	}
	if err := s.appendOutboxBatchLocked(events); err != nil {
		return WorkflowAggregate{}, err
	}
	s.operations[operation.ID] = cloneOperation(operation)
	s.batches[batch.ID] = cloneBatch(batch)
	for i, id := range jobIDs {
		s.jobs[id] = cloneJob(jobs[i])
	}
	return s.aggregateLocked(batch.ID), nil
}

func (s *MemoryStore) ListOutbox(_ context.Context, status string, limit int) ([]OutboxEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var events []OutboxEvent
	for _, event := range s.outbox {
		if status == "" || event.Status == status {
			events = append(events, cloneOutbox(event))
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if !events[i].AvailableAt.Equal(events[j].AvailableAt) {
			return events[i].AvailableAt.Before(events[j].AvailableAt)
		}
		if events[i].CreatedAt.Equal(events[j].CreatedAt) {
			return events[i].ID < events[j].ID
		}
		return events[i].CreatedAt.Before(events[j].CreatedAt)
	})
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (s *MemoryStore) MarkOutboxPublished(_ context.Context, eventID string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	event, ok := s.outbox[strings.TrimSpace(eventID)]
	if !ok {
		return fmt.Errorf("%w: outbox event %s", ErrNotFound, eventID)
	}
	if event.Status == OutboxPublished {
		return nil
	}
	if at.IsZero() {
		at = s.clock()
	}
	event.Status = OutboxPublished
	event.PublishedAt = &at
	s.outbox[event.ID] = event
	return nil
}

func (s *MemoryStore) MarkOutboxFailed(_ context.Context, eventID string, at time.Time, maxAttempts int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	event, ok := s.outbox[strings.TrimSpace(eventID)]
	if !ok {
		return fmt.Errorf("%w: outbox event %s", ErrNotFound, eventID)
	}
	if event.Status != OutboxPending {
		return nil
	}
	if at.IsZero() {
		at = s.clock()
	}
	event.Attempts++
	if maxAttempts > 0 && event.Attempts >= maxAttempts {
		event.Status = OutboxDead
	} else {
		event.AvailableAt = at.Add(retryBackoff(event.Attempts))
	}
	s.outbox[event.ID] = cloneOutbox(event)
	return nil
}

func (s *MemoryStore) appendOutboxLocked(event OutboxEvent) error {
	event.IdempotencyKey = strings.TrimSpace(event.IdempotencyKey)
	if event.IdempotencyKey == "" {
		return fmt.Errorf("outbox idempotency key is required")
	}
	if _, ok := s.outboxByKey[event.IdempotencyKey]; ok {
		return nil
	}
	if strings.TrimSpace(event.ID) == "" {
		event.ID = s.newID()
	}
	if existing, ok := s.outbox[event.ID]; ok {
		return fmt.Errorf("%w: outbox event %s already belongs to key %s", ErrDuplicate, event.ID, existing.IdempotencyKey)
	}
	if event.Status == "" {
		event.Status = OutboxPending
	}
	now := s.clock()
	event.CreatedAt = timeOr(event.CreatedAt, now)
	event.AvailableAt = timeOr(event.AvailableAt, now)
	s.outbox[event.ID] = cloneOutbox(event)
	s.outboxByKey[event.IdempotencyKey] = event.ID
	return nil
}

func (s *MemoryStore) appendOutboxBatchLocked(events []OutboxEvent) error {
	if len(events) == 0 {
		return nil
	}
	originalOutbox := s.outbox
	originalKeys := s.outboxByKey
	s.outbox = make(map[string]OutboxEvent, len(originalOutbox)+len(events))
	for id, event := range originalOutbox {
		s.outbox[id] = cloneOutbox(event)
	}
	s.outboxByKey = make(map[string]string, len(originalKeys)+len(events))
	for key, id := range originalKeys {
		s.outboxByKey[key] = id
	}
	for _, event := range events {
		if err := s.appendOutboxLocked(event); err != nil {
			s.outbox = originalOutbox
			s.outboxByKey = originalKeys
			return err
		}
	}
	return nil
}

func (s *MemoryStore) aggregateLocked(batchID string) WorkflowAggregate {
	batch := cloneBatch(s.batches[batchID])
	return WorkflowAggregate{
		Operation: cloneOperation(s.operations[batch.OperationID]),
		Batch:     batch,
		Jobs:      s.jobsByBatchLocked(batchID),
	}
}

func (s *MemoryStore) jobsByBatchLocked(batchID string) []GenerationJob {
	ids := s.jobIDsByBatch[strings.TrimSpace(batchID)]
	jobs := make([]GenerationJob, 0, len(ids))
	for _, id := range ids {
		jobs = append(jobs, cloneJob(s.jobs[id]))
	}
	return jobs
}

func newJobDispatchEvent(job GenerationJob) OutboxEvent {
	return OutboxEvent{
		IdempotencyKey: "job:" + job.ID + ":dispatch",
		EventType:      EventJobDispatch, Destination: DestinationMediaJobs,
		AggregateType: "job", AggregateID: job.ID, AggregateVersion: job.StatusVersion,
		SessionID: job.SessionID, WorkflowRunID: job.WorkflowRunID, StageRunID: job.StageRunID,
		OperationID: job.OperationID, ToolCallID: job.ToolCallID, BatchID: job.BatchID, JobID: job.ID,
		Payload: map[string]any{"job_id": job.ID, "idempotency_key": job.IdempotencyKey},
	}
}

// newBatchFinalizeRequestedEvent is committed in the same transaction as the
// job state that may unblock the Batch Barrier. The stable per-trigger key
// makes an Outbox replay safe while still allowing compensation settlement to
// request a second barrier pass after the terminal-job pass observed pending
// compensation.
func newBatchFinalizeRequestedEvent(job GenerationJob, trigger string) OutboxEvent {
	trigger = strings.TrimSpace(trigger)
	if trigger == "" {
		trigger = "state-change"
	}
	return OutboxEvent{
		IdempotencyKey: "job:" + job.ID + ":batch-finalize-requested:" + trigger,
		EventType:      EventBatchFinalizeRequested,
		Destination:    DestinationSessionSignal,
		Payload:        map[string]any{"batch_id": job.BatchID, "job_id": job.ID, "trigger": trigger},
	}
}

// ensureJobLifecycleEvent makes every public Worker state transition durable.
// The event is appended by the Store itself so callers cannot accidentally
// persist running/waiting/finalizing/retry state without its matching Outbox
// fact. Existing terminal job events are reused to avoid duplicate UI updates.
func ensureJobLifecycleEvent(events []OutboxEvent, before, after GenerationJob, implicitCurrentJob bool) []OutboxEvent {
	if before.Status == after.Status && before.Phase == after.Phase {
		return events
	}
	if !isPublicJobLifecycleStatus(after.Status) {
		return events
	}
	for _, event := range events {
		if event.Destination == DestinationSessionSignal && strings.HasPrefix(event.EventType, "job.") && (implicitCurrentJob || eventBelongsToJob(event, after.ID)) {
			return events
		}
	}
	return append(events, OutboxEvent{
		IdempotencyKey: fmt.Sprintf("job:%s:lifecycle:%d", after.ID, after.StatusVersion+1),
		EventType:      EventJobLifecycleChanged,
		Destination:    DestinationSessionSignal,
		AggregateType:  "job",
		AggregateID:    after.ID,
		JobID:          after.ID,
	})
}

func eventBelongsToJob(event OutboxEvent, jobID string) bool {
	jobID = strings.TrimSpace(jobID)
	if strings.TrimSpace(event.JobID) == jobID || (event.AggregateType == "job" && strings.TrimSpace(event.AggregateID) == jobID) {
		return true
	}
	value, _ := event.Payload["job_id"].(string)
	return strings.TrimSpace(value) == jobID
}

func isPublicJobLifecycleStatus(status string) bool {
	switch status {
	case StatusRunning, StatusWaitingProvider, StatusFinalizing, StatusRetryWait,
		StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

func fillJobEvent(event *OutboxEvent, job GenerationJob) {
	if event.AggregateType == "" {
		event.AggregateType = "job"
	}
	if event.AggregateID == "" {
		event.AggregateID = job.ID
	}
	if event.AggregateVersion == 0 {
		event.AggregateVersion = job.StatusVersion
	}
	event.SessionID = valueOrDefault(event.SessionID, job.SessionID)
	event.WorkflowRunID = valueOrDefault(event.WorkflowRunID, job.WorkflowRunID)
	event.StageRunID = valueOrDefault(event.StageRunID, job.StageRunID)
	event.OperationID = valueOrDefault(event.OperationID, job.OperationID)
	event.ToolCallID = valueOrDefault(event.ToolCallID, job.ToolCallID)
	event.BatchID = valueOrDefault(event.BatchID, job.BatchID)
	event.JobID = valueOrDefault(event.JobID, job.ID)
	if event.Destination == DestinationSessionSignal && strings.HasPrefix(event.EventType, "job.") {
		fillJobLifecyclePayload(event, job)
	}
}

func fillJobLifecyclePayload(event *OutboxEvent, job GenerationJob) {
	if event.Payload == nil {
		event.Payload = make(map[string]any)
	}
	event.Payload["job_id"] = job.ID
	event.Payload["operation_id"] = job.OperationID
	event.Payload["batch_id"] = job.BatchID
	event.Payload["session_id"] = job.SessionID
	event.Payload["workflow_run_id"] = job.WorkflowRunID
	event.Payload["stage_run_id"] = job.StageRunID
	event.Payload["tool_call_id"] = job.ToolCallID
	event.Payload["status"] = job.Status
	event.Payload["phase"] = job.Phase
	event.Payload["status_version"] = job.StatusVersion
	event.Payload["provider"] = job.Provider
	event.Payload["media_kind"] = job.MediaKind
	event.Payload["target_type"] = job.TargetType
	event.Payload["target_id"] = job.TargetID
	event.Payload["asset_slot"] = job.AssetSlot
	event.Payload["retry_count"] = job.RetryCount
	event.Payload["provider_poll_attempts"] = job.ProviderPollAttempts
	event.Payload["provider_status"] = job.ProviderStatus
	event.Payload["error_stage"] = job.ErrorStage
	event.Payload["error_code"] = job.ErrorCode
	event.Payload["error_message"] = job.ErrorMessage
	event.Payload["result_disposition"] = job.ResultDisposition
	if len(job.ResultAssetIDs) > 0 {
		event.Payload["result_asset_ids"] = append([]string(nil), job.ResultAssetIDs...)
	}
	if !job.NextRunAt.IsZero() {
		event.Payload["next_run_at"] = job.NextRunAt
	}
}

func fillBatchEvent(event *OutboxEvent, operation GenerationOperation, batch GenerationBatch) {
	if event.AggregateType == "" {
		event.AggregateType = "batch"
	}
	if event.AggregateID == "" {
		event.AggregateID = batch.ID
	}
	if event.AggregateVersion == 0 {
		event.AggregateVersion = batch.Version
	}
	event.SessionID = valueOrDefault(event.SessionID, batch.SessionID)
	event.WorkflowRunID = valueOrDefault(event.WorkflowRunID, batch.WorkflowRunID)
	event.StageRunID = valueOrDefault(event.StageRunID, batch.StageRunID)
	event.OperationID = valueOrDefault(event.OperationID, operation.ID)
	event.ToolCallID = valueOrDefault(event.ToolCallID, batch.ToolCallID)
	event.BatchID = valueOrDefault(event.BatchID, batch.ID)
}

func fillWorkflowEvent(event *OutboxEvent, operation GenerationOperation, batch GenerationBatch, jobs []GenerationJob) {
	jobID := strings.TrimSpace(event.JobID)
	if jobID == "" && event.AggregateType == "job" {
		jobID = strings.TrimSpace(event.AggregateID)
	}
	if jobID != "" {
		for _, job := range jobs {
			if job.ID == jobID {
				fillJobEvent(event, job)
				return
			}
		}
	}
	fillBatchEvent(event, operation, batch)
}

func normalizeVersion(version int) int {
	if version <= 0 {
		return 1
	}
	return version
}

func normalizeCompletionPolicy(policy string) string {
	switch policy {
	case CompletionAllRequired, CompletionAllowPartial, CompletionMinSuccess:
		return policy
	default:
		return CompletionAllRequired
	}
}

func normalizeWakePolicy(policy string) string {
	switch policy {
	case WakeOnTerminal, WakeOnFailure, WakeNever:
		return policy
	default:
		return WakeOnTerminal
	}
}

func deliveryPolicyOr(policy, fallback DeliveryPolicy) DeliveryPolicy {
	if policy.BindingMode == "" && policy.ApprovalPolicy == "" && policy.ChargePolicy == "" {
		return fallback.Normalize()
	}
	return policy.Normalize()
}

func timeOr(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}

func cloneOperation(value GenerationOperation) GenerationOperation {
	value.Result = cloneMap(value.Result)
	return value
}

func cloneBatch(value GenerationBatch) GenerationBatch {
	value.Cost.Breakdown = cloneInt64Map(value.Cost.Breakdown)
	value.Cost.BalanceAfter = cloneInt64Pointer(value.Cost.BalanceAfter)
	return value
}

func cloneJob(value GenerationJob) GenerationJob {
	value.Payload = cloneMap(value.Payload)
	value.Result = cloneMap(value.Result)
	value.ResultAssetIDs = append([]string(nil), value.ResultAssetIDs...)
	value.ProviderCostBreakdown = cloneInt64Map(value.ProviderCostBreakdown)
	value.SettlementBreakdown = cloneInt64Map(value.SettlementBreakdown)
	value.CostBreakdown = cloneInt64Map(value.CostBreakdown)
	value.BalanceAfter = cloneInt64Pointer(value.BalanceAfter)
	return value
}

func cloneOutbox(value OutboxEvent) OutboxEvent {
	value.Payload = cloneMap(value.Payload)
	return value
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		out := make(map[string]any, len(input))
		for key, value := range input {
			out[key] = value
		}
		return out
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func cloneInt64Map(input map[string]int64) map[string]int64 {
	if input == nil {
		return nil
	}
	out := make(map[string]int64, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneInt64Pointer(input *int64) *int64 {
	if input == nil {
		return nil
	}
	value := *input
	return &value
}

func equalJSON(left, right any) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return string(a) == string(b)
}

func jobRunnableAt(job GenerationJob, now time.Time) bool {
	if IsTerminalJobStatus(job.Status) {
		return false
	}
	switch job.Status {
	case StatusQueued:
		return job.NextRunAt.IsZero() || !job.NextRunAt.After(now)
	case StatusWaitingProvider, StatusRetryWait:
		return job.NextRunAt.IsZero() || !job.NextRunAt.After(now)
	case StatusRunning, StatusFinalizing:
		return job.LeaseUntil == nil || !job.LeaseUntil.After(now)
	default:
		return false
	}
}

func runnableTime(job GenerationJob) time.Time {
	if job.Status == StatusRunning || job.Status == StatusFinalizing {
		if job.LeaseUntil != nil {
			return *job.LeaseUntil
		}
	}
	if !job.NextRunAt.IsZero() {
		return job.NextRunAt
	}
	return job.CreatedAt
}

var _ WorkflowStore = (*MemoryStore)(nil)
