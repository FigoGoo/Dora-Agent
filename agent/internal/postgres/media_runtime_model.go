package postgres

import "time"

// mediaPreviewRequestModel 保存两类媒体 ingress 的 canonical Intent 与首次生成的稳定执行身份。
// Session Input 持有可变处理状态，本表只保存 first-write-wins 不可变事实。
type mediaPreviewRequestModel struct {
	RequestID           string    `gorm:"column:request_id;type:uuid;primaryKey"`
	SessionID           string    `gorm:"column:session_id;type:uuid"`
	UserID              string    `gorm:"column:user_id;type:uuid"`
	ProjectID           string    `gorm:"column:project_id;type:uuid"`
	IdempotencyKey      string    `gorm:"column:idempotency_key;type:uuid"`
	RequestDigest       string    `gorm:"column:request_digest"`
	ToolKey             string    `gorm:"column:tool_key"`
	IntentSchemaVersion string    `gorm:"column:intent_schema_version"`
	IntentDigest        string    `gorm:"column:intent_digest"`
	Intent              string    `gorm:"column:intent;type:jsonb"`
	InputID             string    `gorm:"column:input_id;type:uuid"`
	TurnID              string    `gorm:"column:turn_id;type:uuid"`
	RunID               string    `gorm:"column:run_id;type:uuid"`
	ToolCallID          string    `gorm:"column:tool_call_id;type:uuid"`
	AcceptedEventID     string    `gorm:"column:accepted_event_id;type:uuid"`
	TerminalEventID     string    `gorm:"column:terminal_event_id;type:uuid"`
	DeadlineAt          time.Time `gorm:"column:deadline_at"`
	CreatedAt           time.Time `gorm:"column:created_at"`
}

func (mediaPreviewRequestModel) TableName() string { return "agent.media_preview_request" }

type mediaPreviewOperationModel struct {
	OperationID               string     `gorm:"column:operation_id;type:uuid;primaryKey"`
	ToolCallID                string     `gorm:"column:tool_call_id;type:uuid"`
	ScopeDigest               string     `gorm:"column:scope_digest"`
	ToolKey                   string     `gorm:"column:tool_key"`
	OutputProfile             string     `gorm:"column:output_profile"`
	SessionID                 string     `gorm:"column:session_id;type:uuid"`
	UserID                    string     `gorm:"column:user_id;type:uuid"`
	ProjectID                 string     `gorm:"column:project_id;type:uuid"`
	InputID                   string     `gorm:"column:input_id;type:uuid"`
	TurnID                    string     `gorm:"column:turn_id;type:uuid"`
	RunID                     string     `gorm:"column:run_id;type:uuid"`
	PlannedBatchID            string     `gorm:"column:planned_batch_id;type:uuid"`
	PlannedJobID              string     `gorm:"column:planned_job_id;type:uuid"`
	PlannedDispatchEventID    string     `gorm:"column:planned_dispatch_event_id;type:uuid"`
	PreparationRequestID      string     `gorm:"column:preparation_request_id;type:uuid"`
	PreparationCommandID      string     `gorm:"column:preparation_command_id;type:uuid"`
	PreparationRequestDigest  *string    `gorm:"column:preparation_request_digest"`
	PreparationRequest        *string    `gorm:"column:preparation_request;type:jsonb"`
	PreparationID             *string    `gorm:"column:preparation_id;type:uuid"`
	PreparationResponseDigest *string    `gorm:"column:preparation_response_digest"`
	PreparationResponse       *string    `gorm:"column:preparation_response;type:jsonb"`
	DispatchDigest            *string    `gorm:"column:dispatch_digest"`
	FailureCode               *string    `gorm:"column:failure_code"`
	RecoveryReasonCode        *string    `gorm:"column:recovery_reason_code"`
	Status                    string     `gorm:"column:status"`
	Version                   int64      `gorm:"column:version"`
	CreatedAt                 time.Time  `gorm:"column:created_at"`
	UpdatedAt                 time.Time  `gorm:"column:updated_at"`
	AcceptedAt                *time.Time `gorm:"column:accepted_at"`
	CompletedAt               *time.Time `gorm:"column:completed_at"`
}

func (mediaPreviewOperationModel) TableName() string { return "agent.media_preview_operation" }

