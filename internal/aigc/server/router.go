package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approval"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/approvalruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/billing"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/events"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/sessionruntime"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

var ErrNotFound = errors.New("not found")

// SessionStore 定义路由层访问会话和消息历史所需的最小存储接口。
type SessionStore interface {
	SaveSession(ctx context.Context, record session.SessionRecord) error
	GetSession(ctx context.Context, sessionID string) (session.SessionRecord, error)
	AppendMessage(ctx context.Context, record session.MessageRecord) (session.MessageRecord, error)
	ListMessages(ctx context.Context, sessionID string, window session.MessageWindow) ([]session.MessageRecord, error)
}

// AgentInvoker 封装 Agent 的普通调用和 interrupt resume 调用。
type AgentInvoker interface {
	Invoke(ctx context.Context, req AgentInvokeRequest) (<-chan AgentEvent, error)
	Resume(ctx context.Context, req AgentResumeRequest) (<-chan AgentEvent, error)
}

type ConfirmedSpecStore interface {
	GetConfirmedBySession(context.Context, string) (spec.FinalVideoSpec, error)
}

// AgentInvokeRequest 是一次新用户消息触发 Agent 运行的输入。
type AgentInvokeRequest struct {
	Messages      []*schema.Message
	CheckpointID  string
	SessionValues map[string]any
}

// AgentResumeRequest 是一次用户确认/表单提交后恢复 Agent 的输入。
type AgentResumeRequest struct {
	CheckpointID  string
	Targets       map[string]any
	SessionValues map[string]any
}

// SkillStore 定义 Skill 导入、查询和列表读取能力。
type SkillStore interface {
	Save(ctx context.Context, record skill.SkillRecord) error
	Get(ctx context.Context, skillID string) (skill.SkillRecord, error)
	ListEnabled(ctx context.Context) ([]skill.SkillRecord, error)
}

// StoryboardStore 定义故事板读取与版本化 patch 写入能力。
type StoryboardStore interface {
	Get(ctx context.Context, storyboardID string) (storyboard.Storyboard, error)
	GetLatestBySession(ctx context.Context, sessionID string) (storyboard.Storyboard, error)
	ApplyPatch(ctx context.Context, req storyboard.PatchRequest) (storyboard.Storyboard, storyboard.EventRecord, error)
}

// AssetStore 定义素材记录的写入、读取和会话内列表能力。
type AssetStore interface {
	Save(ctx context.Context, record asset.Asset) (asset.Asset, error)
	Get(ctx context.Context, assetID string) (asset.Asset, error)
	ListBySession(ctx context.Context, sessionID string) ([]asset.Asset, error)
}

// GenerationJobStore 定义会话生成任务的列表读取能力。
type GenerationJobStore interface {
	ListBySession(ctx context.Context, sessionID string) ([]generation.GenerationJob, error)
}

type SessionRuntime interface {
	Enqueue(context.Context, string, sessionruntime.SessionInput) (sessionruntime.EnqueueResult, error)
	EnsureSession(context.Context, string) (bool, error)
	Wake(string)
}

// CheckpointStore 定义 interrupt checkpoint 的映射和恢复状态更新能力。
type CheckpointStore interface {
	SaveCheckpointMapping(ctx context.Context, record session.CheckpointMapping) (session.CheckpointMapping, error)
	GetCheckpointMapping(ctx context.Context, sessionID string, interruptID string) (session.CheckpointMapping, error)
	MarkCheckpointResumed(ctx context.Context, id string) (session.CheckpointMapping, error)
}

type CheckpointTransitionStore interface {
	TransitionCheckpointMapping(ctx context.Context, id string, expectedStatus string, expectedEpoch int64, nextStatus string, decisionVersion int) (session.CheckpointMapping, error)
}

// AssetUploader 定义上传二进制素材到对象存储的能力。
type AssetUploader interface {
	Upload(ctx context.Context, input asset.UploadInput) (asset.UploadResult, error)
}

type GenerationPreflight func(context.Context, string, []generation.GenerationJob) (int64, error)

type CompensationFinalizer interface {
	ManualFinalize(context.Context, string, int64, string) (generation.GenerationJob, error)
}

// AgentEvent 是后端 Agent runner 推给 HTTP/SSE 层的统一事件。
type AgentEvent struct {
	Event             string
	SurfaceID         string
	DataModelKey      string
	Payload           any
	AssistantText     string
	Message           *schema.Message
	Err               error
	ProgressPublished bool
}

// Config 汇总 AIGC HTTP 路由依赖，方便测试替换存储、Agent 和事件 broker。
type Config struct {
	Store               SessionStore
	Skills              SkillStore
	Storyboards         StoryboardStore
	Assets              AssetStore
	GenerationJobs      GenerationJobStore
	GenerationWorkflow  generation.WorkflowStore
	GenerationCommands  *generation.CommandService
	GenerationPreflight GenerationPreflight
	Compensation        CompensationFinalizer
	AdminToken          string
	AssetUploader       AssetUploader
	LocalAssetDir       string
	Events              a2ui.EventBroker
	EventLog            events.Store
	EventRelay          *events.TailRelay
	Checkpoints         CheckpointStore
	Invoker             AgentInvoker
	DynamicStoryboards  storyboard.AggregateRepository
	StoryboardCommands  *storyboard.CommandService
	Approvals           approval.Store
	ApprovalRuntime     *approvalruntime.Service
	Runtime             SessionRuntime
	RuntimeStore        *sessionruntime.PostgresStore
	Billing             billing.Store
	Specs               ConfirmedSpecStore
	InitialPoints       int64
	SessionValues       func(session.SessionRecord) map[string]any
	MessageWindow       session.MessageWindow
	NewID               func() string
	Now                 func() time.Time
}

// NewRouter 构建 AIGC HTTP API，并为运行时依赖补齐默认 ID、时间和历史窗口。
func NewRouter(cfg Config) *gin.Engine {
	if cfg.NewID == nil {
		cfg.NewID = randomID
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MessageWindow.Limit == 0 {
		cfg.MessageWindow.Limit = 40
	}
	if cfg.InitialPoints <= 0 {
		cfg.InitialPoints = 10000
	}
	if cfg.EventLog != nil && cfg.EventRelay == nil {
		var notifications events.NotificationSource
		if durable, ok := cfg.Events.(*DurableEventBroker); ok {
			notifications = durable.Notifications
		}
		cfg.EventRelay, _ = events.NewTailRelay(events.TailRelayConfig{Store: cfg.EventLog, Notifications: notifications, PollInterval: time.Second, BatchSize: 100})
	}

	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	if localAssetDir := strings.TrimSpace(cfg.LocalAssetDir); localAssetDir != "" {
		router.StaticFS("/api/aigc/local-assets", gin.Dir(localAssetDir, false))
	}
	router.POST("/api/aigc/skills", cfg.createSkill)
	router.GET("/api/aigc/skills", cfg.listSkills)
	router.GET("/api/aigc/skills/:skill_id/plan", cfg.getSkillPlan)
	router.POST("/api/aigc/assets", cfg.uploadAsset)
	router.GET("/api/aigc/assets/:asset_id", cfg.getAsset)
	router.POST("/api/aigc/sessions", cfg.createSession)
	router.POST("/api/aigc/sessions/:session_id/skill", cfg.bindSkillToSession)
	router.GET("/api/aigc/sessions/:session_id/messages", cfg.listSessionMessages)
	router.GET("/api/aigc/sessions/:session_id/assets", cfg.listSessionAssets)
	router.GET("/api/aigc/sessions/:session_id/jobs", cfg.listSessionGenerationJobs)
	router.GET("/api/aigc/sessions/:session_id/events/stream", cfg.streamSessionEvents)
	router.GET("/api/aigc/sessions/:session_id/events", cfg.replaySessionEvents)
	router.GET("/api/aigc/sessions/:session_id/storyboard", cfg.getSessionStoryboard)
	router.PATCH("/api/aigc/sessions/:session_id/storyboards/:storyboard_id", cfg.patchStoryboard)
	router.POST("/api/aigc/sessions/:session_id/storyboards/:storyboard_id/assets/:asset_id/bind", cfg.bindAssetToStoryboard)
	router.PATCH("/api/aigc/sessions/:session_id/storyboards/:storyboard_id/targets/:target_id/prompt", cfg.updateTargetPrompt)
	router.POST("/api/aigc/sessions/:session_id/storyboards/:storyboard_id/targets/:target_id/regenerate", cfg.regenerateTarget)
	router.POST("/api/aigc/sessions/:session_id/storyboards/:storyboard_id/targets/:target_id/assets/:asset_id/bind", cfg.bindTargetAsset)
	router.POST("/api/aigc/sessions/:session_id/storyboards/:storyboard_id/candidate-approvals/decision", cfg.decideCandidateApprovalBatch)
	router.POST("/api/aigc/sessions/:session_id/approvals/:approval_id/decision", cfg.decideApproval)
	router.GET("/api/aigc/sessions/:session_id/generation-operations/:operation_id", cfg.getGenerationOperation)
	router.POST("/api/aigc/sessions/:session_id/generation-operations/:operation_id/control", cfg.controlGenerationOperation)
	router.POST("/api/aigc/admin/generation/jobs/:job_id/compensation/finalize", cfg.manualFinalizeCompensation)
	router.POST("/api/aigc/sessions/:session_id/messages", cfg.createMessage)
	router.POST("/api/aigc/sessions/:session_id/messages/resume", cfg.resumeAgent)

	return router
}

// createSessionRequest 是创建会话的请求体。
type createSessionRequest struct {
	UserID  string `json:"user_id"`
	SkillID string `json:"skill_id"`
	Title   string `json:"title"`
}

// createSkillRequest 是导入 Skill.md 内容的请求体。
type createSkillRequest struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Content string `json:"content"`
	Enabled *bool  `json:"enabled"`
}

// bindSkillToSessionRequest 是给已有会话绑定 Skill 的请求体。
type bindSkillToSessionRequest struct {
	SkillID string `json:"skill_id"`
}

// createSkillResponse 返回保存后的 Skill 记录和解析出的 Skill plan。
type createSkillResponse struct {
	Skill skill.SkillRecord `json:"skill"`
	Plan  skill.SkillPlan   `json:"plan"`
}

// listSkillsResponse 返回当前可用 Skill 列表。
type listSkillsResponse struct {
	Skills []skill.SkillRecord `json:"skills"`
}

// createSkill 导入 Skill.md，并把解析后的 plan 与原始内容一起保存。
func (cfg Config) createSkill(c *gin.Context) {
	if cfg.Skills == nil {
		writeJSONError(c, http.StatusInternalServerError, "skill store is not configured")
		return
	}

	var req createSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid skill request")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeJSONError(c, http.StatusBadRequest, "skill content is required")
		return
	}
	plan, err := skill.ParseSkill(content)
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, err.Error())
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	skillID := strings.TrimSpace(req.ID)
	if skillID == "" {
		skillID = cfg.NewID()
	}
	now := cfg.Now()
	record := skill.SkillRecord{
		ID:          skillID,
		Name:        plan.Name,
		Description: plan.Description,
		Version:     strings.TrimSpace(req.Version),
		Content:     content,
		Enabled:     enabled,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := cfg.Skills.Save(c.Request.Context(), record); err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	plan.SkillID = record.ID
	c.JSON(http.StatusCreated, createSkillResponse{
		Skill: record,
		Plan:  *plan,
	})
}

// listSkills 返回当前启用的 Skill 列表，供前端导入/选择入口使用。
func (cfg Config) listSkills(c *gin.Context) {
	if cfg.Skills == nil {
		writeJSONError(c, http.StatusInternalServerError, "skill store is not configured")
		return
	}
	records, err := cfg.Skills.ListEnabled(c.Request.Context())
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, listSkillsResponse{Skills: records})
}

// getSkillPlan 读取指定 Skill 并实时解析 plan，便于前端查看可执行流程。
func (cfg Config) getSkillPlan(c *gin.Context) {
	if cfg.Skills == nil {
		writeJSONError(c, http.StatusInternalServerError, "skill store is not configured")
		return
	}
	skillID := strings.TrimSpace(c.Param("skill_id"))
	if skillID == "" {
		writeJSONError(c, http.StatusBadRequest, "skill id is required")
		return
	}
	record, err := cfg.Skills.Get(c.Request.Context(), skillID)
	if err != nil {
		if errors.Is(err, skill.ErrSkillNotFound) {
			writeJSONError(c, http.StatusNotFound, "skill not found")
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if !record.Enabled {
		writeJSONError(c, http.StatusNotFound, "skill not found")
		return
	}
	plan, err := skill.ParseSkill(record.Content)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	plan.SkillID = record.ID
	c.JSON(http.StatusOK, plan)
}

// createSession 创建新的 AIGC 创作会话，是“新会话”按钮的后端入口。
func (cfg Config) createSession(c *gin.Context) {
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return
	}

	var req createSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid session request")
		return
	}

	now := cfg.Now()
	userID := strings.TrimSpace(req.UserID)
	if cfg.Billing != nil && userID != "" {
		if _, err := cfg.Billing.EnsureAccount(c.Request.Context(), userID, cfg.InitialPoints); err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	record := session.SessionRecord{
		ID:        cfg.NewID(),
		UserID:    userID,
		SkillID:   strings.TrimSpace(req.SkillID),
		Title:     strings.TrimSpace(req.Title),
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := cfg.Store.SaveSession(c.Request.Context(), record); err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}

	c.JSON(http.StatusCreated, record)
}

// bindSkillToSession 把用户选择的 Skill 绑定到已有会话，后续 Agent 可读取该上下文。
func (cfg Config) bindSkillToSession(c *gin.Context) {
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return
	}
	if cfg.Skills == nil {
		writeJSONError(c, http.StatusInternalServerError, "skill store is not configured")
		return
	}
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	var req bindSkillToSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid skill binding request")
		return
	}
	skillID := strings.TrimSpace(req.SkillID)
	if skillID == "" {
		writeJSONError(c, http.StatusBadRequest, "skill id is required")
		return
	}
	record, err := cfg.Store.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusNotFound, "session not found")
		return
	}
	skillRecord, err := cfg.Skills.Get(c.Request.Context(), skillID)
	if err != nil {
		if errors.Is(err, skill.ErrSkillNotFound) {
			writeJSONError(c, http.StatusNotFound, "skill not found")
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if !skillRecord.Enabled {
		writeJSONError(c, http.StatusNotFound, "skill not found")
		return
	}
	record.SkillID = skillRecord.ID
	record.UpdatedAt = cfg.Now()
	if err := cfg.Store.SaveSession(c.Request.Context(), record); err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, record)
}

