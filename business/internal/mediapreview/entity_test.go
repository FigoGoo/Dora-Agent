package mediapreview

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
)

func mediaPreviewTestUUIDv7(t *testing.T) string {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return id.String()
}

func mediaPreviewTestDigest(value string) Digest { return sha256.Sum256([]byte(value)) }

func TestPrepareCommandValidatesExactToolSourceProfileUnion(t *testing.T) {
	base := PrepareCommand{
		RequestID: mediaPreviewTestUUIDv7(t), CommandID: mediaPreviewTestUUIDv7(t),
		OperationID: mediaPreviewTestUUIDv7(t), RequestDigest: mediaPreviewTestDigest("request"),
		OwnerUserID: mediaPreviewTestUUIDv7(t), ProjectID: mediaPreviewTestUUIDv7(t),
		ToolKey: ToolGenerateMedia, ScopeDigest: mediaPreviewTestDigest("scope"), OutputProfile: OutputProfilePNG,
		PromptSource: &PromptSource{
			ID: mediaPreviewTestUUIDv7(t), Version: 1,
			ContentDigest: mediaPreviewTestDigest("prompt"), TargetLocalKey: "slot_1",
		},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid generate command: %v", err)
	}
	assemble := base
	assemble.ToolKey = ToolAssembleOutput
	assemble.OutputProfile = OutputProfileMP4
	assemble.PromptSource = nil
	assemble.ImageAssetSource = &ImageAssetSource{
		ID: mediaPreviewTestUUIDv7(t), Version: 1, ContentDigest: mediaPreviewTestDigest("png"),
	}
	if err := assemble.Validate(); err != nil {
		t.Fatalf("valid assemble command: %v", err)
	}
	for name, mutate := range map[string]func(*PrepareCommand){
		"both sources":  func(command *PrepareCommand) { command.PromptSource = base.PromptSource },
		"wrong profile": func(command *PrepareCommand) { command.OutputProfile = OutputProfilePNG },
		"unknown tool":  func(command *PrepareCommand) { command.ToolKey = "generate_video" },
		"non image target key": func(command *PrepareCommand) {
			command.ToolKey = ToolGenerateMedia
			command.OutputProfile = OutputProfilePNG
			command.ImageAssetSource = nil
			value := *base.PromptSource
			value.TargetLocalKey = "../slot_1"
			command.PromptSource = &value
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := assemble
			mutate(&candidate)
			if candidate.Validate() == nil {
				t.Fatalf("invalid command accepted: %+v", candidate)
			}
		})
	}
}

func TestObjectKeysAndValidatorRejectTraversalAndAliases(t *testing.T) {
	assetID := mediaPreviewTestUUIDv7(t)
	preparationID := mediaPreviewTestUUIDv7(t)
	staging, final, err := ObjectKeys(assetID, preparationID, ToolGenerateMedia)
	if err != nil || staging != "staging/"+assetID+"/"+preparationID+".png" || final != "objects/"+assetID+"/v1.png" {
		t.Fatalf("ObjectKeys() staging=%q final=%q error=%v", staging, final, err)
	}
	for _, key := range []string{"", "/tmp/file", "staging/../objects/file", `staging\\file`, "staging//file", "staging/./file", "staging/\x00file"} {
		if ValidObjectKey(key) {
			t.Fatalf("unsafe object key accepted: %q", key)
		}
	}
	source := SourceRef{
		SourceType: SourceTypeImageAsset, SourceID: mediaPreviewTestUUIDv7(t), SourceVersion: 1,
		SourceDigest: mediaPreviewTestDigest("source"), SourceObjectKey: "objects/" + assetID + "/v1.png",
	}
	if err := source.Validate(); err != nil {
		t.Fatalf("image_asset source rejected: %v", err)
	}
	source.SourceType = "media_preview_asset"
	if source.Validate() == nil {
		t.Fatal("source_type alias was accepted")
	}
}

