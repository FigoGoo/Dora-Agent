// Package httpserver 提供 Business Service 的 Gin HTTP 生命周期和健康接口。
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/health"
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

// Server 封装 Business HTTP Server 及其只读健康投影。
type Server struct {
	server *http.Server
}

// RouteHandlers 汇总 Business Runtime 的版本化 HTTP 纵切；零值只用于基础健康检查测试。
type RouteHandlers struct {
	// Auth 注册会话创建、查询和撤销路由，并提供受保护资源中间件。
	Auth *AuthHandler
	// Project 注册 Quick Create 与资源级 Bootstrap 路由。
	Project *ProjectHandler
	// Agent 注册同源 Workspace Snapshot 与 EventLog SSE 固定代理路由。
	Agent *AgentProxyHandler
	// Skill 注册 W1 Skill Owner 草稿、列表、详情、替换和审核提交路由。
	Skill *SkillHandler
	// SkillReview 注册 W1-C2 Reviewer 待审队列、冻结详情和批准决定路由。
	SkillReview *SkillReviewHandler
}

// New 使用显式超时、请求头上限和可信代理配置创建 HTTP Server；可选 RouteHandlers 用于完整 Runtime 接线。
func New(httpCfg config.HTTPConfig, serviceCfg config.ServiceConfig, state *health.State, routeHandlers ...RouteHandlers) (*Server, error) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(safeRecovery())
	if err := router.SetTrustedProxies(nil); err != nil {
		return nil, fmt.Errorf("disable business trusted proxies: %w", err)
	}
	if len(routeHandlers) > 1 {
		return nil, fmt.Errorf("register business HTTP handlers: invalid handler count")
	}
	if len(routeHandlers) == 1 {
		handlers := routeHandlers[0]
		if handlers.Auth == nil || handlers.Project == nil || handlers.Agent == nil || handlers.Skill == nil || handlers.SkillReview == nil {
			return nil, fmt.Errorf("register business HTTP handlers: Auth, Project, Agent, Skill, and SkillReview handlers are required together")
		}
		handlers.Auth.Register(router)
		handlers.Project.Register(router, handlers.Auth.RequireSession(), handlers.Auth.RequireSessionAndCSRF())
		handlers.Agent.Register(router, handlers.Auth.RequireSession())
		handlers.Skill.Register(router, handlers.Auth.RequireSession(), handlers.Auth.RequireSessionAndCSRF())
		handlers.SkillReview.Register(router, handlers.Auth.RequireSession(), handlers.Auth.RequireSessionAndCSRF())
	}

	router.GET("/livez", func(c *gin.Context) {
		c.JSON(http.StatusOK, HealthResponse{
			Status:  "alive",
			Service: serviceCfg.Name,
			Version: serviceCfg.Version,
		})
	})
	router.GET("/readyz", func(c *gin.Context) {
		if !state.IsReady() {
			c.JSON(http.StatusServiceUnavailable, HealthResponse{
				Status:  "not_ready",
				Service: serviceCfg.Name,
				Version: serviceCfg.Version,
			})
			return
		}
		c.JSON(http.StatusOK, HealthResponse{
			Status:  "ready",
			Service: serviceCfg.Name,
			Version: serviceCfg.Version,
		})
	})

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

// safeRecovery 捕获 Handler panic，输出统一安全 Envelope，并且绝不转储 Cookie、CSRF、Body、Query 或 panic 原值。
func safeRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				route := c.FullPath()
				if route == "" {
					route = "unmatched"
				}
				slog.Error("Business HTTP 请求发生未处理异常", "method", c.Request.Method, "route", route)
				if !c.Writer.Written() {
					c.Header("Cache-Control", "no-store")
					c.JSON(http.StatusInternalServerError, ErrorResponse{Error: ErrorBody{
						Code: "INTERNAL_ERROR", Message: "服务暂时不可用", RequestID: authEmergencyRequestID,
						Retryable: true, Details: ErrorDetails{},
					}})
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

// Listen 先绑定监听地址，使调用方只在端口可用后注册服务。
func (s *Server) Listen() (net.Listener, error) {
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return nil, fmt.Errorf("listen business http: %w", err)
	}
	return listener, nil
}

// Serve 在已绑定 Listener 上提供 HTTP 服务，正常关闭时返回 nil。
func (s *Server) Serve(listener net.Listener) error {
	if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve business http: %w", err)
	}
	return nil
}

// Shutdown 在调用方提供的有界 Context 内停止接收请求并等待连接收尾。
func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown business http: %w", err)
	}
	return nil
}

// Handler 返回 HTTP Handler，仅用于无网络单元测试。
func (s *Server) Handler() http.Handler {
	return s.server.Handler
}
