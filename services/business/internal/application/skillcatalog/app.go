package skillcatalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetdict"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	statusActive     = "active"
	statusDraft      = "draft"
	statusSubmitted  = "submitted"
	statusPublished  = "published"
	statusDeprecated = "deprecated"

	defaultConfirmationPolicyJSON = `{"requires_confirmation":false}`
)

var (
	markdownMentionPattern = regexp.MustCompile(`@([A-Za-z0-9_.:-]+)`)
	toolTagPattern         = regexp.MustCompile(`(?is)<tool\s+[^>]*id\s*=\s*["']([^"']+)["'][^>]*>.*?</tool>`)
	aguiTagPattern         = regexp.MustCompile(`(?is)<(?:agui|ag-ui)\s+[^>]*id\s*=\s*["']([^"']+)["'][^>]*>.*?</(?:agui|ag-ui)>`)
)

type App struct {
	repo         *businesscore.Repository
	notification notificationService
	dictionary   dictionaryReader
	now          func() time.Time
}

func New(repo *businesscore.Repository) *App {
	return &App{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type notificationService interface {
	CreateNotification(ctx context.Context, in notification.CreateNotificationInput) (notification.NotificationDTO, error)
	RecordCreateFailure(ctx context.Context, in notification.FailureInput) error
}

func (a *App) SetNotificationService(service notificationService) {
	a.notification = service
}

// dictionaryReader 提供 asset_element_types 字典上限，供输出元素结构校验「不得超字典上限」(SKILL-2 FP2)。
type dictionaryReader interface {
	ElementTypeLimits(ctx context.Context, elementTypes []string) (map[string]assetdict.ElementTypeLimit, error)
}

func (a *App) SetDictionary(reader dictionaryReader) {
	a.dictionary = reader
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
	SkillID                    string             `json:"skill_id"`
	Version                    string             `json:"version"`
	SkillSpecJSON              string             `json:"skill_spec_json"`
	OutputSchemaJSON           string             `json:"output_schema_json"`
	ToolRefs                   []string           `json:"tool_refs"`
	MemoryPolicyJSON           string             `json:"memory_policy_json,omitempty"`
	ConfirmationPolicyJSON     string             `json:"confirmation_policy_json"`
	ExecutionPolicySummaryJSON string             `json:"execution_policy_summary_json"`
	OutputElements             []OutputElementDTO `json:"output_elements,omitempty"`
}

// OutputElementDTO 暴露 Skill 输出元素结构(SKILL-2 FP3)，供 agent 按结构组织草稿/最终产物。
// 对应 thrift SkillOutputElementDTO（codegen 后由 RPC handler 映射）。
type OutputElementDTO struct {
	ElementType  string `json:"element_type"`
	ElementName  string `json:"element_name"`
	Required     bool   `json:"required"`
	UseDraft     bool   `json:"use_draft"`
	UseFinal     bool   `json:"use_final"`
	Editable     bool   `json:"editable"`
	Referable    bool   `json:"referable"`
	DisplayOrder int32  `json:"display_order"`
	DisplaySlot  string `json:"display_slot"`
	SchemaJSON   string `json:"schema_json,omitempty"`
}

type ReviewCandidateDTO struct {
	ReviewID               string             `json:"review_id,omitempty"`
	SkillID                string             `json:"skill_id"`
	VersionID              string             `json:"version_id"`
	SkillName              string             `json:"skill_name,omitempty"`
	CreatorID              string             `json:"creator_id,omitempty"`
	Status                 string             `json:"status,omitempty"`
	SubmittedAt            *time.Time         `json:"submitted_at,omitempty"`
	SkillSpecJSON          string             `json:"skill_spec_json"`
	InputSchemaJSON        string             `json:"input_schema_json"`
	OutputSchemaJSON       string             `json:"output_schema_json"`
	ToolRefs               []string           `json:"tool_refs"`
	MemoryPolicyJSON       string             `json:"memory_policy_json"`
	ConfirmationPolicyJSON string             `json:"confirmation_policy_json"`
	TestInputJSON          string             `json:"test_input_json,omitempty"`
	ExpectedElementsJSON   string             `json:"expected_elements_json,omitempty"`
	OutputElements         []OutputElementDTO `json:"output_elements,omitempty"`
}

type SkillDetailDTO struct {
	SkillID             string            `json:"skill_id"`
	SkillKey            string            `json:"skill_key"`
	SkillName           string            `json:"skill_name"`
	SkillScope          string            `json:"skill_scope"`
	Status              string            `json:"status"`
	PublishedVersionID  string            `json:"published_version_id,omitempty"`
	LatestVersionID     string            `json:"latest_version_id"`
	ActiveTestCaseCount int64             `json:"active_test_case_count"`
	RouteHints          map[string]string `json:"route_hints,omitempty"`
	UpdatedAt           time.Time         `json:"updated_at"`
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
	// ACCT-1b：绑定了当前空间/企业白名单显式禁用 Tool 的 Skill 不可路由。
	blocked, err := a.skillsBlockedByToolWhitelist(ctx, auth, rows)
	if err != nil {
		return nil, "", err
	}
	out := make([]SkillSummaryDTO, 0, min(len(rows), limit))
	for i, skill := range rows {
		if i >= limit {
			break
		}
		if blocked[skill.ID] {
			continue
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

// skillsBlockedByToolWhitelist 返回因绑定 Tool 被当前空间/企业白名单显式禁用而不可路由的 Skill 集合(ACCT-1b)。
// 白名单为覆盖语义：仅 allowed=false 的显式禁用规则会拉黑对应 Skill；用 deny 规则 + bindings 两次批量查避免 N+1。
func (a *App) skillsBlockedByToolWhitelist(ctx context.Context, auth accountspace.AuthContext, skills []businesscore.Skill) (map[string]bool, error) {
	blocked := map[string]bool{}
	scopeIDs := make([]string, 0, 2)
	if auth.SpaceID != "" {
		scopeIDs = append(scopeIDs, auth.SpaceID)
	}
	if auth.EnterpriseID != "" {
		scopeIDs = append(scopeIDs, auth.EnterpriseID)
	}
	if len(scopeIDs) == 0 || len(skills) == 0 {
		return blocked, nil
	}
	var denyRules []businesscore.ToolWhitelistRule
	if err := a.repo.DB().WithContext(ctx).
		Where("scope_id IN ? AND allowed = ? AND status = ?", scopeIDs, false, "active").
		Find(&denyRules).Error; err != nil {
		return nil, err
	}
	if len(denyRules) == 0 {
		return blocked, nil
	}
	denied := make(map[string]bool, len(denyRules))
	for _, r := range denyRules {
		denied[r.ToolName+":"+r.ToolType] = true
	}
	versionIDs := make([]string, 0, len(skills))
	versionToSkill := make(map[string]string, len(skills))
	for _, s := range skills {
		if s.PublishedVersionID != nil && *s.PublishedVersionID != "" {
			versionIDs = append(versionIDs, *s.PublishedVersionID)
			versionToSkill[*s.PublishedVersionID] = s.ID
		}
	}
	if len(versionIDs) == 0 {
		return blocked, nil
	}
	var bindings []businesscore.SkillToolBinding
	if err := a.repo.DB().WithContext(ctx).
		Where("version_id IN ?", versionIDs).Find(&bindings).Error; err != nil {
		return nil, err
	}
	for _, b := range bindings {
		if denied[b.ToolName+":"+b.ToolType] {
			if sid, ok := versionToSkill[b.VersionID]; ok {
				blocked[sid] = true
			}
		}
	}
	return blocked, nil
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
	outputElements, err := a.outputElements(ctx, sv.ID)
	if err != nil {
		return SkillSpecDTO{}, err
	}
	return SkillSpecDTO{
		SkillID: skill.ID, Version: sv.Version, SkillSpecJSON: string(sv.SkillSpecJSON),
		OutputSchemaJSON: string(sv.OutputSchemaJSON), ToolRefs: toolRefs, MemoryPolicyJSON: string(sv.MemoryPolicyJSON),
		ConfirmationPolicyJSON: jsonString(sv.ConfirmationPolicyJSON, defaultConfirmationPolicyJSON), ExecutionPolicySummaryJSON: executionSummary(toolRefs),
		OutputElements: outputElements,
	}, nil
}

// outputElements 装配某版本的输出元素结构(按 display_order 稳定排序，一次查询避免 N+1)。SKILL-2 FP3。
func (a *App) outputElements(ctx context.Context, versionID string) ([]OutputElementDTO, error) {
	var rows []businesscore.SkillOutputElementSchema
	if err := a.repo.DB().WithContext(ctx).Where("version_id = ?", versionID).
		Order("display_order ASC, element_type ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]OutputElementDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, OutputElementDTO{
			ElementType: row.ElementType, ElementName: row.ElementName, Required: row.Required,
			UseDraft: row.UseDraft, UseFinal: row.UseFinal, Editable: row.Editable, Referable: row.Referable,
			DisplayOrder: row.DisplayOrder, DisplaySlot: row.DisplaySlot, SchemaJSON: string(row.SchemaJSON),
		})
	}
	return out, nil
}

func (a *App) GetReviewCandidateSkillSpec(ctx context.Context, auth accountspace.AuthContext, skillID, versionID, testCaseID string) (ReviewCandidateDTO, error) {
	if auth.LoginIdentityType != "admin" {
		if _, err := a.visibleSkill(ctx, auth, skillID); err != nil {
			return ReviewCandidateDTO{}, err
		}
	}
	var sv businesscore.SkillVersion
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND skill_id = ?", versionID, skillID).First(&sv).Error; err != nil {
		return ReviewCandidateDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "skill version not found")
	}
	var skill businesscore.Skill
	_ = a.repo.DB().WithContext(ctx).Where("id = ?", skillID).First(&skill).Error
	toolRefs, err := a.toolRefs(ctx, skillID, sv.ID)
	if err != nil {
		return ReviewCandidateDTO{}, err
	}
	creatorID := ""
	if sv.SubmittedByUserID != nil {
		creatorID = *sv.SubmittedByUserID
	} else if skill.OwnerUserID != nil {
		creatorID = *skill.OwnerUserID
	}
	outputElements, err := a.outputElements(ctx, sv.ID)
	if err != nil {
		return ReviewCandidateDTO{}, err
	}
	dto := ReviewCandidateDTO{
		ReviewID: sv.ID, SkillID: skillID, VersionID: sv.ID, SkillName: skill.SkillName, CreatorID: creatorID,
		Status: sv.Status, SubmittedAt: sv.SubmittedAt, SkillSpecJSON: string(sv.SkillSpecJSON),
		InputSchemaJSON: string(sv.InputSchemaJSON), OutputSchemaJSON: string(sv.OutputSchemaJSON),
		ToolRefs: toolRefs, MemoryPolicyJSON: string(sv.MemoryPolicyJSON), ConfirmationPolicyJSON: jsonString(sv.ConfirmationPolicyJSON, defaultConfirmationPolicyJSON),
		OutputElements: outputElements,
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

func (a *App) SaveSkillTestResult(ctx context.Context, auth accountspace.AuthContext, skillID, versionID, testRunID, testCaseID, idempotencyKey, status, actualElementsJSON, errorCode, errorSummary, safetyEvidenceJSON, agentTraceID string) (TestResultDTO, error) {
	if strings.TrimSpace(skillID) == "" || strings.TrimSpace(versionID) == "" || strings.TrimSpace(testRunID) == "" {
		return TestResultDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "skill_id, version_id and test_run_id are required")
	}
	if strings.TrimSpace(idempotencyKey) != "skill_test:"+testRunID {
		return TestResultDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "request_meta.idempotency_key must be skill_test:<test_run_id>")
	}
	now := a.now()
	runStatus, ok := normalizeSkillTestStatus(status)
	if !ok {
		return TestResultDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "status must be passed, failed, blocked, timeout or rejected")
	}
	if !json.Valid([]byte(actualElementsJSON)) {
		return TestResultDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "actual_elements_json must be json")
	}
	evidenceJSON, err := validateSkillTestSafetyEvidence(testRunID, testCaseID, runStatus, safetyEvidenceJSON, agentTraceID, now)
	if err != nil {
		return TestResultDTO{}, err
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
	requestHash := skillTestRequestHash(skillID, versionID, testRunID, testCaseID, runStatus, actualElementsJSON, errorCode, errorSummary, evidenceJSON, agentTraceID)
	db := a.repo.DB().WithContext(ctx)
	var existing businesscore.SkillTestRun
	err = db.Where("idempotency_key = ?", idempotencyKey).First(&existing).Error
	if err == nil {
		if value(existing.RequestHash) != requestHash {
			return TestResultDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "skill test idempotency key conflicts with a different request")
		}
		return TestResultDTO{TestRunID: existing.ID, Status: existing.Status, Saved: true}, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return TestResultDTO{}, err
	}
	err = db.Where("id = ?", testRunID).First(&existing).Error
	if err == nil {
		if value(existing.RequestHash) == requestHash {
			return TestResultDTO{TestRunID: existing.ID, Status: existing.Status, Saved: true}, nil
		}
		return TestResultDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "test_run_id conflicts with a different skill test request")
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return TestResultDTO{}, err
	}
	row := businesscore.SkillTestRun{
		ID: testRunID, SkillID: skillID, VersionID: versionID, TestCaseID: testCasePtr, Status: runStatus,
		ExecutionMode: "sandbox", InputJSON: inputJSON, ActualElementsJSON: datatypes.JSON([]byte(actualElementsJSON)),
		SafetyEvidenceJSON: datatypes.JSON([]byte(evidenceJSON)), ErrorCode: optionalString(errorCode), ErrorSummary: optionalString(errorSummary),
		AgentTraceID: optionalString(agentTraceID), IdempotencyKey: optionalString(idempotencyKey), RequestHash: optionalString(requestHash),
		FinishedAt: &now, CreatedByUserID: optionalString(auth.UserID),
		CreatedBy: optionalString(auth.UserID), UpdatedBy: optionalString(auth.UserID), CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&row).Error; err != nil {
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
		out = append(out, a.skillAdminDTO(ctx, row))
	}
	return Page[SkillDetailDTO]{Items: out, Limit: limit, Offset: nonNegative(offset)}, nil
}