// uploadAsset 接收用户上传文件，写入对象存储并保存素材元数据。
func (cfg Config) uploadAsset(c *gin.Context) {
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return
	}
	if cfg.Assets == nil {
		writeJSONError(c, http.StatusInternalServerError, "asset store is not configured")
		return
	}
	if cfg.AssetUploader == nil {
		writeJSONError(c, http.StatusInternalServerError, "asset uploader is not configured")
		return
	}

	sessionID := strings.TrimSpace(c.PostForm("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	sessionRecord, err := cfg.Store.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusNotFound, "session not found")
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, "file is required")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, "open uploaded file failed")
		return
	}
	defer file.Close()

	mimeType := strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
	content := io.Reader(file)
	if mimeType == "" || mimeType == "application/octet-stream" {
		head := make([]byte, 512)
		n, _ := file.Read(head)
		mimeType = http.DetectContentType(head[:n])
		content = io.MultiReader(bytes.NewReader(head[:n]), file)
	}

	kind := asset.NormalizeKind(c.PostForm("kind"))
	if kind == "" {
		kind = asset.KindFromMIME(mimeType)
	}
	if kind == "" {
		writeJSONError(c, http.StatusBadRequest, "asset kind is required")
		return
	}
	source := strings.TrimSpace(c.PostForm("source"))
	if source == "" {
		source = asset.SourceUpload
	}
	userID := strings.TrimSpace(sessionRecord.UserID)
	if supplied := strings.TrimSpace(c.PostForm("user_id")); supplied != "" && supplied != userID {
		writeJSONError(c, http.StatusForbidden, "asset user does not own the session")
		return
	}

	assetID := cfg.NewID()
	objectKey := asset.NewObjectKey(sessionID, assetID, fileHeader.Filename)
	metadata, uploadMetadata := assetMetadataFromForm(c)
	uploadResult, err := cfg.AssetUploader.Upload(c.Request.Context(), asset.UploadInput{
		ObjectKey:     objectKey,
		Content:       content,
		ContentLength: fileHeader.Size,
		MIMEType:      mimeType,
		Filename:      fileHeader.Filename,
		Metadata:      uploadMetadata,
	})
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if uploadResult.SizeBytes == 0 {
		uploadResult.SizeBytes = fileHeader.Size
	}

	now := cfg.Now()
	record := asset.Asset{
		ID:              assetID,
		SessionID:       sessionID,
		UserID:          userID,
		Kind:            kind,
		Source:          source,
		MIMEType:        mimeType,
		Filename:        fileHeader.Filename,
		SizeBytes:       uploadResult.SizeBytes,
		StorageProvider: uploadResult.Provider,
		Bucket:          uploadResult.Bucket,
		ObjectKey:       uploadResult.ObjectKey,
		URL:             uploadResult.URL,
		Metadata:        metadata,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	saved, err := cfg.Assets.Save(c.Request.Context(), record)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusCreated, saved)
}

// getAsset 按素材 ID 返回素材详情，供前端预览和故事板绑定使用。
func (cfg Config) getAsset(c *gin.Context) {
	if cfg.Assets == nil {
		writeJSONError(c, http.StatusInternalServerError, "asset store is not configured")
		return
	}
	assetID := strings.TrimSpace(c.Param("asset_id"))
	sessionID := strings.TrimSpace(c.Query("session_id"))
	if assetID == "" || sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "asset id and session_id are required")
		return
	}
	if !cfg.ensureSession(c, sessionID) {
		return
	}
	record, err := cfg.Assets.Get(c.Request.Context(), assetID)
	if err != nil {
		if errors.Is(err, asset.ErrNotFound) {
			writeJSONError(c, http.StatusNotFound, "asset not found")
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if record.SessionID != sessionID || (record.Availability != "" && record.Availability != asset.AvailabilityAvailable) {
		writeJSONError(c, http.StatusNotFound, "asset not found")
		return
	}
	c.JSON(http.StatusOK, record)
}

// assetMetadataFromForm 从上传表单中拆出业务元数据和对象存储 metadata。
func assetMetadataFromForm(c *gin.Context) (map[string]any, map[string]string) {
	keys := []string{"element_key", "shot_id", "audio_layer_id", "binding_target"}
	metadata := map[string]any{}
	uploadMetadata := map[string]string{}
	for _, key := range keys {
		value := strings.TrimSpace(c.PostForm(key))
		if value == "" {
			continue
		}
		metadata[key] = value
		uploadMetadata[key] = value
	}
	if len(metadata) == 0 {
		metadata = nil
	}
	if len(uploadMetadata) == 0 {
		uploadMetadata = nil
	}
	return metadata, uploadMetadata
}

// messageRequest 承载用户普通提示词；A2UI 表单提交前端会先渲染成 content。
type messageRequest struct {
	Content        string           `json:"content,omitempty"`
	IdempotencyKey string           `json:"idempotency_key,omitempty"`
	UISource       *messageUISource `json:"ui_source,omitempty"`
}

// messageUISource 标记普通用户消息来自哪个临时 UI，用于刷新历史时关闭已提交 A2UI 表单。
type messageUISource struct {
	Type   string `json:"type,omitempty"`
	CardID string `json:"card_id,omitempty"`
}

// patchStoryboardRequest 是前端直接修改故事板时提交的版本化 JSON Patch。
type patchStoryboardRequest struct {
	BaseVersion int                     `json:"base_version"`
	Source      string                  `json:"source"`
	ToolCallID  string                  `json:"tool_call_id"`
	Ops         []aigctools.JSONPatchOp `json:"ops"`
}

// bindAssetRequest 描述把素材挂到故事板目标对象上的请求。
type bindAssetRequest struct {
	BaseVersion int    `json:"base_version"`
	TargetType  string `json:"target_type"`
	TargetID    string `json:"target_id"`
	Field       string `json:"field,omitempty"`
	Source      string `json:"source,omitempty"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
}

type updateTargetPromptRequest struct {
	ExpectedVersion int    `json:"expected_version"`
	TargetRevision  int    `json:"target_revision"`
	PromptRevision  int    `json:"prompt_revision"`
	Purpose         string `json:"purpose"`
	Prompt          string `json:"prompt"`
	PromptRef       string `json:"prompt_ref,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

type regenerateTargetRequest struct {
	ExpectedVersion int    `json:"expected_version"`
	TargetRevision  int    `json:"target_revision"`
	AssetSlot       string `json:"asset_slot"`
	MediaKind       string `json:"media_kind,omitempty"`
	IdempotencyKey  string `json:"idempotency_key"`
}

type bindTargetAssetRequest struct {
	ExpectedVersion int    `json:"expected_version"`
	TargetRevision  int    `json:"target_revision"`
	PromptRevision  int    `json:"prompt_revision,omitempty"`
	GenerationEpoch int    `json:"generation_epoch,omitempty"`
	AssetSlot       string `json:"asset_slot"`
	BindingID       string `json:"binding_id,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

type approvalDecisionRequest struct {
	ExpectedDecisionVersion *int   `json:"expected_decision_version,omitempty"`
	IdempotencyKey          string `json:"idempotency_key,omitempty"`
	Decision                string `json:"decision"`
	Reason                  string `json:"reason,omitempty"`
}

type candidateApprovalBatchDecisionRequest struct {
	ExpectedStoryboardVersion int    `json:"expected_storyboard_version"`
	IdempotencyKey            string `json:"idempotency_key"`
	Decision                  string `json:"decision"`
	Reason                    string `json:"reason,omitempty"`
}

type generationControlRequest struct {
	Action         string `json:"action"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type manualCompensationRequest struct {
	RefundedPoints      int64  `json:"refunded_points"`
	RefundTransactionID string `json:"refund_transaction_id,omitempty"`
}

// patchStoryboardResponse 返回 patch 后的故事板和可广播的 A2UI patch payload。
type patchStoryboardResponse struct {
	Storyboard storyboard.Storyboard       `json:"storyboard"`
	Patch      a2ui.StoryboardPatchPayload `json:"patch"`
}

// resumeAgentRequest 是 Agent interrupt 被用户输入恢复时的请求体。
type resumeAgentRequest struct {
	CheckpointID string `json:"checkpoint_id"`
	InterruptID  string `json:"interrupt_id"`
	Content      string `json:"content"`
	Data         any    `json:"data"`
}

// messageResponse 是提交用户输入后的同步确认；实际 A2UI 输出只从 /events/stream 下发。
type messageResponse struct {
	RunID     string `json:"run_id"`
	MessageID string `json:"message_id,omitempty"`
	InputID   string `json:"input_id,omitempty"`
	Status    string `json:"status"`
}

// listSessionGenerationJobsResponse 返回会话内生成任务列表。
type listSessionGenerationJobsResponse struct {
	Jobs []generation.GenerationJob `json:"jobs"`
}

// publicGenerationJobs removes provider transport, semantic fencing and
// billing internals from user-facing recovery APIs. Generated assets become
// discoverable through the availability-filtered asset API after finalization.
func publicGenerationJobs(jobs []generation.GenerationJob) []generation.GenerationJob {
	visible := make([]generation.GenerationJob, 0, len(jobs))
	for _, job := range jobs {
		job.UserID = ""
		job.WorkflowRunID = ""
		job.StageRunID = ""
		job.ToolCallID = ""
		job.IdempotencyKey = ""
		job.BindingToken = generation.BindingToken{}
		job.DeliveryPolicy = generation.DeliveryPolicy{}
		job.StoryboardVersionAtDispatch = 0
		job.Payload = nil
		job.Result = nil
		job.ProviderTaskID = ""
		job.ProviderRequestID = ""
		job.ProviderUsageRecorded = false
		job.ProviderUsageReported = false
		job.ProviderActualPoints = 0
		job.ProviderCostBreakdown = nil
		job.SettlementQuoteRecorded = false
		job.SettlementPoints = 0
		job.SettlementBreakdown = nil
		job.LeaseOwner = ""
		job.LeaseUntil = nil
		job.BillingTransactionID = ""
		job.BillingIdempotencyKey = ""
		job.CompensationEventID = ""
		job.RefundTransactionID = ""
		job.BalanceAfter = nil
		job.ErrorMessage = ""
		if job.Status != generation.StatusSucceeded {
			job.ResultAssetIDs = nil
		}
		visible = append(visible, job)
	}
	return visible
}

func publicGenerationOperation(operation generation.GenerationOperation) generation.GenerationOperation {
	operation.UserID = ""
	operation.WorkflowRunID = ""
	operation.StageRunID = ""
	operation.ToolCallID = ""
	operation.IdempotencyKey = ""
	operation.RequestFingerprint = ""
	operation.ErrorMessage = ""
	operation.Result = publicGenerationOperationResult(operation.Result)
	return operation
}

func publicGenerationOperationResult(result map[string]any) map[string]any {
	if len(result) == 0 {
		return nil
	}
	visible := map[string]any{}
	for _, key := range []string{"status", "estimated_points", "cost", "assembly_revision_id", "recovery_of_operation_id"} {
		if value, exists := result[key]; exists {
			visible[key] = value
		}
	}
	if len(visible) == 0 {
		return nil
	}
	return visible
}

func publicGenerationBatch(batch generation.GenerationBatch) generation.GenerationBatch {
	batch.UserID = ""
	batch.WorkflowRunID = ""
	batch.StageRunID = ""
	batch.ToolCallID = ""
	batch.DeliveryPolicy = generation.DeliveryPolicy{}
	batch.ExpectedSpecVersion = 0
	batch.ExpectedStoryboardVersion = 0
	batch.ErrorMessage = ""
	return batch
}

func publicWorkflowAggregate(workflow generation.WorkflowAggregate) generation.WorkflowAggregate {
	workflow.Operation = publicGenerationOperation(workflow.Operation)
	workflow.Batch = publicGenerationBatch(workflow.Batch)
	workflow.Jobs = publicGenerationJobs(workflow.Jobs)
	return workflow
}

// listSessionMessagesResponse 返回会话历史消息列表。
type listSessionMessagesResponse struct {
	Messages []session.MessageRecord `json:"messages"`
}

// listSessionAssetsResponse 返回会话内素材列表。
type listSessionAssetsResponse struct {
	Assets []asset.Asset `json:"assets"`
}

// listSessionMessages 返回会话消息窗口，前端用于刷新历史对话。
func (cfg Config) listSessionMessages(c *gin.Context) {
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return
	}

	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	if !cfg.ensureSession(c, sessionID) {
		return
	}

	records, err := cfg.Store.ListMessages(c.Request.Context(), sessionID, cfg.MessageWindow)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, listSessionMessagesResponse{Messages: records})
}

// listSessionAssets 返回会话素材列表，前端用于构建素材映射和预览。
func (cfg Config) listSessionAssets(c *gin.Context) {
	if cfg.Assets == nil {
		writeJSONError(c, http.StatusInternalServerError, "asset store is not configured")
		return
	}

	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	if !cfg.ensureSession(c, sessionID) {
		return
	}

	records, err := cfg.Assets.ListBySession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	visible := make([]asset.Asset, 0, len(records))
	for _, record := range records {
		// Provider output is created as pending_billing. It becomes user-visible
		// only after finalization, charge and binding have all committed.
		if record.Availability == "" || record.Availability == asset.AvailabilityAvailable {
			visible = append(visible, record)
		}
	}
	c.JSON(http.StatusOK, listSessionAssetsResponse{Assets: visible})
}

// listSessionGenerationJobs 返回会话后台生成任务，前端用于恢复工具进度状态。
func (cfg Config) listSessionGenerationJobs(c *gin.Context) {
	if cfg.GenerationJobs == nil {
		writeJSONError(c, http.StatusInternalServerError, "generation job store is not configured")
		return
	}

	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	if !cfg.ensureSession(c, sessionID) {
		return
	}

	jobs, err := cfg.GenerationJobs.ListBySession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, listSessionGenerationJobsResponse{Jobs: publicGenerationJobs(jobs)})
}

// streamSessionEvents 订阅会话级 broker，把后台事件持续写成 SSE。
func (cfg Config) streamSessionEvents(c *gin.Context) {
	if cfg.Events == nil && cfg.EventRelay == nil {
		writeJSONError(c, http.StatusInternalServerError, "event broker is not configured")
		return
	}

	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	if !cfg.ensureSession(c, sessionID) {
		return
	}
	if cfg.EventRelay != nil && cfg.EventLog != nil {
		cfg.streamDurableSessionEvents(c, sessionID)
		return
	}

	events, unsubscribe := cfg.Events.Subscribe(c.Request.Context(), sessionID)
	defer unsubscribe()
	prepareSSE(c)
	if err := writeSSE(c, a2ui.SSEEvent{
		ID:        cfg.NewID(),
		SessionID: sessionID,
		Event:     a2ui.EventReady,
		Payload: gin.H{
			"status": "connected",
		},
		CreatedAt: cfg.Now(),
	}); err != nil {
		return
	}

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if strings.TrimSpace(event.ID) == "" {
				event.ID = cfg.NewID()
			}
			if strings.TrimSpace(event.SessionID) == "" {
				event.SessionID = sessionID
			}
			if event.CreatedAt.IsZero() {
				event.CreatedAt = cfg.Now()
			}
			if !isClientA2UIEvent(event.Event) {
				continue
			}
			if err := writeSSE(c, event); err != nil {
				return
			}
			if c.Query("once") == "1" {
				return
			}
		}
	}
}

var errSSEOnceComplete = errors.New("sse once complete")

