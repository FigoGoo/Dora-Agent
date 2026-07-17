package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreviewruntime"
	"github.com/gin-gonic/gin"
)

const mediaPreviewMaxBodyBytes = 32 * 1024

var errMediaPreviewMalformedJSON = errors.New("media preview malformed JSON")

// MediaPreviewService 是两个内部媒体 POST Handler 共用的 persist-only typed ingress 端口。
type MediaPreviewService interface {
	Enqueue(context.Context, mediapreviewruntime.EnqueueRequest) (mediapreviewruntime.EnqueueResponse, error)
}

// GenerateMediaPreviewHandler 实现 generate_media canonical 内部 POST。
type GenerateMediaPreviewHandler struct{ base *mediaPreviewHandler }

// AssembleOutputPreviewHandler 实现 assemble_output canonical 内部 POST。
type AssembleOutputPreviewHandler struct{ base *mediaPreviewHandler }

type mediaPreviewHandler struct {
	verifier      IdentityVerifier
	service       MediaPreviewService
	ids           IDGenerator
	toolKey       string
	suffix        string
	scope         string
	requestSchema string
}

// NewGenerateMediaPreviewHandler 创建 generate_media Handler。
func NewGenerateMediaPreviewHandler(verifier IdentityVerifier, service MediaPreviewService, ids IDGenerator) (*GenerateMediaPreviewHandler, error) {
	base, err := newMediaPreviewHandler(verifier, service, ids, mediapreview.GenerateMediaToolKey,
		"/generate-media-previews", httpidentity.ScopeGenerateMediaPreviewWrite, "generate_media.preview.enqueue-request.v1")
	if err != nil {
		return nil, err
	}
	return &GenerateMediaPreviewHandler{base: base}, nil
}

// NewAssembleOutputPreviewHandler 创建 assemble_output Handler。
func NewAssembleOutputPreviewHandler(verifier IdentityVerifier, service MediaPreviewService, ids IDGenerator) (*AssembleOutputPreviewHandler, error) {
	base, err := newMediaPreviewHandler(verifier, service, ids, mediapreview.AssembleOutputToolKey,
		"/assemble-output-previews", httpidentity.ScopeAssembleOutputPreviewWrite, "assemble_output.preview.enqueue-request.v1")
	if err != nil {
		return nil, err
	}
	return &AssembleOutputPreviewHandler{base: base}, nil
}

func newMediaPreviewHandler(verifier IdentityVerifier, service MediaPreviewService, ids IDGenerator, toolKey, suffix, scope, requestSchema string) (*mediaPreviewHandler, error) {
	if verifier == nil || service == nil || ids == nil {
		return nil, fmt.Errorf("create media preview handler: invalid dependency")
	}
	return &mediaPreviewHandler{verifier: verifier, service: service, ids: ids, toolKey: toolKey,
		suffix: suffix, scope: scope, requestSchema: requestSchema}, nil
}

// Register 注册唯一 generate_media 内部 canonical 路径。
func (h *GenerateMediaPreviewHandler) Register(router gin.IRoutes) { h.base.register(router) }

// Register 注册唯一 assemble_output 内部 canonical 路径。
func (h *AssembleOutputPreviewHandler) Register(router gin.IRoutes) { h.base.register(router) }

func (h *mediaPreviewHandler) register(router gin.IRoutes) {
	router.POST("/internal/v1/workspaces/sessions/:session_id"+h.suffix, h.post)
}

// MediaPreviewEnqueueResponse 是 Agent 返回 Business BFF 的 exact 202 DTO。
type MediaPreviewEnqueueResponse struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	SessionID     string `json:"session_id"`
	InputID       string `json:"input_id"`
	TurnID        string `json:"turn_id"`
	RunID         string `json:"run_id"`
	ToolCallID    string `json:"tool_call_id"`
	ToolKey       string `json:"tool_key"`
	Status        string `json:"status"`
	Replayed      bool   `json:"replayed"`
}

type mediaGenerateEnqueueRequest struct {
	SchemaVersion    string               `json:"schema_version"`
	PromptPreviewRef mediaPreviewOuterRef `json:"prompt_preview_ref"`
	ToolIntent       json.RawMessage      `json:"tool_intent"`
}

