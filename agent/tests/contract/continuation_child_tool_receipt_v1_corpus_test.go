// Package contract_test 只承载未 Approved 的 Candidate Activation child ToolReceipt 候选语料，
// 不提供生产 Receipt、Approval、Runner、Migration、Graph Node 或 Business RPC 实现。
package contract_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	continuationChildCorpusPathV1     = "testdata/w2_r04_continuation_child/continuation_child_tool_receipt_v1.json"
	continuationChildManifestPathV1   = "testdata/w2_r04_continuation_child/manifest.json"
	continuationChildDigestDomainV1   = "dora.continuation_child_tool_receipt_request.v1"
	continuationChildResultDomainV1   = "dora.continuation_child_tool_receipt_result.v1"
	continuationChildBusinessDomainV1 = "dora.creation_spec_candidate_decide_request.v1"
	continuationChildVectorCountV1    = 172
)

type continuationChildManifestV1 struct {
	SchemaVersion    string                     `json:"schema_version"`
	Files            []messageSetManifestFileV1 `json:"files"`
	FixtureIDs       []string                   `json:"fixture_ids"`
	VectorIDs        []string                   `json:"vector_ids"`
	TotalVectorCount int                        `json:"total_vector_count"`
	TargetTests      []string                   `json:"target_tests"`
}

// continuationChildCorpusV1 是 child 因果、动作槽和恢复状态机的测试专用机器真源。
type continuationChildCorpusV1 struct {
	SchemaVersion string                       `json:"schema_version"`
	ExactSets     continuationChildExactSetsV1 `json:"exact_sets"`
	Fixture       continuationChildFixtureV1   `json:"fixture"`
	Cases         []continuationChildCaseV1    `json:"cases"`
}

type continuationChildExactSetsV1 struct {
	Outcomes         []string `json:"outcomes"`
	Actions          []string `json:"actions"`
	WriteStates      []string `json:"write_states"`
	ExecutionPhases  []string `json:"execution_phases"`
	LogicalSlotRoles []string `json:"logical_slot_roles"`
	AuthorityOutcome []string `json:"authority_outcomes"`
	CaseKinds        []string `json:"case_kinds"`
}

// continuationChildFixtureV1 聚合 request canonical 外的受信 authority 与纯状态机观察。
type continuationChildFixtureV1 struct {
	FixtureID            string                                 `json:"fixture_id"`
	Request              continuationChildRequestV1             `json:"request"`
	ClaimedRequestDigest string                                 `json:"claimed_request_digest"`
	CurrentReceiptID     string                                 `json:"current_receipt_id"`
	Authorities          continuationChildAuthoritiesV1         `json:"authorities"`
	Scenario             continuationChildScenarioV1            `json:"scenario"`
	OperationalMetadata  continuationChildOperationalMetadataV1 `json:"operational_metadata"`
}

// continuationChildRequestV1 固定十个顶层字段和五十二个必填叶子字段。
type continuationChildRequestV1 struct {
	SchemaVersion      string                                `json:"schema_version"`
	EffectKind         string                                `json:"effect_kind"`
	Scope              continuationChildScopeV1              `json:"scope"`
	ChildKey           continuationChildKeyV1                `json:"child_key"`
	Causal             continuationChildCausalV1             `json:"causal"`
	Root               continuationChildRootV1               `json:"root"`
	Parent             continuationChildParentV1             `json:"parent"`
	Approval           continuationChildApprovalV1           `json:"approval"`
	Decision           continuationChildDecisionV1           `json:"decision"`
	InheritedExecution continuationChildInheritedExecutionV1 `json:"inherited_execution"`
}

type continuationChildScopeV1 struct {
	UserID    string `json:"user_id"`
	ProjectID string `json:"project_id"`
	SessionID string `json:"session_id"`
}

type continuationChildKeyV1 struct {
	SessionID          string `json:"session_id"`
	ContinuationTurnID string `json:"continuation_turn_id"`
	OriginalToolCallID string `json:"original_tool_call_id"`
}

type continuationChildCausalV1 struct {
	ContinuationSourceID string `json:"continuation_source_id"`
	InputID              string `json:"input_id"`
	TurnID               string `json:"turn_id"`
	RunID                string `json:"run_id"`
}

type continuationChildRootV1 struct {
	ToolReceiptID         string `json:"tool_receipt_id"`
	ToolReceiptVersion    int64  `json:"tool_receipt_version"`
	TurnID                string `json:"turn_id"`
	RunID                 string `json:"run_id"`
	RequestSemanticDigest string `json:"request_semantic_digest"`
}

type continuationChildParentV1 struct {
	ToolReceiptID         string `json:"tool_receipt_id"`
	ToolReceiptVersion    int64  `json:"tool_receipt_version"`
	TurnID                string `json:"turn_id"`
	RunID                 string `json:"run_id"`
	RequestSemanticDigest string `json:"request_semantic_digest"`
	ResultStatus          string `json:"result_status"`
	ResultDigest          string `json:"result_digest"`
}

type continuationChildApprovalV1 struct {
	ApprovalType             string `json:"approval_type"`
	ApprovalID               string `json:"approval_id"`
	PresentedApprovalVersion int64  `json:"presented_approval_version"`
	ResultingApprovalVersion int64  `json:"resulting_approval_version"`
	BindingDigest            string `json:"binding_digest"`
}

type continuationChildDecisionV1 struct {
	DecisionReceiptID string `json:"decision_receipt_id"`
	DecisionDigest    string `json:"decision_digest"`
	DecisionID        string `json:"decision_id"`
	Action            string `json:"action"`
	CardID            string `json:"card_id"`
	CardRevision      int64  `json:"card_revision"`
	ActorUserID       string `json:"actor_user_id"`
	ActorProjectID    string `json:"actor_project_id"`
}

type continuationChildInheritedExecutionV1 struct {
	IntentDigest            string `json:"intent_digest"`
	ToolPinOwner            string `json:"tool_pin_owner"`
	ToolPinRef              string `json:"tool_pin_ref"`
	ToolPinDigest           string `json:"tool_pin_digest"`
	ToolKey                 string `json:"tool_key"`
	DefinitionVersion       string `json:"definition_version"`
	IntentSchemaVersion     string `json:"intent_schema_version"`
	ResultSchemaVersion     string `json:"result_schema_version"`
	GraphKey                string `json:"graph_key"`
	ExecutionDigest         string `json:"execution_digest"`
	ParentTurnContextDigest string `json:"parent_turn_context_digest"`
	ResourceID              string `json:"resource_id"`
	ResourceVersion         int64  `json:"resource_version"`
	ResourceDigest          string `json:"resource_digest"`
	TargetExactSetDigest    string `json:"target_exact_set_digest"`
}

type continuationChildAuthoritiesV1 struct {
	AuthenticatedIdentity continuationChildScopeV1            `json:"authenticated_identity"`
	SourceMapping         continuationChildSourceMappingV1    `json:"source_mapping"`
	Root                  continuationChildReceiptAuthorityV1 `json:"root"`
	Parent                continuationChildReceiptAuthorityV1 `json:"parent"`
	ApprovalState         string                              `json:"approval_state"`
	DecisionState         string                              `json:"decision_state"`
	FirstWriteAvailable   bool                                `json:"first_write_available"`
}

type continuationChildSourceMappingV1 struct {
	ContinuationSourceID string `json:"continuation_source_id"`
	InputID              string `json:"input_id"`
	TurnID               string `json:"turn_id"`
	RunID                string `json:"run_id"`
	ApprovalID           string `json:"approval_id"`
	DecisionID           string `json:"decision_id"`
	Action               string `json:"action"`
}

// continuationChildReceiptAuthorityV1 显式保存 turn_kind 与 root ref；它们不进入 child request digest。
type continuationChildReceiptAuthorityV1 struct {
	ReceiptID          string `json:"receipt_id"`
	ReceiptVersion     int64  `json:"receipt_version"`
	TurnID             string `json:"turn_id"`
	RunID              string `json:"run_id"`
	RequestDigest      string `json:"request_digest"`
	TurnKind           string `json:"turn_kind"`
	WriteState         string `json:"write_state"`
	RootToolReceiptID  string `json:"root_tool_receipt_id"`
	RootReceiptVersion int64  `json:"root_receipt_version"`
	OriginalToolCallID string `json:"original_tool_call_id"`
	ApprovalID         string `json:"approval_id"`
	ApprovalVersion    int64  `json:"approval_version"`
	ApprovalDigest     string `json:"approval_digest"`
	ResultStatus       string `json:"result_status"`
	ResultDigest       string `json:"result_digest"`
}

type continuationChildScenarioV1 struct {
	ExistingChild        string `json:"existing_child"`
	StoredMutation       string `json:"stored_mutation"`
	PreGuard             string `json:"pre_guard"`
	Consumption          string `json:"consumption"`
	PostConsumptionGuard string `json:"post_consumption_guard"`
	BusinessObservation  string `json:"business_observation"`
	InvariantViolation   string `json:"invariant_violation"`
	PresentedFence       int64  `json:"presented_fence"`
	CurrentFence         int64  `json:"current_fence"`
}

type continuationChildOperationalMetadataV1 struct {
	TraceID              string `json:"trace_id"`
	Attempt              int64  `json:"attempt"`
	Processor            string `json:"processor"`
	LeaseID              string `json:"lease_id"`
	ReadAt               string `json:"read_at"`
	ExpectedChildVersion int64  `json:"expected_child_version"`
}

type continuationChildCaseV1 struct {
	ID        string                      `json:"id"`
	Kind      string                      `json:"kind"`
	Mutations []string                    `json:"mutations"`
	Expected  continuationChildExpectedV1 `json:"expected"`
}

type continuationChildExpectedV1 struct {
	Outcome          string   `json:"outcome"`
	Code             string   `json:"code"`
	LogicalSlotRoles []string `json:"logical_slot_roles"`
	BusinessWrites   int      `json:"business_writes"`
	BusinessQueries  int      `json:"business_queries"`
	FreezeCount      int      `json:"freeze_count"`
}

type continuationChildLogicalSlotV1 struct {
	Role             string
	EffectClass      string
	RefType          string
	RefSchemaVersion string
	AuthorityOwner   string
	IdempotencyKey   string
	RequestDigest    string
	QueryContract    string
	ResolutionState  string
	AuthorityID      string
	AuthorityDigest  string
	AuthorityOutcome string
}

type continuationChildSnapshotV1 struct {
	ReceiptID        string
	ReceiptVersion   int64
	Scope            continuationChildScopeV1
	ChildKey         continuationChildKeyV1
	RequestCanonical continuationChildRequestV1
	RequestDigest    string
	RootReceiptID    string
	ParentReceiptID  string
	WriteState       string
	ExecutionPhase   string
	OwnerFence       int64
	Slots            []continuationChildLogicalSlotV1
	ResultStatus     string
	ResultCode       string
	ResultRefRoles   []string
	ResultDigest     string
}

// continuationChildResultProjectionV1 固定 frozen child 的最小结果摘要，避免仅比较 role 名称掩盖存量损坏。
type continuationChildResultProjectionV1 struct {
	ReceiptID      string   `json:"receipt_id"`
	RequestDigest  string   `json:"request_digest"`
	Status         string   `json:"status"`
	Code           string   `json:"code"`
	ResultRefRoles []string `json:"result_ref_roles"`
}

// continuationChildBusinessRequestV1 固定 test-only Business slot request digest；它与 child request digest 分域。
type continuationChildBusinessRequestV1 struct {
	Action                     string `json:"action"`
	ApprovalID                 string `json:"approval_id"`
	DecisionID                 string `json:"decision_id"`
	DecisionDigest             string `json:"decision_digest"`
	ConsumptionAuthorityDigest string `json:"consumption_authority_digest,omitempty"`
	UserID                     string `json:"user_id"`
	ProjectID                  string `json:"project_id"`
	ResourceID                 string `json:"resource_id"`
	ResourceVersion            int64  `json:"resource_version"`
	ResourceDigest             string `json:"resource_digest"`
	TargetExactSetDigest       string `json:"target_exact_set_digest"`
}

