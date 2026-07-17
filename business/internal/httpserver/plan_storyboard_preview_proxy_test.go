package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/health"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/gin-gonic/gin"
)

const (
	planStoryboardPreviewCreationSpecID = "019f0000-0000-7000-8000-000000000009"
	planStoryboardPreviewInputID        = "019f0000-0000-7000-8000-00000000000a"
	planStoryboardPreviewTurnID         = "019f0000-0000-7000-8000-00000000000b"
	planStoryboardPreviewRunID          = "019f0000-0000-7000-8000-00000000000c"
	planStoryboardPreviewToolCallID     = "019f0000-0000-7000-8000-00000000000d"
	planStoryboardPreviewDigest         = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// planStoryboardPreviewBinderStub 记录 BFF 交给当前 Workspace 绑定器的完整可信身份与 PresentedRef。
type planStoryboardPreviewBinderStub struct {
	request PlanStoryboardPreviewCreationSpecBindingRequest
	result  PlanStoryboardPreviewCreationSpecRef
	err     error
	calls   int
}

// BindCurrent 返回测试冻结的权威引用或稳定错误，不访问外部资源。
func (stub *planStoryboardPreviewBinderStub) BindCurrent(_ context.Context, request PlanStoryboardPreviewCreationSpecBindingRequest) (PlanStoryboardPreviewCreationSpecRef, error) {
	stub.calls++
	stub.request = request
	return stub.result, stub.err
}

// newPlanStoryboardPreviewProxyForTest 构造独立 Storyboard BFF，并保留 Owner、Binder 与签名观察点。
func newPlanStoryboardPreviewProxyForTest(
	t *testing.T,
	client AgentHTTPClient,
	binder *planStoryboardPreviewBinderStub,
	enabled bool,
	bodyLimit int64,
) (*PlanStoryboardPreviewProxy, *agentProxyAccessStub, *agentProxySignerStub) {
	t.Helper()
	access := &agentProxyAccessStub{result: project.AgentSessionAccess{ProjectID: agentProxyProjectID, AgentSessionID: agentProxySessionID}}
	signer := &agentProxySignerStub{}
	base, err := NewAgentProxyHandler(access, signer, agentProxyIDs{}, client, config.AgentHTTPConfig{
		BaseURL: "http://agent.internal", RequestTimeout: time.Second, PreviewMaxRequestBodyBytes: bodyLimit,
	})
	if err != nil {
		t.Fatalf("NewAgentProxyHandler() error = %v", err)
	}
	proxy, err := NewPlanStoryboardPreviewProxy(base, binder, enabled)
	if err != nil {
		t.Fatalf("NewPlanStoryboardPreviewProxy() error = %v", err)
	}
	return proxy, access, signer
}

// servePlanStoryboardPreviewProxy 注册独立 canonical 路由，并用等价 Session+CSRF 测试中间件执行请求。
func servePlanStoryboardPreviewProxy(proxy *PlanStoryboardPreviewProxy, request *http.Request, requireWrite gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := proxy.Register(router, requireWrite); err != nil {
		panic(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// planStoryboardPreviewWriteMiddleware 模拟正式 RequireSessionAndCSRF，并只在 CSRF 通过后写入认证上下文。
func planStoryboardPreviewWriteMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("X-CSRF-Token") != "valid-csrf" {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "CSRF_INVALID"}})
			c.Abort()
			return
		}
		resolved := auth.ResolvedSession{
			Principal: auth.Principal{ID: agentProxyUserID}, WebSessionID: agentProxyWebID,
			WebSessionVersion: 7, SessionExpiresAt: time.Now().Add(time.Hour),
		}
		ctx := auth.ContextWithResolvedSession(c.Request.Context(), resolved)
		c.Request = c.Request.WithContext(auth.ContextWithPrincipal(ctx, resolved.Principal))
		c.Next()
	}
}

// validPlanStoryboardPreviewRef 返回当前 Workspace Card 的合法精确引用。
func validPlanStoryboardPreviewRef() PlanStoryboardPreviewCreationSpecRef {
	return PlanStoryboardPreviewCreationSpecRef{
		ID: planStoryboardPreviewCreationSpecID, Version: 1, ContentDigest: planStoryboardPreviewDigest,
	}
}

