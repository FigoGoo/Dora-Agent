package eino

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr2"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation"
)

func TestGenericCreationGraphRunnerExecutesPR2Graph(t *testing.T) {
	runner, err := NewGenericCreationGraphRunner(t.Context(), fixedGenericCreationClock)
	if err != nil {
		t.Fatalf("new generic creation graph runner: %v", err)
	}

	input := creation.GenericCreationInput{
		RunID:                "run_eino_city_001",
		ProjectID:            "proj_city_001",
		SessionID:            "sess_city_001",
		SpaceID:              "space_001",
		ActorUserID:          "user_001",
		TraceID:              "trace_eino_generic_creation",
		Prompt:               "生成一支 30 秒城市文旅宣传短片，风格明亮、真实、有文化质感",
		RouterDecisionDigest: "sha256:" + strings.Repeat("1", 64),
	}
	result, err := runner.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("execute generic creation graph: %v", err)
	}
	if err := pr2.ValidateGenericGraphFixture(result.GenericGraph, result.GraphTemplate, result.GraphPlan); err != nil {
		t.Fatalf("generic graph contract: %v", err)
	}
	if err := pr2.ValidateBoardCreation(result.Board, result.Elements); err != nil {
		t.Fatalf("board creation contract: %v", err)
	}
	if err := pr2.ValidateBoardSnapshot(result.Snapshot); err != nil {
		t.Fatalf("board snapshot contract: %v", err)
	}
	if err := pr1.ValidateAGUISequence(result.Events); err != nil {
		t.Fatalf("ag-ui sequence contract: %v", err)
	}
	if result.GraphTemplate.GraphTemplateID != "gtemplate_generic_creation" || result.GraphTemplate.GraphType != pr2.GraphTypeGenericCreation {
		t.Fatalf("unexpected graph template: %#v", result.GraphTemplate)
	}
	if result.Board.ToolPlanAllowed {
		t.Fatalf("generic creation board must wait for explicit board approval")
	}
}

func TestGenericCreationGraphRunnerMatchesDeterministicRuntime(t *testing.T) {
	input := creation.GenericCreationInput{
		RunID:     "run_eino_repeatable_001",
		ProjectID: "proj_001",
		SessionID: "sess_001",
		Prompt:    "为新品发布生成 Storyboard 草稿",
	}
	runner, err := NewGenericCreationGraphRunner(t.Context(), fixedGenericCreationClock)
	if err != nil {
		t.Fatalf("new generic creation graph runner: %v", err)
	}
	viaGraph, err := runner.Execute(t.Context(), input)
	if err != nil {
		t.Fatalf("execute graph: %v", err)
	}
	directRuntime := creation.New(fixedGenericCreationClock)
	direct, err := directRuntime.ExecuteGenericCreation(t.Context(), input)
	if err != nil {
		t.Fatalf("execute direct runtime: %v", err)
	}
	if viaGraph.GraphPlan.GraphPlanDigest != direct.GraphPlan.GraphPlanDigest || viaGraph.Board.BoardDigest != direct.Board.BoardDigest {
		t.Fatalf("graph runner drifted from deterministic runtime")
	}
}

func TestGenericCreationGraphNodeIDsReturnsCopy(t *testing.T) {
	first := GenericCreationGraphNodeIDs()
	first[0] = "mutated"
	second := GenericCreationGraphNodeIDs()
	expected := []string{"brief_parser", "clarifier", "creative_direction", "board_writer", "skill_recommendation"}
	if !reflect.DeepEqual(second, expected) {
		t.Fatalf("node ids should be immutable copy, got %#v", second)
	}
}

func TestGenericCreationGraphRunnerRejectsMissingPrompt(t *testing.T) {
	runner, err := NewGenericCreationGraphRunner(t.Context(), fixedGenericCreationClock)
	if err != nil {
		t.Fatalf("new generic creation graph runner: %v", err)
	}
	_, err = runner.Execute(t.Context(), creation.GenericCreationInput{
		RunID:     "run_eino_invalid_001",
		ProjectID: "proj_001",
		SessionID: "sess_001",
	})
	if err == nil {
		t.Fatalf("missing prompt must be rejected")
	}
}

func TestGenericCreationGraphRunnerHonorsContextCancel(t *testing.T) {
	runner, err := NewGenericCreationGraphRunner(t.Context(), fixedGenericCreationClock)
	if err != nil {
		t.Fatalf("new generic creation graph runner: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = runner.Execute(ctx, creation.GenericCreationInput{
		RunID:     "run_eino_cancelled_001",
		ProjectID: "proj_001",
		SessionID: "sess_001",
		Prompt:    "生成一支城市文旅短片",
	})
	if err == nil {
		t.Fatalf("cancelled context must stop graph execution")
	}
}

func fixedGenericCreationClock() time.Time {
	return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
}
