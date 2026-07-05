package storyboard

import (
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/patch"
)

const (
	StatusDraft      = "draft"
	StatusReviewing  = "reviewing"
	StatusConfirmed  = "confirmed"
	StatusGenerating = "generating"
	StatusReady      = "ready"
)

type Storyboard struct {
	ID          string       `json:"id"`
	SessionID   string       `json:"session_id"`
	SpecID      string       `json:"spec_id,omitempty"`
	Version     int          `json:"version"`
	Status      string       `json:"status"`
	KeyElements []KeyElement `json:"key_elements,omitempty"`
	Shots       []Shot       `json:"shots,omitempty"`
	AudioLayers []AudioLayer `json:"audio_layers,omitempty"`
	CreatedAt   time.Time    `json:"created_at,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at,omitempty"`
}

type KeyElement struct {
	Key          string   `json:"key"`
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Prompt       string   `json:"prompt,omitempty"`
	AssetIDs     []string `json:"asset_ids,omitempty"`
	Status       string   `json:"status,omitempty"`
	LockedByUser bool     `json:"locked_by_user,omitempty"`
}

type Shot struct {
	ShotID            string   `json:"shot_id"`
	Index             int      `json:"index"`
	DurationSec       float64  `json:"duration_sec,omitempty"`
	SceneDescription  string   `json:"scene_description,omitempty"`
	CameraDesign      string   `json:"camera_design,omitempty"`
	Narration         string   `json:"narration,omitempty"`
	ReferenceElements []string `json:"reference_elements,omitempty"`
	Prompt            string   `json:"prompt,omitempty"`
	KeyframeAssetID   string   `json:"keyframe_asset_id,omitempty"`
	VideoAssetID      string   `json:"video_asset_id,omitempty"`
	Status            string   `json:"status,omitempty"`
}

type AudioLayer struct {
	LayerID     string `json:"layer_id"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	AssetID     string `json:"asset_id,omitempty"`
	Status      string `json:"status,omitempty"`
}

type PatchRequest struct {
	EventID      string
	SessionID    string
	StoryboardID string
	BaseVersion  int
	Source       string
	ToolCallID   string
	Ops          []patch.JSONPatchOp
}

type EventRecord struct {
	ID           string              `json:"id"`
	SessionID    string              `json:"session_id"`
	StoryboardID string              `json:"storyboard_id"`
	BaseVersion  int                 `json:"base_version"`
	NextVersion  int                 `json:"next_version"`
	Source       string              `json:"source"`
	ToolCallID   string              `json:"tool_call_id,omitempty"`
	Ops          []patch.JSONPatchOp `json:"ops"`
	CreatedAt    time.Time           `json:"created_at"`
}
