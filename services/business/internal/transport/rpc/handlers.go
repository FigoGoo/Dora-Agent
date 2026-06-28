package rpc

import (
	"context"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/asset"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetcommit"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/assetdict"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/credit"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/modelconfig"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/project"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/skillcatalog"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/toolpolicy"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/work"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
)

type Handler struct {
	Account    *accountspace.App
	Admin      *admin.App
	Project    *project.App
	Model      *modelconfig.App
	Tool       *toolpolicy.App
	Skill      *skillcatalog.App
	Dictionary *assetdict.App
	Credit     *credit.App
	Asset      *asset.App
	Commit     *assetcommit.App
	Work       *work.App
	Notify     *notification.App
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
		case *admin.App:
			h.Admin = typed
		case *toolpolicy.App:
			h.Tool = typed
		case *skillcatalog.App:
			h.Skill = typed
		case *assetdict.App:
			h.Dictionary = typed
		case *credit.App:
			h.Credit = typed
		case *asset.App:
			h.Asset = typed
		case *assetcommit.App:
			h.Commit = typed
		case *work.App:
			h.Work = typed
		case *notification.App:
			h.Notify = typed
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
	if h.Asset == nil {
		return nil, bizerrors.NotImplemented("AssetService.BatchCheckAssetAccess")
	}
	if req == nil || req.AuthContext == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context is required")
	}
	results, err := h.Asset.BatchCheckAssetAccess(ctx, authContextFromRPC(req.AuthContext), req.ProjectId, req.AssetIds, req.Purpose)
	if err != nil {
		return nil, err
	}
	out := make([]*businessagent.AssetAccessResult_, 0, len(results))
	for _, item := range results {
		out = append(out, &businessagent.AssetAccessResult_{
			AssetId: item.AssetID, Allowed: item.Allowed, Reason: item.Reason, AssetSummary: item.AssetSummary,
		})
	}
	return &businessagent.BatchCheckAssetAccessResponse{Results: out}, nil
}

