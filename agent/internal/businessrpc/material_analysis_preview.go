package businessrpc

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/analyzematerials"
	"github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
	"github.com/cloudwego/kitex/client/callopt"
	"github.com/google/uuid"
)

// materialAnalysisPreviewProtocolClient 是 Agent 消费方定义的 Evidence Preview 最小协议接口。
type materialAnalysisPreviewProtocolClient interface {
	BatchGetAssetAnalysisInputsPreviewV1(
		ctx context.Context,
		request *foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
		callOptions ...callopt.Option,
	) (*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1, error)
}

var _ analyzematerials.EvidenceLoader = (*Client)(nil)

// BatchGetAssetAnalysisInputs 以一次有界、无重试 RPC 加载完整 text/image Evidence 快照。
func (c *Client) BatchGetAssetAnalysisInputs(
	ctx context.Context,
	query analyzematerials.EvidenceQuery,
) (analyzematerials.EvidenceSnapshot, error) {
	requestID, err := c.idgen.New()
	if err != nil {
		return analyzematerials.EvidenceSnapshot{}, materialAnalysisError(analyzematerials.ResultCodeInternal, err)
	}
	request, err := mapMaterialAnalysisRequest(requestID, query)
	if err != nil {
		return analyzematerials.EvidenceSnapshot{}, err
	}
	if c.materialAnalysisPreview == nil {
		return analyzematerials.EvidenceSnapshot{}, materialAnalysisError(
			analyzematerials.ResultCodeInternal,
			errors.New("material analysis preview protocol client is unavailable"),
		)
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()
	response, err := c.materialAnalysisPreview.BatchGetAssetAnalysisInputsPreviewV1(requestCtx, request)
	if err != nil {
		return analyzematerials.EvidenceSnapshot{}, mapMaterialAnalysisRPCError(ctx, requestCtx, err)
	}
	snapshot, err := mapMaterialAnalysisResponse(request, response)
	if err != nil {
		return analyzematerials.EvidenceSnapshot{}, err
	}
	if err := analyzematerials.ValidateEvidenceSnapshot(query, snapshot); err != nil {
		return analyzematerials.EvidenceSnapshot{}, err
	}
	return snapshot, nil
}

func mapMaterialAnalysisRequest(
	requestID string,
	query analyzematerials.EvidenceQuery,
) (*foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1, error) {
	if !canonicalMaterialAnalysisUUIDv7(requestID) || !canonicalMaterialAnalysisUUIDv7(query.UserID) ||
		!canonicalMaterialAnalysisUUIDv7(query.ProjectID) || len(query.Targets) < 1 || len(query.Targets) > 8 {
		return nil, materialAnalysisError(analyzematerials.ResultCodeSnapshotInvalid, errors.New("invalid evidence query envelope"))
	}
	targets := append([]analyzematerials.AssetTarget(nil), query.Targets...)
	sort.Slice(targets, func(left, right int) bool { return targets[left].AssetID < targets[right].AssetID })
	protocolTargets := make([]*foundationv1.AssetAnalysisPreviewTargetV1, 0, len(targets))
	for index, target := range targets {
		if !canonicalMaterialAnalysisUUIDv7(target.AssetID) || target.ExpectedVersion < 0 ||
			(index > 0 && targets[index-1].AssetID == target.AssetID) {
			return nil, materialAnalysisError(analyzematerials.ResultCodeSnapshotInvalid, errors.New("invalid evidence query target"))
		}
		protocolTarget := &foundationv1.AssetAnalysisPreviewTargetV1{AssetId: target.AssetID}
		if target.ExpectedVersion > 0 {
			expectedVersion := target.ExpectedVersion
			protocolTarget.ExpectedAssetVersion = &expectedVersion
		}
		protocolTargets = append(protocolTargets, protocolTarget)
	}
	return &foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1{
		SchemaVersion: foundationv1.ASSET_ANALYSIS_INPUTS_PREVIEW_RPC_SCHEMA_VERSION,
		RequestId:     requestID,
		UserId:        query.UserID,
		ProjectId:     query.ProjectID,
		Targets:       protocolTargets,
	}, nil
}

func mapMaterialAnalysisResponse(
	request *foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1,
	response *foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1,
) (analyzematerials.EvidenceSnapshot, error) {
	if response == nil || response.SchemaVersion != foundationv1.ASSET_ANALYSIS_INPUTS_PREVIEW_RPC_SCHEMA_VERSION ||
		response.RequestId != request.RequestId || response.SnapshotToken == "" {
		return analyzematerials.EvidenceSnapshot{}, materialAnalysisError(
			analyzematerials.ResultCodeSnapshotInvalid,
			errors.New("invalid material analysis response envelope"),
		)
	}
	snapshot := analyzematerials.EvidenceSnapshot{
		SchemaVersion:    analyzematerials.EvidenceSnapshotSchemaVersion,
		SnapshotToken:    response.SnapshotToken,
		ResponseComplete: response.ResponseComplete,
		Assets:           make([]analyzematerials.AssetAnalysisInput, 0, len(response.Assets)),
	}
	for _, protocolAsset := range response.Assets {
		asset, err := mapMaterialAnalysisAsset(protocolAsset)
		if err != nil {
			return analyzematerials.EvidenceSnapshot{}, err
		}
		snapshot.Assets = append(snapshot.Assets, asset)
	}
	return snapshot, nil
}

func mapMaterialAnalysisAsset(
	protocolAsset *foundationv1.AssetAnalysisPreviewAssetV1,
) (analyzematerials.AssetAnalysisInput, error) {
	if protocolAsset == nil {
		return analyzematerials.AssetAnalysisInput{}, materialAnalysisError(
			analyzematerials.ResultCodeSnapshotInvalid,
			errors.New("nil material analysis asset"),
		)
	}
	mediaType, err := mapMaterialAnalysisMediaType(protocolAsset.MediaType)
	if err != nil {
		return analyzematerials.AssetAnalysisInput{}, materialAnalysisError(analyzematerials.ResultCodeSnapshotInvalid, err)
	}
	asset := analyzematerials.AssetAnalysisInput{
		AssetID:      protocolAsset.AssetId,
		AssetVersion: protocolAsset.AssetVersion,
		MediaType:    mediaType,
		Evidence:     make([]analyzematerials.EvidenceInput, 0, len(protocolAsset.Evidence)),
	}
	for _, protocolEvidence := range protocolAsset.Evidence {
		evidence, err := mapMaterialAnalysisEvidence(protocolEvidence)
		if err != nil {
			return analyzematerials.AssetAnalysisInput{}, err
		}
		asset.Evidence = append(asset.Evidence, evidence)
	}
	return asset, nil
}

func mapMaterialAnalysisEvidence(
	protocolEvidence *foundationv1.AssetAnalysisPreviewEvidenceV1,
) (analyzematerials.EvidenceInput, error) {
	if protocolEvidence == nil {
		return analyzematerials.EvidenceInput{}, materialAnalysisError(
			analyzematerials.ResultCodeEvidenceConflict,
			errors.New("nil material analysis evidence"),
		)
	}
	mediaType, err := mapMaterialAnalysisMediaType(protocolEvidence.MediaType)
	if err != nil {
		return analyzematerials.EvidenceInput{}, materialAnalysisError(analyzematerials.ResultCodeEvidenceConflict, err)
	}
	evidenceKind, err := mapMaterialAnalysisEvidenceKind(protocolEvidence.EvidenceKind)
	if err != nil {
		return analyzematerials.EvidenceInput{}, materialAnalysisError(analyzematerials.ResultCodeEvidenceConflict, err)
	}
	availability, err := mapMaterialAnalysisAvailability(protocolEvidence.Availability)
	if err != nil {
		return analyzematerials.EvidenceInput{}, materialAnalysisError(analyzematerials.ResultCodeEvidenceConflict, err)
	}
	locator, err := mapMaterialAnalysisLocator(protocolEvidence.Locator)
	if err != nil {
		return analyzematerials.EvidenceInput{}, materialAnalysisError(analyzematerials.ResultCodeEvidenceConflict, err)
	}
	return analyzematerials.EvidenceInput{
		EvidenceID:             protocolEvidence.EvidenceId,
		AssetID:                protocolEvidence.AssetId,
		AssetVersion:           protocolEvidence.AssetVersion,
		MediaType:              mediaType,
		EvidenceKind:           evidenceKind,
		ContentDigest:          optionalString(protocolEvidence.ContentDigest),
		ExtractorSchemaVersion: optionalString(protocolEvidence.ExtractorSchemaVersion),
		ExtractorVersion:       optionalString(protocolEvidence.ExtractorVersion),
		Locator:                locator,
		Availability:           availability,
		ReasonCode:             optionalString(protocolEvidence.ReasonCode),
		Content:                optionalString(protocolEvidence.Content),
	}, nil
}

func mapMaterialAnalysisMediaType(value foundationv1.AssetAnalysisPreviewMediaTypeV1) (string, error) {
	switch value {
	case foundationv1.AssetAnalysisPreviewMediaTypeV1_TEXT:
		return "text", nil
	case foundationv1.AssetAnalysisPreviewMediaTypeV1_IMAGE:
		return "image", nil
	default:
		return "", fmt.Errorf("unknown material analysis media type: %d", value)
	}
}

func mapMaterialAnalysisEvidenceKind(value foundationv1.AssetAnalysisPreviewEvidenceKindV1) (string, error) {
	switch value {
	case foundationv1.AssetAnalysisPreviewEvidenceKindV1_TEXT_SEGMENT:
		return "text_segment", nil
	case foundationv1.AssetAnalysisPreviewEvidenceKindV1_VISUAL_DESCRIPTION:
		return "visual_description", nil
	case foundationv1.AssetAnalysisPreviewEvidenceKindV1_SAFETY_LABEL:
		return "safety_label", nil
	default:
		return "", fmt.Errorf("unknown material analysis evidence kind: %d", value)
	}
}

func mapMaterialAnalysisAvailability(value foundationv1.AssetAnalysisPreviewAvailabilityV1) (string, error) {
	switch value {
	case foundationv1.AssetAnalysisPreviewAvailabilityV1_READY:
		return "ready", nil
	case foundationv1.AssetAnalysisPreviewAvailabilityV1_MISSING:
		return "missing", nil
	case foundationv1.AssetAnalysisPreviewAvailabilityV1_FAILED:
		return "failed", nil
	case foundationv1.AssetAnalysisPreviewAvailabilityV1_REDACTED:
		return "redacted", nil
	case foundationv1.AssetAnalysisPreviewAvailabilityV1_UNSUPPORTED:
		return "unsupported", nil
	default:
		return "", fmt.Errorf("unknown material analysis availability: %d", value)
	}
}

func mapMaterialAnalysisLocator(
	protocolLocator *foundationv1.AssetAnalysisPreviewLocatorV1,
) (analyzematerials.EvidenceLocator, error) {
	if protocolLocator == nil {
		return analyzematerials.EvidenceLocator{}, nil
	}
	kind, err := mapMaterialAnalysisLocatorKind(protocolLocator.Kind)
	if err != nil {
		return analyzematerials.EvidenceLocator{}, err
	}
	start, err := materialAnalysisInt64(optionalInt64(protocolLocator.TextStart))
	if err != nil {
		return analyzematerials.EvidenceLocator{}, err
	}
	end, err := materialAnalysisInt64(optionalInt64(protocolLocator.TextEnd))
	if err != nil {
		return analyzematerials.EvidenceLocator{}, err
	}
	sourceLength, err := materialAnalysisInt64(optionalInt64(protocolLocator.TextSourceLength))
	if err != nil {
		return analyzematerials.EvidenceLocator{}, err
	}
	return analyzematerials.EvidenceLocator{
		Kind:         kind,
		Start:        start,
		End:          end,
		SourceLength: sourceLength,
		X:            int(optionalInt32(protocolLocator.ImageX)),
		Y:            int(optionalInt32(protocolLocator.ImageY)),
		Width:        int(optionalInt32(protocolLocator.ImageWidth)),
		Height:       int(optionalInt32(protocolLocator.ImageHeight)),
	}, nil
}

func mapMaterialAnalysisLocatorKind(value foundationv1.AssetAnalysisPreviewLocatorKindV1) (string, error) {
	switch value {
	case foundationv1.AssetAnalysisPreviewLocatorKindV1_TEXT_RANGE:
		return "text_range", nil
	case foundationv1.AssetAnalysisPreviewLocatorKindV1_IMAGE_WHOLE:
		return "image_whole", nil
	case foundationv1.AssetAnalysisPreviewLocatorKindV1_IMAGE_REGION:
		return "image_region", nil
	default:
		return "", fmt.Errorf("unknown material analysis locator kind: %d", value)
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func optionalInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func materialAnalysisInt64(value int64) (int, error) {
	converted := int(value)
	if int64(converted) != value {
		return 0, fmt.Errorf("material analysis locator value overflows int: %d", value)
	}
	return converted, nil
}

func canonicalMaterialAnalysisUUIDv7(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.String() == value
}

func mapMaterialAnalysisRPCError(parentCtx, requestCtx context.Context, err error) error {
	if parentErr := parentCtx.Err(); parentErr != nil {
		return parentErr
	}
	if requestErr := requestCtx.Err(); requestErr != nil {
		return requestErr
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}

	resultCode := analyzematerials.ResultCodeInternal
	var serviceError *foundationv1.FoundationServiceExceptionV1
	if errors.As(err, &serviceError) {
		switch serviceError.Code {
		case "NOT_FOUND":
			resultCode = analyzematerials.ResultCodeMaterialsNotAvailable
		case "ASSET_ANALYSIS_VERSION_CONFLICT", "LIMIT_EXCEEDED":
			resultCode = analyzematerials.ResultCodeSnapshotInvalid
		case "ASSET_ANALYSIS_EVIDENCE_CONFLICT":
			resultCode = analyzematerials.ResultCodeEvidenceConflict
		case "FEATURE_DISABLED", "PREVIEW_UNAVAILABLE", "PERSISTENCE_UNAVAILABLE":
			resultCode = analyzematerials.ResultCodeInternal
		default:
			resultCode = analyzematerials.ResultCodeInternal
		}
	}
	return materialAnalysisError(resultCode, err)
}

func materialAnalysisError(resultCode string, cause error) error {
	return analyzematerials.NewContractError(resultCode, cause)
}
