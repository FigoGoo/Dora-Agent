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
		take := batch.RemainingPoints
		if take > remaining {
			take = remaining
		}
		batch.RemainingPoints -= take
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
		if batch.ExpiresAt == nil || batch.ExpiresAt.After(now) {
			batch.RemainingPoints += take
			batch.UpdatedBy = optionalString(operatorID)
			batch.UpdatedAt = now
			if err := tx.Save(&batch).Error; err != nil {
				return *freeze, 0, err
			}
			account.AvailablePoints += take
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

func ledger(account businesscore.CreditAccount, entryType string, delta int64, sourceType, sourceID, projectID, runID, traceID, idempotencyKey string) *businesscore.CreditLedgerEntry {
	return &businesscore.CreditLedgerEntry{
		ID: security.RandomID("cled_"), AccountID: account.ID, EntryType: entryType, PointsDelta: delta,
		BalanceAfter: account.AvailablePoints, FrozenAfter: account.FrozenPoints, SourceType: sourceType, SourceID: sourceID,
		ProjectID: optionalString(projectID), RunID: optionalString(runID), TraceID: optionalString(traceID), IdempotencyKey: optionalString(idempotencyKey),
		MetadataJSON: datatypes.JSON([]byte(`{}`)), CreatedAt: time.Now().UTC(),
	}
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
