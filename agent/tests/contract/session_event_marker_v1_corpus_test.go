// Package contract_test 只承载 Session Event Marker 候选 canonical 契约，不提供生产表、Writer、Helper 或 Retention。
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
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	agentevent "github.com/FigoGoo/Dora-Agent/agent/internal/event"
	"github.com/google/uuid"
)

const (
	markerManifestPathV1 = "testdata/w2_r02_marker/manifest.json"
	markerCorpusPathV1   = "testdata/w2_r02_marker/session_event_marker_v1.json"
	markerDigestDomainV1 = "dora.session_event_marker.v1"
)

var markerDigestPatternV1 = regexp.MustCompile(`^[0-9a-f]{64}$`)

//go:embed testdata/w2_r02_marker/*.json
var w2R02MarkerFS embed.FS

// markerManifestV1 冻结 Corpus 文件摘要、向量总数和必须存在的目标测试。
type markerManifestV1 struct {
	SchemaVersion    string                 `json:"schema_version"`
	Files            []markerManifestFileV1 `json:"files"`
	FixtureIDs       []string               `json:"fixture_ids"`
	VectorIDs        []string               `json:"vector_ids"`
	TotalVectorCount int                    `json:"total_vector_count"`
	TargetTests      []string               `json:"target_tests"`
}

// markerManifestFileV1 描述一个 Corpus 文件的内容摘要和向量数量。
type markerManifestFileV1 struct {
	File        string `json:"file"`
	SHA256      string `json:"sha256"`
	VectorCount int    `json:"vector_count"`
}

// markerCorpusV1 冻结 Marker exact-set、三个基础事实和合法/非法向量。
type markerCorpusV1 struct {
	SchemaVersion string            `json:"schema_version"`
	ExactSets     markerExactSetsV1 `json:"exact_sets"`
	Fixtures      []markerFixtureV1 `json:"fixtures"`
	Cases         []markerCaseV1    `json:"cases"`
}

// markerExactSetsV1 固定 Marker v1 的可接受枚举与稳定拒绝原因。
type markerExactSetsV1 struct {
	Decisions      []string `json:"decisions"`
	MarkerTypes    []string `json:"marker_types"`
	SourceKinds    []string `json:"source_kinds"`
	AggregateTypes []string `json:"aggregate_types"`
	ReasonCodes    []string `json:"reason_codes"`
}

// markerFixtureV1 表示一个可独立重算 Payload/Marker 摘要的测试专用事实。
// RecordedAt 与 HelperMetadata 只承载运维信息，禁止进入 semantic canonical。
type markerFixtureV1 struct {
	FixtureID            string                 `json:"fixture_id"`
	PayloadJSON          string                 `json:"payload_json"`
	Event                markerEventV1          `json:"event"`
	Canonical            markerCanonicalV1      `json:"canonical"`
	StoredSemanticDigest string                 `json:"stored_semantic_digest"`
	RecordedAt           string                 `json:"recorded_at"`
	HelperMetadata       markerHelperMetadataV1 `json:"helper_metadata"`
}

// markerEventV1 表示生成 Marker 的原 Event 身份；Marker 必须逐字段复用这些事实。
type markerEventV1 struct {
	EventID          string `json:"event_id"`
	SessionID        string `json:"session_id"`
	EventType        string `json:"event_type"`
	SchemaVersion    string `json:"schema_version"`
	SourceKind       string `json:"source_kind"`
	SourceID         string `json:"source_id"`
	ProjectionIndex  int64  `json:"projection_index"`
	AggregateType    string `json:"aggregate_type"`
	AggregateID      string `json:"aggregate_id"`
	AggregateVersion int64  `json:"aggregate_version"`
	EventSeq         int64  `json:"event_seq"`
	OccurredAtUnixUS int64  `json:"occurred_at_unix_us"`
}

// markerCanonicalV1 严格按设计文档顺序承载 Marker semantic digest 的全部且仅有字段。
type markerCanonicalV1 struct {
	SchemaVersion      string `json:"schema_version"`
	MarkerID           string `json:"marker_id"`
	SessionID          string `json:"session_id"`
	MarkerType         string `json:"marker_type"`
	EventSchemaVersion string `json:"event_schema_version"`
	SourceKind         string `json:"source_kind"`
	SourceID           string `json:"source_id"`
	ProjectionIndex    int64  `json:"projection_index"`
	AggregateType      string `json:"aggregate_type"`
	AggregateID        string `json:"aggregate_id"`
	AggregateVersion   int64  `json:"aggregate_version"`
	EventSeq           int64  `json:"event_seq"`
	PayloadDigest      string `json:"payload_digest"`
	OccurredAtUnixUS   int64  `json:"occurred_at_unix_us"`
}

