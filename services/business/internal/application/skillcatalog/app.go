package skillcatalog

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	statusActive     = "active"
	statusDraft      = "draft"
	statusSubmitted  = "submitted"
	statusPublished  = "published"
	statusDeprecated = "deprecated"
)

type App struct {
	repo *businesscore.Repository
	now  func() time.Time
}

func New(repo *businesscore.Repository) *App {
	return &App{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type SkillSummaryDTO struct {
	SkillID    string            `json:"skill_id"`
	SkillName  string            `json:"skill_name"`
	SkillScope string            `json:"skill_scope"`
	Version    string            `json:"version"`
	Status     string            `json:"status"`
	RouteHints map[string]string `json:"route_hints,omitempty"`
}

type SkillSpecDTO struct {
	SkillID                    string   `json:"skill_id"`
	Version                    string   `json:"version"`
	SkillSpecJSON              string   `json:"skill_spec_json"`
	OutputSchemaJSON           string   `json:"output_schema_json"`
	ToolRefs                   []string `json:"tool_refs"`
	MemoryPolicyJSON           string   `json:"memory_policy_json,omitempty"`
	ConfirmationPolicyJSON     string   `json:"confirmation_policy_json"`
	ExecutionPolicySummaryJSON string   `json:"execution_policy_summary_json"`
}

type ReviewCandidateDTO struct {
	SkillID                string   `json:"skill_id"`
	VersionID              string   `json:"version_id"`
	SkillSpecJSON          string   `json:"skill_spec_json"`
	InputSchemaJSON        string   `json:"input_schema_json"`
	OutputSchemaJSON       string   `json:"output_schema_json"`
	ToolRefs               []string `json:"tool_refs"`
	MemoryPolicyJSON       string   `json:"memory_policy_json"`
	ConfirmationPolicyJSON string   `json:"confirmation_policy_json"`
	TestInputJSON          string   `json:"test_input_json,omitempty"`
	ExpectedElementsJSON   string   `json:"expected_elements_json,omitempty"`
}

type SkillDetailDTO struct {
	SkillID            string            `json:"skill_id"`
	SkillKey           string            `json:"skill_key"`
	SkillName          string            `json:"skill_name"`
	SkillScope         string            `json:"skill_scope"`
	Status             string            `json:"status"`
	PublishedVersionID string            `json:"published_version_id,omitempty"`
	RouteHints         map[string]string `json:"route_hints,omitempty"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

type TestResultDTO struct {
	TestRunID string `json:"test_run_id"`
	Status    string `json:"status"`
	Saved     bool   `json:"saved"`
}

type Page[T any] struct {
	Items  []T `json:"items"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func (a *App) ListRoutableSkills(ctx context.Context, auth accountspace.AuthContext, scopeFilter string, limit int, cursor string) ([]SkillSummaryDTO, string, error) {
	limit = clampLimit(limit, 10, 50)
	offset := cursorOffset(cursor)
	db := a.repo.DB().WithContext(ctx).
		Where("status = ? AND published_version_id IS NOT NULL", statusPublished).
		Where("(skill_scope = ? OR owner_user_id = ? OR enterprise_id = ?)", "public", auth.UserID, auth.EnterpriseID).
		Order("updated_at DESC, id ASC").
		Limit(limit + 1).
		Offset(offset)
	if strings.TrimSpace(scopeFilter) != "" {
		db = db.Where("skill_scope = ?", strings.TrimSpace(scopeFilter))
	}
	var rows []businesscore.Skill
	if err := db.Find(&rows).Error; err != nil {
		return nil, "", err
	}
	out := make([]SkillSummaryDTO, 0, min(len(rows), limit))
	for i, skill := range rows {
		if i >= limit {
			break
		}
		version, err := a.versionByID(ctx, value(skill.PublishedVersionID))
		if err != nil || version.Status != statusPublished {
			continue
		}
		out = append(out, SkillSummaryDTO{
			SkillID: skill.ID, SkillName: skill.SkillName, SkillScope: skill.SkillScope,
			Version: version.Version, Status: skill.Status, RouteHints: stringMap(skill.RouteHintsJSON),
		})
	}
	nextCursor := ""
	if len(rows) > limit {
		nextCursor = encodeCursor(offset + limit)
	}
	return out, nextCursor, nil
}

func (a *App) GetPublishedSkillSpec(ctx context.Context, auth accountspace.AuthContext, skillID, version string) (SkillSpecDTO, error) {
	skill, err := a.visibleSkill(ctx, auth, skillID)
	if err != nil {
		return SkillSpecDTO{}, err
	}
	if skill.Status != statusPublished {
		return SkillSpecDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "published skill not found")
	}
	var sv businesscore.SkillVersion
	if strings.TrimSpace(version) == "" {
		sv, err = a.versionByID(ctx, value(skill.PublishedVersionID))
	} else {
		err = a.repo.DB().WithContext(ctx).Where("skill_id = ? AND version = ? AND status = ?", skill.ID, version, statusPublished).First(&sv).Error
	}
	if err != nil || sv.Status != statusPublished {
		return SkillSpecDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "published skill version not found")
	}
	toolRefs, err := a.toolRefs(ctx, skill.ID, sv.ID)
	if err != nil {
		return SkillSpecDTO{}, err
	}
	return SkillSpecDTO{
		SkillID: skill.ID, Version: sv.Version, SkillSpecJSON: string(sv.SkillSpecJSON),
		OutputSchemaJSON: string(sv.OutputSchemaJSON), ToolRefs: toolRefs, MemoryPolicyJSON: string(sv.MemoryPolicyJSON),
		ConfirmationPolicyJSON: `{"requires_confirmation":false}`, ExecutionPolicySummaryJSON: executionSummary(toolRefs),
	}, nil
}

func (a *App) GetReviewCandidateSkillSpec(ctx context.Context, _ accountspace.AuthContext, skillID, versionID, testCaseID string) (ReviewCandidateDTO, error) {
	var sv businesscore.SkillVersion
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND skill_id = ?", versionID, skillID).First(&sv).Error; err != nil {
		return ReviewCandidateDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "skill version not found")
	}
	toolRefs, err := a.toolRefs(ctx, skillID, sv.ID)
	if err != nil {
		return ReviewCandidateDTO{}, err
	}
	dto := ReviewCandidateDTO{
		SkillID: skillID, VersionID: sv.ID, SkillSpecJSON: string(sv.SkillSpecJSON),
		InputSchemaJSON: string(sv.InputSchemaJSON), OutputSchemaJSON: string(sv.OutputSchemaJSON),
		ToolRefs: toolRefs, MemoryPolicyJSON: string(sv.MemoryPolicyJSON), ConfirmationPolicyJSON: `{"requires_confirmation":false}`,
	}
	if strings.TrimSpace(testCaseID) != "" {
		var testCase businesscore.SkillTestCase
		if err := a.repo.DB().WithContext(ctx).Where("id = ? AND skill_id = ? AND version_id = ? AND status = ?", testCaseID, skillID, sv.ID, statusActive).First(&testCase).Error; err != nil {
			return ReviewCandidateDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "skill test case not found")
		}
		dto.TestInputJSON = string(testCase.TestInputJSON)
		dto.ExpectedElementsJSON = string(testCase.ExpectedElementsJSON)
	}
	return dto, nil
}

