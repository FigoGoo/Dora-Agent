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

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcmediagraph "github.com/FigoGoo/Dora-Agent/internal/aigc/mediagraph"
	aigcmw "github.com/FigoGoo/Dora-Agent/internal/aigc/middleware"
	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

type DeepSeekRunnerConfig struct {
	Name              string
	Description       string
	Instruction       string
	Runtime           aigcconfig.Config
	Registry          *aigctools.Registry
	ToolKeys          []string
	SkillBackend      adkskill.Backend
	RunnerCheckpoints compose.CheckPointStore
	MediaCheckpoints  compose.CheckPointStore
	MediaDispatcher   aigcmediagraph.JobDispatcher
	SpecStore         aigctools.FinalVideoSpecStore
	StoryboardStore   aigctools.StoryboardSnapshotStore
	AssetStore        aigctools.Image2AssetStore
	AssetUploader     aigctools.Image2AssetUploader
	ExtraHandlers     []adk.ChatModelAgentMiddleware
}

func NewDeepSeekChatModel(ctx context.Context, cfg aigcconfig.Config) (einomodel.ToolCallingChatModel, error) {
	cfg = cfg.Normalize()
	if err := cfg.ValidateDeepSeek(); err != nil {
		return nil, err
	}

	return deepseekmodel.NewChatModel(ctx, &deepseekmodel.ChatModelConfig{
		APIKey:  cfg.DeepSeek.APIKey,
		Model:   cfg.DeepSeek.Model,
		BaseURL: cfg.DeepSeek.BaseURL,
	})
}

func NewDeepSeekRunner(ctx context.Context, cfg DeepSeekRunnerConfig) (*adk.Runner, error) {
	chatModel, err := NewDeepSeekChatModel(ctx, cfg.Runtime)
	if err != nil {
		return nil, fmt.Errorf("create deepseek chat model: %w", err)
	}

	registry := cfg.Registry
	if registry == nil {
		registry, err = newRuntimeRegistry(cfg.Runtime, cfg.MediaCheckpoints, cfg.MediaDispatcher, cfg.SpecStore, cfg.StoryboardStore, cfg.AssetStore, cfg.AssetUploader, chatModel)
		if err != nil {
			return nil, err
		}
	}

	toolKeys := cfg.ToolKeys
	if len(toolKeys) == 0 {
		toolKeys = defaultAgentToolKeys(cfg.Runtime, cfg.SpecStore != nil, cfg.StoryboardStore != nil)
	}

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
		instruction = "你是一个 AIGC 内容创作智能体。根据用户需求规划内容、调用合适工具，并在关键阶段请求用户确认。"
	}
	instruction = strings.TrimSpace(instruction + "\n\n" + a2UIProtocolInstruction)

	patchToolCalls, err := patchtoolcalls.New(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("create patch tool calls middleware: %w", err)
	}
	handlers := []adk.ChatModelAgentMiddleware{patchToolCalls}
	contextHandlers, err := newContextControlMiddlewares(ctx, chatModel)
	if err != nil {
		return nil, err
	}
	handlers = append(handlers, contextHandlers...)
	handlers = append(handlers, cfg.ExtraHandlers...)
	if cfg.SkillBackend != nil {
		handler, err := adkskill.NewMiddleware(ctx, &adkskill.Config{
			Backend:    cfg.SkillBackend,
			UseChinese: true,
		})
		if err != nil {
			return nil, fmt.Errorf("create skill middleware: %w", err)
		}
		handlers = append(handlers, handler)
	}
	handlers = append(handlers, aigcmw.NewToolExceptionMiddleware[*schema.Message]())

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          name,
		Description:   description,
		Instruction:   instruction,
		Model:         chatModel,
		GenModelInput: aigcGenModelInput,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: registry.ListByKeys(toolKeys),
			},
		},
		Handlers: handlers,
	})
	if err != nil {
		return nil, fmt.Errorf("create deepseek chat model agent: %w", err)
	}

	return adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
		CheckPointStore: cfg.RunnerCheckpoints,
	}), nil
}

func defaultAgentToolKeys(runtime aigcconfig.Config, hasSpecStore bool, hasStoryboardStore bool) []string {
	_ = runtime.Normalize()
	toolKeys := []string{
		"echo_tool",
		aigctools.ResourcePrepareAnalyzeToolKey,
		aigctools.MultimodalAnalyzeToolKey,
		aigctools.WritePromptToolKey,
		aigctools.MediaGeneratorToolKey,
		aigctools.VideoAssemblerToolKey,
	}
	if hasSpecStore {
		toolKeys = append(toolKeys, aigctools.TextEditorToolKey)
	}
	if hasStoryboardStore {
		toolKeys = append(toolKeys, aigctools.StoryboardDesignerToolKey)
	}
	return toolKeys
}

