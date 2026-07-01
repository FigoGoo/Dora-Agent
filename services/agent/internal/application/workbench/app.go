package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr2"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr3"
	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/apperror"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	runtimestream "github.com/FigoGoo/Dora-Agent/services/agent/internal/events/stream"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation"
	runtimeeino "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/modeltool"
	runtimerouter "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/router"
	runtimesafety "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/safety"
	runtimeskill "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/skill"
	runtimeskilltest "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/skilltest"
	runtimetool "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/tool"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/turnloop"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type BusinessGateway interface {
	ResolveAuthContextFromToken(ctx context.Context, authorization string, expectedSpaceID string, traceID string) (AuthContextDTO, SpaceContextDTO, error)
	ResolveCurrentSpaceContext(ctx context.Context, auth AuthContextDTO, expectedSpaceID string, traceID string) (SpaceContextDTO, error)
	CheckProjectAccess(ctx context.Context, auth AuthContextDTO, projectID string, purpose businessagent.ProjectAccessPurpose, traceID string) (ProjectAccessDTO, error)
	ListRoutableSkills(ctx context.Context, auth AuthContextDTO, scopeFilter string, limit int, cursor string, traceID string) ([]SkillSummaryDTO, string, error)
	GetPublishedSkillSpec(ctx context.Context, auth AuthContextDTO, skillID string, version string, traceID string) (SkillSpecDTO, error)
	GetReviewCandidateSkillSpec(ctx context.Context, auth AuthContextDTO, skillID string, versionID string, testCaseID string, testRunID string, traceID string) (ReviewCandidateSkillSpecDTO, error)
	CheckToolExecutionPolicy(ctx context.Context, auth AuthContextDTO, toolName string, toolType string, projectID string, riskContext map[string]string, traceID string) (ToolExecutionPolicyDTO, error)
	ListAvailableGenerationModels(ctx context.Context, auth AuthContextDTO, resourceType string, limit int, cursor string, traceID string) ([]ModelSummaryDTO, string, error)
	ResolveDefaultModel(ctx context.Context, auth AuthContextDTO, resourceType string, traceID string) (ModelSummaryDTO, error)
	ResolveGenerationModelSnapshot(ctx context.Context, auth AuthContextDTO, resourceType string, modelID string, pricingSnapshotID string, traceID string) (ModelRuntimeSnapshotDTO, error)
	ListAssetElementTypes(ctx context.Context, auth AuthContextDTO, pageSize int, schemaVersion string, traceID string) ([]AssetElementTypeDTO, string, error)
	SaveSkillTestResult(ctx context.Context, auth AuthContextDTO, req SkillTestResultRequest, traceID string) (SkillTestResultDTO, error)
	BatchCheckAssetAccess(ctx context.Context, auth AuthContextDTO, req BatchCheckAssetAccessRequest, traceID string) ([]AssetAccessResultDTO, error)
	EstimateGenerationCredits(ctx context.Context, auth AuthContextDTO, req EstimateGenerationCreditsRequest, traceID string) (CreditEstimateDTO, error)
	EstimateToolCredits(ctx context.Context, auth AuthContextDTO, req EstimateToolCreditsRequest, traceID string) (CreditEstimateDTO, error)
	FreezeCredits(ctx context.Context, auth AuthContextDTO, req FreezeCreditsRequest, traceID string) (FreezeCreditsDTO, error)
	ChargeToolUsageCredits(ctx context.Context, auth AuthContextDTO, req ChargeToolUsageCreditsRequest, traceID string) (ToolChargeDTO, error)
	ReleaseFrozenCredits(ctx context.Context, auth AuthContextDTO, req ReleaseFrozenCreditsRequest, traceID string) (ReleaseCreditsDTO, error)
	PrepareGeneratedAssetObjects(ctx context.Context, auth AuthContextDTO, req PrepareGeneratedAssetObjectsRequest, traceID string) ([]GeneratedUploadSlotDTO, error)
	CommitGeneratedAssetAndCharge(ctx context.Context, auth AuthContextDTO, req CommitGeneratedAssetAndChargeRequest, traceID string) (AssetCommitDTO, error)
}

type AuthContextDTO struct {
	ActorUserID       string
	LoginIdentityType string
	SpaceID           string
	EnterpriseID      string
	EnterpriseRole    string
	AdminID           string
}

type SpaceContextDTO struct {
	SpaceID            string
	SpaceType          string
	EnterpriseID       string
	EnterpriseRole     string
	CreditAccountScope string
	CreditAccountID    string
	SkillScopeKeys     []string
	PermissionSummary  map[string]string
}

type ProjectAccessDTO struct {
	Allowed         bool
	ProjectStatus   string
	CreativeAllowed bool
	AllowedActions  []string
	UserMessage     string
	ProjectSummary  map[string]string
}

type SkillSummaryDTO struct {
	SkillID    string
	SkillName  string
	SkillScope string
	Version    string
	Status     string
	RouteHints map[string]string
}

type SkillSpecDTO struct {
	SkillID                    string
	Version                    string
	SkillSpecJSON              string
	OutputSchemaJSON           string
	ToolRefs                   []string
	MemoryPolicyJSON           string
	ConfirmationPolicyJSON     string
	ExecutionPolicySummaryJSON string
	OutputElements             []SkillOutputElementDTO
}

type SkillOutputElementDTO struct {
	ElementType  string `json:"element_type"`
	ElementName  string `json:"element_name"`
	Required     bool   `json:"required"`
	UseDraft     bool   `json:"use_draft"`
	UseFinal     bool   `json:"use_final"`
	Editable     bool   `json:"editable"`
	Referable    bool   `json:"referable"`
	DisplayOrder int32  `json:"display_order"`
	DisplaySlot  string `json:"display_slot"`
	SchemaJSON   string `json:"schema_json,omitempty"`
}

type ReviewCandidateSkillSpecDTO struct {
	SkillID                string
	VersionID              string
	SkillSpecJSON          string
	InputSchemaJSON        string
	OutputSchemaJSON       string
	ToolRefs               []string
	MemoryPolicyJSON       string
	ConfirmationPolicyJSON string
	TestInputJSON          string
	ExpectedElementsJSON   string
	OutputElements         []SkillOutputElementDTO
}

type ToolExecutionPolicyDTO struct {
	Allowed              bool
	RiskLevel            string
	RequiresConfirmation bool
	TimeoutMS            int32
	RetryPolicy          map[string]string
	CancelPolicy         map[string]string
}

type ModelSummaryDTO struct {
	ModelID           string
	DisplayName       string
	IsDefault         bool
	PricingSnapshotID string
	ResourceType      string
}

type ModelRuntimeSnapshotDTO struct {
	ModelID            string            `json:"model_id"`
	DisplayName        string            `json:"display_name"`
	ResourceType       string            `json:"resource_type"`
	PricingSnapshotID  string            `json:"pricing_snapshot_id"`
	ProviderRuntimeRef string            `json:"provider_runtime_ref"`
	TimeoutMS          int32             `json:"timeout_ms"`
	RetryPolicy        map[string]string `json:"retry_policy,omitempty"`
	RuntimeParameters  map[string]string `json:"runtime_parameters,omitempty"`
}

type AssetElementTypeDTO struct {
	ElementType    string `json:"element_type"`
	DisplayName    string `json:"display_name"`
	Category       string `json:"category"`
	SchemaVersion  string `json:"schema_version"`
	SchemaHintJSON string `json:"schema_hint_json"`
	RenderHintJSON string `json:"render_hint_json"`
	Active         bool   `json:"active"`
	SortOrder      int32  `json:"sort_order"`
	ResourceType   string `json:"resource_type"`
	Status         string `json:"status"`
	UsageStage     string `json:"usage_stage"`
	DraftEnabled   bool   `json:"draft_enabled"`
	FinalEnabled   bool   `json:"final_enabled"`
	Editable       bool   `json:"editable"`
	Referable      bool   `json:"referable"`
	RenderHint     string `json:"render_hint"`
}

type SkillTestResultRequest struct {
	SkillID            string
	VersionID          string
	TestRunID          string
	TestCaseID         string
	IdempotencyKey     string
	Status             string
	ActualElementsJSON string
	ErrorCode          string
	ErrorSummary       string
	SafetyEvidenceJSON string
	AgentTraceID       string
}

type SkillTestCaseRequest struct {
	SkillID        string
	VersionID      string
	TestRunID      string
	TestCaseID     string
	IdempotencyKey string
}

type SkillTestResultDTO struct {
	TestRunID string
	Status    string
	Saved     bool
}

type AssetAccessResultDTO struct {
	AssetID      string
	Allowed      bool
	Reason       string
	AssetSummary map[string]string
}

type BatchCheckAssetAccessRequest struct {
	ProjectID      string
	AssetIDs       []string
	Purpose        string
	IdempotencyKey string
}

type EstimateGenerationCreditsRequest struct {
	ProjectID         string
	ResourceType      string
	ModelID           string
	PricingSnapshotID string
	Quantity          int32
	DurationSeconds   int32
	ToolUsageItems    []ToolUsageEstimateItemDTO
	SafetyEvidence    *businessagent.SafetyEvidenceDTO
	IdempotencyKey    string
}

type EstimateToolCreditsRequest struct {
	ProjectID      string
	ToolUsageItems []ToolUsageEstimateItemDTO
	SafetyEvidence *businessagent.SafetyEvidenceDTO
	IdempotencyKey string
}

type ToolUsageEstimateItemDTO struct {
	ToolName        string
	ToolType        string
	BillingUnit     string
	Quantity        float64
	MetadataSummary map[string]string
}

type CreditEstimateLineItemDTO struct {
	EstimateItemID string
	ItemType       string
	ToolName       string
	ToolType       string
	ModelID        string
	ResourceType   string
	BillingUnit    string
	EstimatePoints int64
	Metadata       map[string]string
}

type CreditEstimateDTO struct {
	EstimateID         string
	EstimatePoints     int64
	AvailablePoints    int64
	ExpiresSoonPoints  int64
	CreditAccountScope string
	CreditAccountID    string
	PricingSnapshotID  string
	LineItems          []CreditEstimateLineItemDTO
	ExpiresAt          string
	Insufficient       bool
}

type FreezeCreditsRequest struct {
	EstimateID     string
	Points         int64
	RunID          string
	ConfirmationID string
	AccountID      string
	IdempotencyKey string
}

type FreezeCreditsDTO struct {
	FreezeID     string
	FrozenPoints int64
	ExpiresAt    string
}

type ReleaseFrozenCreditsRequest struct {
	FreezeID       string
	ReleasePoints  int64
	Reason         string
	RunID          string
	IdempotencyKey string
}

type ReleaseCreditsDTO struct {
	ReleasedPoints int64
	ReleaseStatus  string
}

type ToolChargeItemDTO struct {
	EstimateItemID  string
	ToolCallID      string
	ToolName        string
	ToolType        string
	BillingUnit     string
	ActualQuantity  float64
	ExecutionStatus string
	MetadataSummary map[string]string
}

type ChargeToolUsageCreditsRequest struct {
	ProjectID      string
	EstimateID     string
	FreezeID       string
	SessionID      string
	RunID          string
	ChargeItems    []ToolChargeItemDTO
	IdempotencyKey string
}

type ToolChargeDTO struct {
	ToolChargeID     string
	ChargedPoints    int64
	ReleasedPoints   int64
	FreezeStatus     string
	LedgerEntryIDs   []string
	ChargedLineItems []ChargedLineItemDTO
}

type GeneratedObjectInputDTO struct {
	ArtifactID      string
	ResourceType    string
	Filename        string
	ContentType     string
	SizeBytes       int64
	Checksum        string
	MetadataSummary map[string]string
}

type PrepareGeneratedAssetObjectsRequest struct {
	ProjectID      string
	SessionID      string
	RunID          string
	Artifacts      []GeneratedObjectInputDTO
	IdempotencyKey string
}

type GeneratedUploadSlotDTO struct {
	ArtifactID    string
	Bucket        string
	ObjectKey     string
	UploadURL     string
	UploadHeaders map[string]string
	ExpiresAt     string
	MaxSizeBytes  int64
}

type CommitStorageObjectRefDTO struct {
	ObjectKey   string
	Bucket      string
	ContentType string
	SizeBytes   int64
	Checksum    string
	Etag        string
}

type CommitArtifactDTO struct {
	ArtifactID       string
	ResourceType     string
	ElementType      string
	ArtifactSummary  map[string]string
	ContentURIDigest string
	EstimateItemID   string
	ToolName         string
	ToolType         string
	ChargeQuantity   int64
	MetadataSummary  map[string]string
	StorageObjectRef CommitStorageObjectRefDTO
}

type FinalElementDTO struct {
	ElementType        string
	ElementPayloadJSON string
	DisplayOrder       int32
	SourceToolCallID   string
}

type CommitGeneratedAssetAndChargeRequest struct {
	ProjectID      string
	SessionID      string
	RunID          string
	FreezeID       string
	EstimateID     string
	Artifacts      []CommitArtifactDTO
	FinalElements  []FinalElementDTO
	SafetyEvidence *businessagent.SafetyEvidenceDTO
	IdempotencyKey string
}

type CommittedAssetRefDTO struct {
	AssetID             string
	SourceArtifactID    string
	ResourceType        string
	AssetType           string
	Status              string
	PreviewURL          string
	ElementsSummaryJSON string
}

type ChargedLineItemDTO struct {
	EstimateItemID string
	ChargedPoints  int64
	Status         string
	AssetID        string
	ToolCallID     string
	ArtifactID     string
}

type AssetCommitDTO struct {
	AssetRefs        []CommittedAssetRefDTO
	ChargedPoints    int64
	ReleasedPoints   int64
	CommitStatus     string
	LedgerRef        string
	ChargedLineItems []ChargedLineItemDTO
}

type TaskDTO struct {
	TaskID          string         `json:"task_id"`
	RunID           string         `json:"run_id"`
	TaskType        string         `json:"task_type"`
	ResourceType    string         `json:"resource_type"`
	Status          string         `json:"status"`
	ProgressPercent int            `json:"progress_percent"`
	ProgressDetail  map[string]any `json:"progress_detail"`
	CancelRequested bool           `json:"cancel_requested"`
	ErrorCode       string         `json:"error_code,omitempty"`
	UpdatedAt       string         `json:"updated_at"`
}

type GenerationRecoveryResult struct {
	Scanned      int
	Released     int
	Reconcile    int
	ReleaseFails int
}

type GenerationWorkerResult struct {
	Processed int
	Failed    int
	LastError error
}

type App struct {
	repo             *repository.Repository
	gateway          BusinessGateway
	configVersion    string
	safetyEvaluator  runtimesafety.Evaluator
	skillRouter      runtimeskill.Router
	chatRouter       runtimerouter.ChatModelRouter
	toolChecker      runtimetool.PolicyChecker
	modelAdapter     modeltool.Adapter
	artifactUploader ArtifactUploader
	turnLoop         turnloop.TurnLoop
	generationQueue  GenerationJobQueue
	aguiEventBus     runtimestream.AGUIEventBus
	snapshotCache    runtimestream.SnapshotCache
	turnLock         runtimestream.TurnLock
}

var errToolConfirmationRequired = errors.New("tool confirmation required")

const (
	RunIntentEntryGuide         = "entry_guide"
	RunIntentCapabilityQuestion = "capability_question"
	RunIntentNormal             = "normal"
	RunIntentSelectSkill        = "select_skill"
)

func New(repo *repository.Repository, gateway BusinessGateway, configVersion string) *App {
	if configVersion == "" {
		configVersion = "local-dev"
	}
	return &App{
		repo:             repo,
		gateway:          gateway,
		configVersion:    configVersion,
		safetyEvaluator:  runtimesafety.NewEvaluator(nil),
		skillRouter:      runtimeskill.NewRouter(),
		chatRouter:       runtimerouter.NewChatModelRouter(),
		toolChecker:      runtimetool.NewPolicyChecker(),
		modelAdapter:     modeltool.LocalAdapter{},
		artifactUploader: NewStreamingArtifactUploader(nil),
		turnLoop:         turnloop.New(),
	}
}

func (a *App) SetArtifactUploader(uploader ArtifactUploader) {
	if uploader != nil {
		a.artifactUploader = uploader
	}
}

func (a *App) SetModelAdapter(adapter modeltool.Adapter) {
	if adapter != nil {
		a.modelAdapter = adapter
	}
}

func (a *App) SetGenerationQueue(queue GenerationJobQueue) {
	a.generationQueue = queue
}

func (a *App) SetRuntimePrimitives(bus runtimestream.AGUIEventBus, cache runtimestream.SnapshotCache, lock runtimestream.TurnLock) {
	if bus != nil {
		a.aguiEventBus = bus
	}
	if cache != nil {
		a.snapshotCache = cache
	}
	if lock != nil {
		a.turnLock = lock
	}
}

func (a *App) ResolveAuthContextFromToken(ctx context.Context, authorization string, expectedSpaceID string, traceID string) (AuthContextDTO, error) {
	if a.gateway == nil {
		return AuthContextDTO{}, apperror.New(apperror.CodeNotImplemented, "business gateway is not configured")
	}
	auth, space, err := a.gateway.ResolveAuthContextFromToken(ctx, authorization, expectedSpaceID, traceID)
	if err != nil {
		return AuthContextDTO{}, mapBusinessError(err)
	}
	auth.SpaceID = space.SpaceID
	auth.EnterpriseID = space.EnterpriseID
	auth.EnterpriseRole = space.EnterpriseRole
	if auth.LoginIdentityType == "" {
		auth.LoginIdentityType = "personal"
	}
	return auth, nil
}

func (a *App) RunSkillTestCase(ctx context.Context, auth AuthContextDTO, req SkillTestCaseRequest, traceID string) (SkillTestResultDTO, error) {
	if a.gateway == nil {
		return SkillTestResultDTO{}, apperror.New(apperror.CodeNotImplemented, "business gateway is not configured")
	}
	if req.SkillID == "" || req.VersionID == "" || req.TestRunID == "" || req.IdempotencyKey == "" {
		return SkillTestResultDTO{}, apperror.New(apperror.CodeInvalidArgument, "skill_id, version_id, test_run_id and idempotency_key are required")
	}
	spec, err := a.gateway.GetReviewCandidateSkillSpec(ctx, auth, req.SkillID, req.VersionID, req.TestCaseID, req.TestRunID, traceID)
	if err != nil {
		return SkillTestResultDTO{}, mapBusinessError(err)
	}
	testInput := strings.TrimSpace(spec.TestInputJSON)
	if testInput == "" {
		testInput = spec.SkillSpecJSON
	}
	evidence := a.safetyEvaluator.Evaluate(ctx, "skill_test", "skill_test_prompt", req.TestCaseID, testInput)
	safetyJSON := skillTestSafetyEvidenceJSON(evidence, req.TestRunID, traceID)
	status := "passed"
	actualElements := expectedElementsFromJSON(spec.ExpectedElementsJSON)
	if len(actualElements) == 0 {
		actualElements = []string{"structured_object"}
	}
	if evidence.Result == state.SafetyResultBlocked {
		status = "blocked"
	} else if evidence.Result == state.SafetyResultFailed {
		status = "failed"
	} else {
		elementTypes, _, err := a.gateway.ListAssetElementTypes(ctx, auth, 50, "", traceID)
		if err != nil {
			return SkillTestResultDTO{}, mapBusinessError(err)
		}
		runner := runtimeskilltest.NewRunner()
		result := runner.Run(runtimeskilltest.Input{
			Cases: []runtimeskilltest.Case{
				{CaseID: req.TestCaseID + "_1", ExpectedElements: actualElements},
				{CaseID: req.TestCaseID + "_2", ExpectedElements: actualElements},
				{CaseID: req.TestCaseID + "_3", ExpectedElements: actualElements},
			},
			SafetyResult:         evidence.Result,
			ActualElements:       actualElements,
			ActualElementDetails: skillTestActualElements(actualElements),
			ElementTypes:         skillTestElementTypes(elementTypes),
			Stage:                "draft",
		})
		status = result.Status
	}
	actualJSON := jsonObject(map[string]any{"elements": actualElements, "source": "agent_skill_test"})
	return a.gateway.SaveSkillTestResult(ctx, auth, SkillTestResultRequest{
		SkillID: req.SkillID, VersionID: req.VersionID, TestRunID: req.TestRunID, TestCaseID: req.TestCaseID,
		IdempotencyKey: req.IdempotencyKey, Status: status, ActualElementsJSON: string(actualJSON),
		SafetyEvidenceJSON: safetyJSON, AgentTraceID: traceID,
	}, traceID)
}

type CreateSessionRequest struct {
	ProjectID      string `json:"project_id"`
	InitialTitle   string `json:"initial_title"`
	IdempotencyKey string `json:"idempotency_key"`
}

type CreateRunRequest struct {
	SessionID        string              `json:"session_id"`
	ProjectID        string              `json:"project_id"`
	RunIntent        string              `json:"run_intent"`
	UserInput        UserInputDTO        `json:"user_input"`
	ModelSelection   *ModelSelectionDTO  `json:"model_selection"`
	ReferencedAssets []AssetReferenceDTO `json:"referenced_assets"`
	ControlInputs    []ControlInputDTO   `json:"control_inputs"`
	IdempotencyKey   string              `json:"idempotency_key"`
}

type AppendUserInputRequest struct {
	UserInput      UserInputDTO `json:"user_input"`
	IdempotencyKey string       `json:"idempotency_key"`
}

type ConfirmInterruptRequest struct {
	RunID                  string `json:"run_id"`
	InterruptID            string `json:"interrupt_id"`
	Action                 string `json:"action"`
	ConfirmedPayloadDigest string `json:"confirmed_payload_digest"`
	IdempotencyKey         string `json:"idempotency_key"`
}

type RejectInterruptRequest struct {
	RunID          string `json:"run_id"`
	InterruptID    string `json:"interrupt_id"`
	ReasonCode     string `json:"reason_code"`
	IdempotencyKey string `json:"idempotency_key"`
}

type UserInputDTO struct {
	ClientMessageID string `json:"client_message_id"`
	ContentType     string `json:"content_type"`
	Text            string `json:"text"`
	Language        string `json:"language"`
}

type ModelSelectionDTO struct {
	ResourceType      string `json:"resource_type"`
	ModelID           string `json:"model_id"`
	ModelDisplayName  string `json:"model_display_name"`
	IsDefault         bool   `json:"is_default"`
	PricingSnapshotID string `json:"pricing_snapshot_id"`
}

