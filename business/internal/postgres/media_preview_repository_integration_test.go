package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/mediapreview"
	"github.com/FigoGoo/Dora-Agent/business/internal/promptpreview"
	"gorm.io/gorm"
)

// TestMediaPreviewRepositoryPostgreSQLSemantics 使用真实 PostgreSQL 验证 Source Guard、并发幂等、Fence 与文件发布。
func TestMediaPreviewRepositoryPostgreSQLSemantics(t *testing.T) {
	_, db := openBusinessIntegrationRepository(t)
	assertMediaPreviewSchemaContract(t, db)
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := mediapreview.OpenLocalObjectStore(root)
	if err != nil {
		t.Fatalf("OpenLocalObjectStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repository, err := NewMediaPreviewRepository(&Client{db: db}, store)
	if err != nil {
		t.Fatalf("NewMediaPreviewRepository() error = %v", err)
	}

	ownerUserID := newRepositoryTestUUIDv7(t)
	projectID := newRepositoryTestUUIDv7(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Create(&projectModel{
		ID: projectID, OwnerUserID: ownerUserID, Title: "媒体预览集成项目",
		LifecycleStatus: "active", RecentRunStatus: "idle", InitialPromptStatus: "absent",
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed Media Preview Project: %v", err)
	}
	promptID, promptDigest := seedMediaPreviewPromptDraft(t, db, ownerUserID, projectID, now)
	command := mediaPreviewGeneratePrepareCommand(t, ownerUserID, projectID, promptID, promptDigest)

	const concurrency = 12
	results := make(chan mediapreview.PrepareResult, concurrency)
	errorsChannel := make(chan error, concurrency)
	var waitGroup sync.WaitGroup
	waitGroup.Add(concurrency)
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			result, prepareErr := repository.Prepare(context.Background(), command, mediaPreviewPreparationAllocation(t, command.ToolKey, now))
			if prepareErr != nil {
				errorsChannel <- prepareErr
				return
			}
			results <- result
		}()
	}
	waitGroup.Wait()
	close(results)
	close(errorsChannel)
	for prepareErr := range errorsChannel {
		t.Errorf("concurrent Prepare() error = %v", prepareErr)
	}
	if t.Failed() {
		t.FailNow()
	}
	created, replayed := 0, 0
	var preparation mediapreview.Preparation
	for result := range results {
		if preparation.PreparationID == "" {
			preparation = result.Preparation
		}
		if result.Preparation.PreparationID != preparation.PreparationID ||
			result.Preparation.AssetRef.AssetID != preparation.AssetRef.AssetID {
			t.Fatalf("Prepare results did not converge: first=%+v result=%+v", preparation, result.Preparation)
		}
		if result.Disposition == mediapreview.CommandDispositionCreated {
			created++
		} else if result.Disposition == mediapreview.CommandDispositionReplayed {
			replayed++
		}
	}
	if created != 1 || replayed != concurrency-1 {
		t.Fatalf("Prepare dispositions created=%d replayed=%d", created, replayed)
	}

	queryPreparation, err := repository.QueryPreparation(context.Background(), mediapreview.PreparationQuery{
		CommandID: command.CommandID, RequestDigest: command.RequestDigest,
		OwnerUserID: ownerUserID, ProjectID: projectID,
	})
	if err != nil || queryPreparation.Status != mediapreview.QueryStatusCompleted ||
		queryPreparation.Preparation == nil || queryPreparation.Preparation.PreparationID != preparation.PreparationID {
		t.Fatalf("QueryPreparation() result=%+v error=%v", queryPreparation, err)
	}
	wrongDigest := command.RequestDigest
	wrongDigest[0] ^= 0xff
	conflictQuery, err := repository.QueryPreparation(context.Background(), mediapreview.PreparationQuery{
		CommandID: command.CommandID, RequestDigest: wrongDigest, OwnerUserID: ownerUserID, ProjectID: projectID,
	})
	if err != nil || conflictQuery.Status != mediapreview.QueryStatusConflict {
		t.Fatalf("wrong Prepare digest result=%+v error=%v", conflictQuery, err)
	}
	hiddenQuery, err := repository.QueryPreparation(context.Background(), mediapreview.PreparationQuery{
		CommandID: command.CommandID, RequestDigest: command.RequestDigest,
		OwnerUserID: newRepositoryTestUUIDv7(t), ProjectID: projectID,
	})
	if err != nil || hiddenQuery.Status != mediapreview.QueryStatusNotFound {
		t.Fatalf("cross-owner Prepare query result=%+v error=%v", hiddenQuery, err)
	}

	conflictingCommand := command
	conflictingCommand.ScopeDigest = sha256.Sum256([]byte("different scope"))
	if _, err := repository.Prepare(context.Background(), conflictingCommand, mediaPreviewPreparationAllocation(t, command.ToolKey, now)); !errors.Is(err, mediapreview.ErrIdempotencyConflict) {
		t.Fatalf("same Prepare command different semantic error=%v", err)
	}
	crossOwner := command
	crossOwner.RequestID = newRepositoryTestUUIDv7(t)
	crossOwner.CommandID = newRepositoryTestUUIDv7(t)
	crossOwner.OperationID = newRepositoryTestUUIDv7(t)
	crossOwner.OwnerUserID = newRepositoryTestUUIDv7(t)
	crossOwner.RequestDigest = sha256.Sum256([]byte("cross owner"))
	if _, err := repository.Prepare(context.Background(), crossOwner, mediaPreviewPreparationAllocation(t, command.ToolKey, now)); !errors.Is(err, mediapreview.ErrNotFound) {
		t.Fatalf("cross-owner Prompt was not hidden: %v", err)
	}

	output := writeMediaPreviewPNG(t, root, preparation.StagingObjectKey)
	finalize := mediaPreviewFinalizeCommand(t, preparation, output)
	finalResults := make(chan mediapreview.FinalizeResult, concurrency)
	finalErrors := make(chan error, concurrency)
	waitGroup.Add(concurrency)
	for range concurrency {
		go func() {
			defer waitGroup.Done()
			result, finalizeErr := repository.Finalize(context.Background(), finalize, mediapreview.FinalizationAllocation{
				ReceiptID: newRepositoryTestUUIDv7(t), CompletedAt: now.Add(time.Minute),
			})
			if finalizeErr != nil {
				finalErrors <- finalizeErr
				return
			}
			finalResults <- result
		}()
	}
	waitGroup.Wait()
	close(finalResults)
	close(finalErrors)
	for finalizeErr := range finalErrors {
		t.Errorf("concurrent Finalize() error = %v", finalizeErr)
	}
	if t.Failed() {
		t.FailNow()
	}
	created, replayed = 0, 0
	var finalization mediapreview.Finalization
	for result := range finalResults {
		if finalization.ReceiptID == "" {
			finalization = result.Finalization
		}
		if result.Finalization.ReceiptID != finalization.ReceiptID {
			t.Fatalf("Finalize results did not converge: first=%+v result=%+v", finalization, result.Finalization)
		}
		if result.Disposition == mediapreview.CommandDispositionCreated {
			created++
		} else if result.Disposition == mediapreview.CommandDispositionReplayed {
			replayed++
		}
	}
	if created != 1 || replayed != concurrency-1 {
		t.Fatalf("Finalize dispositions created=%d replayed=%d", created, replayed)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(preparation.StagingObjectKey))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging file remains after Finalize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(preparation.FinalObjectKey))); err != nil {
		t.Fatalf("ready object missing after Finalize: %v", err)
	}

	queryFinalization, err := repository.QueryFinalization(context.Background(), mediapreview.FinalizationQuery{
		CommandID: finalize.CommandID, RequestDigest: finalize.RequestDigest, PreparationID: preparation.PreparationID,
	})
	if err != nil || queryFinalization.Status != mediapreview.QueryStatusCompleted ||
		queryFinalization.Finalization == nil || queryFinalization.Finalization.ReceiptID != finalization.ReceiptID {
		t.Fatalf("QueryFinalization() result=%+v error=%v", queryFinalization, err)
	}

	content, file, err := repository.OpenReadyContent(context.Background(), mediapreview.ContentQuery{
		OwnerUserID: ownerUserID, ProjectID: projectID, AssetID: preparation.AssetRef.AssetID,
	})
	if err != nil || file == nil || content.AssetRef.ContentDigest != output.ContentDigest {
		t.Fatalf("OpenReadyContent() content=%+v file=%v error=%v", content, file, err)
	}
	_ = file.Close()
	_, hiddenFile, err := repository.OpenReadyContent(context.Background(), mediapreview.ContentQuery{
		OwnerUserID: newRepositoryTestUUIDv7(t), ProjectID: projectID, AssetID: preparation.AssetRef.AssetID,
	})
	if !errors.Is(err, mediapreview.ErrNotFound) || hiddenFile != nil {
		t.Fatalf("cross-owner ready content file=%v error=%v", hiddenFile, err)
	}

	assemble := mediaPreviewAssemblePrepareCommand(t, ownerUserID, projectID, preparation.AssetRef.AssetID, output.ContentDigest)
	assembleResult, err := repository.Prepare(context.Background(), assemble, mediaPreviewPreparationAllocation(t, assemble.ToolKey, now.Add(2*time.Minute)))
	if err != nil || assembleResult.Preparation.SourceRef.SourceType != mediapreview.SourceTypeImageAsset ||
		assembleResult.Preparation.SourceRef.SourceObjectKey != preparation.FinalObjectKey {
		t.Fatalf("assemble Prepare result=%+v error=%v", assembleResult, err)
	}
	failedFinalize := mediaPreviewFailedFinalizeCommand(t, assembleResult.Preparation)
	failedResult, err := repository.Finalize(context.Background(), failedFinalize, mediapreview.FinalizationAllocation{
		ReceiptID: newRepositoryTestUUIDv7(t), CompletedAt: now.Add(3 * time.Minute),
	})
	if err != nil || failedResult.Finalization.AssetRef.Status != mediapreview.StatusFailed ||
		failedResult.Finalization.ErrorCode != "FFMPEG_UNAVAILABLE" {
		t.Fatalf("failed Finalize result=%+v error=%v", failedResult, err)
	}
	stale := failedFinalize
	stale.RequestID = newRepositoryTestUUIDv7(t)
	stale.CommandID = newRepositoryTestUUIDv7(t)
	stale.RequestDigest = sha256.Sum256([]byte("stale finalize"))
	stale.AttemptID = newRepositoryTestUUIDv7(t)
	stale.Fence--
	if _, err := repository.Finalize(context.Background(), stale, mediapreview.FinalizationAllocation{
		ReceiptID: newRepositoryTestUUIDv7(t), CompletedAt: now.Add(4 * time.Minute),
	}); !errors.Is(err, mediapreview.ErrFenceStale) {
		t.Fatalf("stale Fence error=%v", err)
	}

	var counts struct {
		Assets        int64 `gorm:"column:assets"`
		Preparations  int64 `gorm:"column:preparations"`
		Finalizations int64 `gorm:"column:finalizations"`
	}
	if err := db.Raw(`SELECT
		(SELECT count(*) FROM business.media_preview_asset) AS assets,
		(SELECT count(*) FROM business.media_preview_preparation_receipt) AS preparations,
		(SELECT count(*) FROM business.media_preview_finalization_receipt) AS finalizations`).Scan(&counts).Error; err != nil {
		t.Fatal(err)
	}
	if counts.Assets != 2 || counts.Preparations != 2 || counts.Finalizations != 2 {
		t.Fatalf("unexpected Media Preview fact counts: %+v", counts)
	}
}

