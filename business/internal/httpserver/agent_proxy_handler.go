package httpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/agentidentity"
	"github.com/FigoGoo/Dora-Agent/business/internal/auth"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	// 16 MiB 覆盖冻结默认 100 条、每条最多 65,536 bytes 正文及 PureJSON 转义开销，同时保持代理内存有界。
	maximumWorkspaceResponseBytes = 16 << 20
	maximumUpstreamErrorBytes     = 64 << 10
	maximumSSEFrameBytes          = 128 << 10
	maximumJavaScriptSafeInteger  = uint64(1<<53 - 1)
)

// AgentSessionAccessService 定义 BFF 签发断言前所需的最小资源级授权边界。
type AgentSessionAccessService interface {
	// Resolve 按可信用户与路由 Session 返回 ready Binding 授权事实。
	Resolve(ctx context.Context, ownerUserID string, agentSessionID string) (project.AgentSessionAccess, error)
}

// AgentIdentitySigner 定义固定三 Header 用户级内部身份断言签发能力。
type AgentIdentitySigner interface {
	// Sign 为一次规范目标签发短期、带随机 Nonce 的身份断言。
	Sign(identity agentidentity.Identity) (agentidentity.Assertion, error)
}

// AgentHTTPClient 定义 BFF 执行单次无自动业务重试内部请求的最小 HTTP 边界。
type AgentHTTPClient interface {
	// Do 执行请求并完整传播 Context 取消；实现不得自动重放带一次性 Nonce 的请求。
	Do(request *http.Request) (*http.Response, error)
}

// AgentProxyHandler 负责两条固定 Agent GET 路由的同源认证、Cursor 规范化与安全代理。
type AgentProxyHandler struct {
	access            AgentSessionAccessService
	signer            AgentIdentitySigner
	requestIDs        auth.IDGenerator
	client            AgentHTTPClient
	baseURL           *url.URL
	requestTimeout    time.Duration
	frameWriteTimeout time.Duration
}

// NewAgentHTTPClient 创建禁用代理、压缩与 Redirect 的内部 Client。
// RequestTimeout 只限制建连和响应头；SSE 响应头成功后由浏览器取消与 Agent 断言期限管理流生命周期。
func NewAgentHTTPClient(cfg config.AgentHTTPConfig) (*http.Client, error) {
	if cfg.RequestTimeout <= 0 {
		return nil, fmt.Errorf("create Agent HTTP client: invalid request timeout")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableCompression = true
	// 每个断言 Nonce 只能消费一次；禁用连接复用和 HTTP/2 自动重放路径，避免 Transport 在已写出 GET 后复用同一断言。
	transport.DisableKeepAlives = true
	transport.ForceAttemptHTTP2 = false
	transport.ResponseHeaderTimeout = cfg.RequestTimeout
	transport.TLSHandshakeTimeout = cfg.RequestTimeout
	transport.DialContext = (&net.Dialer{Timeout: cfg.RequestTimeout, KeepAlive: 30 * time.Second}).DialContext
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			// 一次性身份断言绑定原 CanonicalTarget，Redirect 必须失败关闭且绝不把 Header 带到其他 Host。
			return errors.New("Agent HTTP redirect is forbidden")
		},
	}
	return client, nil
}

