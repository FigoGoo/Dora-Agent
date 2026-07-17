package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/assetanalysis"
	"github.com/FigoGoo/Dora-Agent/business/internal/textmaterial"
)

// TestTextMaterialRepositoryPostgreSQLSemantics 使用真实 PostgreSQL 16 验证并发幂等、Owner 列表和 analyze_materials 可读性。
func TestTextMaterialRepositoryPostgreSQLSemantics(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	repository, err := NewTextMaterialRepository(&Client{db: db})
	if err != nil {
		t.Fatalf("NewTextMaterialRepository() error = %v", err)
	}
	ownerUserID := newRepositoryTestUUIDv7(t)
	otherUserID := newRepositoryTestUUIDv7(t)
	projectID := newRepositoryTestUUIDv7(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Create(&projectModel{
		ID: projectID, OwnerUserID: ownerUserID, Title: "文本素材集成项目", LifecycleStatus: "active",
		RecentRunStatus: "idle", InitialPromptStatus: "absent", Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	content := "真实 PostgreSQL 文本素材"
	material := textmaterial.TextMaterial{
		AssetID: newRepositoryTestUUIDv7(t), EvidenceID: newRepositoryTestUUIDv7(t),
		OwnerUserID: ownerUserID, ProjectID: projectID, AssetVersion: 1,
		ContentDigest: textmaterial.ContentDigest(content), Content: content, CreatedAt: now,
	}

	const concurrency = 16
	results := make(chan textmaterial.CreateResult, concurrency)
	errorsChannel := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			result, err := repository.CreateOrReplay(context.Background(), material)
			if err != nil {
				errorsChannel <- err
				return
			}
			results <- result
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Errorf("concurrent create failed: %v", err)
	}
	if t.Failed() {
		t.FailNow()
	}
	created := 0
	replayed := 0
	for result := range results {
		if result.Replayed {
			replayed++
		} else {
			created++
		}
	}
	if created != 1 || replayed != concurrency-1 {
		t.Fatalf("unexpected dispositions: created=%d replayed=%d", created, replayed)
	}

	conflicting := material
	conflicting.EvidenceID = newRepositoryTestUUIDv7(t)
	conflicting.Content = "同键不同正文"
	conflicting.ContentDigest = textmaterial.ContentDigest(conflicting.Content)
	if _, err := repository.CreateOrReplay(context.Background(), conflicting); !errors.Is(err, textmaterial.ErrIdempotencyConflict) {
		t.Fatalf("different content error = %v", err)
	}

	items, err := repository.ListOwned(context.Background(), textmaterial.ListQuery{
		OwnerUserID: ownerUserID, ProjectID: projectID, Limit: textmaterial.MaxListItems,
	})
	if err != nil || len(items) != 1 || items[0].Content != content {
		t.Fatalf("owned list items=%+v error=%v", items, err)
	}
	hidden, err := repository.ListOwned(context.Background(), textmaterial.ListQuery{
		OwnerUserID: otherUserID, ProjectID: projectID, Limit: textmaterial.MaxListItems,
	})
	if !errors.Is(err, textmaterial.ErrProjectNotFound) || len(hidden) != 0 {
		t.Fatalf("cross-owner list items=%+v error=%v", hidden, err)
	}

	var counts struct {
		Assets   int64 `gorm:"column:assets"`
		Evidence int64 `gorm:"column:evidence"`
	}
	if err := db.Raw(`SELECT
		(SELECT count(*) FROM business.asset_analysis_preview_assets WHERE id = ?) AS assets,
		(SELECT count(*) FROM business.asset_analysis_preview_evidence WHERE asset_id = ?) AS evidence`,
		material.AssetID, material.AssetID).Scan(&counts).Error; err != nil {
		t.Fatalf("count text material facts: %v", err)
	}
	if counts.Assets != 1 || counts.Evidence != 1 {
		t.Fatalf("unexpected fact counts: %+v", counts)
	}

	analysisRepository, err := NewAssetAnalysisRepository(&Client{db: db})
	if err != nil {
		t.Fatal(err)
	}
	analysisService, err := assetanalysis.NewService(analysisRepository)
	if err != nil {
		t.Fatal(err)
	}
	expectedVersion := int64(1)
	snapshot, err := analysisService.BatchGet(context.Background(), assetanalysis.Query{
		SchemaVersion: assetanalysis.RPCSchemaVersion, RequestID: newRepositoryTestUUIDv7(t),
		UserID: ownerUserID, ProjectID: projectID,
		Targets: []assetanalysis.Target{{AssetID: material.AssetID, ExpectedAssetVersion: &expectedVersion}},
	})
	if err != nil || len(snapshot.Assets) != 1 || len(snapshot.Assets[0].Evidence) != 1 ||
		snapshot.Assets[0].Evidence[0].Content != content {
		t.Fatalf("analyze_materials snapshot=%+v error=%v", snapshot, err)
	}

	if err := db.Exec(`UPDATE business.asset_analysis_preview_evidence SET content = 'mutated' WHERE asset_id = ?`, material.AssetID).Error; err == nil {
		t.Fatal("immutable text evidence update unexpectedly succeeded")
	}
}
