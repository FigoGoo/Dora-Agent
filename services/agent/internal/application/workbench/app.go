package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/apperror"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/infra/repository"
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
	ElementType   string
	DisplayName   string
	Category      string
	SchemaVersion string
	Active        bool
	SortOrder     int32
}

type SkillTestResultRequest struct {
	SkillID            string
	VersionID          string
	TestRunID          string
	TestCaseID         string
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

type App struct {
	repo          *repository.Repository
	gateway       BusinessGateway
	configVersion string
}

func New(repo *repository.Repository, gateway BusinessGateway, configVersion string) *App {
	if configVersion == "" {
		configVersion = "local-dev"
	}
	return &App{repo: repo, gateway: gateway, configVersion: configVersion}
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
	EventID   string         `json:"event_id"`
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	Sequence  int64          `json:"sequence"`
	TraceID   string         `json:"trace_id"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
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

func (a *App) ListMessages(ctx context.Context, auth AuthContextDTO, sessionID string, limit, offset int) (ListMessagesResponse, error) {
	session, err := a.requireSession(ctx, auth, sessionID)
	if err != nil {
		return ListMessagesResponse{}, err
	}
	_ = session
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
	runID := securityID("run_")
	run := &model.Run{
		ID: runID, SessionID: session.ID, ProjectID: session.ProjectID, SpaceID: session.SpaceID, UserID: session.UserID,
		TurnNo: 1, Status: state.RunStatusPending, InputSummary: jsonObject(map[string]any{"client_message_id": req.UserInput.ClientMessageID}),
		ModelSelectionSnapshot: jsonObject(req.ModelSelection), RuntimeConfigVersion: a.configVersion, IdempotencyKey: req.IdempotencyKey, TraceID: traceID,
	}
	if err := a.repo.CreateRun(ctx, run); err != nil {
		return CreateRunResponse{}, err
	}
	message := &model.Message{
		ID: securityID("msg_"), SessionID: session.ID, RunID: run.ID, Role: "user", ContentType: req.UserInput.ContentType,
		Content: req.UserInput.Text, Sequence: 1, TraceID: traceID, Metadata: jsonObject(map[string]any{"client_message_id": req.UserInput.ClientMessageID}),
	}
	if err := a.repo.CreateMessage(ctx, message); err != nil {
		return CreateRunResponse{}, err
	}
	event := &model.Event{
		EventID: securityID("evt_"), Type: "agent.run.created", SessionID: session.ID, RunID: run.ID, ProjectID: session.ProjectID,
		SpaceID: session.SpaceID, ActorUserID: session.UserID, Sequence: 1, Component: "agent", Payload: jsonObject(map[string]any{"run_id": run.ID}),
		PayloadSchemaVersion: "2026-06-27", Visibility: "user", TraceID: traceID,
	}
	if err := a.repo.AppendEvent(ctx, event); err != nil {
		return CreateRunResponse{}, err
	}
	if err := a.recordM3StartEvents(ctx, auth, run, traceID); err != nil {
		return CreateRunResponse{}, err
	}
	if updated, err := a.repo.GetRun(ctx, run.ID); err == nil {
		return runResponse(*updated), nil
	}
	return runResponse(*run), nil
}

func (a *App) GetRun(ctx context.Context, auth AuthContextDTO, runID string) (RunDTO, error) {
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return RunDTO{}, apperror.New(apperror.CodeResourceNotFound, "run not found")
	}
	if _, err := a.requireSession(ctx, auth, run.SessionID); err != nil {
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
	_, err = a.gateway.CheckProjectAccess(ctx, auth, run.ProjectID, businessagent.ProjectAccessPurpose_VIEW, traceID)
	if err != nil {
		return RunDTO{}, mapBusinessError(err)
	}
	if run.Status == state.RunStatusCompleted || run.Status == state.RunStatusFailed || run.Status == state.RunStatusCancelled {
		return runDTO(*run), nil
	}
	if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusCancelled, "USER_CANCELLED", reason); err != nil {
		return RunDTO{}, err
	}
	event := &model.Event{
		EventID: securityID("evt_"), Type: "agent.run.cancelled", SessionID: session.ID, RunID: run.ID, ProjectID: run.ProjectID,
		SpaceID: run.SpaceID, ActorUserID: run.UserID, Sequence: session.LastEventSequence + 1, Component: "agent",
		Payload: jsonObject(map[string]any{"reason": reason}), PayloadSchemaVersion: "2026-06-27", Visibility: "user", TraceID: traceID,
	}
	_ = a.repo.AppendEvent(ctx, event)
	updated, _ := a.repo.GetRun(ctx, run.ID)
	return runDTO(*updated), nil
}

func (a *App) ReplayEvents(ctx context.Context, auth AuthContextDTO, runID string, afterSequence int64, limit int) (EventReplayResponse, error) {
	run, err := a.repo.GetRun(ctx, runID)
	if err != nil {
		return EventReplayResponse{}, apperror.New(apperror.CodeResourceNotFound, "run not found")
	}
	if _, err := a.requireSession(ctx, auth, run.SessionID); err != nil {
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
	if access, err := a.gateway.CheckProjectAccess(ctx, auth, session.ProjectID, businessagent.ProjectAccessPurpose_VIEW, traceID); err != nil {
		return SnapshotResponse{}, mapBusinessError(err)
	} else if access.ProjectStatus == "archived" {
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

func (a *App) recordM3StartEvents(ctx context.Context, auth AuthContextDTO, run *model.Run, traceID string) error {
	if a.gateway == nil {
		return nil
	}
	skills, _, err := a.gateway.ListRoutableSkills(ctx, auth, "", 10, "", traceID)
	if err != nil {
		_ = a.appendRunEvent(ctx, run, "skill.route.failed", traceID, map[string]any{"error": err.Error()})
	} else if len(skills) == 0 {
		_ = a.appendRunEvent(ctx, run, "skill.route.fallback", traceID, map[string]any{"reason": "no_published_skill_matched", "mode": "text_fallback"})
	} else {
		selected := skills[0]
		_ = a.appendRunEvent(ctx, run, "skill.route.matched", traceID, map[string]any{
			"skill_id": selected.SkillID, "skill_name": selected.SkillName, "version": selected.Version, "route_hints": selected.RouteHints,
		})
		spec, specErr := a.gateway.GetPublishedSkillSpec(ctx, auth, selected.SkillID, selected.Version, traceID)
		if specErr != nil {
			_ = a.appendRunEvent(ctx, run, "skill.spec.failed", traceID, map[string]any{"skill_id": selected.SkillID, "error": specErr.Error()})
		} else {
			if err := a.recordToolPolicyEvents(ctx, auth, run, spec.ToolRefs, traceID); err != nil {
				return err
			}
		}
	}
	modelSummary, err := a.gateway.ResolveDefaultModel(ctx, auth, "image", traceID)
	if err != nil {
		_ = a.appendRunEvent(ctx, run, "model.default.failed", traceID, map[string]any{"resource_type": "image", "error": err.Error()})
	} else {
		snapshot, snapErr := a.gateway.ResolveGenerationModelSnapshot(ctx, auth, modelSummary.ResourceType, modelSummary.ModelID, modelSummary.PricingSnapshotID, traceID)
		if snapErr != nil {
			_ = a.appendRunEvent(ctx, run, "model.snapshot.failed", traceID, map[string]any{"model_id": modelSummary.ModelID, "error": snapErr.Error()})
		} else {
			_ = a.repo.DB().WithContext(ctx).Model(&model.Run{}).Where("id = ?", run.ID).Update("model_selection_snapshot", jsonObject(snapshot))
			_ = a.appendRunEvent(ctx, run, "model.snapshot.resolved", traceID, map[string]any{
				"model_id": snapshot.ModelID, "resource_type": snapshot.ResourceType, "pricing_snapshot_id": snapshot.PricingSnapshotID,
				"provider_runtime_ref": snapshot.ProviderRuntimeRef, "timeout_ms": snapshot.TimeoutMS,
			})
		}
	}
	if types, version, err := a.gateway.ListAssetElementTypes(ctx, auth, 50, "", traceID); err == nil {
		_ = a.appendRunEvent(ctx, run, "platform.dictionary.loaded", traceID, map[string]any{"asset_element_type_count": len(types), "schema_version": version})
	}
	now := time.Now().UTC()
	safety := &model.SafetyEvaluation{
		SafetyEvidenceID: securityID("safety_"), Scene: "generation", TargetType: "run", TargetRefID: run.ID,
		EvaluatedObjectDigest: "sha256:local-m3:" + run.ID, PolicyVersion: "local-m3", EvidenceVersion: "2026-06-27",
		Result: state.SafetyResultPassed, SourceSessionID: run.SessionID, SourceRunID: run.ID, TraceID: traceID,
		EvaluatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}
	if err := a.repo.CreateSafetyEvaluation(ctx, safety); err == nil {
		_ = a.appendRunEvent(ctx, run, "safety.evaluation.passed", traceID, map[string]any{"safety_evidence_id": safety.SafetyEvidenceID, "result": safety.Result})
	}
	_ = a.appendRunEvent(ctx, run, "m4.asset_credit.deferred", traceID, map[string]any{"reason": "credit_asset_commit_belongs_to_m4", "status": "deferred_to_m4"})
	return nil
}

func (a *App) recordToolPolicyEvents(ctx context.Context, auth AuthContextDTO, run *model.Run, toolRefs []string, traceID string) error {
	for _, ref := range toolRefs {
		toolName, toolType := parseToolRef(ref)
		if toolName == "" || toolType == "" {
			continue
		}
		policy, err := a.gateway.CheckToolExecutionPolicy(ctx, auth, toolName, toolType, run.ProjectID, map[string]string{"source": "m3_start_turn"}, traceID)
		if err != nil {
			_ = a.appendRunEvent(ctx, run, "tool.policy.failed", traceID, map[string]any{"tool_ref": ref, "error": err.Error()})
			return mapBusinessError(err)
		}
		_ = a.appendRunEvent(ctx, run, "tool.policy.checked", traceID, map[string]any{
			"tool_name": toolName, "tool_type": toolType, "allowed": policy.Allowed, "risk_level": policy.RiskLevel,
			"requires_confirmation": policy.RequiresConfirmation, "timeout_ms": policy.TimeoutMS,
		})
		if !policy.Allowed {
			_ = a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusFailed, "TOOL_POLICY_DENIED", "required tool is disabled")
			return apperror.New(apperror.CodePermissionDenied, "required tool is disabled")
		}
		if policy.RequiresConfirmation {
			if err := a.createToolConfirmationInterrupt(ctx, run, toolName, toolType, policy, traceID); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}

func (a *App) createToolConfirmationInterrupt(ctx context.Context, run *model.Run, toolName, toolType string, policy ToolExecutionPolicyDTO, traceID string) error {
	if run.Status == state.RunStatusPending {
		if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusRunning, "", ""); err != nil {
			return err
		}
	}
	interruptID := securityID("intr_")
	interrupt := &model.Interrupt{
		ID: interruptID, RunID: run.ID, InterruptType: "risk_confirmation", Status: state.InterruptStatusRequired,
		Reason: "high risk tool requires confirmation",
		ConfirmationPayload: jsonObject(map[string]any{
			"confirmation_id": interruptID, "tool_name": toolName, "tool_type": toolType, "risk_level": policy.RiskLevel,
		}),
		AllowedActions: jsonObject([]string{"confirm", "reject"}),
		ResumeContext:  jsonObject(map[string]any{"next_step": "resume_turn", "source": "m3_tool_policy"}),
		ExpiresAt:      time.Now().UTC().Add(15 * time.Minute),
		TraceID:        traceID,
	}
	if err := a.repo.CreateInterrupt(ctx, interrupt); err != nil {
		return err
	}
	if err := a.repo.UpdateRunStatus(ctx, run.ID, state.RunStatusWaitingConfirmation, "", ""); err != nil {
		return err
	}
	run.Status = state.RunStatusWaitingConfirmation
	return a.appendRunEvent(ctx, run, "tool.confirmation.required", traceID, map[string]any{
		"interrupt_id": interruptID, "tool_name": toolName, "tool_type": toolType, "risk_level": policy.RiskLevel,
		"allowed_actions": []string{"confirm", "reject"},
	})
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
	return EventDTO{EventID: event.EventID, Type: event.Type, RunID: event.RunID, Sequence: event.Sequence, TraceID: event.TraceID, Payload: payload, CreatedAt: event.CreatedAt}
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
