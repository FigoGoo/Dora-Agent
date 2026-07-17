package assetanalysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"testing"

	"github.com/google/uuid"
)

type repositoryStub struct {
	assets []Asset
	err    error
	calls  int
	query  RepositoryQuery
}

func (stub *repositoryStub) BatchGetAuthorized(_ context.Context, query RepositoryQuery) ([]Asset, error) {
	stub.calls++
	stub.query = query
	return append([]Asset(nil), stub.assets...), stub.err
}

func TestServiceBatchGetAuthorizesBeforeVersionAndBuildsStableSnapshot(t *testing.T) {
	requestID := testUUIDv7(t)
	userID := testUUIDv7(t)
	projectID := testUUIDv7(t)
	assetIDs := sortedUUIDv7s(t, 2)
	evidenceIDs := sortedUUIDv7s(t, 2)
	start, end, sourceLength := int64(0), int64(5), int64(5)
	content := "你好世界"
	digest := sha256.Sum256([]byte(content))
	assets := []Asset{
		{ID: assetIDs[1], Version: 3, MediaType: MediaTypeImage, Evidence: []Evidence{{
			ID: evidenceIDs[1], AssetID: assetIDs[1], AssetVersion: 3, MediaType: MediaTypeImage,
			Kind: EvidenceKindSafetyLabel, Availability: AvailabilityMissing, ReasonCode: "NOT_EXTRACTED",
		}}},
		{ID: assetIDs[0], Version: 1, MediaType: MediaTypeText, Evidence: []Evidence{{
			ID: evidenceIDs[0], AssetID: assetIDs[0], AssetVersion: 1, MediaType: MediaTypeText,
			Kind: EvidenceKindTextSegment, Availability: AvailabilityReady, Content: content,
			ContentDigest: hex.EncodeToString(digest[:]), ExtractorSchemaVersion: "text.evidence.v1",
			ExtractorVersion: "extractor.v1", Locator: &Locator{
				Kind: LocatorKindTextRange, TextStart: &start, TextEnd: &end, TextSourceLength: &sourceLength,
			},
		}}},
	}
	repository := &repositoryStub{assets: assets}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	expectedVersion := int64(1)
	query := Query{SchemaVersion: RPCSchemaVersion, RequestID: requestID, UserID: userID, ProjectID: projectID,
		Targets: []Target{{AssetID: assetIDs[0], ExpectedAssetVersion: &expectedVersion}, {AssetID: assetIDs[1]}},
	}
	snapshot, err := service.BatchGet(context.Background(), query)
	if err != nil {
		t.Fatalf("BatchGet() error = %v", err)
	}
	if !snapshot.ResponseComplete || len(snapshot.Assets) != 2 || snapshot.Assets[0].ID != assetIDs[0] ||
		!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(snapshot.SnapshotToken) {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if repository.calls != 1 || len(repository.query.AssetIDs) != 2 {
		t.Fatalf("repository calls=%d query=%+v", repository.calls, repository.query)
	}
	repository.assets = assets
	repeated, err := service.BatchGet(context.Background(), query)
	if err != nil || repeated.SnapshotToken != snapshot.SnapshotToken {
		t.Fatalf("stable token changed: first=%s second=%s error=%v", snapshot.SnapshotToken, repeated.SnapshotToken, err)
	}

	// exact-set 缺失与错误版本并存时必须先返回 NOT_FOUND，不能泄漏已授权前的版本差异。
	repository.assets = []Asset{{ID: assetIDs[0], Version: 2, MediaType: MediaTypeText}}
	if _, err := service.BatchGet(context.Background(), query); !errors.Is(err, ErrNotFound) {
		t.Fatalf("exact-set before version error = %v", err)
	}
	repository.assets = []Asset{{ID: assetIDs[0], Version: 2, MediaType: MediaTypeText}, {ID: assetIDs[1], Version: 3, MediaType: MediaTypeImage}}
	if _, err := service.BatchGet(context.Background(), query); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("authorized version conflict error = %v", err)
	}
}

func TestServiceRejectsMalformedRequestsAndEvidence(t *testing.T) {
	ids := sortedUUIDv7s(t, 5)
	repository := &repositoryStub{}
	service, _ := NewService(repository)
	invalidOrder := Query{SchemaVersion: RPCSchemaVersion, RequestID: ids[0], UserID: ids[1], ProjectID: ids[2],
		Targets: []Target{{AssetID: ids[4]}, {AssetID: ids[3]}},
	}
	if _, err := service.BatchGet(context.Background(), invalidOrder); !errors.Is(err, ErrInvalidArgument) || repository.calls != 0 {
		t.Fatalf("unsorted request error=%v calls=%d", err, repository.calls)
	}

	query := Query{SchemaVersion: RPCSchemaVersion, RequestID: ids[0], UserID: ids[1], ProjectID: ids[2], Targets: []Target{{AssetID: ids[3]}}}
	start, end, length := int64(0), int64(1), int64(1)
	repository.assets = []Asset{{ID: ids[3], Version: 1, MediaType: MediaTypeText, Evidence: []Evidence{{
		ID: ids[4], AssetID: ids[3], AssetVersion: 1, MediaType: MediaTypeText,
		Kind: EvidenceKindTextSegment, Availability: AvailabilityReady, Content: "a",
		ContentDigest:          "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		ExtractorSchemaVersion: "schema.v1", ExtractorVersion: "extractor.v1",
		Locator: &Locator{Kind: LocatorKindTextRange, TextStart: &start, TextEnd: &end, TextSourceLength: &length},
	}}}}
	if _, err := service.BatchGet(context.Background(), query); !errors.Is(err, ErrEvidenceConflict) {
		t.Fatalf("digest mismatch error = %v", err)
	}

	evidence := make([]Evidence, MaxEvidence+1)
	for index := range evidence {
		evidenceID, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			t.Fatalf("NewV7() error = %v", uuidErr)
		}
		evidence[index] = Evidence{ID: evidenceID.String(), AssetID: ids[3], AssetVersion: 1, MediaType: MediaTypeText,
			Kind: EvidenceKindTextSegment, Availability: AvailabilityMissing, ReasonCode: "NOT_EXTRACTED"}
	}
	repository.assets = []Asset{{ID: ids[3], Version: 1, MediaType: MediaTypeText, Evidence: evidence}}
	if _, err := service.BatchGet(context.Background(), query); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("evidence limit error = %v", err)
	}
}

func testUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() error = %v", err)
	}
	return id.String()
}

func sortedUUIDv7s(t *testing.T, count int) []string {
	t.Helper()
	result := make([]string, count)
	for index := range result {
		result[index] = testUUIDv7(t)
	}
	// UUIDv7 生成器在同一进程中单调递增；显式比较可防测试对实现细节静默依赖。
	for index := 1; index < len(result); index++ {
		if result[index] <= result[index-1] {
			t.Fatalf("generated UUIDv7 values are not increasing: %v", result)
		}
	}
	return result
}