// markerHelperMetadataV1 表示不得影响 Marker semantic digest 的迁移尝试信息。
type markerHelperMetadataV1 struct {
	MigrationRunID string `json:"migration_run_id"`
	Attempt        int64  `json:"attempt"`
	ClaimOwner     string `json:"claim_owner"`
}

// markerCaseV1 描述从基础事实施加确定性变换后的预期结论。
type markerCaseV1 struct {
	ID          string           `json:"id"`
	FromFixture string           `json:"from_fixture"`
	Mutations   []string         `json:"mutations"`
	Expected    markerExpectedV1 `json:"expected"`
}

// markerExpectedV1 固定单个向量的决策、稳定原因与成功摘要。
type markerExpectedV1 struct {
	Decision       string   `json:"decision"`
	ReasonCodes    []string `json:"reason_codes"`
	PayloadDigest  string   `json:"payload_digest"`
	SemanticDigest string   `json:"semantic_digest"`
}

// markerEvaluationV1 是 test-only evaluator 的确定性结果。
type markerEvaluationV1 struct {
	Decision       string
	ReasonCodes    []string
	PayloadDigest  string
	SemanticDigest string
}

// TestW2R02MarkerCorpusManifest 验证 manifest 对文件摘要、向量数量和目标测试的声明不会空跑。
func TestW2R02MarkerCorpusManifest(t *testing.T) {
	manifest := loadMarkerManifestV1(t)
	if manifest.SchemaVersion != "w2_r02_marker_manifest.v1" {
		t.Fatalf("Marker manifest schema=%q", manifest.SchemaVersion)
	}
	if len(manifest.Files) != 1 || manifest.TotalVectorCount != 19 {
		t.Fatalf("Marker manifest files=%d vectors=%d", len(manifest.Files), manifest.TotalVectorCount)
	}
	wantFixtureIDs := []string{"marker.accepted.enqueue", "marker.accepted.ensure", "marker.created.ensure"}
	if !reflect.DeepEqual(manifest.FixtureIDs, wantFixtureIDs) {
		t.Fatalf("Marker fixture ids=%v want=%v", manifest.FixtureIDs, wantFixtureIDs)
	}
	wantVectorIDs := []string{
		"EM-CAN-001-created-golden", "EM-CAN-002-ensure-accepted-golden", "EM-CAN-003-enqueue-accepted-golden",
		"EM-CAN-004-operational-metadata-excluded", "EM-CAN-005-schema-unknown", "EM-CAN-006-marker-id-invalid",
		"EM-CAN-007-event-seq-zero", "EM-CAN-008-source-projection-invalid", "EM-CAN-009-payload-unknown-field",
		"EM-CAN-010-payload-trailing-json", "EM-CAN-011-payload-identity-mismatch", "EM-CAN-012-payload-digest-tamper",
		"EM-CAN-013-semantic-digest-tamper", "EM-CAN-014-event-marker-binding-mismatch", "EM-CAN-015-payload-digest-uppercase",
		"EM-CAN-016-semantic-digest-nonhex", "EM-CAN-017-semantic-digest-short", "EM-CAN-018-occurred-at-zero",
		"EM-CAN-019-aggregate-version-zero",
	}
	if !reflect.DeepEqual(manifest.VectorIDs, wantVectorIDs) {
		t.Fatalf("Marker vector ids=%v want=%v", manifest.VectorIDs, wantVectorIDs)
	}
	wantTests := []string{
		"TestW2R02MarkerCorpusManifest",
		"TestSessionEventMarkerV1Corpus",
		"TestSessionEventMarkerV1ExactSets",
		"TestSessionEventMarkerV1GoldenDigests",
		"TestSessionEventMarkerV1EventBinding",
		"TestSessionEventMarkerV1PayloadCanonicalization",
		"TestSessionEventMarkerV1CanonicalFieldSensitivity",
		"TestSessionEventMarkerV1OperationalMetadataExcluded",
		"TestSessionEventMarkerV1StrictJSON",
	}
	if !reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("Marker target tests=%v want=%v", manifest.TargetTests, wantTests)
	}
	file := manifest.Files[0]
	if file.File != "session_event_marker_v1.json" || file.VectorCount != manifest.TotalVectorCount {
		t.Fatalf("Marker manifest file=%+v", file)
	}
	entries, err := w2R02MarkerFS.ReadDir("testdata/w2_r02_marker")
	if err != nil {
		t.Fatal(err)
	}
	entryNames := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("Marker testdata 不允许子目录: %s", entry.Name())
		}
		entryNames = append(entryNames, entry.Name())
	}
	if want := []string{"manifest.json", "session_event_marker_v1.json"}; !reflect.DeepEqual(entryNames, want) {
		t.Fatalf("Marker testdata files=%v want=%v", entryNames, want)
	}
	raw, err := w2R02MarkerFS.ReadFile("testdata/w2_r02_marker/" + file.File)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if got := "sha256:" + hex.EncodeToString(digest[:]); got != file.SHA256 {
		t.Fatalf("Marker corpus sha256=%s want=%s", got, file.SHA256)
	}
	corpus := loadMarkerCorpusV1(t)
	if len(corpus.Cases) != manifest.TotalVectorCount {
		t.Fatalf("Marker case count=%d want=%d", len(corpus.Cases), manifest.TotalVectorCount)
	}
	actualFixtureIDs := make([]string, 0, len(corpus.Fixtures))
	for _, fixture := range corpus.Fixtures {
		actualFixtureIDs = append(actualFixtureIDs, fixture.FixtureID)
	}
	sort.Strings(actualFixtureIDs)
	if !reflect.DeepEqual(actualFixtureIDs, manifest.FixtureIDs) {
		t.Fatalf("Marker corpus fixture ids=%v manifest=%v", actualFixtureIDs, manifest.FixtureIDs)
	}
	actualVectorIDs := make([]string, 0, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		actualVectorIDs = append(actualVectorIDs, testCase.ID)
	}
	sort.Strings(actualVectorIDs)
	if !reflect.DeepEqual(actualVectorIDs, manifest.VectorIDs) {
		t.Fatalf("Marker corpus vector ids=%v manifest=%v", actualVectorIDs, manifest.VectorIDs)
	}
}

