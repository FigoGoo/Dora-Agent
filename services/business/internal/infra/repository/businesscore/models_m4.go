package businesscore

import (
	"time"

	"gorm.io/datatypes"
)

type CreditBatch struct {
	ID              string     `gorm:"column:id;primaryKey"`
	AccountID       string     `gorm:"column:account_id"`
	BatchType       string     `gorm:"column:batch_type"`
	SourceType      string     `gorm:"column:source_type"`
	SourceID        *string    `gorm:"column:source_id"`
	TotalPoints     int64      `gorm:"column:total_points"`
	RemainingPoints int64      `gorm:"column:remaining_points"`
	ExpiresAt       *time.Time `gorm:"column:expires_at"`
	Status          string     `gorm:"column:status"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
}

func (CreditBatch) TableName() string { return "credit_batches" }

type CreditEstimate struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	EstimateID         string         `gorm:"column:estimate_id"`
	AccountID          string         `gorm:"column:account_id"`
	ProjectID          string         `gorm:"column:project_id"`
	ResourceType       *string        `gorm:"column:resource_type"`
	ModelID            *string        `gorm:"column:model_id"`
	PricingSnapshotID  *string        `gorm:"column:pricing_snapshot_id"`
	EstimatePoints     int64          `gorm:"column:estimate_points"`
	AvailablePoints    int64          `gorm:"column:available_points"`
	ExpiresSoonPoints  int64          `gorm:"column:expires_soon_points"`
	AccountType        string         `gorm:"column:account_type"`
	Insufficient       bool           `gorm:"column:insufficient"`
	Status             string         `gorm:"column:status"`
	ExpiresAt          time.Time      `gorm:"column:expires_at"`
	CreatedByUserID    string         `gorm:"column:created_by_user_id"`
	TraceID            string         `gorm:"column:trace_id"`
	RequestMetaJSON    datatypes.JSON `gorm:"column:request_meta_json;type:jsonb"`
	SafetyEvidenceID   *string        `gorm:"column:safety_evidence_id"`
	SafetyEvidenceHash *string        `gorm:"column:safety_evidence_digest"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
}

func (CreditEstimate) TableName() string { return "credit_estimates" }

