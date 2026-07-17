// Package httpserver 提供 Agent Service 的 Gin HTTP 生命周期和健康接口。
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

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

// New 使用显式超时、请求头上限、可信代理配置和必需只读 Handler 创建 HTTP Server。
func New(
	httpCfg config.HTTPConfig,
	serviceCfg config.ServiceConfig,
	state *health.State,
	workspaceHandler *WorkspaceHandler,
	toolCatalogHandler *ToolCatalogHandler,
	disabledPreviewIDs IDGenerator,
	previewHandlers ...*CreationSpecPreviewHandler,
) (*Server, error) {
	if len(previewHandlers) > 1 {
		return nil, fmt.Errorf("create agent HTTP server: health state and read handlers are required")
	}
	var previewHandler *CreationSpecPreviewHandler
	if len(previewHandlers) == 1 {
		previewHandler = previewHandlers[0]
	}
	return newServer(
		httpCfg, serviceCfg, state, workspaceHandler, toolCatalogHandler, disabledPreviewIDs,
		previewHandler, nil, nil, nil, nil, nil,
	)
}

// NewWithAnalyzeMaterials 创建可选素材分析 Runtime 写入口；配置层必须保证它与 CreationSpec Preview 互斥。
func NewWithAnalyzeMaterials(
	httpCfg config.HTTPConfig,
	serviceCfg config.ServiceConfig,
	state *health.State,
	workspaceHandler *WorkspaceHandler,
	toolCatalogHandler *ToolCatalogHandler,
	disabledPreviewIDs IDGenerator,
	creationSpecHandler *CreationSpecPreviewHandler,
	analyzeMaterialsHandler *AnalyzeMaterialsPreviewHandler,
) (*Server, error) {
	return newServer(
		httpCfg, serviceCfg, state, workspaceHandler, toolCatalogHandler, disabledPreviewIDs,
		creationSpecHandler, analyzeMaterialsHandler, nil, nil, nil, nil,
	)
}

// NewWithPlanStoryboard 创建可选故事板规划 Runtime 写入口；配置层必须保证全部开发预览 Runtime 互斥。
func NewWithPlanStoryboard(
	httpCfg config.HTTPConfig,
	serviceCfg config.ServiceConfig,
	state *health.State,
	workspaceHandler *WorkspaceHandler,
	toolCatalogHandler *ToolCatalogHandler,
	disabledPreviewIDs IDGenerator,
	creationSpecHandler *CreationSpecPreviewHandler,
	analyzeMaterialsHandler *AnalyzeMaterialsPreviewHandler,
	planStoryboardHandler *PlanStoryboardPreviewHandler,
) (*Server, error) {
	return newServer(
		httpCfg, serviceCfg, state, workspaceHandler, toolCatalogHandler, disabledPreviewIDs,
		creationSpecHandler, analyzeMaterialsHandler, planStoryboardHandler, nil, nil, nil,
	)
}

// NewWithWritePrompts 创建可选提示词生成 Runtime 写入口；配置层必须保证全部开发预览 Runtime 互斥。
func NewWithWritePrompts(
	httpCfg config.HTTPConfig,
	serviceCfg config.ServiceConfig,
	state *health.State,
	workspaceHandler *WorkspaceHandler,
	toolCatalogHandler *ToolCatalogHandler,
	disabledPreviewIDs IDGenerator,
	creationSpecHandler *CreationSpecPreviewHandler,
	analyzeMaterialsHandler *AnalyzeMaterialsPreviewHandler,
	planStoryboardHandler *PlanStoryboardPreviewHandler,
	writePromptsHandler *WritePromptsPreviewHandler,
) (*Server, error) {
	return newServer(
		httpCfg, serviceCfg, state, workspaceHandler, toolCatalogHandler, disabledPreviewIDs,
		creationSpecHandler, analyzeMaterialsHandler, planStoryboardHandler, writePromptsHandler, nil, nil,
	)
}

// NewWithMedia 创建统一基础 Profile，并可选注册两个媒体 typed ingress。
func NewWithMedia(
	httpCfg config.HTTPConfig,
	serviceCfg config.ServiceConfig,
	state *health.State,
	workspaceHandler *WorkspaceHandler,
	toolCatalogHandler *ToolCatalogHandler,
	disabledPreviewIDs IDGenerator,
	creationSpecHandler *CreationSpecPreviewHandler,
	analyzeMaterialsHandler *AnalyzeMaterialsPreviewHandler,
	planStoryboardHandler *PlanStoryboardPreviewHandler,
	writePromptsHandler *WritePromptsPreviewHandler,
	generateMediaHandler *GenerateMediaPreviewHandler,
	assembleOutputHandler *AssembleOutputPreviewHandler,
) (*Server, error) {
	return newServer(httpCfg, serviceCfg, state, workspaceHandler, toolCatalogHandler, disabledPreviewIDs,
		creationSpecHandler, analyzeMaterialsHandler, planStoryboardHandler, writePromptsHandler,
		generateMediaHandler, assembleOutputHandler)
}

