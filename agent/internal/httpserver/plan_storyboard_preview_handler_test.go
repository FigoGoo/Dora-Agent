package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/planstoryboardruntime"
	"github.com/gin-gonic/gin"
)

const (
	storyboardHTTPTestRequestID      = "019f68e8-0120-7000-8000-000000000120"
	storyboardHTTPTestUserID         = "019f68e8-0121-7000-8000-000000000121"
	storyboardHTTPTestOtherUserID    = "019f68e8-0122-7000-8000-000000000122"
	storyboardHTTPTestProjectID      = "019f68e8-0123-7000-8000-000000000123"
	storyboardHTTPTestSessionID      = "019f68e8-0124-7000-8000-000000000124"
	storyboardHTTPTestOtherSessionID = "019f68e8-0125-7000-8000-000000000125"
	storyboardHTTPTestWebSessionID   = "019f68e8-0126-7000-8000-000000000126"
	storyboardHTTPTestIdempotencyKey = "019f68e8-0127-7000-8000-000000000127"
	storyboardHTTPTestCreationSpecID = "019f68e8-0128-7000-8000-000000000128"
	storyboardHTTPTestInputID        = "019f68e8-0129-7000-8000-000000000129"
	storyboardHTTPTestTurnID         = "019f68e8-0130-7000-8000-000000000130"
	storyboardHTTPTestRunID          = "019f68e8-0131-7000-8000-000000000131"
	storyboardHTTPTestToolCallID     = "019f68e8-0132-7000-8000-000000000132"
	storyboardHTTPTestErrorID        = "019f68e8-0133-7000-8000-000000000133"
	storyboardHTTPTestDigest         = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	storyboardHTTPTestPath           = "/internal/v1/workspaces/sessions/" + storyboardHTTPTestSessionID + "/plan-storyboard-previews"
	storyboardHTTPTestBody           = `{"schema_version":"plan_storyboard.preview.enqueue-request.v1","creation_spec_ref":{"id":"` + storyboardHTTPTestCreationSpecID + `","version":1,"content_digest":"` + storyboardHTTPTestDigest + `"},"tool_intent":{"schema_version":"plan_storyboard.preview.intent.v1","planning_instruction":"规划三段式故事板","target_duration_seconds":60}}`
)

// storyboardHTTPVerifier 记录 Handler 传入的专用 Scope 与 canonical Target。
type storyboardHTTPVerifier struct {
	request httpidentity.Request
	claims  httpidentity.Claims
	err     error
	calls   int
}

// Verify 实现 Handler 测试所需的最小身份断言端口。
func (verifier *storyboardHTTPVerifier) Verify(_ context.Context, request httpidentity.Request) (httpidentity.Claims, error) {
	verifier.calls++
	verifier.request = request
	return verifier.claims, verifier.err
}

// storyboardHTTPService 记录可信引用与 canonical Tool Intent 入队 DTO。
type storyboardHTTPService struct {
	request  planstoryboardruntime.EnqueueRequest
	result   planstoryboardruntime.EnqueueResponse
	err      error
	calls    int
	deadline time.Time
}

// Enqueue 实现 persist-only Runtime 测试端口并记录断言收紧后的 Deadline。
func (service *storyboardHTTPService) Enqueue(ctx context.Context, request planstoryboardruntime.EnqueueRequest) (planstoryboardruntime.EnqueueResponse, error) {
	service.calls++
	service.request = request
	service.deadline, _ = ctx.Deadline()
	return service.result, service.err
}

// storyboardHTTPIDs 为身份认证前错误返回固定规范 UUIDv7。
type storyboardHTTPIDs struct{}

// New 返回固定测试 RequestID。
func (storyboardHTTPIDs) New() (string, error) { return storyboardHTTPTestErrorID, nil }

