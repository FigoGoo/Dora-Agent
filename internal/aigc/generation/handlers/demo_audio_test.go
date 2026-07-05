package handlers

import (
	"context"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

func TestDemoAudioJobHandlerSkipsRealGeneration(t *testing.T) {
	handler := DemoAudioJobHandler{}

	result, err := handler.Handle(context.Background(), generation.GenerationJob{
		ID:             "job-audio-1",
		SessionID:      "s1",
		StoryboardID:   "storyboard-1",
		IdempotencyKey: "idem-audio-1",
		Provider:       generation.ProviderAudio,
		TargetType:     generation.TargetAudioLayer,
		TargetID:       "music-1",
		Status:         generation.StatusRunning,
		Payload: map[string]any{
			"prompt":     "悲凉沉郁的竹林环境音乐",
			"media_kind": "audio",
		},
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(result.AssetIDs) != 0 {
		t.Fatalf("asset ids = %#v", result.AssetIDs)
	}
	if result.Result["skipped_real_generation"] != true || result.Result["demo_placeholder"] != true {
		t.Fatalf("result = %#v", result.Result)
	}
	if result.Result["target_id"] != "music-1" || result.Result["media_kind"] != "audio" {
		t.Fatalf("result target metadata = %#v", result.Result)
	}
}
