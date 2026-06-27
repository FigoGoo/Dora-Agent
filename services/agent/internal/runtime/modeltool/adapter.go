package modeltool

import (
	"context"

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
	_ = ctx
	return GenerationResult{Status: "deferred_to_m4", Message: prompt, ArtifactCount: 0}, nil
}