type SaveSkillInput struct {
	Auth                   accountspace.AuthContext
	SkillID                string
	SkillKey               string
	SkillName              string
	SkillScope             string
	RouteHints             map[string]string
	Version                string
	SkillMarkdown          string
	SkillTags              []string
	SkillSpecJSON          string
	InputSchemaJSON        string
	OutputSchemaJSON       string
	ToolRefs               []string
	MemoryPolicyJSON       string
	ConfirmationPolicyJSON string
	OutputElements         []OutputElementInput
}

// OutputElementInput 声明 Skill 一个输出元素结构(SKILL-2 FP2)。element_type 必须是平台内置
// 且 active 的资产元素类型；use_draft/use_final/editable/referable 不得超字典上限。
type OutputElementInput struct {
	ElementType  string
	ElementName  string
	Required     bool
	UseDraft     bool
	UseFinal     bool
	Editable     bool
	Referable    bool
	DisplayOrder int32
	DisplaySlot  string
	SchemaJSON   string
}

func (a *App) SaveSkill(ctx context.Context, in SaveSkillInput) (SkillDetailDTO, error) {
	if in.Auth.UserID == "" {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "user auth is required")
	}
	if strings.TrimSpace(in.SkillMarkdown) != "" {
		compiled := compileSkillMarkdown(in.SkillMarkdown, in.SkillName, in.SkillTags)
		if strings.TrimSpace(in.SkillName) == "" {
			in.SkillName = compiled.Name
		}
		if strings.TrimSpace(in.SkillKey) == "" {
			in.SkillKey = skillKeyFromName(compiled.Name, in.SkillMarkdown)
		}
		if len(in.RouteHints) == 0 && strings.TrimSpace(compiled.InvocationRule) != "" {
			in.RouteHints = map[string]string{"invocation_rule": compiled.InvocationRule}
		}
		if strings.TrimSpace(in.SkillSpecJSON) == "" {
			in.SkillSpecJSON = compiled.SkillSpecJSON
		}
		if strings.TrimSpace(in.InputSchemaJSON) == "" {
			in.InputSchemaJSON = compiled.InputSchemaJSON
		}
		if strings.TrimSpace(in.OutputSchemaJSON) == "" {
			in.OutputSchemaJSON = compiled.OutputSchemaJSON
		}
		in.ToolRefs = mergeStrings(in.ToolRefs, compiled.ToolRefs)
	}
	if strings.TrimSpace(in.SkillKey) == "" || strings.TrimSpace(in.SkillName) == "" {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "skill_key and skill_name are required")
	}
	if !validJSONOrEmpty(in.SkillSpecJSON) || !validJSONOrEmpty(in.InputSchemaJSON) || !validJSONOrEmpty(in.OutputSchemaJSON) || !validJSONOrEmpty(in.ConfirmationPolicyJSON) {
		return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "skill schemas must be json")
	}
	if err := a.validateOutputElements(ctx, in.OutputElements); err != nil {
		return SkillDetailDTO{}, err
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
		Status: statusDraft, RouteHintsJSON: mustJSON(in.RouteHints), CreatedByUserID: &userID,
		CreatedBy: optionalString(userID), UpdatedBy: optionalString(userID), CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.DB().WithContext(ctx).Save(&skill).Error; err != nil {
		if isUniqueConstraintError(err) {
			return SkillDetailDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "skill_key already exists")
		}
		return SkillDetailDTO{}, err
	}
	versionID := security.RandomID("skv_")
	version := normalizeStatus(in.Version, "0.1.0")
	memory := normalizeJSON(in.MemoryPolicyJSON, `{"enabled":true}`)
	confirmation := normalizeJSON(in.ConfirmationPolicyJSON, defaultConfirmationPolicyJSON)
	sv := businesscore.SkillVersion{
		ID: versionID, SkillID: skill.ID, Version: version, Status: statusDraft, SkillSpecJSON: datatypes.JSON([]byte(normalizeJSON(in.SkillSpecJSON, "{}"))),
		InputSchemaJSON: datatypes.JSON([]byte(normalizeJSON(in.InputSchemaJSON, "{}"))), OutputSchemaJSON: datatypes.JSON([]byte(normalizeJSON(in.OutputSchemaJSON, "{}"))),
		MemoryPolicyJSON: datatypes.JSON([]byte(memory)), ConfirmationPolicyJSON: datatypes.JSON([]byte(confirmation)),
		SubmittedByUserID: &userID, CreatedBy: optionalString(userID), UpdatedBy: optionalString(userID), CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.DB().WithContext(ctx).Create(&sv).Error; err != nil {
		return SkillDetailDTO{}, err
	}
	if err := a.saveToolBindings(ctx, skill.ID, versionID, userID, now, in.ToolRefs); err != nil {
		return SkillDetailDTO{}, err
	}
	if err := a.saveOutputElements(ctx, skill.ID, versionID, userID, now, in.OutputElements); err != nil {
		return SkillDetailDTO{}, err
	}
	return a.skillAdminDTO(ctx, skill), nil
}

