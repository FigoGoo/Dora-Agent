package contract_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const analyzeMaterialsRuntimeV2Preview1CorpusDir = "agent/tests/contract/testdata/v2_analyze_materials_runtime_preview1"

type analyzeMaterialsRuntimeV2Preview1Manifest struct {
	SchemaVersion    string                                          `json:"schema_version"`
	Profile          string                                          `json:"profile"`
	Files            []analyzeMaterialsRuntimeV2Preview1ManifestFile `json:"files"`
	FixtureIDs       []string                                        `json:"fixture_ids"`
	VectorIDs        []string                                        `json:"vector_ids"`
	TotalVectorCount int                                             `json:"total_vector_count"`
	TargetTests      []string                                        `json:"target_tests"`
}

type analyzeMaterialsRuntimeV2Preview1ManifestFile struct {
	File        string `json:"file"`
	SHA256      string `json:"sha256"`
	VectorCount int    `json:"vector_count"`
}

type analyzeMaterialsRuntimeV2Preview1Corpus struct {
	SchemaVersion string                                          `json:"schema_version"`
	Profile       string                                          `json:"profile"`
	Fixture       analyzeMaterialsRuntimeV2Preview1Fixture        `json:"fixture"`
	Vectors       []analyzeMaterialsRuntimeV2Preview1ContractCase `json:"vectors"`
}

type analyzeMaterialsRuntimeV2Preview1Fixture struct {
	FixtureID                 string                                            `json:"fixture_id"`
	EligibleSourceType        string                                            `json:"eligible_source_type"`
	ExecutableToolRegistry    []string                                          `json:"executable_tool_registry"`
	InputStates               []string                                          `json:"input_states"`
	RunStates                 []string                                          `json:"run_states"`
	ModelCallKinds            []string                                          `json:"model_call_kinds"`
	ModelStates               []string                                          `json:"model_states"`
	ToolStates                []string                                          `json:"tool_states"`
	ProjectionOutcomes        []string                                          `json:"projection_outcomes"`
	PreviewObjectNames        []string                                          `json:"preview_object_names"`
	IntentFields              []string                                          `json:"intent_fields"`
	ExpectedAssetFields       []string                                          `json:"expected_asset_fields"`
	AcceptedEventFields       []string                                          `json:"accepted_event_fields"`
	SuccessCardFields         []string                                          `json:"success_card_fields"`
	FailureCardFields         []string                                          `json:"failure_card_fields"`
	ContextPinValues          analyzeMaterialsRuntimeV2Preview1ContextPinValues `json:"context_pin_values"`
	TurnContextFields         []string                                          `json:"turn_context_fields"`
	TurnContextContractDigest string                                            `json:"turn_context_contract_digest"`
	CardSchemas               []string                                          `json:"card_schemas"`
	EventTypes                []string                                          `json:"event_types"`
	ActivationRequirements    []string                                          `json:"activation_requirements"`
}

type analyzeMaterialsRuntimeV2Preview1ContextPinValues struct {
	Profile               string `json:"profile"`
	SchemaVersion         string `json:"schema_version"`
	ToolRegistryRef       string `json:"tool_registry_ref"`
	ToolDefinitionRef     string `json:"tool_definition_ref"`
	IntentSchemaRef       string `json:"intent_schema_ref"`
	ResultSchemaRef       string `json:"result_schema_ref"`
	PromptRef             string `json:"prompt_ref"`
	ValidatorRef          string `json:"validator_ref"`
	EvidencePolicyRef     string `json:"evidence_policy_ref"`
	RouterModelRouteRef   string `json:"router_model_route_ref"`
	AnalysisModelRouteRef string `json:"analysis_model_route_ref"`
	RuntimePolicyRef      string `json:"runtime_policy_ref"`
	BudgetRef             string `json:"budget_ref"`
}

