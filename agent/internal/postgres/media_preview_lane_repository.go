package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreviewruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Enqueue 原子写媒体 Request、无 Message Session Input 与稳定执行身份；同义重放不生成新 ID。
func (r *MediaRuntimeRepository) Enqueue(ctx context.Context, command mediapreviewruntime.EnqueueCommand) (mediapreviewruntime.EnqueueResult, error) {
	canonical, schemaVersion, err := validateMediaEnqueueCommand(command)
	if err != nil {
		return mediapreviewruntime.EnqueueResult{}, err
	}
	if existing, lookupErr := r.lookupMediaRequest(ctx, command); lookupErr != nil || existing != nil {
		if lookupErr != nil {
			return mediapreviewruntime.EnqueueResult{}, lookupErr
		}
		return mapMediaEnqueueResult(*existing, true), nil
	}
	var result mediapreviewruntime.EnqueueResult
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "media-preview:"+command.SessionID+":"+command.IdempotencyKey).Error; err != nil {
			return err
		}
		var existing mediaPreviewRequestModel
		lookupErr := tx.Where("session_id = ? AND idempotency_key = ?", command.SessionID, command.IdempotencyKey).Take(&existing).Error
		switch {
		case lookupErr == nil:
			if !sameMediaRequest(existing, command) {
				return mediapreviewruntime.ErrIdempotencyConflict
			}
			result = mapMediaEnqueueResult(existing, true)
			return nil
		case !errors.Is(lookupErr, gorm.ErrRecordNotFound):
			return lookupErr
		}
		var target sessionModel
		if err := tx.Where("id = ? AND user_id = ? AND project_id = ? AND status = ? AND archived_at IS NULL",
			command.SessionID, command.UserID, command.ProjectID, string(session.StatusActive)).Take(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return mediapreviewruntime.ErrNotFound
			}
			return err
		}
		now, err := mediaDatabaseNow(tx)
		if err != nil {
			return err
		}
		if !command.DeadlineAt.After(now) || command.DeadlineAt.Sub(now) > mediapreviewruntime.DefaultRequestTTL+time.Minute {
			return mediapreviewruntime.ErrInvalidInput
		}
		identities := make([]string, 6)
		for index := range identities {
			value, err := r.ids.New()
			if err != nil || !mediapreview.ValidUUIDv7(value) {
				return mediapreviewruntime.ErrPersistence
			}
			identities[index] = value
		}
		var sequence sessionSequenceCounterModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", command.SessionID).Take(&sequence).Error; err != nil {
			return err
		}
		enqueueSeq := sequence.LastInputEnqueueSeq + 1
		if enqueueSeq < 1 {
			return mediapreviewruntime.ErrPersistence
		}
		counter := tx.Model(&sessionSequenceCounterModel{}).
			Where("session_id = ? AND last_input_enqueue_seq = ?", command.SessionID, sequence.LastInputEnqueueSeq).
			Updates(map[string]any{"last_input_enqueue_seq": enqueueSeq, "updated_at": now})
		if counter.Error != nil || counter.RowsAffected != 1 {
			return mediapreviewruntime.ErrPersistence
		}
		sourceType := mediaSourceType(command.ToolKey)
		input := sessionInputModel{ID: identities[0], SessionID: command.SessionID, SourceType: sourceType,
			SourceID: command.RequestID, MessageID: nil, Status: "pending", EnqueueSeq: enqueueSeq,
			Attempts: 0, AvailableAt: now, FenceToken: 0, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&input).Error; err != nil {
			return err
		}
		request := mediaPreviewRequestModel{
			RequestID: command.RequestID, SessionID: command.SessionID, UserID: command.UserID,
			ProjectID: command.ProjectID, IdempotencyKey: command.IdempotencyKey,
			RequestDigest: command.RequestDigest, ToolKey: command.ToolKey,
			IntentSchemaVersion: schemaVersion, IntentDigest: command.IntentDigest, Intent: string(canonical),
			InputID: identities[0], TurnID: identities[1], RunID: identities[2], ToolCallID: identities[3],
			AcceptedEventID: identities[4], TerminalEventID: identities[5],
			DeadlineAt: command.DeadlineAt.UTC(), CreatedAt: now,
		}
		if err := tx.Create(&request).Error; err != nil {
			return err
		}
		result = mapMediaEnqueueResult(request, false)
		return nil
	})
	if err != nil {
		return mediapreviewruntime.EnqueueResult{}, mapMediaLaneError(err)
	}
	return result, nil
}

