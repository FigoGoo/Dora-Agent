package work

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/admin"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/notification"
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
	StatusPrivate   = "private"
	StatusShared    = "shared"
	StatusTakenDown = "taken_down"

	SnapshotActive    = "active"
	SnapshotCancelled = "cancelled"

	previewTTL = 10 * time.Minute
)

type Context = context.Context
type RequestMeta = accountspace.RequestMeta
type AuthContext = accountspace.AuthContext
type AdminAuth = admin.AdminAuth
type NotificationInput = notification.CreateNotificationInput

type NotificationCreator interface {
	CreateNotification(ctx context.Context, in NotificationInput) (notification.NotificationDTO, error)
}

type Options struct {
	PublicWebBaseURL string
	TOSBaseURL       string
	Env              string
	Notification     NotificationCreator
}

type App struct {
	repo  *businesscore.Repository
	guard *idempotency.IdempotencyGuard
	audit auditlog.Writer
	opts  Options
	now   func() time.Time
}

func New(repo *businesscore.Repository, guard *idempotency.IdempotencyGuard, audit auditlog.Writer, opts Options) *App {
	if opts.PublicWebBaseURL == "" {
		opts.PublicWebBaseURL = "http://localhost:3000"
	}
	if opts.TOSBaseURL == "" {
		opts.TOSBaseURL = "http://localhost/tos"
	}
	if opts.Env == "" {
		opts.Env = "local"
	}
	return &App{repo: repo, guard: guard, audit: audit, opts: opts, now: func() time.Time { return time.Now().UTC() }}
}

type CreateWorkInput struct {
	Auth         AuthContext
	Meta         RequestMeta
	ProjectID    string
	Title        string
	Description  string
	AssetIDs     []string
	CoverAssetID string
	Category     string
	Tags         []string
}

type UpdateWorkInput struct {
	Auth          AuthContext
	Meta          RequestMeta
	WorkID        string
	Title         *string
	Description   *string
	AssetIDs      []string
	CoverAssetID  *string
	Category      *string
	Tags          []string
	BaseUpdatedAt string
}

type PreviewShareWorkInput struct {
	Auth              AuthContext
	WorkID            string
	PublicTitle       string
	PublicDescription string
	Tags              []string
	SafetyEvidence    *businessagent.SafetyEvidenceDTO
}

type ConfirmShareWorkInput struct {
	Auth         AuthContext
	Meta         RequestMeta
	WorkID       string
	PreviewToken string
}

type UnshareWorkInput struct {
	Auth   AuthContext
	Meta   RequestMeta
	WorkID string
	Reason string
}

type GetPublicWorkInput struct {
	PublicWorkID string
}

type LikePublicWorkInput struct {
	Auth         AuthContext
	Meta         RequestMeta
	PublicWorkID string
}

type PreviewTakeDownWorkInput struct {
	Auth         AdminAuth
	PublicWorkID string
	Reason       string
	NotifyAuthor bool
}

type ConfirmTakeDownWorkInput struct {
	Auth         AdminAuth
	Meta         RequestMeta
	PublicWorkID string
	PreviewToken string
	Reason       string
	NotifyAuthor bool
}

type ListWorksInput struct {
	Auth        AuthContext
	ProjectID   string
	ShareStatus string
	Category    string
	Limit       int
	Offset      int
}

type ListPublicWorksInput struct {
	Category     string
	Tag          string
	ResourceType string
	Limit        int
	Offset       int
}

type WorkDTO struct {
	WorkID       string    `json:"work_id"`
	ProjectID    string    `json:"project_id"`
	Title        string    `json:"title"`
	Description  string    `json:"description,omitempty"`
	ShareStatus  string    `json:"share_status"`
	CoverAssetID string    `json:"cover_asset_id,omitempty"`
	Category     string    `json:"category,omitempty"`
	Tags         []string  `json:"tags"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type WorkAssetDTO struct {
	AssetID      string `json:"asset_id"`
	Role         string `json:"role"`
	DisplayOrder int    `json:"display_order"`
}

type WorkDetailDTO struct {
	Work           WorkDTO        `json:"work"`
	Assets         []WorkAssetDTO `json:"assets"`
	ShareSummary   map[string]any `json:"share_summary"`
	AllowedActions []string       `json:"allowed_actions"`
}

type SharePreviewDTO struct {
	PreviewToken            string              `json:"preview_token"`
	WorkID                  string              `json:"work_id"`
	PublicTitle             string              `json:"public_title"`
	PublicDescriptionDigest string              `json:"public_description_digest"`
	Tags                    []string            `json:"tags"`
	PrivacyRedactionSummary []string            `json:"privacy_redaction_summary"`
	PublicMediaSummary      []PublicMediaRefDTO `json:"public_media_summary"`
	ExpiresAt               time.Time           `json:"expires_at"`
}

type WorkShareResultDTO struct {
	WorkID       string `json:"work_id"`
	PublicWorkID string `json:"public_work_id"`
	ShareURL     string `json:"share_url"`
	ShareStatus  string `json:"share_status"`
	SnapshotID   string `json:"snapshot_id"`
}

type PublicMediaRefDTO struct {
	PublicMediaID  string `json:"public_media_id"`
	ResourceType   string `json:"resource_type"`
	Variant        string `json:"variant"`
	PublicMediaURL string `json:"public_media_url"`
}

type PublicWorkCardDTO struct {
	PublicWorkID string    `json:"public_work_id"`
	Title        string    `json:"title"`
	CoverURL     string    `json:"cover_url,omitempty"`
	ShareURL     string    `json:"share_url"`
	Category     string    `json:"category,omitempty"`
	Tags         []string  `json:"tags"`
	ResourceType string    `json:"resource_type,omitempty"`
	LikeCount    int64     `json:"like_count"`
	PublishedAt  time.Time `json:"published_at"`
}

type PublicWorkDetailDTO struct {
	PublicWorkID       string              `json:"public_work_id"`
	Title              string              `json:"title"`
	Description        string              `json:"description,omitempty"`
	ShareURL           string              `json:"share_url"`
	PublicMediaRefs    []PublicMediaRefDTO `json:"public_media_refs"`
	AuthorDisplayName  string              `json:"author_display_name,omitempty"`
	Category           string              `json:"category,omitempty"`
	Tags               []string            `json:"tags"`
	LikeCount          int64               `json:"like_count"`
	LikedByCurrentUser bool                `json:"liked_by_current_user"`
}

type PublicWorkLikeDTO struct {
	PublicWorkID string `json:"public_work_id"`
	Liked        bool   `json:"liked"`
	LikeCount    int64  `json:"like_count"`
}

type TakeDownPublicWorkPreviewDTO struct {
	PreviewToken                 string    `json:"preview_token"`
	PublicWorkID                 string    `json:"public_work_id"`
	WorkID                       string    `json:"work_id"`
	CurrentStatus                string    `json:"current_status"`
	ImpactItems                  []string  `json:"impact_items"`
	PublicLinkWillBeInaccessible bool      `json:"public_link_will_be_inaccessible"`
	SourceAssetRetained          bool      `json:"source_asset_retained"`
	NotifyAuthor                 bool      `json:"notify_author"`
	ExpiresAt                    time.Time `json:"expires_at"`
}

type AdminPublicWorkDTO struct {
	PublicWorkID       string     `json:"public_work_id"`
	WorkID             string     `json:"work_id"`
	Title              string     `json:"title"`
	Status             string     `json:"status"`
	NotificationStatus string     `json:"notification_status,omitempty"`
	PublishedAt        time.Time  `json:"published_at"`
	TakenDownAt        *time.Time `json:"taken_down_at,omitempty"`
}

type HomePublicContentDTO struct {
	FeaturedWorks []PublicWorkCardDTO `json:"featured_works"`
	LatestWorks   []PublicWorkCardDTO `json:"latest_works"`
}

type Page[T any] struct {
	Items  []T   `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

func (a *App) CreateWork(ctx context.Context, in CreateWorkInput) (WorkDetailDTO, error) {
	if err := validateAuth(in.Auth); err != nil {
		return WorkDetailDTO{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" || len(title) > 160 {
		return WorkDetailDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "title must be 1-160 characters")
	}
	assetIDs := normalizeIDs(in.AssetIDs)
	if len(assetIDs) == 0 {
		return WorkDetailDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "asset_ids is required")
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{"project_id": in.ProjectID, "title": title, "asset_ids": assetIDs, "cover_asset_id": in.CoverAssetID, "category": in.Category})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "work.create", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return WorkDetailDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return WorkDetailDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "work create idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return WorkDetailDTO{}, bizerrors.New(bizerrors.CodeProcessing, "work create request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.GetWorkDetail(ctx, in.Auth, decision.ReplayResult.ID)
	}
	var detail WorkDetailDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := a.getProjectForWriteTx(tx, in.Auth, in.ProjectID)
		if err != nil {
			return err
		}
		if err := a.validateProjectAssetsTx(tx, project.ID, assetIDs, in.CoverAssetID); err != nil {
			return err
		}
		if err := a.validateWorkCategoryTx(tx, in.Category); err != nil {
			return err
		}
		now := a.now()
		workID := security.RandomID("wrk_")
		work := businesscore.Work{
			ID: workID, WorkNo: "W" + workID[4:], ProjectID: project.ID, OwnerUserID: in.Auth.UserID, SpaceID: in.Auth.SpaceID,
			Title: title, Description: optionalString(in.Description), Category: optionalString(in.Category), TagsJSON: mustJSON(normalizeTags(in.Tags)),
			ShareStatus: StatusPrivate, CoverAssetID: optionalString(in.CoverAssetID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&work).Error; err != nil {
			return err
		}
		if err := a.replaceWorkAssetsTx(tx, work.ID, assetIDs, in.CoverAssetID, now); err != nil {
			return err
		}
		projectWork := businesscore.ProjectWork{
			ID: security.RandomID("pw_"), ProjectID: project.ID, WorkID: work.ID, Status: "active",
			CreatedFromAssetIDs: mustJSON(assetIDs), CreatedBy: optionalString(in.Auth.UserID), CreatedAt: now,
		}
		if err := tx.Create(&projectWork).Error; err != nil {
			return err
		}
		if err := tx.Create(auditRecord(in.Meta.TraceID, in.Auth.UserID, in.Auth.SpaceID, "work.create", "work", work.ID, "success")).Error; err != nil {
			return err
		}
		detail, err = a.workDetailTx(tx, work)
		return err
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return WorkDetailDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "work", ID: detail.Work.WorkID})
	return detail, nil
}

