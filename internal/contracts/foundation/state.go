package foundation

const (
	StateRunStatus                 = "RunStatus"
	StateBoardStatus               = "BoardStatus"
	StateGraphPlanStatus           = "GraphPlanStatus"
	StateToolPlanStatus            = "ToolPlanStatus"
	StateToolTaskStatus            = "ToolTaskStatus"
	StateSkillVersionStatus        = "SkillVersionStatus"
	StateMarketplaceListingStatus  = "MarketplaceListingStatus"
	StateSkillUsageStatus          = "SkillUsageStatus"
	StateSkillUsageChargeStatus    = "SkillUsageChargeStatus"
	StateSkillUsageRefundStatus    = "SkillUsageRefundStatus"
	StateSettlementStatus          = "SettlementStatus"
	StateInstallationStatus        = "InstallationStatus"
	StateInstallationUpgradeStatus = "InstallationUpgradeStatus"
	StateRefundCaseStatus          = "RefundCaseStatus"
)

const (
	RunStatusCreated             = "created"
	RunStatusRouting             = "routing"
	RunStatusPlanning            = "planning"
	RunStatusWaitingInput        = "waiting_input"
	RunStatusWaitingConfirmation = "waiting_confirmation"
	RunStatusFreezing            = "freezing"
	RunStatusQueued              = "queued"
	RunStatusRunning             = "running"
	RunStatusCompleted           = "completed"
	RunStatusFailed              = "failed"
	RunStatusCancelled           = "cancelled"
)

var StateEnums = map[string][]string{
	StateRunStatus: {
		RunStatusCreated,
		RunStatusRouting,
		RunStatusPlanning,
		RunStatusWaitingInput,
		RunStatusWaitingConfirmation,
		RunStatusFreezing,
		RunStatusQueued,
		RunStatusRunning,
		RunStatusCompleted,
		RunStatusFailed,
		RunStatusCancelled,
	},
	StateBoardStatus: {
		"draft",
		"ready",
		"editing",
		"approved",
		"generating",
		"completed",
		"archived",
	},
	StateGraphPlanStatus: {
		"compiled",
		"running",
		"waiting_input",
		"waiting_confirmation",
		"completed",
		"failed",
		"cancelled",
	},
	StateToolPlanStatus: {
		"draft",
		"estimated",
		"confirmation_required",
		"confirmed",
		"frozen",
		"queued",
		"running",
		"partially_completed",
		"completed",
		"failed",
		"cancelled",
	},
	StateToolTaskStatus: {
		"pending",
		"queued",
		"running",
		"succeeded",
		"failed",
		"cancelled",
		"released",
	},
	StateSkillVersionStatus: {
		"draft",
		"submitted",
		"reviewing",
		"rejected",
		"published",
		"deprecated",
		"removed",
	},
	StateMarketplaceListingStatus: {
		"draft",
		"pending_listing_review",
		"listed",
		"unlisted",
		"suspended",
		"removed",
	},
	StateSkillUsageStatus: {
		"confirmation_required",
		"confirmation_declined",
		"cancelled",
		"expired",
		"frozen",
		"running",
		"value_delivered",
		"charged",
		"settlement_pending",
		"released",
		"refund_pending",
		"refunded",
		"refund_rejected",
		"failed",
	},
	StateSkillUsageChargeStatus: {
		"not_frozen",
		"frozen",
		"charged",
		"released",
		"failed",
	},
	StateSkillUsageRefundStatus: {
		"none",
		"refund_requested",
		"refund_reviewing",
		"refund_approved",
		"refund_rejected",
		"refund_reversed",
	},
	StateSettlementStatus: {
		"pending_hold",
		"eligible",
		"settling",
		"settled",
		"reversed",
		"frozen",
		"failed",
	},
	StateInstallationStatus: {
		"installed",
		"disabled",
		"removed",
		"upgrade_required",
	},
	StateInstallationUpgradeStatus: {
		"none",
		"available",
		"manual_confirmation_required",
		"confirmed",
		"skipped",
		"failed",
	},
	StateRefundCaseStatus: {
		"refund_requested",
		"refund_reviewing",
		"refund_approved",
		"refund_rejected",
		"refund_reversed",
		"settlement_adjusted",
	},
}

func IsValidState(enumName, value string) bool {
	for _, candidate := range StateEnums[enumName] {
		if candidate == value {
			return true
		}
	}
	return false
}
