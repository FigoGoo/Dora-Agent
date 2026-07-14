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

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"github.com/gin-gonic/gin"
)

const handlerSessionID = "019f0000-0000-7000-8000-000000000005"

type acceptingVerifier struct {
	expiresAt time.Time
}

func (verifier acceptingVerifier) Verify(_ context.Context, request httpidentity.Request) (httpidentity.Claims, error) {
	return httpidentity.Claims{
		RequestID:       "019f0000-0000-7000-8000-000000000001",
		PrincipalUserID: "019f0000-0000-7000-8000-000000000002",
		ProjectID:       "019f0000-0000-7000-8000-000000000004",
		AgentSessionID:  request.AgentSessionID, Scope: request.Scope, ExpiresAt: verifier.expiresAt,
	}, nil
}

type readyWorkspaceService struct{}

func (readyWorkspaceService) LoadSnapshot(context.Context, workspace.Identity, string) (workspace.Snapshot, error) {
	return workspace.Snapshot{}, workspace.ErrNotFound
}

func (readyWorkspaceService) LoadEventBatch(_ context.Context, _ workspace.Identity, cursor int64, _ int) (workspace.EventBatch, error) {
	return workspace.EventBatch{LatestSeq: cursor, MinAvailableSeq: 1, Events: []workspace.ProjectedEvent{}}, nil
}

type systemTimeSource struct{}

func (systemTimeSource) Now() time.Time { return time.Now().UTC() }

type fixedTimeSource struct{ now time.Time }

func (source fixedTimeSource) Now() time.Time { return source.now }

type failingIDGenerator struct{}

func (failingIDGenerator) New() (string, error) { return "", errors.New("random unavailable") }

type invalidIDGenerator struct{}

func (invalidIDGenerator) New() (string, error) { return "not-a-uuid", nil }

