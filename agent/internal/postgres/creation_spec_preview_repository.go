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
	"strings"
	"time"

	previewchatmodel "github.com/FigoGoo/Dora-Agent/agent/internal/chatmodel"
	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	previewruntime "github.com/FigoGoo/Dora-Agent/agent/internal/runtime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const terminalReceiptSchemaVersion = "creation_spec.preview.tool-terminal.v1"

// previewContentProtector 使用与 Session Message 相同的 DRAE v1 Keyring，但每次解密都必须重验摘要。
type previewContentProtector interface {
	Protect(ctx context.Context, plaintext []byte) (session.ProtectedContent, error)
	Open(ctx context.Context, protected session.ProtectedContent, contentDigest string) ([]byte, error)
}

// CreationSpecPreviewRepository 是 Preview Session Lane、Tool Receipt 与 Workspace Projection 的 PostgreSQL 真源。
type CreationSpecPreviewRepository struct {
	db                 *gorm.DB
	protector          previewContentProtector
	maxBusinessResends int
}

// NewCreationSpecPreviewRepository 创建不执行 AutoMigrate 的 Preview Repository。
func NewCreationSpecPreviewRepository(
	client *Client,
	protector previewContentProtector,
	maxBusinessResends int,
) (*CreationSpecPreviewRepository, error) {
	if client == nil || client.db == nil || protector == nil || maxBusinessResends < 1 || maxBusinessResends > 20 {
		return nil, fmt.Errorf("create creation spec preview repository: postgres and protector are required")
	}
	return &CreationSpecPreviewRepository{
		db: client.db, protector: protector, maxBusinessResends: maxBusinessResends,
	}, nil
}

// LookupEnqueue 在 KMS/随机源/时钟之前只读查询已接受幂等键；最终并发仍由 Enqueue 事务锁收敛。
func (r *CreationSpecPreviewRepository) LookupEnqueue(
	ctx context.Context,
	idempotencyKey string,
	requestDigest string,
	userID string,
	projectID string,
	sessionID string,
) (*previewruntime.EnqueueResult, error) {
	if !canonicalPreviewUUIDv7(idempotencyKey) || !validPreviewDigest(requestDigest) ||
		!canonicalPreviewUUIDv7(userID) || !canonicalPreviewUUIDv7(projectID) || !canonicalPreviewUUIDv7(sessionID) {
		return nil, previewruntime.ErrInvalidInput
	}
	var existing creationSpecPreviewRunModel
	err := r.db.WithContext(ctx).Where("idempotency_key = ?", idempotencyKey).Take(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	case err != nil:
		return nil, mapPreviewRepositoryError(err)
	case existing.RequestDigest != requestDigest || existing.SessionID != sessionID ||
		existing.UserID != userID || existing.ProjectID != projectID:
		return nil, previewruntime.ErrIdempotencyConflict
	case !canonicalPreviewUUIDv7(existing.InputID) || !canonicalPreviewUUIDv7(existing.SessionID):
		return nil, previewruntime.ErrPersistence
	default:
		return &previewruntime.EnqueueResult{SessionID: existing.SessionID, InputID: existing.InputID, Status: "pending"}, nil
	}
}

// Enqueue 在一个短事务中创建 Message、Input、稳定 Run/Receipt 与既有 session.input.accepted Event。
func (r *CreationSpecPreviewRepository) Enqueue(
	ctx context.Context,
	plan previewruntime.EnqueuePlan,
) (previewruntime.EnqueueResult, error) {
	if err := validatePreviewEnqueuePlan(plan); err != nil {
		return previewruntime.EnqueueResult{}, err
	}
	var result previewruntime.EnqueueResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", plan.IdempotencyKey).Error; err != nil {
			return fmt.Errorf("lock preview idempotency key: %w", err)
		}

		var existing creationSpecPreviewRunModel
		err := tx.Where("idempotency_key = ?", plan.IdempotencyKey).Take(&existing).Error
		switch {
		case err == nil:
			if existing.RequestDigest != plan.RequestDigest || existing.SessionID != plan.SessionID ||
				existing.UserID != plan.UserID || existing.ProjectID != plan.ProjectID {
				return previewruntime.ErrIdempotencyConflict
			}
			result = previewruntime.EnqueueResult{SessionID: existing.SessionID, InputID: existing.InputID, Status: "pending"}
			return nil
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return fmt.Errorf("read preview idempotency receipt: %w", err)
		}

		var target sessionModel
		if err := tx.Where(
			"id = ? AND user_id = ? AND project_id = ? AND status = ? AND archived_at IS NULL",
			plan.SessionID, plan.UserID, plan.ProjectID, string(session.StatusActive),
		).Take(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return previewruntime.ErrNotFound
			}
			return fmt.Errorf("read preview target session: %w", err)
		}

		var sequence sessionSequenceCounterModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ?", plan.SessionID).Take(&sequence).Error; err != nil {
			return fmt.Errorf("lock preview sequence counter: %w", err)
		}
		var blockedInput sessionInputModel
		blockedErr := tx.Select("id").Where(
			"session_id = ? AND source_type <> ? AND status NOT IN ?",
			plan.SessionID, string(session.InputSourceTypeCreationSpecPreview),
			[]string{string(session.InputStatusResolved), string(session.InputStatusDead)},
		).Order("enqueue_seq ASC").Take(&blockedInput).Error
		switch {
		case blockedErr == nil:
			// Preview Processor 绝不 Claim/skip/改写 legacy user_message；该事务在任何计数器、Message、Input 或 Event 写入前失败。
			return previewruntime.ErrSessionLaneBlocked
		case !errors.Is(blockedErr, gorm.ErrRecordNotFound):
			return fmt.Errorf("read preview session lane predecessor: %w", blockedErr)
		}
		messageSeq := sequence.LastMessageSeq + 1
		enqueueSeq := sequence.LastInputEnqueueSeq + 1
		if messageSeq < 1 || enqueueSeq < 1 {
			return fmt.Errorf("allocate preview sequence: counter overflow")
		}
		updated := tx.Model(&sessionSequenceCounterModel{}).Where(
			"session_id = ? AND last_message_seq = ? AND last_input_enqueue_seq = ?",
			plan.SessionID, sequence.LastMessageSeq, sequence.LastInputEnqueueSeq,
		).Updates(map[string]any{
			"last_message_seq": messageSeq, "last_input_enqueue_seq": enqueueSeq, "updated_at": plan.CreatedAt,
		})
		if updated.Error != nil || updated.RowsAffected != 1 {
			return fmt.Errorf("advance preview sequence counter: concurrent mutation")
		}

		message := sessionMessageModel{
			ID: plan.MessageID, SessionID: plan.SessionID, MessageSeq: messageSeq,
			Role: string(session.MessageRoleUser), ContentCiphertext: append([]byte(nil), plan.Content.Ciphertext...),
			ContentKeyVersion: plan.Content.KeyVersion, ContentDigest: plan.RequestDigest,
			SourceKind: event.SourceKindCreationSpecPreview, SourceID: plan.IdempotencyKey, CreatedAt: plan.CreatedAt,
		}
		if err := tx.Create(&message).Error; err != nil {
			return fmt.Errorf("create preview message: %w", err)
		}
		messageID := plan.MessageID
		input := sessionInputModel{
			ID: plan.InputID, SessionID: plan.SessionID, SourceType: string(session.InputSourceTypeCreationSpecPreview),
			SourceID: plan.IdempotencyKey, MessageID: &messageID, Status: string(session.InputStatusPending),
			EnqueueSeq: enqueueSeq, Attempts: 0, AvailableAt: plan.CreatedAt, FenceToken: 0,
			CreatedAt: plan.CreatedAt, UpdatedAt: plan.CreatedAt,
		}
		if err := tx.Create(&input).Error; err != nil {
			return fmt.Errorf("create preview input: %w", err)
		}
		run := creationSpecPreviewRunModel{
			InputID: plan.InputID, RequestID: plan.RequestID, IdempotencyKey: plan.IdempotencyKey,
			RequestDigest: plan.RequestDigest, SessionID: plan.SessionID, UserID: plan.UserID, ProjectID: plan.ProjectID,
			MessageID: plan.MessageID, TurnID: plan.TurnID, RunID: plan.RunID, ToolCallID: plan.ToolCallID,
			BusinessCommandID: plan.BusinessCommandID, TerminalEventID: plan.TerminalEventID, PromptVersion: plan.PromptVersion,
			ValidatorVersion: plan.ValidatorVersion, CreatedAt: plan.CreatedAt, UpdatedAt: plan.CreatedAt,
		}
		if err := tx.Create(&run).Error; err != nil {
			return fmt.Errorf("create preview run: %w", err)
		}
		toolReceipt := creationSpecPreviewToolReceiptModel{
			ToolCallID: plan.ToolCallID, RequestDigest: plan.RequestDigest, Stage: "pending",
			BusinessCommandID: plan.BusinessCommandID, CreatedAt: plan.CreatedAt, UpdatedAt: plan.CreatedAt,
		}
		if err := tx.Create(&toolReceipt).Error; err != nil {
			return fmt.Errorf("create preview tool receipt: %w", err)
		}

		var counter sessionEventCounterModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", plan.SessionID).Take(&counter).Error; err != nil {
			return fmt.Errorf("lock preview event counter: %w", err)
		}
		eventSeq := counter.LastSeq + 1
		accepted, err := event.NewCreationSpecPreviewInputAccepted(
			plan.EventID, plan.SessionID, plan.InputID, plan.MessageID, plan.IdempotencyKey,
			string(session.InputStatusPending), enqueueSeq, plan.CreatedAt,
		)
		if err != nil {
			return err
		}
		if err := tx.Create(&sessionEventLogModel{
			EventID: accepted.EventID, SessionID: accepted.SessionID, Seq: eventSeq,
			EventType: string(accepted.Type), SchemaVersion: accepted.SchemaVersion,
			SourceKind: accepted.SourceKind, SourceID: accepted.SourceID, ProjectionIndex: accepted.ProjectionIndex,
			AggregateType: string(accepted.AggregateType), AggregateID: accepted.AggregateID,
			AggregateVersion: accepted.AggregateVersion, Payload: string(accepted.PayloadJSON), CreatedAt: accepted.CreatedAt,
		}).Error; err != nil {
			return fmt.Errorf("create preview accepted event: %w", err)
		}
		counterUpdate := tx.Model(&sessionEventCounterModel{}).
			Where("session_id = ? AND last_seq = ?", plan.SessionID, counter.LastSeq).
			Updates(map[string]any{"last_seq": eventSeq, "updated_at": plan.CreatedAt})
		if counterUpdate.Error != nil || counterUpdate.RowsAffected != 1 {
			return fmt.Errorf("advance preview event counter: concurrent mutation")
		}
		result = previewruntime.EnqueueResult{SessionID: plan.SessionID, InputID: plan.InputID, Status: "pending"}
		return nil
	})
	if err != nil {
		return previewruntime.EnqueueResult{}, mapPreviewRepositoryError(err)
	}
	return result, nil
}

