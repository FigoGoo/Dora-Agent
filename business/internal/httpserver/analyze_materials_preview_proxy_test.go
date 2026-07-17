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
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/gin-gonic/gin"
)

const (
	analyzeMaterialsAssetA     = "019f0000-0000-7000-8000-000000000009"
	analyzeMaterialsAssetB     = "019f0000-0000-7000-8000-00000000000a"
	analyzeMaterialsTurnID     = "019f0000-0000-7000-8000-00000000000b"
	analyzeMaterialsRunID      = "019f0000-0000-7000-8000-00000000000c"
	analyzeMaterialsToolCallID = "019f0000-0000-7000-8000-00000000000d"
)

// newAnalyzeMaterialsPreviewProxyForTest 构造仅启用素材分析写入口的 BFF Handler。
func newAnalyzeMaterialsPreviewProxyForTest(t *testing.T, client AgentHTTPClient, enabled bool) (*AgentProxyHandler, *agentProxyAccessStub, *agentProxySignerStub) {
	t.Helper()
	access := &agentProxyAccessStub{result: project.AgentSessionAccess{ProjectID: agentProxyProjectID, AgentSessionID: agentProxySessionID}}
	signer := &agentProxySignerStub{}
	handler, err := NewAgentProxyHandler(access, signer, agentProxyIDs{}, client, config.AgentHTTPConfig{
		BaseURL: "http://agent.internal", RequestTimeout: time.Second,
		AnalyzeMaterialsRuntimeEnabled: enabled, PreviewMaxRequestBodyBytes: 16 << 10,
	})
	if err != nil {
		t.Fatalf("NewAgentProxyHandler() error = %v", err)
	}
	return handler, access, signer
}

// serveAnalyzeMaterialsPreviewProxy 注册正式同源路由并执行一次测试请求。
func serveAnalyzeMaterialsPreviewProxy(handler *AgentProxyHandler, request *http.Request) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.Register(router, agentProxyResolvedMiddleware(), agentProxyResolvedMiddleware())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

// validAnalyzeMaterialsPreviewBody 返回故意打乱 exact-set 顺序的合法请求，用于验证规范编码。
func validAnalyzeMaterialsPreviewBody() string {
	return `{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["` + analyzeMaterialsAssetB + `","` + analyzeMaterialsAssetA + `"],"analysis_goal":"识别可复用的视觉与叙事元素","focus_dimensions":["visual","content"],"output_language":"zh-CN","expected_assets":[{"asset_id":"` + analyzeMaterialsAssetB + `","asset_version":2},{"asset_id":"` + analyzeMaterialsAssetA + `","asset_version":1}]}`
}

// validAnalyzeMaterialsPreviewRequest 构造带规范幂等键的公开 BFF POST。
func validAnalyzeMaterialsPreviewRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/sessions/"+agentProxySessionID+"/analyze-materials-previews", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", previewIdempotencyKey)
	return request
}

// analyzeMaterialsAcceptedResponse 返回 Agent exact 202 DTO。
func analyzeMaterialsAcceptedResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusAccepted,
		Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(`{"schema_version":"analyze_materials.preview.enqueue.v1","request_id":"` + agentProxyRequestID + `","session_id":"` + agentProxySessionID + `","input_id":"` + previewInputID + `","turn_id":"` + analyzeMaterialsTurnID + `","run_id":"` + analyzeMaterialsRunID + `","tool_call_id":"` + analyzeMaterialsToolCallID + `","status":"pending","replayed":false}`)),
	}
}