func (cfg Config) streamDurableSessionEvents(c *gin.Context, sessionID string) {
	afterSeq := int64(0)
	if raw := strings.TrimSpace(c.Query("after_seq")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			writeJSONError(c, http.StatusBadRequest, "after_seq must be a non-negative integer")
			return
		}
		afterSeq = value
	}
	if lastID := strings.TrimSpace(c.GetHeader("Last-Event-ID")); lastID != "" {
		if value, err := strconv.ParseInt(lastID, 10, 64); err == nil {
			afterSeq = max(afterSeq, value)
		} else if event, err := cfg.EventLog.GetByEventID(c.Request.Context(), lastID); err == nil && event.SessionID == sessionID {
			afterSeq = max(afterSeq, event.Seq)
		}
	}
	prepareSSE(c)
	current, _ := cfg.EventLog.CurrentSeq(c.Request.Context(), sessionID)
	// ready is connection metadata, not a consumed log row. Giving it an SSE
	// id equal to current_seq would advance EventSource's Last-Event-ID before
	// older rows were replayed and could create a permanent replay gap.
	if err := writeSSE(c, a2ui.SSEEvent{SessionID: sessionID, Seq: current, Event: a2ui.EventReady, Payload: gin.H{"status": "connected", "after_seq": afterSeq, "current_seq": current}, CreatedAt: cfg.Now()}); err != nil {
		return
	}
	if c.Query("once") == "1" {
		rows, err := cfg.EventLog.Tail(c.Request.Context(), sessionID, events.TailOptions{AfterSeq: afterSeq, Limit: 100})
		if err != nil {
			return
		}
		for _, row := range rows {
			event := sessionEventAsSSE(row)
			if !isClientA2UIEvent(event.Event) {
				continue
			}
			_ = writeSSE(c, event)
			return
		}
		return
	}
	err := cfg.EventRelay.Relay(c.Request.Context(), sessionID, afterSeq, func(row events.SessionEvent) error {
		event := sessionEventAsSSE(row)
		if !isClientA2UIEvent(event.Event) {
			return nil
		}
		return writeSSE(c, event)
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, errSSEOnceComplete) {
		_ = writeSSE(c, a2ui.SSEEvent{ID: cfg.NewID(), SessionID: sessionID, Event: a2ui.EventError, Payload: gin.H{"message": err.Error()}, CreatedAt: cfg.Now()})
	}
}

func (cfg Config) replaySessionEvents(c *gin.Context) {
	if cfg.EventLog == nil {
		writeJSONError(c, http.StatusInternalServerError, "session event log is not configured")
		return
	}
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" || !cfg.ensureSession(c, sessionID) {
		return
	}
	afterSeq := int64(0)
	if raw := strings.TrimSpace(c.Query("after_seq")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			writeJSONError(c, http.StatusBadRequest, "after_seq must be a non-negative integer")
			return
		}
		afterSeq = value
	}
	limit := 100
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 || value > 1000 {
			writeJSONError(c, http.StatusBadRequest, "limit must be between 1 and 1000")
			return
		}
		limit = value
	}
	rows, err := cfg.EventLog.Tail(c.Request.Context(), sessionID, events.TailOptions{AfterSeq: afterSeq, Limit: limit})
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	result := make([]a2ui.SSEEvent, 0, len(rows))
	next := afterSeq
	for _, row := range rows {
		if row.Seq > next {
			next = row.Seq
		}
		event := sessionEventAsSSE(row)
		if isClientA2UIEvent(event.Event) {
			result = append(result, event)
		}
	}
	current, _ := cfg.EventLog.CurrentSeq(c.Request.Context(), sessionID)
	c.JSON(http.StatusOK, gin.H{"events": result, "after_seq": afterSeq, "next_seq": next, "current_seq": current})
}

