package model

import (
	"time"

	"gorm.io/datatypes"
)

type Session struct {
	ID                string         `gorm:"column:id;primaryKey"`               // 会话ID
	TenantID          string         `gorm:"column:tenant_id"`                   // 租户ID
	SpaceID           string         `gorm:"column:space_id"`                    // 空间ID
	ProjectID         string         `gorm:"column:project_id"`                  // 项目ID
	UserID            string         `gorm:"column:user_id"`                     // 用户ID
	Status            string         `gorm:"column:status"`                      // 会话状态
	Title             string         `gorm:"column:title"`                       // 会话标题
	LastRunID         string         `gorm:"column:last_run_id"`                 // 最近运行ID
	LastEventSequence int64          `gorm:"column:last_event_sequence"`         // 最近事件序号
	SnapshotSummary   datatypes.JSON `gorm:"column:snapshot_summary;type:jsonb"` // 会话快照摘要
	IdempotencyKey    string         `gorm:"column:idempotency_key"`             // 创建幂等键
	TraceID           string         `gorm:"column:trace_id"`                    // 链路追踪ID
	CreatedAt         time.Time      `gorm:"column:created_at"`                  // 创建时间
	UpdatedAt         time.Time      `gorm:"column:updated_at"`                  // 更新时间
	DeletedAt         *time.Time     `gorm:"column:deleted_at"`                  // 软删除时间
}

func (Session) TableName() string { return "agent_sessions" }

type Run struct {
	ID                     string         `gorm:"column:id;primaryKey"`                       // 运行ID
	SessionID              string         `gorm:"column:session_id"`                          // 会话ID
	ProjectID              string         `gorm:"column:project_id"`                          // 项目ID
	SpaceID                string         `gorm:"column:space_id"`                            // 空间ID
	UserID                 string         `gorm:"column:user_id"`                             // 用户ID
	TurnNo                 int64          `gorm:"column:turn_no"`                             // 多轮轮次
	Status                 string         `gorm:"column:status"`                              // 运行状态
	InputSummary           datatypes.JSON `gorm:"column:input_summary;type:jsonb"`            // 输入摘要
	SkillSelection         datatypes.JSON `gorm:"column:skill_selection;type:jsonb"`          // 技能选择快照
	ModelSelectionSnapshot datatypes.JSON `gorm:"column:model_selection_snapshot;type:jsonb"` // 模型选择快照
	RuntimeConfigVersion   string         `gorm:"column:runtime_config_version"`              // 运行配置版本
	IdempotencyKey         string         `gorm:"column:idempotency_key"`                     // 创建幂等键
	ErrorCode              string         `gorm:"column:error_code"`                          // 错误码
	ErrorMessage           string         `gorm:"column:error_message"`                       // 错误信息
	TraceID                string         `gorm:"column:trace_id"`                            // 链路追踪ID
	StartedAt              *time.Time     `gorm:"column:started_at"`                          // 开始时间
	CompletedAt            *time.Time     `gorm:"column:completed_at"`                        // 完成时间
	CreatedAt              time.Time      `gorm:"column:created_at"`                          // 创建时间
	UpdatedAt              time.Time      `gorm:"column:updated_at"`                          // 更新时间
	DeletedAt              *time.Time     `gorm:"column:deleted_at"`                          // 软删除时间
}

func (Run) TableName() string { return "agent_runs" }

