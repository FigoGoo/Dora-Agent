package repository_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/boardgraph"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation"
)

func TestBoardGraphRepositoryWithActiveMigration(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_agent_board_graph")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-agent-runtime-contracts/agent")
	testdb.RequireNoForeignKeys(t, db.DB)
	if !testdb.TableExists(t, db.DB, "agent_runs") || !testdb.TableExists(t, db.DB, "graph_plans") || !testdb.TableExists(t, db.DB, "creative_boards") {
		t.Fatal("board graph active migration tables missing")
	}
	if testdb.TableExists(t, db.DB, "credit_holds") {
		t.Fatal("agent database must not contain business credit tables")
	}

	now := time.Date(2026, 7, 1, 10, 30, 0, 0, time.UTC)
	runtime := creation.New(func() time.Time { return now })
	result, err := runtime.ExecuteGenericCreation(t.Context(), creation.GenericCreationInput{
		RunID:                "run_city_tourism_001",
		ProjectID:            "proj_city_001",
		SessionID:            "sess_city_001",
		SpaceID:              "space_001",
		ActorUserID:          "user_001",
		TraceID:              "trace_board_graph",
		Prompt:               "生成一支 30 秒城市文旅宣传短片，风格明亮、真实、有文化质感",
		RouterDecisionDigest: "sha256:" + strings.Repeat("1", 64),
	})
	if err != nil {
		t.Fatalf("execute generic creation: %v", err)
	}

	repo := repository.New(db.DB)
	if err := repo.SaveGenericCreationState(t.Context(), result.GraphTemplate, result.GraphPlan, result.Board, result.Elements, result.Events); err != nil {
		t.Fatalf("save generic creation state: %v", err)
	}
	if err := repo.SaveGenericCreationState(t.Context(), result.GraphTemplate, result.GraphPlan, result.Board, result.Elements, result.Events); err != nil {
		t.Fatalf("save generic creation state idempotently: %v", err)
	}

	run, err := repo.GetAgentRunV1(t.Context(), result.GraphPlan.RunID)
	if err != nil {
		t.Fatalf("get agent run: %v", err)
	}
	if run.Status != foundation.RunStatusWaitingConfirmation || run.CurrentBoardID != result.Board.BoardID || run.CurrentGraphPlanID != result.GraphPlan.GraphPlanID {
		t.Fatalf("unexpected run record: %#v", run)
	}

	plan, err := repo.GetGraphPlanV1(t.Context(), result.GraphPlan.GraphPlanID)
	if err != nil {
		t.Fatalf("get graph plan: %v", err)
	}
	if err := boardgraph.ValidateGraphPlan(plan); err != nil {
		t.Fatalf("graph plan contract invalid: %v", err)
	}

	snapshot, err := repo.GetBoardSnapshotV1(t.Context(), result.Board.BoardID)
	if err != nil {
		t.Fatalf("get board snapshot: %v", err)
	}
	if err := boardgraph.ValidateBoardSnapshot(snapshot); err != nil {
		t.Fatalf("board snapshot contract invalid: %v", err)
	}
	if snapshot.Status != "ready" || snapshot.Version != 1 || len(snapshot.Elements) != len(result.Elements) {
		t.Fatalf("unexpected board snapshot: %#v", snapshot)
	}

	events, err := repo.ListRunEventsV1AfterSeq(t.Context(), result.GraphPlan.RunID, 0, 10)
	if err != nil {
		t.Fatalf("list run events: %v", err)
	}
	if len(events) != 2 || events[0].Seq != 1 || events[0].EventType != boardgraph.EventTypeGraphPlanCreated || events[1].EventType != boardgraph.EventTypeBoardSnapshotUpdated {
		t.Fatalf("unexpected run events: %#v", events)
	}

	approval, err := runtime.ApproveBoard(t.Context(), creation.ApproveBoardInput{
		Board:          result.Board,
		ActorUserID:    "user_001",
		IdempotencyKey: "user_001:" + result.Board.BoardID + ":approve:v1",
		ApprovedAt:     now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("approve board: %v", err)
	}
	if err := repo.ApplyBoardApprovalV1(t.Context(), approval.Patch, approval.Board); err != nil {
		t.Fatalf("apply board approval: %v", err)
	}
	if err := repo.ApplyBoardApprovalV1(t.Context(), approval.Patch, approval.Board); err != nil {
		t.Fatalf("apply board approval idempotently: %v", err)
	}

	approved, err := repo.GetBoardSnapshotV1(t.Context(), result.Board.BoardID)
	if err != nil {
		t.Fatalf("get approved board snapshot: %v", err)
	}
	if approved.Status != "approved" || approved.Version != 2 || approved.LastPatchID == nil || *approved.LastPatchID != approval.Patch.PatchID {
		t.Fatalf("unexpected approved board snapshot: %#v", approved)
	}
	runAfterApproval, err := repo.GetAgentRunV1(t.Context(), result.GraphPlan.RunID)
	if err != nil {
		t.Fatalf("get run after approval: %v", err)
	}
	if runAfterApproval.Status != foundation.RunStatusPlanning {
		t.Fatalf("expected run planning after board approval, got %#v", runAfterApproval)
	}

	stale := approval.Board
	stale.BoardDigest = "sha256:" + strings.Repeat("a", 64)
	if err := repo.ApplyBoardApprovalV1(t.Context(), approval.Patch, stale); !errors.Is(err, repository.ErrBoardVersionConflict) {
		t.Fatalf("expected board version conflict, got %v", err)
	}

	testdb.DownMigrations(t, migrator)
	if count := testdb.CountTables(t, db.DB); count != 0 {
		t.Fatalf("expected migration down to drop tables, got %d", count)
	}
}