// getSessionStoryboard 返回会话最新故事板，未创建时返回 204。
func (cfg Config) getSessionStoryboard(c *gin.Context) {
	if cfg.Storyboards == nil && cfg.DynamicStoryboards == nil {
		writeJSONError(c, http.StatusInternalServerError, "storyboard store is not configured")
		return
	}

	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	if !cfg.ensureSession(c, sessionID) {
		return
	}

	if cfg.DynamicStoryboards != nil {
		aggregate, err := cfg.DynamicStoryboards.GetAggregateBySession(c.Request.Context(), sessionID)
		if err == nil {
			c.JSON(http.StatusOK, aggregate.PublicView())
			return
		}
		if !errors.Is(err, storyboard.ErrAggregateNotFound) {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if cfg.Storyboards == nil {
		c.Status(http.StatusNoContent)
		return
	}
	board, err := cfg.Storyboards.GetLatestBySession(c.Request.Context(), sessionID)
	if err != nil {
		if errors.Is(err, storyboard.ErrNotFound) {
			c.Status(http.StatusNoContent)
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, board)
}

// patchStoryboard 接收前端故事板 JSON Patch，校验版本后写入并返回 patch 事件。
func (cfg Config) patchStoryboard(c *gin.Context) {
	if cfg.Storyboards == nil {
		writeJSONError(c, http.StatusInternalServerError, "storyboard store is not configured")
		return
	}

	sessionID := strings.TrimSpace(c.Param("session_id"))
	storyboardID := strings.TrimSpace(c.Param("storyboard_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	if storyboardID == "" {
		writeJSONError(c, http.StatusBadRequest, "storyboard id is required")
		return
	}
	if !cfg.ensureSession(c, sessionID) {
		return
	}
	board, err := cfg.Storyboards.Get(c.Request.Context(), storyboardID)
	if err != nil || board.SessionID != sessionID {
		writeJSONError(c, http.StatusNotFound, "storyboard not found")
		return
	}

	var req patchStoryboardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid storyboard patch request")
		return
	}
	if req.BaseVersion <= 0 {
		writeJSONError(c, http.StatusBadRequest, "base version is required")
		return
	}
	if len(req.Ops) == 0 {
		writeJSONError(c, http.StatusBadRequest, "patch ops are required")
		return
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "user"
	}
	patched, event, err := cfg.Storyboards.ApplyPatch(c.Request.Context(), storyboard.PatchRequest{
		EventID:      cfg.NewID(),
		SessionID:    sessionID,
		StoryboardID: storyboardID,
		BaseVersion:  req.BaseVersion,
		Source:       source,
		ToolCallID:   strings.TrimSpace(req.ToolCallID),
		Ops:          append([]aigctools.JSONPatchOp(nil), req.Ops...),
	})
	if err != nil {
		switch {
		case errors.Is(err, storyboard.ErrVersionConflict):
			writeJSONError(c, http.StatusConflict, err.Error())
		case errors.Is(err, storyboard.ErrInvalidPatch):
			writeJSONError(c, http.StatusBadRequest, err.Error())
		case errors.Is(err, storyboard.ErrNotFound):
			writeJSONError(c, http.StatusNotFound, "storyboard not found")
		default:
			writeJSONError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}

	c.JSON(http.StatusOK, patchStoryboardResponse{
		Storyboard: patched,
		Patch: a2ui.StoryboardPatchPayload{
			StoryboardID: event.StoryboardID,
			BaseVersion:  event.BaseVersion,
			NextVersion:  event.NextVersion,
			Ops:          append([]aigctools.JSONPatchOp(nil), event.Ops...),
			Source:       event.Source,
			ToolCallID:   event.ToolCallID,
		},
	})
}

// bindAssetToStoryboard 将素材绑定到故事板目标，并以 patch 形式更新故事板。
func (cfg Config) bindAssetToStoryboard(c *gin.Context) {
	if cfg.Storyboards == nil {
		writeJSONError(c, http.StatusInternalServerError, "storyboard store is not configured")
		return
	}
	if cfg.Assets == nil {
		writeJSONError(c, http.StatusInternalServerError, "asset store is not configured")
		return
	}

	sessionID := strings.TrimSpace(c.Param("session_id"))
	storyboardID := strings.TrimSpace(c.Param("storyboard_id"))
	assetID := strings.TrimSpace(c.Param("asset_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	if storyboardID == "" {
		writeJSONError(c, http.StatusBadRequest, "storyboard id is required")
		return
	}
	if assetID == "" {
		writeJSONError(c, http.StatusBadRequest, "asset id is required")
		return
	}
	if !cfg.ensureSession(c, sessionID) {
		return
	}

	var req bindAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid asset binding request")
		return
	}
	if req.BaseVersion <= 0 {
		writeJSONError(c, http.StatusBadRequest, "base version is required")
		return
	}

	assetRecord, err := cfg.Assets.Get(c.Request.Context(), assetID)
	if err != nil {
		if errors.Is(err, asset.ErrNotFound) {
			writeJSONError(c, http.StatusNotFound, "asset not found")
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if assetRecord.SessionID != "" && assetRecord.SessionID != sessionID {
		writeJSONError(c, http.StatusBadRequest, "asset does not belong to session")
		return
	}
	if assetRecord.Availability != asset.AvailabilityAvailable {
		writeJSONError(c, http.StatusConflict, "asset is not available for binding")
		return
	}
	board, err := cfg.Storyboards.Get(c.Request.Context(), storyboardID)
	if err != nil {
		if errors.Is(err, storyboard.ErrNotFound) {
			writeJSONError(c, http.StatusNotFound, "storyboard not found")
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if board.SessionID != "" && board.SessionID != sessionID {
		writeJSONError(c, http.StatusBadRequest, "storyboard does not belong to session")
		return
	}
	ops, err := storyboard.AssetBindingOps(board, storyboard.AssetBindingRequest{
		AssetID:    assetRecord.ID,
		AssetKind:  assetRecord.Kind,
		TargetType: req.TargetType,
		TargetID:   req.TargetID,
		Field:      req.Field,
	})
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, err.Error())
		return
	}

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "user"
	}
	patched, event, err := cfg.Storyboards.ApplyPatch(c.Request.Context(), storyboard.PatchRequest{
		EventID:      cfg.NewID(),
		SessionID:    sessionID,
		StoryboardID: storyboardID,
		BaseVersion:  req.BaseVersion,
		Source:       source,
		ToolCallID:   strings.TrimSpace(req.ToolCallID),
		Ops:          ops,
	})
	if err != nil {
		switch {
		case errors.Is(err, storyboard.ErrVersionConflict):
			writeJSONError(c, http.StatusConflict, err.Error())
		case errors.Is(err, storyboard.ErrInvalidPatch):
			writeJSONError(c, http.StatusBadRequest, err.Error())
		case errors.Is(err, storyboard.ErrNotFound):
			writeJSONError(c, http.StatusNotFound, "storyboard not found")
		default:
			writeJSONError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}

	c.JSON(http.StatusOK, patchStoryboardResponse{
		Storyboard: patched,
		Patch: a2ui.StoryboardPatchPayload{
			StoryboardID: event.StoryboardID,
			BaseVersion:  event.BaseVersion,
			NextVersion:  event.NextVersion,
			Ops:          append([]aigctools.JSONPatchOp(nil), event.Ops...),
			Source:       event.Source,
			ToolCallID:   event.ToolCallID,
		},
	})
}

func (cfg Config) updateTargetPrompt(c *gin.Context) {
	if cfg.DynamicStoryboards == nil || cfg.StoryboardCommands == nil {
		writeJSONError(c, http.StatusInternalServerError, "dynamic storyboard command service is not configured")
		return
	}
	sessionID, storyboardID, targetID := strings.TrimSpace(c.Param("session_id")), strings.TrimSpace(c.Param("storyboard_id")), strings.TrimSpace(c.Param("target_id"))
	if sessionID == "" || storyboardID == "" || targetID == "" || !cfg.ensureSession(c, sessionID) {
		return
	}
	var req updateTargetPromptRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ExpectedVersion <= 0 || req.PromptRevision <= 0 || strings.TrimSpace(req.Purpose) == "" {
		writeJSONError(c, http.StatusBadRequest, "expected_version, prompt_revision and purpose are required")
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeJSONError(c, http.StatusBadRequest, "prompt content is required")
		return
	}
	commandID := strings.TrimSpace(req.IdempotencyKey)
	if commandID == "" {
		commandID = stableRequestID("prompt", strings.Join([]string{sessionID, storyboardID, targetID, req.Purpose, strconv.Itoa(req.PromptRevision), req.Prompt, req.PromptRef}, "\x00"))
	}
	aggregate, err := cfg.DynamicStoryboards.GetAggregate(c.Request.Context(), storyboardID)
	if err != nil {
		writeStoryboardCommandError(c, err)
		return
	}
	if aggregate.SessionID != sessionID {
		writeJSONError(c, http.StatusNotFound, "storyboard not found")
		return
	}
	updated, stale, err := cfg.StoryboardCommands.UpdatePrompt(c.Request.Context(), storyboard.UpdatePromptCommand{
		CommandID: commandID, StoryboardID: storyboardID, BaseVersion: req.ExpectedVersion,
		TargetID: targetID, ExpectedTargetRevision: req.TargetRevision, Purpose: req.Purpose, ExpectedRevision: req.PromptRevision,
		Prompt: req.Prompt, PromptRef: req.PromptRef, LockedByUser: true,
	})
	if err != nil {
		writeStoryboardCommandError(c, err)
		return
	}
	view := updated.PublicView()
	c.JSON(http.StatusOK, gin.H{"aggregate": view, "storyboard": view, "stale_targets": stale})
}

func (cfg Config) regenerateTarget(c *gin.Context) {
	if cfg.DynamicStoryboards == nil || cfg.StoryboardCommands == nil || cfg.GenerationCommands == nil {
		writeJSONError(c, http.StatusInternalServerError, "storyboard and generation command services are not configured")
		return
	}
	sessionID, storyboardID, targetID := strings.TrimSpace(c.Param("session_id")), strings.TrimSpace(c.Param("storyboard_id")), strings.TrimSpace(c.Param("target_id"))
	if sessionID == "" || storyboardID == "" || targetID == "" || !cfg.ensureSession(c, sessionID) {
		return
	}
	sessionRecord, err := cfg.Store.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusNotFound, "session not found")
		return
	}
	var req regenerateTargetRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ExpectedVersion <= 0 || strings.TrimSpace(req.AssetSlot) == "" {
		writeJSONError(c, http.StatusBadRequest, "expected_version and asset_slot are required")
		return
	}
	req.AssetSlot = strings.TrimSpace(req.AssetSlot)
	req.MediaKind = strings.ToLower(strings.TrimSpace(req.MediaKind))
	if req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey); req.IdempotencyKey == "" {
		req.IdempotencyKey = stableRequestID("regenerate", strings.Join([]string{sessionID, storyboardID, targetID, req.AssetSlot, strconv.Itoa(req.ExpectedVersion)}, ":"))
	}
	if cfg.GenerationWorkflow != nil {
		if existing, lookupErr := cfg.GenerationWorkflow.GetOperationByIdempotencyKey(c.Request.Context(), req.IdempotencyKey); lookupErr == nil {
			if existing.SessionID != sessionID || existing.Kind != "target_regeneration" {
				writeJSONError(c, http.StatusConflict, "idempotency key is already bound to a different generation request")
				return
			}
			batch, batchErr := cfg.GenerationWorkflow.GetBatch(c.Request.Context(), existing.BatchID)
			jobs, jobsErr := cfg.GenerationWorkflow.ListJobsByBatch(c.Request.Context(), existing.BatchID)
			if batchErr != nil || jobsErr != nil {
				writeJSONError(c, http.StatusInternalServerError, "load existing regeneration workflow failed")
				return
			}
			if len(jobs) != 1 || jobs[0].TargetID != targetID || jobs[0].AssetSlot != strings.TrimSpace(req.AssetSlot) || jobs[0].StoryboardVersionAtDispatch != req.ExpectedVersion+1 ||
				(req.TargetRevision > 0 && jobs[0].BindingToken.TargetRevision != req.TargetRevision) ||
				(req.MediaKind != "" && strings.ToLower(strings.TrimSpace(jobs[0].MediaKind)) != req.MediaKind) {
				writeJSONError(c, http.StatusConflict, "idempotency key is already bound to a different regeneration target")
				return
			}
			public := publicWorkflowAggregate(generation.WorkflowAggregate{Operation: existing, Batch: batch, Jobs: jobs})
			c.JSON(http.StatusAccepted, gin.H{"status": public.Operation.Status, "operation": public.Operation, "batch": public.Batch, "jobs": public.Jobs})
			return
		} else if !errors.Is(lookupErr, generation.ErrNotFound) {
			writeJSONError(c, http.StatusInternalServerError, lookupErr.Error())
			return
		}
	}
	aggregate, err := cfg.DynamicStoryboards.GetAggregate(c.Request.Context(), storyboardID)
	if err != nil {
		writeStoryboardCommandError(c, err)
		return
	}
	if aggregate.SessionID != sessionID {
		writeJSONError(c, http.StatusNotFound, "storyboard not found")
		return
	}
	if snapshot, found, snapshotErr := loadRegenerationDispatchSnapshot(c.Request.Context(), cfg.DynamicStoryboards, storyboardID, req.IdempotencyKey+":epoch"); snapshotErr != nil {
		writeJSONError(c, http.StatusInternalServerError, snapshotErr.Error())
		return
	} else if found {
		if snapshot.Input.TargetID != targetID || snapshot.Input.AssetSlot != strings.TrimSpace(req.AssetSlot) || snapshot.UserID != sessionRecord.UserID || snapshot.StoryboardVersion != req.ExpectedVersion+1 ||
			(req.TargetRevision > 0 && snapshot.Input.TargetRevision != req.TargetRevision) ||
			(req.MediaKind != "" && strings.ToLower(strings.TrimSpace(snapshot.MediaKind)) != req.MediaKind) {
			writeJSONError(c, http.StatusConflict, "idempotency key is already bound to a different regeneration target")
			return
		}
		workflow, createErr := cfg.createTargetRegenerationWorkflow(c.Request.Context(), sessionID, storyboardID, req.IdempotencyKey, snapshot)
		if createErr != nil {
			writeJSONError(c, http.StatusInternalServerError, createErr.Error())
			return
		}
		public := publicWorkflowAggregate(workflow)
		view := aggregate.PublicView()
		c.JSON(http.StatusAccepted, gin.H{"status": public.Operation.Status, "aggregate": view, "storyboard": view, "operation": public.Operation, "batch": public.Batch, "jobs": public.Jobs})
		return
	}
	if aggregate.PendingRevisionID != "" {
		writeJSONError(c, http.StatusConflict, "approve or reject the pending storyboard revision before regenerating assets")
		return
	}
	element, slot, ok := dynamicTarget(aggregate, targetID, req.AssetSlot)
	if !ok {
		writeJSONError(c, http.StatusNotFound, "storyboard target or asset slot not found")
		return
	}
	if req.MediaKind != "" && strings.ToLower(strings.TrimSpace(slot.MediaKind)) != req.MediaKind {
		writeJSONError(c, http.StatusConflict, "media_kind does not match the asset slot")
		return
	}
	if req.TargetRevision > 0 && element.Revision != req.TargetRevision {
		writeJSONError(c, http.StatusConflict, "storyboard target revision conflict")
		return
	}
	provider := providerForMediaKind(slot.MediaKind)
	if provider == "" {
		writeJSONError(c, http.StatusConflict, "asset slot media kind is not supported by a generation provider")
		return
	}
	activeRevision, err := aggregate.ActiveRevision()
	if err != nil {
		writeStoryboardCommandError(c, err)
		return
	}
	if cfg.Specs != nil {
		confirmed, specErr := cfg.Specs.GetConfirmedBySession(c.Request.Context(), sessionID)
		if specErr != nil {
			writeJSONError(c, http.StatusConflict, "confirmed creation spec is not available")
			return
		}
		if activeRevision.DerivedFromSpecVersion > 0 && activeRevision.DerivedFromSpecVersion != confirmed.Version {
			writeJSONError(c, http.StatusConflict, "storyboard must be replanned for the latest confirmed creation spec")
			return
		}
	}
	// Preview the post-command semantic input so provider validation and billing
	// preflight run before the epoch advances, then persist that exact accepted
	// input in the regeneration domain event for crash recovery.
	preview := aggregate.Clone()
	_, err = preview.RegenerateAsset(storyboard.RegenerateAssetCommand{CommandID: "preview:" + req.IdempotencyKey, StoryboardID: storyboardID, BaseVersion: aggregate.Version, TargetID: targetID, AssetSlot: req.AssetSlot})
	if err != nil {
		writeStoryboardCommandError(c, err)
		return
	}
	predictedInput, err := preview.ResolveGenerationInput(targetID, req.AssetSlot)
	if err != nil {
		writeStoryboardCommandError(c, err)
		return
	}
	if strings.TrimSpace(predictedInput.Prompt) == "" {
		writeJSONError(c, http.StatusConflict, "target prompt is not ready")
		return
	}
	payload := localGenerationPayload(predictedInput, element, slot, sessionRecord.UserID)
	preflightJob := generation.GenerationJob{
		Provider: provider, MediaKind: slot.MediaKind,
		Payload: payload,
	}
	if err := generation.ValidateProviderJob(preflightJob); err != nil {
		writeJSONError(c, http.StatusBadRequest, err.Error())
		return
	}
	estimatedPoints := int64(0)
	if cfg.GenerationPreflight != nil {
		estimatedPoints, err = cfg.GenerationPreflight(c.Request.Context(), sessionRecord.UserID, []generation.GenerationJob{preflightJob})
		if err != nil {
			writeJSONError(c, http.StatusConflict, err.Error())
			return
		}
	}
	snapshot := storyboard.RegenerationDispatchSnapshot{
		Provider: provider, MediaKind: slot.MediaKind, UserID: sessionRecord.UserID,
		SpecVersion: activeRevision.DerivedFromSpecVersion, StoryboardVersion: aggregate.Version + 1,
		EstimatedPoints: estimatedPoints, Input: predictedInput, Payload: payload,
	}
	updated, _, err := cfg.StoryboardCommands.Regenerate(c.Request.Context(), storyboard.RegenerateAssetCommand{CommandID: req.IdempotencyKey + ":epoch", StoryboardID: storyboardID, BaseVersion: req.ExpectedVersion, TargetID: targetID, AssetSlot: req.AssetSlot, DispatchSnapshot: snapshot})
	if err != nil {
		writeStoryboardCommandError(c, err)
		return
	}
	workflow, err := cfg.createTargetRegenerationWorkflow(c.Request.Context(), sessionID, storyboardID, req.IdempotencyKey, snapshot)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	public := publicWorkflowAggregate(workflow)
	view := updated.PublicView()
	c.JSON(http.StatusAccepted, gin.H{"status": public.Operation.Status, "aggregate": view, "storyboard": view, "operation": public.Operation, "batch": public.Batch, "jobs": public.Jobs})
}

func loadRegenerationDispatchSnapshot(ctx context.Context, repository storyboard.AggregateRepository, storyboardID, commandID string) (storyboard.RegenerationDispatchSnapshot, bool, error) {
	events, err := repository.ListDomainEvents(ctx, storyboardID, -1)
	if err != nil {
		return storyboard.RegenerationDispatchSnapshot{}, false, err
	}
	for _, event := range events {
		if event.CommandID != commandID || event.Type != "storyboard.regeneration_requested" {
			continue
		}
		raw, marshalErr := json.Marshal(event.Payload["dispatch_snapshot"])
		if marshalErr != nil {
			return storyboard.RegenerationDispatchSnapshot{}, false, marshalErr
		}
		var snapshot storyboard.RegenerationDispatchSnapshot
		if unmarshalErr := json.Unmarshal(raw, &snapshot); unmarshalErr != nil {
			return storyboard.RegenerationDispatchSnapshot{}, false, unmarshalErr
		}
		if strings.TrimSpace(snapshot.Provider) == "" || strings.TrimSpace(snapshot.Input.Fingerprint) == "" {
			return storyboard.RegenerationDispatchSnapshot{}, false, fmt.Errorf("persisted regeneration command has no dispatch snapshot")
		}
		return snapshot, true, nil
	}
	return storyboard.RegenerationDispatchSnapshot{}, false, nil
}

func (cfg Config) createTargetRegenerationWorkflow(ctx context.Context, sessionID, storyboardID, idempotencyKey string, snapshot storyboard.RegenerationDispatchSnapshot) (generation.WorkflowAggregate, error) {
	operationID, batchID, jobID := cfg.NewID(), cfg.NewID(), cfg.NewID()
	policy := generation.DeliveryPolicy{BindingMode: generation.BindingModeCandidate, ApprovalPolicy: generation.ApprovalReviewRequired, ChargePolicy: generation.ChargePostpaidNoReservation}
	workflow, _, err := cfg.GenerationCommands.Create(ctx, generation.CreateWorkflowCommand{
		Operation: generation.GenerationOperation{ID: operationID, SessionID: sessionID, UserID: snapshot.UserID, IdempotencyKey: idempotencyKey, Kind: "target_regeneration", BatchID: batchID, Result: map[string]any{"estimated_points": snapshot.EstimatedPoints}},
		Batch:     generation.GenerationBatch{ID: batchID, SessionID: sessionID, UserID: snapshot.UserID, OperationID: operationID, Kind: "target_regeneration", CompletionPolicy: generation.CompletionAllRequired, WakePolicy: generation.WakeOnTerminal, DeliveryPolicy: policy, ExpectedSpecVersion: snapshot.SpecVersion, ExpectedStoryboardVersion: snapshot.StoryboardVersion},
		Jobs: []generation.GenerationJob{{
			ID: jobID, SessionID: sessionID, UserID: snapshot.UserID, StoryboardID: storyboardID,
			IdempotencyKey: idempotencyKey + ":job", Provider: snapshot.Provider, MediaKind: snapshot.MediaKind,
			TargetID: snapshot.Input.TargetID, AssetSlot: snapshot.Input.AssetSlot, Required: true,
			StoryboardVersionAtDispatch: snapshot.StoryboardVersion,
			BindingToken:                generation.BindingToken{StoryboardID: storyboardID, TargetID: snapshot.Input.TargetID, AssetSlot: snapshot.Input.AssetSlot, TargetRevision: snapshot.Input.TargetRevision, PromptRevision: snapshot.Input.PromptRevision, GenerationEpoch: snapshot.Input.GenerationEpoch, SpecVersion: snapshot.SpecVersion, InputFingerprint: snapshot.Input.Fingerprint},
			DeliveryPolicy:              policy, MaxAttempts: 4, Payload: snapshot.Payload,
		}},
	})
	return workflow, err
}

func (cfg Config) bindTargetAsset(c *gin.Context) {
	if cfg.DynamicStoryboards == nil || cfg.StoryboardCommands == nil || cfg.Assets == nil {
		writeJSONError(c, http.StatusInternalServerError, "dynamic storyboard and asset stores are not configured")
		return
	}
	sessionID, storyboardID, targetID, assetID := strings.TrimSpace(c.Param("session_id")), strings.TrimSpace(c.Param("storyboard_id")), strings.TrimSpace(c.Param("target_id")), strings.TrimSpace(c.Param("asset_id"))
	if sessionID == "" || storyboardID == "" || targetID == "" || assetID == "" || !cfg.ensureSession(c, sessionID) {
		return
	}
	var req bindTargetAssetRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ExpectedVersion <= 0 || strings.TrimSpace(req.AssetSlot) == "" {
		writeJSONError(c, http.StatusBadRequest, "expected_version and asset_slot are required")
		return
	}
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.BindingID = strings.TrimSpace(req.BindingID)
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = stableRequestID("bind", strings.Join([]string{sessionID, storyboardID, targetID, assetID, req.AssetSlot, strconv.Itoa(req.ExpectedVersion)}, ":"))
	}
	aggregate, err := cfg.DynamicStoryboards.GetAggregate(c.Request.Context(), storyboardID)
	if err != nil {
		writeStoryboardCommandError(c, err)
		return
	}
	if aggregate.SessionID != sessionID {
		writeJSONError(c, http.StatusNotFound, "storyboard not found")
		return
	}
	replayBindingID := req.BindingID
	if replayBindingID == "" {
		replayBindingID = stableRequestID("binding", strings.Join([]string{req.IdempotencyKey, targetID, strings.TrimSpace(req.AssetSlot), assetID}, ":"))
	}
	for _, existing := range aggregate.Bindings {
		if existing.ID != replayBindingID {
			continue
		}
		if existing.AssetID != assetID || existing.TargetID != targetID || existing.AssetSlot != strings.TrimSpace(req.AssetSlot) {
			writeJSONError(c, http.StatusConflict, "binding id is already bound to a different target or asset")
			return
		}
		if existing.State == storyboard.BindingStateActive {
			view := aggregate.PublicView()
			c.JSON(http.StatusOK, gin.H{"aggregate": view, "storyboard": view, "binding_id": existing.ID})
			return
		}
		break
	}
	storedAsset, err := cfg.Assets.Get(c.Request.Context(), assetID)
	if err != nil || (storedAsset.SessionID != "" && storedAsset.SessionID != sessionID) {
		writeJSONError(c, http.StatusNotFound, "asset not found")
		return
	}
	if storedAsset.Availability != "" && storedAsset.Availability != asset.AvailabilityAvailable {
		writeJSONError(c, http.StatusConflict, "asset is not available for binding")
		return
	}
	if aggregate.PendingRevisionID != "" {
		writeJSONError(c, http.StatusConflict, "approve or reject the pending storyboard revision before binding assets")
		return
	}
	if cfg.Specs != nil {
		active, activeErr := aggregate.ActiveRevision()
		confirmed, specErr := cfg.Specs.GetConfirmedBySession(c.Request.Context(), sessionID)
		if activeErr != nil || specErr != nil || (active.DerivedFromSpecVersion > 0 && active.DerivedFromSpecVersion != confirmed.Version) {
			writeJSONError(c, http.StatusConflict, "storyboard must be replanned for the latest confirmed creation spec")
			return
		}
	}
	if req.BindingID != "" {
		for _, binding := range aggregate.Bindings {
			if binding.ID != req.BindingID {
				continue
			}
			if binding.AssetID != assetID || binding.TargetID != targetID || binding.AssetSlot != strings.TrimSpace(req.AssetSlot) {
				writeJSONError(c, http.StatusConflict, "binding id is already bound to a different target or asset")
				return
			}
			switch binding.State {
			case storyboard.BindingStateActive:
				view := aggregate.PublicView()
				c.JSON(http.StatusOK, gin.H{"aggregate": view, "storyboard": view, "binding_id": binding.ID})
				return
			case storyboard.BindingStateCandidate:
				if strings.TrimSpace(binding.ApprovalID) != "" {
					writeJSONError(c, http.StatusConflict, "candidate asset must be adopted through its approval decision")
					return
				}
				updated, stale, err := cfg.StoryboardCommands.Activate(c.Request.Context(), storyboard.ActivateBindingCommand{CommandID: req.IdempotencyKey, StoryboardID: storyboardID, BaseVersion: aggregate.Version, BindingID: req.BindingID})
				if err != nil {
					writeStoryboardCommandError(c, err)
					return
				}
				view := updated.PublicView()
				c.JSON(http.StatusOK, gin.H{"aggregate": view, "storyboard": view, "binding_id": binding.ID, "stale_targets": stale})
				return
			default:
				writeJSONError(c, http.StatusConflict, "binding is no longer eligible for activation")
				return
			}
		}
	}
	element, slot, ok := dynamicTarget(aggregate, targetID, req.AssetSlot)
	if !ok {
		writeJSONError(c, http.StatusNotFound, "storyboard target or asset slot not found")
		return
	}
	if !assetKindMatchesSlot(storedAsset.Kind, slot.MediaKind) {
		writeJSONError(c, http.StatusConflict, fmt.Sprintf("asset kind %s is incompatible with slot media kind %s", storedAsset.Kind, slot.MediaKind))
		return
	}
	if req.TargetRevision > 0 && req.TargetRevision != element.Revision {
		writeJSONError(c, http.StatusConflict, "storyboard target revision conflict")
		return
	}
	input, err := aggregate.ResolveGenerationInput(targetID, slot.Key)
	if err != nil && !errors.Is(err, storyboard.ErrDependencyNotReady) {
		writeStoryboardCommandError(c, err)
		return
	}
	currentPromptRevision := input.PromptRevision
	if req.PromptRevision > 0 && req.PromptRevision != currentPromptRevision {
		writeJSONError(c, http.StatusConflict, "storyboard prompt revision conflict")
		return
	}
	if req.GenerationEpoch > 0 && req.GenerationEpoch != slot.GenerationEpoch {
		writeJSONError(c, http.StatusConflict, "storyboard generation epoch conflict")
		return
	}
	commandID := req.IdempotencyKey
	bindingID := strings.TrimSpace(req.BindingID)
	if bindingID == "" {
		bindingID = stableRequestID("binding", strings.Join([]string{commandID, targetID, slot.Key, assetID}, ":"))
	}
	for _, existing := range aggregate.Bindings {
		if existing.ID != bindingID {
			continue
		}
		if existing.AssetID != assetID || existing.TargetID != targetID || existing.AssetSlot != slot.Key {
			writeJSONError(c, http.StatusConflict, "binding id is already bound to a different target or asset")
			return
		}
		switch existing.State {
		case storyboard.BindingStateActive:
			view := aggregate.PublicView()
			c.JSON(http.StatusOK, gin.H{"aggregate": view, "storyboard": view, "binding_id": bindingID})
			return
		case storyboard.BindingStateCandidate:
			if strings.TrimSpace(existing.ApprovalID) != "" {
				writeJSONError(c, http.StatusConflict, "candidate asset must be adopted through its approval decision")
				return
			}
			activated, stale, activateErr := cfg.StoryboardCommands.Activate(c.Request.Context(), storyboard.ActivateBindingCommand{CommandID: commandID + ":activate", StoryboardID: storyboardID, BaseVersion: aggregate.Version, BindingID: bindingID})
			if activateErr != nil {
				writeStoryboardCommandError(c, activateErr)
				return
			}
			view := activated.PublicView()
			c.JSON(http.StatusOK, gin.H{"aggregate": view, "storyboard": view, "binding_id": bindingID, "stale_targets": stale})
			return
		default:
			writeJSONError(c, http.StatusConflict, "binding is no longer eligible for activation")
			return
		}
	}
	bound, disposition, err := cfg.StoryboardCommands.Bind(c.Request.Context(), storyboard.BindAssetCommand{CommandID: commandID + ":bind", StoryboardID: storyboardID, BaseVersion: req.ExpectedVersion, BindingID: bindingID, TargetID: targetID, AssetSlot: slot.Key, AssetID: assetID, TargetRevision: input.TargetRevision, PromptRevision: currentPromptRevision, GenerationEpoch: input.GenerationEpoch, InputFingerprint: input.Fingerprint})
	if err != nil {
		writeStoryboardCommandError(c, err)
		return
	}
	if disposition == storyboard.BindingDispositionCandidate {
		bound, _, err = cfg.StoryboardCommands.Activate(c.Request.Context(), storyboard.ActivateBindingCommand{CommandID: commandID + ":activate", StoryboardID: storyboardID, BaseVersion: bound.Version, BindingID: bindingID})
		if err != nil {
			writeStoryboardCommandError(c, err)
			return
		}
	} else {
		writeJSONError(c, http.StatusConflict, "asset binding became stale before activation")
		return
	}
	view := bound.PublicView()
	c.JSON(http.StatusOK, gin.H{"aggregate": view, "storyboard": view, "binding_id": bindingID})
}

func assetKindMatchesSlot(assetKind, mediaKind string) bool {
	switch strings.ToLower(strings.TrimSpace(mediaKind)) {
	case "image", "illustration", "keyframe":
		return assetKind == asset.KindImage || assetKind == asset.KindReference
	case "video":
		return assetKind == asset.KindVideo
	case "audio", "music", "voice":
		return assetKind == asset.KindAudio
	case "text", "script", "lyrics":
		return assetKind == asset.KindText || assetKind == asset.KindPDF
	default:
		return false
	}
}

func (cfg Config) decideApproval(c *gin.Context) {
	if cfg.Approvals == nil || cfg.ApprovalRuntime == nil {
		writeJSONError(c, http.StatusInternalServerError, "approval runtime is not configured")
		return
	}
	sessionID, approvalID := strings.TrimSpace(c.Param("session_id")), strings.TrimSpace(c.Param("approval_id"))
	if sessionID == "" || approvalID == "" || !cfg.ensureSession(c, sessionID) {
		return
	}
	record, err := cfg.Approvals.Get(c.Request.Context(), approvalID)
	if err != nil || record.SessionID != sessionID {
		writeJSONError(c, http.StatusNotFound, "approval not found")
		return
	}
	var req approvalDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid approval decision request")
		return
	}
	requestedDecision := strings.ToLower(strings.TrimSpace(req.Decision))
	if requestedDecision != approval.DecisionApprove && requestedDecision != approval.DecisionReject {
		writeJSONError(c, http.StatusBadRequest, "decision must be approved or rejected")
		return
	}
	expectedDecisionVersion := record.DecisionVersion
	if req.ExpectedDecisionVersion != nil {
		expectedDecisionVersion = *req.ExpectedDecisionVersion
	} else if record.DecisionVersion > 0 {
		// A client that omitted explicit concurrency fields may be retrying after
		// the first 200 response was lost. Reuse the recorded decision's original
		// fence/key when the requested decision is identical.
		if decided, decisionErr := cfg.Approvals.GetDecision(c.Request.Context(), approvalID, record.DecisionVersion); decisionErr == nil && decided.RequestedDecision == requestedDecision {
			expectedDecisionVersion = record.DecisionVersion - 1
			if strings.TrimSpace(req.IdempotencyKey) == "" {
				req.IdempotencyKey = decided.IdempotencyKey
			}
		}
	}
	if expectedDecisionVersion < 0 {
		writeJSONError(c, http.StatusBadRequest, "expected_decision_version cannot be negative")
		return
	}
	if strings.TrimSpace(req.IdempotencyKey) == "" {
		req.IdempotencyKey = fmt.Sprintf("approval:%s:decision:%s", approvalID, requestedDecision)
	}
	result, err := cfg.ApprovalRuntime.Decide(c.Request.Context(), approvalruntime.DecideRequest{ApprovalID: approvalID, ExpectedDecisionVersion: expectedDecisionVersion, IdempotencyKey: req.IdempotencyKey, Decision: req.Decision, ActorID: record.UserID, Reason: req.Reason})
	if err != nil {
		switch {
		case errors.Is(err, approval.ErrVersionConflict), errors.Is(err, approval.ErrAlreadyDecided):
			writeJSONError(c, http.StatusConflict, err.Error())
		case errors.Is(err, approval.ErrNotFound):
			writeJSONError(c, http.StatusNotFound, err.Error())
		default:
			writeJSONError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if err := cfg.publishApprovalDecision(c.Request.Context(), result.Decision.Approval); err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"applied": result.Applied,
		"approval": gin.H{
			"id": result.Decision.Approval.ID, "status": result.Decision.Approval.Status,
			"decision_version": result.Decision.Approval.DecisionVersion,
		},
		"decision": gin.H{
			"requested_decision": result.Decision.Decision.RequestedDecision,
			"effective_status":   result.Decision.Decision.EffectiveStatus,
			"decision_version":   result.Decision.Decision.DecisionVersion,
		},
	})
}

func (cfg Config) decideCandidateApprovalBatch(c *gin.Context) {
	if cfg.Approvals == nil || cfg.ApprovalRuntime == nil || cfg.DynamicStoryboards == nil {
		writeJSONError(c, http.StatusInternalServerError, "candidate approval batch runtime is not configured")
		return
	}
	sessionID := strings.TrimSpace(c.Param("session_id"))
	storyboardID := strings.TrimSpace(c.Param("storyboard_id"))
	if sessionID == "" || storyboardID == "" || !cfg.ensureSession(c, sessionID) {
		return
	}
	var req candidateApprovalBatchDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid candidate approval batch decision request")
		return
	}
	req.Decision = strings.ToLower(strings.TrimSpace(req.Decision))
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if req.Decision != approval.DecisionApprove {
		writeJSONError(c, http.StatusBadRequest, "candidate approval batch only supports approved")
		return
	}
	if req.ExpectedStoryboardVersion <= 0 {
		writeJSONError(c, http.StatusBadRequest, "expected_storyboard_version must be positive")
		return
	}
	if req.IdempotencyKey == "" {
		writeJSONError(c, http.StatusBadRequest, "idempotency_key is required")
		return
	}
	sessionRecord, err := cfg.Store.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusNotFound, "session not found")
		return
	}
	result, err := cfg.ApprovalRuntime.ApproveCandidateBatch(c.Request.Context(), approvalruntime.CandidateBatchApproveRequest{
		SessionID: sessionID, StoryboardID: storyboardID,
		ExpectedStoryboardVersion: req.ExpectedStoryboardVersion,
		IdempotencyKey:            req.IdempotencyKey, Decision: req.Decision,
		ActorID: sessionRecord.UserID, Reason: req.Reason,
	})
	if err != nil {
		switch {
		case errors.Is(err, approvalruntime.ErrCandidateBatchStoryboardVersion),
			errors.Is(err, approvalruntime.ErrCandidateBatchGenerationRunning),
			errors.Is(err, approvalruntime.ErrNoPendingCandidateApprovals),
			errors.Is(err, approval.ErrIdempotencyConflict):
			writeJSONError(c, http.StatusConflict, err.Error())
		case errors.Is(err, storyboard.ErrAggregateNotFound):
			writeJSONError(c, http.StatusNotFound, "storyboard not found")
		default:
			writeJSONError(c, http.StatusInternalServerError, err.Error())
		}
		return
	}
	if err := cfg.publishCandidateApprovalBatch(c.Request.Context(), result); err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	status := http.StatusOK
	if !result.Summary.Complete {
		status = http.StatusMultiStatus
	}
	c.JSON(status, result)
}

