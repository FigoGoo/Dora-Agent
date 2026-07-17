package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/gin-gonic/gin"
)

const (
	// writePromptsPreviewEnqueueRequestSchema 是浏览器、Business BFF 与 Agent 共用的严格入队信封版本。
	writePromptsPreviewEnqueueRequestSchema = "write_prompts.preview.enqueue-request.v1"
	// writePromptsPreviewIntentSchema 是唯一允许模型消费的 Prompt 写作 Intent 版本。
	writePromptsPreviewIntentSchema = "write_prompts.preview.intent.v1"
	// writePromptsPreviewEnqueueSchema 是 Agent 持久化 typed Input 后的 202 回执版本。
	writePromptsPreviewEnqueueSchema = "write_prompts.preview.enqueue.v1"
	// maximumWritePromptsPreviewEnqueueResponseBytes 限制 Agent 202 回执，避免代理无界读取。
	maximumWritePromptsPreviewEnqueueResponseBytes = 8 << 10
)

var (
	// ErrWritePromptsPreviewStoryboardNotFound 表示当前 Workspace 没有可用的 Storyboard Preview Draft。
	ErrWritePromptsPreviewStoryboardNotFound = errors.New("write prompts preview storyboard not found")
	// ErrWritePromptsPreviewStoryboardConflict 表示浏览器看到的 Storyboard Card 已不是当前权威版本。
	ErrWritePromptsPreviewStoryboardConflict = errors.New("write prompts preview storyboard conflict")
	// ErrWritePromptsPreviewStoryboardUnavailable 表示当前 Workspace Storyboard 绑定事实暂时无法读取。
	ErrWritePromptsPreviewStoryboardUnavailable = errors.New("write prompts preview storyboard unavailable")
)

// WritePromptsPreviewStoryboardRef 是浏览器提交并由 Business 权威绑定器复核的 Storyboard Draft 精确引用。
type WritePromptsPreviewStoryboardRef struct {
	// ID 是当前 Workspace 展示的 Storyboard Preview Draft UUIDv7。
	ID string `json:"id"`
	// Version 本 Development Preview 固定为一。
	Version int64 `json:"version"`
	// ContentDigest 是 Business canonical Storyboard 内容的小写 SHA-256。
	ContentDigest string `json:"content_digest"`
}

// WritePromptsPreviewStoryboardBindingRequest 描述 Owner、Project、Session 与当前 Storyboard Card 的联合绑定。
type WritePromptsPreviewStoryboardBindingRequest struct {
	// RequestID 是本次 BFF 请求 UUIDv7。
	RequestID string
	// UserID 是 Business Web Session 认证后的用户 UUIDv7。
	UserID string
	// ProjectID 是 Business Session Binding 解析出的当前 Project UUIDv7。
	ProjectID string
	// AgentSessionID 是公开 canonical 路由绑定的 Agent Session UUIDv7。
	AgentSessionID string
	// PresentedRef 是浏览器从当前已验证 Storyboard Card 提交的精确引用。
	PresentedRef WritePromptsPreviewStoryboardRef
}

// WritePromptsPreviewStoryboardBinder 定义 BFF 转发前绑定当前 Workspace Storyboard Card 的最小权限边界。
type WritePromptsPreviewStoryboardBinder interface {
	// BindCurrent 返回当前 Workspace 的权威精确引用；跨 Owner 统一 NotFound，漂移返回 Conflict。
	BindCurrent(context.Context, WritePromptsPreviewStoryboardBindingRequest) (WritePromptsPreviewStoryboardRef, error)
}

// WritePromptsPreviewProxy 负责 Prompt Development Preview 独立 BFF POST，不拥有 Workspace 或 Draft 事实。
type WritePromptsPreviewProxy struct {
	base    *AgentProxyHandler
	binder  WritePromptsPreviewStoryboardBinder
	enabled bool
}

// NewWritePromptsPreviewProxy 创建窄 Prompt BFF；正式注册仍必须显式注入 Session+CSRF 中间件。
func NewWritePromptsPreviewProxy(base *AgentProxyHandler, binder WritePromptsPreviewStoryboardBinder, enabled bool) (*WritePromptsPreviewProxy, error) {
	if base == nil || binder == nil {
		return nil, fmt.Errorf("create Write Prompts Preview proxy: invalid dependency")
	}
	return &WritePromptsPreviewProxy{base: base, binder: binder, enabled: enabled}, nil
}

