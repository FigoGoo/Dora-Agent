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

	"github.com/FigoGoo/Dora-Agent/agent/internal/analyzematerialsruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// analyzeMaterialsContentProtector 约束 Intent、Model Response 与 Tool Result 使用同一受控 AEAD 边界。
type analyzeMaterialsContentProtector interface {
	Protect(context.Context, []byte) (session.ProtectedContent, error)
	Open(context.Context, session.ProtectedContent, string) ([]byte, error)
}

// analyzeMaterialsIDGenerator 为一次首次入队预分配稳定 UUIDv7，技术重放不再次使用候选值。
type analyzeMaterialsIDGenerator interface {
	New() (string, error)
}

// AnalyzeMaterialsRuntimeRepository 实现独立 Profile 的全 Source HOL、Lease/Fence、Receipt 与终态投影。
type AnalyzeMaterialsRuntimeRepository struct {
	// db 是只允许本 Repository 使用的 GORM Agent PostgreSQL 连接。
	db *gorm.DB
	// protector 认证加密受控 Intent、Model Response 与 Tool Result。
	protector analyzeMaterialsContentProtector
	// ids 为首次入队生成应用侧 UUIDv7。
	ids analyzeMaterialsIDGenerator
}

// NewAnalyzeMaterialsRuntimeRepository 创建不执行 Migration 或 AutoMigrate 的 PostgreSQL Adapter。
func NewAnalyzeMaterialsRuntimeRepository(
	client *Client,
	protector analyzeMaterialsContentProtector,
	ids analyzeMaterialsIDGenerator,
) (*AnalyzeMaterialsRuntimeRepository, error) {
	if client == nil || client.db == nil || protector == nil || ids == nil {
		return nil, fmt.Errorf("create analyze materials runtime repository: dependency is nil")
	}
	return &AnalyzeMaterialsRuntimeRepository{db: client.db, protector: protector, ids: ids}, nil
}

// Enqueue 在一个短事务中写无 Message Input、加密 Context、稳定 Run、open Tool Receipt 与 typed accepted Event。
func (r *AnalyzeMaterialsRuntimeRepository) Enqueue(
	ctx context.Context,
	command analyzematerialsruntime.EnqueueCommand,
	_ time.Time,
) (analyzematerialsruntime.EnqueueResult, error) {
	canonicalIntent, err := analyzematerialsruntime.DecodeIntent(command.IntentJSON)
	if err != nil || !bytes.Equal(canonicalIntent.JSON, command.IntentJSON) ||
		!canonicalAnalyzeMaterialsUUIDv7(command.RequestID) ||
		!canonicalAnalyzeMaterialsUUIDv7(command.SessionID) || !canonicalAnalyzeMaterialsUUIDv7(command.UserID) ||
		!canonicalAnalyzeMaterialsUUIDv7(command.ProjectID) || !canonicalAnalyzeMaterialsUUIDv7(command.IdempotencyKey) ||
		command.AccessScopeRef == "" || !validAnalyzeMaterialsDigest(command.AccessScopeDigest) || command.IntentKeyVersion == "" {
		return analyzematerialsruntime.EnqueueResult{}, analyzematerialsruntime.ErrInvalidInput
	}
	if existing, err := r.lookupAnalyzeMaterialsEnqueue(ctx, command, canonicalIntent.Digest); err != nil || existing != nil {
		if err != nil {
			return analyzematerialsruntime.EnqueueResult{}, err
		}
		return *existing, nil
	}
	var result analyzematerialsruntime.EnqueueResult
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		idempotencyScope := command.SessionID + ":" + command.IdempotencyKey
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", idempotencyScope).Error; err != nil {
			return err
		}
		var existing analyzeMaterialsPreviewRunModel
		lookupErr := tx.Where("session_id = ? AND idempotency_key = ?", command.SessionID, command.IdempotencyKey).Take(&existing).Error
		switch {
		case lookupErr == nil:
			if existing.RequestDigest != canonicalIntent.Digest || existing.SessionID != command.SessionID ||
				existing.UserID != command.UserID || existing.ProjectID != command.ProjectID {
				return analyzematerialsruntime.ErrIdempotencyConflict
			}
			result = mapAnalyzeMaterialsEnqueueResult(existing, true)
			return nil
		case !errors.Is(lookupErr, gorm.ErrRecordNotFound):
			return lookupErr
		}
		protected, err := r.protector.Protect(ctx, canonicalIntent.JSON)
		if err != nil || protected.KeyVersion != command.IntentKeyVersion || len(protected.Ciphertext) == 0 {
			return analyzematerialsruntime.ErrPersistence
		}
		identities, err := r.newAnalyzeMaterialsIdentities()
		if err != nil {
			return analyzematerialsruntime.ErrPersistence
		}

		var target sessionModel
		if err := tx.Where("id = ? AND user_id = ? AND project_id = ? AND status = ? AND archived_at IS NULL",
			command.SessionID, command.UserID, command.ProjectID, string(session.StatusActive)).Take(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return analyzematerialsruntime.ErrNotFound
			}
			return err
		}
		var databaseNow time.Time
		if err := tx.Raw("SELECT clock_timestamp()").Scan(&databaseNow).Error; err != nil {
			return err
		}
		var sequence sessionSequenceCounterModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", command.SessionID).Take(&sequence).Error; err != nil {
			return err
		}
		var head sessionInputModel
		headErr := tx.Select("id", "source_type").Where(
			"session_id = ? AND status IN ?", command.SessionID,
			[]string{"pending", "claimed", "running", "retry_wait", "recovery_pending"},
		).Order("enqueue_seq ASC, id ASC").Take(&head).Error
		switch {
		case headErr == nil && head.SourceType != analyzematerialsruntime.SourceType:
			return analyzematerialsruntime.ErrSessionLaneBlocked
		case headErr != nil && !errors.Is(headErr, gorm.ErrRecordNotFound):
			return headErr
		}
		enqueueSeq := sequence.LastInputEnqueueSeq + 1
		if enqueueSeq < 1 {
			return analyzematerialsruntime.ErrPersistence
		}
		counterUpdate := tx.Model(&sessionSequenceCounterModel{}).
			Where("session_id = ? AND last_input_enqueue_seq = ?", command.SessionID, sequence.LastInputEnqueueSeq).
			Updates(map[string]any{"last_input_enqueue_seq": enqueueSeq, "updated_at": databaseNow})
		if counterUpdate.Error != nil || counterUpdate.RowsAffected != 1 {
			return analyzematerialsruntime.ErrPersistence
		}

		input := sessionInputModel{
			ID: identities.InputID, SessionID: command.SessionID,
			SourceType: string(session.InputSourceTypeAnalyzeMaterialsPreview), SourceID: command.RequestID,
			MessageID: nil, Status: string(session.InputStatusPending), EnqueueSeq: enqueueSeq,
			Attempts: 0, AvailableAt: databaseNow, FenceToken: 0, CreatedAt: databaseNow, UpdatedAt: databaseNow,
		}
		if err := tx.Create(&input).Error; err != nil {
			return err
		}
		turnContext := r.buildAnalyzeMaterialsTurnContext(command, canonicalIntent.Digest, protected.KeyVersion, identities)
		contextDigest, err := analyzematerialsruntime.DigestTurnContext(turnContext)
		if err != nil {
			return err
		}
		turnContext.ContextDigest = contextDigest
		run := analyzeMaterialsPreviewRunModel{
			InputID: identities.InputID, RequestID: command.RequestID,
			IdempotencyKey: command.IdempotencyKey, RequestDigest: canonicalIntent.Digest,
			SessionID: command.SessionID, UserID: command.UserID, ProjectID: command.ProjectID,
			TurnID: identities.TurnID, RunID: identities.RunID, ToolCallID: identities.ToolCallID,
			RouterModelCallID: identities.RouterModelCallID, GraphModelCallID: identities.GraphModelCallID,
			AcceptedEventID: identities.AcceptedEventID, TerminalEventID: identities.TerminalEventID,
			OwnerFence: 0, Status: "created", Version: 1, CreatedAt: databaseNow, UpdatedAt: databaseNow,
		}
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		contextRecord := mapAnalyzeMaterialsContextModel(turnContext, protected.Ciphertext, databaseNow)
		contextRecord.RouterModelCallID = identities.RouterModelCallID
		contextRecord.GraphModelCallID = identities.GraphModelCallID
		if err := tx.Create(&contextRecord).Error; err != nil {
			return err
		}
		toolReceipt := analyzeMaterialsPreviewToolReceiptModel{
			ToolCallID: identities.ToolCallID, RunID: identities.RunID, TurnID: identities.TurnID,
			InputID: identities.InputID, RequestDigest: analyzeMaterialsToolRequestDigest(turnContext),
			ExecutionFence: 0, Status: string(analyzematerialsruntime.ToolReceiptOpen), CreatedAt: databaseNow,
		}
		if err := tx.Create(&toolReceipt).Error; err != nil {
			return err
		}
		accepted, err := event.NewAnalyzeMaterialsPreviewAccepted(identities.AcceptedEventID, event.AnalyzeMaterialsPreviewAcceptedPayload{
			InputID: identities.InputID, SessionID: command.SessionID, TurnID: identities.TurnID,
			RunID: identities.RunID, RequestID: command.RequestID, SourceType: analyzematerialsruntime.SourceType,
			IntentDigest: canonicalIntent.Digest, ToolCallID: identities.ToolCallID, ContextDigest: contextDigest,
		}, databaseNow)
		if err != nil {
			return err
		}
		if err := appendAnalyzeMaterialsEvent(tx, databaseNow, mapAnalyzeMaterialsEventRecord(accepted)); err != nil {
			return err
		}
		result = mapAnalyzeMaterialsEnqueueResult(run, false)
		return nil
	})
	if err != nil {
		return analyzematerialsruntime.EnqueueResult{}, mapAnalyzeMaterialsRuntimeError(err)
	}
	return result, nil
}