func (cfg Config) publishCandidateApprovalBatch(ctx context.Context, result approvalruntime.CandidateBatchApproveResult) error {
	if cfg.Events == nil {
		return nil
	}
	aggregate := result.Storyboard.PublicView()
	actions := []a2ui.Action{{
		Type: a2ui.ActionUpdateCard, Surface: "storyboard",
		Target: &a2ui.ActionTarget{Surface: "storyboard", CardID: "storyboard"},
		// Keep this version-keyed projection independent of transient batch
		// continuation failures. A retry may change the HTTP summary without
		// changing the storyboard version; reusing the event ID must therefore
		// reuse byte-equivalent business payload.
		Payload: map[string]any{"storyboard": aggregate, "source": "candidate_approval_batch"},
	}}
	return cfg.Events.Publish(ctx, a2ui.SSEEvent{
		ID:        candidateApprovalBatchStoryboardEventID(result.Batch.ID, aggregate.ID, aggregate.Version),
		SessionID: result.Batch.SessionID, Event: a2ui.EventAction,
		Payload: a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: actions}, CreatedAt: cfg.Now(),
	})
}

func (cfg Config) publishApprovalDecision(ctx context.Context, record approval.Approval) error {
	if cfg.Events == nil {
		return nil
	}
	if record.ArtifactType != "candidate_asset" {
		actions := []a2ui.Action{{Type: a2ui.ActionUpdateCard, Surface: "chat", Target: &a2ui.ActionTarget{Surface: "chat", CardID: "approval:" + record.ID}, Payload: map[string]any{"status": record.Status, "decision_version": record.DecisionVersion}}}
		// Candidate approvals deliberately have no chat card. Other decision
		// events contain only data frozen by that approval; the mutable
		// storyboard snapshot is projected separately below.
		if err := cfg.Events.Publish(ctx, a2ui.SSEEvent{ID: fmt.Sprintf("approval:%s:decided:%d", record.ID, record.DecisionVersion), SessionID: record.SessionID, Event: a2ui.EventAction, Payload: a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: actions}, CreatedAt: cfg.Now()}); err != nil {
			return err
		}
	}
	if cfg.DynamicStoryboards != nil && record.Binding.StoryboardID != "" {
		if aggregate, err := cfg.DynamicStoryboards.GetAggregate(ctx, record.Binding.StoryboardID); err == nil {
			storyboardActions := []a2ui.Action{{Type: a2ui.ActionUpdateCard, Surface: "storyboard", Target: &a2ui.ActionTarget{Surface: "storyboard", CardID: "storyboard"}, Payload: map[string]any{"storyboard": aggregate.PublicView()}}}
			return cfg.Events.Publish(ctx, a2ui.SSEEvent{ID: fmt.Sprintf("approval:%s:storyboard:%s:v%d", record.ID, aggregate.ID, aggregate.Version), SessionID: record.SessionID, Event: a2ui.EventAction, Payload: a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: storyboardActions}, CreatedAt: cfg.Now()})
		}
	}
	return nil
}

func (cfg Config) getGenerationOperation(c *gin.Context) {
	if cfg.GenerationWorkflow == nil {
		writeJSONError(c, http.StatusInternalServerError, "generation workflow store is not configured")
		return
	}
	sessionID, operationID := strings.TrimSpace(c.Param("session_id")), strings.TrimSpace(c.Param("operation_id"))
	if sessionID == "" || operationID == "" || !cfg.ensureSession(c, sessionID) {
		return
	}
	operation, err := cfg.GenerationWorkflow.GetOperation(c.Request.Context(), operationID)
	if err != nil || operation.SessionID != sessionID {
		writeJSONError(c, http.StatusNotFound, "generation operation not found")
		return
	}
	batch, err := cfg.GenerationWorkflow.GetBatch(c.Request.Context(), operation.BatchID)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	jobs, err := cfg.GenerationWorkflow.ListJobsByBatch(c.Request.Context(), batch.ID)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, publicWorkflowAggregate(generation.WorkflowAggregate{Operation: operation, Batch: batch, Jobs: jobs}))
}

