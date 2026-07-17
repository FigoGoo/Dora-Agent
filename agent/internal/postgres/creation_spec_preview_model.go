package postgres

import "time"

// creationSpecPreviewRunModel 映射 Preview Input 的稳定执行身份，技术重试不得替换这些 ID。
type creationSpecPreviewRunModel struct {
	InputID           string    `gorm:"column:input_id;type:uuid;primaryKey"`
	RequestID         string    `gorm:"column:request_id;type:uuid"`
	IdempotencyKey    string    `gorm:"column:idempotency_key;type:uuid"`
	RequestDigest     string    `gorm:"column:request_digest"`
	SessionID         string    `gorm:"column:session_id;type:uuid"`
	UserID            string    `gorm:"column:user_id;type:uuid"`
	ProjectID         string    `gorm:"column:project_id;type:uuid"`
	MessageID         string    `gorm:"column:message_id;type:uuid"`
	TurnID            string    `gorm:"column:turn_id;type:uuid"`
	RunID             string    `gorm:"column:run_id;type:uuid"`
	ToolCallID        string    `gorm:"column:tool_call_id;type:uuid"`
	BusinessCommandID string    `gorm:"column:business_command_id;type:uuid"`
	TerminalEventID   string    `gorm:"column:terminal_event_id;type:uuid"`
	PromptVersion     string    `gorm:"column:prompt_version"`
	ValidatorVersion  string    `gorm:"column:validator_version"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Preview Run 的显式 Agent Schema 表名。
func (creationSpecPreviewRunModel) TableName() string { return "agent.creation_spec_preview_run" }

// creationSpecPreviewModelReceiptModel 映射 Graph ChatModel Node 的 first-write-wins 响应密文。
type creationSpecPreviewModelReceiptModel struct {
	ToolCallID         string     `gorm:"column:tool_call_id;type:uuid;primaryKey"`
	CallIndex          int        `gorm:"column:call_index;primaryKey"`
	RequestDigest      string     `gorm:"column:request_digest"`
	Status             string     `gorm:"column:status"`
	ResponseCiphertext []byte     `gorm:"column:response_ciphertext"`
	ResponseKeyVersion *string    `gorm:"column:response_key_version"`
	ResponseDigest     *string    `gorm:"column:response_digest"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	CompletedAt        *time.Time `gorm:"column:completed_at"`
}

// TableName 返回 Preview Model Receipt 的显式 Agent Schema 表名。
func (creationSpecPreviewModelReceiptModel) TableName() string {
	return "agent.creation_spec_preview_model_receipt"
}

// creationSpecPreviewToolReceiptModel 映射完整 Tool Result 或业务 Unknown Outcome 阶段。
type creationSpecPreviewToolReceiptModel struct {
	ToolCallID                   string     `gorm:"column:tool_call_id;type:uuid;primaryKey"`
	RequestDigest                string     `gorm:"column:request_digest"`
	Stage                        string     `gorm:"column:stage"`
	BusinessCommandID            string     `gorm:"column:business_command_id;type:uuid"`
	BusinessRequestDigest        *string    `gorm:"column:business_request_digest"`
	BusinessContentDigest        *string    `gorm:"column:business_content_digest"`
	BusinessCommandCiphertext    []byte     `gorm:"column:business_command_ciphertext"`
	BusinessCommandKeyVersion    *string    `gorm:"column:business_command_key_version"`
	BusinessCommandPayloadDigest *string    `gorm:"column:business_command_payload_digest"`
	BusinessResendAttempts       int        `gorm:"column:business_resend_attempts"`
	BusinessResendLimit          *int       `gorm:"column:business_resend_limit"`
	BusinessLastResendAt         *time.Time `gorm:"column:business_last_resend_at"`
	BusinessResendExhaustedAt    *time.Time `gorm:"column:business_resend_exhausted_at"`
	ResultCiphertext             []byte     `gorm:"column:result_ciphertext"`
	ResultKeyVersion             *string    `gorm:"column:result_key_version"`
	ResultDigest                 *string    `gorm:"column:result_digest"`
	ErrorCode                    *string    `gorm:"column:error_code"`
	CreatedAt                    time.Time  `gorm:"column:created_at"`
	UpdatedAt                    time.Time  `gorm:"column:updated_at"`
}

// TableName 返回 Preview Tool Receipt 的显式 Agent Schema 表名。
func (creationSpecPreviewToolReceiptModel) TableName() string {
	return "agent.creation_spec_preview_tool_receipt"
}

// creationSpecPreviewProjectionModel 映射 Workspace nullable 最新 Draft Card。
type creationSpecPreviewProjectionModel struct {
	SessionID          string    `gorm:"column:session_id;type:uuid;primaryKey"`
	SourceInputID      string    `gorm:"column:source_input_id;type:uuid"`
	SourceEnqueueSeq   int64     `gorm:"column:source_enqueue_seq"`
	SchemaVersion      string    `gorm:"column:schema_version"`
	ResourceID         string    `gorm:"column:resource_id;type:uuid"`
	ProjectID          string    `gorm:"column:project_id;type:uuid"`
	ResourceVersion    int64     `gorm:"column:resource_version"`
	ContentDigest      string    `gorm:"column:content_digest"`
	Status             string    `gorm:"column:status"`
	Title              string    `gorm:"column:title"`
	Goal               string    `gorm:"column:goal"`
	DeliverableType    string    `gorm:"column:deliverable_type"`
	Audience           *string   `gorm:"column:audience"`
	Locale             string    `gorm:"column:locale"`
	Phases             string    `gorm:"column:phases;type:jsonb"`
	Constraints        string    `gorm:"column:constraints;type:jsonb"`
	AcceptanceCriteria string    `gorm:"column:acceptance_criteria;type:jsonb"`
	UpdatedAt          time.Time `gorm:"column:updated_at"`
}

// TableName 返回 Preview Projection 的显式 Agent Schema 表名。
func (creationSpecPreviewProjectionModel) TableName() string {
	return "agent.creation_spec_preview_projection"
}
