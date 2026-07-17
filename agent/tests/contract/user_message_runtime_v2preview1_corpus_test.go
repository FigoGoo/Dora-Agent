package contract_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const userMessageRuntimeV2Preview1CorpusDir = "agent/tests/contract/testdata/v2_user_message_runtime_preview1"

type userMessageRuntimeV2Preview1Manifest struct {
	SchemaVersion    string                                     `json:"schema_version"`
	Profile          string                                     `json:"profile"`
	SelectedScheme   string                                     `json:"selected_scheme"`
	Files            []userMessageRuntimeV2Preview1ManifestFile `json:"files"`
	FixtureIDs       []string                                   `json:"fixture_ids"`
	VectorIDs        []string                                   `json:"vector_ids"`
	TotalVectorCount int                                        `json:"total_vector_count"`
	TargetTests      []string                                   `json:"target_tests"`
}

type userMessageRuntimeV2Preview1ManifestFile struct {
	File        string `json:"file"`
	SHA256      string `json:"sha256"`
	VectorCount int    `json:"vector_count"`
}

type userMessageRuntimeV2Preview1Corpus struct {
	SchemaVersion  string                                     `json:"schema_version"`
	Profile        string                                     `json:"profile"`
	SelectedScheme string                                     `json:"selected_scheme"`
	Fixture        userMessageRuntimeV2Preview1Fixture        `json:"fixture"`
	Vectors        []userMessageRuntimeV2Preview1ContractCase `json:"vectors"`
}

type userMessageRuntimeV2Preview1Fixture struct {
	FixtureID                 string   `json:"fixture_id"`
	EligibleSourceType        string   `json:"eligible_source_type"`
	EligibleEnqueueSeq        int      `json:"eligible_enqueue_seq"`
	RouterDecisions           []string `json:"router_decisions"`
	ExecutableToolRegistry    []string `json:"executable_tool_registry"`
	InputStates               []string `json:"input_states"`
	TurnStates                []string `json:"turn_states"`
	RunStates                 []string `json:"run_states"`
	ModelStates               []string `json:"model_states"`
	OutputStates              []string `json:"output_states"`
	PreviewObjectNames        []string `json:"preview_object_names"`
	TurnContextFields         []string `json:"turn_context_fields"`
	TurnContextContractDigest string   `json:"turn_context_contract_digest"`
	OutputSchemas             []string `json:"output_schemas"`
	EventTypes                []string `json:"event_types"`
	BlockerPriority           []string `json:"blocker_priority"`
	ActivationRequirements    []string `json:"activation_requirements"`
}

type userMessageRuntimeV2Preview1ContractCase struct {
	ID                      string   `json:"id"`
	Kind                    string   `json:"kind"`
	Scenario                string   `json:"scenario"`
	ExpectedDecision        string   `json:"expected_decision"`
	ExpectedInputStatus     string   `json:"expected_input_status"`
	ExpectedModelCallDelta  int      `json:"expected_model_call_delta"`
	ExpectedToolCallDelta   int      `json:"expected_tool_call_delta"`
	ExpectedSideEffectDelta int      `json:"expected_side_effect_delta"`
	ExpectedWriteDelta      int      `json:"expected_write_delta"`
	RequiredAssertions      []string `json:"required_assertions"`
	ForbiddenAssertions     []string `json:"forbidden_assertions"`
}

