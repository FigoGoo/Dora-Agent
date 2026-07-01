package businesscore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/toolasset"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) FreezeToolCreditsV1(ctx context.Context, request toolasset.FreezeCreditsRequest, creditHoldID string, traceID string, frozenAt time.Time) (toolasset.CreditFreeze, error) {
	if err := toolasset.ValidateFreezeCreditsRequest(request); err != nil {
		return toolasset.CreditFreeze{}, fmt.Errorf("freeze_request: %w", err)
	}
	if strings.TrimSpace(creditHoldID) == "" || strings.TrimSpace(traceID) == "" {
		return toolasset.CreditFreeze{}, errors.New("credit_hold_id and trace_id are required")
	}
	if frozenAt.IsZero() {
		frozenAt = time.Now().UTC()
	}
	record := CreditHoldRecord{
		CreditHoldID:       creditHoldID,
		CreditAccountID:    request.CreditAccountID,
		CreditAccountScope: request.CreditAccountScope,
		RunID:              request.RunID,
		ProjectID:          request.ProjectID,
		ToolPlanID:         request.ToolPlanID,
		ToolPlanDigest:     request.ToolPlanDigest,
		Status:             toolasset.CreditFreezeStatusFrozen,
		FrozenCredits:      request.Credits,
		CommittedCredits:   0,
		ReleasedCredits:    0,
		IdempotencyKey:     request.IdempotencyKey,
		TraceID:            traceID,
		CreatedAt:          frozenAt.UTC(),
		UpdatedAt:          frozenAt.UTC(),
	}
	var hold toolasset.CreditFreeze
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "idempotency_key"}},
			DoNothing: true,
		}).Create(&record)
		if result.Error != nil {
			return result.Error
		}
		stored := record
		if result.RowsAffected == 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("idempotency_key = ?", request.IdempotencyKey).
				First(&stored).Error; err != nil {
				return err
			}
		}
		next, err := creditFreezeContract(stored)
		if err != nil {
			return err
		}
		if err := validateFreezeMatchesRequest(request, next); err != nil {
			return err
		}
		hold = next
		return nil
	})
	if err != nil {
		return toolasset.CreditFreeze{}, err
	}
	return hold, nil
}

func (r *Repository) CommitToolCreditsV1(ctx context.Context, creditHoldID string, ledgerEntryID string, traceID string, committedAt time.Time) (toolasset.CreditFreeze, error) {
	return r.finishToolCreditsV1(ctx, creditHoldID, ledgerEntryID, traceID, toolasset.CreditFreezeStatusCommitted, "tool_generation_commit", committedAt)
}

func (r *Repository) ReleaseToolCreditsV1(ctx context.Context, creditHoldID string, ledgerEntryID string, traceID string, releasedAt time.Time) (toolasset.CreditFreeze, error) {
	return r.finishToolCreditsV1(ctx, creditHoldID, ledgerEntryID, traceID, toolasset.CreditFreezeStatusReleased, "tool_generation_release", releasedAt)
}

func (r *Repository) CommitGeneratedAssetsV1(
	ctx context.Context,
	result toolasset.ToolResult,
	response toolasset.AssetCommitResponse,
	rule toolasset.AssetBillingRule,
	runID string,
	projectID string,
	idempotencyKey string,
	traceID string,
	committedAt time.Time,
) (toolasset.AssetCommitResponse, error) {
	if err := validateAssetCommitInput(result, response, rule, runID, projectID, idempotencyKey, traceID); err != nil {
		return toolasset.AssetCommitResponse{}, err
	}
	if committedAt.IsZero() {
		committedAt = time.Now().UTC()
	}
	committedAssetIDs, err := json.Marshal(response.CommittedAssetIDs)
	if err != nil {
		return toolasset.AssetCommitResponse{}, err
	}
	record := AssetCommitRecord{
		CommitRecordID:    response.CommitRecordID,
		ToolTaskID:        result.ToolTaskID,
		RunID:             runID,
		ProjectID:         projectID,
		Status:            response.Status,
		ToolResultDigest:  result.ResultDigest,
		CommittedAssetIDs: datatypes.JSON(committedAssetIDs),
		FailedAssetCount:  response.FailedAssetCount,
		CommitDigest:      response.CommitDigest,
		IdempotencyKey:    idempotencyKey,
		TraceID:           traceID,
		CreatedAt:         committedAt.UTC(),
	}
	var committed toolasset.AssetCommitResponse
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, asset := range result.Assets {
			assetRecord := generatedAssetRecord(asset)
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&assetRecord).Error; err != nil {
				return err
			}
		}
		insert := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "idempotency_key"}},
			DoNothing: true,
		}).Create(&record)
		if insert.Error != nil {
			return insert.Error
		}
		stored := record
		if insert.RowsAffected == 0 {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("idempotency_key = ?", idempotencyKey).
				First(&stored).Error; err != nil {
				return err
			}
		}
		next, err := assetCommitContract(stored)
		if err != nil {
			return err
		}
		if stored.ToolTaskID != result.ToolTaskID || stored.ToolResultDigest != result.ResultDigest {
			return errors.New("idempotent asset commit replay does not match tool result")
		}
		committed = next
		return nil
	})
	if err != nil {
		return toolasset.AssetCommitResponse{}, err
	}
	return committed, nil
}

