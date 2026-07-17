package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/agent/internal/config"
	"github.com/FigoGoo/Dora-Agent/agent/internal/httpidentity"
	"github.com/FigoGoo/Dora-Agent/agent/internal/workspace"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	errorCodeInvalidArgument              = "INVALID_ARGUMENT"
	errorCodeInvalidCursor                = "INVALID_CURSOR"
	errorCodeInternalIdentityInvalid      = "INTERNAL_IDENTITY_INVALID"
	errorCodeIdentityAssertionUnavailable = "IDENTITY_ASSERTION_UNAVAILABLE"
	errorCodeSessionNotFound              = "SESSION_NOT_FOUND"
	errorCodeStreamRateLimited            = "STREAM_RATE_LIMITED"
	errorCodePersistenceUnavailable       = "PERSISTENCE_UNAVAILABLE"
	errorCodeWorkspaceContentUnavailable  = "WORKSPACE_CONTENT_UNAVAILABLE"
)

// IdentityVerifier 是 Agent HTTP Handler 消费的最小 Business 身份断言校验接口。
type IdentityVerifier interface {
	// Verify 校验完整断言、一次性 Nonce 与当前白名单路由绑定。
	Verify(ctx context.Context, request httpidentity.Request) (httpidentity.Claims, error)
}

// WorkspaceService 是 Handler 消费的 Snapshot 与 EventLog 有界补读接口。
type WorkspaceService interface {
	// LoadSnapshot 返回完成授权解密的完整工作台 DTO。
	LoadSnapshot(ctx context.Context, identity workspace.Identity, requestID string) (workspace.Snapshot, error)
	// LoadEventBatch 返回 PostgreSQL 真源中 cursor 之后的有界连续事件。
	LoadEventBatch(ctx context.Context, identity workspace.Identity, cursor int64, limit int) (workspace.EventBatch, error)
}

// IDGenerator 为未通过身份断言的错误响应生成不依赖调用方输入的 UUIDv7 RequestID。
type IDGenerator interface {
	// New 生成规范 UUIDv7；失败时 Handler 使用固定空值且仍不信任断言内容。
	New() (string, error)
}

// TimeSource 为 SSE Heartbeat 与逐帧 Deadline 提供可测试时间源。
type TimeSource interface {
	// Now 返回当前时间。
	Now() time.Time
}

// WorkspaceHandler 实现冻结的 Workspace Snapshot 与 PostgreSQL poll-first EventLog SSE。
type WorkspaceHandler struct {
	verifier IdentityVerifier
	service  WorkspaceService
	limiter  *workspace.StreamLimiter
	config   config.SSEConfig
	ids      IDGenerator
	clock    TimeSource
}

// NewWorkspaceHandler 校验身份、用例、连接预算与时间依赖，缺失任一项时阻止 HTTP Transport 启动。
func NewWorkspaceHandler(
	verifier IdentityVerifier,
	service WorkspaceService,
	limiter *workspace.StreamLimiter,
	cfg config.SSEConfig,
	ids IDGenerator,
	clock TimeSource,
) (*WorkspaceHandler, error) {
	if verifier == nil || service == nil || limiter == nil || ids == nil || clock == nil || cfg.BatchSize <= 0 ||
		cfg.PollInterval <= 0 || cfg.HeartbeatInterval <= 0 || cfg.MaxConnectionDuration <= 0 ||
		cfg.FrameWriteTimeout <= 0 || cfg.MaxEventBytes <= 0 {
		return nil, fmt.Errorf("create Workspace HTTP handler: invalid dependency or config")
	}
	return &WorkspaceHandler{verifier: verifier, service: service, limiter: limiter, config: cfg, ids: ids, clock: clock}, nil
}

// Register 只注册冻结的两个 GET 路径，不提供通用 Agent 反向代理入口。
func (h *WorkspaceHandler) Register(router gin.IRoutes) {
	router.GET("/api/v1/agent/sessions/:session_id/workspace", h.getWorkspace)
	router.GET("/api/v1/agent/sessions/:session_id/events", h.getEvents)
}