// previewClaimRow 是一次加锁查询返回的 HOL、稳定 Run 与受保护 Intent。
type previewClaimRow struct {
	RequestID         string    `gorm:"column:request_id"`
	RequestDigest     string    `gorm:"column:request_digest"`
	SessionID         string    `gorm:"column:session_id"`
	UserID            string    `gorm:"column:user_id"`
	ProjectID         string    `gorm:"column:project_id"`
	InputID           string    `gorm:"column:input_id"`
	MessageID         string    `gorm:"column:message_id"`
	TurnID            string    `gorm:"column:turn_id"`
	RunID             string    `gorm:"column:run_id"`
	ToolCallID        string    `gorm:"column:tool_call_id"`
	BusinessCommandID string    `gorm:"column:business_command_id"`
	TerminalEventID   string    `gorm:"column:terminal_event_id"`
	PromptVersion     string    `gorm:"column:prompt_version"`
	ValidatorVersion  string    `gorm:"column:validator_version"`
	Attempts          int       `gorm:"column:attempts"`
	InputStatus       string    `gorm:"column:input_status"`
	CurrentFenceToken int64     `gorm:"column:current_fence_token"`
	ContentCiphertext []byte    `gorm:"column:content_ciphertext"`
	ContentKeyVersion string    `gorm:"column:content_key_version"`
	ContentDigest     string    `gorm:"column:content_digest"`
	LeaseVersion      int64     `gorm:"column:lease_version"`
	InputAvailableAt  time.Time `gorm:"column:available_at"`
}

// ClaimNext 使用 SKIP LOCKED 领取全局一个可运行 Session 的真正 HOL，并同时递增 Session/Input Fence。
func (r *CreationSpecPreviewRepository) ClaimNext(
	ctx context.Context,
	owner string,
	now time.Time,
	leaseDuration time.Duration,
) (*previewruntime.Claim, error) {
	if strings.TrimSpace(owner) == "" || now.IsZero() || leaseDuration <= 0 {
		return nil, previewruntime.ErrInvalidInput
	}
	var row previewClaimRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := `
			SELECT run.request_id, run.request_digest, run.session_id, run.user_id, run.project_id,
			       input_record.id AS input_id, run.message_id, run.turn_id, run.run_id,
			       run.tool_call_id, run.business_command_id, run.terminal_event_id,
			       run.prompt_version, run.validator_version,
			       input_record.attempts, input_record.status AS input_status,
			       lane.fence_token AS current_fence_token, lane.version AS lease_version,
			       message.content_ciphertext, message.content_key_version, message.content_digest,
			       input_record.available_at
			FROM agent.session_input AS input_record
			JOIN agent.creation_spec_preview_run AS run ON run.input_id = input_record.id
			JOIN agent.creation_spec_preview_tool_receipt AS receipt ON receipt.tool_call_id = run.tool_call_id
			JOIN agent.session_runtime_lease AS lane ON lane.session_id = input_record.session_id
			JOIN agent.session_message AS message ON message.id = run.message_id
			JOIN agent.session AS session_record ON session_record.id = input_record.session_id
			WHERE input_record.status IN ('pending', 'retry_wait', 'recovery_pending', 'claimed', 'running')
			  AND input_record.available_at <= CURRENT_TIMESTAMP
			  AND (lane.lease_until IS NULL OR lane.lease_until <= CURRENT_TIMESTAMP)
			  AND (
			      input_record.status IN ('pending', 'retry_wait', 'recovery_pending')
			      OR input_record.lease_until IS NULL OR input_record.lease_until <= CURRENT_TIMESTAMP
			  )
			  AND session_record.status = 'active' AND session_record.archived_at IS NULL
			  AND receipt.stage <> 'business_resend_exhausted'
			  AND NOT EXISTS (
			      SELECT 1 FROM agent.session_input AS earlier
			      WHERE earlier.session_id = input_record.session_id
			        AND earlier.enqueue_seq < input_record.enqueue_seq
			        AND earlier.status NOT IN ('resolved', 'dead')
			  )
			ORDER BY input_record.available_at ASC, input_record.session_id ASC, input_record.enqueue_seq ASC
			LIMIT 1
			FOR UPDATE OF input_record, lane SKIP LOCKED`
		if err := tx.Raw(query).Scan(&row).Error; err != nil {
			return fmt.Errorf("claim preview HOL: %w", err)
		}
		if row.InputID == "" {
			return nil
		}
		newFence := row.CurrentFenceToken + 1
		if newFence < 1 {
			return fmt.Errorf("claim preview HOL: fence overflow")
		}
		leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
			Where("session_id = ? AND fence_token = ? AND version = ?", row.SessionID, row.CurrentFenceToken, row.LeaseVersion).
			Updates(map[string]any{
				"lease_owner": owner,
				"lease_until": gorm.Expr("CURRENT_TIMESTAMP + (? * INTERVAL '1 microsecond')", leaseDuration.Microseconds()),
				"fence_token": newFence, "version": row.LeaseVersion + 1, "updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
			})
		if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
			return previewruntime.ErrFenceLost
		}
		nextAttempts := row.Attempts
		// pending/retry_wait 是新执行，expired claimed/running 是崩溃后执行接管，均计入 execution max。
		// 只有已持久化原命令或已冻结终态的 recovery_pending 不消耗执行预算。
		if row.InputStatus != string(session.InputStatusRecoveryPending) {
			nextAttempts++
		}
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND status IN ?", row.InputID, []string{
				string(session.InputStatusPending), string(session.InputStatusRetryWait), string(session.InputStatusRecoveryPending),
				string(session.InputStatusClaimed), string(session.InputStatusRunning),
			}).Updates(map[string]any{
			"status": string(session.InputStatusClaimed), "attempts": nextAttempts,
			"lease_owner": owner,
			"lease_until": gorm.Expr("CURRENT_TIMESTAMP + (? * INTERVAL '1 microsecond')", leaseDuration.Microseconds()),
			"fence_token": newFence, "updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return previewruntime.ErrFenceLost
		}
		row.CurrentFenceToken = newFence
		row.Attempts = nextAttempts
		return nil
	})
	if err != nil {
		return nil, mapPreviewRepositoryError(err)
	}
	if row.InputID == "" {
		return nil, nil
	}
	claim := &previewruntime.Claim{
		Owner: owner, RequestID: row.RequestID, RequestDigest: row.RequestDigest, SessionID: row.SessionID,
		UserID: row.UserID, ProjectID: row.ProjectID, InputID: row.InputID, MessageID: row.MessageID,
		TurnID: row.TurnID, RunID: row.RunID, ToolCallID: row.ToolCallID,
		BusinessCommandID: row.BusinessCommandID, TerminalEventID: row.TerminalEventID, FenceToken: row.CurrentFenceToken,
		PromptVersion: row.PromptVersion, ValidatorVersion: row.ValidatorVersion,
		Attempts: row.Attempts,
	}
	intent, err := r.protector.Open(ctx, session.ProtectedContent{
		Ciphertext: row.ContentCiphertext, KeyVersion: row.ContentKeyVersion,
	}, row.ContentDigest)
	if err != nil || row.ContentDigest != row.RequestDigest {
		claim.Poisoned = true
		return claim, nil
	}
	if _, err := plancreationspec.DecodeIntent(intent); err != nil {
		claim.Poisoned = true
		return claim, nil
	}
	claim.Intent = append([]byte(nil), intent...)
	return claim, nil
}

