package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStore struct {
	db    *gorm.DB
	clock func() time.Time
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db, clock: time.Now}
}

func (s *PostgresStore) WithTx(tx *gorm.DB) *PostgresStore {
	if s == nil {
		return &PostgresStore{db: tx, clock: time.Now}
	}
	return &PostgresStore{db: tx, clock: s.clock}
}

func (s *PostgresStore) DB() *gorm.DB {
	if s == nil {
		return nil
	}
	return s.db
}

func NewPostgresStoreWithClock(db *gorm.DB, clock func() time.Time) *PostgresStore {
	if clock == nil {
		clock = time.Now
	}
	return &PostgresStore{db: db, clock: clock}
}

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres approval store db is required")
	}
	return Migrate(ctx, s.db)
}

type approvalRecord struct {
	ID                  string        `gorm:"primaryKey;size:128"`
	IdempotencyKey      string        `gorm:"size:256;uniqueIndex"`
	TenantID            string        `gorm:"size:128"`
	UserID              string        `gorm:"size:128"`
	SessionID           string        `gorm:"size:128;index"`
	ArtifactType        string        `gorm:"size:128"`
	Binding             []byte        `gorm:"column:binding_json;type:jsonb"`
	ReviewMode          ReviewMode    `gorm:"size:32"`
	ExecutionMode       ExecutionMode `gorm:"size:32"`
	ExecutionEpoch      int64
	Status              Status `gorm:"size:32;index"`
	DecisionVersion     int
	ApproveCommand      []byte `gorm:"column:approve_command_json;type:jsonb"`
	RejectCommand       []byte `gorm:"column:reject_command_json;type:jsonb"`
	CheckpointMappingID string `gorm:"size:256"`
	MappingEpoch        int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
	ExpiresAt           *time.Time
	DecidedAt           *time.Time
}

func (approvalRecord) TableName() string { return "aigc_approvals" }

type approvalDecisionRecord struct {
	ApprovalID            string `gorm:"primaryKey;size:128"`
	DecisionVersion       int    `gorm:"primaryKey;autoIncrement:false"`
	IdempotencyKey        string `gorm:"size:256;uniqueIndex"`
	RequestedDecision     string `gorm:"size:32"`
	EffectiveStatus       Status `gorm:"size:32"`
	ActorID               string `gorm:"size:128"`
	Reason                string `gorm:"type:text"`
	ObservedBinding       []byte `gorm:"column:observed_binding_json;type:jsonb"`
	CommandKind           string `gorm:"size:128"`
	CommandIdempotencyKey string `gorm:"size:256"`
	CommandPayload        []byte `gorm:"column:command_payload_json;type:jsonb"`
	CreatedAt             time.Time
}

func (approvalDecisionRecord) TableName() string { return "aigc_approval_decisions" }

type candidateApprovalBatchRecord struct {
	ID                        string `gorm:"primaryKey;size:128"`
	IdempotencyKey            string `gorm:"size:256;uniqueIndex"`
	SessionID                 string `gorm:"size:128;index"`
	StoryboardID              string `gorm:"size:128;index"`
	ExpectedStoryboardVersion int
	Decision                  string `gorm:"size:32"`
	ActorID                   string `gorm:"size:128"`
	Reason                    string `gorm:"type:text"`
	Targets                   []byte `gorm:"column:targets_json;type:jsonb"`
	CreatedAt                 time.Time
}

func (candidateApprovalBatchRecord) TableName() string { return "aigc_candidate_approval_batches" }

