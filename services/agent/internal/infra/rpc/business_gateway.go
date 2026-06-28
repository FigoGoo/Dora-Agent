package rpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/tracectx"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/accountspaceservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/assetcreditcommitservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/assetservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/creditservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/modelconfigservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/platformdictionaryservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/projectservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/skillcatalogservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/toolcapabilityservice"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/config"
	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/client/callopt"
	"github.com/cloudwego/kitex/pkg/transmeta"
	etcd "github.com/kitex-contrib/registry-etcd"
)

type BusinessGateway struct {
	account accountspaceservice.Client
	project projectservice.Client
	skill   skillcatalogservice.Client
	tool    toolcapabilityservice.Client
	model   modelconfigservice.Client
	dict    platformdictionaryservice.Client
	credit  creditservice.Client
	asset   assetservice.Client
	commit  assetcreditcommitservice.Client
	timeout time.Duration
}

func NewBusinessGateway(cfg config.AgentConfig) (*BusinessGateway, error) {
	opts := []client.Option{client.WithMetaHandler(transmeta.MetainfoClientHandler)}
	if strings.ToLower(strings.TrimSpace(cfg.KitexRegistry)) == "etcd" {
		resolver, err := etcd.NewEtcdResolver(cfg.EtcdEndpoints)
		if err != nil {
			return nil, fmt.Errorf("create etcd resolver: %w", err)
		}
		opts = append(opts, client.WithResolver(resolver))
	}
	accountClient, err := accountspaceservice.NewClient(cfg.BusinessServiceName, opts...)
	if err != nil {
		return nil, fmt.Errorf("create accountspace rpc client: %w", err)
	}
	projectClient, err := projectservice.NewClient(cfg.BusinessServiceName, opts...)
	if err != nil {
		return nil, fmt.Errorf("create project rpc client: %w", err)
	}
	skillClient, err := skillcatalogservice.NewClient(cfg.BusinessServiceName, opts...)
	if err != nil {
		return nil, fmt.Errorf("create skill rpc client: %w", err)
	}
	toolClient, err := toolcapabilityservice.NewClient(cfg.BusinessServiceName, opts...)
	if err != nil {
		return nil, fmt.Errorf("create tool rpc client: %w", err)
	}
	modelClient, err := modelconfigservice.NewClient(cfg.BusinessServiceName, opts...)
	if err != nil {
		return nil, fmt.Errorf("create model rpc client: %w", err)
	}
	dictClient, err := platformdictionaryservice.NewClient(cfg.BusinessServiceName, opts...)
	if err != nil {
		return nil, fmt.Errorf("create platform dictionary rpc client: %w", err)
	}
	creditClient, err := creditservice.NewClient(cfg.BusinessServiceName, opts...)
	if err != nil {
		return nil, fmt.Errorf("create credit rpc client: %w", err)
	}
	assetClient, err := assetservice.NewClient(cfg.BusinessServiceName, opts...)
	if err != nil {
		return nil, fmt.Errorf("create asset rpc client: %w", err)
	}
	commitClient, err := assetcreditcommitservice.NewClient(cfg.BusinessServiceName, opts...)
	if err != nil {
		return nil, fmt.Errorf("create asset commit rpc client: %w", err)
	}
	return &BusinessGateway{
		account: accountClient, project: projectClient, skill: skillClient, tool: toolClient, model: modelClient, dict: dictClient,
		credit: creditClient, asset: assetClient, commit: commitClient, timeout: cfg.KitexTimeout,
	}, nil
}

func (g *BusinessGateway) callContext(ctx context.Context, traceID string) (context.Context, context.CancelFunc) {
	if tracectx.TraceID(ctx) == "" {
		ctx = tracectx.WithTraceID(ctx, traceID)
	}
	ctx = tracectx.InjectMetainfo(ctx)
	return context.WithTimeout(ctx, g.timeout)
}