type analyzeMaterialsRuntimeV2Preview1ContractCase struct {
	ID                           string   `json:"id"`
	Kind                         string   `json:"kind"`
	Scenario                     string   `json:"scenario"`
	ExpectedDecision             string   `json:"expected_decision"`
	ExpectedInputStatus          string   `json:"expected_input_status"`
	ExpectedRouterModelCallDelta int      `json:"expected_router_model_call_delta"`
	ExpectedGraphModelCallDelta  int      `json:"expected_graph_model_call_delta"`
	ExpectedToolCallDelta        int      `json:"expected_tool_call_delta"`
	ExpectedEvidenceReadDelta    int      `json:"expected_evidence_read_delta"`
	ExpectedBusinessWriteDelta   int      `json:"expected_business_write_delta"`
	ExpectedProjectionDelta      int      `json:"expected_projection_delta"`
	ExpectedEventDelta           int      `json:"expected_event_delta"`
	RequiredAssertions           []string `json:"required_assertions"`
	ForbiddenAssertions          []string `json:"forbidden_assertions"`
}

type analyzeMaterialsRuntimeV2Preview1Outcome struct {
	decision   string
	input      string
	router     int
	graph      int
	tool       int
	evidence   int
	projection int
	event      int
}

func TestAnalyzeMaterialsRuntimeV2Preview1CorpusManifest(t *testing.T) {
	manifest, corpus, corpusRaw := loadAnalyzeMaterialsRuntimeV2Preview1Corpus(t)
	if manifest.SchemaVersion != "analyze_materials_runtime_v2preview1_corpus_manifest.v1" ||
		manifest.Profile != "analyze_materials.runtime.v2preview1" {
		t.Fatalf("invalid manifest identity: %+v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].File != "analyze_materials_runtime_v2preview1.json" ||
		manifest.Files[0].VectorCount != 22 || manifest.TotalVectorCount != 22 {
		t.Fatalf("invalid manifest files/count: files=%+v total=%d", manifest.Files, manifest.TotalVectorCount)
	}
	digest := sha256.Sum256(corpusRaw)
	actualDigest := "sha256:" + hex.EncodeToString(digest[:])
	if manifest.Files[0].SHA256 != actualDigest {
		t.Fatalf("corpus digest=%q want=%q", actualDigest, manifest.Files[0].SHA256)
	}
	if !reflect.DeepEqual(manifest.FixtureIDs, []string{"analyze_materials.runtime.base"}) {
		t.Fatalf("fixture_ids=%v", manifest.FixtureIDs)
	}
	if !reflect.DeepEqual(manifest.VectorIDs, analyzeMaterialsRuntimeV2Preview1VectorIDs()) {
		t.Fatalf("vector_ids=%v", manifest.VectorIDs)
	}
	wantTests := []string{
		"TestAnalyzeMaterialsRuntimeV2Preview1CorpusManifest",
		"TestAnalyzeMaterialsRuntimeV2Preview1Corpus",
		"TestAnalyzeMaterialsRuntimeV2Preview1ExactSets",
		"TestAnalyzeMaterialsRuntimeV2Preview1ContextContractDigest",
		"TestAnalyzeMaterialsRuntimeV2Preview1OutcomeAndNegativeInvariants",
		"TestAnalyzeMaterialsRuntimeV2Preview1StrictJSON",
	}
	if !reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("target_tests=%v want=%v", manifest.TargetTests, wantTests)
	}
	if len(corpus.Vectors) != manifest.TotalVectorCount {
		t.Fatalf("vectors=%d want=%d", len(corpus.Vectors), manifest.TotalVectorCount)
	}
}

func TestAnalyzeMaterialsRuntimeV2Preview1Corpus(t *testing.T) {
	_, corpus, _ := loadAnalyzeMaterialsRuntimeV2Preview1Corpus(t)
	if corpus.SchemaVersion != "analyze_materials_runtime_v2preview1_corpus.v1" ||
		corpus.Profile != "analyze_materials.runtime.v2preview1" {
		t.Fatalf("invalid corpus identity: schema=%q profile=%q", corpus.SchemaVersion, corpus.Profile)
	}
	if corpus.Fixture.FixtureID != "analyze_materials.runtime.base" ||
		corpus.Fixture.EligibleSourceType != "analyze_materials_preview" {
		t.Fatalf("invalid fixture identity: %+v", corpus.Fixture)
	}
	seen := make(map[string]struct{}, len(corpus.Vectors))
	for _, vector := range corpus.Vectors {
		if _, duplicate := seen[vector.ID]; duplicate {
			t.Fatalf("duplicate vector id=%q", vector.ID)
		}
		seen[vector.ID] = struct{}{}
		if vector.Kind != "positive" && vector.Kind != "negative" {
			t.Fatalf("%s kind=%q", vector.ID, vector.Kind)
		}
		if strings.TrimSpace(vector.Scenario) == "" || strings.TrimSpace(vector.ExpectedDecision) == "" || strings.TrimSpace(vector.ExpectedInputStatus) == "" {
			t.Fatalf("%s incomplete scenario/decision/status", vector.ID)
		}
		for name, delta := range map[string]int{
			"router": vector.ExpectedRouterModelCallDelta, "graph": vector.ExpectedGraphModelCallDelta,
			"tool": vector.ExpectedToolCallDelta, "evidence": vector.ExpectedEvidenceReadDelta,
			"business": vector.ExpectedBusinessWriteDelta, "projection": vector.ExpectedProjectionDelta,
			"event": vector.ExpectedEventDelta,
		} {
			if delta < 0 || delta > 1 {
				t.Fatalf("%s %s delta=%d outside preview budget", vector.ID, name, delta)
			}
		}
		if vector.ExpectedBusinessWriteDelta != 0 {
			t.Fatalf("%s business write delta=%d, want zero", vector.ID, vector.ExpectedBusinessWriteDelta)
		}
		if len(vector.RequiredAssertions) == 0 || len(vector.ForbiddenAssertions) == 0 {
			t.Fatalf("%s must freeze required and forbidden assertions", vector.ID)
		}
	}
	if len(seen) != len(analyzeMaterialsRuntimeV2Preview1VectorIDs()) {
		t.Fatalf("vector count=%d", len(seen))
	}
	for _, id := range analyzeMaterialsRuntimeV2Preview1VectorIDs() {
		if _, ok := seen[id]; !ok {
			t.Fatalf("missing vector=%s", id)
		}
	}
}

func TestAnalyzeMaterialsRuntimeV2Preview1ExactSets(t *testing.T) {
	_, corpus, _ := loadAnalyzeMaterialsRuntimeV2Preview1Corpus(t)
	fixture := corpus.Fixture
	assertAnalyzeMaterialsSet(t, "executable_tool_registry", fixture.ExecutableToolRegistry, []string{"analyze_materials"})
	assertAnalyzeMaterialsSet(t, "input_states", fixture.InputStates, []string{"pending", "claimed", "running", "retry_wait", "resolved", "dead"})
	assertAnalyzeMaterialsSet(t, "run_states", fixture.RunStates, []string{"created", "running", "completed", "failed"})
	assertAnalyzeMaterialsSet(t, "model_call_kinds", fixture.ModelCallKinds, []string{"router", "graph_analysis"})
	assertAnalyzeMaterialsSet(t, "model_states", fixture.ModelStates, []string{"reserved", "completed", "failed"})
	assertAnalyzeMaterialsSet(t, "tool_states", fixture.ToolStates, []string{"open", "completed", "partial", "failed"})
	assertAnalyzeMaterialsSet(t, "projection_outcomes", fixture.ProjectionOutcomes, []string{"tool_completed", "tool_partial", "tool_failed", "runtime_failed"})
	assertAnalyzeMaterialsSet(t, "preview_object_names", fixture.PreviewObjectNames, []string{
		"agent.analyze_materials_preview_run",
		"agent.analyze_materials_preview_turn_context",
		"agent.analyze_materials_preview_model_receipt",
		"agent.analyze_materials_preview_tool_receipt",
		"agent.analyze_materials_preview_projection",
	})
	assertAnalyzeMaterialsSet(t, "intent_fields", fixture.IntentFields, []string{
		"schema_version", "asset_ids", "analysis_goal", "focus_dimensions", "output_language", "expected_assets",
	})
	assertAnalyzeMaterialsSet(t, "expected_asset_fields", fixture.ExpectedAssetFields, []string{"asset_id", "asset_version"})
	assertAnalyzeMaterialsSet(t, "accepted_event_fields", fixture.AcceptedEventFields, []string{
		"input_id", "session_id", "turn_id", "run_id", "request_id", "source_type", "intent_digest", "tool_call_id", "context_digest",
	})
	assertAnalyzeMaterialsSet(t, "success_card_fields", fixture.SuccessCardFields, []string{
		"schema_version", "input_id", "turn_id", "run_id", "tool_call_id", "status", "result_code", "analysis", "coverage", "evidence_refs",
	})
	assertAnalyzeMaterialsSet(t, "failure_card_fields", fixture.FailureCardFields, []string{
		"schema_version", "input_id", "turn_id", "run_id", "tool_call_id", "status", "result_code", "failure_kind", "summary", "retryable",
	})
	wantPins := analyzeMaterialsRuntimeV2Preview1ContextPinValues{
		Profile: "analyze_materials.runtime.v2preview1", SchemaVersion: "analyze_materials.turn_context.v2preview1",
		ToolRegistryRef: "analyze_materials.preview_tools@v1", ToolDefinitionRef: "analyze_materials.v2preview1",
		IntentSchemaRef: "analyze_materials.preview.intent.v1", ResultSchemaRef: "analyze_materials.preview.result.v1",
		PromptRef: "graph_tool.analyze_materials.preview.v1", ValidatorRef: "analyze_materials.preview.validator.v1",
		EvidencePolicyRef:     "analyze_materials.preview.evidence-policy.v1",
		RouterModelRouteRef:   "local.fake.analyze_materials.router@v1",
		AnalysisModelRouteRef: "local.fake.analyze_materials.analysis@v1",
		RuntimePolicyRef:      "analyze_materials.runtime_policy@v1", BudgetRef: "analyze_materials.local_preview_budget@v1",
	}
	if fixture.ContextPinValues != wantPins {
		t.Fatalf("context_pin_values=%+v want=%+v", fixture.ContextPinValues, wantPins)
	}
	assertAnalyzeMaterialsSet(t, "card_schemas", fixture.CardSchemas, []string{"analyze_materials.preview.card.v1"})
	assertAnalyzeMaterialsSet(t, "event_types", fixture.EventTypes, []string{
		"analyze_materials.preview.accepted",
		"analyze_materials.preview.completed",
		"analyze_materials.preview.partial",
		"analyze_materials.preview.failed",
		"analyze_materials.preview.runtime_failed",
	})
	assertAnalyzeMaterialsSet(t, "activation_requirements", fixture.ActivationRequirements, []string{
		"DORA_ENV=local",
		"postgresql=127.0.0.1:15432/dora_agent_test",
		"redis=127.0.0.1:16379",
		"etcd=127.0.0.1:12379",
		"DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_ENABLED=true",
		"DORA_AGENT_ANALYZE_MATERIALS_RUNTIME_PROFILE=analyze_materials.runtime.v2preview1",
		"DORA_BUSINESS_ANALYZE_MATERIALS_RUNTIME_ENABLED=true",
		"AGENT_SSE_MAX_EVENT_BYTES=131072",
		"profile_exact_match",
		"plan_creation_spec_disabled",
		"user_message_runtime_disabled",
	})
	for _, forbidden := range []string{"recovery_pending", "model_unknown", "dispatched", "plan_creation_spec", "user_message"} {
		if containsAnalyzeMaterialsString(fixture.InputStates, forbidden) ||
			containsAnalyzeMaterialsString(fixture.ModelStates, forbidden) ||
			containsAnalyzeMaterialsString(fixture.ExecutableToolRegistry, forbidden) {
			t.Fatalf("forbidden preview capability %q entered fixture exact sets", forbidden)
		}
	}
}

func TestAnalyzeMaterialsRuntimeV2Preview1ContextContractDigest(t *testing.T) {
	_, corpus, _ := loadAnalyzeMaterialsRuntimeV2Preview1Corpus(t)
	wantFields := []string{
		"profile", "schema_version", "session_id", "input_id", "turn_id", "run_id", "tool_call_id",
		"router_model_call_id", "graph_model_call_id", "user_id", "project_id", "intent_ciphertext",
		"intent_key_version", "intent_digest", "access_scope_ref", "access_scope_digest", "tool_registry_ref",
		"tool_registry_digest", "tool_definition_ref", "tool_definition_digest", "intent_schema_ref",
		"result_schema_ref", "prompt_ref", "prompt_digest", "validator_ref", "validator_digest",
		"evidence_policy_ref", "evidence_policy_digest", "router_model_route_ref", "router_model_route_digest",
		"analysis_model_route_ref", "analysis_model_route_digest", "runtime_policy_ref", "runtime_policy_digest",
		"budget_ref", "budget_digest", "context_digest",
	}
	if !reflect.DeepEqual(corpus.Fixture.TurnContextFields, wantFields) {
		t.Fatalf("turn_context_fields=%v want=%v", corpus.Fixture.TurnContextFields, wantFields)
	}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("analyze_materials.turn_context.fields.v2preview1\x00"))
	for _, field := range wantFields {
		_, _ = hasher.Write([]byte(field))
		_, _ = hasher.Write([]byte{0})
	}
	actual := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if corpus.Fixture.TurnContextContractDigest != actual {
		t.Fatalf("turn_context_contract_digest=%q want=%q", corpus.Fixture.TurnContextContractDigest, actual)
	}
	if len(wantFields) >= 58 {
		t.Fatal("preview context must not alias the production 58-field candidate")
	}
}