type compiledMarkdownSkill struct {
	Name             string
	InvocationRule   string
	SkillSpecJSON    string
	InputSchemaJSON  string
	OutputSchemaJSON string
	ToolRefs         []string
}

func compileSkillMarkdown(markdown, fallbackName string, tags []string) compiledMarkdownSkill {
	markdown = strings.TrimSpace(markdown)
	sections := parseSkillMarkdownSections(markdown)
	nameSection := sections["name"]
	if nameSection == "未命名 Skill" {
		nameSection = ""
	}
	name := firstNonEmpty(nameSection, fallbackName)
	description := sections["description"]
	invocationRule := firstNonEmpty(sections["invocation_rule"], description)
	toolRefs := extractToolRefs(sections["tool_refs"])
	aguiRefs := extractAGUIRefs(sections["agui_refs"])
	inputSchema := buildInputSchema(sections["inputs"])
	outputSchema := buildOutputSchema(sections["result_outputs"])
	cleanTags := compactStrings(tags)
	spec := map[string]any{
		"schema_version":         "skill.markdown.v1",
		"source_format":          "markdown",
		"markdown":               markdown,
		"name":                   name,
		"description":            description,
		"skill_tags":             cleanTags,
		"sections":               sections,
		"input_intents":          inputSchema["input_intents"],
		"tool_refs":              toolRefs,
		"agui_refs":              aguiRefs,
		"generation_preferences": sections["generation_preferences"],
		"prompt_guidelines":      sections["prompt_guidelines"],
		"output_intents":         outputSchema["output_intents"],
	}
	return compiledMarkdownSkill{
		Name: name, InvocationRule: invocationRule, ToolRefs: toolRefs,
		SkillSpecJSON: mustJSONString(spec), InputSchemaJSON: mustJSONString(inputSchema), OutputSchemaJSON: mustJSONString(outputSchema),
	}
}

