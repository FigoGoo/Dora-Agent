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
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/planstoryboardruntime"
	"github.com/gin-gonic/gin"
)

const (
	planStoryboardPreviewMaxBodyBytes      = 64 * 1024
	planStoryboardPreviewRequestSchema     = "plan_storyboard.preview.enqueue-request.v1"
	planStoryboardPreviewAccessScopeSchema = "plan_storyboard.access_scope.v1"
)

// PlanStoryboardPreviewService 是故事板内部 POST Handler 消费的 persist-only typed Input 入队端口。
type PlanStoryboardPreviewService interface {
	// Enqueue 原子保存 typed Input 与预分配执行身份；实现不得在 HTTP 请求内等待模型或 Graph。
	Enqueue(context.Context, planstoryboardruntime.EnqueueRequest) (planstoryboardruntime.EnqueueResponse, error)
}

// PlanStoryboardPreviewHandler 实现唯一内部 canonical Storyboard Development Preview POST。
type PlanStoryboardPreviewHandler struct {
	verifier         IdentityVerifier
	service          PlanStoryboardPreviewService
	ids              IDGenerator
	intentKeyVersion string
}

// NewPlanStoryboardPreviewHandler 创建本地 Profile 启用态 Handler；关闭态不得构造或注册它。
func NewPlanStoryboardPreviewHandler(
	verifier IdentityVerifier,
	service PlanStoryboardPreviewService,
	ids IDGenerator,
	intentKeyVersion string,
) (*PlanStoryboardPreviewHandler, error) {
	if verifier == nil || service == nil || ids == nil || intentKeyVersion == "" || len(intentKeyVersion) > 64 {
		return nil, fmt.Errorf("create Plan Storyboard Preview handler: invalid dependency or key version")
	}
	return &PlanStoryboardPreviewHandler{
		verifier: verifier, service: service, ids: ids, intentKeyVersion: intentKeyVersion,
	}, nil
}

// Register 只注册 Business BFF 可调用的内部 canonical Storyboard Preview 路径。
func (h *PlanStoryboardPreviewHandler) Register(router gin.IRoutes) {
	router.POST("/internal/v1/workspaces/sessions/:session_id/plan-storyboard-previews", h.post)
}

// PlanStoryboardPreviewEnqueueResponse 是 Agent 返回 Business BFF 的 exact 202 pending/replayed DTO。
type PlanStoryboardPreviewEnqueueResponse struct {
	// SchemaVersion 固定为 plan_storyboard.preview.enqueue.v1。
	SchemaVersion string `json:"schema_version"`
	// RequestID 是内部身份断言绑定的 UUIDv7。
	RequestID string `json:"request_id"`
	// SessionID 是 canonical 路由绑定的 Session UUIDv7。
	SessionID string `json:"session_id"`
	// InputID 是已可靠持久化的 typed Input UUIDv7。
	InputID string `json:"input_id"`
	// TurnID 是首次入队事务预分配且重放不变的 Turn UUIDv7。
	TurnID string `json:"turn_id"`
	// RunID 是首次入队事务预分配且重放不变的 Run UUIDv7。
	RunID string `json:"run_id"`
	// ToolCallID 是唯一 plan_storyboard ToolCall 的 UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// Status 固定为 pending，不能解释为 Storyboard Draft 已完成。
	Status string `json:"status"`
	// Replayed 表示同义 Idempotency-Key 返回首次稳定身份。
	Replayed bool `json:"replayed"`
}

// planStoryboardPreviewEnqueueRequest 是内部 HTTP 边界 exact-set；可信引用与模型可控 Intent 必须分栏。
type planStoryboardPreviewEnqueueRequest struct {
	// SchemaVersion 固定为 plan_storyboard.preview.enqueue-request.v1。
	SchemaVersion string `json:"schema_version"`
	// CreationSpecRef 是 Business BFF 已验证并随 Turn Context 冻结的 Draft 引用。
	CreationSpecRef planStoryboardPreviewCreationSpecRef `json:"creation_spec_ref"`
	// ToolIntent 是只含规划字段的原始 JSON，严格验证后再 canonical 编码。
	ToolIntent json.RawMessage `json:"tool_intent"`
}

// planStoryboardPreviewCreationSpecRef 是 HTTP wire 中由 Business BFF 绑定的可信 Draft 引用 DTO。
type planStoryboardPreviewCreationSpecRef struct {
	// ID 是当前 Workspace CreationSpec Draft UUIDv7。
	ID string `json:"id"`
	// Version 本批固定为 1。
	Version int64 `json:"version"`
	// ContentDigest 是当前 Draft 内容的小写 SHA-256。
	ContentDigest string `json:"content_digest"`
}