func TestUserMessageRuntimeV2Preview1CorpusManifest(t *testing.T) {
	manifest, corpus, corpusRaw := loadUserMessageRuntimeV2Preview1Corpus(t)
	if manifest.SchemaVersion != "user_message_runtime_v2preview1_corpus_manifest.v1" ||
		manifest.Profile != "user_message.runtime.v2preview1" || manifest.SelectedScheme != "A" {
		t.Fatalf("invalid manifest identity: %+v", manifest)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].File != "user_message_runtime_v2preview1.json" ||
		manifest.Files[0].VectorCount != 27 || manifest.TotalVectorCount != 27 {
		t.Fatalf("invalid manifest files/count: files=%+v total=%d", manifest.Files, manifest.TotalVectorCount)
	}
	digest := sha256.Sum256(corpusRaw)
	actualDigest := "sha256:" + hex.EncodeToString(digest[:])
	if manifest.Files[0].SHA256 != actualDigest {
		t.Fatalf("corpus digest=%q want=%q", actualDigest, manifest.Files[0].SHA256)
	}
	if !reflect.DeepEqual(manifest.FixtureIDs, []string{"user_message.scheme_a.base"}) {
		t.Fatalf("fixture_ids=%v", manifest.FixtureIDs)
	}
	wantVectorIDs := userMessageRuntimeV2Preview1VectorIDs()
	if !reflect.DeepEqual(manifest.VectorIDs, wantVectorIDs) {
		t.Fatalf("vector_ids=%v want=%v", manifest.VectorIDs, wantVectorIDs)
	}
	wantTests := []string{
		"TestUserMessageRuntimeV2Preview1CorpusManifest",
		"TestUserMessageRuntimeV2Preview1Corpus",
		"TestUserMessageRuntimeV2Preview1ExactSets",
		"TestUserMessageRuntimeV2Preview1ContextContractDigest",
		"TestUserMessageRuntimeV2Preview1NegativeCapabilities",
		"TestUserMessageRuntimeV2Preview1StrictJSON",
	}
	if !reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("target_tests=%v want=%v", manifest.TargetTests, wantTests)
	}
	if len(corpus.Vectors) != manifest.TotalVectorCount {
		t.Fatalf("vectors=%d want=%d", len(corpus.Vectors), manifest.TotalVectorCount)
	}
}

func TestUserMessageRuntimeV2Preview1Corpus(t *testing.T) {
	_, corpus, _ := loadUserMessageRuntimeV2Preview1Corpus(t)
	if corpus.SchemaVersion != "user_message_runtime_v2preview1_corpus.v1" ||
		corpus.Profile != "user_message.runtime.v2preview1" || corpus.SelectedScheme != "A" {
		t.Fatalf("invalid corpus identity: schema=%q profile=%q scheme=%q", corpus.SchemaVersion, corpus.Profile, corpus.SelectedScheme)
	}
	if corpus.Fixture.FixtureID != "user_message.scheme_a.base" || corpus.Fixture.EligibleSourceType != "user_message" || corpus.Fixture.EligibleEnqueueSeq != 1 {
		t.Fatalf("invalid fixture eligibility: %+v", corpus.Fixture)
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
		if vector.ExpectedToolCallDelta != 0 || vector.ExpectedSideEffectDelta != 0 {
			t.Fatalf("%s tool_delta=%d side_effect_delta=%d", vector.ID, vector.ExpectedToolCallDelta, vector.ExpectedSideEffectDelta)
		}
		if len(vector.RequiredAssertions) == 0 || len(vector.ForbiddenAssertions) == 0 {
			t.Fatalf("%s must freeze required and forbidden assertions", vector.ID)
		}
	}
	if !reflect.DeepEqual(keysUserMessageRuntimeV2Preview1(seen), userMessageRuntimeV2Preview1VectorIDs()) {
		t.Fatalf("corpus vector IDs drift")
	}
}

func TestUserMessageRuntimeV2Preview1ExactSets(t *testing.T) {
	_, corpus, _ := loadUserMessageRuntimeV2Preview1Corpus(t)
	fixture := corpus.Fixture
	assertStringSetOrderV2Preview1(t, "router_decisions", fixture.RouterDecisions, []string{"direct_response", "failed", "model_unknown"})
	if fixture.ExecutableToolRegistry == nil || len(fixture.ExecutableToolRegistry) != 0 {
		t.Fatalf("executable_tool_registry=%v, want non-nil empty array", fixture.ExecutableToolRegistry)
	}
	assertStringSetOrderV2Preview1(t, "input_states", fixture.InputStates, []string{"pending", "claimed", "running", "retry_wait", "recovery_pending", "resolved", "dead"})
	assertStringSetOrderV2Preview1(t, "turn_states", fixture.TurnStates, []string{"created", "running", "completed", "failed"})
	assertStringSetOrderV2Preview1(t, "run_states", fixture.RunStates, []string{"created", "running", "completed", "failed", "recovery_pending"})
	assertStringSetOrderV2Preview1(t, "model_states", fixture.ModelStates, []string{"reserved", "dispatched", "completed", "failed", "model_unknown"})
	assertStringSetOrderV2Preview1(t, "output_states", fixture.OutputStates, []string{"open", "completed", "failed"})
	assertStringSetOrderV2Preview1(t, "preview_object_names", fixture.PreviewObjectNames, []string{
		"agent.session_user_message_turn",
		"agent.session_user_message_turn_context",
		"agent.session_user_message_run",
		"agent.session_user_message_model_receipt",
		"agent.session_user_message_output_receipt",
		"agent.session_user_message_output_projection",
		"agent.session_user_message_upgrade_ledger",
	})
	assertStringSetOrderV2Preview1(t, "output_schemas", fixture.OutputSchemas, []string{"session.turn.direct_response.card.v1", "session.turn.failure.card.v1"})
	assertStringSetOrderV2Preview1(t, "event_types", fixture.EventTypes, []string{"session.turn.completed", "session.turn.failed", "session.turn.recovery_pending"})
	assertStringSetOrderV2Preview1(t, "blocker_priority", fixture.BlockerPriority, []string{
		"session_not_active", "source_type_not_user_message", "not_first_input", "provenance_mismatch",
		"message_digest_mismatch", "input_not_pristine", "lease_not_idle",
		"accepted_event_unverifiable", "legacy_upgrade_incomplete",
	})
	assertStringSetOrderV2Preview1(t, "activation_requirements", fixture.ActivationRequirements, []string{
		"DORA_ENV=local", "loopback_listener", "local_dedicated_database", "profile_exact_match", "migration_generation_match", "legacy_blocker_count_zero",
	})
	for _, forbidden := range []string{"quarantined", "analyze_materials", "plan_creation_spec"} {
		if containsUserMessageRuntimeV2Preview1(fixture.InputStates, forbidden) || containsUserMessageRuntimeV2Preview1(fixture.RouterDecisions, forbidden) || containsUserMessageRuntimeV2Preview1(fixture.ExecutableToolRegistry, forbidden) {
			t.Fatalf("forbidden runtime member %q entered fixture exact sets", forbidden)
		}
	}
}

