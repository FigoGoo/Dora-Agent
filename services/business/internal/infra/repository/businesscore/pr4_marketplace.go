package businesscore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr4"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrMarketplaceListingSuspended = errors.New("MARKETPLACE_LISTING_SUSPENDED")

func (r *Repository) SaveCreatorPublishFlowV1(ctx context.Context, pkg pr4.SkillPackage, version pr4.SkillVersion, policy pr4.SkillPricingPolicy, listing pr4.MarketplaceListing) error {
	if err := pr4.ValidateCreatorPublishFlow(pkg, version, policy, listing); err != nil {
		return fmt.Errorf("creator_publish_flow: %w", err)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(skillPackageRecord(pkg)).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(skillVersionRecord(version)).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(skillPricingPolicyRecord(policy)).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(marketplaceListingRecord(listing)).Error; err != nil {
			return err
		}
		return nil
	})
}

func (r *Repository) SuspendMarketplaceListingV1(ctx context.Context, listingID string, suspendedAt time.Time) (pr4.MarketplaceListing, error) {
	if strings.TrimSpace(listingID) == "" {
		return pr4.MarketplaceListing{}, errors.New("listing_id is required")
	}
	if suspendedAt.IsZero() {
		suspendedAt = time.Now().UTC()
	}
	var suspended pr4.MarketplaceListing
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record PR4MarketplaceListingRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("listing_id = ?", listingID).First(&record).Error; err != nil {
			return err
		}
		if record.Status != "suspended" {
			updates := map[string]any{
				"status":     "suspended",
				"updated_at": suspendedAt.UTC(),
			}
			if err := tx.Model(&PR4MarketplaceListingRecord{}).Where("listing_id = ?", listingID).Updates(updates).Error; err != nil {
				return err
			}
			record.Status = "suspended"
			record.UpdatedAt = suspendedAt.UTC()
		}
		next, err := marketplaceListingContract(record)
		if err != nil {
			return err
		}
		suspended = next
		return nil
	})
	if err != nil {
		return pr4.MarketplaceListing{}, err
	}
	return suspended, nil
}

func (r *Repository) EnsureMarketplaceListingInstallableV1(ctx context.Context, listingID string) error {
	if strings.TrimSpace(listingID) == "" {
		return errors.New("listing_id is required")
	}
	var record PR4MarketplaceListingRecord
	if err := r.db.WithContext(ctx).Where("listing_id = ?", listingID).First(&record).Error; err != nil {
		return err
	}
	if record.Status == "suspended" {
		return ErrMarketplaceListingSuspended
	}
	if record.Status != "listed" {
		return fmt.Errorf("marketplace listing %s is %s", listingID, record.Status)
	}
	return nil
}

func (r *Repository) InstallPersonalLatestSkillV1(ctx context.Context, request pr4.InstallSkillRequest, installation pr4.SkillInstallation) (pr4.SkillInstallation, error) {
	if err := pr4.ValidatePersonalLatestInstall(request, installation); err != nil {
		return pr4.SkillInstallation{}, fmt.Errorf("personal_latest_install: %w", err)
	}
	var existing PR4SkillInstallationRecord
	err := r.db.WithContext(ctx).Where("idempotency_key = ?", request.IdempotencyKey).First(&existing).Error
	if err == nil {
		next, err := skillInstallationContract(existing)
		if err != nil {
			return pr4.SkillInstallation{}, err
		}
		if !sameSkillInstallationIdentity(installation, next) {
			return pr4.SkillInstallation{}, errors.New("idempotent skill installation replay does not match request")
		}
		return next, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return pr4.SkillInstallation{}, err
	}
	if err := r.EnsureMarketplaceListingInstallableV1(ctx, request.ListingID); err != nil {
		return pr4.SkillInstallation{}, err
	}
	return r.saveSkillInstallationV1(ctx, installation, request.IdempotencyKey)
}

