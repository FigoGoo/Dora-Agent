package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/agentcontrol"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

// nextCapabilityDirectiveMiddleware turns a trusted durable continuation
// directive into one deterministic ToolCall. The wrapper sits inside the model
// receipt middleware, so the synthetic selection is frozen before the Tool can
// run and is replayed with the same call ID after a crash.
type nextCapabilityDirectiveMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func newNextCapabilityDirectiveMiddleware() *nextCapabilityDirectiveMiddleware {
	return &nextCapabilityDirectiveMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
}

func (m *nextCapabilityDirectiveMiddleware) WrapModel(_ context.Context, inner model.BaseChatModel, _ *adk.ModelContext) (model.BaseChatModel, error) {
	if inner == nil {
		return nil, fmt.Errorf("next-capability directive inner model is required")
	}
	return &nextCapabilityDirectiveModel{inner: inner}, nil
}

type nextCapabilityDirectiveModel struct{ inner model.BaseChatModel }

func (m *nextCapabilityDirectiveModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	directive, force, err := pendingNextCapabilityDirective(input)
	if err != nil {
		return nil, err
	}
	if force {
		return nextCapabilityToolCall(directive)
	}
	return m.inner.Generate(ctx, input, opts...)
}

// nextCapabilityRepeatGuardMiddleware sits outside the receipt wrapper. Raw
// provider attempts are frozen first; once the directive's ToolCall/result pair
// exists, this continuation turn is explanation-only. Any further provider
// ToolCall is replaced with a deterministic A2UI stop card so a model cannot
// branch into premature assembly/export or create duplicate side effects.
type nextCapabilityRepeatGuardMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func newNextCapabilityRepeatGuardMiddleware() *nextCapabilityRepeatGuardMiddleware {
	return &nextCapabilityRepeatGuardMiddleware{BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{}}
}

func (m *nextCapabilityRepeatGuardMiddleware) WrapModel(_ context.Context, inner model.BaseChatModel, _ *adk.ModelContext) (model.BaseChatModel, error) {
	if inner == nil {
		return nil, fmt.Errorf("next-capability repeat guard inner model is required")
	}
	return &nextCapabilityRepeatGuardModel{inner: inner}, nil
}

type nextCapabilityRepeatGuardModel struct{ inner model.BaseChatModel }

func (m *nextCapabilityRepeatGuardModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	directive, force, err := pendingNextCapabilityDirective(input)
	if err != nil {
		return nil, err
	}
	message, err := m.inner.Generate(ctx, input, opts...)
	if err != nil || force || directive.Tool == "" {
		return message, err
	}
	return guardRepeatedNextCapability(directive, message)
}

func (m *nextCapabilityRepeatGuardModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	directive, force, err := pendingNextCapabilityDirective(input)
	if err != nil {
		return nil, err
	}
	reader, err := m.inner.Stream(ctx, input, opts...)
	if err != nil || force || directive.Tool == "" {
		return reader, err
	}
	if reader == nil {
		return nil, fmt.Errorf("next-capability repeat guard received a nil stream")
	}
	defer reader.Close()
	chunks := make([]*schema.Message, 0, 4)
	for {
		chunk, recvErr := reader.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return nil, recvErr
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("next-capability repeat guard received an empty stream")
	}
	message, err := schema.ConcatMessages(chunks)
	if err != nil {
		return nil, err
	}
	message, err = guardRepeatedNextCapability(directive, message)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

func (m *nextCapabilityDirectiveModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	directive, force, err := pendingNextCapabilityDirective(input)
	if err != nil {
		return nil, err
	}
	if force {
		message, err := nextCapabilityToolCall(directive)
		if err != nil {
			return nil, err
		}
		return schema.StreamReaderFromArray([]*schema.Message{message}), nil
	}
	return m.inner.Stream(ctx, input, opts...)
}

func pendingNextCapabilityDirective(input []*schema.Message) (agentcontrol.NextCapabilityDirective, bool, error) {
	var found *agentcontrol.NextCapabilityDirective
	for _, message := range input {
		if message == nil || message.Role != schema.System {
			continue
		}
		directive, ok, err := agentcontrol.ParseNextCapabilityDirective(message.Content)
		if err != nil {
			return agentcontrol.NextCapabilityDirective{}, false, err
		}
		if !ok {
			continue
		}
		if found != nil {
			return agentcontrol.NextCapabilityDirective{}, false, fmt.Errorf("multiple trusted next-capability directives")
		}
		found = &directive
	}
	if found == nil {
		return agentcontrol.NextCapabilityDirective{}, false, nil
	}
	if err := validateNextCapabilityDirective(*found); err != nil {
		return agentcontrol.NextCapabilityDirective{}, false, err
	}
	callID, err := found.StableCallID()
	if err != nil {
		return agentcontrol.NextCapabilityDirective{}, false, err
	}
	callSeen, resultSeen := false, false
	for _, message := range input {
		if message == nil {
			continue
		}
		if strings.TrimSpace(message.ToolCallID) == callID {
			if message.Role != schema.Tool || (strings.TrimSpace(message.ToolName) != "" && strings.TrimSpace(message.ToolName) != found.Tool) {
				return agentcontrol.NextCapabilityDirective{}, false, fmt.Errorf("next-capability directive result does not match its stable ToolCall")
			}
			resultSeen = true
		}
		for _, toolCall := range message.ToolCalls {
			if strings.TrimSpace(toolCall.ID) == callID {
				arguments, compactErr := compactDirectiveArguments([]byte(toolCall.Function.Arguments))
				if message.Role != schema.Assistant || strings.TrimSpace(toolCall.Function.Name) != found.Tool || compactErr != nil || string(arguments) != string(found.Arguments) {
					return agentcontrol.NextCapabilityDirective{}, false, fmt.Errorf("stable next-capability ToolCall does not match its directive")
				}
				callSeen = true
			}
		}
	}
	if callSeen != resultSeen {
		return agentcontrol.NextCapabilityDirective{}, false, fmt.Errorf("next-capability directive ToolCall/result pair is incomplete")
	}
	if callSeen {
		return *found, false, nil
	}
	return *found, true, nil
}

func compactDirectiveArguments(raw []byte) (json.RawMessage, error) {
	if !json.Valid(raw) {
		return nil, fmt.Errorf("invalid JSON arguments")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func validateNextCapabilityDirective(value agentcontrol.NextCapabilityDirective) error {
	switch value.Tool {
	case capability.PlanStoryboardToolKey:
		var intent capability.PlanStoryboardIntent
		if err := decodeStrictDirectiveArguments(value.Arguments, &intent); err != nil {
			return fmt.Errorf("invalid plan_storyboard continuation arguments: %w", err)
		}
		return intent.Validate()
	case capability.GenerateMediaToolKey:
		var intent capability.GenerateMediaIntent
		if err := decodeStrictDirectiveArguments(value.Arguments, &intent); err != nil {
			return fmt.Errorf("invalid generate_media continuation arguments: %w", err)
		}
		return intent.Validate()
	case capability.AssembleOutputToolKey:
		var intent capability.AssembleOutputIntent
		if err := decodeStrictDirectiveArguments(value.Arguments, &intent); err != nil {
			return fmt.Errorf("invalid assemble_output continuation arguments: %w", err)
		}
		return intent.Validate()
	default:
		return fmt.Errorf("next-capability directive tool %q is not allowed", value.Tool)
	}
}

func decodeStrictDirectiveArguments(raw json.RawMessage, output any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
}

func nextCapabilityToolCall(value agentcontrol.NextCapabilityDirective) (*schema.Message, error) {
	callID, err := value.StableCallID()
	if err != nil {
		return nil, err
	}
	message := schema.AssistantMessage("", []schema.ToolCall{{
		ID:   callID,
		Type: "function",
		Function: schema.FunctionCall{
			Name:      value.Tool,
			Arguments: string(value.Arguments),
		},
	}})
	adk.EnsureMessageID(message)
	return message, nil
}

func guardRepeatedNextCapability(value agentcontrol.NextCapabilityDirective, message *schema.Message) (*schema.Message, error) {
	if message == nil {
		return nil, fmt.Errorf("next-capability repeat guard received a nil message")
	}
	if len(message.ToolCalls) > 0 {
		card := a2ui.NewCard(a2ui.CardTypeGeneric, "自动续作已停止", "root", []a2ui.Component{
			a2ui.CardContainer("root", []string{"message"}),
			a2ui.Text("message", fmt.Sprintf("%s 已按审批续作指令执行一次；本轮不会再执行其他 Capability。请查看最新结果，后续阶段由新的持久化事件继续。", value.Tool), "", ""),
		})
		envelope := a2ui.ActionEnvelope{Version: a2ui.Version1, Actions: []a2ui.Action{{
			Type: a2ui.ActionAppendCard, Surface: "chat", CardID: "continuation-stopped", Card: &card,
		}}}
		raw, err := json.Marshal(envelope)
		if err != nil {
			return nil, err
		}
		guarded := schema.AssistantMessage(string(raw), nil)
		adk.EnsureMessageID(guarded)
		return guarded, nil
	}
	return message, nil
}

var _ adk.ChatModelAgentMiddleware = (*nextCapabilityDirectiveMiddleware)(nil)
var _ adk.ChatModelAgentMiddleware = (*nextCapabilityRepeatGuardMiddleware)(nil)
var _ model.BaseChatModel = (*nextCapabilityDirectiveModel)(nil)
var _ model.BaseChatModel = (*nextCapabilityRepeatGuardModel)(nil)