func (a *App) UpdateWork(ctx context.Context, in UpdateWorkInput) (WorkDetailDTO, error) {
	if err := validateAuth(in.Auth); err != nil {
		return WorkDetailDTO{}, err
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{
		"work_id": in.WorkID, "title": in.Title, "description": in.Description, "assets": in.AssetIDs,
		"cover": in.CoverAssetID, "category": in.Category, "tags": in.Tags, "base_updated_at": in.BaseUpdatedAt,
	})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "work.update", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return WorkDetailDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return WorkDetailDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "work update idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.GetWorkDetail(ctx, in.Auth, decision.ReplayResult.ID)
	}
	var detail WorkDetailDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		work, err := a.getOwnedWorkTx(tx, in.Auth, in.WorkID)
		if err != nil {
			return err
		}
		project, err := a.getProjectForWriteTx(tx, in.Auth, work.ProjectID)
		if err != nil {
			return err
		}
		if err := checkBaseUpdatedAt(work.UpdatedAt, in.BaseUpdatedAt); err != nil {
			return err
		}
		now := a.now()
		updates := map[string]any{"updated_at": now}
		assetIDs := normalizeIDs(in.AssetIDs)
		hasChanges, err := a.hasWorkPatchChangesTx(tx, work, in, assetIDs)
		if err != nil {
			return err
		}
		if work.ShareStatus == StatusTakenDown {
			if !hasChanges {
				return bizerrors.New(bizerrors.CodeStateConflict, "taken_down work must be edited before resetting to private")
			}
			updates["share_status"] = StatusPrivate
			updates["private_reset_at"] = now
			updates["current_snapshot_id"] = nil
			work.ShareStatus = StatusPrivate
			work.PrivateResetAt = &now
			work.CurrentSnapshotID = nil
		}
		if in.Title != nil {
			title := strings.TrimSpace(*in.Title)
			if title == "" || len(title) > 160 {
				return bizerrors.New(bizerrors.CodeInvalidArgument, "title must be 1-160 characters")
			}
			updates["title"] = title
			work.Title = title
		}
		if in.Description != nil {
			updates["description"] = optionalString(*in.Description)
			work.Description = optionalString(*in.Description)
		}
		if in.Category != nil {
			category := strings.TrimSpace(*in.Category)
			if err := a.validateWorkCategoryTx(tx, category); err != nil {
				return err
			}
			updates["category"] = optionalString(category)
			work.Category = optionalString(category)
		}
		if in.Tags != nil {
			tags := normalizeTags(in.Tags)
			updates["tags"] = mustJSON(tags)
			work.TagsJSON = mustJSON(tags)
		}
		coverID := value(work.CoverAssetID)
		if in.CoverAssetID != nil {
			coverID = strings.TrimSpace(*in.CoverAssetID)
			updates["cover_asset_id"] = optionalString(coverID)
			work.CoverAssetID = optionalString(coverID)
		}
		if len(assetIDs) > 0 {
			if err := a.validateProjectAssetsTx(tx, project.ID, assetIDs, coverID); err != nil {
				return err
			}
			if err := a.replaceWorkAssetsTx(tx, work.ID, assetIDs, coverID, now); err != nil {
				return err
			}
		} else if coverID != "" {
			if err := a.validateProjectAssetsTx(tx, project.ID, []string{coverID}, coverID); err != nil {
				return err
			}
		}
		if err := tx.Model(&businesscore.Work{}).Where("id = ?", work.ID).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Create(auditRecord(in.Meta.TraceID, in.Auth.UserID, in.Auth.SpaceID, "work.update", "work", work.ID, "success")).Error; err != nil {
			return err
		}
		work.UpdatedAt = now
		detail, err = a.workDetailTx(tx, work)
		return err
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return WorkDetailDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "work", ID: detail.Work.WorkID})
	return detail, nil
}