func (a *App) SaveSkillTestResult(ctx context.Context, auth accountspace.AuthContext, skillID, versionID, testRunID, testCaseID, status, actualElementsJSON, errorCode, errorSummary, safetyEvidenceJSON, agentTraceID string) (TestResultDTO, error) {
	if strings.TrimSpace(skillID) == "" || strings.TrimSpace(versionID) == "" || strings.TrimSpace(testRunID) == "" {
		return TestResultDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "skill_id, version_id and test_run_id are required")
	}
	now := a.now()
	if !json.Valid([]byte(actualElementsJSON)) {
		return TestResultDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "actual_elements_json must be json")
	}
	if strings.TrimSpace(safetyEvidenceJSON) == "" {
		safetyEvidenceJSON = "{}"
	}
	if !json.Valid([]byte(safetyEvidenceJSON)) {
		return TestResultDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "safety_evidence_json must be json")
	}
	var testCasePtr *string
	var inputJSON datatypes.JSON = datatypes.JSON([]byte("{}"))
	if strings.TrimSpace(testCaseID) != "" {
		testCasePtr = &testCaseID
		var testCase businesscore.SkillTestCase
		if err := a.repo.DB().WithContext(ctx).Where("id = ? AND skill_id = ? AND version_id = ?", testCaseID, skillID, versionID).First(&testCase).Error; err == nil {
			inputJSON = testCase.TestInputJSON
		}
	}
	runStatus := normalizeStatus(status, "created")
	row := businesscore.SkillTestRun{
		ID: testRunID, SkillID: skillID, VersionID: versionID, TestCaseID: testCasePtr, Status: runStatus,
		ExecutionMode: "sandbox", InputJSON: inputJSON, ActualElementsJSON: datatypes.JSON([]byte(actualElementsJSON)),
		SafetyEvidenceJSON: datatypes.JSON([]byte(safetyEvidenceJSON)), ErrorCode: optionalString(errorCode), ErrorSummary: optionalString(errorSummary),
		AgentTraceID: optionalString(agentTraceID), FinishedAt: &now, CreatedByUserID: optionalString(auth.UserID), CreatedAt: now, UpdatedAt: now,
	}
	err := a.repo.DB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"status", "test_case_id", "input_json", "actual_elements_json", "safety_evidence_json",
			"error_code", "error_summary", "agent_trace_id", "finished_at", "updated_at",
		}),
	}).Create(&row).Error
	if err != nil {
		return TestResultDTO{}, err
	}
	return TestResultDTO{TestRunID: testRunID, Status: runStatus, Saved: true}, nil
}

