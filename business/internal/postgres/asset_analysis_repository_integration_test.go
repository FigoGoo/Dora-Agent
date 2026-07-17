package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/assetanalysis"
)

// TestAssetAnalysisRepositoryPostgreSQLAuthorizedCollection 使用真实 PostgreSQL 16 验证授权集合读取与 Evidence 不可变性。
func TestAssetAnalysisRepositoryPostgreSQLAuthorizedCollection(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	repository, err := NewAssetAnalysisRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("NewAssetAnalysisRepository() error = %v", err)
	}
	userID := newRepositoryTestUUIDv7(t)
	otherUserID := newRepositoryTestUUIDv7(t)
	projectID := newRepositoryTestUUIDv7(t)
	assetID := newRepositoryTestUUIDv7(t)
	evidenceID := newRepositoryTestUUIDv7(t)
	imageAssetID := newRepositoryTestUUIDv7(t)
	imageEvidenceID := newRepositoryTestUUIDv7(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Create(&projectModel{
		ID: projectID, OwnerUserID: userID, Title: "素材分析集成项目", LifecycleStatus: "active",
		RecentRunStatus: "idle", InitialPromptStatus: "absent", Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := db.Exec(`INSERT INTO business.asset_analysis_preview_assets
		(id, owner_user_id, project_id, asset_version, media_type, status, created_at)
		VALUES (?, ?, ?, 1, 'text', 'ready', ?)`, assetID, userID, projectID, now).Error; err != nil {
		t.Fatalf("seed preview asset: %v", err)
	}
	if err := db.Exec(`INSERT INTO business.asset_analysis_preview_assets
		(id, owner_user_id, project_id, asset_version, media_type, status, created_at)
		VALUES (?, ?, ?, 2, 'image', 'ready', ?)`, imageAssetID, userID, projectID, now).Error; err != nil {
		t.Fatalf("seed image preview asset: %v", err)
	}
	content := "真实 PostgreSQL 证据"
	digest := sha256.Sum256([]byte(content))
	if err := db.Exec(`INSERT INTO business.asset_analysis_preview_evidence
		(id, asset_id, asset_version, media_type, evidence_kind, availability, content_digest,
		extractor_schema_version, extractor_version, locator_kind, text_start, text_end, text_source_length, content, created_at)
		VALUES (?, ?, 1, 'text', 'text_segment', 'ready', ?, 'text.evidence.v1', 'extractor.v1',
		'text_range', 0, 4, 12, ?, ?)`, evidenceID, assetID, hex.EncodeToString(digest[:]), content, now).Error; err != nil {
		t.Fatalf("seed preview evidence: %v", err)
	}
	imageContent := "蓝色产品置于白色背景中央"
	imageDigest := sha256.Sum256([]byte(imageContent))
	if err := db.Exec(`INSERT INTO business.asset_analysis_preview_evidence
		(id, asset_id, asset_version, media_type, evidence_kind, availability, content_digest,
		extractor_schema_version, extractor_version, locator_kind, image_x, image_y, image_width, image_height, content, created_at)
		VALUES (?, ?, 2, 'image', 'visual_description', 'ready', ?, 'visual.evidence.v1', 'extractor.v1',
		'image_region', 100, 200, 300, 400, ?, ?)`, imageEvidenceID, imageAssetID, hex.EncodeToString(imageDigest[:]), imageContent, now).Error; err != nil {
		t.Fatalf("seed image preview evidence: %v", err)
	}

	assetIDs := []string{assetID, imageAssetID}
	sort.Strings(assetIDs)
	assets, err := repository.BatchGetAuthorized(context.Background(), assetanalysis.RepositoryQuery{
		UserID: userID, ProjectID: projectID, AssetIDs: assetIDs,
	})
	if err != nil || len(assets) != 2 {
		t.Fatalf("authorized collection assets=%+v error=%v", assets, err)
	}
	assetsByID := map[string]assetanalysis.Asset{assets[0].ID: assets[0], assets[1].ID: assets[1]}
	if len(assetsByID[assetID].Evidence) != 1 || assetsByID[assetID].Evidence[0].Content != content ||
		len(assetsByID[imageAssetID].Evidence) != 1 || assetsByID[imageAssetID].Evidence[0].Content != imageContent ||
		assetsByID[imageAssetID].Evidence[0].Locator == nil ||
		assetsByID[imageAssetID].Evidence[0].Locator.Kind != assetanalysis.LocatorKindImageRegion {
		t.Fatalf("text/image evidence mapping mismatch: %+v", assetsByID)
	}
	service, err := assetanalysis.NewService(repository)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	requestID := newRepositoryTestUUIDv7(t)
	expectedVersion := int64(1)
	snapshot, err := service.BatchGet(context.Background(), assetanalysis.Query{
		SchemaVersion: assetanalysis.RPCSchemaVersion, RequestID: requestID, UserID: userID, ProjectID: projectID,
		Targets: []assetanalysis.Target{{AssetID: assetID, ExpectedAssetVersion: &expectedVersion}},
	})
	if err != nil || !snapshot.ResponseComplete || len(snapshot.Assets) != 1 {
		t.Fatalf("authorized service snapshot=%+v error=%v", snapshot, err)
	}
	wrongVersion := int64(2)
	if _, err := service.BatchGet(context.Background(), assetanalysis.Query{
		SchemaVersion: assetanalysis.RPCSchemaVersion, RequestID: requestID, UserID: userID, ProjectID: projectID,
		Targets: []assetanalysis.Target{{AssetID: assetID, ExpectedAssetVersion: &wrongVersion}},
	}); !errors.Is(err, assetanalysis.ErrVersionConflict) {
		t.Fatalf("authorized version conflict error=%v", err)
	}
	missingID := newRepositoryTestUUIDv7(t)
	targets := []assetanalysis.Target{{AssetID: assetID, ExpectedAssetVersion: &wrongVersion}, {AssetID: missingID}}
	sort.Slice(targets, func(left, right int) bool { return targets[left].AssetID < targets[right].AssetID })
	if _, err := service.BatchGet(context.Background(), assetanalysis.Query{
		SchemaVersion: assetanalysis.RPCSchemaVersion, RequestID: requestID, UserID: userID, ProjectID: projectID, Targets: targets,
	}); !errors.Is(err, assetanalysis.ErrNotFound) {
		t.Fatalf("exact-set must hide version before authorization: %v", err)
	}
	hidden, err := repository.BatchGetAuthorized(context.Background(), assetanalysis.RepositoryQuery{
		UserID: otherUserID, ProjectID: projectID, AssetIDs: assetIDs,
	})
	if err != nil || len(hidden) != 0 {
		t.Fatalf("cross-owner collection assets=%+v error=%v", hidden, err)
	}
	if err := db.Exec(`UPDATE business.asset_analysis_preview_evidence SET content = 'mutated' WHERE id = ?`, evidenceID).Error; err == nil {
		t.Fatal("immutable evidence update unexpectedly succeeded")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = repository.BatchGetAuthorized(cancelled, assetanalysis.RepositoryQuery{UserID: userID, ProjectID: projectID, AssetIDs: []string{assetID}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
}
