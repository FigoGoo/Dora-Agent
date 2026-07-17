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

	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/FigoGoo/Dora-Agent/agent/internal/writepromptsruntime"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const writePromptsSourceType = "write_prompts_preview"

// writePromptsContentProtector 约束 Intent、Model Response、prepared Command 与 Tool Result 使用同一受控 AEAD 边界。
type writePromptsContentProtector interface {
	Protect(context.Context, []byte) (session.ProtectedContent, error)
	Open(context.Context, session.ProtectedContent, string) ([]byte, error)
}

// writePromptsIDGenerator 为首次入队一次预分配稳定 UUIDv7；技术重放不得再次消费候选值。
type writePromptsIDGenerator interface {
	New() (string, error)
}

// WritePromptsRuntimeRepository 实现独立 Prompt Profile 的全 Source HOL、Lease/Fence、分层回执与 Unknown Recovery。
// Repository 不执行 Migration、不读取最新配置，也不把 Business Prompt Draft 复制为 Agent 权威状态。
type WritePromptsRuntimeRepository struct {
	// db 是只允许 Repository 使用的 GORM Agent PostgreSQL 连接。
	db *gorm.DB
	// protector 认证加密受控 Intent、Model Response、prepared Command 与 Tool Result。
	protector writePromptsContentProtector
	// ids 为首次入队生成应用侧 UUIDv7。
	ids writePromptsIDGenerator
}

// NewWritePromptsRuntimeRepository 创建不执行 Migration 或 AutoMigrate 的 PostgreSQL Adapter。
// client、protector 或 ids 缺失时返回错误，避免构造出会绕过加密或稳定身份的实例。
func NewWritePromptsRuntimeRepository(
	client *Client,
	protector writePromptsContentProtector,
	ids writePromptsIDGenerator,
) (*WritePromptsRuntimeRepository, error) {
	if client == nil || client.db == nil || protector == nil || ids == nil {
		return nil, fmt.Errorf("create write prompts runtime repository: dependency is nil")
	}
	return &WritePromptsRuntimeRepository{db: client.db, protector: protector, ids: ids}, nil
}

// Enqueue 在一个短事务中写无 Message Input、加密 Context、稳定 Run、open Tool Receipt 与 accepted Event。
// 同 Session 幂等键先由事务级 advisory lock 串行化；同义返回首次身份，PromptPreview 或 Intent 异义返回冲突。
func (r *WritePromptsRuntimeRepository) Enqueue(
	ctx context.Context,
	command writepromptsruntime.EnqueueCommand,
	_ time.Time,
) (writepromptsruntime.EnqueueResult, error) {
	canonicalIntent, err := writepromptsruntime.DecodeIntent(command.IntentJSON)
	requestDigest := digestWritePromptsEnqueue(command.StoryboardPreviewRef, canonicalIntent.Digest)
	if err != nil || !bytes.Equal(canonicalIntent.JSON, command.IntentJSON) ||
		!canonicalWritePromptsUUIDv7(command.RequestID) || !canonicalWritePromptsUUIDv7(command.SessionID) ||
		!canonicalWritePromptsUUIDv7(command.UserID) || !canonicalWritePromptsUUIDv7(command.ProjectID) ||
		!canonicalWritePromptsUUIDv7(command.IdempotencyKey) ||
		!canonicalWritePromptsUUIDv7(command.StoryboardPreviewRef.ID) || command.StoryboardPreviewRef.Version != 1 ||
		!validWritePromptsDigest(command.StoryboardPreviewRef.ContentDigest) || command.AccessScopeRef == "" ||
		!validWritePromptsDigest(command.AccessScopeDigest) || command.IntentKeyVersion == "" ||
		!validWritePromptsDigest(requestDigest) {
		return writepromptsruntime.EnqueueResult{}, writepromptsruntime.ErrInvalidInput
	}
	if existing, lookupErr := r.lookupWritePromptsEnqueue(ctx, command, requestDigest); lookupErr != nil || existing != nil {
		if lookupErr != nil {
			return writepromptsruntime.EnqueueResult{}, lookupErr
		}
		return *existing, nil
	}

	var result writepromptsruntime.EnqueueResult
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// advisory lock 只绑定 Session 幂等作用域，不依赖应用进程内锁，因此多实例并发仍是 first-write-wins。
		idempotencyScope := command.SessionID + ":" + command.IdempotencyKey
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", idempotencyScope).Error; err != nil {
			return err
		}
		var existing writePromptsPreviewRunModel
		lookupErr := tx.Where("session_id = ? AND idempotency_key = ?", command.SessionID, command.IdempotencyKey).Take(&existing).Error
		switch {
		case lookupErr == nil:
			if existing.RequestDigest != requestDigest || existing.UserID != command.UserID || existing.ProjectID != command.ProjectID {
				return writepromptsruntime.ErrIdempotencyConflict
			}
			result = mapWritePromptsEnqueueResult(existing, true)
			return nil
		case !errors.Is(lookupErr, gorm.ErrRecordNotFound):
			return lookupErr
		}

		protected, err := r.protector.Protect(ctx, canonicalIntent.JSON)
		if err != nil || protected.KeyVersion != command.IntentKeyVersion || len(protected.Ciphertext) == 0 {
			return writepromptsruntime.ErrPersistence
		}
		identities, err := r.newWritePromptsIdentities()
		if err != nil {
			return err
		}

		var target sessionModel
		if err := tx.Where("id = ? AND user_id = ? AND project_id = ? AND status = ? AND archived_at IS NULL",
			command.SessionID, command.UserID, command.ProjectID, string(session.StatusActive)).Take(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return writepromptsruntime.ErrNotFound
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
		case headErr == nil && head.SourceType != writePromptsSourceType:
			return writepromptsruntime.ErrSessionLaneBlocked
		case headErr != nil && !errors.Is(headErr, gorm.ErrRecordNotFound):
			return headErr
		}
		enqueueSeq := sequence.LastInputEnqueueSeq + 1
		if enqueueSeq < 1 {
			return writepromptsruntime.ErrPersistence
		}
		counterUpdate := tx.Model(&sessionSequenceCounterModel{}).
			Where("session_id = ? AND last_input_enqueue_seq = ?", command.SessionID, sequence.LastInputEnqueueSeq).
			Updates(map[string]any{"last_input_enqueue_seq": enqueueSeq, "updated_at": databaseNow})
		if counterUpdate.Error != nil || counterUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrPersistence
		}

		input := sessionInputModel{
			ID: identities.InputID, SessionID: command.SessionID, SourceType: writePromptsSourceType,
			SourceID: command.RequestID, MessageID: nil, Status: "pending", EnqueueSeq: enqueueSeq,
			Attempts: 0, AvailableAt: databaseNow, FenceToken: 0, CreatedAt: databaseNow, UpdatedAt: databaseNow,
		}
		if err := tx.Create(&input).Error; err != nil {
			return err
		}
		turnContext := r.buildWritePromptsTurnContext(command, canonicalIntent.Digest, protected.KeyVersion, identities)
		contextDigest, err := writepromptsruntime.DigestTurnContext(turnContext)
		if err != nil {
			return err
		}
		turnContext.ContextDigest = contextDigest
		run := writePromptsPreviewRunModel{
			InputID: identities.InputID, RequestID: command.RequestID, IdempotencyKey: command.IdempotencyKey,
			RequestDigest: requestDigest, SessionID: command.SessionID, UserID: command.UserID, ProjectID: command.ProjectID,
			TurnID: identities.TurnID, RunID: identities.RunID, ToolCallID: identities.ToolCallID,
			BusinessCommandID: identities.BusinessCommandID, RouterModelCallID: identities.RouterModelCallID,
			GraphModelCallID: identities.GraphModelCallID, AcceptedEventID: identities.AcceptedEventID,
			TerminalEventID: identities.TerminalEventID, OwnerFence: 0, Status: "created", Version: 1,
			CreatedAt: databaseNow, UpdatedAt: databaseNow,
		}
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		contextRecord := mapWritePromptsContextModel(turnContext, protected.Ciphertext, databaseNow)
		if err := tx.Create(&contextRecord).Error; err != nil {
			return err
		}
		toolReceipt := writePromptsPreviewToolReceiptModel{
			ToolCallID: identities.ToolCallID, RunID: identities.RunID, TurnID: identities.TurnID,
			InputID: identities.InputID, BusinessCommandID: identities.BusinessCommandID,
			RequestDigest: writePromptsToolRequestDigest(turnContext), ExecutionFence: 0,
			Status: string(writepromptsruntime.ToolReceiptOpen), CreatedAt: databaseNow,
		}
		if err := tx.Create(&toolReceipt).Error; err != nil {
			return err
		}
		acceptedPayload := event.WritePromptsPreviewAcceptedPayload{
			SchemaVersion: event.WritePromptsPreviewAcceptedSchemaVersionV1, InputID: identities.InputID,
			TurnID: identities.TurnID, RunID: identities.RunID, ToolCallID: identities.ToolCallID,
			BusinessCommandID: identities.BusinessCommandID, IntentDigest: canonicalIntent.Digest,
			ContextDigest: contextDigest, StoryboardPreviewID: command.StoryboardPreviewRef.ID,
			StoryboardPreviewVersion:       command.StoryboardPreviewRef.Version,
			StoryboardPreviewContentDigest: command.StoryboardPreviewRef.ContentDigest,
		}
		acceptedRecord, err := event.NewWritePromptsPreviewAccepted(
			identities.AcceptedEventID, command.SessionID, command.RequestID, acceptedPayload, databaseNow,
		)
		if err != nil {
			return err
		}
		accepted := sessionEventLogModel{
			EventID: acceptedRecord.EventID, SessionID: acceptedRecord.SessionID,
			EventType: string(acceptedRecord.Type), SchemaVersion: acceptedRecord.SchemaVersion,
			SourceKind: acceptedRecord.SourceKind, SourceID: acceptedRecord.SourceID,
			ProjectionIndex: acceptedRecord.ProjectionIndex, AggregateType: string(acceptedRecord.AggregateType),
			AggregateID: acceptedRecord.AggregateID, AggregateVersion: acceptedRecord.AggregateVersion,
			Payload: string(acceptedRecord.PayloadJSON), CreatedAt: acceptedRecord.CreatedAt,
		}
		if err := appendWritePromptsEvent(tx, databaseNow, accepted); err != nil {
			return err
		}
		result = mapWritePromptsEnqueueResult(run, false)
		return nil
	})
	if err != nil {
		return writepromptsruntime.EnqueueResult{}, mapWritePromptsRuntimeError(err)
	}
	return result, nil
}