func (h *Handler) PrepareGeneratedAssetObjects(ctx context.Context, req *businessagent.PrepareGeneratedAssetObjectsRequest) (*businessagent.PrepareGeneratedAssetObjectsResponse, error) {
	if h.Asset == nil {
		return nil, bizerrors.NotImplemented("AssetService.PrepareGeneratedAssetObjects")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	artifacts := make([]asset.GeneratedObjectInput, 0, len(req.Artifacts))
	for _, item := range req.Artifacts {
		artifacts = append(artifacts, asset.GeneratedObjectInput{
			ArtifactID: item.ArtifactId, ResourceType: item.ResourceType, Filename: item.Filename,
			ContentType: item.ContentType, SizeBytes: item.SizeBytes, Checksum: item.GetChecksum(), MetadataSummary: item.MetadataSummary,
		})
	}
	slots, err := h.Asset.PrepareGeneratedAssetObjects(ctx, authContextFromRPC(req.AuthContext), metaFromRPC(req.RequestMeta), req.ProjectId, req.SessionId, req.RunId, artifacts)
	if err != nil {
		return nil, err
	}
	out := make([]*businessagent.GeneratedAssetUploadSlot, 0, len(slots))
	for _, slot := range slots {
		out = append(out, &businessagent.GeneratedAssetUploadSlot{
			ArtifactId: slot.ArtifactID, Bucket: slot.Bucket, ObjectKey: slot.ObjectKey, UploadUrl: slot.UploadURL,
			UploadHeaders: slot.UploadHeaders, ExpiresAt: slot.ExpiresAt.Format(time.RFC3339Nano), MaxSizeBytes: slot.MaxSizeBytes,
		})
	}
	return &businessagent.PrepareGeneratedAssetObjectsResponse{UploadSlots: out}, nil
}

func (h *Handler) EstimateGenerationCredits(ctx context.Context, req *businessagent.EstimateGenerationCreditsRequest) (*businessagent.EstimateGenerationCreditsResponse, error) {
	if h.Credit == nil {
		return nil, bizerrors.NotImplemented("CreditService.EstimateGenerationCredits")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	items := make([]credit.ToolUsageItem, 0, len(req.ToolUsageItems))
	for _, item := range req.ToolUsageItems {
		items = append(items, toolUsageFromRPC(item))
	}
	out, err := h.Credit.EstimateGenerationCredits(ctx, credit.EstimateGenerationInput{
		Auth: authContextFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), ProjectID: req.ProjectId,
		ResourceType: req.ResourceType, ModelID: req.ModelId, PricingSnapshotID: req.PricingSnapshotId,
		Quantity: req.GetQuantity(), DurationSeconds: req.GetDurationSeconds(), ToolUsageItems: items, SafetyEvidence: req.SafetyEvidence,
	})
	if err != nil {
		return nil, err
	}
	return estimateToRPC(out), nil
}

func (h *Handler) EstimateToolCredits(ctx context.Context, req *businessagent.EstimateToolCreditsRequest) (*businessagent.EstimateToolCreditsResponse, error) {
	if h.Credit == nil {
		return nil, bizerrors.NotImplemented("CreditService.EstimateToolCredits")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	items := make([]credit.ToolUsageItem, 0, len(req.ToolUsageItems))
	for _, item := range req.ToolUsageItems {
		items = append(items, toolUsageFromRPC(item))
	}
	out, err := h.Credit.EstimateToolCredits(ctx, credit.EstimateToolInput{
		Auth: authContextFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), ProjectID: req.ProjectId,
		ToolUsageItems: items, SafetyEvidence: req.SafetyEvidence,
	})
	if err != nil {
		return nil, err
	}
	estimate := estimateToRPC(out)
	return &businessagent.EstimateToolCreditsResponse{
		EstimateId: estimate.EstimateId, EstimatePoints: estimate.EstimatePoints, AvailablePoints: estimate.AvailablePoints,
		ExpiresSoonPoints: estimate.ExpiresSoonPoints, CreditAccountScope: estimate.CreditAccountScope, CreditAccountId: estimate.CreditAccountId,
		LineItems: estimate.LineItems, ExpiresAt: estimate.ExpiresAt, Insufficient: estimate.Insufficient,
	}, nil
}

func (h *Handler) FreezeCredits(ctx context.Context, req *businessagent.FreezeCreditsRequest) (*businessagent.FreezeCreditsResponse, error) {
	if h.Credit == nil {
		return nil, bizerrors.NotImplemented("CreditService.FreezeCredits")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Credit.FreezeCredits(ctx, credit.FreezeInput{
		Auth: authContextFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), EstimateID: req.EstimateId,
		Points: req.Points, RunID: req.RunId, ConfirmationID: req.GetConfirmationId(), AccountID: req.GetAccountId(),
	})
	if err != nil {
		return nil, err
	}
	return &businessagent.FreezeCreditsResponse{FreezeId: out.FreezeID, FrozenPoints: out.FrozenPoints, ExpiresAt: out.ExpiresAt.Format(time.RFC3339Nano)}, nil
}

func (h *Handler) ChargeToolUsageCredits(ctx context.Context, req *businessagent.ChargeToolUsageCreditsRequest) (*businessagent.ChargeToolUsageCreditsResponse, error) {
	if h.Credit == nil {
		return nil, bizerrors.NotImplemented("CreditService.ChargeToolUsageCredits")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	items := make([]credit.ChargeItemInput, 0, len(req.ChargeItems))
	for _, item := range req.ChargeItems {
		items = append(items, credit.ChargeItemInput{
			EstimateItemID: item.EstimateItemId, ToolCallID: item.ToolCallId, ToolName: item.ToolName,
			ToolType: item.ToolType, BillingUnit: item.BillingUnit, ActualQuantity: item.ActualQuantity,
			ExecutionStatus: item.ExecutionStatus, MetadataSummary: item.MetadataSummary,
		})
	}
	out, err := h.Credit.ChargeToolUsageCredits(ctx, credit.ChargeToolInput{
		Auth: authContextFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), ProjectID: req.ProjectId,
		EstimateID: req.EstimateId, FreezeID: req.FreezeId, SessionID: req.SessionId, RunID: req.RunId, ChargeItems: items,
	})
	if err != nil {
		return nil, err
	}
	return &businessagent.ChargeToolUsageCreditsResponse{
		ToolChargeId: out.ToolChargeID, ChargedPoints: out.ChargedPoints, ReleasedPoints: out.ReleasedPoints,
		FreezeStatus: out.FreezeStatus, LedgerEntryIds: out.LedgerEntryIDs, ChargedLineItems: chargedItemsToRPC(out.ChargedLineItems),
	}, nil
}

func (h *Handler) ReleaseFrozenCredits(ctx context.Context, req *businessagent.ReleaseFrozenCreditsRequest) (*businessagent.ReleaseFrozenCreditsResponse, error) {
	if h.Credit == nil {
		return nil, bizerrors.NotImplemented("CreditService.ReleaseFrozenCredits")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	out, err := h.Credit.ReleaseFrozenCredits(ctx, credit.ReleaseInput{
		Auth: authContextFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), FreezeID: req.FreezeId,
		ReleasePoints: req.ReleasePoints, Reason: req.Reason, RunID: req.RunId,
	})
	if err != nil {
		return nil, err
	}
	return &businessagent.ReleaseFrozenCreditsResponse{ReleasedPoints: out.ReleasedPoints, ReleaseStatus: out.ReleaseStatus}, nil
}

func (h *Handler) CommitGeneratedAssetAndCharge(ctx context.Context, req *businessagent.CommitGeneratedAssetAndChargeRequest) (*businessagent.CommitGeneratedAssetAndChargeResponse, error) {
	if h.Commit == nil {
		return nil, bizerrors.NotImplemented("AssetCreditCommitService.CommitGeneratedAssetAndCharge")
	}
	if req == nil || req.AuthContext == nil || req.RequestMeta == nil {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "auth_context and request_meta are required")
	}
	artifacts := make([]assetcommit.CommitArtifactInput, 0, len(req.Artifacts))
	for _, item := range req.Artifacts {
		storage := item.GetStorageObjectRef()
		if storage == nil {
			return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "storage_object_ref is required")
		}
		artifacts = append(artifacts, assetcommit.CommitArtifactInput{
			ArtifactID: item.ArtifactId, ResourceType: item.ResourceType, ElementType: item.ElementType,
			ArtifactSummary: item.ArtifactSummary, ContentURIDigest: item.GetContentUriDigest(), EstimateItemID: item.GetEstimateItemId(),
			ToolName: item.GetToolName(), ToolType: item.GetToolType(), ChargeQuantity: item.GetChargeQuantity(),
			MetadataSummary: item.MetadataSummary,
			StorageObjectRef: assetcommit.StorageObjectRef{
				ObjectKey: storage.ObjectKey, Bucket: storage.Bucket, ContentType: storage.ContentType,
				SizeBytes: storage.SizeBytes, Checksum: storage.Checksum, Etag: storage.GetEtag(),
			},
		})
	}
	finals := make([]assetcommit.FinalElementInput, 0, len(req.FinalElements))
	for _, item := range req.FinalElements {
		finals = append(finals, assetcommit.FinalElementInput{
			ElementType: item.ElementType, ElementPayloadJSON: item.ElementPayloadJson,
			DisplayOrder: item.DisplayOrder, SourceToolCallID: item.GetSourceToolCallId(),
		})
	}
	out, err := h.Commit.CommitGeneratedAssetAndCharge(ctx, assetcommit.CommitInput{
		Auth: authContextFromRPC(req.AuthContext), Meta: metaFromRPC(req.RequestMeta), ProjectID: req.ProjectId,
		SessionID: req.SessionId, RunID: req.RunId, FreezeID: req.FreezeId, EstimateID: req.GetEstimateId(),
		Artifacts: artifacts, FinalElements: finals, SafetyEvidence: req.SafetyEvidence,
	})
	if err != nil {
		return nil, err
	}
	refs := make([]*businessagent.CommittedAssetRefDTO, 0, len(out.AssetRefs))
	for _, item := range out.AssetRefs {
		refs = append(refs, &businessagent.CommittedAssetRefDTO{
			AssetId: item.AssetID, SourceArtifactId: item.SourceArtifactID, ResourceType: item.ResourceType,
			AssetType: item.AssetType, Status: item.Status, PreviewUrl: optionalString(item.PreviewURL),
			ElementsSummaryJson: optionalString(item.ElementsSummaryJSON),
		})
	}
	return &businessagent.CommitGeneratedAssetAndChargeResponse{
		AssetRefs: refs, ChargedPoints: out.ChargedPoints, ReleasedPoints: out.ReleasedPoints,
		CommitStatus: out.CommitStatus, LedgerRef: optionalString(out.LedgerRef), ChargedLineItems: commitChargedItemsToRPC(out.ChargedLineItems),
	}, nil
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
		OutputElements: outputElementsToRPC(out.OutputElements),
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
		OutputElements: outputElementsToRPC(out.OutputElements),
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

