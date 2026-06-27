package modeltool

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
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
	Artifacts     []Artifact
	Partial       bool
}

type Adapter interface {
	Generate(ctx context.Context, snapshot Snapshot, prompt *einoruntime.Message) (GenerationResult, error)
}

type Artifact struct {
	ArtifactID      string
	ResourceType    string
	ElementType     string
	Name            string
	ContentType     string
	SizeBytes       int64
	Checksum        string
	MetadataSummary map[string]string
	ElementsSummary map[string]any
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
	sum := sha256.Sum256([]byte(snapshot.ModelID + ":" + snapshot.ResourceType + ":" + prompt.Content))
	resourceType := strings.TrimSpace(snapshot.ResourceType)
	contentType := "application/octet-stream"
	elementType := "file_ref"
	switch resourceType {
	case "image":
		contentType = "image/png"
		elementType = "image_ref"
	case "audio", "music":
		contentType = "audio/mpeg"
		elementType = "audio_ref"
	case "video":
		contentType = "video/mp4"
		elementType = "video_ref"
	}
	artifact := Artifact{
		ArtifactID:   "art_" + fmt.Sprintf("%x", sum[:8]),
		ResourceType: resourceType,
		ElementType:  elementType,
		Name:         "generated-" + resourceType,
		ContentType:  contentType,
		SizeBytes:    int64(1024 + len(prompt.Content)),
		Checksum:     "sha256:" + fmt.Sprintf("%x", sum[:]),
		MetadataSummary: map[string]string{
			"model_id":        snapshot.ModelID,
			"adapter":         "local",
			"generation_mode": "test_adapter",
		},
		ElementsSummary: map[string]any{
			"count":                1,
			"primary_element_type": elementType,
		},
	}
	return GenerationResult{Status: "completed", Message: prompt, ArtifactCount: 1, Artifacts: []Artifact{artifact}}, nil
}