// MarkRunning 仅允许当前 Session Lane owner+fence 把 claimed 推进 running。
func (r *CreationSpecPreviewRepository) MarkRunning(
	ctx context.Context,
	claim previewruntime.Claim,
	now time.Time,
) error {
	if now.IsZero() {
		return previewruntime.ErrInvalidInput
	}
	result := r.db.WithContext(ctx).Model(&sessionInputModel{}).
		Where(`id = ? AND session_id = ? AND status = 'claimed' AND lease_owner = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP
			AND EXISTS (SELECT 1 FROM agent.session_runtime_lease AS lane
			WHERE lane.session_id = ? AND lane.lease_owner = ? AND lane.fence_token = ? AND lane.lease_until > CURRENT_TIMESTAMP)`,
			claim.InputID, claim.SessionID, claimLeaseOwner(claim), claim.FenceToken,
			claim.SessionID, claimLeaseOwner(claim), claim.FenceToken,
		).Updates(map[string]any{"status": string(session.InputStatusRunning), "updated_at": gorm.Expr("CURRENT_TIMESTAMP")})
	if result.Error != nil {
		return mapPreviewRepositoryError(result.Error)
	}
	if result.RowsAffected != 1 {
		return previewruntime.ErrFenceLost
	}
	return nil
}

// RenewLease 在一个短事务内延长 Session 与 Input 的同一 fence；任一 CAS 失败即整体回滚。
func (r *CreationSpecPreviewRepository) RenewLease(
	ctx context.Context,
	claim previewruntime.Claim,
	now time.Time,
	leaseDuration time.Duration,
) error {
	if now.IsZero() || leaseDuration <= 0 {
		return previewruntime.ErrInvalidInput
	}
	owner := claimLeaseOwner(claim)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		input := tx.Model(&sessionInputModel{}).
			Where("id = ? AND session_id = ? AND lease_owner = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP AND status IN ?",
				claim.InputID, claim.SessionID, owner, claim.FenceToken,
				[]string{string(session.InputStatusClaimed), string(session.InputStatusRunning)},
			).Updates(map[string]any{
			"lease_until": gorm.Expr("CURRENT_TIMESTAMP + (? * INTERVAL '1 microsecond')", leaseDuration.Microseconds()),
			"updated_at":  gorm.Expr("CURRENT_TIMESTAMP"),
		})
		if input.Error != nil || input.RowsAffected != 1 {
			return previewruntime.ErrFenceLost
		}
		lease := tx.Model(&sessionRuntimeLeaseModel{}).
			Where("session_id = ? AND lease_owner = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP", claim.SessionID, owner, claim.FenceToken).
			Updates(map[string]any{
				"lease_until": gorm.Expr("CURRENT_TIMESTAMP + (? * INTERVAL '1 microsecond')", leaseDuration.Microseconds()),
				"updated_at":  gorm.Expr("CURRENT_TIMESTAMP"),
			})
		if lease.Error != nil || lease.RowsAffected != 1 {
			return previewruntime.ErrFenceLost
		}
		return nil
	})
	return mapPreviewRepositoryError(err)
}

// terminalReceiptPayload 是 AEAD 密文内部的完整终态；Tool JSON 不暴露 Card 与业务摘要。
type terminalReceiptPayload struct {
	SchemaVersion         string                  `json:"schema_version"`
	CompletionDisposition string                  `json:"completion_disposition"`
	Result                plancreationspec.Result `json:"result"`
	Card                  *plancreationspec.Card  `json:"card"`
	BusinessRequestDigest string                  `json:"business_request_digest"`
}

// ReplayTerminal 实现 Graph Tool first-write-wins 重放端口。
func (r *CreationSpecPreviewRepository) ReplayTerminal(
	ctx context.Context,
	trusted plancreationspec.TrustedContext,
) (*plancreationspec.Result, error) {
	var receipt creationSpecPreviewToolReceiptModel
	err := r.db.WithContext(ctx).Where("tool_call_id = ?", trusted.ToolCallID).Take(&receipt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, previewruntime.ErrPersistence
	}
	if err != nil {
		return nil, mapPreviewRepositoryError(err)
	}
	if receipt.BusinessCommandID != trusted.BusinessCommandID {
		return nil, previewruntime.ErrIdempotencyConflict
	}
	if receipt.Stage == "pending" || receipt.Stage == "business_prepared" || receipt.Stage == "business_unknown" ||
		receipt.Stage == "business_resend_exhausted" {
		return nil, nil
	}
	return r.openTerminal(ctx, receipt, trusted)
}

// ReplayRecovery 在当前有效 fence 下认证解密完整原命令，并用当前 Owner/Fence 重建可信上下文。
func (r *CreationSpecPreviewRepository) ReplayRecovery(
	ctx context.Context,
	trusted plancreationspec.TrustedContext,
) (*plancreationspec.RecoveryDeferred, error) {
	var receipt creationSpecPreviewToolReceiptModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockActivePreviewFence(tx, previewFenceIdentity{
			Owner: trusted.Owner, SessionID: trusted.SessionID, InputID: trusted.InputID,
			ToolCallID: trusted.ToolCallID, FenceToken: trusted.FenceToken,
		}); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"tool_call_id = ?", trusted.ToolCallID,
		).Take(&receipt).Error; err != nil {
			return fmt.Errorf("lock preview recovery receipt: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, mapPreviewRepositoryError(err)
	}
	if receipt.BusinessCommandID != trusted.BusinessCommandID {
		return nil, previewruntime.ErrIdempotencyConflict
	}
	if receipt.Stage == "pending" || receipt.Stage == "completed" || receipt.Stage == "failed" {
		return nil, nil
	}
	if (receipt.Stage != "business_prepared" && receipt.Stage != "business_unknown" &&
		receipt.Stage != "business_resend_exhausted") || !validDurableBusinessCommandReceipt(receipt) {
		return nil, previewruntime.ErrPersistence
	}
	plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
		Ciphertext: receipt.BusinessCommandCiphertext, KeyVersion: *receipt.BusinessCommandKeyVersion,
	}, *receipt.BusinessCommandPayloadDigest)
	if err != nil {
		return nil, previewruntime.ErrPersistence
	}
	payloadHash := sha256.Sum256(plaintext)
	if hex.EncodeToString(payloadHash[:]) != *receipt.BusinessCommandPayloadDigest {
		return nil, previewruntime.ErrPersistence
	}
	command, err := plancreationspec.DecodeDurableDraftCommand(plaintext, trusted)
	if err != nil || command.RequestDigest != *receipt.BusinessRequestDigest {
		return nil, previewruntime.ErrPersistence
	}
	contentDigest, err := plancreationspec.ContentDigest(command.Content)
	if err != nil || contentDigest != *receipt.BusinessContentDigest {
		return nil, previewruntime.ErrPersistence
	}
	return &plancreationspec.RecoveryDeferred{
		ToolCallID: trusted.ToolCallID, BusinessCommandID: trusted.BusinessCommandID,
		RequestDigest: *receipt.BusinessRequestDigest, ContentDigest: *receipt.BusinessContentDigest,
		Command: command, ResendAttempts: receipt.BusinessResendAttempts,
		ResendLimit:     *receipt.BusinessResendLimit,
		ResendExhausted: receipt.Stage == "business_resend_exhausted",
	}, nil
}

