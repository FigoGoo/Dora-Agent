package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/gin-gonic/gin"
)

// captureHTTPTestLogs 临时捕获默认结构化日志并在测试结束恢复，供拒绝路径断言日志白名单。
func captureHTTPTestLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(original) })
	return &output
}

// authHandlerTestService 捕获 HTTP 协议对认证用例的调用并返回预置结果。
type authHandlerTestService struct {
	loginResult   auth.LoginResult
	loginErr      error
	resolveResult auth.ResolvedSession
	resolveErr    error
	logoutErr     error
	loginCalls    int
	resolveCalls  int
	logoutCalls   int
	loginEmail    string
	loginPassword string
	logoutCookie  string
	logoutCSRF    string
}

// Login 捕获邮箱密码并返回预置会话。
func (s *authHandlerTestService) Login(_ context.Context, email string, password string) (auth.LoginResult, error) {
	s.loginCalls++
	s.loginEmail = email
	s.loginPassword = password
	return s.loginResult, s.loginErr
}

// Resolve 捕获会话解析次数并返回预置 Principal。
func (s *authHandlerTestService) Resolve(_ context.Context, _ string) (auth.ResolvedSession, error) {
	s.resolveCalls++
	return s.resolveResult, s.resolveErr
}

// Logout 捕获 Cookie 与 CSRF Token，不记录到普通日志。
func (s *authHandlerTestService) Logout(_ context.Context, cookieToken string, csrfToken string) error {
	s.logoutCalls++
	s.logoutCookie = cookieToken
	s.logoutCSRF = csrfToken
	return s.logoutErr
}

// authHandlerTestIDs 为错误 Envelope 返回固定 UUIDv7。
type authHandlerTestIDs struct{}

// New 返回固定但格式合法的 UUIDv7。
func (authHandlerTestIDs) New() (string, error) {
	return "019f0000-0000-7000-8000-000000000099", nil
}

// newAuthHandlerTestRouter 创建仅包含 W0 Auth 路由的 Gin 测试 Router。
func newAuthHandlerTestRouter(t *testing.T, service *authHandlerTestService) (*AuthHandler, http.Handler) {
	t.Helper()
	gInMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(gInMode) })
	handler, err := NewAuthHandler(service, config.AuthConfig{
		SessionIdleTTL: 30 * time.Minute, SessionAbsoluteTTL: 24 * time.Hour,
		CookieName: "dora_session", CookieDomain: "example.com", CookieSecure: true, CookieSameSite: "strict",
		MaxRequestBodyBytes: 256,
	}, authHandlerTestIDs{})
	if err != nil {
		t.Fatalf("create auth handler: %v", err)
	}
	router := gin.New()
	handler.Register(router)
	return handler, router
}

// validAuthHandlerSession 构造严格 Frozen v1 响应所需的安全会话投影。
func validAuthHandlerSession() (auth.LoginResult, auth.ResolvedSession) {
	expiresAt := time.Date(2026, 7, 14, 8, 30, 0, 0, time.UTC)
	principal := auth.Principal{
		ID: "019f0000-0000-7000-8000-000000000011", DisplayName: "测试用户", Email: "u***@example.com",
		AccountStatus: "active", Roles: []string{}, Capabilities: []string{},
	}
	return auth.LoginResult{
			Principal: principal, CookieToken: "opaque-cookie-token", CSRFToken: "bound-csrf-token", SessionExpiresAt: expiresAt,
		}, auth.ResolvedSession{
			Principal: principal, WebSessionID: "019f0000-0000-7000-8000-000000000012", WebSessionVersion: 1,
			CSRFToken: "bound-csrf-token", SessionExpiresAt: expiresAt,
		}
}

func TestAuthLoginReturnsStrictDTOAndConfiguredCookie(t *testing.T) {
	login, resolved := validAuthHandlerSession()
	service := &authHandlerTestService{loginResult: login, resolveResult: resolved}
	_, router := newAuthHandlerTestRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", strings.NewReader(`{"email":"User@Example.com","password":"secret"}`))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || service.loginCalls != 1 || service.loginEmail != "User@Example.com" || service.loginPassword != "secret" {
		t.Fatalf("unexpected login result: status=%d service=%+v body=%s", recorder.Code, service, recorder.Body.String())
	}
	var response AuthSessionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if response.Status != "authenticated" || response.Principal.AccountStatus != "active" || response.CSRFToken != "bound-csrf-token" || response.Principal.Roles == nil || response.Principal.Capabilities == nil {
		t.Fatalf("unexpected strict login DTO: %+v", response)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "dora_session" || cookies[0].Value != "opaque-cookie-token" ||
		!cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" || cookies[0].Domain != "example.com" {
		t.Fatalf("unexpected configured session cookie: %+v", cookies)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("auth response must disable caching")
	}
}

