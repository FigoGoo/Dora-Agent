package spec

import "time"

const (
	StatusDraft      = "draft"
	StatusReviewing  = "reviewing"
	StatusConfirmed  = "confirmed"
	StatusRejected   = "rejected"
	StatusSuperseded = "superseded"
)

type FinalVideoSpec struct {
	ID              string         `json:"id"`
	SessionID       string         `json:"session_id"`
	Version         int            `json:"version"`
	Status          string         `json:"status"`
	IdempotencyKey  string         `json:"idempotency_key,omitempty"`
	Title           string         `json:"title,omitempty"`
	VideoType       string         `json:"video_type,omitempty"`
	TargetAudience  string         `json:"target_audience,omitempty"`
	OutputLanguage  string         `json:"output_language,omitempty"`
	DurationSeconds int            `json:"duration_seconds,omitempty"`
	AspectRatio     string         `json:"aspect_ratio,omitempty"`
	NarrativeDriver string         `json:"narrative_driver,omitempty"`
	VisualStyle     string         `json:"visual_style,omitempty"`
	SoundStyle      string         `json:"sound_style,omitempty"`
	ModelPreference string         `json:"model_preference,omitempty"`
	Markdown        string         `json:"markdown,omitempty"`
	Fields          map[string]any `json:"fields,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at,omitempty"`
}