// getWorkspace 严格绑定无 Query 的规范路径与 workspace.read Scope，并在断言 exp 前完成一致性读取。
func (h *WorkspaceHandler) getWorkspace(c *gin.Context) {
	sessionID, ok := canonicalUUIDv7(c.Param("session_id"))
	target := "/api/v1/agent/sessions/" + sessionID + "/workspace"
	if !ok || c.Request.URL.RawQuery != "" || c.Request.URL.EscapedPath() != target {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "Session 标识无效", h.newRequestID(), false)
		return
	}
	claims, ok := h.verify(c, target, httpidentity.ScopeWorkspaceRead, sessionID)
	if !ok {
		return
	}
	requestCtx, cancel := context.WithDeadline(c.Request.Context(), claims.ExpiresAt)
	defer cancel()
	snapshot, err := h.service.LoadSnapshot(requestCtx, identityFromClaims(claims), claims.RequestID)
	if err != nil {
		h.writeWorkspaceError(c, err, claims.RequestID)
		return
	}
	c.Header("Cache-Control", "no-store")
	// PureJSON 避免合法正文中的 HTML 字符被额外膨胀；浏览器展示仍由 React 文本节点负责转义。
	c.PureJSON(http.StatusOK, snapshot)
}

// getEvents 严格解析唯一规范 Cursor，在写 Header 前完成身份、并发预算和初始 PostgreSQL Probe。
func (h *WorkspaceHandler) getEvents(c *gin.Context) {
	sessionID, ok := canonicalUUIDv7(c.Param("session_id"))
	if !ok {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "Session 标识无效", h.newRequestID(), false)
		return
	}
	cursor, ok := canonicalCursor(c.Request.URL.RawQuery)
	if !ok || c.Request.Header.Get("Last-Event-ID") != "" {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidCursor, "事件游标无效", h.newRequestID(), false)
		return
	}
	target := "/api/v1/agent/sessions/" + sessionID + "/events?after_seq=" + strconv.FormatInt(cursor, 10)
	if c.Request.URL.EscapedPath() != "/api/v1/agent/sessions/"+sessionID+"/events" {
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidArgument, "Session 标识无效", h.newRequestID(), false)
		return
	}
	claims, verified := h.verify(c, target, httpidentity.ScopeEventsRead, sessionID)
	if !verified {
		return
	}
	release, acquired := h.limiter.Acquire(claims.PrincipalUserID, claims.AgentSessionID)
	if !acquired {
		h.writeError(c, http.StatusTooManyRequests, errorCodeStreamRateLimited, "事件流连接数已达上限", claims.RequestID, true)
		return
	}
	defer release()

	connectionDeadline := claims.ExpiresAt
	configuredDeadline := h.clock.Now().Add(h.config.MaxConnectionDuration)
	if configuredDeadline.Before(connectionDeadline) {
		connectionDeadline = configuredDeadline
	}
	streamCtx, cancel := context.WithDeadline(c.Request.Context(), connectionDeadline)
	defer cancel()
	identity := identityFromClaims(claims)
	batch, err := h.service.LoadEventBatch(streamCtx, identity, cursor, h.config.BatchSize)
	if err != nil && !isResetError(err) {
		h.writeWorkspaceError(c, err, claims.RequestID)
		return
	}

	writer, writeErr := newSSEWriter(c.Writer, h.config.FrameWriteTimeout, connectionDeadline, h.clock)
	if writeErr != nil {
		h.writeError(c, http.StatusInternalServerError, errorCodePersistenceUnavailable, "事件流暂时不可用", claims.RequestID, true)
		return
	}
	if writer.start() != nil {
		return
	}
	if err != nil {
		h.writeReset(writer, sessionID, resetReason(err), batch)
		return
	}

	current := cursor
	latest := batch.LatestSeq
	minimum := batch.MinAvailableSeq
	for {
		if writeErr := writeProjectedEvents(writer, &current, batch.Events); writeErr != nil {
			return
		}
		if current >= batch.LatestSeq {
			break
		}
		batch, err = h.service.LoadEventBatch(streamCtx, identity, current, h.config.BatchSize)
		if err != nil {
			if isResetError(err) {
				h.writeReset(writer, sessionID, resetReason(err), batch)
			}
			return
		}
		latest, minimum = batch.LatestSeq, batch.MinAvailableSeq
	}
	ready := workspace.StreamReady{
		SchemaVersion: workspace.StreamControlSchemaVersionV1, Event: "stream.ready", SessionID: sessionID,
		Cursor: current, MinAvailableSeq: minimum, LatestSeq: latest,
	}
	if writer.writeJSONEvent("", "stream.ready", ready) != nil {
		return
	}

	pollTicker := time.NewTicker(h.config.PollInterval)
	heartbeatTicker := time.NewTicker(h.config.HeartbeatInterval)
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()
	for {
		select {
		case <-streamCtx.Done():
			return
		case <-pollTicker.C:
			batch, err = h.service.LoadEventBatch(streamCtx, identity, current, h.config.BatchSize)
			if err != nil {
				if isResetError(err) {
					h.writeReset(writer, sessionID, resetReason(err), batch)
				}
				return
			}
			if writeErr := writeProjectedEvents(writer, &current, batch.Events); writeErr != nil {
				return
			}
		case <-heartbeatTicker.C:
			if writer.writeHeartbeat(h.clock.Now().UnixMilli()) != nil {
				return
			}
		}
	}
}