// TestSessionEventMarkerV1Corpus 执行全部合法与失败关闭向量并要求 ID 唯一。
func TestSessionEventMarkerV1Corpus(t *testing.T) {
	corpus := loadMarkerCorpusV1(t)
	fixtures := make(map[string]markerFixtureV1, len(corpus.Fixtures))
	for _, fixture := range corpus.Fixtures {
		if _, exists := fixtures[fixture.FixtureID]; exists {
			t.Fatalf("重复 Marker fixture_id=%s", fixture.FixtureID)
		}
		fixtures[fixture.FixtureID] = fixture
	}
	seen := make(map[string]struct{}, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		testCase := testCase
		t.Run(testCase.ID, func(t *testing.T) {
			if _, exists := seen[testCase.ID]; exists {
				t.Fatalf("重复 Marker case id=%s", testCase.ID)
			}
			seen[testCase.ID] = struct{}{}
			fixture, exists := fixtures[testCase.FromFixture]
			if !exists {
				t.Fatalf("未知 Marker fixture=%s", testCase.FromFixture)
			}
			got := evaluateMarkerCaseV1(fixture, testCase.Mutations)
			want := markerEvaluationV1{
				Decision: testCase.Expected.Decision, ReasonCodes: testCase.Expected.ReasonCodes,
				PayloadDigest: testCase.Expected.PayloadDigest, SemanticDigest: testCase.Expected.SemanticDigest,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Marker evaluation=%+v want=%+v", got, want)
			}
		})
	}
}