func (s *PostgresStore) Create(ctx context.Context, requested Approval) (CreateResult, error) {
	if s == nil || s.db == nil {
		return CreateResult{}, fmt.Errorf("postgres approval store db is required")
	}
	normalized, err := normalizeApproval(requested, s.clock())
	if err != nil {
		return CreateResult{}, err
	}
	record, err := approvalToRecord(normalized)
	if err != nil {
		return CreateResult{}, err
	}
	var result CreateResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, found, err := findApprovalByIDOrKey(tx, normalized.ID, normalized.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if !sameApproval(existing, normalized) {
				return fmt.Errorf("%w: approval id or create key", ErrIdempotencyConflict)
			}
			result = CreateResult{Approval: existing}
			return nil
		}
		if err := tx.Create(&record).Error; err != nil {
			return fmt.Errorf("create approval %s: %w", normalized.ID, err)
		}
		result = CreateResult{Approval: normalized, Created: true}
		return nil
	})
	if err != nil {
		return CreateResult{}, err
	}
	result.Approval = cloneApproval(result.Approval)
	return result, nil
}

func (s *PostgresStore) Get(ctx context.Context, approvalID string) (Approval, error) {
	if s == nil || s.db == nil {
		return Approval{}, fmt.Errorf("postgres approval store db is required")
	}
	var record approvalRecord
	err := s.db.WithContext(ctx).First(&record, "id = ?", strings.TrimSpace(approvalID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Approval{}, fmt.Errorf("%w: %s", ErrNotFound, approvalID)
	}
	if err != nil {
		return Approval{}, fmt.Errorf("get approval %s: %w", approvalID, err)
	}
	return recordToApproval(record)
}

func (s *PostgresStore) GetDecision(ctx context.Context, approvalID string, decisionVersion int) (ApprovalDecision, error) {
	if s == nil || s.db == nil {
		return ApprovalDecision{}, fmt.Errorf("postgres approval store db is required")
	}
	var record approvalDecisionRecord
	err := s.db.WithContext(ctx).First(&record, "approval_id = ? AND decision_version = ?", strings.TrimSpace(approvalID), decisionVersion).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ApprovalDecision{}, fmt.Errorf("%w: decision %s/%d", ErrNotFound, approvalID, decisionVersion)
	}
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("get approval decision: %w", err)
	}
	return recordToDecision(record)
}

func (s *PostgresStore) CreateCandidateApprovalBatch(ctx context.Context, requested CandidateApprovalBatch) (CandidateApprovalBatchCreateResult, error) {
	if s == nil || s.db == nil {
		return CandidateApprovalBatchCreateResult{}, fmt.Errorf("postgres approval store db is required")
	}
	normalized, err := normalizeCandidateApprovalBatch(requested, s.clock())
	if err != nil {
		return CandidateApprovalBatchCreateResult{}, err
	}
	record, err := candidateApprovalBatchToRecord(normalized)
	if err != nil {
		return CandidateApprovalBatchCreateResult{}, err
	}
	var result CandidateApprovalBatchCreateResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existingRecord candidateApprovalBatchRecord
		lookupErr := tx.Where("id = ? OR idempotency_key = ?", normalized.ID, normalized.IdempotencyKey).First(&existingRecord).Error
		if lookupErr == nil {
			existing, decodeErr := recordToCandidateApprovalBatch(existingRecord)
			if decodeErr != nil {
				return decodeErr
			}
			if !sameCandidateApprovalBatch(existing, normalized) {
				return fmt.Errorf("%w: candidate approval batch id or key", ErrIdempotencyConflict)
			}
			result.Batch = existing
			return nil
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return fmt.Errorf("lookup candidate approval batch: %w", lookupErr)
		}
		insert := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&record)
		if insert.Error != nil {
			return fmt.Errorf("create candidate approval batch: %w", insert.Error)
		}
		if insert.RowsAffected == 0 {
			if lookupErr := tx.Where("id = ? OR idempotency_key = ?", normalized.ID, normalized.IdempotencyKey).First(&existingRecord).Error; lookupErr != nil {
				return fmt.Errorf("recover concurrent candidate approval batch create: %w", lookupErr)
			}
			existing, decodeErr := recordToCandidateApprovalBatch(existingRecord)
			if decodeErr != nil {
				return decodeErr
			}
			if !sameCandidateApprovalBatch(existing, normalized) {
				return fmt.Errorf("%w: candidate approval batch concurrent create", ErrIdempotencyConflict)
			}
			result.Batch = existing
			return nil
		}
		result = CandidateApprovalBatchCreateResult{Batch: normalized, Created: true}
		return nil
	})
	if err != nil {
		return CandidateApprovalBatchCreateResult{}, err
	}
	result.Batch = cloneCandidateApprovalBatch(result.Batch)
	return result, nil
}

