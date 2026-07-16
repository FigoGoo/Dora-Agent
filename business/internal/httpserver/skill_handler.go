package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/gin-gonic/gin"
)

// skillEmergencyRequestID 是 Request ID 生成器失效时保持错误 Envelope 的保留 UUIDv7。
const skillEmergencyRequestID = "019f0000-0000-7000-8000-000000000097"

// SkillOwnerService 定义 W1 Skill Owner HTTP Handler 消费的最小应用边界。
type SkillOwnerService interface {
	// Create 创建首个草稿并支持同键安全重放。
	Create(ctx context.Context, command skill.CreateCommand) (skill.CreateResult, error)
	// ListOwned 返回当前可信 Owner 的 keyset 分页投影。
	ListOwned(ctx context.Context, ownerUserID string, cursor string) (skill.OwnerListResult, error)
	// FindOwnedByID 返回一个 Owner-safe Skill 投影。
	FindOwnedByID(ctx context.Context, skillID string, ownerUserID string) (skill.OwnerSkillDTO, error)
	// UpdateDraft 使用 opaque If-Match 全量替换草稿。
	UpdateDraft(ctx context.Context, command skill.UpdateDraftCommand) (skill.OwnerSkillDTO, error)
	// SubmitReview 冻结当前草稿并提交审核。
	SubmitReview(ctx context.Context, command skill.SubmitReviewCommand) (skill.SubmitReviewServiceResult, error)
}

// SkillDefinitionRequest 是 Create 和 Draft Replace 唯一接受的 JSON DTO。
type SkillDefinitionRequest struct {
	// Definition 是完整 SkillDefinitionV1，禁止拆成部分 PATCH 或自由文本。
	Definition skill.SkillDefinitionV1 `json:"definition"`
}

// SubmitSkillReviewRequest 是提交审核可选接受的严格空 JSON 对象。
type SubmitSkillReviewRequest struct{}

// OwnerSkillResponse 是 Create、Detail 和 Draft Replace 的稳定 Envelope。
type OwnerSkillResponse struct {
	// Skill 是当前 Owner 安全投影。
	Skill skill.OwnerSkillDTO `json:"skill"`
	// RequestID 是本次 HTTP 请求关联 UUIDv7。
	RequestID string `json:"request_id"`
}

// OwnerSkillListResponse 是 GET /api/v1/skills?scope=mine 的稳定分页 Envelope。
type OwnerSkillListResponse struct {
	// Items 是当前页 Owner Skill 投影，空页编码为 []。
	Items []skill.OwnerSkillDTO `json:"items"`
	// NextCursor 是下一页 opaque cursor，无更多数据时为 null。
	NextCursor *string `json:"next_cursor"`
	// RequestID 是本次 HTTP 请求关联 UUIDv7。
	RequestID string `json:"request_id"`
}

// SubmitSkillReviewResponse 是提交审核首次接受和幂等重放共用的 Envelope。
type SubmitSkillReviewResponse struct {
	// Skill 是提交后的当前 Owner 投影。
	Skill skill.OwnerSkillDTO `json:"skill"`
	// ReviewID 是首次命令冻结的审核提交 UUIDv7。
	ReviewID string `json:"review_id"`
	// RequestID 是本次 HTTP 请求关联 UUIDv7。
	RequestID string `json:"request_id"`
}

// SkillHandler 负责 W1 Skill Owner 路由的严格 JSON、可信 Principal、CSRF 后写入和错误收敛。
type SkillHandler struct {
	service             SkillOwnerService
	requestIDs          auth.IDGenerator
	maxRequestBodyBytes int64
}

// NewSkillHandler 校验应用服务、Request ID Generator 和请求体上限后创建 Handler。
func NewSkillHandler(service SkillOwnerService, requestIDs auth.IDGenerator, maxRequestBodyBytes int64) (*SkillHandler, error) {
	if service == nil || requestIDs == nil || maxRequestBodyBytes <= 0 {
		return nil, fmt.Errorf("create skill HTTP handler: invalid dependency or config")
	}
	return &SkillHandler{service: service, requestIDs: requestIDs, maxRequestBodyBytes: maxRequestBodyBytes}, nil
}

// Register 使用 Auth Handler 提供的读写中间件注册冻结 W1 Owner API；不注册 Reviewer、治理或市场路由。
func (h *SkillHandler) Register(router gin.IRoutes, requireRead gin.HandlerFunc, requireWrite gin.HandlerFunc) {
	router.POST("/api/v1/skills", requireWrite, h.create)
	router.GET("/api/v1/skills", requireRead, h.listOwned)
	router.GET("/api/v1/skills/:skill_id", requireRead, h.findOwned)
	router.PUT("/api/v1/skills/:skill_id/draft", requireWrite, h.updateDraft)
	router.POST("/api/v1/skills/:skill_id/reviews", requireWrite, h.submitReview)
}

