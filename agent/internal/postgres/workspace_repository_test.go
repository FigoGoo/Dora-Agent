package postgres

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	workspaceTestSessionID  = "019f0000-0000-7000-8000-000000000005"
	workspaceTestProjectID  = "019f0000-0000-7000-8000-000000000004"
	workspaceTestUserID     = "019f0000-0000-7000-8000-000000000002"
	workspaceTestInputID    = "019f0000-0000-7000-8000-000000000007"
	workspaceTestTurnID     = "019f0000-0000-7000-8000-000000000009"
	workspaceTestRunID      = "019f0000-0000-7000-8000-000000000010"
	workspaceTestToolCallID = "019f0000-0000-7000-8000-000000000011"
)

// TestWorkspaceRepositoryLoadSnapshotUsesFixedThreeQueries 验证一次 Snapshot 只执行固定 Session JOIN、Message、Input 三次查询。
func TestWorkspaceRepositoryLoadSnapshotUsesFixedThreeQueries(t *testing.T) {
	repository, mock := newWorkspaceRepositoryMock(t)
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT session_record\.id.*FROM agent\.session AS session_record.*JOIN agent\.session_skill_snapshot.*LEFT JOIN agent\.session_user_message_output_projection.*LEFT JOIN LATERAL.*agent\.analyze_materials_preview_projection.*LEFT JOIN LATERAL.*agent\.session_event_log.*JOIN agent\.plan_storyboard_preview_turn_context.*plan_storyboard\.preview\.runtime_failed.*ORDER BY input_record\.enqueue_seq DESC.*WHERE session_record\.id = \$1 AND session_record\.user_id = \$2.*LIMIT 1`).
		WithArgs(workspaceTestSessionID, workspaceTestUserID).
		WillReturnRows(sqlmock.NewRows(workspaceSessionColumns()).AddRow(
			workspaceTestSessionID, workspaceTestProjectID, workspaceTestUserID, "active", 1, now, now, 3, 1,
			workspaceTestSessionID, workspaceTestInputID, 1, workspaceTestTurnID, workspaceTestRunID,
			"session.turn.direct_response.card.v1", "completed",
			`{"schema_version":"session.turn.direct_response.card.v1","turn_id":"`+workspaceTestTurnID+
				`","run_id":"`+workspaceTestRunID+`","input_id":"`+workspaceTestInputID+
				`","status":"completed","message_code":"creation_request_received","summary":"已收到你的创作需求。你可以继续打开工具箱选择下一步流程。","available_actions":["open_toolbox"]}`,
			1, now,
			workspaceTestSessionID, workspaceTestInputID, 1, workspaceTestTurnID, workspaceTestRunID,
			workspaceTestToolCallID, "analyze_materials.preview.card.v1", "runtime_failed", "failed",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			`{"schema_version":"analyze_materials.preview.card.v1","input_id":"`+workspaceTestInputID+
				`","turn_id":"`+workspaceTestTurnID+`","run_id":"`+workspaceTestRunID+
				`","tool_call_id":"`+workspaceTestToolCallID+`","status":"failed","result_code":"MODEL_TEMPORARILY_UNAVAILABLE","failure_kind":"runtime","summary":"素材分析暂时不可用，请稍后重试。","retryable":true}`,
			1, now,
			workspaceTestSessionID, workspaceTestInputID, 1, workspaceTestTurnID, workspaceTestRunID,
			workspaceTestToolCallID, "plan_storyboard.preview.runtime_failed",
			`{"schema_version":"storyboard.preview.card.v1","input_id":"`+workspaceTestInputID+
				`","turn_id":"`+workspaceTestTurnID+`","run_id":"`+workspaceTestRunID+
				`","tool_call_id":"`+workspaceTestToolCallID+`","status":"failed","result_code":"PLAN_STORYBOARD_RUNTIME_FAILED","updated_at":"2026-07-14T09:00:00Z","failure_kind":"runtime","summary":"故事板规划运行时暂时无法完成","retryable":false}`,
			1, now,
		))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_message" WHERE session_id = $1 ORDER BY message_seq ASC, id ASC LIMIT $2`)).
		WithArgs(workspaceTestSessionID, 2).
		WillReturnRows(sqlmock.NewRows(workspaceMessageColumns()).AddRow(
			"019f0000-0000-7000-8000-000000000006", workspaceTestSessionID, 1, "user",
			[]byte("encrypted"), "content-v1", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"ensure_project_session", "019f0000-0000-7000-8000-000000000001", now,
		))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_input" WHERE session_id = $1 ORDER BY enqueue_seq ASC, id ASC LIMIT $2`)).
		WithArgs(workspaceTestSessionID, 2).
		WillReturnRows(sqlmock.NewRows(workspaceInputColumns()).AddRow(
			workspaceTestInputID, workspaceTestSessionID, "user_message",
			"019f0000-0000-7000-8000-000000000001", "019f0000-0000-7000-8000-000000000006",
			"resolved", 1, 0, now, nil, nil, 0, now, now,
		))
	mock.ExpectCommit()

	snapshot, err := repository.LoadSnapshot(context.Background(), workspace.Identity{
		SessionID: workspaceTestSessionID, ProjectID: workspaceTestProjectID, UserID: workspaceTestUserID,
	}, workspace.SnapshotLimits{MaxMessages: 1, MaxInputs: 1})
	if err != nil || len(snapshot.Messages) != 1 || len(snapshot.Inputs) != 1 || snapshot.EventHighWatermark != 3 ||
		snapshot.LatestTurnOutput == nil || snapshot.LatestTurnOutput.TurnID != workspaceTestTurnID ||
		snapshot.AnalyzeMaterialsPreview == nil || snapshot.AnalyzeMaterialsPreview.ToolCallID != workspaceTestToolCallID ||
		snapshot.PlanStoryboardPreview == nil || snapshot.PlanStoryboardPreview.EventType != "plan_storyboard.preview.runtime_failed" ||
		snapshot.PlanStoryboardPreview.TurnID != workspaceTestTurnID || snapshot.PlanStoryboardPreview.RunID != workspaceTestRunID ||
		snapshot.PlanStoryboardPreview.ToolCallID != workspaceTestToolCallID {
		t.Fatalf("固定三查询 Snapshot=%+v err=%v", snapshot, err)
	}
}

