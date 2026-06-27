package workbench

import (
	"context"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
)

type StaticGateway struct {
	Auth          AuthContextDTO
	Space         SpaceContextDTO
	Access        ProjectAccessDTO
	Skills        []SkillSummaryDTO
	SkillSpec     SkillSpecDTO
	ToolPolicy    ToolExecutionPolicyDTO
	Model         ModelSummaryDTO
	ModelSnapshot ModelRuntimeSnapshotDTO
	ElementTypes  []AssetElementTypeDTO
	Err           error
}

func (g StaticGateway) ResolveAuthContextFromToken(ctx context.Context, authorization string, expectedSpaceID string, traceID string) (AuthContextDTO, SpaceContextDTO, error) {
	if g.Err != nil {
		return AuthContextDTO{}, SpaceContextDTO{}, g.Err
	}
	if authorization == "" {
		return AuthContextDTO{}, SpaceContextDTO{}, nil
	}
	auth := g.Auth
	if auth.ActorUserID == "" {
		auth.ActorUserID = "usr_1001"
	}
	if auth.LoginIdentityType == "" {
		auth.LoginIdentityType = "personal"
	}
	if auth.SpaceID == "" {
		auth.SpaceID = "sp_personal_1001"
	}
	space, err := g.ResolveCurrentSpaceContext(ctx, auth, expectedSpaceID, traceID)
	return auth, space, err
}

func (g StaticGateway) ResolveCurrentSpaceContext(ctx context.Context, auth AuthContextDTO, expectedSpaceID string, traceID string) (SpaceContextDTO, error) {
	if g.Err != nil {
		return SpaceContextDTO{}, g.Err
	}
	space := g.Space
	if space.SpaceID == "" {
		space.SpaceID = auth.SpaceID
	}
	if space.SpaceType == "" {
		space.SpaceType = "personal"
	}
	if space.CreditAccountScope == "" {
		space.CreditAccountScope = space.SpaceType
	}
	if space.CreditAccountID == "" {
		space.CreditAccountID = "credit_" + space.SpaceID
	}
	return space, nil
}

func (g StaticGateway) CheckProjectAccess(ctx context.Context, auth AuthContextDTO, projectID string, purpose businessagent.ProjectAccessPurpose, traceID string) (ProjectAccessDTO, error) {
	if g.Err != nil {
		return ProjectAccessDTO{}, g.Err
	}
	access := g.Access
	if access.ProjectStatus == "" {
		access.ProjectStatus = "active"
	}
	if access.AllowedActions == nil {
		access.AllowedActions = []string{"view", "continue_creation"}
	}
	if !access.Allowed && access.UserMessage == "" && access.ProjectStatus != "archived" {
		access.Allowed = true
		access.CreativeAllowed = true
	}
	return access, nil
}

func (g StaticGateway) ListRoutableSkills(ctx context.Context, auth AuthContextDTO, scopeFilter string, limit int, cursor string, traceID string) ([]SkillSummaryDTO, string, error) {
	if g.Err != nil {
		return nil, "", g.Err
	}
	if g.Skills != nil {
		return g.Skills, "", nil
	}
	return []SkillSummaryDTO{{SkillID: "sk_static", SkillName: "Static Skill", SkillScope: "public", Version: "1.0.0", Status: "published", RouteHints: map[string]string{"intent": "default"}}}, "", nil
}

func (g StaticGateway) GetPublishedSkillSpec(ctx context.Context, auth AuthContextDTO, skillID string, version string, traceID string) (SkillSpecDTO, error) {
	if g.Err != nil {
		return SkillSpecDTO{}, g.Err
	}
	if g.SkillSpec.SkillID != "" {
		return g.SkillSpec, nil
	}
	return SkillSpecDTO{
		SkillID: skillID, Version: version, SkillSpecJSON: `{"name":"static"}`, OutputSchemaJSON: `{"type":"object"}`,
		ToolRefs: []string{"image_generate:model_generation"}, MemoryPolicyJSON: `{"enabled":true}`,
		ConfirmationPolicyJSON: `{"requires_confirmation":false}`, ExecutionPolicySummaryJSON: `{"tool_refs":["image_generate:model_generation"]}`,
	}, nil
}

func (g StaticGateway) CheckToolExecutionPolicy(ctx context.Context, auth AuthContextDTO, toolName string, toolType string, projectID string, riskContext map[string]string, traceID string) (ToolExecutionPolicyDTO, error) {
	if g.Err != nil {
		return ToolExecutionPolicyDTO{}, g.Err
	}
	if g.ToolPolicy.RiskLevel != "" || g.ToolPolicy.TimeoutMS != 0 {
		return g.ToolPolicy, nil
	}
	return ToolExecutionPolicyDTO{Allowed: true, RiskLevel: "low", RequiresConfirmation: false, TimeoutMS: 30000}, nil
}

func (g StaticGateway) ResolveDefaultModel(ctx context.Context, auth AuthContextDTO, resourceType string, traceID string) (ModelSummaryDTO, error) {
	if g.Err != nil {
		return ModelSummaryDTO{}, g.Err
	}
	if g.Model.ModelID != "" {
		return g.Model, nil
	}
	return ModelSummaryDTO{ModelID: "mdl_static_image", DisplayName: "Static Image", IsDefault: true, PricingSnapshotID: "price_static_image", ResourceType: resourceType}, nil
}

func (g StaticGateway) ResolveGenerationModelSnapshot(ctx context.Context, auth AuthContextDTO, resourceType string, modelID string, pricingSnapshotID string, traceID string) (ModelRuntimeSnapshotDTO, error) {
	if g.Err != nil {
		return ModelRuntimeSnapshotDTO{}, g.Err
	}
	if g.ModelSnapshot.ModelID != "" {
		return g.ModelSnapshot, nil
	}
	return ModelRuntimeSnapshotDTO{ModelID: modelID, DisplayName: "Static Image", ResourceType: resourceType, PricingSnapshotID: pricingSnapshotID, ProviderRuntimeRef: "static:local", TimeoutMS: 30000, RetryPolicy: map[string]string{"max_retries": "0"}}, nil
}

func (g StaticGateway) ListAssetElementTypes(ctx context.Context, auth AuthContextDTO, pageSize int, schemaVersion string, traceID string) ([]AssetElementTypeDTO, string, error) {
	if g.Err != nil {
		return nil, "", g.Err
	}
	if g.ElementTypes != nil {
		return g.ElementTypes, "2026-06-27", nil
	}
	return []AssetElementTypeDTO{{
		ElementType: "image.primary", DisplayName: "Primary Image", Category: "image", SchemaVersion: "2026-06-27",
		SchemaHintJSON: `{"type":"object","required":["asset_id"]}`, RenderHintJSON: `{"component":"image_preview"}`,
		Active: true, SortOrder: 10, ResourceType: "image", Status: "active", UsageStage: "draft_final",
		DraftEnabled: true, FinalEnabled: true, Editable: true, Referable: true, RenderHint: "image_preview",
	}}, "2026-06-27", nil
}

func (g StaticGateway) SaveSkillTestResult(ctx context.Context, auth AuthContextDTO, req SkillTestResultRequest, traceID string) (SkillTestResultDTO, error) {
	if g.Err != nil {
		return SkillTestResultDTO{}, g.Err
	}
	return SkillTestResultDTO{TestRunID: req.TestRunID, Status: req.Status, Saved: true}, nil
}