type continuationChildEvaluationV1 struct {
	Outcome         string
	Code            string
	Snapshot        continuationChildSnapshotV1
	BusinessWrites  int
	BusinessQueries int
	FreezeCount     int
	RequestDigest   string
}

func TestW2R04ContinuationChildToolReceiptManifest(t *testing.T) {
	manifest := loadContinuationChildManifestV1(t)
	wantTests := []string{
		"TestW2R04ContinuationChildToolReceiptManifest",
		"TestContinuationChildToolReceiptV1Corpus",
		"TestContinuationChildToolReceiptV1GoldenDigest",
		"TestContinuationChildToolReceiptV1CausalBindings",
		"TestContinuationChildToolReceiptV1ActionSlotExactSets",
		"TestContinuationChildToolReceiptV1ReplayAndRecovery",
		"TestContinuationChildToolReceiptV1ReasonPriority",
		"TestContinuationChildToolReceiptV1StrictJSON",
		"TestContinuationChildToolReceiptV1CanonicalFieldSensitivity",
		"TestContinuationChildToolReceiptV1OperationalMetadataExcluded",
		"TestContinuationChildToolReceiptV1R01R04Bridge",
	}
	if manifest.SchemaVersion != "w2_r04_continuation_child_manifest.v1" || manifest.TotalVectorCount != continuationChildVectorCountV1 || !reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("child manifest 版本、数量或目标测试漂移: %+v", manifest)
	}
	actualTests := contractManifestTargetTestNamesV1(t, []string{"continuation_child_tool_receipt_v1_corpus_test.go"})
	sortedWant := append([]string(nil), wantTests...)
	sort.Strings(sortedWant)
	if !reflect.DeepEqual(actualTests, sortedWant) {
		t.Fatalf("child target tests 未绑定 AST actual=%v want=%v", actualTests, sortedWant)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].File != filepath.Base(continuationChildCorpusPathV1) || manifest.Files[0].VectorCount != continuationChildVectorCountV1 {
		t.Fatalf("child manifest files=%+v", manifest.Files)
	}
	raw, err := os.ReadFile(continuationChildCorpusPathV1)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != manifest.Files[0].SHA256 {
		t.Fatalf("child corpus raw sha=%s want=%s", got, manifest.Files[0].SHA256)
	}
	corpus := loadContinuationChildCorpusV1(t)
	if !reflect.DeepEqual(manifest.FixtureIDs, []string{corpus.Fixture.FixtureID}) || len(corpus.Cases) != manifest.TotalVectorCount {
		t.Fatal("child manifest 未绑定 fixture/case count")
	}
	ids := continuationChildCaseIDsV1(corpus)
	if !reflect.DeepEqual(ids, manifest.VectorIDs) {
		t.Fatalf("child vector ids drift got=%v want=%v", ids, manifest.VectorIDs)
	}
}

func TestContinuationChildToolReceiptV1Corpus(t *testing.T) {
	corpus := loadContinuationChildCorpusV1(t)
	seen := map[string]struct{}{}
	for _, testCase := range corpus.Cases {
		testCase := testCase
		t.Run(testCase.ID, func(t *testing.T) {
			if _, duplicate := seen[testCase.ID]; duplicate {
				t.Fatalf("重复 vector=%s", testCase.ID)
			}
			seen[testCase.ID] = struct{}{}
			got := evaluateContinuationChildCaseV1(t, corpus.Fixture, testCase)
			roles := continuationChildSlotRolesV1(got.Snapshot)
			if got.Outcome != testCase.Expected.Outcome || got.Code != testCase.Expected.Code ||
				!reflect.DeepEqual(roles, testCase.Expected.LogicalSlotRoles) || got.BusinessWrites != testCase.Expected.BusinessWrites ||
				got.BusinessQueries != testCase.Expected.BusinessQueries || got.FreezeCount != testCase.Expected.FreezeCount {
				t.Fatalf("evaluation=%+v roles=%v want=%+v", got, roles, testCase.Expected)
			}
		})
	}
	if len(seen) != continuationChildVectorCountV1 {
		t.Fatalf("child vector count=%d want=%d", len(seen), continuationChildVectorCountV1)
	}
}

func TestContinuationChildToolReceiptV1GoldenDigest(t *testing.T) {
	corpus := loadContinuationChildCorpusV1(t)
	digest, err := continuationChildRequestDigestV1(corpus.Fixture.Request)
	if err != nil || digest != corpus.Fixture.ClaimedRequestDigest {
		t.Fatalf("child golden digest=%s want=%s err=%v", digest, corpus.Fixture.ClaimedRequestDigest, err)
	}
	if digest == corpus.Fixture.Request.Parent.RequestSemanticDigest {
		t.Fatal("current child digest 不得退化为 parent request digest")
	}
}

func TestContinuationChildToolReceiptV1CausalBindings(t *testing.T) {
	corpus := loadContinuationChildCorpusV1(t)
	for _, prefix := range []string{"CCT-P01-", "CCT-P02-", "CCT-P03-", "CCT-N03-", "CCT-N04-", "CCT-N05-", "CCT-N06-", "CCT-N07-", "CCT-N08-", "CCT-N09-", "CCT-N10-", "CCT-N11-", "CCT-N12-", "CCT-N13-", "CCT-N14-", "CCT-N15-", "CCT-N16-", "CCT-N17-", "CCT-N18-", "CCT-N19-"} {
		if !continuationChildHasCasePrefixV1(corpus, prefix) {
			t.Fatalf("缺少 causal vector prefix=%s", prefix)
		}
	}
}

func TestContinuationChildToolReceiptV1ActionSlotExactSets(t *testing.T) {
	corpus := loadContinuationChildCorpusV1(t)
	want := continuationChildExactSetsV1{
		Outcomes:         []string{"digest_changed", "frozen", "recovery_pending", "rejected", "replayed", "validated"},
		Actions:          []string{"approve", "reject"},
		WriteStates:      []string{"frozen", "open"},
		ExecutionPhases:  []string{"claimed", "in_progress", "recovery_pending"},
		LogicalSlotRoles: []string{"approval_consumption", "approval_decision", "business_decide"},
		AuthorityOutcome: []string{"committed", "not_committed"},
		CaseKinds:        []string{"binding", "bridge", "digest_sensitivity", "flow", "priority", "state", "strict_json"},
	}
	if !reflect.DeepEqual(corpus.ExactSets, want) {
		t.Fatalf("child exact sets=%+v want=%+v", corpus.ExactSets, want)
	}
	assertContinuationChildCaseV1(t, corpus, "CCT-P01-first-approve", "frozen", []string{"approval_decision", "approval_consumption", "business_decide"})
	assertContinuationChildCaseV1(t, corpus, "CCT-P03-reject-committed", "frozen", []string{"approval_decision", "business_decide"})
	assertContinuationChildCaseV1(t, corpus, "CCT-P10-approve-pre-guard-failed", "frozen", []string{"approval_decision"})
	assertContinuationChildCaseV1(t, corpus, "CCT-P11-approve-post-consumption-failed", "frozen", []string{"approval_decision", "approval_consumption"})
}

func TestContinuationChildToolReceiptV1ReplayAndRecovery(t *testing.T) {
	corpus := loadContinuationChildCorpusV1(t)
	for _, id := range []string{"CCT-P04-frozen-replay", "CCT-P05-write-lost-query-completed", "CCT-P06-higher-fence-query-only", "CCT-P07-not-found-late-completed", "CCT-P08-query-timeout-completed", "CCT-P09-resolve-lost-authority-replay", "CCT-P16-consumption-transport-unknown", "CCT-P17-consumption-query-not-found"} {
		testCase := continuationChildCaseByIDV1(t, corpus, id)
		got := evaluateContinuationChildCaseV1(t, corpus.Fixture, testCase)
		if got.BusinessWrites > 1 || got.FreezeCount > 1 {
			t.Fatalf("%s 产生第二业务意图或重复冻结: %+v", id, got)
		}
		if strings.Contains(id, "higher-fence") && got.BusinessWrites != 0 {
			t.Fatalf("高 Fence 接管只能 Query: %+v", got)
		}
	}
}

func TestContinuationChildToolReceiptV1ReasonPriority(t *testing.T) {
	corpus := loadContinuationChildCorpusV1(t)
	count := 0
	for _, testCase := range corpus.Cases {
		if testCase.Kind != "priority" {
			continue
		}
		count++
		got := evaluateContinuationChildCaseV1(t, corpus.Fixture, testCase)
		if got.Code != testCase.Expected.Code {
			t.Fatalf("%s priority code=%s want=%s", testCase.ID, got.Code, testCase.Expected.Code)
		}
	}
	if count != 25 {
		t.Fatalf("priority vectors=%d want=25", count)
	}
}

func TestContinuationChildToolReceiptV1StrictJSON(t *testing.T) {
	corpus := loadContinuationChildCorpusV1(t)
	raw, err := canonicalJSON(corpus.Fixture.Request)
	if err != nil {
		t.Fatal(err)
	}
	if err := strictContinuationChildRequestV1(raw); err != nil {
		t.Fatalf("golden request strict decode: %v", err)
	}
	variants := map[string][]byte{
		"malformed":   []byte(`{"schema_version":`),
		"unknown":     []byte(strings.Replace(string(raw), "{", `{"unknown":true,`, 1)),
		"duplicate":   []byte(strings.Replace(string(raw), `"effect_kind":"creation_spec_activation"`, `"effect_kind":"creation_spec_activation","effect_kind":"creation_spec_activation"`, 1)),
		"null nested": []byte(strings.Replace(string(raw), `"scope":{`, `"scope":null,"ignored_scope":{`, 1)),
		"trailing":    append(append([]byte(nil), raw...), []byte(`{}`)...),
	}
	for name, candidate := range variants {
		t.Run(name, func(t *testing.T) {
			if err := strictContinuationChildRequestV1(candidate); err == nil {
				t.Fatalf("strict decoder accepted %s", name)
			}
		})
	}
}

func TestContinuationChildToolReceiptV1CanonicalFieldSensitivity(t *testing.T) {
	corpus := loadContinuationChildCorpusV1(t)
	baseDigest, err := continuationChildRequestDigestV1(corpus.Fixture.Request)
	if err != nil {
		t.Fatal(err)
	}
	paths := continuationChildRequestLeafPathsV1()
	if len(paths) != 52 {
		t.Fatalf("canonical leaf paths=%d want=52", len(paths))
	}
	seen := map[string]struct{}{}
	for _, path := range paths {
		mutated := cloneContinuationChildRequestV1(corpus.Fixture.Request)
		mutateContinuationChildRequestPathV1(t, &mutated, path)
		digest, digestErr := continuationChildRequestDigestV1(mutated)
		if digestErr != nil || digest == baseDigest {
			t.Fatalf("path=%s digest=%s base=%s err=%v", path, digest, baseDigest, digestErr)
		}
		seen[path] = struct{}{}
	}
	if len(seen) != 52 {
		t.Fatalf("sensitivity path exact-set=%d", len(seen))
	}
}

func TestContinuationChildToolReceiptV1OperationalMetadataExcluded(t *testing.T) {
	corpus := loadContinuationChildCorpusV1(t)
	before, err := continuationChildRequestDigestV1(corpus.Fixture.Request)
	if err != nil {
		t.Fatal(err)
	}
	fixture := cloneContinuationChildFixtureV1(corpus.Fixture)
	fixture.CurrentReceiptID = "019f4600-0000-7000-8000-000000009999"
	fixture.OperationalMetadata = continuationChildOperationalMetadataV1{
		TraceID: "trace-z", Attempt: 99, Processor: "processor-z", LeaseID: "lease-z", ReadAt: "2026-07-16T10:11:12Z", ExpectedChildVersion: 77,
	}
	fixture.Scenario.CurrentFence = 999
	fixture.Scenario.PresentedFence = 999
	after, digestErr := continuationChildRequestDigestV1(fixture.Request)
	if digestErr != nil || before != after {
		t.Fatalf("operational metadata changed request digest before=%s after=%s err=%v", before, after, digestErr)
	}
}