func (cfg Config) controlGenerationOperation(c *gin.Context) {
	if cfg.GenerationWorkflow == nil || cfg.GenerationCommands == nil {
		writeJSONError(c, http.StatusInternalServerError, "generation workflow command service is not configured")
		return
	}
	sessionID, operationID := strings.TrimSpace(c.Param("session_id")), strings.TrimSpace(c.Param("operation_id"))
	if sessionID == "" || operationID == "" || !cfg.ensureSession(c, sessionID) {
		return
	}
	operation, err := cfg.GenerationWorkflow.GetOperation(c.Request.Context(), operationID)
	if err != nil || operation.SessionID != sessionID {
		writeJSONError(c, http.StatusNotFound, "generation operation not found")
		return
	}
	var req generationControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid generation control request")
		return
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "cancel":
		workflow, err := cfg.GenerationCommands.CancelBatch(c.Request.Context(), operation.BatchID)
		if err != nil {
			writeJSONError(c, http.StatusConflict, err.Error())
			return
		}
		c.JSON(http.StatusAccepted, publicWorkflowAggregate(workflow))
	case "retry_failed":
		retryKey := strings.TrimSpace(req.IdempotencyKey)
		if retryKey == "" {
			retryKey = "recovery:" + operation.ID
		}
		// Replay is resolved before consulting mutable provider, balance or
		// storyboard state. Once a recovery workflow has been accepted, a lost
		// HTTP response must return that workflow rather than reinterpret it.
		if existing, lookupErr := cfg.GenerationWorkflow.GetOperationByIdempotencyKey(c.Request.Context(), retryKey); lookupErr == nil {
			recoveryOf := strings.TrimSpace(fmt.Sprint(existing.Result["recovery_of_operation_id"]))
			if existing.SessionID != sessionID || existing.Kind != operation.Kind+"_recovery" || recoveryOf != operation.ID {
				writeJSONError(c, http.StatusConflict, "idempotency key is already bound to a different recovery request")
				return
			}
			existingBatch, batchErr := cfg.GenerationWorkflow.GetBatch(c.Request.Context(), existing.BatchID)
			if batchErr != nil {
				writeJSONError(c, http.StatusInternalServerError, batchErr.Error())
				return
			}
			existingJobs, jobsErr := cfg.GenerationWorkflow.ListJobsByBatch(c.Request.Context(), existingBatch.ID)
			if jobsErr != nil {
				writeJSONError(c, http.StatusInternalServerError, jobsErr.Error())
				return
			}
			c.JSON(http.StatusAccepted, publicWorkflowAggregate(generation.WorkflowAggregate{Operation: existing, Batch: existingBatch, Jobs: existingJobs}))
			return
		} else if !errors.Is(lookupErr, generation.ErrNotFound) {
			writeJSONError(c, http.StatusInternalServerError, lookupErr.Error())
			return
		}
		batch, err := cfg.GenerationWorkflow.GetBatch(c.Request.Context(), operation.BatchID)
		if err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		previousJobs, err := cfg.GenerationWorkflow.ListJobsByBatch(c.Request.Context(), batch.ID)
		if err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		operationID, batchID := cfg.NewID(), cfg.NewID()
		retryJobs := make([]generation.GenerationJob, 0)
		for _, previous := range previousJobs {
			if previous.Status != generation.StatusFailed {
				continue
			}
			if previous.ErrorStage != generation.ErrorStageProvider || !generation.ProviderErrorRetryable(errors.New(previous.ErrorMessage)) {
				continue
			}
			// Semantic supersession/orphaning is not a transient provider failure;
			// the user must issue a new targeted generation against current state.
			if previous.ResultDisposition == generation.DispositionSuperseded || previous.ResultDisposition == generation.DispositionOrphaned {
				continue
			}
			if cfg.DynamicStoryboards != nil {
				aggregate, loadErr := cfg.DynamicStoryboards.GetAggregate(c.Request.Context(), previous.BindingToken.StoryboardID)
				if loadErr != nil {
					continue
				}
				active, activeErr := aggregate.ActiveRevision()
				if activeErr != nil {
					continue
				}
				if cfg.Specs != nil {
					confirmed, specErr := cfg.Specs.GetConfirmedBySession(c.Request.Context(), aggregate.SessionID)
					if specErr != nil || (active.DerivedFromSpecVersion > 0 && active.DerivedFromSpecVersion != confirmed.Version) {
						continue
					}
				}
				if strings.HasPrefix(previous.BindingToken.TargetID, "assembly:") {
					if previous.BindingToken.AggregateVersion != aggregate.Version || previous.BindingToken.SpecVersion != active.DerivedFromSpecVersion {
						continue
					}
				} else {
					input, resolveErr := aggregate.ResolveGenerationInput(previous.BindingToken.TargetID, previous.BindingToken.AssetSlot)
					current := generation.BindingToken{StoryboardID: aggregate.ID, TargetID: input.TargetID, AssetSlot: input.AssetSlot, TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision, GenerationEpoch: input.GenerationEpoch, SpecVersion: active.DerivedFromSpecVersion, InputFingerprint: input.Fingerprint}
					if resolveErr != nil || !previous.BindingToken.Equal(current) {
						continue
					}
				}
			}
			retryJobs = append(retryJobs, generation.GenerationJob{
				ID: cfg.NewID(), SessionID: previous.SessionID, UserID: previous.UserID,
				WorkflowRunID: previous.WorkflowRunID, StageRunID: previous.StageRunID,
				StoryboardID: previous.StoryboardID, ToolCallID: previous.ToolCallID,
				IdempotencyKey: retryKey + ":job:" + previous.ID,
				Provider:       previous.Provider, MediaKind: previous.MediaKind, TargetType: previous.TargetType,
				TargetID: previous.TargetID, AssetSlot: previous.AssetSlot, VariantKey: previous.VariantKey,
				Required: previous.Required, StoryboardVersionAtDispatch: previous.StoryboardVersionAtDispatch,
				BindingToken: previous.BindingToken, DeliveryPolicy: previous.DeliveryPolicy,
				MaxAttempts: previous.MaxAttempts, Payload: previous.Payload,
			})
		}
		if len(retryJobs) == 0 {
			writeJSONError(c, http.StatusConflict, "operation has no retryable failed jobs")
			return
		}
		var estimatedPoints int64
		for _, job := range retryJobs {
			if err := generation.ValidateProviderJob(job); err != nil {
				writeJSONError(c, http.StatusConflict, err.Error())
				return
			}
		}
		if cfg.GenerationPreflight != nil {
			estimatedPoints, err = cfg.GenerationPreflight(c.Request.Context(), operation.UserID, retryJobs)
			if err != nil {
				writeJSONError(c, http.StatusConflict, err.Error())
				return
			}
		}
		workflow, _, err := cfg.GenerationCommands.Create(c.Request.Context(), generation.CreateWorkflowCommand{
			Operation: generation.GenerationOperation{ID: operationID, SessionID: operation.SessionID, UserID: operation.UserID, WorkflowRunID: operation.WorkflowRunID, StageRunID: operation.StageRunID, ToolCallID: operation.ToolCallID, IdempotencyKey: retryKey, Kind: operation.Kind + "_recovery", Result: map[string]any{"recovery_of_operation_id": operation.ID, "estimated_points": estimatedPoints}, BatchID: batchID},
			Batch:     generation.GenerationBatch{ID: batchID, SessionID: batch.SessionID, UserID: batch.UserID, WorkflowRunID: batch.WorkflowRunID, StageRunID: batch.StageRunID, ToolCallID: batch.ToolCallID, Kind: batch.Kind + "_recovery", CompletionPolicy: batch.CompletionPolicy, MinSuccess: batch.MinSuccess, WakePolicy: batch.WakePolicy, DeliveryPolicy: batch.DeliveryPolicy, ExpectedSpecVersion: batch.ExpectedSpecVersion, ExpectedStoryboardVersion: batch.ExpectedStoryboardVersion},
			Jobs:      retryJobs,
		})
		if err != nil {
			writeJSONError(c, http.StatusConflict, err.Error())
			return
		}
		c.JSON(http.StatusAccepted, publicWorkflowAggregate(workflow))
	default:
		writeJSONError(c, http.StatusBadRequest, "action must be cancel or retry_failed")
	}
}

func (cfg Config) manualFinalizeCompensation(c *gin.Context) {
	if cfg.Compensation == nil || strings.TrimSpace(cfg.AdminToken) == "" {
		writeJSONError(c, http.StatusNotFound, "admin endpoint is not configured")
		return
	}
	provided := strings.TrimSpace(c.GetHeader("Authorization"))
	if !strings.HasPrefix(provided, "Bearer ") {
		writeJSONError(c, http.StatusUnauthorized, "admin authorization is required")
		return
	}
	provided = strings.TrimSpace(strings.TrimPrefix(provided, "Bearer "))
	expected := strings.TrimSpace(cfg.AdminToken)
	if len(provided) != len(expected) || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
		writeJSONError(c, http.StatusUnauthorized, "admin authorization is invalid")
		return
	}
	jobID := strings.TrimSpace(c.Param("job_id"))
	if jobID == "" {
		writeJSONError(c, http.StatusBadRequest, "job id is required")
		return
	}
	var req manualCompensationRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RefundedPoints < 0 {
		writeJSONError(c, http.StatusBadRequest, "refunded_points must be zero or greater")
		return
	}
	job, err := cfg.Compensation.ManualFinalize(c.Request.Context(), jobID, req.RefundedPoints, strings.TrimSpace(req.RefundTransactionID))
	if err != nil {
		if errors.Is(err, generation.ErrNotFound) {
			writeJSONError(c, http.StatusNotFound, "generation job not found")
			return
		}
		writeJSONError(c, http.StatusConflict, err.Error())
		return
	}
	visible := publicGenerationJobs([]generation.GenerationJob{job})
	c.JSON(http.StatusOK, gin.H{"job": visible[0]})
}

// createMessage 写入用户消息并触发 Agent；A2UI 输出统一发布到 /events/stream。
func (cfg Config) createMessage(c *gin.Context) {
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return
	}
	if cfg.Runtime == nil && cfg.Invoker == nil {
		writeJSONError(c, http.StatusInternalServerError, "agent invoker is not configured")
		return
	}
	if cfg.Runtime == nil && cfg.Events == nil {
		writeJSONError(c, http.StatusInternalServerError, "event broker is not configured")
		return
	}

	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	sessionRecord, err := cfg.Store.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusNotFound, "session not found")
		return
	}

	var req messageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid message request")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeJSONError(c, http.StatusBadRequest, "message content is required")
		return
	}

	userMessage := schema.UserMessage(content)
	if cfg.Runtime != nil {
		messageID := cfg.NewID()
		idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
		if idempotencyKey == "" {
			idempotencyKey = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		}
		if idempotencyKey != "" {
			messageID = stableMessageID(sessionID, idempotencyKey)
		}
		input := sessionruntime.NewUserMessage(messageID, "message:"+messageID)
		runID := sessionruntime.StableRunnerRunID(sessionID, input.InputID)
		record, err := schemaMessageRecord(messageID, sessionID, runID, userMessage, cfg.Now())
		if err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		record.Metadata = messageMetadataFromRequest(req)
		if postgresSession, ok := cfg.Store.(*session.PostgresStore); ok && cfg.RuntimeStore != nil {
			if _, _, err := postgresSession.AppendMessageAndEnqueue(c.Request.Context(), cfg.RuntimeStore, record, input); err != nil {
				writeJSONError(c, http.StatusInternalServerError, err.Error())
				return
			}
			cfg.Runtime.Wake(sessionID)
		} else {
			savedMessage, err := cfg.Store.AppendMessage(c.Request.Context(), record)
			if err != nil {
				writeJSONError(c, http.StatusInternalServerError, err.Error())
				return
			}
			boundedInput, err := sessionruntime.WithContextMessageSeq(input, savedMessage.Seq)
			if err != nil {
				writeJSONError(c, http.StatusInternalServerError, err.Error())
				return
			}
			if _, err := cfg.Runtime.Enqueue(c.Request.Context(), sessionID, boundedInput); err != nil {
				writeJSONError(c, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if _, err := cfg.Runtime.EnsureSession(context.Background(), sessionID); err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		c.JSON(http.StatusAccepted, messageResponse{RunID: runID, MessageID: messageID, InputID: input.InputID, Status: "accepted"})
		return
	}
	runID := cfg.NewID()
	userRecord, err := cfg.appendSchemaMessageWithMetadata(c.Request.Context(), sessionID, runID, userMessage, messageMetadataFromRequest(req))
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}

	records, err := cfg.Store.ListMessages(c.Request.Context(), sessionID, cfg.MessageWindow)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	messages := recordsToSchemaMessages(records)
	if len(messages) == 0 || messages[len(messages)-1].Content != userRecord.Content {
		messages = append(messages, schema.UserMessage(userRecord.Content))
	}
	invokeReq := AgentInvokeRequest{
		Messages:     messages,
		CheckpointID: sessionID,
	}
	if cfg.SessionValues != nil {
		invokeReq.SessionValues = cfg.SessionValues(sessionRecord)
	}
	events, err := cfg.Invoker.Invoke(c.Request.Context(), invokeReq)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := cfg.publishAgentEvents(c.Request.Context(), sessionID, runID, events); err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, messageResponse{RunID: runID, Status: "completed"})
}

