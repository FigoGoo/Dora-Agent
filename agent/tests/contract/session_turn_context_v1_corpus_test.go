// Package contract_test 只承载未 Approved 的 Immutable Turn Context canonical 候选，不提供生产表、Store、Runner 或 Activation。
package contract_test

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	turnContextCorpusPathV1   = "testdata/w2_r02_turn_context/session_turn_context_v1.json"
	turnContextSchemaV1       = "session_turn_context.v1"
	turnContextDigestDomainV1 = "dora.session_turn_context.v1"
)

type turnContextCorpusV1 struct {
	SchemaVersion string                 `json:"schema_version"`
	ExactSets     turnContextExactSetsV1 `json:"exact_sets"`
	Fixtures      []turnContextFixtureV1 `json:"fixtures"`
	Cases         []turnContextCaseV1    `json:"cases"`
}

type turnContextExactSetsV1 struct {
	Decisions                       []string `json:"decisions"`
	TurnKinds                       []string `json:"turn_kinds"`
	AuthorityOwners                 []string `json:"authority_owners"`
	AuthorityRefTypes               []string `json:"authority_ref_types"`
	OwnerTokens                     []string `json:"owner_tokens"`
	HistorySummarySchemaVersions    []string `json:"history_summary_schema_versions"`
	HistorySummaryAlgorithmVersions []string `json:"history_summary_algorithm_versions"`
	ReasonCodes                     []string `json:"reason_codes"`
}

// turnContextCanonicalV1 的字段顺序与设计文档第 7 节 58 字段 exact-set 完全一致。
// 条件字段使用指针，确保缺失语义编码为显式 JSON null，而不是省略字段。
type turnContextCanonicalV1 struct {
	SchemaVersion                  string  `json:"schema_version"`
	TurnID                         string  `json:"turn_id"`
	SessionID                      string  `json:"session_id"`
	InputID                        string  `json:"input_id"`
	TurnKind                       string  `json:"turn_kind"`
	InputSourceDigest              string  `json:"input_source_digest"`
	AuthorityOwner                 string  `json:"authority_owner"`
	AuthorityRefType               string  `json:"authority_ref_type"`
	AuthorityRefID                 string  `json:"authority_ref_id"`
	AuthorityRefDigest             string  `json:"authority_ref_digest"`
	MessageSetSchemaVersion        string  `json:"message_set_schema_version"`
	MessageCutoffSeq               int64   `json:"message_cutoff_seq"`
	EventCutoffSeq                 int64   `json:"event_cutoff_seq"`
	MessageCount                   int     `json:"message_count"`
	MessageSetDigest               string  `json:"message_set_digest"`
	HistorySummaryPresent          bool    `json:"history_summary_present"`
	HistorySummarySchemaVersion    *string `json:"history_summary_schema_version"`
	HistorySummaryAlgorithmVersion *string `json:"history_summary_algorithm_version"`
	HistorySummaryOwner            *string `json:"history_summary_owner"`
	HistorySummaryRef              *string `json:"history_summary_ref"`
	HistorySummaryDigest           *string `json:"history_summary_digest"`
	HistorySummaryMessageCutoffSeq *int64  `json:"history_summary_message_cutoff_seq"`
	HistorySummaryEventCutoffSeq   *int64  `json:"history_summary_event_cutoff_seq"`
	SkillSnapshotSchemaVersion     string  `json:"skill_snapshot_schema_version"`
	SkillSnapshotKind              string  `json:"skill_snapshot_kind"`
	SkillCount                     int     `json:"skill_count"`
	SkillSnapshotOwner             string  `json:"skill_snapshot_owner"`
	SkillSnapshotRef               string  `json:"skill_snapshot_ref"`
	SkillSnapshotDigest            string  `json:"skill_snapshot_digest"`
	PromptBundleOwner              *string `json:"prompt_bundle_owner"`
	PromptBundleRef                *string `json:"prompt_bundle_ref"`
	PromptBundleDigest             *string `json:"prompt_bundle_digest"`
	ToolRegistryOwner              string  `json:"tool_registry_owner"`
	ToolRegistryRef                string  `json:"tool_registry_ref"`
	ToolRegistryDigest             string  `json:"tool_registry_digest"`
	RuntimePolicyOwner             string  `json:"runtime_policy_owner"`
	RuntimePolicyRef               string  `json:"runtime_policy_ref"`
	RuntimePolicyDigest            string  `json:"runtime_policy_digest"`
	ModelRouteOwner                *string `json:"model_route_owner"`
	ModelRouteRef                  *string `json:"model_route_ref"`
	ModelRouteDigest               *string `json:"model_route_digest"`
	BudgetSnapshotOwner            string  `json:"budget_snapshot_owner"`
	BudgetSnapshotRef              string  `json:"budget_snapshot_ref"`
	BudgetSnapshotDigest           string  `json:"budget_snapshot_digest"`
	AccessScopeOwner               string  `json:"access_scope_owner"`
	AccessScopeRef                 string  `json:"access_scope_ref"`
	AccessScopeDigest              string  `json:"access_scope_digest"`
	ContinuationPresent            bool    `json:"continuation_present"`
	ParentToolReceiptOwner         *string `json:"parent_tool_receipt_owner"`
	ParentToolReceiptRef           *string `json:"parent_tool_receipt_ref"`
	ParentToolReceiptDigest        *string `json:"parent_tool_receipt_digest"`
	ParentRequestSemanticDigest    *string `json:"parent_request_semantic_digest"`
	ApprovalOwner                  *string `json:"approval_owner"`
	ApprovalRef                    *string `json:"approval_ref"`
	ApprovalDigest                 *string `json:"approval_digest"`
	PinnedToolOwner                *string `json:"pinned_tool_owner"`
	PinnedToolRef                  *string `json:"pinned_tool_ref"`
	PinnedToolDigest               *string `json:"pinned_tool_digest"`
}

type turnContextFixtureV1 struct {
	FixtureID            string                       `json:"fixture_id"`
	MessageSetFixtureID  string                       `json:"message_set_fixture_id"`
	Canonical            turnContextCanonicalV1       `json:"canonical"`
	StoredContextDigest  string                       `json:"stored_context_digest"`
	LockedMessageCounter turnContextSequenceCounterV1 `json:"locked_message_counter"`
	LockedEventCounter   turnContextEventCounterV1    `json:"locked_event_counter"`
	InputBinding         turnContextInputBindingV1    `json:"input_binding"`
	ResolvedRefs         []turnContextResolvedRefV1   `json:"resolved_refs"`
	RuntimeMetadata      turnContextRuntimeMetadataV1 `json:"runtime_metadata"`
}

type turnContextSequenceCounterV1 struct {
	SessionID      string `json:"session_id"`
	LastMessageSeq int64  `json:"last_message_seq"`
}

type turnContextEventCounterV1 struct {
	SessionID    string `json:"session_id"`
	LastEventSeq int64  `json:"last_event_seq"`
}

type turnContextInputBindingV1 struct {
	TurnID             string `json:"turn_id"`
	SessionID          string `json:"session_id"`
	InputID            string `json:"input_id"`
	TurnKind           string `json:"turn_kind"`
	InputSourceDigest  string `json:"input_source_digest"`
	AuthorityOwner     string `json:"authority_owner"`
	AuthorityRefType   string `json:"authority_ref_type"`
	AuthorityRefID     string `json:"authority_ref_id"`
	AuthorityRefDigest string `json:"authority_ref_digest"`
}

