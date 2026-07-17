package postgres

import "time"

// mediaPreviewAssetModel 映射 Business-owned 媒体预览 Asset 权威状态与文件元数据。
type mediaPreviewAssetModel struct {
	ID                 string     `gorm:"column:id;type:uuid;primaryKey"`
	OwnerUserID        string     `gorm:"column:owner_user_id;type:uuid"`
	ProjectID          string     `gorm:"column:project_id;type:uuid"`
	AssetVersion       int64      `gorm:"column:asset_version"`
	Status             string     `gorm:"column:status"`
	MediaKind          string     `gorm:"column:media_kind"`
	MIMEType           string     `gorm:"column:mime_type"`
	OutputProfile      string     `gorm:"column:output_profile"`
	SourceType         string     `gorm:"column:source_type"`
	SourceID           string     `gorm:"column:source_id;type:uuid"`
	SourceVersion      int64      `gorm:"column:source_version"`
	SourceDigest       []byte     `gorm:"column:source_digest"`
	TargetLocalKey     *string    `gorm:"column:target_local_key"`
	TargetDigest       []byte     `gorm:"column:target_digest"`
	ObjectKey          *string    `gorm:"column:object_key"`
	ContentDigest      []byte     `gorm:"column:content_digest"`
	SizeBytes          *int64     `gorm:"column:size_bytes"`
	Width              *int       `gorm:"column:width"`
	Height             *int       `gorm:"column:height"`
	DurationMS         *int64     `gorm:"column:duration_ms"`
	Codec              *string    `gorm:"column:codec"`
	PixelFormat        *string    `gorm:"column:pixel_format"`
	FinalizedJobID     *string    `gorm:"column:finalized_job_id;type:uuid"`
	FinalizedAttemptID *string    `gorm:"column:finalized_attempt_id;type:uuid"`
	FinalizedFence     *int64     `gorm:"column:finalized_fence"`
	ErrorCode          *string    `gorm:"column:error_code"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	FinalizedAt        *time.Time `gorm:"column:finalized_at"`
}

// TableName 返回媒体预览 Asset 权威表名。
func (mediaPreviewAssetModel) TableName() string { return "business.media_preview_asset" }

// mediaPreviewPreparationReceiptModel 映射 Prepare first-write-wins 回执。
type mediaPreviewPreparationReceiptModel struct {
	ID               string    `gorm:"column:id;type:uuid;primaryKey"`
	RequestID        string    `gorm:"column:request_id;type:uuid"`
	CommandID        string    `gorm:"column:command_id;type:uuid"`
	RequestDigest    []byte    `gorm:"column:request_digest"`
	OperationID      string    `gorm:"column:operation_id;type:uuid"`
	OwnerUserID      string    `gorm:"column:owner_user_id;type:uuid"`
	ProjectID        string    `gorm:"column:project_id;type:uuid"`
	ToolKey          string    `gorm:"column:tool_key"`
	ScopeDigest      []byte    `gorm:"column:scope_digest"`
	OutputProfile    string    `gorm:"column:output_profile"`
	SourceType       string    `gorm:"column:source_type"`
	SourceID         string    `gorm:"column:source_id;type:uuid"`
	SourceVersion    int64     `gorm:"column:source_version"`
	SourceDigest     []byte    `gorm:"column:source_digest"`
	TargetLocalKey   *string   `gorm:"column:target_local_key"`
	TargetDigest     []byte    `gorm:"column:target_digest"`
	SourceObjectKey  *string   `gorm:"column:source_object_key"`
	AssetID          string    `gorm:"column:asset_id;type:uuid"`
	AssetVersion     int64     `gorm:"column:asset_version"`
	AssetStatus      string    `gorm:"column:asset_status"`
	MediaKind        string    `gorm:"column:media_kind"`
	MIMEType         string    `gorm:"column:mime_type"`
	StagingObjectKey string    `gorm:"column:staging_object_key"`
	FinalObjectKey   string    `gorm:"column:final_object_key"`
	CreatedAt        time.Time `gorm:"column:created_at"`
}

// TableName 返回媒体 Preview Preparation 回执表名。
func (mediaPreviewPreparationReceiptModel) TableName() string {
	return "business.media_preview_preparation_receipt"
}

// mediaPreviewFinalizationReceiptModel 映射 Finalize first-write-wins 终态回执。
type mediaPreviewFinalizationReceiptModel struct {
	ID             string    `gorm:"column:id;type:uuid;primaryKey"`
	RequestID      string    `gorm:"column:request_id;type:uuid"`
	CommandID      string    `gorm:"column:command_id;type:uuid"`
	RequestDigest  []byte    `gorm:"column:request_digest"`
	PreparationID  string    `gorm:"column:preparation_id;type:uuid"`
	OperationID    string    `gorm:"column:operation_id;type:uuid"`
	BatchID        string    `gorm:"column:batch_id;type:uuid"`
	JobID          string    `gorm:"column:job_id;type:uuid"`
	AttemptID      string    `gorm:"column:attempt_id;type:uuid"`
	Fence          int64     `gorm:"column:fence"`
	TerminalStatus string    `gorm:"column:terminal_status"`
	AssetID        string    `gorm:"column:asset_id;type:uuid"`
	AssetVersion   int64     `gorm:"column:asset_version"`
	AssetStatus    string    `gorm:"column:asset_status"`
	MediaKind      string    `gorm:"column:media_kind"`
	MIMEType       string    `gorm:"column:mime_type"`
	ContentDigest  []byte    `gorm:"column:content_digest"`
	SizeBytes      *int64    `gorm:"column:size_bytes"`
	Width          *int      `gorm:"column:width"`
	Height         *int      `gorm:"column:height"`
	DurationMS     *int64    `gorm:"column:duration_ms"`
	Codec          *string   `gorm:"column:codec"`
	PixelFormat    *string   `gorm:"column:pixel_format"`
	ErrorCode      *string   `gorm:"column:error_code"`
	CompletedAt    time.Time `gorm:"column:completed_at"`
}

// TableName 返回媒体 Preview Finalization 回执表名。
func (mediaPreviewFinalizationReceiptModel) TableName() string {
	return "business.media_preview_finalization_receipt"
}