func validateMediaEnqueueCommand(command mediapreviewruntime.EnqueueCommand) ([]byte, string, error) {
	for _, value := range []string{command.RequestID, command.SessionID, command.UserID, command.ProjectID, command.IdempotencyKey} {
		if !mediapreview.ValidUUIDv7(value) {
			return nil, "", mediapreviewruntime.ErrInvalidInput
		}
	}
	if !mediapreview.ValidDigest(command.IntentDigest) || !mediapreview.ValidDigest(command.RequestDigest) || command.DeadlineAt.IsZero() {
		return nil, "", mediapreviewruntime.ErrInvalidInput
	}
	canonical, schemaVersion, err := canonicalMediaIntent(command.ToolKey, command.IntentJSON)
	if err != nil || !bytes.Equal(canonical, command.IntentJSON) || schemaVersion != command.IntentSchemaVersion ||
		mediaDigest(canonical) != command.IntentDigest {
		return nil, "", mediapreviewruntime.ErrInvalidInput
	}
	return canonical, schemaVersion, nil
}

// canonicalMediaIntent 恢复 JSONB 读取时丢失的规范化字节表示；语义校验仍由两类 Intent decoder 负责。
func canonicalMediaIntent(toolKey string, raw []byte) ([]byte, string, error) {
	var canonical []byte
	var err error
	schemaVersion := ""
	switch toolKey {
	case mediapreview.GenerateMediaToolKey:
		var value mediapreview.GenerateMediaIntent
		value, err = mediapreview.DecodeGenerateMediaIntent(raw)
		if err == nil {
			canonical, err = mediapreview.CanonicalJSON(value)
		}
		schemaVersion = mediapreview.GenerateMediaIntentVersion
	case mediapreview.AssembleOutputToolKey:
		var value mediapreview.AssembleOutputIntent
		value, err = mediapreview.DecodeAssembleOutputIntent(raw)
		if err == nil {
			canonical, err = mediapreview.CanonicalJSON(value)
		}
		schemaVersion = mediapreview.AssembleOutputIntentVersion
	default:
		return nil, "", mediapreviewruntime.ErrInvalidInput
	}
	if err != nil {
		return nil, "", err
	}
	return canonical, schemaVersion, nil
}

func (r *MediaRuntimeRepository) lookupMediaRequest(ctx context.Context, command mediapreviewruntime.EnqueueCommand) (*mediaPreviewRequestModel, error) {
	var existing mediaPreviewRequestModel
	err := r.db.WithContext(ctx).Where("session_id = ? AND idempotency_key = ?", command.SessionID, command.IdempotencyKey).Take(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	case err != nil:
		return nil, mapMediaLaneError(err)
	case !sameMediaRequest(existing, command):
		return nil, mediapreviewruntime.ErrIdempotencyConflict
	default:
		return &existing, nil
	}
}

func sameMediaRequest(record mediaPreviewRequestModel, command mediapreviewruntime.EnqueueCommand) bool {
	return record.RequestDigest == command.RequestDigest && record.ToolKey == command.ToolKey &&
		record.IntentDigest == command.IntentDigest && record.UserID == command.UserID && record.ProjectID == command.ProjectID &&
		record.SessionID == command.SessionID
}

func mapMediaEnqueueResult(record mediaPreviewRequestModel, replayed bool) mediapreviewruntime.EnqueueResult {
	return mediapreviewruntime.EnqueueResult{InputID: record.InputID, TurnID: record.TurnID, RunID: record.RunID,
		ToolCallID: record.ToolCallID, ToolKey: record.ToolKey, Replayed: replayed}
}

type mediaLaneClaimRow struct {
	RequestID       string    `gorm:"column:request_id"`
	SessionID       string    `gorm:"column:session_id"`
	UserID          string    `gorm:"column:user_id"`
	ProjectID       string    `gorm:"column:project_id"`
	IdempotencyKey  string    `gorm:"column:idempotency_key"`
	ToolKey         string    `gorm:"column:tool_key"`
	IntentDigest    string    `gorm:"column:intent_digest"`
	Intent          string    `gorm:"column:intent"`
	InputID         string    `gorm:"column:input_id"`
	TurnID          string    `gorm:"column:turn_id"`
	RunID           string    `gorm:"column:run_id"`
	ToolCallID      string    `gorm:"column:tool_call_id"`
	AcceptedEventID string    `gorm:"column:accepted_event_id"`
	TerminalEventID string    `gorm:"column:terminal_event_id"`
	DeadlineAt      time.Time `gorm:"column:deadline_at"`
	Attempts        int       `gorm:"column:attempts"`
	LeaseFence      int64     `gorm:"column:lease_fence"`
	LeaseVersion    int64     `gorm:"column:lease_version"`
	DatabaseNow     time.Time `gorm:"column:database_now"`
}