// lookupAnalyzeMaterialsEnqueue 在 KMS、随机源和候选 ID 之前重放已冻结的同义幂等事实。
func (r *AnalyzeMaterialsRuntimeRepository) lookupAnalyzeMaterialsEnqueue(
	ctx context.Context,
	command analyzematerialsruntime.EnqueueCommand,
	requestDigest string,
) (*analyzematerialsruntime.EnqueueResult, error) {
	var existing analyzeMaterialsPreviewRunModel
	err := r.db.WithContext(ctx).Where("session_id = ? AND idempotency_key = ?", command.SessionID, command.IdempotencyKey).Take(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	case err != nil:
		return nil, mapAnalyzeMaterialsRuntimeError(err)
	case existing.RequestDigest != requestDigest || existing.UserID != command.UserID || existing.ProjectID != command.ProjectID:
		return nil, analyzematerialsruntime.ErrIdempotencyConflict
	default:
		result := mapAnalyzeMaterialsEnqueueResult(existing, true)
		return &result, nil
	}
}

// analyzeMaterialsStableIdentities 保存首次入队一次生成、重放时丢弃候选的 exact-set UUIDv7。
type analyzeMaterialsStableIdentities struct {
	// InputID 是无 Message Session Input UUIDv7。
	InputID string
	// TurnID 是技术重试复用的 Turn UUIDv7。
	TurnID string
	// RunID 是 Lease takeover 复用的 Run UUIDv7。
	RunID string
	// ToolCallID 是 Router 必须原样使用的 Tool Call UUIDv7。
	ToolCallID string
	// RouterModelCallID 是外层 Router Model Call UUIDv7。
	RouterModelCallID string
	// GraphModelCallID 是 Graph Analysis Model Call UUIDv7。
	GraphModelCallID string
	// AcceptedEventID 是 typed accepted Event UUIDv7。
	AcceptedEventID string
	// TerminalEventID 是四类互斥终态共用的 Event UUIDv7。
	TerminalEventID string
}

// newAnalyzeMaterialsIdentities 一次生成 exact-set UUIDv7；任一失败都不允许部分持久化。
func (r *AnalyzeMaterialsRuntimeRepository) newAnalyzeMaterialsIdentities() (analyzeMaterialsStableIdentities, error) {
	values := make([]string, 8)
	for index := range values {
		value, err := r.ids.New()
		if err != nil || !canonicalAnalyzeMaterialsUUIDv7(value) {
			return analyzeMaterialsStableIdentities{}, analyzematerialsruntime.ErrPersistence
		}
		values[index] = value
	}
	return analyzeMaterialsStableIdentities{
		InputID: values[0], TurnID: values[1], RunID: values[2], ToolCallID: values[3],
		RouterModelCallID: values[4], GraphModelCallID: values[5], AcceptedEventID: values[6], TerminalEventID: values[7],
	}, nil
}

// buildAnalyzeMaterialsTurnContext 只从批准 pins、可信命令和首次稳定身份组装待摘要 Context。
func (r *AnalyzeMaterialsRuntimeRepository) buildAnalyzeMaterialsTurnContext(
	command analyzematerialsruntime.EnqueueCommand,
	intentDigest string,
	intentKeyVersion string,
	ids analyzeMaterialsStableIdentities,
) turncontext.MaterialAnalysisTurnContext {
	pins := analyzematerialsruntime.ApprovedPins()
	return turncontext.MaterialAnalysisTurnContext{
		SchemaVersion: turncontext.MaterialAnalysisTurnContextSchemaVersion, Profile: analyzematerialsruntime.Profile,
		SessionID: command.SessionID, InputID: ids.InputID, TurnID: ids.TurnID, RunID: ids.RunID,
		ToolCallID: ids.ToolCallID, UserID: command.UserID, ProjectID: command.ProjectID,
		IntentKeyVersion: intentKeyVersion, IntentDigest: intentDigest,
		AccessScopeRef: command.AccessScopeRef, AccessScopeDigest: command.AccessScopeDigest,
		ToolRegistryRef: pins.ToolRegistryRef, ToolRegistryDigest: pins.ToolRegistryDigest,
		ToolDefinitionRef: pins.ToolDefinitionRef, ToolDefinitionDigest: pins.ToolDefinitionDigest,
		IntentSchemaRef: analyzematerials.IntentSchemaVersion, ResultSchemaRef: analyzematerials.ResultSchemaVersion,
		PromptRef: pins.PromptRef, PromptDigest: pins.PromptDigest,
		ValidatorRef: pins.ValidatorRef, ValidatorDigest: pins.ValidatorDigest,
		EvidencePolicyRef: pins.EvidencePolicyRef, EvidencePolicyDigest: pins.EvidencePolicyDigest,
		RouterModelRouteRef: pins.RouterModelRouteRef, RouterModelRouteDigest: pins.RouterModelRouteDigest,
		AnalysisModelRouteRef: pins.AnalysisModelRouteRef, AnalysisModelRouteDigest: pins.AnalysisModelRouteDigest,
		RuntimePolicyRef: pins.RuntimePolicyRef, RuntimePolicyDigest: pins.RuntimePolicyDigest,
		BudgetRef: pins.BudgetRef, BudgetDigest: pins.BudgetDigest,
	}
}

// analyzeMaterialsClaimRow 承接一次全 Source HOL 查询返回的稳定身份、Context 与受保护 Intent。
type analyzeMaterialsClaimRow struct {
	// Context 是一次查询取回的完整不可变 Context 与受保护 Intent。
	Context analyzeMaterialsPreviewContextModel `gorm:"embedded"`
	// TerminalEventID 是当前 Input 唯一互斥终态 Event UUIDv7。
	TerminalEventID string `gorm:"column:terminal_event_id"`
	// EnqueueSeq 是全 Source HOL 使用的 Session Input 序号。
	EnqueueSeq int64 `gorm:"column:enqueue_seq"`
	// Attempts 是非投影恢复执行已领取次数。
	Attempts int `gorm:"column:attempts"`
	// InputStatus 是 Claim 前状态，用于区分执行重试和投影恢复。
	InputStatus string `gorm:"column:input_status"`
	// LeaseFence 是 Claim 前 Session Lane Fence。
	LeaseFence int64 `gorm:"column:lease_fence"`
	// LeaseVersion 是 Claim 前 Session Lease 乐观锁版本。
	LeaseVersion int64 `gorm:"column:lease_version"`
	// DatabaseNow 是同一查询获得的 PostgreSQL 权威时钟。
	DatabaseNow time.Time `gorm:"column:database_now"`
}

