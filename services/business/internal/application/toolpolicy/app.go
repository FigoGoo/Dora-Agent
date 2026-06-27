package toolpolicy

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const activeStatus = "active"

type App struct {
	repo *businesscore.Repository
	now  func() time.Time
}

func New(repo *businesscore.Repository) *App {
	return &App{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type ExecutionPolicyDTO struct {
	Allowed              bool              `json:"allowed"`
	RiskLevel            string            `json:"risk_level"`
	RequiresConfirmation bool              `json:"requires_confirmation"`
	TimeoutMS            int32             `json:"timeout_ms"`
	RetryPolicy          map[string]string `json:"retry_policy,omitempty"`
	CancelPolicy         map[string]string `json:"cancel_policy,omitempty"`
	ChargeMode           string            `json:"charge_mode,omitempty"`
	BillingUnit          string            `json:"billing_unit,omitempty"`
	PricingPolicyID      string            `json:"pricing_policy_id,omitempty"`
	Reason               string            `json:"reason,omitempty"`
}

type ToolDTO struct {
	ToolName             string            `json:"tool_name"`
	ToolType             string            `json:"tool_type"`
	DisplayName          string            `json:"display_name"`
	Description          string            `json:"description,omitempty"`
	Status               string            `json:"status"`
	Version              string            `json:"version"`
	Allowed              bool              `json:"allowed"`
	RiskLevel            string            `json:"risk_level"`
	RequiresConfirmation bool              `json:"requires_confirmation"`
	TimeoutMS            int32             `json:"timeout_ms"`
	RetryPolicy          map[string]string `json:"retry_policy,omitempty"`
	CancelPolicy         map[string]string `json:"cancel_policy,omitempty"`
	ChargeMode           string            `json:"charge_mode,omitempty"`
	BillingUnit          string            `json:"billing_unit,omitempty"`
	PricingPolicyID      string            `json:"pricing_policy_id,omitempty"`
}

type Page[T any] struct {
	Items  []T `json:"items"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func (a *App) CheckToolExecutionPolicy(ctx context.Context, auth accountspace.AuthContext, toolName, toolType, _ string, _ map[string]string) (ExecutionPolicyDTO, error) {
	toolName = strings.TrimSpace(toolName)
	toolType = strings.TrimSpace(toolType)
	if toolName == "" || toolType == "" {
		return ExecutionPolicyDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "tool_name and tool_type are required")
	}
	var definition businesscore.ToolDefinition
	if err := a.repo.DB().WithContext(ctx).Where("tool_name = ? AND tool_type = ?", toolName, toolType).Order("created_at DESC").First(&definition).Error; err != nil {
		return ExecutionPolicyDTO{Allowed: false, RiskLevel: "unknown", TimeoutMS: 0, Reason: "tool_not_found"}, nil
	}
	if definition.Status != activeStatus {
		return ExecutionPolicyDTO{Allowed: false, RiskLevel: "disabled", TimeoutMS: 0, Reason: "tool_disabled"}, nil
	}
	policy, err := a.activePolicy(ctx, toolName, toolType)
	if err != nil {
		return ExecutionPolicyDTO{}, err
	}
	allowed := policy.Allowed
	reason := ""
	if whitelist, ok := a.matchWhitelist(ctx, auth, toolName, toolType); ok {
		allowed = whitelist.Allowed
		if whitelist.Reason != nil {
			reason = *whitelist.Reason
		}
	}
	pricing := a.activePricing(ctx, toolName, toolType)
	dto := ExecutionPolicyDTO{
		Allowed: allowed, RiskLevel: policy.RiskLevel, RequiresConfirmation: policy.RequiresConfirmation,
		TimeoutMS: policy.TimeoutMS, RetryPolicy: stringMap(policy.RetryPolicyJSON), CancelPolicy: stringMap(policy.CancelPolicyJSON),
		ChargeMode: pricing.ChargeMode, BillingUnit: pricing.BillingUnit, PricingPolicyID: pricing.PricingPolicyID, Reason: reason,
	}
	if !dto.Allowed && dto.Reason == "" {
		dto.Reason = "policy_denied"
	}
	return dto, nil
}

func (a *App) ListBindableTools(ctx context.Context, auth accountspace.AuthContext, limit, offset int) (Page[ToolDTO], error) {
	return a.listTools(ctx, auth, admin.AdminAuth{}, "", limit, offset)
}

func (a *App) ListAdminTools(ctx context.Context, auth admin.AdminAuth, status string, limit, offset int) (Page[ToolDTO], error) {
	return a.listTools(ctx, accountspace.AuthContext{}, auth, status, limit, offset)
}

func (a *App) UpdatePolicy(ctx context.Context, auth admin.AdminAuth, toolName, toolType string, allowed *bool, riskLevel string, requiresConfirmation *bool, timeoutMS int32, retryPolicy, cancelPolicy map[string]string) (ToolDTO, error) {
	if auth.AdminID == "" {
		return ToolDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	policy, err := a.activePolicy(ctx, toolName, toolType)
	if err != nil {
		return ToolDTO{}, err
	}
	before, _ := json.Marshal(policy)
	if allowed != nil {
		policy.Allowed = *allowed
	}
	if strings.TrimSpace(riskLevel) != "" {
		policy.RiskLevel = strings.TrimSpace(riskLevel)
	}
	if requiresConfirmation != nil {
		policy.RequiresConfirmation = *requiresConfirmation
	}
	if timeoutMS > 0 {
		policy.TimeoutMS = timeoutMS
	}
	if retryPolicy != nil {
		policy.RetryPolicyJSON = mustJSON(retryPolicy)
	}
	if cancelPolicy != nil {
		policy.CancelPolicyJSON = mustJSON(cancelPolicy)
	}
	policy.ChangedByAdminID = &auth.AdminID
	policy.UpdatedAt = a.now()
	if err := a.repo.DB().WithContext(ctx).Save(&policy).Error; err != nil {
		return ToolDTO{}, err
	}
	after, _ := json.Marshal(policy)
	_ = a.writeChange(ctx, auth.AdminID, toolName, toolType, "policy.update", before, after)
	return a.toolDTO(ctx, accountspace.AuthContext{}, policy.ToolName, policy.ToolType)
}

func (a *App) UpdatePricing(ctx context.Context, auth admin.AdminAuth, toolName, toolType, chargeMode, billingUnit string, unitPoints float64, freeQuota int, minChargePoints int64) (ToolDTO, error) {
	if auth.AdminID == "" {
		return ToolDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	now := a.now()
	pricing := businesscore.ToolPricingPolicy{
		ID: security.RandomID("tprice_"), PricingPolicyID: security.RandomID("tool_price_"), ToolName: toolName, ToolType: toolType,
		ChargeMode: defaultString(chargeMode, "per_call"), BillingUnit: defaultString(billingUnit, "call"), UnitPoints: unitPoints,
		FreeQuota: freeQuota, MinChargePoints: minChargePoints, Status: activeStatus, EffectiveAt: now, ChangedByAdminID: &auth.AdminID,
		MetadataJSON: datatypes.JSON([]byte("{}")), CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.DB().WithContext(ctx).Model(&businesscore.ToolPricingPolicy{}).
		Where("tool_name = ? AND tool_type = ? AND status = ?", toolName, toolType, activeStatus).
		Updates(map[string]any{"status": "inactive", "updated_at": now}).Error; err != nil {
		return ToolDTO{}, err
	}
	if err := a.repo.DB().WithContext(ctx).Create(&pricing).Error; err != nil {
		return ToolDTO{}, err
	}
	return a.toolDTO(ctx, accountspace.AuthContext{}, toolName, toolType)
}

func (a *App) SetToolStatus(ctx context.Context, auth admin.AdminAuth, toolName, toolType, status string) (ToolDTO, error) {
	if auth.AdminID == "" {
		return ToolDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	status = defaultString(status, activeStatus)
	var definition businesscore.ToolDefinition
	if err := a.repo.DB().WithContext(ctx).Where("tool_name = ? AND tool_type = ?", toolName, toolType).First(&definition).Error; err != nil {
		return ToolDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "tool not found")
	}
	definition.Status = status
	definition.UpdatedAt = a.now()
	if err := a.repo.DB().WithContext(ctx).Save(&definition).Error; err != nil {
		return ToolDTO{}, err
	}
	return a.toolDTO(ctx, accountspace.AuthContext{}, toolName, toolType)
}

func (a *App) SaveWhitelist(ctx context.Context, auth admin.AdminAuth, toolName, toolType, scopeType, scopeID string, allowed bool, reason string) (ToolDTO, error) {
	if auth.AdminID == "" {
		return ToolDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if strings.TrimSpace(scopeType) == "" || strings.TrimSpace(scopeID) == "" {
		return ToolDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "scope_type and scope_id are required")
	}
	now := a.now()
	var reasonPtr *string
	if strings.TrimSpace(reason) != "" {
		value := strings.TrimSpace(reason)
		reasonPtr = &value
	}
	var existing businesscore.ToolWhitelistRule
	err := a.repo.DB().WithContext(ctx).Where("tool_name = ? AND tool_type = ? AND scope_type = ? AND scope_id = ?", toolName, toolType, scopeType, scopeID).First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return ToolDTO{}, err
	}
	if err == gorm.ErrRecordNotFound {
		existing = businesscore.ToolWhitelistRule{
			ID: security.RandomID("twl_"), ToolName: toolName, ToolType: toolType, ScopeType: scopeType,
			ScopeID: scopeID, CreatedAt: now,
		}
	}
	existing.Allowed = allowed
	existing.Reason = reasonPtr
	existing.Status = activeStatus
	existing.ChangedByAdminID = &auth.AdminID
	existing.UpdatedAt = now
	if err := a.repo.DB().WithContext(ctx).Save(&existing).Error; err != nil {
		return ToolDTO{}, err
	}
	return a.toolDTO(ctx, accountspace.AuthContext{}, toolName, toolType)
}

func (a *App) ImpactPreview(ctx context.Context, auth admin.AdminAuth, toolName, toolType string) (map[string]any, error) {
	if auth.AdminID == "" {
		return nil, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	var bindingCount int64
	if err := a.repo.DB().WithContext(ctx).Model(&businesscore.SkillToolBinding{}).Where("tool_name = ? AND tool_type = ?", toolName, toolType).Count(&bindingCount).Error; err != nil {
		return nil, err
	}
	return map[string]any{"tool_name": toolName, "tool_type": toolType, "affected_skill_bindings": bindingCount, "requires_review": bindingCount > 0}, nil
}

func (a *App) listTools(ctx context.Context, userAuth accountspace.AuthContext, _ admin.AdminAuth, status string, limit, offset int) (Page[ToolDTO], error) {
	limit = clampLimit(limit, 10, 100)
	db := a.repo.DB().WithContext(ctx).Order("tool_name ASC, tool_type ASC").Limit(limit).Offset(nonNegative(offset))
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var definitions []businesscore.ToolDefinition
	if err := db.Find(&definitions).Error; err != nil {
		return Page[ToolDTO]{}, err
	}
	out := make([]ToolDTO, 0, len(definitions))
	for _, definition := range definitions {
		dto, err := a.toolDTO(ctx, userAuth, definition.ToolName, definition.ToolType)
		if err == nil {
			out = append(out, dto)
		}
	}
	return Page[ToolDTO]{Items: out, Limit: limit, Offset: nonNegative(offset)}, nil
}

func (a *App) toolDTO(ctx context.Context, auth accountspace.AuthContext, toolName, toolType string) (ToolDTO, error) {
	var definition businesscore.ToolDefinition
	if err := a.repo.DB().WithContext(ctx).Where("tool_name = ? AND tool_type = ?", toolName, toolType).Order("created_at DESC").First(&definition).Error; err != nil {
		return ToolDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "tool not found")
	}
	policy, err := a.CheckToolExecutionPolicy(ctx, auth, toolName, toolType, "", nil)
	if err != nil {
		return ToolDTO{}, err
	}
	description := ""
	if definition.Description != nil {
		description = *definition.Description
	}
	return ToolDTO{
		ToolName: definition.ToolName, ToolType: definition.ToolType, DisplayName: definition.DisplayName, Description: description,
		Status: definition.Status, Version: definition.Version, Allowed: policy.Allowed, RiskLevel: policy.RiskLevel,
		RequiresConfirmation: policy.RequiresConfirmation, TimeoutMS: policy.TimeoutMS, RetryPolicy: policy.RetryPolicy,
		CancelPolicy: policy.CancelPolicy, ChargeMode: policy.ChargeMode, BillingUnit: policy.BillingUnit, PricingPolicyID: policy.PricingPolicyID,
	}, nil
}

func (a *App) activePolicy(ctx context.Context, toolName, toolType string) (businesscore.ToolPolicy, error) {
	var row businesscore.ToolPolicy
	err := a.repo.DB().WithContext(ctx).
		Where("tool_name = ? AND tool_type = ? AND policy_scope = ? AND status = ?", toolName, toolType, "global", activeStatus).
		Where("(expired_at IS NULL OR expired_at > ?)", a.now()).
		Order("effective_at DESC").
		First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return businesscore.ToolPolicy{}, bizerrors.New(bizerrors.CodeResourceNotFound, "tool policy not found")
		}
		return businesscore.ToolPolicy{}, err
	}
	return row, nil
}

func (a *App) activePricing(ctx context.Context, toolName, toolType string) businesscore.ToolPricingPolicy {
	var row businesscore.ToolPricingPolicy
	_ = a.repo.DB().WithContext(ctx).
		Where("tool_name = ? AND tool_type = ? AND status = ?", toolName, toolType, activeStatus).
		Where("(expired_at IS NULL OR expired_at > ?)", a.now()).
		Order("effective_at DESC").
		First(&row).Error
	return row
}

func (a *App) matchWhitelist(ctx context.Context, auth accountspace.AuthContext, toolName, toolType string) (businesscore.ToolWhitelistRule, bool) {
	scopes := []struct {
		scopeType string
		scopeID   string
	}{
		{"user", auth.UserID},
		{"space", auth.SpaceID},
		{"enterprise", auth.EnterpriseID},
	}
	for _, scope := range scopes {
		if scope.scopeID == "" {
			continue
		}
		var row businesscore.ToolWhitelistRule
		err := a.repo.DB().WithContext(ctx).Where("tool_name = ? AND tool_type = ? AND scope_type = ? AND scope_id = ? AND status = ?", toolName, toolType, scope.scopeType, scope.scopeID, activeStatus).First(&row).Error
		if err == nil {
			return row, true
		}
	}
	return businesscore.ToolWhitelistRule{}, false
}

func (a *App) writeChange(ctx context.Context, adminID, toolName, toolType, changeType string, before, after []byte) error {
	return a.repo.DB().WithContext(ctx).Create(&businesscore.ToolPolicyChangeRecord{
		ID: security.RandomID("tpcr_"), ToolName: toolName, ToolType: toolType, ChangeType: changeType,
		BeforeJSON: datatypes.JSON(before), AfterJSON: datatypes.JSON(after), ChangedByAdminID: &adminID, CreatedAt: a.now(),
	}).Error
}

func stringMap(raw datatypes.JSON) map[string]string {
	if len(raw) == 0 {
		return map[string]string{}
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return map[string]string{}
	}
	out := map[string]string{}
	for key, value := range values {
		switch typed := value.(type) {
		case string:
			out[key] = typed
		case float64, bool:
			encoded, _ := json.Marshal(typed)
			out[key] = string(encoded)
		}
	}
	return out
}

func mustJSON(value any) datatypes.JSON {
	if value == nil {
		return datatypes.JSON([]byte("{}"))
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte("{}"))
	}
	return datatypes.JSON(encoded)
}

func clampLimit(value, fallback, max int) int {
	if value <= 0 {
		value = fallback
	}
	if value > max {
		value = max
	}
	return value
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
