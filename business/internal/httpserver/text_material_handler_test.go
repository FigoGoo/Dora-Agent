package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/textmaterial"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// textMaterialHTTPService 记录 Handler 冻结的可信命令并返回预设结果。
type textMaterialHTTPService struct {
	command      textmaterial.CreateCommand
	createCalls  int
	createResult textmaterial.CreateResult
	createErr    error
	listOwner    string
	listProject  string
	listCalls    int
	listItems    []textmaterial.TextMaterial
	listErr      error
}

// Create 实现 Handler 测试所需的最小创建边界。
func (service *textMaterialHTTPService) Create(_ context.Context, command textmaterial.CreateCommand) (textmaterial.CreateResult, error) {
	service.createCalls++
	service.command = command
	return service.createResult, service.createErr
}

// ListOwned 实现 Handler 测试所需的最小列表边界。
func (service *textMaterialHTTPService) ListOwned(_ context.Context, ownerUserID string, projectID string) ([]textmaterial.TextMaterial, error) {
	service.listCalls++
	service.listOwner = ownerUserID
	service.listProject = projectID
	return service.listItems, service.listErr
}

// textMaterialRequestIDs 为测试返回固定 UUIDv7 Request ID。
type textMaterialRequestIDs struct{ value string }

// New 返回固定测试 Request ID。
func (ids textMaterialRequestIDs) New() (string, error) { return ids.value, nil }

// newTextMaterialHandlerRouter 注册带可信 Principal 的读写路由。
func newTextMaterialHandlerRouter(t *testing.T, service *textMaterialHTTPService) (*gin.Engine, string, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	projectID, _ := uuid.NewV7()
	userID, _ := uuid.NewV7()
	requestID, _ := uuid.NewV7()
	handler, err := NewTextMaterialHandler(service, textMaterialRequestIDs{value: requestID.String()}, 16<<10)
	if err != nil {
		t.Fatalf("NewTextMaterialHandler() error = %v", err)
	}
	authenticated := func(c *gin.Context) {
		c.Request = c.Request.WithContext(auth.ContextWithPrincipal(c.Request.Context(), auth.Principal{ID: userID.String()}))
		c.Next()
	}
	router := gin.New()
	handler.Register(router, authenticated, authenticated)
	return router, projectID.String(), userID.String(), requestID.String()
}

// textMaterialHTTPFixture 创建可安全映射到公开 DTO 的完整文本素材。
func textMaterialHTTPFixture(t *testing.T, projectID string, ownerUserID string) textmaterial.TextMaterial {
	t.Helper()
	assetID, _ := uuid.NewV7()
	evidenceID, _ := uuid.NewV7()
	content := "工作台文本素材"
	return textmaterial.TextMaterial{
		AssetID: assetID.String(), EvidenceID: evidenceID.String(), OwnerUserID: ownerUserID, ProjectID: projectID,
		AssetVersion: 1, ContentDigest: textmaterial.ContentDigest(content), Content: content,
		CreatedAt: time.Date(2026, 7, 17, 10, 0, 0, 123000000, time.UTC),
	}
}

