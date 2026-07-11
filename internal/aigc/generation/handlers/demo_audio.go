package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/asset"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

type DemoAudioAssetStore interface {
	Save(context.Context, asset.Asset) (asset.Asset, error)
}

type DemoAudioJobHandlerConfig struct {
	Assets   DemoAudioAssetStore
	Uploader asset.Uploader
	Now      func() time.Time
}

type DemoAudioJobHandler struct{ cfg DemoAudioJobHandlerConfig }

func NewDemoAudioJobHandler(config DemoAudioJobHandlerConfig) DemoAudioJobHandler {
	if config.Now == nil {
		config.Now = time.Now
	}
	return DemoAudioJobHandler{cfg: config}
}

func (h DemoAudioJobHandler) Handle(ctx context.Context, job generation.GenerationJob) (generation.HandlerResult, error) {
	if h.cfg.Assets != nil && h.cfg.Uploader != nil {
		return h.persistDemoTone(ctx, job)
	}
	return generation.HandlerResult{
		Result: map[string]any{
			"demo_placeholder":        true,
			"skipped_real_generation": true,
			"reason":                  "demo scope does not include real audio generation",
			"provider":                generation.ProviderAudio,
			"target_type":             strings.TrimSpace(job.TargetType),
			"target_id":               strings.TrimSpace(job.TargetID),
			"media_kind":              jobPayloadValue(job.Payload, "media_kind"),
			"prompt":                  jobPayloadValue(job.Payload, "prompt"),
		},
	}, nil
}

// persistDemoTone creates a deterministic two-second WAV preview. It keeps the
// demo pipeline complete for music/audio storyboards while making the
// placeholder nature explicit in asset metadata; a production audio provider
// can replace this adapter without changing the workflow contracts.
func (h DemoAudioJobHandler) persistDemoTone(ctx context.Context, job generation.GenerationJob) (generation.HandlerResult, error) {
	raw := demoToneWAV(2*time.Second, 440)
	sum := sha256.Sum256([]byte(job.ID))
	assetID := "audio_" + hex.EncodeToString(sum[:12])
	filename := assetID + ".wav"
	objectKey := asset.NewObjectKey(job.SessionID, assetID, filename)
	uploaded, err := h.cfg.Uploader.Upload(ctx, asset.UploadInput{ObjectKey: objectKey, Content: bytes.NewReader(raw), ContentLength: int64(len(raw)), MIMEType: "audio/wav", Filename: filename, Metadata: map[string]string{"provider": "demo_audio", "source_job_id": job.ID}})
	if err != nil {
		return generation.HandlerResult{}, fmt.Errorf("upload demo audio asset: %w", err)
	}
	now := h.cfg.Now()
	contentHash := sha256.Sum256(raw)
	saved, err := h.cfg.Assets.Save(ctx, asset.Asset{
		ID: assetID, SessionID: job.SessionID, UserID: job.UserID, SourceJobID: job.ID, OutputIndex: 0,
		Kind: asset.KindAudio, Source: asset.SourceGenerated, MIMEType: "audio/wav", Filename: filename,
		SizeBytes: int64(len(raw)), ContentHash: hex.EncodeToString(contentHash[:]), StorageProvider: uploaded.Provider,
		Bucket: uploaded.Bucket, ObjectKey: audioValueOrDefault(uploaded.ObjectKey, objectKey), URL: uploaded.URL,
		Metadata:  map[string]any{"provider": "demo_audio", "demo_placeholder": true, "prompt": jobPayloadValue(job.Payload, "prompt"), "frequency_hz": 440},
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return generation.HandlerResult{}, fmt.Errorf("save demo audio asset: %w", err)
	}
	return generation.HandlerResult{AssetIDs: []string{saved.ID}, Result: map[string]any{"asset_ids": []string{saved.ID}, "provider": generation.ProviderAudio, "demo_placeholder": true}}, nil
}

func demoToneWAV(duration time.Duration, frequency float64) []byte {
	const sampleRate = 22050
	const channels = 1
	const bitsPerSample = 16
	samples := int(float64(sampleRate) * duration.Seconds())
	dataSize := samples * channels * bitsPerSample / 8
	buffer := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	buffer.WriteString("RIFF")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(36+dataSize))
	buffer.WriteString("WAVEfmt ")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(16))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(1))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(channels))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(sampleRate*channels*bitsPerSample/8))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(channels*bitsPerSample/8))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(bitsPerSample))
	buffer.WriteString("data")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(dataSize))
	for i := 0; i < samples; i++ {
		// Low amplitude keeps the placeholder unobtrusive while still producing
		// a valid, visibly non-empty waveform in the demo UI.
		value := int16(math.Sin(2*math.Pi*frequency*float64(i)/sampleRate) * 1800)
		_ = binary.Write(buffer, binary.LittleEndian, value)
	}
	return buffer.Bytes()
}

func audioValueOrDefault(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func jobPayloadValue(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

var _ generation.JobHandler = DemoAudioJobHandler{}