// lookupWritePromptsEnqueue 在 KMS、随机源和候选 ID 之前重放已冻结的同义幂等事实。
func (r *WritePromptsRuntimeRepository) lookupWritePromptsEnqueue(
	ctx context.Context,
	command writepromptsruntime.EnqueueCommand,
	requestDigest string,
) (*writepromptsruntime.EnqueueResult, error) {
	var existing writePromptsPreviewRunModel
	err := r.db.WithContext(ctx).Where("session_id = ? AND idempotency_key = ?", command.SessionID, command.IdempotencyKey).Take(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	case err != nil:
		return nil, mapWritePromptsRuntimeError(err)
	case existing.RequestDigest != requestDigest || existing.UserID != command.UserID || existing.ProjectID != command.ProjectID:
		return nil, writepromptsruntime.ErrIdempotencyConflict
	default:
		result := mapWritePromptsEnqueueResult(existing, true)
		return &result, nil
	}
}

// writePromptsStableIdentities 保存首次入队一次生成并永久复用的 exact-set UUIDv7。
type writePromptsStableIdentities struct {
	// InputID 是无 Message Session Input UUIDv7。
	InputID string
	// TurnID 是技术重试复用的 Turn UUIDv7。
	TurnID string
	// RunID 是 Lease takeover 复用的 Run UUIDv7。
	RunID string
	// ToolCallID 是 Router 必须原样使用的 Tool Call UUIDv7。
	ToolCallID string
	// BusinessCommandID 是 Save/Query/Recovery 复用的 Business Command UUIDv7。
	BusinessCommandID string
	// RouterModelCallID 是外层 Router Model Call UUIDv7。
	RouterModelCallID string
	// GraphModelCallID 是 Graph Prompt Model Call UUIDv7。
	GraphModelCallID string
	// AcceptedEventID 是 typed accepted Event UUIDv7。
	AcceptedEventID string
	// TerminalEventID 是互斥终态共用的 Event UUIDv7。
	TerminalEventID string
}

// newWritePromptsIdentities 一次生成 exact-set UUIDv7；任一失败都不允许部分持久化。
func (r *WritePromptsRuntimeRepository) newWritePromptsIdentities() (writePromptsStableIdentities, error) {
	values := make([]string, 9)
	for index := range values {
		value, err := r.ids.New()
		if err != nil || !canonicalWritePromptsUUIDv7(value) {
			return writePromptsStableIdentities{}, writepromptsruntime.ErrPersistence
		}
		values[index] = value
	}
	return writePromptsStableIdentities{
		InputID: values[0], TurnID: values[1], RunID: values[2], ToolCallID: values[3],
		BusinessCommandID: values[4], RouterModelCallID: values[5], GraphModelCallID: values[6],
		AcceptedEventID: values[7], TerminalEventID: values[8],
	}, nil
}

// buildWritePromptsTurnContext 只从批准 pins、可信命令和首次稳定身份组装待摘要 Context。
func (r *WritePromptsRuntimeRepository) buildWritePromptsTurnContext(
	command writepromptsruntime.EnqueueCommand,
	intentDigest string,
	intentKeyVersion string,
	ids writePromptsStableIdentities,
) turncontext.WritePromptsTurnContext {
	pins := writepromptsruntime.ApprovedPins()
	return turncontext.WritePromptsTurnContext{
		SchemaVersion: turncontext.WritePromptsTurnContextSchemaVersion,
		Profile:       turncontext.WritePromptsRuntimeProfile, RequestID: command.RequestID,
		SessionID: command.SessionID, InputID: ids.InputID, TurnID: ids.TurnID, RunID: ids.RunID,
		ToolCallID: ids.ToolCallID, BusinessCommandID: ids.BusinessCommandID,
		RouterModelCallID: ids.RouterModelCallID, GraphModelCallID: ids.GraphModelCallID,
		UserID: command.UserID, ProjectID: command.ProjectID,
		IntentKeyVersion: intentKeyVersion, IntentDigest: intentDigest,
		StoryboardPreviewID: command.StoryboardPreviewRef.ID, StoryboardPreviewVersion: command.StoryboardPreviewRef.Version,
		StoryboardPreviewContentDigest: command.StoryboardPreviewRef.ContentDigest,
		AccessScopeRef:                 command.AccessScopeRef, AccessScopeDigest: command.AccessScopeDigest,
		ToolRegistryRef: pins.ToolRegistryRef, ToolRegistryDigest: pins.ToolRegistryDigest,
		ToolDefinitionRef: pins.ToolDefinitionRef, ToolDefinitionDigest: pins.ToolDefinitionDigest,
		IntentSchemaRef: writeprompts.IntentSchemaVersion, CandidateSchemaRef: writeprompts.CandidateSchemaVersion,
		ResultSchemaRef: writeprompts.ResultSchemaVersion,
		PromptRef:       pins.PromptRef, PromptDigest: pins.PromptDigest,
		ValidatorRef: pins.ValidatorRef, ValidatorDigest: pins.ValidatorDigest,
		ExactSetValidatorRef: pins.ExactSetValidatorRef, ExactSetValidatorDigest: pins.ExactSetValidatorDigest,
		RouterModelRouteRef: pins.RouterModelRouteRef, RouterModelRouteDigest: pins.RouterModelRouteDigest,
		PromptModelRouteRef: pins.PromptModelRouteRef, PromptModelRouteDigest: pins.PromptModelRouteDigest,
		RuntimePolicyRef: pins.RuntimePolicyRef, RuntimePolicyDigest: pins.RuntimePolicyDigest,
		BudgetRef: pins.BudgetRef, BudgetDigest: pins.BudgetDigest,
	}
}

// writePromptsClaimRow 承接一次全 Source HOL 查询返回的稳定身份、Context 与受保护 Intent。
type writePromptsClaimRow struct {
	// Context 是一次查询取回的完整不可变 Context 与受保护 Intent。
	Context writePromptsPreviewContextModel `gorm:"embedded"`
	// TerminalEventID 是当前 Input 唯一互斥终态 Event UUIDv7。
	TerminalEventID string `gorm:"column:terminal_event_id"`
	// EnqueueSeq 是全 Source HOL 使用的 Session Input 序号。
	EnqueueSeq int64 `gorm:"column:enqueue_seq"`
	// Attempts 是非投影/恢复执行已经领取的次数。
	Attempts int `gorm:"column:attempts"`
	// InputStatus 是 Claim 前状态，用于区分执行重试与不消耗 Attempts 的恢复。
	InputStatus string `gorm:"column:input_status"`
	// LeaseFence 是 Claim 前 Session Lane Fence。
	LeaseFence int64 `gorm:"column:lease_fence"`
	// LeaseVersion 是 Claim 前 Session Lease 乐观锁版本。
	LeaseVersion int64 `gorm:"column:lease_version"`
	// DatabaseNow 是同一查询获得的 PostgreSQL 权威时钟。
	DatabaseNow time.Time `gorm:"column:database_now"`
}

// ClaimNext 先计算每个 Session 的全 Source 最小非终态 Input，再只分派当前 Prompt Profile 的真正 HOL。
// Claim 使用短事务、数据库时钟、SKIP LOCKED 与 Session/Input/Run 三层同一 Fence，避免多实例重复提交。
func (r *WritePromptsRuntimeRepository) ClaimNext(
	ctx context.Context,
	owner string,
	_ time.Time,
	leaseDuration time.Duration,
) (*writepromptsruntime.Claim, error) {
	if owner == "" || leaseDuration <= 0 {
		return nil, writepromptsruntime.ErrInvalidClaim
	}
	var row writePromptsClaimRow
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
			JOIN agent.write_prompts_preview_run AS run_record ON run_record.input_id = input_record.id
			JOIN agent.write_prompts_preview_turn_context AS context_record ON context_record.input_id = input_record.id
			WHERE input_record.source_type = 'write_prompts_preview'
			  AND input_record.status IN ('pending','claimed','running','retry_wait','recovery_pending')
			  AND input_record.available_at <= database_clock.database_now
			  AND session_record.status = 'active' AND session_record.archived_at IS NULL
			  AND run_record.session_id = context_record.session_id
			  AND run_record.input_id = context_record.input_id
			  AND run_record.request_id = context_record.request_id
			  AND run_record.turn_id = context_record.turn_id
			  AND run_record.run_id = context_record.run_id
			  AND run_record.tool_call_id = context_record.tool_call_id
			  AND run_record.business_command_id = context_record.business_command_id
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
			return writepromptsruntime.ErrFenceLost
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
			return writepromptsruntime.ErrFenceLost
		}
		nextAttempts := row.Attempts
		if row.InputStatus != "recovery_pending" {
			nextAttempts++
		}
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND status IN ?", row.Context.InputID, []string{"pending", "claimed", "running", "retry_wait", "recovery_pending"}).
			Updates(map[string]any{
				"status": "claimed", "attempts": nextAttempts, "lease_owner": owner,
				"lease_until": leaseUntil, "fence_token": newFence, "updated_at": row.DatabaseNow,
			})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		runUpdate := tx.Model(&writePromptsPreviewRunModel{}).
			Where("input_id = ? AND run_id = ? AND status IN ?", row.Context.InputID, row.Context.RunID,
				[]string{"created", "running", "recovery_pending"}).
			Updates(map[string]any{"owner_fence": newFence, "version": gorm.Expr("version + 1"), "updated_at": row.DatabaseNow})
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		row.LeaseFence, row.Attempts = newFence, nextAttempts
		return nil
	})
	if err != nil {
		return nil, mapWritePromptsRuntimeError(err)
	}
	if row.Context.InputID == "" {
		return nil, nil
	}
	claim := mapWritePromptsClaim(row, owner)
	plaintext, openErr := r.protector.Open(ctx, session.ProtectedContent{
		Ciphertext: append([]byte(nil), row.Context.IntentCiphertext...), KeyVersion: row.Context.IntentKeyVersion,
	}, row.Context.IntentDigest)
	if openErr == nil {
		claim.IntentJSON = append([]byte(nil), plaintext...)
	}
	return &claim, nil
}