func TestAuthLoginRejectsUnknownFieldsExtraJSONAndOversize(t *testing.T) {
	login, resolved := validAuthHandlerSession()
	for name, body := range map[string]string{
		"unknown field": `{"email":"user@example.com","password":"secret","role":"admin"}`,
		"extra JSON":    `{"email":"user@example.com","password":"secret"}{}`,
		"oversize":      `{"email":"user@example.com","password":"` + strings.Repeat("x", 300) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			service := &authHandlerTestService{loginResult: login, resolveResult: resolved}
			_, router := newAuthHandlerTestRouter(t, service)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest || service.loginCalls != 0 || !strings.Contains(recorder.Body.String(), `"code":"INVALID_REQUEST"`) {
				t.Fatalf("invalid JSON crossed handler boundary: status=%d calls=%d body=%s", recorder.Code, service.loginCalls, recorder.Body.String())
			}
		})
	}
}

func TestAuthLoginUsesUnifiedInvalidCredentialsEnvelope(t *testing.T) {
	service := &authHandlerTestService{loginErr: auth.ErrInvalidCredentials}
	_, router := newAuthHandlerTestRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", strings.NewReader(`{"email":"missing@example.com","password":"wrong"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized || !strings.Contains(recorder.Body.String(), `"code":"AUTH_INVALID_CREDENTIALS"`) || strings.Contains(recorder.Body.String(), "missing@example.com") {
		t.Fatalf("unexpected invalid credentials envelope: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAuthLoginMapsRateLimitToRetryable429(t *testing.T) {
	service := &authHandlerTestService{loginErr: auth.ErrRateLimited}
	_, router := newAuthHandlerTestRouter(t, service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", strings.NewReader(`{"email":"user@example.com","password":"candidate"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests ||
		!strings.Contains(recorder.Body.String(), `"code":"AUTH_RATE_LIMITED"`) ||
		!strings.Contains(recorder.Body.String(), `"retryable":true`) ||
		recorder.Header().Get("Retry-After") == "" {
		t.Fatalf("unexpected rate-limit envelope: status=%d retry-after=%q body=%s", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
	}
}

func TestAuthGetDistinguishesUnauthenticatedFromUnavailable(t *testing.T) {
	t.Run("missing cookie", func(t *testing.T) {
		service := &authHandlerTestService{}
		_, router := newAuthHandlerTestRouter(t, service)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil))
		if recorder.Code != http.StatusUnauthorized || service.resolveCalls != 0 || !strings.Contains(recorder.Body.String(), `"code":"UNAUTHENTICATED"`) {
			t.Fatalf("unexpected missing cookie response: %d %s", recorder.Code, recorder.Body.String())
		}
	})
	t.Run("dependency unavailable", func(t *testing.T) {
		service := &authHandlerTestService{resolveErr: errors.New("postgres password=secret SQL=SELECT")}
		_, router := newAuthHandlerTestRouter(t, service)
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
		request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque"})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"AUTH_UNAVAILABLE"`) || strings.Contains(recorder.Body.String(), "secret") || strings.Contains(recorder.Body.String(), "SELECT") {
			t.Fatalf("unexpected unavailable response: %d %s", recorder.Code, recorder.Body.String())
		}
	})
}

func TestAuthLogoutRequiresCSRFAndIsNoCookieIdempotent(t *testing.T) {
	t.Run("invalid csrf", func(t *testing.T) {
		service := &authHandlerTestService{logoutErr: auth.ErrInvalidCSRF}
		_, router := newAuthHandlerTestRouter(t, service)
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/session", nil)
		request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque"})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"CSRF_INVALID"`) || recorder.Header().Get("Set-Cookie") != "" {
			t.Fatalf("invalid CSRF unexpectedly cleared cookie: %d %s header=%s", recorder.Code, recorder.Body.String(), recorder.Header().Get("Set-Cookie"))
		}
	})
	t.Run("missing cookie replay", func(t *testing.T) {
		service := &authHandlerTestService{}
		_, router := newAuthHandlerTestRouter(t, service)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/v1/auth/session", nil))
		if recorder.Code != http.StatusNoContent || service.logoutCalls != 0 || !strings.Contains(recorder.Header().Get("Set-Cookie"), "Max-Age=0") {
			t.Fatalf("missing-cookie logout was not idempotent: %d header=%s", recorder.Code, recorder.Header().Get("Set-Cookie"))
		}
	})
}