func (a *App) ListSkills(ctx context.Context, auth accountspace.AuthContext, status string, limit, offset int) (Page[SkillDetailDTO], error) {
	limit = clampLimit(limit, 10, 100)
	db := a.repo.DB().WithContext(ctx).
		Where("(skill_scope = ? OR owner_user_id = ? OR enterprise_id = ?)", "public", auth.UserID, auth.EnterpriseID).
		Order("updated_at DESC, id ASC").Limit(limit).Offset(nonNegative(offset))
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var rows []businesscore.Skill
	if err := db.Find(&rows).Error; err != nil {
		return Page[SkillDetailDTO]{}, err
	}
	out := make([]SkillDetailDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, skillDTO(row))
	}
	return Page[SkillDetailDTO]{Items: out, Limit: limit, Offset: nonNegative(offset)}, nil
}

type SaveSkillInput struct {
	Auth             accountspace.AuthContext
	SkillID          string
	SkillKey         string
	SkillName        string
	SkillScope       string
	RouteHints       map[string]string
	Version          string
	SkillSpecJSON    string
	InputSchemaJSON  string
	OutputSchemaJSON string
	MemoryPolicyJSON string
}

func (a *App) SaveSkill(ctx context.Context, in SaveSkillInput) (SkillDetailDTO, error) {
	if in.Auth.UserID == "" {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "user auth is required")
	}
	if strings.TrimSpace(in.SkillKey) == "" || strings.TrimSpace(in.SkillName) == "" {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "skill_key and skill_name are required")
	}
	if !validJSONOrEmpty(in.SkillSpecJSON) || !validJSONOrEmpty(in.InputSchemaJSON) || !validJSONOrEmpty(in.OutputSchemaJSON) {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "skill schemas must be json")
	}
	now := a.now()
	skillID := strings.TrimSpace(in.SkillID)
	if skillID == "" {
		skillID = security.RandomID("sk_")
	}
	scope := normalizeStatus(in.SkillScope, "personal")
	var enterpriseID *string
	if scope == "enterprise" && in.Auth.EnterpriseID != "" {
		enterpriseID = &in.Auth.EnterpriseID
	}
	userID := in.Auth.UserID
	skill := businesscore.Skill{
		ID: skillID, SkillKey: in.SkillKey, SkillName: in.SkillName, SkillScope: scope, OwnerUserID: &userID, EnterpriseID: enterpriseID,
		Status: statusDraft, RouteHintsJSON: mustJSON(in.RouteHints), CreatedByUserID: &userID, CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.DB().WithContext(ctx).Save(&skill).Error; err != nil {
		return SkillDetailDTO{}, err
	}
	versionID := security.RandomID("skv_")
	version := normalizeStatus(in.Version, "0.1.0")
	memory := normalizeJSON(in.MemoryPolicyJSON, `{"enabled":true}`)
	sv := businesscore.SkillVersion{
		ID: versionID, SkillID: skill.ID, Version: version, Status: statusDraft, SkillSpecJSON: datatypes.JSON([]byte(normalizeJSON(in.SkillSpecJSON, "{}"))),
		InputSchemaJSON: datatypes.JSON([]byte(normalizeJSON(in.InputSchemaJSON, "{}"))), OutputSchemaJSON: datatypes.JSON([]byte(normalizeJSON(in.OutputSchemaJSON, "{}"))),
		MemoryPolicyJSON: datatypes.JSON([]byte(memory)), SubmittedByUserID: &userID, CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.DB().WithContext(ctx).Create(&sv).Error; err != nil {
		return SkillDetailDTO{}, err
	}
	return skillDTO(skill), nil
}

func (a *App) GetSkill(ctx context.Context, auth accountspace.AuthContext, skillID string) (SkillDetailDTO, error) {
	skill, err := a.visibleSkill(ctx, auth, skillID)
	if err != nil {
		return SkillDetailDTO{}, err
	}
	return skillDTO(skill), nil
}

func (a *App) SubmitReview(ctx context.Context, auth accountspace.AuthContext, skillID string) (map[string]any, error) {
	skill, err := a.visibleSkill(ctx, auth, skillID)
	if err != nil {
		return nil, err
	}
	var version businesscore.SkillVersion
	if err := a.repo.DB().WithContext(ctx).Where("skill_id = ? AND status = ?", skill.ID, statusDraft).Order("updated_at DESC").First(&version).Error; err != nil {
		return nil, bizerrors.New(bizerrors.CodeResourceNotFound, "draft version not found")
	}
	now := a.now()
	version.Status = statusSubmitted
	version.SubmittedAt = &now
	version.UpdatedAt = now
	if err := a.repo.DB().WithContext(ctx).Save(&version).Error; err != nil {
		return nil, err
	}
	return map[string]any{"skill_id": skill.ID, "version_id": version.ID, "review_status": statusSubmitted}, nil
}

func (a *App) Rollback(ctx context.Context, auth accountspace.AuthContext, skillID string) (SkillDetailDTO, error) {
	skill, err := a.visibleSkill(ctx, auth, skillID)
	if err != nil {
		return SkillDetailDTO{}, err
	}
	if skill.PublishedVersionID == nil {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "published version is required")
	}
	skill.Status = statusPublished
	skill.UpdatedAt = a.now()
	if err := a.repo.DB().WithContext(ctx).Save(&skill).Error; err != nil {
		return SkillDetailDTO{}, err
	}
	return skillDTO(skill), nil
}