// PrepareCommand 在 Save RPC 前加密冻结完整稳定命令；密文不包含易变 Owner/Fence。
func (r *CreationSpecPreviewRepository) PrepareCommand(
	ctx context.Context,
	command plancreationspec.DraftCommand,
) error {
	encoded, payloadDigest, contentDigest, err := plancreationspec.EncodeDurableDraftCommand(command)
	if err != nil || !validPreviewDigest(payloadDigest) || !validPreviewDigest(contentDigest) {
		return previewruntime.ErrInvalidInput
	}
	protected, err := r.protector.Protect(ctx, encoded)
	if err != nil {
		return mapPreviewRepositoryError(err)
	}
	trusted := command.TrustedContext
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockActivePreviewFence(tx, previewFenceIdentity{
			Owner: trusted.Owner, SessionID: trusted.SessionID, InputID: trusted.InputID,
			ToolCallID: trusted.ToolCallID, FenceToken: trusted.FenceToken,
		}); err != nil {
			return err
		}
		var receipt creationSpecPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"tool_call_id = ?", trusted.ToolCallID,
		).Take(&receipt).Error; err != nil {
			return fmt.Errorf("lock preview command receipt: %w", err)
		}
		if receipt.BusinessCommandID != trusted.BusinessCommandID {
			return previewruntime.ErrIdempotencyConflict
		}
		if receipt.Stage == "business_prepared" || receipt.Stage == "business_unknown" ||
			receipt.Stage == "business_resend_exhausted" {
			if !validDurableBusinessCommandReceipt(receipt) ||
				*receipt.BusinessRequestDigest != command.RequestDigest ||
				*receipt.BusinessContentDigest != contentDigest ||
				*receipt.BusinessCommandPayloadDigest != payloadDigest {
				return previewruntime.ErrIdempotencyConflict
			}
			return nil
		}
		if receipt.Stage != "pending" {
			return previewruntime.ErrIdempotencyConflict
		}
		update := tx.Model(&creationSpecPreviewToolReceiptModel{}).Where(
			"tool_call_id = ? AND stage = 'pending'", trusted.ToolCallID,
		).Updates(map[string]any{
			"stage": "business_prepared", "business_request_digest": command.RequestDigest,
			"business_content_digest":         contentDigest,
			"business_command_ciphertext":     protected.Ciphertext,
			"business_command_key_version":    protected.KeyVersion,
			"business_command_payload_digest": payloadDigest,
			"business_resend_attempts":        0, "business_resend_limit": r.maxBusinessResends,
			"business_last_resend_at": nil, "business_resend_exhausted_at": nil,
			"updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
		if update.Error != nil || update.RowsAffected != 1 {
			return previewruntime.ErrIdempotencyConflict
		}
		return nil
	})
	return mapPreviewRepositoryError(err)
}

// ReserveCommandResend 先锁当前 Fence 与 Receipt，再原子预留一次重发预算；进程崩溃也不会超发。
func (r *CreationSpecPreviewRepository) ReserveCommandResend(
	ctx context.Context,
	trusted plancreationspec.TrustedContext,
	recovery plancreationspec.RecoveryDeferred,
) (plancreationspec.RecoveryDeferred, bool, error) {
	encoded, payloadDigest, contentDigest, err := plancreationspec.EncodeDurableDraftCommand(recovery.Command)
	if err != nil || recovery.Command.TrustedContext != trusted ||
		recovery.ToolCallID != trusted.ToolCallID || recovery.BusinessCommandID != trusted.BusinessCommandID ||
		recovery.RequestDigest != recovery.Command.RequestDigest || len(encoded) == 0 {
		return plancreationspec.RecoveryDeferred{}, false, previewruntime.ErrInvalidInput
	}
	reserved := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockActivePreviewFence(tx, previewFenceIdentity{
			Owner: trusted.Owner, SessionID: trusted.SessionID, InputID: trusted.InputID,
			ToolCallID: trusted.ToolCallID, FenceToken: trusted.FenceToken,
		}); err != nil {
			return err
		}
		var receipt creationSpecPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"tool_call_id = ?", trusted.ToolCallID,
		).Take(&receipt).Error; err != nil {
			return fmt.Errorf("lock preview resend receipt: %w", err)
		}
		if receipt.BusinessCommandID != trusted.BusinessCommandID || !validDurableBusinessCommandReceipt(receipt) ||
			*receipt.BusinessRequestDigest != recovery.RequestDigest ||
			*receipt.BusinessContentDigest != contentDigest ||
			*receipt.BusinessCommandPayloadDigest != payloadDigest {
			return previewruntime.ErrIdempotencyConflict
		}
		recovery.ResendAttempts = receipt.BusinessResendAttempts
		recovery.ResendLimit = *receipt.BusinessResendLimit
		if receipt.Stage == "business_resend_exhausted" || receipt.BusinessResendAttempts >= *receipt.BusinessResendLimit {
			if receipt.Stage != "business_resend_exhausted" {
				update := tx.Model(&creationSpecPreviewToolReceiptModel{}).Where(
					"tool_call_id = ? AND stage IN ?", trusted.ToolCallID,
					[]string{"business_prepared", "business_unknown"},
				).Updates(map[string]any{
					"stage":                        "business_resend_exhausted",
					"error_code":                   plancreationspec.RecoveryCodeBusinessResendExhausted,
					"business_resend_exhausted_at": gorm.Expr("CURRENT_TIMESTAMP"),
					"updated_at":                   gorm.Expr("CURRENT_TIMESTAMP"),
				})
				if update.Error != nil || update.RowsAffected != 1 {
					return previewruntime.ErrIdempotencyConflict
				}
			}
			recovery.ResendExhausted = true
			return nil
		}
		nextAttempts := receipt.BusinessResendAttempts + 1
		update := tx.Model(&creationSpecPreviewToolReceiptModel{}).Where(
			"tool_call_id = ? AND stage IN ? AND business_resend_attempts = ?", trusted.ToolCallID,
			[]string{"business_prepared", "business_unknown"}, receipt.BusinessResendAttempts,
		).Updates(map[string]any{
			"stage": "business_unknown", "business_resend_attempts": nextAttempts,
			"business_last_resend_at": gorm.Expr("CURRENT_TIMESTAMP"),
			"updated_at":              gorm.Expr("CURRENT_TIMESTAMP"),
		})
		if update.Error != nil || update.RowsAffected != 1 {
			return previewruntime.ErrIdempotencyConflict
		}
		recovery.ResendAttempts = nextAttempts
		recovery.ResendExhausted = false
		reserved = true
		return nil
	})
	if err != nil {
		return plancreationspec.RecoveryDeferred{}, false, mapPreviewRepositoryError(err)
	}
	return recovery, reserved, nil
}

// FreezeTerminal 先加密完整终态，再以 receipt row lock 完成 first-write-wins CAS。
func (r *CreationSpecPreviewRepository) FreezeTerminal(
	ctx context.Context,
	trusted plancreationspec.TrustedContext,
	result plancreationspec.Result,
) error {
	disposition := previewruntime.CompletionResolved
	if plancreationspec.IsDeadLetterFailure(result) {
		disposition = previewruntime.CompletionDead
	}
	return r.freezeTerminal(ctx, trusted, result, disposition)
}

// FreezeExecutionFailure 把 poison、协议不变量或执行耗尽冻结为可投影的 dead-letter failed Result。
func (r *CreationSpecPreviewRepository) FreezeExecutionFailure(
	ctx context.Context,
	claim previewruntime.Claim,
	result plancreationspec.Result,
) error {
	if result.Status != "failed" {
		return previewruntime.ErrInvalidInput
	}
	return r.freezeTerminal(ctx, trustedFromClaim(claim), result, previewruntime.CompletionDead)
}