func parseSkillMarkdownSections(markdown string) map[string]string {
	sections := map[string][]string{
		"name": {}, "description": {}, "invocation_rule": {}, "inputs": {}, "plan": {}, "tool_refs": {}, "agui_refs": {},
		"generation_preferences": {}, "prompt_guidelines": {}, "result_outputs": {},
	}
	current := ""
	for _, raw := range strings.Split(markdown, "\n") {
		line := strings.TrimRight(raw, "\r")
		if title, tag, ok := parseMarkdownHeading(line); ok {
			key := skillSectionKey(tag)
			if key == "" {
				key = skillSectionKey(title)
			}
			current = key
			if key == "name" && strings.TrimSpace(title) != "" {
				sections[key] = []string{strings.TrimSpace(title)}
			}
			continue
		}
		if current != "" {
			sections[current] = append(sections[current], line)
		}
	}
	out := make(map[string]string, len(sections))
	for key, lines := range sections {
		out[key] = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return out
}

func parseMarkdownHeading(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	if i == 0 || i > 6 || i >= len(trimmed) || trimmed[i] != ' ' {
		return "", "", false
	}
	title := strings.TrimSpace(trimmed[i:])
	tag := ""
	if start := strings.LastIndex(title, "<"); start >= 0 && strings.HasSuffix(title, ">") {
		tag = strings.TrimSpace(strings.TrimSuffix(title[start+1:], ">"))
		title = strings.TrimSpace(title[:start])
	}
	return title, tag, true
}

func skillSectionKey(label string) string {
	label = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(label), " ", ""))
	switch label {
	case "名称", "name":
		return "name"
	case "说明", "描述", "description":
		return "description"
	case "调用规则", "触发说明", "skill调用规则", "invocation_rule":
		return "invocation_rule"
	case "输入", "入参", "用户输入", "input", "inputs":
		return "inputs"
	case "计划", "流程", "流程规划", "plan":
		return "plan"
	case "工具引用", "tool_refs", "toolrefs":
		return "tool_refs"
	case "ag-ui元素引用", "agui元素引用", "ag-ui引用", "agui引用":
		return "agui_refs"
	case "生成偏好", "generation_preferences":
		return "generation_preferences"
	case "提示词写法", "提示词", "prompt_guidelines":
		return "prompt_guidelines"
	case "结果输出", "输出", "result_outputs":
		return "result_outputs"
	default:
		return ""
	}
}

func extractToolRefs(section string) []string {
	refs := make([]string, 0)
	for _, match := range toolTagPattern.FindAllStringSubmatch(section, -1) {
		ref := strings.TrimSpace(match[1])
		if ref == "" {
			continue
		}
		if !strings.Contains(ref, ":") {
			ref += ":builtin"
		}
		refs = append(refs, ref)
	}
	for _, match := range markdownMentionPattern.FindAllStringSubmatch(section, -1) {
		ref := strings.TrimSpace(match[1])
		if ref == "" || strings.HasPrefix(ref, "agui.") {
			continue
		}
		if !strings.Contains(ref, ":") {
			ref += ":builtin"
		}
		refs = append(refs, ref)
	}
	return compactStrings(refs)
}

