package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type projectHTTPService struct {
	command         project.QuickCreateCommand
	quickResult     project.QuickCreateResult
	quickErr        error
	bootstrapResult project.BootstrapResult
	bootstrapErr    error
	bootstrapCalls  int
	listQuery       project.ProjectListQuery
	listResult      project.ProjectListResult
	listErr         error
	listCalls       int
}

func (service *projectHTTPService) QuickCreate(_ context.Context, command project.QuickCreateCommand) (project.QuickCreateResult, error) {
	service.command = command
	return service.quickResult, service.quickErr
}
func (service *projectHTTPService) Bootstrap(_ context.Context, _, _ string) (project.BootstrapResult, error) {
	service.bootstrapCalls++
	return service.bootstrapResult, service.bootstrapErr
}
func (service *projectHTTPService) ListOwned(_ context.Context, query project.ProjectListQuery) (project.ProjectListResult, error) {
	service.listCalls++
	service.listQuery = query
	return service.listResult, service.listErr
}

type projectRequestIDs struct{ value string }

func (ids projectRequestIDs) New() (string, error) { return ids.value, nil }

func newProjectHandlerRouter(t *testing.T, service *projectHTTPService) (*gin.Engine, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	projectID, _ := uuid.NewV7()
	userID, _ := uuid.NewV7()
	requestID, _ := uuid.NewV7()
	handler, err := NewProjectHandler(service, projectRequestIDs{value: requestID.String()}, project.MaxInitialPromptBytes+1024)
	if err != nil {
		t.Fatalf("create project handler: %v", err)
	}
	principalMiddleware := func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.ContextWithPrincipal(c.Request.Context(), auth.Principal{ID: userID.String()}))
		c.Next()
	}
	router := gin.New()
	handler.Register(router, principalMiddleware, principalMiddleware)
	return router, projectID.String(), userID.String()
}

func decodeProjectHTTPBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return body
}

func TestProjectHandlerQuickCreateUsesTrustedPrincipalAndReturns201(t *testing.T) {
	projectID, _ := uuid.NewV7()
	service := &projectHTTPService{quickResult: project.QuickCreateResult{ProjectID: projectID.String()}}
	router, _, userID := newProjectHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(`{"initial_prompt":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "intent-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if service.command.OwnerUserID != userID || service.command.IdempotencyKey != "intent-1" || service.command.InitialPrompt != "hello" || service.bootstrapCalls != 0 {
		t.Fatalf("handler trusted unvalidated input or waited for Agent: %+v calls=%d", service.command, service.bootstrapCalls)
	}
	body := decodeProjectHTTPBody(t, recorder)
	if body["project_id"] != projectID.String() || body["creation_status"] != "provisioning" || body["workspace_ref"] != "/projects/"+projectID.String()+"/workspace" {
		t.Fatalf("unexpected quick create response: %+v", body)
	}
}

func TestProjectHandlerRejectsInvalidUTF8AndUnpairedUnicodeEscapes(t *testing.T) {
	projectID, _ := uuid.NewV7()
	for name, body := range map[string][]byte{
		"invalid UTF-8":   append([]byte(`{"initial_prompt":"`), []byte{0xff, '"', '}'}...),
		"high surrogate":  []byte(`{"initial_prompt":"\ud800"}`),
		"low surrogate":   []byte(`{"initial_prompt":"\udc00"}`),
		"mismatched pair": []byte(`{"initial_prompt":"\ud800\u0041"}`),
	} {
		t.Run(name, func(t *testing.T) {
			service := &projectHTTPService{quickResult: project.QuickCreateResult{ProjectID: projectID.String()}}
			router, _, _ := newProjectHandlerRouter(t, service)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "intent-invalid-unicode")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest || service.command.OwnerUserID != "" {
				t.Fatalf("invalid Unicode reached Project service: status=%d command=%+v body=%s", recorder.Code, service.command, recorder.Body.String())
			}
		})
	}
}