func (a *App) ListMyWorks(ctx context.Context, in ListWorksInput) (Page[WorkDTO], error) {
	if err := validateAuth(in.Auth); err != nil {
		return Page[WorkDTO]{}, err
	}
	if err := a.requireActiveEnterpriseMember(ctx, in.Auth); err != nil {
		return Page[WorkDTO]{}, err
	}
	limit, offset := normalizePage(in.Limit, in.Offset)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.Work{}).
		Where("space_id = ? AND owner_user_id = ? AND deleted_at IS NULL", in.Auth.SpaceID, in.Auth.UserID)
	if in.ProjectID != "" {
		db = db.Where("project_id = ?", in.ProjectID)
	}
	if in.ShareStatus != "" && in.ShareStatus != "all" {
		db = db.Where("share_status = ?", in.ShareStatus)
	}
	if in.Category != "" {
		db = db.Where("category = ?", in.Category)
	}
	var rows []businesscore.Work
	if err := db.Order("updated_at DESC, id ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[WorkDTO]{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[WorkDTO]{}, err
	}
	items := make([]WorkDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, workDTO(row))
	}
	return Page[WorkDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) GetWorkDetail(ctx context.Context, auth AuthContext, workID string) (WorkDetailDTO, error) {
	work, err := a.getOwnedWorkTx(a.repo.DB().WithContext(ctx), auth, workID)
	if err != nil {
		return WorkDetailDTO{}, err
	}
	return a.workDetailTx(a.repo.DB().WithContext(ctx), work)
}

func (a *App) PreviewShareWork(ctx context.Context, in PreviewShareWorkInput) (SharePreviewDTO, error) {
	if err := validateAuth(in.Auth); err != nil {
		return SharePreviewDTO{}, err
	}
	work, err := a.getOwnedWorkTx(a.repo.DB().WithContext(ctx), in.Auth, in.WorkID)
	if err != nil {
		return SharePreviewDTO{}, err
	}
	if work.ShareStatus == StatusShared {
		return SharePreviewDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "shared work must be edited or unshared before sharing again")
	}
	if work.ShareStatus == StatusTakenDown {
		return SharePreviewDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "taken down work must be edited before sharing again")
	}
	if _, err := a.getProjectForWriteTx(a.repo.DB().WithContext(ctx), in.Auth, work.ProjectID); err != nil {
		return SharePreviewDTO{}, err
	}
	if err := validateWorkShareEvidence(in.SafetyEvidence, in.PublicTitle, in.PublicDescription, in.Tags, a.now()); err != nil {
		return SharePreviewDTO{}, err
	}
	assets, err := a.listWorkAssets(ctx, work.ID)
	if err != nil {
		return SharePreviewDTO{}, err
	}
	if len(assets) == 0 {
		return SharePreviewDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "work has no publicable assets")
	}
	publicMedia := a.publicMediaRefs("preview", "preview", assets)
	expiresAt := a.now().Add(previewTTL)
	payload := sharePreviewTokenPayload{
		Kind: "work_share", WorkID: work.ID, ActorUserID: in.Auth.UserID, PublicTitle: strings.TrimSpace(in.PublicTitle),
		PublicDescription: strings.TrimSpace(in.PublicDescription), Tags: normalizeTags(in.Tags), EvidenceID: in.SafetyEvidence.SafetyEvidenceId,
		EvidenceDigest: in.SafetyEvidence.EvaluatedObjectDigest, ExpiresAt: expiresAt,
	}
	token, err := signPreviewToken(payload)
	if err != nil {
		return SharePreviewDTO{}, err
	}
	return SharePreviewDTO{
		PreviewToken: token, WorkID: work.ID, PublicTitle: payload.PublicTitle, PublicDescriptionDigest: ShareTextDigest("", payload.PublicDescription, nil),
		Tags: payload.Tags, PrivacyRedactionSummary: []string{"project_id", "session_id", "blackboard", "prompt", "credit", "model_cost"},
		PublicMediaSummary: publicMedia, ExpiresAt: expiresAt,
	}, nil
}

func (a *App) ConfirmShareWork(ctx context.Context, in ConfirmShareWorkInput) (WorkShareResultDTO, error) {
	if err := validateAuth(in.Auth); err != nil {
		return WorkShareResultDTO{}, err
	}
	payload, err := verifySharePreviewToken(in.PreviewToken, a.now())
	if err != nil {
		return WorkShareResultDTO{}, err
	}
	if payload.WorkID != in.WorkID || payload.ActorUserID != in.Auth.UserID {
		return WorkShareResultDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "preview token does not match current work or user")
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{"work_id": in.WorkID, "preview_token": in.PreviewToken})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "work.share.confirm", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return WorkShareResultDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return WorkShareResultDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "work share idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.shareResultBySnapshot(ctx, decision.ReplayResult.ID)
	}
	var result WorkShareResultDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		work, err := a.getOwnedWorkTx(tx.Clauses(clause.Locking{Strength: "UPDATE"}), in.Auth, in.WorkID)
		if err != nil {
			return err
		}
		if work.ShareStatus != StatusPrivate {
			return bizerrors.New(bizerrors.CodeStateConflict, "only private work can be shared")
		}
		if _, err := a.getProjectForWriteTx(tx, in.Auth, work.ProjectID); err != nil {
			return err
		}
		assets, err := a.listWorkAssetsTx(tx, work.ID)
		if err != nil {
			return err
		}
		if len(assets) == 0 {
			return bizerrors.New(bizerrors.CodeStateConflict, "work has no publicable assets")
		}
		now := a.now()
		publicWorkID := security.RandomID("pubw_")
		snapshotID := security.RandomID("wps_")
		slug := publicSlug(payload.PublicTitle, publicWorkID)
		publicURL := strings.TrimRight(a.opts.PublicWebBaseURL, "/") + "/share/" + slug
		mediaRefs := a.publicMediaRefs(publicWorkID, snapshotID, assets)
		snapshotPayload := publicSnapshotPayload{
			PublicWorkID: publicWorkID, Title: payload.PublicTitle, Description: payload.PublicDescription, Tags: payload.Tags,
			ShareURL: publicURL, PublicMediaRefs: mediaRefs, AuthorDisplayName: "Dora Creator",
		}
		row := businesscore.WorkPublicSnapshot{
			ID: security.RandomID("wpsrow_"), SnapshotID: snapshotID, WorkID: work.ID, ShareSlug: slug, Title: payload.PublicTitle,
			Description: optionalString(payload.PublicDescription), CoverAssetID: work.CoverAssetID, SnapshotJSON: mustJSON(snapshotPayload),
			ShareURL: publicURL, Visibility: "public", PublicWorkID: publicWorkID, PublicSlug: slug, PublicURL: publicURL,
			SnapshotPayloadJSON: mustJSON(snapshotPayload), PublicMediaRefsJSON: mustJSON(mediaRefs), Status: SnapshotActive,
			Category: work.Category, ResourceType: optionalString(primaryResourceType(assets)), LikeCount: 0, PublishedBy: in.Auth.UserID, PublishedAt: now,
			PublishedByUserID: in.Auth.UserID,
			SafetyEvidenceID:  optionalString(payload.EvidenceID), SafetyEvidenceDigest: optionalString(payload.EvidenceDigest), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.Work{}).Where("id = ?", work.ID).Updates(map[string]any{
			"share_status": StatusShared, "current_snapshot_id": snapshotID, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(auditRecord(in.Meta.TraceID, in.Auth.UserID, in.Auth.SpaceID, "work.share", "work", work.ID, "success")).Error; err != nil {
			return err
		}
		result = WorkShareResultDTO{WorkID: work.ID, PublicWorkID: publicWorkID, ShareURL: publicURL, ShareStatus: StatusShared, SnapshotID: snapshotID}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return WorkShareResultDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "work_public_snapshot", ID: result.SnapshotID})
	return result, nil
}