// TestPlanStoryboardPreviewPOSTReturnsExactAcceptedAndReplayDTO 验证 202 exact-set 与重放标志不改变稳定身份。
func TestPlanStoryboardPreviewPOSTReturnsExactAcceptedAndReplayDTO(t *testing.T) {
	for _, replayed := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "replayed"}[replayed], func(t *testing.T) {
			verifier := newStoryboardHTTPVerifier()
			service := &storyboardHTTPService{result: validStoryboardHTTPResult(replayed)}
			recorder := httptest.NewRecorder()
			newStoryboardHTTPRouter(t, verifier, service).ServeHTTP(recorder, newStoryboardHTTPRequest([]byte(storyboardHTTPTestBody)))

			if recorder.Code != http.StatusAccepted || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("202 响应错误: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
			want := `{"schema_version":"plan_storyboard.preview.enqueue.v1","request_id":"` + storyboardHTTPTestRequestID + `","session_id":"` + storyboardHTTPTestSessionID + `","input_id":"` + storyboardHTTPTestInputID + `","turn_id":"` + storyboardHTTPTestTurnID + `","run_id":"` + storyboardHTTPTestRunID + `","tool_call_id":"` + storyboardHTTPTestToolCallID + `","status":"pending","replayed":` + map[bool]string{false: "false", true: "true"}[replayed] + `}`
			if strings.TrimSpace(recorder.Body.String()) != want {
				t.Fatalf("202 exact-set 漂移:\n got: %s\nwant: %s", recorder.Body.String(), want)
			}
			if verifier.calls != 1 || verifier.request.Method != http.MethodPost ||
				verifier.request.CanonicalTarget != storyboardHTTPTestPath ||
				verifier.request.Scope != httpidentity.ScopePlanStoryboardPreviewWrite ||
				verifier.request.AgentSessionID != storyboardHTTPTestSessionID {
				t.Fatalf("身份 Scope/Target 映射错误: calls=%d request=%+v", verifier.calls, verifier.request)
			}
			wantIntent := `{"schema_version":"plan_storyboard.preview.intent.v1","planning_instruction":"规划三段式故事板","target_duration_seconds":60}`
			if service.calls != 1 || service.request.RequestID != storyboardHTTPTestRequestID ||
				service.request.SessionID != storyboardHTTPTestSessionID || service.request.UserID != storyboardHTTPTestUserID ||
				service.request.ProjectID != storyboardHTTPTestProjectID ||
				service.request.IdempotencyKey != storyboardHTTPTestIdempotencyKey ||
				service.request.CreationSpecRef.ID != storyboardHTTPTestCreationSpecID ||
				service.request.CreationSpecRef.Version != 1 ||
				service.request.CreationSpecRef.ContentDigest != storyboardHTTPTestDigest ||
				string(service.request.IntentJSON) != wantIntent ||
				service.request.AccessScopeRef != httpidentity.ScopePlanStoryboardPreviewWrite ||
				len(service.request.AccessScopeDigest) != 64 || service.request.IntentKeyVersion != "local-agent-content-v1" {
				t.Fatalf("Runtime 请求映射错误: calls=%d request=%+v", service.calls, service.request)
			}
			if !service.deadline.Equal(verifier.claims.ExpiresAt) {
				t.Fatalf("请求 Deadline 未收紧到 assertion exp: got=%v want=%v", service.deadline, verifier.claims.ExpiresAt)
			}
			for _, forbidden := range []string{storyboardHTTPTestCreationSpecID, storyboardHTTPTestProjectID, storyboardHTTPTestUserID, storyboardHTTPTestDigest} {
				if strings.Contains(recorder.Body.String(), forbidden) {
					t.Fatalf("202 回执泄漏可信资源引用 %q: %s", forbidden, recorder.Body.String())
				}
			}
		})
	}
}