func extractAGUIRefs(section string) map[string][]string {
	out := map[string][]string{"inside_dialog": {}, "outside_dialog": {}}
	slot := "inside_dialog"
	for _, raw := range strings.Split(section, "\n") {
		line := strings.TrimSpace(raw)
		if strings.Contains(line, "对话框外") {
			slot = "outside_dialog"
		} else if strings.Contains(line, "对话框内") {
			slot = "inside_dialog"
		}
		for _, match := range aguiTagPattern.FindAllStringSubmatch(line, -1) {
			ref := strings.TrimPrefix(strings.TrimSpace(match[1]), "agui.")
			if ref != "" {
				out[slot] = append(out[slot], ref)
			}
		}
		for _, match := range markdownMentionPattern.FindAllStringSubmatch(line, -1) {
			ref := strings.TrimPrefix(strings.TrimSpace(match[1]), "agui.")
			if ref != "" {
				out[slot] = append(out[slot], ref)
			}
		}
	}
	out["inside_dialog"] = compactStrings(out["inside_dialog"])
	out["outside_dialog"] = compactStrings(out["outside_dialog"])
	return out
}

func inferOutputArtifacts(section string) []string {
	var out []string
	for _, item := range linesFromSection(section) {
		lower := strings.ToLower(item)
		switch {
		case strings.Contains(item, "故事板") || strings.Contains(lower, "storyboard"):
			out = append(out, "storyboard")
		case strings.Contains(item, "图片") || strings.Contains(item, "图像") || strings.Contains(lower, "image"):
			out = append(out, "image_asset")
		case strings.Contains(item, "音频") || strings.Contains(item, "声音") || strings.Contains(lower, "audio"):
			out = append(out, "audio_asset")
		case strings.Contains(item, "视频") || strings.Contains(lower, "video"):
			out = append(out, "video_asset")
		case strings.Contains(item, "文档") || strings.Contains(item, "规格") || strings.Contains(lower, "markdown") || strings.Contains(lower, ".md"):
			out = append(out, "markdown_doc")
		case strings.Contains(item, "资产") || strings.Contains(lower, "asset"):
			out = append(out, "asset")
		}
	}
	return compactStrings(out)
}

func buildInputSchema(section string) map[string]any {
	lines := linesFromSection(section)
	intents := make([]map[string]any, 0, len(lines))
	for i, line := range lines {
		preferredAGUI := inferInputAGUI(line)
		if len(preferredAGUI) == 0 {
			preferredAGUI = []string{"textarea"}
		}
		intent := map[string]any{
			"id":             fmt.Sprintf("input_%d", i+1),
			"name":           inferInputName(line, i+1),
			"description":    line,
			"required_when":  inferInputRequiredWhen(line),
			"preferred_agui": preferredAGUI,
		}
		if options := inferInputOptions(line); len(options) > 0 {
			intent["options"] = options
		}
		if assetTypes := inferAcceptedAssetTypes(line); len(assetTypes) > 0 {
			intent["accepted_asset_types"] = assetTypes
		}
		intents = append(intents, intent)
	}
	if len(intents) == 0 {
		intents = append(intents, map[string]any{
			"id":             "input_1",
			"name":           "用户目标",
			"description":    "用户可以用自然语言描述目标，Agent 在缺少必要信息时继续追问。",
			"required_when":  "按 Agent 判断",
			"preferred_agui": []string{"textarea"},
		})
	}
	return map[string]any{
		"schema_version": "skill.runtime_input.v1",
		"mode":           "agent_requested_inputs",
		"source":         "skill_markdown",
		"input_intents":  intents,
		"runtime_policy": map[string]any{
			"ask_when_missing":           true,
			"allow_partial_start":        true,
			"allow_iterative_refinement": true,
		},
	}
}

func buildOutputSchema(section string) map[string]any {
	lines := linesFromSection(section)
	intents := make([]map[string]any, 0, len(lines))
	artifactTypes := make([]string, 0, len(lines))
	for i, line := range lines {
		artifacts := inferOutputArtifacts(line)
		if len(artifacts) == 0 {
			artifacts = []string{"structured_result"}
		}
		preferredAGUI := inferOutputAGUI(line)
		if len(preferredAGUI) == 0 {
			preferredAGUI = []string{"result_summary"}
		}
		artifactTypes = append(artifactTypes, artifacts...)
		intents = append(intents, map[string]any{
			"id":             fmt.Sprintf("output_%d", i+1),
			"name":           inferOutputName(line, i+1),
			"description":    line,
			"artifact_types": artifacts,
			"preferred_agui": preferredAGUI,
			"reviewable":     isReviewableOutput(line),
		})
	}
	artifactTypes = compactStrings(artifactTypes)
	if len(intents) == 0 {
		artifactTypes = []string{"structured_result"}
		intents = append(intents, map[string]any{
			"id":             "output_1",
			"name":           "最终结果",
			"description":    "Agent 根据用户目标生成可审阅结果，并在用户满意后输出最终结果。",
			"artifact_types": artifactTypes,
			"preferred_agui": []string{"result_summary"},
			"reviewable":     true,
		})
	}
	return map[string]any{
		"schema_version": "skill.runtime_output.v1",
		"mode":           "agent_generated_outputs",
		"source":         "skill_markdown",
		"artifact_types": artifactTypes,
		"output_intents": intents,
		"runtime_policy": map[string]any{
			"allow_incremental_delivery":        true,
			"allow_user_revision":               true,
			"final_output_after_user_satisfied": true,
		},
	}
}

func inferInputAGUI(line string) []string {
	var out []string
	lower := strings.ToLower(line)
	if containsAny(line, "上传", "文件", "素材", "图片", "图像", "PDF", "文本", "视频", "音频") || strings.Contains(lower, "pdf") {
		out = append(out, "asset_upload", "asset_picker")
	}
	if containsAny(line, "选择", "选项", "风格", "偏好", "提供") {
		out = append(out, "chips", "select")
	}
	if containsAny(line, "多行", "补充", "目标", "描述", "自然语言", "说明") {
		out = append(out, "textarea")
	}
	if containsAny(line, "确认", "满意") {
		out = append(out, "confirm")
	}
	return compactStrings(out)
}

