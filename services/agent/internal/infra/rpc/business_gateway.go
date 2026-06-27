package rpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/accountspaceservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/modelconfigservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/platformdictionaryservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/projectservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/skillcatalogservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/toolcapabilityservice"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/config"
	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/client/callopt"
	etcd "github.com/kitex-contrib/registry-etcd"
)

type BusinessGateway struct {
	account accountspaceservice.Client
	project projectservice.Client
	skill   skillcatalogservice.Client
	tool    toolcapabilityservice.Client
	model   modelconfigservice.Client
	dict    platformdictionaryservice.Client
	timeout time.Duration
}

func NewBusinessGateway(cfg config.AgentConfig) (*BusinessGateway, error) {
	opts := []client.Option{}
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
	return &BusinessGateway{account: accountClient, project: projectClient, skill: skillClient, tool: toolClient, model: modelClient, dict: dictClient, timeout: cfg.KitexTimeout}, nil
}

func (g *BusinessGateway) ResolveAuthContextFromToken(ctx context.Context, authorization string, expectedSpaceID string, traceID string) (workbench.AuthContextDTO, workbench.SpaceContextDTO, error) {
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
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
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
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
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
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
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
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
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
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
		ExecutionPolicySummaryJSON: resp.ExecutionPolicySummaryJson,
	}, nil
}

func (g *BusinessGateway) CheckToolExecutionPolicy(ctx context.Context, auth workbench.AuthContextDTO, toolName string, toolType string, projectID string, riskContext map[string]string, traceID string) (workbench.ToolExecutionPolicyDTO, error) {
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
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

func (g *BusinessGateway) ResolveDefaultModel(ctx context.Context, auth workbench.AuthContextDTO, resourceType string, traceID string) (workbench.ModelSummaryDTO, error) {
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
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
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
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
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
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
			ElementType: item.ElementType, DisplayName: item.DisplayName, Category: item.Category,
			SchemaVersion: item.SchemaVersion, Active: item.Active, SortOrder: item.SortOrder,
		})
	}
	return out, resp.SchemaVersion, nil
}

func (g *BusinessGateway) SaveSkillTestResult(ctx context.Context, auth workbench.AuthContextDTO, req workbench.SkillTestResultRequest, traceID string) (workbench.SkillTestResultDTO, error) {
	callCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	resp, err := g.skill.SaveSkillTestResult_(callCtx, &businessagent.SaveSkillTestResultRequest{
		AuthContext: rpcAuth(auth), RequestMeta: rpcMeta(traceID), SkillId: req.SkillID, VersionId: req.VersionID,
		TestRunId: req.TestRunID, TestCaseId: optionalString(req.TestCaseID), Status: req.Status, ActualElementsJson: req.ActualElementsJSON,
		ErrorCode: optionalString(req.ErrorCode), ErrorSummary: optionalString(req.ErrorSummary), SafetyEvidenceJson: optionalString(req.SafetyEvidenceJSON), AgentTraceId: req.AgentTraceID,
	}, callopt.WithRPCTimeout(g.timeout))
	if err != nil {
		return workbench.SkillTestResultDTO{}, err
	}
	return workbench.SkillTestResultDTO{TestRunID: resp.TestRunId, Status: resp.Status, Saved: resp.Saved}, nil
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

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}