// ClaimNext 先计算每个 Session 的全 Source 最小非终态 Input，再只分派当前 Profile 的真正 HOL。
func (r *AnalyzeMaterialsRuntimeRepository) ClaimNext(
	ctx context.Context,
	owner string,
	_ time.Time,
	leaseDuration time.Duration,
) (*analyzematerialsruntime.Claim, error) {
	if owner == "" || leaseDuration <= 0 {
		return nil, analyzematerialsruntime.ErrInvalidClaim
	}
	var row analyzeMaterialsClaimRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Raw(`
			WITH database_clock AS MATERIALIZED (
				SELECT clock_timestamp() AS database_now
			), head_ids AS MATERIALIZED (
				SELECT DISTINCT ON (candidate.session_id) candidate.session_id, candidate.id
				FROM agent.session_input AS candidate
				WHERE candidate.status IN ('pending','claimed','running','retry_wait','recovery_pending')
				ORDER BY candidate.session_id, candidate.enqueue_seq, candidate.id
			)
			SELECT context_record.*, run_record.terminal_event_id, input_record.enqueue_seq, input_record.attempts,
			       input_record.status AS input_status, lease.fence_token AS lease_fence,
			       lease.version AS lease_version, database_clock.database_now
			FROM head_ids
			CROSS JOIN database_clock
			JOIN agent.session_input AS input_record ON input_record.id = head_ids.id
			JOIN agent.session AS session_record ON session_record.id = input_record.session_id
			JOIN agent.session_runtime_lease AS lease ON lease.session_id = input_record.session_id
			JOIN agent.analyze_materials_preview_run AS run_record ON run_record.input_id = input_record.id
			JOIN agent.analyze_materials_preview_turn_context AS context_record ON context_record.input_id = input_record.id
			WHERE input_record.source_type = 'analyze_materials_preview'
			  AND input_record.status IN ('pending','claimed','running','retry_wait','recovery_pending')
			  AND input_record.available_at <= database_clock.database_now
			  AND session_record.status = 'active' AND session_record.archived_at IS NULL
			  AND run_record.session_id = context_record.session_id
			  AND run_record.input_id = context_record.input_id
			  AND run_record.turn_id = context_record.turn_id
			  AND run_record.run_id = context_record.run_id
			  AND run_record.tool_call_id = context_record.tool_call_id
			  AND run_record.router_model_call_id = context_record.router_model_call_id
			  AND run_record.graph_model_call_id = context_record.graph_model_call_id
			  AND (lease.lease_owner IS NULL OR lease.lease_until <= database_clock.database_now)
			  AND (input_record.lease_owner IS NULL OR input_record.lease_until <= database_clock.database_now)
			ORDER BY input_record.available_at, input_record.session_id, input_record.enqueue_seq
			FOR UPDATE OF input_record, lease SKIP LOCKED
			LIMIT 1`).Scan(&row)
		if query.Error != nil || query.RowsAffected == 0 {
			return query.Error
		}
		newFence := row.LeaseFence + 1
		if newFence < 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		leaseUntil := row.DatabaseNow.Add(leaseDuration)
		leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
			Where("session_id = ? AND version = ? AND fence_token = ? AND (lease_owner IS NULL OR lease_until <= ?)",
				row.Context.SessionID, row.LeaseVersion, row.LeaseFence, row.DatabaseNow).
			Updates(map[string]any{
				"lease_owner": owner, "lease_until": leaseUntil, "fence_token": newFence,
				"version": gorm.Expr("version + 1"), "updated_at": row.DatabaseNow,
			})
		if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
			return fmt.Errorf("claim analyze materials session lease: %w", analyzematerialsruntime.ErrFenceLost)
		}
		nextAttempts := row.Attempts
		if row.InputStatus != string(session.InputStatusRecoveryPending) {
			nextAttempts++
		}
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND status IN ?", row.Context.InputID, []string{"pending", "claimed", "running", "retry_wait", "recovery_pending"}).
			Updates(map[string]any{
				"status": "claimed", "attempts": nextAttempts, "lease_owner": owner,
				"lease_until": leaseUntil, "fence_token": newFence, "updated_at": row.DatabaseNow,
			})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return fmt.Errorf("claim analyze materials input: %w", analyzematerialsruntime.ErrFenceLost)
		}
		runUpdate := tx.Model(&analyzeMaterialsPreviewRunModel{}).
			Where("input_id = ? AND run_id = ? AND status IN ?", row.Context.InputID, row.Context.RunID, []string{"created", "running"}).
			Updates(map[string]any{"owner_fence": newFence, "version": gorm.Expr("version + 1"), "updated_at": row.DatabaseNow})
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return fmt.Errorf("claim analyze materials run: %w", analyzematerialsruntime.ErrFenceLost)
		}
		row.LeaseFence, row.Attempts = newFence, nextAttempts
		return nil
	})
	if err != nil {
		return nil, mapAnalyzeMaterialsRuntimeError(err)
	}
	if row.Context.InputID == "" {
		return nil, nil
	}
	claim := mapAnalyzeMaterialsClaim(row, owner)
	plaintext, openErr := r.protector.Open(ctx, session.ProtectedContent{
		Ciphertext: append([]byte(nil), row.Context.IntentCiphertext...), KeyVersion: row.Context.IntentKeyVersion,
	}, row.Context.IntentDigest)
	if openErr == nil {
		claim.IntentJSON = append([]byte(nil), plaintext...)
	}
	return &claim, nil
}

// MarkRunning 原子推进 Input 与 Run；任何零行都表示当前 owner/fence 已失效。
func (r *AnalyzeMaterialsRuntimeRepository) MarkRunning(
	ctx context.Context,
	claim analyzematerialsruntime.Claim,
	_ time.Time,
) error {
	return r.withActiveAnalyzeMaterialsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND status = 'claimed' AND lease_owner = ? AND fence_token = ?",
				claim.Context.InputID, claim.Owner, claim.FenceToken).
			Updates(map[string]any{"status": "running", "updated_at": databaseNow})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		runUpdate := tx.Model(&analyzeMaterialsPreviewRunModel{}).
			Where("run_id = ? AND owner_fence = ? AND status IN ?", claim.Context.RunID, claim.FenceToken, []string{"created", "running"}).
			Updates(map[string]any{
				"status": "running", "started_at": gorm.Expr("COALESCE(started_at, ?)", databaseNow),
				"version": gorm.Expr("version + 1"), "updated_at": databaseNow,
			})
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		return nil
	})
}

// RenewLease 使用 PostgreSQL clock 同事务延长 Session 与 Input 的相同 owner/fence。
func (r *AnalyzeMaterialsRuntimeRepository) RenewLease(
	ctx context.Context,
	claim analyzematerialsruntime.Claim,
	_ time.Time,
	leaseDuration time.Duration,
) error {
	if leaseDuration <= 0 {
		return analyzematerialsruntime.ErrInvalidClaim
	}
	return r.withActiveAnalyzeMaterialsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		leaseUntil := databaseNow.Add(leaseDuration)
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?",
				claim.Context.InputID, claim.Owner, claim.FenceToken, []string{"claimed", "running"}).
			Updates(map[string]any{"lease_until": leaseUntil, "updated_at": databaseNow})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
			Where("session_id = ? AND lease_owner = ? AND fence_token = ?", claim.Context.SessionID, claim.Owner, claim.FenceToken).
			Updates(map[string]any{"lease_until": leaseUntil, "version": gorm.Expr("version + 1"), "updated_at": databaseNow})
		if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		return nil
	})
}

