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
	if cfg.Store == nil {
		writeJSONError(c, http.StatusInternalServerError, "session store is not configured")
		return
	}
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" {
		writeJSONError(c, http.StatusBadRequest, "session id is required")
		return
	}
	record, err := cfg.Store.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		writeJSONError(c, http.StatusNotFound, "session not found")
		return
	}
	if record.SkillID == "" || cfg.Skills == nil {
		c.JSON(http.StatusOK, gin.H{"bound": false})
		return
	}
	skillRecord, err := cfg.Skills.Get(c.Request.Context(), record.SkillID)
	if err != nil {
		if errors.Is(err, skill.ErrSkillNotFound) {
			c.JSON(http.StatusOK, gin.H{"bound": false})
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"bound":   true,
		"id":      skillRecord.ID,
		"name":    skillRecord.Name,
		"content": skillRecord.Content,
	})
}
