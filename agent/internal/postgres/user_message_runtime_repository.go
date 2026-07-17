package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/FigoGoo/Dora-Agent/agent/internal/usermessageruntime"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type userMessageContentProtector interface {
	Protect(context.Context, []byte) (session.ProtectedContent, error)
	Open(context.Context, session.ProtectedContent, string) ([]byte, error)
}

type userMessageIDGenerator interface {
	New() (string, error)
}

// UserMessageRuntimeRepository 实现方案 A 隔离表、全 source HOL、Lease/Fence 与 first-write-wins Receipt。
type UserMessageRuntimeRepository struct {
	db        *gorm.DB
	protector userMessageContentProtector
	ids       userMessageIDGenerator
}

// NewUserMessageRuntimeRepository 创建本地方案 A PostgreSQL Adapter，不执行 Migration 或自动回填。
func NewUserMessageRuntimeRepository(
	client *Client,
	protector userMessageContentProtector,
	ids userMessageIDGenerator,
) (*UserMessageRuntimeRepository, error) {
	if client == nil || client.db == nil || protector == nil || ids == nil {
		return nil, fmt.Errorf("create user message runtime repository: dependency is nil")
	}
	return &UserMessageRuntimeRepository{db: client.db, protector: protector, ids: ids}, nil
}

type userMessageClaimRow struct {
	InputID              string    `gorm:"column:input_id"`
	SessionID            string    `gorm:"column:session_id"`
	MessageID            string    `gorm:"column:message_id"`
	UserID               string    `gorm:"column:user_id"`
	ProjectID            string    `gorm:"column:project_id"`
	EnqueueSeq           int64     `gorm:"column:enqueue_seq"`
	Attempts             int       `gorm:"column:attempts"`
	TurnID               string    `gorm:"column:turn_id"`
	OutputID             string    `gorm:"column:output_id"`
	ModelCallID          string    `gorm:"column:model_call_id"`
	RecoveryEventID      string    `gorm:"column:recovery_event_id"`
	TerminalEventID      string    `gorm:"column:terminal_event_id"`
	ContextSchemaVersion string    `gorm:"column:context_schema_version"`
	MessageCutoffSeq     int64     `gorm:"column:message_cutoff_seq"`
	MessageContentDigest string    `gorm:"column:message_content_digest"`
	SkillSnapshotRef     string    `gorm:"column:skill_snapshot_ref"`
	SkillSnapshotDigest  string    `gorm:"column:skill_snapshot_digest"`
	PromptRef            string    `gorm:"column:prompt_ref"`
	PromptDigest         string    `gorm:"column:prompt_digest"`
	ToolRegistryRef      string    `gorm:"column:tool_registry_ref"`
	ToolRegistryDigest   string    `gorm:"column:tool_registry_digest"`
	RuntimePolicyRef     string    `gorm:"column:runtime_policy_ref"`
	RuntimePolicyDigest  string    `gorm:"column:runtime_policy_digest"`
	ModelRouteRef        string    `gorm:"column:model_route_ref"`
	ModelRouteDigest     string    `gorm:"column:model_route_digest"`
	BudgetRef            string    `gorm:"column:budget_ref"`
	BudgetDigest         string    `gorm:"column:budget_digest"`
	AccessScopeRef       string    `gorm:"column:access_scope_ref"`
	AccessScopeDigest    string    `gorm:"column:access_scope_digest"`
	ContextDigest        string    `gorm:"column:context_digest"`
	MessageCiphertext    []byte    `gorm:"column:message_ciphertext"`
	MessageKeyVersion    string    `gorm:"column:message_key_version"`
	StoredMessageDigest  string    `gorm:"column:stored_message_digest"`
	LeaseFence           int64     `gorm:"column:lease_fence"`
	LeaseVersion         int64     `gorm:"column:lease_version"`
	LeaseExpiredTakeover bool      `gorm:"column:lease_expired_takeover"`
	DatabaseNow          time.Time `gorm:"column:database_now"`
}

