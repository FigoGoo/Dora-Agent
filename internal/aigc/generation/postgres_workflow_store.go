package generation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PostgresWorkflowStore is the production transaction boundary for the new
// Operation/Batch/Job/Outbox workflow. It intentionally uses a workflow job
// table separate from the legacy aigc_generation_jobs table so migration can
// be rolled out without breaking the existing PostgresStore/ListBySession API.
type PostgresWorkflowStore struct {
	db    *gorm.DB
	clock func() time.Time
	newID func() string
}

type PostgresWorkflowStoreOption func(*PostgresWorkflowStore)

func WithPostgresWorkflowClock(clock func() time.Time) PostgresWorkflowStoreOption {
	return func(store *PostgresWorkflowStore) {
		if clock != nil {
			store.clock = clock
		}
	}
}

func WithPostgresWorkflowIDGenerator(newID func() string) PostgresWorkflowStoreOption {
	return func(store *PostgresWorkflowStore) {
		if newID != nil {
			store.newID = newID
		}
	}
}

func NewPostgresWorkflowStore(db *gorm.DB, options ...PostgresWorkflowStoreOption) *PostgresWorkflowStore {
	store := &PostgresWorkflowStore{db: db, clock: time.Now, newID: defaultID}
	for _, option := range options {
		option(store)
	}
	return store
}

