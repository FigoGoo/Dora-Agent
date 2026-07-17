package httpserver

import (
	"context"
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

// writePromptsPreviewBinderStub 记录 BFF 交给当前 Workspace 绑定器的完整可信身份与 PresentedRef。
type writePromptsPreviewBinderStub struct {
	request WritePromptsPreviewStoryboardBindingRequest
	result  WritePromptsPreviewStoryboardRef
	err     error
	calls   int
}

// BindCurrent 返回测试冻结的权威引用或稳定错误，不访问外部资源。
func (stub *writePromptsPreviewBinderStub) BindCurrent(_ context.Context, request WritePromptsPreviewStoryboardBindingRequest) (WritePromptsPreviewStoryboardRef, error) {
	stub.calls++
	stub.request = request
	return stub.result, stub.err
}

// newWritePromptsPreviewProxyForTest 构造独立 Prompt BFF，并保留 Owner、Binder 与签名观察点。
func newWritePromptsPreviewProxyForTest(t *testing.T, client AgentHTTPClient, binder *writePromptsPreviewBinderStub, enabled bool, bodyLimit int64) (*WritePromptsPreviewProxy, *agentProxyAccessStub, *agentProxySignerStub) {
	t.Helper()
	access := &agentProxyAccessStub{result: project.AgentSessionAccess{ProjectID: agentProxyProjectID, AgentSessionID: agentProxySessionID}}
	signer := &agentProxySignerStub{}
	base, err := NewAgentProxyHandler(access, signer, agentProxyIDs{}, client, config.AgentHTTPConfig{
		BaseURL: "http://agent.internal", RequestTimeout: time.Second, PreviewMaxRequestBodyBytes: bodyLimit,
	})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NewWritePromptsPreviewProxy(base, binder, enabled)
	if err != nil {
		t.Fatal(err)
	}
	return proxy, access, signer
}

// serveWritePromptsPreviewProxy 注册独立 canonical 路由，并用等价 Session+CSRF 测试中间件执行请求。
func serveWritePromptsPreviewProxy(proxy *WritePromptsPreviewProxy, request *http.Request) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := proxy.Register(router, planStoryboardPreviewWriteMiddleware()); err != nil {
		panic(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// validWritePromptsPreviewBody 故意打乱浏览器字段顺序，用于证明 BFF 输出唯一 canonical 字节。
func validWritePromptsPreviewBody(t *testing.T) string {
	t.Helper()
	ref := writePromptsBinderCurrentRef(t)
	return `{"tool_intent":{"output_language":"zh-CN","writing_instruction":"为每个槽位编写清晰提示词","schema_version":"write_prompts.preview.intent.v1"},` +
		`"storyboard_preview_ref":{"content_digest":"` + ref.ContentDigest + `","version":1,"id":"` + ref.ID + `"},` +
		`"schema_version":"write_prompts.preview.enqueue-request.v1"}`
}

// validWritePromptsPreviewRequest 构造带 canonical 幂等键和 CSRF 的浏览器请求。
func validWritePromptsPreviewRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/sessions/"+agentProxySessionID+"/write-prompts-previews", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", previewIdempotencyKey)
	request.Header.Set("X-CSRF-Token", "valid-csrf")
	request.Header.Set("Cookie", "dora_session=browser-secret")
	request.Header.Set("Authorization", "Bearer browser-secret")
	request.Header.Set(agentidentity.HeaderAssertion, "browser-forged")
	return request
}

// writePromptsPreviewAcceptedResponse 返回 Agent exact 202 pending DTO。
func writePromptsPreviewAcceptedResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusAccepted, Header: http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{"schema_version":"write_prompts.preview.enqueue.v1","request_id":"` + agentProxyRequestID +
			`","session_id":"` + agentProxySessionID + `","input_id":"` + planStoryboardPreviewInputID +
			`","turn_id":"` + planStoryboardPreviewTurnID + `","run_id":"` + planStoryboardPreviewRunID +
			`","tool_call_id":"` + planStoryboardPreviewToolCallID + `","status":"pending","replayed":false}`)),
	}
}

