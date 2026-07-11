package capability

const (
	StatusCompleted   = "completed"
	StatusAccepted    = "accepted"
	StatusWaitingUser = "waiting_user"
	StatusPartial     = "partial"
	StatusFailed      = "failed"
	StatusCancelled   = "cancelled"
)

type CapabilityResult[T any] struct {
	Status            string           `json:"status"`
	OperationID       string           `json:"operation_id,omitempty"`
	BatchID           string           `json:"batch_id,omitempty"`
	StageRunID        string           `json:"stage_run_id,omitempty"`
	StoryboardID      string           `json:"storyboard_id,omitempty"`
	StoryboardVersion int              `json:"storyboard_version,omitempty"`
	ArtifactRefs      []ArtifactRef    `json:"artifact_refs,omitempty"`
	EventIDs          []string         `json:"event_ids,omitempty"`
	Cost              *CostSummary     `json:"cost,omitempty"`
	Data              T                `json:"data,omitempty"`
	Error             *CapabilityError `json:"error,omitempty"`
}

type ArtifactRef struct {
	AssetID     string `json:"asset_id"`
	StorageURI  string `json:"storage_uri,omitempty"`
	DeliveryURL string `json:"delivery_url,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Version     int    `json:"version,omitempty"`
}

type CostSummary struct {
	Currency       string `json:"currency,omitempty"`
	EstimatedMinor int64  `json:"estimated_minor,omitempty"`
	GrossMinor     int64  `json:"gross_minor,omitempty"`
	RefundMinor    int64  `json:"refund_minor,omitempty"`
	NetMinor       int64  `json:"net_minor,omitempty"`
}

type CapabilityError struct {
	Code             string `json:"code"`
	UserMessage      string `json:"user_message"`
	TechnicalMessage string `json:"technical_message,omitempty"`
	Retryable        bool   `json:"retryable"`
	SuggestedAction  string `json:"suggested_action,omitempty"`
}
