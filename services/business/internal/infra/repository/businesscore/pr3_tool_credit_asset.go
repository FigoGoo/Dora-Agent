package businesscore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr3"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) FreezeToolCreditsV1(ctx context.Context, request pr3.FreezeCreditsRequest, creditHoldID string, traceID string, frozenAt time.Time) (pr3.CreditFreeze, error) {
	if err := pr3.ValidateFreezeCreditsRequest(request); err != nil {
		return pr3.CreditFreeze{}, fmt.Errorf("freeze_request: %w", err)
	}
	if strings.TrimSpace(creditHoldID) == "" || strings.TrimSpace(traceID) == "" {
		return pr3.CreditFreeze{}, errors.New("credit_hold_id and trace_id are required")
	}
	if frozenAt.IsZero() {
		frozenAt = time.Now().UTC()
	}
	record := PR3CreditHoldRecord{
		CreditHoldID:       creditHoldID,
		CreditAccountID:    request.CreditAccountID,
		CreditAccountScope: request.CreditAccountScope,
		RunID:              request.RunID,
		ProjectID:          request.ProjectID,
		ToolPlanID:         request.ToolPlanID,
		ToolPlanDigest:     request.ToolPlanDigest,
		Status:             pr3.CreditFreezeStatusFrozen,
		FrozenCredits:      request.Credits,
		CommittedCredits:   0,
		ReleasedCredits:    0,
		IdempotencyKey:     request.IdempotencyKey,
		TraceID:            traceID,
		CreatedAt:          frozenAt.UTC(),
		UpdatedAt:          frozenAt.UTC(),
	}
	var hold pr3.CreditFreeze
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
		return pr3.CreditFreeze{}, err
	}
	return hold, nil
}

func (r *Repository) CommitToolCreditsV1(ctx context.Context, creditHoldID string, ledgerEntryID string, traceID string, committedAt time.Time) (pr3.CreditFreeze, error) {
	return r.finishToolCreditsV1(ctx, creditHoldID, ledgerEntryID, traceID, pr3.CreditFreezeStatusCommitted, "tool_generation_commit", committedAt)
}

func (r *Repository) ReleaseToolCreditsV1(ctx context.Context, creditHoldID string, ledgerEntryID string, traceID string, releasedAt time.Time) (pr3.CreditFreeze, error) {
	return r.finishToolCreditsV1(ctx, creditHoldID, ledgerEntryID, traceID, pr3.CreditFreezeStatusReleased, "tool_generation_release", releasedAt)
}

func (r *Repository) CommitGeneratedAssetsV1(
	ctx context.Context,
	result pr3.ToolResult,
	response pr3.AssetCommitResponse,
	rule pr3.AssetBillingRule,
	runID string,
	projectID string,
	idempotencyKey string,
	traceID string,
	committedAt time.Time,
) (pr3.AssetCommitResponse, error) {
	if err := validateAssetCommitInput(result, response, rule, runID, projectID, idempotencyKey, traceID); err != nil {
		return pr3.AssetCommitResponse{}, err
	}
	if committedAt.IsZero() {
		committedAt = time.Now().UTC()
	}
	committedAssetIDs, err := json.Marshal(response.CommittedAssetIDs)
	if err != nil {
		return pr3.AssetCommitResponse{}, err
	}
	record := PR3AssetCommitRecord{
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
	var committed pr3.AssetCommitResponse
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
		return pr3.AssetCommitResponse{}, err
	}
	return committed, nil
}