type Message struct {
	ID             string         `gorm:"column:id;primaryKey"`              // 消息ID
	SessionID      string         `gorm:"column:session_id"`                 // 会话ID
	RunID          string         `gorm:"column:run_id"`                     // 运行ID
	Role           string         `gorm:"column:role"`                       // 消息角色
	ContentType    string         `gorm:"column:content_type"`               // 内容类型
	Content        string         `gorm:"column:content"`                    // 消息内容
	ContentSummary datatypes.JSON `gorm:"column:content_summary;type:jsonb"` // 内容摘要
	Sequence       int64          `gorm:"column:sequence"`                   // 会话内序号
	SafetyStatus   string         `gorm:"column:safety_status"`              // 安全状态
	Metadata       datatypes.JSON `gorm:"column:metadata;type:jsonb"`        // 扩展元数据
	TraceID        string         `gorm:"column:trace_id"`                   // 链路追踪ID
	CreatedAt      time.Time      `gorm:"column:created_at"`                 // 创建时间
	UpdatedAt      time.Time      `gorm:"column:updated_at"`                 // 更新时间
	DeletedAt      *time.Time     `gorm:"column:deleted_at"`                 // 软删除时间
}

func (Message) TableName() string { return "agent_messages" }

type Event struct {
	EventID              string         `gorm:"column:event_id;primaryKey"`    // 事件ID
	Type                 string         `gorm:"column:type"`                   // AG-UI 事件类型
	SessionID            string         `gorm:"column:session_id"`             // 会话ID
	RunID                string         `gorm:"column:run_id"`                 // 运行ID
	ProjectID            string         `gorm:"column:project_id"`             // 项目ID
	SpaceID              string         `gorm:"column:space_id"`               // 空间ID
	ActorUserID          string         `gorm:"column:actor_user_id"`          // 操作用户ID
	Sequence             int64          `gorm:"column:sequence"`               // 运行内事件序号
	Component            string         `gorm:"column:component"`              // 事件来源组件
	Payload              datatypes.JSON `gorm:"column:payload;type:jsonb"`     // 事件载荷
	PayloadSchemaVersion string         `gorm:"column:payload_schema_version"` // payload schema 版本
	Visibility           string         `gorm:"column:visibility"`             // 可见性
	TraceID              string         `gorm:"column:trace_id"`               // 链路追踪ID
	CreatedAt            time.Time      `gorm:"column:created_at"`             // 创建时间
}

func (Event) TableName() string { return "agent_events" }

type ToolCall struct {
	ID             string         `gorm:"column:id;primaryKey"`             // 工具调用ID
	RunID          string         `gorm:"column:run_id"`                    // 运行ID
	TaskID         string         `gorm:"column:task_id"`                   // 任务ID
	ToolName       string         `gorm:"column:tool_name"`                 // 工具名称
	ToolType       string         `gorm:"column:tool_type"`                 // 工具类型
	RiskLevel      string         `gorm:"column:risk_level"`                // 风险等级
	Status         string         `gorm:"column:status"`                    // 调用状态
	InputSummary   datatypes.JSON `gorm:"column:input_summary;type:jsonb"`  // 输入摘要
	OutputSummary  datatypes.JSON `gorm:"column:output_summary;type:jsonb"` // 输出摘要
	IdempotencyKey string         `gorm:"column:idempotency_key"`           // 幂等键
	TimeoutMS      int            `gorm:"column:timeout_ms"`                // 超时时间毫秒
	RetryCount     int            `gorm:"column:retry_count"`               // 重试次数
	ErrorCode      string         `gorm:"column:error_code"`                // 错误码
	LatencyMS      int64          `gorm:"column:latency_ms"`                // 延迟毫秒
	TraceID        string         `gorm:"column:trace_id"`                  // 链路追踪ID
	CreatedAt      time.Time      `gorm:"column:created_at"`                // 创建时间
	UpdatedAt      time.Time      `gorm:"column:updated_at"`                // 更新时间
	DeletedAt      *time.Time     `gorm:"column:deleted_at"`                // 软删除时间
}

func (ToolCall) TableName() string { return "agent_tool_calls" }