func TestContinuationChildToolReceiptV1R01R04Bridge(t *testing.T) {
	corpus := loadContinuationChildCorpusV1(t)
	for _, id := range []string{
		"CCT-B01-r04-consumption-record-bridge", "CCT-B02-r01-approve-not-committed-bridge",
		"CCT-B03-r01-reject-not-committed-bridge", "CCT-B04-r04-consumption-conflict-bridge",
		"CCT-B05-r04-consumption-not-found-bridge", "CCT-B06-r01-business-unknown-bridge",
	} {
		testCase := continuationChildCaseByIDV1(t, corpus, id)
		got := evaluateContinuationChildCaseV1(t, corpus.Fixture, testCase)
		if got.Outcome != "validated" || got.Code != "" {
			t.Fatalf("bridge case=%s evaluation=%+v", id, got)
		}
	}
	approved := evaluateContinuationChildCaseV1(t, corpus.Fixture, continuationChildCaseByIDV1(t, corpus, "CCT-P19-approve-business-not-committed"))
	if !reflect.DeepEqual(approved.Snapshot.ResultRefRoles, []string{"approval_consumption", "business_decide"}) {
		t.Fatalf("Decision evidence-only 不得进入 R01 failed-after result refs: %+v", approved.Snapshot.ResultRefRoles)
	}
}