// ClaimNext 先计算每个 Session 的全 source 最小非终态 Input，再只分派 eligible user_message。
func (r *UserMessageRuntimeRepository) ClaimNext(
	ctx context.Context,
	owner string,
	_ time.Time,
	leaseDuration time.Duration,
) (*usermessageruntime.Claim, error) {
	if owner == "" || leaseDuration <= 0 {
		return nil, usermessageruntime.ErrInvalidClaim
	}
	candidateRunID, err := r.ids.New()
	if err != nil || !canonicalRuntimeUUIDv7(candidateRunID) {
		return nil, usermessageruntime.ErrPersistence
	}
	var row userMessageClaimRow
	var runID string
	var fence int64
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Raw(`
			WITH database_clock AS MATERIALIZED (
				SELECT clock_timestamp() AS database_now
			), head_ids AS MATERIALIZED (
				SELECT DISTINCT ON (candidate.session_id)
				       candidate.session_id, candidate.id
				FROM agent.session_input AS candidate
				WHERE candidate.status IN ('pending','claimed','running','retry_wait','recovery_pending')
				ORDER BY candidate.session_id, candidate.enqueue_seq, candidate.id
			)
			SELECT input_record.id AS input_id, input_record.session_id, input_record.message_id,
			       session_record.user_id, session_record.project_id, input_record.enqueue_seq,
			       input_record.attempts, turn_record.turn_id, turn_record.output_id,
			       turn_record.model_call_id, turn_record.recovery_event_id, turn_record.terminal_event_id,
			       context_record.schema_version AS context_schema_version,
			       context_record.message_cutoff_seq, context_record.message_content_digest,
			       context_record.skill_snapshot_ref, context_record.skill_snapshot_digest,
			       context_record.prompt_ref, context_record.prompt_digest,
			       context_record.tool_registry_ref, context_record.tool_registry_digest,
			       context_record.runtime_policy_ref, context_record.runtime_policy_digest,
			       context_record.model_route_ref, context_record.model_route_digest,
			       context_record.budget_ref, context_record.budget_digest,
			       context_record.access_scope_ref, context_record.access_scope_digest,
			       context_record.context_digest,
			       message_record.content_ciphertext AS message_ciphertext,
			       message_record.content_key_version AS message_key_version,
			       message_record.content_digest AS stored_message_digest,
			       lease.fence_token AS lease_fence, lease.version AS lease_version,
			       lease.lease_owner IS NOT NULL AND lease.lease_until <= database_clock.database_now
			           AS lease_expired_takeover,
			       database_clock.database_now
			FROM head_ids
			CROSS JOIN database_clock
			JOIN agent.session_input AS input_record ON input_record.id = head_ids.id
			JOIN agent.session AS session_record ON session_record.id = input_record.session_id
			JOIN agent.session_runtime_lease AS lease ON lease.session_id = input_record.session_id
			JOIN agent.session_message AS message_record ON message_record.id = input_record.message_id
			JOIN agent.session_user_message_turn AS turn_record ON turn_record.input_id = input_record.id
			JOIN agent.session_user_message_turn_context AS context_record ON context_record.turn_id = turn_record.turn_id
			LEFT JOIN agent.session_user_message_upgrade_ledger AS upgrade_ledger ON upgrade_ledger.input_id = input_record.id
			WHERE input_record.source_type = 'user_message'
			  AND input_record.status IN ('pending','claimed','running','retry_wait','recovery_pending')
			  AND input_record.available_at <= database_clock.database_now
			  AND session_record.status = 'active'
			  AND (upgrade_ledger.input_id IS NULL OR upgrade_ledger.stage = 'verified')
			  AND (lease.lease_owner IS NULL OR lease.lease_until <= database_clock.database_now)
			ORDER BY input_record.available_at, input_record.session_id, input_record.enqueue_seq
			FOR UPDATE OF input_record SKIP LOCKED
			LIMIT 1`).Scan(&row)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected == 0 {
			return nil
		}
		var lockedLease sessionRuntimeLeaseModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ?", row.SessionID).Take(&lockedLease).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		if lockedLease.Version != row.LeaseVersion || lockedLease.FenceToken != row.LeaseFence ||
			(lockedLease.LeaseOwner != nil && (lockedLease.LeaseUntil == nil || lockedLease.LeaseUntil.After(row.DatabaseNow))) {
			return usermessageruntime.ErrFenceLost
		}
		row.LeaseExpiredTakeover = lockedLease.LeaseOwner != nil && lockedLease.LeaseUntil != nil &&
			!lockedLease.LeaseUntil.After(row.DatabaseNow)

		var lockedTurn userMessageTurnModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("turn_id = ? AND input_id = ? AND session_id = ? AND status IN ?",
				row.TurnID, row.InputID, row.SessionID, []string{"created", "running"}).
			Take(&lockedTurn).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		if lockedTurn.OutputID != row.OutputID || lockedTurn.ModelCallID != row.ModelCallID {
			return usermessageruntime.ErrFenceLost
		}

		var existingRun userMessageRunModel
		runErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("input_id = ?", row.InputID).Take(&existingRun).Error
		if runErr != nil && !errors.Is(runErr, gorm.ErrRecordNotFound) {
			return runErr
		}

		fence = lockedLease.FenceToken + 1
		leaseUntil := row.DatabaseNow.Add(leaseDuration)
		leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
			Where("session_id = ? AND version = ? AND fence_token = ? AND (lease_owner IS NULL OR lease_until <= ?)",
				row.SessionID, lockedLease.Version, lockedLease.FenceToken, row.DatabaseNow).
			Updates(map[string]any{
				"lease_owner": owner, "lease_until": leaseUntil, "fence_token": fence,
				"version": gorm.Expr("version + 1"), "updated_at": row.DatabaseNow,
			})
		if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND status IN ?", row.InputID, []string{"pending", "claimed", "running", "retry_wait", "recovery_pending"}).
			Updates(map[string]any{
				"status": "claimed", "attempts": gorm.Expr("attempts + 1"),
				"lease_owner": owner, "lease_until": leaseUntil, "fence_token": fence, "updated_at": row.DatabaseNow,
			})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		row.Attempts++
		switch {
		case runErr == nil:
			runID = existingRun.RunID
			runUpdates := map[string]any{
				"owner_fence": fence,
				"version":     gorm.Expr("version + 1"),
			}
			if row.LeaseExpiredTakeover && existingRun.Status == "running" {
				runUpdates["status"] = "recovery_pending"
			}
			update := tx.Model(&userMessageRunModel{}).Where("run_id = ? AND version = ? AND status IN ?",
				existingRun.RunID, existingRun.Version, []string{"created", "running", "recovery_pending"}).
				Updates(runUpdates)
			if update.Error != nil || update.RowsAffected != 1 {
				return usermessageruntime.ErrFenceLost
			}
		case errors.Is(runErr, gorm.ErrRecordNotFound):
			runID = candidateRunID
			run := userMessageRunModel{
				RunID: runID, TurnID: row.TurnID, InputID: row.InputID, SessionID: row.SessionID,
				OwnerFence: fence, Status: "created", Version: 1, CreatedAt: row.DatabaseNow,
			}
			if err := tx.Create(&run).Error; err != nil {
				return err
			}
			outputReceipt := userMessageOutputReceiptModel{
				OutputID: row.OutputID, RunID: runID, TurnID: row.TurnID, InputID: row.InputID,
				ProjectionKey: "session:" + row.SessionID + ":turn:" + row.TurnID,
				SchemaVersion: usermessageruntime.DirectResponseCardSchemaVersion,
				Status:        "open", CreatedAt: row.DatabaseNow,
			}
			if err := tx.Create(&outputReceipt).Error; err != nil {
				return err
			}
		default:
			return runErr
		}
		return nil
	})
	if err != nil {
		return nil, mapUserMessageRuntimeError(err)
	}
	if row.InputID == "" {
		return nil, nil
	}
	claim := mapUserMessageClaim(row, owner, runID, fence)
	plaintext, openErr := r.protector.Open(ctx, session.ProtectedContent{
		Ciphertext: append([]byte(nil), row.MessageCiphertext...), KeyVersion: row.MessageKeyVersion,
	}, row.StoredMessageDigest)
	claim.MessagePlaintext = string(plaintext)
	if openErr != nil || row.StoredMessageDigest != row.MessageContentDigest ||
		!validUserMessageContextDigest(row) || usermessageruntime.ValidateClaim(claim) != nil {
		claim.MessagePlaintext = ""
		claim.Poisoned = true
		return &claim, nil
	}
	return &claim, nil
}

