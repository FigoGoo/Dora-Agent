// Package etcdcheck 负责 Business Worker 的 etcd 发现依赖探针。
package etcdcheck

import (
	"context"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Client 封装 Worker 使用的 etcd Client 生命周期；当前不注册无 RPC 能力的 Worker。
type Client struct{ client *clientv3.Client }

// Open 创建 etcd Client 并检查至少一个 Endpoint 可用。
func Open(ctx context.Context, cfg config.EtcdConfig) (*Client, error) {
	client, err := clientv3.New(clientv3.Config{Endpoints: cfg.Endpoints, DialTimeout: cfg.DialTimeout})
	if err != nil {
		return nil, fmt.Errorf("open worker etcd client: %w", err)
	}
	var lastStatusErr error
	for _, endpoint := range cfg.Endpoints {
		statusCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
		_, lastStatusErr = client.Status(statusCtx, endpoint)
		cancel()
		if lastStatusErr == nil {
			return &Client{client: client}, nil
		}
	}
	_ = client.Close()
	return nil, fmt.Errorf("check worker etcd endpoints: %w", lastStatusErr)
}

// Close 关闭 Worker etcd Client。
func (c *Client) Close() error {
	if err := c.client.Close(); err != nil {
		return fmt.Errorf("close worker etcd client: %w", err)
	}
	return nil
}