func (r *Repository) SaveSkillInstallationSnapshotV1(ctx context.Context, installation pr4.SkillInstallation, idempotencyKey string) (pr4.SkillInstallation, error) {
	if err := pr4.ValidateSkillInstallation(installation); err != nil {
		return pr4.SkillInstallation{}, fmt.Errorf("skill_installation: %w", err)
	}
	return r.saveSkillInstallationV1(ctx, installation, idempotencyKey)
}

func (r *Repository) UpgradeSkillInstallationV1(ctx context.Context, request pr4.UpgradeSkillInstallationRequest, after pr4.SkillInstallation, rule pr4.HistoricalRunRule) (pr4.SkillInstallation, error) {
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		return pr4.SkillInstallation{}, errors.New("idempotency_key is required")
	}
	var upgraded pr4.SkillInstallation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record PR4SkillInstallationRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("installation_id = ?", request.InstallationID).First(&record).Error; err != nil {
			return err
		}
		initial, err := skillInstallationContract(record)
		if err != nil {
			return err
		}
		if initial.InstalledVersion == request.TargetVersion && initial.UpgradeStatus == "confirmed" && record.IdempotencyKey == request.IdempotencyKey {
			upgraded = initial
			return nil
		}
		if err := pr4.ValidateEnterprisePinnedUpgrade(initial, request, after, rule); err != nil {
			return fmt.Errorf("enterprise_pinned_upgrade: %w", err)
		}
		updates := map[string]any{
			"installed_version": after.InstalledVersion,
			"status":            after.Status,
			"upgrade_status":    after.UpgradeStatus,
			"idempotency_key":   request.IdempotencyKey,
			"updated_at":        after.UpdatedAt,
		}
		if err := tx.Model(&PR4SkillInstallationRecord{}).Where("installation_id = ?", after.InstallationID).Updates(updates).Error; err != nil {
			return err
		}
		upgraded = after
		return nil
	})
	if err != nil {
		return pr4.SkillInstallation{}, err
	}
	return upgraded, nil
}

func (r *Repository) CreateSkillUsageRecordV1(ctx context.Context, usage pr4.SkillUsageRecord, idempotencyKey string) (pr4.SkillUsageRecord, error) {
	if err := validatePrecreatedUsage(usage, idempotencyKey); err != nil {
		return pr4.SkillUsageRecord{}, err
	}
	record := skillUsageRecord(usage, idempotencyKey)
	var created pr4.SkillUsageRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "idempotency_key"}},
			DoNothing: true,
		}).Create(&record)
		if result.Error != nil {
			return result.Error
		}
		stored := record
		if result.RowsAffected == 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("idempotency_key = ?", idempotencyKey).First(&stored).Error; err != nil {
				return err
			}
		}
		next, err := skillUsageContract(stored)
		if err != nil {
			return err
		}
		if !sameSkillUsageIdentity(usage, next) {
			return errors.New("idempotent skill usage replay does not match request")
		}
		created = next
		return nil
	})
	if err != nil {
		return pr4.SkillUsageRecord{}, err
	}
	return created, nil
}

