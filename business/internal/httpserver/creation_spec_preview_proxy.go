package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/gin-gonic/gin"
	"golang.org/x/text/unicode/norm"
)

const (
	// creationSpecPreviewIntentSchema 是浏览器到 BFF/Agent 的冻结 Preview Intent 版本。
	creationSpecPreviewIntentSchema = "plan_creation_spec.preview.intent.v1"
	// creationSpecPreviewEnqueueSchema 是 202 持久化入队回执版本；pending 不代表 Draft 已生成。
	creationSpecPreviewEnqueueSchema   = "plan_creation_spec.preview.enqueue.v1"
	maximumPreviewEnqueueResponseBytes = 8 << 10
)

// creationSpecPreviewIntentRequest 是 BFF 唯一接受并重新规范编码的 Preview Intent HTTP DTO。
type creationSpecPreviewIntentRequest struct {
	// SchemaVersion 固定为 plan_creation_spec.preview.intent.v1。
	SchemaVersion string `json:"schema_version"`
	// Goal 是用户本次创作目标，必须是 1 至 2000 个 NFC 字符。
	Goal string `json:"goal"`
	// DeliverableType 是 video、image_set、audio 或 mixed。
	DeliverableType string `json:"deliverable_type"`
	// Audience 区分字段省略与显式空字符串；显式 null 被拒绝。
	Audience *string `json:"audience,omitempty"`
	// Locale 仅允许 zh-CN 或 en-US。
	Locale string `json:"locale"`
	// Constraints 必须显式存在且非 null，可包含零至八条硬约束。
	Constraints *[]string `json:"constraints"`
}

// creationSpecPreviewEnqueueResponse 是 Agent 可靠持久化 Input 后的最小 202 DTO。
type creationSpecPreviewEnqueueResponse struct {
	// SchemaVersion 固定为 plan_creation_spec.preview.enqueue.v1。
	SchemaVersion string `json:"schema_version"`
	// RequestID 与 Business 身份断言 Request ID 完全一致。
	RequestID string `json:"request_id"`
	// SessionID 是路由绑定的 Agent Session UUIDv7。
	SessionID string `json:"session_id"`
	// InputID 是 Agent 已持久化的 Session Input UUIDv7。
	InputID string `json:"input_id"`
	// Status 固定为 pending，不冒充 Tool 或 Draft 完成。
	Status string `json:"status"`
}

// creationSpecPreview 严格校验 Intent、Idempotency-Key 与 Owner，再以 POST 专用 HMAC Scope 调用 Agent。
func (handler *AgentProxyHandler) creationSpecPreview(c *gin.Context) {
	requestID, ok := handler.newAgentRequestID(c)
	if !ok {
		return
	}
	if !handler.previewEnabled {
		handler.writeAgentError(c, http.StatusNotFound, "PREVIEW_DISABLED", "CreationSpec 开发预览未启用", requestID, false)
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
	intent, canonicalBody, ok := handler.decodeCreationSpecPreviewIntent(c, requestID)
	if !ok {
		return
	}
	_ = intent // Intent 已通过本地边界校验；Agent 会独立重算 Canonical 与摘要，BFF 不推断执行结果。

	target := "/internal/v1/workspaces/sessions/" + sessionID + "/creation-spec-previews"
	request, ok := handler.prepareBoundUpstreamRequest(
		c, requestID, sessionID, http.MethodPost, target, agentidentity.ScopeCreationSpecPreviewWrite,
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
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "CreationSpec 预览依赖暂时不可用", requestID, true)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		handler.proxyUpstreamError(c, response, requestID)
		return
	}
	contentTypes := response.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "CreationSpec 预览依赖暂时不可用", requestID, true)
		return
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "CreationSpec 预览依赖暂时不可用", requestID, true)
		return
	}
	body, err := readBoundedBody(response.Body, maximumPreviewEnqueueResponseBytes)
	if err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "CreationSpec 预览依赖暂时不可用", requestID, true)
		return
	}
	enqueue, err := decodeCreationSpecPreviewEnqueue(body)
	if err != nil || enqueue.RequestID != requestID || enqueue.SessionID != sessionID {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "CreationSpec 预览依赖暂时不可用", requestID, true)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusAccepted, enqueue)
}