// LoadToolReceipt 在当前 Fence 下读取并认证解密 open 或已冻结完整 Result。
func (r *AnalyzeMaterialsRuntimeRepository) LoadToolReceipt(
	ctx context.Context,
	claim analyzematerialsruntime.Claim,
) (analyzematerialsruntime.ToolReceiptSnapshot, error) {
	var snapshot analyzematerialsruntime.ToolReceiptSnapshot
	err := r.withActiveAnalyzeMaterialsFence(ctx, claim, func(tx *gorm.DB, _ time.Time) error {
		var record analyzeMaterialsPreviewToolReceiptModel
		if err := tx.Where("tool_call_id = ?", claim.Context.ToolCallID).Take(&record).Error; err != nil {
			return err
		}
		snapshot = analyzematerialsruntime.ToolReceiptSnapshot{
			Stage: analyzematerialsruntime.ToolReceiptStage(record.Status), RequestDigest: record.RequestDigest,
		}
		if record.Status == string(analyzematerialsruntime.ToolReceiptOpen) {
			if record.RunID != claim.Context.RunID || record.TurnID != claim.Context.TurnID ||
				record.InputID != claim.Context.InputID || record.RequestDigest != analyzeMaterialsToolRequestDigest(claim.Context) {
				return analyzematerialsruntime.ErrReceiptConflict
			}
			return nil
		}
		if record.RunID != claim.Context.RunID || record.TurnID != claim.Context.TurnID ||
			record.InputID != claim.Context.InputID || record.RequestDigest != analyzeMaterialsToolRequestDigest(claim.Context) {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		if record.ResultKeyVersion == nil || record.ResultDigest == nil {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
			Ciphertext: append([]byte(nil), record.ResultCiphertext...), KeyVersion: *record.ResultKeyVersion,
		}, *record.ResultDigest)
		if err != nil {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		snapshot.ResultJSON = append([]byte(nil), plaintext...)
		snapshot.ResultDigest = *record.ResultDigest
		return nil
	})
	return snapshot, err
}

// CompleteToolResult 在一个事务中重验 frozen Receipt，插入 immutable Card/Event 并释放 Session Lane。
func (r *AnalyzeMaterialsRuntimeRepository) CompleteToolResult(
	ctx context.Context,
	claim analyzematerialsruntime.Claim,
	result analyzematerials.Result,
	_ time.Time,
) error {
	encodedResult, err := json.Marshal(result)
	if err != nil {
		return analyzematerialsruntime.ErrOutputContract
	}
	resultDigest := digestAnalyzeMaterialsBytes(encodedResult)
	card, eventType, outcomeKind, err := analyzeMaterialsCardFromResult(claim, result)
	if err != nil {
		return err
	}
	return r.completeAnalyzeMaterialsProjection(ctx, claim, card, eventType, outcomeKind, "completed", resultDigest)
}

// CompleteRuntimeFailure 只写安全 Runtime Failure Card，不伪造合法 Tool failed Result。
func (r *AnalyzeMaterialsRuntimeRepository) CompleteRuntimeFailure(
	ctx context.Context,
	claim analyzematerialsruntime.Claim,
	failure analyzematerialsruntime.RuntimeFailure,
	_ time.Time,
) error {
	if failure.SchemaVersion != "analyze_materials.preview.runtime_failure.v1" ||
		failure.InputID != claim.Context.InputID || failure.TurnID != claim.Context.TurnID ||
		failure.RunID != claim.Context.RunID || failure.Code == "" || failure.Summary == "" {
		return analyzematerialsruntime.ErrOutputContract
	}
	retryable := failure.Retryable
	card := event.AnalyzeMaterialsPreviewCardPayload{
		SchemaVersion: event.AnalyzeMaterialsPreviewCardSchemaVersionV1, InputID: claim.Context.InputID,
		TurnID: claim.Context.TurnID, RunID: claim.Context.RunID, ToolCallID: claim.Context.ToolCallID,
		Status: "failed", ResultCode: failure.Code, FailureKind: event.AnalyzeMaterialsPreviewFailureKindRuntime,
		Summary: failure.Summary, Retryable: &retryable,
	}
	encoded, err := json.Marshal(card)
	if err != nil {
		return analyzematerialsruntime.ErrOutputContract
	}
	return r.completeAnalyzeMaterialsProjection(ctx, claim, card, "analyze_materials.preview.runtime_failed", "runtime_failed", "failed", digestAnalyzeMaterialsBytes(encoded))
}

// completeAnalyzeMaterialsProjection 将首写 Projection/Event、Input/Run 终态和 Lane release 原子提交。
func (r *AnalyzeMaterialsRuntimeRepository) completeAnalyzeMaterialsProjection(
	ctx context.Context,
	claim analyzematerialsruntime.Claim,
	card event.AnalyzeMaterialsPreviewCardPayload,
	eventType string,
	outcomeKind string,
	runStatus string,
	resultDigest string,
) error {
	payload, err := json.Marshal(card)
	if err != nil || !validAnalyzeMaterialsDigest(resultDigest) {
		return analyzematerialsruntime.ErrOutputContract
	}
	return r.withActiveAnalyzeMaterialsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var activeRun analyzeMaterialsPreviewRunModel
		if err := tx.Where("run_id = ? AND input_id = ?", claim.Context.RunID, claim.Context.InputID).Take(&activeRun).Error; err != nil {
			return err
		}
		if outcomeKind != "runtime_failed" {
			var receipt analyzeMaterialsPreviewToolReceiptModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", claim.Context.ToolCallID).Take(&receipt).Error; err != nil {
				return err
			}
			if receipt.RunID != claim.Context.RunID || receipt.TurnID != claim.Context.TurnID ||
				receipt.InputID != claim.Context.InputID || receipt.ResultDigest == nil ||
				*receipt.ResultDigest != resultDigest || receipt.ResultCode == nil ||
				*receipt.ResultCode != card.ResultCode || receipt.Status != card.Status {
				return analyzematerialsruntime.ErrReceiptConflict
			}
		}
		projection := analyzeMaterialsPreviewProjectionModel{
			SourceInputID: claim.Context.InputID, SessionID: claim.Context.SessionID, SourceEnqueueSeq: claim.EnqueueSeq,
			TurnID: claim.Context.TurnID, RunID: claim.Context.RunID, ToolCallID: claim.Context.ToolCallID,
			SchemaVersion: event.AnalyzeMaterialsPreviewCardSchemaVersionV1, OutcomeKind: outcomeKind, Status: card.Status,
			ResultDigest: resultDigest, Payload: string(payload), ProjectionVersion: 1, CreatedAt: databaseNow,
		}
		if err := tx.Create(&projection).Error; err != nil {
			return err
		}
		terminalEvent, err := newAnalyzeMaterialsTerminalEvent(eventType, claim, activeRun.RequestID, card, databaseNow)
		if err != nil {
			return err
		}
		if err := appendAnalyzeMaterialsEvent(tx, databaseNow, mapAnalyzeMaterialsEventRecord(terminalEvent)); err != nil {
			return err
		}
		inputStatus := "resolved"
		if runStatus == "failed" {
			inputStatus = "dead"
		}
		allowedInputStatuses := []string{"running"}
		allowedRunStatuses := []string{"running"}
		if runStatus == "failed" {
			allowedInputStatuses = []string{"claimed", "running"}
			allowedRunStatuses = []string{"created", "running"}
		}
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?", claim.Context.InputID, claim.Owner, claim.FenceToken, allowedInputStatuses).
			Updates(map[string]any{
				"status": inputStatus, "lease_owner": nil, "lease_until": nil, "updated_at": databaseNow,
			})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		runUpdates := map[string]any{
			"status": runStatus, "completed_at": databaseNow, "version": gorm.Expr("version + 1"), "updated_at": databaseNow,
		}
		if runStatus == "failed" {
			runUpdates["started_at"] = gorm.Expr("COALESCE(started_at, ?)", databaseNow)
		}
		runUpdate := tx.Model(&analyzeMaterialsPreviewRunModel{}).
			Where("run_id = ? AND owner_fence = ? AND status IN ?", claim.Context.RunID, claim.FenceToken, allowedRunStatuses).
			Updates(runUpdates)
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		return releaseAnalyzeMaterialsLane(tx, claim, databaseNow)
	})
}