func (r *Repository) finishToolCreditsV1(ctx context.Context, creditHoldID string, ledgerEntryID string, traceID string, targetStatus string, reason string, finishedAt time.Time) (pr3.CreditFreeze, error) {
	if strings.TrimSpace(creditHoldID) == "" || strings.TrimSpace(ledgerEntryID) == "" || strings.TrimSpace(traceID) == "" {
		return pr3.CreditFreeze{}, errors.New("credit_hold_id, ledger_entry_id and trace_id are required")
	}
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}
	var hold pr3.CreditFreeze
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var record PR3CreditHoldRecord
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
		if current.Status != pr3.CreditFreezeStatusFrozen {
			return fmt.Errorf("credit hold %s is %s, expected frozen", creditHoldID, current.Status)
		}
		if current.FrozenCredits <= 0 {
			return errors.New("frozen credits must be > 0 before finish")
		}
		record.Status = targetStatus
		record.UpdatedAt = finishedAt.UTC()
		switch targetStatus {
		case pr3.CreditFreezeStatusCommitted:
			record.CommittedCredits = record.FrozenCredits
			record.ReleasedCredits = 0
		case pr3.CreditFreezeStatusReleased:
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
		if err := tx.Model(&PR3CreditHoldRecord{}).Where("credit_hold_id = ?", creditHoldID).Updates(updates).Error; err != nil {
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
		return pr3.CreditFreeze{}, err
	}
	return hold, nil
}

func creditFreezeContract(record PR3CreditHoldRecord) (pr3.CreditFreeze, error) {
	hold := pr3.CreditFreeze{
		SchemaVersion:    pr3.SchemaVersionCreditFreeze,
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
	if record.Status != pr3.CreditFreezeStatusFrozen {
		updatedAt := record.UpdatedAt.UTC()
		hold.UpdatedAt = &updatedAt
	}
	if err := pr3.ValidateCreditFreeze(hold); err != nil {
		return pr3.CreditFreeze{}, err
	}
	return hold, nil
}

func validateFreezeMatchesRequest(request pr3.FreezeCreditsRequest, hold pr3.CreditFreeze) error {
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

func creditLedgerEntryRecord(record PR3CreditHoldRecord, ledgerEntryID string, traceID string, reason string, createdAt time.Time) (PR3CreditLedgerEntryRecord, error) {
	entryType := "debit"
	credits := record.CommittedCredits
	if record.Status == pr3.CreditFreezeStatusReleased {
		entryType = "release"
		credits = record.ReleasedCredits
	}
	digest, err := pr1.CanonicalDigest(map[string]any{
		"credit_hold_id":    record.CreditHoldID,
		"credit_account_id": record.CreditAccountID,
		"entry_type":        entryType,
		"credits":           credits,
		"reason":            reason,
	})
	if err != nil {
		return PR3CreditLedgerEntryRecord{}, err
	}
	return PR3CreditLedgerEntryRecord{
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

func validateAssetCommitInput(result pr3.ToolResult, response pr3.AssetCommitResponse, rule pr3.AssetBillingRule, runID string, projectID string, idempotencyKey string, traceID string) error {
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(projectID) == "" || strings.TrimSpace(idempotencyKey) == "" || strings.TrimSpace(traceID) == "" {
		return errors.New("run_id, project_id, idempotency_key and trace_id are required")
	}
	if response.Status == pr3.AssetCommitStatusPartiallyCommitted || result.Status == pr3.ToolResultStatusPartiallySucceeded {
		if err := pr3.ValidatePartialAssetCommit(result, response, rule); err != nil {
			return fmt.Errorf("partial_asset_commit: %w", err)
		}
	} else {
		if err := pr3.ValidateToolResult(result); err != nil {
			return fmt.Errorf("tool_result: %w", err)
		}
		if err := pr3.ValidateAssetCommitResponse(response); err != nil {
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

func generatedAssetRecord(asset pr3.GeneratedAsset) PR3GeneratedAssetRecord {
	return PR3GeneratedAssetRecord{
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

func assetCommitContract(record PR3AssetCommitRecord) (pr3.AssetCommitResponse, error) {
	var committedAssetIDs []string
	if err := json.Unmarshal(record.CommittedAssetIDs, &committedAssetIDs); err != nil {
		return pr3.AssetCommitResponse{}, err
	}
	response := pr3.AssetCommitResponse{
		CommitRecordID:    record.CommitRecordID,
		Status:            record.Status,
		CommittedAssetIDs: committedAssetIDs,
		FailedAssetCount:  record.FailedAssetCount,
		CommitDigest:      record.CommitDigest,
	}
	if err := pr3.ValidateAssetCommitResponse(response); err != nil {
		return pr3.AssetCommitResponse{}, err
	}
	return response, nil
}
