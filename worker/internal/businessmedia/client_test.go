package businessmedia

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
	"github.com/FigoGoo/Dora-Agent/worker/internal/mediajob"
	"github.com/google/uuid"
)

func TestClientFinalizeReadyStrictRoundTrip(t *testing.T) {
	request := validFinalizeRequest(t)
	client := newTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.Method != http.MethodPost || incoming.URL.Path != finalizePath || incoming.URL.RawQuery != "" {
			t.Errorf("unexpected Finalize target: %s %s", incoming.Method, incoming.URL.RequestURI())
		}
		if incoming.Header.Get("Content-Type") != "application/json" || incoming.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected media headers: %+v", incoming.Header)
		}
		var got mediajob.FinalizeRequestV1
		decoder := json.NewDecoder(incoming.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&got); err != nil || !reflect.DeepEqual(got, request) {
			t.Errorf("decode strict Finalize request: got=%+v err=%v", got, err)
		}
		writeJSON(t, writer, validFinalizeResult(t, request))
	}), 16*1024)

	result, err := client.Finalize(t.Context(), request)
	if err != nil {
		t.Fatalf("Finalize ready: %v", err)
	}
	if result.CommandID != request.CommandID || result.AssetRef.Status != "ready" {
		t.Fatalf("unexpected Finalize response: %+v", result)
	}
}

func TestClientQueryFinalizationStrictUnions(t *testing.T) {
	request := mediajob.QueryFinalizationRequestV1{
		SchemaVersion: mediajob.QueryFinalizationRequestSchemaV1,
		RequestID:     mustUUIDv7(t),
		CommandID:     mustUUIDv7(t),
		RequestDigest: strings.Repeat("b", 64),
		PreparationID: mustUUIDv7(t),
	}
	finalizeRequest := validFinalizeRequest(t)
	finalizeRequest.RequestID = request.RequestID
	finalizeRequest.CommandID = request.CommandID
	completed := validFinalizeResult(t, finalizeRequest)
	tests := []struct {
		name     string
		response mediajob.QueryFinalizationResultV1
		wantErr  bool
	}{
		{name: "not_found", response: mediajob.QueryFinalizationResultV1{
			SchemaVersion: mediajob.QueryFinalizationResultSchemaV1, RequestID: request.RequestID, Status: "not_found",
		}},
		{name: "completed", response: mediajob.QueryFinalizationResultV1{
			SchemaVersion: mediajob.QueryFinalizationResultSchemaV1, RequestID: request.RequestID,
			Status: "completed", Result: &completed,
		}},
		{name: "conflict", response: mediajob.QueryFinalizationResultV1{
			SchemaVersion: mediajob.QueryFinalizationResultSchemaV1, RequestID: request.RequestID,
			Status: "conflict", ErrorCode: "IDEMPOTENCY_CONFLICT",
		}},
		{name: "unknown_status", response: mediajob.QueryFinalizationResultV1{
			SchemaVersion: mediajob.QueryFinalizationResultSchemaV1, RequestID: request.RequestID, Status: "pending",
		}, wantErr: true},
		{name: "invalid_union", response: mediajob.QueryFinalizationResultV1{
			SchemaVersion: mediajob.QueryFinalizationResultSchemaV1, RequestID: request.RequestID,
			Status: "not_found", ErrorCode: "IDEMPOTENCY_CONFLICT",
		}, wantErr: true},
		{name: "nested_request_mismatch", response: mediajob.QueryFinalizationResultV1{
			SchemaVersion: mediajob.QueryFinalizationResultSchemaV1, RequestID: request.RequestID,
			Status: "completed", Result: func() *mediajob.FinalizeResultV1 {
				mismatched := completed
				mismatched.RequestID = mustUUIDv7(t)
				return &mismatched
			}(),
		}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
				if incoming.URL.Path != queryFinalizationPath {
					t.Errorf("unexpected Query path: %s", incoming.URL.Path)
				}
				writeJSON(t, writer, test.response)
			}), 16*1024)
			_, err := client.QueryFinalization(t.Context(), request)
			if (err != nil) != test.wantErr {
				t.Fatalf("QueryFinalization error=%v wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestClientRejectsMalformedOrOversizedJSON(t *testing.T) {
	request := validFinalizeRequest(t)
	tests := []struct {
		name        string
		contentType string
		payload     string
		budget      int64
	}{
		{name: "duplicate_key", contentType: "application/json", payload: `{"schema_version":"x","schema_version":"y"}`, budget: 1024},
		{name: "unknown_field", contentType: "application/json", payload: `{"unknown":true}`, budget: 1024},
		{name: "trailing_token", contentType: "application/json", payload: `{} {}`, budget: 1024},
		{name: "wrong_content_type", contentType: "text/plain", payload: `{}`, budget: 1024},
		{name: "oversized", contentType: "application/json", payload: strings.Repeat(" ", 65), budget: 64},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", test.contentType)
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(test.payload))
			}), test.budget)
			if _, err := client.Finalize(t.Context(), request); err == nil {
				t.Fatal("malformed Business JSON was accepted")
			}
		})
	}
}

