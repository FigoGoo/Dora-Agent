package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	adkskill "github.com/cloudwego/eino/adk/middlewares/skill"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

func TestNewDeepSeekChatModelRequiresAPIKey(t *testing.T) {
	_, err := NewDeepSeekChatModel(context.Background(), aigcconfig.Config{})
	if !errors.Is(err, aigcconfig.ErrMissingDeepSeekAPIKey) {
		t.Fatalf("NewDeepSeekChatModel() error = %v, want ErrMissingDeepSeekAPIKey", err)
	}
}

func TestNewDeepSeekRunnerBuildsWithFakeKey(t *testing.T) {
	runner, err := NewDeepSeekRunner(context.Background(), DeepSeekRunnerConfig{
		Runtime: aigcconfig.Config{
			DeepSeek: aigcconfig.DeepSeekConfig{APIKey: "test-deepseek-key"},
		},
	})
	if err != nil {
		t.Fatalf("NewDeepSeekRunner() error = %v", err)
	}
	if runner == nil {
		t.Fatalf("NewDeepSeekRunner() returned nil runner")
	}
}

func TestAIGCGenModelInputKeepsLiteralA2UIJSONWithSessionValues(t *testing.T) {
	ctx := context.Background()
	model := &captureInputModel{}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "test-aigc-agent",
		Description:   "test",
		Instruction:   a2ui.AgentInstruction(),
		Model:         model,
		GenModelInput: aigcGenModelInput,
	})
	if err != nil {
		t.Fatalf("NewChatModelAgent() error = %v", err)
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})

	iter := runner.Query(ctx, "开始", adk.WithSessionValues(map[string]any{"session_id": "s1"}))
	event, ok := iter.Next()
	if !ok {
		t.Fatalf("expected agent event")
	}
	if event.Err != nil {
		t.Fatalf("event error = %v", event.Err)
	}
	if len(model.input) < 2 {
		t.Fatalf("model input length = %d, want at least 2", len(model.input))
	}
	if model.input[0].Role != schema.System {
		t.Fatalf("first model message role = %s, want system", model.input[0].Role)
	}
	if !strings.Contains(model.input[0].Content, `{"a2ui_version":"1.0","actions":[`) {
		t.Fatalf("system instruction lost literal A2UI JSON: %s", model.input[0].Content)
	}
	if strings.Contains(model.input[0].Content, `{"a2ui_events":[`) {
		t.Fatalf("system instruction still teaches legacy A2UI JSON: %s", model.input[0].Content)
	}
}

func TestEstimatedContextTokensIncludesMessagesAndTools(t *testing.T) {
	got := estimatedContextTokens([]*schema.Message{
		schema.UserMessage("生成一个两分钟武侠短片"),
		schema.AssistantMessage("我会先规划 Final Video Spec 和故事板。", nil),
	}, []*schema.ToolInfo{{
		Name: "media_generator",
		Desc: "Run media generation graph.",
	}})
	if got <= 0 {
		t.Fatalf("estimatedContextTokens() = %d", got)
	}
}

func TestNewDeepSeekRunnerBuildsWithSkillBackend(t *testing.T) {
	runner, err := NewDeepSeekRunner(context.Background(), DeepSeekRunnerConfig{
		Runtime: aigcconfig.Config{
			DeepSeek: aigcconfig.DeepSeekConfig{APIKey: "test-deepseek-key"},
		},
		SkillBackend: fakeSkillBackend{},
	})
	if err != nil {
		t.Fatalf("NewDeepSeekRunner() error = %v", err)
	}
	if runner == nil {
		t.Fatalf("NewDeepSeekRunner() returned nil runner")
	}
}

func TestNewRuntimeRegistryIncludesImage2WhenConfigured(t *testing.T) {
	registry, err := newRuntimeRegistry(aigcconfig.Config{
		Image2: aigcconfig.ProviderConfig{APIKey: "test-image2-key"},
	}, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("newRuntimeRegistry() error = %v", err)
	}
	if _, ok := registry.Get(aigctools.Image2GenerateToolKey); !ok {
		t.Fatalf("expected image2 tool to be registered")
	}
}

