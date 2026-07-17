package planstoryboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	jsonschema "github.com/eino-contrib/jsonschema"
)

// Tool 用真实 Eino InvokableTool 包装启动期预编译 Graph，但默认不进入静态生产 Catalog。
type Tool struct {
	graph    *CompiledGraph
	resolver TrustedContextResolver
}

// NewTool 创建默认不注册的 plan_storyboard Preview Tool Core。
func NewTool(graph *CompiledGraph, resolver TrustedContextResolver) (*Tool, error) {
	if graph == nil || graph.runnable == nil || resolver == nil {
		return nil, fmt.Errorf("create plan_storyboard tool: compiled graph and trusted context resolver are required")
	}
	return &Tool{graph: graph, resolver: resolver}, nil
}

// Info 返回不含 CreationSpecRef、身份、命令、Fence 或任何 Prompt 字段的严格 Tool Schema。
func (*Tool) Info(context.Context) (*schema.ToolInfo, error) {
	params := schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"schema_version": {
			Type: schema.String, Required: true, Enum: []string{IntentSchemaVersion}, Desc: "固定 Intent Schema 版本。",
		},
		"planning_instruction": {
			Type: schema.String, Required: true, Desc: "一至一千字符的 NFC Storyboard 规划要求。",
		},
		"target_duration_seconds": {
			Type: schema.Integer, Required: false, Desc: "可选的五至六百秒目标总时长。",
		},
	})
	strictSchema, err := params.ToJSONSchema()
	if err != nil {
		return nil, fmt.Errorf("build plan_storyboard tool schema: %w", err)
	}
	strictSchema.AdditionalProperties = jsonschema.FalseSchema
	planningInstruction, _ := strictSchema.Properties.Get("planning_instruction")
	minimumLength := uint64(intentInstructionMin)
	maximumLength := uint64(intentInstructionMax)
	planningInstruction.MinLength = &minimumLength
	planningInstruction.MaxLength = &maximumLength
	targetDuration, _ := strictSchema.Properties.Get("target_duration_seconds")
	targetDuration.Minimum = json.Number(fmt.Sprintf("%d", intentTargetMin))
	targetDuration.Maximum = json.Number(fmt.Sprintf("%d", intentTargetMax))
	return &schema.ToolInfo{
		Name:        ToolKey,
		Desc:        "基于 Runtime 已绑定的 CreationSpec Draft 规划严格结构化 Storyboard 开发预览草稿。",
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(strictSchema),
	}, nil
}

// InvokableRun 严格解码模型参数、注入 Runtime 私有上下文并执行预编译 Graph。
func (t *Tool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	intent, err := DecodeIntent([]byte(argumentsInJSON))
	if err != nil {
		return "", err
	}
	canonical, err := json.Marshal(intent)
	if err != nil {
		return "", fmt.Errorf("run plan_storyboard tool: encode strict intent: %w", err)
	}
	trusted, ok := t.resolver(ctx)
	if !ok {
		return "", fmt.Errorf("run plan_storyboard tool: trusted turn context is missing")
	}
	outcome, err := t.graph.Invoke(ctx, GraphInput{TrustedContext: trusted, IntentJSON: canonical})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, ErrBusinessUnknownOutcome) || errors.Is(err, ErrBusinessTechnical) {
			return "", err
		}
		code := ResultCodeInternal
		switch {
		case errors.Is(err, ErrBusinessNotFound):
			code = ResultCodeCreationSpecNotFound
		case errors.Is(err, ErrBusinessCreationSpecConflict):
			code = ResultCodeCreationSpecConflict
		case errors.Is(err, ErrBusinessConflict):
			code = ResultCodeBusinessConflict
		case errors.Is(err, ErrBusinessDisabled):
			code = ResultCodeBusinessDisabled
		}
		result := failedResult(trusted, code)
		encoded, encodeErr := json.Marshal(result)
		if encodeErr != nil {
			return "", fmt.Errorf("run plan_storyboard tool: encode failed result: %w", encodeErr)
		}
		return string(encoded), nil
	}
	if (outcome.Terminal == nil) == (outcome.Recovery == nil) {
		return "", fmt.Errorf("run plan_storyboard tool: invalid graph outcome union")
	}
	if outcome.Recovery != nil {
		return "", ErrBusinessUnknownOutcome
	}
	if err := ValidateTerminalResult(*outcome.Terminal, trusted); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(*outcome.Terminal)
	if err != nil {
		return "", fmt.Errorf("run plan_storyboard tool: encode result: %w", err)
	}
	return string(encoded), nil
}

var _ einotool.InvokableTool = (*Tool)(nil)
