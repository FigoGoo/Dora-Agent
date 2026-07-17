package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	previewruntime "github.com/FigoGoo/Dora-Agent/agent/internal/runtime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/session"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	previewRepoTestRequestID       = "019f68e8-4001-7000-8000-000000000001"
	previewRepoTestUserID          = "019f68e8-4002-7000-8000-000000000002"
	previewRepoTestProjectID       = "019f68e8-4003-7000-8000-000000000003"
	previewRepoTestSessionID       = "019f68e8-4004-7000-8000-000000000004"
	previewRepoTestInputID         = "019f68e8-4005-7000-8000-000000000005"
	previewRepoTestTurnID          = "019f68e8-4006-7000-8000-000000000006"
	previewRepoTestRunID           = "019f68e8-4007-7000-8000-000000000007"
	previewRepoTestToolCallID      = "019f68e8-4008-7000-8000-000000000008"
	previewRepoTestBusinessCmdID   = "019f68e8-4009-7000-8000-000000000009"
	previewRepoTestTerminalEventID = "019f68e8-4010-7000-8000-000000000010"
	previewRepoTestMessageID       = "019f68e8-4011-7000-8000-000000000011"
	previewRepoTestEventID         = "019f68e8-4012-7000-8000-000000000012"
	previewRepoTestIdempotencyKey  = "019f68e8-4013-7000-8000-000000000013"
	previewRepoTestPredecessorID   = "019f68e8-4014-7000-8000-000000000014"
	previewRepoTestDigest          = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// TestPreviewRepositoryRejectsNonPreviewPredecessorWithoutWrites 验证 Quick Create 的 initial_prompt