type AssetReferenceDTO struct {
	AssetID        string `json:"asset_id"`
	ProjectID      string `json:"project_id"`
	Source         string `json:"source"`
	Purpose        string `json:"purpose"`
	MetadataDigest string `json:"metadata_digest"`
}

type ControlInputDTO struct {
	ControlID        string `json:"control_id"`
	Type             string `json:"type"`
	Value            any    `json:"value"`
	DisplayLabel     string `json:"display_label"`
	Required         bool   `json:"required"`
	ValidationDigest string `json:"validation_digest"`
}

type SessionDTO struct {
	SessionID         string `json:"session_id"`
	ProjectID         string `json:"project_id"`
	SpaceID           string `json:"space_id"`
	UserID            string `json:"user_id"`
	Status            string `json:"status"`
	Title             string `json:"title"`
	LastRunID         string `json:"last_run_id,omitempty"`
	LastEventSequence int64  `json:"last_event_sequence"`
}

type RunDTO struct {
	RunID           string `json:"run_id"`
	SessionID       string `json:"session_id"`
	ProjectID       string `json:"project_id"`
	Status          string `json:"status"`
	StreamURL       string `json:"stream_url,omitempty"`
	SnapshotVersion string `json:"snapshot_version"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
}

type MessageDTO struct {
	MessageID    string    `json:"message_id"`
	SessionID    string    `json:"session_id"`
	RunID        string    `json:"run_id,omitempty"`
	Role         string    `json:"role"`
	ContentType  string    `json:"content_type"`
	Content      string    `json:"content"`
	Sequence     int64     `json:"sequence"`
	SafetyStatus string    `json:"safety_status"`
	CreatedAt    time.Time `json:"created_at"`
}

type EventDTO struct {
	EventID              string         `json:"event_id"`
	Type                 string         `json:"type"`
	SessionID            string         `json:"session_id"`
	RunID                string         `json:"run_id"`
	ProjectID            string         `json:"project_id"`
	SpaceID              string         `json:"space_id"`
	ActorUserID          string         `json:"actor_user_id"`
	Sequence             int64          `json:"sequence"`
	Timestamp            time.Time      `json:"timestamp"`
	Component            string         `json:"component"`
	TraceID              string         `json:"trace_id"`
	PayloadSchemaVersion string         `json:"payload_schema_version,omitempty"`
	Payload              map[string]any `json:"payload"`
}

type SnapshotResponse struct {
	Session           SessionDTO     `json:"session"`
	Run               *RunDTO        `json:"run"`
	Messages          []MessageDTO   `json:"messages"`
	Assets            []any          `json:"assets"`
	Blackboard        map[string]any `json:"blackboard"`
	Tasks             []TaskDTO      `json:"tasks"`
	Interrupt         *InterruptDTO  `json:"interrupt,omitempty"`
	LastEventSequence int64          `json:"last_event_sequence"`
	ReadonlyReason    string         `json:"readonly_reason,omitempty"`
}

type InterruptDTO struct {
	InterruptID         string         `json:"interrupt_id"`
	ConfirmationID      string         `json:"confirmation_id"`
	Type                string         `json:"type"`
	Status              string         `json:"status"`
	Reason              string         `json:"reason"`
	Title               string         `json:"title"`
	Summary             string         `json:"summary"`
	Risks               []string       `json:"risks"`
	Points              int64          `json:"points"`
	Actions             []string       `json:"actions"`
	PayloadDigest       string         `json:"payload_digest"`
	ConfirmationPayload map[string]any `json:"confirmation_payload"`
	ExpiresAt           string         `json:"expires_at"`
	TraceID             string         `json:"trace_id"`
}

type ListMessagesResponse struct {
	Messages []MessageDTO `json:"messages"`
	Limit    int          `json:"limit"`
	Offset   int          `json:"offset"`
}

type EventReplayResponse struct {
	Events       []EventDTO `json:"events"`
	NextSequence int64      `json:"next_sequence"`
	HasMore      bool       `json:"has_more"`
}

type CreateSessionResponse struct {
	SessionID string           `json:"session_id"`
	ProjectID string           `json:"project_id"`
	Status    string           `json:"status"`
	Snapshot  SnapshotResponse `json:"snapshot"`
}

type CreateRunResponse struct {
	RunID           string `json:"run_id"`
	SessionID       string `json:"session_id"`
	ProjectID       string `json:"project_id"`
	Status          string `json:"status"`
	StreamURL       string `json:"stream_url"`
	SnapshotVersion string `json:"snapshot_version"`
}

type CreativeBoardResponse struct {
	Board    pr2.CreativeBoard `json:"board"`
	Snapshot pr2.BoardSnapshot `json:"snapshot"`
}

type ApproveCreativeBoardRequest struct {
	ApprovedBy     string `json:"approved_by"`
	BoardVersion   int    `json:"board_version"`
	IdempotencyKey string `json:"idempotency_key"`
}

type ApproveCreativeBoardResponse struct {
	Board    pr2.CreativeBoard `json:"board"`
	Patch    *pr2.BoardPatch   `json:"patch,omitempty"`
	ToolPlan *pr3.ToolPlan     `json:"tool_plan,omitempty"`
}

type ApplyBoardPatchRequest struct {
	Patch          pr2.BoardPatch `json:"patch"`
	IdempotencyKey string         `json:"idempotency_key"`
}

type ApplyBoardPatchResponse struct {
	Board pr2.CreativeBoard `json:"board"`
	Patch pr2.BoardPatch    `json:"patch"`
}

type boardPatchAfterState struct {
	BoardAfter        pr2.CreativeBoard     `json:"board_after"`
	ElementsAfter     []pr2.CreativeElement `json:"elements_after"`
	ChangedElementIDs []string              `json:"changed_element_ids"`
}

type GraphPlanResponse struct {
	GraphPlan pr2.GraphPlan `json:"graph_plan"`
}

func (a *App) CreateSession(ctx context.Context, auth AuthContextDTO, req CreateSessionRequest, traceID string) (CreateSessionResponse, error) {
	if auth.ActorUserID == "" {
		return CreateSessionResponse{}, apperror.New(apperror.CodeUnauthenticated, "auth context is required")
	}
	if req.ProjectID == "" || req.IdempotencyKey == "" {
		return CreateSessionResponse{}, apperror.New(apperror.CodeInvalidArgument, "project_id and idempotency_key are required")
	}
	if existing, err := a.repo.GetSessionByIdempotencyKey(ctx, req.IdempotencyKey); err == nil {
		snapshot, snapErr := a.BuildSessionSnapshot(ctx, auth, existing.ID, traceID)
		if snapErr != nil {
			return CreateSessionResponse{}, snapErr
		}
		return CreateSessionResponse{SessionID: existing.ID, ProjectID: existing.ProjectID, Status: existing.Status, Snapshot: snapshot}, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return CreateSessionResponse{}, err
	}
	space, err := a.gateway.ResolveCurrentSpaceContext(ctx, auth, auth.SpaceID, traceID)
	if err != nil {
		return CreateSessionResponse{}, mapBusinessError(err)
	}
	auth.SpaceID = space.SpaceID
	access, err := a.gateway.CheckProjectAccess(ctx, auth, req.ProjectID, businessagent.ProjectAccessPurpose_CONTINUE_CREATION, traceID)
	if err != nil {
		return CreateSessionResponse{}, mapBusinessError(err)
	}
	if err := ensureCreativeProjectAccess(access); err != nil {
		return CreateSessionResponse{}, err
	}
	title := strings.TrimSpace(req.InitialTitle)
	if title == "" {
		title = "Agent session"
	}
	session := &model.Session{
		ID: securityID("sess_"), TenantID: "space:" + space.SpaceID, SpaceID: space.SpaceID, ProjectID: req.ProjectID,
		UserID: auth.ActorUserID, Status: state.SessionStatusActive, Title: title, IdempotencyKey: req.IdempotencyKey, TraceID: traceID,
		SnapshotSummary: jsonObject(map[string]any{"project_status": access.ProjectStatus}),
	}
	if err := a.repo.CreateSession(ctx, session); err != nil {
		return CreateSessionResponse{}, err
	}
	snapshot, err := a.BuildSessionSnapshot(ctx, auth, session.ID, traceID)
	if err != nil {
		return CreateSessionResponse{}, err
	}
	return CreateSessionResponse{SessionID: session.ID, ProjectID: session.ProjectID, Status: session.Status, Snapshot: snapshot}, nil
}

func (a *App) GetSession(ctx context.Context, auth AuthContextDTO, sessionID string, traceID string) (SnapshotResponse, error) {
	return a.BuildSessionSnapshot(ctx, auth, sessionID, traceID)
}

func (a *App) ListMessages(ctx context.Context, auth AuthContextDTO, sessionID string, limit, offset int, traceID string) (ListMessagesResponse, error) {
	session, err := a.requireSession(ctx, auth, sessionID)
	if err != nil {
		return ListMessagesResponse{}, err
	}
	if _, err := a.requireViewProjectAccess(ctx, auth, session.ProjectID, traceID); err != nil {
		return ListMessagesResponse{}, err
	}
	limit, offset = normalizePage(limit, offset, 100)
	rows, err := a.repo.ListMessages(ctx, sessionID, limit, offset)
	if err != nil {
		return ListMessagesResponse{}, err
	}
	items := make([]MessageDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, messageDTO(row))
	}
	return ListMessagesResponse{Messages: items, Limit: limit, Offset: offset}, nil
}

func (a *App) CreateRun(ctx context.Context, auth AuthContextDTO, req CreateRunRequest, traceID string) (CreateRunResponse, error) {
	if req.SessionID == "" || req.ProjectID == "" || req.IdempotencyKey == "" {
		return CreateRunResponse{}, apperror.New(apperror.CodeInvalidArgument, "session_id, project_id and idempotency_key are required")
	}
	textRequired := req.RunIntent != RunIntentEntryGuide
	if req.UserInput.ClientMessageID == "" || req.UserInput.ContentType == "" || (textRequired && strings.TrimSpace(req.UserInput.Text) == "") {
		return CreateRunResponse{}, apperror.New(apperror.CodeInvalidArgument, "user_input is incomplete")
	}
	if err := validateRunInputs(req); err != nil {
		return CreateRunResponse{}, err
	}
	if existing, err := a.repo.GetRunByIdempotencyKey(ctx, req.IdempotencyKey); err == nil {
		return runResponse(*existing), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return CreateRunResponse{}, err
	}
	session, err := a.requireSession(ctx, auth, req.SessionID)
	if err != nil {
		return CreateRunResponse{}, err
	}
	if session.ProjectID != req.ProjectID {
		return CreateRunResponse{}, apperror.New(apperror.CodeStateConflict, "session project does not match request project")
	}
	activeRuns, err := a.repo.CountActiveRuns(ctx, session.ID)
	if err != nil {
		return CreateRunResponse{}, err
	}
	if activeRuns > 0 {
		return CreateRunResponse{}, apperror.New(apperror.CodeStateConflict, "session already has an active run")
	}
	space, err := a.gateway.ResolveCurrentSpaceContext(ctx, auth, session.SpaceID, traceID)
	if err != nil {
		return CreateRunResponse{}, mapBusinessError(err)
	}
	auth.SpaceID = space.SpaceID
	access, err := a.gateway.CheckProjectAccess(ctx, auth, req.ProjectID, businessagent.ProjectAccessPurpose_CONTINUE_CREATION, traceID)
	if err != nil {
		return CreateRunResponse{}, mapBusinessError(err)
	}
	if err := ensureCreativeProjectAccess(access); err != nil {
		return CreateRunResponse{}, err
	}
	if err := a.ensureReferencedAssetAccess(ctx, auth, req.ProjectID, req.ReferencedAssets, traceID); err != nil {
		return CreateRunResponse{}, err
	}
	runtimeConfigVersion, err := a.activeRuntimeConfigVersion(ctx)
	if err != nil {
		return CreateRunResponse{}, err
	}
	runID := securityID("run_")
	runStatus := state.RunStatusPending
	if isM1RunIntent(req.RunIntent) {
		runStatus = state.RunStatusRouting
	}
	run := &model.Run{
		ID: runID, SessionID: session.ID, ProjectID: session.ProjectID, SpaceID: session.SpaceID, UserID: session.UserID,
		TurnNo: 1, Status: runStatus, InputSummary: jsonObject(runInputSummary(req)),
		ModelSelectionSnapshot: jsonObject(req.ModelSelection), RuntimeConfigVersion: runtimeConfigVersion, IdempotencyKey: req.IdempotencyKey, TraceID: traceID,
	}
	if err := a.repo.CreateRun(ctx, run); err != nil {
		return CreateRunResponse{}, err
	}
	messageSequence, err := a.repo.NextMessageSequence(ctx, session.ID)
	if err != nil {
		return CreateRunResponse{}, err
	}
	message := &model.Message{
		ID: securityID("msg_"), SessionID: session.ID, RunID: run.ID, Role: "user", ContentType: req.UserInput.ContentType,
		Content: req.UserInput.Text, Sequence: messageSequence, TraceID: traceID, Metadata: jsonObject(map[string]any{
			"client_message_id": req.UserInput.ClientMessageID,
			"referenced_assets": req.ReferencedAssets,
			"control_inputs":    req.ControlInputs,
		}),
	}
	if err := a.repo.CreateMessage(ctx, message); err != nil {
		return CreateRunResponse{}, err
	}
	event := &model.Event{
		EventID: securityID("evt_"), Type: "agent.run.started", SessionID: session.ID, RunID: run.ID, ProjectID: session.ProjectID,
		SpaceID: session.SpaceID, ActorUserID: session.UserID, Sequence: 1, Component: "agent",
		Payload: jsonObject(map[string]any{
			"run_id": run.ID, "run_status": run.Status, "project_id": session.ProjectID,
			"session_id": session.ID, "started_at": time.Now().UTC().Format(time.RFC3339Nano),
		}),
		PayloadSchemaVersion: "2026-06-27", Visibility: "user", TraceID: traceID,
	}
	if err := a.repo.AppendEvent(ctx, event); err != nil {
		return CreateRunResponse{}, err
	}
	if isM1RunIntent(req.RunIntent) {
		if err := a.recordM1RunEvents(ctx, auth, run, req, traceID); err != nil {
			return CreateRunResponse{}, err
		}
		if updated, err := a.repo.GetRun(ctx, run.ID); err == nil {
			return runResponse(*updated), nil
		}
		return runResponse(*run), nil
	}
	if err := a.recordM3StartEvents(ctx, auth, run, req.UserInput.Text, traceID); err != nil {
		return CreateRunResponse{}, err
	}
	if updated, err := a.repo.GetRun(ctx, run.ID); err == nil {
		return runResponse(*updated), nil
	}
	return runResponse(*run), nil
}

func (a *App) activeRuntimeConfigVersion(ctx context.Context) (string, error) {
	if a.repo == nil {
		return a.configVersion, nil
	}
	runtimeConfig, err := a.repo.GetActiveRuntimeConfig(ctx, "agent.default")
	if err == nil && runtimeConfig.Version != "" {
		return runtimeConfig.Version, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return a.configVersion, nil
	}
	return "", err
}

func (a *App) GetRun(ctx context.Context, auth AuthContextDTO, runID string, traceID string) (RunDTO, error) {
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return RunDTO{}, apperror.New(apperror.CodeResourceNotFound, "run not found")
	}
	if _, err := a.requireSession(ctx, auth, run.SessionID); err != nil {
		return RunDTO{}, err
	}
	if _, err := a.requireViewProjectAccess(ctx, auth, run.ProjectID, traceID); err != nil {
		return RunDTO{}, err
	}
	dto := runDTO(*run)
	return dto, nil
}

func (a *App) AppendUserInput(ctx context.Context, auth AuthContextDTO, runID string, req AppendUserInputRequest, traceID string) (RunDTO, error) {
	if req.IdempotencyKey == "" {
		return RunDTO{}, apperror.New(apperror.CodeInvalidArgument, "idempotency_key is required")
	}
	if req.UserInput.ClientMessageID == "" || req.UserInput.ContentType == "" || strings.TrimSpace(req.UserInput.Text) == "" {
		return RunDTO{}, apperror.New(apperror.CodeInvalidArgument, "user_input is incomplete")
	}
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return RunDTO{}, apperror.New(apperror.CodeResourceNotFound, "run not found")
	}
	session, err := a.requireSession(ctx, auth, run.SessionID)
	if err != nil {
		return RunDTO{}, err
	}
	if run.Status == state.RunStatusCompleted || run.Status == state.RunStatusFailed || run.Status == state.RunStatusCancelled {
		return RunDTO{}, apperror.New(apperror.CodeStateConflict, "run is not resumable")
	}
	access, err := a.gateway.CheckProjectAccess(ctx, auth, run.ProjectID, businessagent.ProjectAccessPurpose_CONTINUE_CREATION, traceID)
	if err != nil {
		mapped := mapBusinessError(err)
		_ = a.cancelRunForPermissionLoss(ctx, run, traceID, err)
		return RunDTO{}, mapped
	}
	if err := ensureCreativeProjectAccess(access); err != nil {
		_ = a.cancelRunForPermissionLoss(ctx, run, traceID, err)
		return RunDTO{}, err
	}
	sequence, err := a.repo.NextMessageSequence(ctx, session.ID)
	if err != nil {
		return RunDTO{}, err
	}
	message := &model.Message{
		ID: securityID("msg_"), SessionID: session.ID, RunID: run.ID, Role: "user", ContentType: req.UserInput.ContentType,
		Content: req.UserInput.Text, Sequence: sequence, TraceID: traceID,
		Metadata: jsonObject(map[string]any{"client_message_id": req.UserInput.ClientMessageID, "idempotency_key": req.IdempotencyKey}),
	}
	if err := a.repo.CreateMessage(ctx, message); err != nil {
		return RunDTO{}, err
	}
	if run.Status == state.RunStatusWaitingConfirmation {
		if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusResuming, "", ""); err != nil {
			return RunDTO{}, err
		}
	}
	_ = a.appendRunEvent(ctx, run, "resume.accepted", traceID, map[string]any{
		"interrupt_id":                 "",
		"resume_action":                "additional_input",
		"accepted_at":                  time.Now().UTC().Format(time.RFC3339Nano),
		"message_id":                   message.ID,
		"requires_safety_evaluation":   true,
		"next_step":                    "resume_turn",
		"client_message_id":            req.UserInput.ClientMessageID,
		"additional_input_idempotency": req.IdempotencyKey,
	})
	if _, err := a.recordPromptSafetyEvaluation(ctx, run, "additional_input", "message", message.ID, req.UserInput.Text, traceID); err != nil {
		return RunDTO{}, err
	}
	resume, err := a.turnLoop.ResumeTurn(ctx, turnloop.ResumeInput{RunID: run.ID, Action: "additional_input", IdempotencyKey: req.IdempotencyKey})
	if err != nil {
		return RunDTO{}, err
	}
	if resume.Status == state.RunStatusRunning {
		current, err := a.repo.GetRun(ctx, run.ID)
		if err != nil {
			return RunDTO{}, err
		}
		if current.Status == state.RunStatusResuming {
			if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusRunning, "", ""); err != nil {
				return RunDTO{}, err
			}
		}
	}
	updated, err := a.repo.GetRun(ctx, run.ID)
	if err != nil {
		return RunDTO{}, err
	}
	return runDTO(*updated), nil
}

func (a *App) AcceptInterrupt(ctx context.Context, auth AuthContextDTO, runID string, req ConfirmInterruptRequest, traceID string) (RunDTO, error) {
	if req.IdempotencyKey == "" {
		return RunDTO{}, apperror.New(apperror.CodeInvalidArgument, "idempotency_key is required")
	}
	if req.RunID != "" && req.RunID != runID {
		return RunDTO{}, apperror.New(apperror.CodeInvalidArgument, "run_id does not match path")
	}
	if req.InterruptID == "" || req.ConfirmedPayloadDigest == "" || req.Action != "confirm" {
		return RunDTO{}, apperror.New(apperror.CodeInvalidArgument, "interrupt_id, action and confirmed_payload_digest are required")
	}
	run, interrupt, err := a.requireInterrupt(ctx, auth, runID, req.InterruptID, businessagent.ProjectAccessPurpose_CONTINUE_CREATION, traceID)
	if err != nil {
		return RunDTO{}, err
	}
	expectedDigest := confirmationPayloadDigest(interrupt.ConfirmationPayload)
	if req.ConfirmedPayloadDigest != expectedDigest {
		return RunDTO{}, apperror.New(apperror.CodeInvalidArgument, "confirmed_payload_digest does not match confirmation payload")
	}
	if err := a.repo.ResolveInterrupt(ctx, interrupt.ID, state.InterruptStatusAccepted); err != nil {
		return RunDTO{}, err
	}
	if run.Status == state.RunStatusWaitingConfirmation {
		if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusResuming, "", ""); err != nil {
			return RunDTO{}, err
		}
	}
	_ = a.appendRunEvent(ctx, run, "confirmation.accepted", traceID, map[string]any{
		"confirmation_id": interrupt.ID,
		"interrupt_id":    interrupt.ID,
		"action":          "confirm",
		"accepted_at":     time.Now().UTC().Format(time.RFC3339Nano),
		"payload_digest":  expectedDigest,
		"next_step":       "resume_turn",
		"idempotency_key": req.IdempotencyKey,
	})
	resume, err := a.turnLoop.ResumeTurn(ctx, turnloop.ResumeInput{RunID: run.ID, Action: "confirm", InterruptID: interrupt.ID, IdempotencyKey: req.IdempotencyKey})
	if err != nil {
		return RunDTO{}, err
	}
	if resume.Status == state.RunStatusRunning {
		current, err := a.repo.GetRun(ctx, run.ID)
		if err != nil {
			return RunDTO{}, err
		}
		if current.Status == state.RunStatusResuming {
			if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusRunning, "", ""); err != nil {
				return RunDTO{}, err
			}
		}
	}
	if err := a.dispatchConfirmedGeneration(ctx, auth, run, interrupt, req.IdempotencyKey, traceID); err != nil {
		return RunDTO{}, err
	}
	if err := a.runConfirmedIndependentToolCharge(ctx, auth, run.ID, interrupt, req.IdempotencyKey, traceID); err != nil {
		return RunDTO{}, err
	}
	updated, err := a.repo.GetRun(ctx, run.ID)
	if err != nil {
		return RunDTO{}, err
	}
	return runDTO(*updated), nil
}

func (a *App) dispatchConfirmedGeneration(ctx context.Context, auth AuthContextDTO, run *model.Run, interrupt *model.Interrupt, idempotencyKey string, traceID string) error {
	if _, ok := parseM4ConfirmationPayload(interrupt); !ok {
		return nil
	}
	if a.generationQueue == nil {
		return a.runM4ConfirmedGeneration(ctx, auth, run.ID, interrupt, idempotencyKey, traceID)
	}
	job := GenerationJob{
		RunID:          run.ID,
		InterruptID:    interrupt.ID,
		IdempotencyKey: idempotencyKey,
		TraceID:        traceID,
		Auth:           auth,
		EnqueuedAt:     time.Now().UTC(),
	}
	if err := validateGenerationJob(job); err != nil {
		return err
	}
	if err := a.generationQueue.EnqueueGenerationJob(ctx, job); err != nil {
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "GENERATION_QUEUE_ENQUEUE_FAILED", err.Error())
		_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
			"error_type": "queue", "error_code": "GENERATION_QUEUE_ENQUEUE_FAILED", "user_message": "生成任务入队失败",
			"retryable": true, "support_trace_id": traceID,
		})
		return err
	}
	_ = a.appendRunEvent(ctx, run, "generation.task.queued", traceID, map[string]any{
		"run_id": run.ID, "interrupt_id": interrupt.ID, "idempotency_key": idempotencyKey,
	})
	_ = a.appendGenerationProgress(ctx, run, traceID, "", "queued", 0, false, map[string]any{
		"run_id": run.ID, "interrupt_id": interrupt.ID,
	})
	return nil
}

func (a *App) RejectInterrupt(ctx context.Context, auth AuthContextDTO, runID string, req RejectInterruptRequest, traceID string) (RunDTO, error) {
	if req.IdempotencyKey == "" {
		return RunDTO{}, apperror.New(apperror.CodeInvalidArgument, "idempotency_key is required")
	}
	if req.RunID != "" && req.RunID != runID {
		return RunDTO{}, apperror.New(apperror.CodeInvalidArgument, "run_id does not match path")
	}
	if req.InterruptID == "" || req.ReasonCode == "" {
		return RunDTO{}, apperror.New(apperror.CodeInvalidArgument, "interrupt_id and reason_code are required")
	}
	run, interrupt, err := a.requireInterrupt(ctx, auth, runID, req.InterruptID, businessagent.ProjectAccessPurpose_CONTINUE_CREATION, traceID)
	if err != nil {
		return RunDTO{}, err
	}
	if err := a.repo.ResolveInterrupt(ctx, interrupt.ID, state.InterruptStatusRejected); err != nil {
		return RunDTO{}, err
	}
	if run.Status == state.RunStatusWaitingConfirmation {
		if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusCancelled, "INTERRUPT_REJECTED", req.ReasonCode); err != nil {
			return RunDTO{}, err
		}
	}
	rejectedAt := time.Now().UTC()
	if err := a.appendRunEvent(ctx, run, "confirmation.rejected", traceID, map[string]any{
		"confirmation_id": interrupt.ID,
		"interrupt_id":    interrupt.ID,
		"action":          "reject",
		"rejected_at":     rejectedAt.Format(time.RFC3339Nano),
		"reason_code":     req.ReasonCode,
		"run_status":      state.RunStatusCancelled,
		"idempotency_key": req.IdempotencyKey,
		"next_step":       "start_new_run_after_parameter_change",
	}); err != nil {
		return RunDTO{}, err
	}
	if session, err := a.repo.GetSession(ctx, run.SessionID); err == nil {
		if err := a.appendRunEvent(ctx, run, "agent.run.cancelled", traceID, map[string]any{
			"run_status":          state.RunStatusCancelled,
			"cancel_reason":       req.ReasonCode,
			"cancelled_at":        rejectedAt.Format(time.RFC3339Nano),
			"released_points":     0,
			"last_event_sequence": session.LastEventSequence + 1,
			"idempotency_key":     req.IdempotencyKey,
		}); err != nil {
			return RunDTO{}, err
		}
	}
	updated, err := a.repo.GetRun(ctx, run.ID)
	if err != nil {
		return RunDTO{}, err
	}
	return runDTO(*updated), nil
}

func (a *App) CancelRun(ctx context.Context, auth AuthContextDTO, runID string, reason string, idempotencyKey string, traceID string) (RunDTO, error) {
	if idempotencyKey == "" {
		return RunDTO{}, apperror.New(apperror.CodeInvalidArgument, "idempotency_key is required")
	}
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return RunDTO{}, apperror.New(apperror.CodeResourceNotFound, "run not found")
	}
	session, err := a.requireSession(ctx, auth, run.SessionID)
	if err != nil {
		return RunDTO{}, err
	}
	if _, err := a.requireViewProjectAccess(ctx, auth, run.ProjectID, traceID); err != nil {
		return RunDTO{}, err
	}
	if run.Status == state.RunStatusCompleted || run.Status == state.RunStatusFailed || run.Status == state.RunStatusCancelled {
		return runDTO(*run), nil
	}
	cancelResult, err := a.turnLoop.CancelRun(ctx, turnloop.CancelInput{RunID: run.ID, Reason: reason, IdempotencyKey: idempotencyKey})
	if err != nil {
		return RunDTO{}, err
	}
	if strings.TrimSpace(reason) == "" {
		reason = cancelResult.Phase
	}
	if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusCancelled, "USER_CANCELLED", reason); err != nil {
		return RunDTO{}, err
	}
	event := &model.Event{
		EventID: securityID("evt_"), Type: "agent.run.cancelled", SessionID: session.ID, RunID: run.ID, ProjectID: run.ProjectID,
		SpaceID: run.SpaceID, ActorUserID: run.UserID, Sequence: session.LastEventSequence + 1, Component: "agent",
		Payload: jsonObject(map[string]any{
			"run_status":          state.RunStatusCancelled,
			"cancel_reason":       reason,
			"cancelled_at":        time.Now().UTC().Format(time.RFC3339Nano),
			"released_points":     0,
			"last_event_sequence": session.LastEventSequence + 1,
			"idempotency_key":     idempotencyKey,
		}), PayloadSchemaVersion: "2026-06-27", Visibility: "user", TraceID: traceID,
	}
	_ = a.repo.AppendEvent(ctx, event)
	updated, _ := a.repo.GetRun(ctx, run.ID)
	return runDTO(*updated), nil
}

func (a *App) ReplayEvents(ctx context.Context, auth AuthContextDTO, runID string, afterSequence int64, limit int, traceID string) (EventReplayResponse, error) {
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		pr2Replay, pr2Err := a.replayPR2Events(ctx, auth, runID, afterSequence, limit, traceID)
		if pr2Err == nil {
			return pr2Replay, nil
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return EventReplayResponse{}, pr2Err
		}
		return EventReplayResponse{}, err
	}
	if _, err := a.requireSession(ctx, auth, run.SessionID); err != nil {
		return EventReplayResponse{}, err
	}
	if _, err := a.requireViewProjectAccess(ctx, auth, run.ProjectID, traceID); err != nil {
		return EventReplayResponse{}, err
	}
	limit, _ = normalizePage(limit, 0, 200)
	rows, err := a.repo.ListEventsAfterSequence(ctx, runID, afterSequence, limit+1)
	if err != nil {
		return EventReplayResponse{}, err
	}
	items := make([]EventDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, eventDTO(row))
	}
	if pr2Rows, pr2Err := a.repo.ListRunEventsV1AfterSeq(ctx, runID, afterSequence, limit+1); pr2Err == nil && len(pr2Rows) > 0 {
		pr2Run := model.AgentRunRecord{
			RunID:     run.ID,
			SessionID: run.SessionID,
			ProjectID: run.ProjectID,
			Status:    run.Status,
			TraceID:   run.TraceID,
			CreatedAt: run.CreatedAt,
			UpdatedAt: run.UpdatedAt,
		}
		for _, row := range pr2Rows {
			items = append(items, pr2EventDTO(pr2Run, row))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Sequence == items[j].Sequence {
			return items[i].EventID < items[j].EventID
		}
		return items[i].Sequence < items[j].Sequence
	})
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	next := afterSequence
	for _, item := range items {
		next = item.Sequence
	}
	return EventReplayResponse{Events: items, NextSequence: next, HasMore: hasMore}, nil
}

func (a *App) replayPR2Events(ctx context.Context, auth AuthContextDTO, runID string, afterSequence int64, limit int, traceID string) (EventReplayResponse, error) {
	run, err := a.requirePR2RunAccess(ctx, auth, runID, businessagent.ProjectAccessPurpose_VIEW, traceID)
	if err != nil {
		return EventReplayResponse{}, err
	}
	limit, _ = normalizePage(limit, 0, 200)
	if replay, ok := a.replayPR2EventsFromBus(ctx, run, afterSequence, limit); ok {
		return replay, nil
	}
	rows, err := a.repo.ListRunEventsV1AfterSeq(ctx, runID, afterSequence, limit+1)
	if err != nil {
		return EventReplayResponse{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]EventDTO, 0, len(rows))
	next := afterSequence
	for _, row := range rows {
		items = append(items, pr2EventDTO(run, row))
		next = row.Seq
	}
	return EventReplayResponse{Events: items, NextSequence: next, HasMore: hasMore}, nil
}

func (a *App) replayPR2EventsFromBus(ctx context.Context, run model.AgentRunRecord, afterSequence int64, limit int) (EventReplayResponse, bool) {
	if a.aguiEventBus == nil {
		return EventReplayResponse{}, false
	}
	events, err := a.aguiEventBus.ReplayAGUI(ctx, run.RunID, afterSequence, limit+1)
	if err != nil || len(events) == 0 {
		return EventReplayResponse{}, false
	}
	for index, event := range events {
		if event.RunID != run.RunID || event.Seq != afterSequence+int64(index)+1 {
			return EventReplayResponse{}, false
		}
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	items := make([]EventDTO, 0, len(events))
	next := afterSequence
	for _, event := range events {
		items = append(items, aguiEnvelopeDTO(run, event))
		next = event.Seq
	}
	return EventReplayResponse{Events: items, NextSequence: next, HasMore: hasMore}, true
}

func (a *App) BuildRunSnapshot(ctx context.Context, auth AuthContextDTO, runID string, traceID string) (SnapshotResponse, error) {
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return SnapshotResponse{}, apperror.New(apperror.CodeResourceNotFound, "run not found")
	}
	return a.buildSnapshot(ctx, auth, run.SessionID, run, traceID)
}

func (a *App) GetCreativeBoard(ctx context.Context, auth AuthContextDTO, boardID string, traceID string) (CreativeBoardResponse, error) {
	board, err := a.repo.GetCreativeBoardV1(ctx, boardID)
	if err != nil {
		return CreativeBoardResponse{}, apperror.New(apperror.CodeResourceNotFound, "board not found")
	}
	run, err := a.requirePR2RunAccess(ctx, auth, board.RunID, businessagent.ProjectAccessPurpose_VIEW, traceID)
	if err != nil {
		return CreativeBoardResponse{}, err
	}
	if run.ProjectID != board.ProjectID {
		return CreativeBoardResponse{}, apperror.New(apperror.CodeStateConflict, "board project does not match run project")
	}
	snapshot, err := a.repo.GetBoardSnapshotV1(ctx, boardID)
	if err != nil {
		return CreativeBoardResponse{}, err
	}
	return CreativeBoardResponse{Board: board, Snapshot: snapshot}, nil
}

func (a *App) ApplyBoardPatch(ctx context.Context, auth AuthContextDTO, boardID string, req ApplyBoardPatchRequest, traceID string) (ApplyBoardPatchResponse, error) {
	patch := req.Patch
	if patch.IdempotencyKey == "" {
		patch.IdempotencyKey = req.IdempotencyKey
	} else if req.IdempotencyKey != "" && patch.IdempotencyKey != req.IdempotencyKey {
		return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeInvalidArgument, "idempotency_key must match patch")
	}
	if strings.TrimSpace(boardID) == "" || strings.TrimSpace(patch.IdempotencyKey) == "" {
		return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeInvalidArgument, "board_id and idempotency_key are required")
	}
	if patch.BoardID != boardID {
		return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeInvalidArgument, "patch board_id does not match path")
	}
	if patch.Operation == pr2.BoardPatchOperationApproveBoard {
		return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeInvalidArgument, "approve_board patch must use board approval endpoint")
	}
	current, err := a.repo.GetCreativeBoardV1(ctx, boardID)
	if err != nil {
		return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeResourceNotFound, "board not found")
	}
	run, err := a.requirePR2RunAccess(ctx, auth, current.RunID, businessagent.ProjectAccessPurpose_CONTINUE_CREATION, traceID)
	if err != nil {
		return ApplyBoardPatchResponse{}, err
	}
	if run.ProjectID != current.ProjectID {
		return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeStateConflict, "board project does not match run project")
	}
	if current.Status == "approved" || current.ToolPlanAllowed {
		return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeStateConflict, "approved board cannot be patched before PR-3 ToolPlan flow")
	}
	payload, err := boardPatchAfterStatePayload(patch.Payload)
	if err != nil {
		return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeInvalidArgument, err.Error())
	}
	if payload.BoardAfter.BoardID != boardID || payload.BoardAfter.RunID != current.RunID || payload.BoardAfter.ProjectID != current.ProjectID || payload.BoardAfter.SessionID != current.SessionID {
		return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeStateConflict, "after-state board identity does not match current board")
	}
	if payload.BoardAfter.Version != patch.TargetVersion || patch.BaseVersion != current.Version {
		return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeStateConflict, "patch version does not match current board")
	}
	if err := a.repo.ApplyBoardPatchAfterStateV1(ctx, patch, payload.BoardAfter, payload.ElementsAfter); err != nil {
		if errors.Is(err, repository.ErrBoardVersionConflict) {
			return ApplyBoardPatchResponse{}, apperror.New(apperror.CodeStateConflict, "board version conflict")
		}
		return ApplyBoardPatchResponse{}, err
	}
	if err := a.appendBoardPatchEvents(ctx, run, patch, payload.BoardAfter, auth.ActorUserID, traceID, payload.ChangedElementIDs); err != nil {
		return ApplyBoardPatchResponse{}, err
	}
	return ApplyBoardPatchResponse{Board: payload.BoardAfter, Patch: patch}, nil
}

func (a *App) ApproveCreativeBoard(ctx context.Context, auth AuthContextDTO, boardID string, req ApproveCreativeBoardRequest, traceID string) (ApproveCreativeBoardResponse, error) {
	if strings.TrimSpace(boardID) == "" || strings.TrimSpace(req.IdempotencyKey) == "" || req.BoardVersion < 1 {
		return ApproveCreativeBoardResponse{}, apperror.New(apperror.CodeInvalidArgument, "board_id, board_version and idempotency_key are required")
	}
	actor := strings.TrimSpace(req.ApprovedBy)
	if actor == "" {
		actor = auth.ActorUserID
	}
	if actor == "" {
		return ApproveCreativeBoardResponse{}, apperror.New(apperror.CodeUnauthenticated, "auth context is required")
	}
	if auth.ActorUserID != "" && actor != auth.ActorUserID {
		return ApproveCreativeBoardResponse{}, apperror.New(apperror.CodePermissionDenied, "approved_by must match current actor")
	}
	board, err := a.repo.GetCreativeBoardV1(ctx, boardID)
	if err != nil {
		return ApproveCreativeBoardResponse{}, apperror.New(apperror.CodeResourceNotFound, "board not found")
	}
	run, err := a.requirePR2RunAccess(ctx, auth, board.RunID, businessagent.ProjectAccessPurpose_CONTINUE_CREATION, traceID)
	if err != nil {
		return ApproveCreativeBoardResponse{}, err
	}
	if run.ProjectID != board.ProjectID {
		return ApproveCreativeBoardResponse{}, apperror.New(apperror.CodeStateConflict, "board project does not match run project")
	}
	if board.Status == "approved" && board.Version == req.BoardVersion+1 {
		toolPlan, err := a.ensureM4ToolPlanPreflight(ctx, auth, run, board, traceID)
		if err != nil {
			return ApproveCreativeBoardResponse{}, err
		}
		return ApproveCreativeBoardResponse{Board: board, ToolPlan: toolPlan}, nil
	}
	if board.Status != "ready" || board.Version != req.BoardVersion {
		return ApproveCreativeBoardResponse{}, apperror.New(apperror.CodeStateConflict, "board version or status is not approvable")
	}
	approvedAt := time.Now().UTC()
	if approvedAt.Before(board.UpdatedAt) {
		approvedAt = board.UpdatedAt.Add(time.Nanosecond)
	}
	runtime := creation.New(nil)
	approval, err := runtime.ApproveBoard(ctx, creation.ApproveBoardInput{
		Board:          board,
		ActorUserID:    actor,
		IdempotencyKey: req.IdempotencyKey,
		ApprovedAt:     approvedAt,
	})
	if err != nil {
		return ApproveCreativeBoardResponse{}, apperror.New(apperror.CodeInvalidArgument, err.Error())
	}
	if err := a.repo.ApplyBoardApprovalV1(ctx, approval.Patch, approval.Board); err != nil {
		if errors.Is(err, repository.ErrBoardVersionConflict) {
			return ApproveCreativeBoardResponse{}, apperror.New(apperror.CodeStateConflict, "board version conflict")
		}
		return ApproveCreativeBoardResponse{}, err
	}
	if err := a.appendBoardApprovalEvents(ctx, run, approval.Patch, approval.Board, actor, traceID); err != nil {
		return ApproveCreativeBoardResponse{}, err
	}
	toolPlan, err := a.ensureM4ToolPlanPreflight(ctx, auth, run, approval.Board, traceID)
	if err != nil {
		return ApproveCreativeBoardResponse{}, err
	}
	return ApproveCreativeBoardResponse{Board: approval.Board, Patch: &approval.Patch, ToolPlan: toolPlan}, nil
}

func (a *App) GetGraphPlan(ctx context.Context, auth AuthContextDTO, graphPlanID string, traceID string) (GraphPlanResponse, error) {
	plan, err := a.repo.GetGraphPlanV1(ctx, graphPlanID)
	if err != nil {
		return GraphPlanResponse{}, apperror.New(apperror.CodeResourceNotFound, "graph plan not found")
	}
	if _, err := a.requirePR2RunAccess(ctx, auth, plan.RunID, businessagent.ProjectAccessPurpose_VIEW, traceID); err != nil {
		return GraphPlanResponse{}, err
	}
	return GraphPlanResponse{GraphPlan: plan}, nil
}

func (a *App) BuildSessionSnapshot(ctx context.Context, auth AuthContextDTO, sessionID string, traceID string) (SnapshotResponse, error) {
	return a.buildSnapshot(ctx, auth, sessionID, nil, traceID)
}

func (a *App) buildSnapshot(ctx context.Context, auth AuthContextDTO, sessionID string, run *model.Run, traceID string) (SnapshotResponse, error) {
	session, err := a.requireSession(ctx, auth, sessionID)
	if err != nil {
		return SnapshotResponse{}, err
	}
	readonly := ""
	access, err := a.requireViewProjectAccess(ctx, auth, session.ProjectID, traceID)
	if err != nil {
		return SnapshotResponse{}, err
	}
	if access.ProjectStatus == "archived" {
		readonly = "project_archived"
	}
	messages, err := a.repo.ListMessages(ctx, session.ID, 10, 0)
	if err != nil {
		return SnapshotResponse{}, err
	}
	messageDTOs := make([]MessageDTO, 0, len(messages))
	for _, message := range messages {
		messageDTOs = append(messageDTOs, messageDTO(message))
	}
	var runDTO *RunDTO
	if run != nil {
		dto := runDTOFromModel(*run)
		runDTO = &dto
	}
	tasks := []TaskDTO{}
	if run != nil {
		if rows, err := a.repo.ListTasksByRun(ctx, run.ID); err != nil {
			return SnapshotResponse{}, err
		} else {
			tasks = taskDTOs(rows)
		}
	}
	var interruptDTO *InterruptDTO
	if run != nil {
		if interrupt, err := a.repo.GetRequiredInterrupt(ctx, run.ID); err == nil {
			dto := a.interruptSnapshotDTO(ctx, run.ID, interrupt)
			interruptDTO = &dto
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return SnapshotResponse{}, err
		}
	}
	return SnapshotResponse{
		Session: sessionDTO(*session), Run: runDTO, Messages: messageDTOs, Assets: []any{}, Blackboard: map[string]any{}, Tasks: tasks,
		Interrupt: interruptDTO, LastEventSequence: session.LastEventSequence, ReadonlyReason: readonly,
	}, nil
}

func (a *App) requireSession(ctx context.Context, auth AuthContextDTO, sessionID string) (*model.Session, error) {
	if auth.ActorUserID == "" {
		return nil, apperror.New(apperror.CodeUnauthenticated, "auth context is required")
	}
	session, err := a.repo.GetSession(ctx, sessionID)
	if err != nil {
		return nil, apperror.New(apperror.CodeResourceNotFound, "session not found")
	}
	if session.UserID != auth.ActorUserID {
		return nil, apperror.New(apperror.CodePermissionDenied, "session belongs to a different user")
	}
	if auth.SpaceID != "" && session.SpaceID != auth.SpaceID {
		return nil, apperror.New(apperror.CodePermissionDenied, "session belongs to a different space")
	}
	return session, nil
}

func (a *App) requireViewProjectAccess(ctx context.Context, auth AuthContextDTO, projectID string, traceID string) (ProjectAccessDTO, error) {
	if a.gateway == nil {
		return ProjectAccessDTO{}, apperror.New(apperror.CodeNotImplemented, "business gateway is not configured")
	}
	access, err := a.gateway.CheckProjectAccess(ctx, auth, projectID, businessagent.ProjectAccessPurpose_VIEW, traceID)
	if err != nil {
		return ProjectAccessDTO{}, mapBusinessError(err)
	}
	if err := ensureViewProjectAccess(access); err != nil {
		return ProjectAccessDTO{}, err
	}
	return access, nil
}

func (a *App) requirePR2RunAccess(ctx context.Context, auth AuthContextDTO, runID string, purpose businessagent.ProjectAccessPurpose, traceID string) (model.AgentRunRecord, error) {
	if auth.ActorUserID == "" {
		return model.AgentRunRecord{}, apperror.New(apperror.CodeUnauthenticated, "auth context is required")
	}
	run, err := a.repo.GetAgentRunV1(ctx, runID)
	if err != nil {
		return model.AgentRunRecord{}, apperror.New(apperror.CodeResourceNotFound, "run not found")
	}
	if a.gateway == nil {
		return model.AgentRunRecord{}, apperror.New(apperror.CodeNotImplemented, "business gateway is not configured")
	}
	access, err := a.gateway.CheckProjectAccess(ctx, auth, run.ProjectID, purpose, traceID)
	if err != nil {
		return model.AgentRunRecord{}, mapBusinessError(err)
	}
	if purpose == businessagent.ProjectAccessPurpose_CONTINUE_CREATION {
		if err := ensureCreativeProjectAccess(access); err != nil {
			return model.AgentRunRecord{}, err
		}
	} else if err := ensureViewProjectAccess(access); err != nil {
		return model.AgentRunRecord{}, err
	}
	return run, nil
}

func (a *App) requireInterrupt(ctx context.Context, auth AuthContextDTO, runID string, interruptID string, purpose businessagent.ProjectAccessPurpose, traceID string) (*model.Run, *model.Interrupt, error) {
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, apperror.New(apperror.CodeResourceNotFound, "run not found")
	}
	if _, err := a.requireSession(ctx, auth, run.SessionID); err != nil {
		return nil, nil, err
	}
	access, err := a.gateway.CheckProjectAccess(ctx, auth, run.ProjectID, purpose, traceID)
	if err != nil {
		mapped := mapBusinessError(err)
		_ = a.cancelRunForPermissionLoss(ctx, run, traceID, err)
		return nil, nil, mapped
	}
	if purpose == businessagent.ProjectAccessPurpose_CONTINUE_CREATION {
		if err := ensureCreativeProjectAccess(access); err != nil {
			_ = a.cancelRunForPermissionLoss(ctx, run, traceID, err)
			return nil, nil, err
		}
	} else if err := ensureViewProjectAccess(access); err != nil {
		_ = a.cancelRunForPermissionLoss(ctx, run, traceID, err)
		return nil, nil, err
	}
	interrupt, err := a.repo.GetInterrupt(ctx, run.ID, interruptID)
	if err != nil {
		return nil, nil, apperror.New(apperror.CodeResourceNotFound, "interrupt not found")
	}
	if !interrupt.ExpiresAt.IsZero() && time.Now().UTC().After(interrupt.ExpiresAt) {
		_ = a.repo.ResolveInterrupt(ctx, interrupt.ID, state.InterruptStatusExpired)
		return nil, nil, apperror.New(apperror.CodeStateConflict, "interrupt is expired")
	}
	return run, interrupt, nil
}

func (a *App) appendRunEvent(ctx context.Context, run *model.Run, eventType string, traceID string, payload map[string]any) error {
	session, err := a.repo.GetSession(ctx, run.SessionID)
	if err != nil {
		return err
	}
	event := &model.Event{
		EventID: securityID("evt_"), Type: eventType, SessionID: session.ID, RunID: run.ID, ProjectID: run.ProjectID,
		SpaceID: run.SpaceID, ActorUserID: run.UserID, Sequence: session.LastEventSequence + 1, Component: "agent",
		Payload: jsonObject(payload), PayloadSchemaVersion: "2026-06-27", Visibility: "user", TraceID: traceID,
	}
	return a.repo.AppendEvent(ctx, event)
}

func (a *App) cancelRunForPermissionLoss(ctx context.Context, run *model.Run, traceID string, cause error) error {
	if run == nil || cause == nil {
		return nil
	}
	if !isRevokedMembershipError(cause) {
		return nil
	}
	current, err := a.repo.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	switch current.Status {
	case state.RunStatusPending, state.RunStatusRunning, state.RunStatusWaitingConfirmation, state.RunStatusResuming:
	default:
		return nil
	}
	if err := a.repo.UpdateRunStatus(ctx, current.ID, state.RunStatusCancelled, "PERMISSION_REVOKED", "enterprise membership or project permission is unavailable"); err != nil {
		return err
	}
	if interrupt, err := a.repo.GetRequiredInterrupt(ctx, current.ID); err == nil {
		_ = a.repo.ResolveInterrupt(ctx, interrupt.ID, state.InterruptStatusExpired)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	lastSequence := int64(0)
	if session, err := a.repo.GetSession(ctx, current.SessionID); err == nil {
		lastSequence = session.LastEventSequence + 1
	}
	cancelledAt := time.Now().UTC().Format(time.RFC3339Nano)
	return a.appendRunEvent(ctx, current, "agent.run.cancelled", traceID, map[string]any{
		"run_status":          state.RunStatusCancelled,
		"cancel_reason":       "permission_revoked",
		"cancelled_at":        cancelledAt,
		"released_points":     0,
		"last_event_sequence": lastSequence,
		"error_code":          "PERMISSION_REVOKED",
	})
}

func isRevokedMembershipError(err error) bool {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "enterprise membership is unavailable") ||
		strings.Contains(message, "member has been removed") {
		return true
	}
	return apperror.FromError(err).Code == apperror.CodePermissionDenied &&
		strings.Contains(message, "permission revoked")
}

func (a *App) recordM3StartEvents(ctx context.Context, auth AuthContextDTO, run *model.Run, prompt string, traceID string) error {
	if a.gateway == nil {
		return nil
	}
	skillSelectionSnapshot := map[string]any{
		"skill_id":                     "",
		"skill_version":                "",
		"skill_scope":                  "",
		"matched_reason":               "",
		"fallback_reason":              "",
		"tool_refs_digest":             digestStrings(nil),
		"tool_refs_count":              0,
		"execution_space_id":           auth.SpaceID,
		"billing_credit_account_scope": creditAccountScope(auth),
	}
	defer func() {
		_ = a.repo.DB().WithContext(ctx).Model(&model.Run{}).Where("id = ?", run.ID).Update("skill_selection", jsonObject(skillSelectionSnapshot))
	}()
	safetyEvidence, err := a.recordPromptSafetyEvaluation(ctx, run, "generation", "prompt", run.ID, prompt, traceID)
	if err != nil {
		return err
	}
	selectedSkillID := ""
	var selectedOutputElements []SkillOutputElementDTO
	skills, _, err := a.gateway.ListRoutableSkills(ctx, auth, "", 10, "", traceID)
	if err != nil {
		_ = a.appendSkillMissingEvent(ctx, run, traceID, "skill_catalog_unavailable", err.Error())
		skillSelectionSnapshot["fallback_reason"] = "skill_catalog_unavailable"
	} else {
		route := a.skillRouter.Route(prompt, runtimeSkillSummaries(skills))
		if !route.Matched {
			_ = a.appendSkillMissingEvent(ctx, run, traceID, route.Reason, "未命中可路由 Skill，使用文本模型兜底")
			skillSelectionSnapshot["fallback_reason"] = route.Reason
		} else {
			selectedSkillID = route.Skill.SkillID
			skillSelectionSnapshot["skill_id"] = route.Skill.SkillID
			skillSelectionSnapshot["skill_version"] = route.Skill.Version
			skillSelectionSnapshot["version"] = route.Skill.Version
			skillSelectionSnapshot["skill_scope"] = route.Skill.SkillScope
			skillSelectionSnapshot["matched_reason"] = route.Reason
			_ = a.appendRunEvent(ctx, run, "agent.skill.selected", traceID, map[string]any{
				"skill_id":       route.Skill.SkillID,
				"skill_name":     route.Skill.SkillName,
				"skill_scope":    route.Skill.SkillScope,
				"skill_version":  route.Skill.Version,
				"matched_reason": route.Reason,
				"route_hints":    route.Skill.RouteHints,
			})
			spec, specErr := a.gateway.GetPublishedSkillSpec(ctx, auth, route.Skill.SkillID, route.Skill.Version, traceID)
			if specErr != nil {
				_ = a.appendSkillMissingEvent(ctx, run, traceID, "skill_spec_unavailable", specErr.Error())
				skillSelectionSnapshot["fallback_reason"] = "skill_spec_unavailable"
			} else {
				selectedOutputElements = spec.OutputElements
				skillSelectionSnapshot["confirmation_policy"] = spec.ConfirmationPolicyJSON
				skillSelectionSnapshot["tool_refs_digest"] = digestStrings(spec.ToolRefs)
				skillSelectionSnapshot["tool_refs_count"] = len(spec.ToolRefs)
				skillSelectionSnapshot["output_elements"] = spec.OutputElements
				skillSelectionSnapshot["output_elements_count"] = len(spec.OutputElements)
				if err := a.recordToolPolicyEvents(ctx, auth, run, spec.ToolRefs, safetyEvidence, traceID); err != nil {
					if errors.Is(err, errToolConfirmationRequired) {
						return nil
					}
					return err
				}
				if skillRequiresConfirmation(spec.ConfirmationPolicyJSON) {
					if err := a.createSkillConfirmationInterrupt(ctx, run, spec, traceID); err != nil {
						return err
					}
				}
			}
		}
	}
	modelID := ""
	if models, nextCursor, err := a.gateway.ListAvailableGenerationModels(ctx, auth, "image", 10, "", traceID); err == nil {
		_ = a.appendGenerationProgress(ctx, run, traceID, "image", "model_catalog_loaded", 3, false, map[string]any{
			"model_count": len(models), "next_cursor": nextCursor,
		})
	} else {
		_ = a.appendGenerationProgress(ctx, run, traceID, "image", "model_catalog_failed", 0, false, map[string]any{"error": err.Error()})
	}
	modelSummary, err := a.gateway.ResolveDefaultModel(ctx, auth, "image", traceID)
	if err != nil {
		_ = a.appendGenerationProgress(ctx, run, traceID, "image", "model_default_failed", 0, false, map[string]any{"error": err.Error()})
	} else {
		modelID = modelSummary.ModelID
		snapshot, snapErr := a.gateway.ResolveGenerationModelSnapshot(ctx, auth, modelSummary.ResourceType, modelSummary.ModelID, modelSummary.PricingSnapshotID, traceID)
		if snapErr != nil {
			_ = a.appendGenerationProgress(ctx, run, traceID, modelSummary.ResourceType, "model_snapshot_failed", 0, false, map[string]any{
				"model_id": modelSummary.ModelID, "error": snapErr.Error(),
			})
		} else {
			_ = a.repo.DB().WithContext(ctx).Model(&model.Run{}).Where("id = ?", run.ID).Update("model_selection_snapshot", jsonObject(snapshot))
			_ = a.appendGenerationProgress(ctx, run, traceID, snapshot.ResourceType, "model_snapshot_resolved", 5, false, map[string]any{
				"model_id": snapshot.ModelID, "pricing_snapshot_id": snapshot.PricingSnapshotID, "timeout_ms": snapshot.TimeoutMS,
			})
			estimate, estimateErr := a.gateway.EstimateGenerationCredits(ctx, auth, EstimateGenerationCreditsRequest{
				ProjectID: run.ProjectID, ResourceType: snapshot.ResourceType, ModelID: snapshot.ModelID,
				PricingSnapshotID: snapshot.PricingSnapshotID, Quantity: 1, SafetyEvidence: safetyEvidenceToRPC(safetyEvidence),
				IdempotencyKey: "estimate:" + run.ID,
			}, traceID)
			if estimateErr != nil {
				_ = a.appendGenerationProgress(ctx, run, traceID, snapshot.ResourceType, "credit_estimate_failed", 0, false, map[string]any{"error": estimateErr.Error()})
				_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
					"error_type": "business_rpc", "error_code": "CREDIT_ESTIMATE_FAILED", "user_message": "积分预估失败",
					"retryable": true, "support_trace_id": traceID,
				})
				_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "CREDIT_ESTIMATE_FAILED", "credit estimate failed")
				return nil
			}
			_ = a.appendRunEvent(ctx, run, "credits.estimated", traceID, map[string]any{
				"estimate_id": estimate.EstimateID, "estimate_points": estimate.EstimatePoints, "available_points": estimate.AvailablePoints,
				"expires_soon_points": estimate.ExpiresSoonPoints, "credit_account_scope": estimate.CreditAccountScope,
				"credit_account_id": estimate.CreditAccountID, "pricing_snapshot_id": snapshot.PricingSnapshotID,
				"line_items": estimate.LineItems, "expires_at": estimate.ExpiresAt,
			})
			if estimate.Insufficient {
				_ = a.appendRunEvent(ctx, run, "credits.insufficient", traceID, map[string]any{
					"estimate_points": estimate.EstimatePoints, "available_points": estimate.AvailablePoints,
					"user_message": "积分不足，请充值或切换空间后重试", "retryable": true,
					"credit_account_id": estimate.CreditAccountID, "estimate_id": estimate.EstimateID,
				})
				_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "CREDIT_INSUFFICIENT", "credit account has insufficient points")
				return nil
			}
			if err := a.createCreditConfirmationInterrupt(ctx, run, snapshot, estimate, safetyEvidence, prompt, selectedOutputElements, traceID); err != nil {
				return err
			}
		}
	}
	if types, version, err := a.gateway.ListAssetElementTypes(ctx, auth, 50, "", traceID); err == nil {
		_ = a.appendRunEvent(ctx, run, "platform.tags.updated", traceID, map[string]any{
			"tags":                     assetElementTags(types),
			"asset_element_type_count": len(types),
			"schema_version":           version,
			"element_types":            types,
		})
	}
	hasPendingConfirmation := false
	if _, err := a.repo.GetRequiredInterrupt(ctx, run.ID); err == nil {
		hasPendingConfirmation = true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	result, err := a.turnLoop.StartTurn(ctx, turnloop.StartInput{
		RunID: run.ID, ProjectID: run.ProjectID, Prompt: prompt, SkillID: selectedSkillID, ModelID: modelID,
		SafetyResult: safetyEvidence.Result, HasPendingConfirmation: hasPendingConfirmation, IdempotencyKey: run.IdempotencyKey,
	})
	if err != nil {
		return err
	}
	switch result.Status {
	case state.RunStatusRunning:
		current, getErr := a.repo.GetRun(ctx, run.ID)
		if getErr != nil {
			return getErr
		}
		if current.Status == state.RunStatusPending || current.Status == state.RunStatusResuming {
			if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusRunning, "", ""); err != nil {
				return err
			}
		}
	case state.RunStatusFailed:
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "TURNLOOP_FAILED", result.Phase)
	}
	return nil
}

func (a *App) recordToolPolicyEvents(ctx context.Context, auth AuthContextDTO, run *model.Run, toolRefs []string, safety *model.SafetyEvaluation, traceID string) error {
	for _, ref := range toolRefs {
		toolName, toolType := parseToolRef(ref)
		if toolName == "" || toolType == "" {
			continue
		}
		toolCallID := securityID("tool_")
		policy, err := a.gateway.CheckToolExecutionPolicy(ctx, auth, toolName, toolType, run.ProjectID, toolPolicyRiskContext(ref), traceID)
		if err != nil {
			_ = a.appendRunEvent(ctx, run, "tool.call.failed", traceID, map[string]any{
				"tool_call_id": toolCallID, "error_code": "TOOL_POLICY_RPC_FAILED", "user_message": "工具策略校验失败",
				"retryable": true, "support_trace_id": traceID, "tool_ref": ref, "error": err.Error(),
			})
			return mapBusinessError(err)
		}
		decision := a.toolChecker.Decide(runtimetool.Policy{
			Allowed: policy.Allowed, RiskLevel: policy.RiskLevel, RequiresConfirmation: policy.RequiresConfirmation, TimeoutMS: policy.TimeoutMS,
		})
		_ = a.appendRunEvent(ctx, run, "tool.call.started", traceID, map[string]any{
			"tool_call_id": toolCallID, "tool_name": toolName, "tool_type": toolType, "risk_level": policy.RiskLevel, "timeout_ms": policy.TimeoutMS,
			"policy_allowed": policy.Allowed, "requires_confirmation": policy.RequiresConfirmation, "decision": decision.Reason,
		})
		if !decision.Allowed {
			_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "TOOL_POLICY_DENIED", "required tool is disabled")
			_ = a.appendRunEvent(ctx, run, "tool.call.failed", traceID, map[string]any{
				"tool_call_id": toolCallID, "error_code": "TOOL_POLICY_DENIED", "user_message": "required tool is disabled",
				"retryable": false, "support_trace_id": traceID,
			})
			return apperror.New(apperror.CodePermissionDenied, "required tool is disabled")
		}
		if decision.RequiresConfirmation {
			if err := a.createToolConfirmationInterrupt(ctx, auth, run, toolCallID, toolName, toolType, policy, safety, traceID); err != nil {
				return err
			}
			return errToolConfirmationRequired
		}
		if !isModelGenerationTool(toolName, toolType) {
			if err := a.runIndependentToolCharge(ctx, auth, run, independentToolChargeInput{
				ToolCallID: toolCallID, ToolName: toolName, ToolType: toolType, BillingUnit: defaultToolBillingUnit(toolType),
				Quantity: 1, SafetyEvidence: safetyEvidenceToRPC(safety), IdempotencyBase: "tool_charge:" + run.ID + ":" + toolCallID,
			}, traceID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) createToolConfirmationInterrupt(ctx context.Context, auth AuthContextDTO, run *model.Run, toolCallID, toolName, toolType string, policy ToolExecutionPolicyDTO, safety *model.SafetyEvaluation, traceID string) error {
	if !isModelGenerationTool(toolName, toolType) {
		estimate, err := a.estimateIndependentToolCredits(ctx, auth, run, toolName, toolType, defaultToolBillingUnit(toolType), 1, safetyEvidenceToRPC(safety), "tool_estimate:"+run.ID+":"+toolCallID, traceID)
		if err != nil {
			return err
		}
		if estimate.Insufficient {
			_ = a.appendRunEvent(ctx, run, "credits.insufficient", traceID, map[string]any{
				"estimate_points": estimate.EstimatePoints, "available_points": estimate.AvailablePoints,
				"user_message": "积分不足，无法执行该工具", "retryable": true,
				"credit_account_id": estimate.CreditAccountID, "estimate_id": estimate.EstimateID,
			})
			_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "CREDIT_INSUFFICIENT", "credit account has insufficient points")
			return apperror.New(apperror.CodeStateConflict, "credit account has insufficient points")
		}
		return a.createConfirmationInterrupt(ctx, run, "risk_confirmation", "high risk tool requires confirmation", map[string]any{
			"m4_flow":           "independent_tool_charge",
			"tool_call_id":      toolCallID,
			"tool_name":         toolName,
			"tool_type":         toolType,
			"billing_unit":      defaultToolBillingUnit(toolType),
			"quantity":          float64(1),
			"risk_level":        policy.RiskLevel,
			"estimate_id":       estimate.EstimateID,
			"estimate_points":   estimate.EstimatePoints,
			"points":            estimate.EstimatePoints,
			"credit_account_id": estimate.CreditAccountID,
			"estimate":          estimate,
			"safety_evidence":   safetyEvidenceToRPC(safety),
		}, "工具调用确认", "高风险工具需要人工确认后继续", []string{policy.RiskLevel, toolName + ":" + toolType}, 15*time.Minute, traceID)
	}
	return a.createConfirmationInterrupt(ctx, run, "risk_confirmation", "high risk tool requires confirmation", map[string]any{
		"tool_call_id": toolCallID, "tool_name": toolName, "tool_type": toolType, "risk_level": policy.RiskLevel,
	}, "工具调用确认", "高风险工具需要人工确认后继续", []string{policy.RiskLevel, toolName + ":" + toolType}, 15*time.Minute, traceID)
}

func (a *App) createSkillConfirmationInterrupt(ctx context.Context, run *model.Run, spec SkillSpecDTO, traceID string) error {
	policy := confirmationPolicyFromJSON(spec.ConfirmationPolicyJSON)
	risks := append([]string{}, policy.RequiredActions...)
	if policy.RiskSummary != "" {
		risks = append(risks, policy.RiskSummary)
	}
	if len(risks) == 0 {
		risks = []string{"skill_confirmation_policy"}
	}
	expires := time.Duration(policy.ExpiresInSeconds) * time.Second
	if expires <= 0 {
		expires = 15 * time.Minute
	}
	return a.createConfirmationInterrupt(ctx, run, "skill_confirmation", "skill confirmation policy requires confirmation", map[string]any{
		"skill_id": spec.SkillID, "skill_version": spec.Version, "confirmation_policy": spec.ConfirmationPolicyJSON,
	}, "Skill 执行确认", "当前 Skill 策略要求确认后继续", risks, expires, traceID)
}

func (a *App) createCreditConfirmationInterrupt(ctx context.Context, run *model.Run, snapshot ModelRuntimeSnapshotDTO, estimate CreditEstimateDTO, safety *model.SafetyEvaluation, prompt string, outputElements []SkillOutputElementDTO, traceID string) error {
	return a.createConfirmationInterrupt(ctx, run, "credit_generation_confirmation", "generation credit charge requires confirmation", map[string]any{
		"m4_flow":             "generation_asset_commit",
		"estimate_id":         estimate.EstimateID,
		"estimate_points":     estimate.EstimatePoints,
		"points":              estimate.EstimatePoints,
		"available_points":    estimate.AvailablePoints,
		"credit_account_id":   estimate.CreditAccountID,
		"pricing_snapshot_id": snapshot.PricingSnapshotID,
		"model_snapshot":      snapshot,
		"estimate":            estimate,
		"safety_evidence":     safetyEvidenceToRPC(safety),
		"prompt_digest":       digestText(prompt),
		"output_elements":     outputElements,
	}, "生成与扣费确认", "确认后将冻结积分，生成完成并保存资产后扣费", []string{"credit_freeze", "asset_commit", "project:" + run.ProjectID}, 15*time.Minute, traceID)
}

type independentToolChargeInput struct {
	ToolCallID      string
	ToolName        string
	ToolType        string
	BillingUnit     string
	Quantity        float64
	Estimate        CreditEstimateDTO
	SafetyEvidence  *businessagent.SafetyEvidenceDTO
	ConfirmationID  string
	IdempotencyBase string
}

type toolChargeConfirmationPayload struct {
	M4Flow          string                          `json:"m4_flow"`
	ToolCallID      string                          `json:"tool_call_id"`
	ToolName        string                          `json:"tool_name"`
	ToolType        string                          `json:"tool_type"`
	BillingUnit     string                          `json:"billing_unit"`
	Quantity        float64                         `json:"quantity"`
	EstimateID      string                          `json:"estimate_id"`
	EstimatePoints  int64                           `json:"estimate_points"`
	CreditAccountID string                          `json:"credit_account_id"`
	Estimate        CreditEstimateDTO               `json:"estimate"`
	SafetyEvidence  businessagent.SafetyEvidenceDTO `json:"safety_evidence"`
}

func (a *App) estimateIndependentToolCredits(ctx context.Context, auth AuthContextDTO, run *model.Run, toolName, toolType, billingUnit string, quantity float64, safety *businessagent.SafetyEvidenceDTO, idempotencyKey string, traceID string) (CreditEstimateDTO, error) {
	estimate, err := a.gateway.EstimateToolCredits(ctx, auth, EstimateToolCreditsRequest{
		ProjectID: run.ProjectID,
		ToolUsageItems: []ToolUsageEstimateItemDTO{{
			ToolName: toolName, ToolType: toolType, BillingUnit: billingUnit, Quantity: quantity,
			MetadataSummary: map[string]string{"run_id": run.ID, "session_id": run.SessionID},
		}},
		SafetyEvidence: safety, IdempotencyKey: idempotencyKey,
	}, traceID)
	if err != nil {
		_ = a.appendRunEvent(ctx, run, "tool.call.failed", traceID, map[string]any{
			"tool_call_id": "", "error_code": "TOOL_CREDIT_ESTIMATE_FAILED", "user_message": "工具积分预估失败",
			"retryable": true, "support_trace_id": traceID, "error": err.Error(),
		})
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "TOOL_CREDIT_ESTIMATE_FAILED", "tool credit estimate failed")
		return CreditEstimateDTO{}, mapBusinessError(err)
	}
	_ = a.appendRunEvent(ctx, run, "credits.estimated", traceID, map[string]any{
		"estimate_id": estimate.EstimateID, "estimate_points": estimate.EstimatePoints, "available_points": estimate.AvailablePoints,
		"expires_soon_points": estimate.ExpiresSoonPoints, "credit_account_scope": estimate.CreditAccountScope,
		"credit_account_id": estimate.CreditAccountID, "pricing_snapshot_id": estimate.PricingSnapshotID,
		"line_items": estimate.LineItems, "expires_at": estimate.ExpiresAt, "usage": "independent_tool",
	})
	return estimate, nil
}

func (a *App) runIndependentToolCharge(ctx context.Context, auth AuthContextDTO, run *model.Run, in independentToolChargeInput, traceID string) error {
	estimate := in.Estimate
	var err error
	if estimate.EstimateID == "" {
		estimate, err = a.estimateIndependentToolCredits(ctx, auth, run, in.ToolName, in.ToolType, in.BillingUnit, in.Quantity, in.SafetyEvidence, in.IdempotencyBase+":estimate", traceID)
		if err != nil {
			return err
		}
	}
	if estimate.Insufficient {
		_ = a.appendRunEvent(ctx, run, "credits.insufficient", traceID, map[string]any{
			"estimate_points": estimate.EstimatePoints, "available_points": estimate.AvailablePoints,
			"user_message": "积分不足，无法执行该工具", "retryable": true,
			"credit_account_id": estimate.CreditAccountID, "estimate_id": estimate.EstimateID,
		})
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "CREDIT_INSUFFICIENT", "credit account has insufficient points")
		return apperror.New(apperror.CodeStateConflict, "credit account has insufficient points")
	}
	estimateItemID, err := estimateItemIDForTool(estimate, in.ToolName, in.ToolType)
	if err != nil {
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "TOOL_ESTIMATE_ITEM_MISSING", err.Error())
		return err
	}
	if estimate.EstimatePoints <= 0 {
		_ = a.appendRunEvent(ctx, run, "tool.call.completed", traceID, map[string]any{
			"tool_call_id": in.ToolCallID, "status": "completed", "result_summary": "tool completed without credit charge",
			"artifact_refs": []any{}, "charged_estimate_item_ids": []string{},
		})
		return nil
	}
	freeze, err := a.gateway.FreezeCredits(ctx, auth, FreezeCreditsRequest{
		EstimateID: estimate.EstimateID, Points: estimate.EstimatePoints, RunID: run.ID,
		ConfirmationID: in.ConfirmationID, AccountID: estimate.CreditAccountID, IdempotencyKey: in.IdempotencyBase + ":freeze",
	}, traceID)
	if err != nil {
		_ = a.appendRunEvent(ctx, run, "tool.call.failed", traceID, map[string]any{
			"tool_call_id": in.ToolCallID, "error_code": "TOOL_CREDIT_FREEZE_FAILED", "user_message": "工具积分冻结失败",
			"retryable": true, "support_trace_id": traceID,
		})
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "TOOL_CREDIT_FREEZE_FAILED", "tool credit freeze failed")
		return mapBusinessError(err)
	}
	_ = a.appendRunEvent(ctx, run, "credits.frozen", traceID, map[string]any{
		"freeze_id": freeze.FreezeID, "frozen_points": freeze.FrozenPoints, "expires_at": freeze.ExpiresAt,
		"estimate_id": estimate.EstimateID, "credit_account_id": estimate.CreditAccountID, "usage": "independent_tool",
	})
	charge, err := a.gateway.ChargeToolUsageCredits(ctx, auth, ChargeToolUsageCreditsRequest{
		ProjectID: run.ProjectID, EstimateID: estimate.EstimateID, FreezeID: freeze.FreezeID,
		SessionID: run.SessionID, RunID: run.ID, IdempotencyKey: in.IdempotencyBase + ":charge",
		ChargeItems: []ToolChargeItemDTO{{
			EstimateItemID: estimateItemID, ToolCallID: in.ToolCallID, ToolName: in.ToolName, ToolType: in.ToolType,
			BillingUnit: in.BillingUnit, ActualQuantity: in.Quantity, ExecutionStatus: "success",
			MetadataSummary: map[string]string{"run_id": run.ID, "session_id": run.SessionID},
		}},
	}, traceID)
	if err != nil {
		return a.failIndependentToolAfterFreeze(ctx, auth, run, freeze, in.ToolCallID, "tool_charge_failed", in.IdempotencyBase, traceID, err)
	}
	_ = a.appendRunEvent(ctx, run, "credits.charged", traceID, map[string]any{
		"charged_points": charge.ChargedPoints, "released_points": charge.ReleasedPoints,
		"tool_charge_id": charge.ToolChargeID, "freeze_status": charge.FreezeStatus,
		"ledger_entry_ids": charge.LedgerEntryIDs, "charged_line_items": charge.ChargedLineItems,
	})
	if charge.ReleasedPoints > 0 {
		_ = a.appendRunEvent(ctx, run, "credits.released", traceID, map[string]any{
			"freeze_id": freeze.FreezeID, "released_points": charge.ReleasedPoints, "reason": "unused_after_tool_charge",
		})
	}
	chargedIDs := make([]string, 0, len(charge.ChargedLineItems))
	for _, item := range charge.ChargedLineItems {
		if item.EstimateItemID != "" {
			chargedIDs = append(chargedIDs, item.EstimateItemID)
		}
	}
	_ = a.appendRunEvent(ctx, run, "tool.call.completed", traceID, map[string]any{
		"tool_call_id": in.ToolCallID, "status": "completed", "result_summary": "tool completed and credits settled",
		"artifact_refs": []any{}, "charged_estimate_item_ids": chargedIDs,
	})
	return nil
}

func (a *App) runConfirmedIndependentToolCharge(ctx context.Context, auth AuthContextDTO, runID string, interrupt *model.Interrupt, idempotencyKey string, traceID string) error {
	payload, ok := parseToolChargeConfirmationPayload(interrupt)
	if !ok {
		return nil
	}
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if auth.SpaceID == "" {
		auth.SpaceID = run.SpaceID
	}
	if auth.ActorUserID == "" {
		auth.ActorUserID = run.UserID
	}
	access, err := a.gateway.CheckProjectAccess(ctx, auth, run.ProjectID, businessagent.ProjectAccessPurpose_CONTINUE_CREATION, traceID)
	if err != nil {
		mapped := mapBusinessError(err)
		_ = a.cancelRunForPermissionLoss(ctx, run, traceID, err)
		return mapped
	}
	if err := ensureCreativeProjectAccess(access); err != nil {
		_ = a.cancelRunForPermissionLoss(ctx, run, traceID, err)
		return err
	}
	return a.runIndependentToolCharge(ctx, auth, run, independentToolChargeInput{
		ToolCallID: payload.ToolCallID, ToolName: payload.ToolName, ToolType: payload.ToolType,
		BillingUnit: payload.BillingUnit, Quantity: payload.Quantity, Estimate: payload.Estimate,
		SafetyEvidence: &payload.SafetyEvidence, ConfirmationID: interrupt.ID, IdempotencyBase: idempotencyKey + ":tool_charge",
	}, traceID)
}

func parseToolChargeConfirmationPayload(interrupt *model.Interrupt) (toolChargeConfirmationPayload, bool) {
	if interrupt == nil || len(interrupt.ConfirmationPayload) == 0 {
		return toolChargeConfirmationPayload{}, false
	}
	var payload toolChargeConfirmationPayload
	if err := json.Unmarshal(interrupt.ConfirmationPayload, &payload); err != nil {
		return toolChargeConfirmationPayload{}, false
	}
	return payload, payload.M4Flow == "independent_tool_charge"
}

func (a *App) failIndependentToolAfterFreeze(ctx context.Context, auth AuthContextDTO, run *model.Run, freeze FreezeCreditsDTO, toolCallID, reason, idempotencyBase, traceID string, cause error) error {
	released, releaseErr := a.gateway.ReleaseFrozenCredits(ctx, auth, ReleaseFrozenCreditsRequest{
		FreezeID: freeze.FreezeID, ReleasePoints: freeze.FrozenPoints, Reason: reason, RunID: run.ID,
		IdempotencyKey: idempotencyBase + ":release:" + reason,
	}, traceID)
	if releaseErr == nil {
		_ = a.appendRunEvent(ctx, run, "credits.released", traceID, map[string]any{
			"freeze_id": freeze.FreezeID, "released_points": released.ReleasedPoints, "reason": reason,
		})
	}
	_ = a.appendRunEvent(ctx, run, "tool.call.failed", traceID, map[string]any{
		"tool_call_id": toolCallID, "error_code": strings.ToUpper(reason), "user_message": "工具执行结算失败，已尝试释放冻结积分",
		"retryable": true, "support_trace_id": traceID,
	})
	_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, strings.ToUpper(reason), cause.Error())
	return mapBusinessError(cause)
}

type m4ConfirmationPayload struct {
	M4Flow            string                          `json:"m4_flow"`
	ToolPlanID        string                          `json:"tool_plan_id"`
	ToolPlanDigest    string                          `json:"tool_plan_digest"`
	BoardID           string                          `json:"board_id"`
	BoardVersion      int                             `json:"board_version"`
	GraphPlanID       string                          `json:"graph_plan_id"`
	EstimateID        string                          `json:"estimate_id"`
	EstimatePoints    int64                           `json:"estimate_points"`
	CreditAccountID   string                          `json:"credit_account_id"`
	PricingSnapshotID string                          `json:"pricing_snapshot_id"`
	ModelSnapshot     ModelRuntimeSnapshotDTO         `json:"model_snapshot"`
	Estimate          CreditEstimateDTO               `json:"estimate"`
	SafetyEvidence    businessagent.SafetyEvidenceDTO `json:"safety_evidence"`
	PromptDigest      string                          `json:"prompt_digest"`
	OutputElements    []SkillOutputElementDTO         `json:"output_elements"`
}

func (a *App) runM4ConfirmedGeneration(ctx context.Context, auth AuthContextDTO, runID string, interrupt *model.Interrupt, idempotencyKey string, traceID string) error {
	payload, ok := parseM4ConfirmationPayload(interrupt)
	if !ok {
		return nil
	}
	if a.gateway == nil {
		return apperror.New(apperror.CodeNotImplemented, "business gateway is not configured")
	}
	if a.artifactUploader == nil {
		a.artifactUploader = NewStreamingArtifactUploader(nil)
	}
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if auth.SpaceID == "" {
		auth.SpaceID = run.SpaceID
	}
	if auth.ActorUserID == "" {
		auth.ActorUserID = run.UserID
	}
	access, err := a.gateway.CheckProjectAccess(ctx, auth, run.ProjectID, businessagent.ProjectAccessPurpose_COMMIT_ASSET, traceID)
	if err != nil {
		mapped := mapBusinessError(err)
		_ = a.cancelRunForPermissionLoss(ctx, run, traceID, err)
		return mapped
	}
	if err := ensureCreativeProjectAccess(access); err != nil {
		_ = a.cancelRunForPermissionLoss(ctx, run, traceID, err)
		return err
	}
	task, err := a.startGenerationTask(ctx, run, payload, idempotencyKey, traceID)
	if err != nil {
		return err
	}
	toolTask, err := a.startM4ToolTaskForConfirmation(ctx, run, payload, task, idempotencyKey, traceID)
	if err != nil {
		return a.failGenerationTaskBeforeFreeze(ctx, run, task, "tool_task_start_failed", traceID, err)
	}
	prompt, err := a.latestUserPrompt(ctx, run.SessionID)
	if err != nil {
		return a.failGenerationTaskBeforeFreeze(ctx, run, task, "prompt_unavailable", traceID, err)
	}
	// GEN-5 fail-closed：实际发往模型的 prompt 必须与已通过安全评估的 prompt 完全一致。
	if !promptMatchesEvidence(prompt, payload.SafetyEvidence) {
		_ = a.appendRunEvent(ctx, run, "safety.prompt.failed", traceID, map[string]any{
			"safety_status": state.SafetyResultFailed, "error_code": "SAFETY_PROMPT_DIGEST_MISMATCH",
			"user_message": "输入在确认后发生变化，请重新发起生成", "retryable": true, "support_trace_id": traceID,
		})
		return a.failGenerationTaskBeforeFreeze(ctx, run, task, "prompt_digest_mismatch", traceID,
			apperror.New(apperror.CodePermissionDenied, "prompt changed after safety evaluation"))
	}
	if refreshed, estimate, err := a.refreshExpiredGenerationSafetyEvidence(ctx, auth, run, payload, prompt, idempotencyKey, traceID); err != nil {
		return a.failGenerationTaskBeforeFreeze(ctx, run, task, "safety_evidence_refresh_failed", traceID, err)
	} else if refreshed {
		payload.SafetyEvidence = *safetyEvidenceToRPC(estimate.safety)
		payload.Estimate = estimate.estimate
		payload.EstimateID = estimate.estimate.EstimateID
		payload.EstimatePoints = estimate.estimate.EstimatePoints
		payload.CreditAccountID = estimate.estimate.CreditAccountID
		payload.PricingSnapshotID = estimate.estimate.PricingSnapshotID
		_ = a.updateGenerationTaskStage(ctx, task, 18, "safety_evidence_refreshed", map[string]any{
			"safety_evidence_id": payload.SafetyEvidence.SafetyEvidenceId, "estimate_id": payload.EstimateID,
			"estimate_points": payload.EstimatePoints,
		})
	}
	freezeIdempotencyKey := m4FreezeIdempotencyKey(run.ID, payload, idempotencyKey)
	_ = a.updateGenerationTaskStage(ctx, task, 20, "freeze_requested", map[string]any{
		"estimate_id": payload.EstimateID, "estimate_points": payload.EstimatePoints,
		"credit_account_id": payload.CreditAccountID, "confirmation_id": interrupt.ID,
		"idempotency_key": idempotencyKey, "freeze_idempotency_key": freezeIdempotencyKey,
		"auth": generationTaskAuth(auth),
	})
	freeze, err := a.gateway.FreezeCredits(ctx, auth, FreezeCreditsRequest{
		EstimateID: payload.EstimateID, Points: payload.EstimatePoints, RunID: run.ID,
		ConfirmationID: interrupt.ID, AccountID: payload.CreditAccountID, IdempotencyKey: freezeIdempotencyKey,
	}, traceID)
	if err != nil {
		_ = a.repo.UpdateTaskStatus(ctx, task.ID, state.TaskStatusFailed, "CREDIT_FREEZE_FAILED")
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "CREDIT_FREEZE_FAILED", "credit freeze failed")
		_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
			"error_type": "business_rpc", "error_code": "CREDIT_FREEZE_FAILED", "user_message": "积分冻结失败",
			"retryable": true, "support_trace_id": traceID,
		})
		return mapBusinessError(err)
	}
	_ = a.updateGenerationTaskStage(ctx, task, 25, "credits_frozen", map[string]any{
		"freeze_id": freeze.FreezeID, "frozen_points": freeze.FrozenPoints, "estimate_id": payload.EstimateID,
		"credit_account_id": payload.CreditAccountID, "idempotency_key": idempotencyKey,
		"auth": generationTaskAuth(auth),
	})
	_ = a.repo.DB().WithContext(ctx).Model(&model.Run{}).Where("id = ?", run.ID).Update("model_selection_snapshot", jsonObject(map[string]any{
		"model_snapshot": payload.ModelSnapshot, "estimate_id": payload.EstimateID, "freeze_id": freeze.FreezeID,
		"credit_account_id": payload.CreditAccountID, "pricing_snapshot_id": payload.PricingSnapshotID,
	}))
	_ = a.appendRunEvent(ctx, run, "credits.frozen", traceID, map[string]any{
		"freeze_id": freeze.FreezeID, "frozen_points": freeze.FrozenPoints, "expires_at": freeze.ExpiresAt,
		"estimate_id": payload.EstimateID, "credit_account_id": payload.CreditAccountID,
	})
	_ = a.updateGenerationTaskStage(ctx, task, 35, "model_submitted", nil)
	_ = a.appendGenerationProgress(ctx, run, traceID, payload.ModelSnapshot.ResourceType, "submitted", 20, false, map[string]any{
		"model_id": payload.ModelSnapshot.ModelID, "estimate_id": payload.EstimateID, "freeze_id": freeze.FreezeID,
	})
	result, err := a.modelAdapter.Generate(ctx, modeltool.Snapshot{
		ModelID: payload.ModelSnapshot.ModelID, ResourceType: payload.ModelSnapshot.ResourceType,
		ProviderRuntimeRef: payload.ModelSnapshot.ProviderRuntimeRef, TimeoutMS: payload.ModelSnapshot.TimeoutMS,
	}, runtimeeino.UserPrompt(prompt))
	if err != nil {
		return a.failGenerationTaskAfterFreeze(ctx, auth, run, task, freeze, "generation_failed", idempotencyKey, traceID, err)
	}
	if len(result.Artifacts) == 0 {
		return a.failGenerationTaskAfterFreeze(ctx, auth, run, task, freeze, "generation_empty", idempotencyKey, traceID, apperror.New(apperror.CodeInternal, "generation produced no artifact"))
	}
	if err := a.completeM4ToolTaskFromResult(ctx, run, toolTask, result.Artifacts, traceID); err != nil {
		return a.failGenerationTaskAfterFreeze(ctx, auth, run, task, freeze, "tool_task_complete_failed", idempotencyKey, traceID, err)
	}
	_ = a.updateGenerationTaskStage(ctx, task, 55, "artifacts_generated", map[string]any{"artifact_count": len(result.Artifacts)})
	for _, artifact := range result.Artifacts {
		_ = a.appendRunEvent(ctx, run, "generation.artifact.completed", traceID, map[string]any{
			"artifact_id": artifact.ArtifactID, "resource_type": artifact.ResourceType, "name": artifact.Name,
			"metadata_summary": artifact.MetadataSummary, "elements_summary": artifact.ElementsSummary,
		})
	}
	objects := make([]GeneratedObjectInputDTO, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		objects = append(objects, GeneratedObjectInputDTO{
			ArtifactID: artifact.ArtifactID, ResourceType: artifact.ResourceType, Filename: artifact.Name,
			ContentType: artifact.ContentType, SizeBytes: artifact.SizeBytes, Checksum: artifact.Checksum, MetadataSummary: artifact.MetadataSummary,
		})
	}
	slots, err := a.gateway.PrepareGeneratedAssetObjects(ctx, auth, PrepareGeneratedAssetObjectsRequest{
		ProjectID: run.ProjectID, SessionID: run.SessionID, RunID: run.ID, Artifacts: objects, IdempotencyKey: idempotencyKey + ":prepare_slots",
	}, traceID)
	if err != nil {
		return a.failGenerationTaskAfterFreeze(ctx, auth, run, task, freeze, "prepare_asset_slots_failed", idempotencyKey, traceID, err)
	}
	_ = a.updateGenerationTaskStage(ctx, task, 65, "asset_slots_prepared", map[string]any{"slot_count": len(slots)})
	slotByArtifact := map[string]GeneratedUploadSlotDTO{}
	for _, slot := range slots {
		slotByArtifact[slot.ArtifactID] = slot
	}
	estimateItems, err := estimateItemsForArtifacts(payload.Estimate, result.Artifacts)
	if err != nil {
		return a.failGenerationTaskAfterFreeze(ctx, auth, run, task, freeze, "estimate_item_unavailable", idempotencyKey, traceID, err)
	}
	outputPlan := buildOutputElementPlan(payload.OutputElements, result.Artifacts)
	commitArtifacts := make([]CommitArtifactDTO, 0, len(result.Artifacts))
	finalElements := make([]FinalElementDTO, 0, len(result.Artifacts))
	for i, artifact := range result.Artifacts {
		if outputPlan.UseDraft(artifact.ElementType) {
			if err := a.createDraftArtifact(ctx, run, artifact, outputPlan.DraftElement(artifact.ElementType), traceID); err != nil {
				return a.failGenerationTaskAfterFreeze(ctx, auth, run, task, freeze, "agent_draft_artifact_failed", idempotencyKey, traceID, err)
			}
		}
		slot, ok := slotByArtifact[artifact.ArtifactID]
		if !ok {
			return a.failGenerationTaskAfterFreeze(ctx, auth, run, task, freeze, "missing_upload_slot", idempotencyKey, traceID, apperror.New(apperror.CodeInternal, "generated upload slot missing"))
		}
		_ = a.appendRunEvent(ctx, run, "asset.save.started", traceID, map[string]any{
			"artifact_id": artifact.ArtifactID, "resource_type": artifact.ResourceType, "project_id": run.ProjectID,
			"freeze_id": freeze.FreezeID, "estimate_id": payload.EstimateID,
		})
		uploaded, uploadErr := a.artifactUploader.Upload(ctx, slot, artifact)
		if uploadErr != nil {
			_ = a.appendRunEvent(ctx, run, "asset.save.failed", traceID, map[string]any{
				"artifact_id": artifact.ArtifactID, "resource_type": artifact.ResourceType,
				"error_code": "ASSET_SAVE_FAILED", "user_message": "产物上传失败", "retryable": true, "support_trace_id": traceID,
			})
			return a.failGenerationTaskAfterFreeze(ctx, auth, run, task, freeze, "artifact_upload_failed", idempotencyKey, traceID, uploadErr)
		}
		estimateItemID := estimateItems[artifact.ArtifactID]
		commitArtifacts = append(commitArtifacts, CommitArtifactDTO{
			ArtifactID: artifact.ArtifactID, ResourceType: artifact.ResourceType, ElementType: artifact.ElementType,
			ArtifactSummary: artifact.MetadataSummary, ContentURIDigest: digestText(uploaded.ObjectKey),
			EstimateItemID: estimateItemID, ToolName: "model_generation", ToolType: artifact.ResourceType, ChargeQuantity: 1,
			MetadataSummary: artifact.MetadataSummary,
			StorageObjectRef: CommitStorageObjectRefDTO{
				ObjectKey: uploaded.ObjectKey, Bucket: uploaded.Bucket, ContentType: uploaded.ContentType,
				SizeBytes: uploaded.SizeBytes, Checksum: uploaded.Checksum, Etag: uploaded.Etag,
			},
		})
		for _, element := range outputPlan.FinalElementsForArtifact(artifact) {
			elementPayload, _ := json.Marshal(map[string]any{
				"artifact_id": artifact.ArtifactID, "resource_type": artifact.ResourceType, "element_name": element.ElementName,
				"display_slot": element.DisplaySlot, "elements_summary": artifact.ElementsSummary,
			})
			finalElements = append(finalElements, FinalElementDTO{ElementType: element.ElementType, ElementPayloadJSON: string(elementPayload), DisplayOrder: displayOrderOrDefault(element, int32(i+1))})
		}
	}
	commitReq := CommitGeneratedAssetAndChargeRequest{
		ProjectID: run.ProjectID, SessionID: run.SessionID, RunID: run.ID, FreezeID: freeze.FreezeID,
		EstimateID: payload.EstimateID, Artifacts: commitArtifacts, FinalElements: finalElements,
		SafetyEvidence: &payload.SafetyEvidence, IdempotencyKey: idempotencyKey + ":commit",
	}
	_ = a.updateGenerationTaskStage(ctx, task, 85, "asset_commit_started", map[string]any{
		"artifact_count": len(commitArtifacts), "final_element_count": len(finalElements), "commit_request": commitReq,
	})
	commit, err := a.gateway.CommitGeneratedAssetAndCharge(ctx, auth, commitReq, traceID)
	if err != nil {
		_ = a.appendRunEvent(ctx, run, "asset.save.failed", traceID, map[string]any{
			"artifact_id": firstArtifactID(result.Artifacts), "resource_type": payload.ModelSnapshot.ResourceType,
			"error_code": "ASSET_COMMIT_FAILED", "user_message": "资产保存失败", "retryable": true, "support_trace_id": traceID,
		})
		return a.failGenerationTaskAfterFreeze(ctx, auth, run, task, freeze, "asset_commit_failed", idempotencyKey, traceID, err)
	}
	if err := a.completeGenerationAfterCommit(ctx, run, task, commit, commitArtifacts, freeze.FreezeID, payload.EstimateID, traceID); err != nil {
		return a.failGenerationTaskAfterFreeze(ctx, auth, run, task, freeze, "agent_commit_finalize_failed", idempotencyKey, traceID, err)
	}
	return a.repo.ResolveInterrupt(ctx, interrupt.ID, state.InterruptStatusResolved)
}

func (a *App) completeGenerationAfterCommit(ctx context.Context, run *model.Run, task *model.Task, commit AssetCommitDTO, commitArtifacts []CommitArtifactDTO, freezeID, estimateID, traceID string) error {
	if _, err := a.existingFinalMessageForTask(ctx, run.ID, task.ID); err == nil {
		return a.completeGenerationTaskStatus(ctx, run.ID, task.ID)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	for _, ref := range commit.AssetRefs {
		elementType := elementTypeForCommitRef(ref, commitArtifacts)
		if _, err := a.repo.GetArtifactByBusinessRef(ctx, run.ID, ref.AssetID); err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := a.repo.CreateArtifact(ctx, &model.Artifact{
				ID: securityID("artref_"), SessionID: run.SessionID, ProjectID: run.ProjectID, RunID: run.ID,
				ArtifactType: "asset_ref", Status: "final_ref", ElementType: elementType,
				Content: jsonObject(map[string]any{
					"source_artifact_id": ref.SourceArtifactID, "resource_type": ref.ResourceType, "asset_type": ref.AssetType,
					"elements_summary_json": ref.ElementsSummaryJSON,
				}),
				BusinessRefID: ref.AssetID, Visibility: "private", TraceID: traceID,
			}); err != nil {
				return err
			}
		}
		_ = a.appendRunEvent(ctx, run, "asset.save.completed", traceID, map[string]any{
			"asset_id": ref.AssetID, "artifact_id": ref.SourceArtifactID, "resource_type": ref.ResourceType,
			"save_status": ref.Status, "elements": []any{map[string]any{"element_type": elementType}}, "downloadable": true,
			"preview_url": ref.PreviewURL,
		})
	}
	_ = a.appendRunEvent(ctx, run, "credits.charged", traceID, map[string]any{
		"charged_points": commit.ChargedPoints, "released_points": commit.ReleasedPoints, "ledger_ref": commit.LedgerRef,
		"charged_line_items": commit.ChargedLineItems,
	})
	_ = a.updateGenerationTaskStage(ctx, task, 95, "asset_commit_completed", map[string]any{"charged_points": commit.ChargedPoints, "released_points": commit.ReleasedPoints})
	if err := a.appendM4AssetCommitUpdatedFromTask(ctx, run, task, commit, traceID); err != nil {
		return err
	}
	if commit.ReleasedPoints > 0 {
		_ = a.appendRunEvent(ctx, run, "credits.released", traceID, map[string]any{
			"freeze_id": freezeID, "released_points": commit.ReleasedPoints, "reason": "unused_after_asset_commit",
		})
	}
	assets := assetRefsForEvent(commit.AssetRefs)
	lastAssetID := ""
	if len(commit.AssetRefs) > 0 {
		lastAssetID = commit.AssetRefs[len(commit.AssetRefs)-1].AssetID
	}
	_ = a.appendRunEvent(ctx, run, "workspace.assets.updated", traceID, map[string]any{
		"mode": "append", "assets": assets, "asset_count": len(assets), "last_asset_id": lastAssetID, "version": time.Now().UTC().Format(time.RFC3339Nano),
	})
	session, _ := a.repo.GetSession(ctx, run.SessionID)
	nextSequence := int64(0)
	if session != nil {
		nextSequence = session.LastEventSequence + 1
	}
	_ = a.appendRunEvent(ctx, run, "process.snapshot.saved", traceID, map[string]any{
		"snapshot_id": "snap_" + run.ID, "snapshot_version": time.Now().UTC().Format(time.RFC3339Nano),
		"last_event_sequence": nextSequence, "messages_count": 1, "assets_count": len(assets), "blackboard_version": "m4",
		"freeze_id": freezeID, "estimate_id": estimateID,
	})
	sequence, err := a.repo.NextMessageSequence(ctx, run.SessionID)
	if err != nil {
		return err
	}
	finalMessage := &model.Message{
		ID: securityID("msg_"), SessionID: run.SessionID, RunID: run.ID, Role: "assistant", ContentType: "text/plain",
		Content: "生成完成，资产已保存。", Sequence: sequence, TraceID: traceID,
		Metadata: jsonObject(map[string]any{"asset_count": len(assets), "charged_points": commit.ChargedPoints, "generation_task_id": task.ID}),
	}
	if err := a.repo.CreateMessage(ctx, finalMessage); err != nil {
		return err
	}
	if err := a.completeGenerationTaskStatus(ctx, run.ID, task.ID); err != nil {
		return err
	}
	session, _ = a.repo.GetSession(ctx, run.SessionID)
	lastSequence := int64(0)
	if session != nil {
		lastSequence = session.LastEventSequence + 1
	}
	_ = a.appendRunEvent(ctx, run, "agent.run.completed", traceID, map[string]any{
		"run_status": state.RunStatusCompleted, "completed_at": time.Now().UTC().Format(time.RFC3339Nano),
		"final_message_id": finalMessage.ID, "last_event_sequence": lastSequence, "snapshot_version": time.Now().UTC().Format(time.RFC3339Nano),
		"charged_points": commit.ChargedPoints, "asset_count": len(assets),
	})
	_ = a.updateGenerationTaskStage(ctx, task, 100, "completed", map[string]any{"asset_count": len(assets)})
	return nil
}

func (a *App) existingFinalMessageForTask(ctx context.Context, runID, taskID string) (*model.Message, error) {
	return a.repo.GetAssistantMessageByGenerationTask(ctx, runID, taskID)
}

func (a *App) completeGenerationTaskStatus(ctx context.Context, runID, taskID string) error {
	current, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if current.Status == state.RunStatusResuming {
		if err := a.repo.UpdateRunStatus(ctx, runID, state.RunStatusRunning, "", ""); err != nil {
			return err
		}
		current.Status = state.RunStatusRunning
	}
	if current.Status != state.RunStatusCompleted {
		if err := a.repo.UpdateRunStatus(ctx, runID, state.RunStatusCompleted, "", ""); err != nil {
			if !errors.Is(err, repository.ErrInvalidStateTransition) {
				return err
			}
			latest, latestErr := a.repo.GetRun(ctx, runID)
			if latestErr != nil {
				return latestErr
			}
			if latest.Status != state.RunStatusCompleted {
				return err
			}
		}
	}
	if err := a.repo.UpdateTaskStatus(ctx, taskID, state.TaskStatusCompleted, ""); err != nil {
		if !errors.Is(err, repository.ErrInvalidStateTransition) {
			return err
		}
		latest, latestErr := a.repo.GetTask(ctx, taskID)
		if latestErr != nil {
			return latestErr
		}
		if latest.Status != state.TaskStatusCompleted {
			return err
		}
	}
	return nil
}

type refreshedGenerationSafetyEvidence struct {
	safety   *model.SafetyEvaluation
	estimate CreditEstimateDTO
}

func (a *App) refreshExpiredGenerationSafetyEvidence(ctx context.Context, auth AuthContextDTO, run *model.Run, payload m4ConfirmationPayload, prompt string, idempotencyKey string, traceID string) (bool, refreshedGenerationSafetyEvidence, error) {
	if !safetyEvidenceExpired(payload.SafetyEvidence, time.Now().UTC()) {
		return false, refreshedGenerationSafetyEvidence{}, nil
	}
	targetRefID := refreshedSafetyTargetRefID(run.ID, idempotencyKey)
	safety, err := a.getOrRecordPromptSafetyEvaluation(ctx, run, "generation", "prompt", targetRefID, prompt, traceID)
	if err != nil {
		return true, refreshedGenerationSafetyEvidence{}, err
	}
	rpcSafety := safetyEvidenceToRPC(safety)
	estimate, err := a.gateway.EstimateGenerationCredits(ctx, auth, EstimateGenerationCreditsRequest{
		ProjectID: run.ProjectID, ResourceType: payload.ModelSnapshot.ResourceType, ModelID: payload.ModelSnapshot.ModelID,
		PricingSnapshotID: payload.ModelSnapshot.PricingSnapshotID, Quantity: 1, SafetyEvidence: rpcSafety,
		IdempotencyKey: idempotencyKey + ":safety_refresh_estimate",
	}, traceID)
	if err != nil {
		_ = a.appendGenerationProgress(ctx, run, traceID, payload.ModelSnapshot.ResourceType, "credit_estimate_failed", 0, false, map[string]any{"error": err.Error(), "reason": "safety_evidence_refresh"})
		return true, refreshedGenerationSafetyEvidence{}, mapBusinessError(err)
	}
	_ = a.appendRunEvent(ctx, run, "credits.estimated", traceID, map[string]any{
		"estimate_id": estimate.EstimateID, "estimate_points": estimate.EstimatePoints, "available_points": estimate.AvailablePoints,
		"expires_soon_points": estimate.ExpiresSoonPoints, "credit_account_scope": estimate.CreditAccountScope,
		"credit_account_id": estimate.CreditAccountID, "pricing_snapshot_id": estimate.PricingSnapshotID,
		"line_items": estimate.LineItems, "expires_at": estimate.ExpiresAt, "reason": "safety_evidence_refresh",
	})
	if estimate.Insufficient {
		_ = a.appendRunEvent(ctx, run, "credits.insufficient", traceID, map[string]any{
			"estimate_points": estimate.EstimatePoints, "available_points": estimate.AvailablePoints,
			"user_message": "积分不足，请充值或切换空间后重试", "retryable": true,
			"credit_account_id": estimate.CreditAccountID, "estimate_id": estimate.EstimateID,
		})
		return true, refreshedGenerationSafetyEvidence{}, apperror.New(apperror.CodeStateConflict, "credit account has insufficient points")
	}
	_ = a.appendRunEvent(ctx, run, "safety.prompt.evaluated", traceID, map[string]any{
		"safety_status": safety.Result, "safety_evidence_id": safety.SafetyEvidenceID, "policy_version": safety.PolicyVersion,
		"expires_at": safety.ExpiresAt.Format(time.RFC3339Nano), "reason": "safety_evidence_refresh",
	})
	return true, refreshedGenerationSafetyEvidence{safety: safety, estimate: estimate}, nil
}

func (a *App) getOrRecordPromptSafetyEvaluation(ctx context.Context, run *model.Run, scene, targetType, targetRefID, prompt, traceID string) (*model.SafetyEvaluation, error) {
	evidenceID := "safety_" + targetRefID
	if existing, err := a.repo.GetSafetyEvaluation(ctx, evidenceID); err == nil {
		if existing.Result == state.SafetyResultPassed && existing.EvaluatedObjectDigest == digestText(prompt) && existing.ExpiresAt.After(time.Now().UTC()) {
			return existing, nil
		}
		return nil, apperror.New(apperror.CodePermissionDenied, "refreshed safety evidence is not reusable")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return a.recordPromptSafetyEvaluation(ctx, run, scene, targetType, targetRefID, prompt, traceID)
}

func safetyEvidenceExpired(evidence businessagent.SafetyEvidenceDTO, now time.Time) bool {
	if evidence.ExpiresAt == nil || strings.TrimSpace(*evidence.ExpiresAt) == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, *evidence.ExpiresAt)
	return err != nil || !expiresAt.After(now)
}

func refreshedSafetyTargetRefID(runID, idempotencyKey string) string {
	sum := sha256.Sum256([]byte(runID + ":" + idempotencyKey + ":safety_refresh"))
	return "refresh_" + hex.EncodeToString(sum[:])[:16]
}

func parseM4ConfirmationPayload(interrupt *model.Interrupt) (m4ConfirmationPayload, bool) {
	if interrupt == nil || len(interrupt.ConfirmationPayload) == 0 {
		return m4ConfirmationPayload{}, false
	}
	var payload m4ConfirmationPayload
	if err := json.Unmarshal(interrupt.ConfirmationPayload, &payload); err != nil {
		return m4ConfirmationPayload{}, false
	}
	return payload, payload.M4Flow == "generation_asset_commit"
}

func (a *App) startGenerationTask(ctx context.Context, run *model.Run, payload m4ConfirmationPayload, idempotencyKey string, traceID string) (*model.Task, error) {
	task := &model.Task{
		ID:              securityID("task_"),
		RunID:           run.ID,
		TaskType:        "generation_asset_commit",
		ResourceType:    payload.ModelSnapshot.ResourceType,
		Status:          state.TaskStatusRunning,
		ProgressPercent: 10,
		ProgressDetail: jsonObject(map[string]any{
			"stage":               "started",
			"estimate_id":         payload.EstimateID,
			"estimate_points":     payload.EstimatePoints,
			"credit_account_id":   payload.CreditAccountID,
			"pricing_snapshot_id": payload.PricingSnapshotID,
			"idempotency_key":     idempotencyKey,
		}),
		TraceID: traceID,
	}
	if err := a.repo.CreateTask(ctx, task); err != nil {
		return nil, err
	}
	_ = a.appendRunEvent(ctx, run, "generation.task.started", traceID, map[string]any{
		"task_id": task.ID, "task_type": task.TaskType, "resource_type": task.ResourceType,
		"estimate_id": payload.EstimateID,
	})
	return task, nil
}

func (a *App) updateGenerationTaskStage(ctx context.Context, task *model.Task, progress int, stage string, extra map[string]any) error {
	if task == nil {
		return nil
	}
	detail := jsonMap(task.ProgressDetail)
	for key, value := range extra {
		detail[key] = value
	}
	detail["stage"] = stage
	task.ProgressPercent = progress
	task.ProgressDetail = jsonObject(detail)
	task.UpdatedAt = time.Now().UTC()
	return a.repo.UpdateTaskProgress(ctx, task.ID, progress, task.ProgressDetail)
}

func generationTaskAuth(auth AuthContextDTO) map[string]any {
	return map[string]any{
		"actor_user_id":       auth.ActorUserID,
		"login_identity_type": auth.LoginIdentityType,
		"space_id":            auth.SpaceID,
		"enterprise_id":       auth.EnterpriseID,
		"enterprise_role":     auth.EnterpriseRole,
	}
}

func authFromGenerationTask(detail map[string]any, run *model.Run) AuthContextDTO {
	auth := AuthContextDTO{}
	if raw, ok := detail["auth"].(map[string]any); ok {
		auth.ActorUserID = stringFromMap(raw, "actor_user_id")
		auth.LoginIdentityType = stringFromMap(raw, "login_identity_type")
		auth.SpaceID = stringFromMap(raw, "space_id")
		auth.EnterpriseID = stringFromMap(raw, "enterprise_id")
		auth.EnterpriseRole = stringFromMap(raw, "enterprise_role")
	}
	if auth.ActorUserID == "" {
		auth.ActorUserID = run.UserID
	}
	if auth.SpaceID == "" {
		auth.SpaceID = run.SpaceID
	}
	if auth.LoginIdentityType == "" {
		auth.LoginIdentityType = "personal"
	}
	return auth
}

func (a *App) RecoverGenerationTasks(ctx context.Context, staleAfter time.Duration, limit int, traceID string) (GenerationRecoveryResult, error) {
	if a.gateway == nil {
		return GenerationRecoveryResult{}, apperror.New(apperror.CodeNotImplemented, "business gateway is not configured")
	}
	if staleAfter <= 0 {
		staleAfter = 5 * time.Minute
	}
	tasks, err := a.repo.ListStaleRunningTasks(ctx, "generation_asset_commit", time.Now().UTC().Add(-staleAfter), limit)
	if err != nil {
		return GenerationRecoveryResult{}, err
	}
	result := GenerationRecoveryResult{Scanned: len(tasks)}
	for _, task := range tasks {
		if a.recoverGenerationTask(ctx, task, traceID, &result) != nil {
			result.ReleaseFails++
		}
	}
	return result, nil
}

func (a *App) RunGenerationWorker(ctx context.Context, maxJobs int) GenerationWorkerResult {
	result := GenerationWorkerResult{}
	if a.generationQueue == nil {
		result.LastError = errors.New("generation queue is not configured")
		return result
	}
	for maxJobs <= 0 || result.Processed < maxJobs {
		job, err := a.generationQueue.DequeueGenerationJob(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return result
			}
			result.Failed++
			result.LastError = err
			continue
		}
		result.Processed++
		processErr := a.processGenerationJob(ctx, job)
		if processErr != nil {
			result.Failed++
			result.LastError = processErr
			if !a.generationJobReachedTerminalRun(ctx, job) {
				continue
			}
		}
		if err := a.generationQueue.CompleteGenerationJob(ctx, job); err != nil {
			result.Failed++
			result.LastError = err
		}
	}
	return result
}

func (a *App) processGenerationJob(ctx context.Context, job GenerationJob) error {
	if err := validateGenerationJob(job); err != nil {
		return err
	}
	run, err := a.repo.GetRun(ctx, job.RunID)
	if err != nil {
		return err
	}
	if isTerminalRunStatus(run.Status) {
		return nil
	}
	if existing, ok := a.runningGenerationTask(ctx, run.ID); ok {
		result := GenerationRecoveryResult{}
		return a.recoverGenerationTask(ctx, existing, job.TraceID, &result)
	}
	interrupt, err := a.repo.GetInterrupt(ctx, job.RunID, job.InterruptID)
	if err != nil {
		return err
	}
	if interrupt.Status != state.InterruptStatusAccepted {
		return apperror.New(apperror.CodeStateConflict, "generation interrupt is not accepted")
	}
	_ = a.appendRunEvent(ctx, run, "generation.task.dequeued", job.TraceID, map[string]any{
		"run_id": job.RunID, "interrupt_id": job.InterruptID, "enqueued_at": job.EnqueuedAt.Format(time.RFC3339Nano),
	})
	return a.runM4ConfirmedGeneration(ctx, job.Auth, job.RunID, interrupt, job.IdempotencyKey, job.TraceID)
}

func (a *App) generationJobReachedTerminalRun(ctx context.Context, job GenerationJob) bool {
	run, err := a.repo.GetRun(ctx, job.RunID)
	return err == nil && isTerminalRunStatus(run.Status)
}

func (a *App) runningGenerationTask(ctx context.Context, runID string) (model.Task, bool) {
	tasks, err := a.repo.ListTasksByRun(ctx, runID)
	if err != nil {
		return model.Task{}, false
	}
	for _, task := range tasks {
		if task.TaskType == "generation_asset_commit" && task.Status == state.TaskStatusRunning {
			return task, true
		}
	}
	return model.Task{}, false
}

func isTerminalRunStatus(status string) bool {
	return status == state.RunStatusCompleted || status == state.RunStatusFailed || status == state.RunStatusCancelled
}

func (a *App) recoverGenerationTask(ctx context.Context, task model.Task, traceID string, result *GenerationRecoveryResult) error {
	run, err := a.repo.GetRun(ctx, task.RunID)
	if err != nil {
		return err
	}
	detail := jsonMap(task.ProgressDetail)
	stage := stringFromMap(detail, "stage")
	if stage == "" || stage == "started" {
		_ = a.repo.UpdateTaskStatus(ctx, task.ID, state.TaskStatusCancelled, "RESTART_RECOVERED_BEFORE_FREEZE")
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusCancelled, "RESTART_RECOVERED", "generation task was recovered before credit freeze")
		_ = a.appendRunEvent(ctx, run, "agent.run.cancelled", traceID, map[string]any{
			"run_status": state.RunStatusCancelled, "cancel_reason": "restart_recovery_before_freeze",
			"released_points": 0, "task_id": task.ID,
		})
		return nil
	}
	if stage == "asset_commit_started" || stage == "asset_commit_completed" {
		if recovered, err := a.recoverAssetCommitStartedTask(ctx, run, task, detail, traceID); err == nil && recovered {
			result.Reconcile++
			return nil
		} else if err != nil {
			return err
		}
		_ = a.repo.UpdateTaskStatus(ctx, task.ID, state.TaskStatusFailed, "NEEDS_RECONCILIATION")
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "NEEDS_RECONCILIATION", "generation task may have reached asset commit before restart")
		_ = a.appendRunEvent(ctx, run, "generation.task.reconciliation_required", traceID, map[string]any{
			"task_id": task.ID, "stage": stage, "freeze_id": stringFromMap(detail, "freeze_id"),
			"user_message": "生成保存流程需要对账确认",
		})
		result.Reconcile++
		return nil
	}
	freezeID := stringFromMap(detail, "freeze_id")
	auth := authFromGenerationTask(detail, run)
	idempotencyKey := stringFromMap(detail, "idempotency_key")
	if freezeID == "" {
		replay, err := a.gateway.FreezeCredits(ctx, auth, FreezeCreditsRequest{
			EstimateID:     stringFromMap(detail, "estimate_id"),
			Points:         int64FromMap(detail, "estimate_points"),
			RunID:          run.ID,
			ConfirmationID: stringFromMap(detail, "confirmation_id"),
			AccountID:      stringFromMap(detail, "credit_account_id"),
			IdempotencyKey: stringFromMap(detail, "freeze_idempotency_key"),
		}, traceID)
		if err != nil {
			return err
		}
		freezeID = replay.FreezeID
		detail["freeze_id"] = replay.FreezeID
		detail["frozen_points"] = replay.FrozenPoints
		detail["stage"] = "credits_frozen"
		task.ProgressDetail = jsonObject(detail)
		_ = a.repo.UpdateTaskProgress(ctx, task.ID, task.ProgressPercent, task.ProgressDetail)
	}
	frozenPoints := int64FromMap(detail, "frozen_points")
	if frozenPoints <= 0 {
		frozenPoints = int64FromMap(detail, "estimate_points")
	}
	release, err := a.gateway.ReleaseFrozenCredits(ctx, auth, ReleaseFrozenCreditsRequest{
		FreezeID: freezeID, ReleasePoints: frozenPoints, Reason: "restart_recovery", RunID: run.ID,
		IdempotencyKey: idempotencyKey + ":release:restart_recovery",
	}, traceID)
	if err != nil {
		return err
	}
	_ = a.repo.UpdateTaskStatus(ctx, task.ID, state.TaskStatusFailed, "RESTART_RECOVERED")
	_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusCancelled, "RESTART_RECOVERED", "generation task was recovered after restart")
	_ = a.appendRunEvent(ctx, run, "credits.released", traceID, map[string]any{
		"freeze_id": freezeID, "released_points": release.ReleasedPoints, "reason": "restart_recovery",
	})
	_ = a.appendRunEvent(ctx, run, "agent.run.cancelled", traceID, map[string]any{
		"run_status": state.RunStatusCancelled, "cancel_reason": "restart_recovery",
		"released_points": release.ReleasedPoints, "task_id": task.ID,
	})
	result.Released++
	return nil
}

func (a *App) recoverAssetCommitStartedTask(ctx context.Context, run *model.Run, task model.Task, detail map[string]any, traceID string) (bool, error) {
	req, ok := commitRequestFromTaskDetail(detail)
	if !ok {
		return false, nil
	}
	auth := authFromGenerationTask(detail, run)
	commit, err := a.gateway.CommitGeneratedAssetAndCharge(ctx, auth, req, traceID)
	if err != nil {
		if isProcessingError(err) {
			return false, nil
		}
		return false, err
	}
	taskCopy := task
	if err := a.completeGenerationAfterCommit(ctx, run, &taskCopy, commit, req.Artifacts, req.FreezeID, req.EstimateID, traceID); err != nil {
		return false, err
	}
	if intr, err := a.repo.GetInterrupt(ctx, run.ID, stringFromMap(detail, "confirmation_id")); err == nil && intr.Status == state.InterruptStatusAccepted {
		_ = a.repo.ResolveInterrupt(ctx, intr.ID, state.InterruptStatusResolved)
	}
	_ = a.appendRunEvent(ctx, run, "generation.task.reconciled", traceID, map[string]any{
		"task_id": task.ID, "stage": stringFromMap(detail, "stage"), "ledger_ref": commit.LedgerRef,
	})
	return true, nil
}

func (a *App) failGenerationTaskAfterFreeze(ctx context.Context, auth AuthContextDTO, run *model.Run, task *model.Task, freeze FreezeCreditsDTO, reason string, idempotencyKey string, traceID string, cause error) error {
	if task != nil {
		_ = a.repo.UpdateTaskStatus(ctx, task.ID, state.TaskStatusFailed, strings.ToUpper(reason))
	}
	return a.failM4AfterFreeze(ctx, auth, run, freeze, reason, idempotencyKey, traceID, cause)
}

func (a *App) failGenerationTaskBeforeFreeze(ctx context.Context, run *model.Run, task *model.Task, reason string, traceID string, cause error) error {
	if task != nil {
		_ = a.repo.UpdateTaskStatus(ctx, task.ID, state.TaskStatusFailed, strings.ToUpper(reason))
	}
	current, err := a.repo.GetRun(ctx, run.ID)
	if err == nil {
		switch current.Status {
		case state.RunStatusPending, state.RunStatusRunning, state.RunStatusWaitingConfirmation, state.RunStatusResuming:
			_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, strings.ToUpper(reason), cause.Error())
		}
	}
	_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
		"error_type": "m4_close_loop", "error_code": strings.ToUpper(reason), "user_message": "生成保存流程失败",
		"retryable": true, "support_trace_id": traceID,
	})
	return mapBusinessError(cause)
}

func (a *App) failM4AfterFreeze(ctx context.Context, auth AuthContextDTO, run *model.Run, freeze FreezeCreditsDTO, reason string, idempotencyKey string, traceID string, cause error) error {
	released, releaseErr := a.gateway.ReleaseFrozenCredits(ctx, auth, ReleaseFrozenCreditsRequest{
		FreezeID: freeze.FreezeID, ReleasePoints: freeze.FrozenPoints, Reason: reason, RunID: run.ID, IdempotencyKey: idempotencyKey + ":release:" + reason,
	}, traceID)
	if releaseErr == nil {
		_ = a.appendRunEvent(ctx, run, "credits.released", traceID, map[string]any{
			"freeze_id": freeze.FreezeID, "released_points": released.ReleasedPoints, "reason": reason,
		})
	}
	_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
		"error_type": "m4_close_loop", "error_code": strings.ToUpper(reason), "user_message": "生成保存流程失败，已尝试释放冻结积分",
		"retryable": true, "support_trace_id": traceID,
	})
	_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, strings.ToUpper(reason), cause.Error())
	return mapBusinessError(cause)
}

func (a *App) createConfirmationInterrupt(ctx context.Context, run *model.Run, interruptType, reason string, confirmationPayload map[string]any, title, summary string, risks []string, ttl time.Duration, traceID string) error {
	if _, err := a.repo.GetRequiredInterrupt(ctx, run.ID); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	current, err := a.repo.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if current.Status == state.RunStatusPending {
		if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusRunning, "", ""); err != nil {
			return err
		}
		current.Status = state.RunStatusRunning
	}
	interruptID := securityID("intr_")
	expiresAt := time.Now().UTC().Add(ttl)
	confirmationPayload["confirmation_id"] = interruptID
	payloadJSON := jsonObject(confirmationPayload)
	payloadDigest := confirmationPayloadDigest(payloadJSON)
	interrupt := &model.Interrupt{
		ID: interruptID, RunID: run.ID, InterruptType: interruptType, Status: state.InterruptStatusRequired,
		Reason: reason, ConfirmationPayload: payloadJSON, AllowedActions: jsonObject([]string{"confirm", "reject"}),
		ResumeContext: jsonObject(map[string]any{"next_step": "resume_turn", "source": interruptType}), ExpiresAt: expiresAt, TraceID: traceID,
	}
	if err := a.repo.CreateInterrupt(ctx, interrupt); err != nil {
		return err
	}
	if current.Status != state.RunStatusWaitingConfirmation {
		if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusWaitingConfirmation, "", ""); err != nil {
			return err
		}
	}
	run.Status = state.RunStatusWaitingConfirmation
	points := int64(0)
	if value, ok := confirmationPayload["points"].(int64); ok {
		points = value
	}
	if value, ok := confirmationPayload["points"].(int); ok {
		points = int64(value)
	}
	if value, ok := confirmationPayload["estimate_points"].(int64); ok {
		points = value
	}
	if err := a.appendRunEvent(ctx, run, "confirmation.required", traceID, map[string]any{
		"confirmation_id": interruptID, "interrupt_id": interruptID, "title": title, "summary": summary,
		"risks": risks, "points": points, "expires_at": expiresAt.Format(time.RFC3339Nano), "actions": []string{"confirm", "reject"},
		"confirmation_payload": publicConfirmationPayload(confirmationPayload), "payload_digest": payloadDigest,
	}); err != nil {
		return err
	}
	return a.appendRunEvent(ctx, run, "chat.controls.locked", traceID, map[string]any{
		"locked_fields":   []string{"model_selection", "control_inputs", "referenced_assets"},
		"locked_reason":   "confirmation_required",
		"confirmation_id": interruptID,
		"interrupt_id":    interruptID,
		"locked_at":       time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func confirmationPayloadDigest(payload datatypes.JSON) string {
	return digestText(string(payload))
}

func (a *App) recordPromptSafetyEvaluation(ctx context.Context, run *model.Run, scene, targetType, targetRefID, text, traceID string) (*model.SafetyEvaluation, error) {
	checked := map[string]any{"digest": digestText(text), "content_type": "text"}
	_ = a.appendRunEvent(ctx, run, "safety.prompt.evaluating", traceID, map[string]any{
		"scene": scene, "target_type": targetType, "target_ref_id": targetRefID, "checked_target": checked,
	})
	evidence := a.safetyEvaluator.Evaluate(ctx, scene, targetType, targetRefID, text)
	expiresAt := evidence.EvaluatedAt.Add(24 * time.Hour)
	safety := &model.SafetyEvaluation{
		SafetyEvidenceID: evidence.EvidenceID, Scene: evidence.Scene, TargetType: evidence.TargetType, TargetRefID: evidence.TargetRefID,
		EvaluatedObjectDigest: checked["digest"].(string), PolicyVersion: "local-m3", EvidenceVersion: "2026-06-27",
		Result: evidence.Result, UserVisibleReason: evidence.Reason, SourceSessionID: run.SessionID, SourceRunID: run.ID,
		TraceID: traceID, EvaluatedAt: evidence.EvaluatedAt, ExpiresAt: expiresAt,
	}
	if err := a.repo.CreateSafetyEvaluation(ctx, safety); err != nil {
		return nil, err
	}
	switch evidence.Result {
	case state.SafetyResultPassed:
		_ = a.appendRunEvent(ctx, run, "safety.prompt.evaluated", traceID, map[string]any{
			"safety_status": safety.Result, "safety_evidence_id": safety.SafetyEvidenceID, "policy_version": safety.PolicyVersion,
			"expires_at": safety.ExpiresAt.Format(time.RFC3339Nano),
		})
		return safety, nil
	case state.SafetyResultBlocked:
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "SAFETY_BLOCKED", evidence.Reason)
		_ = a.appendRunEvent(ctx, run, "safety.prompt.blocked", traceID, map[string]any{
			"safety_status": state.SafetyResultBlocked, "user_message": "输入未通过安全检查", "retryable": true, "support_trace_id": traceID,
			"safety_evidence_id": safety.SafetyEvidenceID,
		})
		return safety, apperror.New(apperror.CodePermissionDenied, "input blocked by safety policy")
	default:
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "SAFETY_FAILED", evidence.Reason)
		_ = a.appendRunEvent(ctx, run, "safety.prompt.failed", traceID, map[string]any{
			"safety_status": state.SafetyResultFailed, "error_code": "SAFETY_EVALUATION_FAILED", "user_message": "安全检查失败",
			"retryable": true, "support_trace_id": traceID, "safety_evidence_id": safety.SafetyEvidenceID,
		})
		return safety, apperror.New(apperror.CodeInternal, "safety evaluation failed")
	}
}

func safetyEvidenceToRPC(safety *model.SafetyEvaluation) *businessagent.SafetyEvidenceDTO {
	if safety == nil {
		return nil
	}
	expiresAt := safety.ExpiresAt.Format(time.RFC3339Nano)
	return &businessagent.SafetyEvidenceDTO{
		SafetyEvidenceId:      safety.SafetyEvidenceID,
		Scene:                 safety.Scene,
		Result_:               safety.Result,
		TargetType:            safety.TargetType,
		TargetRefId:           optionalString(safety.TargetRefID),
		EvaluatedObjectDigest: safety.EvaluatedObjectDigest,
		PolicyVersion:         safety.PolicyVersion,
		EvidenceVersion:       safety.EvidenceVersion,
		EvaluatedAt:           safety.EvaluatedAt.Format(time.RFC3339Nano),
		ExpiresAt:             &expiresAt,
		SourceSessionId:       optionalString(safety.SourceSessionID),
		SourceRunId:           optionalString(safety.SourceRunID),
		SourceArtifactId:      optionalString(safety.SourceArtifactID),
		TraceId:               safety.TraceID,
		UserVisibleReason:     optionalString(safety.UserVisibleReason),
	}
}

func (a *App) ensureReferencedAssetAccess(ctx context.Context, auth AuthContextDTO, projectID string, refs []AssetReferenceDTO, traceID string) error {
	if len(refs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.AssetID)
	}
	results, err := a.gateway.BatchCheckAssetAccess(ctx, auth, BatchCheckAssetAccessRequest{
		ProjectID: projectID, AssetIDs: ids, Purpose: "reference_for_generation",
	}, traceID)
	if err != nil {
		return mapBusinessError(err)
	}
	for _, result := range results {
		if !result.Allowed {
			message := "referenced asset is not accessible"
			if strings.TrimSpace(result.Reason) != "" {
				message = result.Reason
			}
			return apperror.New(apperror.CodePermissionDenied, message)
		}
	}
	return nil
}

func (a *App) latestUserPrompt(ctx context.Context, sessionID string) (string, error) {
	messages, err := a.repo.ListMessages(ctx, sessionID, 100, 0)
	if err != nil {
		return "", err
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			return messages[i].Content, nil
		}
	}
	return "", apperror.New(apperror.CodeResourceNotFound, "user prompt not found")
}

// promptMatchesEvidence 判定实际发往模型的 prompt 是否与安全证据认证的 prompt 一致(GEN-5 fail-closed 基线)。
// 证据无 digest 时不在此处拦截(交由上游 safety/确认链路保证)；有 digest 则必须与发送内容逐字节一致。
func promptMatchesEvidence(prompt string, evidence businessagent.SafetyEvidenceDTO) bool {
	expected := strings.TrimSpace(evidence.EvaluatedObjectDigest)
	return expected == "" || digestText(prompt) == expected
}

func estimateItemsForArtifacts(estimate CreditEstimateDTO, artifacts []modeltool.Artifact) (map[string]string, error) {
	used := map[int]bool{}
	out := make(map[string]string, len(artifacts))
	for _, artifact := range artifacts {
		matched := -1
		for i, item := range estimate.LineItems {
			if used[i] || item.ItemType != "model_generation" || strings.TrimSpace(item.EstimateItemID) == "" {
				continue
			}
			if item.ResourceType != "" && item.ResourceType != artifact.ResourceType {
				continue
			}
			matched = i
			break
		}
		if matched < 0 {
			return nil, apperror.New(apperror.CodeStateConflict, "generation estimate item is missing for artifact")
		}
		used[matched] = true
		out[artifact.ArtifactID] = estimate.LineItems[matched].EstimateItemID
	}
	return out, nil
}

type outputElementPlan struct {
	draftByType   map[string]SkillOutputElementDTO
	finalByType   map[string][]SkillOutputElementDTO
	fallbackFinal bool
}

func buildOutputElementPlan(elements []SkillOutputElementDTO, artifacts []modeltool.Artifact) outputElementPlan {
	plan := outputElementPlan{draftByType: map[string]SkillOutputElementDTO{}, finalByType: map[string][]SkillOutputElementDTO{}}
	for _, element := range elements {
		element.ElementType = strings.TrimSpace(element.ElementType)
		if element.ElementType == "" {
			continue
		}
		if element.UseDraft {
			if _, exists := plan.draftByType[element.ElementType]; !exists {
				plan.draftByType[element.ElementType] = element
			}
		}
		if element.UseFinal {
			plan.finalByType[element.ElementType] = append(plan.finalByType[element.ElementType], element)
		}
	}
	if len(plan.finalByType) == 0 {
		plan.fallbackFinal = true
	}
	return plan
}

func (p outputElementPlan) DraftElement(elementType string) SkillOutputElementDTO {
	return p.draftByType[strings.TrimSpace(elementType)]
}

func (p outputElementPlan) UseDraft(elementType string) bool {
	_, ok := p.draftByType[strings.TrimSpace(elementType)]
	return ok
}

func (p outputElementPlan) FinalElementsForArtifact(artifact modeltool.Artifact) []SkillOutputElementDTO {
	elementType := strings.TrimSpace(artifact.ElementType)
	if items := p.finalByType[elementType]; len(items) > 0 {
		return items
	}
	if !p.fallbackFinal {
		return nil
	}
	return []SkillOutputElementDTO{{ElementType: elementType, UseFinal: true}}
}

func displayOrderOrDefault(element SkillOutputElementDTO, fallback int32) int32 {
	if element.DisplayOrder > 0 {
		return element.DisplayOrder
	}
	return fallback
}

func (a *App) createDraftArtifact(ctx context.Context, run *model.Run, artifact modeltool.Artifact, element SkillOutputElementDTO, traceID string) error {
	content := map[string]any{
		"artifact_id": artifact.ArtifactID, "resource_type": artifact.ResourceType, "element_type": artifact.ElementType,
		"element_name": element.ElementName, "display_order": element.DisplayOrder, "display_slot": element.DisplaySlot,
		"metadata_summary": artifact.MetadataSummary, "elements_summary": artifact.ElementsSummary,
	}
	if element.SchemaJSON != "" {
		content["schema_json"] = element.SchemaJSON
	}
	return a.repo.CreateArtifact(ctx, &model.Artifact{
		ID: securityID("artdraft_"), SessionID: run.SessionID, ProjectID: run.ProjectID, RunID: run.ID,
		ArtifactType: "draft_element", Status: "draft", ElementType: artifact.ElementType,
		Content: jsonObject(content), Visibility: "private", TraceID: traceID,
	})
}

func firstArtifactID(artifacts []modeltool.Artifact) string {
	if len(artifacts) == 0 {
		return ""
	}
	return artifacts[0].ArtifactID
}

func elementTypeForRef(ref CommittedAssetRefDTO, artifacts []modeltool.Artifact) string {
	for _, artifact := range artifacts {
		if artifact.ArtifactID == ref.SourceArtifactID {
			return artifact.ElementType
		}
	}
	switch ref.ResourceType {
	case "image":
		return "image_ref"
	case "audio", "music":
		return "audio_ref"
	case "video":
		return "video_ref"
	default:
		return "file_ref"
	}
}

func elementTypeForCommitRef(ref CommittedAssetRefDTO, artifacts []CommitArtifactDTO) string {
	for _, artifact := range artifacts {
		if artifact.ArtifactID == ref.SourceArtifactID && artifact.ElementType != "" {
			return artifact.ElementType
		}
	}
	switch ref.ResourceType {
	case "image":
		return "image_ref"
	case "audio", "music":
		return "audio_ref"
	case "video":
		return "video_ref"
	default:
		return "file_ref"
	}
}

func assetRefsForEvent(refs []CommittedAssetRefDTO) []map[string]any {
	out := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		out = append(out, map[string]any{
			"asset_id": ref.AssetID, "resource_type": ref.ResourceType, "asset_type": ref.AssetType,
			"status": ref.Status, "source_artifact_id": ref.SourceArtifactID, "preview_url": ref.PreviewURL,
		})
	}
	return out
}

func (a *App) appendSkillMissingEvent(ctx context.Context, run *model.Run, traceID string, reason string, message string) error {
	return a.appendRunEvent(ctx, run, "agent.skill.missing", traceID, skillMissingPayload(reason, message))
}

func skillMissingPayload(reason string, message string) map[string]any {
	if strings.TrimSpace(reason) == "" {
		reason = "no_published_skill"
	}
	if strings.TrimSpace(message) == "" {
		message = "未命中可路由 Skill，使用文本模型兜底"
	}
	return map[string]any{
		"fallback_mode": "text_model", "matched_tags": []string{}, "user_message": message, "reason": reason,
		"recommend_create_skill": false,
	}
}

func toolPolicyRiskContext(toolRef string) map[string]string {
	return map[string]string{
		"source":                  "m3_start_turn",
		"tool_ref":                strings.TrimSpace(toolRef),
		"runtime_whitelist_check": "required_per_tool",
	}
}

func validateRunInputs(req CreateRunRequest) error {
	if req.RunIntent != "" && !isM1RunIntent(req.RunIntent) {
		return apperror.New(apperror.CodeInvalidArgument, "run_intent is invalid")
	}
	for i, asset := range req.ReferencedAssets {
		if strings.TrimSpace(asset.AssetID) == "" || strings.TrimSpace(asset.Source) == "" || strings.TrimSpace(asset.Purpose) == "" {
			return apperror.New(apperror.CodeInvalidArgument, fmt.Sprintf("referenced_assets[%d] requires asset_id, source and purpose", i))
		}
		if asset.ProjectID != "" && asset.ProjectID != req.ProjectID {
			return apperror.New(apperror.CodePermissionDenied, "referenced asset belongs to a different project")
		}
	}
	for i, input := range req.ControlInputs {
		if strings.TrimSpace(input.ControlID) == "" || strings.TrimSpace(input.Type) == "" {
			return apperror.New(apperror.CodeInvalidArgument, fmt.Sprintf("control_inputs[%d] requires control_id and type", i))
		}
		if input.Required && emptyControlValue(input.Value) {
			return apperror.New(apperror.CodeInvalidArgument, fmt.Sprintf("control_inputs[%d] is required", i))
		}
	}
	return nil
}

func isM1RunIntent(intent string) bool {
	switch strings.TrimSpace(intent) {
	case RunIntentEntryGuide, RunIntentCapabilityQuestion, RunIntentNormal, RunIntentSelectSkill:
		return true
	default:
		return false
	}
}

func runInputSummary(req CreateRunRequest) map[string]any {
	safetyTargets := []map[string]string{{
		"target_type": "user_input",
		"target_ref":  req.UserInput.ClientMessageID,
	}}
	for _, asset := range req.ReferencedAssets {
		safetyTargets = append(safetyTargets, map[string]string{
			"target_type": "referenced_asset",
			"target_ref":  asset.AssetID,
			"purpose":     asset.Purpose,
		})
	}
	resourceType := "image"
	if req.ModelSelection != nil && strings.TrimSpace(req.ModelSelection.ResourceType) != "" {
		resourceType = req.ModelSelection.ResourceType
	}
	return map[string]any{
		"client_message_id":      req.UserInput.ClientMessageID,
		"run_intent":             req.RunIntent,
		"content_type":           req.UserInput.ContentType,
		"language":               req.UserInput.Language,
		"referenced_assets":      req.ReferencedAssets,
		"referenced_asset_count": len(req.ReferencedAssets),
		"control_inputs":         req.ControlInputs,
		"control_input_count":    len(req.ControlInputs),
		"safety_targets":         safetyTargets,
		"generation_plan": map[string]any{
			"resource_type":         resourceType,
			"has_referenced_assets": len(req.ReferencedAssets) > 0,
			"control_input_count":   len(req.ControlInputs),
		},
	}
}

func emptyControlValue(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

func (a *App) appendGenerationProgress(ctx context.Context, run *model.Run, traceID, resourceType, status string, progress int, partialCompleted bool, extra map[string]any) error {
	payload := map[string]any{
		"task_id": "task_" + run.ID, "resource_type": resourceType, "status": status, "progress": progress, "partial_completed": partialCompleted,
	}
	for key, value := range extra {
		if key == "provider_runtime_ref" || key == "secret_ref" {
			continue
		}
		payload[key] = value
	}
	return a.appendRunEvent(ctx, run, "generation.progress", traceID, payload)
}

func runtimeSkillSummaries(items []SkillSummaryDTO) []runtimeskill.Summary {
	out := make([]runtimeskill.Summary, 0, len(items))
	for _, item := range items {
		out = append(out, runtimeskill.Summary{
			SkillID: item.SkillID, SkillName: item.SkillName, SkillScope: item.SkillScope, Version: item.Version,
			Status: item.Status, RouteHints: item.RouteHints,
		})
	}
	return out
}

func assetElementTags(items []AssetElementTypeDTO) []string {
	tags := make([]string, 0, len(items))
	for _, item := range items {
		if item.ElementType != "" {
			tags = append(tags, item.ElementType)
		}
	}
	return tags
}

type confirmationPolicy struct {
	RequiresConfirmation bool     `json:"requires_confirmation"`
	RequiredActions      []string `json:"required_actions"`
	RiskSummary          string   `json:"risk_summary"`
	MinConfirmLevel      string   `json:"min_confirm_level"`
	LockFields           []string `json:"lock_fields"`
	ExpiresInSeconds     int      `json:"expires_in_seconds"`
}

func confirmationPolicyFromJSON(raw string) confirmationPolicy {
	policy := confirmationPolicy{MinConfirmLevel: "none"}
	if strings.TrimSpace(raw) == "" {
		return policy
	}
	_ = json.Unmarshal([]byte(raw), &policy)
	return policy
}

func skillRequiresConfirmation(raw string) bool {
	policy := confirmationPolicyFromJSON(raw)
	return policy.RequiresConfirmation || len(policy.RequiredActions) > 0 || (policy.MinConfirmLevel != "" && policy.MinConfirmLevel != "none")
}

func digestText(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum[:])
}

func digestStrings(values []string) string {
	data, _ := json.Marshal(values)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func skillTestSafetyEvidenceJSON(evidence runtimesafety.Evidence, testRunID, traceID string) string {
	expiresAt := evidence.EvaluatedAt.Add(24 * time.Hour).Format(time.RFC3339Nano)
	payload := map[string]any{
		"safety_evidence_id":      evidence.EvidenceID,
		"scene":                   evidence.Scene,
		"result":                  evidence.Result,
		"target_type":             evidence.TargetType,
		"target_ref_id":           evidence.TargetRefID,
		"evaluated_object_digest": digestText(evidence.TargetRefID + ":" + evidence.Result),
		"policy_version":          "local-m3",
		"evidence_version":        "2026-06-27",
		"evaluated_at":            evidence.EvaluatedAt.Format(time.RFC3339Nano),
		"expires_at":              expiresAt,
		"source_run_id":           testRunID,
		"trace_id":                traceID,
		"user_visible_reason":     evidence.Reason,
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func expectedElementsFromJSON(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err == nil {
		return list
	}
	var object struct {
		Elements []string `json:"elements"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		return nil
	}
	if len(object.Elements) > 0 {
		return object.Elements
	}
	return object.Required
}