func (a *App) ListSystemSkills(ctx context.Context, _ admin.AdminAuth, status string, limit, offset int) (Page[SkillDetailDTO], error) {
	limit = clampLimit(limit, 10, 100)
	db := a.repo.DB().WithContext(ctx).Order("updated_at DESC, id ASC").Limit(limit).Offset(nonNegative(offset))
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var rows []businesscore.Skill
	if err := db.Find(&rows).Error; err != nil {
		return Page[SkillDetailDTO]{}, err
	}
	out := make([]SkillDetailDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, skillDTO(row))
	}
	return Page[SkillDetailDTO]{Items: out, Limit: limit, Offset: nonNegative(offset)}, nil
}

func (a *App) Publish(ctx context.Context, auth admin.AdminAuth, skillID, versionID string) (SkillDetailDTO, error) {
	if auth.AdminID == "" {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	var skill businesscore.Skill
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", skillID).First(&skill).Error; err != nil {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "skill not found")
	}
	var version businesscore.SkillVersion
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND skill_id = ?", versionID, skillID).First(&version).Error; err != nil {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "skill version not found")
	}
	var caseCount int64
	if err := a.repo.DB().WithContext(ctx).Model(&businesscore.SkillTestCase{}).Where("skill_id = ? AND version_id = ? AND status = ?", skillID, versionID, statusActive).Count(&caseCount).Error; err != nil {
		return SkillDetailDTO{}, err
	}
	if caseCount < 3 {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "at least 3 active skill test cases are required")
	}
	now := a.now()
	tx := a.repo.DB().WithContext(ctx).Begin()
	if err := tx.Model(&businesscore.SkillVersion{}).Where("skill_id = ? AND status = ?", skillID, statusPublished).Updates(map[string]any{"status": statusDeprecated, "updated_at": now}).Error; err != nil {
		tx.Rollback()
		return SkillDetailDTO{}, err
	}
	version.Status = statusPublished
	version.ReviewedByAdminID = &auth.AdminID
	version.ReviewedAt = &now
	version.PublishedAt = &now
	version.UpdatedAt = now
	if err := tx.Save(&version).Error; err != nil {
		tx.Rollback()
		return SkillDetailDTO{}, err
	}
	skill.Status = statusPublished
	skill.PublishedVersionID = &version.ID
	skill.UpdatedAt = now
	if err := tx.Save(&skill).Error; err != nil {
		tx.Rollback()
		return SkillDetailDTO{}, err
	}
	if err := tx.Create(&businesscore.SkillReviewRecord{ID: security.RandomID("skr_"), SkillID: skillID, VersionID: versionID, ReviewAction: "publish", ReviewStatus: "approved", ReviewedByAdminID: auth.AdminID, CreatedAt: now}).Error; err != nil {
		tx.Rollback()
		return SkillDetailDTO{}, err
	}
	if err := tx.Commit().Error; err != nil {
		return SkillDetailDTO{}, err
	}
	return skillDTO(skill), nil
}

