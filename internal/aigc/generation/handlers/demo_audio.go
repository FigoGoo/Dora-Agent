package handlers

import (
	"context"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
)

type DemoAudioJobHandler struct{}

func (DemoAudioJobHandler) Handle(_ context.Context, job generation.GenerationJob) (generation.HandlerResult, error) {
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

func jobPayloadValue(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

var _ generation.JobHandler = DemoAudioJobHandler{}
