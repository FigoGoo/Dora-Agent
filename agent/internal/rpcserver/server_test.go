package rpcserver

import (
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
)

// TestNewRejectsMissingHandler 验证 Composition Root 漏装 Session Handler 时不会打开 RPC 监听端口。
func TestNewRejectsMissingHandler(t *testing.T) {
	server, err := New(config.RPCConfig{
		ListenAddress: ":0", ReadWriteTimeout: time.Second, MaxConnectionIdleTime: time.Minute,
	}, config.SessionRPCAuthConfig{
		SharedSecret: []byte("0123456789abcdef0123456789abcdef"), MaxClockSkew: 30 * time.Second,
	}, time.Second, nil)
	if err == nil || server != nil {
		t.Fatalf("缺失 Handler 仍创建 Server: server=%v err=%v", server, err)
	}
}
