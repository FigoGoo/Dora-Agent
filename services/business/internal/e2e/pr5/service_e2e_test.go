package pr5e2e_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr3"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr4"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr5"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"gorm.io/gorm"
)

type pr5ServiceEnv struct {
	agent    *testdb.Database
	business *testdb.Database
	repo     *businesscore.Repository
}

func TestPR5ServiceLevelE2EWithFakeProviderAndPostgres(t *testing.T) {
	env := newPR5ServiceEnv(t)
	validatePR5E2EFixtures(t)
	assertFakeProviderScenariosExecutable(t)

	publish := runCreatorPublish(t, env)
	runCityTourismDefaultSkill(t, env)
	runGenericCreationGraphFallback(t, env)
	runPaidMarketplaceSkillUsage(t, env)
	runEnterprisePinnedInstallUpgrade(t, env)
	runToolPartialFailureRelease(t, env)
	runListingSuspendedGuard(t, env, publish)
	runRefundSettlementReverse(t, env)
	runReplayAfterRestart(t, env)
	assertNoFrozenCreditHolds(t, env)

	downPR5Migrations(t, env)
	if count := testdb.CountTables(t, env.agent.DB); count != 0 {
		t.Fatalf("expected PR-5 agent migration down to drop tables, got %d", count)
	}
	if count := testdb.CountTables(t, env.business.DB); count != 0 {
		t.Fatalf("expected PR-5 business migration down to drop tables, got %d", count)
	}
}

func newPR5ServiceEnv(t *testing.T) *pr5ServiceEnv {
	t.Helper()
	env := &pr5ServiceEnv{
		agent:    testdb.StartPostgres(t, "dora_pr5_agent"),
		business: testdb.StartPostgres(t, "dora_pr5_business"),
	}
	for _, path := range []string{
		"db/migrations/iterations/2026-07-01-agent-runtime-contracts/agent/0001_agent_runtime_board_graph.up.sql",
		"db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/agent/0001_agent_tool_plan_task.up.sql",
	} {
		testdb.ExecSQL(t, env.agent.DB, testdb.MustReadSQL(t, path))
	}
	for _, path := range []string{
		"db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/business/0001_business_credit_asset_tool.up.sql",
		"db/migrations/iterations/2026-07-01-marketplace-contracts/business/0001_skill_marketplace_settlement.up.sql",
	} {
		testdb.ExecSQL(t, env.business.DB, testdb.MustReadSQL(t, path))
	}
	testdb.RequireNoForeignKeys(t, env.agent.DB)
	testdb.RequireNoForeignKeys(t, env.business.DB)
	env.repo = businesscore.New(env.business.DB)
	return env
}

func downPR5Migrations(t *testing.T, env *pr5ServiceEnv) {
	t.Helper()
	for _, path := range []string{
		"db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/agent/0001_agent_tool_plan_task.down.sql",
		"db/migrations/iterations/2026-07-01-agent-runtime-contracts/agent/0001_agent_runtime_board_graph.down.sql",
	} {
		testdb.ExecSQL(t, env.agent.DB, testdb.MustReadSQL(t, path))
	}
	for _, path := range []string{
		"db/migrations/iterations/2026-07-01-marketplace-contracts/business/0001_skill_marketplace_settlement.down.sql",
		"db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/business/0001_business_credit_asset_tool.down.sql",
	} {
		testdb.ExecSQL(t, env.business.DB, testdb.MustReadSQL(t, path))
	}
}