func TestAnalyzeMaterialsRuntimeV2Preview1OutcomeAndNegativeInvariants(t *testing.T) {
	_, corpus, _ := loadAnalyzeMaterialsRuntimeV2Preview1Corpus(t)
	vectors := make(map[string]analyzeMaterialsRuntimeV2Preview1ContractCase, len(corpus.Vectors))
	for _, vector := range corpus.Vectors {
		vectors[vector.ID] = vector
	}
	wantOutcomes := map[string]analyzeMaterialsRuntimeV2Preview1Outcome{
		"AM-P03-tool-completed":                {"tool_completed", "resolved", 1, 1, 1, 1, 1, 1},
		"AM-P04-tool-partial":                  {"tool_partial", "resolved", 1, 1, 1, 1, 1, 1},
		"AM-P05-tool-failed":                   {"tool_failed", "resolved", 1, 1, 1, 1, 1, 1},
		"AM-P06-runtime-failed":                {"runtime_failed", "dead", 1, 0, 0, 0, 1, 1},
		"AM-P07-frozen-tool-projection-replay": {"tool_completed", "resolved", 0, 0, 0, 0, 1, 1},
		"AM-P08-terminal-response-loss":        {"already_terminal", "resolved", 0, 0, 0, 0, 0, 0},
	}
	for id, want := range wantOutcomes {
		vector, ok := vectors[id]
		if !ok {
			t.Fatalf("missing outcome vector=%s", id)
		}
		got := analyzeMaterialsRuntimeV2Preview1Outcome{
			vector.ExpectedDecision, vector.ExpectedInputStatus,
			vector.ExpectedRouterModelCallDelta, vector.ExpectedGraphModelCallDelta,
			vector.ExpectedToolCallDelta, vector.ExpectedEvidenceReadDelta,
			vector.ExpectedProjectionDelta, vector.ExpectedEventDelta,
		}
		if got != want {
			t.Fatalf("%s outcome=%+v want=%+v", id, got, want)
		}
	}
	wantForbidden := map[string][]string{
		"AM-N01-idempotency-conflict":     {"second_context", "second_accepted_event"},
		"AM-N02-extra-tool":               {"dynamic_tool_search", "provider_tool", "crud_tool"},
		"AM-N03-dual-processor":           {"dual_claim", "source_filtered_parallel_processor"},
		"AM-N04-stale-fence":              {"receipt_freeze", "projection_write", "event_increment", "lease_release"},
		"AM-N05-evidence-permission":      {"evidence_content", "asset_existence", "rpc_error_detail"},
		"AM-N06-business-side-effects":    {"material_analysis_revision", "creation_spec", "approval", "billing", "operation", "batch", "job", "worker"},
		"AM-N07-external-provider":        {"provider_dispatch", "model_unknown", "automatic_resend", "failover"},
		"AM-N08-shared-production-enable": {"processor_start", "production_authorized"},
		"AM-N09-recovery-pending":         {"recovery_pending", "model_unknown", "production_receipt_alias"},
		"AM-N10-message-fabrication":      {"session_message", "assistant_message", "tool_message_history", "quick_create_prompt_reuse"},
	}
	for id, forbidden := range wantForbidden {
		vector, ok := vectors[id]
		if !ok {
			t.Fatalf("missing negative vector=%s", id)
		}
		if vector.Kind != "negative" || !reflect.DeepEqual(vector.ForbiddenAssertions, forbidden) {
			t.Fatalf("%s forbidden_assertions=%v want=%v", id, vector.ForbiddenAssertions, forbidden)
		}
	}
	if vectors["AM-N05-evidence-permission"].ExpectedInputStatus != "resolved" ||
		vectors["AM-N06-business-side-effects"].ExpectedBusinessWriteDelta != 0 {
		t.Fatal("safe tool failure or read-only Business invariant drifted")
	}
}

