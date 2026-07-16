package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/gin-gonic/gin"
)

const skillGovernanceMaxResponseBytes = 2 << 20

// SkillGovernanceService 是治理 HTTP Handler 消费的独立应用边界。
type SkillGovernanceService interface {
	ListGovernance(ctx context.Context, principal skill.GovernancePrincipal, status string, cursor string) (skill.GovernanceQueueResult, error)
	FindGovernanceDetail(ctx context.Context, principal skill.GovernancePrincipal, skillID string) (skill.GovernanceDetailDTO, error)
	DecideGovernance(ctx context.Context, command skill.GovernanceDecisionCommand) (skill.GovernanceDecisionResult, error)
}

// SkillGovernanceQueueResponse 是治理列表的稳定分页 Envelope。
type SkillGovernanceQueueResponse struct {
	Items      []skill.GovernanceQueueItemDTO `json:"items"`
	NextCursor *string                        `json:"next_cursor"`
	RequestID  string                         `json:"request_id"`
}

// SkillGovernanceDetailResponse 是治理详情的稳定 Envelope。
type SkillGovernanceDetailResponse struct {
	Skill     skill.GovernanceDetailDTO `json:"skill"`
	RequestID string                    `json:"request_id"`
}

// SkillGovernanceDecisionRequest 是治理写入口唯一接受的严格 JSON DTO。
type SkillGovernanceDecisionRequest struct {
	Action            string `json:"action"`
	ReasonCode        string `json:"reason_code"`
	ApprovalReference string `json:"approval_reference"`
}

// SkillGovernanceDecisionResponse 是首次迁移与同义重放共用的 Envelope。
type SkillGovernanceDecisionResponse struct {
	Skill     skill.GovernanceDecisionDTO `json:"skill"`
	RequestID string                      `json:"request_id"`
}

// SkillGovernanceHandler 负责治理列表、详情和决定的严格 transport、capability 与来源地址边界。
type SkillGovernanceHandler struct {
	service    SkillGovernanceService
	requestIDs auth.IDGenerator
}

// NewSkillGovernanceHandler 校验应用服务与 Request ID Generator 后创建治理 Handler。
func NewSkillGovernanceHandler(service SkillGovernanceService, requestIDs auth.IDGenerator) (*SkillGovernanceHandler, error) {
	if service == nil || requestIDs == nil {
		return nil, errors.New("create skill governance HTTP handler: required dependency is missing")
	}
	return &SkillGovernanceHandler{service: service, requestIDs: requestIDs}, nil
}

// Register 由 Composition Root 显式提供动态 Session 与 CSRF 中间件。
func (handler *SkillGovernanceHandler) Register(router gin.IRoutes, requireRead gin.HandlerFunc, requireWrite gin.HandlerFunc) {
	router.GET("/api/v1/admin/skill-governance", requireRead, handler.list)
	router.GET("/api/v1/admin/skill-governance/:skill_id", requireRead, handler.detail)
	router.POST("/api/v1/admin/skill-governance/:skill_id/decisions", requireWrite, handler.decide)
}

