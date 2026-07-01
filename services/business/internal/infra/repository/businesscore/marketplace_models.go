package businesscore

import "time"

type MarketplaceSkillPackageRecord struct {
	SkillID        string    `gorm:"column:skill_id;primaryKey"`
	CreatorUserID  string    `gorm:"column:creator_user_id"`
	Name           string    `gorm:"column:name"`
	Description    string    `gorm:"column:description"`
	Visibility     string    `gorm:"column:visibility"`
	CurrentVersion *string   `gorm:"column:current_version"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (MarketplaceSkillPackageRecord) TableName() string { return "skill_packages" }

type MarketplaceSkillVersionRecord struct {
	SkillVersionID      string     `gorm:"column:skill_version_id;primaryKey"`
	SkillID             string     `gorm:"column:skill_id"`
	Version             string     `gorm:"column:version"`
	Status              string     `gorm:"column:status"`
	RuntimeSpecDigest   string     `gorm:"column:runtime_spec_digest"`
	PricingPolicyDigest string     `gorm:"column:pricing_policy_digest"`
	SubmittedAt         *time.Time `gorm:"column:submitted_at"`
	PublishedAt         *time.Time `gorm:"column:published_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (MarketplaceSkillVersionRecord) TableName() string { return "skill_versions" }

type SkillPricingPolicyRecord struct {
	PricingPolicyID     string    `gorm:"column:pricing_policy_id;primaryKey"`
	SkillID             string    `gorm:"column:skill_id"`
	SkillVersion        string    `gorm:"column:skill_version"`
	PricingModel        string    `gorm:"column:pricing_model"`
	UsageCredits        int       `gorm:"column:usage_credits"`
	ValueDeliveredStage string    `gorm:"column:value_delivered_stage"`
	PricingPolicyDigest string    `gorm:"column:pricing_policy_digest"`
	CreatedAt           time.Time `gorm:"column:created_at"`
}

func (SkillPricingPolicyRecord) TableName() string { return "skill_pricing_policies" }

type MarketplaceListingRecord struct {
	ListingID           string     `gorm:"column:listing_id;primaryKey"`
	SkillID             string     `gorm:"column:skill_id"`
	SkillVersionID      string     `gorm:"column:skill_version_id"`
	Status              string     `gorm:"column:status"`
	PricingPolicyDigest string     `gorm:"column:pricing_policy_digest"`
	PublishedBy         string     `gorm:"column:published_by"`
	ListedAt            *time.Time `gorm:"column:listed_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (MarketplaceListingRecord) TableName() string { return "marketplace_listings" }

type SkillInstallationRecord struct {
	InstallationID   string    `gorm:"column:installation_id;primaryKey"`
	AccountID        string    `gorm:"column:account_id"`
	AccountScope     string    `gorm:"column:account_scope"`
	ListingID        string    `gorm:"column:listing_id"`
	SkillID          string    `gorm:"column:skill_id"`
	InstalledVersion string    `gorm:"column:installed_version"`
	VersionStrategy  string    `gorm:"column:version_strategy"`
	Status           string    `gorm:"column:status"`
	UpgradeStatus    string    `gorm:"column:upgrade_status"`
	IdempotencyKey   string    `gorm:"column:idempotency_key"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at"`
}

func (SkillInstallationRecord) TableName() string { return "skill_installations" }

type SkillUsageRecord struct {
	UsageID             string     `gorm:"column:usage_id;primaryKey"`
	RunID               string     `gorm:"column:run_id"`
	ListingID           string     `gorm:"column:listing_id"`
	SkillID             string     `gorm:"column:skill_id"`
	SkillVersion        string     `gorm:"column:skill_version"`
	PricingPolicyDigest string     `gorm:"column:pricing_policy_digest"`
	SkillUsageDigest    string     `gorm:"column:skill_usage_digest"`
	UsageStatus         string     `gorm:"column:usage_status"`
	ChargeStatus        string     `gorm:"column:charge_status"`
	RefundStatus        string     `gorm:"column:refund_status"`
	SettlementStatus    string     `gorm:"column:settlement_status"`
	EstimatedCredits    int        `gorm:"column:estimated_credits"`
	CreditHoldID        *string    `gorm:"column:credit_hold_id"`
	IdempotencyKey      string     `gorm:"column:idempotency_key"`
	ValueDeliveredAt    *time.Time `gorm:"column:value_delivered_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (SkillUsageRecord) TableName() string { return "skill_usage_records" }

type SkillSettlementRecord struct {
	SettlementID       string    `gorm:"column:settlement_id;primaryKey"`
	UsageID            string    `gorm:"column:usage_id"`
	CreatorUserID      string    `gorm:"column:creator_user_id"`
	Status             string    `gorm:"column:status"`
	GrossCredits       int       `gorm:"column:gross_credits"`
	PlatformFeeCredits int       `gorm:"column:platform_fee_credits"`
	CreatorCredits     int       `gorm:"column:creator_credits"`
	HoldUntil          time.Time `gorm:"column:hold_until"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

func (SkillSettlementRecord) TableName() string { return "skill_settlement_records" }

type SkillSettlementPayoutRecord struct {
	PayoutID        string    `gorm:"column:payout_id;primaryKey"`
	SettlementID    string    `gorm:"column:settlement_id"`
	CreatorUserID   string    `gorm:"column:creator_user_id"`
	Action          string    `gorm:"column:action"`
	StatusBefore    string    `gorm:"column:status_before"`
	StatusAfter     string    `gorm:"column:status_after"`
	PayoutReference string    `gorm:"column:payout_reference"`
	ReasonCode      string    `gorm:"column:reason_code"`
	OperatorAdminID string    `gorm:"column:operator_admin_id"`
	IdempotencyKey  string    `gorm:"column:idempotency_key"`
	CreatedAt       time.Time `gorm:"column:created_at"`
	UpdatedAt       time.Time `gorm:"column:updated_at"`
}

func (SkillSettlementPayoutRecord) TableName() string {
	return "skill_settlement_payout_records"
}

type SkillRefundCaseRecord struct {
	RefundCaseID string    `gorm:"column:refund_case_id;primaryKey"`
	UsageID      string    `gorm:"column:usage_id"`
	SettlementID *string   `gorm:"column:settlement_id"`
	Status       string    `gorm:"column:status"`
	ReasonCode   string    `gorm:"column:reason_code"`
	RefundDigest string    `gorm:"column:refund_digest"`
	CreatedBy    string    `gorm:"column:created_by"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

func (SkillRefundCaseRecord) TableName() string { return "skill_refund_cases" }

type MarketplaceSkillReviewRecord struct {
	ReviewID       string    `gorm:"column:review_id;primaryKey"`
	SkillID        string    `gorm:"column:skill_id"`
	SkillVersionID string    `gorm:"column:skill_version_id"`
	Status         string    `gorm:"column:status"`
	ReviewerID     *string   `gorm:"column:reviewer_id"`
	DecisionReason *string   `gorm:"column:decision_reason"`
	CreatedAt      time.Time `gorm:"column:created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at"`
}

func (MarketplaceSkillReviewRecord) TableName() string { return "skill_review_records" }
