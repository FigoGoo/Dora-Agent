package assetcommit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuthContext = accountspace.AuthContext
type RequestMeta = accountspace.RequestMeta

type App struct {
	repo     *businesscore.Repository
	guard    *idempotency.IdempotencyGuard
	audit    auditlog.Writer
	verifier ObjectVerifier
	now      func() time.Time
}

func New(repo *businesscore.Repository, guard *idempotency.IdempotencyGuard, audit auditlog.Writer, verifiers ...ObjectVerifier) *App {
	verifier := ObjectVerifier(MetadataObjectVerifier{})
	if len(verifiers) > 0 && verifiers[0] != nil {
		verifier = verifiers[0]
	}
	return &App{repo: repo, guard: guard, audit: audit, verifier: verifier, now: func() time.Time { return time.Now().UTC() }}
}

type ObjectExpectation struct {
	Bucket      string
	ObjectKey   string
	ContentType string
	SizeBytes   int64
	Checksum    string
}

type VerifiedObject struct {
	Bucket      string
	ObjectKey   string
	ContentType string
	SizeBytes   int64
	Checksum    string
	Etag        string
}

type ObjectVerifier interface {
	VerifyGeneratedObject(ctx context.Context, expected ObjectExpectation, actual StorageObjectRef) (VerifiedObject, error)
}

type MetadataObjectVerifier struct{}

func (MetadataObjectVerifier) VerifyGeneratedObject(ctx context.Context, expected ObjectExpectation, actual StorageObjectRef) (VerifiedObject, error) {
	if err := ctx.Err(); err != nil {
		return VerifiedObject{}, err
	}
	if actual.Bucket != expected.Bucket || actual.ObjectKey != expected.ObjectKey ||
		actual.ContentType != expected.ContentType || actual.SizeBytes != expected.SizeBytes ||
		actual.Checksum != expected.Checksum {
		return VerifiedObject{}, bizerrors.New(bizerrors.CodeAssetSaveFailed, "uploaded object metadata does not match generated slot")
	}
	if strings.TrimSpace(actual.Etag) == "" || strings.HasPrefix(strings.ToLower(strings.TrimSpace(actual.Etag)), "local-") {
		return VerifiedObject{}, bizerrors.New(bizerrors.CodeAssetSaveFailed, "uploaded object is not verifiable")
	}
	return VerifiedObject{
		Bucket: actual.Bucket, ObjectKey: actual.ObjectKey, ContentType: actual.ContentType,
		SizeBytes: actual.SizeBytes, Checksum: actual.Checksum, Etag: normalizeETag(actual.Etag),
	}, nil
}

type TOSHeadObjectVerifier struct {
	client *tos.ClientV2
}

func NewTOSHeadObjectVerifier(endpoint, region, accessKeyID, secretAccessKey string) (ObjectVerifier, error) {
	if strings.TrimSpace(endpoint) == "" || strings.TrimSpace(region) == "" || strings.TrimSpace(accessKeyID) == "" || strings.TrimSpace(secretAccessKey) == "" {
		return nil, nil
	}
	client, err := tos.NewClientV2(endpoint, tos.WithRegion(region), tos.WithCredentials(tos.NewStaticCredentials(accessKeyID, secretAccessKey)))
	if err != nil {
		return nil, err
	}
	return TOSHeadObjectVerifier{client: client}, nil
}

