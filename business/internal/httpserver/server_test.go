package httpserver

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/health"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// TestReadinessTransitions 验证未就绪实例失败关闭，完成依赖检查后才返回成功。
func TestReadinessTransitions(t *testing.T) {
	state := health.NewState()
	server, err := New(config.HTTPConfig{
		Address:        ":0",
		HeaderTimeout:  time.Second,
		ReadTimeout:    time.Second,
		WriteTimeout:   time.Second,
		IdleTimeout:    time.Second,
		MaxHeaderBytes: 1024,
	}, config.ServiceConfig{Name: "business-test", Version: "test"}, state)
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
}

func TestSafeRecoveryReturnsEnvelopeWithoutDumpingRequest(t *testing.T) {
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(safeRecovery())
	router.GET("/panic", func(*gin.Context) { panic("cookie=dora-secret csrf=secret") })
	request := httptest.NewRequest(http.MethodGet, "/panic?token=must-not-log", nil)
	request.Header.Set("Cookie", "dora_session=must-not-log")
	request.Header.Set("X-CSRF-Token", "must-not-log")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("recovery status = %d", recorder.Code)
	}
	var response ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode recovery response: %v", err)
	}
	if response.Error.Code != "INTERNAL_ERROR" || response.Error.RequestID != authEmergencyRequestID || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unsafe recovery envelope: %+v headers=%v", response, recorder.Header())
	}
}

