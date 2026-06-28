package asset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/kitex_gen/dora/api/businessagent"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/idempotency"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/infra/repository/businesscore"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/auditlog"
	bizerrors "github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/errors"
	"github.com/FigoGoo/Dora-Agent/services/business/internal/pkg/security"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos/enum"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	StatusActive    = "active"
	StatusPending   = "pending"
	StatusCreated   = "created"
	StatusConfirmed = "confirmed"
	StatusAborted   = "aborted"
	StatusCommitted = "committed"
)

type AuthContext = accountspace.AuthContext
type RequestMeta = accountspace.RequestMeta

type TOSOptions struct {
	Env             string
	Bucket          string
	BaseURL         string
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
}

// UploadURLSigner 为前端直传签发 TOS 预签名 PUT URL。
// 本地无 TOS 凭证(Endpoint/Region/AK/SK 任一为空)时为 nil，uploadURL 回退占位，便于本地与测试不依赖真实 TOS。
type UploadURLSigner interface {
	PresignPut(bucket, objectKey, contentType string, ttl time.Duration) (string, error)
}

type tosPresigner struct{ client *tos.ClientV2 }

func newUploadURLSigner(opts TOSOptions) UploadURLSigner {
	if strings.TrimSpace(opts.Endpoint) == "" || strings.TrimSpace(opts.Region) == "" ||
		strings.TrimSpace(opts.AccessKeyID) == "" || strings.TrimSpace(opts.SecretAccessKey) == "" {
		return nil
	}
	client, err := tos.NewClientV2(opts.Endpoint, tos.WithRegion(opts.Region),
		tos.WithCredentials(tos.NewStaticCredentials(opts.AccessKeyID, opts.SecretAccessKey)))
	if err != nil {
		return nil
	}
	return tosPresigner{client: client}
}

// PresignPut 生成 TOS 直传预签名 URL，限制单 key + Content-Type；不暴露 AK/SK。
func (p tosPresigner) PresignPut(bucket, objectKey, contentType string, ttl time.Duration) (string, error) {
	input := &tos.PreSignedURLInput{
		HTTPMethod: enum.HttpMethodPut,
		Bucket:     bucket,
		Key:        objectKey,
		Expires:    int64(ttl.Seconds()),
	}
	if strings.TrimSpace(contentType) != "" {
		input.Header = map[string]string{"Content-Type": contentType}
	}
	out, err := p.client.PreSignedURL(input)
	if err != nil {
		return "", err
	}
	return out.SignedUrl, nil
}

type App struct {
	repo   *businesscore.Repository
	guard  *idempotency.IdempotencyGuard
	audit  auditlog.Writer
	tos    TOSOptions
	signer UploadURLSigner
	now    func() time.Time
}

func New(repo *businesscore.Repository, guard *idempotency.IdempotencyGuard, audit auditlog.Writer, tos TOSOptions) *App {
	if strings.TrimSpace(tos.Env) == "" {
		tos.Env = "local"
	}
	if strings.TrimSpace(tos.Bucket) == "" {
		tos.Bucket = "dora-public"
	}
	if strings.TrimRight(tos.BaseURL, "/") == "" {
		tos.BaseURL = "http://localhost/tos"
	}
	return &App{repo: repo, guard: guard, audit: audit, tos: tos, signer: newUploadURLSigner(tos), now: func() time.Time { return time.Now().UTC() }}
}

type Page[T any] struct {
	Items  []T   `json:"items"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
	Total  int64 `json:"total"`
}

type AssetCardDTO struct {
	AssetID    string    `json:"asset_id"`
	ProjectID  string    `json:"project_id,omitempty"`
	AssetType  string    `json:"asset_type"`
	SourceType string    `json:"source_type"`
	Status     string    `json:"status"`
	PreviewURL string    `json:"preview_url,omitempty"`
	MIMEType   string    `json:"mime_type,omitempty"`
	SizeBytes  int64     `json:"size_bytes,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type AssetDetailDTO struct {
	Asset           AssetCardDTO      `json:"asset"`
	Elements        []AssetElementDTO `json:"elements"`
	ProjectSummary  map[string]string `json:"project_summary,omitempty"`
	SourceSessionID string            `json:"source_session_id,omitempty"`
	SourceRunID     string            `json:"source_run_id,omitempty"`
	AccessActions   []string          `json:"access_actions"`
}