// MarkRunning 原子推进 Input 与 Run；任何零行都表示当前 owner/fence 已失效。
// recovery_pending 的 Run 重新进入 running 只表示继续确定性恢复，不会改变稳定身份或重置预算。
func (r *WritePromptsRuntimeRepository) MarkRunning(
	ctx context.Context,
	claim writepromptsruntime.Claim,
	_ time.Time,
) error {
	return r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND status = 'claimed' AND lease_owner = ? AND fence_token = ?",
				claim.Context.InputID, claim.Owner, claim.FenceToken).
			Updates(map[string]any{"status": "running", "updated_at": databaseNow})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		runUpdate := tx.Model(&writePromptsPreviewRunModel{}).
			Where("run_id = ? AND owner_fence = ? AND status IN ?", claim.Context.RunID, claim.FenceToken,
				[]string{"created", "running", "recovery_pending"}).
			Updates(map[string]any{
				"status": "running", "started_at": gorm.Expr("COALESCE(started_at, ?)", databaseNow),
				"version": gorm.Expr("version + 1"), "updated_at": databaseNow,
			})
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		return nil
	})
}

// RenewLease 使用 PostgreSQL clock 同事务延长 Session 与 Input 的相同 owner/fence。
// 续租不改变 Receipt 状态或业务重发预算，超时或 stale Fence 一律返回 ErrFenceLost。
func (r *WritePromptsRuntimeRepository) RenewLease(
	ctx context.Context,
	claim writepromptsruntime.Claim,
	_ time.Time,
	leaseDuration time.Duration,
) error {
	if leaseDuration <= 0 {
		return writepromptsruntime.ErrInvalidClaim
	}
	return r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		leaseUntil := databaseNow.Add(leaseDuration)
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?",
				claim.Context.InputID, claim.Owner, claim.FenceToken, []string{"claimed", "running"}).
			Updates(map[string]any{"lease_until": leaseUntil, "updated_at": databaseNow})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
			Where("session_id = ? AND lease_owner = ? AND fence_token = ?", claim.Context.SessionID, claim.Owner, claim.FenceToken).
			Updates(map[string]any{"lease_until": leaseUntil, "version": gorm.Expr("version + 1"), "updated_at": databaseNow})
		if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		return nil
	})
}

// LoadToolReceipt 在当前 Fence 下读取并认证解密 open、prepared、unknown 或 terminal 回执。
// 本方法只读且不抢占 open 执行权；prepared Command 的两类摘要、Business Command ID 与恢复预算必须全部匹配。
func (r *WritePromptsRuntimeRepository) LoadToolReceipt(
	ctx context.Context,
	claim writepromptsruntime.Claim,
) (writepromptsruntime.ToolReceiptSnapshot, error) {
	var snapshot writepromptsruntime.ToolReceiptSnapshot
	err := r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, _ time.Time) error {
		var record writePromptsPreviewToolReceiptModel
		if err := tx.Where("tool_call_id = ?", claim.Context.ToolCallID).Take(&record).Error; err != nil {
			return err
		}
		if err := assertWritePromptsToolRecord(record, claim.Context, writePromptsToolRequestDigest(claim.Context)); err != nil {
			return err
		}
		loaded, err := r.decodeWritePromptsToolReceipt(ctx, record, claim.Context, claim.Owner, claim.FenceToken)
		if err != nil {
			return err
		}
		snapshot = loaded
		return nil
	})
	return snapshot, err
}

// CompleteToolResult 重验 frozen terminal Receipt，append-once 写安全 Card Event，并原子释放 Session Lane。
// completed Card 只由冻结 Result 与 prepared Command 重建；技术重放不得重新调用模型或 Business Save。
func (r *WritePromptsRuntimeRepository) CompleteToolResult(
	ctx context.Context,
	claim writepromptsruntime.Claim,
	result writeprompts.Result,
	_ time.Time,
) error {
	trusted := writepromptsruntime.CoreContextFromRuntime(writepromptsruntime.RuntimeContextFromClaim(claim))
	if writeprompts.ValidateTerminalResult(result, trusted) != nil {
		return writepromptsruntime.ErrOutputContract
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return writepromptsruntime.ErrOutputContract
	}
	resultDigest := digestWritePromptsBytes(encoded)
	return r.completeWritePromptsTerminal(ctx, claim, &result, nil, resultDigest, true)
}

// CompleteRuntimeFailure 写独立 runtime_failed Event 并把 Input/Run 推进 dead/failed，不伪造合法 Tool failed Result。
// 失败载荷只允许稳定错误码、安全摘要和 retryable 标志，内部错误、密文与 Provider 信息不得持久化到 Event。
func (r *WritePromptsRuntimeRepository) CompleteRuntimeFailure(
	ctx context.Context,
	claim writepromptsruntime.Claim,
	failure writepromptsruntime.RuntimeFailure,
	_ time.Time,
) error {
	if failure.SchemaVersion != "write_prompts.preview.runtime_failure.v1" ||
		failure.InputID != claim.Context.InputID || failure.TurnID != claim.Context.TurnID ||
		failure.RunID != claim.Context.RunID || failure.Code == "" || failure.Summary == "" {
		return writepromptsruntime.ErrOutputContract
	}
	encoded, err := json.Marshal(failure)
	if err != nil {
		return writepromptsruntime.ErrOutputContract
	}
	return r.completeWritePromptsTerminal(ctx, claim, nil, &failure, digestWritePromptsBytes(encoded), false)
}

// completeWritePromptsTerminal 将 terminal Event、Input/Run 终态与 Lane release 原子提交。
// requireToolReceipt 为 true 时必须先证明密文 Tool Result 已冻结，避免投影成功却丢失可重放业务结果。
func (r *WritePromptsRuntimeRepository) completeWritePromptsTerminal(
	ctx context.Context,
	claim writepromptsruntime.Claim,
	result *writeprompts.Result,
	runtimeFailure *writepromptsruntime.RuntimeFailure,
	resultDigest string,
	requireToolReceipt bool,
) error {
	if !validWritePromptsDigest(resultDigest) || (result == nil) == (runtimeFailure == nil) {
		return writepromptsruntime.ErrOutputContract
	}
	return r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var activeRun writePromptsPreviewRunModel
		if err := tx.Where("run_id = ? AND input_id = ?", claim.Context.RunID, claim.Context.InputID).Take(&activeRun).Error; err != nil {
			return err
		}
		cardUpdatedAt := databaseNow.UTC()
		if requireToolReceipt {
			var receipt writePromptsPreviewToolReceiptModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", claim.Context.ToolCallID).Take(&receipt).Error; err != nil {
				return err
			}
			if receipt.Status != "completed" && receipt.Status != "failed" {
				return writepromptsruntime.ErrReceiptConflict
			}
			// terminal Receipt 本身 append-once；投影恢复可在更高 Session Fence 下重放，
			// 因此只核对稳定身份与结果摘要，不要求 Receipt 的历史执行 Fence 等于当前 Claim。
			if receipt.RunID != claim.Context.RunID || receipt.TurnID != claim.Context.TurnID ||
				receipt.InputID != claim.Context.InputID || receipt.BusinessCommandID != claim.Context.BusinessCommandID ||
				receipt.ResultDigest == nil || *receipt.ResultDigest != resultDigest || receipt.CompletedAt == nil {
				return writepromptsruntime.ErrReceiptConflict
			}
			cardUpdatedAt = receipt.CompletedAt.UTC()
		}
		card, eventType, runStatus, err := writePromptsTerminalCard(claim, result, runtimeFailure, cardUpdatedAt)
		if err != nil {
			return err
		}
		var terminalRecord event.Record
		switch eventType {
		case event.TypeWritePromptsPreviewCompleted:
			terminalRecord, err = event.NewWritePromptsPreviewCompleted(
				claim.TerminalEventID, claim.Context.SessionID, activeRun.RequestID, card, 1, databaseNow,
			)
		case event.TypeWritePromptsPreviewFailed:
			terminalRecord, err = event.NewWritePromptsPreviewFailed(
				claim.TerminalEventID, claim.Context.SessionID, activeRun.RequestID, card, 1, databaseNow,
			)
		case event.TypeWritePromptsPreviewRuntimeFailed:
			terminalRecord, err = event.NewWritePromptsPreviewRuntimeFailed(
				claim.TerminalEventID, claim.Context.SessionID, activeRun.RequestID, card, 1, databaseNow,
			)
		default:
			err = writepromptsruntime.ErrOutputContract
		}
		if err != nil {
			return fmt.Errorf("%w: build write prompts terminal event: %v", writepromptsruntime.ErrOutputContract, err)
		}
		terminal := sessionEventLogModel{
			EventID: terminalRecord.EventID, SessionID: terminalRecord.SessionID,
			EventType: string(terminalRecord.Type), SchemaVersion: terminalRecord.SchemaVersion,
			SourceKind: terminalRecord.SourceKind, SourceID: terminalRecord.SourceID,
			ProjectionIndex: terminalRecord.ProjectionIndex, AggregateType: string(terminalRecord.AggregateType),
			AggregateID: terminalRecord.AggregateID, AggregateVersion: terminalRecord.AggregateVersion,
			Payload: string(terminalRecord.PayloadJSON), CreatedAt: terminalRecord.CreatedAt,
		}
		if err := appendWritePromptsEvent(tx, databaseNow, terminal); err != nil {
			return err
		}
		inputStatus := "resolved"
		allowedInputStatuses := []string{"running"}
		allowedRunStatuses := []string{"running"}
		if runStatus == "failed" {
			inputStatus = "dead"
			allowedInputStatuses = []string{"claimed", "running"}
			allowedRunStatuses = []string{"created", "running", "recovery_pending"}
		}
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?",
				claim.Context.InputID, claim.Owner, claim.FenceToken, allowedInputStatuses).
			Updates(map[string]any{"status": inputStatus, "lease_owner": nil, "lease_until": nil, "updated_at": databaseNow})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		runUpdates := map[string]any{
			"status": runStatus, "completed_at": databaseNow,
			"version": gorm.Expr("version + 1"), "updated_at": databaseNow,
		}
		if runStatus == "failed" {
			runUpdates["started_at"] = gorm.Expr("COALESCE(started_at, ?)", databaseNow)
		}
		runUpdate := tx.Model(&writePromptsPreviewRunModel{}).
			Where("run_id = ? AND owner_fence = ? AND status IN ?", claim.Context.RunID, claim.FenceToken, allowedRunStatuses).
			Updates(runUpdates)
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		return releaseWritePromptsLane(tx, claim, databaseNow)
	})
}