func newServer(
	httpCfg config.HTTPConfig,
	serviceCfg config.ServiceConfig,
	state *health.State,
	workspaceHandler *WorkspaceHandler,
	toolCatalogHandler *ToolCatalogHandler,
	disabledPreviewIDs IDGenerator,
	previewHandler *CreationSpecPreviewHandler,
	analyzeMaterialsHandler *AnalyzeMaterialsPreviewHandler,
	planStoryboardHandler *PlanStoryboardPreviewHandler,
	writePromptsHandler *WritePromptsPreviewHandler,
	generateMediaHandler *GenerateMediaPreviewHandler,
	assembleOutputHandler *AssembleOutputPreviewHandler,
) (*Server, error) {
	if state == nil || workspaceHandler == nil || toolCatalogHandler == nil || disabledPreviewIDs == nil ||
		(generateMediaHandler == nil) != (assembleOutputHandler == nil) {
		return nil, fmt.Errorf("create agent HTTP server: health state, read handlers, and paired media handlers are required")
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	// 内部写路由要求 method + EscapedPath exact-match；框架级自动纠正会在身份校验前把错误路径变成 307/301。
	router.RedirectTrailingSlash = false
	router.RedirectFixedPath = false
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
	toolCatalogHandler.Register(router)
	if previewHandler != nil {
		previewHandler.Register(router)
	}
	if analyzeMaterialsHandler != nil {
		analyzeMaterialsHandler.Register(router)
	}
	if planStoryboardHandler != nil {
		planStoryboardHandler.Register(router)
	}
	if writePromptsHandler != nil {
		writePromptsHandler.Register(router)
	}
	if generateMediaHandler != nil {
		generateMediaHandler.Register(router)
	}
	if assembleOutputHandler != nil {
		assembleOutputHandler.Register(router)
	}
	// Flag-off 不注册写路由；NoRoute 只为未启用的 canonical Preview 形状返回稳定关闭码。
	router.NoRoute(func(c *gin.Context) {
		if c.Request.Method == http.MethodPost && c.Request.URL.RawQuery == "" {
			path := c.Request.URL.EscapedPath()
			if previewHandler == nil && canonicalDisabledPreviewPath(path, "/creation-spec-previews") {
				writeDisabledPreview(c, disabledPreviewIDs, "CreationSpec Preview 未启用")
				return
			}
			if analyzeMaterialsHandler == nil && canonicalDisabledPreviewPath(path, "/analyze-materials-previews") {
				writeDisabledPreview(c, disabledPreviewIDs, "素材分析 Preview 未启用")
				return
			}
			if planStoryboardHandler == nil && canonicalDisabledPreviewPath(path, "/plan-storyboard-previews") {
				writeDisabledPreview(c, disabledPreviewIDs, "故事板规划 Preview 未启用")
				return
			}
			if writePromptsHandler == nil && canonicalDisabledPreviewPath(path, "/write-prompts-previews") {
				writeDisabledPreview(c, disabledPreviewIDs, "提示词生成 Preview 未启用")
				return
			}
			if generateMediaHandler == nil && canonicalDisabledPreviewPath(path, "/generate-media-previews") {
				writeDisabledPreview(c, disabledPreviewIDs, "媒体生成 Preview 未启用")
				return
			}
			if assembleOutputHandler == nil && canonicalDisabledPreviewPath(path, "/assemble-output-previews") {
				writeDisabledPreview(c, disabledPreviewIDs, "媒体装配 Preview 未启用")
				return
			}
		}
		c.Status(http.StatusNotFound)
		c.Writer.WriteHeaderNow()
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

func enabledPreviewHandlerCount(enabled ...bool) int {
	count := 0
	for _, value := range enabled {
		if value {
			count++
		}
	}
	return count
}

// canonicalDisabledPreviewPath 只识别一个规范小写 UUIDv7 Session 与 exact suffix。
func canonicalDisabledPreviewPath(path, suffix string) bool {
	const prefix = "/internal/v1/workspaces/sessions/"
	rawSessionID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	sessionID, canonical := canonicalUUIDv7(rawSessionID)
	return canonical && path == prefix+sessionID+suffix
}

// writeDisabledPreview 为 canonical flag-off 路径返回 no-store 稳定错误；随机源异常时保持普通 404。
func writeDisabledPreview(c *gin.Context, ids IDGenerator, message string) {
	requestID, err := ids.New()
	if normalized, ok := canonicalUUIDv7(requestID); err != nil || !ok {
		c.Status(http.StatusNotFound)
		c.Writer.WriteHeaderNow()
		return
	} else {
		requestID = normalized
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusNotFound, ErrorResponse{Error: ErrorBody{
		Code: errorCodePreviewDisabled, Message: message,
		RequestID: requestID, Retryable: false, Details: ErrorDetails{},
	}})
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