func (r *Repository) FreezeSkillUsageRecordV1(ctx context.Context, usageID string, skillUsageDigest string, creditHoldID string, frozenAt time.Time) (pr4.SkillUsageRecord, error) {
	if strings.TrimSpace(usageID) == "" || strings.TrimSpace(skillUsageDigest) == "" || strings.TrimSpace(creditHoldID) == "" {
		return pr4.SkillUsageRecord{}, errors.New("usage_id, skill_usage_digest and credit_hold_id are required")
	}
	if frozenAt.IsZero() {
		frozenAt = time.Now().UTC()
	}
	var frozen pr4.SkillUsageRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var usageRecord PR4SkillUsageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("usage_id = ?", usageID).First(&usageRecord).Error; err != nil {
			return err
		}
		current, err := skillUsageContract(usageRecord)
		if err != nil {
			return err
		}
		if current.SkillUsageDigest != skillUsageDigest {
			return errors.New("skill usage digest does not match usage record")
		}
		if current.UsageStatus == "running" && current.ChargeStatus == "frozen" {
			if current.CreditHoldID == nil || *current.CreditHoldID != creditHoldID {
				return errors.New("idempotent skill usage freeze replay does not match credit hold")
			}
			frozen = current
			return nil
		}
		if current.UsageStatus != "confirmation_required" || current.ChargeStatus != "not_frozen" || current.RefundStatus != "none" || current.CreditHoldID != nil {
			return errors.New("skill usage must be precreated before freeze")
		}
		next := current
		next.UsageStatus = "running"
		next.ChargeStatus = "frozen"
		next.CreditHoldID = &creditHoldID
		next.UpdatedAt = frozenAt.UTC()
		if err := pr4.ValidateSkillUsageRecord(next); err != nil {
			return fmt.Errorf("usage_after_freeze: %w", err)
		}
		updates := map[string]any{
			"usage_status":   next.UsageStatus,
			"charge_status":  next.ChargeStatus,
			"credit_hold_id": next.CreditHoldID,
			"updated_at":     next.UpdatedAt,
		}
		if err := tx.Model(&PR4SkillUsageRecord{}).Where("usage_id = ?", usageID).Updates(updates).Error; err != nil {
			return err
		}
		frozen = next
		return nil
	})
	if err != nil {
		return pr4.SkillUsageRecord{}, err
	}
	return frozen, nil
}

func (r *Repository) ReleaseSkillUsageFreezeV1(ctx context.Context, usageID string, releaseReason string, releasedAt time.Time) (pr4.SkillUsageRecord, error) {
	if strings.TrimSpace(usageID) == "" || strings.TrimSpace(releaseReason) == "" {
		return pr4.SkillUsageRecord{}, errors.New("usage_id and release_reason are required")
	}
	if releasedAt.IsZero() {
		releasedAt = time.Now().UTC()
	}
	var released pr4.SkillUsageRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var usageRecord PR4SkillUsageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("usage_id = ?", usageID).First(&usageRecord).Error; err != nil {
			return err
		}
		current, err := skillUsageContract(usageRecord)
		if err != nil {
			return err
		}
		if current.UsageStatus == "released" && current.ChargeStatus == "released" {
			released = current
			return nil
		}
		if current.UsageStatus != "running" || current.ChargeStatus != "frozen" {
			return errors.New("skill usage freeze can only be released from running/frozen")
		}
		next := current
		next.UsageStatus = "released"
		next.ChargeStatus = "released"
		next.UpdatedAt = releasedAt.UTC()
		if err := pr4.ValidateSkillUsageRecord(next); err != nil {
			return fmt.Errorf("usage_after_release: %w", err)
		}
		updates := map[string]any{
			"usage_status":  next.UsageStatus,
			"charge_status": next.ChargeStatus,
			"updated_at":    next.UpdatedAt,
		}
		if err := tx.Model(&PR4SkillUsageRecord{}).Where("usage_id = ?", usageID).Updates(updates).Error; err != nil {
			return err
		}
		released = next
		return nil
	})
	if err != nil {
		return pr4.SkillUsageRecord{}, err
	}
	return released, nil
}

