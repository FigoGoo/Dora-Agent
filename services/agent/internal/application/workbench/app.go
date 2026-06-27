package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/apperror"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
	runtimeeino "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/modeltool"
	runtimesafety "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/safety"
	runtimeskill "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/skill"
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
	CheckToolExecutionPolicy(ctx context.Context, auth AuthContextDTO, toolName string, toolType string, projectID string, riskContext map[string]string, traceID string) (ToolExecutionPolicyDTO, error)
	ResolveDefaultModel(ctx context.Context, auth AuthContextDTO, resourceType string, traceID string) (ModelSummaryDTO, error)
	ResolveGenerationModelSnapshot(ctx context.Context, auth AuthContextDTO, resourceType string, modelID string, pricingSnapshotID string, traceID string) (ModelRuntimeSnapshotDTO, error)
	ListAssetElementTypes(ctx context.Context, auth AuthContextDTO, pageSize int, schemaVersion string, traceID string) ([]AssetElementTypeDTO, string, error)
	SaveSkillTestResult(ctx context.Context, auth AuthContextDTO, req SkillTestResultRequest, traceID string) (SkillTestResultDTO, error)
	BatchCheckAssetAccess(ctx context.Context, auth AuthContextDTO, req BatchCheckAssetAccessRequest, traceID string) ([]AssetAccessResultDTO, error)
	EstimateGenerationCredits(ctx context.Context, auth AuthContextDTO, req EstimateGenerationCreditsRequest, traceID string) (CreditEstimateDTO, error)
	FreezeCredits(ctx context.Context, auth AuthContextDTO, req FreezeCreditsRequest, traceID string) (FreezeCreditsDTO, error)
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
	ModelID            string
	DisplayName        string
	ResourceType       string
	PricingSnapshotID  string
	ProviderRuntimeRef string
	TimeoutMS          int32
	RetryPolicy        map[string]string
	RuntimeParameters  map[string]string
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

type App struct {
	repo            *repository.Repository
	gateway         BusinessGateway
	configVersion   string
	safetyEvaluator runtimesafety.Evaluator
	skillRouter     runtimeskill.Router
	toolChecker     runtimetool.PolicyChecker
	modelAdapter    modeltool.Adapter
	turnLoop        turnloop.TurnLoop
}