// TestWorkspaceSSECommitsHeadersBeforeFrames 验证真实 net/http 响应在 Ready/Heartbeat 前已带 text/event-stream 与禁缓冲 Header。
func TestWorkspaceSSECommitsHeadersBeforeFrames(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	limiter, err := workspace.NewStreamLimiter(10, 2, 1)
	if err != nil {
		t.Fatalf("创建流限流器失败: %v", err)
	}
	handler, err := NewWorkspaceHandler(
		acceptingVerifier{expiresAt: time.Now().Add(time.Second)}, readyWorkspaceService{}, limiter,
		config.SSEConfig{
			BatchSize: 10, PollInterval: 5 * time.Millisecond, HeartbeatInterval: 10 * time.Millisecond,
			MaxConnectionDuration: 30 * time.Millisecond, FrameWriteTimeout: 5 * time.Millisecond, MaxEventBytes: 1024,
		},
		serverTestIDs{}, systemTimeSource{},
	)
	if err != nil {
		t.Fatalf("创建 Workspace Handler 失败: %v", err)
	}
	router := gin.New()
	handler.Register(router)
	server := httptest.NewServer(router)
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/api/v1/agent/sessions/" + handlerSessionID + "/events?after_seq=2")
	if err != nil {
		t.Fatalf("请求真实 SSE 失败: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("读取 SSE 失败: %v", err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream; charset=utf-8" ||
		response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("SSE Header 不完整: status=%d headers=%v", response.StatusCode, response.Header)
	}
	if !strings.Contains(string(body), "event: stream.ready\n") || strings.Contains(string(body), "id: 2\nevent: stream.ready") {
		t.Fatalf("Ready 控制帧错误: %q", body)
	}
}

// TestCanonicalCursorRejectsNonUniqueAndUnsafeIntegers 验证入口在消耗身份 Nonce 前拒绝非规范及非 JavaScript safe Cursor。
func TestCanonicalCursorRejectsNonUniqueAndUnsafeIntegers(t *testing.T) {
	for _, raw := range []string{"", "after_seq=", "after_seq=01", "after_seq=-1", "after_seq=1&after_seq=2", "after_seq=9007199254740992"} {
		if _, ok := canonicalCursor(raw); ok {
			t.Fatalf("非法 Cursor %q 被接受", raw)
		}
	}
	if cursor, ok := canonicalCursor("after_seq=9007199254740991"); !ok || cursor != 9007199254740991 {
		t.Fatalf("最大 safe Cursor=%d ok=%v", cursor, ok)
	}
}

// TestNewRequestIDEmergencyFallbackRemainsUUIDv7 验证随机源故障时错误 Envelope 仍保留规范 UUIDv7，而不是空值。
func TestNewRequestIDEmergencyFallbackRemainsUUIDv7(t *testing.T) {
	for _, generator := range []IDGenerator{failingIDGenerator{}, invalidIDGenerator{}} {
		handler := &WorkspaceHandler{ids: generator}
		if requestID, ok := canonicalUUIDv7(handler.newRequestID()); !ok || requestID == "" {
			t.Fatalf("紧急 RequestID=%q ok=%v", requestID, ok)
		}
	}
}

type deadlineTestWriter struct {
	header        http.Header
	deadlines     []time.Time
	writeErr      error
	flushErr      error
	clearErr      error
	writtenStatus int
	flushCalls    int
	flushErrors   map[int]error
}

func (writer *deadlineTestWriter) Header() http.Header {
	if writer.header == nil {
		writer.header = make(http.Header)
	}
	return writer.header
}

func (writer *deadlineTestWriter) WriteHeader(status int) { writer.writtenStatus = status }

func (writer *deadlineTestWriter) Write(value []byte) (int, error) {
	if writer.writeErr != nil {
		return 0, writer.writeErr
	}
	return len(value), nil
}

func (writer *deadlineTestWriter) Flush() {}

func (writer *deadlineTestWriter) FlushError() error {
	writer.flushCalls++
	if err := writer.flushErrors[writer.flushCalls]; err != nil {
		return err
	}
	return writer.flushErr
}

func (writer *deadlineTestWriter) SetWriteDeadline(deadline time.Time) error {
	writer.deadlines = append(writer.deadlines, deadline)
	if deadline.IsZero() {
		return writer.clearErr
	}
	return nil
}

// TestSSEWriterAlwaysClearsFrameDeadline 验证 Write/Flush 失败后仍清空 Deadline，并保留原始错误优先级。
func TestSSEWriterAlwaysClearsFrameDeadline(t *testing.T) {
	writeFailure := errors.New("write failed")
	flushFailure := errors.New("flush failed")
	clearFailure := errors.New("clear failed")
	for _, fixture := range []struct {
		name    string
		writer  *deadlineTestWriter
		invoke  func(*sseWriter) error
		wantErr error
	}{
		{
			name: "write failure still clears", writer: &deadlineTestWriter{writeErr: writeFailure},
			invoke: func(writer *sseWriter) error { return writer.writeHeartbeat(1) }, wantErr: writeFailure,
		},
		{
			name: "flush failure still clears", writer: &deadlineTestWriter{flushErr: flushFailure},
			invoke: func(writer *sseWriter) error { return writer.writeHeartbeat(1) }, wantErr: flushFailure,
		},
		{
			name: "write error wins over clear", writer: &deadlineTestWriter{writeErr: writeFailure, clearErr: clearFailure},
			invoke: func(writer *sseWriter) error { return writer.writeHeartbeat(1) }, wantErr: writeFailure,
		},
		{
			name: "clear error returned after success", writer: &deadlineTestWriter{clearErr: clearFailure},
			invoke: func(writer *sseWriter) error { return writer.writeHeartbeat(1) }, wantErr: clearFailure,
		},
		{
			name: "start flush failure still clears", writer: &deadlineTestWriter{flushErr: flushFailure},
			invoke: func(writer *sseWriter) error { return writer.start() }, wantErr: flushFailure,
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			sse, err := newSSEWriter(fixture.writer, time.Second, time.Time{}, systemTimeSource{})
			if err != nil {
				t.Fatalf("创建 SSE Writer 失败: %v", err)
			}
			gotErr := fixture.invoke(sse)
			if !errors.Is(gotErr, fixture.wantErr) {
				t.Fatalf("错误=%v want=%v", gotErr, fixture.wantErr)
			}
			if len(fixture.writer.deadlines) != 2 || fixture.writer.deadlines[0].IsZero() || !fixture.writer.deadlines[1].IsZero() {
				t.Fatalf("Deadline 调用=%v", fixture.writer.deadlines)
			}
		})
	}
}

