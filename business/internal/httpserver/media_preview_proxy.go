package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/gin-gonic/gin"
)

const (
	generateMediaPreviewRequestSchema  = "generate_media.preview.enqueue-request.v1"
	generateMediaIntentSchema          = "generate_media.intent.v3preview1"
	assembleOutputPreviewRequestSchema = "assemble_output.preview.enqueue-request.v1"
	assembleOutputIntentSchema         = "assemble_output.intent.v3preview1"
	mediaPreviewEnqueueSchema          = "media_preview.enqueue.v1"
	maximumMediaEnqueueResponseBytes   = 16 << 10
)

var mediaPreviewTargetLocalKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)

type mediaPreviewVersionedRefV1 struct {
	ID            string `json:"id"`
	Version       int64  `json:"version"`
	ContentDigest string `json:"content_digest"`
}

type generateMediaPreviewRequestV1 struct {
	SchemaVersion    string                        `json:"schema_version"`
	PromptPreviewRef *mediaPreviewVersionedRefV1   `json:"prompt_preview_ref"`
	ToolIntent       *generateMediaPreviewIntentV1 `json:"tool_intent"`
}

type generateMediaPreviewIntentV1 struct {
	SchemaVersion               string `json:"schema_version"`
	PromptPreviewID             string `json:"prompt_preview_id"`
	ExpectedPromptVersion       int64  `json:"expected_prompt_version"`
	ExpectedPromptContentDigest string `json:"expected_prompt_content_digest"`
	TargetLocalKey              string `json:"target_local_key"`
	OutputProfile               string `json:"output_profile"`
}

type assembleOutputPreviewRequestV1 struct {
	SchemaVersion  string                         `json:"schema_version"`
	SourceAssetRef *mediaPreviewVersionedRefV1    `json:"source_asset_ref"`
	ToolIntent     *assembleOutputPreviewIntentV1 `json:"tool_intent"`
}

type assembleOutputPreviewIntentV1 struct {
	SchemaVersion               string `json:"schema_version"`
	SourceAssetID               string `json:"source_asset_id"`
	ExpectedSourceVersion       int64  `json:"expected_source_version"`
	ExpectedSourceContentDigest string `json:"expected_source_content_digest"`
	OutputProfile               string `json:"output_profile"`
}

type mediaPreviewEnqueueResponseV1 struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	SessionID     string `json:"session_id"`
	InputID       string `json:"input_id"`
	TurnID        string `json:"turn_id"`
	RunID         string `json:"run_id"`
	ToolCallID    string `json:"tool_call_id"`
	ToolKey       string `json:"tool_key"`
	Status        string `json:"status"`
	Replayed      *bool  `json:"replayed"`
}

func (handler *AgentProxyHandler) generateMediaPreview(c *gin.Context) {
	handler.proxyMediaPreview(c, "generate_media", agentidentity.ScopeGenerateMediaPreviewWrite,
		func(c *gin.Context, requestID string) ([]byte, bool) {
			var request generateMediaPreviewRequestV1
			if !handler.decodeMediaPreviewBody(c, requestID, &request) || !validGenerateMediaPreviewRequest(request) {
				if !c.Writer.Written() {
					handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Generate Media 请求格式无效", requestID, false)
				}
				return nil, false
			}
			canonical, err := json.Marshal(request)
			if err != nil {
				handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "媒体预览依赖暂时不可用", requestID, true)
				return nil, false
			}
			return canonical, true
		})
}

func (handler *AgentProxyHandler) assembleOutputPreview(c *gin.Context) {
	handler.proxyMediaPreview(c, "assemble_output", agentidentity.ScopeAssembleOutputPreviewWrite,
		func(c *gin.Context, requestID string) ([]byte, bool) {
			var request assembleOutputPreviewRequestV1
			if !handler.decodeMediaPreviewBody(c, requestID, &request) || !validAssembleOutputPreviewRequest(request) {
				if !c.Writer.Written() {
					handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Assemble Output 请求格式无效", requestID, false)
				}
				return nil, false
			}
			canonical, err := json.Marshal(request)
			if err != nil {
				handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "媒体预览依赖暂时不可用", requestID, true)
				return nil, false
			}
			return canonical, true
		})
}