// verify 调用严格身份 Verifier，并把内部无效与 Redis 故障映射为稳定、互不混淆的安全错误。
func (h *WorkspaceHandler) verify(
	c *gin.Context,
	target string,
	scope string,
	sessionID string,
) (httpidentity.Claims, bool) {
	claims, err := h.verifier.Verify(c.Request.Context(), httpidentity.Request{
		Headers: c.Request.Header, Method: c.Request.Method, CanonicalTarget: target,
		Scope: scope, AgentSessionID: sessionID,
	})
	if err == nil {
		return claims, true
	}
	if errors.Is(err, httpidentity.ErrUnavailable) {
		h.writeError(c, http.StatusServiceUnavailable, errorCodeIdentityAssertionUnavailable,
			"内部身份校验暂时不可用", h.newRequestID(), true)
		return httpidentity.Claims{}, false
	}
	h.writeError(c, http.StatusUnauthorized, errorCodeInternalIdentityInvalid,
		"内部身份断言无效", h.newRequestID(), false)
	return httpidentity.Claims{}, false
}

// writeWorkspaceError 将领域失败收敛为冻结 HTTP 代码，不泄漏密钥、密文、SQL 或投影 Payload。
func (h *WorkspaceHandler) writeWorkspaceError(c *gin.Context, err error, requestID string) {
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		h.writeError(c, http.StatusNotFound, errorCodeSessionNotFound, "Session 不存在或不可访问", requestID, false)
	case errors.Is(err, workspace.ErrInvalidCursor):
		h.writeError(c, http.StatusBadRequest, errorCodeInvalidCursor, "事件游标无效", requestID, false)
	case errors.Is(err, workspace.ErrContentUnavailable):
		h.writeError(c, http.StatusServiceUnavailable, errorCodeWorkspaceContentUnavailable, "工作台内容暂时不可用", requestID, false)
	default:
		h.writeError(c, http.StatusServiceUnavailable, errorCodePersistenceUnavailable, "Agent 持久化暂时不可用", requestID, true)
	}
}

// ErrorDetails 是 Agent HTTP 稳定错误 Envelope 的空详情对象，避免输出 null。
type ErrorDetails struct{}

// ErrorBody 是 Agent HTTP 对外稳定错误 DTO。
type ErrorBody struct {
	// Code 是 BFF 分支使用的稳定英文代码。
	Code string `json:"code"`
	// Message 是不含内部细节的安全中文说明。
	Message string `json:"message"`
	// RequestID 是可信或本地生成的 UUIDv7。
	RequestID string `json:"request_id"`
	// Retryable 表示同语义技术重试是否可能成功。
	Retryable bool `json:"retryable"`
	// Details 固定为空对象，便于前端稳定解码。
	Details ErrorDetails `json:"details"`
}

// ErrorResponse 是 Agent HTTP 失败的统一 Envelope。
type ErrorResponse struct {
	// Error 保存稳定代码、安全说明和重试语义。
	Error ErrorBody `json:"error"`
}

// writeError 输出禁止缓存的稳定错误 Envelope。
func (h *WorkspaceHandler) writeError(
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

// newRequestID 生成不信任请求头内容的本地 UUIDv7；生成失败或结果非规范时使用稳定紧急 UUIDv7。
func (h *WorkspaceHandler) newRequestID() string {
	value, err := h.ids.New()
	if _, canonical := canonicalUUIDv7(value); err != nil || !canonical {
		// 紧急值仍是规范 UUIDv7，只在系统随机源不可用且断言尚未可信时使用，避免错误 Envelope 破坏 DTO 契约。
		return "019f0000-0000-7000-8000-000000000000"
	}
	return value
}

// identityFromClaims 显式收窄 HTTP Claims，Web Session 和 Scope 不进入 Repository。
func identityFromClaims(claims httpidentity.Claims) workspace.Identity {
	return workspace.Identity{
		UserID: claims.PrincipalUserID, ProjectID: claims.ProjectID, SessionID: claims.AgentSessionID,
	}
}

// canonicalUUIDv7 只接受小写 RFC 9562 UUIDv7 唯一表示。
func canonicalUUIDv7(value string) (string, bool) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.Version() != 7 || parsed.String() != value {
		return "", false
	}
	return value, true
}