type turnContextResolvedRefV1 struct {
	Owner  string `json:"owner"`
	Ref    string `json:"ref"`
	Digest string `json:"digest"`
}

type turnContextRuntimeMetadataV1 struct {
	TraceID                 string `json:"trace_id"`
	RequestID               string `json:"request_id"`
	ProcessorInstance       string `json:"processor_instance"`
	LeaseOwner              string `json:"lease_owner"`
	LeaseFence              int64  `json:"lease_fence"`
	Attempt                 int64  `json:"attempt"`
	CheckpointRef           string `json:"checkpoint_ref"`
	RuntimeReadAt           string `json:"runtime_read_at"`
	LatestMessageSeq        int64  `json:"latest_message_seq"`
	LatestEventSeq          int64  `json:"latest_event_seq"`
	LatestConfigGeneration  string `json:"latest_config_generation"`
	LatestPromptBundleRef   string `json:"latest_prompt_bundle_ref"`
	LatestToolRegistryRef   string `json:"latest_tool_registry_ref"`
	LatestRuntimePolicyRef  string `json:"latest_runtime_policy_ref"`
	LatestModelRouteRef     string `json:"latest_model_route_ref"`
	LatestBudgetSnapshotRef string `json:"latest_budget_snapshot_ref"`
	LatestAccessScopeRef    string `json:"latest_access_scope_ref"`
}

type turnContextCaseV1 struct {
	ID          string                `json:"id"`
	FromFixture string                `json:"from_fixture"`
	Mutations   []string              `json:"mutations"`
	Expected    turnContextExpectedV1 `json:"expected"`
}

type turnContextExpectedV1 struct {
	Decision         string   `json:"decision"`
	ReasonCodes      []string `json:"reason_codes"`
	ContextDigest    string   `json:"context_digest"`
	MessageSetDigest string   `json:"message_set_digest"`
}

type turnContextEvaluationV1 struct {
	Decision         string
	ReasonCodes      []string
	ContextDigest    string
	MessageSetDigest string
}

func TestSessionTurnContextV1Corpus(t *testing.T) {
	corpus := loadTurnContextCorpusV1(t)
	messageSets := loadMessageSetFixtureMapV1(t)
	fixtures := make(map[string]turnContextFixtureV1, len(corpus.Fixtures))
	for _, fixture := range corpus.Fixtures {
		if _, exists := fixtures[fixture.FixtureID]; exists {
			t.Fatalf("重复 Context fixture=%s", fixture.FixtureID)
		}
		fixtures[fixture.FixtureID] = fixture
	}
	seen := make(map[string]struct{}, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		testCase := testCase
		t.Run(testCase.ID, func(t *testing.T) {
			if _, exists := seen[testCase.ID]; exists {
				t.Fatalf("重复 Context case=%s", testCase.ID)
			}
			seen[testCase.ID] = struct{}{}
			fixture, exists := fixtures[testCase.FromFixture]
			if !exists {
				t.Fatalf("未知 Context fixture=%s", testCase.FromFixture)
			}
			got := evaluateTurnContextCaseV1(fixture, testCase.Mutations, messageSets)
			want := turnContextEvaluationV1{
				Decision: testCase.Expected.Decision, ReasonCodes: testCase.Expected.ReasonCodes,
				ContextDigest: testCase.Expected.ContextDigest, MessageSetDigest: testCase.Expected.MessageSetDigest,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Context evaluation=%+v want=%+v", got, want)
			}
		})
	}
}

func TestSessionTurnContextV1GoldenDigests(t *testing.T) {
	messageSets := loadMessageSetFixtureMapV1(t)
	for _, fixture := range loadTurnContextCorpusV1(t).Fixtures {
		evaluation := evaluateTurnContextFixtureV1(fixture, messageSets, false)
		if evaluation.Decision != "valid" {
			t.Errorf("fixture=%s reasons=%v", fixture.FixtureID, evaluation.ReasonCodes)
			continue
		}
		if evaluation.ContextDigest != fixture.StoredContextDigest {
			t.Errorf("fixture=%s golden=%s stored=%s", fixture.FixtureID, evaluation.ContextDigest, fixture.StoredContextDigest)
		}
	}
}

func TestSessionTurnContextV1ExactSets(t *testing.T) {
	got := loadTurnContextCorpusV1(t).ExactSets
	want := turnContextExactSetsV1{
		Decisions: []string{"valid", "invalid"},
		TurnKinds: []string{"chat", "approval_continuation", "batch_explanation"},
		AuthorityOwners: []string{
			"business.authorization", "agent.approval_store", "agent.operation_store", "agent.legacy_authority",
		},
		AuthorityRefTypes: []string{"authenticated_user", "approval_decision", "approval_invalidation", "batch_terminal_event", "legacy_ensure_receipt_attestation"},
		OwnerTokens: []string{
			"agent.history_materialization", "agent.session_skill_snapshot", "agent.prompt_registry", "agent.tool_registry",
			"agent.runner_policy", "agent.model_gateway", "agent.budget_policy", "agent.access_snapshot",
			"agent.tool_receipt", "agent.approval_store",
		},
		HistorySummarySchemaVersions:    []string{"session_history_summary.v1"},
		HistorySummaryAlgorithmVersions: []string{"deterministic_summary.v1"},
		ReasonCodes: []string{
			"TURN_CONTEXT_SCHEMA_INVALID", "TURN_CONTEXT_IDENTITY_INVALID", "TURN_CONTEXT_KIND_INVALID",
			"TURN_CONTEXT_CONDITION_INVALID", "TURN_CONTEXT_CONTINUATION_INVALID", "TURN_CONTEXT_DIGEST_FORMAT_INVALID",
			"TURN_CONTEXT_AUTHORITY_INVALID",
			"TURN_CONTEXT_CUTOFF_INVALID", "TURN_CONTEXT_MESSAGE_SET_INVALID", "TURN_CONTEXT_SUMMARY_INVALID",
			"TURN_CONTEXT_SKILL_INVALID", "TURN_CONTEXT_FROZEN_REF_INVALID", "TURN_CONTEXT_DIGEST_MISMATCH",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Context exact sets=%+v want=%+v", got, want)
	}
}

func TestSessionTurnContextV1ReasonPriority(t *testing.T) {
	messageSets := loadMessageSetFixtureMapV1(t)
	tests := []struct {
		name       string
		fixtureID  string
		mutations  []string
		wantReason string
	}{
		{name: "identity_before_kind", fixtureID: "turn.chat.single", mutations: []string{"input_binding_mismatch", "turn_kind_unknown"}, wantReason: "TURN_CONTEXT_IDENTITY_INVALID"},
		{name: "identity_before_condition", fixtureID: "turn.chat.single", mutations: []string{"input_binding_mismatch", "chat_prompt_partial"}, wantReason: "TURN_CONTEXT_IDENTITY_INVALID"},
		{name: "kind_before_condition", fixtureID: "turn.chat.single", mutations: []string{"turn_kind_unknown", "chat_prompt_partial"}, wantReason: "TURN_CONTEXT_KIND_INVALID"},
		{name: "condition_before_continuation_and_digest", fixtureID: "turn.approval.continuation", mutations: []string{"approval_model_forbidden", "continuation_partial", "common_digest_invalid"}, wantReason: "TURN_CONTEXT_CONDITION_INVALID"},
		{name: "continuation_before_digest", fixtureID: "turn.approval.continuation", mutations: []string{"continuation_partial", "common_digest_invalid"}, wantReason: "TURN_CONTEXT_CONTINUATION_INVALID"},
		{name: "digest_before_authority", fixtureID: "turn.chat.single", mutations: []string{"common_digest_invalid", "authority_ref_mismatch"}, wantReason: "TURN_CONTEXT_DIGEST_FORMAT_INVALID"},
		{name: "authority_before_cutoff", fixtureID: "turn.chat.tool_pair", mutations: []string{"authority_ref_mismatch", "cutoff_after_locked_counter"}, wantReason: "TURN_CONTEXT_AUTHORITY_INVALID"},
		{name: "cutoff_before_message_set", fixtureID: "turn.chat.tool_pair", mutations: []string{"cutoff_after_locked_counter", "message_set_schema_unknown"}, wantReason: "TURN_CONTEXT_CUTOFF_INVALID"},
		{name: "message_set_before_summary", fixtureID: "turn.batch.summary", mutations: []string{"message_set_schema_unknown", "summary_owner_invalid"}, wantReason: "TURN_CONTEXT_MESSAGE_SET_INVALID"},
		{name: "summary_before_skill", fixtureID: "turn.batch.summary", mutations: []string{"summary_owner_invalid", "common_owner_invalid"}, wantReason: "TURN_CONTEXT_SUMMARY_INVALID"},
		{name: "skill_before_frozen_ref", fixtureID: "turn.chat.single", mutations: []string{"common_owner_invalid", "common_ref_invalid"}, wantReason: "TURN_CONTEXT_SKILL_INVALID"},
		{name: "frozen_ref_before_context_digest", fixtureID: "turn.chat.single", mutations: []string{"common_ref_invalid", "context_digest_tamper"}, wantReason: "TURN_CONTEXT_FROZEN_REF_INVALID"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evaluateTurnContextCaseV1(findTurnContextFixtureV1(t, test.fixtureID), test.mutations, messageSets)
			if !reflect.DeepEqual(got.ReasonCodes, []string{test.wantReason}) {
				t.Fatalf("reason=%v want=%s", got.ReasonCodes, test.wantReason)
			}
		})
	}
}

