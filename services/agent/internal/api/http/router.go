package http

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/tracectx"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/apperror"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/observability"
	"github.com/gin-gonic/gin"
)

type ReadyChecker func(context.Context) error

type RouterOptions struct {
	Logger *slog.Logger
	Ready  ReadyChecker
	App    *workbench.App
}

func NewRouter(opts RouterOptions) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	router.Use(traceMiddleware())
	router.Use(requestLogMiddleware(logger))
	router.Use(errorMiddleware())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(nethttp.StatusOK, gin.H{"status": "ok", "service": "agent"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		if opts.Ready != nil {
			ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
			defer cancel()
			if err := opts.Ready(ctx); err != nil {
				c.JSON(nethttp.StatusServiceUnavailable, gin.H{"status": "unready", "error": "dependency_unavailable"})
				return
			}
		}
		c.JSON(nethttp.StatusOK, gin.H{"status": "ready", "service": "agent"})
	})
	registerWorkbenchRoutes(router, opts.App)

	return router
}

func traceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := tracectx.FromHTTPHeaders(c.Request.Context(), c.Request.Header)
		c.Request = c.Request.WithContext(ctx)
		tracectx.InjectHTTPHeaders(ctx, c.Writer.Header())
		c.Next()
	}
}

func requestLogMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		traceID := observability.TraceID(c.Request.Context())
		logger.InfoContext(c.Request.Context(), "agent_http_request",
			"trace_id", traceID,
			"method", c.Request.Method,
			"path", c.FullPath(),
			"status", c.Writer.Status(),
			"latency_ms", time.Since(started).Milliseconds(),
		)
	}
}

func errorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		err := apperror.FromError(c.Errors.Last().Err)
		err.TraceID = observability.TraceID(c.Request.Context())
		c.JSON(err.HTTPStatus(), gin.H{
			"error": gin.H{
				"code":      err.Code,
				"category":  err.Category(),
				"message":   err.Message,
				"trace_id":  err.TraceID,
				"retryable": err.Retryable,
			},
		})
	}
}