// Register 只注册公开 canonical POST，并强制要求完整 Session+CSRF 写中间件。
func (proxy *WritePromptsPreviewProxy) Register(router gin.IRoutes, requireSessionAndCSRF gin.HandlerFunc) error {
	if proxy == nil || router == nil || requireSessionAndCSRF == nil {
		return fmt.Errorf("register Write Prompts Preview proxy: invalid dependency")
	}
	router.POST("/api/v1/agent/sessions/:session_id/write-prompts-previews", requireSessionAndCSRF, proxy.post)
	return nil
}

// writePromptsPreviewRequest 是浏览器公开 POST 与 Agent 内部 POST 共用的 exact-set DTO。
type writePromptsPreviewRequest struct {
	// SchemaVersion 固定为 write_prompts.preview.enqueue-request.v1。
	SchemaVersion string `json:"schema_version"`
	// StoryboardPreviewRef 来自当前 Workspace Storyboard Card，转发前由 Business 再绑定。
	StoryboardPreviewRef WritePromptsPreviewStoryboardRef `json:"storyboard_preview_ref"`
	// ToolIntent 只包含模型可控的写作字段。
	ToolIntent writePromptsPreviewIntentRequest `json:"tool_intent"`
}

// writePromptsPreviewIntentRequest 是用户可控且允许传给唯一 write_prompts Tool 的严格 Intent。
type writePromptsPreviewIntentRequest struct {
	// SchemaVersion 固定为 write_prompts.preview.intent.v1。
	SchemaVersion string `json:"schema_version"`
	// WritingInstruction 是一至一千个 NFC Unicode scalar 的写作说明。
	WritingInstruction string `json:"writing_instruction"`
	// OutputLanguage 是可选的 zh-CN 或 en-US；省略时由冻结 Runtime Policy 决定。
	OutputLanguage string `json:"output_language,omitempty"`
}

// writePromptsPreviewEnqueueResponse 是 BFF 允许返回浏览器的最小 pending/replayed 回执。
type writePromptsPreviewEnqueueResponse struct {
	// SchemaVersion 固定为 write_prompts.preview.enqueue.v1。
	SchemaVersion string `json:"schema_version"`
	// RequestID 与本次 Business 内部身份断言完全一致。
	RequestID string `json:"request_id"`
	// SessionID 是公开与内部 canonical 路由共同绑定的 Session UUIDv7。
	SessionID string `json:"session_id"`
	// InputID 是 Agent 已可靠持久化的 typed Input UUIDv7。
	InputID string `json:"input_id"`
	// TurnID 是首次事务预分配且重放不变的 Turn UUIDv7。
	TurnID string `json:"turn_id"`
	// RunID 是首次事务预分配且重放不变的 Run UUIDv7。
	RunID string `json:"run_id"`
	// ToolCallID 是唯一 write_prompts ToolCall UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// Status 固定为 pending，不表示 Prompt Draft 已完成。
	Status string `json:"status"`
	// Replayed 表示同义 Idempotency-Key 返回首次稳定身份。
	Replayed bool `json:"replayed"`
}

