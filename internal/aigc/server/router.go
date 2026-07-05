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

type SessionStore interface {
	SaveSession(ctx context.Context, record session.SessionRecord) error
	GetSession(ctx context.Context, sessionID string) (session.SessionRecord, error)
	AppendMessage(ctx context.Context, record session.MessageRecord) (session.MessageRecord, error)
	ListMessages(ctx context.Context, sessionID string, window session.MessageWindow) ([]session.MessageRecord, error)
}

type AgentInvoker interface {
	Invoke(ctx context.Context, req AgentInvokeRequest) (<-chan AgentEvent, error)
	Resume(ctx context.Context, req AgentResumeRequest) (<-chan AgentEvent, error)
}

type AgentInvokeRequest struct {
	Messages      []*schema.Message
	CheckpointID  string
	SessionValues map[string]any
}

type AgentResumeRequest struct {
	CheckpointID  string
	Targets       map[string]any
	SessionValues map[string]any
}

type SkillStore interface {
	Save(ctx context.Context, record skill.SkillRecord) error
	Get(ctx context.Context, skillID string) (skill.SkillRecord, error)
	ListEnabled(ctx context.Context) ([]skill.SkillRecord, error)
}

type StoryboardStore interface {
	Get(ctx context.Context, storyboardID string) (storyboard.Storyboard, error)
	GetLatestBySession(ctx context.Context, sessionID string) (storyboard.Storyboard, error)
	ApplyPatch(ctx context.Context, req storyboard.PatchRequest) (storyboard.Storyboard, storyboard.EventRecord, error)
}

type AssetStore interface {
	Save(ctx context.Context, record asset.Asset) (asset.Asset, error)
	Get(ctx context.Context, assetID string) (asset.Asset, error)
	ListBySession(ctx context.Context, sessionID string) ([]asset.Asset, error)
}

type GenerationJobStore interface {
	ListBySession(ctx context.Context, sessionID string) ([]generation.GenerationJob, error)
}

type CheckpointStore interface {
	SaveCheckpointMapping(ctx context.Context, record session.CheckpointMapping) (session.CheckpointMapping, error)
	GetCheckpointMapping(ctx context.Context, sessionID string, interruptID string) (session.CheckpointMapping, error)
	MarkCheckpointResumed(ctx context.Context, id string) (session.CheckpointMapping, error)
}

type AssetUploader interface {
	Upload(ctx context.Context, input asset.UploadInput) (asset.UploadResult, error)
}

type MediaGraphResumer interface {
	Resume(ctx context.Context, checkpointID string, interruptID string, decision mediagraph.ReferenceConfirmDecision) (mediagraph.RunResult, error)
}

type AgentEvent struct {
	Event         string
	SurfaceID     string
	DataModelKey  string
	Payload       any
	AssistantText string
	Message       *schema.Message
	Err           error
}

type Config struct {
	Store          SessionStore
	Skills         SkillStore
	Storyboards    StoryboardStore
	Assets         AssetStore
	GenerationJobs GenerationJobStore
	AssetUploader  AssetUploader
	Events         a2ui.EventSubscriber
	Checkpoints    CheckpointStore
	Invoker        AgentInvoker
	MediaGraph     MediaGraphResumer
	SessionValues  func(session.SessionRecord) map[string]any
	MessageWindow  session.MessageWindow
	NewID          func() string
	Now            func() time.Time
}

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
	router.POST("/api/aigc/sessions/:session_id/messages/stream", cfg.streamMessage)
	router.POST("/api/aigc/sessions/:session_id/messages/resume/stream", cfg.resumeAgent)

	return router
}

type createSessionRequest struct {
	UserID  string `json:"user_id"`
	SkillID string `json:"skill_id"`
	Title   string `json:"title"`
}

type createSkillRequest struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Content string `json:"content"`
	Enabled *bool  `json:"enabled"`
}

type bindSkillToSessionRequest struct {
	SkillID string `json:"skill_id"`
}

type createSkillResponse struct {
	Skill skill.SkillRecord `json:"skill"`
	Plan  skill.SkillPlan   `json:"plan"`
}

type listSkillsResponse struct {
	Skills []skill.SkillRecord `json:"skills"`
}

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

type streamMessageRequest struct {
	Content string `json:"content"`
}

type patchStoryboardRequest struct {
	BaseVersion int                     `json:"base_version"`
	Source      string                  `json:"source"`
	ToolCallID  string                  `json:"tool_call_id"`
	Ops         []aigctools.JSONPatchOp `json:"ops"`
}