func (s *PostgresStore) GetCandidateApprovalBatchByKey(ctx context.Context, idempotencyKey string) (CandidateApprovalBatch, error) {
	if s == nil || s.db == nil {
		return CandidateApprovalBatch{}, fmt.Errorf("postgres approval store db is required")
	}
	key := strings.TrimSpace(idempotencyKey)
	var record candidateApprovalBatchRecord
	err := s.db.WithContext(ctx).First(&record, "idempotency_key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CandidateApprovalBatch{}, fmt.Errorf("%w: candidate approval batch key=%s", ErrNotFound, key)
	}
	if err != nil {
		return CandidateApprovalBatch{}, fmt.Errorf("get candidate approval batch: %w", err)
	}
	return recordToCandidateApprovalBatch(record)
}

func (s *PostgresStore) Decide(ctx context.Context, command DecideCommand) (DecisionResult, error) {
	request, err := decisionRequestFromCommand(command)
	if err != nil {
		return DecisionResult{}, err
	}
	return s.decide(ctx, request)
}

func (s *PostgresStore) Close(ctx context.Context, command CloseCommand) (DecisionResult, error) {
	request, err := decisionRequestFromClose(command)
	if err != nil {
		return DecisionResult{}, err
	}
	return s.decide(ctx, request)
}

func (s *PostgresStore) decide(ctx context.Context, request decisionRequest) (DecisionResult, error) {
	if s == nil || s.db == nil {
		return DecisionResult{}, fmt.Errorf("postgres approval store db is required")
	}
	var result DecisionResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		approval, err := lockApproval(tx, request.ApprovalID)
		if err != nil {
			return err
		}
		var existingRecord approvalDecisionRecord
		err = tx.First(&existingRecord, "idempotency_key = ?", request.IdempotencyKey).Error
		if err == nil {
			decision, err := recordToDecision(existingRecord)
			if err != nil {
				return err
			}
			if !sameDecisionRequest(decision, request) {
				return fmt.Errorf("%w: decision key=%s", ErrIdempotencyConflict, request.IdempotencyKey)
			}
			result, err = loadDecisionResult(tx, approval, decision, false)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("lookup approval decision key: %w", err)
		}

		updated, decision, continuation, outbox, err := prepareDecision(approval, request)
		if err != nil {
			return err
		}
		decisionRecord, err := decisionToRecord(decision)
		if err != nil {
			return err
		}
		update := tx.Model(&approvalRecord{}).
			Where("id = ? AND status = ? AND decision_version = ?", approval.ID, StatusPending, request.ExpectedDecisionVersion).
			Updates(map[string]any{
				"status": updated.Status, "decision_version": updated.DecisionVersion,
				"updated_at": updated.UpdatedAt, "decided_at": updated.DecidedAt,
			})
		if update.Error != nil {
			return fmt.Errorf("save approval decision state: %w", update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("%w: decision CAS", ErrVersionConflict)
		}
		if err := tx.Create(&decisionRecord).Error; err != nil {
			return fmt.Errorf("create approval decision: %w", err)
		}
		if err := tx.Create(&continuation).Error; err != nil {
			return fmt.Errorf("create approval continuation: %w", err)
		}
		if err := tx.Create(&outbox).Error; err != nil {
			return fmt.Errorf("create approval decision outbox: %w", err)
		}
		result = DecisionResult{
			Approval: updated, Decision: decision, Continuation: continuation,
			Outbox: outbox, Created: true,
		}
		return nil
	})
	if err != nil {
		return DecisionResult{}, err
	}
	return cloneDecisionResult(result), nil
}

