package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	adkskill "github.com/cloudwego/eino/adk/middlewares/skill"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestDynamicSkillMiddlewareEnablesImportedSkillOnLaterRun(t *testing.T) {
	ctx := context.Background()
	backend := &mutableSkillBackend{}
	middleware, err := newDynamicSkillMiddleware(ctx, backend)
	if err != nil {
		t.Fatalf("newDynamicSkillMiddleware() error = %v", err)
	}

	_, emptyRun, err := middleware.BeforeAgent(ctx, &adk.ChatModelAgentContext{Instruction: "base instruction"})
	if err != nil {
		t.Fatalf("empty BeforeAgent() error = %v", err)
	}
	if emptyRun.Instruction != "base instruction" || len(emptyRun.Tools) != 0 {
		t.Fatalf("empty backend injected Skill state: instruction=%q tools=%d", emptyRun.Instruction, len(emptyRun.Tools))
	}

	backend.skills = []adkskill.Skill{{
		FrontMatter: adkskill.FrontMatter{Name: "video", Description: "video creation"},
		Content:     "video skill instructions",
	}}
	_, importedRun, err := middleware.BeforeAgent(ctx, &adk.ChatModelAgentContext{Instruction: "base instruction"})
	if err != nil {
		t.Fatalf("imported BeforeAgent() error = %v", err)
	}
	if !strings.Contains(importedRun.Instruction, "Skill 系统") {
		t.Fatalf("imported Skill instruction missing: %q", importedRun.Instruction)
	}
	if len(importedRun.Tools) != 1 {
		t.Fatalf("imported run tools = %d, want one Skill loader", len(importedRun.Tools))
	}
	info, err := importedRun.Tools[0].Info(ctx)
	if err != nil {
		t.Fatalf("Skill tool Info() error = %v", err)
	}
	if info.Name != "skill" || !strings.Contains(info.Desc, "video creation") {
		t.Fatalf("Skill tool info = %#v", info)
	}
	if backend.listCalls != 3 {
		t.Fatalf("SkillBackend.List calls = %d, want two runs plus tool metadata refresh", backend.listCalls)
	}
}

func TestDynamicSkillMiddlewareRefreshesSameAgentBetweenRuns(t *testing.T) {
	ctx := context.Background()
	backend := &mutableSkillBackend{}
	middleware, err := newDynamicSkillMiddleware(ctx, backend)
	if err != nil {
		t.Fatal(err)
	}
	model := &skillRunCaptureModel{}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "dynamic-skill-agent", Description: "test", Instruction: "base instruction",
		Model: model, Handlers: []adk.ChatModelAgentMiddleware{middleware},
	})
	if err != nil {
		t.Fatal(err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})

	consumeSkillTestRun(t, runner.Query(ctx, "first turn"))
	backend.skills = []adkskill.Skill{{
		FrontMatter: adkskill.FrontMatter{Name: "video", Description: "video creation"},
		Content:     "video skill instructions",
	}}
	consumeSkillTestRun(t, runner.Query(ctx, "second turn"))

	if len(model.runs) != 2 {
		t.Fatalf("model runs = %d, want 2", len(model.runs))
	}
	if strings.Contains(model.runs[0].systemInstruction, "Skill 系统") || len(model.runs[0].tools) != 0 {
		t.Fatalf("empty first run exposed Skill: %#v", model.runs[0])
	}
	if !strings.Contains(model.runs[1].systemInstruction, "Skill 系统") {
		t.Fatalf("second run did not refresh Skill instruction: %q", model.runs[1].systemInstruction)
	}
	if len(model.runs[1].tools) != 1 || model.runs[1].tools[0].Name != "skill" {
		t.Fatalf("second run tools = %#v", model.runs[1].tools)
	}
}

func consumeSkillTestRun(t *testing.T, iter *adk.AsyncIterator[*adk.AgentEvent]) {
	t.Helper()
	for {
		event, ok := iter.Next()
		if !ok {
			return
		}
		if event.Err != nil {
			t.Fatalf("Agent run error = %v", event.Err)
		}
	}
}

type mutableSkillBackend struct {
	skills    []adkskill.Skill
	listCalls int
}

func (b *mutableSkillBackend) List(context.Context) ([]adkskill.FrontMatter, error) {
	b.listCalls++
	matters := make([]adkskill.FrontMatter, 0, len(b.skills))
	for _, item := range b.skills {
		matters = append(matters, item.FrontMatter)
	}
	return matters, nil
}

func (b *mutableSkillBackend) Get(_ context.Context, name string) (adkskill.Skill, error) {
	for _, item := range b.skills {
		if item.Name == name {
			return item, nil
		}
	}
	return adkskill.Skill{}, fmt.Errorf("skill %q not found", name)
}

type skillRunCapture struct {
	systemInstruction string
	tools             []*schema.ToolInfo
}

type skillRunCaptureModel struct {
	runs []skillRunCapture
}

func (m *skillRunCaptureModel) Generate(_ context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.Message, error) {
	run := skillRunCapture{}
	if len(input) > 0 && input[0].Role == schema.System {
		run.systemInstruction = input[0].Content
	}
	options := einomodel.GetCommonOptions(&einomodel.Options{}, opts...)
	run.tools = append([]*schema.ToolInfo(nil), options.Tools...)
	m.runs = append(m.runs, run)
	return schema.AssistantMessage("ok", nil), nil
}

func (m *skillRunCaptureModel) Stream(ctx context.Context, input []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}