func TestAnalyzeMaterialsRuntimeV2Preview1StrictJSON(t *testing.T) {
	root := approvalRepoRootV1(t)
	for _, relative := range []string{
		filepath.Join(analyzeMaterialsRuntimeV2Preview1CorpusDir, "manifest.json"),
		filepath.Join(analyzeMaterialsRuntimeV2Preview1CorpusDir, "analyze_materials_runtime_v2preview1.json"),
	} {
		raw, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		cases := map[string][]byte{
			"unknown":   []byte(strings.Replace(string(raw), "{", "{\"future\":true,", 1)),
			"duplicate": []byte(strings.Replace(string(raw), "{", "{\"schema_version\":\"duplicate\",", 1)),
			"trailing":  append(append([]byte(nil), raw...), []byte(`{}`)...),
		}
		for name, candidate := range cases {
			t.Run(filepath.Base(relative)+"/"+name, func(t *testing.T) {
				var target any
				if strings.HasSuffix(relative, "manifest.json") {
					target = &analyzeMaterialsRuntimeV2Preview1Manifest{}
				} else {
					target = &analyzeMaterialsRuntimeV2Preview1Corpus{}
				}
				if err := messageSetStrictDecodeV1(candidate, target); err == nil {
					t.Fatalf("strict decoder accepted %s", name)
				}
			})
		}
	}
}

