package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type generatedMediaJob struct {
	JobID      string `json:"job_id"`
	Provider   string `json:"provider"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Status     string `json:"status"`
	Created    bool   `json:"created"`
}

type generateSessionMediaResponse struct {
	StoryboardID string              `json:"storyboard_id"`
	Dispatched   int                 `json:"dispatched"`
	Reused       int                 `json:"reused"`
	Skipped      int                 `json:"skipped"`
	Jobs         []generatedMediaJob `json:"jobs"`
}

type mediaTarget struct {
	provider   string
	targetType string
	targetID   string
	mediaKind  string
	field      string
	filled     bool // already has a bound asset → skip
}

// mediaTargetsForBoard enumerates every media target of a storyboard: one image
// job per key element, one video job per shot, one audio job per audio layer.
// Targets that already have a bound asset are marked filled so the endpoint skips
// them (making the button idempotent by state — re-clicking only fills gaps).
func mediaTargetsForBoard(board storyboard.Storyboard) []mediaTarget {
	var out []mediaTarget
	for _, ke := range board.KeyElements {
		if strings.TrimSpace(ke.Key) == "" {
			continue
		}
		out = append(out, mediaTarget{
			provider:   generation.ProviderImage2,
			targetType: generation.TargetKeyElement,
			targetID:   ke.Key,
			mediaKind:  "image",
			filled:     len(ke.AssetIDs) > 0,
		})
	}
	for _, sh := range board.Shots {
		if strings.TrimSpace(sh.ShotID) == "" {
			continue
		}
		out = append(out, mediaTarget{
			provider:   generation.ProviderSeedance,
			targetType: generation.TargetShot,
			targetID:   sh.ShotID,
			mediaKind:  "video",
			field:      "video_asset_id",
			filled:     strings.TrimSpace(sh.VideoAssetID) != "",
		})
	}
	for _, al := range board.AudioLayers {
		if strings.TrimSpace(al.LayerID) == "" {
			continue
		}
		out = append(out, mediaTarget{
			provider:   generation.ProviderAudio,
			targetType: generation.TargetAudioLayer,
			targetID:   al.LayerID,
			mediaKind:  "audio",
			filled:     strings.TrimSpace(al.AssetID) != "",
		})
	}
	return out
}

// generateSessionMedia dispatches media generation jobs for every not-yet-filled
// target of the session's latest storyboard, bypassing the model's stochastic
// media_generator tool calls. The async worker processes the jobs and binds the
// resulting assets onto the storyboard, streaming job.status/storyboard.patch
// events over SSE.
func (cfg Config) generateSessionMedia(c *gin.Context) {
	if cfg.MediaDispatcher == nil {
		writeJSONError(c, http.StatusInternalServerError, "media dispatcher is not configured")
		return
	}
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
			writeJSONError(c, http.StatusNotFound, "storyboard not found for session")
			return
		}
		writeJSONError(c, http.StatusInternalServerError, err.Error())
		return
	}

	resp := generateSessionMediaResponse{StoryboardID: board.ID}
	for _, t := range mediaTargetsForBoard(board) {
		if t.filled {
			resp.Skipped++
			continue
		}
		idem := fmt.Sprintf("media:%s:%s:%s", sessionID, t.targetType, t.targetID)
		payload := map[string]any{"media_kind": t.mediaKind, "stage_key": "media_generate_endpoint"}
		if t.field != "" {
			payload["field"] = t.field
		}
		saved, created, err := cfg.MediaDispatcher.Dispatch(c.Request.Context(), generation.GenerationJob{
			ID:             idem,
			SessionID:      sessionID,
			StoryboardID:   board.ID,
			IdempotencyKey: idem,
			Provider:       t.provider,
			TargetType:     t.targetType,
			TargetID:       t.targetID,
			MaxRetries:     2,
			Payload:        payload,
		})
		if err != nil {
			writeJSONError(c, http.StatusInternalServerError, err.Error())
			return
		}
		if created {
			resp.Dispatched++
		} else {
			resp.Reused++
		}
		resp.Jobs = append(resp.Jobs, generatedMediaJob{
			JobID:      saved.ID,
			Provider:   saved.Provider,
			TargetType: saved.TargetType,
			TargetID:   saved.TargetID,
			Status:     saved.Status,
			Created:    created,
		})
	}

	c.JSON(http.StatusOK, resp)
}