// ClaimNext 先计算每个 Session 的全来源真正 HOL，再只领取指定媒体 source。
func (r *MediaRuntimeRepository) ClaimNext(ctx context.Context, sourceType, owner string, leaseDuration time.Duration) (*mediapreviewruntime.Claim, error) {
	if (sourceType != mediapreviewruntime.GenerateSourceType && sourceType != mediapreviewruntime.AssembleSourceType) || owner == "" || leaseDuration <= 0 {
		return nil, mediapreviewruntime.ErrInvalidInput
	}
	var row mediaLaneClaimRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Raw(`
			WITH database_clock AS MATERIALIZED (SELECT clock_timestamp() AS database_now),
			head_ids AS MATERIALIZED (
				SELECT DISTINCT ON (candidate.session_id) candidate.session_id, candidate.id
				FROM agent.session_input AS candidate
				WHERE candidate.status IN ('pending','claimed','running','retry_wait','recovery_pending')
				ORDER BY candidate.session_id, candidate.enqueue_seq, candidate.id
			)
			SELECT request_record.request_id, request_record.session_id, request_record.user_id,
			       request_record.project_id, request_record.idempotency_key, request_record.tool_key,
			       request_record.intent_digest, request_record.intent, request_record.input_id,
			       request_record.turn_id, request_record.run_id, request_record.tool_call_id,
			       request_record.accepted_event_id, request_record.terminal_event_id,
			       request_record.deadline_at, input_record.attempts, lease.fence_token AS lease_fence,
			       lease.version AS lease_version, database_clock.database_now
			FROM head_ids CROSS JOIN database_clock
			JOIN agent.session_input AS input_record ON input_record.id = head_ids.id
			JOIN agent.session AS session_record ON session_record.id = input_record.session_id
			JOIN agent.session_runtime_lease AS lease ON lease.session_id = input_record.session_id
			JOIN agent.media_preview_request AS request_record ON request_record.input_id = input_record.id
			WHERE input_record.source_type = ? AND input_record.available_at <= database_clock.database_now
			  AND session_record.status = 'active' AND session_record.archived_at IS NULL
			  AND (lease.lease_owner IS NULL OR lease.lease_until <= database_clock.database_now)
			  AND (input_record.lease_owner IS NULL OR input_record.lease_until <= database_clock.database_now)
			ORDER BY input_record.available_at, input_record.session_id, input_record.enqueue_seq
			FOR UPDATE OF input_record, lease SKIP LOCKED LIMIT 1`, sourceType).Scan(&row)
		if query.Error != nil || query.RowsAffected == 0 {
			return query.Error
		}
		canonicalIntent, _, canonicalErr := canonicalMediaIntent(row.ToolKey, []byte(row.Intent))
		if canonicalErr != nil || mediaDigest(canonicalIntent) != row.IntentDigest {
			return mediapreviewruntime.ErrOutputContract
		}
		row.Intent = string(canonicalIntent)
		newFence := row.LeaseFence + 1
		leaseUntil := row.DatabaseNow.Add(leaseDuration)
		leaseUpdate := tx.Model(&sessionRuntimeLeaseModel{}).
			Where("session_id = ? AND version = ? AND fence_token = ? AND (lease_owner IS NULL OR lease_until <= ?)",
				row.SessionID, row.LeaseVersion, row.LeaseFence, row.DatabaseNow).
			Updates(map[string]any{"lease_owner": owner, "lease_until": leaseUntil, "fence_token": newFence,
				"version": gorm.Expr("version + 1"), "updated_at": row.DatabaseNow})
		if leaseUpdate.Error != nil || leaseUpdate.RowsAffected != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		inputUpdate := tx.Model(&sessionInputModel{}).
			Where("id = ? AND status IN ?", row.InputID, []string{"pending", "claimed", "running", "retry_wait", "recovery_pending"}).
			Updates(map[string]any{"status": "claimed", "attempts": row.Attempts + 1, "lease_owner": owner,
				"lease_until": leaseUntil, "fence_token": newFence, "updated_at": row.DatabaseNow})
		if inputUpdate.Error != nil || inputUpdate.RowsAffected != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		row.LeaseFence, row.Attempts = newFence, row.Attempts+1
		return nil
	})
	if err != nil {
		return nil, mapMediaLaneError(err)
	}
	if row.InputID == "" {
		return nil, nil
	}
	return &mediapreviewruntime.Claim{Owner: owner, RequestID: row.RequestID, IdempotencyKey: row.IdempotencyKey,
		UserID: row.UserID, ProjectID: row.ProjectID, SessionID: row.SessionID, InputID: row.InputID,
		TurnID: row.TurnID, RunID: row.RunID, ToolCallID: row.ToolCallID, AcceptedEventID: row.AcceptedEventID,
		TerminalEventID: row.TerminalEventID, ToolKey: row.ToolKey, IntentDigest: row.IntentDigest,
		IntentJSON: []byte(row.Intent), FenceToken: row.LeaseFence, Attempts: row.Attempts, DeadlineAt: row.DeadlineAt.UTC()}, nil
}