// NewAgentProxyHandler 校验授权服务、断言签发器、Client、Endpoint 与超时后创建固定 allowlist BFF。
func NewAgentProxyHandler(access AgentSessionAccessService, signer AgentIdentitySigner, requestIDs auth.IDGenerator, client AgentHTTPClient, cfg config.AgentHTTPConfig) (*AgentProxyHandler, error) {
	if access == nil || signer == nil || requestIDs == nil || client == nil || cfg.RequestTimeout <= 0 {
		return nil, fmt.Errorf("create Agent proxy HTTP handler: invalid dependency or config")
	}
	baseURL, err := url.Parse(cfg.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" ||
		(baseURL.Path != "" && baseURL.Path != "/") || (baseURL.Scheme != "http" && baseURL.Scheme != "https") {
		return nil, fmt.Errorf("create Agent proxy HTTP handler: invalid base URL")
	}
	baseURL.Path = ""
	frameWriteTimeout := min(cfg.RequestTimeout, 5*time.Second)
	return &AgentProxyHandler{
		access: access, signer: signer, requestIDs: requestIDs, client: client, baseURL: baseURL,
		requestTimeout: cfg.RequestTimeout, frameWriteTimeout: frameWriteTimeout,
	}, nil
}

// Register 仅注册 Workspace Snapshot 与 EventLog SSE 两条固定 GET 路由，并复用 Business Cookie Resolver。
func (handler *AgentProxyHandler) Register(router gin.IRoutes, requireSession gin.HandlerFunc) {
	router.GET("/api/v1/agent/sessions/:session_id/workspace", requireSession, handler.workspace)
	router.GET("/api/v1/agent/sessions/:session_id/events", requireSession, handler.events)
}

// workspace 严格拒绝 Query，重新构造内部 Snapshot 请求，并对 JSON 响应执行有界复制。
func (handler *AgentProxyHandler) workspace(c *gin.Context) {
	requestID, ok := handler.newAgentRequestID(c)
	if !ok {
		return
	}
	sessionID := c.Param("session_id")
	if !canonicalUUIDv7(sessionID) || c.Request.URL.RawQuery != "" {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Session 标识无效", requestID, false)
		return
	}
	target := "/api/v1/agent/sessions/" + sessionID + "/workspace"
	request, ok := handler.prepareUpstreamRequest(c, requestID, sessionID, target, agentidentity.ScopeWorkspaceRead, "application/json")
	if !ok {
		return
	}
	requestContext, cancel := context.WithTimeout(request.Context(), handler.requestTimeout)
	defer cancel()
	response, err := handler.client.Do(request.WithContext(requestContext))
	if err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "工作台依赖暂时不可用", requestID, true)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		handler.proxyUpstreamError(c, response, requestID)
		return
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "工作台依赖暂时不可用", requestID, true)
		return
	}
	body, err := readBoundedBody(response.Body, maximumWorkspaceResponseBytes)
	if err != nil || !json.Valid(body) {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "工作台依赖暂时不可用", requestID, true)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// events 规范化 Query 与 Last-Event-ID 的最大合法 Cursor，再以逐帧 Deadline 无缓冲代理 SSE。
func (handler *AgentProxyHandler) events(c *gin.Context) {
	requestID, ok := handler.newAgentRequestID(c)
	if !ok {
		return
	}
	sessionID := c.Param("session_id")
	if !canonicalUUIDv7(sessionID) {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Session 标识无效", requestID, false)
		return
	}
	cursor, err := parseProxyCursor(c.Request)
	if err != nil {
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_CURSOR", "事件游标无效", requestID, false)
		return
	}
	target := "/api/v1/agent/sessions/" + sessionID + "/events?after_seq=" + strconv.FormatUint(cursor, 10)
	request, ok := handler.prepareUpstreamRequest(c, requestID, sessionID, target, agentidentity.ScopeEventsRead, "text/event-stream")
	if !ok {
		return
	}
	response, err := handler.client.Do(request)
	if err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "事件流依赖暂时不可用", requestID, true)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		handler.proxyUpstreamError(c, response, requestID)
		return
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/event-stream" {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "事件流依赖暂时不可用", requestID, true)
		return
	}
	if _, ok := c.Writer.(http.Flusher); !ok {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "事件流依赖暂时不可用", requestID, true)
		return
	}
	// Gin ResponseWriter 自身只暴露无错误返回的 Flush；先解包到 net/http Writer，
	// 让 ResponseController 能调用底层 FlushError 并真实观察客户端断开或慢写失败。
	controller := http.NewResponseController(unwrapProxyResponseWriter(c.Writer))
	if err := withProxyWriteDeadline(controller, handler.frameWriteTimeout, func() error {
		c.Header("Content-Type", "text/event-stream; charset=utf-8")
		c.Header("Cache-Control", "no-cache, no-store")
		c.Header("X-Accel-Buffering", "no")
		c.Status(http.StatusOK)
		c.Writer.WriteHeaderNow()
		return controller.Flush()
	}); err != nil {
		// Deadline 设置失败发生在 Header 提交前，仍可返回公共依赖错误；Write/Flush/清零失败只能关闭已提交的流。
		if !c.Writer.Written() {
			handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "事件流依赖暂时不可用", requestID, true)
		}
		return
	}
	handler.copySSEFrames(c, controller, response.Body)
}

