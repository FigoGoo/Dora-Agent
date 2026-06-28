package businesscore

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type Work struct {
	ID                     string         `gorm:"column:id;primaryKey"`
	WorkNo                 string         `gorm:"column:work_no"`
	ProjectID              string         `gorm:"column:project_id"`
	OwnerUserID            string         `gorm:"column:owner_user_id"`
	SpaceID                string         `gorm:"column:space_id"`
	Title                  string         `gorm:"column:title"`
	Description            *string        `gorm:"column:description"`
	Category               *string        `gorm:"column:category"`
	TagsJSON               datatypes.JSON `gorm:"column:tags;type:jsonb"`
	ShareStatus            string         `gorm:"column:share_status"`
	CoverAssetID           *string        `gorm:"column:cover_asset_id"`
	CurrentSnapshotID      *string        `gorm:"column:current_snapshot_id"`
	LastModerationRecordID *string        `gorm:"column:last_moderation_record_id"`
	PrivateResetAt         *time.Time     `gorm:"column:private_reset_at"`
	CreatedBy              *string        `gorm:"column:created_by"`
	UpdatedBy              *string        `gorm:"column:updated_by"`
	CreatedAt              time.Time      `gorm:"column:created_at"`
	UpdatedAt              time.Time      `gorm:"column:updated_at"`
	DeletedAt              gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (Work) TableName() string { return "works" }

type WorkAsset struct {
	ID           string         `gorm:"column:id;primaryKey"`
	WorkAssetID  string         `gorm:"column:work_asset_id"`
	WorkID       string         `gorm:"column:work_id"`
	AssetID      string         `gorm:"column:asset_id"`
	Role         string         `gorm:"column:role"`
	DisplayOrder int            `gorm:"column:display_order"`
	CreatedBy    *string        `gorm:"column:created_by"`
	UpdatedBy    *string        `gorm:"column:updated_by"`
	CreatedAt    time.Time      `gorm:"column:created_at"`
	UpdatedAt    time.Time      `gorm:"column:updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (WorkAsset) TableName() string { return "work_assets" }

type WorkPublicSnapshot struct {
	ID                   string         `gorm:"column:id;primaryKey"`
	SnapshotID           string         `gorm:"column:snapshot_id"`
	WorkID               string         `gorm:"column:work_id"`
	ShareSlug            string         `gorm:"column:share_slug"`
	Title                string         `gorm:"column:title"`
	Description          *string        `gorm:"column:description"`
	CoverAssetID         *string        `gorm:"column:cover_asset_id"`
	SnapshotJSON         datatypes.JSON `gorm:"column:snapshot_json;type:jsonb"`
	ShareURL             string         `gorm:"column:share_url"`
	Visibility           string         `gorm:"column:visibility"`
	PublicWorkID         string         `gorm:"column:public_work_id"`
	PublicSlug           string         `gorm:"column:public_slug"`
	PublicURL            string         `gorm:"column:public_url"`
	SnapshotPayloadJSON  datatypes.JSON `gorm:"column:snapshot_payload;type:jsonb"`
	PublicMediaRefsJSON  datatypes.JSON `gorm:"column:public_media_refs;type:jsonb"`
	Status               string         `gorm:"column:status"`
	Category             *string        `gorm:"column:category"`
	ResourceType         *string        `gorm:"column:resource_type"`
	LikeCount            int64          `gorm:"column:like_count"`
	PublishedByUserID    string         `gorm:"column:published_by_user_id"`
	PublishedBy          string         `gorm:"column:published_by"`
	PublishedAt          time.Time      `gorm:"column:published_at"`
	TakenDownByAdminID   *string        `gorm:"column:taken_down_by_admin_id"`
	TakenDownBy          *string        `gorm:"column:taken_down_by"`
	TakenDownAt          *time.Time     `gorm:"column:taken_down_at"`
	TakeDownReason       *string        `gorm:"column:take_down_reason"`
	TakenDownReason      *string        `gorm:"column:taken_down_reason"`
	SafetyEvidenceID     *string        `gorm:"column:safety_evidence_id"`
	SafetyEvidenceDigest *string        `gorm:"column:safety_evidence_digest"`
	CreatedBy            *string        `gorm:"column:created_by"`
	UpdatedBy            *string        `gorm:"column:updated_by"`
	CreatedAt            time.Time      `gorm:"column:created_at"`
	UpdatedAt            time.Time      `gorm:"column:updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (WorkPublicSnapshot) TableName() string { return "work_public_snapshots" }

type WorkLike struct {
	ID           string         `gorm:"column:id;primaryKey"`
	LikeID       string         `gorm:"column:like_id"`
	PublicWorkID string         `gorm:"column:public_work_id"`
	WorkID       string         `gorm:"column:work_id"`
	SnapshotID   string         `gorm:"column:snapshot_id"`
	UserID       string         `gorm:"column:user_id"`
	Status       string         `gorm:"column:status"`
	LikedAt      *time.Time     `gorm:"column:liked_at"`
	CreatedBy    *string        `gorm:"column:created_by"`
	UpdatedBy    *string        `gorm:"column:updated_by"`
	CreatedAt    time.Time      `gorm:"column:created_at"`
	UpdatedAt    time.Time      `gorm:"column:updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (WorkLike) TableName() string { return "work_likes" }

type WorkCategory struct {
	ID          string         `gorm:"column:id;primaryKey"`
	CategoryKey string         `gorm:"column:category_key"`
	DisplayName string         `gorm:"column:display_name"`
	Status      string         `gorm:"column:status"`
	SortOrder   int            `gorm:"column:sort_order"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (WorkCategory) TableName() string { return "work_categories" }

type WorkModerationRecord struct {
	ID                string    `gorm:"column:id;primaryKey"`
	RecordID          string    `gorm:"column:record_id"`
	SnapshotID        string    `gorm:"column:snapshot_id"`
	PublicWorkID      string    `gorm:"column:public_work_id"`
	Action            string    `gorm:"column:action"`
	Reason            string    `gorm:"column:reason"`
	BeforeStatus      *string   `gorm:"column:before_status"`
	AfterStatus       string    `gorm:"column:after_status"`
	OperatedByAdminID string    `gorm:"column:operated_by_admin_id"`
	OperatorAdminID   string    `gorm:"column:operator_admin_id"`
	TraceID           string    `gorm:"column:trace_id"`
	CreatedAt         time.Time `gorm:"column:created_at"`
}

func (WorkModerationRecord) TableName() string { return "work_moderation_records" }

type Notification struct {
	ID                    string         `gorm:"column:id;primaryKey"`
	NotificationID        string         `gorm:"column:notification_id"`
	NotificationNo        string         `gorm:"column:notification_no"`
	RecipientUserID       string         `gorm:"column:recipient_user_id"`
	RecipientSpaceID      *string        `gorm:"column:recipient_space_id"`
	RecipientEnterpriseID *string        `gorm:"column:recipient_enterprise_id"`
	NotificationType      string         `gorm:"column:notification_type"`
	Type                  string         `gorm:"column:type"`
	Title                 string         `gorm:"column:title"`
	Summary               string         `gorm:"column:summary"`
	Body                  *string        `gorm:"column:body"`
	JumpType              string         `gorm:"column:jump_type"`
	JumpTargetID          *string        `gorm:"column:jump_target_id"`
	JumpPayloadJSON       datatypes.JSON `gorm:"column:jump_payload_json;type:jsonb"`
	SourceType            string         `gorm:"column:source_type"`
	SourceID              *string        `gorm:"column:source_id"`
	Status                string         `gorm:"column:status"`
	RelatedResourceType   *string        `gorm:"column:related_resource_type"`
	RelatedResourceID     *string        `gorm:"column:related_resource_id"`
	NavigationHintJSON    datatypes.JSON `gorm:"column:navigation_hint;type:jsonb"`
	ReadAt                *time.Time     `gorm:"column:read_at"`
	IdempotencyKey        string         `gorm:"column:idempotency_key"`
	TraceID               string         `gorm:"column:trace_id"`
	CreatedAt             time.Time      `gorm:"column:created_at"`
	UpdatedAt             time.Time      `gorm:"column:updated_at"`
	DeletedAt             gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (Notification) TableName() string { return "notifications" }

type NotificationCreateFailure struct {
	ID                  string         `gorm:"column:id;primaryKey"`
	FailureID           string         `gorm:"column:failure_id"`
	SourceType          string         `gorm:"column:source_type"`
	SourceID            string         `gorm:"column:source_id"`
	RecipientUserID     *string        `gorm:"column:recipient_user_id"`
	Type                string         `gorm:"column:type"`
	RelatedResourceType *string        `gorm:"column:related_resource_type"`
	RelatedResourceID   *string        `gorm:"column:related_resource_id"`
	IdempotencyKey      string         `gorm:"column:idempotency_key"`
	FailureCode         string         `gorm:"column:failure_code"`
	FailureSummary      *string        `gorm:"column:failure_summary"`
	ErrorCode           string         `gorm:"column:error_code"`
	RetryCount          int            `gorm:"column:retry_count"`
	NextRetryAt         *time.Time     `gorm:"column:next_retry_at"`
	TraceID             string         `gorm:"column:trace_id"`
	CreatedAt           time.Time      `gorm:"column:created_at"`
	UpdatedAt           time.Time      `gorm:"column:updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (NotificationCreateFailure) TableName() string { return "notification_create_failures" }
