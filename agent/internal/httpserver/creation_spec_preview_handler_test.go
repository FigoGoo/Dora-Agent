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

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/health"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	previewruntime "github.com/FigoGoo/Dora-Agent/agent/internal/runtime"
	agenttool "github.com/FigoGoo/Dora-Agent/agent/internal/tool"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"github.com/gin-gonic/gin"
)

const (
	previewHTTPTestRequestID      = "019f68e8-0100-7000-8000-000000000100"
	previewHTTPTestUserID         = "019f68e8-0101-7000-8000-000000000101"
	previewHTTPTestProjectID      = "019f68e8-0102-7000-8000-000000000102"
	previewHTTPTestSessionID      = "019f68e8-0103-7000-8000-000000000103"
	previewHTTPTestIdempotencyKey = "019f68e8-0104-7000-8000-000000000104"
	previewHTTPTestInputID        = "019f68e8-0105-7000-8000-000000000105"
	previewHTTPTestErrorID        = "019f68e8-0106-7000-8000-000000000106"
	previewHTTPTestErrorID2       = "019f68e8-0107-7000-8000-000000000107"
	previewHTTPTestPath           = "/internal/v1/workspaces/sessions/019f68e8-0103-7000-8000-000000000103/creation-spec-previews"
	previewHTTPTestBody           = `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"制作品牌短片","deliverable_type":"video","audience":"","locale":"zh-CN","constraints":[]}`
)

type previewHTTPVerifier struct {
	request httpidentity.Request
	claims  httpidentity.Claims
	err     error
	calls   int
}

func (verifier *previewHTTPVerifier) Verify(_ context.Context, request httpidentity.Request) (httpidentity.Claims, error) {
	verifier.calls++
	verifier.request = request
	return verifier.claims, verifier.err
}

type previewHTTPService struct {
	command previewruntime.EnqueueCommand
	result  previewruntime.EnqueueResult
	err     error
	calls   int
}

func (service *previewHTTPService) Enqueue(_ context.Context, command previewruntime.EnqueueCommand) (previewruntime.EnqueueResult, error) {
	service.calls++
	service.command = command
	return service.result, service.err
}

type previewHTTPIDs struct{}

func (previewHTTPIDs) New() (string, error) { return previewHTTPTestErrorID, nil }

type previewFlagOffIDs struct {
	index int
}

func (ids *previewFlagOffIDs) New() (string, error) {
	values := [...]string{previewHTTPTestErrorID, previewHTTPTestErrorID2}
	if ids.index >= len(values) {
		return "", errors.New("test IDs exhausted")
	}
	value := values[ids.index]
	ids.index++
	return value, nil
}

// TestCreationSpecPreviewPOSTReturnsExactAcceptedDTO 验证唯一 POST 的 Scope、可信身份映射与 202 exact-set。
func TestCreationSpecPreviewPOSTReturnsExactAcceptedDTO(t *testing.T) {
	verifier := newPreviewHTTPVerifier()
	service := &previewHTTPService{result: previewruntime.EnqueueResult{
		RequestID: previewHTTPTestRequestID, SessionID: previewHTTPTestSessionID,
		InputID: previewHTTPTestInputID, Status: "pending",
	}}
	router := newPreviewHTTPRouter(t, verifier, service)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newPreviewHTTPRequest(http.MethodPost, previewHTTPTestPath, previewHTTPTestBody))

	if recorder.Code != http.StatusAccepted || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("202 响应错误: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	wantBody := `{"schema_version":"plan_creation_spec.preview.enqueue.v1","request_id":"019f68e8-0100-7000-8000-000000000100","session_id":"019f68e8-0103-7000-8000-000000000103","input_id":"019f68e8-0105-7000-8000-000000000105","status":"pending"}`
	if strings.TrimSpace(recorder.Body.String()) != wantBody {
		t.Fatalf("202 exact-set 漂移:\n got: %s\nwant: %s", recorder.Body.String(), wantBody)
	}
	if verifier.calls != 1 || verifier.request.Method != http.MethodPost ||
		verifier.request.CanonicalTarget != previewHTTPTestPath ||
		verifier.request.Scope != httpidentity.ScopeCreationSpecPreviewWrite ||
		verifier.request.AgentSessionID != previewHTTPTestSessionID {
		t.Fatalf("身份 Scope/Target 映射错误: calls=%d request=%+v", verifier.calls, verifier.request)
	}
	if service.calls != 1 || service.command.RequestID != previewHTTPTestRequestID ||
		service.command.IdempotencyKey != previewHTTPTestIdempotencyKey ||
		service.command.UserID != previewHTTPTestUserID || service.command.ProjectID != previewHTTPTestProjectID ||
		service.command.SessionID != previewHTTPTestSessionID || service.command.Intent.Audience == nil ||
		*service.command.Intent.Audience != "" {
		t.Fatalf("Runtime 命令映射错误: calls=%d command=%+v", service.calls, service.command)
	}
}

