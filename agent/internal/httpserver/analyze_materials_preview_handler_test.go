package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/analyzematerialsruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/gin-gonic/gin"
)

const (
	analyzeHTTPWebSessionID = "019f68e8-0110-7000-8000-000000000110"
	analyzeHTTPAssetID      = "019f68e8-0111-7000-8000-000000000111"
	analyzeHTTPTurnID       = "019f68e8-0112-7000-8000-000000000112"
	analyzeHTTPRunID        = "019f68e8-0113-7000-8000-000000000113"
	analyzeHTTPToolCallID   = "019f68e8-0114-7000-8000-000000000114"
	analyzeHTTPPath         = "/internal/v1/workspaces/sessions/" + previewHTTPTestSessionID + "/analyze-materials-previews"
	analyzeHTTPBody         = `{"schema_version":"analyze_materials.preview.intent.v1","asset_ids":["` + analyzeHTTPAssetID + `"],"analysis_goal":"识别素材核心信息","focus_dimensions":["content"],"output_language":"zh-CN","expected_assets":[{"asset_id":"` + analyzeHTTPAssetID + `","asset_version":1}]}`
)

// analyzeHTTPService 记录 Handler 交付的可信入队 DTO。
type analyzeHTTPService struct {
	request analyzematerialsruntime.EnqueueRequest
	result  analyzematerialsruntime.EnqueueResponse
	err     error
	calls   int
}

// Enqueue 实现素材分析 Handler 测试所需的最小 Service 端口。
func (service *analyzeHTTPService) Enqueue(_ context.Context, request analyzematerialsruntime.EnqueueRequest) (analyzematerialsruntime.EnqueueResponse, error) {
	service.calls++
	service.request = request
	return service.result, service.err
}

// newAnalyzeHTTPRouter 构造只注册内部素材分析 POST 的测试 Router。
func newAnalyzeHTTPRouter(t *testing.T, verifier *previewHTTPVerifier, service *analyzeHTTPService) http.Handler {
	t.Helper()
	handler, err := NewAnalyzeMaterialsPreviewHandler(verifier, service, previewHTTPIDs{}, "local-agent-content-v1")
	if err != nil {
		t.Fatalf("创建 Analyze Materials Preview Handler 失败: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.Register(router)
	return router
}

// newAnalyzeHTTPVerifier 返回与素材分析 POST scope 精确绑定的可信断言结果。
func newAnalyzeHTTPVerifier() *previewHTTPVerifier {
	return &previewHTTPVerifier{claims: httpidentity.Claims{
		RequestID: previewHTTPTestRequestID, PrincipalUserID: previewHTTPTestUserID,
		WebSessionID: analyzeHTTPWebSessionID, WebSessionVersion: 3,
		ProjectID: previewHTTPTestProjectID, AgentSessionID: previewHTTPTestSessionID,
		Scope: httpidentity.ScopeAnalyzeMaterialsPreviewWrite, ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
}

// newAnalyzeHTTPRequest 构造带严格 Content-Type 和 UUIDv7 幂等键的内部 POST。
func newAnalyzeHTTPRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, analyzeHTTPPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", previewHTTPTestIdempotencyKey)
	return request
}

// TestAnalyzeMaterialsPreviewPOSTReturnsExactAcceptedDTO 验证专用 Scope、Access digest、typed body 与 202 exact-set。
func TestAnalyzeMaterialsPreviewPOSTReturnsExactAcceptedDTO(t *testing.T) {
	verifier := newAnalyzeHTTPVerifier()
	service := &analyzeHTTPService{result: analyzematerialsruntime.EnqueueResponse{
		SchemaVersion: analyzematerialsruntime.EnqueueResponseSchemaVersion,
		Status:        analyzematerialsruntime.EnqueuePendingStatus, InputID: previewHTTPTestInputID,
		TurnID: analyzeHTTPTurnID, RunID: analyzeHTTPRunID, ToolCallID: analyzeHTTPToolCallID,
	}}
	router := newAnalyzeHTTPRouter(t, verifier, service)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newAnalyzeHTTPRequest(analyzeHTTPBody))
	if recorder.Code != http.StatusAccepted || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("202 响应错误: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	want := `{"schema_version":"analyze_materials.preview.enqueue.v1","request_id":"` + previewHTTPTestRequestID + `","session_id":"` + previewHTTPTestSessionID + `","input_id":"` + previewHTTPTestInputID + `","turn_id":"` + analyzeHTTPTurnID + `","run_id":"` + analyzeHTTPRunID + `","tool_call_id":"` + analyzeHTTPToolCallID + `","status":"pending","replayed":false}`
	if strings.TrimSpace(recorder.Body.String()) != want {
		t.Fatalf("202 exact-set 漂移:\n got: %s\nwant: %s", recorder.Body.String(), want)
	}
	if verifier.calls != 1 || verifier.request.Scope != httpidentity.ScopeAnalyzeMaterialsPreviewWrite ||
		verifier.request.CanonicalTarget != analyzeHTTPPath || verifier.request.Method != http.MethodPost {
		t.Fatalf("身份 Scope/Target 映射错误: calls=%d request=%+v", verifier.calls, verifier.request)
	}
	if service.calls != 1 || service.request.RequestID != previewHTTPTestRequestID ||
		service.request.SessionID != previewHTTPTestSessionID || service.request.UserID != previewHTTPTestUserID ||
		service.request.ProjectID != previewHTTPTestProjectID || service.request.IdempotencyKey != previewHTTPTestIdempotencyKey ||
		string(service.request.IntentJSON) != analyzeHTTPBody || service.request.AccessScopeRef != httpidentity.ScopeAnalyzeMaterialsPreviewWrite ||
		len(service.request.AccessScopeDigest) != 64 || service.request.IntentKeyVersion != "local-agent-content-v1" {
		t.Fatalf("Runtime 请求映射错误: calls=%d request=%+v", service.calls, service.request)
	}
}

// TestAnalyzeMaterialsPreviewPOSTMapsStableServiceErrors 验证严格输入、Owner、幂等、Lane 与持久化错误映射。
func TestAnalyzeMaterialsPreviewPOSTMapsStableServiceErrors(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantCode   string
		retryable  bool
	}{
		{analyzematerialsruntime.ErrInvalidInput, http.StatusBadRequest, errorCodeInvalidArgument, false},
		{analyzematerialsruntime.ErrNotFound, http.StatusNotFound, errorCodeSessionNotFound, false},
		{analyzematerialsruntime.ErrIdempotencyConflict, http.StatusConflict, errorCodeIdempotencyConflict, false},
		{analyzematerialsruntime.ErrSessionLaneBlocked, http.StatusConflict, errorCodeSessionLaneBlocked, false},
		{analyzematerialsruntime.ErrPersistence, http.StatusServiceUnavailable, errorCodePersistenceUnavailable, true},
	}
	for _, fixture := range tests {
		verifier := newAnalyzeHTTPVerifier()
		service := &analyzeHTTPService{err: fixture.err}
		recorder := httptest.NewRecorder()
		newAnalyzeHTTPRouter(t, verifier, service).ServeHTTP(recorder, newAnalyzeHTTPRequest(analyzeHTTPBody))
		if recorder.Code != fixture.wantStatus {
			t.Fatalf("error=%v status=%d want=%d body=%s", fixture.err, recorder.Code, fixture.wantStatus, recorder.Body.String())
		}
		assertPreviewHTTPError(t, recorder, fixture.wantCode, fixture.retryable)
	}
}

var _ AnalyzeMaterialsPreviewService = (*analyzeHTTPService)(nil)
