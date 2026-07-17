package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/health"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/planstoryboardruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/tool"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
)

type serverTestVerifier struct{}

func (serverTestVerifier) Verify(context.Context, httpidentity.Request) (httpidentity.Claims, error) {
	return httpidentity.Claims{}, httpidentity.ErrInvalid
}

type serverTestWorkspaceService struct{}

type serverTestPlanStoryboardService struct{}

func (serverTestPlanStoryboardService) Enqueue(
	context.Context,
	planstoryboardruntime.EnqueueRequest,
) (planstoryboardruntime.EnqueueResponse, error) {
	return planstoryboardruntime.EnqueueResponse{}, planstoryboardruntime.ErrPersistence
}

func (serverTestWorkspaceService) LoadSnapshot(context.Context, workspace.Identity, string) (workspace.Snapshot, error) {
	return workspace.Snapshot{}, workspace.ErrNotFound
}

func (serverTestWorkspaceService) LoadEventBatch(context.Context, workspace.Identity, int64, int) (workspace.EventBatch, error) {
	return workspace.EventBatch{}, workspace.ErrNotFound
}

type serverTestIDs struct{}

func (serverTestIDs) New() (string, error) { return "019f0000-0000-7000-8000-000000000001", nil }

type serverTestClock struct{}

func (serverTestClock) Now() time.Time { return time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC) }

// TestReadinessTransitions 验证依赖完成前后 Readiness 的失败关闭行为。
func TestReadinessTransitions(t *testing.T) {
	state := health.NewState()
	limiter, err := workspace.NewStreamLimiter(10, 2, 1)
	if err != nil {
		t.Fatalf("创建测试流限流器失败: %v", err)
	}
	workspaceHandler, err := NewWorkspaceHandler(
		serverTestVerifier{}, serverTestWorkspaceService{}, limiter,
		config.SSEConfig{
			BatchSize: 10, PollInterval: time.Second, HeartbeatInterval: 2 * time.Second,
			MaxConnectionDuration: 3 * time.Second, FrameWriteTimeout: time.Second, MaxEventBytes: 1024,
		},
		serverTestIDs{}, serverTestClock{},
	)
	if err != nil {
		t.Fatalf("创建测试 Workspace Handler 失败: %v", err)
	}
	toolCatalogHandler, err := NewToolCatalogHandler(serverTestVerifier{}, tool.NewCatalogProvider(), serverTestIDs{})
	if err != nil {
		t.Fatalf("创建测试 Tool Catalog Handler 失败: %v", err)
	}
	server, err := New(config.HTTPConfig{
		Address: ":0", HeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1024,
	}, config.ServiceConfig{Name: "agent-test", Version: "test"}, state, workspaceHandler, toolCatalogHandler, serverTestIDs{})
	if err != nil {
		t.Fatalf("创建测试服务器失败: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("未就绪状态码错误: got %d", recorder.Code)
	}
	state.SetReady(true)
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("就绪状态码错误: got %d", recorder.Code)
	}
	catalogRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/agent/sessions/019f0000-0000-7000-8000-000000000005/tools", nil)
	catalogRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(catalogRecorder, catalogRequest)
	if catalogRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("Tool Catalog 路由未显式装配: status=%d body=%s", catalogRecorder.Code, catalogRecorder.Body.String())
	}
}

// TestPlanStoryboardPreviewServerRegistration 验证关闭态稳定失败和启用态显式注册使用同一 canonical 路径。
func TestPlanStoryboardPreviewServerRegistration(t *testing.T) {
	state := health.NewState()
	limiter, err := workspace.NewStreamLimiter(10, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	workspaceHandler, err := NewWorkspaceHandler(
		serverTestVerifier{}, serverTestWorkspaceService{}, limiter,
		config.SSEConfig{
			BatchSize: 10, PollInterval: time.Second, HeartbeatInterval: 2 * time.Second,
			MaxConnectionDuration: 3 * time.Second, FrameWriteTimeout: time.Second, MaxEventBytes: 1024,
		},
		serverTestIDs{}, serverTestClock{},
	)
	if err != nil {
		t.Fatal(err)
	}
	toolCatalogHandler, err := NewToolCatalogHandler(serverTestVerifier{}, tool.NewCatalogProvider(), serverTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	httpCfg := config.HTTPConfig{
		Address: ":0", HeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1024,
	}
	serviceCfg := config.ServiceConfig{Name: "agent-test", Version: "test"}
	path := "/internal/v1/workspaces/sessions/019f0000-0000-7000-8000-000000000005/plan-storyboard-previews"

	disabled, err := New(httpCfg, serviceCfg, state, workspaceHandler, toolCatalogHandler, serverTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	disabled.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"PREVIEW_DISABLED"`) {
		t.Fatalf("关闭态 Storyboard 路由未返回稳定错误: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	storyboardHandler, err := NewPlanStoryboardPreviewHandler(
		serverTestVerifier{}, serverTestPlanStoryboardService{}, serverTestIDs{}, "key-v1",
	)
	if err != nil {
		t.Fatal(err)
	}
	enabled, err := NewWithPlanStoryboard(
		httpCfg, serviceCfg, state, workspaceHandler, toolCatalogHandler, serverTestIDs{},
		nil, nil, storyboardHandler,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "019f0000-0000-7000-8000-000000000006")
	recorder = httptest.NewRecorder()
	enabled.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("启用态 Storyboard 路由未进入身份验证: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	trailing := httptest.NewRequest(http.MethodPost, path+"/", strings.NewReader(`{}`))
	trailing.Header.Set("Content-Type", "application/json")
	trailing.Header.Set("Idempotency-Key", "019f0000-0000-7000-8000-000000000006")
	recorder = httptest.NewRecorder()
	enabled.Handler().ServeHTTP(recorder, trailing)
	if recorder.Code != http.StatusNotFound || recorder.Header().Get("Location") != "" {
		t.Fatalf("尾斜线 Storyboard 路由未严格拒绝: status=%d location=%q", recorder.Code, recorder.Header().Get("Location"))
	}
}
