package http

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/asset"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetcommit"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetdict"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/credit"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/modelconfig"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/skillcatalog"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/toolpolicy"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/work"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/logger"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

type ReadyChecker func(context.Context) error

type RouterOptions struct {
	Logger       *slog.Logger
	Ready        ReadyChecker
	AccountSpace *accountspace.App
	Admin        *admin.App
	Project      *project.App
	Model        *modelconfig.App
	Tool         *toolpolicy.App
	Skill        *skillcatalog.App
	Dictionary   *assetdict.App
	Credit       *credit.App
	Asset        *asset.App
	Commit       *assetcommit.App
	Work         *work.App
	Notification *notification.App
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

	registerM2Routes(router, opts)
	registerM3Routes(router, opts)
	registerM4Routes(router, opts)
	registerM5Routes(router, opts)

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
		requestID := c.GetHeader("X-Request-Id")
		c.Set("request_id", requestID)
		c.Set("idempotency_key", c.GetHeader("Idempotency-Key"))
		c.Set("actor_user_id", c.GetHeader("X-Actor-User-Id"))
		c.Request = c.Request.WithContext(logger.WithRequestID(c.Request.Context(), requestID))
		c.Next()
	}
}

func requestLogMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		log.InfoContext(c.Request.Context(), "business_http_request",
			logger.FieldTraceID, logger.TraceID(c.Request.Context()),
			logger.FieldRequestID, logger.RequestID(c.Request.Context()),
			logger.FieldMethod, c.Request.Method,
			logger.FieldPath, c.FullPath(),
			logger.FieldStatus, c.Writer.Status(),
			logger.FieldLatencyMS, time.Since(started).Milliseconds(),
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
		details := err.Details
		if err.Code == bizerrors.CodeUnauthenticated {
			details = loginRequiredDetails(c, details)
		}
		c.JSON(err.HTTPStatus(), gin.H{
			"error": gin.H{
				"code":      err.Code,
				"category":  err.Category(),
				"message":   err.Message,
				"trace_id":  err.TraceID,
				"retryable": err.Retryable,
				"details":   details,
			},
		})
	}
}

func loginRequiredDetails(c *gin.Context, existing map[string]string) map[string]string {
	details := map[string]string{}
	for key, value := range existing {
		details[key] = value
	}
	details["login_required"] = "true"
	details["return_to"] = c.Request.URL.RequestURI()
	intentPath := c.FullPath()
	if intentPath == "" {
		intentPath = c.Request.URL.Path
	}
	details["pending_intent"] = strings.ToUpper(c.Request.Method) + " " + intentPath
	return details
}
