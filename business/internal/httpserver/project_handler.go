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
	"github.com/FigoGoo/Dora-Agent/business/internal/project"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectcreation"
	"github.com/FigoGoo/Dora-Agent/business/internal/projectskillbinding"
	"github.com/gin-gonic/gin"
)

// projectEmergencyRequestID 是 Request ID 生成器失效时的保留 UUIDv7，只用于保持错误 Envelope 契约。
const projectEmergencyRequestID = "019f0000-0000-7000-8000-000000000098"

// ProjectService 定义 W0 Project HTTP Handler 消费的最小 QuickCreate 与 Bootstrap 应用边界。
type ProjectService interface {
	// QuickCreate 可靠接受一次带稳定幂等键的 Project 创建命令。
	QuickCreate(ctx context.Context, command project.QuickCreateCommand) (project.QuickCreateResult, error)
	// Bootstrap 读取可信用户拥有的 Project 与 Agent Session 初始化状态。
	Bootstrap(ctx context.Context, projectID string, ownerUserID string) (project.BootstrapResult, error)
}

// ProjectSkillBindingV2Service 是 HTTP Handler 消费的显式 QuickCreate v2 最小应用边界。
type ProjectSkillBindingV2Service interface {
	// QuickCreateV2 冻结初始 Skill 选择并可靠接受 Session Bootstrap v2。
	QuickCreateV2(ctx context.Context, command projectcreation.QuickCreateV2Command) (projectskillbinding.QuickCreateV2Result, error)
}

// QuickCreateRequest 是 POST /api/v1/projects:quick-create 唯一接受的 JSON DTO。
type QuickCreateRequest struct {
	// InitialPrompt 是可选首提示词；缺失或 null 与空工作台语义一致，服务端仍权威执行 Unicode 规范化。
	InitialPrompt *string `json:"initial_prompt"`
}

// QuickCreateRequestV2 是同一路径显式选择的严格 v2 variant；EnabledSkillIDs 必须存在且非 null。
type QuickCreateRequestV2 struct {
	SchemaVersion   string    `json:"schema_version"`
	InitialPrompt   *string   `json:"initial_prompt"`
	EnabledSkillIDs *[]string `json:"enabled_skill_ids"`
}

// QuickCreateResponse 是首次接受与同键重放共用的 Frozen v1 安全响应。
type QuickCreateResponse struct {
	// ProjectID Business 已可靠创建的 Project UUIDv7。
	ProjectID string `json:"project_id"`
	// SessionID Agent 已确认的 Session UUIDv7；provisioning 时为空。
	SessionID *string `json:"session_id"`
	// InputID 非空首提示词对应的 Agent Input UUIDv7；空 Prompt 或 provisioning 时为空。
	InputID *string `json:"input_id"`
	// CreationStatus 为 provisioning 或 ready。
	CreationStatus string `json:"creation_status"`
	// WorkspaceRef 是不承载授权的前端正式路由引用。
	WorkspaceRef string `json:"workspace_ref"`
	// RequestID 是本次 HTTP 请求的 UUIDv7，用于错误与冒烟证据关联。
	RequestID string `json:"request_id"`
}

// ProjectBootstrapResponse 是 GET Project Bootstrap 的 Business 权威安全读模型。
type ProjectBootstrapResponse struct {
	// ProjectID 当前用户拥有的 Project UUIDv7。
	ProjectID string `json:"project_id"`
	// Title Project 安全展示标题。
	Title string `json:"title"`
	// LifecycleStatus Project 生命周期状态。
	LifecycleStatus string `json:"lifecycle_status"`
	// RecentRunStatus Project 最近运行摘要。
	RecentRunStatus string `json:"recent_run_status"`
	// InitialPromptStatus 首提示词初始化状态。
	InitialPromptStatus string `json:"initial_prompt_status"`
	// CreationStatus 为 provisioning 或 ready；永久失败通过稳定 HTTP 错误返回。
	CreationStatus string `json:"creation_status"`
	// SessionID Agent 已确认的 Session；provisioning 时为空。
	SessionID *string `json:"session_id"`
	// InputID Agent 已确认的首 Input；空 Prompt 或 provisioning 时为空。
	InputID *string `json:"input_id"`
	// UpdatedAt Business 读模型最近变化的 UTC RFC3339 时间。
	UpdatedAt string `json:"updated_at"`
	// RequestID 是本次 HTTP 请求的 UUIDv7。
	RequestID string `json:"request_id"`
}

