package businesscore

import (
	"time"

	"gorm.io/datatypes"
)

type CreditHoldRecord struct {
	CreditHoldID       string    `gorm:"column:credit_hold_id;primaryKey"`
	CreditAccountID    string    `gorm:"column:credit_account_id"`
	CreditAccountScope string    `gorm:"column:credit_account_scope"`
	RunID              string    `gorm:"column:run_id"`
	ProjectID          string    `gorm:"column:project_id"`
	ToolPlanID         string    `gorm:"column:tool_plan_id"`
	ToolPlanDigest     string    `gorm:"column:tool_plan_digest"`
	Status             string    `gorm:"column:status"`
	FrozenCredits      int       `gorm:"column:frozen_credits"`
	CommittedCredits   int       `gorm:"column:committed_credits"`
	ReleasedCredits    int       `gorm:"column:released_credits"`
	IdempotencyKey     string    `gorm:"column:idempotency_key"`
	TraceID            string    `gorm:"column:trace_id"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

func (CreditHoldRecord) TableName() string { return "credit_holds" }

type CreditLedgerEntryRecord struct {
	LedgerEntryID   string    `gorm:"column:ledger_entry_id;primaryKey"`
	CreditHoldID    string    `gorm:"column:credit_hold_id"`
	CreditAccountID string    `gorm:"column:credit_account_id"`
	EntryType       string    `gorm:"column:entry_type"`
	Credits         int       `gorm:"column:credits"`
	Reason          string    `gorm:"column:reason"`
	Digest          string    `gorm:"column:digest"`
	TraceID         string    `gorm:"column:trace_id"`
	CreatedAt       time.Time `gorm:"column:created_at"`
}

func (CreditLedgerEntryRecord) TableName() string { return "credit_ledger_entries" }

type ToolPricingSnapshotRecord struct {
	PricingSnapshotID string     `gorm:"column:pricing_snapshot_id;primaryKey"`
	ToolID            string     `gorm:"column:tool_id"`
	ToolVersion       string     `gorm:"column:tool_version"`
	ResourceType      string     `gorm:"column:resource_type"`
	UnitCredits       int        `gorm:"column:unit_credits"`
	PricingDigest     string     `gorm:"column:pricing_digest"`
	EffectiveAt       time.Time  `gorm:"column:effective_at"`
	ExpiresAt         *time.Time `gorm:"column:expires_at"`
}

func (ToolPricingSnapshotRecord) TableName() string { return "tool_pricing_snapshots" }

type GeneratedAssetRecord struct {
	AssetID      string    `gorm:"column:asset_id;primaryKey"`
	ProjectID    string    `gorm:"column:project_id"`
	RunID        string    `gorm:"column:run_id"`
	ToolTaskID   string    `gorm:"column:tool_task_id"`
	ResourceType string    `gorm:"column:resource_type"`
	Status       string    `gorm:"column:status"`
	TOSObjectKey string    `gorm:"column:tos_object_key"`
	PreviewURL   *string   `gorm:"column:preview_url"`
	AssetDigest  string    `gorm:"column:asset_digest"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (GeneratedAssetRecord) TableName() string { return "generated_assets" }

type AssetCommitRecord struct {
	CommitRecordID    string         `gorm:"column:commit_record_id;primaryKey"`
	ToolTaskID        string         `gorm:"column:tool_task_id"`
	RunID             string         `gorm:"column:run_id"`
	ProjectID         string         `gorm:"column:project_id"`
	Status            string         `gorm:"column:status"`
	ToolResultDigest  string         `gorm:"column:tool_result_digest"`
	CommittedAssetIDs datatypes.JSON `gorm:"column:committed_asset_ids;type:jsonb"`
	FailedAssetCount  int            `gorm:"column:failed_asset_count"`
	CommitDigest      string         `gorm:"column:commit_digest"`
	IdempotencyKey    string         `gorm:"column:idempotency_key"`
	TraceID           string         `gorm:"column:trace_id"`
	CreatedAt         time.Time      `gorm:"column:created_at"`
}

func (AssetCommitRecord) TableName() string { return "asset_commit_records" }
