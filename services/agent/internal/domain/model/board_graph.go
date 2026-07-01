package model

import (
	"time"

	"gorm.io/datatypes"
)

type AgentRunRecord struct {
	RunID                string    `gorm:"column:run_id;primaryKey"`
	SessionID            string    `gorm:"column:session_id"`
	ProjectID            string    `gorm:"column:project_id"`
	Status               string    `gorm:"column:status"`
	RouterDecisionDigest string    `gorm:"column:router_decision_digest"`
	CurrentBoardID       string    `gorm:"column:current_board_id"`
	CurrentGraphPlanID   string    `gorm:"column:current_graph_plan_id"`
	TraceID              string    `gorm:"column:trace_id"`
	CreatedAt            time.Time `gorm:"column:created_at"`
	UpdatedAt            time.Time `gorm:"column:updated_at"`
}

func (AgentRunRecord) TableName() string { return "agent_runs" }

type RunEventRecord struct {
	EventID              string         `gorm:"column:event_id;primaryKey"`
	RunID                string         `gorm:"column:run_id"`
	Seq                  int64          `gorm:"column:seq"`
	EventType            string         `gorm:"column:event_type"`
	PayloadSchemaVersion string         `gorm:"column:payload_schema_version"`
	DedupeKey            string         `gorm:"column:dedupe_key"`
	PayloadDigest        string         `gorm:"column:payload_digest"`
	Payload              datatypes.JSON `gorm:"column:payload;type:jsonb"`
	TraceID              string         `gorm:"column:trace_id"`
	CreatedAt            time.Time      `gorm:"column:created_at"`
}

func (RunEventRecord) TableName() string { return "agent_run_events" }

type CreativeBoardRecord struct {
	BoardID         string     `gorm:"column:board_id;primaryKey"`
	ProjectID       string     `gorm:"column:project_id"`
	SessionID       string     `gorm:"column:session_id"`
	RunID           string     `gorm:"column:run_id"`
	GraphPlanID     string     `gorm:"column:graph_plan_id"`
	Title           string     `gorm:"column:title"`
	Status          string     `gorm:"column:status"`
	Version         int        `gorm:"column:version"`
	ElementsCount   int        `gorm:"column:elements_count"`
	BoardDigest     string     `gorm:"column:board_digest"`
	ApprovedAt      *time.Time `gorm:"column:approved_at"`
	ApprovedBy      string     `gorm:"column:approved_by"`
	ToolPlanAllowed bool       `gorm:"column:tool_plan_allowed"`
	CreatedAt       time.Time  `gorm:"column:created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at"`
}

func (CreativeBoardRecord) TableName() string { return "creative_boards" }

type CreativeElementRecord struct {
	ElementID      string         `gorm:"column:element_id;primaryKey"`
	BoardID        string         `gorm:"column:board_id"`
	ElementType    string         `gorm:"column:element_type"`
	Source         string         `gorm:"column:source"`
	Status         string         `gorm:"column:status"`
	Position       datatypes.JSON `gorm:"column:position;type:jsonb"`
	Content        datatypes.JSON `gorm:"column:content;type:jsonb"`
	LinkedAssetIDs datatypes.JSON `gorm:"column:linked_asset_ids;type:jsonb"`
	ContentDigest  string         `gorm:"column:content_digest"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
}

func (CreativeElementRecord) TableName() string { return "creative_elements" }

type BoardPatchRecord struct {
	PatchID        string         `gorm:"column:patch_id;primaryKey"`
	BoardID        string         `gorm:"column:board_id"`
	BaseVersion    int            `gorm:"column:base_version"`
	TargetVersion  int            `gorm:"column:target_version"`
	Operation      string         `gorm:"column:operation"`
	Actor          string         `gorm:"column:actor"`
	IdempotencyKey string         `gorm:"column:idempotency_key"`
	Payload        datatypes.JSON `gorm:"column:payload;type:jsonb"`
	PatchDigest    string         `gorm:"column:patch_digest"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
}

func (BoardPatchRecord) TableName() string { return "board_patches" }