func (g *BusinessGateway) ResolveAuthContextFromToken(ctx context.Context, authorization string, expectedSpaceID string, traceID string) (workbench.AuthContextDTO, workbench.SpaceContextDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	resp, err := g.account.ResolveAuthContextFromToken(callCtx, &businessagent.ResolveAuthContextFromTokenRequest{
		Authorization:   authorization,
		RequestMeta:     rpcMeta(traceID),
		ExpectedSpaceId: optionalString(expectedSpaceID),
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.AuthContextDTO{}, workbench.SpaceContextDTO{}, err
	}
	return authFromRPC(resp.AuthContext), spaceFromRPC(resp.SpaceContext), nil
}

func (g *BusinessGateway) ResolveCurrentSpaceContext(ctx context.Context, auth workbench.AuthContextDTO, expectedSpaceID string, traceID string) (workbench.SpaceContextDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	req := &businessagent.ResolveCurrentSpaceContextRequest{
		AuthContext:     rpcAuth(auth),
		RequestMeta:     rpcMeta(traceID),
		ExpectedSpaceId: optionalString(expectedSpaceID),
	}
	resp, err := g.account.ResolveCurrentSpaceContext(callCtx, req, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.SpaceContextDTO{}, err
	}
	return spaceFromRPC(resp), nil
}

func (g *BusinessGateway) CheckProjectAccess(ctx context.Context, auth workbench.AuthContextDTO, projectID string, purpose businessagent.ProjectAccessPurpose, traceID string) (workbench.ProjectAccessDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	resp, err := g.project.CheckProjectAccess(callCtx, &businessagent.CheckProjectAccessRequest{
		AuthContext:   rpcAuth(auth),
		RequestMeta:   rpcMeta(traceID),
		ProjectId:     projectID,
		AccessPurpose: purpose,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.ProjectAccessDTO{}, err
	}
	return workbench.ProjectAccessDTO{
		Allowed: resp.Allowed, ProjectStatus: resp.ProjectStatus, CreativeAllowed: resp.CreativeAllowed, AllowedActions: resp.AllowedActions,
		UserMessage: value(resp.UserMessage), ProjectSummary: resp.ProjectSummary,
	}, nil
}

func (g *BusinessGateway) ListRoutableSkills(ctx context.Context, auth workbench.AuthContextDTO, scopeFilter string, limit int, cursor string, traceID string) ([]workbench.SkillSummaryDTO, string, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	var pageSize *int32
	if limit > 0 {
		value := int32(limit)
		pageSize = &value
	}
	resp, err := g.skill.ListRoutableSkills(callCtx, &businessagent.ListRoutableSkillsRequest{
		AuthContext: rpcAuth(auth), RequestMeta: rpcMeta(traceID), SkillScopeFilter: optionalString(scopeFilter), PageSize: pageSize, Cursor: optionalString(cursor),
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return nil, "", err
	}
	out := make([]workbench.SkillSummaryDTO, 0, len(resp.Skills))
	for _, item := range resp.Skills {
		out = append(out, workbench.SkillSummaryDTO{
			SkillID: item.SkillId, SkillName: item.SkillName, SkillScope: item.SkillScope,
			Version: item.Version, Status: item.Status, RouteHints: item.RouteHints,
		})
	}
	return out, value(resp.NextCursor), nil
}

func (g *BusinessGateway) GetPublishedSkillSpec(ctx context.Context, auth workbench.AuthContextDTO, skillID string, version string, traceID string) (workbench.SkillSpecDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	resp, err := g.skill.GetPublishedSkillSpec(callCtx, &businessagent.GetPublishedSkillSpecRequest{
		AuthContext: rpcAuth(auth), RequestMeta: rpcMeta(traceID), SkillId: skillID, Version: optionalString(version),
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.SkillSpecDTO{}, err
	}
	return workbench.SkillSpecDTO{
		SkillID: resp.SkillId, Version: resp.Version, SkillSpecJSON: resp.SkillSpecJson, OutputSchemaJSON: resp.OutputSchemaJson,
		ToolRefs: resp.ToolRefs, MemoryPolicyJSON: value(resp.MemoryPolicyJson), ConfirmationPolicyJSON: resp.ConfirmationPolicyJson,
		ExecutionPolicySummaryJSON: resp.ExecutionPolicySummaryJson, OutputElements: outputElementsFromRPC(resp.OutputElements),
	}, nil
}

func (g *BusinessGateway) GetReviewCandidateSkillSpec(ctx context.Context, auth workbench.AuthContextDTO, skillID string, versionID string, testCaseID string, testRunID string, traceID string) (workbench.ReviewCandidateSkillSpecDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	resp, err := g.skill.GetReviewCandidateSkillSpec(callCtx, &businessagent.GetReviewCandidateSkillSpecRequest{
		AuthContext: rpcAuth(auth), RequestMeta: rpcMeta(traceID), SkillId: skillID, VersionId: versionID,
		TestCaseId: optionalString(testCaseID), TestRunId: optionalString(testRunID),
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.ReviewCandidateSkillSpecDTO{}, err
	}
	return workbench.ReviewCandidateSkillSpecDTO{
		SkillID: resp.SkillId, VersionID: resp.VersionId, SkillSpecJSON: resp.SkillSpecJson,
		InputSchemaJSON: resp.InputSchemaJson, OutputSchemaJSON: resp.OutputSchemaJson, ToolRefs: resp.ToolRefs,
		MemoryPolicyJSON: resp.MemoryPolicyJson, ConfirmationPolicyJSON: resp.ConfirmationPolicyJson,
		TestInputJSON: value(resp.TestInputJson), ExpectedElementsJSON: value(resp.ExpectedElementsJson),
		OutputElements: outputElementsFromRPC(resp.OutputElements),
	}, nil
}

func (g *BusinessGateway) CheckToolExecutionPolicy(ctx context.Context, auth workbench.AuthContextDTO, toolName string, toolType string, projectID string, riskContext map[string]string, traceID string) (workbench.ToolExecutionPolicyDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	resp, err := g.tool.CheckToolExecutionPolicy(callCtx, &businessagent.CheckToolExecutionPolicyRequest{
		AuthContext: rpcAuth(auth), RequestMeta: rpcMeta(traceID), ToolName: toolName, ToolType: toolType, ProjectId: projectID, RiskContext: riskContext,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.ToolExecutionPolicyDTO{}, err
	}
	return workbench.ToolExecutionPolicyDTO{
		Allowed: resp.Allowed, RiskLevel: resp.RiskLevel, RequiresConfirmation: resp.RequiresConfirmation,
		TimeoutMS: resp.TimeoutMs, RetryPolicy: resp.RetryPolicy, CancelPolicy: resp.CancelPolicy,
	}, nil
}

func (g *BusinessGateway) ListAvailableGenerationModels(ctx context.Context, auth workbench.AuthContextDTO, resourceType string, limit int, cursor string, traceID string) ([]workbench.ModelSummaryDTO, string, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	var pageSize *int32
	if limit > 0 {
		value := int32(limit)
		pageSize = &value
	}
	resp, err := g.model.ListAvailableGenerationModels(callCtx, &businessagent.ListAvailableGenerationModelsRequest{
		AuthContext: rpcAuth(auth), RequestMeta: rpcMeta(traceID), ResourceType: resourceType, PageSize: pageSize, Cursor: optionalString(cursor),
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return nil, "", err
	}
	out := make([]workbench.ModelSummaryDTO, 0, len(resp.Models))
	for _, item := range resp.Models {
		out = append(out, modelSummaryFromRPC(item))
	}
	return out, value(resp.NextCursor), nil
}

func (g *BusinessGateway) ResolveDefaultModel(ctx context.Context, auth workbench.AuthContextDTO, resourceType string, traceID string) (workbench.ModelSummaryDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	resp, err := g.model.ResolveDefaultModel(callCtx, &businessagent.ResolveDefaultModelRequest{
		AuthContext: rpcAuth(auth), RequestMeta: rpcMeta(traceID), ResourceType: resourceType,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.ModelSummaryDTO{}, err
	}
	return modelSummaryFromRPC(resp), nil
}

func (g *BusinessGateway) ResolveGenerationModelSnapshot(ctx context.Context, auth workbench.AuthContextDTO, resourceType string, modelID string, pricingSnapshotID string, traceID string) (workbench.ModelRuntimeSnapshotDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	resp, err := g.model.ResolveGenerationModelSnapshot(callCtx, &businessagent.ResolveGenerationModelSnapshotRequest{
		AuthContext: rpcAuth(auth), RequestMeta: rpcMeta(traceID), ResourceType: resourceType, ModelId: modelID, PricingSnapshotId: pricingSnapshotID,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.ModelRuntimeSnapshotDTO{}, err
	}
	return workbench.ModelRuntimeSnapshotDTO{
		ModelID: resp.ModelId, DisplayName: resp.DisplayName, ResourceType: resp.ResourceType, PricingSnapshotID: resp.PricingSnapshotId,
		ProviderRuntimeRef: resp.ProviderRuntimeRef, TimeoutMS: resp.TimeoutMs, RetryPolicy: resp.RetryPolicy, RuntimeParameters: resp.RuntimeParameters,
	}, nil
}

func (g *BusinessGateway) ListAssetElementTypes(ctx context.Context, auth workbench.AuthContextDTO, pageSize int, schemaVersion string, traceID string) ([]workbench.AssetElementTypeDTO, string, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	var size *int32
	if pageSize > 0 {
		value := int32(pageSize)
		size = &value
	}
	resp, err := g.dict.ListAssetElementTypes(callCtx, &businessagent.ListAssetElementTypesRequest{
		AuthContext: rpcAuth(auth), RequestMeta: rpcMeta(traceID), PageSize: size, SchemaVersion: optionalString(schemaVersion),
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return nil, "", err
	}
	out := make([]workbench.AssetElementTypeDTO, 0, len(resp.ElementTypes))
	for _, item := range resp.ElementTypes {
		out = append(out, workbench.AssetElementTypeDTO{
			ElementType: item.ElementType, DisplayName: item.DisplayName, Category: item.Category, SchemaVersion: item.SchemaVersion,
			SchemaHintJSON: item.SchemaHintJson, RenderHintJSON: value(item.RenderHintJson), Active: item.Active, SortOrder: item.SortOrder,
			ResourceType: item.ResourceType, Status: item.Status, UsageStage: item.UsageStage, DraftEnabled: item.DraftEnabled,
			FinalEnabled: item.FinalEnabled, Editable: item.Editable, Referable: item.Referable, RenderHint: value(item.RenderHint),
		})
	}
	return out, resp.SchemaVersion, nil
}

func (g *BusinessGateway) SaveSkillTestResult(ctx context.Context, auth workbench.AuthContextDTO, req workbench.SkillTestResultRequest, traceID string) (workbench.SkillTestResultDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	meta := rpcMeta(traceID)
	meta.IdempotencyKey = optionalString(req.IdempotencyKey)
	resp, err := g.skill.SaveSkillTestResult_(callCtx, &businessagent.SaveSkillTestResultRequest{
		AuthContext: rpcAuth(auth), RequestMeta: meta, SkillId: req.SkillID, VersionId: req.VersionID,
		TestRunId: req.TestRunID, TestCaseId: optionalString(req.TestCaseID), Status: req.Status, ActualElementsJson: req.ActualElementsJSON,
		ErrorCode: optionalString(req.ErrorCode), ErrorSummary: optionalString(req.ErrorSummary), SafetyEvidenceJson: optionalString(req.SafetyEvidenceJSON), AgentTraceId: req.AgentTraceID,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.SkillTestResultDTO{}, err
	}
	return workbench.SkillTestResultDTO{TestRunID: resp.TestRunId, Status: resp.Status, Saved: resp.Saved}, nil
}

func (g *BusinessGateway) BatchCheckAssetAccess(ctx context.Context, auth workbench.AuthContextDTO, req workbench.BatchCheckAssetAccessRequest, traceID string) ([]workbench.AssetAccessResultDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	resp, err := g.asset.BatchCheckAssetAccess(callCtx, &businessagent.BatchCheckAssetAccessRequest{
		AuthContext: rpcAuth(auth), RequestMeta: rpcMeta(traceID), ProjectId: req.ProjectID, AssetIds: req.AssetIDs, Purpose: req.Purpose,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return nil, err
	}
	out := make([]workbench.AssetAccessResultDTO, 0, len(resp.Results))
	for _, item := range resp.Results {
		out = append(out, workbench.AssetAccessResultDTO{AssetID: item.AssetId, Allowed: item.Allowed, Reason: item.Reason, AssetSummary: item.AssetSummary})
	}
	return out, nil
}

func (g *BusinessGateway) EstimateGenerationCredits(ctx context.Context, auth workbench.AuthContextDTO, req workbench.EstimateGenerationCreditsRequest, traceID string) (workbench.CreditEstimateDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	meta := rpcMeta(traceID)
	meta.IdempotencyKey = optionalString(req.IdempotencyKey)
	items := make([]*businessagent.ToolUsageEstimateItemInput, 0, len(req.ToolUsageItems))
	for _, item := range req.ToolUsageItems {
		items = append(items, &businessagent.ToolUsageEstimateItemInput{
			ToolName: item.ToolName, ToolType: item.ToolType, BillingUnit: item.BillingUnit,
			Quantity: item.Quantity, MetadataSummary: item.MetadataSummary,
		})
	}
	resp, err := g.credit.EstimateGenerationCredits(callCtx, &businessagent.EstimateGenerationCreditsRequest{
		AuthContext: rpcAuth(auth), RequestMeta: meta, ProjectId: req.ProjectID, ResourceType: req.ResourceType,
		ModelId: req.ModelID, PricingSnapshotId: req.PricingSnapshotID, Quantity: optionalInt32(req.Quantity),
		DurationSeconds: optionalInt32(req.DurationSeconds), ToolUsageItems: items, SafetyEvidence: req.SafetyEvidence,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.CreditEstimateDTO{}, err
	}
	return estimateFromRPC(resp, req.PricingSnapshotID), nil
}

func (g *BusinessGateway) EstimateToolCredits(ctx context.Context, auth workbench.AuthContextDTO, req workbench.EstimateToolCreditsRequest, traceID string) (workbench.CreditEstimateDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	meta := rpcMeta(traceID)
	meta.IdempotencyKey = optionalString(req.IdempotencyKey)
	items := make([]*businessagent.ToolUsageEstimateItemInput, 0, len(req.ToolUsageItems))
	for _, item := range req.ToolUsageItems {
		items = append(items, &businessagent.ToolUsageEstimateItemInput{
			ToolName: item.ToolName, ToolType: item.ToolType, BillingUnit: item.BillingUnit,
			Quantity: item.Quantity, MetadataSummary: item.MetadataSummary,
		})
	}
	resp, err := g.credit.EstimateToolCredits(callCtx, &businessagent.EstimateToolCreditsRequest{
		AuthContext: rpcAuth(auth), RequestMeta: meta, ProjectId: req.ProjectID, ToolUsageItems: items, SafetyEvidence: req.SafetyEvidence,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.CreditEstimateDTO{}, err
	}
	return estimateToolFromRPC(resp), nil
}

func (g *BusinessGateway) FreezeCredits(ctx context.Context, auth workbench.AuthContextDTO, req workbench.FreezeCreditsRequest, traceID string) (workbench.FreezeCreditsDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	meta := rpcMeta(traceID)
	meta.IdempotencyKey = optionalString(req.IdempotencyKey)
	resp, err := g.credit.FreezeCredits(callCtx, &businessagent.FreezeCreditsRequest{
		AuthContext: rpcAuth(auth), RequestMeta: meta, EstimateId: req.EstimateID, Points: req.Points,
		RunId: req.RunID, ConfirmationId: optionalString(req.ConfirmationID), AccountId: optionalString(req.AccountID),
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.FreezeCreditsDTO{}, err
	}
	return workbench.FreezeCreditsDTO{FreezeID: resp.FreezeId, FrozenPoints: resp.FrozenPoints, ExpiresAt: resp.ExpiresAt}, nil
}

func (g *BusinessGateway) ChargeToolUsageCredits(ctx context.Context, auth workbench.AuthContextDTO, req workbench.ChargeToolUsageCreditsRequest, traceID string) (workbench.ToolChargeDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	meta := rpcMeta(traceID)
	meta.IdempotencyKey = optionalString(req.IdempotencyKey)
	items := make([]*businessagent.ToolChargeItemInput, 0, len(req.ChargeItems))
	for _, item := range req.ChargeItems {
		items = append(items, &businessagent.ToolChargeItemInput{
			EstimateItemId: item.EstimateItemID, ToolCallId: item.ToolCallID, ToolName: item.ToolName, ToolType: item.ToolType,
			BillingUnit: item.BillingUnit, ActualQuantity: item.ActualQuantity, ExecutionStatus: item.ExecutionStatus,
			MetadataSummary: item.MetadataSummary,
		})
	}
	resp, err := g.credit.ChargeToolUsageCredits(callCtx, &businessagent.ChargeToolUsageCreditsRequest{
		AuthContext: rpcAuth(auth), RequestMeta: meta, ProjectId: req.ProjectID, EstimateId: req.EstimateID, FreezeId: req.FreezeID,
		SessionId: req.SessionID, RunId: req.RunID, ChargeItems: items,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.ToolChargeDTO{}, err
	}
	lines := make([]workbench.ChargedLineItemDTO, 0, len(resp.ChargedLineItems))
	for _, item := range resp.ChargedLineItems {
		lines = append(lines, workbench.ChargedLineItemDTO{
			EstimateItemID: item.EstimateItemId, ChargedPoints: item.ChargedPoints, Status: item.Status,
			AssetID: value(item.AssetId), ToolCallID: value(item.ToolCallId), ArtifactID: value(item.ArtifactId),
		})
	}
	return workbench.ToolChargeDTO{
		ToolChargeID: resp.ToolChargeId, ChargedPoints: resp.ChargedPoints, ReleasedPoints: resp.ReleasedPoints,
		FreezeStatus: resp.FreezeStatus, LedgerEntryIDs: resp.LedgerEntryIds, ChargedLineItems: lines,
	}, nil
}

func (g *BusinessGateway) ReleaseFrozenCredits(ctx context.Context, auth workbench.AuthContextDTO, req workbench.ReleaseFrozenCreditsRequest, traceID string) (workbench.ReleaseCreditsDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	meta := rpcMeta(traceID)
	meta.IdempotencyKey = optionalString(req.IdempotencyKey)
	resp, err := g.credit.ReleaseFrozenCredits(callCtx, &businessagent.ReleaseFrozenCreditsRequest{
		AuthContext: rpcAuth(auth), RequestMeta: meta, FreezeId: req.FreezeID, ReleasePoints: req.ReleasePoints, Reason: req.Reason, RunId: req.RunID,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.ReleaseCreditsDTO{}, err
	}
	return workbench.ReleaseCreditsDTO{ReleasedPoints: resp.ReleasedPoints, ReleaseStatus: resp.ReleaseStatus}, nil
}

func (g *BusinessGateway) PrepareGeneratedAssetObjects(ctx context.Context, auth workbench.AuthContextDTO, req workbench.PrepareGeneratedAssetObjectsRequest, traceID string) ([]workbench.GeneratedUploadSlotDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	meta := rpcMeta(traceID)
	meta.IdempotencyKey = optionalString(req.IdempotencyKey)
	artifacts := make([]*businessagent.GeneratedAssetObjectInput, 0, len(req.Artifacts))
	for _, item := range req.Artifacts {
		artifacts = append(artifacts, &businessagent.GeneratedAssetObjectInput{
			ArtifactId: item.ArtifactID, ResourceType: item.ResourceType, Filename: item.Filename,
			ContentType: item.ContentType, SizeBytes: item.SizeBytes, Checksum: optionalString(item.Checksum),
			MetadataSummary: item.MetadataSummary,
		})
	}
	resp, err := g.asset.PrepareGeneratedAssetObjects(callCtx, &businessagent.PrepareGeneratedAssetObjectsRequest{
		AuthContext: rpcAuth(auth), RequestMeta: meta, ProjectId: req.ProjectID, SessionId: req.SessionID, RunId: req.RunID, Artifacts: artifacts,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return nil, err
	}
	out := make([]workbench.GeneratedUploadSlotDTO, 0, len(resp.UploadSlots))
	for _, item := range resp.UploadSlots {
		out = append(out, workbench.GeneratedUploadSlotDTO{
			ArtifactID: item.ArtifactId, Bucket: item.Bucket, ObjectKey: item.ObjectKey, UploadURL: item.UploadUrl,
			UploadHeaders: item.UploadHeaders, ExpiresAt: item.ExpiresAt, MaxSizeBytes: item.MaxSizeBytes,
		})
	}
	return out, nil
}

func (g *BusinessGateway) CommitGeneratedAssetAndCharge(ctx context.Context, auth workbench.AuthContextDTO, req workbench.CommitGeneratedAssetAndChargeRequest, traceID string) (workbench.AssetCommitDTO, error) {
	callCtx, cancel := g.callContext(ctx, traceID)
	defer cancel()
	meta := rpcMeta(traceID)
	meta.IdempotencyKey = optionalString(req.IdempotencyKey)
	artifacts := make([]*businessagent.CommitArtifactDTO, 0, len(req.Artifacts))
	for _, item := range req.Artifacts {
		artifacts = append(artifacts, &businessagent.CommitArtifactDTO{
			ArtifactId: item.ArtifactID, ResourceType: item.ResourceType, ElementType: item.ElementType,
			ArtifactSummary: item.ArtifactSummary, ContentUriDigest: optionalString(item.ContentURIDigest),
			EstimateItemId: optionalString(item.EstimateItemID), ToolName: optionalString(item.ToolName), ToolType: optionalString(item.ToolType),
			ChargeQuantity: optionalInt64(item.ChargeQuantity), MetadataSummary: item.MetadataSummary,
			StorageObjectRef: &businessagent.GeneratedStorageObjectRef{
				ObjectKey: item.StorageObjectRef.ObjectKey, Bucket: item.StorageObjectRef.Bucket, ContentType: item.StorageObjectRef.ContentType,
				SizeBytes: item.StorageObjectRef.SizeBytes, Checksum: item.StorageObjectRef.Checksum, Etag: optionalString(item.StorageObjectRef.Etag),
			},
		})
	}
	finals := make([]*businessagent.GeneratedAssetElementInput, 0, len(req.FinalElements))
	for _, item := range req.FinalElements {
		finals = append(finals, &businessagent.GeneratedAssetElementInput{
			ElementType: item.ElementType, ElementPayloadJson: item.ElementPayloadJSON, DisplayOrder: item.DisplayOrder,
			SourceToolCallId: optionalString(item.SourceToolCallID),
		})
	}
	resp, err := g.commit.CommitGeneratedAssetAndCharge(callCtx, &businessagent.CommitGeneratedAssetAndChargeRequest{
		AuthContext: rpcAuth(auth), RequestMeta: meta, ProjectId: req.ProjectID, SessionId: req.SessionID, RunId: req.RunID,
		FreezeId: req.FreezeID, Artifacts: artifacts, FinalElements: finals, SafetyEvidence: req.SafetyEvidence, EstimateId: optionalString(req.EstimateID),
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.AssetCommitDTO{}, err
	}
	return commitFromRPC(resp), nil
}

func rpcAuth(auth workbench.AuthContextDTO) *businessagent.AuthContext {
	loginType := businessagent.LoginIdentityType_PERSONAL
	switch strings.ToLower(auth.LoginIdentityType) {
	case "enterprise_member", "enterprise":
		loginType = businessagent.LoginIdentityType_ENTERPRISE_MEMBER
	case "admin":
		loginType = businessagent.LoginIdentityType_ADMIN
	}
	return &businessagent.AuthContext{
		ActorUserId:       auth.ActorUserID,
		LoginIdentityType: loginType,
		SpaceId:           optionalString(auth.SpaceID),
		EnterpriseId:      optionalString(auth.EnterpriseID),
		EnterpriseRole:    optionalString(auth.EnterpriseRole),
		AdminId:           optionalString(auth.AdminID),
	}
}

func authFromRPC(auth *businessagent.AuthContext) workbench.AuthContextDTO {
	if auth == nil {
		return workbench.AuthContextDTO{}
	}
	loginType := "personal"
	if auth.LoginIdentityType == businessagent.LoginIdentityType_ENTERPRISE_MEMBER {
		loginType = "enterprise_member"
	}
	if auth.LoginIdentityType == businessagent.LoginIdentityType_ADMIN {
		loginType = "admin"
	}
	return workbench.AuthContextDTO{
		ActorUserID: auth.ActorUserId, LoginIdentityType: loginType, SpaceID: value(auth.SpaceId),
		EnterpriseID: value(auth.EnterpriseId), EnterpriseRole: value(auth.EnterpriseRole), AdminID: value(auth.AdminId),
	}
}

func spaceFromRPC(resp *businessagent.ResolveCurrentSpaceContextResponse) workbench.SpaceContextDTO {
	if resp == nil {
		return workbench.SpaceContextDTO{}
	}
	return workbench.SpaceContextDTO{
		SpaceID: resp.SpaceId, SpaceType: resp.SpaceType, EnterpriseID: value(resp.EnterpriseId), EnterpriseRole: value(resp.EnterpriseRole),
		CreditAccountScope: resp.CreditAccountScope, CreditAccountID: resp.CreditAccountId, SkillScopeKeys: resp.SkillScopeKeys, PermissionSummary: resp.PermissionSummary,
	}
}

func modelSummaryFromRPC(resp *businessagent.ModelSummaryDTO) workbench.ModelSummaryDTO {
	if resp == nil {
		return workbench.ModelSummaryDTO{}
	}
	return workbench.ModelSummaryDTO{
		ModelID: resp.ModelId, DisplayName: resp.DisplayName, IsDefault: resp.IsDefault,
		PricingSnapshotID: resp.PricingSnapshotId, ResourceType: resp.ResourceType,
	}
}

func outputElementsFromRPC(items []*businessagent.SkillOutputElementDTO) []workbench.SkillOutputElementDTO {
	if len(items) == 0 {
		return nil
	}
	out := make([]workbench.SkillOutputElementDTO, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out = append(out, workbench.SkillOutputElementDTO{
			ElementType: item.ElementType, ElementName: item.ElementName, Required: item.Required,
			UseDraft: item.UseDraft, UseFinal: item.UseFinal, Editable: item.Editable, Referable: item.Referable,
			DisplayOrder: item.GetDisplayOrder(), DisplaySlot: item.GetDisplaySlot(), SchemaJSON: item.GetSchemaJson(),
		})
	}
	return out
}

func estimateFromRPC(resp *businessagent.EstimateGenerationCreditsResponse, pricingSnapshotID string) workbench.CreditEstimateDTO {
	if resp == nil {
		return workbench.CreditEstimateDTO{}
	}
	return workbench.CreditEstimateDTO{
		EstimateID: resp.EstimateId, EstimatePoints: resp.EstimatePoints, AvailablePoints: resp.AvailablePoints,
		ExpiresSoonPoints: resp.ExpiresSoonPoints, CreditAccountScope: resp.CreditAccountScope, CreditAccountID: resp.CreditAccountId,
		PricingSnapshotID: pricingSnapshotID, LineItems: estimateLineItemsFromRPC(resp.LineItems), ExpiresAt: resp.ExpiresAt, Insufficient: resp.Insufficient,
	}
}

func estimateToolFromRPC(resp *businessagent.EstimateToolCreditsResponse) workbench.CreditEstimateDTO {
	if resp == nil {
		return workbench.CreditEstimateDTO{}
	}
	return workbench.CreditEstimateDTO{
		EstimateID: resp.EstimateId, EstimatePoints: resp.EstimatePoints, AvailablePoints: resp.AvailablePoints,
		ExpiresSoonPoints: resp.ExpiresSoonPoints, CreditAccountScope: resp.CreditAccountScope, CreditAccountID: resp.CreditAccountId,
		LineItems: estimateLineItemsFromRPC(resp.LineItems), ExpiresAt: resp.ExpiresAt, Insufficient: resp.Insufficient,
	}
}

func estimateLineItemsFromRPC(lineItems []*businessagent.CreditEstimateLineItemDTO) []workbench.CreditEstimateLineItemDTO {
	items := make([]workbench.CreditEstimateLineItemDTO, 0, len(lineItems))
	for _, item := range lineItems {
		items = append(items, workbench.CreditEstimateLineItemDTO{
			EstimateItemID: item.EstimateItemId, ItemType: item.ItemType, ToolName: value(item.ToolName),
			ToolType: value(item.ToolType), ModelID: value(item.ModelId), ResourceType: value(item.ResourceType),
			BillingUnit: value(item.BillingUnit), EstimatePoints: item.EstimatePoints, Metadata: item.MetadataSummary,
		})
	}
	return items
}

func commitFromRPC(resp *businessagent.CommitGeneratedAssetAndChargeResponse) workbench.AssetCommitDTO {
	if resp == nil {
		return workbench.AssetCommitDTO{}
	}
	refs := make([]workbench.CommittedAssetRefDTO, 0, len(resp.AssetRefs))
	for _, item := range resp.AssetRefs {
		refs = append(refs, workbench.CommittedAssetRefDTO{
			AssetID: item.AssetId, SourceArtifactID: item.SourceArtifactId, ResourceType: item.ResourceType,
			AssetType: item.AssetType, Status: item.Status, PreviewURL: value(item.PreviewUrl), ElementsSummaryJSON: value(item.ElementsSummaryJson),
		})
	}
	lines := make([]workbench.ChargedLineItemDTO, 0, len(resp.ChargedLineItems))
	for _, item := range resp.ChargedLineItems {
		lines = append(lines, workbench.ChargedLineItemDTO{
			EstimateItemID: item.EstimateItemId, ChargedPoints: item.ChargedPoints, Status: item.Status,
			AssetID: value(item.AssetId), ToolCallID: value(item.ToolCallId), ArtifactID: value(item.ArtifactId),
		})
	}
	return workbench.AssetCommitDTO{
		AssetRefs: refs, ChargedPoints: resp.ChargedPoints, ReleasedPoints: resp.ReleasedPoints,
		CommitStatus: resp.CommitStatus, LedgerRef: value(resp.LedgerRef), ChargedLineItems: lines,
	}
}

func rpcMeta(traceID string) *businessagent.RequestMeta {
	if traceID == "" {
		traceID = "agent-local"
	}
	return &businessagent.RequestMeta{RequestId: traceID, TraceId: traceID, Source: "agent_service"}
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

func optionalInt64(value int64) *int64 {
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
