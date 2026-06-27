package workbench

import (
	"context"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
)

type StaticGateway struct {
	Space  SpaceContextDTO
	Access ProjectAccessDTO
	Err    error
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
