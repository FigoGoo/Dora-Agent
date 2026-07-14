package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/gin-gonic/gin"
)

// authEmergencyRequestID 是 Request ID 生成器失效时的保留 UUIDv7，仅用于保持统一错误 Envelope 格式并显式表示追踪已降级。
const authEmergencyRequestID = "019f0000-0000-7000-8000-000000000000"

// AuthService 定义 W0 Auth HTTP Handler 使用的最小登录、解析和退出边界。
type AuthService interface {
	// Login 校验邮箱密码并建立新 Web Session。
	Login(ctx context.Context, email string, password string) (auth.LoginResult, error)
	// Resolve 从不透明 Cookie 解析权威会话和可信 Principal。
	Resolve(ctx context.Context, cookieToken string) (auth.ResolvedSession, error)
	// Logout 校验会话绑定 CSRF 并幂等撤销会话。
	Logout(ctx context.Context, cookieToken string, csrfToken string) error
}

// LoginRequest 是 POST /api/v1/auth/session 唯一接受的 JSON DTO。
type LoginRequest struct {
	// Email 用户输入邮箱，由认证用例规范化且不进入普通日志。
	Email string `json:"email"`
	// Password 用户原始密码，只在当前请求内交给 Argon2id 校验。
	Password string `json:"password"`
}

// PrincipalResponse 是会话响应中不含凭据的用户投影。
type PrincipalResponse struct {
	// ID 用户 UUIDv7。
	ID string `json:"id"`
	// DisplayName 用户安全展示名。
	DisplayName string `json:"display_name"`
	// Email 用户脱敏邮箱。
	Email string `json:"email"`
	// AccountStatus 账户状态，W0 认证成功时固定为 active。
	AccountStatus string `json:"account_status"`
	// Roles W0 尚未实现完整 RBAC，因此返回空数组。
	Roles []string `json:"roles"`
	// Capabilities W0 尚未实现能力投影，因此返回空数组。
	Capabilities []string `json:"capabilities"`
}

// AuthSessionResponse 是 GET/POST 认证会话的 Frozen v1 响应 DTO。
type AuthSessionResponse struct {
	// Status 认证成功时固定为 authenticated。
	Status string `json:"status"`
	// Principal 不含密码、Cookie 或内部摘要的用户投影。
	Principal PrincipalResponse `json:"principal"`
	// CSRFToken 对当前会话绑定且仅供前端内存保留的 Token。
	CSRFToken string `json:"csrf_token"`
	// SessionExpiresAt 当前会话窗口的 UTC RFC3339 最早过期时间。
	SessionExpiresAt string `json:"session_expires_at"`
}

// ErrorDetails 是 W0 错误 Envelope 的稳定空详情对象，避免输出 null。
type ErrorDetails struct{}

// ErrorBody 是错误 Envelope 内层的稳定错误 DTO。
type ErrorBody struct {
	// Code 供前端分支的稳定英文代码。
	Code string `json:"code"`
	// Message 不包含 SQL、Cookie、密码、堆栈或内部地址的安全中文说明。
	Message string `json:"message"`
	// RequestID 本次 HTTP 请求的 UUIDv7 标识。
	RequestID string `json:"request_id"`
	// Retryable 说明技术重试是否可能成功。
	Retryable bool `json:"retryable"`
	// Details 是可选结构化详情，W0 Auth 固定返回空对象。
	Details ErrorDetails `json:"details"`
}

// ErrorResponse 是 Business HTTP 失败的统一 W0 Envelope。
type ErrorResponse struct {
	// Error 包含稳定代码、安全说明、Request ID 和重试语义。
	Error ErrorBody `json:"error"`
}

// AuthHandler 负责 W0 Auth HTTP 协议边界、Cookie 属性、错误映射和可信 Principal 中间件。
type AuthHandler struct {
	service    AuthService
	config     config.AuthConfig
	requestIDs auth.IDGenerator
}

// NewAuthHandler 校验依赖与 Cookie 配置并创建 Handler；安全配置应已由 Config.Load 首先验证。
func NewAuthHandler(service AuthService, cfg config.AuthConfig, requestIDs auth.IDGenerator) (*AuthHandler, error) {
	if service == nil || requestIDs == nil || cfg.CookieName == "" || cfg.MaxRequestBodyBytes <= 0 {
		return nil, fmt.Errorf("create auth HTTP handler: invalid dependency or config")
	}
	return &AuthHandler{service: service, config: cfg, requestIDs: requestIDs}, nil
}

// Register 在给定 Gin Router 注册 Frozen v1 GET/POST/DELETE 会话端点。
func (h *AuthHandler) Register(router gin.IRoutes) {
	router.GET("/api/v1/auth/session", h.getSession)
	router.POST("/api/v1/auth/session", h.login)
	router.DELETE("/api/v1/auth/session", h.logout)
}

// RequireSession 返回受保护路由使用的 Gin 中间件；只在 Resolver 成功后才将 Principal 写入私有 Context Key。
func (h *AuthHandler) RequireSession() gin.HandlerFunc {
	return h.requireSession(false)
}

