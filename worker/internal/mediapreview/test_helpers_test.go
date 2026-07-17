package mediapreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestObjectRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("chmod test object root: %v", err)
	}
	for _, directory := range []string{"staging", "staging/objects", "objects"} {
		if err := os.Mkdir(filepath.Join(root, filepath.FromSlash(directory)), 0o700); err != nil {
			t.Fatalf("create test object directory %s: %v", directory, err)
		}
	}
	return root
}

func mustUUIDv7(t *testing.T) uuid.UUID {
	t.Helper()
	value, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("create UUIDv7: %v", err)
	}
	return value
}

func validGenerateEnvelope(t *testing.T, objectKey string) MediaJobEnvelopeV1 {
	t.Helper()
	createdAt := time.Now().UTC()
	return MediaJobEnvelopeV1{
		SchemaVersion:         EnvelopeSchemaVersionV1,
		JobID:                 mustUUIDv7(t),
		BatchID:               mustUUIDv7(t),
		OperationID:           mustUUIDv7(t),
		SessionID:             mustUUIDv7(t),
		UserID:                mustUUIDv7(t),
		ProjectID:             mustUUIDv7(t),
		JobType:               JobTypeGeneratePNG,
		DefinitionVersion:     DefinitionVersionGenerateMediaV3Preview1,
		ScopeDigest:           strings.Repeat("1", 64),
		OutputProfile:         OutputProfilePNG640x360V1,
		ArtifactRequestDigest: strings.Repeat("4", 64),
		AttemptID:             mustUUIDv7(t),
		Fence:                 1,
		LeaseExpiresAt:        createdAt.Add(20 * time.Second),
		CreatedAt:             createdAt,
		DeadlineAt:            createdAt.Add(30 * time.Second),
		SourceRef: SourceRefV1{
			SourceType:     string(SourceTypePromptPreview),
			SourceID:       mustUUIDv7(t),
			SourceVersion:  1,
			SourceDigest:   strings.Repeat("2", 64),
			TargetLocalKey: "hero_image",
			TargetDigest:   strings.Repeat("3", 64),
		},
		Target: TargetV1{
			AssetID:          mustUUIDv7(t),
			AssetVersion:     1,
			PreparationID:    mustUUIDv7(t),
			StagingObjectKey: objectKey,
		},
	}
}

func validAssembleEnvelope(t *testing.T, sourceKey string, targetKey string) MediaJobEnvelopeV1 {
	t.Helper()
	envelope := validGenerateEnvelope(t, targetKey)
	envelope.JobType = JobTypeAssembleMP4
	envelope.DefinitionVersion = DefinitionVersionAssembleOutputV3Preview1
	envelope.OutputProfile = OutputProfileMP4H264640x3602sV1
	envelope.SourceRef.SourceType = string(SourceTypeImageAsset)
	envelope.SourceRef.TargetLocalKey = ""
	envelope.SourceRef.TargetDigest = ""
	envelope.SourceRef.SourceObjectKey = sourceKey
	envelope.ArtifactRequestDigest = strings.Repeat("5", 64)
	return envelope
}

func newPNGOnlyEngine(t *testing.T, root string) *Engine {
	t.Helper()
	engine, err := NewEngine(t.Context(), Config{
		Profile:          RuntimeProfileMediaV3Preview1,
		ObjectRoot:       root,
		GeneratorVersion: GeneratorVersionPNG640x360V1,
	})
	if err != nil {
		t.Fatalf("create PNG engine: %v", err)
	}
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Errorf("close PNG engine: %v", err)
		}
	})
	return engine
}
