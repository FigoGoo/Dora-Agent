package analyzematerials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/FigoGoo/Dora-Agent/agent/internal/turncontext"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	jsonschema "github.com/eino-contrib/jsonschema"
)

const canonicalUUIDv7Pattern = `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`

// graphInvoker 是 Tool 包装层消费的最小预编译 Graph 接口；生产构造函数只接受 CompiledGraph。
type graphInvoker interface {
	Invoke(context.Context, GraphInput) (Outcome, error)
}

// Tool 使用真实 Eino InvokableTool 包装启动期预编译的 analyze_materials V2 Tool Core Graph。
// Tool 不持久化回执、不写 Business，也不进入当前可执行 Registry。
type Tool struct {
	graph graphInvoker
}

// NewTool 创建默认不注册的素材分析开发预览 Tool；空 Graph 会在装配阶段失败关闭。
func NewTool(graph *CompiledGraph) (*Tool, error) {
	if graph == nil || graph.runnable == nil {
		return nil, fmt.Errorf("create analyze_materials tool: compiled graph is required")
	}
	return &Tool{graph: graph}, nil
}

// Info 返回只包含模型可控 Intent 的固定严格 Schema；可信身份、Evidence 和内部策略不能由模型填写。
func (t *Tool) Info(context.Context) (*schema.ToolInfo, error) {
	params := schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"schema_version": {
			Type: schema.String, Required: true, Enum: []string{IntentSchemaVersion},
			Desc: "固定 Intent Schema 版本。",
		},
		"asset_ids": {
			Type: schema.Array, Required: true, ElemInfo: &schema.ParameterInfo{Type: schema.String},
			Desc: "一至八个规范小写 UUIDv7 Asset ID；拒绝重复。",
		},
		"analysis_goal": {
			Type: schema.String, Required: true,
			Desc: "一至一千字符的素材分析目标。",
		},
		"focus_dimensions": {
			Type: schema.Array, Required: true,
			ElemInfo: &schema.ParameterInfo{
				Type: schema.String, Enum: []string{"content", "visual", "narrative", "risk"},
			},
			Desc: "一至四个精确去重的分析维度。",
		},
		"output_language": {
			Type: schema.String, Required: true, Enum: []string{"zh-CN", "en-US"},
			Desc: "分析结果语言。",
		},
		"expected_assets": {
			Type: schema.Array, Required: false,
			ElemInfo: &schema.ParameterInfo{
				Type: schema.Object,
				SubParams: map[string]*schema.ParameterInfo{
					"asset_id": {
						Type: schema.String, Required: true,
						Desc: "必须来自 asset_ids exact-set 的规范小写 UUIDv7。",
					},
					"asset_version": {
						Type: schema.Integer, Required: true,
						Desc: "大于等于一的预期 Asset 版本。",
					},
				},
			},
			Desc: "可选 Asset 乐观版本 exact-set；存在时必须与 asset_ids 完全一致。",
		},
	})
	strictSchema, err := params.ToJSONSchema()
	if err != nil {
		return nil, fmt.Errorf("build analyze_materials tool schema: %w", err)
	}
	applyStrictIntentSchema(strictSchema)
	return &schema.ToolInfo{
		Name:        ToolKey,
		Desc:        "基于用户已授权素材的持久化摘要 Evidence，生成带可验证引用的非权威素材分析开发预览。",
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(strictSchema),
	}, nil
}

// applyStrictIntentSchema 补充 ParameterInfo 无法表达的边界，并递归关闭未知字段。
func applyStrictIntentSchema(root *jsonschema.Schema) {
	root.AdditionalProperties = jsonschema.FalseSchema
	assetIDs, _ := root.Properties.Get("asset_ids")
	setArrayBounds(assetIDs, 1, maxAssets)
	assetIDs.Items.Pattern = canonicalUUIDv7Pattern

	analysisGoal, _ := root.Properties.Get("analysis_goal")
	setStringBounds(analysisGoal, 1, 1000)

	focusDimensions, _ := root.Properties.Get("focus_dimensions")
	setArrayBounds(focusDimensions, 1, 4)

	expectedAssets, _ := root.Properties.Get("expected_assets")
	setArrayBounds(expectedAssets, 1, maxAssets)
	expectedAssets.Items.AdditionalProperties = jsonschema.FalseSchema
	expectedAssetID, _ := expectedAssets.Items.Properties.Get("asset_id")
	expectedAssetID.Pattern = canonicalUUIDv7Pattern
	expectedAssetVersion, _ := expectedAssets.Items.Properties.Get("asset_version")
	expectedAssetVersion.Minimum = json.Number("1")
}

// setArrayBounds 为 Info Schema 冻结数组计数和 exact-set 去重约束。
func setArrayBounds(value *jsonschema.Schema, minimum int, maximum int) {
	minItems := uint64(minimum)
	maxItems := uint64(maximum)
	value.MinItems = &minItems
	value.MaxItems = &maxItems
	value.UniqueItems = true
}

