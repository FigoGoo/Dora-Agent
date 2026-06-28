package assetcommit

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/testdb"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/asset"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/credit"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"gorm.io/gorm"
)

func TestCommitRejectsUnverifiedGeneratedObject(t *testing.T) {
	env := newCommitTestEnv(t)
	base := env.prepare(t, "run_unverified", nil)
	_, err := env.commit.CommitGeneratedAssetAndCharge(t.Context(), base.commitInput("idem-commit-unverified", base.estimate.LineItems[0].EstimateItemID, "local-"+base.artifactID))
	if codeOf(err) != bizerrors.CodeAssetSaveFailed {
		t.Fatalf("expected ASSET_SAVE_FAILED for local/unverified etag, got %v", err)
	}
	assertNoCommittedAsset(t, env.repo, base.artifactID)
}

func TestCommitBindsEstimateItemToCurrentFreeze(t *testing.T) {
	env := newCommitTestEnv(t)
	base := env.prepare(t, "run_cross_estimate", nil)
	other := env.prepare(t, "run_other_estimate", nil)
	_, err := env.commit.CommitGeneratedAssetAndCharge(t.Context(), base.commitInput("idem-commit-cross-estimate", other.estimate.LineItems[0].EstimateItemID, "uploaded-cross-estimate"))
	if codeOf(err) != bizerrors.CodeStateConflict {
		t.Fatalf("expected STATE_CONFLICT for cross-estimate item, got %v", err)
	}
	assertNoCommittedAsset(t, env.repo, base.artifactID)
}

func TestCommitRejectsToolEstimateItem(t *testing.T) {
	env := newCommitTestEnv(t)
	base := env.prepare(t, "run_tool_item", []credit.ToolUsageItem{{
		ToolName: "web_fetch", ToolType: "browser", BillingUnit: "call", Quantity: 1, MetadataSummary: map[string]string{"purpose": "test"},
	}})
	if len(base.estimate.LineItems) < 2 || base.estimate.LineItems[1].ItemType != "tool_usage" {
		t.Fatalf("expected tool usage estimate item, got %#v", base.estimate.LineItems)
	}
	_, err := env.commit.CommitGeneratedAssetAndCharge(t.Context(), base.commitInput("idem-commit-tool-item", base.estimate.LineItems[1].EstimateItemID, "uploaded-tool-item"))
	if codeOf(err) != bizerrors.CodeStateConflict {
		t.Fatalf("expected STATE_CONFLICT for tool estimate item, got %v", err)
	}
	assertNoCommittedAsset(t, env.repo, base.artifactID)
}