// canonicalCursor 只接受 after_seq=<唯一规范非负十进制 int64>，拒绝重复、转义、前导零和额外参数。
func canonicalCursor(rawQuery string) (int64, bool) {
	const prefix = "after_seq="
	if !strings.HasPrefix(rawQuery, prefix) || strings.Count(rawQuery, "after_seq=") != 1 {
		return 0, false
	}
	value := strings.TrimPrefix(rawQuery, prefix)
	if value == "" || strings.ContainsAny(value, "&;+") || (len(value) > 1 && value[0] == '0') {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 || parsed > int64(1<<53-1) || strconv.FormatInt(parsed, 10) != value {
		return 0, false
	}
	return parsed, true
}

// isResetError 判断已经通过身份的 SSE 是否应发送安全 Reset 控制帧后关闭。
func isResetError(err error) bool {
	return errors.Is(err, workspace.ErrCursorExpired) || errors.Is(err, workspace.ErrEventGap) ||
		errors.Is(err, workspace.ErrProjectionInvalid)
}

// resetReason 把内部投影失败收敛为冻结白名单，未知值默认 projection_invalid。
func resetReason(err error) string {
	switch {
	case errors.Is(err, workspace.ErrCursorExpired):
		return "cursor_expired"
	case errors.Is(err, workspace.ErrEventGap):
		return "event_gap"
	default:
		return "projection_invalid"
	}
}

// writeReset 记录不含 Payload 的高优先级诊断，并发送无 id 的 Reset 后关闭连接。
func (h *WorkspaceHandler) writeReset(writer *sseWriter, sessionID, reason string, batch workspace.EventBatch) {
	slog.Error("Workspace EventLog 投影需要客户端 Reset", "session_id", sessionID, "reason", reason)
	reset := workspace.StreamReset{
		SchemaVersion: workspace.StreamControlSchemaVersionV1, Event: "stream.reset", SessionID: sessionID,
		Reason: reason, SnapshotRequired: true, MinAvailableSeq: batch.MinAvailableSeq, LatestSeq: batch.LatestSeq,
	}
	_ = writer.writeJSONEvent("", "stream.reset", reset)
}

// sseWriter 对每一帧单独设置 Write Deadline、完成写入与 Flush，并在帧间清除 Deadline。
type sseWriter struct {
	response     http.ResponseWriter
	control      *http.ResponseController
	timeout      time.Duration
	hardDeadline time.Time
	clock        TimeSource
}

// newSSEWriter 要求 ResponseWriter 支持 Flush；hardDeadline 把阻塞中的单帧写入也限制在断言/连接期限内。
func newSSEWriter(response http.ResponseWriter, timeout time.Duration, hardDeadline time.Time, clock TimeSource) (*sseWriter, error) {
	controllerWriter, err := unwrapResponseWriter(response)
	if err != nil {
		return nil, err
	}
	// 最底层必须显式暴露 FlushError；只实现无返回值 Flush 的包装层可能吞掉底层网络故障并错误推进 Cursor。
	if _, ok := controllerWriter.(interface{ FlushError() error }); !ok {
		return nil, fmt.Errorf("SSE response does not support error-aware flush")
	}
	controller := http.NewResponseController(controllerWriter)
	return &sseWriter{
		response: response, control: controller, timeout: timeout,
		hardDeadline: hardDeadline, clock: clock,
	}, nil
}

// unwrapResponseWriter 递归剥离 Gin 等协议包装层，只把 Deadline/Flush 控制器绑定到底层 net/http Writer。
// Header 与 Body 仍经原始包装 Writer 写出；nil、环或异常深度会失败关闭，避免退回吞错 Flush。
func unwrapResponseWriter(response http.ResponseWriter) (http.ResponseWriter, error) {
	if response == nil {
		return nil, fmt.Errorf("SSE response writer is required")
	}
	current := response
	for range 32 {
		unwrapper, ok := current.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return current, nil
		}
		next := unwrapper.Unwrap()
		if next == nil {
			return nil, fmt.Errorf("SSE response writer unwrap returned nil")
		}
		current = next
	}
	return nil, fmt.Errorf("SSE response writer unwrap depth exceeded")
}

