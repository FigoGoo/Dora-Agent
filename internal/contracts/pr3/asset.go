package pr3

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

const (
	SchemaVersionGeneratedAsset = "generated_asset.v1"
)

const (
	GeneratedAssetStatusCommitted = "committed"
	GeneratedAssetStatusFailed    = "failed"
	GeneratedAssetStatusSkipped   = "skipped"
)

const (
	AssetCommitStatusCommitted          = "COMMITTED"
	AssetCommitStatusPartiallyCommitted = "PARTIALLY_COMMITTED"
	AssetCommitStatusFailed             = "FAILED"
)

type GeneratedAsset struct {
	SchemaVersion string    `json:"schema_version"`
	AssetID       string    `json:"asset_id"`
	ProjectID     string    `json:"project_id"`
	RunID         string    `json:"run_id"`
	ToolTaskID    string    `json:"tool_task_id"`
	ResourceType  string    `json:"resource_type"`
	Status        string    `json:"status"`
	TOSObjectKey  string    `json:"tos_object_key"`
	PreviewURL    *string   `json:"preview_url"`
	AssetDigest   string    `json:"asset_digest"`
	CreatedAt     time.Time `json:"created_at"`
}

type AssetCommitResponse struct {
	CommitRecordID    string   `json:"commit_record_id"`
	Status            string   `json:"status"`
	CommittedAssetIDs []string `json:"committed_asset_ids"`
	FailedAssetCount  int      `json:"failed_asset_count"`
	CommitDigest      string   `json:"commit_digest"`
}

type AssetBillingRule struct {
	ChargeOnlyCommittedAssets    bool `json:"charge_only_committed_assets"`
	FailedAssetsMustNotBeCharged bool `json:"failed_assets_must_not_be_charged"`
}

func ValidateGeneratedAsset(asset GeneratedAsset) error {
	if asset.SchemaVersion != SchemaVersionGeneratedAsset {
		return fmt.Errorf("schema_version must be %s", SchemaVersionGeneratedAsset)
	}
	if err := validatePrefixID(asset.AssetID, "asset_"); err != nil {
		return fmt.Errorf("asset_id: %w", err)
	}
	if strings.TrimSpace(asset.ProjectID) == "" || strings.TrimSpace(asset.RunID) == "" {
		return errors.New("project_id and run_id are required")
	}
	if err := validatePrefixID(asset.ToolTaskID, "ttask_"); err != nil {
		return fmt.Errorf("tool_task_id: %w", err)
	}
	if !isAllowed(asset.ResourceType, resourceTypes()) {
		return fmt.Errorf("invalid resource_type %q", asset.ResourceType)
	}
	if !isAllowed(asset.Status, []string{
		GeneratedAssetStatusCommitted,
		GeneratedAssetStatusFailed,
		GeneratedAssetStatusSkipped,
	}) {
		return fmt.Errorf("invalid asset status %q", asset.Status)
	}
	if strings.TrimSpace(asset.TOSObjectKey) == "" {
		return errors.New("tos_object_key is required")
	}
	if asset.Status == GeneratedAssetStatusCommitted && (asset.PreviewURL == nil || strings.TrimSpace(*asset.PreviewURL) == "") {
		return errors.New("committed asset requires preview_url")
	}
	if err := pr1.ValidateDigest(asset.AssetDigest); err != nil {
		return fmt.Errorf("asset_digest: %w", err)
	}
	if asset.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	return nil
}

func ValidateAssetCommitResponse(response AssetCommitResponse) error {
	if strings.TrimSpace(response.CommitRecordID) == "" {
		return errors.New("commit_record_id is required")
	}
	if !isAllowed(response.Status, []string{
		AssetCommitStatusCommitted,
		AssetCommitStatusPartiallyCommitted,
		AssetCommitStatusFailed,
	}) {
		return fmt.Errorf("invalid commit status %q", response.Status)
	}
	if response.FailedAssetCount < 0 {
		return errors.New("failed_asset_count must be >= 0")
	}
	for _, assetID := range response.CommittedAssetIDs {
		if err := validatePrefixID(assetID, "asset_"); err != nil {
			return fmt.Errorf("committed_asset_ids: %w", err)
		}
	}
	if response.Status == AssetCommitStatusPartiallyCommitted && (len(response.CommittedAssetIDs) == 0 || response.FailedAssetCount == 0) {
		return errors.New("partial commit requires committed and failed assets")
	}
	if err := pr1.ValidateDigest(response.CommitDigest); err != nil {
		return fmt.Errorf("commit_digest: %w", err)
	}
	return nil
}

func ValidatePartialAssetCommit(result ToolResult, response AssetCommitResponse, rule AssetBillingRule) error {
	if err := ValidateToolResult(result); err != nil {
		return fmt.Errorf("tool_result: %w", err)
	}
	if err := ValidateAssetCommitResponse(response); err != nil {
		return fmt.Errorf("commit_response: %w", err)
	}
	if result.Status != ToolResultStatusPartiallySucceeded || response.Status != AssetCommitStatusPartiallyCommitted {
		return errors.New("partial asset commit fixture requires partial statuses")
	}
	if !rule.ChargeOnlyCommittedAssets || !rule.FailedAssetsMustNotBeCharged {
		return errors.New("billing rule must charge only committed assets")
	}
	committed := make(map[string]struct{}, len(response.CommittedAssetIDs))
	for _, assetID := range response.CommittedAssetIDs {
		committed[assetID] = struct{}{}
	}
	failedCount := 0
	for _, asset := range result.Assets {
		_, isCommittedInResponse := committed[asset.AssetID]
		switch asset.Status {
		case GeneratedAssetStatusCommitted:
			if !isCommittedInResponse {
				return fmt.Errorf("committed asset %q missing from commit response", asset.AssetID)
			}
		case GeneratedAssetStatusFailed:
			failedCount++
			if isCommittedInResponse {
				return fmt.Errorf("failed asset %q must not be committed", asset.AssetID)
			}
		}
	}
	if failedCount != response.FailedAssetCount {
		return fmt.Errorf("failed_asset_count=%d, actual failed=%d", response.FailedAssetCount, failedCount)
	}
	return nil
}