// ProjectHandler 负责 W0 QuickCreate/Bootstrap 的严格 DTO、可信 Principal、幂等 Header 和错误映射。
type ProjectHandler struct {
	service             ProjectService
	serviceV2           ProjectSkillBindingV2Service
	requestIDs          auth.IDGenerator
	maxRequestBodyBytes int64
}

// NewProjectHandler 校验应用服务、Request ID Generator 和请求体上限后创建 Project Handler。
func NewProjectHandler(service ProjectService, requestIDs auth.IDGenerator, maxRequestBodyBytes int64) (*ProjectHandler, error) {
	return newProjectHandler(service, nil, requestIDs, maxRequestBodyBytes)
}

// NewProjectHandlerWithV2 创建同时接受 Frozen v1 与显式 v2 variant 的 Project Handler。
func NewProjectHandlerWithV2(service ProjectService, serviceV2 ProjectSkillBindingV2Service, requestIDs auth.IDGenerator, maxRequestBodyBytes int64) (*ProjectHandler, error) {
	if serviceV2 == nil {
		return nil, fmt.Errorf("create Project HTTP handler: QuickCreate v2 service is required")
	}
	return newProjectHandler(service, serviceV2, requestIDs, maxRequestBodyBytes)
}

func newProjectHandler(service ProjectService, serviceV2 ProjectSkillBindingV2Service, requestIDs auth.IDGenerator, maxRequestBodyBytes int64) (*ProjectHandler, error) {
	if service == nil || requestIDs == nil || maxRequestBodyBytes <= 0 {
		return nil, fmt.Errorf("create project HTTP handler: invalid dependency or config")
	}
	return &ProjectHandler{service: service, serviceV2: serviceV2, requestIDs: requestIDs, maxRequestBodyBytes: maxRequestBodyBytes}, nil
}

// Register 使用读/写两种认证中间件注册 Frozen v1 路由；写中间件必须同时校验 Session 与 CSRF。
func (h *ProjectHandler) Register(router gin.IRoutes, requireRead gin.HandlerFunc, requireWrite gin.HandlerFunc) {
	router.POST("/api/v1/projects:quick-create", requireWrite, h.quickCreate)
	router.GET("/api/v1/projects/:project_id/bootstrap", requireRead, h.bootstrap)
}

