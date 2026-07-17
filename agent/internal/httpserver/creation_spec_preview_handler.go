package httpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	previewruntime "github.com/FigoGoo/Dora-Agent/agent/internal/runtime"
	"github.com/gin-gonic/gin"
)

const (
	previewMaxBodyBytes          = 64 * 1024
	errorCodePreviewDisabled     = "PREVIEW_DISABLED"
	errorCodeIdempotencyConflict = "IDEMPOTENCY_CONFLICT"
	errorCodeSessionLaneBlocked  = "SESSION_LANE_BLOCKED"
)

// CreationSpecPreviewService 是 POST Handler 消费的 persist-only 入队端口。
type CreationSpecPreviewService interface {
	Enqueue(ctx context.Context, command previewruntime.EnqueueCommand) (previewruntime.EnqueueResult, error)
}

// CreationSpecPreviewHandler 实现唯一内部 canonical POST；它不等待模型、Graph、Runner 或 Business RPC。
type CreationSpecPreviewHandler struct {
	verifier IdentityVerifier
	service  CreationSpecPreviewService
	ids      IDGenerator
}

// NewCreationSpecPreviewHandler 创建启用态写 Handler；关闭态必须根本不构造或注册它。
func NewCreationSpecPreviewHandler(
	verifier IdentityVerifier,
	service CreationSpecPreviewService,
	ids IDGenerator,
) (*CreationSpecPreviewHandler, error) {
	if verifier == nil || service == nil || ids == nil {
		return nil, fmt.Errorf("create CreationSpec Preview handler: verifier, service and IDs are required")
	}
	return &CreationSpecPreviewHandler{verifier: verifier, service: service, ids: ids}, nil
}

// Register 只注册 Business BFF 可调用的内部 canonical 路径。
func (h *CreationSpecPreviewHandler) Register(router gin.IRoutes) {
	router.POST("/internal/v1/workspaces/sessions/:session_id/creation-spec-previews", h.post)
}

// CreationSpecPreviewEnqueueResponse 是 202 exact-set 回执。
type CreationSpecPreviewEnqueueResponse struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	SessionID     string `json:"session_id"`
	InputID       string `json:"input_id"`
	Status        string `json:"status"`
}

func (h *CreationSpecPreviewHandler) post(c *gin.Context) {
	sessionID, ok := canonicalUUIDv7(c.Param("session_id"))
	target := "/internal/v1/workspaces/sessions/" + sessionID + "/creation-spec-previews"
	if !ok || c.Request.URL.RawQuery != "" || c.Request.URL.EscapedPath() != target ||
		!singleExactHeader(c.Request.Header, "Content-Type", "application/json") {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "预览请求无效", h.newRequestID(), false)
		return
	}
	idempotencyKey, ok := singleHeader(c.Request.Header, "Idempotency-Key")
	if !ok {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "Idempotency-Key 无效", h.newRequestID(), false)
		return
	}
	if _, canonical := canonicalUUIDv7(idempotencyKey); !canonical {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "Idempotency-Key 无效", h.newRequestID(), false)
		return
	}
	claims, err := h.verifier.Verify(c.Request.Context(), httpidentity.Request{
		Headers: c.Request.Header, Method: http.MethodPost, CanonicalTarget: target,
		Scope: httpidentity.ScopeCreationSpecPreviewWrite, AgentSessionID: sessionID,
	})
	if err != nil {
		if errors.Is(err, httpidentity.ErrUnavailable) {
			h.writeError(c, http.StatusServiceUnavailable, errorCodeIdentityAssertionUnavailable,
				"内部身份校验暂时不可用", h.newRequestID(), true)
			return
		}
		h.writeError(c, http.StatusUnauthorized, errorCodeInternalIdentityInvalid,
			"内部身份断言无效", h.newRequestID(), false)
		return
	}
	requestCtx, cancel := context.WithDeadline(c.Request.Context(), claims.ExpiresAt)
	defer cancel()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, previewMaxBodyBytes+1)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(body) == 0 || len(body) > previewMaxBodyBytes {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "预览请求正文无效", claims.RequestID, false)
		return
	}
	intent, err := plancreationspec.DecodeIntent(body)
	if err != nil {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "预览请求正文无效", claims.RequestID, false)
		return
	}
	result, err := h.service.Enqueue(requestCtx, previewruntime.EnqueueCommand{
		RequestID: claims.RequestID, IdempotencyKey: idempotencyKey,
		UserID: claims.PrincipalUserID, ProjectID: claims.ProjectID, SessionID: sessionID, Intent: intent,
	})
	if err != nil {
		h.writeServiceError(c, err, claims.RequestID)
		return
	}
	if result.SessionID != sessionID || result.InputID == "" || result.Status != "pending" {
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable,
			"Agent 持久化暂时不可用", claims.RequestID, true)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.PureJSON(http.StatusAccepted, CreationSpecPreviewEnqueueResponse{
		SchemaVersion: plancreationspec.EnqueueSchemaVersion,
		RequestID:     claims.RequestID, SessionID: sessionID, InputID: result.InputID, Status: "pending",
	})
}

func (h *CreationSpecPreviewHandler) writeServiceError(c *gin.Context, err error, requestID string) {
	switch {
	case errors.Is(err, previewruntime.ErrInvalidInput):
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "预览请求正文无效", requestID, false)
	case errors.Is(err, previewruntime.ErrNotFound):
		h.writeError(c, http.StatusNotFound, errorCodeSessionNotFound, "Session 不存在或不可访问", requestID, false)
	case errors.Is(err, previewruntime.ErrIdempotencyConflict):
		h.writeError(c, http.StatusConflict, errorCodeIdempotencyConflict, "Idempotency-Key 已绑定其他请求", requestID, false)
	case errors.Is(err, previewruntime.ErrSessionLaneBlocked):
		h.writeError(c, http.StatusConflict, errorCodeSessionLaneBlocked, "Session 存在尚未完成的先行输入", requestID, false)
	default:
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable,
			"Agent 持久化暂时不可用", requestID, true)
	}
}

func (h *CreationSpecPreviewHandler) writeError(
	c *gin.Context,
	status int,
	code string,
	message string,
	requestID string,
	retryable bool,
) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: requestID, Retryable: retryable, Details: ErrorDetails{},
	}})
}

func (h *CreationSpecPreviewHandler) newRequestID() string {
	value, err := h.ids.New()
	if _, ok := canonicalUUIDv7(value); err != nil || !ok {
		return "019f0000-0000-7000-8000-000000000000"
	}
	return value
}

func singleHeader(headers http.Header, name string) (string, bool) {
	values := headers.Values(name)
	if len(values) != 1 || values[0] == "" {
		return "", false
	}
	return values[0], true
}

func singleExactHeader(headers http.Header, name, expected string) bool {
	value, ok := singleHeader(headers, name)
	return ok && value == expected
}
