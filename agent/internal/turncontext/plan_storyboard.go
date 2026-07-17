package turncontext

import "context"

const (
	// PlanStoryboardTurnContextSchemaVersion 是 Storyboard Preview Runtime 的最小不可变上下文版本。
	PlanStoryboardTurnContextSchemaVersion = "plan_storyboard.turn_context.v2preview1"
	// PlanStoryboardRuntimeProfile 是唯一获准在本地开发预览启用的 Storyboard Runtime Profile。
	PlanStoryboardRuntimeProfile = "plan_storyboard.runtime.v2preview1"
)

// PlanStoryboardTurnContext 是入队事务 append-once 冻结的 Storyboard 执行契约。
// 本类型只保存稳定身份、CreationSpec 精确引用和批准的配置摘要；Intent 明文、模型响应与 Business 命令正文不属于上下文。
type PlanStoryboardTurnContext struct {
	// SchemaVersion 固定为 plan_storyboard.turn_context.v2preview1。
	SchemaVersion string `json:"schema_version"`
	// Profile 固定为 plan_storyboard.runtime.v2preview1。
	Profile string `json:"profile"`
	// RequestID 是首次入队冻结并由 accepted/terminal Event 复用的请求 UUIDv7。
	RequestID string `json:"request_id"`
	// SessionID 是当前 Agent Session UUIDv7。
	SessionID string `json:"session_id"`
	// InputID 是不创建 Message 的稳定 Session Input UUIDv7。
	InputID string `json:"input_id"`
	// TurnID 是技术重试和恢复复用的稳定 Turn UUIDv7。
	TurnID string `json:"turn_id"`
	// RunID 是 Lease takeover 复用的稳定 Run UUIDv7。
	RunID string `json:"run_id"`
	// ToolCallID 是 Router 必须原样使用的 plan_storyboard Tool Call UUIDv7。
	ToolCallID string `json:"tool_call_id"`
	// BusinessCommandID 是 Save/Query Unknown Outcome 必须复用的 Business 命令 UUIDv7。
	BusinessCommandID string `json:"business_command_id"`
	// RouterModelCallID 是外层 Router Model 的稳定调用 UUIDv7。
	RouterModelCallID string `json:"router_model_call_id"`
	// GraphModelCallID 是 Graph Planning Model 的稳定调用 UUIDv7。
	GraphModelCallID string `json:"graph_model_call_id"`
	// UserID 是入队时冻结的可信 Business User UUIDv7。
	UserID string `json:"user_id"`
	// ProjectID 是入队时冻结的可信 Business Project UUIDv7。
	ProjectID string `json:"project_id"`

	// IntentKeyVersion 是 canonical Intent 密文的内容密钥版本引用。
	IntentKeyVersion string `json:"intent_key_version"`
	// IntentDigest 是 canonical Intent 明文的 SHA-256 摘要。
	IntentDigest string `json:"intent_digest"`

	// CreationSpecID 是本 Turn 唯一允许消费的 Business CreationSpec Draft UUIDv7。
	CreationSpecID string `json:"creation_spec_id"`
	// CreationSpecVersion 是本 Turn 冻结的 CreationSpec Draft 精确版本。
	CreationSpecVersion int64 `json:"creation_spec_version"`
	// CreationSpecContentDigest 是本 Turn 冻结的 CreationSpec 内容 SHA-256 摘要。
	CreationSpecContentDigest string `json:"creation_spec_content_digest"`

	// AccessScopeRef 是 Owner、Project 与 CreationSpec 读取权限的冻结策略引用。
	AccessScopeRef string `json:"access_scope_ref"`
	// AccessScopeDigest 是访问范围 canonical 摘要。
	AccessScopeDigest string `json:"access_scope_digest"`
	// ToolRegistryRef 是恰好包含 plan_storyboard 的可执行 Registry 引用。
	ToolRegistryRef string `json:"tool_registry_ref"`
	// ToolRegistryDigest 是可执行 Registry canonical 摘要。
	ToolRegistryDigest string `json:"tool_registry_digest"`
	// ToolDefinitionRef 是 plan_storyboard.v2preview1 Definition 引用。
	ToolDefinitionRef string `json:"tool_definition_ref"`
	// ToolDefinitionDigest 是 Tool Definition canonical 摘要。
	ToolDefinitionDigest string `json:"tool_definition_digest"`
	// IntentSchemaRef 是严格 Tool Intent Schema 引用。
	IntentSchemaRef string `json:"intent_schema_ref"`
	// CandidateSchemaRef 是 Graph Planning Model 候选 Schema 引用。
	CandidateSchemaRef string `json:"candidate_schema_ref"`
	// ResultSchemaRef 是严格 Tool Result Schema 引用。
	ResultSchemaRef string `json:"result_schema_ref"`
	// PromptRef 是 Graph Planning Prompt 精确版本引用。
	PromptRef string `json:"prompt_ref"`
	// PromptDigest 是 Graph Planning Prompt canonical 摘要。
	PromptDigest string `json:"prompt_digest"`
	// ValidatorRef 是 Storyboard 候选 Validator 精确版本引用。
	ValidatorRef string `json:"validator_ref"`
	// ValidatorDigest 是候选 Validator canonical 摘要。
	ValidatorDigest string `json:"validator_digest"`
	// DAGValidatorRef 是 Storyboard 局部引用与依赖 DAG Validator 引用。
	DAGValidatorRef string `json:"dag_validator_ref"`
	// DAGValidatorDigest 是依赖 DAG Validator canonical 摘要。
	DAGValidatorDigest string `json:"dag_validator_digest"`
	// RouterModelRouteRef 是本地 Fake Router Route 引用。
	RouterModelRouteRef string `json:"router_model_route_ref"`
	// RouterModelRouteDigest 是 Router Route canonical 摘要。
	RouterModelRouteDigest string `json:"router_model_route_digest"`
	// PlanningModelRouteRef 是本地 Graph Planning Model Route 引用。
	PlanningModelRouteRef string `json:"planning_model_route_ref"`
	// PlanningModelRouteDigest 是 Graph Planning Model Route canonical 摘要。
	PlanningModelRouteDigest string `json:"planning_model_route_digest"`
	// RuntimePolicyRef 是 receipt-first、ReturnDirectly 与 Unknown Recovery 策略引用。
	RuntimePolicyRef string `json:"runtime_policy_ref"`
	// RuntimePolicyDigest 是 Runtime Policy canonical 摘要。
	RuntimePolicyDigest string `json:"runtime_policy_digest"`
	// BudgetRef 是本批 Router、Tool、Graph Model 与 Business 重发硬预算引用。
	BudgetRef string `json:"budget_ref"`
	// BudgetDigest 是硬预算 canonical 摘要。
	BudgetDigest string `json:"budget_digest"`
	// ContextDigest 是上述具名字段 canonical 编码的整体 SHA-256 摘要。
	ContextDigest string `json:"context_digest"`
}