// create 严格解码完整 Definition，Owner 只取私有 Context，首次返回 201、同义重放返回 200。
func (h *SkillHandler) create(c *gin.Context) {
	requestID, principal, ok := h.requestContext(c)
	if !ok {
		return
	}
	request, ok := h.decodeDefinitionRequest(c, requestID)
	if !ok {
		return
	}
	result, err := h.service.Create(c.Request.Context(), skill.CreateCommand{
		OwnerUserID: principal.ID, IdempotencyKey: c.GetHeader("Idempotency-Key"), Definition: request.Definition,
	})
	if err != nil {
		h.writeMappedSkillError(c, err, requestID)
		return
	}
	status := http.StatusCreated
	if result.IdempotentReplay {
		status = http.StatusOK
	}
	h.writeOwnerSkill(c, status, result.Skill, requestID)
}

// listOwned 只接受 scope=mine 与可选 cursor，其他查询参数失败关闭。
func (h *SkillHandler) listOwned(c *gin.Context) {
	requestID, principal, ok := h.requestContext(c)
	if !ok {
		return
	}
	query := c.Request.URL.Query()
	for key := range query {
		if key != "scope" && key != "cursor" {
			h.writeSkillError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false, nil)
			return
		}
	}
	if query.Get("scope") != "mine" || len(query["scope"]) != 1 || len(query["cursor"]) > 1 {
		h.writeSkillError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false, nil)
		return
	}
	result, err := h.service.ListOwned(c.Request.Context(), principal.ID, query.Get("cursor"))
	if err != nil {
		h.writeMappedSkillError(c, err, requestID)
		return
	}
	items := result.Items
	if items == nil {
		items = []skill.OwnerSkillDTO{}
	}
	var nextCursor *string
	if result.NextCursor != "" {
		nextCursor = &result.NextCursor
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, OwnerSkillListResponse{Items: items, NextCursor: nextCursor, RequestID: requestID})
}

// findOwned 以可信 Principal 读取详情，跨 Owner 和不存在由应用层统一返回 SKILL_NOT_FOUND。
func (h *SkillHandler) findOwned(c *gin.Context) {
	requestID, principal, ok := h.requestContext(c)
	if !ok {
		return
	}
	result, err := h.service.FindOwnedByID(c.Request.Context(), c.Param("skill_id"), principal.ID)
	if err != nil {
		h.writeMappedSkillError(c, err, requestID)
		return
	}
	h.writeOwnerSkill(c, http.StatusOK, result, requestID)
}

// updateDraft 严格解码完整 Definition，并要求客户端原样返回最近一次 opaque draft_etag。
func (h *SkillHandler) updateDraft(c *gin.Context) {
	requestID, principal, ok := h.requestContext(c)
	if !ok {
		return
	}
	request, ok := h.decodeDefinitionRequest(c, requestID)
	if !ok {
		return
	}
	result, err := h.service.UpdateDraft(c.Request.Context(), skill.UpdateDraftCommand{
		OwnerUserID: principal.ID, SkillID: c.Param("skill_id"), IfMatch: c.GetHeader("If-Match"), Definition: request.Definition,
	})
	if err != nil {
		h.writeMappedSkillError(c, err, requestID)
		return
	}
	h.writeOwnerSkill(c, http.StatusOK, result, requestID)
}

// submitReview 接受空 Body 或严格空 JSON 对象，幂等冻结当前草稿且不允许客户端提交 revision、digest 或权限。
func (h *SkillHandler) submitReview(c *gin.Context) {
	requestID, principal, ok := h.requestContext(c)
	if !ok {
		return
	}
	if !h.decodeOptionalEmptyJSON(c, requestID) {
		return
	}
	result, err := h.service.SubmitReview(c.Request.Context(), skill.SubmitReviewCommand{
		OwnerUserID: principal.ID, SkillID: c.Param("skill_id"), IdempotencyKey: c.GetHeader("Idempotency-Key"),
		IfMatch: c.GetHeader("If-Match"),
	})
	if err != nil {
		h.writeMappedSkillError(c, err, requestID)
		return
	}
	status := http.StatusCreated
	if result.IdempotentReplay {
		status = http.StatusOK
	}
	c.Header("Cache-Control", "no-store")
	c.Header("ETag", result.Skill.DraftETag)
	c.JSON(status, SubmitSkillReviewResponse{Skill: result.Skill, ReviewID: result.ReviewID, RequestID: requestID})
}

// decodeDefinitionRequest 执行媒体类型、Body 上限、UTF-8、Surrogate、未知字段和尾随 JSON 的统一失败关闭。
func (h *SkillHandler) decodeDefinitionRequest(c *gin.Context, requestID string) (SkillDefinitionRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		h.writeSkillError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false, nil)
		return SkillDefinitionRequest{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxRequestBodyBytes)
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil || !utf8.Valid(rawBody) || !validJSONSurrogateEscapes(rawBody) {
		h.writeSkillError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false, nil)
		return SkillDefinitionRequest{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.DisallowUnknownFields()
	var request SkillDefinitionRequest
	if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
		h.writeSkillError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false, nil)
		return SkillDefinitionRequest{}, false
	}
	return request, true
}

