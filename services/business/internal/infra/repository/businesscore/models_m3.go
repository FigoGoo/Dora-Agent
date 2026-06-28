package businesscore

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type ModelProvider struct {
	ID               string         `gorm:"column:id;primaryKey"`
	ProviderCode     string         `gorm:"column:provider_code"`
	DisplayName      string         `gorm:"column:display_name"`
	ProviderType     string         `gorm:"column:provider_type"`
	Status           string         `gorm:"column:status"`
	BaseURL          *string        `gorm:"column:base_url"`
	ConfigJSON       datatypes.JSON `gorm:"column:config_json;type:jsonb"`
	CreatedByAdminID *string        `gorm:"column:created_by_admin_id"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (ModelProvider) TableName() string { return "model_providers" }

type ModelProviderCredential struct {
	ID                     string         `gorm:"column:id;primaryKey"`
	ProviderID             string         `gorm:"column:provider_id"`
	CredentialName         string         `gorm:"column:credential_name"`
	SecretRef              string         `gorm:"column:secret_ref"`
	EncryptedPayloadDigest *string        `gorm:"column:encrypted_payload_digest"`
	Status                 string         `gorm:"column:status"`
	CreatedByAdminID       *string        `gorm:"column:created_by_admin_id"`
	CreatedAt              time.Time      `gorm:"column:created_at"`
	UpdatedAt              time.Time      `gorm:"column:updated_at"`
	DeletedAt              gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (ModelProviderCredential) TableName() string { return "model_provider_credentials" }

type Model struct {
	ID               string         `gorm:"column:id;primaryKey"`
	ProviderID       string         `gorm:"column:provider_id"`
	ModelCode        string         `gorm:"column:model_code"`
	DisplayName      string         `gorm:"column:display_name"`
	ResourceType     string         `gorm:"column:resource_type"`
	CapabilityTags   datatypes.JSON `gorm:"column:capability_tags;type:jsonb"`
	Status           string         `gorm:"column:status"`
	CredentialID     *string        `gorm:"column:credential_id"`
	RouteConfigJSON  datatypes.JSON `gorm:"column:route_config_json;type:jsonb"`
	CreatedByAdminID *string        `gorm:"column:created_by_admin_id"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (Model) TableName() string { return "models" }

type ModelPrice struct {
	ID                string         `gorm:"column:id;primaryKey"`
	PricingSnapshotID string         `gorm:"column:pricing_snapshot_id"`
	ModelID           string         `gorm:"column:model_id"`
	ResourceType      string         `gorm:"column:resource_type"`
	BillingUnit       string         `gorm:"column:billing_unit"`
	UnitPoints        float64        `gorm:"column:unit_points"`
	MinChargePoints   int64          `gorm:"column:min_charge_points"`
	Status            string         `gorm:"column:status"`
	EffectiveAt       time.Time      `gorm:"column:effective_at"`
	ExpiredAt         *time.Time     `gorm:"column:expired_at"`
	CreatedByAdminID  *string        `gorm:"column:created_by_admin_id"`
	CreatedAt         time.Time      `gorm:"column:created_at"`
	DeletedAt         gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (ModelPrice) TableName() string { return "model_prices" }

type DefaultModel struct {
	ID                string         `gorm:"column:id;primaryKey"`
	ResourceType      string         `gorm:"column:resource_type"`
	ModelID           string         `gorm:"column:model_id"`
	PricingSnapshotID string         `gorm:"column:pricing_snapshot_id"`
	Scope             string         `gorm:"column:scope"`
	Status            string         `gorm:"column:status"`
	CreatedByAdminID  *string        `gorm:"column:created_by_admin_id"`
	CreatedAt         time.Time      `gorm:"column:created_at"`
	UpdatedAt         time.Time      `gorm:"column:updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (DefaultModel) TableName() string { return "default_models" }

type ModelConnectivityTest struct {
	ID                 string    `gorm:"column:id;primaryKey"`
	ProviderID         string    `gorm:"column:provider_id"`
	ModelID            *string   `gorm:"column:model_id"`
	Status             string    `gorm:"column:status"`
	LatencyMS          *int      `gorm:"column:latency_ms"`
	ErrorCode          *string   `gorm:"column:error_code"`
	ErrorMessageDigest *string   `gorm:"column:error_message_digest"`
	TestedByAdminID    *string   `gorm:"column:tested_by_admin_id"`
	TraceID            *string   `gorm:"column:trace_id"`
	CreatedAt          time.Time `gorm:"column:created_at"`
}

func (ModelConnectivityTest) TableName() string { return "model_connectivity_tests" }

type ToolDefinition struct {
	ID               string         `gorm:"column:id;primaryKey"`
	ToolName         string         `gorm:"column:tool_name"`
	ToolType         string         `gorm:"column:tool_type"`
	DisplayName      string         `gorm:"column:display_name"`
	Description      *string        `gorm:"column:description"`
	Status           string         `gorm:"column:status"`
	Version          string         `gorm:"column:version"`
	InputSchemaJSON  datatypes.JSON `gorm:"column:input_schema_json;type:jsonb"`
	OutputSchemaJSON datatypes.JSON `gorm:"column:output_schema_json;type:jsonb"`
	CreatedByAdminID *string        `gorm:"column:created_by_admin_id"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (ToolDefinition) TableName() string { return "tool_definitions" }

type ToolPolicy struct {
	ID                   string         `gorm:"column:id;primaryKey"`
	ToolName             string         `gorm:"column:tool_name"`
	ToolType             string         `gorm:"column:tool_type"`
	PolicyScope          string         `gorm:"column:policy_scope"`
	Allowed              bool           `gorm:"column:allowed"`
	RiskLevel            string         `gorm:"column:risk_level"`
	RequiresConfirmation bool           `gorm:"column:requires_confirmation"`
	TimeoutMS            int32          `gorm:"column:timeout_ms"`
	RetryPolicyJSON      datatypes.JSON `gorm:"column:retry_policy_json;type:jsonb"`
	CancelPolicyJSON     datatypes.JSON `gorm:"column:cancel_policy_json;type:jsonb"`
	Status               string         `gorm:"column:status"`
	EffectiveAt          time.Time      `gorm:"column:effective_at"`
	ExpiredAt            *time.Time     `gorm:"column:expired_at"`
	ChangedByAdminID     *string        `gorm:"column:changed_by_admin_id"`
	CreatedAt            time.Time      `gorm:"column:created_at"`
	UpdatedAt            time.Time      `gorm:"column:updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (ToolPolicy) TableName() string { return "tool_policies" }

type ToolPricingPolicy struct {
	ID               string         `gorm:"column:id;primaryKey"`
	PricingPolicyID  string         `gorm:"column:pricing_policy_id"`
	ToolName         string         `gorm:"column:tool_name"`
	ToolType         string         `gorm:"column:tool_type"`
	ChargeMode       string         `gorm:"column:charge_mode"`
	BillingUnit      string         `gorm:"column:billing_unit"`
	UnitPoints       float64        `gorm:"column:unit_points"`
	FreeQuota        int            `gorm:"column:free_quota"`
	MinChargePoints  int64          `gorm:"column:min_charge_points"`
	Status           string         `gorm:"column:status"`
	EffectiveAt      time.Time      `gorm:"column:effective_at"`
	ExpiredAt        *time.Time     `gorm:"column:expired_at"`
	ChangedByAdminID *string        `gorm:"column:changed_by_admin_id"`
	MetadataJSON     datatypes.JSON `gorm:"column:metadata_json;type:jsonb"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (ToolPricingPolicy) TableName() string { return "tool_pricing_policies" }

type ToolWhitelistRule struct {
	ID               string         `gorm:"column:id;primaryKey"`
	ToolName         string         `gorm:"column:tool_name"`
	ToolType         string         `gorm:"column:tool_type"`
	ScopeType        string         `gorm:"column:scope_type"`
	ScopeID          string         `gorm:"column:scope_id"`
	Allowed          bool           `gorm:"column:allowed"`
	Reason           *string        `gorm:"column:reason"`
	Status           string         `gorm:"column:status"`
	ChangedByAdminID *string        `gorm:"column:changed_by_admin_id"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
	UpdatedAt        time.Time      `gorm:"column:updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (ToolWhitelistRule) TableName() string { return "tool_whitelist_rules" }

type ToolPolicyChangeRecord struct {
	ID               string         `gorm:"column:id;primaryKey"`
	ToolName         string         `gorm:"column:tool_name"`
	ToolType         string         `gorm:"column:tool_type"`
	ChangeType       string         `gorm:"column:change_type"`
	BeforeJSON       datatypes.JSON `gorm:"column:before_json;type:jsonb"`
	AfterJSON        datatypes.JSON `gorm:"column:after_json;type:jsonb"`
	ChangedByAdminID *string        `gorm:"column:changed_by_admin_id"`
	TraceID          *string        `gorm:"column:trace_id"`
	CreatedAt        time.Time      `gorm:"column:created_at"`
}

func (ToolPolicyChangeRecord) TableName() string { return "tool_policy_change_records" }

type Skill struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	SkillKey           string         `gorm:"column:skill_key"`
	SkillName          string         `gorm:"column:skill_name"`
	SkillScope         string         `gorm:"column:skill_scope"`
	OwnerUserID        *string        `gorm:"column:owner_user_id"`
	EnterpriseID       *string        `gorm:"column:enterprise_id"`
	Status             string         `gorm:"column:status"`
	PublishedVersionID *string        `gorm:"column:published_version_id"`
	RouteHintsJSON     datatypes.JSON `gorm:"column:route_hints_json;type:jsonb"`
	CreatedByUserID    *string        `gorm:"column:created_by_user_id"`
	CreatedByAdminID   *string        `gorm:"column:created_by_admin_id"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (Skill) TableName() string { return "skills" }

type SkillVersion struct {
	ID                      string         `gorm:"column:id;primaryKey"`
	SkillID                 string         `gorm:"column:skill_id"`
	Version                 string         `gorm:"column:version"`
	Status                  string         `gorm:"column:status"`
	SkillSpecJSON           datatypes.JSON `gorm:"column:skill_spec_json;type:jsonb"`
	InputSchemaJSON         datatypes.JSON `gorm:"column:input_schema_json;type:jsonb"`
	OutputSchemaJSON        datatypes.JSON `gorm:"column:output_schema_json;type:jsonb"`
	MemoryPolicyJSON        datatypes.JSON `gorm:"column:memory_policy_json;type:jsonb"`
	ConfirmationPolicyJSON  datatypes.JSON `gorm:"column:confirmation_policy_json;type:jsonb"`
	Changelog               *string        `gorm:"column:changelog"`
	SubmittedByUserID       *string        `gorm:"column:submitted_by_user_id"`
	ReviewedByAdminID       *string        `gorm:"column:reviewed_by_admin_id"`
	SubmittedAt             *time.Time     `gorm:"column:submitted_at"`
	ReviewedAt              *time.Time     `gorm:"column:reviewed_at"`
	PublishedAt             *time.Time     `gorm:"column:published_at"`
	RolledBackFromVersionID *string        `gorm:"column:rolled_back_from_version_id"`
	CreatedAt               time.Time      `gorm:"column:created_at"`
	UpdatedAt               time.Time      `gorm:"column:updated_at"`
	DeletedAt               gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (SkillVersion) TableName() string { return "skill_versions" }

type SkillToolBinding struct {
	ID        string         `gorm:"column:id;primaryKey"`
	SkillID   string         `gorm:"column:skill_id"`
	VersionID string         `gorm:"column:version_id"`
	ToolName  string         `gorm:"column:tool_name"`
	ToolType  string         `gorm:"column:tool_type"`
	Required  bool           `gorm:"column:required"`
	CreatedAt time.Time      `gorm:"column:created_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (SkillToolBinding) TableName() string { return "skill_tool_bindings" }

type SkillOutputElementSchema struct {
	ID           string         `gorm:"column:id;primaryKey"`
	SkillID      string         `gorm:"column:skill_id"`
	VersionID    string         `gorm:"column:version_id"`
	ElementType  string         `gorm:"column:element_type"`
	ElementName  string         `gorm:"column:element_name"`
	SchemaJSON   datatypes.JSON `gorm:"column:schema_json;type:jsonb"`
	Required     bool           `gorm:"column:required"`
	DisplayOrder int32          `gorm:"column:display_order"`
	DisplaySlot  string         `gorm:"column:display_slot"`
	UseDraft     bool           `gorm:"column:use_draft"`
	UseFinal     bool           `gorm:"column:use_final"`
	Editable     bool           `gorm:"column:editable"`
	Referable    bool           `gorm:"column:referable"`
	CreatedAt    time.Time      `gorm:"column:created_at"`
	DeletedAt    gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (SkillOutputElementSchema) TableName() string { return "skill_output_element_schemas" }

type SkillTestCase struct {
	ID                   string         `gorm:"column:id;primaryKey"`
	SkillID              string         `gorm:"column:skill_id"`
	VersionID            string         `gorm:"column:version_id"`
	CaseName             string         `gorm:"column:case_name"`
	TestInputJSON        datatypes.JSON `gorm:"column:test_input_json;type:jsonb"`
	ExpectedElementsJSON datatypes.JSON `gorm:"column:expected_elements_json;type:jsonb"`
	Status               string         `gorm:"column:status"`
	CreatedByUserID      *string        `gorm:"column:created_by_user_id"`
	CreatedAt            time.Time      `gorm:"column:created_at"`
	UpdatedAt            time.Time      `gorm:"column:updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (SkillTestCase) TableName() string { return "skill_test_cases" }

type SkillTestRun struct {
	ID                 string         `gorm:"column:id;primaryKey"`
	SkillID            string         `gorm:"column:skill_id"`
	VersionID          string         `gorm:"column:version_id"`
	TestCaseID         *string        `gorm:"column:test_case_id"`
	Status             string         `gorm:"column:status"`
	ExecutionMode      string         `gorm:"column:execution_mode"`
	InputJSON          datatypes.JSON `gorm:"column:input_json;type:jsonb"`
	ActualElementsJSON datatypes.JSON `gorm:"column:actual_elements_json;type:jsonb"`
	SafetyEvidenceJSON datatypes.JSON `gorm:"column:safety_evidence_json;type:jsonb"`
	ErrorCode          *string        `gorm:"column:error_code"`
	ErrorSummary       *string        `gorm:"column:error_summary"`
	AgentTraceID       *string        `gorm:"column:agent_trace_id"`
	IdempotencyKey     *string        `gorm:"column:idempotency_key"`
	RequestHash        *string        `gorm:"column:request_hash"`
	StartedAt          *time.Time     `gorm:"column:started_at"`
	FinishedAt         *time.Time     `gorm:"column:finished_at"`
	CreatedByUserID    *string        `gorm:"column:created_by_user_id"`
	CreatedAt          time.Time      `gorm:"column:created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (SkillTestRun) TableName() string { return "skill_test_runs" }

type SkillReviewRecord struct {
	ID                string    `gorm:"column:id;primaryKey"`
	SkillID           string    `gorm:"column:skill_id"`
	VersionID         string    `gorm:"column:version_id"`
	ReviewAction      string    `gorm:"column:review_action"`
	ReviewStatus      string    `gorm:"column:review_status"`
	ReviewComment     *string   `gorm:"column:review_comment"`
	ReviewedByAdminID string    `gorm:"column:reviewed_by_admin_id"`
	TraceID           *string   `gorm:"column:trace_id"`
	CreatedAt         time.Time `gorm:"column:created_at"`
}

func (SkillReviewRecord) TableName() string { return "skill_review_records" }

type AssetElementType struct {
	ID            string         `gorm:"column:id;primaryKey"`
	ElementType   string         `gorm:"column:element_type"`
	DisplayName   string         `gorm:"column:display_name"`
	SchemaVersion string         `gorm:"column:schema_version"`
	SchemaJSON    datatypes.JSON `gorm:"column:schema_json;type:jsonb"`
	Status        string         `gorm:"column:status"`
	OperatorID    *string        `gorm:"column:operator_id"`
	CreatedAt     time.Time      `gorm:"column:created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"column:deleted_at"`
}

func (AssetElementType) TableName() string { return "asset_element_types" }