func (s *PostgresStore) BindInterruptMapping(ctx context.Context, command MappingCommand) (Approval, error) {
	if s == nil || s.db == nil {
		return Approval{}, fmt.Errorf("postgres approval store db is required")
	}
	command.ApprovalID = strings.TrimSpace(command.ApprovalID)
	command.CheckpointMappingID = strings.TrimSpace(command.CheckpointMappingID)
	if command.ApprovalID == "" || command.CheckpointMappingID == "" || command.ExpectedExecutionEpoch <= 0 || command.MappingEpoch <= 0 {
		return Approval{}, fmt.Errorf("approval id, mapping id, and positive epochs are required")
	}
	var result Approval
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		approval, err := lockApproval(tx, command.ApprovalID)
		if err != nil {
			return err
		}
		if approval.ReviewMode != ReviewModeInterrupt || approval.ExecutionMode != ExecutionModeInterrupt || approval.Status != StatusPending {
			return fmt.Errorf("%w: mapping cannot be bound", ErrInvalidTransition)
		}
		if approval.ExecutionEpoch != command.ExpectedExecutionEpoch {
			return fmt.Errorf("%w: execution epoch", ErrFallbackFenced)
		}
		if approval.CheckpointMappingID != "" {
			if approval.CheckpointMappingID == command.CheckpointMappingID && approval.MappingEpoch == command.MappingEpoch {
				result = approval
				return nil
			}
			return fmt.Errorf("%w: mapping is already frozen", ErrFallbackFenced)
		}
		now := s.clock().UTC()
		update := tx.Model(&approvalRecord{}).
			Where("id = ? AND execution_mode = ? AND execution_epoch = ? AND status = ? AND checkpoint_mapping_id = ''", approval.ID, ExecutionModeInterrupt, command.ExpectedExecutionEpoch, StatusPending).
			Updates(map[string]any{"checkpoint_mapping_id": command.CheckpointMappingID, "mapping_epoch": command.MappingEpoch, "updated_at": now})
		if update.Error != nil || update.RowsAffected != 1 {
			return fmt.Errorf("%w: bind mapping CAS", ErrFallbackFenced)
		}
		approval.CheckpointMappingID = command.CheckpointMappingID
		approval.MappingEpoch = command.MappingEpoch
		approval.UpdatedAt = now
		result = approval
		return nil
	})
	if err != nil {
		return Approval{}, err
	}
	return cloneApproval(result), nil
}

