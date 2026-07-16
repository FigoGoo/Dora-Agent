package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/gin-gonic/gin"
)

const skillReviewMaxResponseBytes = 3 << 20

// SkillReviewService 是 Reviewer HTTP 消费的专用应用边界。
type SkillReviewService interface {
	ListReviewQueue(ctx context.Context, reviewer skill.ReviewerPrincipal, status string, cursor string) (skill.ReviewerQueueResult, error)
	FindReviewDetail(ctx context.Context, reviewer skill.ReviewerPrincipal, reviewID string) (skill.ReviewerDetailDTO, error)
	ApproveAndPublish(ctx context.Context, command skill.ApproveAndPublishServiceCommand) (skill.ApproveAndPublishResult, error)
}

type SkillReviewQueueResponse struct {
	Items      []skill.ReviewerQueueItemDTO `json:"items"`
	NextCursor *string                      `json:"next_cursor"`
	RequestID  string                       `json:"request_id"`
}

type SkillReviewDetailResponse struct {
	Review    skill.ReviewerDetailDTO `json:"review"`
	RequestID string                  `json:"request_id"`
}

type SkillReviewDecisionRequest struct {
	Decision string `json:"decision"`
}

type SkillReviewDecisionResponse struct {
	Review    skill.ReviewerDecisionDTO `json:"review"`
	RequestID string                    `json:"request_id"`
}

// SkillReviewHandler 负责 Reviewer 队列、冻结详情和批准决定的严格 transport 契约。
type SkillReviewHandler struct {
	service    SkillReviewService
	requestIDs auth.IDGenerator
}

func NewSkillReviewHandler(service SkillReviewService, requestIDs auth.IDGenerator) (*SkillReviewHandler, error) {
	if service == nil || requestIDs == nil {
		return nil, errors.New("create skill review HTTP handler: required dependency is missing")
	}
	return &SkillReviewHandler{service: service, requestIDs: requestIDs}, nil
}

// Register 由 Bootstrap 显式提供 Auth read/write 中间件；write 必须包含 CSRF 校验。
func (h *SkillReviewHandler) Register(router gin.IRoutes, requireRead gin.HandlerFunc, requireWrite gin.HandlerFunc) {
	router.GET("/api/v1/admin/skill-reviews", requireRead, h.list)
	router.GET("/api/v1/admin/skill-reviews/:review_id", requireRead, h.detail)
	router.POST("/api/v1/admin/skill-reviews/:review_id/decisions", requireWrite, h.decide)
}