// TestPlanStoryboardPreviewPOSTCanonicalizesOptionalToolIntent 验证省略可选时长后不补 null 或零值。
func TestPlanStoryboardPreviewPOSTCanonicalizesOptionalToolIntent(t *testing.T) {
	body := strings.Replace(storyboardHTTPTestBody, `,"target_duration_seconds":60`, "", 1)
	service := &storyboardHTTPService{result: validStoryboardHTTPResult(false)}
	recorder := httptest.NewRecorder()
	newStoryboardHTTPRouter(t, newStoryboardHTTPVerifier(), service).ServeHTTP(recorder, newStoryboardHTTPRequest([]byte(body)))
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("省略可选时长 status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	want := `{"schema_version":"plan_storyboard.preview.intent.v1","planning_instruction":"规划三段式故事板"}`
	if string(service.request.IntentJSON) != want {
		t.Fatalf("optional Tool Intent canonical 漂移: got=%s want=%s", service.request.IntentJSON, want)
	}
}

// TestPlanStoryboardPreviewPOSTRejectsNonCanonicalPathAndHeaders 验证非法协议边界不会消费一次性身份断言。
func TestPlanStoryboardPreviewPOSTRejectsNonCanonicalPathAndHeaders(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		mutate func(*http.Request)
	}{
		{name: "query forbidden", path: storyboardHTTPTestPath + "?debug=1"},
		{name: "uppercase session forbidden", path: strings.Replace(storyboardHTTPTestPath, "8000", "800A", 1)},
		{name: "escaped path forbidden", path: strings.Replace(storyboardHTTPTestPath, "/019f", "/%30%31%39f", 1)},
		{name: "missing content type", path: storyboardHTTPTestPath, mutate: func(request *http.Request) {
			request.Header.Del("Content-Type")
		}},
		{name: "content type parameter forbidden", path: storyboardHTTPTestPath, mutate: func(request *http.Request) {
			request.Header.Set("Content-Type", "application/json; charset=utf-8")
		}},
		{name: "duplicate content type", path: storyboardHTTPTestPath, mutate: func(request *http.Request) {
			request.Header["Content-Type"] = []string{"application/json", "application/json"}
		}},
		{name: "missing idempotency key", path: storyboardHTTPTestPath, mutate: func(request *http.Request) {
			request.Header.Del("Idempotency-Key")
		}},
		{name: "invalid idempotency key", path: storyboardHTTPTestPath, mutate: func(request *http.Request) {
			request.Header.Set("Idempotency-Key", "not-a-uuid")
		}},
		{name: "duplicate idempotency key", path: storyboardHTTPTestPath, mutate: func(request *http.Request) {
			request.Header["Idempotency-Key"] = []string{storyboardHTTPTestIdempotencyKey, storyboardHTTPTestIdempotencyKey}
		}},
	}
	for _, fixture := range tests {
		t.Run(fixture.name, func(t *testing.T) {
			verifier := newStoryboardHTTPVerifier()
			service := &storyboardHTTPService{}
			request := newStoryboardHTTPRequest([]byte(storyboardHTTPTestBody))
			request.URL = mustStoryboardHTTPURL(t, fixture.path)
			request.RequestURI = request.URL.RequestURI()
			if fixture.mutate != nil {
				fixture.mutate(request)
			}
			recorder := httptest.NewRecorder()
			newStoryboardHTTPRouter(t, verifier, service).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want=400 body=%s", recorder.Code, recorder.Body.String())
			}
			if verifier.calls != 0 || service.calls != 0 {
				t.Fatalf("非法协议边界仍消费身份或入队: verifier=%d service=%d", verifier.calls, service.calls)
			}
		})
	}
}