type mediaAssembleEnqueueRequest struct {
	SchemaVersion  string               `json:"schema_version"`
	SourceAssetRef mediaPreviewOuterRef `json:"source_asset_ref"`
	ToolIntent     json.RawMessage      `json:"tool_intent"`
}

type mediaPreviewOuterRef struct {
	ID            string `json:"id"`
	Version       int64  `json:"version"`
	ContentDigest string `json:"content_digest"`
}

func (h *mediaPreviewHandler) post(c *gin.Context) {
	sessionID, ok := canonicalUUIDv7(c.Param("session_id"))
	target := "/internal/v1/workspaces/sessions/" + sessionID + h.suffix
	if !ok || c.Request.URL.RawQuery != "" || c.Request.URL.EscapedPath() != target ||
		!singleExactHeader(c.Request.Header, "Content-Type", "application/json") {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "媒体预览请求无效", h.newRequestID(), false)
		return
	}
	idempotencyKey, ok := singleHeader(c.Request.Header, "Idempotency-Key")
	if !ok {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "Idempotency-Key 无效", h.newRequestID(), false)
		return
	}
	if _, ok = canonicalUUIDv7(idempotencyKey); !ok {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "Idempotency-Key 无效", h.newRequestID(), false)
		return
	}
	claims, err := h.verifier.Verify(c.Request.Context(), httpidentity.Request{Headers: c.Request.Header,
		Method: http.MethodPost, CanonicalTarget: target, Scope: h.scope, AgentSessionID: sessionID})
	if err != nil {
		if errors.Is(err, httpidentity.ErrUnavailable) {
			h.writeError(c, http.StatusServiceUnavailable, errorCodeIdentityAssertionUnavailable, "内部身份校验暂时不可用", h.newRequestID(), true)
			return
		}
		h.writeError(c, http.StatusUnauthorized, errorCodeInternalIdentityInvalid, "内部身份断言无效", h.newRequestID(), false)
		return
	}
	if !validMediaPreviewClaims(claims, sessionID, h.scope) {
		h.writeError(c, http.StatusUnauthorized, errorCodeInternalIdentityInvalid, "内部身份断言无效", h.newRequestID(), false)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, mediaPreviewMaxBodyBytes+1)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(body) == 0 || len(body) > mediaPreviewMaxBodyBytes {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "媒体预览请求正文无效", claims.RequestID, false)
		return
	}
	intentJSON, err := decodeMediaPreviewRequest(h.toolKey, h.requestSchema, body)
	if err != nil {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "媒体预览请求正文无效", claims.RequestID, false)
		return
	}
	requestCtx, cancel := context.WithDeadline(c.Request.Context(), claims.ExpiresAt)
	defer cancel()
	result, err := h.service.Enqueue(requestCtx, mediapreviewruntime.EnqueueRequest{RequestID: claims.RequestID,
		SessionID: sessionID, UserID: claims.PrincipalUserID, ProjectID: claims.ProjectID,
		IdempotencyKey: idempotencyKey, ToolKey: h.toolKey, IntentJSON: intentJSON})
	if err != nil {
		h.writeServiceError(c, err, claims.RequestID)
		return
	}
	if !validMediaEnqueueResponse(result, h.toolKey) {
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable, "Agent 持久化暂时不可用", claims.RequestID, true)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.PureJSON(http.StatusAccepted, MediaPreviewEnqueueResponse{SchemaVersion: result.SchemaVersion,
		RequestID: claims.RequestID, SessionID: sessionID, InputID: result.InputID, TurnID: result.TurnID,
		RunID: result.RunID, ToolCallID: result.ToolCallID, ToolKey: result.ToolKey, Status: result.Status, Replayed: result.Replayed})
}