// MarkRunning 推进当前媒体 Input；Request 不复制可变运行状态。
func (r *MediaRuntimeRepository) MarkRunning(ctx context.Context, claim mediapreviewruntime.Claim) error {
	return r.withActiveMediaFence(ctx, claim.SessionID, claim.InputID, claim.Owner, claim.FenceToken, func(tx *gorm.DB, now time.Time) error {
		update := tx.Model(&sessionInputModel{}).Where("id = ? AND status = 'claimed' AND lease_owner = ? AND fence_token = ?", claim.InputID, claim.Owner, claim.FenceToken).
			Updates(map[string]any{"status": "running", "updated_at": now})
		if update.Error != nil || update.RowsAffected != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		return nil
	})
}

// RenewLease 同事务延长 Session/Input 相同 owner/fence。
func (r *MediaRuntimeRepository) RenewLease(ctx context.Context, claim mediapreviewruntime.Claim, leaseDuration time.Duration) error {
	if leaseDuration <= 0 {
		return mediapreviewruntime.ErrInvalidInput
	}
	return r.withActiveMediaFence(ctx, claim.SessionID, claim.InputID, claim.Owner, claim.FenceToken, func(tx *gorm.DB, now time.Time) error {
		until := now.Add(leaseDuration)
		input := tx.Model(&sessionInputModel{}).Where("id = ? AND status IN ? AND lease_owner = ? AND fence_token = ?", claim.InputID,
			[]string{"claimed", "running"}, claim.Owner, claim.FenceToken).Updates(map[string]any{"lease_until": until, "updated_at": now})
		if input.Error != nil || input.RowsAffected != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		lease := tx.Model(&sessionRuntimeLeaseModel{}).Where("session_id = ? AND lease_owner = ? AND fence_token = ?", claim.SessionID, claim.Owner, claim.FenceToken).
			Updates(map[string]any{"lease_until": until, "version": gorm.Expr("version + 1"), "updated_at": now})
		if lease.Error != nil || lease.RowsAffected != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		return nil
	})
}

// CompleteGraphResult AppendOnce 投影 accepted/early-failed Card 并释放原请求 Lane。
func (r *MediaRuntimeRepository) CompleteGraphResult(ctx context.Context, claim mediapreviewruntime.Claim, result mediapreview.GraphToolResult) error {
	if mediapreview.ValidateGraphToolResult(result) != nil || result.ToolKey != claim.ToolKey {
		return mediapreviewruntime.ErrOutputContract
	}
	return r.completeMediaRequest(ctx, claim, result, false)
}

// CompleteRuntimeFailure 用独立 runtime_failed early Card 终结无法验证的媒体请求。
func (r *MediaRuntimeRepository) CompleteRuntimeFailure(ctx context.Context, claim mediapreviewruntime.Claim, code string) error {
	if code != "MEDIA_PREVIEW_RUNTIME_FAILED" {
		return mediapreviewruntime.ErrOutputContract
	}
	return r.completeMediaRequest(ctx, claim, mediapreview.GraphToolResult{SchemaVersion: mediapreview.ToolResultSchemaVersion,
		ToolKey: claim.ToolKey, Status: "failed", ResultCode: code, ErrorCode: code, UpdatedAt: time.Now().UTC()}, true)
}

func (r *MediaRuntimeRepository) completeMediaRequest(ctx context.Context, claim mediapreviewruntime.Claim, result mediapreview.GraphToolResult, runtimeFailure bool) error {
	return r.withActiveMediaFence(ctx, claim.SessionID, claim.InputID, claim.Owner, claim.FenceToken, func(tx *gorm.DB, now time.Time) error {
		card := event.MediaPreviewCardPayload{SchemaVersion: event.MediaPreviewCardSchemaVersionV1,
			InputID: claim.InputID, TurnID: claim.TurnID, RunID: claim.RunID, ToolCallID: claim.ToolCallID,
			ToolKey: claim.ToolKey, Status: result.Status, ResultCode: result.ResultCode, UpdatedAt: result.UpdatedAt.UTC()}
		var record event.Record
		var err error
		eventID := claim.TerminalEventID
		inputStatus := "resolved"
		if result.Status == "accepted" {
			eventID = claim.AcceptedEventID
			kind, mime := mediaKindAndMIME(claim.ToolKey)
			card.OperationID, card.BatchID = result.OperationID, result.BatchID
			card.AssetRef = &event.MediaPreviewAssetRef{ID: result.AssetID, Version: 1, Status: "reserved", MediaKind: kind, MIMEType: mime}
			record, err = event.NewMediaPreviewAccepted(eventID, claim.SessionID, claim.RequestID, claim.InputID, card, now)
		} else {
			card.ErrorCode = result.ErrorCode
			if runtimeFailure {
				inputStatus = "dead"
				record, err = event.NewMediaPreviewRuntimeFailed(eventID, claim.SessionID, claim.RequestID, claim.InputID, card, now)
			} else {
				record, err = event.NewMediaPreviewFailed(eventID, claim.SessionID, claim.RequestID, claim.InputID, card, now)
			}
			_ = tx.Model(&mediaPreviewOperationModel{}).Where("tool_call_id = ? AND status IN ?", claim.ToolCallID,
				[]string{"preparing", "recovery_pending"}).Updates(map[string]any{"status": "failed", "failure_code": result.ErrorCode,
				"completed_at": now, "updated_at": now, "version": gorm.Expr("version + 1")}).Error
		}
		if err != nil {
			return mediapreviewruntime.ErrOutputContract
		}
		if err := appendMediaEvent(tx, now, record); err != nil {
			return err
		}
		input := tx.Model(&sessionInputModel{}).Where("id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?", claim.InputID,
			claim.Owner, claim.FenceToken, []string{"claimed", "running"}).Updates(map[string]any{"status": inputStatus,
			"lease_owner": nil, "lease_until": nil, "updated_at": now})
		if input.Error != nil || input.RowsAffected != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		return releaseMediaLane(tx, claim.SessionID, claim.Owner, claim.FenceToken, now)
	})
}

