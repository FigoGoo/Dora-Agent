package credit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	StatusActive    = "active"
	StatusFrozen    = "frozen"
	StatusReleased  = "released"
	StatusCharged   = "charged"
	StatusEstimated = "estimated"
)

type AuthContext = accountspace.AuthContext
type RequestMeta = accountspace.RequestMeta

type App struct {
	repo  *businesscore.Repository
	guard *idempotency.IdempotencyGuard
	audit auditlog.Writer
	now   func() time.Time
}

func New(repo *businesscore.Repository, guard *idempotency.IdempotencyGuard, audit auditlog.Writer) *App {
	return &App{repo: repo, guard: guard, audit: audit, now: func() time.Time { return time.Now().UTC() }}
}

type Page[T any] struct {
	Items  []T   `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

type SummaryDTO struct {
	AccountID         string     `json:"account_id"`
	AccountType       string     `json:"account_type"`
	AvailablePoints   int64      `json:"available_points"`
	FrozenPoints      int64      `json:"frozen_points"`
	ExpiresSoonPoints int64      `json:"expires_soon_points"`
	NearestExpireAt   *time.Time `json:"nearest_expire_at,omitempty"`
}

type LedgerDTO struct {
	EntryID      string    `json:"entry_id"`
	EntryType    string    `json:"entry_type"`
	Amount       int64     `json:"amount"`
	BalanceAfter int64     `json:"balance_after"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	CreatedAt    time.Time `json:"created_at"`
}