func (s *PostgresStore) SwitchToDurableFallback(ctx context.Context, command FallbackCommand) (FallbackResult, error) {
	if s == nil || s.db == nil {
		return FallbackResult{}, fmt.Errorf("postgres approval store db is required")
	}
	command.ApprovalID = strings.TrimSpace(command.ApprovalID)
	if command.ApprovalID == "" || command.ExpectedExecutionEpoch <= 0 || command.ExpectedDecisionVersion < 0 {
		return FallbackResult{}, fmt.Errorf("approval id, positive execution epoch, and decision version are required")
	}
	if command.ExpectedExecutionMode == "" {
		command.ExpectedExecutionMode = ExecutionModeInterrupt
	}
	if command.Now.IsZero() {
		command.Now = s.clock().UTC()
	} else {
		command.Now = command.Now.UTC()
	}
	var result FallbackResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		approval, err := lockApproval(tx, command.ApprovalID)
		if err != nil {
			return err
		}
		if approval.ReviewMode != ReviewModeInterrupt {
			return ErrReviewModeImmutable
		}
		if approval.ExecutionMode == ExecutionModeDurableFallback && approval.ExecutionEpoch == command.ExpectedExecutionEpoch+1 && approval.DecisionVersion == command.ExpectedDecisionVersion {
			result, err = loadFallbackResult(tx, approval, false)
			return err
		}
		if approval.ExecutionMode != command.ExpectedExecutionMode || approval.ExecutionEpoch != command.ExpectedExecutionEpoch || approval.DecisionVersion != command.ExpectedDecisionVersion {
			return fmt.Errorf("%w: mode/epoch/version", ErrFallbackFenced)
		}

		approval.ExecutionMode = ExecutionModeDurableFallback
		approval.ExecutionEpoch++
		approval.UpdatedAt = command.Now
		var continuation *sessionruntime.ApprovalContinuation
		if approval.DecisionVersion > 0 {
			var current sessionruntime.ApprovalContinuation
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current,
				"approval_id = ? AND decision_version = ?", approval.ID, approval.DecisionVersion).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: continuation", ErrNotFound)
			}
			if err != nil {
				return fmt.Errorf("lock approval continuation: %w", err)
			}
			if current.Status == sessionruntime.ContinuationStatusApplied {
				return fmt.Errorf("%w: continuation is applied", ErrFallbackFenced)
			}
			if current.Status == sessionruntime.ContinuationStatusClaimed && current.LeaseUntil != nil && current.LeaseUntil.After(command.Now) {
				return ErrContinuationBusy
			}
			current.Executor = sessionruntime.ContinuationExecutorDeterministic
			current.ExecutionEpoch = approval.ExecutionEpoch
			current.Status = sessionruntime.ContinuationStatusRequested
			current.LeaseOwner, current.LeaseUntil = "", nil
			current.ErrorCode, current.ErrorMessage = "", ""
			current.UpdatedAt = command.Now
			if err := tx.Save(&current).Error; err != nil {
				return fmt.Errorf("switch approval continuation to fallback: %w", err)
			}
			continuation = &current
		}
		update := tx.Model(&approvalRecord{}).
			Where("id = ? AND execution_mode = ? AND execution_epoch = ? AND decision_version = ?", approval.ID, command.ExpectedExecutionMode, command.ExpectedExecutionEpoch, command.ExpectedDecisionVersion).
			Updates(map[string]any{"execution_mode": approval.ExecutionMode, "execution_epoch": approval.ExecutionEpoch, "updated_at": approval.UpdatedAt})
		if update.Error != nil || update.RowsAffected != 1 {
			return fmt.Errorf("%w: fallback CAS", ErrFallbackFenced)
		}
		outbox, err := newFallbackOutbox(approval, continuation, command.Now)
		if err != nil {
			return err
		}
		if err := tx.Create(&outbox).Error; err != nil {
			return fmt.Errorf("create fallback outbox: %w", err)
		}
		result = FallbackResult{Approval: approval, Continuation: continuation, Outbox: outbox, Switched: true}
		return nil
	})
	if err != nil {
		return FallbackResult{}, err
	}
	return cloneFallbackResult(result), nil
}

func (s *PostgresStore) GetContinuation(ctx context.Context, approvalID string, decisionVersion int) (sessionruntime.ApprovalContinuation, error) {
	if s == nil || s.db == nil {
		return sessionruntime.ApprovalContinuation{}, fmt.Errorf("postgres approval store db is required")
	}
	var continuation sessionruntime.ApprovalContinuation
	err := s.db.WithContext(ctx).First(&continuation, "approval_id = ? AND decision_version = ?", strings.TrimSpace(approvalID), decisionVersion).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return sessionruntime.ApprovalContinuation{}, fmt.Errorf("%w: continuation %s/%d", ErrNotFound, approvalID, decisionVersion)
	}
	if err != nil {
		return sessionruntime.ApprovalContinuation{}, fmt.Errorf("get approval continuation: %w", err)
	}
	return continuation, nil
}

func (s *PostgresStore) ListOutbox(ctx context.Context, status string, limit int) ([]OutboxEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("postgres approval store db is required")
	}
	query := s.db.WithContext(ctx).Order("available_at ASC, created_at ASC, id ASC")
	if limit > 0 {
		query = query.Limit(normalizeListLimit(limit))
	}
	if status = strings.TrimSpace(status); status != "" {
		query = query.Where("status = ?", status)
	}
	var values []OutboxEvent
	if err := query.Find(&values).Error; err != nil {
		return nil, fmt.Errorf("list approval outbox: %w", err)
	}
	for i := range values {
		values[i] = cloneOutbox(values[i])
	}
	return values, nil
}

