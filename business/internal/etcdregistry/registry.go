// Package etcdregistry 负责 Business Service 的 etcd 注册发现生命周期。
package etcdregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Endpoint 描述同一 Business 进程要在一个租约下发布的可访问 Endpoint。
type Endpoint struct {
	// Service 是消费者用于发现的稳定服务名。
	Service string
	// InstanceID 是本次进程实例标识。
	InstanceID string
	// Address 是消费者可访问的 Advertised Address。
	Address string
	// Version 是构建版本。
	Version string
}

// Registration 是写入 etcd 的最小服务注册值，不包含业务配置或凭据。
type Registration struct {
	// Service 是稳定服务名。
	Service string `json:"service"`
	// InstanceID 是本次进程实例标识。
	InstanceID string `json:"instance_id"`
	// Address 是其他服务可访问的地址。
	Address string `json:"address"`
	// Version 是构建版本。
	Version string `json:"version"`
	// RegisteredAt 是 UTC 注册时间。
	RegisteredAt time.Time `json:"registered_at"`
}

// Registry 管理 etcd Client、注册租约和 KeepAlive goroutine。
type Registry struct {
	client   *clientv3.Client
	leaseID  clientv3.LeaseID
	cancel   context.CancelFunc
	wait     sync.WaitGroup
	errors   chan error
	closeOne sync.Once
}

// Open 创建 etcd Client 并检查至少一个 Endpoint 可用。
func Open(ctx context.Context, cfg config.EtcdConfig) (*Registry, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("open business etcd client: %w", err)
	}
	var lastStatusErr error
	for _, endpoint := range cfg.Endpoints {
		statusCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
		_, lastStatusErr = client.Status(statusCtx, endpoint)
		cancel()
		if lastStatusErr == nil {
			return &Registry{client: client, errors: make(chan error, 1)}, nil
		}
	}
	_ = client.Close()
	return nil, fmt.Errorf("check business etcd endpoints: %w", lastStatusErr)
}

// Register 在一个租约下发布全部 Endpoint，并持续监控 KeepAlive 是否意外中断。
func (r *Registry) Register(ctx context.Context, endpoints []Endpoint, leaseTTL time.Duration) error {
	if r.leaseID != 0 {
		return fmt.Errorf("business service is already registered")
	}
	if len(endpoints) == 0 {
		return fmt.Errorf("at least one business endpoint is required")
	}
	ttlSeconds := int64(leaseTTL / time.Second)
	lease, err := r.client.Grant(ctx, ttlSeconds)
	if err != nil {
		return fmt.Errorf("grant business registration lease: %w", err)
	}

	registeredAt := time.Now().UTC()
	seenServices := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint.Service) == "" || strings.TrimSpace(endpoint.InstanceID) == "" ||
			strings.TrimSpace(endpoint.Address) == "" || strings.TrimSpace(endpoint.Version) == "" {
			r.revokeLease(ctx, lease.ID)
			return fmt.Errorf("business endpoint fields are required")
		}
		if _, exists := seenServices[endpoint.Service]; exists {
			r.revokeLease(ctx, lease.ID)
			return fmt.Errorf("duplicate business endpoint service %q", endpoint.Service)
		}
		seenServices[endpoint.Service] = struct{}{}

		payload, err := json.Marshal(Registration{
			Service: endpoint.Service, InstanceID: endpoint.InstanceID, Address: endpoint.Address,
			Version: endpoint.Version, RegisteredAt: registeredAt,
		})
		if err != nil {
			r.revokeLease(ctx, lease.ID)
			return fmt.Errorf("encode business registration %q: %w", endpoint.Service, err)
		}
		key := fmt.Sprintf("/dora/services/%s/%s", endpoint.Service, endpoint.InstanceID)
		if _, err := r.client.Put(ctx, key, string(payload), clientv3.WithLease(lease.ID)); err != nil {
			r.revokeLease(ctx, lease.ID)
			return fmt.Errorf("put business registration %q: %w", endpoint.Service, err)
		}
	}

	keepCtx, cancel := context.WithCancel(ctx)
	keepAlive, err := r.client.KeepAlive(keepCtx, lease.ID)
	if err != nil {
		cancel()
		r.revokeLease(ctx, lease.ID)
		return fmt.Errorf("start business registration keepalive: %w", err)
	}
	r.leaseID = lease.ID
	r.cancel = cancel
	r.wait.Add(1)
	go r.monitorKeepAlive(keepCtx, keepAlive)
	return nil
}

// revokeLease 尽力清理尚未进入 Registry 生命周期管理的临时租约。
func (r *Registry) revokeLease(ctx context.Context, leaseID clientv3.LeaseID) {
	_, _ = r.client.Revoke(ctx, leaseID)
}

// Errors 返回租约意外丢失通知；调用方必须将实例切为未就绪。
func (r *Registry) Errors() <-chan error {
	return r.errors
}

// monitorKeepAlive 由 Registry 持有并在 Close 时等待，避免无 Owner goroutine。
func (r *Registry) monitorKeepAlive(ctx context.Context, keepAlive <-chan *clientv3.LeaseKeepAliveResponse) {
	defer r.wait.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case response, ok := <-keepAlive:
			if !ok || response == nil {
				select {
				case r.errors <- fmt.Errorf("business registration keepalive stopped"):
				default:
				}
				return
			}
		}
	}
}

// Close 先停止 KeepAlive、撤销租约，再关闭 etcd Client。
func (r *Registry) Close(ctx context.Context) error {
	var closeErr error
	r.closeOne.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		r.wait.Wait()
		if r.leaseID != 0 {
			if _, err := r.client.Revoke(ctx, r.leaseID); err != nil {
				closeErr = fmt.Errorf("revoke business registration lease: %w", err)
			}
		}
		if err := r.client.Close(); err != nil && closeErr == nil {
			closeErr = fmt.Errorf("close business etcd client: %w", err)
		}
	})
	return closeErr
}