// TestWorkspaceRepositoryLoadSnapshotDetectsLimitPlusOne 验证 Message 第 max+1 条会使只读事务回滚并返回完整快照超界。
func TestWorkspaceRepositoryLoadSnapshotDetectsLimitPlusOne(t *testing.T) {
	repository, mock := newWorkspaceRepositoryMock(t)
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT session_record\.id.*FROM agent\.session AS session_record.*LIMIT 1`).
		WithArgs(workspaceTestSessionID, workspaceTestUserID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "project_id", "user_id", "status", "version", "created_at", "updated_at", "last_seq", "min_available_seq",
		}).AddRow(workspaceTestSessionID, workspaceTestProjectID, workspaceTestUserID, "active", 1, now, now, 2, 1))
	messageRows := sqlmock.NewRows(workspaceMessageColumns())
	for _, id := range []string{"019f0000-0000-7000-8000-000000000006", "019f0000-0000-7000-8000-000000000009"} {
		messageRows.AddRow(
			id, workspaceTestSessionID, 1, "user", []byte("encrypted"), "content-v1",
			"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"ensure_project_session", "019f0000-0000-7000-8000-000000000001", now,
		)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "agent"."session_message" WHERE session_id = $1 ORDER BY message_seq ASC, id ASC LIMIT $2`)).
		WithArgs(workspaceTestSessionID, 2).WillReturnRows(messageRows)
	mock.ExpectRollback()

	_, err := repository.LoadSnapshot(context.Background(), workspace.Identity{
		SessionID: workspaceTestSessionID, ProjectID: workspaceTestProjectID, UserID: workspaceTestUserID,
	}, workspace.SnapshotLimits{MaxMessages: 1, MaxInputs: 1})
	if !errors.Is(err, workspace.ErrSnapshotTooLarge) {
		t.Fatalf("limit+1 错误=%v", err)
	}
}