// post 严格校验浏览器 DTO，绑定当前 Storyboard，签发专用断言并转发 canonical 请求。
func (proxy *WritePromptsPreviewProxy) post(c *gin.Context) {
	requestID, ok := proxy.base.newAgentRequestID(c)
	if !ok {
		return
	}
	if !proxy.enabled {
		proxy.base.writeAgentError(c, http.StatusNotFound, "PREVIEW_DISABLED", "Prompt 开发预览未启用", requestID, false)
		return
	}
	sessionID := c.Param("session_id")
	publicTarget := "/api/v1/agent/sessions/" + sessionID + "/write-prompts-previews"
	if !canonicalUUIDv7(sessionID) || c.Request.URL.RawQuery != "" || c.Request.URL.EscapedPath() != publicTarget {
		proxy.writeInvalid(c, requestID)
		return
	}
	idempotencyValues := c.Request.Header.Values("Idempotency-Key")
	if len(idempotencyValues) != 1 || !canonicalUUIDv7(idempotencyValues[0]) {
		proxy.base.writeAgentError(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "幂等键无效", requestID, false)
		return
	}
	browserRequest, ok := proxy.decodeRequest(c, requestID)
	if !ok {
		return
	}
	resolved, ok := auth.ResolvedSessionFromContext(c.Request.Context())
	if !ok || resolved.Principal.ID == "" || resolved.WebSessionID == "" || resolved.WebSessionVersion < 1 {
		proxy.base.writeAgentError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
		return
	}
	access, err := proxy.base.access.Resolve(c.Request.Context(), resolved.Principal.ID, sessionID)
	if err != nil {
		if errors.Is(err, project.ErrAgentSessionNotFound) {
			proxy.base.writeAgentError(c, http.StatusNotFound, "SESSION_NOT_FOUND", "Session 不存在或不可访问", requestID, false)
		} else {
			proxy.writeUnavailable(c, requestID)
		}
		return
	}
	if !canonicalUUIDv7(access.ProjectID) || access.AgentSessionID != sessionID {
		proxy.writeUnavailable(c, requestID)
		return
	}
	authoritativeRef, err := proxy.binder.BindCurrent(c.Request.Context(), WritePromptsPreviewStoryboardBindingRequest{
		RequestID: requestID, UserID: resolved.Principal.ID, ProjectID: access.ProjectID,
		AgentSessionID: sessionID, PresentedRef: browserRequest.StoryboardPreviewRef,
	})
	if err != nil {
		proxy.writeBindingError(c, requestID, err)
		return
	}
	if !validWritePromptsPreviewStoryboardRef(authoritativeRef) {
		proxy.writeUnavailable(c, requestID)
		return
	}
	if authoritativeRef != browserRequest.StoryboardPreviewRef {
		proxy.base.writeAgentError(c, http.StatusConflict, "STORYBOARD_PREVIEW_CONFLICT", "当前 Storyboard Preview 已变化，请刷新工作台", requestID, false)
		return
	}
	browserRequest.StoryboardPreviewRef = authoritativeRef
	canonicalBody, err := json.Marshal(browserRequest)
	if err != nil || int64(len(canonicalBody)) > proxy.base.previewBodyBytes {
		proxy.writeUnavailable(c, requestID)
		return
	}
	internalTarget := "/internal/v1/workspaces/sessions/" + sessionID + "/write-prompts-previews"
	assertion, err := proxy.base.signer.Sign(agentidentity.Identity{
		RequestID: requestID, CanonicalTarget: internalTarget, Method: http.MethodPost,
		PrincipalUserID: resolved.Principal.ID, WebSessionID: resolved.WebSessionID,
		WebSessionVersion: resolved.WebSessionVersion, ProjectID: access.ProjectID,
		AgentSessionID: access.AgentSessionID, Scope: agentidentity.ScopeWritePromptsPreviewWrite,
	})
	if err != nil {
		proxy.writeUnavailable(c, requestID)
		return
	}
	upstream, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, proxy.base.baseURL.String()+internalTarget, bytes.NewReader(canonicalBody))
	if err != nil {
		proxy.writeUnavailable(c, requestID)
		return
	}
	// 浏览器 Cookie、CSRF、Authorization 与伪造内部断言都不复制。
	upstream.Header.Set("Accept", "application/json")
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("Idempotency-Key", idempotencyValues[0])
	upstream.Header.Set(agentidentity.HeaderAssertion, assertion.EncodedCanonical)
	upstream.Header.Set(agentidentity.HeaderKeyVersion, assertion.KeyVersion)
	upstream.Header.Set(agentidentity.HeaderSignature, assertion.Signature)
	requestContext, cancel := contextWithProxyTimeout(upstream, proxy.base.requestTimeout)
	defer cancel()
	response, err := proxy.base.client.Do(upstream.WithContext(requestContext))
	if err != nil || response == nil || response.Body == nil {
		proxy.writeUnavailable(c, requestID)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		proxy.proxyUpstreamError(c, response, requestID)
		return
	}
	contentTypes := response.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		proxy.writeUnavailable(c, requestID)
		return
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		proxy.writeUnavailable(c, requestID)
		return
	}
	body, err := readBoundedBody(response.Body, maximumWritePromptsPreviewEnqueueResponseBytes)
	if err != nil {
		proxy.writeUnavailable(c, requestID)
		return
	}
	enqueue, err := decodeWritePromptsPreviewEnqueue(body)
	if err != nil || enqueue.RequestID != requestID || enqueue.SessionID != sessionID {
		proxy.writeUnavailable(c, requestID)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusAccepted, enqueue)
}

