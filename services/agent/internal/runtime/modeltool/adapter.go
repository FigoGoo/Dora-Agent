package modeltool

import (
	"context"
	"errors"
	"strings"
	"time"

	einoruntime "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino"
)

type Snapshot struct {
	ModelID            string
	ResourceType       string
	ProviderRuntimeRef string
	TimeoutMS          int32
}

type GenerationResult struct {
	Status        string
	Message       *einoruntime.Message
	ArtifactCount int
}

type Adapter interface {
	Generate(ctx context.Context, snapshot Snapshot, prompt *einoruntime.Message) (GenerationResult, error)
}

type LocalAdapter struct{}

func (LocalAdapter) Generate(ctx context.Context, snapshot Snapshot, prompt *einoruntime.Message) (GenerationResult, error) {
	if strings.TrimSpace(snapshot.ModelID) == "" || strings.TrimSpace(snapshot.ResourceType) == "" {
		return GenerationResult{}, errors.New("model_id and resource_type are required")
	}
	if strings.TrimSpace(snapshot.ProviderRuntimeRef) == "" {
		return GenerationResult{}, errors.New("provider_runtime_ref is required")
	}
	if prompt == nil {
		return GenerationResult{}, errors.New("prompt message is required")
	}
	if snapshot.TimeoutMS > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(snapshot.TimeoutMS)*time.Millisecond)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return GenerationResult{}, err
	}
	return GenerationResult{Status: "deferred_to_m4", Message: prompt, ArtifactCount: 0}, nil
}
