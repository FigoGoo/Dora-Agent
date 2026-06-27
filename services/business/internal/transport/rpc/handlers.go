package rpc

import (
	"context"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetdict"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/modelconfig"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/skillcatalog"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/toolpolicy"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

type Handler struct {
	Account    *accountspace.App
	Project    *project.App
	Model      *modelconfig.App
	Tool       *toolpolicy.App
	Skill      *skillcatalog.App
	Dictionary *assetdict.App
}

func NewUnimplementedHandler() *Handler {
	return &Handler{}
}

func NewHandler(accountApp *accountspace.App, projectApp *project.App, optionalApps ...any) *Handler {
	h := &Handler{Account: accountApp, Project: projectApp}
	for _, app := range optionalApps {
		switch typed := app.(type) {
		case *modelconfig.App:
			h.Model = typed
		case *toolpolicy.App:
			h.Tool = typed
		case *skillcatalog.App:
			h.Skill = typed
		case *assetdict.App:
			h.Dictionary = typed
		}
	}
	return h
}

func (h *Handler) ResolveCurrentSpaceContext(ctx context.Context, req *businessagent.ResolveCurrentSpaceContextRequest) (*businessagent.ResolveCurrentSpaceContextResponse, error) {
	if h.Account == nil {
		return nil, bizerrors.NotImplemented("AccountSpaceService.ResolveCurrentSpaceContext")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	auth := authContextFromRPC(req.AuthContext)
	expected := ""
	if req.ExpectedSpaceId != nil {
		expected = *req.ExpectedSpaceId
	}
	out, err := h.Account.ResolveCurrentSpaceContext(ctx, auth, expected)
	if err != nil {
		return nil, err
	}
	return &businessagent.ResolveCurrentSpaceContextResponse{
		SpaceId:            out.SpaceID,
		SpaceType:          out.SpaceType,
		EnterpriseId:       optionalString(out.EnterpriseID),
		EnterpriseRole:     optionalString(out.EnterpriseRole),
		CreditAccountScope: out.CreditAccountScope,
		CreditAccountId:    out.CreditAccountID,
		SkillScopeKeys:     out.SkillScopeKeys,
		PermissionSummary:  out.PermissionSummary,
	}, nil
}

func (h *Handler) ResolveAuthContextFromToken(ctx context.Context, req *businessagent.ResolveAuthContextFromTokenRequest) (*businessagent.ResolveAuthContextFromTokenResponse, error) {
	if h.Account == nil {
		return nil, bizerrors.NotImplemented("AccountSpaceService.ResolveAuthContextFromToken")
	}
	if req == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "request_meta is required")
	}
	auth, err := h.Account.AuthenticateToken(ctx, req.Authorization)
	if err != nil {
		return nil, err
	}
	expected := ""
	if req.ExpectedSpaceId != nil {
		expected = *req.ExpectedSpaceId
	}
	space, err := h.Account.ResolveCurrentSpaceContext(ctx, auth, expected)
	if err != nil {
		return nil, err
	}
	return &businessagent.ResolveAuthContextFromTokenResponse{
		AuthContext:  authContextToRPC(auth),
		SpaceContext: spaceContextToRPC(space),
		SessionId:    auth.SessionID,
	}, nil
}