func mapUserMessageClaim(row userMessageClaimRow, owner, runID string, fence int64) usermessageruntime.Claim {
	return usermessageruntime.Claim{
		Profile: usermessageruntime.Profile, Owner: owner, RunID: runID,
		ModelCallID: row.ModelCallID, OutputID: row.OutputID,
		RecoveryEventID: row.RecoveryEventID, TerminalEventID: row.TerminalEventID,
		FenceToken: fence, Attempts: row.Attempts, EnqueueSeq: row.EnqueueSeq,
		Context: turncontext.UserMessageTurnContext{
			SchemaVersion: row.ContextSchemaVersion, TurnID: row.TurnID, SessionID: row.SessionID,
			InputID: row.InputID, MessageID: row.MessageID, UserID: row.UserID, ProjectID: row.ProjectID,
			MessageCutoffSeq: row.MessageCutoffSeq, MessageContentDigest: row.MessageContentDigest,
			SkillSnapshotRef: row.SkillSnapshotRef, SkillSnapshotDigest: row.SkillSnapshotDigest,
			PromptRef: row.PromptRef, PromptDigest: row.PromptDigest,
			ToolRegistryRef: row.ToolRegistryRef, ToolRegistryDigest: row.ToolRegistryDigest,
			RuntimePolicyRef: row.RuntimePolicyRef, RuntimePolicyDigest: row.RuntimePolicyDigest,
			ModelRouteRef: row.ModelRouteRef, ModelRouteDigest: row.ModelRouteDigest,
			BudgetRef: row.BudgetRef, BudgetDigest: row.BudgetDigest,
			AccessScopeRef: row.AccessScopeRef, AccessScopeDigest: row.AccessScopeDigest,
			ContextDigest: row.ContextDigest,
		},
	}
}

func validUserMessageContextDigest(row userMessageClaimRow) bool {
	value := session.UserMessageContext{
		TurnID: row.TurnID, SchemaVersion: row.ContextSchemaVersion, SessionID: row.SessionID,
		InputID: row.InputID, MessageID: row.MessageID, UserID: row.UserID, ProjectID: row.ProjectID,
		MessageCutoffSeq: row.MessageCutoffSeq, MessageContentDigest: row.MessageContentDigest,
		SkillSnapshotRef: row.SkillSnapshotRef, SkillSnapshotDigest: row.SkillSnapshotDigest,
		PromptRef: row.PromptRef, PromptDigest: row.PromptDigest,
		ToolRegistryRef: row.ToolRegistryRef, ToolRegistryDigest: row.ToolRegistryDigest,
		RuntimePolicyRef: row.RuntimePolicyRef, RuntimePolicyDigest: row.RuntimePolicyDigest,
		ModelRouteRef: row.ModelRouteRef, ModelRouteDigest: row.ModelRouteDigest,
		BudgetRef: row.BudgetRef, BudgetDigest: row.BudgetDigest,
		AccessScopeRef: row.AccessScopeRef, AccessScopeDigest: row.AccessScopeDigest,
	}
	digest, err := session.DigestUserMessageContext(value)
	return err == nil && digest == row.ContextDigest
}

// MarkRunning 原子推进 Input/Turn/Run；任何零行都表示当前 Fence 已失效。
func (r *UserMessageRuntimeRepository) MarkRunning(ctx context.Context, claim usermessageruntime.Claim, _ time.Time) error {
	return r.withActiveUserMessageFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND status = 'claimed' AND lease_owner = ? AND fence_token = ?", claim.Context.InputID, claim.Owner, claim.FenceToken).
			Updates(map[string]any{"status": "running", "updated_at": databaseNow})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		turnUpdate := tx.Model(&userMessageTurnModel{}).
			Where("turn_id = ? AND status IN ?", claim.Context.TurnID, []string{"created", "running"}).
			Updates(map[string]any{"status": "running", "version": gorm.Expr("version + 1"), "updated_at": databaseNow})
		if turnUpdate.Error != nil || turnUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		runUpdate := tx.Model(&userMessageRunModel{}).
			Where("run_id = ? AND owner_fence = ? AND status IN ?", claim.RunID, claim.FenceToken, []string{"created", "running", "recovery_pending"}).
			Updates(map[string]any{
				"status": "running", "started_at": gorm.Expr("COALESCE(started_at, ?)", databaseNow),
				"version": gorm.Expr("version + 1"),
			})
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		return nil
	})
}