// PlanStoryboardRuntime 保存当前 Claim 的 owner/fence、受保护 Intent 解密副本和不可变上下文。
// IntentJSON 必须由 Runtime 认证解密并复核摘要；调用链不得用 Router 参数覆盖该副本。
type PlanStoryboardRuntime struct {
	// Owner 是当前 Session Lane Lease owner。
	Owner string
	// FenceToken 是当前 Claim 的正整数隔离令牌。
	FenceToken int64
	// IntentJSON 是认证解密后的 canonical Intent JSON 副本。
	IntentJSON string
	// Context 是 PostgreSQL append-once 冻结的可信上下文值。
	Context PlanStoryboardTurnContext
}

type planStoryboardRuntimeKey struct{}

// WithPlanStoryboardRuntime 返回携带不可变值副本的新 Context。
// 调用方必须先完成密文认证、Intent 摘要与全部 pins 复核，缺少任一复核时不得调用。
func WithPlanStoryboardRuntime(ctx context.Context, value PlanStoryboardRuntime) context.Context {
	return context.WithValue(ctx, planStoryboardRuntimeKey{}, value)
}

// PlanStoryboardRuntimeFrom 读取 Storyboard Runtime 注入的可信上下文。
// 返回 false 表示调用未经过持久化 Session Lane，Router、Model 与 Tool Wrapper 必须失败关闭。
func PlanStoryboardRuntimeFrom(ctx context.Context) (PlanStoryboardRuntime, bool) {
	value, ok := ctx.Value(planStoryboardRuntimeKey{}).(PlanStoryboardRuntime)
	return value, ok
}

// PlanStoryboardPreview 保存 plan_storyboard V2 Tool Core 所需的最小可信调用身份。
// 本类型不包含模型可控 Intent、Prompt 正文、模型响应或 Business 命令正文。
type PlanStoryboardPreview struct {
	// Owner 是当前 Session Lane Lease owner。
	Owner string
	// RequestID 是首次入队冻结并由 accepted/terminal Event 复用的请求 UUIDv7。
	RequestID string
	// UserID 是可信 Business Principal UUIDv7。
	UserID string
	// ProjectID 是可信 Business Project UUIDv7。
	ProjectID string
	// SessionID 是当前 Agent Session UUIDv7。
	SessionID string
	// InputID 是稳定 Session Input UUIDv7。
	InputID string
	// TurnID 是稳定 Runner Turn UUIDv7。
	TurnID string
	// RunID 是稳定 Runner Run UUIDv7。
	RunID string
	// ToolCallID 是稳定 plan_storyboard Tool Call UUIDv7。
	ToolCallID string
	// BusinessCommandID 是 Save/Query 必须复用的 Business Command UUIDv7。
	BusinessCommandID string
	// FenceToken 是当前 Session Lane Claim Fence。
	FenceToken int64
	// CreationSpecID 是 Runtime 冻结的 CreationSpec Draft UUIDv7。
	CreationSpecID string
	// CreationSpecVersion 是 Runtime 冻结的 CreationSpec Draft 精确版本。
	CreationSpecVersion int64
	// CreationSpecContentDigest 是 Runtime 冻结的 CreationSpec 内容 SHA-256 摘要。
	CreationSpecContentDigest string
	// PromptVersion 是调用前冻结的 Prompt pin。
	PromptVersion string
	// ValidatorVersion 是调用前冻结的候选 Validator pin。
	ValidatorVersion string
	// DAGValidatorVersion 是调用前冻结的依赖 DAG Validator pin。
	DAGValidatorVersion string
}

type planStoryboardPreviewKey struct{}

// WithPlanStoryboardPreview 返回携带不可变值副本的新 Context。
// 该值只允许由已验证的 PlanStoryboardRuntime Claim 显式映射得到。
func WithPlanStoryboardPreview(ctx context.Context, value PlanStoryboardPreview) context.Context {
	return context.WithValue(ctx, planStoryboardPreviewKey{}, value)
}

// PlanStoryboardPreviewFrom 读取 Runtime 注入的最小 Storyboard Tool Core 上下文。
// 返回 false 时 Graph Tool 不得从其他 Context key、用户正文或模型参数推断可信字段。
func PlanStoryboardPreviewFrom(ctx context.Context) (PlanStoryboardPreview, bool) {
	value, ok := ctx.Value(planStoryboardPreviewKey{}).(PlanStoryboardPreview)
	return value, ok
}
