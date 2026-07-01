package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/skillmarket"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuthContext = accountspace.AuthContext
type RequestMeta = accountspace.RequestMeta

type App struct {
	repo *businesscore.Repository
	now  func() time.Time
}

func New(repo *businesscore.Repository) *App {
	return &App{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type MarketplaceListingDTO struct {
	ListingID           string     `json:"listing_id"`
	SkillID             string     `json:"skill_id"`
	SkillVersionID      string     `json:"skill_version_id"`
	SkillVersion        string     `json:"skill_version"`
	SkillName           string     `json:"skill_name"`
	SkillDescription    string     `json:"skill_description"`
	CreatorUserID       string     `json:"creator_user_id"`
	Status              string     `json:"status"`
	PricingModel        string     `json:"pricing_model"`
	UsageCredits        int        `json:"usage_credits"`
	ValueDeliveredStage string     `json:"value_delivered_stage"`
	PricingPolicyDigest string     `json:"pricing_policy_digest"`
	PublishedBy         string     `json:"published_by"`
	ListedAt            *time.Time `json:"listed_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SkillInstallationDTO struct {
	InstallationID   string    `json:"installation_id"`
	AccountID        string    `json:"account_id"`
	AccountScope     string    `json:"account_scope"`
	ListingID        string    `json:"listing_id"`
	SkillID          string    `json:"skill_id"`
	SkillName        string    `json:"skill_name,omitempty"`
	InstalledVersion string    `json:"installed_version"`
	VersionStrategy  string    `json:"version_strategy"`
	Status           string    `json:"status"`
	UpgradeStatus    string    `json:"upgrade_status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SkillUsageRecordDTO struct {
	UsageID             string     `json:"usage_id"`
	RunID               string     `json:"run_id"`
	ListingID           string     `json:"listing_id"`
	SkillID             string     `json:"skill_id"`
	SkillVersion        string     `json:"skill_version"`
	PricingPolicyDigest string     `json:"pricing_policy_digest"`
	SkillUsageDigest    string     `json:"skill_usage_digest"`
	UsageStatus         string     `json:"usage_status"`
	ChargeStatus        string     `json:"charge_status"`
	RefundStatus        string     `json:"refund_status"`
	SettlementStatus    string     `json:"settlement_status"`
	EstimatedCredits    int        `json:"estimated_credits"`
	CreditHoldID        *string    `json:"credit_hold_id,omitempty"`
	ValueDeliveredAt    *time.Time `json:"value_delivered_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SkillSettlementDTO struct {
	SettlementID       string    `json:"settlement_id"`
	UsageID            string    `json:"usage_id"`
	CreatorUserID      string    `json:"creator_user_id"`
	Status             string    `json:"status"`
	GrossCredits       int       `json:"gross_credits"`
	PlatformFeeCredits int       `json:"platform_fee_credits"`
	CreatorCredits     int       `json:"creator_credits"`
	HoldUntil          time.Time `json:"hold_until"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type SkillSettlementPayoutDTO struct {
	PayoutID        string    `json:"payout_id"`
	SettlementID    string    `json:"settlement_id"`
	CreatorUserID   string    `json:"creator_user_id"`
	Action          string    `json:"action"`
	StatusBefore    string    `json:"status_before"`
	StatusAfter     string    `json:"status_after"`
	PayoutReference string    `json:"payout_reference,omitempty"`
	ReasonCode      string    `json:"reason_code"`
	OperatorAdminID string    `json:"operator_admin_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreatorSkillDTO struct {
	SkillID             string     `json:"skill_id"`
	Name                string     `json:"name"`
	Description         string     `json:"description"`
	Visibility          string     `json:"visibility"`
	Version             string     `json:"version"`
	SkillVersionID      string     `json:"skill_version_id"`
	VersionStatus       string     `json:"version_status"`
	RuntimeSpecDigest   string     `json:"runtime_spec_digest"`
	PricingPolicyDigest string     `json:"pricing_policy_digest"`
	PricingModel        string     `json:"pricing_model"`
	UsageCredits        int        `json:"usage_credits"`
	ValueDeliveredStage string     `json:"value_delivered_stage"`
	ReviewID            string     `json:"review_id,omitempty"`
	ReviewStatus        string     `json:"review_status"`
	ListingID           string     `json:"listing_id,omitempty"`
	ListingStatus       string     `json:"listing_status"`
	SubmittedAt         *time.Time `json:"submitted_at,omitempty"`
	PublishedAt         *time.Time `json:"published_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type ListMarketplaceSkillsInput struct {
	Auth   AuthContext
	Query  string
	Limit  int
	Cursor string
}

type ListMarketplaceSkillsOutput struct {
	Items      []MarketplaceListingDTO `json:"items"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

type GetMarketplaceSkillOutput struct {
	Listing MarketplaceListingDTO `json:"listing"`
}

type InstallSkillInput struct {
	Auth         AuthContext
	Meta         RequestMeta
	ListingID    string
	TargetScope  string
	EnterpriseID string
}

type InstallSkillOutput struct {
	Installation     SkillInstallationDTO `json:"installation"`
	IdempotentReplay bool                 `json:"idempotent_replay"`
}

type ListInstalledSkillsInput struct {
	Auth         AuthContext
	AccountScope string
	Limit        int
	Offset       int
}

type ListInstalledSkillsOutput struct {
	Items []SkillInstallationDTO `json:"items"`
}

type UpgradeSkillInstallationInput struct {
	Auth           AuthContext
	Meta           RequestMeta
	InstallationID string
	TargetVersion  string
	Confirmed      bool
}

type UpgradeSkillInstallationOutput struct {
	Installation SkillInstallationDTO `json:"installation"`
}

type EstimateSkillUsageCreditsInput struct {
	Auth                AuthContext
	Meta                RequestMeta
	RunID               string
	ListingID           string
	PricingPolicyDigest string
}

type EstimateSkillUsageCreditsOutput struct {
	EstimatedCredits    int       `json:"estimated_credits"`
	PricingPolicyDigest string    `json:"pricing_policy_digest"`
	SkillUsageDigest    string    `json:"skill_usage_digest"`
	ExpiresAt           time.Time `json:"expires_at"`
}

type CreateSkillUsageRecordInput struct {
	Auth                AuthContext
	Meta                RequestMeta
	RunID               string
	ListingID           string
	SkillID             string
	SkillVersion        string
	PricingPolicyDigest string
	SkillUsageDigest    string
	EstimatedCredits    int
}

type CreateSkillUsageRecordOutput struct {
	Usage            SkillUsageRecordDTO `json:"usage"`
	IdempotentReplay bool                `json:"idempotent_replay"`
}

type FreezeSkillUsageCreditsInput struct {
	Auth             AuthContext
	UsageID          string
	SkillUsageDigest string
	CreditHoldID     string
}

type FreezeSkillUsageCreditsOutput struct {
	Usage SkillUsageRecordDTO `json:"usage"`
}

type ReleaseSkillUsageFreezeInput struct {
	Auth          AuthContext
	UsageID       string
	ReleaseReason string
}

type ReleaseSkillUsageFreezeOutput struct {
	Usage SkillUsageRecordDTO `json:"usage"`
}

type CommitSkillUsageAndSettleInput struct {
	Auth         AuthContext
	UsageID      string
	CreditHoldID string
}

type CommitSkillUsageAndSettleOutput struct {
	Usage      SkillUsageRecordDTO `json:"usage"`
	Settlement SkillSettlementDTO  `json:"settlement"`
}

type CreateCreatorSkillDraftInput struct {
	Auth        AuthContext
	Meta        RequestMeta
	Name        string
	Description string
}

type CreateCreatorSkillDraftOutput struct {
	Skill CreatorSkillDTO `json:"skill"`
}

type SubmitCreatorSkillVersionInput struct {
	Auth    AuthContext
	Meta    RequestMeta
	SkillID string
	Version string
}

type SubmitCreatorSkillVersionOutput struct {
	SkillVersion CreatorSkillDTO `json:"skill_version"`
}

type ListCreatorListingsInput struct {
	Auth  AuthContext
	Limit int
}

type ListCreatorListingsOutput struct {
	Items []CreatorSkillDTO `json:"items"`
}

type CreatorSkillUsageAnalyticsOutput struct {
	UsageCount         int64          `json:"usage_count"`
	RevenueHoldAmount  int64          `json:"revenue_hold_amount"`
	RefundCount        int64          `json:"refund_count"`
	FailureCodeSummary map[string]int `json:"failure_code_summary"`
}

type AdminPage[T any] struct {
	Items  []T   `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

type AdminSkillReviewDTO struct {
	ReviewID            string     `json:"review_id"`
	SkillID             string     `json:"skill_id"`
	SkillVersionID      string     `json:"skill_version_id"`
	SkillVersion        string     `json:"skill_version"`
	SkillName           string     `json:"skill_name"`
	SkillDescription    string     `json:"skill_description"`
	CreatorUserID       string     `json:"creator_user_id"`
	Status              string     `json:"status"`
	VersionStatus       string     `json:"version_status"`
	RuntimeSpecDigest   string     `json:"runtime_spec_digest"`
	PricingPolicyDigest string     `json:"pricing_policy_digest"`
	PricingModel        string     `json:"pricing_model"`
	UsageCredits        int        `json:"usage_credits"`
	ValueDeliveredStage string     `json:"value_delivered_stage"`
	ListingID           string     `json:"listing_id,omitempty"`
	ListingStatus       string     `json:"listing_status"`
	ReviewerID          string     `json:"reviewer_id,omitempty"`
	DecisionReason      string     `json:"decision_reason,omitempty"`
	SubmittedAt         *time.Time `json:"submitted_at,omitempty"`
	PublishedAt         *time.Time `json:"published_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type AdminRefundCaseDTO struct {
	RefundCaseID     string    `json:"refund_case_id"`
	UsageID          string    `json:"usage_id"`
	SettlementID     string    `json:"settlement_id,omitempty"`
	Status           string    `json:"status"`
	ReasonCode       string    `json:"reason_code"`
	CreatedBy        string    `json:"created_by"`
	SkillID          string    `json:"skill_id"`
	SkillName        string    `json:"skill_name"`
	ListingID        string    `json:"listing_id"`
	CreatorUserID    string    `json:"creator_user_id"`
	UsageStatus      string    `json:"usage_status"`
	ChargeStatus     string    `json:"charge_status"`
	RefundStatus     string    `json:"refund_status"`
	SettlementStatus string    `json:"settlement_status"`
	EstimatedCredits int       `json:"estimated_credits"`
	CreatorCredits   int       `json:"creator_credits"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AdminSettlementDTO struct {
	SettlementID       string    `json:"settlement_id"`
	UsageID            string    `json:"usage_id"`
	SkillID            string    `json:"skill_id"`
	SkillName          string    `json:"skill_name"`
	CreatorUserID      string    `json:"creator_user_id"`
	Status             string    `json:"status"`
	GrossCredits       int       `json:"gross_credits"`
	PlatformFeeCredits int       `json:"platform_fee_credits"`
	CreatorCredits     int       `json:"creator_credits"`
	HoldUntil          time.Time `json:"hold_until"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ListAdminSkillReviewsInput struct {
	AdminID string
	Status  string
	Keyword string
	Limit   int
	Offset  int
}

type ApproveSkillReviewInput struct {
	AdminID  string
	ReviewID string
	Reason   string
}

type ApproveSkillReviewOutput struct {
	SkillVersion AdminSkillReviewDTO   `json:"skill_version"`
	Listing      MarketplaceListingDTO `json:"listing"`
}

type ListAdminMarketplaceListingsInput struct {
	AdminID string
	Status  string
	Keyword string
	Limit   int
	Offset  int
}

type SuspendMarketplaceListingInput struct {
	AdminID    string
	ListingID  string
	ReasonCode string
}

type SuspendMarketplaceListingOutput struct {
	Listing MarketplaceListingDTO `json:"listing"`
}

type ListAdminRefundCasesInput struct {
	AdminID string
	Status  string
	Limit   int
	Offset  int
}

type ApproveSkillUsageRefundInput struct {
	AdminID      string
	RefundCaseID string
}

type ApproveSkillUsageRefundOutput struct {
	Usage      SkillUsageRecordDTO `json:"usage"`
	Settlement SkillSettlementDTO  `json:"settlement"`
}

type ListAdminSettlementsInput struct {
	AdminID string
	Status  string
	Limit   int
	Offset  int
}

type ReleaseSkillSettlementHoldInput struct {
	AdminID      string
	Meta         RequestMeta
	SettlementID string
	ReasonCode   string
}

type ConfirmSkillSettlementPayoutInput struct {
	AdminID         string
	Meta            RequestMeta
	SettlementID    string
	PayoutReference string
	ReasonCode      string
}

type SkillSettlementGovernanceOutput struct {
	Settlement SkillSettlementDTO       `json:"settlement"`
	Payout     SkillSettlementPayoutDTO `json:"payout"`
}

type listingRow struct {
	ListingID           string     `gorm:"column:listing_id"`
	SkillID             string     `gorm:"column:skill_id"`
	SkillVersionID      string     `gorm:"column:skill_version_id"`
	SkillVersion        string     `gorm:"column:skill_version"`
	SkillName           string     `gorm:"column:skill_name"`
	SkillDescription    string     `gorm:"column:skill_description"`
	CreatorUserID       string     `gorm:"column:creator_user_id"`
	Status              string     `gorm:"column:status"`
	PricingModel        string     `gorm:"column:pricing_model"`
	UsageCredits        int        `gorm:"column:usage_credits"`
	ValueDeliveredStage string     `gorm:"column:value_delivered_stage"`
	PricingPolicyDigest string     `gorm:"column:pricing_policy_digest"`
	PublishedBy         string     `gorm:"column:published_by"`
	ListedAt            *time.Time `gorm:"column:listed_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

type adminSkillReviewRow struct {
	ReviewID            string     `gorm:"column:review_id"`
	SkillID             string     `gorm:"column:skill_id"`
	SkillVersionID      string     `gorm:"column:skill_version_id"`
	SkillVersion        string     `gorm:"column:skill_version"`
	SkillName           string     `gorm:"column:skill_name"`
	SkillDescription    string     `gorm:"column:skill_description"`
	CreatorUserID       string     `gorm:"column:creator_user_id"`
	Status              string     `gorm:"column:status"`
	VersionStatus       string     `gorm:"column:version_status"`
	RuntimeSpecDigest   string     `gorm:"column:runtime_spec_digest"`
	PricingPolicyDigest string     `gorm:"column:pricing_policy_digest"`
	PricingModel        string     `gorm:"column:pricing_model"`
	UsageCredits        int        `gorm:"column:usage_credits"`
	ValueDeliveredStage string     `gorm:"column:value_delivered_stage"`
	ListingID           string     `gorm:"column:listing_id"`
	ListingStatus       string     `gorm:"column:listing_status"`
	ReviewerID          string     `gorm:"column:reviewer_id"`
	DecisionReason      string     `gorm:"column:decision_reason"`
	SubmittedAt         *time.Time `gorm:"column:submitted_at"`
	PublishedAt         *time.Time `gorm:"column:published_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

type adminRefundCaseRow struct {
	RefundCaseID     string    `gorm:"column:refund_case_id"`
	UsageID          string    `gorm:"column:usage_id"`
	SettlementID     string    `gorm:"column:settlement_id"`
	Status           string    `gorm:"column:status"`
	ReasonCode       string    `gorm:"column:reason_code"`
	CreatedBy        string    `gorm:"column:created_by"`
	SkillID          string    `gorm:"column:skill_id"`
	SkillName        string    `gorm:"column:skill_name"`
	ListingID        string    `gorm:"column:listing_id"`
	CreatorUserID    string    `gorm:"column:creator_user_id"`
	UsageStatus      string    `gorm:"column:usage_status"`
	ChargeStatus     string    `gorm:"column:charge_status"`
	RefundStatus     string    `gorm:"column:refund_status"`
	SettlementStatus string    `gorm:"column:settlement_status"`
	EstimatedCredits int       `gorm:"column:estimated_credits"`
	CreatorCredits   int       `gorm:"column:creator_credits"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at"`
}

type adminSettlementRow struct {
	SettlementID       string    `gorm:"column:settlement_id"`
	UsageID            string    `gorm:"column:usage_id"`
	SkillID            string    `gorm:"column:skill_id"`
	SkillName          string    `gorm:"column:skill_name"`
	CreatorUserID      string    `gorm:"column:creator_user_id"`
	Status             string    `gorm:"column:status"`
	GrossCredits       int       `gorm:"column:gross_credits"`
	PlatformFeeCredits int       `gorm:"column:platform_fee_credits"`
	CreatorCredits     int       `gorm:"column:creator_credits"`
	HoldUntil          time.Time `gorm:"column:hold_until"`
	CreatedAt          time.Time `gorm:"column:created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

type installationRow struct {
	InstallationID   string    `gorm:"column:installation_id"`
	AccountID        string    `gorm:"column:account_id"`
	AccountScope     string    `gorm:"column:account_scope"`
	ListingID        string    `gorm:"column:listing_id"`
	SkillID          string    `gorm:"column:skill_id"`
	SkillName        string    `gorm:"column:skill_name"`
	InstalledVersion string    `gorm:"column:installed_version"`
	VersionStrategy  string    `gorm:"column:version_strategy"`
	Status           string    `gorm:"column:status"`
	UpgradeStatus    string    `gorm:"column:upgrade_status"`
	CreatedAt        time.Time `gorm:"column:created_at"`
	UpdatedAt        time.Time `gorm:"column:updated_at"`
}

type creatorSkillRow struct {
	SkillID             string     `gorm:"column:skill_id"`
	Name                string     `gorm:"column:name"`
	Description         string     `gorm:"column:description"`
	Visibility          string     `gorm:"column:visibility"`
	Version             string     `gorm:"column:version"`
	SkillVersionID      string     `gorm:"column:skill_version_id"`
	VersionStatus       string     `gorm:"column:version_status"`
	RuntimeSpecDigest   string     `gorm:"column:runtime_spec_digest"`
	PricingPolicyDigest string     `gorm:"column:pricing_policy_digest"`
	PricingModel        string     `gorm:"column:pricing_model"`
	UsageCredits        int        `gorm:"column:usage_credits"`
	ValueDeliveredStage string     `gorm:"column:value_delivered_stage"`
	ReviewID            string     `gorm:"column:review_id"`
	ReviewStatus        string     `gorm:"column:review_status"`
	ListingID           string     `gorm:"column:listing_id"`
	ListingStatus       string     `gorm:"column:listing_status"`
	SubmittedAt         *time.Time `gorm:"column:submitted_at"`
	PublishedAt         *time.Time `gorm:"column:published_at"`
	CreatedAt           time.Time  `gorm:"column:created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at"`
}

func (a *App) CreateCreatorSkillDraft(ctx context.Context, in CreateCreatorSkillDraftInput) (CreateCreatorSkillDraftOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return CreateCreatorSkillDraftOutput{}, err
	}
	if strings.TrimSpace(in.Meta.IdempotencyKey) == "" {
		return CreateCreatorSkillDraftOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "idempotency_key is required")
	}
	name := strings.TrimSpace(in.Name)
	description := strings.TrimSpace(in.Description)
	if name == "" {
		return CreateCreatorSkillDraftOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "name is required")
	}
	if len([]rune(name)) > 80 {
		return CreateCreatorSkillDraftOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "name must be <= 80 characters")
	}
	if len([]rune(description)) > 1000 {
		return CreateCreatorSkillDraftOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "description must be <= 1000 characters")
	}

	now := a.now().UTC()
	version := "v1"
	skillID := prefixedStableID("skill_", in.Auth.UserID, in.Meta.IdempotencyKey)
	skillVersionID := prefixedStableID("skv_", skillID, version)
	pricingPolicyID := prefixedStableID("spp_", skillID, version)
	runtimeDigest, err := foundation.CanonicalDigest(map[string]any{
		"schema_version": "creator_skill_runtime_spec.v1",
		"skill_id":       skillID,
		"version":        version,
		"name":           name,
		"description":    description,
	})
	if err != nil {
		return CreateCreatorSkillDraftOutput{}, err
	}
	pricingDigest, err := foundation.CanonicalDigest(map[string]any{
		"schema_version":         "skill_pricing_policy.v1",
		"skill_id":               skillID,
		"skill_version":          version,
		"pricing_model":          skillmarket.PricingModelFree,
		"usage_credits":          0,
		"value_delivered_stage":  "storyboard_ready",
		"two_stage_confirmation": true,
	})
	if err != nil {
		return CreateCreatorSkillDraftOutput{}, err
	}

	currentVersion := version
	pkg := skillmarket.SkillPackage{
		SchemaVersion:  skillmarket.SchemaVersionSkillPackage,
		SkillID:        skillID,
		CreatorUserID:  in.Auth.UserID,
		Name:           name,
		Description:    description,
		Visibility:     skillmarket.SkillVisibilityReviewOnly,
		CurrentVersion: &currentVersion,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	skillVersion := skillmarket.SkillVersion{
		SchemaVersion:       skillmarket.SchemaVersionSkillVersion,
		SkillVersionID:      skillVersionID,
		SkillID:             skillID,
		Version:             version,
		Status:              "draft",
		RuntimeSpecDigest:   runtimeDigest,
		PricingPolicyDigest: pricingDigest,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	policy := skillmarket.SkillPricingPolicy{
		SchemaVersion:       skillmarket.SchemaVersionSkillPricingPolicy,
		PricingPolicyID:     pricingPolicyID,
		SkillID:             skillID,
		SkillVersion:        version,
		PricingModel:        skillmarket.PricingModelFree,
		UsageCredits:        0,
		ValueDeliveredStage: "storyboard_ready",
		PricingPolicyDigest: pricingDigest,
		CreatedAt:           now,
	}
	if err := skillmarket.ValidateSkillPackage(pkg); err != nil {
		return CreateCreatorSkillDraftOutput{}, err
	}
	if err := skillmarket.ValidateSkillVersion(skillVersion); err != nil {
		return CreateCreatorSkillDraftOutput{}, err
	}
	if err := skillmarket.ValidateSkillPricingPolicy(policy); err != nil {
		return CreateCreatorSkillDraftOutput{}, err
	}

	if err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(skillPackageRecordFromContract(pkg)).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(skillVersionRecordFromContract(skillVersion)).Error; err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(skillPricingPolicyRecordFromContract(policy)).Error
	}); err != nil {
		return CreateCreatorSkillDraftOutput{}, mapStoreError(err)
	}
	skill, err := a.getCreatorSkill(ctx, in.Auth, skillID)
	if err != nil {
		return CreateCreatorSkillDraftOutput{}, err
	}
	return CreateCreatorSkillDraftOutput{Skill: skill}, nil
}

func (a *App) SubmitCreatorSkillVersion(ctx context.Context, in SubmitCreatorSkillVersionInput) (SubmitCreatorSkillVersionOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return SubmitCreatorSkillVersionOutput{}, err
	}
	if strings.TrimSpace(in.Meta.IdempotencyKey) == "" {
		return SubmitCreatorSkillVersionOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "idempotency_key is required")
	}
	skillID := strings.TrimSpace(in.SkillID)
	version := strings.TrimSpace(in.Version)
	if skillID == "" || version == "" {
		return SubmitCreatorSkillVersionOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "skill_id and version are required")
	}
	now := a.now().UTC()
	if err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var pkg businesscore.MarketplaceSkillPackageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("skill_id = ? AND creator_user_id = ?", skillID, in.Auth.UserID).
			First(&pkg).Error; err != nil {
			return err
		}
		var skillVersion businesscore.MarketplaceSkillVersionRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("skill_id = ? AND version = ?", skillID, version).
			First(&skillVersion).Error; err != nil {
			return err
		}
		switch skillVersion.Status {
		case "published", "deprecated", "removed":
			return bizerrors.New(bizerrors.CodeStateConflict, "skill version cannot be submitted from current status")
		case "submitted", "reviewing":
		default:
			if err := tx.Model(&businesscore.MarketplaceSkillVersionRecord{}).
				Where("skill_version_id = ?", skillVersion.SkillVersionID).
				Updates(map[string]any{"status": "submitted", "submitted_at": now, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		review := businesscore.MarketplaceSkillReviewRecord{
			ReviewID:       prefixedStableID("review_", skillID, skillVersion.SkillVersionID),
			SkillID:        skillID,
			SkillVersionID: skillVersion.SkillVersionID,
			Status:         "submitted",
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&review).Error; err != nil {
			return err
		}
		return tx.Model(&businesscore.MarketplaceSkillPackageRecord{}).
			Where("skill_id = ?", skillID).
			Updates(map[string]any{"visibility": skillmarket.SkillVisibilityReviewOnly, "updated_at": now}).Error
	}); err != nil {
		return SubmitCreatorSkillVersionOutput{}, mapStoreError(err)
	}
	skill, err := a.getCreatorSkill(ctx, in.Auth, skillID)
	if err != nil {
		return SubmitCreatorSkillVersionOutput{}, err
	}
	return SubmitCreatorSkillVersionOutput{SkillVersion: skill}, nil
}

func (a *App) ListCreatorListings(ctx context.Context, in ListCreatorListingsInput) (ListCreatorListingsOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return ListCreatorListingsOutput{}, err
	}
	limit := normalizeLimit(in.Limit)
	var rows []creatorSkillRow
	if err := a.creatorSkillQuery(ctx, in.Auth.UserID).
		Order("sp.updated_at DESC, sv.updated_at DESC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return ListCreatorListingsOutput{}, err
	}
	items := make([]CreatorSkillDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, creatorSkillDTO(row))
	}
	return ListCreatorListingsOutput{Items: items}, nil
}

func (a *App) GetCreatorSkillUsageAnalytics(ctx context.Context, auth AuthContext) (CreatorSkillUsageAnalyticsOutput, error) {
	if err := requireAuth(auth); err != nil {
		return CreatorSkillUsageAnalyticsOutput{}, err
	}
	var usageCount int64
	if err := a.repo.DB().WithContext(ctx).Table("skill_usage_records AS su").
		Joins("JOIN skill_packages AS sp ON sp.skill_id = su.skill_id").
		Where("sp.creator_user_id = ?", auth.UserID).
		Count(&usageCount).Error; err != nil {
		return CreatorSkillUsageAnalyticsOutput{}, err
	}
	var revenueHold struct {
		Total int64 `gorm:"column:total"`
	}
	if err := a.repo.DB().WithContext(ctx).Table("skill_settlement_records").
		Select("COALESCE(SUM(creator_credits), 0) AS total").
		Where("creator_user_id = ? AND status = ?", auth.UserID, "pending_hold").
		Scan(&revenueHold).Error; err != nil {
		return CreatorSkillUsageAnalyticsOutput{}, err
	}
	var refundCount int64
	if err := a.repo.DB().WithContext(ctx).Table("skill_refund_cases AS rc").
		Joins("JOIN skill_usage_records AS su ON su.usage_id = rc.usage_id").
		Joins("JOIN skill_packages AS sp ON sp.skill_id = su.skill_id").
		Where("sp.creator_user_id = ?", auth.UserID).
		Count(&refundCount).Error; err != nil {
		return CreatorSkillUsageAnalyticsOutput{}, err
	}
	var reasons []struct {
		ReasonCode string `gorm:"column:reason_code"`
		Count      int    `gorm:"column:count"`
	}
	if err := a.repo.DB().WithContext(ctx).Table("skill_refund_cases AS rc").
		Select("rc.reason_code, COUNT(*) AS count").
		Joins("JOIN skill_usage_records AS su ON su.usage_id = rc.usage_id").
		Joins("JOIN skill_packages AS sp ON sp.skill_id = su.skill_id").
		Where("sp.creator_user_id = ?", auth.UserID).
		Group("rc.reason_code").
		Scan(&reasons).Error; err != nil {
		return CreatorSkillUsageAnalyticsOutput{}, err
	}
	summary := map[string]int{}
	for _, item := range reasons {
		summary[item.ReasonCode] = item.Count
	}
	return CreatorSkillUsageAnalyticsOutput{
		UsageCount:         usageCount,
		RevenueHoldAmount:  revenueHold.Total,
		RefundCount:        refundCount,
		FailureCodeSummary: summary,
	}, nil
}

func (a *App) ListAdminSkillReviews(ctx context.Context, in ListAdminSkillReviewsInput) (AdminPage[AdminSkillReviewDTO], error) {
	if err := requireAdminID(in.AdminID); err != nil {
		return AdminPage[AdminSkillReviewDTO]{}, err
	}
	limit := normalizeLimit(in.Limit)
	offset := nonNegativeOffset(in.Offset)
	var total int64
	if err := a.adminSkillReviewBaseQuery(ctx, in.Status, in.Keyword).Count(&total).Error; err != nil {
		return AdminPage[AdminSkillReviewDTO]{}, err
	}
	var rows []adminSkillReviewRow
	if err := a.adminSkillReviewQuery(ctx, in.Status, in.Keyword).
		Order("sr.updated_at DESC, sr.review_id ASC").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error; err != nil {
		return AdminPage[AdminSkillReviewDTO]{}, err
	}
	items := make([]AdminSkillReviewDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, adminSkillReviewDTO(row))
	}
	return AdminPage[AdminSkillReviewDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) ApproveSkillReview(ctx context.Context, in ApproveSkillReviewInput) (ApproveSkillReviewOutput, error) {
	adminID := strings.TrimSpace(in.AdminID)
	if err := requireAdminID(adminID); err != nil {
		return ApproveSkillReviewOutput{}, err
	}
	reviewID := strings.TrimSpace(in.ReviewID)
	if reviewID == "" {
		return ApproveSkillReviewOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "review_id is required")
	}
	now := a.now().UTC()
	var listingID string
	if err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var review businesscore.MarketplaceSkillReviewRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("review_id = ?", reviewID).
			First(&review).Error; err != nil {
			return err
		}
		if !isAllowed(review.Status, []string{"submitted", "reviewing", "approved"}) {
			return bizerrors.New(bizerrors.CodeStateConflict, "skill review cannot be approved from current status")
		}
		var skillVersion businesscore.MarketplaceSkillVersionRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("skill_version_id = ?", review.SkillVersionID).
			First(&skillVersion).Error; err != nil {
			return err
		}
		if !isAllowed(skillVersion.Status, []string{"submitted", "reviewing", "published"}) {
			return bizerrors.New(bizerrors.CodeStateConflict, "skill version cannot be published from current status")
		}
		var pkg businesscore.MarketplaceSkillPackageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("skill_id = ?", review.SkillID).
			First(&pkg).Error; err != nil {
			return err
		}
		var policy businesscore.SkillPricingPolicyRecord
		if err := tx.Where("skill_id = ? AND skill_version = ? AND pricing_policy_digest = ?", review.SkillID, skillVersion.Version, skillVersion.PricingPolicyDigest).
			First(&policy).Error; err != nil {
			return err
		}

		reason := strings.TrimSpace(in.Reason)
		currentVersion := skillVersion.Version
		publishedAt := now
		if skillVersion.PublishedAt != nil {
			publishedAt = skillVersion.PublishedAt.UTC()
		}
		listedAt := now
		listingID = prefixedStableID("listing_", review.SkillID, review.SkillVersionID)
		listing := skillmarket.MarketplaceListing{
			SchemaVersion:       skillmarket.SchemaVersionMarketplaceListing,
			ListingID:           listingID,
			SkillID:             review.SkillID,
			SkillVersionID:      review.SkillVersionID,
			Status:              "listed",
			PricingPolicyDigest: skillVersion.PricingPolicyDigest,
			PublishedBy:         adminID,
			ListedAt:            &listedAt,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		if err := skillmarket.ValidateCreatorPublishFlow(
			skillmarket.SkillPackage{
				SchemaVersion:  skillmarket.SchemaVersionSkillPackage,
				SkillID:        pkg.SkillID,
				CreatorUserID:  pkg.CreatorUserID,
				Name:           pkg.Name,
				Description:    pkg.Description,
				Visibility:     skillmarket.SkillVisibilityPublic,
				CurrentVersion: &currentVersion,
				CreatedAt:      pkg.CreatedAt.UTC(),
				UpdatedAt:      now,
			},
			skillmarket.SkillVersion{
				SchemaVersion:       skillmarket.SchemaVersionSkillVersion,
				SkillVersionID:      skillVersion.SkillVersionID,
				SkillID:             skillVersion.SkillID,
				Version:             skillVersion.Version,
				Status:              "published",
				RuntimeSpecDigest:   skillVersion.RuntimeSpecDigest,
				PricingPolicyDigest: skillVersion.PricingPolicyDigest,
				SubmittedAt:         utcTimePointer(skillVersion.SubmittedAt),
				PublishedAt:         &publishedAt,
				CreatedAt:           skillVersion.CreatedAt.UTC(),
				UpdatedAt:           now,
			},
			skillmarket.SkillPricingPolicy{
				SchemaVersion:       skillmarket.SchemaVersionSkillPricingPolicy,
				PricingPolicyID:     policy.PricingPolicyID,
				SkillID:             policy.SkillID,
				SkillVersion:        policy.SkillVersion,
				PricingModel:        policy.PricingModel,
				UsageCredits:        policy.UsageCredits,
				ValueDeliveredStage: policy.ValueDeliveredStage,
				PricingPolicyDigest: policy.PricingPolicyDigest,
				CreatedAt:           policy.CreatedAt.UTC(),
			},
			listing,
		); err != nil {
			return err
		}

		if err := tx.Model(&businesscore.MarketplaceSkillReviewRecord{}).
			Where("review_id = ?", reviewID).
			Updates(map[string]any{
				"status":          "approved",
				"reviewer_id":     adminID,
				"decision_reason": nullableString(reason),
				"updated_at":      now,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.MarketplaceSkillVersionRecord{}).
			Where("skill_version_id = ?", review.SkillVersionID).
			Updates(map[string]any{"status": "published", "published_at": publishedAt, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.MarketplaceSkillPackageRecord{}).
			Where("skill_id = ?", review.SkillID).
			Updates(map[string]any{"visibility": skillmarket.SkillVisibilityPublic, "current_version": currentVersion, "updated_at": now}).Error; err != nil {
			return err
		}
		record := marketplaceListingRecordFromContract(listing)
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "listing_id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"status":                "listed",
				"pricing_policy_digest": listing.PricingPolicyDigest,
				"published_by":          adminID,
				"listed_at":             listedAt,
				"updated_at":            now,
			}),
		}).Create(record).Error
	}); err != nil {
		return ApproveSkillReviewOutput{}, mapStoreError(err)
	}
	review, err := a.getAdminSkillReview(ctx, reviewID)
	if err != nil {
		return ApproveSkillReviewOutput{}, err
	}
	listing, err := a.getListingRow(ctx, listingID, false)
	if err != nil {
		return ApproveSkillReviewOutput{}, err
	}
	return ApproveSkillReviewOutput{SkillVersion: review, Listing: listingDTO(listing)}, nil
}

func (a *App) ListAdminMarketplaceListings(ctx context.Context, in ListAdminMarketplaceListingsInput) (AdminPage[MarketplaceListingDTO], error) {
	if err := requireAdminID(in.AdminID); err != nil {
		return AdminPage[MarketplaceListingDTO]{}, err
	}
	limit := normalizeLimit(in.Limit)
	offset := nonNegativeOffset(in.Offset)
	query := a.listingQuery(ctx)
	if status := strings.TrimSpace(in.Status); status != "" {
		query = query.Where("ml.status = ?", status)
	}
	if keyword := strings.TrimSpace(in.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("sp.name ILIKE ? OR sp.description ILIKE ? OR ml.listing_id ILIKE ?", like, like, like)
	}
	var rows []listingRow
	if err := query.Order("ml.updated_at DESC, ml.listing_id ASC").Limit(limit).Offset(offset).Scan(&rows).Error; err != nil {
		return AdminPage[MarketplaceListingDTO]{}, err
	}
	items := make([]MarketplaceListingDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, listingDTO(row))
	}
	return AdminPage[MarketplaceListingDTO]{Items: items, Limit: limit, Offset: offset, Total: int64(offset + len(items))}, nil
}

func (a *App) SuspendMarketplaceListing(ctx context.Context, in SuspendMarketplaceListingInput) (SuspendMarketplaceListingOutput, error) {
	if err := requireAdminID(in.AdminID); err != nil {
		return SuspendMarketplaceListingOutput{}, err
	}
	if strings.TrimSpace(in.ReasonCode) == "" {
		return SuspendMarketplaceListingOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "reason_code is required")
	}
	suspended, err := a.repo.SuspendMarketplaceListingV1(ctx, in.ListingID, a.now().UTC())
	if err != nil {
		return SuspendMarketplaceListingOutput{}, mapStoreError(err)
	}
	row, err := a.getListingRow(ctx, suspended.ListingID, false)
	if err != nil {
		return SuspendMarketplaceListingOutput{}, err
	}
	return SuspendMarketplaceListingOutput{Listing: listingDTO(row)}, nil
}

func (a *App) ListAdminRefundCases(ctx context.Context, in ListAdminRefundCasesInput) (AdminPage[AdminRefundCaseDTO], error) {
	if err := requireAdminID(in.AdminID); err != nil {
		return AdminPage[AdminRefundCaseDTO]{}, err
	}
	limit := normalizeLimit(in.Limit)
	offset := nonNegativeOffset(in.Offset)
	query := a.adminRefundCaseQuery(ctx)
	if status := strings.TrimSpace(in.Status); status != "" {
		query = query.Where("rc.status = ?", status)
	}
	var rows []adminRefundCaseRow
	if err := query.Order("rc.updated_at DESC, rc.refund_case_id ASC").Limit(limit).Offset(offset).Scan(&rows).Error; err != nil {
		return AdminPage[AdminRefundCaseDTO]{}, err
	}
	items := make([]AdminRefundCaseDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, adminRefundCaseDTO(row))
	}
	return AdminPage[AdminRefundCaseDTO]{Items: items, Limit: limit, Offset: offset, Total: int64(offset + len(items))}, nil
}

func (a *App) ApproveSkillUsageRefund(ctx context.Context, in ApproveSkillUsageRefundInput) (ApproveSkillUsageRefundOutput, error) {
	if err := requireAdminID(in.AdminID); err != nil {
		return ApproveSkillUsageRefundOutput{}, err
	}
	refundCaseID := strings.TrimSpace(in.RefundCaseID)
	if refundCaseID == "" {
		return ApproveSkillUsageRefundOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "refund_case_id is required")
	}
	now := a.now().UTC()
	var usage skillmarket.SkillUsageRecord
	var settlement skillmarket.SkillSettlement
	if err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var refundCase businesscore.SkillRefundCaseRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("refund_case_id = ?", refundCaseID).
			First(&refundCase).Error; err != nil {
			return err
		}
		if !isAllowed(refundCase.Status, []string{"refund_requested", "refund_reviewing", "refund_reversed"}) {
			return bizerrors.New(bizerrors.CodeStateConflict, "refund case cannot be approved from current status")
		}
		currentUsage, err := a.getUsageContractTx(tx, refundCase.UsageID)
		if err != nil {
			return err
		}
		currentSettlement, err := a.getSettlementContractTx(tx, refundCase.SettlementID, refundCase.UsageID)
		if err != nil {
			return err
		}
		if currentUsage.UsageStatus == "refunded" && currentUsage.RefundStatus == "refund_reversed" {
			usage = currentUsage
			settlement = currentSettlement
			settlementID := currentSettlement.SettlementID
			return tx.Model(&businesscore.SkillRefundCaseRecord{}).
				Where("refund_case_id = ?", refundCaseID).
				Updates(map[string]any{"status": "refund_reversed", "settlement_id": settlementID, "updated_at": now}).Error
		}
		afterRefund := currentUsage
		afterRefund.UsageStatus = "refunded"
		afterRefund.ChargeStatus = "released"
		afterRefund.RefundStatus = "refund_reversed"
		afterRefund.SettlementStatus = "reversed"
		afterRefund.UpdatedAt = now
		afterSettlement := currentSettlement
		afterSettlement.Status = "reversed"
		afterSettlement.UpdatedAt = now
		if err := skillmarket.ValidateSkillUsageRefundReversal(currentUsage, afterRefund, afterSettlement); err != nil {
			return err
		}
		if err := tx.Model(&businesscore.SkillUsageRecord{}).
			Where("usage_id = ?", afterRefund.UsageID).
			Updates(map[string]any{
				"usage_status":      afterRefund.UsageStatus,
				"charge_status":     afterRefund.ChargeStatus,
				"refund_status":     afterRefund.RefundStatus,
				"settlement_status": afterRefund.SettlementStatus,
				"updated_at":        afterRefund.UpdatedAt,
			}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.SkillSettlementRecord{}).
			Where("settlement_id = ?", afterSettlement.SettlementID).
			Updates(map[string]any{"status": afterSettlement.Status, "updated_at": afterSettlement.UpdatedAt}).Error; err != nil {
			return err
		}
		usage = afterRefund
		settlement = afterSettlement
		settlementID := afterSettlement.SettlementID
		return tx.Model(&businesscore.SkillRefundCaseRecord{}).
			Where("refund_case_id = ?", refundCaseID).
			Updates(map[string]any{"status": "refund_reversed", "settlement_id": settlementID, "updated_at": now}).Error
	}); err != nil {
		return ApproveSkillUsageRefundOutput{}, mapStoreError(err)
	}
	return ApproveSkillUsageRefundOutput{Usage: usageDTO(usage), Settlement: settlementDTO(settlement)}, nil
}

func (a *App) ListAdminSettlements(ctx context.Context, in ListAdminSettlementsInput) (AdminPage[AdminSettlementDTO], error) {
	if err := requireAdminID(in.AdminID); err != nil {
		return AdminPage[AdminSettlementDTO]{}, err
	}
	limit := normalizeLimit(in.Limit)
	offset := nonNegativeOffset(in.Offset)
	query := a.adminSettlementQuery(ctx)
	if status := strings.TrimSpace(in.Status); status != "" {
		query = query.Where("ss.status = ?", status)
	}
	var rows []adminSettlementRow
	if err := query.Order("ss.updated_at DESC, ss.settlement_id ASC").Limit(limit).Offset(offset).Scan(&rows).Error; err != nil {
		return AdminPage[AdminSettlementDTO]{}, err
	}
	items := make([]AdminSettlementDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, adminSettlementDTO(row))
	}
	return AdminPage[AdminSettlementDTO]{Items: items, Limit: limit, Offset: offset, Total: int64(offset + len(items))}, nil
}

func (a *App) ReleaseSkillSettlementHold(ctx context.Context, in ReleaseSkillSettlementHoldInput) (SkillSettlementGovernanceOutput, error) {
	if err := requireAdminID(in.AdminID); err != nil {
		return SkillSettlementGovernanceOutput{}, err
	}
	settlementID := strings.TrimSpace(in.SettlementID)
	if settlementID == "" {
		return SkillSettlementGovernanceOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "settlement_id is required")
	}
	idempotencyKey := strings.TrimSpace(in.Meta.IdempotencyKey)
	if idempotencyKey == "" {
		return SkillSettlementGovernanceOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "idempotency_key is required")
	}
	reasonCode := strings.TrimSpace(in.ReasonCode)
	if reasonCode == "" {
		return SkillSettlementGovernanceOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "reason_code is required")
	}

	now := a.now().UTC()
	var settlement skillmarket.SkillSettlement
	var payout businesscore.SkillSettlementPayoutRecord
	err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, found, err := findSettlementPayoutByIdempotencyTx(tx, idempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existing.SettlementID != settlementID || existing.Action != "release_hold" {
				return bizerrors.New(bizerrors.CodeIdempotencyConflict, "settlement payout idempotency key conflicts")
			}
			current, err := a.getSettlementContractTx(tx, &settlementID, "")
			if err != nil {
				return err
			}
			settlement = current
			payout = existing
			return nil
		}

		current, err := a.getSettlementContractTx(tx, &settlementID, "")
		if err != nil {
			return err
		}
		if current.Status != "pending_hold" {
			return bizerrors.New(bizerrors.CodeStateConflict, "settlement hold can only be released from pending_hold")
		}
		if now.Before(current.HoldUntil) {
			return bizerrors.New(bizerrors.CodeStateConflict, "settlement hold period is not over")
		}
		usage, err := a.getUsageContractTx(tx, current.UsageID)
		if err != nil {
			return err
		}
		after := current
		after.Status = "eligible"
		after.UpdatedAt = now
		if err := skillmarket.ValidateSkillSettlement(after); err != nil {
			return err
		}
		afterUsage := usage
		afterUsage.SettlementStatus = after.Status
		afterUsage.UpdatedAt = now
		if err := skillmarket.ValidateSkillUsageRecord(afterUsage); err != nil {
			return err
		}
		if err := updateSettlementAndUsageStatusTx(tx, after, afterUsage); err != nil {
			return err
		}
		payout = buildSettlementPayoutRecord(after, "release_hold", current.Status, after.Status, "", reasonCode, in.AdminID, idempotencyKey, now)
		if err := tx.Create(&payout).Error; err != nil {
			return err
		}
		settlement = after
		return nil
	})
	if err != nil {
		return SkillSettlementGovernanceOutput{}, mapStoreError(err)
	}
	return SkillSettlementGovernanceOutput{Settlement: settlementDTO(settlement), Payout: settlementPayoutDTO(payout)}, nil
}

func (a *App) ConfirmSkillSettlementPayout(ctx context.Context, in ConfirmSkillSettlementPayoutInput) (SkillSettlementGovernanceOutput, error) {
	if err := requireAdminID(in.AdminID); err != nil {
		return SkillSettlementGovernanceOutput{}, err
	}
	settlementID := strings.TrimSpace(in.SettlementID)
	if settlementID == "" {
		return SkillSettlementGovernanceOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "settlement_id is required")
	}
	idempotencyKey := strings.TrimSpace(in.Meta.IdempotencyKey)
	if idempotencyKey == "" {
		return SkillSettlementGovernanceOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "idempotency_key is required")
	}
	payoutReference := strings.TrimSpace(in.PayoutReference)
	if payoutReference == "" {
		return SkillSettlementGovernanceOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "payout_reference is required")
	}
	reasonCode := strings.TrimSpace(in.ReasonCode)
	if reasonCode == "" {
		return SkillSettlementGovernanceOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "reason_code is required")
	}

	now := a.now().UTC()
	var settlement skillmarket.SkillSettlement
	var payout businesscore.SkillSettlementPayoutRecord
	err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, found, err := findSettlementPayoutByIdempotencyTx(tx, idempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existing.SettlementID != settlementID || existing.Action != "confirm_payout" {
				return bizerrors.New(bizerrors.CodeIdempotencyConflict, "settlement payout idempotency key conflicts")
			}
			current, err := a.getSettlementContractTx(tx, &settlementID, "")
			if err != nil {
				return err
			}
			settlement = current
			payout = existing
			return nil
		}

		current, err := a.getSettlementContractTx(tx, &settlementID, "")
		if err != nil {
			return err
		}
		if current.Status != "eligible" {
			return bizerrors.New(bizerrors.CodeStateConflict, "settlement payout can only be confirmed from eligible")
		}
		usage, err := a.getUsageContractTx(tx, current.UsageID)
		if err != nil {
			return err
		}
		after := current
		after.Status = "settled"
		after.UpdatedAt = now
		if err := skillmarket.ValidateSkillSettlement(after); err != nil {
			return err
		}
		afterUsage := usage
		afterUsage.SettlementStatus = after.Status
		afterUsage.UpdatedAt = now
		if err := skillmarket.ValidateSkillUsageRecord(afterUsage); err != nil {
			return err
		}
		if err := updateSettlementAndUsageStatusTx(tx, after, afterUsage); err != nil {
			return err
		}
		payout = buildSettlementPayoutRecord(after, "confirm_payout", current.Status, after.Status, payoutReference, reasonCode, in.AdminID, idempotencyKey, now)
		if err := tx.Create(&payout).Error; err != nil {
			return err
		}
		settlement = after
		return nil
	})
	if err != nil {
		return SkillSettlementGovernanceOutput{}, mapStoreError(err)
	}
	return SkillSettlementGovernanceOutput{Settlement: settlementDTO(settlement), Payout: settlementPayoutDTO(payout)}, nil
}

func (a *App) ListMarketplaceSkills(ctx context.Context, in ListMarketplaceSkillsInput) (ListMarketplaceSkillsOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return ListMarketplaceSkillsOutput{}, err
	}
	limit := normalizeLimit(in.Limit)
	offset := cursorOffset(in.Cursor)
	query := a.listingQuery(ctx).Where("ml.status = ?", "listed")
	if keyword := strings.TrimSpace(in.Query); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("sp.name ILIKE ? OR sp.description ILIKE ?", like, like)
	}
	var rows []listingRow
	if err := query.Order("ml.updated_at DESC, ml.listing_id ASC").Limit(limit + 1).Offset(offset).Scan(&rows).Error; err != nil {
		return ListMarketplaceSkillsOutput{}, err
	}
	nextCursor := ""
	if len(rows) > limit {
		rows = rows[:limit]
		nextCursor = strconv.Itoa(offset + limit)
	}
	items := make([]MarketplaceListingDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, listingDTO(row))
	}
	return ListMarketplaceSkillsOutput{Items: items, NextCursor: nextCursor}, nil
}

func (a *App) GetMarketplaceSkill(ctx context.Context, auth AuthContext, listingID string) (GetMarketplaceSkillOutput, error) {
	if err := requireAuth(auth); err != nil {
		return GetMarketplaceSkillOutput{}, err
	}
	row, err := a.getListingRow(ctx, listingID, true)
	if err != nil {
		return GetMarketplaceSkillOutput{}, err
	}
	return GetMarketplaceSkillOutput{Listing: listingDTO(row)}, nil
}

func (a *App) InstallSkill(ctx context.Context, in InstallSkillInput) (InstallSkillOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return InstallSkillOutput{}, err
	}
	scope := strings.TrimSpace(in.TargetScope)
	if scope == "" {
		scope = skillmarket.AccountScopePersonal
	}
	accountID, err := accountIDForScope(in.Auth, scope, in.EnterpriseID)
	if err != nil {
		return InstallSkillOutput{}, err
	}
	if strings.TrimSpace(in.Meta.IdempotencyKey) == "" {
		return InstallSkillOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "idempotency_key is required")
	}
	row, err := a.getListingRow(ctx, in.ListingID, true)
	if err != nil {
		return InstallSkillOutput{}, err
	}
	existing, found, err := a.findInstallation(ctx, accountID, scope, row.SkillID)
	if err != nil {
		return InstallSkillOutput{}, err
	}
	if found {
		return InstallSkillOutput{Installation: installationDTO(existing), IdempotentReplay: true}, nil
	}

	now := a.now().UTC()
	versionStrategy := skillmarket.VersionStrategyLatestPublished
	if scope == skillmarket.AccountScopeEnterprise {
		versionStrategy = skillmarket.VersionStrategyPinned
	}
	installation := skillmarket.SkillInstallation{
		SchemaVersion:    skillmarket.SchemaVersionSkillInstallation,
		InstallationID:   prefixedStableID("sinst_", accountID, scope, row.ListingID, row.SkillID),
		AccountID:        accountID,
		AccountScope:     scope,
		ListingID:        row.ListingID,
		SkillID:          row.SkillID,
		InstalledVersion: row.SkillVersion,
		VersionStrategy:  versionStrategy,
		Status:           "installed",
		UpgradeStatus:    "none",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	request := skillmarket.InstallSkillRequest{AccountID: accountID, AccountScope: scope, ListingID: row.ListingID, IdempotencyKey: in.Meta.IdempotencyKey}
	var saved skillmarket.SkillInstallation
	if scope == skillmarket.AccountScopePersonal {
		saved, err = a.repo.InstallPersonalLatestSkillV1(ctx, request, installation)
	} else {
		if err = a.repo.EnsureMarketplaceListingInstallableV1(ctx, row.ListingID); err == nil {
			saved, err = a.repo.SaveSkillInstallationSnapshotV1(ctx, installation, in.Meta.IdempotencyKey)
		}
	}
	if err != nil {
		return InstallSkillOutput{}, mapStoreError(err)
	}
	return InstallSkillOutput{Installation: installationDTOFromContract(saved)}, nil
}

func (a *App) ListInstalledSkills(ctx context.Context, in ListInstalledSkillsInput) (ListInstalledSkillsOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return ListInstalledSkillsOutput{}, err
	}
	scope := strings.TrimSpace(in.AccountScope)
	if scope == "" {
		scope = currentAccountScope(in.Auth)
	}
	accountID, err := accountIDForScope(in.Auth, scope, "")
	if err != nil {
		return ListInstalledSkillsOutput{}, err
	}
	limit := normalizeLimit(in.Limit)
	offset := in.Offset
	if offset < 0 {
		offset = 0
	}
	var rows []installationRow
	err = a.installationQuery(ctx).
		Where("si.account_id = ? AND si.account_scope = ?", accountID, scope).
		Order("si.updated_at DESC, si.installation_id ASC").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return ListInstalledSkillsOutput{}, err
	}
	items := make([]SkillInstallationDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, installationDTO(row))
	}
	return ListInstalledSkillsOutput{Items: items}, nil
}

func (a *App) UpgradeSkillInstallation(ctx context.Context, in UpgradeSkillInstallationInput) (UpgradeSkillInstallationOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return UpgradeSkillInstallationOutput{}, err
	}
	if strings.TrimSpace(in.Meta.IdempotencyKey) == "" {
		return UpgradeSkillInstallationOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "idempotency_key is required")
	}
	if !in.Confirmed {
		return UpgradeSkillInstallationOutput{}, bizerrors.New(bizerrors.CodeStateConflict, "enterprise upgrade requires confirmation")
	}
	initial, err := a.getInstallationContract(ctx, in.InstallationID)
	if err != nil {
		return UpgradeSkillInstallationOutput{}, err
	}
	accountID, err := accountIDForScope(in.Auth, initial.AccountScope, "")
	if err != nil {
		return UpgradeSkillInstallationOutput{}, err
	}
	if initial.AccountID != accountID {
		return UpgradeSkillInstallationOutput{}, bizerrors.New(bizerrors.CodePermissionDenied, "installation is not visible to current account")
	}
	now := a.now().UTC()
	after := initial
	after.InstalledVersion = strings.TrimSpace(in.TargetVersion)
	after.UpgradeStatus = "confirmed"
	after.UpdatedAt = now
	request := skillmarket.UpgradeSkillInstallationRequest{
		InstallationID: initial.InstallationID,
		TargetVersion:  after.InstalledVersion,
		Confirmed:      in.Confirmed,
		IdempotencyKey: in.Meta.IdempotencyKey,
	}
	rule := skillmarket.HistoricalRunRule{
		RunID:                      prefixedStableID("run_upgrade_", initial.InstallationID, initial.InstalledVersion),
		MustResumeWithSkillVersion: initial.InstalledVersion,
	}
	upgraded, err := a.repo.UpgradeSkillInstallationV1(ctx, request, after, rule)
	if err != nil {
		return UpgradeSkillInstallationOutput{}, mapStoreError(err)
	}
	return UpgradeSkillInstallationOutput{Installation: installationDTOFromContract(upgraded)}, nil
}

func (a *App) EstimateSkillUsageCredits(ctx context.Context, in EstimateSkillUsageCreditsInput) (EstimateSkillUsageCreditsOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return EstimateSkillUsageCreditsOutput{}, err
	}
	row, err := a.getListingRow(ctx, in.ListingID, true)
	if err != nil {
		return EstimateSkillUsageCreditsOutput{}, err
	}
	if strings.TrimSpace(in.RunID) == "" {
		return EstimateSkillUsageCreditsOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "run_id is required")
	}
	if in.PricingPolicyDigest != "" && in.PricingPolicyDigest != row.PricingPolicyDigest {
		return EstimateSkillUsageCreditsOutput{}, bizerrors.New(bizerrors.CodeStateConflict, "pricing_policy_digest does not match listing")
	}
	digest, err := skillUsageDigest(in.RunID, row)
	if err != nil {
		return EstimateSkillUsageCreditsOutput{}, err
	}
	return EstimateSkillUsageCreditsOutput{
		EstimatedCredits:    row.UsageCredits,
		PricingPolicyDigest: row.PricingPolicyDigest,
		SkillUsageDigest:    digest,
		ExpiresAt:           a.now().UTC().Add(15 * time.Minute),
	}, nil
}

func (a *App) CreateSkillUsageRecord(ctx context.Context, in CreateSkillUsageRecordInput) (CreateSkillUsageRecordOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return CreateSkillUsageRecordOutput{}, err
	}
	row, err := a.getListingRow(ctx, in.ListingID, true)
	if err != nil {
		return CreateSkillUsageRecordOutput{}, err
	}
	if strings.TrimSpace(in.RunID) == "" {
		return CreateSkillUsageRecordOutput{}, bizerrors.New(bizerrors.CodeInvalidArgument, "run_id is required")
	}
	if err := validateUsageEstimateInput(in, row); err != nil {
		return CreateSkillUsageRecordOutput{}, err
	}
	expectedDigest, err := skillUsageDigest(in.RunID, row)
	if err != nil {
		return CreateSkillUsageRecordOutput{}, err
	}
	digest := strings.TrimSpace(in.SkillUsageDigest)
	if digest == "" {
		digest = expectedDigest
	} else if digest != expectedDigest {
		return CreateSkillUsageRecordOutput{}, bizerrors.New(bizerrors.CodeStateConflict, "skill_usage_digest does not match listing estimate")
	}
	credits := in.EstimatedCredits
	if credits == 0 {
		credits = row.UsageCredits
	}
	now := a.now().UTC()
	usage := skillmarket.SkillUsageRecord{
		SchemaVersion:       skillmarket.SchemaVersionSkillUsageRecord,
		UsageID:             prefixedStableID("susage_", in.RunID, row.ListingID, row.SkillVersion, row.PricingPolicyDigest),
		RunID:               in.RunID,
		ListingID:           row.ListingID,
		SkillID:             row.SkillID,
		SkillVersion:        row.SkillVersion,
		PricingPolicyDigest: row.PricingPolicyDigest,
		SkillUsageDigest:    digest,
		UsageStatus:         "confirmation_required",
		ChargeStatus:        "not_frozen",
		RefundStatus:        "none",
		SettlementStatus:    "pending_hold",
		EstimatedCredits:    credits,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	idempotencyKey := strings.TrimSpace(in.Meta.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = strings.Join([]string{in.RunID, row.ListingID, row.SkillVersion, row.PricingPolicyDigest}, ":")
	}
	created, err := a.repo.CreateSkillUsageRecordV1(ctx, usage, idempotencyKey)
	if err != nil {
		return CreateSkillUsageRecordOutput{}, mapStoreError(err)
	}
	return CreateSkillUsageRecordOutput{Usage: usageDTO(created)}, nil
}

func (a *App) FreezeSkillUsageCredits(ctx context.Context, in FreezeSkillUsageCreditsInput) (FreezeSkillUsageCreditsOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return FreezeSkillUsageCreditsOutput{}, err
	}
	creditHoldID := strings.TrimSpace(in.CreditHoldID)
	if creditHoldID == "" {
		creditHoldID = prefixedStableID("chold_", in.UsageID, in.SkillUsageDigest)
	}
	frozen, err := a.repo.FreezeSkillUsageRecordV1(ctx, in.UsageID, in.SkillUsageDigest, creditHoldID, a.now().UTC())
	if err != nil {
		return FreezeSkillUsageCreditsOutput{}, mapStoreError(err)
	}
	return FreezeSkillUsageCreditsOutput{Usage: usageDTO(frozen)}, nil
}

func (a *App) ReleaseSkillUsageFreeze(ctx context.Context, in ReleaseSkillUsageFreezeInput) (ReleaseSkillUsageFreezeOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return ReleaseSkillUsageFreezeOutput{}, err
	}
	released, err := a.repo.ReleaseSkillUsageFreezeV1(ctx, in.UsageID, in.ReleaseReason, a.now().UTC())
	if err != nil {
		return ReleaseSkillUsageFreezeOutput{}, mapStoreError(err)
	}
	return ReleaseSkillUsageFreezeOutput{Usage: usageDTO(released)}, nil
}

func (a *App) CommitSkillUsageAndSettle(ctx context.Context, in CommitSkillUsageAndSettleInput) (CommitSkillUsageAndSettleOutput, error) {
	if err := requireAuth(in.Auth); err != nil {
		return CommitSkillUsageAndSettleOutput{}, err
	}
	current, err := a.getUsageContract(ctx, in.UsageID)
	if err != nil {
		return CommitSkillUsageAndSettleOutput{}, err
	}
	creatorUserID, err := a.getSkillCreatorUserID(ctx, current.SkillID)
	if err != nil {
		return CommitSkillUsageAndSettleOutput{}, err
	}
	now := a.now().UTC()
	creditHoldID := strings.TrimSpace(in.CreditHoldID)
	if current.CreditHoldID != nil && *current.CreditHoldID != "" {
		creditHoldID = *current.CreditHoldID
	}
	if creditHoldID == "" {
		creditHoldID = prefixedStableID("chold_", current.UsageID, current.SkillUsageDigest)
	}
	afterCharge := current
	afterCharge.UsageStatus = "value_delivered"
	afterCharge.ChargeStatus = "charged"
	afterCharge.RefundStatus = "none"
	afterCharge.SettlementStatus = "pending_hold"
	afterCharge.CreditHoldID = &creditHoldID
	afterCharge.ValueDeliveredAt = &now
	afterCharge.UpdatedAt = now
	platformFee := current.EstimatedCredits / 5
	settlement := skillmarket.SkillSettlement{
		SchemaVersion:      skillmarket.SchemaVersionSkillSettlement,
		SettlementID:       prefixedStableID("settle_", current.UsageID),
		UsageID:            current.UsageID,
		CreatorUserID:      creatorUserID,
		Status:             "pending_hold",
		GrossCredits:       current.EstimatedCredits,
		PlatformFeeCredits: platformFee,
		CreatorCredits:     current.EstimatedCredits - platformFee,
		HoldUntil:          now.Add(7 * 24 * time.Hour),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	committed, settled, err := a.repo.CommitSkillUsageAndSettleV1(ctx, afterCharge, settlement)
	if err != nil {
		return CommitSkillUsageAndSettleOutput{}, mapStoreError(err)
	}
	return CommitSkillUsageAndSettleOutput{Usage: usageDTO(committed), Settlement: settlementDTO(settled)}, nil
}

func (a *App) listingQuery(ctx context.Context) *gorm.DB {
	return a.repo.DB().WithContext(ctx).Table("marketplace_listings AS ml").
		Select(`ml.listing_id,
			ml.skill_id,
			ml.skill_version_id,
			sv.version AS skill_version,
			sp.name AS skill_name,
			sp.description AS skill_description,
			sp.creator_user_id,
			ml.status,
			pp.pricing_model,
			pp.usage_credits,
			pp.value_delivered_stage,
			ml.pricing_policy_digest,
			ml.published_by,
			ml.listed_at,
			ml.created_at,
			ml.updated_at`).
		Joins("JOIN skill_packages AS sp ON sp.skill_id = ml.skill_id").
		Joins("JOIN skill_versions AS sv ON sv.skill_version_id = ml.skill_version_id").
		Joins("JOIN skill_pricing_policies AS pp ON pp.skill_id = ml.skill_id AND pp.skill_version = sv.version AND pp.pricing_policy_digest = ml.pricing_policy_digest")
}

func (a *App) installationQuery(ctx context.Context) *gorm.DB {
	return a.repo.DB().WithContext(ctx).Table("skill_installations AS si").
		Select(`si.installation_id,
			si.account_id,
			si.account_scope,
			si.listing_id,
			si.skill_id,
			sp.name AS skill_name,
			si.installed_version,
			si.version_strategy,
			si.status,
			si.upgrade_status,
			si.created_at,
			si.updated_at`).
		Joins("LEFT JOIN skill_packages AS sp ON sp.skill_id = si.skill_id")
}

func (a *App) creatorSkillQuery(ctx context.Context, creatorUserID string) *gorm.DB {
	return a.repo.DB().WithContext(ctx).Table("skill_packages AS sp").
		Select(`sp.skill_id,
			sp.name,
			sp.description,
			sp.visibility,
			sv.version,
			sv.skill_version_id,
			sv.status AS version_status,
			sv.runtime_spec_digest,
			sv.pricing_policy_digest,
			COALESCE(pp.pricing_model, '') AS pricing_model,
			COALESCE(pp.usage_credits, 0) AS usage_credits,
			COALESCE(pp.value_delivered_stage, '') AS value_delivered_stage,
			COALESCE(sr.review_id, '') AS review_id,
			COALESCE(sr.status, 'not_submitted') AS review_status,
			COALESCE(ml.listing_id, '') AS listing_id,
			COALESCE(ml.status, 'not_listed') AS listing_status,
			sv.submitted_at,
			sv.published_at,
			sp.created_at,
			sp.updated_at`).
		Joins("JOIN skill_versions AS sv ON sv.skill_id = sp.skill_id").
		Joins("LEFT JOIN skill_pricing_policies AS pp ON pp.skill_id = sp.skill_id AND pp.skill_version = sv.version AND pp.pricing_policy_digest = sv.pricing_policy_digest").
		Joins("LEFT JOIN skill_review_records AS sr ON sr.skill_version_id = sv.skill_version_id").
		Joins("LEFT JOIN marketplace_listings AS ml ON ml.skill_version_id = sv.skill_version_id").
		Where("sp.creator_user_id = ?", creatorUserID)
}

func (a *App) adminSkillReviewBaseQuery(ctx context.Context, status string, keyword string) *gorm.DB {
	query := a.repo.DB().WithContext(ctx).Table("skill_review_records AS sr").
		Joins("JOIN skill_packages AS sp ON sp.skill_id = sr.skill_id").
		Joins("JOIN skill_versions AS sv ON sv.skill_version_id = sr.skill_version_id")
	if normalized := strings.TrimSpace(status); normalized != "" {
		query = query.Where("sr.status = ?", normalized)
	}
	if normalized := strings.TrimSpace(keyword); normalized != "" {
		like := "%" + normalized + "%"
		query = query.Where("sp.name ILIKE ? OR sp.description ILIKE ? OR sr.review_id ILIKE ? OR sr.skill_id ILIKE ?", like, like, like, like)
	}
	return query
}

func (a *App) adminSkillReviewQuery(ctx context.Context, status string, keyword string) *gorm.DB {
	return a.adminSkillReviewBaseQuery(ctx, status, keyword).
		Select(`sr.review_id,
			sr.skill_id,
			sr.skill_version_id,
			sv.version AS skill_version,
			sp.name AS skill_name,
			sp.description AS skill_description,
			sp.creator_user_id,
			sr.status,
			sv.status AS version_status,
			sv.runtime_spec_digest,
			sv.pricing_policy_digest,
			COALESCE(pp.pricing_model, '') AS pricing_model,
			COALESCE(pp.usage_credits, 0) AS usage_credits,
			COALESCE(pp.value_delivered_stage, '') AS value_delivered_stage,
			COALESCE(ml.listing_id, '') AS listing_id,
			COALESCE(ml.status, 'not_listed') AS listing_status,
			COALESCE(sr.reviewer_id, '') AS reviewer_id,
			COALESCE(sr.decision_reason, '') AS decision_reason,
			sv.submitted_at,
			sv.published_at,
			sr.created_at,
			sr.updated_at`).
		Joins("LEFT JOIN skill_pricing_policies AS pp ON pp.skill_id = sr.skill_id AND pp.skill_version = sv.version AND pp.pricing_policy_digest = sv.pricing_policy_digest").
		Joins("LEFT JOIN marketplace_listings AS ml ON ml.skill_version_id = sr.skill_version_id")
}

func (a *App) adminRefundCaseQuery(ctx context.Context) *gorm.DB {
	return a.repo.DB().WithContext(ctx).Table("skill_refund_cases AS rc").
		Select(`rc.refund_case_id,
			rc.usage_id,
			COALESCE(rc.settlement_id, ss.settlement_id, '') AS settlement_id,
			rc.status,
			rc.reason_code,
			rc.created_by,
			su.skill_id,
			sp.name AS skill_name,
			su.listing_id,
			sp.creator_user_id,
			su.usage_status,
			su.charge_status,
			su.refund_status,
			su.settlement_status,
			su.estimated_credits,
			COALESCE(ss.creator_credits, 0) AS creator_credits,
			rc.created_at,
			rc.updated_at`).
		Joins("JOIN skill_usage_records AS su ON su.usage_id = rc.usage_id").
		Joins("JOIN skill_packages AS sp ON sp.skill_id = su.skill_id").
		Joins("LEFT JOIN skill_settlement_records AS ss ON ss.usage_id = su.usage_id")
}

func (a *App) adminSettlementQuery(ctx context.Context) *gorm.DB {
	return a.repo.DB().WithContext(ctx).Table("skill_settlement_records AS ss").
		Select(`ss.settlement_id,
			ss.usage_id,
			su.skill_id,
			sp.name AS skill_name,
			ss.creator_user_id,
			ss.status,
			ss.gross_credits,
			ss.platform_fee_credits,
			ss.creator_credits,
			ss.hold_until,
			ss.created_at,
			ss.updated_at`).
		Joins("JOIN skill_usage_records AS su ON su.usage_id = ss.usage_id").
		Joins("JOIN skill_packages AS sp ON sp.skill_id = su.skill_id")
}

func (a *App) getAdminSkillReview(ctx context.Context, reviewID string) (AdminSkillReviewDTO, error) {
	var row adminSkillReviewRow
	err := a.adminSkillReviewQuery(ctx, "", "").
		Where("sr.review_id = ?", reviewID).
		First(&row).Error
	if err != nil {
		return AdminSkillReviewDTO{}, mapStoreError(err)
	}
	return adminSkillReviewDTO(row), nil
}

func (a *App) getCreatorSkill(ctx context.Context, auth AuthContext, skillID string) (CreatorSkillDTO, error) {
	var row creatorSkillRow
	err := a.creatorSkillQuery(ctx, auth.UserID).
		Where("sp.skill_id = ?", skillID).
		Order("sv.updated_at DESC").
		First(&row).Error
	if err != nil {
		return CreatorSkillDTO{}, mapStoreError(err)
	}
	return creatorSkillDTO(row), nil
}

func (a *App) getListingRow(ctx context.Context, listingID string, requireListed bool) (listingRow, error) {
	if strings.TrimSpace(listingID) == "" {
		return listingRow{}, bizerrors.New(bizerrors.CodeInvalidArgument, "listing_id is required")
	}
	query := a.listingQuery(ctx).Where("ml.listing_id = ?", listingID)
	if requireListed {
		query = query.Where("ml.status = ?", "listed")
	}
	var row listingRow
	if err := query.First(&row).Error; err != nil {
		return listingRow{}, mapStoreError(err)
	}
	return row, nil
}

func (a *App) findInstallation(ctx context.Context, accountID string, scope string, skillID string) (installationRow, bool, error) {
	var row installationRow
	err := a.installationQuery(ctx).
		Where("si.account_id = ? AND si.account_scope = ? AND si.skill_id = ?", accountID, scope, skillID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return installationRow{}, false, nil
	}
	if err != nil {
		return installationRow{}, false, err
	}
	return row, true, nil
}

func (a *App) getInstallationContract(ctx context.Context, installationID string) (skillmarket.SkillInstallation, error) {
	if strings.TrimSpace(installationID) == "" {
		return skillmarket.SkillInstallation{}, bizerrors.New(bizerrors.CodeInvalidArgument, "installation_id is required")
	}
	var record businesscore.SkillInstallationRecord
	if err := a.repo.DB().WithContext(ctx).Where("installation_id = ?", installationID).First(&record).Error; err != nil {
		return skillmarket.SkillInstallation{}, mapStoreError(err)
	}
	installation := skillmarket.SkillInstallation{
		SchemaVersion:    skillmarket.SchemaVersionSkillInstallation,
		InstallationID:   record.InstallationID,
		AccountID:        record.AccountID,
		AccountScope:     record.AccountScope,
		ListingID:        record.ListingID,
		SkillID:          record.SkillID,
		InstalledVersion: record.InstalledVersion,
		VersionStrategy:  record.VersionStrategy,
		Status:           record.Status,
		UpgradeStatus:    record.UpgradeStatus,
		CreatedAt:        record.CreatedAt.UTC(),
		UpdatedAt:        record.UpdatedAt.UTC(),
	}
	if err := skillmarket.ValidateSkillInstallation(installation); err != nil {
		return skillmarket.SkillInstallation{}, err
	}
	return installation, nil
}

func (a *App) getUsageContract(ctx context.Context, usageID string) (skillmarket.SkillUsageRecord, error) {
	if strings.TrimSpace(usageID) == "" {
		return skillmarket.SkillUsageRecord{}, bizerrors.New(bizerrors.CodeInvalidArgument, "usage_id is required")
	}
	var record businesscore.SkillUsageRecord
	if err := a.repo.DB().WithContext(ctx).Where("usage_id = ?", usageID).First(&record).Error; err != nil {
		return skillmarket.SkillUsageRecord{}, mapStoreError(err)
	}
	usage := skillmarket.SkillUsageRecord{
		SchemaVersion:       skillmarket.SchemaVersionSkillUsageRecord,
		UsageID:             record.UsageID,
		RunID:               record.RunID,
		ListingID:           record.ListingID,
		SkillID:             record.SkillID,
		SkillVersion:        record.SkillVersion,
		PricingPolicyDigest: record.PricingPolicyDigest,
		SkillUsageDigest:    record.SkillUsageDigest,
		UsageStatus:         record.UsageStatus,
		ChargeStatus:        record.ChargeStatus,
		RefundStatus:        record.RefundStatus,
		SettlementStatus:    record.SettlementStatus,
		EstimatedCredits:    record.EstimatedCredits,
		CreditHoldID:        record.CreditHoldID,
		ValueDeliveredAt:    utcTimePointer(record.ValueDeliveredAt),
		CreatedAt:           record.CreatedAt.UTC(),
		UpdatedAt:           record.UpdatedAt.UTC(),
	}
	if err := skillmarket.ValidateSkillUsageRecord(usage); err != nil {
		return skillmarket.SkillUsageRecord{}, err
	}
	return usage, nil
}

func (a *App) getUsageContractTx(tx *gorm.DB, usageID string) (skillmarket.SkillUsageRecord, error) {
	if strings.TrimSpace(usageID) == "" {
		return skillmarket.SkillUsageRecord{}, bizerrors.New(bizerrors.CodeInvalidArgument, "usage_id is required")
	}
	var record businesscore.SkillUsageRecord
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("usage_id = ?", usageID).First(&record).Error; err != nil {
		return skillmarket.SkillUsageRecord{}, err
	}
	usage := skillmarket.SkillUsageRecord{
		SchemaVersion:       skillmarket.SchemaVersionSkillUsageRecord,
		UsageID:             record.UsageID,
		RunID:               record.RunID,
		ListingID:           record.ListingID,
		SkillID:             record.SkillID,
		SkillVersion:        record.SkillVersion,
		PricingPolicyDigest: record.PricingPolicyDigest,
		SkillUsageDigest:    record.SkillUsageDigest,
		UsageStatus:         record.UsageStatus,
		ChargeStatus:        record.ChargeStatus,
		RefundStatus:        record.RefundStatus,
		SettlementStatus:    record.SettlementStatus,
		EstimatedCredits:    record.EstimatedCredits,
		CreditHoldID:        record.CreditHoldID,
		ValueDeliveredAt:    utcTimePointer(record.ValueDeliveredAt),
		CreatedAt:           record.CreatedAt.UTC(),
		UpdatedAt:           record.UpdatedAt.UTC(),
	}
	if err := skillmarket.ValidateSkillUsageRecord(usage); err != nil {
		return skillmarket.SkillUsageRecord{}, err
	}
	return usage, nil
}

func (a *App) getSettlementContractTx(tx *gorm.DB, settlementID *string, usageID string) (skillmarket.SkillSettlement, error) {
	var record businesscore.SkillSettlementRecord
	query := tx.Clauses(clause.Locking{Strength: "UPDATE"})
	if settlementID != nil && strings.TrimSpace(*settlementID) != "" {
		query = query.Where("settlement_id = ?", strings.TrimSpace(*settlementID))
	} else {
		query = query.Where("usage_id = ?", usageID)
	}
	if err := query.First(&record).Error; err != nil {
		return skillmarket.SkillSettlement{}, err
	}
	settlement := skillmarket.SkillSettlement{
		SchemaVersion:      skillmarket.SchemaVersionSkillSettlement,
		SettlementID:       record.SettlementID,
		UsageID:            record.UsageID,
		CreatorUserID:      record.CreatorUserID,
		Status:             record.Status,
		GrossCredits:       record.GrossCredits,
		PlatformFeeCredits: record.PlatformFeeCredits,
		CreatorCredits:     record.CreatorCredits,
		HoldUntil:          record.HoldUntil.UTC(),
		CreatedAt:          record.CreatedAt.UTC(),
		UpdatedAt:          record.UpdatedAt.UTC(),
	}
	if err := skillmarket.ValidateSkillSettlement(settlement); err != nil {
		return skillmarket.SkillSettlement{}, err
	}
	return settlement, nil
}

func findSettlementPayoutByIdempotencyTx(tx *gorm.DB, idempotencyKey string) (businesscore.SkillSettlementPayoutRecord, bool, error) {
	var record businesscore.SkillSettlementPayoutRecord
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("idempotency_key = ?", idempotencyKey).
		First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return businesscore.SkillSettlementPayoutRecord{}, false, nil
	}
	if err != nil {
		return businesscore.SkillSettlementPayoutRecord{}, false, err
	}
	return record, true, nil
}

func updateSettlementAndUsageStatusTx(tx *gorm.DB, settlement skillmarket.SkillSettlement, usage skillmarket.SkillUsageRecord) error {
	if err := tx.Model(&businesscore.SkillSettlementRecord{}).
		Where("settlement_id = ?", settlement.SettlementID).
		Updates(map[string]any{"status": settlement.Status, "updated_at": settlement.UpdatedAt}).Error; err != nil {
		return err
	}
	return tx.Model(&businesscore.SkillUsageRecord{}).
		Where("usage_id = ?", usage.UsageID).
		Updates(map[string]any{"settlement_status": usage.SettlementStatus, "updated_at": usage.UpdatedAt}).Error
}

func buildSettlementPayoutRecord(settlement skillmarket.SkillSettlement, action string, before string, after string, payoutReference string, reasonCode string, adminID string, idempotencyKey string, now time.Time) businesscore.SkillSettlementPayoutRecord {
	return businesscore.SkillSettlementPayoutRecord{
		PayoutID:        prefixedStableID("spayout_", settlement.SettlementID, action, idempotencyKey),
		SettlementID:    settlement.SettlementID,
		CreatorUserID:   settlement.CreatorUserID,
		Action:          action,
		StatusBefore:    before,
		StatusAfter:     after,
		PayoutReference: payoutReference,
		ReasonCode:      reasonCode,
		OperatorAdminID: adminID,
		IdempotencyKey:  idempotencyKey,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func (a *App) getSkillCreatorUserID(ctx context.Context, skillID string) (string, error) {
	var record businesscore.MarketplaceSkillPackageRecord
	if err := a.repo.DB().WithContext(ctx).Where("skill_id = ?", skillID).First(&record).Error; err != nil {
		return "", mapStoreError(err)
	}
	return record.CreatorUserID, nil
}

func validateUsageEstimateInput(in CreateSkillUsageRecordInput, row listingRow) error {
	if in.SkillID != "" && in.SkillID != row.SkillID {
		return bizerrors.New(bizerrors.CodeStateConflict, "skill_id does not match listing")
	}
	if in.SkillVersion != "" && in.SkillVersion != row.SkillVersion {
		return bizerrors.New(bizerrors.CodeStateConflict, "skill_version does not match listing")
	}
	if in.PricingPolicyDigest != "" && in.PricingPolicyDigest != row.PricingPolicyDigest {
		return bizerrors.New(bizerrors.CodeStateConflict, "pricing_policy_digest does not match listing")
	}
	if in.EstimatedCredits != 0 && in.EstimatedCredits != row.UsageCredits {
		return bizerrors.New(bizerrors.CodeStateConflict, "estimated_credits does not match listing")
	}
	return nil
}

func requireAuth(auth AuthContext) error {
	if strings.TrimSpace(auth.UserID) == "" {
		return bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	return nil
}

func requireAdminID(adminID string) error {
	if strings.TrimSpace(adminID) == "" {
		return bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth context is required")
	}
	return nil
}

func accountIDForScope(auth AuthContext, scope string, enterpriseID string) (string, error) {
	switch scope {
	case skillmarket.AccountScopePersonal:
		if auth.SpaceID != "" {
			return auth.SpaceID, nil
		}
		if auth.UserID != "" {
			return auth.UserID, nil
		}
	case skillmarket.AccountScopeEnterprise:
		id := strings.TrimSpace(enterpriseID)
		if id == "" {
			id = auth.EnterpriseID
		}
		if id != "" {
			return id, nil
		}
		return "", bizerrors.New(bizerrors.CodePermissionDenied, "enterprise context is required")
	}
	return "", bizerrors.New(bizerrors.CodeInvalidArgument, "target_scope must be personal or enterprise")
}

func currentAccountScope(auth AuthContext) string {
	if auth.LoginIdentityType == accountspace.IdentityEnterprise || auth.EnterpriseID != "" {
		return skillmarket.AccountScopeEnterprise
	}
	return skillmarket.AccountScopePersonal
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return bizerrors.New(bizerrors.CodeResourceNotFound, "marketplace resource not found")
	}
	if errors.Is(err, businesscore.ErrMarketplaceListingSuspended) {
		return bizerrors.New(bizerrors.CodeStateConflict, "marketplace listing is suspended")
	}
	if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "SQLSTATE 23505") {
		return bizerrors.New(bizerrors.CodeStateConflict, "marketplace resource already exists")
	}
	return err
}

func skillUsageDigest(runID string, row listingRow) (string, error) {
	return foundation.CanonicalDigest(map[string]any{
		"schema_version":         "skill_usage_preflight.v1",
		"run_id":                 runID,
		"listing_id":             row.ListingID,
		"skill_id":               row.SkillID,
		"skill_version":          row.SkillVersion,
		"pricing_policy_digest":  row.PricingPolicyDigest,
		"estimated_credits":      row.UsageCredits,
		"value_delivered_stage":  row.ValueDeliveredStage,
		"pricing_model":          row.PricingModel,
		"two_stage_confirmation": true,
	})
}

func skillPackageRecordFromContract(pkg skillmarket.SkillPackage) *businesscore.MarketplaceSkillPackageRecord {
	return &businesscore.MarketplaceSkillPackageRecord{
		SkillID:        pkg.SkillID,
		CreatorUserID:  pkg.CreatorUserID,
		Name:           pkg.Name,
		Description:    pkg.Description,
		Visibility:     pkg.Visibility,
		CurrentVersion: pkg.CurrentVersion,
		CreatedAt:      pkg.CreatedAt.UTC(),
		UpdatedAt:      pkg.UpdatedAt.UTC(),
	}
}

func skillVersionRecordFromContract(version skillmarket.SkillVersion) *businesscore.MarketplaceSkillVersionRecord {
	return &businesscore.MarketplaceSkillVersionRecord{
		SkillVersionID:      version.SkillVersionID,
		SkillID:             version.SkillID,
		Version:             version.Version,
		Status:              version.Status,
		RuntimeSpecDigest:   version.RuntimeSpecDigest,
		PricingPolicyDigest: version.PricingPolicyDigest,
		SubmittedAt:         utcTimePointer(version.SubmittedAt),
		PublishedAt:         utcTimePointer(version.PublishedAt),
		CreatedAt:           version.CreatedAt.UTC(),
		UpdatedAt:           version.UpdatedAt.UTC(),
	}
}

func skillPricingPolicyRecordFromContract(policy skillmarket.SkillPricingPolicy) *businesscore.SkillPricingPolicyRecord {
	return &businesscore.SkillPricingPolicyRecord{
		PricingPolicyID:     policy.PricingPolicyID,
		SkillID:             policy.SkillID,
		SkillVersion:        policy.SkillVersion,
		PricingModel:        policy.PricingModel,
		UsageCredits:        policy.UsageCredits,
		ValueDeliveredStage: policy.ValueDeliveredStage,
		PricingPolicyDigest: policy.PricingPolicyDigest,
		CreatedAt:           policy.CreatedAt.UTC(),
	}
}

func marketplaceListingRecordFromContract(listing skillmarket.MarketplaceListing) *businesscore.MarketplaceListingRecord {
	return &businesscore.MarketplaceListingRecord{
		ListingID:           listing.ListingID,
		SkillID:             listing.SkillID,
		SkillVersionID:      listing.SkillVersionID,
		Status:              listing.Status,
		PricingPolicyDigest: listing.PricingPolicyDigest,
		PublishedBy:         listing.PublishedBy,
		ListedAt:            utcTimePointer(listing.ListedAt),
		CreatedAt:           listing.CreatedAt.UTC(),
		UpdatedAt:           listing.UpdatedAt.UTC(),
	}
}

func prefixedStableID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + hex.EncodeToString(sum[:])[:24]
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func nonNegativeOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}

func cursorOffset(cursor string) int {
	offset, err := strconv.Atoi(strings.TrimSpace(cursor))
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func nullableString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func isAllowed(value string, allowed []string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func listingDTO(row listingRow) MarketplaceListingDTO {
	return MarketplaceListingDTO{
		ListingID:           row.ListingID,
		SkillID:             row.SkillID,
		SkillVersionID:      row.SkillVersionID,
		SkillVersion:        row.SkillVersion,
		SkillName:           row.SkillName,
		SkillDescription:    row.SkillDescription,
		CreatorUserID:       row.CreatorUserID,
		Status:              row.Status,
		PricingModel:        row.PricingModel,
		UsageCredits:        row.UsageCredits,
		ValueDeliveredStage: row.ValueDeliveredStage,
		PricingPolicyDigest: row.PricingPolicyDigest,
		PublishedBy:         row.PublishedBy,
		ListedAt:            utcTimePointer(row.ListedAt),
		CreatedAt:           row.CreatedAt.UTC(),
		UpdatedAt:           row.UpdatedAt.UTC(),
	}
}

func creatorSkillDTO(row creatorSkillRow) CreatorSkillDTO {
	return CreatorSkillDTO{
		SkillID:             row.SkillID,
		Name:                row.Name,
		Description:         row.Description,
		Visibility:          row.Visibility,
		Version:             row.Version,
		SkillVersionID:      row.SkillVersionID,
		VersionStatus:       row.VersionStatus,
		RuntimeSpecDigest:   row.RuntimeSpecDigest,
		PricingPolicyDigest: row.PricingPolicyDigest,
		PricingModel:        row.PricingModel,
		UsageCredits:        row.UsageCredits,
		ValueDeliveredStage: row.ValueDeliveredStage,
		ReviewID:            row.ReviewID,
		ReviewStatus:        row.ReviewStatus,
		ListingID:           row.ListingID,
		ListingStatus:       row.ListingStatus,
		SubmittedAt:         utcTimePointer(row.SubmittedAt),
		PublishedAt:         utcTimePointer(row.PublishedAt),
		CreatedAt:           row.CreatedAt.UTC(),
		UpdatedAt:           row.UpdatedAt.UTC(),
	}
}

func adminSkillReviewDTO(row adminSkillReviewRow) AdminSkillReviewDTO {
	return AdminSkillReviewDTO{
		ReviewID:            row.ReviewID,
		SkillID:             row.SkillID,
		SkillVersionID:      row.SkillVersionID,
		SkillVersion:        row.SkillVersion,
		SkillName:           row.SkillName,
		SkillDescription:    row.SkillDescription,
		CreatorUserID:       row.CreatorUserID,
		Status:              row.Status,
		VersionStatus:       row.VersionStatus,
		RuntimeSpecDigest:   row.RuntimeSpecDigest,
		PricingPolicyDigest: row.PricingPolicyDigest,
		PricingModel:        row.PricingModel,
		UsageCredits:        row.UsageCredits,
		ValueDeliveredStage: row.ValueDeliveredStage,
		ListingID:           row.ListingID,
		ListingStatus:       row.ListingStatus,
		ReviewerID:          row.ReviewerID,
		DecisionReason:      row.DecisionReason,
		SubmittedAt:         utcTimePointer(row.SubmittedAt),
		PublishedAt:         utcTimePointer(row.PublishedAt),
		CreatedAt:           row.CreatedAt.UTC(),
		UpdatedAt:           row.UpdatedAt.UTC(),
	}
}

func adminRefundCaseDTO(row adminRefundCaseRow) AdminRefundCaseDTO {
	return AdminRefundCaseDTO{
		RefundCaseID:     row.RefundCaseID,
		UsageID:          row.UsageID,
		SettlementID:     row.SettlementID,
		Status:           row.Status,
		ReasonCode:       row.ReasonCode,
		CreatedBy:        row.CreatedBy,
		SkillID:          row.SkillID,
		SkillName:        row.SkillName,
		ListingID:        row.ListingID,
		CreatorUserID:    row.CreatorUserID,
		UsageStatus:      row.UsageStatus,
		ChargeStatus:     row.ChargeStatus,
		RefundStatus:     row.RefundStatus,
		SettlementStatus: row.SettlementStatus,
		EstimatedCredits: row.EstimatedCredits,
		CreatorCredits:   row.CreatorCredits,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}
}

func adminSettlementDTO(row adminSettlementRow) AdminSettlementDTO {
	return AdminSettlementDTO{
		SettlementID:       row.SettlementID,
		UsageID:            row.UsageID,
		SkillID:            row.SkillID,
		SkillName:          row.SkillName,
		CreatorUserID:      row.CreatorUserID,
		Status:             row.Status,
		GrossCredits:       row.GrossCredits,
		PlatformFeeCredits: row.PlatformFeeCredits,
		CreatorCredits:     row.CreatorCredits,
		HoldUntil:          row.HoldUntil.UTC(),
		CreatedAt:          row.CreatedAt.UTC(),
		UpdatedAt:          row.UpdatedAt.UTC(),
	}
}

func installationDTO(row installationRow) SkillInstallationDTO {
	return SkillInstallationDTO{
		InstallationID:   row.InstallationID,
		AccountID:        row.AccountID,
		AccountScope:     row.AccountScope,
		ListingID:        row.ListingID,
		SkillID:          row.SkillID,
		SkillName:        row.SkillName,
		InstalledVersion: row.InstalledVersion,
		VersionStrategy:  row.VersionStrategy,
		Status:           row.Status,
		UpgradeStatus:    row.UpgradeStatus,
		CreatedAt:        row.CreatedAt.UTC(),
		UpdatedAt:        row.UpdatedAt.UTC(),
	}
}

func installationDTOFromContract(installation skillmarket.SkillInstallation) SkillInstallationDTO {
	return SkillInstallationDTO{
		InstallationID:   installation.InstallationID,
		AccountID:        installation.AccountID,
		AccountScope:     installation.AccountScope,
		ListingID:        installation.ListingID,
		SkillID:          installation.SkillID,
		InstalledVersion: installation.InstalledVersion,
		VersionStrategy:  installation.VersionStrategy,
		Status:           installation.Status,
		UpgradeStatus:    installation.UpgradeStatus,
		CreatedAt:        installation.CreatedAt.UTC(),
		UpdatedAt:        installation.UpdatedAt.UTC(),
	}
}

func usageDTO(usage skillmarket.SkillUsageRecord) SkillUsageRecordDTO {
	return SkillUsageRecordDTO{
		UsageID:             usage.UsageID,
		RunID:               usage.RunID,
		ListingID:           usage.ListingID,
		SkillID:             usage.SkillID,
		SkillVersion:        usage.SkillVersion,
		PricingPolicyDigest: usage.PricingPolicyDigest,
		SkillUsageDigest:    usage.SkillUsageDigest,
		UsageStatus:         usage.UsageStatus,
		ChargeStatus:        usage.ChargeStatus,
		RefundStatus:        usage.RefundStatus,
		SettlementStatus:    usage.SettlementStatus,
		EstimatedCredits:    usage.EstimatedCredits,
		CreditHoldID:        usage.CreditHoldID,
		ValueDeliveredAt:    utcTimePointer(usage.ValueDeliveredAt),
		CreatedAt:           usage.CreatedAt.UTC(),
		UpdatedAt:           usage.UpdatedAt.UTC(),
	}
}

func settlementDTO(settlement skillmarket.SkillSettlement) SkillSettlementDTO {
	return SkillSettlementDTO{
		SettlementID:       settlement.SettlementID,
		UsageID:            settlement.UsageID,
		CreatorUserID:      settlement.CreatorUserID,
		Status:             settlement.Status,
		GrossCredits:       settlement.GrossCredits,
		PlatformFeeCredits: settlement.PlatformFeeCredits,
		CreatorCredits:     settlement.CreatorCredits,
		HoldUntil:          settlement.HoldUntil.UTC(),
		CreatedAt:          settlement.CreatedAt.UTC(),
		UpdatedAt:          settlement.UpdatedAt.UTC(),
	}
}

func settlementPayoutDTO(record businesscore.SkillSettlementPayoutRecord) SkillSettlementPayoutDTO {
	return SkillSettlementPayoutDTO{
		PayoutID:        record.PayoutID,
		SettlementID:    record.SettlementID,
		CreatorUserID:   record.CreatorUserID,
		Action:          record.Action,
		StatusBefore:    record.StatusBefore,
		StatusAfter:     record.StatusAfter,
		PayoutReference: record.PayoutReference,
		ReasonCode:      record.ReasonCode,
		OperatorAdminID: record.OperatorAdminID,
		CreatedAt:       record.CreatedAt.UTC(),
		UpdatedAt:       record.UpdatedAt.UTC(),
	}
}
