package toolasset

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
)

const (
	SchemaVersionCreditFreeze = "credit_freeze.v1"
)

const (
	AccountScopePersonal   = "personal"
	AccountScopeEnterprise = "enterprise"
)

const (
	CreditFreezeStatusFrozen    = "frozen"
	CreditFreezeStatusCommitted = "committed"
	CreditFreezeStatusReleased  = "released"
	CreditFreezeStatusFailed    = "failed"
)

type FreezeCreditsRequest struct {
	CreditAccountID    string `json:"credit_account_id"`
	CreditAccountScope string `json:"credit_account_scope"`
	RunID              string `json:"run_id"`
	ProjectID          string `json:"project_id"`
	ToolPlanID         string `json:"tool_plan_id"`
	ToolPlanDigest     string `json:"tool_plan_digest"`
	Credits            int    `json:"credits"`
	IdempotencyKey     string `json:"idempotency_key"`
}

type CreditFreeze struct {
	SchemaVersion    string     `json:"schema_version"`
	CreditHoldID     string     `json:"credit_hold_id"`
	AccountID        string     `json:"account_id"`
	AccountScope     string     `json:"account_scope"`
	RunID            string     `json:"run_id"`
	ToolPlanID       string     `json:"tool_plan_id"`
	ToolPlanDigest   string     `json:"tool_plan_digest"`
	Status           string     `json:"status"`
	FrozenCredits    int        `json:"frozen_credits"`
	CommittedCredits int        `json:"committed_credits"`
	ReleasedCredits  int        `json:"released_credits"`
	IdempotencyKey   string     `json:"idempotency_key"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        *time.Time `json:"updated_at"`
}

func ValidateFreezeCreditsRequest(request FreezeCreditsRequest) error {
	if strings.TrimSpace(request.CreditAccountID) == "" || strings.TrimSpace(request.RunID) == "" || strings.TrimSpace(request.ProjectID) == "" {
		return errors.New("credit_account_id, run_id and project_id are required")
	}
	if !isAllowed(request.CreditAccountScope, []string{AccountScopePersonal, AccountScopeEnterprise}) {
		return fmt.Errorf("invalid credit_account_scope %q", request.CreditAccountScope)
	}
	if err := validatePrefixID(request.ToolPlanID, "tpl_"); err != nil {
		return fmt.Errorf("tool_plan_id: %w", err)
	}
	if err := foundation.ValidateDigest(request.ToolPlanDigest); err != nil {
		return fmt.Errorf("tool_plan_digest: %w", err)
	}
	if request.Credits < 0 {
		return errors.New("credits must be >= 0")
	}
	if strings.TrimSpace(request.IdempotencyKey) == "" || len(request.IdempotencyKey) > 160 {
		return errors.New("idempotency_key is required and must be <= 160 characters")
	}
	return nil
}

func ValidateCreditFreeze(hold CreditFreeze) error {
	if hold.SchemaVersion != SchemaVersionCreditFreeze {
		return fmt.Errorf("schema_version must be %s", SchemaVersionCreditFreeze)
	}
	if err := validatePrefixID(hold.CreditHoldID, "chold_"); err != nil {
		return fmt.Errorf("credit_hold_id: %w", err)
	}
	if strings.TrimSpace(hold.AccountID) == "" || strings.TrimSpace(hold.RunID) == "" {
		return errors.New("account_id and run_id are required")
	}
	if !isAllowed(hold.AccountScope, []string{AccountScopePersonal, AccountScopeEnterprise}) {
		return fmt.Errorf("invalid account_scope %q", hold.AccountScope)
	}
	if err := validatePrefixID(hold.ToolPlanID, "tpl_"); err != nil {
		return fmt.Errorf("tool_plan_id: %w", err)
	}
	if err := foundation.ValidateDigest(hold.ToolPlanDigest); err != nil {
		return fmt.Errorf("tool_plan_digest: %w", err)
	}
	if !isAllowed(hold.Status, []string{
		CreditFreezeStatusFrozen,
		CreditFreezeStatusCommitted,
		CreditFreezeStatusReleased,
		CreditFreezeStatusFailed,
	}) {
		return fmt.Errorf("invalid status %q", hold.Status)
	}
	if hold.FrozenCredits < 0 || hold.CommittedCredits < 0 || hold.ReleasedCredits < 0 {
		return errors.New("credit amounts must be >= 0")
	}
	if hold.CommittedCredits+hold.ReleasedCredits > hold.FrozenCredits {
		return errors.New("committed + released credits must not exceed frozen credits")
	}
	switch hold.Status {
	case CreditFreezeStatusFrozen:
		if hold.CommittedCredits != 0 || hold.ReleasedCredits != 0 {
			return errors.New("frozen hold must not have committed or released credits")
		}
	case CreditFreezeStatusCommitted:
		if hold.CommittedCredits <= 0 || hold.ReleasedCredits != 0 {
			return errors.New("committed hold requires committed credits and no released credits")
		}
	case CreditFreezeStatusReleased:
		if hold.ReleasedCredits <= 0 || hold.CommittedCredits != 0 {
			return errors.New("released hold requires released credits and no committed credits")
		}
	}
	if strings.TrimSpace(hold.IdempotencyKey) == "" || len(hold.IdempotencyKey) > 160 {
		return errors.New("idempotency_key is required and must be <= 160 characters")
	}
	if hold.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	if hold.UpdatedAt != nil && hold.UpdatedAt.Before(hold.CreatedAt) {
		return errors.New("updated_at must not be before created_at")
	}
	return nil
}

func ValidateFreezeCommitFlow(request FreezeCreditsRequest, frozen CreditFreeze, committed CreditFreeze) error {
	if err := ValidateFreezeCreditsRequest(request); err != nil {
		return fmt.Errorf("freeze_request: %w", err)
	}
	if err := ValidateCreditFreeze(frozen); err != nil {
		return fmt.Errorf("hold_after_freeze: %w", err)
	}
	if err := ValidateCreditFreeze(committed); err != nil {
		return fmt.Errorf("hold_after_commit: %w", err)
	}
	if frozen.Status != CreditFreezeStatusFrozen || committed.Status != CreditFreezeStatusCommitted {
		return errors.New("freeze commit flow requires frozen -> committed")
	}
	if !sameCreditHoldIdentity(frozen, committed) {
		return errors.New("credit hold identity must be stable")
	}
	if request.CreditAccountID != frozen.AccountID || request.CreditAccountScope != frozen.AccountScope {
		return errors.New("freeze request account must match hold")
	}
	if request.ToolPlanID != frozen.ToolPlanID || request.ToolPlanDigest != frozen.ToolPlanDigest {
		return errors.New("freeze request tool plan must match hold")
	}
	if request.Credits != frozen.FrozenCredits {
		return errors.New("freeze request credits must match frozen credits")
	}
	if committed.CommittedCredits != frozen.FrozenCredits {
		return errors.New("committed credits must equal frozen credits")
	}
	return nil
}

func ValidateFreezeReleaseOnFailure(frozen CreditFreeze, failedTask ToolTask, released CreditFreeze) error {
	if err := ValidateCreditFreeze(frozen); err != nil {
		return fmt.Errorf("hold_after_freeze: %w", err)
	}
	if err := ValidateToolTask(failedTask); err != nil {
		return fmt.Errorf("tool_task_failure: %w", err)
	}
	if err := ValidateCreditFreeze(released); err != nil {
		return fmt.Errorf("hold_after_release: %w", err)
	}
	if frozen.Status != CreditFreezeStatusFrozen || failedTask.Status != "failed" || released.Status != CreditFreezeStatusReleased {
		return errors.New("release on failure requires frozen hold, failed task and released hold")
	}
	if !sameCreditHoldIdentity(frozen, released) {
		return errors.New("credit hold identity must be stable")
	}
	if released.ReleasedCredits != frozen.FrozenCredits {
		return errors.New("released credits must equal frozen credits")
	}
	return nil
}

func sameCreditHoldIdentity(left CreditFreeze, right CreditFreeze) bool {
	return left.CreditHoldID == right.CreditHoldID &&
		left.AccountID == right.AccountID &&
		left.AccountScope == right.AccountScope &&
		left.RunID == right.RunID &&
		left.ToolPlanID == right.ToolPlanID &&
		left.ToolPlanDigest == right.ToolPlanDigest &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.FrozenCredits == right.FrozenCredits
}