// TestCreationSpecPreviewPOSTRejectsNonCanonicalPathAndHeaders 验证身份断言消费前先拒绝非唯一 Path、Query 与协议 Header。
func TestCreationSpecPreviewPOSTRejectsNonCanonicalPathAndHeaders(t *testing.T) {
	testCases := []struct {
		name   string
		method string
		path   string
		mutate func(*http.Request)
		want   int
	}{
		{name: "query forbidden", method: http.MethodPost, path: previewHTTPTestPath + "?debug=1", want: http.StatusBadRequest},
		{name: "uppercase UUID forbidden", method: http.MethodPost, path: strings.Replace(previewHTTPTestPath, "8000", "800A", 1), want: http.StatusBadRequest},
		{name: "escaped canonical UUID forbidden", method: http.MethodPost, path: strings.Replace(previewHTTPTestPath, "/019f", "/%30%31%39f", 1), want: http.StatusBadRequest},
		{name: "wrong method", method: http.MethodGet, path: previewHTTPTestPath, want: http.StatusNotFound},
		{name: "missing content type", method: http.MethodPost, path: previewHTTPTestPath, mutate: func(request *http.Request) {
			request.Header.Del("Content-Type")
		}, want: http.StatusBadRequest},
		{name: "content type parameters forbidden", method: http.MethodPost, path: previewHTTPTestPath, mutate: func(request *http.Request) {
			request.Header.Set("Content-Type", "application/json; charset=utf-8")
		}, want: http.StatusBadRequest},
		{name: "duplicate content type", method: http.MethodPost, path: previewHTTPTestPath, mutate: func(request *http.Request) {
			request.Header["Content-Type"] = []string{"application/json", "application/json"}
		}, want: http.StatusBadRequest},
		{name: "missing idempotency key", method: http.MethodPost, path: previewHTTPTestPath, mutate: func(request *http.Request) {
			request.Header.Del("Idempotency-Key")
		}, want: http.StatusBadRequest},
		{name: "invalid idempotency key", method: http.MethodPost, path: previewHTTPTestPath, mutate: func(request *http.Request) {
			request.Header.Set("Idempotency-Key", "not-a-uuid")
		}, want: http.StatusBadRequest},
		{name: "duplicate idempotency key", method: http.MethodPost, path: previewHTTPTestPath, mutate: func(request *http.Request) {
			request.Header["Idempotency-Key"] = []string{previewHTTPTestIdempotencyKey, previewHTTPTestIdempotencyKey}
		}, want: http.StatusBadRequest},
	}
	for _, fixture := range testCases {
		t.Run(fixture.name, func(t *testing.T) {
			verifier := newPreviewHTTPVerifier()
			service := &previewHTTPService{}
			router := newPreviewHTTPRouter(t, verifier, service)
			request := newPreviewHTTPRequest(fixture.method, fixture.path, previewHTTPTestBody)
			if fixture.mutate != nil {
				fixture.mutate(request)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != fixture.want {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, fixture.want, recorder.Body.String())
			}
			if verifier.calls != 0 || service.calls != 0 {
				t.Fatalf("非法边界仍消费身份或调用 Service: verifier=%d service=%d", verifier.calls, service.calls)
			}
		})
	}
}

// TestCreationSpecPreviewPOSTRejectsStrictBodyViolations 验证 trailing、unknown、duplicate、null 与有界正文全部失败关闭。
func TestCreationSpecPreviewPOSTRejectsStrictBodyViolations(t *testing.T) {
	testCases := map[string]string{
		"empty":                 "",
		"malformed":             `{`,
		"trailing token":        previewHTTPTestBody + ` {}`,
		"unknown field":         strings.Replace(previewHTTPTestBody, `"constraints":[]`, `"constraints":[],"extra":true`, 1),
		"duplicate field":       strings.Replace(previewHTTPTestBody, `"goal":"制作品牌短片"`, `"goal":"制作品牌短片","goal":"另一目标"`, 1),
		"audience null":         strings.Replace(previewHTTPTestBody, `"audience":""`, `"audience":null`, 1),
		"constraints null":      strings.Replace(previewHTTPTestBody, `"constraints":[]`, `"constraints":null`, 1),
		"body over byte budget": strings.Repeat("x", previewMaxBodyBytes+1),
	}
	for name, body := range testCases {
		t.Run(name, func(t *testing.T) {
			verifier := newPreviewHTTPVerifier()
			service := &previewHTTPService{}
			router := newPreviewHTTPRouter(t, verifier, service)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, newPreviewHTTPRequest(http.MethodPost, previewHTTPTestPath, body))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			if verifier.calls != 1 || service.calls != 0 {
				t.Fatalf("Body 校验顺序错误: verifier=%d service=%d", verifier.calls, service.calls)
			}
			assertPreviewHTTPError(t, recorder, errorCodeInvalidArgument, false)
		})
	}
}

