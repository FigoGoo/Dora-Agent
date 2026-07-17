package mediapreviewruntime

import (
	"context"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/chatmodelagent"
	"github.com/FigoGoo/Dora-Agent/agent/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Runner 是媒体请求 Processor 消费的共享主 Agent 端口。
type Runner interface {
	Run(context.Context, Claim) (mediapreview.GraphToolResult, error)
}

// EinoRunner 使用唯一主 ChatModelAgent 执行 deterministic Router 与媒体 Graph Tool。
type EinoRunner struct{ runner *adk.Runner }

// NewEinoRunner 只接受统一 Profile 的主 Agent。
func NewEinoRunner(ctx context.Context, agent adk.Agent) (*EinoRunner, error) {
	if agent == nil || agent.Name(ctx) != chatmodelagent.MVPAllToolsName {
		return nil, fmt.Errorf("create media preview Eino runner: unified main Agent is required")
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: false})
	if runner == nil {
		return nil, fmt.Errorf("create media preview Eino runner: nil runner")
	}
	return &EinoRunner{runner: runner}, nil
}

// Run 注入两类互斥可信 Context，只接受一个 exact ToolCall 与一个 ReturnDirectly Tool Result。
func (r *EinoRunner) Run(ctx context.Context, claim Claim) (mediapreview.GraphToolResult, error) {
	if r == nil || r.runner == nil || validateClaim(claim) != nil {
		return mediapreview.GraphToolResult{}, ErrInvalidInput
	}
	trusted := turncontext.MediaPreview{
		Owner: claim.Owner, RequestID: claim.RequestID, IdempotencyKey: claim.IdempotencyKey,
		UserID: claim.UserID, ProjectID: claim.ProjectID, SessionID: claim.SessionID,
		InputID: claim.InputID, TurnID: claim.TurnID, RunID: claim.RunID,
		ToolCallID: claim.ToolCallID, FenceToken: claim.FenceToken, DeadlineAt: claim.DeadlineAt,
	}
	if claim.ToolKey == mediapreview.GenerateMediaToolKey {
		ctx = turncontext.WithGenerateMediaPreview(ctx, trusted)
	} else {
		ctx = turncontext.WithAssembleOutputPreview(ctx, trusted)
	}
	iterator := r.runner.Query(ctx, string(claim.IntentJSON))
	if iterator == nil {
		return mediapreview.GraphToolResult{}, ErrOutputContract
	}
	var result mediapreview.GraphToolResult
	seenToolCall, seenOutput := false, false
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
			runErr = ErrOutputContract
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
		switch variant.Role {
		case schema.Assistant:
			if seenToolCall || len(message.ToolCalls) != 1 || message.Content != "" ||
				message.ToolCalls[0].ID != claim.ToolCallID ||
				message.ToolCalls[0].Function.Name != claim.ToolKey ||
				message.ToolCalls[0].Function.Arguments != string(claim.IntentJSON) {
				if runErr == nil {
					runErr = ErrOutputContract
				}
			} else {
				seenToolCall = true
			}
		case schema.Tool:
			if !seenToolCall || seenOutput || variant.ToolName != claim.ToolKey {
				if runErr == nil {
					runErr = ErrOutputContract
				}
				continue
			}
			decoded, err := mediapreview.DecodeGraphToolResult([]byte(message.Content))
			if err != nil || decoded.ToolKey != claim.ToolKey {
				if runErr == nil {
					runErr = ErrOutputContract
				}
				continue
			}
			result, seenOutput = decoded, true
		default:
			if runErr == nil {
				runErr = ErrOutputContract
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return mediapreview.GraphToolResult{}, err
	}
	if runErr != nil {
		return mediapreview.GraphToolResult{}, runErr
	}
	if !seenToolCall || !seenOutput {
		return mediapreview.GraphToolResult{}, ErrOutputContract
	}
	return result, nil
}

func validateClaim(claim Claim) error {
	for _, value := range []string{claim.RequestID, claim.IdempotencyKey, claim.UserID, claim.ProjectID,
		claim.SessionID, claim.InputID, claim.TurnID, claim.RunID, claim.ToolCallID,
		claim.AcceptedEventID, claim.TerminalEventID} {
		if !mediapreview.ValidUUIDv7(value) {
			return ErrInvalidInput
		}
	}
	if claim.Owner == "" || claim.FenceToken < 1 || claim.DeadlineAt.IsZero() ||
		(claim.ToolKey != mediapreview.GenerateMediaToolKey && claim.ToolKey != mediapreview.AssembleOutputToolKey) ||
		!mediapreview.ValidDigest(claim.IntentDigest) {
		return ErrInvalidInput
	}
	canonical, _, err := canonicalIntent(claim.ToolKey, claim.IntentJSON)
	if err != nil || digest(canonical) != claim.IntentDigest || string(canonical) != string(claim.IntentJSON) {
		return ErrInvalidInput
	}
	return nil
}

var _ Runner = (*EinoRunner)(nil)