func (a *App) UnshareWork(ctx context.Context, in UnshareWorkInput) (WorkDetailDTO, error) {
	if err := validateAuth(in.Auth); err != nil {
		return WorkDetailDTO{}, err
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{"work_id": in.WorkID, "reason": in.Reason})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "work.unshare", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return WorkDetailDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return WorkDetailDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "work unshare idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.GetWorkDetail(ctx, in.Auth, decision.ReplayResult.ID)
	}
	var detail WorkDetailDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		work, err := a.getOwnedWorkTx(tx.Clauses(clause.Locking{Strength: "UPDATE"}), in.Auth, in.WorkID)
		if err != nil {
			return err
		}
		if work.ShareStatus != StatusShared || work.CurrentSnapshotID == nil {
			return bizerrors.New(bizerrors.CodeStateConflict, "work is not shared")
		}
		now := a.now()
		if err := tx.Model(&businesscore.WorkPublicSnapshot{}).
			Where("snapshot_id = ? AND status = ?", *work.CurrentSnapshotID, SnapshotActive).
			Updates(map[string]any{"status": SnapshotCancelled, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.Work{}).Where("id = ?", work.ID).
			Updates(map[string]any{"share_status": StatusPrivate, "current_snapshot_id": nil, "updated_at": now}).Error; err != nil {
			return err
		}
		work.ShareStatus = StatusPrivate
		work.CurrentSnapshotID = nil
		work.UpdatedAt = now
		if err := tx.Create(auditRecord(in.Meta.TraceID, in.Auth.UserID, in.Auth.SpaceID, "work.unshare", "work", work.ID, "success")).Error; err != nil {
			return err
		}
		detail, err = a.workDetailTx(tx, work)
		return err
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return WorkDetailDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "work", ID: detail.Work.WorkID})
	return detail, nil
}

func (a *App) GetHomePublicContent(ctx context.Context) (HomePublicContentDTO, error) {
	page, err := a.ListPublicWorks(ctx, ListPublicWorksInput{Limit: 10})
	if err != nil {
		return HomePublicContentDTO{}, err
	}
	return HomePublicContentDTO{FeaturedWorks: page.Items, LatestWorks: page.Items}, nil
}

func (a *App) ListPublicWorks(ctx context.Context, in ListPublicWorksInput) (Page[PublicWorkCardDTO], error) {
	limit, offset := normalizePage(in.Limit, in.Offset)
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.WorkPublicSnapshot{}).Where("status = ?", SnapshotActive)
	if in.Category != "" {
		db = db.Where("category = ?", in.Category)
	}
	if in.ResourceType != "" {
		db = db.Where("resource_type = ?", in.ResourceType)
	}
	if in.Tag != "" {
		db = db.Where("snapshot_payload->'tags' ? ?", in.Tag)
	}
	var rows []businesscore.WorkPublicSnapshot
	if err := db.Order("published_at DESC, public_work_id ASC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[PublicWorkCardDTO]{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[PublicWorkCardDTO]{}, err
	}
	items := make([]PublicWorkCardDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, publicCardDTO(row))
	}
	return Page[PublicWorkCardDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) GetPublicWork(ctx context.Context, in GetPublicWorkInput) (PublicWorkDetailDTO, error) {
	row, err := a.getActiveSnapshot(ctx, in.PublicWorkID)
	if err != nil {
		return PublicWorkDetailDTO{}, err
	}
	payload := snapshotPayload(row)
	return PublicWorkDetailDTO{
		PublicWorkID: row.PublicWorkID, Title: payload.Title, Description: payload.Description, ShareURL: row.PublicURL,
		PublicMediaRefs: payload.PublicMediaRefs, AuthorDisplayName: payload.AuthorDisplayName, Category: value(row.Category),
		Tags: payload.Tags, LikeCount: row.LikeCount,
	}, nil
}

func (a *App) LikePublicWork(ctx context.Context, in LikePublicWorkInput) (PublicWorkLikeDTO, error) {
	return a.setLike(ctx, in, true)
}

func (a *App) UnlikePublicWork(ctx context.Context, in LikePublicWorkInput) (PublicWorkLikeDTO, error) {
	return a.setLike(ctx, in, false)
}

func (a *App) PreviewTakeDownWork(ctx context.Context, in PreviewTakeDownWorkInput) (TakeDownPublicWorkPreviewDTO, error) {
	if in.Auth.AdminID == "" {
		return TakeDownPublicWorkPreviewDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	if strings.TrimSpace(in.Reason) == "" {
		return TakeDownPublicWorkPreviewDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "reason is required")
	}
	snapshot, err := a.getActiveSnapshot(ctx, in.PublicWorkID)
	if err != nil {
		return TakeDownPublicWorkPreviewDTO{}, err
	}
	expiresAt := a.now().Add(previewTTL)
	payload := takedownPreviewTokenPayload{
		Kind: "work_takedown", PublicWorkID: snapshot.PublicWorkID, WorkID: snapshot.WorkID, AdminID: in.Auth.AdminID,
		ReasonDigest: textDigest(in.Reason), NotifyAuthor: in.NotifyAuthor, ExpiresAt: expiresAt,
	}
	token, err := signTakedownToken(payload)
	if err != nil {
		return TakeDownPublicWorkPreviewDTO{}, err
	}
	return TakeDownPublicWorkPreviewDTO{
		PreviewToken: token, PublicWorkID: snapshot.PublicWorkID, WorkID: snapshot.WorkID, CurrentStatus: snapshot.Status,
		ImpactItems: []string{"public link will be inaccessible", "source asset retained"}, PublicLinkWillBeInaccessible: true,
		SourceAssetRetained: true, NotifyAuthor: in.NotifyAuthor, ExpiresAt: expiresAt,
	}, nil
}