func assertMediaPreviewSchemaContract(t *testing.T, db *gorm.DB) {
	t.Helper()
	var contract struct {
		ForeignKeys           int64 `gorm:"column:foreign_keys"`
		MissingTableComments  int64 `gorm:"column:missing_table_comments"`
		MissingColumnComments int64 `gorm:"column:missing_column_comments"`
	}
	if err := db.Raw(`
		SELECT
			(SELECT count(*) FROM pg_constraint AS constraint_record
			 JOIN pg_class AS table_record ON table_record.oid = constraint_record.conrelid
			 JOIN pg_namespace AS schema_record ON schema_record.oid = table_record.relnamespace
			 WHERE schema_record.nspname = 'business'
			   AND table_record.relname IN ('media_preview_asset', 'media_preview_preparation_receipt', 'media_preview_finalization_receipt')
			   AND constraint_record.contype = 'f') AS foreign_keys,
			(SELECT count(*) FROM pg_class AS table_record
			 JOIN pg_namespace AS schema_record ON schema_record.oid = table_record.relnamespace
			 WHERE schema_record.nspname = 'business'
			   AND table_record.relname IN ('media_preview_asset', 'media_preview_preparation_receipt', 'media_preview_finalization_receipt')
			   AND coalesce(obj_description(table_record.oid, 'pg_class'), '') !~ '[一-龥]') AS missing_table_comments,
			(SELECT count(*) FROM pg_attribute AS column_record
			 JOIN pg_class AS table_record ON table_record.oid = column_record.attrelid
			 JOIN pg_namespace AS schema_record ON schema_record.oid = table_record.relnamespace
			 WHERE schema_record.nspname = 'business'
			   AND table_record.relname IN ('media_preview_asset', 'media_preview_preparation_receipt', 'media_preview_finalization_receipt')
			   AND column_record.attnum > 0 AND NOT column_record.attisdropped
			   AND coalesce(col_description(table_record.oid, column_record.attnum), '') !~ '[一-龥]') AS missing_column_comments`).Scan(&contract).Error; err != nil {
		t.Fatalf("query Media Preview schema contract: %v", err)
	}
	if contract.ForeignKeys != 0 || contract.MissingTableComments != 0 || contract.MissingColumnComments != 0 {
		t.Fatalf("Media Preview schema contract violated: %+v", contract)
	}
}