// 仍是非 Preview 未决 HOL 时 fail-fast；事务不得改写其 provenance，也不得推进任何计数器或追加 Event。
func TestPreviewRepositoryRejectsNonPreviewPredecessorWithoutWrites(t *testing.T) {
	repository, mock := newCreationSpecPreviewRepositoryMock(t)
	plan := previewRepoTestEnqueuePlan(t)
	now := plan.CreatedAt

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs(plan.IdempotencyKey).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."creation_spec_preview_run".*idempotency_key = \$1.*LIMIT \$2`).
		WithArgs(plan.IdempotencyKey, 1).
		WillReturnRows(sqlmock.NewRows([]string{"input_id"}))
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."session".*id = \$1.*user_id = \$2.*project_id = \$3.*status = \$4.*archived_at IS NULL.*LIMIT \$5`).
		WithArgs(plan.SessionID, plan.UserID, plan.ProjectID, string(session.StatusActive), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(plan.SessionID))
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."session_sequence_counter".*session_id = \$1.*LIMIT \$2 FOR UPDATE`).
		WithArgs(plan.SessionID, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"session_id", "last_message_seq", "last_input_enqueue_seq", "updated_at",
		}).AddRow(plan.SessionID, int64(1), int64(1), now))
	mock.ExpectQuery(`(?s)SELECT "id" FROM "agent"\."session_input".*session_id = \$1.*source_type <> \$2.*status NOT IN \(\$3,\$4\).*ORDER BY enqueue_seq ASC.*LIMIT \$5`).
		WithArgs(plan.SessionID, string(session.InputSourceTypeCreationSpecPreview),
			string(session.InputStatusResolved), string(session.InputStatusDead), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(previewRepoTestPredecessorID))
	mock.ExpectRollback()

	result, err := repository.Enqueue(context.Background(), plan)
	if !errors.Is(err, previewruntime.ErrSessionLaneBlocked) {
		t.Fatalf("非 Preview HOL 未被稳定拒绝: result=%+v err=%v", result, err)
	}
	if result != (previewruntime.EnqueueResult{}) {
		t.Fatalf("lane blocked 不得返回伪入队回执: %+v", result)
	}
}

// TestPreviewRepositoryEnqueueKeepsPlanInputIDAndSingleAcceptedEvent 验证正常/连续 Preview 入队
// 始终新建 plan.InputID，并只为该 Input 追加一次 accepted Event；不得接管或改写既有 Input。
func TestPreviewRepositoryEnqueueKeepsPlanInputIDAndSingleAcceptedEvent(t *testing.T) {
	repository, mock := newCreationSpecPreviewRepositoryMock(t)
	plan := previewRepoTestEnqueuePlan(t)
	now := plan.CreatedAt

	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WithArgs(plan.IdempotencyKey).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."creation_spec_preview_run".*idempotency_key = \$1.*LIMIT \$2`).
		WithArgs(plan.IdempotencyKey, 1).
		WillReturnRows(sqlmock.NewRows([]string{"input_id"}))
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."session".*id = \$1.*user_id = \$2.*project_id = \$3.*status = \$4.*archived_at IS NULL.*LIMIT \$5`).
		WithArgs(plan.SessionID, plan.UserID, plan.ProjectID, string(session.StatusActive), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(plan.SessionID))
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."session_sequence_counter".*session_id = \$1.*LIMIT \$2 FOR UPDATE`).
		WithArgs(plan.SessionID, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"session_id", "last_message_seq", "last_input_enqueue_seq", "updated_at",
		}).AddRow(plan.SessionID, int64(2), int64(2), now))
	mock.ExpectQuery(`(?s)SELECT "id" FROM "agent"\."session_input".*source_type <> \$2.*status NOT IN \(\$3,\$4\).*LIMIT \$5`).
		WithArgs(plan.SessionID, string(session.InputSourceTypeCreationSpecPreview),
			string(session.InputStatusResolved), string(session.InputStatusDead), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec(`UPDATE "agent"\."session_sequence_counter"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO "agent"\."session_message"`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "agent"\."session_input"`).
		WithArgs(
			plan.InputID, plan.SessionID, string(session.InputSourceTypeCreationSpecPreview), plan.IdempotencyKey,
			plan.MessageID, string(session.InputStatusPending), int64(3), 0, now,
			nil, nil, int64(0), now, now,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "agent"\."creation_spec_preview_run"`).
		WithArgs(
			plan.InputID, plan.RequestID, plan.IdempotencyKey, plan.RequestDigest,
			plan.SessionID, plan.UserID, plan.ProjectID, plan.MessageID, plan.TurnID, plan.RunID,
			plan.ToolCallID, plan.BusinessCommandID, plan.TerminalEventID, plan.PromptVersion,
			plan.ValidatorVersion, now, now,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`INSERT INTO "agent"\."creation_spec_preview_tool_receipt"`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."session_event_counter".*session_id = \$1.*LIMIT \$2 FOR UPDATE`).
		WithArgs(plan.SessionID, 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"session_id", "last_seq", "min_available_seq", "updated_at",
		}).AddRow(plan.SessionID, int64(7), int64(1), now))
	mock.ExpectExec(`INSERT INTO "agent"\."session_event_log"`).
		WithArgs(
			plan.EventID, plan.SessionID, int64(8), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), plan.IdempotencyKey, sqlmock.AnyArg(), sqlmock.AnyArg(),
			plan.InputID, sqlmock.AnyArg(), sqlmock.AnyArg(), now,
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE "agent"\."session_event_counter"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repository.Enqueue(context.Background(), plan)
	if err != nil {
		t.Fatalf("合法 Preview 入队失败: %v", err)
	}
	if result.SessionID != plan.SessionID || result.InputID != plan.InputID || result.Status != "pending" {
		t.Fatalf("Preview 入队替换了稳定 InputID 或回执: %+v", result)
	}
}

// TestPreviewRepositoryRefusesDeadLetterAfterBusinessBoundary 验证 prepared/unknown 可能已提交 Business；runtime dead-letter 绝不能覆盖该开放回执。
func TestPreviewRepositoryRefusesDeadLetterAfterBusinessBoundary(t *testing.T) {
	for _, stage := range []string{"business_prepared", "business_unknown"} {
		t.Run(stage, func(t *testing.T) {
			repository, mock := newCreationSpecPreviewRepositoryMock(t)
			claim := previewRepoTestClaim()
			now := time.Date(2026, 7, 16, 13, 30, 0, 0, time.UTC)

			mock.ExpectBegin()
			mock.ExpectQuery(`(?s)SELECT run\.session_id.*WHERE run\.tool_call_id = \$1.*FOR UPDATE OF input_record, lane`).
				WithArgs(previewRepoTestToolCallID).
				WillReturnRows(sqlmock.NewRows([]string{
					"session_id", "input_id", "tool_call_id", "input_status", "input_owner", "input_fence",
					"input_lease_valid", "lane_owner", "lane_fence", "lane_lease_valid",
				}).AddRow(
					previewRepoTestSessionID, previewRepoTestInputID, previewRepoTestToolCallID,
					"running", "preview-owner", int64(17), true, "preview-owner", int64(17), true,
				))
			mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."creation_spec_preview_tool_receipt".*tool_call_id = \$1.*LIMIT \$2 FOR UPDATE`).
				WithArgs(previewRepoTestToolCallID, 1).
				WillReturnRows(sqlmock.NewRows(previewRepoToolReceiptColumns()).AddRow(
					previewRepoTestToolCallID, previewRepoTestDigest, stage, previewRepoTestBusinessCmdID,
					previewRepoTestDigest, previewRepoTestDigest, nil, nil, nil, nil, now, now,
				))
			mock.ExpectRollback()

			result := plancreationspec.Result{
				Status: "failed", ResultCode: plancreationspec.ResultCodeRuntimeProcessingFailed,
				ReceiptRef: plancreationspec.ReceiptRef{
					ToolCallID: previewRepoTestToolCallID, BusinessCommandID: previewRepoTestBusinessCmdID,
				},
				Summary: "创作规格预览处理失败，请重新提交。", Retryable: false,
			}
			err := repository.FreezeExecutionFailure(context.Background(), claim, result)
			if !errors.Is(err, previewruntime.ErrIdempotencyConflict) {
				t.Fatalf("%s 被 dead-letter 覆盖: err=%v", stage, err)
			}
		})
	}
}

