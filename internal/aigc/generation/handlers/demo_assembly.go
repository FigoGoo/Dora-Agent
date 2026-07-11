package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

type DemoAssemblyJobHandlerConfig struct {
	Assets   DemoAudioAssetStore
	Uploader asset.Uploader
	Now      func() time.Time
}

type DemoAssemblyJobHandler struct{ cfg DemoAssemblyJobHandlerConfig }

func NewDemoAssemblyJobHandler(config DemoAssemblyJobHandlerConfig) DemoAssemblyJobHandler {
	if config.Now == nil {
		config.Now = time.Now
	}
	return DemoAssemblyJobHandler{cfg: config}
}

// Handle persists an immutable assembly manifest through the same object-store
// and finalization path as media providers. It is a demo render adapter: a real
// compositor can replace it while keeping Operation/Batch/Job semantics.
func (h DemoAssemblyJobHandler) Handle(ctx context.Context, job generation.GenerationJob) (generation.HandlerResult, error) {
	if h.cfg.Assets == nil || h.cfg.Uploader == nil {
		return generation.HandlerResult{}, fmt.Errorf("demo assembly asset store and uploader are required")
	}
	raw, err := json.MarshalIndent(map[string]any{
		"schema_version": "1.0", "provider": generation.ProviderAssembly,
		"job_id": job.ID, "operation_id": job.OperationID, "payload": job.Payload,
	}, "", "  ")
	if err != nil {
		return generation.HandlerResult{}, fmt.Errorf("encode assembly manifest: %w", err)
	}
	sum := sha256.Sum256([]byte(job.ID))
	assetID := "assembly_" + hex.EncodeToString(sum[:12])
	filename := assetID + ".json"
	objectKey := asset.NewObjectKey(job.SessionID, assetID, filename)
	uploaded, err := h.cfg.Uploader.Upload(ctx, asset.UploadInput{ObjectKey: objectKey, Content: bytes.NewReader(raw), ContentLength: int64(len(raw)), MIMEType: "application/json", Filename: filename, Metadata: map[string]string{"provider": generation.ProviderAssembly, "source_job_id": job.ID}})
	if err != nil {
		return generation.HandlerResult{}, fmt.Errorf("upload assembly manifest: %w", err)
	}
	now := h.cfg.Now()
	contentHash := sha256.Sum256(raw)
	saved, err := h.cfg.Assets.Save(ctx, asset.Asset{
		ID: assetID, SessionID: job.SessionID, UserID: job.UserID, SourceJobID: job.ID, OutputIndex: 0,
		Kind: asset.KindText, Source: asset.SourceGenerated, MIMEType: "application/json", Filename: filename,
		SizeBytes: int64(len(raw)), ContentHash: hex.EncodeToString(contentHash[:]), StorageProvider: uploaded.Provider,
		Bucket: uploaded.Bucket, ObjectKey: audioValueOrDefault(uploaded.ObjectKey, objectKey), URL: uploaded.URL,
		Metadata:  map[string]any{"provider": generation.ProviderAssembly, "demo_manifest": true, "output_type": jobPayloadValue(job.Payload, "output_type")},
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return generation.HandlerResult{}, fmt.Errorf("save assembly manifest: %w", err)
	}
	return generation.HandlerResult{AssetIDs: []string{saved.ID}, Result: map[string]any{"asset_ids": []string{saved.ID}, "provider": generation.ProviderAssembly, "demo_manifest": true}}, nil
}

var _ generation.JobHandler = DemoAssemblyJobHandler{}