func TestUserMessageRuntimeV2Preview1ContextContractDigest(t *testing.T) {
	_, corpus, _ := loadUserMessageRuntimeV2Preview1Corpus(t)
	wantFields := []string{
		"schema_version", "session_id", "input_id", "message_id", "turn_id", "user_id", "project_id",
		"message_cutoff_seq", "message_content_digest", "skill_snapshot_ref", "skill_snapshot_digest",
		"prompt_ref", "prompt_digest", "tool_registry_ref", "tool_registry_digest",
		"runtime_policy_ref", "runtime_policy_digest", "model_route_ref", "model_route_digest",
		"budget_ref", "budget_digest", "access_scope_ref", "access_scope_digest", "context_digest",
	}
	if !reflect.DeepEqual(corpus.Fixture.TurnContextFields, wantFields) {
		t.Fatalf("turn_context_fields=%v want=%v", corpus.Fixture.TurnContextFields, wantFields)
	}
	hasher := sha256.New()
	_, _ = hasher.Write([]byte("user_message.turn_context.fields.v2preview1\x00"))
	for _, field := range wantFields {
		_, _ = hasher.Write([]byte(field))
		_, _ = hasher.Write([]byte{0})
	}
	actual := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if corpus.Fixture.TurnContextContractDigest != actual {
		t.Fatalf("turn_context_contract_digest=%q want=%q", corpus.Fixture.TurnContextContractDigest, actual)
	}
	if len(wantFields) == 58 {
		t.Fatal("preview context must not alias the 58-field production candidate")
	}
}