// TestWorkspaceRepositoryLoadEventBatchUsesBoundedTruthQuery 验证每批先校验三重绑定水位，再执行唯一 seq>cursor 升序 LIMIT 查询。
func TestWorkspaceRepositoryLoadEventBatchUsesBoundedTruthQuery(t *testing.T) {
	repository, mock := newWorkspaceRepositoryMock(t)
	now := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT event_counter\.last_seq.*FROM agent\.session AS session_record.*session_record\.id = \$1.*session_record\.user_id = \$2.*session_record\.project_id = \$3.*LIMIT 1`).
		WithArgs(workspaceTestSessionID, workspaceTestUserID, workspaceTestProjectID).
		WillReturnRows(sqlmock.NewRows([]string{"last_seq", "min_available_seq"}).AddRow(2, 1))
	mock.ExpectQuery(`(?s)SELECT event_record\.event_id.*FROM agent\.session_event_log AS event_record.*LEFT JOIN agent\.plan_storyboard_preview_turn_context AS context_record.*WHERE event_record\.session_id = \$1 AND event_record\.seq > \$2.*ORDER BY event_record\.seq ASC.*LIMIT \$3`).
		WithArgs(workspaceTestSessionID, int64(1), 100).
		WillReturnRows(sqlmock.NewRows(workspaceEventColumns()).AddRow(
			"019f0000-0000-7000-8000-000000000008", workspaceTestSessionID, 2,
			"plan_storyboard.preview.runtime_failed", "session.event.v1", "plan_storyboard_preview",
			"019f0000-0000-7000-8000-000000000001", 1, "plan_storyboard_preview",
			workspaceTestInputID, 1, `{}`, now,
			workspaceTestTurnID, workspaceTestRunID, workspaceTestToolCallID,
		))
	mock.ExpectCommit()

	batch, err := repository.LoadEventBatch(context.Background(), workspace.Identity{
		SessionID: workspaceTestSessionID, ProjectID: workspaceTestProjectID, UserID: workspaceTestUserID,
	}, 1, 100)
	if err != nil || batch.LastSeq != 2 || len(batch.Events) != 1 || batch.Events[0].Seq != 2 ||
		batch.Events[0].PlanTurnID != workspaceTestTurnID || batch.Events[0].PlanRunID != workspaceTestRunID ||
		batch.Events[0].PlanToolCallID != workspaceTestToolCallID {
		t.Fatalf("EventBatch=%+v err=%v", batch, err)
	}
}

// newWorkspaceRepositoryMock 创建禁止真实网络的 GORM PostgreSQL Mock，并严格核对 Query 数量与事务终态。
func newWorkspaceRepositoryMock(t *testing.T) (*WorkspaceRepository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("创建 Workspace SQL Mock 失败: %v", err)
	}
	t.Cleanup(func() {
		if expectationErr := mock.ExpectationsWereMet(); expectationErr != nil {
			t.Errorf("Workspace SQL 不符合固定查询契约: %v", expectationErr)
		}
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("创建 Workspace GORM Mock 失败: %v", err)
	}
	return &WorkspaceRepository{db: db}, mock
}

func workspaceMessageColumns() []string {
	return []string{
		"id", "session_id", "message_seq", "role", "content_ciphertext", "content_key_version",
		"content_digest", "source_kind", "source_id", "created_at",
	}
}

func workspaceInputColumns() []string {
	return []string{
		"id", "session_id", "source_type", "source_id", "message_id", "status", "enqueue_seq", "attempts",
		"available_at", "lease_owner", "lease_until", "fence_token", "created_at", "updated_at",
	}
}

func workspaceEventColumns() []string {
	return []string{
		"event_id", "session_id", "seq", "event_type", "schema_version", "source_kind", "source_id",
		"projection_index", "aggregate_type", "aggregate_id", "aggregate_version", "payload", "created_at",
		"plan_turn_id", "plan_run_id", "plan_tool_call_id",
	}
}

func workspaceSessionColumns() []string {
	return []string{
		"id", "project_id", "user_id", "status", "version", "created_at", "updated_at", "last_seq", "min_available_seq",
		"output_session_id", "output_source_input_id", "output_source_enqueue_seq", "output_turn_id", "output_run_id",
		"output_schema_version", "output_status", "output_payload", "output_projection_version", "output_updated_at",
		"analyze_session_id", "analyze_source_input_id", "analyze_source_enqueue_seq", "analyze_turn_id", "analyze_run_id",
		"analyze_tool_call_id", "analyze_schema_version", "analyze_outcome_kind", "analyze_status", "analyze_result_digest",
		"analyze_payload", "analyze_projection_version", "analyze_created_at",
		"plan_session_id", "plan_source_input_id", "plan_source_enqueue_seq", "plan_turn_id", "plan_run_id",
		"plan_tool_call_id", "plan_event_type", "plan_payload",
		"plan_aggregate_version", "plan_created_at",
	}
}