// TestAnalyzeMaterialsPreviewProxyReturnsExact202AndCanonicalIntent 验证专用 Scope、Owner 绑定和排序后的唯一 Intent 字节表示。
func TestAnalyzeMaterialsPreviewProxyReturnsExact202AndCanonicalIntent(t *testing.T) {
	var upstream *http.Request
	var upstreamBody string
	client := agentProxyClientFunc(func(request *http.Request) (*http.Response, error) {
		upstream = request
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		upstreamBody = string(body)
		return analyzeMaterialsAcceptedResponse(), nil
	})
	handler, access, signer := newAnalyzeMaterialsPreviewProxyForTest(t, client, true)
	recorder := serveAnalyzeMaterialsPreviewProxy(handler, validAnalyzeMaterialsPreviewRequest(validAnalyzeMaterialsPreviewBody()))
	if recorder.Code != http.StatusAccepted || recorder.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(recorder.Body.String(), `"schema_version":"analyze_materials.preview.enqueue.v1"`) {
		t.Fatalf("analyze preview response status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	target := "/internal/v1/workspaces/sessions/" + agentProxySessionID + "/analyze-materials-previews"
	if upstream == nil || upstream.Method != http.MethodPost || upstream.URL.String() != "http://agent.internal"+target ||
		upstream.Header.Get("Idempotency-Key") != previewIdempotencyKey || upstream.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("invalid upstream=%v headers=%v", upstream, upstream.Header)
	}
	wantBody := `{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["` + analyzeMaterialsAssetA + `","` + analyzeMaterialsAssetB + `"],"analysis_goal":"识别可复用的视觉与叙事元素","focus_dimensions":["content","visual"],"output_language":"zh-CN","expected_assets":[{"asset_id":"` + analyzeMaterialsAssetA + `","asset_version":1},{"asset_id":"` + analyzeMaterialsAssetB + `","asset_version":2}]}`
	if upstreamBody != wantBody {
		t.Fatalf("canonical upstream body=%s, want %s", upstreamBody, wantBody)
	}
	if signer.identity.Scope != agentidentity.ScopeAnalyzeMaterialsPreviewWrite || signer.identity.CanonicalTarget != target ||
		signer.identity.Method != http.MethodPost || signer.identity.PrincipalUserID != agentProxyUserID ||
		access.userID != agentProxyUserID || access.sessionID != agentProxySessionID {
		t.Fatalf("analyze owner/identity mismatch: access=%+v identity=%+v", access, signer.identity)
	}
}

// TestAnalyzeMaterialsPreviewProxyRejectsNestedDuplicateAndExactSetDrift 验证 BFF 在 Owner 查询和 Agent 调用前拒绝歧义 Intent。
func TestAnalyzeMaterialsPreviewProxyRejectsNestedDuplicateAndExactSetDrift(t *testing.T) {
	tests := []string{
		`{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["` + analyzeMaterialsAssetA + `"],"analysis_goal":"目标","focus_dimensions":["content"],"output_language":"zh-CN","expected_assets":[{"asset_id":"` + analyzeMaterialsAssetA + `","asset_id":"` + analyzeMaterialsAssetA + `","asset_version":1}]}`,
		`{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["` + analyzeMaterialsAssetA + `"],"analysis_goal":"目标","focus_dimensions":["content"],"output_language":"zh-CN","expected_assets":[]}`,
		`{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["` + analyzeMaterialsAssetA + `"],"analysis_goal":"目标","focus_dimensions":["content","content"],"output_language":"zh-CN","expected_assets":[{"asset_id":"` + analyzeMaterialsAssetA + `","asset_version":1}]}`,
		`{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["` + analyzeMaterialsAssetA + `"],"analysis_goal":"目标","focus_dimensions":["content"],"output_language":"zh-CN","expected_assets":null}`,
	}
	for _, body := range tests {
		calls := 0
		handler, access, signer := newAnalyzeMaterialsPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return analyzeMaterialsAcceptedResponse(), nil
		}), true)
		recorder := serveAnalyzeMaterialsPreviewProxy(handler, validAnalyzeMaterialsPreviewRequest(body))
		if recorder.Code != http.StatusBadRequest || calls != 0 || access.sessionID != "" || signer.identity.RequestID != "" {
			t.Fatalf("invalid analyze intent reached protected flow: status=%d calls=%d access=%+v identity=%+v body=%s", recorder.Code, calls, access, signer.identity, recorder.Body.String())
		}
	}
}

// TestAnalyzeMaterialsPreviewProxyDisabledFailsBeforeOwnerLookup 验证默认关闭不读取 Owner 且不调用 Agent。
func TestAnalyzeMaterialsPreviewProxyDisabledFailsBeforeOwnerLookup(t *testing.T) {
	calls := 0
	handler, access, signer := newAnalyzeMaterialsPreviewProxyForTest(t, agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return analyzeMaterialsAcceptedResponse(), nil
	}), false)
	recorder := serveAnalyzeMaterialsPreviewProxy(handler, validAnalyzeMaterialsPreviewRequest(validAnalyzeMaterialsPreviewBody()))
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"PREVIEW_DISABLED"`) ||
		calls != 0 || access.sessionID != "" || signer.identity.RequestID != "" {
		t.Fatalf("disabled analyze preview reached protected flow: status=%d calls=%d access=%+v identity=%+v body=%s", recorder.Code, calls, access, signer.identity, recorder.Body.String())
	}
}