type AssetElementDTO struct {
	ElementID   string         `json:"element_id"`
	ElementType string         `json:"element_type"`
	Payload     map[string]any `json:"payload"`
	PreviewText string         `json:"preview_text,omitempty"`
}

type UploadIntentDTO struct {
	UploadIntentID string            `json:"upload_intent_id"`
	AssetID        string            `json:"asset_id"`
	Bucket         string            `json:"bucket"`
	ObjectKey      string            `json:"object_key"`
	UploadURL      string            `json:"upload_url"`
	UploadHeaders  map[string]string `json:"upload_headers"`
	ExpiresAt      time.Time         `json:"expires_at"`
	MaxSizeBytes   int64             `json:"max_size_bytes"`
	ContentType    string            `json:"content_type"`
}

type AssetAccessDTO struct {
	AssetID     string    `json:"asset_id"`
	AccessType  string    `json:"access_type"`
	PublicURL   string    `json:"public_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	ContentType string    `json:"content_type"`
	Filename    string    `json:"filename"`
}

type AccessResultDTO struct {
	AssetID      string            `json:"asset_id"`
	Allowed      bool              `json:"allowed"`
	Reason       string            `json:"reason"`
	AssetSummary map[string]string `json:"asset_summary,omitempty"`
}

type GeneratedObjectInput struct {
	ArtifactID      string
	ResourceType    string
	Filename        string
	ContentType     string
	SizeBytes       int64
	Checksum        string
	MetadataSummary map[string]string
}

type GeneratedUploadSlotDTO struct {
	ArtifactID    string            `json:"artifact_id"`
	Bucket        string            `json:"bucket"`
	ObjectKey     string            `json:"object_key"`
	UploadURL     string            `json:"upload_url"`
	UploadHeaders map[string]string `json:"upload_headers"`
	ExpiresAt     time.Time         `json:"expires_at"`
	MaxSizeBytes  int64             `json:"max_size_bytes"`
}

type CreateUploadIntentInput struct {
	Auth           AuthContext
	Meta           RequestMeta
	ProjectID      string
	AssetType      string
	Filename       string
	ContentType    string
	SizeBytes      int64
	Checksum       string
	MetadataText   string
	SafetyEvidence *businessagent.SafetyEvidenceDTO
}

type ConfirmUploadInput struct {
	Auth           AuthContext
	Meta           RequestMeta
	UploadIntentID string
	Etag           string
	SizeBytes      int64
	ContentType    string
	Checksum       string
}

func (a *App) ListAssets(ctx context.Context, auth AuthContext, projectID, assetType, status string, limit, offset int) (Page[AssetCardDTO], error) {
	if err := requireAuth(auth); err != nil {
		return Page[AssetCardDTO]{}, err
	}
	limit, offset = normalizePage(limit, offset, 100)
	db := a.repo.DB().WithContext(ctx).Where("space_id = ? AND owner_user_id = ?", auth.SpaceID, auth.UserID)
	if projectID != "" {
		db = db.Where("project_id = ?", projectID)
	}
	if assetType != "" {
		db = db.Where("asset_type = ?", assetType)
	}
	if status == "" {
		status = StatusActive
	}
	if status != "all" {
		db = db.Where("status = ?", status)
	}
	var rows []businesscore.Asset
	if err := db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return Page[AssetCardDTO]{}, err
	}
	items := make([]AssetCardDTO, 0, len(rows))
	for _, row := range rows {
		card, _ := a.assetCard(ctx, row)
		items = append(items, card)
	}
	var total int64
	_ = db.Model(&businesscore.Asset{}).Count(&total).Error
	return Page[AssetCardDTO]{Items: items, Limit: limit, Offset: offset, Total: total}, nil
}

func (a *App) GetAsset(ctx context.Context, auth AuthContext, assetID string) (AssetDetailDTO, error) {
	asset, err := a.visibleAsset(ctx, auth, assetID)
	if err != nil {
		return AssetDetailDTO{}, err
	}
	card, err := a.assetCard(ctx, asset)
	if err != nil {
		return AssetDetailDTO{}, err
	}
	var elements []businesscore.AssetElement
	if err := a.repo.DB().WithContext(ctx).Where("asset_id = ? AND status = ?", asset.ID, StatusActive).Order("created_at ASC").Find(&elements).Error; err != nil {
		return AssetDetailDTO{}, err
	}
	out := make([]AssetElementDTO, 0, len(elements))
	for _, element := range elements {
		out = append(out, elementDTO(element))
	}
	return AssetDetailDTO{Asset: card, Elements: out, AccessActions: []string{"preview", "download"}}, nil
}

func (a *App) CreateUploadIntent(ctx context.Context, in CreateUploadIntentInput) (UploadIntentDTO, error) {
	if err := requireAuth(in.Auth); err != nil {
		return UploadIntentDTO{}, err
	}
	if in.Meta.IdempotencyKey == "" || in.ProjectID == "" || in.AssetType == "" || in.ContentType == "" || in.SizeBytes <= 0 {
		return UploadIntentDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "upload intent request is incomplete")
	}
	if err := validateSafetyEvidence(in.SafetyEvidence, "asset_upload_metadata", "asset_metadata", in.Meta.TraceID, a.now()); err != nil {
		return UploadIntentDTO{}, err
	}
	if err := a.ensureProjectWritable(ctx, in.Auth, in.ProjectID); err != nil {
		return UploadIntentDTO{}, err
	}
	if err := validateUpload(in.AssetType, in.ContentType, in.SizeBytes); err != nil {
		return UploadIntentDTO{}, err
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{
		"project_id": in.ProjectID, "asset_type": in.AssetType, "content_type": in.ContentType,
		"size_bytes": in.SizeBytes, "checksum": in.Checksum,
		"safety_evidence_id": in.SafetyEvidence.SafetyEvidenceId, "evaluated_object_digest": in.SafetyEvidence.EvaluatedObjectDigest,
	})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "asset.upload_intent", IdempotencyKey: in.Meta.IdempotencyKey, RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID)})
	if err != nil {
		return UploadIntentDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return UploadIntentDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "upload intent idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return UploadIntentDTO{}, bizerrors.New(bizerrors.CodeProcessing, "upload intent request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.getUploadIntentDTO(ctx, decision.ReplayResult.ID)
	}
	var dto UploadIntentDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		assetID := security.RandomID("ast_")
		objectKey := a.objectKey(in.Auth.SpaceID, in.ProjectID, "assets/"+assetID+"/original", assetID, in.ContentType)
		title := strings.TrimSpace(in.Filename)
		asset := businesscore.Asset{
			ID: assetID, AssetNo: "A" + strings.ToUpper(assetID[4:12]), OwnerUserID: in.Auth.UserID, SpaceID: in.Auth.SpaceID,
			EnterpriseID: optionalString(in.Auth.EnterpriseID), ProjectID: &in.ProjectID, AssetType: in.AssetType, Title: optionalString(title),
			Status: StatusPending, Visibility: "private", SourceType: "upload", SourceRefID: optionalString(in.Meta.RequestID),
			ContentDigest: optionalString(in.Checksum), MetadataJSON: mustJSON(map[string]any{"filename_digest": digest(in.Filename), "metadata_text_digest": digest(in.MetadataText)}),
			CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&asset).Error; err != nil {
			return err
		}
		intentID := security.RandomID("upl_")
		intent := businesscore.UploadIntent{
			ID: security.RandomID("ui_"), UploadIntentID: intentID, OwnerUserID: in.Auth.UserID, SpaceID: in.Auth.SpaceID,
			ProjectID: &in.ProjectID, AssetType: in.AssetType, Bucket: &a.tos.Bucket, ObjectKey: &objectKey,
			ObjectKeyHash: digest(objectKey), MIMEType: &in.ContentType, MaxSizeBytes: in.SizeBytes, Status: StatusCreated,
			ExpiresAt: now.Add(15 * time.Minute), ConfirmedAssetID: &assetID, IdempotencyKey: in.Meta.IdempotencyKey,
			TraceID: in.Meta.TraceID, CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&intent).Error; err != nil {
			return err
		}
		dto = a.uploadIntentDTO(intent)
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return UploadIntentDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "upload_intent", ID: dto.UploadIntentID})
	return dto, nil
}

func (a *App) ConfirmUploadIntent(ctx context.Context, in ConfirmUploadInput) (AssetDetailDTO, error) {
	if err := requireAuth(in.Auth); err != nil {
		return AssetDetailDTO{}, err
	}
	if in.Meta.IdempotencyKey == "" || in.UploadIntentID == "" || in.SizeBytes <= 0 || in.ContentType == "" || in.Checksum == "" {
		return AssetDetailDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "confirm upload request is incomplete")
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{"upload_intent_id": in.UploadIntentID, "etag": in.Etag, "size_bytes": in.SizeBytes, "checksum": in.Checksum})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "asset.upload_confirm", IdempotencyKey: in.Meta.IdempotencyKey, RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID)})
	if err != nil {
		return AssetDetailDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return AssetDetailDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "upload confirm idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.GetAsset(ctx, in.Auth, decision.ReplayResult.ID)
	}
	var assetID string
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		var intent businesscore.UploadIntent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("upload_intent_id = ? AND owner_user_id = ? AND space_id = ?", in.UploadIntentID, in.Auth.UserID, in.Auth.SpaceID).First(&intent).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "upload intent not found")
		}
		if intent.Status == StatusConfirmed && intent.ConfirmedAssetID != nil {
			assetID = *intent.ConfirmedAssetID
			return nil
		}
		if intent.Status != StatusCreated || intent.ExpiresAt.Before(now) {
			return bizerrors.New(bizerrors.CodeStateConflict, "upload intent is not confirmable")
		}
		if intent.MIMEType == nil || *intent.MIMEType != in.ContentType || intent.MaxSizeBytes < in.SizeBytes {
			return bizerrors.New(bizerrors.CodeStateConflict, "uploaded object does not match intent")
		}
		if intent.ConfirmedAssetID == nil {
			return bizerrors.New(bizerrors.CodeStateConflict, "upload intent is missing asset")
		}
		assetID = *intent.ConfirmedAssetID
		if err := tx.Model(&businesscore.Asset{}).Where("id = ?", assetID).Updates(map[string]any{"status": StatusActive, "content_digest": in.Checksum, "updated_at": now, "updated_by": in.Auth.UserID}).Error; err != nil {
			return err
		}
		objectKey := value(intent.ObjectKey)
		object := businesscore.AssetStorageObject{
			ID: security.RandomID("aso_"), AssetID: assetID, Bucket: a.tos.Bucket, ObjectKey: optionalString(objectKey),
			ObjectKeyHash: digest(objectKey), ObjectURI: "tos://" + a.tos.Bucket + "/" + objectKey, MIMEType: optionalString(in.ContentType),
			SizeBytes: &in.SizeBytes, Checksum: &in.Checksum, Etag: optionalString(in.Etag), StorageStatus: "available",
			PreviewURI: optionalString(a.publicURL(objectKey)), DownloadPolicy: mustJSON(map[string]any{"access": "signed_by_business"}),
			CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&object).Error; err != nil {
			return err
		}
		intent.Status = StatusConfirmed
		intent.UpdatedBy = optionalString(in.Auth.UserID)
		intent.UpdatedAt = now
		if err := tx.Save(&intent).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return AssetDetailDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "asset", ID: assetID})
	return a.GetAsset(ctx, in.Auth, assetID)
}

func (a *App) AbortUploadIntent(ctx context.Context, auth AuthContext, meta RequestMeta, uploadIntentID string) (UploadIntentDTO, error) {
	if meta.IdempotencyKey == "" {
		return UploadIntentDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "idempotency_key is required")
	}
	var dto UploadIntentDTO
	err := a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var intent businesscore.UploadIntent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("upload_intent_id = ? AND owner_user_id = ?", uploadIntentID, auth.UserID).First(&intent).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "upload intent not found")
		}
		if intent.Status == StatusConfirmed {
			return bizerrors.New(bizerrors.CodeStateConflict, "confirmed upload intent cannot be aborted")
		}
		intent.Status = StatusAborted
		intent.UpdatedBy = optionalString(auth.UserID)
		intent.UpdatedAt = a.now()
		if err := tx.Save(&intent).Error; err != nil {
			return err
		}
		if intent.ConfirmedAssetID != nil {
			_ = tx.Model(&businesscore.Asset{}).Where("id = ?", *intent.ConfirmedAssetID).Updates(map[string]any{"status": "deleted", "updated_at": a.now(), "updated_by": auth.UserID}).Error
		}
		dto = a.uploadIntentDTO(intent)
		return nil
	})
	return dto, err
}

func (a *App) GetAssetAccess(ctx context.Context, auth AuthContext, assetID, accessType string) (AssetAccessDTO, error) {
	asset, err := a.visibleAsset(ctx, auth, assetID)
	if err != nil {
		return AssetAccessDTO{}, err
	}
	if accessType == "" {
		accessType = "preview"
	}
	var object businesscore.AssetStorageObject
	if err := a.repo.DB().WithContext(ctx).Where("asset_id = ? AND storage_status = ?", asset.ID, "available").Order("created_at DESC").First(&object).Error; err != nil {
		return AssetAccessDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "asset storage object not found")
	}
	objectKey := value(object.ObjectKey)
	if objectKey == "" {
		objectKey = strings.TrimPrefix(object.ObjectURI, "tos://"+object.Bucket+"/")
	}
	_ = a.repo.DB().WithContext(ctx).Create(&businesscore.AssetAccessLog{
		ID: security.RandomID("aal_"), AssetID: asset.ID, ActorUserID: auth.UserID, SpaceID: auth.SpaceID, ProjectID: asset.ProjectID,
		AccessPurpose: accessType, Allowed: true, TraceID: optionalString(""), CreatedAt: a.now(),
	}).Error
	return AssetAccessDTO{
		AssetID: asset.ID, AccessType: accessType, PublicURL: a.publicURL(objectKey), ExpiresAt: a.now().Add(15 * time.Minute),
		ContentType: value(object.MIMEType), Filename: asset.ID + extensionForContentType(value(object.MIMEType)),
	}, nil
}

func (a *App) BatchCheckAssetAccess(ctx context.Context, auth AuthContext, projectID string, assetIDs []string, purpose string) ([]AccessResultDTO, error) {
	if err := requireAuth(auth); err != nil {
		return nil, err
	}
	if projectID == "" || len(assetIDs) == 0 {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "project_id and asset_ids are required")
	}
	unique := uniqueStrings(assetIDs)
	var rows []businesscore.Asset
	if err := a.repo.DB().WithContext(ctx).Where("id IN ?", unique).Find(&rows).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]businesscore.Asset, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	out := make([]AccessResultDTO, 0, len(unique))
	for _, id := range unique {
		row, ok := byID[id]
		result := AccessResultDTO{AssetID: id, Allowed: false, Reason: "asset_not_found"}
		if ok {
			result = a.checkAssetAccess(row, auth, projectID, purpose)
		}
		out = append(out, result)
	}
	return out, nil
}

func (a *App) PrepareGeneratedAssetObjects(ctx context.Context, auth AuthContext, meta RequestMeta, projectID, sessionID, runID string, artifacts []GeneratedObjectInput) ([]GeneratedUploadSlotDTO, error) {
	if err := requireAuth(auth); err != nil {
		return nil, err
	}
	if meta.IdempotencyKey == "" || projectID == "" || sessionID == "" || runID == "" || len(artifacts) == 0 {
		return nil, bizerrors.New(bizerrors.CodeInvalidArgument, "prepare generated asset objects request is incomplete")
	}
	if err := a.ensureProjectWritable(ctx, auth, projectID); err != nil {
		return nil, err
	}
	hash := requestHash(meta, auth, map[string]any{"project_id": projectID, "session_id": sessionID, "run_id": runID, "artifacts": artifacts})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{TenantID: "space:" + auth.SpaceID, SpaceID: auth.SpaceID, Scope: "asset.generated_objects.prepare", IdempotencyKey: meta.IdempotencyKey, RequestHash: hash, ActorUserID: auth.UserID, EnterpriseID: optionalString(auth.EnterpriseID)})
	if err != nil {
		return nil, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return nil, bizerrors.New(bizerrors.CodeIdempotencyConflict, "generated object slot idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return nil, bizerrors.New(bizerrors.CodeProcessing, "generated object slot request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay {
		return a.listGeneratedSlots(ctx, runID, artifacts)
	}
	var slots []GeneratedUploadSlotDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		for _, artifact := range artifacts {
			if err := validateGeneratedArtifact(artifact); err != nil {
				return err
			}
			var existing businesscore.GeneratedAssetObjectSlot
			err := tx.Where("run_id = ? AND artifact_id = ?", runID, artifact.ArtifactID).First(&existing).Error
			if err == nil {
				if existing.ContentType != artifact.ContentType || existing.SizeBytes != artifact.SizeBytes {
					return bizerrors.New(bizerrors.CodeIdempotencyConflict, "artifact slot exists with different object constraints")
				}
				slots = append(slots, a.generatedSlotDTO(existing))
				continue
			}
			if err != nil && err != gorm.ErrRecordNotFound {
				return err
			}
			objectKey := a.objectKey(auth.SpaceID, projectID, "runs/"+runID+"/artifacts/"+artifact.ArtifactID+"/outputs", artifact.ArtifactID, artifact.ContentType)
			row := businesscore.GeneratedAssetObjectSlot{
				ID: security.RandomID("gaos_"), SlotID: security.RandomID("slot_"), ProjectID: projectID, SessionID: sessionID, RunID: runID,
				ArtifactID: artifact.ArtifactID, ResourceType: artifact.ResourceType, Bucket: a.tos.Bucket, ObjectKey: objectKey,
				ObjectKeyHash: digest(objectKey), ContentType: artifact.ContentType, SizeBytes: artifact.SizeBytes, Checksum: optionalStringValue(artifact.Checksum),
				Status: StatusCreated, IdempotencyKey: meta.IdempotencyKey, MetadataJSON: mustJSON(artifact.MetadataSummary), ExpiresAt: now.Add(15 * time.Minute),
				CreatedByUserID: auth.UserID, TraceID: meta.TraceID, CreatedBy: optionalString(auth.UserID), UpdatedBy: optionalString(auth.UserID), CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			slots = append(slots, a.generatedSlotDTO(row))
		}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return nil, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "generated_object_slots", ID: runID})
	return slots, nil
}

func (a *App) ensureProjectWritable(ctx context.Context, auth AuthContext, projectID string) error {
	var project businesscore.Project
	err := a.repo.DB().WithContext(ctx).Where("id = ? AND space_id = ?", projectID, auth.SpaceID).First(&project).Error
	if err != nil {
		return bizerrors.New(bizerrors.CodeProjectNotFound, "project not found")
	}
	if project.OwnerUserID != auth.UserID {
		return bizerrors.New(bizerrors.CodePermissionDenied, "project belongs to another user")
	}
	if project.Status == "archived" || !project.CreativeAllowed {
		return bizerrors.New(bizerrors.CodeProjectArchived, "project is archived")
	}
	return nil
}

func (a *App) visibleAsset(ctx context.Context, auth AuthContext, assetID string) (businesscore.Asset, error) {
	if err := requireAuth(auth); err != nil {
		return businesscore.Asset{}, err
	}
	var asset businesscore.Asset
	if err := a.repo.DB().WithContext(ctx).Where("id = ? AND status = ?", assetID, StatusActive).First(&asset).Error; err != nil {
		return businesscore.Asset{}, bizerrors.New(bizerrors.CodeResourceNotFound, "asset not found")
	}
	if asset.SpaceID != auth.SpaceID || asset.OwnerUserID != auth.UserID {
		return businesscore.Asset{}, bizerrors.New(bizerrors.CodePermissionDenied, "asset access denied")
	}
	return asset, nil
}

func (a *App) checkAssetAccess(asset businesscore.Asset, auth AuthContext, projectID, purpose string) AccessResultDTO {
	result := AccessResultDTO{AssetID: asset.ID, Allowed: false, Reason: "permission_denied"}
	if asset.Status != StatusActive {
		result.Reason = "asset_unavailable"
		return result
	}
	if asset.SpaceID != auth.SpaceID || asset.OwnerUserID != auth.UserID {
		result.Reason = "cross_space_or_owner_denied"
		return result
	}
	if projectID != "" && asset.ProjectID != nil && *asset.ProjectID != projectID {
		result.Reason = "project_mismatch"
		return result
	}
	result.Allowed = true
	result.Reason = "allowed"
	result.AssetSummary = map[string]string{"asset_type": asset.AssetType, "display_name": value(asset.Title), "purpose": purpose}
	return result
}

func (a *App) assetCard(ctx context.Context, asset businesscore.Asset) (AssetCardDTO, error) {
	card := AssetCardDTO{AssetID: asset.ID, AssetType: asset.AssetType, SourceType: asset.SourceType, Status: asset.Status, CreatedAt: asset.CreatedAt}
	if asset.ProjectID != nil {
		card.ProjectID = *asset.ProjectID
	}
	var object businesscore.AssetStorageObject
	if err := a.repo.DB().WithContext(ctx).Where("asset_id = ? AND storage_status = ?", asset.ID, "available").Order("created_at DESC").First(&object).Error; err == nil {
		card.PreviewURL = value(object.PreviewURI)
		card.MIMEType = value(object.MIMEType)
		if object.SizeBytes != nil {
			card.SizeBytes = *object.SizeBytes
		}
	}
	return card, nil
}

func (a *App) getUploadIntentDTO(ctx context.Context, uploadIntentID string) (UploadIntentDTO, error) {
	var row businesscore.UploadIntent
	if err := a.repo.DB().WithContext(ctx).Where("upload_intent_id = ?", uploadIntentID).First(&row).Error; err != nil {
		return UploadIntentDTO{}, err
	}
	return a.uploadIntentDTO(row), nil
}

func (a *App) listGeneratedSlots(ctx context.Context, runID string, artifacts []GeneratedObjectInput) ([]GeneratedUploadSlotDTO, error) {
	ids := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		ids = append(ids, artifact.ArtifactID)
	}
	var rows []businesscore.GeneratedAssetObjectSlot
	if err := a.repo.DB().WithContext(ctx).Where("run_id = ? AND artifact_id IN ?", runID, ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]GeneratedUploadSlotDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, a.generatedSlotDTO(row))
	}
	return out, nil
}

func (a *App) uploadIntentDTO(row businesscore.UploadIntent) UploadIntentDTO {
	objectKey := value(row.ObjectKey)
	return UploadIntentDTO{
		UploadIntentID: row.UploadIntentID, AssetID: value(row.ConfirmedAssetID), Bucket: value(row.Bucket), ObjectKey: objectKey,
		UploadURL: a.uploadURL(objectKey, value(row.MIMEType), row.ExpiresAt), UploadHeaders: uploadHeaders(value(row.MIMEType), row.MaxSizeBytes),
		ExpiresAt: row.ExpiresAt, MaxSizeBytes: row.MaxSizeBytes, ContentType: value(row.MIMEType),
	}
}

func (a *App) generatedSlotDTO(row businesscore.GeneratedAssetObjectSlot) GeneratedUploadSlotDTO {
	return GeneratedUploadSlotDTO{
		ArtifactID: row.ArtifactID, Bucket: row.Bucket, ObjectKey: row.ObjectKey, UploadURL: a.uploadURL(row.ObjectKey, row.ContentType, row.ExpiresAt),
		UploadHeaders: uploadHeaders(row.ContentType, row.SizeBytes), ExpiresAt: row.ExpiresAt, MaxSizeBytes: row.SizeBytes,
	}
}

func (a *App) objectKey(spaceID, projectID, path, id, contentType string) string {
	return strings.Trim(a.tos.Env, "/") + "/spaces/" + safeSegment(spaceID) + "/projects/" + safeSegment(projectID) + "/" + strings.Trim(path, "/") + "/" + safeSegment(id) + extensionForContentType(contentType)
}

func (a *App) uploadURL(objectKey, contentType string, expiresAt time.Time) string {
	if a.signer != nil {
		if url, err := a.signer.PresignPut(a.tos.Bucket, objectKey, contentType, clampUploadTTL(expiresAt.Sub(a.now()))); err == nil && url != "" {
			return url
		}
	}
	return strings.TrimRight(a.tos.BaseURL, "/") + "/" + strings.TrimLeft(objectKey, "/") + "?upload_token=local-m4"
}

// clampUploadTTL 把上传凭证有效期约束到规范要求的 5-15 分钟。
func clampUploadTTL(d time.Duration) time.Duration {
	const minTTL, maxTTL = 5 * time.Minute, 15 * time.Minute
	if d < minTTL {
		return minTTL
	}
	if d > maxTTL {
		return maxTTL
	}
	return d
}

func (a *App) publicURL(objectKey string) string {
	return strings.TrimRight(a.tos.BaseURL, "/") + "/" + strings.TrimLeft(objectKey, "/")
}

func validateUpload(assetType, contentType string, sizeBytes int64) error {
	limits := map[string]int64{"image": 20 << 20, "music": 100 << 20, "audio": 100 << 20, "video": 500 << 20, "file": 50 << 20}
	if max := limits[assetType]; max > 0 && sizeBytes > max {
		return bizerrors.New(bizerrors.CodeInvalidArgument, "file size exceeds asset type limit")
	}
	if !contentTypeAllowed(assetType, contentType) {
		return bizerrors.New(bizerrors.CodeInvalidArgument, "content type is not supported")
	}
	return nil
}

func validateGeneratedArtifact(item GeneratedObjectInput) error {
	if item.ArtifactID == "" || item.ResourceType == "" || item.ContentType == "" || item.SizeBytes <= 0 {
		return bizerrors.New(bizerrors.CodeInvalidArgument, "generated artifact is incomplete")
	}
	return validateUpload(assetTypeFromResource(item.ResourceType), item.ContentType, item.SizeBytes)
}

func validateSafetyEvidence(evidence *businessagent.SafetyEvidenceDTO, expectedScene, expectedTargetType, expectedTraceID string, now time.Time) error {
	if evidence == nil {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence is required")
	}
	if evidence.Result_ != "passed" {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence must be passed")
	}
	if evidence.Scene != expectedScene || evidence.TargetType != expectedTargetType {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence scene or target_type is invalid")
	}
	if strings.TrimSpace(evidence.SafetyEvidenceId) == "" || !strings.HasPrefix(evidence.EvaluatedObjectDigest, "sha256:") ||
		strings.TrimSpace(evidence.PolicyVersion) == "" || strings.TrimSpace(evidence.EvidenceVersion) == "" ||
		strings.TrimSpace(evidence.EvaluatedAt) == "" || strings.TrimSpace(evidence.TraceId) == "" {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence fields are incomplete")
	}
	if expectedTraceID != "" && evidence.TraceId != expectedTraceID {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence trace_id does not match request")
	}
	if _, err := time.Parse(time.RFC3339Nano, evidence.EvaluatedAt); err != nil {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence evaluated_at is invalid")
	}
	if evidence.ExpiresAt != nil && *evidence.ExpiresAt != "" {
		expires, err := time.Parse(time.RFC3339Nano, *evidence.ExpiresAt)
		if err != nil {
			return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence expires_at is invalid")
		}
		if !expires.After(now) {
			return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence is expired")
		}
	}
	return nil
}

func contentTypeAllowed(assetType, contentType string) bool {
	switch assetType {
	case "image":
		return contentType == "image/jpeg" || contentType == "image/png" || contentType == "image/webp"
	case "music", "audio":
		return contentType == "audio/mpeg" || contentType == "audio/wav" || contentType == "audio/x-wav" || contentType == "audio/mp4"
	case "video":
		return contentType == "video/mp4" || contentType == "video/quicktime" || contentType == "video/webm"
	case "file":
		return strings.HasPrefix(contentType, "text/") || contentType == "application/pdf" || strings.Contains(contentType, "word")
	default:
		return false
	}
}

func assetTypeFromResource(resourceType string) string {
	if resourceType == "audio" {
		return "music"
	}
	return resourceType
}

func extensionForContentType(contentType string) string {
	if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "video/mp4":
		return ".mp4"
	case "audio/mpeg":
		return ".mp3"
	}
	ext := filepath.Ext(contentType)
	if ext != "" {
		return ext
	}
	return ".bin"
}

func uploadHeaders(contentType string, sizeBytes int64) map[string]string {
	return map[string]string{"Content-Type": contentType, "X-Dora-Max-Size": strconvFormat(sizeBytes)}
}

func elementDTO(row businesscore.AssetElement) AssetElementDTO {
	payload := map[string]any{}
	_ = json.Unmarshal(row.ElementSummaryJSON, &payload)
	return AssetElementDTO{ElementID: row.ID, ElementType: row.ElementType, Payload: payload, PreviewText: value(row.PreviewText)}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func requestHash(meta RequestMeta, auth AuthContext, extra map[string]any) string {
	if meta.RequestHash != "" {
		return meta.RequestHash
	}
	data, _ := json.Marshal(map[string]any{"space_id": auth.SpaceID, "actor_user_id": auth.UserID, "enterprise_id": auth.EnterpriseID, "extra": extra})
	return digest(string(data))
}

func requireAuth(auth AuthContext) error {
	if auth.UserID == "" || auth.SpaceID == "" {
		return bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	return nil
}

func mustJSON(value any) datatypes.JSON {
	data, err := json.Marshal(value)
	if err != nil {
		return datatypes.JSON([]byte(`{}`))
	}
	return datatypes.JSON(data)
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func safeSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '/' {
			return r
		}
		return '-'
	}, value)
	return strings.Trim(value, "-/")
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func optionalStringValue(value string) *string {
	return optionalString(value)
}

func value(ptr *string) string {
	if ptr == nil {
		return ""
	}
	return *ptr
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

func errorCode(err error) string {
	if biz := bizerrors.FromError(err); biz != nil {
		return string(biz.Code)
	}
	return "INTERNAL_ERROR"
}

func strconvFormat(value int64) string {
	return strconv.FormatInt(value, 10)
}