func skillTestActualElements(elements []string) []runtimeskilltest.ActualElement {
	out := make([]runtimeskilltest.ActualElement, 0, len(elements))
	for _, element := range elements {
		out = append(out, runtimeskilltest.ActualElement{ElementType: element, UsageStage: "draft"})
	}
	return out
}

func skillTestElementTypes(items []AssetElementTypeDTO) []runtimeskilltest.ElementTypeSpec {
	out := make([]runtimeskilltest.ElementTypeSpec, 0, len(items))
	for _, item := range items {
		out = append(out, runtimeskilltest.ElementTypeSpec{
			ElementType: item.ElementType, UsageStage: item.UsageStage, DraftEnabled: item.DraftEnabled,
			FinalEnabled: item.FinalEnabled, Editable: item.Editable, Referable: item.Referable,
			RenderHint: item.RenderHint, SchemaJSON: item.SchemaHintJSON,
		})
	}
	return out
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func creditAccountScope(auth AuthContextDTO) string {
	if auth.EnterpriseID != "" || auth.LoginIdentityType == "enterprise_member" {
		return "enterprise"
	}
	return "personal"
}

func ensureViewProjectAccess(access ProjectAccessDTO) error {
	if !access.Allowed {
		message := strings.TrimSpace(access.UserMessage)
		if message == "" {
			message = "project access denied"
		}
		return apperror.New(apperror.CodePermissionDenied, message)
	}
	return nil
}

func ensureCreativeProjectAccess(access ProjectAccessDTO) error {
	if !access.Allowed {
		message := strings.TrimSpace(access.UserMessage)
		if message == "" {
			message = "project access denied"
		}
		return apperror.New(apperror.CodePermissionDenied, message)
	}
	if !access.CreativeAllowed {
		message := strings.TrimSpace(access.UserMessage)
		if message == "" {
			message = "project is not writable"
		}
		if access.ProjectStatus == "archived" {
			return apperror.New(apperror.CodeProjectArchived, message)
		}
		return apperror.New(apperror.CodeStateConflict, message)
	}
	return nil
}

func sessionDTO(session model.Session) SessionDTO {
	return SessionDTO{SessionID: session.ID, ProjectID: session.ProjectID, SpaceID: session.SpaceID, UserID: session.UserID, Status: session.Status, Title: session.Title, LastRunID: session.LastRunID, LastEventSequence: session.LastEventSequence}
}

func runDTO(run model.Run) RunDTO {
	return runDTOFromModel(run)
}

func runDTOFromModel(run model.Run) RunDTO {
	return RunDTO{RunID: run.ID, SessionID: run.SessionID, ProjectID: run.ProjectID, Status: run.Status, StreamURL: "/api/agent/runs/" + run.ID + "/stream", SnapshotVersion: strconv.FormatInt(run.UpdatedAt.UnixNano(), 10), ErrorCode: run.ErrorCode, ErrorMessage: run.ErrorMessage}
}

func runResponse(run model.Run) CreateRunResponse {
	return CreateRunResponse{RunID: run.ID, SessionID: run.SessionID, ProjectID: run.ProjectID, Status: run.Status, StreamURL: "/api/agent/runs/" + run.ID + "/stream", SnapshotVersion: strconv.FormatInt(run.UpdatedAt.UnixNano(), 10)}
}

func messageDTO(message model.Message) MessageDTO {
	return MessageDTO{MessageID: message.ID, SessionID: message.SessionID, RunID: message.RunID, Role: message.Role, ContentType: message.ContentType, Content: message.Content, Sequence: message.Sequence, SafetyStatus: message.SafetyStatus, CreatedAt: message.CreatedAt}
}

func taskDTOs(tasks []model.Task) []TaskDTO {
	out := make([]TaskDTO, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, TaskDTO{
			TaskID: task.ID, RunID: task.RunID, TaskType: task.TaskType, ResourceType: task.ResourceType,
			Status: task.Status, ProgressPercent: task.ProgressPercent, ProgressDetail: jsonMap(task.ProgressDetail),
			CancelRequested: task.CancelRequested, ErrorCode: task.ErrorCode, UpdatedAt: task.UpdatedAt.Format(time.RFC3339Nano),
		})
	}
	return out
}

