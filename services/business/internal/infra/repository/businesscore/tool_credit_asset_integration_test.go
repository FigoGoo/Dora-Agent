package businesscore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/toolasset"
	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
)

func TestBusinessCreditAssetRepositoryWithActiveMigration(t *testing.T) {
	db := testdb.StartPostgres(t, "dora_business_tool_asset")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-07-01-tool-credit-asset-contracts/business")
	testdb.RequireNoForeignKeys(t, db.DB)
	for _, table := range []string{"credit_holds", "credit_ledger_entries", "tool_pricing_snapshots", "generated_assets", "asset_commit_records"} {
		if !testdb.TableExists(t, db.DB, table) {
			t.Fatalf("tool asset active business migration table %s missing", table)
		}
	}
	if testdb.TableExists(t, db.DB, "tool_plans") || testdb.TableExists(t, db.DB, "tool_tasks") {
		t.Fatal("business database must not contain agent tool plan or task tables")
	}

	repo := businesscore.New(db.DB)

	var creditCommitFixture struct {
		FreezeRequest   toolasset.FreezeCreditsRequest `json:"freeze_request"`
		HoldAfterFreeze toolasset.CreditFreeze         `json:"hold_after_freeze"`
		HoldAfterCommit toolasset.CreditFreeze         `json:"hold_after_commit"`
	}
	readToolAssetBusinessFixture(t, "tests/fixtures/contracts/credit/freeze_commit_success.json", &creditCommitFixture)
	frozen, err := repo.FreezeToolCreditsV1(
		t.Context(),
		creditCommitFixture.FreezeRequest,
		creditCommitFixture.HoldAfterFreeze.CreditHoldID,
		"trace_tool_asset_credit_commit",
		creditCommitFixture.HoldAfterFreeze.CreatedAt,
	)
	if err != nil {
		t.Fatalf("freeze tool credits: %v", err)
	}
	if !reflect.DeepEqual(frozen, creditCommitFixture.HoldAfterFreeze) {
		t.Fatalf("unexpected frozen hold\nwant: %#v\ngot:  %#v", creditCommitFixture.HoldAfterFreeze, frozen)
	}
	replayedFreeze, err := repo.FreezeToolCreditsV1(
		t.Context(),
		creditCommitFixture.FreezeRequest,
		creditCommitFixture.HoldAfterFreeze.CreditHoldID,
		"trace_tool_asset_credit_commit",
		creditCommitFixture.HoldAfterFreeze.CreatedAt,
	)
	if err != nil {
		t.Fatalf("freeze tool credits idempotently: %v", err)
	}
	if !reflect.DeepEqual(replayedFreeze, frozen) {
		t.Fatalf("unexpected replayed frozen hold: %#v", replayedFreeze)
	}
	committed, err := repo.CommitToolCreditsV1(
		t.Context(),
		creditCommitFixture.HoldAfterFreeze.CreditHoldID,
		"cled_city_video_commit_001",
		"trace_tool_asset_credit_commit",
		*creditCommitFixture.HoldAfterCommit.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("commit tool credits: %v", err)
	}
	if err := toolasset.ValidateFreezeCommitFlow(creditCommitFixture.FreezeRequest, frozen, committed); err != nil {
		t.Fatalf("freeze commit flow contract: %v", err)
	}
	if !reflect.DeepEqual(committed, creditCommitFixture.HoldAfterCommit) {
		t.Fatalf("unexpected committed hold\nwant: %#v\ngot:  %#v", creditCommitFixture.HoldAfterCommit, committed)
	}
	replayedCommit, err := repo.CommitToolCreditsV1(
		t.Context(),
		creditCommitFixture.HoldAfterFreeze.CreditHoldID,
		"cled_city_video_commit_001",
		"trace_tool_asset_credit_commit",
		*creditCommitFixture.HoldAfterCommit.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("commit tool credits idempotently: %v", err)
	}
	if !reflect.DeepEqual(replayedCommit, committed) {
		t.Fatalf("unexpected replayed committed hold: %#v", replayedCommit)
	}

	var creditReleaseFixture struct {
		HoldAfterFreeze  toolasset.CreditFreeze `json:"hold_after_freeze"`
		ToolTaskFailure  toolasset.ToolTask     `json:"tool_task_failure"`
		HoldAfterRelease toolasset.CreditFreeze `json:"hold_after_release"`
	}
	readToolAssetBusinessFixture(t, "tests/fixtures/contracts/credit/freeze_release_on_tool_failure.json", &creditReleaseFixture)
	releaseRequest := toolasset.FreezeCreditsRequest{
		CreditAccountID:    creditReleaseFixture.HoldAfterFreeze.AccountID,
		CreditAccountScope: creditReleaseFixture.HoldAfterFreeze.AccountScope,
		RunID:              creditReleaseFixture.HoldAfterFreeze.RunID,
		ProjectID:          "proj_city_001",
		ToolPlanID:         creditReleaseFixture.HoldAfterFreeze.ToolPlanID,
		ToolPlanDigest:     creditReleaseFixture.HoldAfterFreeze.ToolPlanDigest,
		Credits:            creditReleaseFixture.HoldAfterFreeze.FrozenCredits,
		IdempotencyKey:     creditReleaseFixture.HoldAfterFreeze.IdempotencyKey,
	}
	frozenFailed, err := repo.FreezeToolCreditsV1(
		t.Context(),
		releaseRequest,
		creditReleaseFixture.HoldAfterFreeze.CreditHoldID,
		"trace_tool_asset_credit_release",
		creditReleaseFixture.HoldAfterFreeze.CreatedAt,
	)
	if err != nil {
		t.Fatalf("freeze tool credits for failure path: %v", err)
	}
	released, err := repo.ReleaseToolCreditsV1(
		t.Context(),
		creditReleaseFixture.HoldAfterFreeze.CreditHoldID,
		"cled_city_video_release_001",
		"trace_tool_asset_credit_release",
		*creditReleaseFixture.HoldAfterRelease.UpdatedAt,
	)
	if err != nil {
		t.Fatalf("release tool credits: %v", err)
	}
	if err := toolasset.ValidateFreezeReleaseOnFailure(frozenFailed, creditReleaseFixture.ToolTaskFailure, released); err != nil {
		t.Fatalf("freeze release flow contract: %v", err)
	}
	if !reflect.DeepEqual(released, creditReleaseFixture.HoldAfterRelease) {
		t.Fatalf("unexpected released hold\nwant: %#v\ngot:  %#v", creditReleaseFixture.HoldAfterRelease, released)
	}

	var assetFixture struct {
		ToolResult     toolasset.ToolResult          `json:"tool_result"`
		CommitResponse toolasset.AssetCommitResponse `json:"commit_response"`
		BillingRule    toolasset.AssetBillingRule    `json:"billing_rule"`
	}
	readToolAssetBusinessFixture(t, "tests/fixtures/contracts/asset/partial_commit_success.json", &assetFixture)
	commitResponse, err := repo.CommitGeneratedAssetsV1(
		t.Context(),
		assetFixture.ToolResult,
		assetFixture.CommitResponse,
		assetFixture.BillingRule,
		"run_city_tourism_001",
		"proj_city_001",
		"commit:ttask_city_video_001:v1",
		"trace_tool_asset_asset_commit",
		assetFixture.ToolResult.CreatedAt,
	)
	if err != nil {
		t.Fatalf("commit generated assets: %v", err)
	}
	if err := toolasset.ValidatePartialAssetCommit(assetFixture.ToolResult, commitResponse, assetFixture.BillingRule); err != nil {
		t.Fatalf("partial asset commit contract: %v", err)
	}
	if !reflect.DeepEqual(commitResponse, assetFixture.CommitResponse) {
		t.Fatalf("unexpected asset commit response\nwant: %#v\ngot:  %#v", assetFixture.CommitResponse, commitResponse)
	}
	replayedAssetCommit, err := repo.CommitGeneratedAssetsV1(
		t.Context(),
		assetFixture.ToolResult,
		assetFixture.CommitResponse,
		assetFixture.BillingRule,
		"run_city_tourism_001",
		"proj_city_001",
		"commit:ttask_city_video_001:v1",
		"trace_tool_asset_asset_commit",
		assetFixture.ToolResult.CreatedAt,
	)
	if err != nil {
		t.Fatalf("commit generated assets idempotently: %v", err)
	}
	if !reflect.DeepEqual(replayedAssetCommit, commitResponse) {
		t.Fatalf("unexpected replayed asset commit: %#v", replayedAssetCommit)
	}

	var ledgerCount int64
	if err := db.DB.Model(&businesscore.CreditLedgerEntryRecord{}).Count(&ledgerCount).Error; err != nil {
		t.Fatalf("count credit ledger entries: %v", err)
	}
	if ledgerCount != 2 {
		t.Fatalf("expected 2 credit ledger entries, got %d", ledgerCount)
	}
	var generatedAssetCount int64
	if err := db.DB.Model(&businesscore.GeneratedAssetRecord{}).Count(&generatedAssetCount).Error; err != nil {
		t.Fatalf("count generated assets: %v", err)
	}
	if generatedAssetCount != int64(len(assetFixture.ToolResult.Assets)) {
		t.Fatalf("expected %d generated assets, got %d", len(assetFixture.ToolResult.Assets), generatedAssetCount)
	}

	testdb.DownMigrations(t, migrator)
	if count := testdb.CountTables(t, db.DB); count != 0 {
		t.Fatalf("expected migration down to drop tables, got %d", count)
	}
}

func readToolAssetBusinessFixture(t *testing.T, relativePath string, target any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(testdb.RepoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read fixture %s: %v", relativePath, err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", relativePath, err)
	}
}
