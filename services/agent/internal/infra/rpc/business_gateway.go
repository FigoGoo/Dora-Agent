package rpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/accountspaceservice"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent/projectservice"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/application/workbench"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/config"
	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/client/callopt"
	etcd "github.com/kitex-contrib/registry-etcd"
)

type BusinessGateway struct {
	account accountspaceservice.Client
	project projectservice.Client
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
	return &BusinessGateway{account: accountClient, project: projectClient, timeout: cfg.KitexTimeout}, nil
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
	return workbench.SpaceContextDTO{
		SpaceID: resp.SpaceId, SpaceType: resp.SpaceType, EnterpriseID: value(resp.EnterpriseId), EnterpriseRole: value(resp.EnterpriseRole),
		CreditAccountScope: resp.CreditAccountScope, CreditAccountID: resp.CreditAccountId, SkillScopeKeys: resp.SkillScopeKeys, PermissionSummary: resp.PermissionSummary,
	}, nil
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