func (h *Handler) CheckProjectAccess(ctx context.Context, req *businessagent.CheckProjectAccessRequest) (*businessagent.ProjectAccessResponse, error) {
	if h.Project == nil {
		return nil, bizerrors.NotImplemented("ProjectService.CheckProjectAccess")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	out, err := h.Project.CheckProjectAccess(ctx, authContextFromRPC(req.AuthContext), req.ProjectId, req.AccessPurpose)
	if err != nil {
		return nil, err
	}
	return &businessagent.ProjectAccessResponse{
		Allowed:         out.Allowed,
		ProjectStatus:   out.ProjectStatus,
		CreativeAllowed: out.CreativeAllowed,
		AllowedActions:  out.AllowedActions,
		UserMessage:     optionalString(out.DeniedReason),
		ProjectSummary:  out.ProjectSummary,
	}, nil
}

func (h *Handler) BatchCheckAssetAccess(ctx context.Context, req *businessagent.BatchCheckAssetAccessRequest) (*businessagent.BatchCheckAssetAccessResponse, error) {
	return nil, bizerrors.NotImplemented("AssetService.BatchCheckAssetAccess")
}

func (h *Handler) PrepareGeneratedAssetObjects(ctx context.Context, req *businessagent.PrepareGeneratedAssetObjectsRequest) (*businessagent.PrepareGeneratedAssetObjectsResponse, error) {
	return nil, bizerrors.NotImplemented("AssetService.PrepareGeneratedAssetObjects")
}

func (h *Handler) EstimateGenerationCredits(ctx context.Context, req *businessagent.EstimateGenerationCreditsRequest) (*businessagent.EstimateGenerationCreditsResponse, error) {
	return nil, bizerrors.NotImplemented("CreditService.EstimateGenerationCredits")
}

func (h *Handler) EstimateToolCredits(ctx context.Context, req *businessagent.EstimateToolCreditsRequest) (*businessagent.EstimateToolCreditsResponse, error) {
	return nil, bizerrors.NotImplemented("CreditService.EstimateToolCredits")
}

func (h *Handler) FreezeCredits(ctx context.Context, req *businessagent.FreezeCreditsRequest) (*businessagent.FreezeCreditsResponse, error) {
	return nil, bizerrors.NotImplemented("CreditService.FreezeCredits")
}

func (h *Handler) ChargeToolUsageCredits(ctx context.Context, req *businessagent.ChargeToolUsageCreditsRequest) (*businessagent.ChargeToolUsageCreditsResponse, error) {
	return nil, bizerrors.NotImplemented("CreditService.ChargeToolUsageCredits")
}

func (h *Handler) ReleaseFrozenCredits(ctx context.Context, req *businessagent.ReleaseFrozenCreditsRequest) (*businessagent.ReleaseFrozenCreditsResponse, error) {
	return nil, bizerrors.NotImplemented("CreditService.ReleaseFrozenCredits")
}

func (h *Handler) CommitGeneratedAssetAndCharge(ctx context.Context, req *businessagent.CommitGeneratedAssetAndChargeRequest) (*businessagent.CommitGeneratedAssetAndChargeResponse, error) {
	return nil, bizerrors.NotImplemented("AssetCreditCommitService.CommitGeneratedAssetAndCharge")
}

func (h *Handler) ListRoutableSkills(ctx context.Context, req *businessagent.ListRoutableSkillsRequest) (*businessagent.ListRoutableSkillsResponse, error) {
	if h.Skill == nil {
		return nil, bizerrors.NotImplemented("SkillCatalogService.ListRoutableSkills")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	skills, next, err := h.Skill.ListRoutableSkills(ctx, authContextFromRPC(req.AuthContext), req.GetSkillScopeFilter(), int(req.GetPageSize()), req.GetCursor())
	if err != nil {
		return nil, err
	}
	out := make([]*businessagent.SkillSummaryDTO, 0, len(skills))
	for _, skill := range skills {
		out = append(out, &businessagent.SkillSummaryDTO{
			SkillId: skill.SkillID, SkillName: skill.SkillName, SkillScope: skill.SkillScope,
			Version: skill.Version, Status: skill.Status, RouteHints: skill.RouteHints,
		})
	}
	return &businessagent.ListRoutableSkillsResponse{Skills: out, NextCursor: optionalString(next)}, nil
}

func (h *Handler) GetPublishedSkillSpec(ctx context.Context, req *businessagent.GetPublishedSkillSpecRequest) (*businessagent.SkillSpecResponse, error) {
	if h.Skill == nil {
		return nil, bizerrors.NotImplemented("SkillCatalogService.GetPublishedSkillSpec")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	out, err := h.Skill.GetPublishedSkillSpec(ctx, authContextFromRPC(req.AuthContext), req.SkillId, req.GetVersion())
	if err != nil {
		return nil, err
	}
	return &businessagent.SkillSpecResponse{
		SkillId: out.SkillID, Version: out.Version, SkillSpecJson: out.SkillSpecJSON, OutputSchemaJson: out.OutputSchemaJSON,
		ToolRefs: out.ToolRefs, MemoryPolicyJson: optionalString(out.MemoryPolicyJSON),
		ConfirmationPolicyJson: out.ConfirmationPolicyJSON, ExecutionPolicySummaryJson: out.ExecutionPolicySummaryJSON,
	}, nil
}

func (h *Handler) GetReviewCandidateSkillSpec(ctx context.Context, req *businessagent.GetReviewCandidateSkillSpecRequest) (*businessagent.ReviewCandidateSkillSpecResponse, error) {
	if h.Skill == nil {
		return nil, bizerrors.NotImplemented("SkillCatalogService.GetReviewCandidateSkillSpec")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	out, err := h.Skill.GetReviewCandidateSkillSpec(ctx, authContextFromRPC(req.AuthContext), req.SkillId, req.VersionId, req.GetTestCaseId())
	if err != nil {
		return nil, err
	}
	return &businessagent.ReviewCandidateSkillSpecResponse{
		SkillId: out.SkillID, VersionId: out.VersionID, SkillSpecJson: out.SkillSpecJSON, InputSchemaJson: out.InputSchemaJSON,
		OutputSchemaJson: out.OutputSchemaJSON, ToolRefs: out.ToolRefs, MemoryPolicyJson: out.MemoryPolicyJSON,
		ConfirmationPolicyJson: out.ConfirmationPolicyJSON, TestInputJson: optionalString(out.TestInputJSON), ExpectedElementsJson: optionalString(out.ExpectedElementsJSON),
	}, nil
}

func (h *Handler) SaveSkillTestResult_(ctx context.Context, req *businessagent.SaveSkillTestResultRequest) (*businessagent.SaveSkillTestResultResponse, error) {
	if h.Skill == nil {
		return nil, bizerrors.NotImplemented("SkillCatalogService.SaveSkillTestResult")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Skill.SaveSkillTestResult(ctx, authContextFromRPC(req.AuthContext), req.SkillId, req.VersionId, req.TestRunId, req.GetTestCaseId(), value(req.RequestMeta.IdempotencyKey), req.Status, req.ActualElementsJson, req.GetErrorCode(), req.GetErrorSummary(), req.GetSafetyEvidenceJson(), req.AgentTraceId)
	if err != nil {
		return nil, err
	}
	return &businessagent.SaveSkillTestResultResponse{TestRunId: out.TestRunID, Status: out.Status, Saved: out.Saved}, nil
}

func (h *Handler) CheckToolExecutionPolicy(ctx context.Context, req *businessagent.CheckToolExecutionPolicyRequest) (*businessagent.ToolExecutionPolicyResponse, error) {
	if h.Tool == nil {
		return nil, bizerrors.NotImplemented("ToolCapabilityService.CheckToolExecutionPolicy")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	out, err := h.Tool.CheckToolExecutionPolicy(ctx, authContextFromRPC(req.AuthContext), req.ToolName, req.ToolType, req.ProjectId, req.RiskContext)
	if err != nil {
		return nil, err
	}
	return &businessagent.ToolExecutionPolicyResponse{
		Allowed: out.Allowed, RiskLevel: out.RiskLevel, RequiresConfirmation: out.RequiresConfirmation,
		TimeoutMs: out.TimeoutMS, RetryPolicy: out.RetryPolicy, CancelPolicy: out.CancelPolicy,
	}, nil
}

func (h *Handler) ListAvailableGenerationModels(ctx context.Context, req *businessagent.ListAvailableGenerationModelsRequest) (*businessagent.ListAvailableGenerationModelsResponse, error) {
	if h.Model == nil {
		return nil, bizerrors.NotImplemented("ModelConfigService.ListAvailableGenerationModels")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	models, next, err := h.Model.ListAvailableGenerationModels(ctx, authContextFromRPC(req.AuthContext), req.ResourceType, int(req.GetPageSize()), req.GetCursor())
	if err != nil {
		return nil, err
	}
	out := make([]*businessagent.ModelSummaryDTO, 0, len(models))
	for _, model := range models {
		out = append(out, modelSummaryToRPC(model))
	}
	return &businessagent.ListAvailableGenerationModelsResponse{Models: out, NextCursor: optionalString(next)}, nil
}

func (h *Handler) ResolveDefaultModel(ctx context.Context, req *businessagent.ResolveDefaultModelRequest) (*businessagent.ModelSummaryDTO, error) {
	if h.Model == nil {
		return nil, bizerrors.NotImplemented("ModelConfigService.ResolveDefaultModel")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	out, err := h.Model.ResolveDefaultModel(ctx, authContextFromRPC(req.AuthContext), req.ResourceType)
	if err != nil {
		return nil, err
	}
	return modelSummaryToRPC(out), nil
}

func (h *Handler) ResolveGenerationModelSnapshot(ctx context.Context, req *businessagent.ResolveGenerationModelSnapshotRequest) (*businessagent.ModelRuntimeSnapshotDTO, error) {
	if h.Model == nil {
		return nil, bizerrors.NotImplemented("ModelConfigService.ResolveGenerationModelSnapshot")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	out, err := h.Model.ResolveGenerationModelSnapshot(ctx, authContextFromRPC(req.AuthContext), req.ResourceType, req.ModelId, req.PricingSnapshotId)
	if err != nil {
		return nil, err
	}
	return &businessagent.ModelRuntimeSnapshotDTO{
		ModelId: out.ModelID, DisplayName: out.DisplayName, ResourceType: out.ResourceType, PricingSnapshotId: out.PricingSnapshotID,
		ProviderRuntimeRef: out.ProviderRuntimeRef, TimeoutMs: out.TimeoutMS, RetryPolicy: out.RetryPolicy, RuntimeParameters: out.RuntimeParameters,
	}, nil
}

func (h *Handler) ListAssetElementTypes(ctx context.Context, req *businessagent.ListAssetElementTypesRequest) (*businessagent.ListAssetElementTypesResponse, error) {
	if h.Dictionary == nil {
		return nil, bizerrors.NotImplemented("PlatformDictionaryService.ListAssetElementTypes")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	items, version, err := h.Dictionary.ListAssetElementTypes(ctx, authContextFromRPC(req.AuthContext), int(req.GetPageSize()), req.GetSchemaVersion())
	if err != nil {
		return nil, err
	}
	out := make([]*businessagent.AssetElementTypeDTO, 0, len(items))
	for _, item := range items {
		out = append(out, &businessagent.AssetElementTypeDTO{
			ElementType: item.ElementType, DisplayName: item.DisplayName, Category: item.Category, SchemaVersion: item.SchemaVersion,
			SchemaHintJson: item.SchemaHintJSON, RenderHintJson: item.RenderHintJSON, Active: item.Active, SortOrder: item.SortOrder,
			ResourceType: item.ResourceType, Status: item.Status, UsageStage: item.UsageStage, DraftEnabled: item.DraftEnabled,
			FinalEnabled: item.FinalEnabled, Editable: item.Editable, Referable: item.Referable, RenderHint: item.RenderHint,
		})
	}
	return &businessagent.ListAssetElementTypesResponse{ElementTypes: out, SchemaVersion: version}, nil
}

func authContextFromRPC(in *businessagent.AuthContext) accountspace.AuthContext {
	loginType := "personal"
	if in.LoginIdentityType == businessagent.LoginIdentityType_ENTERPRISE_MEMBER {
		loginType = "enterprise_member"
	}
	if in.LoginIdentityType == businessagent.LoginIdentityType_ADMIN {
		loginType = "admin"
	}
	return accountspace.AuthContext{
		UserID: in.ActorUserId, LoginIdentityType: loginType, SpaceID: value(in.SpaceId),
		EnterpriseID: value(in.EnterpriseId), EnterpriseRole: value(in.EnterpriseRole),
	}
}

func authContextToRPC(in accountspace.AuthContext) *businessagent.AuthContext {
	return &businessagent.AuthContext{
		ActorUserId:       in.UserID,
		LoginIdentityType: loginIdentityToRPC(in.LoginIdentityType),
		SpaceId:           optionalString(in.SpaceID),
		EnterpriseId:      optionalString(in.EnterpriseID),
		EnterpriseRole:    optionalString(in.EnterpriseRole),
	}
}

func loginIdentityToRPC(value string) businessagent.LoginIdentityType {
	switch value {
	case "enterprise_member":
		return businessagent.LoginIdentityType_ENTERPRISE_MEMBER
	case "admin":
		return businessagent.LoginIdentityType_ADMIN
	default:
		return businessagent.LoginIdentityType_PERSONAL
	}
}

func spaceContextToRPC(out accountspace.SpaceContextDTO) *businessagent.ResolveCurrentSpaceContextResponse {
	return &businessagent.ResolveCurrentSpaceContextResponse{
		SpaceId: out.SpaceID, SpaceType: out.SpaceType, EnterpriseId: optionalString(out.EnterpriseID),
		EnterpriseRole: optionalString(out.EnterpriseRole), CreditAccountScope: out.CreditAccountScope,
		CreditAccountId: out.CreditAccountID, SkillScopeKeys: out.SkillScopeKeys, PermissionSummary: out.PermissionSummary,
	}
}

func modelSummaryToRPC(in modelconfig.ModelSummaryDTO) *businessagent.ModelSummaryDTO {
	return &businessagent.ModelSummaryDTO{
		ModelId: in.ModelID, DisplayName: in.DisplayName, IsDefault: in.IsDefault,
		PricingSnapshotId: in.PricingSnapshotID, ResourceType: in.ResourceType,
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
