package businesscore

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CreditBatch struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	AccountID          string         `gorm:"column:account_id"`
	BatchType          string         `gorm:"column:batch_type"`
	SourceType         string         `gorm:"column:source_type"`
	SourceID           *string        `gorm:"column:source_id"`
	TotalPoints        int64          `gorm:"column:total_points"`
	RemainingPoints    int64          `gorm:"column:remaining_points"`
	OriginalPoints     int64          `gorm:"column:original_points"`
	AvailablePoints    int64          `gorm:"column:available_points"`
	FrozenPoints       int64          `gorm:"column:frozen_points"`
	ConsumedPoints     int64          `gorm:"column:consumed_points"`
	ExpiredPoints      int64          `gorm:"column:expired_points"`
	GrantedAt          time.Time      `gorm:"column:granted_at"`
	ExpiresAt          *time.Time     `gorm:"column:expires_at"`
	ExpiryPolicyJSON   datatypes.JSON `gorm:"column:expiry_policy_json;type:jsonb"`
	SpendScopeJSON     datatypes.JSON `gorm:"column:spend_scope_json;type:jsonb"`
	SettlementEligible bool           `gorm:"column:settlement_eligible"`
	Status             string         `gorm:"column:status"`
	CreatedBy          *string        `gorm:"column:created_by"`
	UpdatedBy          *string        `gorm:"column:updated_by"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at"`
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
	CreatedBy          *string        `gorm:"column:created_by"`
	UpdatedBy          *string        `gorm:"column:updated_by"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at"`
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
	CreatedBy       *string        `gorm:"column:created_by"`
	UpdatedBy       *string        `gorm:"column:updated_by"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
	DeletedAt       gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (CreditEstimateItem) TableName() string { return "credit_estimate_items" }

type CreditFreeze struct {
	ID             string         `gorm:"column:id;primaryKey"`
	FreezeID       string         `gorm:"column:freeze_id"`
	EstimateID     string         `gorm:"column:estimate_id"`
	AccountID      string         `gorm:"column:account_id"`
	ProjectID      string         `gorm:"column:project_id"`
	RunID          string         `gorm:"column:run_id"`
	ConfirmationID *string        `gorm:"column:confirmation_id"`
	FrozenPoints   int64          `gorm:"column:frozen_points"`
	ChargedPoints  int64          `gorm:"column:charged_points"`
	ReleasedPoints int64          `gorm:"column:released_points"`
	Status         string         `gorm:"column:status"`
	ExpiresAt      time.Time      `gorm:"column:expires_at"`
	IdempotencyKey string         `gorm:"column:idempotency_key"`
	TraceID        string         `gorm:"column:trace_id"`
	CreatedBy      *string        `gorm:"column:created_by"`
	UpdatedBy      *string        `gorm:"column:updated_by"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (CreditFreeze) TableName() string { return "credit_freezes" }

