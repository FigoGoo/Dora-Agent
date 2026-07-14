// Package httpserver 提供 Business Worker 的 Gin 健康检查生命周期。
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/FigoGoo/Dora-Agent/worker/internal/config"
	"github.com/FigoGoo/Dora-Agent/worker/internal/health"
	"github.com/gin-gonic/gin"
)

// HealthResponse 是 Worker 健康接口使用的稳定 DTO。
type HealthResponse struct {
	// Status 是 alive、ready 或 not_ready。
	Status string `json:"status"`
	// Service 是稳定服务名。
	Service string `json:"service"`
	// Version 是构建版本。
	Version string `json:"version"`
}

// Server 封装 Worker 健康检查 HTTP Server。
type Server struct{ server *http.Server }

// New 使用显式超时和请求头上限创建健康检查 Server。
func New(httpCfg config.HTTPConfig, serviceCfg config.ServiceConfig, state *health.State) (*Server, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	if err := router.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("disable worker trusted proxies: %w", err)
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
	return &Server{server: &http.Server{
		Addr: httpCfg.Address, Handler: router, ReadHeaderTimeout: httpCfg.HeaderTimeout,
		ReadTimeout: httpCfg.ReadTimeout, WriteTimeout: httpCfg.WriteTimeout,
		IdleTimeout: httpCfg.IdleTimeout, MaxHeaderBytes: httpCfg.MaxHeaderBytes,
	}}, nil
}

// Listen 先绑定监听地址，使调用方只在端口可用后宣告就绪。
func (s *Server) Listen() (net.Listener, error) {
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return nil, fmt.Errorf("listen worker http: %w", err)
	}
	return listener, nil
}

// Serve 在已绑定 Listener 上提供健康检查，正常关闭时返回 nil。
func (s *Server) Serve(listener net.Listener) error {
	if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve worker http: %w", err)
	}
	return nil
}

// Shutdown 在有界 Context 内停止健康检查 Server。
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown worker http: %w", err)
	}
	return nil
}

// Handler 返回 HTTP Handler，仅用于无网络单元测试。
func (s *Server) Handler() http.Handler { return s.server.Handler }
