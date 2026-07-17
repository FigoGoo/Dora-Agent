package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"github.com/FigoGoo/Dora-Agent/agent/internal/usermessageruntime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserMessageLegacyUpgradeRepository 是 Preview Profile 专用的 legacy Helper Adapter。
type UserMessageLegacyUpgradeRepository struct{ db *gorm.DB }

func NewUserMessageLegacyUpgradeRepository(client *Client) (*UserMessageLegacyUpgradeRepository, error) {
	if client == nil || client.db == nil {
		return nil, fmt.Errorf("create user message legacy upgrade repository: dependency is nil")
	}
	return &UserMessageLegacyUpgradeRepository{db: client.db}, nil
}

type legacyUpgradePreviewRow struct {
	CommandID, CommandType, RequestDigest, ReceiptSessionID, ReceiptMessageID, ReceiptInputID string
	ReceiptSkillDigest                                                                        string
	ReceiptSkillCount                                                                         int
	SessionID, ProjectID, UserID, Status                                                      string
	Archived                                                                                  bool
	InputID, InputSource, InputSourceID, InputMessageID, InputStatus                          string
	EnqueueSeq                                                                                int64
	Attempts                                                                                  int
	InputLeaseIdle                                                                            bool
	InputFence                                                                                int64
	LeaseIdle                                                                                 bool
	LeaseFence                                                                                int64
	MessageID                                                                                 string
	MessageSeq                                                                                int64
	MessageRole, MessageSourceKind, MessageSourceID, MessageDigest                            string
	MessageCiphertext                                                                         []byte
	MessageKeyVersion                                                                         string
	SnapshotKind, SnapshotDigest                                                              string
	SnapshotCount                                                                             int
	AcceptedEventValid                                                                        bool
	TurnPresent, ContextPresent                                                               bool
	LedgerInputID, LedgerSessionID, LedgerStage, LedgerTurnID, LedgerContextDigest            *string
	LedgerGeneration, LedgerVersion                                                           *int64
	LedgerCreatedAt, LedgerUpdatedAt                                                          *time.Time
}