type CreditFreezeBatchItem struct {
	ID             string         `gorm:"column:id;primaryKey"`
	FreezeID       string         `gorm:"column:freeze_id"`
	AccountID      string         `gorm:"column:account_id"`
	BatchID        string         `gorm:"column:batch_id"`
	FrozenPoints   int64          `gorm:"column:frozen_points"`
	ChargedPoints  int64          `gorm:"column:charged_points"`
	ReleasedPoints int64          `gorm:"column:released_points"`
	Status         string         `gorm:"column:status"`
	CreatedBy      *string        `gorm:"column:created_by"`
	UpdatedBy      *string        `gorm:"column:updated_by"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"column:deleted_at"`
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
	ID             string         `gorm:"column:id;primaryKey"`
	ToolChargeID   string         `gorm:"column:tool_charge_id"`
	AccountID      string         `gorm:"column:account_id"`
	ProjectID      string         `gorm:"column:project_id"`
	EstimateID     string         `gorm:"column:estimate_id"`
	FreezeID       string         `gorm:"column:freeze_id"`
	SessionID      string         `gorm:"column:session_id"`
	RunID          string         `gorm:"column:run_id"`
	ChargedPoints  int64          `gorm:"column:charged_points"`
	ReleasedPoints int64          `gorm:"column:released_points"`
	Status         string         `gorm:"column:status"`
	IdempotencyKey string         `gorm:"column:idempotency_key"`
	TraceID        string         `gorm:"column:trace_id"`
	CreatedBy      *string        `gorm:"column:created_by"`
	UpdatedBy      *string        `gorm:"column:updated_by"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"column:deleted_at"`
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
	CreatedBy       *string        `gorm:"column:created_by"`
	UpdatedBy       *string        `gorm:"column:updated_by"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
	DeletedAt       gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (CreditToolChargeItem) TableName() string { return "credit_tool_charge_items" }

type RedeemCodeBatch struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	BatchNo            string         `gorm:"column:batch_no"`
	TargetType         string         `gorm:"column:target_type"`
	AccountType        string         `gorm:"column:account_type"`
	BindTargetType     string         `gorm:"column:bind_target_type"`
	BindTargetID       *string        `gorm:"column:bind_target_id"`
	TargetUserID       *string        `gorm:"column:target_user_id"`
	TargetEnterpriseID *string        `gorm:"column:target_enterprise_id"`
	ChannelCode        *string        `gorm:"column:channel_code"`
	TotalCodes         int            `gorm:"column:total_codes"`
	PointsPerCode      int64          `gorm:"column:points_per_code"`
	ExpiresAt          *time.Time     `gorm:"column:expires_at"`
	CreditExpiresAt    *time.Time     `gorm:"column:credit_expires_at"`
	Status             string         `gorm:"column:status"`
	CreatedByAdminID   *string        `gorm:"column:created_by_admin_id"`
	Reason             *string        `gorm:"column:reason"`
	CreatedBy          *string        `gorm:"column:created_by"`
	UpdatedBy          *string        `gorm:"column:updated_by"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (RedeemCodeBatch) TableName() string { return "redeem_code_batches" }

type RedeemCode struct {
	ID                   string         `gorm:"column:id;primaryKey"`
	BatchID              string         `gorm:"column:batch_id"`
	CodeDigest           string         `gorm:"column:code_digest"`
	Status               string         `gorm:"column:status"`
	RedeemedByUserID     *string        `gorm:"column:redeemed_by_user_id"`
	RedeemedEnterpriseID *string        `gorm:"column:redeemed_enterprise_id"`
	RedeemedAccountID    *string        `gorm:"column:redeemed_account_id"`
	RedeemedAt           *time.Time     `gorm:"column:redeemed_at"`
	ExpiresAt            *time.Time     `gorm:"column:expires_at"`
	CreatedBy            *string        `gorm:"column:created_by"`
	UpdatedBy            *string        `gorm:"column:updated_by"`
	CreatedAt            time.Time      `gorm:"column:created_at"`
	UpdatedAt            time.Time      `gorm:"column:updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"column:deleted_at"`
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

type RechargePackage struct {
	ID                  string         `gorm:"column:id;primaryKey"`
	PackageID           string         `gorm:"column:package_id"`
	PackageType         string         `gorm:"column:package_type"`
	TargetScope         string         `gorm:"column:target_scope"`
	BillingMode         string         `gorm:"column:billing_mode"`
	DisplayName         string         `gorm:"column:display_name"`
	Name                string         `gorm:"column:name"`
	Points              int64          `gorm:"column:points"`
	GrantedPoints       int64          `gorm:"column:granted_points"`
	BonusPoints         int64          `gorm:"column:bonus_points"`
	PriceCents          int64          `gorm:"column:price_cents"`
	PriceAmount         int64          `gorm:"column:price_amount"`
	Currency            string         `gorm:"column:currency"`
	CreditValidDuration string         `gorm:"column:credit_valid_duration"`
	CreditExpiryPolicy  string         `gorm:"column:credit_expiry_policy"`
	SpendScopeJSON      datatypes.JSON `gorm:"column:spend_scope_json;type:jsonb"`
	SettlementEligible  bool           `gorm:"column:settlement_eligible"`
	EntitlementPolicy   datatypes.JSON `gorm:"column:entitlement_policy_json;type:jsonb"`
	RenewalPolicy       datatypes.JSON `gorm:"column:renewal_policy_json;type:jsonb"`
	RefundPolicy        datatypes.JSON `gorm:"column:refund_policy_json;type:jsonb"`
	VisibleScope        string         `gorm:"column:visible_scope"`
	Status              string         `gorm:"column:status"`
	CreatedBy           *string        `gorm:"column:created_by"`
	UpdatedBy           *string        `gorm:"column:updated_by"`
	CreatedAt           time.Time      `gorm:"column:created_at"`
	UpdatedAt           time.Time      `gorm:"column:updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (RechargePackage) TableName() string { return "recharge_packages" }

type BillingPackageSKU struct {
	ID                  string         `gorm:"column:id;primaryKey"`
	SKUID               string         `gorm:"column:sku_id"`
	PackageID           string         `gorm:"column:package_id"`
	ChannelCode         string         `gorm:"column:channel_code"`
	PriceAmount         int64          `gorm:"column:price_amount"`
	Currency            string         `gorm:"column:currency"`
	ActivityPriceAmount *int64         `gorm:"column:activity_price_amount"`
	EffectiveAt         time.Time      `gorm:"column:effective_at"`
	ExpiredAt           *time.Time     `gorm:"column:expired_at"`
	Status              string         `gorm:"column:status"`
	CreatedBy           *string        `gorm:"column:created_by"`
	UpdatedBy           *string        `gorm:"column:updated_by"`
	CreatedAt           time.Time      `gorm:"column:created_at"`
	UpdatedAt           time.Time      `gorm:"column:updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (BillingPackageSKU) TableName() string { return "billing_package_skus" }

type RechargeOrder struct {
	ID                    string         `gorm:"column:id;primaryKey"`
	OrderID               string         `gorm:"column:order_id"`
	UserID                string         `gorm:"column:user_id"`
	EnterpriseID          *string        `gorm:"column:enterprise_id"`
	AccountID             string         `gorm:"column:account_id"`
	PackageID             string         `gorm:"column:package_id"`
	SKUID                 *string        `gorm:"column:sku_id"`
	PackageType           string         `gorm:"column:package_type"`
	TargetScope           string         `gorm:"column:target_scope"`
	BillingMode           string         `gorm:"column:billing_mode"`
	Points                int64          `gorm:"column:points"`
	GrantedPoints         int64          `gorm:"column:granted_points"`
	BonusPoints           int64          `gorm:"column:bonus_points"`
	PriceCents            int64          `gorm:"column:price_cents"`
	PriceAmount           int64          `gorm:"column:price_amount"`
	Currency              string         `gorm:"column:currency"`
	PaymentProvider       string         `gorm:"column:payment_provider"`
	PaymentStatus         string         `gorm:"column:payment_status"`
	CreditLotID           *string        `gorm:"column:credit_lot_id"`
	EntitlementSnapshotID *string        `gorm:"column:entitlement_snapshot_id"`
	OrderSource           string         `gorm:"column:order_source"`
	IdempotencyKey        string         `gorm:"column:idempotency_key"`
	TraceID               *string        `gorm:"column:trace_id"`
	PaidAt                *time.Time     `gorm:"column:paid_at"`
	FailedReason          *string        `gorm:"column:failed_reason"`
	CreatedBy             *string        `gorm:"column:created_by"`
	UpdatedBy             *string        `gorm:"column:updated_by"`
	CreatedAt             time.Time      `gorm:"column:created_at"`
	UpdatedAt             time.Time      `gorm:"column:updated_at"`
	DeletedAt             gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (RechargeOrder) TableName() string { return "recharge_orders" }

type MockPaymentTransaction struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	TransactionID      string         `gorm:"column:transaction_id"`
	OrderID            string         `gorm:"column:order_id"`
	PaymentResult      string         `gorm:"column:payment_result"`
	PaymentStatus      string         `gorm:"column:payment_status"`
	IdempotencyKey     string         `gorm:"column:idempotency_key"`
	TraceID            *string        `gorm:"column:trace_id"`
	RequestPayloadJSON datatypes.JSON `gorm:"column:request_payload_json;type:jsonb"`
	CreatedBy          *string        `gorm:"column:created_by"`
	UpdatedBy          *string        `gorm:"column:updated_by"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (MockPaymentTransaction) TableName() string { return "mock_payment_transactions" }

type PackageEntitlementSnapshot struct {
	ID                    string         `gorm:"column:id;primaryKey"`
	EntitlementSnapshotID string         `gorm:"column:entitlement_snapshot_id"`
	AccountID             string         `gorm:"column:account_id"`
	UserID                *string        `gorm:"column:user_id"`
	EnterpriseID          *string        `gorm:"column:enterprise_id"`
	PackageID             string         `gorm:"column:package_id"`
	OrderID               string         `gorm:"column:order_id"`
	TargetScope           string         `gorm:"column:target_scope"`
	EntitlementPolicy     datatypes.JSON `gorm:"column:entitlement_policy_json;type:jsonb"`
	Status                string         `gorm:"column:status"`
	EffectiveAt           time.Time      `gorm:"column:effective_at"`
	ExpiresAt             *time.Time     `gorm:"column:expires_at"`
	CreatedBy             *string        `gorm:"column:created_by"`
	UpdatedBy             *string        `gorm:"column:updated_by"`
	CreatedAt             time.Time      `gorm:"column:created_at"`
	UpdatedAt             time.Time      `gorm:"column:updated_at"`
	DeletedAt             gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (PackageEntitlementSnapshot) TableName() string { return "package_entitlement_snapshots" }

type EnterpriseContract struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	ContractID         string         `gorm:"column:contract_id"`
	EnterpriseID       string         `gorm:"column:enterprise_id"`
	PackageID          string         `gorm:"column:package_id"`
	OrderID            *string        `gorm:"column:order_id"`
	ContractStatus     string         `gorm:"column:contract_status"`
	BillingMode        string         `gorm:"column:billing_mode"`
	PeriodStart        time.Time      `gorm:"column:period_start"`
	PeriodEnd          *time.Time     `gorm:"column:period_end"`
	SeatQuota          int            `gorm:"column:seat_quota"`
	BudgetPoints       int64          `gorm:"column:budget_points"`
	ApprovalPolicyJSON datatypes.JSON `gorm:"column:approval_policy_json;type:jsonb"`
	InvoicePolicyJSON  datatypes.JSON `gorm:"column:invoice_policy_json;type:jsonb"`
	CreatedBy          *string        `gorm:"column:created_by"`
	UpdatedBy          *string        `gorm:"column:updated_by"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (EnterpriseContract) TableName() string { return "enterprise_contracts" }

type BillingInvoice struct {
	ID            string         `gorm:"column:id;primaryKey"`
	InvoiceID     string         `gorm:"column:invoice_id"`
	EnterpriseID  *string        `gorm:"column:enterprise_id"`
	OrderID       *string        `gorm:"column:order_id"`
	Amount        int64          `gorm:"column:amount"`
	Currency      string         `gorm:"column:currency"`
	InvoiceStatus string         `gorm:"column:invoice_status"`
	IssuedAt      *time.Time     `gorm:"column:issued_at"`
	DueAt         *time.Time     `gorm:"column:due_at"`
	MetadataJSON  datatypes.JSON `gorm:"column:metadata_json;type:jsonb"`
	CreatedBy     *string        `gorm:"column:created_by"`
	UpdatedBy     *string        `gorm:"column:updated_by"`
	CreatedAt     time.Time      `gorm:"column:created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (BillingInvoice) TableName() string { return "billing_invoices" }

type BillingPromotion struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	PromotionID        string         `gorm:"column:promotion_id"`
	PromotionName      string         `gorm:"column:promotion_name"`
	PackageID          *string        `gorm:"column:package_id"`
	DiscountPolicyJSON datatypes.JSON `gorm:"column:discount_policy_json;type:jsonb"`
	VisibleScope       string         `gorm:"column:visible_scope"`
	Status             string         `gorm:"column:status"`
	StartsAt           time.Time      `gorm:"column:starts_at"`
	EndsAt             *time.Time     `gorm:"column:ends_at"`
	CreatedBy          *string        `gorm:"column:created_by"`
	UpdatedBy          *string        `gorm:"column:updated_by"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (BillingPromotion) TableName() string { return "billing_promotions" }

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
	CreatedBy     *string        `gorm:"column:created_by"`
	UpdatedBy     *string        `gorm:"column:updated_by"`
	CreatedAt     time.Time      `gorm:"column:created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"column:deleted_at"`
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
	CreatedBy      *string        `gorm:"column:created_by"`
	UpdatedBy      *string        `gorm:"column:updated_by"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (AssetStorageObject) TableName() string { return "asset_storage_objects" }

type UploadIntent struct {
	ID               string         `gorm:"column:id;primaryKey"`
	UploadIntentID   string         `gorm:"column:upload_intent_id"`
	OwnerUserID      string         `gorm:"column:owner_user_id"`
	SpaceID          string         `gorm:"column:space_id"`
	ProjectID        *string        `gorm:"column:project_id"`
	AssetType        string         `gorm:"column:asset_type"`
	Bucket           *string        `gorm:"column:bucket"`
	ObjectKey        *string        `gorm:"column:object_key"`
	ObjectKeyHash    string         `gorm:"column:object_key_digest"`
	MIMEType         *string        `gorm:"column:mime_type"`
	MaxSizeBytes     int64          `gorm:"column:max_size_bytes"`
	Status           string         `gorm:"column:status"`
	ExpiresAt        time.Time      `gorm:"column:expires_at"`
	ConfirmedAssetID *string        `gorm:"column:confirmed_asset_id"`
	IdempotencyKey   string         `gorm:"column:idempotency_key"`
	TraceID          string         `gorm:"column:trace_id"`
	CreatedBy        *string        `gorm:"column:created_by"`
	UpdatedBy        *string        `gorm:"column:updated_by"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at"`
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
	CreatedBy          *string        `gorm:"column:created_by"`
	UpdatedBy          *string        `gorm:"column:updated_by"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at"`
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
	CreatedBy       *string        `gorm:"column:created_by"`
	UpdatedBy       *string        `gorm:"column:updated_by"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
	UpdatedAt       time.Time      `gorm:"column:updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (GeneratedAssetObjectSlot) TableName() string { return "generated_asset_object_slots" }

type AssetCommitBatch struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	CommitID           string         `gorm:"column:commit_id"`
	ProjectID          string         `gorm:"column:project_id"`
	SessionID          string         `gorm:"column:session_id"`
	RunID              string         `gorm:"column:run_id"`
	FreezeID           string         `gorm:"column:freeze_id"`
	EstimateID         *string        `gorm:"column:estimate_id"`
	ActorUserID        string         `gorm:"column:actor_user_id"`
	SpaceID            string         `gorm:"column:space_id"`
	SafetyEvidenceID   string         `gorm:"column:safety_evidence_id"`
	SafetyEvidenceHash string         `gorm:"column:safety_evidence_digest"`
	ChargedPoints      int64          `gorm:"column:charged_points"`
	ReleasedPoints     int64          `gorm:"column:released_points"`
	CommitStatus       string         `gorm:"column:commit_status"`
	LedgerRef          *string        `gorm:"column:ledger_ref"`
	IdempotencyKey     string         `gorm:"column:idempotency_key"`
	TraceID            string         `gorm:"column:trace_id"`
	CreatedBy          *string        `gorm:"column:created_by"`
	UpdatedBy          *string        `gorm:"column:updated_by"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at"`
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
	CreatedBy           *string        `gorm:"column:created_by"`
	UpdatedBy           *string        `gorm:"column:updated_by"`
	CreatedAt           time.Time      `gorm:"column:created_at"`
	DeletedAt           gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (AssetCommitItem) TableName() string { return "asset_commit_items" }