// prepareUpstreamRequest 读取私有 ResolvedSession、完成一次 owner JOIN、签发断言，并创建无浏览器 Header 的新请求。
func (handler *AgentProxyHandler) prepareUpstreamRequest(c *gin.Context, requestID string, sessionID string, target string, scope string, accept string) (*http.Request, bool) {
	resolved, ok := auth.ResolvedSessionFromContext(c.Request.Context())
	if !ok || resolved.Principal.ID == "" || resolved.WebSessionID == "" || resolved.WebSessionVersion < 1 {
		handler.writeAgentError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
		return nil, false
	}
	access, err := handler.access.Resolve(c.Request.Context(), resolved.Principal.ID, sessionID)
	if err != nil {
		if errors.Is(err, project.ErrAgentSessionNotFound) {
			handler.writeAgentError(c, http.StatusNotFound, "SESSION_NOT_FOUND", "Session 不存在或不可访问", requestID, false)
		} else {
			handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "工作台依赖暂时不可用", requestID, true)
		}
		return nil, false
	}
	assertion, err := handler.signer.Sign(agentidentity.Identity{
		RequestID: requestID, CanonicalTarget: target,
		PrincipalUserID: resolved.Principal.ID, WebSessionID: resolved.WebSessionID,
		WebSessionVersion: resolved.WebSessionVersion, ProjectID: access.ProjectID,
		AgentSessionID: access.AgentSessionID, Scope: scope,
	})
	if err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "工作台依赖暂时不可用", requestID, true)
		return nil, false
	}
	upstreamURL := handler.baseURL.String() + target
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "工作台依赖暂时不可用", requestID, true)
		return nil, false
	}
	// 新请求只包含固定 Accept 和三身份 Header；Cookie、Authorization、CSRF、原始 Query 与浏览器伪造断言均不会被复制。
	request.Header.Set("Accept", accept)
	request.Header.Set(agentidentity.HeaderAssertion, assertion.EncodedCanonical)
	request.Header.Set(agentidentity.HeaderKeyVersion, assertion.KeyVersion)
	request.Header.Set(agentidentity.HeaderSignature, assertion.Signature)
	return request, true
}

// proxyUpstreamError 只接受冻结公共错误白名单；内部认证错误、未知状态、未知代码和非法 Envelope 统一映射 503。
func (handler *AgentProxyHandler) proxyUpstreamError(c *gin.Context, response *http.Response, requestID string) {
	body, err := readBoundedBody(response.Body, maximumUpstreamErrorBytes)
	if err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "工作台依赖暂时不可用", requestID, true)
		return
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "工作台依赖暂时不可用", requestID, true)
		return
	}
	switch {
	case response.StatusCode == http.StatusBadRequest && envelope.Error.Code == "INVALID_ARGUMENT":
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_ARGUMENT", "Session 标识无效", requestID, false)
	case response.StatusCode == http.StatusBadRequest && envelope.Error.Code == "INVALID_CURSOR":
		handler.writeAgentError(c, http.StatusBadRequest, "INVALID_CURSOR", "事件游标无效", requestID, false)
	case response.StatusCode == http.StatusNotFound && envelope.Error.Code == "SESSION_NOT_FOUND":
		handler.writeAgentError(c, http.StatusNotFound, "SESSION_NOT_FOUND", "Session 不存在或不可访问", requestID, false)
	case response.StatusCode == http.StatusTooManyRequests && envelope.Error.Code == "STREAM_RATE_LIMITED":
		handler.writeAgentError(c, http.StatusTooManyRequests, "STREAM_RATE_LIMITED", "事件流连接过多，请稍后重试", requestID, true)
	case response.StatusCode == http.StatusServiceUnavailable && envelope.Error.Code == "PERSISTENCE_UNAVAILABLE":
		handler.writeAgentError(c, http.StatusServiceUnavailable, "PERSISTENCE_UNAVAILABLE", "工作台存储暂时不可用", requestID, true)
	case response.StatusCode == http.StatusServiceUnavailable && envelope.Error.Code == "WORKSPACE_CONTENT_UNAVAILABLE":
		handler.writeAgentError(c, http.StatusServiceUnavailable, "WORKSPACE_CONTENT_UNAVAILABLE", "工作台内容暂时不可用", requestID, true)
	default:
		// Agent 401 INTERNAL_IDENTITY_INVALID 与 503 IDENTITY_ASSERTION_UNAVAILABLE 都是内部依赖失败，绝不清除浏览器登录态。
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "工作台依赖暂时不可用", requestID, true)
	}
}

// copySSEFrames 按空行识别完整 SSE Frame，逐帧设置 Write Deadline、写出并 Flush；慢写、取消或上游异常直接关闭。
func (handler *AgentProxyHandler) copySSEFrames(c *gin.Context, controller *http.ResponseController, upstream io.Reader) {
	reader := bufio.NewReaderSize(upstream, maximumSSEFrameBytes)
	var frame bytes.Buffer
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			return
		}
		if len(line) > 0 {
			if frame.Len()+len(line) > maximumSSEFrameBytes {
				return
			}
			_, _ = frame.Write(line)
			if bytes.Equal(line, []byte("\n")) || bytes.Equal(line, []byte("\r\n")) {
				if frameErr := withProxyWriteDeadline(controller, handler.frameWriteTimeout, func() error {
					if _, writeErr := c.Writer.Write(frame.Bytes()); writeErr != nil {
						return writeErr
					}
					return controller.Flush()
				}); frameErr != nil {
					return
				}
				frame.Reset()
			}
		}
		if err != nil {
			return
		}
	}
}

