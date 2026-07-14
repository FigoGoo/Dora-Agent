// Package contract_test 只承载未 Approved 的 Message Set full-array canonical 候选，不提供生产 History Store 或 Runner。
package contract_test

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const (
	messageSetManifestPathV1 = "testdata/w2_r02_turn_context/manifest.json"
	messageSetCorpusPathV1   = "testdata/w2_r02_turn_context/session_message_set_v1.json"
	messageSetSchemaV1       = "session_message_set.full_array.v1"
	messageSetDigestDomainV1 = "dora.session_message_set.full_array.v1"
	messageSetMaxMessagesV1  = 256
)

var messageSetDigestPatternV1 = regexp.MustCompile(`^[0-9a-f]{64}$`)

//go:embed testdata/w2_r02_turn_context/*.json
var w2R02TurnContextFS embed.FS

type messageSetManifestV1 struct {
	SchemaVersion    string                     `json:"schema_version"`
	Files            []messageSetManifestFileV1 `json:"files"`
	FixtureIDs       []string                   `json:"fixture_ids"`
	VectorIDs        []string                   `json:"vector_ids"`
	TotalVectorCount int                        `json:"total_vector_count"`
	TargetTests      []string                   `json:"target_tests"`
}

type messageSetManifestFileV1 struct {
	File        string `json:"file"`
	SHA256      string `json:"sha256"`
	VectorCount int    `json:"vector_count"`
}

type messageSetCorpusV1 struct {
	SchemaVersion string                `json:"schema_version"`
	ExactSets     messageSetExactSetsV1 `json:"exact_sets"`
	Fixtures      []messageSetFixtureV1 `json:"fixtures"`
	Cases         []messageSetCaseV1    `json:"cases"`
}

type messageSetExactSetsV1 struct {
	Decisions             []string `json:"decisions"`
	Roles                 []string `json:"roles"`
	ContentSchemaVersions []string `json:"content_schema_versions"`
	SourceKinds           []string `json:"source_kinds"`
	ToolKeys              []string `json:"tool_keys"`
	OwnerTokens           []string `json:"owner_tokens"`
	MaxMessageCount       int      `json:"max_message_count"`
	ReasonCodes           []string `json:"reason_codes"`
}

type messageSetFixtureV1 struct {
	FixtureID         string                         `json:"fixture_id"`
	SchemaVersion     string                         `json:"schema_version"`
	SessionID         string                         `json:"session_id"`
	MessageCutoffSeq  int64                          `json:"message_cutoff_seq"`
	LastMessageSeq    int64                          `json:"last_message_seq"`
	AvailableMessages []messageSetAvailableMessageV1 `json:"available_messages"`
	StoredDigest      string                         `json:"stored_digest"`
	RecordedAt        string                         `json:"recorded_at"`
	RuntimeMetadata   messageSetRuntimeMetadataV1    `json:"runtime_metadata"`
}

type messageSetAvailableMessageV1 struct {
	MessageID            string                  `json:"message_id"`
	SessionID            string                  `json:"session_id"`
	MessageSeq           int64                   `json:"message_seq"`
	OriginTurnID         string                  `json:"origin_turn_id"`
	Role                 string                  `json:"role"`
	ContentSchemaVersion string                  `json:"content_schema_version"`
	ContentDigest        string                  `json:"content_digest"`
	ContentJSON          string                  `json:"content_json"`
	SourceKind           string                  `json:"source_kind"`
	SourceID             string                  `json:"source_id"`
	ToolCalls            []messageSetToolCallV1  `json:"tool_calls"`
	ToolResult           *messageSetToolResultV1 `json:"tool_result"`
	Ciphertext           string                  `json:"ciphertext"`
	KeyVersion           string                  `json:"key_version"`
	CreatedAt            string                  `json:"created_at"`
}

type messageSetLeafV1 struct {
	MessageID            string                  `json:"message_id"`
	MessageSeq           int64                   `json:"message_seq"`
	OriginTurnID         string                  `json:"origin_turn_id"`
	Role                 string                  `json:"role"`
	ContentSchemaVersion string                  `json:"content_schema_version"`
	ContentDigest        string                  `json:"content_digest"`
	SourceKind           string                  `json:"source_kind"`
	SourceID             string                  `json:"source_id"`
	ToolCalls            []messageSetToolCallV1  `json:"tool_calls"`
	ToolResult           *messageSetToolResultV1 `json:"tool_result"`
}

type messageSetToolCallV1 struct {
	ToolCallID    string `json:"tool_call_id"`
	Ordinal       int64  `json:"ordinal"`
	ToolKey       string `json:"tool_key"`
	RequestDigest string `json:"request_digest"`
}

type messageSetToolResultV1 struct {
	ToolCallID          string `json:"tool_call_id"`
	AssistantMessageSeq int64  `json:"assistant_message_seq"`
	ToolKey             string `json:"tool_key"`
	ToolReceiptOwner    string `json:"tool_receipt_owner"`
	ToolReceiptRef      string `json:"tool_receipt_ref"`
	ToolReceiptDigest   string `json:"tool_receipt_digest"`
}

type messageSetUserTextContentV1 struct {
	SchemaVersion string `json:"schema_version"`
	Text          string `json:"text"`
}

type messageSetCanonicalV1 struct {
	SchemaVersion    string             `json:"schema_version"`
	SessionID        string             `json:"session_id"`
	MessageCutoffSeq int64              `json:"message_cutoff_seq"`
	MessageCount     int                `json:"message_count"`
	Messages         []messageSetLeafV1 `json:"messages"`
}

type messageSetRuntimeMetadataV1 struct {
	TraceID    string `json:"trace_id"`
	RequestID  string `json:"request_id"`
	OwnerFence int64  `json:"owner_fence"`
	Attempt    int64  `json:"attempt"`
}

type messageSetCaseV1 struct {
	ID          string               `json:"id"`
	FromFixture string               `json:"from_fixture"`
	Mutations   []string             `json:"mutations"`
	Expected    messageSetExpectedV1 `json:"expected"`
}

