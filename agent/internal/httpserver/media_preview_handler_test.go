package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreviewruntime"
	"github.com/gin-gonic/gin"
)

const (
	mediaHTTPRequestID      = "019f68e8-0300-7000-8000-000000000300"
	mediaHTTPUserID         = "019f68e8-0301-7000-8000-000000000301"
	mediaHTTPProjectID      = "019f68e8-0302-7000-8000-000000000302"
	mediaHTTPSessionID      = "019f68e8-0303-7000-8000-000000000303"
	mediaHTTPWebSessionID   = "019f68e8-0304-7000-8000-000000000304"
	mediaHTTPIdempotencyKey = "019f68e8-0305-7000-8000-000000000305"
	mediaHTTPSourceID       = "019f68e8-0306-7000-8000-000000000306"
	mediaHTTPInputID        = "019f68e8-0307-7000-8000-000000000307"
	mediaHTTPTurnID         = "019f68e8-0308-7000-8000-000000000308"
	mediaHTTPRunID          = "019f68e8-0309-7000-8000-000000000309"
	mediaHTTPToolCallID     = "019f68e8-0310-7000-8000-000000000310"
	mediaHTTPErrorID        = "019f68e8-0311-7000-8000-000000000311"
	mediaHTTPDigest         = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

type mediaHTTPVerifier struct {
	request httpidentity.Request
	claims  httpidentity.Claims
	calls   int
}

func (verifier *mediaHTTPVerifier) Verify(_ context.Context, request httpidentity.Request) (httpidentity.Claims, error) {
	verifier.calls++
	verifier.request = request
	return verifier.claims, nil
}

type mediaHTTPService struct {
	request mediapreviewruntime.EnqueueRequest
	result  mediapreviewruntime.EnqueueResponse
	calls   int
}

func (service *mediaHTTPService) Enqueue(_ context.Context, request mediapreviewruntime.EnqueueRequest) (mediapreviewruntime.EnqueueResponse, error) {
	service.calls++
	service.request = request
	return service.result, nil
}

type mediaHTTPIDs struct{}

func (mediaHTTPIDs) New() (string, error) { return mediaHTTPErrorID, nil }

func TestMediaPreviewPOSTReturnsExactAcceptedDTO(t *testing.T) {
	fixtures := []struct {
		name       string
		toolKey    string
		scope      string
		pathSuffix string
		body       string
		wantIntent string
	}{
		{
			name: "generate media", toolKey: mediapreview.GenerateMediaToolKey,
			scope: httpidentity.ScopeGenerateMediaPreviewWrite, pathSuffix: "/generate-media-previews",
			body:       `{"schema_version":"generate_media.preview.enqueue-request.v1","prompt_preview_ref":{"id":"` + mediaHTTPSourceID + `","version":1,"content_digest":"` + mediaHTTPDigest + `"},"tool_intent":{"schema_version":"generate_media.intent.v3preview1","prompt_preview_id":"` + mediaHTTPSourceID + `","expected_prompt_version":1,"expected_prompt_content_digest":"` + mediaHTTPDigest + `","target_local_key":"slot_1","output_profile":"png_640x360.v1"}}`,
			wantIntent: `{"schema_version":"generate_media.intent.v3preview1","prompt_preview_id":"` + mediaHTTPSourceID + `","expected_prompt_version":1,"expected_prompt_content_digest":"` + mediaHTTPDigest + `","target_local_key":"slot_1","output_profile":"png_640x360.v1"}`,
		},
		{
			name: "assemble output", toolKey: mediapreview.AssembleOutputToolKey,
			scope: httpidentity.ScopeAssembleOutputPreviewWrite, pathSuffix: "/assemble-output-previews",
			body:       `{"schema_version":"assemble_output.preview.enqueue-request.v1","source_asset_ref":{"id":"` + mediaHTTPSourceID + `","version":1,"content_digest":"` + mediaHTTPDigest + `"},"tool_intent":{"schema_version":"assemble_output.intent.v3preview1","source_asset_id":"` + mediaHTTPSourceID + `","expected_source_version":1,"expected_source_content_digest":"` + mediaHTTPDigest + `","output_profile":"mp4_h264_640x360_2s.v1"}}`,
			wantIntent: `{"schema_version":"assemble_output.intent.v3preview1","source_asset_id":"` + mediaHTTPSourceID + `","expected_source_version":1,"expected_source_content_digest":"` + mediaHTTPDigest + `","output_profile":"mp4_h264_640x360_2s.v1"}`,
		},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := "/internal/v1/workspaces/sessions/" + mediaHTTPSessionID + fixture.pathSuffix
			verifier := newMediaHTTPVerifier(fixture.scope)
			service := &mediaHTTPService{result: mediapreviewruntime.EnqueueResponse{
				SchemaVersion: mediapreviewruntime.EnqueueSchemaVersion, Status: mediapreviewruntime.PendingStatus,
				InputID: mediaHTTPInputID, TurnID: mediaHTTPTurnID, RunID: mediaHTTPRunID,
				ToolCallID: mediaHTTPToolCallID, ToolKey: fixture.toolKey, Replayed: true,
			}}
			recorder := httptest.NewRecorder()
			newMediaHTTPRouter(t, fixture.toolKey, verifier, service).ServeHTTP(recorder, newMediaHTTPRequest(path, fixture.body))
			if recorder.Code != http.StatusAccepted || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("202 response mismatch: status=%d headers=%v body=%s", recorder.Code, recorder.Header(), recorder.Body.String())
			}
			want := `{"schema_version":"media_preview.enqueue.v1","request_id":"` + mediaHTTPRequestID + `","session_id":"` + mediaHTTPSessionID + `","input_id":"` + mediaHTTPInputID + `","turn_id":"` + mediaHTTPTurnID + `","run_id":"` + mediaHTTPRunID + `","tool_call_id":"` + mediaHTTPToolCallID + `","tool_key":"` + fixture.toolKey + `","status":"pending","replayed":true}`
			if strings.TrimSpace(recorder.Body.String()) != want {
				t.Fatalf("202 exact-set drift:\n got: %s\nwant: %s", recorder.Body.String(), want)
			}
			if verifier.calls != 1 || verifier.request.CanonicalTarget != path || verifier.request.Scope != fixture.scope {
				t.Fatalf("identity target/scope mismatch: calls=%d request=%+v", verifier.calls, verifier.request)
			}
			if service.calls != 1 || service.request.RequestID != mediaHTTPRequestID ||
				service.request.SessionID != mediaHTTPSessionID || service.request.UserID != mediaHTTPUserID ||
				service.request.ProjectID != mediaHTTPProjectID || service.request.IdempotencyKey != mediaHTTPIdempotencyKey ||
				service.request.ToolKey != fixture.toolKey || string(service.request.IntentJSON) != fixture.wantIntent {
				t.Fatalf("runtime request mismatch: calls=%d request=%+v", service.calls, service.request)
			}
		})
	}
}