func New(repo *repository.Repository, gateway BusinessGateway, configVersion string) *App {
	if configVersion == "" {
		configVersion = "local-dev"
	}
	return &App{
		repo:            repo,
		gateway:         gateway,
		configVersion:   configVersion,
		safetyEvaluator: runtimesafety.NewEvaluator(nil),
		skillRouter:     runtimeskill.NewRouter(),
		toolChecker:     runtimetool.NewPolicyChecker(),
		modelAdapter:    modeltool.LocalAdapter{},
		turnLoop:        turnloop.New(),
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

type CreateSessionRequest struct {
	ProjectID      string `json:"project_id"`
	InitialTitle   string `json:"initial_title"`
	IdempotencyKey string `json:"idempotency_key"`
}

type CreateRunRequest struct {
	SessionID        string              `json:"session_id"`
	ProjectID        string              `json:"project_id"`
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
	EventID     string         `json:"event_id"`
	Type        string         `json:"type"`
	SessionID   string         `json:"session_id"`
	RunID       string         `json:"run_id"`
	ProjectID   string         `json:"project_id"`
	SpaceID     string         `json:"space_id"`
	ActorUserID string         `json:"actor_user_id"`
	Sequence    int64          `json:"sequence"`
	Timestamp   time.Time      `json:"timestamp"`
	Component   string         `json:"component"`
	TraceID     string         `json:"trace_id"`
	Payload     map[string]any `json:"payload"`
}

type SnapshotResponse struct {
	Session           SessionDTO     `json:"session"`
	Run               *RunDTO        `json:"run"`
	Messages          []MessageDTO   `json:"messages"`
	Assets            []any          `json:"assets"`
	Blackboard        map[string]any `json:"blackboard"`
	Tasks             []any          `json:"tasks"`
	LastEventSequence int64          `json:"last_event_sequence"`
	ReadonlyReason    string         `json:"readonly_reason,omitempty"`
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
	if req.UserInput.ClientMessageID == "" || req.UserInput.ContentType == "" || strings.TrimSpace(req.UserInput.Text) == "" {
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
	runID := securityID("run_")
	run := &model.Run{
		ID: runID, SessionID: session.ID, ProjectID: session.ProjectID, SpaceID: session.SpaceID, UserID: session.UserID,
		TurnNo: 1, Status: state.RunStatusPending, InputSummary: jsonObject(runInputSummary(req)),
		ModelSelectionSnapshot: jsonObject(req.ModelSelection), RuntimeConfigVersion: a.configVersion, IdempotencyKey: req.IdempotencyKey, TraceID: traceID,
	}
	if err := a.repo.CreateRun(ctx, run); err != nil {
		return CreateRunResponse{}, err
	}
	message := &model.Message{
		ID: securityID("msg_"), SessionID: session.ID, RunID: run.ID, Role: "user", ContentType: req.UserInput.ContentType,
		Content: req.UserInput.Text, Sequence: 1, TraceID: traceID, Metadata: jsonObject(map[string]any{
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
	if err := a.recordM3StartEvents(ctx, auth, run, req.UserInput.Text, traceID); err != nil {
		return CreateRunResponse{}, err
	}
	if updated, err := a.repo.GetRun(ctx, run.ID); err == nil {
		return runResponse(*updated), nil
	}
	return runResponse(*run), nil
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
		return RunDTO{}, mapBusinessError(err)
	}
	if err := ensureCreativeProjectAccess(access); err != nil {
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
		"payload_digest":  req.ConfirmedPayloadDigest,
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
	if err := a.runM4ConfirmedGeneration(ctx, auth, run.ID, interrupt, req.IdempotencyKey, traceID); err != nil {
		return RunDTO{}, err
	}
	updated, err := a.repo.GetRun(ctx, run.ID)
	if err != nil {
		return RunDTO{}, err
	}
	return runDTO(*updated), nil
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
		if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "INTERRUPT_REJECTED", req.ReasonCode); err != nil {
			return RunDTO{}, err
		}
	}
	_ = a.appendRunEvent(ctx, run, "confirmation.rejected", traceID, map[string]any{
		"confirmation_id": interrupt.ID,
		"interrupt_id":    interrupt.ID,
		"action":          "reject",
		"rejected_at":     time.Now().UTC().Format(time.RFC3339Nano),
		"reason_code":     req.ReasonCode,
		"run_status":      state.RunStatusFailed,
		"idempotency_key": req.IdempotencyKey,
	})
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
		return EventReplayResponse{}, apperror.New(apperror.CodeResourceNotFound, "run not found")
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
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	items := make([]EventDTO, 0, len(rows))
	next := afterSequence
	for _, row := range rows {
		items = append(items, eventDTO(row))
		next = row.Sequence
	}
	return EventReplayResponse{Events: items, NextSequence: next, HasMore: hasMore}, nil
}

func (a *App) BuildRunSnapshot(ctx context.Context, auth AuthContextDTO, runID string, traceID string) (SnapshotResponse, error) {
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return SnapshotResponse{}, apperror.New(apperror.CodeResourceNotFound, "run not found")
	}
	return a.buildSnapshot(ctx, auth, run.SessionID, run, traceID)
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
	return SnapshotResponse{
		Session: sessionDTO(*session), Run: runDTO, Messages: messageDTOs, Assets: []any{}, Blackboard: map[string]any{}, Tasks: []any{},
		LastEventSequence: session.LastEventSequence, ReadonlyReason: readonly,
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
		return nil, nil, mapBusinessError(err)
	}
	if purpose == businessagent.ProjectAccessPurpose_CONTINUE_CREATION {
		if err := ensureCreativeProjectAccess(access); err != nil {
			return nil, nil, err
		}
	} else if err := ensureViewProjectAccess(access); err != nil {
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
	safetyEvidence, err := a.recordPromptSafetyEvaluation(ctx, run, "generation", "run", run.ID, prompt, traceID)
	if err != nil {
		return err
	}
	selectedSkillID := ""
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
				skillSelectionSnapshot["confirmation_policy"] = spec.ConfirmationPolicyJSON
				skillSelectionSnapshot["tool_refs_digest"] = digestStrings(spec.ToolRefs)
				skillSelectionSnapshot["tool_refs_count"] = len(spec.ToolRefs)
				if err := a.recordToolPolicyEvents(ctx, auth, run, spec.ToolRefs, traceID); err != nil {
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
			if err := a.createCreditConfirmationInterrupt(ctx, run, snapshot, estimate, safetyEvidence, prompt, traceID); err != nil {
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

func (a *App) recordToolPolicyEvents(ctx context.Context, auth AuthContextDTO, run *model.Run, toolRefs []string, traceID string) error {
	for _, ref := range toolRefs {
		toolName, toolType := parseToolRef(ref)
		if toolName == "" || toolType == "" {
			continue
		}
		toolCallID := securityID("tool_")
		policy, err := a.gateway.CheckToolExecutionPolicy(ctx, auth, toolName, toolType, run.ProjectID, map[string]string{"source": "m3_start_turn"}, traceID)
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
			if err := a.createToolConfirmationInterrupt(ctx, run, toolName, toolType, policy, traceID); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}

func (a *App) createToolConfirmationInterrupt(ctx context.Context, run *model.Run, toolName, toolType string, policy ToolExecutionPolicyDTO, traceID string) error {
	return a.createConfirmationInterrupt(ctx, run, "risk_confirmation", "high risk tool requires confirmation", map[string]any{
		"tool_name": toolName, "tool_type": toolType, "risk_level": policy.RiskLevel,
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

func (a *App) createCreditConfirmationInterrupt(ctx context.Context, run *model.Run, snapshot ModelRuntimeSnapshotDTO, estimate CreditEstimateDTO, safety *model.SafetyEvaluation, prompt string, traceID string) error {
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
	}, "生成与扣费确认", "确认后将冻结积分，生成完成并保存资产后扣费", []string{"credit_freeze", "asset_commit", "project:" + run.ProjectID}, 15*time.Minute, traceID)
}

type m4ConfirmationPayload struct {
	M4Flow            string                          `json:"m4_flow"`
	EstimateID        string                          `json:"estimate_id"`
	EstimatePoints    int64                           `json:"estimate_points"`
	CreditAccountID   string                          `json:"credit_account_id"`
	PricingSnapshotID string                          `json:"pricing_snapshot_id"`
	ModelSnapshot     ModelRuntimeSnapshotDTO         `json:"model_snapshot"`
	Estimate          CreditEstimateDTO               `json:"estimate"`
	SafetyEvidence    businessagent.SafetyEvidenceDTO `json:"safety_evidence"`
	PromptDigest      string                          `json:"prompt_digest"`
}

func (a *App) runM4ConfirmedGeneration(ctx context.Context, auth AuthContextDTO, runID string, interrupt *model.Interrupt, idempotencyKey string, traceID string) error {
	payload, ok := parseM4ConfirmationPayload(interrupt)
	if !ok {
		return nil
	}
	if a.gateway == nil {
		return apperror.New(apperror.CodeNotImplemented, "business gateway is not configured")
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
		return mapBusinessError(err)
	}
	if err := ensureCreativeProjectAccess(access); err != nil {
		return err
	}
	freeze, err := a.gateway.FreezeCredits(ctx, auth, FreezeCreditsRequest{
		EstimateID: payload.EstimateID, Points: payload.EstimatePoints, RunID: run.ID,
		ConfirmationID: interrupt.ID, AccountID: payload.CreditAccountID, IdempotencyKey: idempotencyKey + ":freeze",
	}, traceID)
	if err != nil {
		_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "CREDIT_FREEZE_FAILED", "credit freeze failed")
		_ = a.appendRunEvent(ctx, run, "agent.run.failed", traceID, map[string]any{
			"error_type": "business_rpc", "error_code": "CREDIT_FREEZE_FAILED", "user_message": "积分冻结失败",
			"retryable": true, "support_trace_id": traceID,
		})
		return mapBusinessError(err)
	}
	_ = a.repo.DB().WithContext(ctx).Model(&model.Run{}).Where("id = ?", run.ID).Update("model_selection_snapshot", jsonObject(map[string]any{
		"model_snapshot": payload.ModelSnapshot, "estimate_id": payload.EstimateID, "freeze_id": freeze.FreezeID,
		"credit_account_id": payload.CreditAccountID, "pricing_snapshot_id": payload.PricingSnapshotID,
	}))
	_ = a.appendRunEvent(ctx, run, "credits.frozen", traceID, map[string]any{
		"freeze_id": freeze.FreezeID, "frozen_points": freeze.FrozenPoints, "expires_at": freeze.ExpiresAt,
		"estimate_id": payload.EstimateID, "credit_account_id": payload.CreditAccountID,
	})
	prompt, err := a.latestUserPrompt(ctx, run.SessionID)
	if err != nil {
		return a.failM4AfterFreeze(ctx, auth, run, freeze, "prompt_unavailable", idempotencyKey, traceID, err)
	}
	_ = a.appendGenerationProgress(ctx, run, traceID, payload.ModelSnapshot.ResourceType, "submitted", 20, false, map[string]any{
		"model_id": payload.ModelSnapshot.ModelID, "estimate_id": payload.EstimateID, "freeze_id": freeze.FreezeID,
	})
	result, err := a.modelAdapter.Generate(ctx, modeltool.Snapshot{
		ModelID: payload.ModelSnapshot.ModelID, ResourceType: payload.ModelSnapshot.ResourceType,
		ProviderRuntimeRef: payload.ModelSnapshot.ProviderRuntimeRef, TimeoutMS: payload.ModelSnapshot.TimeoutMS,
	}, runtimeeino.UserPrompt(prompt))
	if err != nil {
		return a.failM4AfterFreeze(ctx, auth, run, freeze, "generation_failed", idempotencyKey, traceID, err)
	}
	if len(result.Artifacts) == 0 {
		return a.failM4AfterFreeze(ctx, auth, run, freeze, "generation_empty", idempotencyKey, traceID, apperror.New(apperror.CodeInternal, "generation produced no artifact"))
	}
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
		return a.failM4AfterFreeze(ctx, auth, run, freeze, "prepare_asset_slots_failed", idempotencyKey, traceID, err)
	}
	slotByArtifact := map[string]GeneratedUploadSlotDTO{}
	for _, slot := range slots {
		slotByArtifact[slot.ArtifactID] = slot
	}
	commitArtifacts := make([]CommitArtifactDTO, 0, len(result.Artifacts))
	finalElements := make([]FinalElementDTO, 0, len(result.Artifacts))
	for i, artifact := range result.Artifacts {
		slot, ok := slotByArtifact[artifact.ArtifactID]
		if !ok {
			return a.failM4AfterFreeze(ctx, auth, run, freeze, "missing_upload_slot", idempotencyKey, traceID, apperror.New(apperror.CodeInternal, "generated upload slot missing"))
		}
		_ = a.appendRunEvent(ctx, run, "asset.save.started", traceID, map[string]any{
			"artifact_id": artifact.ArtifactID, "resource_type": artifact.ResourceType, "project_id": run.ProjectID,
			"freeze_id": freeze.FreezeID, "estimate_id": payload.EstimateID,
		})
		estimateItemID := estimateItemForArtifact(payload.Estimate, artifact.ResourceType)
		commitArtifacts = append(commitArtifacts, CommitArtifactDTO{
			ArtifactID: artifact.ArtifactID, ResourceType: artifact.ResourceType, ElementType: artifact.ElementType,
			ArtifactSummary: artifact.MetadataSummary, ContentURIDigest: digestText(slot.ObjectKey),
			EstimateItemID: estimateItemID, ToolName: "model_generation", ToolType: artifact.ResourceType, ChargeQuantity: 1,
			MetadataSummary: artifact.MetadataSummary,
			StorageObjectRef: CommitStorageObjectRefDTO{
				ObjectKey: slot.ObjectKey, Bucket: slot.Bucket, ContentType: artifact.ContentType,
				SizeBytes: artifact.SizeBytes, Checksum: artifact.Checksum, Etag: "local-" + artifact.ArtifactID,
			},
		})
		elementPayload, _ := json.Marshal(map[string]any{"artifact_id": artifact.ArtifactID, "resource_type": artifact.ResourceType, "elements_summary": artifact.ElementsSummary})
		finalElements = append(finalElements, FinalElementDTO{ElementType: artifact.ElementType, ElementPayloadJSON: string(elementPayload), DisplayOrder: int32(i + 1)})
	}
	commit, err := a.gateway.CommitGeneratedAssetAndCharge(ctx, auth, CommitGeneratedAssetAndChargeRequest{
		ProjectID: run.ProjectID, SessionID: run.SessionID, RunID: run.ID, FreezeID: freeze.FreezeID,
		EstimateID: payload.EstimateID, Artifacts: commitArtifacts, FinalElements: finalElements,
		SafetyEvidence: &payload.SafetyEvidence, IdempotencyKey: idempotencyKey + ":commit",
	}, traceID)
	if err != nil {
		_ = a.appendRunEvent(ctx, run, "asset.save.failed", traceID, map[string]any{
			"artifact_id": firstArtifactID(result.Artifacts), "resource_type": payload.ModelSnapshot.ResourceType,
			"error_code": "ASSET_COMMIT_FAILED", "user_message": "资产保存失败", "retryable": true, "support_trace_id": traceID,
		})
		return a.failM4AfterFreeze(ctx, auth, run, freeze, "asset_commit_failed", idempotencyKey, traceID, err)
	}
	for _, ref := range commit.AssetRefs {
		if err := a.repo.CreateArtifact(ctx, &model.Artifact{
			ID: securityID("artref_"), SessionID: run.SessionID, ProjectID: run.ProjectID, RunID: run.ID,
			ArtifactType: "generated_asset", Status: "saved", ElementType: elementTypeForRef(ref, result.Artifacts),
			Content: jsonObject(map[string]any{
				"source_artifact_id": ref.SourceArtifactID, "resource_type": ref.ResourceType, "asset_type": ref.AssetType,
				"elements_summary_json": ref.ElementsSummaryJSON,
			}),
			BusinessRefID: ref.AssetID, Visibility: "private", TraceID: traceID,
		}); err != nil {
			return a.failM4AfterFreeze(ctx, auth, run, freeze, "agent_artifact_ref_failed", idempotencyKey, traceID, err)
		}
		_ = a.appendRunEvent(ctx, run, "asset.save.completed", traceID, map[string]any{
			"asset_id": ref.AssetID, "artifact_id": ref.SourceArtifactID, "resource_type": ref.ResourceType,
			"save_status": ref.Status, "elements": []any{map[string]any{"element_type": elementTypeForRef(ref, result.Artifacts)}}, "downloadable": true,
			"preview_url": ref.PreviewURL,
		})
	}
	_ = a.appendRunEvent(ctx, run, "credits.charged", traceID, map[string]any{
		"charged_points": commit.ChargedPoints, "released_points": commit.ReleasedPoints, "ledger_ref": commit.LedgerRef,
		"charged_line_items": commit.ChargedLineItems,
	})
	if commit.ReleasedPoints > 0 {
		_ = a.appendRunEvent(ctx, run, "credits.released", traceID, map[string]any{
			"freeze_id": freeze.FreezeID, "released_points": commit.ReleasedPoints, "reason": "unused_after_asset_commit",
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
		"freeze_id": freeze.FreezeID, "estimate_id": payload.EstimateID,
	})
	sequence, err := a.repo.NextMessageSequence(ctx, run.SessionID)
	if err != nil {
		return err
	}
	finalMessage := &model.Message{
		ID: securityID("msg_"), SessionID: run.SessionID, RunID: run.ID, Role: "assistant", ContentType: "text/plain",
		Content: "生成完成，资产已保存。", Sequence: sequence, TraceID: traceID,
		Metadata: jsonObject(map[string]any{"asset_count": len(assets), "charged_points": commit.ChargedPoints}),
	}
	if err := a.repo.CreateMessage(ctx, finalMessage); err != nil {
		return err
	}
	if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusCompleted, "", ""); err != nil {
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
	return a.repo.ResolveInterrupt(ctx, interrupt.ID, state.InterruptStatusResolved)
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
	interrupt := &model.Interrupt{
		ID: interruptID, RunID: run.ID, InterruptType: interruptType, Status: state.InterruptStatusRequired,
		Reason: reason, ConfirmationPayload: jsonObject(confirmationPayload), AllowedActions: jsonObject([]string{"confirm", "reject"}),
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
	return a.appendRunEvent(ctx, run, "confirmation.required", traceID, map[string]any{
		"confirmation_id": interruptID, "interrupt_id": interruptID, "title": title, "summary": summary,
		"risks": risks, "points": points, "expires_at": expiresAt.Format(time.RFC3339Nano), "actions": []string{"confirm", "reject"},
		"confirmation_payload": confirmationPayload,
	})
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

func estimateItemForArtifact(estimate CreditEstimateDTO, resourceType string) string {
	for _, item := range estimate.LineItems {
		if item.ItemType == "model_generation" && (item.ResourceType == "" || item.ResourceType == resourceType) {
			return item.EstimateItemID
		}
	}
	if len(estimate.LineItems) > 0 {
		return estimate.LineItems[0].EstimateItemID
	}
	return ""
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
	if strings.TrimSpace(reason) == "" {
		reason = "no_published_skill"
	}
	if strings.TrimSpace(message) == "" {
		message = "未命中可路由 Skill，使用文本模型兜底"
	}
	return a.appendRunEvent(ctx, run, "agent.skill.missing", traceID, map[string]any{
		"fallback_mode": "text_model", "matched_tags": []string{}, "user_message": message, "reason": reason,
	})
}

func validateRunInputs(req CreateRunRequest) error {
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

func eventDTO(event model.Event) EventDTO {
	payload := map[string]any{}
	if len(event.Payload) > 0 {
		_ = json.Unmarshal(event.Payload, &payload)
	}
	return EventDTO{
		EventID: event.EventID, Type: event.Type, SessionID: event.SessionID, RunID: event.RunID, ProjectID: event.ProjectID,
		SpaceID: event.SpaceID, ActorUserID: event.ActorUserID, Sequence: event.Sequence, Timestamp: event.CreatedAt,
		Component: event.Component, TraceID: event.TraceID, Payload: payload,
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