// DeferInputRecovery 保持原请求 HOL 为 recovery_pending，按原 Operation/Prepare/Dispatch key 后续核对。
func (r *MediaRuntimeRepository) DeferInputRecovery(ctx context.Context, claim mediapreviewruntime.Claim, delay time.Duration) error {
	if delay <= 0 {
		return mediapreviewruntime.ErrInvalidInput
	}
	return r.withActiveMediaFence(ctx, claim.SessionID, claim.InputID, claim.Owner, claim.FenceToken, func(tx *gorm.DB, now time.Time) error {
		input := tx.Model(&sessionInputModel{}).Where("id = ? AND lease_owner = ? AND fence_token = ? AND status IN ?", claim.InputID,
			claim.Owner, claim.FenceToken, []string{"claimed", "running"}).Updates(map[string]any{"status": "recovery_pending",
			"available_at": now.Add(delay), "lease_owner": nil, "lease_until": nil, "updated_at": now})
		if input.Error != nil || input.RowsAffected != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		return releaseMediaLane(tx, claim.SessionID, claim.Owner, claim.FenceToken, now)
	})
}

// BridgeNextTerminal 把最早一个 Worker Terminal Outbox AppendOnce 追加成全局 Lane Input。
func (r *MediaRuntimeRepository) BridgeNextTerminal(ctx context.Context) (bool, error) {
	bridged := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var outbox mediaPreviewTerminalOutboxModel
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("delivered_at IS NULL AND lane_input_id IS NULL").Order("session_id, occurred_at, event_id").Limit(1).Take(&outbox)
		if errors.Is(query.Error, gorm.ErrRecordNotFound) {
			return nil
		}
		if query.Error != nil {
			return query.Error
		}
		var existing sessionInputModel
		lookup := tx.Where("session_id = ? AND source_type = ? AND source_id = ?", outbox.SessionID, mediapreviewruntime.TerminalSourceType, outbox.EventID).Take(&existing)
		if lookup.Error == nil {
			now, err := mediaDatabaseNow(tx)
			if err != nil {
				return err
			}
			update := tx.Model(&mediaPreviewTerminalOutboxModel{}).Where("event_id = ? AND delivered_at IS NULL", outbox.EventID).
				Updates(map[string]any{"lane_input_id": existing.ID, "delivered_at": now})
			if update.Error != nil || update.RowsAffected != 1 {
				return mediapreviewruntime.ErrPersistence
			}
			bridged = true
			return nil
		}
		if !errors.Is(lookup.Error, gorm.ErrRecordNotFound) {
			return lookup.Error
		}
		inputID, err := r.ids.New()
		if err != nil || !mediapreview.ValidUUIDv7(inputID) {
			return mediapreviewruntime.ErrPersistence
		}
		now, err := mediaDatabaseNow(tx)
		if err != nil {
			return err
		}
		var sequence sessionSequenceCounterModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", outbox.SessionID).Take(&sequence).Error; err != nil {
			return err
		}
		next := sequence.LastInputEnqueueSeq + 1
		counter := tx.Model(&sessionSequenceCounterModel{}).Where("session_id = ? AND last_input_enqueue_seq = ?", outbox.SessionID, sequence.LastInputEnqueueSeq).
			Updates(map[string]any{"last_input_enqueue_seq": next, "updated_at": now})
		if counter.Error != nil || counter.RowsAffected != 1 {
			return mediapreviewruntime.ErrPersistence
		}
		input := sessionInputModel{ID: inputID, SessionID: outbox.SessionID, SourceType: mediapreviewruntime.TerminalSourceType,
			SourceID: outbox.EventID, Status: "pending", EnqueueSeq: next, AvailableAt: now, CreatedAt: now, UpdatedAt: now}
		if err := tx.Create(&input).Error; err != nil {
			return err
		}
		update := tx.Model(&mediaPreviewTerminalOutboxModel{}).Where("event_id = ? AND delivered_at IS NULL AND lane_input_id IS NULL", outbox.EventID).
			Updates(map[string]any{"lane_input_id": inputID, "delivered_at": now})
		if update.Error != nil || update.RowsAffected != 1 {
			return mediapreviewruntime.ErrPersistence
		}
		bridged = true
		return nil
	})
	return bridged, mapMediaLaneError(err)
}

