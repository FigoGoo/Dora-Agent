package httpserver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/gin-gonic/gin"
)

const (
	agentProxyRequestID = "019f0000-0000-7000-8000-000000000001"
	agentProxyUserID    = "019f0000-0000-7000-8000-000000000002"
	agentProxyWebID     = "019f0000-0000-7000-8000-000000000003"
	agentProxyProjectID = "019f0000-0000-7000-8000-000000000004"
	agentProxySessionID = "019f0000-0000-7000-8000-000000000005"
	agentProxyOtherID   = "019f0000-0000-7000-8000-000000000006"
	agentProxyAssertion = "YXNzZXJ0aW9u"
)

type agentProxyAccessStub struct {
	result    project.AgentSessionAccess
	err       error
	userID    string
	sessionID string
}

func (stub *agentProxyAccessStub) Resolve(_ context.Context, userID string, sessionID string) (project.AgentSessionAccess, error) {
	stub.userID = userID
	stub.sessionID = sessionID
	return stub.result, stub.err
}

type agentProxySignerStub struct {
	identity agentidentity.Identity
	err      error
}

func (stub *agentProxySignerStub) Sign(identity agentidentity.Identity) (agentidentity.Assertion, error) {
	stub.identity = identity
	return agentidentity.Assertion{
		EncodedCanonical: agentProxyAssertion, KeyVersion: "active-v1", Signature: strings.Repeat("a", 64),
	}, stub.err
}

type agentProxyClientFunc func(*http.Request) (*http.Response, error)

func (client agentProxyClientFunc) Do(request *http.Request) (*http.Response, error) {
	return client(request)
}

type agentProxyIDs struct{}

func (agentProxyIDs) New() (string, error) { return agentProxyRequestID, nil }

func newAgentProxyHandlerForTest(t *testing.T, client AgentHTTPClient) (*AgentProxyHandler, *agentProxyAccessStub, *agentProxySignerStub) {
	t.Helper()
	access := &agentProxyAccessStub{result: project.AgentSessionAccess{ProjectID: agentProxyProjectID, AgentSessionID: agentProxySessionID}}
	signer := &agentProxySignerStub{}
	handler, err := NewAgentProxyHandler(access, signer, agentProxyIDs{}, client, config.AgentHTTPConfig{
		BaseURL: "http://agent.internal", RequestTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewAgentProxyHandler() error = %v", err)
	}
	return handler, access, signer
}

func agentProxyResolvedMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		resolved := auth.ResolvedSession{
			Principal: auth.Principal{ID: agentProxyUserID}, WebSessionID: agentProxyWebID,
			WebSessionVersion: 7, SessionExpiresAt: time.Now().Add(time.Hour),
		}
		ctx := auth.ContextWithResolvedSession(c.Request.Context(), resolved)
		c.Request = c.Request.WithContext(auth.ContextWithPrincipal(ctx, resolved.Principal))
		c.Next()
	}
}

func serveAgentProxyRequest(handler *AgentProxyHandler, request *http.Request) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.Register(router, agentProxyResolvedMiddleware())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

type agentProxyFlushRecorder struct {
	*httptest.ResponseRecorder
	flushCalls     int
	failAt         int
	flushErr       error
	deadlineCalls  []time.Time
	deadlineFailAt int
	deadlineErr    error
}

func (recorder *agentProxyFlushRecorder) FlushError() error {
	recorder.flushCalls++
	if recorder.flushCalls == recorder.failAt {
		return recorder.flushErr
	}
	recorder.ResponseRecorder.Flush()
	return nil
}

func (recorder *agentProxyFlushRecorder) SetWriteDeadline(deadline time.Time) error {
	recorder.deadlineCalls = append(recorder.deadlineCalls, deadline)
	if len(recorder.deadlineCalls) == recorder.deadlineFailAt {
		return recorder.deadlineErr
	}
	return nil
}

func assertAgentProxyDeadlinePairs(t *testing.T, calls []time.Time, expectedPairs int) {
	t.Helper()
	if len(calls) != expectedPairs*2 {
		t.Fatalf("deadline calls=%d, want %d", len(calls), expectedPairs*2)
	}
	for index, deadline := range calls {
		if index%2 == 0 && deadline.IsZero() {
			t.Fatalf("deadline call %d unexpectedly clears before write", index+1)
		}
		if index%2 == 1 && !deadline.IsZero() {
			t.Fatalf("deadline call %d did not clear immediately after write", index+1)
		}
	}
}

type agentProxyChunkReader struct {
	chunks [][]byte
	reads  int
}

