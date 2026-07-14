package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/skill"
	"github.com/gin-gonic/gin"
)

const skillMarketMaxResponseBytes = 2 * skill.MaxCanonicalDefinitionBytes

// SkillMarketService 是匿名 HTTP Handler 消费的最小公开读取边界。
type SkillMarketService interface {
	ListPublished(context.Context, string) (skill.MarketListResult, error)
	FindPublishedByID(context.Context, string) (skill.MarketDetailDTO, error)
}

// SkillMarketListResponse 是 GET /api/v1/skill-market 的冻结 Envelope。
type SkillMarketListResponse struct {
	Items      []skill.MarketListItemDTO `json:"items"`
	NextCursor *string                   `json:"next_cursor"`
	RequestID  string                    `json:"request_id"`
}

// SkillMarketDetailResponse 是 GET /api/v1/skill-market/:skill_id 的冻结 Envelope。
type SkillMarketDetailResponse struct {
	Skill     skill.MarketDetailDTO `json:"skill"`
	RequestID string                `json:"request_id"`
}

// SkillMarketHandler 负责匿名 Market Query、路径、错误与有界公开 DTO 编码。
type SkillMarketHandler struct {
	service    SkillMarketService
	requestIDs auth.IDGenerator
}

// NewSkillMarketHandler 创建匿名 Market Handler。
func NewSkillMarketHandler(service SkillMarketService, requestIDs auth.IDGenerator) (*SkillMarketHandler, error) {
	if service == nil || requestIDs == nil {
		return nil, errors.New("create skill market HTTP handler: required dependency is missing")
	}
	return &SkillMarketHandler{service: service, requestIDs: requestIDs}, nil
}

// Register 直接注册匿名公开路由，不接受 Session 或 CSRF Middleware。
func (handler *SkillMarketHandler) Register(router gin.IRoutes) {
	router.GET("/api/v1/skill-market", handler.list)
	router.GET("/api/v1/skill-market/:skill_id", handler.detail)
}

func (handler *SkillMarketHandler) list(c *gin.Context) {
	requestID, ok := handler.requestID(c)
	if !ok {
		return
	}
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	for key := range query {
		if key != "cursor" {
			handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
			return
		}
	}
	if len(query["cursor"]) > 1 || (len(query["cursor"]) == 1 && (query.Get("cursor") == "" || len(query.Get("cursor")) > 1024)) {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	result, err := handler.service.ListPublished(c.Request.Context(), query.Get("cursor"))
	if err != nil {
		handler.writeMappedError(c, err, requestID)
		return
	}
	items := result.Items
	if items == nil {
		items = []skill.MarketListItemDTO{}
	}
	var nextCursor *string
	if result.NextCursor != "" {
		nextCursor = &result.NextCursor
	}
	handler.writeBounded(c, http.StatusOK, SkillMarketListResponse{
		Items: items, NextCursor: nextCursor, RequestID: requestID,
	}, requestID)
}

func (handler *SkillMarketHandler) detail(c *gin.Context) {
	requestID, ok := handler.requestID(c)
	if !ok {
		return
	}
	skillID := c.Param("skill_id")
	if c.Request.URL.RawQuery != "" || !canonicalUUIDv7(skillID) {
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	detail, err := handler.service.FindPublishedByID(c.Request.Context(), skillID)
	if err != nil {
		handler.writeMappedError(c, err, requestID)
		return
	}
	handler.writeBounded(c, http.StatusOK, SkillMarketDetailResponse{Skill: detail, RequestID: requestID}, requestID)
}

func (handler *SkillMarketHandler) requestID(c *gin.Context) (string, bool) {
	requestID, err := handler.requestIDs.New()
	if err != nil || !canonicalUUIDv7(requestID) {
		handler.writeError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", skillEmergencyRequestID, true)
		return "", false
	}
	c.Header("Cache-Control", "no-store")
	return requestID, true
}

func (handler *SkillMarketHandler) writeMappedError(c *gin.Context, err error, requestID string) {
	switch {
	case errors.Is(err, skill.ErrInvalidMarketRequest), errors.Is(err, skill.ErrInvalidCursor):
		handler.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
	case errors.Is(err, skill.ErrMarketNotFound):
		handler.writeError(c, http.StatusNotFound, "SKILL_MARKET_NOT_FOUND", "Skill 暂不可用", requestID, false)
	default:
		handler.writeError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", requestID, true)
	}
}

func (handler *SkillMarketHandler) writeBounded(c *gin.Context, status int, response any, requestID string) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(response); err != nil || body.Len() > skillMarketMaxResponseBytes {
		handler.writeError(c, http.StatusServiceUnavailable, "SKILL_PERSISTENCE_UNAVAILABLE", "Skill 服务暂时不可用", requestID, true)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Status(status)
	_, _ = c.Writer.Write(body.Bytes())
}

func (handler *SkillMarketHandler) writeError(c *gin.Context, status int, code string, message string, requestID string, retryable bool) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: requestID, Retryable: retryable, Details: ErrorDetails{},
	}})
}