// TestSessionEventMarkerV1ExactSets 固定 Marker 类型组合与稳定原因集合，防止静默放宽。
func TestSessionEventMarkerV1ExactSets(t *testing.T) {
	corpus := loadMarkerCorpusV1(t)
	want := markerExactSetsV1{
		Decisions:      []string{"valid", "invalid"},
		MarkerTypes:    []string{"session.created", "session.input.accepted"},
		SourceKinds:    []string{"ensure_project_session", "enqueue_input_v1"},
		AggregateTypes: []string{"session", "session_input"},
		ReasonCodes: []string{
			"MARKER_SCHEMA_INVALID", "MARKER_ID_INVALID", "MARKER_NUMERIC_INVALID",
			"MARKER_TYPE_COMBINATION_INVALID", "EVENT_MARKER_BINDING_MISMATCH", "EVENT_PAYLOAD_INVALID",
			"EVENT_PAYLOAD_BINDING_MISMATCH", "PAYLOAD_DIGEST_MISMATCH", "SEMANTIC_DIGEST_MISMATCH",
		},
	}
	if !reflect.DeepEqual(corpus.ExactSets, want) {
		t.Fatalf("Marker exact sets=%+v want=%+v", corpus.ExactSets, want)
	}
}

// TestSessionEventMarkerV1EventBinding 固定 Event 与 Marker 必须逐值复用的全部身份字段。
func TestSessionEventMarkerV1EventBinding(t *testing.T) {
	fixture := loadMarkerCorpusV1(t).Fixtures[0]
	if !markerEventBindingValidV1(fixture.Event, fixture.Canonical) {
		t.Fatal("合法 Event 与 Marker 未绑定")
	}
	mutations := []struct {
		name   string
		mutate func(*markerEventV1)
	}{
		{name: "event_id", mutate: func(v *markerEventV1) { v.EventID = "019f1000-0000-7000-8000-000000000099" }},
		{name: "session_id", mutate: func(v *markerEventV1) { v.SessionID = "019f1000-0000-7000-8000-000000000199" }},
		{name: "event_type", mutate: func(v *markerEventV1) { v.EventType += ".x" }},
		{name: "schema_version", mutate: func(v *markerEventV1) { v.SchemaVersion += ".x" }},
		{name: "source_kind", mutate: func(v *markerEventV1) { v.SourceKind += ".x" }},
		{name: "source_id", mutate: func(v *markerEventV1) { v.SourceID = "019f1000-0000-7000-8000-000000000299" }},
		{name: "projection_index", mutate: func(v *markerEventV1) { v.ProjectionIndex++ }},
		{name: "aggregate_type", mutate: func(v *markerEventV1) { v.AggregateType += ".x" }},
		{name: "aggregate_id", mutate: func(v *markerEventV1) { v.AggregateID = "019f1000-0000-7000-8000-000000000499" }},
		{name: "aggregate_version", mutate: func(v *markerEventV1) { v.AggregateVersion++ }},
		{name: "event_seq", mutate: func(v *markerEventV1) { v.EventSeq++ }},
		{name: "occurred_at_unix_us", mutate: func(v *markerEventV1) { v.OccurredAtUnixUS++ }},
	}
	for _, testCase := range mutations {
		t.Run(testCase.name, func(t *testing.T) {
			mutated := fixture.Event
			testCase.mutate(&mutated)
			if markerEventBindingValidV1(mutated, fixture.Canonical) {
				t.Fatalf("Event 字段 %s 未参与 Marker 绑定", testCase.name)
			}
		})
	}
}

// TestSessionEventMarkerV1PayloadCanonicalization 证明 Payload 摘要来自强类型重编码而非原始 JSON 字节。
func TestSessionEventMarkerV1PayloadCanonicalization(t *testing.T) {
	fixture := loadMarkerCorpusV1(t).Fixtures[0]
	if rawDigest := markerSHA256HexV1([]byte(fixture.PayloadJSON)); rawDigest == fixture.Canonical.PayloadDigest {
		t.Fatalf("fixture 原始 JSON 意外等于 canonical: %s", rawDigest)
	}
	canonical, bindingValid, err := markerPayloadCanonicalV1(fixture.PayloadJSON, fixture.Canonical)
	if err != nil || !bindingValid {
		t.Fatalf("Payload canonical err=%v binding=%t", err, bindingValid)
	}
	if got := markerSHA256HexV1(canonical); got != fixture.Canonical.PayloadDigest {
		t.Fatalf("Payload canonical digest=%s want=%s", got, fixture.Canonical.PayloadDigest)
	}
}

