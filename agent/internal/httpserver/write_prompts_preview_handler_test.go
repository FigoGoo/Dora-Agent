package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/writepromptsruntime"
	"github.com/gin-gonic/gin"
)

const (
	writeHTTPTestRequestID      = "019f68e8-0220-7000-8000-000000000220"
	writeHTTPTestUserID         = "019f68e8-0221-7000-8000-000000000221"
	writeHTTPTestProjectID      = "019f68e8-0222-7000-8000-000000000222"
	writeHTTPTestSessionID      = "019f68e8-0223-7000-8000-000000000223"
	writeHTTPTestWebSessionID   = "019f68e8-0224-7000-8000-000000000224"
	writeHTTPTestIdempotencyKey = "019f68e8-0225-7000-8000-000000000225"
	writeHTTPTestStoryboardID   = "019f68e8-0226-7000-8000-000000000226"
	writeHTTPTestInputID        = "019f68e8-0227-7000-8000-000000000227"
	writeHTTPTestTurnID         = "019f68e8-0228-7000-8000-000000000228"
	writeHTTPTestRunID          = "019f68e8-0229-7000-8000-000000000229"
	writeHTTPTestToolCallID     = "019f68e8-0230-7000-8000-000000000230"
	writeHTTPTestErrorID        = "019f68e8-0231-7000-8000-000000000231"
	writeHTTPTestDigest         = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	writeHTTPTestPath           = "/internal/v1/workspaces/sessions/" + writeHTTPTestSessionID + "/write-prompts-previews"
	writeHTTPTestBody           = `{"schema_version":"write_prompts.preview.enqueue-request.v1","storyboard_preview_ref":{"id":"` + writeHTTPTestStoryboardID + `","version":1,"content_digest":"` + writeHTTPTestDigest + `"},"tool_intent":{"schema_version":"write_prompts.preview.intent.v1","writing_instruction":"为每个镜头编写可执行提示词","output_language":"zh-CN"}}`
)

type writeHTTPVerifier struct {
	request httpidentity.Request
	claims  httpidentity.Claims
	calls   int
}

func (verifier *writeHTTPVerifier) Verify(_ context.Context, request httpidentity.Request) (httpidentity.Claims, error) {
	verifier.calls++
	verifier.request = request
	return verifier.claims, nil
}

type writeHTTPService struct {
	request writepromptsruntime.EnqueueRequest
	result  writepromptsruntime.EnqueueResponse
	calls   int
}

func (service *writeHTTPService) Enqueue(_ context.Context, request writepromptsruntime.EnqueueRequest) (writepromptsruntime.EnqueueResponse, error) {
	service.calls++
	service.request = request
	return service.result, nil
}

type writeHTTPIDs struct{}

func (writeHTTPIDs) New() (string, error) { return writeHTTPTestErrorID, nil }