func TestFinalizeCommandValidatesReadyAndFailedUnion(t *testing.T) {
	base := FinalizeCommand{
		RequestID: mediaPreviewTestUUIDv7(t), CommandID: mediaPreviewTestUUIDv7(t),
		RequestDigest: mediaPreviewTestDigest("finalize"), PreparationID: mediaPreviewTestUUIDv7(t),
		OperationID: mediaPreviewTestUUIDv7(t), BatchID: mediaPreviewTestUUIDv7(t),
		JobID: mediaPreviewTestUUIDv7(t), AttemptID: mediaPreviewTestUUIDv7(t), Fence: 2,
		TerminalStatus: StatusReady,
		Output: &OutputMetadata{
			ContentDigest: mediaPreviewTestDigest("png"), SizeBytes: 100, MIMEType: MIMEPNG,
			Width: PNGWidth, Height: PNGHeight,
		},
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid ready finalize: %v", err)
	}
	failed := base
	failed.TerminalStatus = StatusFailed
	failed.Output = nil
	failed.ErrorCode = "ARTIFACT_INVALID"
	if err := failed.Validate(); err != nil {
		t.Fatalf("valid failed finalize: %v", err)
	}
	failed.ErrorCode = "arbitrary/path/error"
	if failed.Validate() == nil {
		t.Fatal("unbounded terminal error code accepted")
	}
	base.Output.DurationMS = MP4DurationMS
	if base.Validate() == nil {
		t.Fatal("PNG accepted MP4-only metadata")
	}
}

func TestTerminalErrorCodeMatchesFrozenCrossModuleWhitelist(t *testing.T) {
	for _, code := range []string{
		"FEATURE_DISABLED", "INVALID_ARGUMENT", "NOT_FOUND", "VERSION_CONFLICT", "IDEMPOTENCY_CONFLICT",
		"DEPENDENCY_NOT_READY", "UNSUPPORTED_PROFILE", "LEASE_LOST", "FENCE_STALE", "ARTIFACT_INVALID",
		"FFMPEG_UNAVAILABLE", "EXECUTION_TIMEOUT", "UNKNOWN_OUTCOME", "INTERNAL",
	} {
		if !ValidTerminalErrorCode(code) {
			t.Fatalf("frozen terminal error code rejected: %s", code)
		}
	}
	for _, code := range []string{"", "not_found", "RETRYABLE", "INTERNAL_DETAIL", "NOT_FOUND/path"} {
		if ValidTerminalErrorCode(code) {
			t.Fatalf("unknown terminal error code accepted: %s", code)
		}
	}
}

func TestPreparationAllocationRequiresBusinessGeneratedKeysAndUTC(t *testing.T) {
	command := PrepareCommand{
		RequestID: mediaPreviewTestUUIDv7(t), CommandID: mediaPreviewTestUUIDv7(t), OperationID: mediaPreviewTestUUIDv7(t),
		RequestDigest: mediaPreviewTestDigest("request"), OwnerUserID: mediaPreviewTestUUIDv7(t), ProjectID: mediaPreviewTestUUIDv7(t),
		ToolKey: ToolGenerateMedia, ScopeDigest: mediaPreviewTestDigest("scope"), OutputProfile: OutputProfilePNG,
		PromptSource: &PromptSource{ID: mediaPreviewTestUUIDv7(t), Version: 1, ContentDigest: mediaPreviewTestDigest("prompt"), TargetLocalKey: "slot_2"},
	}
	assetID := mediaPreviewTestUUIDv7(t)
	preparationID := mediaPreviewTestUUIDv7(t)
	staging, final, _ := ObjectKeys(assetID, preparationID, command.ToolKey)
	allocation := PreparationAllocation{
		PreparationID: preparationID, AssetID: assetID, StagingObjectKey: staging, FinalObjectKey: final,
		CreatedAt: time.Now().UTC(),
	}
	if err := allocation.ValidateFor(command); err != nil {
		t.Fatalf("valid allocation: %v", err)
	}
	allocation.FinalObjectKey = "objects/" + mediaPreviewTestUUIDv7(t) + "/v1.png"
	if allocation.ValidateFor(command) == nil {
		t.Fatal("foreign final key accepted")
	}
}