func TestCommitGeneratedAssetAndChargePersistsFullSuccessPath(t *testing.T) {
	env := newCommitTestEnv(t)
	base := env.prepare(t, "run_success", nil)
	estimateItem := base.estimate.LineItems[0]
	beforeAvailable, beforeFrozen := creditAccountBalance(t, env.repo, base.estimate.CreditAccountID)
	out, err := env.commit.CommitGeneratedAssetAndCharge(t.Context(), base.commitInput("idem-commit-success", estimateItem.EstimateItemID, "uploaded-success-etag"))
	if err != nil {
		t.Fatalf("commit generated asset: %v", err)
	}
	if out.CommitStatus != "committed" || out.ChargedPoints != estimateItem.EstimatePoints || out.ReleasedPoints != 0 || len(out.AssetRefs) != 1 {
		t.Fatalf("unexpected commit result: %#v", out)
	}
	if countRows(t, env.repo, &businesscore.Asset{}, "source_ref_id = ?", base.artifactID) != 1 {
		t.Fatalf("expected generated asset row")
	}
	if countRows(t, env.repo, &businesscore.AssetStorageObject{}, "object_key = ?", base.slot.ObjectKey) != 1 {
		t.Fatalf("expected storage object row")
	}
	if countRows(t, env.repo, &businesscore.ProjectAsset{}, "source_artifact_id = ?", base.artifactID) != 1 {
		t.Fatalf("expected project asset row")
	}
	if countRows(t, env.repo, &businesscore.AssetCommitItem{}, "artifact_id = ? AND estimate_item_id = ?", base.artifactID, estimateItem.EstimateItemID) != 1 {
		t.Fatalf("expected asset commit item row")
	}
	requireCommitOperatorColumns(t, env.repo, "assets", "id = ?", env.auth.UserID, env.auth.UserID, out.AssetRefs[0].AssetID)
	requireCommitOperatorColumns(t, env.repo, "asset_storage_objects", "asset_id = ?", env.auth.UserID, env.auth.UserID, out.AssetRefs[0].AssetID)
	requireCommitOperatorColumns(t, env.repo, "asset_elements", "asset_id = ? AND element_key = ?", env.auth.UserID, env.auth.UserID, out.AssetRefs[0].AssetID, base.artifactID)
	requireCommitOperatorColumns(t, env.repo, "project_assets", "source_artifact_id = ?", env.auth.UserID, env.auth.UserID, base.artifactID)
	requireCommitOperatorColumns(t, env.repo, "generated_asset_object_slots", "run_id = ? AND artifact_id = ?", env.auth.UserID, env.auth.UserID, base.runID, base.artifactID)
	requireCommitOperatorColumns(t, env.repo, "asset_commit_batches", "ledger_ref = ?", env.auth.UserID, env.auth.UserID, out.LedgerRef)
	requireCommitOperatorColumns(t, env.repo, "asset_commit_items", "artifact_id = ? AND estimate_item_id = ?", env.auth.UserID, env.auth.UserID, base.artifactID, estimateItem.EstimateItemID)
	requireCommitUpdatedBy(t, env.repo, "credit_accounts", "id = ?", env.auth.UserID, base.estimate.CreditAccountID)
	requireCommitOperatorColumns(t, env.repo, "credit_freezes", "freeze_id = ?", env.auth.UserID, env.auth.UserID, base.freeze.FreezeID)
	if countRows(t, env.repo, &businesscore.CreditLedgerEntry{}, "entry_type = ? AND source_type = ? AND source_id = ?", "asset_commit_charge", "asset_commit", out.LedgerRef) != 0 {
		t.Fatalf("ledger source_id should be commit_id, not ledger ref")
	}
	if countRows(t, env.repo, &businesscore.CreditLedgerEntry{}, "entry_type = ? AND source_type = ? AND points_delta = ?", "asset_commit_charge", "asset_commit", -estimateItem.EstimatePoints) != 1 {
		t.Fatalf("expected asset commit ledger charge")
	}
	var freeze businesscore.CreditFreeze
	if err := env.repo.DB().WithContext(t.Context()).Where("freeze_id = ?", base.freeze.FreezeID).First(&freeze).Error; err != nil {
		t.Fatalf("load freeze: %v", err)
	}
	if freeze.Status != "charged" || freeze.ChargedPoints != estimateItem.EstimatePoints || freeze.ReleasedPoints != 0 {
		t.Fatalf("unexpected freeze after commit: %#v", freeze)
	}
	afterAvailable, afterFrozen := creditAccountBalance(t, env.repo, base.estimate.CreditAccountID)
	if afterAvailable != beforeAvailable || afterFrozen != beforeFrozen-estimateItem.EstimatePoints {
		t.Fatalf("unexpected account balance after commit: before=(%d,%d) after=(%d,%d)", beforeAvailable, beforeFrozen, afterAvailable, afterFrozen)
	}
}

