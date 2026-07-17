package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/FigoGoo/Dora-Agent/agent/internal/analyzematerialsruntime"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/gin-gonic/gin"
)

const analyzeMaterialsPreviewMaxBodyBytes = 64 * 1024

// AnalyzeMaterialsPreviewService 是素材分析内部 POST Handler 消费的 persist-only 入队端口。
type AnalyzeMaterialsPreviewService interface {
	// Enqueue 只持久化 typed Input 与不可变执行身份，不在 HTTP 请求内等待模型或 Tool。
	Enqueue(context.Context, analyzematerialsruntime.EnqueueRequest) (analyzematerialsruntime.EnqueueResponse, error)
}

// AnalyzeMaterialsPreviewHandler 实现唯一内部 canonical 素材分析 POST。
type AnalyzeMaterialsPreviewHandler struct {
	verifier         IdentityVerifier
	service          AnalyzeMaterialsPreviewService
	ids              IDGenerator
	intentKeyVersion string
}

// NewAnalyzeMaterialsPreviewHandler 创建本地 Profile 启用态 Handler；关闭态不得构造或注册它。
func NewAnalyzeMaterialsPreviewHandler(
	verifier IdentityVerifier,
	service AnalyzeMaterialsPreviewService,
	ids IDGenerator,
	intentKeyVersion string,
) (*AnalyzeMaterialsPreviewHandler, error) {
	if verifier == nil || service == nil || ids == nil || intentKeyVersion == "" || len(intentKeyVersion) > 64 {
		return nil, fmt.Errorf("create Analyze Materials Preview handler: invalid dependency or key version")
	}
	return &AnalyzeMaterialsPreviewHandler{
		verifier: verifier, service: service, ids: ids, intentKeyVersion: intentKeyVersion,
	}, nil
}

// Register 只注册 Business BFF 可调用的内部 canonical 素材分析路径。
func (h *AnalyzeMaterialsPreviewHandler) Register(router gin.IRoutes) {
	router.POST("/internal/v1/workspaces/sessions/:session_id/analyze-materials-previews", h.post)
}

// AnalyzeMaterialsPreviewEnqueueResponse 是 Agent 对 Business BFF 返回的 exact 202 DTO。
type AnalyzeMaterialsPreviewEnqueueResponse struct {
	// SchemaVersion 固定为 analyze_materials.preview.enqueue.v1。
	SchemaVersion string `json:"schema_version"`
	// RequestID 是本次内部身份断言绑定的 UUIDv7。
	RequestID string `json:"request_id"`
	// SessionID 是 canonical 路由绑定的 Session UUIDv7。
	SessionID string `json:"session_id"`
	// InputID 是已可靠持久化的 typed Input UUIDv7。
	InputID string `json:"input_id"`
	// TurnID 是入队事务预分配的 Turn UUIDv7。
	TurnID string `json:"turn_id"`
	// RunID 是入队事务预分配的 Run UUIDv7。
	RunID string `json:"run_id"`
	// ToolCallID 是唯一 analyze_materials ToolCall 的 UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// Status 固定为 pending。
	Status string `json:"status"`
	// Replayed 表示同义幂等键返回原持久化身份。
	Replayed bool `json:"replayed"`
}