func TestProjectHandlerAcceptsValidSurrogatePair(t *testing.T) {
	projectID, _ := uuid.NewV7()
	service := &projectHTTPService{quickResult: project.QuickCreateResult{ProjectID: projectID.String()}}
	router, _, _ := newProjectHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(`{"initial_prompt":"\ud83d\ude80"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "intent-valid-surrogate")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated || service.command.InitialPrompt != "🚀" {
		t.Fatalf("valid surrogate pair rejected or changed: status=%d command=%+v body=%s", recorder.Code, service.command, recorder.Body.String())
	}
}

func TestProjectHandlerReplayCanReturnReadyReceipt(t *testing.T) {
	projectID, _ := uuid.NewV7()
	sessionID, _ := uuid.NewV7()
	inputID, _ := uuid.NewV7()
	service := &projectHTTPService{
		quickResult: project.QuickCreateResult{ProjectID: projectID.String(), IdempotentReplay: true},
		bootstrapResult: project.BootstrapResult{
			ProjectID: projectID.String(), Title: project.DefaultProjectTitle,
			LifecycleStatus: project.LifecycleStatusActive, RecentRunStatus: project.RecentRunStatusQueued,
			InitialPromptStatus: project.InitialPromptStatusAccepted, ProvisioningStatus: project.ProvisioningStatusReady,
			AgentSessionID: stringTestPointer(sessionID.String()), AgentInputID: stringTestPointer(inputID.String()), UpdatedAt: time.Now().UTC(),
		},
	}
	router, _, _ := newProjectHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(`{"initial_prompt":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "intent-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || service.bootstrapCalls != 1 {
		t.Fatalf("expected replay bootstrap 200, got %d calls=%d body=%s", recorder.Code, service.bootstrapCalls, recorder.Body.String())
	}
	body := decodeProjectHTTPBody(t, recorder)
	if body["creation_status"] != "ready" || body["session_id"] != sessionID.String() || body["input_id"] != inputID.String() {
		t.Fatalf("unexpected ready replay: %+v", body)
	}
}

func TestProjectHandlerBootstrapAndStableErrors(t *testing.T) {
	projectID, _ := uuid.NewV7()
	service := &projectHTTPService{bootstrapResult: project.BootstrapResult{
		ProjectID: projectID.String(), Title: project.DefaultProjectTitle,
		LifecycleStatus: project.LifecycleStatusActive, RecentRunStatus: project.RecentRunStatusIdle,
		InitialPromptStatus: project.InitialPromptStatusAbsent, ProvisioningStatus: project.ProvisioningStatusPending,
		UpdatedAt: time.Date(2026, 7, 14, 3, 0, 0, 0, time.UTC),
	}}
	router, _, _ := newProjectHandlerRouter(t, service)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID.String()+"/bootstrap", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected bootstrap 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	body := decodeProjectHTTPBody(t, recorder)
	if body["creation_status"] != "provisioning" || body["project_id"] != projectID.String() || body["session_id"] != nil {
		t.Fatalf("unexpected bootstrap response: %+v", body)
	}

	service.bootstrapErr = errors.New("postgres dsn=secret SQL=SELECT")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID.String()+"/bootstrap", nil))
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), "secret") || strings.Contains(recorder.Body.String(), "SELECT") {
		t.Fatalf("bootstrap error leaked details: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestProjectHandlerListProjectsUsesTrustedOwnerAndStableKeysetContract(t *testing.T) {
	projectID, _ := uuid.NewV7()
	secondProjectID, _ := uuid.NewV7()
	updatedAt := time.Date(2026, 7, 17, 9, 8, 7, 123000000, time.UTC)
	secondUpdatedAt := updatedAt.Add(-time.Minute)
	service := &projectHTTPService{listResult: project.ProjectListResult{
		Items: []project.ProjectListItem{
			{
				ProjectID: projectID.String(), Title: "真实项目", LifecycleStatus: project.LifecycleStatusActive,
				RecentRunStatus: project.RecentRunStatusRunning, InitialPromptStatus: project.InitialPromptStatusAccepted,
				UpdatedAt: updatedAt,
			},
			{
				ProjectID: secondProjectID.String(), Title: "归档项目", LifecycleStatus: project.LifecycleStatusArchived,
				RecentRunStatus: project.RecentRunStatusSucceeded, InitialPromptStatus: project.InitialPromptStatusAbsent,
				UpdatedAt: secondUpdatedAt,
			},
		},
		NextAfter: &project.ProjectListCursor{UpdatedAt: secondUpdatedAt, ProjectID: secondProjectID.String()},
	}}
	router, _, trustedUserID := newProjectHandlerRouter(t, service)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/projects?limit=2", nil))

	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected project list 200/no-store, got %d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	if service.listCalls != 1 || service.listQuery.OwnerUserID != trustedUserID || service.listQuery.Limit != 2 || service.listQuery.After != nil {
		t.Fatalf("project list did not freeze trusted owner/query: calls=%d query=%+v", service.listCalls, service.listQuery)
	}
	var response ProjectListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode project list response: %v", err)
	}
	if len(response.Items) != 2 || response.Items[0].ProjectID != projectID.String() ||
		response.Items[0].WorkspaceRef != "/projects/"+projectID.String()+"/workspace" ||
		response.Items[0].UpdatedAt != updatedAt.Format(time.RFC3339Nano) || response.NextAfter == nil {
		t.Fatalf("unexpected project list response: %+v", response)
	}
	decodedCursor, err := decodeProjectListCursor(*response.NextAfter)
	if err != nil || decodedCursor.ProjectID != secondProjectID.String() || !decodedCursor.UpdatedAt.Equal(secondUpdatedAt) {
		t.Fatalf("next_after did not round-trip: cursor=%+v err=%v", decodedCursor, err)
	}
}