// resumeAgent 恢复 Agent interrupt，把用户确认内容交给 Eino runner 继续执行。
func (cfg Config) resumeAgent(c *gin.Context) {
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return
	}
	if cfg.Invoker == nil {
		writeJSONError(c, http.StatusInternalServerError, "agent invoker is not configured")
		return
	}
	if cfg.Events == nil {
		writeJSONError(c, http.StatusInternalServerError, "event broker is not configured")
		return
	}

	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	sessionRecord, err := cfg.Store.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusNotFound, "session not found")
		return
	}

	var req resumeAgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid resume request")
		return
	}
	req.CheckpointID = strings.TrimSpace(req.CheckpointID)
	req.InterruptID = strings.TrimSpace(req.InterruptID)
	if req.CheckpointID == "" {
		writeJSONError(c, http.StatusBadRequest, "checkpoint id is required")
		return
	}
	if req.InterruptID == "" {
		writeJSONError(c, http.StatusBadRequest, "interrupt id is required")
		return
	}
	mapping, err := cfg.checkpointForResume(c.Request.Context(), sessionID, req.InterruptID, req.CheckpointID, session.CheckpointScopeRunner)
	if errors.Is(err, session.ErrCheckpointNotFound) {
		writeJSONError(c, http.StatusNotFound, "checkpoint mapping not found")
		return
	}
	if err != nil {
		writeJSONError(c, http.StatusConflict, err.Error())
		return
	}
	if strings.TrimSpace(mapping.ApprovalID) != "" {
		writeJSONError(c, http.StatusConflict, "approval-bound checkpoint must be resumed through the approval decision endpoint")
		return
	}
	if mapping.Status == session.CheckpointStatusResumed {
		runID := cfg.NewID()
		if err := cfg.Events.Publish(c.Request.Context(), a2ui.SSEEvent{
			ID:        cfg.NewID(),
			SessionID: sessionID,
			RunID:     runID,
			Seq:       1,
			Event:     a2ui.EventAction,
			Payload: a2ui.ActionEnvelope{
				Version: a2ui.Version1,
				Actions: []a2ui.Action{{
					Type:    a2ui.ActionAppendCard,
					Surface: "chat",
					CardID:  "checkpoint-" + req.InterruptID,
					Card: &a2ui.Card{
						Type:    a2ui.CardTypeGeneric,
						Title:   "确认已处理",
						Message: "确认已处理。",
						Status:  session.CheckpointStatusResumed,
						Root:    "root",
						Components: []a2ui.Component{
							a2ui.CardContainer("root", []string{"message"}),
							a2ui.Text("message", "确认已处理。", "", ""),
						},
						Data: map[string]any{
							"checkpoint_id": req.CheckpointID,
							"interrupt_id":  req.InterruptID,
						},
					},
				}},
			},
			CreatedAt: cfg.Now(),
		}); err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		if err := cfg.publishInterruptResolved(c.Request.Context(), sessionID, req.CheckpointID, req.InterruptID); err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		c.JSON(http.StatusOK, messageResponse{RunID: runID, Status: "already_resumed"})
		return
	}
	// In durable mode resume_applied means the Runner output and any frozen
	// domain continuation are committed, but the authoritative Agent output may
	// still be awaiting projection. Re-enqueue/wake the same stable input; only
	// the durable processor may advance it to resumed after projection succeeds.
	if cfg.Runtime != nil {
		cfg.enqueueDurableAgentResume(c, sessionRecord, mapping, req)
		return
	}
	if mapping.Status == session.CheckpointStatusResumeApplied {
		if err := cfg.completeCheckpoint(c.Request.Context(), mapping); err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		if err := cfg.publishInterruptResolved(c.Request.Context(), sessionID, req.CheckpointID, req.InterruptID); err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		c.JSON(http.StatusOK, messageResponse{RunID: mapping.RunID, Status: "already_resumed"})
		return
	}
	if mapping.Status != session.CheckpointStatusPending {
		writeJSONError(c, http.StatusConflict, "checkpoint is already being resumed")
		return
	}
	mapping, err = cfg.claimCheckpoint(c.Request.Context(), mapping)
	if err != nil {
		writeJSONError(c, http.StatusConflict, "checkpoint is already being resumed")
		return
	}

	runID := cfg.NewID()
	if content := resumeContent(req); content != "" {
		if _, err := cfg.appendSchemaMessage(c.Request.Context(), sessionID, runID, schema.UserMessage(content)); err != nil {
			cfg.releaseCheckpoint(c.Request.Context(), mapping)
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
	}

	data := req.Data
	if data == nil {
		data = strings.TrimSpace(req.Content)
	}
	resumeReq := AgentResumeRequest{
		CheckpointID: req.CheckpointID,
		Targets:      map[string]any{req.InterruptID: data},
	}
	if cfg.SessionValues != nil {
		resumeReq.SessionValues = cfg.SessionValues(sessionRecord)
	}
	events, err := cfg.Invoker.Resume(c.Request.Context(), resumeReq)
	if err != nil {
		cfg.releaseCheckpoint(c.Request.Context(), mapping)
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := cfg.publishAgentEvents(c.Request.Context(), sessionID, runID, events); err != nil {
		cfg.releaseCheckpoint(c.Request.Context(), mapping)
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	mapping, err = cfg.markCheckpointResumeApplied(c.Request.Context(), mapping)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := cfg.completeCheckpoint(c.Request.Context(), mapping); err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := cfg.publishInterruptResolved(c.Request.Context(), sessionID, req.CheckpointID, req.InterruptID); err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, messageResponse{RunID: runID, Status: "completed"})
}

func (cfg Config) enqueueDurableAgentResume(c *gin.Context, sessionRecord session.SessionRecord, mapping session.CheckpointMapping, req resumeAgentRequest) {
	if strings.TrimSpace(mapping.ApprovalID) != "" {
		writeJSONError(c, http.StatusConflict, "approval-bound checkpoint must be resumed through the approval decision endpoint")
		return
	}
	content := resumeContent(req)
	if strings.TrimSpace(content) == "" && req.Data == nil {
		writeJSONError(c, http.StatusBadRequest, "resume content or data is required")
		return
	}
	target := req.Data
	if target == nil {
		target = strings.TrimSpace(req.Content)
	}
	raw, err := json.Marshal(target)
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, "resume data must be JSON serializable")
		return
	}
	epoch := mapping.MappingEpoch
	if epoch <= 0 {
		epoch = 1
	}
	input := sessionruntime.NewInterruptResumeRequested(mapping.ID, epoch, req.CheckpointID, req.InterruptID, content, raw, "")
	messageID := stableRequestID("resume-message", input.InputID)
	runID := sessionruntime.StableRunnerRunID(sessionRecord.ID, input.InputID)
	record, err := schemaMessageRecord(messageID, sessionRecord.ID, runID, schema.UserMessage(content), cfg.Now())
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if postgresSession, ok := cfg.Store.(*session.PostgresStore); ok && cfg.RuntimeStore != nil {
		if _, _, err = postgresSession.AppendMessageAndEnqueue(c.Request.Context(), cfg.RuntimeStore, record, input); err == nil {
			cfg.Runtime.Wake(sessionRecord.ID)
		}
	} else {
		if _, err = cfg.Store.AppendMessage(c.Request.Context(), record); err == nil {
			_, err = cfg.Runtime.Enqueue(c.Request.Context(), sessionRecord.ID, input)
		}
	}
	if err != nil {
		if errors.Is(err, sessionruntime.ErrIdempotencyConflict) || errors.Is(err, session.ErrMessageIdempotencyConflict) {
			writeJSONError(c, http.StatusConflict, err.Error())
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := cfg.Runtime.EnsureSession(context.Background(), sessionRecord.ID); err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusAccepted, messageResponse{RunID: runID, MessageID: messageID, InputID: input.InputID, Status: "accepted"})
}

func (cfg Config) publishInterruptResolved(ctx context.Context, sessionID, checkpointID, interruptID string) error {
	if cfg.Events == nil {
		return nil
	}
	identity := strings.Join([]string{sessionID, checkpointID, interruptID}, "\x00")
	return cfg.Events.Publish(ctx, a2ui.SSEEvent{
		ID: stableRequestID("interrupt-resolved", identity), SessionID: sessionID,
		Event:     a2ui.EventInterruptResolved,
		Payload:   map[string]any{"checkpoint_id": checkpointID, "interrupt_id": interruptID, "status": session.CheckpointStatusResumed},
		CreatedAt: cfg.Now(),
	})
}

// publishAgentEvents 消费 AgentEvent，只发布最新完整 assistant 消息并顺序写入事件 broker。
func (cfg Config) publishAgentEvents(ctx context.Context, sessionID string, runID string, events <-chan AgentEvent) error {
	var assistant strings.Builder
	var latestAssistantEvent *AgentEvent
	chatSurface := newChatA2UISurface(sessionID)
	seq := int64(1)
	for event := range events {
		if event.Event == "" {
			event.Event = a2ui.EventChatDelta
		}
		if event.Err != nil {
			event.Event = a2ui.EventError
			if event.Payload == nil {
				event.Payload = gin.H{"message": event.Err.Error()}
			}
		}
		// Eino 可能把历史完整消息也交给前端；这里只保留最新一条完整 assistant 消息。
		if isCompleteAssistantMessageEvent(event) {
			if event.AssistantText == "" {
				event.AssistantText = event.Message.Content
			}
			latest := event
			latestAssistantEvent = &latest
			continue
		}
		if event.AssistantText != "" {
			assistant.WriteString(event.AssistantText)
		}
		if event.Event == a2ui.EventChatDelta || event.Event == a2ui.EventChatMessage {
			if event.Message != nil {
				if shouldPersistImmediately(event.Message) {
					if _, err := cfg.appendSchemaMessage(ctx, sessionID, runID, event.Message); err != nil {
						return err
					}
				}
			}
			continue
		}
		if !event.ProgressPublished {
			if err := cfg.publishRenderEvents(ctx, sessionID, runID, &seq, chatSurface.eventsFromAgentEvent(event)); err != nil {
				return err
			}
		}
		if event.Message != nil {
			if shouldPersistImmediately(event.Message) {
				if _, err := cfg.appendSchemaMessage(ctx, sessionID, runID, event.Message); err != nil {
					return err
				}
			}
		}
		if event.Err != nil {
			return event.Err
		}
	}

	if latestAssistantEvent != nil {
		rewritten := assistantEventWithA2UIInstanceCardIDs(*latestAssistantEvent, cfg.NewID)
		latestAssistantEvent = &rewritten
		if err := cfg.publishRenderEvents(ctx, sessionID, runID, &seq, chatSurface.eventsFromAgentEvent(*latestAssistantEvent)); err != nil {
			return err
		}
		return cfg.appendAssistantEventMessage(ctx, sessionID, runID, latestAssistantEvent)
	}

	assistantText := assistant.String()
	if assistantText == "" {
		return nil
	}
	if rewritten, ok := contentWithA2UIInstanceCardIDs(assistantText, cfg.NewID); ok {
		assistantText = rewritten
	}
	if err := cfg.publishRenderEvents(ctx, sessionID, runID, &seq, chatSurface.assistantEvents(AgentEvent{
		Event:         a2ui.EventChatDelta,
		AssistantText: assistantText,
	})); err != nil {
		return err
	}
	assistantText = strings.TrimSpace(displayTextWithoutA2UIEnvelope(assistantText))
	if assistantText == "" {
		return nil
	}
	assistantMessage := schema.AssistantMessage(assistantText, nil)
	_, err := cfg.appendSchemaMessage(ctx, sessionID, runID, assistantMessage)
	return err
}

// isCompleteAssistantMessageEvent 判断事件是否是可直接渲染/持久化的 assistant 完整消息。
func isCompleteAssistantMessageEvent(event AgentEvent) bool {
	return event.Message != nil &&
		event.Message.Role == schema.Assistant &&
		len(event.Message.ToolCalls) == 0 &&
		strings.TrimSpace(event.Message.Content) != ""
}

// appendAssistantEventMessage 持久化最新完整 assistant 消息；纯 A2UI JSON 作为结构化历史保留。
func (cfg Config) appendAssistantEventMessage(ctx context.Context, sessionID string, runID string, event *AgentEvent) error {
	if event == nil || event.Message == nil {
		return nil
	}
	content := strings.TrimSpace(event.AssistantText)
	if content == "" {
		content = strings.TrimSpace(event.Message.Content)
	}
	content = displayTextWithoutA2UIEnvelope(content)
	if content == "" {
		return nil
	}
	message := schema.AssistantMessage(content, nil)
	if event.Message.Content == content {
		message = event.Message
	}
	_, err := cfg.appendSchemaMessage(ctx, sessionID, runID, message)
	return err
}

// publishRenderEvents 批量发布内部渲染事件，任一事件失败时停止本轮输出。
func (cfg Config) publishRenderEvents(ctx context.Context, sessionID string, runID string, seq *int64, events []a2ui.RenderEventHint) error {
	for _, event := range events {
		if err := cfg.publishRenderEvent(ctx, sessionID, runID, seq, event); err != nil {
			return err
		}
	}
	return nil
}

// publishRenderEvent 发布单个 A2UI SSE，并在 interrupt 事件到达时保存 checkpoint 映射。
func (cfg Config) publishRenderEvent(ctx context.Context, sessionID string, runID string, seq *int64, event a2ui.RenderEventHint) error {
	if event.Event == "" {
		return nil
	}
	if event.Event == a2ui.EventInterruptRequest {
		if err := cfg.saveInterruptCheckpoint(ctx, sessionID, runID, event.Payload); err != nil {
			_ = cfg.Events.Publish(ctx, a2ui.SSEEvent{
				ID:        cfg.NewID(),
				SessionID: sessionID,
				RunID:     runID,
				Seq:       *seq,
				Event:     a2ui.EventError,
				Payload:   gin.H{"message": err.Error()},
				CreatedAt: cfg.Now(),
			})
			return err
		}
	}
	if err := cfg.Events.Publish(ctx, a2ui.SSEEvent{
		ID:           cfg.NewID(),
		SessionID:    sessionID,
		RunID:        runID,
		Seq:          *seq,
		Event:        event.Event,
		SurfaceID:    event.SurfaceID,
		DataModelKey: event.DataModelKey,
		Payload:      event.Payload,
		CreatedAt:    cfg.Now(),
	}); err != nil {
		return err
	}
	*seq = *seq + 1
	return nil
}

// resumeContent 把恢复请求整理成可写入用户消息历史的文本内容。
func resumeContent(req resumeAgentRequest) string {
	content := strings.TrimSpace(req.Content)
	if content != "" {
		return content
	}
	if req.Data == nil {
		return ""
	}
	raw, err := json.Marshal(req.Data)
	if err != nil {
		return fmt.Sprint(req.Data)
	}
	return string(raw)
}

// shouldPersistImmediately 判断工具消息和带 tool call 的 assistant 消息是否需要立即入库。
func shouldPersistImmediately(message *schema.Message) bool {
	if message == nil {
		return false
	}
	return message.Role == schema.Tool || (message.Role == schema.Assistant && len(message.ToolCalls) > 0)
}

// saveInterruptCheckpoint 从 interrupt payload 中记录 checkpoint 映射，避免重复 resume。
func (cfg Config) saveInterruptCheckpoint(ctx context.Context, sessionID string, runID string, payload any) error {
	if cfg.Checkpoints == nil {
		return nil
	}
	values := payloadMap(payload)
	interruptID := payloadString(values, "interrupt_id")
	if interruptID == "" {
		return nil
	}
	scope := payloadString(values, "scope")
	if scope == "" {
		scope = session.CheckpointScopeRunner
	}
	checkpointID := payloadString(values, "checkpoint_id")
	record := session.CheckpointMapping{
		ID:                checkpointMappingID(sessionID, scope, interruptID),
		SessionID:         sessionID,
		RunID:             runID,
		Scope:             scope,
		InterruptID:       interruptID,
		Status:            session.CheckpointStatusPending,
		SpecVersion:       payloadInt(values, "spec_version"),
		StoryboardVersion: payloadInt(values, "storyboard_version"),
	}
	record.RunnerCheckpointID = checkpointID
	_, err := cfg.Checkpoints.SaveCheckpointMapping(ctx, record)
	return err
}

// markCheckpointResumed 将 checkpoint 映射标记为已恢复，重复提交时可直接返回。
func (cfg Config) markCheckpointResumed(ctx context.Context, sessionID string, interruptID string) {
	if cfg.Checkpoints == nil {
		return
	}
	record, err := cfg.Checkpoints.GetCheckpointMapping(ctx, sessionID, interruptID)
	if err != nil {
		return
	}
	if record.Status == session.CheckpointStatusResumed {
		return
	}
	_, _ = cfg.Checkpoints.MarkCheckpointResumed(ctx, record.ID)
}