// RetryExecution 释放当前 Fence 并把未冻结 Tool 的执行放回有限重试队列。
func (r *AnalyzeMaterialsRuntimeRepository) RetryExecution(
	ctx context.Context,
	claim analyzematerialsruntime.Claim,
	availableAt time.Time,
) error {
	return r.deferAnalyzeMaterialsInput(ctx, claim, availableAt, "retry_wait", true)
}

// DeferProjection 把已冻结 Result 的投影恢复标记为 recovery_pending，后续 Claim 不增加执行 Attempts。
func (r *AnalyzeMaterialsRuntimeRepository) DeferProjection(
	ctx context.Context,
	claim analyzematerialsruntime.Claim,
	availableAt time.Time,
) error {
	return r.deferAnalyzeMaterialsInput(ctx, claim, availableAt, "recovery_pending", false)
}

// deferAnalyzeMaterialsInput 区分执行 retry_wait 与不消耗 Attempts 的 recovery_pending 投影恢复。
func (r *AnalyzeMaterialsRuntimeRepository) deferAnalyzeMaterialsInput(
	ctx context.Context,
	claim analyzematerialsruntime.Claim,
	availableAt time.Time,
	status string,
	requireOpenReceipt bool,
) error {
	if availableAt.IsZero() {
		return analyzematerialsruntime.ErrInvalidClaim
	}
	return r.withActiveAnalyzeMaterialsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var receipt analyzeMaterialsPreviewToolReceiptModel
		if err := tx.Where("tool_call_id = ?", claim.Context.ToolCallID).Take(&receipt).Error; err != nil {
			return err
		}
		if requireOpenReceipt && receipt.Status != string(analyzematerialsruntime.ToolReceiptOpen) {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?",
				claim.Context.InputID, claim.Owner, claim.FenceToken, []string{"claimed", "running"}).
			Updates(map[string]any{
				"status": status, "available_at": availableAt.UTC(), "lease_owner": nil, "lease_until": nil,
				"updated_at": databaseNow,
			})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		return releaseAnalyzeMaterialsLane(tx, claim, databaseNow)
	})
}

// ReplayOrReserveModel 对 Router/Graph Model 分别创建或重放 first-write-wins 回执。
func (r *AnalyzeMaterialsRuntimeRepository) ReplayOrReserveModel(
	ctx context.Context,
	identity analyzematerialsruntime.ModelReceiptIdentity,
	requestDigest string,
) (analyzematerialsruntime.ModelReceiptSnapshot, bool, error) {
	claim := analyzeMaterialsClaimFromModelIdentity(identity)
	var snapshot analyzematerialsruntime.ModelReceiptSnapshot
	var execute bool
	err := r.withActiveAnalyzeMaterialsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		if !validAnalyzeMaterialsDigest(requestDigest) || !validAnalyzeMaterialsModelKind(identity.CallKind) {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		if err := assertAnalyzeMaterialsModelIdentity(tx, identity); err != nil {
			return err
		}
		var record analyzeMaterialsPreviewModelReceiptModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("model_call_id = ?", identity.ModelCallID).Take(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			record = analyzeMaterialsPreviewModelReceiptModel{
				ModelCallID: identity.ModelCallID, CallKind: string(identity.CallKind), RunID: identity.RunID,
				TurnID: identity.TurnID, InputID: identity.InputID, RequestDigest: requestDigest,
				ExecutionFence: identity.FenceToken, Status: string(analyzematerialsruntime.ModelReceiptReserved), CreatedAt: databaseNow,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			snapshot.Stage, execute = analyzematerialsruntime.ModelReceiptReserved, true
			return nil
		}
		if err != nil {
			return err
		}
		if record.CallKind != string(identity.CallKind) || record.RunID != identity.RunID || record.TurnID != identity.TurnID ||
			record.InputID != identity.InputID || record.RequestDigest != requestDigest {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		snapshot.Stage = analyzematerialsruntime.ModelReceiptStage(record.Status)
		switch snapshot.Stage {
		case analyzematerialsruntime.ModelReceiptReserved:
			switch {
			case identity.FenceToken < record.ExecutionFence:
				return analyzematerialsruntime.ErrFenceLost
			case identity.FenceToken == record.ExecutionFence:
				execute = false
			default:
				advance := tx.Model(&analyzeMaterialsPreviewModelReceiptModel{}).
					Where("model_call_id = ? AND status = 'reserved' AND execution_fence = ?", identity.ModelCallID, record.ExecutionFence).
					Update("execution_fence", identity.FenceToken)
				if advance.Error != nil || advance.RowsAffected != 1 {
					return analyzematerialsruntime.ErrFenceLost
				}
				execute = true
			}
		case analyzematerialsruntime.ModelReceiptCompleted:
			if record.ResponseKeyVersion == nil || record.ResponseDigest == nil {
				return analyzematerialsruntime.ErrReceiptConflict
			}
			plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
				Ciphertext: append([]byte(nil), record.ResponseCiphertext...), KeyVersion: *record.ResponseKeyVersion,
			}, *record.ResponseDigest)
			if err != nil {
				return analyzematerialsruntime.ErrReceiptConflict
			}
			var message schema.Message
			if err := strictAnalyzeMaterialsJSON(plaintext, &message); err != nil {
				return analyzematerialsruntime.ErrReceiptConflict
			}
			snapshot.Response = &message
		case analyzematerialsruntime.ModelReceiptFailed:
			if record.ErrorCode == nil || *record.ErrorCode == "" {
				return analyzematerialsruntime.ErrReceiptConflict
			}
			snapshot.ErrorCode = *record.ErrorCode
		default:
			return analyzematerialsruntime.ErrReceiptConflict
		}
		return nil
	})
	return snapshot, execute, err
}

// FreezeModelCompleted 认证加密完整 classic Message，并以当前 Fence 首写冻结。
func (r *AnalyzeMaterialsRuntimeRepository) FreezeModelCompleted(
	ctx context.Context,
	identity analyzematerialsruntime.ModelReceiptIdentity,
	requestDigest string,
	response *schema.Message,
) error {
	if response == nil {
		return analyzematerialsruntime.ErrOutputContract
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return analyzematerialsruntime.ErrOutputContract
	}
	protected, err := r.protector.Protect(ctx, encoded)
	if err != nil {
		return analyzematerialsruntime.ErrPersistence
	}
	return r.freezeAnalyzeMaterialsModel(ctx, identity, requestDigest, "completed", protected, digestAnalyzeMaterialsBytes(encoded), "")
}

// FreezeModelFailed 只冻结稳定脱敏错误码，不保存本地模型内部错误原文。
func (r *AnalyzeMaterialsRuntimeRepository) FreezeModelFailed(
	ctx context.Context,
	identity analyzematerialsruntime.ModelReceiptIdentity,
	requestDigest string,
	errorCode string,
) error {
	if errorCode == "" {
		return analyzematerialsruntime.ErrOutputContract
	}
	return r.freezeAnalyzeMaterialsModel(ctx, identity, requestDigest, "failed", session.ProtectedContent{}, "", errorCode)
}