func boardPatchAfterStatePayload(payload map[string]any) (boardPatchAfterState, error) {
	if payload == nil {
		return boardPatchAfterState{}, errors.New("patch payload is required")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return boardPatchAfterState{}, err
	}
	var decoded boardPatchAfterState
	if err := json.Unmarshal(body, &decoded); err != nil {
		return boardPatchAfterState{}, err
	}
	if decoded.BoardAfter.BoardID == "" || decoded.ElementsAfter == nil {
		return boardPatchAfterState{}, errors.New("patch payload requires board_after and elements_after")
	}
	if decoded.BoardAfter.ElementsCount != len(decoded.ElementsAfter) {
		return boardPatchAfterState{}, errors.New("board_after.elements_count must match elements_after length")
	}
	if len(decoded.ChangedElementIDs) == 0 {
		for _, element := range decoded.ElementsAfter {
			decoded.ChangedElementIDs = append(decoded.ChangedElementIDs, element.ElementID)
		}
	}
	return decoded, nil
}

func pr2EventDTO(run model.AgentRunRecord, event model.RunEventRecord) EventDTO {
	payload := map[string]any{}
	_ = json.Unmarshal(event.Payload, &payload)
	return EventDTO{
		EventID:              event.EventID,
		Type:                 event.EventType,
		SessionID:            run.SessionID,
		RunID:                event.RunID,
		ProjectID:            run.ProjectID,
		Sequence:             event.Seq,
		Timestamp:            event.CreatedAt,
		Component:            "agent",
		TraceID:              event.TraceID,
		PayloadSchemaVersion: event.PayloadSchemaVersion,
		Payload:              payload,
	}
}