// Complete 在同一事务中验证冻结回执、投影安全 Card、追加终态事件、推进 Input/Turn/Run 并释放 Lane。
func (r *UserMessageRuntimeRepository) Complete(
	ctx context.Context,
	claim usermessageruntime.Claim,
	output usermessageruntime.Output,
	_ time.Time,
) error {
	if err := usermessageruntime.ValidateOutput(output, claim); err != nil {
		return err
	}
	encoded, err := output.CanonicalJSON()
	if err != nil {
		return err
	}
	digestBytes := sha256.Sum256(encoded)
	digest := hex.EncodeToString(digestBytes[:])

	return r.withActiveUserMessageFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var input sessionInputModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND session_id = ? AND status = 'running' AND lease_owner = ? AND fence_token = ?",
				claim.Context.InputID, claim.Context.SessionID, claim.Owner, claim.FenceToken).
			Take(&input).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		var lease sessionRuntimeLeaseModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ? AND lease_owner = ? AND fence_token = ? AND lease_until > clock_timestamp()",
				claim.Context.SessionID, claim.Owner, claim.FenceToken).
			Take(&lease).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		var turn userMessageTurnModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("turn_id = ? AND input_id = ? AND session_id = ? AND status = 'running'",
				claim.Context.TurnID, claim.Context.InputID, claim.Context.SessionID).
			Take(&turn).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		var run userMessageRunModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("run_id = ? AND turn_id = ? AND input_id = ? AND session_id = ? AND owner_fence = ? AND status = 'running'",
				claim.RunID, claim.Context.TurnID, claim.Context.InputID, claim.Context.SessionID, claim.FenceToken).
			Take(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		var receipt userMessageOutputReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("output_id = ? AND run_id = ? AND turn_id = ? AND input_id = ?",
				claim.OutputID, claim.RunID, claim.Context.TurnID, claim.Context.InputID).
			Take(&receipt).Error; err != nil {
			return err
		}
		wantReceiptStatus, wantSchema := "completed", usermessageruntime.DirectResponseCardSchemaVersion
		terminalStatus, inputStatus := "completed", "resolved"
		if output.Failure != nil {
			wantReceiptStatus, wantSchema = "failed", usermessageruntime.FailureCardSchemaVersion
			terminalStatus, inputStatus = "failed", "dead"
		}
		if receipt.Status != wantReceiptStatus || receipt.SchemaVersion != wantSchema ||
			receipt.ResultDigest == nil || *receipt.ResultDigest != digest ||
			receipt.ResultKeyVersion == nil || len(receipt.ResultCiphertext) == 0 {
			return usermessageruntime.ErrOutputContract
		}
		plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
			Ciphertext: append([]byte(nil), receipt.ResultCiphertext...), KeyVersion: *receipt.ResultKeyVersion,
		}, *receipt.ResultDigest)
		if err != nil || !bytes.Equal(plaintext, encoded) {
			return usermessageruntime.ErrOutputContract
		}
		if err := r.validateUserMessageTerminalModelReceipt(ctx, tx, claim, output); err != nil {
			return err
		}

		projectionVersion, err := r.upsertUserMessageProjection(
			tx, claim, wantSchema, terminalStatus, encoded, databaseNow,
		)
		if err != nil {
			return err
		}
		_ = projectionVersion // 投影版本独立于 Turn 聚合版本，不进入 Event payload。

		turnVersion := turn.Version + 1
		projectedEvent, err := newUserMessageTerminalEvent(
			claim, output, turnVersion, databaseNow,
		)
		if err != nil {
			return err
		}
		var counter sessionEventCounterModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ?", claim.Context.SessionID).Take(&counter).Error; err != nil {
			return err
		}
		seq := counter.LastSeq + 1
		if err := tx.Create(&sessionEventLogModel{
			EventID: projectedEvent.EventID, SessionID: projectedEvent.SessionID, Seq: seq,
			EventType: string(projectedEvent.Type), SchemaVersion: projectedEvent.SchemaVersion,
			SourceKind: projectedEvent.SourceKind, SourceID: projectedEvent.SourceID,
			ProjectionIndex: projectedEvent.ProjectionIndex,
			AggregateType:   string(projectedEvent.AggregateType), AggregateID: projectedEvent.AggregateID,
			AggregateVersion: projectedEvent.AggregateVersion,
			Payload:          string(projectedEvent.PayloadJSON), CreatedAt: projectedEvent.CreatedAt,
		}).Error; err != nil {
			return err
		}
		counterUpdate := tx.Model(&sessionEventCounterModel{}).
			Where("session_id = ? AND last_seq = ?", claim.Context.SessionID, counter.LastSeq).
			Updates(map[string]any{"last_seq": seq, "updated_at": databaseNow})
		if counterUpdate.Error != nil || counterUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}

		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND status = 'running' AND lease_owner = ? AND fence_token = ?",
				claim.Context.InputID, claim.Owner, claim.FenceToken).
			Updates(map[string]any{
				"status": inputStatus, "lease_owner": nil, "lease_until": nil, "updated_at": databaseNow,
			})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		turnUpdate := tx.Model(&userMessageTurnModel{}).
			Where("turn_id = ? AND version = ? AND status = 'running'", claim.Context.TurnID, turn.Version).
			Updates(map[string]any{
				"status": terminalStatus, "version": turnVersion,
				"updated_at": databaseNow, "completed_at": databaseNow,
			})
		if turnUpdate.Error != nil || turnUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		runUpdate := tx.Model(&userMessageRunModel{}).
			Where("run_id = ? AND version = ? AND owner_fence = ? AND status = 'running'",
				claim.RunID, run.Version, claim.FenceToken).
			Updates(map[string]any{
				"status": terminalStatus, "version": run.Version + 1, "completed_at": databaseNow,
			})
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
			Where("session_id = ? AND lease_owner = ? AND fence_token = ? AND lease_until > clock_timestamp()",
				claim.Context.SessionID, claim.Owner, claim.FenceToken).
			Updates(map[string]any{
				"lease_owner": nil, "lease_until": nil,
				"version": gorm.Expr("version + 1"), "updated_at": databaseNow,
			})
		if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		return nil
	})
}

