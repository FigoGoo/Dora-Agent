package pr3

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

const (
	SchemaVersionToolPlan   = "tool_plan.v1"
	SchemaVersionToolTask   = "tool_task.v1"
	SchemaVersionToolResult = "tool_result.v1"
)

const (
	CurrencyCredits = "credits"
)

const (
	ResourceTypeImage      = "image"
	ResourceTypeVideo      = "video"
	ResourceTypeAudio      = "audio"
	ResourceTypeText       = "text"
	ResourceTypeMultimodal = "multimodal"
)

const (
	ProviderModeSync          = "sync"
	ProviderModeAsyncCallback = "async_callback"
	ProviderModeAsyncPolling  = "async_polling"
)

const (
	ToolResultStatusSucceeded          = "succeeded"
	ToolResultStatusPartiallySucceeded = "partially_succeeded"
	ToolResultStatusFailed             = "failed"
)

type ToolPlan struct {
	SchemaVersion        string         `json:"schema_version"`
	ToolPlanID           string         `json:"tool_plan_id"`
	RunID                string         `json:"run_id"`
	BoardID              string         `json:"board_id"`
	BoardVersion         int            `json:"board_version"`
	GraphPlanID          string         `json:"graph_plan_id"`
	Status               string         `json:"status"`
	Items                []ToolPlanItem `json:"items"`
	EstimatedCredits     int            `json:"estimated_credits"`
	Currency             string         `json:"currency"`
	ConfirmationRequired bool           `json:"confirmation_required"`
	ExpiresAt            *time.Time     `json:"expires_at"`
	ToolPlanDigest       string         `json:"tool_plan_digest"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

type ToolPlanItem struct {
	ToolPlanItemID   string `json:"tool_plan_item_id"`
	ToolID           string `json:"tool_id"`
	ToolVersion      string `json:"tool_version"`
	ResourceType     string `json:"resource_type"`
	Quantity         int    `json:"quantity"`
	EstimatedCredits int    `json:"estimated_credits"`
	InputDigest      string `json:"input_digest"`
}

type ToolTask struct {
	SchemaVersion  string         `json:"schema_version"`
	ToolTaskID     string         `json:"tool_task_id"`
	ToolPlanID     string         `json:"tool_plan_id"`
	ToolPlanItemID string         `json:"tool_plan_item_id"`
	RunID          string         `json:"run_id"`
	Status         string         `json:"status"`
	Progress       int            `json:"progress"`
	ProviderPolicy ProviderPolicy `json:"provider_policy"`
	IdempotencyKey string         `json:"idempotency_key"`
	InputDigest    string         `json:"input_digest"`
	OutputDigest   *string        `json:"output_digest,omitempty"`
	ErrorCode      *string        `json:"error_code"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type ProviderPolicy struct {
	Mode      string `json:"mode"`
	TimeoutMS int    `json:"timeout_ms"`
	Retryable bool   `json:"retryable"`
}

type ToolResult struct {
	SchemaVersion string           `json:"schema_version"`
	ToolResultID  string           `json:"tool_result_id"`
	ToolTaskID    string           `json:"tool_task_id"`
	Status        string           `json:"status"`
	Assets        []GeneratedAsset `json:"assets"`
	ResultDigest  string           `json:"result_digest"`
	CreatedAt     time.Time        `json:"created_at"`
}

type ToolPlanPrecondition struct {
	BoardID      string `json:"board_id"`
	BoardVersion int    `json:"board_version"`
	BoardStatus  string `json:"board_status"`
	GraphPlanID  string `json:"graph_plan_id"`
}

func ValidateToolPlan(plan ToolPlan) error {
	if plan.SchemaVersion != SchemaVersionToolPlan {
		return fmt.Errorf("schema_version must be %s", SchemaVersionToolPlan)
	}
	if err := validatePrefixID(plan.ToolPlanID, "tpl_"); err != nil {
		return fmt.Errorf("tool_plan_id: %w", err)
	}
	if strings.TrimSpace(plan.RunID) == "" {
		return errors.New("run_id is required")
	}
	if err := validatePrefixID(plan.BoardID, "board_"); err != nil {
		return fmt.Errorf("board_id: %w", err)
	}
	if plan.BoardVersion < 1 {
		return errors.New("board_version must be >= 1")
	}
	if err := validatePrefixID(plan.GraphPlanID, "gplan_"); err != nil {
		return fmt.Errorf("graph_plan_id: %w", err)
	}
	if !pr1.IsValidState(pr1.StateToolPlanStatus, plan.Status) {
		return fmt.Errorf("invalid tool plan status %q", plan.Status)
	}
	if len(plan.Items) == 0 {
		return errors.New("items are required")
	}
	total := 0
	for index, item := range plan.Items {
		if err := ValidateToolPlanItem(item); err != nil {
			return fmt.Errorf("item %d: %w", index+1, err)
		}
		total += item.EstimatedCredits
	}
	if plan.EstimatedCredits != total {
		return fmt.Errorf("estimated_credits=%d does not match item total %d", plan.EstimatedCredits, total)
	}
	if plan.Currency != CurrencyCredits {
		return fmt.Errorf("currency must be %s", CurrencyCredits)
	}
	if plan.Status == "confirmation_required" && !plan.ConfirmationRequired {
		return errors.New("confirmation_required status requires confirmation_required=true")
	}
	if err := pr1.ValidateDigest(plan.ToolPlanDigest); err != nil {
		return fmt.Errorf("tool_plan_digest: %w", err)
	}
	if plan.CreatedAt.IsZero() || plan.UpdatedAt.IsZero() {
		return errors.New("created_at and updated_at are required")
	}
	if plan.UpdatedAt.Before(plan.CreatedAt) {
		return errors.New("updated_at must not be before created_at")
	}
	if plan.ExpiresAt != nil && plan.ExpiresAt.Before(plan.CreatedAt) {
		return errors.New("expires_at must not be before created_at")
	}
	return nil
}

func ValidateToolPlanItem(item ToolPlanItem) error {
	if err := validatePrefixID(item.ToolPlanItemID, "tpi_"); err != nil {
		return fmt.Errorf("tool_plan_item_id: %w", err)
	}
	if strings.TrimSpace(item.ToolID) == "" || strings.TrimSpace(item.ToolVersion) == "" {
		return errors.New("tool_id and tool_version are required")
	}
	if !isAllowed(item.ResourceType, resourceTypes()) {
		return fmt.Errorf("invalid resource_type %q", item.ResourceType)
	}
	if item.Quantity < 1 || item.EstimatedCredits < 0 {
		return errors.New("quantity must be >= 1 and estimated_credits must be >= 0")
	}
	if err := pr1.ValidateDigest(item.InputDigest); err != nil {
		return fmt.Errorf("input_digest: %w", err)
	}
	return nil
}

func ValidateToolPlanForApprovedBoard(precondition ToolPlanPrecondition, plan ToolPlan) error {
	if err := ValidateToolPlan(plan); err != nil {
		return err
	}
	if precondition.BoardStatus != "approved" {
		return errors.New("ToolPlan requires approved board")
	}
	if plan.BoardID != precondition.BoardID || plan.BoardVersion != precondition.BoardVersion {
		return errors.New("ToolPlan board id and version must match approved board")
	}
	if plan.GraphPlanID != precondition.GraphPlanID {
		return errors.New("ToolPlan graph_plan_id must match precondition")
	}
	return nil
}

func ValidateToolTask(task ToolTask) error {
	if task.SchemaVersion != SchemaVersionToolTask {
		return fmt.Errorf("schema_version must be %s", SchemaVersionToolTask)
	}
	if err := validatePrefixID(task.ToolTaskID, "ttask_"); err != nil {
		return fmt.Errorf("tool_task_id: %w", err)
	}
	if err := validatePrefixID(task.ToolPlanID, "tpl_"); err != nil {
		return fmt.Errorf("tool_plan_id: %w", err)
	}
	if err := validatePrefixID(task.ToolPlanItemID, "tpi_"); err != nil {
		return fmt.Errorf("tool_plan_item_id: %w", err)
	}
	if strings.TrimSpace(task.RunID) == "" {
		return errors.New("run_id is required")
	}
	if !pr1.IsValidState(pr1.StateToolTaskStatus, task.Status) {
		return fmt.Errorf("invalid tool task status %q", task.Status)
	}
	if task.Progress < 0 || task.Progress > 100 {
		return errors.New("progress must be between 0 and 100")
	}
	if err := ValidateProviderPolicy(task.ProviderPolicy); err != nil {
		return fmt.Errorf("provider_policy: %w", err)
	}
	if strings.TrimSpace(task.IdempotencyKey) == "" || len(task.IdempotencyKey) > 160 {
		return errors.New("idempotency_key is required and must be <= 160 characters")
	}
	if err := pr1.ValidateDigest(task.InputDigest); err != nil {
		return fmt.Errorf("input_digest: %w", err)
	}
	if task.OutputDigest != nil {
		if err := pr1.ValidateDigest(*task.OutputDigest); err != nil {
			return fmt.Errorf("output_digest: %w", err)
		}
	}
	if task.Status == "failed" && (task.ErrorCode == nil || strings.TrimSpace(*task.ErrorCode) == "") {
		return errors.New("failed task requires error_code")
	}
	if task.CreatedAt.IsZero() || task.UpdatedAt.IsZero() {
		return errors.New("created_at and updated_at are required")
	}
	if task.UpdatedAt.Before(task.CreatedAt) {
		return errors.New("updated_at must not be before created_at")
	}
	return nil
}

func ValidateProviderPolicy(policy ProviderPolicy) error {
	if !isAllowed(policy.Mode, []string{ProviderModeSync, ProviderModeAsyncCallback, ProviderModeAsyncPolling}) {
		return fmt.Errorf("invalid mode %q", policy.Mode)
	}
	if policy.TimeoutMS < 1000 {
		return errors.New("timeout_ms must be >= 1000")
	}
	return nil
}

func ValidateToolResult(result ToolResult) error {
	if result.SchemaVersion != SchemaVersionToolResult {
		return fmt.Errorf("schema_version must be %s", SchemaVersionToolResult)
	}
	if err := validatePrefixID(result.ToolResultID, "tres_"); err != nil {
		return fmt.Errorf("tool_result_id: %w", err)
	}
	if err := validatePrefixID(result.ToolTaskID, "ttask_"); err != nil {
		return fmt.Errorf("tool_task_id: %w", err)
	}
	if !isAllowed(result.Status, []string{
		ToolResultStatusSucceeded,
		ToolResultStatusPartiallySucceeded,
		ToolResultStatusFailed,
	}) {
		return fmt.Errorf("invalid result status %q", result.Status)
	}
	for index, asset := range result.Assets {
		if asset.ToolTaskID != result.ToolTaskID {
			return fmt.Errorf("asset %d belongs to task %q, expected %q", index+1, asset.ToolTaskID, result.ToolTaskID)
		}
		if err := ValidateGeneratedAsset(asset); err != nil {
			return fmt.Errorf("asset %d: %w", index+1, err)
		}
	}
	if err := pr1.ValidateDigest(result.ResultDigest); err != nil {
		return fmt.Errorf("result_digest: %w", err)
	}
	if result.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	return nil
}

func resourceTypes() []string {
	return []string{
		ResourceTypeImage,
		ResourceTypeVideo,
		ResourceTypeAudio,
		ResourceTypeText,
		ResourceTypeMultimodal,
	}
}