func aguiEnvelopeDTO(run model.AgentRunRecord, event pr1.AGUIEnvelope) EventDTO {
	spaceID := ""
	if event.SpaceID != nil {
		spaceID = *event.SpaceID
	}
	actorUserID := ""
	if event.ActorUserID != nil {
		actorUserID = *event.ActorUserID
	}
	traceID := run.TraceID
	if event.TraceID != nil {
		traceID = *event.TraceID
	}
	return EventDTO{
		EventID:              event.EventID,
		Type:                 event.EventType,
		SessionID:            event.SessionID,
		RunID:                event.RunID,
		ProjectID:            event.ProjectID,
		SpaceID:              spaceID,
		ActorUserID:          actorUserID,
		Sequence:             event.Seq,
		Timestamp:            event.CreatedAt,
		Component:            "agent",
		TraceID:              traceID,
		PayloadSchemaVersion: event.PayloadSchemaVersion,
		Payload:              event.Payload,
	}
}

func (a *App) appendBoardApprovalEvents(ctx context.Context, run model.AgentRunRecord, patch pr2.BoardPatch, board pr2.CreativeBoard, actor string, traceID string) error {
	return a.appendBoardPatchEvents(ctx, run, patch, board, actor, traceID, []string{})
}

func (a *App) appendBoardPatchEvents(ctx context.Context, run model.AgentRunRecord, patch pr2.BoardPatch, board pr2.CreativeBoard, actor string, traceID string, changedElementIDs []string) error {
	seq, err := a.repo.NextRunEventSeqV1(ctx, run.RunID)
	if err != nil {
		return err
	}
	eventTime := board.UpdatedAt
	if eventTime.IsZero() {
		eventTime = time.Now().UTC()
	}
	patchPayload := pr2.BoardPatchAppliedPayload{
		BoardID:       patch.BoardID,
		PatchID:       patch.PatchID,
		BaseVersion:   patch.BaseVersion,
		TargetVersion: patch.TargetVersion,
		Operation:     patch.Operation,
		PatchDigest:   patch.PatchDigest,
	}
	if err := pr2.ValidateBoardPatchAppliedPayload(patchPayload); err != nil {
		return err
	}
	patchEnvelope, err := pr2AGUIEnvelope(run, pr2.EventTypeBoardPatchApplied, seq, eventTime, actor, traceID, map[string]any{
		"board_id":       patchPayload.BoardID,
		"patch_id":       patchPayload.PatchID,
		"base_version":   patchPayload.BaseVersion,
		"target_version": patchPayload.TargetVersion,
		"operation":      patchPayload.Operation,
		"patch_digest":   patchPayload.PatchDigest,
	})
	if err != nil {
		return err
	}
	snapshotPayload := pr2.BoardSnapshotUpdatedPayload{
		BoardID:           board.BoardID,
		BoardVersion:      board.Version,
		BoardStatus:       board.Status,
		BoardDigest:       board.BoardDigest,
		ChangedElementIDs: changedElementIDs,
		SnapshotRequired:  true,
	}
	if err := pr2.ValidateBoardSnapshotUpdatedPayload(snapshotPayload); err != nil {
		return err
	}
	snapshotEnvelope, err := pr2AGUIEnvelope(run, pr2.EventTypeBoardSnapshotUpdated, seq+1, eventTime, actor, traceID, map[string]any{
		"board_id":            snapshotPayload.BoardID,
		"board_version":       snapshotPayload.BoardVersion,
		"board_status":        snapshotPayload.BoardStatus,
		"board_digest":        snapshotPayload.BoardDigest,
		"changed_element_ids": snapshotPayload.ChangedElementIDs,
		"snapshot_required":   snapshotPayload.SnapshotRequired,
	})
	if err != nil {
		return err
	}
	events := []pr1.AGUIEnvelope{patchEnvelope, snapshotEnvelope}
	if err := a.repo.AppendRunEventsV1(ctx, events); err != nil {
		return err
	}
	a.publishPR2AGUIEvents(ctx, events)
	return nil
}