type Task struct {
	ID              string         `gorm:"column:id;primaryKey"`              // 任务ID
	RunID           string         `gorm:"column:run_id"`                     // 运行ID
	TaskType        string         `gorm:"column:task_type"`                  // 任务类型
	ResourceType    string         `gorm:"column:resource_type"`              // 资源类型
	Status          string         `gorm:"column:status"`                     // 任务状态
	ProgressPercent int            `gorm:"column:progress_percent"`           // 进度百分比
	ProgressDetail  datatypes.JSON `gorm:"column:progress_detail;type:jsonb"` // 进度详情
	CancelRequested bool           `gorm:"column:cancel_requested"`           // 是否请求取消
	ExternalTaskRef string         `gorm:"column:external_task_ref"`          // 外部任务引用
	ErrorCode       string         `gorm:"column:error_code"`                 // 错误码
	StartedAt       *time.Time     `gorm:"column:started_at"`                 // 开始时间
	CompletedAt     *time.Time     `gorm:"column:completed_at"`               // 完成时间
	TraceID         string         `gorm:"column:trace_id"`                   // 链路追踪ID
	CreatedAt       time.Time      `gorm:"column:created_at"`                 // 创建时间
	UpdatedAt       time.Time      `gorm:"column:updated_at"`                 // 更新时间
	DeletedAt       *time.Time     `gorm:"column:deleted_at"`                 // 软删除时间
}

func (Task) TableName() string { return "agent_tasks" }

type Interrupt struct {
	ID                  string         `gorm:"column:id;primaryKey"`                   // 中断ID
	RunID               string         `gorm:"column:run_id"`                          // 运行ID
	InterruptType       string         `gorm:"column:interrupt_type"`                  // 中断类型
	Status              string         `gorm:"column:status"`                          // 中断状态
	Reason              string         `gorm:"column:reason"`                          // 中断原因
	ConfirmationPayload datatypes.JSON `gorm:"column:confirmation_payload;type:jsonb"` // 确认载荷
	AllowedActions      datatypes.JSON `gorm:"column:allowed_actions;type:jsonb"`      // 允许动作
	ResumeContext       datatypes.JSON `gorm:"column:resume_context;type:jsonb"`       // 恢复上下文
	IdempotencyKey      string         `gorm:"column:idempotency_key"`                 // 幂等键
	ExpiresAt           time.Time      `gorm:"column:expires_at"`                      // 过期时间
	ResolvedAt          *time.Time     `gorm:"column:resolved_at"`                     // 解决时间
	TraceID             string         `gorm:"column:trace_id"`                        // 链路追踪ID
	CreatedAt           time.Time      `gorm:"column:created_at"`                      // 创建时间
	UpdatedAt           time.Time      `gorm:"column:updated_at"`                      // 更新时间
	DeletedAt           *time.Time     `gorm:"column:deleted_at"`                      // 软删除时间
}

func (Interrupt) TableName() string { return "agent_interrupts" }

type Artifact struct {
	ID            string         `gorm:"column:id;primaryKey"`      // 产物ID
	SessionID     string         `gorm:"column:session_id"`         // 会话ID
	ProjectID     string         `gorm:"column:project_id"`         // 项目ID
	RunID         string         `gorm:"column:run_id"`             // 运行ID
	ArtifactType  string         `gorm:"column:artifact_type"`      // 产物类型
	Status        string         `gorm:"column:status"`             // 产物状态
	ElementType   string         `gorm:"column:element_type"`       // 资产元素类型
	Content       datatypes.JSON `gorm:"column:content;type:jsonb"` // 产物内容
	BusinessRefID string         `gorm:"column:business_ref_id"`    // 业务引用ID
	Visibility    string         `gorm:"column:visibility"`         // 可见性
	Version       int            `gorm:"column:version"`            // 版本号
	TraceID       string         `gorm:"column:trace_id"`           // 链路追踪ID
	CreatedAt     time.Time      `gorm:"column:created_at"`         // 创建时间
	UpdatedAt     time.Time      `gorm:"column:updated_at"`         // 更新时间
	DeletedAt     *time.Time     `gorm:"column:deleted_at"`         // 软删除时间
}

func (Artifact) TableName() string { return "agent_artifacts" }

