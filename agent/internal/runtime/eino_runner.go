package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/plancreationspec"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/adk"
)

// EinoRunner 是真实 adk.NewRunner 的 Runtime 适配器；它不实现自定义 ReAct 循环。
type EinoRunner struct {
	runner *adk.Runner
}

// NewEinoRunner 在启动期用唯一主 ChatModelAgent 创建 ADK Runner。
func NewEinoRunner(ctx context.Context, agent adk.Agent) (*EinoRunner, error) {
	if agent == nil {
		return nil, fmt.Errorf("create Eino runner: main agent is required")
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
	if runner == nil {
		return nil, fmt.Errorf("create Eino runner: adk.NewRunner returned nil")
	}
	return &EinoRunner{runner: runner}, nil
}

// Run 注入不可变可信 Turn Context，并完整消费所有 AgentEvent 与 MessageStream。
func (r *EinoRunner) Run(ctx context.Context, claim Claim) error {
	if r == nil || r.runner == nil || claim.FenceToken < 1 || claim.Owner == "" {
		return ErrInvalidInput
	}
	if _, err := plancreationspec.DecodeIntent(claim.Intent); err != nil {
		return ErrInvalidInput
	}
	runCtx := turncontext.WithPreview(ctx, turncontext.Preview{
		Owner: claim.Owner, RequestID: claim.RequestID, UserID: claim.UserID, ProjectID: claim.ProjectID,
		SessionID: claim.SessionID, InputID: claim.InputID, TurnID: claim.TurnID, RunID: claim.RunID,
		ToolCallID: claim.ToolCallID, BusinessCommandID: claim.BusinessCommandID, FenceToken: claim.FenceToken,
		PromptVersion: claim.PromptVersion, ValidatorVersion: claim.ValidatorVersion,
	})
	iterator := r.runner.Query(runCtx, string(claim.Intent))
	if iterator == nil {
		return fmt.Errorf("run Eino preview: runner returned nil iterator")
	}
	var runErr error
	for {
		agentEvent, ok := iterator.Next()
		if !ok {
			break
		}
		if agentEvent == nil {
			if runErr == nil {
				runErr = fmt.Errorf("run Eino preview: nil AgentEvent")
			}
			continue
		}
		if agentEvent.Output != nil && agentEvent.Output.MessageOutput != nil {
			// GetMessage 对 streaming event 会读到 EOF 并关闭独占 Reader，避免泄漏 Graph/Model goroutine。
			if _, err := agentEvent.Output.MessageOutput.GetMessage(); err != nil && runErr == nil {
				runErr = fmt.Errorf("consume Eino preview message: %w", err)
			}
		}
		if agentEvent.Err != nil && runErr == nil {
			runErr = agentEvent.Err
		}
		if agentEvent.Action != nil && agentEvent.Action.Interrupted != nil && runErr == nil {
			runErr = fmt.Errorf("run Eino preview: unexpected interrupt")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if errors.Is(runErr, plancreationspec.ErrBusinessUnknownOutcome) {
		return plancreationspec.ErrBusinessUnknownOutcome
	}
	return runErr
}

var _ Runner = (*EinoRunner)(nil)
