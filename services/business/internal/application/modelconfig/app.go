package modelconfig

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

type ModelSummaryDTO struct {
	ModelID           string `json:"model_id"`
	DisplayName       string `json:"display_name"`
	IsDefault         bool   `json:"is_default"`
	PricingSnapshotID string `json:"pricing_snapshot_id"`
	ResourceType      string `json:"resource_type"`
}

type ModelRuntimeSnapshotDTO struct {
	ModelID            string            `json:"model_id"`
	DisplayName        string            `json:"display_name"`
	ResourceType       string            `json:"resource_type"`
	PricingSnapshotID  string            `json:"pricing_snapshot_id"`
	ProviderRuntimeRef string            `json:"provider_runtime_ref"`
	TimeoutMS          int32             `json:"timeout_ms"`
	RetryPolicy        map[string]string `json:"retry_policy,omitempty"`
	RuntimeParameters  map[string]string `json:"runtime_parameters,omitempty"`
}

type ProviderDTO struct {
	ProviderID      string         `json:"provider_id"`
	ProviderCode    string         `json:"provider_code"`
	ProviderName    string         `json:"provider_name,omitempty"`
	DisplayName     string         `json:"display_name"`
	ProviderType    string         `json:"provider_type"`
	Status          string         `json:"status"`
	SecretRefStatus string         `json:"secret_ref_status,omitempty"`
	BaseURL         string         `json:"base_url,omitempty"`
	Config          map[string]any `json:"config,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type ModelAdminDTO struct {
	ModelID            string         `json:"model_id"`
	ProviderID         string         `json:"provider_id"`
	ModelCode          string         `json:"model_code"`
	DisplayName        string         `json:"display_name"`
	ResourceType       string         `json:"resource_type"`
	CapabilityTags     []string       `json:"capability_tags,omitempty"`
	Status             string         `json:"status"`
	RouteConfig        map[string]any `json:"route_config,omitempty"`
	PricingSnapshotID  string         `json:"pricing_snapshot_id,omitempty"`
	IsDefault          bool           `json:"is_default"`
	DefaultForResource bool           `json:"default_for_resource"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

type Page[T any] struct {
	Items  []T `json:"items"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func (a *App) ListAvailableGenerationModels(ctx context.Context, _ accountspace.AuthContext, resourceType string, limit int, cursor string) ([]ModelSummaryDTO, string, error) {
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		return nil, "", bizerrors.New(bizerrors.CodeInvalidArgument, "resource_type is required")
	}
	offset := cursorOffset(cursor)
	limit = clampLimit(limit, 10, 50)
	var models []businesscore.Model
	if err := a.repo.DB().WithContext(ctx).
		Where("resource_type = ? AND status = ?", resourceType, activeStatus).
		Order("display_name ASC, id ASC").
		Limit(limit + 1).
		Offset(offset).
		Find(&models).Error; err != nil {
		return nil, "", err
	}
	defaultModel, _ := a.activeDefault(ctx, resourceType)
	out := make([]ModelSummaryDTO, 0, min(len(models), limit))
	for i, model := range models {
		if i >= limit {
			break
		}
		price, err := a.activePrice(ctx, model.ID, resourceType, "")
		if err != nil {
			continue
		}
		out = append(out, ModelSummaryDTO{
			ModelID: model.ID, DisplayName: model.DisplayName, IsDefault: defaultModel.ModelID == model.ID,
			PricingSnapshotID: price.PricingSnapshotID, ResourceType: model.ResourceType,
		})
	}
	nextCursor := ""
	if len(models) > limit {
		nextCursor = encodeCursor(offset + limit)
	}
	return out, nextCursor, nil
}

func (a *App) ResolveDefaultModel(ctx context.Context, auth accountspace.AuthContext, resourceType string) (ModelSummaryDTO, error) {
	defaultModel, err := a.activeDefault(ctx, strings.TrimSpace(resourceType))
	if err != nil {
		return ModelSummaryDTO{}, err
	}
	models, _, err := a.ListAvailableGenerationModels(ctx, auth, defaultModel.ResourceType, 50, "")
	if err != nil {
		return ModelSummaryDTO{}, err
	}
	for _, item := range models {
		if item.ModelID == defaultModel.ModelID && item.PricingSnapshotID == defaultModel.PricingSnapshotID {
			return item, nil
		}
	}
	return ModelSummaryDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "default model is not active")
}

func (a *App) ResolveGenerationModelSnapshot(ctx context.Context, _ accountspace.AuthContext, resourceType, modelID, pricingSnapshotID string) (ModelRuntimeSnapshotDTO, error) {
	resourceType = strings.TrimSpace(resourceType)
	modelID = strings.TrimSpace(modelID)
	pricingSnapshotID = strings.TrimSpace(pricingSnapshotID)
	if resourceType == "" || modelID == "" || pricingSnapshotID == "" {
		return ModelRuntimeSnapshotDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "resource_type, model_id and pricing_snapshot_id are required")
	}
	var model businesscore.Model
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND resource_type = ? AND status = ?", modelID, resourceType, activeStatus).First(&model).Error; err != nil {
		return ModelRuntimeSnapshotDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "model is not available")
	}
	var provider businesscore.ModelProvider
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND status = ?", model.ProviderID, activeStatus).First(&provider).Error; err != nil {
		return ModelRuntimeSnapshotDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "model provider is not available")
	}
	if model.CredentialID != nil && *model.CredentialID != "" {
		var credential businesscore.ModelProviderCredential
		if err := a.repo.DB().WithContext(ctx).Where("id = ? AND status = ?", *model.CredentialID, activeStatus).First(&credential).Error; err != nil {
			return ModelRuntimeSnapshotDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "model credential is not available")
		}
	}
	if _, err := a.activePrice(ctx, model.ID, resourceType, pricingSnapshotID); err != nil {
		return ModelRuntimeSnapshotDTO{}, err
	}
	providerConfig := jsonObject(provider.ConfigJSON)
	routeConfig := jsonObject(model.RouteConfigJSON)
	timeoutMS := int32(numberValue(providerConfig["timeout_ms"], 30000))
	if timeoutMS <= 0 {
		timeoutMS = 30000
	}
	return ModelRuntimeSnapshotDTO{
		ModelID:            model.ID,
		DisplayName:        model.DisplayName,
		ResourceType:       model.ResourceType,
		PricingSnapshotID:  pricingSnapshotID,
		ProviderRuntimeRef: provider.ProviderCode + ":" + model.ModelCode,
		TimeoutMS:          timeoutMS,
		RetryPolicy:        map[string]string{"max_retries": "1"},
		RuntimeParameters:  stringMap(routeConfig),
	}, nil
}

func (a *App) ListProviders(ctx context.Context, _ admin.AdminAuth, status string, limit, offset int) (Page[ProviderDTO], error) {
	limit = clampLimit(limit, 10, 100)
	db := a.repo.DB().WithContext(ctx).Order("display_name ASC, id ASC").Limit(limit).Offset(nonNegative(offset))
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var rows []businesscore.ModelProvider
	if err := db.Find(&rows).Error; err != nil {
		return Page[ProviderDTO]{}, err
	}
	out := make([]ProviderDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, providerDTO(row))
	}
	return Page[ProviderDTO]{Items: out, Limit: limit, Offset: nonNegative(offset)}, nil
}

type SaveProviderInput struct {
	Auth         admin.AdminAuth
	ProviderID   string
	ProviderCode string
	DisplayName  string
	ProviderType string
	Status       string
	BaseURL      string
	Config       map[string]any
}

func (a *App) SaveProvider(ctx context.Context, in SaveProviderInput) (ProviderDTO, error) {
	if in.Auth.AdminID == "" {
		return ProviderDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	code := strings.TrimSpace(in.ProviderCode)
	name := strings.TrimSpace(in.DisplayName)
	if code == "" || name == "" {
		return ProviderDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "provider_code and display_name are required")
	}
	now := a.now()
	id := strings.TrimSpace(in.ProviderID)
	if id == "" {
		id = security.RandomID("mp_")
	}
	status := normalizeStatus(in.Status, activeStatus)
	var baseURL *string
	if strings.TrimSpace(in.BaseURL) != "" {
		value := strings.TrimSpace(in.BaseURL)
		baseURL = &value
	}
	adminID := in.Auth.AdminID
	row := businesscore.ModelProvider{
		ID: id, ProviderCode: code, DisplayName: name, ProviderType: defaultString(in.ProviderType, "openai_compatible"),
		Status: status, BaseURL: baseURL, ConfigJSON: mustJSON(in.Config), CreatedByAdminID: &adminID,
		CreatedBy: stringPtr(adminID), UpdatedBy: stringPtr(adminID), CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.DB().WithContext(ctx).Save(&row).Error; err != nil {
		return ProviderDTO{}, err
	}
	return providerDTO(row), nil
}

func (a *App) SetProviderStatus(ctx context.Context, auth admin.AdminAuth, providerID, status string) (ProviderDTO, error) {
	if auth.AdminID == "" {
		return ProviderDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	status = normalizeStatus(status, activeStatus)
	var row businesscore.ModelProvider
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", providerID).First(&row).Error; err != nil {
		return ProviderDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "provider not found")
	}
	row.Status = status
	row.UpdatedBy = stringPtr(auth.AdminID)
	row.UpdatedAt = a.now()
	if err := a.repo.DB().WithContext(ctx).Save(&row).Error; err != nil {
		return ProviderDTO{}, err
	}
	return providerDTO(row), nil
}

func (a *App) RecordConnectivityTest(ctx context.Context, auth admin.AdminAuth, providerID, modelID string) (map[string]any, error) {
	if auth.AdminID == "" {
		return nil, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	var provider businesscore.ModelProvider
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", providerID).First(&provider).Error; err != nil {
		return nil, bizerrors.New(bizerrors.CodeResourceNotFound, "provider not found")
	}
	var modelPtr *string
	if strings.TrimSpace(modelID) != "" {
		modelPtr = &modelID
	}
	latency := 1
	traceID := "business-http"
	row := businesscore.ModelConnectivityTest{
		ID: security.RandomID("mct_"), ProviderID: providerID, ModelID: modelPtr, Status: "passed",
		LatencyMS: &latency, TestedByAdminID: &auth.AdminID, TraceID: &traceID, CreatedAt: a.now(),
	}
	if err := a.repo.DB().WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return map[string]any{"test_id": row.ID, "provider_id": provider.ID, "status": row.Status, "latency_ms": latency}, nil
}

func (a *App) ListModels(ctx context.Context, _ admin.AdminAuth, resourceType, status string, limit, offset int) (Page[ModelAdminDTO], error) {
	limit = clampLimit(limit, 10, 100)
	db := a.repo.DB().WithContext(ctx).Order("display_name ASC, id ASC").Limit(limit).Offset(nonNegative(offset))
	if strings.TrimSpace(resourceType) != "" {
		db = db.Where("resource_type = ?", strings.TrimSpace(resourceType))
	}
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var rows []businesscore.Model
	if err := db.Find(&rows).Error; err != nil {
		return Page[ModelAdminDTO]{}, err
	}
	out := make([]ModelAdminDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, a.modelAdminDTO(ctx, row))
	}
	return Page[ModelAdminDTO]{Items: out, Limit: limit, Offset: nonNegative(offset)}, nil
}

type SaveModelInput struct {
	Auth              admin.AdminAuth
	ModelID           string
	ProviderID        string
	ModelCode         string
	DisplayName       string
	ResourceType      string
	PricingSnapshotID string
	BillingUnit       string
	UnitPoints        float64
	MinChargePoints   int64
	Status            string
	CapabilityTags    []string
	RouteConfig       map[string]any
	CredentialID      string
}

func (a *App) SaveModel(ctx context.Context, in SaveModelInput) (ModelAdminDTO, error) {
	if in.Auth.AdminID == "" {
		return ModelAdminDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if strings.TrimSpace(in.ProviderID) == "" || strings.TrimSpace(in.ModelCode) == "" || strings.TrimSpace(in.ResourceType) == "" {
		return ModelAdminDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "provider_id, model_code and resource_type are required")
	}
	now := a.now()
	id := strings.TrimSpace(in.ModelID)
	if id == "" {
		id = security.RandomID("mdl_")
	}
	var credentialID *string
	if strings.TrimSpace(in.CredentialID) != "" {
		value := strings.TrimSpace(in.CredentialID)
		credentialID = &value
	}
	adminID := in.Auth.AdminID
	row := businesscore.Model{
		ID: id, ProviderID: in.ProviderID, ModelCode: in.ModelCode, DisplayName: defaultString(in.DisplayName, in.ModelCode),
		ResourceType: in.ResourceType, CapabilityTags: mustJSON(in.CapabilityTags), Status: normalizeStatus(in.Status, activeStatus),
		CredentialID: credentialID, RouteConfigJSON: mustJSON(in.RouteConfig), CreatedByAdminID: &adminID,
		CreatedBy: stringPtr(adminID), UpdatedBy: stringPtr(adminID), CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.DB().WithContext(ctx).Save(&row).Error; err != nil {
		return ModelAdminDTO{}, err
	}
	if err := a.saveModelPrice(ctx, in, row.ID, row.ResourceType, now); err != nil {
		return ModelAdminDTO{}, err
	}
	return a.modelAdminDTO(ctx, row), nil
}

func (a *App) saveModelPrice(ctx context.Context, in SaveModelInput, modelID string, resourceType string, now time.Time) error {
	if strings.TrimSpace(in.BillingUnit) == "" && in.UnitPoints == 0 && strings.TrimSpace(in.PricingSnapshotID) == "" {
		return nil
	}
	if strings.TrimSpace(in.BillingUnit) == "" {
		return bizerrors.New(bizerrors.CodeInvalidArgument, "billing_unit is required when saving model price")
	}
	if in.UnitPoints <= 0 {
		return bizerrors.New(bizerrors.CodeInvalidArgument, "unit_points must be greater than 0 when saving model price")
	}
	pricingSnapshotID := strings.TrimSpace(in.PricingSnapshotID)
	if pricingSnapshotID == "" {
		pricingSnapshotID = security.RandomID("price_")
	}
	adminID := in.Auth.AdminID
	return a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&businesscore.ModelPrice{}).
			Where("model_id = ? AND resource_type = ? AND status = ? AND expired_at IS NULL", modelID, resourceType, activeStatus).
			Updates(map[string]any{"status": "expired", "expired_at": now, "updated_by": adminID}).Error; err != nil {
			return err
		}
		row := businesscore.ModelPrice{
			ID: security.RandomID("mprice_"), PricingSnapshotID: pricingSnapshotID, ModelID: modelID,
			ResourceType: resourceType, BillingUnit: strings.TrimSpace(in.BillingUnit), UnitPoints: in.UnitPoints,
			MinChargePoints: in.MinChargePoints, Status: activeStatus, EffectiveAt: now,
			CreatedByAdminID: &adminID, CreatedBy: stringPtr(adminID), UpdatedBy: stringPtr(adminID), CreatedAt: now,
		}
		return tx.Create(&row).Error
	})
}

func (a *App) SetDefaultModel(ctx context.Context, auth admin.AdminAuth, resourceType, modelID, pricingSnapshotID string) (ModelSummaryDTO, error) {
	if auth.AdminID == "" {
		return ModelSummaryDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if strings.TrimSpace(pricingSnapshotID) == "" {
		price, err := a.activePrice(ctx, strings.TrimSpace(modelID), strings.TrimSpace(resourceType), "")
		if err != nil {
			return ModelSummaryDTO{}, err
		}
		pricingSnapshotID = price.PricingSnapshotID
	}
	if _, err := a.ResolveGenerationModelSnapshot(ctx, accountspace.AuthContext{}, resourceType, modelID, pricingSnapshotID); err != nil {
		return ModelSummaryDTO{}, err
	}
	now := a.now()
	tx := a.repo.DB().WithContext(ctx).Begin()
	if err := tx.Model(&businesscore.DefaultModel{}).Where("resource_type = ? AND scope = ? AND status = ?", resourceType, "global", activeStatus).Updates(map[string]any{"status": "inactive", "updated_at": now, "updated_by": auth.AdminID}).Error; err != nil {
		tx.Rollback()
		return ModelSummaryDTO{}, err
	}
	row := businesscore.DefaultModel{
		ID: security.RandomID("dm_"), ResourceType: resourceType, ModelID: modelID, PricingSnapshotID: pricingSnapshotID,
		Scope: "global", Status: activeStatus, CreatedByAdminID: &auth.AdminID,
		CreatedBy: stringPtr(auth.AdminID), UpdatedBy: stringPtr(auth.AdminID), CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&row).Error; err != nil {
		tx.Rollback()
		return ModelSummaryDTO{}, err
	}
	if err := tx.Commit().Error; err != nil {
		return ModelSummaryDTO{}, err
	}
	return a.ResolveDefaultModel(ctx, accountspace.AuthContext{}, resourceType)
}

func (a *App) SetModelStatus(ctx context.Context, auth admin.AdminAuth, modelID, status string) (ModelAdminDTO, error) {
	if auth.AdminID == "" {
		return ModelAdminDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	var row businesscore.Model
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", modelID).First(&row).Error; err != nil {
		return ModelAdminDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "model not found")
	}
	row.Status = normalizeStatus(status, activeStatus)
	row.UpdatedBy = stringPtr(auth.AdminID)
	row.UpdatedAt = a.now()
	if err := a.repo.DB().WithContext(ctx).Save(&row).Error; err != nil {
		return ModelAdminDTO{}, err
	}
	return a.modelAdminDTO(ctx, row), nil
}

func (a *App) activeDefault(ctx context.Context, resourceType string) (businesscore.DefaultModel, error) {
	if strings.TrimSpace(resourceType) == "" {
		return businesscore.DefaultModel{}, bizerrors.New(bizerrors.CodeInvalidArgument, "resource_type is required")
	}
	var row businesscore.DefaultModel
	if err := a.repo.DB().WithContext(ctx).Where("resource_type = ? AND scope = ? AND status = ?", resourceType, "global", activeStatus).First(&row).Error; err != nil {
		return businesscore.DefaultModel{}, bizerrors.New(bizerrors.CodeResourceNotFound, "default model not found")
	}
	return row, nil
}

func (a *App) activePrice(ctx context.Context, modelID, resourceType, pricingSnapshotID string) (businesscore.ModelPrice, error) {
	db := a.repo.DB().WithContext(ctx).Where("model_id = ? AND resource_type = ? AND status = ?", modelID, resourceType, activeStatus)
	if pricingSnapshotID != "" {
		db = db.Where("pricing_snapshot_id = ?", pricingSnapshotID)
	}
	var row businesscore.ModelPrice
	err := db.Where("(expired_at IS NULL OR expired_at > ?)", a.now()).Order("effective_at DESC").First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return businesscore.ModelPrice{}, bizerrors.New(bizerrors.CodeResourceNotFound, "pricing snapshot not found")
		}
		return businesscore.ModelPrice{}, err
	}
	return row, nil
}

func (a *App) modelAdminDTO(ctx context.Context, row businesscore.Model) ModelAdminDTO {
	defaultModel, _ := a.activeDefault(ctx, row.ResourceType)
	price, _ := a.activePrice(ctx, row.ID, row.ResourceType, "")
	return ModelAdminDTO{
		ModelID: row.ID, ProviderID: row.ProviderID, ModelCode: row.ModelCode, DisplayName: row.DisplayName,
		ResourceType: row.ResourceType, CapabilityTags: stringSlice(row.CapabilityTags), Status: row.Status,
		RouteConfig: jsonObject(row.RouteConfigJSON), PricingSnapshotID: price.PricingSnapshotID,
		IsDefault: defaultModel.ModelID == row.ID, DefaultForResource: defaultModel.ModelID == row.ID, UpdatedAt: row.UpdatedAt,
	}
}

func providerDTO(row businesscore.ModelProvider) ProviderDTO {
	baseURL := ""
	if row.BaseURL != nil {
		baseURL = *row.BaseURL
	}
	config := jsonObject(row.ConfigJSON)
	secretRefStatus := ""
	if _, ok := config["secret_key_ref"].(string); ok {
		secretRefStatus = "configured"
	}
	return ProviderDTO{
		ProviderID: row.ID, ProviderCode: row.ProviderCode, ProviderName: row.DisplayName, DisplayName: row.DisplayName,
		ProviderType: row.ProviderType, Status: row.Status, SecretRefStatus: secretRefStatus, BaseURL: baseURL, Config: config, UpdatedAt: row.UpdatedAt,
	}
}

func jsonObject(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func stringMap(values map[string]any) map[string]string {
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

func stringSlice(raw datatypes.JSON) []string {
	var out []string
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil {
		return nil
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

func numberValue(value any, fallback int) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return fallback
	}
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

func cursorOffset(cursor string) int {
	var offset int
	if cursor == "" || json.Unmarshal([]byte(cursor), &offset) != nil || offset < 0 {
		return 0
	}
	return offset
}

func encodeCursor(offset int) string {
	encoded, _ := json.Marshal(offset)
	return string(encoded)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func normalizeStatus(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