// validPlanStoryboardPreviewBody 故意打乱浏览器字段顺序，用于证明 BFF 输出唯一 canonical 字节。
func validPlanStoryboardPreviewBody() string {
	return `{"tool_intent":{"target_duration_seconds":60,"planning_instruction":"规划三段式故事板","schema_version":"plan_storyboard.preview.intent.v1"},"creation_spec_ref":{"content_digest":"` + planStoryboardPreviewDigest + `","version":1,"id":"` + planStoryboardPreviewCreationSpecID + `"},"schema_version":"plan_storyboard.preview.enqueue-request.v1"}`
}

// validPlanStoryboardPreviewRequest 构造带 canonical 幂等键和 CSRF 的浏览器请求。
func validPlanStoryboardPreviewRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/sessions/"+agentProxySessionID+"/plan-storyboard-previews", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", previewIdempotencyKey)
	request.Header.Set("X-CSRF-Token", "valid-csrf")
	request.Header.Set("Cookie", "dora_session=browser-secret")
	request.Header.Set("Authorization", "Bearer browser-secret")
	request.Header.Set(agentidentity.HeaderAssertion, "browser-forged")
	return request
}

// planStoryboardPreviewAcceptedResponse 返回 Agent exact 202 pending DTO。
func planStoryboardPreviewAcceptedResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusAccepted,
		Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body: io.NopCloser(strings.NewReader(
			`{"schema_version":"plan_storyboard.preview.enqueue.v1","request_id":"` + agentProxyRequestID +
				`","session_id":"` + agentProxySessionID + `","input_id":"` + planStoryboardPreviewInputID +
				`","turn_id":"` + planStoryboardPreviewTurnID + `","run_id":"` + planStoryboardPreviewRunID +
				`","tool_call_id":"` + planStoryboardPreviewToolCallID + `","status":"pending","replayed":false}`,
		)),
	}
}

