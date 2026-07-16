package businessrpc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/clock"
	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/idgen"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1/businessfoundationservicev1"
	kitexclient "github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/client/callopt"
)

var errInvalidProbeResponse = errors.New("invalid Business Foundation Probe response")

// Clock 提供可注入的请求时间，避免启动探针测试依赖真实时钟。
type Clock interface {
	// Now 返回当前时间。
	Now() time.Time
}

// IDGenerator 生成启动探针使用的稳定 UUIDv7。
type IDGenerator interface {
	// New 返回一个新的 UUIDv7 字符串。
	New() (string, error)
}

// foundationProtocolClient 是 Agent 消费方定义的最小生成 Client 接口，便于隔离协议测试。
type foundationProtocolClient interface {
	Probe(ctx context.Context, request *foundationv1.FoundationProbeRequestV1, callOptions ...callopt.Option) (*foundationv1.FoundationProbeResponseV1, error)
}

// ProbeReceipt 是 Agent 启动成功后保留的最小跨服务探针回执，不扩散生成类型。
type ProbeReceipt struct {
	// RequestID 是本次启动探针的 UUIDv7，用于日志和冒烟证据关联。
	RequestID string
	// BusinessService 是响应的 Business 稳定服务名。
	BusinessService string
	// BusinessVersion 是实际处理请求的 Business 构建版本。
	BusinessVersion string
	// BusinessInstanceID 是实际处理请求的 Business 实例标识。
	BusinessInstanceID string
	// ReceivedAt 是 Business 接收请求的 UTC 时间。
	ReceivedAt time.Time
}

// Client 管理生成的 Foundation Client、etcd Resolver 和启动重试预算。
type Client struct {
	protocol foundationProtocolClient
	resolver *EtcdResolver
	config   config.BusinessRPCConfig
	caller   config.ServiceConfig
	clock    Clock
	idgen    IDGenerator
}

// NewClient 创建禁用写重试、具有显式连接和请求超时的 Foundation Client。
func NewClient(ctx context.Context, rpcCfg config.BusinessRPCConfig, etcdCfg config.EtcdConfig, caller config.ServiceConfig) (*Client, error) {
	resolver, err := NewEtcdResolver(ctx, etcdCfg)
	if err != nil {
		return nil, err
	}
	protocol, err := businessfoundationservicev1.NewClient(
		foundationv1.BUSINESS_FOUNDATION_SERVICE_NAME,
		kitexclient.WithResolver(resolver),
		kitexclient.WithConnectTimeout(rpcCfg.ConnectTimeout),
		kitexclient.WithRPCTimeout(rpcCfg.RequestTimeout),
	)
	if err != nil {
		_ = resolver.Close()
		return nil, fmt.Errorf("create Business Foundation RPC client: %w", err)
	}
	return &Client{
		protocol: protocol, resolver: resolver, config: rpcCfg, caller: caller,
		clock: clock.System{}, idgen: idgen.UUIDv7{},
	}, nil
}

// WaitUntilReady 在一个启动预算内重试只读 Probe；契约错误立即失败，不消耗无意义重试。
func (c *Client) WaitUntilReady(ctx context.Context) (ProbeReceipt, error) {
	requestID, err := c.idgen.New()
	if err != nil {
		return ProbeReceipt{}, fmt.Errorf("generate Foundation Probe request ID: %w", err)
	}
	request := &foundationv1.FoundationProbeRequestV1{
		SchemaVersion: foundationv1.FOUNDATION_SCHEMA_VERSION,
		RequestId:     requestID,
		CallerService: c.caller.Name,
		CallerVersion: c.caller.Version,
		SentAtUnixMs:  c.clock.Now().UTC().UnixMilli(),
	}

	var lastErr error
	for {
		receipt, err := c.probeOnce(ctx, request)
		if err == nil {
			return receipt, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return ProbeReceipt{}, fmt.Errorf("wait Business Foundation RPC ready: %w: %v", ctx.Err(), lastErr)
		}
		if isPermanentProbeError(err) {
			return ProbeReceipt{}, err
		}

		timer := time.NewTimer(c.config.ProbeInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ProbeReceipt{}, fmt.Errorf("wait Business Foundation RPC ready: %w: %v", ctx.Err(), lastErr)
		case <-timer.C:
		}
	}
}

// probeOnce 执行一次有界 Probe，并把生成响应显式映射为 Agent 内部回执。
func (c *Client) probeOnce(ctx context.Context, request *foundationv1.FoundationProbeRequestV1) (ProbeReceipt, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	response, err := c.protocol.Probe(requestCtx, request)
	if err != nil {
		return ProbeReceipt{}, fmt.Errorf("call Business Foundation Probe: %w", err)
	}
	if response == nil || response.SchemaVersion != foundationv1.FOUNDATION_SCHEMA_VERSION ||
		response.RequestId != request.RequestId || response.ServiceName != "dora-business-service" ||
		response.ServiceVersion == "" || response.InstanceId == "" || response.ReceivedAtUnixMs <= 0 {
		return ProbeReceipt{}, errInvalidProbeResponse
	}
	return ProbeReceipt{
		RequestID: request.RequestId, BusinessService: response.ServiceName,
		BusinessVersion: response.ServiceVersion, BusinessInstanceID: response.InstanceId,
		ReceivedAt: time.UnixMilli(response.ReceivedAtUnixMs).UTC(),
	}, nil
}

// isPermanentProbeError 识别服务端失败关闭和响应契约错误，避免对确定性错误热重试。
func isPermanentProbeError(err error) bool {
	var serviceErr *foundationv1.FoundationServiceExceptionV1
	if errors.As(err, &serviceErr) {
		return !serviceErr.Retryable
	}
	return errors.Is(err, errInvalidProbeResponse)
}

// Close 关闭 Client 自有的 etcd Resolver；Kitex 生成 Client 与进程连接池同生命周期。
func (c *Client) Close() error {
	return c.resolver.Close()
}