// TestPreviewRepositoryRejectsExpiredFenceBeforeReceiptWrite 验证 lease 已过期时，Tool Result 冻结在读取回执前即失败关闭。
func TestPreviewRepositoryRejectsExpiredFenceBeforeReceiptWrite(t *testing.T) {
	repository, mock := newCreationSpecPreviewRepositoryMock(t)
	claim := previewRepoTestClaim()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT run\.session_id.*input_record\.lease_until > CURRENT_TIMESTAMP.*lane\.lease_until > CURRENT_TIMESTAMP.*WHERE run\.tool_call_id = \$1.*FOR UPDATE OF input_record, lane`).
		WithArgs(previewRepoTestToolCallID).
		WillReturnRows(sqlmock.NewRows([]string{
			"session_id", "input_id", "tool_call_id", "input_status", "input_owner", "input_fence",
			"input_lease_valid", "lane_owner", "lane_fence", "lane_lease_valid",
		}).AddRow(
			previewRepoTestSessionID, previewRepoTestInputID, previewRepoTestToolCallID,
			"running", "preview-owner", int64(17), false, "preview-owner", int64(17), false,
		))
	mock.ExpectRollback()

	result := plancreationspec.Result{
		Status: "failed", ResultCode: plancreationspec.ResultCodeRuntimeProcessingFailed,
		ReceiptRef: plancreationspec.ReceiptRef{
			ToolCallID: previewRepoTestToolCallID, BusinessCommandID: previewRepoTestBusinessCmdID,
		},
		Summary: "创作规格预览处理失败，请重新提交。", Retryable: false,
	}
	err := repository.FreezeExecutionFailure(context.Background(), claim, result)
	if !errors.Is(err, previewruntime.ErrFenceLost) {
		t.Fatalf("过期 fence 仍可写 Tool Receipt: err=%v", err)
	}
}

// TestPreviewRepositoryRenewLeaseUsesDatabaseClock 锁定 lease/fence 的唯一时间真源为 PostgreSQL。
// 应用 now 即使严重漂移也不得参与过期判定或 lease_until 写入。
func TestPreviewRepositoryRenewLeaseUsesDatabaseClock(t *testing.T) {
	repository, mock := newCreationSpecPreviewRepositoryMock(t)
	claim := previewRepoTestClaim()
	leaseDuration := 30 * time.Second
	appClockDrift := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE "agent"\."session_input" SET .*"lease_until"=CURRENT_TIMESTAMP \+ \(\$1 \* INTERVAL '1 microsecond'\).*"updated_at"=CURRENT_TIMESTAMP.*lease_until > CURRENT_TIMESTAMP`).
		WithArgs(leaseDuration.Microseconds(), claim.InputID, claim.SessionID, claim.Owner, claim.FenceToken,
			string(session.InputStatusClaimed), string(session.InputStatusRunning)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE "agent"\."session_runtime_lease" SET .*"lease_until"=CURRENT_TIMESTAMP \+ \(\$1 \* INTERVAL '1 microsecond'\).*"updated_at"=CURRENT_TIMESTAMP.*lease_until > CURRENT_TIMESTAMP`).
		WithArgs(leaseDuration.Microseconds(), claim.SessionID, claim.Owner, claim.FenceToken).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repository.RenewLease(context.Background(), claim, appClockDrift, leaseDuration); err != nil {
		t.Fatalf("数据库时钟续租失败: %v", err)
	}
}

// TestPreviewRepositoryPrepareAndRestartReplayDurableCommand 验证 Prepare 先加密完整命令，重启后用新 Owner/Fence 重建且不重跑模型。
func TestPreviewRepositoryPrepareAndRestartReplayDurableCommand(t *testing.T) {
	protector := &previewRepoRoundTripProtector{}
	repository, mock := newCreationSpecPreviewRepositoryMockWithProtector(t, protector, 2)
	claim := previewRepoTestClaim()
	trusted := trustedFromClaim(claim)
	command := previewRepoTestCommand(t, trusted)
	now := time.Date(2026, 7, 16, 15, 0, 0, 0, time.UTC)

	mock.ExpectBegin()
	expectPreviewActiveFence(mock, trusted.Owner, trusted.FenceToken)
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."creation_spec_preview_tool_receipt".*tool_call_id = \$1.*LIMIT \$2 FOR UPDATE`).
		WithArgs(trusted.ToolCallID, 1).
		WillReturnRows(sqlmock.NewRows(previewRepoToolReceiptColumnsV2()).AddRow(
			trusted.ToolCallID, previewRepoTestDigest, "pending", trusted.BusinessCommandID,
			nil, nil, nil, nil, nil, 0, nil, nil, nil, nil, nil, nil, nil, now, now,
		))
	mock.ExpectExec(`UPDATE "agent"\."creation_spec_preview_tool_receipt" SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repository.PrepareCommand(context.Background(), command); err != nil {
		t.Fatalf("Prepare durable command 失败: %v", err)
	}
	if protector.protectCalls != 1 || len(protector.plaintext) == 0 ||
		strings.Contains(string(protector.plaintext), trusted.Owner) || strings.Contains(string(protector.plaintext), "fence_token") {
		t.Fatalf("Prepare 未加密完整稳定命令或泄露易变 Lease: %+v", protector)
	}
	payloadDigestBytes := sha256.Sum256(protector.plaintext)
	payloadDigest := hex.EncodeToString(payloadDigestBytes[:])
	contentDigest, err := plancreationspec.ContentDigest(command.Content)
	if err != nil {
		t.Fatalf("计算 Content digest 失败: %v", err)
	}

	// 模拟进程重启与更高 Fence：持久密文不变，只有当前并发身份变化。
	restarted := trusted
	restarted.Owner = "preview-owner-after-restart"
	restarted.FenceToken++
	restartedRepository := &CreationSpecPreviewRepository{
		db: repository.db, protector: protector, maxBusinessResends: 2,
	}
	mock.ExpectBegin()
	expectPreviewActiveFence(mock, restarted.Owner, restarted.FenceToken)
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."creation_spec_preview_tool_receipt".*tool_call_id = \$1.*LIMIT \$2 FOR UPDATE`).
		WithArgs(restarted.ToolCallID, 1).
		WillReturnRows(sqlmock.NewRows(previewRepoToolReceiptColumnsV2()).AddRow(
			restarted.ToolCallID, previewRepoTestDigest, "business_unknown", restarted.BusinessCommandID,
			command.RequestDigest, contentDigest, protector.ciphertext, protector.keyVersion, payloadDigest,
			0, 2, nil, nil, nil, nil, nil, nil, now, now,
		))
	mock.ExpectCommit()

	recovery, err := restartedRepository.ReplayRecovery(context.Background(), restarted)
	if err != nil || recovery == nil {
		t.Fatalf("重启恢复 durable command 失败: recovery=%+v err=%v", recovery, err)
	}
	if protector.openCalls != 1 || recovery.Command.TrustedContext.Owner != restarted.Owner ||
		recovery.Command.TrustedContext.FenceToken != restarted.FenceToken ||
		recovery.Command.RequestDigest != command.RequestDigest || recovery.ResendLimit != 2 {
		t.Fatalf("重启恢复未使用当前 Fence 或稳定命令漂移: %+v", recovery)
	}
}