func (reader *agentProxyChunkReader) Read(target []byte) (int, error) {
	if reader.reads >= len(reader.chunks) {
		return 0, io.EOF
	}
	chunk := reader.chunks[reader.reads]
	reader.reads++
	if len(chunk) > len(target) {
		panic("agentProxyChunkReader test chunk exceeds target buffer")
	}
	return copy(target, chunk), nil
}

func serveAgentProxyRequestWithWriter(handler *AgentProxyHandler, request *http.Request, writer http.ResponseWriter) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.Register(router, agentProxyResolvedMiddleware())
	router.ServeHTTP(writer, request)
}

func TestAgentProxyWorkspaceRebuildsRequestWithoutBrowserHeaders(t *testing.T) {
	var upstream *http.Request
	client := agentProxyClientFunc(func(request *http.Request) (*http.Response, error) {
		upstream = request
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"schema_version":"session.workspace.v1","messages":[],"inputs":[]}`)),
		}, nil
	})
	handler, access, signer := newAgentProxyHandlerForTest(t, client)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxySessionID+"/workspace", nil)
	request.Header.Set("Cookie", "dora_session=browser-secret")
	request.Header.Set("Authorization", "Bearer browser-secret")
	request.Header.Set("X-CSRF-Token", "browser-secret")
	request.Header.Set(agentidentity.HeaderAssertion, "browser-forged")
	recorder := serveAgentProxyRequest(handler, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("workspace status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if upstream == nil || upstream.Method != http.MethodGet || upstream.URL.String() != "http://agent.internal/api/v1/agent/sessions/"+agentProxySessionID+"/workspace" {
		t.Fatalf("upstream request=%v", upstream)
	}
	if upstream.Header.Get("Cookie") != "" || upstream.Header.Get("Authorization") != "" || upstream.Header.Get("X-CSRF-Token") != "" ||
		upstream.Header.Get(agentidentity.HeaderAssertion) != agentProxyAssertion || upstream.Header.Get("Accept") != "application/json" || len(upstream.Header) != 4 {
		t.Fatalf("unsafe upstream headers=%v", upstream.Header)
	}
	if access.userID != agentProxyUserID || access.sessionID != agentProxySessionID ||
		signer.identity.WebSessionID != agentProxyWebID || signer.identity.WebSessionVersion != 7 ||
		signer.identity.ProjectID != agentProxyProjectID || signer.identity.Scope != agentidentity.ScopeWorkspaceRead {
		t.Fatalf("identity/access mismatch: access=%+v identity=%+v", access, signer.identity)
	}
}

func TestAgentProxyEventsUsesMaxCursorAndDoesNotApplySnapshotTimeout(t *testing.T) {
	var target string
	var hasDeadline bool
	client := agentProxyClientFunc(func(request *http.Request) (*http.Response, error) {
		target = request.URL.RequestURI()
		_, hasDeadline = request.Context().Deadline()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("event: stream.ready\ndata: {\"cursor\":9}\n\n: heartbeat 1\n\n")),
		}, nil
	})
	handler, _, signer := newAgentProxyHandlerForTest(t, client)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxySessionID+"/events?after_seq=7", nil)
	request.Header.Set("Last-Event-ID", "9")
	recorder := &agentProxyFlushRecorder{ResponseRecorder: httptest.NewRecorder()}
	serveAgentProxyRequestWithWriter(handler, request, recorder)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "event: stream.ready\ndata: {\"cursor\":9}\n\n: heartbeat 1\n\n" {
		t.Fatalf("events status=%d headers=%v body=%q", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	if recorder.flushCalls != 3 || recorder.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" ||
		recorder.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("events flush/header mismatch: calls=%d headers=%v", recorder.flushCalls, recorder.Header())
	}
	assertAgentProxyDeadlinePairs(t, recorder.deadlineCalls, 3)
	if target != "/api/v1/agent/sessions/"+agentProxySessionID+"/events?after_seq=9" || hasDeadline {
		t.Fatalf("target=%q has_deadline=%v", target, hasDeadline)
	}
	if signer.identity.CanonicalTarget != target || signer.identity.Scope != agentidentity.ScopeEventsRead {
		t.Fatalf("events identity=%+v", signer.identity)
	}
}

func TestAgentProxyEventsStopsOnObservableFlushFailure(t *testing.T) {
	flushFailure := errors.New("flush failed")
	for _, testCase := range []struct {
		name          string
		failAt        int
		expectedReads int
		expectedBody  string
	}{
		{name: "initial header", failAt: 1, expectedReads: 0, expectedBody: ""},
		{name: "first complete frame", failAt: 2, expectedReads: 3, expectedBody: "event: stream.ready\ndata: {}\n\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			reader := &agentProxyChunkReader{chunks: [][]byte{
				[]byte("event: stream.ready\n"), []byte("data: {}\n"), []byte("\n"),
				[]byte(": heartbeat 1\n"), []byte("\n"),
			}}
			client := agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(reader),
				}, nil
			})
			handler, _, _ := newAgentProxyHandlerForTest(t, client)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxySessionID+"/events?after_seq=0", nil)
			recorder := &agentProxyFlushRecorder{
				ResponseRecorder: httptest.NewRecorder(), failAt: testCase.failAt, flushErr: flushFailure,
			}

			serveAgentProxyRequestWithWriter(handler, request, recorder)

			if recorder.Code != http.StatusOK || recorder.flushCalls != testCase.failAt || reader.reads != testCase.expectedReads ||
				recorder.Body.String() != testCase.expectedBody {
				t.Fatalf("flush failure status=%d calls=%d reads=%d body=%q", recorder.Code, recorder.flushCalls, reader.reads, recorder.Body.String())
			}
			assertAgentProxyDeadlinePairs(t, recorder.deadlineCalls, testCase.failAt)
		})
	}
}

func TestWithProxyWriteDeadlinePreservesOperationErrorWhenClearFails(t *testing.T) {
	operationFailure := errors.New("write failed")
	recorder := &agentProxyFlushRecorder{
		ResponseRecorder: httptest.NewRecorder(), deadlineFailAt: 2, deadlineErr: errors.New("clear failed"),
	}
	controller := http.NewResponseController(recorder)

	err := withProxyWriteDeadline(controller, time.Second, func() error { return operationFailure })

	if !errors.Is(err, operationFailure) {
		t.Fatalf("withProxyWriteDeadline() error = %v, want operation error", err)
	}
	assertAgentProxyDeadlinePairs(t, recorder.deadlineCalls, 1)
}

func TestWithProxyWriteDeadlineClearsAfterSetFailureWithoutRunningOperation(t *testing.T) {
	setFailure := errors.New("set deadline failed")
	recorder := &agentProxyFlushRecorder{
		ResponseRecorder: httptest.NewRecorder(), deadlineFailAt: 1, deadlineErr: setFailure,
	}
	controller := http.NewResponseController(recorder)
	operationCalled := false

	err := withProxyWriteDeadline(controller, time.Second, func() error {
		operationCalled = true
		return nil
	})

	if !errors.Is(err, setFailure) || operationCalled {
		t.Fatalf("withProxyWriteDeadline() error = %v, operation_called=%t", err, operationCalled)
	}
	assertAgentProxyDeadlinePairs(t, recorder.deadlineCalls, 1)
}

func TestAgentProxyRejectsNonCanonicalCursorBeforeUpstream(t *testing.T) {
	clientCalls := 0
	client := agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		clientCalls++
		return nil, nil
	})
	handler, _, _ := newAgentProxyHandlerForTest(t, client)
	tests := []string{
		"?after_seq=01", "?after_seq=-1", "?after_seq=1&after_seq=2", "?other=1", "?after%5fseq=1",
		"?after_seq=9007199254740992",
	}
	for _, query := range tests {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxySessionID+"/events"+query, nil)
		recorder := serveAgentProxyRequest(handler, request)
		if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"INVALID_CURSOR"`) {
			t.Fatalf("query=%q status=%d body=%s", query, recorder.Code, recorder.Body.String())
		}
	}
	if clientCalls != 0 {
		t.Fatalf("invalid cursor reached upstream: calls=%d", clientCalls)
	}
}