// writePromptsTerminalCard 将已冻结 Tool Result/Runtime Failure 映射为前端唯一允许的 Card exact-set。
func writePromptsTerminalCard(
	claim writepromptsruntime.Claim,
	result *writeprompts.Result,
	runtimeFailure *writepromptsruntime.RuntimeFailure,
	updatedAt time.Time,
) (event.WritePromptsPreviewCardPayload, event.Type, string, error) {
	base := event.WritePromptsPreviewCardPayload{
		SchemaVersion: event.WritePromptsPreviewCardSchemaVersionV1,
		InputID:       claim.Context.InputID, TurnID: claim.Context.TurnID, RunID: claim.Context.RunID,
		ToolCallID: claim.Context.ToolCallID, UpdatedAt: updatedAt.UTC(),
	}
	if runtimeFailure != nil {
		retryable := runtimeFailure.Retryable
		base.Status = "failed"
		base.ResultCode = runtimeFailure.Code
		base.FailureKind = event.WritePromptsPreviewFailureKindRuntime
		base.Summary = runtimeFailure.Summary
		base.Retryable = &retryable
		return base, event.TypeWritePromptsPreviewRuntimeFailed, "failed", nil
	}
	if result == nil {
		return event.WritePromptsPreviewCardPayload{}, "", "", writepromptsruntime.ErrOutputContract
	}
	base.Status = result.Status
	base.ResultCode = result.ResultCode
	if result.Status == "failed" {
		if result.Retryable == nil {
			return event.WritePromptsPreviewCardPayload{}, "", "", writepromptsruntime.ErrOutputContract
		}
		retryable := *result.Retryable
		base.FailureKind = event.WritePromptsPreviewFailureKindTool
		base.Summary = result.Summary
		base.Retryable = &retryable
		return base, event.TypeWritePromptsPreviewFailed, "completed", nil
	}
	if result.Status != "completed" || result.Card == nil {
		return event.WritePromptsPreviewCardPayload{}, "", "", writepromptsruntime.ErrOutputContract
	}
	draft := result.Card
	base = *draft
	base.UpdatedAt = updatedAt.UTC()
	base.Prompts = cloneWritePromptsEntries(draft.Prompts)
	return base, event.TypeWritePromptsPreviewCompleted, "completed", nil
}

// cloneWritePromptsEntries 深拷贝投影条目，避免 Card 编码修改回执中的恢复工件。
func cloneWritePromptsEntries(values []writeprompts.PromptEntry) []writeprompts.PromptEntry {
	cloned := append(make([]writeprompts.PromptEntry, 0, len(values)), values...)
	for index := range cloned {
		cloned[index].NegativeConstraints = append([]string{}, values[index].NegativeConstraints...)
	}
	return cloned
}

// RetryExecution 释放当前 Fence 并把尚未越过 Business 副作用边界的执行放回有限重试队列。
func (r *WritePromptsRuntimeRepository) RetryExecution(
	ctx context.Context,
	claim writepromptsruntime.Claim,
	availableAt time.Time,
) error {
	return r.deferWritePromptsInput(ctx, claim, availableAt, "retry_wait", "running", true)
}

// DeferRecovery 把 prepared/unknown Business 命令标记为 recovery_pending，后续 Claim 不增加模型执行 Attempts。
func (r *WritePromptsRuntimeRepository) DeferRecovery(
	ctx context.Context,
	claim writepromptsruntime.Claim,
	availableAt time.Time,
) error {
	return r.deferWritePromptsInput(ctx, claim, availableAt, "recovery_pending", "recovery_pending", false)
}

// DeferProjection 把已经冻结 terminal Tool Result 的 Event/Card 补偿标记为 recovery_pending。
func (r *WritePromptsRuntimeRepository) DeferProjection(
	ctx context.Context,
	claim writepromptsruntime.Claim,
	availableAt time.Time,
) error {
	return r.deferWritePromptsInput(ctx, claim, availableAt, "recovery_pending", "recovery_pending", false)
}

// deferWritePromptsInput 区分 open 执行 retry_wait 与不消耗 Attempts 的 prepared/result 恢复。
// 状态、可用时间、Input Lease、Run 和 Session Lease 在一个事务提交，防止释放 Lane 后丢失 HOL 真源。
func (r *WritePromptsRuntimeRepository) deferWritePromptsInput(
	ctx context.Context,
	claim writepromptsruntime.Claim,
	availableAt time.Time,
	inputStatus string,
	runStatus string,
	requireOpenReceipt bool,
) error {
	if availableAt.IsZero() {
		return writepromptsruntime.ErrInvalidClaim
	}
	return r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var receipt writePromptsPreviewToolReceiptModel
		if err := tx.Where("tool_call_id = ?", claim.Context.ToolCallID).Take(&receipt).Error; err != nil {
			return err
		}
		if requireOpenReceipt && receipt.Status != string(writepromptsruntime.ToolReceiptOpen) {
			return writepromptsruntime.ErrReceiptConflict
		}
		if !requireOpenReceipt && receipt.Status == string(writepromptsruntime.ToolReceiptOpen) {
			return writepromptsruntime.ErrReceiptConflict
		}
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?",
				claim.Context.InputID, claim.Owner, claim.FenceToken, []string{"claimed", "running"}).
			Updates(map[string]any{
				"status": inputStatus, "available_at": availableAt.UTC(), "lease_owner": nil,
				"lease_until": nil, "updated_at": databaseNow,
			})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		runUpdate := tx.Model(&writePromptsPreviewRunModel{}).
			Where("run_id = ? AND owner_fence = ? AND status IN ?", claim.Context.RunID, claim.FenceToken,
				[]string{"created", "running", "recovery_pending"}).
			Updates(map[string]any{"status": runStatus, "version": gorm.Expr("version + 1"), "updated_at": databaseNow})
		if runUpdate.Error != nil || runUpdate.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		return releaseWritePromptsLane(tx, claim, databaseNow)
	})
}

// ReplayOrReserveModel 对 Router/Graph Prompt Model 分别创建或重放 first-write-wins 回执。
// reserved 回执只允许更高有效 Session Fence takeover，terminal 回执只解密重放而不再次调用模型。
func (r *WritePromptsRuntimeRepository) ReplayOrReserveModel(
	ctx context.Context,
	identity writepromptsruntime.ModelReceiptIdentity,
	requestDigest string,
) (writepromptsruntime.ModelReceiptSnapshot, bool, error) {
	claim := writePromptsClaimFromModelIdentity(identity)
	var snapshot writepromptsruntime.ModelReceiptSnapshot
	var execute bool
	err := r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		if !validWritePromptsDigest(requestDigest) || !validWritePromptsModelKind(identity.CallKind) {
			return writepromptsruntime.ErrReceiptConflict
		}
		// Receipt 首次不存在时 SELECT FOR UPDATE 无行可锁；以稳定 ModelCallID 的
		// 事务级 advisory lock 串行化跨实例首写，避免并发 INSERT 唯一键冲突被误报为持久层不可用。
		lockScope := "write_prompts:model_receipt:" + identity.ModelCallID
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", lockScope).Error; err != nil {
			return err
		}
		if err := assertWritePromptsModelIdentity(tx, identity); err != nil {
			return err
		}
		var record writePromptsPreviewModelReceiptModel
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("model_call_id = ?", identity.ModelCallID).Take(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			record = writePromptsPreviewModelReceiptModel{
				ModelCallID: identity.ModelCallID, CallKind: string(identity.CallKind), RunID: identity.RunID,
				TurnID: identity.TurnID, InputID: identity.InputID, RequestDigest: requestDigest,
				ExecutionFence: identity.FenceToken, Status: string(writepromptsruntime.ModelReceiptReserved), CreatedAt: databaseNow,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			snapshot.Stage, execute = writepromptsruntime.ModelReceiptReserved, true
			return nil
		}
		if err != nil {
			return err
		}
		if record.CallKind != string(identity.CallKind) || record.RunID != identity.RunID ||
			record.TurnID != identity.TurnID || record.InputID != identity.InputID || record.RequestDigest != requestDigest {
			return writepromptsruntime.ErrReceiptConflict
		}
		snapshot.Stage = writepromptsruntime.ModelReceiptStage(record.Status)
		switch snapshot.Stage {
		case writepromptsruntime.ModelReceiptReserved:
			switch {
			case identity.FenceToken < record.ExecutionFence:
				return writepromptsruntime.ErrFenceLost
			default:
				// reserved 表示上一个进程可能已经调用过模型但尚未冻结结果。即使更高 Fence takeover，
				// 也不能再次调用 Fake/Provider；Processor 会把不可证明结果的调用失败关闭为 Runtime Failure。
				execute = false
			}
		case writepromptsruntime.ModelReceiptCompleted:
			if record.ResponseKeyVersion == nil || record.ResponseDigest == nil {
				return writepromptsruntime.ErrReceiptConflict
			}
			plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
				Ciphertext: append([]byte(nil), record.ResponseCiphertext...), KeyVersion: *record.ResponseKeyVersion,
			}, *record.ResponseDigest)
			if err != nil {
				return writepromptsruntime.ErrReceiptConflict
			}
			var message schema.Message
			if err := strictWritePromptsJSON(plaintext, &message); err != nil {
				return writepromptsruntime.ErrReceiptConflict
			}
			snapshot.Response = &message
		case writepromptsruntime.ModelReceiptFailed:
			if record.ErrorCode == nil || *record.ErrorCode == "" {
				return writepromptsruntime.ErrReceiptConflict
			}
			snapshot.ErrorCode = *record.ErrorCode
		default:
			return writepromptsruntime.ErrReceiptConflict
		}
		return nil
	})
	return snapshot, execute, err
}

// FreezeModelCompleted 认证加密完整 classic Message，并以当前 Fence first-write-wins 冻结。
func (r *WritePromptsRuntimeRepository) FreezeModelCompleted(
	ctx context.Context,
	identity writepromptsruntime.ModelReceiptIdentity,
	requestDigest string,
	response *schema.Message,
) error {
	if response == nil {
		return writepromptsruntime.ErrOutputContract
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return writepromptsruntime.ErrOutputContract
	}
	protected, err := r.protector.Protect(ctx, encoded)
	if err != nil {
		return writepromptsruntime.ErrPersistence
	}
	return r.freezeWritePromptsModel(ctx, identity, requestDigest, "completed", protected,
		digestWritePromptsBytes(encoded), "")
}

// FreezeModelFailed 只冻结稳定脱敏错误码，不保存 Fake Model 内部错误原文。
func (r *WritePromptsRuntimeRepository) FreezeModelFailed(
	ctx context.Context,
	identity writepromptsruntime.ModelReceiptIdentity,
	requestDigest string,
	errorCode string,
) error {
	if errorCode == "" {
		return writepromptsruntime.ErrOutputContract
	}
	return r.freezeWritePromptsModel(ctx, identity, requestDigest, "failed", session.ProtectedContent{}, "", errorCode)
}