func (r *CreationSpecPreviewRepository) freezeTerminal(
	ctx context.Context,
	trusted plancreationspec.TrustedContext,
	result plancreationspec.Result,
	disposition previewruntime.CompletionDisposition,
) error {
	if err := plancreationspec.ValidateTerminalResult(result, trusted); err != nil {
		return previewruntime.ErrInvalidInput
	}
	if disposition != previewruntime.CompletionResolved && disposition != previewruntime.CompletionDead {
		return previewruntime.ErrInvalidInput
	}
	payload := terminalReceiptPayload{
		SchemaVersion: terminalReceiptSchemaVersion, CompletionDisposition: string(disposition), Result: result, Card: result.Card,
		BusinessRequestDigest: result.BusinessRequestDigest,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return previewruntime.ErrPersistence
	}
	digest := sha256.Sum256(encoded)
	digestHex := hex.EncodeToString(digest[:])
	protected, err := r.protector.Protect(ctx, encoded)
	if err != nil {
		return mapPreviewRepositoryError(err)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockActivePreviewFence(tx, previewFenceIdentity{
			Owner: trusted.Owner, SessionID: trusted.SessionID, InputID: trusted.InputID,
			ToolCallID: trusted.ToolCallID, FenceToken: trusted.FenceToken,
		}); err != nil {
			return err
		}
		var receipt creationSpecPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", trusted.ToolCallID).Take(&receipt).Error; err != nil {
			return fmt.Errorf("lock preview tool receipt: %w", err)
		}
		if receipt.BusinessCommandID != trusted.BusinessCommandID {
			return previewruntime.ErrIdempotencyConflict
		}
		if receipt.Stage == "completed" || receipt.Stage == "failed" {
			if receipt.ResultDigest == nil || *receipt.ResultDigest != digestHex {
				return previewruntime.ErrIdempotencyConflict
			}
			return nil
		}
		// Save 命令一旦 prepared/unknown，可能已在 Business 提交；绝不允许 poison/协议/技术耗尽覆盖为 dead-letter。
		if disposition == previewruntime.CompletionDead && receipt.Stage != "pending" {
			return previewruntime.ErrIdempotencyConflict
		}
		if result.Status == "failed" && receipt.Stage != "pending" &&
			!plancreationspec.IsAuthoritativeBusinessFailure(result) {
			return previewruntime.ErrIdempotencyConflict
		}
		if receipt.Stage != "pending" && receipt.Stage != "business_prepared" && receipt.Stage != "business_unknown" {
			return previewruntime.ErrIdempotencyConflict
		}
		if result.Status == "completed" {
			if receipt.Stage == "pending" || receipt.BusinessRequestDigest == nil || receipt.BusinessContentDigest == nil ||
				*receipt.BusinessRequestDigest != result.BusinessRequestDigest || result.Card == nil ||
				*receipt.BusinessContentDigest != result.Card.ContentDigest {
				return previewruntime.ErrIdempotencyConflict
			}
		}
		if receipt.BusinessRequestDigest != nil && result.BusinessRequestDigest != "" &&
			*receipt.BusinessRequestDigest != result.BusinessRequestDigest {
			return previewruntime.ErrIdempotencyConflict
		}
		stage := result.Status
		updates := map[string]any{
			"stage": stage, "result_ciphertext": protected.Ciphertext,
			"result_key_version": protected.KeyVersion, "result_digest": digestHex, "updated_at": time.Now().UTC(),
		}
		if result.BusinessRequestDigest != "" {
			updates["business_request_digest"] = result.BusinessRequestDigest
		}
		if result.Status == "failed" {
			updates["error_code"] = result.ResultCode
		} else {
			updates["error_code"] = nil
		}
		update := tx.Model(&creationSpecPreviewToolReceiptModel{}).
			Where("tool_call_id = ? AND stage IN ?", trusted.ToolCallID, []string{"pending", "business_prepared", "business_unknown"}).
			Updates(updates)
		if update.Error != nil || update.RowsAffected != 1 {
			return previewruntime.ErrIdempotencyConflict
		}
		return nil
	})
	return mapPreviewRepositoryError(err)
}

// MarkRecovery 只冻结原 Business 请求摘要和 unknown 阶段，不生成 result_ciphertext。
func (r *CreationSpecPreviewRepository) MarkRecovery(
	ctx context.Context,
	trusted plancreationspec.TrustedContext,
	recovery plancreationspec.RecoveryDeferred,
) error {
	if recovery.ToolCallID != trusted.ToolCallID || recovery.BusinessCommandID != trusted.BusinessCommandID ||
		!validPreviewDigest(recovery.RequestDigest) || !validPreviewDigest(recovery.ContentDigest) ||
		recovery.Command.TrustedContext != trusted || recovery.Command.RequestDigest != recovery.RequestDigest {
		return previewruntime.ErrInvalidInput
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockActivePreviewFence(tx, previewFenceIdentity{
			Owner: trusted.Owner, SessionID: trusted.SessionID, InputID: trusted.InputID,
			ToolCallID: trusted.ToolCallID, FenceToken: trusted.FenceToken,
		}); err != nil {
			return err
		}
		var receipt creationSpecPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", trusted.ToolCallID).Take(&receipt).Error; err != nil {
			return fmt.Errorf("lock preview recovery receipt: %w", err)
		}
		if receipt.BusinessCommandID != trusted.BusinessCommandID {
			return previewruntime.ErrIdempotencyConflict
		}
		if receipt.Stage == "business_resend_exhausted" {
			if !validDurableBusinessCommandReceipt(receipt) ||
				*receipt.BusinessRequestDigest != recovery.RequestDigest ||
				*receipt.BusinessContentDigest != recovery.ContentDigest || receipt.ErrorCode == nil ||
				*receipt.ErrorCode != plancreationspec.RecoveryCodeBusinessResendExhausted {
				return previewruntime.ErrIdempotencyConflict
			}
			return nil
		}
		if (receipt.Stage != "business_prepared" && receipt.Stage != "business_unknown") ||
			!validDurableBusinessCommandReceipt(receipt) ||
			*receipt.BusinessRequestDigest != recovery.RequestDigest ||
			*receipt.BusinessContentDigest != recovery.ContentDigest {
			return previewruntime.ErrIdempotencyConflict
		}
		if recovery.ResendExhausted {
			if receipt.BusinessResendAttempts < *receipt.BusinessResendLimit {
				return previewruntime.ErrIdempotencyConflict
			}
			update := tx.Model(&creationSpecPreviewToolReceiptModel{}).
				Where("tool_call_id = ? AND stage IN ?", trusted.ToolCallID, []string{"business_prepared", "business_unknown"}).
				Updates(map[string]any{
					"stage":                        "business_resend_exhausted",
					"error_code":                   plancreationspec.RecoveryCodeBusinessResendExhausted,
					"business_resend_exhausted_at": gorm.Expr("CURRENT_TIMESTAMP"),
					"updated_at":                   gorm.Expr("CURRENT_TIMESTAMP"),
				})
			if update.Error != nil || update.RowsAffected != 1 {
				return previewruntime.ErrIdempotencyConflict
			}
			return nil
		}
		if receipt.Stage == "business_unknown" {
			return nil
		}
		update := tx.Model(&creationSpecPreviewToolReceiptModel{}).
			Where("tool_call_id = ? AND stage = 'business_prepared'", trusted.ToolCallID).
			Updates(map[string]any{
				"stage": "business_unknown", "updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
			})
		if update.Error != nil || update.RowsAffected != 1 {
			return previewruntime.ErrIdempotencyConflict
		}
		return nil
	})
	return mapPreviewRepositoryError(err)
}