func (r *UserMessageRuntimeRepository) upsertUserMessageProjection(
	tx *gorm.DB,
	claim usermessageruntime.Claim,
	schemaVersion string,
	status string,
	payload []byte,
	databaseNow time.Time,
) (int64, error) {
	var current userMessageOutputProjectionModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_id = ?", claim.Context.SessionID).Take(&current).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		projection := userMessageOutputProjectionModel{
			SessionID: claim.Context.SessionID, SourceInputID: claim.Context.InputID,
			SourceEnqueueSeq: claim.EnqueueSeq, TurnID: claim.Context.TurnID, RunID: claim.RunID,
			SchemaVersion: schemaVersion, Status: status, Payload: string(payload),
			ProjectionVersion: 1, UpdatedAt: databaseNow,
		}
		if err := tx.Create(&projection).Error; err != nil {
			return 0, err
		}
		return 1, nil
	case err != nil:
		return 0, err
	case current.SourceEnqueueSeq > claim.EnqueueSeq:
		return 0, usermessageruntime.ErrOutputContract
	case current.SourceEnqueueSeq == claim.EnqueueSeq:
		if current.SourceInputID != claim.Context.InputID || current.TurnID != claim.Context.TurnID ||
			current.RunID != claim.RunID || current.SchemaVersion != schemaVersion || current.Status != status {
			return 0, usermessageruntime.ErrOutputContract
		}
		stored, decodeErr := decodeStoredUserMessageOutput(current.SchemaVersion, []byte(current.Payload))
		if decodeErr != nil {
			return 0, usermessageruntime.ErrOutputContract
		}
		canonical, canonicalErr := stored.CanonicalJSON()
		if canonicalErr != nil || !bytes.Equal(canonical, payload) {
			return 0, usermessageruntime.ErrOutputContract
		}
		return current.ProjectionVersion, nil
	default:
		version := current.ProjectionVersion + 1
		update := tx.Model(&userMessageOutputProjectionModel{}).
			Where("session_id = ? AND source_enqueue_seq = ? AND projection_version = ?",
				claim.Context.SessionID, current.SourceEnqueueSeq, current.ProjectionVersion).
			Updates(map[string]any{
				"source_input_id": claim.Context.InputID, "source_enqueue_seq": claim.EnqueueSeq,
				"turn_id": claim.Context.TurnID, "run_id": claim.RunID,
				"schema_version": schemaVersion, "status": status,
				"payload": string(payload), "projection_version": version, "updated_at": databaseNow,
			})
		if update.Error != nil || update.RowsAffected != 1 {
			return 0, usermessageruntime.ErrFenceLost
		}
		return version, nil
	}
}

func newUserMessageTerminalEvent(
	claim usermessageruntime.Claim,
	output usermessageruntime.Output,
	aggregateVersion int64,
	createdAt time.Time,
) (event.Record, error) {
	if output.DirectResponse != nil {
		card := *output.DirectResponse
		return event.NewSessionTurnCompleted(
			claim.TerminalEventID, claim.Context.SessionID, claim.Context.InputID,
			event.SessionTurnDirectResponsePayload{
				SchemaVersion: card.SchemaVersion, TurnID: card.TurnID, RunID: card.RunID,
				InputID: card.InputID, Status: card.Status, MessageCode: card.MessageCode,
				Summary: card.Summary, AvailableActions: append([]string(nil), card.AvailableActions...),
			}, aggregateVersion, createdAt,
		)
	}
	card := *output.Failure
	return event.NewSessionTurnFailed(
		claim.TerminalEventID, claim.Context.SessionID, claim.Context.InputID,
		event.SessionTurnFailurePayload{
			SchemaVersion: card.SchemaVersion, TurnID: card.TurnID, RunID: card.RunID,
			InputID: card.InputID, Status: card.Status, ErrorCode: card.ErrorCode,
			Retryable: card.Retryable, Summary: card.Summary,
		}, aggregateVersion, createdAt,
	)
}

// RenewLease 使用数据库时钟延长唯一 Session Lease，并同步 Input provenance lease_until。
func (r *UserMessageRuntimeRepository) RenewLease(
	ctx context.Context,
	claim usermessageruntime.Claim,
	_ time.Time,
	leaseDuration time.Duration,
) error {
	return r.withActiveUserMessageFence(ctx, claim, func(tx *gorm.DB, _ time.Time) error {
		var input sessionInputModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND session_id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?",
				claim.Context.InputID, claim.Context.SessionID, claim.Owner, claim.FenceToken, []string{"claimed", "running"}).
			Take(&input).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		var lease sessionRuntimeLeaseModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ? AND lease_owner = ? AND fence_token = ? AND lease_until > clock_timestamp()",
				claim.Context.SessionID, claim.Owner, claim.FenceToken).
			Take(&lease).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		var renewNow time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&renewNow).Error; err != nil {
			return err
		}
		until := renewNow.Add(leaseDuration)
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?",
				claim.Context.InputID, claim.Owner, claim.FenceToken, []string{"claimed", "running"}).
			Updates(map[string]any{"lease_until": until, "updated_at": renewNow})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
			Where("session_id = ? AND version = ? AND lease_owner = ? AND fence_token = ? AND lease_until > clock_timestamp()",
				claim.Context.SessionID, lease.Version, claim.Owner, claim.FenceToken).
			Updates(map[string]any{"lease_until": until, "version": gorm.Expr("version + 1"), "updated_at": renewNow})
		if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		return nil
	})
}