func inferInputName(line string, index int) string {
	switch {
	case containsAny(line, "剧本", "素材", "文件", "上传"):
		return "剧本/素材"
	case containsAny(line, "风格", "偏好"):
		return "风格偏好"
	case containsAny(line, "目标", "描述", "自然语言"):
		return "创作目标"
	case containsAny(line, "修改", "镜头"):
		return "修改意见"
	default:
		return fmt.Sprintf("输入项 %d", index)
	}
}

func inferInputRequiredWhen(line string) string {
	if containsAny(line, "如果", "缺少", "不清楚", "需要") {
		return line
	}
	return "按 Agent 判断"
}

func inferInputOptions(line string) []string {
	options := make([]string, 0)
	for _, candidate := range []string{"写实", "动画", "电影感"} {
		if strings.Contains(line, candidate) {
			options = append(options, candidate)
		}
	}
	return options
}

func inferAcceptedAssetTypes(line string) []string {
	lower := strings.ToLower(line)
	var out []string
	if containsAny(line, "图片", "图像") {
		out = append(out, "image")
	}
	if strings.Contains(line, "PDF") || strings.Contains(lower, "pdf") {
		out = append(out, "pdf")
	}
	if strings.Contains(line, "文本") {
		out = append(out, "text")
	}
	if strings.Contains(line, "视频") {
		out = append(out, "video")
	}
	if strings.Contains(line, "音频") || strings.Contains(line, "声音") {
		out = append(out, "audio")
	}
	return compactStrings(out)
}

func inferOutputAGUI(line string) []string {
	var out []string
	lower := strings.ToLower(line)
	if strings.Contains(line, "故事板") || strings.Contains(lower, "storyboard") {
		out = append(out, "storyboard_panel")
	}
	if containsAny(line, "图片", "图像", "资产") || strings.Contains(lower, "image") || strings.Contains(lower, "asset") {
		out = append(out, "asset_panel")
	}
	if strings.Contains(line, "图片") || strings.Contains(line, "图像") || strings.Contains(lower, "image") {
		out = append(out, "image_preview")
	}
	if strings.Contains(line, "视频") || strings.Contains(lower, "video") {
		out = append(out, "video_preview", "asset_panel")
	}
	if strings.Contains(line, "音频") || strings.Contains(line, "声音") || strings.Contains(lower, "audio") {
		out = append(out, "audio_preview", "asset_panel")
	}
	if containsAny(line, "最终", "结果", "完成") {
		out = append(out, "result_summary")
	}
	return compactStrings(out)
}

func inferOutputName(line string, index int) string {
	switch {
	case strings.Contains(line, "故事板"):
		return "故事板"
	case containsAny(line, "图片", "图像"):
		return "图片资产"
	case strings.Contains(line, "视频"):
		return "视频资产"
	case strings.Contains(line, "音频") || strings.Contains(line, "声音"):
		return "音频资产"
	case containsAny(line, "最终", "结果"):
		return "最终结果"
	default:
		return fmt.Sprintf("输出项 %d", index)
	}
}

func isReviewableOutput(line string) bool {
	return containsAny(line, "审阅", "修改", "确认", "满意", "预览")
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func linesFromSection(section string) []string {
	out := make([]string, 0)
	for _, raw := range strings.Split(section, "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "-"), "*"))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func skillKeyFromName(name, seed string) string {
	source := firstNonEmpty(name, seed)
	var b strings.Builder
	for _, r := range strings.ToLower(source) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			if b.Len() > 0 {
				b.WriteByte('_')
			}
		}
	}
	key := strings.Trim(b.String(), "_")
	for strings.Contains(key, "__") {
		key = strings.ReplaceAll(key, "__", "_")
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(name) + "\n" + strings.TrimSpace(seed)))
	suffix := fmt.Sprintf("%x", sum[:4])
	if key != "" {
		if len(key) > 118 {
			key = strings.Trim(key[:118], "_")
		}
		return key + "_" + suffix
	}
	return "skill_" + suffix
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "SQLSTATE 23505") || strings.Contains(text, "duplicate key value")
}

func saveToolRefRows(refs []string, skillID, versionID, operatorID string, now time.Time) []businesscore.SkillToolBinding {
	clean := compactStrings(refs)
	rows := make([]businesscore.SkillToolBinding, 0, len(clean))
	for _, ref := range clean {
		toolName, toolType := splitToolRef(ref)
		if toolName == "" || toolType == "" {
			continue
		}
		rows = append(rows, businesscore.SkillToolBinding{
			ID: security.RandomID("stb_"), SkillID: skillID, VersionID: versionID,
			ToolName: toolName, ToolType: toolType, Required: true,
			CreatedBy: optionalString(operatorID), UpdatedBy: optionalString(operatorID), CreatedAt: now,
		})
	}
	return rows
}

func splitToolRef(ref string) (string, string) {
	parts := strings.SplitN(strings.TrimSpace(ref), ":", 2)
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0]), "builtin"
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func (a *App) saveToolBindings(ctx context.Context, skillID, versionID, operatorID string, now time.Time, refs []string) error {
	rows := saveToolRefRows(refs, skillID, versionID, operatorID, now)
	if len(rows) == 0 {
		return nil
	}
	return a.repo.DB().WithContext(ctx).Create(&rows).Error
}

func compactStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func mergeStrings(left, right []string) []string {
	merged := make([]string, 0, len(left)+len(right))
	merged = append(merged, left...)
	merged = append(merged, right...)
	return compactStrings(merged)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mustJSONString(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// validateOutputElements 校验输出元素结构：类型必须平台内置且 active，双态/编辑/引用不得超字典上限(SKILL-2 FP2)。
func (a *App) validateOutputElements(ctx context.Context, elements []OutputElementInput) error {
	if len(elements) == 0 {
		return nil
	}
	if a.dictionary == nil {
		return bizerrors.New(bizerrors.CodeStateConflict, "element dictionary is unavailable")
	}
	types := make([]string, 0, len(elements))
	seen := make(map[string]bool, len(elements))
	for _, el := range elements {
		et := strings.TrimSpace(el.ElementType)
		if et == "" {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "output element type is required")
		}
		if seen[et] {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "duplicate output element type: "+et)
		}
		seen[et] = true
		if !el.UseDraft && !el.UseFinal {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "output element must enable draft or final stage: "+et)
		}
		if !validJSONOrEmpty(el.SchemaJSON) {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "output element schema_json must be json: "+et)
		}
		types = append(types, et)
	}
	limits, err := a.dictionary.ElementTypeLimits(ctx, types)
	if err != nil {
		return err
	}
	for _, el := range elements {
		et := strings.TrimSpace(el.ElementType)
		limit, ok := limits[et]
		if !ok || !limit.Active {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "output element type is not a platform built-in active type: "+et)
		}
		if el.UseDraft && !limit.DraftEnabled {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "draft stage not allowed for element type: "+et)
		}
		if el.UseFinal && !limit.FinalEnabled {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "final stage not allowed for element type: "+et)
		}
		if el.Editable && !limit.Editable {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "editable not allowed for element type: "+et)
		}
		if el.Referable && !limit.Referable {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "referable not allowed for element type: "+et)
		}
	}
	return nil
}