// TestCreationSpecPreviewPOSTMapsIdentityAndServiceErrors 验证错误状态、稳定 code 与 retryable 不泄漏内部详情。
func TestCreationSpecPreviewPOSTMapsIdentityAndServiceErrors(t *testing.T) {
	testCases := []struct {
		name          string
		verifyErr     error
		serviceErr    error
		serviceResult previewruntime.EnqueueResult
		wantStatus    int
		wantCode      string
		wantRetry     bool
	}{
		{name: "identity invalid", verifyErr: httpidentity.ErrInvalid, wantStatus: http.StatusUnauthorized, wantCode: errorCodeInternalIdentityInvalid},
		{name: "identity unavailable", verifyErr: httpidentity.ErrUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeIdentityAssertionUnavailable, wantRetry: true},
		{name: "service invalid input", serviceErr: previewruntime.ErrInvalidInput, wantStatus: http.StatusBadRequest, wantCode: errorCodeInvalidArgument},
		{name: "service not found", serviceErr: previewruntime.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: errorCodeSessionNotFound},
		{name: "service idempotency conflict", serviceErr: previewruntime.ErrIdempotencyConflict, wantStatus: http.StatusConflict, wantCode: errorCodeIdempotencyConflict},
		{name: "session lane blocked", serviceErr: previewruntime.ErrSessionLaneBlocked, wantStatus: http.StatusConflict, wantCode: errorCodeSessionLaneBlocked},
		{name: "service persistence", serviceErr: previewruntime.ErrPersistence, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodePersistenceUnavailable, wantRetry: true},
		{name: "unknown service error", serviceErr: errors.New("SQL detail must not escape"), wantStatus: http.StatusServiceUnavailable, wantCode: errorCodePersistenceUnavailable, wantRetry: true},
		{name: "invalid service result", serviceResult: previewruntime.EnqueueResult{SessionID: previewHTTPTestSessionID, InputID: "", Status: "pending"}, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodePersistenceUnavailable, wantRetry: true},
	}
	for _, fixture := range testCases {
		t.Run(fixture.name, func(t *testing.T) {
			verifier := newPreviewHTTPVerifier()
			verifier.err = fixture.verifyErr
			service := &previewHTTPService{err: fixture.serviceErr, result: fixture.serviceResult}
			router := newPreviewHTTPRouter(t, verifier, service)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, newPreviewHTTPRequest(http.MethodPost, previewHTTPTestPath, previewHTTPTestBody))
			if recorder.Code != fixture.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, fixture.wantStatus, recorder.Body.String())
			}
			assertPreviewHTTPError(t, recorder, fixture.wantCode, fixture.wantRetry)
			if strings.Contains(recorder.Body.String(), "SQL detail") {
				t.Fatalf("内部错误详情泄漏: %s", recorder.Body.String())
			}
		})
	}
}

