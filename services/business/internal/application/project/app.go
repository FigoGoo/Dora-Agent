package project

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	StatusActive   = "active"
	StatusArchived = "archived"
)

type RequestMeta = accountspace.RequestMeta
type AuthContext = accountspace.AuthContext

type App struct {
	repo  *businesscore.Repository
	guard *idempotency.IdempotencyGuard
	audit auditlog.Writer
	now   func() time.Time
}

func New(repo *businesscore.Repository, guard *idempotency.IdempotencyGuard, audit auditlog.Writer) *App {
	return &App{repo: repo, guard: guard, audit: audit, now: func() time.Time { return time.Now().UTC() }}
}

type PageRequest struct {
	Limit  int
	Offset int
	Status string
}

type CreateInput struct {
	Auth                AuthContext
	Title               string
	InitialPromptDigest string
	Source              string
	SpaceID             string
	Meta                RequestMeta
}

type UpdateInput struct {
	Auth          AuthContext
	ProjectID     string
	Title         *string
	Description   *string
	CoverAssetID  *string
	BaseUpdatedAt string
	Meta          RequestMeta
}

type ArchiveInput struct {
	Auth      AuthContext
	ProjectID string
	Reason    string
	Meta      RequestMeta
}

type ProjectSummaryDTO struct {
	ProjectID       string    `json:"project_id"`
	Title           string    `json:"title"`
	Status          string    `json:"status"`
	CreativeAllowed bool      `json:"creative_allowed"`
	CoverAssetURL   string    `json:"cover_asset_url,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type ProjectDetailDTO struct {
	ProjectID            string    `json:"project_id"`
	Title                string    `json:"title"`
	Description          string    `json:"description,omitempty"`
	CoverAssetID         string    `json:"cover_asset_id,omitempty"`
	Status               string    `json:"status"`
	CreativeAllowed      bool      `json:"creative_allowed"`
	AllowedActions       []string  `json:"allowed_actions"`
	AgentSessionQueryRef string    `json:"agent_session_query_ref"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type ProjectAssetDTO struct {
	AssetID         string    `json:"asset_id"`
	AssetRole       string    `json:"asset_role,omitempty"`
	SourceType      string    `json:"source_type"`
	SourceSessionID string    `json:"source_session_id,omitempty"`
	SourceRunID     string    `json:"source_run_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type AttachAssetInput struct {
	Auth             AuthContext
	ProjectID        string
	AssetID          string
	AssetRole        string
	SourceSessionID  string
	SourceRunID      string
	SourceArtifactID string
	SourceType       string
	DisplayOrder     int
	Meta             RequestMeta
}

type ProjectWorkDTO struct {
	WorkID    string    `json:"work_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type ProjectAccessResult struct {
	Allowed         bool
	ProjectID       string
	ProjectStatus   string
	CreativeAllowed bool
	AllowedActions  []string
	OwnerUserID     string
	DeniedReason    string
	ProjectSummary  map[string]string
}

type Page[T any] struct {
	Items  []T   `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

func (a *App) ListProjects(ctx context.Context, auth AuthContext, page PageRequest) (Page[ProjectSummaryDTO], error) {
	if auth.UserID == "" || auth.SpaceID == "" {
		return Page[ProjectSummaryDTO]{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	limit, offset := normalizePage(page.Limit, page.Offset)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.Project{}).
		Where("space_id = ? AND owner_user_id = ?", auth.SpaceID, auth.UserID)
	if page.Status != "" && page.Status != "all" {
		db = db.Where("status = ?", page.Status)
	}
	var rows []businesscore.Project
	if err := db.Order("last_activity_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[ProjectSummaryDTO]{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[ProjectSummaryDTO]{}, err
	}
	items := make([]ProjectSummaryDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, summaryDTO(row))
	}
	return Page[ProjectSummaryDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) CreateProject(ctx context.Context, in CreateInput) (ProjectDetailDTO, error) {
	if in.Auth.UserID == "" || in.Auth.SpaceID == "" {
		return ProjectDetailDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Untitled project"
	}
	if len(title) > 120 {
		return ProjectDetailDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "title is too long")
	}
	spaceID := in.Auth.SpaceID
	if in.SpaceID != "" && in.SpaceID != spaceID {
		return ProjectDetailDTO{}, bizerrors.New(bizerrors.CodeCrossSpaceDenied, "request space does not match current identity")
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Auth.UserID + ":" + spaceID + ":" + title)
	}
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID:       "space:" + spaceID,
		SpaceID:        spaceID,
		Scope:          "project.create",
		IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash:    hash,
		ActorUserID:    in.Auth.UserID,
		EnterpriseID:   optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return ProjectDetailDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return ProjectDetailDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "idempotency key conflicts with another project create request")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return ProjectDetailDTO{}, bizerrors.New(bizerrors.CodeProcessing, "project create request is still processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.GetProject(ctx, in.Auth, decision.ReplayResult.ID)
	}
	var dto ProjectDetailDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		projectID := security.RandomID("prj_")
		project := businesscore.Project{
			ID: projectID, ProjectNo: "P" + projectID[4:], OwnerUserID: in.Auth.UserID, SpaceID: spaceID,
			EnterpriseID: optionalString(in.Auth.EnterpriseID), Title: title, Status: StatusActive, CreativeStatus: "editable",
			CreativeAllowed: true, LastActivityAt: now, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&project).Error; err != nil {
			return err
		}
		dto = detailDTO(project)
		audit := auditRecord(in.Meta.TraceID, in.Auth.UserID, spaceID, auditlog.ActionProjectCreate, "project", project.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return ProjectDetailDTO{}, err
	}
	if err := a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "project", ID: dto.ProjectID}); err != nil {
		return ProjectDetailDTO{}, err
	}
	return dto, nil
}

func (a *App) GetProject(ctx context.Context, auth AuthContext, projectID string) (ProjectDetailDTO, error) {
	project, err := a.getVisibleProject(ctx, auth, projectID)
	if err != nil {
		return ProjectDetailDTO{}, err
	}
	return detailDTO(project), nil
}

func (a *App) UpdateProject(ctx context.Context, in UpdateInput) (ProjectDetailDTO, error) {
	if in.Title == nil && in.Description == nil && in.CoverAssetID == nil {
		return ProjectDetailDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "at least one update field is required")
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Auth.UserID + ":" + in.ProjectID + ":update")
	}
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "project.update", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return ProjectDetailDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return ProjectDetailDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "idempotency key conflicts with another project update request")
	}
	var dto ProjectDetailDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := a.getVisibleProjectTx(tx, in.Auth, in.ProjectID)
		if err != nil {
			return err
		}
		if project.Status == StatusArchived {
			return bizerrors.New(bizerrors.CodeProjectArchived, "project is archived")
		}
		if err := checkBaseUpdatedAt(project.UpdatedAt, in.BaseUpdatedAt); err != nil {
			return err
		}
		now := a.now()
		updates := map[string]any{"updated_at": now, "last_activity_at": now}
		if in.Title != nil {
			title := strings.TrimSpace(*in.Title)
			if title == "" || len(title) > 120 {
				return bizerrors.New(bizerrors.CodeInvalidArgument, "title must be 1-120 characters")
			}
			updates["title"] = title
			project.Title = title
		}
		if in.Description != nil {
			desc := strings.TrimSpace(*in.Description)
			if len(desc) > 512 {
				return bizerrors.New(bizerrors.CodeInvalidArgument, "description is too long")
			}
			updates["description"] = optionalString(desc)
			project.Description = optionalString(desc)
		}
		if in.CoverAssetID != nil {
			coverAssetID := strings.TrimSpace(*in.CoverAssetID)
			if coverAssetID != "" {
				if err := validateCoverAssetTx(tx, project.ID, coverAssetID); err != nil {
					return err
				}
			}
			updates["cover_asset_id"] = optionalString(coverAssetID)
			project.CoverAssetID = optionalString(coverAssetID)
		}
		if err := tx.Model(&businesscore.Project{}).Where("id = ?", project.ID).Updates(updates).Error; err != nil {
			return err
		}
		project.UpdatedAt = now
		project.LastActivityAt = now
		dto = detailDTO(project)
		audit := auditRecord(in.Meta.TraceID, in.Auth.UserID, in.Auth.SpaceID, auditlog.ActionProjectUpdate, "project", project.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return ProjectDetailDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "project", ID: dto.ProjectID})
	return dto, nil
}

func (a *App) ArchiveProject(ctx context.Context, in ArchiveInput) (ProjectDetailDTO, error) {
	return a.setArchiveState(ctx, in, true)
}

func (a *App) RestoreProject(ctx context.Context, in ArchiveInput) (ProjectDetailDTO, error) {
	return a.setArchiveState(ctx, in, false)
}

func (a *App) ListProjectAssets(ctx context.Context, auth AuthContext, projectID string, page PageRequest) (Page[ProjectAssetDTO], error) {
	if _, err := a.getVisibleProject(ctx, auth, projectID); err != nil {
		return Page[ProjectAssetDTO]{}, err
	}
	limit, offset := normalizePage(page.Limit, page.Offset)
	var rows []businesscore.ProjectAsset
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.ProjectAsset{}).Where("project_id = ? AND status = ?", projectID, StatusActive)
	if err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[ProjectAssetDTO]{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[ProjectAssetDTO]{}, err
	}
	items := make([]ProjectAssetDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, ProjectAssetDTO{AssetID: row.AssetID, AssetRole: row.AssetRole, SourceType: row.SourceType, SourceSessionID: value(row.SourceSessionID), SourceRunID: value(row.SourceRunID), CreatedAt: row.CreatedAt})
	}
	return Page[ProjectAssetDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) AttachAssetToProject(ctx context.Context, in AttachAssetInput) (ProjectAssetDTO, error) {
	if in.Auth.UserID == "" || in.Auth.SpaceID == "" {
		return ProjectAssetDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	if in.ProjectID == "" || in.AssetID == "" {
		return ProjectAssetDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "project_id and asset_id are required")
	}
	role := strings.TrimSpace(in.AssetRole)
	if role == "" {
		role = "content"
	}
	sourceType := strings.TrimSpace(in.SourceType)
	if sourceType == "" {
		sourceType = "agent"
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Auth.UserID + ":" + in.ProjectID + ":" + in.AssetID + ":" + role)
	}
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "project.asset.attach", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return ProjectAssetDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return ProjectAssetDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "asset attach idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return ProjectAssetDTO{}, bizerrors.New(bizerrors.CodeProcessing, "asset attach request is processing")
	}
	var dto ProjectAssetDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := a.getVisibleProjectTx(tx, in.Auth, in.ProjectID)
		if err != nil {
			return err
		}
		if project.Status == StatusArchived {
			return bizerrors.New(bizerrors.CodeProjectArchived, "project is archived")
		}
		var asset struct {
			ID      string
			SpaceID string
			OwnerID string `gorm:"column:owner_user_id"`
			Project *string
			Status  string
		}
		if err := tx.Table("assets").Select("id, space_id, owner_user_id, project_id AS project, status").Where("id = ?", in.AssetID).First(&asset).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return bizerrors.New(bizerrors.CodeResourceNotFound, "asset not found")
			}
			return err
		}
		if asset.SpaceID != in.Auth.SpaceID || asset.OwnerID != in.Auth.UserID {
			return bizerrors.New(bizerrors.CodePermissionDenied, "asset is not visible to current user")
		}
		if asset.Project != nil && *asset.Project != project.ID {
			return bizerrors.New(bizerrors.CodeCrossSpaceDenied, "asset belongs to a different project")
		}
		if asset.Status != StatusActive {
			return bizerrors.New(bizerrors.CodeStateConflict, "asset is not active")
		}
		now := a.now()
		row := businesscore.ProjectAsset{
			ID: security.RandomID("pa_"), ProjectID: project.ID, AssetID: in.AssetID, AssetRole: role,
			AttachedByUserID: in.Auth.UserID, AttachedBy: optionalString(sourceType), Status: StatusActive,
			SourceSessionID: optionalString(in.SourceSessionID), SourceRunID: optionalString(in.SourceRunID),
			SourceArtifactID: optionalString(in.SourceArtifactID), SourceType: sourceType, DisplayOrder: in.DisplayOrder, CreatedAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "project_id"}, {Name: "asset_id"}},
			DoUpdates: clause.Assignments(map[string]any{"status": StatusActive, "asset_role": role, "source_type": sourceType}),
		}).Create(&row).Error; err != nil {
			return err
		}
		audit := auditRecord(in.Meta.TraceID, in.Auth.UserID, in.Auth.SpaceID, auditlog.ActionProjectAssetAttach, "project_asset", in.AssetID, "success")
		if err := tx.Create(audit).Error; err != nil {
			return err
		}
		dto = ProjectAssetDTO{
			AssetID: in.AssetID, AssetRole: role, SourceType: sourceType,
			SourceSessionID: in.SourceSessionID, SourceRunID: in.SourceRunID, CreatedAt: now,
		}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return ProjectAssetDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "project_asset", ID: dto.AssetID})
	return dto, nil
}

func (a *App) ListProjectWorks(ctx context.Context, auth AuthContext, projectID string, page PageRequest) (Page[ProjectWorkDTO], error) {
	if _, err := a.getVisibleProject(ctx, auth, projectID); err != nil {
		return Page[ProjectWorkDTO]{}, err
	}
	limit, offset := normalizePage(page.Limit, page.Offset)
	var rows []businesscore.ProjectWork
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.ProjectWork{}).Where("project_id = ?", projectID)
	if err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[ProjectWorkDTO]{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[ProjectWorkDTO]{}, err
	}
	items := make([]ProjectWorkDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, ProjectWorkDTO{WorkID: row.WorkID, Status: row.Status, CreatedAt: row.CreatedAt})
	}
	return Page[ProjectWorkDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) CheckProjectAccess(ctx context.Context, auth AuthContext, projectID string, purpose businessagent.ProjectAccessPurpose) (ProjectAccessResult, error) {
	if auth.UserID == "" || auth.SpaceID == "" {
		return ProjectAccessResult{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	project, err := a.getVisibleProject(ctx, auth, projectID)
	if err != nil {
		return ProjectAccessResult{}, err
	}
	actions := allowedActions(project)
	if project.Status == StatusArchived && purpose != businessagent.ProjectAccessPurpose_VIEW {
		return ProjectAccessResult{
			Allowed: false, ProjectID: project.ID, ProjectStatus: project.Status, CreativeAllowed: false,
			AllowedActions: actions, OwnerUserID: project.OwnerUserID, DeniedReason: "project_archived", ProjectSummary: projectSummaryMap(project),
		}, bizerrors.New(bizerrors.CodeProjectArchived, "project is archived")
	}
	return ProjectAccessResult{
		Allowed: true, ProjectID: project.ID, ProjectStatus: project.Status, CreativeAllowed: project.Status == StatusActive && project.CreativeAllowed,
		AllowedActions: actions, OwnerUserID: project.OwnerUserID, ProjectSummary: projectSummaryMap(project),
	}, nil
}

func (a *App) setArchiveState(ctx context.Context, in ArchiveInput, archived bool) (ProjectDetailDTO, error) {
	action := auditlog.ActionProjectRestore
	scope := "project.restore"
	if archived {
		action = auditlog.ActionProjectArchive
		scope = "project.archive"
	}
	hash := in.Meta.RequestHash
	if hash == "" {
		hash = security.HashIdentifier(in.Auth.UserID + ":" + in.ProjectID + ":" + action + ":" + in.Reason)
	}
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: scope, IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return ProjectDetailDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return ProjectDetailDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "idempotency key conflicts with another project state request")
	}
	var dto ProjectDetailDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := a.getVisibleProjectTx(tx, in.Auth, in.ProjectID)
		if err != nil {
			return err
		}
		now := a.now()
		status := StatusActive
		creativeStatus := "editable"
		creativeAllowed := true
		var archivedAt any
		var archivedBy any
		if archived {
			status = StatusArchived
			creativeStatus = "locked"
			creativeAllowed = false
			archivedAt = now
			archivedBy = in.Auth.UserID
		} else {
			archivedAt = nil
			archivedBy = nil
		}
		if err := tx.Model(&businesscore.Project{}).Where("id = ?", project.ID).Updates(map[string]any{
			"status": status, "creative_status": creativeStatus, "creative_allowed": creativeAllowed,
			"archive_reason": optionalString(in.Reason), "archived_at": archivedAt, "archived_by": archivedBy,
			"updated_at": now, "last_activity_at": now,
		}).Error; err != nil {
			return err
		}
		project.Status = status
		project.CreativeStatus = creativeStatus
		project.CreativeAllowed = creativeAllowed
		project.ArchivedAt = nil
		dto = detailDTO(project)
		audit := auditRecord(in.Meta.TraceID, in.Auth.UserID, in.Auth.SpaceID, action, "project", project.ID, "success")
		return tx.Create(audit).Error
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return ProjectDetailDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "project", ID: dto.ProjectID})
	return dto, nil
}

func (a *App) getVisibleProject(ctx context.Context, auth AuthContext, projectID string) (businesscore.Project, error) {
	return a.getVisibleProjectTx(a.repo.DB().WithContext(ctx), auth, projectID)
}

func (a *App) getVisibleProjectTx(tx *gorm.DB, auth AuthContext, projectID string) (businesscore.Project, error) {
	if projectID == "" {
		return businesscore.Project{}, bizerrors.New(bizerrors.CodeInvalidArgument, "project_id is required")
	}
	var project businesscore.Project
	err := tx.Where("id = ?", projectID).First(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return businesscore.Project{}, bizerrors.New(bizerrors.CodeProjectNotFound, "project not found")
	}
	if err != nil {
		return businesscore.Project{}, err
	}
	if project.SpaceID != auth.SpaceID {
		return businesscore.Project{}, bizerrors.New(bizerrors.CodeCrossSpaceDenied, "project belongs to a different space")
	}
	if project.OwnerUserID != auth.UserID {
		return businesscore.Project{}, bizerrors.New(bizerrors.CodePermissionDenied, "project is not visible to current user")
	}
	return project, nil
}

func detailDTO(project businesscore.Project) ProjectDetailDTO {
	desc := ""
	if project.Description != nil {
		desc = *project.Description
	}
	coverAssetID := ""
	if project.CoverAssetID != nil {
		coverAssetID = *project.CoverAssetID
	}
	return ProjectDetailDTO{
		ProjectID: project.ID, Title: project.Title, Description: desc, CoverAssetID: coverAssetID, Status: project.Status,
		CreativeAllowed: project.Status == StatusActive && project.CreativeAllowed,
		AllowedActions:  allowedActions(project), AgentSessionQueryRef: "project_id=" + project.ID, UpdatedAt: project.UpdatedAt,
	}
}

func summaryDTO(project businesscore.Project) ProjectSummaryDTO {
	return ProjectSummaryDTO{ProjectID: project.ID, Title: project.Title, Status: project.Status, CreativeAllowed: project.Status == StatusActive && project.CreativeAllowed, UpdatedAt: project.UpdatedAt}
}

func allowedActions(project businesscore.Project) []string {
	if project.Status == StatusArchived || !project.CreativeAllowed {
		return []string{"view"}
	}
	return []string{"view", "continue_creation", "attach_asset", "commit_asset", "create_work", "update", "archive"}
}

func projectSummaryMap(project businesscore.Project) map[string]string {
	return map[string]string{
		"project_id":    project.ID,
		"title":         project.Title,
		"status":        project.Status,
		"owner_user_id": project.OwnerUserID,
	}
}

func auditRecord(traceID, userID, spaceID, action, resourceType, resourceID, result string) *auditlog.AuditRecord {
	return &auditlog.AuditRecord{
		AuditID: security.RandomID("audit_"), TraceID: traceID, OperatorType: "user", OperatorID: &userID,
		TenantID: "space:" + spaceID, SpaceID: &spaceID, BusinessAction: action, ResourceType: resourceType, ResourceID: &resourceID,
		Result: result, MetadataSummary: datatypes.JSON([]byte(`{}`)), CreatedAt: time.Now().UTC(),
	}
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
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

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
}

func checkBaseUpdatedAt(current time.Time, baseUpdatedAt string) error {
	baseUpdatedAt = strings.TrimSpace(baseUpdatedAt)
	if baseUpdatedAt == "" {
		return nil
	}
	base, err := time.Parse(time.RFC3339Nano, baseUpdatedAt)
	if err != nil {
		return bizerrors.New(bizerrors.CodeInvalidArgument, "base_updated_at must be RFC3339 timestamp")
	}
	if !current.UTC().Equal(base.UTC()) {
		return bizerrors.New(bizerrors.CodeStateConflict, "project was updated by another request")
	}
	return nil
}

func validateCoverAssetTx(tx *gorm.DB, projectID string, coverAssetID string) error {
	var count int64
	if err := tx.Model(&businesscore.ProjectAsset{}).
		Where("project_id = ? AND asset_id = ? AND status = ?", projectID, coverAssetID, StatusActive).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return bizerrors.New(bizerrors.CodePermissionDenied, "cover asset is not referable in this project")
	}
	return nil
}

func errorCode(err error) string {
	var businessErr *bizerrors.BusinessError
	if errors.As(err, &businessErr) {
		return string(businessErr.Code)
	}
	return string(bizerrors.CodeInternal)
}
