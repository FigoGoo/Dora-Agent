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
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuthContext = accountspace.AuthContext
type RequestMeta = accountspace.RequestMeta

type App struct {
	repo  *businesscore.Repository
	guard *idempotency.IdempotencyGuard
	audit auditlog.Writer
	now   func() time.Time
}

func New(repo *businesscore.Repository, guard *idempotency.IdempotencyGuard, audit auditlog.Writer) *App {
	return &App{repo: repo, guard: guard, audit: audit, now: func() time.Time { return time.Now().UTC() }}
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

func (a *App) CommitGeneratedAssetAndCharge(ctx context.Context, in CommitInput) (CommitDTO, error) {
	if in.Auth.UserID == "" || in.Auth.SpaceID == "" {
		return CommitDTO{}, bizerrors.New(bizerrors.CodeUnauthenticated, "auth context is required")
	}
	if in.Meta.IdempotencyKey == "" || in.ProjectID == "" || in.SessionID == "" || in.RunID == "" || in.FreezeID == "" || len(in.Artifacts) == 0 {
		return CommitDTO{}, bizerrors.New(bizerrors.CodeInvalidArgument, "asset commit request is incomplete")
	}
	if err := validateSafetyEvidence(in.SafetyEvidence, a.now()); err != nil {
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
		commitID := security.RandomID("acm_")
		assetRefs := make([]CommittedAssetRefDTO, 0, len(in.Artifacts))
		lineItems := make([]ChargedLineItemDTO, 0, len(in.Artifacts))
		var charged int64
		for _, artifact := range in.Artifacts {
			assetRef, line, err := a.commitArtifact(tx, in, commitID, artifact, now)
			if err != nil {
				return err
			}
			assetRefs = append(assetRefs, assetRef)
			lineItems = append(lineItems, line)
			charged += line.ChargedPoints
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
			IdempotencyKey: optionalString(in.Meta.IdempotencyKey), MetadataJSON: mustJSON(map[string]any{"asset_count": len(assetRefs)}), CreatedAt: now,
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
			ChargedPoints: charged, ReleasedPoints: released, CommitStatus: "committed", LedgerRef: &ledgerID,
			IdempotencyKey: in.Meta.IdempotencyKey, TraceID: in.Meta.TraceID, CreatedAt: now, UpdatedAt: now,
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

func (a *App) commitArtifact(tx *gorm.DB, in CommitInput, commitID string, artifact CommitArtifactInput, now time.Time) (CommittedAssetRefDTO, ChargedLineItemDTO, error) {
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
	var slot businesscore.GeneratedAssetObjectSlot
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id = ? AND artifact_id = ?", in.RunID, artifact.ArtifactID).First(&slot).Error
	if err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "generated object slot not found")
	}
	if slot.ExpiresAt.Before(now) || (slot.Status != "created" && slot.Status != "uploaded" && slot.Status != "committed") {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "generated object slot is not committable")
	}
	if slot.ObjectKey != artifact.StorageObjectRef.ObjectKey || slot.Bucket != artifact.StorageObjectRef.Bucket || slot.ContentType != artifact.StorageObjectRef.ContentType || slot.SizeBytes != artifact.StorageObjectRef.SizeBytes {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "storage object does not match generated slot")
	}
	if slot.Checksum != nil && *slot.Checksum != "" && artifact.StorageObjectRef.Checksum != "" && *slot.Checksum != artifact.StorageObjectRef.Checksum {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, bizerrors.New(bizerrors.CodeStateConflict, "storage checksum does not match generated slot")
	}
	assetID := security.RandomID("ast_")
	title := artifact.ArtifactSummary["display_name"]
	asset := businesscore.Asset{
		ID: assetID, AssetNo: "A" + strings.ToUpper(assetID[4:12]), OwnerUserID: in.Auth.UserID, SpaceID: in.Auth.SpaceID,
		EnterpriseID: optionalString(in.Auth.EnterpriseID), ProjectID: &in.ProjectID, AssetType: assetTypeFromResource(artifact.ResourceType),
		Title: optionalString(title), Status: "active", Visibility: "private", SourceType: "agent_commit", SourceRefID: &artifact.ArtifactID,
		ContentDigest: optionalString(artifact.StorageObjectRef.Checksum), MetadataJSON: mustJSON(artifact.MetadataSummary), CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&asset).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	object := businesscore.AssetStorageObject{
		ID: security.RandomID("aso_"), AssetID: assetID, Bucket: artifact.StorageObjectRef.Bucket, ObjectKey: &artifact.StorageObjectRef.ObjectKey,
		ObjectKeyHash: digest(artifact.StorageObjectRef.ObjectKey), ObjectURI: "tos://" + artifact.StorageObjectRef.Bucket + "/" + artifact.StorageObjectRef.ObjectKey,
		MIMEType: &artifact.StorageObjectRef.ContentType, SizeBytes: &artifact.StorageObjectRef.SizeBytes, Checksum: &artifact.StorageObjectRef.Checksum,
		Etag: optionalString(artifact.StorageObjectRef.Etag), StorageStatus: "available", PreviewURI: optionalString("/api/assets/" + assetID + "/access?access_type=preview"),
		DownloadPolicy: mustJSON(map[string]any{"access": "business_signed"}), CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&object).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	elementPayload := map[string]any{"artifact_summary": artifact.ArtifactSummary, "storage_object_ref": map[string]string{"bucket": artifact.StorageObjectRef.Bucket, "object_key_digest": digest(artifact.StorageObjectRef.ObjectKey)}}
	element := businesscore.AssetElement{
		ID: security.RandomID("asel_"), AssetID: assetID, ElementType: artifact.ElementType, ElementKey: artifact.ArtifactID,
		ElementSummaryJSON: mustJSON(elementPayload), Status: "active", CreatedAt: now, UpdatedAt: now,
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
			Status: "active", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&finalElement).Error; err != nil {
			return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
		}
	}
	projectAsset := businesscore.ProjectAsset{
		ID: security.RandomID("pa_"), ProjectID: in.ProjectID, AssetID: assetID, AssetRole: "generated",
		AttachedByUserID: in.Auth.UserID, AttachedBy: optionalString("agent"), Status: "active",
		SourceSessionID: &in.SessionID, SourceRunID: &in.RunID, SourceArtifactID: &artifact.ArtifactID, SourceType: "agent_commit", CreatedAt: now,
	}
	if err := tx.Create(&projectAsset).Error; err != nil {
		return CommittedAssetRefDTO{}, ChargedLineItemDTO{}, err
	}
	slot.Status = "committed"
	slot.Etag = optionalString(artifact.StorageObjectRef.Etag)
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
		MetadataJSON: mustJSON(artifact.MetadataSummary), Status: "committed", CreatedAt: now,
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

func validateSafetyEvidence(evidence *businessagent.SafetyEvidenceDTO, now time.Time) error {
	if evidence == nil || evidence.Result_ != "passed" || evidence.Scene != "generation" || evidence.TargetType != "prompt" {
		return bizerrors.New(bizerrors.CodeSafetyEvidenceInvalid, "safety evidence is invalid")
	}
	if evidence.ExpiresAt != nil && *evidence.ExpiresAt != "" {
		expires, err := time.Parse(time.RFC3339Nano, *evidence.ExpiresAt)
		if err == nil && !expires.After(now) {
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
	for _, item := range items {
		refs = append(refs, CommittedAssetRefDTO{AssetID: item.AssetID, SourceArtifactID: item.ArtifactID, ResourceType: item.ResourceType, Status: item.Status})
	}
	return CommitDTO{AssetRefs: refs, ChargedPoints: batch.ChargedPoints, ReleasedPoints: batch.ReleasedPoints, CommitStatus: batch.CommitStatus, LedgerRef: value(batch.LedgerRef)}, nil
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

func errorCode(err error) string {
	if biz := bizerrors.FromError(err); biz != nil {
		return string(biz.Code)
	}
	return "INTERNAL_ERROR"
}