func aigcGenModelInput(_ context.Context, instruction string, input *adk.AgentInput) ([]*schema.Message, error) {
	msgs := make([]*schema.Message, 0, len(input.Messages)+1)
	if instruction != "" {
		msgs = append(msgs, schema.SystemMessage(instruction))
	}
	msgs = append(msgs, input.Messages...)
	return msgs, nil
}

const a2UIProtocolInstruction = `
面向用户展示和交互必须走 A2UI 协议：
1. 普通说明可以简短输出；但凡需要用户确认、补充信息、单选、多选、填写文本、查看图片/视频、查看阶段进度，都必须输出纯 JSON，不要包 Markdown，不要解释 JSON。
2. JSON 顶层格式为 {"a2ui_events":[...]}，每个 event 包含 event、surface_id、data_model_key、payload。
3. 支持的 event：a2ui.begin_rendering、a2ui.surface_update、a2ui.data_model_update、a2ui.interrupt_request。
4. a2ui.surface_update 的 payload 可使用 components 组件树，组件支持 Text、Column、Row、Card、TextInput、SingleChoice、MultiChoice、ImagePreview、VideoPreview、VerticalSteps。
5. 组件示例：
{"a2ui_events":[{"event":"a2ui.surface_update","surface_id":"brief-intake","data_model_key":"brief","payload":{"root":"root","title":"补充产品信息","submit_label":"提交信息","components":[{"id":"root","component":{"Card":{"children":["title","product","style","platform","steps"]}}},{"id":"title","component":{"Text":{"value":"请补充商品宣传短片信息","usageHint":"title"}}},{"id":"product","component":{"TextInput":{"key":"product_name","label":"产品名称/品类","required":true}}},{"id":"style","component":{"SingleChoice":{"key":"visual_style","label":"视觉风格","options":[{"value":"tech","label":"高级科技感"},{"value":"warm","label":"温暖生活感"}]}}},{"id":"platform","component":{"MultiChoice":{"key":"platforms","label":"投放平台","options":[{"value":"douyin","label":"抖音"},{"value":"xiaohongshu","label":"小红书"}]}}},{"id":"steps","component":{"VerticalSteps":{"steps":[{"title":"Agent 分析","status":"running"},{"title":"资产配置","status":"pending"}]}}}]}}]}。
6. 图片预览组件使用 {"ImagePreview":{"url":"...","title":"参考图"}}；视频预览组件使用 {"VideoPreview":{"url":"...","poster":"...","title":"预览视频"}}。
7. 不要把生成模型原始大结果、base64、长 prompt 全量放入 A2UI 或普通回答；只返回业务摘要、asset_id、url、状态和需要用户决策的信息。
`

func newDefaultRegistry() (*aigctools.Registry, error) {
	registry := aigctools.NewRegistry()
	if err := registry.Register("echo_tool", aigctools.EchoTool{}, aigctools.ToolMeta{
		Category:    "demo",
		StageHints:  []string{"phase_0"},
		OutputKinds: []string{"text"},
		Provider:    "local",
	}); err != nil {
		return nil, err
	}
	return registry, nil
}

