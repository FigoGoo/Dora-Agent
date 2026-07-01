package businesscore

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type SystemFeatureFlag struct {
	ID             string         `gorm:"column:id;primaryKey"`
	FlagKey        string         `gorm:"column:flag_key"`
	Enabled        bool           `gorm:"column:enabled"`
	DefaultEnabled bool           `gorm:"column:default_enabled"`
	Description    *string        `gorm:"column:description"`
	CreatedBy      *string        `gorm:"column:created_by"`
	UpdatedBy      *string        `gorm:"column:updated_by"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (SystemFeatureFlag) TableName() string { return "system_feature_flags" }

type TestSeedRun struct {
	ID          string         `gorm:"column:id;primaryKey"`
	SeedRunID   string         `gorm:"column:seed_run_id"`
	FixtureID   string         `gorm:"column:fixture_id"`
	Status      string         `gorm:"column:status"`
	SummaryJSON datatypes.JSON `gorm:"column:summary_json;type:jsonb"`
	TraceID     *string        `gorm:"column:trace_id"`
	CreatedBy   *string        `gorm:"column:created_by"`
	UpdatedBy   *string        `gorm:"column:updated_by"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (TestSeedRun) TableName() string { return "test_seed_runs" }

type FakeProviderTask struct {
	ID             string         `gorm:"column:id;primaryKey"`
	ProviderTaskID string         `gorm:"column:provider_task_id"`
	ProviderKey    string         `gorm:"column:provider_key"`
	ToolID         string         `gorm:"column:tool_id"`
	Scenario       string         `gorm:"column:scenario"`
	LatencyMS      int            `gorm:"column:latency_ms"`
	ArtifactURI    *string        `gorm:"column:artifact_uri"`
	Status         string         `gorm:"column:status"`
	ResultJSON     datatypes.JSON `gorm:"column:result_json;type:jsonb"`
	CreatedBy      *string        `gorm:"column:created_by"`
	UpdatedBy      *string        `gorm:"column:updated_by"`
	CreatedAt      time.Time      `gorm:"column:created_at"`
	UpdatedAt      time.Time      `gorm:"column:updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (FakeProviderTask) TableName() string { return "fake_provider_tasks" }

type SmokeTestRun struct {
	ID          string         `gorm:"column:id;primaryKey"`
	SmokeRunID  string         `gorm:"column:smoke_run_id"`
	SuiteKey    string         `gorm:"column:suite_key"`
	Status      string         `gorm:"column:status"`
	StartedAt   time.Time      `gorm:"column:started_at"`
	FinishedAt  *time.Time     `gorm:"column:finished_at"`
	SummaryJSON datatypes.JSON `gorm:"column:summary_json;type:jsonb"`
	TraceID     *string        `gorm:"column:trace_id"`
	CreatedBy   *string        `gorm:"column:created_by"`
	UpdatedBy   *string        `gorm:"column:updated_by"`
	CreatedAt   time.Time      `gorm:"column:created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (SmokeTestRun) TableName() string { return "smoke_test_runs" }

type SmokeTestStep struct {
	ID           string         `gorm:"column:id;primaryKey"`
	SmokeRunID   string         `gorm:"column:smoke_run_id"`
	StepKey      string         `gorm:"column:step_key"`
	Status       string         `gorm:"column:status"`
	EvidenceJSON datatypes.JSON `gorm:"column:evidence_json;type:jsonb"`
	ErrorMessage *string        `gorm:"column:error_message"`
	CreatedBy    *string        `gorm:"column:created_by"`
	UpdatedBy    *string        `gorm:"column:updated_by"`
	CreatedAt    time.Time      `gorm:"column:created_at"`
	UpdatedAt    time.Time      `gorm:"column:updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (SmokeTestStep) TableName() string { return "smoke_test_steps" }
