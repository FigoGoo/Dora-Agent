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

// TestProbeOnceRequiresExactPlanStoryboardRuntimeProfile 验证任一端单独开启或 Profile 漂移都阻断 Agent Ready。
func TestProbeOnceRequiresExactPlanStoryboardRuntimeProfile(t *testing.T) {
	request := &foundationv1.FoundationProbeRequestV1{
		SchemaVersion: foundationv1.FOUNDATION_SCHEMA_VERSION,
		RequestId:     "018f0000-0000-7000-8000-000000000001",
		CallerService: "dora-agent-service", CallerVersion: "test", SentAtUnixMs: 1,
	}
	response := &foundationv1.FoundationProbeResponseV1{
		SchemaVersion: foundationv1.FOUNDATION_SCHEMA_VERSION, RequestId: request.RequestId,
		ServiceName: "dora-business-service", ServiceVersion: "test", Environment: "local",
		InstanceId: "business-test-1", ReceivedAtUnixMs: 1,
	}
	enabled := true
	profile := foundationv1.PLAN_STORYBOARD_RUNTIME_PROFILE
	response.PlanStoryboardRuntimeEnabled = &enabled
	response.PlanStoryboardRuntimeProfile = &profile
	protocol := foundationProtocolClientFunc(func(_ context.Context, _ *foundationv1.FoundationProbeRequestV1, _ ...callopt.Option) (*foundationv1.FoundationProbeResponseV1, error) {
		return response, nil
	})
	client := &Client{protocol: protocol, config: config.BusinessRPCConfig{RequestTimeout: time.Second}}
	if _, err := client.probeOnce(context.Background(), request); err == nil {
		t.Fatal("Business 单独开启 Storyboard Runtime 未阻断 Agent Ready")
	}
	client.storyboardExpected = true
	if _, err := client.probeOnce(context.Background(), request); err != nil {
		t.Fatalf("双端 exact-match 未通过: %v", err)
	}
	wrongProfile := "plan_storyboard.runtime.v3"
	response.PlanStoryboardRuntimeProfile = &wrongProfile
	if _, err := client.probeOnce(context.Background(), request); err == nil {
		t.Fatal("Storyboard Runtime Profile 漂移未阻断 Agent Ready")
	}
	disabled := false
	emptyProfile := ""
	response.PlanStoryboardRuntimeEnabled = &disabled
	response.PlanStoryboardRuntimeProfile = &emptyProfile
	if _, err := client.probeOnce(context.Background(), request); err == nil {
		t.Fatal("Agent 单独开启 Storyboard Runtime 未阻断 Agent Ready")
	}
}

// TestProbeOnceRequiresExactWritePromptsRuntimeProfile 验证 Prompt Preview 双端开关与 Profile 必须同时 exact-match。
func TestProbeOnceRequiresExactWritePromptsRuntimeProfile(t *testing.T) {
	request := &foundationv1.FoundationProbeRequestV1{
		SchemaVersion: foundationv1.FOUNDATION_SCHEMA_VERSION,
		RequestId:     "018f0000-0000-7000-8000-000000000001",
		CallerService: "dora-agent-service", CallerVersion: "test", SentAtUnixMs: 1,
	}
	response := &foundationv1.FoundationProbeResponseV1{
		SchemaVersion: foundationv1.FOUNDATION_SCHEMA_VERSION, RequestId: request.RequestId,
		ServiceName: "dora-business-service", ServiceVersion: "test", Environment: "local",
		InstanceId: "business-test-1", ReceivedAtUnixMs: 1,
	}
	enabled := true
	profile := foundationv1.WRITE_PROMPTS_RUNTIME_PROFILE
	response.WritePromptsRuntimeEnabled = &enabled
	response.WritePromptsRuntimeProfile = &profile
	protocol := foundationProtocolClientFunc(func(_ context.Context, _ *foundationv1.FoundationProbeRequestV1, _ ...callopt.Option) (*foundationv1.FoundationProbeResponseV1, error) {
		return response, nil
	})
	client := &Client{protocol: protocol, config: config.BusinessRPCConfig{RequestTimeout: time.Second}}
	if _, err := client.probeOnce(context.Background(), request); err == nil {
		t.Fatal("Business 单独开启 Write Prompts Runtime 未阻断 Agent Ready")
	}
	client.promptExpected = true
	if _, err := client.probeOnce(context.Background(), request); err != nil {
		t.Fatalf("双端 Write Prompts exact-match 未通过: %v", err)
	}
	wrongProfile := "write_prompts.runtime.v3"
	response.WritePromptsRuntimeProfile = &wrongProfile
	if _, err := client.probeOnce(context.Background(), request); err == nil {
		t.Fatal("Write Prompts Runtime Profile 漂移未阻断 Agent Ready")
	}
	disabled := false
	emptyProfile := ""
	response.WritePromptsRuntimeEnabled = &disabled
	response.WritePromptsRuntimeProfile = &emptyProfile
	if _, err := client.probeOnce(context.Background(), request); err == nil {
		t.Fatal("Agent 单独开启 Write Prompts Runtime 未阻断 Agent Ready")
	}
}

type foundationProtocolClientFunc func(context.Context, *foundationv1.FoundationProbeRequestV1, ...callopt.Option) (*foundationv1.FoundationProbeResponseV1, error)

func (f foundationProtocolClientFunc) Probe(ctx context.Context, request *foundationv1.FoundationProbeRequestV1, options ...callopt.Option) (*foundationv1.FoundationProbeResponseV1, error) {
	return f(ctx, request, options...)
}