func (handler *AgentProxyHandler) proxyMediaPreview(
	c *gin.Context,
	toolKey string,
	scope string,
	decode func(*gin.Context, string) ([]byte, bool),
) {
	requestID, ok := handler.newAgentRequestID(c)
	if !ok {
		return
	}
	if !handler.mediaRuntimeEnabled {
		handler.writeAgentError(c, http.StatusNotFound, "PREVIEW_DISABLED", "媒体开发预览未启用", requestID, false)
		return
	}
	sessionID := c.Param("session_id")
	if !canonicalUUIDv7(sessionID) || c.Request.URL.RawQuery != "" {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Session 标识无效", requestID, false)
		return
	}
	idempotencyValues := c.Request.Header.Values("Idempotency-Key")
	if len(idempotencyValues) != 1 || !canonicalUUIDv7(idempotencyValues[0]) {
		handler.writeAgentError(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "幂等键无效", requestID, false)
		return
	}
	canonicalBody, ok := decode(c, requestID)
	if !ok {
		return
	}
	target := "/internal/v1/workspaces/sessions/" + sessionID + "/" + toolKeyMediaTargetSuffix(toolKey)
	request, ok := handler.prepareBoundUpstreamRequest(
		c, requestID, sessionID, http.MethodPost, target, scope,
		"application/json", "application/json", bytes.NewReader(canonicalBody),
	)
	if !ok {
		return
	}
	request.Header.Set("Idempotency-Key", idempotencyValues[0])
	requestContext, cancel := contextWithProxyTimeout(request, handler.requestTimeout)
	defer cancel()
	response, err := handler.client.Do(request.WithContext(requestContext))
	if err != nil || response == nil || response.Body == nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "媒体预览依赖暂时不可用", requestID, true)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		handler.proxyUpstreamError(c, response, requestID)
		return
	}
	contentTypes := response.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "媒体预览依赖暂时不可用", requestID, true)
		return
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "媒体预览依赖暂时不可用", requestID, true)
		return
	}
	body, err := readBoundedBody(response.Body, maximumMediaEnqueueResponseBytes)
	if err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "媒体预览依赖暂时不可用", requestID, true)
		return
	}
	enqueue, err := decodeMediaPreviewEnqueue(body)
	if err != nil || enqueue.RequestID != requestID || enqueue.SessionID != sessionID || enqueue.ToolKey != toolKey {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "媒体预览依赖暂时不可用", requestID, true)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusAccepted, enqueue)
}

func (handler *AgentProxyHandler) decodeMediaPreviewBody(c *gin.Context, requestID string, output any) bool {
	contentTypes := c.Request.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "媒体预览请求格式无效", requestID, false)
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "媒体预览请求格式无效", requestID, false)
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, handler.previewBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	trimmed := bytes.TrimSpace(raw)
	if err != nil || len(trimmed) == 0 || trimmed[0] != '{' || !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "媒体预览请求格式无效", requestID, false)
		return false
	}
	duplicate, err := hasDuplicateJSONKey(raw)
	if err != nil || duplicate {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "媒体预览请求格式无效", requestID, false)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil || ensureJSONEOF(decoder) != nil {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "媒体预览请求格式无效", requestID, false)
		return false
	}
	return true
}

func validGenerateMediaPreviewRequest(request generateMediaPreviewRequestV1) bool {
	if request.SchemaVersion != generateMediaPreviewRequestSchema || request.PromptPreviewRef == nil || request.ToolIntent == nil ||
		!validMediaVersionedRef(*request.PromptPreviewRef) || request.ToolIntent.SchemaVersion != generateMediaIntentSchema ||
		request.ToolIntent.PromptPreviewID != request.PromptPreviewRef.ID ||
		request.ToolIntent.ExpectedPromptVersion != request.PromptPreviewRef.Version ||
		request.ToolIntent.ExpectedPromptContentDigest != request.PromptPreviewRef.ContentDigest ||
		len(request.ToolIntent.TargetLocalKey) > 128 || !mediaPreviewTargetLocalKeyPattern.MatchString(request.ToolIntent.TargetLocalKey) ||
		request.ToolIntent.OutputProfile != "png_640x360.v1" {
		return false
	}
	return true
}

func validAssembleOutputPreviewRequest(request assembleOutputPreviewRequestV1) bool {
	if request.SchemaVersion != assembleOutputPreviewRequestSchema || request.SourceAssetRef == nil || request.ToolIntent == nil ||
		!validMediaVersionedRef(*request.SourceAssetRef) || request.ToolIntent.SchemaVersion != assembleOutputIntentSchema ||
		request.ToolIntent.SourceAssetID != request.SourceAssetRef.ID ||
		request.ToolIntent.ExpectedSourceVersion != request.SourceAssetRef.Version ||
		request.ToolIntent.ExpectedSourceContentDigest != request.SourceAssetRef.ContentDigest ||
		request.ToolIntent.OutputProfile != "mp4_h264_640x360_2s.v1" {
		return false
	}
	return true
}

func validMediaVersionedRef(reference mediaPreviewVersionedRefV1) bool {
	if !canonicalUUIDv7(reference.ID) || reference.Version != 1 || len(reference.ContentDigest) != 64 {
		return false
	}
	for _, character := range reference.ContentDigest {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func decodeMediaPreviewEnqueue(raw []byte) (mediaPreviewEnqueueResponseV1, error) {
	var response mediaPreviewEnqueueResponseV1
	if !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		return response, errors.New("invalid media enqueue encoding")
	}
	duplicate, err := hasDuplicateJSONKey(raw)
	if err != nil || duplicate {
		return response, errors.New("invalid media enqueue object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil || ensureJSONEOF(decoder) != nil ||
		response.SchemaVersion != mediaPreviewEnqueueSchema || response.Status != "pending" ||
		!canonicalUUIDv7(response.RequestID) || !canonicalUUIDv7(response.SessionID) ||
		!canonicalUUIDv7(response.InputID) || !canonicalUUIDv7(response.TurnID) ||
		!canonicalUUIDv7(response.RunID) || !canonicalUUIDv7(response.ToolCallID) ||
		(response.ToolKey != "generate_media" && response.ToolKey != "assemble_output") || response.Replayed == nil {
		return mediaPreviewEnqueueResponseV1{}, errors.New("invalid media enqueue response")
	}
	return response, nil
}

func toolKeyMediaTargetSuffix(toolKey string) string {
	if toolKey == "generate_media" {
		return "generate-media-previews"
	}
	return "assemble-output-previews"
}