type mediaTerminalClaimRow struct {
	BridgeInputID   string    `gorm:"column:bridge_input_id"`
	OriginalInputID string    `gorm:"column:original_input_id"`
	SessionID       string    `gorm:"column:session_id"`
	TurnID          string    `gorm:"column:turn_id"`
	RunID           string    `gorm:"column:run_id"`
	ToolCallID      string    `gorm:"column:tool_call_id"`
	ToolKey         string    `gorm:"column:tool_key"`
	OperationID     string    `gorm:"column:operation_id"`
	BatchID         string    `gorm:"column:batch_id"`
	JobID           string    `gorm:"column:job_id"`
	JobType         string    `gorm:"column:job_type"`
	TargetJSON      string    `gorm:"column:target_json"`
	TerminalEventID string    `gorm:"column:terminal_event_id"`
	TerminalStatus  string    `gorm:"column:terminal_status"`
	ResultDigest    string    `gorm:"column:result_digest"`
	ResultJSON      string    `gorm:"column:result_json"`
	OccurredAt      time.Time `gorm:"column:occurred_at"`
	LeaseFence      int64     `gorm:"column:lease_fence"`
	LeaseVersion    int64     `gorm:"column:lease_version"`
	DatabaseNow     time.Time `gorm:"column:database_now"`
}

// ClaimNextTerminal 只领取全局真正 HOL 的 media_job_preview_terminal Input。
func (r *MediaRuntimeRepository) ClaimNextTerminal(ctx context.Context, owner string, leaseDuration time.Duration) (*mediapreviewruntime.TerminalClaim, error) {
	if owner == "" || leaseDuration <= 0 {
		return nil, mediapreviewruntime.ErrInvalidInput
	}
	var row mediaTerminalClaimRow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Raw(`
			WITH database_clock AS MATERIALIZED (SELECT clock_timestamp() AS database_now),
			head_ids AS MATERIALIZED (
				SELECT DISTINCT ON (candidate.session_id) candidate.session_id, candidate.id
				FROM agent.session_input AS candidate
				WHERE candidate.status IN ('pending','claimed','running','retry_wait','recovery_pending')
				ORDER BY candidate.session_id, candidate.enqueue_seq, candidate.id
			)
			SELECT input_record.id AS bridge_input_id, operation_record.input_id AS original_input_id,
			       input_record.session_id, operation_record.turn_id, operation_record.run_id,
			       operation_record.tool_call_id, operation_record.tool_key AS tool_key,
			       outbox.operation_id, outbox.batch_id,
			       outbox.job_id, job_record.job_type, job_record.target::text AS target_json,
			       outbox.event_id AS terminal_event_id,
			       outbox.terminal_status, outbox.result_digest, outbox.result::text AS result_json,
			       outbox.occurred_at, lease.fence_token AS lease_fence, lease.version AS lease_version,
			       database_clock.database_now
			FROM head_ids CROSS JOIN database_clock
			JOIN agent.session_input AS input_record ON input_record.id = head_ids.id
			JOIN agent.session_runtime_lease AS lease ON lease.session_id = input_record.session_id
			JOIN agent.media_preview_terminal_outbox AS outbox ON outbox.lane_input_id = input_record.id
			JOIN agent.media_preview_operation AS operation_record ON operation_record.operation_id = outbox.operation_id
			JOIN agent.media_preview_job AS job_record ON job_record.job_id = outbox.job_id
			 AND job_record.operation_id = outbox.operation_id AND job_record.batch_id = outbox.batch_id
			WHERE input_record.source_type = 'media_job_preview_terminal'
			  AND operation_record.planned_job_id = job_record.job_id
			  AND input_record.available_at <= database_clock.database_now
			  AND (lease.lease_owner IS NULL OR lease.lease_until <= database_clock.database_now)
			  AND (input_record.lease_owner IS NULL OR input_record.lease_until <= database_clock.database_now)
			ORDER BY input_record.available_at, input_record.session_id, input_record.enqueue_seq
			FOR UPDATE OF input_record, lease SKIP LOCKED LIMIT 1`).Scan(&row)
		if query.Error != nil || query.RowsAffected == 0 {
			return query.Error
		}
		newFence := row.LeaseFence + 1
		until := row.DatabaseNow.Add(leaseDuration)
		lease := tx.Model(&sessionRuntimeLeaseModel{}).Where("session_id = ? AND version = ? AND fence_token = ? AND (lease_owner IS NULL OR lease_until <= ?)",
			row.SessionID, row.LeaseVersion, row.LeaseFence, row.DatabaseNow).Updates(map[string]any{"lease_owner": owner,
			"lease_until": until, "fence_token": newFence, "version": gorm.Expr("version + 1"), "updated_at": row.DatabaseNow})
		if lease.Error != nil || lease.RowsAffected != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		input := tx.Model(&sessionInputModel{}).Where("id = ? AND status IN ?", row.BridgeInputID,
			[]string{"pending", "claimed", "running", "retry_wait", "recovery_pending"}).Updates(map[string]any{"status": "claimed",
			"attempts": gorm.Expr("attempts + 1"), "lease_owner": owner, "lease_until": until, "fence_token": newFence, "updated_at": row.DatabaseNow})
		if input.Error != nil || input.RowsAffected != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		row.LeaseFence = newFence
		return nil
	})
	if err != nil {
		return nil, mapMediaLaneError(err)
	}
	if row.BridgeInputID == "" {
		return nil, nil
	}
	var target mediapreview.Target
	if err := json.Unmarshal([]byte(row.TargetJSON), &target); err != nil ||
		!mediapreview.ValidUUIDv7(target.AssetID) || target.AssetVersion != 1 {
		return nil, mediapreviewruntime.ErrOutputContract
	}
	return &mediapreviewruntime.TerminalClaim{Owner: owner, BridgeInputID: row.BridgeInputID,
		OriginalInputID: row.OriginalInputID, SessionID: row.SessionID, TurnID: row.TurnID, RunID: row.RunID,
		ToolCallID: row.ToolCallID, ToolKey: row.ToolKey, OperationID: row.OperationID, BatchID: row.BatchID,
		JobID: row.JobID, JobType: row.JobType, AssetID: target.AssetID, AssetVersion: target.AssetVersion,
		TerminalEventID: row.TerminalEventID, TerminalStatus: row.TerminalStatus,
		ResultDigest: row.ResultDigest, ResultJSON: []byte(row.ResultJSON), FenceToken: row.LeaseFence, OccurredAt: row.OccurredAt.UTC()}, nil
}