type bindAssetRequest struct {
	BaseVersion int    `json:"base_version"`
	TargetType  string `json:"target_type"`
	TargetID    string `json:"target_id"`
	Field       string `json:"field,omitempty"`
	Source      string `json:"source,omitempty"`
	ToolCallID  string `json:"tool_call_id,omitempty"`
}

type patchStoryboardResponse struct {
	Storyboard storyboard.Storyboard       `json:"storyboard"`
	Patch      a2ui.StoryboardPatchPayload `json:"patch"`
}

type resumeMediaGraphRequest struct {
	CheckpointID string `json:"checkpoint_id"`
	InterruptID  string `json:"interrupt_id"`
	Approved     *bool  `json:"approved"`
	Note         string `json:"note"`
}

type resumeAgentRequest struct {
	CheckpointID string `json:"checkpoint_id"`
	InterruptID  string `json:"interrupt_id"`
	Content      string `json:"content"`
	Data         any    `json:"data"`
}

type resumeMediaGraphResponse struct {
	Status  string                          `json:"status"`
	Event   string                          `json:"event,omitempty"`
	Payload any                             `json:"payload,omitempty"`
	Output  mediagraph.MediaGeneratorOutput `json:"output,omitempty"`
}

type listSessionGenerationJobsResponse struct {
	Jobs []generation.GenerationJob `json:"jobs"`
}

type listSessionMessagesResponse struct {
	Messages []session.MessageRecord `json:"messages"`
}

type listSessionAssetsResponse struct {
	Assets []asset.Asset `json:"assets"`
}

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
			if err := writeSSE(c, event); err != nil {
				return
			}
			if c.Query("once") == "1" {
				return
			}
		}
	}
}

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
		event := result.Interrupt.Event
		if event == "" {
			event = a2ui.EventInterruptRequest
		}
		payload := payloadMap(result.Interrupt.Payload)
		payload["scope"] = session.CheckpointScopeMediaGraph
		c.JSON(http.StatusOK, resumeMediaGraphResponse{
			Status:  "interrupted",
			Event:   event,
			Payload: payload,
		})
		return
	}

	cfg.markCheckpointResumed(c.Request.Context(), sessionID, req.InterruptID)
	c.JSON(http.StatusOK, resumeMediaGraphResponse{
		Status: "completed",
		Output: result.Output,
	})
}

func (cfg Config) streamMessage(c *gin.Context) {
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return
	}
	if cfg.Invoker == nil {
		writeJSONError(c, http.StatusInternalServerError, "agent invoker is not configured")
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

	var req streamMessageRequest
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
	userRecord, err := cfg.appendSchemaMessage(c.Request.Context(), sessionID, runID, userMessage)
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

	cfg.streamAgentEvents(c, sessionID, runID, events)
}

func (cfg Config) resumeAgent(c *gin.Context) {
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return
	}
	if cfg.Invoker == nil {
		writeJSONError(c, http.StatusInternalServerError, "agent invoker is not configured")
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
		prepareSSE(c)
		_ = writeSSE(c, a2ui.SSEEvent{
			ID:        cfg.NewID(),
			SessionID: sessionID,
			Event:     a2ui.EventSurfaceUpdate,
			Payload: gin.H{
				"checkpoint_id": req.CheckpointID,
				"interrupt_id":  req.InterruptID,
				"status":        session.CheckpointStatusResumed,
				"message":       "确认已处理。",
			},
			CreatedAt: cfg.Now(),
		})
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

	cfg.streamAgentEvents(c, sessionID, runID, events)
	cfg.markCheckpointResumed(c.Request.Context(), sessionID, req.InterruptID)
}