func TestTextMaterialHandlerCreatesWithTrustedOwnerAndTypedResponse(t *testing.T) {
	service := &textMaterialHTTPService{}
	router, projectID, ownerUserID, requestID := newTextMaterialHandlerRouter(t, service)
	material := textMaterialHTTPFixture(t, projectID, ownerUserID)
	service.createResult = textmaterial.CreateResult{Material: material}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/text-materials", strings.NewReader(`{"content":"工作台文本素材"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", material.AssetID)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	if service.createCalls != 1 || service.command.OwnerUserID != ownerUserID || service.command.ProjectID != projectID ||
		service.command.IdempotencyKey != material.AssetID || service.command.Content != material.Content {
		t.Fatalf("handler did not freeze trusted command: %+v", service.command)
	}
	var response TextMaterialCreateResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.RequestID != requestID || response.Replayed || response.Material.AssetID != material.AssetID ||
		response.Material.AssetVersion != 1 || response.Material.Content != material.Content ||
		response.Material.CreatedAt != material.CreatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestTextMaterialHandlerListsOwnedFullContentWithoutQueryOptions(t *testing.T) {
	service := &textMaterialHTTPService{}
	router, projectID, ownerUserID, _ := newTextMaterialHandlerRouter(t, service)
	service.listItems = []textmaterial.TextMaterial{textMaterialHTTPFixture(t, projectID, ownerUserID)}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/text-materials", nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Cache-Control") != "no-store" ||
		service.listCalls != 1 || service.listOwner != ownerUserID || service.listProject != projectID {
		t.Fatalf("list status=%d calls=%d owner=%s project=%s body=%s", recorder.Code, service.listCalls, service.listOwner, service.listProject, recorder.Body.String())
	}
	var response TextMaterialListResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || response.Items[0].Content != service.listItems[0].Content {
		t.Fatalf("unexpected list: %+v", response)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/text-materials?limit=1", nil))
	if recorder.Code != http.StatusBadRequest || service.listCalls != 1 {
		t.Fatalf("unsupported query reached service: status=%d calls=%d", recorder.Code, service.listCalls)
	}
}

func TestTextMaterialHandlerHidesMissingOrForeignProjectOnList(t *testing.T) {
	service := &textMaterialHTTPService{listErr: textmaterial.ErrProjectNotFound}
	router, projectID, _, _ := newTextMaterialHandlerRouter(t, service)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/text-materials", nil))
	if recorder.Code != http.StatusNotFound || recorder.Header().Get("Cache-Control") != "no-store" ||
		service.listCalls != 1 || !strings.Contains(recorder.Body.String(), `"code":"PROJECT_NOT_FOUND"`) {
		t.Fatalf("unexpected hidden-project response: status=%d calls=%d body=%s", recorder.Code, service.listCalls, recorder.Body.String())
	}
}

func TestTextMaterialHandlerRejectsAmbiguousOrInvalidInputBeforeService(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		key  string
	}{
		{name: "unknown field", body: `{"content":"正文","debug":true}`},
		{name: "duplicate field", body: `{"content":"正文","content":"另一正文"}`},
		{name: "non NFC", body: `{"content":"e\u0301"}`},
		{name: "invalid key", body: `{"content":"正文"}`, key: "not-a-uuid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &textMaterialHTTPService{}
			router, projectID, _, _ := newTextMaterialHandlerRouter(t, service)
			key := test.key
			if key == "" {
				id, _ := uuid.NewV7()
				key = id.String()
			}
			request := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/text-materials", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", key)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest || service.createCalls != 0 {
				t.Fatalf("invalid input reached service: status=%d calls=%d body=%s", recorder.Code, service.createCalls, recorder.Body.String())
			}
		})
	}
}

func TestTextMaterialHandlerMapsConflictWithoutLeakingPersistenceDetails(t *testing.T) {
	service := &textMaterialHTTPService{createErr: textmaterial.ErrIdempotencyConflict}
	router, projectID, _, _ := newTextMaterialHandlerRouter(t, service)
	key, _ := uuid.NewV7()
	newRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/text-materials", strings.NewReader(`{"content":"正文"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", key.String())
		return request
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newRequest())
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "IDEMPOTENCY_CONFLICT") {
		t.Fatalf("unexpected conflict response: %d %s", recorder.Code, recorder.Body.String())
	}

	service.createErr = errors.New("postgres dsn=secret SQL=INSERT")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, newRequest())
	if recorder.Code != http.StatusServiceUnavailable || strings.Contains(recorder.Body.String(), "secret") || strings.Contains(recorder.Body.String(), "SQL") {
		t.Fatalf("persistence details leaked: %d %s", recorder.Code, recorder.Body.String())
	}
}
