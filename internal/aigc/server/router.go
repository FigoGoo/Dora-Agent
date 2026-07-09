package server

import (
	"bytes"
	"context"
	"crypto/rand"
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
	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/mediagraph"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
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

// CheckpointStore 定义 interrupt checkpoint 的映射和恢复状态更新能力。
type CheckpointStore interface {
	SaveCheckpointMapping(ctx context.Context, record session.CheckpointMapping) (session.CheckpointMapping, error)
	GetCheckpointMapping(ctx context.Context, sessionID string, interruptID string) (session.CheckpointMapping, error)
	MarkCheckpointResumed(ctx context.Context, id string) (session.CheckpointMapping, error)
}

// AssetUploader 定义上传二进制素材到对象存储的能力。
type AssetUploader interface {
	Upload(ctx context.Context, input asset.UploadInput) (asset.UploadResult, error)
}

// MediaGraphResumer 定义媒体生成图在人审确认后继续运行的能力。
type MediaGraphResumer interface {
	Resume(ctx context.Context, checkpointID string, interruptID string, decision mediagraph.ReferenceConfirmDecision) (mediagraph.RunResult, error)
}

// AgentEvent 是后端 Agent runner 推给 HTTP/SSE 层的统一事件。
type AgentEvent struct {
	Event         string
	SurfaceID     string
	DataModelKey  string
	Payload       any
	AssistantText string
	Message       *schema.Message
	Err           error
}

// Config 汇总 AIGC HTTP 路由依赖，方便测试替换存储、Agent 和事件 broker。
type Config struct {
	Store          SessionStore
	Skills         SkillStore
	Storyboards    StoryboardStore
	Assets         AssetStore
	GenerationJobs GenerationJobStore
	AssetUploader  AssetUploader
	Events         a2ui.EventBroker
	Checkpoints    CheckpointStore
	Invoker        AgentInvoker
	MediaGraph     MediaGraphResumer
	SessionValues  func(session.SessionRecord) map[string]any
	MessageWindow  session.MessageWindow
	NewID          func() string
	Now            func() time.Time
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

	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
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
	router.GET("/api/aigc/sessions/:session_id/storyboard", cfg.getSessionStoryboard)
	router.PATCH("/api/aigc/sessions/:session_id/storyboards/:storyboard_id", cfg.patchStoryboard)
	router.POST("/api/aigc/sessions/:session_id/storyboards/:storyboard_id/assets/:asset_id/bind", cfg.bindAssetToStoryboard)
	router.POST("/api/aigc/sessions/:session_id/media-graph/resume", cfg.resumeMediaGraph)
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
	record := session.SessionRecord{
		ID:        cfg.NewID(),
		UserID:    strings.TrimSpace(req.UserID),
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
	userID := strings.TrimSpace(c.PostForm("user_id"))
	if userID == "" {
		userID = sessionRecord.UserID
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
	if assetID == "" {
		writeJSONError(c, http.StatusBadRequest, "asset id is required")
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
	Content  string           `json:"content,omitempty"`
	UISource *messageUISource `json:"ui_source,omitempty"`
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

// patchStoryboardResponse 返回 patch 后的故事板和可广播的 A2UI patch payload。
type patchStoryboardResponse struct {
	Storyboard storyboard.Storyboard       `json:"storyboard"`
	Patch      a2ui.StoryboardPatchPayload `json:"patch"`
}

// resumeMediaGraphRequest 是媒体生成图等待人审确认后的恢复请求。
type resumeMediaGraphRequest struct {
	CheckpointID string `json:"checkpoint_id"`
	InterruptID  string `json:"interrupt_id"`
	Approved     *bool  `json:"approved"`
	Note         string `json:"note"`
}

// resumeAgentRequest 是 Agent interrupt 被用户输入恢复时的请求体。
type resumeAgentRequest struct {
	CheckpointID string `json:"checkpoint_id"`
	InterruptID  string `json:"interrupt_id"`
	Content      string `json:"content"`
	Data         any    `json:"data"`
}

// resumeMediaGraphResponse 只返回媒体图恢复状态；后续 A2UI 事件统一从 /events/stream 下发。
type resumeMediaGraphResponse struct {
	Status string                          `json:"status"`
	Output mediagraph.MediaGeneratorOutput `json:"output,omitempty"`
}

// messageResponse 是提交用户输入后的同步确认；实际 A2UI 输出只从 /events/stream 下发。
type messageResponse struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

// listSessionGenerationJobsResponse 返回会话内生成任务列表。
type listSessionGenerationJobsResponse struct {
	Jobs []generation.GenerationJob `json:"jobs"`
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
	c.JSON(http.StatusOK, listSessionAssetsResponse{Assets: records})
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
	c.JSON(http.StatusOK, listSessionGenerationJobsResponse{Jobs: jobs})
}

// streamSessionEvents 订阅会话级 broker，把后台事件持续写成 SSE。
func (cfg Config) streamSessionEvents(c *gin.Context) {
	if cfg.Events == nil {
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

// getSessionStoryboard 返回会话最新故事板，未创建时返回 204。
func (cfg Config) getSessionStoryboard(c *gin.Context) {
	if cfg.Storyboards == nil {
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

// resumeMediaGraph 恢复媒体生成图的人审节点，并处理可能出现的后续 interrupt。
func (cfg Config) resumeMediaGraph(c *gin.Context) {
	if cfg.MediaGraph == nil {
		writeJSONError(c, http.StatusInternalServerError, "media graph is not configured")
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

	var req resumeMediaGraphRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, "invalid media graph resume request")
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
	alreadyResumed, err := cfg.checkpointAlreadyResumed(c.Request.Context(), sessionID, req.InterruptID)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if alreadyResumed {
		c.JSON(http.StatusOK, resumeMediaGraphResponse{Status: session.CheckpointStatusResumed})
		return
	}
	if req.Approved == nil {
		writeJSONError(c, http.StatusBadRequest, "approved is required")
		return
	}

	result, err := cfg.MediaGraph.Resume(c.Request.Context(), req.CheckpointID, req.InterruptID, mediagraph.ReferenceConfirmDecision{
		Approved: *req.Approved,
		Note:     strings.TrimSpace(req.Note),
	})
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if result.Interrupted && result.Interrupt != nil {
		if cfg.Events == nil {
			writeJSONError(c, http.StatusInternalServerError, "event broker is not configured")
			return
		}
		event := result.Interrupt.Event
		if event == "" {
			event = a2ui.EventInterruptRequest
		}
		payload := payloadMap(result.Interrupt.Payload)
		payload["scope"] = session.CheckpointScopeMediaGraph
		if err := cfg.Events.Publish(c.Request.Context(), a2ui.SSEEvent{
			ID:        cfg.NewID(),
			SessionID: sessionID,
			Event:     event,
			Payload:   payload,
			CreatedAt: cfg.Now(),
		}); err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		c.JSON(http.StatusOK, resumeMediaGraphResponse{
			Status: "interrupted",
		})
		return
	}

	cfg.markCheckpointResumed(c.Request.Context(), sessionID, req.InterruptID)
	c.JSON(http.StatusOK, resumeMediaGraphResponse{
		Status: "completed",
		Output: result.Output,
	})
}

// createMessage 写入用户消息并触发 Agent；A2UI 输出统一发布到 /events/stream。
func (cfg Config) createMessage(c *gin.Context) {
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

	runID := cfg.NewID()
	userMessage := schema.UserMessage(content)
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
	alreadyResumed, err := cfg.checkpointAlreadyResumed(c.Request.Context(), sessionID, req.InterruptID)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if alreadyResumed {
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
		c.JSON(http.StatusOK, messageResponse{RunID: runID, Status: "already_resumed"})
		return
	}

	runID := cfg.NewID()
	if content := resumeContent(req); content != "" {
		if _, err := cfg.appendSchemaMessage(c.Request.Context(), sessionID, runID, schema.UserMessage(content)); err != nil {
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
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}

	if err := cfg.publishAgentEvents(c.Request.Context(), sessionID, runID, events); err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	cfg.markCheckpointResumed(c.Request.Context(), sessionID, req.InterruptID)
	c.JSON(http.StatusOK, messageResponse{RunID: runID, Status: "completed"})
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
		if err := cfg.publishRenderEvents(ctx, sessionID, runID, &seq, chatSurface.eventsFromAgentEvent(event)); err != nil {
			return err
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
	if scope == session.CheckpointScopeMediaGraph {
		record.GraphCheckpointID = checkpointID
		record.GraphName = "media_generator"
	} else {
		record.RunnerCheckpointID = checkpointID
	}
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
	case a2ui.EventReady, a2ui.EventAction, a2ui.EventInterruptRequest, a2ui.EventError:
		return true
	default:
		return false
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