func (a *App) ConfirmTakeDownWork(ctx context.Context, in ConfirmTakeDownWorkInput) (AdminPublicWorkDTO, error) {
	if in.Auth.AdminID == "" {
		return AdminPublicWorkDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "admin auth is required")
	}
	payload, err := verifyTakedownToken(in.PreviewToken, a.now())
	if err != nil {
		return AdminPublicWorkDTO{}, err
	}
	if payload.PublicWorkID != in.PublicWorkID || payload.AdminID != in.Auth.AdminID || payload.ReasonDigest != textDigest(in.Reason) {
		return AdminPublicWorkDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "take-down preview token does not match confirm request")
	}
	hash := adminRequestHash(in.Meta, in.Auth, map[string]any{"public_work_id": in.PublicWorkID, "reason": in.Reason, "notify_author": in.NotifyAuthor})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "admin:" + in.Auth.AdminID, Scope: "work.public.take_down", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.AdminID,
	})
	if err != nil {
		return AdminPublicWorkDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return AdminPublicWorkDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "take-down idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.adminPublicWorkByID(ctx, decision.ReplayResult.ID)
	}
	var out AdminPublicWorkDTO
	var authorUserID string
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		snapshot, err := a.getActiveSnapshotTx(tx.Clauses(clause.Locking{Strength: "UPDATE"}), in.PublicWorkID)
		if err != nil {
			return err
		}
		var work businesscore.Work
		if err := tx.Where("id = ?", snapshot.WorkID).First(&work).Error; err != nil {
			return err
		}
		authorUserID = work.OwnerUserID
		now := a.now()
		recordID := security.RandomID("wmr_")
		record := businesscore.WorkModerationRecord{
			ID: recordID, RecordID: recordID, SnapshotID: snapshot.SnapshotID, PublicWorkID: snapshot.PublicWorkID,
			Action: "take_down", Reason: strings.TrimSpace(in.Reason), BeforeStatus: optionalString(snapshot.Status), AfterStatus: StatusTakenDown,
			OperatedByAdminID: in.Auth.AdminID, OperatorAdminID: in.Auth.AdminID, TraceID: in.Meta.TraceID, CreatedAt: now,
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.WorkPublicSnapshot{}).Where("public_work_id = ?", snapshot.PublicWorkID).Updates(map[string]any{
			"status": StatusTakenDown, "taken_down_by": in.Auth.AdminID, "taken_down_by_admin_id": in.Auth.AdminID,
			"taken_down_at": now, "taken_down_reason": in.Reason, "take_down_reason": in.Reason, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&businesscore.Work{}).Where("id = ?", work.ID).Updates(map[string]any{
			"share_status": StatusTakenDown, "last_moderation_record_id": recordID, "updated_at": now,
		}).Error; err != nil {
			return err
		}
		audit := adminAuditRecord(in.Meta.TraceID, in.Auth.AdminID, "work.public.take_down", "public_work", snapshot.PublicWorkID, "success", in.Reason)
		if err := tx.Create(audit).Error; err != nil {
			return err
		}
		out = AdminPublicWorkDTO{PublicWorkID: snapshot.PublicWorkID, WorkID: snapshot.WorkID, Title: snapshotPayload(snapshot).Title, Status: StatusTakenDown, PublishedAt: snapshot.PublishedAt, TakenDownAt: &now}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return AdminPublicWorkDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "public_work", ID: out.PublicWorkID})
	out.NotificationStatus = "skipped"
	if in.NotifyAuthor {
		if err := a.notifyTakenDown(ctx, authorUserID, out, in.Meta); err != nil {
			out.NotificationStatus = "failed"
			_ = a.recordNotificationFailure(ctx, authorUserID, out.PublicWorkID, in.Meta.TraceID, err)
		} else {
			out.NotificationStatus = "created"
		}
	}
	return out, nil
}

func (a *App) ListAdminPublicWorks(ctx context.Context, limit, offset int) (Page[AdminPublicWorkDTO], error) {
	limit, offset = normalizePage(limit, offset)
	var rows []businesscore.WorkPublicSnapshot
	db := a.repo.DB().WithContext(ctx).Model(&businesscore.WorkPublicSnapshot{})
	if err := db.Order("published_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[AdminPublicWorkDTO]{}, err
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[AdminPublicWorkDTO]{}, err
	}
	items := make([]AdminPublicWorkDTO, 0, len(rows))
	for _, row := range rows {
		items = append(items, AdminPublicWorkDTO{PublicWorkID: row.PublicWorkID, WorkID: row.WorkID, Title: snapshotPayload(row).Title, Status: row.Status, PublishedAt: row.PublishedAt, TakenDownAt: row.TakenDownAt})
	}
	return Page[AdminPublicWorkDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) CountNotificationFailuresForTest(ctx context.Context, publicWorkID string) int64 {
	var count int64
	_ = a.repo.DB().WithContext(ctx).Model(&businesscore.NotificationCreateFailure{}).
		Where("related_resource_type = ? AND related_resource_id = ?", "public_work", publicWorkID).Count(&count).Error
	return count
}

func (a *App) setLike(ctx context.Context, in LikePublicWorkInput, liked bool) (PublicWorkLikeDTO, error) {
	if err := validateAuth(in.Auth); err != nil {
		return PublicWorkLikeDTO{}, err
	}
	scope := "work.public.like"
	if !liked {
		scope = "work.public.unlike"
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{"public_work_id": in.PublicWorkID, "liked": liked})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "user:" + in.Auth.UserID, SpaceID: in.Auth.SpaceID, Scope: scope, IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return PublicWorkLikeDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return PublicWorkLikeDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "public work reaction idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.publicWorkLikeResult(ctx, decision.ReplayResult.ID, liked)
	}
	var out PublicWorkLikeDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		snapshot, err := a.getActiveSnapshotTx(tx.Clauses(clause.Locking{Strength: "UPDATE"}), in.PublicWorkID)
		if err != nil {
			return err
		}
		var existing businesscore.WorkLike
		err = tx.Where("public_work_id = ? AND user_id = ?", snapshot.PublicWorkID, in.Auth.UserID).First(&existing).Error
		now := a.now()
		delta := int64(0)
		targetStatus := "unliked"
		if liked {
			targetStatus = "liked"
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			existing = businesscore.WorkLike{
				ID: security.RandomID("wlike_"), LikeID: security.RandomID("wlike_"), PublicWorkID: snapshot.PublicWorkID, WorkID: snapshot.WorkID,
				SnapshotID: snapshot.SnapshotID, UserID: in.Auth.UserID, Status: targetStatus, CreatedAt: now, UpdatedAt: now,
			}
			if liked {
				existing.LikedAt = &now
				delta = 1
			}
			if err := tx.Create(&existing).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if existing.Status != targetStatus {
			if liked {
				delta = 1
				existing.LikedAt = &now
			} else {
				delta = -1
				existing.LikedAt = nil
			}
			if err := tx.Model(&businesscore.WorkLike{}).Where("id = ?", existing.ID).Updates(map[string]any{"status": targetStatus, "liked_at": existing.LikedAt, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		if delta != 0 {
			if err := tx.Model(&businesscore.WorkPublicSnapshot{}).Where("public_work_id = ?", snapshot.PublicWorkID).
				UpdateColumn("like_count", gorm.Expr("GREATEST(like_count + ?, 0)", delta)).Error; err != nil {
				return err
			}
			snapshot.LikeCount += delta
			if snapshot.LikeCount < 0 {
				snapshot.LikeCount = 0
			}
		}
		if err := tx.Create(auditRecord(in.Meta.TraceID, in.Auth.UserID, in.Auth.SpaceID, "work.like", "public_work", snapshot.PublicWorkID, "success")).Error; err != nil {
			return err
		}
		out = PublicWorkLikeDTO{PublicWorkID: snapshot.PublicWorkID, Liked: liked, LikeCount: snapshot.LikeCount}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return PublicWorkLikeDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "public_work", ID: out.PublicWorkID})
	return out, nil
}

func (a *App) publicWorkLikeResult(ctx context.Context, publicWorkID string, liked bool) (PublicWorkLikeDTO, error) {
	var snapshot businesscore.WorkPublicSnapshot
	err := a.repo.DB().WithContext(ctx).Where("public_work_id = ?", publicWorkID).First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return PublicWorkLikeDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "public work not found")
	}
	if err != nil {
		return PublicWorkLikeDTO{}, err
	}
	return PublicWorkLikeDTO{PublicWorkID: snapshot.PublicWorkID, Liked: liked, LikeCount: snapshot.LikeCount}, nil
}