// saveOutputElements 批量持久化输出元素结构(一次 Create，避免逐条写)。校验已在 validateOutputElements 前置完成。
func (a *App) saveOutputElements(ctx context.Context, skillID, versionID, operatorID string, now time.Time, elements []OutputElementInput) error {
	if len(elements) == 0 {
		return nil
	}
	rows := make([]businesscore.SkillOutputElementSchema, 0, len(elements))
	for _, el := range elements {
		slot := strings.TrimSpace(el.DisplaySlot)
		if slot == "" {
			slot = "blackboard"
		}
		rows = append(rows, businesscore.SkillOutputElementSchema{
			ID: security.RandomID("soe_"), SkillID: skillID, VersionID: versionID,
			ElementType: strings.TrimSpace(el.ElementType), ElementName: strings.TrimSpace(el.ElementName),
			SchemaJSON: datatypes.JSON([]byte(normalizeJSON(el.SchemaJSON, "{}"))), Required: el.Required,
			DisplayOrder: el.DisplayOrder, DisplaySlot: slot,
			UseDraft: el.UseDraft, UseFinal: el.UseFinal, Editable: el.Editable, Referable: el.Referable,
			CreatedBy: optionalString(operatorID), UpdatedBy: optionalString(operatorID), CreatedAt: now,
		})
	}
	return a.repo.DB().WithContext(ctx).Create(&rows).Error
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
	version.UpdatedBy = optionalString(auth.UserID)
	version.UpdatedAt = now
	skill.Status = statusSubmitted
	skill.UpdatedBy = optionalString(auth.UserID)
	skill.UpdatedAt = now
	if err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&version).Error; err != nil {
			return err
		}
		return tx.Save(&skill).Error
	}); err != nil {
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
	skill.UpdatedBy = optionalString(auth.UserID)
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
		out = append(out, a.skillAdminDTO(ctx, row))
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
	if err := tx.Model(&businesscore.SkillVersion{}).Where("skill_id = ? AND status = ?", skillID, statusPublished).Updates(map[string]any{"status": statusDeprecated, "updated_at": now, "updated_by": auth.AdminID}).Error; err != nil {
		tx.Rollback()
		return SkillDetailDTO{}, err
	}
	version.Status = statusPublished
	version.ReviewedByAdminID = &auth.AdminID
	version.ReviewedAt = &now
	version.PublishedAt = &now
	version.UpdatedBy = optionalString(auth.AdminID)
	version.UpdatedAt = now
	if err := tx.Save(&version).Error; err != nil {
		tx.Rollback()
		return SkillDetailDTO{}, err
	}
	skill.Status = statusPublished
	skill.PublishedVersionID = &version.ID
	skill.UpdatedBy = optionalString(auth.AdminID)
	skill.UpdatedAt = now
	if err := tx.Save(&skill).Error; err != nil {
		tx.Rollback()
		return SkillDetailDTO{}, err
	}
	if err := tx.Create(&businesscore.SkillReviewRecord{ID: security.RandomID("skr_"), SkillID: skillID, VersionID: versionID, ReviewAction: "publish", ReviewStatus: "approved", ReviewedByAdminID: auth.AdminID, CreatedBy: optionalString(auth.AdminID), UpdatedBy: optionalString(auth.AdminID), CreatedAt: now}).Error; err != nil {
		tx.Rollback()
		return SkillDetailDTO{}, err
	}
	if err := tx.Commit().Error; err != nil {
		return SkillDetailDTO{}, err
	}
	a.notifySkillReview(ctx, skill, version.ID, "approved", "")
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
	skill.UpdatedBy = optionalString(auth.AdminID)
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
	commentPtr := optionalString(comment)
	var skill businesscore.Skill
	if err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		version.Status = statusDraft
		version.ReviewedByAdminID = &auth.AdminID
		version.ReviewedAt = &now
		version.UpdatedBy = optionalString(auth.AdminID)
		version.UpdatedAt = now
		if err := tx.Save(&version).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", version.SkillID).First(&skill).Error; err != nil {
			return err
		}
		if skill.PublishedVersionID != nil && *skill.PublishedVersionID != "" {
			skill.Status = statusPublished
		} else {
			skill.Status = statusDraft
		}
		skill.UpdatedBy = optionalString(auth.AdminID)
		skill.UpdatedAt = now
		if err := tx.Save(&skill).Error; err != nil {
			return err
		}
		return tx.Create(&businesscore.SkillReviewRecord{ID: security.RandomID("skr_"), SkillID: version.SkillID, VersionID: version.ID, ReviewAction: "reject", ReviewStatus: "rejected", ReviewComment: commentPtr, ReviewedByAdminID: auth.AdminID, CreatedBy: optionalString(auth.AdminID), UpdatedBy: optionalString(auth.AdminID), CreatedAt: now}).Error
	}); err != nil {
		return SkillDetailDTO{}, err
	}
	a.notifySkillReview(ctx, skill, version.ID, "rejected", comment)
	return skillDTO(skill), nil
}

