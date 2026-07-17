package postgres

import "time"

// writePromptsPreviewRunModel 映射 Prompt Preview 的稳定执行身份、Fence 与 Run 状态。
type writePromptsPreviewRunModel struct {
	// InputID 是无 Message Session Input UUIDv7，同时作为本表主键。
	InputID string `gorm:"column:input_id;type:uuid;primaryKey"`
	// RequestID 是首次入队 HTTP 请求 UUIDv7，accepted 与 terminal Event 必须复用。
	RequestID string `gorm:"column:request_id;type:uuid"`
	// IdempotencyKey 是 Session 内同义重放使用的稳定键。
	IdempotencyKey string `gorm:"column:idempotency_key;type:uuid"`
	// RequestDigest 是 canonical typed Intent 与 StoryboardPreview 精确引用的请求摘要。
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
	// BusinessCommandID 是 Business Save/Query 与 Unknown Outcome 恢复复用的命令 UUIDv7。
	BusinessCommandID string `gorm:"column:business_command_id;type:uuid"`
	// RouterModelCallID 是外层 Router Model 的稳定调用 UUIDv7。
	RouterModelCallID string `gorm:"column:router_model_call_id;type:uuid"`
	// GraphModelCallID 是 Graph Prompt Model 的稳定调用 UUIDv7。
	GraphModelCallID string `gorm:"column:graph_model_call_id;type:uuid"`
	// AcceptedEventID 是首次入队 accepted Event UUIDv7。
	AcceptedEventID string `gorm:"column:accepted_event_id;type:uuid"`
	// TerminalEventID 是 completed、failed 或 runtime_failed 互斥终态共用的 Event UUIDv7。
	TerminalEventID string `gorm:"column:terminal_event_id;type:uuid"`
	// OwnerFence 是最近取得执行权的 Session Lane Fence。
	OwnerFence int64 `gorm:"column:owner_fence"`
	// Status 是 created、running、recovery_pending、completed 或 failed。
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

// TableName 返回 Prompt Preview 稳定 Run 的显式 Agent Schema 表名。
func (writePromptsPreviewRunModel) TableName() string {
	return "agent.write_prompts_preview_run"
}

// writePromptsPreviewContextModel 映射入队事务冻结且 append-only 的可信 Turn Context。
type writePromptsPreviewContextModel struct {
	// TurnID 是 Context 主键和稳定 Turn 逻辑引用。
	TurnID string `gorm:"column:turn_id;type:uuid;primaryKey"`
	// Profile 固定为批准的本地开发预览 Profile。
	Profile string `gorm:"column:profile"`
	// SchemaVersion 是最小不可变上下文版本。
	SchemaVersion string `gorm:"column:schema_version"`
	// RequestID 是首次入队并由 accepted/terminal Event 复用的请求 UUIDv7。
	RequestID string `gorm:"column:request_id;type:uuid"`
	// SessionID 是冻结的 Agent Session 逻辑引用。
	SessionID string `gorm:"column:session_id;type:uuid"`
	// InputID 是冻结的无 Message Session Input UUIDv7。
	InputID string `gorm:"column:input_id;type:uuid"`
	// RunID 是冻结的稳定 Run UUIDv7。
	RunID string `gorm:"column:run_id;type:uuid"`
	// ToolCallID 是冻结的稳定 Tool Call UUIDv7。
	ToolCallID string `gorm:"column:tool_call_id;type:uuid"`
	// BusinessCommandID 是冻结的 Business Save/Query 命令 UUIDv7。
	BusinessCommandID string `gorm:"column:business_command_id;type:uuid"`
	// RouterModelCallID 是冻结的 Router Model Call UUIDv7。
	RouterModelCallID string `gorm:"column:router_model_call_id;type:uuid"`
	// GraphModelCallID 是冻结的 Graph Prompt Model Call UUIDv7。
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
	// StoryboardPreviewID 是冻结的 Business StoryboardPreview Draft UUIDv7。
	StoryboardPreviewID string `gorm:"column:storyboard_preview_id;type:uuid"`
	// StoryboardPreviewVersion 是冻结的 StoryboardPreview Draft 精确版本。
	StoryboardPreviewVersion int64 `gorm:"column:storyboard_preview_version"`
	// StoryboardPreviewContentDigest 是冻结的 StoryboardPreview 内容 SHA-256 摘要。
	StoryboardPreviewContentDigest string `gorm:"column:storyboard_preview_content_digest"`
	// AccessScopeRef 是冻结的 Owner、Project 与 StoryboardPreview 访问范围引用。
	AccessScopeRef string `gorm:"column:access_scope_ref"`
	// AccessScopeDigest 是访问范围 canonical 摘要。
	AccessScopeDigest string `gorm:"column:access_scope_digest"`
	// ToolRegistryRef 是单 Tool Executable Registry 引用。
	ToolRegistryRef string `gorm:"column:tool_registry_ref"`
	// ToolRegistryDigest 是 Executable Registry canonical 摘要。
	ToolRegistryDigest string `gorm:"column:tool_registry_digest"`
	// ToolDefinitionRef 是 write_prompts.v2preview1 Definition 引用。
	ToolDefinitionRef string `gorm:"column:tool_definition_ref"`
	// ToolDefinitionDigest 是 Tool Definition canonical 摘要。
	ToolDefinitionDigest string `gorm:"column:tool_definition_digest"`
	// IntentSchemaRef 是严格 Tool Intent Schema 引用。
	IntentSchemaRef string `gorm:"column:intent_schema_ref"`
	// CandidateSchemaRef 是 Graph Model 候选 Schema 引用。
	CandidateSchemaRef string `gorm:"column:candidate_schema_ref"`
	// ResultSchemaRef 是严格 Tool Result Schema 引用。
	ResultSchemaRef string `gorm:"column:result_schema_ref"`
	// PromptRef 是 Graph Prompt Prompt 精确版本引用。
	PromptRef string `gorm:"column:prompt_ref"`
	// PromptDigest 是 Graph Prompt Prompt canonical 摘要。
	PromptDigest string `gorm:"column:prompt_digest"`
	// ValidatorRef 是候选 Validator 精确版本引用。
	ValidatorRef string `gorm:"column:validator_ref"`
	// ValidatorDigest 是候选 Validator canonical 摘要。
	ValidatorDigest string `gorm:"column:validator_digest"`
	// ExactSetValidatorRef 是局部引用与依赖 DAG Validator 引用。
	ExactSetValidatorRef string `gorm:"column:exact_set_validator_ref"`
	// ExactSetValidatorDigest 是 DAG Validator canonical 摘要。
	ExactSetValidatorDigest string `gorm:"column:exact_set_validator_digest"`
	// RouterModelRouteRef 是本地 Fake Router Route 引用。
	RouterModelRouteRef string `gorm:"column:router_model_route_ref"`
	// RouterModelRouteDigest 是 Router Route canonical 摘要。
	RouterModelRouteDigest string `gorm:"column:router_model_route_digest"`
	// PromptModelRouteRef 是本地 Graph Fake Prompt Model Route 引用。
	PromptModelRouteRef string `gorm:"column:prompt_model_route_ref"`
	// PromptModelRouteDigest 是 Graph Prompt Model Route canonical 摘要。
	PromptModelRouteDigest string `gorm:"column:prompt_model_route_digest"`
	// RuntimePolicyRef 是 receipt-first、ReturnDirectly 与 Unknown Recovery Policy 引用。
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

// TableName 返回 Prompt Preview 不可变 Turn Context 的显式 Agent Schema 表名。
func (writePromptsPreviewContextModel) TableName() string {
	return "agent.write_prompts_preview_turn_context"
}

// writePromptsPreviewModelReceiptModel 映射 Router 或 Graph Prompt Fake Model 的冻结调用回执。
type writePromptsPreviewModelReceiptModel struct {
	// ModelCallID 是 Router 或 Graph Model 稳定调用主键。
	ModelCallID string `gorm:"column:model_call_id;type:uuid;primaryKey"`
	// CallKind 区分 router 与 graph_prompt 命名空间。
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

// TableName 返回 Prompt Preview 分层 Model Receipt 的显式 Agent Schema 表名。
func (writePromptsPreviewModelReceiptModel) TableName() string {
	return "agent.write_prompts_preview_model_receipt"
}

// writePromptsPreviewToolReceiptModel 映射 receipt-first Tool Result 与 Business Unknown Recovery 工件。
type writePromptsPreviewToolReceiptModel struct {
	// ToolCallID 是稳定 Tool Call 主键。
	ToolCallID string `gorm:"column:tool_call_id;type:uuid;primaryKey"`
	// RunID 是稳定 Run 逻辑引用。
	RunID string `gorm:"column:run_id;type:uuid"`
	// TurnID 是稳定 Turn 逻辑引用。
	TurnID string `gorm:"column:turn_id;type:uuid"`
	// InputID 是严格 HOL Input 逻辑引用。
	InputID string `gorm:"column:input_id;type:uuid"`
	// BusinessCommandID 是 prepared Command、Query 与同键重发复用的 UUIDv7。
	BusinessCommandID string `gorm:"column:business_command_id;type:uuid"`
	// RequestDigest 是 Context、Definition、Schema 与 Intent 的外层 Tool 请求摘要。
	RequestDigest string `gorm:"column:request_digest"`
	// ExecutionFence 是当前 open/prepared/unknown Tool 执行权 Fence。
	ExecutionFence int64 `gorm:"column:execution_fence"`
	// Status 是 open、business_prepared、business_unknown、completed 或 failed。
	Status string `gorm:"column:status"`
	// CommandCiphertext 是 Save RPC 前冻结的完整 Business Draft Command AEAD Envelope。
	CommandCiphertext []byte `gorm:"column:command_ciphertext"`
	// CommandKeyVersion 是 prepared Command 内容密钥版本。
	CommandKeyVersion *string `gorm:"column:command_key_version"`
	// CommandDigest 是 prepared Command canonical 明文 SHA-256 摘要。
	CommandDigest *string `gorm:"column:command_digest"`
	// ExpectedProjectVersion 是 prepared Save Command 保存时复验的 Business Project 乐观锁版本。
	ExpectedProjectVersion *int64 `gorm:"column:expected_project_version"`
	// BusinessRequestDigest 是 Agent 与 Business 共同冻结的 Save 请求摘要。
	BusinessRequestDigest *string `gorm:"column:business_request_digest"`
	// ContentDigest 是 prepared Prompt Draft 完整内容摘要。
	ContentDigest *string `gorm:"column:content_digest"`
	// ResendAttempts 是 Unknown Outcome 后已持久化预留的同键重发次数。
	ResendAttempts int `gorm:"column:resend_attempts"`
	// ResendLimit 是首次 prepared 时冻结且不得提高的同键重发上限。
	ResendLimit int `gorm:"column:resend_limit"`
	// ResultCiphertext 是 completed/failed 完整严格 Tool Result 的 AEAD Envelope。
	ResultCiphertext []byte `gorm:"column:result_ciphertext"`
	// ResultKeyVersion 是 terminal Tool Result 内容密钥版本。
	ResultKeyVersion *string `gorm:"column:result_key_version"`
	// ResultDigest 是 canonical Tool Result 摘要。
	ResultDigest *string `gorm:"column:result_digest"`
	// ResultCode 是经 Validator 冻结的 Tool 稳定结果码。
	ResultCode *string `gorm:"column:result_code"`
	// CreatedAt 是 open Receipt 入队首写数据库 UTC 时间。
	CreatedAt time.Time `gorm:"column:created_at"`
	// PreparedAt 是 Business Save RPC 前完整 Command 首次冻结的 UTC 时间。
	PreparedAt *time.Time `gorm:"column:prepared_at"`
	// UnknownAt 是 Business Save Unknown Outcome 首次冻结的 UTC 时间。
	UnknownAt *time.Time `gorm:"column:unknown_at"`
	// CompletedAt 是 terminal Result 首写数据库 UTC 时间。
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

// TableName 返回 Prompt Preview receipt-first Tool Receipt 的显式 Agent Schema 表名。
func (writePromptsPreviewToolReceiptModel) TableName() string {
	return "agent.write_prompts_preview_tool_receipt"
}