// parseProxyCursor 严格解析唯一 after_seq 与 Last-Event-ID，并取两者合法值的较大者。
func parseProxyCursor(request *http.Request) (uint64, error) {
	var afterValues []string
	if request.URL.RawQuery != "" {
		// Cursor 只含十进制数字，因此要求原始 Query 也恰好是唯一 `after_seq=<canonical>`，拒绝重复、编码别名和未知参数。
		if strings.Count(request.URL.RawQuery, "&") != 0 || !strings.HasPrefix(request.URL.RawQuery, "after_seq=") {
			return 0, errors.New("unexpected or duplicate query parameter")
		}
		afterValues = []string{strings.TrimPrefix(request.URL.RawQuery, "after_seq=")}
	}
	headerValues := request.Header.Values("Last-Event-ID")
	if len(headerValues) > 1 {
		return 0, errors.New("duplicate Last-Event-ID")
	}
	var after, last uint64
	var err error
	if len(afterValues) == 1 {
		after, err = parseCanonicalCursor(afterValues[0])
		if err != nil {
			return 0, err
		}
	}
	if len(headerValues) == 1 {
		last, err = parseCanonicalCursor(headerValues[0])
		if err != nil {
			return 0, err
		}
	}
	return max(after, last), nil
}

// parseCanonicalCursor 只接受 JavaScript Safe Integer 范围内唯一规范非负十进制。
func parseCanonicalCursor(value string) (uint64, error) {
	if value == "0" {
		return 0, nil
	}
	if value == "" || value[0] < '1' || value[0] > '9' {
		return 0, errors.New("invalid cursor encoding")
	}
	for index := 1; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return 0, errors.New("invalid cursor encoding")
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed > maximumJavaScriptSafeInteger {
		return 0, errors.New("cursor out of range")
	}
	return parsed, nil
}

// readBoundedBody 完整读取有界响应；超界或读取失败不返回部分数据。
func readBoundedBody(reader io.Reader, maximum int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maximum {
		return nil, errors.New("upstream response exceeds limit")
	}
	return body, nil
}

// unwrapProxyResponseWriter 去除 Gin 等只暴露无错误 Flush 的包装层，保留底层 net/http FlushError 语义。
func unwrapProxyResponseWriter(writer http.ResponseWriter) http.ResponseWriter {
	for {
		unwrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return writer
		}
		unwrapped := unwrapper.Unwrap()
		if unwrapped == nil || unwrapped == writer {
			return writer
		}
		writer = unwrapped
	}
}

// setProxyWriteDeadline 覆盖普通 Server WriteTimeout；测试 Recorder 不支持该控制时允许继续，真实网络错误失败关闭。
func setProxyWriteDeadline(controller *http.ResponseController, timeout time.Duration) error {
	err := controller.SetWriteDeadline(time.Now().Add(timeout))
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

// clearProxyWriteDeadline 清除本帧连接级 Deadline；测试 Recorder 不支持时与设置操作保持相同兼容语义。
func clearProxyWriteDeadline(controller *http.ResponseController) error {
	err := controller.SetWriteDeadline(time.Time{})
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

// withProxyWriteDeadline 把 Deadline 严格限制在单次 Write/Flush 内；无论操作成功失败都立即尝试清零，且保留原始操作错误优先。
func withProxyWriteDeadline(controller *http.ResponseController, timeout time.Duration, writeAndFlush func() error) (err error) {
	err = setProxyWriteDeadline(controller, timeout)
	defer func() {
		clearErr := clearProxyWriteDeadline(controller)
		if err == nil {
			err = clearErr
		}
	}()
	if err != nil {
		return err
	}
	return writeAndFlush()
}

// canonicalUUIDv7 校验路由 ID 使用小写标准 UUIDv7，避免多编码路径签名。
func canonicalUUIDv7(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id.Version() == 7 && id.String() == value
}

// newAgentRequestID 为代理成功与失败生成同一个 UUIDv7；生成失败时返回最小安全 503。
func (handler *AgentProxyHandler) newAgentRequestID(c *gin.Context) (string, bool) {
	requestID, err := handler.requestIDs.New()
	if err != nil || !canonicalUUIDv7(requestID) {
		handler.writeAgentError(c, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "工作台依赖暂时不可用", projectEmergencyRequestID, true)
		return "", false
	}
	return requestID, true
}

// writeAgentError 输出 W0.5 公共错误 Envelope，禁止缓存身份、权限、Cursor 与依赖失败。
func (handler *AgentProxyHandler) writeAgentError(c *gin.Context, status int, code string, message string, requestID string, retryable bool) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: requestID, Retryable: retryable, Details: ErrorDetails{},
	}})
}