type CreditLotDTO struct {
	LotID              string     `json:"lot_id"`
	AccountID          string     `json:"account_id"`
	SourceType         string     `json:"source_type"`
	SourceID           string     `json:"source_id,omitempty"`
	OriginalPoints     int64      `json:"original_points"`
	AvailablePoints    int64      `json:"available_points"`
	FrozenPoints       int64      `json:"frozen_points"`
	ConsumedPoints     int64      `json:"consumed_points"`
	ExpiredPoints      int64      `json:"expired_points"`
	GrantedAt          time.Time  `json:"granted_at"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	ExpiryPolicyJSON   string     `json:"expiry_policy_json"`
	SpendScopeJSON     string     `json:"spend_scope_json"`
	SettlementEligible bool       `json:"settlement_eligible"`
	Status             string     `json:"status"`
}

type RechargePackageDTO struct {
	PackageID          string         `json:"package_id"`
	PackageType        string         `json:"package_type"`
	Name               string         `json:"name"`
	DisplayName        string         `json:"display_name"`
	TargetScope        string         `json:"target_scope"`
	BillingMode        string         `json:"billing_mode"`
	Points             int64          `json:"points"`
	GrantedPoints      int64          `json:"granted_points"`
	BonusPoints        int64          `json:"bonus_points"`
	PriceCents         int64          `json:"price_cents"`
	PriceAmount        int64          `json:"price_amount"`
	Currency           string         `json:"currency"`
	CreditExpiryPolicy string         `json:"credit_expiry_policy"`
	SpendScope         []string       `json:"spend_scope"`
	SettlementEligible bool           `json:"settlement_eligible"`
	EntitlementPolicy  map[string]any `json:"entitlement_policy"`
	RenewalPolicy      map[string]any `json:"renewal_policy"`
	RefundPolicy       map[string]any `json:"refund_policy"`
	VisibleScope       string         `json:"visible_scope"`
	Status             string         `json:"status"`
	UpdatedAt          time.Time      `json:"updated_at,omitempty"`
}

type BillingPackageSKUDTO struct {
	SKUID               string     `json:"sku_id"`
	PackageID           string     `json:"package_id"`
	ChannelCode         string     `json:"channel_code"`
	PriceAmount         int64      `json:"price_amount"`
	Currency            string     `json:"currency"`
	ActivityPriceAmount *int64     `json:"activity_price_amount,omitempty"`
	Status              string     `json:"status"`
	EffectiveAt         time.Time  `json:"effective_at"`
	ExpiredAt           *time.Time `json:"expired_at,omitempty"`
}

type EntitlementSnapshotDTO struct {
	EntitlementSnapshotID string         `json:"entitlement_snapshot_id"`
	AccountID             string         `json:"account_id"`
	UserID                string         `json:"user_id,omitempty"`
	EnterpriseID          string         `json:"enterprise_id,omitempty"`
	PackageID             string         `json:"package_id"`
	OrderID               string         `json:"order_id"`
	TargetScope           string         `json:"target_scope"`
	EntitlementPolicy     map[string]any `json:"entitlement_policy"`
	Status                string         `json:"status"`
	EffectiveAt           time.Time      `json:"effective_at"`
	ExpiresAt             *time.Time     `json:"expires_at,omitempty"`
}

type EnterpriseContractDTO struct {
	ContractID     string         `json:"contract_id"`
	EnterpriseID   string         `json:"enterprise_id"`
	PackageID      string         `json:"package_id"`
	OrderID        string         `json:"order_id,omitempty"`
	ContractStatus string         `json:"contract_status"`
	BillingMode    string         `json:"billing_mode"`
	PeriodStart    time.Time      `json:"period_start"`
	PeriodEnd      *time.Time     `json:"period_end,omitempty"`
	SeatQuota      int            `json:"seat_quota"`
	BudgetPoints   int64          `json:"budget_points"`
	ApprovalPolicy map[string]any `json:"approval_policy"`
	InvoicePolicy  map[string]any `json:"invoice_policy"`
}

type BillingInvoiceDTO struct {
	InvoiceID     string         `json:"invoice_id"`
	EnterpriseID  string         `json:"enterprise_id,omitempty"`
	OrderID       string         `json:"order_id,omitempty"`
	Amount        int64          `json:"amount"`
	Currency      string         `json:"currency"`
	InvoiceStatus string         `json:"invoice_status"`
	IssuedAt      *time.Time     `json:"issued_at,omitempty"`
	DueAt         *time.Time     `json:"due_at,omitempty"`
	Metadata      map[string]any `json:"metadata"`
}

type BillingPromotionDTO struct {
	PromotionID    string         `json:"promotion_id"`
	PromotionName  string         `json:"promotion_name"`
	PackageID      string         `json:"package_id,omitempty"`
	DiscountPolicy map[string]any `json:"discount_policy"`
	VisibleScope   string         `json:"visible_scope"`
	Status         string         `json:"status"`
	StartsAt       time.Time      `json:"starts_at"`
	EndsAt         *time.Time     `json:"ends_at,omitempty"`
}

type CreditAccountDTO struct {
	AccountID         string `json:"account_id"`
	AccountType       string `json:"account_type"`
	OwnerUserID       string `json:"owner_user_id,omitempty"`
	EnterpriseID      string `json:"enterprise_id,omitempty"`
	AvailablePoints   int64  `json:"available_points"`
	FrozenPoints      int64  `json:"frozen_points"`
	ExpiresSoonPoints int64  `json:"expires_soon_points"`
	Status            string `json:"status"`
}

type EstimateLineItemDTO struct {
	EstimateItemID  string            `json:"estimate_item_id"`
	ItemType        string            `json:"item_type"`
	ToolName        string            `json:"tool_name,omitempty"`
	ToolType        string            `json:"tool_type,omitempty"`
	PricingPolicyID string            `json:"pricing_policy_id,omitempty"`
	ModelID         string            `json:"model_id,omitempty"`
	ResourceType    string            `json:"resource_type,omitempty"`
	BillingUnit     string            `json:"billing_unit,omitempty"`
	Quantity        float64           `json:"quantity,omitempty"`
	UnitPoints      float64           `json:"unit_points,omitempty"`
	EstimatePoints  int64             `json:"estimate_points"`
	FreeReason      string            `json:"free_reason,omitempty"`
	Metadata        map[string]string `json:"metadata_summary,omitempty"`
}

type EstimateDTO struct {
	EstimateID         string                `json:"estimate_id"`
	EstimatePoints     int64                 `json:"estimate_points"`
	AvailablePoints    int64                 `json:"available_points"`
	ExpiresSoonPoints  int64                 `json:"expires_soon_points"`
	CreditAccountScope string                `json:"credit_account_scope"`
	CreditAccountID    string                `json:"credit_account_id"`
	LineItems          []EstimateLineItemDTO `json:"line_items"`
	ExpiresAt          time.Time             `json:"expires_at"`
	Insufficient       bool                  `json:"insufficient"`
}

type ToolUsageItem struct {
	ToolName        string
	ToolType        string
	BillingUnit     string
	Quantity        float64
	MetadataSummary map[string]string
}

type EstimateGenerationInput struct {
	Auth              AuthContext
	Meta              RequestMeta
	ProjectID         string
	ResourceType      string
	ModelID           string
	PricingSnapshotID string
	Quantity          int32
	DurationSeconds   int32
	ToolUsageItems    []ToolUsageItem
	SafetyEvidence    *businessagent.SafetyEvidenceDTO
}

type EstimateToolInput struct {
	Auth           AuthContext
	Meta           RequestMeta
	ProjectID      string
	ToolUsageItems []ToolUsageItem
	SafetyEvidence *businessagent.SafetyEvidenceDTO
}

type FreezeInput struct {
	Auth           AuthContext
	Meta           RequestMeta
	EstimateID     string
	Points         int64
	RunID          string
	ConfirmationID string
	AccountID      string
}

type FreezeDTO struct {
	FreezeID     string    `json:"freeze_id"`
	FrozenPoints int64     `json:"frozen_points"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type ReleaseInput struct {
	Auth          AuthContext
	Meta          RequestMeta
	FreezeID      string
	ReleasePoints int64
	Reason        string
	RunID         string
}

type ReleaseDTO struct {
	ReleasedPoints int64  `json:"released_points"`
	ReleaseStatus  string `json:"release_status"`
}

type ChargeItemInput struct {
	EstimateItemID  string
	ToolCallID      string
	ToolName        string
	ToolType        string
	BillingUnit     string
	ActualQuantity  float64
	ExecutionStatus string
	MetadataSummary map[string]string
}

type ChargeToolInput struct {
	Auth        AuthContext
	Meta        RequestMeta
	ProjectID   string
	EstimateID  string
	FreezeID    string
	SessionID   string
	RunID       string
	ChargeItems []ChargeItemInput
}

type ChargedLineItemDTO struct {
	EstimateItemID string `json:"estimate_item_id"`
	ChargedPoints  int64  `json:"charged_points"`
	Status         string `json:"status"`
	AssetID        string `json:"asset_id,omitempty"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	ArtifactID     string `json:"artifact_id,omitempty"`
}

type ChargeToolDTO struct {
	ToolChargeID     string               `json:"tool_charge_id"`
	ChargedPoints    int64                `json:"charged_points"`
	ReleasedPoints   int64                `json:"released_points"`
	FreezeStatus     string               `json:"freeze_status"`
	LedgerEntryIDs   []string             `json:"ledger_entry_ids"`
	ChargedLineItems []ChargedLineItemDTO `json:"charged_line_items"`
}

type RedeemInput struct {
	Auth              AuthContext
	Meta              RequestMeta
	Code              string
	TargetAccountType string
	RedeemChannel     string
}

type RedeemDTO struct {
	AccountID       string `json:"account_id"`
	RedeemedPoints  int64  `json:"redeemed_points"`
	CreditBatchID   string `json:"credit_batch_id"`
	RedemptionID    string `json:"redemption_id"`
	AvailablePoints int64  `json:"available_points"`
}

type CreateRechargeOrderInput struct {
	Auth              AuthContext
	Meta              RequestMeta
	PackageID         string
	SKUID             string
	TargetAccountType string
}

type MockPayRechargeOrderInput struct {
	Auth                  AuthContext
	Meta                  RequestMeta
	OrderID               string
	PaymentResult         string
	ProviderTransactionID string
}

type RechargeOrderDTO struct {
	OrderID               string     `json:"order_id"`
	AccountID             string     `json:"account_id"`
	EnterpriseID          string     `json:"enterprise_id,omitempty"`
	PackageID             string     `json:"package_id"`
	SKUID                 string     `json:"sku_id,omitempty"`
	PackageType           string     `json:"package_type"`
	TargetScope           string     `json:"target_scope"`
	BillingMode           string     `json:"billing_mode"`
	Points                int64      `json:"points"`
	GrantedPoints         int64      `json:"granted_points"`
	BonusPoints           int64      `json:"bonus_points"`
	PriceCents            int64      `json:"price_cents"`
	PriceAmount           int64      `json:"price_amount"`
	Currency              string     `json:"currency"`
	PaymentProvider       string     `json:"payment_provider"`
	PaymentStatus         string     `json:"payment_status"`
	CreditLotID           string     `json:"credit_lot_id,omitempty"`
	EntitlementSnapshotID string     `json:"entitlement_snapshot_id,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	PaidAt                *time.Time `json:"paid_at,omitempty"`
}

type CreditTargetDTO struct {
	TargetType  string `json:"target_type"`
	TargetID    string `json:"target_id"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
	AccountID   string `json:"account_id"`
}

type AdminGrantInput struct {
	Auth       admin.AdminAuth
	Meta       RequestMeta
	TargetType string
	TargetID   string
	Points     int64
	ExpiresAt  time.Time
	Reason     string
}

type AdminGrantDTO struct {
	BatchID         string `json:"batch_id"`
	AccountID       string `json:"account_id"`
	GrantedPoints   int64  `json:"granted_points"`
	AvailablePoints int64  `json:"available_points"`
}

type ExpireCreditLotsInput struct {
	Auth      admin.AdminAuth
	Meta      RequestMeta
	AccountID string
	LotID     string
	Limit     int
	Reason    string
}

type RefundCreditsInput struct {
	Auth                  admin.AdminAuth
	Meta                  RequestMeta
	AccountID             string
	Points                int64
	OriginalLotID         string
	OriginalLedgerEntryID string
	Reason                string
	GracePeriodDays       int
}

type ReverseCreditLedgerEntryInput struct {
	Auth          admin.AdminAuth
	Meta          RequestMeta
	LedgerEntryID string
	Reason        string
}

type CreditMaintenanceDTO struct {
	OperationID           string `json:"operation_id"`
	Status                string `json:"status"`
	AccountID             string `json:"account_id,omitempty"`
	LotID                 string `json:"lot_id,omitempty"`
	LedgerEntryID         string `json:"ledger_entry_id,omitempty"`
	ReversedLedgerEntryID string `json:"reversed_ledger_entry_id,omitempty"`
	AffectedLots          int    `json:"affected_lots,omitempty"`
	Points                int64  `json:"points"`
	AvailablePoints       int64  `json:"available_points,omitempty"`
	FrozenPoints          int64  `json:"frozen_points,omitempty"`
}

type CreateCodesInput struct {
	Auth            admin.AdminAuth
	Meta            RequestMeta
	Count           int
	Points          int64
	CodeExpiresAt   time.Time
	CreditExpiresAt time.Time
	AccountType     string
	BindTargetType  string
	BindTargetID    string
	Channel         string
	Reason          string
}

type RedeemCodeDTO struct {
	BatchID         string     `json:"batch_id"`
	BatchNo         string     `json:"batch_no"`
	AccountType     string     `json:"account_type"`
	BindTargetType  string     `json:"bind_target_type"`
	BindTargetID    string     `json:"bind_target_id,omitempty"`
	Channel         string     `json:"channel,omitempty"`
	TargetType      string     `json:"target_type"`
	TotalCodes      int        `json:"count"`
	PointsPerCode   int64      `json:"points"`
	ExpiresAt       *time.Time `json:"code_expires_at,omitempty"`
	CreditExpiresAt *time.Time `json:"credit_expires_at,omitempty"`
	Status          string     `json:"status"`
}

type CreateCodesDTO struct {
	BatchID string   `json:"batch_id"`
	BatchNo string   `json:"batch_no"`
	Codes   []string `json:"codes,omitempty"`
	Count   int      `json:"count"`
}

type SaveBillingPackageInput struct {
	Auth               admin.AdminAuth
	Meta               RequestMeta
	PackageID          string
	PackageType        string
	Name               string
	TargetScope        string
	BillingMode        string
	PriceAmount        int64
	Currency           string
	GrantedPoints      int64
	BonusPoints        int64
	CreditExpiryPolicy string
	SpendScope         []string
	SettlementEligible bool
	EntitlementPolicy  map[string]any
	RenewalPolicy      map[string]any
	RefundPolicy       map[string]any
	VisibleScope       string
	Status             string
	Reason             string
}

type BillingPackageStatusInput struct {
	Auth      admin.AdminAuth
	Meta      RequestMeta
	PackageID string
	Status    string
	Reason    string
}

type CreateBillingSKUInput struct {
	Auth                admin.AdminAuth
	Meta                RequestMeta
	PackageID           string
	SKUID               string
	ChannelCode         string
	PriceAmount         int64
	Currency            string
	ActivityPriceAmount *int64
	EffectiveAt         time.Time
	ExpiredAt           *time.Time
	Reason              string
}

func (a *App) GetSummary(ctx context.Context, auth AuthContext) (SummaryDTO, error) {
	account, err := a.resolveAccount(ctx, a.repo.DB().WithContext(ctx), auth)
	if err != nil {
		return SummaryDTO{}, err
	}
	return a.summaryDTO(ctx, account)
}

func (a *App) GetEnterpriseSummary(ctx context.Context, auth AuthContext) (SummaryDTO, error) {
	if auth.EnterpriseID == "" || auth.EnterpriseRole != accountspace.RoleOwner {
		return SummaryDTO{}, bizerrors.New(bizerrors.CodePermissionDenied, "enterprise owner permission is required")
	}
	return a.GetSummary(ctx, auth)
}

func (a *App) ListLedger(ctx context.Context, auth AuthContext, limit, offset int) (Page[LedgerDTO], error) {
	account, err := a.resolveAccount(ctx, a.repo.DB().WithContext(ctx), auth)
	if err != nil {
		return Page[LedgerDTO]{}, err
	}
	return a.listLedgerForAccount(ctx, account.ID, limit, offset)
}

func (a *App) ListCreditLots(ctx context.Context, auth AuthContext, sourceType, status string, limit, offset int) (Page[CreditLotDTO], error) {
	account, err := a.resolveAccount(ctx, a.repo.DB().WithContext(ctx), auth)
	if err != nil {
		return Page[CreditLotDTO]{}, err
	}
	return a.listCreditLots(ctx, account.ID, sourceType, status, limit, offset)
}

func (a *App) ListExpiringCredits(ctx context.Context, auth AuthContext, withinDays, limit, offset int) (Page[CreditLotDTO], error) {
	account, err := a.resolveAccount(ctx, a.repo.DB().WithContext(ctx), auth)
	if err != nil {
		return Page[CreditLotDTO]{}, err
	}
	if withinDays <= 0 {
		withinDays = 30
	}
	limit, offset = normalizePage(limit, offset, 100)
	now := a.now()
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.CreditBatch{}).
		Where("account_id = ? AND status = ? AND available_points > 0 AND expires_at > ? AND expires_at <= ?", account.ID, StatusActive, now, now.AddDate(0, 0, withinDays))
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[CreditLotDTO]{}, err
	}
	var rows []businesscore.CreditBatch
	if err := db.Order("expires_at ASC, granted_at ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[CreditLotDTO]{}, err
	}
	return Page[CreditLotDTO]{Items: creditLotDTOs(rows), Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) ListEnterpriseUsage(ctx context.Context, auth AuthContext, limit, offset int) (Page[LedgerDTO], error) {
	if auth.EnterpriseID == "" {
		return Page[LedgerDTO]{}, bizerrors.New(bizerrors.CodePermissionDenied, "enterprise identity is required")
	}
	account, err := a.resolveAccount(ctx, a.repo.DB().WithContext(ctx), auth)
	if err != nil {
		return Page[LedgerDTO]{}, err
	}
	// ACCT-3：企业拥有者看企业全部流水；普通成员只看自己在企业空间产生的消耗明细(本人 project 的流水)。
	if auth.EnterpriseRole == accountspace.RoleOwner {
		return a.listLedgerForAccount(ctx, account.ID, limit, offset)
	}
	return a.listLedgerForMember(ctx, account.ID, auth.UserID, auth.EnterpriseID, limit, offset)
}

func (a *App) EstimateGenerationCredits(ctx context.Context, in EstimateGenerationInput) (EstimateDTO, error) {
	if strings.TrimSpace(in.ProjectID) == "" || strings.TrimSpace(in.ResourceType) == "" || strings.TrimSpace(in.ModelID) == "" || strings.TrimSpace(in.PricingSnapshotID) == "" || strings.TrimSpace(in.Meta.IdempotencyKey) == "" {
		return EstimateDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "project_id, resource_type, model_id, pricing_snapshot_id and idempotency_key are required")
	}
	if err := validateSafetyEvidence(in.SafetyEvidence, "generation", "prompt", in.Meta.TraceID, a.now()); err != nil {
		return EstimateDTO{}, err
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{
		"project_id": in.ProjectID, "resource_type": in.ResourceType, "model_id": in.ModelID, "pricing_snapshot_id": in.PricingSnapshotID,
		"quantity": in.Quantity, "duration_seconds": in.DurationSeconds, "tool_usage_items": in.ToolUsageItems, "safety_evidence_digest": safetyDigest(in.SafetyEvidence),
	})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "credit.estimate_generation", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return EstimateDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return EstimateDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "generation estimate idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return EstimateDTO{}, bizerrors.New(bizerrors.CodeProcessing, "generation estimate request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.getEstimateDTO(ctx, decision.ReplayResult.ID)
	}
	account, err := a.resolveAccount(ctx, a.repo.DB().WithContext(ctx), in.Auth)
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return EstimateDTO{}, err
	}
	price, err := a.activeModelPrice(ctx, in.ModelID, in.ResourceType, in.PricingSnapshotID)
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return EstimateDTO{}, err
	}
	quantity := generationQuantity(in.ResourceType, in.Quantity, in.DurationSeconds)
	points := estimatePoints(quantity, price.UnitPoints, price.MinChargePoints)
	lineItems := []EstimateLineItemDTO{{
		EstimateItemID: security.RandomID("est_item_"), ItemType: "model_generation", ModelID: in.ModelID,
		ResourceType: in.ResourceType, BillingUnit: price.BillingUnit, Quantity: quantity, UnitPoints: price.UnitPoints,
		EstimatePoints: points, PricingPolicyID: price.PricingSnapshotID,
	}}
	for _, item := range in.ToolUsageItems {
		toolLine, err := a.estimateToolLine(ctx, item)
		if err != nil {
			_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
			return EstimateDTO{}, err
		}
		lineItems = append(lineItems, toolLine)
		points += toolLine.EstimatePoints
	}
	dto, err := a.createEstimate(ctx, in.Auth, in.Meta, account, in.ProjectID, in.ResourceType, in.ModelID, in.PricingSnapshotID, points, lineItems, in.SafetyEvidence)
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return EstimateDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "credit_estimate", ID: dto.EstimateID}); err != nil {
		return EstimateDTO{}, err
	}
	return dto, nil
}

func (a *App) EstimateToolCredits(ctx context.Context, in EstimateToolInput) (EstimateDTO, error) {
	if strings.TrimSpace(in.ProjectID) == "" || len(in.ToolUsageItems) == 0 {
		return EstimateDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "project_id and tool_usage_items are required")
	}
	if err := validateSafetyEvidence(in.SafetyEvidence, "generation", "prompt", in.Meta.TraceID, a.now()); err != nil {
		return EstimateDTO{}, err
	}
	account, err := a.resolveAccount(ctx, a.repo.DB().WithContext(ctx), in.Auth)
	if err != nil {
		return EstimateDTO{}, err
	}
	var points int64
	lineItems := make([]EstimateLineItemDTO, 0, len(in.ToolUsageItems))
	for _, item := range in.ToolUsageItems {
		line, err := a.estimateToolLine(ctx, item)
		if err != nil {
			return EstimateDTO{}, err
		}
		lineItems = append(lineItems, line)
		points += line.EstimatePoints
	}
	return a.createEstimate(ctx, in.Auth, in.Meta, account, in.ProjectID, "", "", "", points, lineItems, in.SafetyEvidence)
}

func (a *App) FreezeCredits(ctx context.Context, in FreezeInput) (FreezeDTO, error) {
	if in.EstimateID == "" || in.Points <= 0 || in.RunID == "" || in.Meta.IdempotencyKey == "" {
		return FreezeDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "estimate_id, points, run_id and idempotency_key are required")
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{"estimate_id": in.EstimateID, "points": in.Points, "run_id": in.RunID, "confirmation_id": in.ConfirmationID})
	tenant := "space:" + in.Auth.SpaceID
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: tenant, SpaceID: in.Auth.SpaceID, Scope: "credit.freeze", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return FreezeDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return FreezeDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "freeze idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return FreezeDTO{}, bizerrors.New(bizerrors.CodeProcessing, "freeze request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.getFreezeDTO(ctx, decision.ReplayResult.ID)
	}
	var dto FreezeDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		var estimate businesscore.CreditEstimate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("estimate_id = ?", in.EstimateID).First(&estimate).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "credit estimate not found")
		}
		if estimate.ExpiresAt.Before(now) || estimate.Status != StatusEstimated {
			return bizerrors.New(bizerrors.CodeStateConflict, "credit estimate is not freezeable")
		}
		if estimate.Insufficient || in.Points > estimate.AvailablePoints {
			return bizerrors.New(bizerrors.CodeStateConflict, "credit estimate is insufficient")
		}
		if in.Points != estimate.EstimatePoints {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "freeze points must match estimate points")
		}
		var account businesscore.CreditAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", estimate.AccountID).First(&account).Error; err != nil {
			return err
		}
		if in.AccountID != "" && in.AccountID != account.ID {
			return bizerrors.New(bizerrors.CodeStateConflict, "freeze account does not match estimate")
		}
		if account.AvailablePoints < in.Points {
			return bizerrors.New(bizerrors.CodeStateConflict, "credit account has insufficient points")
		}
		freezeID := security.RandomID("frz_")
		if err := a.allocateFreezeBatches(tx, account.ID, freezeID, in.Points, in.Auth.UserID, now); err != nil {
			return err
		}
		account.AvailablePoints -= in.Points
		account.FrozenPoints += in.Points
		account.UpdatedBy = optionalString(in.Auth.UserID)
		account.UpdatedAt = now
		if err := tx.Save(&account).Error; err != nil {
			return err
		}
		freeze := businesscore.CreditFreeze{
			ID: security.RandomID("cfz_"), FreezeID: freezeID, EstimateID: estimate.EstimateID, AccountID: account.ID,
			ProjectID: estimate.ProjectID, RunID: in.RunID, ConfirmationID: optionalString(in.ConfirmationID),
			FrozenPoints: in.Points, Status: StatusFrozen, ExpiresAt: now.Add(24 * time.Hour), IdempotencyKey: in.Meta.IdempotencyKey,
			TraceID: in.Meta.TraceID, CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&freeze).Error; err != nil {
			return err
		}
		if err := tx.Create(ledger(account, "freeze", 0, "credit_freeze", freezeID, estimate.ProjectID, in.RunID, in.Meta.TraceID, in.Meta.IdempotencyKey)).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.CreditEstimate{}).Where("estimate_id = ?", estimate.EstimateID).Updates(map[string]any{"status": "frozen", "updated_by": in.Auth.UserID, "updated_at": now}).Error; err != nil {
			return err
		}
		dto = FreezeDTO{FreezeID: freezeID, FrozenPoints: in.Points, ExpiresAt: freeze.ExpiresAt}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return FreezeDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "credit_freeze", ID: dto.FreezeID}); err != nil {
		return FreezeDTO{}, err
	}
	return dto, nil
}

func (a *App) ReleaseFrozenCredits(ctx context.Context, in ReleaseInput) (ReleaseDTO, error) {
	if in.FreezeID == "" || in.ReleasePoints <= 0 || in.Reason == "" || in.RunID == "" || in.Meta.IdempotencyKey == "" {
		return ReleaseDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "freeze_id, release_points, reason, run_id and idempotency_key are required")
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{"freeze_id": in.FreezeID, "release_points": in.ReleasePoints, "reason": in.Reason, "run_id": in.RunID})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "credit.release", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return ReleaseDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return ReleaseDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "release idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return ReleaseDTO{}, bizerrors.New(bizerrors.CodeProcessing, "release request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay {
		return a.getReleaseDTO(ctx, in.FreezeID, in.Meta.IdempotencyKey)
	}
	var dto ReleaseDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		freeze, released, err := a.releaseFreezeLocked(tx, in.FreezeID, in.ReleasePoints, in.Reason, in.RunID, in.Auth.UserID, in.Meta.TraceID, in.Meta.IdempotencyKey)
		if err != nil {
			return err
		}
		dto = ReleaseDTO{ReleasedPoints: released, ReleaseStatus: freeze.Status}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return ReleaseDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "credit_release", ID: in.FreezeID})
	return dto, nil
}

func (a *App) ChargeToolUsageCredits(ctx context.Context, in ChargeToolInput) (ChargeToolDTO, error) {
	if in.ProjectID == "" || in.EstimateID == "" || in.FreezeID == "" || in.SessionID == "" || in.RunID == "" || len(in.ChargeItems) == 0 || in.Meta.IdempotencyKey == "" {
		return ChargeToolDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "charge tool request is incomplete")
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{"estimate_id": in.EstimateID, "freeze_id": in.FreezeID, "items": in.ChargeItems})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "credit.tool_charge", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return ChargeToolDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return ChargeToolDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "tool charge idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return ChargeToolDTO{}, bizerrors.New(bizerrors.CodeProcessing, "tool charge request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.getToolChargeDTO(ctx, decision.ReplayResult.ID)
	}
	var dto ChargeToolDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		freeze, account, err := a.lockFreezeAndAccount(tx, in.FreezeID)
		if err != nil {
			return err
		}
		if freeze.EstimateID != in.EstimateID || freeze.ProjectID != in.ProjectID || freeze.RunID != in.RunID {
			return bizerrors.New(bizerrors.CodeStateConflict, "tool charge does not match freeze")
		}
		chargeID := security.RandomID("toolchg_")
		var charged int64
		lines := make([]ChargedLineItemDTO, 0, len(in.ChargeItems))
		for _, item := range in.ChargeItems {
			line, err := a.chargeToolItem(tx, chargeID, in.EstimateID, item, in.Auth.UserID, now)
			if err != nil {
				return err
			}
			lines = append(lines, line)
			charged += line.ChargedPoints
		}
		unsettled := freeze.FrozenPoints - freeze.ChargedPoints - freeze.ReleasedPoints - charged
		if unsettled < 0 {
			return bizerrors.New(bizerrors.CodeStateConflict, "charged points exceed frozen points")
		}
		released := int64(0)
		if charged > 0 {
			if _, err := a.consumeFreezeRows(tx, freeze.FreezeID, charged, in.Auth.UserID, now); err != nil {
				return err
			}
		}
		if unsettled > 0 {
			updated, releasedPoints, err := a.releaseFreezeRows(tx, &freeze, &account, unsettled, in.Auth.UserID, now)
			if err != nil {
				return err
			}
			freeze = updated
			released = releasedPoints
			freeze.ReleasedPoints += released
		}
		account.FrozenPoints -= charged
		freeze.ChargedPoints += charged
		if freeze.ChargedPoints+freeze.ReleasedPoints >= freeze.FrozenPoints {
			freeze.Status = StatusCharged
		}
		freeze.UpdatedBy = optionalString(in.Auth.UserID)
		freeze.UpdatedAt = now
		account.UpdatedBy = optionalString(in.Auth.UserID)
		account.UpdatedAt = now
		if err := tx.Save(&account).Error; err != nil {
			return err
		}
		if err := tx.Save(&freeze).Error; err != nil {
			return err
		}
		batch := businesscore.CreditToolChargeBatch{
			ID: security.RandomID("ctcb_"), ToolChargeID: chargeID, AccountID: account.ID, ProjectID: in.ProjectID,
			EstimateID: in.EstimateID, FreezeID: in.FreezeID, SessionID: in.SessionID, RunID: in.RunID,
			ChargedPoints: charged, ReleasedPoints: released, Status: StatusCharged, IdempotencyKey: in.Meta.IdempotencyKey,
			TraceID: in.Meta.TraceID, CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		ledgerID := security.RandomID("cled_")
		entry := ledger(account, "charge", -charged, "tool_charge", chargeID, in.ProjectID, in.RunID, in.Meta.TraceID, in.Meta.IdempotencyKey)
		entry.ID = ledgerID
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
		dto = ChargeToolDTO{ToolChargeID: chargeID, ChargedPoints: charged, ReleasedPoints: released, FreezeStatus: freeze.Status, LedgerEntryIDs: []string{ledgerID}, ChargedLineItems: lines}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return ChargeToolDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "tool_charge", ID: dto.ToolChargeID})
	return dto, nil
}

func (a *App) ListRechargePackages(ctx context.Context) (Page[RechargePackageDTO], error) {
	var rows []businesscore.RechargePackage
	if err := a.repo.DB().WithContext(ctx).Where("status = ?", StatusActive).Order("target_scope ASC, price_amount ASC").Find(&rows).Error; err != nil {
		return Page[RechargePackageDTO]{}, err
	}
	items := make([]RechargePackageDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, rechargePackageDTO(row))
	}
	return Page[RechargePackageDTO]{Items: items, Limit: len(items), Total: int64(len(items))}, nil
}

func (a *App) CreateRechargeOrder(ctx context.Context, in CreateRechargeOrderInput) (RechargeOrderDTO, error) {
	packageID := strings.TrimSpace(in.PackageID)
	if packageID == "" || strings.TrimSpace(in.Meta.IdempotencyKey) == "" {
		return RechargeOrderDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "package_id and idempotency_key are required")
	}
	var pkg businesscore.RechargePackage
	if err := a.repo.DB().WithContext(ctx).Where("package_id = ? AND status = ?", packageID, StatusActive).First(&pkg).Error; err != nil {
		return RechargeOrderDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "recharge package not found")
	}
	account, err := a.resolvePackagePurchaseAccount(ctx, in.Auth, pkg, in.TargetAccountType)
	if err != nil {
		return RechargeOrderDTO{}, err
	}
	sku, priceAmount, currency, err := a.resolvePackagePrice(ctx, pkg, strings.TrimSpace(in.SKUID))
	if err != nil {
		return RechargeOrderDTO{}, err
	}
	totalPoints := packageTotalPoints(pkg)
	hash := requestHash(in.Meta, in.Auth, map[string]any{
		"account_id": account.ID, "package_id": pkg.PackageID, "sku_id": stringPtrValue(sku), "points": totalPoints, "price_amount": priceAmount, "currency": currency,
	})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "credit.recharge_order.create", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return RechargeOrderDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return RechargeOrderDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "recharge order idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return RechargeOrderDTO{}, bizerrors.New(bizerrors.CodeProcessing, "recharge order request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.getRechargeOrderDTO(ctx, decision.ReplayResult.ID)
	}
	var dto RechargeOrderDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		orderID := security.RandomID("ord_")
		row := businesscore.RechargeOrder{
			ID: security.RandomID("ro_"), OrderID: orderID, UserID: in.Auth.UserID, AccountID: account.ID,
			EnterpriseID: account.EnterpriseID, PackageID: pkg.PackageID, SKUID: sku,
			PackageType: pkg.PackageType, TargetScope: pkg.TargetScope, BillingMode: pkg.BillingMode,
			Points: totalPoints, GrantedPoints: pkg.GrantedPoints, BonusPoints: pkg.BonusPoints,
			PriceCents: priceAmount, PriceAmount: priceAmount, Currency: currency,
			PaymentProvider: "mock_payment", PaymentStatus: "pending", OrderSource: "user_purchase",
			IdempotencyKey: in.Meta.IdempotencyKey, TraceID: optionalString(in.Meta.TraceID),
			CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		dto = rechargeOrderDTO(row)
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return RechargeOrderDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "recharge_order", ID: dto.OrderID}); err != nil {
		return RechargeOrderDTO{}, err
	}
	return dto, nil
}

func (a *App) ListRechargeOrders(ctx context.Context, auth AuthContext, status string, limit, offset int) (Page[RechargeOrderDTO], error) {
	account, err := a.resolveAccount(ctx, a.repo.DB().WithContext(ctx), auth)
	if err != nil {
		return Page[RechargeOrderDTO]{}, err
	}
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.RechargeOrder{}).Where("account_id = ?", account.ID)
	if strings.TrimSpace(status) != "" {
		db = db.Where("payment_status = ?", strings.TrimSpace(status))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[RechargeOrderDTO]{}, err
	}
	var rows []businesscore.RechargeOrder
	if err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[RechargeOrderDTO]{}, err
	}
	items := make([]RechargeOrderDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, rechargeOrderDTO(row))
	}
	return Page[RechargeOrderDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) MockPayRechargeOrder(ctx context.Context, in MockPayRechargeOrderInput) (RechargeOrderDTO, error) {
	orderID := strings.TrimSpace(in.OrderID)
	paymentResult := strings.TrimSpace(in.PaymentResult)
	if orderID == "" || paymentResult == "" || strings.TrimSpace(in.Meta.IdempotencyKey) == "" {
		return RechargeOrderDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "order_id, payment_result and idempotency_key are required")
	}
	if paymentResult != "success" && paymentResult != "failed" {
		return RechargeOrderDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "payment_result must be success or failed")
	}
	account, err := a.resolveAccount(ctx, a.repo.DB().WithContext(ctx), in.Auth)
	if err != nil {
		return RechargeOrderDTO{}, err
	}
	transactionID := strings.TrimSpace(in.ProviderTransactionID)
	if transactionID == "" {
		transactionID = security.RandomID("mock_txn_")
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{
		"account_id": account.ID, "order_id": orderID, "payment_result": paymentResult, "provider_transaction_id": transactionID,
	})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "credit.recharge_order.mock_pay", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return RechargeOrderDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return RechargeOrderDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "mock payment idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return RechargeOrderDTO{}, bizerrors.New(bizerrors.CodeProcessing, "mock payment request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.getRechargeOrderDTO(ctx, decision.ReplayResult.ID)
	}
	var dto RechargeOrderDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		var order businesscore.RechargeOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ?", orderID).First(&order).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "recharge order not found")
		}
		if order.AccountID != account.ID {
			return bizerrors.New(bizerrors.CodePermissionDenied, "recharge order account does not match")
		}
		if order.PaymentStatus == "paid" && order.CreditLotID != nil {
			dto = rechargeOrderDTO(order)
			return nil
		}
		if order.PaymentStatus != "pending" {
			return bizerrors.New(bizerrors.CodeStateConflict, "recharge order is not payable")
		}
		paymentStatus := "failed"
		if paymentResult == "success" {
			paymentStatus = "paid"
		}
		transaction := businesscore.MockPaymentTransaction{
			ID: security.RandomID("mpt_"), TransactionID: transactionID, OrderID: order.OrderID,
			PaymentResult: paymentResult, PaymentStatus: paymentStatus, IdempotencyKey: in.Meta.IdempotencyKey, TraceID: optionalString(in.Meta.TraceID),
			RequestPayloadJSON: mustJSON(map[string]any{"order_id": order.OrderID, "payment_result": paymentResult, "provider_transaction_id": transactionID}),
			CreatedBy:          optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&transaction).Error; err != nil {
			return err
		}
		if paymentResult == "failed" {
			order.PaymentStatus = "failed"
			order.FailedReason = optionalString("mock payment failed")
			order.UpdatedBy = optionalString(in.Auth.UserID)
			order.UpdatedAt = now
			if err := tx.Save(&order).Error; err != nil {
				return err
			}
			dto = rechargeOrderDTO(order)
			return nil
		}
		var pkg businesscore.RechargePackage
		if err := tx.Where("package_id = ? AND status = ?", order.PackageID, StatusActive).First(&pkg).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "recharge package not found")
		}
		expiresAt, expiryPolicyJSON, err := packageCreditExpiry(now, pkg.CreditExpiryPolicy)
		if err != nil {
			return err
		}
		var lockedAccount businesscore.CreditAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", account.ID).First(&lockedAccount).Error; err != nil {
			return err
		}
		var creditLotID string
		if order.Points > 0 {
			creditLotID = security.RandomID("cb_")
			sourceID := order.OrderID
			batch := businesscore.CreditBatch{
				ID: creditLotID, AccountID: lockedAccount.ID, BatchType: "recharge", SourceType: "recharge_package", SourceID: &sourceID,
				TotalPoints: order.Points, RemainingPoints: order.Points, OriginalPoints: order.Points, AvailablePoints: order.Points, GrantedAt: now,
				ExpiresAt: expiresAt, ExpiryPolicyJSON: expiryPolicyJSON, SpendScopeJSON: packageSpendScopeJSON(pkg),
				SettlementEligible: pkg.SettlementEligible, Status: StatusActive, CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&batch).Error; err != nil {
				return err
			}
			lockedAccount.AvailablePoints += order.Points
		}
		lockedAccount.UpdatedBy = optionalString(in.Auth.UserID)
		lockedAccount.UpdatedAt = now
		if err := tx.Save(&lockedAccount).Error; err != nil {
			return err
		}
		entitlementID := security.RandomID("ents_")
		entitlement := businesscore.PackageEntitlementSnapshot{
			ID: security.RandomID("pes_"), EntitlementSnapshotID: entitlementID, AccountID: lockedAccount.ID,
			UserID: optionalString(in.Auth.UserID), EnterpriseID: lockedAccount.EnterpriseID, PackageID: pkg.PackageID, OrderID: order.OrderID,
			TargetScope: pkg.TargetScope, EntitlementPolicy: packageEntitlementPolicyJSON(pkg), Status: StatusActive,
			EffectiveAt: now, ExpiresAt: expiresAt, CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&entitlement).Error; err != nil {
			return err
		}
		if pkg.TargetScope == "enterprise" {
			if err := a.createEnterpriseContractForPackage(tx, lockedAccount, pkg, order, now, in.Auth.UserID); err != nil {
				return err
			}
		}
		order.PaymentStatus = "paid"
		order.CreditLotID = optionalString(creditLotID)
		order.EntitlementSnapshotID = &entitlementID
		order.PaidAt = &now
		order.UpdatedBy = optionalString(in.Auth.UserID)
		order.UpdatedAt = now
		if err := tx.Save(&order).Error; err != nil {
			return err
		}
		if order.Points > 0 {
			if err := tx.Create(ledger(lockedAccount, "recharge", order.Points, "recharge_order", order.OrderID, "", "", in.Meta.TraceID, in.Meta.IdempotencyKey)).Error; err != nil {
				return err
			}
		}
		dto = rechargeOrderDTO(order)
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return RechargeOrderDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "recharge_order", ID: dto.OrderID}); err != nil {
		return RechargeOrderDTO{}, err
	}
	return dto, nil
}

func (a *App) AdminListCreditAccounts(ctx context.Context, _ admin.AdminAuth, accountType, status string, limit, offset int) (Page[CreditAccountDTO], error) {
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.CreditAccount{})
	if strings.TrimSpace(accountType) != "" {
		db = db.Where("account_type = ?", strings.TrimSpace(accountType))
	}
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[CreditAccountDTO]{}, err
	}
	var rows []businesscore.CreditAccount
	if err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[CreditAccountDTO]{}, err
	}
	items := make([]CreditAccountDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, creditAccountDTO(row))
	}
	return Page[CreditAccountDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) AdminListCreditLots(ctx context.Context, _ admin.AdminAuth, accountID, sourceType, status string, limit, offset int) (Page[CreditLotDTO], error) {
	return a.listCreditLots(ctx, accountID, sourceType, status, limit, offset)
}

func (a *App) AdminListRechargeOrders(ctx context.Context, _ admin.AdminAuth, userID, accountID, status string, limit, offset int) (Page[RechargeOrderDTO], error) {
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.RechargeOrder{})
	if strings.TrimSpace(userID) != "" {
		db = db.Where("user_id = ?", strings.TrimSpace(userID))
	}
	if strings.TrimSpace(accountID) != "" {
		db = db.Where("account_id = ?", strings.TrimSpace(accountID))
	}
	if strings.TrimSpace(status) != "" {
		db = db.Where("payment_status = ?", strings.TrimSpace(status))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[RechargeOrderDTO]{}, err
	}
	var rows []businesscore.RechargeOrder
	if err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[RechargeOrderDTO]{}, err
	}
	items := make([]RechargeOrderDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, rechargeOrderDTO(row))
	}
	return Page[RechargeOrderDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) AdminListBillingPackages(ctx context.Context, auth admin.AdminAuth, targetScope, packageType, status string, limit, offset int) (Page[RechargePackageDTO], error) {
	if auth.AdminID == "" {
		return Page[RechargePackageDTO]{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.RechargePackage{})
	if strings.TrimSpace(targetScope) != "" {
		db = db.Where("target_scope = ?", strings.TrimSpace(targetScope))
	}
	if strings.TrimSpace(packageType) != "" {
		db = db.Where("package_type = ?", strings.TrimSpace(packageType))
	}
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[RechargePackageDTO]{}, err
	}
	var rows []businesscore.RechargePackage
	if err := db.Order("updated_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[RechargePackageDTO]{}, err
	}
	items := make([]RechargePackageDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, rechargePackageDTO(row))
	}
	return Page[RechargePackageDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) AdminSaveBillingPackage(ctx context.Context, in SaveBillingPackageInput) (RechargePackageDTO, error) {
	if in.Auth.AdminID == "" {
		return RechargePackageDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	packageID := strings.TrimSpace(in.PackageID)
	if packageID == "" || strings.TrimSpace(in.Name) == "" || in.PriceAmount < 0 || in.GrantedPoints < 0 || in.BonusPoints < 0 || in.Meta.IdempotencyKey == "" {
		return RechargePackageDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "invalid billing package request")
	}
	hash := requestHash(in.Meta, AuthContext{UserID: in.Auth.AdminID}, map[string]any{
		"package_id": packageID, "package_type": in.PackageType, "target_scope": in.TargetScope, "billing_mode": in.BillingMode,
		"name": in.Name, "price_amount": in.PriceAmount, "currency": in.Currency, "granted_points": in.GrantedPoints,
		"bonus_points": in.BonusPoints, "credit_expiry_policy": in.CreditExpiryPolicy, "spend_scope": in.SpendScope,
		"entitlement_policy": in.EntitlementPolicy, "renewal_policy": in.RenewalPolicy, "refund_policy": in.RefundPolicy,
		"visible_scope": in.VisibleScope, "status": in.Status,
	})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{TenantID: "admin:" + in.Auth.AdminID, Scope: "billing.package.save", IdempotencyKey: in.Meta.IdempotencyKey, RequestHash: hash, ActorUserID: in.Auth.AdminID})
	if err != nil {
		return RechargePackageDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return RechargePackageDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "billing package idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.getBillingPackageDTO(ctx, decision.ReplayResult.ID)
	}
	now := a.now()
	row := businesscore.RechargePackage{
		ID: security.RandomID("rpkg_"), PackageID: packageID, PackageType: defaultString(in.PackageType, "personal_credit_pack"),
		TargetScope: defaultString(in.TargetScope, "personal"), BillingMode: defaultString(in.BillingMode, "one_time"),
		DisplayName: strings.TrimSpace(in.Name), Name: strings.TrimSpace(in.Name), Points: in.GrantedPoints + in.BonusPoints,
		GrantedPoints: in.GrantedPoints, BonusPoints: in.BonusPoints, PriceCents: in.PriceAmount, PriceAmount: in.PriceAmount,
		Currency: defaultString(in.Currency, "CNY"), CreditValidDuration: defaultString(in.CreditExpiryPolicy, "P1M"),
		CreditExpiryPolicy: defaultString(in.CreditExpiryPolicy, "P1M"), SpendScopeJSON: spendScopeJSON(in.SpendScope),
		SettlementEligible: in.SettlementEligible, EntitlementPolicy: mustJSON(in.EntitlementPolicy),
		RenewalPolicy: mustJSON(defaultMap(in.RenewalPolicy, map[string]any{"mode": "none"})),
		RefundPolicy:  mustJSON(defaultMap(in.RefundPolicy, map[string]any{"mode": "unused_refund"})),
		VisibleScope:  defaultString(in.VisibleScope, "all_users"), Status: defaultString(in.Status, StatusActive),
		CreatedBy: optionalString(in.Auth.AdminID), UpdatedBy: optionalString(in.Auth.AdminID), CreatedAt: now, UpdatedAt: now,
	}
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var before businesscore.RechargePackage
		_ = tx.Where("package_id = ?", packageID).First(&before).Error
		if before.ID != "" {
			row.ID = before.ID
			row.CreatedBy = before.CreatedBy
			row.CreatedAt = before.CreatedAt
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return a.writeBillingAuditTx(tx, in.Auth.AdminID, in.Meta.TraceID, "billing.package.save", "billing_package", packageID, valueStatus(before.Status), row.Status, in.Reason)
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return RechargePackageDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "billing_package", ID: row.PackageID})
	return rechargePackageDTO(row), nil
}

func (a *App) AdminSetBillingPackageStatus(ctx context.Context, in BillingPackageStatusInput) (RechargePackageDTO, error) {
	if in.Auth.AdminID == "" {
		return RechargePackageDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	status := strings.TrimSpace(in.Status)
	if status == "" || in.Meta.IdempotencyKey == "" {
		return RechargePackageDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "status and idempotency_key are required")
	}
	var row businesscore.RechargePackage
	err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("package_id = ?", strings.TrimSpace(in.PackageID)).First(&row).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "billing package not found")
		}
		beforeStatus := row.Status
		row.Status = status
		row.UpdatedBy = optionalString(in.Auth.AdminID)
		row.UpdatedAt = a.now()
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return a.writeBillingAuditTx(tx, in.Auth.AdminID, in.Meta.TraceID, "billing.package.status", "billing_package", row.PackageID, beforeStatus, row.Status, in.Reason)
	})
	if err != nil {
		return RechargePackageDTO{}, err
	}
	return rechargePackageDTO(row), nil
}

func (a *App) AdminListBillingPackageSKUs(ctx context.Context, auth admin.AdminAuth, packageID, status string, limit, offset int) (Page[BillingPackageSKUDTO], error) {
	if auth.AdminID == "" {
		return Page[BillingPackageSKUDTO]{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.BillingPackageSKU{})
	if strings.TrimSpace(packageID) != "" {
		db = db.Where("package_id = ?", strings.TrimSpace(packageID))
	}
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[BillingPackageSKUDTO]{}, err
	}
	var rows []businesscore.BillingPackageSKU
	if err := db.Order("updated_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[BillingPackageSKUDTO]{}, err
	}
	items := make([]BillingPackageSKUDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, billingPackageSKUDTO(row))
	}
	return Page[BillingPackageSKUDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) AdminCreateBillingPackageSKU(ctx context.Context, in CreateBillingSKUInput) (BillingPackageSKUDTO, error) {
	if in.Auth.AdminID == "" {
		return BillingPackageSKUDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if strings.TrimSpace(in.PackageID) == "" || in.PriceAmount < 0 || in.Meta.IdempotencyKey == "" {
		return BillingPackageSKUDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "invalid billing sku request")
	}
	now := a.now()
	effectiveAt := in.EffectiveAt
	if effectiveAt.IsZero() {
		effectiveAt = now
	}
	skuID := strings.TrimSpace(in.SKUID)
	if skuID == "" {
		skuID = security.RandomID("sku_")
	}
	row := businesscore.BillingPackageSKU{
		ID: security.RandomID("bps_"), SKUID: skuID, PackageID: strings.TrimSpace(in.PackageID), ChannelCode: defaultString(in.ChannelCode, "default"),
		PriceAmount: in.PriceAmount, Currency: defaultString(in.Currency, "CNY"), ActivityPriceAmount: in.ActivityPriceAmount,
		EffectiveAt: effectiveAt, ExpiredAt: in.ExpiredAt, Status: StatusActive,
		CreatedBy: optionalString(in.Auth.AdminID), UpdatedBy: optionalString(in.Auth.AdminID), CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.DB().WithContext(ctx).Create(&row).Error; err != nil {
		return BillingPackageSKUDTO{}, err
	}
	return billingPackageSKUDTO(row), nil
}

func (a *App) AdminListEntitlementSnapshots(ctx context.Context, auth admin.AdminAuth, accountID, enterpriseID, status string, limit, offset int) (Page[EntitlementSnapshotDTO], error) {
	if auth.AdminID == "" {
		return Page[EntitlementSnapshotDTO]{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.PackageEntitlementSnapshot{})
	if strings.TrimSpace(accountID) != "" {
		db = db.Where("account_id = ?", strings.TrimSpace(accountID))
	}
	if strings.TrimSpace(enterpriseID) != "" {
		db = db.Where("enterprise_id = ?", strings.TrimSpace(enterpriseID))
	}
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[EntitlementSnapshotDTO]{}, err
	}
	var rows []businesscore.PackageEntitlementSnapshot
	if err := db.Order("effective_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[EntitlementSnapshotDTO]{}, err
	}
	items := make([]EntitlementSnapshotDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, entitlementSnapshotDTO(row))
	}
	return Page[EntitlementSnapshotDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) AdminListEnterpriseContracts(ctx context.Context, auth admin.AdminAuth, enterpriseID, status string, limit, offset int) (Page[EnterpriseContractDTO], error) {
	if auth.AdminID == "" {
		return Page[EnterpriseContractDTO]{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.EnterpriseContract{})
	if strings.TrimSpace(enterpriseID) != "" {
		db = db.Where("enterprise_id = ?", strings.TrimSpace(enterpriseID))
	}
	if strings.TrimSpace(status) != "" {
		db = db.Where("contract_status = ?", strings.TrimSpace(status))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[EnterpriseContractDTO]{}, err
	}
	var rows []businesscore.EnterpriseContract
	if err := db.Order("period_start DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[EnterpriseContractDTO]{}, err
	}
	items := make([]EnterpriseContractDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, enterpriseContractDTO(row))
	}
	return Page[EnterpriseContractDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) AdminListBillingInvoices(ctx context.Context, auth admin.AdminAuth, enterpriseID, status string, limit, offset int) (Page[BillingInvoiceDTO], error) {
	if auth.AdminID == "" {
		return Page[BillingInvoiceDTO]{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.BillingInvoice{})
	if strings.TrimSpace(enterpriseID) != "" {
		db = db.Where("enterprise_id = ?", strings.TrimSpace(enterpriseID))
	}
	if strings.TrimSpace(status) != "" {
		db = db.Where("invoice_status = ?", strings.TrimSpace(status))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[BillingInvoiceDTO]{}, err
	}
	var rows []businesscore.BillingInvoice
	if err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[BillingInvoiceDTO]{}, err
	}
	items := make([]BillingInvoiceDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, billingInvoiceDTO(row))
	}
	return Page[BillingInvoiceDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) AdminListBillingPromotions(ctx context.Context, auth admin.AdminAuth, packageID, status string, limit, offset int) (Page[BillingPromotionDTO], error) {
	if auth.AdminID == "" {
		return Page[BillingPromotionDTO]{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.BillingPromotion{})
	if strings.TrimSpace(packageID) != "" {
		db = db.Where("package_id = ?", strings.TrimSpace(packageID))
	}
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[BillingPromotionDTO]{}, err
	}
	var rows []businesscore.BillingPromotion
	if err := db.Order("starts_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[BillingPromotionDTO]{}, err
	}
	items := make([]BillingPromotionDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, billingPromotionDTO(row))
	}
	return Page[BillingPromotionDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) RedeemCode(ctx context.Context, in RedeemInput) (RedeemDTO, error) {
	if in.Code == "" || in.Meta.IdempotencyKey == "" {
		return RedeemDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "code and idempotency_key are required")
	}
	targetAccountType := normalizeAccountType(in.TargetAccountType)
	if targetAccountType == "" {
		return RedeemDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "target_account_type is required")
	}
	account, err := a.resolveRedeemAccount(ctx, in.Auth, targetAccountType)
	if err != nil {
		return RedeemDTO{}, err
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{
		"code_digest": codeDigest(in.Code), "account_id": account.ID,
		"target_account_type": targetAccountType, "redeem_channel": strings.TrimSpace(in.RedeemChannel),
	})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "credit.redeem", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return RedeemDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return RedeemDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "redeem idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return RedeemDTO{}, bizerrors.New(bizerrors.CodeProcessing, "redeem request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.getRedemptionDTO(ctx, decision.ReplayResult.ID)
	}
	var dto RedeemDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		var code businesscore.RedeemCode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("code_digest IN ?", possibleCodeDigests(in.Code)).First(&code).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "redeem code not found")
		}
		var batch businesscore.RedeemCodeBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", code.BatchID).First(&batch).Error; err != nil {
			return err
		}
		if err := validateRedeemTarget(code, batch, account, in.Auth, targetAccountType, in.RedeemChannel, now); err != nil {
			return err
		}
		points := batch.PointsPerCode
		creditBatchID := security.RandomID("cb_")
		creditExpiry := batch.CreditExpiresAt
		if creditExpiry == nil {
			creditExpiry = batch.ExpiresAt
		}
		if creditExpiry == nil || !creditExpiry.After(now) {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "credit expiration must be in the future")
		}
		creditBatch := businesscore.CreditBatch{
			ID: creditBatchID, AccountID: account.ID, BatchType: "redeem", SourceType: "redeem_code", SourceID: &code.ID,
			TotalPoints: points, RemainingPoints: points, ExpiresAt: creditExpiry, Status: StatusActive,
			OriginalPoints: points, AvailablePoints: points, GrantedAt: now,
			ExpiryPolicyJSON: creditExpiryPolicyJSON(creditExpiry), SpendScopeJSON: defaultSpendScopeJSON(), SettlementEligible: true,
			CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&creditBatch).Error; err != nil {
			return err
		}
		account.AvailablePoints += points
		account.UpdatedBy = optionalString(in.Auth.UserID)
		account.UpdatedAt = now
		if err := tx.Save(&account).Error; err != nil {
			return err
		}
		code.Status = "redeemed"
		code.RedeemedByUserID = &in.Auth.UserID
		code.RedeemedEnterpriseID = optionalString(in.Auth.EnterpriseID)
		code.RedeemedAccountID = &account.ID
		code.RedeemedAt = &now
		code.UpdatedBy = optionalString(in.Auth.UserID)
		code.UpdatedAt = now
		if err := tx.Save(&code).Error; err != nil {
			return err
		}
		redemptionID := security.RandomID("rcr_")
		redemption := businesscore.RedeemCodeRedemption{
			ID: redemptionID, RedeemCodeID: code.ID, AccountID: account.ID, UserID: in.Auth.UserID,
			EnterpriseID: optionalString(in.Auth.EnterpriseID), Points: points, Status: "redeemed",
			IdempotencyKey: in.Meta.IdempotencyKey, TraceID: in.Meta.TraceID, CreatedAt: now,
		}
		if err := tx.Create(&redemption).Error; err != nil {
			return err
		}
		if err := tx.Create(ledger(account, "redeem", points, "redeem_code", code.ID, "", "", in.Meta.TraceID, in.Meta.IdempotencyKey)).Error; err != nil {
			return err
		}
		dto = RedeemDTO{AccountID: account.ID, RedeemedPoints: points, CreditBatchID: creditBatchID, RedemptionID: redemptionID, AvailablePoints: account.AvailablePoints}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return RedeemDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "redemption", ID: dto.RedemptionID})
	return dto, nil
}

func (a *App) SearchCreditTargets(ctx context.Context, _ admin.AdminAuth, keyword, targetType string, limit, offset int) (Page[CreditTargetDTO], error) {
	limit, offset = normalizePage(limit, offset, 100)
	targetType = strings.TrimSpace(targetType)
	var items []CreditTargetDTO
	if targetType == "" || targetType == "user" || targetType == "personal" {
		var users []businesscore.User
		db := a.repo.DB().WithContext(ctx).Where("status = ?", StatusActive).Limit(limit).Offset(offset)
		if keyword != "" {
			db = db.Where("display_name ILIKE ?", "%"+keyword+"%")
		}
		if err := db.Find(&users).Error; err != nil {
			return Page[CreditTargetDTO]{}, err
		}
		for _, user := range users {
			var account businesscore.CreditAccount
			_ = a.repo.DB().WithContext(ctx).Where("account_type = ? AND owner_user_id = ?", "personal", user.ID).First(&account).Error
			items = append(items, CreditTargetDTO{TargetType: "user", TargetID: user.ID, DisplayName: user.DisplayName, Status: user.Status, AccountID: account.ID})
		}
	}
	if targetType == "enterprise" {
		var enterprises []businesscore.Enterprise
		db := a.repo.DB().WithContext(ctx).Where("status = ?", StatusActive).Limit(limit).Offset(offset)
		if keyword != "" {
			db = db.Where("name ILIKE ?", "%"+keyword+"%")
		}
		if err := db.Find(&enterprises).Error; err != nil {
			return Page[CreditTargetDTO]{}, err
		}
		for _, ent := range enterprises {
			accountID := ""
			if ent.CreditAccountID != nil {
				accountID = *ent.CreditAccountID
			}
			items = append(items, CreditTargetDTO{TargetType: "enterprise", TargetID: ent.ID, DisplayName: ent.Name, Status: ent.Status, AccountID: accountID})
		}
	}
	return Page[CreditTargetDTO]{Items: items, Limit: limit, Offset: offset, Total: int64(len(items))}, nil
}

func (a *App) AdminGrantCredits(ctx context.Context, in AdminGrantInput) (AdminGrantDTO, error) {
	if in.Auth.AdminID == "" {
		return AdminGrantDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if in.TargetID == "" || in.Points <= 0 || !in.ExpiresAt.After(a.now()) || in.Meta.IdempotencyKey == "" {
		return AdminGrantDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "target_id, positive points, future expires_at and idempotency_key are required")
	}
	account, err := a.resolveGrantAccount(ctx, in.TargetType, in.TargetID)
	if err != nil {
		return AdminGrantDTO{}, err
	}
	hash := requestHash(in.Meta, AuthContext{UserID: in.Auth.AdminID}, map[string]any{"target_type": in.TargetType, "target_id": in.TargetID, "points": in.Points, "expires_at": in.ExpiresAt})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{TenantID: "admin:" + in.Auth.AdminID, Scope: "credit.admin_grant", IdempotencyKey: in.Meta.IdempotencyKey, RequestHash: hash, ActorUserID: in.Auth.AdminID})
	if err != nil {
		return AdminGrantDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return AdminGrantDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "admin grant idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return AdminGrantDTO{BatchID: decision.ReplayResult.ID, AccountID: account.ID}, nil
	}
	var dto AdminGrantDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", account.ID).First(&account).Error; err != nil {
			return err
		}
		batchID := security.RandomID("cb_")
		sourceID := in.Auth.AdminID
		batch := businesscore.CreditBatch{
			ID: batchID, AccountID: account.ID, BatchType: "grant", SourceType: "admin_grant", SourceID: &sourceID,
			TotalPoints: in.Points, RemainingPoints: in.Points, ExpiresAt: &in.ExpiresAt, Status: StatusActive,
			OriginalPoints: in.Points, AvailablePoints: in.Points, GrantedAt: now,
			ExpiryPolicyJSON: creditExpiryPolicyJSON(&in.ExpiresAt), SpendScopeJSON: defaultSpendScopeJSON(), SettlementEligible: true,
			CreatedBy: optionalString(in.Auth.AdminID), UpdatedBy: optionalString(in.Auth.AdminID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		account.AvailablePoints += in.Points
		account.UpdatedBy = optionalString(in.Auth.AdminID)
		account.UpdatedAt = now
		if err := tx.Save(&account).Error; err != nil {
			return err
		}
		if err := tx.Create(ledger(account, "admin_grant", in.Points, "credit_batch", batchID, "", "", in.Meta.TraceID, in.Meta.IdempotencyKey)).Error; err != nil {
			return err
		}
		dto = AdminGrantDTO{BatchID: batchID, AccountID: account.ID, GrantedPoints: in.Points, AvailablePoints: account.AvailablePoints}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return AdminGrantDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "credit_batch", ID: dto.BatchID})
	return dto, nil
}

func (a *App) AdminExpireCreditLots(ctx context.Context, in ExpireCreditLotsInput) (CreditMaintenanceDTO, error) {
	if in.Auth.AdminID == "" {
		return CreditMaintenanceDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if in.Meta.IdempotencyKey == "" || strings.TrimSpace(in.Reason) == "" {
		return CreditMaintenanceDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "reason and idempotency_key are required")
	}
	if strings.TrimSpace(in.LotID) == "" && strings.TrimSpace(in.AccountID) == "" {
		return CreditMaintenanceDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "lot_id or account_id is required")
	}
	limit, _ := normalizePage(in.Limit, 0, 500)
	hash := requestHash(in.Meta, AuthContext{UserID: in.Auth.AdminID}, map[string]any{"account_id": in.AccountID, "lot_id": in.LotID, "limit": limit, "reason": in.Reason})
	decision, err := a.beginAdminMaintenance(ctx, in.Auth, in.Meta, "credit.lots.expire", hash)
	if err != nil {
		return CreditMaintenanceDTO{}, err
	}
	if replay, ok, err := maintenanceReplay(decision, "expire credit lots"); ok || err != nil {
		return replay, err
	}
	operationID := security.RandomID("credit_expire_")
	var dto CreditMaintenanceDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		var lots []businesscore.CreditBatch
		q := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("status = ? AND (available_points > 0 OR frozen_points > 0)", StatusActive).
			Where("expires_at IS NOT NULL AND expires_at <= ?", now)
		if strings.TrimSpace(in.LotID) != "" {
			q = q.Where("id = ?", strings.TrimSpace(in.LotID))
		}
		if strings.TrimSpace(in.AccountID) != "" {
			q = q.Where("account_id = ?", strings.TrimSpace(in.AccountID))
		}
		if err := q.Order("expires_at ASC, granted_at ASC").Limit(limit).Find(&lots).Error; err != nil {
			return err
		}
		var totalExpired int64
		for _, lot := range lots {
			points := lot.AvailablePoints
			if points <= 0 && lot.FrozenPoints > 0 {
				lot.Status = "frozen_only"
				lot.UpdatedBy = optionalString(in.Auth.AdminID)
				lot.UpdatedAt = now
				if err := tx.Save(&lot).Error; err != nil {
					return err
				}
				dto.AccountID = lot.AccountID
				dto.LotID = lot.ID
				dto.AffectedLots++
				continue
			}
			var account businesscore.CreditAccount
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", lot.AccountID).First(&account).Error; err != nil {
				return err
			}
			lot.AvailablePoints -= points
			lot.RemainingPoints -= points
			if lot.RemainingPoints < 0 {
				lot.RemainingPoints = 0
			}
			lot.ExpiredPoints += points
			if lot.FrozenPoints > 0 {
				lot.Status = "frozen_only"
			} else {
				lot.Status = "expired"
			}
			lot.UpdatedBy = optionalString(in.Auth.AdminID)
			lot.UpdatedAt = now
			account.AvailablePoints -= points
			if account.AvailablePoints < 0 {
				account.AvailablePoints = 0
			}
			account.UpdatedBy = optionalString(in.Auth.AdminID)
			account.UpdatedAt = now
			if err := tx.Save(&lot).Error; err != nil {
				return err
			}
			if err := tx.Save(&account).Error; err != nil {
				return err
			}
			entry := ledger(account, "expire", -points, "credit_lot", lot.ID, "", "", in.Meta.TraceID, in.Meta.IdempotencyKey+":"+lot.ID)
			entry.BatchID = optionalString(lot.ID)
			entry.MetadataJSON = mustJSON(map[string]any{"operation_id": operationID, "reason": in.Reason})
			if err := tx.Create(entry).Error; err != nil {
				return err
			}
			dto.AccountID = account.ID
			dto.LotID = lot.ID
			dto.AvailablePoints = account.AvailablePoints
			dto.FrozenPoints = account.FrozenPoints
			totalExpired += points
			dto.AffectedLots++
		}
		dto.OperationID = operationID
		dto.Status = "expired"
		dto.Points = totalExpired
		return a.writeBillingAuditTx(tx, in.Auth.AdminID, in.Meta.TraceID, "credit.lots.expire", "credit_lot", defaultString(in.LotID, in.AccountID), "", "expired", in.Reason)
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return CreditMaintenanceDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "credit_maintenance", ID: operationID})
	return dto, nil
}

func (a *App) AdminRefundCredits(ctx context.Context, in RefundCreditsInput) (CreditMaintenanceDTO, error) {
	if in.Auth.AdminID == "" {
		return CreditMaintenanceDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if in.Meta.IdempotencyKey == "" || strings.TrimSpace(in.Reason) == "" {
		return CreditMaintenanceDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "reason and idempotency_key are required")
	}
	hash := requestHash(in.Meta, AuthContext{UserID: in.Auth.AdminID}, map[string]any{
		"account_id": in.AccountID, "points": in.Points, "original_lot_id": in.OriginalLotID,
		"original_ledger_entry_id": in.OriginalLedgerEntryID, "reason": in.Reason,
	})
	decision, err := a.beginAdminMaintenance(ctx, in.Auth, in.Meta, "credit.refund", hash)
	if err != nil {
		return CreditMaintenanceDTO{}, err
	}
	if replay, ok, err := maintenanceReplay(decision, "refund credits"); ok || err != nil {
		return replay, err
	}
	operationID := security.RandomID("credit_refund_")
	var dto CreditMaintenanceDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		accountID := strings.TrimSpace(in.AccountID)
		points := in.Points
		originalLotID := strings.TrimSpace(in.OriginalLotID)
		if strings.TrimSpace(in.OriginalLedgerEntryID) != "" {
			var original businesscore.CreditLedgerEntry
			if err := tx.Where("id = ?", strings.TrimSpace(in.OriginalLedgerEntryID)).First(&original).Error; err != nil {
				return bizerrors.New(bizerrors.CodeResourceNotFound, "original ledger entry not found")
			}
			if original.PointsDelta >= 0 && points <= 0 {
				return bizerrors.New(bizerrors.CodeStateConflict, "only debit ledger entries can infer refund points")
			}
			if accountID == "" {
				accountID = original.AccountID
			}
			if points <= 0 {
				points = -original.PointsDelta
			}
			if originalLotID == "" {
				originalLotID = value(original.BatchID)
			}
		}
		if accountID == "" || points <= 0 {
			return bizerrors.New(bizerrors.CodeInvalidArgument, "account_id and positive points are required")
		}
		out, err := a.applyRefundCreditsTx(tx, accountID, originalLotID, points, operationID, "refund", "credit_refund", operationID, in.Auth.AdminID, in.Meta.TraceID, in.Meta.IdempotencyKey, in.Reason, in.GracePeriodDays)
		if err != nil {
			return err
		}
		dto = out
		return a.writeBillingAuditTx(tx, in.Auth.AdminID, in.Meta.TraceID, "credit.refund", "credit_account", accountID, "", "refunded", in.Reason)
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return CreditMaintenanceDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "credit_maintenance", ID: operationID})
	return dto, nil
}

func (a *App) AdminReverseCreditLedgerEntry(ctx context.Context, in ReverseCreditLedgerEntryInput) (CreditMaintenanceDTO, error) {
	if in.Auth.AdminID == "" {
		return CreditMaintenanceDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if strings.TrimSpace(in.LedgerEntryID) == "" || strings.TrimSpace(in.Reason) == "" || in.Meta.IdempotencyKey == "" {
		return CreditMaintenanceDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "ledger_entry_id, reason and idempotency_key are required")
	}
	hash := requestHash(in.Meta, AuthContext{UserID: in.Auth.AdminID}, map[string]any{"ledger_entry_id": in.LedgerEntryID, "reason": in.Reason})
	decision, err := a.beginAdminMaintenance(ctx, in.Auth, in.Meta, "credit.ledger.reverse", hash)
	if err != nil {
		return CreditMaintenanceDTO{}, err
	}
	if replay, ok, err := maintenanceReplay(decision, "reverse credit ledger"); ok || err != nil {
		return replay, err
	}
	operationID := security.RandomID("credit_reverse_")
	var dto CreditMaintenanceDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var original businesscore.CreditLedgerEntry
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", strings.TrimSpace(in.LedgerEntryID)).First(&original).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "ledger entry not found")
		}
		if original.EntryType == "reverse" || original.SourceType == "ledger_reversal" {
			return bizerrors.New(bizerrors.CodeStateConflict, "ledger reversal cannot be reversed again")
		}
		var account businesscore.CreditAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", original.AccountID).First(&account).Error; err != nil {
			return err
		}
		reverseDelta := -original.PointsDelta
		if reverseDelta > 0 {
			out, err := a.applyRefundCreditsTx(tx, account.ID, value(original.BatchID), reverseDelta, operationID, "reverse", "ledger_reversal", original.ID, in.Auth.AdminID, in.Meta.TraceID, in.Meta.IdempotencyKey, in.Reason, 7)
			if err != nil {
				return err
			}
			dto = out
		} else if reverseDelta < 0 {
			points := -reverseDelta
			lotID := value(original.BatchID)
			if lotID == "" {
				return bizerrors.New(bizerrors.CodeInvalidArgument, "positive ledger reversal requires original batch_id")
			}
			var lot businesscore.CreditBatch
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND account_id = ?", lotID, account.ID).First(&lot).Error; err != nil {
				return bizerrors.New(bizerrors.CodeResourceNotFound, "original credit lot not found")
			}
			if account.AvailablePoints < points || lot.AvailablePoints < points {
				return bizerrors.New(bizerrors.CodeStateConflict, "available points are insufficient for ledger reversal")
			}
			lot.AvailablePoints -= points
			lot.RemainingPoints -= points
			if lot.RemainingPoints < 0 {
				lot.RemainingPoints = 0
			}
			lot.ConsumedPoints += points
			if lot.AvailablePoints == 0 && lot.FrozenPoints == 0 {
				lot.Status = "exhausted"
			}
			lot.UpdatedBy = optionalString(in.Auth.AdminID)
			lot.UpdatedAt = a.now()
			account.AvailablePoints -= points
			account.UpdatedBy = optionalString(in.Auth.AdminID)
			account.UpdatedAt = lot.UpdatedAt
			if err := tx.Save(&lot).Error; err != nil {
				return err
			}
			if err := tx.Save(&account).Error; err != nil {
				return err
			}
			dto = CreditMaintenanceDTO{OperationID: operationID, Status: "reversed", AccountID: account.ID, LotID: lot.ID, Points: reverseDelta, AvailablePoints: account.AvailablePoints, FrozenPoints: account.FrozenPoints}
		} else {
			dto = CreditMaintenanceDTO{OperationID: operationID, Status: "reversed", AccountID: account.ID, Points: 0, AvailablePoints: account.AvailablePoints, FrozenPoints: account.FrozenPoints}
		}
		if reverseDelta <= 0 {
			entry := ledger(account, "reverse", reverseDelta, "ledger_reversal", original.ID, value(original.ProjectID), value(original.RunID), in.Meta.TraceID, in.Meta.IdempotencyKey)
			entry.BatchID = original.BatchID
			entry.MetadataJSON = mustJSON(map[string]any{"operation_id": operationID, "reason": in.Reason, "original_entry_type": original.EntryType})
			if err := tx.Create(entry).Error; err != nil {
				return err
			}
			dto.LedgerEntryID = entry.ID
		}
		dto.ReversedLedgerEntryID = original.ID
		return a.writeBillingAuditTx(tx, in.Auth.AdminID, in.Meta.TraceID, "credit.ledger.reverse", "credit_ledger_entry", original.ID, original.EntryType, "reverse", in.Reason)
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return CreditMaintenanceDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "credit_maintenance", ID: operationID})
	return dto, nil
}

func (a *App) ListRedeemCodes(ctx context.Context, _ admin.AdminAuth, limit, offset int) (Page[RedeemCodeDTO], error) {
	limit, offset = normalizePage(limit, offset, 100)
	var rows []businesscore.RedeemCodeBatch
	if err := a.repo.DB().WithContext(ctx).Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[RedeemCodeDTO]{}, err
	}
	items := make([]RedeemCodeDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, redeemBatchDTO(row))
	}
	var total int64
	_ = a.repo.DB().WithContext(ctx).Model(&businesscore.RedeemCodeBatch{}).Count(&total).Error
	return Page[RedeemCodeDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) CreateRedeemCodes(ctx context.Context, in CreateCodesInput) (CreateCodesDTO, error) {
	if in.Auth.AdminID == "" {
		return CreateCodesDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	now := a.now()
	accountType := normalizeAccountType(in.AccountType)
	bindTargetType := normalizeBindTargetType(in.BindTargetType)
	if in.Count <= 0 || in.Count > 1000 || in.Points <= 0 || !in.CodeExpiresAt.After(now) || !in.CreditExpiresAt.After(now) || in.Meta.IdempotencyKey == "" ||
		accountType == "" || bindTargetType == "" || !redeemBindTargetMatchesAccount(accountType, bindTargetType) {
		return CreateCodesDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "invalid redeem code batch request")
	}
	if (bindTargetType == "user" || bindTargetType == "enterprise") && strings.TrimSpace(in.BindTargetID) == "" {
		return CreateCodesDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "bind_target_id is required")
	}
	if bindTargetType == "channel" && strings.TrimSpace(in.Channel) == "" {
		return CreateCodesDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "channel is required")
	}
	hash := requestHash(in.Meta, AuthContext{UserID: in.Auth.AdminID}, map[string]any{
		"count": in.Count, "points": in.Points,
		"code_expires_at":   in.CodeExpiresAt.UTC().Format(time.RFC3339Nano),
		"credit_expires_at": in.CreditExpiresAt.UTC().Format(time.RFC3339Nano),
		"account_type":      accountType, "bind_target_type": bindTargetType,
		"bind_target_id": strings.TrimSpace(in.BindTargetID), "channel": strings.TrimSpace(in.Channel), "reason": strings.TrimSpace(in.Reason),
	})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{TenantID: "admin:" + in.Auth.AdminID, Scope: "credit.codes.create", IdempotencyKey: in.Meta.IdempotencyKey, RequestHash: hash, ActorUserID: in.Auth.AdminID})
	if err != nil {
		return CreateCodesDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return CreateCodesDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "redeem code idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return CreateCodesDTO{BatchID: decision.ReplayResult.ID}, nil
	}
	var dto CreateCodesDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		batchID := security.RandomID("rcb_")
		batchNo := "RCB-" + strings.ToUpper(batchID[4:12])
		batch := businesscore.RedeemCodeBatch{
			ID: batchID, BatchNo: batchNo, TargetType: bindTargetType,
			AccountType: accountType, BindTargetType: bindTargetType, BindTargetID: optionalString(in.BindTargetID),
			TargetUserID: targetPtr(bindTargetType, "user", in.BindTargetID), TargetEnterpriseID: targetPtr(bindTargetType, "enterprise", in.BindTargetID),
			ChannelCode: optionalString(in.Channel), TotalCodes: in.Count, PointsPerCode: in.Points, ExpiresAt: &in.CodeExpiresAt,
			CreditExpiresAt: &in.CreditExpiresAt, Status: StatusActive, CreatedByAdminID: &in.Auth.AdminID, Reason: optionalString(in.Reason),
			CreatedBy: optionalString(in.Auth.AdminID), UpdatedBy: optionalString(in.Auth.AdminID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		codes := make([]string, 0, in.Count)
		for i := 0; i < in.Count; i++ {
			code := "DORA-" + strings.ToUpper(security.RandomID("")[0:16])
			codes = append(codes, code)
			row := businesscore.RedeemCode{
				ID: security.RandomID("rc_"), BatchID: batch.ID, CodeDigest: codeDigest(code), Status: "unused", ExpiresAt: &in.CodeExpiresAt,
				CreatedBy: optionalString(in.Auth.AdminID), UpdatedBy: optionalString(in.Auth.AdminID), CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		}
		dto = CreateCodesDTO{BatchID: batch.ID, BatchNo: batch.BatchNo, Codes: codes, Count: len(codes)}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return CreateCodesDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "redeem_code_batch", ID: dto.BatchID})
	return dto, nil
}

func (a *App) DisableRedeemCodeBatch(ctx context.Context, auth admin.AdminAuth, batchID string) (RedeemCodeDTO, error) {
	if auth.AdminID == "" {
		return RedeemCodeDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	var batch businesscore.RedeemCodeBatch
	if err := a.repo.DB().WithContext(ctx).Where("id = ? OR batch_no = ?", batchID, batchID).First(&batch).Error; err != nil {
		return RedeemCodeDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "redeem code batch not found")
	}
	batch.Status = "disabled"
	now := a.now()
	batch.UpdatedBy = optionalString(auth.AdminID)
	batch.UpdatedAt = now
	if err := a.repo.DB().WithContext(ctx).Save(&batch).Error; err != nil {
		return RedeemCodeDTO{}, err
	}
	_ = a.repo.DB().WithContext(ctx).Model(&businesscore.RedeemCode{}).Where("batch_id = ? AND status = ?", batch.ID, "unused").Updates(map[string]any{"status": "disabled", "updated_by": auth.AdminID, "updated_at": now}).Error
	return redeemBatchDTO(batch), nil
}

func (a *App) ExportRedeemCodes(ctx context.Context, auth admin.AdminAuth, batchID string) (map[string]any, error) {
	if auth.AdminID == "" {
		return nil, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	var batch businesscore.RedeemCodeBatch
	if err := a.repo.DB().WithContext(ctx).Where("id = ? OR batch_no = ?", batchID, batchID).First(&batch).Error; err != nil {
		return nil, bizerrors.New(bizerrors.CodeResourceNotFound, "redeem code batch not found")
	}
	var count int64
	_ = a.repo.DB().WithContext(ctx).Model(&businesscore.RedeemCode{}).Where("batch_id = ?", batch.ID).Count(&count).Error
	return map[string]any{"batch_id": batch.ID, "batch_no": batch.BatchNo, "code_count": count, "export_note": "plain codes are returned only at creation time"}, nil
}

func (a *App) createEstimate(ctx context.Context, auth AuthContext, meta RequestMeta, account businesscore.CreditAccount, projectID, resourceType, modelID, pricingSnapshotID string, points int64, lineItems []EstimateLineItemDTO, evidence *businessagent.SafetyEvidenceDTO) (EstimateDTO, error) {
	now := a.now()
	expiresAt := now.Add(15 * time.Minute)
	estimateID := security.RandomID("est_")
	insufficient := points > account.AvailablePoints
	err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row := businesscore.CreditEstimate{
			ID: security.RandomID("cest_"), EstimateID: estimateID, AccountID: account.ID, ProjectID: projectID,
			ResourceType: optionalString(resourceType), ModelID: optionalString(modelID), PricingSnapshotID: optionalString(pricingSnapshotID),
			EstimatePoints: points, AvailablePoints: account.AvailablePoints, ExpiresSoonPoints: account.ExpiresSoonPoints,
			AccountType: account.AccountType, Insufficient: insufficient, Status: StatusEstimated, ExpiresAt: expiresAt,
			CreatedByUserID: auth.UserID, TraceID: meta.TraceID, RequestMetaJSON: mustJSON(meta),
			SafetyEvidenceID: optionalString(evidence.GetSafetyEvidenceId()), SafetyEvidenceHash: optionalString(safetyDigest(evidence)),
			CreatedBy: optionalString(auth.UserID), UpdatedBy: optionalString(auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		for order, item := range lineItems {
			item.EstimateItemID = defaultString(item.EstimateItemID, security.RandomID("est_item_"))
			row := businesscore.CreditEstimateItem{
				ID: security.RandomID("cesti_"), EstimateID: estimateID, EstimateItemID: item.EstimateItemID, ItemType: item.ItemType,
				ToolName: optionalString(item.ToolName), ToolType: optionalString(item.ToolType), PricingPolicyID: optionalString(item.PricingPolicyID),
				ModelID: optionalString(item.ModelID), ResourceType: optionalString(item.ResourceType), BillingUnit: optionalString(item.BillingUnit),
				Quantity: optionalFloat(item.Quantity), UnitPoints: optionalFloat(item.UnitPoints), EstimatePoints: item.EstimatePoints,
				FreeReason: optionalString(item.FreeReason), Status: StatusEstimated, MetadataJSON: mustJSON(map[string]any{"order": order, "metadata_summary": item.Metadata}),
				CreatedBy: optionalString(auth.UserID), UpdatedBy: optionalString(auth.UserID), CreatedAt: now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			lineItems[order].EstimateItemID = item.EstimateItemID
		}
		return nil
	})
	if err != nil {
		return EstimateDTO{}, err
	}
	return EstimateDTO{
		EstimateID: estimateID, EstimatePoints: points, AvailablePoints: account.AvailablePoints,
		ExpiresSoonPoints: account.ExpiresSoonPoints, CreditAccountScope: account.AccountType, CreditAccountID: account.ID,
		LineItems: lineItems, ExpiresAt: expiresAt, Insufficient: insufficient,
	}, nil
}

func (a *App) resolveAccount(ctx context.Context, db *gorm.DB, auth AuthContext) (businesscore.CreditAccount, error) {
	if auth.UserID == "" || auth.SpaceID == "" {
		return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	var space businesscore.Space
	if err := db.WithContext(ctx).Where("id = ? AND status = ?", auth.SpaceID, StatusActive).First(&space).Error; err != nil {
		return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodePermissionDenied, "space is not active")
	}
	var account businesscore.CreditAccount
	if space.CreditAccountID != nil && *space.CreditAccountID != "" {
		if err := db.WithContext(ctx).Where("id = ? AND status = ?", *space.CreditAccountID, StatusActive).First(&account).Error; err == nil {
			return account, nil
		}
	}
	if space.SpaceType == accountspace.SpaceEnterprise || auth.EnterpriseID != "" {
		enterpriseID := auth.EnterpriseID
		if enterpriseID == "" && space.EnterpriseID != nil {
			enterpriseID = *space.EnterpriseID
		}
		if enterpriseID == "" {
			return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodePermissionDenied, "enterprise credit account is unavailable")
		}
		err := db.WithContext(ctx).Where("account_type = ? AND enterprise_id = ? AND status = ?", "enterprise", enterpriseID, StatusActive).First(&account).Error
		if err != nil {
			return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeResourceNotFound, "enterprise credit account not found")
		}
		return account, nil
	}
	err := db.WithContext(ctx).Where("account_type = ? AND owner_user_id = ? AND status = ?", "personal", auth.UserID, StatusActive).First(&account).Error
	if err != nil {
		return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeResourceNotFound, "personal credit account not found")
	}
	return account, nil
}

func (a *App) resolveRedeemAccount(ctx context.Context, auth AuthContext, accountType string) (businesscore.CreditAccount, error) {
	if auth.UserID == "" {
		return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	var account businesscore.CreditAccount
	switch accountType {
	case "personal":
		err := a.repo.DB().WithContext(ctx).Where("account_type = ? AND owner_user_id = ? AND status = ?", "personal", auth.UserID, StatusActive).First(&account).Error
		if err != nil {
			return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeResourceNotFound, "personal credit account not found")
		}
		return account, nil
	case "enterprise":
		if auth.EnterpriseID == "" {
			return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeRedeemCodeTargetMismatch, "enterprise context is required")
		}
		err := a.repo.DB().WithContext(ctx).Where("account_type = ? AND enterprise_id = ? AND status = ?", "enterprise", auth.EnterpriseID, StatusActive).First(&account).Error
		if err != nil {
			return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeResourceNotFound, "enterprise credit account not found")
		}
		return account, nil
	default:
		return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeInvalidArgument, "target_account_type is invalid")
	}
}

func (a *App) resolveGrantAccount(ctx context.Context, targetType, targetID string) (businesscore.CreditAccount, error) {
	var account businesscore.CreditAccount
	switch strings.TrimSpace(targetType) {
	case "enterprise":
		if err := a.repo.DB().WithContext(ctx).Where("account_type = ? AND enterprise_id = ?", "enterprise", targetID).First(&account).Error; err != nil {
			return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeResourceNotFound, "enterprise credit account not found")
		}
	default:
		if err := a.repo.DB().WithContext(ctx).Where("account_type = ? AND owner_user_id = ?", "personal", targetID).First(&account).Error; err != nil {
			return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeResourceNotFound, "personal credit account not found")
		}
	}
	return account, nil
}

func (a *App) resolvePackagePurchaseAccount(ctx context.Context, auth AuthContext, pkg businesscore.RechargePackage, requestedAccountType string) (businesscore.CreditAccount, error) {
	targetScope := strings.TrimSpace(pkg.TargetScope)
	switch targetScope {
	case "enterprise":
		if auth.EnterpriseID == "" || auth.EnterpriseRole != accountspace.RoleOwner {
			return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodePermissionDenied, "enterprise owner permission is required to buy enterprise packages")
		}
		return a.resolveRedeemAccount(ctx, auth, "enterprise")
	case "personal", "creator", "":
		if normalizeAccountType(requestedAccountType) == "enterprise" {
			return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeInvalidArgument, "personal package cannot be bought for enterprise account")
		}
		return a.resolveRedeemAccount(ctx, auth, "personal")
	default:
		return businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeInvalidArgument, "package target_scope is unsupported")
	}
}

func (a *App) resolvePackagePrice(ctx context.Context, pkg businesscore.RechargePackage, skuID string) (*string, int64, string, error) {
	priceAmount := pkg.PriceAmount
	if priceAmount == 0 {
		priceAmount = pkg.PriceCents
	}
	currency := defaultString(pkg.Currency, "CNY")
	if skuID == "" {
		return nil, priceAmount, currency, nil
	}
	now := a.now()
	var sku businesscore.BillingPackageSKU
	err := a.repo.DB().WithContext(ctx).
		Where("sku_id = ? AND package_id = ? AND status = ?", skuID, pkg.PackageID, StatusActive).
		Where("effective_at <= ? AND (expired_at IS NULL OR expired_at > ?)", now, now).
		First(&sku).Error
	if err != nil {
		return nil, 0, "", bizerrors.New(bizerrors.CodeResourceNotFound, "billing package sku not found")
	}
	priceAmount = sku.PriceAmount
	if sku.ActivityPriceAmount != nil {
		priceAmount = *sku.ActivityPriceAmount
	}
	return &sku.SKUID, priceAmount, defaultString(sku.Currency, currency), nil
}

func (a *App) summaryDTO(ctx context.Context, account businesscore.CreditAccount) (SummaryDTO, error) {
	var nearest *time.Time
	var batch businesscore.CreditBatch
	err := a.repo.DB().WithContext(ctx).
		Where("account_id = ? AND status = ? AND remaining_points > 0 AND (expires_at IS NULL OR expires_at > ?)", account.ID, StatusActive, a.now()).
		Order("expires_at ASC NULLS LAST, created_at ASC").First(&batch).Error
	if err == nil {
		nearest = batch.ExpiresAt
	}
	return SummaryDTO{AccountID: account.ID, AccountType: account.AccountType, AvailablePoints: account.AvailablePoints, FrozenPoints: account.FrozenPoints, ExpiresSoonPoints: account.ExpiresSoonPoints, NearestExpireAt: nearest}, nil
}

func (a *App) listLedgerForAccount(ctx context.Context, accountID string, limit, offset int) (Page[LedgerDTO], error) {
	limit, offset = normalizePage(limit, offset, 100)
	var rows []businesscore.CreditLedgerEntry
	if err := a.repo.DB().WithContext(ctx).Where("account_id = ?", accountID).Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[LedgerDTO]{}, err
	}
	var total int64
	_ = a.repo.DB().WithContext(ctx).Model(&businesscore.CreditLedgerEntry{}).Where("account_id = ?", accountID).Count(&total).Error
	return ledgerPage(rows, total, limit, offset), nil
}

func (a *App) listCreditLots(ctx context.Context, accountID, sourceType, status string, limit, offset int) (Page[CreditLotDTO], error) {
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.CreditBatch{})
	if strings.TrimSpace(accountID) != "" {
		db = db.Where("account_id = ?", strings.TrimSpace(accountID))
	}
	if strings.TrimSpace(sourceType) != "" {
		db = db.Where("source_type = ?", strings.TrimSpace(sourceType))
	}
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[CreditLotDTO]{}, err
	}
	var rows []businesscore.CreditBatch
	if err := db.Order("expires_at ASC NULLS LAST, granted_at ASC, created_at ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[CreditLotDTO]{}, err
	}
	return Page[CreditLotDTO]{Items: creditLotDTOs(rows), Limit: limit, Offset: offset, Total: total}, nil
}

// listLedgerForMember 仅返回成员本人在企业空间产生的流水(ACCT-3：成员只看自己的消耗明细)。
// 归属以"本人在企业空间发起的 project"为界——企业 project.owner_user_id = 发起成员；
// 无 project 的兑换/发放流水属拥有者范畴，成员不可见，子查询自然排除。
func (a *App) listLedgerForMember(ctx context.Context, accountID, memberUserID, enterpriseID string, limit, offset int) (Page[LedgerDTO], error) {
	limit, offset = normalizePage(limit, offset, 100)
	memberProjects := a.repo.DB().WithContext(ctx).Model(&businesscore.Project{}).
		Select("id").Where("owner_user_id = ? AND enterprise_id = ?", memberUserID, enterpriseID)
	var rows []businesscore.CreditLedgerEntry
	if err := a.repo.DB().WithContext(ctx).
		Where("account_id = ? AND project_id IN (?)", accountID, memberProjects).
		Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[LedgerDTO]{}, err
	}
	var total int64
	_ = a.repo.DB().WithContext(ctx).Model(&businesscore.CreditLedgerEntry{}).
		Where("account_id = ? AND project_id IN (?)", accountID, memberProjects).Count(&total).Error
	return ledgerPage(rows, total, limit, offset), nil
}

func ledgerPage(rows []businesscore.CreditLedgerEntry, total int64, limit, offset int) Page[LedgerDTO] {
	items := make([]LedgerDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, LedgerDTO{EntryID: row.ID, EntryType: row.EntryType, Amount: row.PointsDelta, BalanceAfter: row.BalanceAfter, ResourceType: row.SourceType, ResourceID: row.SourceID, CreatedAt: row.CreatedAt})
	}
	return Page[LedgerDTO]{Items: items, Limit: limit, Offset: offset, Total: total}
}

func creditLotDTOs(rows []businesscore.CreditBatch) []CreditLotDTO {
	items := make([]CreditLotDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, creditLotDTO(row))
	}
	return items
}

func creditLotDTO(row businesscore.CreditBatch) CreditLotDTO {
	return CreditLotDTO{
		LotID: row.ID, AccountID: row.AccountID, SourceType: row.SourceType, SourceID: value(row.SourceID),
		OriginalPoints: row.OriginalPoints, AvailablePoints: row.AvailablePoints, FrozenPoints: row.FrozenPoints,
		ConsumedPoints: row.ConsumedPoints, ExpiredPoints: row.ExpiredPoints, GrantedAt: row.GrantedAt, ExpiresAt: row.ExpiresAt,
		ExpiryPolicyJSON: string(row.ExpiryPolicyJSON), SpendScopeJSON: string(row.SpendScopeJSON),
		SettlementEligible: row.SettlementEligible, Status: row.Status,
	}
}

func rechargePackageDTO(row businesscore.RechargePackage) RechargePackageDTO {
	return RechargePackageDTO{
		PackageID: row.PackageID, PackageType: row.PackageType, Name: defaultString(row.Name, row.DisplayName),
		DisplayName: row.DisplayName, TargetScope: row.TargetScope, BillingMode: row.BillingMode,
		Points: packageTotalPoints(row), GrantedPoints: row.GrantedPoints, BonusPoints: row.BonusPoints,
		PriceCents: packagePriceAmount(row), PriceAmount: packagePriceAmount(row), Currency: row.Currency,
		CreditExpiryPolicy: defaultString(row.CreditExpiryPolicy, row.CreditValidDuration),
		SpendScope:         jsonStringSlice(row.SpendScopeJSON), SettlementEligible: row.SettlementEligible,
		EntitlementPolicy: jsonObject(row.EntitlementPolicy), RenewalPolicy: jsonObject(row.RenewalPolicy),
		RefundPolicy: jsonObject(row.RefundPolicy), VisibleScope: row.VisibleScope, Status: row.Status, UpdatedAt: row.UpdatedAt,
	}
}

func creditAccountDTO(row businesscore.CreditAccount) CreditAccountDTO {
	return CreditAccountDTO{
		AccountID: row.ID, AccountType: row.AccountType, OwnerUserID: value(row.OwnerUserID), EnterpriseID: value(row.EnterpriseID),
		AvailablePoints: row.AvailablePoints, FrozenPoints: row.FrozenPoints, ExpiresSoonPoints: row.ExpiresSoonPoints, Status: row.Status,
	}
}

func billingPackageSKUDTO(row businesscore.BillingPackageSKU) BillingPackageSKUDTO {
	return BillingPackageSKUDTO{
		SKUID: row.SKUID, PackageID: row.PackageID, ChannelCode: row.ChannelCode, PriceAmount: row.PriceAmount,
		Currency: row.Currency, ActivityPriceAmount: row.ActivityPriceAmount, Status: row.Status,
		EffectiveAt: row.EffectiveAt, ExpiredAt: row.ExpiredAt,
	}
}

func entitlementSnapshotDTO(row businesscore.PackageEntitlementSnapshot) EntitlementSnapshotDTO {
	return EntitlementSnapshotDTO{
		EntitlementSnapshotID: row.EntitlementSnapshotID, AccountID: row.AccountID, UserID: value(row.UserID),
		EnterpriseID: value(row.EnterpriseID), PackageID: row.PackageID, OrderID: row.OrderID, TargetScope: row.TargetScope,
		EntitlementPolicy: jsonObject(row.EntitlementPolicy), Status: row.Status, EffectiveAt: row.EffectiveAt, ExpiresAt: row.ExpiresAt,
	}
}

func enterpriseContractDTO(row businesscore.EnterpriseContract) EnterpriseContractDTO {
	return EnterpriseContractDTO{
		ContractID: row.ContractID, EnterpriseID: row.EnterpriseID, PackageID: row.PackageID, OrderID: value(row.OrderID),
		ContractStatus: row.ContractStatus, BillingMode: row.BillingMode, PeriodStart: row.PeriodStart, PeriodEnd: row.PeriodEnd,
		SeatQuota: row.SeatQuota, BudgetPoints: row.BudgetPoints, ApprovalPolicy: jsonObject(row.ApprovalPolicyJSON),
		InvoicePolicy: jsonObject(row.InvoicePolicyJSON),
	}
}

func billingInvoiceDTO(row businesscore.BillingInvoice) BillingInvoiceDTO {
	return BillingInvoiceDTO{
		InvoiceID: row.InvoiceID, EnterpriseID: value(row.EnterpriseID), OrderID: value(row.OrderID),
		Amount: row.Amount, Currency: row.Currency, InvoiceStatus: row.InvoiceStatus, IssuedAt: row.IssuedAt,
		DueAt: row.DueAt, Metadata: jsonObject(row.MetadataJSON),
	}
}

func billingPromotionDTO(row businesscore.BillingPromotion) BillingPromotionDTO {
	return BillingPromotionDTO{
		PromotionID: row.PromotionID, PromotionName: row.PromotionName, PackageID: value(row.PackageID),
		DiscountPolicy: jsonObject(row.DiscountPolicyJSON), VisibleScope: row.VisibleScope, Status: row.Status,
		StartsAt: row.StartsAt, EndsAt: row.EndsAt,
	}
}

func (a *App) activeModelPrice(ctx context.Context, modelID, resourceType, pricingSnapshotID string) (businesscore.ModelPrice, error) {
	var price businesscore.ModelPrice
	err := a.repo.DB().WithContext(ctx).
		Where("model_id = ? AND resource_type = ? AND pricing_snapshot_id = ? AND status = ?", modelID, resourceType, pricingSnapshotID, StatusActive).
		Where("(expired_at IS NULL OR expired_at > ?)", a.now()).
		Order("effective_at DESC").
		First(&price).Error
	if err != nil {
		return businesscore.ModelPrice{}, bizerrors.New(bizerrors.CodeResourceNotFound, "model pricing snapshot is not available")
	}
	return price, nil
}

func (a *App) estimateToolLine(ctx context.Context, item ToolUsageItem) (EstimateLineItemDTO, error) {
	if item.ToolName == "" || item.ToolType == "" || item.Quantity < 0 {
		return EstimateLineItemDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "tool estimate item is invalid")
	}
	var price businesscore.ToolPricingPolicy
	err := a.repo.DB().WithContext(ctx).
		Where("tool_name = ? AND tool_type = ? AND status = ?", item.ToolName, item.ToolType, StatusActive).
		Where("(expired_at IS NULL OR expired_at > ?)", a.now()).
		Order("effective_at DESC").
		First(&price).Error
	if err != nil || price.ChargeMode == "no_charge" {
		return EstimateLineItemDTO{
			EstimateItemID: security.RandomID("est_item_"), ItemType: "tool_usage", ToolName: item.ToolName, ToolType: item.ToolType,
			BillingUnit: defaultString(item.BillingUnit, "call"), Quantity: item.Quantity, FreeReason: "no_charge", Metadata: item.MetadataSummary,
		}, nil
	}
	chargeable := math.Max(item.Quantity-float64(price.FreeQuota), 0)
	points := estimatePoints(chargeable, price.UnitPoints, price.MinChargePoints)
	return EstimateLineItemDTO{
		EstimateItemID: security.RandomID("est_item_"), ItemType: "tool_usage", ToolName: item.ToolName, ToolType: item.ToolType,
		BillingUnit: price.BillingUnit, Quantity: item.Quantity, UnitPoints: price.UnitPoints, EstimatePoints: points,
		PricingPolicyID: price.PricingPolicyID, Metadata: item.MetadataSummary,
	}, nil
}

func (a *App) allocateFreezeBatches(tx *gorm.DB, accountID, freezeID string, points int64, operatorID string, now time.Time) error {
	var batches []businesscore.CreditBatch
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("account_id = ? AND status = ? AND remaining_points > 0", accountID, StatusActive).
		Where("(expires_at IS NULL OR expires_at > ?)", now).
		Order("expires_at ASC NULLS LAST, created_at ASC").
		Find(&batches).Error; err != nil {
		return err
	}
	remaining := points
	for _, batch := range batches {
		if remaining <= 0 {
			break
		}
		take := batch.AvailablePoints
		if take <= 0 {
			take = batch.RemainingPoints
		}
		if take > remaining {
			take = remaining
		}
		batch.RemainingPoints -= take
		batch.AvailablePoints -= take
		batch.FrozenPoints += take
		batch.UpdatedBy = optionalString(operatorID)
		batch.UpdatedAt = now
		if err := tx.Save(&batch).Error; err != nil {
			return err
		}
		item := businesscore.CreditFreezeBatchItem{
			ID: security.RandomID("cfbi_"), FreezeID: freezeID, AccountID: accountID, BatchID: batch.ID, FrozenPoints: take,
			Status: StatusFrozen, CreatedBy: optionalString(operatorID), UpdatedBy: optionalString(operatorID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		remaining -= take
	}
	if remaining > 0 {
		return bizerrors.New(bizerrors.CodeStateConflict, "insufficient unexpired credit batches")
	}
	return nil
}

func (a *App) consumeFreezeRows(tx *gorm.DB, freezeID string, points int64, operatorID string, now time.Time) (int64, error) {
	if points <= 0 {
		return 0, nil
	}
	var rows []businesscore.CreditFreezeBatchItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("freeze_id = ? AND status = ?", freezeID, StatusFrozen).
		Order("created_at ASC").Find(&rows).Error; err != nil {
		return 0, err
	}
	remaining := points
	var consumed int64
	for _, row := range rows {
		if remaining <= 0 {
			break
		}
		available := row.FrozenPoints - row.ChargedPoints - row.ReleasedPoints
		if available <= 0 {
			continue
		}
		take := available
		if take > remaining {
			take = remaining
		}
		var batch businesscore.CreditBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", row.BatchID).First(&batch).Error; err != nil {
			return 0, err
		}
		if batch.FrozenPoints < take {
			return 0, bizerrors.New(bizerrors.CodeStateConflict, "credit lot frozen points are insufficient")
		}
		batch.FrozenPoints -= take
		batch.ConsumedPoints += take
		batch.UpdatedBy = optionalString(operatorID)
		batch.UpdatedAt = now
		if err := tx.Save(&batch).Error; err != nil {
			return 0, err
		}
		row.ChargedPoints += take
		if row.ChargedPoints+row.ReleasedPoints >= row.FrozenPoints {
			row.Status = StatusCharged
		}
		row.UpdatedBy = optionalString(operatorID)
		row.UpdatedAt = now
		if err := tx.Save(&row).Error; err != nil {
			return 0, err
		}
		consumed += take
		remaining -= take
	}
	if remaining > 0 {
		return consumed, bizerrors.New(bizerrors.CodeStateConflict, "charge points exceed unsettled freeze allocations")
	}
	return consumed, nil
}

func (a *App) lockFreezeAndAccount(tx *gorm.DB, freezeID string) (businesscore.CreditFreeze, businesscore.CreditAccount, error) {
	var freeze businesscore.CreditFreeze
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("freeze_id = ?", freezeID).First(&freeze).Error; err != nil {
		return freeze, businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeResourceNotFound, "credit freeze not found")
	}
	var account businesscore.CreditAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", freeze.AccountID).First(&account).Error; err != nil {
		return freeze, account, err
	}
	return freeze, account, nil
}

func (a *App) getReleaseDTO(ctx context.Context, freezeID, idempotencyKey string) (ReleaseDTO, error) {
	var freeze businesscore.CreditFreeze
	if err := a.repo.DB().WithContext(ctx).Where("freeze_id = ?", freezeID).First(&freeze).Error; err != nil {
		return ReleaseDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "credit freeze not found")
	}
	var entry businesscore.CreditLedgerEntry
	err := a.repo.DB().WithContext(ctx).
		Where("entry_type = ? AND source_type = ? AND source_id = ? AND idempotency_key = ?", "release", "credit_freeze", freezeID, idempotencyKey).
		Order("created_at DESC").First(&entry).Error
	if err != nil {
		return ReleaseDTO{}, err
	}
	return ReleaseDTO{ReleasedPoints: entry.PointsDelta, ReleaseStatus: freeze.Status}, nil
}

func (a *App) releaseFreezeLocked(tx *gorm.DB, freezeID string, releasePoints int64, reason, runID, operatorID, traceID, idempotencyKey string) (businesscore.CreditFreeze, int64, error) {
	freeze, account, err := a.lockFreezeAndAccount(tx, freezeID)
	if err != nil {
		return freeze, 0, err
	}
	if runID != "" && freeze.RunID != runID {
		return freeze, 0, bizerrors.New(bizerrors.CodeStateConflict, "release run does not match freeze")
	}
	remaining := freeze.FrozenPoints - freeze.ChargedPoints - freeze.ReleasedPoints
	if remaining <= 0 {
		return freeze, 0, nil
	}
	if releasePoints > remaining {
		releasePoints = remaining
	}
	now := a.now()
	updated, released, err := a.releaseFreezeRows(tx, &freeze, &account, releasePoints, operatorID, now)
	if err != nil {
		return updated, 0, err
	}
	updated.ReleasedPoints += released
	if updated.ChargedPoints+updated.ReleasedPoints >= updated.FrozenPoints {
		updated.Status = StatusReleased
	}
	updated.UpdatedBy = optionalString(operatorID)
	updated.UpdatedAt = now
	account.UpdatedBy = optionalString(operatorID)
	account.UpdatedAt = updated.UpdatedAt
	if err := tx.Save(&account).Error; err != nil {
		return updated, 0, err
	}
	if err := tx.Save(&updated).Error; err != nil {
		return updated, 0, err
	}
	if err := tx.Create(ledger(account, "release", released, "credit_freeze", freezeID, updated.ProjectID, updated.RunID, traceID, idempotencyKey)).Error; err != nil {
		return updated, 0, err
	}
	return updated, released, nil
}

func (a *App) releaseFreezeRows(tx *gorm.DB, freeze *businesscore.CreditFreeze, account *businesscore.CreditAccount, releasePoints int64, operatorID string, now time.Time) (businesscore.CreditFreeze, int64, error) {
	var rows []businesscore.CreditFreezeBatchItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("freeze_id = ? AND status = ?", freeze.FreezeID, StatusFrozen).
		Order("created_at ASC").Find(&rows).Error; err != nil {
		return *freeze, 0, err
	}
	remaining := releasePoints
	var released int64
	for _, row := range rows {
		if remaining <= 0 {
			break
		}
		available := row.FrozenPoints - row.ChargedPoints - row.ReleasedPoints
		if available <= 0 {
			continue
		}
		take := available
		if take > remaining {
			take = remaining
		}
		var batch businesscore.CreditBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", row.BatchID).First(&batch).Error; err != nil {
			return *freeze, 0, err
		}
		if batch.FrozenPoints < take {
			return *freeze, 0, bizerrors.New(bizerrors.CodeStateConflict, "credit lot frozen points are insufficient")
		}
		batch.FrozenPoints -= take
		if batch.ExpiresAt == nil || batch.ExpiresAt.After(now) {
			batch.RemainingPoints += take
			batch.AvailablePoints += take
			batch.UpdatedBy = optionalString(operatorID)
			batch.UpdatedAt = now
			if err := tx.Save(&batch).Error; err != nil {
				return *freeze, 0, err
			}
			account.AvailablePoints += take
		} else {
			batch.ExpiredPoints += take
			batch.UpdatedBy = optionalString(operatorID)
			batch.UpdatedAt = now
			if err := tx.Save(&batch).Error; err != nil {
				return *freeze, 0, err
			}
		}
		row.ReleasedPoints += take
		if row.ChargedPoints+row.ReleasedPoints >= row.FrozenPoints {
			row.Status = StatusReleased
		}
		row.UpdatedBy = optionalString(operatorID)
		row.UpdatedAt = now
		if err := tx.Save(&row).Error; err != nil {
			return *freeze, 0, err
		}
		account.FrozenPoints -= take
		released += take
		remaining -= take
	}
	if remaining > 0 {
		return *freeze, 0, bizerrors.New(bizerrors.CodeStateConflict, "release points exceed unsettled freeze")
	}
	return *freeze, released, nil
}

func (a *App) chargeToolItem(tx *gorm.DB, chargeID, estimateID string, item ChargeItemInput, operatorID string, now time.Time) (ChargedLineItemDTO, error) {
	var estimateItem businesscore.CreditEstimateItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("estimate_id = ? AND estimate_item_id = ?", estimateID, item.EstimateItemID).First(&estimateItem).Error; err != nil {
		return ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "estimate item not found")
	}
	if err := ensureEstimateItemUnsettled(tx, item.EstimateItemID); err != nil {
		return ChargedLineItemDTO{}, err
	}
	if estimateItem.Quantity != nil && item.ActualQuantity > *estimateItem.Quantity {
		return ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "actual quantity exceeds estimate")
	}
	charged := int64(0)
	status := "skipped"
	if item.ExecutionStatus == "success" {
		charged = estimateItem.EstimatePoints
		status = StatusCharged
	}
	row := businesscore.CreditToolChargeItem{
		ID: security.RandomID("ctci_"), ToolChargeID: chargeID, EstimateItemID: item.EstimateItemID,
		ToolCallID: item.ToolCallID, ToolName: item.ToolName, ToolType: item.ToolType, BillingUnit: item.BillingUnit,
		ActualQuantity: item.ActualQuantity, ChargedPoints: charged, ExecutionStatus: item.ExecutionStatus,
		Status: status, MetadataJSON: mustJSON(item.MetadataSummary), CreatedBy: optionalString(operatorID), UpdatedBy: optionalString(operatorID), CreatedAt: now,
	}
	if err := tx.Create(&row).Error; err != nil {
		return ChargedLineItemDTO{}, err
	}
	return ChargedLineItemDTO{EstimateItemID: item.EstimateItemID, ChargedPoints: charged, Status: status, ToolCallID: item.ToolCallID}, nil
}

func ensureEstimateItemUnsettled(tx *gorm.DB, estimateItemID string) error {
	var count int64
	if err := tx.Model(&businesscore.CreditToolChargeItem{}).Where("estimate_item_id = ?", estimateItemID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return bizerrors.New(bizerrors.CodeStateConflict, "estimate item already settled by tool charge")
	}
	if err := tx.Model(&businesscore.AssetCommitItem{}).Where("estimate_item_id = ?", estimateItemID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return bizerrors.New(bizerrors.CodeStateConflict, "estimate item already settled by asset commit")
	}
	return nil
}

func validateSafetyEvidence(evidence *businessagent.SafetyEvidenceDTO, expectedScene, expectedTargetType, expectedTraceID string, now time.Time) error {
	if evidence == nil || strings.TrimSpace(evidence.SafetyEvidenceId) == "" {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence is required")
	}
	if evidence.Result_ != "passed" || evidence.Scene != expectedScene || evidence.TargetType != expectedTargetType {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence is invalid")
	}
	if !strings.HasPrefix(evidence.EvaluatedObjectDigest, "sha256:") ||
		strings.TrimSpace(evidence.PolicyVersion) == "" || strings.TrimSpace(evidence.EvidenceVersion) == "" ||
		strings.TrimSpace(evidence.EvaluatedAt) == "" || strings.TrimSpace(evidence.TraceId) == "" {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence fields are incomplete")
	}
	if expectedTraceID != "" && evidence.TraceId != expectedTraceID {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence trace_id does not match request")
	}
	if _, err := time.Parse(time.RFC3339Nano, evidence.EvaluatedAt); err != nil {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence evaluated_at is invalid")
	}
	if evidence.ExpiresAt != nil && *evidence.ExpiresAt != "" {
		expires, err := time.Parse(time.RFC3339Nano, *evidence.ExpiresAt)
		if err != nil {
			return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence expires_at is invalid")
		}
		if !expires.After(now) {
			return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence is expired")
		}
	}
	return nil
}

func validateRedeemTarget(code businesscore.RedeemCode, batch businesscore.RedeemCodeBatch, account businesscore.CreditAccount, auth AuthContext, targetAccountType, redeemChannel string, now time.Time) error {
	if code.Status != "unused" {
		return bizerrors.New(bizerrors.CodeRedeemCodeUsed, "redeem code has been used")
	}
	if batch.Status != StatusActive {
		return bizerrors.New(bizerrors.CodeRedeemCodeInvalid, "redeem code batch is not active")
	}
	if code.ExpiresAt != nil && code.ExpiresAt.Before(now) {
		return bizerrors.New(bizerrors.CodeRedeemCodeExpired, "redeem code is expired")
	}
	if batch.ExpiresAt != nil && batch.ExpiresAt.Before(now) {
		return bizerrors.New(bizerrors.CodeRedeemCodeExpired, "redeem code batch is expired")
	}
	if account.AccountType != targetAccountType || redeemBatchAccountType(batch) != targetAccountType {
		return bizerrors.New(bizerrors.CodeRedeemCodeTargetMismatch, "redeem code account_type does not match")
	}
	if account.AccountType == "enterprise" && auth.EnterpriseRole != accountspace.RoleOwner {
		return bizerrors.New(bizerrors.CodePermissionDenied, "enterprise owner permission is required to redeem enterprise credits")
	}
	switch redeemBatchBindTargetType(batch) {
	case "none":
		return nil
	case "user":
		if redeemBatchBindTargetID(batch) != auth.UserID {
			return bizerrors.New(bizerrors.CodeRedeemCodeTargetMismatch, "redeem code target user does not match")
		}
	case "enterprise":
		if redeemBatchBindTargetID(batch) != auth.EnterpriseID {
			return bizerrors.New(bizerrors.CodeRedeemCodeTargetMismatch, "redeem code target enterprise does not match")
		}
	case "channel":
		if value(batch.ChannelCode) == "" || value(batch.ChannelCode) != strings.TrimSpace(redeemChannel) {
			return bizerrors.New(bizerrors.CodeRedeemCodeTargetMismatch, "redeem code channel does not match")
		}
	default:
		return bizerrors.New(bizerrors.CodeRedeemCodeInvalid, "redeem code bind target is invalid")
	}
	return nil
}

func (a *App) getFreezeDTO(ctx context.Context, freezeID string) (FreezeDTO, error) {
	var row businesscore.CreditFreeze
	if err := a.repo.DB().WithContext(ctx).Where("freeze_id = ?", freezeID).First(&row).Error; err != nil {
		return FreezeDTO{}, err
	}
	return FreezeDTO{FreezeID: row.FreezeID, FrozenPoints: row.FrozenPoints, ExpiresAt: row.ExpiresAt}, nil
}

func (a *App) getEstimateDTO(ctx context.Context, estimateID string) (EstimateDTO, error) {
	var row businesscore.CreditEstimate
	if err := a.repo.DB().WithContext(ctx).Where("estimate_id = ?", estimateID).First(&row).Error; err != nil {
		return EstimateDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "credit estimate not found")
	}
	var items []businesscore.CreditEstimateItem
	if err := a.repo.DB().WithContext(ctx).Where("estimate_id = ?", estimateID).Order("created_at ASC, id ASC").Find(&items).Error; err != nil {
		return EstimateDTO{}, err
	}
	lineItems := make([]EstimateLineItemDTO, 0, len(items))
	for _, item := range items {
		lineItems = append(lineItems, EstimateLineItemDTO{
			EstimateItemID:  item.EstimateItemID,
			ItemType:        item.ItemType,
			ToolName:        stringPtrValue(item.ToolName),
			ToolType:        stringPtrValue(item.ToolType),
			PricingPolicyID: stringPtrValue(item.PricingPolicyID),
			ModelID:         stringPtrValue(item.ModelID),
			ResourceType:    stringPtrValue(item.ResourceType),
			BillingUnit:     stringPtrValue(item.BillingUnit),
			Quantity:        floatPtrValue(item.Quantity),
			UnitPoints:      floatPtrValue(item.UnitPoints),
			EstimatePoints:  item.EstimatePoints,
			FreeReason:      stringPtrValue(item.FreeReason),
			Metadata:        estimateItemMetadata(item.MetadataJSON),
		})
	}
	return EstimateDTO{
		EstimateID: row.EstimateID, EstimatePoints: row.EstimatePoints, AvailablePoints: row.AvailablePoints,
		ExpiresSoonPoints: row.ExpiresSoonPoints, CreditAccountScope: row.AccountType, CreditAccountID: row.AccountID,
		LineItems: lineItems, ExpiresAt: row.ExpiresAt, Insufficient: row.Insufficient,
	}, nil
}

func (a *App) getToolChargeDTO(ctx context.Context, chargeID string) (ChargeToolDTO, error) {
	var row businesscore.CreditToolChargeBatch
	if err := a.repo.DB().WithContext(ctx).Where("tool_charge_id = ?", chargeID).First(&row).Error; err != nil {
		return ChargeToolDTO{}, err
	}
	return ChargeToolDTO{ToolChargeID: row.ToolChargeID, ChargedPoints: row.ChargedPoints, ReleasedPoints: row.ReleasedPoints, FreezeStatus: row.Status}, nil
}

func (a *App) getRedemptionDTO(ctx context.Context, redemptionID string) (RedeemDTO, error) {
	var row businesscore.RedeemCodeRedemption
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", redemptionID).First(&row).Error; err != nil {
		return RedeemDTO{}, err
	}
	return RedeemDTO{AccountID: row.AccountID, RedeemedPoints: row.Points, RedemptionID: row.ID}, nil
}

func (a *App) getRechargeOrderDTO(ctx context.Context, orderID string) (RechargeOrderDTO, error) {
	var row businesscore.RechargeOrder
	if err := a.repo.DB().WithContext(ctx).Where("order_id = ?", orderID).First(&row).Error; err != nil {
		return RechargeOrderDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "recharge order not found")
	}
	return rechargeOrderDTO(row), nil
}

func (a *App) getBillingPackageDTO(ctx context.Context, packageID string) (RechargePackageDTO, error) {
	var row businesscore.RechargePackage
	if err := a.repo.DB().WithContext(ctx).Where("package_id = ?", packageID).First(&row).Error; err != nil {
		return RechargePackageDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "billing package not found")
	}
	return rechargePackageDTO(row), nil
}

func rechargeOrderDTO(row businesscore.RechargeOrder) RechargeOrderDTO {
	return RechargeOrderDTO{
		OrderID: row.OrderID, AccountID: row.AccountID, EnterpriseID: value(row.EnterpriseID), PackageID: row.PackageID, SKUID: value(row.SKUID),
		PackageType: row.PackageType, TargetScope: row.TargetScope, BillingMode: row.BillingMode,
		Points: row.Points, GrantedPoints: row.GrantedPoints, BonusPoints: row.BonusPoints,
		PriceCents: packageOrderPriceAmount(row), PriceAmount: packageOrderPriceAmount(row), Currency: row.Currency,
		PaymentProvider: row.PaymentProvider, PaymentStatus: row.PaymentStatus, CreditLotID: value(row.CreditLotID),
		EntitlementSnapshotID: value(row.EntitlementSnapshotID), CreatedAt: row.CreatedAt, PaidAt: row.PaidAt,
	}
}

func rechargePackageExpiresAt(now time.Time, duration string) (time.Time, error) {
	switch strings.TrimSpace(duration) {
	case "P7D":
		return now.AddDate(0, 0, 7), nil
	case "P1M":
		return now.AddDate(0, 1, 0), nil
	case "P1Y":
		return now.AddDate(1, 0, 0), nil
	default:
		return time.Time{}, bizerrors.New(bizerrors.CodeInvalidArgument, "unsupported recharge package duration")
	}
}

func packageCreditExpiry(now time.Time, policy string) (*time.Time, datatypes.JSON, error) {
	policy = defaultString(policy, "P1M")
	if policy == "never_expire" {
		return nil, mustJSON(map[string]any{"type": "never_expire"}), nil
	}
	expiresAt, err := rechargePackageExpiresAt(now, policy)
	if err != nil {
		return nil, nil, err
	}
	return &expiresAt, rechargeExpiryPolicyJSON(policy), nil
}

func rechargeExpiryPolicyJSON(duration string) datatypes.JSON {
	return mustJSON(map[string]any{"type": "relative_duration", "duration": strings.TrimSpace(duration)})
}

func packageTotalPoints(row businesscore.RechargePackage) int64 {
	if row.GrantedPoints+row.BonusPoints > 0 {
		return row.GrantedPoints + row.BonusPoints
	}
	return row.Points
}

func packagePriceAmount(row businesscore.RechargePackage) int64 {
	if row.PriceAmount > 0 {
		return row.PriceAmount
	}
	return row.PriceCents
}

func packageOrderPriceAmount(row businesscore.RechargeOrder) int64 {
	if row.PriceAmount > 0 {
		return row.PriceAmount
	}
	return row.PriceCents
}

func packageSpendScopeJSON(row businesscore.RechargePackage) datatypes.JSON {
	if len(row.SpendScopeJSON) > 0 {
		return row.SpendScopeJSON
	}
	return defaultSpendScopeJSON()
}

func packageEntitlementPolicyJSON(row businesscore.RechargePackage) datatypes.JSON {
	if len(row.EntitlementPolicy) > 0 {
		return row.EntitlementPolicy
	}
	return datatypes.JSON([]byte(`{}`))
}

func (a *App) createEnterpriseContractForPackage(tx *gorm.DB, account businesscore.CreditAccount, pkg businesscore.RechargePackage, order businesscore.RechargeOrder, now time.Time, operatorID string) error {
	enterpriseID := value(account.EnterpriseID)
	if enterpriseID == "" {
		return bizerrors.New(bizerrors.CodePermissionDenied, "enterprise account is required for enterprise package")
	}
	expiresAt, _, err := packageCreditExpiry(now, pkg.CreditExpiryPolicy)
	if err != nil {
		return err
	}
	policy := jsonObject(pkg.EntitlementPolicy)
	contract := businesscore.EnterpriseContract{
		ID: security.RandomID("ect_"), ContractID: security.RandomID("contract_"), EnterpriseID: enterpriseID,
		PackageID: pkg.PackageID, OrderID: &order.OrderID, ContractStatus: StatusActive, BillingMode: defaultString(pkg.BillingMode, "subscription"),
		PeriodStart: now, PeriodEnd: expiresAt, SeatQuota: intValue(policy["seat_quota"]), BudgetPoints: order.Points,
		ApprovalPolicyJSON: mustJSON(map[string]any{
			"department_budget":         policy["department_budget"],
			"approval_threshold_points": policy["approval_threshold_points"],
		}),
		InvoicePolicyJSON: mustJSON(map[string]any{
			"invoice":       policy["invoice"],
			"billing_mode":  pkg.BillingMode,
			"package_type":  pkg.PackageType,
			"payment_order": order.OrderID,
		}),
		CreatedBy: optionalString(operatorID), UpdatedBy: optionalString(operatorID), CreatedAt: now, UpdatedAt: now,
	}
	return tx.Create(&contract).Error
}

func generationQuantity(resourceType string, quantity, duration int32) float64 {
	switch resourceType {
	case "video":
		if duration > 0 {
			return float64(duration)
		}
	case "music":
		if quantity > 0 {
			return float64(quantity)
		}
	default:
		if quantity > 0 {
			return float64(quantity)
		}
	}
	return 1
}

func estimatePoints(quantity, unitPoints float64, minPoints int64) int64 {
	points := int64(math.Ceil(quantity * unitPoints))
	if minPoints > 0 && points < minPoints {
		points = minPoints
	}
	return points
}

func creditExpiryPolicyJSON(expiresAt *time.Time) datatypes.JSON {
	if expiresAt == nil {
		return mustJSON(map[string]any{"type": "never_expire"})
	}
	return mustJSON(map[string]any{
		"type":       "fixed_date",
		"expires_at": expiresAt.UTC().Format(time.RFC3339),
	})
}

func defaultSpendScopeJSON() datatypes.JSON {
	return mustJSON([]string{"tool_generation", "skill_usage"})
}

func (a *App) beginAdminMaintenance(ctx context.Context, auth admin.AdminAuth, meta RequestMeta, scope, hash string) (idempotency.Decision, error) {
	return a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "admin:" + auth.AdminID, Scope: scope, IdempotencyKey: meta.IdempotencyKey, RequestHash: hash, ActorUserID: auth.AdminID,
	})
}

func maintenanceReplay(decision idempotency.Decision, action string) (CreditMaintenanceDTO, bool, error) {
	if decision.Mode == idempotency.DecisionConflict {
		return CreditMaintenanceDTO{}, true, bizerrors.New(bizerrors.CodeIdempotencyConflict, action+" idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return CreditMaintenanceDTO{}, true, bizerrors.New(bizerrors.CodeProcessing, action+" request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return CreditMaintenanceDTO{OperationID: decision.ReplayResult.ID, Status: "replayed"}, true, nil
	}
	return CreditMaintenanceDTO{}, false, nil
}

func (a *App) applyRefundCreditsTx(tx *gorm.DB, accountID, originalLotID string, points int64, operationID, entryType, sourceType, sourceID, operatorID, traceID, idempotencyKey, reason string, gracePeriodDays int) (CreditMaintenanceDTO, error) {
	if points <= 0 {
		return CreditMaintenanceDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "positive refund points are required")
	}
	var account businesscore.CreditAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", strings.TrimSpace(accountID)).First(&account).Error; err != nil {
		return CreditMaintenanceDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "credit account not found")
	}
	now := a.now()
	var lotID string
	var original businesscore.CreditBatch
	useOriginalLot := false
	if strings.TrimSpace(originalLotID) != "" {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND account_id = ?", strings.TrimSpace(originalLotID), account.ID).
			First(&original).Error
		if err != nil {
			return CreditMaintenanceDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "original credit lot not found")
		}
		useOriginalLot = original.ExpiresAt == nil || original.ExpiresAt.After(now)
	}
	if useOriginalLot {
		original.AvailablePoints += points
		original.RemainingPoints += points
		if original.ConsumedPoints >= points {
			original.ConsumedPoints -= points
		} else {
			original.ConsumedPoints = 0
		}
		original.Status = StatusActive
		original.UpdatedBy = optionalString(operatorID)
		original.UpdatedAt = now
		if err := tx.Save(&original).Error; err != nil {
			return CreditMaintenanceDTO{}, err
		}
		lotID = original.ID
	} else {
		if gracePeriodDays <= 0 {
			gracePeriodDays = 7
		}
		expiresAt := now.AddDate(0, 0, gracePeriodDays)
		source := defaultString(sourceID, operationID)
		spendScope := defaultSpendScopeJSON()
		settlementEligible := false
		if original.ID != "" {
			spendScope = original.SpendScopeJSON
			settlementEligible = original.SettlementEligible
			if source == operationID {
				source = original.ID
			}
		}
		refundLot := businesscore.CreditBatch{
			ID: security.RandomID("cb_"), AccountID: account.ID, BatchType: "refund", SourceType: "refund", SourceID: optionalString(source),
			TotalPoints: points, RemainingPoints: points, OriginalPoints: points, AvailablePoints: points, GrantedAt: now,
			ExpiresAt: &expiresAt, ExpiryPolicyJSON: rechargeExpiryPolicyJSON(fmt.Sprintf("P%dD", gracePeriodDays)),
			SpendScopeJSON: spendScope, SettlementEligible: settlementEligible, Status: StatusActive,
			CreatedBy: optionalString(operatorID), UpdatedBy: optionalString(operatorID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&refundLot).Error; err != nil {
			return CreditMaintenanceDTO{}, err
		}
		lotID = refundLot.ID
	}
	account.AvailablePoints += points
	account.UpdatedBy = optionalString(operatorID)
	account.UpdatedAt = now
	if err := tx.Save(&account).Error; err != nil {
		return CreditMaintenanceDTO{}, err
	}
	entry := ledger(account, entryType, points, sourceType, defaultString(sourceID, operationID), "", "", traceID, idempotencyKey)
	entry.BatchID = optionalString(lotID)
	entry.MetadataJSON = mustJSON(map[string]any{"operation_id": operationID, "reason": reason, "original_lot_id": originalLotID})
	if err := tx.Create(entry).Error; err != nil {
		return CreditMaintenanceDTO{}, err
	}
	return CreditMaintenanceDTO{
		OperationID: operationID, Status: defaultString(entryType, "refunded"), AccountID: account.ID, LotID: lotID,
		LedgerEntryID: entry.ID, AffectedLots: 1, Points: points, AvailablePoints: account.AvailablePoints, FrozenPoints: account.FrozenPoints,
	}, nil
}

func ledger(account businesscore.CreditAccount, entryType string, delta int64, sourceType, sourceID, projectID, runID, traceID, idempotencyKey string) *businesscore.CreditLedgerEntry {
	return &businesscore.CreditLedgerEntry{
		ID: security.RandomID("cled_"), AccountID: account.ID, EntryType: entryType, PointsDelta: delta,
		BalanceAfter: account.AvailablePoints, FrozenAfter: account.FrozenPoints, SourceType: sourceType, SourceID: sourceID,
		ProjectID: optionalString(projectID), RunID: optionalString(runID), TraceID: optionalString(traceID), IdempotencyKey: optionalString(idempotencyKey),
		MetadataJSON: datatypes.JSON([]byte(`{}`)), CreatedAt: time.Now().UTC(),
	}
}

func (a *App) writeBillingAuditTx(tx *gorm.DB, adminID, traceID, action, resourceType, resourceID, beforeStatus, afterStatus, reason string) error {
	if a.audit == nil {
		return nil
	}
	return tx.Create(&auditlog.AuditRecord{
		AuditID:        security.RandomID("audit_"),
		TraceID:        traceID,
		OperatorType:   "admin",
		OperatorID:     optionalString(adminID),
		TenantID:       "admin:" + adminID,
		BusinessAction: action,
		ResourceType:   resourceType,
		ResourceID:     optionalString(resourceID),
		BeforeStatus:   optionalString(beforeStatus),
		AfterStatus:    optionalString(afterStatus),
		Reason:         optionalString(reason),
		Result:         "success",
		MetadataSummary: mustJSON(map[string]any{
			"resource_id": resourceID,
		}),
		CreatedAt: a.now(),
	}).Error
}

func requestHash(meta RequestMeta, auth AuthContext, extra map[string]any) string {
	if meta.RequestHash != "" {
		return meta.RequestHash
	}
	data, _ := json.Marshal(map[string]any{
		"space_id": auth.SpaceID, "actor_user_id": auth.UserID, "enterprise_id": auth.EnterpriseID, "extra": extra,
	})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func estimateItemMetadata(raw datatypes.JSON) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var data struct {
		MetadataSummary map[string]string `json:"metadata_summary"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return data.MetadataSummary
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func floatPtrValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func safetyDigest(evidence *businessagent.SafetyEvidenceDTO) string {
	if evidence == nil {
		return ""
	}
	data, _ := json.Marshal(evidence)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func codeDigest(code string) string {
	return security.HashOpaque(strings.TrimSpace(code))
}

func possibleCodeDigests(code string) []string {
	code = strings.TrimSpace(code)
	return []string{security.HashOpaque(code), "sha256:" + code}
}

func normalizePage(limit, offset, max int) (int, int) {
	if limit <= 0 {
		limit = 10
	}
	if limit > max {
		limit = max
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func mustJSON(value any) datatypes.JSON {
	data, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte(`{}`))
	}
	return datatypes.JSON(data)
}

func defaultMap(value map[string]any, fallback map[string]any) map[string]any {
	if len(value) > 0 {
		return value
	}
	return fallback
}

func spendScopeJSON(scopes []string) datatypes.JSON {
	cleaned := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if strings.TrimSpace(scope) != "" {
			cleaned = append(cleaned, strings.TrimSpace(scope))
		}
	}
	if len(cleaned) == 0 {
		return defaultSpendScopeJSON()
	}
	return mustJSON(cleaned)
}

func jsonObject(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return map[string]any{}
	}
	return data
}

func jsonStringSlice(raw datatypes.JSON) []string {
	if len(raw) == 0 {
		return nil
	}
	var data []string
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return data
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalFloat(value float64) *float64 {
	if value == 0 {
		return nil
	}
	return &value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func valueStatus(value string) string {
	return strings.TrimSpace(value)
}

func errorCode(err error) string {
	if biz := bizerrors.FromError(err); biz != nil {
		return string(biz.Code)
	}
	return "INTERNAL_ERROR"
}

func targetPtr(actual, expected, targetID string) *string {
	if actual == expected && strings.TrimSpace(targetID) != "" {
		return &targetID
	}
	return nil
}

func normalizeAccountType(accountType string) string {
	switch strings.TrimSpace(accountType) {
	case "personal", "enterprise":
		return strings.TrimSpace(accountType)
	default:
		return ""
	}
}

func normalizeBindTargetType(bindTargetType string) string {
	switch strings.TrimSpace(bindTargetType) {
	case "", "none":
		return "none"
	case "user", "enterprise", "channel":
		return strings.TrimSpace(bindTargetType)
	default:
		return ""
	}
}

func redeemBindTargetMatchesAccount(accountType, bindTargetType string) bool {
	switch bindTargetType {
	case "user":
		return accountType == "personal"
	case "enterprise":
		return accountType == "enterprise"
	case "none", "channel":
		return accountType == "personal" || accountType == "enterprise"
	default:
		return false
	}
}

func redeemBatchAccountType(row businesscore.RedeemCodeBatch) string {
	if normalized := normalizeAccountType(row.AccountType); normalized != "" {
		return normalized
	}
	if row.TargetType == "enterprise" || row.TargetEnterpriseID != nil {
		return "enterprise"
	}
	return "personal"
}

func redeemBatchBindTargetType(row businesscore.RedeemCodeBatch) string {
	if normalized := normalizeBindTargetType(row.BindTargetType); normalized != "" {
		return normalized
	}
	switch row.TargetType {
	case "user", "personal_user":
		return "user"
	case "enterprise":
		return "enterprise"
	case "none":
		return "none"
	default:
		if row.ChannelCode != nil && *row.ChannelCode != "" {
			return "channel"
		}
		return "none"
	}
}

func redeemBatchBindTargetID(row businesscore.RedeemCodeBatch) string {
	if row.BindTargetID != nil && *row.BindTargetID != "" {
		return *row.BindTargetID
	}
	switch redeemBatchBindTargetType(row) {
	case "user":
		return value(row.TargetUserID)
	case "enterprise":
		return value(row.TargetEnterpriseID)
	default:
		return ""
	}
}

func redeemBatchDTO(row businesscore.RedeemCodeBatch) RedeemCodeDTO {
	return RedeemCodeDTO{
		BatchID: row.ID, BatchNo: row.BatchNo, AccountType: redeemBatchAccountType(row), BindTargetType: redeemBatchBindTargetType(row),
		BindTargetID: redeemBatchBindTargetID(row), Channel: value(row.ChannelCode), TargetType: row.TargetType, TotalCodes: row.TotalCodes,
		PointsPerCode: row.PointsPerCode, ExpiresAt: row.ExpiresAt, CreditExpiresAt: row.CreditExpiresAt, Status: row.Status,
	}
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func stringMap(in map[string]string) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (a *App) DB() *gorm.DB {
	return a.repo.DB()
}

func ErrAlreadySettled(estimateItemID string) error {
	return fmt.Errorf("%s: %w", estimateItemID, bizerrors.New(bizerrors.CodeStateConflict, "estimate item already settled"))
}
