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
	// planStoryboardPreviewEnqueueRequestSchema 是浏览器、Business BFF 与 Agent 共用的严格入队信封版本。
	planStoryboardPreviewEnqueueRequestSchema = "plan_storyboard.preview.enqueue-request.v1"
	// planStoryboardPreviewIntentSchema 是唯一允许模型消费的规划 Intent 版本。
	planStoryboardPreviewIntentSchema = "plan_storyboard.preview.intent.v1"
	// planStoryboardPreviewEnqueueSchema 是 Agent 持久化 typed Input 后的 202 回执版本。
	planStoryboardPreviewEnqueueSchema = "plan_storyboard.preview.enqueue.v1"
	// maximumPlanStoryboardPreviewEnqueueResponseBytes 限制 Agent 202 回执，避免代理无界读取。
	maximumPlanStoryboardPreviewEnqueueResponseBytes = 8 << 10
)

var (
	// ErrPlanStoryboardPreviewCreationSpecNotFound 表示当前 Workspace 没有可由该 Owner 使用的 CreationSpec Draft。
	ErrPlanStoryboardPreviewCreationSpecNotFound = errors.New("plan storyboard preview creation spec not found")
	// ErrPlanStoryboardPreviewCreationSpecConflict 表示浏览器看到的 CreationSpec Card 已不是当前权威版本。
	ErrPlanStoryboardPreviewCreationSpecConflict = errors.New("plan storyboard preview creation spec conflict")
	// ErrPlanStoryboardPreviewCreationSpecUnavailable 表示当前 Workspace CreationSpec 绑定事实暂时无法读取。
	ErrPlanStoryboardPreviewCreationSpecUnavailable = errors.New("plan storyboard preview creation spec unavailable")
)

// PlanStoryboardPreviewCreationSpecRef 是浏览器提交并由 Business 权威绑定器复核的 CreationSpec Draft 精确引用。
type PlanStoryboardPreviewCreationSpecRef struct {
	// ID 是当前 Workspace 展示的 CreationSpec Draft UUIDv7。
	ID string `json:"id"`
	// Version 本 Development Preview 固定为一。
	Version int64 `json:"version"`
	// ContentDigest 是 Business canonical CreationSpec 内容的小写 SHA-256。
	ContentDigest string `json:"content_digest"`
}

// PlanStoryboardPreviewCreationSpecBindingRequest 描述一次 Owner、Project、Session 与可见 Card 的联合绑定请求。
type PlanStoryboardPreviewCreationSpecBindingRequest struct {
	// RequestID 是本次 BFF 请求生成的 UUIDv7，用于绑定内部 Workspace 读取断言与响应。
	RequestID string
	// UserID 是 Business Web Session 认证后的用户 UUIDv7。
	UserID string
	// ProjectID 是 Business Session Binding 解析出的当前 Project UUIDv7。
	ProjectID string
	// AgentSessionID 是公开 canonical 路由绑定的 Agent Session UUIDv7。
	AgentSessionID string
	// PresentedRef 是浏览器从当前已验证 CreationSpec Card 提交的精确引用。
	PresentedRef PlanStoryboardPreviewCreationSpecRef
}

// PlanStoryboardPreviewCreationSpecBinder 定义 BFF 转发前读取并绑定当前 Workspace CreationSpec Card 的最小权限边界。
// 实现必须同时复核 Owner、Project、Session、draft 状态、version 与 content digest；跨 Owner 统一返回 NotFound。
type PlanStoryboardPreviewCreationSpecBinder interface {
	// BindCurrent 返回当前 Workspace 的权威精确引用；若 PresentedRef 已漂移则返回 Conflict，且不得泄漏其他引用。
	BindCurrent(ctx context.Context, request PlanStoryboardPreviewCreationSpecBindingRequest) (PlanStoryboardPreviewCreationSpecRef, error)
}