func loadAnalyzeMaterialsRuntimeV2Preview1Corpus(t *testing.T) (analyzeMaterialsRuntimeV2Preview1Manifest, analyzeMaterialsRuntimeV2Preview1Corpus, []byte) {
	t.Helper()
	root := approvalRepoRootV1(t)
	manifestRaw, err := os.ReadFile(filepath.Join(root, analyzeMaterialsRuntimeV2Preview1CorpusDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest analyzeMaterialsRuntimeV2Preview1Manifest
	if err := messageSetStrictDecodeV1(manifestRaw, &manifest); err != nil {
		t.Fatalf("decode corpus manifest: %v", err)
	}
	corpusRaw, err := os.ReadFile(filepath.Join(root, analyzeMaterialsRuntimeV2Preview1CorpusDir, "analyze_materials_runtime_v2preview1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus analyzeMaterialsRuntimeV2Preview1Corpus
	if err := messageSetStrictDecodeV1(corpusRaw, &corpus); err != nil {
		t.Fatalf("decode corpus: %v", err)
	}
	return manifest, corpus, corpusRaw
}

func analyzeMaterialsRuntimeV2Preview1VectorIDs() []string {
	return []string{
		"AM-P01-enqueue-accepted", "AM-P02-idempotent-replay", "AM-P03-tool-completed",
		"AM-P04-tool-partial", "AM-P05-tool-failed", "AM-P06-runtime-failed",
		"AM-P07-frozen-tool-projection-replay", "AM-P08-terminal-response-loss",
		"AM-P09-lost-wake-scanner", "AM-P10-fence-takeover", "AM-P11-session-hol", "AM-P12-local-activation",
		"AM-N01-idempotency-conflict", "AM-N02-extra-tool", "AM-N03-dual-processor", "AM-N04-stale-fence",
		"AM-N05-evidence-permission", "AM-N06-business-side-effects", "AM-N07-external-provider",
		"AM-N08-shared-production-enable", "AM-N09-recovery-pending", "AM-N10-message-fabrication",
	}
}

func assertAnalyzeMaterialsSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s=%v want=%v", name, got, want)
	}
}

func containsAnalyzeMaterialsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