func TestNewRuntimeRegistryIncludesSeedanceWhenConfigured(t *testing.T) {
	registry, err := newRuntimeRegistry(aigcconfig.Config{
		Seedance: aigcconfig.ProviderConfig{APIKey: "test-seedance-key"},
	}, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("newRuntimeRegistry() error = %v", err)
	}
	if _, ok := registry.Get(aigctools.SeedanceGenerateVideoToolKey); !ok {
		t.Fatalf("expected seedance video tool to be registered")
	}
}

func TestNewRuntimeRegistryIncludesMediaGenerator(t *testing.T) {
	registry, err := newRuntimeRegistry(aigcconfig.Config{}, nil, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("newRuntimeRegistry() error = %v", err)
	}
	if _, ok := registry.Get(aigctools.MediaGeneratorToolKey); !ok {
		t.Fatalf("expected media generator tool to be registered")
	}
	for _, toolKey := range []string{
		aigctools.ResourcePrepareAnalyzeToolKey,
		aigctools.MultimodalAnalyzeToolKey,
		aigctools.WritePromptToolKey,
		aigctools.VideoAssemblerToolKey,
	} {
		if _, ok := registry.Get(toolKey); !ok {
			t.Fatalf("expected %s tool to be registered", toolKey)
		}
	}
}

func TestDefaultAgentToolKeysPreferMediaGraphOverProviderTools(t *testing.T) {
	keys := defaultAgentToolKeys(aigcconfig.Config{
		Image2:   aigcconfig.ProviderConfig{APIKey: "image-key"},
		Seedance: aigcconfig.ProviderConfig{APIKey: "seedance-key"},
	}, true, true)

	if !containsToolKey(keys, aigctools.MediaGeneratorToolKey) {
		t.Fatalf("expected media generator in agent tools: %#v", keys)
	}
	for _, providerTool := range []string{aigctools.Image2GenerateToolKey, aigctools.SeedanceGenerateVideoToolKey} {
		if containsToolKey(keys, providerTool) {
			t.Fatalf("provider tool %s should not be exposed by default: %#v", providerTool, keys)
		}
	}
}

func TestNewRuntimeRegistryIncludesPlanningToolsWhenStoresConfigured(t *testing.T) {
	registry, err := newRuntimeRegistry(aigcconfig.Config{}, nil, nil, fakeSpecStore{}, fakeStoryboardStore{}, nil, nil, nil)
	if err != nil {
		t.Fatalf("newRuntimeRegistry() error = %v", err)
	}
	if _, ok := registry.Get(aigctools.TextEditorToolKey); !ok {
		t.Fatalf("expected text editor tool to be registered")
	}
	if _, ok := registry.Get(aigctools.StoryboardDesignerToolKey); !ok {
		t.Fatalf("expected storyboard designer tool to be registered")
	}
}

func containsToolKey(keys []string, want string) bool {
	for _, key := range keys {
		if key == want {
			return true
		}
	}
	return false
}

type fakeSkillBackend struct{}

func (fakeSkillBackend) List(context.Context) ([]adkskill.FrontMatter, error) {
	return []adkskill.FrontMatter{{Name: "video", Description: "video creation"}}, nil
}

func (fakeSkillBackend) Get(context.Context, string) (adkskill.Skill, error) {
	return adkskill.Skill{
		FrontMatter: adkskill.FrontMatter{Name: "video", Description: "video creation"},
		Content:     "video skill",
	}, nil
}

type fakeSpecStore struct{}

func (fakeSpecStore) Save(_ context.Context, in spec.FinalVideoSpec) (spec.FinalVideoSpec, error) {
	return in, nil
}

type fakeStoryboardStore struct{}

func (fakeStoryboardStore) SaveSnapshot(_ context.Context, _ storyboard.Storyboard) error {
	return nil
}

type captureInputModel struct {
	input []*schema.Message
}

func (m *captureInputModel) Generate(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.input = input
	return schema.AssistantMessage("ok", nil), nil
}

func (m *captureInputModel) Stream(_ context.Context, input []*schema.Message, _ ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	m.input = input
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("ok", nil)}), nil
}