// TestSSEWriterClampsFrameDeadlineToAssertionExpiry 验证临近断言到期的阻塞 Flush 不会越过连接硬期限。
func TestSSEWriterClampsFrameDeadlineToAssertionExpiry(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 30, 0, 0, time.UTC)
	hardDeadline := now.Add(100 * time.Millisecond)
	writer := &deadlineTestWriter{}
	sse, err := newSSEWriter(writer, time.Second, hardDeadline, fixedTimeSource{now: now})
	if err != nil {
		t.Fatalf("创建 SSE Writer 失败: %v", err)
	}
	if err := sse.writeHeartbeat(1); err != nil {
		t.Fatalf("写入临近到期帧失败: %v", err)
	}
	if len(writer.deadlines) != 2 || !writer.deadlines[0].Equal(hardDeadline) || !writer.deadlines[1].IsZero() {
		t.Fatalf("Deadline 调用=%v want=[%v zero]", writer.deadlines, hardDeadline)
	}

	expiredWriter := &deadlineTestWriter{}
	expired, err := newSSEWriter(expiredWriter, time.Second, now, fixedTimeSource{now: now})
	if err != nil {
		t.Fatalf("创建已到期 SSE Writer 失败: %v", err)
	}
	if err := expired.writeHeartbeat(1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("已到期帧错误=%v want=%v", err, context.DeadlineExceeded)
	}
	if len(expiredWriter.deadlines) != 0 || expiredWriter.flushCalls != 0 {
		t.Fatalf("已到期帧仍触发 IO: deadlines=%v flush=%d", expiredWriter.deadlines, expiredWriter.flushCalls)
	}
}

type maskingFlushWriter struct {
	underlying http.ResponseWriter
}

func (writer *maskingFlushWriter) Header() http.Header { return writer.underlying.Header() }
func (writer *maskingFlushWriter) Write(value []byte) (int, error) {
	return writer.underlying.Write(value)
}
func (writer *maskingFlushWriter) WriteHeader(status int)      { writer.underlying.WriteHeader(status) }
func (writer *maskingFlushWriter) Flush()                      {}
func (writer *maskingFlushWriter) Unwrap() http.ResponseWriter { return writer.underlying }

// TestSSEWriterUnwrapsMaskingFlushWriter 验证 Gin 风格无 error Flush 不会遮蔽底层初始 Header 或事件帧 FlushError。
func TestSSEWriterUnwrapsMaskingFlushWriter(t *testing.T) {
	initialFailure := errors.New("initial flush failed")
	initialBottom := &deadlineTestWriter{flushErr: initialFailure}
	initialWriter, err := newSSEWriter(&maskingFlushWriter{underlying: initialBottom}, time.Second, time.Time{}, systemTimeSource{})
	if err != nil {
		t.Fatalf("创建初始故障 SSE Writer 失败: %v", err)
	}
	if err := initialWriter.start(); !errors.Is(err, initialFailure) {
		t.Fatalf("初始 FlushError=%v", err)
	}

	eventFailure := errors.New("event flush failed")
	eventBottom := &deadlineTestWriter{flushErrors: map[int]error{2: eventFailure}}
	eventWriter, err := newSSEWriter(&maskingFlushWriter{underlying: eventBottom}, time.Second, time.Time{}, systemTimeSource{})
	if err != nil {
		t.Fatalf("创建事件故障 SSE Writer 失败: %v", err)
	}
	if err := eventWriter.start(); err != nil {
		t.Fatalf("初始 Header Flush 失败: %v", err)
	}
	cursor := int64(2)
	err = writeProjectedEvents(eventWriter, &cursor, []workspace.ProjectedEvent{{
		Seq: 3, Event: "session.input.accepted", Data: []byte(`{"seq":3}`),
	}})
	if !errors.Is(err, eventFailure) || cursor != 2 {
		t.Fatalf("事件 Flush 失败 err=%v cursor=%d", err, cursor)
	}
	if eventBottom.flushCalls != 2 {
		t.Fatalf("底层 FlushError 调用=%d，want 2", eventBottom.flushCalls)
	}
}

var _ http.ResponseWriter = (*deadlineTestWriter)(nil)
var _ http.Flusher = (*deadlineTestWriter)(nil)
var _ interface{ FlushError() error } = (*deadlineTestWriter)(nil)
var _ interface{ SetWriteDeadline(time.Time) error } = (*deadlineTestWriter)(nil)
var _ http.Flusher = (*maskingFlushWriter)(nil)
var _ interface{ Unwrap() http.ResponseWriter } = (*maskingFlushWriter)(nil)