type mediaPreviewBatchModel struct {
	BatchID     string     `gorm:"column:batch_id;type:uuid;primaryKey"`
	OperationID string     `gorm:"column:operation_id;type:uuid"`
	Status      string     `gorm:"column:status"`
	Version     int64      `gorm:"column:version"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	StartedAt   *time.Time `gorm:"column:started_at"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

func (mediaPreviewBatchModel) TableName() string { return "agent.media_preview_batch" }

type mediaPreviewJobModel struct {
	JobID                    string     `gorm:"column:job_id;type:uuid;primaryKey"`
	BatchID                  string     `gorm:"column:batch_id;type:uuid"`
	OperationID              string     `gorm:"column:operation_id;type:uuid"`
	SessionID                string     `gorm:"column:session_id;type:uuid"`
	UserID                   string     `gorm:"column:user_id;type:uuid"`
	ProjectID                string     `gorm:"column:project_id;type:uuid"`
	JobType                  string     `gorm:"column:job_type"`
	DefinitionVersion        string     `gorm:"column:definition_version"`
	ScopeDigest              string     `gorm:"column:scope_digest"`
	OutputProfile            string     `gorm:"column:output_profile"`
	SourceRef                string     `gorm:"column:source_ref;type:jsonb"`
	Target                   string     `gorm:"column:target;type:jsonb"`
	ArtifactRequestDigest    string     `gorm:"column:artifact_request_digest"`
	Priority                 int        `gorm:"column:priority"`
	Status                   string     `gorm:"column:status"`
	AvailableAt              time.Time  `gorm:"column:available_at"`
	AttemptCount             int        `gorm:"column:attempt_count"`
	AttemptID                *string    `gorm:"column:attempt_id;type:uuid"`
	ClaimRequestID           *string    `gorm:"column:claim_request_id;type:uuid"`
	LeaseOwner               *string    `gorm:"column:lease_owner"`
	LeaseExpiresAt           *time.Time `gorm:"column:lease_expires_at"`
	Fence                    int64      `gorm:"column:fence"`
	RetryCount               int        `gorm:"column:retry_count"`
	LastErrorCode            *string    `gorm:"column:last_error_code"`
	ReconciliationReasonCode *string    `gorm:"column:reconciliation_reason_code"`
	ResultSchemaVersion      *string    `gorm:"column:result_schema_version"`
	ResultDigest             *string    `gorm:"column:result_digest"`
	Result                   *string    `gorm:"column:result;type:jsonb"`
	TerminalEventID          *string    `gorm:"column:terminal_event_id;type:uuid"`
	CreatedAt                time.Time  `gorm:"column:created_at"`
	UpdatedAt                time.Time  `gorm:"column:updated_at"`
	StartedAt                *time.Time `gorm:"column:started_at"`
	CompletedAt              *time.Time `gorm:"column:completed_at"`
	DeadlineAt               time.Time  `gorm:"column:deadline_at"`
}

func (mediaPreviewJobModel) TableName() string { return "agent.media_preview_job" }

type mediaPreviewDispatchOutboxModel struct {
	EventID       string     `gorm:"column:event_id;type:uuid;primaryKey"`
	JobID         string     `gorm:"column:job_id;type:uuid"`
	SchemaVersion string     `gorm:"column:schema_version"`
	PayloadDigest string     `gorm:"column:payload_digest"`
	Payload       string     `gorm:"column:payload;type:jsonb"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	DeliveredAt   *time.Time `gorm:"column:delivered_at"`
}

func (mediaPreviewDispatchOutboxModel) TableName() string {
	return "agent.media_preview_dispatch_outbox"
}

type mediaPreviewTerminalOutboxModel struct {
	EventID             string     `gorm:"column:event_id;type:uuid;primaryKey"`
	SessionID           string     `gorm:"column:session_id;type:uuid"`
	OperationID         string     `gorm:"column:operation_id;type:uuid"`
	BatchID             string     `gorm:"column:batch_id;type:uuid"`
	JobID               string     `gorm:"column:job_id;type:uuid"`
	ToolKey             string     `gorm:"column:tool_key"`
	TerminalStatus      string     `gorm:"column:terminal_status"`
	ResultSchemaVersion string     `gorm:"column:result_schema_version"`
	ResultDigest        string     `gorm:"column:result_digest"`
	Result              string     `gorm:"column:result;type:jsonb"`
	OccurredAt          time.Time  `gorm:"column:occurred_at"`
	DeliveredAt         *time.Time `gorm:"column:delivered_at"`
	LaneInputID         *string    `gorm:"column:lane_input_id;type:uuid"`
}

func (mediaPreviewTerminalOutboxModel) TableName() string {
	return "agent.media_preview_terminal_outbox"
}