// PlanStoryboardPreviewProxy 负责 Storyboard Development Preview 独立 BFF POST，不拥有 Workspace 或 Draft 业务事实。
type PlanStoryboardPreviewProxy struct {
	base    *AgentProxyHandler
	binder  PlanStoryboardPreviewCreationSpecBinder
	enabled bool
}

// NewPlanStoryboardPreviewProxy 创建窄 Storyboard BFF；正式注册仍必须显式注入 Session+CSRF 中间件。
func NewPlanStoryboardPreviewProxy(base *AgentProxyHandler, binder PlanStoryboardPreviewCreationSpecBinder, enabled bool) (*PlanStoryboardPreviewProxy, error) {
	if base == nil || binder == nil {
		return nil, fmt.Errorf("create Plan Storyboard Preview proxy: invalid dependency")
	}
	return &PlanStoryboardPreviewProxy{base: base, binder: binder, enabled: enabled}, nil
}

// Register 只注册公开 canonical POST，并强制要求调用方提供完整 Session+CSRF 写中间件。
func (proxy *PlanStoryboardPreviewProxy) Register(router gin.IRoutes, requireSessionAndCSRF gin.HandlerFunc) error {
	if proxy == nil || router == nil || requireSessionAndCSRF == nil {
		return fmt.Errorf("register Plan Storyboard Preview proxy: invalid dependency")
	}
	router.POST("/api/v1/agent/sessions/:session_id/plan-storyboard-previews", requireSessionAndCSRF, proxy.post)
	return nil
}

// planStoryboardPreviewRequest 是浏览器公开 POST 与 Agent 内部 POST 共用的 exact-set DTO。
type planStoryboardPreviewRequest struct {
	// SchemaVersion 固定为 plan_storyboard.preview.enqueue-request.v1。
	SchemaVersion string `json:"schema_version"`
	// CreationSpecRef 来自当前 Workspace CreationSpec Card，并在转发前由 Business 再绑定。
	CreationSpecRef PlanStoryboardPreviewCreationSpecRef `json:"creation_spec_ref"`
	// ToolIntent 只包含模型可控的规划字段，不包含可信身份或资源引用。
	ToolIntent planStoryboardPreviewIntentRequest `json:"tool_intent"`
}

// planStoryboardPreviewIntentRequest 是用户可控且允许传给唯一 plan_storyboard Tool 的严格 Intent。
type planStoryboardPreviewIntentRequest struct {
	// SchemaVersion 固定为 plan_storyboard.preview.intent.v1。
	SchemaVersion string `json:"schema_version"`
	// PlanningInstruction 是一至一千个 NFC Unicode scalar 的规划说明。
	PlanningInstruction string `json:"planning_instruction"`
	// TargetDurationSeconds 是可选的五至六百秒目标时长；省略与零值语义严格区分。
	TargetDurationSeconds *int64 `json:"target_duration_seconds,omitempty"`
}

// planStoryboardPreviewEnqueueResponse 是 BFF 允许返回浏览器的最小 pending/replayed 回执。
type planStoryboardPreviewEnqueueResponse struct {
	// SchemaVersion 固定为 plan_storyboard.preview.enqueue.v1。
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
	// ToolCallID 是唯一 plan_storyboard ToolCall UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// Status 固定为 pending，不能解释为 Storyboard Draft 已完成。
	Status string `json:"status"`
	// Replayed 表示同义 Idempotency-Key 返回了首次稳定身份。
	Replayed bool `json:"replayed"`
}