func TestAuthProtectionMiddlewareResolvesOnceAndWritesPrivatePrincipal(t *testing.T) {
	_, resolved := validAuthHandlerSession()
	service := &authHandlerTestService{resolveResult: resolved}
	handler, _ := newAuthHandlerTestRouter(t, service)
	router := gin.New()
	router.POST("/protected", handler.RequireSessionAndCSRF(), func(c *gin.Context) {
		principal, ok := auth.PrincipalFromContext(c.Request.Context())
		internalSession, sessionOK := auth.ResolvedSessionFromContext(c.Request.Context())
		if !ok || !sessionOK || internalSession.WebSessionID != resolved.WebSessionID || internalSession.WebSessionVersion != resolved.WebSessionVersion {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.String(http.StatusOK, principal.ID)
	})
	request := httptest.NewRequest(http.MethodPost, "/protected", nil)
	request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque"})
	request.Header.Set("X-CSRF-Token", "bound-csrf-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || service.resolveCalls != 1 || recorder.Body.String() != resolved.Principal.ID {
		t.Fatalf("unexpected protected route result: status=%d resolves=%d body=%s", recorder.Code, service.resolveCalls, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/protected", nil)
	request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque"})
	request.Header.Set("X-CSRF-Token", "wrong")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || service.resolveCalls != 2 || !strings.Contains(recorder.Body.String(), `"code":"CSRF_INVALID"`) {
		t.Fatalf("invalid middleware CSRF reached route: status=%d resolves=%d body=%s", recorder.Code, service.resolveCalls, recorder.Body.String())
	}
}

func TestAuthProtectionMiddlewareAuditsDenialsWithoutSensitiveValues(t *testing.T) {
	_, resolved := validAuthHandlerSession()
	service := &authHandlerTestService{resolveResult: resolved}
	handler, _ := newAuthHandlerTestRouter(t, service)
	router := gin.New()
	router.POST("/protected", handler.RequireSessionAndCSRF(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	logs := captureHTTPTestLogs(t)

	missingCookie := httptest.NewRecorder()
	router.ServeHTTP(missingCookie, httptest.NewRequest(http.MethodPost, "/protected", nil))
	if missingCookie.Code != http.StatusUnauthorized || !strings.Contains(logs.String(), `"route":"/protected"`) ||
		!strings.Contains(logs.String(), `"action":"auth.require_session"`) || !strings.Contains(logs.String(), `"error_code":"UNAUTHENTICATED"`) {
		t.Fatalf("missing-cookie denial was not safely audited: status=%d logs=%s", missingCookie.Code, logs.String())
	}

	logs.Reset()
	service.resolveErr = errors.New("postgres password=resolver-secret SQL=SELECT")
	unavailableRequest := httptest.NewRequest(http.MethodPost, "/protected", nil)
	unavailableRequest.AddCookie(&http.Cookie{Name: "dora_session", Value: "sensitive-cookie"})
	unavailable := httptest.NewRecorder()
	router.ServeHTTP(unavailable, unavailableRequest)
	if unavailable.Code != http.StatusServiceUnavailable || !strings.Contains(logs.String(), `"error_code":"AUTH_UNAVAILABLE"`) ||
		strings.Contains(logs.String(), "resolver-secret") || strings.Contains(logs.String(), "SELECT") || strings.Contains(logs.String(), "sensitive-cookie") {
		t.Fatalf("resolver denial audit leaked sensitive details: status=%d logs=%s", unavailable.Code, logs.String())
	}

	logs.Reset()
	service.resolveErr = nil
	csrfRequest := httptest.NewRequest(http.MethodPost, "/protected", nil)
	csrfRequest.AddCookie(&http.Cookie{Name: "dora_session", Value: "sensitive-cookie"})
	csrfRequest.Header.Set("X-CSRF-Token", "sensitive-wrong-csrf")
	csrfDenied := httptest.NewRecorder()
	router.ServeHTTP(csrfDenied, csrfRequest)
	if csrfDenied.Code != http.StatusForbidden || !strings.Contains(logs.String(), `"error_code":"CSRF_INVALID"`) ||
		!strings.Contains(logs.String(), `"actor_id":"`+resolved.Principal.ID+`"`) || strings.Contains(logs.String(), "sensitive-wrong-csrf") ||
		strings.Contains(logs.String(), "bound-csrf-token") || strings.Contains(logs.String(), "sensitive-cookie") {
		t.Fatalf("CSRF denial was not safely audited: status=%d logs=%s", csrfDenied.Code, logs.String())
	}
}
