package capabilityruntime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/artifact"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/generation"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

// Storyboards/Specs 全部为空（未 seed）：deliverable 路径触碰任何
// storyboard/spec 闸门都会报 not found——本测试是免闸门的物理证明。
func newDeliverableTestRuntime(t *testing.T, store *generation.MemoryStore) *Runtime {
	t.Helper()
	repository := storyboard.NewMemoryAggregateRepository()
	commands, err := storyboard.NewCommandService(repository)
	if err != nil {
		t.Fatal(err)
	}
	idCounter := 0
	runtime, err := New(Config{
		Model: scriptedModel{}, Artifacts: artifact.NewMemoryStore(),
		Specs:      &memorySpecs{},
		Storyboards: repository, StoryboardCommands: commands,
		GenerationCommands: generation.NewCommandService(generation.CommandServiceConfig{Store: store}),
		GenerationWorkflow: store,
		GenerationPreflight: func(context.Context, string, []generation.GenerationJob) (int64, error) {
			return 0, nil
		},
		NewID: func() string { idCounter++; return fmt.Sprintf("id-%d", idCounter) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestGenerateMediaSessionDeliverableSkipsStoryboardGates(t *testing.T) {
	ctx := context.Background()
	store := generation.NewMemoryStore()
	runtime := newDeliverableTestRuntime(t, store)
	command := capability.CommandContext{SessionID: "session-d", UserID: "user-d", RequestID: "request-d", IdempotencyKey: "idem-d"}
	request := capability.Request[capability.GenerateMediaIntent]{Command: command, Intent: capability.GenerateMediaIntent{
		Target: capability.MediaTargetSessionDeliverable, MediaKind: "image",
		Prompt: "雨中柴犬", Count: 2, AspectRatio: "1:1",
	}}

	result, err := runtime.Handlers().GenerateMedia(ctx, request)
	if err != nil {
		t.Fatalf("deliverable generate_media must skip storyboard gates: %v", err)
	}
	if result.Status != capability.StatusAccepted || result.Data.JobCount != 2 {
		t.Fatalf("result = %+v", result)
	}

	batch, err := store.GetBatch(ctx, result.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	if batch.WakePolicy != generation.WakeOnTerminal {
		t.Fatalf("deliverable batch must wake on terminal, got %s", batch.WakePolicy)
	}
	wantPolicy := generation.DeliveryPolicy{BindingMode: generation.BindingModeActive, ApprovalPolicy: generation.ApprovalAutoApprove, ChargePolicy: generation.ChargePostpaidNoReservation}
	if batch.DeliveryPolicy != wantPolicy {
		t.Fatalf("batch policy = %+v", batch.DeliveryPolicy)
	}

	fingerprints := map[string]struct{}{}
	for _, target := range result.Data.SelectedTargets {
		if !strings.HasPrefix(target, "deliverable:") || !strings.HasSuffix(target, ":primary") {
			t.Fatalf("selected target = %s", target)
		}
	}
	jobs, err := store.ListBySession(ctx, "session-d")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("job count = %d", len(jobs))
	}
	for _, job := range jobs {
		if job.Provider != generation.ProviderImage2 || job.MediaKind != "image" {
			t.Fatalf("job provider/kind = %s/%s", job.Provider, job.MediaKind)
		}
		token := job.BindingToken
		if token.NormalizedKind() != generation.TargetKindSessionDeliverable || token.StoryboardID != "" ||
			!strings.HasPrefix(token.TargetID, "deliverable:") || token.AssetSlot != "primary" {
			t.Fatalf("token = %+v", token)
		}
		if job.DeliveryPolicy != wantPolicy {
			t.Fatalf("job policy = %+v", job.DeliveryPolicy)
		}
		if job.Payload["prompt"] != "雨中柴犬" || job.Payload["ratio"] != "1:1" {
			t.Fatalf("payload = %v", job.Payload)
		}
		fingerprints[token.InputFingerprint] = struct{}{}
	}
	if len(fingerprints) != 2 {
		t.Fatalf("fingerprints must differ per variant, got %v", fingerprints)
	}

	replay, err := runtime.Handlers().GenerateMedia(ctx, request)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.OperationID != result.OperationID || replay.BatchID != result.BatchID {
		t.Fatalf("replay must be idempotent: first=%s/%s second=%s/%s", result.OperationID, result.BatchID, replay.OperationID, replay.BatchID)
	}
}

func TestProviderForCoversAllDeliverableKinds(t *testing.T) {
	for _, kind := range []string{"image", "video", "music", "audio"} {
		if providerFor(kind) == "" {
			t.Fatalf("providerFor(%s) must resolve", kind)
		}
	}
}