// post 先认证 canonical 路由，再严格拆分可信引用与 Tool Intent，最后只执行一次可靠入队。
func (h *PlanStoryboardPreviewHandler) post(c *gin.Context) {
	sessionID, ok := canonicalUUIDv7(c.Param("session_id"))
	target := "/internal/v1/workspaces/sessions/" + sessionID + "/plan-storyboard-previews"
	if !ok || c.Request.URL.RawQuery != "" || c.Request.URL.EscapedPath() != target ||
		!singleExactHeader(c.Request.Header, "Content-Type", "application/json") {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "故事板预览请求无效", h.newRequestID(), false)
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
		Scope: httpidentity.ScopePlanStoryboardPreviewWrite, AgentSessionID: sessionID,
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
	// 即使 Verifier 被测试替身或未来适配器替换，Handler 仍复核最小 Owner/Session/Scope 绑定，避免错租户式误投递。
	if !validPlanStoryboardPreviewClaims(claims, sessionID) {
		h.writeError(c, http.StatusUnauthorized, errorCodeInternalIdentityInvalid,
			"内部身份断言无效", h.newRequestID(), false)
		return
	}
	requestCtx, cancel := context.WithDeadline(c.Request.Context(), claims.ExpiresAt)
	defer cancel()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, planStoryboardPreviewMaxBodyBytes+1)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(body) == 0 || len(body) > planStoryboardPreviewMaxBodyBytes {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument,
			"故事板预览请求正文无效", claims.RequestID, false)
		return
	}
	creationSpecRef, intentJSON, err := decodePlanStoryboardPreviewEnqueueRequest(body)
	if err != nil {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument,
			"故事板预览请求正文无效", claims.RequestID, false)
		return
	}
	accessDigest, err := planStoryboardPreviewAccessScopeDigest(claims)
	if err != nil {
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable,
			"Agent 持久化暂时不可用", claims.RequestID, true)
		return
	}
	enqueueRequest := planstoryboardruntime.EnqueueRequest{
		RequestID: claims.RequestID, SessionID: sessionID, UserID: claims.PrincipalUserID,
		ProjectID: claims.ProjectID, IdempotencyKey: idempotencyKey, IntentJSON: intentJSON,
		AccessScopeRef:    httpidentity.ScopePlanStoryboardPreviewWrite,
		AccessScopeDigest: accessDigest, IntentKeyVersion: h.intentKeyVersion,
	}
	// 显式 Mapper 防止 HTTP DTO、Runtime DTO 与 Graph DTO 通过 JSON 往返或反射产生字段漂移。
	enqueueRequest.CreationSpecRef.ID = creationSpecRef.ID
	enqueueRequest.CreationSpecRef.Version = creationSpecRef.Version
	enqueueRequest.CreationSpecRef.ContentDigest = creationSpecRef.ContentDigest
	result, err := h.service.Enqueue(requestCtx, enqueueRequest)
	if err != nil {
		h.writeServiceError(c, err, claims.RequestID)
		return
	}
	if !validPlanStoryboardPreviewEnqueueResponse(result) {
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable,
			"Agent 持久化暂时不可用", claims.RequestID, true)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.PureJSON(http.StatusAccepted, PlanStoryboardPreviewEnqueueResponse{
		SchemaVersion: result.SchemaVersion, RequestID: claims.RequestID, SessionID: sessionID,
		InputID: result.InputID, TurnID: result.TurnID, RunID: result.RunID, ToolCallID: result.ToolCallID,
		Status: result.Status, Replayed: result.Replayed,
	})
}