func (h *SkillReviewHandler) list(c *gin.Context) {
	requestID, principal, ok := h.requestContext(c)
	if !ok || !h.requireCapability(c, principal, requestID, "list") {
		return
	}
	query := c.Request.URL.Query()
	for key := range query {
		if key != "status" && key != "cursor" {
			h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
			return
		}
	}
	if len(query["status"]) != 1 || query.Get("status") != string(skill.ReviewStatusReviewing) || len(query["cursor"]) > 1 ||
		(len(query["cursor"]) == 1 && query.Get("cursor") == "") {
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	result, err := h.service.ListReviewQueue(c.Request.Context(), reviewerPrincipal(principal), query.Get("status"), query.Get("cursor"))
	if err != nil {
		h.writeMappedError(c, err, requestID, principal, "list")
		return
	}
	items := result.Items
	if items == nil {
		items = []skill.ReviewerQueueItemDTO{}
	}
	var nextCursor *string
	if result.NextCursor != "" {
		nextCursor = &result.NextCursor
	}
	h.writeBounded(c, http.StatusOK, SkillReviewQueueResponse{Items: items, NextCursor: nextCursor, RequestID: requestID}, "", requestID)
}

func (h *SkillReviewHandler) detail(c *gin.Context) {
	requestID, principal, ok := h.requestContext(c)
	if !ok || !h.requireCapability(c, principal, requestID, "detail") {
		return
	}
	if len(c.Request.URL.Query()) != 0 {
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	result, err := h.service.FindReviewDetail(c.Request.Context(), reviewerPrincipal(principal), c.Param("review_id"))
	if err != nil {
		h.writeMappedError(c, err, requestID, principal, "detail")
		return
	}
	h.writeBounded(c, http.StatusOK, SkillReviewDetailResponse{Review: result, RequestID: requestID}, result.ReviewETag, requestID)
}

func (h *SkillReviewHandler) decide(c *gin.Context) {
	requestID, principal, ok := h.requestContext(c)
	if !ok || !h.requireCapability(c, principal, requestID, "approve_and_publish") {
		return
	}
	if len(c.Request.URL.Query()) != 0 {
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	ifMatchValues := c.Request.Header.Values("If-Match")
	idempotencyValues := c.Request.Header.Values("Idempotency-Key")
	if len(ifMatchValues) != 1 || len(idempotencyValues) > 1 || skill.ValidateStrongReviewETag(ifMatchValues[0]) != nil {
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	if len(idempotencyValues) == 0 {
		h.writeError(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "幂等键无效", requestID, false)
		return
	}
	request, ok := h.decodeDecision(c, requestID)
	if !ok {
		return
	}
	result, err := h.service.ApproveAndPublish(c.Request.Context(), skill.ApproveAndPublishServiceCommand{
		Reviewer: reviewerPrincipal(principal), ReviewID: c.Param("review_id"), IdempotencyKey: idempotencyValues[0],
		Decision: request.Decision, IfMatch: ifMatchValues[0], RequestID: requestID,
	})
	if err != nil {
		h.writeMappedError(c, err, requestID, principal, "approve_and_publish")
		return
	}
	h.writeBounded(c, http.StatusOK, SkillReviewDecisionResponse{Review: result.Review, RequestID: requestID}, "", requestID)
}

func (h *SkillReviewHandler) decodeDecision(c *gin.Context, requestID string) (SkillReviewDecisionRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return SkillReviewDecisionRequest{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return SkillReviewDecisionRequest{}, false
	}
	duplicateKey, err := hasDuplicateTopLevelJSONKey(raw)
	if err != nil || duplicateKey {
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return SkillReviewDecisionRequest{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request SkillReviewDecisionRequest
	if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil || request.Decision != string(skill.ReviewStatusApproved) {
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return SkillReviewDecisionRequest{}, false
	}
	return request, true
}

// hasDuplicateTopLevelJSONKey 拒绝顶层重复字段，避免标准 JSON Decoder 的“最后字段获胜”语义改变审核决定。
func hasDuplicateTopLevelJSONKey(raw []byte) (bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return false, errors.New("decode skill review decision object")
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return false, errors.New("decode skill review decision key")
		}
		key, ok := keyToken.(string)
		if !ok {
			return false, errors.New("decode skill review decision key type")
		}
		if _, exists := seen[key]; exists {
			return true, nil
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return false, errors.New("decode skill review decision value")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || ensureJSONEOF(decoder) != nil {
		return false, errors.New("decode skill review decision closing token")
	}
	return false, nil
}

func (h *SkillReviewHandler) requestContext(c *gin.Context) (string, auth.Principal, bool) {
	requestID, err := h.requestIDs.New()
	if err != nil {
		h.writeError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", skillEmergencyRequestID, true)
		return "", auth.Principal{}, false
	}
	principal, ok := auth.PrincipalFromContext(c.Request.Context())
	if !ok {
		h.writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
		return "", auth.Principal{}, false
	}
	return requestID, principal, true
}

func (h *SkillReviewHandler) requireCapability(c *gin.Context, principal auth.Principal, requestID string, action string) bool {
	for _, capability := range principal.Capabilities {
		if capability == skill.ReviewCapability {
			return true
		}
	}
	h.auditCapabilityDenied(c, principal, requestID, action)
	h.writeError(c, http.StatusForbidden, "SKILL_REVIEW_CAPABILITY_REQUIRED", "当前账号没有 Skill 审核权限", requestID, false)
	return false
}

// auditCapabilityDenied 记录最小结构化授权拒绝事实，不写 Cookie、CSRF、幂等键、Definition 或底层错误。
func (h *SkillReviewHandler) auditCapabilityDenied(c *gin.Context, principal auth.Principal, requestID string, action string) {
	slog.WarnContext(c.Request.Context(), "Skill Reviewer 授权审计事件",
		"event_type", "security.authorization.v1",
		"route", safeAuditRoute(c),
		"action", action,
		"decision", "denied",
		"actor_id", principal.ID,
		"request_id", requestID,
		"error_code", "SKILL_REVIEW_CAPABILITY_REQUIRED",
	)
}

func reviewerPrincipal(principal auth.Principal) skill.ReviewerPrincipal {
	return skill.ReviewerPrincipal{UserID: principal.ID, Capabilities: append([]string(nil), principal.Capabilities...)}
}

func (h *SkillReviewHandler) writeBounded(c *gin.Context, status int, response any, etag string, requestID string) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(response); err != nil || body.Len() > skillReviewMaxResponseBytes {
		h.writeError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", requestID, true)
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

func (h *SkillReviewHandler) writeMappedError(c *gin.Context, err error, requestID string, principal auth.Principal, action string) {
	switch {
	case errors.Is(err, skill.ErrInvalidReviewRequest), errors.Is(err, skill.ErrInvalidCursor):
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
	case errors.Is(err, skill.ErrInvalidIdempotencyKey):
		h.writeError(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "幂等键无效", requestID, false)
	case errors.Is(err, skill.ErrReviewCapabilityRequired):
		h.auditCapabilityDenied(c, principal, requestID, action)
		h.writeError(c, http.StatusForbidden, "SKILL_REVIEW_CAPABILITY_REQUIRED", "当前账号没有 Skill 审核权限", requestID, false)
	case errors.Is(err, skill.ErrReviewNotFound):
		h.writeError(c, http.StatusNotFound, "SKILL_REVIEW_NOT_FOUND", "审核记录不存在", requestID, false)
	case errors.Is(err, skill.ErrReviewConflict):
		h.writeError(c, http.StatusConflict, "SKILL_REVIEW_CONFLICT", "审核状态已发生变化，请刷新后重试", requestID, false)
	case errors.Is(err, skill.ErrIdempotencyConflict):
		h.writeError(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同的请求", requestID, false)
	default:
		h.writeError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", requestID, true)
	}
}

func (h *SkillReviewHandler) writeError(c *gin.Context, status int, code string, message string, requestID string, retryable bool) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: requestID, Retryable: retryable, Details: ErrorDetails{},
	}})
}
