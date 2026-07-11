package agent

import (
	"context"
	"fmt"
	"strings"

	deepseekmodel "github.com/cloudwego/eino-ext/components/model/deepseek"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/patchtoolcalls"
	"github.com/cloudwego/eino/adk/middlewares/reduction"
	adkskill "github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcmw "github.com/FigoGoo/Dora-Agent/internal/aigc/middleware"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/modelreceipt"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

// DeepSeekRunnerConfig 汇总创建 AIGC ChatModelAgent Runner 所需的运行时依赖。
type DeepSeekRunnerConfig struct {
	Name              string
	Description       string
	Instruction       string
	Runtime           aigcconfig.Config
	ChatModel         einomodel.ToolCallingChatModel
	Registry          *aigctools.Registry
	SkillBackend      adkskill.Backend
	RunnerCheckpoints compose.CheckPointStore
	ModelReceiptStore modelreceipt.Store
	ExtraHandlers     []adk.ChatModelAgentMiddleware
}

const a2uiRetryInstruction = `上一轮最终 assistant 输出未通过 A2UI 1.0 协议或模型输出安全策略校验。请重新生成完整响应，不要解释、引用或复述上一轮内容；只输出一个 JSON object，顶层必须是 {"a2ui_version":"1.0","actions":[...]}。输出前检查所有括号、数组、字符串和逗号都完整匹配，并确保 append_card 包含以 Card 组件为 root 的 components。MultiChoice.value 如需提供必须是 JSON 字符串数组，SingleChoice.value 如需提供必须是字符串。禁止自行生成 Approval 卡或 decision/approved/rejected 审核单选项，禁止携带 approval_id，也禁止在 Card.message、Text 或 Markdown 中提示用户回复、发送或输入“确认/同意/拒绝”；真正的审批只能通过系统发布的权威审核卡提交。`

// NewDeepSeekChatModel 根据运行时配置创建 DeepSeek ToolCallingChatModel。
func NewDeepSeekChatModel(ctx context.Context, cfg aigcconfig.Config) (einomodel.ToolCallingChatModel, error) {
	cfg = cfg.Normalize()
	if err := cfg.ValidateDeepSeek(); err != nil {
		return nil, err
	}

	return deepseekmodel.NewChatModel(ctx, &deepseekmodel.ChatModelConfig{
		APIKey:      cfg.DeepSeek.APIKey,
		Model:       cfg.DeepSeek.Model,
		BaseURL:     cfg.DeepSeek.BaseURL,
		MaxTokens:   8192,
		Temperature: 0.2,
	})
}

// NewDeepSeekRunner 组装 ChatModelAgent、工具注册表、中间件和 checkpoint runner。
func NewDeepSeekRunner(ctx context.Context, cfg DeepSeekRunnerConfig) (*adk.Runner, error) {
	chatModel := cfg.ChatModel
	var err error
	if chatModel == nil {
		chatModel, err = NewDeepSeekChatModel(ctx, cfg.Runtime)
		if err != nil {
			return nil, fmt.Errorf("create deepseek chat model: %w", err)
		}
	}

	registry := cfg.Registry
	if registry == nil {
		return nil, fmt.Errorf("five-capability Agent registry is required")
	}
	if registry.Len() != len(capability.AgentToolKeys) {
		return nil, fmt.Errorf("Agent registry must contain exactly the five capability tools; got %d entries", registry.Len())
	}

	// This is a production invariant, not a caller preference: Provider,
	// prompt-preparation and business CRUD tools can never be injected into the
	// Agent's ReAct tool set through configuration.
	toolKeys := defaultAgentToolKeys()

	name := cfg.Name
	if name == "" {
		name = "AIGCChatModelAgent"
	}
	description := cfg.Description
	if description == "" {
		description = "AIGC content creation agent powered by DeepSeek and Eino ChatModelAgent."
	}
	instruction := cfg.Instruction
	if instruction == "" {
		instruction = `你是一个 AIGC 内容创作智能体。你只能使用 analyze_materials、plan_creation_spec、plan_storyboard、generate_media、assemble_output 五个用户可感知能力。
	先根据需要分析素材，再生成或修订创作规范；规范确认后才能创建或整体重规划故事板；故事板确认后才能生成素材；依赖齐备后才能合成。Tool 返回 waiting_user 时必须停止继续调用下游 Tool，等待用户通过系统发布的权威审核入口提交决定。不得创建重复的确认表单，不得提示用户在聊天中回复“确认”来代替 Approval Decision，也不得把普通聊天文本当成已完成审批。生成的候选素材统一在左侧故事板确认，不要为每个候选素材生成聊天审批卡或逐项确认提示。
故事板模块、元素类型与数量必须按用户场景动态推理，不套用固定短剧模板。提示词生成是 Graph 内部 ChatModel 节点，不存在 prepare_prompts Tool。用户在前端进行单元素提示词编辑、素材上传/填充、候选采用或局部重生成时，不要再调用 plan_storyboard；只有用户改变整体背景、目标或结构时才整体 replan。
用户明确说明没有参考素材、且当前会话也没有 asset_id 时，不要调用 analyze_materials，直接从 plan_creation_spec 开始。不要尝试调用积分、权限、资产 CRUD 或 Provider Tool；这些业务能力只允许在 Graph/Worker 内部执行。异步生成返回 accepted 后，只说明已受理和可见进度，不虚构尚未完成的素材地址、费用或结果。`
	}
	instruction = strings.TrimSpace(instruction + "\n\n" + a2ui.AgentInstruction())

	patchToolCalls, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create patch tool calls middleware: %w", err)
	}
	handlers := make([]adk.ChatModelAgentMiddleware, 0, 9+len(cfg.ExtraHandlers))
	// The normalizer sits outside the receipt wrapper: durable receipts retain
	// the exact provider output, while retry/projection sees one clean envelope.
	handlers = append(handlers, newA2UIOutputNormalizerMiddleware())
	// Freeze a provider's attempted duplicate ToolCall before replacing it with
	// a deterministic stop card for the Agent loop.
	handlers = append(handlers, newNextCapabilityRepeatGuardMiddleware())
	if cfg.ModelReceiptStore != nil {
		receiptMiddleware, err := NewModelReceiptMiddleware(ModelReceiptMiddlewareConfig{Store: cfg.ModelReceiptStore})
		if err != nil {
			return nil, fmt.Errorf("create model receipt middleware: %w", err)
		}
		// The inner receipt freezes raw output and ToolCall IDs before the outer
		// A2UI normalizer removes harmless final-response formatting noise.
		handlers = append(handlers, receiptMiddleware)
	}
	// Approved durable continuations carry a trusted machine directive. This
	// inner wrapper emits the one deterministic next ToolCall; an outer receipt
	// freezes it before execution, while later calls still reach DeepSeek.
	handlers = append(handlers, newNextCapabilityDirectiveMiddleware())
	handlers = append(handlers, patchToolCalls)
	contextHandlers, err := newContextControlMiddlewares(ctx, chatModel)
	if err != nil {
		return nil, err
	}
	handlers = append(handlers, contextHandlers...)
	handlers = append(handlers, cfg.ExtraHandlers...)
	if cfg.SkillBackend != nil {
		handler, err := newDynamicSkillMiddleware(ctx, cfg.SkillBackend)
		if err != nil {
			return nil, fmt.Errorf("create skill middleware: %w", err)
		}
		handlers = append(handlers, handler)
	}
	handlers = append(handlers, aigcmw.NewToolExceptionMiddleware[*schema.Message]())

	agentTools := registry.ListByKeys(toolKeys)
	if len(agentTools) != len(toolKeys) {
		return nil, fmt.Errorf("Agent registry resolved %d tools for %d requested capability keys", len(agentTools), len(toolKeys))
	}
	for index, agentTool := range agentTools {
		info, err := agentTool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("load Agent capability %s: %w", toolKeys[index], err)
		}
		if info == nil || info.Name != toolKeys[index] {
			return nil, fmt.Errorf("Agent capability key/name mismatch: key=%s", toolKeys[index])
		}
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:             name,
		Description:      description,
		Instruction:      instruction,
		Model:            chatModel,
		GenModelInput:    aigcGenModelInput,
		ModelRetryConfig: newA2UIModelRetryConfig(),
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: agentTools,
			},
		},
		Handlers: handlers,
	})
	if err != nil {
		return nil, fmt.Errorf("create deepseek chat model agent: %w", err)
	}

	return adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: false,
		CheckPointStore: cfg.RunnerCheckpoints,
	}), nil
}