// ReplayOrReserveModel 创建 pending 占位或解密 first-write-wins completed 响应。
func (r *CreationSpecPreviewRepository) ReplayOrReserveModel(
	ctx context.Context,
	identity previewchatmodel.ReceiptIdentity,
	callIndex int,
	requestDigest string,
) (*schema.Message, bool, error) {
	if !validModelReceiptIdentity(identity) || callIndex < 1 || callIndex > 3 || !validPreviewDigest(requestDigest) {
		return nil, false, previewruntime.ErrInvalidInput
	}
	toolCallID := identity.ToolCallID
	var receipt creationSpecPreviewModelReceiptModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockActivePreviewFence(tx, previewFenceIdentity{
			Owner: identity.Owner, SessionID: identity.SessionID, InputID: identity.InputID,
			ToolCallID: identity.ToolCallID, FenceToken: identity.FenceToken,
		}); err != nil {
			return err
		}
		candidate := creationSpecPreviewModelReceiptModel{
			ToolCallID: toolCallID, CallIndex: callIndex, RequestDigest: requestDigest,
			Status: "pending", CreatedAt: time.Now().UTC(),
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "tool_call_id"}, {Name: "call_index"}},
			DoNothing: true,
		}).Create(&candidate).Error; err != nil {
			return fmt.Errorf("reserve preview model receipt: %w", err)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"tool_call_id = ? AND call_index = ?", toolCallID, callIndex,
		).Take(&receipt).Error; err != nil {
			return fmt.Errorf("lock preview model receipt: %w", err)
		}
		if receipt.RequestDigest != requestDigest {
			return previewruntime.ErrIdempotencyConflict
		}
		return nil
	})
	if err != nil {
		return nil, false, mapPreviewRepositoryError(err)
	}
	if receipt.Status == "pending" {
		return nil, true, nil
	}
	if receipt.Status != "completed" || receipt.ResponseKeyVersion == nil ||
		receipt.ResponseDigest == nil || len(receipt.ResponseCiphertext) == 0 {
		return nil, false, previewruntime.ErrPersistence
	}
	plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
		Ciphertext: receipt.ResponseCiphertext, KeyVersion: *receipt.ResponseKeyVersion,
	}, *receipt.ResponseDigest)
	if err != nil {
		return nil, false, previewruntime.ErrPersistence
	}
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var response schema.Message
	if err := decoder.Decode(&response); err != nil {
		return nil, false, previewruntime.ErrPersistence
	}
	var trailing json.RawMessage
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		return nil, false, previewruntime.ErrPersistence
	}
	return &response, false, nil
}

// FreezeModel 在 Business 副作用和 Workspace 投影前冻结模型首个完成响应。
func (r *CreationSpecPreviewRepository) FreezeModel(
	ctx context.Context,
	identity previewchatmodel.ReceiptIdentity,
	callIndex int,
	requestDigest string,
	response *schema.Message,
) error {
	if response == nil || !validModelReceiptIdentity(identity) || callIndex < 1 || callIndex > 3 ||
		!validPreviewDigest(requestDigest) {
		return previewruntime.ErrInvalidInput
	}
	toolCallID := identity.ToolCallID
	encoded, err := json.Marshal(response)
	if err != nil {
		return previewruntime.ErrPersistence
	}
	digest := sha256.Sum256(encoded)
	digestHex := hex.EncodeToString(digest[:])
	protected, err := r.protector.Protect(ctx, encoded)
	if err != nil {
		return mapPreviewRepositoryError(err)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := r.lockActivePreviewFence(tx, previewFenceIdentity{
			Owner: identity.Owner, SessionID: identity.SessionID, InputID: identity.InputID,
			ToolCallID: identity.ToolCallID, FenceToken: identity.FenceToken,
		}); err != nil {
			return err
		}
		var receipt creationSpecPreviewModelReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"tool_call_id = ? AND call_index = ?", toolCallID, callIndex,
		).Take(&receipt).Error; err != nil {
			return fmt.Errorf("lock preview model receipt for freeze: %w", err)
		}
		if receipt.RequestDigest != requestDigest {
			return previewruntime.ErrIdempotencyConflict
		}
		if receipt.Status == "completed" {
			if receipt.ResponseDigest == nil || *receipt.ResponseDigest != digestHex {
				return previewruntime.ErrIdempotencyConflict
			}
			return nil
		}
		if receipt.Status != "pending" {
			return previewruntime.ErrPersistence
		}
		now := time.Now().UTC()
		update := tx.Model(&creationSpecPreviewModelReceiptModel{}).Where(
			"tool_call_id = ? AND call_index = ? AND status = 'pending'", toolCallID, callIndex,
		).Updates(map[string]any{
			"status": "completed", "response_ciphertext": protected.Ciphertext,
			"response_key_version": protected.KeyVersion, "response_digest": digestHex, "completed_at": now,
		})
		if update.Error != nil || update.RowsAffected != 1 {
			return previewruntime.ErrIdempotencyConflict
		}
		return nil
	})
	return mapPreviewRepositoryError(err)
}

// LoadReceipt 以一次真源读恢复 Processor 所需的开放、recovery-only 或终态状态。
func (r *CreationSpecPreviewRepository) LoadReceipt(
	ctx context.Context,
	claim previewruntime.Claim,
) (previewruntime.ReceiptSnapshot, error) {
	trusted := trustedFromClaim(claim)
	var receipt creationSpecPreviewToolReceiptModel
	if err := r.db.WithContext(ctx).Where("tool_call_id = ?", claim.ToolCallID).Take(&receipt).Error; err != nil {
		return previewruntime.ReceiptSnapshot{}, mapPreviewRepositoryError(err)
	}
	if receipt.BusinessCommandID != claim.BusinessCommandID {
		return previewruntime.ReceiptSnapshot{}, previewruntime.ErrIdempotencyConflict
	}
	stage := previewruntime.ReceiptStage(receipt.Stage)
	if receipt.Stage == "pending" || receipt.Stage == "business_prepared" || receipt.Stage == "business_unknown" ||
		receipt.Stage == "business_resend_exhausted" {
		return previewruntime.ReceiptSnapshot{Stage: stage}, nil
	}
	if receipt.Stage != "completed" && receipt.Stage != "failed" {
		return previewruntime.ReceiptSnapshot{}, previewruntime.ErrPersistence
	}
	payload, err := r.openTerminalPayload(ctx, receipt, trusted)
	if err != nil {
		return previewruntime.ReceiptSnapshot{}, err
	}
	disposition := previewruntime.CompletionDisposition(payload.CompletionDisposition)
	return previewruntime.ReceiptSnapshot{
		Stage: stage, Terminal: &previewruntime.Terminal{Result: payload.Result, Disposition: disposition},
	}, nil
}

