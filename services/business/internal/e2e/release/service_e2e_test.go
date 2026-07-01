package release_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/releasegate"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/skillmarket"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/toolasset"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"gorm.io/gorm"
)

type releaseServiceEnv struct {
	agent    *testdb.Database
	business *testdb.Database
	repo     *businesscore.Repository
}

func TestReleaseServiceLevelE2EWithFakeProviderAndPostgres(t *testing.T) {
	env := newReleaseServiceEnv(t)
	validateReleaseE2EFixtures(t)
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

	downReleaseMigrations(t, env)
	if count := testdb.CountTables(t, env.agent.DB); count != 0 {
		t.Fatalf("expected release agent migration down to drop tables, got %d", count)
	}
	if count := testdb.CountTables(t, env.business.DB); count != 0 {
		t.Fatalf("expected release business migration down to drop tables, got %d", count)
	}
}

func newReleaseServiceEnv(t *testing.T) *releaseServiceEnv {
	t.Helper()
	env := &releaseServiceEnv{
		agent:    testdb.StartPostgres(t, "dora_release_agent"),
		business: testdb.StartPostgres(t, "dora_release_business"),
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

func downReleaseMigrations(t *testing.T, env *releaseServiceEnv) {
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

func validateReleaseE2EFixtures(t *testing.T) {
	t.Helper()
	suites := make([]releasegate.E2ESuite, 0, 3)
	for _, relativePath := range []string{
		"tests/e2e/agent-workspace/scenarios.json",
		"tests/e2e/skill-marketplace/scenarios.json",
		"tests/e2e/admin-governance/scenarios.json",
	} {
		var suite releasegate.E2ESuite
		readJSON(t, relativePath, &suite)
		suites = append(suites, suite)
	}
	if err := releasegate.ValidateE2ESuiteIndexes(suites); err != nil {
		t.Fatalf("validate release e2e suite indexes: %v", err)
	}
	for relativePath := range releasegate.RequiredE2EFixtures {
		var fixture releasegate.E2EFixture
		readJSON(t, filepath.Join("tests/fixtures/e2e", relativePath), &fixture)
		if err := releasegate.ValidateE2EFixture(relativePath, fixture); err != nil {
			t.Fatalf("validate release e2e fixture %s: %v", relativePath, err)
		}
	}
}

func assertFakeProviderScenariosExecutable(t *testing.T) {
	t.Helper()
	var manifest releasegate.FakeProviderManifest
	var scenarios releasegate.ProviderScenarios
	readJSON(t, "tests/e2e/fake-provider/fake_provider_manifest.json", &manifest)
	readJSON(t, "tests/e2e/fake-provider/provider_scenarios.json", &scenarios)
	if err := releasegate.ValidateFakeProviderArtifacts(manifest, scenarios); err != nil {
		t.Fatalf("validate fake provider artifacts: %v", err)
	}
	for _, scenario := range scenarios.Scenarios {
		first, err := releasegate.SimulateFakeProviderScenario(scenario)
		if err != nil {
			t.Fatalf("simulate fake provider scenario %s: %v", scenario.CaseID, err)
		}
		second, err := releasegate.SimulateFakeProviderScenario(scenario)
		if err != nil {
			t.Fatalf("simulate fake provider replay %s: %v", scenario.CaseID, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("fake provider scenario %s is not idempotent", scenario.CaseID)
		}
	}
}

func runCreatorPublish(t *testing.T, env *releaseServiceEnv) creatorPublishFixture {
	t.Helper()
	var fixture creatorPublishFixture
	readJSON(t, "tests/fixtures/contracts/marketplace/creator_submit_approve_publish.json", &fixture)
	if err := env.repo.SaveCreatorPublishFlowV1(t.Context(), fixture.SkillPackage, fixture.SkillVersion, fixture.PricingPolicy, fixture.Listing); err != nil {
		t.Fatalf("save creator publish flow: %v", err)
	}
	return fixture
}

func runCityTourismDefaultSkill(t *testing.T, env *releaseServiceEnv) {
	t.Helper()
	var planFixture toolPlanFixture
	readJSON(t, "tests/fixtures/contracts/toolplan/city_video_toolplan.json", &planFixture)
	if err := toolasset.ValidateToolPlanForApprovedBoard(planFixture.Precondition, planFixture.ToolPlan); err != nil {
		t.Fatalf("validate city tool plan: %v", err)
	}
	insertAgentToolPlan(t, env.agent.DB, planFixture.ToolPlan)

	var creditFixture creditCommitFixture
	readJSON(t, "tests/fixtures/contracts/credit/freeze_commit_success.json", &creditFixture)
	frozen, err := env.repo.FreezeToolCreditsV1(
		t.Context(),
		creditFixture.FreezeRequest,
		creditFixture.HoldAfterFreeze.CreditHoldID,
		"trace_release_city_credit_commit",
		creditFixture.HoldAfterFreeze.CreatedAt,
	)
	if err != nil {
		t.Fatalf("freeze city tool credits: %v", err)
	}
	committed, err := env.repo.CommitToolCreditsV1(
		t.Context(),
		creditFixture.HoldAfterFreeze.CreditHoldID,
		"cled_release_city_video_commit_001",
		"trace_release_city_credit_commit",
		*creditFixture.HoldAfterCommit.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("commit city tool credits: %v", err)
	}
	if err := toolasset.ValidateFreezeCommitFlow(creditFixture.FreezeRequest, frozen, committed); err != nil {
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
		"commit:ttask_city_video_001:release",
		"trace_release_city_asset_commit",
		assetFixture.ToolResult.CreatedAt,
	)
	if err != nil {
		t.Fatalf("commit city generated assets: %v", err)
	}
	if err := toolasset.ValidatePartialAssetCommit(assetFixture.ToolResult, assetCommit, assetFixture.BillingRule); err != nil {
		t.Fatalf("city partial asset commit contract: %v", err)
	}
	assertRecordCount(t, env.business.DB, "skill_usage_records", 0)
}

func runGenericCreationGraphFallback(t *testing.T, env *releaseServiceEnv) {
	t.Helper()
	var fixture releasegate.E2EFixture
	readJSON(t, "tests/fixtures/e2e/agent-workspace/generic_creation_graph_fallback.json", &fixture)
	if fixture.ExpectedBusinessState["skill_usage_record"] != "not_created" {
		t.Fatalf("generic creation fallback fixture must keep skill usage uncreated")
	}
	assertRecordCount(t, env.business.DB, "skill_usage_records", 0)
	assertRecordCount(t, env.agent.DB, "tool_plans", 1)
}

func runPaidMarketplaceSkillUsage(t *testing.T, env *releaseServiceEnv) {
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
	if err := skillmarket.ValidateSkillUsagePrecreateConfirmCharge(chargeFixture.Sequence, usageAfterCreate, usageAfterCharge, settlement); err != nil {
		t.Fatalf("marketplace skill usage charge contract: %v", err)
	}
}

func runEnterprisePinnedInstallUpgrade(t *testing.T, env *releaseServiceEnv) {
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
	if err := skillmarket.ValidateEnterprisePinnedUpgrade(initial, fixture.UpgradeRequest, upgraded, fixture.HistoricalRunRule); err != nil {
		t.Fatalf("enterprise pinned upgrade contract: %v", err)
	}
}

func runToolPartialFailureRelease(t *testing.T, env *releaseServiceEnv) {
	t.Helper()
	var fixture creditReleaseFixture
	readJSON(t, "tests/fixtures/contracts/credit/freeze_release_on_tool_failure.json", &fixture)
	releaseRequest := toolasset.FreezeCreditsRequest{
		CreditAccountID:    fixture.HoldAfterFreeze.AccountID,
		CreditAccountScope: fixture.HoldAfterFreeze.AccountScope,
		RunID:              fixture.HoldAfterFreeze.RunID,
		ProjectID:          "proj_city_001",
		ToolPlanID:         fixture.HoldAfterFreeze.ToolPlanID,
		ToolPlanDigest:     fixture.HoldAfterFreeze.ToolPlanDigest,
		Credits:            fixture.HoldAfterFreeze.FrozenCredits,
		IdempotencyKey:     fixture.HoldAfterFreeze.IdempotencyKey,
	}
	frozen, err := env.repo.FreezeToolCreditsV1(t.Context(), releaseRequest, fixture.HoldAfterFreeze.CreditHoldID, "trace_release_credit_release", fixture.HoldAfterFreeze.CreatedAt)
	if err != nil {
		t.Fatalf("freeze tool credits for failure path: %v", err)
	}
	released, err := env.repo.ReleaseToolCreditsV1(t.Context(), fixture.HoldAfterFreeze.CreditHoldID, "cled_release_city_video_release_001", "trace_release_credit_release", *fixture.HoldAfterRelease.UpdatedAt)
	if err != nil {
		t.Fatalf("release tool credits for failure path: %v", err)
	}
	if err := toolasset.ValidateFreezeReleaseOnFailure(frozen, fixture.ToolTaskFailure, released); err != nil {
		t.Fatalf("tool partial failure release contract: %v", err)
	}
}

func runListingSuspendedGuard(t *testing.T, env *releaseServiceEnv, publish creatorPublishFixture) {
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

func runRefundSettlementReverse(t *testing.T, env *releaseServiceEnv) {
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
	if err := skillmarket.ValidateSkillUsageRefundReversal(beforeRefund, afterRefund, settlementAfterReverse); err != nil {
		t.Fatalf("skill usage refund reversal contract: %v", err)
	}
}

func runReplayAfterRestart(t *testing.T, env *releaseServiceEnv) {
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

func assertNoFrozenCreditHolds(t *testing.T, env *releaseServiceEnv) {
	t.Helper()
	var count int64
	if err := env.business.DB.Model(&businesscore.CreditHoldRecord{}).Where("status = ?", "frozen").Count(&count).Error; err != nil {
		t.Fatalf("count frozen credit holds: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no leaked frozen credit holds, got %d", count)
	}
}

func insertAgentToolPlan(t *testing.T, db *gorm.DB, plan toolasset.ToolPlan) {
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

func insertAgentToolTask(t *testing.T, db *gorm.DB, task toolasset.ToolTask) {
	t.Helper()
	if err := toolasset.ValidateToolTask(task); err != nil {
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

func applyAgentToolTaskCompletedEvent(t *testing.T, db *gorm.DB, event toolasset.ToolTaskCompletedStreamEvent, expectedAfter toolasset.ToolTask) toolasset.ToolTask {
	t.Helper()
	if err := toolasset.ValidateToolTaskCompletedStreamEvent(event); err != nil {
		t.Fatalf("validate provider completion event: %v", err)
	}
	before := getAgentToolTask(t, db, event.ToolTaskID)
	if before.Status == "succeeded" && before.OutputDigest != nil && *before.OutputDigest == event.OutputDigest {
		return before
	}
	if err := toolasset.ValidateProviderAsyncResume(before, event, expectedAfter); err != nil {
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

func getAgentToolTask(t *testing.T, db *gorm.DB, toolTaskID string) toolasset.ToolTask {
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
	var policy toolasset.ProviderPolicy
	if err := json.Unmarshal(row.ProviderPolicy, &policy); err != nil {
		t.Fatalf("unmarshal provider policy: %v", err)
	}
	task := toolasset.ToolTask{
		SchemaVersion:  toolasset.SchemaVersionToolTask,
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
	if err := toolasset.ValidateToolTask(task); err != nil {
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
	SkillPackage  skillmarket.SkillPackage       `json:"skill_package"`
	SkillVersion  skillmarket.SkillVersion       `json:"skill_version"`
	PricingPolicy skillmarket.SkillPricingPolicy `json:"pricing_policy"`
	Listing       skillmarket.MarketplaceListing `json:"listing"`
}

type personalInstallFixture struct {
	Request      skillmarket.InstallSkillRequest `json:"request"`
	Installation skillmarket.SkillInstallation   `json:"installation"`
}

type enterpriseUpgradeFixture struct {
	InitialInstallation      skillmarket.SkillInstallation               `json:"initial_installation"`
	UpgradeRequest           skillmarket.UpgradeSkillInstallationRequest `json:"upgrade_request"`
	InstallationAfterUpgrade skillmarket.SkillInstallation               `json:"installation_after_upgrade"`
	HistoricalRunRule        skillmarket.HistoricalRunRule               `json:"historical_run_rule"`
}

type skillUsageChargeFixture struct {
	Sequence         []string                     `json:"sequence"`
	UsageAfterCreate skillmarket.SkillUsageRecord `json:"usage_after_create"`
	UsageAfterCharge skillmarket.SkillUsageRecord `json:"usage_after_charge"`
	Settlement       skillmarket.SkillSettlement  `json:"settlement"`
}

type skillUsageRefundFixture struct {
	UsageBeforeRefund      skillmarket.SkillUsageRecord `json:"usage_before_refund"`
	UsageAfterRefund       skillmarket.SkillUsageRecord `json:"usage_after_refund"`
	SettlementAfterReverse skillmarket.SkillSettlement  `json:"settlement_after_reverse"`
}

type toolPlanFixture struct {
	Precondition toolasset.ToolPlanPrecondition `json:"precondition"`
	ToolPlan     toolasset.ToolPlan             `json:"tool_plan"`
}

type creditCommitFixture struct {
	FreezeRequest   toolasset.FreezeCreditsRequest `json:"freeze_request"`
	HoldAfterFreeze toolasset.CreditFreeze         `json:"hold_after_freeze"`
	HoldAfterCommit toolasset.CreditFreeze         `json:"hold_after_commit"`
}

type creditReleaseFixture struct {
	HoldAfterFreeze  toolasset.CreditFreeze `json:"hold_after_freeze"`
	ToolTaskFailure  toolasset.ToolTask     `json:"tool_task_failure"`
	HoldAfterRelease toolasset.CreditFreeze `json:"hold_after_release"`
}

type assetCommitFixture struct {
	ToolResult     toolasset.ToolResult          `json:"tool_result"`
	CommitResponse toolasset.AssetCommitResponse `json:"commit_response"`
	BillingRule    toolasset.AssetBillingRule    `json:"billing_rule"`
}

type providerAsyncResumeFixture struct {
	ToolTaskBeforeRestart toolasset.ToolTask                     `json:"tool_task_before_restart"`
	RedisStreamEvent      toolasset.ToolTaskCompletedStreamEvent `json:"redis_stream_event"`
	ToolTaskAfterResume   toolasset.ToolTask                     `json:"tool_task_after_resume"`
}
