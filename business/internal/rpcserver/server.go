// Package rpcserver 管理 Business Foundation Kitex Server 的监听和优雅退出。
package rpcserver

import (
	"fmt"
	"net"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1/businessfoundationservicev1"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	kitexserver "github.com/cloudwego/kitex/server"
)

// Server 封装已绑定 Listener 的 Foundation Kitex Server。
type Server struct {
	server kitexserver.Server
}

// New 先绑定 RPC 端口，再创建具有显式资源超时的 Kitex Server。
func New(cfg config.RPCConfig, shutdownTimeout time.Duration, handler foundationv1.BusinessFoundationServiceV1) (*Server, error) {
	if handler == nil {
		return nil, fmt.Errorf("foundation RPC handler is required")
	}
	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen business foundation RPC: %w", err)
	}
	server := businessfoundationservicev1.NewServer(handler,
		kitexserver.WithListener(listener),
		kitexserver.WithServerBasicInfo(&rpcinfo.EndpointBasicInfo{
			ServiceName: foundationv1.BUSINESS_FOUNDATION_SERVICE_NAME,
		}),
		kitexserver.WithReadWriteTimeout(cfg.ReadWriteTimeout),
		kitexserver.WithMaxConnIdleTime(cfg.MaxConnectionIdleTime),
		kitexserver.WithExitWaitTime(shutdownTimeout),
	)
	return &Server{server: server}, nil
}

// Serve 启动 Kitex 事件循环；正常 Stop 返回 nil。
func (s *Server) Serve() error {
	if err := s.server.Run(); err != nil {
		return fmt.Errorf("serve business foundation RPC: %w", err)
	}
	return nil
}

// Stop 停止接收新 RPC，并在 Kitex 退出预算内等待连接收尾。
func (s *Server) Stop() error {
	if err := s.server.Stop(); err != nil {
		return fmt.Errorf("stop business foundation RPC: %w", err)
	}
	return nil
}
