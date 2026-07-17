package usermessageruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Runner 是 Processor 消费的单次无 Tool Agent 端口。
type Runner interface {
	Run(ctx context.Context, claim Claim) (Output, error)
}

// EinoRunner 使用真实 adk.Runner 执行无 Tool ChatModelAgent，不实现第二套 ReAct 循环。
type EinoRunner struct {
	runner *adk.Runner
}

// NewEinoRunner 只接受 NewDirectResponse 创建的稳定 Agent，并固定关闭 streaming 建议。
func NewEinoRunner(ctx context.Context, agent adk.Agent) (*EinoRunner, error) {
	if agent == nil || (agent.Name(ctx) != chatmodelagent.DirectResponseName && agent.Name(ctx) != chatmodelagent.MVPAllToolsName) {
		return nil, fmt.Errorf("create user message Eino runner: direct response agent is required")
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: false})
	if runner == nil {
		return nil, fmt.Errorf("create user message Eino runner: adk.NewRunner returned nil")
	}
	return &EinoRunner{runner: runner}, nil
}

// Run 注入可信 Context、完整消费事件流，并且只接受一个纯 Assistant exact-set JSON Card。
func (r *EinoRunner) Run(ctx context.Context, claim Claim) (Output, error) {
	if r == nil || r.runner == nil {
		return Output{}, ErrInvalidClaim
	}
	if err := ValidateClaim(claim); err != nil {
		return Output{}, err
	}
	runCtx := turncontext.WithUserMessageRuntime(ctx, turncontext.UserMessageRuntime{
		Profile: claim.Profile, Owner: claim.Owner, RunID: claim.RunID,
		ModelCallID: claim.ModelCallID, OutputID: claim.OutputID, FenceToken: claim.FenceToken,
		Context: claim.Context,
	})
	iterator := r.runner.Query(runCtx, claim.MessagePlaintext)
	if iterator == nil {
		return Output{}, fmt.Errorf("run user message Eino agent: runner returned nil iterator")
	}
	var (
		card       DirectResponseCard
		outputSeen bool
		runErr     error
	)
	for {
		agentEvent, ok := iterator.Next()
		if !ok {
			break
		}
		if agentEvent == nil {
			if runErr == nil {
				runErr = fmt.Errorf("%w: nil AgentEvent", ErrOutputContract)
			}
			continue
		}
		if agentEvent.Err != nil && runErr == nil {
			runErr = agentEvent.Err
		}
		if agentEvent.Action != nil && runErr == nil {
			runErr = fmt.Errorf("%w: AgentAction is forbidden", ErrOutputContract)
		}
		if agentEvent.Output == nil {
			continue
		}
		if agentEvent.Output.CustomizedOutput != nil && runErr == nil {
			runErr = fmt.Errorf("%w: customized output is forbidden", ErrOutputContract)
		}
		variant := agentEvent.Output.MessageOutput
		if variant == nil {
			continue
		}
		// 即使前面已经观察到错误也必须消费流，避免泄漏模型/Graph goroutine。
		message, err := variant.GetMessage()
		if err != nil {
			if runErr == nil {
				runErr = fmt.Errorf("consume user message Eino output: %w", err)
			}
			continue
		}
		if outputSeen {
			if runErr == nil {
				runErr = fmt.Errorf("%w: multiple message outputs", ErrOutputContract)
			}
			continue
		}
		outputSeen = true
		if variant.Role != schema.Assistant || variant.ToolName != "" {
			if runErr == nil {
				runErr = fmt.Errorf("%w: non-assistant event", ErrOutputContract)
			}
			continue
		}
		decoded, err := decodePureAssistant(message)
		if err != nil {
			if runErr == nil {
				runErr = err
			}
			continue
		}
		if err := ValidateDirectResponse(decoded, claim); err != nil {
			if runErr == nil {
				runErr = err
			}
			continue
		}
		card = decoded
	}
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	if runErr != nil {
		return Output{}, runErr
	}
	if !outputSeen {
		return Output{}, fmt.Errorf("%w: assistant output is missing", ErrOutputContract)
	}
	return Output{DirectResponse: &card}, nil
}

func decodePureAssistant(message *schema.Message) (DirectResponseCard, error) {
	violations := pureAssistantViolations(message)
	if len(violations) != 0 {
		return DirectResponseCard{}, fmt.Errorf(
			"%w: model output is not a pure assistant content message (%s)",
			ErrOutputContract, strings.Join(violations, ","),
		)
	}
	card, err := DecodeDirectResponseCard(message.Content)
	if err != nil {
		return DirectResponseCard{}, err
	}
	return card, nil
}

// pureAssistantViolations 只返回字段名，不回显模型内容、Tool arguments 或 Provider metadata。
func pureAssistantViolations(message *schema.Message) []string {
	if message == nil {
		return []string{"nil_message"}
	}
	violations := make([]string, 0, 8)
	if message.Role != schema.Assistant {
		violations = append(violations, "role")
	}
	if message.Content == "" {
		violations = append(violations, "empty_content")
	}
	if len(message.ToolCalls) != 0 {
		violations = append(violations, "tool_calls")
	}
	if message.Name != "" {
		violations = append(violations, "name")
	}
	if message.ToolCallID != "" || message.ToolName != "" {
		violations = append(violations, "tool_result_identity")
	}
	if len(message.MultiContent) != 0 || len(message.UserInputMultiContent) != 0 || len(message.AssistantGenMultiContent) != 0 {
		violations = append(violations, "multi_content")
	}
	if message.ResponseMeta != nil {
		violations = append(violations, "response_meta")
	}
	if message.ReasoningContent != "" {
		violations = append(violations, "reasoning_content")
	}
	// Eino ADK 在 AgentEvent 发出前会加入唯一 `_eino_msg_id`（UUIDv4）以关联事件与内部 state。
	// 该字段不是模型内容，不进入 Card/Receipt；只允许这个由 ADK 公共 API 可识别的单键 metadata。
	if len(message.Extra) > 1 || (len(message.Extra) == 1 && !isCanonicalUUIDv4(adk.GetMessageID(message))) {
		violations = append(violations, "extra")
	}
	return violations
}

var _ Runner = (*EinoRunner)(nil)
