// Package agentsessionrpc 实现 Business 消费 Agent Session RPC v1 的 Client 与 etcd 发现边界。
package agentsessionrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/cloudwego/kitex/pkg/discovery"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// registrationRecordV1 是 Dora etcd 服务注册记录的最小安全投影。
type registrationRecordV1 struct {
	// Service 稳定 Kitex 服务名。
	Service string `json:"service"`
	// InstanceID 处理请求的实例标识。
	InstanceID string `json:"instance_id"`
	// Address 其他 Runtime 可访问的 TCP 地址。
	Address string `json:"address"`
	// Version 实例构建版本。
	Version string `json:"version"`
}

// EtcdResolver 从 Dora 稳定 Prefix 解析当前租约下的 Agent Session RPC 实例。
type EtcdResolver struct {
	client        *clientv3.Client
	allowLoopback bool
}

// NewEtcdResolver 创建自有 etcd Client 并确认至少一个 Endpoint 可用；失败时关闭已创建连接。
// allowLoopback 只供已经通过 Config local + exact Storyboard Profile 门禁的单机 Trial 使用。
func NewEtcdResolver(ctx context.Context, cfg config.EtcdConfig, allowLoopback bool) (*EtcdResolver, error) {
	client, err := clientv3.New(clientv3.Config{Endpoints: cfg.Endpoints, DialTimeout: cfg.DialTimeout})
	if err != nil {
		return nil, fmt.Errorf("open Agent Session RPC etcd resolver: %w", err)
	}
	var lastStatusErr error
	for _, endpoint := range cfg.Endpoints {
		statusCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
		_, lastStatusErr = client.Status(statusCtx, endpoint)
		cancel()
		if lastStatusErr == nil {
			return &EtcdResolver{client: client, allowLoopback: allowLoopback}, nil
		}
	}
	_ = client.Close()
	return nil, fmt.Errorf("check Agent Session RPC resolver endpoints: %w", lastStatusErr)
}

// Target 返回 Kitex 发现使用的稳定目标服务名。
func (resolver *EtcdResolver) Target(_ context.Context, target rpcinfo.EndpointInfo) string {
	return target.ServiceName()
}

// Resolve 读取合法 Agent Endpoint；隔离畸形记录，全部无效时失败关闭。
func (resolver *EtcdResolver) Resolve(ctx context.Context, serviceName string) (discovery.Result, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" || strings.Contains(serviceName, "/") {
		return discovery.Result{}, fmt.Errorf("invalid Agent Session RPC discovery service name")
	}
	response, err := resolver.client.Get(ctx, "/dora/services/"+serviceName+"/", clientv3.WithPrefix())
	if err != nil {
		return discovery.Result{}, fmt.Errorf("resolve Agent Session RPC endpoints: %w", err)
	}
	instances := make([]discovery.Instance, 0, len(response.Kvs))
	for _, item := range response.Kvs {
		if instance, ok := parseRegistrationInstance(serviceName, item.Value, resolver.allowLoopback); ok {
			instances = append(instances, instance)
		}
	}
	if len(instances) == 0 {
		return discovery.Result{}, fmt.Errorf("no valid Agent Session RPC instance for %q", serviceName)
	}
	return discovery.Result{Cacheable: false, Instances: instances}, nil
}

// parseRegistrationInstance 将不可信 etcd JSON 收敛为可访问 Kitex Instance。
func parseRegistrationInstance(serviceName string, value []byte, allowLoopback bool) (discovery.Instance, bool) {
	var record registrationRecordV1
	if err := json.Unmarshal(value, &record); err != nil || record.Service != serviceName ||
		strings.TrimSpace(record.InstanceID) == "" || strings.TrimSpace(record.Version) == "" {
		return nil, false
	}
	host, port, err := net.SplitHostPort(record.Address)
	if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
		return nil, false
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, false
	}
	plainHost := strings.TrimSuffix(strings.Trim(strings.ToLower(host), "[]"), ".")
	isLocalhostName := plainHost == "localhost" || strings.HasSuffix(plainHost, ".localhost")
	if isLocalhostName && !allowLoopback {
		return nil, false
	}
	if ip := net.ParseIP(plainHost); ip != nil {
		if ip.IsUnspecified() || (ip.IsLoopback() && !allowLoopback) {
			return nil, false
		}
	}
	return discovery.NewInstance("tcp", record.Address, discovery.DefaultWeight, map[string]string{
		"instance_id": record.InstanceID, "version": record.Version,
	}), true
}

// Diff 使用 Kitex 默认地址与权重规则计算实例变化。
func (resolver *EtcdResolver) Diff(cacheKey string, previous, next discovery.Result) (discovery.Change, bool) {
	return discovery.DefaultDiff(cacheKey, previous, next)
}

// Name 返回诊断使用的固定 Resolver 名称。
func (resolver *EtcdResolver) Name() string { return "dora-etcd-agent-session-v1" }

// Close 关闭 Resolver 自有 etcd Client。
func (resolver *EtcdResolver) Close() error {
	if err := resolver.client.Close(); err != nil {
		return fmt.Errorf("close Agent Session RPC etcd resolver: %w", err)
	}
	return nil
}
