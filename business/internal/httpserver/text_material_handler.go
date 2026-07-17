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
	"time"
	"unicode/utf8"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/textmaterial"
	"github.com/gin-gonic/gin"
)

// TextMaterialService 定义 Project 工作台文本素材 HTTP Handler 消费的最小应用边界。
type TextMaterialService interface {
	// Create 以 UUIDv7 Idempotency-Key 作为 asset_id 创建或重放完整文本素材。
	Create(ctx context.Context, command textmaterial.CreateCommand) (textmaterial.CreateResult, error)
	// ListOwned 返回可信用户在 Project 下最近创建的最多一百条完整文本素材。
	ListOwned(ctx context.Context, ownerUserID string, projectID string) ([]textmaterial.TextMaterial, error)
}

// TextMaterialCreateRequest 是 POST 文本素材接口唯一接受的严格 JSON DTO。
type TextMaterialCreateRequest struct {
	// Content 是必须为 NFC、1..2000 字符的完整文本正文。
	Content string `json:"content"`
}

// TextMaterialResponse 是创建与列表共用的安全文本素材 DTO。
type TextMaterialResponse struct {
	// AssetID 是可直接传给 analyze_materials 的 UUIDv7。
	AssetID string `json:"asset_id"`
	// AssetVersion 固定为 1，并进入 analyze_materials expected_assets。
	AssetVersion int64 `json:"asset_version"`
	// MediaType 固定为 text。
	MediaType string `json:"media_type"`
	// Status 固定为 ready。
	Status string `json:"status"`
	// Content 是当前 Project Owner 可读取和选择的完整正文。
	Content string `json:"content"`
	// CreatedAt 是素材创建 UTC RFC3339Nano 时间。
	CreatedAt string `json:"created_at"`
}

// TextMaterialCreateResponse 是首次创建和同义幂等重放共用的严格响应。
type TextMaterialCreateResponse struct {
	// Material 是首次持久化的原始不可变素材。
	Material TextMaterialResponse `json:"material"`
	// Replayed 表示同一 asset_id、Owner、Project 与正文已经存在。
	Replayed bool `json:"replayed"`
	// RequestID 是当前 HTTP 调用的 UUIDv7 关联标识。
	RequestID string `json:"request_id"`
}

// TextMaterialListResponse 是 GET 文本素材接口的固定上限列表响应。
type TextMaterialListResponse struct {
	// Items 按 created_at DESC、asset_id DESC 返回最多一百条完整文本素材。
	Items []TextMaterialResponse `json:"items"`
	// RequestID 是当前 HTTP 调用的 UUIDv7 关联标识。
	RequestID string `json:"request_id"`
}

// TextMaterialHandler 负责严格 JSON、可信 Principal、CSRF 路由和稳定错误映射。
type TextMaterialHandler struct {
	service             TextMaterialService
	requestIDs          auth.IDGenerator
	maxRequestBodyBytes int64
}

// NewTextMaterialHandler 校验应用服务、Request ID Generator 和请求体上限后创建 Handler。
func NewTextMaterialHandler(service TextMaterialService, requestIDs auth.IDGenerator, maxRequestBodyBytes int64) (*TextMaterialHandler, error) {
	if service == nil || requestIDs == nil || maxRequestBodyBytes <= 0 {
		return nil, fmt.Errorf("create text material HTTP handler: invalid dependency or config")
	}
	return &TextMaterialHandler{service: service, requestIDs: requestIDs, maxRequestBodyBytes: maxRequestBodyBytes}, nil
}

// Register 使用读 Session 保护列表、使用 Session+CSRF 保护创建。
func (handler *TextMaterialHandler) Register(router gin.IRoutes, requireRead gin.HandlerFunc, requireWrite gin.HandlerFunc) {
	router.GET("/api/v1/projects/:project_id/text-materials", requireRead, handler.list)
	router.POST("/api/v1/projects/:project_id/text-materials", requireWrite, handler.create)
}

// create 严格解码正文，并让 Idempotency-Key 直接成为不可变 asset_id。
func (handler *TextMaterialHandler) create(c *gin.Context) {
	requestID, ok := handler.newRequestID(c)
	if !ok {
		return
	}
	principal, ok := auth.PrincipalFromContext(c.Request.Context())
	if !ok {
		handler.writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
		return
	}
	request, ok := handler.decodeCreateRequest(c, requestID)
	if !ok {
		return
	}
	if !textmaterial.CanonicalUUIDv7(c.GetHeader("Idempotency-Key")) {
		handler.writeError(c, http.StatusBadRequest, "TEXT_MATERIAL_INVALID", "文本素材请求格式无效", requestID, false)
		return
	}
	result, err := handler.service.Create(c.Request.Context(), textmaterial.CreateCommand{
		OwnerUserID: principal.ID, ProjectID: c.Param("project_id"),
		IdempotencyKey: c.GetHeader("Idempotency-Key"), Content: request.Content,
	})
	if err != nil {
		handler.writeMappedError(c, err, requestID)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, TextMaterialCreateResponse{
		Material: textMaterialResponse(result.Material), Replayed: result.Replayed, RequestID: requestID,
	})
}