func (r *Repository) CommitSkillUsageAndSettleV1(ctx context.Context, afterCharge pr4.SkillUsageRecord, settlement pr4.SkillSettlement) (pr4.SkillUsageRecord, pr4.SkillSettlement, error) {
	var committed pr4.SkillUsageRecord
	var settled pr4.SkillSettlement
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var usageRecord PR4SkillUsageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("usage_id = ?", afterCharge.UsageID).First(&usageRecord).Error; err != nil {
			return err
		}
		current, err := skillUsageContract(usageRecord)
		if err != nil {
			return err
		}
		if current.UsageStatus == "value_delivered" && current.ChargeStatus == "charged" {
			committed = current
			storedSettlement, err := r.getSkillSettlementByUsageTx(tx, current.UsageID)
			if err != nil {
				return err
			}
			settled = storedSettlement
			return nil
		}
		if current.UsageStatus == "running" && current.ChargeStatus == "frozen" {
			if err := validateFrozenSkillUsageCharge(current, afterCharge, settlement); err != nil {
				return err
			}
		} else {
			if err := pr4.ValidateSkillUsagePrecreateConfirmCharge(pr4.SkillUsageChargeSequence, current, afterCharge, settlement); err != nil {
				return err
			}
		}
		updates := map[string]any{
			"usage_status":       afterCharge.UsageStatus,
			"charge_status":      afterCharge.ChargeStatus,
			"refund_status":      afterCharge.RefundStatus,
			"settlement_status":  afterCharge.SettlementStatus,
			"credit_hold_id":     afterCharge.CreditHoldID,
			"value_delivered_at": afterCharge.ValueDeliveredAt,
			"updated_at":         afterCharge.UpdatedAt,
		}
		if err := tx.Model(&PR4SkillUsageRecord{}).Where("usage_id = ?", afterCharge.UsageID).Updates(updates).Error; err != nil {
			return err
		}
		settlementRecord := skillSettlementRecord(settlement)
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&settlementRecord).Error; err != nil {
			return err
		}
		committed = afterCharge
		settled = settlement
		return nil
	})
	if err != nil {
		return pr4.SkillUsageRecord{}, pr4.SkillSettlement{}, err
	}
	return committed, settled, nil
}

func (r *Repository) MarkSkillUsageRefundPendingV1(ctx context.Context, beforeRefund pr4.SkillUsageRecord) (pr4.SkillUsageRecord, error) {
	if err := pr4.ValidateSkillUsageRecord(beforeRefund); err != nil {
		return pr4.SkillUsageRecord{}, fmt.Errorf("usage_before_refund: %w", err)
	}
	var marked pr4.SkillUsageRecord
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record PR4SkillUsageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("usage_id = ?", beforeRefund.UsageID).First(&record).Error; err != nil {
			return err
		}
		current, err := skillUsageContract(record)
		if err != nil {
			return err
		}
		if current.UsageStatus == "refund_pending" && current.RefundStatus == "refund_requested" {
			marked = current
			return nil
		}
		if current.UsageStatus != "value_delivered" || current.ChargeStatus != "charged" || !sameSkillUsageIdentity(current, beforeRefund) {
			return errors.New("refund request requires charged usage with stable identity")
		}
		updates := map[string]any{
			"usage_status":      beforeRefund.UsageStatus,
			"charge_status":     beforeRefund.ChargeStatus,
			"refund_status":     beforeRefund.RefundStatus,
			"settlement_status": beforeRefund.SettlementStatus,
			"credit_hold_id":    beforeRefund.CreditHoldID,
			"updated_at":        beforeRefund.UpdatedAt,
		}
		if err := tx.Model(&PR4SkillUsageRecord{}).Where("usage_id = ?", beforeRefund.UsageID).Updates(updates).Error; err != nil {
			return err
		}
		marked = beforeRefund
		return nil
	})
	if err != nil {
		return pr4.SkillUsageRecord{}, err
	}
	return marked, nil
}