// freezeWritePromptsModel 仅允许当前 Fence 把 reserved 首写推进为相同请求的单一终态。
// 同终态同摘要重放返回成功；任何异义、命名空间错配或 stale Fence 均拒绝覆盖。
func (r *WritePromptsRuntimeRepository) freezeWritePromptsModel(
	ctx context.Context,
	identity writepromptsruntime.ModelReceiptIdentity,
	requestDigest string,
	status string,
	protected session.ProtectedContent,
	digest string,
	errorCode string,
) error {
	claim := writePromptsClaimFromModelIdentity(identity)
	return r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var record writePromptsPreviewModelReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("model_call_id = ?", identity.ModelCallID).Take(&record).Error; err != nil {
			return err
		}
		if record.CallKind != string(identity.CallKind) || record.RunID != identity.RunID ||
			record.TurnID != identity.TurnID || record.InputID != identity.InputID || record.RequestDigest != requestDigest {
			return writepromptsruntime.ErrReceiptConflict
		}
		if record.Status != "reserved" {
			if record.Status == status && ((status == "completed" && record.ResponseDigest != nil && *record.ResponseDigest == digest) ||
				(status == "failed" && record.ErrorCode != nil && *record.ErrorCode == errorCode)) {
				return nil
			}
			return writepromptsruntime.ErrReceiptConflict
		}
		if record.ExecutionFence != identity.FenceToken {
			return writepromptsruntime.ErrFenceLost
		}
		updates := map[string]any{"status": status, "completed_at": databaseNow}
		if status == "completed" {
			if len(protected.Ciphertext) == 0 || protected.KeyVersion == "" || !validWritePromptsDigest(digest) {
				return writepromptsruntime.ErrOutputContract
			}
			updates["response_ciphertext"] = protected.Ciphertext
			updates["response_key_version"] = protected.KeyVersion
			updates["response_digest"] = digest
		} else if status == "failed" && errorCode != "" {
			updates["error_code"] = errorCode
		} else {
			return writepromptsruntime.ErrOutputContract
		}
		update := tx.Model(&writePromptsPreviewModelReceiptModel{}).
			Where("model_call_id = ? AND status = 'reserved' AND execution_fence = ?", identity.ModelCallID, identity.FenceToken).
			Updates(updates)
		if update.Error != nil || update.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		return nil
	})
}

// ReplayOrOpenTool 在入队 open Receipt 上取得当前 Fence 执行权，或认证解密并重放 prepared/unknown/terminal 事实。
// prepared 与 unknown takeover 只推进 Fence，不消耗重发预算；业务查询确定 not_found 后必须另调 ReserveToolCommandResend。
func (r *WritePromptsRuntimeRepository) ReplayOrOpenTool(
	ctx context.Context,
	identity writepromptsruntime.ToolReceiptIdentity,
	requestDigest string,
) (writepromptsruntime.ToolReceiptSnapshot, bool, error) {
	claim := writePromptsClaimFromToolIdentity(identity)
	var snapshot writepromptsruntime.ToolReceiptSnapshot
	var execute bool
	err := r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, _ time.Time) error {
		var record writePromptsPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", identity.ToolCallID).Take(&record).Error; err != nil {
			return err
		}
		if record.RunID != identity.RunID || record.TurnID != identity.TurnID || record.InputID != identity.InputID ||
			record.BusinessCommandID != identity.BusinessCommandID || record.RequestDigest != requestDigest {
			return writepromptsruntime.ErrReceiptConflict
		}
		stage := writepromptsruntime.ToolReceiptStage(record.Status)
		switch stage {
		case writepromptsruntime.ToolReceiptOpen:
			switch {
			case identity.FenceToken < record.ExecutionFence:
				return writepromptsruntime.ErrFenceLost
			case identity.FenceToken == record.ExecutionFence:
				execute = false
			default:
				advance := tx.Model(&writePromptsPreviewToolReceiptModel{}).
					Where("tool_call_id = ? AND status = 'open' AND execution_fence = ?", identity.ToolCallID, record.ExecutionFence).
					Update("execution_fence", identity.FenceToken)
				if advance.Error != nil || advance.RowsAffected != 1 {
					return writepromptsruntime.ErrFenceLost
				}
				record.ExecutionFence = identity.FenceToken
				execute = true
			}
		case writepromptsruntime.ToolReceiptBusinessPrepared, writepromptsruntime.ToolReceiptBusinessUnknown:
			if identity.FenceToken < record.ExecutionFence {
				return writepromptsruntime.ErrFenceLost
			}
			if identity.FenceToken > record.ExecutionFence {
				advance := tx.Model(&writePromptsPreviewToolReceiptModel{}).
					Where("tool_call_id = ? AND status = ? AND execution_fence = ?", identity.ToolCallID, record.Status, record.ExecutionFence).
					Update("execution_fence", identity.FenceToken)
				if advance.Error != nil || advance.RowsAffected != 1 {
					return writepromptsruntime.ErrFenceLost
				}
				record.ExecutionFence = identity.FenceToken
			}
		case writepromptsruntime.ToolReceiptCompleted, writepromptsruntime.ToolReceiptFailed:
			// terminal Receipt 可在任意后续有效 Session Fence 下重放，但数据库触发器禁止再写。
		default:
			return writepromptsruntime.ErrReceiptConflict
		}
		storedContext, err := loadWritePromptsTurnContext(tx, identity.TurnID)
		if err != nil {
			return err
		}
		loaded, err := r.decodeWritePromptsToolReceipt(ctx, record, storedContext, identity.Owner, identity.FenceToken)
		if err != nil {
			return err
		}
		snapshot = loaded
		return nil
	})
	return snapshot, execute, err
}

// PrepareToolCommand 在 Save RPC 前冻结完整稳定业务命令语义、加密恢复命令、Project Fence 与重发上限。
// 重放必须逐项匹配；一旦 prepared，命令正文、摘要、版本和预算均由数据库触发器永久禁止漂移。
func (r *WritePromptsRuntimeRepository) PrepareToolCommand(
	ctx context.Context,
	identity writepromptsruntime.ToolReceiptIdentity,
	outerRequestDigest string,
	command writeprompts.DraftCommand,
	commandDigest string,
	contentDigest string,
	resendLimit int,
) error {
	if !validWritePromptsDigest(outerRequestDigest) || !validWritePromptsDigest(commandDigest) ||
		!validWritePromptsDigest(contentDigest) || resendLimit < 0 ||
		command.TrustedContext.SessionID != identity.SessionID || command.TrustedContext.InputID != identity.InputID ||
		command.TrustedContext.TurnID != identity.TurnID || command.TrustedContext.RunID != identity.RunID ||
		command.TrustedContext.ToolCallID != identity.ToolCallID ||
		command.TrustedContext.BusinessCommandID != identity.BusinessCommandID || command.DomainContext.ProjectVersion < 1 {
		return writepromptsruntime.ErrOutputContract
	}
	recomputedRequest, err := writeprompts.SaveRequestDigest(command)
	if err != nil || recomputedRequest != command.RequestDigest {
		return writepromptsruntime.ErrOutputContract
	}
	recomputedCommand, err := digestWritePromptsPreparedCommand(command)
	if err != nil || recomputedCommand != commandDigest {
		return writepromptsruntime.ErrOutputContract
	}
	recomputedContent, err := writeprompts.ContentDigest(command.Content)
	if err != nil || recomputedContent != contentDigest {
		return writepromptsruntime.ErrOutputContract
	}
	// Unknown Outcome 跨 Claim 恢复需要 exact targets、scope digest 与 Source 快照；只保存 Content 会丢失 Business Guard。
	// Owner/Fence 属于当前 Claim，因此从密文工件剥离并在解密后由已验证 Context 重建。
	encodedCommand, err := encodeWritePromptsDurableCommand(command)
	if err != nil {
		return writepromptsruntime.ErrOutputContract
	}
	protected, err := r.protector.Protect(ctx, encodedCommand)
	if err != nil || len(protected.Ciphertext) == 0 || protected.KeyVersion == "" {
		return writepromptsruntime.ErrPersistence
	}
	claim := writePromptsClaimFromToolIdentity(identity)
	return r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var record writePromptsPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", identity.ToolCallID).Take(&record).Error; err != nil {
			return err
		}
		if record.RunID != identity.RunID || record.TurnID != identity.TurnID || record.InputID != identity.InputID ||
			record.BusinessCommandID != identity.BusinessCommandID || record.RequestDigest != outerRequestDigest {
			return writepromptsruntime.ErrReceiptConflict
		}
		if record.Status != string(writepromptsruntime.ToolReceiptOpen) {
			if record.CommandDigest != nil && record.BusinessRequestDigest != nil && record.ContentDigest != nil &&
				record.ExpectedProjectVersion != nil && *record.CommandDigest == commandDigest &&
				*record.BusinessRequestDigest == command.RequestDigest && *record.ContentDigest == contentDigest &&
				*record.ExpectedProjectVersion == command.DomainContext.ProjectVersion && record.ResendLimit == resendLimit {
				return nil
			}
			return writepromptsruntime.ErrReceiptConflict
		}
		if record.ExecutionFence != identity.FenceToken || identity.FenceToken <= 0 {
			return writepromptsruntime.ErrFenceLost
		}
		update := tx.Model(&writePromptsPreviewToolReceiptModel{}).
			Where("tool_call_id = ? AND status = 'open' AND execution_fence = ?", identity.ToolCallID, identity.FenceToken).
			Updates(map[string]any{
				"status":             string(writepromptsruntime.ToolReceiptBusinessPrepared),
				"command_ciphertext": protected.Ciphertext, "command_key_version": protected.KeyVersion,
				"command_digest": commandDigest, "expected_project_version": command.DomainContext.ProjectVersion,
				"business_request_digest": command.RequestDigest, "content_digest": contentDigest,
				"resend_attempts": 0, "resend_limit": resendLimit, "prepared_at": databaseNow,
			})
		if update.Error != nil || update.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		return nil
	})
}