func (a *App) notifySkillReview(ctx context.Context, skill businesscore.Skill, versionID, result, comment string) {
	if a.notification == nil || skill.OwnerUserID == nil || *skill.OwnerUserID == "" {
		return
	}
	notificationType := "skill_review_approved"
	title := "Skill approved"
	summary := "Your Skill review has been approved."
	if result == "rejected" {
		notificationType = "skill_review_rejected"
		title = "Skill rejected"
		summary = "Your Skill review has been rejected."
	}
	if strings.TrimSpace(comment) != "" && result == "rejected" {
		summary = "Your Skill review has been rejected. Please update and submit again."
	}
	idem := "skill_review:" + result + ":" + versionID
	_, err := a.notification.CreateNotification(ctx, notification.CreateNotificationInput{
		RecipientUserID: *skill.OwnerUserID, Type: notificationType, Title: title, Summary: summary,
		RelatedResourceType: "skill", RelatedResourceID: skill.ID,
		NavigationHint: map[string]any{"target_route": "/skills/" + skill.ID, "target_resource_id": skill.ID},
		IdempotencyKey: idem, TraceID: "skill-review-" + versionID,
	})
	if err == nil {
		return
	}
	_ = a.notification.RecordCreateFailure(ctx, notification.FailureInput{
		RecipientUserID: *skill.OwnerUserID, Type: notificationType, RelatedResourceType: "skill", RelatedResourceID: skill.ID,
		IdempotencyKey: idem, ErrorCode: errorCode(err), ErrorSummary: err.Error(), TraceID: "skill-review-" + versionID,
	})
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

func (a *App) skillAdminDTO(ctx context.Context, row businesscore.Skill) SkillDetailDTO {
	dto := skillDTO(row)
	var version businesscore.SkillVersion
	if err := a.repo.DB().WithContext(ctx).
		Where("skill_id = ?", row.ID).
		Order("updated_at DESC, created_at DESC").
		First(&version).Error; err == nil {
		dto.LatestVersionID = version.ID
		_ = a.repo.DB().WithContext(ctx).Model(&businesscore.SkillTestCase{}).
			Where("skill_id = ? AND version_id = ? AND status = ?", row.ID, version.ID, statusActive).
			Count(&dto.ActiveTestCaseCount).Error
	}
	return dto
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

type skillTestSafetyEvidence struct {
	Scene                 string `json:"scene"`
	TargetType            string `json:"target_type"`
	TargetRefID           string `json:"target_ref_id"`
	EvaluatedObjectDigest string `json:"evaluated_object_digest"`
	PolicyVersion         string `json:"policy_version"`
	EvidenceVersion       string `json:"evidence_version"`
	Result                string `json:"result"`
	SourceRunID           string `json:"source_run_id"`
	TraceID               string `json:"trace_id"`
	ExpiresAt             string `json:"expires_at"`
}

func normalizeSkillTestStatus(status string) (string, bool) {
	switch strings.TrimSpace(status) {
	case "passed", "failed", "blocked", "timeout", "rejected":
		return strings.TrimSpace(status), true
	default:
		return "", false
	}
}

func validateSkillTestSafetyEvidence(testRunID, testCaseID, status, raw, agentTraceID string, now time.Time) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if status == "rejected" {
			return "null", nil
		}
		return "", bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety_evidence_json is required")
	}
	if !json.Valid([]byte(raw)) {
		return "", bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety_evidence_json must be json")
	}
	var evidence skillTestSafetyEvidence
	if err := json.Unmarshal([]byte(raw), &evidence); err != nil {
		return "", bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety_evidence_json must match SafetyEvidenceDTO")
	}
	targetRefID := testRunID
	if strings.TrimSpace(testCaseID) != "" {
		targetRefID = testCaseID
	}
	if evidence.Scene != "skill_test" ||
		evidence.TargetType != "skill_test_prompt" ||
		evidence.TargetRefID != targetRefID ||
		evidence.EvaluatedObjectDigest == "" ||
		evidence.PolicyVersion == "" ||
		evidence.EvidenceVersion == "" ||
		evidence.SourceRunID != testRunID ||
		evidence.TraceID == "" {
		return "", bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence fields do not match skill test contract")
	}
	if strings.TrimSpace(agentTraceID) != "" && evidence.TraceID != strings.TrimSpace(agentTraceID) {
		return "", bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence trace_id must match agent_trace_id")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, evidence.ExpiresAt)
	if err != nil || !expiresAt.After(now) {
		return "", bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence expires_at must be in the future")
	}
	switch evidence.Result {
	case "passed", "blocked", "failed":
	default:
		return "", bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence result must be passed, blocked or failed")
	}
	if status == "passed" && evidence.Result != "passed" {
		return "", bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "passed skill test requires passed safety evidence")
	}
	if status == "blocked" && evidence.Result != "blocked" {
		return "", bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "blocked skill test requires blocked safety evidence")
	}
	if status == "rejected" && evidence.Result == "passed" {
		return "", bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "rejected skill test cannot carry passed safety evidence")
	}
	return raw, nil
}

func skillTestRequestHash(skillID, versionID, testRunID, testCaseID, status, actualElementsJSON, errorCode, errorSummary, safetyEvidenceJSON, agentTraceID string) string {
	payload := map[string]any{
		"skill_id":             strings.TrimSpace(skillID),
		"version_id":           strings.TrimSpace(versionID),
		"test_run_id":          strings.TrimSpace(testRunID),
		"test_case_id":         strings.TrimSpace(testCaseID),
		"status":               strings.TrimSpace(status),
		"actual_elements_json": json.RawMessage(actualElementsJSON),
		"error_code":           strings.TrimSpace(errorCode),
		"error_summary":        strings.TrimSpace(errorSummary),
		"safety_evidence_json": json.RawMessage(safetyEvidenceJSON),
		"agent_trace_id":       strings.TrimSpace(agentTraceID),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		encoded = []byte(fmt.Sprintf("%s|%s|%s|%s|%s", skillID, versionID, testRunID, testCaseID, status))
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", sum[:])
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

func jsonString(raw datatypes.JSON, fallback string) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return fallback
	}
	return text
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

func errorCode(err error) string {
	var businessErr *bizerrors.BusinessError
	if errors.As(err, &businessErr) {
		return string(businessErr.Code)
	}
	return string(bizerrors.CodeInternal)
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
