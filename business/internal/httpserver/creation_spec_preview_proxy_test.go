package httpserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/health"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/gin-gonic/gin"
)

const previewIdempotencyKey = "019f0000-0000-7000-8000-000000000007"
const previewInputID = "019f0000-0000-7000-8000-000000000008"

func newCreationSpecPreviewProxyForTest(t *testing.T, client AgentHTTPClient, enabled bool, bodyLimit int64) (*AgentProxyHandler, *agentProxyAccessStub, *agentProxySignerStub) {
	t.Helper()
	access := &agentProxyAccessStub{result: project.AgentSessionAccess{ProjectID: agentProxyProjectID, AgentSessionID: agentProxySessionID}}
	signer := &agentProxySignerStub{}
	handler, err := NewAgentProxyHandler(access, signer, agentProxyIDs{}, client, config.AgentHTTPConfig{
		BaseURL: "http://agent.internal", RequestTimeout: time.Second,
		PlanSpecPreviewEnabled: enabled, PreviewMaxRequestBodyBytes: bodyLimit,
	})
	if err != nil {
		t.Fatalf("NewAgentProxyHandler() error = %v", err)
	}
	return handler, access, signer
}

func serveCreationSpecPreviewProxy(handler *AgentProxyHandler, request *http.Request) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Header("X-Test-Route", "preview")
		c.Next()
	})
	handler.Register(router, agentProxyResolvedMiddleware(), agentProxyResolvedMiddleware())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func validCreationSpecPreviewBody() string {
	return `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"制作夏日短片","deliverable_type":"video","audience":"年轻消费者","locale":"zh-CN","constraints":["竖屏 9:16"]}`
}

func validCreationSpecPreviewRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/sessions/"+agentProxySessionID+"/creation-spec-previews", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", previewIdempotencyKey)
	request.Header.Set("Cookie", "dora_session=browser-secret")
	request.Header.Set("X-CSRF-Token", "browser-secret")
	request.Header.Set(agentidentity.HeaderAssertion, "browser-forged")
	return request
}

func previewAcceptedResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusAccepted,
		Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(`{"schema_version":"plan_creation_spec.preview.enqueue.v1","request_id":"` + agentProxyRequestID + `","session_id":"` + agentProxySessionID + `","input_id":"` + previewInputID + `","status":"pending"}`)),
	}
}