// MarkToolBusinessUnknown 把 prepared Save 的歧义冻结为 business_unknown；重复 unknown 是无增量幂等。
// open 阶段不能直接进入 unknown，因为没有可认证重放的完整命令；terminal 阶段也不得被降级。
func (r *WritePromptsRuntimeRepository) MarkToolBusinessUnknown(
	ctx context.Context,
	identity writepromptsruntime.ToolReceiptIdentity,
	requestDigest string,
) error {
	claim := writePromptsClaimFromToolIdentity(identity)
	return r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var record writePromptsPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", identity.ToolCallID).Take(&record).Error; err != nil {
			return err
		}
		if record.RunID != identity.RunID || record.TurnID != identity.TurnID || record.InputID != identity.InputID ||
			record.BusinessCommandID != identity.BusinessCommandID || record.RequestDigest != requestDigest {
			return writepromptsruntime.ErrReceiptConflict
		}
		if record.Status == string(writepromptsruntime.ToolReceiptBusinessUnknown) {
			return nil
		}
		if record.Status != string(writepromptsruntime.ToolReceiptBusinessPrepared) || record.ExecutionFence != identity.FenceToken {
			return writepromptsruntime.ErrReceiptConflict
		}
		update := tx.Model(&writePromptsPreviewToolReceiptModel{}).
			Where("tool_call_id = ? AND status = 'business_prepared' AND execution_fence = ?", identity.ToolCallID, identity.FenceToken).
			Updates(map[string]any{"status": string(writepromptsruntime.ToolReceiptBusinessUnknown), "unknown_at": databaseNow})
		if update.Error != nil || update.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		return nil
	})
}

// ReserveToolCommandResend 只在 business_unknown 且权威 Query 返回 not_found 后原子消耗一次同键重发预算。
// 调用方提供的 Recovery 必须与加密 prepared Command 完全一致；预算耗尽返回同一恢复事实和 false，不产生写入。
func (r *WritePromptsRuntimeRepository) ReserveToolCommandResend(
	ctx context.Context,
	identity writepromptsruntime.ToolReceiptIdentity,
	requestDigest string,
	recovery writeprompts.RecoveryDeferred,
) (writeprompts.RecoveryDeferred, bool, error) {
	claim := writePromptsClaimFromToolIdentity(identity)
	var updated writeprompts.RecoveryDeferred
	var reserved bool
	err := r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, _ time.Time) error {
		var record writePromptsPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", identity.ToolCallID).Take(&record).Error; err != nil {
			return err
		}
		if record.Status != string(writepromptsruntime.ToolReceiptBusinessUnknown) || record.ExecutionFence != identity.FenceToken ||
			record.RequestDigest != requestDigest || record.BusinessCommandID != identity.BusinessCommandID {
			return writepromptsruntime.ErrReceiptConflict
		}
		storedContext, err := loadWritePromptsTurnContext(tx, identity.TurnID)
		if err != nil {
			return err
		}
		snapshot, err := r.decodeWritePromptsToolReceipt(ctx, record, storedContext, identity.Owner, identity.FenceToken)
		if err != nil || snapshot.Recovery == nil || snapshot.PreparedCommand == nil {
			return writepromptsruntime.ErrReceiptConflict
		}
		persisted := *snapshot.Recovery
		if recovery.ToolCallID != persisted.ToolCallID || recovery.BusinessCommandID != persisted.BusinessCommandID ||
			recovery.RequestDigest != persisted.RequestDigest || recovery.ContentDigest != persisted.ContentDigest ||
			recovery.ResendAttempts > persisted.ResendAttempts || recovery.ResendLimit != persisted.ResendLimit ||
			digestWritePromptsRecoveryCommand(recovery.Command) != digestWritePromptsRecoveryCommand(persisted.Command) {
			return writepromptsruntime.ErrReceiptConflict
		}
		// 同一恢复请求的并发输家看到更大的已持久化 attempts 时重放首写结果，不再次消耗预算。
		if recovery.ResendAttempts < persisted.ResendAttempts {
			updated = persisted
			return nil
		}
		if record.ResendAttempts >= record.ResendLimit {
			persisted.ResendExhausted = true
			updated = persisted
			return nil
		}
		advance := tx.Model(&writePromptsPreviewToolReceiptModel{}).
			Where("tool_call_id = ? AND status = 'business_unknown' AND execution_fence = ? AND resend_attempts = ? AND resend_attempts < resend_limit",
				identity.ToolCallID, identity.FenceToken, record.ResendAttempts).
			Update("resend_attempts", gorm.Expr("resend_attempts + 1"))
		if advance.Error != nil || advance.RowsAffected != 1 {
			return writepromptsruntime.ErrReceiptConflict
		}
		persisted.ResendAttempts++
		persisted.ResendExhausted = persisted.ResendAttempts >= persisted.ResendLimit
		updated, reserved = persisted, true
		return nil
	})
	return updated, reserved, err
}

// FreezeToolResult 从 open、prepared 或 unknown first-write-wins 冻结确定性 failed Result；
// completed 必须先有 prepared Command，以便 Processor 可从 Receipt 重建完整 Card。
// 该转换必须保持当前 Fence；同终态同摘要重放成功，异义结果或对 terminal 的覆盖一律冲突。
func (r *WritePromptsRuntimeRepository) FreezeToolResult(
	ctx context.Context,
	identity writepromptsruntime.ToolReceiptIdentity,
	requestDigest string,
	stage writepromptsruntime.ToolReceiptStage,
	resultJSON []byte,
	resultDigest string,
) error {
	if (stage != writepromptsruntime.ToolReceiptCompleted && stage != writepromptsruntime.ToolReceiptFailed) ||
		digestWritePromptsBytes(resultJSON) != resultDigest {
		return writepromptsruntime.ErrOutputContract
	}
	var result writeprompts.Result
	if err := strictWritePromptsJSON(resultJSON, &result); err != nil || result.Status != string(stage) ||
		result.InvocationRef.ToolCallID != identity.ToolCallID ||
		result.InvocationRef.BusinessCommandID != identity.BusinessCommandID || result.ResultCode == "" {
		return writepromptsruntime.ErrOutputContract
	}
	protected, err := r.protector.Protect(ctx, resultJSON)
	if err != nil || len(protected.Ciphertext) == 0 || protected.KeyVersion == "" {
		return writepromptsruntime.ErrPersistence
	}
	claim := writePromptsClaimFromToolIdentity(identity)
	return r.withActiveWritePromptsFence(ctx, claim, func(tx *gorm.DB, databaseNow time.Time) error {
		var record writePromptsPreviewToolReceiptModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tool_call_id = ?", identity.ToolCallID).Take(&record).Error; err != nil {
			return err
		}
		if record.RunID != identity.RunID || record.TurnID != identity.TurnID || record.InputID != identity.InputID ||
			record.BusinessCommandID != identity.BusinessCommandID || record.RequestDigest != requestDigest {
			return writepromptsruntime.ErrReceiptConflict
		}
		if record.Status == string(stage) && record.ResultDigest != nil && *record.ResultDigest == resultDigest {
			return nil
		}
		if record.Status == string(writepromptsruntime.ToolReceiptCompleted) || record.Status == string(writepromptsruntime.ToolReceiptFailed) {
			return writepromptsruntime.ErrReceiptConflict
		}
		if stage == writepromptsruntime.ToolReceiptCompleted && record.Status == string(writepromptsruntime.ToolReceiptOpen) {
			return writepromptsruntime.ErrReceiptConflict
		}
		if record.Status != string(writepromptsruntime.ToolReceiptOpen) &&
			record.Status != string(writepromptsruntime.ToolReceiptBusinessPrepared) &&
			record.Status != string(writepromptsruntime.ToolReceiptBusinessUnknown) {
			return writepromptsruntime.ErrReceiptConflict
		}
		if record.ExecutionFence != identity.FenceToken || identity.FenceToken <= 0 {
			return writepromptsruntime.ErrFenceLost
		}
		update := tx.Model(&writePromptsPreviewToolReceiptModel{}).
			Where("tool_call_id = ? AND status = ? AND execution_fence = ?", identity.ToolCallID, record.Status, identity.FenceToken).
			Updates(map[string]any{
				"status": string(stage), "result_ciphertext": protected.Ciphertext,
				"result_key_version": protected.KeyVersion, "result_digest": resultDigest,
				"result_code": result.ResultCode, "completed_at": databaseNow,
			})
		if update.Error != nil || update.RowsAffected != 1 {
			return writepromptsruntime.ErrFenceLost
		}
		return nil
	})
}

