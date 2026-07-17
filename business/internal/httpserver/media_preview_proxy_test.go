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
)

func TestMediaPreviewBFFSignsCanonicalTargetsAndPreservesStrictBody(t *testing.T) {
	tests := []struct {
		name       string
		publicPath string
		internal   string
		scope      string
		toolKey    string
		body       string
	}{
		{
			name: "generate", publicPath: "generate-media-previews", internal: "generate-media-previews",
			scope: agentidentity.ScopeGenerateMediaPreviewWrite, toolKey: "generate_media",
			body: `{"schema_version":"generate_media.preview.enqueue-request.v1","prompt_preview_ref":{"id":"` + agentProxyOtherID + `","version":1,"content_digest":"` + strings.Repeat("a", 64) + `"},"tool_intent":{"schema_version":"generate_media.intent.v3preview1","prompt_preview_id":"` + agentProxyOtherID + `","expected_prompt_version":1,"expected_prompt_content_digest":"` + strings.Repeat("a", 64) + `","target_local_key":"slot_1","output_profile":"png_640x360.v1"}}`,
		},
		{
			name: "assemble", publicPath: "assemble-output-previews", internal: "assemble-output-previews",
			scope: agentidentity.ScopeAssembleOutputPreviewWrite, toolKey: "assemble_output",
			body: `{"schema_version":"assemble_output.preview.enqueue-request.v1","source_asset_ref":{"id":"` + agentProxyOtherID + `","version":1,"content_digest":"` + strings.Repeat("b", 64) + `"},"tool_intent":{"schema_version":"assemble_output.intent.v3preview1","source_asset_id":"` + agentProxyOtherID + `","expected_source_version":1,"expected_source_content_digest":"` + strings.Repeat("b", 64) + `","output_profile":"mp4_h264_640x360_2s.v1"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var upstream *http.Request
			var upstreamBody string
			client := agentProxyClientFunc(func(request *http.Request) (*http.Response, error) {
				upstream = request
				body, _ := io.ReadAll(request.Body)
				upstreamBody = string(body)
				return &http.Response{
					StatusCode: http.StatusAccepted,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body: io.NopCloser(strings.NewReader(`{"schema_version":"media_preview.enqueue.v1","request_id":"` + agentProxyRequestID +
						`","session_id":"` + agentProxySessionID + `","input_id":"` + agentProxyOtherID +
						`","turn_id":"` + agentProxyOtherID + `","run_id":"` + agentProxyOtherID +
						`","tool_call_id":"` + agentProxyOtherID + `","tool_key":"` + test.toolKey + `","status":"pending","replayed":false}`)),
				}, nil
			})
			access := &agentProxyAccessStub{result: project.AgentSessionAccess{ProjectID: agentProxyProjectID, AgentSessionID: agentProxySessionID}}
			signer := &agentProxySignerStub{}
			handler, err := NewAgentProxyHandler(access, signer, agentProxyIDs{}, client, config.AgentHTTPConfig{
				BaseURL: "http://agent.internal", RequestTimeout: time.Second, MediaRuntimeEnabled: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost,
				"/api/v1/agent/sessions/"+agentProxySessionID+"/"+test.publicPath, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", agentProxyOtherID)
			recorder := serveCreationSpecPreviewProxy(handler, request)
			if recorder.Code != http.StatusAccepted {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			wantTarget := "/internal/v1/workspaces/sessions/" + agentProxySessionID + "/" + test.internal
			if upstream == nil || upstream.URL.Path != wantTarget || upstream.Method != http.MethodPost ||
				upstream.Header.Get("Idempotency-Key") != agentProxyOtherID || upstreamBody != test.body {
				t.Fatalf("upstream=%v body=%s", upstream, upstreamBody)
			}
			if signer.identity.Method != http.MethodPost || signer.identity.CanonicalTarget != wantTarget || signer.identity.Scope != test.scope {
				t.Fatalf("signed identity=%+v", signer.identity)
			}
		})
	}
}

func TestMediaPreviewBFFDisabledAndMismatchedRefsFailClosed(t *testing.T) {
	clientCalls := 0
	client := agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		clientCalls++
		return nil, nil
	})
	access := &agentProxyAccessStub{result: project.AgentSessionAccess{ProjectID: agentProxyProjectID, AgentSessionID: agentProxySessionID}}
	signer := &agentProxySignerStub{}
	disabled, err := NewAgentProxyHandler(access, signer, agentProxyIDs{}, client, config.AgentHTTPConfig{
		BaseURL: "http://agent.internal", RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/agent/sessions/"+agentProxySessionID+"/generate-media-previews", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", agentProxyOtherID)
	if recorder := serveCreationSpecPreviewProxy(disabled, request); recorder.Code != http.StatusNotFound {
		t.Fatalf("disabled status=%d", recorder.Code)
	}

	enabled, err := NewAgentProxyHandler(access, signer, agentProxyIDs{}, client, config.AgentHTTPConfig{
		BaseURL: "http://agent.internal", RequestTimeout: time.Second, MediaRuntimeEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := `{"schema_version":"assemble_output.preview.enqueue-request.v1","source_asset_ref":{"id":"` + agentProxyOtherID + `","version":1,"content_digest":"` + strings.Repeat("a", 64) + `"},"tool_intent":{"schema_version":"assemble_output.intent.v3preview1","source_asset_id":"` + agentProxySessionID + `","expected_source_version":1,"expected_source_content_digest":"` + strings.Repeat("a", 64) + `","output_profile":"mp4_h264_640x360_2s.v1"}}`
	request = httptest.NewRequest(http.MethodPost,
		"/api/v1/agent/sessions/"+agentProxySessionID+"/assemble-output-previews", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", agentProxyOtherID)
	if recorder := serveCreationSpecPreviewProxy(enabled, request); recorder.Code != http.StatusBadRequest {
		t.Fatalf("mismatch status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if clientCalls != 0 {
		t.Fatalf("invalid requests reached Agent: calls=%d", clientCalls)
	}
}