type messageSetExpectedV1 struct {
	Decision     string   `json:"decision"`
	ReasonCodes  []string `json:"reason_codes"`
	Digest       string   `json:"digest"`
	MessageCount int      `json:"message_count"`
}

type messageSetEvaluationV1 struct {
	Decision     string
	ReasonCodes  []string
	Digest       string
	MessageCount int
}

type messageSetPendingCallV1 struct {
	OriginTurnID        string
	AssistantMessageSeq int64
	ToolKey             string
	RequestDigest       string
	Consumed            bool
}

func TestW2R02TurnContextCorpusManifest(t *testing.T) {
	manifest := loadMessageSetManifestV1(t)
	if manifest.SchemaVersion != "w2_r02_turn_context_manifest.v1" || manifest.TotalVectorCount != 70 {
		t.Fatalf("Turn Context manifest schema=%q vectors=%d", manifest.SchemaVersion, manifest.TotalVectorCount)
	}
	wantFixtures := []string{
		"message_set.empty", "message_set.tool_pair", "message_set.unicode",
		"turn.approval.continuation", "turn.batch.empty", "turn.batch.summary", "turn.chat.single", "turn.chat.tool_pair",
	}
	wantVectors := []string{
		"MS-CAN-001-empty-golden", "MS-CAN-002-unicode-golden", "MS-CAN-003-tool-pair-golden",
		"MS-CAN-004-operational-metadata-excluded", "MS-CAN-005-later-message-isolated", "MS-CAN-006-physical-order-canonicalized",
		"MS-N01-schema-unknown", "MS-N02-session-invalid", "MS-N03-cutoff-negative", "MS-N04-cutoff-after-counter",
		"MS-N05-sequence-gap", "MS-N06-sequence-duplicate", "MS-N07-cross-session", "MS-N08-message-id-invalid",
		"MS-N09-role-invalid", "MS-N10-content-schema-invalid", "MS-N11-content-digest-uppercase", "MS-N12-source-invalid",
		"MS-N13-tool-result-before-call", "MS-N14-tool-call-duplicate", "MS-N15-tool-result-call-mismatch",
		"MS-N16-unpaired-tool-call", "MS-N17-digest-tamper", "MS-N18-reason-priority-order-before-leaf",
		"MS-N19-tool-result-cross-turn", "MS-N20-tool-result-assistant-seq-mismatch", "MS-N21-tool-result-key-mismatch",
		"MS-N22-tool-result-duplicate", "MS-N23-tool-receipt-owner-invalid", "MS-N24-tool-receipt-ref-mismatch",
		"MS-N25-tool-receipt-digest-uppercase", "MS-N26-tool-call-request-digest-uppercase",
		"TC-CAN-001-chat-single-golden", "TC-CAN-002-chat-tool-pair-golden", "TC-CAN-003-batch-empty-golden",
		"TC-CAN-004-batch-summary-golden", "TC-CAN-005-approval-continuation-golden", "TC-CAN-006-config-update-isolated",
		"TC-CAN-007-retry-isolated", "TC-CAN-008-resume-isolated", "TC-CAN-009-takeover-isolated",
		"TC-CAN-010-later-message-isolated", "TC-CAN-011-later-event-isolated", "TC-CAN-012-runtime-metadata-isolated",
		"TC-N01-schema-unknown", "TC-N02-turn-id-invalid", "TC-N03-turn-kind-unknown", "TC-N04-input-binding-mismatch",
		"TC-N05-authority-ref-mismatch", "TC-N06-cutoff-after-locked-counter", "TC-N07-message-set-schema-unknown",
		"TC-N08-message-count-mismatch", "TC-N09-message-set-digest-invalid", "TC-N10-summary-partial",
		"TC-N11-summary-cutoff-after-context", "TC-N12-summary-owner-invalid", "TC-N13-summary-digest-invalid",
		"TC-N14-chat-prompt-partial", "TC-N15-chat-model-partial", "TC-N16-approval-model-forbidden",
		"TC-N17-continuation-partial", "TC-N18-common-owner-invalid", "TC-N19-common-ref-invalid",
		"TC-N20-common-digest-invalid", "TC-N21-resolved-ref-mismatch", "TC-N22-context-digest-tamper",
		"TC-N23-reason-priority-schema-before-identity",
		"TC-N24-event-cutoff-after-locked-counter", "TC-N25-summary-schema-unknown", "TC-N26-summary-algorithm-unknown",
	}
	if len(wantVectors) != 70 {
		t.Fatalf("内部向量清单=%d", len(wantVectors))
	}
	if manifest.TotalVectorCount != len(wantVectors) {
		t.Fatalf("manifest vectors=%d want=%d", manifest.TotalVectorCount, len(wantVectors))
	}
	if !reflect.DeepEqual(manifest.FixtureIDs, wantFixtures) || !reflect.DeepEqual(manifest.VectorIDs, wantVectors) {
		t.Fatalf("manifest exact IDs 不符 fixtures=%v vectors=%v", manifest.FixtureIDs, manifest.VectorIDs)
	}
	wantTests := []string{
		"TestW2R02TurnContextCorpusManifest", "TestSessionMessageSetFullArrayV1Corpus",
		"TestSessionMessageSetFullArrayV1ExactSets", "TestSessionMessageSetFullArrayV1GoldenDigests",
		"TestSessionMessageSetFullArrayV1ToolCausality", "TestSessionMessageSetFullArrayV1CutoffIsolation",
		"TestSessionMessageSetFullArrayV1AllToolKeys", "TestSessionMessageSetFullArrayV1Limit",
		"TestSessionMessageSetFullArrayV1CanonicalFieldSensitivity", "TestSessionMessageSetFullArrayV1OperationalMetadataExcluded",
		"TestSessionMessageSetFullArrayV1StrictJSON",
		"TestSessionTurnContextV1Corpus", "TestSessionTurnContextV1GoldenDigests", "TestSessionTurnContextV1ExactSets",
		"TestSessionTurnContextV1ReasonPriority",
		"TestSessionTurnContextV1ConditionalGroups", "TestSessionTurnContextV1FrozenReplay", "TestSessionTurnContextV1StrictJSON",
		"TestSessionTurnContextV1CanonicalFieldCount", "TestSessionTurnContextV1OperationalMetadataExcluded",
		"TestSessionTurnContextV1CanonicalFieldSensitivity", "TestSessionTurnContextV1SummaryCutoffIsolation",
		"TestSessionTurnContextV1MessageSetBindings", "TestSessionTurnContextV1ContinuationStructuralGroups",
		"TestSessionTurnContextV1AuthorityOwnerMatrix",
		"TestSessionTurnContextV1LegacyAuthorityClassification", "TestSessionTurnContextV1DigestDomain",
	}
	if !reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("manifest target tests=%v want=%v", manifest.TargetTests, wantTests)
	}
	actualTests := turnContextTargetTestNamesV1(t)
	manifestTests := append([]string(nil), manifest.TargetTests...)
	sort.Strings(manifestTests)
	if !reflect.DeepEqual(actualTests, manifestTests) {
		t.Fatalf("manifest target tests 未绑定实际 Test 函数 actual=%v manifest=%v", actualTests, manifestTests)
	}
	entries, err := w2R02TurnContextFS.ReadDir("testdata/w2_r02_turn_context")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("Turn Context testdata 不允许子目录: %s", entry.Name())
		}
		names = append(names, entry.Name())
	}
	if want := []string{"manifest.json", "session_message_set_v1.json", "session_turn_context_v1.json"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("Turn Context files=%v want=%v", names, want)
	}
	wantFiles := []messageSetManifestFileV1{
		{File: "session_message_set_v1.json", VectorCount: 32},
		{File: "session_turn_context_v1.json", VectorCount: 38},
	}
	if len(manifest.Files) != len(wantFiles) {
		t.Fatalf("manifest files=%+v", manifest.Files)
	}
	for index, file := range manifest.Files {
		if file.File != wantFiles[index].File || file.VectorCount != wantFiles[index].VectorCount {
			t.Fatalf("manifest file=%+v want=%+v", file, wantFiles[index])
		}
		raw, err := w2R02TurnContextFS.ReadFile("testdata/w2_r02_turn_context/" + file.File)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(raw)
		if got := "sha256:" + hex.EncodeToString(digest[:]); got != file.SHA256 {
			t.Fatalf("corpus=%s sha=%s want=%s", file.File, got, file.SHA256)
		}
	}
	messageCorpus := loadMessageSetCorpusV1(t)
	contextCorpus := loadTurnContextCorpusV1(t)
	fixtureIDs := make([]string, 0, len(messageCorpus.Fixtures)+len(contextCorpus.Fixtures))
	for _, fixture := range messageCorpus.Fixtures {
		fixtureIDs = append(fixtureIDs, fixture.FixtureID)
	}
	for _, fixture := range contextCorpus.Fixtures {
		fixtureIDs = append(fixtureIDs, fixture.FixtureID)
	}
	sort.Strings(fixtureIDs)
	vectorIDs := make([]string, 0, len(messageCorpus.Cases)+len(contextCorpus.Cases))
	for _, testCase := range messageCorpus.Cases {
		vectorIDs = append(vectorIDs, testCase.ID)
	}
	for _, testCase := range contextCorpus.Cases {
		vectorIDs = append(vectorIDs, testCase.ID)
	}
	sort.Strings(vectorIDs)
	if !reflect.DeepEqual(fixtureIDs, manifest.FixtureIDs) || !reflect.DeepEqual(vectorIDs, manifest.VectorIDs) {
		t.Fatalf("Corpus IDs 未绑定 manifest fixtures=%v vectors=%v", fixtureIDs, vectorIDs)
	}
}