func metaFromRPC(in *businessagent.RequestMeta) accountspace.RequestMeta {
	if in == nil {
		return accountspace.RequestMeta{}
	}
	return accountspace.RequestMeta{
		RequestID:      in.RequestId,
		TraceID:        in.TraceId,
		IdempotencyKey: in.GetIdempotencyKey(),
		Source:         in.Source,
	}
}

func toolUsageFromRPC(in *businessagent.ToolUsageEstimateItemInput) credit.ToolUsageItem {
	if in == nil {
		return credit.ToolUsageItem{}
	}
	return credit.ToolUsageItem{
		ToolName: in.ToolName, ToolType: in.ToolType, BillingUnit: in.BillingUnit,
		Quantity: in.Quantity, MetadataSummary: in.MetadataSummary,
	}
}

func estimateToRPC(in credit.EstimateDTO) *businessagent.EstimateGenerationCreditsResponse {
	return &businessagent.EstimateGenerationCreditsResponse{
		EstimateId: in.EstimateID, EstimatePoints: in.EstimatePoints, AvailablePoints: in.AvailablePoints,
		ExpiresSoonPoints: in.ExpiresSoonPoints, CreditAccountScope: in.CreditAccountScope, CreditAccountId: in.CreditAccountID,
		LineItems: estimateLineItemsToRPC(in.LineItems), ExpiresAt: in.ExpiresAt.Format(time.RFC3339Nano), Insufficient: in.Insufficient,
	}
}