// list 从可信 Principal 冻结 Owner，并返回固定上限的完整文本素材集合。
func (handler *TextMaterialHandler) list(c *gin.Context) {
	requestID, ok := handler.newRequestID(c)
	if !ok {
		return
	}
	principal, ok := auth.PrincipalFromContext(c.Request.Context())
	if !ok {
		handler.writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
		return
	}
	// 本最小纵切固定返回最近一百条；拒绝查询参数，避免形成未设计的分页或过滤口径。
	if c.Request.URL.RawQuery != "" {
		handler.writeError(c, http.StatusBadRequest, "TEXT_MATERIAL_QUERY_INVALID", "文本素材查询参数无效", requestID, false)
		return
	}
	materials, err := handler.service.ListOwned(c.Request.Context(), principal.ID, c.Param("project_id"))
	if err != nil {
		handler.writeMappedError(c, err, requestID)
		return
	}
	items := make([]TextMaterialResponse, 0, len(materials))
	for _, material := range materials {
		items = append(items, textMaterialResponse(material))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, TextMaterialListResponse{Items: items, RequestID: requestID})
}

// decodeCreateRequest 执行有界读取、Unicode、重复字段、未知字段和 trailing JSON 校验。
func (handler *TextMaterialHandler) decodeCreateRequest(c *gin.Context, requestID string) (TextMaterialCreateRequest, bool) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		handler.writeError(c, http.StatusBadRequest, "TEXT_MATERIAL_INVALID", "文本素材请求格式无效", requestID, false)
		return TextMaterialCreateRequest{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, handler.maxRequestBodyBytes)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil || !utf8.Valid(raw) || !validJSONSurrogateEscapes(raw) {
		handler.writeError(c, http.StatusBadRequest, "TEXT_MATERIAL_INVALID", "文本素材请求格式无效", requestID, false)
		return TextMaterialCreateRequest{}, false
	}
	duplicate, err := hasDuplicateJSONKey(raw)
	if err != nil || duplicate {
		handler.writeError(c, http.StatusBadRequest, "TEXT_MATERIAL_INVALID", "文本素材请求格式无效", requestID, false)
		return TextMaterialCreateRequest{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request TextMaterialCreateRequest
	if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil || !textmaterial.ValidContent(request.Content) {
		handler.writeError(c, http.StatusBadRequest, "TEXT_MATERIAL_INVALID", "文本素材请求格式无效", requestID, false)
		return TextMaterialCreateRequest{}, false
	}
	return request, true
}

// newRequestID 生成创建、列表、成功和失败共用的 UUIDv7 关联标识。
func (handler *TextMaterialHandler) newRequestID(c *gin.Context) (string, bool) {
	requestID, err := handler.requestIDs.New()
	if err != nil {
		handler.writeError(c, http.StatusServiceUnavailable, "TEXT_MATERIAL_UNAVAILABLE", "文本素材服务暂时不可用", projectEmergencyRequestID, true)
		return "", false
	}
	return requestID, true
}

// writeMappedError 将领域错误收敛为稳定 HTTP 状态，不暴露 SQL、正文或资源存在性差异。
func (handler *TextMaterialHandler) writeMappedError(c *gin.Context, err error, requestID string) {
	switch {
	case errors.Is(err, textmaterial.ErrInvalidArgument):
		handler.writeError(c, http.StatusBadRequest, "TEXT_MATERIAL_INVALID", "文本素材请求格式无效", requestID, false)
	case errors.Is(err, textmaterial.ErrProjectNotFound):
		handler.writeError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在或不可访问", requestID, false)
	case errors.Is(err, textmaterial.ErrIdempotencyConflict):
		handler.writeError(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同的文本素材", requestID, false)
	default:
		handler.writeError(c, http.StatusServiceUnavailable, "TEXT_MATERIAL_UNAVAILABLE", "文本素材服务暂时不可用", requestID, true)
	}
}

// writeError 输出统一 no-store 错误 Envelope。
func (handler *TextMaterialHandler) writeError(c *gin.Context, status int, code string, message string, requestID string, retryable bool) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: requestID, Retryable: retryable, Details: ErrorDetails{},
	}})
}

// textMaterialResponse 显式映射领域对象，避免向前端暴露 Owner、Project 或 Evidence 内部字段。
func textMaterialResponse(material textmaterial.TextMaterial) TextMaterialResponse {
	return TextMaterialResponse{
		AssetID: material.AssetID, AssetVersion: material.AssetVersion, MediaType: "text", Status: "ready",
		Content: material.Content, CreatedAt: material.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}