// start 写入禁止代理缓冲的标准 SSE Header，并在任何 Event 帧前显式 Flush Header。
func (w *sseWriter) start() error {
	w.response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.response.Header().Set("Cache-Control", "no-cache, no-transform")
	w.response.Header().Set("Connection", "keep-alive")
	w.response.Header().Set("X-Accel-Buffering", "no")
	return w.withFrameDeadline(func() error {
		w.response.WriteHeader(http.StatusOK)
		// Gin 的 WriteHeader 只缓存状态码；底层 ResponseController Flush 会绕过包装层提交 Header。
		// 先同步包装层的 committed 状态，避免后续帧再次向 net/http 写同一个 Header。
		if committer, ok := w.response.(interface{ WriteHeaderNow() }); ok {
			committer.WriteHeaderNow()
		}
		return w.control.Flush()
	})
}

// writeEvent 写入 id==seq、event 名和已冻结 JSON data，并只在 Flush 成功后返回 nil。
func (w *sseWriter) writeEvent(projected workspace.ProjectedEvent) error {
	return w.writeFrame(strconv.FormatInt(projected.Seq, 10), projected.Event, projected.Data)
}

// writeProjectedEvents 串行写出连续持久事件，并只在单帧 Write+Flush 完整成功后推进连接内 Cursor。
// Flush 失败时调用方保留最后已确认 Cursor，由重连从 PostgreSQL 真源重新补读该事件。
func writeProjectedEvents(writer *sseWriter, cursor *int64, events []workspace.ProjectedEvent) error {
	if writer == nil || cursor == nil {
		return fmt.Errorf("write projected events: writer and cursor are required")
	}
	for _, projected := range events {
		if projected.Seq != *cursor+1 {
			return workspace.ErrEventGap
		}
		if err := writer.writeEvent(projected); err != nil {
			return err
		}
		// 只有底层 error-aware Flush 成功才确认当前事件，避免代理缓冲失败后丢失重放起点。
		*cursor = projected.Seq
	}
	return nil
}

// writeJSONEvent 编码无敏感内部字段的控制 DTO，并写入不推进 Cursor 的无 id 帧。
func (w *sseWriter) writeJSONEvent(id, eventName string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return w.writeFrame(id, eventName, data)
}

// writeHeartbeat 写入 SSE Comment；Heartbeat 不包含 id 且不推进持久 Cursor。
func (w *sseWriter) writeHeartbeat(unixMillis int64) error {
	return w.writeRaw(": heartbeat " + strconv.FormatInt(unixMillis, 10) + "\n\n")
}

// writeFrame 组装单行 SSE 帧；事件名来自服务端白名单，JSON 换行已由 Encoder 转义。
func (w *sseWriter) writeFrame(id, eventName string, data []byte) error {
	if strings.ContainsAny(eventName, "\r\n") || strings.ContainsAny(id, "\r\n") || bytesContainLineBreak(data) {
		return fmt.Errorf("invalid SSE frame")
	}
	var builder strings.Builder
	if id != "" {
		builder.WriteString("id: ")
		builder.WriteString(id)
		builder.WriteByte('\n')
	}
	builder.WriteString("event: ")
	builder.WriteString(eventName)
	builder.WriteByte('\n')
	builder.WriteString("data: ")
	builder.Write(data)
	builder.WriteString("\n\n")
	return w.writeRaw(builder.String())
}

// writeRaw 给单帧设置独立 Deadline，成功 Flush 后清除，避免普通 Server WriteTimeout 控制长连接生命周期。
func (w *sseWriter) writeRaw(frame string) error {
	return w.withFrameDeadline(func() error {
		if _, err := w.response.Write([]byte(frame)); err != nil {
			return err
		}
		return w.control.Flush()
	})
}

// withFrameDeadline 保证一旦成功设置逐帧 Deadline，所有 Write/Flush 返回路径都 best-effort 清空。
// 写入或 Flush 原始错误优先返回；只有帧操作成功时，清理失败才成为本次调用错误。
func (w *sseWriter) withFrameDeadline(action func() error) (resultErr error) {
	now := w.clock.Now()
	deadline := now.Add(w.timeout)
	if !w.hardDeadline.IsZero() {
		if !w.hardDeadline.After(now) {
			return context.DeadlineExceeded
		}
		if w.hardDeadline.Before(deadline) {
			deadline = w.hardDeadline
		}
	}
	if err := w.control.SetWriteDeadline(deadline); err != nil {
		return err
	}
	defer func() {
		clearErr := w.control.SetWriteDeadline(time.Time{})
		if resultErr == nil && clearErr != nil {
			resultErr = clearErr
		}
	}()
	return action()
}

// bytesContainLineBreak 确保 JSON data 保持单行，防止持久 Payload 注入额外 SSE 字段。
func bytesContainLineBreak(value []byte) bool {
	for _, character := range value {
		if character == '\r' || character == '\n' {
			return true
		}
	}
	return false
}