func turnContextTargetTestNamesV1(t *testing.T) []string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	files := []string{"session_message_set_v1_corpus_test.go", "session_turn_context_v1_corpus_test.go"}
	result := make([]string, 0, 27)
	for _, name := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			if function.Name.Name == "TestW2R02TurnContextCorpusManifest" ||
				strings.HasPrefix(function.Name.Name, "TestSessionMessageSetFullArrayV1") ||
				strings.HasPrefix(function.Name.Name, "TestSessionTurnContextV1") {
				result = append(result, function.Name.Name)
			}
		}
	}
	sort.Strings(result)
	return result
}

func TestSessionMessageSetFullArrayV1Corpus(t *testing.T) {
	corpus := loadMessageSetCorpusV1(t)
	fixtures := make(map[string]messageSetFixtureV1, len(corpus.Fixtures))
	for _, fixture := range corpus.Fixtures {
		if _, exists := fixtures[fixture.FixtureID]; exists {
			t.Fatalf("重复 fixture=%s", fixture.FixtureID)
		}
		fixtures[fixture.FixtureID] = fixture
	}
	seen := make(map[string]struct{}, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		testCase := testCase
		t.Run(testCase.ID, func(t *testing.T) {
			if _, exists := seen[testCase.ID]; exists {
				t.Fatalf("重复 case=%s", testCase.ID)
			}
			seen[testCase.ID] = struct{}{}
			fixture, exists := fixtures[testCase.FromFixture]
			if !exists {
				t.Fatalf("未知 fixture=%s", testCase.FromFixture)
			}
			got := evaluateMessageSetCaseV1(fixture, testCase.Mutations)
			want := messageSetEvaluationV1{
				Decision: testCase.Expected.Decision, ReasonCodes: testCase.Expected.ReasonCodes,
				Digest: testCase.Expected.Digest, MessageCount: testCase.Expected.MessageCount,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Message Set evaluation=%+v want=%+v", got, want)
			}
		})
	}
}