func (a *App) Deprecate(ctx context.Context, auth admin.AdminAuth, skillID string) (SkillDetailDTO, error) {
	if auth.AdminID == "" {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	var skill businesscore.Skill
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", skillID).First(&skill).Error; err != nil {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "skill not found")
	}
	skill.Status = statusDeprecated
	skill.UpdatedAt = a.now()
	if err := a.repo.DB().WithContext(ctx).Save(&skill).Error; err != nil {
		return SkillDetailDTO{}, err
	}
	return skillDTO(skill), nil
}

func (a *App) ListReviews(ctx context.Context, _ admin.AdminAuth, limit, offset int) (Page[ReviewCandidateDTO], error) {
	limit = clampLimit(limit, 10, 100)
	var versions []businesscore.SkillVersion
	if err := a.repo.DB().WithContext(ctx).Where("status = ?", statusSubmitted).Order("submitted_at DESC, id ASC").Limit(limit).Offset(nonNegative(offset)).Find(&versions).Error; err != nil {
		return Page[ReviewCandidateDTO]{}, err
	}
	out := make([]ReviewCandidateDTO, 0, len(versions))
	for _, version := range versions {
		item, err := a.GetReviewCandidateSkillSpec(ctx, accountspace.AuthContext{}, version.SkillID, version.ID, "")
		if err == nil {
			out = append(out, item)
		}
	}
	return Page[ReviewCandidateDTO]{Items: out, Limit: limit, Offset: nonNegative(offset)}, nil
}

func (a *App) ConfirmReview(ctx context.Context, auth admin.AdminAuth, reviewID, action, comment string) (SkillDetailDTO, error) {
	if action == "reject" {
		return a.rejectReview(ctx, auth, reviewID, comment)
	}
	var version businesscore.SkillVersion
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", reviewID).First(&version).Error; err != nil {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "review candidate not found")
	}
	return a.Publish(ctx, auth, version.SkillID, version.ID)
}

