package pr4

import "testing"

func TestSkillUsagePrecreateConfirmChargeFixture(t *testing.T) {
	var fixture struct {
		Sequence         []string         `json:"sequence"`
		UsageAfterCreate SkillUsageRecord `json:"usage_after_create"`
		UsageAfterCharge SkillUsageRecord `json:"usage_after_charge"`
		Settlement       SkillSettlement  `json:"settlement"`
	}
	readFixture(t, "tests/fixtures/contracts/billing/skill_usage_precreate_confirm_charge.json", &fixture)

	if err := ValidateSkillUsagePrecreateConfirmCharge(fixture.Sequence, fixture.UsageAfterCreate, fixture.UsageAfterCharge, fixture.Settlement); err != nil {
		t.Fatalf("fixture violates skill usage charge contract: %v", err)
	}
}

func TestSkillUsageCreationMustPrecedeFreeze(t *testing.T) {
	var fixture struct {
		Sequence         []string         `json:"sequence"`
		UsageAfterCreate SkillUsageRecord `json:"usage_after_create"`
		UsageAfterCharge SkillUsageRecord `json:"usage_after_charge"`
		Settlement       SkillSettlement  `json:"settlement"`
	}
	readFixture(t, "tests/fixtures/contracts/billing/skill_usage_precreate_confirm_charge.json", &fixture)

	fixture.Sequence[1], fixture.Sequence[3] = fixture.Sequence[3], fixture.Sequence[1]
	if err := ValidateSkillUsagePrecreateConfirmCharge(fixture.Sequence, fixture.UsageAfterCreate, fixture.UsageAfterCharge, fixture.Settlement); err == nil {
		t.Fatalf("skill usage record must be created before freeze")
	}
}

func TestSkillUsageRefundReversalFixture(t *testing.T) {
	var fixture struct {
		UsageBeforeRefund      SkillUsageRecord `json:"usage_before_refund"`
		UsageAfterRefund       SkillUsageRecord `json:"usage_after_refund"`
		SettlementAfterReverse SkillSettlement  `json:"settlement_after_reverse"`
	}
	readFixture(t, "tests/fixtures/contracts/billing/skill_usage_refund_reversal.json", &fixture)

	if err := ValidateSkillUsageRefundReversal(fixture.UsageBeforeRefund, fixture.UsageAfterRefund, fixture.SettlementAfterReverse); err != nil {
		t.Fatalf("fixture violates refund reversal contract: %v", err)
	}
}