// newA2UIModelRetryConfig rejects malformed final responses and model-authored
// Approval imitations. ToolCall messages pass through for the ReAct loop.
func newA2UIModelRetryConfig() *adk.ModelRetryConfig {
	return &adk.ModelRetryConfig{
		MaxRetries: 1,
		ShouldRetry: func(_ context.Context, retryCtx *adk.RetryContext) *adk.RetryDecision {
			if retryCtx == nil || retryCtx.Err != nil {
				return &adk.RetryDecision{Retry: false}
			}
			output := retryCtx.OutputMessage
			if output != nil && len(output.ToolCalls) > 0 {
				return &adk.RetryDecision{Retry: false}
			}
			rejectReason := "invalid_a2ui_action_envelope"
			if output != nil {
				if envelope, ok := a2ui.ParseActionEnvelopeContent(output.Content); ok {
					if a2ui.ValidateModelAuthoredActionEnvelope(envelope) == nil {
						return &adk.RetryDecision{Retry: false}
					}
					rejectReason = "forbidden_model_authored_approval"
				}
			}
			messages := append([]*schema.Message(nil), retryCtx.InputMessages...)
			messages = append(messages, schema.SystemMessage(a2uiRetryInstruction))
			return &adk.RetryDecision{
				Retry:                        true,
				ModifiedInputMessages:        messages,
				PersistModifiedInputMessages: false,
				RejectReason:                 rejectReason,
			}
		},
	}
}