// decodeCreationSpecPreviewIntent 执行有界读取、严格 JSON 字段集、显式 null、NFC、枚举与数组唯一性校验。
func (handler *AgentProxyHandler) decodeCreationSpecPreviewIntent(c *gin.Context, requestID string) (creationSpecPreviewIntentRequest, []byte, bool) {
	contentTypes := c.Request.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "CreationSpec 预览请求格式无效", requestID, false)
		return creationSpecPreviewIntentRequest{}, nil, false
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "CreationSpec 预览请求格式无效", requestID, false)
		return creationSpecPreviewIntentRequest{}, nil, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, handler.previewBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	trimmed := bytes.TrimSpace(raw)
	if err != nil || len(trimmed) == 0 || trimmed[0] != '{' || !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "CreationSpec 预览请求格式无效", requestID, false)
		return creationSpecPreviewIntentRequest{}, nil, false
	}
	duplicate, err := hasDuplicateTopLevelJSONKey(raw)
	if err != nil || duplicate {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "CreationSpec 预览请求格式无效", requestID, false)
		return creationSpecPreviewIntentRequest{}, nil, false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "CreationSpec 预览请求格式无效", requestID, false)
		return creationSpecPreviewIntentRequest{}, nil, false
	}
	for _, required := range []string{"schema_version", "goal", "deliverable_type", "locale", "constraints"} {
		value, exists := fields[required]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "CreationSpec 预览请求格式无效", requestID, false)
			return creationSpecPreviewIntentRequest{}, nil, false
		}
	}
	if audience, exists := fields["audience"]; exists && bytes.Equal(bytes.TrimSpace(audience), []byte("null")) {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "CreationSpec 预览请求格式无效", requestID, false)
		return creationSpecPreviewIntentRequest{}, nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var intent creationSpecPreviewIntentRequest
	if err := decoder.Decode(&intent); err != nil || ensureJSONEOF(decoder) != nil || !validCreationSpecPreviewIntent(intent) {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "CreationSpec 预览请求格式无效", requestID, false)
		return creationSpecPreviewIntentRequest{}, nil, false
	}
	canonical, err := json.Marshal(intent)
	if err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "CreationSpec 预览依赖暂时不可用", requestID, true)
		return creationSpecPreviewIntentRequest{}, nil, false
	}
	return intent, canonical, true
}

// validCreationSpecPreviewIntent 校验冻结 Intent 语义，不接受边界空白、控制字符、非 NFC 或重复约束。
func validCreationSpecPreviewIntent(intent creationSpecPreviewIntentRequest) bool {
	if intent.SchemaVersion != creationSpecPreviewIntentSchema || !validPreviewText(intent.Goal, 1, 2000, false) {
		return false
	}
	if intent.DeliverableType != "video" && intent.DeliverableType != "image_set" &&
		intent.DeliverableType != "audio" && intent.DeliverableType != "mixed" {
		return false
	}
	if intent.Locale != "zh-CN" && intent.Locale != "en-US" {
		return false
	}
	if intent.Audience != nil && !validPreviewText(*intent.Audience, 0, 500, true) {
		return false
	}
	if intent.Constraints == nil || len(*intent.Constraints) > 8 {
		return false
	}
	seen := make(map[string]struct{}, len(*intent.Constraints))
	for _, constraint := range *intent.Constraints {
		if !validPreviewText(constraint, 1, 200, false) {
			return false
		}
		if _, exists := seen[constraint]; exists {
			return false
		}
		seen[constraint] = struct{}{}
	}
	return true
}

// validPreviewText 校验 UTF-8/NFC、Rune 长度、边界空白与不可见控制字符。
func validPreviewText(value string, minimum int, maximum int, allowEmpty bool) bool {
	if !utf8.ValidString(value) || !norm.NFC.IsNormalString(value) || strings.TrimSpace(value) != value {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length == 0 && allowEmpty {
		return true
	}
	if length < minimum || length > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			return false
		}
	}
	return true
}

// decodeCreationSpecPreviewEnqueue 严格验证 Agent 202 DTO，拒绝未知字段、重复键、尾随值与伪完成状态。
func decodeCreationSpecPreviewEnqueue(raw []byte) (creationSpecPreviewEnqueueResponse, error) {
	if !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		return creationSpecPreviewEnqueueResponse{}, errors.New("invalid preview enqueue encoding")
	}
	duplicate, err := hasDuplicateTopLevelJSONKey(raw)
	if err != nil || duplicate {
		return creationSpecPreviewEnqueueResponse{}, errors.New("invalid preview enqueue object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var response creationSpecPreviewEnqueueResponse
	if err := decoder.Decode(&response); err != nil || ensureJSONEOF(decoder) != nil ||
		response.SchemaVersion != creationSpecPreviewEnqueueSchema || response.Status != "pending" ||
		!canonicalUUIDv7(response.RequestID) || !canonicalUUIDv7(response.SessionID) || !canonicalUUIDv7(response.InputID) {
		return creationSpecPreviewEnqueueResponse{}, errors.New("invalid preview enqueue response")
	}
	return response, nil
}

// contextWithProxyTimeout 为非流式内部请求建立总时长预算；单独封装便于测试确认 POST 不会无界等待。
func contextWithProxyTimeout(request *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(request.Context(), timeout)
}
