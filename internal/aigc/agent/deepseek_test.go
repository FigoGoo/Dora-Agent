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
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

func TestNewDeepSeekChatModelRequiresAPIKey(t *testing.T) {
	_, err := NewDeepSeekChatModel(context.Background(), aigcconfig.Config{})
	if !errors.Is(err, aigcconfig.ErrMissingDeepSeekAPIKey) {
		t.Fatalf("NewDeepSeekChatModel() error = %v, want ErrMissingDeepSeekAPIKey", err)
	}
}

func TestNewDeepSeekRunnerRequiresCapabilityRegistry(t *testing.T) {
	_, err := NewDeepSeekRunner(context.Background(), DeepSeekRunnerConfig{
		Runtime: aigcconfig.Config{
			DeepSeek: aigcconfig.DeepSeekConfig{APIKey: "test-deepseek-key"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "registry is required") {
		t.Fatalf("NewDeepSeekRunner() error = %v", err)
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
	if strings.Contains(model.input[0].Content, "response_format.type=json_object") {
		t.Fatalf("system instruction still couples JSON Output to native Tool Calling: %s", model.input[0].Content)
	}
}

func TestAIGCGenModelInputHoistsTrustedSystemEvents(t *testing.T) {
	user := schema.UserMessage("创建短片")
	assistant := schema.AssistantMessage("旧阶段结果", nil)
	got, err := aigcGenModelInput(context.Background(), "基础系统指令", &adk.AgentInput{Messages: []*schema.Message{
		user,
		assistant,
		schema.SystemMessage("可信审批事件：creation_spec_revision 已 approved，必须调用 plan_storyboard。"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Role != schema.System {
		t.Fatalf("model input=%#v, want one leading system plus two conversation messages", got)
	}
	if !strings.Contains(got[0].Content, "基础系统指令") || !strings.Contains(got[0].Content, "可信审批事件") {
		t.Fatalf("hoisted system content=%q", got[0].Content)
	}
	if got[1] != user || got[2] != assistant {
		t.Fatalf("conversation order changed: %#v", got)
	}
	for _, message := range got[1:] {
		if message.Role == schema.System {
			t.Fatalf("trailing system message was not hoisted: %#v", got)
		}
	}
}

func TestA2UIModelRetryConfigRetriesOnlyInvalidFinalAssistant(t *testing.T) {
	config := newA2UIModelRetryConfig()
	baseInput := []*schema.Message{schema.UserMessage("创建短片")}
	valid := `{"a2ui_version":"1.0","actions":[{"type":"update_card","surface":"tool_runs","payload":{"status":"running"}}]}`
	invalid := `{"a2ui_version":"1.0","actions":[{"type":"append_card","card":{"root":"root","components":[{"id":"root","component":{"Card":{"children":[]}}}]}}}]}`

	decision := config.ShouldRetry(context.Background(), &adk.RetryContext{
		InputMessages: baseInput,
		OutputMessage: schema.AssistantMessage(invalid, nil),
	})
	if decision == nil || !decision.Retry || len(decision.ModifiedInputMessages) != 2 {
		t.Fatalf("invalid final decision = %#v", decision)
	}
	correction := decision.ModifiedInputMessages[len(decision.ModifiedInputMessages)-1]
	if correction.Role != schema.System || !strings.Contains(correction.Content, "A2UI 1.0") || strings.Contains(correction.Content, invalid) {
		t.Fatalf("correction message = %#v", correction)
	}
	if decision.PersistModifiedInputMessages {
		t.Fatalf("retry correction must not persist into Agent history")
	}

	pseudoApproval := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"storyboard","card":{"root":"root","components":[{"id":"root","component":{"Card":{"children":["details"]}}},{"id":"details","component":{"Markdown":{"value":"故事板已生成，请回复「确认」开始生成素材。"}}}]}}]}`
	decision = config.ShouldRetry(context.Background(), &adk.RetryContext{
		InputMessages: baseInput,
		OutputMessage: schema.AssistantMessage(pseudoApproval, nil),
	})
	if decision == nil || !decision.Retry || decision.RejectReason != "forbidden_model_authored_approval" {
		t.Fatalf("pseudo Approval decision = %#v", decision)
	}
	policyCorrection := decision.ModifiedInputMessages[len(decision.ModifiedInputMessages)-1]
	for _, expected := range []string{"禁止自行生成 Approval 卡", "禁止携带 approval_id", "权威审核卡"} {
		if !strings.Contains(policyCorrection.Content, expected) {
			t.Fatalf("policy correction missing %q: %s", expected, policyCorrection.Content)
		}
	}

	decision = config.ShouldRetry(context.Background(), &adk.RetryContext{
		InputMessages: baseInput,
		OutputMessage: schema.AssistantMessage(valid, nil),
	})
	if decision == nil || decision.Retry {
		t.Fatalf("valid final decision = %#v", decision)
	}

	decision = config.ShouldRetry(context.Background(), &adk.RetryContext{
		InputMessages: baseInput,
		OutputMessage: schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call-1", Type: "function", Function: schema.FunctionCall{Name: capability.PlanCreationSpecToolKey, Arguments: `{}`},
		}}),
	})
	if decision == nil || decision.Retry {
		t.Fatalf("ToolCall decision = %#v", decision)
	}
}

func TestA2UIModelRetryExhaustionDoesNotEmitPseudoApproval(t *testing.T) {
	pseudoApproval := `{"a2ui_version":"1.0","actions":[{"type":"append_card","surface":"chat","card_id":"storyboard","card":{"root":"root","components":[{"id":"root","component":{"Card":{"children":["details"]}}},{"id":"details","component":{"Markdown":{"value":"故事板已生成，请回复「确认」开始生成素材。"}}}]}}]}`
	underlying := &sequenceChatModel{outputs: []*schema.Message{
		schema.AssistantMessage(pseudoApproval, nil),
		schema.AssistantMessage(pseudoApproval, nil),
	}}
	agent, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name: "pseudo-approval-retry", Description: "test", Model: underlying,
		ModelRetryConfig: newA2UIModelRetryConfig(),
	})
	if err != nil {
		t.Fatal(err)
	}
	iter := adk.NewRunner(context.Background(), adk.RunnerConfig{Agent: agent}).Query(context.Background(), "生成故事板")
	var terminalErr error
	for {
		event, ok := iter.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			var retrying *adk.WillRetryError
			if errors.As(event.Err, &retrying) {
				continue
			}
			terminalErr = event.Err
			continue
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		message, messageErr := event.Output.MessageOutput.GetMessage()
		if messageErr != nil {
			terminalErr = messageErr
			continue
		}
		if message != nil && strings.Contains(message.Content, "回复「确认」") {
			t.Fatalf("pseudo Approval escaped bounded retry: %s", message.Content)
		}
	}
	if underlying.CallCount() != 2 {
		t.Fatalf("model calls = %d, want initial + one bounded retry", underlying.CallCount())
	}
	if terminalErr == nil {
		t.Fatal("retry exhaustion did not fail closed")
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

func TestNewDeepSeekRunnerDoesNotFallbackToLegacyToolsWithSkillBackend(t *testing.T) {
	_, err := NewDeepSeekRunner(context.Background(), DeepSeekRunnerConfig{
		Runtime: aigcconfig.Config{
			DeepSeek: aigcconfig.DeepSeekConfig{APIKey: "test-deepseek-key"},
		},
		SkillBackend: fakeSkillBackend{},
	})
	if err == nil || !strings.Contains(err.Error(), "registry is required") {
		t.Fatalf("NewDeepSeekRunner() error = %v", err)
	}
}

func TestNewDeepSeekRunnerRejectsRegistryWithExtraTool(t *testing.T) {
	registry := aigctools.NewRegistry()
	for _, key := range append(append([]string(nil), capability.AgentToolKeys...), "forbidden_business_tool") {
		if err := registry.Register(key, aigctools.EchoTool{}, aigctools.ToolMeta{Category: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := NewDeepSeekRunner(context.Background(), DeepSeekRunnerConfig{
		Runtime:  aigcconfig.Config{DeepSeek: aigcconfig.DeepSeekConfig{APIKey: "test-deepseek-key"}},
		Registry: registry,
	})
	if err == nil || !strings.Contains(err.Error(), "exactly the five capability tools") {
		t.Fatalf("NewDeepSeekRunner() error = %v", err)
	}
}

func TestNewDeepSeekRunnerRejectsCapabilityKeyAliasing(t *testing.T) {
	registry := aigctools.NewRegistry()
	for _, key := range capability.AgentToolKeys {
		if err := registry.Register(key, aigctools.EchoTool{}, aigctools.ToolMeta{Category: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := NewDeepSeekRunner(context.Background(), DeepSeekRunnerConfig{
		Runtime:  aigcconfig.Config{DeepSeek: aigcconfig.DeepSeekConfig{APIKey: "test-deepseek-key"}},
		Registry: registry,
	})
	if err == nil || !strings.Contains(err.Error(), "key/name mismatch") {
		t.Fatalf("NewDeepSeekRunner() error = %v", err)
	}
}

func TestDefaultAgentToolKeysExposeOnlyFiveCapabilities(t *testing.T) {
	keys := defaultAgentToolKeys()

	if len(keys) != len(capability.AgentToolKeys) {
		t.Fatalf("agent tools = %#v, want exactly %#v", keys, capability.AgentToolKeys)
	}
	for _, expected := range capability.AgentToolKeys {
		if !containsToolKey(keys, expected) {
			t.Fatalf("capability %s is missing: %#v", expected, keys)
		}
	}
	for _, internalTool := range []string{aigctools.WritePromptToolKey, aigctools.Image2GenerateToolKey, aigctools.SeedanceGenerateVideoToolKey} {
		if containsToolKey(keys, internalTool) {
			t.Fatalf("internal tool %s leaked: %#v", internalTool, keys)
		}
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