func TestCommitGeneratedAssetAndChargePartiallyCommitsAndReleasesMissingArtifact(t *testing.T) {
	env := newCommitTestEnv(t)
	base := env.prepare(t, "run_partial_commit", nil)
	secondEstimateItemID := appendSecondModelEstimateItem(t, env.repo, base)
	in := base.commitInput("idem-commit-partial", base.estimate.LineItems[0].EstimateItemID, "uploaded-partial-etag")
	in.Artifacts = append(in.Artifacts, CommitArtifactInput{
		ArtifactID: "art_missing_" + base.runID, ResourceType: "image", ElementType: "image_ref", EstimateItemID: secondEstimateItemID,
		ToolName: "model_generation", ToolType: "image", ChargeQuantity: 1,
		ArtifactSummary: map[string]string{"display_name": "missing"}, MetadataSummary: map[string]string{"display_name": "missing"},
		ContentURIDigest: "sha256:missing-content-uri",
		StorageObjectRef: StorageObjectRef{
			Bucket: base.slot.Bucket, ObjectKey: "missing/" + base.runID + ".png", ContentType: "image/png",
			SizeBytes: 128, Checksum: "sha256:missing-" + base.runID, Etag: "uploaded-missing-etag",
		},
	})

	out, err := env.commit.CommitGeneratedAssetAndCharge(t.Context(), in)
	if err != nil {
		t.Fatalf("partial commit should not fail when at least one artifact is committed: %v", err)
	}
	if out.CommitStatus != "partial_committed" || len(out.AssetRefs) != 1 {
		t.Fatalf("unexpected partial commit result: %#v", out)
	}
	if out.ChargedPoints != base.estimate.LineItems[0].EstimatePoints || out.ReleasedPoints != base.estimate.LineItems[0].EstimatePoints {
		t.Fatalf("expected one artifact charged and one released, got charged=%d released=%d", out.ChargedPoints, out.ReleasedPoints)
	}
	if len(out.ChargedLineItems) != 2 || out.ChargedLineItems[1].Status != "skipped" || out.ChargedLineItems[1].ChargedPoints != 0 {
		t.Fatalf("expected skipped line for missing artifact, got %#v", out.ChargedLineItems)
	}
	replay, err := env.commit.CommitGeneratedAssetAndCharge(t.Context(), in)
	if err != nil {
		t.Fatalf("partial commit replay: %v", err)
	}
	if replay.CommitStatus != out.CommitStatus || len(replay.ChargedLineItems) != 2 || replay.ChargedLineItems[1].Status != "skipped" {
		t.Fatalf("partial commit replay should preserve skipped line, got %#v", replay)
	}
	if countRows(t, env.repo, &businesscore.Asset{}, "source_ref_id = ?", base.artifactID) != 1 {
		t.Fatalf("expected first artifact committed")
	}
	if countRows(t, env.repo, &businesscore.Asset{}, "source_ref_id = ?", "art_missing_"+base.runID) != 0 {
		t.Fatalf("missing artifact must not create asset")
	}
	var freeze businesscore.CreditFreeze
	if err := env.repo.DB().WithContext(t.Context()).Where("freeze_id = ?", base.freeze.FreezeID).First(&freeze).Error; err != nil {
		t.Fatalf("load freeze: %v", err)
	}
	if freeze.Status != "charged" || freeze.ChargedPoints != out.ChargedPoints || freeze.ReleasedPoints != out.ReleasedPoints {
		t.Fatalf("unexpected freeze after partial commit: %#v", freeze)
	}
}

type commitTestEnv struct {
	repo   *businesscore.Repository
	credit *credit.App
	asset  *asset.App
	commit *App
	auth   accountspace.AuthContext
}

type commitBase struct {
	env        *commitTestEnv
	sessionID  string
	runID      string
	artifactID string
	checksum   string
	safety     *businessagent.SafetyEvidenceDTO
	estimate   credit.EstimateDTO
	freeze     credit.FreezeDTO
	slot       asset.GeneratedUploadSlotDTO
}

func newCommitTestEnv(t *testing.T) *commitTestEnv {
	t.Helper()
	db := testdb.StartPostgres(t, "dora_business_assetcommit_app")
	migrator := testdb.ApplyMigrations(t, db.URL, "db/migrations/iterations/2026-06-27-business-core/business")
	t.Cleanup(func() { testdb.DownMigrations(t, migrator) })
	testdb.ExecSQL(t, db.DB, testdb.MustReadSQL(t, "tests/business/seed/business_core_seed.sql"))
	repo := businesscore.New(db.DB)
	guard := idempotency.NewGuard(db.DB, time.Hour, time.Hour)
	auditWriter := auditlog.NewGormWriter(db.DB)
	return &commitTestEnv{
		repo:   repo,
		credit: credit.New(repo, guard, auditWriter),
		asset:  asset.New(repo, guard, auditWriter, asset.TOSOptions{Env: "local", Bucket: "dora-local", BaseURL: "http://localhost/tos"}),
		commit: New(repo, guard, auditWriter, MetadataObjectVerifier{}),
		auth:   accountspace.AuthContext{UserID: "usr_1001", SpaceID: "sp_personal_1001", LoginIdentityType: "personal"},
	}
}

