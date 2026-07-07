package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
)

// FinalVideoSpecReader 是文档展示所需的最小只读能力（spec.PostgresStore 满足）。
type FinalVideoSpecReader interface {
	GetLatestBySession(ctx context.Context, sessionID string) (spec.FinalVideoSpec, error)
}

// sessionSkillResponse 是 GET /skill 的具名响应结构（对齐仓库惯例）。
type sessionSkillResponse struct {
	Bound   bool   `json:"bound"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
}

// getSessionSpec 返回本会话最新 Final Video Spec（含 Markdown 原文）。
func (cfg Config) getSessionSpec(c *gin.Context) {
	if cfg.Specs == nil {
		writeJSONError(c, http.StatusInternalServerError, "spec store is not configured")
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
	got, err := cfg.Specs.GetLatestBySession(c.Request.Context(), sessionID)
	if err != nil {
		if errors.Is(err, spec.ErrNotFound) {
			writeJSONError(c, http.StatusNotFound, "spec not found")
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, got)
}

// getSessionSkill 返回本会话当前绑定的 Skill 原文（skill.md）；未绑返回 {bound:false}。
func (cfg Config) getSessionSkill(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	// ensureSession 已正确区分 nil-store→500 / not-found→404。
	if !cfg.ensureSession(c, sessionID) {
		return
	}
	record, err := cfg.Store.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	if record.SkillID == "" || cfg.Skills == nil {
		c.JSON(http.StatusOK, sessionSkillResponse{Bound: false})
		return
	}
	skillRecord, err := cfg.Skills.Get(c.Request.Context(), record.SkillID)
	if err != nil {
		if errors.Is(err, skill.ErrSkillNotFound) {
			c.JSON(http.StatusOK, sessionSkillResponse{Bound: false})
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 有意：展示会话实际绑定的 skill 原文，即使该 skill 已被禁用（文档 tab 反映真实绑定，不做 Enabled 过滤）。
	c.JSON(http.StatusOK, sessionSkillResponse{
		Bound:   true,
		ID:      skillRecord.ID,
		Name:    skillRecord.Name,
		Content: skillRecord.Content,
	})
}
