package sessionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStore struct{ db *gorm.DB }

var errTurnMutationNoop = errors.New("session turn mutation is an idempotent replay")

func NewPostgresStore(db *gorm.DB) *PostgresStore { return &PostgresStore{db: db} }

// WithTx binds all runtime mutations to a caller-owned transaction. This is
// used to atomically combine UserMessage persistence, Turn output messages,
// checkpoint mappings, SessionEventLog rows, and the runtime state change.
func (s *PostgresStore) WithTx(tx *gorm.DB) *PostgresStore { return &PostgresStore{db: tx} }

func (s *PostgresStore) AutoMigrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres session runtime store db is required")
	}
	return Migrate(ctx, s.db)
}

func (s *PostgresStore) EnqueueInput(ctx context.Context, sessionID string, input SessionInput) (EnqueueResult, error) {
	if err := s.ready(); err != nil {
		return EnqueueResult{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return EnqueueResult{}, fmt.Errorf("session id is required")
	}
	identity, payload, err := encodeInput(input)
	if err != nil {
		return EnqueueResult{}, err
	}
	requested := SessionInputRecord{InputID: identity.InputID, SessionID: sessionID, InputType: identity.Type, SourceID: identity.SourceID, EventID: identity.EventID, Payload: payload, Priority: identity.Priority, ContextMessageSeq: inputContextMessageSeq(input)}
	var result EnqueueResult
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO aigc_session_input_counters (session_id, next_seq, updated_at)
			VALUES (?, 0, CURRENT_TIMESTAMP) ON CONFLICT (session_id) DO NOTHING`, sessionID).Error; err != nil {
			return fmt.Errorf("ensure session input counter: %w", err)
		}
		var counter sessionInputCounter
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&counter, "session_id = ?", sessionID).Error; err != nil {
			return fmt.Errorf("lock session input counter: %w", err)
		}
		var existing SessionInputRecord
		err := tx.First(&existing, "input_id = ?", identity.InputID).Error
		if err == nil {
			if !sameInput(existing, requested) {
				return fmt.Errorf("%w: input_id=%s", ErrIdempotencyConflict, identity.InputID)
			}
			result.Input = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find input by id: %w", err)
		}
		err = tx.Where("session_id = ? AND input_type = ? AND source_id = ?", sessionID, identity.Type, identity.SourceID).First(&existing).Error
		if err == nil {
			if !sameInput(existing, requested) {
				return fmt.Errorf("%w: source_id=%s", ErrIdempotencyConflict, identity.SourceID)
			}
			result.Input = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find input by source: %w", err)
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		requested.EnqueueSeq = counter.NextSeq + 1
		requested.Status = InputStatusPending
		requested.AvailableAt, requested.CreatedAt, requested.UpdatedAt = now, now, now
		if err := tx.Create(&requested).Error; err != nil {
			return fmt.Errorf("%w: insert input %s: %v", ErrIdempotencyConflict, identity.InputID, err)
		}
		if err := tx.Model(&counter).Updates(map[string]any{"next_seq": requested.EnqueueSeq, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("advance session input counter: %w", err)
		}
		result = EnqueueResult{Input: requested, Enqueued: true}
		return nil
	})
	return result, err
}

func (s *PostgresStore) GetInput(ctx context.Context, inputID string) (SessionInputRecord, error) {
	if err := s.ready(); err != nil {
		return SessionInputRecord{}, err
	}
	var record SessionInputRecord
	err := s.db.WithContext(ctx).First(&record, "input_id = ?", strings.TrimSpace(inputID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SessionInputRecord{}, fmt.Errorf("%w: input_id=%s", ErrInputNotFound, inputID)
	}
	if err != nil {
		return SessionInputRecord{}, fmt.Errorf("get session input %s: %w", inputID, err)
	}
	return record, nil
}

func (s *PostgresStore) ListRunnableSessions(ctx context.Context, limit int) ([]string, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	type row struct {
		SessionID string
		FirstSeq  int64
	}
	var rows []row
	err := s.db.WithContext(ctx).Raw(`WITH started_head AS (
			SELECT DISTINCT ON (session_id) input_id, session_id
			FROM aigc_session_inputs
			WHERE attempts > 0 AND status IN ('pending', 'retry_wait', 'claimed', 'running')
			ORDER BY session_id, enqueue_seq ASC
		), runnable AS (
			SELECT i.session_id, i.enqueue_seq
			FROM aigc_session_inputs i
			LEFT JOIN started_head h ON h.session_id = i.session_id
			WHERE ((h.input_id IS NOT NULL AND i.input_id = h.input_id)
				OR (h.input_id IS NULL AND i.attempts = 0))
			AND ((i.status IN ('pending', 'retry_wait') AND i.available_at <= CURRENT_TIMESTAMP)
				OR (i.status IN ('claimed', 'running') AND (i.lease_until IS NULL OR i.lease_until <= CURRENT_TIMESTAMP)))
		)
		SELECT session_id, MIN(enqueue_seq) AS first_seq FROM runnable
		GROUP BY session_id ORDER BY first_seq ASC, session_id ASC LIMIT ?`, limit).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list runnable sessions: %w", err)
	}
	sessions := make([]string, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, row.SessionID)
	}
	return sessions, nil
}

func (s *PostgresStore) ClaimNext(ctx context.Context, options ClaimOptions) (SessionInputRecord, error) {
	if err := s.ready(); err != nil {
		return SessionInputRecord{}, err
	}
	if options.ClaimTTL <= 0 {
		return SessionInputRecord{}, fmt.Errorf("claim ttl must be positive")
	}
	var out SessionInputRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lease, err := validateFenceTx(tx, options.Fence, true)
		if err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		for {
			// The runtime lease row above serializes claim selection for this
			// session. Lock all nonterminal inputs as well so enqueue/claim state is
			// observed and updated as one atomic head-of-line decision.
			var work []SessionInputRecord
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("session_id = ? AND status IN ?", options.Fence.SessionID, []InputStatus{InputStatusPending, InputStatusRetryWait, InputStatusClaimed, InputStatusRunning}).
				Order("enqueue_seq ASC").Find(&work).Error; err != nil {
				return fmt.Errorf("lock session input lane: %w", err)
			}

			var started *SessionInputRecord
			for i := range work {
				if work[i].Attempts > 0 {
					started = &work[i]
					break
				}
			}
			if started != nil {
				if started.Status != InputStatusPending && started.Status != InputStatusRetryWait {
					return ErrNoInputAvailable
				}
				if started.AvailableAt.After(now) {
					return ErrNoInputAvailable
				}
				if options.MaxAttempts > 0 && started.Attempts >= options.MaxAttempts {
					frozen, frozenErr := inputHasFrozenOutputTx(tx, started.InputID)
					if frozenErr != nil {
						return frozenErr
					}
					if frozen {
						out = *started
						break
					}
					if err := deadExhaustedInputTx(ctx, tx, *started, now); err != nil {
						return err
					}
					continue
				}
				out = *started
				break
			}

			candidates := make([]SessionInputRecord, 0, len(work))
			for _, candidate := range work {
				if candidate.Attempts == 0 && (candidate.Status == InputStatusPending || candidate.Status == InputStatusRetryWait) && !candidate.AvailableAt.After(now) {
					candidates = append(candidates, candidate)
				}
			}
			if len(candidates) == 0 {
				return ErrNoInputAvailable
			}
			sort.Slice(candidates, func(i, j int) bool {
				if candidates[i].Priority != candidates[j].Priority {
					return candidates[i].Priority > candidates[j].Priority
				}
				return candidates[i].EnqueueSeq < candidates[j].EnqueueSeq
			})
			out = candidates[0]
			break
		}
		until := cappedLeaseUntil(now, options.ClaimTTL, lease.LeaseUntil)
		out.Status = InputStatusClaimed
		out.ClaimOwner, out.ClaimFence, out.LeaseUntil = options.Fence.OwnerID, options.Fence.FenceToken, &until
		out.Attempts++
		out.ErrorCode, out.ErrorMessage, out.UpdatedAt = "", "", now
		return tx.Save(&out).Error
	})
	if err != nil {
		return SessionInputRecord{}, err
	}
	return out, nil
}

func deadExhaustedInputTx(ctx context.Context, tx *gorm.DB, record SessionInputRecord, now time.Time) error {
	if err := appendTerminalFailureEventTx(ctx, tx, record, record.TurnID); err != nil {
		return fmt.Errorf("append exhausted input failure event: %w", err)
	}
	record.Status = InputStatusDead
	record.ClaimOwner, record.ClaimFence, record.LeaseUntil = "", 0, nil
	record.ErrorCode = "max_attempts_exceeded"
	record.ErrorMessage = "session input exceeded maximum attempts"
	record.UpdatedAt = now
	if err := tx.Save(&record).Error; err != nil {
		return fmt.Errorf("expire exhausted session input %s: %w", record.InputID, err)
	}
	if strings.TrimSpace(record.TurnID) == "" {
		return nil
	}
	updates := map[string]any{
		"status": TurnStatusDead, "error_code": record.ErrorCode,
		"error_message": record.ErrorMessage, "updated_at": now,
	}
	if err := tx.Model(&SessionTurnRun{}).
		Where("turn_id = ? AND status NOT IN ?", record.TurnID, []TurnStatus{TurnStatusWaitingInterrupt, TurnStatusCommitted, TurnStatusDead}).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("expire exhausted session turn %s: %w", record.TurnID, err)
	}
	return nil
}

func inputHasFrozenOutputTx(tx *gorm.DB, inputID string) (bool, error) {
	var count int64
	if err := tx.Model(&SessionTurnRun{}).
		Where("input_id = ? AND output_payload_json IS NOT NULL", strings.TrimSpace(inputID)).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("check frozen turn output: %w", err)
	}
	return count > 0, nil
}

func (s *PostgresStore) MarkInputRunning(ctx context.Context, fence Fence, inputID string, ttl time.Duration) (SessionInputRecord, error) {
	if ttl <= 0 {
		return SessionInputRecord{}, fmt.Errorf("input lease ttl must be positive")
	}
	return s.mutateInput(ctx, fence, inputID, []InputStatus{InputStatusClaimed, InputStatusRunning}, func(record *SessionInputRecord, lease SessionRuntimeLease, now time.Time) error {
		until := cappedLeaseUntil(now, ttl, lease.LeaseUntil)
		record.Status, record.LeaseUntil = InputStatusRunning, &until
		record.ErrorCode, record.ErrorMessage = "", ""
		return nil
	})
}

func (s *PostgresStore) RetryInput(ctx context.Context, fence Fence, inputID string, availableAt time.Time, failure Failure) (SessionInputRecord, error) {
	return s.mutateInput(ctx, fence, inputID, []InputStatus{InputStatusClaimed, InputStatusRunning}, func(record *SessionInputRecord, _ SessionRuntimeLease, now time.Time) error {
		if availableAt.IsZero() || availableAt.Before(now) {
			availableAt = now
		}
		record.Status, record.AvailableAt = InputStatusRetryWait, availableAt.UTC()
		record.ClaimOwner, record.ClaimFence, record.LeaseUntil = "", 0, nil
		record.ErrorCode, record.ErrorMessage = strings.TrimSpace(failure.Code), strings.TrimSpace(failure.Message)
		return nil
	})
}

func (s *PostgresStore) ResolveInput(ctx context.Context, fence Fence, inputID string) (SessionInputRecord, error) {
	return s.finishInput(ctx, fence, inputID, InputStatusResolved, Failure{})
}

func (s *PostgresStore) DeadInput(ctx context.Context, fence Fence, inputID string, failure Failure) (SessionInputRecord, error) {
	if err := s.ready(); err != nil {
		return SessionInputRecord{}, err
	}
	var out SessionInputRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := validateFenceTx(tx, fence, true); err != nil {
			return err
		}
		record, err := loadClaimedInputTx(tx, fence, inputID, InputStatusClaimed, InputStatusRunning)
		if err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		if err := appendTerminalFailureEventTx(ctx, tx, record, record.TurnID); err != nil {
			return fmt.Errorf("append dead input failure event: %w", err)
		}
		record.Status, record.ClaimOwner, record.ClaimFence, record.LeaseUntil = InputStatusDead, "", 0, nil
		record.ErrorCode, record.ErrorMessage = strings.TrimSpace(failure.Code), strings.TrimSpace(failure.Message)
		record.UpdatedAt = now
		if err := tx.Save(&record).Error; err != nil {
			return err
		}
		out = record
		return nil
	})
	return out, err
}

func (s *PostgresStore) RecoverExpiredInputs(ctx context.Context, fence Fence) (int64, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	var count int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := validateFenceTx(tx, fence, true); err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		var records []SessionInputRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ? AND status IN ? AND (claim_owner <> ? OR claim_fence <> ? OR lease_until IS NULL OR lease_until <= CURRENT_TIMESTAMP)", fence.SessionID, []InputStatus{InputStatusClaimed, InputStatusRunning}, fence.OwnerID, fence.FenceToken).
			Find(&records).Error; err != nil {
			return fmt.Errorf("find expired input claims: %w", err)
		}
		for _, record := range records {
			if err := tx.Model(&record).Updates(map[string]any{
				"status": InputStatusRetryWait, "available_at": now, "claim_owner": "", "claim_fence": 0,
				"lease_until": nil, "error_code": "claim_recovered", "error_message": "previous runtime claim expired or was fenced", "updated_at": now,
			}).Error; err != nil {
				return err
			}
			if record.TurnID != "" {
				if err := tx.Model(&SessionTurnRun{}).Where("turn_id = ? AND status NOT IN ?", record.TurnID, []TurnStatus{TurnStatusCommitted, TurnStatusDead, TurnStatusWaitingInterrupt}).Updates(map[string]any{
					"status": TurnStatusRetryWait, "error_code": "claim_recovered", "error_message": "previous runtime claim expired or was fenced", "updated_at": now,
				}).Error; err != nil {
					return err
				}
			}
			count++
		}
		return nil
	})
	return count, err
}

func (s *PostgresStore) AcquireLease(ctx context.Context, sessionID, ownerID string, ttl time.Duration) (SessionRuntimeLease, error) {
	if err := s.ready(); err != nil {
		return SessionRuntimeLease{}, err
	}
	sessionID, ownerID = strings.TrimSpace(sessionID), strings.TrimSpace(ownerID)
	if sessionID == "" || ownerID == "" || ttl <= 0 {
		return SessionRuntimeLease{}, fmt.Errorf("session id, owner id, and positive ttl are required")
	}
	var lease SessionRuntimeLease
	result := s.db.WithContext(ctx).Raw(`INSERT INTO aigc_session_runtime_leases
		(session_id, owner_id, fence_token, lease_until, updated_at)
		VALUES (?, ?, 1, CURRENT_TIMESTAMP + (? * INTERVAL '1 microsecond'), CURRENT_TIMESTAMP)
		ON CONFLICT (session_id) DO UPDATE SET
			owner_id = EXCLUDED.owner_id,
			fence_token = CASE WHEN aigc_session_runtime_leases.owner_id = EXCLUDED.owner_id
				THEN aigc_session_runtime_leases.fence_token ELSE aigc_session_runtime_leases.fence_token + 1 END,
			lease_until = EXCLUDED.lease_until, updated_at = CURRENT_TIMESTAMP
		WHERE aigc_session_runtime_leases.owner_id = EXCLUDED.owner_id
			OR aigc_session_runtime_leases.lease_until <= CURRENT_TIMESTAMP
		RETURNING session_id, owner_id, fence_token, lease_until, updated_at`, sessionID, ownerID, ttl.Microseconds()).Scan(&lease)
	if result.Error != nil {
		return SessionRuntimeLease{}, fmt.Errorf("acquire session lease: %w", result.Error)
	}
	if result.RowsAffected == 0 || lease.SessionID == "" {
		return SessionRuntimeLease{}, fmt.Errorf("%w: session_id=%s", ErrLeaseHeld, sessionID)
	}
	return lease, nil
}

func (s *PostgresStore) RenewLease(ctx context.Context, fence Fence, ttl time.Duration) (SessionRuntimeLease, error) {
	if err := s.ready(); err != nil {
		return SessionRuntimeLease{}, err
	}
	if ttl <= 0 {
		return SessionRuntimeLease{}, fmt.Errorf("lease ttl must be positive")
	}
	var lease SessionRuntimeLease
	result := s.db.WithContext(ctx).Raw(`UPDATE aigc_session_runtime_leases
		SET lease_until = CURRENT_TIMESTAMP + (? * INTERVAL '1 microsecond'), updated_at = CURRENT_TIMESTAMP
		WHERE session_id = ? AND owner_id = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP
		RETURNING session_id, owner_id, fence_token, lease_until, updated_at`, ttl.Microseconds(), fence.SessionID, fence.OwnerID, fence.FenceToken).Scan(&lease)
	if result.Error != nil {
		return SessionRuntimeLease{}, fmt.Errorf("renew session lease: %w", result.Error)
	}
	if result.RowsAffected == 0 || lease.SessionID == "" {
		return SessionRuntimeLease{}, fenceError(fence)
	}
	return lease, nil
}

func (s *PostgresStore) HandoffLease(ctx context.Context, fence Fence, newOwnerID string, ttl time.Duration) (SessionRuntimeLease, error) {
	if err := s.ready(); err != nil {
		return SessionRuntimeLease{}, err
	}
	newOwnerID = strings.TrimSpace(newOwnerID)
	if newOwnerID == "" || ttl <= 0 {
		return SessionRuntimeLease{}, fmt.Errorf("new owner id and positive ttl are required")
	}
	var lease SessionRuntimeLease
	result := s.db.WithContext(ctx).Raw(`UPDATE aigc_session_runtime_leases SET
		owner_id = ?, fence_token = fence_token + CASE WHEN owner_id = ? THEN 0 ELSE 1 END,
		lease_until = CURRENT_TIMESTAMP + (? * INTERVAL '1 microsecond'), updated_at = CURRENT_TIMESTAMP
		WHERE session_id = ? AND owner_id = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP
		RETURNING session_id, owner_id, fence_token, lease_until, updated_at`, newOwnerID, newOwnerID, ttl.Microseconds(), fence.SessionID, fence.OwnerID, fence.FenceToken).Scan(&lease)
	if result.Error != nil {
		return SessionRuntimeLease{}, fmt.Errorf("handoff session lease: %w", result.Error)
	}
	if result.RowsAffected == 0 || lease.SessionID == "" {
		return SessionRuntimeLease{}, fenceError(fence)
	}
	return lease, nil
}

func (s *PostgresStore) ReleaseLease(ctx context.Context, fence Fence) error {
	if err := s.ready(); err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&SessionRuntimeLease{}).
		Where("session_id = ? AND owner_id = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP", fence.SessionID, fence.OwnerID, fence.FenceToken).
		Updates(map[string]any{"lease_until": gorm.Expr("CURRENT_TIMESTAMP"), "updated_at": gorm.Expr("CURRENT_TIMESTAMP")})
	if result.Error != nil {
		return fmt.Errorf("release session lease: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fenceError(fence)
	}
	return nil
}

func (s *PostgresStore) ValidateFence(ctx context.Context, fence Fence) error {
	if err := s.ready(); err != nil {
		return err
	}
	_, err := validateFenceTx(s.db.WithContext(ctx), fence, false)
	return err
}

func (s *PostgresStore) GetOrCreateTurn(ctx context.Context, fence Fence, inputID string, spec TurnSpec) (SessionTurnRun, bool, error) {
	if err := s.ready(); err != nil {
		return SessionTurnRun{}, false, err
	}
	var out SessionTurnRun
	created := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := validateFenceTx(tx, fence, true); err != nil {
			return err
		}
		input, err := loadClaimedInputTx(tx, fence, inputID, InputStatusClaimed, InputStatusRunning)
		if err != nil {
			return err
		}
		err = tx.Where("input_id = ?", input.InputID).First(&out).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find stable turn: %w", err)
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		turnID := strings.TrimSpace(spec.TurnID)
		if turnID == "" {
			turnID = StableTurnID(input.SessionID, input.InputID)
		}
		runnerRunID := strings.TrimSpace(spec.RunnerRunID)
		if runnerRunID == "" {
			runnerRunID = StableRunnerRunID(input.SessionID, input.InputID)
		}
		out = SessionTurnRun{TurnID: turnID, InputID: input.InputID, SessionID: input.SessionID, RunnerRunID: runnerRunID, ParentTurnID: strings.TrimSpace(spec.ParentTurnID), ClaimFence: fence.FenceToken, Kind: input.InputType, Status: TurnStatusPrepared, RunnerCheckpointID: strings.TrimSpace(spec.RunnerCheckpointID), Attempt: input.Attempts, ContextMessageSeq: input.ContextMessageSeq, ContextSeqFrozen: input.InputType == InputTypeUserMessage && input.ContextMessageSeq > 0, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&out).Error; err != nil {
			return fmt.Errorf("%w: create stable turn %s: %v", ErrIdempotencyConflict, turnID, err)
		}
		if err := tx.Model(&input).Updates(map[string]any{"turn_id": turnID, "updated_at": now}).Error; err != nil {
			return fmt.Errorf("attach stable turn to input: %w", err)
		}
		created = true
		return nil
	})
	return out, created, err
}

func (s *PostgresStore) GetTurn(ctx context.Context, turnID string) (SessionTurnRun, error) {
	if err := s.ready(); err != nil {
		return SessionTurnRun{}, err
	}
	var turn SessionTurnRun
	err := s.db.WithContext(ctx).First(&turn, "turn_id = ?", strings.TrimSpace(turnID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SessionTurnRun{}, fmt.Errorf("%w: turn_id=%s", ErrTurnNotFound, turnID)
	}
	if err != nil {
		return SessionTurnRun{}, fmt.Errorf("get turn %s: %w", turnID, err)
	}
	return turn, nil
}

func (s *PostgresStore) BeginTurn(ctx context.Context, fence Fence, turnID string) (SessionTurnRun, error) {
	return s.mutateTurn(ctx, fence, turnID, []TurnStatus{TurnStatusPrepared, TurnStatusRetryWait, TurnStatusRunning}, func(turn *SessionTurnRun, input *SessionInputRecord, now time.Time) error {
		turn.Status, turn.ClaimFence, turn.Attempt = TurnStatusRunning, fence.FenceToken, input.Attempts
		turn.ErrorCode, turn.ErrorMessage = "", ""
		input.Status = InputStatusRunning
		return nil
	})
}

func (s *PostgresStore) FreezeTurnContextMessageSeq(ctx context.Context, fence Fence, turnID string, throughSeq int64) (SessionTurnRun, error) {
	if throughSeq < 0 {
		return SessionTurnRun{}, fmt.Errorf("context message sequence cannot be negative")
	}
	return s.mutateTurn(ctx, fence, turnID, []TurnStatus{TurnStatusRunning, TurnStatusRetryWait}, func(turn *SessionTurnRun, _ *SessionInputRecord, _ time.Time) error {
		if turn.ContextSeqFrozen {
			return errTurnMutationNoop
		}
		turn.ContextMessageSeq = throughSeq
		turn.ContextSeqFrozen = true
		return nil
	})
}

func (s *PostgresStore) FreezeTurnContextFromTerminalUserInputs(ctx context.Context, fence Fence, turnID string) (SessionTurnRun, error) {
	if err := s.ready(); err != nil {
		return SessionTurnRun{}, err
	}
	if err := s.ValidateFence(ctx, fence); err != nil {
		return SessionTurnRun{}, err
	}
	turn, err := s.GetTurn(ctx, turnID)
	if err != nil {
		return SessionTurnRun{}, err
	}
	if turn.SessionID != fence.SessionID {
		return SessionTurnRun{}, fmt.Errorf("%w: turn_id=%s", ErrFenceRejected, turnID)
	}
	if turn.ContextSeqFrozen {
		return turn, nil
	}
	var throughSeq int64
	if err := s.db.WithContext(ctx).Model(&SessionInputRecord{}).
		Where("session_id = ? AND input_type = ? AND status IN ?", fence.SessionID, InputTypeUserMessage, []InputStatus{InputStatusResolved, InputStatusDead}).
		Select("COALESCE(MAX(context_message_seq), 0)").Scan(&throughSeq).Error; err != nil {
		return SessionTurnRun{}, fmt.Errorf("read terminal user input boundary: %w", err)
	}
	return s.FreezeTurnContextMessageSeq(ctx, fence, turnID, throughSeq)
}

func (s *PostgresStore) SaveTurnCheckpoint(ctx context.Context, fence Fence, turnID, checkpointID string) (SessionTurnRun, error) {
	checkpointID = strings.TrimSpace(checkpointID)
	if checkpointID == "" {
		return SessionTurnRun{}, fmt.Errorf("runner checkpoint id is required")
	}
	return s.mutateTurn(ctx, fence, turnID, []TurnStatus{TurnStatusRunning, TurnStatusRetryWait}, func(turn *SessionTurnRun, _ *SessionInputRecord, _ time.Time) error {
		turn.RunnerCheckpointID = checkpointID
		return nil
	})
}

func (s *PostgresStore) SaveTurnOutput(ctx context.Context, fence Fence, turnID string, payload json.RawMessage, digest string) (SessionTurnRun, error) {
	if len(payload) == 0 || !json.Valid(payload) {
		return SessionTurnRun{}, fmt.Errorf("turn output payload must be non-empty valid JSON")
	}
	digest = strings.TrimSpace(digest)
	return s.mutateTurn(ctx, fence, turnID, []TurnStatus{TurnStatusRunning, TurnStatusRetryWait}, func(turn *SessionTurnRun, _ *SessionInputRecord, _ time.Time) error {
		if len(turn.OutputPayload) != 0 {
			if turn.OutputDigest != digest || !jsonEqual(turn.OutputPayload, payload) {
				return fmt.Errorf("%w: turn %s output receipt changed", ErrIdempotencyConflict, turnID)
			}
			return errTurnMutationNoop
		}
		turn.OutputPayload = append(json.RawMessage(nil), payload...)
		turn.OutputDigest = digest
		return nil
	})
}

func (s *PostgresStore) WaitForInterrupt(ctx context.Context, fence Fence, turnID, checkpointID string) (SessionTurnRun, error) {
	checkpointID = strings.TrimSpace(checkpointID)
	if checkpointID == "" {
		return SessionTurnRun{}, fmt.Errorf("runner checkpoint id is required")
	}
	turn, err := s.mutateTurn(ctx, fence, turnID, []TurnStatus{TurnStatusRunning, TurnStatusWaitingInterrupt}, func(turn *SessionTurnRun, input *SessionInputRecord, now time.Time) error {
		turn.Status, turn.RunnerCheckpointID = TurnStatusWaitingInterrupt, checkpointID
		input.Status, input.ClaimOwner, input.ClaimFence, input.LeaseUntil = InputStatusResolved, "", 0, nil
		input.ResolvedAt = &now
		return nil
	})
	if err == nil && turn.RunnerCheckpointID != checkpointID {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s checkpoint changed", ErrIdempotencyConflict, turnID)
	}
	return turn, err
}

func (s *PostgresStore) RetryTurn(ctx context.Context, fence Fence, turnID string, failure Failure) (SessionTurnRun, error) {
	return s.RetryTurnAt(ctx, fence, turnID, time.Time{}, failure)
}

func (s *PostgresStore) RetryTurnAt(ctx context.Context, fence Fence, turnID string, availableAt time.Time, failure Failure) (SessionTurnRun, error) {
	return s.mutateTurn(ctx, fence, turnID, []TurnStatus{TurnStatusPrepared, TurnStatusRunning, TurnStatusRetryWait}, func(turn *SessionTurnRun, input *SessionInputRecord, now time.Time) error {
		turn.Status, turn.ErrorCode, turn.ErrorMessage = TurnStatusRetryWait, strings.TrimSpace(failure.Code), strings.TrimSpace(failure.Message)
		if availableAt.IsZero() || availableAt.Before(now) {
			availableAt = now
		}
		input.Status, input.AvailableAt = InputStatusRetryWait, availableAt.UTC()
		input.ClaimOwner, input.ClaimFence, input.LeaseUntil = "", 0, nil
		input.ErrorCode, input.ErrorMessage = turn.ErrorCode, turn.ErrorMessage
		return nil
	})
}

func (s *PostgresStore) CommitTurn(ctx context.Context, fence Fence, turnID, outputDigest string) (SessionTurnRun, error) {
	outputDigest = strings.TrimSpace(outputDigest)
	turn, err := s.mutateTurn(ctx, fence, turnID, []TurnStatus{TurnStatusRunning, TurnStatusCommitting, TurnStatusCommitted}, func(turn *SessionTurnRun, input *SessionInputRecord, now time.Time) error {
		if len(turn.OutputPayload) != 0 && turn.OutputDigest != outputDigest {
			return fmt.Errorf("%w: turn %s output digest changed", ErrIdempotencyConflict, turnID)
		}
		turn.Status, turn.OutputDigest, turn.CommittedAt = TurnStatusCommitted, outputDigest, &now
		input.Status, input.ClaimOwner, input.ClaimFence, input.LeaseUntil = InputStatusResolved, "", 0, nil
		input.ResolvedAt = &now
		return nil
	})
	if err == nil && turn.OutputDigest != outputDigest {
		return SessionTurnRun{}, fmt.Errorf("%w: turn %s output digest changed", ErrIdempotencyConflict, turnID)
	}
	return turn, err
}

func (s *PostgresStore) DeadTurn(ctx context.Context, fence Fence, turnID string, failure Failure) (SessionTurnRun, error) {
	if err := s.ready(); err != nil {
		return SessionTurnRun{}, err
	}
	var out SessionTurnRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := validateFenceTx(tx, fence, true); err != nil {
			return err
		}
		var turn SessionTurnRun
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&turn, "turn_id = ?", strings.TrimSpace(turnID)).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: turn_id=%s", ErrTurnNotFound, turnID)
		}
		if err != nil {
			return err
		}
		if turn.Status == TurnStatusCommitted || turn.Status == TurnStatusWaitingInterrupt {
			return fmt.Errorf("%w: turn %s cannot die from %s", ErrInvalidTransition, turn.TurnID, turn.Status)
		}
		if turn.Status == TurnStatusDead {
			var input SessionInputRecord
			if err := tx.First(&input, "input_id = ?", turn.InputID).Error; err != nil {
				return err
			}
			if err := appendTerminalFailureEventTx(ctx, tx, input, turn.TurnID); err != nil {
				return fmt.Errorf("repair dead turn failure event: %w", err)
			}
			out = turn
			return nil
		}
		input, err := loadClaimedInputTx(tx, fence, turn.InputID, InputStatusClaimed, InputStatusRunning)
		if err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		if err := appendTerminalFailureEventTx(ctx, tx, input, turn.TurnID); err != nil {
			return fmt.Errorf("append dead turn failure event: %w", err)
		}
		turn.Status, turn.ErrorCode, turn.ErrorMessage = TurnStatusDead, strings.TrimSpace(failure.Code), strings.TrimSpace(failure.Message)
		turn.UpdatedAt = now
		input.Status, input.ClaimOwner, input.ClaimFence, input.LeaseUntil = InputStatusDead, "", 0, nil
		input.ErrorCode, input.ErrorMessage, input.UpdatedAt = turn.ErrorCode, turn.ErrorMessage, now
		if err := tx.Save(&turn).Error; err != nil {
			return err
		}
		if err := tx.Save(&input).Error; err != nil {
			return err
		}
		out = turn
		return nil
	})
	return out, err
}

func (s *PostgresStore) RequestContinuation(ctx context.Context, continuation ApprovalContinuation) (ApprovalContinuation, bool, error) {
	if err := s.ready(); err != nil {
		return ApprovalContinuation{}, false, err
	}
	continuation, err := normalizeContinuation(continuation)
	if err != nil {
		return ApprovalContinuation{}, false, err
	}
	now := time.Now().UTC()
	continuation.Status, continuation.CreatedAt, continuation.UpdatedAt = ContinuationStatusRequested, now, now
	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&continuation)
	if result.Error != nil {
		return ApprovalContinuation{}, false, fmt.Errorf("request approval continuation: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return continuation, true, nil
	}
	existing, err := s.GetContinuation(ctx, continuation.ApprovalID, continuation.DecisionVersion)
	if err != nil {
		return ApprovalContinuation{}, false, err
	}
	if existing.SessionID != continuation.SessionID || existing.Executor != continuation.Executor || existing.ExecutionEpoch != continuation.ExecutionEpoch {
		return ApprovalContinuation{}, false, fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrIdempotencyConflict, continuation.ApprovalID, continuation.DecisionVersion)
	}
	return existing, false, nil
}

func (s *PostgresStore) GetContinuation(ctx context.Context, approvalID string, decisionVersion int) (ApprovalContinuation, error) {
	if err := s.ready(); err != nil {
		return ApprovalContinuation{}, err
	}
	var record ApprovalContinuation
	err := s.db.WithContext(ctx).Where("approval_id = ? AND decision_version = ?", strings.TrimSpace(approvalID), decisionVersion).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ApprovalContinuation{}, fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrContinuationNotFound, approvalID, decisionVersion)
	}
	if err != nil {
		return ApprovalContinuation{}, fmt.Errorf("get approval continuation: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) ClaimContinuation(ctx context.Context, claim ContinuationClaim, ttl time.Duration) (ApprovalContinuation, error) {
	if err := s.ready(); err != nil {
		return ApprovalContinuation{}, err
	}
	if err := validateContinuationClaim(claim); err != nil {
		return ApprovalContinuation{}, err
	}
	if ttl <= 0 {
		return ApprovalContinuation{}, fmt.Errorf("continuation lease ttl must be positive")
	}
	var out ApprovalContinuation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record ApprovalContinuation
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("approval_id = ? AND decision_version = ?", claim.ApprovalID, claim.DecisionVersion).First(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrContinuationNotFound, claim.ApprovalID, claim.DecisionVersion)
		}
		if err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		if record.Executor != claim.Executor || record.ExecutionEpoch != claim.ExecutionEpoch {
			return fmt.Errorf("%w: continuation executor or epoch changed", ErrFenceRejected)
		}
		if record.Status == ContinuationStatusApplied {
			out = record
			return nil
		}
		if record.Status == ContinuationStatusClaimed && record.LeaseUntil != nil && record.LeaseUntil.After(now) {
			return fmt.Errorf("%w: lease_owner=%s", ErrContinuationClaimed, record.LeaseOwner)
		}
		if record.Status != ContinuationStatusRequested && record.Status != ContinuationStatusClaimed && record.Status != ContinuationStatusFailed {
			return fmt.Errorf("%w: continuation cannot claim from %s", ErrInvalidTransition, record.Status)
		}
		until := now.Add(ttl)
		record.Status, record.LeaseOwner, record.LeaseUntil = ContinuationStatusClaimed, claim.LeaseOwner, &until
		record.ErrorCode, record.ErrorMessage = "", ""
		record.UpdatedAt = now
		if err := tx.Save(&record).Error; err != nil {
			return err
		}
		out = record
		return nil
	})
	return out, err
}

func (s *PostgresStore) ApplyContinuation(ctx context.Context, claim ContinuationClaim, commands []ApprovalCommandLedger) (ApprovalContinuation, error) {
	return s.finishContinuation(ctx, claim, func(tx *gorm.DB, record *ApprovalContinuation, now time.Time) error {
		for _, command := range commands {
			item, err := normalizeCommand(command, claim)
			if err != nil {
				return err
			}
			if item.CreatedAt.IsZero() {
				item.CreatedAt = now
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&item)
			if result.Error != nil {
				return fmt.Errorf("record approval command: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				var existing ApprovalCommandLedger
				if err := tx.Where("approval_id = ? AND decision_version = ? AND command_kind = ?", item.ApprovalID, item.DecisionVersion, item.CommandKind).First(&existing).Error; err != nil {
					return fmt.Errorf("%w: command idempotency key collision", ErrIdempotencyConflict)
				}
				if !sameCommand(existing, item) {
					return fmt.Errorf("%w: command_kind=%s", ErrIdempotencyConflict, item.CommandKind)
				}
			}
		}
		record.Status, record.LeaseUntil, record.AppliedAt = ContinuationStatusApplied, nil, &now
		return nil
	})
}

func (s *PostgresStore) FailContinuation(ctx context.Context, claim ContinuationClaim, failure Failure) (ApprovalContinuation, error) {
	return s.finishContinuation(ctx, claim, func(_ *gorm.DB, record *ApprovalContinuation, _ time.Time) error {
		record.Status, record.LeaseUntil = ContinuationStatusFailed, nil
		record.ErrorCode, record.ErrorMessage = strings.TrimSpace(failure.Code), strings.TrimSpace(failure.Message)
		return nil
	})
}

func (s *PostgresStore) FallbackContinuation(ctx context.Context, approvalID string, decisionVersion int, expectedEpoch int64) (ApprovalContinuation, error) {
	if err := s.ready(); err != nil {
		return ApprovalContinuation{}, err
	}
	var out ApprovalContinuation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record ApprovalContinuation
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("approval_id = ? AND decision_version = ?", strings.TrimSpace(approvalID), decisionVersion).First(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrContinuationNotFound, approvalID, decisionVersion)
		}
		if err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		if record.Status == ContinuationStatusApplied {
			out = record
			return nil
		}
		if record.Executor != ContinuationExecutorRunnerResume || record.ExecutionEpoch != expectedEpoch {
			return fmt.Errorf("%w: continuation executor or epoch changed", ErrFenceRejected)
		}
		if record.Status == ContinuationStatusClaimed && record.LeaseUntil != nil && record.LeaseUntil.After(now) {
			return fmt.Errorf("%w: lease_owner=%s", ErrContinuationClaimed, record.LeaseOwner)
		}
		record.Executor, record.ExecutionEpoch, record.Status = ContinuationExecutorDeterministic, record.ExecutionEpoch+1, ContinuationStatusRequested
		record.LeaseOwner, record.LeaseUntil, record.ErrorCode, record.ErrorMessage, record.UpdatedAt = "", nil, "", "", now
		if err := tx.Save(&record).Error; err != nil {
			return err
		}
		out = record
		return nil
	})
	return out, err
}

func (s *PostgresStore) GetCommand(ctx context.Context, approvalID string, decisionVersion int, commandKind string) (ApprovalCommandLedger, error) {
	if err := s.ready(); err != nil {
		return ApprovalCommandLedger{}, err
	}
	var record ApprovalCommandLedger
	err := s.db.WithContext(ctx).Where("approval_id = ? AND decision_version = ? AND command_kind = ?", strings.TrimSpace(approvalID), decisionVersion, strings.TrimSpace(commandKind)).First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ApprovalCommandLedger{}, fmt.Errorf("%w: approval command not found", ErrContinuationNotFound)
	}
	if err != nil {
		return ApprovalCommandLedger{}, fmt.Errorf("get approval command: %w", err)
	}
	return record, nil
}

func (s *PostgresStore) mutateInput(ctx context.Context, fence Fence, inputID string, statuses []InputStatus, mutate func(*SessionInputRecord, SessionRuntimeLease, time.Time) error) (SessionInputRecord, error) {
	if err := s.ready(); err != nil {
		return SessionInputRecord{}, err
	}
	var out SessionInputRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lease, err := validateFenceTx(tx, fence, true)
		if err != nil {
			return err
		}
		record, err := loadClaimedInputTx(tx, fence, inputID, statuses...)
		if err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		if err := mutate(&record, lease, now); err != nil {
			return err
		}
		record.UpdatedAt = now
		if err := tx.Save(&record).Error; err != nil {
			return err
		}
		out = record
		return nil
	})
	return out, err
}

func (s *PostgresStore) finishInput(ctx context.Context, fence Fence, inputID string, status InputStatus, failure Failure) (SessionInputRecord, error) {
	return s.mutateInput(ctx, fence, inputID, []InputStatus{InputStatusClaimed, InputStatusRunning}, func(record *SessionInputRecord, _ SessionRuntimeLease, now time.Time) error {
		record.Status, record.ClaimOwner, record.ClaimFence, record.LeaseUntil = status, "", 0, nil
		record.ErrorCode, record.ErrorMessage = strings.TrimSpace(failure.Code), strings.TrimSpace(failure.Message)
		if status == InputStatusResolved {
			record.ResolvedAt = &now
		}
		return nil
	})
}

func (s *PostgresStore) mutateTurn(ctx context.Context, fence Fence, turnID string, statuses []TurnStatus, mutate func(*SessionTurnRun, *SessionInputRecord, time.Time) error) (SessionTurnRun, error) {
	if err := s.ready(); err != nil {
		return SessionTurnRun{}, err
	}
	var out SessionTurnRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := validateFenceTx(tx, fence, true); err != nil {
			return err
		}
		var turn SessionTurnRun
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&turn, "turn_id = ?", strings.TrimSpace(turnID)).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: turn_id=%s", ErrTurnNotFound, turnID)
		}
		if err != nil {
			return err
		}
		allowed := false
		for _, status := range statuses {
			allowed = allowed || turn.Status == status
		}
		if !allowed {
			return fmt.Errorf("%w: turn %s cannot transition from %s", ErrInvalidTransition, turnID, turn.Status)
		}
		if turn.Status == TurnStatusCommitted || turn.Status == TurnStatusWaitingInterrupt || turn.Status == TurnStatusDead {
			out = turn
			return nil
		}
		input, err := loadClaimedInputTx(tx, fence, turn.InputID, InputStatusClaimed, InputStatusRunning)
		if err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		if err := mutate(&turn, &input, now); err != nil {
			if errors.Is(err, errTurnMutationNoop) {
				out = turn
				return nil
			}
			return err
		}
		turn.UpdatedAt, input.UpdatedAt = now, now
		if err := tx.Save(&turn).Error; err != nil {
			return err
		}
		if err := tx.Save(&input).Error; err != nil {
			return err
		}
		out = turn
		return nil
	})
	return out, err
}

func (s *PostgresStore) finishContinuation(ctx context.Context, claim ContinuationClaim, mutate func(*gorm.DB, *ApprovalContinuation, time.Time) error) (ApprovalContinuation, error) {
	if err := s.ready(); err != nil {
		return ApprovalContinuation{}, err
	}
	if err := validateContinuationClaim(claim); err != nil {
		return ApprovalContinuation{}, err
	}
	var out ApprovalContinuation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record ApprovalContinuation
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("approval_id = ? AND decision_version = ?", claim.ApprovalID, claim.DecisionVersion).First(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: approval_id=%s decision_version=%d", ErrContinuationNotFound, claim.ApprovalID, claim.DecisionVersion)
		}
		if err != nil {
			return err
		}
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		if record.Status == ContinuationStatusApplied {
			out = record
			return nil
		}
		if record.Executor != claim.Executor || record.ExecutionEpoch != claim.ExecutionEpoch || record.Status != ContinuationStatusClaimed || record.LeaseOwner != claim.LeaseOwner || record.LeaseUntil == nil || !record.LeaseUntil.After(now) {
			return fmt.Errorf("%w: approval continuation claim is stale", ErrFenceRejected)
		}
		if err := mutate(tx, &record, now); err != nil {
			return err
		}
		record.UpdatedAt = now
		if err := tx.Save(&record).Error; err != nil {
			return err
		}
		out = record
		return nil
	})
	return out, err
}

func (s *PostgresStore) ready() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("postgres session runtime store db is required")
	}
	return nil
}

func validateFenceTx(tx *gorm.DB, fence Fence, lock bool) (SessionRuntimeLease, error) {
	fence.SessionID, fence.OwnerID = strings.TrimSpace(fence.SessionID), strings.TrimSpace(fence.OwnerID)
	if fence.SessionID == "" || fence.OwnerID == "" || fence.FenceToken <= 0 {
		return SessionRuntimeLease{}, fenceError(fence)
	}
	query := tx.Where("session_id = ? AND owner_id = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP", fence.SessionID, fence.OwnerID, fence.FenceToken)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var lease SessionRuntimeLease
	err := query.First(&lease).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SessionRuntimeLease{}, fenceError(fence)
	}
	if err != nil {
		return SessionRuntimeLease{}, fmt.Errorf("validate session fence: %w", err)
	}
	return lease, nil
}

func loadClaimedInputTx(tx *gorm.DB, fence Fence, inputID string, statuses ...InputStatus) (SessionInputRecord, error) {
	var record SessionInputRecord
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&record, "input_id = ?", strings.TrimSpace(inputID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return SessionInputRecord{}, fmt.Errorf("%w: input_id=%s", ErrInputNotFound, inputID)
	}
	if err != nil {
		return SessionInputRecord{}, err
	}
	if record.SessionID != fence.SessionID || record.ClaimOwner != fence.OwnerID || record.ClaimFence != fence.FenceToken {
		return SessionInputRecord{}, fmt.Errorf("%w: input_id=%s", ErrFenceRejected, inputID)
	}
	now, err := databaseNow(tx)
	if err != nil {
		return SessionInputRecord{}, err
	}
	if record.LeaseUntil == nil || !record.LeaseUntil.After(now) {
		return SessionInputRecord{}, fmt.Errorf("%w: input claim expired for input_id=%s", ErrFenceRejected, inputID)
	}
	allowed := false
	for _, status := range statuses {
		allowed = allowed || record.Status == status
	}
	if !allowed {
		return SessionInputRecord{}, fmt.Errorf("%w: input %s is %s", ErrInvalidTransition, inputID, record.Status)
	}
	return record, nil
}

func databaseNow(tx *gorm.DB) (time.Time, error) {
	var row struct{ Now time.Time }
	if err := tx.Raw("SELECT CURRENT_TIMESTAMP AS now").Scan(&row).Error; err != nil {
		return time.Time{}, fmt.Errorf("read database time: %w", err)
	}
	return row.Now.UTC(), nil
}

func fenceError(fence Fence) error {
	return fmt.Errorf("%w: session_id=%s owner_id=%s fence_token=%d", ErrFenceRejected, fence.SessionID, fence.OwnerID, fence.FenceToken)
}