func TestWritePromptsPreviewPOSTReturnsExactAcceptedDTO(t *testing.T) {
	verifier := newWriteHTTPVerifier()
	service := &writeHTTPService{result: writepromptsruntime.EnqueueResponse{
		SchemaVersion: writepromptsruntime.EnqueueResponseSchemaVersion, Status: writepromptsruntime.EnqueuePendingStatus,
		InputID: writeHTTPTestInputID, TurnID: writeHTTPTestTurnID, RunID: writeHTTPTestRunID,
		ToolCallID: writeHTTPTestToolCallID, Replayed: true,
	}}
	recorder := httptest.NewRecorder()
	newWriteHTTPRouter(t, verifier, service).ServeHTTP(recorder, newWriteHTTPRequest(writeHTTPTestBody))
	if recorder.Code != http.StatusAccepted || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("202 响应错误: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	want := `{"schema_version":"write_prompts.preview.enqueue.v1","request_id":"` + writeHTTPTestRequestID + `","session_id":"` + writeHTTPTestSessionID + `","input_id":"` + writeHTTPTestInputID + `","turn_id":"` + writeHTTPTestTurnID + `","run_id":"` + writeHTTPTestRunID + `","tool_call_id":"` + writeHTTPTestToolCallID + `","status":"pending","replayed":true}`
	if strings.TrimSpace(recorder.Body.String()) != want {
		t.Fatalf("202 exact-set 漂移:\n got: %s\nwant: %s", recorder.Body.String(), want)
	}
	if verifier.calls != 1 || verifier.request.CanonicalTarget != writeHTTPTestPath ||
		verifier.request.Scope != httpidentity.ScopeWritePromptsPreviewWrite {
		t.Fatalf("身份 Scope/Target 映射错误: calls=%d request=%+v", verifier.calls, verifier.request)
	}
	wantIntent := `{"schema_version":"write_prompts.preview.intent.v1","writing_instruction":"为每个镜头编写可执行提示词","output_language":"zh-CN"}`
	if service.calls != 1 || service.request.SessionID != writeHTTPTestSessionID ||
		service.request.UserID != writeHTTPTestUserID || service.request.ProjectID != writeHTTPTestProjectID ||
		service.request.IdempotencyKey != writeHTTPTestIdempotencyKey ||
		service.request.StoryboardPreviewRef.ID != writeHTTPTestStoryboardID ||
		service.request.StoryboardPreviewRef.Version != 1 ||
		service.request.StoryboardPreviewRef.ContentDigest != writeHTTPTestDigest ||
		string(service.request.IntentJSON) != wantIntent ||
		service.request.AccessScopeRef != httpidentity.ScopeWritePromptsPreviewWrite ||
		len(service.request.AccessScopeDigest) != 64 || service.request.IntentKeyVersion != "local-agent-content-v1" {
		t.Fatalf("Runtime 请求映射错误: calls=%d request=%+v", service.calls, service.request)
	}
}

func TestWritePromptsPreviewPOSTRejectsWrongEnvelopeAndIntent(t *testing.T) {
	tests := map[string]string{
		"old field":           strings.Replace(writeHTTPTestBody, "storyboard_preview_ref", "creation_spec_ref", 1),
		"duplicate outer":     strings.Replace(writeHTTPTestBody, `"schema_version":"write_prompts.preview.enqueue-request.v1"`, `"schema_version":"write_prompts.preview.enqueue-request.v1","schema_version":"write_prompts.preview.enqueue-request.v1"`, 1),
		"null ref":            strings.Replace(writeHTTPTestBody, `{"id":"`+writeHTTPTestStoryboardID+`","version":1,"content_digest":"`+writeHTTPTestDigest+`"}`, "null", 1),
		"wrong ref version":   strings.Replace(writeHTTPTestBody, `"version":1`, `"version":2`, 1),
		"uppercase digest":    strings.Replace(writeHTTPTestBody, writeHTTPTestDigest, strings.ToUpper(writeHTTPTestDigest), 1),
		"unknown intent":      strings.Replace(writeHTTPTestBody, `"output_language":"zh-CN"`, `"output_language":"zh-CN","project_id":"`+writeHTTPTestProjectID+`"`, 1),
		"invalid language":    strings.Replace(writeHTTPTestBody, `"output_language":"zh-CN"`, `"output_language":"fr-FR"`, 1),
		"non NFC instruction": strings.Replace(writeHTTPTestBody, "为每个镜头编写可执行提示词", "e\u0301", 1),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			verifier := newWriteHTTPVerifier()
			service := &writeHTTPService{}
			recorder := httptest.NewRecorder()
			newWriteHTTPRouter(t, verifier, service).ServeHTTP(recorder, newWriteHTTPRequest(body))
			if recorder.Code != http.StatusBadRequest || verifier.calls != 1 || service.calls != 0 {
				t.Fatalf("非法正文未失败关闭: status=%d verifier=%d service=%d body=%s", recorder.Code, verifier.calls, service.calls, recorder.Body.String())
			}
		})
	}
}

func TestWritePromptsPreviewPOSTRejectsNonCanonicalPathBeforeIdentity(t *testing.T) {
	verifier := newWriteHTTPVerifier()
	service := &writeHTTPService{}
	request := newWriteHTTPRequest(writeHTTPTestBody)
	request.URL.Path = strings.Replace(writeHTTPTestPath, "/write-prompts-previews", "/plan-prompt-previews", 1)
	request.RequestURI = request.URL.RequestURI()
	recorder := httptest.NewRecorder()
	newWriteHTTPRouter(t, verifier, service).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || verifier.calls != 0 || service.calls != 0 {
		t.Fatalf("非 canonical 路径仍消费身份或入队: status=%d verifier=%d service=%d", recorder.Code, verifier.calls, service.calls)
	}
}

func newWriteHTTPVerifier() *writeHTTPVerifier {
	issuedAt := time.Now().UTC().Add(-time.Minute)
	return &writeHTTPVerifier{claims: httpidentity.Claims{
		RequestID: writeHTTPTestRequestID, PrincipalUserID: writeHTTPTestUserID,
		ProjectID: writeHTTPTestProjectID, AgentSessionID: writeHTTPTestSessionID,
		WebSessionID: writeHTTPTestWebSessionID, WebSessionVersion: 1,
		Scope: httpidentity.ScopeWritePromptsPreviewWrite, IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(2 * time.Minute),
	}}
}

func newWriteHTTPRouter(t *testing.T, verifier *writeHTTPVerifier, service *writeHTTPService) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	handler, err := NewWritePromptsPreviewHandler(verifier, service, writeHTTPIDs{}, "local-agent-content-v1")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	handler.Register(router)
	return router
}

func newWriteHTTPRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, writeHTTPTestPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", writeHTTPTestIdempotencyKey)
	return request
}
