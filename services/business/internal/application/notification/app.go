package notification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const systemOperator = "system"

type RequestMeta = accountspace.RequestMeta
type AuthContext = accountspace.AuthContext

type App struct {
	repo  *businesscore.Repository
	guard *idempotency.IdempotencyGuard
	now   func() time.Time
}

func New(repo *businesscore.Repository, guard *idempotency.IdempotencyGuard) *App {
	return &App{repo: repo, guard: guard, now: func() time.Time { return time.Now().UTC() }}
}

type CreateNotificationInput struct {
	RecipientUserID     string
	Type                string
	Title               string
	Summary             string
	Body                string
	RelatedResourceType string
	RelatedResourceID   string
	NavigationHint      map[string]any
	IdempotencyKey      string
	TraceID             string
}

type FailureInput struct {
	RecipientUserID     string
	Type                string
	RelatedResourceType string
	RelatedResourceID   string
	IdempotencyKey      string
	ErrorCode           string
	ErrorSummary        string
	TraceID             string
}

type ListInput struct {
	ReadStatus string
	Type       string
	Limit      int
	Offset     int
}

type NotificationDTO struct {
	NotificationID      string         `json:"notification_id"`
	Type                string         `json:"type"`
	Title               string         `json:"title"`
	Summary             string         `json:"summary"`
	Body                string         `json:"body,omitempty"`
	RelatedResourceType string         `json:"related_resource_type,omitempty"`
	RelatedResourceID   string         `json:"related_resource_id,omitempty"`
	NavigationHint      map[string]any `json:"navigation_hint"`
	ReadAt              *time.Time     `json:"read_at,omitempty"`
	CreatedAt           time.Time      `json:"created_at"`
}