// MarkOutboxPublished performs the relay ACK under a row lock, making
// pending->published idempotent across concurrent publisher retries.
func (s *PostgresStore) MarkOutboxPublished(ctx context.Context, eventID string, at time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres approval store db is required")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return fmt.Errorf("outbox event id is required")
	}
	if at.IsZero() {
		at = s.clock().UTC()
	} else {
		at = at.UTC()
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var event OutboxEvent
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&event, "id = ?", eventID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: outbox event %s", ErrNotFound, eventID)
		}
		if err != nil {
			return fmt.Errorf("lock approval outbox event %s: %w", eventID, err)
		}
		if event.Status == OutboxStatusPublished {
			return nil
		}
		if event.Status != OutboxStatusPending {
			return fmt.Errorf("%w: outbox event %s cannot publish from %s", ErrInvalidTransition, eventID, event.Status)
		}
		result := tx.Model(&OutboxEvent{}).
			Where("id = ? AND status = ?", eventID, OutboxStatusPending).
			Updates(map[string]any{"status": OutboxStatusPublished, "published_at": at})
		if result.Error != nil {
			return fmt.Errorf("ack approval outbox event %s: %w", eventID, result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: concurrent outbox ACK %s", ErrVersionConflict, eventID)
		}
		return nil
	})
}

func (s *PostgresStore) MarkOutboxFailed(ctx context.Context, eventID string, at time.Time, maxAttempts int) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres approval store db is required")
	}
	if at.IsZero() {
		at = s.clock().UTC()
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var event OutboxEvent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&event, "id = ?", strings.TrimSpace(eventID)).Error; err != nil {
			return err
		}
		if event.Status != OutboxStatusPending {
			return nil
		}
		event.Attempts++
		updates := map[string]any{"attempts": event.Attempts}
		if maxAttempts > 0 && event.Attempts >= maxAttempts {
			updates["status"] = OutboxStatusDead
		} else {
			updates["available_at"] = at.Add(approvalOutboxBackoff(event.Attempts))
		}
		return tx.Model(&OutboxEvent{}).Where("id = ? AND status = ?", event.ID, OutboxStatusPending).Updates(updates).Error
	})
}

func lockApproval(tx *gorm.DB, approvalID string) (Approval, error) {
	var record approvalRecord
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, "id = ?", strings.TrimSpace(approvalID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Approval{}, fmt.Errorf("%w: %s", ErrNotFound, approvalID)
	}
	if err != nil {
		return Approval{}, fmt.Errorf("lock approval %s: %w", approvalID, err)
	}
	return recordToApproval(record)
}

func findApprovalByIDOrKey(tx *gorm.DB, approvalID, key string) (Approval, bool, error) {
	var record approvalRecord
	err := tx.Where("id = ? OR idempotency_key = ?", approvalID, key).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Approval{}, false, nil
	}
	if err != nil {
		return Approval{}, false, fmt.Errorf("find approval by idempotency identity: %w", err)
	}
	value, err := recordToApproval(record)
	return value, true, err
}

func approvalToRecord(value Approval) (approvalRecord, error) {
	binding, err := json.Marshal(value.Binding)
	if err != nil {
		return approvalRecord{}, fmt.Errorf("marshal approval binding: %w", err)
	}
	approve, err := json.Marshal(value.ApproveCommand)
	if err != nil {
		return approvalRecord{}, fmt.Errorf("marshal approve command: %w", err)
	}
	reject, err := json.Marshal(value.RejectCommand)
	if err != nil {
		return approvalRecord{}, fmt.Errorf("marshal reject command: %w", err)
	}
	return approvalRecord{
		ID: value.ID, IdempotencyKey: value.IdempotencyKey, TenantID: value.TenantID, UserID: value.UserID,
		SessionID: value.SessionID, ArtifactType: value.ArtifactType, Binding: binding,
		ReviewMode: value.ReviewMode, ExecutionMode: value.ExecutionMode, ExecutionEpoch: value.ExecutionEpoch,
		Status: value.Status, DecisionVersion: value.DecisionVersion,
		ApproveCommand: approve, RejectCommand: reject,
		CheckpointMappingID: value.CheckpointMappingID, MappingEpoch: value.MappingEpoch,
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, ExpiresAt: value.ExpiresAt, DecidedAt: value.DecidedAt,
	}, nil
}