func (e *commitTestEnv) prepare(t *testing.T, runID string, toolItems []credit.ToolUsageItem) commitBase {
	t.Helper()
	sessionID := "sess_" + runID
	traceID := "trace_" + runID
	safety := commitSafetyEvidence(traceID, sessionID, runID)
	estimate, err := e.credit.EstimateGenerationCredits(t.Context(), credit.EstimateGenerationInput{
		Auth: e.auth, Meta: commitMeta(traceID, "idem-estimate-"+runID), ProjectID: "prj_active_1001",
		ResourceType: "image", ModelID: "mdl_seed_image", PricingSnapshotID: "price_model_image_seed", Quantity: 1,
		ToolUsageItems: toolItems, SafetyEvidence: safety,
	})
	if err != nil {
		t.Fatalf("estimate credits: %v", err)
	}
	freeze, err := e.credit.FreezeCredits(t.Context(), credit.FreezeInput{
		Auth: e.auth, Meta: commitMeta(traceID, "idem-freeze-"+runID), EstimateID: estimate.EstimateID,
		Points: estimate.EstimatePoints, RunID: runID, ConfirmationID: "intr_" + runID, AccountID: estimate.CreditAccountID,
	})
	if err != nil {
		t.Fatalf("freeze credits: %v", err)
	}
	artifactID := "art_" + runID
	checksum := "sha256:artifact-" + runID
	slots, err := e.asset.PrepareGeneratedAssetObjects(t.Context(), e.auth, commitMeta(traceID, "idem-slots-"+runID), "prj_active_1001", sessionID, runID, []asset.GeneratedObjectInput{{
		ArtifactID: artifactID, ResourceType: "image", Filename: "generated.png", ContentType: "image/png",
		SizeBytes: 128, Checksum: checksum, MetadataSummary: map[string]string{"display_name": "generated"},
	}})
	if err != nil {
		t.Fatalf("prepare slots: %v", err)
	}
	return commitBase{env: e, sessionID: sessionID, runID: runID, artifactID: artifactID, checksum: checksum, safety: safety, estimate: estimate, freeze: freeze, slot: slots[0]}
}

func (b commitBase) commitInput(idem, estimateItemID, etag string) CommitInput {
	return CommitInput{
		Auth: b.env.auth, Meta: commitMeta("trace_"+b.runID, idem), ProjectID: "prj_active_1001",
		SessionID: b.sessionID, RunID: b.runID, FreezeID: b.freeze.FreezeID, EstimateID: b.estimate.EstimateID,
		SafetyEvidence: b.safety,
		Artifacts: []CommitArtifactInput{{
			ArtifactID: b.artifactID, ResourceType: "image", ElementType: "image_ref", EstimateItemID: estimateItemID,
			ToolName: "model_generation", ToolType: "image", ChargeQuantity: 1,
			ArtifactSummary: map[string]string{"display_name": "generated"}, MetadataSummary: map[string]string{"display_name": "generated"},
			ContentURIDigest: "sha256:content-uri",
			StorageObjectRef: StorageObjectRef{
				Bucket: b.slot.Bucket, ObjectKey: b.slot.ObjectKey, ContentType: "image/png",
				SizeBytes: 128, Checksum: b.checksum, Etag: etag,
			},
		}},
		FinalElements: []FinalElementInput{{ElementType: "image_ref", ElementPayloadJSON: `{"artifact_id":"` + b.artifactID + `"}`, DisplayOrder: 1}},
	}
}

func commitMeta(traceID, idem string) accountspace.RequestMeta {
	return accountspace.RequestMeta{RequestID: "req-" + idem, TraceID: traceID, IdempotencyKey: idem, Source: "test"}
}

func commitSafetyEvidence(traceID, sessionID, runID string) *businessagent.SafetyEvidenceDTO {
	expires := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
	return &businessagent.SafetyEvidenceDTO{
		SafetyEvidenceId: "safe_" + runID, Scene: "generation", Result_: "passed", TargetType: "prompt",
		EvaluatedObjectDigest: "sha256:test-prompt-" + runID, PolicyVersion: "test-policy",
		EvidenceVersion: "2026-06-28", EvaluatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		ExpiresAt: &expires, SourceSessionId: &sessionID, SourceRunId: &runID, TraceId: traceID,
	}
}

func assertNoCommittedAsset(t *testing.T, repo *businesscore.Repository, artifactID string) {
	t.Helper()
	var count int64
	if err := repo.DB().Model(&businesscore.Asset{}).Where("source_ref_id = ?", artifactID).Count(&count).Error; err != nil {
		t.Fatalf("count committed assets: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no committed assets for %s, found %d", artifactID, count)
	}
}