// post 校验路径、幂等键、内部身份和 strict Intent 后只执行一次可靠入队。
func (h *AnalyzeMaterialsPreviewHandler) post(c *gin.Context) {
	sessionID, ok := canonicalUUIDv7(c.Param("session_id"))
	target := "/internal/v1/workspaces/sessions/" + sessionID + "/analyze-materials-previews"
	if !ok || c.Request.URL.RawQuery != "" || c.Request.URL.EscapedPath() != target ||
		!singleExactHeader(c.Request.Header, "Content-Type", "application/json") {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "素材分析预览请求无效", h.newRequestID(), false)
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
		Scope: httpidentity.ScopeAnalyzeMaterialsPreviewWrite, AgentSessionID: sessionID,
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, analyzeMaterialsPreviewMaxBodyBytes+1)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(body) == 0 || len(body) > analyzeMaterialsPreviewMaxBodyBytes {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "素材分析预览请求正文无效", claims.RequestID, false)
		return
	}
	accessDigest, err := analyzeMaterialsAccessScopeDigest(claims)
	if err != nil {
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable,
			"Agent 持久化暂时不可用", claims.RequestID, true)
		return
	}
	result, err := h.service.Enqueue(requestCtx, analyzematerialsruntime.EnqueueRequest{
		RequestID: claims.RequestID, SessionID: sessionID, UserID: claims.PrincipalUserID,
		ProjectID: claims.ProjectID, IdempotencyKey: idempotencyKey, IntentJSON: body,
		AccessScopeRef: httpidentity.ScopeAnalyzeMaterialsPreviewWrite, AccessScopeDigest: accessDigest,
		IntentKeyVersion: h.intentKeyVersion,
	})
	if err != nil {
		h.writeServiceError(c, err, claims.RequestID)
		return
	}
	if result.SchemaVersion != analyzematerialsruntime.EnqueueResponseSchemaVersion ||
		result.Status != analyzematerialsruntime.EnqueuePendingStatus {
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable,
			"Agent 持久化暂时不可用", claims.RequestID, true)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.PureJSON(http.StatusAccepted, AnalyzeMaterialsPreviewEnqueueResponse{
		SchemaVersion: result.SchemaVersion, RequestID: claims.RequestID, SessionID: sessionID,
		InputID: result.InputID, TurnID: result.TurnID, RunID: result.RunID, ToolCallID: result.ToolCallID,
		Status: result.Status, Replayed: result.Replayed,
	})
}

// analyzeMaterialsAccessScopeDigest 冻结 Business 已认证的用户、Project、Session 与 Web Session 授权事实。
func analyzeMaterialsAccessScopeDigest(claims httpidentity.Claims) (string, error) {
	wire := struct {
		SchemaVersion     string `json:"schema_version"`
		Scope             string `json:"scope"`
		PrincipalUserID   string `json:"principal_user_id"`
		ProjectID         string `json:"project_id"`
		AgentSessionID    string `json:"agent_session_id"`
		WebSessionID      string `json:"web_session_id"`
		WebSessionVersion int64  `json:"web_session_version"`
	}{
		SchemaVersion: "analyze_materials.access_scope.v1", Scope: claims.Scope,
		PrincipalUserID: claims.PrincipalUserID, ProjectID: claims.ProjectID,
		AgentSessionID: claims.AgentSessionID, WebSessionID: claims.WebSessionID,
		WebSessionVersion: claims.WebSessionVersion,
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("digest analyze materials access scope: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// writeServiceError 把领域和持久化稳定错误映射为无内部细节的 HTTP 错误。
func (h *AnalyzeMaterialsPreviewHandler) writeServiceError(c *gin.Context, err error, requestID string) {
	switch {
	case errors.Is(err, analyzematerialsruntime.ErrInvalidInput):
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "素材分析预览请求正文无效", requestID, false)
	case errors.Is(err, analyzematerialsruntime.ErrNotFound):
		h.writeError(c, http.StatusNotFound, errorCodeSessionNotFound, "Session 不存在或不可访问", requestID, false)
	case errors.Is(err, analyzematerialsruntime.ErrIdempotencyConflict):
		h.writeError(c, http.StatusConflict, errorCodeIdempotencyConflict, "Idempotency-Key 已绑定其他请求", requestID, false)
	case errors.Is(err, analyzematerialsruntime.ErrSessionLaneBlocked):
		h.writeError(c, http.StatusConflict, errorCodeSessionLaneBlocked, "Session 存在尚未完成的先行输入", requestID, false)
	default:
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable,
			"Agent 持久化暂时不可用", requestID, true)
	}
}

// writeError 输出稳定、no-store 且无内部错误原文的公共错误 DTO。
func (h *AnalyzeMaterialsPreviewHandler) writeError(c *gin.Context, status int, code, message, requestID string, retryable bool) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: requestID, Retryable: retryable, Details: ErrorDetails{},
	}})
}

// newRequestID 为身份校验前错误生成规范 UUIDv7，随机源异常时只返回固定安全占位。
func (h *AnalyzeMaterialsPreviewHandler) newRequestID() string {
	value, err := h.ids.New()
	if normalized, ok := canonicalUUIDv7(value); err != nil || !ok {
		return "019f0000-0000-7000-8000-000000000000"
	} else {
		return normalized
	}
}