// LoadFrozenOutput 解密并严格校验 first-write-wins Output Receipt。
func (r *UserMessageRuntimeRepository) LoadFrozenOutput(
	ctx context.Context,
	claim usermessageruntime.Claim,
) (usermessageruntime.OutputReceiptSnapshot, error) {
	if err := r.assertActiveUserMessageFence(ctx, claim); err != nil {
		return usermessageruntime.OutputReceiptSnapshot{}, err
	}
	var record userMessageOutputReceiptModel
	if err := r.db.WithContext(ctx).Where("output_id = ? AND run_id = ? AND turn_id = ? AND input_id = ?",
		claim.OutputID, claim.RunID, claim.Context.TurnID, claim.Context.InputID).Take(&record).Error; err != nil {
		return usermessageruntime.OutputReceiptSnapshot{}, mapUserMessageRuntimeError(err)
	}
	if record.Status == "open" {
		return usermessageruntime.OutputReceiptSnapshot{Stage: usermessageruntime.OutputReceiptOpen}, nil
	}
	if record.ResultDigest == nil || record.ResultKeyVersion == nil || len(record.ResultCiphertext) == 0 {
		return usermessageruntime.OutputReceiptSnapshot{}, usermessageruntime.ErrOutputContract
	}
	plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
		Ciphertext: append([]byte(nil), record.ResultCiphertext...), KeyVersion: *record.ResultKeyVersion,
	}, *record.ResultDigest)
	if err != nil {
		return usermessageruntime.OutputReceiptSnapshot{}, usermessageruntime.ErrOutputContract
	}
	output, err := decodeStoredUserMessageOutput(record.SchemaVersion, plaintext)
	if err != nil || usermessageruntime.ValidateOutput(output, claim) != nil {
		return usermessageruntime.OutputReceiptSnapshot{}, usermessageruntime.ErrOutputContract
	}
	stage := usermessageruntime.OutputReceiptCompleted
	if record.Status == "failed" {
		stage = usermessageruntime.OutputReceiptFailed
	}
	return usermessageruntime.OutputReceiptSnapshot{Stage: stage, Output: &output}, nil
}

// FreezeOutput first-write-wins 冻结完整加密 DTO；同一 owner/fence 的重复同义写可重放。
func (r *UserMessageRuntimeRepository) FreezeOutput(
	ctx context.Context,
	claim usermessageruntime.Claim,
	output usermessageruntime.Output,
	_ time.Time,
) error {
	if err := usermessageruntime.ValidateOutput(output, claim); err != nil {
		return err
	}
	encoded, err := output.CanonicalJSON()
	if err != nil {
		return err
	}
	digestBytes := sha256.Sum256(encoded)
	digest := hex.EncodeToString(digestBytes[:])
	protected, err := r.protector.Protect(ctx, encoded)
	if err != nil {
		return usermessageruntime.ErrPersistence
	}
	return r.withActiveUserMessageFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var current userMessageOutputReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("output_id = ?", claim.OutputID).Take(&current).Error; err != nil {
			return err
		}
		if current.Status != "open" {
			if current.ResultDigest != nil && *current.ResultDigest == digest {
				return nil
			}
			return usermessageruntime.ErrOutputContract
		}
		status, schemaVersion := "completed", usermessageruntime.DirectResponseCardSchemaVersion
		var errorCode *string
		if output.Failure != nil {
			status, schemaVersion = "failed", usermessageruntime.FailureCardSchemaVersion
			value := output.Failure.ErrorCode
			errorCode = &value
		}
		update := tx.Model(&userMessageOutputReceiptModel{}).
			Where("output_id = ? AND status = 'open'", claim.OutputID).
			Updates(map[string]any{
				"schema_version": schemaVersion, "status": status,
				"result_ciphertext": protected.Ciphertext, "result_key_version": protected.KeyVersion,
				"result_digest": digest, "error_code": errorCode, "completed_at": databaseNow,
			})
		if update.Error != nil || update.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		return nil
	})
}

// RetryExecution 仅在 Output 仍 open 时释放 Lane，并保持同一 Input/Turn/Run 身份。
func (r *UserMessageRuntimeRepository) RetryExecution(
	ctx context.Context,
	claim usermessageruntime.Claim,
	availableAt time.Time,
) error {
	return r.releaseUserMessageClaim(ctx, claim, "retry_wait", availableAt)
}

// DeferProjection 在回执/投影不确定时保持 HOL recovery_pending，后续只重读原 Output。
func (r *UserMessageRuntimeRepository) DeferProjection(
	ctx context.Context,
	claim usermessageruntime.Claim,
	availableAt time.Time,
) error {
	return r.releaseUserMessageClaim(ctx, claim, "recovery_pending", availableAt)
}