func estimateLineItemsToRPC(items []credit.EstimateLineItemDTO) []*businessagent.CreditEstimateLineItemDTO {
	out := make([]*businessagent.CreditEstimateLineItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, &businessagent.CreditEstimateLineItemDTO{
			EstimateItemId: item.EstimateItemID, ItemType: item.ItemType, ToolName: optionalString(item.ToolName),
			ToolType: optionalString(item.ToolType), PricingPolicyId: optionalString(item.PricingPolicyID),
			ModelId: optionalString(item.ModelID), ResourceType: optionalString(item.ResourceType), BillingUnit: optionalString(item.BillingUnit),
			Quantity: optionalFloat(item.Quantity), UnitPoints: optionalFloat(item.UnitPoints), EstimatePoints: item.EstimatePoints,
			FreeReason: optionalString(item.FreeReason), MetadataSummary: item.Metadata,
		})
	}
	return out
}

func chargedItemsToRPC(items []credit.ChargedLineItemDTO) []*businessagent.ChargedLineItemDTO {
	out := make([]*businessagent.ChargedLineItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, &businessagent.ChargedLineItemDTO{
			EstimateItemId: item.EstimateItemID, ChargedPoints: item.ChargedPoints, Status: item.Status,
			AssetId: optionalString(item.AssetID), ToolCallId: optionalString(item.ToolCallID), ArtifactId: optionalString(item.ArtifactID),
		})
	}
	return out
}

func commitChargedItemsToRPC(items []assetcommit.ChargedLineItemDTO) []*businessagent.ChargedLineItemDTO {
	out := make([]*businessagent.ChargedLineItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, &businessagent.ChargedLineItemDTO{
			EstimateItemId: item.EstimateItemID, ChargedPoints: item.ChargedPoints, Status: item.Status,
			AssetId: optionalString(item.AssetID), ToolCallId: optionalString(item.ToolCallID), ArtifactId: optionalString(item.ArtifactID),
		})
	}
	return out
}

func outputElementsToRPC(items []skillcatalog.OutputElementDTO) []*businessagent.SkillOutputElementDTO {
	if len(items) == 0 {
		return nil
	}
	out := make([]*businessagent.SkillOutputElementDTO, 0, len(items))
	for _, item := range items {
		out = append(out, &businessagent.SkillOutputElementDTO{
			ElementType: item.ElementType, ElementName: item.ElementName, Required: item.Required,
			UseDraft: item.UseDraft, UseFinal: item.UseFinal, Editable: item.Editable, Referable: item.Referable,
			DisplayOrder: optionalInt32(item.DisplayOrder), DisplaySlot: optionalString(item.DisplaySlot), SchemaJson: optionalString(item.SchemaJSON),
		})
	}
	return out
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func optionalInt32(value int32) *int32 {
	if value == 0 {
		return nil
	}
	return &value
}

func optionalFloat(value float64) *float64 {
	if value == 0 {
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