type CreditEstimateItem struct {
	ID              string         `gorm:"column:id;primaryKey"`
	EstimateID      string         `gorm:"column:estimate_id"`
	EstimateItemID  string         `gorm:"column:estimate_item_id"`
	ItemType        string         `gorm:"column:item_type"`
	ToolName        *string        `gorm:"column:tool_name"`
	ToolType        *string        `gorm:"column:tool_type"`
	PricingPolicyID *string        `gorm:"column:pricing_policy_id"`
	ModelID         *string        `gorm:"column:model_id"`
	ResourceType    *string        `gorm:"column:resource_type"`
	BillingUnit     *string        `gorm:"column:billing_unit"`
	Quantity        *float64       `gorm:"column:quantity"`
	UnitPoints      *float64       `gorm:"column:unit_points"`
	EstimatePoints  int64          `gorm:"column:estimate_points"`
	FreeReason      *string        `gorm:"column:free_reason"`
	Status          string         `gorm:"column:status"`
	MetadataJSON    datatypes.JSON `gorm:"column:metadata_json;type:jsonb"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
}

func (CreditEstimateItem) TableName() string { return "credit_estimate_items" }

type CreditFreeze struct {
	ID             string    `gorm:"column:id;primaryKey"`
	FreezeID       string    `gorm:"column:freeze_id"`
	EstimateID     string    `gorm:"column:estimate_id"`
	AccountID      string    `gorm:"column:account_id"`
	ProjectID      string    `gorm:"column:project_id"`
	RunID          string    `gorm:"column:run_id"`
	ConfirmationID *string   `gorm:"column:confirmation_id"`
	FrozenPoints   int64     `gorm:"column:frozen_points"`
	ChargedPoints  int64     `gorm:"column:charged_points"`
	ReleasedPoints int64     `gorm:"column:released_points"`
	Status         string    `gorm:"column:status"`
	ExpiresAt      time.Time `gorm:"column:expires_at"`
	IdempotencyKey string    `gorm:"column:idempotency_key"`
	TraceID        string    `gorm:"column:trace_id"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (CreditFreeze) TableName() string { return "credit_freezes" }

type CreditFreezeBatchItem struct {
	ID             string    `gorm:"column:id;primaryKey"`
	FreezeID       string    `gorm:"column:freeze_id"`
	AccountID      string    `gorm:"column:account_id"`
	BatchID        string    `gorm:"column:batch_id"`
	FrozenPoints   int64     `gorm:"column:frozen_points"`
	ChargedPoints  int64     `gorm:"column:charged_points"`
	ReleasedPoints int64     `gorm:"column:released_points"`
	Status         string    `gorm:"column:status"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (CreditFreezeBatchItem) TableName() string { return "credit_freeze_batch_items" }

type CreditLedgerEntry struct {
	ID             string         `gorm:"column:id;primaryKey"`
	AccountID      string         `gorm:"column:account_id"`
	EntryType      string         `gorm:"column:entry_type"`
	PointsDelta    int64          `gorm:"column:points_delta"`
	BalanceAfter   int64          `gorm:"column:balance_after"`
	FrozenAfter    int64          `gorm:"column:frozen_after"`
	SourceType     string         `gorm:"column:source_type"`
	SourceID       string         `gorm:"column:source_id"`
	BatchID        *string        `gorm:"column:batch_id"`
	ProjectID      *string        `gorm:"column:project_id"`
	RunID          *string        `gorm:"column:run_id"`
	TraceID        *string        `gorm:"column:trace_id"`
	IdempotencyKey *string        `gorm:"column:idempotency_key"`
	MetadataJSON   datatypes.JSON `gorm:"column:metadata_json;type:jsonb"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
}

func (CreditLedgerEntry) TableName() string { return "credit_ledger_entries" }

type CreditToolChargeBatch struct {
	ID             string    `gorm:"column:id;primaryKey"`
	ToolChargeID   string    `gorm:"column:tool_charge_id"`
	AccountID      string    `gorm:"column:account_id"`
	ProjectID      string    `gorm:"column:project_id"`
	EstimateID     string    `gorm:"column:estimate_id"`
	FreezeID       string    `gorm:"column:freeze_id"`
	SessionID      string    `gorm:"column:session_id"`
	RunID          string    `gorm:"column:run_id"`
	ChargedPoints  int64     `gorm:"column:charged_points"`
	ReleasedPoints int64     `gorm:"column:released_points"`
	Status         string    `gorm:"column:status"`
	IdempotencyKey string    `gorm:"column:idempotency_key"`
	TraceID        string    `gorm:"column:trace_id"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (CreditToolChargeBatch) TableName() string { return "credit_tool_charge_batches" }

type CreditToolChargeItem struct {
	ID              string         `gorm:"column:id;primaryKey"`
	ToolChargeID    string         `gorm:"column:tool_charge_id"`
	EstimateItemID  string         `gorm:"column:estimate_item_id"`
	ToolCallID      string         `gorm:"column:tool_call_id"`
	ToolName        string         `gorm:"column:tool_name"`
	ToolType        string         `gorm:"column:tool_type"`
	BillingUnit     string         `gorm:"column:billing_unit"`
	ActualQuantity  float64        `gorm:"column:actual_quantity"`
	ChargedPoints   int64          `gorm:"column:charged_points"`
	ExecutionStatus string         `gorm:"column:execution_status"`
	Status          string         `gorm:"column:status"`
	MetadataJSON    datatypes.JSON `gorm:"column:metadata_json;type:jsonb"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
}

func (CreditToolChargeItem) TableName() string { return "credit_tool_charge_items" }

type RedeemCodeBatch struct {
	ID                 string     `gorm:"column:id;primaryKey"`
	BatchNo            string     `gorm:"column:batch_no"`
	TargetType         string     `gorm:"column:target_type"`
	TargetUserID       *string    `gorm:"column:target_user_id"`
	TargetEnterpriseID *string    `gorm:"column:target_enterprise_id"`
	ChannelCode        *string    `gorm:"column:channel_code"`
	TotalCodes         int        `gorm:"column:total_codes"`
	PointsPerCode      int64      `gorm:"column:points_per_code"`
	ExpiresAt          *time.Time `gorm:"column:expires_at"`
	CreditExpiresAt    *time.Time `gorm:"column:credit_expires_at"`
	Status             string     `gorm:"column:status"`
	CreatedByAdminID   *string    `gorm:"column:created_by_admin_id"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

func (RedeemCodeBatch) TableName() string { return "redeem_code_batches" }

type RedeemCode struct {
	ID                   string     `gorm:"column:id;primaryKey"`
	BatchID              string     `gorm:"column:batch_id"`
	CodeDigest           string     `gorm:"column:code_digest"`
	Status               string     `gorm:"column:status"`
	RedeemedByUserID     *string    `gorm:"column:redeemed_by_user_id"`
	RedeemedEnterpriseID *string    `gorm:"column:redeemed_enterprise_id"`
	RedeemedAccountID    *string    `gorm:"column:redeemed_account_id"`
	RedeemedAt           *time.Time `gorm:"column:redeemed_at"`
	ExpiresAt            *time.Time `gorm:"column:expires_at"`
	CreatedAt            time.Time  `gorm:"column:created_at"`
	UpdatedAt            time.Time  `gorm:"column:updated_at"`
}

func (RedeemCode) TableName() string { return "redeem_codes" }

type RedeemCodeRedemption struct {
	ID             string    `gorm:"column:id;primaryKey"`
	RedeemCodeID   string    `gorm:"column:redeem_code_id"`
	AccountID      string    `gorm:"column:account_id"`
	UserID         string    `gorm:"column:user_id"`
	EnterpriseID   *string   `gorm:"column:enterprise_id"`
	Points         int64     `gorm:"column:points"`
	Status         string    `gorm:"column:status"`
	IdempotencyKey string    `gorm:"column:idempotency_key"`
	TraceID        string    `gorm:"column:trace_id"`
	CreatedAt      time.Time `gorm:"column:created_at"`
}

func (RedeemCodeRedemption) TableName() string { return "redeem_code_redemptions" }

type Asset struct {
	ID            string         `gorm:"column:id;primaryKey"`
	AssetNo       string         `gorm:"column:asset_no"`
	OwnerUserID   string         `gorm:"column:owner_user_id"`
	SpaceID       string         `gorm:"column:space_id"`
	EnterpriseID  *string        `gorm:"column:enterprise_id"`
	ProjectID     *string        `gorm:"column:project_id"`
	AssetType     string         `gorm:"column:asset_type"`
	Title         *string        `gorm:"column:title"`
	Status        string         `gorm:"column:status"`
	Visibility    string         `gorm:"column:visibility"`
	SourceType    string         `gorm:"column:source_type"`
	SourceRefID   *string        `gorm:"column:source_ref_id"`
	ContentDigest *string        `gorm:"column:content_digest"`
	MetadataJSON  datatypes.JSON `gorm:"column:metadata_json;type:jsonb"`
	CreatedAt     time.Time      `gorm:"column:created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at"`
	DeletedAt     *time.Time     `gorm:"column:deleted_at"`
}

func (Asset) TableName() string { return "assets" }

type AssetStorageObject struct {
	ID             string         `gorm:"column:id;primaryKey"`
	AssetID        string         `gorm:"column:asset_id"`
	Bucket         string         `gorm:"column:bucket"`
	ObjectKey      *string        `gorm:"column:object_key"`
	ObjectKeyHash  string         `gorm:"column:object_key_digest"`
	ObjectURI      string         `gorm:"column:object_uri"`
	MIMEType       *string        `gorm:"column:mime_type"`
	SizeBytes      *int64         `gorm:"column:size_bytes"`
	Checksum       *string        `gorm:"column:checksum"`
	Etag           *string        `gorm:"column:etag"`
	StorageStatus  string         `gorm:"column:storage_status"`
	PreviewURI     *string        `gorm:"column:preview_uri"`
	DownloadPolicy datatypes.JSON `gorm:"column:download_policy_json;type:jsonb"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
}

func (AssetStorageObject) TableName() string { return "asset_storage_objects" }

type UploadIntent struct {
	ID               string    `gorm:"column:id;primaryKey"`
	UploadIntentID   string    `gorm:"column:upload_intent_id"`
	OwnerUserID      string    `gorm:"column:owner_user_id"`
	SpaceID          string    `gorm:"column:space_id"`
	ProjectID        *string   `gorm:"column:project_id"`
	AssetType        string    `gorm:"column:asset_type"`
	Bucket           *string   `gorm:"column:bucket"`
	ObjectKey        *string   `gorm:"column:object_key"`
	ObjectKeyHash    string    `gorm:"column:object_key_digest"`
	MIMEType         *string   `gorm:"column:mime_type"`
	MaxSizeBytes     int64     `gorm:"column:max_size_bytes"`
	Status           string    `gorm:"column:status"`
	ExpiresAt        time.Time `gorm:"column:expires_at"`
	ConfirmedAssetID *string   `gorm:"column:confirmed_asset_id"`
	IdempotencyKey   string    `gorm:"column:idempotency_key"`
	TraceID          string    `gorm:"column:trace_id"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at"`
}

func (UploadIntent) TableName() string { return "upload_intents" }

type AssetElement struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	AssetID            string         `gorm:"column:asset_id"`
	ElementType        string         `gorm:"column:element_type"`
	ElementKey         string         `gorm:"column:element_key"`
	ElementSummaryJSON datatypes.JSON `gorm:"column:element_summary_json;type:jsonb"`
	PreviewText        *string        `gorm:"column:preview_text"`
	Status             string         `gorm:"column:status"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
}

func (AssetElement) TableName() string { return "asset_elements" }

type AssetAccessLog struct {
	ID            string    `gorm:"column:id;primaryKey"`
	AssetID       string    `gorm:"column:asset_id"`
	ActorUserID   string    `gorm:"column:actor_user_id"`
	SpaceID       string    `gorm:"column:space_id"`
	ProjectID     *string   `gorm:"column:project_id"`
	AccessPurpose string    `gorm:"column:access_purpose"`
	Allowed       bool      `gorm:"column:allowed"`
	DenyReason    *string   `gorm:"column:deny_reason"`
	TraceID       *string   `gorm:"column:trace_id"`
	CreatedAt     time.Time `gorm:"column:created_at"`
}

func (AssetAccessLog) TableName() string { return "asset_access_logs" }

type GeneratedAssetObjectSlot struct {
	ID              string         `gorm:"column:id;primaryKey"`
	SlotID          string         `gorm:"column:slot_id"`
	ProjectID       string         `gorm:"column:project_id"`
	SessionID       string         `gorm:"column:session_id"`
	RunID           string         `gorm:"column:run_id"`
	ArtifactID      string         `gorm:"column:artifact_id"`
	ResourceType    string         `gorm:"column:resource_type"`
	Bucket          string         `gorm:"column:bucket"`
	ObjectKey       string         `gorm:"column:object_key"`
	ObjectKeyHash   string         `gorm:"column:object_key_digest"`
	ContentType     string         `gorm:"column:content_type"`
	SizeBytes       int64          `gorm:"column:size_bytes"`
	Checksum        *string        `gorm:"column:checksum"`
	Etag            *string        `gorm:"column:etag"`
	Status          string         `gorm:"column:status"`
	IdempotencyKey  string         `gorm:"column:idempotency_key"`
	MetadataJSON    datatypes.JSON `gorm:"column:metadata_json;type:jsonb"`
	ExpiresAt       time.Time      `gorm:"column:expires_at"`
	CreatedByUserID string         `gorm:"column:created_by_user_id"`
	TraceID         string         `gorm:"column:trace_id"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
	UpdatedAt       time.Time      `gorm:"column:updated_at"`
}