func (r *Repository) ReverseSkillUsageRefundV1(ctx context.Context, afterRefund pr4.SkillUsageRecord, settlementAfterReverse pr4.SkillSettlement) (pr4.SkillUsageRecord, pr4.SkillSettlement, error) {
	var refunded pr4.SkillUsageRecord
	var reversed pr4.SkillSettlement
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var usageRecord PR4SkillUsageRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("usage_id = ?", afterRefund.UsageID).First(&usageRecord).Error; err != nil {
			return err
		}
		before, err := skillUsageContract(usageRecord)
		if err != nil {
			return err
		}
		if before.UsageStatus == "refunded" && before.RefundStatus == "refund_reversed" {
			refunded = before
			storedSettlement, err := r.getSkillSettlementByUsageTx(tx, before.UsageID)
			if err != nil {
				return err
			}
			reversed = storedSettlement
			return nil
		}
		if err := pr4.ValidateSkillUsageRefundReversal(before, afterRefund, settlementAfterReverse); err != nil {
			return err
		}
		usageUpdates := map[string]any{
			"usage_status":      afterRefund.UsageStatus,
			"charge_status":     afterRefund.ChargeStatus,
			"refund_status":     afterRefund.RefundStatus,
			"settlement_status": afterRefund.SettlementStatus,
			"updated_at":        afterRefund.UpdatedAt,
		}
		if err := tx.Model(&PR4SkillUsageRecord{}).Where("usage_id = ?", afterRefund.UsageID).Updates(usageUpdates).Error; err != nil {
			return err
		}
		settlementUpdates := map[string]any{
			"status":     settlementAfterReverse.Status,
			"updated_at": settlementAfterReverse.UpdatedAt,
		}
		if err := tx.Model(&PR4SkillSettlementRecord{}).Where("settlement_id = ?", settlementAfterReverse.SettlementID).Updates(settlementUpdates).Error; err != nil {
			return err
		}
		refunded = afterRefund
		reversed = settlementAfterReverse
		return nil
	})
	if err != nil {
		return pr4.SkillUsageRecord{}, pr4.SkillSettlement{}, err
	}
	return refunded, reversed, nil
}

func (r *Repository) saveSkillInstallationV1(ctx context.Context, installation pr4.SkillInstallation, idempotencyKey string) (pr4.SkillInstallation, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return pr4.SkillInstallation{}, errors.New("idempotency_key is required")
	}
	record := skillInstallationRecord(installation, idempotencyKey)
	var saved pr4.SkillInstallation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "idempotency_key"}},
			DoNothing: true,
		}).Create(&record)
		if result.Error != nil {
			return result.Error
		}
		stored := record
		if result.RowsAffected == 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("idempotency_key = ?", idempotencyKey).First(&stored).Error; err != nil {
				return err
			}
		}
		next, err := skillInstallationContract(stored)
		if err != nil {
			return err
		}
		if !sameSkillInstallationIdentity(installation, next) {
			return errors.New("idempotent skill installation replay does not match request")
		}
		saved = next
		return nil
	})
	if err != nil {
		return pr4.SkillInstallation{}, err
	}
	return saved, nil
}

func (r *Repository) getSkillSettlementByUsageTx(tx *gorm.DB, usageID string) (pr4.SkillSettlement, error) {
	var record PR4SkillSettlementRecord
	if err := tx.Where("usage_id = ?", usageID).First(&record).Error; err != nil {
		return pr4.SkillSettlement{}, err
	}
	return skillSettlementContract(record)
}

func validatePrecreatedUsage(usage pr4.SkillUsageRecord, idempotencyKey string) error {
	if strings.TrimSpace(idempotencyKey) == "" {
		return errors.New("idempotency_key is required")
	}
	if err := pr4.ValidateSkillUsageRecord(usage); err != nil {
		return fmt.Errorf("usage_after_create: %w", err)
	}
	if usage.UsageStatus != "confirmation_required" || usage.ChargeStatus != "not_frozen" || usage.RefundStatus != "none" || usage.CreditHoldID != nil || usage.ValueDeliveredAt != nil {
		return errors.New("skill usage must be precreated before freeze or charge")
	}
	return nil
}