func (a *App) adminPublicWorkByID(ctx context.Context, publicWorkID string) (AdminPublicWorkDTO, error) {
	var snapshot businesscore.WorkPublicSnapshot
	err := a.repo.DB().WithContext(ctx).Where("public_work_id = ?", publicWorkID).First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AdminPublicWorkDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "public work not found")
	}
	if err != nil {
		return AdminPublicWorkDTO{}, err
	}
	out := AdminPublicWorkDTO{
		PublicWorkID: snapshot.PublicWorkID, WorkID: snapshot.WorkID, Title: snapshotPayload(snapshot).Title,
		Status: snapshot.Status, PublishedAt: snapshot.PublishedAt, TakenDownAt: snapshot.TakenDownAt,
	}
	if snapshot.Status == StatusTakenDown {
		out.NotificationStatus = a.takedownNotificationStatus(ctx, snapshot.PublicWorkID)
	}
	return out, nil
}

func (a *App) takedownNotificationStatus(ctx context.Context, publicWorkID string) string {
	var count int64
	err := a.repo.DB().WithContext(ctx).Model(&businesscore.Notification{}).
		Where("idempotency_key = ?", "work_takedown:"+publicWorkID).Count(&count).Error
	if err == nil && count > 0 {
		return "created"
	}
	err = a.repo.DB().WithContext(ctx).Model(&businesscore.NotificationCreateFailure{}).
		Where("idempotency_key = ?", "work_takedown:"+publicWorkID).Count(&count).Error
	if err == nil && count > 0 {
		return "failed"
	}
	return "skipped"
}

func (a *App) notifyTakenDown(ctx context.Context, recipientUserID string, out AdminPublicWorkDTO, meta RequestMeta) error {
	if a.opts.Notification == nil {
		return nil
	}
	_, err := a.opts.Notification.CreateNotification(ctx, NotificationInput{
		RecipientUserID: recipientUserID, Type: "work_public_taken_down", Title: "作品已下架", Summary: "你的公开作品已被平台下架。",
		RelatedResourceType: "work", RelatedResourceID: out.WorkID,
		NavigationHint: map[string]any{"target_route": "/works/" + out.WorkID, "target_resource_id": out.WorkID},
		IdempotencyKey: "work_takedown:" + out.PublicWorkID, TraceID: meta.TraceID,
	})
	return err
}

func (a *App) recordNotificationFailure(ctx context.Context, recipientUserID, publicWorkID, traceID string, cause error) error {
	now := a.now()
	id := security.RandomID("ntffail_")
	row := businesscore.NotificationCreateFailure{
		ID: id, FailureID: id, SourceType: "public_work", SourceID: publicWorkID,
		RecipientUserID: optionalString(recipientUserID), Type: "work_public_taken_down",
		RelatedResourceType: optionalString("public_work"), RelatedResourceID: optionalString(publicWorkID),
		IdempotencyKey: "work_takedown:" + publicWorkID, FailureCode: errorCode(cause), FailureSummary: optionalString(cause.Error()),
		ErrorCode: errorCode(cause), TraceID: traceID,
		CreatedAt: now, UpdatedAt: now,
	}
	return a.repo.DB().WithContext(ctx).Create(&row).Error
}

func (a *App) shareResultBySnapshot(ctx context.Context, snapshotID string) (WorkShareResultDTO, error) {
	var row businesscore.WorkPublicSnapshot
	err := a.repo.DB().WithContext(ctx).Where("snapshot_id = ?", snapshotID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return WorkShareResultDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "public snapshot not found")
	}
	if err != nil {
		return WorkShareResultDTO{}, err
	}
	return WorkShareResultDTO{WorkID: row.WorkID, PublicWorkID: row.PublicWorkID, ShareURL: row.PublicURL, ShareStatus: StatusShared, SnapshotID: row.SnapshotID}, nil
}

func (a *App) getProjectForWriteTx(tx *gorm.DB, auth AuthContext, projectID string) (businesscore.Project, error) {
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
	if project.Status == "archived" || !project.CreativeAllowed {
		return businesscore.Project{}, bizerrors.New(bizerrors.CodeProjectArchived, "project is archived")
	}
	if auth.EnterpriseID != "" {
		if err := a.requireActiveEnterpriseMemberTx(tx, auth); err != nil {
			return businesscore.Project{}, err
		}
	}
	return project, nil
}

func (a *App) getOwnedWorkTx(tx *gorm.DB, auth AuthContext, workID string) (businesscore.Work, error) {
	if err := validateAuth(auth); err != nil {
		return businesscore.Work{}, err
	}
	var work businesscore.Work
	err := tx.Where("id = ? AND deleted_at IS NULL", workID).First(&work).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return businesscore.Work{}, bizerrors.New(bizerrors.CodeResourceNotFound, "work not found")
	}
	if err != nil {
		return businesscore.Work{}, err
	}
	if work.SpaceID != auth.SpaceID {
		return businesscore.Work{}, bizerrors.New(bizerrors.CodeCrossSpaceDenied, "work belongs to a different space")
	}
	if work.OwnerUserID != auth.UserID {
		return businesscore.Work{}, bizerrors.New(bizerrors.CodePermissionDenied, "work is not owned by current user")
	}
	if auth.EnterpriseID != "" {
		if err := a.requireActiveEnterpriseMemberTx(tx, auth); err != nil {
			return businesscore.Work{}, err
		}
	}
	return work, nil
}

func (a *App) requireActiveEnterpriseMember(ctx context.Context, auth AuthContext) error {
	return a.requireActiveEnterpriseMemberTx(a.repo.DB().WithContext(ctx), auth)
}

func (a *App) requireActiveEnterpriseMemberTx(tx *gorm.DB, auth AuthContext) error {
	if auth.EnterpriseID == "" {
		return nil
	}
	var count int64
	if err := tx.Session(&gorm.Session{NewDB: true}).Model(&businesscore.EnterpriseMember{}).
		Where("enterprise_id = ? AND user_id = ? AND status = ?", auth.EnterpriseID, auth.UserID, "active").Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return bizerrors.New(bizerrors.CodePermissionDenied, "enterprise member is not active")
	}
	return nil
}

func (a *App) validateProjectAssetsTx(tx *gorm.DB, projectID string, assetIDs []string, coverAssetID string) error {
	assetIDs = normalizeIDs(assetIDs)
	if coverAssetID != "" {
		assetIDs = normalizeIDs(append(assetIDs, coverAssetID))
	}
	var count int64
	if err := tx.Model(&businesscore.ProjectAsset{}).
		Where("project_id = ? AND asset_id IN ? AND status = ?", projectID, assetIDs, "active").
		Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(assetIDs)) {
		return bizerrors.New(bizerrors.CodePermissionDenied, "work assets must belong to current project")
	}
	return nil
}

