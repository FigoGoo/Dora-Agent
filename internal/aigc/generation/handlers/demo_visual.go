package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

type DemoVisualJobHandlerConfig struct {
	Assets   DemoAudioAssetStore
	Uploader asset.Uploader
	Now      func() time.Time
}

// DemoVisualJobHandler persists deterministic local placeholder image/video
// assets. It exists only to make the trusted local workflow executable without
// Image2 or Seedance credentials; production keys replace it in main wiring.
type DemoVisualJobHandler struct{ cfg DemoVisualJobHandlerConfig }

func NewDemoVisualJobHandler(config DemoVisualJobHandlerConfig) DemoVisualJobHandler {
	if config.Now == nil {
		config.Now = time.Now
	}
	return DemoVisualJobHandler{cfg: config}
}

func (h DemoVisualJobHandler) Handle(ctx context.Context, job generation.GenerationJob) (generation.HandlerResult, error) {
	if h.cfg.Assets == nil || h.cfg.Uploader == nil {
		return generation.HandlerResult{}, fmt.Errorf("demo visual asset store and uploader are required")
	}
	raw, kind, mimeType, extension, err := demoVisualBytes(job)
	if err != nil {
		return generation.HandlerResult{}, err
	}
	sum := sha256.Sum256([]byte(job.ID + "\x00" + kind))
	assetID := kind + "_" + hex.EncodeToString(sum[:12])
	filename := assetID + extension
	objectKey := asset.NewObjectKey(job.SessionID, assetID, filename)
	uploaded, err := h.cfg.Uploader.Upload(ctx, asset.UploadInput{
		ObjectKey: objectKey, Content: bytes.NewReader(raw), ContentLength: int64(len(raw)), MIMEType: mimeType, Filename: filename,
		Metadata: map[string]string{"provider": "local_demo_" + kind, "source_job_id": job.ID},
	})
	if err != nil {
		return generation.HandlerResult{}, fmt.Errorf("upload demo %s asset: %w", kind, err)
	}
	now := h.cfg.Now()
	contentHash := sha256.Sum256(raw)
	saved, err := h.cfg.Assets.Save(ctx, asset.Asset{
		ID: assetID, SessionID: job.SessionID, UserID: job.UserID, SourceJobID: job.ID, OutputIndex: 0,
		Kind: kind, Source: asset.SourceGenerated, MIMEType: mimeType, Filename: filename,
		SizeBytes: int64(len(raw)), ContentHash: hex.EncodeToString(contentHash[:]), StorageProvider: uploaded.Provider,
		Bucket: uploaded.Bucket, ObjectKey: audioValueOrDefault(uploaded.ObjectKey, objectKey), URL: uploaded.URL,
		Metadata: map[string]any{
			"provider": job.Provider, "demo_placeholder": true, "prompt": jobPayloadValue(job.Payload, "prompt"),
		},
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return generation.HandlerResult{}, fmt.Errorf("save demo %s asset: %w", kind, err)
	}
	return generation.HandlerResult{
		AssetIDs: []string{saved.ID},
		Result: map[string]any{
			"asset_ids": []string{saved.ID}, "provider": job.Provider, "demo_placeholder": true, "media_kind": kind,
		},
	}, nil
}

func demoVisualBytes(job generation.GenerationJob) ([]byte, string, string, string, error) {
	mediaKind := strings.ToLower(strings.TrimSpace(job.MediaKind))
	if mediaKind == "" {
		mediaKind = strings.ToLower(jobPayloadValue(job.Payload, "media_kind"))
	}
	if mediaKind == asset.KindVideo || job.Provider == generation.ProviderSeedance {
		raw, err := base64.StdEncoding.DecodeString(demoMP4Base64)
		return raw, asset.KindVideo, "video/mp4", ".mp4", err
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: uint8(40 + x*2), G: uint8(60 + y*2), B: 180, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		return nil, "", "", "", fmt.Errorf("encode demo image: %w", err)
	}
	return buffer.Bytes(), asset.KindImage, "image/png", ".png", nil
}

const demoMP4Base64 = "AAAAIGZ0eXBpc29tAAACAGlzb21pc28yYXZjMW1wNDEAAANebW9vdgAAAGxtdmhkAAAAAAAAAAAAAAAAAAAD6AAAAlgAAQAAAQAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAgAAAoh0cmFrAAAAXHRraGQAAAADAAAAAAAAAAAAAAABAAAAAAAAAlgAAAAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAACAAAAAgAAAAAAAkZWR0cwAAABxlbHN0AAAAAAAAAAEAAAJYAAAQAAABAAAAAAIAbWRpYQAAACBtZGhkAAAAAAAAAAAAAAAAAAAoAAAAIABVxAAAAAAALWhkbHIAAAAAAAAAAHZpZGUAAAAAAAAAAAAAAABWaWRlb0hhbmRsZXIAAAABq21pbmYAAAAUdm1oZAAAAAEAAAAAAAAAAAAAACRkaW5mAAAAHGRyZWYAAAAAAAAAAQAAAAx1cmwgAAAAAQAAAWtzdGJsAAAAv3N0c2QAAAAAAAAAAQAAAK9hdmMxAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAACAAIABIAAAASAAAAAAAAAABFUxhdmM2Mi4yOC4xMDAgbGlieDI2NAAAAAAAAAAAAAAAGP//AAAANWF2Y0MBZAAK/+EAGGdkAAqs2UlsBEAAAAMAQAAAAwKDxIllgAEABmjr48siwP34+AAAAAAQcGFzcAAAAAEAAAABAAAAFGJ0cnQAAAAAAAAmwAAAAAAAAAAYc3R0cwAAAAAAAAABAAAAAwAACAAAAAAUc3RzcwAAAAAAAAABAAAAAQAAAChjdHRzAAAAAAAAAAMAAAABAAAQAAAAAAEAABgAAAAAAQAACAAAAAAcc3RzYwAAAAAAAAABAAAAAQAAAAMAAAABAAAAIHN0c3oAAAAAAAAAAAAAAAMAAALPAAAADQAAAAwAAAAUc3RjbwAAAAAAAAABAAADjgAAAGJ1ZHRhAAAAWm1ldGEAAAAAAAAAIWhkbHIAAAAAAAAAAG1kaXJhcHBsAAAAAAAAAAAAAAAALWlsc3QAAAAlqXRvbwAAAB1kYXRhAAAAAQAAAABMYXZmNjIuMTIuMTAwAAAACGZyZWUAAALwbWRhdAAAAq0GBf//qdxF6b3m2Ui3lizYINkj7u94MjY0IC0gY29yZSAxNjUgcjMyMjIgYjM1NjA1YSAtIEguMjY0L01QRUctNCBBVkMgY29kZWMgLSBDb3B5bGVmdCAyMDAzLTIwMjUgLSBodHRwOi8vd3d3LnZpZGVvbGFuLm9yZy94MjY0Lmh0bWwgLSBvcHRpb25zOiBjYWJhYz0xIHJlZj0zIGRlYmxvY2s9MTowOjAgYW5hbHlzZT0weDM6MHgxMTMgbWU9aGV4IHN1Ym1lPTcgcHN5PTEgcHN5X3JkPTEuMDA6MC4wMiBtaXhlZF9yZWY9MSBtZV9yYW5nZT0xNiBjaHJvbWFfbWU9MSB0cmVsbGlzPTEgOHg4ZGN0PTEgY3FtPTAgZGVhZHpvbmU9MjEsMTEgZmFzdF9wc2tpcD0xIGNocm9tYV9xcF9vZmZzZXQ9LTIgdGhyZWFkcz0xIGxvb2thaGVhZF90aHJlYWRzPTEgc2xpY2VkX3RocmVhZHM9MCBucj0wIGRlY2ltYXRlPTEgaW50ZXJsYWNlZD0wIGJmcmFtZXM9MyBiX3B5cmFtaWQ9MiBiX2FkYXB0PTEgYl9iaWFzPTAgZGlyZWN0PTEgd2VpZ2h0Yj0xIG9wZW5fZ29wPTAgd2VpZ2h0cD0yIGtleWludD0yNTAga2V5aW50X21pbj01IHNjZW5lY3V0PTQwIGludHJhX3JlZnJlc2g9MCByY19sb29rYWhlYWQ9NDAgcmM9Y3JmIG1idHJlZT0xIGNyZj0yMy4wIHFjb21wPTAuNjAgcXBtaW49MCBxcG1heD02OSBxcHN0ZXA9NCBpcF9yYXRpbz0xLjQwIGFxPTE6MS4wMACAAAAAGmWIhAAR//7n4/wKaPH3E3kFjkY1tds+/BvhAAAACUGaImxD//6rgAAAAAgBnkF5D/9EQQ=="

var _ generation.JobHandler = DemoVisualJobHandler{}