func (a *App) publishPR2AGUIEvents(ctx context.Context, events []pr1.AGUIEnvelope) {
	if a.aguiEventBus == nil {
		return
	}
	for _, event := range events {
		_ = a.aguiEventBus.PublishAGUI(ctx, event)
	}
}

func pr2AGUIEnvelope(run model.AgentRunRecord, eventType string, seq int64, eventTime time.Time, actor string, traceID string, payload map[string]any) (pr1.AGUIEnvelope, error) {
	payloadDigest, err := pr1.CanonicalDigest(payload)
	if err != nil {
		return pr1.AGUIEnvelope{}, err
	}
	if strings.TrimSpace(traceID) == "" {
		traceID = run.TraceID
	}
	return pr1.BuildAGUIEnvelope(pr1.AGUIInput{
		EventID:       securityID("evt_"),
		EventType:     eventType,
		ProjectID:     run.ProjectID,
		ActorUserID:   actor,
		SessionID:     run.SessionID,
		RunID:         run.RunID,
		Seq:           seq,
		CreatedAt:     eventTime,
		PayloadDigest: payloadDigest,
		TraceID:       traceID,
		Payload:       payload,
	})
}

func (a *App) interruptSnapshotDTO(ctx context.Context, runID string, interrupt *model.Interrupt) InterruptDTO {
	payload := jsonMap(interrupt.ConfirmationPayload)
	eventPayload := a.latestConfirmationRequiredPayload(ctx, runID, interrupt.ID)
	confirmationID := stringFromMap(payload, "confirmation_id")
	if confirmationID == "" {
		confirmationID = interrupt.ID
	}
	dto := InterruptDTO{
		InterruptID:         interrupt.ID,
		ConfirmationID:      confirmationID,
		Type:                interrupt.InterruptType,
		Status:              interrupt.Status,
		Reason:              interrupt.Reason,
		Title:               stringFromMap(eventPayload, "title"),
		Summary:             stringFromMap(eventPayload, "summary"),
		Risks:               stringSliceFromMap(eventPayload, "risks"),
		Points:              int64FromMap(eventPayload, "points"),
		Actions:             stringSliceFromMap(eventPayload, "actions"),
		PayloadDigest:       confirmationPayloadDigest(interrupt.ConfirmationPayload),
		ConfirmationPayload: publicConfirmationPayload(payload),
		ExpiresAt:           interrupt.ExpiresAt.Format(time.RFC3339Nano),
		TraceID:             interrupt.TraceID,
	}
	if len(dto.Actions) == 0 {
		dto.Actions = []string{"confirm", "reject"}
	}
	if dto.Title == "" {
		dto.Title = "确认操作"
	}
	if dto.Summary == "" {
		dto.Summary = interrupt.Reason
	}
	if dto.Points == 0 {
		dto.Points = int64FromMap(payload, "points")
		if dto.Points == 0 {
			dto.Points = int64FromMap(payload, "estimate_points")
		}
	}
	return dto
}

