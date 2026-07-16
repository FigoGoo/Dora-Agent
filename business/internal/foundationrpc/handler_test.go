package foundationrpc

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
	"github.com/google/uuid"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// TestProbeReturnsFrozenIdentity 验证 Probe 只回显冻结身份和原 Request ID。
func TestProbeReturnsFrozenIdentity(t *testing.T) {
	now := time.Date(2026, 7, 14, 1, 2, 3, 0, time.UTC)
	handler, err := NewHandler(config.ServiceConfig{
		Name: "dora-business-service", Version: "test", Environment: "test", InstanceID: "business-test-1",
	}, fixedClock{now: now}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("创建 Handler 失败: %v", err)
	}
	requestID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("生成 UUIDv7 失败: %v", err)
	}
	response, err := handler.Probe(context.Background(), &foundationv1.FoundationProbeRequestV1{
		SchemaVersion: foundationv1.FOUNDATION_SCHEMA_VERSION,
		RequestId:     requestID.String(), CallerService: "dora-agent-service", CallerVersion: "test",
		SentAtUnixMs: now.Add(-time.Second).UnixMilli(),
	})
	if err != nil {
		t.Fatalf("执行合法 Probe 失败: %v", err)
	}
	if response.RequestId != requestID.String() || response.InstanceId != "business-test-1" {
		t.Fatalf("Probe 回执不一致: %+v", response)
	}
	if response.ReceivedAtUnixMs != now.UnixMilli() {
		t.Fatalf("接收时间错误: got %d", response.ReceivedAtUnixMs)
	}
}

// TestProbeRejectsInvalidContract 覆盖 v1 版本、UUID 和长度的失败关闭分支。
func TestProbeRejectsInvalidContract(t *testing.T) {
	handler, err := NewHandler(config.ServiceConfig{
		Name: "dora-business-service", Version: "test", Environment: "test", InstanceID: "business-test-1",
	}, fixedClock{now: time.Now()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("创建 Handler 失败: %v", err)
	}
	tests := []struct {
		name    string
		request *foundationv1.FoundationProbeRequestV1
	}{
		{name: "空请求", request: nil},
		{name: "未知版本", request: &foundationv1.FoundationProbeRequestV1{SchemaVersion: "v2"}},
		{name: "非 UUIDv7", request: &foundationv1.FoundationProbeRequestV1{
			SchemaVersion: foundationv1.FOUNDATION_SCHEMA_VERSION, RequestId: uuid.NewString(),
			CallerService: "agent", CallerVersion: "test", SentAtUnixMs: 1,
		}},
		{name: "调用方过长", request: &foundationv1.FoundationProbeRequestV1{
			SchemaVersion: foundationv1.FOUNDATION_SCHEMA_VERSION, RequestId: mustUUIDv7(t),
			CallerService: strings.Repeat("a", maxIdentityLength+1), CallerVersion: "test", SentAtUnixMs: 1,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := handler.Probe(context.Background(), test.request)
			serviceErr, ok := err.(*foundationv1.FoundationServiceExceptionV1)
			if !ok || serviceErr.Code != invalidArgumentCode || serviceErr.Retryable {
				t.Fatalf("期望 INVALID_ARGUMENT，得到 %T %v", err, err)
			}
		})
	}
}

func mustUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("生成 UUIDv7 失败: %v", err)
	}
	return id.String()
}