func decodeMediaPreviewRequest(toolKey, requestSchema string, body []byte) ([]byte, error) {
	if !utf8.Valid(body) || !validWritePromptsPreviewJSONSurrogateEscapes(body) || rejectWritePromptsPreviewDuplicateFields(body) != nil {
		return nil, errMediaPreviewMalformedJSON
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var outer mediaPreviewOuterRef
	var raw json.RawMessage
	if toolKey == mediapreview.GenerateMediaToolKey {
		var request mediaGenerateEnqueueRequest
		if err := decoder.Decode(&request); err != nil || request.SchemaVersion != requestSchema {
			return nil, errMediaPreviewMalformedJSON
		}
		outer, raw = request.PromptPreviewRef, request.ToolIntent
		intent, err := mediapreview.DecodeGenerateMediaIntent(raw)
		if err != nil || outer.ID != intent.PromptPreviewID || outer.Version != intent.ExpectedPromptVersion || outer.ContentDigest != intent.ExpectedPromptContentDigest {
			return nil, errMediaPreviewMalformedJSON
		}
		encoded, err := mediapreview.CanonicalJSON(intent)
		if err != nil {
			return nil, err
		}
		raw = encoded
	} else {
		var request mediaAssembleEnqueueRequest
		if err := decoder.Decode(&request); err != nil || request.SchemaVersion != requestSchema {
			return nil, errMediaPreviewMalformedJSON
		}
		outer, raw = request.SourceAssetRef, request.ToolIntent
		intent, err := mediapreview.DecodeAssembleOutputIntent(raw)
		if err != nil || outer.ID != intent.SourceAssetID || outer.Version != intent.ExpectedSourceVersion || outer.ContentDigest != intent.ExpectedSourceContentDigest {
			return nil, errMediaPreviewMalformedJSON
		}
		encoded, err := mediapreview.CanonicalJSON(intent)
		if err != nil {
			return nil, err
		}
		raw = encoded
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errMediaPreviewMalformedJSON
	}
	if !mediapreview.ValidUUIDv7(outer.ID) || outer.Version != 1 || !mediapreview.ValidDigest(outer.ContentDigest) {
		return nil, errMediaPreviewMalformedJSON
	}
	return raw, nil
}

func validMediaPreviewClaims(claims httpidentity.Claims, sessionID, scope string) bool {
	for _, value := range []string{claims.RequestID, claims.PrincipalUserID, claims.ProjectID, claims.AgentSessionID, claims.WebSessionID} {
		if _, ok := canonicalUUIDv7(value); !ok {
			return false
		}
	}
	return claims.AgentSessionID == sessionID && claims.Scope == scope && claims.WebSessionVersion > 0 && claims.ExpiresAt.After(claims.IssuedAt)
}

func validMediaEnqueueResponse(result mediapreviewruntime.EnqueueResponse, toolKey string) bool {
	if result.SchemaVersion != mediapreviewruntime.EnqueueSchemaVersion || result.Status != mediapreviewruntime.PendingStatus || result.ToolKey != toolKey {
		return false
	}
	seen := map[string]bool{}
	for _, value := range []string{result.InputID, result.TurnID, result.RunID, result.ToolCallID} {
		if _, ok := canonicalUUIDv7(value); !ok || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func (h *mediaPreviewHandler) writeServiceError(c *gin.Context, err error, requestID string) {
	switch {
	case errors.Is(err, mediapreviewruntime.ErrInvalidInput):
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "媒体预览请求正文无效", requestID, false)
	case errors.Is(err, mediapreviewruntime.ErrNotFound):
		h.writeError(c, http.StatusNotFound, errorCodeSessionNotFound, "Session 不存在或不可访问", requestID, false)
	case errors.Is(err, mediapreviewruntime.ErrIdempotencyConflict):
		h.writeError(c, http.StatusConflict, errorCodeIdempotencyConflict, "Idempotency-Key 已绑定其他请求", requestID, false)
	default:
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable, "Agent 持久化暂时不可用", requestID, true)
	}
}

func (h *mediaPreviewHandler) writeError(c *gin.Context, status int, code, message, requestID string, retryable bool) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{Code: code, Message: message, RequestID: requestID, Retryable: retryable, Details: ErrorDetails{}}})
}

func (h *mediaPreviewHandler) newRequestID() string {
	value, err := h.ids.New()
	if normalized, ok := canonicalUUIDv7(value); err == nil && ok {
		return normalized
	}
	return "019f0000-0000-7000-8000-000000000000"
}