func TestSessionTurnContextV1ConditionalGroups(t *testing.T) {
	corpus := loadTurnContextCorpusV1(t)
	for _, fixture := range corpus.Fixtures {
		if reason := validateTurnContextConditionGroupsV1(fixture.Canonical); reason != "" {
			t.Fatalf("fixture=%s condition reason=%s", fixture.FixtureID, reason)
		}
	}
}

func TestSessionTurnContextV1FrozenReplay(t *testing.T) {
	fixture := findTurnContextFixtureV1(t, "turn.chat.tool_pair")
	messageSets := loadMessageSetFixtureMapV1(t)
	base := evaluateTurnContextFixtureV1(fixture, messageSets, true)
	for _, mutation := range []string{
		"config_update_after_freeze", "retry_attempt_changed", "resume_checkpoint_changed", "takeover_owner_changed",
		"later_message_appended", "later_event_appended", "runtime_metadata_changed",
	} {
		got := evaluateTurnContextCaseV1(fixture, []string{mutation}, messageSets)
		if !reflect.DeepEqual(got, base) {
			t.Fatalf("mutation=%s changed frozen Context got=%+v want=%+v", mutation, got, base)
		}
	}
}

func TestSessionTurnContextV1StrictJSON(t *testing.T) {
	var canonical turnContextCanonicalV1
	if err := messageSetStrictDecodeV1([]byte(`{"schema_version":"x","schema_version":"y"}`), &canonical); err == nil {
		t.Fatal("Context 未拒绝重复字段")
	}
	if err := messageSetStrictDecodeV1([]byte(`{"schema_version":"x","future":true}`), &canonical); err == nil {
		t.Fatal("Context 未拒绝未知字段")
	}
	if err := messageSetInspectJSONV1([]byte(`{"prompt_bundle_owner":null,"model_route_owner":null}`)); err != nil {
		t.Fatalf("Context 显式 null 被错误拒绝: %v", err)
	}
	fixture := findTurnContextFixtureV1(t, "turn.approval.continuation")
	raw, err := json.Marshal(fixture.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"prompt_bundle_owner", "model_route_owner", "history_summary_owner"} {
		if !bytes.Contains(raw, []byte(`"`+field+`":null`)) {
			t.Fatalf("条件字段未显式编码 null field=%s canonical=%s", field, raw)
		}
	}
	missing := bytes.Replace(raw, []byte(`"history_summary_owner":null,`), nil, 1)
	fields, err := turnContextJSONObjectFieldNamesV1(missing)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(fields, turnContextCanonicalFieldNamesV1()) {
		t.Fatal("Context canonical 未拒绝缺失字段")
	}
}

