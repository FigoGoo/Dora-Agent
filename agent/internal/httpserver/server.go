// Package httpserver 提供 Agent Service 的 Gin HTTP 生命周期和健康接口。
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/health"
	"github.com/gin-gonic/gin"
)

// HealthResponse 是 Liveness 和 Readiness 使用的稳定 HTTP DTO。
type HealthResponse struct {
	// Status 是 alive、ready 或 not_ready。
	Status string `json:"status"`
	// Service 是稳定服务名。
	Service string `json:"service"`
	// Version 是构建版本。
	Version string `json:"version"`
}

// Server 封装 Agent HTTP Server 及其只读健康投影。
type Server struct {
	server *http.Server
}

// New 使用显式超时、请求头上限、可信代理配置和必需 Workspace Transport 创建 HTTP Server。
func New(
	httpCfg config.HTTPConfig,
	serviceCfg config.ServiceConfig,
	state *health.State,
	workspaceHandler *WorkspaceHandler,
) (*Server, error) {
	if state == nil || workspaceHandler == nil {
		return nil, fmt.Errorf("create agent HTTP server: health state and Workspace handler are required")
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	if err := router.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("disable agent trusted proxies: %w", err)
	}
	router.GET("/livez", func(c *gin.Context) {
		c.JSON(http.StatusOK, HealthResponse{Status: "alive", Service: serviceCfg.Name, Version: serviceCfg.Version})
	})
	router.GET("/readyz", func(c *gin.Context) {
		if !state.IsReady() {
			c.JSON(http.StatusServiceUnavailable, HealthResponse{Status: "not_ready", Service: serviceCfg.Name, Version: serviceCfg.Version})
			return
		}
		c.JSON(http.StatusOK, HealthResponse{Status: "ready", Service: serviceCfg.Name, Version: serviceCfg.Version})
	})
	workspaceHandler.Register(router)
	return &Server{server: &http.Server{
		Addr:              httpCfg.Address,
		Handler:           router,
		ReadHeaderTimeout: httpCfg.HeaderTimeout,
		ReadTimeout:       httpCfg.ReadTimeout,
		WriteTimeout:      httpCfg.WriteTimeout,
		IdleTimeout:       httpCfg.IdleTimeout,
		MaxHeaderBytes:    httpCfg.MaxHeaderBytes,
	}}, nil
}

// Listen 先绑定监听地址，使调用方只在端口可用后注册服务。
func (s *Server) Listen() (net.Listener, error) {
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return nil, fmt.Errorf("listen agent http: %w", err)
	}
	return listener, nil
}

// Serve 在已绑定 Listener 上提供 HTTP 服务，正常关闭时返回 nil。
func (s *Server) Serve(listener net.Listener) error {
	if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve agent http: %w", err)
	}
	return nil
}

// Shutdown 在调用方提供的有界 Context 内停止接收请求并等待连接收尾。
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown agent http: %w", err)
	}
	return nil
}

// Handler 返回 HTTP Handler，仅用于无网络单元测试。
func (s *Server) Handler() http.Handler {
	return s.server.Handler
}
