package postgres

import "time"

// analyzeMaterialsPreviewRunModel 映射 Preview 的稳定执行身份、Fence 与 Run 状态。
type analyzeMaterialsPreviewRunModel struct {
	// InputID 是无 Message Session Input UUIDv7，同时作为本表主键。
	InputID string `gorm:"column:input_id;type:uuid;primaryKey"`
	// RequestID 是首次入队 HTTP 请求 UUIDv7，后续事件必须复用。
	RequestID string `gorm:"column:request_id;type:uuid"`
	// IdempotencyKey 是 Session 内同义重放使用的稳定键。
	IdempotencyKey string `gorm:"column:idempotency_key;type:uuid"`
	// RequestDigest 是 canonical typed Intent 摘要。
	RequestDigest string `gorm:"column:request_digest"`
	// SessionID 是 Agent Session 逻辑引用。
	SessionID string `gorm:"column:session_id;type:uuid"`
	// UserID 是入队时冻结的可信 Business User 逻辑引用。
	UserID string `gorm:"column:user_id;type:uuid"`
	// ProjectID 是入队时冻结的可信 Business Project 逻辑引用。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// TurnID 是技术重试复用的稳定 Turn UUIDv7。
	TurnID string `gorm:"column:turn_id;type:uuid"`
	// RunID 是 Lease takeover 复用的稳定 Run UUIDv7。
	RunID string `gorm:"column:run_id;type:uuid"`
	// ToolCallID 是 Router 必须原样使用的 Tool Call UUIDv7。
	ToolCallID string `gorm:"column:tool_call_id;type:uuid"`
	// RouterModelCallID 是外层 Router Model 的稳定调用 UUIDv7。
	RouterModelCallID string `gorm:"column:router_model_call_id;type:uuid"`
	// GraphModelCallID 是 Graph Analysis Model 的稳定调用 UUIDv7。
	GraphModelCallID string `gorm:"column:graph_model_call_id;type:uuid"`
	// AcceptedEventID 是首次入队 accepted Event UUIDv7。
	AcceptedEventID string `gorm:"column:accepted_event_id;type:uuid"`
	// TerminalEventID 是四类互斥终态共用的稳定 Event UUIDv7。
	TerminalEventID string `gorm:"column:terminal_event_id;type:uuid"`
	// OwnerFence 是最近取得执行权的 Session Lane Fence。
	OwnerFence int64 `gorm:"column:owner_fence"`
	// Status 是 created、running、completed 或 failed。
	Status string `gorm:"column:status"`
	// Version 是 Run 状态变化的乐观锁版本。
	Version int64 `gorm:"column:version"`
	// CreatedAt 是数据库生成的首次入队 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// UpdatedAt 是数据库状态最近变化 UTC 时间。
	UpdatedAt time.Time `gorm:"column:updated_at"`
	// StartedAt 是首次合法运行 UTC 时间。
	StartedAt *time.Time `gorm:"column:started_at"`
	// CompletedAt 是 completed 或 failed 终态 UTC 时间。
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

// TableName 返回稳定 Run 的显式 Agent Schema 表名。
func (analyzeMaterialsPreviewRunModel) TableName() string {
	return "agent.analyze_materials_preview_run"
}

// analyzeMaterialsPreviewContextModel 映射入队事务冻结且 append-only 的最小可信 Turn Context。
type analyzeMaterialsPreviewContextModel struct {
	// TurnID 是 Context 主键和稳定 Turn 逻辑引用。
	TurnID string `gorm:"column:turn_id;type:uuid;primaryKey"`
	// Profile 固定为批准的本地开发预览 Profile。
	Profile string `gorm:"column:profile"`
	// SchemaVersion 是最小不可变上下文版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// SessionID 是冻结的 Agent Session 逻辑引用。
	SessionID string `gorm:"column:session_id;type:uuid"`
	// InputID 是冻结的无 Message Session Input UUIDv7。
	InputID string `gorm:"column:input_id;type:uuid"`
	// RunID 是冻结的稳定 Run UUIDv7。
	RunID string `gorm:"column:run_id;type:uuid"`
	// ToolCallID 是冻结的稳定 Tool Call UUIDv7。
	ToolCallID string `gorm:"column:tool_call_id;type:uuid"`
	// RouterModelCallID 是冻结的 Router Model Call UUIDv7。
	RouterModelCallID string `gorm:"column:router_model_call_id;type:uuid"`
	// GraphModelCallID 是冻结的 Graph Model Call UUIDv7。
	GraphModelCallID string `gorm:"column:graph_model_call_id;type:uuid"`
	// UserID 是冻结的可信 Business User 逻辑引用。
	UserID string `gorm:"column:user_id;type:uuid"`
	// ProjectID 是冻结的可信 Business Project 逻辑引用。
	ProjectID string `gorm:"column:project_id;type:uuid"`
	// IntentCiphertext 是完整 canonical typed Intent 的 AEAD Envelope。
	IntentCiphertext []byte `gorm:"column:intent_ciphertext"`
	// IntentKeyVersion 是 Intent 内容密钥版本引用。
	IntentKeyVersion string `gorm:"column:intent_key_version"`
	// IntentDigest 是 canonical Intent SHA-256 摘要。
	IntentDigest string `gorm:"column:intent_digest"`
	// AccessScopeRef 是冻结的 Evidence 访问范围引用。
	AccessScopeRef string `gorm:"column:access_scope_ref"`
	// AccessScopeDigest 是访问范围 canonical 摘要。
	AccessScopeDigest string `gorm:"column:access_scope_digest"`
	// ToolRegistryRef 是单 Tool Executable Registry 引用。
	ToolRegistryRef string `gorm:"column:tool_registry_ref"`
	// ToolRegistryDigest 是 Executable Registry canonical 摘要。
	ToolRegistryDigest string `gorm:"column:tool_registry_digest"`
	// ToolDefinitionRef 是 analyze_materials.v2preview1 Definition 引用。
	ToolDefinitionRef string `gorm:"column:tool_definition_ref"`
	// ToolDefinitionDigest 是 Tool Definition canonical 摘要。
	ToolDefinitionDigest string `gorm:"column:tool_definition_digest"`
	// IntentSchemaRef 是严格 Tool Intent Schema 引用。
	IntentSchemaRef string `gorm:"column:intent_schema_ref"`
	// ResultSchemaRef 是严格 Tool Result Schema 引用。
	ResultSchemaRef string `gorm:"column:result_schema_ref"`
	// PromptRef 是 Graph Analysis Prompt 精确版本引用。
	PromptRef string `gorm:"column:prompt_ref"`
	// PromptDigest 是 Graph Analysis Prompt canonical 摘要。
	PromptDigest string `gorm:"column:prompt_digest"`
	// ValidatorRef 是 Result Validator 精确版本引用。
	ValidatorRef string `gorm:"column:validator_ref"`
	// ValidatorDigest 是 Result Validator canonical 摘要。
	ValidatorDigest string `gorm:"column:validator_digest"`
	// EvidencePolicyRef 是 text/image Evidence Policy 引用。
	EvidencePolicyRef string `gorm:"column:evidence_policy_ref"`
	// EvidencePolicyDigest 是 Evidence Policy canonical 摘要。
	EvidencePolicyDigest string `gorm:"column:evidence_policy_digest"`
	// RouterModelRouteRef 是本地 Fake Router Route 引用。
	RouterModelRouteRef string `gorm:"column:router_model_route_ref"`
	// RouterModelRouteDigest 是 Router Route canonical 摘要。
	RouterModelRouteDigest string `gorm:"column:router_model_route_digest"`
	// AnalysisModelRouteRef 是本地 Graph Fake Model Route 引用。
	AnalysisModelRouteRef string `gorm:"column:analysis_model_route_ref"`
	// AnalysisModelRouteDigest 是 Graph Model Route canonical 摘要。
	AnalysisModelRouteDigest string `gorm:"column:analysis_model_route_digest"`
	// RuntimePolicyRef 是 receipt-first/ReturnDirectly Policy 引用。
	RuntimePolicyRef string `gorm:"column:runtime_policy_ref"`
	// RuntimePolicyDigest 是 Runtime Policy canonical 摘要。
	RuntimePolicyDigest string `gorm:"column:runtime_policy_digest"`
	// BudgetRef 是本批硬预算版本引用。
	BudgetRef string `gorm:"column:budget_ref"`
	// BudgetDigest 是本批硬预算 canonical 摘要。
	BudgetDigest string `gorm:"column:budget_digest"`
	// ContextDigest 是全部具名冻结字段的整体摘要。
	ContextDigest string `gorm:"column:context_digest"`
	// CreatedAt 是 Context 首写数据库 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回不可变 Turn Context 的显式 Agent Schema 表名。
func (analyzeMaterialsPreviewContextModel) TableName() string {
	return "agent.analyze_materials_preview_turn_context"
}

// analyzeMaterialsPreviewModelReceiptModel 映射 Router 或 Graph Fake Model 的冻结调用回执。
type analyzeMaterialsPreviewModelReceiptModel struct {
	// ModelCallID 是 Router 或 Graph Model 稳定调用主键。
	ModelCallID string `gorm:"column:model_call_id;type:uuid;primaryKey"`
	// CallKind 区分 router 与 graph_analysis 命名空间。
	CallKind string `gorm:"column:call_kind"`
	// RunID 是稳定 Run 逻辑引用。
	RunID string `gorm:"column:run_id;type:uuid"`
	// TurnID 是稳定 Turn 逻辑引用。
	TurnID string `gorm:"column:turn_id;type:uuid"`
	// InputID 是严格 HOL Input 逻辑引用。
	InputID string `gorm:"column:input_id;type:uuid"`
	// RequestDigest 是模型请求 canonical 摘要。
	RequestDigest string `gorm:"column:request_digest"`
	// ExecutionFence 是最近 reserve 本地 Fake 执行权的 Fence。
	ExecutionFence int64 `gorm:"column:execution_fence"`
	// Status 是 reserved、completed 或 failed。
	Status string `gorm:"column:status"`
	// ResponseCiphertext 是 completed Message 的 AEAD Envelope。
	ResponseCiphertext []byte `gorm:"column:response_ciphertext"`
	// ResponseKeyVersion 是 completed Message 内容密钥版本。
	ResponseKeyVersion *string `gorm:"column:response_key_version"`
	// ResponseDigest 是 completed Message canonical 摘要。
	ResponseDigest *string `gorm:"column:response_digest"`
	// ErrorCode 是 failed 时冻结的稳定脱敏错误码。
	ErrorCode *string `gorm:"column:error_code"`
	// CreatedAt 是首次 reserve 的数据库 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// CompletedAt 是 terminal Receipt 首写数据库 UTC 时间。
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

// TableName 返回分层 Model Receipt 的显式 Agent Schema 表名。
func (analyzeMaterialsPreviewModelReceiptModel) TableName() string {
	return "agent.analyze_materials_preview_model_receipt"
}

// analyzeMaterialsPreviewToolReceiptModel 映射 receipt-first 的完整严格 Tool Result 回执。
type analyzeMaterialsPreviewToolReceiptModel struct {
	// ToolCallID 是稳定 Tool Call 主键。
	ToolCallID string `gorm:"column:tool_call_id;type:uuid;primaryKey"`
	// RunID 是稳定 Run 逻辑引用。
	RunID string `gorm:"column:run_id;type:uuid"`
	// TurnID 是稳定 Turn 逻辑引用。
	TurnID string `gorm:"column:turn_id;type:uuid"`
	// InputID 是严格 HOL Input 逻辑引用。
	InputID string `gorm:"column:input_id;type:uuid"`
	// RequestDigest 是 Context/Definition/Schema/Intent 的摘要。
	RequestDigest string `gorm:"column:request_digest"`
	// ExecutionFence 是取得 open Tool 执行权的 Fence。
	ExecutionFence int64 `gorm:"column:execution_fence"`
	// Status 是 open、completed、partial 或 failed。
	Status string `gorm:"column:status"`
	// ResultCiphertext 是完整严格 Tool Result 的 AEAD Envelope。
	ResultCiphertext []byte `gorm:"column:result_ciphertext"`
	// ResultKeyVersion 是 Tool Result 内容密钥版本。
	ResultKeyVersion *string `gorm:"column:result_key_version"`
	// ResultDigest 是 canonical Tool Result 摘要。
	ResultDigest *string `gorm:"column:result_digest"`
	// ResultCode 是经 Validator 冻结的 Tool 稳定结果码。
	ResultCode *string `gorm:"column:result_code"`
	// CreatedAt 是 open Receipt 入队首写数据库 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// CompletedAt 是 terminal Result 首写数据库 UTC 时间。
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

// TableName 返回 receipt-first Tool Receipt 的显式 Agent Schema 表名。
func (analyzeMaterialsPreviewToolReceiptModel) TableName() string {
	return "agent.analyze_materials_preview_tool_receipt"
}

// analyzeMaterialsPreviewProjectionModel 映射每个 Input 首写后不可变的安全 Card 投影。
type analyzeMaterialsPreviewProjectionModel struct {
	// SourceInputID 是产生 Card 的 Input UUIDv7，同时作为投影主键。
	SourceInputID string `gorm:"column:source_input_id;type:uuid;primaryKey"`
	// SessionID 是按最新入队序号读取 Snapshot 的 Session 引用。
	SessionID string `gorm:"column:session_id;type:uuid"`
	// SourceEnqueueSeq 是 Input 的 Session HOL 序号。
	SourceEnqueueSeq int64 `gorm:"column:source_enqueue_seq"`
	// TurnID 是稳定 Turn 逻辑引用。
	TurnID string `gorm:"column:turn_id;type:uuid"`
	// RunID 是稳定 Run 逻辑引用。
	RunID string `gorm:"column:run_id;type:uuid"`
	// ToolCallID 是稳定 Tool Call 逻辑引用。
	ToolCallID string `gorm:"column:tool_call_id;type:uuid"`
	// SchemaVersion 是浏览器安全 Card 版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// OutcomeKind 严格区分 Tool 三态与 Runtime Failure。
	OutcomeKind string `gorm:"column:outcome_kind"`
	// Status 是 completed、partial 或 failed。
	Status string `gorm:"column:status"`
	// ResultDigest 是 Tool Result 或 Runtime Failure Card 摘要。
	ResultDigest string `gorm:"column:result_digest"`
	// Payload 是不含 Intent/Evidence 正文/模型原文的安全 Card JSON。
	Payload string `gorm:"column:payload;type:jsonb"`
	// ProjectionVersion 是 append-only 投影聚合版本。
	ProjectionVersion int64 `gorm:"column:projection_version"`
	// CreatedAt 是投影与终态 Event 同事务提交 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
}

// TableName 返回 append-only Card Projection 的显式 Agent Schema 表名。
func (analyzeMaterialsPreviewProjectionModel) TableName() string {
	return "agent.analyze_materials_preview_projection"
}