func recordToApproval(record approvalRecord) (Approval, error) {
	value := Approval{
		ID: record.ID, IdempotencyKey: record.IdempotencyKey, TenantID: record.TenantID, UserID: record.UserID,
		SessionID: record.SessionID, ArtifactType: record.ArtifactType,
		ReviewMode: record.ReviewMode, ExecutionMode: record.ExecutionMode, ExecutionEpoch: record.ExecutionEpoch,
		Status: record.Status, DecisionVersion: record.DecisionVersion,
		CheckpointMappingID: record.CheckpointMappingID, MappingEpoch: record.MappingEpoch,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, ExpiresAt: record.ExpiresAt, DecidedAt: record.DecidedAt,
	}
	if err := json.Unmarshal(record.Binding, &value.Binding); err != nil {
		return Approval{}, fmt.Errorf("unmarshal approval binding %s: %w", record.ID, err)
	}
	if err := json.Unmarshal(record.ApproveCommand, &value.ApproveCommand); err != nil {
		return Approval{}, fmt.Errorf("unmarshal approve command %s: %w", record.ID, err)
	}
	if err := json.Unmarshal(record.RejectCommand, &value.RejectCommand); err != nil {
		return Approval{}, fmt.Errorf("unmarshal reject command %s: %w", record.ID, err)
	}
	return value, nil
}

func candidateApprovalBatchToRecord(value CandidateApprovalBatch) (candidateApprovalBatchRecord, error) {
	targets, err := json.Marshal(value.Targets)
	if err != nil {
		return candidateApprovalBatchRecord{}, fmt.Errorf("marshal candidate approval batch targets: %w", err)
	}
	return candidateApprovalBatchRecord{
		ID: value.ID, IdempotencyKey: value.IdempotencyKey, SessionID: value.SessionID,
		StoryboardID: value.StoryboardID, ExpectedStoryboardVersion: value.ExpectedStoryboardVersion,
		Decision: value.Decision, ActorID: value.ActorID, Reason: value.Reason,
		Targets: targets, CreatedAt: value.CreatedAt,
	}, nil
}

func recordToCandidateApprovalBatch(record candidateApprovalBatchRecord) (CandidateApprovalBatch, error) {
	value := CandidateApprovalBatch{
		ID: record.ID, IdempotencyKey: record.IdempotencyKey, SessionID: record.SessionID,
		StoryboardID: record.StoryboardID, ExpectedStoryboardVersion: record.ExpectedStoryboardVersion,
		Decision: record.Decision, ActorID: record.ActorID, Reason: record.Reason, CreatedAt: record.CreatedAt,
	}
	if err := json.Unmarshal(record.Targets, &value.Targets); err != nil {
		return CandidateApprovalBatch{}, fmt.Errorf("unmarshal candidate approval batch targets %s: %w", record.ID, err)
	}
	return value, nil
}

func decisionToRecord(value ApprovalDecision) (approvalDecisionRecord, error) {
	var observed []byte
	var err error
	if value.ObservedBinding != nil {
		observed, err = json.Marshal(value.ObservedBinding)
		if err != nil {
			return approvalDecisionRecord{}, fmt.Errorf("marshal observed binding: %w", err)
		}
	}
	return approvalDecisionRecord{
		ApprovalID: value.ApprovalID, DecisionVersion: value.DecisionVersion,
		IdempotencyKey: value.IdempotencyKey, RequestedDecision: value.RequestedDecision,
		EffectiveStatus: value.EffectiveStatus, ActorID: value.ActorID, Reason: value.Reason,
		ObservedBinding: observed, CommandKind: value.CommandKind,
		CommandIdempotencyKey: value.CommandIdempotencyKey,
		CommandPayload:        append([]byte(nil), value.CommandPayload...), CreatedAt: value.CreatedAt,
	}, nil
}