func validatePR5E2EFixtures(t *testing.T) {
	t.Helper()
	suites := make([]pr5.E2ESuite, 0, 3)
	for _, relativePath := range []string{
		"tests/e2e/agent-workspace/scenarios.json",
		"tests/e2e/skill-marketplace/scenarios.json",
		"tests/e2e/admin-governance/scenarios.json",
	} {
		var suite pr5.E2ESuite
		readJSON(t, relativePath, &suite)
		suites = append(suites, suite)
	}
	if err := pr5.ValidateE2ESuiteIndexes(suites); err != nil {
		t.Fatalf("validate PR-5 e2e suite indexes: %v", err)
	}
	for relativePath := range pr5.RequiredE2EFixtures {
		var fixture pr5.E2EFixture
		readJSON(t, filepath.Join("tests/fixtures/e2e", relativePath), &fixture)
		if err := pr5.ValidateE2EFixture(relativePath, fixture); err != nil {
			t.Fatalf("validate PR-5 e2e fixture %s: %v", relativePath, err)
		}
	}
}

func assertFakeProviderScenariosExecutable(t *testing.T) {
	t.Helper()
	var manifest pr5.FakeProviderManifest
	var scenarios pr5.ProviderScenarios
	readJSON(t, "tests/e2e/fake-provider/fake_provider_manifest.json", &manifest)
	readJSON(t, "tests/e2e/fake-provider/provider_scenarios.json", &scenarios)
	if err := pr5.ValidateFakeProviderArtifacts(manifest, scenarios); err != nil {
		t.Fatalf("validate fake provider artifacts: %v", err)
	}
	for _, scenario := range scenarios.Scenarios {
		first, err := pr5.SimulateFakeProviderScenario(scenario)
		if err != nil {
			t.Fatalf("simulate fake provider scenario %s: %v", scenario.CaseID, err)
		}
		second, err := pr5.SimulateFakeProviderScenario(scenario)
		if err != nil {
			t.Fatalf("simulate fake provider replay %s: %v", scenario.CaseID, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("fake provider scenario %s is not idempotent", scenario.CaseID)
		}
	}
}

func runCreatorPublish(t *testing.T, env *pr5ServiceEnv) creatorPublishFixture {
	t.Helper()
	var fixture creatorPublishFixture
	readJSON(t, "tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json", &fixture)
	if err := env.repo.SaveCreatorPublishFlowV1(t.Context(), fixture.SkillPackage, fixture.SkillVersion, fixture.PricingPolicy, fixture.Listing); err != nil {
		t.Fatalf("save creator publish flow: %v", err)
	}
	return fixture
}

func runCityTourismDefaultSkill(t *testing.T, env *pr5ServiceEnv) {
	t.Helper()
	var planFixture toolPlanFixture
	readJSON(t, "tests/fixtures/contracts/toolplan/city_video_toolplan.json", &planFixture)
	if err := pr3.ValidateToolPlanForApprovedBoard(planFixture.Precondition, planFixture.ToolPlan); err != nil {
		t.Fatalf("validate city tool plan: %v", err)
	}
	insertAgentToolPlan(t, env.agent.DB, planFixture.ToolPlan)

	var creditFixture creditCommitFixture
	readJSON(t, "tests/fixtures/contracts/credit/freeze_commit_success.json", &creditFixture)
	frozen, err := env.repo.FreezeToolCreditsV1(
		t.Context(),
		creditFixture.FreezeRequest,
		creditFixture.HoldAfterFreeze.CreditHoldID,
		"trace_pr5_city_credit_commit",
		creditFixture.HoldAfterFreeze.CreatedAt,
	)
	if err != nil {
		t.Fatalf("freeze city tool credits: %v", err)
	}
	committed, err := env.repo.CommitToolCreditsV1(
		t.Context(),
		creditFixture.HoldAfterFreeze.CreditHoldID,
		"cled_pr5_city_video_commit_001",
		"trace_pr5_city_credit_commit",
		*creditFixture.HoldAfterCommit.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("commit city tool credits: %v", err)
	}
	if err := pr3.ValidateFreezeCommitFlow(creditFixture.FreezeRequest, frozen, committed); err != nil {
		t.Fatalf("city credit freeze commit contract: %v", err)
	}

	var assetFixture assetCommitFixture
	readJSON(t, "tests/fixtures/contracts/asset/partial_commit_success.json", &assetFixture)
	assetCommit, err := env.repo.CommitGeneratedAssetsV1(
		t.Context(),
		assetFixture.ToolResult,
		assetFixture.CommitResponse,
		assetFixture.BillingRule,
		"run_city_tourism_001",
		"proj_city_001",
		"commit:ttask_city_video_001:pr5",
		"trace_pr5_city_asset_commit",
		assetFixture.ToolResult.CreatedAt,
	)
	if err != nil {
		t.Fatalf("commit city generated assets: %v", err)
	}
	if err := pr3.ValidatePartialAssetCommit(assetFixture.ToolResult, assetCommit, assetFixture.BillingRule); err != nil {
		t.Fatalf("city partial asset commit contract: %v", err)
	}
	assertRecordCount(t, env.business.DB, "skill_usage_records", 0)
}

func runGenericCreationGraphFallback(t *testing.T, env *pr5ServiceEnv) {
	t.Helper()
	var fixture pr5.E2EFixture
	readJSON(t, "tests/fixtures/e2e/agent-workspace/generic_creation_graph_fallback.json", &fixture)
	if fixture.ExpectedBusinessState["skill_usage_record"] != "not_created" {
		t.Fatalf("generic creation fallback fixture must keep skill usage uncreated")
	}
	assertRecordCount(t, env.business.DB, "skill_usage_records", 0)
	assertRecordCount(t, env.agent.DB, "tool_plans", 1)
}

func runPaidMarketplaceSkillUsage(t *testing.T, env *pr5ServiceEnv) {
	t.Helper()
	var installFixture personalInstallFixture
	readJSON(t, "tests/fixtures/contracts/marketplace/user_install_latest_personal.json", &installFixture)
	installed, err := env.repo.InstallPersonalLatestSkillV1(t.Context(), installFixture.Request, installFixture.Installation)
	if err != nil {
		t.Fatalf("install personal marketplace skill: %v", err)
	}
	if !reflect.DeepEqual(installed, installFixture.Installation) {
		t.Fatalf("unexpected personal installation\nwant: %#v\ngot:  %#v", installFixture.Installation, installed)
	}

	var chargeFixture skillUsageChargeFixture
	readJSON(t, "tests/fixtures/contracts/billing/skill_usage_precreate_confirm_charge.json", &chargeFixture)
	usageAfterCreate, err := env.repo.CreateSkillUsageRecordV1(t.Context(), chargeFixture.UsageAfterCreate, "run_city_tourism_paid_001:listing_city_tourism_creator_001:v1")
	if err != nil {
		t.Fatalf("create marketplace skill usage record: %v", err)
	}
	if usageAfterCreate.UsageStatus != "confirmation_required" || usageAfterCreate.ChargeStatus != "not_frozen" {
		t.Fatalf("skill usage record must be created before charge, got %#v", usageAfterCreate)
	}
	usageAfterCharge, settlement, err := env.repo.CommitSkillUsageAndSettleV1(t.Context(), chargeFixture.UsageAfterCharge, chargeFixture.Settlement)
	if err != nil {
		t.Fatalf("commit marketplace skill usage and settlement: %v", err)
	}
	if err := pr4.ValidateSkillUsagePrecreateConfirmCharge(chargeFixture.Sequence, usageAfterCreate, usageAfterCharge, settlement); err != nil {
		t.Fatalf("marketplace skill usage charge contract: %v", err)
	}
}

func runEnterprisePinnedInstallUpgrade(t *testing.T, env *pr5ServiceEnv) {
	t.Helper()
	var fixture enterpriseUpgradeFixture
	readJSON(t, "tests/fixtures/contracts/marketplace/enterprise_install_pinned_upgrade.json", &fixture)
	initial, err := env.repo.SaveSkillInstallationSnapshotV1(t.Context(), fixture.InitialInstallation, "ent_001:listing_city_tourism_creator_001:install")
	if err != nil {
		t.Fatalf("save enterprise pinned installation: %v", err)
	}
	upgraded, err := env.repo.UpgradeSkillInstallationV1(t.Context(), fixture.UpgradeRequest, fixture.InstallationAfterUpgrade, fixture.HistoricalRunRule)
	if err != nil {
		t.Fatalf("upgrade enterprise pinned installation: %v", err)
	}
	if err := pr4.ValidateEnterprisePinnedUpgrade(initial, fixture.UpgradeRequest, upgraded, fixture.HistoricalRunRule); err != nil {
		t.Fatalf("enterprise pinned upgrade contract: %v", err)
	}
}

func runToolPartialFailureRelease(t *testing.T, env *pr5ServiceEnv) {
	t.Helper()
	var fixture creditReleaseFixture
	readJSON(t, "tests/fixtures/contracts/credit/freeze_release_on_tool_failure.json", &fixture)
	releaseRequest := pr3.FreezeCreditsRequest{
		CreditAccountID:    fixture.HoldAfterFreeze.AccountID,
		CreditAccountScope: fixture.HoldAfterFreeze.AccountScope,
		RunID:              fixture.HoldAfterFreeze.RunID,
		ProjectID:          "proj_city_001",
		ToolPlanID:         fixture.HoldAfterFreeze.ToolPlanID,
		ToolPlanDigest:     fixture.HoldAfterFreeze.ToolPlanDigest,
		Credits:            fixture.HoldAfterFreeze.FrozenCredits,
		IdempotencyKey:     fixture.HoldAfterFreeze.IdempotencyKey,
	}
	frozen, err := env.repo.FreezeToolCreditsV1(t.Context(), releaseRequest, fixture.HoldAfterFreeze.CreditHoldID, "trace_pr5_credit_release", fixture.HoldAfterFreeze.CreatedAt)
	if err != nil {
		t.Fatalf("freeze tool credits for failure path: %v", err)
	}
	released, err := env.repo.ReleaseToolCreditsV1(t.Context(), fixture.HoldAfterFreeze.CreditHoldID, "cled_pr5_city_video_release_001", "trace_pr5_credit_release", *fixture.HoldAfterRelease.UpdatedAt)
	if err != nil {
		t.Fatalf("release tool credits for failure path: %v", err)
	}
	if err := pr3.ValidateFreezeReleaseOnFailure(frozen, fixture.ToolTaskFailure, released); err != nil {
		t.Fatalf("tool partial failure release contract: %v", err)
	}
}

func runListingSuspendedGuard(t *testing.T, env *pr5ServiceEnv, publish creatorPublishFixture) {
	t.Helper()
	suspendedAt := publish.Listing.UpdatedAt.Add(30 * time.Minute)
	suspended, err := env.repo.SuspendMarketplaceListingV1(t.Context(), publish.Listing.ListingID, suspendedAt)
	if err != nil {
		t.Fatalf("suspend marketplace listing: %v", err)
	}
	if suspended.Status != "suspended" {
		t.Fatalf("expected suspended listing, got %s", suspended.Status)
	}

	var installFixture personalInstallFixture
	readJSON(t, "tests/fixtures/contracts/marketplace/user_install_latest_personal.json", &installFixture)
	installFixture.Request.AccountID = "acct_personal_suspended_001"
	installFixture.Request.IdempotencyKey = "acct_personal_suspended_001:listing_city_tourism_creator_001:install"
	installFixture.Installation.InstallationID = "sinst_personal_suspended_city_001"
	installFixture.Installation.AccountID = installFixture.Request.AccountID
	if _, err := env.repo.InstallPersonalLatestSkillV1(t.Context(), installFixture.Request, installFixture.Installation); !errors.Is(err, businesscore.ErrMarketplaceListingSuspended) {
		t.Fatalf("expected MARKETPLACE_LISTING_SUSPENDED for new install, got %v", err)
	}
}

func runRefundSettlementReverse(t *testing.T, env *pr5ServiceEnv) {
	t.Helper()
	var fixture skillUsageRefundFixture
	readJSON(t, "tests/fixtures/contracts/billing/skill_usage_refund_reversal.json", &fixture)
	beforeRefund, err := env.repo.MarkSkillUsageRefundPendingV1(t.Context(), fixture.UsageBeforeRefund)
	if err != nil {
		t.Fatalf("mark skill usage refund pending: %v", err)
	}
	afterRefund, settlementAfterReverse, err := env.repo.ReverseSkillUsageRefundV1(t.Context(), fixture.UsageAfterRefund, fixture.SettlementAfterReverse)
	if err != nil {
		t.Fatalf("reverse skill usage refund: %v", err)
	}
	if err := pr4.ValidateSkillUsageRefundReversal(beforeRefund, afterRefund, settlementAfterReverse); err != nil {
		t.Fatalf("skill usage refund reversal contract: %v", err)
	}
}

func runReplayAfterRestart(t *testing.T, env *pr5ServiceEnv) {
	t.Helper()
	var fixture providerAsyncResumeFixture
	readJSON(t, "tests/fixtures/contracts/tool/provider_async_resume.json", &fixture)
	insertAgentToolTask(t, env.agent.DB, fixture.ToolTaskBeforeRestart)
	resumed := applyAgentToolTaskCompletedEvent(t, env.agent.DB, fixture.RedisStreamEvent, fixture.ToolTaskAfterResume)
	if !reflect.DeepEqual(resumed, fixture.ToolTaskAfterResume) {
		t.Fatalf("unexpected resumed tool task\nwant: %#v\ngot:  %#v", fixture.ToolTaskAfterResume, resumed)
	}
	replayed := applyAgentToolTaskCompletedEvent(t, env.agent.DB, fixture.RedisStreamEvent, fixture.ToolTaskAfterResume)
	if !reflect.DeepEqual(replayed, resumed) {
		t.Fatalf("duplicate provider callback must be ignored\nfirst: %#v\nreplay:%#v", resumed, replayed)
	}
}

func assertNoFrozenCreditHolds(t *testing.T, env *pr5ServiceEnv) {
	t.Helper()
	var count int64
	if err := env.business.DB.Model(&businesscore.PR3CreditHoldRecord{}).Where("status = ?", "frozen").Count(&count).Error; err != nil {
		t.Fatalf("count frozen credit holds: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no leaked frozen credit holds, got %d", count)
	}
}

func insertAgentToolPlan(t *testing.T, db *gorm.DB, plan pr3.ToolPlan) {
	t.Helper()
	itemsJSON := mustJSON(t, plan.Items)
	var expiresAt any
	if plan.ExpiresAt != nil {
		expiresAt = *plan.ExpiresAt
	}
	err := db.Exec(`
INSERT INTO tool_plans (
  tool_plan_id, run_id, board_id, board_version, graph_plan_id, status, items,
  estimated_credits, currency, confirmation_required, expires_at, tool_plan_digest, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (tool_plan_id) DO NOTHING
`,
		plan.ToolPlanID,
		plan.RunID,
		plan.BoardID,
		plan.BoardVersion,
		plan.GraphPlanID,
		plan.Status,
		itemsJSON,
		plan.EstimatedCredits,
		plan.Currency,
		plan.ConfirmationRequired,
		expiresAt,
		plan.ToolPlanDigest,
		plan.CreatedAt,
		plan.UpdatedAt,
	).Error
	if err != nil {
		t.Fatalf("insert agent tool plan: %v", err)
	}
}

func insertAgentToolTask(t *testing.T, db *gorm.DB, task pr3.ToolTask) {
	t.Helper()
	if err := pr3.ValidateToolTask(task); err != nil {
		t.Fatalf("validate agent tool task before insert: %v", err)
	}
	err := db.Exec(`
INSERT INTO tool_tasks (
  tool_task_id, tool_plan_id, tool_plan_item_id, run_id, status, progress,
  provider_policy, idempotency_key, input_digest, output_digest, error_code, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?)
ON CONFLICT (tool_task_id) DO NOTHING
`,
		task.ToolTaskID,
		task.ToolPlanID,
		task.ToolPlanItemID,
		task.RunID,
		task.Status,
		task.Progress,
		mustJSON(t, task.ProviderPolicy),
		task.IdempotencyKey,
		task.InputDigest,
		stringPointerValue(task.OutputDigest),
		stringPointerValue(task.ErrorCode),
		task.CreatedAt,
		task.UpdatedAt,
	).Error
	if err != nil {
		t.Fatalf("insert agent tool task: %v", err)
	}
}

func applyAgentToolTaskCompletedEvent(t *testing.T, db *gorm.DB, event pr3.ToolTaskCompletedStreamEvent, expectedAfter pr3.ToolTask) pr3.ToolTask {
	t.Helper()
	if err := pr3.ValidateToolTaskCompletedStreamEvent(event); err != nil {
		t.Fatalf("validate provider completion event: %v", err)
	}
	before := getAgentToolTask(t, db, event.ToolTaskID)
	if before.Status == "succeeded" && before.OutputDigest != nil && *before.OutputDigest == event.OutputDigest {
		return before
	}
	if err := pr3.ValidateProviderAsyncResume(before, event, expectedAfter); err != nil {
		t.Fatalf("validate provider async resume: %v", err)
	}
	err := db.Exec(`
UPDATE tool_tasks
SET status = ?, progress = ?, output_digest = ?, error_code = ?, updated_at = ?
WHERE tool_task_id = ?
`,
		expectedAfter.Status,
		expectedAfter.Progress,
		stringPointerValue(expectedAfter.OutputDigest),
		stringPointerValue(expectedAfter.ErrorCode),
		expectedAfter.UpdatedAt,
		expectedAfter.ToolTaskID,
	).Error
	if err != nil {
		t.Fatalf("apply provider completion event: %v", err)
	}
	return getAgentToolTask(t, db, event.ToolTaskID)
}

func getAgentToolTask(t *testing.T, db *gorm.DB, toolTaskID string) pr3.ToolTask {
	t.Helper()
	var row struct {
		ToolTaskID     string  `gorm:"column:tool_task_id"`
		ToolPlanID     string  `gorm:"column:tool_plan_id"`
		ToolPlanItemID string  `gorm:"column:tool_plan_item_id"`
		RunID          string  `gorm:"column:run_id"`
		Status         string  `gorm:"column:status"`
		Progress       int     `gorm:"column:progress"`
		ProviderPolicy []byte  `gorm:"column:provider_policy"`
		IdempotencyKey string  `gorm:"column:idempotency_key"`
		InputDigest    string  `gorm:"column:input_digest"`
		OutputDigest   *string `gorm:"column:output_digest"`
		ErrorCode      *string `gorm:"column:error_code"`
		CreatedAt      time.Time
		UpdatedAt      time.Time
	}
	err := db.Raw(`
SELECT tool_task_id, tool_plan_id, tool_plan_item_id, run_id, status, progress,
       provider_policy, idempotency_key, input_digest, output_digest, error_code, created_at, updated_at
FROM tool_tasks
WHERE tool_task_id = ?
`, toolTaskID).Scan(&row).Error
	if err != nil {
		t.Fatalf("query agent tool task %s: %v", toolTaskID, err)
	}
	if row.ToolTaskID == "" {
		t.Fatalf("agent tool task %s not found", toolTaskID)
	}
	var policy pr3.ProviderPolicy
	if err := json.Unmarshal(row.ProviderPolicy, &policy); err != nil {
		t.Fatalf("unmarshal provider policy: %v", err)
	}
	task := pr3.ToolTask{
		SchemaVersion:  pr3.SchemaVersionToolTask,
		ToolTaskID:     row.ToolTaskID,
		ToolPlanID:     row.ToolPlanID,
		ToolPlanItemID: row.ToolPlanItemID,
		RunID:          row.RunID,
		Status:         row.Status,
		Progress:       row.Progress,
		ProviderPolicy: policy,
		IdempotencyKey: row.IdempotencyKey,
		InputDigest:    row.InputDigest,
		OutputDigest:   row.OutputDigest,
		ErrorCode:      row.ErrorCode,
		CreatedAt:      row.CreatedAt.UTC(),
		UpdatedAt:      row.UpdatedAt.UTC(),
	}
	if err := pr3.ValidateToolTask(task); err != nil {
		t.Fatalf("validate loaded agent tool task: %v", err)
	}
	return task
}

func assertRecordCount(t *testing.T, db *gorm.DB, table string, expected int64) {
	t.Helper()
	var count int64
	if err := db.Table(table).Count(&count).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != expected {
		t.Fatalf("expected %s count=%d, got %d", table, expected, count)
	}
}

func readJSON(t *testing.T, relativePath string, target any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdb.RepoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relativePath, err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", relativePath, err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(body)
}

func stringPointerValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

type creatorPublishFixture struct {
	SkillPackage  pr4.SkillPackage       `json:"skill_package"`
	SkillVersion  pr4.SkillVersion       `json:"skill_version"`
	PricingPolicy pr4.SkillPricingPolicy `json:"pricing_policy"`
	Listing       pr4.MarketplaceListing `json:"listing"`
}

type personalInstallFixture struct {
	Request      pr4.InstallSkillRequest `json:"request"`
	Installation pr4.SkillInstallation   `json:"installation"`
}

type enterpriseUpgradeFixture struct {
	InitialInstallation      pr4.SkillInstallation               `json:"initial_installation"`
	UpgradeRequest           pr4.UpgradeSkillInstallationRequest `json:"upgrade_request"`
	InstallationAfterUpgrade pr4.SkillInstallation               `json:"installation_after_upgrade"`
	HistoricalRunRule        pr4.HistoricalRunRule               `json:"historical_run_rule"`
}

type skillUsageChargeFixture struct {
	Sequence         []string             `json:"sequence"`
	UsageAfterCreate pr4.SkillUsageRecord `json:"usage_after_create"`
	UsageAfterCharge pr4.SkillUsageRecord `json:"usage_after_charge"`
	Settlement       pr4.SkillSettlement  `json:"settlement"`
}

type skillUsageRefundFixture struct {
	UsageBeforeRefund      pr4.SkillUsageRecord `json:"usage_before_refund"`
	UsageAfterRefund       pr4.SkillUsageRecord `json:"usage_after_refund"`
	SettlementAfterReverse pr4.SkillSettlement  `json:"settlement_after_reverse"`
}

type toolPlanFixture struct {
	Precondition pr3.ToolPlanPrecondition `json:"precondition"`
	ToolPlan     pr3.ToolPlan             `json:"tool_plan"`
}

type creditCommitFixture struct {
	FreezeRequest   pr3.FreezeCreditsRequest `json:"freeze_request"`
	HoldAfterFreeze pr3.CreditFreeze         `json:"hold_after_freeze"`
	HoldAfterCommit pr3.CreditFreeze         `json:"hold_after_commit"`
}

type creditReleaseFixture struct {
	HoldAfterFreeze  pr3.CreditFreeze `json:"hold_after_freeze"`
	ToolTaskFailure  pr3.ToolTask     `json:"tool_task_failure"`
	HoldAfterRelease pr3.CreditFreeze `json:"hold_after_release"`
}

type assetCommitFixture struct {
	ToolResult     pr3.ToolResult          `json:"tool_result"`
	CommitResponse pr3.AssetCommitResponse `json:"commit_response"`
	BillingRule    pr3.AssetBillingRule    `json:"billing_rule"`
}

type providerAsyncResumeFixture struct {
	ToolTaskBeforeRestart pr3.ToolTask                     `json:"tool_task_before_restart"`
	RedisStreamEvent      pr3.ToolTaskCompletedStreamEvent `json:"redis_stream_event"`
	ToolTaskAfterResume   pr3.ToolTask                     `json:"tool_task_after_resume"`
}