func (v TOSHeadObjectVerifier) VerifyGeneratedObject(ctx context.Context, expected ObjectExpectation, actual StorageObjectRef) (VerifiedObject, error) {
	if v.client == nil {
		return MetadataObjectVerifier{}.VerifyGeneratedObject(ctx, expected, actual)
	}
	head, err := v.client.HeadObjectV2(ctx, &tos.HeadObjectV2Input{Bucket: expected.Bucket, Key: expected.ObjectKey})
	if err != nil {
		return VerifiedObject{}, bizerrors.New(bizerrors.CodeAssetSaveFailed, "tos head object failed")
	}
	if head.ContentLength != expected.SizeBytes || head.ContentType != expected.ContentType {
		return VerifiedObject{}, bizerrors.New(bizerrors.CodeAssetSaveFailed, "tos object metadata does not match generated slot")
	}
	if actual.Bucket != expected.Bucket || actual.ObjectKey != expected.ObjectKey ||
		actual.ContentType != expected.ContentType || actual.SizeBytes != expected.SizeBytes ||
		actual.Checksum != expected.Checksum {
		return VerifiedObject{}, bizerrors.New(bizerrors.CodeAssetSaveFailed, "uploaded object metadata does not match generated slot")
	}
	if actual.Etag != "" && normalizeETag(head.ETag) != normalizeETag(actual.Etag) {
		return VerifiedObject{}, bizerrors.New(bizerrors.CodeAssetSaveFailed, "tos object etag does not match upload result")
	}
	return VerifiedObject{
		Bucket: expected.Bucket, ObjectKey: expected.ObjectKey, ContentType: head.ContentType,
		SizeBytes: head.ContentLength, Checksum: actual.Checksum, Etag: normalizeETag(defaultString(actual.Etag, head.ETag)),
	}, nil
}

type StorageObjectRef struct {
	ObjectKey   string
	Bucket      string
	ContentType string
	SizeBytes   int64
	Checksum    string
	Etag        string
}

type CommitArtifactInput struct {
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
	StorageObjectRef StorageObjectRef
}

type FinalElementInput struct {
	ElementType        string
	ElementPayloadJSON string
	DisplayOrder       int32
	SourceToolCallID   string
}

type CommitInput struct {
	Auth           AuthContext
	Meta           RequestMeta
	ProjectID      string
	SessionID      string
	RunID          string
	FreezeID       string
	EstimateID     string
	Artifacts      []CommitArtifactInput
	FinalElements  []FinalElementInput
	SafetyEvidence *businessagent.SafetyEvidenceDTO
}

type CommittedAssetRefDTO struct {
	AssetID             string `json:"asset_id"`
	SourceArtifactID    string `json:"source_artifact_id"`
	ResourceType        string `json:"resource_type"`
	AssetType           string `json:"asset_type"`
	Status              string `json:"status"`
	PreviewURL          string `json:"preview_url,omitempty"`
	ElementsSummaryJSON string `json:"elements_summary_json,omitempty"`
}