// CompleteTerminal 校验冻结 Result，AppendOnce 投影最终 Card 并释放 Terminal Lane。
func (r *MediaRuntimeRepository) CompleteTerminal(ctx context.Context, claim mediapreviewruntime.TerminalClaim, result mediapreviewruntime.TerminalResult) error {
	if mediapreviewruntime.ValidateTerminalResultBinding(claim, result) != nil {
		return mediapreviewruntime.ErrOutputContract
	}
	return r.withActiveMediaFence(ctx, claim.SessionID, claim.BridgeInputID, claim.Owner, claim.FenceToken, func(tx *gorm.DB, now time.Time) error {
		kind, mime := mediaKindAndMIME(claim.ToolKey)
		card := event.MediaPreviewCardPayload{SchemaVersion: event.MediaPreviewCardSchemaVersionV1,
			InputID: claim.OriginalInputID, TurnID: claim.TurnID, RunID: claim.RunID, ToolCallID: claim.ToolCallID,
			ToolKey: claim.ToolKey, OperationID: claim.OperationID, BatchID: claim.BatchID, JobID: claim.JobID,
			UpdatedAt: claim.OccurredAt.UTC()}
		var record event.Record
		var err error
		if result.Status == "succeeded" && result.AssetRef != nil {
			card.Status, card.ResultCode = "completed", "MEDIA_PREVIEW_COMPLETED"
			card.AssetRef = &event.MediaPreviewAssetRef{ID: result.AssetRef.AssetID, Version: result.AssetRef.Version,
				Status: result.AssetRef.Status, MediaKind: result.AssetRef.MediaKind, MIMEType: result.AssetRef.MIMEType,
				ContentDigest: result.AssetRef.ContentDigest, SizeBytes: result.AssetRef.SizeBytes}
			card.ContentURL = "/api/v1/projects/" + mediaProjectIDForTerminal(tx, claim.OperationID) + "/media-preview-assets/" + result.AssetRef.AssetID + "/content"
			record, err = event.NewMediaPreviewCompleted(claim.TerminalEventID, claim.SessionID, claim.TerminalEventID, claim.BridgeInputID, card, now)
		} else {
			card.Status, card.ResultCode, card.ErrorCode = "failed", result.ErrorCode, result.ErrorCode
			card.AssetRef = &event.MediaPreviewAssetRef{ID: claim.AssetID, Version: 1, Status: "failed", MediaKind: kind, MIMEType: mime}
			if result.ErrorCode == "MEDIA_PREVIEW_RUNTIME_FAILED" {
				record, err = event.NewMediaPreviewRuntimeFailed(claim.TerminalEventID, claim.SessionID, claim.TerminalEventID, claim.BridgeInputID, card, now)
			} else {
				record, err = event.NewMediaPreviewFailed(claim.TerminalEventID, claim.SessionID, claim.TerminalEventID, claim.BridgeInputID, card, now)
			}
		}
		if err != nil {
			return mediapreviewruntime.ErrOutputContract
		}
		if err := appendMediaEvent(tx, now, record); err != nil {
			return err
		}
		input := tx.Model(&sessionInputModel{}).Where("id = ? AND status = 'claimed' AND lease_owner = ? AND fence_token = ?", claim.BridgeInputID,
			claim.Owner, claim.FenceToken).Updates(map[string]any{"status": "resolved", "lease_owner": nil, "lease_until": nil, "updated_at": now})
		if input.Error != nil || input.RowsAffected != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		return releaseMediaLane(tx, claim.SessionID, claim.Owner, claim.FenceToken, now)
	})
}

