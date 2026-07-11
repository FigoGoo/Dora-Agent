package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
)

type RunnerInvoker struct {
	runner           *adk.Runner
	chatModelOptions []einomodel.Option
}

// RunnerInvokerOption 调整 RunnerInvoker 调用 Agent 时传入的运行级配置。
type RunnerInvokerOption func(*RunnerInvoker)

// WithRunnerChatModelOptions 把 Eino ChatModel options 透传给 ChatModelAgent。
func WithRunnerChatModelOptions(options ...einomodel.Option) RunnerInvokerOption {
	return func(invoker *RunnerInvoker) {
		invoker.chatModelOptions = append([]einomodel.Option(nil), options...)
	}
}

// NewRunnerInvoker 创建 Agent Runner 调用器。
func NewRunnerInvoker(runner *adk.Runner, options ...RunnerInvokerOption) *RunnerInvoker {
	invoker := &RunnerInvoker{runner: runner}
	for _, option := range options {
		if option != nil {
			option(invoker)
		}
	}
	return invoker
}

func (i *RunnerInvoker) Invoke(ctx context.Context, req AgentInvokeRequest) (<-chan AgentEvent, error) {
	if i == nil || i.runner == nil {
		return nil, fmt.Errorf("runner is required")
	}

	opts := make([]adk.AgentRunOption, 0, 3)
	if req.CheckpointID != "" {
		opts = append(opts, adk.WithCheckPointID(req.CheckpointID))
	}
	if len(req.SessionValues) > 0 {
		opts = append(opts, adk.WithSessionValues(req.SessionValues))
	}
	if len(i.chatModelOptions) > 0 {
		opts = append(opts, adk.WithChatModelOptions(i.chatModelOptions))
	}

	iter := i.runner.Run(ctx, req.Messages, opts...)
	return runnerEvents(iter, req.CheckpointID), nil
}

func (i *RunnerInvoker) Resume(ctx context.Context, req AgentResumeRequest) (<-chan AgentEvent, error) {
	if i == nil || i.runner == nil {
		return nil, fmt.Errorf("runner is required")
	}
	if req.CheckpointID == "" {
		return nil, fmt.Errorf("checkpoint id is required")
	}

	opts := make([]adk.AgentRunOption, 0, 2)
	if len(req.SessionValues) > 0 {
		opts = append(opts, adk.WithSessionValues(req.SessionValues))
	}
	if len(i.chatModelOptions) > 0 {
		opts = append(opts, adk.WithChatModelOptions(i.chatModelOptions))
	}
	iter, err := i.runner.ResumeWithParams(ctx, req.CheckpointID, &adk.ResumeParams{
		Targets: req.Targets,
	}, opts...)
	if err != nil {
		return nil, err
	}
	return runnerEvents(iter, req.CheckpointID), nil
}

func runnerEvents(iter *adk.AsyncIterator[*adk.AgentEvent], checkpointID string) <-chan AgentEvent {
	out := make(chan AgentEvent)
	go func() {
		defer close(out)
		for {
			event, ok := iter.Next()
			if !ok {
				return
			}
			if event == nil {
				continue
			}
			if event.Err != nil {
				if isWillRetryEvent(event.Err) {
					continue
				}
				payload := map[string]any{"message": event.Err.Error()}
				out <- AgentEvent{
					Event:   a2ui.EventError,
					Payload: payload,
					Err:     event.Err,
				}
				return
			}
			if event.Action != nil && event.Action.Interrupted != nil {
				out <- interruptToAgentEvent(checkpointID, event.Action.Interrupted)
				return
			}
			if event.Action != nil && event.Action.Exit {
				return
			}
			if event.Output == nil || event.Output.MessageOutput == nil {
				if event.Output != nil && event.Output.CustomizedOutput != nil {
					out <- customizedOutputToAgentEvent(event.Output.CustomizedOutput)
				}
				continue
			}

			message, err := event.Output.MessageOutput.GetMessage()
			if err != nil {
				if isWillRetryEvent(err) {
					continue
				}
				payload := map[string]any{"message": err.Error()}
				out <- AgentEvent{
					Event:   a2ui.EventError,
					Payload: payload,
					Err:     err,
				}
				return
			}
			if message == nil {
				continue
			}

			out <- messageToAgentEvent(message)
		}
	}()
	return out
}

func isWillRetryEvent(err error) bool {
	var willRetry *adk.WillRetryError
	return errors.As(err, &willRetry)
}

func messageToAgentEvent(message *schema.Message) AgentEvent {
	payload := map[string]any{
		"role":    string(message.Role),
		"content": message.Content,
	}
	if message.ToolName != "" {
		payload["tool_name"] = message.ToolName
	}
	if message.ToolCallID != "" {
		payload["tool_call_id"] = message.ToolCallID
	}

	if message.Role == schema.Tool {
		return AgentEvent{
			Event:   a2ui.EventToolProgress,
			Payload: payload,
			Message: message,
		}
	}

	if message.Role == schema.Assistant && len(message.ToolCalls) > 0 {
		payload["tool_calls"] = message.ToolCalls
		return AgentEvent{
			Event:   a2ui.EventToolProgress,
			Payload: payload,
			Message: message,
		}
	}

	return AgentEvent{
		Event:         a2ui.EventChatDelta,
		Payload:       payload,
		AssistantText: message.Content,
		Message:       message,
	}
}

func interruptToAgentEvent(checkpointID string, info *adk.InterruptInfo) AgentEvent {
	payload := map[string]any{
		"checkpoint_id": checkpointID,
		"scope":         "runner",
	}
	contexts := make([]map[string]any, 0, len(info.InterruptContexts))
	var selected *adk.InterruptCtx
	for _, ctx := range info.InterruptContexts {
		if ctx == nil {
			continue
		}
		if selected == nil || ctx.IsRootCause {
			selected = ctx
		}
		contexts = append(contexts, map[string]any{
			"id":            ctx.ID,
			"address":       ctx.Address.String(),
			"info":          ctx.Info,
			"is_root_cause": ctx.IsRootCause,
		})
	}
	if selected != nil {
		payload["interrupt_id"] = selected.ID
		payload["message"] = fmt.Sprint(selected.Info)
	}
	if len(contexts) > 0 {
		payload["interrupts"] = contexts
	}
	return AgentEvent{
		Event:   a2ui.EventInterruptRequest,
		Payload: payload,
	}
}

func customizedOutputToAgentEvent(output any) AgentEvent {
	payload := output
	eventName := a2ui.EventToolProgress
	if values, ok := output.(map[string]any); ok {
		if raw, ok := values["event"].(string); ok && raw != "" {
			eventName = raw
		}
	}
	return AgentEvent{
		Event:   eventName,
		Payload: payload,
	}
}