func seedMediaPreviewPromptDraft(t *testing.T, db *gorm.DB, userID string, projectID string, now time.Time) (string, mediapreview.Digest) {
	t.Helper()
	storyboardDigest := sha256.Sum256([]byte("media preview storyboard"))
	reference := promptpreview.StoryboardPreviewRef{
		ID: newRepositoryTestUUIDv7(t), Version: 1, ContentDigest: hex.EncodeToString(storyboardDigest[:]),
	}
	content := promptpreview.Content{
		SchemaVersion: promptpreview.DraftSchemaVersion, Mode: promptpreview.DraftMode,
		SourceStoryboardPreviewRef: reference,
		Prompts: []promptpreview.PromptEntry{{
			TargetLocalKey: "slot_1", ElementLocalKey: "element_1", SlotType: "image", MediaKind: "image",
			Purpose: "主视觉", Required: true, PositivePrompt: "生成媒体预览主视觉",
			NegativeConstraints: []string{}, OutputLanguage: "zh-CN",
		}},
	}
	contentJSON, err := content.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := promptpreview.ContentDigest(content)
	if err != nil {
		t.Fatal(err)
	}
	exactTargetSetDigest := sha256.Sum256([]byte("targets"))
	promptID := newRepositoryTestUUIDv7(t)
	model := promptPreviewDraftModel{
		ID: promptID, ProjectID: projectID, UserID: userID,
		StoryboardPreviewID: reference.ID, StoryboardPreviewVersion: 1,
		StoryboardPreviewContentDigest: storyboardDigest[:], Status: promptpreview.DraftStatus,
		Version: 1, SchemaVersion: promptpreview.DraftSchemaVersion, ContentJSON: contentJSON,
		ContentDigest: digest.Bytes(), ExactTargetSetDigest: exactTargetSetDigest[:],
		SourceToolCallID: newRepositoryTestUUIDv7(t), SourcePromptVersion: "write_prompts.preview.v1",
		SourceValidatorVersion: "validator.v1", SourceExactSetValidatorVersion: "exact-set.v1",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&model).Error; err != nil {
		t.Fatalf("seed Prompt Preview Draft: %v", err)
	}
	converted, err := mediapreview.DigestFromBytes(digest.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return promptID, converted
}

func mediaPreviewGeneratePrepareCommand(t *testing.T, userID string, projectID string, promptID string, promptDigest mediapreview.Digest) mediapreview.PrepareCommand {
	t.Helper()
	return mediapreview.PrepareCommand{
		RequestID: newRepositoryTestUUIDv7(t), CommandID: newRepositoryTestUUIDv7(t), OperationID: newRepositoryTestUUIDv7(t),
		RequestDigest: sha256.Sum256([]byte("generate Prepare")), OwnerUserID: userID, ProjectID: projectID,
		ToolKey: mediapreview.ToolGenerateMedia, ScopeDigest: sha256.Sum256([]byte("generate scope")),
		OutputProfile: mediapreview.OutputProfilePNG,
		PromptSource:  &mediapreview.PromptSource{ID: promptID, Version: 1, ContentDigest: promptDigest, TargetLocalKey: "slot_1"},
	}
}

func mediaPreviewAssemblePrepareCommand(t *testing.T, userID string, projectID string, sourceAssetID string, sourceDigest mediapreview.Digest) mediapreview.PrepareCommand {
	t.Helper()
	return mediapreview.PrepareCommand{
		RequestID: newRepositoryTestUUIDv7(t), CommandID: newRepositoryTestUUIDv7(t), OperationID: newRepositoryTestUUIDv7(t),
		RequestDigest: sha256.Sum256([]byte("assemble Prepare")), OwnerUserID: userID, ProjectID: projectID,
		ToolKey: mediapreview.ToolAssembleOutput, ScopeDigest: sha256.Sum256([]byte("assemble scope")),
		OutputProfile:    mediapreview.OutputProfileMP4,
		ImageAssetSource: &mediapreview.ImageAssetSource{ID: sourceAssetID, Version: 1, ContentDigest: sourceDigest},
	}
}

func mediaPreviewPreparationAllocation(t *testing.T, toolKey string, createdAt time.Time) mediapreview.PreparationAllocation {
	t.Helper()
	preparationID := newRepositoryTestUUIDv7(t)
	assetID := newRepositoryTestUUIDv7(t)
	staging, final, err := mediapreview.ObjectKeys(assetID, preparationID, toolKey)
	if err != nil {
		t.Fatal(err)
	}
	return mediapreview.PreparationAllocation{
		PreparationID: preparationID, AssetID: assetID, StagingObjectKey: staging,
		FinalObjectKey: final, CreatedAt: createdAt,
	}
}

func writeMediaPreviewPNG(t *testing.T, root string, objectKey string) mediapreview.OutputMetadata {
	t.Helper()
	preview := image.NewRGBA(image.Rect(0, 0, mediapreview.PNGWidth, mediapreview.PNGHeight))
	for y := 0; y < mediapreview.PNGHeight; y++ {
		for x := 0; x < mediapreview.PNGWidth; x++ {
			preview.SetRGBA(x, y, color.RGBA{R: byte(x), G: byte(y), B: 0x55, A: 0xff})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, preview); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(objectKey)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return mediapreview.OutputMetadata{
		ContentDigest: sha256.Sum256(data), SizeBytes: int64(len(data)), MIMEType: mediapreview.MIMEPNG,
		Width: mediapreview.PNGWidth, Height: mediapreview.PNGHeight,
	}
}

func mediaPreviewFinalizeCommand(t *testing.T, preparation mediapreview.Preparation, output mediapreview.OutputMetadata) mediapreview.FinalizeCommand {
	t.Helper()
	return mediapreview.FinalizeCommand{
		RequestID: newRepositoryTestUUIDv7(t), CommandID: newRepositoryTestUUIDv7(t),
		RequestDigest: sha256.Sum256([]byte("ready Finalize")), PreparationID: preparation.PreparationID,
		OperationID: preparation.OperationID, BatchID: newRepositoryTestUUIDv7(t), JobID: newRepositoryTestUUIDv7(t),
		AttemptID: newRepositoryTestUUIDv7(t), Fence: 2, TerminalStatus: mediapreview.StatusReady, Output: &output,
	}
}

func mediaPreviewFailedFinalizeCommand(t *testing.T, preparation mediapreview.Preparation) mediapreview.FinalizeCommand {
	t.Helper()
	return mediapreview.FinalizeCommand{
		RequestID: newRepositoryTestUUIDv7(t), CommandID: newRepositoryTestUUIDv7(t),
		RequestDigest: sha256.Sum256([]byte("failed Finalize")), PreparationID: preparation.PreparationID,
		OperationID: preparation.OperationID, BatchID: newRepositoryTestUUIDv7(t), JobID: newRepositoryTestUUIDv7(t),
		AttemptID: newRepositoryTestUUIDv7(t), Fence: 2, TerminalStatus: mediapreview.StatusFailed,
		ErrorCode: "FFMPEG_UNAVAILABLE",
	}
}