type workflowOperationRecord struct {
	ID             string `gorm:"primaryKey;size:128"`
	SessionID      string `gorm:"size:128;index"`
	IdempotencyKey string `gorm:"size:256;uniqueIndex"`
	BatchID        string `gorm:"size:128;uniqueIndex"`
	Status         string `gorm:"size:64;index"`
	Version        int
	Data           []byte `gorm:"type:jsonb;not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (workflowOperationRecord) TableName() string { return "aigc_generation_operations" }

type workflowBatchRecord struct {
	ID              string `gorm:"primaryKey;size:128"`
	SessionID       string `gorm:"size:128;index"`
	OperationID     string `gorm:"size:128;uniqueIndex"`
	StageRunID      string `gorm:"size:128;index"`
	Status          string `gorm:"size:64;index"`
	CancelRequested bool   `gorm:"index"`
	Version         int
	Data            []byte `gorm:"type:jsonb;not null"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (workflowBatchRecord) TableName() string { return "aigc_generation_batches" }

type workflowJobRecord struct {
	ID             string `gorm:"primaryKey;size:128"`
	BatchID        string `gorm:"size:128;index:idx_generation_workflow_job_batch"`
	OperationID    string `gorm:"size:128;index"`
	SessionID      string `gorm:"size:128;index"`
	IdempotencyKey string `gorm:"size:256;uniqueIndex"`
	Provider       string `gorm:"size:64;index"`
	Status         string `gorm:"size:64;index:idx_generation_workflow_job_due"`
	Phase          string `gorm:"size:64"`
	StatusVersion  int
	NextRunAt      time.Time  `gorm:"index:idx_generation_workflow_job_due"`
	LeaseUntil     *time.Time `gorm:"index"`
	Data           []byte     `gorm:"type:jsonb;not null"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (workflowJobRecord) TableName() string { return "aigc_generation_workflow_jobs" }

type workflowOutboxRecord struct {
	ID               string `gorm:"primaryKey;size:128"`
	IdempotencyKey   string `gorm:"size:384;uniqueIndex"`
	EventType        string `gorm:"size:128;index"`
	Destination      string `gorm:"size:128;index"`
	AggregateType    string `gorm:"size:64"`
	AggregateID      string `gorm:"size:128;index"`
	AggregateVersion int
	SessionID        string    `gorm:"size:128;index"`
	Status           string    `gorm:"size:32;index:idx_generation_workflow_outbox_due"`
	AvailableAt      time.Time `gorm:"index:idx_generation_workflow_outbox_due"`
	Attempts         int
	Data             []byte `gorm:"type:jsonb;not null"`
	CreatedAt        time.Time
	PublishedAt      *time.Time
}

func (workflowOutboxRecord) TableName() string { return "aigc_generation_outbox_events" }

// AutoMigrate is intentionally local to generation. Central migration wiring
// can call it without modifying the repository's central migration registry.
func (s *PostgresWorkflowStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres workflow store db is required")
	}
	if err := s.db.WithContext(ctx).AutoMigrate(
		&workflowOperationRecord{}, &workflowBatchRecord{}, &workflowJobRecord{}, &workflowOutboxRecord{},
	); err != nil {
		return fmt.Errorf("migrate generation workflow tables: %w", err)
	}
	return nil
}

func (s *PostgresWorkflowStore) CreateWorkflow(ctx context.Context, command CreateWorkflowCommand) (WorkflowAggregate, bool, error) {
	if s == nil || s.db == nil {
		return WorkflowAggregate{}, false, fmt.Errorf("postgres workflow store db is required")
	}
	key := strings.TrimSpace(command.Operation.IdempotencyKey)
	if key == "" {
		return WorkflowAggregate{}, false, fmt.Errorf("operation idempotency key is required")
	}
	if err := freezeWorkflowRequest(&command); err != nil {
		return WorkflowAggregate{}, false, err
	}
	var result WorkflowAggregate
	created := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing workflowOperationRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&existing, "idempotency_key = ?", key).Error
		if err == nil {
			var loadErr error
			result, loadErr = loadWorkflowAggregate(tx, existing.BatchID)
			if loadErr != nil {
				return loadErr
			}
			return validateWorkflowReplay(result.Operation, command.Operation)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find generation operation by idempotency key: %w", err)
		}

		normalized, events, err := normalizeWorkflowCommand(command, s.clock, s.newID)
		if err != nil {
			return err
		}
		operationRecord, err := encodeWorkflowOperation(normalized.Operation)
		if err != nil {
			return err
		}
		batchRecord, err := encodeWorkflowBatch(normalized.Batch)
		if err != nil {
			return err
		}
		if err := tx.Create(&operationRecord).Error; err != nil {
			return fmt.Errorf("create generation operation %s: %w", operationRecord.ID, err)
		}
		if err := tx.Create(&batchRecord).Error; err != nil {
			return fmt.Errorf("create generation batch %s: %w", batchRecord.ID, err)
		}
		for _, job := range normalized.Jobs {
			record, err := encodeWorkflowJob(job)
			if err != nil {
				return err
			}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("create generation workflow job %s: %w", record.ID, err)
			}
		}
		for _, event := range events {
			if err := appendWorkflowOutbox(tx, event, s.clock, s.newID); err != nil {
				return err
			}
		}
		result = normalized
		created = true
		return nil
	})
	if err != nil {
		// Concurrent creators may both observe no row before one wins the
		// unique idempotency key. Resolve that race by returning the winner.
		var existing workflowOperationRecord
		if lookupErr := s.db.WithContext(ctx).First(&existing, "idempotency_key = ?", key).Error; lookupErr == nil {
			aggregate, loadErr := loadWorkflowAggregate(s.db.WithContext(ctx), existing.BatchID)
			if loadErr == nil {
				if replayErr := validateWorkflowReplay(aggregate.Operation, command.Operation); replayErr != nil {
					return WorkflowAggregate{}, false, replayErr
				}
				return aggregate, false, nil
			}
		}
		return WorkflowAggregate{}, false, err
	}
	return result, created, nil
}

func (s *PostgresWorkflowStore) GetOperation(ctx context.Context, operationID string) (GenerationOperation, error) {
	if s == nil || s.db == nil {
		return GenerationOperation{}, fmt.Errorf("postgres workflow store db is required")
	}
	var record workflowOperationRecord
	if err := s.db.WithContext(ctx).First(&record, "id = ?", strings.TrimSpace(operationID)).Error; err != nil {
		return GenerationOperation{}, workflowRecordError(err, "operation", operationID)
	}
	return decodeWorkflowOperation(record)
}

func (s *PostgresWorkflowStore) GetOperationByIdempotencyKey(ctx context.Context, key string) (GenerationOperation, error) {
	if s == nil || s.db == nil {
		return GenerationOperation{}, fmt.Errorf("postgres workflow store db is required")
	}
	var record workflowOperationRecord
	if err := s.db.WithContext(ctx).First(&record, "idempotency_key = ?", strings.TrimSpace(key)).Error; err != nil {
		return GenerationOperation{}, workflowRecordError(err, "operation idempotency key", key)
	}
	return decodeWorkflowOperation(record)
}

func (s *PostgresWorkflowStore) GetBatch(ctx context.Context, batchID string) (GenerationBatch, error) {
	if s == nil || s.db == nil {
		return GenerationBatch{}, fmt.Errorf("postgres workflow store db is required")
	}
	var record workflowBatchRecord
	if err := s.db.WithContext(ctx).First(&record, "id = ?", strings.TrimSpace(batchID)).Error; err != nil {
		return GenerationBatch{}, workflowRecordError(err, "batch", batchID)
	}
	return decodeWorkflowBatch(record)
}

func (s *PostgresWorkflowStore) GetJob(ctx context.Context, jobID string) (GenerationJob, error) {
	if s == nil || s.db == nil {
		return GenerationJob{}, fmt.Errorf("postgres workflow store db is required")
	}
	var record workflowJobRecord
	if err := s.db.WithContext(ctx).First(&record, "id = ?", strings.TrimSpace(jobID)).Error; err != nil {
		return GenerationJob{}, workflowRecordError(err, "job", jobID)
	}
	return decodeWorkflowJob(record)
}

func (s *PostgresWorkflowStore) GetJobByIdempotencyKey(ctx context.Context, key string) (GenerationJob, error) {
	if s == nil || s.db == nil {
		return GenerationJob{}, fmt.Errorf("postgres workflow store db is required")
	}
	var record workflowJobRecord
	if err := s.db.WithContext(ctx).First(&record, "idempotency_key = ?", strings.TrimSpace(key)).Error; err != nil {
		return GenerationJob{}, workflowRecordError(err, "job idempotency key", key)
	}
	return decodeWorkflowJob(record)
}

// ListBySession mirrors the legacy PostgresStore query shape for callers that
// migrate to workflow jobs without changing their read path all at once.
func (s *PostgresWorkflowStore) ListBySession(ctx context.Context, sessionID string) ([]GenerationJob, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres workflow store db is required")
	}
	var records []workflowJobRecord
	if err := s.db.WithContext(ctx).Where("session_id = ?", strings.TrimSpace(sessionID)).Order("updated_at DESC, id").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list generation workflow jobs for session %s: %w", sessionID, err)
	}
	jobs := make([]GenerationJob, 0, len(records))
	for _, record := range records {
		job, err := decodeWorkflowJob(record)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *PostgresWorkflowStore) ListJobsByBatch(ctx context.Context, batchID string) ([]GenerationJob, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres workflow store db is required")
	}
	var records []workflowJobRecord
	if err := s.db.WithContext(ctx).Where("batch_id = ?", strings.TrimSpace(batchID)).Order("created_at, id").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list generation workflow jobs for batch %s: %w", batchID, err)
	}
	jobs := make([]GenerationJob, 0, len(records))
	for _, record := range records {
		job, err := decodeWorkflowJob(record)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *PostgresWorkflowStore) ListRunnableJobs(ctx context.Context, now time.Time, limit int) ([]GenerationJob, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres workflow store db is required")
	}
	if now.IsZero() {
		now = s.clock()
	}
	var records []workflowJobRecord
	dueStatuses := []string{StatusQueued, StatusWaitingProvider, StatusRetryWait}
	leasedStatuses := []string{StatusRunning, StatusFinalizing}
	query := s.db.WithContext(ctx).
		Where(
			"(status IN ? AND next_run_at <= ?) OR (status IN ? AND (lease_until IS NULL OR lease_until <= ?))",
			dueStatuses, now, leasedStatuses, now,
		).
		Order("next_run_at, updated_at, id")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list runnable generation jobs: %w", err)
	}
	jobs := make([]GenerationJob, 0, len(records))
	for _, record := range records {
		job, err := decodeWorkflowJob(record)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *PostgresWorkflowStore) RenewJobLease(ctx context.Context, jobID, leaseOwner string, leaseUntil time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres workflow store db is required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record workflowJobRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, "id = ?", strings.TrimSpace(jobID)).Error; err != nil {
			return err
		}
		job, err := decodeWorkflowJob(record)
		if err != nil {
			return err
		}
		if job.LeaseOwner != strings.TrimSpace(leaseOwner) || (job.Status != StatusRunning && job.Status != StatusFinalizing) {
			return fmt.Errorf("%w: generation job lease is not owned by %s", ErrVersionConflict, leaseOwner)
		}
		until := leaseUntil
		job.LeaseUntil = &until
		job.UpdatedAt = s.clock()
		updated, err := encodeWorkflowJob(job)
		if err != nil {
			return err
		}
		result := tx.Model(&workflowJobRecord{}).Where("id = ? AND status_version = ?", job.ID, job.StatusVersion).Updates(map[string]any{"lease_until": until, "data": updated.Data, "updated_at": job.UpdatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: renew job lease", ErrVersionConflict)
		}
		return nil
	})
}

func (s *PostgresWorkflowStore) MutateJob(ctx context.Context, jobID string, expectedVersion int, mutation JobMutation) (GenerationJob, error) {
	if s == nil || s.db == nil {
		return GenerationJob{}, fmt.Errorf("postgres workflow store db is required")
	}
	if mutation == nil {
		return GenerationJob{}, fmt.Errorf("job mutation is required")
	}
	var result GenerationJob
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record workflowJobRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, "id = ?", strings.TrimSpace(jobID)).Error; err != nil {
			return workflowRecordError(err, "job", jobID)
		}
		job, err := decodeWorkflowJob(record)
		if err != nil {
			return err
		}
		if expectedVersion > 0 && job.StatusVersion != expectedVersion {
			return fmt.Errorf("%w: job %s expected %d got %d", ErrVersionConflict, job.ID, expectedVersion, job.StatusVersion)
		}
		before := cloneJob(job)
		events, err := mutation(&job)
		if err != nil {
			return err
		}
		events = ensureJobLifecycleEvent(events, before, job, true)
		if before.Status != job.Status {
			if err := ValidateJobTransition(before.Status, job.Status); err != nil {
				return err
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
		updated, err := encodeWorkflowJob(job)
		if err != nil {
			return err
		}
		updateResult := tx.Model(&workflowJobRecord{}).Where("id = ? AND status_version = ?", job.ID, before.StatusVersion).Updates(map[string]any{
			"status": updated.Status, "phase": updated.Phase, "status_version": updated.StatusVersion,
			"next_run_at": updated.NextRunAt, "lease_until": updated.LeaseUntil, "data": updated.Data, "updated_at": updated.UpdatedAt,
		})
		if updateResult.Error != nil {
			return fmt.Errorf("update generation workflow job %s: %w", job.ID, updateResult.Error)
		}
		if updateResult.RowsAffected != 1 {
			return fmt.Errorf("%w: generation workflow job %s", ErrVersionConflict, job.ID)
		}
		for i := range events {
			fillJobEvent(&events[i], job)
			if err := appendWorkflowOutbox(tx, events[i], s.clock, s.newID); err != nil {
				return err
			}
		}
		result = job
		return nil
	})
	if err != nil {
		return GenerationJob{}, err
	}
	return result, nil
}

func (s *PostgresWorkflowStore) TransactBatch(ctx context.Context, batchID string, transaction BatchTransaction) (WorkflowAggregate, error) {
	if s == nil || s.db == nil {
		return WorkflowAggregate{}, fmt.Errorf("postgres workflow store db is required")
	}
	if transaction == nil {
		return WorkflowAggregate{}, fmt.Errorf("batch transaction is required")
	}
	var result WorkflowAggregate
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var batchRecord workflowBatchRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&batchRecord, "id = ?", strings.TrimSpace(batchID)).Error; err != nil {
			return workflowRecordError(err, "batch", batchID)
		}
		batch, err := decodeWorkflowBatch(batchRecord)
		if err != nil {
			return err
		}
		var operationRecord workflowOperationRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&operationRecord, "id = ?", batch.OperationID).Error; err != nil {
			return workflowRecordError(err, "operation", batch.OperationID)
		}
		operation, err := decodeWorkflowOperation(operationRecord)
		if err != nil {
			return err
		}
		var jobRecords []workflowJobRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("batch_id = ?", batch.ID).Order("created_at, id").Find(&jobRecords).Error; err != nil {
			return fmt.Errorf("lock generation workflow jobs for batch %s: %w", batch.ID, err)
		}
		jobs := make([]GenerationJob, len(jobRecords))
		jobPtrs := make([]*GenerationJob, len(jobRecords))
		for i, record := range jobRecords {
			jobs[i], err = decodeWorkflowJob(record)
			if err != nil {
				return err
			}
			jobPtrs[i] = &jobs[i]
		}
		beforeOperation := cloneOperation(operation)
		beforeBatch := cloneBatch(batch)
		beforeJobs := make([]GenerationJob, len(jobs))
		for i := range jobs {
			beforeJobs[i] = cloneJob(jobs[i])
		}
		events, err := transaction(&operation, &batch, jobPtrs)
		if err != nil {
			return err
		}
		for i := range jobs {
			events = ensureJobLifecycleEvent(events, beforeJobs[i], jobs[i], false)
		}
		if beforeBatch.Status != batch.Status {
			if err := ValidateBatchTransition(beforeBatch.Status, batch.Status); err != nil {
				return err
			}
		}
		if beforeOperation.Status != operation.Status {
			if err := ValidateOperationTransition(beforeOperation.Status, operation.Status); err != nil {
				return err
			}
		}
		for i := range jobs {
			if beforeJobs[i].Status != jobs[i].Status {
				if err := ValidateJobTransition(beforeJobs[i].Status, jobs[i].Status); err != nil {
					return err
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
		encodedOperation, err := encodeWorkflowOperation(operation)
		if err != nil {
			return err
		}
		encodedBatch, err := encodeWorkflowBatch(batch)
		if err != nil {
			return err
		}
		operationUpdate := tx.Model(&workflowOperationRecord{}).Where("id = ? AND version = ?", operation.ID, beforeOperation.Version).Updates(map[string]any{
			"status": encodedOperation.Status, "version": encodedOperation.Version, "data": encodedOperation.Data, "updated_at": encodedOperation.UpdatedAt,
		})
		if operationUpdate.Error != nil {
			return fmt.Errorf("update generation operation %s: %w", operation.ID, operationUpdate.Error)
		}
		if operationUpdate.RowsAffected != 1 {
			return fmt.Errorf("%w: generation operation %s", ErrVersionConflict, operation.ID)
		}
		batchUpdate := tx.Model(&workflowBatchRecord{}).Where("id = ? AND version = ?", batch.ID, beforeBatch.Version).Updates(map[string]any{
			"status": encodedBatch.Status, "cancel_requested": encodedBatch.CancelRequested, "version": encodedBatch.Version,
			"data": encodedBatch.Data, "updated_at": encodedBatch.UpdatedAt,
		})
		if batchUpdate.Error != nil {
			return fmt.Errorf("update generation batch %s: %w", batch.ID, batchUpdate.Error)
		}
		if batchUpdate.RowsAffected != 1 {
			return fmt.Errorf("%w: generation batch %s", ErrVersionConflict, batch.ID)
		}
		for i := range jobs {
			if equalJSON(beforeJobs[i], jobs[i]) {
				continue
			}
			encoded, err := encodeWorkflowJob(jobs[i])
			if err != nil {
				return err
			}
			jobUpdate := tx.Model(&workflowJobRecord{}).Where("id = ? AND status_version = ?", jobs[i].ID, beforeJobs[i].StatusVersion).Updates(map[string]any{
				"status": encoded.Status, "phase": encoded.Phase, "status_version": encoded.StatusVersion,
				"next_run_at": encoded.NextRunAt, "lease_until": encoded.LeaseUntil, "data": encoded.Data, "updated_at": encoded.UpdatedAt,
			})
			if jobUpdate.Error != nil {
				return fmt.Errorf("update generation workflow job %s: %w", jobs[i].ID, jobUpdate.Error)
			}
			if jobUpdate.RowsAffected != 1 {
				return fmt.Errorf("%w: generation workflow job %s", ErrVersionConflict, jobs[i].ID)
			}
		}
		for i := range events {
			fillWorkflowEvent(&events[i], operation, batch, jobs)
			if err := appendWorkflowOutbox(tx, events[i], s.clock, s.newID); err != nil {
				return err
			}
		}
		result = WorkflowAggregate{Operation: operation, Batch: batch, Jobs: jobs}
		return nil
	})
	if err != nil {
		return WorkflowAggregate{}, err
	}
	return result, nil
}

func (s *PostgresWorkflowStore) ListOutbox(ctx context.Context, status string, limit int) ([]OutboxEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres workflow store db is required")
	}
	query := s.db.WithContext(ctx).Order("available_at, created_at, id")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	var records []workflowOutboxRecord
	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list generation outbox: %w", err)
	}
	events := make([]OutboxEvent, 0, len(records))
	for _, record := range records {
		event, err := decodeWorkflowOutbox(record)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *PostgresWorkflowStore) MarkOutboxPublished(ctx context.Context, eventID string, at time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres workflow store db is required")
	}
	if at.IsZero() {
		at = s.clock()
	}
	result := s.db.WithContext(ctx).Model(&workflowOutboxRecord{}).
		Where("id = ? AND status <> ?", strings.TrimSpace(eventID), OutboxPublished).
		Updates(map[string]any{"status": OutboxPublished, "published_at": at})
	if result.Error != nil {
		return fmt.Errorf("mark generation outbox %s published: %w", eventID, result.Error)
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := s.db.WithContext(ctx).Model(&workflowOutboxRecord{}).Where("id = ?", strings.TrimSpace(eventID)).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return fmt.Errorf("%w: outbox event %s", ErrNotFound, eventID)
		}
	}
	return nil
}

func (s *PostgresWorkflowStore) MarkOutboxFailed(ctx context.Context, eventID string, at time.Time, maxAttempts int) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres workflow store db is required")
	}
	if at.IsZero() {
		at = s.clock()
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record workflowOutboxRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, "id = ?", strings.TrimSpace(eventID)).Error; err != nil {
			return workflowRecordError(err, "outbox event", eventID)
		}
		if record.Status != OutboxPending {
			return nil
		}
		record.Attempts++
		updates := map[string]any{"attempts": record.Attempts}
		if maxAttempts > 0 && record.Attempts >= maxAttempts {
			updates["status"] = OutboxDead
		} else {
			updates["available_at"] = at.Add(retryBackoff(record.Attempts))
		}
		return tx.Model(&workflowOutboxRecord{}).Where("id = ? AND status = ?", record.ID, OutboxPending).Updates(updates).Error
	})
}

func normalizeWorkflowCommand(command CreateWorkflowCommand, clock func() time.Time, newID func() string) (WorkflowAggregate, []OutboxEvent, error) {
	temporary := NewMemoryStore(WithMemoryClock(clock), WithMemoryIDGenerator(newID))
	aggregate, _, err := temporary.CreateWorkflow(context.Background(), command)
	if err != nil {
		return WorkflowAggregate{}, nil, err
	}
	events, err := temporary.ListOutbox(context.Background(), OutboxPending, 0)
	return aggregate, events, err
}

func loadWorkflowAggregate(tx *gorm.DB, batchID string) (WorkflowAggregate, error) {
	var batchRecord workflowBatchRecord
	if err := tx.First(&batchRecord, "id = ?", batchID).Error; err != nil {
		return WorkflowAggregate{}, workflowRecordError(err, "batch", batchID)
	}
	batch, err := decodeWorkflowBatch(batchRecord)
	if err != nil {
		return WorkflowAggregate{}, err
	}
	var operationRecord workflowOperationRecord
	if err := tx.First(&operationRecord, "id = ?", batch.OperationID).Error; err != nil {
		return WorkflowAggregate{}, workflowRecordError(err, "operation", batch.OperationID)
	}
	operation, err := decodeWorkflowOperation(operationRecord)
	if err != nil {
		return WorkflowAggregate{}, err
	}
	var jobRecords []workflowJobRecord
	if err := tx.Where("batch_id = ?", batch.ID).Order("created_at, id").Find(&jobRecords).Error; err != nil {
		return WorkflowAggregate{}, err
	}
	jobs := make([]GenerationJob, 0, len(jobRecords))
	for _, record := range jobRecords {
		job, err := decodeWorkflowJob(record)
		if err != nil {
			return WorkflowAggregate{}, err
		}
		jobs = append(jobs, job)
	}
	return WorkflowAggregate{Operation: operation, Batch: batch, Jobs: jobs}, nil
}

func appendWorkflowOutbox(tx *gorm.DB, event OutboxEvent, clock func() time.Time, newID func() string) error {
	event.IdempotencyKey = strings.TrimSpace(event.IdempotencyKey)
	if event.IdempotencyKey == "" {
		return fmt.Errorf("outbox idempotency key is required")
	}
	if event.ID == "" {
		event.ID = newID()
	}
	now := clock()
	if event.Status == "" {
		event.Status = OutboxPending
	}
	event.CreatedAt = timeOr(event.CreatedAt, now)
	event.AvailableAt = timeOr(event.AvailableAt, now)
	record, err := encodeWorkflowOutbox(event)
	if err != nil {
		return err
	}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "idempotency_key"}}, DoNothing: true}).Create(&record).Error; err != nil {
		return fmt.Errorf("append generation outbox %s: %w", event.IdempotencyKey, err)
	}
	return nil
}

func encodeWorkflowOperation(value GenerationOperation) (workflowOperationRecord, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return workflowOperationRecord{}, fmt.Errorf("marshal generation operation: %w", err)
	}
	return workflowOperationRecord{ID: value.ID, SessionID: value.SessionID, IdempotencyKey: value.IdempotencyKey, BatchID: value.BatchID, Status: value.Status, Version: value.Version, Data: raw, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}, nil
}

func decodeWorkflowOperation(record workflowOperationRecord) (GenerationOperation, error) {
	var value GenerationOperation
	if err := decodeGenerationJSON(record.Data, &value); err != nil {
		return GenerationOperation{}, fmt.Errorf("unmarshal generation operation %s: %w", record.ID, err)
	}
	return value, nil
}

func encodeWorkflowBatch(value GenerationBatch) (workflowBatchRecord, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return workflowBatchRecord{}, fmt.Errorf("marshal generation batch: %w", err)
	}
	return workflowBatchRecord{ID: value.ID, SessionID: value.SessionID, OperationID: value.OperationID, StageRunID: value.StageRunID, Status: value.Status, CancelRequested: value.CancelRequested, Version: value.Version, Data: raw, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}, nil
}

func decodeWorkflowBatch(record workflowBatchRecord) (GenerationBatch, error) {
	var value GenerationBatch
	if err := decodeGenerationJSON(record.Data, &value); err != nil {
		return GenerationBatch{}, fmt.Errorf("unmarshal generation batch %s: %w", record.ID, err)
	}
	return value, nil
}

func encodeWorkflowJob(value GenerationJob) (workflowJobRecord, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return workflowJobRecord{}, fmt.Errorf("marshal generation workflow job: %w", err)
	}
	return workflowJobRecord{ID: value.ID, BatchID: value.BatchID, OperationID: value.OperationID, SessionID: value.SessionID, IdempotencyKey: value.IdempotencyKey, Provider: value.Provider, Status: value.Status, Phase: value.Phase, StatusVersion: value.StatusVersion, NextRunAt: value.NextRunAt, LeaseUntil: value.LeaseUntil, Data: raw, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt}, nil
}

func decodeWorkflowJob(record workflowJobRecord) (GenerationJob, error) {
	var value GenerationJob
	if err := decodeGenerationJSON(record.Data, &value); err != nil {
		return GenerationJob{}, fmt.Errorf("unmarshal generation workflow job %s: %w", record.ID, err)
	}
	return value, nil
}

func encodeWorkflowOutbox(value OutboxEvent) (workflowOutboxRecord, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return workflowOutboxRecord{}, fmt.Errorf("marshal generation outbox: %w", err)
	}
	return workflowOutboxRecord{ID: value.ID, IdempotencyKey: value.IdempotencyKey, EventType: value.EventType, Destination: value.Destination, AggregateType: value.AggregateType, AggregateID: value.AggregateID, AggregateVersion: value.AggregateVersion, SessionID: value.SessionID, Status: value.Status, AvailableAt: value.AvailableAt, Attempts: value.Attempts, Data: raw, CreatedAt: value.CreatedAt, PublishedAt: value.PublishedAt}, nil
}

func decodeWorkflowOutbox(record workflowOutboxRecord) (OutboxEvent, error) {
	var value OutboxEvent
	if err := decodeGenerationJSON(record.Data, &value); err != nil {
		return OutboxEvent{}, fmt.Errorf("unmarshal generation outbox %s: %w", record.ID, err)
	}
	value.Status = record.Status
	value.Attempts = record.Attempts
	value.AvailableAt = record.AvailableAt
	value.PublishedAt = record.PublishedAt
	return value, nil
}

func workflowRecordError(err error, kind, id string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %s %s", ErrNotFound, kind, id)
	}
	return fmt.Errorf("get generation %s %s: %w", kind, id, err)
}

var _ WorkflowStore = (*PostgresWorkflowStore)(nil)