func (cfg Config) streamAgentEvents(c *gin.Context, sessionID string, runID string, events <-chan AgentEvent) {
	prepareSSE(c)
	var assistant strings.Builder
	var assistantMessages []*schema.Message
	chatSurface := newChatA2UISurface(sessionID)
	seq := int64(1)
	if !cfg.writeRenderEvent(c, sessionID, runID, &seq, chatSurface.beginEvent()) {
		return
	}
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
		if event.AssistantText != "" {
			assistant.WriteString(event.AssistantText)
		}
		if !cfg.writeRenderEvents(c, sessionID, runID, &seq, chatSurface.eventsFromAgentEvent(event)) {
			return
		}
		if event.Message != nil {
			if !cfg.writeRenderEvents(c, sessionID, runID, &seq, cfg.renderEventsFromToolMessage(c.Request.Context(), sessionID, event.Message)) {
				return
			}
			if shouldPersistImmediately(event.Message) {
				if _, err := cfg.appendSchemaMessage(c.Request.Context(), sessionID, runID, event.Message); err != nil {
					_ = writeSSE(c, a2ui.SSEEvent{
						ID:        cfg.NewID(),
						SessionID: sessionID,
						RunID:     runID,
						Seq:       seq,
						Event:     a2ui.EventError,
						Payload:   gin.H{"message": err.Error()},
						CreatedAt: cfg.Now(),
					})
					return
				}
			} else if event.Message.Role == schema.Assistant && len(event.Message.ToolCalls) == 0 {
				assistantMessages = append(assistantMessages, event.Message)
			}
		}
		if event.Err != nil {
			return
		}
	}

	assistantText := assistant.String()
	if assistantText == "" {
		return
	}
	assistantText = displayTextWithoutA2UIEnvelope(assistantText)
	if assistantText == "" {
		return
	}
	assistantMessage := schema.AssistantMessage(assistantText, nil)
	if len(assistantMessages) == 1 && assistantMessages[0].Content == assistantText {
		assistantMessage = assistantMessages[0]
	}
	if _, err := cfg.appendSchemaMessage(c.Request.Context(), sessionID, runID, assistantMessage); err != nil {
		_ = writeSSE(c, a2ui.SSEEvent{
			ID:        cfg.NewID(),
			SessionID: sessionID,
			RunID:     runID,
			Seq:       seq,
			Event:     a2ui.EventError,
			Payload:   gin.H{"message": err.Error()},
			CreatedAt: cfg.Now(),
		})
	}
}

func (cfg Config) writeRenderEvents(c *gin.Context, sessionID string, runID string, seq *int64, events []aigctools.RenderEventHint) bool {
	for _, event := range events {
		if !cfg.writeRenderEvent(c, sessionID, runID, seq, event) {
			return false
		}
	}
	return true
}