// post 严格校验浏览器 DTO，在一次 Owner Resolve 后绑定当前 CreationSpec，再签发专用断言并转发 canonical 请求。
func (proxy *PlanStoryboardPreviewProxy) post(c *gin.Context) {
	requestID, ok := proxy.base.newAgentRequestID(c)
	if !ok {
		return
	}
	if !proxy.enabled {
		proxy.base.writeAgentError(c, http.StatusNotFound, "PREVIEW_DISABLED", "故事板开发预览未启用", requestID, false)
		return
	}
	sessionID := c.Param("session_id")
	publicTarget := "/api/v1/agent/sessions/" + sessionID + "/plan-storyboard-previews"
	if !canonicalUUIDv7(sessionID) || c.Request.URL.RawQuery != "" || c.Request.URL.EscapedPath() != publicTarget {
		proxy.base.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Session 标识无效", requestID, false)
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
	authoritativeRef, err := proxy.binder.BindCurrent(c.Request.Context(), PlanStoryboardPreviewCreationSpecBindingRequest{
		RequestID: requestID, UserID: resolved.Principal.ID, ProjectID: access.ProjectID, AgentSessionID: sessionID,
		PresentedRef: browserRequest.CreationSpecRef,
	})
	if err != nil {
		proxy.writeBindingError(c, requestID, err)
		return
	}
	// 绑定器返回值必须同时是合法权威引用并与浏览器看到的 Card 完全相等；任何漂移都在调用 Agent 前收敛为安全冲突。
	if !validPlanStoryboardPreviewCreationSpecRef(authoritativeRef) {
		proxy.writeUnavailable(c, requestID)
		return
	}
	if authoritativeRef != browserRequest.CreationSpecRef {
		proxy.base.writeAgentError(c, http.StatusConflict, "CREATION_SPEC_CONFLICT", "当前 CreationSpec Draft 已变化，请刷新工作台", requestID, false)
		return
	}
	browserRequest.CreationSpecRef = authoritativeRef
	canonicalBody, err := json.Marshal(browserRequest)
	if err != nil {
		proxy.writeUnavailable(c, requestID)
		return
	}
	if int64(len(canonicalBody)) > proxy.base.previewBodyBytes {
		proxy.writeInvalid(c, requestID)
		return
	}
	internalTarget := "/internal/v1/workspaces/sessions/" + sessionID + "/plan-storyboard-previews"
	assertion, err := proxy.base.signer.Sign(agentidentity.Identity{
		RequestID: requestID, CanonicalTarget: internalTarget, Method: http.MethodPost,
		PrincipalUserID: resolved.Principal.ID, WebSessionID: resolved.WebSessionID,
		WebSessionVersion: resolved.WebSessionVersion, ProjectID: access.ProjectID,
		AgentSessionID: access.AgentSessionID, Scope: agentidentity.ScopePlanStoryboardPreviewWrite,
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
	// 浏览器 Cookie、CSRF、Authorization 与伪造内部断言都不复制；内部请求只保留固定协议 Header。
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
	body, err := readBoundedBody(response.Body, maximumPlanStoryboardPreviewEnqueueResponseBytes)
	if err != nil {
		proxy.writeUnavailable(c, requestID)
		return
	}
	enqueue, err := decodePlanStoryboardPreviewEnqueue(body)
	if err != nil || enqueue.RequestID != requestID || enqueue.SessionID != sessionID {
		proxy.writeUnavailable(c, requestID)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusAccepted, enqueue)
}

// decodeRequest 有界读取并拒绝重复、未知、缺失、null、尾随、非法 Unicode 与不规范业务字段。
func (proxy *PlanStoryboardPreviewProxy) decodeRequest(c *gin.Context, requestID string) (planStoryboardPreviewRequest, bool) {
	contentTypes := c.Request.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		proxy.writeInvalid(c, requestID)
		return planStoryboardPreviewRequest{}, false
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		proxy.writeInvalid(c, requestID)
		return planStoryboardPreviewRequest{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, proxy.base.previewBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	trimmed := bytes.TrimSpace(raw)
	if err != nil || len(trimmed) == 0 || trimmed[0] != '{' || !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		proxy.writeInvalid(c, requestID)
		return planStoryboardPreviewRequest{}, false
	}
	duplicate, err := hasDuplicateJSONKey(raw)
	if err != nil || duplicate || !planStoryboardPreviewRequiredFieldsPresent(raw) {
		proxy.writeInvalid(c, requestID)
		return planStoryboardPreviewRequest{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request planStoryboardPreviewRequest
	if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil ||
		request.SchemaVersion != planStoryboardPreviewEnqueueRequestSchema ||
		!validPlanStoryboardPreviewCreationSpecRef(request.CreationSpecRef) ||
		!validPlanStoryboardPreviewIntent(request.ToolIntent) {
		proxy.writeInvalid(c, requestID)
		return planStoryboardPreviewRequest{}, false
	}
	return request, true
}

// planStoryboardPreviewRequiredFieldsPresent 逐层验证 exact-set 必填字段非 null，并允许目标时长只省略、不显式传 null。
func planStoryboardPreviewRequiredFieldsPresent(raw []byte) bool {
	var outer map[string]json.RawMessage
	if json.Unmarshal(raw, &outer) != nil || !requiredNonNullJSONFields(outer, "schema_version", "creation_spec_ref", "tool_intent") {
		return false
	}
	var ref map[string]json.RawMessage
	if json.Unmarshal(outer["creation_spec_ref"], &ref) != nil || !requiredNonNullJSONFields(ref, "id", "version", "content_digest") {
		return false
	}
	var intent map[string]json.RawMessage
	if json.Unmarshal(outer["tool_intent"], &intent) != nil || !requiredNonNullJSONFields(intent, "schema_version", "planning_instruction") {
		return false
	}
	if duration, exists := intent["target_duration_seconds"]; exists && bytes.Equal(bytes.TrimSpace(duration), []byte("null")) {
		return false
	}
	return true
}

// requiredNonNullJSONFields 验证指定字段都存在且不是显式 null；未知字段由 typed Decoder 统一拒绝。
func requiredNonNullJSONFields(fields map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		value, exists := fields[name]
		if !exists || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return true
}

// validPlanStoryboardPreviewCreationSpecRef 校验规范 UUIDv7、固定版本和小写 SHA-256。
func validPlanStoryboardPreviewCreationSpecRef(reference PlanStoryboardPreviewCreationSpecRef) bool {
	if !canonicalUUIDv7(reference.ID) || reference.Version != 1 || len(reference.ContentDigest) != sha256.Size*2 ||
		strings.ToLower(reference.ContentDigest) != reference.ContentDigest {
		return false
	}
	decoded, err := hex.DecodeString(reference.ContentDigest)
	return err == nil && len(decoded) == sha256.Size
}

// validPlanStoryboardPreviewIntent 校验模型可控字段不包含边界空白、控制字符、非 NFC 或越界时长。
func validPlanStoryboardPreviewIntent(intent planStoryboardPreviewIntentRequest) bool {
	if intent.SchemaVersion != planStoryboardPreviewIntentSchema || !validPreviewText(intent.PlanningInstruction, 1, 1000, false) {
		return false
	}
	return intent.TargetDurationSeconds == nil || (*intent.TargetDurationSeconds >= 5 && *intent.TargetDurationSeconds <= 600)
}

// decodePlanStoryboardPreviewEnqueue 严格验证 Agent 202 DTO，不接受额外字段、重复身份或伪完成状态。
func decodePlanStoryboardPreviewEnqueue(raw []byte) (planStoryboardPreviewEnqueueResponse, error) {
	if !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		return planStoryboardPreviewEnqueueResponse{}, errors.New("invalid Plan Storyboard Preview enqueue encoding")
	}
	duplicate, err := hasDuplicateJSONKey(raw)
	if err != nil || duplicate {
		return planStoryboardPreviewEnqueueResponse{}, errors.New("invalid Plan Storyboard Preview enqueue object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var response planStoryboardPreviewEnqueueResponse
	if err := decoder.Decode(&response); err != nil || ensureJSONEOF(decoder) != nil ||
		response.SchemaVersion != planStoryboardPreviewEnqueueSchema || response.Status != "pending" ||
		!canonicalUUIDv7(response.RequestID) || !canonicalUUIDv7(response.SessionID) {
		return planStoryboardPreviewEnqueueResponse{}, errors.New("invalid Plan Storyboard Preview enqueue response")
	}
	seen := make(map[string]struct{}, 4)
	for _, identity := range []string{response.InputID, response.TurnID, response.RunID, response.ToolCallID} {
		if !canonicalUUIDv7(identity) {
			return planStoryboardPreviewEnqueueResponse{}, errors.New("invalid Plan Storyboard Preview enqueue identity")
		}
		if _, exists := seen[identity]; exists {
			return planStoryboardPreviewEnqueueResponse{}, errors.New("duplicate Plan Storyboard Preview enqueue identity")
		}
		seen[identity] = struct{}{}
	}
	return response, nil
}

// writeBindingError 把 Binder 结果收敛为 404/409/503，不回显 PresentedRef、权威摘要或内部错误。
func (proxy *PlanStoryboardPreviewProxy) writeBindingError(c *gin.Context, requestID string, err error) {
	switch {
	case errors.Is(err, ErrPlanStoryboardPreviewCreationSpecNotFound):
		proxy.base.writeAgentError(c, http.StatusNotFound, "CREATION_SPEC_NOT_FOUND", "当前 CreationSpec Draft 不存在或不可访问", requestID, false)
	case errors.Is(err, ErrPlanStoryboardPreviewCreationSpecConflict):
		proxy.base.writeAgentError(c, http.StatusConflict, "CREATION_SPEC_CONFLICT", "当前 CreationSpec Draft 已变化，请刷新工作台", requestID, false)
	default:
		proxy.writeUnavailable(c, requestID)
	}
}

// proxyUpstreamError 只透出 Agent 冻结公共错误白名单；身份错误、未知状态或非法 Envelope 一律映射 503。
func (proxy *PlanStoryboardPreviewProxy) proxyUpstreamError(c *gin.Context, response *http.Response, requestID string) {
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
		proxy.base.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "故事板预览请求参数无效", requestID, false)
	case response.StatusCode == http.StatusNotFound && envelope.Error.Code == "SESSION_NOT_FOUND":
		proxy.base.writeAgentError(c, http.StatusNotFound, "SESSION_NOT_FOUND", "Session 不存在或不可访问", requestID, false)
	case response.StatusCode == http.StatusNotFound && envelope.Error.Code == "PREVIEW_DISABLED":
		proxy.base.writeAgentError(c, http.StatusNotFound, "PREVIEW_DISABLED", "故事板开发预览未启用", requestID, false)
	case response.StatusCode == http.StatusConflict && envelope.Error.Code == "IDEMPOTENCY_CONFLICT":
		proxy.base.writeAgentError(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同的故事板预览请求", requestID, false)
	case response.StatusCode == http.StatusConflict && envelope.Error.Code == "SESSION_LANE_BLOCKED":
		proxy.base.writeAgentError(c, http.StatusConflict, "SESSION_LANE_BLOCKED", "Session 存在未完成输入，请稍后重试", requestID, false)
	case response.StatusCode == http.StatusServiceUnavailable && envelope.Error.Code == "PERSISTENCE_UNAVAILABLE":
		proxy.base.writeAgentError(c, http.StatusServiceUnavailable, "PERSISTENCE_UNAVAILABLE", "故事板预览存储暂时不可用", requestID, true)
	default:
		proxy.writeUnavailable(c, requestID)
	}
}

// writeInvalid 输出统一 400，避免向浏览器暴露具体字段、引用或解析细节。
func (proxy *PlanStoryboardPreviewProxy) writeInvalid(c *gin.Context, requestID string) {
	proxy.base.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "故事板预览请求格式无效", requestID, false)
}

// writeUnavailable 输出统一可重试 503，避免传播 Binder、Signer、Transport 或 Agent 内部错误。
func (proxy *PlanStoryboardPreviewProxy) writeUnavailable(c *gin.Context, requestID string) {
	proxy.base.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "故事板预览依赖暂时不可用", requestID, true)
}