func (r *UserMessageRuntimeRepository) releaseUserMessageClaim(
	ctx context.Context,
	claim usermessageruntime.Claim,
	inputStatus string,
	availableAt time.Time,
) error {
	return r.withActiveUserMessageFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var input sessionInputModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND session_id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?",
				claim.Context.InputID, claim.Context.SessionID, claim.Owner, claim.FenceToken, []string{"claimed", "running"}).
			Take(&input).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		var lease sessionRuntimeLeaseModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ? AND lease_owner = ? AND fence_token = ? AND lease_until > clock_timestamp()",
				claim.Context.SessionID, claim.Owner, claim.FenceToken).
			Take(&lease).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		var run userMessageRunModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("run_id = ? AND input_id = ? AND session_id = ? AND owner_fence = ? AND status IN ?",
				claim.RunID, claim.Context.InputID, claim.Context.SessionID, claim.FenceToken,
				[]string{"created", "running", "recovery_pending"}).
			Take(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return usermessageruntime.ErrFenceLost
			}
			return err
		}
		if inputStatus == "retry_wait" {
			var receipt userMessageOutputReceiptModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("output_id = ? AND run_id = ? AND turn_id = ? AND input_id = ? AND status = 'open'",
					claim.OutputID, claim.RunID, claim.Context.TurnID, claim.Context.InputID).
				Take(&receipt).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return usermessageruntime.ErrOutputContract
				}
				return err
			}
		}
		if availableAt.Before(databaseNow) {
			availableAt = databaseNow
		}
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?", claim.Context.InputID, claim.Owner, claim.FenceToken, []string{"claimed", "running"}).
			Updates(map[string]any{
				"status": inputStatus, "available_at": availableAt, "lease_owner": nil,
				"lease_until": nil, "updated_at": databaseNow,
			})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		runUpdate := tx.Model(&userMessageRunModel{}).
			Where("run_id = ? AND owner_fence = ? AND status IN ?", claim.RunID, claim.FenceToken, []string{"created", "running", "recovery_pending"}).
			Updates(map[string]any{"status": "recovery_pending", "version": gorm.Expr("version + 1")})
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
			Where("session_id = ? AND lease_owner = ? AND fence_token = ? AND lease_until > clock_timestamp()",
				claim.Context.SessionID, claim.Owner, claim.FenceToken).
			Updates(map[string]any{"lease_owner": nil, "lease_until": nil, "version": gorm.Expr("version + 1"), "updated_at": databaseNow})
		if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		return nil
	})
}

// ReplayOrReserveModel 实现本地 Fake 单调用 first-write-wins 回执。
func (r *UserMessageRuntimeRepository) ReplayOrReserveModel(
	ctx context.Context,
	identity usermessageruntime.ModelReceiptIdentity,
	requestDigest string,
) (usermessageruntime.ModelReceiptSnapshot, bool, error) {
	claim := claimFromModelIdentity(identity)
	var result usermessageruntime.ModelReceiptSnapshot
	var execute bool
	err := r.withActiveUserMessageFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var record userMessageModelReceiptModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("model_call_id = ?", identity.ModelCallID).Take(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			record = userMessageModelReceiptModel{
				ModelCallID: identity.ModelCallID, RunID: identity.RunID, TurnID: identity.TurnID,
				InputID: identity.InputID, RequestDigest: requestDigest, ExecutionFence: identity.FenceToken,
				Status: "reserved", CreatedAt: databaseNow,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			result = usermessageruntime.ModelReceiptSnapshot{Stage: usermessageruntime.ModelReceiptReserved}
			execute = true
			return nil
		}
		if err != nil {
			return err
		}
		if record.RunID != identity.RunID || record.TurnID != identity.TurnID || record.InputID != identity.InputID || record.RequestDigest != requestDigest {
			return usermessageruntime.ErrOutputContract
		}
		switch record.Status {
		case "reserved":
			result = usermessageruntime.ModelReceiptSnapshot{Stage: usermessageruntime.ModelReceiptReserved}
			switch {
			case identity.FenceToken < record.ExecutionFence:
				return usermessageruntime.ErrFenceLost
			case identity.FenceToken == record.ExecutionFence:
				execute = false
			default:
				advance := tx.Model(&userMessageModelReceiptModel{}).
					Where("model_call_id = ? AND status = 'reserved' AND execution_fence = ?",
						identity.ModelCallID, record.ExecutionFence).
					Update("execution_fence", identity.FenceToken)
				if advance.Error != nil || advance.RowsAffected != 1 {
					return usermessageruntime.ErrFenceLost
				}
				execute = true
			}
		case "completed":
			if record.ResponseDigest == nil || record.ResponseKeyVersion == nil {
				return usermessageruntime.ErrOutputContract
			}
			plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
				Ciphertext: append([]byte(nil), record.ResponseCiphertext...), KeyVersion: *record.ResponseKeyVersion,
			}, *record.ResponseDigest)
			if err != nil {
				return usermessageruntime.ErrOutputContract
			}
			var message schema.Message
			if err := strictJSONDecode(plaintext, &message); err != nil {
				return usermessageruntime.ErrOutputContract
			}
			result = usermessageruntime.ModelReceiptSnapshot{Stage: usermessageruntime.ModelReceiptCompleted, Response: &message}
		case "failed":
			if record.ErrorCode == nil || *record.ErrorCode == "" {
				return usermessageruntime.ErrOutputContract
			}
			result = usermessageruntime.ModelReceiptSnapshot{Stage: usermessageruntime.ModelReceiptFailed, ErrorCode: *record.ErrorCode}
		default:
			return usermessageruntime.ErrOutputContract
		}
		return nil
	})
	return result, execute, err
}

// FreezeModelCompleted 冻结 Fake Model 完成响应；同摘要重放返回原事实。
func (r *UserMessageRuntimeRepository) FreezeModelCompleted(
	ctx context.Context,
	identity usermessageruntime.ModelReceiptIdentity,
	requestDigest string,
	response *schema.Message,
) error {
	if response == nil {
		return usermessageruntime.ErrOutputContract
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return usermessageruntime.ErrOutputContract
	}
	digestBytes := sha256.Sum256(encoded)
	digest := hex.EncodeToString(digestBytes[:])
	protected, err := r.protector.Protect(ctx, encoded)
	if err != nil {
		return usermessageruntime.ErrPersistence
	}
	return r.freezeModel(ctx, identity, requestDigest, "completed", protected, digest, "")
}