// Preview 从 Ensure Receipt 根出发做 anti-join；孤立 Ledger/Turn/Context 永远不会成为候选。
func (r *UserMessageLegacyUpgradeRepository) Preview(ctx context.Context, limit int) (usermessageruntime.LegacyUpgradePreview, error) {
	if limit <= 0 {
		return usermessageruntime.LegacyUpgradePreview{}, fmt.Errorf("preview legacy upgrade: invalid limit")
	}
	var rows []legacyUpgradePreviewRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT receipt.command_id, receipt.command_type, receipt.request_digest,
		       receipt.session_id AS receipt_session_id,
		       COALESCE(receipt.message_id::text, '') AS receipt_message_id,
		       COALESCE(receipt.input_id::text, '') AS receipt_input_id,
		       receipt.skill_snapshot_digest AS receipt_skill_digest,
		       receipt.skill_count AS receipt_skill_count,
		       COALESCE(session_record.id::text, receipt.session_id::text) AS session_id,
		       COALESCE(session_record.project_id::text, '') AS project_id,
		       COALESCE(session_record.user_id::text, '') AS user_id,
		       COALESCE(session_record.status, '') AS status,
		       session_record.archived_at IS NOT NULL AS archived,
		       COALESCE(input_record.id::text, receipt.input_id::text) AS input_id,
		       COALESCE(input_record.source_type, '') AS input_source,
		       COALESCE(input_record.source_id::text, '') AS input_source_id,
		       COALESCE(input_record.message_id::text, '') AS input_message_id,
		       COALESCE(input_record.status, '') AS input_status,
		       COALESCE(input_record.enqueue_seq, 0) AS enqueue_seq,
		       COALESCE(input_record.attempts, -1) AS attempts,
		       input_record.lease_owner IS NULL AND input_record.lease_until IS NULL AS input_lease_idle,
		       COALESCE(input_record.fence_token, -1) AS input_fence,
		       lease.lease_owner IS NULL AND lease.lease_until IS NULL AS lease_idle,
		       COALESCE(lease.fence_token, -1) AS lease_fence,
		       COALESCE(message_record.id::text, receipt.message_id::text) AS message_id,
		       COALESCE(message_record.message_seq, 0) AS message_seq,
		       COALESCE(message_record.role, '') AS message_role,
		       COALESCE(message_record.source_kind, '') AS message_source_kind,
		       COALESCE(message_record.source_id::text, '') AS message_source_id,
		       COALESCE(message_record.content_digest, '') AS message_digest,
		       message_record.content_ciphertext AS message_ciphertext,
		       COALESCE(message_record.content_key_version, '') AS message_key_version,
		       COALESCE(snapshot.snapshot_kind, '') AS snapshot_kind,
		       COALESCE(snapshot.snapshot_digest, '') AS snapshot_digest,
		       COALESCE(snapshot.skill_count, -1) AS snapshot_count,
		       EXISTS (
		           SELECT 1 FROM agent.session_event_log created
		           JOIN agent.session_event_counter counter ON counter.session_id = created.session_id
		           WHERE created.session_id = receipt.session_id
		             AND created.seq BETWEEN counter.min_available_seq AND counter.last_seq
		             AND created.event_type = 'session.created'
		             AND created.schema_version = 'session.event.v1'
		             AND created.source_kind = 'ensure_project_session'
		             AND created.source_id = receipt.command_id
		             AND created.projection_index = 0
		             AND created.aggregate_type = 'session'
		             AND created.aggregate_id = receipt.session_id
		             AND created.aggregate_version = session_record.version
		             AND created.payload = jsonb_build_object(
		                 'session_id', receipt.session_id::text,
		                 'project_id', session_record.project_id::text,
		                 'status', 'active',
		                 'version', session_record.version)
		       ) AND EXISTS (
		           SELECT 1 FROM agent.session_event_log accepted
		           JOIN agent.session_event_counter counter ON counter.session_id = accepted.session_id
		           WHERE accepted.session_id = receipt.session_id
		             AND accepted.seq BETWEEN counter.min_available_seq AND counter.last_seq
		             AND accepted.event_type = 'session.input.accepted'
		             AND accepted.schema_version = 'session.event.v1'
		             AND accepted.source_kind = 'ensure_project_session'
		             AND accepted.source_id = receipt.command_id
		             AND accepted.projection_index = 1
		             AND accepted.aggregate_type = 'session_input'
		             AND accepted.aggregate_id = receipt.input_id
		             AND accepted.aggregate_version = 1
		             AND accepted.payload = jsonb_build_object(
		                 'session_id', receipt.session_id::text,
		                 'input_id', receipt.input_id::text,
		                 'message_id', receipt.message_id::text,
		                 'enqueue_seq', 1,
		                 'status', 'pending')
		       ) AS accepted_event_valid,
		       turn_record.turn_id IS NOT NULL AS turn_present,
		       context_record.turn_id IS NOT NULL AS context_present,
		       ledger.input_id::text AS ledger_input_id,
		       ledger.session_id::text AS ledger_session_id,
		       ledger.stage AS ledger_stage,
		       ledger.turn_id::text AS ledger_turn_id,
		       ledger.context_digest AS ledger_context_digest,
		       ledger.upgrade_generation AS ledger_generation,
		       ledger.version AS ledger_version,
		       ledger.created_at AS ledger_created_at,
		       ledger.updated_at AS ledger_updated_at
		FROM agent.session_command_receipt receipt
		LEFT JOIN agent.session_input input_record ON input_record.id = receipt.input_id
		LEFT JOIN agent.session session_record ON session_record.id = receipt.session_id
		LEFT JOIN agent.session_runtime_lease lease ON lease.session_id = receipt.session_id
		LEFT JOIN agent.session_message message_record ON message_record.id = receipt.message_id
		LEFT JOIN agent.session_skill_snapshot snapshot ON snapshot.session_id = receipt.session_id
		LEFT JOIN agent.session_user_message_upgrade_ledger ledger ON ledger.input_id = receipt.input_id
		LEFT JOIN agent.session_user_message_turn turn_record ON turn_record.input_id = receipt.input_id
		LEFT JOIN agent.session_user_message_turn_context context_record ON context_record.turn_id = turn_record.turn_id
		WHERE receipt.input_id IS NOT NULL AND receipt.message_id IS NOT NULL
		  AND receipt.command_type IN ('ensure_project_session_v1','ensure_project_session_v2')
		  AND (ledger.stage IS NULL OR ledger.stage <> 'verified')
		  AND NOT (ledger.input_id IS NULL AND turn_record.turn_id IS NOT NULL AND context_record.turn_id IS NOT NULL)
		ORDER BY CASE ledger.stage WHEN 'applied' THEN 0 WHEN 'prepared' THEN 1 ELSE 2 END,
		         receipt.input_id`).Scan(&rows).Error
	if err != nil {
		return usermessageruntime.LegacyUpgradePreview{}, err
	}
	result := usermessageruntime.LegacyUpgradePreview{Candidates: make([]usermessageruntime.LegacyUpgradeCandidate, 0, len(rows))}
	for _, row := range rows {
		result.Candidates = append(result.Candidates, mapLegacyUpgradeCandidate(row))
	}
	var orphanFacts bool
	if err := r.db.WithContext(ctx).Raw(`
		SELECT
			EXISTS (
				SELECT 1
				FROM agent.session_user_message_upgrade_ledger ledger
				LEFT JOIN agent.session_input input_record ON input_record.id = ledger.input_id
				LEFT JOIN agent.session_command_receipt receipt ON receipt.input_id = ledger.input_id
				WHERE input_record.id IS NULL OR receipt.command_id IS NULL
				   OR ledger.session_id <> input_record.session_id OR receipt.session_id <> ledger.session_id
			)
			OR EXISTS (
				SELECT 1
				FROM agent.session_user_message_turn_context context_record
				LEFT JOIN agent.session_user_message_turn turn_record ON turn_record.turn_id = context_record.turn_id
				WHERE turn_record.turn_id IS NULL OR turn_record.input_id <> context_record.input_id
				   OR turn_record.session_id <> context_record.session_id
			)
			OR EXISTS (
				SELECT 1
				FROM agent.session_user_message_turn turn_record
				LEFT JOIN agent.session_input input_record ON input_record.id = turn_record.input_id
				LEFT JOIN agent.session_command_receipt receipt ON receipt.input_id = turn_record.input_id
				LEFT JOIN agent.session_user_message_turn_context context_record ON context_record.turn_id = turn_record.turn_id
				WHERE input_record.id IS NULL OR receipt.command_id IS NULL OR context_record.turn_id IS NULL
				   OR turn_record.session_id <> input_record.session_id OR receipt.session_id <> turn_record.session_id
			)`).Scan(&orphanFacts).Error; err != nil {
		return usermessageruntime.LegacyUpgradePreview{}, err
	}
	if orphanFacts {
		result.Blockers = append(result.Blockers, usermessageruntime.LegacyUpgradeBlocker{Code: "legacy_upgrade_incomplete"})
	}
	return result, nil
}

func mapLegacyUpgradeCandidate(row legacyUpgradePreviewRow) usermessageruntime.LegacyUpgradeCandidate {
	c := usermessageruntime.LegacyUpgradeCandidate{
		CommandID: row.CommandID, CommandType: row.CommandType, RequestDigest: row.RequestDigest,
		ReceiptSessionID: row.ReceiptSessionID, ReceiptMessageID: row.ReceiptMessageID, ReceiptInputID: row.ReceiptInputID,
		ReceiptSkillDigest: row.ReceiptSkillDigest, ReceiptSkillCount: row.ReceiptSkillCount,
		SessionID: row.SessionID, ProjectID: row.ProjectID, UserID: row.UserID, Status: row.Status, Archived: row.Archived,
		InputID: row.InputID, InputSource: row.InputSource, InputSourceID: row.InputSourceID,
		InputMessageID: row.InputMessageID, InputStatus: row.InputStatus, EnqueueSeq: row.EnqueueSeq,
		Attempts: row.Attempts, InputLeaseIdle: row.InputLeaseIdle, InputFence: row.InputFence,
		LeaseIdle: row.LeaseIdle, LeaseFence: row.LeaseFence,
		MessageID: row.MessageID, MessageSeq: row.MessageSeq, MessageRole: row.MessageRole,
		MessageSourceKind: row.MessageSourceKind, MessageSourceID: row.MessageSourceID,
		MessageDigest: row.MessageDigest, MessageProtected: session.ProtectedContent{Ciphertext: row.MessageCiphertext, KeyVersion: row.MessageKeyVersion},
		SnapshotKind: session.SkillSnapshotKind(row.SnapshotKind), SnapshotDigest: row.SnapshotDigest,
		SnapshotCount: row.SnapshotCount, AcceptedEventValid: row.AcceptedEventValid,
		TurnPresent: row.TurnPresent, ContextPresent: row.ContextPresent,
	}
	if row.LedgerInputID != nil {
		c.Ledger = &usermessageruntime.LegacyUpgradeLedger{InputID: *row.LedgerInputID, SessionID: valueString(row.LedgerSessionID),
			Stage: valueString(row.LedgerStage), TurnID: valueString(row.LedgerTurnID), ContextDigest: valueString(row.LedgerContextDigest),
			UpgradeGeneration: valueInt64(row.LedgerGeneration), Version: valueInt64(row.LedgerVersion),
			CreatedAt: valueTime(row.LedgerCreatedAt), UpdatedAt: valueTime(row.LedgerUpdatedAt)}
	}
	return c
}

func (r *UserMessageLegacyUpgradeRepository) Prepare(ctx context.Context, plan usermessageruntime.LegacyUpgradePreparePlan) (usermessageruntime.LegacyUpgradeLedger, error) {
	var result userMessageLegacyUpgradeLedgerModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		input, lease, ledger, err := lockLegacyUpgradeHead(tx, plan.Candidate.InputID, plan.Candidate.SessionID)
		if err != nil {
			return err
		}
		if err := validateLockedLegacyFoundation(tx, plan.Candidate, input, lease); err != nil {
			return err
		}
		if ledger != nil {
			result = *ledger
			return nil
		}
		if err := lockLegacyUpgradeTargetsAbsent(tx, plan.Candidate.InputID, plan.TurnID); err != nil {
			return err
		}
		now, err := databaseClock(tx)
		if err != nil {
			return err
		}
		result = userMessageLegacyUpgradeLedgerModel{InputID: plan.Candidate.InputID, SessionID: plan.Candidate.SessionID,
			Stage: usermessageruntime.LegacyUpgradeStagePrepared, TurnID: plan.TurnID, ContextDigest: plan.ContextDigest,
			UpgradeGeneration: usermessageruntime.LegacyUpgradeGeneration, Version: 1, CreatedAt: now, UpdatedAt: now}
		return tx.Create(&result).Error
	})
	return mapLegacyLedger(result), mapLegacyUpgradeError(err)
}

func (r *UserMessageLegacyUpgradeRepository) Apply(ctx context.Context, plan usermessageruntime.LegacyUpgradeApplyPlan) (usermessageruntime.LegacyUpgradeLedger, error) {
	var result userMessageLegacyUpgradeLedgerModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		input, lease, ledger, err := lockLegacyUpgradeHead(tx, plan.Candidate.InputID, plan.Candidate.SessionID)
		if err != nil {
			return err
		}
		if err := validateLockedLegacyFoundation(tx, plan.Candidate, input, lease); err != nil {
			return err
		}
		if ledger == nil || !sameLegacyLedgerIdentity(*ledger, plan.Ledger) {
			return usermessageruntime.ErrLegacyUpgradeConflict
		}
		var turn userMessageTurnModel
		turnErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("input_id = ?", plan.Candidate.InputID).Take(&turn).Error
		if turnErr != nil && !errors.Is(turnErr, gorm.ErrRecordNotFound) {
			return turnErr
		}
		var contextRow userMessageContextModel
		contextErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("turn_id = ?", ledger.TurnID).Take(&contextRow).Error
		if contextErr != nil && !errors.Is(contextErr, gorm.ErrRecordNotFound) {
			return contextErr
		}
		if ledger.Stage == usermessageruntime.LegacyUpgradeStageApplied || ledger.Stage == usermessageruntime.LegacyUpgradeStageVerified {
			if turnErr != nil || contextErr != nil || !sameLegacyAppliedFacts(turn, contextRow, plan.Candidate, *ledger) {
				return usermessageruntime.ErrLegacyUpgradeConflict
			}
			result = *ledger
			return nil
		}
		if ledger.Stage != usermessageruntime.LegacyUpgradeStagePrepared ||
			plan.Ledger.Stage != usermessageruntime.LegacyUpgradeStagePrepared || ledger.Version != plan.Ledger.Version ||
			turnErr == nil || contextErr == nil {
			return usermessageruntime.ErrLegacyUpgradeConflict
		}
		if plan.Turn.TurnID != ledger.TurnID || plan.Context.TurnID != ledger.TurnID || plan.Context.ContextDigest != ledger.ContextDigest {
			return usermessageruntime.ErrLegacyUpgradeConflict
		}
		now, err := databaseClock(tx)
		if err != nil {
			return err
		}
		turn = mapLegacyTurnModel(plan.Turn, now)
		contextRow = mapLegacyContextModel(plan.Context, now)
		if err := tx.Create(&turn).Error; err != nil {
			return err
		}
		if err := tx.Create(&contextRow).Error; err != nil {
			return err
		}
		update := tx.Model(&userMessageLegacyUpgradeLedgerModel{}).
			Where("input_id = ? AND stage = 'prepared' AND version = ?", ledger.InputID, ledger.Version).
			Updates(map[string]any{"stage": usermessageruntime.LegacyUpgradeStageApplied,
				"version": gorm.Expr("version + 1"), "updated_at": gorm.Expr("GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')")})
		if update.Error != nil || update.RowsAffected != 1 {
			return usermessageruntime.ErrLegacyUpgradeConflict
		}
		return tx.Where("input_id = ?", ledger.InputID).Take(&result).Error
	})
	return mapLegacyLedger(result), mapLegacyUpgradeError(err)
}

func (r *UserMessageLegacyUpgradeRepository) Verify(ctx context.Context, plan usermessageruntime.LegacyUpgradeVerifyPlan) (usermessageruntime.LegacyUpgradeLedger, error) {
	var result userMessageLegacyUpgradeLedgerModel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		input, lease, ledger, err := lockLegacyUpgradeHead(tx, plan.Candidate.InputID, plan.Candidate.SessionID)
		if err != nil {
			return err
		}
		if err := validateLockedLegacyFoundation(tx, plan.Candidate, input, lease); err != nil {
			return err
		}
		if ledger == nil || !sameLegacyLedgerIdentity(*ledger, plan.Ledger) {
			return usermessageruntime.ErrLegacyUpgradeConflict
		}
		var turn userMessageTurnModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("input_id = ?", plan.Candidate.InputID).Take(&turn).Error; err != nil {
			return err
		}
		var contextRow userMessageContextModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("turn_id = ?", ledger.TurnID).Take(&contextRow).Error; err != nil {
			return err
		}
		if !sameLegacyAppliedFacts(turn, contextRow, plan.Candidate, *ledger) || !sameLegacyContext(contextRow, plan.Context) {
			return usermessageruntime.ErrLegacyUpgradeConflict
		}
		var forbidden int64
		if err := tx.Raw(`SELECT
			(SELECT count(*) FROM agent.session_user_message_run WHERE input_id = ?) +
			(SELECT count(*) FROM agent.session_user_message_model_receipt WHERE input_id = ?) +
			(SELECT count(*) FROM agent.session_user_message_output_receipt WHERE input_id = ?)`,
			plan.Candidate.InputID, plan.Candidate.InputID, plan.Candidate.InputID).Scan(&forbidden).Error; err != nil {
			return err
		}
		if forbidden != 0 {
			return usermessageruntime.ErrLegacyUpgradeConflict
		}
		if ledger.Stage == usermessageruntime.LegacyUpgradeStageVerified {
			result = *ledger
			return nil
		}
		if ledger.Stage != usermessageruntime.LegacyUpgradeStageApplied ||
			plan.Ledger.Stage != usermessageruntime.LegacyUpgradeStageApplied || ledger.Version != plan.Ledger.Version {
			return usermessageruntime.ErrLegacyUpgradeConflict
		}
		update := tx.Model(&userMessageLegacyUpgradeLedgerModel{}).
			Where("input_id = ? AND stage = 'applied' AND version = ?", ledger.InputID, ledger.Version).
			Updates(map[string]any{"stage": usermessageruntime.LegacyUpgradeStageVerified,
				"version": gorm.Expr("version + 1"), "updated_at": gorm.Expr("GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')")})
		if update.Error != nil || update.RowsAffected != 1 {
			return usermessageruntime.ErrLegacyUpgradeConflict
		}
		return tx.Where("input_id = ?", ledger.InputID).Take(&result).Error
	})
	return mapLegacyLedger(result), mapLegacyUpgradeError(err)
}

// lockLegacyUpgradeHead 固定全 Helper 的 Input -> Lease -> Ledger 锁序。
func lockLegacyUpgradeHead(tx *gorm.DB, inputID, sessionID string) (sessionInputModel, sessionRuntimeLeaseModel, *userMessageLegacyUpgradeLedgerModel, error) {
	var input sessionInputModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", inputID).Take(&input).Error; err != nil {
		return input, sessionRuntimeLeaseModel{}, nil, err
	}
	var lease sessionRuntimeLeaseModel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("session_id = ?", sessionID).Take(&lease).Error; err != nil {
		return input, lease, nil, err
	}
	var ledger userMessageLegacyUpgradeLedgerModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("input_id = ?", inputID).Take(&ledger).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return input, lease, nil, nil
	}
	return input, lease, &ledger, err
}

func lockLegacyUpgradeTargetsAbsent(tx *gorm.DB, inputID, turnID string) error {
	var turn userMessageTurnModel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("input_id = ?", inputID).Take(&turn).Error
	if err == nil {
		return usermessageruntime.ErrLegacyUpgradeConflict
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var contextRow userMessageContextModel
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("turn_id = ?", turnID).Take(&contextRow).Error
	if err == nil {
		return usermessageruntime.ErrLegacyUpgradeConflict
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

func validateLockedLegacyFoundation(tx *gorm.DB, c usermessageruntime.LegacyUpgradeCandidate, input sessionInputModel, lease sessionRuntimeLeaseModel) error {
	if input.ID != c.InputID || input.SessionID != c.SessionID || input.SourceType != "user_message" || input.SourceID != c.CommandID ||
		input.MessageID == nil || *input.MessageID != c.MessageID || input.Status != "pending" || input.EnqueueSeq != 1 ||
		input.Attempts != 0 || input.LeaseOwner != nil || input.LeaseUntil != nil || input.FenceToken != 0 ||
		lease.SessionID != c.SessionID || lease.LeaseOwner != nil || lease.LeaseUntil != nil || lease.FenceToken != 0 {
		return usermessageruntime.ErrLegacyUpgradeConflict
	}
	var count int64
	err := tx.Raw(`SELECT count(*) FROM agent.session_command_receipt receipt
		JOIN agent.session s ON s.id = receipt.session_id
		JOIN agent.session_message m ON m.id = receipt.message_id
		JOIN agent.session_skill_snapshot snapshot ON snapshot.session_id = receipt.session_id
		WHERE receipt.command_id = ? AND receipt.command_type = ? AND receipt.request_digest = ?
		  AND receipt.session_id = ? AND receipt.input_id = ? AND receipt.message_id = ?
		  AND receipt.skill_snapshot_digest = ? AND receipt.skill_count = ?
		  AND s.status = 'active' AND s.archived_at IS NULL AND s.project_id = ? AND s.user_id = ?
		  AND m.session_id = s.id AND m.message_seq = 1 AND m.role = 'user'
		  AND m.source_kind = 'ensure_project_session' AND m.source_id = receipt.command_id
		  AND m.content_digest = ? AND snapshot.snapshot_digest = receipt.skill_snapshot_digest
		  AND snapshot.skill_count = receipt.skill_count
		  AND EXISTS (SELECT 1 FROM agent.session_event_log created
		      JOIN agent.session_event_counter ec ON ec.session_id=created.session_id
		      WHERE created.session_id=receipt.session_id AND created.seq BETWEEN ec.min_available_seq AND ec.last_seq
		        AND created.event_type='session.created' AND created.schema_version='session.event.v1'
		        AND created.source_kind='ensure_project_session' AND created.source_id=receipt.command_id
		        AND created.projection_index=0 AND created.aggregate_type='session' AND created.aggregate_id=receipt.session_id
		        AND created.aggregate_version=s.version AND created.payload=jsonb_build_object('session_id',receipt.session_id::text,
		          'project_id',s.project_id::text,'status','active','version',s.version))
		  AND EXISTS (SELECT 1 FROM agent.session_event_log e
		      JOIN agent.session_event_counter ec ON ec.session_id=e.session_id
		      WHERE e.session_id=receipt.session_id AND e.seq BETWEEN ec.min_available_seq AND ec.last_seq
		        AND e.event_type='session.input.accepted' AND e.schema_version='session.event.v1'
		        AND e.source_kind='ensure_project_session' AND e.source_id=receipt.command_id
		        AND e.projection_index=1 AND e.aggregate_type='session_input' AND e.aggregate_id=receipt.input_id
		        AND e.aggregate_version=1 AND e.payload=jsonb_build_object('session_id',receipt.session_id::text,
		          'input_id',receipt.input_id::text,'message_id',receipt.message_id::text,'enqueue_seq',1,'status','pending'))`,
		c.CommandID, c.CommandType, c.RequestDigest, c.SessionID, c.InputID, c.MessageID,
		c.SnapshotDigest, c.SnapshotCount, c.ProjectID, c.UserID, c.MessageDigest).Scan(&count).Error
	if err != nil {
		return err
	}
	if count != 1 {
		return usermessageruntime.ErrLegacyUpgradeConflict
	}
	return nil
}

func databaseClock(tx *gorm.DB) (time.Time, error) {
	var now time.Time
	err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error
	return now, err
}

func mapLegacyTurnModel(v session.UserMessageTurn, now time.Time) userMessageTurnModel {
	return userMessageTurnModel{TurnID: v.TurnID, InputID: v.InputID, SessionID: v.SessionID, MessageID: v.MessageID,
		UserID: v.UserID, ProjectID: v.ProjectID, OutputID: v.OutputID, ModelCallID: v.ModelCallID,
		RecoveryEventID: v.RecoveryEventID, TerminalEventID: v.TerminalEventID,
		Status: "created", Version: 1, CreatedAt: now, UpdatedAt: now}
}

func mapLegacyContextModel(v session.UserMessageContext, now time.Time) userMessageContextModel {
	return userMessageContextModel{TurnID: v.TurnID, SchemaVersion: v.SchemaVersion, SessionID: v.SessionID, InputID: v.InputID,
		MessageID: v.MessageID, UserID: v.UserID, ProjectID: v.ProjectID, MessageCutoffSeq: v.MessageCutoffSeq,
		MessageContentDigest: v.MessageContentDigest, SkillSnapshotRef: v.SkillSnapshotRef, SkillSnapshotDigest: v.SkillSnapshotDigest,
		PromptRef: v.PromptRef, PromptDigest: v.PromptDigest, ToolRegistryRef: v.ToolRegistryRef,
		ToolRegistryDigest: v.ToolRegistryDigest, RuntimePolicyRef: v.RuntimePolicyRef, RuntimePolicyDigest: v.RuntimePolicyDigest,
		ModelRouteRef: v.ModelRouteRef, ModelRouteDigest: v.ModelRouteDigest, BudgetRef: v.BudgetRef, BudgetDigest: v.BudgetDigest,
		AccessScopeRef: v.AccessScopeRef, AccessScopeDigest: v.AccessScopeDigest, ContextDigest: v.ContextDigest, CreatedAt: now}
}

func sameLegacyAppliedFacts(turn userMessageTurnModel, contextRow userMessageContextModel, c usermessageruntime.LegacyUpgradeCandidate, ledger userMessageLegacyUpgradeLedgerModel) bool {
	return turn.TurnID == ledger.TurnID && turn.InputID == c.InputID && turn.SessionID == c.SessionID && turn.MessageID == c.MessageID &&
		turn.UserID == c.UserID && turn.ProjectID == c.ProjectID && turn.Status == "created" && turn.Version == 1 &&
		contextRow.TurnID == ledger.TurnID && contextRow.InputID == c.InputID && contextRow.SessionID == c.SessionID &&
		contextRow.MessageID == c.MessageID && contextRow.ContextDigest == ledger.ContextDigest
}

func sameLegacyContext(row userMessageContextModel, v session.UserMessageContext) bool {
	return row.SchemaVersion == v.SchemaVersion && row.TurnID == v.TurnID && row.SessionID == v.SessionID && row.InputID == v.InputID &&
		row.MessageID == v.MessageID && row.UserID == v.UserID && row.ProjectID == v.ProjectID && row.MessageCutoffSeq == v.MessageCutoffSeq &&
		row.MessageContentDigest == v.MessageContentDigest && row.SkillSnapshotRef == v.SkillSnapshotRef && row.SkillSnapshotDigest == v.SkillSnapshotDigest &&
		row.PromptRef == v.PromptRef && row.PromptDigest == v.PromptDigest && row.ToolRegistryRef == v.ToolRegistryRef &&
		row.ToolRegistryDigest == v.ToolRegistryDigest && row.RuntimePolicyRef == v.RuntimePolicyRef && row.RuntimePolicyDigest == v.RuntimePolicyDigest &&
		row.ModelRouteRef == v.ModelRouteRef && row.ModelRouteDigest == v.ModelRouteDigest && row.BudgetRef == v.BudgetRef &&
		row.BudgetDigest == v.BudgetDigest && row.AccessScopeRef == v.AccessScopeRef && row.AccessScopeDigest == v.AccessScopeDigest &&
		row.ContextDigest == v.ContextDigest
}

func sameLegacyLedgerIdentity(row userMessageLegacyUpgradeLedgerModel, value usermessageruntime.LegacyUpgradeLedger) bool {
	return row.InputID == value.InputID && row.SessionID == value.SessionID && row.TurnID == value.TurnID &&
		row.ContextDigest == value.ContextDigest && row.UpgradeGeneration == usermessageruntime.LegacyUpgradeGeneration &&
		value.UpgradeGeneration == usermessageruntime.LegacyUpgradeGeneration
}

func mapLegacyLedger(row userMessageLegacyUpgradeLedgerModel) usermessageruntime.LegacyUpgradeLedger {
	return usermessageruntime.LegacyUpgradeLedger{InputID: row.InputID, SessionID: row.SessionID, Stage: row.Stage,
		TurnID: row.TurnID, ContextDigest: row.ContextDigest, UpgradeGeneration: row.UpgradeGeneration,
		Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func mapLegacyUpgradeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, usermessageruntime.ErrLegacyUpgradeConflict) || errors.Is(err, gorm.ErrRecordNotFound) {
		return usermessageruntime.ErrLegacyUpgradeConflict
	}
	return err
}

func valueString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func valueInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
func valueTime(v *time.Time) time.Time {
	if v == nil {
		return time.Time{}
	}
	return *v
}
