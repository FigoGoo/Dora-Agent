package rpc

import (
	"context"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

type Handler struct {
	Account *accountspace.App
	Project *project.App
}

func NewUnimplementedHandler() *Handler {
	return &Handler{}
}

func NewHandler(accountApp *accountspace.App, projectApp *project.App) *Handler {
	return &Handler{Account: accountApp, Project: projectApp}
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
	return nil, bizerrors.NotImplemented("SkillCatalogService.ListRoutableSkills")
}

func (h *Handler) GetPublishedSkillSpec(ctx context.Context, req *businessagent.GetPublishedSkillSpecRequest) (*businessagent.SkillSpecResponse, error) {
	return nil, bizerrors.NotImplemented("SkillCatalogService.GetPublishedSkillSpec")
}

func (h *Handler) GetReviewCandidateSkillSpec(ctx context.Context, req *businessagent.GetReviewCandidateSkillSpecRequest) (*businessagent.ReviewCandidateSkillSpecResponse, error) {
	return nil, bizerrors.NotImplemented("SkillCatalogService.GetReviewCandidateSkillSpec")
}

func (h *Handler) SaveSkillTestResult_(ctx context.Context, req *businessagent.SaveSkillTestResultRequest) (*businessagent.SaveSkillTestResultResponse, error) {
	return nil, bizerrors.NotImplemented("SkillCatalogService.SaveSkillTestResult")
}

func (h *Handler) CheckToolExecutionPolicy(ctx context.Context, req *businessagent.CheckToolExecutionPolicyRequest) (*businessagent.ToolExecutionPolicyResponse, error) {
	return nil, bizerrors.NotImplemented("ToolCapabilityService.CheckToolExecutionPolicy")
}

func (h *Handler) ListAvailableGenerationModels(ctx context.Context, req *businessagent.ListAvailableGenerationModelsRequest) (*businessagent.ListAvailableGenerationModelsResponse, error) {
	return nil, bizerrors.NotImplemented("ModelConfigService.ListAvailableGenerationModels")
}

func (h *Handler) ResolveDefaultModel(ctx context.Context, req *businessagent.ResolveDefaultModelRequest) (*businessagent.ModelSummaryDTO, error) {
	return nil, bizerrors.NotImplemented("ModelConfigService.ResolveDefaultModel")
}

func (h *Handler) ResolveGenerationModelSnapshot(ctx context.Context, req *businessagent.ResolveGenerationModelSnapshotRequest) (*businessagent.ModelRuntimeSnapshotDTO, error) {
	return nil, bizerrors.NotImplemented("ModelConfigService.ResolveGenerationModelSnapshot")
}

func (h *Handler) ListAssetElementTypes(ctx context.Context, req *businessagent.ListAssetElementTypesRequest) (*businessagent.ListAssetElementTypesResponse, error) {
	return nil, bizerrors.NotImplemented("PlatformDictionaryService.ListAssetElementTypes")
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