// RequireSessionAndCSRF 返回状态变更路由使用的组合中间件；只解析一次会话，再常量时间校验同一会话的 X-CSRF-Token。
func (h *AuthHandler) RequireSessionAndCSRF() gin.HandlerFunc {
	return h.requireSession(true)
}

// requireSession 统一执行 Cookie Resolver、可选 CSRF 校验和私有 Principal Context 写入，避免受保护路由复制安全逻辑。
func (h *AuthHandler) requireSession(requireCSRF bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID, ok := h.newRequestID(c)
		if !ok {
			return
		}
		cookieToken, err := c.Cookie(h.config.CookieName)
		if err != nil {
			h.writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
			c.Abort()
			return
		}
		resolved, err := h.service.Resolve(c.Request.Context(), cookieToken)
		if err != nil {
			h.writeMappedAuthError(c, err, requestID)
			c.Abort()
			return
		}
		if requireCSRF && subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(resolved.CSRFToken)) != 1 {
			h.writeError(c, http.StatusForbidden, "CSRF_INVALID", "请求安全校验失败", requestID, false)
			c.Abort()
			return
		}
		// 只使用 auth 包私有 Key 写入已校验会话事实，后续 Handler 不得从 Header/Body 接受 user_id 或 web_session_id。
		requestContext := auth.ContextWithResolvedSession(c.Request.Context(), resolved)
		c.Request = c.Request.WithContext(auth.ContextWithPrincipal(requestContext, resolved.Principal))
		c.Next()
	}
}

// getSession 解析 HttpOnly Cookie 并返回严格 Frozen v1 会话 DTO；依赖不可用时不伪装成匿名。
func (h *AuthHandler) getSession(c *gin.Context) {
	requestID, ok := h.newRequestID(c)
	if !ok {
		return
	}
	cookieToken, err := c.Cookie(h.config.CookieName)
	if err != nil {
		h.writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
		return
	}
	resolved, err := h.service.Resolve(c.Request.Context(), cookieToken)
	if err != nil {
		h.writeMappedAuthError(c, err, requestID)
		return
	}
	h.writeSession(c, resolved.Principal, resolved.CSRFToken, resolved.SessionExpiresAt)
}

// login 严格解码单个有界 JSON 对象，拒绝未知字段和多余 JSON，成功后写入配置化 HttpOnly Cookie。
func (h *AuthHandler) login(c *gin.Context) {
	requestID, ok := h.newRequestID(c)
	if !ok {
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		h.audit(c, "auth.login", "denied", "", requestID, "INVALID_REQUEST")
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.config.MaxRequestBodyBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request LoginRequest
	if err := decoder.Decode(&request); err != nil {
		h.audit(c, "auth.login", "denied", "", requestID, "INVALID_REQUEST")
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		h.audit(c, "auth.login", "denied", "", requestID, "INVALID_REQUEST")
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	result, err := h.service.Login(c.Request.Context(), request.Email, request.Password)
	if err != nil {
		h.audit(c, "auth.login", "denied", "", requestID, authErrorCode(err))
		h.writeMappedAuthError(c, err, requestID)
		return
	}
	h.audit(c, "auth.login", "succeeded", result.Principal.ID, requestID, "")
	h.setSessionCookie(c, result.CookieToken, result.SessionExpiresAt)
	h.writeSession(c, result.Principal, result.CSRFToken, result.SessionExpiresAt)
}

// logout 在 Cookie 存在时校验 X-CSRF-Token 并撤销会话；缺少 Cookie、会话不存在和重复撤销均返回 204。
func (h *AuthHandler) logout(c *gin.Context) {
	requestID, ok := h.newRequestID(c)
	if !ok {
		return
	}
	cookieToken, cookieErr := c.Cookie(h.config.CookieName)
	if cookieErr != nil {
		h.audit(c, "auth.logout", "succeeded", "", requestID, "")
		h.clearSessionCookie(c)
		c.Status(http.StatusNoContent)
		return
	}
	if err := h.service.Logout(c.Request.Context(), cookieToken, c.GetHeader("X-CSRF-Token")); err != nil {
		h.audit(c, "auth.logout", "denied", "", requestID, authErrorCode(err))
		h.writeMappedAuthError(c, err, requestID)
		return
	}
	h.audit(c, "auth.logout", "succeeded", "", requestID, "")
	h.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

// ensureJSONEOF 确保主 JSON 对象之后只剩空白，防止追加的第二个 JSON 被静默忽略。
func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

// newRequestID 为错误 Envelope 生成 UUIDv7；生成器失败时返回最小安全 503 且不继续业务操作。
func (h *AuthHandler) newRequestID(c *gin.Context) (string, bool) {
	requestID, err := h.requestIDs.New()
	if err != nil {
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{Error: ErrorBody{
			Code: "AUTH_UNAVAILABLE", Message: "认证服务暂时不可用", RequestID: authEmergencyRequestID, Retryable: true, Details: ErrorDetails{},
		}})
		return "", false
	}
	return requestID, true
}

// writeSession 将领域 Principal 显式映射为前端 DTO，不返回 Entity、持久化 Model 或安全摘要。
func (h *AuthHandler) writeSession(c *gin.Context, principal auth.Principal, csrfToken string, expiresAt time.Time) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, AuthSessionResponse{
		Status: "authenticated",
		Principal: PrincipalResponse{
			ID: principal.ID, DisplayName: principal.DisplayName, Email: principal.Email, AccountStatus: principal.AccountStatus,
			Roles: nonNilStrings(principal.Roles), Capabilities: nonNilStrings(principal.Capabilities),
		},
		CSRFToken: csrfToken, SessionExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	})
}