func TestProjectHandlerListProjectsDecodesAfterAndRejectsAmbiguousQueries(t *testing.T) {
	projectID, _ := uuid.NewV7()
	afterTime := time.Date(2026, 7, 17, 8, 0, 0, 456000000, time.UTC)
	after, err := encodeProjectListCursor(project.ProjectListCursor{UpdatedAt: afterTime, ProjectID: projectID.String()})
	if err != nil {
		t.Fatal(err)
	}
	service := &projectHTTPService{listResult: project.ProjectListResult{Items: []project.ProjectListItem{}}}
	router, _, _ := newProjectHandlerRouter(t, service)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/projects?limit=25&after="+after, nil))
	if recorder.Code != http.StatusOK || service.listCalls != 1 || service.listQuery.After == nil ||
		service.listQuery.After.ProjectID != projectID.String() || !service.listQuery.After.UpdatedAt.Equal(afterTime) {
		t.Fatalf("valid after cursor not forwarded: status=%d calls=%d query=%+v body=%s", recorder.Code, service.listCalls, service.listQuery, recorder.Body.String())
	}

	for _, rawQuery := range []string{
		"limit=0", "limit=101", "limit=abc", "limit=1&limit=2", "after=", "after=not-base64", "owner_user_id=forged",
	} {
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/projects?"+rawQuery, nil))
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "PROJECT_LIST_QUERY_INVALID") {
			t.Fatalf("query %q: expected stable 400, got %d body=%s", rawQuery, recorder.Code, recorder.Body.String())
		}
	}
	if service.listCalls != 1 {
		t.Fatalf("invalid project list query reached service: calls=%d", service.listCalls)
	}
}

func TestProjectHandlerRejectsUnknownJSONAndMapsConflict(t *testing.T) {
	service := &projectHTTPService{}
	router, _, _ := newProjectHandlerRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(`{"initial_prompt":"hello","user_id":"forged"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "intent-1")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || service.command.OwnerUserID != "" {
		t.Fatalf("unknown JSON reached service: %d command=%+v", recorder.Code, service.command)
	}

	service.quickErr = project.ErrIdempotencyConflict
	request = httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(`{"initial_prompt":null}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "intent-1")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("expected stable conflict, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func stringTestPointer(value string) *string { return &value }