type GraphTemplateRecord struct {
	GraphTemplateID string         `gorm:"column:graph_template_id;primaryKey"`
	Name            string         `gorm:"column:name"`
	Version         string         `gorm:"column:version"`
	GraphType       string         `gorm:"column:graph_type"`
	SkillLevel      string         `gorm:"column:skill_level"`
	EntryNode       string         `gorm:"column:entry_node"`
	TerminalNodes   datatypes.JSON `gorm:"column:terminal_nodes;type:jsonb"`
	Nodes           datatypes.JSON `gorm:"column:nodes;type:jsonb"`
	Edges           datatypes.JSON `gorm:"column:edges;type:jsonb"`
	TemplateDigest  string         `gorm:"column:template_digest"`
	CreatedAt       time.Time      `gorm:"column:created_at"`
}

func (GraphTemplateRecord) TableName() string { return "graph_templates" }

type GraphPlanRecord struct {
	GraphPlanID          string         `gorm:"column:graph_plan_id;primaryKey"`
	GraphTemplateID      string         `gorm:"column:graph_template_id"`
	GraphTemplateVersion string         `gorm:"column:graph_template_version"`
	RunID                string         `gorm:"column:run_id"`
	BoardID              string         `gorm:"column:board_id"`
	Status               string         `gorm:"column:status"`
	CurrentNode          string         `gorm:"column:current_node"`
	ValueDeliveredStage  string         `gorm:"column:value_delivered_stage"`
	Nodes                datatypes.JSON `gorm:"column:nodes;type:jsonb"`
	Edges                datatypes.JSON `gorm:"column:edges;type:jsonb"`
	GraphPlanDigest      string         `gorm:"column:graph_plan_digest"`
	CreatedAt            time.Time      `gorm:"column:created_at"`
	UpdatedAt            time.Time      `gorm:"column:updated_at"`
}

func (GraphPlanRecord) TableName() string { return "graph_plans" }

type GraphCheckpointRecord struct {
	CheckpointID   string     `gorm:"column:checkpoint_id;primaryKey"`
	GraphPlanID    string     `gorm:"column:graph_plan_id"`
	RunID          string     `gorm:"column:run_id"`
	NodeID         string     `gorm:"column:node_id"`
	CheckpointType string     `gorm:"column:checkpoint_type"`
	Status         string     `gorm:"column:status"`
	StateDigest    string     `gorm:"column:state_digest"`
	Resumable      bool       `gorm:"column:resumable"`
	ExpiresAt      *time.Time `gorm:"column:expires_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
}

func (GraphCheckpointRecord) TableName() string { return "graph_checkpoints" }

type ToolPlanRecord struct {
	ToolPlanID           string         `gorm:"column:tool_plan_id;primaryKey"`
	RunID                string         `gorm:"column:run_id"`
	BoardID              string         `gorm:"column:board_id"`
	BoardVersion         int            `gorm:"column:board_version"`
	GraphPlanID          string         `gorm:"column:graph_plan_id"`
	Status               string         `gorm:"column:status"`
	Items                datatypes.JSON `gorm:"column:items;type:jsonb"`
	EstimatedCredits     int            `gorm:"column:estimated_credits"`
	Currency             string         `gorm:"column:currency"`
	ConfirmationRequired bool           `gorm:"column:confirmation_required"`
	ExpiresAt            *time.Time     `gorm:"column:expires_at"`
	ToolPlanDigest       string         `gorm:"column:tool_plan_digest"`
	CreatedAt            time.Time      `gorm:"column:created_at"`
	UpdatedAt            time.Time      `gorm:"column:updated_at"`
}

func (ToolPlanRecord) TableName() string { return "tool_plans" }

type ToolTaskRecord struct {
	ToolTaskID     string         `gorm:"column:tool_task_id;primaryKey"`
	ToolPlanID     string         `gorm:"column:tool_plan_id"`
	ToolPlanItemID string         `gorm:"column:tool_plan_item_id"`
	RunID          string         `gorm:"column:run_id"`
	Status         string         `gorm:"column:status"`
	Progress       int            `gorm:"column:progress"`
	ProviderPolicy datatypes.JSON `gorm:"column:provider_policy;type:jsonb"`
	IdempotencyKey string         `gorm:"column:idempotency_key"`
	InputDigest    string         `gorm:"column:input_digest"`
	OutputDigest   string         `gorm:"column:output_digest"`
	ErrorCode      string         `gorm:"column:error_code"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
}

func (ToolTaskRecord) TableName() string { return "tool_tasks" }