// writeMappedAuthError 将稳定认证错误映射到 Frozen v1 HTTP 代码，未知原错一律收敛为可重试 503。
func (h *AuthHandler) writeMappedAuthError(c *gin.Context, err error, requestID string) {
	switch {
	case errors.Is(err, auth.ErrInvalidLoginInput):
		h.writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
	case errors.Is(err, auth.ErrInvalidCredentials):
		h.writeError(c, http.StatusUnauthorized, "AUTH_INVALID_CREDENTIALS", "邮箱或密码错误", requestID, false)
	case errors.Is(err, auth.ErrRateLimited):
		c.Header("Retry-After", fmt.Sprintf("%d", max(1, int(h.config.LoginRateLimitWindow.Seconds()))))
		h.writeError(c, http.StatusTooManyRequests, "AUTH_RATE_LIMITED", "登录尝试过于频繁，请稍后再试", requestID, true)
	case errors.Is(err, auth.ErrUnauthenticated):
		h.writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
	case errors.Is(err, auth.ErrInvalidCSRF):
		h.writeError(c, http.StatusForbidden, "CSRF_INVALID", "请求安全校验失败", requestID, false)
	default:
		h.writeError(c, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "认证服务暂时不可用", requestID, true)
	}
}

// audit 输出不包含邮箱、密码、Cookie、CSRF 或原始错误的结构化鉴权审计事件。
func (h *AuthHandler) audit(c *gin.Context, action string, decision string, actorID string, requestID string, errorCode string) {
	level := slog.LevelInfo
	if decision != "succeeded" {
		level = slog.LevelWarn
	}
	slog.Log(c.Request.Context(), level, "鉴权审计事件",
		"event_type", "security.auth.v1",
		"action", action,
		"decision", decision,
		"actor_id", actorID,
		"request_id", requestID,
		"trace_id", "",
		"error_code", errorCode,
	)
}

// authErrorCode 将内部认证错误收敛为可审计的稳定代码，不写入底层错误文本。
func authErrorCode(err error) string {
	switch {
	case errors.Is(err, auth.ErrInvalidLoginInput):
		return "INVALID_REQUEST"
	case errors.Is(err, auth.ErrInvalidCredentials):
		return "AUTH_INVALID_CREDENTIALS"
	case errors.Is(err, auth.ErrRateLimited):
		return "AUTH_RATE_LIMITED"
	case errors.Is(err, auth.ErrUnauthenticated):
		return "UNAUTHENTICATED"
	case errors.Is(err, auth.ErrInvalidCSRF):
		return "CSRF_INVALID"
	default:
		return "AUTH_UNAVAILABLE"
	}
}

// writeError 输出统一 W0 错误 Envelope 并禁止中间缓存认证失败。
func (h *AuthHandler) writeError(c *gin.Context, status int, code string, message string, requestID string, retryable bool) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: requestID, Retryable: retryable, Details: ErrorDetails{},
	}})
}

// setSessionCookie 使用版本化配置写入 HttpOnly、Secure/SameSite/Domain 受控且 Path=/ 的会话 Cookie。
func (h *AuthHandler) setSessionCookie(c *gin.Context, token string, expiresAt time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: h.config.CookieName, Value: token, Path: "/", Domain: h.config.CookieDomain,
		Expires: expiresAt.UTC(), MaxAge: int(h.config.SessionIdleTTL.Seconds()), HttpOnly: true,
		Secure: h.config.CookieSecure, SameSite: parseSameSite(h.config.CookieSameSite),
	})
}

// clearSessionCookie 使用与建立时一致的 Name/Domain/Path/Secure/SameSite 覆盖会话 Cookie 并立即过期。
func (h *AuthHandler) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: h.config.CookieName, Value: "", Path: "/", Domain: h.config.CookieDomain,
		Expires: time.Unix(1, 0).UTC(), MaxAge: -1, HttpOnly: true,
		Secure: h.config.CookieSecure, SameSite: parseSameSite(h.config.CookieSameSite),
	})
}

// parseSameSite 将已通过 Config.Validate 校验的字符串映射为 net/http Cookie 常量。
func parseSameSite(value string) http.SameSite {
	switch strings.ToLower(value) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

// nonNilStrings 复制字符串投影并确保空值编码为 [] 而不是 null。
func nonNilStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}
