package generation

import "time"

const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"

	ProviderImage2   = "image2"
	ProviderSeedance = "seedance"
	ProviderAudio    = "audio"

	TargetKeyElement = "key_element"
	TargetShot       = "shot"
	TargetAudioLayer = "audio_layer"
)

type GenerationJob struct {
	ID             string         `json:"id"`
	SessionID      string         `json:"session_id"`
	StoryboardID   string         `json:"storyboard_id,omitempty"`
	ToolCallID     string         `json:"tool_call_id,omitempty"`
	IdempotencyKey string         `json:"idempotency_key"`
	Provider       string         `json:"provider,omitempty"`
	TargetType     string         `json:"target_type,omitempty"`
	TargetID       string         `json:"target_id,omitempty"`
	Status         string         `json:"status"`
	RetryCount     int            `json:"retry_count"`
	MaxRetries     int            `json:"max_retries"`
	StatusVersion  int            `json:"status_version"`
	Payload        map[string]any `json:"payload,omitempty"`
	Result         map[string]any `json:"result,omitempty"`
	ResultAssetIDs []string       `json:"result_asset_ids,omitempty"`
	ErrorCode      string         `json:"error_code,omitempty"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type StatusUpdate struct {
	ResultAssetIDs []string
	Result         map[string]any
	ErrorCode      string
	ErrorMessage   string
}

func NormalizeStatus(status string) string {
	switch status {
	case StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled:
		return status
	default:
		return ""
	}
}