// quickCreate 严格解码可选 Prompt，身份只读取私有 Context，首次返回 201、同语义重放返回 200。
func (h *ProjectHandler) quickCreate(c *gin.Context) {
	requestID, ok := h.newProjectRequestID(c)
	if !ok {
		return
	}
	principal, ok := auth.PrincipalFromContext(c.Request.Context())
	if !ok {
		h.writeProjectError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		h.writeProjectError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxRequestBodyBytes)
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil || !utf8.Valid(rawBody) || !validJSONSurrogateEscapes(rawBody) {
		h.writeProjectError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	requestV1, requestV2, err := decodeQuickCreateVariant(rawBody)
	if err != nil {
		h.writeProjectError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
		return
	}
	prompt := ""
	if requestV2 != nil {
		if requestV2.InitialPrompt != nil {
			prompt = *requestV2.InitialPrompt
		}
		if h.serviceV2 == nil {
			h.writeProjectError(c, http.StatusServiceUnavailable, "PROJECT_SKILL_SNAPSHOT_V2_UNAVAILABLE", "项目技能快照功能暂时不可用", requestID, true)
			return
		}
		result, createErr := h.serviceV2.QuickCreateV2(c.Request.Context(), projectcreation.QuickCreateV2Command{
			OwnerUserID: principal.ID, IdempotencyKey: c.GetHeader("Idempotency-Key"), InitialPrompt: prompt,
			EnabledSkillIDs: append([]string{}, (*requestV2.EnabledSkillIDs)...),
		})
		if createErr != nil {
			h.writeMappedProjectError(c, createErr, requestID)
			return
		}
		h.writeQuickCreateSuccess(c, result.ProjectID, result.IdempotentReplay, principal.ID, requestID)
		return
	}
	if requestV1.InitialPrompt != nil {
		prompt = *requestV1.InitialPrompt
	}
	result, err := h.service.QuickCreate(c.Request.Context(), project.QuickCreateCommand{
		OwnerUserID: principal.ID, IdempotencyKey: c.GetHeader("Idempotency-Key"), InitialPrompt: prompt,
	})
	if err != nil {
		h.writeMappedProjectError(c, err, requestID)
		return
	}

	h.writeQuickCreateSuccess(c, result.ProjectID, result.IdempotentReplay, principal.ID, requestID)
}

// decodeQuickCreateVariant 先只读取 schema_version presence，再按唯一版本 DTO 严格拒绝未知字段和 trailing JSON。
func decodeQuickCreateVariant(rawBody []byte) (QuickCreateRequest, *QuickCreateRequestV2, error) {
	var fields map[string]json.RawMessage
	probe := json.NewDecoder(bytes.NewReader(rawBody))
	if err := probe.Decode(&fields); err != nil || fields == nil || ensureJSONEOF(probe) != nil {
		return QuickCreateRequest{}, nil, errors.New("invalid QuickCreate JSON")
	}
	_, hasSchemaVersion := fields["schema_version"]
	decoder := json.NewDecoder(bytes.NewReader(rawBody))
	decoder.DisallowUnknownFields()
	if !hasSchemaVersion {
		var request QuickCreateRequest
		if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil {
			return QuickCreateRequest{}, nil, errors.New("invalid QuickCreate v1 JSON")
		}
		return request, nil, nil
	}
	var request QuickCreateRequestV2
	if err := decoder.Decode(&request); err != nil || ensureJSONEOF(decoder) != nil ||
		request.SchemaVersion != projectskillbinding.QuickCreateSchemaVersionV2 || request.EnabledSkillIDs == nil {
		return QuickCreateRequest{}, nil, errors.New("invalid QuickCreate v2 JSON")
	}
	return QuickCreateRequest{}, &request, nil
}

// writeQuickCreateSuccess 统一 v1/v2 首次接受和幂等重放响应；首次接受不等待 Agent RPC。
func (h *ProjectHandler) writeQuickCreateSuccess(c *gin.Context, projectID string, replay bool, ownerUserID string, requestID string) {
	response := QuickCreateResponse{
		ProjectID: projectID, CreationStatus: "provisioning",
		WorkspaceRef: "/projects/" + projectID + "/workspace", RequestID: requestID,
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
		// 重放可以安全读取跨服务最终一致投影并返回 ready；首次提交仍不等待 Agent RPC。
		bootstrap, bootstrapErr := h.service.Bootstrap(c.Request.Context(), projectID, ownerUserID)
		if bootstrapErr != nil {
			h.writeMappedProjectError(c, bootstrapErr, requestID)
			return
		}
		if bootstrap.CreationStatus() == "failed" {
			h.writeProjectError(c, http.StatusConflict, "PROJECT_PROVISIONING_FAILED", "工作台初始化失败", requestID, false)
			return
		}
		response.CreationStatus = bootstrap.CreationStatus()
		response.SessionID = bootstrap.AgentSessionID
		response.InputID = bootstrap.AgentInputID
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(status, response)
}

// validJSONSurrogateEscapes 在 encoding/json 替换非法 Unicode Surrogate 前失败关闭。
// 原始字节 UTF-8 已由调用方校验；此处只允许成对的 high/low `\uXXXX` 转义。
func validJSONSurrogateEscapes(raw []byte) bool {
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
			code, ok := parseJSONHexCodeUnit(raw, index+2)
			if !ok {
				return false
			}
			if code >= 0xD800 && code <= 0xDBFF {
				next := index + 6
				if next+6 > len(raw) || raw[next] != '\\' || raw[next+1] != 'u' {
					return false
				}
				low, lowOK := parseJSONHexCodeUnit(raw, next+2)
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

// parseJSONHexCodeUnit 解析恰好四位 JSON Unicode 十六进制码元。
func parseJSONHexCodeUnit(raw []byte, start int) (uint16, bool) {
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

// bootstrap 按 URL Project ID 与可信 Principal 做资源级读取；不存在和越权统一返回 404。
func (h *ProjectHandler) bootstrap(c *gin.Context) {
	requestID, ok := h.newProjectRequestID(c)
	if !ok {
		return
	}
	principal, ok := auth.PrincipalFromContext(c.Request.Context())
	if !ok {
		h.writeProjectError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "未认证或会话已失效", requestID, false)
		return
	}
	result, err := h.service.Bootstrap(c.Request.Context(), c.Param("project_id"), principal.ID)
	if err != nil {
		h.writeMappedProjectError(c, err, requestID)
		return
	}
	if result.CreationStatus() == "failed" {
		h.writeProjectError(c, http.StatusConflict, "PROJECT_PROVISIONING_FAILED", "工作台初始化失败", requestID, false)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, ProjectBootstrapResponse{
		ProjectID: result.ProjectID, Title: result.Title, LifecycleStatus: string(result.LifecycleStatus),
		RecentRunStatus: string(result.RecentRunStatus), InitialPromptStatus: string(result.InitialPromptStatus),
		CreationStatus: result.CreationStatus(), SessionID: result.AgentSessionID, InputID: result.AgentInputID,
		UpdatedAt: result.UpdatedAt.UTC().Format(time.RFC3339), RequestID: requestID,
	})
}

// newProjectRequestID 生成 Project HTTP 成功和失败共用的 UUIDv7 关联标识。
func (h *ProjectHandler) newProjectRequestID(c *gin.Context) (string, bool) {
	requestID, err := h.requestIDs.New()
	if err != nil {
		h.writeProjectError(c, http.StatusServiceUnavailable, "PROJECT_UNAVAILABLE", "项目服务暂时不可用", projectEmergencyRequestID, true)
		return "", false
	}
	return requestID, true
}

// writeMappedProjectError 将领域错误收敛为 Frozen v1 HTTP 状态，不暴露数据库、密钥、Prompt 或 Agent RPC 原文。
func (h *ProjectHandler) writeMappedProjectError(c *gin.Context, err error, requestID string) {
	switch {
	case errors.Is(err, project.ErrInvalidIdempotencyKey):
		h.writeProjectError(c, http.StatusBadRequest, "IDEMPOTENCY_KEY_INVALID", "幂等键无效", requestID, false)
	case errors.Is(err, project.ErrInvalidQuickCreate):
		h.writeProjectError(c, http.StatusBadRequest, "INVALID_REQUEST", "请求格式无效", requestID, false)
	case errors.Is(err, project.ErrIdempotencyConflict):
		h.writeProjectError(c, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同的创建请求", requestID, false)
	case errors.Is(err, project.ErrProjectNotFound):
		h.writeProjectError(c, http.StatusNotFound, "PROJECT_NOT_FOUND", "项目不存在或不可访问", requestID, false)
	case errors.Is(err, projectcreation.ErrV2Disabled):
		h.writeProjectError(c, http.StatusServiceUnavailable, "PROJECT_SKILL_SNAPSHOT_V2_UNAVAILABLE", "项目技能快照功能暂时不可用", requestID, true)
	case errors.Is(err, projectskillbinding.ErrInvalidBinding):
		h.writeProjectError(c, http.StatusBadRequest, "PROJECT_SKILL_BINDING_INVALID", "项目技能选择无效", requestID, false)
	case errors.Is(err, projectskillbinding.ErrSkillUnavailable), errors.Is(err, projectskillbinding.ErrGovernanceUnavailable), errors.Is(err, projectskillbinding.ErrPublicToolUnavailable):
		h.writeProjectError(c, http.StatusConflict, "PROJECT_SKILL_UNAVAILABLE", "所选技能不可用于项目", requestID, false)
	case errors.Is(err, projectskillbinding.ErrSnapshotInvalid):
		h.writeProjectError(c, http.StatusConflict, "PROJECT_SKILL_SNAPSHOT_INVALID", "项目技能快照无效", requestID, false)
	case errors.Is(err, projectskillbinding.ErrSnapshotLimitExceeded):
		h.writeProjectError(c, http.StatusRequestEntityTooLarge, "SNAPSHOT_LIMIT_EXCEEDED", "项目技能快照超过大小或数量上限", requestID, false)
	case errors.Is(err, projectskillbinding.ErrContentProtection):
		h.writeProjectError(c, http.StatusServiceUnavailable, "PROJECT_SKILL_SNAPSHOT_PROTECTION_UNAVAILABLE", "项目技能快照保护暂时不可用", requestID, true)
	default:
		h.writeProjectError(c, http.StatusServiceUnavailable, "PROJECT_UNAVAILABLE", "项目服务暂时不可用", requestID, true)
	}
}

// writeProjectError 输出统一错误 Envelope，并禁止缓存资源级鉴权和创建失败。
func (h *ProjectHandler) writeProjectError(c *gin.Context, status int, code string, message string, requestID string, retryable bool) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, ErrorResponse{Error: ErrorBody{
		Code: code, Message: message, RequestID: requestID, Retryable: retryable, Details: ErrorDetails{},
	}})
}
