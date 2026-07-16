package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// skillHTTPService 是 Handler 测试使用的可观察应用边界，不执行持久化。
type skillHTTPService struct {
	createCommand skill.CreateCommand
	createResult  skill.CreateResult
	createErr     error
	listOwnerID   string
	listCursor    string
	listResult    skill.OwnerListResult
	listErr       error
	findSkillID   string
	findOwnerID   string
	findResult    skill.OwnerSkillDTO
	findErr       error
	updateCommand skill.UpdateDraftCommand
	updateResult  skill.OwnerSkillDTO
	updateErr     error
	submitCommand skill.SubmitReviewCommand
	submitResult  skill.SubmitReviewServiceResult
	submitErr     error
}

// Create 记录创建命令并返回预设结果。
func (service *skillHTTPService) Create(_ context.Context, command skill.CreateCommand) (skill.CreateResult, error) {
	service.createCommand = command
	return service.createResult, service.createErr
}

// ListOwned 记录可信 Owner 与 cursor 并返回预设结果。
func (service *skillHTTPService) ListOwned(_ context.Context, ownerUserID string, cursor string) (skill.OwnerListResult, error) {
	service.listOwnerID = ownerUserID
	service.listCursor = cursor
	return service.listResult, service.listErr
}

// FindOwnedByID 记录资源和可信 Owner 并返回预设结果。
func (service *skillHTTPService) FindOwnedByID(_ context.Context, skillID string, ownerUserID string) (skill.OwnerSkillDTO, error) {
	service.findSkillID = skillID
	service.findOwnerID = ownerUserID
	return service.findResult, service.findErr
}

// UpdateDraft 记录完整替换命令并返回预设结果。
func (service *skillHTTPService) UpdateDraft(_ context.Context, command skill.UpdateDraftCommand) (skill.OwnerSkillDTO, error) {
	service.updateCommand = command
	return service.updateResult, service.updateErr
}

// SubmitReview 记录审核提交命令并返回预设结果。
func (service *skillHTTPService) SubmitReview(_ context.Context, command skill.SubmitReviewCommand) (skill.SubmitReviewServiceResult, error) {
	service.submitCommand = command
	return service.submitResult, service.submitErr
}

// mustSkillHandlerForServerTest 创建 Server 注册测试使用的最小 Skill Handler。
func mustSkillHandlerForServerTest(t *testing.T) *SkillHandler {
	t.Helper()
	handler, err := NewSkillHandler(&skillHTTPService{}, authHandlerTestIDs{}, 64*1024)
	if err != nil {
		t.Fatalf("create server test skill handler: %v", err)
	}
	return handler
}

// newSkillHandlerRouter 创建已写入私有可信 Principal 的 Handler 单元测试 Router。
func newSkillHandlerRouter(t *testing.T, service *skillHTTPService) (*gin.Engine, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	userID, _ := uuid.NewV7()
	skillID, _ := uuid.NewV7()
	requestID, _ := uuid.NewV7()
	handler, err := NewSkillHandler(service, projectRequestIDs{value: requestID.String()}, 256*1024)
	if err != nil {
		t.Fatalf("create skill handler: %v", err)
	}
	principalMiddleware := func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.ContextWithPrincipal(c.Request.Context(), auth.Principal{ID: userID.String()}))
		c.Next()
	}
	router := gin.New()
	handler.Register(router, principalMiddleware, principalMiddleware)
	return router, skillID.String(), userID.String()
}

// validSkillHTTPDefinition 返回六个能力字段完整且公共 Tool 引用为空的请求定义。
func validSkillHTTPDefinition() skill.SkillDefinitionV1 {
	capability := skill.CapabilityGuidanceV1{Applicability: "enabled", Guidance: "按确定步骤处理", NotApplicableReason: ""}
	return skill.SkillDefinitionV1{
		SchemaVersion: skill.DefinitionSchemaVersionV1,
		Name:          "视频策划助手", Summary: "将需求整理为可执行方案", Category: "video", Tags: []string{"策划", "视频"},
		InputDescription: "用户目标和素材", OutputDescription: "结构化创作方案", InvocationRules: "目标匹配时使用",
		PlanCreationSpec: capability, AnalyzeMaterials: capability, PlanStoryboard: capability,
		GenerateMedia: capability, WritePrompts: capability, AssembleOutput: capability,
		Examples:       []skill.SkillExampleV1{{Input: "制作产品视频", Output: "输出创作方案"}},
		StarterPrompts: []string{"帮我策划产品视频"}, MarketListing: skill.MarketListingV1{Detail: "视频策划能力"},
		PublicToolRefs: []skill.PublicToolReferenceV1{},
	}
}

