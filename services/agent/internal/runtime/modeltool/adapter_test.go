package modeltool

import (
	"testing"

	einoruntime "github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/eino"
)

func TestLocalAdapterGenerate(t *testing.T) {
	adapter := LocalAdapter{}
	result, err := adapter.Generate(t.Context(), Snapshot{
		ModelID: "mdl_1", ResourceType: "image", ProviderRuntimeRef: "local:test", TimeoutMS: 1000,
	}, einoruntime.UserPrompt("make image"))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if result.Status != "completed" || result.ArtifactCount != 1 || len(result.Artifacts) != 1 {
		t.Fatalf("unexpected local result: %#v", result)
	}
	if result.Artifacts[0].ElementType != "image_ref" || result.Artifacts[0].Checksum == "" {
		t.Fatalf("unexpected artifact: %#v", result.Artifacts[0])
	}
}

func TestLocalAdapterValidatesRuntimeInput(t *testing.T) {
	adapter := LocalAdapter{}
	_, err := adapter.Generate(t.Context(), Snapshot{ModelID: "mdl_1", ResourceType: "image"}, einoruntime.UserPrompt("make image"))
	if err == nil {
		t.Fatal("expected missing provider_runtime_ref error")
	}
	_, err = adapter.Generate(t.Context(), Snapshot{ModelID: "mdl_1", ResourceType: "image", ProviderRuntimeRef: "local:test"}, nil)
	if err == nil {
		t.Fatal("expected missing prompt error")
	}
}