func (r *Repository) finishToolCreditsV1(ctx context.Context, creditHoldID string, ledgerEntryID string, traceID string, targetStatus string, reason string, finishedAt time.Time) (toolasset.CreditFreeze, error) {
	if strings.TrimSpace(creditHoldID) == "" || strings.TrimSpace(ledgerEntryID) == "" || strings.TrimSpace(traceID) == "" {
		return toolasset.CreditFreeze{}, errors.New("credit_hold_id, ledger_entry_id and trace_id are required")
	}
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	var hold toolasset.CreditFreeze
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record CreditHoldRecord
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("credit_hold_id = ?", creditHoldID).First(&record).Error; err != nil {
			return err
		}
		current, err := creditFreezeContract(record)
		if err != nil {
			return err
		}
		if current.Status == targetStatus {
			hold = current
			return nil
		}
		if current.Status != toolasset.CreditFreezeStatusFrozen {
			return fmt.Errorf("credit hold %s is %s, expected frozen", creditHoldID, current.Status)
		}
		if current.FrozenCredits <= 0 {
			return errors.New("frozen credits must be > 0 before finish")
		}
		record.Status = targetStatus
		record.UpdatedAt = finishedAt.UTC()
		switch targetStatus {
		case toolasset.CreditFreezeStatusCommitted:
			record.CommittedCredits = record.FrozenCredits
			record.ReleasedCredits = 0
		case toolasset.CreditFreezeStatusReleased:
			record.CommittedCredits = 0
			record.ReleasedCredits = record.FrozenCredits
		default:
			return fmt.Errorf("unsupported target status %s", targetStatus)
		}
		updates := map[string]any{
			"status":            record.Status,
			"committed_credits": record.CommittedCredits,
			"released_credits":  record.ReleasedCredits,
			"updated_at":        record.UpdatedAt,
		}
		if err := tx.Model(&CreditHoldRecord{}).Where("credit_hold_id = ?", creditHoldID).Updates(updates).Error; err != nil {
			return err
		}
		entry, err := creditLedgerEntryRecord(record, ledgerEntryID, traceID, reason, finishedAt.UTC())
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&entry).Error; err != nil {
			return err
		}
		next, err := creditFreezeContract(record)
		if err != nil {
			return err
		}
		hold = next
		return nil
	})
	if err != nil {
		return toolasset.CreditFreeze{}, err
	}
	return hold, nil
}

func creditFreezeContract(record CreditHoldRecord) (toolasset.CreditFreeze, error) {
	hold := toolasset.CreditFreeze{
		SchemaVersion:    toolasset.SchemaVersionCreditFreeze,
		CreditHoldID:     record.CreditHoldID,
		AccountID:        record.CreditAccountID,
		AccountScope:     record.CreditAccountScope,
		RunID:            record.RunID,
		ToolPlanID:       record.ToolPlanID,
		ToolPlanDigest:   record.ToolPlanDigest,
		Status:           record.Status,
		FrozenCredits:    record.FrozenCredits,
		CommittedCredits: record.CommittedCredits,
		ReleasedCredits:  record.ReleasedCredits,
		IdempotencyKey:   record.IdempotencyKey,
		CreatedAt:        record.CreatedAt.UTC(),
	}
	if record.Status != toolasset.CreditFreezeStatusFrozen {
		updatedAt := record.UpdatedAt.UTC()
		hold.UpdatedAt = &updatedAt
	}
	if err := toolasset.ValidateCreditFreeze(hold); err != nil {
		return toolasset.CreditFreeze{}, err
	}
	return hold, nil
}