func (handler *SkillGovernanceHandler) list(c *gin.Context) {
	requestID, principal, ok := handler.requestContext(c)
	if !ok || !handler.requireCapability(c, principal, requestID, "list") {
		return
	}
	query := c.Request.URL.Query()
	for key := range query {
		if key != "status" && key != "cursor" {
			handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
			return
		}
	}
	if len(query["status"]) != 1 || len(query["cursor"]) > 1 ||
		(len(query["cursor"]) == 1 && query.Get("cursor") == "") {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	result, err := handler.service.ListGovernance(
		c.Request.Context(), governancePrincipal(principal), query.Get("status"), query.Get("cursor"),
	)
	if err != nil {
		handler.writeMappedError(c, err, requestID, principal, "list")
		return
	}
	items := result.Items
	if items == nil {
		items = []skill.GovernanceQueueItemDTO{}
	}
	var nextCursor *string
	if result.NextCursor != "" {
		nextCursor = &result.NextCursor
	}
	handler.writeBounded(c, http.StatusOK, SkillGovernanceQueueResponse{
		Items: items, NextCursor: nextCursor, RequestID: requestID,
	}, "", requestID)
}

func (handler *SkillGovernanceHandler) detail(c *gin.Context) {
	requestID, principal, ok := handler.requestContext(c)
	if !ok || !handler.requireCapability(c, principal, requestID, "detail") {
		return
	}
	if len(c.Request.URL.Query()) != 0 {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	result, err := handler.service.FindGovernanceDetail(c.Request.Context(), governancePrincipal(principal), c.Param("skill_id"))
	if err != nil {
		handler.writeMappedError(c, err, requestID, principal, "detail")
		return
	}
	handler.writeBounded(c, http.StatusOK, SkillGovernanceDetailResponse{Skill: result, RequestID: requestID}, result.GovernanceETag, requestID)
}

func (handler *SkillGovernanceHandler) decide(c *gin.Context) {
	requestID, principal, ok := handler.requestContext(c)
	if !ok || !handler.requireCapability(c, principal, requestID, "governance_transition") {
		return
	}
	if len(c.Request.URL.Query()) != 0 {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	ifMatchValues := c.Request.Header.Values("If-Match")
	idempotencyValues := c.Request.Header.Values("Idempotency-Key")
	if len(ifMatchValues) != 1 || len(idempotencyValues) > 1 || skill.ValidateStrongGovernanceETag(valueAt(ifMatchValues, 0)) != nil {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	if len(idempotencyValues) == 0 {
		handler.writeError(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "幂等键无效", requestID, false)
		return
	}
	sourceAddress, err := governanceSourceAddress(c.Request.RemoteAddr)
	if err != nil {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	request, ok := handler.decodeDecision(c, requestID)
	if !ok {
		return
	}
	result, err := handler.service.DecideGovernance(c.Request.Context(), skill.GovernanceDecisionCommand{
		Governor: governancePrincipal(principal), SkillID: c.Param("skill_id"),
		Action: request.Action, ReasonCode: request.ReasonCode, ApprovalReference: request.ApprovalReference,
		SourceAddress: sourceAddress, IfMatch: ifMatchValues[0], IdempotencyKey: idempotencyValues[0], RequestID: requestID,
	})
	if err != nil {
		handler.writeMappedError(c, err, requestID, principal, "governance_transition")
		return
	}
	handler.writeBounded(c, http.StatusOK, SkillGovernanceDecisionResponse{
		Skill: result.Skill, RequestID: requestID,
	}, result.Skill.GovernanceETag, requestID)
}

func (handler *SkillGovernanceHandler) decodeDecision(c *gin.Context, requestID string) (SkillGovernanceDecisionRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return SkillGovernanceDecisionRequest{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2048)
	raw, err := io.ReadAll(c.Request.Body)
	trimmed := bytes.TrimSpace(raw)
	if err != nil || !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) || len(trimmed) == 0 || trimmed[0] != '{' {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return SkillGovernanceDecisionRequest{}, false
	}
	duplicateKey, err := hasDuplicateTopLevelJSONKey(raw)
	if err != nil || duplicateKey {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return SkillGovernanceDecisionRequest{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request SkillGovernanceDecisionRequest
	if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return SkillGovernanceDecisionRequest{}, false
	}
	return request, true
}

func (handler *SkillGovernanceHandler) requestContext(c *gin.Context) (string, auth.Principal, bool) {
	requestID, err := handler.requestIDs.New()
	if err != nil || !canonicalUUIDv7(requestID) {
		handler.writeError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", skillEmergencyRequestID, true)
		return "", auth.Principal{}, false
	}
	principal, ok := auth.PrincipalFromContext(c.Request.Context())
	if !ok {
		handler.writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
		return "", auth.Principal{}, false
	}
	return requestID, principal, true
}

func (handler *SkillGovernanceHandler) requireCapability(c *gin.Context, principal auth.Principal, requestID string, action string) bool {
	for _, capability := range principal.Capabilities {
		if capability == skill.GovernanceCapability {
			return true
		}
	}
	handler.auditCapabilityDenied(c, principal, requestID, action)
	handler.writeError(c, http.StatusForbidden, "SKILL_GOVERNANCE_CAPABILITY_REQUIRED", "当前账号没有 Skill 治理权限", requestID, false)
	return false
}

func (handler *SkillGovernanceHandler) auditCapabilityDenied(c *gin.Context, principal auth.Principal, requestID string, action string) {
	slog.WarnContext(c.Request.Context(), "Skill Governance 授权审计事件",
		"event_type", "security.authorization.v1",
		"route", safeAuditRoute(c),
		"action", action,
		"decision", "denied",
		"actor_id", principal.ID,
		"request_id", requestID,
		"error_code", "SKILL_GOVERNANCE_CAPABILITY_REQUIRED",
	)
}

func governancePrincipal(principal auth.Principal) skill.GovernancePrincipal {
	return skill.GovernancePrincipal{UserID: principal.ID, Capabilities: append([]string(nil), principal.Capabilities...)}
}

// governanceSourceAddress 只信任 TCP peer，不读取任何代理 Header。
func governanceSourceAddress(remoteAddress string) (string, error) {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return "", skill.ErrInvalidGovernanceRequest
	}
	address, err := netip.ParseAddr(host)
	if err != nil || address.Zone() != "" {
		return "", skill.ErrInvalidGovernanceRequest
	}
	return address.Unmap().String(), nil
}

func (handler *SkillGovernanceHandler) writeBounded(c *gin.Context, status int, response any, etag string, requestID string) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(response); err != nil || body.Len() > skillGovernanceMaxResponseBytes {
		handler.writeError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", requestID, true)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/json; charset=utf-8")
	if etag != "" {
		c.Header("ETag", etag)
	}
	c.Status(status)
	_, _ = c.Writer.Write(body.Bytes())
}

func (handler *SkillGovernanceHandler) writeMappedError(c *gin.Context, err error, requestID string, principal auth.Principal, action string) {
	switch {
	case errors.Is(err, skill.ErrInvalidGovernanceRequest), errors.Is(err, skill.ErrInvalidCursor):
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
	case errors.Is(err, skill.ErrInvalidIdempotencyKey):
		handler.writeError(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "幂等键无效", requestID, false)
	case errors.Is(err, skill.ErrGovernanceCapabilityRequired):
		handler.auditCapabilityDenied(c, principal, requestID, action)
		handler.writeError(c, http.StatusForbidden, "SKILL_GOVERNANCE_CAPABILITY_REQUIRED", "当前账号没有 Skill 治理权限", requestID, false)
	case errors.Is(err, skill.ErrGovernanceNotFound):
		handler.writeError(c, http.StatusNotFound, "SKILL_GOVERNANCE_NOT_FOUND", "Skill 不存在或尚未发布", requestID, false)
	case errors.Is(err, skill.ErrGovernanceConflict):
		handler.writeError(c, http.StatusConflict, "SKILL_GOVERNANCE_CONFLICT", "治理状态已发生变化，请刷新后重试", requestID, false)
	case errors.Is(err, skill.ErrIdempotencyConflict):
		handler.writeError(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同的请求", requestID, false)
	default:
		handler.writeError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", requestID, true)
	}
}

func (handler *SkillGovernanceHandler) writeError(c *gin.Context, status int, code string, message string, requestID string, retryable bool) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: requestID, Retryable: retryable, Details: ErrorDetails{},
	}})
}

func valueAt(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return values[index]
}