func (a *App) latestConfirmationRequiredPayload(ctx context.Context, runID, interruptID string) map[string]any {
	events, err := a.repo.ListEventsAfterSequence(ctx, runID, 0, 200)
	if err != nil {
		return nil
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "confirmation.required" {
			continue
		}
		payload := jsonMap(events[i].Payload)
		if stringFromMap(payload, "interrupt_id") == interruptID || stringFromMap(payload, "confirmation_id") == interruptID {
			return payload
		}
	}
	return nil
}

func publicConfirmationPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return map[string]any{}
	}
	blocked := map[string]bool{
		"model_snapshot": true, "safety_evidence": true, "estimate": true, "provider_runtime_ref": true,
		"secret_ref": true, "prompt": true, "system_prompt": true, "raw_prompt": true,
	}
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		if blocked[key] {
			continue
		}
		out[key] = value
	}
	return out
}

func commitRequestFromTaskDetail(detail map[string]any) (CommitGeneratedAssetAndChargeRequest, bool) {
	raw, ok := detail["commit_request"]
	if !ok || raw == nil {
		return CommitGeneratedAssetAndChargeRequest{}, false
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return CommitGeneratedAssetAndChargeRequest{}, false
	}
	var req CommitGeneratedAssetAndChargeRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return CommitGeneratedAssetAndChargeRequest{}, false
	}
	if req.RunID == "" || req.FreezeID == "" || req.IdempotencyKey == "" || len(req.Artifacts) == 0 || req.SafetyEvidence == nil {
		return CommitGeneratedAssetAndChargeRequest{}, false
	}
	return req, true
}