func (cfg Config) writeRenderEvent(c *gin.Context, sessionID string, runID string, seq *int64, event aigctools.RenderEventHint) bool {
	if event.Event == "" {
		return true
	}
	if event.Event == a2ui.EventInterruptRequest {
		if err := cfg.saveInterruptCheckpoint(c.Request.Context(), sessionID, runID, event.Payload); err != nil {
			_ = writeSSE(c, a2ui.SSEEvent{
				ID:        cfg.NewID(),
				SessionID: sessionID,
				RunID:     runID,
				Seq:       *seq,
				Event:     a2ui.EventError,
				Payload:   gin.H{"message": err.Error()},
				CreatedAt: cfg.Now(),
			})
			return false
		}
	}
	if err := writeSSE(c, a2ui.SSEEvent{
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
		return false
	}
	*seq = *seq + 1
	return true
}

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

func shouldPersistImmediately(message *schema.Message) bool {
	if message == nil {
		return false
	}
	return message.Role == schema.Tool || (message.Role == schema.Assistant && len(message.ToolCalls) > 0)
}

func renderEventsFromToolMessage(message *schema.Message) []aigctools.RenderEventHint {
	if message == nil || message.Role != schema.Tool || strings.TrimSpace(message.Content) == "" {
		return nil
	}
	var result struct {
		Data struct {
			RenderEvents []aigctools.RenderEventHint `json:"render_events"`
			Interrupt    *mediagraph.InterruptEvent  `json:"interrupt"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
		return nil
	}
	events := append([]aigctools.RenderEventHint(nil), result.Data.RenderEvents...)
	if result.Data.Interrupt != nil && result.Data.Interrupt.Event != "" {
		payload := map[string]any{}
		raw, err := json.Marshal(result.Data.Interrupt.Payload)
		if err == nil {
			_ = json.Unmarshal(raw, &payload)
		}
		payload["scope"] = "media_graph"
		events = append(events, aigctools.RenderEventHint{
			Event:        result.Data.Interrupt.Event,
			SurfaceID:    "storyboard",
			DataModelKey: "interrupt",
			Payload:      payload,
		})
	}
	return events
}

func (cfg Config) renderEventsFromToolMessage(ctx context.Context, sessionID string, message *schema.Message) []aigctools.RenderEventHint {
	events := renderEventsFromToolMessage(message)
	if len(events) == 0 || cfg.Storyboards == nil {
		return events
	}
	out := make([]aigctools.RenderEventHint, 0, len(events))
	for _, event := range events {
		if materialized, ok := cfg.materializeStoryboardUpdateEvent(ctx, sessionID, message, event); ok {
			out = append(out, materialized)
			continue
		}
		out = append(out, event)
	}
	return out
}

func (cfg Config) materializeStoryboardUpdateEvent(ctx context.Context, sessionID string, message *schema.Message, event aigctools.RenderEventHint) (aigctools.RenderEventHint, bool) {
	if event.Event != a2ui.EventStoryboardPatch {
		return aigctools.RenderEventHint{}, false
	}
	values := payloadMap(event.Payload)
	if _, hasOps := values["ops"]; hasOps {
		return aigctools.RenderEventHint{}, false
	}
	updates, ok := storyboardUpdatesFromPayload(values["updates"])
	if !ok || len(updates) == 0 {
		return aigctools.RenderEventHint{}, false
	}

	board, err := storyboardForUpdateEvent(ctx, cfg.Storyboards, sessionID, values)
	if err != nil {
		return aigctools.RenderEventHint{}, false
	}
	ops := make([]aigctools.JSONPatchOp, 0, len(updates)*2)
	for _, update := range updates {
		for _, assetID := range update.AssetIDs {
			bindingOps, err := storyboard.AssetBindingOps(board, storyboard.AssetBindingRequest{
				AssetID:    assetID,
				AssetKind:  update.AssetKind,
				TargetType: update.TargetType,
				TargetID:   update.TargetID,
				Field:      update.Field,
			})
			if err != nil {
				return aigctools.RenderEventHint{}, false
			}
			ops = append(ops, bindingOps...)
		}
	}
	if len(ops) == 0 {
		return aigctools.RenderEventHint{}, false
	}

	source := payloadString(values, "source")
	if source == "" {
		source = "tool"
	}
	patched, patchEvent, err := cfg.Storyboards.ApplyPatch(ctx, storyboard.PatchRequest{
		EventID:      cfg.NewID(),
		SessionID:    sessionID,
		StoryboardID: board.ID,
		BaseVersion:  board.Version,
		Source:       source,
		ToolCallID:   strings.TrimSpace(message.ToolCallID),
		Ops:          ops,
	})
	if err != nil {
		return aigctools.RenderEventHint{}, false
	}
	if patchEvent.StoryboardID == "" {
		patchEvent.StoryboardID = patched.ID
	}
	if patchEvent.BaseVersion == 0 {
		patchEvent.BaseVersion = board.Version
	}
	if patchEvent.NextVersion == 0 {
		patchEvent.NextVersion = patched.Version
	}
	if len(patchEvent.Ops) == 0 {
		patchEvent.Ops = ops
	}
	if patchEvent.Source == "" {
		patchEvent.Source = source
	}
	if patchEvent.ToolCallID == "" {
		patchEvent.ToolCallID = strings.TrimSpace(message.ToolCallID)
	}

	return aigctools.RenderEventHint{
		Event:        a2ui.EventStoryboardPatch,
		SurfaceID:    "storyboard",
		DataModelKey: "storyboard",
		Payload: a2ui.StoryboardPatchPayload{
			StoryboardID: patchEvent.StoryboardID,
			BaseVersion:  patchEvent.BaseVersion,
			NextVersion:  patchEvent.NextVersion,
			Ops:          append([]aigctools.JSONPatchOp(nil), patchEvent.Ops...),
			Source:       patchEvent.Source,
			ToolCallID:   patchEvent.ToolCallID,
		},
	}, true
}

func storyboardForUpdateEvent(ctx context.Context, store StoryboardStore, sessionID string, values map[string]any) (storyboard.Storyboard, error) {
	storyboardID := payloadString(values, "storyboard_id")
	if storyboardID != "" {
		return store.Get(ctx, storyboardID)
	}
	return store.GetLatestBySession(ctx, sessionID)
}

func storyboardUpdatesFromPayload(value any) ([]aigctools.StoryboardUpdateHint, bool) {
	if value == nil {
		return nil, false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var updates []aigctools.StoryboardUpdateHint
	if err := json.Unmarshal(raw, &updates); err != nil {
		return nil, false
	}
	return updates, len(updates) > 0
}

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

func payloadString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

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

func checkpointMappingID(sessionID string, scope string, interruptID string) string {
	parts := []string{"checkpoint", strings.TrimSpace(scope), strings.TrimSpace(sessionID), strings.TrimSpace(interruptID)}
	return strings.Join(parts, ":")
}

func (cfg Config) appendSchemaMessage(ctx context.Context, sessionID string, runID string, message *schema.Message) (session.MessageRecord, error) {
	record, err := schemaMessageRecord(cfg.NewID(), sessionID, runID, message, cfg.Now())
	if err != nil {
		return session.MessageRecord{}, err
	}
	return cfg.Store.AppendMessage(ctx, record)
}

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

func prepareSSE(c *gin.Context) {
	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
}

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

func writeJSONError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

func randomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}
