package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	adkskill "github.com/cloudwego/eino/adk/middlewares/skill"
)

// dynamicSkillMiddleware 只在当前 Agent run 存在可用 Skill 时委托给 Eino
// middleware。Backend 会在每个 run 重新查询，因此运行中导入的 Skill 可在
// 后续 turn 生效，无需重建 Runner。
type dynamicSkillMiddleware struct {
	adk.ChatModelAgentMiddleware
	backend adkskill.Backend
}

func newDynamicSkillMiddleware(ctx context.Context, backend adkskill.Backend) (adk.ChatModelAgentMiddleware, error) {
	if backend == nil {
		return nil, fmt.Errorf("skill backend is required")
	}
	delegate, err := adkskill.NewMiddleware(ctx, &adkskill.Config{
		Backend:    backend,
		UseChinese: true,
	})
	if err != nil {
		return nil, err
	}
	return &dynamicSkillMiddleware{ChatModelAgentMiddleware: delegate, backend: backend}, nil
}

func (m *dynamicSkillMiddleware) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	skills, err := m.backend.List(ctx)
	if err != nil {
		return ctx, runCtx, fmt.Errorf("list skills for Agent run: %w", err)
	}
	if len(skills) == 0 {
		return ctx, runCtx, nil
	}
	return m.ChatModelAgentMiddleware.BeforeAgent(ctx, runCtx)
}

var _ adk.ChatModelAgentMiddleware = (*dynamicSkillMiddleware)(nil)
