package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/model"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/domain/state"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInvalidStateTransition = errors.New("invalid agent state transition")
var ErrInvalidSafetyEvidence = errors.New("invalid agent safety evidence")

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) DB() *gorm.DB {
	return r.db
}

func (r *Repository) CreateSession(ctx context.Context, session *model.Session) error {
	normalizeSession(session)
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *Repository) GetSession(ctx context.Context, id string) (*model.Session, error) {
	var session model.Session
	if err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *Repository) GetSessionByIdempotencyKey(ctx context.Context, key string) (*model.Session, error) {
	var session model.Session
	if err := r.db.WithContext(ctx).Where("idempotency_key = ? AND deleted_at IS NULL", key).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *Repository) ListSessions(ctx context.Context, spaceID, projectID, userID string, limit, offset int) ([]model.Session, error) {
	limit = normalizeLimit(limit, 10, 100)
	var sessions []model.Session
	err := r.db.WithContext(ctx).
		Where("space_id = ? AND project_id = ? AND user_id = ? AND deleted_at IS NULL", spaceID, projectID, userID).
		Order("updated_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&sessions).Error
	return sessions, err
}

func (r *Repository) UpdateSessionSnapshot(ctx context.Context, id string, lastRunID string, lastSequence int64, summary datatypes.JSON) error {
	if len(summary) == 0 {
		summary = jsonObject()
	}
	return r.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{
			"last_run_id":         lastRunID,
			"last_event_sequence": lastSequence,
			"snapshot_summary":    summary,
			"updated_at":          time.Now().UTC(),
		}).Error
}

func (r *Repository) CreateRun(ctx context.Context, run *model.Run) error {
	normalizeRun(run)
	return r.db.WithContext(ctx).Create(run).Error
}

func (r *Repository) CountActiveRuns(ctx context.Context, sessionID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.Run{}).
		Where("session_id = ? AND status IN ? AND deleted_at IS NULL", sessionID, []string{
			state.RunStatusPending,
			state.RunStatusRunning,
			state.RunStatusWaitingConfirmation,
			state.RunStatusResuming,
		}).
		Count(&count).Error
	return count, err
}

func (r *Repository) GetRun(ctx context.Context, id string) (*model.Run, error) {
	var run model.Run
	if err := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *Repository) GetRunByIdempotencyKey(ctx context.Context, key string) (*model.Run, error) {
	var run model.Run
	if err := r.db.WithContext(ctx).Where("idempotency_key = ? AND deleted_at IS NULL", key).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *Repository) CreateMessage(ctx context.Context, message *model.Message) error {
	normalizeMessage(message)
	return r.db.WithContext(ctx).Create(message).Error
}

func (r *Repository) ListMessages(ctx context.Context, sessionID string, limit, offset int) ([]model.Message, error) {
	limit = normalizeLimit(limit, 10, 100)
	var messages []model.Message
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND deleted_at IS NULL", sessionID).
		Order("sequence ASC").
		Limit(limit).
		Offset(offset).
		Find(&messages).Error
	return messages, err
}

func (r *Repository) UpdateRunStatus(ctx context.Context, id, toStatus, errorCode, errorMessage string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND deleted_at IS NULL", id).
			First(&run).Error; err != nil {
			return err
		}
		if !state.CanTransitionRun(run.Status, toStatus) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidStateTransition, run.Status, toStatus)
		}
		updates := map[string]any{
			"status":        toStatus,
			"error_code":    errorCode,
			"error_message": errorMessage,
			"updated_at":    time.Now().UTC(),
		}
		now := time.Now().UTC()
		if toStatus == state.RunStatusRunning && run.StartedAt == nil {
			updates["started_at"] = now
		}
		if toStatus == state.RunStatusCompleted || toStatus == state.RunStatusFailed || toStatus == state.RunStatusCancelled {
			updates["completed_at"] = now
		}
		return tx.Model(&model.Run{}).Where("id = ?", id).Updates(updates).Error
	})
}

