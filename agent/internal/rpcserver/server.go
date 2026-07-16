// Package rpcserver 管理 Agent Session Kitex Server 的监听和优雅退出。
package rpcserver

import (
	"fmt"
	"net"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/sessionauth"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/sessionv1/agentsessionservicev1"
	"github.com/cloudwego/kitex/pkg/rpcinfo"
	"github.com/cloudwego/kitex/pkg/transmeta"
	kitexserver "github.com/cloudwego/kitex/server"
)

// Server 封装已绑定 Listener 的 Agent Session Kitex Server，生命周期由 bootstrap 单独管理。
type Server struct {
	server   kitexserver.Server
	listener net.Listener
}

// New 先绑定 RPC 端口，再创建具有显式读写、空闲连接和退出预算的 Kitex Server。
// Server 不安装写操作自动重试；Ensure 的 Unknown Outcome 恢复由 Business 显式 Query 决定。
func New(
	cfg config.RPCConfig,
	authConfig config.SessionRPCAuthConfig,
	shutdownTimeout time.Duration,
	handler sessionv1.AgentSessionServiceV1,
) (*Server, error) {
	if handler == nil {
		return nil, fmt.Errorf("Agent Session RPC Handler 不能为空")
	}
	authenticator, err := sessionauth.New(authConfig.SharedSecret, authConfig.MaxClockSkew)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("listen Agent Session RPC: %w", err)
	}
	server := agentsessionservicev1.NewServer(handler,
		kitexserver.WithListener(listener),
		kitexserver.WithServerBasicInfo(&rpcinfo.EndpointBasicInfo{ServiceName: sessionv1.AGENT_SESSION_SERVICE_NAME}),
		kitexserver.WithReadWriteTimeout(cfg.ReadWriteTimeout),
		kitexserver.WithMaxConnIdleTime(cfg.MaxConnectionIdleTime),
		kitexserver.WithExitWaitTime(shutdownTimeout),
		kitexserver.WithMetaHandler(transmeta.ServerTTHeaderHandler),
		kitexserver.WithMetaHandler(transmeta.MetainfoServerHandler),
		kitexserver.WithMiddleware(authenticator.Middleware()),
		kitexserver.WithEnableContextTimeout(true),
	)
	return &Server{server: server, listener: listener}, nil
}

// Address 返回已经绑定的 RPC Listener 地址，仅用于线级测试和生命周期观测，不用于服务注册。
func (s *Server) Address() net.Addr { return s.listener.Addr() }

// Serve 启动 Kitex 事件循环；正常 Stop 返回 nil。
func (s *Server) Serve() error {
	if err := s.server.Run(); err != nil {
		return fmt.Errorf("serve Agent Session RPC: %w", err)
	}
	return nil
}

// Stop 停止接收新 Session RPC，并在 Kitex 退出预算内等待连接收尾。
func (s *Server) Stop() error {
	if err := s.server.Stop(); err != nil {
		return fmt.Errorf("stop Agent Session RPC: %w", err)
	}
	return nil
}