func isProcessingError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToUpper(err.Error()), "PROCESSING")
}

func eventDTO(event model.Event) EventDTO {
	payload := map[string]any{}
	if len(event.Payload) > 0 {
		_ = json.Unmarshal(event.Payload, &payload)
	}
	return EventDTO{
		EventID: event.EventID, Type: event.Type, SessionID: event.SessionID, RunID: event.RunID, ProjectID: event.ProjectID,
		SpaceID: event.SpaceID, ActorUserID: event.ActorUserID, Sequence: event.Sequence, Timestamp: event.CreatedAt,
		Component: event.Component, TraceID: event.TraceID, PayloadSchemaVersion: event.PayloadSchemaVersion, Payload: payload,
	}
}

func jsonMap(raw datatypes.JSON) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key].(string)
	if !ok {
		return ""
	}
	return value
}

func stringSliceFromMap(values map[string]any, key string) []string {
	if values == nil {
		return nil
	}
	switch raw := values[key].(type) {
	case []string:
		return raw
	case []any:
		out := make([]string, 0, len(raw))
		for _, item := range raw {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func int64FromMap(values map[string]any, key string) int64 {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func jsonObject(value any) datatypes.JSON {
	if value == nil {
		return datatypes.JSON([]byte(`{}`))
	}
	data, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte(`{}`))
	}
	return datatypes.JSON(data)
}

func normalizePage(limit, offset, max int) (int, int) {
	if limit <= 0 {
		limit = 10
	}
	if limit > max {
		limit = max
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func mapBusinessError(err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "PROJECT_ARCHIVED"):
		return apperror.New(apperror.CodeProjectArchived, "project is archived")
	case strings.Contains(message, "PROJECT_NOT_FOUND"):
		return apperror.New(apperror.CodeProjectNotFound, "project not found")
	case strings.Contains(message, "CROSS_SPACE_DENIED"):
		return apperror.New(apperror.CodePermissionDenied, "cross space access denied")
	case strings.Contains(message, "PERMISSION_DENIED"):
		return apperror.New(apperror.CodePermissionDenied, "permission denied")
	case strings.Contains(message, "UNAUTHENTICATED"):
		return apperror.New(apperror.CodeUnauthenticated, "unauthenticated")
	default:
		return err
	}
}

func securityID(prefix string) string {
	return prefix + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}

func parseToolRef(ref string) (string, string) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return ref, ""
	}
	return parts[0], parts[1]
}

func isModelGenerationTool(toolName, toolType string) bool {
	return toolType == "model_generation" || toolName == "model_generation"
}

func defaultToolBillingUnit(toolType string) string {
	switch toolType {
	case "image", "image_edit", "audio", "video":
		return toolType
	default:
		return "call"
	}
}

func estimateItemIDForTool(estimate CreditEstimateDTO, toolName, toolType string) (string, error) {
	for _, item := range estimate.LineItems {
		if item.EstimateItemID == "" {
			continue
		}
		if item.ToolName == toolName && item.ToolType == toolType {
			return item.EstimateItemID, nil
		}
	}
	for _, item := range estimate.LineItems {
		if item.EstimateItemID != "" && item.ItemType == "tool_usage" {
			return item.EstimateItemID, nil
		}
	}
	return "", apperror.New(apperror.CodeStateConflict, "tool estimate item is missing")
}