// TestPlanStoryboardPreviewProxyBindsCurrentRefAndReturnsExact202 验证 Owner/Project/Session/ref 联合绑定、专用 Scope 和 canonical 内部请求。
func TestPlanStoryboardPreviewProxyBindsCurrentRefAndReturnsExact202(t *testing.T) {
	binder := &planStoryboardPreviewBinderStub{result: validPlanStoryboardPreviewRef()}
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
			t.Fatal("Plan Storyboard Preview POST has no bounded deadline")
		}
		return planStoryboardPreviewAcceptedResponse(), nil
	})
	proxy, access, signer := newPlanStoryboardPreviewProxyForTest(t, client, binder, true, 4096)
	recorder := servePlanStoryboardPreviewProxy(proxy, validPlanStoryboardPreviewRequest(validPlanStoryboardPreviewBody()), planStoryboardPreviewWriteMiddleware())
	wantResponse := `{"schema_version":"plan_storyboard.preview.enqueue.v1","request_id":"` + agentProxyRequestID +
		`","session_id":"` + agentProxySessionID + `","input_id":"` + planStoryboardPreviewInputID +
		`","turn_id":"` + planStoryboardPreviewTurnID + `","run_id":"` + planStoryboardPreviewRunID +
		`","tool_call_id":"` + planStoryboardPreviewToolCallID + `","status":"pending","replayed":false}`
	if recorder.Code != http.StatusAccepted || recorder.Header().Get("Cache-Control") != "no-store" || recorder.Body.String() != wantResponse {
		t.Fatalf("storyboard response status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	internalTarget := "/internal/v1/workspaces/sessions/" + agentProxySessionID + "/plan-storyboard-previews"
	if upstream == nil || upstream.Method != http.MethodPost || upstream.URL.String() != "http://agent.internal"+internalTarget ||
		upstream.Header.Get("Idempotency-Key") != previewIdempotencyKey || upstream.Header.Get("Content-Type") != "application/json" ||
		upstream.Header.Get("Cookie") != "" || upstream.Header.Get("Authorization") != "" || upstream.Header.Get("X-CSRF-Token") != "" ||
		upstream.Header.Get(agentidentity.HeaderAssertion) != agentProxyAssertion || len(upstream.Header) != 6 {
		t.Fatalf("unsafe storyboard upstream=%v headers=%v", upstream, upstream.Header)
	}
	wantBody := `{"schema_version":"plan_storyboard.preview.enqueue-request.v1","creation_spec_ref":{"id":"` +
		planStoryboardPreviewCreationSpecID + `","version":1,"content_digest":"` + planStoryboardPreviewDigest +
		`"},"tool_intent":{"schema_version":"plan_storyboard.preview.intent.v1","planning_instruction":"规划三段式故事板","target_duration_seconds":60}}`
	if upstreamBody != wantBody {
		t.Fatalf("canonical upstream body=%s, want %s", upstreamBody, wantBody)
	}
	if access.userID != agentProxyUserID || access.sessionID != agentProxySessionID || binder.calls != 1 ||
		binder.request.UserID != agentProxyUserID || binder.request.ProjectID != agentProxyProjectID ||
		binder.request.AgentSessionID != agentProxySessionID || binder.request.PresentedRef != validPlanStoryboardPreviewRef() ||
		signer.identity.Method != http.MethodPost || signer.identity.CanonicalTarget != internalTarget ||
		signer.identity.Scope != agentidentity.ScopePlanStoryboardPreviewWrite || signer.identity.ProjectID != agentProxyProjectID {
		t.Fatalf("binding/identity mismatch access=%+v binding=%+v identity=%+v", access, binder.request, signer.identity)
	}
}

// TestPlanStoryboardPreviewProxyRejectsStrictBrowserBoundary 验证非法请求在 Owner、Binder、Signer 与 Agent 调用前失败关闭。
func TestPlanStoryboardPreviewProxyRejectsStrictBrowserBoundary(t *testing.T) {
	valid := validPlanStoryboardPreviewBody()
	tests := []struct {
		name         string
		body         string
		idempotency  []string
		contentTypes []string
		pathSuffix   string
	}{
		{name: "unknown outer", body: strings.Replace(valid, `"schema_version":"plan_storyboard.preview.enqueue-request.v1"`, `"schema_version":"plan_storyboard.preview.enqueue-request.v1","user_id":"`+agentProxyUserID+`"`, 1)},
		{name: "unknown ref", body: strings.Replace(valid, `"version":1`, `"version":1,"project_id":"`+agentProxyProjectID+`"`, 1)},
		{name: "unknown intent", body: strings.Replace(valid, `"target_duration_seconds":60`, `"target_duration_seconds":60,"creation_spec_id":"`+planStoryboardPreviewCreationSpecID+`"`, 1)},
		{name: "duplicate nested", body: strings.Replace(valid, `"planning_instruction":"规划三段式故事板"`, `"planning_instruction":"规划三段式故事板","planning_instruction":"覆盖"`, 1)},
		{name: "null ref", body: strings.Replace(valid, `"creation_spec_ref":{"content_digest":"`+planStoryboardPreviewDigest+`","version":1,"id":"`+planStoryboardPreviewCreationSpecID+`"}`, `"creation_spec_ref":null`, 1)},
		{name: "null duration", body: strings.Replace(valid, `"target_duration_seconds":60`, `"target_duration_seconds":null`, 1)},
		{name: "trailing", body: valid + `{}`},
		{name: "lone surrogate", body: strings.Replace(valid, "规划三段式故事板", `\ud800`, 1)},
		{name: "non NFC", body: strings.Replace(valid, "规划三段式故事板", "e\u0301", 1)},
		{name: "duration low", body: strings.Replace(valid, `"target_duration_seconds":60`, `"target_duration_seconds":4`, 1)},
		{name: "too many scalars", body: strings.Replace(valid, "规划三段式故事板", strings.Repeat("x", 1001), 1)},
		{name: "invalid ref", body: strings.Replace(valid, planStoryboardPreviewCreationSpecID, "not-uuid-v7", 1)},
		{name: "invalid idempotency", body: valid, idempotency: []string{"not-uuid-v7"}},
		{name: "duplicate idempotency", body: valid, idempotency: []string{previewIdempotencyKey, previewIdempotencyKey}},
		{name: "duplicate content type", body: valid, contentTypes: []string{"application/json", "application/json"}},
		{name: "query", body: valid, pathSuffix: "?ref=hidden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			binder := &planStoryboardPreviewBinderStub{result: validPlanStoryboardPreviewRef()}
			proxy, access, signer := newPlanStoryboardPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return planStoryboardPreviewAcceptedResponse(), nil
			}), binder, true, 1024)
			request := validPlanStoryboardPreviewRequest(test.body)
			if test.pathSuffix != "" {
				request = httptest.NewRequest(http.MethodPost, request.URL.Path+test.pathSuffix, strings.NewReader(test.body))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Idempotency-Key", previewIdempotencyKey)
				request.Header.Set("X-CSRF-Token", "valid-csrf")
			}
			if test.idempotency != nil {
				request.Header.Del("Idempotency-Key")
				for _, value := range test.idempotency {
					request.Header.Add("Idempotency-Key", value)
				}
			}
			if test.contentTypes != nil {
				request.Header.Del("Content-Type")
				for _, value := range test.contentTypes {
					request.Header.Add("Content-Type", value)
				}
			}
			recorder := servePlanStoryboardPreviewProxy(proxy, request, planStoryboardPreviewWriteMiddleware())
			if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"`) ||
				calls != 0 || access.sessionID != "" || binder.calls != 0 || signer.identity.RequestID != "" ||
				strings.Contains(recorder.Body.String(), planStoryboardPreviewDigest) || strings.Contains(recorder.Body.String(), planStoryboardPreviewCreationSpecID) {
				t.Fatalf("invalid request reached protected flow: status=%d calls=%d access=%+v binder=%d identity=%+v body=%s",
					recorder.Code, calls, access, binder.calls, signer.identity, recorder.Body.String())
			}
		})
	}
}