func loadContinuationChildManifestV1(t *testing.T) continuationChildManifestV1 {
	t.Helper()
	raw, err := os.ReadFile(continuationChildManifestPathV1)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("child manifest JSON: %v", err)
	}
	var manifest continuationChildManifestV1
	if err := strictDecode(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func loadContinuationChildCorpusV1(t *testing.T) continuationChildCorpusV1 {
	t.Helper()
	raw, err := os.ReadFile(continuationChildCorpusPathV1)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("child corpus JSON: %v", err)
	}
	var corpus continuationChildCorpusV1
	if err := strictDecode(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != "continuation_child_tool_receipt_v1_corpus.v1" || len(corpus.Cases) != continuationChildVectorCountV1 {
		t.Fatalf("child corpus version=%s cases=%d", corpus.SchemaVersion, len(corpus.Cases))
	}
	return corpus
}

func evaluateContinuationChildCaseV1(t *testing.T, base continuationChildFixtureV1, testCase continuationChildCaseV1) continuationChildEvaluationV1 {
	t.Helper()
	if testCase.Kind == "strict_json" {
		return evaluateContinuationChildStrictCaseV1(t, base, testCase)
	}
	if testCase.Kind == "digest_sensitivity" {
		request := cloneContinuationChildRequestV1(base.Request)
		for _, mutation := range testCase.Mutations {
			mutateContinuationChildRequestPathV1(t, &request, strings.TrimPrefix(mutation, "path:"))
		}
		before, _ := continuationChildRequestDigestV1(base.Request)
		after, _ := continuationChildRequestDigestV1(request)
		if before == after {
			return continuationChildRejectV1("CHILD_REQUEST_DIGEST_MISMATCH")
		}
		return continuationChildEvaluationV1{Outcome: "digest_changed", RequestDigest: after}
	}
	if testCase.Kind == "bridge" {
		if len(testCase.Mutations) != 1 {
			return continuationChildRejectV1("BRIDGE_EVIDENCE_MISMATCH")
		}
		return evaluateContinuationChildBridgeV1(t, base, testCase.Mutations[0])
	}
	fixture := cloneContinuationChildFixtureV1(base)
	authorityRequest := cloneContinuationChildRequestV1(base.Request)
	for _, mutation := range testCase.Mutations {
		applyContinuationChildMutationV1(t, &fixture, &authorityRequest, mutation)
	}
	return reduceContinuationChildV1(fixture, authorityRequest)
}

func evaluateContinuationChildStrictCaseV1(t *testing.T, base continuationChildFixtureV1, testCase continuationChildCaseV1) continuationChildEvaluationV1 {
	t.Helper()
	raw, err := canonicalJSON(base.Request)
	if err != nil {
		t.Fatal(err)
	}
	variant := ""
	if len(testCase.Mutations) == 1 {
		variant = testCase.Mutations[0]
	}
	switch variant {
	case "malformed":
		raw = []byte(`{"schema_version":`)
	case "unknown":
		raw = []byte(strings.Replace(string(raw), "{", `{"unknown":true,`, 1))
	case "duplicate":
		raw = []byte(strings.Replace(string(raw), `"effect_kind":"creation_spec_activation"`, `"effect_kind":"creation_spec_activation","effect_kind":"creation_spec_activation"`, 1))
	case "null":
		raw = []byte(strings.Replace(string(raw), `"scope":{`, `"scope":null,"ignored_scope":{`, 1))
	case "trailing":
		raw = append(raw, []byte(`{}`)...)
	default:
		return continuationChildRejectV1("CHILD_RECEIPT_SCHEMA_INVALID")
	}
	if err := strictContinuationChildRequestV1(raw); err == nil {
		return continuationChildEvaluationV1{Outcome: "validated"}
	}
	return continuationChildRejectV1("CHILD_RECEIPT_SCHEMA_INVALID")
}

func reduceContinuationChildV1(fixture continuationChildFixtureV1, authority continuationChildRequestV1) continuationChildEvaluationV1 {
	request := fixture.Request
	if code := validateContinuationChildRequestShapeV1(request, fixture.CurrentReceiptID); code != "" {
		return continuationChildRejectV1(code)
	}
	if request.Decision.Action != "approve" && request.Decision.Action != "reject" {
		return continuationChildRejectV1("CONTINUATION_ACTION_INVALID")
	}
	if fixture.Authorities.AuthenticatedIdentity != request.Scope || request.Decision.ActorUserID != request.Scope.UserID || request.Decision.ActorProjectID != request.Scope.ProjectID {
		return continuationChildRejectV1("CONTINUATION_IDENTITY_INVALID")
	}
	wantSource := "approval-decision:" + request.Approval.ApprovalID + ":" + request.Decision.DecisionID
	if request.Causal.ContinuationSourceID != wantSource || request.ChildKey.ContinuationTurnID != request.Causal.TurnID || request.ChildKey.SessionID != request.Scope.SessionID {
		return continuationChildRejectV1("CONTINUATION_CAUSAL_BINDING_INVALID")
	}
	mapping := fixture.Authorities.SourceMapping
	if mapping.ContinuationSourceID != request.Causal.ContinuationSourceID || mapping.InputID != request.Causal.InputID || mapping.TurnID != request.Causal.TurnID || mapping.RunID != request.Causal.RunID ||
		mapping.ApprovalID != request.Approval.ApprovalID || mapping.DecisionID != request.Decision.DecisionID || mapping.Action != request.Decision.Action {
		return continuationChildRejectV1("CONTINUATION_SOURCE_MAPPING_CONFLICT")
	}
	if request.ChildKey.SessionID != authority.ChildKey.SessionID || request.ChildKey.ContinuationTurnID != authority.ChildKey.ContinuationTurnID || request.ChildKey.OriginalToolCallID != authority.ChildKey.OriginalToolCallID {
		return continuationChildRejectV1("CHILD_RECEIPT_KEY_INVALID")
	}
	digest, err := continuationChildRequestDigestV1(request)
	if err != nil || digest != fixture.ClaimedRequestDigest {
		return continuationChildRejectV1("CHILD_REQUEST_DIGEST_MISMATCH")
	}
	var recovered *continuationChildSnapshotV1
	if fixture.Scenario.ExistingChild != "absent" {
		stored := buildContinuationChildStoredV1(fixture, authority)
		applyContinuationChildStoredMutationV1(&stored, fixture.Scenario.StoredMutation)
		if !validateContinuationChildStoredIntegrityV1(stored, authority) {
			return continuationChildRejectWithSnapshotV1("STORED_CHILD_RECEIPT_INVALID", stored)
		}
		if stored.Scope != fixture.Authorities.AuthenticatedIdentity {
			return continuationChildRejectWithSnapshotV1("CONTINUATION_IDENTITY_INVALID", stored)
		}
		if stored.RequestDigest != digest {
			return continuationChildRejectWithSnapshotV1("TOOL_RECEIPT_CONFLICT", stored)
		}
		if stored.WriteState == "frozen" {
			return continuationChildEvaluationV1{Outcome: "replayed", Snapshot: stored, RequestDigest: digest}
		}
		recovered = &stored
	}
	if recovered == nil {
		if !validContinuationChildRootAuthorityV1(fixture.Authorities.Root, request, authority) {
			return continuationChildRejectV1("ROOT_TOOL_RECEIPT_INVALID")
		}
		if !validContinuationChildParentAuthorityV1(fixture.Authorities.Parent, request, authority) {
			return continuationChildRejectV1("PARENT_TOOL_RECEIPT_INVALID")
		}
		if fixture.Authorities.Parent.ApprovalID != request.Approval.ApprovalID || fixture.Authorities.Parent.ApprovalVersion != request.Approval.PresentedApprovalVersion || fixture.Authorities.Parent.ApprovalDigest != request.Approval.BindingDigest ||
			request.Approval != authority.Approval || fixture.Authorities.ApprovalState != expectedContinuationChildApprovalStateV1(request.Decision.Action) {
			return continuationChildRejectV1("APPROVAL_BINDING_INVALID")
		}
		if request.Decision != authority.Decision || fixture.Authorities.DecisionState != expectedContinuationChildApprovalStateV1(request.Decision.Action) {
			return continuationChildRejectV1("APPROVAL_DECISION_INVALID")
		}
		if request.Scope != authority.Scope || request.Root != authority.Root || request.Parent != authority.Parent || request.Causal != authority.Causal || request.InheritedExecution != authority.InheritedExecution {
			return continuationChildRejectV1("CONTINUATION_INHERITANCE_CONFLICT")
		}
		if !fixture.Authorities.FirstWriteAvailable {
			return continuationChildRejectV1("CHILD_RECEIPT_CREATE_CONFLICT")
		}
	}
	if fixture.Scenario.PresentedFence != fixture.Scenario.CurrentFence {
		return continuationChildRejectV1("STALE_FENCE")
	}
	if continuationChildHasInvariantV1(fixture.Scenario, "state_phase") {
		return continuationChildRejectV1("CHILD_RECEIPT_STATE_CONFLICT")
	}
	if recovered != nil {
		snapshot := *recovered
		if fixture.Scenario.CurrentFence <= snapshot.OwnerFence || snapshot.ExecutionPhase != "recovery_pending" {
			return continuationChildRejectWithSnapshotV1("STALE_FENCE", snapshot)
		}
		snapshot.OwnerFence = fixture.Scenario.CurrentFence
		if code := validateContinuationChildLogicalSlotsV1(snapshot, request); code != "" {
			return continuationChildRejectWithSnapshotV1(code, snapshot)
		}
		return evaluateContinuationChildBusinessV1(snapshot, request, fixture.Scenario, 0, 0, true)
	}
	snapshot := continuationChildSnapshotV1{
		ReceiptID: fixture.CurrentReceiptID, ReceiptVersion: 1, Scope: request.Scope, ChildKey: request.ChildKey,
		RequestCanonical: request, RequestDigest: digest,
		RootReceiptID: request.Root.ToolReceiptID, ParentReceiptID: request.Parent.ToolReceiptID,
		WriteState: "open", ExecutionPhase: "in_progress", OwnerFence: fixture.Scenario.CurrentFence,
		Slots: []continuationChildLogicalSlotV1{continuationChildDecisionSlotV1(request)},
	}
	if probe, ok := continuationChildInvariantLedgerV1(request, fixture.CurrentReceiptID, fixture.Scenario); ok {
		snapshot.Slots = probe
		if code := validateContinuationChildLogicalSlotsV1(snapshot, request); code != "" {
			return continuationChildRejectWithSnapshotV1(code, snapshot)
		}
	}
	if code := validateContinuationChildLogicalSlotsV1(snapshot, request); code != "" {
		return continuationChildRejectWithSnapshotV1(code, snapshot)
	}
	if fixture.Scenario.PreGuard != "eligible" {
		return freezeContinuationChildV1(snapshot, "APPROVAL_PRE_CONSUMPTION_NOT_ELIGIBLE", "failed", 0, 0)
	}
	if request.Decision.Action == "approve" {
		switch fixture.Scenario.Consumption {
		case "recorded":
			snapshot.Slots = append(snapshot.Slots, continuationChildConsumptionSlotV1(request))
		case "determined_conflict", "determined_not_eligible", "stored_invalid":
			return freezeContinuationChildV1(snapshot, "APPROVAL_CONSUMPTION_REJECTED", "failed", 0, 0)
		case "transport_unknown", "query_not_found":
			snapshot.ExecutionPhase = "recovery_pending"
			queries := 0
			if fixture.Scenario.Consumption == "query_not_found" {
				queries = 1
			}
			return continuationChildEvaluationV1{Outcome: "recovery_pending", Code: "UNKNOWN_OUTCOME", Snapshot: snapshot, BusinessQueries: queries, RequestDigest: digest}
		default:
			return continuationChildRejectV1("APPROVAL_CONSUMPTION_REJECTED")
		}
		if code := validateContinuationChildLogicalSlotsV1(snapshot, request); code != "" {
			return continuationChildRejectWithSnapshotV1(code, snapshot)
		}
		if fixture.Scenario.PostConsumptionGuard != "eligible" {
			return freezeContinuationChildV1(snapshot, "APPROVAL_POST_CONSUMPTION_NOT_ELIGIBLE", "failed", 0, 0)
		}
	}
	if continuationChildHasInvariantV1(fixture.Scenario, "call_without_prepared") {
		return continuationChildRejectWithSnapshotV1(validateContinuationChildBusinessCallV1(snapshot), snapshot)
	}
	business := continuationChildPreparedBusinessSlotV1(request, fixture.CurrentReceiptID)
	snapshot.Slots = append(snapshot.Slots, business)
	applyContinuationChildPreparedInvariantV1(&snapshot, fixture.Scenario)
	if code := validateContinuationChildLogicalSlotsV1(snapshot, request); code != "" {
		return continuationChildRejectWithSnapshotV1(code, snapshot)
	}
	return evaluateContinuationChildBusinessV1(snapshot, request, fixture.Scenario, 1, 0, false)
}

// evaluateContinuationChildBusinessV1 只允许对已 prepared 的原槽写入一次或按原 key 查询，并保持 unknown 隔离。
func evaluateContinuationChildBusinessV1(snapshot continuationChildSnapshotV1, request continuationChildRequestV1, scenario continuationChildScenarioV1, writes, queries int, recovering bool) continuationChildEvaluationV1 {
	index := continuationChildBusinessSlotIndexV1(snapshot.Slots)
	if index < 0 || snapshot.Slots[index].ResolutionState != "prepared" {
		return continuationChildRejectWithSnapshotV1("BUSINESS_DECIDE_NOT_PREPARED", snapshot)
	}
	if recovering && writes != 0 {
		return continuationChildRejectWithSnapshotV1("TOOL_EXECUTION_SLOT_CONFLICT", snapshot)
	}
	if continuationChildHasInvariantV1(scenario, "response_digest_mismatch") {
		snapshot.ExecutionPhase = "recovery_pending"
		return continuationChildEvaluationV1{Outcome: "recovery_pending", Code: "UNKNOWN_OUTCOME", Snapshot: snapshot, BusinessWrites: writes, BusinessQueries: queries, RequestDigest: snapshot.RequestDigest}
	}
	if continuationChildHasInvariantV1(scenario, "query_conflict") {
		snapshot.ExecutionPhase = "recovery_pending"
		return continuationChildEvaluationV1{Outcome: "recovery_pending", Code: "BUSINESS_DECIDE_RECEIPT_CONFLICT", Snapshot: snapshot, BusinessWrites: writes, BusinessQueries: queries + 1, RequestDigest: snapshot.RequestDigest}
	}
	if continuationChildHasInvariantV1(scenario, "positive_authority_conflict") {
		return continuationChildRejectWithSnapshotCountsV1("BUSINESS_DECIDE_RECEIPT_CONFLICT", snapshot, writes, queries)
	}
	prepared := snapshot.Slots[index]
	switch scenario.BusinessObservation {
	case "write_committed":
		if recovering {
			return continuationChildRejectWithSnapshotV1("TOOL_EXECUTION_SLOT_CONFLICT", snapshot)
		}
		snapshot.Slots[index] = continuationChildResolvedBusinessSlotV1(prepared, request, "committed")
	case "write_lost_query_completed":
		queries++
		snapshot.Slots[index] = continuationChildResolvedBusinessSlotV1(prepared, request, "committed")
	case "higher_fence_query_completed":
		if !recovering {
			return continuationChildRejectWithSnapshotV1("TOOL_EXECUTION_SLOT_CONFLICT", snapshot)
		}
		queries++
		snapshot.Slots[index] = continuationChildResolvedBusinessSlotV1(prepared, request, "committed")
	case "not_found_not_found_completed":
		queries += 3
		snapshot.Slots[index] = continuationChildResolvedBusinessSlotV1(prepared, request, "committed")
	case "query_timeout_completed":
		queries += 2
		snapshot.Slots[index] = continuationChildResolvedBusinessSlotV1(prepared, request, "committed")
	case "resolve_lost_authority_replay":
		queries++
		first := continuationChildResolvedBusinessSlotV1(prepared, request, "committed")
		replayed := continuationChildResolvedBusinessSlotV1(prepared, request, "committed")
		if first != replayed {
			return continuationChildRejectWithSnapshotV1("TOOL_EXECUTION_SLOT_CONFLICT", snapshot)
		}
		snapshot.Slots[index] = first
	case "not_committed", "not_committed_with_transport_unknown":
		snapshot.Slots[index] = continuationChildResolvedBusinessSlotV1(prepared, request, "not_committed")
		applyContinuationChildResolvedInvariantV1(&snapshot, scenario)
		if code := validateContinuationChildLogicalSlotsV1(snapshot, request); code != "" {
			return continuationChildRejectWithSnapshotCountsV1(code, snapshot, writes, queries)
		}
		code := "APPROVAL_APPROVE_BUSINESS_NOT_COMMITTED"
		if request.Decision.Action == "reject" {
			code = "APPROVAL_REJECT_BUSINESS_NOT_COMMITTED"
		}
		result := freezeContinuationChildV1(snapshot, code, "failed", writes, queries)
		return applyContinuationChildResultInvariantV1(result, request, scenario)
	case "query_not_found", "query_timeout":
		queries++
		snapshot.ExecutionPhase = "recovery_pending"
		return continuationChildEvaluationV1{Outcome: "recovery_pending", Code: "UNKNOWN_OUTCOME", Snapshot: snapshot, BusinessWrites: writes, BusinessQueries: queries, RequestDigest: snapshot.RequestDigest}
	default:
		return continuationChildRejectWithSnapshotV1("BUSINESS_DECIDE_RECEIPT_CONFLICT", snapshot)
	}
	applyContinuationChildResolvedInvariantV1(&snapshot, scenario)
	if code := validateContinuationChildLogicalSlotsV1(snapshot, request); code != "" {
		return continuationChildRejectWithSnapshotCountsV1(code, snapshot, writes, queries)
	}
	result := freezeContinuationChildV1(snapshot, "", "completed", writes, queries)
	return applyContinuationChildResultInvariantV1(result, request, scenario)
}

// validateContinuationChildRequestShapeV1 在任何 authority 比较前固定 UUID、safe integer、摘要和固定 pin 形状。
func validateContinuationChildRequestShapeV1(request continuationChildRequestV1, currentReceiptID string) string {
	if request.SchemaVersion != "continuation_child_tool_receipt_request.v1" || request.EffectKind != "creation_spec_activation" {
		return "CHILD_RECEIPT_SCHEMA_INVALID"
	}
	ids := []string{
		currentReceiptID, request.Scope.UserID, request.Scope.ProjectID, request.Scope.SessionID,
		request.ChildKey.SessionID, request.ChildKey.ContinuationTurnID, request.ChildKey.OriginalToolCallID,
		request.Causal.InputID, request.Causal.TurnID, request.Causal.RunID,
		request.Root.ToolReceiptID, request.Root.TurnID, request.Root.RunID,
		request.Parent.ToolReceiptID, request.Parent.TurnID, request.Parent.RunID,
		request.Approval.ApprovalID, request.Decision.DecisionReceiptID, request.Decision.DecisionID,
		request.Decision.CardID, request.Decision.ActorUserID, request.Decision.ActorProjectID,
		request.InheritedExecution.ResourceID,
	}
	for _, id := range ids {
		if !canonicalUUIDv7(id) {
			return "CHILD_RECEIPT_SCHEMA_INVALID"
		}
	}
	integers := []int64{
		request.Root.ToolReceiptVersion, request.Parent.ToolReceiptVersion,
		request.Approval.PresentedApprovalVersion, request.Approval.ResultingApprovalVersion,
		request.Decision.CardRevision, request.InheritedExecution.ResourceVersion,
	}
	for _, value := range integers {
		if !safePositiveIntegerV1(value) {
			return "CHILD_RECEIPT_SCHEMA_INVALID"
		}
	}
	if request.Approval.ResultingApprovalVersion != request.Approval.PresentedApprovalVersion+1 {
		return "APPROVAL_BINDING_INVALID"
	}
	digests := []string{
		request.Root.RequestSemanticDigest, request.Parent.RequestSemanticDigest, request.Parent.ResultDigest,
		request.Approval.BindingDigest, request.Decision.DecisionDigest, request.InheritedExecution.IntentDigest,
		request.InheritedExecution.ToolPinDigest, request.InheritedExecution.ExecutionDigest,
		request.InheritedExecution.ResourceDigest, request.InheritedExecution.TargetExactSetDigest,
	}
	for _, digest := range digests {
		if !digestPattern.MatchString(digest) {
			return "CHILD_RECEIPT_SCHEMA_INVALID"
		}
	}
	if !bareSHA256HexV1(request.InheritedExecution.ParentTurnContextDigest) || request.Parent.ResultStatus != "waiting_user" ||
		request.Approval.ApprovalType != "candidate_activation" || request.InheritedExecution.ToolPinOwner != "agent.tool_registry" ||
		request.InheritedExecution.ToolKey != "plan_creation_spec" || request.InheritedExecution.DefinitionVersion != "plan_creation_spec.v1alpha1" ||
		request.InheritedExecution.IntentSchemaVersion != "plan_creation_spec_intent.v1" || request.InheritedExecution.ResultSchemaVersion != "graph_tool_result.v1" ||
		request.InheritedExecution.GraphKey != "plan_creation_spec_graph_v1" || request.InheritedExecution.ToolPinRef == "" {
		return "CHILD_RECEIPT_SCHEMA_INVALID"
	}
	return ""
}

func bareSHA256HexV1(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

// continuationChildInvariantLedgerV1 把负向 mutation 转换为真实 logical ledger，再交给统一 validator。
func continuationChildInvariantLedgerV1(request continuationChildRequestV1, receiptID string, scenario continuationChildScenarioV1) ([]continuationChildLogicalSlotV1, bool) {
	d := continuationChildDecisionSlotV1(request)
	c := continuationChildConsumptionSlotV1(request)
	b := continuationChildPreparedBusinessSlotV1(request, receiptID)
	switch {
	case continuationChildHasInvariantV1(scenario, "prepared_drift"):
		snapshot := continuationChildSnapshotV1{ReceiptID: receiptID, Slots: []continuationChildLogicalSlotV1{d, c, b}}
		applyContinuationChildPreparedInvariantV1(&snapshot, scenario)
		return snapshot.Slots, true
	case continuationChildHasInvariantV1(scenario, "unpinned_slot"):
		extra := b
		extra.Role = "quote"
		return []continuationChildLogicalSlotV1{d, extra}, true
	case continuationChildHasInvariantV1(scenario, "approve_missing_consumption", "business_before_consumption"):
		return []continuationChildLogicalSlotV1{d, b}, true
	case continuationChildHasInvariantV1(scenario, "reject_has_consumption"):
		return []continuationChildLogicalSlotV1{d, c}, true
	case continuationChildHasInvariantV1(scenario, "consumption_before_decision"):
		return []continuationChildLogicalSlotV1{c}, true
	case continuationChildHasInvariantV1(scenario, "business_before_decision"):
		return []continuationChildLogicalSlotV1{b}, true
	default:
		return nil, false
	}
}

func applyContinuationChildPreparedInvariantV1(snapshot *continuationChildSnapshotV1, scenario continuationChildScenarioV1) {
	index := continuationChildBusinessSlotIndexV1(snapshot.Slots)
	if index < 0 {
		return
	}
	slot := &snapshot.Slots[index]
	switch {
	case continuationChildHasInvariantV1(scenario, "prepared_drift"):
		slot.QueryContract = "business.creation_spec_candidate_decision.query.v2"
	case continuationChildHasInvariantV1(scenario, "second_business_key"):
		slot.IdempotencyKey += ":second"
	case continuationChildHasInvariantV1(scenario, "authority_schema_switch"):
		slot.RefSchemaVersion = "business_creation_spec_candidate_decision_authority.v2"
	}
}

func applyContinuationChildResolvedInvariantV1(snapshot *continuationChildSnapshotV1, scenario continuationChildScenarioV1) {
	index := continuationChildBusinessSlotIndexV1(snapshot.Slots)
	if index < 0 {
		return
	}
	slot := &snapshot.Slots[index]
	switch {
	case continuationChildHasInvariantV1(scenario, "authority_ref_conflict"):
		conflicting := *slot
		conflicting.AuthorityID = "019f4600-0000-7000-8000-000000009971"
		conflicting.AuthorityDigest = "sha256:9797979797979797979797979797979797979797979797979797979797979797"
		snapshot.Slots = append(snapshot.Slots, conflicting)
	case continuationChildHasInvariantV1(scenario, "business_outcome_missing"):
		slot.AuthorityOutcome = ""
	case continuationChildHasInvariantV1(scenario, "business_outcome_invalid"):
		slot.AuthorityOutcome = "unknown"
	case continuationChildHasInvariantV1(scenario, "non_business_outcome"):
		for index := range snapshot.Slots {
			if snapshot.Slots[index].Role == "approval_consumption" {
				snapshot.Slots[index].AuthorityOutcome = "committed"
			}
		}
	}
}

func applyContinuationChildResultInvariantV1(result continuationChildEvaluationV1, request continuationChildRequestV1, scenario continuationChildScenarioV1) continuationChildEvaluationV1 {
	if result.Outcome != "frozen" {
		return result
	}
	mutated := false
	if continuationChildHasInvariantV1(scenario, "ordinary_failure_hides_consumption") {
		result.Snapshot.ResultStatus = "failed"
		result.Snapshot.ResultCode = "INVALID_ARGUMENT"
		result.Snapshot.ResultRefRoles = nil
		mutated = true
	}
	if continuationChildHasInvariantV1(scenario, "missing_consumption_result_ref") {
		result.Snapshot.ResultRefRoles = removeContinuationChildRoleV1(result.Snapshot.ResultRefRoles, "approval_consumption")
		mutated = true
	}
	if continuationChildHasInvariantV1(scenario, "missing_business_negative_ref") {
		result.Snapshot.ResultRefRoles = removeContinuationChildRoleV1(result.Snapshot.ResultRefRoles, "business_decide")
		mutated = true
	}
	if !mutated {
		return result
	}
	result.Snapshot.ResultDigest = continuationChildResultDigestV1(result.Snapshot)
	if code := validateContinuationChildFrozenV1(result.Snapshot, request); code != "" {
		return continuationChildRejectWithSnapshotCountsV1(code, result.Snapshot, result.BusinessWrites, result.BusinessQueries)
	}
	return result
}

func removeContinuationChildRoleV1(values []string, target string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != target {
			result = append(result, value)
		}
	}
	return result
}

func validateContinuationChildBusinessCallV1(snapshot continuationChildSnapshotV1) string {
	index := continuationChildBusinessSlotIndexV1(snapshot.Slots)
	if index < 0 || snapshot.Slots[index].ResolutionState != "prepared" {
		return "BUSINESS_DECIDE_NOT_PREPARED"
	}
	return ""
}

func continuationChildBusinessSlotIndexV1(slots []continuationChildLogicalSlotV1) int {
	for index := range slots {
		if slots[index].Role == "business_decide" {
			return index
		}
	}
	return -1
}

func freezeContinuationChildV1(snapshot continuationChildSnapshotV1, code, status string, writes, queries int) continuationChildEvaluationV1 {
	snapshot.WriteState = "frozen"
	snapshot.ExecutionPhase = ""
	snapshot.ResultStatus = status
	snapshot.ResultCode = code
	snapshot.ResultRefRoles = continuationChildProjectedResultRolesV1(snapshot.Slots)
	snapshot.ReceiptVersion++
	snapshot.ResultDigest = continuationChildResultDigestV1(snapshot)
	if validationCode := validateContinuationChildFrozenV1(snapshot, snapshot.RequestCanonical); validationCode != "" {
		return continuationChildRejectWithSnapshotV1(validationCode, snapshot)
	}
	return continuationChildEvaluationV1{Outcome: "frozen", Code: code, Snapshot: snapshot, BusinessWrites: writes, BusinessQueries: queries, FreezeCount: 1, RequestDigest: snapshot.RequestDigest}
}

func continuationChildRejectV1(code string) continuationChildEvaluationV1 {
	return continuationChildEvaluationV1{Outcome: "rejected", Code: code, Snapshot: continuationChildSnapshotV1{Slots: []continuationChildLogicalSlotV1{}}}
}

func continuationChildRejectWithSnapshotV1(code string, snapshot continuationChildSnapshotV1) continuationChildEvaluationV1 {
	return continuationChildEvaluationV1{Outcome: "rejected", Code: code, Snapshot: snapshot, RequestDigest: snapshot.RequestDigest}
}

func continuationChildRejectWithSnapshotCountsV1(code string, snapshot continuationChildSnapshotV1, writes, queries int) continuationChildEvaluationV1 {
	return continuationChildEvaluationV1{
		Outcome: "rejected", Code: code, Snapshot: snapshot, RequestDigest: snapshot.RequestDigest,
		BusinessWrites: writes, BusinessQueries: queries,
	}
}

func continuationChildDecisionSlotV1(request continuationChildRequestV1) continuationChildLogicalSlotV1 {
	return continuationChildLogicalSlotV1{
		Role: "approval_decision", EffectClass: "evidence_only", RefType: "approval_decision_receipt",
		RefSchemaVersion: "approval_decision_receipt.v1", AuthorityOwner: "agent", QueryContract: "agent.approval_decision.query.v1",
		ResolutionState: "resolved", AuthorityID: request.Decision.DecisionReceiptID, AuthorityDigest: request.Decision.DecisionDigest,
	}
}

func continuationChildConsumptionSlotV1(request continuationChildRequestV1) continuationChildLogicalSlotV1 {
	return continuationChildLogicalSlotV1{
		Role: "approval_consumption", EffectClass: "side_effect", RefType: "approval_consumption_receipt",
		RefSchemaVersion: "approval_consumption_receipt.v1", AuthorityOwner: "agent", QueryContract: "agent.approval_consumption.query.v1",
		ResolutionState: "resolved", AuthorityID: "019f4600-0000-7000-8000-000000000501",
		AuthorityDigest: "sha256:5151515151515151515151515151515151515151515151515151515151515151",
	}
}

func continuationChildPreparedBusinessSlotV1(request continuationChildRequestV1, receiptID string) continuationChildLogicalSlotV1 {
	digest := continuationChildBusinessRequestDigestV1(request)
	return continuationChildLogicalSlotV1{
		Role: "business_decide", EffectClass: "side_effect", RefType: "business_decision_authority",
		RefSchemaVersion: "business_creation_spec_candidate_decision_authority.v1", AuthorityOwner: "business",
		IdempotencyKey: "tr:" + receiptID + ":business_decide:v1", RequestDigest: digest,
		QueryContract: "business.creation_spec_candidate_decision.query.v1", ResolutionState: "prepared",
	}
}

func continuationChildResolvedBusinessSlotV1(slot continuationChildLogicalSlotV1, request continuationChildRequestV1, outcome string) continuationChildLogicalSlotV1 {
	slot.ResolutionState = "resolved"
	slot.AuthorityID = "019f4600-0000-7000-8000-000000000601"
	slot.AuthorityDigest = "sha256:6161616161616161616161616161616161616161616161616161616161616161"
	slot.AuthorityOutcome = outcome
	return slot
}

func continuationChildBusinessRequestDigestV1(request continuationChildRequestV1) string {
	consumptionDigest := ""
	if request.Decision.Action == "approve" {
		consumptionDigest = "sha256:5151515151515151515151515151515151515151515151515151515151515151"
	}
	projection := continuationChildBusinessRequestV1{
		Action: request.Decision.Action, ApprovalID: request.Approval.ApprovalID,
		DecisionID: request.Decision.DecisionID, DecisionDigest: request.Decision.DecisionDigest,
		ConsumptionAuthorityDigest: consumptionDigest, UserID: request.Scope.UserID, ProjectID: request.Scope.ProjectID,
		ResourceID: request.InheritedExecution.ResourceID, ResourceVersion: request.InheritedExecution.ResourceVersion,
		ResourceDigest: request.InheritedExecution.ResourceDigest, TargetExactSetDigest: request.InheritedExecution.TargetExactSetDigest,
	}
	raw, _ := canonicalJSON(projection)
	return semanticDigest(continuationChildBusinessDomainV1, raw)
}

func continuationChildResolvedSlotRolesV1(slots []continuationChildLogicalSlotV1) []string {
	roles := make([]string, 0, len(slots))
	for _, slot := range slots {
		if slot.ResolutionState == "resolved" {
			roles = append(roles, slot.Role)
		}
	}
	return roles
}

func continuationChildProjectedResultRolesV1(slots []continuationChildLogicalSlotV1) []string {
	roles := make([]string, 0, len(slots))
	for _, slot := range slots {
		if slot.EffectClass == "side_effect" && slot.ResolutionState == "resolved" {
			roles = append(roles, slot.Role)
		}
	}
	return roles
}

func continuationChildSlotRolesV1(snapshot continuationChildSnapshotV1) []string {
	roles := make([]string, 0, len(snapshot.Slots))
	for _, slot := range snapshot.Slots {
		roles = append(roles, slot.Role)
	}
	return roles
}

// validateContinuationChildLogicalSlotsV1 同时校验每个槽的 pinned 形状和 action/phase 前缀 exact-set。
func validateContinuationChildLogicalSlotsV1(snapshot continuationChildSnapshotV1, request continuationChildRequestV1) string {
	seen := map[string]struct{}{}
	for _, slot := range snapshot.Slots {
		if _, exists := seen[slot.Role]; exists {
			return "TOOL_EXECUTION_SLOT_CONFLICT"
		}
		seen[slot.Role] = struct{}{}
		switch slot.Role {
		case "approval_decision":
			if slot.EffectClass != "evidence_only" || slot.RefType != "approval_decision_receipt" || slot.RefSchemaVersion != "approval_decision_receipt.v1" ||
				slot.AuthorityOwner != "agent" || slot.QueryContract != "agent.approval_decision.query.v1" || slot.ResolutionState != "resolved" ||
				slot.AuthorityID != request.Decision.DecisionReceiptID || slot.AuthorityDigest != request.Decision.DecisionDigest || slot.AuthorityOutcome != "" ||
				slot.IdempotencyKey != "" || slot.RequestDigest != "" {
				return "TOOL_EXECUTION_SLOT_CONFLICT"
			}
		case "approval_consumption":
			if slot.EffectClass != "side_effect" || slot.RefType != "approval_consumption_receipt" || slot.RefSchemaVersion != "approval_consumption_receipt.v1" ||
				slot.AuthorityOwner != "agent" || slot.QueryContract != "agent.approval_consumption.query.v1" || slot.ResolutionState != "resolved" ||
				!canonicalUUIDv7(slot.AuthorityID) || !digestPattern.MatchString(slot.AuthorityDigest) ||
				slot.IdempotencyKey != "" || slot.RequestDigest != "" {
				return "TOOL_EXECUTION_SLOT_CONFLICT"
			}
			if slot.AuthorityOutcome != "" {
				return "INVALID_TOOL_RECEIPT"
			}
		case "business_decide":
			if slot.EffectClass != "side_effect" || slot.RefType != "business_decision_authority" ||
				slot.RefSchemaVersion != "business_creation_spec_candidate_decision_authority.v1" || slot.AuthorityOwner != "business" ||
				slot.IdempotencyKey != "tr:"+snapshot.ReceiptID+":business_decide:v1" || slot.RequestDigest != continuationChildBusinessRequestDigestV1(request) ||
				slot.QueryContract != "business.creation_spec_candidate_decision.query.v1" {
				return "TOOL_EXECUTION_SLOT_CONFLICT"
			}
			switch slot.ResolutionState {
			case "prepared":
				if slot.AuthorityID != "" || slot.AuthorityDigest != "" || slot.AuthorityOutcome != "" {
					return "TOOL_EXECUTION_SLOT_CONFLICT"
				}
			case "resolved":
				if !canonicalUUIDv7(slot.AuthorityID) || !digestPattern.MatchString(slot.AuthorityDigest) ||
					(slot.AuthorityOutcome != "committed" && slot.AuthorityOutcome != "not_committed") {
					return "INVALID_TOOL_RECEIPT"
				}
			default:
				return "TOOL_EXECUTION_SLOT_CONFLICT"
			}
		default:
			return "TOOL_EXECUTION_SLOT_CONFLICT"
		}
	}
	roles := continuationChildSlotRolesV1(snapshot)
	allowed := [][]string{{"approval_decision"}}
	if request.Decision.Action == "approve" {
		allowed = append(allowed,
			[]string{"approval_decision", "approval_consumption"},
			[]string{"approval_decision", "approval_consumption", "business_decide"},
		)
	} else {
		allowed = append(allowed, []string{"approval_decision", "business_decide"})
	}
	for _, candidate := range allowed {
		if reflect.DeepEqual(roles, candidate) {
			return ""
		}
	}
	return "CONTINUATION_SLOT_SET_INVALID"
}

func validateContinuationChildFrozenV1(snapshot continuationChildSnapshotV1, request continuationChildRequestV1) string {
	if snapshot.WriteState != "frozen" || snapshot.ExecutionPhase != "" || snapshot.ResultStatus == "" || snapshot.ResultDigest == "" {
		return "CHILD_RECEIPT_STATE_CONFLICT"
	}
	if code := validateContinuationChildLogicalSlotsV1(snapshot, request); code != "" {
		return code
	}
	roles := continuationChildSlotRolesV1(snapshot)
	wantRoles := []string{}
	switch snapshot.ResultCode {
	case "APPROVAL_PRE_CONSUMPTION_NOT_ELIGIBLE", "APPROVAL_CONSUMPTION_REJECTED":
		wantRoles = []string{"approval_decision"}
	case "APPROVAL_POST_CONSUMPTION_NOT_ELIGIBLE":
		wantRoles = []string{"approval_decision", "approval_consumption"}
	case "APPROVAL_APPROVE_BUSINESS_NOT_COMMITTED":
		wantRoles = []string{"approval_decision", "approval_consumption", "business_decide"}
	case "APPROVAL_REJECT_BUSINESS_NOT_COMMITTED":
		wantRoles = []string{"approval_decision", "business_decide"}
	case "":
		if snapshot.ResultStatus != "completed" {
			return "RESULT_REF_MISMATCH"
		}
		wantRoles = []string{"approval_decision", "approval_consumption", "business_decide"}
		if request.Decision.Action == "reject" {
			wantRoles = []string{"approval_decision", "business_decide"}
		}
	default:
		return "RESULT_REF_MISMATCH"
	}
	if !reflect.DeepEqual(roles, wantRoles) || !reflect.DeepEqual(snapshot.ResultRefRoles, continuationChildProjectedResultRolesV1(snapshot.Slots)) {
		return "RESULT_REF_MISMATCH"
	}
	index := continuationChildBusinessSlotIndexV1(snapshot.Slots)
	if index >= 0 {
		outcome := snapshot.Slots[index].AuthorityOutcome
		if strings.Contains(snapshot.ResultCode, "NOT_COMMITTED") && outcome != "not_committed" {
			return "RESULT_REF_MISMATCH"
		}
		if snapshot.ResultStatus == "completed" && outcome != "committed" {
			return "RESULT_REF_MISMATCH"
		}
	}
	if snapshot.ResultDigest != continuationChildResultDigestV1(snapshot) {
		return "STORED_CHILD_RECEIPT_INVALID"
	}
	return ""
}

func continuationChildResultDigestV1(snapshot continuationChildSnapshotV1) string {
	projection := continuationChildResultProjectionV1{
		ReceiptID: snapshot.ReceiptID, RequestDigest: snapshot.RequestDigest, Status: snapshot.ResultStatus,
		Code: snapshot.ResultCode, ResultRefRoles: snapshot.ResultRefRoles,
	}
	raw, _ := canonicalJSON(projection)
	return semanticDigest(continuationChildResultDomainV1, raw)
}

func continuationChildHasInvariantV1(scenario continuationChildScenarioV1, values ...string) bool {
	actual := strings.Split(scenario.InvariantViolation, ",")
	for _, candidate := range values {
		for _, item := range actual {
			if item == candidate {
				return true
			}
		}
	}
	return false
}

func validContinuationChildRootAuthorityV1(root continuationChildReceiptAuthorityV1, request, authority continuationChildRequestV1) bool {
	return root.ReceiptID == request.Root.ToolReceiptID && root.ReceiptVersion == request.Root.ToolReceiptVersion &&
		root.TurnID == request.Root.TurnID && root.RunID == request.Root.RunID && root.RequestDigest == request.Root.RequestSemanticDigest &&
		root.TurnKind == "origin" && root.WriteState == "frozen" && root.RootToolReceiptID == root.ReceiptID &&
		root.RootReceiptVersion == root.ReceiptVersion && root.OriginalToolCallID == request.ChildKey.OriginalToolCallID &&
		root.ResultStatus == "waiting_user" && digestPattern.MatchString(root.ResultDigest) && request.Root == authority.Root
}

func validContinuationChildParentAuthorityV1(parent continuationChildReceiptAuthorityV1, request, authority continuationChildRequestV1) bool {
	wantKind := "origin"
	if request.Parent.ToolReceiptID != request.Root.ToolReceiptID {
		wantKind = "continuation"
	}
	return parent.ReceiptID == request.Parent.ToolReceiptID && parent.ReceiptVersion == request.Parent.ToolReceiptVersion &&
		parent.TurnID == request.Parent.TurnID && parent.RunID == request.Parent.RunID && parent.RequestDigest == request.Parent.RequestSemanticDigest &&
		parent.TurnKind == wantKind && parent.WriteState == "frozen" && parent.ResultStatus == "waiting_user" &&
		parent.ResultDigest == request.Parent.ResultDigest && parent.RootToolReceiptID == request.Root.ToolReceiptID &&
		parent.RootReceiptVersion == request.Root.ToolReceiptVersion && parent.OriginalToolCallID == request.ChildKey.OriginalToolCallID && request.Parent == authority.Parent
}

func expectedContinuationChildApprovalStateV1(action string) string {
	if action == "reject" {
		return "rejected"
	}
	return "approved"
}

func buildContinuationChildStoredV1(fixture continuationChildFixtureV1, authority continuationChildRequestV1) continuationChildSnapshotV1 {
	digest, _ := continuationChildRequestDigestV1(authority)
	snapshot := continuationChildSnapshotV1{
		ReceiptID: fixture.CurrentReceiptID, ReceiptVersion: 4, Scope: authority.Scope, ChildKey: authority.ChildKey,
		RequestCanonical: authority, RequestDigest: digest,
		RootReceiptID: authority.Root.ToolReceiptID, ParentReceiptID: authority.Parent.ToolReceiptID,
		WriteState: "frozen", OwnerFence: fixture.Scenario.CurrentFence,
		Slots: []continuationChildLogicalSlotV1{continuationChildDecisionSlotV1(authority)}, ResultStatus: "completed",
	}
	if authority.Decision.Action == "approve" {
		snapshot.Slots = append(snapshot.Slots, continuationChildConsumptionSlotV1(authority))
	}
	business := continuationChildPreparedBusinessSlotV1(authority, fixture.CurrentReceiptID)
	snapshot.Slots = append(snapshot.Slots, continuationChildResolvedBusinessSlotV1(business, authority, "committed"))
	snapshot.ResultRefRoles = continuationChildProjectedResultRolesV1(snapshot.Slots)
	snapshot.ResultDigest = continuationChildResultDigestV1(snapshot)
	if fixture.Scenario.ExistingChild == "open_prepared" {
		snapshot.WriteState = "open"
		snapshot.ExecutionPhase = "recovery_pending"
		snapshot.ResultStatus = ""
		snapshot.ResultCode = ""
		snapshot.ResultRefRoles = nil
		snapshot.ResultDigest = ""
		snapshot.Slots[len(snapshot.Slots)-1] = business
		snapshot.OwnerFence = fixture.Scenario.CurrentFence - 1
	}
	return snapshot
}

func applyContinuationChildStoredMutationV1(snapshot *continuationChildSnapshotV1, mutation string) {
	if strings.Contains(mutation, ",") {
		for _, item := range strings.Split(mutation, ",") {
			applyContinuationChildStoredMutationV1(snapshot, item)
		}
		return
	}
	switch mutation {
	case "key":
		snapshot.ChildKey.OriginalToolCallID = "019f4600-0000-7000-8000-000000009901"
	case "request":
		snapshot.RequestDigest = "sha256:9191919191919191919191919191919191919191919191919191919191919191"
	case "root":
		snapshot.RootReceiptID = "019f4600-0000-7000-8000-000000009902"
	case "parent":
		snapshot.ParentReceiptID = "019f4600-0000-7000-8000-000000009903"
	case "slot":
		snapshot.Slots[0].AuthorityDigest = "sha256:9292929292929292929292929292929292929292929292929292929292929292"
	case "result":
		snapshot.ResultDigest = "sha256:9494949494949494949494949494949494949494949494949494949494949494"
	case "user_scope":
		snapshot.Scope.UserID = "019f4600-0000-7000-8000-000000009904"
	case "project_scope":
		snapshot.Scope.ProjectID = "019f4600-0000-7000-8000-000000009905"
	case "session_scope":
		snapshot.Scope.SessionID = "019f4600-0000-7000-8000-000000009906"
	}
}

func validateContinuationChildStoredIntegrityV1(snapshot continuationChildSnapshotV1, authority continuationChildRequestV1) bool {
	digest, _ := continuationChildRequestDigestV1(authority)
	if !canonicalUUIDv7(snapshot.ReceiptID) || !safePositiveIntegerV1(snapshot.ReceiptVersion) || !safePositiveIntegerV1(snapshot.OwnerFence) ||
		snapshot.ChildKey != authority.ChildKey || snapshot.RequestCanonical != authority || snapshot.RequestDigest != digest ||
		snapshot.RootReceiptID != authority.Root.ToolReceiptID || snapshot.ParentReceiptID != authority.Parent.ToolReceiptID {
		return false
	}
	if code := validateContinuationChildLogicalSlotsV1(snapshot, authority); code != "" {
		return false
	}
	if snapshot.WriteState == "frozen" {
		return validateContinuationChildFrozenV1(snapshot, authority) == ""
	}
	return snapshot.WriteState == "open" && snapshot.ExecutionPhase == "recovery_pending" && snapshot.ResultStatus == "" &&
		snapshot.ResultCode == "" && snapshot.ResultDigest == "" && snapshot.ResultRefRoles == nil &&
		continuationChildBusinessSlotIndexV1(snapshot.Slots) >= 0 && snapshot.Slots[continuationChildBusinessSlotIndexV1(snapshot.Slots)].ResolutionState == "prepared"
}

func applyContinuationChildMutationV1(t *testing.T, fixture *continuationChildFixtureV1, authority *continuationChildRequestV1, mutation string) {
	t.Helper()
	switch {
	case mutation == "valid:nested_parent":
		for _, path := range []string{"parent.tool_receipt_id", "parent.tool_receipt_version", "parent.turn_id", "parent.run_id", "parent.request_semantic_digest", "parent.result_digest"} {
			mutateContinuationChildRequestPathV1(t, &fixture.Request, path)
			mutateContinuationChildRequestPathV1(t, authority, path)
		}
		fixture.Authorities.Parent.TurnKind = "continuation"
		fixture.Authorities.Parent.ReceiptID = fixture.Request.Parent.ToolReceiptID
		fixture.Authorities.Parent.ReceiptVersion = fixture.Request.Parent.ToolReceiptVersion
		fixture.Authorities.Parent.TurnID = fixture.Request.Parent.TurnID
		fixture.Authorities.Parent.RunID = fixture.Request.Parent.RunID
		fixture.Authorities.Parent.RequestDigest = fixture.Request.Parent.RequestSemanticDigest
		fixture.Authorities.Parent.ResultDigest = fixture.Request.Parent.ResultDigest
		syncContinuationChildFixtureDigestV1(t, fixture)
	case mutation == "valid:reject":
		fixture.Request.Decision.Action = "reject"
		authority.Decision.Action = "reject"
		fixture.Authorities.SourceMapping.Action = "reject"
		fixture.Authorities.ApprovalState = "rejected"
		fixture.Authorities.DecisionState = "rejected"
		syncContinuationChildFixtureDigestV1(t, fixture)
	case strings.HasPrefix(mutation, "drift:"):
		mutateContinuationChildRequestPathV1(t, &fixture.Request, strings.TrimPrefix(mutation, "drift:"))
		syncContinuationChildFixtureDigestV1(t, fixture)
	case mutation == "claimed_digest":
		fixture.ClaimedRequestDigest = "sha256:9393939393939393939393939393939393939393939393939393939393939393"
	case mutation == "identity:user":
		fixture.Authorities.AuthenticatedIdentity.UserID = "019f4600-0000-7000-8000-000000009911"
	case mutation == "identity:project":
		fixture.Authorities.AuthenticatedIdentity.ProjectID = "019f4600-0000-7000-8000-000000009912"
	case mutation == "identity:session":
		fixture.Authorities.AuthenticatedIdentity.SessionID = "019f4600-0000-7000-8000-000000009913"
	case strings.HasPrefix(mutation, "source_mapping:"):
		field := strings.TrimPrefix(mutation, "source_mapping:")
		switch field {
		case "input":
			fixture.Authorities.SourceMapping.InputID = "019f4600-0000-7000-8000-000000009921"
		case "turn":
			fixture.Authorities.SourceMapping.TurnID = "019f4600-0000-7000-8000-000000009922"
		case "run":
			fixture.Authorities.SourceMapping.RunID = "019f4600-0000-7000-8000-000000009923"
		case "approval":
			fixture.Authorities.SourceMapping.ApprovalID = "019f4600-0000-7000-8000-000000009924"
		case "decision":
			fixture.Authorities.SourceMapping.DecisionID = "019f4600-0000-7000-8000-000000009925"
		case "action":
			fixture.Authorities.SourceMapping.Action = "reject"
		}
	case strings.HasPrefix(mutation, "root_authority:"):
		field := strings.TrimPrefix(mutation, "root_authority:")
		switch field {
		case "turn_kind":
			fixture.Authorities.Root.TurnKind = "continuation"
		case "write_state":
			fixture.Authorities.Root.WriteState = "open"
		case "root_ref":
			fixture.Authorities.Root.RootToolReceiptID = ""
		case "tool_call":
			fixture.Authorities.Root.OriginalToolCallID = "019f4600-0000-7000-8000-000000009931"
		}
	case strings.HasPrefix(mutation, "parent_authority:"):
		field := strings.TrimPrefix(mutation, "parent_authority:")
		switch field {
		case "write_state":
			fixture.Authorities.Parent.WriteState = "open"
		case "result_status":
			fixture.Authorities.Parent.ResultStatus = "completed"
		case "root_ref":
			fixture.Authorities.Parent.RootToolReceiptID = fixture.Request.Parent.ToolReceiptID
		case "approval":
			fixture.Authorities.Parent.ApprovalID = "019f4600-0000-7000-8000-000000009941"
		}
	case mutation == "approval_state":
		fixture.Authorities.ApprovalState = "pending"
	case mutation == "decision_state":
		fixture.Authorities.DecisionState = "pending"
	case mutation == "first_write_conflict":
		fixture.Authorities.FirstWriteAvailable = false
	case strings.HasPrefix(mutation, "existing:"):
		fixture.Scenario.ExistingChild = strings.TrimPrefix(mutation, "existing:")
	case strings.HasPrefix(mutation, "stored:"):
		item := strings.TrimPrefix(mutation, "stored:")
		if fixture.Scenario.StoredMutation == "" {
			fixture.Scenario.StoredMutation = item
		} else {
			fixture.Scenario.StoredMutation += "," + item
		}
	case strings.HasPrefix(mutation, "pre_guard:"):
		fixture.Scenario.PreGuard = strings.TrimPrefix(mutation, "pre_guard:")
	case strings.HasPrefix(mutation, "consumption:"):
		fixture.Scenario.Consumption = strings.TrimPrefix(mutation, "consumption:")
	case strings.HasPrefix(mutation, "post_guard:"):
		fixture.Scenario.PostConsumptionGuard = strings.TrimPrefix(mutation, "post_guard:")
	case strings.HasPrefix(mutation, "business:"):
		fixture.Scenario.BusinessObservation = strings.TrimPrefix(mutation, "business:")
	case strings.HasPrefix(mutation, "violation:"):
		item := strings.TrimPrefix(mutation, "violation:")
		if fixture.Scenario.InvariantViolation == "" {
			fixture.Scenario.InvariantViolation = item
		} else {
			fixture.Scenario.InvariantViolation += "," + item
		}
	case mutation == "stale_fence":
		fixture.Scenario.PresentedFence = fixture.Scenario.CurrentFence - 1
	case strings.HasPrefix(mutation, "shape:"):
		switch strings.TrimPrefix(mutation, "shape:") {
		case "uuid":
			fixture.CurrentReceiptID = "not-a-uuid"
		case "digest":
			fixture.Request.Decision.DecisionDigest = "sha256:not-a-digest"
		case "integer":
			fixture.Request.Decision.CardRevision = 0
		case "owner":
			fixture.Request.InheritedExecution.ToolPinOwner = "caller.controlled"
		case "fixed_schema":
			fixture.Request.InheritedExecution.ResultSchemaVersion = "graph_tool_result.v2"
		default:
			t.Fatalf("未知 child shape mutation=%s", mutation)
		}
		syncContinuationChildFixtureDigestV1(t, fixture)
	case mutation == "schema_invalid":
		fixture.Request.SchemaVersion = "continuation_child_tool_receipt_request.v2"
		syncContinuationChildFixtureDigestV1(t, fixture)
	case mutation == "action_invalid":
		fixture.Request.Decision.Action = "cancel"
		syncContinuationChildFixtureDigestV1(t, fixture)
	default:
		t.Fatalf("未知 child mutation=%s", mutation)
	}
}

func continuationChildRequestDigestV1(request continuationChildRequestV1) (string, error) {
	raw, err := canonicalJSON(request)
	if err != nil {
		return "", err
	}
	return semanticDigest(continuationChildDigestDomainV1, raw), nil
}

func syncContinuationChildFixtureDigestV1(t *testing.T, fixture *continuationChildFixtureV1) {
	t.Helper()
	digest, err := continuationChildRequestDigestV1(fixture.Request)
	if err != nil {
		t.Fatal(err)
	}
	fixture.ClaimedRequestDigest = digest
}

func cloneContinuationChildFixtureV1(value continuationChildFixtureV1) continuationChildFixtureV1 {
	raw, _ := canonicalJSON(value)
	var clone continuationChildFixtureV1
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func cloneContinuationChildRequestV1(value continuationChildRequestV1) continuationChildRequestV1 {
	raw, _ := canonicalJSON(value)
	var clone continuationChildRequestV1
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func strictContinuationChildRequestV1(raw []byte) error {
	if err := inspectJSON(raw); err != nil {
		return err
	}
	var request continuationChildRequestV1
	if err := strictDecode(raw, &request); err != nil {
		return err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return err
	}
	want := map[string][]string{
		"":                    {"schema_version", "effect_kind", "scope", "child_key", "causal", "root", "parent", "approval", "decision", "inherited_execution"},
		"scope":               {"user_id", "project_id", "session_id"},
		"child_key":           {"session_id", "continuation_turn_id", "original_tool_call_id"},
		"causal":              {"continuation_source_id", "input_id", "turn_id", "run_id"},
		"root":                {"tool_receipt_id", "tool_receipt_version", "turn_id", "run_id", "request_semantic_digest"},
		"parent":              {"tool_receipt_id", "tool_receipt_version", "turn_id", "run_id", "request_semantic_digest", "result_status", "result_digest"},
		"approval":            {"approval_type", "approval_id", "presented_approval_version", "resulting_approval_version", "binding_digest"},
		"decision":            {"decision_receipt_id", "decision_digest", "decision_id", "action", "card_id", "card_revision", "actor_user_id", "actor_project_id"},
		"inherited_execution": {"intent_digest", "tool_pin_owner", "tool_pin_ref", "tool_pin_digest", "tool_key", "definition_version", "intent_schema_version", "result_schema_version", "graph_key", "execution_digest", "parent_turn_context_digest", "resource_id", "resource_version", "resource_digest", "target_exact_set_digest"},
	}
	if err := exactContinuationChildJSONFieldsV1(top, want[""]); err != nil {
		return err
	}
	for key, fields := range want {
		if key == "" {
			continue
		}
		if string(top[key]) == "null" {
			return fmt.Errorf("required object=%s", key)
		}
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(top[key], &nested); err != nil {
			return err
		}
		if err := exactContinuationChildJSONFieldsV1(nested, fields); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	return nil
}

func exactContinuationChildJSONFieldsV1(value map[string]json.RawMessage, fields []string) error {
	if len(value) != len(fields) {
		return fmt.Errorf("field count=%d want=%d", len(value), len(fields))
	}
	for _, field := range fields {
		raw, exists := value[field]
		if !exists || string(raw) == "null" {
			return fmt.Errorf("required field=%s", field)
		}
	}
	return nil
}

func continuationChildRequestLeafPathsV1() []string {
	return []string{
		"schema_version", "effect_kind",
		"scope.user_id", "scope.project_id", "scope.session_id",
		"child_key.session_id", "child_key.continuation_turn_id", "child_key.original_tool_call_id",
		"causal.continuation_source_id", "causal.input_id", "causal.turn_id", "causal.run_id",
		"root.tool_receipt_id", "root.tool_receipt_version", "root.turn_id", "root.run_id", "root.request_semantic_digest",
		"parent.tool_receipt_id", "parent.tool_receipt_version", "parent.turn_id", "parent.run_id", "parent.request_semantic_digest", "parent.result_status", "parent.result_digest",
		"approval.approval_type", "approval.approval_id", "approval.presented_approval_version", "approval.resulting_approval_version", "approval.binding_digest",
		"decision.decision_receipt_id", "decision.decision_digest", "decision.decision_id", "decision.action", "decision.card_id", "decision.card_revision", "decision.actor_user_id", "decision.actor_project_id",
		"inherited_execution.intent_digest", "inherited_execution.tool_pin_owner", "inherited_execution.tool_pin_ref", "inherited_execution.tool_pin_digest", "inherited_execution.tool_key", "inherited_execution.definition_version", "inherited_execution.intent_schema_version", "inherited_execution.result_schema_version", "inherited_execution.graph_key", "inherited_execution.execution_digest", "inherited_execution.parent_turn_context_digest", "inherited_execution.resource_id", "inherited_execution.resource_version", "inherited_execution.resource_digest", "inherited_execution.target_exact_set_digest",
	}
}

func mutateContinuationChildRequestPathV1(t *testing.T, request *continuationChildRequestV1, path string) {
	t.Helper()
	raw, err := canonicalJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(path, ".")
	cursor := value
	for _, part := range parts[:len(parts)-1] {
		next, ok := cursor[part].(map[string]any)
		if !ok {
			t.Fatalf("path=%s part=%s not object", path, part)
		}
		cursor = next
	}
	leaf := parts[len(parts)-1]
	current, exists := cursor[leaf]
	if !exists {
		t.Fatalf("unknown path=%s", path)
	}
	switch typed := current.(type) {
	case float64:
		cursor[leaf] = typed + 1
	case string:
		switch {
		case strings.Contains(leaf, "digest"):
			if len(typed) == 64 && !strings.HasPrefix(typed, "sha256:") {
				cursor[leaf] = strings.Repeat("9", 64)
			} else {
				cursor[leaf] = "sha256:" + strings.Repeat("9", 64)
			}
		case strings.HasSuffix(leaf, "_id") || leaf == "user_id" || leaf == "project_id" || leaf == "session_id" || leaf == "turn_id" || leaf == "run_id":
			cursor[leaf] = "019f4600-0000-7000-8000-000000009999"
		case path == "decision.action":
			cursor[leaf] = "reject"
		case path == "parent.result_status":
			cursor[leaf] = "completed"
		default:
			cursor[leaf] = typed + ".drift"
		}
	default:
		t.Fatalf("unsupported path=%s type=%T", path, current)
	}
	mutated, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := strictDecode(mutated, request); err != nil {
		t.Fatal(err)
	}
}

func continuationChildCaseIDsV1(corpus continuationChildCorpusV1) []string {
	ids := make([]string, 0, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		ids = append(ids, testCase.ID)
	}
	return ids
}

func continuationChildCaseByIDV1(t *testing.T, corpus continuationChildCorpusV1, id string) continuationChildCaseV1 {
	t.Helper()
	for _, testCase := range corpus.Cases {
		if testCase.ID == id {
			return testCase
		}
	}
	t.Fatalf("missing child case=%s", id)
	return continuationChildCaseV1{}
}

func continuationChildHasCasePrefixV1(corpus continuationChildCorpusV1, prefix string) bool {
	for _, testCase := range corpus.Cases {
		if strings.HasPrefix(testCase.ID, prefix) {
			return true
		}
	}
	return false
}

func assertContinuationChildCaseV1(t *testing.T, corpus continuationChildCorpusV1, id, outcome string, roles []string) {
	t.Helper()
	testCase := continuationChildCaseByIDV1(t, corpus, id)
	got := evaluateContinuationChildCaseV1(t, corpus.Fixture, testCase)
	if got.Outcome != outcome || !reflect.DeepEqual(continuationChildSlotRolesV1(got.Snapshot), roles) {
		t.Fatalf("%s outcome=%s roles=%v", id, got.Outcome, continuationChildSlotRolesV1(got.Snapshot))
	}
}

func projectContinuationChildToConsumptionV1(t *testing.T, child continuationChildFixtureV1, base approvalConsumptionFixtureV1) approvalConsumptionFixtureV1 {
	t.Helper()
	request := child.Request
	intent := base.RecordCommand.IntentBinding
	intent.ApprovalID = request.Approval.ApprovalID
	intent.PresentedApprovalVersion = request.Approval.PresentedApprovalVersion
	intent.ResultingApprovalVersion = request.Approval.ResultingApprovalVersion
	intent.DecisionReceiptID = request.Decision.DecisionReceiptID
	intent.DecisionID = request.Decision.DecisionID
	intent.DecisionDigest = request.Decision.DecisionDigest
	intent.ContinuationSourceID = request.Causal.ContinuationSourceID
	intent.UserID, intent.ProjectID, intent.SessionID = request.Scope.UserID, request.Scope.ProjectID, request.Scope.SessionID
	intent.RootToolReceiptID, intent.RootToolReceiptVersion = request.Root.ToolReceiptID, request.Root.ToolReceiptVersion
	intent.RootTurnID, intent.RootRunID, intent.RootRequestSemanticDigest = request.Root.TurnID, request.Root.RunID, request.Root.RequestSemanticDigest
	intent.ParentToolReceiptID, intent.ParentToolReceiptVersion = request.Parent.ToolReceiptID, request.Parent.ToolReceiptVersion
	intent.ParentTurnID, intent.ParentRunID, intent.ParentRequestSemanticDigest = request.Parent.TurnID, request.Parent.RunID, request.Parent.RequestSemanticDigest
	intent.ParentResultStatus, intent.ParentResultDigest = request.Parent.ResultStatus, request.Parent.ResultDigest
	intent.OriginalToolCallID = request.ChildKey.OriginalToolCallID
	intent.ParentToolReceiptOwnerRef = "tool-receipt:" + request.Parent.ToolReceiptID + "@v1"
	intent.ContinuationInputID, intent.ContinuationTurnID, intent.ContinuationRunID = request.Causal.InputID, request.Causal.TurnID, request.Causal.RunID
	intent.ChildToolReceiptID = child.CurrentReceiptID
	intent.ToolPinOwner, intent.ToolPinRef, intent.ToolPinDigest = request.InheritedExecution.ToolPinOwner, request.InheritedExecution.ToolPinRef, request.InheritedExecution.ToolPinDigest
	intent.ToolKey, intent.DefinitionVersion = request.InheritedExecution.ToolKey, request.InheritedExecution.DefinitionVersion
	intent.IntentSchemaVersion, intent.ResultSchemaVersion, intent.GraphKey = request.InheritedExecution.IntentSchemaVersion, request.InheritedExecution.ResultSchemaVersion, request.InheritedExecution.GraphKey
	intent.IntentDigest = request.InheritedExecution.IntentDigest
	intent.ResourceID, intent.ResourceVersion, intent.ResourceDigest = request.InheritedExecution.ResourceID, request.InheritedExecution.ResourceVersion, request.InheritedExecution.ResourceDigest
	intent.TargetExactSetDigest = request.InheritedExecution.TargetExactSetDigest
	base.RecordCommand.IntentBinding = intent
	base.DecisionAuthority.FrozenIntent = intent
	base.RecordCommand.AuthenticatedIdentity.UserID, base.RecordCommand.AuthenticatedIdentity.ProjectID, base.RecordCommand.AuthenticatedIdentity.SessionID = request.Scope.UserID, request.Scope.ProjectID, request.Scope.SessionID
	base.QueryExpectedBinding.UserID, base.QueryExpectedBinding.ProjectID, base.QueryExpectedBinding.SessionID = request.Scope.UserID, request.Scope.ProjectID, request.Scope.SessionID
	base.QueryExpectedBinding.ApprovalID, base.QueryExpectedBinding.ChildToolReceiptID = request.Approval.ApprovalID, child.CurrentReceiptID
	base.CurrentEligibility.ResourceID, base.CurrentEligibility.ResourceVersion, base.CurrentEligibility.ResourceDigest = intent.ResourceID, intent.ResourceVersion, intent.ResourceDigest
	base.CurrentEligibility.TargetExactSetDigest = intent.TargetExactSetDigest
	base.FirstWriteMaterial.ReceiptID = "019f4600-0000-7000-8000-000000000501"
	syncApprovalConsumptionRequestDigestV1(t, &base)
	return base
}

func evaluateContinuationChildBridgeV1(t *testing.T, child continuationChildFixtureV1, bridge string) continuationChildEvaluationV1 {
	t.Helper()
	switch bridge {
	case "r04_record":
		_, fixtures := loadApprovalConsumptionCorpusV1(t)
		projected := projectContinuationChildToConsumptionV1(t, child, cloneApprovalConsumptionFixtureV1(fixtures["acr.creation_spec_activation.approved_unconsumed"]))
		result := evaluateApprovalConsumptionRecordV1(projected)
		if result.Decision != "recorded" || result.Core == nil || result.Core.IntentBinding.ChildToolReceiptID != child.CurrentReceiptID {
			return continuationChildRejectV1("BRIDGE_EVIDENCE_MISMATCH")
		}
	case "r04_conflict":
		if result := evaluateApprovalConsumptionSharedCaseV1(t, "ACR-N15-consumption-digest-conflict"); result.Decision != "conflict" || !reflect.DeepEqual(result.ReasonCodes, []string{"CONSUMPTION_DIGEST_CONFLICT"}) {
			return continuationChildRejectV1("BRIDGE_EVIDENCE_MISMATCH")
		}
		fixture := cloneContinuationChildFixtureV1(child)
		fixture.Scenario.Consumption = "determined_conflict"
		mapped := reduceContinuationChildV1(fixture, child.Request)
		if mapped.Outcome != "frozen" || mapped.Code != "APPROVAL_CONSUMPTION_REJECTED" || !reflect.DeepEqual(continuationChildSlotRolesV1(mapped.Snapshot), []string{"approval_decision"}) {
			return continuationChildRejectV1("BRIDGE_EVIDENCE_MISMATCH")
		}
	case "r04_not_found":
		if result := evaluateApprovalConsumptionSharedCaseV1(t, "ACR-P05-query-not-found"); result.Decision != "not_found" || !reflect.DeepEqual(result.ReasonCodes, []string{"NOT_FOUND"}) {
			return continuationChildRejectV1("BRIDGE_EVIDENCE_MISMATCH")
		}
		fixture := cloneContinuationChildFixtureV1(child)
		fixture.Scenario.Consumption = "query_not_found"
		mapped := reduceContinuationChildV1(fixture, child.Request)
		if mapped.Outcome != "recovery_pending" || mapped.Code != "UNKNOWN_OUTCOME" {
			return continuationChildRejectV1("BRIDGE_EVIDENCE_MISMATCH")
		}
	case "r01_approve_not_committed":
		if err := evaluateContinuationChildR01CaseV1(t, "TR-FA-P02-approve-not-committed"); err != nil {
			return continuationChildRejectV1("BRIDGE_EVIDENCE_MISMATCH")
		}
	case "r01_reject_not_committed":
		if err := evaluateContinuationChildR01CaseV1(t, "TR-FA-P03-reject-not-committed"); err != nil {
			return continuationChildRejectV1("BRIDGE_EVIDENCE_MISMATCH")
		}
	case "r01_unknown_unresolved":
		if err := evaluateContinuationChildR01CaseV1(t, "TR-FA-N05-business-unknown-unresolved"); errorCode(err) != "TOOL_EXECUTION_SLOT_UNRESOLVED" {
			return continuationChildRejectV1("BRIDGE_EVIDENCE_MISMATCH")
		}
	default:
		return continuationChildRejectV1("BRIDGE_EVIDENCE_MISMATCH")
	}
	return continuationChildEvaluationV1{Outcome: "validated"}
}

func evaluateApprovalConsumptionSharedCaseV1(t *testing.T, id string) approvalConsumptionEvaluationV1 {
	t.Helper()
	corpus, fixtures := loadApprovalConsumptionCorpusV1(t)
	for _, testCase := range corpus.Cases {
		if testCase.ID == id {
			return evaluateApprovalConsumptionCaseV1(t, fixtures, testCase)
		}
	}
	t.Fatalf("missing R04 bridge case=%s", id)
	return approvalConsumptionEvaluationV1{}
}

func evaluateContinuationChildR01CaseV1(t *testing.T, id string) error {
	t.Helper()
	corpus := loadFailedAfterReceiptCorpusV1(t)
	policies := receiptPolicySetV1{Result: buildResultPolicies(t, loadResultCorpus(t)), Slots: buildSlotPolicies(t, corpus.SlotPolicies)}
	for _, testCase := range corpus.Cases {
		if testCase.ID == id {
			_, err := evaluateFailedAfterReceiptCaseV1(corpus, testCase, policies)
			return err
		}
	}
	t.Fatalf("missing R01 bridge case=%s", id)
	return nil
}
