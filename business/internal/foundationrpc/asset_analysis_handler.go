package foundationrpc

import (
	"context"
	"errors"

	"github.com/FigoGoo/Dora-Agent/business/internal/assetanalysis"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
)

const (
	assetVersionConflictCode = "ASSET_ANALYSIS_VERSION_CONFLICT"
	limitExceededCode        = "LIMIT_EXCEEDED"
	evidenceConflictCode     = "ASSET_ANALYSIS_EVIDENCE_CONFLICT"
)

// BatchGetAssetAnalysisInputsPreviewV1 显式转换 DTO，并在独立本地门禁之后调用领域 exact-set 读取。
func (handler *Handler) BatchGetAssetAnalysisInputsPreviewV1(
	ctx context.Context,
	request *foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error) {
	if !handler.assetAnalysisEnabled {
		return nil, creationSpecServiceError(featureDisabledCode, "素材分析输入开发预览未启用", false)
	}
	if handler.assetAnalysis == nil {
		return nil, creationSpecServiceError(previewUnavailableCode, "素材分析输入开发预览暂时不可用", true)
	}
	if request == nil || request.Targets == nil {
		return nil, invalidArgument("素材分析输入预览请求无效")
	}
	targets := make([]assetanalysis.Target, len(request.Targets))
	for index, target := range request.Targets {
		if target == nil {
			return nil, invalidArgument("素材分析输入预览请求无效")
		}
		targets[index] = assetanalysis.Target{
			AssetID: target.AssetId, ExpectedAssetVersion: target.ExpectedAssetVersion,
		}
	}
	snapshot, err := handler.assetAnalysis.BatchGet(ctx, assetanalysis.Query{
		SchemaVersion: request.SchemaVersion, RequestID: request.RequestId,
		UserID: request.UserId, ProjectID: request.ProjectId, Targets: targets,
	})
	if err != nil {
		return nil, mapAssetAnalysisServiceError(err)
	}
	assets := make([]*foundationv1.AssetAnalysisPreviewAssetV1, len(snapshot.Assets))
	for index, asset := range snapshot.Assets {
		assets[index] = assetAnalysisAssetToRPC(asset)
	}
	handler.logger.InfoContext(ctx, "读取素材分析输入 Preview 快照成功",
		"request_id", request.RequestId, "project_id", request.ProjectId,
		"asset_count", len(assets), "response_complete", snapshot.ResponseComplete)
	return &foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1{
		SchemaVersion: assetanalysis.RPCSchemaVersion, RequestId: request.RequestId,
		SnapshotToken: snapshot.SnapshotToken, ResponseComplete: snapshot.ResponseComplete, Assets: assets,
	}, nil
}

func assetAnalysisAssetToRPC(asset assetanalysis.Asset) *foundationv1.AssetAnalysisPreviewAssetV1 {
	evidence := make([]*foundationv1.AssetAnalysisPreviewEvidenceV1, len(asset.Evidence))
	for index, item := range asset.Evidence {
		evidence[index] = assetAnalysisEvidenceToRPC(item)
	}
	return &foundationv1.AssetAnalysisPreviewAssetV1{
		AssetId: asset.ID, AssetVersion: asset.Version, MediaType: assetAnalysisMediaTypeToRPC(asset.MediaType), Evidence: evidence,
	}
}

func assetAnalysisEvidenceToRPC(evidence assetanalysis.Evidence) *foundationv1.AssetAnalysisPreviewEvidenceV1 {
	result := &foundationv1.AssetAnalysisPreviewEvidenceV1{
		EvidenceId: evidence.ID, AssetId: evidence.AssetID, AssetVersion: evidence.AssetVersion,
		MediaType: assetAnalysisMediaTypeToRPC(evidence.MediaType), EvidenceKind: assetAnalysisEvidenceKindToRPC(evidence.Kind),
		Availability: assetAnalysisAvailabilityToRPC(evidence.Availability),
	}
	if evidence.Availability == assetanalysis.AvailabilityReady {
		result.ContentDigest = stringPointer(evidence.ContentDigest)
		result.ExtractorSchemaVersion = stringPointer(evidence.ExtractorSchemaVersion)
		result.ExtractorVersion = stringPointer(evidence.ExtractorVersion)
		result.Content = stringPointer(evidence.Content)
		result.Locator = assetAnalysisLocatorToRPC(*evidence.Locator)
	} else {
		result.ReasonCode = stringPointer(evidence.ReasonCode)
	}
	return result
}