func (a *App) replaceWorkAssetsTx(tx *gorm.DB, workID string, assetIDs []string, coverAssetID string, now time.Time) error {
	if err := tx.Where("work_id = ?", workID).Delete(&businesscore.WorkAsset{}).Error; err != nil {
		return err
	}
	rows := make([]businesscore.WorkAsset, 0, len(assetIDs))
	for i, assetID := range assetIDs {
		role := "content"
		if assetID == coverAssetID {
			role = "cover"
		}
		id := security.RandomID("wka_")
		rows = append(rows, businesscore.WorkAsset{ID: id, WorkAssetID: id, WorkID: workID, AssetID: assetID, Role: role, DisplayOrder: i, CreatedAt: now, UpdatedAt: now})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

func (a *App) workDetailTx(tx *gorm.DB, work businesscore.Work) (WorkDetailDTO, error) {
	assets, err := a.listWorkAssetsTx(tx, work.ID)
	if err != nil {
		return WorkDetailDTO{}, err
	}
	shareSummary := map[string]any{"share_status": work.ShareStatus}
	if work.CurrentSnapshotID != nil {
		shareSummary["snapshot_id"] = *work.CurrentSnapshotID
	}
	return WorkDetailDTO{Work: workDTO(work), Assets: assets, ShareSummary: shareSummary, AllowedActions: allowedActions(work)}, nil
}

func (a *App) listWorkAssets(ctx context.Context, workID string) ([]WorkAssetDTO, error) {
	return a.listWorkAssetsTx(a.repo.DB().WithContext(ctx), workID)
}

func (a *App) listWorkAssetsTx(tx *gorm.DB, workID string) ([]WorkAssetDTO, error) {
	var rows []businesscore.WorkAsset
	if err := tx.Where("work_id = ?", workID).Order("display_order ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]WorkAssetDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, WorkAssetDTO{AssetID: row.AssetID, Role: row.Role, DisplayOrder: row.DisplayOrder})
	}
	return out, nil
}

func (a *App) hasWorkPatchChangesTx(tx *gorm.DB, work businesscore.Work, in UpdateWorkInput, assetIDs []string) (bool, error) {
	if in.Title != nil && strings.TrimSpace(*in.Title) != work.Title {
		return true, nil
	}
	if in.Description != nil && strings.TrimSpace(*in.Description) != value(work.Description) {
		return true, nil
	}
	if in.Category != nil && strings.TrimSpace(*in.Category) != value(work.Category) {
		return true, nil
	}
	if in.Tags != nil && !slices.Equal(normalizeTags(in.Tags), stringSlice(work.TagsJSON)) {
		return true, nil
	}
	if in.CoverAssetID != nil && strings.TrimSpace(*in.CoverAssetID) != value(work.CoverAssetID) {
		return true, nil
	}
	if len(assetIDs) == 0 {
		return false, nil
	}
	current, err := a.listWorkAssetsTx(tx, work.ID)
	if err != nil {
		return false, err
	}
	currentIDs := make([]string, 0, len(current))
	for _, item := range current {
		currentIDs = append(currentIDs, item.AssetID)
	}
	return !slices.Equal(assetIDs, currentIDs), nil
}

func (a *App) validateWorkCategoryTx(tx *gorm.DB, category string) error {
	category = strings.TrimSpace(category)
	if category == "" {
		return nil
	}
	var count int64
	if err := tx.Model(&businesscore.WorkCategory{}).
		Where("category_key = ? AND status = ?", category, "active").
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return bizerrors.New(bizerrors.CodeInvalidArgument, "work category is not active")
	}
	return nil
}

func (a *App) getActiveSnapshot(ctx context.Context, publicWorkID string) (businesscore.WorkPublicSnapshot, error) {
	return a.getActiveSnapshotTx(a.repo.DB().WithContext(ctx), publicWorkID)
}

func (a *App) getActiveSnapshotTx(tx *gorm.DB, publicWorkID string) (businesscore.WorkPublicSnapshot, error) {
	var row businesscore.WorkPublicSnapshot
	err := tx.Where("(public_work_id = ? OR public_slug = ?) AND status = ?", publicWorkID, publicWorkID, SnapshotActive).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return businesscore.WorkPublicSnapshot{}, bizerrors.New(bizerrors.CodeResourceNotFound, "public work not found")
	}
	return row, err
}

func (a *App) publicMediaRefs(publicWorkID, snapshotID string, assets []WorkAssetDTO) []PublicMediaRefDTO {
	out := make([]PublicMediaRefDTO, 0, len(assets))
	for range assets {
		mediaID := security.RandomID("pm_")
		resourceType := "image"
		objectKey := fmt.Sprintf("%s/public/works/%s/snapshots/%s/media/%s/preview", safeEnv(a.opts.Env), publicWorkID, snapshotID, mediaID)
		out = append(out, PublicMediaRefDTO{
			PublicMediaID: mediaID, ResourceType: resourceType, Variant: "preview",
			PublicMediaURL: strings.TrimRight(a.opts.TOSBaseURL, "/") + "/" + objectKey,
		})
	}
	return out
}

func workDTO(row businesscore.Work) WorkDTO {
	return WorkDTO{
		WorkID: row.ID, ProjectID: row.ProjectID, Title: row.Title, Description: value(row.Description), ShareStatus: row.ShareStatus,
		CoverAssetID: value(row.CoverAssetID), Category: value(row.Category), Tags: stringSlice(row.TagsJSON), UpdatedAt: row.UpdatedAt,
	}
}

func publicCardDTO(row businesscore.WorkPublicSnapshot) PublicWorkCardDTO {
	payload := snapshotPayload(row)
	coverURL := ""
	if len(payload.PublicMediaRefs) > 0 {
		coverURL = payload.PublicMediaRefs[0].PublicMediaURL
	}
	return PublicWorkCardDTO{
		PublicWorkID: row.PublicWorkID, Title: payload.Title, CoverURL: coverURL, ShareURL: row.PublicURL, Category: value(row.Category),
		Tags: payload.Tags, ResourceType: value(row.ResourceType), LikeCount: row.LikeCount, PublishedAt: row.PublishedAt,
	}
}

type publicSnapshotPayload struct {
	PublicWorkID      string              `json:"public_work_id"`
	Title             string              `json:"title"`
	Description       string              `json:"description,omitempty"`
	Tags              []string            `json:"tags"`
	ShareURL          string              `json:"share_url"`
	PublicMediaRefs   []PublicMediaRefDTO `json:"public_media_refs"`
	AuthorDisplayName string              `json:"author_display_name,omitempty"`
}

func snapshotPayload(row businesscore.WorkPublicSnapshot) publicSnapshotPayload {
	var payload publicSnapshotPayload
	_ = json.Unmarshal(row.SnapshotPayloadJSON, &payload)
	if payload.PublicWorkID == "" {
		payload.PublicWorkID = row.PublicWorkID
	}
	if payload.ShareURL == "" {
		payload.ShareURL = row.PublicURL
	}
	if len(payload.PublicMediaRefs) == 0 {
		_ = json.Unmarshal(row.PublicMediaRefsJSON, &payload.PublicMediaRefs)
	}
	return payload
}