func (a *App) rejectReview(ctx context.Context, auth admin.AdminAuth, versionID, comment string) (SkillDetailDTO, error) {
	if auth.AdminID == "" {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	var version businesscore.SkillVersion
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", versionID).First(&version).Error; err != nil {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "review candidate not found")
	}
	now := a.now()
	version.Status = statusDraft
	version.ReviewedByAdminID = &auth.AdminID
	version.ReviewedAt = &now
	version.UpdatedAt = now
	if err := a.repo.DB().WithContext(ctx).Save(&version).Error; err != nil {
		return SkillDetailDTO{}, err
	}
	commentPtr := optionalString(comment)
	if err := a.repo.DB().WithContext(ctx).Create(&businesscore.SkillReviewRecord{ID: security.RandomID("skr_"), SkillID: version.SkillID, VersionID: version.ID, ReviewAction: "reject", ReviewStatus: "rejected", ReviewComment: commentPtr, ReviewedByAdminID: auth.AdminID, CreatedAt: now}).Error; err != nil {
		return SkillDetailDTO{}, err
	}
	var skill businesscore.Skill
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", version.SkillID).First(&skill).Error; err != nil {
		return SkillDetailDTO{}, err
	}
	return skillDTO(skill), nil
}

func (a *App) visibleSkill(ctx context.Context, auth accountspace.AuthContext, skillID string) (businesscore.Skill, error) {
	var skill businesscore.Skill
	err := a.repo.DB().WithContext(ctx).
		Where("id = ?", skillID).
		Where("(skill_scope = ? OR owner_user_id = ? OR enterprise_id = ?)", "public", auth.UserID, auth.EnterpriseID).
		First(&skill).Error
	if err != nil {
		return businesscore.Skill{}, bizerrors.New(bizerrors.CodeResourceNotFound, "skill not found")
	}
	return skill, nil
}

func (a *App) versionByID(ctx context.Context, versionID string) (businesscore.SkillVersion, error) {
	var version businesscore.SkillVersion
	if versionID == "" {
		return businesscore.SkillVersion{}, gorm.ErrRecordNotFound
	}
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", versionID).First(&version).Error; err != nil {
		return businesscore.SkillVersion{}, err
	}
	return version, nil
}

func (a *App) toolRefs(ctx context.Context, skillID, versionID string) ([]string, error) {
	var rows []businesscore.SkillToolBinding
	if err := a.repo.DB().WithContext(ctx).Where("skill_id = ? AND version_id = ?", skillID, versionID).Order("required DESC, tool_name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ToolName+":"+row.ToolType)
	}
	return out, nil
}

func skillDTO(row businesscore.Skill) SkillDetailDTO {
	versionID := ""
	if row.PublishedVersionID != nil {
		versionID = *row.PublishedVersionID
	}
	return SkillDetailDTO{
		SkillID: row.ID, SkillKey: row.SkillKey, SkillName: row.SkillName, SkillScope: row.SkillScope,
		Status: row.Status, PublishedVersionID: versionID, RouteHints: stringMap(row.RouteHintsJSON), UpdatedAt: row.UpdatedAt,
	}
}

func stringMap(raw datatypes.JSON) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for key, value := range values {
		if text, ok := value.(string); ok {
			out[key] = text
		}
	}
	return out
}

func mustJSON(value any) datatypes.JSON {
	if value == nil {
		return datatypes.JSON([]byte("{}"))
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(encoded)
}

func validJSONOrEmpty(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || json.Valid([]byte(value))
}

func normalizeJSON(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func executionSummary(toolRefs []string) string {
	encoded, _ := json.Marshal(map[string]any{"tool_refs": toolRefs})
	return string(encoded)
}

func clampLimit(value, fallback, max int) int {
	if value <= 0 {
		value = fallback
	}
	if value > max {
		value = max
	}
	return value
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func cursorOffset(cursor string) int {
	var offset int
	if cursor == "" || json.Unmarshal([]byte(cursor), &offset) != nil || offset < 0 {
		return 0
	}
	return offset
}

func encodeCursor(offset int) string {
	encoded, _ := json.Marshal(offset)
	return string(encoded)
}

func normalizeStatus(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	value = strings.TrimSpace(value)
	return &value
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