// decodeWritePromptsToolReceipt 把密文 Receipt 显式映射为 Runtime Snapshot，并复核完整 prepared 恢复工件。
// prepared Content 使用 content_digest 完成 AEAD 认证；其余可信身份、pins 与 Project Fence 来自同一 append-once Context/Receipt。
func (r *WritePromptsRuntimeRepository) decodeWritePromptsToolReceipt(
	ctx context.Context,
	record writePromptsPreviewToolReceiptModel,
	stored turncontext.WritePromptsTurnContext,
	owner string,
	fence int64,
) (writepromptsruntime.ToolReceiptSnapshot, error) {
	snapshot := writepromptsruntime.ToolReceiptSnapshot{
		Stage: writepromptsruntime.ToolReceiptStage(record.Status), RequestDigest: record.RequestDigest,
	}
	if record.Status != string(writepromptsruntime.ToolReceiptOpen) {
		if record.Status != string(writepromptsruntime.ToolReceiptBusinessPrepared) &&
			record.Status != string(writepromptsruntime.ToolReceiptBusinessUnknown) &&
			record.Status != string(writepromptsruntime.ToolReceiptCompleted) &&
			record.Status != string(writepromptsruntime.ToolReceiptFailed) {
			return writepromptsruntime.ToolReceiptSnapshot{}, writepromptsruntime.ErrReceiptConflict
		}
	}
	if record.CommandDigest != nil || record.CommandKeyVersion != nil || record.ContentDigest != nil ||
		record.BusinessRequestDigest != nil || record.ExpectedProjectVersion != nil || len(record.CommandCiphertext) != 0 {
		if record.CommandDigest == nil || record.CommandKeyVersion == nil || record.ContentDigest == nil ||
			record.BusinessRequestDigest == nil || record.ExpectedProjectVersion == nil ||
			!validWritePromptsDigest(*record.CommandDigest) || !validWritePromptsDigest(*record.ContentDigest) ||
			!validWritePromptsDigest(*record.BusinessRequestDigest) || *record.ExpectedProjectVersion < 1 ||
			len(record.CommandCiphertext) == 0 {
			return writepromptsruntime.ToolReceiptSnapshot{}, writepromptsruntime.ErrReceiptConflict
		}
		plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
			Ciphertext: append([]byte(nil), record.CommandCiphertext...), KeyVersion: *record.CommandKeyVersion,
		}, *record.CommandDigest)
		if err != nil {
			return writepromptsruntime.ToolReceiptSnapshot{}, writepromptsruntime.ErrReceiptConflict
		}
		command, err := decodeWritePromptsDurableCommand(plaintext)
		if err != nil {
			return writepromptsruntime.ToolReceiptSnapshot{}, writepromptsruntime.ErrReceiptConflict
		}
		trusted := writeprompts.TrustedContext{
			Owner: owner, RequestID: stored.RequestID, UserID: stored.UserID, ProjectID: stored.ProjectID,
			SessionID: stored.SessionID, InputID: stored.InputID, TurnID: stored.TurnID, RunID: stored.RunID,
			ToolCallID: stored.ToolCallID, BusinessCommandID: stored.BusinessCommandID, FenceToken: fence,
			StoryboardPreviewRef: writeprompts.StoryboardPreviewRef{
				ID: stored.StoryboardPreviewID, Version: stored.StoryboardPreviewVersion,
				ContentDigest: stored.StoryboardPreviewContentDigest,
			},
			PromptVersion: writeprompts.PromptVersion, ValidatorVersion: writeprompts.ValidatorVersion,
			ExactSetValidatorVersion: writeprompts.ExactSetValidatorVersion, Policy: writepromptsruntime.ApprovedPolicy(),
		}
		command.TrustedContext = trusted
		if command.DomainContext.ProjectID != stored.ProjectID || command.DomainContext.ProjectVersion != *record.ExpectedProjectVersion ||
			command.DomainContext.Storyboard.ID != stored.StoryboardPreviewID ||
			command.DomainContext.Storyboard.Version != stored.StoryboardPreviewVersion ||
			command.DomainContext.Storyboard.ContentDigest != stored.StoryboardPreviewContentDigest ||
			command.RequestDigest != *record.BusinessRequestDigest {
			return writepromptsruntime.ToolReceiptSnapshot{}, writepromptsruntime.ErrReceiptConflict
		}
		computedCommandDigest, err := digestWritePromptsPreparedCommand(command)
		if err != nil || computedCommandDigest != *record.CommandDigest {
			return writepromptsruntime.ToolReceiptSnapshot{}, writepromptsruntime.ErrReceiptConflict
		}
		computedBusinessDigest, err := writeprompts.SaveRequestDigest(command)
		if err != nil || computedBusinessDigest != *record.BusinessRequestDigest {
			return writepromptsruntime.ToolReceiptSnapshot{}, writepromptsruntime.ErrReceiptConflict
		}
		snapshot.PreparedCommand = &command
		snapshot.PreparedCommandDigest = *record.CommandDigest
		snapshot.ContentDigest = *record.ContentDigest
		if record.Status == string(writepromptsruntime.ToolReceiptBusinessUnknown) {
			recovery := writeprompts.RecoveryDeferred{
				ToolCallID: stored.ToolCallID, BusinessCommandID: stored.BusinessCommandID,
				RequestDigest: *record.BusinessRequestDigest, ContentDigest: *record.ContentDigest,
				Command: command, ResendAttempts: record.ResendAttempts, ResendLimit: record.ResendLimit,
				ResendExhausted: record.ResendAttempts >= record.ResendLimit,
			}
			snapshot.Recovery = &recovery
		}
	}
	if record.Status == string(writepromptsruntime.ToolReceiptCompleted) || record.Status == string(writepromptsruntime.ToolReceiptFailed) {
		if record.ResultKeyVersion == nil || record.ResultDigest == nil || record.ResultCode == nil ||
			!validWritePromptsDigest(*record.ResultDigest) || len(record.ResultCiphertext) == 0 {
			return writepromptsruntime.ToolReceiptSnapshot{}, writepromptsruntime.ErrReceiptConflict
		}
		plaintext, err := r.protector.Open(ctx, session.ProtectedContent{
			Ciphertext: append([]byte(nil), record.ResultCiphertext...), KeyVersion: *record.ResultKeyVersion,
		}, *record.ResultDigest)
		if err != nil {
			return writepromptsruntime.ToolReceiptSnapshot{}, writepromptsruntime.ErrReceiptConflict
		}
		var result writeprompts.Result
		if err := strictWritePromptsJSON(plaintext, &result); err != nil ||
			result.Status != record.Status || result.ResultCode != *record.ResultCode ||
			result.InvocationRef.ToolCallID != stored.ToolCallID ||
			result.InvocationRef.BusinessCommandID != stored.BusinessCommandID {
			return writepromptsruntime.ToolReceiptSnapshot{}, writepromptsruntime.ErrReceiptConflict
		}
		snapshot.ResultJSON = append([]byte(nil), plaintext...)
		snapshot.ResultDigest = *record.ResultDigest
	}
	return snapshot, nil
}

// loadWritePromptsTurnContext 以稳定 Turn ID 读取同一 prepared Receipt 的 append-once Context。
func loadWritePromptsTurnContext(tx *gorm.DB, turnID string) (turncontext.WritePromptsTurnContext, error) {
	var record writePromptsPreviewContextModel
	if err := tx.Where("turn_id = ?", turnID).Take(&record).Error; err != nil {
		return turncontext.WritePromptsTurnContext{}, err
	}
	return mapWritePromptsContextValue(record), nil
}

// withActiveWritePromptsFence 使用一次 PostgreSQL clock 校验 Session/Input/Run 三层相同 owner/fence。
// callback 与 Fence 校验处于同一事务，防止校验后被 takeover 再写回执或终态。
func (r *WritePromptsRuntimeRepository) withActiveWritePromptsFence(
	ctx context.Context,
	claim writepromptsruntime.Claim,
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
			JOIN agent.write_prompts_preview_run AS run_record ON run_record.input_id = input_record.id
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
			return writepromptsruntime.ErrFenceLost
		}
		return callback(tx, databaseNow)
	})
	return mapWritePromptsRuntimeError(err)
}

// releaseWritePromptsLane 只允许当前 owner/fence 清空 Session Lease，并单调推进 Lease Version。
func releaseWritePromptsLane(tx *gorm.DB, claim writepromptsruntime.Claim, databaseNow time.Time) error {
	leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
		Where("session_id = ? AND lease_owner = ? AND fence_token = ?", claim.Context.SessionID, claim.Owner, claim.FenceToken).
		Updates(map[string]any{
			"lease_owner": nil, "lease_until": nil, "version": gorm.Expr("version + 1"), "updated_at": databaseNow,
		})
	if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
		return writepromptsruntime.ErrFenceLost
	}
	return nil
}

// appendWritePromptsEvent 锁定 Session Event Counter 后以单调序号 append-once 写安全事件。
func appendWritePromptsEvent(tx *gorm.DB, databaseNow time.Time, record sessionEventLogModel) error {
	var counter sessionEventCounterModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", record.SessionID).Take(&counter).Error; err != nil {
		return err
	}
	sequence := counter.LastSeq + 1
	if sequence < 1 {
		return writepromptsruntime.ErrPersistence
	}
	record.Seq, record.CreatedAt = sequence, databaseNow
	if err := tx.Create(&record).Error; err != nil {
		return err
	}
	update := tx.Model(&sessionEventCounterModel{}).
		Where("session_id = ? AND last_seq = ?", record.SessionID, counter.LastSeq).
		Updates(map[string]any{"last_seq": sequence, "updated_at": databaseNow})
	if update.Error != nil || update.RowsAffected != 1 {
		return writepromptsruntime.ErrPersistence
	}
	return nil
}

// mapWritePromptsEnqueueResult 返回首次冻结的稳定身份，重放不混入当前 HTTP RequestID。
func mapWritePromptsEnqueueResult(record writePromptsPreviewRunModel, replayed bool) writepromptsruntime.EnqueueResult {
	return writepromptsruntime.EnqueueResult{
		InputID: record.InputID, TurnID: record.TurnID, RunID: record.RunID, ToolCallID: record.ToolCallID,
		BusinessCommandID: record.BusinessCommandID, RouterModelCallID: record.RouterModelCallID,
		GraphModelCallID: record.GraphModelCallID, AcceptedEventID: record.AcceptedEventID,
		TerminalEventID: record.TerminalEventID, Replayed: replayed,
	}
}

// mapWritePromptsContextModel 显式映射不可变 Context 与受保护 Intent，避免反射复制造成字段遗漏。
func mapWritePromptsContextModel(
	value turncontext.WritePromptsTurnContext,
	ciphertext []byte,
	createdAt time.Time,
) writePromptsPreviewContextModel {
	return writePromptsPreviewContextModel{
		TurnID: value.TurnID, Profile: value.Profile, SchemaVersion: value.SchemaVersion, RequestID: value.RequestID,
		SessionID: value.SessionID, InputID: value.InputID, RunID: value.RunID, ToolCallID: value.ToolCallID,
		BusinessCommandID: value.BusinessCommandID, RouterModelCallID: value.RouterModelCallID,
		GraphModelCallID: value.GraphModelCallID, UserID: value.UserID, ProjectID: value.ProjectID,
		IntentCiphertext: append([]byte(nil), ciphertext...), IntentKeyVersion: value.IntentKeyVersion,
		IntentDigest: value.IntentDigest, StoryboardPreviewID: value.StoryboardPreviewID,
		StoryboardPreviewVersion: value.StoryboardPreviewVersion, StoryboardPreviewContentDigest: value.StoryboardPreviewContentDigest,
		AccessScopeRef: value.AccessScopeRef, AccessScopeDigest: value.AccessScopeDigest,
		ToolRegistryRef: value.ToolRegistryRef, ToolRegistryDigest: value.ToolRegistryDigest,
		ToolDefinitionRef: value.ToolDefinitionRef, ToolDefinitionDigest: value.ToolDefinitionDigest,
		IntentSchemaRef: value.IntentSchemaRef, CandidateSchemaRef: value.CandidateSchemaRef,
		ResultSchemaRef: value.ResultSchemaRef, PromptRef: value.PromptRef, PromptDigest: value.PromptDigest,
		ValidatorRef: value.ValidatorRef, ValidatorDigest: value.ValidatorDigest,
		ExactSetValidatorRef: value.ExactSetValidatorRef, ExactSetValidatorDigest: value.ExactSetValidatorDigest,
		RouterModelRouteRef: value.RouterModelRouteRef, RouterModelRouteDigest: value.RouterModelRouteDigest,
		PromptModelRouteRef: value.PromptModelRouteRef, PromptModelRouteDigest: value.PromptModelRouteDigest,
		RuntimePolicyRef: value.RuntimePolicyRef, RuntimePolicyDigest: value.RuntimePolicyDigest,
		BudgetRef: value.BudgetRef, BudgetDigest: value.BudgetDigest,
		ContextDigest: value.ContextDigest, CreatedAt: createdAt,
	}
}