func TestUserMessageRuntimeV2Preview1NegativeCapabilities(t *testing.T) {
	_, corpus, _ := loadUserMessageRuntimeV2Preview1Corpus(t)
	vectors := make(map[string]userMessageRuntimeV2Preview1ContractCase, len(corpus.Vectors))
	for _, vector := range corpus.Vectors {
		vectors[vector.ID] = vector
	}
	wantForbidden := map[string][]string{
		"UM-N01-source-retype":               {"source_type_update"},
		"UM-N02-preview-fact-fabrication":    {"creation_spec_preview_run", "synthetic_terminal"},
		"UM-N03-later-source-skip":           {"source_filter_before_head"},
		"UM-N04-dual-processor":              {"dual_claim"},
		"UM-N05-tool-call":                   {"tool_execution"},
		"UM-N06-analyze-materials":           {"analyze_materials"},
		"UM-N07-plan-creation-spec":          {"plan_creation_spec"},
		"UM-N08-assistant-history":           {"assistant_message"},
		"UM-N09-model-unknown-resend":        {"model_resend", "failover"},
		"UM-N10-stale-fence":                 {"event_increment", "lease_release"},
		"UM-N11-unverifiable-accepted-event": {"marker_fabrication", "event_fabrication"},
		"UM-N12-shared-production-enable":    {"processor_start"},
		"UM-N13-side-effects":                {"approval", "billing", "job", "worker", "business_draft"},
		"UM-N14-production-schema-alias":     {"agent.session_turn", "agent.session_run", "session_turn_context", "agent.session_event_marker"},
		"UM-N15-production-corpus-reuse":     {"enqueue_input_v1", "dora.session_turn_context.v1", "quarantined"},
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
}

func TestUserMessageRuntimeV2Preview1StrictJSON(t *testing.T) {
	root := approvalRepoRootV1(t)
	for _, relative := range []string{
		filepath.Join(userMessageRuntimeV2Preview1CorpusDir, "manifest.json"),
		filepath.Join(userMessageRuntimeV2Preview1CorpusDir, "user_message_runtime_v2preview1.json"),
	} {
		raw, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		var target any
		if strings.HasSuffix(relative, "manifest.json") {
			target = &userMessageRuntimeV2Preview1Manifest{}
		} else {
			target = &userMessageRuntimeV2Preview1Corpus{}
		}
		cases := map[string][]byte{
			"unknown":   []byte(strings.Replace(string(raw), "{", "{\"future\":true,", 1)),
			"duplicate": []byte(strings.Replace(string(raw), "{", "{\"schema_version\":\"duplicate\",", 1)),
			"trailing":  append(append([]byte(nil), raw...), []byte(`{}`)...),
		}
		for name, candidate := range cases {
			t.Run(filepath.Base(relative)+"/"+name, func(t *testing.T) {
				if err := messageSetStrictDecodeV1(candidate, target); err == nil {
					t.Fatalf("strict decoder accepted %s", name)
				}
			})
		}
	}
}

func loadUserMessageRuntimeV2Preview1Corpus(t *testing.T) (userMessageRuntimeV2Preview1Manifest, userMessageRuntimeV2Preview1Corpus, []byte) {
	t.Helper()
	root := approvalRepoRootV1(t)
	manifestRaw, err := os.ReadFile(filepath.Join(root, userMessageRuntimeV2Preview1CorpusDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest userMessageRuntimeV2Preview1Manifest
	if err := messageSetStrictDecodeV1(manifestRaw, &manifest); err != nil {
		t.Fatalf("decode corpus manifest: %v", err)
	}
	corpusRaw, err := os.ReadFile(filepath.Join(root, userMessageRuntimeV2Preview1CorpusDir, "user_message_runtime_v2preview1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var corpus userMessageRuntimeV2Preview1Corpus
	if err := messageSetStrictDecodeV1(corpusRaw, &corpus); err != nil {
		t.Fatalf("decode corpus: %v", err)
	}
	return manifest, corpus, corpusRaw
}

func userMessageRuntimeV2Preview1VectorIDs() []string {
	return []string{
		"UM-P01-direct-response", "UM-P02-skill-does-not-expand-tools", "UM-P03-empty-prompt-zero-facts",
		"UM-P04-command-replay-single-facts", "UM-P05-session-hol", "UM-P06-cross-session-parallel",
		"UM-P07-frozen-output-replay", "UM-P08-fence-takeover", "UM-P09-lost-wake-scanner",
		"UM-P10-terminal-response-loss", "UM-P11-preview-drain-handoff", "UM-P12-local-activation",
		"UM-N01-source-retype", "UM-N02-preview-fact-fabrication", "UM-N03-later-source-skip",
		"UM-N04-dual-processor", "UM-N05-tool-call", "UM-N06-analyze-materials",
		"UM-N07-plan-creation-spec", "UM-N08-assistant-history", "UM-N09-model-unknown-resend",
		"UM-N10-stale-fence", "UM-N11-unverifiable-accepted-event", "UM-N12-shared-production-enable",
		"UM-N13-side-effects", "UM-N14-production-schema-alias", "UM-N15-production-corpus-reuse",
	}
}

func keysUserMessageRuntimeV2Preview1(values map[string]struct{}) []string {
	want := userMessageRuntimeV2Preview1VectorIDs()
	got := make([]string, 0, len(values))
	for _, key := range want {
		if _, ok := values[key]; ok {
			got = append(got, key)
		}
	}
	for key := range values {
		if !containsUserMessageRuntimeV2Preview1(want, key) {
			got = append(got, fmt.Sprintf("unexpected:%s", key))
		}
	}
	return got
}

func assertStringSetOrderV2Preview1(t *testing.T, name string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s=%v want=%v", name, got, want)
	}
}

func containsUserMessageRuntimeV2Preview1(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