// definitionRequestJSON 编码 Handler 测试使用的严格请求 Envelope。
func definitionRequestJSON(t *testing.T, definition skill.SkillDefinitionV1) string {
	t.Helper()
	encoded, err := json.Marshal(SkillDefinitionRequest{Definition: definition})
	if err != nil {
		t.Fatalf("encode skill definition request: %v", err)
	}
	return string(encoded)
}

func TestSkillHandlerCreateUsesTrustedPrincipalAndReturnsETag(t *testing.T) {
	skillID, _ := uuid.NewV7()
	definition := validSkillHTTPDefinition()
	projection := skill.OwnerSkillDTO{
		SkillID: skillID.String(), Definition: definition, ContentStatus: "draft", HasUnpublishedChanges: true,
		GovernanceStatus: skill.GovernanceStatusActive, AllowedActions: []string{"edit_draft", "submit_review"}, DraftETag: `"s1-test"`,
	}
	service := &skillHTTPService{createResult: skill.CreateResult{Skill: projection}}
	router, _, ownerID := newSkillHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/skills", strings.NewReader(definitionRequestJSON(t, definition)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "skill-create-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated || recorder.Header().Get("ETag") != projection.DraftETag {
		t.Fatalf("create status=%d etag=%q body=%s", recorder.Code, recorder.Header().Get("ETag"), recorder.Body.String())
	}
	if service.createCommand.OwnerUserID != ownerID || service.createCommand.IdempotencyKey != "skill-create-1" || service.createCommand.Definition.Name != definition.Name {
		t.Fatalf("handler did not use trusted owner/strict DTO: %+v", service.createCommand)
	}
	var response OwnerSkillResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Skill.ReviewStatus != nil || response.Skill.SkillID != skillID.String() {
		t.Fatalf("invalid create response: error=%v response=%+v", err, response)
	}
}

