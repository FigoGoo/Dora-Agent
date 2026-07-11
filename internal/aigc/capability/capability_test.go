package capability

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

func TestAgentRegistryContainsExactlyFiveCapabilities(t *testing.T) {
	ctx := context.Background()
	set, err := NewToolSetFromHandlers(ctx, successfulHandlers())
	if err != nil {
		t.Fatalf("NewToolSetFromHandlers() error = %v", err)
	}
	registry, err := NewAgentRegistry(set)
	if err != nil {
		t.Fatalf("NewAgentRegistry() error = %v", err)
	}
	if registry.Len() != 5 {
		t.Fatalf("registry length = %d, want 5", registry.Len())
	}
	for _, key := range AgentToolKeys {
		if _, ok := registry.Get(key); !ok {
			t.Errorf("registry is missing %q", key)
		}
	}
	for _, hidden := range []string{"prepare_prompts", "write_the_prompt", "image2_generate_image", "seedance_generate_video", "query_user_balance", "asset_crud"} {
		if _, ok := registry.Get(hidden); ok {
			t.Errorf("internal tool %q must not be Agent-facing", hidden)
		}
	}
	summaries, err := registry.ListSummaries(ctx)
	if err != nil {
		t.Fatalf("ListSummaries() error = %v", err)
	}
	if len(summaries) != 5 {
		t.Fatalf("summary count = %d", len(summaries))
	}
}

