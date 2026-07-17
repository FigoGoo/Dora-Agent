package foundationrpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/assetanalysis"
	"github.com/FigoGoo/Dora-Agent/business/internal/config"
	"github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1"
)

const (
	rpcPreviewAssetID    = "019f68e8-0016-7000-8000-000000000016"
	rpcPreviewEvidenceID = "019f68e8-0017-7000-8000-000000000017"
)

type assetAnalysisRPCServiceStub struct {
	snapshot assetanalysis.Snapshot
	err      error
	calls    int
	query    assetanalysis.Query
}

func (stub *assetAnalysisRPCServiceStub) BatchGet(_ context.Context, query assetanalysis.Query) (assetanalysis.Snapshot, error) {
	stub.calls++
	stub.query = query
	return stub.snapshot, stub.err
}

func newAssetAnalysisRPCHandler(t *testing.T, service *assetAnalysisRPCServiceStub, enabled bool) *Handler {
	t.Helper()
	handler, err := NewHandlerWithDevelopmentPreviews(config.ServiceConfig{
		Name: "dora-business-service", Version: "test", Environment: "local", InstanceID: "business-test-1",
	}, fixedClock{now: time.Now()}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		&creationSpecRPCServiceStub{}, false, service, enabled)
	if err != nil {
		t.Fatalf("NewHandlerWithDevelopmentPreviews() error = %v", err)
	}
	return handler
}

func TestAssetAnalysisPreviewRPCGateAndMapping(t *testing.T) {
	service := &assetAnalysisRPCServiceStub{}
	disabled := newAssetAnalysisRPCHandler(t, service, false)
	if _, err := disabled.BatchGetAssetAnalysisInputsPreviewV1(context.Background(), nil); foundationErrorCode(err) != featureDisabledCode || service.calls != 0 {
		t.Fatalf("disabled error=%v calls=%d", err, service.calls)
	}

	start, end, sourceLength := int64(0), int64(2), int64(2)
	service.snapshot = assetanalysis.Snapshot{SnapshotToken: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ResponseComplete: true,
		Assets: []assetanalysis.Asset{{ID: rpcPreviewAssetID, Version: 1, MediaType: assetanalysis.MediaTypeText, Evidence: []assetanalysis.Evidence{{
			ID: rpcPreviewEvidenceID, AssetID: rpcPreviewAssetID, AssetVersion: 1, MediaType: assetanalysis.MediaTypeText,
			Kind: assetanalysis.EvidenceKindTextSegment, Availability: assetanalysis.AvailabilityReady,
			Content: "文本", ContentDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			ExtractorSchemaVersion: "schema.v1", ExtractorVersion: "extractor.v1",
			Locator: &assetanalysis.Locator{Kind: assetanalysis.LocatorKindTextRange, TextStart: &start, TextEnd: &end, TextSourceLength: &sourceLength},
		}}}},
	}
	handler := newAssetAnalysisRPCHandler(t, service, true)
	expectedVersion := int64(1)
	response, err := handler.BatchGetAssetAnalysisInputsPreviewV1(context.Background(), &foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1{
		SchemaVersion: assetanalysis.RPCSchemaVersion, RequestId: rpcPreviewRequestID,
		UserId: rpcPreviewUserID, ProjectId: rpcPreviewProjectID,
		Targets: []*foundationv1.AssetAnalysisPreviewTargetV1{{AssetId: rpcPreviewAssetID, ExpectedAssetVersion: &expectedVersion}},
	})
	if err != nil || response == nil || !response.ResponseComplete || len(response.Assets) != 1 ||
		response.Assets[0].Evidence[0].Locator == nil || response.Assets[0].Evidence[0].Content == nil ||
		response.Assets[0].Evidence[0].ReasonCode != nil || service.query.Targets[0].ExpectedAssetVersion == nil {
		t.Fatalf("response=%+v query=%+v error=%v", response, service.query, err)
	}
}

func TestAssetAnalysisPreviewRPCStableErrorsAndContext(t *testing.T) {
	service := &assetAnalysisRPCServiceStub{}
	handler := newAssetAnalysisRPCHandler(t, service, true)
	request := &foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1{
		SchemaVersion: assetanalysis.RPCSchemaVersion, RequestId: rpcPreviewRequestID,
		UserId: rpcPreviewUserID, ProjectId: rpcPreviewProjectID,
		Targets: []*foundationv1.AssetAnalysisPreviewTargetV1{{AssetId: rpcPreviewAssetID}},
	}
	for _, testCase := range []struct {
		name string
		err  error
		code string
	}{
		{name: "invalid", err: assetanalysis.ErrInvalidArgument, code: invalidArgumentCode},
		{name: "not found", err: assetanalysis.ErrNotFound, code: notFoundCode},
		{name: "version", err: assetanalysis.ErrVersionConflict, code: assetVersionConflictCode},
		{name: "limit", err: assetanalysis.ErrLimitExceeded, code: limitExceededCode},
		{name: "evidence", err: assetanalysis.ErrEvidenceConflict, code: evidenceConflictCode},
		{name: "persistence", err: assetanalysis.ErrPersistence, code: persistenceCode},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service.err = testCase.err
			if _, err := handler.BatchGetAssetAnalysisInputsPreviewV1(context.Background(), request); foundationErrorCode(err) != testCase.code {
				t.Fatalf("service error=%v mapped error=%v want code=%s", testCase.err, err, testCase.code)
			}
		})
	}
	service.err = context.DeadlineExceeded
	if _, err := handler.BatchGetAssetAnalysisInputsPreviewV1(context.Background(), request); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v", err)
	}
}

func foundationErrorCode(err error) string {
	serviceError, ok := err.(*foundationv1.FoundationServiceExceptionV1)
	if !ok {
		return ""
	}
	return serviceError.Code
}