func assetAnalysisLocatorToRPC(locator assetanalysis.Locator) *foundationv1.AssetAnalysisPreviewLocatorV1 {
	return &foundationv1.AssetAnalysisPreviewLocatorV1{
		Kind: assetAnalysisLocatorKindToRPC(locator.Kind), TextStart: locator.TextStart, TextEnd: locator.TextEnd,
		TextSourceLength: locator.TextSourceLength, ImageX: locator.ImageX, ImageY: locator.ImageY,
		ImageWidth: locator.ImageWidth, ImageHeight: locator.ImageHeight,
	}
}

func assetAnalysisMediaTypeToRPC(value assetanalysis.MediaType) foundationv1.AssetAnalysisPreviewMediaTypeV1 {
	if value == assetanalysis.MediaTypeText {
		return foundationv1.AssetAnalysisPreviewMediaTypeV1_TEXT
	}
	return foundationv1.AssetAnalysisPreviewMediaTypeV1_IMAGE
}

func assetAnalysisEvidenceKindToRPC(value assetanalysis.EvidenceKind) foundationv1.AssetAnalysisPreviewEvidenceKindV1 {
	switch value {
	case assetanalysis.EvidenceKindTextSegment:
		return foundationv1.AssetAnalysisPreviewEvidenceKindV1_TEXT_SEGMENT
	case assetanalysis.EvidenceKindVisualDescription:
		return foundationv1.AssetAnalysisPreviewEvidenceKindV1_VISUAL_DESCRIPTION
	default:
		return foundationv1.AssetAnalysisPreviewEvidenceKindV1_SAFETY_LABEL
	}
}

func assetAnalysisAvailabilityToRPC(value assetanalysis.Availability) foundationv1.AssetAnalysisPreviewAvailabilityV1 {
	switch value {
	case assetanalysis.AvailabilityReady:
		return foundationv1.AssetAnalysisPreviewAvailabilityV1_READY
	case assetanalysis.AvailabilityMissing:
		return foundationv1.AssetAnalysisPreviewAvailabilityV1_MISSING
	case assetanalysis.AvailabilityFailed:
		return foundationv1.AssetAnalysisPreviewAvailabilityV1_FAILED
	case assetanalysis.AvailabilityRedacted:
		return foundationv1.AssetAnalysisPreviewAvailabilityV1_REDACTED
	default:
		return foundationv1.AssetAnalysisPreviewAvailabilityV1_UNSUPPORTED
	}
}

func assetAnalysisLocatorKindToRPC(value assetanalysis.LocatorKind) foundationv1.AssetAnalysisPreviewLocatorKindV1 {
	switch value {
	case assetanalysis.LocatorKindTextRange:
		return foundationv1.AssetAnalysisPreviewLocatorKindV1_TEXT_RANGE
	case assetanalysis.LocatorKindImageWhole:
		return foundationv1.AssetAnalysisPreviewLocatorKindV1_IMAGE_WHOLE
	default:
		return foundationv1.AssetAnalysisPreviewLocatorKindV1_IMAGE_REGION
	}
}

func stringPointer(value string) *string {
	result := value
	return &result
}

func mapAssetAnalysisServiceError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, assetanalysis.ErrInvalidArgument):
		return invalidArgument("素材分析输入预览请求无效")
	case errors.Is(err, assetanalysis.ErrNotFound):
		return creationSpecServiceError(notFoundCode, "Project 或素材不存在或不可访问", false)
	case errors.Is(err, assetanalysis.ErrVersionConflict):
		return creationSpecServiceError(assetVersionConflictCode, "素材版本已变化", false)
	case errors.Is(err, assetanalysis.ErrLimitExceeded):
		return creationSpecServiceError(limitExceededCode, "素材分析输入证据超过完整响应上限", false)
	case errors.Is(err, assetanalysis.ErrEvidenceConflict):
		return creationSpecServiceError(evidenceConflictCode, "素材分析输入证据不一致", false)
	default:
		return creationSpecServiceError(persistenceCode, "素材分析输入预览存储暂时不可用", true)
	}
}