// TestCreationSpecPreviewRouteIsAbsentWhenServerFlagIsOff 验证关闭态不注册写 Handler，并以 404 PREVIEW_DISABLED 失败关闭。
func TestCreationSpecPreviewRouteIsAbsentWhenServerFlagIsOff(t *testing.T) {
	verifier := newPreviewHTTPVerifier()
	limiter, err := workspace.NewStreamLimiter(4, 2, 1)
	if err != nil {
		t.Fatalf("创建 Workspace 限流器失败: %v", err)
	}
	workspaceHandler, err := NewWorkspaceHandler(
		verifier, previewFlagOffWorkspaceService{}, limiter,
		config.SSEConfig{
			BatchSize: 10, PollInterval: time.Second, HeartbeatInterval: 2 * time.Second,
			MaxConnectionDuration: 3 * time.Second, FrameWriteTimeout: time.Second, MaxEventBytes: 1024,
		},
		previewHTTPIDs{}, previewHTTPClock{},
	)
	if err != nil {
		t.Fatalf("创建 Workspace Handler 失败: %v", err)
	}
	toolHandler, err := NewToolCatalogHandler(verifier, agenttool.NewCatalogProvider(), previewHTTPIDs{})
	if err != nil {
		t.Fatalf("创建 Tool Catalog Handler 失败: %v", err)
	}
	disabledIDs := &previewFlagOffIDs{}
	server, err := New(
		config.HTTPConfig{
			Address: ":0", HeaderTimeout: time.Second, ReadTimeout: time.Second,
			WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1024,
		},
		config.ServiceConfig{Name: "agent-test", Version: "test"}, health.NewState(), workspaceHandler, toolHandler, disabledIDs,
	)
	if err != nil {
		t.Fatalf("创建关闭 Preview 的 Server 失败: %v", err)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, newPreviewHTTPRequest(http.MethodPost, previewHTTPTestPath, previewHTTPTestBody))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("关闭态 status=%d want=404 body=%s", recorder.Code, recorder.Body.String())
	}
	assertPreviewHTTPError(t, recorder, errorCodePreviewDisabled, false)
	var first ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &first); err != nil {
		t.Fatalf("解码关闭态错误失败: %v", err)
	}
	secondRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondRecorder, newPreviewHTTPRequest(http.MethodPost, previewHTTPTestPath, previewHTTPTestBody))
	var second ErrorResponse
	if err := json.Unmarshal(secondRecorder.Body.Bytes(), &second); err != nil {
		t.Fatalf("解码第二个关闭态错误失败: %v", err)
	}
	if first.Error.RequestID == second.Error.RequestID || first.Error.RequestID != previewHTTPTestErrorID ||
		second.Error.RequestID != previewHTTPTestErrorID2 {
		t.Fatalf("关闭态 request_id 未逐请求新生成: first=%q second=%q", first.Error.RequestID, second.Error.RequestID)
	}
	for _, path := range []string{
		previewHTTPTestPath + "?debug=1",
		strings.Replace(previewHTTPTestPath, "019f68e8", "019F68E8", 1),
		strings.Replace(previewHTTPTestPath, "/019f", "/%30%31%39f", 1),
		previewHTTPTestPath + "/extra",
		"/internal/v1/workspaces/sessions/not-a-uuid/creation-spec-previews",
	} {
		invalidRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(invalidRecorder, newPreviewHTTPRequest(http.MethodPost, path, previewHTTPTestBody))
		if invalidRecorder.Code != http.StatusNotFound || invalidRecorder.Body.Len() != 0 {
			t.Fatalf("非 canonical 关闭态路径应 plain 404: path=%q status=%d body=%q", path, invalidRecorder.Code, invalidRecorder.Body.String())
		}
	}
	if verifier.calls != 0 {
		t.Fatalf("关闭态路由仍消费身份断言: calls=%d", verifier.calls)
	}
}

type previewFlagOffWorkspaceService struct{}

func (previewFlagOffWorkspaceService) LoadSnapshot(context.Context, workspace.Identity, string) (workspace.Snapshot, error) {
	return workspace.Snapshot{}, workspace.ErrNotFound
}

func (previewFlagOffWorkspaceService) LoadEventBatch(context.Context, workspace.Identity, int64, int) (workspace.EventBatch, error) {
	return workspace.EventBatch{}, workspace.ErrNotFound
}

type previewHTTPClock struct{}

func (previewHTTPClock) Now() time.Time { return time.Now().UTC() }

func newPreviewHTTPVerifier() *previewHTTPVerifier {
	return &previewHTTPVerifier{claims: httpidentity.Claims{
		RequestID: previewHTTPTestRequestID, PrincipalUserID: previewHTTPTestUserID,
		ProjectID: previewHTTPTestProjectID, AgentSessionID: previewHTTPTestSessionID,
		Scope: httpidentity.ScopeCreationSpecPreviewWrite, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
}

func newPreviewHTTPRouter(t *testing.T, verifier *previewHTTPVerifier, service *previewHTTPService) http.Handler {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)
	handler, err := NewCreationSpecPreviewHandler(verifier, service, previewHTTPIDs{})
	if err != nil {
		t.Fatalf("创建 CreationSpec Preview Handler 失败: %v", err)
	}
	router := gin.New()
	handler.Register(router)
	return router
}

func newPreviewHTTPRequest(method, target, body string) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", previewHTTPTestIdempotencyKey)
	return request
}

func assertPreviewHTTPError(t *testing.T, recorder *httptest.ResponseRecorder, code string, retryable bool) {
	t.Helper()
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("错误响应缺少 no-store: headers=%v", recorder.Header())
	}
	var response ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析错误 Envelope 失败: %v body=%s", err, recorder.Body.String())
	}
	if response.Error.Code != code || response.Error.Retryable != retryable || response.Error.RequestID == "" || response.Error.Message == "" {
		t.Fatalf("错误 Envelope=%+v want code=%q retryable=%t", response, code, retryable)
	}
}

var _ IdentityVerifier = (*previewHTTPVerifier)(nil)
var _ CreationSpecPreviewService = (*previewHTTPService)(nil)