func recordToDecision(record approvalDecisionRecord) (ApprovalDecision, error) {
	value := ApprovalDecision{
		ApprovalID: record.ApprovalID, DecisionVersion: record.DecisionVersion,
		IdempotencyKey: record.IdempotencyKey, RequestedDecision: record.RequestedDecision,
		EffectiveStatus: record.EffectiveStatus, ActorID: record.ActorID, Reason: record.Reason,
		CommandKind: record.CommandKind, CommandIdempotencyKey: record.CommandIdempotencyKey,
		CommandPayload: append([]byte(nil), record.CommandPayload...), CreatedAt: record.CreatedAt,
	}
	if len(record.ObservedBinding) > 0 {
		var observed VersionBinding
		if err := json.Unmarshal(record.ObservedBinding, &observed); err != nil {
			return ApprovalDecision{}, fmt.Errorf("unmarshal observed binding: %w", err)
		}
		value.ObservedBinding = &observed
	}
	return value, nil
}

func loadDecisionResult(tx *gorm.DB, approval Approval, decision ApprovalDecision, created bool) (DecisionResult, error) {
	var continuation sessionruntime.ApprovalContinuation
	if err := tx.First(&continuation, "approval_id = ? AND decision_version = ?", decision.ApprovalID, decision.DecisionVersion).Error; err != nil {
		return DecisionResult{}, fmt.Errorf("load approval continuation: %w", err)
	}
	eventType := EventApprovalContinuationRequested
	if continuation.Executor == sessionruntime.ContinuationExecutorRunnerResume {
		eventType = EventSessionInputRequested
	}
	key := decisionOutboxKey(approval.ID, decision.DecisionVersion, continuation.ExecutionEpoch, eventType)
	var outbox OutboxEvent
	if err := tx.First(&outbox, "idempotency_key = ?", key).Error; err != nil {
		return DecisionResult{}, fmt.Errorf("load approval decision outbox: %w", err)
	}
	return DecisionResult{Approval: approval, Decision: decision, Continuation: continuation, Outbox: outbox, Created: created}, nil
}

func loadFallbackResult(tx *gorm.DB, approval Approval, switched bool) (FallbackResult, error) {
	var continuation *sessionruntime.ApprovalContinuation
	if approval.DecisionVersion > 0 {
		var value sessionruntime.ApprovalContinuation
		if err := tx.First(&value, "approval_id = ? AND decision_version = ?", approval.ID, approval.DecisionVersion).Error; err != nil {
			return FallbackResult{}, fmt.Errorf("load fallback continuation: %w", err)
		}
		continuation = &value
	}
	eventType := EventApprovalFallbackEnabled
	if continuation != nil {
		eventType = EventApprovalContinuationRequested
	}
	key := decisionOutboxKey(approval.ID, approval.DecisionVersion, approval.ExecutionEpoch, eventType)
	var outbox OutboxEvent
	if err := tx.First(&outbox, "idempotency_key = ?", key).Error; err != nil {
		return FallbackResult{}, fmt.Errorf("load fallback outbox: %w", err)
	}
	return FallbackResult{Approval: approval, Continuation: continuation, Outbox: outbox, Switched: switched}, nil
}

func cloneDecisionResult(value DecisionResult) DecisionResult {
	value.Approval = cloneApproval(value.Approval)
	value.Decision = cloneDecision(value.Decision)
	value.Outbox = cloneOutbox(value.Outbox)
	return value
}

func cloneFallbackResult(value FallbackResult) FallbackResult {
	value.Approval = cloneApproval(value.Approval)
	if value.Continuation != nil {
		copy := *value.Continuation
		value.Continuation = &copy
	}
	value.Outbox = cloneOutbox(value.Outbox)
	return value
}

var _ Store = (*PostgresStore)(nil)