// mapWritePromptsContextValue 显式还原不可变 Context；Intent 密文只由 Repository 单独认证解密。
func mapWritePromptsContextValue(record writePromptsPreviewContextModel) turncontext.WritePromptsTurnContext {
	return turncontext.WritePromptsTurnContext{
		SchemaVersion: record.SchemaVersion, Profile: record.Profile, RequestID: record.RequestID,
		SessionID: record.SessionID, InputID: record.InputID, TurnID: record.TurnID, RunID: record.RunID,
		ToolCallID: record.ToolCallID, BusinessCommandID: record.BusinessCommandID,
		RouterModelCallID: record.RouterModelCallID, GraphModelCallID: record.GraphModelCallID,
		UserID: record.UserID, ProjectID: record.ProjectID, IntentKeyVersion: record.IntentKeyVersion,
		IntentDigest: record.IntentDigest, StoryboardPreviewID: record.StoryboardPreviewID,
		StoryboardPreviewVersion: record.StoryboardPreviewVersion, StoryboardPreviewContentDigest: record.StoryboardPreviewContentDigest,
		AccessScopeRef: record.AccessScopeRef, AccessScopeDigest: record.AccessScopeDigest,
		ToolRegistryRef: record.ToolRegistryRef, ToolRegistryDigest: record.ToolRegistryDigest,
		ToolDefinitionRef: record.ToolDefinitionRef, ToolDefinitionDigest: record.ToolDefinitionDigest,
		IntentSchemaRef: record.IntentSchemaRef, CandidateSchemaRef: record.CandidateSchemaRef,
		ResultSchemaRef: record.ResultSchemaRef, PromptRef: record.PromptRef, PromptDigest: record.PromptDigest,
		ValidatorRef: record.ValidatorRef, ValidatorDigest: record.ValidatorDigest,
		ExactSetValidatorRef: record.ExactSetValidatorRef, ExactSetValidatorDigest: record.ExactSetValidatorDigest,
		RouterModelRouteRef: record.RouterModelRouteRef, RouterModelRouteDigest: record.RouterModelRouteDigest,
		PromptModelRouteRef: record.PromptModelRouteRef, PromptModelRouteDigest: record.PromptModelRouteDigest,
		RuntimePolicyRef: record.RuntimePolicyRef, RuntimePolicyDigest: record.RuntimePolicyDigest,
		BudgetRef: record.BudgetRef, BudgetDigest: record.BudgetDigest, ContextDigest: record.ContextDigest,
	}
}

// mapWritePromptsClaim 只映射数据库冻结事实和本次合法 owner，不读取最新配置。
func mapWritePromptsClaim(row writePromptsClaimRow, owner string) writepromptsruntime.Claim {
	return writepromptsruntime.Claim{
		Owner: owner, FenceToken: row.LeaseFence, Attempts: row.Attempts, EnqueueSeq: row.EnqueueSeq,
		TerminalEventID: row.TerminalEventID, Context: mapWritePromptsContextValue(row.Context),
	}
}

// assertWritePromptsToolRecord 拒绝用其他 Turn、Run、Input 或 Business Command 的回执替换当前 Claim。
func assertWritePromptsToolRecord(
	record writePromptsPreviewToolReceiptModel,
	ctx turncontext.WritePromptsTurnContext,
	requestDigest string,
) error {
	if record.ToolCallID != ctx.ToolCallID || record.RunID != ctx.RunID || record.TurnID != ctx.TurnID ||
		record.InputID != ctx.InputID || record.BusinessCommandID != ctx.BusinessCommandID || record.RequestDigest != requestDigest {
		return writepromptsruntime.ErrReceiptConflict
	}
	return nil
}

// writePromptsClaimFromModelIdentity 构造仅用于基础设施 Fence 校验的最小 Claim。
func writePromptsClaimFromModelIdentity(identity writepromptsruntime.ModelReceiptIdentity) writepromptsruntime.Claim {
	return writepromptsruntime.Claim{
		Owner: identity.Owner, FenceToken: identity.FenceToken,
		Context: turncontext.WritePromptsTurnContext{
			SessionID: identity.SessionID, InputID: identity.InputID, TurnID: identity.TurnID, RunID: identity.RunID,
		},
	}
}

// writePromptsClaimFromToolIdentity 构造仅用于基础设施 Fence 校验的最小 Claim。
func writePromptsClaimFromToolIdentity(identity writepromptsruntime.ToolReceiptIdentity) writepromptsruntime.Claim {
	return writepromptsruntime.Claim{
		Owner: identity.Owner, FenceToken: identity.FenceToken,
		Context: turncontext.WritePromptsTurnContext{
			SessionID: identity.SessionID, InputID: identity.InputID, TurnID: identity.TurnID, RunID: identity.RunID,
			ToolCallID: identity.ToolCallID, BusinessCommandID: identity.BusinessCommandID,
		},
	}
}

// assertWritePromptsModelIdentity 拒绝用 Router ID 写 Graph 命名空间或反向替换稳定调用 ID。
func assertWritePromptsModelIdentity(tx *gorm.DB, identity writepromptsruntime.ModelReceiptIdentity) error {
	column := "router_model_call_id"
	if identity.CallKind == writepromptsruntime.ModelCallGraphPrompt {
		column = "graph_model_call_id"
	}
	var count int64
	if err := tx.Model(&writePromptsPreviewRunModel{}).
		Where("run_id = ? AND input_id = ? AND turn_id = ? AND "+column+" = ?",
			identity.RunID, identity.InputID, identity.TurnID, identity.ModelCallID).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return writepromptsruntime.ErrReceiptConflict
	}
	return nil
}

// validWritePromptsModelKind 只接受批准的两层本地 Fake Model 命名空间。
func validWritePromptsModelKind(kind writepromptsruntime.ModelCallKind) bool {
	return kind == writepromptsruntime.ModelCallRouter || kind == writepromptsruntime.ModelCallGraphPrompt
}

// digestWritePromptsEnqueue 把 PromptPreview 精确引用加入入队语义，避免同幂等键偷偷替换上游 Draft。
func digestWritePromptsEnqueue(ref writeprompts.StoryboardPreviewRef, intentDigest string) string {
	wire := struct {
		SchemaVersion     string                            `json:"schema_version"`
		StoryboardPreview writeprompts.StoryboardPreviewRef `json:"storyboard_preview_ref"`
		IntentDigest      string                            `json:"intent_digest"`
	}{"write_prompts.preview.enqueue.digest.v1", ref, intentDigest}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return ""
	}
	return digestWritePromptsBytes(encoded)
}

// writePromptsToolRequestDigest 与 Tool Wrapper 的具名顺序保持一致，防止同键异义执行。
func writePromptsToolRequestDigest(value turncontext.WritePromptsTurnContext) string {
	wire := value.ContextDigest + "\n" + value.ToolDefinitionRef + "\n" + value.ToolDefinitionDigest + "\n" +
		value.IntentSchemaRef + "\n" + value.CandidateSchemaRef + "\n" + value.ResultSchemaRef + "\n" +
		value.IntentDigest + "\n" + value.StoryboardPreviewID + "\n" + fmt.Sprint(value.StoryboardPreviewVersion) + "\n" +
		value.StoryboardPreviewContentDigest
	return digestWritePromptsBytes([]byte(wire))
}

// digestWritePromptsPreparedCommand 复算 Runtime 冻结的 prepared 语义摘要，不包含 takeover 时变化的 Owner/Fence。
func digestWritePromptsPreparedCommand(command writeprompts.DraftCommand) (string, error) {
	encoded, err := writepromptsruntime.EncodeDurableDraftCommand(command)
	if err != nil {
		return "", err
	}
	return digestWritePromptsBytes(encoded), nil
}

// digestWritePromptsRecoveryCommand 返回 Recovery command 的稳定语义摘要；错误时返回空串并触发调用方冲突。
func digestWritePromptsRecoveryCommand(command writeprompts.DraftCommand) string {
	digest, err := digestWritePromptsPreparedCommand(command)
	if err != nil {
		return ""
	}
	return digest
}

// encodeWritePromptsDurableCommand 编码完整 Source、target、scope 与 Content，避免恢复时重新调用模型或 Business Query。
func encodeWritePromptsDurableCommand(command writeprompts.DraftCommand) ([]byte, error) {
	return writepromptsruntime.EncodeDurableDraftCommand(command)
}

// decodeWritePromptsDurableCommand 严格解码 canonical 恢复工件；Owner/Fence 仍保持空值等待可信 Context 回填。
func decodeWritePromptsDurableCommand(encoded []byte) (writeprompts.DraftCommand, error) {
	return writepromptsruntime.DecodeDurableDraftCommand(encoded)
}

// strictWritePromptsJSON 拒绝未知字段和尾随 JSON，防止冻结后宽松解释。
func strictWritePromptsJSON(encoded []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return writepromptsruntime.ErrOutputContract
	}
	return nil
}

// digestWritePromptsBytes 返回 canonical 内容的 SHA-256 小写十六进制摘要。
func digestWritePromptsBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// validWritePromptsDigest 校验固定长度的小写 SHA-256 十六进制编码。
func validWritePromptsDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

// canonicalWritePromptsUUIDv7 拒绝非规范表示和非 UUIDv7 稳定标识。
func canonicalWritePromptsUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

// mapWritePromptsRuntimeError 保留稳定业务错误，其余数据库细节统一折叠为 Persistence。
func mapWritePromptsRuntimeError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.Is(err, writepromptsruntime.ErrFenceLost), errors.Is(err, writepromptsruntime.ErrInvalidInput),
		errors.Is(err, writepromptsruntime.ErrNotFound), errors.Is(err, writepromptsruntime.ErrSessionLaneBlocked),
		errors.Is(err, writepromptsruntime.ErrInvalidClaim), errors.Is(err, writepromptsruntime.ErrReceiptConflict),
		errors.Is(err, writepromptsruntime.ErrOutputContract), errors.Is(err, writepromptsruntime.ErrIdempotencyConflict),
		errors.Is(err, writepromptsruntime.ErrRecoveryDeferred):
		return err
	default:
		return writepromptsruntime.ErrPersistence
	}
}

var _ writepromptsruntime.ExecutionStore = (*WritePromptsRuntimeRepository)(nil)
var _ writepromptsruntime.ModelReceiptStore = (*WritePromptsRuntimeRepository)(nil)
var _ writepromptsruntime.ToolReceiptStore = (*WritePromptsRuntimeRepository)(nil)