func validateFrozenSkillUsageCharge(current pr4.SkillUsageRecord, afterCharge pr4.SkillUsageRecord, settlement pr4.SkillSettlement) error {
	if err := pr4.ValidateSkillUsageRecord(afterCharge); err != nil {
		return fmt.Errorf("usage_after_charge: %w", err)
	}
	if err := pr4.ValidateSkillSettlement(settlement); err != nil {
		return fmt.Errorf("settlement: %w", err)
	}
	if !sameSkillUsageIdentity(current, afterCharge) {
		return errors.New("usage identity must be stable across frozen charge flow")
	}
	if afterCharge.UsageStatus != "value_delivered" || afterCharge.ChargeStatus != "charged" || afterCharge.RefundStatus != "none" {
		return errors.New("usage after charge must be value_delivered and charged")
	}
	if current.CreditHoldID == nil || afterCharge.CreditHoldID == nil || *current.CreditHoldID != *afterCharge.CreditHoldID {
		return errors.New("charged usage must keep frozen credit hold")
	}
	if settlement.UsageID != afterCharge.UsageID || settlement.Status != afterCharge.SettlementStatus {
		return errors.New("settlement must match charged usage")
	}
	if settlement.GrossCredits != afterCharge.EstimatedCredits {
		return errors.New("settlement gross credits must match usage estimated credits")
	}
	return nil
}

func skillPackageRecord(pkg pr4.SkillPackage) *PR4SkillPackageRecord {
	return &PR4SkillPackageRecord{
		SkillID:        pkg.SkillID,
		CreatorUserID:  pkg.CreatorUserID,
		Name:           pkg.Name,
		Description:    pkg.Description,
		Visibility:     pkg.Visibility,
		CurrentVersion: pkg.CurrentVersion,
		CreatedAt:      pkg.CreatedAt,
		UpdatedAt:      pkg.UpdatedAt,
	}
}

func skillVersionRecord(version pr4.SkillVersion) *PR4SkillVersionRecord {
	return &PR4SkillVersionRecord{
		SkillVersionID:      version.SkillVersionID,
		SkillID:             version.SkillID,
		Version:             version.Version,
		Status:              version.Status,
		RuntimeSpecDigest:   version.RuntimeSpecDigest,
		PricingPolicyDigest: version.PricingPolicyDigest,
		SubmittedAt:         version.SubmittedAt,
		PublishedAt:         version.PublishedAt,
		CreatedAt:           version.CreatedAt,
		UpdatedAt:           version.UpdatedAt,
	}
}

func skillPricingPolicyRecord(policy pr4.SkillPricingPolicy) *PR4SkillPricingPolicyRecord {
	return &PR4SkillPricingPolicyRecord{
		PricingPolicyID:     policy.PricingPolicyID,
		SkillID:             policy.SkillID,
		SkillVersion:        policy.SkillVersion,
		PricingModel:        policy.PricingModel,
		UsageCredits:        policy.UsageCredits,
		ValueDeliveredStage: policy.ValueDeliveredStage,
		PricingPolicyDigest: policy.PricingPolicyDigest,
		CreatedAt:           policy.CreatedAt,
	}
}

func marketplaceListingRecord(listing pr4.MarketplaceListing) *PR4MarketplaceListingRecord {
	return &PR4MarketplaceListingRecord{
		ListingID:           listing.ListingID,
		SkillID:             listing.SkillID,
		SkillVersionID:      listing.SkillVersionID,
		Status:              listing.Status,
		PricingPolicyDigest: listing.PricingPolicyDigest,
		PublishedBy:         listing.PublishedBy,
		ListedAt:            listing.ListedAt,
		CreatedAt:           listing.CreatedAt,
		UpdatedAt:           listing.UpdatedAt,
	}
}

func skillInstallationRecord(installation pr4.SkillInstallation, idempotencyKey string) PR4SkillInstallationRecord {
	return PR4SkillInstallationRecord{
		InstallationID:   installation.InstallationID,
		AccountID:        installation.AccountID,
		AccountScope:     installation.AccountScope,
		ListingID:        installation.ListingID,
		SkillID:          installation.SkillID,
		InstalledVersion: installation.InstalledVersion,
		VersionStrategy:  installation.VersionStrategy,
		Status:           installation.Status,
		UpgradeStatus:    installation.UpgradeStatus,
		IdempotencyKey:   idempotencyKey,
		CreatedAt:        installation.CreatedAt,
		UpdatedAt:        installation.UpdatedAt,
	}
}