type Page[T any] struct {
	Items  []T   `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

type UnreadCountDTO struct {
	UnreadCount int64 `json:"unread_count"`
}

type NotificationNavigationDTO struct {
	NotificationID   string `json:"notification_id"`
	Allowed          bool   `json:"allowed"`
	TargetRoute      string `json:"target_route,omitempty"`
	TargetResourceID string `json:"target_resource_id,omitempty"`
	TargetType       string `json:"target_type,omitempty"`
	DeniedReason     string `json:"denied_reason,omitempty"`
}

func (a *App) CreateNotification(ctx context.Context, in CreateNotificationInput) (NotificationDTO, error) {
	in.RecipientUserID = strings.TrimSpace(in.RecipientUserID)
	in.Type = strings.TrimSpace(in.Type)
	in.Title = strings.TrimSpace(in.Title)
	in.Summary = strings.TrimSpace(in.Summary)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.RecipientUserID == "" || in.Type == "" || in.Title == "" || in.Summary == "" || in.IdempotencyKey == "" {
		return NotificationDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "recipient_user_id, type, title, summary and idempotency_key are required")
	}
	if len(in.Title) > 160 || len(in.Summary) > 512 {
		return NotificationDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "notification title or summary is too long")
	}
	if err := a.ensureUserExists(ctx, in.RecipientUserID); err != nil {
		return NotificationDTO{}, err
	}
	var existing businesscore.Notification
	err := a.repo.DB().WithContext(ctx).Where("idempotency_key = ?", in.IdempotencyKey).First(&existing).Error
	if err == nil {
		if existing.RecipientUserID != in.RecipientUserID || existing.Type != in.Type {
			return NotificationDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "notification idempotency key conflicts with another request")
		}
		return notificationDTO(existing), nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return NotificationDTO{}, err
	}
	now := a.now()
	id := security.RandomID("ntf_")
	body := strings.TrimSpace(in.Body)
	if body == "" {
		body = in.Summary
	}
	relatedType := strings.TrimSpace(in.RelatedResourceType)
	relatedID := strings.TrimSpace(in.RelatedResourceID)
	row := businesscore.Notification{
		ID: id, NotificationID: id, NotificationNo: "N" + id[4:], RecipientUserID: in.RecipientUserID, NotificationType: in.Type,
		Type: in.Type, Title: in.Title, Summary: in.Summary, Body: optionalString(body), JumpType: defaultString(relatedType, "system"),
		JumpTargetID: optionalString(relatedID), JumpPayloadJSON: mustJSON(in.NavigationHint), SourceType: defaultString(relatedType, "system"),
		SourceID: optionalString(relatedID), Status: "unread", RelatedResourceType: optionalString(relatedType), RelatedResourceID: optionalString(relatedID),
		NavigationHintJSON: mustJSON(in.NavigationHint), IdempotencyKey: in.IdempotencyKey, TraceID: normalizeTrace(in.TraceID),
		CreatedBy: optionalString(systemOperator), UpdatedBy: optionalString(systemOperator), CreatedAt: now, UpdatedAt: now,
	}
	if err := a.repo.DB().WithContext(ctx).Create(&row).Error; err != nil {
		return NotificationDTO{}, err
	}
	return notificationDTO(row), nil
}

func (a *App) ListNotifications(ctx context.Context, auth AuthContext, in ListInput) (Page[NotificationDTO], error) {
	if auth.UserID == "" {
		return Page[NotificationDTO]{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	limit, offset := normalizePage(in.Limit, in.Offset)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.Notification{}).Where("recipient_user_id = ?", auth.UserID)
	switch strings.TrimSpace(in.ReadStatus) {
	case "unread":
		db = db.Where("read_at IS NULL")
	case "read":
		db = db.Where("read_at IS NOT NULL")
	}
	if strings.TrimSpace(in.Type) != "" {
		db = db.Where("type = ?", strings.TrimSpace(in.Type))
	}
	var rows []businesscore.Notification
	if err := db.Order("created_at DESC, id ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[NotificationDTO]{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[NotificationDTO]{}, err
	}
	items := make([]NotificationDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, notificationDTO(row))
	}
	return Page[NotificationDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) GetUnreadCount(ctx context.Context, auth AuthContext) (UnreadCountDTO, error) {
	if auth.UserID == "" {
		return UnreadCountDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	var count int64
	if err := a.repo.DB().WithContext(ctx).Model(&businesscore.Notification{}).
		Where("recipient_user_id = ? AND read_at IS NULL", auth.UserID).Count(&count).Error; err != nil {
		return UnreadCountDTO{}, err
	}
	return UnreadCountDTO{UnreadCount: count}, nil
}

func (a *App) MarkNotificationRead(ctx context.Context, auth AuthContext, meta RequestMeta, notificationID string) (NotificationDTO, error) {
	if auth.UserID == "" {
		return NotificationDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	hash := requestHash(meta, auth, map[string]any{"notification_id": notificationID})
	idempotencyKey := businessIdempotencyKey(meta, "notification.read", hash)
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "user:" + auth.UserID, SpaceID: auth.SpaceID, Scope: "notification.read", IdempotencyKey: idempotencyKey,
		RequestHash: hash, ActorUserID: auth.UserID, EnterpriseID: optionalString(auth.EnterpriseID),
	})
	if err != nil {
		return NotificationDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return NotificationDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "notification read idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.getNotification(ctx, auth.UserID, decision.ReplayResult.ID)
	}
	row, err := a.getNotificationRow(ctx, auth.UserID, notificationID)
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return NotificationDTO{}, err
	}
	if row.ReadAt == nil {
		now := a.now()
		if err := a.repo.DB().WithContext(ctx).Model(&businesscore.Notification{}).
			Where("notification_id = ? AND recipient_user_id = ?", notificationID, auth.UserID).
			Updates(map[string]any{"read_at": now, "updated_at": now, "updated_by": auth.UserID}).Error; err != nil {
			_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
			return NotificationDTO{}, err
		}
		row.ReadAt = &now
		row.UpdatedBy = optionalString(auth.UserID)
		row.UpdatedAt = now
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "notification", ID: row.NotificationID})
	return notificationDTO(row), nil
}

func (a *App) MarkAllNotificationsRead(ctx context.Context, auth AuthContext, meta RequestMeta, notificationType string) (UnreadCountDTO, error) {
	if auth.UserID == "" {
		return UnreadCountDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	hash := requestHash(meta, auth, map[string]any{"type": notificationType})
	idempotencyKey := businessIdempotencyKey(meta, "notification.read_all", hash)
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "user:" + auth.UserID, SpaceID: auth.SpaceID, Scope: "notification.read_all", IdempotencyKey: idempotencyKey,
		RequestHash: hash, ActorUserID: auth.UserID, EnterpriseID: optionalString(auth.EnterpriseID),
	})
	if err != nil {
		return UnreadCountDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return UnreadCountDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "notification read-all idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.GetUnreadCount(ctx, auth)
	}
	now := a.now()
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.Notification{}).
		Where("recipient_user_id = ? AND read_at IS NULL", auth.UserID)
	if strings.TrimSpace(notificationType) != "" {
		db = db.Where("type = ?", strings.TrimSpace(notificationType))
	}
	if err := db.Updates(map[string]any{"read_at": now, "updated_at": now, "updated_by": auth.UserID}).Error; err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return UnreadCountDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "notification_read_all", ID: auth.UserID})
	return a.GetUnreadCount(ctx, auth)
}

func (a *App) GetNotificationNavigation(ctx context.Context, auth AuthContext, notificationID string) (NotificationNavigationDTO, error) {
	row, err := a.getNotificationRow(ctx, auth.UserID, notificationID)
	if err != nil {
		return NotificationNavigationDTO{}, err
	}
	dto := notificationDTO(row)
	nav := NotificationNavigationDTO{NotificationID: row.NotificationID, Allowed: true}
	if targetRoute, ok := dto.NavigationHint["target_route"].(string); ok {
		nav.TargetRoute = targetRoute
	}
	if targetID, ok := dto.NavigationHint["target_resource_id"].(string); ok {
		nav.TargetResourceID = targetID
	}
	nav.TargetType = value(row.RelatedResourceType)
	if nav.TargetResourceID == "" {
		nav.TargetResourceID = value(row.RelatedResourceID)
	}
	if err := a.checkNavigationPermission(ctx, auth, nav.TargetType, nav.TargetResourceID); err != nil {
		nav.Allowed = false
		nav.DeniedReason = errorCode(err)
	}
	return nav, nil
}

func (a *App) RecordCreateFailure(ctx context.Context, in FailureInput) error {
	id := security.RandomID("ntffail_")
	errorCode := strings.TrimSpace(in.ErrorCode)
	if errorCode == "" {
		errorCode = string(bizerrors.CodeInternal)
	}
	idempotencyKey := strings.TrimSpace(in.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = "failure:" + id
	}
	now := a.now()
	row := businesscore.NotificationCreateFailure{
		ID: id, FailureID: id, SourceType: defaultString(in.RelatedResourceType, "system"), SourceID: defaultString(in.RelatedResourceID, id),
		RecipientUserID: optionalString(in.RecipientUserID), Type: defaultString(in.Type, "system"),
		RelatedResourceType: optionalString(in.RelatedResourceType), RelatedResourceID: optionalString(in.RelatedResourceID),
		IdempotencyKey: idempotencyKey, FailureCode: errorCode, FailureSummary: optionalString(in.ErrorSummary), ErrorCode: errorCode,
		TraceID: normalizeTrace(in.TraceID), CreatedBy: optionalString(systemOperator), UpdatedBy: optionalString(systemOperator), CreatedAt: now, UpdatedAt: now,
	}
	return a.repo.DB().WithContext(ctx).Create(&row).Error
}

func (a *App) getNotification(ctx context.Context, userID, notificationID string) (NotificationDTO, error) {
	row, err := a.getNotificationRow(ctx, userID, notificationID)
	if err != nil {
		return NotificationDTO{}, err
	}
	return notificationDTO(row), nil
}

func (a *App) getNotificationRow(ctx context.Context, userID, notificationID string) (businesscore.Notification, error) {
	var row businesscore.Notification
	err := a.repo.DB().WithContext(ctx).
		Where("recipient_user_id = ? AND notification_id = ?", userID, notificationID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return businesscore.Notification{}, bizerrors.New(bizerrors.CodeResourceNotFound, "notification not found")
	}
	return row, err
}

func (a *App) ensureUserExists(ctx context.Context, userID string) error {
	var count int64
	if err := a.repo.DB().WithContext(ctx).Model(&businesscore.User{}).
		Where("id = ? AND status = ?", userID, "active").Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return bizerrors.New(bizerrors.CodeResourceNotFound, "recipient user not found")
	}
	return nil
}

func (a *App) checkNavigationPermission(ctx context.Context, auth AuthContext, resourceType, resourceID string) error {
	if auth.UserID == "" {
		return bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	switch strings.TrimSpace(resourceType) {
	case "work":
		var work businesscore.Work
		err := a.repo.DB().WithContext(ctx).Where("id = ? AND owner_user_id = ? AND deleted_at IS NULL", resourceID, auth.UserID).First(&work).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return bizerrors.New(bizerrors.CodePermissionDenied, "work is not visible")
		}
		if err != nil {
			return err
		}
		if work.SpaceID != auth.SpaceID {
			return bizerrors.New(bizerrors.CodePermissionDenied, "work is not visible")
		}
		if auth.EnterpriseID != "" {
			var count int64
			err = a.repo.DB().WithContext(ctx).Model(&businesscore.EnterpriseMember{}).
				Where("enterprise_id = ? AND user_id = ? AND status = ?", auth.EnterpriseID, auth.UserID, "active").Count(&count).Error
			if err != nil {
				return err
			}
			if count == 0 {
				return bizerrors.New(bizerrors.CodePermissionDenied, "enterprise member is not active")
			}
		}
	case "skill", "skill_version":
		var count int64
		err := a.repo.DB().WithContext(ctx).Model(&businesscore.Skill{}).
			Where("id = ? OR published_version_id = ?", resourceID, resourceID).
			Where("(skill_scope = ? OR owner_user_id = ? OR enterprise_id = ?)", "public", auth.UserID, auth.EnterpriseID).
			Count(&count).Error
		if err != nil {
			return err
		}
		if count == 0 {
			return bizerrors.New(bizerrors.CodePermissionDenied, "skill is not visible")
		}
	}
	return nil
}

func notificationDTO(row businesscore.Notification) NotificationDTO {
	return NotificationDTO{
		NotificationID: row.NotificationID, Type: row.Type, Title: row.Title, Summary: row.Summary, Body: value(row.Body),
		RelatedResourceType: value(row.RelatedResourceType), RelatedResourceID: value(row.RelatedResourceID),
		NavigationHint: jsonMap(row.NavigationHintJSON), ReadAt: row.ReadAt, CreatedAt: row.CreatedAt,
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

func mustJSON(value any) datatypes.JSON {
	if value == nil {
		return datatypes.JSON([]byte(`{}`))
	}
	data, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte(`{}`))
	}
	return datatypes.JSON(data)
}

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func requestHash(meta RequestMeta, auth AuthContext, extra map[string]any) string {
	if meta.RequestHash != "" {
		return meta.RequestHash
	}
	payload, _ := json.Marshal(extra)
	sum := sha256.Sum256([]byte(auth.UserID + ":" + auth.SpaceID + ":" + string(payload)))
	return hex.EncodeToString(sum[:])
}

func businessIdempotencyKey(meta RequestMeta, scope, hash string) string {
	if key := strings.TrimSpace(meta.IdempotencyKey); key != "" {
		return key
	}
	if hash == "" {
		return ""
	}
	return scope + ":" + hash
}

func normalizeTrace(traceID string) string {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return "notification-local"
	}
	return traceID
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func errorCode(err error) string {
	var businessErr *bizerrors.BusinessError
	if errors.As(err, &businessErr) {
		return string(businessErr.Code)
	}
	return string(bizerrors.CodeInternal)
}