// Complete 以 Session/Input fence CAS 原子追加 Projection/Event、resolve Input 并释放 Lane Lease。
func (r *CreationSpecPreviewRepository) Complete(
	ctx context.Context,
	claim previewruntime.Claim,
	terminal previewruntime.Terminal,
	now time.Time,
) error {
	result := terminal.Result
	trusted := trustedFromClaim(claim)
	if !canonicalPreviewUUIDv7(claim.TerminalEventID) || now.IsZero() ||
		(terminal.Disposition != previewruntime.CompletionResolved && terminal.Disposition != previewruntime.CompletionDead) ||
		plancreationspec.ValidateTerminalResult(result, trusted) != nil ||
		(result.Status == "completed" && terminal.Disposition != previewruntime.CompletionResolved) {
		return previewruntime.ErrInvalidInput
	}
	terminalPayload, err := json.Marshal(terminalReceiptPayload{
		SchemaVersion: terminalReceiptSchemaVersion, CompletionDisposition: string(terminal.Disposition),
		Result: result, Card: result.Card, BusinessRequestDigest: result.BusinessRequestDigest,
	})
	if err != nil {
		return previewruntime.ErrPersistence
	}
	terminalDigestBytes := sha256.Sum256(terminalPayload)
	terminalDigest := hex.EncodeToString(terminalDigestBytes[:])
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var input sessionInputModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"id = ? AND session_id = ?", claim.InputID, claim.SessionID,
		).Take(&input).Error; err != nil {
			return fmt.Errorf("lock preview input for completion: %w", err)
		}
		owner := claimLeaseOwner(claim)
		if input.Status != string(session.InputStatusRunning) || input.LeaseOwner == nil || *input.LeaseOwner != owner ||
			input.FenceToken != claim.FenceToken || input.LeaseUntil == nil {
			return previewruntime.ErrFenceLost
		}
		var lane sessionRuntimeLeaseModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", claim.SessionID).Take(&lane).Error; err != nil {
			return fmt.Errorf("lock preview lane for completion: %w", err)
		}
		if lane.LeaseOwner == nil || *lane.LeaseOwner != owner || lane.FenceToken != claim.FenceToken || lane.LeaseUntil == nil {
			return previewruntime.ErrFenceLost
		}
		var receipt creationSpecPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", claim.ToolCallID).Take(&receipt).Error; err != nil {
			return fmt.Errorf("lock preview terminal receipt: %w", err)
		}
		if receipt.Stage != result.Status || receipt.ResultDigest == nil || *receipt.ResultDigest != terminalDigest ||
			receipt.ResultKeyVersion == nil || len(receipt.ResultCiphertext) == 0 {
			return previewruntime.ErrPersistence
		}

		var projectedEvent event.Record
		if result.Status == "completed" {
			card := *result.Card
			phases, _ := json.Marshal(card.Phases)
			constraints, _ := json.Marshal(card.Constraints)
			criteria, _ := json.Marshal(card.AcceptanceCriteria)
			audience := card.Audience
			projection := creationSpecPreviewProjectionModel{
				SessionID: claim.SessionID, SourceInputID: claim.InputID, SourceEnqueueSeq: input.EnqueueSeq,
				SchemaVersion: card.SchemaVersion, ResourceID: card.CreationSpecID,
				ProjectID: card.ProjectID, ResourceVersion: card.Version, ContentDigest: card.ContentDigest,
				Status: card.Status, Title: card.Title, Goal: card.Goal, DeliverableType: card.DeliverableType,
				Audience: &audience, Locale: card.Locale, Phases: string(phases), Constraints: string(constraints),
				AcceptanceCriteria: string(criteria), UpdatedAt: card.UpdatedAt,
			}
			var existing creationSpecPreviewProjectionModel
			projectionErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", claim.SessionID).Take(&existing).Error
			switch {
			case errors.Is(projectionErr, gorm.ErrRecordNotFound):
				if err := tx.Create(&projection).Error; err != nil {
					return fmt.Errorf("create preview projection: %w", err)
				}
			case projectionErr != nil:
				return fmt.Errorf("read preview projection: %w", projectionErr)
			case existing.ProjectID != projection.ProjectID || existing.SourceEnqueueSeq >= projection.SourceEnqueueSeq:
				return previewruntime.ErrIdempotencyConflict
			default:
				if existing.ResourceID == projection.ResourceID &&
					(existing.ResourceVersion > projection.ResourceVersion ||
						(existing.ResourceVersion == projection.ResourceVersion && existing.ContentDigest != projection.ContentDigest)) {
					return previewruntime.ErrIdempotencyConflict
				}
				if err := tx.Model(&creationSpecPreviewProjectionModel{}).Where("session_id = ?", claim.SessionID).Updates(projection).Error; err != nil {
					return fmt.Errorf("update preview projection: %w", err)
				}
			}
			cardJSON, err := json.Marshal(card)
			if err != nil {
				return previewruntime.ErrPersistence
			}
			projectedEvent = event.NewCreationSpecPreviewCompleted(
				claim.TerminalEventID, claim.SessionID, claim.InputID, card.CreationSpecID, card.Version, cardJSON, now,
			)
		} else {
			var err error
			projectedEvent, err = event.NewCreationSpecPreviewFailed(
				claim.TerminalEventID, claim.SessionID, claim.InputID, claim.InputID,
				result.ResultCode, result.Summary, result.Retryable, now,
			)
			if err != nil {
				return err
			}
		}

		var counter sessionEventCounterModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", claim.SessionID).Take(&counter).Error; err != nil {
			return fmt.Errorf("lock preview completion event counter: %w", err)
		}
		seq := counter.LastSeq + 1
		if err := tx.Create(&sessionEventLogModel{
			EventID: projectedEvent.EventID, SessionID: projectedEvent.SessionID, Seq: seq,
			EventType: string(projectedEvent.Type), SchemaVersion: projectedEvent.SchemaVersion,
			SourceKind: projectedEvent.SourceKind, SourceID: projectedEvent.SourceID,
			ProjectionIndex: projectedEvent.ProjectionIndex, AggregateType: string(projectedEvent.AggregateType),
			AggregateID: projectedEvent.AggregateID, AggregateVersion: projectedEvent.AggregateVersion,
			Payload: string(projectedEvent.PayloadJSON), CreatedAt: projectedEvent.CreatedAt,
		}).Error; err != nil {
			return fmt.Errorf("create preview terminal event: %w", err)
		}
		if update := tx.Model(&sessionEventCounterModel{}).Where("session_id = ? AND last_seq = ?", claim.SessionID, counter.LastSeq).
			Updates(map[string]any{"last_seq": seq, "updated_at": now.UTC()}); update.Error != nil || update.RowsAffected != 1 {
			return fmt.Errorf("advance preview terminal event counter")
		}
		inputTerminalStatus := session.InputStatusResolved
		if terminal.Disposition == previewruntime.CompletionDead {
			inputTerminalStatus = session.InputStatusDead
		}
		if update := tx.Model(&sessionInputModel{}).Where(
			"id = ? AND status = ? AND lease_owner = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP",
			claim.InputID, string(session.InputStatusRunning), owner, claim.FenceToken,
		).Updates(map[string]any{
			"status": string(inputTerminalStatus), "lease_owner": nil, "lease_until": nil, "updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		}); update.Error != nil || update.RowsAffected != 1 {
			return previewruntime.ErrFenceLost
		}
		if update := tx.Model(&sessionRuntimeLeaseModel{}).Where(
			"session_id = ? AND lease_owner = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP",
			claim.SessionID, owner, claim.FenceToken,
		).Updates(map[string]any{"lease_owner": nil, "lease_until": nil, "updated_at": gorm.Expr("CURRENT_TIMESTAMP")}); update.Error != nil || update.RowsAffected != 1 {
			return previewruntime.ErrFenceLost
		}
		return nil
	})
	return mapPreviewRepositoryError(err)
}

// DeferRecovery 保持当前 Input 为 Session HOL，释放 lease 后按 available_at 查询原命令。
func (r *CreationSpecPreviewRepository) DeferRecovery(
	ctx context.Context,
	claim previewruntime.Claim,
	availableAt time.Time,
) error {
	return r.releaseClaim(ctx, claim, session.InputStatusRecoveryPending, availableAt)
}

// DeferProjection 把 Receipt 读取或终态投影故障放回 recovery_pending；该路径不消耗 execution attempts。
func (r *CreationSpecPreviewRepository) DeferProjection(
	ctx context.Context,
	claim previewruntime.Claim,
	availableAt time.Time,
) error {
	return r.releaseClaim(ctx, claim, session.InputStatusRecoveryPending, availableAt)
}

// RetryExecution 仅把尚未冻结终态的执行技术失败放回 retry_wait。
func (r *CreationSpecPreviewRepository) RetryExecution(
	ctx context.Context,
	claim previewruntime.Claim,
	availableAt time.Time,
) error {
	return r.releaseClaim(ctx, claim, session.InputStatusRetryWait, availableAt)
}

func (r *CreationSpecPreviewRepository) releaseClaim(
	ctx context.Context,
	claim previewruntime.Claim,
	status session.InputStatus,
	availableAt time.Time,
) error {
	if status != session.InputStatusRetryWait && status != session.InputStatusRecoveryPending {
		return previewruntime.ErrInvalidInput
	}
	owner := claimLeaseOwner(claim)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		input := tx.Model(&sessionInputModel{}).Where(
			"id = ? AND session_id = ? AND status IN ? AND lease_owner = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP",
			claim.InputID, claim.SessionID,
			[]string{string(session.InputStatusClaimed), string(session.InputStatusRunning)}, owner, claim.FenceToken,
		).Updates(map[string]any{
			"status": string(status), "available_at": availableAt.UTC(),
			"lease_owner": nil, "lease_until": nil, "updated_at": gorm.Expr("CURRENT_TIMESTAMP"),
		})
		if input.Error != nil || input.RowsAffected != 1 {
			return previewruntime.ErrFenceLost
		}
		lane := tx.Model(&sessionRuntimeLeaseModel{}).Where(
			"session_id = ? AND lease_owner = ? AND fence_token = ? AND lease_until > CURRENT_TIMESTAMP",
			claim.SessionID, owner, claim.FenceToken,
		).Updates(map[string]any{"lease_owner": nil, "lease_until": nil, "updated_at": gorm.Expr("CURRENT_TIMESTAMP")})
		if lane.Error != nil || lane.RowsAffected != 1 {
			return previewruntime.ErrFenceLost
		}
		return nil
	})
	return mapPreviewRepositoryError(err)
}

func (r *CreationSpecPreviewRepository) openTerminal(
	ctx context.Context,
	receipt creationSpecPreviewToolReceiptModel,
	trusted plancreationspec.TrustedContext,
) (*plancreationspec.Result, error) {
	payload, err := r.openTerminalPayload(ctx, receipt, trusted)
	if err != nil {
		return nil, err
	}
	return &payload.Result, nil
}