// setStringBounds 为 Info Schema 冻结 Unicode 字符长度边界。
func setStringBounds(value *jsonschema.Schema, minimum int, maximum int) {
	minLength := uint64(minimum)
	maxLength := uint64(maximum)
	value.MinLength = &minLength
	value.MaxLength = &maxLength
}

// InvokableRun 从 Runtime Context 读取可信身份，严格解码模型参数，并只执行构造期注入的预编译 Graph。
func (t *Tool) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...einotool.Option) (string, error) {
	if t == nil || t.graph == nil {
		return "", fmt.Errorf("run analyze_materials tool: compiled graph is missing")
	}
	trustedPreview, ok := turncontext.MaterialAnalysisPreviewFrom(ctx)
	if !ok {
		return "", fmt.Errorf("run analyze_materials tool: trusted turn context is missing")
	}
	trusted := TrustedContext{
		Owner: trustedPreview.Owner, UserID: trustedPreview.UserID, ProjectID: trustedPreview.ProjectID,
		SessionID: trustedPreview.SessionID, InputID: trustedPreview.InputID, TurnID: trustedPreview.TurnID,
		RunID: trustedPreview.RunID, ToolCallID: trustedPreview.ToolCallID, FenceToken: trustedPreview.FenceToken,
		PromptVersion: trustedPreview.PromptVersion, ValidatorVersion: trustedPreview.ValidatorVersion,
		EvidencePolicyVersion: trustedPreview.EvidencePolicyVersion,
	}
	if err := ValidateTrustedContext(trusted); err != nil {
		// 可信上下文损坏属于 Runtime 装配错误，不能伪造成用户可修复的 Tool 失败。
		return "", fmt.Errorf("run analyze_materials tool: trusted turn context is invalid: %w", err)
	}
	intent, err := DecodeIntent([]byte(argumentsInJSON))
	if err != nil {
		return encodePreviewResult(failedPreviewResult(trusted, ResultCodeInvalidArgument), trusted)
	}
	// 重新编码严格 DTO，消除 JSON 空白和字段顺序差异；集合规范化与 digest 仍由 validate_intent Node 完成。
	intentJSON, err := json.Marshal(intent)
	if err != nil {
		return encodePreviewResult(failedPreviewResult(trusted, ResultCodeInternal), trusted)
	}
	outcome, err := t.graph.Invoke(ctx, GraphInput{TrustedContext: trusted, IntentJSON: intentJSON})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return encodePreviewResult(failedPreviewResult(trusted, ErrorResultCode(err)), trusted)
	}
	result := outcome.Result
	if result.Status == "failed" {
		return encodePreviewResult(failedPreviewResult(trusted, result.ResultCode), trusted)
	}
	if !validSuccessfulResult(result, trusted) {
		return encodePreviewResult(failedPreviewResult(trusted, ResultCodeInternal), trusted)
	}
	return encodePreviewResult(result, trusted)
}

// validSuccessfulResult 阻止 Graph 的畸形结果或错误状态越过 Tool 边界进入主 Agent。
func validSuccessfulResult(result Result, trusted TrustedContext) bool {
	return (result.Status == "completed" || result.Status == "partial") &&
		ValidateResultForContext(result, trusted) == nil
}

// failedPreviewResult 只按稳定错误码生成安全失败，不复制 Loader、模型或内部错误原文。
func failedPreviewResult(trusted TrustedContext, code string) Result {
	code, summary := safeFailure(code)
	retryable := false
	return Result{
		SchemaVersion: ResultSchemaVersion,
		Status:        "failed",
		ResultCode:    code,
		InvocationRef: InvocationRef{ToolCallID: trusted.ToolCallID},
		Summary:       summary,
		Retryable:     &retryable,
	}
}

// safeFailure 拒绝未知错误码，并从统一错误目录取得安全摘要。
func safeFailure(code string) (string, string) {
	switch code {
	case ResultCodeInvalidArgument, ResultCodeMaterialsNotAvailable, ResultCodeSnapshotInvalid,
		ResultCodeEvidenceConflict, ResultCodeDependencyNotReady, ResultCodePromptRenderFailed,
		ResultCodeModelFailed, ResultCodeModelOutputInvalid:
		return code, safeSummaryForCode(code)
	default:
		return ResultCodeInternal, safeSummaryForCode(ResultCodeInternal)
	}
}

// encodePreviewResult 把严格 Result 编码为 Eino Tool Message 使用的单个 JSON 对象。
func encodePreviewResult(result Result, trusted TrustedContext) (string, error) {
	if err := ValidateResultForContext(result, trusted); err != nil {
		return "", fmt.Errorf("encode analyze_materials result: invalid result: %w", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode analyze_materials result: %w", err)
	}
	return string(encoded), nil
}

var _ einotool.InvokableTool = (*Tool)(nil)
