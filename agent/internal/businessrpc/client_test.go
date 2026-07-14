package businessrpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
	"github.com/cloudwego/kitex/client/callopt"
)

type fakeProtocolClient struct {
	calls    int
	requests []*foundationv1.FoundationProbeRequestV1
	failures int
}

type fixedIDGenerator struct{ id string }

func (g fixedIDGenerator) New() (string, error) { return g.id, nil }

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func (c *fakeProtocolClient) Probe(_ context.Context, request *foundationv1.FoundationProbeRequestV1, _ ...callopt.Option) (*foundationv1.FoundationProbeResponseV1, error) {
	c.calls++
	c.requests = append(c.requests, request)
	if c.calls <= c.failures {
		return nil, errors.New("temporary transport failure")
	}
	return &foundationv1.FoundationProbeResponseV1{
		SchemaVersion: foundationv1.FOUNDATION_SCHEMA_VERSION,
		RequestId:     request.RequestId, ServiceName: "dora-business-service", ServiceVersion: "test",
		Environment: "test", InstanceId: "business-test-1", ReceivedAtUnixMs: time.Now().UnixMilli(),
	}, nil
}

// TestWaitUntilReadyReusesRequestID 验证有限重试复用同一 UUIDv7，不制造第二个语义请求。
func TestWaitUntilReadyReusesRequestID(t *testing.T) {
	protocol := &fakeProtocolClient{failures: 2}
	client := &Client{
		protocol: protocol,
		config:   config.BusinessRPCConfig{RequestTimeout: time.Second, ProbeInterval: time.Millisecond},
		caller:   config.ServiceConfig{Name: "dora-agent-service", Version: "test"},
		clock:    fixedClock{now: time.Unix(1, 0)}, idgen: fixedIDGenerator{id: "018f0000-0000-7000-8000-000000000001"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	receipt, err := client.WaitUntilReady(ctx)
	if err != nil {
		t.Fatalf("等待 Probe 成功失败: %v", err)
	}
	if protocol.calls != 3 {
		t.Fatalf("Probe 次数错误: got %d", protocol.calls)
	}
	for _, request := range protocol.requests {
		if request.RequestId != receipt.RequestID {
			t.Fatalf("重试生成了不同 Request ID: got %q want %q", request.RequestId, receipt.RequestID)
		}
	}
}

// TestProbeOnceRejectsInvalidResponse 验证未知响应版本不会进入 Agent 就绪状态。
func TestProbeOnceRejectsInvalidResponse(t *testing.T) {
	protocol := &fakeProtocolClient{}
	client := &Client{
		protocol: protocol,
		config:   config.BusinessRPCConfig{RequestTimeout: time.Second},
		caller:   config.ServiceConfig{Name: "dora-agent-service", Version: "test"},
		clock:    fixedClock{now: time.Unix(1, 0)}, idgen: fixedIDGenerator{id: "018f0000-0000-7000-8000-000000000001"},
	}
	request := &foundationv1.FoundationProbeRequestV1{
		SchemaVersion: foundationv1.FOUNDATION_SCHEMA_VERSION,
		RequestId:     "request", CallerService: "agent", CallerVersion: "test", SentAtUnixMs: 1,
	}
	response, err := protocol.Probe(context.Background(), request)
	if err != nil {
		t.Fatalf("构造假响应失败: %v", err)
	}
	response.SchemaVersion = "foundation.rpc.v2"
	protocolWithInvalidResponse := foundationProtocolClientFunc(func(_ context.Context, _ *foundationv1.FoundationProbeRequestV1, _ ...callopt.Option) (*foundationv1.FoundationProbeResponseV1, error) {
		return response, nil
	})
	client.protocol = protocolWithInvalidResponse
	if _, err := client.probeOnce(context.Background(), request); err == nil {
		t.Fatal("期望未知响应版本失败关闭")
	}
}

type foundationProtocolClientFunc func(context.Context, *foundationv1.FoundationProbeRequestV1, ...callopt.Option) (*foundationv1.FoundationProbeResponseV1, error)

func (f foundationProtocolClientFunc) Probe(ctx context.Context, request *foundationv1.FoundationProbeRequestV1, options ...callopt.Option) (*foundationv1.FoundationProbeResponseV1, error) {
	return f(ctx, request, options...)
}