// TestPreviewRepositoryResendBudgetUsesFenceLockedCAS 验证重发预算先在 Receipt 行锁下预留，耗尽时持久化可观察阶段。
func TestPreviewRepositoryResendBudgetUsesFenceLockedCAS(t *testing.T) {
	protector := &previewRepoRoundTripProtector{}
	repository, mock := newCreationSpecPreviewRepositoryMockWithProtector(t, protector, 1)
	trusted := trustedFromClaim(previewRepoTestClaim())
	command := previewRepoTestCommand(t, trusted)
	plaintext, payloadDigest, contentDigest, err := plancreationspec.EncodeDurableDraftCommand(command)
	if err != nil {
		t.Fatalf("编码预算测试命令失败: %v", err)
	}
	protector.plaintext = plaintext
	protector.ciphertext = append([]byte("protected:"), plaintext...)
	protector.keyVersion = "test-key-v1"
	now := time.Date(2026, 7, 16, 15, 30, 0, 0, time.UTC)
	recovery := plancreationspec.RecoveryDeferred{
		ToolCallID: trusted.ToolCallID, BusinessCommandID: trusted.BusinessCommandID,
		RequestDigest: command.RequestDigest, ContentDigest: contentDigest, Command: command,
	}

	mock.ExpectBegin()
	expectPreviewActiveFence(mock, trusted.Owner, trusted.FenceToken)
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."creation_spec_preview_tool_receipt".*FOR UPDATE`).
		WithArgs(trusted.ToolCallID, 1).
		WillReturnRows(sqlmock.NewRows(previewRepoToolReceiptColumnsV2()).AddRow(
			trusted.ToolCallID, previewRepoTestDigest, "business_unknown", trusted.BusinessCommandID,
			command.RequestDigest, contentDigest, protector.ciphertext, protector.keyVersion, payloadDigest,
			0, 1, nil, nil, nil, nil, nil, nil, now, now,
		))
	mock.ExpectExec(`(?s)UPDATE "agent"\."creation_spec_preview_tool_receipt" SET .*"business_resend_attempts".*business_resend_attempts = \$`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	reservedRecovery, reserved, err := repository.ReserveCommandResend(context.Background(), trusted, recovery)
	if err != nil || !reserved || reservedRecovery.ResendAttempts != 1 || reservedRecovery.ResendLimit != 1 {
		t.Fatalf("原子预留重发预算失败: recovery=%+v reserved=%t err=%v", reservedRecovery, reserved, err)
	}

	lastResendAt := now
	mock.ExpectBegin()
	expectPreviewActiveFence(mock, trusted.Owner, trusted.FenceToken)
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."creation_spec_preview_tool_receipt".*FOR UPDATE`).
		WithArgs(trusted.ToolCallID, 1).
		WillReturnRows(sqlmock.NewRows(previewRepoToolReceiptColumnsV2()).AddRow(
			trusted.ToolCallID, previewRepoTestDigest, "business_unknown", trusted.BusinessCommandID,
			command.RequestDigest, contentDigest, protector.ciphertext, protector.keyVersion, payloadDigest,
			1, 1, lastResendAt, nil, nil, nil, nil, nil, now, now,
		))
	mock.ExpectExec(`(?s)UPDATE "agent"\."creation_spec_preview_tool_receipt" SET .*"business_resend_exhausted_at".*"stage"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	exhausted, reserved, err := repository.ReserveCommandResend(context.Background(), trusted, reservedRecovery)
	if err != nil || reserved || !exhausted.ResendExhausted || exhausted.ResendAttempts != 1 {
		t.Fatalf("预算耗尽阶段未原子冻结: recovery=%+v reserved=%t err=%v", exhausted, reserved, err)
	}
}

// TestPreviewMarkRecoveryRequiresAuthoritativeExhaustion 验证次数达到上限本身不等于权威 not_found；
// Query 技术失败时必须保持 query-only，只有 Graph 显式确认耗尽后才冻结停止 Claim 的阶段。
func TestPreviewMarkRecoveryRequiresAuthoritativeExhaustion(t *testing.T) {
	repository, mock := newCreationSpecPreviewRepositoryMockWithProtector(t, &previewRepoRoundTripProtector{}, 3)
	trusted := trustedFromClaim(previewRepoTestClaim())
	command := previewRepoTestCommand(t, trusted)
	encoded, payloadDigest, contentDigest, err := plancreationspec.EncodeDurableDraftCommand(command)
	if err != nil {
		t.Fatalf("编码 MarkRecovery 测试命令失败: %v", err)
	}
	now := time.Date(2026, 7, 16, 15, 45, 0, 0, time.UTC)
	lastResendAt := now.Add(-time.Second)
	recovery := plancreationspec.RecoveryDeferred{
		ToolCallID: trusted.ToolCallID, BusinessCommandID: trusted.BusinessCommandID,
		RequestDigest: command.RequestDigest, ContentDigest: contentDigest, Command: command,
		ResendAttempts: 3, ResendLimit: 3,
	}
	row := func() *sqlmock.Rows {
		return sqlmock.NewRows(previewRepoToolReceiptColumnsV2()).AddRow(
			trusted.ToolCallID, previewRepoTestDigest, "business_unknown", trusted.BusinessCommandID,
			command.RequestDigest, contentDigest, append([]byte("protected:"), encoded...), "test-key-v1", payloadDigest,
			3, 3, lastResendAt, nil, nil, nil, nil, nil, now, now,
		)
	}

	mock.ExpectBegin()
	expectPreviewActiveFence(mock, trusted.Owner, trusted.FenceToken)
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."creation_spec_preview_tool_receipt".*FOR UPDATE`).
		WithArgs(trusted.ToolCallID, 1).WillReturnRows(row())
	mock.ExpectCommit()
	if err := repository.MarkRecovery(context.Background(), trusted, recovery); err != nil {
		t.Fatalf("Query 技术失败后的 recovery 被错误耗尽: %v", err)
	}

	recovery.ResendExhausted = true
	mock.ExpectBegin()
	expectPreviewActiveFence(mock, trusted.Owner, trusted.FenceToken)
	mock.ExpectQuery(`(?s)SELECT \* FROM "agent"\."creation_spec_preview_tool_receipt".*FOR UPDATE`).
		WithArgs(trusted.ToolCallID, 1).WillReturnRows(row())
	mock.ExpectExec(`(?s)UPDATE "agent"\."creation_spec_preview_tool_receipt" SET .*"business_resend_exhausted_at".*"stage"`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := repository.MarkRecovery(context.Background(), trusted, recovery); err != nil {
		t.Fatalf("权威 not_found 后未冻结 recovery exhausted: %v", err)
	}
}