// freezeAnalyzeMaterialsModel 仅允许当前 Fence 把 reserved 首写推进为相同请求的单一终态。
func (r *AnalyzeMaterialsRuntimeRepository) freezeAnalyzeMaterialsModel(
	ctx context.Context,
	identity analyzematerialsruntime.ModelReceiptIdentity,
	requestDigest string,
	status string,
	protected session.ProtectedContent,
	digest string,
	errorCode string,
) error {
	claim := analyzeMaterialsClaimFromModelIdentity(identity)
	return r.withActiveAnalyzeMaterialsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var record analyzeMaterialsPreviewModelReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("model_call_id = ?", identity.ModelCallID).Take(&record).Error; err != nil {
			return err
		}
		if record.CallKind != string(identity.CallKind) || record.RunID != identity.RunID || record.TurnID != identity.TurnID ||
			record.InputID != identity.InputID || record.RequestDigest != requestDigest {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		if record.Status != "reserved" {
			if record.Status == status && ((status == "completed" && record.ResponseDigest != nil && *record.ResponseDigest == digest) ||
				(status == "failed" && record.ErrorCode != nil && *record.ErrorCode == errorCode)) {
				return nil
			}
			return analyzematerialsruntime.ErrReceiptConflict
		}
		if record.ExecutionFence != identity.FenceToken {
			return analyzematerialsruntime.ErrFenceLost
		}
		updates := map[string]any{"status": status, "completed_at": databaseNow}
		if status == "completed" {
			updates["response_ciphertext"] = protected.Ciphertext
			updates["response_key_version"] = protected.KeyVersion
			updates["response_digest"] = digest
		} else {
			updates["error_code"] = errorCode
		}
		update := tx.Model(&analyzeMaterialsPreviewModelReceiptModel{}).
			Where("model_call_id = ? AND status = 'reserved' AND execution_fence = ?", identity.ModelCallID, identity.FenceToken).
			Updates(updates)
		if update.Error != nil || update.RowsAffected != 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		return nil
	})
}

// ReplayOrOpenTool 在入队 open Receipt 上取得当前 Fence 执行权，或认证解密并重放终态。
func (r *AnalyzeMaterialsRuntimeRepository) ReplayOrOpenTool(
	ctx context.Context,
	identity analyzematerialsruntime.ToolReceiptIdentity,
	requestDigest string,
) (analyzematerialsruntime.ToolReceiptSnapshot, bool, error) {
	claim := analyzeMaterialsClaimFromToolIdentity(identity)
	var snapshot analyzematerialsruntime.ToolReceiptSnapshot
	var execute bool
	err := r.withActiveAnalyzeMaterialsFence(ctx, claim, func(tx *gorm.DB, _ time.Time) error {
		var record analyzeMaterialsPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", identity.ToolCallID).Take(&record).Error; err != nil {
			return err
		}
		if record.RunID != identity.RunID || record.TurnID != identity.TurnID || record.InputID != identity.InputID ||
			record.RequestDigest != requestDigest {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		snapshot.Stage = analyzematerialsruntime.ToolReceiptStage(record.Status)
		snapshot.RequestDigest = record.RequestDigest
		if snapshot.Stage == analyzematerialsruntime.ToolReceiptOpen {
			switch {
			case identity.FenceToken < record.ExecutionFence:
				return analyzematerialsruntime.ErrFenceLost
			case identity.FenceToken == record.ExecutionFence:
				execute = false
			default:
				advance := tx.Model(&analyzeMaterialsPreviewToolReceiptModel{}).
					Where("tool_call_id = ? AND status = 'open' AND execution_fence = ?", identity.ToolCallID, record.ExecutionFence).
					Update("execution_fence", identity.FenceToken)
				if advance.Error != nil || advance.RowsAffected != 1 {
					return analyzematerialsruntime.ErrFenceLost
				}
				execute = true
			}
			return nil
		}
		if record.ResultKeyVersion == nil || record.ResultDigest == nil {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
			Ciphertext: append([]byte(nil), record.ResultCiphertext...), KeyVersion: *record.ResultKeyVersion,
		}, *record.ResultDigest)
		if err != nil {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		snapshot.ResultJSON, snapshot.ResultDigest = append([]byte(nil), plaintext...), *record.ResultDigest
		return nil
	})
	return snapshot, execute, err
}

// FreezeToolResult 重验 canonical Result/状态/摘要后，以当前执行 Fence 首写冻结完整密文。
func (r *AnalyzeMaterialsRuntimeRepository) FreezeToolResult(
	ctx context.Context,
	identity analyzematerialsruntime.ToolReceiptIdentity,
	requestDigest string,
	stage analyzematerialsruntime.ToolReceiptStage,
	resultJSON []byte,
	resultDigest string,
) error {
	if stage != analyzematerialsruntime.ToolReceiptCompleted && stage != analyzematerialsruntime.ToolReceiptPartial && stage != analyzematerialsruntime.ToolReceiptFailed ||
		digestAnalyzeMaterialsBytes(resultJSON) != resultDigest {
		return analyzematerialsruntime.ErrOutputContract
	}
	var result analyzematerials.Result
	if err := strictAnalyzeMaterialsJSON(resultJSON, &result); err != nil || result.Status != string(stage) ||
		result.InvocationRef.ToolCallID != identity.ToolCallID || analyzematerials.ValidateResult(result) != nil {
		return analyzematerialsruntime.ErrOutputContract
	}
	protected, err := r.protector.Protect(ctx, resultJSON)
	if err != nil {
		return analyzematerialsruntime.ErrPersistence
	}
	claim := analyzeMaterialsClaimFromToolIdentity(identity)
	return r.withActiveAnalyzeMaterialsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var record analyzeMaterialsPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", identity.ToolCallID).Take(&record).Error; err != nil {
			return err
		}
		if record.RunID != identity.RunID || record.TurnID != identity.TurnID || record.InputID != identity.InputID ||
			record.RequestDigest != requestDigest {
			return analyzematerialsruntime.ErrReceiptConflict
		}
		if record.Status != "open" {
			if record.Status == string(stage) && record.ResultDigest != nil && *record.ResultDigest == resultDigest {
				return nil
			}
			return analyzematerialsruntime.ErrReceiptConflict
		}
		if record.ExecutionFence != identity.FenceToken {
			return analyzematerialsruntime.ErrFenceLost
		}
		update := tx.Model(&analyzeMaterialsPreviewToolReceiptModel{}).
			Where("tool_call_id = ? AND status = 'open' AND execution_fence = ?", identity.ToolCallID, identity.FenceToken).
			Updates(map[string]any{
				"status": string(stage), "result_ciphertext": protected.Ciphertext,
				"result_key_version": protected.KeyVersion, "result_digest": resultDigest,
				"result_code": result.ResultCode, "completed_at": databaseNow,
			})
		if update.Error != nil || update.RowsAffected != 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		return nil
	})
}