func TestAgentProxyClosesOversizedSSELineWithoutUnboundedRead(t *testing.T) {
	reader := strings.NewReader(strings.Repeat("x", maximumSSEFrameBytes+1))
	client := agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(reader),
		}, nil
	})
	handler, _, _ := newAgentProxyHandlerForTest(t, client)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxySessionID+"/events?after_seq=0", nil)
	recorder := serveAgentProxyRequest(handler, request)
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
		t.Fatalf("oversized SSE status=%d body_bytes=%d", recorder.Code, recorder.Body.Len())
	}
	if consumed := reader.Size() - int64(reader.Len()); consumed > maximumSSEFrameBytes {
		t.Fatalf("oversized SSE read exceeded cap: %d", consumed)
	}
}

func TestAgentProxyEventsPropagatesBrowserCancellation(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	client := agentProxyClientFunc(func(request *http.Request) (*http.Response, error) {
		close(entered)
		<-request.Context().Done()
		close(canceled)
		return nil, request.Context().Err()
	})
	handler, _, _ := newAgentProxyHandlerForTest(t, client)
	requestContext, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxySessionID+"/events?after_seq=0", nil).WithContext(requestContext)
	done := make(chan struct{})
	go func() {
		_ = serveAgentProxyRequest(handler, request)
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("upstream request was not started")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("browser cancellation did not reach upstream context")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("canceled proxy handler did not return")
	}
}

