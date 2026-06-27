package rpc

import (
	"context"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

type UnimplementedHandler struct{}

func NewUnimplementedHandler() *UnimplementedHandler {
	return &UnimplementedHandler{}
}

func (h *UnimplementedHandler) ResolveCurrentSpaceContext(ctx context.Context, req *businessagent.ResolveCurrentSpaceContextRequest) (*businessagent.ResolveCurrentSpaceContextResponse, error) {
	return nil, bizerrors.NotImplemented("AccountSpaceService.ResolveCurrentSpaceContext")
}

func (h *UnimplementedHandler) CheckProjectAccess(ctx context.Context, req *businessagent.CheckProjectAccessRequest) (*businessagent.ProjectAccessResponse, error) {
	return nil, bizerrors.NotImplemented("ProjectService.CheckProjectAccess")
}

func (h *UnimplementedHandler) BatchCheckAssetAccess(ctx context.Context, req *businessagent.BatchCheckAssetAccessRequest) (*businessagent.BatchCheckAssetAccessResponse, error) {
	return nil, bizerrors.NotImplemented("AssetService.BatchCheckAssetAccess")
}

func (h *UnimplementedHandler) PrepareGeneratedAssetObjects(ctx context.Context, req *businessagent.PrepareGeneratedAssetObjectsRequest) (*businessagent.PrepareGeneratedAssetObjectsResponse, error) {
	return nil, bizerrors.NotImplemented("AssetService.PrepareGeneratedAssetObjects")
}

func (h *UnimplementedHandler) EstimateGenerationCredits(ctx context.Context, req *businessagent.EstimateGenerationCreditsRequest) (*businessagent.EstimateGenerationCreditsResponse, error) {
	return nil, bizerrors.NotImplemented("CreditService.EstimateGenerationCredits")
}

func (h *UnimplementedHandler) EstimateToolCredits(ctx context.Context, req *businessagent.EstimateToolCreditsRequest) (*businessagent.EstimateToolCreditsResponse, error) {
	return nil, bizerrors.NotImplemented("CreditService.EstimateToolCredits")
}

func (h *UnimplementedHandler) FreezeCredits(ctx context.Context, req *businessagent.FreezeCreditsRequest) (*businessagent.FreezeCreditsResponse, error) {
	return nil, bizerrors.NotImplemented("CreditService.FreezeCredits")
}

func (h *UnimplementedHandler) ChargeToolUsageCredits(ctx context.Context, req *businessagent.ChargeToolUsageCreditsRequest) (*businessagent.ChargeToolUsageCreditsResponse, error) {
	return nil, bizerrors.NotImplemented("CreditService.ChargeToolUsageCredits")
}

func (h *UnimplementedHandler) ReleaseFrozenCredits(ctx context.Context, req *businessagent.ReleaseFrozenCreditsRequest) (*businessagent.ReleaseFrozenCreditsResponse, error) {
	return nil, bizerrors.NotImplemented("CreditService.ReleaseFrozenCredits")
}

func (h *UnimplementedHandler) CommitGeneratedAssetAndCharge(ctx context.Context, req *businessagent.CommitGeneratedAssetAndChargeRequest) (*businessagent.CommitGeneratedAssetAndChargeResponse, error) {
	return nil, bizerrors.NotImplemented("AssetCreditCommitService.CommitGeneratedAssetAndCharge")
}

func (h *UnimplementedHandler) ListRoutableSkills(ctx context.Context, req *businessagent.ListRoutableSkillsRequest) (*businessagent.ListRoutableSkillsResponse, error) {
	return nil, bizerrors.NotImplemented("SkillCatalogService.ListRoutableSkills")
}

func (h *UnimplementedHandler) GetPublishedSkillSpec(ctx context.Context, req *businessagent.GetPublishedSkillSpecRequest) (*businessagent.SkillSpecResponse, error) {
	return nil, bizerrors.NotImplemented("SkillCatalogService.GetPublishedSkillSpec")
}

func (h *UnimplementedHandler) GetReviewCandidateSkillSpec(ctx context.Context, req *businessagent.GetReviewCandidateSkillSpecRequest) (*businessagent.ReviewCandidateSkillSpecResponse, error) {
	return nil, bizerrors.NotImplemented("SkillCatalogService.GetReviewCandidateSkillSpec")
}

func (h *UnimplementedHandler) SaveSkillTestResult_(ctx context.Context, req *businessagent.SaveSkillTestResultRequest) (*businessagent.SaveSkillTestResultResponse, error) {
	return nil, bizerrors.NotImplemented("SkillCatalogService.SaveSkillTestResult")
}

func (h *UnimplementedHandler) CheckToolExecutionPolicy(ctx context.Context, req *businessagent.CheckToolExecutionPolicyRequest) (*businessagent.ToolExecutionPolicyResponse, error) {
	return nil, bizerrors.NotImplemented("ToolCapabilityService.CheckToolExecutionPolicy")
}

func (h *UnimplementedHandler) ListAvailableGenerationModels(ctx context.Context, req *businessagent.ListAvailableGenerationModelsRequest) (*businessagent.ListAvailableGenerationModelsResponse, error) {
	return nil, bizerrors.NotImplemented("ModelConfigService.ListAvailableGenerationModels")
}

func (h *UnimplementedHandler) ResolveDefaultModel(ctx context.Context, req *businessagent.ResolveDefaultModelRequest) (*businessagent.ModelSummaryDTO, error) {
	return nil, bizerrors.NotImplemented("ModelConfigService.ResolveDefaultModel")
}

func (h *UnimplementedHandler) ResolveGenerationModelSnapshot(ctx context.Context, req *businessagent.ResolveGenerationModelSnapshotRequest) (*businessagent.ModelRuntimeSnapshotDTO, error) {
	return nil, bizerrors.NotImplemented("ModelConfigService.ResolveGenerationModelSnapshot")
}

func (h *UnimplementedHandler) ListAssetElementTypes(ctx context.Context, req *businessagent.ListAssetElementTypesRequest) (*businessagent.ListAssetElementTypesResponse, error) {
	return nil, bizerrors.NotImplemented("PlatformDictionaryService.ListAssetElementTypes")
}