func newContextControlMiddlewares(ctx context.Context, model einomodel.BaseChatModel) ([]adk.ChatModelAgentMiddleware, error) {
	reductionMW, err := reduction.New(ctx, &reduction.Config{
		Backend:                   filesystem.NewInMemoryBackend(),
		MaxLengthForTrunc:         12000,
		MaxTokensForClear:         90000,
		ClearRetentionSuffixLimit: 8,
		ClearAtLeastTokens:        12000,
		TokenCounter:              reductionTokenCounter,
		TruncExcludeTools:         []string{aigctools.MediaGeneratorToolKey},
		ClearExcludeTools:         []string{aigctools.MediaGeneratorToolKey},
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

func reductionTokenCounter(_ context.Context, messages []*schema.Message, tools []*schema.ToolInfo) (int64, error) {
	return int64(estimatedContextTokens(messages, tools)), nil
}

func summarizationTokenCounter(_ context.Context, input *summarization.TokenCounterInput) (int, error) {
	if input == nil {
		return 0, nil
	}
	return estimatedContextTokens(input.Messages, input.Tools), nil
}

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

func newRuntimeRegistry(runtime aigcconfig.Config, mediaCheckpoints compose.CheckPointStore, mediaDispatcher aigcmediagraph.JobDispatcher, specStore aigctools.FinalVideoSpecStore, storyboardStore aigctools.StoryboardSnapshotStore, assetStore aigctools.Image2AssetStore, assetUploader aigctools.Image2AssetUploader, promptModel einomodel.BaseChatModel) (*aigctools.Registry, error) {
	registry, err := newDefaultRegistry()
	if err != nil {
		return nil, err
	}

	if specStore != nil {
		if err := registry.Register(aigctools.TextEditorToolKey, aigctools.NewTextEditorTool(aigctools.TextEditorToolConfig{
			Specs: specStore,
		}), aigctools.ToolMeta{
			Category:    "text_editor",
			StageHints:  []string{"final_video_spec", "spec_review", "revision"},
			OutputKinds: []string{"final_video_spec", "markdown", "versioned_document"},
			Provider:    "local_postgres",
		}); err != nil {
			return nil, err
		}
	}

	if storyboardStore != nil {
		if err := registry.Register(aigctools.StoryboardDesignerToolKey, aigctools.NewStoryboardDesignerTool(aigctools.StoryboardDesignerToolConfig{
			Storyboards: storyboardStore,
		}), aigctools.ToolMeta{
			Category:    "storyboard_designer",
			StageHints:  []string{"storyboard", "key_elements", "shots", "audio_layers", "revision"},
			OutputKinds: []string{"storyboard_snapshot", "key_elements", "shot_list", "audio_layers"},
			Provider:    "local_postgres",
		}); err != nil {
			return nil, err
		}
	}

	if err := registry.Register(aigctools.MediaGeneratorToolKey, aigctools.NewMediaGeneratorTool(aigctools.MediaGeneratorToolConfig{
		Checkpoints: mediaCheckpoints,
		Dispatcher:  mediaDispatcher,
	}), aigctools.ToolMeta{
		Category:    "media_generator",
		StageHints:  []string{"key_elements", "shot_assets", "storyboard_assets", "reference_confirm"},
		OutputKinds: []string{"interrupt_request", "job_plan", "storyboard_patch"},
		Provider:    "eino_graph",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(aigctools.ResourcePrepareAnalyzeToolKey, aigctools.ResourcePrepareAnalyzeTool{}, aigctools.ToolMeta{
		Category:    "resource_prepare",
		StageHints:  []string{"resource_prepare", "multimodal_analyze", "script_upload"},
		OutputKinds: []string{"resource_analysis", "asset_requirements"},
		Provider:    "local_demo",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(aigctools.MultimodalAnalyzeToolKey, aigctools.MultimodalAnalyzeTool{}, aigctools.ToolMeta{
		Category:    "multimodal_analyze",
		StageHints:  []string{"multimodal_analyze", "reference_assets", "material_analysis"},
		OutputKinds: []string{"resource_analysis", "reference_summary"},
		Provider:    "local_demo",
	}); err != nil {
		return nil, err
	}
	var promptSpecs aigctools.PromptSpecStore
	if typed, ok := specStore.(aigctools.PromptSpecStore); ok {
		promptSpecs = typed
	}
	var promptStoryboards aigctools.PromptStoryboardStore
	if typed, ok := storyboardStore.(aigctools.PromptStoryboardStore); ok {
		promptStoryboards = typed
	}
	if err := registry.Register(aigctools.WritePromptToolKey, aigctools.NewWritePromptTool(aigctools.WritePromptToolConfig{
		Model:       promptModel,
		Specs:       promptSpecs,
		Storyboards: promptStoryboards,
	}), aigctools.ToolMeta{
		Category:    "prompt_generation",
		StageHints:  []string{"write_the_prompt", "shot_prompt", "asset_prompt"},
		OutputKinds: []string{"prompt_reference", "storyboard_patch", "prompt_ready"},
		Provider:    "deepseek",
	}); err != nil {
		return nil, err
	}
	if err := registry.Register(aigctools.VideoAssemblerToolKey, aigctools.VideoAssemblerTool{}, aigctools.ToolMeta{
		Category:    "video_assembler",
		StageHints:  []string{"final_assembly", "export"},
		OutputKinds: []string{"assembly_plan", "export_status"},
		Provider:    "local_demo",
	}); err != nil {
		return nil, err
	}

	runtime = runtime.Normalize()
	if runtime.Image2.APIKey != "" {
		if err := registry.Register(aigctools.Image2GenerateToolKey, aigctools.NewImage2GenerateTool(aigctools.Image2ToolConfig{
			APIKey:        runtime.Image2.APIKey,
			Assets:        assetStore,
			AssetUploader: assetUploader,
		}), aigctools.ToolMeta{
			Category:    "media_generator",
			StageHints:  []string{"key_elements", "shot_assets", "storyboard_assets"},
			OutputKinds: []string{"image", "asset_id", "url", "data_url", "b64_json"},
			Provider:    "image2",
		}); err != nil {
			return nil, err
		}
	}
	if runtime.Seedance.APIKey != "" {
		if err := registry.Register(aigctools.SeedanceGenerateVideoToolKey, aigctools.NewSeedanceGenerateTool(aigctools.SeedanceToolConfig{
			APIKey:        runtime.Seedance.APIKey,
			Assets:        assetStore,
			AssetUploader: assetUploader,
		}), aigctools.ToolMeta{
			Category:    "media_generator",
			StageHints:  []string{"shot_video", "storyboard_assets", "video_generation"},
			OutputKinds: []string{"video", "asset_id", "url", "provider_task_id"},
			Provider:    "seedance",
		}); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