// TestSessionEventMarkerV1GoldenDigests 要求三类 Marker 都有稳定、无前缀的小写 SHA-256 golden。
func TestSessionEventMarkerV1GoldenDigests(t *testing.T) {
	corpus := loadMarkerCorpusV1(t)
	if len(corpus.Fixtures) != 3 {
		t.Fatalf("Marker golden fixtures=%d want=3", len(corpus.Fixtures))
	}
	for _, fixture := range corpus.Fixtures {
		payloadDigest, semanticDigest, reasons := validateMarkerFixtureV1(fixture)
		if len(reasons) != 0 {
			t.Fatalf("Marker fixture=%s reasons=%v", fixture.FixtureID, reasons)
		}
		if payloadDigest != fixture.Canonical.PayloadDigest || semanticDigest != fixture.StoredSemanticDigest {
			t.Fatalf("Marker fixture=%s payload=%s semantic=%s", fixture.FixtureID, payloadDigest, semanticDigest)
		}
	}
}

// TestSessionEventMarkerV1CanonicalFieldSensitivity 确保任一 semantic 字段变化都会改变摘要。
func TestSessionEventMarkerV1CanonicalFieldSensitivity(t *testing.T) {
	fixture := loadMarkerCorpusV1(t).Fixtures[1]
	base, err := markerSemanticDigestV1(fixture.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*markerCanonicalV1)
	}{
		{name: "schema_version", mutate: func(v *markerCanonicalV1) { v.SchemaVersion += ".x" }},
		{name: "marker_id", mutate: func(v *markerCanonicalV1) { v.MarkerID = "019f1000-0000-7000-8000-000000000099" }},
		{name: "session_id", mutate: func(v *markerCanonicalV1) { v.SessionID = "019f1000-0000-7000-8000-000000000199" }},
		{name: "marker_type", mutate: func(v *markerCanonicalV1) { v.MarkerType = "session.created" }},
		{name: "event_schema_version", mutate: func(v *markerCanonicalV1) { v.EventSchemaVersion += ".x" }},
		{name: "source_kind", mutate: func(v *markerCanonicalV1) { v.SourceKind = "enqueue_input_v1" }},
		{name: "source_id", mutate: func(v *markerCanonicalV1) { v.SourceID = "019f1000-0000-7000-8000-000000000299" }},
		{name: "projection_index", mutate: func(v *markerCanonicalV1) { v.ProjectionIndex++ }},
		{name: "aggregate_type", mutate: func(v *markerCanonicalV1) { v.AggregateType = "session" }},
		{name: "aggregate_id", mutate: func(v *markerCanonicalV1) { v.AggregateID = "019f1000-0000-7000-8000-000000000499" }},
		{name: "aggregate_version", mutate: func(v *markerCanonicalV1) { v.AggregateVersion++ }},
		{name: "event_seq", mutate: func(v *markerCanonicalV1) { v.EventSeq++ }},
		{name: "payload_digest", mutate: func(v *markerCanonicalV1) { v.PayloadDigest = markerAlternateDigestV1(v.PayloadDigest) }},
		{name: "occurred_at_unix_us", mutate: func(v *markerCanonicalV1) { v.OccurredAtUnixUS++ }},
	}
	for _, testCase := range mutations {
		t.Run(testCase.name, func(t *testing.T) {
			mutated := fixture.Canonical
			testCase.mutate(&mutated)
			got, err := markerSemanticDigestV1(mutated)
			if err != nil {
				t.Fatal(err)
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(base)) == 1 {
				t.Fatalf("字段 %s 未改变 Marker semantic digest", testCase.name)
			}
		})
	}
}

