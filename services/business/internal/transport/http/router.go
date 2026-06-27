package http

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/logger"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

type ReadyChecker func(context.Context) error

type RouterOptions struct {
	Logger *slog.Logger
	Ready  ReadyChecker
}

func NewRouter(opts RouterOptions) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	router.Use(traceMiddleware())
	router.Use(requestMetaMiddleware())
	router.Use(requestLogMiddleware(log))
	router.Use(errorMiddleware())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(nethttp.StatusOK, gin.H{"status": "ok", "service": "business"})
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
		c.JSON(nethttp.StatusOK, gin.H{"status": "ready", "service": "business"})
	})

	return router
}

func traceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader("X-Trace-Id")
		if traceID == "" {
			traceID = c.GetHeader("X-Request-Id")
		}
		if traceID == "" {
			traceID = "local-" + time.Now().UTC().Format("20060102150405.000000000")
		}
		c.Request = c.Request.WithContext(logger.WithTraceID(c.Request.Context(), traceID))
		c.Header("X-Trace-Id", traceID)
		c.Next()
	}
}

func requestMetaMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("request_id", c.GetHeader("X-Request-Id"))
		c.Set("idempotency_key", c.GetHeader("Idempotency-Key"))
		c.Set("actor_user_id", c.GetHeader("X-Actor-User-Id"))
		c.Next()
	}
}

func requestLogMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		log.InfoContext(c.Request.Context(), "business_http_request",
			"trace_id", logger.TraceID(c.Request.Context()),
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
		err := bizerrors.FromError(c.Errors.Last().Err)
		err.TraceID = logger.TraceID(c.Request.Context())
		c.JSON(err.HTTPStatus(), gin.H{
			"error": gin.H{
				"code":      err.Code,
				"message":   err.Message,
				"trace_id":  err.TraceID,
				"retryable": err.Retryable,
				"details":   err.Details,
			},
		})
	}
}