// TestPlanStoryboardPreviewPOSTRejectsStrictBodyViolations 验证完整 exact-set、UTF-8、Unicode 与大小门禁。
func TestPlanStoryboardPreviewPOSTRejectsStrictBodyViolations(t *testing.T) {
	longInstruction := strings.Repeat("界", 1001)
	tests := map[string][]byte{
		"empty":                   {},
		"malformed":               []byte(`{`),
		"trailing value":          []byte(storyboardHTTPTestBody + ` {}`),
		"unknown outer field":     []byte(strings.Replace(storyboardHTTPTestBody, `,"tool_intent":`, `,"extra":true,"tool_intent":`, 1)),
		"duplicate outer field":   []byte(strings.Replace(storyboardHTTPTestBody, `"schema_version":"plan_storyboard.preview.enqueue-request.v1"`, `"schema_version":"plan_storyboard.preview.enqueue-request.v1","schema_version":"plan_storyboard.preview.enqueue-request.v1"`, 1)),
		"unknown trusted ref":     []byte(strings.Replace(storyboardHTTPTestBody, `,"version":1`, `,"version":1,"project_id":"`+storyboardHTTPTestProjectID+`"`, 1)),
		"duplicate trusted ref":   []byte(strings.Replace(storyboardHTTPTestBody, `"version":1`, `"version":1,"version":1`, 1)),
		"null trusted ref":        []byte(strings.Replace(storyboardHTTPTestBody, `{"id":"`+storyboardHTTPTestCreationSpecID+`","version":1,"content_digest":"`+storyboardHTTPTestDigest+`"}`, `null`, 1)),
		"non UUIDv7 ref":          []byte(strings.Replace(storyboardHTTPTestBody, storyboardHTTPTestCreationSpecID, "019f68e8-0128-4000-8000-000000000128", 1)),
		"uppercase UUID ref":      []byte(strings.Replace(storyboardHTTPTestBody, "8000-000000000128", "800A-000000000128", 1)),
		"version conflict":        []byte(strings.Replace(storyboardHTTPTestBody, `"version":1`, `"version":2`, 1)),
		"uppercase digest":        []byte(strings.Replace(storyboardHTTPTestBody, storyboardHTTPTestDigest, strings.ToUpper(storyboardHTTPTestDigest), 1)),
		"unknown Tool Intent":     []byte(strings.Replace(storyboardHTTPTestBody, `,"target_duration_seconds":60`, `,"target_duration_seconds":60,"asset_id":"`+storyboardHTTPTestCreationSpecID+`"`, 1)),
		"duplicate Tool Intent":   []byte(strings.Replace(storyboardHTTPTestBody, `"planning_instruction":"规划三段式故事板"`, `"planning_instruction":"规划三段式故事板","planning_instruction":"第二语义"`, 1)),
		"null optional duration":  []byte(strings.Replace(storyboardHTTPTestBody, `"target_duration_seconds":60`, `"target_duration_seconds":null`, 1)),
		"duration below boundary": []byte(strings.Replace(storyboardHTTPTestBody, `"target_duration_seconds":60`, `"target_duration_seconds":4`, 1)),
		"duration above boundary": []byte(strings.Replace(storyboardHTTPTestBody, `"target_duration_seconds":60`, `"target_duration_seconds":601`, 1)),
		"duration fraction":       []byte(strings.Replace(storyboardHTTPTestBody, `"target_duration_seconds":60`, `"target_duration_seconds":60.5`, 1)),
		"non NFC instruction":     []byte(strings.Replace(storyboardHTTPTestBody, "规划三段式故事板", "e\u0301", 1)),
		"boundary whitespace":     []byte(strings.Replace(storyboardHTTPTestBody, "规划三段式故事板", " 规划三段式故事板", 1)),
		"control character":       []byte(strings.Replace(storyboardHTTPTestBody, "规划三段式故事板", `规划\u0000故事板`, 1)),
		"isolated surrogate":      []byte(strings.Replace(storyboardHTTPTestBody, "规划三段式故事板", `\ud800`, 1)),
		"instruction too long":    []byte(strings.Replace(storyboardHTTPTestBody, "规划三段式故事板", longInstruction, 1)),
		"invalid raw UTF-8":       []byte{0xff},
		"body over byte budget":   bytes.Repeat([]byte("x"), planStoryboardPreviewMaxBodyBytes+1),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			verifier := newStoryboardHTTPVerifier()
			service := &storyboardHTTPService{}
			recorder := httptest.NewRecorder()
			newStoryboardHTTPRouter(t, verifier, service).ServeHTTP(recorder, newStoryboardHTTPRequest(body))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want=400 body=%s", recorder.Code, recorder.Body.String())
			}
			if verifier.calls != 1 || service.calls != 0 {
				t.Fatalf("Body 校验顺序错误: verifier=%d service=%d", verifier.calls, service.calls)
			}
			assertStoryboardHTTPError(t, recorder, errorCodeInvalidArgument, false)
			if strings.Contains(recorder.Body.String(), storyboardHTTPTestCreationSpecID) ||
				strings.Contains(recorder.Body.String(), "规划三段式故事板") {
				t.Fatalf("错误响应泄漏正文或可信引用: %s", recorder.Body.String())
			}
		})
	}
}