func loadTurnContextCorpusV1(t *testing.T) turnContextCorpusV1 {
	t.Helper()
	raw, err := w2R02TurnContextFS.ReadFile(turnContextCorpusPathV1)
	if err != nil {
		t.Fatal(err)
	}
	var corpus turnContextCorpusV1
	if err := messageSetStrictDecodeV1(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	var rawCorpus struct {
		Fixtures []struct {
			FixtureID string          `json:"fixture_id"`
			Canonical json.RawMessage `json:"canonical"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &rawCorpus); err != nil {
		t.Fatal(err)
	}
	wantFields := turnContextCanonicalFieldNamesV1()
	for _, fixture := range rawCorpus.Fixtures {
		gotFields, err := turnContextJSONObjectFieldNamesV1(fixture.Canonical)
		if err != nil {
			t.Fatalf("fixture=%s canonical fields: %v", fixture.FixtureID, err)
		}
		if !reflect.DeepEqual(gotFields, wantFields) {
			t.Fatalf("fixture=%s canonical fields=%v want=%v", fixture.FixtureID, gotFields, wantFields)
		}
	}
	if corpus.SchemaVersion != "session_turn_context_v1_corpus.v1" {
		t.Fatalf("Context corpus schema=%q", corpus.SchemaVersion)
	}
	return corpus
}

func turnContextCanonicalFieldNamesV1() []string {
	typeOf := reflect.TypeOf(turnContextCanonicalV1{})
	fields := make([]string, 0, typeOf.NumField())
	for index := 0; index < typeOf.NumField(); index++ {
		fields = append(fields, typeOf.Field(index).Tag.Get("json"))
	}
	return fields
}

func turnContextJSONObjectFieldNamesV1(raw []byte) ([]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("object start: %w", err)
	}
	if token != json.Delim('{') {
		return nil, fmt.Errorf("object start token=%v", token)
	}
	fields := make([]string, 0, 58)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, err
		}
		field, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("field token=%v", token)
		}
		fields = append(fields, field)
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
	}
	if token, err = decoder.Token(); err != nil {
		return nil, fmt.Errorf("object end: %w", err)
	}
	if token != json.Delim('}') {
		return nil, fmt.Errorf("object end token=%v", token)
	}
	return fields, nil
}

func loadMessageSetFixtureMapV1(t *testing.T) map[string]messageSetFixtureV1 {
	t.Helper()
	result := make(map[string]messageSetFixtureV1)
	for _, fixture := range loadMessageSetCorpusV1(t).Fixtures {
		result[fixture.FixtureID] = fixture
	}
	return result
}

func findTurnContextFixtureV1(t *testing.T, fixtureID string) turnContextFixtureV1 {
	t.Helper()
	for _, fixture := range loadTurnContextCorpusV1(t).Fixtures {
		if fixture.FixtureID == fixtureID {
			return fixture
		}
	}
	t.Fatalf("Context fixture not found=%s", fixtureID)
	return turnContextFixtureV1{}
}

func evaluateTurnContextCaseV1(fixture turnContextFixtureV1, mutations []string, messageSets map[string]messageSetFixtureV1) turnContextEvaluationV1 {
	fixture = cloneTurnContextFixtureV1(fixture)
	for _, mutation := range mutations {
		applyTurnContextMutationV1(&fixture, mutation)
	}
	return evaluateTurnContextFixtureV1(fixture, messageSets, true)
}

func evaluateTurnContextFixtureV1(fixture turnContextFixtureV1, messageSets map[string]messageSetFixtureV1, verifyStored bool) turnContextEvaluationV1 {
	canonical := fixture.Canonical
	if canonical.SchemaVersion != turnContextSchemaV1 {
		return invalidTurnContextV1("TURN_CONTEXT_SCHEMA_INVALID")
	}
	if !turnContextIdentityValidV1(canonical) {
		return invalidTurnContextV1("TURN_CONTEXT_IDENTITY_INVALID")
	}
	if !turnContextIdentityBindingValidV1(canonical, fixture.InputBinding) {
		return invalidTurnContextV1("TURN_CONTEXT_IDENTITY_INVALID")
	}
	if !turnContextKindValidV1(canonical.TurnKind) || canonical.TurnKind != fixture.InputBinding.TurnKind {
		return invalidTurnContextV1("TURN_CONTEXT_KIND_INVALID")
	}
	if !turnContextModelConditionGroupsValidV1(canonical) {
		return invalidTurnContextV1("TURN_CONTEXT_CONDITION_INVALID")
	}
	if !turnContextContinuationGroupValidV1(canonical) {
		return invalidTurnContextV1("TURN_CONTEXT_CONTINUATION_INVALID")
	}
	if !turnContextDigestFormatsValidV1(canonical) {
		return invalidTurnContextV1("TURN_CONTEXT_DIGEST_FORMAT_INVALID")
	}
	if !turnContextAuthorityBindingValidV1(canonical, fixture.InputBinding) {
		return invalidTurnContextV1("TURN_CONTEXT_AUTHORITY_INVALID")
	}
	if canonical.MessageCutoffSeq < 0 || canonical.EventCutoffSeq < 0 || canonical.MessageCount < 0 ||
		fixture.LockedMessageCounter.SessionID != canonical.SessionID || fixture.LockedEventCounter.SessionID != canonical.SessionID ||
		canonical.MessageCutoffSeq > fixture.LockedMessageCounter.LastMessageSeq ||
		canonical.EventCutoffSeq > fixture.LockedEventCounter.LastEventSeq {
		return invalidTurnContextV1("TURN_CONTEXT_CUTOFF_INVALID")
	}
	messageFixture, exists := messageSets[fixture.MessageSetFixtureID]
	if !exists || canonical.MessageSetSchemaVersion != messageSetSchemaV1 || messageFixture.SessionID != canonical.SessionID {
		return invalidTurnContextV1("TURN_CONTEXT_MESSAGE_SET_INVALID")
	}
	messageCanonical, messageReasons := buildTurnContextMessageSetCanonicalV1(messageFixture, canonical.MessageCutoffSeq)
	if len(messageReasons) != 0 {
		return invalidTurnContextV1("TURN_CONTEXT_MESSAGE_SET_INVALID")
	}
	messageDigest, err := messageSetSemanticDigestV1(messageCanonical)
	if err != nil || messageCanonical.SessionID != canonical.SessionID || int64(canonical.MessageCount) != canonical.MessageCutoffSeq ||
		canonical.MessageCount != len(messageCanonical.Messages) ||
		canonical.MessageCutoffSeq != messageCanonical.MessageCutoffSeq ||
		subtle.ConstantTimeCompare([]byte(messageDigest), []byte(canonical.MessageSetDigest)) != 1 {
		return invalidTurnContextV1("TURN_CONTEXT_MESSAGE_SET_INVALID")
	}
	if !turnContextSummaryValidV1(canonical) {
		return invalidTurnContextV1("TURN_CONTEXT_SUMMARY_INVALID")
	}
	if !turnContextSkillValidV1(canonical) {
		return invalidTurnContextV1("TURN_CONTEXT_SKILL_INVALID")
	}
	triplets, reason := turnContextFrozenTripletsV1(canonical)
	if reason != "" {
		return invalidTurnContextV1("TURN_CONTEXT_FROZEN_REF_INVALID")
	}
	if !turnContextResolvedRefsValidV1(triplets, fixture.ResolvedRefs) {
		return invalidTurnContextV1("TURN_CONTEXT_FROZEN_REF_INVALID")
	}
	contextDigest, err := turnContextSemanticDigestV1(canonical)
	if err != nil {
		return invalidTurnContextV1("TURN_CONTEXT_SCHEMA_INVALID")
	}
	if verifyStored && (!messageSetDigestPatternV1.MatchString(fixture.StoredContextDigest) ||
		subtle.ConstantTimeCompare([]byte(contextDigest), []byte(fixture.StoredContextDigest)) != 1) {
		return invalidTurnContextV1("TURN_CONTEXT_DIGEST_MISMATCH")
	}
	return turnContextEvaluationV1{Decision: "valid", ReasonCodes: []string{}, ContextDigest: contextDigest, MessageSetDigest: messageDigest}
}

func buildTurnContextMessageSetCanonicalV1(fixture messageSetFixtureV1, cutoff int64) (messageSetCanonicalV1, []string) {
	fixture.MessageCutoffSeq = cutoff
	return buildMessageSetCanonicalV1(fixture)
}

func invalidTurnContextV1(reason string) turnContextEvaluationV1 {
	return turnContextEvaluationV1{Decision: "invalid", ReasonCodes: []string{reason}}
}

func turnContextIdentityValidV1(value turnContextCanonicalV1) bool {
	return messageSetUUIDv7V1(value.TurnID) && messageSetUUIDv7V1(value.SessionID) && messageSetUUIDv7V1(value.InputID) &&
		messageSetUUIDv7V1(value.AuthorityRefID)
}

func turnContextKindValidV1(value string) bool {
	return value == "chat" || value == "batch_explanation" || value == "approval_continuation"
}

func validateTurnContextConditionGroupsV1(value turnContextCanonicalV1) string {
	if !turnContextModelConditionGroupsValidV1(value) {
		return "TURN_CONTEXT_CONDITION_INVALID"
	}
	if !turnContextContinuationGroupValidV1(value) {
		return "TURN_CONTEXT_CONTINUATION_INVALID"
	}
	return ""
}

func turnContextModelConditionGroupsValidV1(value turnContextCanonicalV1) bool {
	promptPresent := allStringPointersPresentV1(value.PromptBundleOwner, value.PromptBundleRef, value.PromptBundleDigest)
	promptAbsent := allStringPointersNilV1(value.PromptBundleOwner, value.PromptBundleRef, value.PromptBundleDigest)
	modelPresent := allStringPointersPresentV1(value.ModelRouteOwner, value.ModelRouteRef, value.ModelRouteDigest)
	modelAbsent := allStringPointersNilV1(value.ModelRouteOwner, value.ModelRouteRef, value.ModelRouteDigest)
	if value.TurnKind == "approval_continuation" {
		return promptAbsent && modelAbsent
	}
	return promptPresent && modelPresent
}

func turnContextContinuationGroupValidV1(value turnContextCanonicalV1) bool {
	continuationPresent := allStringPointersPresentV1(
		value.ParentToolReceiptOwner, value.ParentToolReceiptRef, value.ParentToolReceiptDigest, value.ParentRequestSemanticDigest,
		value.ApprovalOwner, value.ApprovalRef, value.ApprovalDigest, value.PinnedToolOwner, value.PinnedToolRef, value.PinnedToolDigest,
	)
	continuationAbsent := allStringPointersNilV1(
		value.ParentToolReceiptOwner, value.ParentToolReceiptRef, value.ParentToolReceiptDigest, value.ParentRequestSemanticDigest,
		value.ApprovalOwner, value.ApprovalRef, value.ApprovalDigest, value.PinnedToolOwner, value.PinnedToolRef, value.PinnedToolDigest,
	)
	if value.TurnKind == "approval_continuation" {
		return value.ContinuationPresent && continuationPresent
	}
	return !value.ContinuationPresent && continuationAbsent
}

func turnContextDigestFormatsValidV1(value turnContextCanonicalV1) bool {
	digests := []string{
		value.InputSourceDigest, value.AuthorityRefDigest, value.MessageSetDigest, value.SkillSnapshotDigest,
		value.ToolRegistryDigest, value.RuntimePolicyDigest, value.BudgetSnapshotDigest, value.AccessScopeDigest,
	}
	for _, pointer := range []*string{
		value.HistorySummaryDigest, value.PromptBundleDigest, value.ModelRouteDigest, value.ParentToolReceiptDigest,
		value.ParentRequestSemanticDigest, value.ApprovalDigest, value.PinnedToolDigest,
	} {
		if pointer != nil {
			digests = append(digests, *pointer)
		}
	}
	for _, digest := range digests {
		if !messageSetDigestPatternV1.MatchString(digest) {
			return false
		}
	}
	return true
}

func turnContextIdentityBindingValidV1(value turnContextCanonicalV1, binding turnContextInputBindingV1) bool {
	return value.TurnID == binding.TurnID && value.SessionID == binding.SessionID && value.InputID == binding.InputID &&
		value.InputSourceDigest == binding.InputSourceDigest
}

func turnContextAuthorityBindingValidV1(value turnContextCanonicalV1, binding turnContextInputBindingV1) bool {
	return value.AuthorityOwner == binding.AuthorityOwner && value.AuthorityRefType == binding.AuthorityRefType &&
		value.AuthorityRefID == binding.AuthorityRefID && value.AuthorityRefDigest == binding.AuthorityRefDigest &&
		turnContextAuthorityCombinationValidV1(value)
}

func turnContextAuthorityCombinationValidV1(value turnContextCanonicalV1) bool {
	switch value.AuthorityOwner {
	case "business.authorization":
		return value.AuthorityRefType == "authenticated_user" && value.TurnKind == "chat"
	case "agent.approval_store":
		return (value.AuthorityRefType == "approval_decision" || value.AuthorityRefType == "approval_invalidation") &&
			value.TurnKind == "approval_continuation"
	case "agent.operation_store":
		return value.AuthorityRefType == "batch_terminal_event" && value.TurnKind == "batch_explanation"
	case "agent.legacy_authority":
		return value.AuthorityRefType == "legacy_ensure_receipt_attestation" && value.TurnKind == "chat"
	}
	return false
}

func turnContextSummaryValidV1(value turnContextCanonicalV1) bool {
	stringsPresent := allStringPointersPresentV1(
		value.HistorySummarySchemaVersion, value.HistorySummaryAlgorithmVersion, value.HistorySummaryOwner,
		value.HistorySummaryRef, value.HistorySummaryDigest,
	)
	stringsAbsent := allStringPointersNilV1(
		value.HistorySummarySchemaVersion, value.HistorySummaryAlgorithmVersion, value.HistorySummaryOwner,
		value.HistorySummaryRef, value.HistorySummaryDigest,
	)
	if !value.HistorySummaryPresent {
		return stringsAbsent && value.HistorySummaryMessageCutoffSeq == nil && value.HistorySummaryEventCutoffSeq == nil
	}
	return stringsPresent && value.HistorySummaryMessageCutoffSeq != nil && value.HistorySummaryEventCutoffSeq != nil &&
		*value.HistorySummarySchemaVersion == "session_history_summary.v1" &&
		*value.HistorySummaryAlgorithmVersion == "deterministic_summary.v1" &&
		*value.HistorySummaryOwner == "agent.history_materialization" && *value.HistorySummaryMessageCutoffSeq >= 0 &&
		*value.HistorySummaryEventCutoffSeq >= 0 && *value.HistorySummaryMessageCutoffSeq <= value.MessageCutoffSeq &&
		*value.HistorySummaryEventCutoffSeq <= value.EventCutoffSeq
}

func turnContextSkillValidV1(value turnContextCanonicalV1) bool {
	if value.SkillSnapshotSchemaVersion != "session_skill_snapshot.v1" || value.SkillSnapshotOwner != "agent.session_skill_snapshot" ||
		strings.TrimSpace(value.SkillSnapshotRef) == "" || value.SkillCount < 0 || value.SkillCount > 32 {
		return false
	}
	switch value.SkillSnapshotKind {
	case "empty":
		return value.SkillCount == 0
	case "published_refs":
		return value.SkillCount > 0
	default:
		return false
	}
}

func turnContextFrozenTripletsV1(value turnContextCanonicalV1) ([]turnContextResolvedRefV1, string) {
	triplets := []turnContextResolvedRefV1{
		{Owner: value.AuthorityOwner, Ref: value.AuthorityRefID, Digest: value.AuthorityRefDigest},
		{Owner: value.SkillSnapshotOwner, Ref: value.SkillSnapshotRef, Digest: value.SkillSnapshotDigest},
		{Owner: value.ToolRegistryOwner, Ref: value.ToolRegistryRef, Digest: value.ToolRegistryDigest},
		{Owner: value.RuntimePolicyOwner, Ref: value.RuntimePolicyRef, Digest: value.RuntimePolicyDigest},
		{Owner: value.BudgetSnapshotOwner, Ref: value.BudgetSnapshotRef, Digest: value.BudgetSnapshotDigest},
		{Owner: value.AccessScopeOwner, Ref: value.AccessScopeRef, Digest: value.AccessScopeDigest},
	}
	appendOptional := func(owner, ref, digest *string) bool {
		if allStringPointersNilV1(owner, ref, digest) {
			return true
		}
		if !allStringPointersPresentV1(owner, ref, digest) {
			return false
		}
		triplets = append(triplets, turnContextResolvedRefV1{Owner: *owner, Ref: *ref, Digest: *digest})
		return true
	}
	if !appendOptional(value.HistorySummaryOwner, value.HistorySummaryRef, value.HistorySummaryDigest) ||
		!appendOptional(value.PromptBundleOwner, value.PromptBundleRef, value.PromptBundleDigest) ||
		!appendOptional(value.ModelRouteOwner, value.ModelRouteRef, value.ModelRouteDigest) ||
		!appendOptional(value.ParentToolReceiptOwner, value.ParentToolReceiptRef, value.ParentToolReceiptDigest) ||
		!appendOptional(value.ApprovalOwner, value.ApprovalRef, value.ApprovalDigest) ||
		!appendOptional(value.PinnedToolOwner, value.PinnedToolRef, value.PinnedToolDigest) {
		return nil, "FROZEN_REF_GROUP_INVALID"
	}
	ownerByExpected := map[string]bool{
		"business.authorization": true, "agent.approval_store": true, "agent.operation_store": true, "agent.legacy_authority": true,
		"agent.session_skill_snapshot": true, "agent.history_materialization": true, "agent.prompt_registry": true,
		"agent.tool_registry": true, "agent.runner_policy": true, "agent.model_gateway": true,
		"agent.budget_policy": true, "agent.access_snapshot": true, "agent.tool_receipt": true,
	}
	for _, triplet := range triplets {
		if !ownerByExpected[triplet.Owner] {
			return nil, "FROZEN_REF_OWNER_INVALID"
		}
		if strings.TrimSpace(triplet.Ref) == "" || !messageSetDigestPatternV1.MatchString(triplet.Digest) {
			return nil, "FROZEN_REF_GROUP_INVALID"
		}
	}
	if value.ToolRegistryOwner != "agent.tool_registry" || value.RuntimePolicyOwner != "agent.runner_policy" ||
		value.BudgetSnapshotOwner != "agent.budget_policy" || value.AccessScopeOwner != "agent.access_snapshot" ||
		(value.PromptBundleOwner != nil && *value.PromptBundleOwner != "agent.prompt_registry") ||
		(value.ModelRouteOwner != nil && *value.ModelRouteOwner != "agent.model_gateway") ||
		(value.ParentToolReceiptOwner != nil && *value.ParentToolReceiptOwner != "agent.tool_receipt") ||
		(value.ApprovalOwner != nil && *value.ApprovalOwner != "agent.approval_store") ||
		(value.PinnedToolOwner != nil && *value.PinnedToolOwner != "agent.tool_registry") {
		return nil, "FROZEN_REF_OWNER_INVALID"
	}
	return triplets, ""
}

func turnContextResolvedRefsValidV1(expected, actual []turnContextResolvedRefV1) bool {
	expected = append([]turnContextResolvedRefV1(nil), expected...)
	actual = append([]turnContextResolvedRefV1(nil), actual...)
	sort.Slice(expected, func(i, j int) bool { return turnContextRefKeyV1(expected[i]) < turnContextRefKeyV1(expected[j]) })
	sort.Slice(actual, func(i, j int) bool { return turnContextRefKeyV1(actual[i]) < turnContextRefKeyV1(actual[j]) })
	return reflect.DeepEqual(expected, actual)
}

func turnContextRefKeyV1(value turnContextResolvedRefV1) string {
	return value.Owner + "\x00" + value.Ref + "\x00" + value.Digest
}

func turnContextSemanticDigestV1(value turnContextCanonicalV1) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(turnContextDigestDomainV1))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(raw)
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func cloneTurnContextFixtureV1(fixture turnContextFixtureV1) turnContextFixtureV1 {
	raw, err := json.Marshal(fixture)
	if err != nil {
		panic(err)
	}
	var cloned turnContextFixtureV1
	if err := messageSetStrictDecodeV1(raw, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func allStringPointersPresentV1(values ...*string) bool {
	for _, value := range values {
		if value == nil || strings.TrimSpace(*value) == "" {
			return false
		}
	}
	return true
}

func allStringPointersNilV1(values ...*string) bool {
	for _, value := range values {
		if value != nil {
			return false
		}
	}
	return true
}

func applyTurnContextMutationV1(fixture *turnContextFixtureV1, mutation string) {
	value := &fixture.Canonical
	switch mutation {
	case "config_update_after_freeze":
		fixture.RuntimeMetadata.LatestConfigGeneration = "config-generation-99"
		fixture.RuntimeMetadata.LatestPromptBundleRef = "prompt-bundle:current@v99"
	case "retry_attempt_changed":
		fixture.RuntimeMetadata.Attempt++
	case "resume_checkpoint_changed":
		fixture.RuntimeMetadata.CheckpointRef = "checkpoint:resume-99"
	case "takeover_owner_changed":
		fixture.RuntimeMetadata.LeaseFence++
		fixture.RuntimeMetadata.LeaseOwner = "runner-takeover"
		fixture.RuntimeMetadata.ProcessorInstance = "agent-runtime-takeover"
	case "later_message_appended":
		fixture.RuntimeMetadata.LatestMessageSeq++
	case "later_event_appended":
		fixture.RuntimeMetadata.LatestEventSeq++
	case "runtime_metadata_changed":
		fixture.RuntimeMetadata.TraceID = "trace-changed"
		fixture.RuntimeMetadata.RequestID = "request-changed"
		fixture.RuntimeMetadata.RuntimeReadAt = "2026-07-15T09:00:00Z"
	case "schema_unknown":
		value.SchemaVersion = "session_turn_context.v2"
	case "turn_id_invalid":
		value.TurnID = "bad"
	case "turn_kind_unknown":
		value.TurnKind = "deterministic_projection"
	case "input_binding_mismatch":
		fixture.InputBinding.InputID = "019f4000-0000-7000-8000-000000009998"
	case "authority_ref_mismatch":
		value.AuthorityRefDigest = strings.Repeat("9", 64)
	case "cutoff_after_locked_counter":
		value.MessageCutoffSeq = fixture.LockedMessageCounter.LastMessageSeq + 1
	case "event_cutoff_after_locked_counter":
		value.EventCutoffSeq = fixture.LockedEventCounter.LastEventSeq + 1
	case "message_set_schema_unknown":
		value.MessageSetSchemaVersion = "session_message_set.full_array.v2"
	case "message_count_mismatch":
		value.MessageCount++
	case "message_set_digest_invalid":
		value.MessageSetDigest = strings.ToUpper(value.MessageSetDigest)
	case "summary_partial":
		value.HistorySummaryRef = nil
	case "summary_cutoff_after_context":
		if value.HistorySummaryEventCutoffSeq != nil {
			*value.HistorySummaryEventCutoffSeq = value.EventCutoffSeq + 1
		}
	case "summary_owner_invalid":
		if value.HistorySummaryOwner != nil {
			*value.HistorySummaryOwner = "frontend.history"
		}
	case "summary_schema_unknown":
		if value.HistorySummarySchemaVersion != nil {
			*value.HistorySummarySchemaVersion = "session_history_summary.v999"
		}
	case "summary_algorithm_unknown":
		if value.HistorySummaryAlgorithmVersion != nil {
			*value.HistorySummaryAlgorithmVersion = "deterministic_summary.v999"
		}
	case "summary_digest_invalid":
		if value.HistorySummaryDigest != nil {
			*value.HistorySummaryDigest = strings.Repeat("A", 64)
		}
	case "chat_prompt_partial":
		value.PromptBundleRef = nil
	case "chat_model_partial":
		value.ModelRouteDigest = nil
	case "approval_model_forbidden":
		owner, ref, digest := "agent.prompt_registry", "prompt:forbidden@v1", strings.Repeat("1", 64)
		value.PromptBundleOwner, value.PromptBundleRef, value.PromptBundleDigest = &owner, &ref, &digest
	case "continuation_partial":
		value.ApprovalRef = nil
	case "common_owner_invalid":
		value.SkillSnapshotOwner = "frontend.skill"
	case "common_ref_invalid":
		value.BudgetSnapshotRef = ""
	case "common_digest_invalid":
		value.AccessScopeDigest = strings.Repeat("A", 64)
	case "resolved_ref_mismatch":
		fixture.ResolvedRefs[0].Digest = strings.Repeat("9", 64)
	case "context_digest_tamper":
		fixture.StoredContextDigest = strings.Repeat("9", 64)
	default:
		panic("unknown Turn Context mutation: " + mutation)
	}
}

// compile-time guard: 58 个字段顺序变化时，golden/sensitivity 测试必须同步审核。
func TestSessionTurnContextV1CanonicalFieldCount(t *testing.T) {
	typeOf := reflect.TypeOf(turnContextCanonicalV1{})
	if typeOf.NumField() != 58 {
		t.Fatalf("Context canonical fields=%d want=58", typeOf.NumField())
	}
	for index := 0; index < typeOf.NumField(); index++ {
		if tag := typeOf.Field(index).Tag.Get("json"); tag == "" || strings.Contains(tag, "omitempty") {
			t.Fatalf("Context field=%s json tag=%q", typeOf.Field(index).Name, tag)
		}
	}
}

func TestSessionTurnContextV1OperationalMetadataExcluded(t *testing.T) {
	fixture := findTurnContextFixtureV1(t, "turn.chat.tool_pair")
	base, err := turnContextSemanticDigestV1(fixture.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	fixture.RuntimeMetadata = turnContextRuntimeMetadataV1{
		TraceID: "trace-x", RequestID: "request-x", ProcessorInstance: "processor-x", LeaseOwner: "lease-x",
		LeaseFence: 99, Attempt: 10, CheckpointRef: "checkpoint-x", RuntimeReadAt: "2026-07-15T10:00:00Z",
		LatestMessageSeq: 99, LatestEventSeq: 99, LatestConfigGeneration: "generation-x",
		LatestPromptBundleRef: "prompt-x", LatestToolRegistryRef: "tools-x", LatestRuntimePolicyRef: "policy-x",
		LatestModelRouteRef: "model-x", LatestBudgetSnapshotRef: "budget-x", LatestAccessScopeRef: "access-x",
	}
	got, err := turnContextSemanticDigestV1(fixture.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(base)) != 1 {
		t.Fatalf("运维元数据改变 Context digest got=%s want=%s", got, base)
	}
}

func TestSessionTurnContextV1CanonicalFieldSensitivity(t *testing.T) {
	fixture := findTurnContextFixtureV1(t, "turn.chat.single")
	base, _ := turnContextSemanticDigestV1(fixture.Canonical)
	typeOf := reflect.TypeOf(fixture.Canonical)
	for index := 0; index < typeOf.NumField(); index++ {
		mutated := fixture.Canonical
		field := reflect.ValueOf(&mutated).Elem().Field(index)
		switch field.Kind() {
		case reflect.String:
			field.SetString(field.String() + "#")
		case reflect.Bool:
			field.SetBool(!field.Bool())
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			field.SetInt(field.Int() + 1)
		case reflect.Ptr:
			if field.IsNil() {
				value := reflect.New(field.Type().Elem())
				switch value.Elem().Kind() {
				case reflect.String:
					value.Elem().SetString("sensitivity")
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					value.Elem().SetInt(1)
				}
				field.Set(value)
			} else {
				value := reflect.New(field.Type().Elem())
				value.Elem().Set(field.Elem())
				switch value.Elem().Kind() {
				case reflect.String:
					value.Elem().SetString(value.Elem().String() + "#")
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					value.Elem().SetInt(value.Elem().Int() + 1)
				}
				field.Set(value)
			}
		default:
			t.Fatalf("field=%s unsupported kind=%s", typeOf.Field(index).Name, field.Kind())
		}
		got, err := turnContextSemanticDigestV1(mutated)
		if err != nil {
			t.Fatal(err)
		}
		if got == base {
			t.Fatalf("field=%s 未改变 Context digest", typeOf.Field(index).Name)
		}
	}
}

func TestSessionTurnContextV1MessageSetBindings(t *testing.T) {
	messageSets := loadMessageSetFixtureMapV1(t)
	for _, fixture := range loadTurnContextCorpusV1(t).Fixtures {
		messageFixture, exists := messageSets[fixture.MessageSetFixtureID]
		if !exists {
			t.Fatalf("fixture=%s message set 不存在", fixture.FixtureID)
		}
		canonical, reasons := buildTurnContextMessageSetCanonicalV1(messageFixture, fixture.Canonical.MessageCutoffSeq)
		if len(reasons) != 0 {
			t.Fatalf("fixture=%s message reasons=%v", fixture.FixtureID, reasons)
		}
		digest, err := messageSetSemanticDigestV1(canonical)
		if err != nil {
			t.Fatal(err)
		}
		if canonical.SessionID != fixture.Canonical.SessionID ||
			canonical.MessageCutoffSeq != fixture.Canonical.MessageCutoffSeq ||
			len(canonical.Messages) != fixture.Canonical.MessageCount || digest != fixture.Canonical.MessageSetDigest {
			t.Fatalf("fixture=%s 跨 Session/边界 Message Set 绑定不一致 digest=%s want=%s", fixture.FixtureID, digest, fixture.Canonical.MessageSetDigest)
		}
		if fixture.Canonical.TurnKind == "chat" {
			if len(canonical.Messages) == 0 {
				t.Fatalf("fixture=%s chat 缺少当前 User Message", fixture.FixtureID)
			}
			current := canonical.Messages[len(canonical.Messages)-1]
			if current.Role != "user" || current.SourceKind != "authenticated_user_input" ||
				current.OriginTurnID != fixture.Canonical.TurnID || current.SourceID != fixture.Canonical.InputID {
				t.Fatalf("fixture=%s 当前 User Message 未绑定 Turn/Input got=%+v", fixture.FixtureID, current)
			}
		}
	}
}

// TestSessionTurnContextV1ContinuationStructuralGroups 只冻结字段组和 Owner token；真实跨对象因果须由 Owner fixture/Store 验证。
func TestSessionTurnContextV1ContinuationStructuralGroups(t *testing.T) {
	fixture := findTurnContextFixtureV1(t, "turn.approval.continuation")
	value := fixture.Canonical
	if value.AuthorityOwner != "agent.approval_store" || value.AuthorityRefType != "approval_decision" ||
		value.PromptBundleOwner != nil || value.ModelRouteOwner != nil ||
		!value.ContinuationPresent || !allStringPointersPresentV1(
		value.ParentToolReceiptOwner, value.ParentToolReceiptRef, value.ParentToolReceiptDigest, value.ParentRequestSemanticDigest,
		value.ApprovalOwner, value.ApprovalRef, value.ApprovalDigest, value.PinnedToolOwner, value.PinnedToolRef, value.PinnedToolDigest,
	) {
		t.Fatal("Approval Continuation 未完整冻结 Receipt/Approval/Tool Pin 或错误进入模型")
	}
	if *value.ParentToolReceiptOwner != "agent.tool_receipt" || *value.ApprovalOwner != "agent.approval_store" ||
		*value.PinnedToolOwner != "agent.tool_registry" {
		t.Fatal("Approval Continuation owner token 不一致")
	}
}

func TestSessionTurnContextV1AuthorityOwnerMatrix(t *testing.T) {
	messageSets := loadMessageSetFixtureMapV1(t)
	valid := []struct {
		name, fixtureID, owner, refType string
	}{
		{name: "authenticated_user", fixtureID: "turn.chat.single", owner: "business.authorization", refType: "authenticated_user"},
		{name: "approval_decision", fixtureID: "turn.approval.continuation", owner: "agent.approval_store", refType: "approval_decision"},
		{name: "approval_invalidation", fixtureID: "turn.approval.continuation", owner: "agent.approval_store", refType: "approval_invalidation"},
		{name: "batch_terminal_event", fixtureID: "turn.batch.empty", owner: "agent.operation_store", refType: "batch_terminal_event"},
		{name: "legacy_attestation", fixtureID: "turn.chat.single", owner: "agent.legacy_authority", refType: "legacy_ensure_receipt_attestation"},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			fixture := cloneTurnContextFixtureV1(findTurnContextFixtureV1(t, test.fixtureID))
			setTurnContextAuthorityV1(&fixture, test.owner, test.refType)
			got := evaluateTurnContextFixtureV1(fixture, messageSets, false)
			if got.Decision != "valid" {
				t.Fatalf("valid authority owner/type=%s/%s got=%+v", test.owner, test.refType, got)
			}
		})
	}
	invalid := []struct {
		name, fixtureID, owner, refType string
	}{
		{name: "business_cannot_publish_approval", fixtureID: "turn.approval.continuation", owner: "business.authorization", refType: "approval_decision"},
		{name: "approval_store_cannot_publish_user", fixtureID: "turn.chat.single", owner: "agent.approval_store", refType: "authenticated_user"},
		{name: "operation_store_cannot_publish_approval", fixtureID: "turn.approval.continuation", owner: "agent.operation_store", refType: "approval_invalidation"},
		{name: "legacy_cannot_publish_user", fixtureID: "turn.chat.single", owner: "agent.legacy_authority", refType: "authenticated_user"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			fixture := cloneTurnContextFixtureV1(findTurnContextFixtureV1(t, test.fixtureID))
			setTurnContextAuthorityV1(&fixture, test.owner, test.refType)
			got := evaluateTurnContextFixtureV1(fixture, messageSets, false)
			if !reflect.DeepEqual(got.ReasonCodes, []string{"TURN_CONTEXT_AUTHORITY_INVALID"}) {
				t.Fatalf("invalid authority owner/type=%s/%s got=%+v", test.owner, test.refType, got)
			}
		})
	}
}

func setTurnContextAuthorityV1(fixture *turnContextFixtureV1, owner, refType string) {
	oldOwner := fixture.Canonical.AuthorityOwner
	fixture.Canonical.AuthorityOwner = owner
	fixture.Canonical.AuthorityRefType = refType
	fixture.InputBinding.AuthorityOwner = owner
	fixture.InputBinding.AuthorityRefType = refType
	for index := range fixture.ResolvedRefs {
		if fixture.ResolvedRefs[index].Owner == oldOwner && fixture.ResolvedRefs[index].Ref == fixture.Canonical.AuthorityRefID {
			fixture.ResolvedRefs[index].Owner = owner
			return
		}
	}
	panic("authority resolved ref not found")
}

// TestSessionTurnContextV1LegacyAuthorityClassification 只冻结分类 fail-closed；不把普通 chat fixture 伪装为 legacy attestation golden。
func TestSessionTurnContextV1LegacyAuthorityClassification(t *testing.T) {
	fixture := cloneTurnContextFixtureV1(findTurnContextFixtureV1(t, "turn.chat.single"))
	setTurnContextAuthorityV1(&fixture, "agent.legacy_authority", "legacy_ensure_receipt_attestation")
	got := evaluateTurnContextFixtureV1(fixture, loadMessageSetFixtureMapV1(t), false)
	if got.Decision != "valid" {
		t.Fatalf("legacy attestation chat=%+v", got)
	}
	fixture.Canonical.AuthorityRefType = "authenticated_user"
	fixture.InputBinding.AuthorityRefType = "authenticated_user"
	got = evaluateTurnContextFixtureV1(fixture, loadMessageSetFixtureMapV1(t), false)
	if !reflect.DeepEqual(got.ReasonCodes, []string{"TURN_CONTEXT_AUTHORITY_INVALID"}) {
		t.Fatalf("legacy 不得伪装 authenticated_user got=%+v", got)
	}
}

func TestSessionTurnContextV1DigestDomain(t *testing.T) {
	fixture := findTurnContextFixtureV1(t, "turn.chat.single")
	raw, err := json.Marshal(fixture.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	plain := sha256.Sum256(raw)
	got, err := turnContextSemanticDigestV1(fixture.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	if got == hex.EncodeToString(plain[:]) || strings.HasPrefix(got, "sha256:") || !messageSetDigestPatternV1.MatchString(got) {
		t.Fatalf("Context digest 未使用独立 domain/lowercase raw hex got=%s", got)
	}
}

func TestSessionTurnContextV1SummaryCutoffIsolation(t *testing.T) {
	fixture := findTurnContextFixtureV1(t, "turn.batch.summary")
	if !turnContextSummaryValidV1(fixture.Canonical) {
		t.Fatal("合法 Summary fixture 未通过")
	}
	if fixture.Canonical.HistorySummaryMessageCutoffSeq == nil || fixture.Canonical.HistorySummaryEventCutoffSeq == nil {
		t.Fatal("Summary cutoff 缺失")
	}
	if *fixture.Canonical.HistorySummaryMessageCutoffSeq > fixture.Canonical.MessageCutoffSeq ||
		*fixture.Canonical.HistorySummaryEventCutoffSeq > fixture.Canonical.EventCutoffSeq {
		t.Fatal("Summary cutoff 越过 Context")
	}
}
