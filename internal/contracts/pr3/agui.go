package pr3

import (
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

const (
	EventTypeCostDisclosureGenerationPresented = "cost_disclosure.generation.presented"
	EventTypeToolTaskUpdated                   = "tool.task.updated"
	EventTypeAssetCommitUpdated                = "asset.commit.updated"
)

type GenerationCostDisclosurePayload struct {
	ToolPlanID       string    `json:"tool_plan_id"`
	ToolPlanDigest   string    `json:"tool_plan_digest"`
	BoardID          string    `json:"board_id"`
	BoardVersion     int       `json:"board_version"`
	EstimatedCredits int       `json:"estimated_credits"`
	Currency         string    `json:"currency"`
	ExpiresAt        time.Time `json:"expires_at"`
	SkillUsageStatus *string   `json:"skill_usage_status"`
}

type ToolTaskUpdatedPayload struct {
	ToolTaskID   string  `json:"tool_task_id"`
	ToolPlanID   string  `json:"tool_plan_id"`
	Status       string  `json:"status"`
	Progress     int     `json:"progress"`
	OutputDigest *string `json:"output_digest,omitempty"`
	ErrorCode    *string `json:"error_code"`
}

type AssetCommitUpdatedPayload struct {
	ToolTaskID        string   `json:"tool_task_id"`
	CommitStatus      string   `json:"commit_status"`
	CommittedAssetIDs []string `json:"committed_asset_ids"`
	FailedAssetCount  int      `json:"failed_asset_count"`
}

func ValidateGenerationCostDisclosurePayload(payload GenerationCostDisclosurePayload) error {
	if err := validatePrefixID(payload.ToolPlanID, "tpl_"); err != nil {
		return fmt.Errorf("tool_plan_id: %w", err)
	}
	if err := pr1.ValidateDigest(payload.ToolPlanDigest); err != nil {
		return fmt.Errorf("tool_plan_digest: %w", err)
	}
	if err := validatePrefixID(payload.BoardID, "board_"); err != nil {
		return fmt.Errorf("board_id: %w", err)
	}
	if payload.BoardVersion < 1 || payload.EstimatedCredits < 0 {
		return errors.New("board_version must be >= 1 and estimated_credits must be >= 0")
	}
	if payload.Currency != CurrencyCredits {
		return fmt.Errorf("currency must be %s", CurrencyCredits)
	}
	if payload.ExpiresAt.IsZero() {
		return errors.New("expires_at is required")
	}
	if payload.SkillUsageStatus != nil && !pr1.IsValidState(pr1.StateSkillUsageStatus, *payload.SkillUsageStatus) {
		return fmt.Errorf("invalid skill_usage_status %q", *payload.SkillUsageStatus)
	}
	return nil
}

func ValidateToolTaskUpdatedPayload(payload ToolTaskUpdatedPayload) error {
	if err := validatePrefixID(payload.ToolTaskID, "ttask_"); err != nil {
		return fmt.Errorf("tool_task_id: %w", err)
	}
	if err := validatePrefixID(payload.ToolPlanID, "tpl_"); err != nil {
		return fmt.Errorf("tool_plan_id: %w", err)
	}
	if !pr1.IsValidState(pr1.StateToolTaskStatus, payload.Status) {
		return fmt.Errorf("invalid status %q", payload.Status)
	}
	if payload.Progress < 0 || payload.Progress > 100 {
		return errors.New("progress must be between 0 and 100")
	}
	if payload.OutputDigest != nil {
		if err := pr1.ValidateDigest(*payload.OutputDigest); err != nil {
			return fmt.Errorf("output_digest: %w", err)
		}
	}
	if payload.Status == "failed" && (payload.ErrorCode == nil || *payload.ErrorCode == "") {
		return errors.New("failed task update requires error_code")
	}
	return nil
}

func ValidateAssetCommitUpdatedPayload(payload AssetCommitUpdatedPayload) error {
	if err := validatePrefixID(payload.ToolTaskID, "ttask_"); err != nil {
		return fmt.Errorf("tool_task_id: %w", err)
	}
	if !isAllowed(payload.CommitStatus, []string{"committed", "partially_committed", "failed"}) {
		return fmt.Errorf("invalid commit_status %q", payload.CommitStatus)
	}
	if payload.FailedAssetCount < 0 {
		return errors.New("failed_asset_count must be >= 0")
	}
	for _, assetID := range payload.CommittedAssetIDs {
		if err := validatePrefixID(assetID, "asset_"); err != nil {
			return fmt.Errorf("committed_asset_ids: %w", err)
		}
	}
	return nil
}
