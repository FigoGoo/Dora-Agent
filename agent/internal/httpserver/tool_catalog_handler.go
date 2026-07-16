package httpserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/tool"
	"github.com/gin-gonic/gin"
)

const (
	maxToolCatalogResponseBytes     = 16 * 1024
	errorCodeToolCatalogUnavailable = "TOOL_CATALOG_UNAVAILABLE"
)

// ToolCatalogHandler 实现 Session 级静态 Tool Definition Catalog 读取，不访问数据库、配置或 Graph。
type ToolCatalogHandler struct {
	verifier IdentityVerifier
	provider *tool.CatalogProvider
	ids      IDGenerator
}

// NewToolCatalogHandler 要求显式注入身份校验、静态 Provider 和本地错误 RequestID 生成器。
func NewToolCatalogHandler(
	verifier IdentityVerifier,
	provider *tool.CatalogProvider,
	ids IDGenerator,
) (*ToolCatalogHandler, error) {
	if verifier == nil || provider == nil || ids == nil {
		return nil, fmt.Errorf("create Tool Catalog HTTP handler: required dependency is nil")
	}
	return &ToolCatalogHandler{verifier: verifier, provider: provider, ids: ids}, nil
}

// Register 只注册 Session Tool Definition Catalog 的固定 GET 路径，不提供写入或运行入口。
func (h *ToolCatalogHandler) Register(router gin.IRoutes) {
	router.GET("/api/v1/agent/sessions/:session_id/tools", h.getCatalog)
}

// getCatalog 先验证规范 UUIDv7、无 Query 和精确 Escaped Path，再消耗一次性身份 Nonce。
func (h *ToolCatalogHandler) getCatalog(c *gin.Context) {
	sessionID, ok := canonicalUUIDv7(c.Param("session_id"))
	target := "/api/v1/agent/sessions/" + sessionID + "/tools"
	if !ok || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery || c.Request.URL.EscapedPath() != target {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "Session 标识无效", h.newRequestID(), false)
		return
	}
	claims, err := h.verifier.Verify(c.Request.Context(), httpidentity.Request{
		Headers: c.Request.Header, Method: c.Request.Method, CanonicalTarget: target,
		Scope: httpidentity.ScopeToolsRead, AgentSessionID: sessionID,
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
	response := tool.DefinitionCatalogResponse{
		SchemaVersion: tool.DefinitionCatalogSchemaVersionV1,
		RequestID:     claims.RequestID,
		Items:         h.provider.ListDefinitions(),
	}
	body, encodeErr := json.Marshal(response)
	if encodeErr != nil || len(body) > maxToolCatalogResponseBytes {
		// 编码上限是内部契约门禁；如果静态集合未来漂移，必须失败关闭而不是返回截断 JSON。
		h.writeError(c, http.StatusServiceUnavailable, errorCodeToolCatalogUnavailable,
			"Tool 目录暂时不可用", claims.RequestID, false)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// writeError 输出禁止缓存的稳定 Agent HTTP 错误 Envelope。
func (h *ToolCatalogHandler) writeError(
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

// newRequestID 为未通过身份校验的错误生成本地 UUIDv7，不信任请求头中的任何标识。
func (h *ToolCatalogHandler) newRequestID() string {
	value, err := h.ids.New()
	if _, canonical := canonicalUUIDv7(value); err != nil || !canonical {
		return "019f0000-0000-7000-8000-000000000000"
	}
	return value
}