func TestSkillHandlerRejectsUnknownJSONBeforeService(t *testing.T) {
	service := &skillHTTPService{}
	router, _, _ := newSkillHandlerRouter(t, service)
	body := strings.TrimSuffix(definitionRequestJSON(t, validSkillHTTPDefinition()), "}") + `,"owner_user_id":"forged"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/skills", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "skill-create-2")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.createCommand.OwnerUserID != "" {
		t.Fatalf("unknown field reached service: status=%d command=%+v body=%s", recorder.Code, service.createCommand, recorder.Body.String())
	}
}

func TestSkillHandlerReturnsFieldErrorForUnavailablePublicToolRefs(t *testing.T) {
	definition := validSkillHTTPDefinition()
	definition.PublicToolRefs = []skill.PublicToolReferenceV1{skill.PublicToolReferenceV1(json.RawMessage(`{"tool_key":"forbidden"}`))}
	validation := &skill.ValidationError{
		Cause:       skill.ErrToolReferenceUnavailable,
		FieldErrors: []skill.FieldError{{Field: "public_tool_refs", Code: "SKILL_TOOL_REFERENCE_UNAVAILABLE", Message: "公共 Tool 引用入口尚未开放"}},
	}
	service := &skillHTTPService{createErr: validation}
	router, _, _ := newSkillHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/skills", strings.NewReader(definitionRequestJSON(t, definition)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "skill-create-3")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"SKILL_TOOL_REFERENCE_UNAVAILABLE"`) ||
		!strings.Contains(recorder.Body.String(), `"field":"definition.public_tool_refs"`) {
		t.Fatalf("missing public tool field error: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSkillHandlerPrefixesUnavailableAssetFieldError(t *testing.T) {
	definition := validSkillHTTPDefinition()
	assetID := "019f0000-0000-7000-8000-000000000123"
	definition.MarketListing.CoverAssetID = &assetID
	service := &skillHTTPService{createErr: &skill.ValidationError{
		Cause: skill.ErrInvalidDefinition,
		FieldErrors: []skill.FieldError{{
			Field: "market_listing.cover_asset_id", Code: "ASSET_REFERENCE_UNAVAILABLE", Message: "封面 Asset 引用入口尚未开放",
		}},
	}}
	router, _, _ := newSkillHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/skills", strings.NewReader(definitionRequestJSON(t, definition)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "skill-create-asset-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"field":"definition.market_listing.cover_asset_id"`) ||
		!strings.Contains(recorder.Body.String(), `"code":"ASSET_REFERENCE_UNAVAILABLE"`) {
		t.Fatalf("missing prefixed Asset field error: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSkillHandlerListRequiresMineAndUsesOwner(t *testing.T) {
	service := &skillHTTPService{listResult: skill.OwnerListResult{Items: []skill.OwnerSkillDTO{}}}
	router, _, ownerID := newSkillHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/skills?scope=mine&cursor=opaque", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.listOwnerID != ownerID || service.listCursor != "opaque" ||
		!strings.Contains(recorder.Body.String(), `"items":[]`) || !strings.Contains(recorder.Body.String(), `"next_cursor":null`) {
		t.Fatalf("invalid owner list: status=%d owner=%q cursor=%q body=%s", recorder.Code, service.listOwnerID, service.listCursor, recorder.Body.String())
	}
}

func TestSkillHandlerMapsCrossOwnerToSafe404(t *testing.T) {
	service := &skillHTTPService{findErr: skill.ErrSkillNotFound}
	router, skillID, _ := newSkillHandlerRouter(t, service)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/skills/"+skillID, nil))
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"SKILL_NOT_FOUND"`) {
		t.Fatalf("unsafe owner lookup response: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSkillHandlerUpdatePassesOpaqueIfMatch(t *testing.T) {
	definition := validSkillHTTPDefinition()
	projection := skill.OwnerSkillDTO{SkillID: "unused", DraftETag: `"s1-new"`, AllowedActions: []string{}}
	service := &skillHTTPService{updateResult: projection}
	router, skillID, ownerID := newSkillHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/skills/"+skillID+"/draft", strings.NewReader(definitionRequestJSON(t, definition)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"s1-old"`)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.updateCommand.OwnerUserID != ownerID || service.updateCommand.IfMatch != `"s1-old"` {
		t.Fatalf("invalid draft replace: status=%d command=%+v body=%s", recorder.Code, service.updateCommand, recorder.Body.String())
	}
}

func TestSkillHandlerSubmitRejectsInjectedReviewFields(t *testing.T) {
	service := &skillHTTPService{}
	router, skillID, _ := newSkillHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/skills/"+skillID+"/reviews", strings.NewReader(`{"status":"approved"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "review-submit-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.submitCommand.SkillID != "" {
		t.Fatalf("review field injection reached service: status=%d command=%+v body=%s", recorder.Code, service.submitCommand, recorder.Body.String())
	}
}

func TestSkillHandlerSubmitRejectsJSONNullBeforeService(t *testing.T) {
	service := &skillHTTPService{}
	router, skillID, _ := newSkillHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/skills/"+skillID+"/reviews", strings.NewReader("null"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "review-submit-null-1")
	request.Header.Set("If-Match", `"s1-current"`)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.submitCommand.SkillID != "" ||
		!strings.Contains(recorder.Body.String(), `"code":"INVALID_REQUEST"`) {
		t.Fatalf("JSON null reached review service: status=%d command=%+v body=%s", recorder.Code, service.submitCommand, recorder.Body.String())
	}
}

func TestSkillHandlerSubmitPassesOpaqueIfMatchAndIdempotencyKey(t *testing.T) {
	reviewID, _ := uuid.NewV7()
	projection := skill.OwnerSkillDTO{SkillID: "unused", DraftETag: `"s1-current"`, AllowedActions: []string{"edit_draft"}}
	service := &skillHTTPService{submitResult: skill.SubmitReviewServiceResult{
		Skill: projection, ReviewID: reviewID.String(),
	}}
	router, skillID, ownerID := newSkillHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/skills/"+skillID+"/reviews", nil)
	request.Header.Set("Idempotency-Key", "review-submit-if-match-1")
	request.Header.Set("If-Match", `"s1-current"`)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || service.submitCommand.OwnerUserID != ownerID ||
		service.submitCommand.SkillID != skillID || service.submitCommand.IdempotencyKey != "review-submit-if-match-1" ||
		service.submitCommand.IfMatch != `"s1-current"` {
		t.Fatalf("submit did not forward trusted owner/If-Match/key: status=%d command=%+v body=%s", recorder.Code, service.submitCommand, recorder.Body.String())
	}
}

func TestSkillHandlerMapsDraftConflict(t *testing.T) {
	service := &skillHTTPService{updateErr: skill.ErrDraftConflict}
	router, skillID, _ := newSkillHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/skills/"+skillID+"/draft", strings.NewReader(definitionRequestJSON(t, validSkillHTTPDefinition())))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"stale"`)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict || !errors.Is(service.updateErr, skill.ErrDraftConflict) || !strings.Contains(recorder.Body.String(), `"code":"SKILL_DRAFT_CONFLICT"`) {
		t.Fatalf("draft conflict status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