func TestMediaPreviewPOSTRejectsOuterIntentMismatch(t *testing.T) {
	path := "/internal/v1/workspaces/sessions/" + mediaHTTPSessionID + "/generate-media-previews"
	body := `{"schema_version":"generate_media.preview.enqueue-request.v1","prompt_preview_ref":{"id":"` + mediaHTTPSourceID + `","version":1,"content_digest":"` + mediaHTTPDigest + `"},"tool_intent":{"schema_version":"generate_media.intent.v3preview1","prompt_preview_id":"` + mediaHTTPInputID + `","expected_prompt_version":1,"expected_prompt_content_digest":"` + mediaHTTPDigest + `","target_local_key":"slot_1","output_profile":"png_640x360.v1"}}`
	verifier := newMediaHTTPVerifier(httpidentity.ScopeGenerateMediaPreviewWrite)
	service := &mediaHTTPService{}
	recorder := httptest.NewRecorder()
	newMediaHTTPRouter(t, mediapreview.GenerateMediaToolKey, verifier, service).ServeHTTP(recorder, newMediaHTTPRequest(path, body))
	if recorder.Code != http.StatusBadRequest || verifier.calls != 1 || service.calls != 0 {
		t.Fatalf("outer/intent mismatch did not fail closed: status=%d verifier=%d service=%d body=%s", recorder.Code, verifier.calls, service.calls, recorder.Body.String())
	}
}

func newMediaHTTPVerifier(scope string) *mediaHTTPVerifier {
	issuedAt := time.Now().UTC().Add(-time.Minute)
	return &mediaHTTPVerifier{claims: httpidentity.Claims{
		RequestID: mediaHTTPRequestID, PrincipalUserID: mediaHTTPUserID, ProjectID: mediaHTTPProjectID,
		AgentSessionID: mediaHTTPSessionID, WebSessionID: mediaHTTPWebSessionID, WebSessionVersion: 1,
		Scope: scope, IssuedAt: issuedAt, ExpiresAt: issuedAt.Add(2 * time.Minute),
	}}
}

func newMediaHTTPRouter(t *testing.T, toolKey string, verifier *mediaHTTPVerifier, service *mediaHTTPService) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
	if toolKey == mediapreview.GenerateMediaToolKey {
		handler, err := NewGenerateMediaPreviewHandler(verifier, service, mediaHTTPIDs{})
		if err != nil {
			t.Fatal(err)
		}
		handler.Register(router)
	} else {
		handler, err := NewAssembleOutputPreviewHandler(verifier, service, mediaHTTPIDs{})
		if err != nil {
			t.Fatal(err)
		}
		handler.Register(router)
	}
	return router
}

func newMediaHTTPRequest(path, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", mediaHTTPIdempotencyKey)
	return request
}
