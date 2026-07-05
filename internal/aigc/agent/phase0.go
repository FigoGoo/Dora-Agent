package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	aigcmw "github.com/FigoGoo/Dora-Agent/internal/aigc/middleware"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

type Phase0Config struct {
	Name        string
	Description string
	Instruction string
	Registry    *aigctools.Registry
}

func NewPhase0Runner(ctx context.Context, cfg Phase0Config) (*adk.TypedRunner[*schema.AgenticMessage], error) {
	registry := cfg.Registry
	if registry == nil {
		var err error
		registry, err = newDefaultRegistry()
		if err != nil {
			return nil, err
		}
	}

	name := cfg.Name
	if name == "" {
		name = "AIGCPhase0Agent"
	}
	description := cfg.Description
	if description == "" {
		description = "Phase 0 AIGC ChatModelAgent spike with local echo tool and tool exception middleware."
	}
	instruction := cfg.Instruction
	if instruction == "" {
		instruction = "You are a Phase 0 AIGC creation agent. Use tools when useful and keep answers concise."
	}

	agent, err := adk.NewTypedChatModelAgent(ctx, &adk.TypedChatModelAgentConfig[*schema.AgenticMessage]{
		Name:        name,
		Description: description,
		Instruction: instruction,
		Model:       staticAgenticModel{},
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: registry.ListByKeys([]string{"echo_tool"}),
			},
		},
		Handlers: []adk.TypedChatModelAgentMiddleware[*schema.AgenticMessage]{
			aigcmw.NewToolExceptionMiddleware[*schema.AgenticMessage](),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create phase0 chat model agent: %w", err)
	}

	return adk.NewTypedRunner(adk.TypedRunnerConfig[*schema.AgenticMessage]{
		Agent:           agent,
		EnableStreaming: true,
	}), nil
}

type staticAgenticModel struct{}

func (staticAgenticModel) Generate(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.AgenticMessage, error) {
	return &schema.AgenticMessage{
		Role: schema.AgenticRoleTypeAssistant,
		ContentBlocks: []*schema.ContentBlock{
			schema.NewContentBlock(&schema.AssistantGenText{Text: "Phase 0 AIGC agent is ready."}),
		},
	}, nil
}

func (m staticAgenticModel) Stream(ctx context.Context, input []*schema.AgenticMessage, opts ...model.Option) (*schema.StreamReader[*schema.AgenticMessage], error) {
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	reader, writer := schema.Pipe[*schema.AgenticMessage](1)
	go func() {
		defer writer.Close()
		writer.Send(msg, nil)
	}()
	return reader, nil
}

var _ model.BaseModel[*schema.AgenticMessage] = staticAgenticModel{}
