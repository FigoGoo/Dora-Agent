package http

import (
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/agent/internal/apperror"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/observability"
	"github.com/gin-gonic/gin"
)

func registerWorkbenchRoutes(router *gin.Engine, app *workbench.App) {
	h := workbenchHandler{app: app}
	router.POST("/api/agent/sessions", h.authRequired(), h.createSession)
	router.GET("/api/agent/sessions/:session_id", h.authRequired(), h.getSession)
	router.GET("/api/agent/sessions/:session_id/messages", h.authRequired(), h.listMessages)
	router.POST("/api/agent/runs", h.authRequired(), h.createRun)
	router.GET("/api/agent/runs/:run_id", h.authRequired(), h.getRun)
	router.GET("/api/agent/runs/:run_id/stream", h.authRequired(), h.openRunStream)
	router.GET("/api/agent/runs/:run_id/events", h.authRequired(), h.replayEvents)
	router.POST("/api/agent/runs/:run_id/messages", h.authRequired(), h.appendUserInput)
	router.POST("/api/agent/runs/:run_id/interrupts/:interrupt_id/accept", h.authRequired(), h.acceptInterrupt)
	router.POST("/api/agent/runs/:run_id/interrupts/:interrupt_id/reject", h.authRequired(), h.rejectInterrupt)
	router.POST("/api/agent/runs/:run_id/cancel", h.authRequired(), h.cancelRun)
	router.GET("/api/agent/runs/:run_id/snapshot", h.authRequired(), h.getRunSnapshot)
}

type workbenchHandler struct {
	app *workbench.App
}