// TestPlanStoryboardPreviewProxyRequiresCSRFAndBindsOwnerBeforeDraft 验证缺失 CSRF 不进入 Handler，Session Owner 不存在时也不读取 Draft。
func TestPlanStoryboardPreviewProxyRequiresCSRFAndBindsOwnerBeforeDraft(t *testing.T) {
	binder := &planStoryboardPreviewBinderStub{result: validPlanStoryboardPreviewRef()}
	calls := 0
	proxy, access, signer := newPlanStoryboardPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return planStoryboardPreviewAcceptedResponse(), nil
	}), binder, true, 4096)
	request := validPlanStoryboardPreviewRequest(validPlanStoryboardPreviewBody())
	request.Header.Del("X-CSRF-Token")
	recorder := servePlanStoryboardPreviewProxy(proxy, request, planStoryboardPreviewWriteMiddleware())
	if recorder.Code != http.StatusForbidden || calls != 0 || access.sessionID != "" || binder.calls != 0 || signer.identity.RequestID != "" {
		t.Fatalf("missing CSRF reached handler: status=%d calls=%d access=%+v binder=%d identity=%+v", recorder.Code, calls, access, binder.calls, signer.identity)
	}

	access.err = project.ErrAgentSessionNotFound
	request = validPlanStoryboardPreviewRequest(validPlanStoryboardPreviewBody())
	recorder = servePlanStoryboardPreviewProxy(proxy, request, planStoryboardPreviewWriteMiddleware())
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"SESSION_NOT_FOUND"`) ||
		calls != 0 || binder.calls != 0 || signer.identity.RequestID != "" {
		t.Fatalf("missing owner reached Binder: status=%d calls=%d binder=%d identity=%+v body=%s",
			recorder.Code, calls, binder.calls, signer.identity, recorder.Body.String())
	}
}

// TestPlanStoryboardPreviewProxyMapsBindingFailuresWithoutReferenceLeak 验证 NotFound/Conflict/Unavailable 与权威引用漂移只输出公共错误。
func TestPlanStoryboardPreviewProxyMapsBindingFailuresWithoutReferenceLeak(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		result     PlanStoryboardPreviewCreationSpecRef
		wantStatus int
		wantCode   string
	}{
		{name: "not found", err: ErrPlanStoryboardPreviewCreationSpecNotFound, wantStatus: http.StatusNotFound, wantCode: "CREATION_SPEC_NOT_FOUND"},
		{name: "conflict", err: ErrPlanStoryboardPreviewCreationSpecConflict, wantStatus: http.StatusConflict, wantCode: "CREATION_SPEC_CONFLICT"},
		{name: "unavailable", err: fmtTestError("postgres hidden"), wantStatus: http.StatusServiceUnavailable, wantCode: "DEPENDENCY_UNAVAILABLE"},
		{name: "invalid authoritative ref", result: PlanStoryboardPreviewCreationSpecRef{}, wantStatus: http.StatusServiceUnavailable, wantCode: "DEPENDENCY_UNAVAILABLE"},
		{name: "authoritative mismatch", result: PlanStoryboardPreviewCreationSpecRef{ID: agentProxyOtherID, Version: 1, ContentDigest: strings.Repeat("b", 64)}, wantStatus: http.StatusConflict, wantCode: "CREATION_SPEC_CONFLICT"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			binder := &planStoryboardPreviewBinderStub{result: test.result, err: test.err}
			proxy, _, signer := newPlanStoryboardPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return planStoryboardPreviewAcceptedResponse(), nil
			}), binder, true, 4096)
			recorder := servePlanStoryboardPreviewProxy(proxy, validPlanStoryboardPreviewRequest(validPlanStoryboardPreviewBody()), planStoryboardPreviewWriteMiddleware())
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), `"code":"`+test.wantCode+`"`) ||
				calls != 0 || signer.identity.RequestID != "" || strings.Contains(recorder.Body.String(), "postgres") ||
				strings.Contains(recorder.Body.String(), planStoryboardPreviewDigest) || strings.Contains(recorder.Body.String(), planStoryboardPreviewCreationSpecID) {
				t.Fatalf("binding mapping status=%d calls=%d identity=%+v body=%s", recorder.Code, calls, signer.identity, recorder.Body.String())
			}
		})
	}
}

// TestPlanStoryboardPreviewProxyMapsOnlySafeUpstreamErrors 验证 400/404/409/503 白名单，内部身份和未知错误统一降为 503。
func TestPlanStoryboardPreviewProxyMapsOnlySafeUpstreamErrors(t *testing.T) {
	tests := []struct {
		name           string
		upstreamStatus int
		upstreamCode   string
		wantStatus     int
		wantCode       string
	}{
		{name: "invalid", upstreamStatus: 400, upstreamCode: "INVALID_ARGUMENT", wantStatus: 400, wantCode: "INVALID_ARGUMENT"},
		{name: "session", upstreamStatus: 404, upstreamCode: "SESSION_NOT_FOUND", wantStatus: 404, wantCode: "SESSION_NOT_FOUND"},
		{name: "disabled", upstreamStatus: 404, upstreamCode: "PREVIEW_DISABLED", wantStatus: 404, wantCode: "PREVIEW_DISABLED"},
		{name: "idempotency", upstreamStatus: 409, upstreamCode: "IDEMPOTENCY_CONFLICT", wantStatus: 409, wantCode: "IDEMPOTENCY_CONFLICT"},
		{name: "lane", upstreamStatus: 409, upstreamCode: "SESSION_LANE_BLOCKED", wantStatus: 409, wantCode: "SESSION_LANE_BLOCKED"},
		{name: "persistence", upstreamStatus: 503, upstreamCode: "PERSISTENCE_UNAVAILABLE", wantStatus: 503, wantCode: "PERSISTENCE_UNAVAILABLE"},
		{name: "identity hidden", upstreamStatus: 401, upstreamCode: "INTERNAL_IDENTITY_INVALID", wantStatus: 503, wantCode: "DEPENDENCY_UNAVAILABLE"},
		{name: "unknown hidden", upstreamStatus: 418, upstreamCode: "REF_" + planStoryboardPreviewDigest, wantStatus: 503, wantCode: "DEPENDENCY_UNAVAILABLE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binder := &planStoryboardPreviewBinderStub{result: validPlanStoryboardPreviewRef()}
			proxy, _, _ := newPlanStoryboardPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: test.upstreamStatus, Header: http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"error":{"code":"` + test.upstreamCode + `","message":"internal ` +
						planStoryboardPreviewCreationSpecID + `","request_id":"` + agentProxyRequestID + `","retryable":false,"details":{}}}`)),
				}, nil
			}), binder, true, 4096)
			recorder := servePlanStoryboardPreviewProxy(proxy, validPlanStoryboardPreviewRequest(validPlanStoryboardPreviewBody()), planStoryboardPreviewWriteMiddleware())
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), `"code":"`+test.wantCode+`"`) ||
				strings.Contains(recorder.Body.String(), "internal") || strings.Contains(recorder.Body.String(), planStoryboardPreviewCreationSpecID) ||
				strings.Contains(recorder.Body.String(), planStoryboardPreviewDigest) {
				t.Fatalf("upstream mapping status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// TestPlanStoryboardPreviewProxyDisabledAndInvalid202FailClosed 验证默认关闭不查 Owner，非法 202 不回显上游正文。
func TestPlanStoryboardPreviewProxyDisabledAndInvalid202FailClosed(t *testing.T) {
	binder := &planStoryboardPreviewBinderStub{result: validPlanStoryboardPreviewRef()}
	calls := 0
	disabled, access, signer := newPlanStoryboardPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return planStoryboardPreviewAcceptedResponse(), nil
	}), binder, false, 4096)
	recorder := servePlanStoryboardPreviewProxy(disabled, validPlanStoryboardPreviewRequest(validPlanStoryboardPreviewBody()), planStoryboardPreviewWriteMiddleware())
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"PREVIEW_DISABLED"`) ||
		calls != 0 || access.sessionID != "" || binder.calls != 0 || signer.identity.RequestID != "" {
		t.Fatalf("disabled proxy reached protected flow: status=%d calls=%d access=%+v binder=%d body=%s",
			recorder.Code, calls, access, binder.calls, recorder.Body.String())
	}

	binder = &planStoryboardPreviewBinderStub{result: validPlanStoryboardPreviewRef()}
	invalid, _, _ := newPlanStoryboardPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusAccepted, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"schema_version":"plan_storyboard.preview.enqueue.v1","request_id":"` + agentProxyRequestID +
				`","session_id":"` + agentProxySessionID + `","input_id":"` + planStoryboardPreviewInputID +
				`","turn_id":"` + planStoryboardPreviewInputID + `","run_id":"` + planStoryboardPreviewRunID +
				`","tool_call_id":"` + planStoryboardPreviewToolCallID + `","status":"pending","replayed":false,"internal_ref":"` +
				planStoryboardPreviewDigest + `"}`))}, nil
	}), binder, true, 4096)
	recorder = servePlanStoryboardPreviewProxy(invalid, validPlanStoryboardPreviewRequest(validPlanStoryboardPreviewBody()), planStoryboardPreviewWriteMiddleware())
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"DEPENDENCY_UNAVAILABLE"`) ||
		strings.Contains(recorder.Body.String(), planStoryboardPreviewDigest) {
		t.Fatalf("invalid 202 leaked upstream: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// TestServerRegistersPlanStoryboardPreviewBehindSessionAndCSRF 验证 Composition Root 路由只在完整写中间件后进入 Owner、Binder 与 Agent。
func TestServerRegistersPlanStoryboardPreviewBehindSessionAndCSRF(t *testing.T) {
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
	binder := &planStoryboardPreviewBinderStub{result: validPlanStoryboardPreviewRef()}
	clientCalls := 0
	proxy, access, signer := newPlanStoryboardPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		clientCalls++
		return planStoryboardPreviewAcceptedResponse(), nil
	}), binder, true, 4096)
	server, err := New(config.HTTPConfig{
		Address: ":0", HeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1024,
	}, config.ServiceConfig{Name: "business-test", Version: "test"}, health.NewState(), RouteHandlers{
		Auth: authHandler, Project: projectHandler, Agent: proxy.base, PlanStoryboard: proxy,
		Skill: mustSkillHandlerForServerTest(t), SkillReview: mustSkillReviewHandlerForServerTest(t),
		SkillGovernance: mustSkillGovernanceHandlerForServerTest(t), SkillMarket: mustSkillMarketHandlerForServerTest(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := validPlanStoryboardPreviewRequest(validPlanStoryboardPreviewBody())
	request.Header.Del("X-CSRF-Token")
	request.Header.Del("Cookie")
	request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque-cookie-token"})
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || clientCalls != 0 || access.sessionID != "" || binder.calls != 0 || signer.identity.RequestID != "" {
		t.Fatalf("missing CSRF reached Storyboard flow: status=%d calls=%d access=%+v binder=%d identity=%+v body=%s",
			recorder.Code, clientCalls, access, binder.calls, signer.identity, recorder.Body.String())
	}
	request = validPlanStoryboardPreviewRequest(validPlanStoryboardPreviewBody())
	request.Header.Set("X-CSRF-Token", resolved.CSRFToken)
	request.Header.Del("Cookie")
	request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque-cookie-token"})
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || clientCalls != 1 || binder.calls != 1 ||
		access.userID != resolved.Principal.ID || binder.request.RequestID != agentProxyRequestID ||
		signer.identity.PrincipalUserID != resolved.Principal.ID || signer.identity.Scope != agentidentity.ScopePlanStoryboardPreviewWrite {
		t.Fatalf("valid CSRF/authority status=%d calls=%d access=%+v binding=%+v identity=%+v body=%s",
			recorder.Code, clientCalls, access, binder.request, signer.identity, recorder.Body.String())
	}
}

// fmtTestError 生成带内部文本的普通错误，验证 Transport 不会依据错误字符串分支或回显。
func fmtTestError(message string) error {
	return errors.New(message)
}