// withActiveAnalyzeMaterialsFence 使用一次 PostgreSQL clock 校验 Session/Input/Run 三层相同 owner/fence。
func (r *AnalyzeMaterialsRuntimeRepository) withActiveAnalyzeMaterialsFence(
	ctx context.Context,
	claim analyzematerialsruntime.Claim,
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
			JOIN agent.analyze_materials_preview_run AS run_record ON run_record.input_id = input_record.id
			WHERE lease.session_id = ? AND lease.lease_owner = ? AND lease.fence_token = ?
			  AND lease.lease_until > ? AND input_record.id = ?
			  AND input_record.lease_owner = ? AND input_record.fence_token = ?
			  AND input_record.lease_until > ? AND run_record.run_id = ? AND run_record.owner_fence = ?`,
			claim.Context.SessionID, claim.Owner, claim.FenceToken, databaseNow,
			claim.Context.InputID, claim.Owner, claim.FenceToken, databaseNow,
			claim.Context.RunID, claim.FenceToken).Scan(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return analyzematerialsruntime.ErrFenceLost
		}
		return callback(tx, databaseNow)
	})
	return mapAnalyzeMaterialsRuntimeError(err)
}

// releaseAnalyzeMaterialsLane 只允许当前 owner/fence 清空 Session Lease，并单调推进 Lease Version。
func releaseAnalyzeMaterialsLane(tx *gorm.DB, claim analyzematerialsruntime.Claim, databaseNow time.Time) error {
	leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
		Where("session_id = ? AND lease_owner = ? AND fence_token = ?", claim.Context.SessionID, claim.Owner, claim.FenceToken).
		Updates(map[string]any{
			"lease_owner": nil, "lease_until": nil, "version": gorm.Expr("version + 1"), "updated_at": databaseNow,
		})
	if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
		return analyzematerialsruntime.ErrFenceLost
	}
	return nil
}

// appendAnalyzeMaterialsEvent 锁定 Session Event Counter 后以单调序号 append-once 写安全事件。
func appendAnalyzeMaterialsEvent(tx *gorm.DB, databaseNow time.Time, record sessionEventLogModel) error {
	var counter sessionEventCounterModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", record.SessionID).Take(&counter).Error; err != nil {
		return err
	}
	sequence := counter.LastSeq + 1
	if sequence < 1 {
		return analyzematerialsruntime.ErrPersistence
	}
	record.Seq = sequence
	record.CreatedAt = databaseNow
	if err := tx.Create(&record).Error; err != nil {
		return err
	}
	update := tx.Model(&sessionEventCounterModel{}).
		Where("session_id = ? AND last_seq = ?", record.SessionID, counter.LastSeq).
		Updates(map[string]any{"last_seq": sequence, "updated_at": databaseNow})
	if update.Error != nil || update.RowsAffected != 1 {
		return analyzematerialsruntime.ErrPersistence
	}
	return nil
}

// analyzeMaterialsCardFromResult 把已验证 Tool Result 映射为不含 Evidence 正文的严格判别联合 Card。
func analyzeMaterialsCardFromResult(
	claim analyzematerialsruntime.Claim,
	result analyzematerials.Result,
) (event.AnalyzeMaterialsPreviewCardPayload, string, string, error) {
	if result.InvocationRef.ToolCallID != claim.Context.ToolCallID || result.ResultCode == "" {
		return event.AnalyzeMaterialsPreviewCardPayload{}, "", "", analyzematerialsruntime.ErrOutputContract
	}
	card := event.AnalyzeMaterialsPreviewCardPayload{
		SchemaVersion: event.AnalyzeMaterialsPreviewCardSchemaVersionV1, InputID: claim.Context.InputID,
		TurnID: claim.Context.TurnID, RunID: claim.Context.RunID, ToolCallID: claim.Context.ToolCallID,
		Status: result.Status, ResultCode: result.ResultCode, Analysis: result.Analysis,
		Coverage: result.Coverage, EvidenceRefs: append([]analyzematerials.EvidenceRef(nil), result.EvidenceRefs...),
		Summary: result.Summary, Retryable: result.Retryable,
	}
	switch result.Status {
	case "completed":
		return card, "analyze_materials.preview.completed", "tool_completed", nil
	case "partial":
		return card, "analyze_materials.preview.partial", "tool_partial", nil
	case "failed":
		card.FailureKind = event.AnalyzeMaterialsPreviewFailureKindTool
		return card, "analyze_materials.preview.failed", "tool_failed", nil
	default:
		return event.AnalyzeMaterialsPreviewCardPayload{}, "", "", analyzematerialsruntime.ErrOutputContract
	}
}

// newAnalyzeMaterialsTerminalEvent 依据互斥 outcome 调用强类型 Event 构造器，未知类型失败关闭。
func newAnalyzeMaterialsTerminalEvent(
	eventType string,
	claim analyzematerialsruntime.Claim,
	sourceID string,
	card event.AnalyzeMaterialsPreviewCardPayload,
	createdAt time.Time,
) (event.Record, error) {
	switch eventType {
	case "analyze_materials.preview.completed":
		return event.NewAnalyzeMaterialsPreviewCompleted(claim.TerminalEventID, claim.Context.SessionID, sourceID, card, 1, createdAt)
	case "analyze_materials.preview.partial":
		return event.NewAnalyzeMaterialsPreviewPartial(claim.TerminalEventID, claim.Context.SessionID, sourceID, card, 1, createdAt)
	case "analyze_materials.preview.failed":
		return event.NewAnalyzeMaterialsPreviewFailed(claim.TerminalEventID, claim.Context.SessionID, sourceID, card, 1, createdAt)
	case "analyze_materials.preview.runtime_failed":
		return event.NewAnalyzeMaterialsPreviewRuntimeFailed(claim.TerminalEventID, claim.Context.SessionID, sourceID, card, 1, createdAt)
	default:
		return event.Record{}, analyzematerialsruntime.ErrOutputContract
	}
}

// mapAnalyzeMaterialsEventRecord 显式映射强类型 Event，不通过 JSON 反射复制持久化字段。
func mapAnalyzeMaterialsEventRecord(record event.Record) sessionEventLogModel {
	return sessionEventLogModel{
		EventID: record.EventID, SessionID: record.SessionID, EventType: string(record.Type),
		SchemaVersion: record.SchemaVersion, SourceKind: record.SourceKind, SourceID: record.SourceID,
		ProjectionIndex: record.ProjectionIndex, AggregateType: string(record.AggregateType),
		AggregateID: record.AggregateID, AggregateVersion: record.AggregateVersion,
		Payload: string(record.PayloadJSON), CreatedAt: record.CreatedAt,
	}
}

// mapAnalyzeMaterialsEnqueueResult 返回首次冻结的稳定身份，重放不混入当前 HTTP RequestID。
func mapAnalyzeMaterialsEnqueueResult(record analyzeMaterialsPreviewRunModel, replayed bool) analyzematerialsruntime.EnqueueResult {
	return analyzematerialsruntime.EnqueueResult{
		InputID: record.InputID, TurnID: record.TurnID, RunID: record.RunID, ToolCallID: record.ToolCallID,
		RouterModelCallID: record.RouterModelCallID, GraphModelCallID: record.GraphModelCallID,
		AcceptedEventID: record.AcceptedEventID, TerminalEventID: record.TerminalEventID, Replayed: replayed,
	}
}

// mapAnalyzeMaterialsContextModel 显式映射不可变 Context 与受保护 Intent，避免隐式序列化漂移。
func mapAnalyzeMaterialsContextModel(
	value turncontext.MaterialAnalysisTurnContext,
	ciphertext []byte,
	createdAt time.Time,
) analyzeMaterialsPreviewContextModel {
	return analyzeMaterialsPreviewContextModel{
		TurnID: value.TurnID, Profile: value.Profile, SchemaVersion: value.SchemaVersion,
		SessionID: value.SessionID, InputID: value.InputID, RunID: value.RunID, ToolCallID: value.ToolCallID,
		UserID: value.UserID, ProjectID: value.ProjectID, IntentCiphertext: append([]byte(nil), ciphertext...),
		IntentKeyVersion: value.IntentKeyVersion, IntentDigest: value.IntentDigest,
		AccessScopeRef: value.AccessScopeRef, AccessScopeDigest: value.AccessScopeDigest,
		ToolRegistryRef: value.ToolRegistryRef, ToolRegistryDigest: value.ToolRegistryDigest,
		ToolDefinitionRef: value.ToolDefinitionRef, ToolDefinitionDigest: value.ToolDefinitionDigest,
		IntentSchemaRef: value.IntentSchemaRef, ResultSchemaRef: value.ResultSchemaRef,
		PromptRef: value.PromptRef, PromptDigest: value.PromptDigest,
		ValidatorRef: value.ValidatorRef, ValidatorDigest: value.ValidatorDigest,
		EvidencePolicyRef: value.EvidencePolicyRef, EvidencePolicyDigest: value.EvidencePolicyDigest,
		RouterModelRouteRef: value.RouterModelRouteRef, RouterModelRouteDigest: value.RouterModelRouteDigest,
		AnalysisModelRouteRef: value.AnalysisModelRouteRef, AnalysisModelRouteDigest: value.AnalysisModelRouteDigest,
		RuntimePolicyRef: value.RuntimePolicyRef, RuntimePolicyDigest: value.RuntimePolicyDigest,
		BudgetRef: value.BudgetRef, BudgetDigest: value.BudgetDigest, ContextDigest: value.ContextDigest, CreatedAt: createdAt,
	}
}

// mapAnalyzeMaterialsClaim 只映射数据库冻结事实和本次合法 owner，不读取最新配置。
func mapAnalyzeMaterialsClaim(row analyzeMaterialsClaimRow, owner string) analyzematerialsruntime.Claim {
	contextRecord := row.Context
	return analyzematerialsruntime.Claim{
		Owner: owner, FenceToken: row.LeaseFence, Attempts: row.Attempts, EnqueueSeq: row.EnqueueSeq,
		RouterModelCallID: row.Context.RouterModelCallID, GraphModelCallID: row.Context.GraphModelCallID,
		TerminalEventID: row.TerminalEventID,
		Context: turncontext.MaterialAnalysisTurnContext{
			SchemaVersion: contextRecord.SchemaVersion, Profile: contextRecord.Profile,
			SessionID: contextRecord.SessionID, InputID: contextRecord.InputID, TurnID: contextRecord.TurnID,
			RunID: contextRecord.RunID, ToolCallID: contextRecord.ToolCallID, UserID: contextRecord.UserID,
			ProjectID: contextRecord.ProjectID, IntentKeyVersion: contextRecord.IntentKeyVersion,
			IntentDigest: contextRecord.IntentDigest, AccessScopeRef: contextRecord.AccessScopeRef,
			AccessScopeDigest: contextRecord.AccessScopeDigest, ToolRegistryRef: contextRecord.ToolRegistryRef,
			ToolRegistryDigest: contextRecord.ToolRegistryDigest, ToolDefinitionRef: contextRecord.ToolDefinitionRef,
			ToolDefinitionDigest: contextRecord.ToolDefinitionDigest, IntentSchemaRef: contextRecord.IntentSchemaRef,
			ResultSchemaRef: contextRecord.ResultSchemaRef, PromptRef: contextRecord.PromptRef,
			PromptDigest: contextRecord.PromptDigest, ValidatorRef: contextRecord.ValidatorRef,
			ValidatorDigest: contextRecord.ValidatorDigest, EvidencePolicyRef: contextRecord.EvidencePolicyRef,
			EvidencePolicyDigest: contextRecord.EvidencePolicyDigest, RouterModelRouteRef: contextRecord.RouterModelRouteRef,
			RouterModelRouteDigest: contextRecord.RouterModelRouteDigest, AnalysisModelRouteRef: contextRecord.AnalysisModelRouteRef,
			AnalysisModelRouteDigest: contextRecord.AnalysisModelRouteDigest, RuntimePolicyRef: contextRecord.RuntimePolicyRef,
			RuntimePolicyDigest: contextRecord.RuntimePolicyDigest, BudgetRef: contextRecord.BudgetRef,
			BudgetDigest: contextRecord.BudgetDigest, ContextDigest: contextRecord.ContextDigest,
		},
	}
}

// analyzeMaterialsToolRequestDigest 与 Tool Wrapper 的具名顺序保持一致，防止同键异义执行。
func analyzeMaterialsToolRequestDigest(value turncontext.MaterialAnalysisTurnContext) string {
	wire := value.ContextDigest + "\n" + value.ToolDefinitionRef + "\n" + value.ToolDefinitionDigest + "\n" +
		value.IntentSchemaRef + "\n" + value.ResultSchemaRef + "\n" + value.IntentDigest
	return digestAnalyzeMaterialsBytes([]byte(wire))
}

// analyzeMaterialsClaimFromModelIdentity 构造仅用于基础设施 Fence 校验的最小 Claim。
func analyzeMaterialsClaimFromModelIdentity(identity analyzematerialsruntime.ModelReceiptIdentity) analyzematerialsruntime.Claim {
	return analyzematerialsruntime.Claim{
		Owner: identity.Owner, FenceToken: identity.FenceToken,
		Context: turncontext.MaterialAnalysisTurnContext{
			SessionID: identity.SessionID, InputID: identity.InputID, TurnID: identity.TurnID, RunID: identity.RunID,
		},
	}
}

// analyzeMaterialsClaimFromToolIdentity 构造仅用于基础设施 Fence 校验的最小 Claim。
func analyzeMaterialsClaimFromToolIdentity(identity analyzematerialsruntime.ToolReceiptIdentity) analyzematerialsruntime.Claim {
	return analyzematerialsruntime.Claim{
		Owner: identity.Owner, FenceToken: identity.FenceToken,
		Context: turncontext.MaterialAnalysisTurnContext{
			SessionID: identity.SessionID, InputID: identity.InputID, TurnID: identity.TurnID,
			RunID: identity.RunID, ToolCallID: identity.ToolCallID,
		},
	}
}

// assertAnalyzeMaterialsModelIdentity 拒绝用 Router ID 写 Graph 命名空间或反向替换稳定调用 ID。
func assertAnalyzeMaterialsModelIdentity(tx *gorm.DB, identity analyzematerialsruntime.ModelReceiptIdentity) error {
	column := "router_model_call_id"
	if identity.CallKind == analyzematerialsruntime.ModelCallGraphAnalysis {
		column = "graph_model_call_id"
	}
	var count int64
	if err := tx.Model(&analyzeMaterialsPreviewRunModel{}).
		Where("run_id = ? AND input_id = ? AND turn_id = ? AND "+column+" = ?",
			identity.RunID, identity.InputID, identity.TurnID, identity.ModelCallID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return analyzematerialsruntime.ErrReceiptConflict
	}
	return nil
}

// validAnalyzeMaterialsModelKind 只接受批准的两层本地 Fake Model 命名空间。
func validAnalyzeMaterialsModelKind(kind analyzematerialsruntime.ModelCallKind) bool {
	return kind == analyzematerialsruntime.ModelCallRouter || kind == analyzematerialsruntime.ModelCallGraphAnalysis
}

// strictAnalyzeMaterialsJSON 拒绝未知字段和尾随 JSON，防止冻结后宽松解释。
func strictAnalyzeMaterialsJSON(encoded []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return analyzematerialsruntime.ErrOutputContract
	}
	return nil
}

// digestAnalyzeMaterialsBytes 返回 canonical 内容的 SHA-256 小写十六进制摘要。
func digestAnalyzeMaterialsBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// validAnalyzeMaterialsDigest 校验固定长度的小写 SHA-256 十六进制编码。
func validAnalyzeMaterialsDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

// canonicalAnalyzeMaterialsUUIDv7 拒绝非规范表示和非 UUIDv7 稳定标识。
func canonicalAnalyzeMaterialsUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

// mapAnalyzeMaterialsRuntimeError 保留稳定业务错误，其余数据库细节统一折叠为 Persistence。
func mapAnalyzeMaterialsRuntimeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, analyzematerialsruntime.ErrFenceLost),
		errors.Is(err, analyzematerialsruntime.ErrInvalidInput),
		errors.Is(err, analyzematerialsruntime.ErrNotFound),
		errors.Is(err, analyzematerialsruntime.ErrSessionLaneBlocked),
		errors.Is(err, analyzematerialsruntime.ErrInvalidClaim),
		errors.Is(err, analyzematerialsruntime.ErrReceiptConflict),
		errors.Is(err, analyzematerialsruntime.ErrOutputContract),
		errors.Is(err, analyzematerialsruntime.ErrIdempotencyConflict):
		return err
	default:
		return analyzematerialsruntime.ErrPersistence
	}
}

var _ analyzematerialsruntime.ExecutionStore = (*AnalyzeMaterialsRuntimeRepository)(nil)
var _ analyzematerialsruntime.ModelReceiptStore = (*AnalyzeMaterialsRuntimeRepository)(nil)
var _ analyzematerialsruntime.ToolReceiptStore = (*AnalyzeMaterialsRuntimeRepository)(nil)