func validateFreezeMatchesRequest(request toolasset.FreezeCreditsRequest, hold toolasset.CreditFreeze) error {
	if request.CreditAccountID != hold.AccountID ||
		request.CreditAccountScope != hold.AccountScope ||
		request.RunID != hold.RunID ||
		request.ToolPlanID != hold.ToolPlanID ||
		request.ToolPlanDigest != hold.ToolPlanDigest ||
		request.Credits != hold.FrozenCredits ||
		request.IdempotencyKey != hold.IdempotencyKey {
		return errors.New("idempotent credit freeze replay does not match request")
	}
	return nil
}

func creditLedgerEntryRecord(record CreditHoldRecord, ledgerEntryID string, traceID string, reason string, createdAt time.Time) (CreditLedgerEntryRecord, error) {
	entryType := "debit"
	credits := record.CommittedCredits
	if record.Status == toolasset.CreditFreezeStatusReleased {
		entryType = "release"
		credits = record.ReleasedCredits
	}
	digest, err := foundation.CanonicalDigest(map[string]any{
		"credit_hold_id":    record.CreditHoldID,
		"credit_account_id": record.CreditAccountID,
		"entry_type":        entryType,
		"credits":           credits,
		"reason":            reason,
	})
	if err != nil {
		return CreditLedgerEntryRecord{}, err
	}
	return CreditLedgerEntryRecord{
		LedgerEntryID:   ledgerEntryID,
		CreditHoldID:    record.CreditHoldID,
		CreditAccountID: record.CreditAccountID,
		EntryType:       entryType,
		Credits:         credits,
		Reason:          reason,
		Digest:          digest,
		TraceID:         traceID,
		CreatedAt:       createdAt,
	}, nil
}

func validateAssetCommitInput(result toolasset.ToolResult, response toolasset.AssetCommitResponse, rule toolasset.AssetBillingRule, runID string, projectID string, idempotencyKey string, traceID string) error {
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(idempotencyKey) == "" || strings.TrimSpace(traceID) == "" {
		return errors.New("run_id, project_id, idempotency_key and trace_id are required")
	}
	if response.Status == toolasset.AssetCommitStatusPartiallyCommitted || result.Status == toolasset.ToolResultStatusPartiallySucceeded {
		if err := toolasset.ValidatePartialAssetCommit(result, response, rule); err != nil {
			return fmt.Errorf("partial_asset_commit: %w", err)
		}
	} else {
		if err := toolasset.ValidateToolResult(result); err != nil {
			return fmt.Errorf("tool_result: %w", err)
		}
		if err := toolasset.ValidateAssetCommitResponse(response); err != nil {
			return fmt.Errorf("commit_response: %w", err)
		}
	}
	for _, asset := range result.Assets {
		if asset.RunID != runID || asset.ProjectID != projectID {
			return errors.New("tool result assets must match run_id and project_id")
		}
	}
	return nil
}

func generatedAssetRecord(asset toolasset.GeneratedAsset) GeneratedAssetRecord {
	return GeneratedAssetRecord{
		AssetID:      asset.AssetID,
		ProjectID:    asset.ProjectID,
		RunID:        asset.RunID,
		ToolTaskID:   asset.ToolTaskID,
		ResourceType: asset.ResourceType,
		Status:       asset.Status,
		TOSObjectKey: asset.TOSObjectKey,
		PreviewURL:   asset.PreviewURL,
		AssetDigest:  asset.AssetDigest,
		CreatedAt:    asset.CreatedAt,
	}
}

func assetCommitContract(record AssetCommitRecord) (toolasset.AssetCommitResponse, error) {
	var committedAssetIDs []string
	if err := json.Unmarshal(record.CommittedAssetIDs, &committedAssetIDs); err != nil {
		return toolasset.AssetCommitResponse{}, err
	}
	response := toolasset.AssetCommitResponse{
		CommitRecordID:    record.CommitRecordID,
		Status:            record.Status,
		CommittedAssetIDs: committedAssetIDs,
		FailedAssetCount:  record.FailedAssetCount,
		CommitDigest:      record.CommitDigest,
	}
	if err := toolasset.ValidateAssetCommitResponse(response); err != nil {
		return toolasset.AssetCommitResponse{}, err
	}
	return response, nil
}