func (cfg Config) checkpointForResume(ctx context.Context, sessionID, interruptID, checkpointID, expectedScope string) (session.CheckpointMapping, error) {
	if cfg.Checkpoints == nil {
		return session.CheckpointMapping{}, session.ErrCheckpointNotFound
	}
	record, err := cfg.Checkpoints.GetCheckpointMapping(ctx, strings.TrimSpace(sessionID), strings.TrimSpace(interruptID))
	if err != nil {
		return session.CheckpointMapping{}, err
	}
	if record.SessionID != strings.TrimSpace(sessionID) || record.InterruptID != strings.TrimSpace(interruptID) || record.Scope != expectedScope {
		return session.CheckpointMapping{}, session.ErrCheckpointNotFound
	}
	expectedCheckpointID := record.RunnerCheckpointID
	if strings.TrimSpace(expectedCheckpointID) == "" || strings.TrimSpace(expectedCheckpointID) != strings.TrimSpace(checkpointID) {
		return session.CheckpointMapping{}, session.ErrCheckpointNotFound
	}
	if record.Status != session.CheckpointStatusPending && record.Status != session.CheckpointStatusResumeQueued && record.Status != session.CheckpointStatusResuming && record.Status != session.CheckpointStatusResumeApplied && record.Status != session.CheckpointStatusResumed {
		return session.CheckpointMapping{}, fmt.Errorf("checkpoint status %q cannot be resumed", record.Status)
	}
	return record, nil
}

func (cfg Config) claimCheckpoint(ctx context.Context, record session.CheckpointMapping) (session.CheckpointMapping, error) {
	transition, ok := cfg.Checkpoints.(CheckpointTransitionStore)
	if !ok {
		return record, nil
	}
	epoch := record.MappingEpoch
	if epoch <= 0 {
		epoch = 1
	}
	return transition.TransitionCheckpointMapping(ctx, record.ID, session.CheckpointStatusPending, epoch, session.CheckpointStatusResuming, record.DecisionVersion)
}

func (cfg Config) releaseCheckpoint(ctx context.Context, record session.CheckpointMapping) {
	transition, ok := cfg.Checkpoints.(CheckpointTransitionStore)
	if !ok {
		return
	}
	epoch := record.MappingEpoch
	if epoch <= 0 {
		epoch = 1
	}
	_, _ = transition.TransitionCheckpointMapping(ctx, record.ID, session.CheckpointStatusResuming, epoch, session.CheckpointStatusPending, record.DecisionVersion)
}

func (cfg Config) markCheckpointResumeApplied(ctx context.Context, record session.CheckpointMapping) (session.CheckpointMapping, error) {
	transition, ok := cfg.Checkpoints.(CheckpointTransitionStore)
	if !ok {
		return record, nil
	}
	epoch := record.MappingEpoch
	if epoch <= 0 {
		epoch = 1
	}
	updated, err := transition.TransitionCheckpointMapping(ctx, record.ID, session.CheckpointStatusResuming, epoch, session.CheckpointStatusResumeApplied, record.DecisionVersion)
	if err != nil {
		return session.CheckpointMapping{}, fmt.Errorf("record checkpoint resume receipt: %w", err)
	}
	return updated, nil
}

func (cfg Config) completeCheckpoint(ctx context.Context, record session.CheckpointMapping) error {
	transition, ok := cfg.Checkpoints.(CheckpointTransitionStore)
	if ok {
		epoch := record.MappingEpoch
		if epoch <= 0 {
			epoch = 1
		}
		expectedStatus := record.Status
		if expectedStatus == "" {
			expectedStatus = session.CheckpointStatusResuming
		}
		if _, err := transition.TransitionCheckpointMapping(ctx, record.ID, expectedStatus, epoch, session.CheckpointStatusResumed, record.DecisionVersion); err != nil {
			return fmt.Errorf("complete checkpoint resume: %w", err)
		}
		return nil
	}
	if _, err := cfg.Checkpoints.MarkCheckpointResumed(ctx, record.ID); err != nil {
		return fmt.Errorf("complete checkpoint resume: %w", err)
	}
	return nil
}

// checkpointAlreadyResumed 查询 interrupt 是否已经被恢复过。
func (cfg Config) checkpointAlreadyResumed(ctx context.Context, sessionID string, interruptID string) (bool, error) {
	if cfg.Checkpoints == nil {
		return false, nil
	}
	record, err := cfg.Checkpoints.GetCheckpointMapping(ctx, sessionID, interruptID)
	if err != nil {
		if errors.Is(err, session.ErrCheckpointNotFound) {
			return false, nil
		}
		return false, err
	}
	return record.Status == session.CheckpointStatusResumed, nil
}

// payloadMap 把任意 payload 转成 map，便于从动态协议中读取字段。
func payloadMap(payload any) map[string]any {
	if values, ok := payload.(map[string]any); ok {
		return values
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return map[string]any{}
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return map[string]any{}
	}
	return values
}

// payloadString 从动态字段表中读取并 trim 字符串值。
func payloadString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

// payloadInt 从动态字段表中读取 int/int64/float64 数字值。
func payloadInt(values map[string]any, key string) int {
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

// checkpointMappingID 为 checkpoint 映射生成可重复计算的稳定主键。
func checkpointMappingID(sessionID string, scope string, interruptID string) string {
	parts := []string{"checkpoint", strings.TrimSpace(scope), strings.TrimSpace(sessionID), strings.TrimSpace(interruptID)}
	return strings.Join(parts, ":")
}

// messageMetadataFromRequest 提取不进入 Agent 的前端 UI 生命周期元数据。
func messageMetadataFromRequest(req messageRequest) map[string]any {
	if req.UISource == nil {
		return nil
	}
	sourceType := strings.TrimSpace(req.UISource.Type)
	cardID := strings.TrimSpace(req.UISource.CardID)
	if sourceType != "a2ui_submit" || cardID == "" {
		return nil
	}
	return map[string]any{
		"ui_source": map[string]any{
			"type":    sourceType,
			"card_id": cardID,
		},
	}
}

// appendSchemaMessage 把 Eino 消息转换为会话记录并追加到存储。
func (cfg Config) appendSchemaMessage(ctx context.Context, sessionID string, runID string, message *schema.Message) (session.MessageRecord, error) {
	return cfg.appendSchemaMessageWithMetadata(ctx, sessionID, runID, message, nil)
}

// appendSchemaMessageWithMetadata 写入消息记录附带的 UI 元数据；元数据不会进入 Eino schema.Message。
func (cfg Config) appendSchemaMessageWithMetadata(ctx context.Context, sessionID string, runID string, message *schema.Message, metadata map[string]any) (session.MessageRecord, error) {
	record, err := schemaMessageRecord(cfg.NewID(), sessionID, runID, message, cfg.Now())
	if err != nil {
		return session.MessageRecord{}, err
	}
	record.Metadata = metadata
	return cfg.Store.AppendMessage(ctx, record)
}

// schemaMessageRecord 将 schema.Message 序列化成数据库消息记录。
func schemaMessageRecord(id string, sessionID string, runID string, message *schema.Message, createdAt time.Time) (session.MessageRecord, error) {
	if message == nil {
		return session.MessageRecord{}, fmt.Errorf("schema message is required")
	}
	raw, err := json.Marshal(message)
	if err != nil {
		return session.MessageRecord{}, fmt.Errorf("marshal schema message: %w", err)
	}
	toolCalls, err := marshalToolCalls(message.ToolCalls)
	if err != nil {
		return session.MessageRecord{}, err
	}
	return session.MessageRecord{
		ID:          id,
		SessionID:   sessionID,
		RunID:       runID,
		Role:        string(message.Role),
		Content:     message.Content,
		MessageJSON: raw,
		ToolCalls:   toolCalls,
		ToolCallID:  message.ToolCallID,
		ToolName:    message.ToolName,
		CreatedAt:   createdAt,
	}, nil
}

// ensureSession 校验会话存在，并在失败时直接写 HTTP 错误响应。
func (cfg Config) ensureSession(c *gin.Context, sessionID string) bool {
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return false
	}
	if _, err := cfg.Store.GetSession(c.Request.Context(), sessionID); err != nil {
		writeJSONError(c, http.StatusNotFound, "session not found")
		return false
	}
	return true
}

// recordsToSchemaMessages 将数据库消息窗口还原成 Eino runner 可消费的 schema.Message。
func recordsToSchemaMessages(records []session.MessageRecord) []*schema.Message {
	messages := make([]*schema.Message, 0, len(records))
	for _, record := range records {
		if len(record.MessageJSON) > 0 {
			var message schema.Message
			if err := json.Unmarshal(record.MessageJSON, &message); err == nil {
				messages = append(messages, &message)
				continue
			}
		}

		content := record.Content
		switch schema.RoleType(strings.ToLower(record.Role)) {
		case schema.System:
			messages = append(messages, schema.SystemMessage(content))
		case schema.Assistant:
			toolCalls, err := unmarshalToolCalls(record.ToolCalls)
			if err != nil {
				messages = append(messages, schema.AssistantMessage(content, nil))
				continue
			}
			messages = append(messages, schema.AssistantMessage(content, toolCalls))
		case schema.Tool:
			messages = append(messages, &schema.Message{
				Role:       schema.Tool,
				Content:    content,
				ToolCallID: record.ToolCallID,
				ToolName:   record.ToolName,
			})
		default:
			messages = append(messages, schema.UserMessage(content))
		}
	}
	return messages
}

// marshalToolCalls 序列化 assistant tool calls，供消息历史持久化。
func marshalToolCalls(toolCalls []schema.ToolCall) ([]byte, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(toolCalls)
	if err != nil {
		return nil, fmt.Errorf("marshal tool calls: %w", err)
	}
	return raw, nil
}

// unmarshalToolCalls 反序列化 assistant tool calls，供恢复历史上下文。
func unmarshalToolCalls(raw []byte) ([]schema.ToolCall, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var toolCalls []schema.ToolCall
	if err := json.Unmarshal(raw, &toolCalls); err != nil {
		return nil, fmt.Errorf("unmarshal tool calls: %w", err)
	}
	return toolCalls, nil
}

// prepareSSE 设置 SSE 响应头并立即返回 200 状态。
func prepareSSE(c *gin.Context) {
	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
}

// writeSSE 按 SSE 格式写入已经符合 A2UI 协议的事件。
func writeSSE(c *gin.Context, event a2ui.SSEEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\n", event.Event); err != nil {
		return err
	}
	if strings.TrimSpace(event.ID) != "" {
		if _, err := fmt.Fprintf(c.Writer, "id: %s\n", event.ID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", data); err != nil {
		return err
	}
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

// isClientA2UIEvent 限制 HTTP SSE 只输出新 A2UI 协议事件，旧 worker 事件必须在发布前显式转换。
func isClientA2UIEvent(event string) bool {
	switch strings.TrimSpace(event) {
	case a2ui.EventReady, a2ui.EventAction, a2ui.EventInterruptRequest, a2ui.EventInterruptResolved, a2ui.EventError:
		return true
	default:
		return false
	}
}

func dynamicTarget(aggregate storyboard.StoryboardAggregate, targetID, slotKey string) (storyboard.StoryboardElement, storyboard.AssetSlot, bool) {
	var revision *storyboard.StoryboardRevision
	var err error
	if aggregate.PendingRevisionID != "" {
		revision, err = aggregate.PendingRevision()
	} else {
		revision, err = aggregate.ActiveRevision()
	}
	if err != nil {
		return storyboard.StoryboardElement{}, storyboard.AssetSlot{}, false
	}
	for _, module := range revision.Modules {
		for _, element := range module.Elements {
			if element.ID != strings.TrimSpace(targetID) {
				continue
			}
			if strings.TrimSpace(slotKey) == "" {
				return element, storyboard.AssetSlot{}, true
			}
			for _, slot := range element.AssetSlots {
				if slot.Key == strings.TrimSpace(slotKey) {
					return element, slot, true
				}
			}
			return storyboard.StoryboardElement{}, storyboard.AssetSlot{}, false
		}
	}
	return storyboard.StoryboardElement{}, storyboard.AssetSlot{}, false
}

func promptForDynamicSlot(element storyboard.StoryboardElement, slot storyboard.AssetSlot) (string, int) {
	for _, prompt := range element.PromptSlots {
		if prompt.Purpose == slot.Key || prompt.Purpose == slot.Role || prompt.Purpose == slot.MediaKind || strings.Contains(slot.Key, prompt.Purpose) {
			return prompt.Prompt, prompt.Revision
		}
	}
	if len(element.PromptSlots) == 1 {
		return element.PromptSlots[0].Prompt, element.PromptSlots[0].Revision
	}
	return "", 0
}

func providerForMediaKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "video":
		return generation.ProviderSeedance
	case "audio", "music", "voice":
		return generation.ProviderAudio
	case "image", "illustration", "keyframe":
		return generation.ProviderImage2
	default:
		return ""
	}
}

func localGenerationPayload(input storyboard.GenerationInput, element storyboard.StoryboardElement, slot storyboard.AssetSlot, userID string) map[string]any {
	payload := map[string]any{
		"prompt": input.Prompt, "target": element.Content, "media_kind": slot.MediaKind,
		"bind_asset_ids": input.InputAssetIDs, "user_id": userID,
	}
	for _, source := range []map[string]any{element.Content, element.Metadata} {
		for _, key := range []string{"model", "size", "n", "ratio", "resolution", "duration_seconds", "fps", "filename_prefix"} {
			if value, exists := source[key]; exists && strings.TrimSpace(fmt.Sprint(value)) != "" {
				payload[key] = value
			}
		}
	}
	return payload
}

func valueOr(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func writeStoryboardCommandError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, storyboard.ErrAggregateNotFound), errors.Is(err, storyboard.ErrTargetNotFound), errors.Is(err, storyboard.ErrSlotNotFound), errors.Is(err, storyboard.ErrRevisionNotFound):
		writeJSONError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, storyboard.ErrVersionConflict), errors.Is(err, storyboard.ErrRevisionMismatch), errors.Is(err, storyboard.ErrPendingRevision), errors.Is(err, storyboard.ErrNoPendingRevision), errors.Is(err, storyboard.ErrDependencyNotReady), errors.Is(err, storyboard.ErrIdempotencyConflict), errors.Is(err, storyboard.ErrDuplicateCommand):
		writeJSONError(c, http.StatusConflict, err.Error())
	case errors.Is(err, storyboard.ErrInvalidMutation):
		writeJSONError(c, http.StatusBadRequest, err.Error())
	default:
		writeJSONError(c, http.StatusInternalServerError, err.Error())
	}
}

// writeJSONError 以统一结构返回 HTTP JSON 错误。
func writeJSONError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

// randomID 生成 128-bit 随机 ID，随机源失败时降级为时间戳字符串。
func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

func stableMessageID(sessionID, idempotencyKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionID) + "\x00" + strings.TrimSpace(idempotencyKey)))
	return "msg_" + hex.EncodeToString(sum[:16])
}

func stableRequestID(kind, identity string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(kind) + "\x00" + strings.TrimSpace(identity)))
	return strings.TrimSpace(kind) + ":" + hex.EncodeToString(sum[:16])
}

// candidateApprovalBatchStoryboardEventID hashes unbounded domain identifiers
// into a stable ASCII key that fits aigc_session_event_log.event_id VARCHAR(128).
func candidateApprovalBatchStoryboardEventID(batchID, storyboardID string, version int) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(batchID) + "\x00" + strings.TrimSpace(storyboardID) + "\x00" + strconv.Itoa(version)))
	return "candidate-batch-storyboard:" + hex.EncodeToString(sum[:])
}
