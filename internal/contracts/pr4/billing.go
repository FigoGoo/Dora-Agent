package pr4

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

const (
	SchemaVersionSkillUsageRecord = "skill_usage_record.v1"
	SchemaVersionSkillSettlement  = "skill_settlement.v1"
)

var SkillUsageChargeSequence = []string{
	"EstimateSkillUsageCredits",
	"CreateSkillUsageRecord",
	"cost_disclosure.skill_usage.presented",
	"FreezeSkillUsageCredits",
	"GraphValueDelivered",
	"CommitSkillUsageAndSettle",
}

type SkillUsageRecord struct {
	SchemaVersion       string     `json:"schema_version"`
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
	CreditHoldID        *string    `json:"credit_hold_id"`
	ValueDeliveredAt    *time.Time `json:"value_delivered_at"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type SkillSettlement struct {
	SchemaVersion      string    `json:"schema_version"`
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

func ValidateSkillUsageRecord(record SkillUsageRecord) error {
	if record.SchemaVersion != SchemaVersionSkillUsageRecord {
		return fmt.Errorf("schema_version must be %s", SchemaVersionSkillUsageRecord)
	}
	if err := validatePrefixID(record.UsageID, "susage_"); err != nil {
		return fmt.Errorf("usage_id: %w", err)
	}
	if strings.TrimSpace(record.RunID) == "" {
		return errors.New("run_id is required")
	}
	if err := validatePrefixID(record.ListingID, "listing_"); err != nil {
		return fmt.Errorf("listing_id: %w", err)
	}
	if err := validatePrefixID(record.SkillID, "skill_"); err != nil {
		return fmt.Errorf("skill_id: %w", err)
	}
	if strings.TrimSpace(record.SkillVersion) == "" {
		return errors.New("skill_version is required")
	}
	if err := pr1.ValidateDigest(record.PricingPolicyDigest); err != nil {
		return fmt.Errorf("pricing_policy_digest: %w", err)
	}
	if err := pr1.ValidateDigest(record.SkillUsageDigest); err != nil {
		return fmt.Errorf("skill_usage_digest: %w", err)
	}
	if !pr1.IsValidState(pr1.StateSkillUsageStatus, record.UsageStatus) {
		return fmt.Errorf("invalid usage_status %q", record.UsageStatus)
	}
	if !pr1.IsValidState(pr1.StateSkillUsageChargeStatus, record.ChargeStatus) {
		return fmt.Errorf("invalid charge_status %q", record.ChargeStatus)
	}
	if !pr1.IsValidState(pr1.StateSkillUsageRefundStatus, record.RefundStatus) {
		return fmt.Errorf("invalid refund_status %q", record.RefundStatus)
	}
	if !pr1.IsValidState(pr1.StateSettlementStatus, record.SettlementStatus) {
		return fmt.Errorf("invalid settlement_status %q", record.SettlementStatus)
	}
	if record.EstimatedCredits < 0 {
		return errors.New("estimated_credits must be >= 0")
	}
	if record.ChargeStatus == "not_frozen" && record.CreditHoldID != nil {
		return errors.New("not_frozen usage must not have credit_hold_id")
	}
	if record.ChargeStatus == "charged" && (record.CreditHoldID == nil || strings.TrimSpace(*record.CreditHoldID) == "") {
		return errors.New("charged usage requires credit_hold_id")
	}
	if record.UsageStatus == "value_delivered" && record.ValueDeliveredAt == nil {
		return errors.New("value_delivered usage requires value_delivered_at")
	}
	return validateTimeRange(record.CreatedAt, record.UpdatedAt)
}

func ValidateSkillSettlement(settlement SkillSettlement) error {
	if settlement.SchemaVersion != SchemaVersionSkillSettlement {
		return fmt.Errorf("schema_version must be %s", SchemaVersionSkillSettlement)
	}
	if err := validatePrefixID(settlement.SettlementID, "settle_"); err != nil {
		return fmt.Errorf("settlement_id: %w", err)
	}
	if err := validatePrefixID(settlement.UsageID, "susage_"); err != nil {
		return fmt.Errorf("usage_id: %w", err)
	}
	if strings.TrimSpace(settlement.CreatorUserID) == "" {
		return errors.New("creator_user_id is required")
	}
	if !pr1.IsValidState(pr1.StateSettlementStatus, settlement.Status) {
		return fmt.Errorf("invalid settlement status %q", settlement.Status)
	}
	if settlement.GrossCredits < 0 || settlement.PlatformFeeCredits < 0 || settlement.CreatorCredits < 0 {
		return errors.New("settlement credits must be >= 0")
	}
	if settlement.PlatformFeeCredits+settlement.CreatorCredits != settlement.GrossCredits {
		return errors.New("platform_fee_credits + creator_credits must equal gross_credits")
	}
	if settlement.HoldUntil.IsZero() {
		return errors.New("hold_until is required")
	}
	return validateTimeRange(settlement.CreatedAt, settlement.UpdatedAt)
}

func ValidateSkillUsagePrecreateConfirmCharge(sequence []string, afterCreate SkillUsageRecord, afterCharge SkillUsageRecord, settlement SkillSettlement) error {
	if !sameStringSlice(sequence, SkillUsageChargeSequence) {
		return errors.New("skill usage sequence must match PR-4 precreate confirmation flow")
	}
	if err := ValidateSkillUsageRecord(afterCreate); err != nil {
		return fmt.Errorf("usage_after_create: %w", err)
	}
	if err := ValidateSkillUsageRecord(afterCharge); err != nil {
		return fmt.Errorf("usage_after_charge: %w", err)
	}
	if err := ValidateSkillSettlement(settlement); err != nil {
		return fmt.Errorf("settlement: %w", err)
	}
	if afterCreate.UsageStatus != "confirmation_required" || afterCreate.ChargeStatus != "not_frozen" || afterCreate.RefundStatus != "none" {
		return errors.New("usage must be precreated as confirmation_required and not_frozen")
	}
	if afterCreate.CreditHoldID != nil || afterCreate.ValueDeliveredAt != nil {
		return errors.New("precreated usage must not have credit hold or value delivered time")
	}
	if !sameUsageIdentity(afterCreate, afterCharge) {
		return errors.New("usage identity must be stable across charge flow")
	}
	if afterCharge.UsageStatus != "value_delivered" || afterCharge.ChargeStatus != "charged" || afterCharge.RefundStatus != "none" {
		return errors.New("usage after charge must be value_delivered and charged")
	}
	if settlement.UsageID != afterCharge.UsageID || settlement.Status != afterCharge.SettlementStatus {
		return errors.New("settlement must match charged usage")
	}
	if settlement.GrossCredits != afterCharge.EstimatedCredits {
		return errors.New("settlement gross credits must match usage estimated credits")
	}
	return nil
}

func ValidateSkillUsageRefundReversal(before SkillUsageRecord, after SkillUsageRecord, settlement SkillSettlement) error {
	if err := ValidateSkillUsageRecord(before); err != nil {
		return fmt.Errorf("usage_before_refund: %w", err)
	}
	if err := ValidateSkillUsageRecord(after); err != nil {
		return fmt.Errorf("usage_after_refund: %w", err)
	}
	if err := ValidateSkillSettlement(settlement); err != nil {
		return fmt.Errorf("settlement_after_reverse: %w", err)
	}
	if !sameUsageIdentity(before, after) {
		return errors.New("usage identity must be stable across refund")
	}
	if before.UsageStatus != "refund_pending" || before.RefundStatus != "refund_requested" {
		return errors.New("refund reversal must start from refund_pending/refund_requested")
	}
	if after.UsageStatus != "refunded" || after.ChargeStatus != "released" || after.RefundStatus != "refund_reversed" || after.SettlementStatus != "reversed" {
		return errors.New("refund reversal must set refunded/released/refund_reversed/reversed")
	}
	if settlement.UsageID != after.UsageID || settlement.Status != "reversed" {
		return errors.New("settlement must be reversed for refunded usage")
	}
	return nil
}

func sameUsageIdentity(left SkillUsageRecord, right SkillUsageRecord) bool {
	return left.UsageID == right.UsageID &&
		left.RunID == right.RunID &&
		left.ListingID == right.ListingID &&
		left.SkillID == right.SkillID &&
		left.SkillVersion == right.SkillVersion &&
		left.PricingPolicyDigest == right.PricingPolicyDigest &&
		left.SkillUsageDigest == right.SkillUsageDigest &&
		left.EstimatedCredits == right.EstimatedCredits
}

func sameStringSlice(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