func mediaProjectIDForTerminal(tx *gorm.DB, operationID string) string {
	var row struct {
		ProjectID string `gorm:"column:project_id"`
	}
	if tx.Model(&mediaPreviewOperationModel{}).Select("project_id").Where("operation_id = ?", operationID).Take(&row).Error != nil {
		return ""
	}
	return row.ProjectID
}

func (r *MediaRuntimeRepository) withActiveMediaFence(ctx context.Context, sessionID, inputID, owner string, fence int64, callback func(*gorm.DB, time.Time) error) error {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now, err := mediaDatabaseNow(tx)
		if err != nil {
			return err
		}
		var count int64
		if err := tx.Raw(`SELECT COUNT(*) FROM agent.session_runtime_lease AS lease
			JOIN agent.session_input AS input_record ON input_record.session_id = lease.session_id
			WHERE lease.session_id = ? AND lease.lease_owner = ? AND lease.fence_token = ? AND lease.lease_until > ?
			  AND input_record.id = ? AND input_record.lease_owner = ? AND input_record.fence_token = ? AND input_record.lease_until > ?`,
			sessionID, owner, fence, now, inputID, owner, fence, now).Scan(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return mediapreviewruntime.ErrFenceLost
		}
		return callback(tx, now)
	})
	return mapMediaLaneError(err)
}

func releaseMediaLane(tx *gorm.DB, sessionID, owner string, fence int64, now time.Time) error {
	update := tx.Model(&sessionRuntimeLeaseModel{}).Where("session_id = ? AND lease_owner = ? AND fence_token = ?", sessionID, owner, fence).
		Updates(map[string]any{"lease_owner": nil, "lease_until": nil, "version": gorm.Expr("version + 1"), "updated_at": now})
	if update.Error != nil || update.RowsAffected != 1 {
		return mediapreviewruntime.ErrFenceLost
	}
	return nil
}

func appendMediaEvent(tx *gorm.DB, now time.Time, record event.Record) error {
	var existing sessionEventLogModel
	if err := tx.Where("event_id = ?", record.EventID).Take(&existing).Error; err == nil {
		if existing.SessionID == record.SessionID && existing.EventType == string(record.Type) && existing.Payload == string(record.PayloadJSON) {
			return nil
		}
		return mediapreviewruntime.ErrIdempotencyConflict
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var counter sessionEventCounterModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", record.SessionID).Take(&counter).Error; err != nil {
		return err
	}
	sequence := counter.LastSeq + 1
	model := sessionEventLogModel{EventID: record.EventID, SessionID: record.SessionID, Seq: sequence,
		EventType: string(record.Type), SchemaVersion: record.SchemaVersion, SourceKind: record.SourceKind,
		SourceID: record.SourceID, ProjectionIndex: record.ProjectionIndex, AggregateType: string(record.AggregateType),
		AggregateID: record.AggregateID, AggregateVersion: record.AggregateVersion, Payload: string(record.PayloadJSON), CreatedAt: now}
	if err := tx.Create(&model).Error; err != nil {
		return err
	}
	update := tx.Model(&sessionEventCounterModel{}).Where("session_id = ? AND last_seq = ?", record.SessionID, counter.LastSeq).
		Updates(map[string]any{"last_seq": sequence, "updated_at": now})
	if update.Error != nil || update.RowsAffected != 1 {
		return mediapreviewruntime.ErrPersistence
	}
	return nil
}

func mediaKindAndMIME(toolKey string) (string, string) {
	if toolKey == mediapreview.GenerateMediaToolKey {
		return "image", "image/png"
	}
	return "video", "video/mp4"
}

func mediaSourceType(toolKey string) string {
	if toolKey == mediapreview.GenerateMediaToolKey {
		return mediapreviewruntime.GenerateSourceType
	}
	return mediapreviewruntime.AssembleSourceType
}

func mediaDigest(value []byte) string {
	digest, _ := mediapreview.DigestJSON(json.RawMessage(value))
	return digest
}

func mapMediaLaneError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return mediapreviewruntime.ErrNotFound
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return mediapreviewruntime.ErrIdempotencyConflict
	}
	return err
}

var _ mediapreviewruntime.Repository = (*MediaRuntimeRepository)(nil)