// FreezeModelFailed 冻结稳定错误码，不持久化模型实现原始错误。
func (r *UserMessageRuntimeRepository) FreezeModelFailed(
	ctx context.Context,
	identity usermessageruntime.ModelReceiptIdentity,
	requestDigest string,
	errorCode string,
) error {
	if errorCode == "" {
		return usermessageruntime.ErrOutputContract
	}
	return r.freezeModel(ctx, identity, requestDigest, "failed", session.ProtectedContent{}, "", errorCode)
}

func (r *UserMessageRuntimeRepository) freezeModel(
	ctx context.Context,
	identity usermessageruntime.ModelReceiptIdentity,
	requestDigest string,
	status string,
	protected session.ProtectedContent,
	digest string,
	errorCode string,
) error {
	claim := claimFromModelIdentity(identity)
	return r.withActiveUserMessageFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var record userMessageModelReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("model_call_id = ?", identity.ModelCallID).Take(&record).Error; err != nil {
			return err
		}
		if record.RunID != identity.RunID || record.TurnID != identity.TurnID ||
			record.InputID != identity.InputID || record.RequestDigest != requestDigest {
			return usermessageruntime.ErrOutputContract
		}
		if record.ExecutionFence != identity.FenceToken {
			return usermessageruntime.ErrFenceLost
		}
		if record.Status != "reserved" {
			if record.Status == status && ((status == "completed" && record.ResponseDigest != nil && *record.ResponseDigest == digest) ||
				(status == "failed" && record.ErrorCode != nil && *record.ErrorCode == errorCode)) {
				return nil
			}
			return usermessageruntime.ErrOutputContract
		}
		updates := map[string]any{"status": status, "completed_at": databaseNow}
		if status == "completed" {
			updates["response_ciphertext"] = protected.Ciphertext
			updates["response_key_version"] = protected.KeyVersion
			updates["response_digest"] = digest
		} else {
			updates["error_code"] = errorCode
		}
		result := tx.Model(&userMessageModelReceiptModel{}).Where("model_call_id = ? AND status = 'reserved'", identity.ModelCallID).Updates(updates)
		if result.Error != nil || result.RowsAffected != 1 {
			return usermessageruntime.ErrFenceLost
		}
		return nil
	})
}

func claimFromModelIdentity(identity usermessageruntime.ModelReceiptIdentity) usermessageruntime.Claim {
	return usermessageruntime.Claim{
		Owner: identity.Owner, RunID: identity.RunID, ModelCallID: identity.ModelCallID,
		FenceToken: identity.FenceToken,
		Context: turncontext.UserMessageTurnContext{
			SessionID: identity.SessionID, InputID: identity.InputID, TurnID: identity.TurnID,
		},
	}
}

func (r *UserMessageRuntimeRepository) withActiveUserMessageFence(
	ctx context.Context,
	claim usermessageruntime.Claim,
	callback func(*gorm.DB, time.Time) error,
) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var databaseNow time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&databaseNow).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Raw(`
			SELECT COUNT(*)
			FROM agent.session_runtime_lease AS lease
			JOIN agent.session_input AS input_record ON input_record.session_id = lease.session_id
			JOIN agent.session_user_message_run AS run_record ON run_record.input_id = input_record.id
			WHERE lease.session_id = ? AND lease.lease_owner = ? AND lease.fence_token = ?
			  AND lease.lease_until > ? AND input_record.id = ?
			  AND input_record.lease_owner = ? AND input_record.fence_token = ?
			  AND run_record.run_id = ? AND run_record.owner_fence = ?`,
			claim.Context.SessionID, claim.Owner, claim.FenceToken, databaseNow,
			claim.Context.InputID, claim.Owner, claim.FenceToken, claim.RunID, claim.FenceToken).Scan(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return usermessageruntime.ErrFenceLost
		}
		return callback(tx, databaseNow)
	})
	return mapUserMessageRuntimeError(err)
}

func (r *UserMessageRuntimeRepository) assertActiveUserMessageFence(ctx context.Context, claim usermessageruntime.Claim) error {
	return r.withActiveUserMessageFence(ctx, claim, func(_ *gorm.DB, _ time.Time) error { return nil })
}

func decodeStoredUserMessageOutput(schemaVersion string, plaintext []byte) (usermessageruntime.Output, error) {
	switch schemaVersion {
	case usermessageruntime.DirectResponseCardSchemaVersion:
		card, err := usermessageruntime.DecodeDirectResponseCard(string(plaintext))
		if err != nil {
			return usermessageruntime.Output{}, err
		}
		return usermessageruntime.Output{DirectResponse: &card}, nil
	case usermessageruntime.FailureCardSchemaVersion:
		var card usermessageruntime.FailureCard
		if err := strictJSONDecode(plaintext, &card); err != nil {
			return usermessageruntime.Output{}, err
		}
		return usermessageruntime.Output{Failure: &card}, nil
	default:
		return usermessageruntime.Output{}, usermessageruntime.ErrOutputContract
	}
}

func strictJSONDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return usermessageruntime.ErrOutputContract
	}
	return nil
}

func canonicalRuntimeUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

func mapUserMessageRuntimeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, usermessageruntime.ErrFenceLost), errors.Is(err, usermessageruntime.ErrInvalidClaim),
		errors.Is(err, usermessageruntime.ErrOutputContract):
		return err
	default:
		return usermessageruntime.ErrPersistence
	}
}

var _ usermessageruntime.Repository = (*UserMessageRuntimeRepository)(nil)
var _ usermessageruntime.ModelReceiptStore = (*UserMessageRuntimeRepository)(nil)