type ChargedLineItemDTO struct {
	EstimateItemID string `json:"estimate_item_id"`
	ChargedPoints  int64  `json:"charged_points"`
	Status         string `json:"status"`
	AssetID        string `json:"asset_id,omitempty"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	ArtifactID     string `json:"artifact_id,omitempty"`
}

type CommitDTO struct {
	AssetRefs        []CommittedAssetRefDTO `json:"asset_refs"`
	ChargedPoints    int64                  `json:"charged_points"`
	ReleasedPoints   int64                  `json:"released_points"`
	CommitStatus     string                 `json:"commit_status"`
	LedgerRef        string                 `json:"ledger_ref,omitempty"`
	ChargedLineItems []ChargedLineItemDTO   `json:"charged_line_items,omitempty"`
}

type artifactCommitError struct {
	line ChargedLineItemDTO
	err  error
}

func (e artifactCommitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e artifactCommitError) Unwrap() error {
	return e.err
}

func (a *App) CommitGeneratedAssetAndCharge(ctx context.Context, in CommitInput) (CommitDTO, error) {
	if in.Auth.UserID == "" || in.Auth.SpaceID == "" {
		return CommitDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	if in.Meta.IdempotencyKey == "" || in.ProjectID == "" || in.SessionID == "" || in.RunID == "" || in.FreezeID == "" || len(in.Artifacts) == 0 {
		return CommitDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "asset commit request is incomplete")
	}
	if err := validateSafetyEvidence(in.SafetyEvidence, in.SessionID, in.RunID, in.Meta.TraceID, a.now()); err != nil {
		return CommitDTO{}, err
	}
	hash := requestHash(in.Meta, in.Auth, map[string]any{"project_id": in.ProjectID, "run_id": in.RunID, "freeze_id": in.FreezeID, "artifacts": in.Artifacts, "elements": in.FinalElements})
	decision, err := a.guard.Begin(ctx, idempotency.BeginInput{
		TenantID: "space:" + in.Auth.SpaceID, SpaceID: in.Auth.SpaceID, Scope: "asset.commit_charge", IdempotencyKey: in.Meta.IdempotencyKey,
		RequestHash: hash, ActorUserID: in.Auth.UserID, EnterpriseID: optionalString(in.Auth.EnterpriseID),
	})
	if err != nil {
		return CommitDTO{}, err
	}
	if decision.Mode == idempotency.DecisionConflict {
		return CommitDTO{}, bizerrors.New(bizerrors.CodeIdempotencyConflict, "asset commit idempotency key conflicts")
	}
	if decision.Mode == idempotency.DecisionProcessing {
		return CommitDTO{}, bizerrors.New(bizerrors.CodeProcessing, "asset commit request is processing")
	}
	if decision.Mode == idempotency.DecisionReplay && decision.ReplayResult != nil {
		return a.getCommitDTO(ctx, decision.ReplayResult.ID)
	}
	var dto CommitDTO
	err = a.repo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := a.now()
		if err := a.ensureProjectWritable(tx, in.Auth, in.ProjectID); err != nil {
			return err
		}
		freeze, account, err := a.lockFreezeAndAccount(tx, in.FreezeID)
		if err != nil {
			return err
		}
		if freeze.ProjectID != in.ProjectID || freeze.RunID != in.RunID {
			return bizerrors.New(bizerrors.CodeStateConflict, "asset commit does not match freeze")
		}
		if in.EstimateID != "" && freeze.EstimateID != in.EstimateID {
			return bizerrors.New(bizerrors.CodeStateConflict, "asset commit estimate does not match freeze")
		}
		var estimate businesscore.CreditEstimate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("estimate_id = ?", freeze.EstimateID).First(&estimate).Error; err != nil {
			return bizerrors.New(bizerrors.CodeResourceNotFound, "credit estimate not found")
		}
		if estimate.AccountID != freeze.AccountID || estimate.ProjectID != in.ProjectID {
			return bizerrors.New(bizerrors.CodeStateConflict, "asset commit estimate does not match freeze")
		}
		if err := validateSafetyEvidence(in.SafetyEvidence, in.SessionID, in.RunID, in.Meta.TraceID, a.now()); err != nil {
			return err
		}
		if estimate.SafetyEvidenceHash == nil || *estimate.SafetyEvidenceHash != safetyDigest(in.SafetyEvidence) {
			return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence does not match credit estimate")
		}
		commitID := security.RandomID("acm_")
		assetRefs := make([]CommittedAssetRefDTO, 0, len(in.Artifacts))
		lineItems := make([]ChargedLineItemDTO, 0, len(in.Artifacts))
		var charged int64
		var firstArtifactErr error
		for _, artifact := range in.Artifacts {
			assetRef, line, err := a.commitArtifact(ctx, tx, in, freeze, commitID, artifact, now)
			if err != nil {
				if partial, ok := err.(artifactCommitError); ok {
					if firstArtifactErr == nil {
						firstArtifactErr = partial.err
					}
					lineItems = append(lineItems, partial.line)
					continue
				}
				return err
			}
			assetRefs = append(assetRefs, assetRef)
			lineItems = append(lineItems, line)
			charged += line.ChargedPoints
		}
		if len(assetRefs) == 0 && firstArtifactErr != nil {
			return firstArtifactErr
		}
		unsettled := freeze.FrozenPoints - freeze.ChargedPoints - freeze.ReleasedPoints - charged
		if unsettled < 0 {
			return bizerrors.New(bizerrors.CodeStateConflict, "asset commit charge exceeds frozen points")
		}
		released, err := a.releaseUnused(tx, &freeze, &account, unsettled, now)
		if err != nil {
			return err
		}
		account.FrozenPoints -= charged
		account.UpdatedAt = now
		freeze.ChargedPoints += charged
		freeze.ReleasedPoints += released
		if freeze.ChargedPoints+freeze.ReleasedPoints >= freeze.FrozenPoints {
			freeze.Status = "charged"
		}
		freeze.UpdatedAt = now
		if err := tx.Save(&account).Error; err != nil {
			return err
		}
		if err := tx.Save(&freeze).Error; err != nil {
			return err
		}
		ledgerID := security.RandomID("cled_")
		if err := tx.Create(&businesscore.CreditLedgerEntry{
			ID: ledgerID, AccountID: account.ID, EntryType: "asset_commit_charge", PointsDelta: -charged,
			BalanceAfter: account.AvailablePoints, FrozenAfter: account.FrozenPoints, SourceType: "asset_commit",
			SourceID: commitID, ProjectID: &in.ProjectID, RunID: &in.RunID, TraceID: optionalString(in.Meta.TraceID),
			IdempotencyKey: optionalString(in.Meta.IdempotencyKey), MetadataJSON: mustJSON(map[string]any{"asset_count": len(assetRefs), "charged_line_items": lineItems}), CreatedAt: now,
		}).Error; err != nil {
			return err
		}
		estimateID := optionalString(in.EstimateID)
		if estimateID == nil {
			estimateID = optionalString(freeze.EstimateID)
		}
		batch := businesscore.AssetCommitBatch{
			ID: security.RandomID("acmb_"), CommitID: commitID, ProjectID: in.ProjectID, SessionID: in.SessionID,
			RunID: in.RunID, FreezeID: in.FreezeID, EstimateID: estimateID, ActorUserID: in.Auth.UserID, SpaceID: in.Auth.SpaceID,
			SafetyEvidenceID: in.SafetyEvidence.SafetyEvidenceId, SafetyEvidenceHash: safetyDigest(in.SafetyEvidence),
			ChargedPoints: charged, ReleasedPoints: released, CommitStatus: commitStatusForSkipped(lineItems), LedgerRef: &ledgerID,
			IdempotencyKey: in.Meta.IdempotencyKey, TraceID: in.Meta.TraceID,
			CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&batch).Error; err != nil {
			return err
		}
		dto = CommitDTO{AssetRefs: assetRefs, ChargedPoints: charged, ReleasedPoints: released, CommitStatus: batch.CommitStatus, LedgerRef: ledgerID, ChargedLineItems: lineItems}
		return nil
	})
	if err != nil {
		_ = a.guard.Fail(ctx, decision.Record.ID, errorCode(err))
		return CommitDTO{}, err
	}
	_ = a.guard.Succeed(ctx, decision.Record.ID, idempotency.ResultRef{Type: "asset_commit", ID: dto.LedgerRef})
	return dto, nil
}

func (a *App) commitArtifact(ctx context.Context, tx *gorm.DB, in CommitInput, freeze businesscore.CreditFreeze, commitID string, artifact CommitArtifactInput, now time.Time) (CommittedAssetRefDTO, ChargedLineItemDTO, error) {
	if artifact.ArtifactID == "" || artifact.ResourceType == "" || artifact.ElementType == "" || artifact.StorageObjectRef.ObjectKey == "" {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "commit artifact is incomplete")
	}
	if artifact.EstimateItemID == "" {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "estimate_item_id is required for generated asset commit")
	}
	if err := ensureEstimateItemUnsettled(tx, artifact.EstimateItemID); err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	var estimateItem businesscore.CreditEstimateItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("estimate_item_id = ?", artifact.EstimateItemID).First(&estimateItem).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeResourceNotFound, "estimate item not found")
	}
	if estimateItem.EstimateID != freeze.EstimateID {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "estimate item does not belong to current freeze")
	}
	if estimateItem.ItemType != "model_generation" && estimateItem.ItemType != "asset_generation" {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "estimate item is not chargeable by asset commit")
	}
	if estimateItem.ResourceType != nil && *estimateItem.ResourceType != "" && *estimateItem.ResourceType != artifact.ResourceType {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "estimate item resource type does not match artifact")
	}
	var slot businesscore.GeneratedAssetObjectSlot
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND artifact_id = ?", in.RunID, artifact.ArtifactID).First(&slot).Error
	if err != nil {
		return skippedArtifact(artifact, estimateItem, bizerrors.New(bizerrors.CodeStateConflict, "generated object slot not found"))
	}
	if slot.ExpiresAt.Before(now) || (slot.Status != "created" && slot.Status != "uploaded") {
		return skippedArtifact(artifact, estimateItem, bizerrors.New(bizerrors.CodeStateConflict, "generated object slot is not committable"))
	}
	if slot.ObjectKey != artifact.StorageObjectRef.ObjectKey || slot.Bucket != artifact.StorageObjectRef.Bucket || slot.ContentType != artifact.StorageObjectRef.ContentType || slot.SizeBytes != artifact.StorageObjectRef.SizeBytes {
		return skippedArtifact(artifact, estimateItem, bizerrors.New(bizerrors.CodeStateConflict, "storage object does not match generated slot"))
	}
	if slot.Checksum != nil && *slot.Checksum != "" && artifact.StorageObjectRef.Checksum != "" && *slot.Checksum != artifact.StorageObjectRef.Checksum {
		return skippedArtifact(artifact, estimateItem, bizerrors.New(bizerrors.CodeStateConflict, "storage checksum does not match generated slot"))
	}
	verified, err := a.verifier.VerifyGeneratedObject(ctx, ObjectExpectation{
		Bucket: slot.Bucket, ObjectKey: slot.ObjectKey, ContentType: slot.ContentType,
		SizeBytes: slot.SizeBytes, Checksum: value(slot.Checksum),
	}, artifact.StorageObjectRef)
	if err != nil {
		return skippedArtifact(artifact, estimateItem, err)
	}
	if verified.Etag == "" {
		return skippedArtifact(artifact, estimateItem, bizerrors.New(bizerrors.CodeAssetSaveFailed, "uploaded object is missing etag"))
	}
	slot.Status = "uploaded"
	slot.Etag = optionalString(verified.Etag)
	slot.UpdatedBy = optionalString(in.Auth.UserID)
	slot.UpdatedAt = now
	if err := tx.Save(&slot).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	assetID := security.RandomID("ast_")
	title := artifact.ArtifactSummary["display_name"]
	asset := businesscore.Asset{
		ID: assetID, AssetNo: "A" + strings.ToUpper(assetID[4:12]), OwnerUserID: in.Auth.UserID, SpaceID: in.Auth.SpaceID,
		EnterpriseID: optionalString(in.Auth.EnterpriseID), ProjectID: &in.ProjectID, AssetType: assetTypeFromResource(artifact.ResourceType),
		Title: optionalString(title), Status: "active", Visibility: "private", SourceType: "agent_commit", SourceRefID: &artifact.ArtifactID,
		ContentDigest: optionalString(artifact.StorageObjectRef.Checksum), MetadataJSON: mustJSON(artifact.MetadataSummary),
		CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&asset).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	object := businesscore.AssetStorageObject{
		ID: security.RandomID("aso_"), AssetID: assetID, Bucket: verified.Bucket, ObjectKey: &verified.ObjectKey,
		ObjectKeyHash: digest(verified.ObjectKey), ObjectURI: "tos://" + verified.Bucket + "/" + verified.ObjectKey,
		MIMEType: &verified.ContentType, SizeBytes: &verified.SizeBytes, Checksum: &verified.Checksum,
		Etag: optionalString(verified.Etag), StorageStatus: "available", PreviewURI: optionalString("/api/assets/" + assetID + "/access?access_type=preview"),
		DownloadPolicy: mustJSON(map[string]any{"access": "business_signed"}),
		CreatedBy:      optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&object).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	elementPayload := map[string]any{"artifact_summary": artifact.ArtifactSummary, "storage_object_ref": map[string]string{"bucket": artifact.StorageObjectRef.Bucket, "object_key_digest": digest(artifact.StorageObjectRef.ObjectKey)}}
	element := businesscore.AssetElement{
		ID: security.RandomID("asel_"), AssetID: assetID, ElementType: artifact.ElementType, ElementKey: artifact.ArtifactID,
		ElementSummaryJSON: mustJSON(elementPayload), Status: "active",
		CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&element).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	for _, final := range in.FinalElements {
		if final.ElementType == "" || strings.TrimSpace(final.ElementPayloadJSON) == "" {
			continue
		}
		payload := map[string]any{}
		_ = json.Unmarshal([]byte(final.ElementPayloadJSON), &payload)
		finalElement := businesscore.AssetElement{
			ID: security.RandomID("asel_"), AssetID: assetID, ElementType: final.ElementType,
			ElementKey: final.ElementType + "_" + security.RandomID(""), ElementSummaryJSON: mustJSON(payload),
			Status: "active", CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&finalElement).Error; err != nil {
			return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
		}
	}
	projectAsset := businesscore.ProjectAsset{
		ID: security.RandomID("pa_"), ProjectID: in.ProjectID, AssetID: assetID, AssetRole: "generated",
		AttachedByUserID: in.Auth.UserID, AttachedBy: optionalString("agent"), Status: "active",
		SourceSessionID: &in.SessionID, SourceRunID: &in.RunID, SourceArtifactID: &artifact.ArtifactID, SourceType: "agent_commit",
		CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now,
	}
	if err := tx.Create(&projectAsset).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	slot.Status = "committed"
	slot.Etag = optionalString(verified.Etag)
	slot.UpdatedBy = optionalString(in.Auth.UserID)
	slot.UpdatedAt = now
	if err := tx.Save(&slot).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	charged := estimateItem.EstimatePoints
	item := businesscore.AssetCommitItem{
		ID: security.RandomID("acmi_"), CommitID: commitID, ArtifactID: artifact.ArtifactID, AssetID: assetID,
		ResourceType: artifact.ResourceType, ElementType: artifact.ElementType, EstimateItemID: &artifact.EstimateItemID,
		ToolName: optionalString(artifact.ToolName), ToolType: optionalString(artifact.ToolType), ChargeQuantity: optionalInt64(artifact.ChargeQuantity),
		ChargedPoints: charged, ContentURIDigest: optionalString(artifact.ContentURIDigest), ArtifactSummaryJSON: mustJSON(artifact.ArtifactSummary),
		MetadataJSON: mustJSON(artifact.MetadataSummary), Status: "committed",
		CreatedBy: optionalString(in.Auth.UserID), UpdatedBy: optionalString(in.Auth.UserID), CreatedAt: now,
	}
	if err := tx.Create(&item).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	ref := CommittedAssetRefDTO{
		AssetID: assetID, SourceArtifactID: artifact.ArtifactID, ResourceType: artifact.ResourceType, AssetType: asset.AssetType,
		Status: asset.Status, PreviewURL: value(object.PreviewURI), ElementsSummaryJSON: string(element.ElementSummaryJSON),
	}
	line := ChargedLineItemDTO{EstimateItemID: artifact.EstimateItemID, ChargedPoints: charged, Status: "charged", AssetID: assetID, ArtifactID: artifact.ArtifactID}
	return ref, line, nil
}

func skippedArtifact(artifact CommitArtifactInput, estimateItem businesscore.CreditEstimateItem, err error) (CommittedAssetRefDTO, ChargedLineItemDTO, error) {
	line := ChargedLineItemDTO{
		EstimateItemID: estimateItem.EstimateItemID,
		ChargedPoints:  0,
		Status:         "skipped",
		ArtifactID:     artifact.ArtifactID,
	}
	return CommittedAssetRefDTO{}, line, artifactCommitError{line: line, err: err}
}

func commitStatusForSkipped(items []ChargedLineItemDTO) string {
	for _, item := range items {
		if item.Status == "skipped" {
			return "partial_committed"
		}
	}
	return "committed"
}

func (a *App) ensureProjectWritable(tx *gorm.DB, auth AuthContext, projectID string) error {
	var project businesscore.Project
	if err := tx.Where("id = ? AND space_id = ?", projectID, auth.SpaceID).First(&project).Error; err != nil {
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

func (a *App) lockFreezeAndAccount(tx *gorm.DB, freezeID string) (businesscore.CreditFreeze, businesscore.CreditAccount, error) {
	var freeze businesscore.CreditFreeze
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("freeze_id = ?", freezeID).First(&freeze).Error; err != nil {
		return freeze, businesscore.CreditAccount{}, bizerrors.New(bizerrors.CodeResourceNotFound, "credit freeze not found")
	}
	var account businesscore.CreditAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", freeze.AccountID).First(&account).Error; err != nil {
		return freeze, account, err
	}
	return freeze, account, nil
}

func (a *App) releaseUnused(tx *gorm.DB, freeze *businesscore.CreditFreeze, account *businesscore.CreditAccount, points int64, now time.Time) (int64, error) {
	if points <= 0 {
		return 0, nil
	}
	var rows []businesscore.CreditFreezeBatchItem
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("freeze_id = ? AND status = ?", freeze.FreezeID, "frozen").Order("created_at ASC").Find(&rows).Error; err != nil {
		return 0, err
	}
	remaining := points
	var released int64
	for _, row := range rows {
		if remaining <= 0 {
			break
		}
		available := row.FrozenPoints - row.ChargedPoints - row.ReleasedPoints
		if available <= 0 {
			continue
		}
		take := available
		if take > remaining {
			take = remaining
		}
		var batch businesscore.CreditBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", row.BatchID).First(&batch).Error; err != nil {
			return 0, err
		}
		if batch.ExpiresAt == nil || batch.ExpiresAt.After(now) {
			batch.RemainingPoints += take
			batch.UpdatedAt = now
			if err := tx.Save(&batch).Error; err != nil {
				return 0, err
			}
			account.AvailablePoints += take
		}
		row.ReleasedPoints += take
		if row.ChargedPoints+row.ReleasedPoints >= row.FrozenPoints {
			row.Status = "released"
		}
		row.UpdatedAt = now
		if err := tx.Save(&row).Error; err != nil {
			return 0, err
		}
		account.FrozenPoints -= take
		released += take
		remaining -= take
	}
	if remaining > 0 {
		return 0, bizerrors.New(bizerrors.CodeStateConflict, "release points exceed freeze")
	}
	return released, nil
}

func ensureEstimateItemUnsettled(tx *gorm.DB, estimateItemID string) error {
	var count int64
	if err := tx.Model(&businesscore.CreditToolChargeItem{}).Where("estimate_item_id = ?", estimateItemID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return bizerrors.New(bizerrors.CodeStateConflict, "estimate item already settled by tool charge")
	}
	if err := tx.Model(&businesscore.AssetCommitItem{}).Where("estimate_item_id = ?", estimateItemID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return bizerrors.New(bizerrors.CodeStateConflict, "estimate item already settled by asset commit")
	}
	return nil
}

func validateSafetyEvidence(evidence *businessagent.SafetyEvidenceDTO, sessionID, runID, traceID string, now time.Time) error {
	if evidence == nil || evidence.Result_ != "passed" || evidence.Scene != "generation" || evidence.TargetType != "prompt" {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence is invalid")
	}
	if strings.TrimSpace(evidence.SafetyEvidenceId) == "" || !strings.HasPrefix(evidence.EvaluatedObjectDigest, "sha256:") ||
		strings.TrimSpace(evidence.PolicyVersion) == "" || strings.TrimSpace(evidence.EvidenceVersion) == "" ||
		strings.TrimSpace(evidence.EvaluatedAt) == "" || strings.TrimSpace(evidence.TraceId) == "" {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence fields are incomplete")
	}
	if traceID != "" && evidence.TraceId != traceID {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence trace_id does not match request")
	}
	if evidence.SourceSessionId != nil && *evidence.SourceSessionId != "" && *evidence.SourceSessionId != sessionID {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence source_session_id does not match")
	}
	if evidence.SourceRunId != nil && *evidence.SourceRunId != "" && *evidence.SourceRunId != runID {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence source_run_id does not match")
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

func (a *App) getCommitDTO(ctx context.Context, ledgerRef string) (CommitDTO, error) {
	var batch businesscore.AssetCommitBatch
	if err := a.repo.DB().WithContext(ctx).Where("ledger_ref = ? OR commit_id = ?", ledgerRef, ledgerRef).First(&batch).Error; err != nil {
		return CommitDTO{}, err
	}
	var items []businesscore.AssetCommitItem
	_ = a.repo.DB().WithContext(ctx).Where("commit_id = ?", batch.CommitID).Find(&items).Error
	refs := make([]CommittedAssetRefDTO, 0, len(items))
	chargedLines := make([]ChargedLineItemDTO, 0, len(items))
	for _, item := range items {
		refs = append(refs, CommittedAssetRefDTO{AssetID: item.AssetID, SourceArtifactID: item.ArtifactID, ResourceType: item.ResourceType, Status: item.Status})
		chargedLines = append(chargedLines, ChargedLineItemDTO{
			EstimateItemID: value(item.EstimateItemID), ChargedPoints: item.ChargedPoints, Status: item.Status,
			AssetID: item.AssetID, ArtifactID: item.ArtifactID,
		})
	}
	var ledger businesscore.CreditLedgerEntry
	if err := a.repo.DB().WithContext(ctx).Where("id = ?", value(batch.LedgerRef)).First(&ledger).Error; err == nil {
		if lines := chargedLineItemsFromMetadata(ledger.MetadataJSON); len(lines) > 0 {
			chargedLines = lines
		}
	}
	return CommitDTO{
		AssetRefs: refs, ChargedPoints: batch.ChargedPoints, ReleasedPoints: batch.ReleasedPoints,
		CommitStatus: batch.CommitStatus, LedgerRef: value(batch.LedgerRef), ChargedLineItems: chargedLines,
	}, nil
}

func chargedLineItemsFromMetadata(raw datatypes.JSON) []ChargedLineItemDTO {
	if len(raw) == 0 {
		return nil
	}
	var data struct {
		ChargedLineItems []ChargedLineItemDTO `json:"charged_line_items"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return data.ChargedLineItems
}

func assetTypeFromResource(resourceType string) string {
	if resourceType == "audio" {
		return "music"
	}
	return resourceType
}

func requestHash(meta RequestMeta, auth AuthContext, extra map[string]any) string {
	if meta.RequestHash != "" {
		return meta.RequestHash
	}
	data, _ := json.Marshal(map[string]any{"space_id": auth.SpaceID, "actor_user_id": auth.UserID, "enterprise_id": auth.EnterpriseID, "extra": extra})
	return digest(string(data))
}

func safetyDigest(evidence *businessagent.SafetyEvidenceDTO) string {
	data, _ := json.Marshal(evidence)
	return digest(string(data))
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
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

func optionalInt64(value int64) *int64 {
	if value == 0 {
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

func normalizeETag(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func errorCode(err error) string {
	if biz := bizerrors.FromError(err); biz != nil {
		return string(biz.Code)
	}
	return "INTERNAL_ERROR"
}