func TestServerRegistersProjectRouteBehindSessionAndCSRF(t *testing.T) {
	_, resolved := validAuthHandlerSession()
	authService := &authHandlerTestService{resolveResult: resolved}
	authHandler, err := NewAuthHandler(authService, config.AuthConfig{
		CookieName: "dora_session", CookieSameSite: "lax", MaxRequestBodyBytes: 4096,
	}, authHandlerTestIDs{})
	if err != nil {
		t.Fatalf("NewAuthHandler() error = %v", err)
	}
	projectID, _ := uuid.NewV7()
	projectService := &projectHTTPService{quickResult: project.QuickCreateResult{ProjectID: projectID.String()}}
	projectHandler, err := NewProjectHandler(projectService, authHandlerTestIDs{}, project.MaxInitialPromptBytes+1024)
	if err != nil {
		t.Fatalf("NewProjectHandler() error = %v", err)
	}
	state := health.NewState()
	server, err := New(config.HTTPConfig{
		Address: ":0", HeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1024,
	}, config.ServiceConfig{Name: "business-test", Version: "test"}, state, RouteHandlers{
		Auth: authHandler, Project: projectHandler,
		Agent: mustAgentProxyHandlerForServerTest(t), Skill: mustSkillHandlerForServerTest(t),
		SkillReview: mustSkillReviewHandlerForServerTest(t),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/projects:quick-create", strings.NewReader(`{"initial_prompt":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "intent-server-route-1")
	request.Header.Set("X-CSRF-Token", resolved.CSRFToken)
	request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque-cookie-token"})
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("protected Quick Create status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if authService.resolveCalls != 1 || projectService.command.OwnerUserID != resolved.Principal.ID {
		t.Fatalf("route did not use one trusted session resolution: resolve_calls=%d command=%+v", authService.resolveCalls, projectService.command)
	}
}

func mustAgentProxyHandlerForServerTest(t *testing.T) *AgentProxyHandler {
	t.Helper()
	handler, _, _ := newAgentProxyHandlerForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"messages":[],"inputs":[]}`)),
		}, nil
	}))
	return handler
}

func mustSkillReviewHandlerForServerTest(t *testing.T) *SkillReviewHandler {
	t.Helper()
	handler, err := NewSkillReviewHandler(&skillReviewHTTPService{}, authHandlerTestIDs{})
	if err != nil {
		t.Fatalf("NewSkillReviewHandler() error = %v", err)
	}
	return handler
}

func TestServerRegistersAgentProxyBehindBusinessSession(t *testing.T) {
	_, resolved := validAuthHandlerSession()
	authService := &authHandlerTestService{resolveResult: resolved}
	authHandler, err := NewAuthHandler(authService, config.AuthConfig{
		CookieName: "dora_session", CookieSameSite: "lax", MaxRequestBodyBytes: 4096,
	}, authHandlerTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	projectHandler, err := NewProjectHandler(&projectHTTPService{}, authHandlerTestIDs{}, project.MaxInitialPromptBytes+1024)
	if err != nil {
		t.Fatal(err)
	}
	state := health.NewState()
	server, err := New(config.HTTPConfig{
		Address: ":0", HeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1024,
	}, config.ServiceConfig{Name: "business-test", Version: "test"}, state, RouteHandlers{
		Auth: authHandler, Project: projectHandler, Agent: mustAgentProxyHandlerForServerTest(t),
		Skill: mustSkillHandlerForServerTest(t), SkillReview: mustSkillReviewHandlerForServerTest(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	for index, resource := range []string{"workspace", "tools"} {
		path := "/api/v1/agent/sessions/" + agentProxySessionID + "/" + resource
		unauthenticated := httptest.NewRecorder()
		server.Handler().ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, path, nil))
		if unauthenticated.Code != http.StatusUnauthorized || !strings.Contains(unauthenticated.Body.String(), `"code":"UNAUTHENTICATED"`) {
			t.Fatalf("anonymous Agent %s proxy status=%d body=%s", resource, unauthenticated.Code, unauthenticated.Body.String())
		}
		authenticatedRequest := httptest.NewRequest(http.MethodGet, path, nil)
		authenticatedRequest.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque-cookie-token"})
		authenticated := httptest.NewRecorder()
		server.Handler().ServeHTTP(authenticated, authenticatedRequest)
		if authenticated.Code != http.StatusOK || authService.resolveCalls != index+1 {
			t.Fatalf("authenticated Agent %s proxy status=%d resolves=%d body=%s", resource, authenticated.Code, authService.resolveCalls, authenticated.Body.String())
		}
	}
}

func TestServerRegistersSkillWriteBehindSessionAndCSRF(t *testing.T) {
	_, resolved := validAuthHandlerSession()
	authService := &authHandlerTestService{resolveResult: resolved}
	authHandler, err := NewAuthHandler(authService, config.AuthConfig{
		CookieName: "dora_session", CookieSameSite: "lax", MaxRequestBodyBytes: 4096,
	}, authHandlerTestIDs{})
	if err != nil {
		t.Fatal(err)
	}
	projectHandler, err := NewProjectHandler(&projectHTTPService{}, authHandlerTestIDs{}, project.MaxInitialPromptBytes+1024)
	if err != nil {
		t.Fatal(err)
	}
	skillService := &skillHTTPService{}
	skillHandler, err := NewSkillHandler(skillService, authHandlerTestIDs{}, 256*1024)
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(config.HTTPConfig{
		Address: ":0", HeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1024,
	}, config.ServiceConfig{Name: "business-test", Version: "test"}, health.NewState(), RouteHandlers{
		Auth: authHandler, Project: projectHandler, Agent: mustAgentProxyHandlerForServerTest(t),
		Skill: skillHandler, SkillReview: mustSkillReviewHandlerForServerTest(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	body := definitionRequestJSON(t, validSkillHTTPDefinition())
	withoutCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/skills", strings.NewReader(body))
	withoutCSRF.Header.Set("Content-Type", "application/json")
	withoutCSRF.Header.Set("Idempotency-Key", "server-skill-1")
	withoutCSRF.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque-cookie-token"})
	denied := httptest.NewRecorder()
	server.Handler().ServeHTTP(denied, withoutCSRF)
	if denied.Code != http.StatusForbidden || skillService.createCommand.OwnerUserID != "" {
		t.Fatalf("Skill write bypassed CSRF: status=%d command=%+v body=%s", denied.Code, skillService.createCommand, denied.Body.String())
	}

	withCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/skills", strings.NewReader(body))
	withCSRF.Header.Set("Content-Type", "application/json")
	withCSRF.Header.Set("Idempotency-Key", "server-skill-1")
	withCSRF.Header.Set("X-CSRF-Token", resolved.CSRFToken)
	withCSRF.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque-cookie-token"})
	accepted := httptest.NewRecorder()
	server.Handler().ServeHTTP(accepted, withCSRF)
	if accepted.Code != http.StatusCreated || skillService.createCommand.OwnerUserID != resolved.Principal.ID {
		t.Fatalf("Skill write did not use trusted session: status=%d command=%+v body=%s", accepted.Code, skillService.createCommand, accepted.Body.String())
	}
}