// defaultAgentToolKeys 返回生产 Agent 唯一允许调用的五个高层能力。
func defaultAgentToolKeys() []string {
	return append([]string(nil), capability.AgentToolKeys...)
}

// aigcGenModelInput 把基础指令和可信内部 System 事件合并到消息窗口最前面。
// 部分 OpenAI-compatible Provider 对历史尾部的 System 消息服从性较弱；
// hoist 后仍保留所有非 System 会话消息的原始顺序，也不会把内部事件伪装成用户消息。
func aigcGenModelInput(_ context.Context, instruction string, input *adk.AgentInput) ([]*schema.Message, error) {
	if input == nil {
		input = &adk.AgentInput{}
	}
	systemParts := make([]string, 0, 2)
	if instruction = strings.TrimSpace(instruction); instruction != "" {
		systemParts = append(systemParts, instruction)
	}
	conversation := make([]*schema.Message, 0, len(input.Messages))
	for _, message := range input.Messages {
		if message == nil {
			continue
		}
		if message.Role == schema.System {
			if content := strings.TrimSpace(message.Content); content != "" {
				systemParts = append(systemParts, content)
			}
			continue
		}
		conversation = append(conversation, message)
	}
	msgs := make([]*schema.Message, 0, len(conversation)+1)
	if len(systemParts) > 0 {
		msgs = append(msgs, schema.SystemMessage(strings.Join(systemParts, "\n\n")))
	}
	msgs = append(msgs, conversation...)
	return msgs, nil
}

// newContextControlMiddlewares 创建上下文裁剪和摘要中间件，防止长会话撑爆模型窗口。
func newContextControlMiddlewares(ctx context.Context, model einomodel.BaseChatModel) ([]adk.ChatModelAgentMiddleware, error) {
	reductionMW, err := reduction.New(ctx, &reduction.Config{
		Backend:                   filesystem.NewInMemoryBackend(),
		MaxLengthForTrunc:         12000,
		MaxTokensForClear:         90000,
		ClearRetentionSuffixLimit: 8,
		ClearAtLeastTokens:        12000,
		TokenCounter:              reductionTokenCounter,
		TruncExcludeTools:         []string{capability.GenerateMediaToolKey},
		ClearExcludeTools:         []string{capability.GenerateMediaToolKey},
	})
	if err != nil {
		return nil, fmt.Errorf("create reduction middleware: %w", err)
	}

	summaryMW, err := summarization.New(ctx, &summarization.Config{
		Model: model,
		Trigger: &summarization.TriggerCondition{
			ContextTokens:   90000,
			ContextMessages: 80,
		},
		TokenCounter:    summarizationTokenCounter,
		UserInstruction: "请用中文总结 AIGC 创作会话，保留用户已确认的 Final Video Spec、故事板版本、角色/场景/镜头约束、素材生成状态、待确认事项和最近修改意图。",
	})
	if err != nil {
		return nil, fmt.Errorf("create summarization middleware: %w", err)
	}

	return []adk.ChatModelAgentMiddleware{reductionMW, summaryMW}, nil
}

// reductionTokenCounter 为 reduction 中间件提供粗略 token 估算。
func reductionTokenCounter(_ context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (int64, error) {
	return int64(estimatedContextTokens(messages, tools)), nil
}

// summarizationTokenCounter 为 summarization 触发条件提供粗略 token 估算。
func summarizationTokenCounter(_ context.Context, input *summarization.TokenCounterInput) (int, error) {
	if input == nil {
		return 0, nil
	}
	return estimatedContextTokens(input.Messages, input.Tools), nil
}

// estimatedContextTokens 用字符数近似估算上下文 token 数，避免引入额外 tokenizer 依赖。
func estimatedContextTokens(messages []*schema.Message, tools []*schema.ToolInfo) int {
	chars := 0
	for _, message := range messages {
		if message == nil {
			continue
		}
		chars += len([]rune(message.Content))
		chars += len(message.ToolCalls) * 64
		if message.ToolCallID != "" {
			chars += len([]rune(message.ToolCallID))
		}
		if message.ToolName != "" {
			chars += len([]rune(message.ToolName))
		}
	}
	for _, toolInfo := range tools {
		if toolInfo == nil {
			continue
		}
		chars += len([]rune(toolInfo.Name))
		chars += len([]rune(toolInfo.Desc))
	}
	if chars <= 0 {
		return 0
	}
	return chars/4 + 1
}