func countRows(t *testing.T, repo *businesscore.Repository, model any, query string, args ...any) int64 {
	t.Helper()
	var count int64
	if err := repo.DB().Model(model).Where(query, args...).Count(&count).Error; err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func creditAccountBalance(t *testing.T, repo *businesscore.Repository, accountID string) (available, frozen int64) {
	t.Helper()
	var account businesscore.CreditAccount
	if err := repo.DB().Where("id = ?", accountID).First(&account).Error; err != nil {
		t.Fatalf("load credit account: %v", err)
	}
	return account.AvailablePoints, account.FrozenPoints
}

func codeOf(err error) bizerrors.Code {
	if err == nil {
		return ""
	}
	if businessErr, ok := err.(*bizerrors.BusinessError); ok {
		return businessErr.Code
	}
	return ""
}

func appendSecondModelEstimateItem(t *testing.T, repo *businesscore.Repository, base commitBase) string {
	t.Helper()
	now := time.Now().UTC()
	first := base.estimate.LineItems[0]
	secondID := "est_item_partial_" + base.runID
	if err := repo.DB().WithContext(t.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&businesscore.CreditEstimateItem{
			ID: security.RandomID("cesti_"), EstimateID: base.estimate.EstimateID, EstimateItemID: secondID, ItemType: "model_generation",
			ModelID: optionalString(first.ModelID), ResourceType: optionalString(first.ResourceType), BillingUnit: optionalString(first.BillingUnit),
			Quantity: optionalFloatForTest(1), UnitPoints: optionalFloatForTest(first.UnitPoints), EstimatePoints: first.EstimatePoints,
			Status: "estimated", MetadataJSON: mustJSON(map[string]any{"order": 99, "metadata_summary": map[string]string{"test": "partial"}}), CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.CreditEstimate{}).Where("estimate_id = ?", base.estimate.EstimateID).Updates(map[string]any{
			"estimate_points": base.estimate.EstimatePoints + first.EstimatePoints,
			"updated_at":      now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.CreditFreeze{}).Where("freeze_id = ?", base.freeze.FreezeID).Updates(map[string]any{
			"frozen_points": base.freeze.FrozenPoints + first.EstimatePoints,
			"updated_at":    now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.CreditAccount{}).Where("id = ?", base.estimate.CreditAccountID).Updates(map[string]any{
			"available_points": gorm.Expr("available_points - ?", first.EstimatePoints),
			"frozen_points":    gorm.Expr("frozen_points + ?", first.EstimatePoints),
			"updated_at":       now,
		}).Error; err != nil {
			return err
		}
		var freezeItem businesscore.CreditFreezeBatchItem
		if err := tx.Where("freeze_id = ?", base.freeze.FreezeID).Order("created_at ASC").First(&freezeItem).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.CreditFreezeBatchItem{}).Where("id = ?", freezeItem.ID).Updates(map[string]any{
			"frozen_points": gorm.Expr("frozen_points + ?", first.EstimatePoints),
			"updated_at":    now,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&businesscore.CreditBatch{}).Where("id = ?", freezeItem.BatchID).Updates(map[string]any{
			"remaining_points": gorm.Expr("remaining_points - ?", first.EstimatePoints),
			"updated_at":       now,
		}).Error
	}); err != nil {
		t.Fatalf("append second estimate item: %v", err)
	}
	return secondID
}

func optionalFloatForTest(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}

func requireCommitOperatorColumns(t *testing.T, repo *businesscore.Repository, table string, where string, wantCreatedBy string, wantUpdatedBy string, args ...any) {
	t.Helper()
	var row struct {
		CreatedBy *string `gorm:"column:created_by"`
		UpdatedBy *string `gorm:"column:updated_by"`
	}
	tx := repo.DB().Raw("SELECT created_by, updated_by FROM "+table+" WHERE "+where+" ORDER BY created_at DESC LIMIT 1", args...).Scan(&row)
	if tx.Error != nil {
		t.Fatalf("query operator columns for %s: %v", table, tx.Error)
	}
	if tx.RowsAffected == 0 {
		t.Fatalf("expected row in %s where %s", table, where)
	}
	if value(row.CreatedBy) != wantCreatedBy || value(row.UpdatedBy) != wantUpdatedBy {
		t.Fatalf("unexpected operator columns in %s: created_by=%q updated_by=%q", table, value(row.CreatedBy), value(row.UpdatedBy))
	}
}

func requireCommitUpdatedBy(t *testing.T, repo *businesscore.Repository, table string, where string, wantUpdatedBy string, args ...any) {
	t.Helper()
	var row struct {
		UpdatedBy *string `gorm:"column:updated_by"`
	}
	tx := repo.DB().Raw("SELECT updated_by FROM "+table+" WHERE "+where+" ORDER BY created_at DESC LIMIT 1", args...).Scan(&row)
	if tx.Error != nil {
		t.Fatalf("query updated_by for %s: %v", table, tx.Error)
	}
	if tx.RowsAffected == 0 {
		t.Fatalf("expected row in %s where %s", table, where)
	}
	if value(row.UpdatedBy) != wantUpdatedBy {
		t.Fatalf("unexpected updated_by in %s: %q", table, value(row.UpdatedBy))
	}
}