func TestClientClassifiesUnknownHTTPOutcome(t *testing.T) {
	request := validFinalizeRequest(t)
	client := newTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}), 1024)
	_, err := client.Finalize(t.Context(), request)
	if !errors.Is(err, mediajob.ErrOutcomeUnknown) {
		t.Fatalf("5xx must be unknown outcome, got %v", err)
	}
}

func TestClientReadinessRequiresExactProfileAndCapabilities(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		if incoming.Method != http.MethodGet || incoming.URL.Path != readinessPath {
			t.Errorf("unexpected readiness target: %s %s", incoming.Method, incoming.URL.Path)
		}
		writeJSON(t, writer, mediajob.BusinessReadinessV1{
			SchemaVersion: mediajob.ReadinessSchemaV1, Profile: config.MediaRuntimeProfileV3Preview1,
			ObjectRootReady: true, Prepare: true, Finalize: true,
		})
	}), 1024)
	if err := client.Readiness(t.Context()); err != nil {
		t.Fatalf("Business readiness: %v", err)
	}
}

func TestNewRejectsNonLoopbackBusinessURL(t *testing.T) {
	_, err := New(config.MediaRuntimeConfig{
		BusinessBaseURL: "http://192.0.2.10:8080", BusinessCallTimeout: time.Second, MaxResponseBytes: 1024,
	})
	if err == nil {
		t.Fatal("non-loopback Business URL was accepted")
	}
}

func newTestClient(t *testing.T, handler http.Handler, budget int64) *Client {
	t.Helper()
	baseURL, err := url.Parse("http://127.0.0.1:8080")
	if err != nil {
		t.Fatalf("parse test Business URL: %v", err)
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			response := recorder.Result()
			response.Request = request
			return response, nil
		})},
		maxResponseBytes: budget,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func validFinalizeRequest(t *testing.T) mediajob.FinalizeRequestV1 {
	t.Helper()
	return mediajob.FinalizeRequestV1{
		SchemaVersion: mediajob.FinalizeRequestSchemaV1,
		RequestID:     mustUUIDv7(t), CommandID: mustUUIDv7(t), RequestDigest: strings.Repeat("a", 64),
		PreparationID: mustUUIDv7(t), OperationID: mustUUIDv7(t), BatchID: mustUUIDv7(t),
		JobID: mustUUIDv7(t), AttemptID: mustUUIDv7(t), Fence: 1, TerminalStatus: "ready",
		Output: &mediajob.FinalizeOutputV1{
			ContentDigest: strings.Repeat("c", 64), SizeBytes: 4096, MIMEType: "image/png", Width: 640, Height: 360,
		},
	}
}

func validFinalizeResult(t *testing.T, request mediajob.FinalizeRequestV1) mediajob.FinalizeResultV1 {
	t.Helper()
	return mediajob.FinalizeResultV1{
		SchemaVersion: mediajob.FinalizeResultSchemaV1, RequestID: request.RequestID,
		CommandID: request.CommandID, Disposition: "created",
		AssetRef: mediajob.MediaAssetRefV1{
			AssetID: mustUUIDv7(t), Version: 1, Status: "ready", MediaKind: "image",
			MIMEType: "image/png", ContentDigest: strings.Repeat("c", 64), SizeBytes: 4096,
		},
		FinalizationReceiptID: mustUUIDv7(t), CompletedAt: time.Now().UTC(),
	}
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encode test response: %v", err)
	}
}

func mustUUIDv7(t *testing.T) uuid.UUID {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("create UUIDv7: %v", err)
	}
	return value
}
