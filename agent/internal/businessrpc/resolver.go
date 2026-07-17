// Package businessrpc 提供 Agent 调用 Business Foundation RPC 的 Kitex Client 和 etcd 发现。
package businessrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/cloudwego/kitex/pkg/discovery"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// registrationRecordV1 是 Foundation RPC v1 冻结的 etcd Endpoint JSON 投影。
type registrationRecordV1 struct {
	Service    string `json:"service"`
	InstanceID string `json:"instance_id"`
	Address    string `json:"address"`
	Version    string `json:"version"`
}

// EtcdResolver 从 Dora 稳定服务 Prefix 解析可访问的 Business RPC 实例。
type EtcdResolver struct {
	client        *clientv3.Client
	allowLoopback bool
}

// NewEtcdResolver 创建具有独立生命周期的 Resolver，并确认至少一个 etcd Endpoint 可用。
// allowLoopback 只供已经通过 Config local + exact Profile 门禁的单机 Preview 使用。
func NewEtcdResolver(ctx context.Context, cfg config.EtcdConfig, allowLoopback bool) (*EtcdResolver, error) {
	client, err := clientv3.New(clientv3.Config{Endpoints: cfg.Endpoints, DialTimeout: cfg.DialTimeout})
	if err != nil {
		return nil, fmt.Errorf("open Business RPC etcd resolver: %w", err)
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
	return nil, fmt.Errorf("check Business RPC resolver endpoints: %w", lastStatusErr)
}

// Target 返回 Kitex 用作发现目标的稳定服务名。
func (r *EtcdResolver) Target(_ context.Context, target rpcinfo.EndpointInfo) string {
	return target.ServiceName()
}

// Resolve 读取当前租约下的合法 Endpoint；格式错误记录被隔离，全部非法时失败关闭。
func (r *EtcdResolver) Resolve(ctx context.Context, serviceName string) (discovery.Result, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" || strings.Contains(serviceName, "/") {
		return discovery.Result{}, fmt.Errorf("invalid Business RPC discovery service name")
	}
	prefix := "/dora/services/" + serviceName + "/"
	response, err := r.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return discovery.Result{}, fmt.Errorf("resolve Business RPC endpoints: %w", err)
	}
	instances := make([]discovery.Instance, 0, len(response.Kvs))
	for _, item := range response.Kvs {
		if instance, ok := parseRegistrationInstance(serviceName, item.Value, r.allowLoopback); ok {
			instances = append(instances, instance)
		}
	}
	if len(instances) == 0 {
		return discovery.Result{}, fmt.Errorf("no valid Business RPC instance for %q", serviceName)
	}
	// 不声明 Cacheable，确保无 Watcher 的 v1 Resolver 每次都观察当前租约集合。
	return discovery.Result{Cacheable: false, Instances: instances}, nil
}

// parseRegistrationInstance 把不可信 etcd Value 收敛为 Kitex Instance；任何不完整或不可访问记录都被隔离。
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
	plainHost := strings.Trim(strings.ToLower(host), "[]")
	if plainHost == "localhost" && !allowLoopback {
		return nil, false
	}
	if ip := net.ParseIP(plainHost); ip != nil {
		if ip.IsUnspecified() || (ip.IsLoopback() && !allowLoopback) {
			return nil, false
		}
	}
	return discovery.NewInstance("tcp", record.Address, discovery.DefaultWeight, map[string]string{
		"instance_id": record.InstanceID,
		"version":     record.Version,
	}), true
}

// Diff 使用 Kitex 默认地址和权重比较规则计算实例变化。
func (r *EtcdResolver) Diff(cacheKey string, previous, next discovery.Result) (discovery.Change, bool) {
	return discovery.DefaultDiff(cacheKey, previous, next)
}

// Name 返回日志和诊断使用的 Resolver 名称。
func (r *EtcdResolver) Name() string {
	return "dora-etcd-foundation-v1"
}

// Close 关闭 Resolver 自有的 etcd Client。
func (r *EtcdResolver) Close() error {
	if err := r.client.Close(); err != nil {
		return fmt.Errorf("close Business RPC etcd resolver: %w", err)
	}
	return nil
}
