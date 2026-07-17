package postgres

import "time"

type userMessageTurnModel struct {
	TurnID          string     `gorm:"column:turn_id;type:uuid;primaryKey"`
	InputID         string     `gorm:"column:input_id;type:uuid"`
	SessionID       string     `gorm:"column:session_id;type:uuid"`
	MessageID       string     `gorm:"column:message_id;type:uuid"`
	UserID          string     `gorm:"column:user_id;type:uuid"`
	ProjectID       string     `gorm:"column:project_id;type:uuid"`
	OutputID        string     `gorm:"column:output_id;type:uuid"`
	ModelCallID     string     `gorm:"column:model_call_id;type:uuid"`
	RecoveryEventID string     `gorm:"column:recovery_event_id;type:uuid"`
	TerminalEventID string     `gorm:"column:terminal_event_id;type:uuid"`
	Status          string     `gorm:"column:status"`
	Version         int64      `gorm:"column:version"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
	CompletedAt     *time.Time `gorm:"column:completed_at"`
}

func (userMessageTurnModel) TableName() string { return "agent.session_user_message_turn" }

type userMessageContextModel struct {
	TurnID               string    `gorm:"column:turn_id;type:uuid;primaryKey"`
	SchemaVersion        string    `gorm:"column:schema_version"`
	SessionID            string    `gorm:"column:session_id;type:uuid"`
	InputID              string    `gorm:"column:input_id;type:uuid"`
	MessageID            string    `gorm:"column:message_id;type:uuid"`
	UserID               string    `gorm:"column:user_id;type:uuid"`
	ProjectID            string    `gorm:"column:project_id;type:uuid"`
	MessageCutoffSeq     int64     `gorm:"column:message_cutoff_seq"`
	MessageContentDigest string    `gorm:"column:message_content_digest"`
	SkillSnapshotRef     string    `gorm:"column:skill_snapshot_ref"`
	SkillSnapshotDigest  string    `gorm:"column:skill_snapshot_digest"`
	PromptRef            string    `gorm:"column:prompt_ref"`
	PromptDigest         string    `gorm:"column:prompt_digest"`
	ToolRegistryRef      string    `gorm:"column:tool_registry_ref"`
	ToolRegistryDigest   string    `gorm:"column:tool_registry_digest"`
	RuntimePolicyRef     string    `gorm:"column:runtime_policy_ref"`
	RuntimePolicyDigest  string    `gorm:"column:runtime_policy_digest"`
	ModelRouteRef        string    `gorm:"column:model_route_ref"`
	ModelRouteDigest     string    `gorm:"column:model_route_digest"`
	BudgetRef            string    `gorm:"column:budget_ref"`
	BudgetDigest         string    `gorm:"column:budget_digest"`
	AccessScopeRef       string    `gorm:"column:access_scope_ref"`
	AccessScopeDigest    string    `gorm:"column:access_scope_digest"`
	ContextDigest        string    `gorm:"column:context_digest"`
	CreatedAt            time.Time `gorm:"column:created_at"`
}

func (userMessageContextModel) TableName() string { return "agent.session_user_message_turn_context" }

type userMessageLegacyUpgradeLedgerModel struct {
	InputID           string    `gorm:"column:input_id;type:uuid;primaryKey"`
	SessionID         string    `gorm:"column:session_id;type:uuid"`
	Stage             string    `gorm:"column:stage"`
	TurnID            string    `gorm:"column:turn_id;type:uuid"`
	ContextDigest     string    `gorm:"column:context_digest"`
	UpgradeGeneration int64     `gorm:"column:upgrade_generation"`
	Version           int64     `gorm:"column:version"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (userMessageLegacyUpgradeLedgerModel) TableName() string {
	return "agent.session_user_message_upgrade_ledger"
}

type userMessageRunModel struct {
	RunID       string     `gorm:"column:run_id;type:uuid;primaryKey"`
	TurnID      string     `gorm:"column:turn_id;type:uuid"`
	InputID     string     `gorm:"column:input_id;type:uuid"`
	SessionID   string     `gorm:"column:session_id;type:uuid"`
	OwnerFence  int64      `gorm:"column:owner_fence"`
	Status      string     `gorm:"column:status"`
	Version     int64      `gorm:"column:version"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	StartedAt   *time.Time `gorm:"column:started_at"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

func (userMessageRunModel) TableName() string { return "agent.session_user_message_run" }

type userMessageModelReceiptModel struct {
	ModelCallID        string     `gorm:"column:model_call_id;type:uuid;primaryKey"`
	RunID              string     `gorm:"column:run_id;type:uuid"`
	TurnID             string     `gorm:"column:turn_id;type:uuid"`
	InputID            string     `gorm:"column:input_id;type:uuid"`
	RequestDigest      string     `gorm:"column:request_digest"`
	ExecutionFence     int64      `gorm:"column:execution_fence"`
	Status             string     `gorm:"column:status"`
	ResponseCiphertext []byte     `gorm:"column:response_ciphertext"`
	ResponseKeyVersion *string    `gorm:"column:response_key_version"`
	ResponseDigest     *string    `gorm:"column:response_digest"`
	ErrorCode          *string    `gorm:"column:error_code"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	CompletedAt        *time.Time `gorm:"column:completed_at"`
}

func (userMessageModelReceiptModel) TableName() string {
	return "agent.session_user_message_model_receipt"
}

type userMessageOutputReceiptModel struct {
	OutputID         string     `gorm:"column:output_id;type:uuid;primaryKey"`
	RunID            string     `gorm:"column:run_id;type:uuid"`
	TurnID           string     `gorm:"column:turn_id;type:uuid"`
	InputID          string     `gorm:"column:input_id;type:uuid"`
	ProjectionKey    string     `gorm:"column:projection_key"`
	SchemaVersion    string     `gorm:"column:schema_version"`
	Status           string     `gorm:"column:status"`
	ResultCiphertext []byte     `gorm:"column:result_ciphertext"`
	ResultKeyVersion *string    `gorm:"column:result_key_version"`
	ResultDigest     *string    `gorm:"column:result_digest"`
	ErrorCode        *string    `gorm:"column:error_code"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
	CompletedAt      *time.Time `gorm:"column:completed_at"`
}

func (userMessageOutputReceiptModel) TableName() string {
	return "agent.session_user_message_output_receipt"
}

type userMessageOutputProjectionModel struct {
	SessionID         string    `gorm:"column:session_id;type:uuid;primaryKey"`
	SourceInputID     string    `gorm:"column:source_input_id;type:uuid"`
	SourceEnqueueSeq  int64     `gorm:"column:source_enqueue_seq"`
	TurnID            string    `gorm:"column:turn_id;type:uuid"`
	RunID             string    `gorm:"column:run_id;type:uuid"`
	SchemaVersion     string    `gorm:"column:schema_version"`
	Status            string    `gorm:"column:status"`
	Payload           string    `gorm:"column:payload;type:jsonb"`
	ProjectionVersion int64     `gorm:"column:projection_version"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (userMessageOutputProjectionModel) TableName() string {
	return "agent.session_user_message_output_projection"
}