// TestWritePromptsPreviewProxyBindsCurrentStoryboardAndReturnsExact202 验证 Owner/Project/Session/ref 联合绑定、专用 Scope 和 canonical 内部请求。
func TestWritePromptsPreviewProxyBindsCurrentStoryboardAndReturnsExact202(t *testing.T) {
	ref := writePromptsBinderCurrentRef(t)
	binder := &writePromptsPreviewBinderStub{result: ref}
	var upstream *http.Request
	var upstreamBody string
	proxy, access, signer := newWritePromptsPreviewProxyForTest(t, agentProxyClientFunc(func(request *http.Request) (*http.Response, error) {
		upstream = request
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		upstreamBody = string(body)
		if _, ok := request.Context().Deadline(); !ok {
			t.Fatal("Write Prompts Preview POST has no bounded deadline")
		}
		return writePromptsPreviewAcceptedResponse(), nil
	}), binder, true, 4096)
	recorder := serveWritePromptsPreviewProxy(proxy, validWritePromptsPreviewRequest(t, validWritePromptsPreviewBody(t)))
	if recorder.Code != http.StatusAccepted || recorder.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(recorder.Body.String(), `"schema_version":"write_prompts.preview.enqueue.v1"`) {
		t.Fatalf("response status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	internalTarget := "/internal/v1/workspaces/sessions/" + agentProxySessionID + "/write-prompts-previews"
	if upstream == nil || upstream.Method != http.MethodPost || upstream.URL.String() != "http://agent.internal"+internalTarget ||
		upstream.Header.Get("Cookie") != "" || upstream.Header.Get("Authorization") != "" || upstream.Header.Get("X-CSRF-Token") != "" ||
		upstream.Header.Get(agentidentity.HeaderAssertion) != agentProxyAssertion {
		t.Fatalf("unsafe Prompt upstream=%v headers=%v", upstream, upstream.Header)
	}
	wantBody := `{"schema_version":"write_prompts.preview.enqueue-request.v1","storyboard_preview_ref":{"id":"` + ref.ID +
		`","version":1,"content_digest":"` + ref.ContentDigest + `"},"tool_intent":{"schema_version":"write_prompts.preview.intent.v1",` +
		`"writing_instruction":"为每个槽位编写清晰提示词","output_language":"zh-CN"}}`
	if upstreamBody != wantBody {
		t.Fatalf("canonical body=%s want=%s", upstreamBody, wantBody)
	}
	if access.userID != agentProxyUserID || binder.calls != 1 || binder.request.PresentedRef != ref ||
		signer.identity.Scope != agentidentity.ScopeWritePromptsPreviewWrite || signer.identity.CanonicalTarget != internalTarget {
		t.Fatalf("binding mismatch access=%+v binding=%+v identity=%+v", access, binder.request, signer.identity)
	}
}

// TestWritePromptsPreviewProxyRejectsStrictBrowserBoundary 验证非法请求在 Owner、Binder、Signer 与 Agent 调用前失败关闭。
func TestWritePromptsPreviewProxyRejectsStrictBrowserBoundary(t *testing.T) {
	valid := validWritePromptsPreviewBody(t)
	ref := writePromptsBinderCurrentRef(t)
	tests := []struct {
		name string
		body string
	}{
		{name: "unknown outer", body: strings.Replace(valid, `"schema_version":"write_prompts.preview.enqueue-request.v1"`, `"schema_version":"write_prompts.preview.enqueue-request.v1","user_id":"`+agentProxyUserID+`"`, 1)},
		{name: "unknown ref", body: strings.Replace(valid, `"version":1`, `"version":1,"project_id":"`+agentProxyProjectID+`"`, 1)},
		{name: "unknown intent", body: strings.Replace(valid, `"output_language":"zh-CN"`, `"output_language":"zh-CN","target_ids":[]`, 1)},
		{name: "duplicate", body: strings.Replace(valid, `"writing_instruction":"为每个槽位编写清晰提示词"`, `"writing_instruction":"为每个槽位编写清晰提示词","writing_instruction":"覆盖"`, 1)},
		{name: "null language", body: strings.Replace(valid, `"output_language":"zh-CN"`, `"output_language":null`, 1)},
		{name: "unknown language", body: strings.Replace(valid, `"output_language":"zh-CN"`, `"output_language":"fr-FR"`, 1)},
		{name: "non NFC", body: strings.Replace(valid, "为每个槽位编写清晰提示词", "e\u0301", 1)},
		{name: "too long", body: strings.Replace(valid, "为每个槽位编写清晰提示词", strings.Repeat("x", 1001), 1)},
		{name: "invalid ref", body: strings.Replace(valid, ref.ID, "not-uuid-v7", 1)},
		{name: "trailing", body: valid + `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			binder := &writePromptsPreviewBinderStub{result: ref}
			proxy, access, signer := newWritePromptsPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return writePromptsPreviewAcceptedResponse(), nil
			}), binder, true, 2048)
			recorder := serveWritePromptsPreviewProxy(proxy, validWritePromptsPreviewRequest(t, test.body))
			if recorder.Code != http.StatusBadRequest || calls != 0 || access.sessionID != "" || binder.calls != 0 || signer.identity.RequestID != "" ||
				strings.Contains(recorder.Body.String(), ref.ContentDigest) || strings.Contains(recorder.Body.String(), ref.ID) {
				t.Fatalf("invalid request reached protected flow: status=%d calls=%d access=%+v binder=%d identity=%+v body=%s", recorder.Code, calls, access, binder.calls, signer.identity, recorder.Body.String())
			}
		})
	}
}

// TestWritePromptsPreviewProxyRequiresCSRFAndOwnerBeforeStoryboard 验证缺失 CSRF 不进入 Handler，Session Owner 不存在时也不读取 Storyboard。
func TestWritePromptsPreviewProxyRequiresCSRFAndOwnerBeforeStoryboard(t *testing.T) {
	ref := writePromptsBinderCurrentRef(t)
	binder := &writePromptsPreviewBinderStub{result: ref}
	proxy, access, signer := newWritePromptsPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		return writePromptsPreviewAcceptedResponse(), nil
	}), binder, true, 4096)
	request := validWritePromptsPreviewRequest(t, validWritePromptsPreviewBody(t))
	request.Header.Del("X-CSRF-Token")
	recorder := serveWritePromptsPreviewProxy(proxy, request)
	if recorder.Code != http.StatusForbidden || access.sessionID != "" || binder.calls != 0 || signer.identity.RequestID != "" {
		t.Fatalf("missing CSRF reached flow: status=%d access=%+v binder=%d identity=%+v", recorder.Code, access, binder.calls, signer.identity)
	}
	access.err = project.ErrAgentSessionNotFound
	recorder = serveWritePromptsPreviewProxy(proxy, validWritePromptsPreviewRequest(t, validWritePromptsPreviewBody(t)))
	if recorder.Code != http.StatusNotFound || binder.calls != 0 || signer.identity.RequestID != "" {
		t.Fatalf("missing Owner reached Binder: status=%d binder=%d identity=%+v", recorder.Code, binder.calls, signer.identity)
	}
}

// TestServerRegistersWritePromptsPreviewBehindSessionAndCSRF 验证 Composition Root 路由只在完整写中间件后进入 Owner、Binder 与 Agent。
func TestServerRegistersWritePromptsPreviewBehindSessionAndCSRF(t *testing.T) {
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
	binder := &writePromptsPreviewBinderStub{result: writePromptsBinderCurrentRef(t)}
	proxy, access, signer := newWritePromptsPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		return writePromptsPreviewAcceptedResponse(), nil
	}), binder, true, 4096)
	server, err := New(config.HTTPConfig{Address: ":0", HeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, MaxHeaderBytes: 1024},
		config.ServiceConfig{Name: "business-test", Version: "test"}, health.NewState(), RouteHandlers{
			Auth: authHandler, Project: projectHandler, Agent: proxy.base, WritePrompts: proxy,
			Skill: mustSkillHandlerForServerTest(t), SkillReview: mustSkillReviewHandlerForServerTest(t),
			SkillGovernance: mustSkillGovernanceHandlerForServerTest(t), SkillMarket: mustSkillMarketHandlerForServerTest(t),
		})
	if err != nil {
		t.Fatal(err)
	}
	request := validWritePromptsPreviewRequest(t, validWritePromptsPreviewBody(t))
	request.Header.Del("X-CSRF-Token")
	request.Header.Del("Cookie")
	request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque-cookie-token"})
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || access.sessionID != "" || binder.calls != 0 {
		t.Fatalf("missing CSRF reached Prompt flow: status=%d access=%+v binder=%d", recorder.Code, access, binder.calls)
	}
	request = validWritePromptsPreviewRequest(t, validWritePromptsPreviewBody(t))
	request.Header.Set("X-CSRF-Token", resolved.CSRFToken)
	request.Header.Del("Cookie")
	request.AddCookie(&http.Cookie{Name: "dora_session", Value: "opaque-cookie-token"})
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted || binder.calls != 1 || access.userID != resolved.Principal.ID ||
		signer.identity.PrincipalUserID != resolved.Principal.ID || signer.identity.Scope != agentidentity.ScopeWritePromptsPreviewWrite {
		t.Fatalf("valid authority status=%d access=%+v binding=%+v identity=%+v body=%s", recorder.Code, access, binder.request, signer.identity, recorder.Body.String())
	}
}