// TestPlanStoryboardPreviewPOSTMapsIdentityOwnerAndServiceErrors 验证 400/404/409/503 与身份失败关闭映射。
func TestPlanStoryboardPreviewPOSTMapsIdentityOwnerAndServiceErrors(t *testing.T) {
	tests := []struct {
		name       string
		verifyErr  error
		mutate     func(*httpidentity.Claims)
		serviceErr error
		wantStatus int
		wantCode   string
		wantRetry  bool
	}{
		{name: "assertion invalid", verifyErr: httpidentity.ErrInvalid, wantStatus: http.StatusUnauthorized, wantCode: errorCodeInternalIdentityInvalid},
		{name: "assertion unavailable", verifyErr: httpidentity.ErrUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodeIdentityAssertionUnavailable, wantRetry: true},
		{name: "assertion session mismatch", mutate: func(claims *httpidentity.Claims) { claims.AgentSessionID = storyboardHTTPTestOtherSessionID }, wantStatus: http.StatusUnauthorized, wantCode: errorCodeInternalIdentityInvalid},
		{name: "assertion scope mismatch", mutate: func(claims *httpidentity.Claims) { claims.Scope = "analyze_materials.preview.write" }, wantStatus: http.StatusUnauthorized, wantCode: errorCodeInternalIdentityInvalid},
		{name: "service invalid input", serviceErr: planstoryboardruntime.ErrInvalidInput, wantStatus: http.StatusBadRequest, wantCode: errorCodeInvalidArgument},
		{name: "owner mismatch is not found", mutate: func(claims *httpidentity.Claims) { claims.PrincipalUserID = storyboardHTTPTestOtherUserID }, serviceErr: planstoryboardruntime.ErrNotFound, wantStatus: http.StatusNotFound, wantCode: errorCodeSessionNotFound},
		{name: "idempotency conflict", serviceErr: planstoryboardruntime.ErrIdempotencyConflict, wantStatus: http.StatusConflict, wantCode: errorCodeIdempotencyConflict},
		{name: "session lane blocked", serviceErr: planstoryboardruntime.ErrSessionLaneBlocked, wantStatus: http.StatusConflict, wantCode: errorCodeSessionLaneBlocked},
		{name: "persistence unavailable", serviceErr: planstoryboardruntime.ErrPersistence, wantStatus: http.StatusServiceUnavailable, wantCode: errorCodePersistenceUnavailable, wantRetry: true},
		{name: "unknown internal error", serviceErr: errors.New("SQL and refs must not escape"), wantStatus: http.StatusServiceUnavailable, wantCode: errorCodePersistenceUnavailable, wantRetry: true},
	}
	for _, fixture := range tests {
		t.Run(fixture.name, func(t *testing.T) {
			verifier := newStoryboardHTTPVerifier()
			verifier.err = fixture.verifyErr
			if fixture.mutate != nil {
				fixture.mutate(&verifier.claims)
			}
			service := &storyboardHTTPService{result: validStoryboardHTTPResult(false), err: fixture.serviceErr}
			recorder := httptest.NewRecorder()
			newStoryboardHTTPRouter(t, verifier, service).ServeHTTP(recorder, newStoryboardHTTPRequest([]byte(storyboardHTTPTestBody)))
			if recorder.Code != fixture.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, fixture.wantStatus, recorder.Body.String())
			}
			assertStoryboardHTTPError(t, recorder, fixture.wantCode, fixture.wantRetry)
			if strings.Contains(recorder.Body.String(), "SQL and refs") {
				t.Fatalf("内部错误原文泄漏: %s", recorder.Body.String())
			}
			if fixture.verifyErr != nil || fixture.name == "assertion session mismatch" || fixture.name == "assertion scope mismatch" {
				if service.calls != 0 {
					t.Fatalf("无效 assertion 仍调用 Service: calls=%d", service.calls)
				}
			}
		})
	}
}

// TestPlanStoryboardPreviewPOSTRejectsInvalidRuntimeReceipt 验证漂移 Schema、状态、ID 与重复身份均映射为 503。
func TestPlanStoryboardPreviewPOSTRejectsInvalidRuntimeReceipt(t *testing.T) {
	tests := map[string]planstoryboardruntime.EnqueueResponse{
		"wrong schema": func() planstoryboardruntime.EnqueueResponse {
			value := validStoryboardHTTPResult(false)
			value.SchemaVersion = "plan_storyboard.preview.enqueue.v2"
			return value
		}(),
		"wrong status": func() planstoryboardruntime.EnqueueResponse {
			value := validStoryboardHTTPResult(false)
			value.Status = "completed"
			return value
		}(),
		"invalid input ID": func() planstoryboardruntime.EnqueueResponse {
			value := validStoryboardHTTPResult(false)
			value.InputID = "not-a-uuid"
			return value
		}(),
		"duplicated identity": func() planstoryboardruntime.EnqueueResponse {
			value := validStoryboardHTTPResult(false)
			value.RunID = value.TurnID
			return value
		}(),
	}
	for name, result := range tests {
		t.Run(name, func(t *testing.T) {
			service := &storyboardHTTPService{result: result}
			recorder := httptest.NewRecorder()
			newStoryboardHTTPRouter(t, newStoryboardHTTPVerifier(), service).ServeHTTP(recorder, newStoryboardHTTPRequest([]byte(storyboardHTTPTestBody)))
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d want=503 body=%s", recorder.Code, recorder.Body.String())
			}
			assertStoryboardHTTPError(t, recorder, errorCodePersistenceUnavailable, true)
		})
	}
}