func TestAgentHTTPClientForbidsRedirectReplay(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		targetCalls.Add(1)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", target.URL)
		writer.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	client, err := NewAgentHTTPClient(config.AgentHTTPConfig{RequestTimeout: time.Second})
	if err != nil {
		t.Fatalf("NewAgentHTTPClient() error = %v", err)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || !transport.DisableKeepAlives || transport.ForceAttemptHTTP2 {
		t.Fatalf("client transport permits assertion replay: %#v", client.Transport)
	}
	request, _ := http.NewRequest(http.MethodGet, redirect.URL, nil)
	request.Header.Set(agentidentity.HeaderAssertion, agentProxyAssertion)
	response, err := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil || targetCalls.Load() != 0 {
		t.Fatalf("redirect was followed: err=%v target_calls=%d", err, targetCalls.Load())
	}
}

func TestAgentProxyMapsInternalIdentityFailureToDependencyUnavailable(t *testing.T) {
	client := agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"INTERNAL_IDENTITY_INVALID","message":"internal detail"}}`)),
		}, nil
	})
	handler, _, _ := newAgentProxyHandlerForTest(t, client)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxySessionID+"/workspace", nil)
	recorder := serveAgentProxyRequest(handler, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"DEPENDENCY_UNAVAILABLE"`) ||
		strings.Contains(recorder.Body.String(), "internal detail") || strings.Contains(recorder.Body.String(), "INTERNAL_IDENTITY_INVALID") {
		t.Fatalf("unsafe identity mapping: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestAgentProxyWorkspaceRejectsResponseBeyondBoundedDefaultCapacity(t *testing.T) {
	reader := strings.NewReader(strings.Repeat("x", maximumWorkspaceResponseBytes+2))
	client := agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(reader),
		}, nil
	})
	handler, _, _ := newAgentProxyHandlerForTest(t, client)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxySessionID+"/workspace", nil)
	recorder := serveAgentProxyRequest(handler, request)
	if recorder.Code != http.StatusServiceUnavailable || !strings.Contains(recorder.Body.String(), `"code":"DEPENDENCY_UNAVAILABLE"`) {
		t.Fatalf("oversized workspace status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if consumed := reader.Size() - int64(reader.Len()); consumed != maximumWorkspaceResponseBytes+1 {
		t.Fatalf("bounded reader consumed=%d", consumed)
	}
}

func TestAgentProxyHidesUnauthorizedOrUnreadyBinding(t *testing.T) {
	clientCalls := 0
	client := agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		clientCalls++
		return nil, nil
	})
	handler, access, signer := newAgentProxyHandlerForTest(t, client)
	access.err = project.ErrAgentSessionNotFound
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxyOtherID+"/workspace", nil)
	recorder := serveAgentProxyRequest(handler, request)
	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"SESSION_NOT_FOUND"`) || clientCalls != 0 || signer.identity.RequestID != "" {
		t.Fatalf("authorization mapping status=%d body=%s calls=%d identity=%+v", recorder.Code, recorder.Body.String(), clientCalls, signer.identity)
	}
}

func TestAgentProxyRegistersOnlyFrozenGETAllowlist(t *testing.T) {
	clientCalls := 0
	client := agentProxyClientFunc(func(*http.Request) (*http.Response, error) {
		clientCalls++
		return nil, nil
	})
	handler, _, _ := newAgentProxyHandlerForTest(t, client)
	tests := []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/agent/sessions/"+agentProxySessionID+"/workspace", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxySessionID+"/event-window", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/agent/sessions/"+agentProxySessionID+"/events/probe", nil),
	}
	for _, request := range tests {
		recorder := serveAgentProxyRequest(handler, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("unexpected allowlist route %s %s status=%d", request.Method, request.URL.Path, recorder.Code)
		}
	}
	if clientCalls != 0 {
		t.Fatalf("non-allowlisted route reached upstream: %d", clientCalls)
	}
}
