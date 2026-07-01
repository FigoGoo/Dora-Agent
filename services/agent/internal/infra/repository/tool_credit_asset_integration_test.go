package repository_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr3"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
)

func TestPR3AgentToolRepositoryWithActiveMigration(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_agent_pr3")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/agent")
	testdb.RequireNoForeignKeys(t, db.DB)
	if !testdb.TableExists(t, db.DB, "tool_plans") || !testdb.TableExists(t, db.DB, "tool_tasks") {
		t.Fatal("PR-3 active agent migration tables missing")
	}
	if testdb.TableExists(t, db.DB, "credit_holds") || testdb.TableExists(t, db.DB, "generated_assets") {
		t.Fatal("agent database must not contain business credit or asset tables")
	}

	var toolPlanFixture struct {
		Precondition pr3.ToolPlanPrecondition `json:"precondition"`
		ToolPlan     pr3.ToolPlan             `json:"tool_plan"`
	}
	readPR3Fixture(t, "tests/fixtures/contracts/toolplan/city_video_toolplan.json", &toolPlanFixture)
	if err := pr3.ValidateToolPlanForApprovedBoard(toolPlanFixture.Precondition, toolPlanFixture.ToolPlan); err != nil {
		t.Fatalf("tool plan fixture contract: %v", err)
	}

	repo := repository.New(db.DB)
	if err := repo.SaveToolPlanV1(t.Context(), toolPlanFixture.ToolPlan); err != nil {
		t.Fatalf("save tool plan: %v", err)
	}
	if err := repo.SaveToolPlanV1(t.Context(), toolPlanFixture.ToolPlan); err != nil {
		t.Fatalf("save tool plan idempotently: %v", err)
	}
	plan, err := repo.GetToolPlanV1(t.Context(), toolPlanFixture.ToolPlan.ToolPlanID)
	if err != nil {
		t.Fatalf("get tool plan: %v", err)
	}
	if plan.ToolPlanDigest != toolPlanFixture.ToolPlan.ToolPlanDigest || len(plan.Items) != len(toolPlanFixture.ToolPlan.Items) {
		t.Fatalf("unexpected tool plan: %#v", plan)
	}

	var resumeFixture struct {
		ToolTaskBeforeRestart pr3.ToolTask                     `json:"tool_task_before_restart"`
		RedisStreamEvent      pr3.ToolTaskCompletedStreamEvent `json:"redis_stream_event"`
		ToolTaskAfterResume   pr3.ToolTask                     `json:"tool_task_after_resume"`
	}
	readPR3Fixture(t, "tests/fixtures/contracts/tool/provider_async_resume.json", &resumeFixture)
	if err := repo.SaveToolTaskV1(t.Context(), resumeFixture.ToolTaskBeforeRestart); err != nil {
		t.Fatalf("save tool task: %v", err)
	}
	before, err := repo.GetToolTaskV1(t.Context(), resumeFixture.ToolTaskBeforeRestart.ToolTaskID)
	if err != nil {
		t.Fatalf("get tool task before resume: %v", err)
	}
	after, err := repo.ApplyToolTaskCompletedEventV1(t.Context(), resumeFixture.RedisStreamEvent, resumeFixture.ToolTaskAfterResume.UpdatedAt)
	if err != nil {
		t.Fatalf("apply provider completed event: %v", err)
	}
	if err := pr3.ValidateProviderAsyncResume(before, resumeFixture.RedisStreamEvent, after); err != nil {
		t.Fatalf("provider async resume contract: %v", err)
	}
	if after.OutputDigest == nil || *after.OutputDigest != resumeFixture.RedisStreamEvent.OutputDigest || after.Status != "succeeded" {
		t.Fatalf("unexpected resumed task: %#v", after)
	}
	replayed, err := repo.ApplyToolTaskCompletedEventV1(t.Context(), resumeFixture.RedisStreamEvent, resumeFixture.ToolTaskAfterResume.UpdatedAt)
	if err != nil {
		t.Fatalf("apply provider completed event idempotently: %v", err)
	}
	if replayed.Status != "succeeded" || replayed.OutputDigest == nil || *replayed.OutputDigest != *after.OutputDigest {
		t.Fatalf("unexpected idempotent replay task: %#v", replayed)
	}

	testdb.DownMigrations(t, migrator)
	if count := testdb.CountTables(t, db.DB); count != 0 {
		t.Fatalf("expected migration down to drop tables, got %d", count)
	}
}

func readPR3Fixture(t *testing.T, relativePath string, target any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdb.RepoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relativePath, err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", relativePath, err)
	}
}