func TestSessionMessageSetFullArrayV1ExactSets(t *testing.T) {
	got := loadMessageSetCorpusV1(t).ExactSets
	want := messageSetExactSetsV1{
		Decisions:             []string{"valid", "invalid"},
		Roles:                 []string{"user", "assistant", "tool"},
		ContentSchemaVersions: []string{"message.user_text.v1", "message.assistant_text.v1", "message.assistant_tool_call.v1", "message.tool_result.v1"},
		SourceKinds:           []string{"authenticated_user_input", "model_receipt", "tool_receipt"},
		ToolKeys:              []string{"plan_creation_spec", "analyze_materials", "plan_storyboard", "generate_media", "write_prompts", "assemble_output"},
		OwnerTokens:           []string{"agent.tool_receipt"},
		MaxMessageCount:       messageSetMaxMessagesV1,
		ReasonCodes: []string{
			"MESSAGE_SET_SCHEMA_INVALID", "MESSAGE_SET_SESSION_INVALID", "MESSAGE_SET_CUTOFF_INVALID",
			"MESSAGE_SET_LIMIT_EXCEEDED", "MESSAGE_SET_ORDER_INVALID", "MESSAGE_SET_LEAF_INVALID",
			"MESSAGE_TOOL_CAUSALITY_INVALID", "MESSAGE_SET_DIGEST_MISMATCH",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Message Set exact sets=%+v want=%+v", got, want)
	}
}

func TestSessionMessageSetFullArrayV1GoldenDigests(t *testing.T) {
	for _, fixture := range loadMessageSetCorpusV1(t).Fixtures {
		canonical, reasons := buildMessageSetCanonicalV1(fixture)
		if len(reasons) != 0 {
			t.Errorf("fixture=%s reasons=%v", fixture.FixtureID, reasons)
			continue
		}
		digest, err := messageSetSemanticDigestV1(canonical)
		if err != nil {
			t.Errorf("fixture=%s digest err=%v", fixture.FixtureID, err)
			continue
		}
		if digest != fixture.StoredDigest {
			t.Errorf("fixture=%s golden=%s stored=%s", fixture.FixtureID, digest, fixture.StoredDigest)
		}
	}
}

func TestSessionMessageSetFullArrayV1ToolCausality(t *testing.T) {
	fixture := findMessageSetFixtureV1(t, "message_set.tool_pair")
	canonical, reasons := buildMessageSetCanonicalV1(fixture)
	if len(reasons) != 0 || len(canonical.Messages) != 4 {
		t.Fatalf("合法 Tool 因果链 reasons=%v count=%d", reasons, len(canonical.Messages))
	}
	call := canonical.Messages[1].ToolCalls[0]
	result := canonical.Messages[2].ToolResult
	if result == nil || result.ToolCallID != call.ToolCallID || result.ToolKey != call.ToolKey || result.AssistantMessageSeq != canonical.Messages[1].MessageSeq {
		t.Fatalf("Tool Result 未逐值绑定 call=%+v result=%+v", call, result)
	}
}

func TestSessionMessageSetFullArrayV1AllToolKeys(t *testing.T) {
	toolKeys := loadMessageSetCorpusV1(t).ExactSets.ToolKeys
	for _, toolKey := range toolKeys {
		toolKey := toolKey
		t.Run(toolKey, func(t *testing.T) {
			fixture := cloneMessageSetFixtureV1(findMessageSetFixtureV1(t, "message_set.tool_pair"))
			fixture.AvailableMessages[1].ToolCalls[0].ToolKey = toolKey
			fixture.AvailableMessages[2].ToolResult.ToolKey = toolKey
			if _, reasons := buildMessageSetCanonicalV1(fixture); len(reasons) != 0 {
				t.Fatalf("合法 Tool key=%s reasons=%v", toolKey, reasons)
			}
		})
	}
}

func TestSessionMessageSetFullArrayV1Limit(t *testing.T) {
	fixture := messageSetFixtureV1{
		FixtureID: "generated.limit", SchemaVersion: messageSetSchemaV1,
		SessionID:        "019f3000-0000-7000-8000-000000000001",
		MessageCutoffSeq: messageSetMaxMessagesV1, LastMessageSeq: messageSetMaxMessagesV1,
		AvailableMessages: make([]messageSetAvailableMessageV1, 0, messageSetMaxMessagesV1),
	}
	for index := 1; index <= messageSetMaxMessagesV1; index++ {
		fixture.AvailableMessages = append(fixture.AvailableMessages, messageSetAvailableMessageV1{
			MessageID: fmt.Sprintf("019f3000-0000-7000-8000-%012d", index), SessionID: fixture.SessionID,
			MessageSeq: int64(index), OriginTurnID: "019f3000-0000-7000-8000-000000000901",
			Role: "user", ContentSchemaVersion: "message.user_text.v1", ContentDigest: fmt.Sprintf("%064x", index),
			SourceKind: "authenticated_user_input", SourceID: fmt.Sprintf("019f3000-0000-7000-8001-%012d", index),
			ToolCalls: []messageSetToolCallV1{},
		})
	}
	canonical, reasons := buildMessageSetCanonicalV1(fixture)
	if len(reasons) != 0 || len(canonical.Messages) != messageSetMaxMessagesV1 {
		t.Fatalf("max boundary reasons=%v count=%d", reasons, len(canonical.Messages))
	}
	fixture.MessageCutoffSeq++
	fixture.LastMessageSeq++
	if _, reasons := buildMessageSetCanonicalV1(fixture); !reflect.DeepEqual(reasons, []string{"MESSAGE_SET_LIMIT_EXCEEDED"}) {
		t.Fatalf("max+1 reasons=%v", reasons)
	}
}

func TestSessionMessageSetFullArrayV1CanonicalFieldSensitivity(t *testing.T) {
	fixture := findMessageSetFixtureV1(t, "message_set.tool_pair")
	base, reasons := buildMessageSetCanonicalV1(fixture)
	if len(reasons) != 0 {
		t.Fatal(reasons)
	}
	baseDigest, _ := messageSetSemanticDigestV1(base)
	mutations := []struct {
		name   string
		mutate func(*messageSetCanonicalV1)
	}{
		{name: "schema_version", mutate: func(v *messageSetCanonicalV1) { v.SchemaVersion += ".x" }},
		{name: "session_id", mutate: func(v *messageSetCanonicalV1) { v.SessionID = "019f3000-0000-7000-8000-000000000999" }},
		{name: "message_cutoff_seq", mutate: func(v *messageSetCanonicalV1) { v.MessageCutoffSeq++ }},
		{name: "message_count", mutate: func(v *messageSetCanonicalV1) { v.MessageCount++ }},
		{name: "message_id", mutate: func(v *messageSetCanonicalV1) { v.Messages[0].MessageID = "019f3000-0000-7000-8000-000000000998" }},
		{name: "message_seq", mutate: func(v *messageSetCanonicalV1) { v.Messages[0].MessageSeq++ }},
		{name: "origin_turn_id", mutate: func(v *messageSetCanonicalV1) { v.Messages[0].OriginTurnID = "019f3000-0000-7000-8000-000000000997" }},
		{name: "role", mutate: func(v *messageSetCanonicalV1) { v.Messages[0].Role = "assistant" }},
		{name: "content_schema_version", mutate: func(v *messageSetCanonicalV1) { v.Messages[0].ContentSchemaVersion += ".x" }},
		{name: "content_digest", mutate: func(v *messageSetCanonicalV1) { v.Messages[0].ContentDigest = strings.Repeat("9", 64) }},
		{name: "source_kind", mutate: func(v *messageSetCanonicalV1) { v.Messages[0].SourceKind += ".x" }},
		{name: "source_id", mutate: func(v *messageSetCanonicalV1) { v.Messages[0].SourceID = "019f3000-0000-7000-8000-000000000996" }},
		{name: "tool_calls", mutate: func(v *messageSetCanonicalV1) {
			v.Messages[0].ToolCalls = append(v.Messages[0].ToolCalls, v.Messages[1].ToolCalls[0])
		}},
		{name: "tool_result_presence", mutate: func(v *messageSetCanonicalV1) {
			result := *v.Messages[2].ToolResult
			v.Messages[0].ToolResult = &result
		}},
		{name: "tool_call_id", mutate: func(v *messageSetCanonicalV1) {
			v.Messages[1].ToolCalls[0].ToolCallID = "019f3000-0000-7000-8000-000000000995"
		}},
		{name: "tool_call_ordinal", mutate: func(v *messageSetCanonicalV1) { v.Messages[1].ToolCalls[0].Ordinal++ }},
		{name: "tool_call_key", mutate: func(v *messageSetCanonicalV1) { v.Messages[1].ToolCalls[0].ToolKey = "analyze_materials" }},
		{name: "tool_call_request_digest", mutate: func(v *messageSetCanonicalV1) { v.Messages[1].ToolCalls[0].RequestDigest = strings.Repeat("8", 64) }},
		{name: "result_call_id", mutate: func(v *messageSetCanonicalV1) {
			v.Messages[2].ToolResult.ToolCallID = "019f3000-0000-7000-8000-000000000994"
		}},
		{name: "result_assistant_seq", mutate: func(v *messageSetCanonicalV1) { v.Messages[2].ToolResult.AssistantMessageSeq++ }},
		{name: "result_tool_key", mutate: func(v *messageSetCanonicalV1) { v.Messages[2].ToolResult.ToolKey = "analyze_materials" }},
		{name: "result_receipt_owner", mutate: func(v *messageSetCanonicalV1) { v.Messages[2].ToolResult.ToolReceiptOwner += ".x" }},
		{name: "result_receipt_ref", mutate: func(v *messageSetCanonicalV1) { v.Messages[2].ToolResult.ToolReceiptRef += ".x" }},
		{name: "result_receipt_digest", mutate: func(v *messageSetCanonicalV1) { v.Messages[2].ToolResult.ToolReceiptDigest = strings.Repeat("7", 64) }},
	}
	for _, testCase := range mutations {
		t.Run(testCase.name, func(t *testing.T) {
			mutated := cloneMessageSetCanonicalV1(base)
			testCase.mutate(&mutated)
			got, err := messageSetSemanticDigestV1(mutated)
			if err != nil {
				t.Fatal(err)
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(baseDigest)) == 1 {
				t.Fatalf("字段 %s 未改变 Message Set digest", testCase.name)
			}
		})
	}
}

func TestSessionMessageSetFullArrayV1CutoffIsolation(t *testing.T) {
	fixture := findMessageSetFixtureV1(t, "message_set.tool_pair")
	base, reasons := buildMessageSetCanonicalV1(fixture)
	if len(reasons) != 0 {
		t.Fatal(reasons)
	}
	baseDigest, _ := messageSetSemanticDigestV1(base)
	fixture.AvailableMessages[4].ContentDigest = strings.Repeat("f", 64)
	fixture.AvailableMessages[4].Ciphertext = "changed-after-cutoff"
	got, reasons := buildMessageSetCanonicalV1(fixture)
	if len(reasons) != 0 {
		t.Fatal(reasons)
	}
	gotDigest, _ := messageSetSemanticDigestV1(got)
	if gotDigest != baseDigest {
		t.Fatalf("cutoff 后 Message 改变旧摘要 got=%s want=%s", gotDigest, baseDigest)
	}
}

func TestSessionMessageSetFullArrayV1OperationalMetadataExcluded(t *testing.T) {
	fixture := findMessageSetFixtureV1(t, "message_set.unicode")
	base, _ := buildMessageSetCanonicalV1(fixture)
	baseDigest, _ := messageSetSemanticDigestV1(base)
	fixture.RecordedAt = "2026-07-16T00:00:00Z"
	fixture.RuntimeMetadata = messageSetRuntimeMetadataV1{TraceID: "trace-b", RequestID: "request-b", OwnerFence: 99, Attempt: 9}
	fixture.AvailableMessages[0].Ciphertext = "rotated-ciphertext"
	fixture.AvailableMessages[0].KeyVersion = "key-v99"
	fixture.AvailableMessages[0].CreatedAt = "2026-07-16T00:00:00Z"
	got, reasons := buildMessageSetCanonicalV1(fixture)
	if len(reasons) != 0 {
		t.Fatal(reasons)
	}
	gotDigest, _ := messageSetSemanticDigestV1(got)
	if gotDigest != baseDigest {
		t.Fatalf("运维元数据改变 Message Set digest got=%s want=%s", gotDigest, baseDigest)
	}
}

func TestSessionMessageSetFullArrayV1StrictJSON(t *testing.T) {
	var manifest messageSetManifestV1
	if err := messageSetStrictDecodeV1([]byte(`{"schema_version":"x","files":[],"fixture_ids":[],"vector_ids":[],"total_vector_count":0,"target_tests":[],"future":true}`), &manifest); err == nil {
		t.Fatal("manifest 未拒绝未知字段")
	}
	if err := messageSetStrictDecodeV1([]byte(`{"schema_version":"x","files":[],"fixture_ids":[],"vector_ids":[],"total_vector_count":0,"target_tests":[]}{}`), &manifest); err == nil {
		t.Fatal("manifest 未拒绝尾随 JSON")
	}
	if err := messageSetStrictDecodeV1([]byte(`{"schema_version":"x","schema_version":"y","files":[],"fixture_ids":[],"vector_ids":[],"total_vector_count":0,"target_tests":[]}`), &manifest); err == nil {
		t.Fatal("manifest 未拒绝重复字段")
	}
	if err := messageSetInspectJSONV1([]byte(`{"tool_result":null,"tool_calls":[]}`)); err != nil {
		t.Fatalf("Message canonical 所需显式 null 被错误拒绝: %v", err)
	}
	var corpus messageSetCorpusV1
	if err := messageSetStrictDecodeV1([]byte(`{"schema_version":"x","exact_sets":{},"fixtures":[],"cases":[],"future":true}`), &corpus); err == nil {
		t.Fatal("Corpus 未拒绝未知字段")
	}
	var leaf messageSetAvailableMessageV1
	if err := messageSetStrictDecodeV1([]byte(`{"message_id":"x","session_id":"x","message_seq":1,"origin_turn_id":"x","role":"user","content_schema_version":"x","content_digest":"x","content_json":"","source_kind":"x","source_id":"x","tool_calls":[],"tool_result":null,"ciphertext":"","key_version":"","created_at":"","future":true}`), &leaf); err == nil {
		t.Fatal("Message leaf 未拒绝未知字段")
	}
	var call messageSetToolCallV1
	if err := messageSetStrictDecodeV1([]byte(`{"tool_call_id":"x","ordinal":1,"tool_key":"x","request_digest":"x","future":true}`), &call); err == nil {
		t.Fatal("ToolCall 未拒绝未知字段")
	}
	var result messageSetToolResultV1
	if err := messageSetStrictDecodeV1([]byte(`{"tool_call_id":"x","assistant_message_seq":1,"tool_key":"x","tool_receipt_owner":"x","tool_receipt_ref":"x","tool_receipt_digest":"x","future":true}`), &result); err == nil {
		t.Fatal("ToolResult 未拒绝未知字段")
	}
}

func loadMessageSetManifestV1(t *testing.T) messageSetManifestV1 {
	t.Helper()
	raw, err := w2R02TurnContextFS.ReadFile(messageSetManifestPathV1)
	if err != nil {
		t.Fatal(err)
	}
	var manifest messageSetManifestV1
	if err := messageSetStrictDecodeV1(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func loadMessageSetCorpusV1(t *testing.T) messageSetCorpusV1 {
	t.Helper()
	raw, err := w2R02TurnContextFS.ReadFile(messageSetCorpusPathV1)
	if err != nil {
		t.Fatal(err)
	}
	var corpus messageSetCorpusV1
	if err := messageSetStrictDecodeV1(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != "session_message_set_full_array_v1_corpus.v1" {
		t.Fatalf("Message Set corpus schema=%q", corpus.SchemaVersion)
	}
	return corpus
}

func findMessageSetFixtureV1(t *testing.T, fixtureID string) messageSetFixtureV1 {
	t.Helper()
	for _, fixture := range loadMessageSetCorpusV1(t).Fixtures {
		if fixture.FixtureID == fixtureID {
			return fixture
		}
	}
	t.Fatalf("fixture not found=%s", fixtureID)
	return messageSetFixtureV1{}
}

func evaluateMessageSetCaseV1(fixture messageSetFixtureV1, mutations []string) messageSetEvaluationV1 {
	fixture = cloneMessageSetFixtureV1(fixture)
	for _, mutation := range mutations {
		applyMessageSetMutationV1(&fixture, mutation)
	}
	canonical, reasons := buildMessageSetCanonicalV1(fixture)
	if len(reasons) != 0 {
		return messageSetEvaluationV1{Decision: "invalid", ReasonCodes: reasons}
	}
	digest, err := messageSetSemanticDigestV1(canonical)
	if err != nil {
		return messageSetEvaluationV1{Decision: "invalid", ReasonCodes: []string{"MESSAGE_SET_SCHEMA_INVALID"}}
	}
	if !messageSetDigestPatternV1.MatchString(fixture.StoredDigest) || subtle.ConstantTimeCompare([]byte(digest), []byte(fixture.StoredDigest)) != 1 {
		return messageSetEvaluationV1{Decision: "invalid", ReasonCodes: []string{"MESSAGE_SET_DIGEST_MISMATCH"}}
	}
	return messageSetEvaluationV1{Decision: "valid", ReasonCodes: []string{}, Digest: digest, MessageCount: len(canonical.Messages)}
}

func cloneMessageSetFixtureV1(fixture messageSetFixtureV1) messageSetFixtureV1 {
	raw, err := json.Marshal(fixture)
	if err != nil {
		panic(err)
	}
	var cloned messageSetFixtureV1
	if err := messageSetStrictDecodeV1(raw, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func cloneMessageSetCanonicalV1(canonical messageSetCanonicalV1) messageSetCanonicalV1 {
	raw, err := json.Marshal(canonical)
	if err != nil {
		panic(err)
	}
	var cloned messageSetCanonicalV1
	if err := messageSetStrictDecodeV1(raw, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func applyMessageSetMutationV1(fixture *messageSetFixtureV1, mutation string) {
	switch mutation {
	case "operational_metadata_changed":
		fixture.RecordedAt = "2026-07-16T00:00:00Z"
		fixture.RuntimeMetadata.OwnerFence++
		fixture.RuntimeMetadata.Attempt++
		fixture.AvailableMessages[0].Ciphertext += "-changed"
		fixture.AvailableMessages[0].KeyVersion = "key-v99"
	case "later_message_changed":
		fixture.AvailableMessages[len(fixture.AvailableMessages)-1].ContentDigest = strings.Repeat("f", 64)
	case "physical_order_reversed":
		for left, right := 0, len(fixture.AvailableMessages)-1; left < right; left, right = left+1, right-1 {
			fixture.AvailableMessages[left], fixture.AvailableMessages[right] = fixture.AvailableMessages[right], fixture.AvailableMessages[left]
		}
	case "schema_unknown":
		fixture.SchemaVersion = "session_message_set.full_array.v2"
	case "session_invalid":
		fixture.SessionID = "not-a-uuid"
	case "cutoff_negative":
		fixture.MessageCutoffSeq = -1
	case "cutoff_after_counter":
		fixture.MessageCutoffSeq = fixture.LastMessageSeq + 1
	case "sequence_gap":
		fixture.AvailableMessages[1].MessageSeq = 9
	case "sequence_duplicate":
		fixture.AvailableMessages[2].MessageSeq = fixture.AvailableMessages[1].MessageSeq
	case "cross_session":
		fixture.AvailableMessages[0].SessionID = "019f2000-0000-7000-8000-000000000999"
	case "message_id_invalid":
		fixture.AvailableMessages[0].MessageID = "bad"
	case "role_invalid":
		fixture.AvailableMessages[0].Role = "system"
	case "content_schema_invalid":
		fixture.AvailableMessages[0].ContentSchemaVersion = "message.future.v1"
	case "content_digest_uppercase":
		fixture.AvailableMessages[0].ContentDigest = strings.ToUpper(fixture.AvailableMessages[0].ContentDigest)
	case "source_invalid":
		fixture.AvailableMessages[0].SourceKind = "frontend"
	case "tool_result_before_call":
		fixture.AvailableMessages[1].MessageSeq, fixture.AvailableMessages[2].MessageSeq = 3, 2
	case "tool_call_duplicate":
		call := fixture.AvailableMessages[1].ToolCalls[0]
		call.Ordinal = 2
		fixture.AvailableMessages[1].ToolCalls = append(fixture.AvailableMessages[1].ToolCalls, call)
	case "tool_result_call_mismatch":
		fixture.AvailableMessages[2].ToolResult.ToolCallID = "019f2000-0000-7000-8000-000000000499"
	case "unpaired_tool_call":
		fixture.MessageCutoffSeq = 2
	case "digest_tamper":
		fixture.StoredDigest = strings.Repeat("f", 64)
	case "tool_result_cross_turn":
		fixture.AvailableMessages[2].OriginTurnID = "019f2000-0000-7000-8000-000000000599"
	case "tool_result_assistant_seq_mismatch":
		fixture.AvailableMessages[2].ToolResult.AssistantMessageSeq = 1
	case "tool_result_key_mismatch":
		fixture.AvailableMessages[2].ToolResult.ToolKey = "analyze_materials"
	case "tool_result_duplicate":
		duplicate := fixture.AvailableMessages[2]
		result := *duplicate.ToolResult
		duplicate.ToolResult = &result
		duplicate.MessageID = "019f2000-0000-7000-8000-000000000215"
		duplicate.MessageSeq = 5
		duplicate.SourceID = "019f2000-0000-7000-8000-000000000615"
		duplicate.ToolResult.ToolReceiptRef = "tool-receipt:" + duplicate.SourceID + "@v1"
		fixture.AvailableMessages[4] = duplicate
		fixture.MessageCutoffSeq = 5
	case "tool_receipt_owner_invalid":
		fixture.AvailableMessages[2].ToolResult.ToolReceiptOwner = "business.tool_receipt"
	case "tool_receipt_ref_mismatch":
		fixture.AvailableMessages[2].ToolResult.ToolReceiptRef = "tool-receipt:other@v1"
	case "tool_receipt_digest_uppercase":
		fixture.AvailableMessages[2].ToolResult.ToolReceiptDigest = strings.ToUpper(fixture.AvailableMessages[2].ToolResult.ToolReceiptDigest)
	case "tool_call_request_digest_uppercase":
		fixture.AvailableMessages[1].ToolCalls[0].RequestDigest = strings.ToUpper(fixture.AvailableMessages[1].ToolCalls[0].RequestDigest)
	}
}

func buildMessageSetCanonicalV1(fixture messageSetFixtureV1) (messageSetCanonicalV1, []string) {
	if fixture.SchemaVersion != messageSetSchemaV1 {
		return messageSetCanonicalV1{}, []string{"MESSAGE_SET_SCHEMA_INVALID"}
	}
	if !messageSetUUIDv7V1(fixture.SessionID) {
		return messageSetCanonicalV1{}, []string{"MESSAGE_SET_SESSION_INVALID"}
	}
	if fixture.MessageCutoffSeq < 0 || fixture.LastMessageSeq < 0 || fixture.MessageCutoffSeq > fixture.LastMessageSeq {
		return messageSetCanonicalV1{}, []string{"MESSAGE_SET_CUTOFF_INVALID"}
	}
	if fixture.MessageCutoffSeq > messageSetMaxMessagesV1 {
		return messageSetCanonicalV1{}, []string{"MESSAGE_SET_LIMIT_EXCEEDED"}
	}
	visible := make([]messageSetAvailableMessageV1, 0, fixture.MessageCutoffSeq)
	for _, message := range fixture.AvailableMessages {
		if message.MessageSeq <= fixture.MessageCutoffSeq {
			visible = append(visible, message)
		}
	}
	sort.Slice(visible, func(i, j int) bool { return visible[i].MessageSeq < visible[j].MessageSeq })
	if int64(len(visible)) != fixture.MessageCutoffSeq {
		return messageSetCanonicalV1{}, []string{"MESSAGE_SET_ORDER_INVALID"}
	}
	canonical := messageSetCanonicalV1{
		SchemaVersion: messageSetSchemaV1, SessionID: fixture.SessionID,
		MessageCutoffSeq: fixture.MessageCutoffSeq, MessageCount: len(visible), Messages: make([]messageSetLeafV1, 0, len(visible)),
	}
	pending := make(map[string]messageSetPendingCallV1)
	for index, message := range visible {
		if message.MessageSeq != int64(index+1) {
			return messageSetCanonicalV1{}, []string{"MESSAGE_SET_ORDER_INVALID"}
		}
		leaf, reason := validateMessageSetLeafV1(fixture.SessionID, message, pending)
		if reason != "" {
			return messageSetCanonicalV1{}, []string{reason}
		}
		canonical.Messages = append(canonical.Messages, leaf)
	}
	for _, call := range pending {
		if !call.Consumed {
			return messageSetCanonicalV1{}, []string{"MESSAGE_TOOL_CAUSALITY_INVALID"}
		}
	}
	return canonical, nil
}

func validateMessageSetLeafV1(sessionID string, message messageSetAvailableMessageV1, pending map[string]messageSetPendingCallV1) (messageSetLeafV1, string) {
	if message.SessionID != sessionID || !messageSetUUIDv7V1(message.MessageID) || !messageSetUUIDv7V1(message.OriginTurnID) ||
		!messageSetUUIDv7V1(message.SourceID) || !messageSetDigestPatternV1.MatchString(message.ContentDigest) || message.ToolCalls == nil {
		return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
	}
	if message.ContentJSON != "" && !messageSetContentDigestValidV1(message) {
		return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
	}
	leaf := messageSetLeafV1{
		MessageID: message.MessageID, MessageSeq: message.MessageSeq, OriginTurnID: message.OriginTurnID,
		Role: message.Role, ContentSchemaVersion: message.ContentSchemaVersion, ContentDigest: message.ContentDigest,
		SourceKind: message.SourceKind, SourceID: message.SourceID, ToolCalls: message.ToolCalls, ToolResult: message.ToolResult,
	}
	switch message.Role {
	case "user":
		if message.ContentSchemaVersion != "message.user_text.v1" || message.SourceKind != "authenticated_user_input" || len(message.ToolCalls) != 0 || message.ToolResult != nil {
			return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
		}
	case "assistant":
		if message.SourceKind != "model_receipt" || message.ToolResult != nil {
			return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
		}
		if message.ContentSchemaVersion == "message.assistant_text.v1" {
			if len(message.ToolCalls) != 0 {
				return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
			}
		} else if message.ContentSchemaVersion == "message.assistant_tool_call.v1" {
			if len(message.ToolCalls) == 0 {
				return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
			}
			for index, call := range message.ToolCalls {
				if call.Ordinal != int64(index+1) || !messageSetUUIDv7V1(call.ToolCallID) || !messageSetKnownToolV1(call.ToolKey) || !messageSetDigestPatternV1.MatchString(call.RequestDigest) {
					return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
				}
				if _, exists := pending[call.ToolCallID]; exists {
					return messageSetLeafV1{}, "MESSAGE_TOOL_CAUSALITY_INVALID"
				}
				pending[call.ToolCallID] = messageSetPendingCallV1{
					OriginTurnID: message.OriginTurnID, AssistantMessageSeq: message.MessageSeq,
					ToolKey: call.ToolKey, RequestDigest: call.RequestDigest,
				}
			}
		} else {
			return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
		}
	case "tool":
		if message.ContentSchemaVersion != "message.tool_result.v1" || message.SourceKind != "tool_receipt" || len(message.ToolCalls) != 0 || message.ToolResult == nil {
			return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
		}
		result := message.ToolResult
		if !messageSetUUIDv7V1(result.ToolCallID) || result.ToolReceiptOwner != "agent.tool_receipt" || strings.TrimSpace(result.ToolReceiptRef) == "" ||
			result.ToolReceiptRef != "tool-receipt:"+message.SourceID+"@v1" || !messageSetDigestPatternV1.MatchString(result.ToolReceiptDigest) || !messageSetKnownToolV1(result.ToolKey) {
			return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
		}
		call, exists := pending[result.ToolCallID]
		if !exists || call.Consumed || call.OriginTurnID != message.OriginTurnID || call.AssistantMessageSeq >= message.MessageSeq ||
			call.AssistantMessageSeq != result.AssistantMessageSeq || call.ToolKey != result.ToolKey {
			return messageSetLeafV1{}, "MESSAGE_TOOL_CAUSALITY_INVALID"
		}
		call.Consumed = true
		pending[result.ToolCallID] = call
	default:
		return messageSetLeafV1{}, "MESSAGE_SET_LEAF_INVALID"
	}
	return leaf, ""
}

func messageSetContentDigestValidV1(message messageSetAvailableMessageV1) bool {
	if message.ContentSchemaVersion != "message.user_text.v1" {
		return false
	}
	var content messageSetUserTextContentV1
	if err := messageSetStrictDecodeV1([]byte(message.ContentJSON), &content); err != nil || content.SchemaVersion != message.ContentSchemaVersion {
		return false
	}
	raw, err := json.Marshal(content)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(raw)
	got := hex.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(message.ContentDigest)) == 1
}

func messageSetKnownToolV1(value string) bool {
	switch value {
	case "plan_creation_spec", "analyze_materials", "plan_storyboard", "generate_media", "write_prompts", "assemble_output":
		return true
	default:
		return false
	}
}

func messageSetSemanticDigestV1(value messageSetCanonicalV1) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(messageSetDigestDomainV1))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(raw)
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func messageSetUUIDv7V1(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.Variant() == uuid.RFC4122 && parsed.String() == value
}

func messageSetStrictDecodeV1(raw []byte, target any) error {
	if err := messageSetInspectJSONV1(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func messageSetInspectJSONV1(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := messageSetInspectJSONValueV1(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON")
		}
		return err
	}
	return nil
}

func messageSetInspectJSONValueV1(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key must be string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate field %q", key)
			}
			seen[key] = struct{}{}
			if err := messageSetInspectJSONValueV1(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return fmt.Errorf("invalid object close")
		}
	case '[':
		for decoder.More() {
			if err := messageSetInspectJSONValueV1(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return fmt.Errorf("invalid array close")
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delim)
	}
	return nil
}