// TestSessionEventMarkerV1OperationalMetadataExcluded 验证 recorded/helper 技术元数据不会改变业务语义摘要。
func TestSessionEventMarkerV1OperationalMetadataExcluded(t *testing.T) {
	fixture := loadMarkerCorpusV1(t).Fixtures[0]
	base, err := markerSemanticDigestV1(fixture.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	fixture.RecordedAt = "2026-07-16T00:00:00Z"
	fixture.HelperMetadata.MigrationRunID = "019f1000-0000-7000-8000-000000000999"
	fixture.HelperMetadata.Attempt = 99
	fixture.HelperMetadata.ClaimOwner = "helper-b"
	got, err := markerSemanticDigestV1(fixture.Canonical)
	if err != nil {
		t.Fatal(err)
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(base)) != 1 {
		t.Fatalf("运维元数据改变 Marker digest: got=%s want=%s", got, base)
	}
}

// TestSessionEventMarkerV1StrictJSON 验证 Corpus 与 Event Payload 都拒绝未知字段和尾随 JSON。
func TestSessionEventMarkerV1StrictJSON(t *testing.T) {
	var payload agentevent.SessionCreatedPayload
	if err := markerStrictDecodeV1([]byte(`{"session_id":"s","project_id":"p","status":"active","version":1,"future":true}`), &payload); err == nil {
		t.Fatal("Event Payload 未拒绝未知字段")
	}
	if err := markerStrictDecodeV1([]byte(`{"session_id":"s","project_id":"p","status":"active","version":1}{}`), &payload); err == nil {
		t.Fatal("Event Payload 未拒绝尾随 JSON")
	}
	var manifest markerManifestV1
	if err := markerStrictDecodeV1([]byte(`{"schema_version":"x","files":[],"total_vector_count":0,"target_tests":[],"future":true}`), &manifest); err == nil {
		t.Fatal("Marker manifest 未拒绝未知字段")
	}
}

// loadMarkerManifestV1 严格读取并解码 Marker manifest；非法 UTF-8/JSON 直接失败。
func loadMarkerManifestV1(t *testing.T) markerManifestV1 {
	t.Helper()
	raw, err := w2R02MarkerFS.ReadFile(markerManifestPathV1)
	if err != nil {
		t.Fatal(err)
	}
	var manifest markerManifestV1
	if err := markerStrictDecodeV1(raw, &manifest); err != nil {
		t.Fatalf("Marker manifest 非法: %v", err)
	}
	return manifest
}

// loadMarkerCorpusV1 严格读取 Marker Corpus，并固定 schema 和 case ID 顺序。
func loadMarkerCorpusV1(t *testing.T) markerCorpusV1 {
	t.Helper()
	raw, err := w2R02MarkerFS.ReadFile(markerCorpusPathV1)
	if err != nil {
		t.Fatal(err)
	}
	var corpus markerCorpusV1
	if err := markerStrictDecodeV1(raw, &corpus); err != nil {
		t.Fatalf("Marker corpus 非法: %v", err)
	}
	if corpus.SchemaVersion != "session_event_marker_v1_corpus.v1" {
		t.Fatalf("Marker corpus schema=%q", corpus.SchemaVersion)
	}
	ids := make([]string, 0, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		ids = append(ids, testCase.ID)
	}
	if !sort.StringsAreSorted(ids) {
		t.Fatalf("Marker case ID 必须稳定升序: %v", ids)
	}
	return corpus
}

// evaluateMarkerCaseV1 应用白名单 mutation 后执行确定性校验；首个原因使用固定优先级。
func evaluateMarkerCaseV1(fixture markerFixtureV1, mutations []string) markerEvaluationV1 {
	for _, mutation := range mutations {
		switch mutation {
		case "operational_metadata_changed":
			fixture.RecordedAt = "2026-07-16T00:00:00Z"
			fixture.HelperMetadata.Attempt++
			fixture.HelperMetadata.ClaimOwner += "-changed"
		case "schema_unknown":
			fixture.Canonical.SchemaVersion = "session_event_marker.v2"
		case "marker_id_not_uuidv7":
			fixture.Canonical.MarkerID = "not-a-uuid"
		case "event_seq_zero":
			fixture.Canonical.EventSeq = 0
		case "source_projection_invalid":
			fixture.Canonical.ProjectionIndex = 9
		case "event_identity_mismatch":
			fixture.Event.SourceID = "019f1000-0000-7000-8000-000000000299"
		case "payload_unknown_field":
			fixture.PayloadJSON = markerAppendObjectFieldV1(fixture.PayloadJSON, `"future":true`)
		case "payload_trailing_json":
			fixture.PayloadJSON += `{}`
		case "payload_identity_mismatch":
			fixture.PayloadJSON = markerReplacePayloadIdentityV1(fixture)
		case "payload_digest_tamper":
			fixture.Canonical.PayloadDigest = markerAlternateDigestV1(fixture.Canonical.PayloadDigest)
		case "payload_digest_uppercase":
			fixture.Canonical.PayloadDigest = strings.ToUpper(fixture.Canonical.PayloadDigest)
		case "semantic_digest_tamper":
			fixture.StoredSemanticDigest = markerAlternateDigestV1(fixture.StoredSemanticDigest)
		case "semantic_digest_nonhex":
			fixture.StoredSemanticDigest = strings.Repeat("g", 64)
		case "semantic_digest_short":
			fixture.StoredSemanticDigest = "abc"
		case "occurred_at_zero":
			fixture.Canonical.OccurredAtUnixUS = 0
		case "aggregate_version_zero":
			fixture.Canonical.AggregateVersion = 0
		default:
			return markerEvaluationV1{Decision: "invalid", ReasonCodes: []string{"MARKER_SCHEMA_INVALID"}}
		}
	}
	payloadDigest, semanticDigest, reasons := validateMarkerFixtureV1(fixture)
	if len(reasons) != 0 {
		return markerEvaluationV1{Decision: "invalid", ReasonCodes: reasons}
	}
	return markerEvaluationV1{
		Decision: "valid", ReasonCodes: []string{}, PayloadDigest: payloadDigest, SemanticDigest: semanticDigest,
	}
}

// validateMarkerFixtureV1 按 schema、ID、数值、类型组合、Payload 绑定和两级摘要顺序失败关闭。
func validateMarkerFixtureV1(fixture markerFixtureV1) (string, string, []string) {
	canonical := fixture.Canonical
	if canonical.SchemaVersion != "session_event_marker.v1" || canonical.EventSchemaVersion != agentevent.SchemaVersionV1 {
		return "", "", []string{"MARKER_SCHEMA_INVALID"}
	}
	for _, value := range []string{canonical.MarkerID, canonical.SessionID, canonical.SourceID, canonical.AggregateID} {
		if !markerCanonicalUUIDv7V1(value) {
			return "", "", []string{"MARKER_ID_INVALID"}
		}
	}
	if canonical.ProjectionIndex < 0 || canonical.AggregateVersion <= 0 || canonical.EventSeq <= 0 || canonical.OccurredAtUnixUS <= 0 {
		return "", "", []string{"MARKER_NUMERIC_INVALID"}
	}
	if !markerTypeCombinationValidV1(canonical) {
		return "", "", []string{"MARKER_TYPE_COMBINATION_INVALID"}
	}
	if !markerEventBindingValidV1(fixture.Event, canonical) {
		return "", "", []string{"EVENT_MARKER_BINDING_MISMATCH"}
	}
	payloadCanonical, bindingValid, err := markerPayloadCanonicalV1(fixture.PayloadJSON, canonical)
	if err != nil {
		return "", "", []string{"EVENT_PAYLOAD_INVALID"}
	}
	if !bindingValid {
		return "", "", []string{"EVENT_PAYLOAD_BINDING_MISMATCH"}
	}
	payloadDigest := markerSHA256HexV1(payloadCanonical)
	if !markerDigestPatternV1.MatchString(canonical.PayloadDigest) ||
		subtle.ConstantTimeCompare([]byte(payloadDigest), []byte(canonical.PayloadDigest)) != 1 {
		return "", "", []string{"PAYLOAD_DIGEST_MISMATCH"}
	}
	semanticDigest, err := markerSemanticDigestV1(canonical)
	if err != nil {
		return "", "", []string{"MARKER_SCHEMA_INVALID"}
	}
	if !markerDigestPatternV1.MatchString(fixture.StoredSemanticDigest) ||
		subtle.ConstantTimeCompare([]byte(semanticDigest), []byte(fixture.StoredSemanticDigest)) != 1 {
		return "", "", []string{"SEMANTIC_DIGEST_MISMATCH"}
	}
	return payloadDigest, semanticDigest, nil
}

// markerEventBindingValidV1 要求 Marker 精确复用原 Event 身份、来源、聚合、序号和发生时间。
func markerEventBindingValidV1(event markerEventV1, marker markerCanonicalV1) bool {
	return event.EventID == marker.MarkerID && event.SessionID == marker.SessionID &&
		event.EventType == marker.MarkerType && event.SchemaVersion == marker.EventSchemaVersion &&
		event.SourceKind == marker.SourceKind && event.SourceID == marker.SourceID &&
		event.ProjectionIndex == marker.ProjectionIndex && event.AggregateType == marker.AggregateType &&
		event.AggregateID == marker.AggregateID && event.AggregateVersion == marker.AggregateVersion &&
		event.EventSeq == marker.EventSeq && event.OccurredAtUnixUS == marker.OccurredAtUnixUS
}

// markerTypeCombinationValidV1 校验 created/accepted 与 Source、Projection、Aggregate 的精确组合。
func markerTypeCombinationValidV1(value markerCanonicalV1) bool {
	switch value.MarkerType {
	case string(agentevent.TypeSessionCreated):
		return value.SourceKind == agentevent.SourceKindEnsureProjectSession && value.ProjectionIndex == 0 &&
			value.AggregateType == string(agentevent.AggregateTypeSession) && value.AggregateID == value.SessionID
	case string(agentevent.TypeSessionInputAccepted):
		sourceValid := (value.SourceKind == agentevent.SourceKindEnsureProjectSession && value.ProjectionIndex == 1) ||
			(value.SourceKind == "enqueue_input_v1" && value.ProjectionIndex == 0)
		return sourceValid && value.AggregateType == string(agentevent.AggregateTypeSessionInput)
	default:
		return false
	}
}

// markerPayloadCanonicalV1 严格解码强类型 Event Payload、验证身份绑定并按 DTO 字段顺序重编码。
func markerPayloadCanonicalV1(raw string, marker markerCanonicalV1) ([]byte, bool, error) {
	switch marker.MarkerType {
	case string(agentevent.TypeSessionCreated):
		var payload agentevent.SessionCreatedPayload
		if err := markerStrictDecodeV1([]byte(raw), &payload); err != nil {
			return nil, false, err
		}
		encoded, err := json.Marshal(payload)
		binding := markerCanonicalUUIDv7V1(payload.ProjectID) && payload.SessionID == marker.SessionID &&
			payload.Status == "active" && payload.Version == marker.AggregateVersion
		return encoded, binding, err
	case string(agentevent.TypeSessionInputAccepted):
		var payload agentevent.SessionInputAcceptedPayload
		if err := markerStrictDecodeV1([]byte(raw), &payload); err != nil {
			return nil, false, err
		}
		encoded, err := json.Marshal(payload)
		binding := payload.SessionID == marker.SessionID && payload.InputID == marker.AggregateID &&
			markerCanonicalUUIDv7V1(payload.MessageID) && payload.EnqueueSeq > 0 && payload.Status == "pending"
		return encoded, binding, err
	default:
		return nil, false, fmt.Errorf("unsupported marker type")
	}
}

// markerSemanticDigestV1 使用固定域和单字节零分隔符计算无前缀小写摘要。
func markerSemanticDigestV1(canonical markerCanonicalV1) (string, error) {
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal Marker canonical: %w", err)
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(markerDigestDomainV1))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(raw)
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// markerSHA256HexV1 计算 Payload canonical 的裸 SHA-256 小写十六进制。
func markerSHA256HexV1(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// markerCanonicalUUIDv7V1 要求 UUID 字符串同时满足版本、variant 和 canonical 小写格式。
func markerCanonicalUUIDv7V1(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.Version() == 7 && parsed.Variant() == uuid.RFC4122 && parsed.String() == value
}

// markerStrictDecodeV1 严格拒绝未知字段、尾随 JSON 和非单一 JSON 值。
func markerStrictDecodeV1(raw []byte, target any) error {
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

// markerAlternateDigestV1 生成同形但不同值的摘要，用于稳定篡改向量。
func markerAlternateDigestV1(value string) string {
	if len(value) != 64 {
		return "f" + value
	}
	if value[0] == 'f' {
		return "e" + value[1:]
	}
	return "f" + value[1:]
}

// markerAppendObjectFieldV1 在对象尾部插入未知字段，保持 JSON 语法合法。
func markerAppendObjectFieldV1(raw, field string) string {
	if len(raw) == 0 || raw[len(raw)-1] != '}' {
		return raw + field
	}
	return raw[:len(raw)-1] + "," + field + "}"
}

// markerReplacePayloadIdentityV1 构造语法合法但与 Marker Aggregate 不一致的强类型 Payload。
func markerReplacePayloadIdentityV1(fixture markerFixtureV1) string {
	if fixture.Canonical.MarkerType == string(agentevent.TypeSessionCreated) {
		var payload agentevent.SessionCreatedPayload
		_ = markerStrictDecodeV1([]byte(fixture.PayloadJSON), &payload)
		payload.SessionID = "019f1000-0000-7000-8000-000000000199"
		raw, _ := json.Marshal(payload)
		return string(raw)
	}
	var payload agentevent.SessionInputAcceptedPayload
	_ = markerStrictDecodeV1([]byte(fixture.PayloadJSON), &payload)
	payload.InputID = "019f1000-0000-7000-8000-000000000499"
	raw, _ := json.Marshal(payload)
	return string(raw)
}