// decodeRequest 有界读取并拒绝重复、未知、null、尾随、非法 Unicode 与不规范字段。
func (proxy *WritePromptsPreviewProxy) decodeRequest(c *gin.Context, requestID string) (writePromptsPreviewRequest, bool) {
	contentTypes := c.Request.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		proxy.writeInvalid(c, requestID)
		return writePromptsPreviewRequest{}, false
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		proxy.writeInvalid(c, requestID)
		return writePromptsPreviewRequest{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, proxy.base.previewBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	trimmed := bytes.TrimSpace(raw)
	if err != nil || len(trimmed) == 0 || trimmed[0] != '{' || !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		proxy.writeInvalid(c, requestID)
		return writePromptsPreviewRequest{}, false
	}
	duplicate, err := hasDuplicateJSONKey(raw)
	if err != nil || duplicate || !writePromptsPreviewRequiredFieldsPresent(raw) {
		proxy.writeInvalid(c, requestID)
		return writePromptsPreviewRequest{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request writePromptsPreviewRequest
	if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil ||
		request.SchemaVersion != writePromptsPreviewEnqueueRequestSchema ||
		!validWritePromptsPreviewStoryboardRef(request.StoryboardPreviewRef) ||
		!validWritePromptsPreviewIntent(request.ToolIntent) {
		proxy.writeInvalid(c, requestID)
		return writePromptsPreviewRequest{}, false
	}
	return request, true
}

// writePromptsPreviewRequiredFieldsPresent 验证 exact-set 必填字段非 null，输出语言只允许省略而不允许显式 null。
func writePromptsPreviewRequiredFieldsPresent(raw []byte) bool {
	var outer map[string]json.RawMessage
	if json.Unmarshal(raw, &outer) != nil || !requiredNonNullJSONFields(outer, "schema_version", "storyboard_preview_ref", "tool_intent") {
		return false
	}
	var ref map[string]json.RawMessage
	if json.Unmarshal(outer["storyboard_preview_ref"], &ref) != nil || !requiredNonNullJSONFields(ref, "id", "version", "content_digest") {
		return false
	}
	var intent map[string]json.RawMessage
	if json.Unmarshal(outer["tool_intent"], &intent) != nil || !requiredNonNullJSONFields(intent, "schema_version", "writing_instruction") {
		return false
	}
	if language, exists := intent["output_language"]; exists && bytes.Equal(bytes.TrimSpace(language), []byte("null")) {
		return false
	}
	return true
}

// validWritePromptsPreviewStoryboardRef 校验规范 UUIDv7、固定版本和小写 SHA-256。
func validWritePromptsPreviewStoryboardRef(reference WritePromptsPreviewStoryboardRef) bool {
	if !canonicalUUIDv7(reference.ID) || reference.Version != 1 || len(reference.ContentDigest) != sha256.Size*2 ||
		strings.ToLower(reference.ContentDigest) != reference.ContentDigest {
		return false
	}
	decoded, err := hex.DecodeString(reference.ContentDigest)
	return err == nil && len(decoded) == sha256.Size
}

// validWritePromptsPreviewIntent 校验写作文本和可选语言，不允许模型控制 Source 或目标集。
func validWritePromptsPreviewIntent(intent writePromptsPreviewIntentRequest) bool {
	if intent.SchemaVersion != writePromptsPreviewIntentSchema || !validPreviewText(intent.WritingInstruction, 1, 1000, false) {
		return false
	}
	return intent.OutputLanguage == "" || intent.OutputLanguage == "zh-CN" || intent.OutputLanguage == "en-US"
}

// decodeWritePromptsPreviewEnqueue 严格验证 Agent 202 DTO，不接受额外字段、重复身份或伪完成状态。
func decodeWritePromptsPreviewEnqueue(raw []byte) (writePromptsPreviewEnqueueResponse, error) {
	if !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		return writePromptsPreviewEnqueueResponse{}, errors.New("invalid Write Prompts Preview enqueue encoding")
	}
	duplicate, err := hasDuplicateJSONKey(raw)
	if err != nil || duplicate {
		return writePromptsPreviewEnqueueResponse{}, errors.New("invalid Write Prompts Preview enqueue object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var response writePromptsPreviewEnqueueResponse
	if err := decoder.Decode(&response); err != nil || ensureJSONEOF(decoder) != nil ||
		response.SchemaVersion != writePromptsPreviewEnqueueSchema || response.Status != "pending" ||
		!canonicalUUIDv7(response.RequestID) || !canonicalUUIDv7(response.SessionID) {
		return writePromptsPreviewEnqueueResponse{}, errors.New("invalid Write Prompts Preview enqueue response")
	}
	seen := make(map[string]struct{}, 4)
	for _, identity := range []string{response.InputID, response.TurnID, response.RunID, response.ToolCallID} {
		if !canonicalUUIDv7(identity) {
			return writePromptsPreviewEnqueueResponse{}, errors.New("invalid Write Prompts Preview enqueue identity")
		}
		if _, exists := seen[identity]; exists {
			return writePromptsPreviewEnqueueResponse{}, errors.New("duplicate Write Prompts Preview enqueue identity")
		}
		seen[identity] = struct{}{}
	}
	return response, nil
}

// writeBindingError 把 Binder 结果收敛为 404/409/503，不回显引用、权威摘要或内部错误。
func (proxy *WritePromptsPreviewProxy) writeBindingError(c *gin.Context, requestID string, err error) {
	switch {
	case errors.Is(err, ErrWritePromptsPreviewStoryboardNotFound):
		proxy.base.writeAgentError(c, http.StatusNotFound, "STORYBOARD_PREVIEW_NOT_FOUND", "当前 Storyboard Preview 不存在或不可访问", requestID, false)
	case errors.Is(err, ErrWritePromptsPreviewStoryboardConflict):
		proxy.base.writeAgentError(c, http.StatusConflict, "STORYBOARD_PREVIEW_CONFLICT", "当前 Storyboard Preview 已变化，请刷新工作台", requestID, false)
	default:
		proxy.writeUnavailable(c, requestID)
	}
}

// proxyUpstreamError 只透出 Agent 冻结公共错误白名单，身份错误或未知状态统一映射为 503。
func (proxy *WritePromptsPreviewProxy) proxyUpstreamError(c *gin.Context, response *http.Response, requestID string) {
	body, err := readBoundedBody(response.Body, maximumUpstreamErrorBytes)
	if err != nil || !utf8.Valid(body) || !validJSONSurrogateEscapes(body) {
		proxy.writeUnavailable(c, requestID)
		return
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		proxy.writeUnavailable(c, requestID)
		return
	}
	switch {
	case response.StatusCode == http.StatusBadRequest && envelope.Error.Code == "INVALID_ARGUMENT":
		proxy.writeInvalid(c, requestID)
	case response.StatusCode == http.StatusNotFound && envelope.Error.Code == "SESSION_NOT_FOUND":
		proxy.base.writeAgentError(c, http.StatusNotFound, "SESSION_NOT_FOUND", "Session 不存在或不可访问", requestID, false)
	case response.StatusCode == http.StatusNotFound && envelope.Error.Code == "PREVIEW_DISABLED":
		proxy.base.writeAgentError(c, http.StatusNotFound, "PREVIEW_DISABLED", "Prompt 开发预览未启用", requestID, false)
	case response.StatusCode == http.StatusConflict && envelope.Error.Code == "IDEMPOTENCY_CONFLICT":
		proxy.base.writeAgentError(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同的 Prompt 预览请求", requestID, false)
	case response.StatusCode == http.StatusConflict && envelope.Error.Code == "SESSION_LANE_BLOCKED":
		proxy.base.writeAgentError(c, http.StatusConflict, "SESSION_LANE_BLOCKED", "Session 存在未完成输入，请稍后重试", requestID, false)
	case response.StatusCode == http.StatusServiceUnavailable && envelope.Error.Code == "PERSISTENCE_UNAVAILABLE":
		proxy.base.writeAgentError(c, http.StatusServiceUnavailable, "PERSISTENCE_UNAVAILABLE", "Prompt 预览存储暂时不可用", requestID, true)
	default:
		proxy.writeUnavailable(c, requestID)
	}
}

// writeInvalid 输出统一 400，避免向浏览器暴露字段、引用或解析细节。
func (proxy *WritePromptsPreviewProxy) writeInvalid(c *gin.Context, requestID string) {
	proxy.base.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Prompt 预览请求格式无效", requestID, false)
}

// writeUnavailable 输出统一可重试 503，避免传播 Binder、Signer、Transport 或 Agent 内部错误。
func (proxy *WritePromptsPreviewProxy) writeUnavailable(c *gin.Context, requestID string) {
	proxy.base.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "Prompt 预览依赖暂时不可用", requestID, true)
}
