package mediapreview

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// GenerateMediaTool 使用真实 Eino InvokableTool 包装启动期预编译 generate_media Graph。
type GenerateMediaTool struct {
	graph *GenerateMediaGraph
}

// NewGenerateMediaTool 创建 Tool；Graph 未编译时阻止 Registry 与 Readiness。
func NewGenerateMediaTool(graph *GenerateMediaGraph) (*GenerateMediaTool, error) {
	if graph == nil || graph.runnable == nil {
		return nil, fmt.Errorf("create generate_media tool: compiled graph is required")
	}
	return &GenerateMediaTool{graph: graph}, nil
}

// Info 返回不含身份、路径、Prompt 正文和执行参数的固定 Intent Schema。
func (*GenerateMediaTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: GenerateMediaToolKey,
		Desc: "为已冻结 Prompt Preview 的一个图片目标创建本地确定性 PNG 异步预览任务。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"schema_version": {
				Type: schema.String, Required: true, Enum: []string{GenerateMediaIntentVersion},
				Desc: "固定 Intent 版本。",
			},
			"prompt_preview_id": {
				Type: schema.String, Required: true, Desc: "Business Prompt Preview UUIDv7。",
			},
			"expected_prompt_version": {
				Type: schema.Integer, Required: true, Desc: "固定为 1。",
			},
			"expected_prompt_content_digest": {
				Type: schema.String, Required: true, Desc: "Prompt Preview lowercase SHA-256。",
			},
			"target_local_key": {
				Type: schema.String, Required: true, Desc: "Prompt Preview 内唯一图片目标键。",
			},
			"output_profile": {
				Type: schema.String, Required: true, Enum: []string{GenerateOutputProfile},
				Desc: "固定 PNG 输出规格。",
			},
		}),
	}, nil
}

// InvokableRun 严格解码、注入可信 Context 并执行预编译 Graph；不生成模型自由文本。
func (tool *GenerateMediaTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	intent, err := DecodeGenerateMediaIntent([]byte(argumentsInJSON))
	if err != nil {
		return "", err
	}
	encoded, err := CanonicalJSON(intent)
	if err != nil {
		return "", fmt.Errorf("encode generate_media intent: %w", err)
	}
	trusted, err := trustedContextFrom(ctx)
	if err != nil {
		return "", err
	}
	outcome, err := tool.graph.Invoke(ctx, GenerateMediaGraphInput{TrustedContext: trusted, IntentJSON: encoded})
	return finishToolOutcome(outcome, err)
}

// AssembleOutputTool 使用真实 Eino InvokableTool 包装启动期预编译 assemble_output Graph。
type AssembleOutputTool struct {
	graph *AssembleOutputGraph
}

// NewAssembleOutputTool 创建 Tool；Graph 未编译时阻止 Registry 与 Readiness。
func NewAssembleOutputTool(graph *AssembleOutputGraph) (*AssembleOutputTool, error) {
	if graph == nil || graph.runnable == nil {
		return nil, fmt.Errorf("create assemble_output tool: compiled graph is required")
	}
	return &AssembleOutputTool{graph: graph}, nil
}

// Info 返回只含 ready PNG exact ref 与固定 MP4 Profile 的 Intent Schema。
func (*AssembleOutputTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: AssembleOutputToolKey,
		Desc: "将同项目 ready PNG 装配为固定两秒 H.264 MP4 本地异步预览任务。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"schema_version": {
				Type: schema.String, Required: true, Enum: []string{AssembleOutputIntentVersion},
				Desc: "固定 Intent 版本。",
			},
			"source_asset_id": {
				Type: schema.String, Required: true, Desc: "同项目 ready PNG Asset UUIDv7。",
			},
			"expected_source_version": {
				Type: schema.Integer, Required: true, Desc: "固定为 1。",
			},
			"expected_source_content_digest": {
				Type: schema.String, Required: true, Desc: "ready PNG lowercase SHA-256。",
			},
			"output_profile": {
				Type: schema.String, Required: true, Enum: []string{AssembleOutputProfile},
				Desc: "固定 MP4 输出规格。",
			},
		}),
	}, nil
}

// InvokableRun 严格解码并执行预编译确定性 Graph；不接受 Timeline 或 ffmpeg 参数。
func (tool *AssembleOutputTool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	intent, err := DecodeAssembleOutputIntent([]byte(argumentsInJSON))
	if err != nil {
		return "", err
	}
	encoded, err := CanonicalJSON(intent)
	if err != nil {
		return "", fmt.Errorf("encode assemble_output intent: %w", err)
	}
	trusted, err := trustedContextFrom(ctx)
	if err != nil {
		return "", err
	}
	outcome, err := tool.graph.Invoke(ctx, AssembleOutputGraphInput{TrustedContext: trusted, IntentJSON: encoded})
	return finishToolOutcome(outcome, err)
}

// trustedContextFrom 只从 Runtime 私有 Context Key 映射身份，Intent 不能覆盖任何字段。
func trustedContextFrom(ctx context.Context) (TrustedContext, error) {
	value, ok := turncontext.MediaPreviewFrom(ctx)
	if !ok {
		return TrustedContext{}, fmt.Errorf("media preview trusted turn context is missing")
	}
	trusted := TrustedContext{
		RequestID: value.RequestID, IdempotencyKey: value.IdempotencyKey,
		UserID: value.UserID, ProjectID: value.ProjectID, SessionID: value.SessionID,
		InputID: value.InputID, TurnID: value.TurnID, RunID: value.RunID, ToolCallID: value.ToolCallID,
		FenceToken: value.FenceToken, DeadlineAt: value.DeadlineAt,
	}
	if ValidateTrustedContext(trusted) != nil {
		return TrustedContext{}, ErrInvalidArgument
	}
	return trusted, nil
}

// finishToolOutcome 编码唯一终态；recovery_pending 返回 error 使 Processor 保持 Input 未决。
func finishToolOutcome(outcome GraphOutcome, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if (outcome.Terminal == nil) == (outcome.Recovery == nil) {
		return "", fmt.Errorf("media preview graph returned invalid outcome union")
	}
	if outcome.Recovery != nil {
		return "", ErrUnknownOutcome
	}
	encoded, err := json.Marshal(*outcome.Terminal)
	if err != nil {
		return "", fmt.Errorf("encode media preview tool result: %w", err)
	}
	return string(encoded), nil
}

var _ einotool.InvokableTool = (*GenerateMediaTool)(nil)
var _ einotool.InvokableTool = (*AssembleOutputTool)(nil)
