package storyboard

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDynamicStoryboardPendingPromptRegenerateAndBindingLifecycle(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryAggregateRepository()
	service, err := NewCommandService(repository)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err := service.Create(ctx, "board-1", "session-1")
	if err != nil {
		t.Fatal(err)
	}
	candidate := StoryboardRevision{ID: "revision-1", Modules: []StoryboardModule{{
		ID: "module-1", Key: "characters", SemanticType: "character", Title: "角色", PlannedCount: 1,
		Elements: []StoryboardElement{{ID: "hero", Key: "hero", SemanticType: "character", Title: "主角", Revision: 1,
			PromptSlots: []PromptSlot{{Purpose: "portrait", Prompt: "old", Revision: 1, Status: PromptStatusReady}},
			AssetSlots:  []AssetSlot{{Key: "portrait", MediaKind: "image", Required: true, ReviewRequired: true, Status: AssetSlotStatusMissing}},
		}},
	}}}
	aggregate, _, err = service.CreatePending(ctx, CreatePendingRevisionCommand{CommandID: "plan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = service.UpdatePrompt(ctx, UpdatePromptCommand{CommandID: "prompt", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: "hero", Purpose: "portrait", ExpectedRevision: 1, Prompt: "user locked", LockedByUser: true})
	if err != nil {
		t.Fatalf("edit pending prompt: %v", err)
	}
	pending, _ := aggregate.PendingRevision()
	if got := pending.Modules[0].Elements[0].PromptSlots[0]; got.Prompt != "user locked" || got.Revision != 2 || !got.LockedByUser {
		t.Fatalf("prompt=%+v", got)
	}
	aggregate, _, err = service.DecidePending(ctx, DecidePendingRevisionCommand{CommandID: "approve", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: pending.ID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, epoch, err := service.Regenerate(ctx, RegenerateAssetCommand{CommandID: "regen", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: "hero", AssetSlot: "portrait"})
	if err != nil || epoch != 1 {
		t.Fatalf("epoch=%d err=%v", epoch, err)
	}
	input, err := aggregate.ResolveGenerationInput("hero", "portrait")
	if err != nil {
		t.Fatal(err)
	}
	aggregate, disposition, err := service.Bind(ctx, BindAssetCommand{CommandID: "bind", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, BindingID: "binding-1", TargetID: "hero", AssetSlot: "portrait", AssetID: "asset-1", TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision, GenerationEpoch: input.GenerationEpoch, InputFingerprint: input.Fingerprint})
	if err != nil || disposition != BindingDispositionCandidate {
		t.Fatalf("disposition=%s err=%v", disposition, err)
	}
	if _, _, duplicateErr := service.Bind(ctx, BindAssetCommand{CommandID: "different-command", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, BindingID: "binding-1", TargetID: "hero", AssetSlot: "portrait", AssetID: "asset-1", TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision, GenerationEpoch: input.GenerationEpoch, InputFingerprint: input.Fingerprint}); !errors.Is(duplicateErr, ErrInvalidMutation) {
		t.Fatalf("duplicate binding id error = %v", duplicateErr)
	}
	aggregate, stale, err := service.Activate(ctx, ActivateBindingCommand{CommandID: "activate", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, BindingID: "binding-1"})
	if err != nil || len(stale) != 0 {
		t.Fatalf("stale=%v err=%v", stale, err)
	}
	active, _ := aggregate.ActiveRevision()
	slot := active.Modules[0].Elements[0].AssetSlots[0]
	if slot.ActiveBindingID != "binding-1" || slot.Status != AssetSlotStatusActive {
		t.Fatalf("slot=%+v", slot)
	}
}

func TestBindUsesResolveGenerationInputSinglePromptFallback(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryAggregateRepository()
	service, err := NewCommandService(repository)
	if err != nil {
		t.Fatal(err)
	}
	aggregate, err := service.Create(ctx, "board-natural-purpose", "session-natural-purpose")
	if err != nil {
		t.Fatal(err)
	}
	candidate := StoryboardRevision{ID: "revision-natural-purpose", Modules: []StoryboardModule{{
		ID: "audio", Key: "audio", SemanticType: "audio_layer", Title: "音频", PlannedCount: 1, Required: true,
		Elements: []StoryboardElement{{
			ID: "ambient", Key: "ambient", SemanticType: "audio_layer", Title: "环境音", Revision: 1,
			PromptSlots: []PromptSlot{{Purpose: "描述雨声与城市氛围", Prompt: "rain ambience", Revision: 3, Status: PromptStatusReady}},
			AssetSlots:  []AssetSlot{{Key: "asset_ambient_audio", MediaKind: "audio", Required: true, ReviewRequired: true, Status: AssetSlotStatusMissing}},
		}},
	}}}
	aggregate, _, err = service.CreatePending(ctx, CreatePendingRevisionCommand{CommandID: "plan-natural-purpose", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := aggregate.PendingRevision()
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = service.DecidePending(ctx, DecidePendingRevisionCommand{CommandID: "approve-natural-purpose", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: pending.ID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	input, err := aggregate.ResolveGenerationInput("ambient", "asset_ambient_audio")
	if err != nil {
		t.Fatal(err)
	}
	if input.Prompt != "rain ambience" || input.PromptRevision != 3 {
		t.Fatalf("resolved input = %+v", input)
	}
	_, disposition, err := service.Bind(ctx, BindAssetCommand{
		CommandID: "bind-natural-purpose", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version,
		BindingID: "binding-natural-purpose", TargetID: "ambient", AssetSlot: "asset_ambient_audio", AssetID: "asset-audio",
		TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision,
		GenerationEpoch: input.GenerationEpoch, InputFingerprint: input.Fingerprint,
	})
	if err != nil || disposition != BindingDispositionCandidate {
		t.Fatalf("bind disposition=%s err=%v", disposition, err)
	}
}

func TestStoryboardCommandIdempotencyRejectsChangedPayload(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryAggregateRepository()
	service, _ := NewCommandService(repository)
	aggregate, _ := service.Create(ctx, "board-idempotency", "session-idempotency")
	candidate := StoryboardRevision{ID: "revision-idempotency", Modules: []StoryboardModule{{
		ID: "module", Key: "scenes", SemanticType: "scene", Title: "场景", PlannedCount: 1,
		Elements: []StoryboardElement{{ID: "scene", Key: "scene", SemanticType: "scene", Title: "开场", Revision: 1,
			PromptSlots: []PromptSlot{{Purpose: "image", Prompt: "first", Revision: 1, Status: PromptStatusReady}},
		}},
	}}}
	aggregate, _, err := service.CreatePending(ctx, CreatePendingRevisionCommand{CommandID: "plan-idempotency", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	command := UpdatePromptCommand{CommandID: "edit-idempotency", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: "scene", Purpose: "image", ExpectedRevision: 1, Prompt: "second", LockedByUser: true}
	updated, _, err := service.UpdatePrompt(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	replayed, _, err := service.UpdatePrompt(ctx, command)
	if err != nil || replayed.Version != updated.Version {
		t.Fatalf("exact replay version=%d want=%d err=%v", replayed.Version, updated.Version, err)
	}

	changed := command
	changed.Prompt = "different payload"
	if _, _, err := service.UpdatePrompt(ctx, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed replay error = %v", err)
	}
	persisted, err := repository.GetAggregate(ctx, aggregate.ID)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := persisted.PendingRevision()
	if err != nil {
		t.Fatal(err)
	}
	if got := pending.Modules[0].Elements[0].PromptSlots[0].Prompt; got != "second" {
		t.Fatalf("changed replay mutated prompt to %q", got)
	}
}

func TestValidateRevisionRejectsInvalidDependencyGraph(t *testing.T) {
	revision := dependencyTestRevision()
	revision.Dependencies = []DependencyEdge{{FromTargetID: "source", FromSlot: "image", ToTargetID: "output", ToSlot: "video"}, {FromTargetID: "output", FromSlot: "video", ToTargetID: "source", ToSlot: "image"}}
	if err := ValidateRevision(revision); err == nil || !strings.Contains(err.Error(), "DAG") {
		t.Fatalf("cycle validation error = %v", err)
	}
	revision.Dependencies = []DependencyEdge{{FromTargetID: "source", FromSlot: "missing", ToTargetID: "output", ToSlot: "video"}}
	if err := ValidateRevision(revision); err == nil || !strings.Contains(err.Error(), "unknown source slot") {
		t.Fatalf("slot validation error = %v", err)
	}
}

func TestValidateRevisionRejectsUnsupportedAssetSlotMediaKind(t *testing.T) {
	revision := dependencyTestRevision()
	revision.Modules[0].Elements[0].AssetSlots[0].MediaKind = "visual"
	if err := ValidateRevision(revision); err == nil || !strings.Contains(err.Error(), "unsupported media_kind") {
		t.Fatalf("unsupported media kind validation error = %v", err)
	}
}

func TestCandidateCannotActivateAfterUpstreamAssetChanges(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryAggregateRepository()
	service, _ := NewCommandService(repository)
	aggregate, _ := service.Create(ctx, "board-dependencies", "session-dependencies")
	candidate := dependencyTestRevision()
	aggregate, _, err := service.CreatePending(ctx, CreatePendingRevisionCommand{CommandID: "plan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = service.DecidePending(ctx, DecidePendingRevisionCommand{CommandID: "approve", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}

	aggregate = bindAndActivate(t, ctx, service, aggregate, "source", "image", "asset-source-v1", "source-v1")
	downstreamInput, err := aggregate.ResolveGenerationInput("output", "video")
	if err != nil {
		t.Fatal(err)
	}
	aggregate, disposition, err := service.Bind(ctx, BindAssetCommand{CommandID: "output-candidate", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, BindingID: "binding-output-v1", TargetID: "output", AssetSlot: "video", AssetID: "asset-output-v1", TargetRevision: downstreamInput.TargetRevision, PromptRevision: downstreamInput.PromptRevision, GenerationEpoch: downstreamInput.GenerationEpoch, InputFingerprint: downstreamInput.Fingerprint})
	if err != nil || disposition != BindingDispositionCandidate {
		t.Fatalf("downstream bind disposition=%s err=%v", disposition, err)
	}

	aggregate, _, err = service.Regenerate(ctx, RegenerateAssetCommand{CommandID: "source-regenerate", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, TargetID: "source", AssetSlot: "image"})
	if err != nil {
		t.Fatal(err)
	}
	aggregate = bindAndActivate(t, ctx, service, aggregate, "source", "image", "asset-source-v2", "source-v2")
	_, _, err = service.Activate(ctx, ActivateBindingCommand{CommandID: "activate-stale-output", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, BindingID: "binding-output-v1"})
	if !errors.Is(err, ErrInvalidMutation) {
		t.Fatalf("activate stale downstream candidate error = %v", err)
	}
	latest, loadErr := repository.GetAggregate(ctx, aggregate.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	for _, binding := range latest.Bindings {
		if binding.ID == "binding-output-v1" && binding.State != BindingStateSuperseded {
			t.Fatalf("stale candidate state = %s", binding.State)
		}
	}
}

func TestAutoActivatedUpstreamReplacementMarksDependencyClosureStale(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryAggregateRepository()
	service, _ := NewCommandService(repository)
	aggregate, _ := service.Create(ctx, "board-auto-dependencies", "session-auto-dependencies")
	candidate := dependencyTestRevision()
	candidate.ID = "revision-auto-dependencies"
	aggregate, _, err := service.CreatePending(ctx, CreatePendingRevisionCommand{CommandID: "plan-auto", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = service.DecidePending(ctx, DecidePendingRevisionCommand{CommandID: "approve-auto", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an explicit trusted policy command enabling auto-activation for
	// this upstream slot. LLM-planned slots are review-required by default.
	beforePolicy := aggregate.Version
	active, _ := aggregate.mutableActiveRevision()
	source, _ := findElement(active, "source")
	findAssetSlot(source, "image").ReviewRequired = false
	aggregate.touch("trusted-auto-policy")
	if err := repository.SaveAggregate(ctx, aggregate, beforePolicy, DomainEvent{CommandID: "trusted-auto-policy", Type: "storyboard.delivery_policy_updated"}); err != nil {
		t.Fatal(err)
	}

	autoBind := func(commandID, assetID string) {
		input, resolveErr := aggregate.ResolveGenerationInput("source", "image")
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		aggregate, _, err = service.Bind(ctx, BindAssetCommand{
			CommandID: commandID, StoryboardID: aggregate.ID, BaseVersion: aggregate.Version,
			BindingID: commandID + ":binding", TargetID: "source", AssetSlot: "image", AssetID: assetID,
			TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision,
			GenerationEpoch: input.GenerationEpoch, InputFingerprint: input.Fingerprint, Activate: true,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	autoBind("source-v1", "asset-source-v1")
	aggregate = bindAndActivate(t, ctx, service, aggregate, "output", "video", "asset-output-v1", "output-v1")
	active, _ = aggregate.ActiveRevision()
	output, _ := findElement(active, "output")
	if slot := findAssetSlot(output, "video"); slot == nil || slot.Status != AssetSlotStatusActive {
		t.Fatalf("downstream slot was not active before replacement: %+v", slot)
	}

	autoBind("source-v2", "asset-source-v2")
	active, _ = aggregate.ActiveRevision()
	output, _ = findElement(active, "output")
	if slot := findAssetSlot(output, "video"); slot == nil || slot.Status != AssetSlotStatusStale {
		t.Fatalf("downstream slot did not become stale after auto activation: %+v", slot)
	}
}

func TestPromoteRebasesApprovedAssetActivatedDuringPlanReview(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryAggregateRepository()
	service, _ := NewCommandService(repository)
	aggregate, _ := service.Create(ctx, "board-review-rebase", "session-review-rebase")
	candidate := StoryboardRevision{ID: "revision-active", Modules: []StoryboardModule{{
		ID: "module", Key: "scenes", SemanticType: "scene", Title: "场景", PlannedCount: 1,
		Elements: []StoryboardElement{{ID: "scene", Key: "scene", SemanticType: "scene", Title: "开场", Revision: 1,
			PromptSlots: []PromptSlot{{Purpose: "image", Prompt: "city", Revision: 1, Status: PromptStatusReady}},
			AssetSlots:  []AssetSlot{{Key: "image", MediaKind: "image", Required: true, ReviewRequired: true, Status: AssetSlotStatusMissing}},
		}},
	}}}
	aggregate, _, err := service.CreatePending(ctx, CreatePendingRevisionCommand{CommandID: "initial-plan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = service.DecidePending(ctx, DecidePendingRevisionCommand{CommandID: "initial-approve", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}

	replan := candidate
	replan.ID = "revision-pending"
	aggregate, _, err = service.CreatePending(ctx, CreatePendingRevisionCommand{CommandID: "replan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: replan, PreserveApprovedAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	aggregate = bindAndActivate(t, ctx, service, aggregate, "scene", "image", "asset-during-review", "during-review")
	pendingID := aggregate.PendingRevisionID
	aggregate, _, err = service.DecidePending(ctx, DecidePendingRevisionCommand{CommandID: "approve-replan", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: pendingID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	active, err := aggregate.ActiveRevision()
	if err != nil {
		t.Fatal(err)
	}
	element, ok := findElement(active, "scene")
	if !ok {
		t.Fatal("scene missing after promotion")
	}
	slot := findAssetSlot(element, "image")
	if slot == nil || slot.ActiveBindingID != "during-review:binding" || slot.Status != AssetSlotStatusActive {
		t.Fatalf("asset adopted during review was not rebased: %+v", slot)
	}
}

func TestPromoteDoesNotReviveDownstreamAssetStaledDuringReview(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryAggregateRepository()
	service, _ := NewCommandService(repository)
	aggregate, _ := service.Create(ctx, "board-review-stale", "session-review-stale")
	candidate := dependencyTestRevision()
	candidate.ID = "revision-review-stale-active"
	aggregate, _, err := service.CreatePending(ctx, CreatePendingRevisionCommand{CommandID: "initial-plan-stale", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: candidate})
	if err != nil {
		t.Fatal(err)
	}
	aggregate, _, err = service.DecidePending(ctx, DecidePendingRevisionCommand{CommandID: "initial-approve-stale", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: aggregate.PendingRevisionID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	aggregate = bindAndActivate(t, ctx, service, aggregate, "source", "image", "asset-source-old", "source-old")
	aggregate = bindAndActivate(t, ctx, service, aggregate, "output", "video", "asset-output-old", "output-old")

	replan := dependencyTestRevision()
	replan.ID = "revision-review-stale-pending"
	aggregate, _, err = service.CreatePending(ctx, CreatePendingRevisionCommand{CommandID: "replan-stale", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, Candidate: replan, PreserveApprovedAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	aggregate = bindAndActivate(t, ctx, service, aggregate, "source", "image", "asset-source-new", "source-new-during-review")
	activeBeforePromote, _ := aggregate.ActiveRevision()
	outputBeforePromote, _ := findElement(activeBeforePromote, "output")
	if slot := findAssetSlot(outputBeforePromote, "video"); slot == nil || slot.Status != AssetSlotStatusStale {
		t.Fatalf("downstream output was not stale before promote: %+v", slot)
	}

	pendingID := aggregate.PendingRevisionID
	aggregate, _, err = service.DecidePending(ctx, DecidePendingRevisionCommand{CommandID: "approve-replan-stale", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, RevisionID: pendingID, Decision: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	active, _ := aggregate.ActiveRevision()
	output, _ := findElement(active, "output")
	slot := findAssetSlot(output, "video")
	if slot == nil || slot.Status != AssetSlotStatusStale || slot.ActiveBindingID != "" {
		t.Fatalf("promotion revived stale downstream output: %+v", slot)
	}
}

func TestReplanDoesNotReuseAssetWhenDependencyTopologyChanges(t *testing.T) {
	active := dependencyTestRevision()
	active.Modules[1].Elements[0].AssetSlots[0].ActiveBindingID = "binding-output"
	active.Modules[1].Elements[0].AssetSlots[0].GenerationEpoch = 3
	active.Modules[1].Elements[0].AssetSlots[0].Status = AssetSlotStatusActive
	active.Modules[0].Elements = append(active.Modules[0].Elements, dependencyAlternateElement())
	active.Modules[0].PlannedCount = 2
	candidate := dependencyTestRevision()
	candidate.Modules[0].Elements = append(candidate.Modules[0].Elements, dependencyAlternateElement())
	candidate.Modules[0].PlannedCount = 2
	candidate.ID = "revision-next"
	candidate.Dependencies = []DependencyEdge{{FromTargetID: "alternate", FromSlot: "image", ToTargetID: "output", ToSlot: "video"}}
	normalized, _, err := ReconcileRevision(&active, candidate, true)
	if err != nil {
		t.Fatal(err)
	}
	output, ok := findElement(&normalized, "output")
	if !ok {
		t.Fatal("output target missing")
	}
	slot := findAssetSlot(output, "video")
	if slot == nil || slot.ActiveBindingID != "" || slot.Status != AssetSlotStatusStale || slot.GenerationEpoch != 4 {
		t.Fatalf("dependency-incompatible slot = %+v", slot)
	}
}

func dependencyAlternateElement() StoryboardElement {
	return StoryboardElement{ID: "alternate", Key: "alternate", SemanticType: "reference", Title: "备用参考", Revision: 1, PromptSlots: []PromptSlot{{Purpose: "image", Prompt: "alternate", Revision: 1, Status: PromptStatusReady}}, AssetSlots: []AssetSlot{{Key: "image", MediaKind: "image", Required: true, Status: AssetSlotStatusActive, ActiveBindingID: "binding-alternate"}}}
}

func bindAndActivate(t *testing.T, ctx context.Context, service *CommandService, aggregate StoryboardAggregate, targetID, slotKey, assetID, commandPrefix string) StoryboardAggregate {
	t.Helper()
	input, err := aggregate.ResolveGenerationInput(targetID, slotKey)
	if err != nil && !errors.Is(err, ErrDependencyNotReady) {
		t.Fatal(err)
	}
	bound, disposition, err := service.Bind(ctx, BindAssetCommand{CommandID: commandPrefix + ":bind", StoryboardID: aggregate.ID, BaseVersion: aggregate.Version, BindingID: commandPrefix + ":binding", TargetID: targetID, AssetSlot: slotKey, AssetID: assetID, TargetRevision: input.TargetRevision, PromptRevision: input.PromptRevision, GenerationEpoch: input.GenerationEpoch, InputFingerprint: input.Fingerprint})
	if err != nil || disposition != BindingDispositionCandidate {
		t.Fatalf("bind %s disposition=%s err=%v", targetID, disposition, err)
	}
	activated, _, err := service.Activate(ctx, ActivateBindingCommand{CommandID: commandPrefix + ":activate", StoryboardID: aggregate.ID, BaseVersion: bound.Version, BindingID: commandPrefix + ":binding"})
	if err != nil {
		t.Fatalf("activate %s: %v", targetID, err)
	}
	return activated
}

func dependencyTestRevision() StoryboardRevision {
	return StoryboardRevision{ID: "revision-dependencies", StoryboardID: "board-dependencies", Modules: []StoryboardModule{
		{ID: "module-input", Key: "inputs", SemanticType: "reference", Title: "参考", PlannedCount: 1, Elements: []StoryboardElement{{ID: "source", Key: "source", SemanticType: "reference", Title: "参考图", Revision: 1, PromptSlots: []PromptSlot{{Purpose: "image", Prompt: "source image", Revision: 1, Status: PromptStatusReady}}, AssetSlots: []AssetSlot{{Key: "image", MediaKind: "image", Required: true, Status: AssetSlotStatusMissing}}}}},
		{ID: "module-output", Key: "outputs", SemanticType: "shot", Title: "成片", PlannedCount: 1, Elements: []StoryboardElement{{ID: "output", Key: "output", SemanticType: "shot", Title: "镜头", Revision: 1, PromptSlots: []PromptSlot{{Purpose: "video", Prompt: "animate source", Revision: 1, Status: PromptStatusReady}}, AssetSlots: []AssetSlot{{Key: "video", MediaKind: "video", Required: true, ReviewRequired: true, Status: AssetSlotStatusMissing}}}}},
	}, Dependencies: []DependencyEdge{{FromTargetID: "source", FromSlot: "image", ToTargetID: "output", ToSlot: "video"}}}
}