func (r *CreationSpecPreviewRepository) openTerminalPayload(
	ctx context.Context,
	receipt creationSpecPreviewToolReceiptModel,
	trusted plancreationspec.TrustedContext,
) (*terminalReceiptPayload, error) {
	if receipt.ResultKeyVersion == nil || receipt.ResultDigest == nil || len(receipt.ResultCiphertext) == 0 {
		return nil, previewruntime.ErrPersistence
	}
	plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
		Ciphertext: receipt.ResultCiphertext, KeyVersion: *receipt.ResultKeyVersion,
	}, *receipt.ResultDigest)
	if err != nil {
		return nil, previewruntime.ErrPersistence
	}
	decoder := json.NewDecoder(bytes.NewReader(plaintext))
	decoder.DisallowUnknownFields()
	var payload terminalReceiptPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, previewruntime.ErrPersistence
	}
	var trailing json.RawMessage
	if !errors.Is(decoder.Decode(&trailing), io.EOF) || payload.SchemaVersion != terminalReceiptSchemaVersion ||
		(payload.CompletionDisposition != string(previewruntime.CompletionResolved) &&
			payload.CompletionDisposition != string(previewruntime.CompletionDead)) ||
		(payload.Result.Status == "completed" && payload.CompletionDisposition != string(previewruntime.CompletionResolved)) {
		return nil, previewruntime.ErrPersistence
	}
	payload.Result.Card = payload.Card
	payload.Result.BusinessRequestDigest = payload.BusinessRequestDigest
	if err := plancreationspec.ValidateTerminalResult(payload.Result, trusted); err != nil || payload.Result.Status != receipt.Stage {
		return nil, previewruntime.ErrPersistence
	}
	return &payload, nil
}

func validatePreviewEnqueuePlan(plan previewruntime.EnqueuePlan) error {
	ids := []string{
		plan.RequestID, plan.IdempotencyKey, plan.UserID, plan.ProjectID, plan.SessionID, plan.MessageID,
		plan.InputID, plan.TurnID, plan.RunID, plan.ToolCallID, plan.BusinessCommandID, plan.EventID, plan.TerminalEventID,
	}
	for _, id := range ids {
		if !canonicalPreviewUUIDv7(id) {
			return previewruntime.ErrInvalidInput
		}
	}
	if len(plan.RequestDigest) != 64 || strings.ToLower(plan.RequestDigest) != plan.RequestDigest ||
		plan.PromptVersion != plancreationspec.PromptVersion || plan.ValidatorVersion != plancreationspec.ValidatorVersion ||
		plan.CreatedAt.IsZero() || plan.Content.KeyVersion == "" || session.ValidateEnvelopeV1(plan.Content.Ciphertext) != nil {
		return previewruntime.ErrInvalidInput
	}
	if decoded, err := hex.DecodeString(plan.RequestDigest); err != nil || len(decoded) != sha256.Size {
		return previewruntime.ErrInvalidInput
	}
	return nil
}

func trustedFromClaim(claim previewruntime.Claim) plancreationspec.TrustedContext {
	return plancreationspec.TrustedContext{
		Owner: claim.Owner, RequestID: claim.RequestID, UserID: claim.UserID, ProjectID: claim.ProjectID,
		SessionID: claim.SessionID, InputID: claim.InputID, TurnID: claim.TurnID, RunID: claim.RunID,
		ToolCallID: claim.ToolCallID, BusinessCommandID: claim.BusinessCommandID, FenceToken: claim.FenceToken,
		PromptVersion: claim.PromptVersion, ValidatorVersion: claim.ValidatorVersion,
	}
}

func claimLeaseOwner(claim previewruntime.Claim) string { return claim.Owner }

func canonicalPreviewUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

func validPreviewDigest(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

// validDurableBusinessCommandReceipt 校验可恢复命令密文、摘要和独立重发预算的持久化完整性。
func validDurableBusinessCommandReceipt(receipt creationSpecPreviewToolReceiptModel) bool {
	return receipt.BusinessRequestDigest != nil && validPreviewDigest(*receipt.BusinessRequestDigest) &&
		receipt.BusinessContentDigest != nil && validPreviewDigest(*receipt.BusinessContentDigest) &&
		len(receipt.BusinessCommandCiphertext) > 0 && receipt.BusinessCommandKeyVersion != nil &&
		*receipt.BusinessCommandKeyVersion != "" && receipt.BusinessCommandPayloadDigest != nil &&
		validPreviewDigest(*receipt.BusinessCommandPayloadDigest) && receipt.BusinessResendLimit != nil &&
		*receipt.BusinessResendLimit >= 1 && *receipt.BusinessResendLimit <= 20 &&
		receipt.BusinessResendAttempts >= 0 && receipt.BusinessResendAttempts <= *receipt.BusinessResendLimit
}

type previewFenceIdentity struct {
	Owner      string
	SessionID  string
	InputID    string
	ToolCallID string
	FenceToken int64
}

type activePreviewFenceRow struct {
	SessionID       string `gorm:"column:session_id"`
	InputID         string `gorm:"column:input_id"`
	ToolCallID      string `gorm:"column:tool_call_id"`
	InputStatus     string `gorm:"column:input_status"`
	InputOwner      string `gorm:"column:input_owner"`
	InputFence      int64  `gorm:"column:input_fence"`
	InputLeaseValid bool   `gorm:"column:input_lease_valid"`
	LaneOwner       string `gorm:"column:lane_owner"`
	LaneFence       int64  `gorm:"column:lane_fence"`
	LaneLeaseValid  bool   `gorm:"column:lane_lease_valid"`
}

// lockActivePreviewFence 统一关闭 Model/Tool Receipt 写点：旧 owner/fence 或过期 lease 一律不得冻结事实。
func (r *CreationSpecPreviewRepository) lockActivePreviewFence(tx *gorm.DB, identity previewFenceIdentity) error {
	if tx == nil || identity.Owner == "" || identity.FenceToken < 1 ||
		!canonicalPreviewUUIDv7(identity.SessionID) || !canonicalPreviewUUIDv7(identity.InputID) ||
		!canonicalPreviewUUIDv7(identity.ToolCallID) {
		return previewruntime.ErrFenceLost
	}
	var row activePreviewFenceRow
	query := tx.Raw(`
		SELECT run.session_id, run.input_id, run.tool_call_id,
		       input_record.status AS input_status,
		       COALESCE(input_record.lease_owner, '') AS input_owner,
		       input_record.fence_token AS input_fence,
		       COALESCE(input_record.lease_until > CURRENT_TIMESTAMP, FALSE) AS input_lease_valid,
		       COALESCE(lane.lease_owner, '') AS lane_owner,
		       lane.fence_token AS lane_fence,
		       COALESCE(lane.lease_until > CURRENT_TIMESTAMP, FALSE) AS lane_lease_valid
		FROM agent.creation_spec_preview_run AS run
		JOIN agent.session_input AS input_record ON input_record.id = run.input_id
		JOIN agent.session_runtime_lease AS lane ON lane.session_id = run.session_id
		WHERE run.tool_call_id = ?
		LIMIT 1
		FOR UPDATE OF input_record, lane`, identity.ToolCallID).Scan(&row)
	if query.Error != nil {
		return fmt.Errorf("lock active preview fence: %w", query.Error)
	}
	if query.RowsAffected != 1 || row.SessionID != identity.SessionID || row.InputID != identity.InputID ||
		row.ToolCallID != identity.ToolCallID || row.InputStatus != string(session.InputStatusRunning) ||
		row.InputOwner != identity.Owner || row.InputFence != identity.FenceToken || !row.InputLeaseValid ||
		row.LaneOwner != identity.Owner || row.LaneFence != identity.FenceToken || !row.LaneLeaseValid {
		return previewruntime.ErrFenceLost
	}
	return nil
}

func validModelReceiptIdentity(identity previewchatmodel.ReceiptIdentity) bool {
	return identity.Owner != "" && identity.FenceToken > 0 && canonicalPreviewUUIDv7(identity.SessionID) &&
		canonicalPreviewUUIDv7(identity.InputID) && canonicalPreviewUUIDv7(identity.ToolCallID)
}

func mapPreviewRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, previewruntime.ErrInvalidInput) || errors.Is(err, previewruntime.ErrNotFound) ||
		errors.Is(err, previewruntime.ErrIdempotencyConflict) || errors.Is(err, previewruntime.ErrSessionLaneBlocked) ||
		errors.Is(err, previewruntime.ErrFenceLost) {
		return err
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return previewruntime.ErrIdempotencyConflict
	}
	return previewruntime.ErrPersistence
}

var _ previewruntime.Repository = (*CreationSpecPreviewRepository)(nil)
var _ previewruntime.EnqueueRepository = (*CreationSpecPreviewRepository)(nil)
var _ plancreationspec.ResultStore = (*CreationSpecPreviewRepository)(nil)
var _ previewchatmodel.ReceiptStore = (*CreationSpecPreviewRepository)(nil)
