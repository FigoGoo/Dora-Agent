package creation

import (
	"context"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/boardgraph"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

func TestExecuteGenericCreationBuildsBoardGraphObjects(t *testing.T) {
	runtime := New(fixedClock)

	result, err := runtime.ExecuteGenericCreation(context.Background(), GenericCreationInput{
		RunID:                "run_city_tourism_001",
		ProjectID:            "proj_city_001",
		SessionID:            "sess_city_001",
		SpaceID:              "space_city_001",
		ActorUserID:          "user_001",
		TraceID:              "trc_city_001",
		Prompt:               "生成一支 30 秒城市文旅宣传短片，风格明亮、真实、有文化质感",
		RouterDecisionDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
	})
	if err != nil {
		t.Fatalf("execute generic creation: %v", err)
	}
	if err := boardgraph.ValidateGenericGraphFixture(result.GenericGraph, result.GraphTemplate, result.GraphPlan); err != nil {
		t.Fatalf("generic graph contract: %v", err)
	}
	if err := boardgraph.ValidateBoardCreation(result.Board, result.Elements); err != nil {
		t.Fatalf("board creation contract: %v", err)
	}
	if err := boardgraph.ValidateBoardSnapshot(result.Snapshot); err != nil {
		t.Fatalf("board snapshot contract: %v", err)
	}
	if err := foundation.ValidateAGUISequence(result.Events); err != nil {
		t.Fatalf("agui sequence contract: %v", err)
	}
	if result.GenericGraph.MarketplaceListingID != nil || result.GenericGraph.UsageFee != 0 {
		t.Fatalf("generic L0 fallback must not enter marketplace or charge usage fee")
	}
	if result.Board.ToolPlanAllowed {
		t.Fatalf("board must not allow ToolPlan before approval")
	}
	if result.Events[0].EventType != boardgraph.EventTypeGraphPlanCreated || result.Events[1].EventType != boardgraph.EventTypeBoardSnapshotUpdated {
		t.Fatalf("unexpected event order: %s, %s", result.Events[0].EventType, result.Events[1].EventType)
	}
}

func TestExecuteGenericCreationIsDeterministic(t *testing.T) {
	runtime := New(fixedClock)
	input := GenericCreationInput{
		RunID:     "run_repeatable_001",
		ProjectID: "proj_001",
		SessionID: "sess_001",
		Prompt:    "为新品发布生成 Storyboard 草稿",
	}

	first, err := runtime.ExecuteGenericCreation(context.Background(), input)
	if err != nil {
		t.Fatalf("first execute: %v", err)
	}
	second, err := runtime.ExecuteGenericCreation(context.Background(), input)
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if first.Board.BoardID != second.Board.BoardID || first.Board.BoardDigest != second.Board.BoardDigest {
		t.Fatalf("board output should be deterministic")
	}
	if first.GraphPlan.GraphPlanID != second.GraphPlan.GraphPlanID || first.GraphPlan.GraphPlanDigest != second.GraphPlan.GraphPlanDigest {
		t.Fatalf("graph output should be deterministic")
	}
}

func TestExecuteGenericCreationRejectsMissingPrompt(t *testing.T) {
	runtime := New(fixedClock)
	_, err := runtime.ExecuteGenericCreation(context.Background(), GenericCreationInput{
		RunID:     "run_001",
		ProjectID: "proj_001",
		SessionID: "sess_001",
	})
	if err == nil {
		t.Fatalf("missing prompt must be rejected")
	}
}

func TestApproveBoardEnablesToolPlanGate(t *testing.T) {
	runtime := New(fixedClock)
	created, err := runtime.ExecuteGenericCreation(context.Background(), GenericCreationInput{
		RunID:     "run_approve_001",
		ProjectID: "proj_001",
		SessionID: "sess_001",
		Prompt:    "生成一支城市文旅短片",
	})
	if err != nil {
		t.Fatalf("execute generic creation: %v", err)
	}

	approved, err := runtime.ApproveBoard(context.Background(), ApproveBoardInput{
		Board:          created.Board,
		ActorUserID:    "user_001",
		IdempotencyKey: "user_001:" + created.Board.BoardID + ":approve:v1",
		ApprovedAt:     fixedClock(),
	})
	if err != nil {
		t.Fatalf("approve board: %v", err)
	}
	if err := boardgraph.ValidateBoardApproval(created.Board, approved.Patch, approved.Board); err != nil {
		t.Fatalf("approval contract: %v", err)
	}
	if approved.Board.Status != "approved" || !approved.Board.ToolPlanAllowed {
		t.Fatalf("approved board must allow ToolPlan")
	}
	if approved.Patch.BaseVersion != 1 || approved.Patch.TargetVersion != 2 {
		t.Fatalf("approval patch should move board from v1 to v2")
	}
}

func TestApproveBoardRequiresIdempotency(t *testing.T) {
	runtime := New(fixedClock)
	created, err := runtime.ExecuteGenericCreation(context.Background(), GenericCreationInput{
		RunID:     "run_no_idempotency_001",
		ProjectID: "proj_001",
		SessionID: "sess_001",
		Prompt:    "生成一支城市文旅短片",
	})
	if err != nil {
		t.Fatalf("execute generic creation: %v", err)
	}
	if _, err := runtime.ApproveBoard(context.Background(), ApproveBoardInput{Board: created.Board, ActorUserID: "user_001"}); err == nil {
		t.Fatalf("approval without idempotency_key must be rejected")
	}
}

func fixedClock() time.Time {
	return time.Date(2026, 7, 1, 2, 0, 0, 0, time.UTC)
}