func (h workbenchHandler) createSession(c *gin.Context) {
	var req workbench.CreateSessionRequest
	if !bind(c, &req) {
		return
	}
	req.IdempotencyKey = idempotencyKey(c, req.IdempotencyKey)
	out, err := h.app.CreateSession(c.Request.Context(), auth(c), req, traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) getSession(c *gin.Context) {
	out, err := h.app.GetSession(c.Request.Context(), auth(c), c.Param("session_id"), traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) listMessages(c *gin.Context) {
	out, err := h.app.ListMessages(c.Request.Context(), auth(c), c.Param("session_id"), intQuery(c, "limit", 10), intQuery(c, "offset", 0), traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) createRun(c *gin.Context) {
	var req workbench.CreateRunRequest
	if !bind(c, &req) {
		return
	}
	req.IdempotencyKey = idempotencyKey(c, req.IdempotencyKey)
	out, err := h.app.CreateRun(c.Request.Context(), auth(c), req, traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) getRun(c *gin.Context) {
	out, err := h.app.GetRun(c.Request.Context(), auth(c), c.Param("run_id"), traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) openRunStream(c *gin.Context) {
	after := int64(0)
	if lastEventID := c.GetHeader("Last-Event-ID"); lastEventID != "" {
		if parsed, err := strconv.ParseInt(lastEventID, 10, 64); err == nil {
			after = parsed
		}
	}
	out, err := h.app.ReplayEvents(c.Request.Context(), auth(c), c.Param("run_id"), after, 200, traceID(c))
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(nethttp.StatusOK)
	writeSSEEvents(c, out.Events)
	if !strings.Contains(c.GetHeader("Accept"), "text/event-stream") {
		return
	}
	next := out.NextSequence
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeat.C:
			replay, replayErr := h.app.ReplayEvents(c.Request.Context(), auth(c), c.Param("run_id"), next, 200, traceID(c))
			if replayErr != nil {
				_ = c.Error(replayErr)
				return
			}
			writeSSEEvents(c, replay.Events)
			next = replay.NextSequence
			if len(replay.Events) == 0 {
				_, _ = fmt.Fprint(c.Writer, ": heartbeat\n\n")
				if flusher, ok := c.Writer.(nethttp.Flusher); ok {
					flusher.Flush()
				}
			}
		}
	}
}

func (h workbenchHandler) replayEvents(c *gin.Context) {
	after := int64(intQuery(c, "after_sequence", 0))
	if lastEventID := c.GetHeader("Last-Event-ID"); lastEventID != "" {
		if parsed, err := strconv.ParseInt(lastEventID, 10, 64); err == nil {
			after = parsed
		}
	}
	out, err := h.app.ReplayEvents(c.Request.Context(), auth(c), c.Param("run_id"), after, intQuery(c, "limit", 10), traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) appendUserInput(c *gin.Context) {
	var req workbench.AppendUserInputRequest
	if !bind(c, &req) {
		return
	}
	req.IdempotencyKey = idempotencyKey(c, req.IdempotencyKey)
	out, err := h.app.AppendUserInput(c.Request.Context(), auth(c), c.Param("run_id"), req, traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) acceptInterrupt(c *gin.Context) {
	var req workbench.ConfirmInterruptRequest
	if !bind(c, &req) {
		return
	}
	req.RunID = c.Param("run_id")
	req.InterruptID = c.Param("interrupt_id")
	req.IdempotencyKey = idempotencyKey(c, req.IdempotencyKey)
	out, err := h.app.AcceptInterrupt(c.Request.Context(), auth(c), c.Param("run_id"), req, traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) rejectInterrupt(c *gin.Context) {
	var req workbench.RejectInterruptRequest
	if !bind(c, &req) {
		return
	}
	req.RunID = c.Param("run_id")
	req.InterruptID = c.Param("interrupt_id")
	req.IdempotencyKey = idempotencyKey(c, req.IdempotencyKey)
	out, err := h.app.RejectInterrupt(c.Request.Context(), auth(c), c.Param("run_id"), req, traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) cancelRun(c *gin.Context) {
	var req struct {
		CancelReason   string `json:"cancel_reason"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	_ = c.ShouldBindJSON(&req)
	out, err := h.app.CancelRun(c.Request.Context(), auth(c), c.Param("run_id"), req.CancelReason, idempotencyKey(c, req.IdempotencyKey), traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) getRunSnapshot(c *gin.Context) {
	out, err := h.app.BuildRunSnapshot(c.Request.Context(), auth(c), c.Param("run_id"), traceID(c))
	respond(c, out, err)
}

func (h workbenchHandler) authRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		if h.app == nil {
			_ = c.Error(apperror.New(apperror.CodeNotImplemented, "agent workbench application is not configured"))
			c.Abort()
			return
		}
		authorization := c.GetHeader("Authorization")
		if authorization == "" {
			_ = c.Error(apperror.New(apperror.CodeUnauthenticated, "Authorization is required"))
			c.Abort()
			return
		}
		auth, err := h.app.ResolveAuthContextFromToken(c.Request.Context(), authorization, c.GetHeader("X-Space-Id"), traceID(c))
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		if auth.ActorUserID == "" || auth.SpaceID == "" {
			_ = c.Error(apperror.New(apperror.CodeUnauthenticated, "authorization token is invalid"))
			c.Abort()
			return
		}
		c.Set("auth", auth)
		c.Next()
	}
}

func writeSSEEvents(c *gin.Context, events []workbench.EventDTO) {
	for _, event := range events {
		data, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			_ = c.Error(apperror.New(apperror.CodeInternal, "failed to marshal event"))
			return
		}
		_, _ = fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, data)
		if flusher, ok := c.Writer.(nethttp.Flusher); ok {
			flusher.Flush()
		}
	}
}

func bind(c *gin.Context, out any) bool {
	if err := c.ShouldBindJSON(out); err != nil {
		_ = c.Error(apperror.New(apperror.CodeInvalidArgument, "invalid json request"))
		return false
	}
	return true
}

func respond(c *gin.Context, data any, err error) {
	if err != nil {
		_ = c.Error(err)
		return
	}
	c.JSON(nethttp.StatusOK, data)
}

func auth(c *gin.Context) workbench.AuthContextDTO {
	value, _ := c.Get("auth")
	auth, _ := value.(workbench.AuthContextDTO)
	return auth
}

func traceID(c *gin.Context) string {
	return observability.TraceID(c.Request.Context())
}

func idempotencyKey(c *gin.Context, bodyValue string) string {
	if header := c.GetHeader("Idempotency-Key"); header != "" {
		return header
	}
	return strings.TrimSpace(bodyValue)
}

func intQuery(c *gin.Context, key string, fallback int) int {
	value := c.Query(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