type SafetyEvaluation struct {
	SafetyEvidenceID      string     `gorm:"column:safety_evidence_id;primaryKey"` // 安全证据ID
	Scene                 string     `gorm:"column:scene"`                         // 安全场景
	TargetType            string     `gorm:"column:target_type"`                   // 目标类型
	TargetRefID           string     `gorm:"column:target_ref_id"`                 // 目标引用ID
	EvaluatedObjectDigest string     `gorm:"column:evaluated_object_digest"`       // 评估对象摘要
	PolicyVersion         string     `gorm:"column:policy_version"`                // 策略版本
	EvidenceVersion       string     `gorm:"column:evidence_version"`              // 证据版本
	Result                string     `gorm:"column:result"`                        // 评估结果
	UserVisibleReason     string     `gorm:"column:user_visible_reason"`           // 用户可见原因
	SourceSessionID       string     `gorm:"column:source_session_id"`             // 来源会话ID
	SourceRunID           string     `gorm:"column:source_run_id"`                 // 来源运行ID
	SourceArtifactID      string     `gorm:"column:source_artifact_id"`            // 来源产物ID
	TraceID               string     `gorm:"column:trace_id"`                      // 链路追踪ID
	EvaluatedAt           time.Time  `gorm:"column:evaluated_at"`                  // 评估时间
	ExpiresAt             time.Time  `gorm:"column:expires_at"`                    // 过期时间
	CreatedAt             time.Time  `gorm:"column:created_at"`                    // 创建时间
	UpdatedAt             time.Time  `gorm:"column:updated_at"`                    // 更新时间
	DeletedAt             *time.Time `gorm:"column:deleted_at"`                    // 软删除时间
}

func (SafetyEvaluation) TableName() string { return "agent_safety_evaluations" }

type Memory struct {
	ID              string         `gorm:"column:id;primaryKey"`              // 记忆ID
	UserID          string         `gorm:"column:user_id"`                    // 用户ID
	SpaceID         string         `gorm:"column:space_id"`                   // 空间ID
	MemoryType      string         `gorm:"column:memory_type"`                // 记忆类型
	Scope           string         `gorm:"column:scope"`                      // 记忆范围
	ContentSummary  datatypes.JSON `gorm:"column:content_summary;type:jsonb"` // 内容摘要
	Authorized      bool           `gorm:"column:authorized"`                 // 是否授权
	ExpiresAt       *time.Time     `gorm:"column:expires_at"`                 // 过期时间
	SourceSessionID string         `gorm:"column:source_session_id"`          // 来源会话ID
	TraceID         string         `gorm:"column:trace_id"`                   // 链路追踪ID
	CreatedAt       time.Time      `gorm:"column:created_at"`                 // 创建时间
	UpdatedAt       time.Time      `gorm:"column:updated_at"`                 // 更新时间
	DeletedAt       *time.Time     `gorm:"column:deleted_at"`                 // 软删除时间
}

func (Memory) TableName() string { return "agent_memories" }

type RuntimeConfig struct {
	ConfigKey      string         `gorm:"column:config_key;primaryKey"`       // 配置键
	Version        string         `gorm:"column:version;primaryKey"`          // 配置版本
	Status         string         `gorm:"column:status"`                      // 配置状态
	Owner          string         `gorm:"column:owner"`                       // 配置负责人
	Content        datatypes.JSON `gorm:"column:content;type:jsonb"`          // 配置内容
	SafeConfigRefs datatypes.JSON `gorm:"column:safe_config_refs;type:jsonb"` // 安全配置引用
	ActivatedAt    *time.Time     `gorm:"column:activated_at"`                // 激活时间
	DeprecatedAt   *time.Time     `gorm:"column:deprecated_at"`               // 废弃时间
	CreatedAt      time.Time      `gorm:"column:created_at"`                  // 创建时间
	UpdatedAt      time.Time      `gorm:"column:updated_at"`                  // 更新时间
}

func (RuntimeConfig) TableName() string { return "agent_runtime_configs" }