// newStoryboardHTTPVerifier 返回与 Storyboard Preview scope 精确绑定的可信断言。
func newStoryboardHTTPVerifier() *storyboardHTTPVerifier {
	issuedAt := time.Now().UTC().Add(-time.Second)
	return &storyboardHTTPVerifier{claims: httpidentity.Claims{
		RequestID: storyboardHTTPTestRequestID, PrincipalUserID: storyboardHTTPTestUserID,
		WebSessionID: storyboardHTTPTestWebSessionID, WebSessionVersion: 3,
		ProjectID: storyboardHTTPTestProjectID, AgentSessionID: storyboardHTTPTestSessionID,
		Scope: httpidentity.ScopePlanStoryboardPreviewWrite, IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(time.Hour),
	}}
}

// newStoryboardHTTPRouter 构造只注册内部 Storyboard Preview POST 的测试 Router。
func newStoryboardHTTPRouter(t *testing.T, verifier *storyboardHTTPVerifier, service *storyboardHTTPService) http.Handler {
	t.Helper()
	handler, err := NewPlanStoryboardPreviewHandler(verifier, service, storyboardHTTPIDs{}, "local-agent-content-v1")
	if err != nil {
		t.Fatalf("创建 Plan Storyboard Preview Handler 失败: %v", err)
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	handler.Register(router)
	return router
}

// newStoryboardHTTPRequest 构造带 exact Content-Type 与规范 UUIDv7 幂等键的内部 POST。
func newStoryboardHTTPRequest(body []byte) *http.Request {
	request := httptest.NewRequest(http.MethodPost, storyboardHTTPTestPath, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", storyboardHTTPTestIdempotencyKey)
	return request
}

// mustStoryboardHTTPURL 解析表驱动测试路径，失败时立即终止当前用例。
func mustStoryboardHTTPURL(t *testing.T, target string) *url.URL {
	t.Helper()
	parsed, err := url.ParseRequestURI(target)
	if err != nil {
		t.Fatalf("解析测试 URL 失败: %v", err)
	}
	return parsed
}

// validStoryboardHTTPResult 返回首次受理与同义重放共用的稳定执行身份。
func validStoryboardHTTPResult(replayed bool) planstoryboardruntime.EnqueueResponse {
	return planstoryboardruntime.EnqueueResponse{
		SchemaVersion: planstoryboardruntime.EnqueueResponseSchemaVersion,
		Status:        planstoryboardruntime.EnqueuePendingStatus,
		InputID:       storyboardHTTPTestInputID,
		TurnID:        storyboardHTTPTestTurnID,
		RunID:         storyboardHTTPTestRunID,
		ToolCallID:    storyboardHTTPTestToolCallID,
		Replayed:      replayed,
	}
}

// assertStoryboardHTTPError 校验 no-store 稳定错误 Envelope，且不依赖内部错误原文。
func assertStoryboardHTTPError(t *testing.T, recorder *httptest.ResponseRecorder, code string, retryable bool) {
	t.Helper()
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("错误响应缺少 no-store: headers=%v", recorder.Header())
	}
	var response ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析错误 Envelope 失败: %v body=%s", err, recorder.Body.String())
	}
	if response.Error.Code != code || response.Error.Retryable != retryable ||
		response.Error.RequestID == "" || response.Error.Message == "" {
		t.Fatalf("错误 Envelope=%+v want code=%q retryable=%t", response, code, retryable)
	}
}

var _ IdentityVerifier = (*storyboardHTTPVerifier)(nil)
var _ PlanStoryboardPreviewService = (*storyboardHTTPService)(nil)
