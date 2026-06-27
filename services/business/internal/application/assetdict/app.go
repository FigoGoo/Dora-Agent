package assetdict

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
)

const activeStatus = "active"

type App struct {
	repo *businesscore.Repository
	now  func() time.Time
}

func New(repo *businesscore.Repository) *App {
	return &App{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type AssetElementTypeDTO struct {
	ElementType    string  `json:"element_type"`
	DisplayName    string  `json:"display_name"`
	Category       string  `json:"category"`
	SchemaVersion  string  `json:"schema_version"`
	SchemaHintJSON string  `json:"schema_hint_json"`
	RenderHintJSON *string `json:"render_hint_json,omitempty"`
	Active         bool    `json:"active"`
	SortOrder      int32   `json:"sort_order"`
	ResourceType   string  `json:"resource_type"`
	Status         string  `json:"status"`
	UsageStage     string  `json:"usage_stage"`
	DraftEnabled   bool    `json:"draft_enabled"`
	FinalEnabled   bool    `json:"final_enabled"`
	Editable       bool    `json:"editable"`
	Referable      bool    `json:"referable"`
	RenderHint     *string `json:"render_hint,omitempty"`
}

type Page[T any] struct {
	Items  []T `json:"items"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

func (a *App) ListAssetElementTypes(ctx context.Context, _ accountspace.AuthContext, pageSize int, schemaVersion string) ([]AssetElementTypeDTO, string, error) {
	limit := clampLimit(pageSize, 50, 100)
	db := a.repo.DB().WithContext(ctx).Where("status = ?", activeStatus).Order("element_type ASC").Limit(limit)
	if strings.TrimSpace(schemaVersion) != "" {
		db = db.Where("schema_version = ?", strings.TrimSpace(schemaVersion))
	}
	var rows []businesscore.AssetElementType
	if err := db.Find(&rows).Error; err != nil {
		return nil, "", err
	}
	out := make([]AssetElementTypeDTO, 0, len(rows))
	version := strings.TrimSpace(schemaVersion)
	for _, row := range rows {
		if version == "" {
			version = row.SchemaVersion
		}
		out = append(out, elementDTO(row))
	}
	return out, version, nil
}

func (a *App) ListAdminElementTypes(ctx context.Context, _ admin.AdminAuth, status string, limit, offset int) (Page[AssetElementTypeDTO], error) {
	limit = clampLimit(limit, 20, 100)
	db := a.repo.DB().WithContext(ctx).Order("element_type ASC").Limit(limit).Offset(nonNegative(offset))
	if strings.TrimSpace(status) != "" {
		db = db.Where("status = ?", strings.TrimSpace(status))
	}
	var rows []businesscore.AssetElementType
	if err := db.Find(&rows).Error; err != nil {
		return Page[AssetElementTypeDTO]{}, err
	}
	out := make([]AssetElementTypeDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, elementDTO(row))
	}
	return Page[AssetElementTypeDTO]{Items: out, Limit: limit, Offset: nonNegative(offset)}, nil
}

type SaveInput struct {
	Auth          admin.AdminAuth
	ElementType   string
	DisplayName   string
	SchemaVersion string
	SchemaJSON    string
	Status        string
}

func (a *App) SaveElementType(ctx context.Context, in SaveInput) (AssetElementTypeDTO, error) {
	if in.Auth.AdminID == "" {
		return AssetElementTypeDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if strings.TrimSpace(in.ElementType) == "" || strings.TrimSpace(in.DisplayName) == "" {
		return AssetElementTypeDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "element_type and display_name are required")
	}
	schema := normalizeJSON(in.SchemaJSON, "{}")
	if !json.Valid([]byte(schema)) {
		return AssetElementTypeDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "schema_json must be json")
	}
	now := a.now()
	var existing businesscore.AssetElementType
	err := a.repo.DB().WithContext(ctx).Where("element_type = ?", in.ElementType).First(&existing).Error
	if err != nil {
		existing = businesscore.AssetElementType{ID: security.RandomID("aet_"), ElementType: in.ElementType, CreatedAt: now}
	}
	existing.DisplayName = in.DisplayName
	existing.SchemaVersion = defaultString(in.SchemaVersion, time.Now().UTC().Format("2006-01-02"))
	existing.SchemaJSON = datatypes.JSON([]byte(schema))
	existing.Status = defaultString(in.Status, activeStatus)
	existing.OperatorID = &in.Auth.AdminID
	existing.UpdatedAt = now
	if err := a.repo.DB().WithContext(ctx).Save(&existing).Error; err != nil {
		return AssetElementTypeDTO{}, err
	}
	return elementDTO(existing), nil
}

func elementDTO(row businesscore.AssetElementType) AssetElementTypeDTO {
	schemaObj := jsonObject(row.SchemaJSON)
	renderHint := ""
	if value, ok := schemaObj["render_hint"]; ok {
		encoded, _ := json.Marshal(value)
		renderHint = string(encoded)
	}
	var renderPtr *string
	if renderHint != "" {
		renderPtr = &renderHint
	}
	sortOrder := int32(numberValue(schemaObj["sort_order"], 1000))
	category, _ := schemaObj["category"].(string)
	if category == "" {
		category = categoryFromElementType(row.ElementType)
	}
	resourceType := stringValue(schemaObj["resource_type"], category)
	usageStage := stringValue(schemaObj["usage_stage"], "draft_final")
	renderHintText := stringValue(schemaObj["render_hint"], "")
	if renderHintText == "" && renderHint != "" {
		renderHintText = renderHint
	}
	var renderHintPtr *string
	if renderHintText != "" {
		renderHintPtr = &renderHintText
	}
	return AssetElementTypeDTO{
		ElementType: row.ElementType, DisplayName: row.DisplayName, Category: category, SchemaVersion: row.SchemaVersion,
		SchemaHintJSON: string(row.SchemaJSON), RenderHintJSON: renderPtr, Active: row.Status == activeStatus, SortOrder: sortOrder,
		ResourceType: resourceType, Status: row.Status, UsageStage: usageStage,
		DraftEnabled: boolValue(schemaObj["draft_enabled"], true), FinalEnabled: boolValue(schemaObj["final_enabled"], true),
		Editable: boolValue(schemaObj["editable"], true), Referable: boolValue(schemaObj["referable"], true), RenderHint: renderHintPtr,
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

func categoryFromElementType(elementType string) string {
	parts := strings.SplitN(elementType, ".", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "general"
	}
	return parts[0]
}

func normalizeJSON(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
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

func stringValue(value any, fallback string) string {
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) != "" {
			return strings.TrimSpace(typed)
		}
	case map[string]any:
		if component, ok := typed["component"].(string); ok && strings.TrimSpace(component) != "" {
			return strings.TrimSpace(component)
		}
	}
	return fallback
}

func boolValue(value any, fallback bool) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	return fallback
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