func (r *Repository) AppendEvent(ctx context.Context, event *model.Event) error {
	normalizeEvent(event)
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(event).Error; err != nil {
			return err
		}
		return tx.Model(&model.Session{}).
			Where("id = ? AND deleted_at IS NULL", event.SessionID).
			Updates(map[string]any{
				"last_run_id":         event.RunID,
				"last_event_sequence": event.Sequence,
				"updated_at":          event.CreatedAt,
			}).Error
	})
}

func (r *Repository) ListEventsAfterSequence(ctx context.Context, runID string, afterSequence int64, limit int) ([]model.Event, error) {
	limit = normalizeLimit(limit, 10, 200)
	var events []model.Event
	err := r.db.WithContext(ctx).
		Where("run_id = ? AND sequence > ?", runID, afterSequence).
		Order("sequence ASC").
		Limit(limit).
		Find(&events).Error
	return events, err
}

func (r *Repository) CreateInterrupt(ctx context.Context, interrupt *model.Interrupt) error {
	normalizeInterrupt(interrupt)
	return r.db.WithContext(ctx).Create(interrupt).Error
}

func (r *Repository) GetRequiredInterrupt(ctx context.Context, runID string) (*model.Interrupt, error) {
	var interrupt model.Interrupt
	if err := r.db.WithContext(ctx).
		Where("run_id = ? AND status = ? AND deleted_at IS NULL", runID, state.InterruptStatusRequired).
		Order("created_at DESC").
		First(&interrupt).Error; err != nil {
		return nil, err
	}
	return &interrupt, nil
}

func (r *Repository) ResolveInterrupt(ctx context.Context, id, toStatus string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var interrupt model.Interrupt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND deleted_at IS NULL", id).
			First(&interrupt).Error; err != nil {
			return err
		}
		if !state.CanTransitionInterrupt(interrupt.Status, toStatus) {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidStateTransition, interrupt.Status, toStatus)
		}
		now := time.Now().UTC()
		updates := map[string]any{
			"status":     toStatus,
			"updated_at": now,
		}
		if toStatus == state.InterruptStatusRejected || toStatus == state.InterruptStatusExpired || toStatus == state.InterruptStatusResolved {
			updates["resolved_at"] = now
		}
		return tx.Model(&model.Interrupt{}).Where("id = ?", id).Updates(updates).Error
	})
}

func (r *Repository) CreateArtifact(ctx context.Context, artifact *model.Artifact) error {
	normalizeArtifact(artifact)
	return r.db.WithContext(ctx).Create(artifact).Error
}

func (r *Repository) ListArtifacts(ctx context.Context, sessionID string, limit, offset int) ([]model.Artifact, error) {
	limit = normalizeLimit(limit, 10, 100)
	var artifacts []model.Artifact
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND deleted_at IS NULL", sessionID).
		Order("updated_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&artifacts).Error
	return artifacts, err
}

func (r *Repository) CreateSafetyEvaluation(ctx context.Context, safety *model.SafetyEvaluation) error {
	normalizeSafety(safety)
	if !state.IsValidSafetyResult(safety.Result) {
		return fmt.Errorf("%w: result=%s", ErrInvalidSafetyEvidence, safety.Result)
	}
	return r.db.WithContext(ctx).Create(safety).Error
}

func (r *Repository) GetSafetyEvaluation(ctx context.Context, id string) (*model.SafetyEvaluation, error) {
	var safety model.SafetyEvaluation
	if err := r.db.WithContext(ctx).Where("safety_evidence_id = ? AND deleted_at IS NULL", id).First(&safety).Error; err != nil {
		return nil, err
	}
	return &safety, nil
}

func (r *Repository) UpsertRuntimeConfig(ctx context.Context, runtimeConfig *model.RuntimeConfig) error {
	normalizeRuntimeConfig(runtimeConfig)
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "config_key"}, {Name: "version"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"status",
			"owner",
			"content",
			"safe_config_refs",
			"activated_at",
			"deprecated_at",
			"updated_at",
		}),
	}).Create(runtimeConfig).Error
}

func (r *Repository) GetActiveRuntimeConfig(ctx context.Context, key string) (*model.RuntimeConfig, error) {
	var runtimeConfig model.RuntimeConfig
	if err := r.db.WithContext(ctx).
		Where("config_key = ? AND status = ?", key, "active").
		Order("activated_at DESC NULLS LAST, updated_at DESC").
		First(&runtimeConfig).Error; err != nil {
		return nil, err
	}
	return &runtimeConfig, nil
}