// decodePlanStoryboardPreviewEnqueueRequest 拒绝重复/未知/尾随/null/非法 Unicode，并输出 canonical Tool Intent。
func decodePlanStoryboardPreviewEnqueueRequest(body []byte) (planStoryboardPreviewCreationSpecRef, []byte, error) {
	if len(body) == 0 || len(body) > planStoryboardPreviewMaxBodyBytes || !utf8.Valid(body) ||
		!validPlanStoryboardPreviewJSONSurrogateEscapes(body) || rejectPlanStoryboardPreviewDuplicateFields(body) != nil {
		return planStoryboardPreviewCreationSpecRef{}, nil, fmt.Errorf("decode Plan Storyboard Preview request: invalid JSON or UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var request planStoryboardPreviewEnqueueRequest
	if err := decoder.Decode(&request); err != nil {
		return planStoryboardPreviewCreationSpecRef{}, nil, fmt.Errorf("decode Plan Storyboard Preview request: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return planStoryboardPreviewCreationSpecRef{}, nil, fmt.Errorf("decode Plan Storyboard Preview request: trailing JSON")
	}
	if request.SchemaVersion != planStoryboardPreviewRequestSchema ||
		!validPlanStoryboardPreviewCreationSpecRef(request.CreationSpecRef) {
		return planStoryboardPreviewCreationSpecRef{}, nil, fmt.Errorf("decode Plan Storyboard Preview request: invalid envelope")
	}
	intent, err := planstoryboardruntime.DecodeIntent(request.ToolIntent)
	if err != nil {
		return planStoryboardPreviewCreationSpecRef{}, nil, fmt.Errorf("decode Plan Storyboard Preview request: invalid Tool Intent")
	}
	if len(intent.JSON) == 0 || len(intent.JSON) > planStoryboardPreviewMaxBodyBytes {
		return planStoryboardPreviewCreationSpecRef{}, nil, fmt.Errorf("decode Plan Storyboard Preview request: canonical Tool Intent failed")
	}
	return request.CreationSpecRef, intent.JSON, nil
}

// validPlanStoryboardPreviewCreationSpecRef 校验可信 Draft 引用的 UUIDv7、固定版本与小写 SHA-256。
func validPlanStoryboardPreviewCreationSpecRef(ref planStoryboardPreviewCreationSpecRef) bool {
	if _, ok := canonicalUUIDv7(ref.ID); !ok || ref.Version != 1 || len(ref.ContentDigest) != sha256.Size*2 ||
		strings.ToLower(ref.ContentDigest) != ref.ContentDigest {
		return false
	}
	decoded, err := hex.DecodeString(ref.ContentDigest)
	return err == nil && len(decoded) == sha256.Size
}

// validPlanStoryboardPreviewClaims 防御性复核 Verifier 返回的身份集合与当前写路由完全绑定。
func validPlanStoryboardPreviewClaims(claims httpidentity.Claims, sessionID string) bool {
	for _, value := range []string{
		claims.RequestID, claims.PrincipalUserID, claims.ProjectID, claims.AgentSessionID, claims.WebSessionID,
	} {
		if _, ok := canonicalUUIDv7(value); !ok {
			return false
		}
	}
	return claims.AgentSessionID == sessionID && claims.Scope == httpidentity.ScopePlanStoryboardPreviewWrite &&
		claims.WebSessionVersion > 0 && !claims.IssuedAt.IsZero() && claims.ExpiresAt.After(claims.IssuedAt)
}

// planStoryboardPreviewAccessScopeDigest 冻结已认证 User、Project、Session 与 Web Session 授权事实。
func planStoryboardPreviewAccessScopeDigest(claims httpidentity.Claims) (string, error) {
	wire := struct {
		SchemaVersion     string `json:"schema_version"`
		Scope             string `json:"scope"`
		PrincipalUserID   string `json:"principal_user_id"`
		ProjectID         string `json:"project_id"`
		AgentSessionID    string `json:"agent_session_id"`
		WebSessionID      string `json:"web_session_id"`
		WebSessionVersion int64  `json:"web_session_version"`
	}{
		SchemaVersion: planStoryboardPreviewAccessScopeSchema, Scope: claims.Scope,
		PrincipalUserID: claims.PrincipalUserID, ProjectID: claims.ProjectID,
		AgentSessionID: claims.AgentSessionID, WebSessionID: claims.WebSessionID,
		WebSessionVersion: claims.WebSessionVersion,
	}
	encoded, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("digest Plan Storyboard Preview access scope: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// validPlanStoryboardPreviewEnqueueResponse 拒绝 Runtime 漂移的 Schema、状态或非规范执行身份。
func validPlanStoryboardPreviewEnqueueResponse(result planstoryboardruntime.EnqueueResponse) bool {
	if result.SchemaVersion != planstoryboardruntime.EnqueueResponseSchemaVersion ||
		result.Status != planstoryboardruntime.EnqueuePendingStatus {
		return false
	}
	seen := make(map[string]struct{}, 4)
	for _, value := range []string{result.InputID, result.TurnID, result.RunID, result.ToolCallID} {
		if _, ok := canonicalUUIDv7(value); !ok {
			return false
		}
		if _, duplicated := seen[value]; duplicated {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

// writeServiceError 将 Runtime 稳定错误映射为无内部细节的 HTTP Envelope。
func (h *PlanStoryboardPreviewHandler) writeServiceError(c *gin.Context, err error, requestID string) {
	switch {
	case errors.Is(err, planstoryboardruntime.ErrInvalidInput):
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "故事板预览请求正文无效", requestID, false)
	case errors.Is(err, planstoryboardruntime.ErrNotFound):
		h.writeError(c, http.StatusNotFound, errorCodeSessionNotFound, "Session 不存在或不可访问", requestID, false)
	case errors.Is(err, planstoryboardruntime.ErrIdempotencyConflict):
		h.writeError(c, http.StatusConflict, errorCodeIdempotencyConflict, "Idempotency-Key 已绑定其他请求", requestID, false)
	case errors.Is(err, planstoryboardruntime.ErrSessionLaneBlocked):
		h.writeError(c, http.StatusConflict, errorCodeSessionLaneBlocked, "Session 存在尚未完成的先行输入", requestID, false)
	default:
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable,
			"Agent 持久化暂时不可用", requestID, true)
	}
}

// writeError 输出 no-store、稳定且不包含正文、资源引用或内部错误原文的错误 DTO。
func (h *PlanStoryboardPreviewHandler) writeError(
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

// newRequestID 为身份校验前错误生成规范 UUIDv7；随机源异常时只返回固定安全占位。
func (h *PlanStoryboardPreviewHandler) newRequestID() string {
	value, err := h.ids.New()
	if normalized, ok := canonicalUUIDv7(value); err != nil || !ok {
		return "019f0000-0000-7000-8000-000000000000"
	} else {
		return normalized
	}
}

// rejectPlanStoryboardPreviewDuplicateFields 递归拒绝重复字段、null 和根值后的第二个 JSON 值。
func rejectPlanStoryboardPreviewDuplicateFields(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := consumePlanStoryboardPreviewUniqueJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return fmt.Errorf("Plan Storyboard Preview JSON contains trailing value")
	}
	return nil
}

// consumePlanStoryboardPreviewUniqueJSONValue 为每个 Object 独立维护字段集合并拒绝任何 null。
func consumePlanStoryboardPreviewUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("Plan Storyboard Preview JSON null is forbidden")
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			fieldToken, fieldErr := decoder.Token()
			if fieldErr != nil {
				return fieldErr
			}
			field, ok := fieldToken.(string)
			if !ok {
				return fmt.Errorf("Plan Storyboard Preview JSON object field is invalid")
			}
			if _, duplicated := seen[field]; duplicated {
				return fmt.Errorf("Plan Storyboard Preview JSON contains duplicate field")
			}
			seen[field] = struct{}{}
			if err := consumePlanStoryboardPreviewUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim('}') {
			return fmt.Errorf("Plan Storyboard Preview JSON object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := consumePlanStoryboardPreviewUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim(']') {
			return fmt.Errorf("Plan Storyboard Preview JSON array is not closed")
		}
	default:
		return fmt.Errorf("Plan Storyboard Preview JSON delimiter is invalid")
	}
	return nil
}

// validPlanStoryboardPreviewJSONSurrogateEscapes 拒绝 JSON 字符串中的孤立 UTF-16 surrogate 转义。
func validPlanStoryboardPreviewJSONSurrogateEscapes(raw []byte) bool {
	inString := false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '"':
			inString = !inString
		case '\\':
			if !inString || index+1 >= len(raw) {
				continue
			}
			if raw[index+1] != 'u' {
				index++
				continue
			}
			code, ok := parsePlanStoryboardPreviewJSONHexCodeUnit(raw, index+2)
			if !ok {
				return false
			}
			if code >= 0xD800 && code <= 0xDBFF {
				next := index + 6
				if next+6 > len(raw) || raw[next] != '\\' || raw[next+1] != 'u' {
					return false
				}
				low, lowOK := parsePlanStoryboardPreviewJSONHexCodeUnit(raw, next+2)
				if !lowOK || low < 0xDC00 || low > 0xDFFF {
					return false
				}
				index += 11
				continue
			}
			if code >= 0xDC00 && code <= 0xDFFF {
				return false
			}
			index += 5
		}
	}
	return true
}

// parsePlanStoryboardPreviewJSONHexCodeUnit 解析一个 JSON \uXXXX code unit，不接受短值或非十六进制。
func parsePlanStoryboardPreviewJSONHexCodeUnit(raw []byte, start int) (uint16, bool) {
	if start < 0 || start+4 > len(raw) {
		return 0, false
	}
	var value uint16
	for _, character := range raw[start : start+4] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value += uint16(character - '0')
		case character >= 'a' && character <= 'f':
			value += uint16(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value += uint16(character-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}
