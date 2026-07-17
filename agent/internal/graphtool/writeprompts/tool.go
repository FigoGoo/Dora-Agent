package writeprompts

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

// NewTool 创建默认不注册的 write_prompts Preview Tool Core。
func NewTool(graph *CompiledGraph, resolver TrustedContextResolver) (*Tool, error) {
	if graph == nil || graph.runnable == nil || graph.clock == nil || resolver == nil {
		return nil, fmt.Errorf("create write_prompts tool: compiled graph and trusted context resolver are required")
	}
	return &Tool{graph: graph, resolver: resolver}, nil
}

// Info 返回不含 Storyboard 引用、身份、目标、命令、Fence 或任何 Prompt 字段的严格 Tool Schema。
func (*Tool) Info(context.Context) (*schema.ToolInfo, error) {
	params := schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"schema_version": {
			Type: schema.String, Required: true, Enum: []string{IntentSchemaVersion}, Desc: "固定 Intent Schema 版本。",
		},
		"writing_instruction": {
			Type: schema.String, Required: true, Desc: "一至一千字符的 NFC Prompt 写作要求。",
		},
		"output_language": {
			Type: schema.String, Required: false, Enum: []string{"zh-CN", "en-US"}, Desc: "可选输出语言，缺省由冻结 Runtime Policy 决定。",
		},
	})
	strictSchema, err := params.ToJSONSchema()
	if err != nil {
		return nil, fmt.Errorf("build write_prompts tool schema: %w", err)
	}
	strictSchema.AdditionalProperties = jsonschema.FalseSchema
	writingInstruction, _ := strictSchema.Properties.Get("writing_instruction")
	minimumLength := uint64(intentInstructionMin)
	maximumLength := uint64(intentInstructionMax)
	writingInstruction.MinLength = &minimumLength
	writingInstruction.MaxLength = &maximumLength
	return &schema.ToolInfo{
		Name:        ToolKey,
		Desc:        "基于 Runtime 已绑定的 Storyboard Preview Draft 全部 Slot 编写严格结构化 Prompt 开发预览草稿。",
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(strictSchema),
	}, nil
}

// InvokableRun 严格解码模型参数、注入 Runtime 私有上下文并执行预编译 Graph。
func (t *Tool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	trusted, ok := t.resolver(ctx)
	if !ok {
		return "", fmt.Errorf("run write_prompts tool: trusted turn context is missing")
	}
	intent, err := DecodeIntent([]byte(argumentsInJSON))
	if err != nil {
		result := failedResult(trusted, ResultCodeInvalidArgument, t.graph.clock.Now())
		if validateErr := ValidateTerminalResult(result, trusted); validateErr != nil {
			return "", validateErr
		}
		return encodeTerminalResult(result)
	}
	canonical, err := json.Marshal(intent)
	if err != nil {
		return "", fmt.Errorf("run write_prompts tool: encode strict intent: %w", err)
	}
	outcome, err := t.graph.Invoke(ctx, GraphInput{TrustedContext: trusted, IntentJSON: canonical})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(err, ErrBusinessUnknownOutcome) || errors.Is(err, ErrBusinessTechnical) {
			return "", err
		}
		code := ""
		switch {
		case errors.Is(err, ErrBusinessNotFound):
			code = ResultCodeStoryboardNotFound
		case errors.Is(err, ErrBusinessStoryboardConflict):
			code = ResultCodeStoryboardConflict
		case errors.Is(err, ErrBusinessConflict):
			code = ResultCodeBusinessConflict
		case errors.Is(err, ErrBusinessDisabled):
			code = ResultCodeBusinessDisabled
		}
		if code == "" {
			return "", err
		}
		result := failedResult(trusted, code, t.graph.clock.Now())
		if validateErr := ValidateTerminalResult(result, trusted); validateErr != nil {
			return "", validateErr
		}
		return encodeTerminalResult(result)
	}
	if (outcome.Terminal == nil) == (outcome.Recovery == nil) {
		return "", fmt.Errorf("run write_prompts tool: invalid graph outcome union")
	}
	if outcome.Recovery != nil {
		return "", ErrBusinessUnknownOutcome
	}
	if err := ValidateTerminalResult(*outcome.Terminal, trusted); err != nil {
		return "", err
	}
	return encodeTerminalResult(*outcome.Terminal)
}

// encodeTerminalResult 只序列化 Tool Result exact-set；内部 Card 由自定义 MarshalJSON 永久排除。
func encodeTerminalResult(result Result) (string, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("run write_prompts tool: encode result: %w", err)
	}
	return string(encoded), nil
}

var _ einotool.InvokableTool = (*Tool)(nil)