// TestPreviewClaimExcludesExhaustedReceiptButKeepsHOLGuard 验证耗尽 Receipt 不再被 Claim，而 recovery_pending Input 仍阻塞后序。
func TestPreviewClaimExcludesExhaustedReceiptButKeepsHOLGuard(t *testing.T) {
	repository, mock := newCreationSpecPreviewRepositoryMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)JOIN agent\.creation_spec_preview_tool_receipt AS receipt.*receipt\.stage <> 'business_resend_exhausted'.*NOT EXISTS \(.*earlier\.enqueue_seq < input_record\.enqueue_seq.*earlier\.status NOT IN \('resolved', 'dead'\)`).
		WillReturnRows(sqlmock.NewRows([]string{"input_id"}))
	mock.ExpectCommit()

	claim, err := repository.ClaimNext(context.Background(), "preview-owner", time.Now().UTC(), 30*time.Second)
	if err != nil || claim != nil {
		t.Fatalf("耗尽 Receipt 被错误 Claim: claim=%+v err=%v", claim, err)
	}
}

type previewRepoTestProtector struct{}

func (previewRepoTestProtector) Protect(_ context.Context, plaintext []byte) (session.ProtectedContent, error) {
	return session.ProtectedContent{Ciphertext: append([]byte("protected:"), plaintext...), KeyVersion: "test-key-v1"}, nil
}

func (previewRepoTestProtector) Open(context.Context, session.ProtectedContent, string) ([]byte, error) {
	return nil, errors.New("unexpected Open")
}

type previewRepoRoundTripProtector struct {
	plaintext    []byte
	ciphertext   []byte
	keyVersion   string
	protectCalls int
	openCalls    int
}

func (p *previewRepoRoundTripProtector) Protect(_ context.Context, plaintext []byte) (session.ProtectedContent, error) {
	p.protectCalls++
	p.plaintext = append([]byte(nil), plaintext...)
	p.ciphertext = append([]byte("protected:"), plaintext...)
	p.keyVersion = "test-key-v1"
	return session.ProtectedContent{Ciphertext: append([]byte(nil), p.ciphertext...), KeyVersion: p.keyVersion}, nil
}

func (p *previewRepoRoundTripProtector) Open(_ context.Context, protected session.ProtectedContent, digest string) ([]byte, error) {
	p.openCalls++
	if protected.KeyVersion != p.keyVersion || string(protected.Ciphertext) != string(p.ciphertext) {
		return nil, errors.New("protected command mismatch")
	}
	hash := sha256.Sum256(p.plaintext)
	if hex.EncodeToString(hash[:]) != digest {
		return nil, errors.New("protected command digest mismatch")
	}
	return append([]byte(nil), p.plaintext...), nil
}

func newCreationSpecPreviewRepositoryMock(t *testing.T) (*CreationSpecPreviewRepository, sqlmock.Sqlmock) {
	return newCreationSpecPreviewRepositoryMockWithProtector(t, previewRepoTestProtector{}, 3)
}

func newCreationSpecPreviewRepositoryMockWithProtector(
	t *testing.T,
	protector previewContentProtector,
	maxBusinessResends int,
) (*CreationSpecPreviewRepository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("创建 Preview SQL Mock 失败: %v", err)
	}
	t.Cleanup(func() {
		if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
			t.Errorf("Preview SQL 不符合 fence/receipt 契约: %v", expectationErr)
		}
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("创建 Preview GORM Mock 失败: %v", err)
	}
	return &CreationSpecPreviewRepository{
		db: db, protector: protector, maxBusinessResends: maxBusinessResends,
	}, mock
}

func expectPreviewActiveFence(mock sqlmock.Sqlmock, owner string, fence int64) {
	mock.ExpectQuery(`(?s)SELECT run\.session_id.*WHERE run\.tool_call_id = \$1.*FOR UPDATE OF input_record, lane`).
		WithArgs(previewRepoTestToolCallID).
		WillReturnRows(sqlmock.NewRows([]string{
			"session_id", "input_id", "tool_call_id", "input_status", "input_owner", "input_fence",
			"input_lease_valid", "lane_owner", "lane_fence", "lane_lease_valid",
		}).AddRow(
			previewRepoTestSessionID, previewRepoTestInputID, previewRepoTestToolCallID,
			"running", owner, fence, true, owner, fence, true,
		))
}

func previewRepoTestCommand(t *testing.T, trusted plancreationspec.TrustedContext) plancreationspec.DraftCommand {
	t.Helper()
	command := plancreationspec.DraftCommand{
		TrustedContext: trusted,
		DomainContext:  plancreationspec.DomainContext{ProjectID: trusted.ProjectID, ProjectVersion: 3},
		Content: plancreationspec.Content{
			Title: "品牌短片创作规格", Goal: "制作一支品牌发布短片", DeliverableType: "video",
			Audience: "潜在客户", Locale: "zh-CN",
			Phases: []plancreationspec.Phase{{
				Key: "phase_1", Title: "规划", Objective: "冻结叙事结构", Output: "可执行方案",
			}},
			Constraints: []string{"时长不超过六十秒"}, AcceptanceCriteria: []string{"成片时长不超过六十秒"},
		},
	}
	digest, err := plancreationspec.SaveRequestDigest(command)
	if err != nil {
		t.Fatalf("计算 Preview repository 命令摘要失败: %v", err)
	}
	command.RequestDigest = digest
	return command
}

func previewRepoTestClaim() previewruntime.Claim {
	return previewruntime.Claim{
		Owner: "preview-owner", RequestID: previewRepoTestRequestID, UserID: previewRepoTestUserID,
		ProjectID: previewRepoTestProjectID, SessionID: previewRepoTestSessionID, InputID: previewRepoTestInputID,
		TurnID: previewRepoTestTurnID, RunID: previewRepoTestRunID, ToolCallID: previewRepoTestToolCallID,
		BusinessCommandID: previewRepoTestBusinessCmdID, TerminalEventID: previewRepoTestTerminalEventID,
		PromptVersion: plancreationspec.PromptVersion, ValidatorVersion: plancreationspec.ValidatorVersion,
		FenceToken: 17, Attempts: 3,
	}
}

func previewRepoTestEnqueuePlan(t *testing.T) previewruntime.EnqueuePlan {
	t.Helper()
	now := time.Date(2026, 7, 16, 14, 0, 0, 0, time.UTC)
	ciphertextAndTag := append([]byte("preview-repository-test:"), make([]byte, 16)...)
	envelope, err := session.BuildEnvelopeV1(
		session.EnvelopeAlgorithmAES256GCM,
		make([]byte, 12),
		ciphertextAndTag,
	)
	if err != nil {
		t.Fatalf("构造 Preview 测试 Envelope 失败: %v", err)
	}
	return previewruntime.EnqueuePlan{
		RequestID: previewRepoTestRequestID, IdempotencyKey: previewRepoTestIdempotencyKey,
		RequestDigest: previewRepoTestDigest, UserID: previewRepoTestUserID,
		ProjectID: previewRepoTestProjectID, SessionID: previewRepoTestSessionID,
		MessageID: previewRepoTestMessageID, InputID: previewRepoTestInputID,
		TurnID: previewRepoTestTurnID, RunID: previewRepoTestRunID,
		ToolCallID: previewRepoTestToolCallID, BusinessCommandID: previewRepoTestBusinessCmdID,
		EventID: previewRepoTestEventID, TerminalEventID: previewRepoTestTerminalEventID,
		PromptVersion: plancreationspec.PromptVersion, ValidatorVersion: plancreationspec.ValidatorVersion,
		Content: session.ProtectedContent{Ciphertext: envelope, KeyVersion: "preview-test-key-v1"}, CreatedAt: now,
	}
}

func previewRepoToolReceiptColumns() []string {
	return []string{
		"tool_call_id", "request_digest", "stage", "business_command_id", "business_request_digest",
		"business_content_digest", "result_ciphertext", "result_key_version", "result_digest", "error_code",
		"created_at", "updated_at",
	}
}

func previewRepoToolReceiptColumnsV2() []string {
	return []string{
		"tool_call_id", "request_digest", "stage", "business_command_id", "business_request_digest",
		"business_content_digest", "business_command_ciphertext", "business_command_key_version",
		"business_command_payload_digest", "business_resend_attempts", "business_resend_limit",
		"business_last_resend_at", "business_resend_exhausted_at", "result_ciphertext",
		"result_key_version", "result_digest", "error_code", "created_at", "updated_at",
	}
}

var _ previewContentProtector = previewRepoTestProtector{}
var _ previewContentProtector = (*previewRepoRoundTripProtector)(nil)
