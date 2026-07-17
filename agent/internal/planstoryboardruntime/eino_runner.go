package planstoryboardruntime

import (
	"context"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/planstoryboard"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Runner 是 Processor 消费的一次单 Tool Agent 端口。
type Runner interface {
	Run(context.Context, Claim) (planstoryboard.Result, error)
}

// EinoRunner 使用真实 ADK Runner 执行唯一主 ChatModelAgent，并完整消费事件流。
type EinoRunner struct{ runner *adk.Runner }

// NewEinoRunner 只接受本 Profile 的固定单 Tool Agent。
func NewEinoRunner(ctx context.Context, agent adk.Agent) (*EinoRunner, error) {
	if agent == nil || (agent.Name(ctx) != chatmodelagent.PlanStoryboardName && agent.Name(ctx) != chatmodelagent.MVPAllToolsName) {
		return nil, fmt.Errorf("create plan storyboard Eino runner: exact preview agent is required")
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: false})
	if runner == nil {
		return nil, fmt.Errorf("create plan storyboard Eino runner: ADK runner is nil")
	}
	return &EinoRunner{runner: runner}, nil
}

// Run 注入 Runtime/Core 双层可信 Context，只接受 ReturnDirectly 的一个 plan_storyboard Tool Result。
func (r *EinoRunner) Run(ctx context.Context, claim Claim) (planstoryboard.Result, error) {
	if r == nil || r.runner == nil || ValidateClaim(claim) != nil {
		return planstoryboard.Result{}, ErrInvalidClaim
	}
	ctx = turncontext.WithPlanStoryboardRuntime(ctx, RuntimeContextFromClaim(claim))
	ctx = turncontext.WithPlanStoryboardPreview(ctx, PreviewContextFromClaim(claim))
	iterator := r.runner.Query(ctx, string(claim.IntentJSON))
	if iterator == nil {
		return planstoryboard.Result{}, fmt.Errorf("run plan storyboard Eino agent: nil iterator")
	}
	var result planstoryboard.Result
	seen := false
	var runErr error
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event == nil {
			if runErr == nil {
				runErr = ErrOutputContract
			}
			continue
		}
		if event.Err != nil && runErr == nil {
			runErr = event.Err
		}
		if event.Action != nil && runErr == nil {
			runErr = fmt.Errorf("%w: AgentAction is forbidden", ErrOutputContract)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		variant := event.Output.MessageOutput
		message, err := variant.GetMessage()
		if err != nil {
			if runErr == nil {
				runErr = err
			}
			continue
		}
		if variant.Role == schema.Assistant {
			if seen || len(message.ToolCalls) != 1 || message.ToolCalls[0].ID != claim.Context.ToolCallID ||
				message.ToolCalls[0].Function.Name != planstoryboard.ToolKey ||
				message.ToolCalls[0].Function.Arguments != string(claim.IntentJSON) {
				if runErr == nil {
					runErr = fmt.Errorf("%w: exact Router ToolCall is required", ErrOutputContract)
				}
			}
			continue
		}
		if variant.Role != schema.Tool || variant.ToolName != planstoryboard.ToolKey || seen {
			if runErr == nil {
				runErr = fmt.Errorf("%w: exact single Tool output is required", ErrOutputContract)
			}
			continue
		}
		seen = true
		decoded, _, err := decodeToolResult([]byte(message.Content), RuntimeContextFromClaim(claim))
		if err != nil {
			if runErr == nil {
				runErr = err
			}
			continue
		}
		result = decoded
	}
	if err := ctx.Err(); err != nil {
		return planstoryboard.Result{}, err
	}
	if runErr != nil {
		return planstoryboard.Result{}, runErr
	}
	if !seen {
		return planstoryboard.Result{}, fmt.Errorf("%w: Tool output is missing", ErrOutputContract)
	}
	return result, nil
}

var _ Runner = (*EinoRunner)(nil)