// TestCreationSpecPreviewProxyReturnsExact202AndPOSTIdentity 验证 BFF 只转发规范 Intent/幂等键并签入内部 POST Target 与最小 Scope。
func TestCreationSpecPreviewProxyReturnsExact202AndPOSTIdentity(t *testing.T) {
	var upstream *http.Request
	var upstreamBody string
	client := agentProxyClientFunc(func(request *http.Request) (*http.Response, error) {
		upstream = request
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		upstreamBody = string(body)
		if _, ok := request.Context().Deadline(); !ok {
			t.Fatal("preview POST has no bounded deadline")
		}
		return previewAcceptedResponse(), nil
	})
	handler, access, signer := newCreationSpecPreviewProxyForTest(t, client, true, 4096)
	recorder := serveCreationSpecPreviewProxy(handler, validCreationSpecPreviewRequest(validCreationSpecPreviewBody()))
	if recorder.Code != http.StatusAccepted || recorder.Header().Get("Cache-Control") != "no-store" ||
		recorder.Body.String() != `{"schema_version":"plan_creation_spec.preview.enqueue.v1","request_id":"`+agentProxyRequestID+`","session_id":"`+agentProxySessionID+`","input_id":"`+previewInputID+`","status":"pending"}` {
		t.Fatalf("preview response status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	internalTarget := "/internal/v1/workspaces/sessions/" + agentProxySessionID + "/creation-spec-previews"
	if upstream == nil || upstream.Method != http.MethodPost || upstream.URL.String() != "http://agent.internal"+internalTarget ||
		upstream.Header.Get("Idempotency-Key") != previewIdempotencyKey || upstream.Header.Get("Content-Type") != "application/json" ||
		upstream.Header.Get("Cookie") != "" || upstream.Header.Get("X-CSRF-Token") != "" ||
		upstream.Header.Get(agentidentity.HeaderAssertion) != agentProxyAssertion || len(upstream.Header) != 6 {
		t.Fatalf("unsafe preview upstream request=%v headers=%v", upstream, upstream.Header)
	}
	wantBody := `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"制作夏日短片","deliverable_type":"video","audience":"年轻消费者","locale":"zh-CN","constraints":["竖屏 9:16"]}`
	if upstreamBody != wantBody {
		t.Fatalf("canonical upstream body=%s, want %s", upstreamBody, wantBody)
	}
	if signer.identity.Method != http.MethodPost || signer.identity.CanonicalTarget != internalTarget ||
		signer.identity.Scope != agentidentity.ScopeCreationSpecPreviewWrite || signer.identity.PrincipalUserID != agentProxyUserID ||
		signer.identity.ProjectID != agentProxyProjectID || access.userID != agentProxyUserID || access.sessionID != agentProxySessionID {
		t.Fatalf("preview owner/identity mismatch: access=%+v identity=%+v", access, signer.identity)
	}
}

// TestCreationSpecPreviewProxyRejectsStrictBodySizeAndIdempotency 覆盖未知/重复/null/非 NFC/尾随/超界与幂等键门禁。
func TestCreationSpecPreviewProxyRejectsStrictBodySizeAndIdempotency(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		key          string
		contentTypes []string
	}{
		{name: "unknown field", body: `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"目标","deliverable_type":"video","locale":"zh-CN","constraints":[],"extra":1}`, key: previewIdempotencyKey},
		{name: "duplicate field", body: `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"目标","goal":"覆盖","deliverable_type":"video","locale":"zh-CN","constraints":[]}`, key: previewIdempotencyKey},
		{name: "explicit null", body: `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"目标","deliverable_type":"video","audience":null,"locale":"zh-CN","constraints":[]}`, key: previewIdempotencyKey},
		{name: "non NFC", body: `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"é","deliverable_type":"video","locale":"zh-CN","constraints":[]}`, key: previewIdempotencyKey},
		{name: "duplicate constraints", body: `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"目标","deliverable_type":"video","locale":"zh-CN","constraints":["相同","相同"]}`, key: previewIdempotencyKey},
		{name: "trailing token", body: validCreationSpecPreviewBody() + `{}`, key: previewIdempotencyKey},
		{name: "invalid idempotency", body: validCreationSpecPreviewBody(), key: "not-uuid-v7"},
		{name: "duplicate content type", body: validCreationSpecPreviewBody(), key: previewIdempotencyKey, contentTypes: []string{"application/json", "application/json"}},
		{name: "oversized", body: `{"schema_version":"plan_creation_spec.preview.intent.v1","goal":"` + strings.Repeat("x", 1100) + `","deliverable_type":"video","locale":"zh-CN","constraints":[]}`, key: previewIdempotencyKey},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clientCalls := 0
			handler, access, signer := newCreationSpecPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
				clientCalls++
				return previewAcceptedResponse(), nil
			}), true, 1024)
			request := validCreationSpecPreviewRequest(test.body)
			request.Header.Del("Idempotency-Key")
			request.Header.Add("Idempotency-Key", test.key)
			if test.contentTypes != nil {
				request.Header.Del("Content-Type")
				for _, value := range test.contentTypes {
					request.Header.Add("Content-Type", value)
				}
			}
			recorder := serveCreationSpecPreviewProxy(handler, request)
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"retryable":false`) ||
				clientCalls != 0 || access.sessionID != "" || signer.identity.RequestID != "" {
				t.Fatalf("invalid preview reached protected flow: status=%d calls=%d access=%+v identity=%+v body=%s",
					recorder.Code, clientCalls, access, signer.identity, recorder.Body.String())
			}
		})
	}
}

// TestCreationSpecPreviewProxyAvailabilityAndUpstreamErrors 验证本地默认关闭及 Agent 非 202 公共错误白名单。
func TestCreationSpecPreviewProxyAvailabilityAndUpstreamErrors(t *testing.T) {
	clientCalls := 0
	disabled, _, _ := newCreationSpecPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		clientCalls++
		return previewAcceptedResponse(), nil
	}), false, 4096)
	disabledResponse := serveCreationSpecPreviewProxy(disabled, validCreationSpecPreviewRequest(validCreationSpecPreviewBody()))
	if disabledResponse.Code != http.StatusNotFound || !strings.Contains(disabledResponse.Body.String(), `"code":"PREVIEW_DISABLED"`) || clientCalls != 0 {
		t.Fatalf("disabled preview status=%d calls=%d body=%s", disabledResponse.Code, clientCalls, disabledResponse.Body.String())
	}

	tests := []struct {
		name           string
		upstreamStatus int
		upstreamCode   string
		wantStatus     int
		wantCode       string
	}{
		{name: "invalid", upstreamStatus: 400, upstreamCode: "INVALID_ARGUMENT", wantStatus: 400, wantCode: "INVALID_ARGUMENT"},
		{name: "identity", upstreamStatus: 401, upstreamCode: "INTERNAL_IDENTITY_INVALID", wantStatus: 503, wantCode: "DEPENDENCY_UNAVAILABLE"},
		{name: "session", upstreamStatus: 404, upstreamCode: "SESSION_NOT_FOUND", wantStatus: 404, wantCode: "SESSION_NOT_FOUND"},
		{name: "disabled", upstreamStatus: 404, upstreamCode: "PREVIEW_DISABLED", wantStatus: 404, wantCode: "PREVIEW_DISABLED"},
		{name: "conflict", upstreamStatus: 409, upstreamCode: "IDEMPOTENCY_CONFLICT", wantStatus: 409, wantCode: "IDEMPOTENCY_CONFLICT"},
		{name: "session lane blocked", upstreamStatus: 409, upstreamCode: "SESSION_LANE_BLOCKED", wantStatus: 409, wantCode: "SESSION_LANE_BLOCKED"},
		{name: "persistence", upstreamStatus: 503, upstreamCode: "PERSISTENCE_UNAVAILABLE", wantStatus: 503, wantCode: "PERSISTENCE_UNAVAILABLE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler, _, _ := newCreationSpecPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: test.upstreamStatus, Header: http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"error":{"code":"` + test.upstreamCode + `","message":"hidden","request_id":"` + agentProxyRequestID + `","retryable":false,"details":{}}}`)),
				}, nil
			}), true, 4096)
			recorder := serveCreationSpecPreviewProxy(handler, validCreationSpecPreviewRequest(validCreationSpecPreviewBody()))
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), `"code":"`+test.wantCode+`"`) || strings.Contains(recorder.Body.String(), "hidden") {
				t.Fatalf("upstream mapping status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestServerRegistersCreationSpecPreviewBehindSessionAndCSRF 验证正式路由缺失 CSRF 时不会做 Owner 查询或调用 Agent。
func TestServerRegistersCreationSpecPreviewBehindSessionAndCSRF(t *testing.T) {
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
	clientCalls := 0
	agentHandler, access, signer := newCreationSpecPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		clientCalls++
		return previewAcceptedResponse(), nil
	}), true, 4096)
	server, err := New(config.HTTPConfig{
		Address: ":0", HeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1024,
	}, config.ServiceConfig{Name: "business-test", Version: "test"}, health.NewState(), RouteHandlers{
		Auth: authHandler, Project: projectHandler, Agent: agentHandler,
		Skill: mustSkillHandlerForServerTest(t), SkillReview: mustSkillReviewHandlerForServerTest(t),
		SkillGovernance: mustSkillGovernanceHandlerForServerTest(t), SkillMarket: mustSkillMarketHandlerForServerTest(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := validCreationSpecPreviewRequest(validCreationSpecPreviewBody())
	request.Header.Del("X-CSRF-Token")
	request.Header.Del("Cookie")
	request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque-cookie-token"})
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), `"code":"CSRF_INVALID"`) ||
		clientCalls != 0 || access.sessionID != "" || signer.identity.RequestID != "" {
		t.Fatalf("missing CSRF reached preview: status=%d calls=%d access=%+v identity=%+v body=%s",
			recorder.Code, clientCalls, access, signer.identity, recorder.Body.String())
	}
	request = validCreationSpecPreviewRequest(validCreationSpecPreviewBody())
	request.Header.Set("X-CSRF-Token", resolved.CSRFToken)
	request.Header.Del("Cookie")
	request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque-cookie-token"})
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || clientCalls != 1 || access.userID != resolved.Principal.ID || signer.identity.PrincipalUserID != resolved.Principal.ID {
		t.Fatalf("valid CSRF/owner status=%d calls=%d access=%+v identity=%+v body=%s",
			recorder.Code, clientCalls, access, signer.identity, recorder.Body.String())
	}
}