type sharePreviewTokenPayload struct {
	Kind              string    `json:"kind"`
	WorkID            string    `json:"work_id"`
	ActorUserID       string    `json:"actor_user_id"`
	PublicTitle       string    `json:"public_title"`
	PublicDescription string    `json:"public_description"`
	Tags              []string  `json:"tags"`
	EvidenceID        string    `json:"evidence_id"`
	EvidenceDigest    string    `json:"evidence_digest"`
	ExpiresAt         time.Time `json:"expires_at"`
	Signature         string    `json:"signature,omitempty"`
}

type takedownPreviewTokenPayload struct {
	Kind         string    `json:"kind"`
	PublicWorkID string    `json:"public_work_id"`
	WorkID       string    `json:"work_id"`
	AdminID      string    `json:"admin_id"`
	ReasonDigest string    `json:"reason_digest"`
	NotifyAuthor bool      `json:"notify_author"`
	ExpiresAt    time.Time `json:"expires_at"`
	Signature    string    `json:"signature,omitempty"`
}

func signPreviewToken(payload sharePreviewTokenPayload) (string, error) {
	payload.Signature = signature(payload)
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func verifySharePreviewToken(token string, now time.Time) (sharePreviewTokenPayload, error) {
	var payload sharePreviewTokenPayload
	if err := decodeToken(token, &payload); err != nil {
		return payload, err
	}
	sig := payload.Signature
	payload.Signature = ""
	if sig == "" || sig != signature(payload) || payload.Kind != "work_share" || now.After(payload.ExpiresAt) {
		return payload, bizerrors.New(bizerrors.CodeStateConflict, "share preview token is invalid or expired")
	}
	payload.Signature = sig
	return payload, nil
}

func signTakedownToken(payload takedownPreviewTokenPayload) (string, error) {
	payload.Signature = signature(payload)
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func verifyTakedownToken(token string, now time.Time) (takedownPreviewTokenPayload, error) {
	var payload takedownPreviewTokenPayload
	if err := decodeToken(token, &payload); err != nil {
		return payload, err
	}
	sig := payload.Signature
	payload.Signature = ""
	if sig == "" || sig != signature(payload) || payload.Kind != "work_takedown" || now.After(payload.ExpiresAt) {
		return payload, bizerrors.New(bizerrors.CodeStateConflict, "take-down preview token is invalid or expired")
	}
	payload.Signature = sig
	return payload, nil
}

func decodeToken(token string, out any) error {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return bizerrors.New(bizerrors.CodeStateConflict, "preview token is invalid")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return bizerrors.New(bizerrors.CodeStateConflict, "preview token is invalid")
	}
	return nil
}

func signature(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(append(data, []byte(":dora-m5-preview")...))
	return hex.EncodeToString(sum[:])
}

func ShareTextDigest(title, description string, tags []string) string {
	payload := map[string]any{"title": strings.TrimSpace(title), "description": strings.TrimSpace(description), "tags": normalizeTags(tags)}
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validateWorkShareEvidence(evidence *businessagent.SafetyEvidenceDTO, title, description string, tags []string, now time.Time) error {
	if evidence == nil {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "work share safety evidence is required")
	}
	if evidence.Scene != "work_share" || evidence.TargetType != "work_share_text" || evidence.Result_ != "passed" {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "work share safety evidence must be passed")
	}
	if evidence.EvaluatedObjectDigest != ShareTextDigest(title, description, tags) {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "work share safety evidence digest does not match public text")
	}
	if evidence.ExpiresAt != nil && *evidence.ExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339Nano, *evidence.ExpiresAt)
		if err != nil || now.After(expiresAt) {
			return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "work share safety evidence is expired")
		}
	}
	return nil
}

func auditRecord(traceID, userID, spaceID, action, resourceType, resourceID, result string) *auditlog.AuditRecord {
	return &auditlog.AuditRecord{
		AuditID: security.RandomID("audit_"), TraceID: traceID, OperatorType: "user", OperatorID: &userID, TenantID: "space:" + spaceID,
		SpaceID: &spaceID, BusinessAction: action, ResourceType: resourceType, ResourceID: &resourceID, Result: result,
		MetadataSummary: datatypes.JSON([]byte(`{}`)), CreatedAt: time.Now().UTC(),
	}
}

func adminAuditRecord(traceID, adminID, action, resourceType, resourceID, result, reason string) *auditlog.AuditRecord {
	return &auditlog.AuditRecord{
		AuditID: security.RandomID("audit_"), TraceID: traceID, OperatorType: "admin", OperatorID: &adminID, TenantID: "admin:" + adminID,
		BusinessAction: action, ResourceType: resourceType, ResourceID: &resourceID, Result: result, Reason: optionalString(reason),
		MetadataSummary: datatypes.JSON([]byte(`{}`)), CreatedAt: time.Now().UTC(),
	}
}

func validateAuth(auth AuthContext) error {
	if auth.UserID == "" || auth.SpaceID == "" {
		return bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	return nil
}

func requestHash(meta RequestMeta, auth AuthContext, extra map[string]any) string {
	if meta.RequestHash != "" {
		return meta.RequestHash
	}
	data, _ := json.Marshal(extra)
	return security.HashIdentifier(auth.UserID + ":" + auth.SpaceID + ":" + string(data))
}

func adminRequestHash(meta RequestMeta, auth AdminAuth, extra map[string]any) string {
	if meta.RequestHash != "" {
		return meta.RequestHash
	}
	data, _ := json.Marshal(extra)
	return security.HashIdentifier(auth.AdminID + ":" + string(data))
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

func normalizeIDs(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func normalizeTags(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func stringSlice(raw datatypes.JSON) []string {
	var out []string
	if len(raw) == 0 || json.Unmarshal(raw, &out) != nil {
		return []string{}
	}
	return out
}

func mustJSON(value any) datatypes.JSON {
	data, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte(`{}`))
	}
	return datatypes.JSON(data)
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
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
		return bizerrors.New(bizerrors.CodeStateConflict, "work was updated by another request")
	}
	return nil
}

func allowedActions(work businesscore.Work) []string {
	switch work.ShareStatus {
	case StatusTakenDown:
		return []string{"view", "update"}
	case StatusShared:
		return []string{"view", "unshare"}
	default:
		return []string{"view", "update", "share"}
	}
}

func primaryResourceType(assets []WorkAssetDTO) string {
	if len(assets) == 0 {
		return ""
	}
	return "image"
}

func publicSlug(title, publicWorkID string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "work"
	}
	if len(slug) > 80 {
		slug = strings.Trim(slug[:80], "-")
	}
	return slug + "-" + strings.ToLower(publicWorkID[len("pubw_"):])
}

func safeEnv(env string) string {
	switch strings.TrimSpace(env) {
	case "local", "dev", "staging", "prod":
		return strings.TrimSpace(env)
	default:
		return "local"
	}
}

func textDigest(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func errorCode(err error) string {
	var businessErr *bizerrors.BusinessError
	if errors.As(err, &businessErr) {
		return string(businessErr.Code)
	}
	return string(bizerrors.CodeInternal)
}