func (GeneratedAssetObjectSlot) TableName() string { return "generated_asset_object_slots" }

type AssetCommitBatch struct {
	ID                 string    `gorm:"column:id;primaryKey"`
	CommitID           string    `gorm:"column:commit_id"`
	ProjectID          string    `gorm:"column:project_id"`
	SessionID          string    `gorm:"column:session_id"`
	RunID              string    `gorm:"column:run_id"`
	FreezeID           string    `gorm:"column:freeze_id"`
	EstimateID         *string   `gorm:"column:estimate_id"`
	ActorUserID        string    `gorm:"column:actor_user_id"`
	SpaceID            string    `gorm:"column:space_id"`
	SafetyEvidenceID   string    `gorm:"column:safety_evidence_id"`
	SafetyEvidenceHash string    `gorm:"column:safety_evidence_digest"`
	ChargedPoints      int64     `gorm:"column:charged_points"`
	ReleasedPoints     int64     `gorm:"column:released_points"`
	CommitStatus       string    `gorm:"column:commit_status"`
	LedgerRef          *string   `gorm:"column:ledger_ref"`
	IdempotencyKey     string    `gorm:"column:idempotency_key"`
	TraceID            string    `gorm:"column:trace_id"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

func (AssetCommitBatch) TableName() string { return "asset_commit_batches" }

type AssetCommitItem struct {
	ID                  string         `gorm:"column:id;primaryKey"`
	CommitID            string         `gorm:"column:commit_id"`
	ArtifactID          string         `gorm:"column:artifact_id"`
	AssetID             string         `gorm:"column:asset_id"`
	ResourceType        string         `gorm:"column:resource_type"`
	ElementType         string         `gorm:"column:element_type"`
	EstimateItemID      *string        `gorm:"column:estimate_item_id"`
	ToolName            *string        `gorm:"column:tool_name"`
	ToolType            *string        `gorm:"column:tool_type"`
	ChargeQuantity      *int64         `gorm:"column:charge_quantity"`
	ChargedPoints       int64          `gorm:"column:charged_points"`
	ContentURIDigest    *string        `gorm:"column:content_uri_digest"`
	ArtifactSummaryJSON datatypes.JSON `gorm:"column:artifact_summary_json;type:jsonb"`
	MetadataJSON        datatypes.JSON `gorm:"column:metadata_json;type:jsonb"`
	Status              string         `gorm:"column:status"`
	CreatedAt           time.Time      `gorm:"column:created_at"`
}

func (AssetCommitItem) TableName() string { return "asset_commit_items" }