func skillUsageRecord(usage pr4.SkillUsageRecord, idempotencyKey string) PR4SkillUsageRecord {
	return PR4SkillUsageRecord{
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
		IdempotencyKey:      idempotencyKey,
		ValueDeliveredAt:    usage.ValueDeliveredAt,
		CreatedAt:           usage.CreatedAt,
		UpdatedAt:           usage.UpdatedAt,
	}
}

func skillSettlementRecord(settlement pr4.SkillSettlement) PR4SkillSettlementRecord {
	return PR4SkillSettlementRecord{
		SettlementID:       settlement.SettlementID,
		UsageID:            settlement.UsageID,
		CreatorUserID:      settlement.CreatorUserID,
		Status:             settlement.Status,
		GrossCredits:       settlement.GrossCredits,
		PlatformFeeCredits: settlement.PlatformFeeCredits,
		CreatorCredits:     settlement.CreatorCredits,
		HoldUntil:          settlement.HoldUntil,
		CreatedAt:          settlement.CreatedAt,
		UpdatedAt:          settlement.UpdatedAt,
	}
}

func skillInstallationContract(record PR4SkillInstallationRecord) (pr4.SkillInstallation, error) {
	installation := pr4.SkillInstallation{
		SchemaVersion:    pr4.SchemaVersionSkillInstallation,
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
	if err := pr4.ValidateSkillInstallation(installation); err != nil {
		return pr4.SkillInstallation{}, err
	}
	return installation, nil
}

func marketplaceListingContract(record PR4MarketplaceListingRecord) (pr4.MarketplaceListing, error) {
	listing := pr4.MarketplaceListing{
		SchemaVersion:       pr4.SchemaVersionMarketplaceListing,
		ListingID:           record.ListingID,
		SkillID:             record.SkillID,
		SkillVersionID:      record.SkillVersionID,
		Status:              record.Status,
		PricingPolicyDigest: record.PricingPolicyDigest,
		PublishedBy:         record.PublishedBy,
		ListedAt:            utcTimePointer(record.ListedAt),
		CreatedAt:           record.CreatedAt.UTC(),
		UpdatedAt:           record.UpdatedAt.UTC(),
	}
	if err := pr4.ValidateMarketplaceListing(listing); err != nil {
		return pr4.MarketplaceListing{}, err
	}
	return listing, nil
}

func skillUsageContract(record PR4SkillUsageRecord) (pr4.SkillUsageRecord, error) {
	usage := pr4.SkillUsageRecord{
		SchemaVersion:       pr4.SchemaVersionSkillUsageRecord,
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
	if err := pr4.ValidateSkillUsageRecord(usage); err != nil {
		return pr4.SkillUsageRecord{}, err
	}
	return usage, nil
}

func skillSettlementContract(record PR4SkillSettlementRecord) (pr4.SkillSettlement, error) {
	settlement := pr4.SkillSettlement{
		SchemaVersion:      pr4.SchemaVersionSkillSettlement,
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
	if err := pr4.ValidateSkillSettlement(settlement); err != nil {
		return pr4.SkillSettlement{}, err
	}
	return settlement, nil
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func sameSkillInstallationIdentity(left pr4.SkillInstallation, right pr4.SkillInstallation) bool {
	return left.InstallationID == right.InstallationID &&
		left.AccountID == right.AccountID &&
		left.AccountScope == right.AccountScope &&
		left.ListingID == right.ListingID &&
		left.SkillID == right.SkillID &&
		left.InstalledVersion == right.InstalledVersion &&
		left.VersionStrategy == right.VersionStrategy
}

func sameSkillUsageIdentity(left pr4.SkillUsageRecord, right pr4.SkillUsageRecord) bool {
	return left.UsageID == right.UsageID &&
		left.RunID == right.RunID &&
		left.ListingID == right.ListingID &&
		left.SkillID == right.SkillID &&
		left.SkillVersion == right.SkillVersion &&
		left.PricingPolicyDigest == right.PricingPolicyDigest &&
		left.SkillUsageDigest == right.SkillUsageDigest &&
		left.EstimatedCredits == right.EstimatedCredits
}
