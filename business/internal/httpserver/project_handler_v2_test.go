package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectcreation"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type projectV2HTTPService struct {
	command projectcreation.QuickCreateV2Command
	result  projectskillbinding.QuickCreateV2Result
	err     error
	calls   int
}

func (service *projectV2HTTPService) QuickCreateV2(_ context.Context, command projectcreation.QuickCreateV2Command) (projectskillbinding.QuickCreateV2Result, error) {
	service.calls++
	service.command = command
	return service.result, service.err
}

func newProjectHandlerV2Router(t *testing.T, service *projectHTTPService, serviceV2 *projectV2HTTPService) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	userID, _ := uuid.NewV7()
	requestID, _ := uuid.NewV7()
	handler, err := NewProjectHandlerWithV2(
		service, serviceV2, projectRequestIDs{value: requestID.String()}, project.MaxInitialPromptBytes+4096,
	)
	if err != nil {
		t.Fatalf("创建 v2 Project Handler 失败: %v", err)
	}
	principalMiddleware := func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.ContextWithPrincipal(c.Request.Context(), auth.Principal{ID: userID.String()}))
		c.Next()
	}
	router := gin.New()
	handler.Register(router, principalMiddleware, principalMiddleware)
	return router, userID.String()
}

func TestProjectHandlerQuickCreateV2UsesExplicitVariantWithoutV1Fallback(t *testing.T) {
	projectID, _ := uuid.NewV7()
	skillA, _ := uuid.NewV7()
	skillB, _ := uuid.NewV7()
	v1 := &projectHTTPService{}
	v2 := &projectV2HTTPService{result: projectskillbinding.QuickCreateV2Result{ProjectID: projectID.String()}}
	router, userID := newProjectHandlerV2Router(t, v1, v2)
	body := `{"schema_version":"project_quick_create.v2","initial_prompt":"hello","enabled_skill_ids":["` + skillB.String() + `","` + skillA.String() + `"]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "intent-v2-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("v2 QuickCreate 状态=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if v2.calls != 1 || v2.command.OwnerUserID != userID || v2.command.IdempotencyKey != "intent-v2-1" ||
		v2.command.InitialPrompt != "hello" || len(v2.command.EnabledSkillIDs) != 2 || v1.command.OwnerUserID != "" {
		t.Fatalf("v2 分流或可信命令漂移: calls=%d command=%+v v1=%+v", v2.calls, v2.command, v1.command)
	}
}

func TestProjectHandlerQuickCreateV2AcceptsExplicitEmptySelection(t *testing.T) {
	projectID, _ := uuid.NewV7()
	v1 := &projectHTTPService{}
	v2 := &projectV2HTTPService{result: projectskillbinding.QuickCreateV2Result{ProjectID: projectID.String()}}
	router, _ := newProjectHandlerV2Router(t, v1, v2)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(
		`{"schema_version":"project_quick_create.v2","initial_prompt":null,"enabled_skill_ids":[]}`,
	))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "intent-v2-empty")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || v2.calls != 1 || v2.command.EnabledSkillIDs == nil || len(v2.command.EnabledSkillIDs) != 0 {
		t.Fatalf("显式空 v2 未保留 empty 数组: status=%d command=%+v body=%s", recorder.Code, v2.command, recorder.Body.String())
	}
}

func TestProjectHandlerQuickCreateVariantsRejectAmbiguousOrMalformedJSON(t *testing.T) {
	invalidBodies := map[string]string{
		"v1 cannot inject skills":      `{"initial_prompt":"x","enabled_skill_ids":[]}`,
		"schema null":                  `{"schema_version":null,"enabled_skill_ids":[]}`,
		"schema v1 is not explicit v2": `{"schema_version":"project_quick_create.v1","enabled_skill_ids":[]}`,
		"missing enabled skills":       `{"schema_version":"project_quick_create.v2"}`,
		"null enabled skills":          `{"schema_version":"project_quick_create.v2","enabled_skill_ids":null}`,
		"enabled skills is not array":  `{"schema_version":"project_quick_create.v2","enabled_skill_ids":"x"}`,
		"unknown v2 field":             `{"schema_version":"project_quick_create.v2","enabled_skill_ids":[],"priority":1}`,
		"trailing v2 document":         `{"schema_version":"project_quick_create.v2","enabled_skill_ids":[]} {}`,
	}
	for name, body := range invalidBodies {
		t.Run(name, func(t *testing.T) {
			v1 := &projectHTTPService{}
			v2 := &projectV2HTTPService{}
			router, _ := newProjectHandlerV2Router(t, v1, v2)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "intent-v2-invalid")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest || v2.calls != 0 || v1.command.OwnerUserID != "" {
				t.Fatalf("歧义 JSON 到达应用服务: status=%d v2calls=%d v1=%+v body=%s", recorder.Code, v2.calls, v1.command, recorder.Body.String())
			}
		})
	}
}

func TestProjectHandlerV1OnlyDoesNotDowngradeExplicitV2(t *testing.T) {
	v1 := &projectHTTPService{}
	router, _, _ := newProjectHandlerRouter(t, v1)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(
		`{"schema_version":"project_quick_create.v2","enabled_skill_ids":[]}`,
	))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "intent-v2-disabled")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || v1.command.OwnerUserID != "" ||
		!strings.Contains(recorder.Body.String(), "PROJECT_SKILL_SNAPSHOT_V2_UNAVAILABLE") {
		t.Fatalf("显式 v2 被错误降级: status=%d v1=%+v body=%s", recorder.Code, v1.command, recorder.Body.String())
	}
}

func TestProjectHandlerMapsV2ErrorsWithoutLeakingCause(t *testing.T) {
	testCases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "disabled", err: projectcreation.ErrV2Disabled, status: http.StatusServiceUnavailable, code: "PROJECT_SKILL_SNAPSHOT_V2_UNAVAILABLE"},
		{name: "invalid binding", err: projectskillbinding.ErrInvalidBinding, status: http.StatusBadRequest, code: "PROJECT_SKILL_BINDING_INVALID"},
		{name: "unavailable", err: projectskillbinding.ErrSkillUnavailable, status: http.StatusConflict, code: "PROJECT_SKILL_UNAVAILABLE"},
		{name: "governance unavailable", err: projectskillbinding.ErrGovernanceUnavailable, status: http.StatusConflict, code: "PROJECT_SKILL_UNAVAILABLE"},
		{name: "invalid snapshot", err: projectskillbinding.ErrSnapshotInvalid, status: http.StatusConflict, code: "PROJECT_SKILL_SNAPSHOT_INVALID"},
		{name: "limit", err: projectskillbinding.ErrSnapshotLimitExceeded, status: http.StatusRequestEntityTooLarge, code: "SNAPSHOT_LIMIT_EXCEEDED"},
		{name: "protection", err: projectskillbinding.ErrContentProtection, status: http.StatusServiceUnavailable, code: "PROJECT_SKILL_SNAPSHOT_PROTECTION_UNAVAILABLE"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			v2 := &projectV2HTTPService{err: errors.Join(testCase.err, errors.New("postgres dsn=secret runtime body"))}
			router, _ := newProjectHandlerV2Router(t, &projectHTTPService{}, v2)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(
				`{"schema_version":"project_quick_create.v2","enabled_skill_ids":[]}`,
			))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "intent-v2-error")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != testCase.status || !strings.Contains(recorder.Body.String(), testCase.code) ||
				strings.Contains(recorder.Body.String(), "secret") || strings.Contains(recorder.Body.String(), "runtime body") {
				t.Fatalf("v2 错误映射漂移或泄漏: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
