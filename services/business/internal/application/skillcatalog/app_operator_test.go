package skillcatalog

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetdict"
)

func TestSkillCatalogOperatorColumnsAreFilledFromAuth(t *testing.T) {
	app := newSkillTestApp(t)
	app.SetDictionary(assetdict.New(app.repo))
	auth := accountspace.AuthContext{UserID: "usr_1001", LoginIdentityType: "personal"}

	detail, err := app.SaveSkill(t.Context(), SaveSkillInput{
		Auth: auth, SkillKey: "operator_skill", SkillName: "Operator Skill", SkillScope: "personal", Version: "0.1.0",
		SkillSpecJSON: `{"name":"operator"}`, InputSchemaJSON: `{}`, OutputSchemaJSON: `{}`,
		OutputElements: []OutputElementInput{{
			ElementType: "image_ref", ElementName: "Image", Required: true, UseDraft: true, UseFinal: true, Editable: true, Referable: true,
		}},
	})
	if err != nil {
		t.Fatalf("save skill: %v", err)
	}
	requireSkillOperatorColumns(t, app, "skills", "id = ?", auth.UserID, auth.UserID, detail.SkillID)

	versionID := latestVersionID(t, app, detail.SkillID)
	requireSkillOperatorColumns(t, app, "skill_versions", "id = ?", auth.UserID, auth.UserID, versionID)
	requireSkillOperatorColumns(t, app, "skill_output_element_schemas", "skill_id = ? AND version_id = ?", auth.UserID, auth.UserID, detail.SkillID, versionID)

	evidence := skillTestEvidence("skrun_operator", "trace-skill-operator")
	if _, err := app.SaveSkillTestResult(t.Context(), auth, detail.SkillID, versionID, "skrun_operator", "", "skill_test:skrun_operator", "passed", `[]`, "", "", evidence, "trace-skill-operator"); err != nil {
		t.Fatalf("save skill test result: %v", err)
	}
	requireSkillOperatorColumns(t, app, "skill_test_runs", "id = ?", auth.UserID, auth.UserID, "skrun_operator")

	submitted, err := app.SubmitReview(t.Context(), auth, detail.SkillID)
	if err != nil {
		t.Fatalf("submit review: %v", err)
	}
	requireSkillOperatorColumns(t, app, "skill_versions", "id = ?", auth.UserID, auth.UserID, submitted["version_id"])

	adminAuth := admin.AdminAuth{AdminID: "adm_root"}
	if _, err := app.ConfirmReview(t.Context(), adminAuth, versionID, "reject", "needs changes"); err != nil {
		t.Fatalf("reject review: %v", err)
	}
	requireSkillOperatorColumns(t, app, "skill_versions", "id = ?", auth.UserID, adminAuth.AdminID, versionID)
	requireSkillOperatorColumns(t, app, "skill_review_records", "version_id = ? AND review_action = ?", adminAuth.AdminID, adminAuth.AdminID, versionID, "reject")

	if _, err := app.Publish(t.Context(), adminAuth, "sk_seed_storyboard", "skv_seed_storyboard_100"); err != nil {
		t.Fatalf("publish seed skill: %v", err)
	}
	requireSkillOperatorColumns(t, app, "skills", "id = ?", "", adminAuth.AdminID, "sk_seed_storyboard")
	requireSkillOperatorColumns(t, app, "skill_versions", "id = ?", "", adminAuth.AdminID, "skv_seed_storyboard_100")
	requireSkillOperatorColumns(t, app, "skill_review_records", "version_id = ? AND review_action = ?", adminAuth.AdminID, adminAuth.AdminID, "skv_seed_storyboard_100", "publish")
}

func latestVersionID(t *testing.T, app *App, skillID string) string {
	t.Helper()
	var row struct {
		ID string `gorm:"column:id"`
	}
	if err := app.repo.DB().Raw("SELECT id FROM skill_versions WHERE skill_id = ? ORDER BY created_at DESC LIMIT 1", skillID).Scan(&row).Error; err != nil {
		t.Fatalf("load latest version: %v", err)
	}
	if row.ID == "" {
		t.Fatalf("expected version for skill %s", skillID)
	}
	return row.ID
}

func skillTestEvidence(testRunID, traceID string) string {
	expires := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339Nano)
	return `{"scene":"skill_test","target_type":"skill_test_prompt","target_ref_id":"` + testRunID + `","evaluated_object_digest":"sha256:skill-test","policy_version":"test-policy","evidence_version":"2026-06-28","result":"passed","source_run_id":"` + testRunID + `","trace_id":"` + traceID + `","expires_at":"` + expires + `"}`
}

func requireSkillOperatorColumns(t *testing.T, app *App, table string, where string, wantCreatedBy string, wantUpdatedBy string, args ...any) {
	t.Helper()
	var row struct {
		CreatedBy *string `gorm:"column:created_by"`
		UpdatedBy *string `gorm:"column:updated_by"`
	}
	tx := app.repo.DB().Raw("SELECT created_by, updated_by FROM "+table+" WHERE "+where+" ORDER BY created_at DESC LIMIT 1", args...).Scan(&row)
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