func normalizeSession(session *model.Session) {
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = now
	}
	if session.Status == "" {
		session.Status = state.SessionStatusActive
	}
	if len(session.SnapshotSummary) == 0 {
		session.SnapshotSummary = jsonObject()
	}
}

func normalizeMessage(message *model.Message) {
	now := time.Now().UTC()
	if message.CreatedAt.IsZero() {
		message.CreatedAt = now
	}
	if message.UpdatedAt.IsZero() {
		message.UpdatedAt = now
	}
	if message.ContentType == "" {
		message.ContentType = "text"
	}
	if message.SafetyStatus == "" {
		message.SafetyStatus = "not_evaluated"
	}
	if len(message.ContentSummary) == 0 {
		message.ContentSummary = jsonObject()
	}
	if len(message.Metadata) == 0 {
		message.Metadata = jsonObject()
	}
}

func normalizeRun(run *model.Run) {
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}
	if run.Status == "" {
		run.Status = state.RunStatusPending
	}
	if len(run.InputSummary) == 0 {
		run.InputSummary = jsonObject()
	}
	if len(run.SkillSelection) == 0 {
		run.SkillSelection = jsonObject()
	}
	if len(run.ModelSelectionSnapshot) == 0 {
		run.ModelSelectionSnapshot = jsonObject()
	}
}

func normalizeEvent(event *model.Event) {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if len(event.Payload) == 0 {
		event.Payload = jsonObject()
	}
}

func normalizeInterrupt(interrupt *model.Interrupt) {
	now := time.Now().UTC()
	if interrupt.CreatedAt.IsZero() {
		interrupt.CreatedAt = now
	}
	if interrupt.UpdatedAt.IsZero() {
		interrupt.UpdatedAt = now
	}
	if interrupt.Status == "" {
		interrupt.Status = state.InterruptStatusRequired
	}
	if len(interrupt.ConfirmationPayload) == 0 {
		interrupt.ConfirmationPayload = jsonObject()
	}
	if len(interrupt.AllowedActions) == 0 {
		interrupt.AllowedActions = jsonArray()
	}
	if len(interrupt.ResumeContext) == 0 {
		interrupt.ResumeContext = jsonObject()
	}
}

func normalizeArtifact(artifact *model.Artifact) {
	now := time.Now().UTC()
	if artifact.CreatedAt.IsZero() {
		artifact.CreatedAt = now
	}
	if artifact.UpdatedAt.IsZero() {
		artifact.UpdatedAt = now
	}
	if artifact.Version == 0 {
		artifact.Version = 1
	}
	if artifact.Visibility == "" {
		artifact.Visibility = "private"
	}
	if len(artifact.Content) == 0 {
		artifact.Content = jsonObject()
	}
}

func normalizeSafety(safety *model.SafetyEvaluation) {
	now := time.Now().UTC()
	if safety.EvaluatedAt.IsZero() {
		safety.EvaluatedAt = now
	}
	if safety.CreatedAt.IsZero() {
		safety.CreatedAt = now
	}
	if safety.UpdatedAt.IsZero() {
		safety.UpdatedAt = now
	}
}

func normalizeRuntimeConfig(runtimeConfig *model.RuntimeConfig) {
	now := time.Now().UTC()
	if runtimeConfig.CreatedAt.IsZero() {
		runtimeConfig.CreatedAt = now
	}
	if runtimeConfig.UpdatedAt.IsZero() {
		runtimeConfig.UpdatedAt = now
	}
	if len(runtimeConfig.Content) == 0 {
		runtimeConfig.Content = jsonObject()
	}
	if len(runtimeConfig.SafeConfigRefs) == 0 {
		runtimeConfig.SafeConfigRefs = jsonArray()
	}
}

func jsonObject() datatypes.JSON {
	return datatypes.JSON([]byte(`{}`))
}

func jsonArray() datatypes.JSON {
	return datatypes.JSON([]byte(`[]`))
}

func normalizeLimit(value, fallback, max int) int {
	if value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}
