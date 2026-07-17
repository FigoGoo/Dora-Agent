package turncontext

import "context"

const (
	// MaterialAnalysisTurnContextSchemaVersion 是素材分析 Runtime 的最小不可变上下文版本。
	MaterialAnalysisTurnContextSchemaVersion = "analyze_materials.turn_context.v2preview1"
	// MaterialAnalysisRuntimeProfile 是唯一获准本地启用的素材分析 Runtime Profile。
	MaterialAnalysisRuntimeProfile = "analyze_materials.runtime.v2preview1"
)

// MaterialAnalysisTurnContext 是入队事务冻结的素材分析执行契约。
// 本类型只携带稳定标识、引用和摘要；Intent 明文、Evidence 正文和模型响应不属于上下文。
type MaterialAnalysisTurnContext struct {
	SchemaVersion string
	Profile       string
	SessionID     string
	InputID       string
	TurnID        string
	RunID         string
	ToolCallID    string
	UserID        string
	ProjectID     string

	IntentKeyVersion string
	IntentDigest     string

	AccessScopeRef           string
	AccessScopeDigest        string
	ToolRegistryRef          string
	ToolRegistryDigest       string
	ToolDefinitionRef        string
	ToolDefinitionDigest     string
	IntentSchemaRef          string
	ResultSchemaRef          string
	PromptRef                string
	PromptDigest             string
	ValidatorRef             string
	ValidatorDigest          string
	EvidencePolicyRef        string
	EvidencePolicyDigest     string
	RouterModelRouteRef      string
	RouterModelRouteDigest   string
	AnalysisModelRouteRef    string
	AnalysisModelRouteDigest string
	RuntimePolicyRef         string
	RuntimePolicyDigest      string
	BudgetRef                string
	BudgetDigest             string
	ContextDigest            string
}

// MaterialAnalysisRuntime 保存当前 Claim 的 owner/fence、稳定模型调用标识和不可变上下文。
// IntentJSON 必须是 Runtime 在认证解密、摘要复核后得到的 canonical JSON 副本。
type MaterialAnalysisRuntime struct {
	Owner             string
	FenceToken        int64
	RouterModelCallID string
	GraphModelCallID  string
	IntentJSON        string
	Context           MaterialAnalysisTurnContext
}

type materialAnalysisRuntimeKey struct{}

// WithMaterialAnalysisRuntime 返回携带深拷贝可信值的新 Context。
func WithMaterialAnalysisRuntime(ctx context.Context, value MaterialAnalysisRuntime) context.Context {
	return context.WithValue(ctx, materialAnalysisRuntimeKey{}, value)
}

// MaterialAnalysisRuntimeFrom 读取素材分析 Runtime 注入的可信上下文。
func MaterialAnalysisRuntimeFrom(ctx context.Context) (MaterialAnalysisRuntime, bool) {
	value, ok := ctx.Value(materialAnalysisRuntimeKey{}).(MaterialAnalysisRuntime)
	return value, ok
}