// decodeOptionalEmptyJSON 禁止 Submit Body 注入 revision、审核状态或权限，同时兼容真正空 Body。
func (h *SkillHandler) decodeOptionalEmptyJSON(c *gin.Context, requestID string) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.writeSkillError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false, nil)
		return false
	}
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return true
	}
	trimmedBody := bytes.TrimSpace(rawBody)
	if len(trimmedBody) == 0 || trimmedBody[0] != '{' {
		h.writeSkillError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false, nil)
		return false
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" || !utf8.Valid(rawBody) || !validJSONSurrogateEscapes(rawBody) {
		h.writeSkillError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false, nil)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.DisallowUnknownFields()
	var request SubmitSkillReviewRequest
	if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
		h.writeSkillError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false, nil)
		return false
	}
	return true
}

// requestContext 生成响应 Request ID 并只读取认证中间件放入私有 Context 的可信 Principal。
func (h *SkillHandler) requestContext(c *gin.Context) (string, auth.Principal, bool) {
	requestID, err := h.requestIDs.New()
	if err != nil {
		h.writeSkillError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", skillEmergencyRequestID, true, nil)
		return "", auth.Principal{}, false
	}
	principal, ok := auth.PrincipalFromContext(c.Request.Context())
	if !ok {
		h.writeSkillError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false, nil)
		return "", auth.Principal{}, false
	}
	return requestID, principal, true
}

// writeOwnerSkill 输出不可缓存的 Owner Envelope，并把同一个 opaque ETag 同步放入响应头。
func (h *SkillHandler) writeOwnerSkill(c *gin.Context, status int, result skill.OwnerSkillDTO, requestID string) {
	c.Header("Cache-Control", "no-store")
	c.Header("ETag", result.DraftETag)
	c.JSON(status, OwnerSkillResponse{Skill: result, RequestID: requestID})
}

// writeMappedSkillError 将领域错误映射为冻结 HTTP 状态、稳定代码和可选字段错误。
func (h *SkillHandler) writeMappedSkillError(c *gin.Context, err error, requestID string) {
	var validationError *skill.ValidationError
	if errors.As(err, &validationError) {
		fieldErrors := make([]FieldErrorDetail, 0, len(validationError.FieldErrors))
		for _, item := range validationError.FieldErrors {
			field := item.Field
			if !strings.HasPrefix(field, "definition.") {
				field = "definition." + field
			}
			fieldErrors = append(fieldErrors, FieldErrorDetail{Field: field, Code: item.Code, Message: item.Message})
		}
		code := "SKILL_INVALID_DEFINITION"
		if errors.Is(err, skill.ErrToolReferenceUnavailable) {
			code = "SKILL_TOOL_REFERENCE_UNAVAILABLE"
		}
		h.writeSkillError(c, http.StatusBadRequest, code, "Skill 定义校验失败", requestID, false, fieldErrors)
		return
	}
	switch {
	case errors.Is(err, skill.ErrInvalidIdempotencyKey):
		h.writeSkillError(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "幂等键无效", requestID, false, nil)
	case errors.Is(err, skill.ErrInvalidCursor):
		h.writeSkillError(c, http.StatusBadRequest, "INVALID_REQUEST", "分页游标无效", requestID, false, nil)
	case errors.Is(err, skill.ErrInvalidDefinition):
		h.writeSkillError(c, http.StatusBadRequest, "SKILL_INVALID_DEFINITION", "Skill 请求校验失败", requestID, false, nil)
	case errors.Is(err, skill.ErrSkillNotFound):
		h.writeSkillError(c, http.StatusNotFound, "SKILL_NOT_FOUND", "Skill 不存在或不可访问", requestID, false, nil)
	case errors.Is(err, skill.ErrDraftConflict):
		h.writeSkillError(c, http.StatusConflict, "SKILL_DRAFT_CONFLICT", "草稿已发生变化，请刷新后重试", requestID, false, nil)
	case errors.Is(err, skill.ErrReviewConflict):
		h.writeSkillError(c, http.StatusConflict, "SKILL_REVIEW_CONFLICT", "当前审核状态不允许此操作", requestID, false, nil)
	case errors.Is(err, skill.ErrIdempotencyConflict):
		h.writeSkillError(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同的请求", requestID, false, nil)
	default:
		h.writeSkillError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", requestID, true, nil)
	}
}

// writeSkillError 输出统一安全 Envelope，禁止缓存资源级鉴权、内容校验和持久化失败。
func (h *SkillHandler) writeSkillError(c *gin.Context, status int, code string, message string, requestID string, retryable bool, fieldErrors []FieldErrorDetail) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: requestID, Retryable: retryable,
		Details: ErrorDetails{FieldErrors: fieldErrors},
	}})
}