func TestPlanStoryboardSchemaExposesOnlyWholePlanIntent(t *testing.T) {
	set, err := NewToolSetFromHandlers(context.Background(), successfulHandlers())
	if err != nil {
		t.Fatalf("NewToolSetFromHandlers() error = %v", err)
	}
	info, err := set.PlanStoryboard.Info(context.Background())
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	schemaValue, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("ToJSONSchema() error = %v", err)
	}
	raw, err := json.Marshal(schemaValue)
	if err != nil {
		t.Fatalf("Marshal(schema) error = %v", err)
	}
	schemaJSON := string(raw)
	for _, required := range []string{"mode", "instruction", "preserve_approved_assets"} {
		if !strings.Contains(schemaJSON, `"`+required+`"`) {
			t.Errorf("schema is missing %q: %s", required, schemaJSON)
		}
	}
	for _, forbidden := range []string{"target_id", "target_ids", "scope", "patch", "provider", "session_id", "request_id", "idempotency_key"} {
		if strings.Contains(schemaJSON, `"`+forbidden+`"`) {
			t.Errorf("schema exposes forbidden field %q: %s", forbidden, schemaJSON)
		}
	}

	ctx := testCommandContext()
	if _, err := set.PlanStoryboard.InvokableRun(ctx, `{"mode":"replan","target_id":"element_hero"}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("targeted plan_storyboard input error = %v, want strict unknown-field rejection", err)
	}
	if _, err := set.PlanStoryboard.InvokableRun(ctx, `{"mode":"replan","patch":[]}`); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("patch plan_storyboard input error = %v, want strict unknown-field rejection", err)
	}
}

func TestCapabilityToolsUseTrustedContextAndTypedIntents(t *testing.T) {
	var analyzed Request[AnalyzeMaterialsIntent]
	handlers := successfulHandlers()
	handlers.AnalyzeMaterials = func(_ context.Context, request Request[AnalyzeMaterialsIntent]) (CapabilityResult[AnalyzeMaterialsData], error) {
		analyzed = request
		return CapabilityResult[AnalyzeMaterialsData]{
			Status: StatusCompleted,
			Data:   AnalyzeMaterialsData{AnalysisID: "analysis-1", AnalysisVersion: 2, Summary: "usable"},
		}, nil
	}
	set, err := NewToolSetFromHandlers(context.Background(), handlers)
	if err != nil {
		t.Fatalf("NewToolSetFromHandlers() error = %v", err)
	}
	out, err := set.AnalyzeMaterials.InvokableRun(testCommandContext(), `{"asset_ids":["asset-1"],"goal":"extract visual references"}`)
	if err != nil {
		t.Fatalf("InvokableRun() error = %v", err)
	}
	if analyzed.Command.SessionID != "session-1" || analyzed.Command.RequestID != "request-1" || !strings.HasPrefix(analyzed.Command.IdempotencyKey, "idem-1:analyze_materials:") || analyzed.Command.ToolCallID == "" {
		t.Fatalf("handler command context = %#v", analyzed.Command)
	}
	if len(analyzed.Intent.AssetIDs) != 1 || analyzed.Intent.Goal == "" {
		t.Fatalf("handler intent = %#v", analyzed.Intent)
	}
	var result CapabilityResult[AnalyzeMaterialsData]
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("Unmarshal(result) error = %v", err)
	}
	if result.Status != StatusCompleted || result.Data.AnalysisID != "analysis-1" {
		t.Fatalf("result = %#v", result)
	}

	if _, err := set.AnalyzeMaterials.InvokableRun(context.Background(), `{"goal":"analyze"}`); err == nil || !strings.Contains(err.Error(), "trusted capability command context") {
		t.Fatalf("missing trusted context error = %v", err)
	}
}

func TestCapabilityIdempotencyUsesCanonicalIntentJSON(t *testing.T) {
	commands := make([]CommandContext, 0, 2)
	handlers := successfulHandlers()
	handlers.AnalyzeMaterials = func(_ context.Context, request Request[AnalyzeMaterialsIntent]) (CapabilityResult[AnalyzeMaterialsData], error) {
		commands = append(commands, request.Command)
		return CapabilityResult[AnalyzeMaterialsData]{Status: StatusCompleted, Data: AnalyzeMaterialsData{AnalysisID: "analysis-1", Summary: "ok"}}, nil
	}
	set, err := NewToolSetFromHandlers(context.Background(), handlers)
	if err != nil {
		t.Fatal(err)
	}
	ctx := testCommandContext()
	if _, err := set.AnalyzeMaterials.InvokableRun(ctx, `{"goal":"analyze","asset_ids":["asset-1"]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := set.AnalyzeMaterials.InvokableRun(ctx, " { \n  \"asset_ids\" : [\"asset-1\"], \"goal\" : \"analyze\" }"); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[0].ToolCallID != commands[1].ToolCallID || commands[0].IdempotencyKey != commands[1].IdempotencyKey || commands[0].StageRunID != commands[1].StageRunID {
		t.Fatalf("canonical commands differ: %#v", commands)
	}
}

func TestCapabilityIdempotencySeparatesFrozenToolCallSlots(t *testing.T) {
	commands := make([]CommandContext, 0, 2)
	handlers := successfulHandlers()
	handlers.AnalyzeMaterials = func(_ context.Context, request Request[AnalyzeMaterialsIntent]) (CapabilityResult[AnalyzeMaterialsData], error) {
		commands = append(commands, request.Command)
		return CapabilityResult[AnalyzeMaterialsData]{Status: StatusCompleted, Data: AnalyzeMaterialsData{AnalysisID: "analysis-1", Summary: "ok"}}, nil
	}
	set, err := NewToolSetFromHandlers(context.Background(), handlers)
	if err != nil {
		t.Fatal(err)
	}
	for _, callID := range []string{"call-slot-a", "call-slot-b"} {
		ctx := WithCommandContext(context.Background(), CommandContext{SessionID: "session-1", RequestID: "turn-1", IdempotencyKey: "input-1", ToolCallID: callID})
		if _, err := set.AnalyzeMaterials.InvokableRun(ctx, `{"goal":"analyze"}`); err != nil {
			t.Fatal(err)
		}
	}
	if len(commands) != 2 || commands[0].ToolCallID == commands[1].ToolCallID || commands[0].IdempotencyKey == commands[1].IdempotencyKey {
		t.Fatalf("logical call slots collapsed: %#v", commands)
	}
}

func TestAllFivePrecompiledGraphsExecute(t *testing.T) {
	set, err := NewToolSetFromHandlers(context.Background(), successfulHandlers())
	if err != nil {
		t.Fatalf("NewToolSetFromHandlers() error = %v", err)
	}
	ctx := testCommandContext()
	invocations := []struct {
		name string
		run  func() (string, error)
	}{
		{AnalyzeMaterialsToolKey, func() (string, error) { return set.AnalyzeMaterials.InvokableRun(ctx, `{"goal":"analyze brief"}`) }},
		{PlanCreationSpecToolKey, func() (string, error) {
			return set.PlanCreationSpec.InvokableRun(ctx, `{"mode":"create","goal":"short drama"}`)
		}},
		{PlanStoryboardToolKey, func() (string, error) { return set.PlanStoryboard.InvokableRun(ctx, `{"mode":"create"}`) }},
		{GenerateMediaToolKey, func() (string, error) {
			return set.GenerateMedia.InvokableRun(ctx, `{"phase":"auto_next","policy":"single_next"}`)
		}},
		{AssembleOutputToolKey, func() (string, error) { return set.AssembleOutput.InvokableRun(ctx, `{"mode":"plan"}`) }},
	}
	for _, invocation := range invocations {
		t.Run(invocation.name, func(t *testing.T) {
			out, err := invocation.run()
			if err != nil {
				t.Fatalf("InvokableRun() error = %v", err)
			}
			if !strings.Contains(out, `"status":"`) {
				t.Fatalf("result = %s", out)
			}
		})
	}
}

func TestIntentValidationAndResultStatus(t *testing.T) {
	set, err := NewToolSetFromHandlers(context.Background(), successfulHandlers())
	if err != nil {
		t.Fatalf("NewToolSetFromHandlers() error = %v", err)
	}
	ctx := testCommandContext()
	if _, err := set.GenerateMedia.InvokableRun(ctx, `{"phase":"target","policy":"single_next"}`); err == nil {
		t.Fatal("expected invalid generate_media phase to fail")
	}
	if _, err := set.PlanCreationSpec.InvokableRun(ctx, `{"mode":"create"}`); err == nil {
		t.Fatal("expected empty creation spec intent to fail")
	}

	executors, err := CompileExecutors(context.Background(), successfulHandlers())
	if err != nil {
		t.Fatalf("CompileExecutors() error = %v", err)
	}
	executors.AssembleOutput = ExecutorFunc[AssembleOutputIntent, AssembleOutputData](func(context.Context, Request[AssembleOutputIntent]) (CapabilityResult[AssembleOutputData], error) {
		return CapabilityResult[AssembleOutputData]{Status: "queued"}, nil
	})
	invalidSet, err := NewToolSet(executors)
	if err != nil {
		t.Fatalf("NewToolSet() error = %v", err)
	}
	if _, err := invalidSet.AssembleOutput.InvokableRun(ctx, `{"mode":"plan"}`); err == nil || !strings.Contains(err.Error(), "invalid result") {
		t.Fatalf("invalid result status error = %v", err)
	}
}

func successfulHandlers() Handlers {
	return Handlers{
		AnalyzeMaterials: func(context.Context, Request[AnalyzeMaterialsIntent]) (CapabilityResult[AnalyzeMaterialsData], error) {
			return CapabilityResult[AnalyzeMaterialsData]{Status: StatusCompleted, Data: AnalyzeMaterialsData{AnalysisID: "analysis-1", Summary: "ok"}}, nil
		},
		PlanCreationSpec: func(context.Context, Request[PlanCreationSpecIntent]) (CapabilityResult[PlanCreationSpecData], error) {
			return CapabilityResult[PlanCreationSpecData]{Status: StatusWaitingUser, Data: PlanCreationSpecData{SpecID: "spec-1", SpecVersion: 1, ApprovalID: "approval-spec"}}, nil
		},
		PlanStoryboard: func(context.Context, Request[PlanStoryboardIntent]) (CapabilityResult[PlanStoryboardData], error) {
			return CapabilityResult[PlanStoryboardData]{Status: StatusWaitingUser, StoryboardID: "board-1", Data: PlanStoryboardData{
				Revision: storyboard.StoryboardRevision{ID: "revision-1", StoryboardID: "board-1"}, ApprovalID: "approval-board",
			}}, nil
		},
		GenerateMedia: func(context.Context, Request[GenerateMediaIntent]) (CapabilityResult[GenerateMediaData], error) {
			return CapabilityResult[GenerateMediaData]{Status: StatusAccepted, OperationID: "operation-1", BatchID: "batch-1", Data: GenerateMediaData{SelectedTargets: []string{"element-1"}, JobCount: 1}}, nil
		},
		AssembleOutput: func(context.Context, Request[AssembleOutputIntent]) (CapabilityResult[AssembleOutputData], error) {
			return CapabilityResult[AssembleOutputData]{Status: StatusCompleted, Data: AssembleOutputData{AssemblyRevisionID: "assembly-1"}}, nil
		},
	}
}

func testCommandContext() context.Context {
	return WithCommandContext(context.Background(), CommandContext{
		SessionID: "session-1", RequestID: "request-1", IdempotencyKey: "idem-1", ExpectedSpecVersion: 2, ExpectedStoryboardVersion: 3,
	})
}

func TestAgentToolKeysAreStableAndUnique(t *testing.T) {
	if len(AgentToolKeys) != 5 {
		t.Fatalf("AgentToolKeys = %#v", AgentToolKeys)
	}
	for i, key := range AgentToolKeys {
		if slices.Contains(AgentToolKeys[:i], key) {
			t.Fatalf("duplicate Agent tool key %q", key)
		}
	}
}
