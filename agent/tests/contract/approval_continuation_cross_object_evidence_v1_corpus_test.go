// Package contract_test 只承载未 Approved 的 W2-R03 Approval Continuation 跨对象证据候选，
// 不提供生产 Approval Store、Continuation Runner、Migration 或 Graph 实现。
package contract_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	crossObjectCorpusPathV1    = "testdata/w2_r03_cross_object/approval_continuation_cross_object_evidence_v1.json"
	crossObjectManifestPathV1  = "testdata/w2_r03_cross_object/manifest.json"
	approvalDigestDomainV1     = "dora.approval_binding.v1"
	decisionDigestDomainV1     = "dora.approval_decision_receipt.v1"
	toolPinDigestDomainV1      = "dora.graph_tool_pin.v1"
	receiptOwnerDigestDomainV1 = "dora.tool_receipt_owner_record.v1"
	crossObjectDigestDomainV1  = "dora.approval_continuation_cross_object_evidence.v1"
	crossObjectFixtureIDV1     = "coe.approval_continuation.plan_creation_spec"
	crossObjectVectorCountV1   = 20
)

type crossObjectManifestV1 struct {
	SchemaVersion    string                     `json:"schema_version"`
	Files            []messageSetManifestFileV1 `json:"files"`
	FixtureIDs       []string                   `json:"fixture_ids"`
	VectorIDs        []string                   `json:"vector_ids"`
	TotalVectorCount int                        `json:"total_vector_count"`
	TargetTests      []string                   `json:"target_tests"`
}

type crossObjectCorpusV1 struct {
	SchemaVersion string                 `json:"schema_version"`
	ExactSets     crossObjectExactSetsV1 `json:"exact_sets"`
	Fixtures      []crossObjectFixtureV1 `json:"fixtures"`
	Cases         []crossObjectCaseV1    `json:"cases"`
}

type crossObjectExactSetsV1 struct {
	Decisions       []string `json:"decisions"`
	ReasonCodes     []string `json:"reason_codes"`
	ApprovalStates  []string `json:"approval_states"`
	DecisionActions []string `json:"decision_actions"`
	ToolKeys        []string `json:"tool_keys"`
}

type crossObjectFixtureV1 struct {
	FixtureID                string                                     `json:"fixture_id"`
	BaseTurnContextFixtureID string                                     `json:"base_turn_context_fixture_id"`
	ParentReceipt            approvalContinuationParentReceiptFixtureV1 `json:"parent_receipt"`
	ParentReceiptOwnerRecord crossObjectReceiptOwnerRecordV1            `json:"parent_receipt_owner_record"`
	StoredReceiptOwnerDigest string                                     `json:"stored_receipt_owner_digest"`
	Approval                 crossObjectApprovalBindingV1               `json:"approval"`
	StoredApprovalDigest     string                                     `json:"stored_approval_digest"`
	Decision                 crossObjectDecisionReceiptV1               `json:"decision_receipt"`
	StoredDecisionDigest     string                                     `json:"stored_decision_digest"`
	ToolPin                  crossObjectToolPinV1                       `json:"tool_pin"`
	StoredToolPinDigest      string                                     `json:"stored_tool_pin_digest"`
	TurnContext              turnContextFixtureV1                       `json:"turn_context"`
	StoredEvidenceDigest     string                                     `json:"stored_evidence_digest"`
	OperationalMetadata      crossObjectOperationalMetadataV1           `json:"operational_metadata"`
}

type crossObjectReceiptOwnerRecordV1 struct {
	SchemaVersion  string `json:"schema_version"`
	Owner          string `json:"owner"`
	Ref            string `json:"ref"`
	ReceiptID      string `json:"receipt_id"`
	ReceiptVersion int64  `json:"receipt_version"`
	WriteState     string `json:"write_state"`
	SnapshotDigest string `json:"snapshot_digest"`
}

// crossObjectApprovalBindingV1 是 Approval 创建时冻结的不可变 binding；ApprovalVersion
// 是原 waiting_user Receipt 展示并由 Decision CAS 校验的版本，不冒充生产聚合物理行。
type crossObjectApprovalBindingV1 struct {
	SchemaVersion               string `json:"schema_version"`
	ApprovalID                  string `json:"approval_id"`
	ApprovalVersion             int64  `json:"approval_version"`
	CreationState               string `json:"creation_state"`
	ApprovalType                string `json:"approval_type"`
	UserID                      string `json:"user_id"`
	ProjectID                   string `json:"project_id"`
	SessionID                   string `json:"session_id"`
	RootToolReceiptID           string `json:"root_tool_receipt_id"`
	OriginalTurnID              string `json:"original_turn_id"`
	OriginalRunID               string `json:"original_run_id"`
	OriginalToolCallID          string `json:"original_tool_call_id"`
	ParentRequestSemanticDigest string `json:"parent_request_semantic_digest"`
	IntentDigest                string `json:"intent_digest"`
	ExecutionDigest             string `json:"execution_digest"`
	ToolPinOwner                string `json:"tool_pin_owner"`
	ToolPinRef                  string `json:"tool_pin_ref"`
	ToolPinDigest               string `json:"tool_pin_digest"`
	CardID                      string `json:"card_id"`
	CardRevision                int64  `json:"card_revision"`
	ResourceID                  string `json:"resource_id"`
	ResourceVersion             int64  `json:"resource_version"`
	ResourceDigest              string `json:"resource_digest"`
	TargetExactSetDigest        string `json:"target_exact_set_digest"`
	ExpiresAt                   string `json:"expires_at"`
}

type crossObjectDecisionReceiptV1 struct {
	SchemaVersion            string `json:"schema_version"`
	DecisionReceiptID        string `json:"decision_receipt_id"`
	DecisionID               string `json:"decision_id"`
	RequestID                string `json:"request_id"`
	ApprovalID               string `json:"approval_id"`
	PresentedApprovalVersion int64  `json:"presented_approval_version"`
	ResultingApprovalVersion int64  `json:"resulting_approval_version"`
	Action                   string `json:"action"`
	ResultingState           string `json:"resulting_state"`
	ActorUserID              string `json:"actor_user_id"`
	ActorProjectID           string `json:"actor_project_id"`
	CardID                   string `json:"card_id"`
	CardRevision             int64  `json:"card_revision"`
	ApprovalBindingDigest    string `json:"approval_binding_digest"`
	ContinuationSourceID     string `json:"continuation_source_id"`
	ContinuationInputID      string `json:"continuation_input_id"`
}

type crossObjectToolPinV1 struct {
	SchemaVersion       string `json:"schema_version"`
	Owner               string `json:"owner"`
	Ref                 string `json:"ref"`
	ToolKey             string `json:"tool_key"`
	DefinitionVersion   string `json:"definition_version"`
	IntentSchemaVersion string `json:"intent_schema_version"`
	ResultSchemaVersion string `json:"result_schema_version"`
	GraphKey            string `json:"graph_key"`
}

type crossObjectOperationalMetadataV1 struct {
	TraceID           string `json:"trace_id"`
	Attempt           int64  `json:"attempt"`
	ProcessorInstance string `json:"processor_instance"`
	ReadAt            string `json:"read_at"`
}

type crossObjectCaseV1 struct {
	ID          string                `json:"id"`
	FromFixture string                `json:"from_fixture"`
	Mutations   []string              `json:"mutations"`
	Expected    crossObjectExpectedV1 `json:"expected"`
}

type crossObjectExpectedV1 struct {
	Decision    string   `json:"decision"`
	ReasonCodes []string `json:"reason_codes"`
}

type crossObjectEvaluationV1 struct {
	Decision              string
	ReasonCodes           []string
	EvidenceDigest        string
	ContextDigest         string
	ReceiptSnapshotDigest string
	ReceiptOwnerDigest    string
}

func TestW2R03CrossObjectEvidenceManifest(t *testing.T) {
	manifest := loadCrossObjectManifestV1(t)
	wantVectors := crossObjectVectorIDsV1()
	wantTests := []string{
		"TestW2R03CrossObjectEvidenceManifest",
		"TestApprovalContinuationCrossObjectEvidenceV1Corpus",
		"TestApprovalContinuationCrossObjectEvidenceV1GoldenDigests",
		"TestApprovalContinuationCrossObjectEvidenceV1ExactSets",
		"TestApprovalContinuationCrossObjectEvidenceV1ReceiptBinding",
		"TestApprovalContinuationCrossObjectEvidenceV1ApprovalDecisionBinding",
		"TestApprovalContinuationCrossObjectEvidenceV1PinnedToolBinding",
		"TestApprovalContinuationCrossObjectEvidenceV1TurnContextBinding",
		"TestApprovalContinuationCrossObjectEvidenceV1ReasonPriority",
		"TestApprovalContinuationCrossObjectEvidenceV1StrictJSON",
		"TestApprovalContinuationCrossObjectEvidenceV1DigestEncoding",
		"TestApprovalContinuationCrossObjectEvidenceV1LegacyAuthorityIsolation",
		"TestApprovalContinuationCrossObjectEvidenceV1OperationalMetadataExcluded",
	}
	if manifest.SchemaVersion != "w2_r03_cross_object_manifest.v1" || manifest.TotalVectorCount != crossObjectVectorCountV1 ||
		!reflect.DeepEqual(manifest.FixtureIDs, []string{crossObjectFixtureIDV1}) ||
		!reflect.DeepEqual(manifest.VectorIDs, wantVectors) || !reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("R03 manifest exact-set 不符: %+v", manifest)
	}
	actualTests := crossObjectTargetTestNamesV1(t)
	sortedWant := append([]string(nil), wantTests...)
	sort.Strings(sortedWant)
	if !reflect.DeepEqual(actualTests, sortedWant) {
		t.Fatalf("R03 manifest target tests 未绑定 AST actual=%v want=%v", actualTests, sortedWant)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].File != "approval_continuation_cross_object_evidence_v1.json" || manifest.Files[0].VectorCount != crossObjectVectorCountV1 {
		t.Fatalf("R03 manifest files=%+v", manifest.Files)
	}
	raw, err := os.ReadFile(filepath.Join("testdata/w2_r03_cross_object", manifest.Files[0].File))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != manifest.Files[0].SHA256 {
		t.Fatalf("R03 corpus sha=%s want=%s", got, manifest.Files[0].SHA256)
	}
	corpus := loadCrossObjectCorpusV1(t)
	if len(corpus.Fixtures) != 1 || corpus.Fixtures[0].FixtureID != manifest.FixtureIDs[0] || len(corpus.Cases) != manifest.TotalVectorCount {
		t.Fatalf("R03 manifest 未绑定 corpus")
	}
	caseIDs := make([]string, 0, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		caseIDs = append(caseIDs, testCase.ID)
	}
	sort.Strings(caseIDs)
	if !reflect.DeepEqual(caseIDs, manifest.VectorIDs) {
		t.Fatalf("R03 vector IDs 未绑定 manifest got=%v", caseIDs)
	}
}

func TestApprovalContinuationCrossObjectEvidenceV1Corpus(t *testing.T) {
	corpus := loadCrossObjectCorpusV1(t)
	fixtures := map[string]crossObjectFixtureV1{}
	for _, fixture := range corpus.Fixtures {
		fixtures[fixture.FixtureID] = fixture
	}
	seen := map[string]struct{}{}
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
			got := evaluateCrossObjectCaseV1(t, fixture, testCase.Mutations, true)
			if got.Decision != testCase.Expected.Decision || !reflect.DeepEqual(got.ReasonCodes, testCase.Expected.ReasonCodes) {
				t.Fatalf("evaluation=%+v want=%+v", got, testCase.Expected)
			}
		})
	}
	if len(seen) != crossObjectVectorCountV1 {
		t.Fatalf("case count=%d", len(seen))
	}
}

func TestApprovalContinuationCrossObjectEvidenceV1GoldenDigests(t *testing.T) {
	fixture := onlyCrossObjectFixtureV1(t)
	evaluation := evaluateCrossObjectCaseV1(t, fixture, nil, false)
	if evaluation.Decision != "valid" || evaluation.EvidenceDigest != fixture.StoredEvidenceDigest ||
		evaluation.ContextDigest != fixture.TurnContext.StoredContextDigest || evaluation.ReceiptSnapshotDigest != fixture.ParentReceipt.ExpectedSnapshotDigest ||
		evaluation.ReceiptOwnerDigest != fixture.StoredReceiptOwnerDigest {
		t.Fatalf("golden evaluation=%+v fixture evidence=%s context=%s snapshot=%s owner=%s", evaluation, fixture.StoredEvidenceDigest, fixture.TurnContext.StoredContextDigest, fixture.ParentReceipt.ExpectedSnapshotDigest, fixture.StoredReceiptOwnerDigest)
	}
	assertStoredDigestV1(t, receiptOwnerDigestDomainV1, fixture.ParentReceiptOwnerRecord, fixture.StoredReceiptOwnerDigest)
	assertStoredDigestV1(t, approvalDigestDomainV1, fixture.Approval, fixture.StoredApprovalDigest)
	assertStoredDigestV1(t, decisionDigestDomainV1, fixture.Decision, fixture.StoredDecisionDigest)
	assertStoredDigestV1(t, toolPinDigestDomainV1, fixture.ToolPin, fixture.StoredToolPinDigest)
}

func TestApprovalContinuationCrossObjectEvidenceV1ExactSets(t *testing.T) {
	corpus := loadCrossObjectCorpusV1(t)
	want := crossObjectExactSetsV1{
		Decisions:       []string{"invalid", "valid"},
		ReasonCodes:     []string{"APPROVAL_BINDING_MISMATCH", "APPROVAL_STATE_INVALID", "CONTINUATION_SOURCE_MISMATCH", "DECISION_BINDING_MISMATCH", "IDENTITY_MISMATCH", "LEGACY_AUTHORITY_FORBIDDEN", "PARENT_RECEIPT_INVALID", "PARENT_REQUEST_DIGEST_MISMATCH", "RESOLVED_REF_EXACT_SET_MISMATCH", "SCHEMA_INVALID", "TOOL_PIN_BINDING_MISMATCH", "TURN_CONTEXT_INVALID"},
		ApprovalStates:  []string{"approved", "cancelled", "expired", "pending", "rejected"},
		DecisionActions: []string{"approve", "reject"},
		ToolKeys:        []string{"analyze_materials", "assemble_output", "generate_media", "plan_creation_spec", "plan_storyboard", "write_prompts"},
	}
	if !reflect.DeepEqual(corpus.ExactSets, want) {
		t.Fatalf("exact sets=%+v want=%+v", corpus.ExactSets, want)
	}
}

func TestApprovalContinuationCrossObjectEvidenceV1ReceiptBinding(t *testing.T) {
	fixture := onlyCrossObjectFixtureV1(t)
	snapshot, digest, reason := buildCrossObjectReceiptV1(t, fixture)
	if reason != "" || digest != fixture.ParentReceipt.ExpectedSnapshotDigest {
		t.Fatalf("receipt reason=%s digest=%s", reason, digest)
	}
	approvalRef := snapshot.Result.ApprovalRef
	if snapshot.ReceiptID != fixture.Approval.RootToolReceiptID || snapshot.SessionID != fixture.Approval.SessionID ||
		snapshot.TurnID != fixture.Approval.OriginalTurnID || snapshot.RunID != fixture.Approval.OriginalRunID ||
		snapshot.ToolCallID != fixture.Approval.OriginalToolCallID || snapshot.RequestSemanticDigest != fixture.Approval.ParentRequestSemanticDigest ||
		approvalRef == nil || approvalRef.ApprovalID != fixture.Approval.ApprovalID || approvalRef.ApprovalVersion != fixture.Approval.ApprovalVersion ||
		approvalRef.ApprovalDigest != fixture.StoredApprovalDigest || approvalRef.CardID != fixture.Approval.CardID {
		t.Fatalf("Receipt→Approval binding 不符 snapshot=%+v approval=%+v", snapshot, fixture.Approval)
	}
	t.Run("valid_r01_non_approval_result_stays_in_approval_layer", func(t *testing.T) {
		got := evaluateCrossObjectCaseV1(t, fixture, []string{"receipt.result.failed_without_approval"}, true)
		if !reflect.DeepEqual(got.ReasonCodes, []string{"APPROVAL_BINDING_MISMATCH"}) {
			t.Fatalf("R01 内在合法的非 Approval Result 不得降级为 Parent Receipt 错误: %+v", got)
		}
	})
}

func TestApprovalContinuationCrossObjectEvidenceV1ApprovalDecisionBinding(t *testing.T) {
	fixture := onlyCrossObjectFixtureV1(t)
	if fixture.Approval.CreationState != "pending" || fixture.Decision.Action != "approve" || fixture.Decision.ResultingState != "approved" ||
		fixture.Decision.ApprovalID != fixture.Approval.ApprovalID || fixture.Decision.PresentedApprovalVersion != fixture.Approval.ApprovalVersion ||
		fixture.Decision.ResultingApprovalVersion != fixture.Approval.ApprovalVersion+1 || fixture.Decision.ActorUserID != fixture.Approval.UserID ||
		fixture.Decision.ActorProjectID != fixture.Approval.ProjectID || fixture.Decision.CardID != fixture.Approval.CardID ||
		fixture.Decision.CardRevision != fixture.Approval.CardRevision || fixture.Decision.ApprovalBindingDigest != fixture.StoredApprovalDigest {
		t.Fatalf("Approval→Decision binding 不符")
	}
}

func TestApprovalContinuationCrossObjectEvidenceV1PinnedToolBinding(t *testing.T) {
	fixture := onlyCrossObjectFixtureV1(t)
	base := fixture.ParentReceipt.BaseSnapshot
	if fixture.Approval.ToolPinOwner != fixture.ToolPin.Owner || fixture.Approval.ToolPinRef != fixture.ToolPin.Ref ||
		fixture.Approval.ToolPinDigest != fixture.StoredToolPinDigest || base.ToolKey != fixture.ToolPin.ToolKey ||
		base.DefinitionVersion != fixture.ToolPin.DefinitionVersion || base.IntentSchemaVersion != fixture.ToolPin.IntentSchemaVersion ||
		base.ResultSchemaVersion != fixture.ToolPin.ResultSchemaVersion || fixture.ToolPin.GraphKey != "plan_creation_spec_graph_v1" {
		t.Fatalf("Tool Pin binding 不符")
	}
	for _, mutation := range []string{
		"tool_pin.tool_key.drift", "tool_pin.definition_version.drift", "tool_pin.intent_schema_version.drift",
		"tool_pin.result_schema_version.drift", "tool_pin.graph_key.drift",
	} {
		t.Run(mutation, func(t *testing.T) {
			got := evaluateCrossObjectCaseV1(t, fixture, []string{mutation}, true)
			if !reflect.DeepEqual(got.ReasonCodes, []string{"TOOL_PIN_BINDING_MISMATCH"}) {
				t.Fatalf("Tool Pin sensitivity mutation=%s evaluation=%+v", mutation, got)
			}
		})
	}
}

func TestApprovalContinuationCrossObjectEvidenceV1TurnContextBinding(t *testing.T) {
	fixture := onlyCrossObjectFixtureV1(t)
	evaluation := evaluateCrossObjectCaseV1(t, fixture, nil, true)
	if evaluation.Decision != "valid" {
		t.Fatalf("完整 Turn Context evaluator 未通过: %+v", evaluation)
	}
	canonical := fixture.TurnContext.Canonical
	if canonical.ParentToolReceiptRef == nil || *canonical.ParentToolReceiptRef != "tool-receipt:"+fixture.ParentReceipt.BaseSnapshot.ReceiptID+"@v1" ||
		canonical.ParentToolReceiptDigest == nil || *canonical.ParentToolReceiptDigest != stripSHA256V1(t, fixture.StoredReceiptOwnerDigest) ||
		canonical.ParentRequestSemanticDigest == nil || *canonical.ParentRequestSemanticDigest != stripSHA256V1(t, fixture.ParentReceipt.BaseSnapshot.RequestSemanticDigest) ||
		canonical.ApprovalRef == nil || *canonical.ApprovalRef != "approval:"+fixture.Approval.ApprovalID+"@v1" ||
		canonical.ApprovalDigest == nil || *canonical.ApprovalDigest != stripSHA256V1(t, fixture.StoredApprovalDigest) ||
		canonical.PinnedToolRef == nil || *canonical.PinnedToolRef != fixture.ToolPin.Ref ||
		canonical.PinnedToolDigest == nil || *canonical.PinnedToolDigest != stripSHA256V1(t, fixture.StoredToolPinDigest) {
		t.Fatalf("Turn Context cross-object binding 不符")
	}
	t.Run("stored_context_digest_tamper", func(t *testing.T) {
		tampered := cloneCrossObjectFixtureV1(fixture)
		tampered.TurnContext.StoredContextDigest = strings.Repeat("6", 64)
		got := evaluateCrossObjectCaseV1(t, tampered, nil, true)
		if !reflect.DeepEqual(got.ReasonCodes, []string{"TURN_CONTEXT_INVALID"}) {
			t.Fatalf("Context golden tamper evaluation=%+v", got)
		}
	})
}

func TestApprovalContinuationCrossObjectEvidenceV1ReasonPriority(t *testing.T) {
	fixture := onlyCrossObjectFixtureV1(t)
	tests := []struct {
		name      string
		mutations []string
		want      string
	}{
		{"schema_over_receipt", []string{"context.schema.unknown", "receipt.schema.unknown"}, "SCHEMA_INVALID"},
		{"receipt_over_identity", []string{"receipt.schema.unknown", "approval.origin_turn_id.mismatch"}, "PARENT_RECEIPT_INVALID"},
		{"identity_over_parent_request", []string{"approval.origin_turn_id.mismatch", "context.parent_request_digest.mismatch"}, "IDENTITY_MISMATCH"},
		{"parent_request_over_approval", []string{"context.parent_request_digest.mismatch", "receipt.approval_id.mismatch"}, "PARENT_REQUEST_DIGEST_MISMATCH"},
		{"approval_over_decision", []string{"receipt.approval_id.mismatch", "decision.approval_id.mismatch"}, "APPROVAL_BINDING_MISMATCH"},
		{"decision_over_continuation", []string{"decision.approval_id.mismatch", "decision.continuation_source.mismatch"}, "DECISION_BINDING_MISMATCH"},
		{"continuation_over_tool_pin", []string{"decision.continuation_source.mismatch", "tool_pin.definition_version.drift"}, "CONTINUATION_SOURCE_MISMATCH"},
		{"tool_pin_over_parent_context", []string{"tool_pin.definition_version.drift", "context.parent_receipt_ref.mismatch"}, "TOOL_PIN_BINDING_MISMATCH"},
		{"parent_context_over_resolved_refs", []string{"context.parent_receipt_ref.mismatch", "context.resolved_refs.extra"}, "PARENT_RECEIPT_INVALID"},
		{"resolved_refs_over_turn_context", []string{"context.resolved_refs.extra", "context.stored_digest.mismatch"}, "RESOLVED_REF_EXACT_SET_MISMATCH"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			got := evaluateCrossObjectCaseV1(t, fixture, testCase.mutations, true)
			if !reflect.DeepEqual(got.ReasonCodes, []string{testCase.want}) {
				t.Fatalf("mutations=%v reason priority=%v want=%s", testCase.mutations, got.ReasonCodes, testCase.want)
			}
		})
	}
}

func TestApprovalContinuationCrossObjectEvidenceV1StrictJSON(t *testing.T) {
	var corpus crossObjectCorpusV1
	for _, raw := range [][]byte{
		[]byte(`{"schema_version":"x","exact_sets":{},"fixtures":[],"cases":[],"future":true}`),
		[]byte(`{"schema_version":"x","exact_sets":{},"fixtures":[],"cases":[]}{}`),
		[]byte(`{"schema_version":"x","schema_version":"y","exact_sets":{},"fixtures":[],"cases":[]}`),
	} {
		if err := messageSetStrictDecodeV1(raw, &corpus); err == nil {
			t.Fatalf("strict JSON 未拒绝 %s", raw)
		}
	}
	var manifest crossObjectManifestV1
	if err := messageSetStrictDecodeV1([]byte(`{"schema_version":"x","files":[],"fixture_ids":[],"vector_ids":[],"total_vector_count":0,"target_tests":[],"future":true}`), &manifest); err == nil {
		t.Fatal("manifest 未拒绝未知字段")
	}
}

func TestApprovalContinuationCrossObjectEvidenceV1DigestEncoding(t *testing.T) {
	fixture := onlyCrossObjectFixtureV1(t)
	for _, digest := range []string{fixture.StoredApprovalDigest, fixture.StoredDecisionDigest, fixture.StoredToolPinDigest, fixture.ParentReceipt.ExpectedSnapshotDigest} {
		payload := stripSHA256V1(t, digest)
		if len(payload) != 64 || payload != strings.ToLower(payload) {
			t.Fatalf("digest payload=%q", payload)
		}
	}
	for _, invalid := range []string{"5555", "SHA256:" + strings.Repeat("a", 64), "sha512:" + strings.Repeat("a", 64), strings.Repeat("a", 64)} {
		if _, ok := strictSHA256PayloadV1(invalid); ok {
			t.Fatalf("非法 digest 被接受=%s", invalid)
		}
	}
}

func TestApprovalContinuationCrossObjectEvidenceV1LegacyAuthorityIsolation(t *testing.T) {
	fixture := onlyCrossObjectFixtureV1(t)
	got := evaluateCrossObjectCaseV1(t, fixture, []string{"context.authority.legacy_substitution"}, true)
	if !reflect.DeepEqual(got.ReasonCodes, []string{"LEGACY_AUTHORITY_FORBIDDEN"}) {
		t.Fatalf("legacy authority reason=%v", got.ReasonCodes)
	}
}

func TestApprovalContinuationCrossObjectEvidenceV1OperationalMetadataExcluded(t *testing.T) {
	fixture := onlyCrossObjectFixtureV1(t)
	before := evaluateCrossObjectCaseV1(t, fixture, nil, true)
	fixture.OperationalMetadata = crossObjectOperationalMetadataV1{TraceID: "trace-retry", Attempt: 99, ProcessorInstance: "runner-z", ReadAt: "2026-07-16T09:10:11Z"}
	fixture.TurnContext.RuntimeMetadata.TraceID = "trace-context-retry"
	fixture.TurnContext.RuntimeMetadata.Attempt = 99
	after := evaluateCrossObjectCaseV1(t, fixture, nil, true)
	if before.Decision != "valid" || after.Decision != "valid" || before.EvidenceDigest != after.EvidenceDigest || before.ContextDigest != after.ContextDigest ||
		before.ReceiptSnapshotDigest != after.ReceiptSnapshotDigest || before.ReceiptOwnerDigest != after.ReceiptOwnerDigest {
		t.Fatalf("operational metadata 影响语义 before=%+v after=%+v", before, after)
	}
}

func evaluateCrossObjectCaseV1(t *testing.T, fixture crossObjectFixtureV1, mutations []string, verifyStored bool) crossObjectEvaluationV1 {
	t.Helper()
	fixture = cloneCrossObjectFixtureV1(fixture)
	syncMutatedReceiptOwner := false
	for _, mutation := range mutations {
		applyCrossObjectMutationV1(&fixture, mutation)
		if strings.HasPrefix(mutation, "receipt.approval_") || mutation == "receipt.result.failed_without_approval" {
			syncMutatedReceiptOwner = true
		}
	}
	if syncMutatedReceiptOwner {
		syncCrossObjectReceiptOwnerV1(t, &fixture)
	}
	if fixture.FixtureID != crossObjectFixtureIDV1 || fixture.BaseTurnContextFixtureID != "turn.approval.continuation" ||
		fixture.ParentReceiptOwnerRecord.SchemaVersion != "tool_receipt_owner_record.v1" ||
		fixture.Approval.SchemaVersion != "approval_binding.v1" || fixture.Decision.SchemaVersion != "approval_decision_receipt.v1" ||
		fixture.ToolPin.SchemaVersion != "graph_tool_pin.v1" || fixture.TurnContext.Canonical.SchemaVersion != turnContextSchemaV1 {
		return invalidCrossObjectV1("SCHEMA_INVALID")
	}
	snapshot, receiptDigest, receiptReason := buildCrossObjectReceiptV1(t, fixture)
	if receiptReason != "" {
		return invalidCrossObjectV1("PARENT_RECEIPT_INVALID")
	}
	receiptOwnerDigest, err := digestValueV1(receiptOwnerDigestDomainV1, fixture.ParentReceiptOwnerRecord)
	if err != nil || receiptOwnerDigest != fixture.StoredReceiptOwnerDigest ||
		fixture.ParentReceiptOwnerRecord.SchemaVersion != "tool_receipt_owner_record.v1" ||
		fixture.ParentReceiptOwnerRecord.Owner != "agent.tool_receipt" ||
		fixture.ParentReceiptOwnerRecord.Ref != "tool-receipt:"+snapshot.ReceiptID+"@v1" ||
		fixture.ParentReceiptOwnerRecord.ReceiptID != snapshot.ReceiptID ||
		fixture.ParentReceiptOwnerRecord.ReceiptVersion != snapshot.ReceiptVersion ||
		fixture.ParentReceiptOwnerRecord.WriteState != snapshot.WriteState ||
		fixture.ParentReceiptOwnerRecord.SnapshotDigest != receiptDigest {
		return invalidCrossObjectV1("PARENT_RECEIPT_INVALID")
	}
	canonical := fixture.TurnContext.Canonical
	if snapshot.SessionID != fixture.Approval.SessionID || snapshot.ReceiptID != fixture.Approval.RootToolReceiptID ||
		snapshot.TurnID != fixture.Approval.OriginalTurnID || snapshot.RunID != fixture.Approval.OriginalRunID || snapshot.ToolCallID != fixture.Approval.OriginalToolCallID ||
		canonical.SessionID != snapshot.SessionID {
		return invalidCrossObjectV1("IDENTITY_MISMATCH")
	}
	if snapshot.RequestSemanticDigest != fixture.Approval.ParentRequestSemanticDigest || canonical.ParentRequestSemanticDigest == nil ||
		*canonical.ParentRequestSemanticDigest != stripSHA256V1(t, snapshot.RequestSemanticDigest) {
		return invalidCrossObjectV1("PARENT_REQUEST_DIGEST_MISMATCH")
	}
	approvalDigest, err := digestValueV1(approvalDigestDomainV1, fixture.Approval)
	if err != nil || approvalDigest != fixture.StoredApprovalDigest {
		return invalidCrossObjectV1("APPROVAL_BINDING_MISMATCH")
	}
	if fixture.Approval.CreationState != "pending" {
		return invalidCrossObjectV1("APPROVAL_STATE_INVALID")
	}
	if snapshot.Result == nil || snapshot.Result.ApprovalRef == nil || snapshot.Result.ApprovalRef.ApprovalID != fixture.Approval.ApprovalID ||
		snapshot.Result.ApprovalRef.ApprovalVersion != fixture.Approval.ApprovalVersion || snapshot.Result.ApprovalRef.ApprovalDigest != approvalDigest ||
		snapshot.Result.ApprovalRef.CardID != fixture.Approval.CardID || canonical.ApprovalRef == nil || *canonical.ApprovalRef != "approval:"+fixture.Approval.ApprovalID+"@v1" ||
		canonical.ApprovalDigest == nil || *canonical.ApprovalDigest != stripSHA256V1(t, approvalDigest) {
		return invalidCrossObjectV1("APPROVAL_BINDING_MISMATCH")
	}
	decisionDigest, err := digestValueV1(decisionDigestDomainV1, fixture.Decision)
	if err != nil || decisionDigest != fixture.StoredDecisionDigest || fixture.Decision.ApprovalID != fixture.Approval.ApprovalID ||
		fixture.Decision.PresentedApprovalVersion != fixture.Approval.ApprovalVersion || fixture.Decision.ResultingApprovalVersion != fixture.Approval.ApprovalVersion+1 ||
		fixture.Decision.Action != "approve" || fixture.Decision.ResultingState != "approved" || fixture.Decision.ActorUserID != fixture.Approval.UserID ||
		fixture.Decision.ActorProjectID != fixture.Approval.ProjectID || fixture.Decision.CardID != fixture.Approval.CardID ||
		fixture.Decision.CardRevision != fixture.Approval.CardRevision || fixture.Decision.ApprovalBindingDigest != approvalDigest {
		return invalidCrossObjectV1("DECISION_BINDING_MISMATCH")
	}
	if canonical.AuthorityOwner == "agent.legacy_authority" || canonical.AuthorityRefType == "legacy_ensure_receipt_attestation" {
		return invalidCrossObjectV1("LEGACY_AUTHORITY_FORBIDDEN")
	}
	if canonical.InputID != fixture.Decision.ContinuationInputID || canonical.InputSourceDigest != stripSHA256V1(t, decisionDigest) ||
		canonical.AuthorityOwner != "agent.approval_store" || canonical.AuthorityRefType != "approval_decision" ||
		canonical.AuthorityRefID != fixture.Decision.DecisionReceiptID || canonical.AuthorityRefDigest != stripSHA256V1(t, decisionDigest) ||
		fixture.Decision.ContinuationSourceID != "approval-decision:"+fixture.Approval.ApprovalID+":"+fixture.Decision.DecisionID {
		return invalidCrossObjectV1("CONTINUATION_SOURCE_MISMATCH")
	}
	toolPinDigest, err := digestValueV1(toolPinDigestDomainV1, fixture.ToolPin)
	if err != nil || !crossObjectToolPinRegistryValidV1(fixture.ToolPin) || toolPinDigest != fixture.StoredToolPinDigest || fixture.Approval.ToolPinOwner != fixture.ToolPin.Owner ||
		fixture.Approval.ToolPinRef != fixture.ToolPin.Ref || fixture.Approval.ToolPinDigest != toolPinDigest || snapshot.ToolKey != fixture.ToolPin.ToolKey ||
		snapshot.DefinitionVersion != fixture.ToolPin.DefinitionVersion || snapshot.IntentSchemaVersion != fixture.ToolPin.IntentSchemaVersion ||
		snapshot.ResultSchemaVersion != fixture.ToolPin.ResultSchemaVersion || canonical.PinnedToolRef == nil || *canonical.PinnedToolRef != fixture.ToolPin.Ref ||
		canonical.PinnedToolDigest == nil || *canonical.PinnedToolDigest != stripSHA256V1(t, toolPinDigest) {
		return invalidCrossObjectV1("TOOL_PIN_BINDING_MISMATCH")
	}
	if canonical.ParentToolReceiptRef == nil || *canonical.ParentToolReceiptRef != fixture.ParentReceiptOwnerRecord.Ref ||
		canonical.ParentToolReceiptDigest == nil || *canonical.ParentToolReceiptDigest != stripSHA256V1(t, receiptOwnerDigest) {
		return invalidCrossObjectV1("PARENT_RECEIPT_INVALID")
	}
	if !crossObjectResolvedRefsValidV1(fixture, receiptOwnerDigest, approvalDigest, decisionDigest, toolPinDigest) {
		return invalidCrossObjectV1("RESOLVED_REF_EXACT_SET_MISMATCH")
	}
	messageSets := loadMessageSetFixtureMapV1(t)
	contextEvaluation := evaluateTurnContextFixtureV1(fixture.TurnContext, messageSets, verifyStored)
	if contextEvaluation.Decision != "valid" {
		return invalidCrossObjectV1("TURN_CONTEXT_INVALID")
	}
	evidenceProjection := struct {
		SchemaVersion         string `json:"schema_version"`
		FixtureID             string `json:"fixture_id"`
		ReceiptSnapshotDigest string `json:"receipt_snapshot_digest"`
		ReceiptOwnerDigest    string `json:"receipt_owner_digest"`
		ApprovalDigest        string `json:"approval_digest"`
		DecisionDigest        string `json:"decision_digest"`
		ToolPinDigest         string `json:"tool_pin_digest"`
		ContextDigest         string `json:"context_digest"`
	}{"approval_continuation_cross_object_evidence.v1", fixture.FixtureID, receiptDigest, receiptOwnerDigest, approvalDigest, decisionDigest, toolPinDigest, "sha256:" + contextEvaluation.ContextDigest}
	evidenceDigest, err := digestValueV1(crossObjectDigestDomainV1, evidenceProjection)
	if err != nil || (verifyStored && evidenceDigest != fixture.StoredEvidenceDigest) {
		return invalidCrossObjectV1("TURN_CONTEXT_INVALID")
	}
	return crossObjectEvaluationV1{Decision: "valid", ReasonCodes: []string{}, EvidenceDigest: evidenceDigest, ContextDigest: contextEvaluation.ContextDigest, ReceiptSnapshotDigest: receiptDigest, ReceiptOwnerDigest: receiptOwnerDigest}
}

func syncCrossObjectReceiptOwnerV1(t *testing.T, fixture *crossObjectFixtureV1) {
	t.Helper()
	snapshot, snapshotDigest, reason := buildCrossObjectReceiptV1(t, *fixture)
	if reason != "" {
		t.Fatalf("同步 mutation Receipt Owner 失败: %s", reason)
	}
	fixture.ParentReceiptOwnerRecord.ReceiptID = snapshot.ReceiptID
	fixture.ParentReceiptOwnerRecord.ReceiptVersion = snapshot.ReceiptVersion
	fixture.ParentReceiptOwnerRecord.WriteState = snapshot.WriteState
	fixture.ParentReceiptOwnerRecord.SnapshotDigest = snapshotDigest
	ownerDigest, err := digestValueV1(receiptOwnerDigestDomainV1, fixture.ParentReceiptOwnerRecord)
	if err != nil {
		t.Fatal(err)
	}
	fixture.StoredReceiptOwnerDigest = ownerDigest
}

func crossObjectToolPinRegistryValidV1(pin crossObjectToolPinV1) bool {
	return pin.SchemaVersion == "graph_tool_pin.v1" && pin.Owner == "agent.tool_registry" &&
		pin.Ref == "tool-pin:plan_creation_spec.v1alpha1@v1" && pin.ToolKey == "plan_creation_spec" &&
		pin.DefinitionVersion == "plan_creation_spec.v1alpha1" && pin.IntentSchemaVersion == "plan_creation_spec_intent.v1" &&
		pin.ResultSchemaVersion == "graph_tool_result.v1" && pin.GraphKey == "plan_creation_spec_graph_v1"
}

func buildCrossObjectReceiptV1(t *testing.T, fixture crossObjectFixtureV1) (approvalContinuationParentReceiptProjectionV1, string, string) {
	t.Helper()
	projection, reason := evaluateApprovalContinuationParentReceiptV1(t, fixture.ParentReceipt)
	if reason != "" {
		return approvalContinuationParentReceiptProjectionV1{}, "", reason
	}
	return projection, projection.SnapshotDigest, ""
}

func crossObjectResolvedRefsValidV1(fixture crossObjectFixtureV1, receiptOwnerDigest, approvalDigest, decisionDigest, toolPinDigest string) bool {
	want := append([]turnContextResolvedRefV1(nil), fixture.TurnContext.ResolvedRefs...)
	expected, reason := turnContextFrozenTripletsV1(fixture.TurnContext.Canonical)
	if reason != "" || !turnContextResolvedRefsValidV1(expected, want) {
		return false
	}
	receiptOwnerPayload, receiptOwnerOK := strictSHA256PayloadV1(receiptOwnerDigest)
	approvalPayload, approvalOK := strictSHA256PayloadV1(approvalDigest)
	decisionPayload, decisionOK := strictSHA256PayloadV1(decisionDigest)
	toolPinPayload, toolPinOK := strictSHA256PayloadV1(toolPinDigest)
	if !receiptOwnerOK || !approvalOK || !decisionOK || !toolPinOK {
		return false
	}
	// 先由现有 Turn Context evaluator 验证完整 frozen triplet exact-set；这里再逐值要求四个 R03 Owner record。
	required := []turnContextResolvedRefV1{
		{Owner: "agent.tool_receipt", Ref: fixture.ParentReceiptOwnerRecord.Ref, Digest: receiptOwnerPayload},
		{Owner: "agent.approval_store", Ref: "approval:" + fixture.Approval.ApprovalID + "@v1", Digest: approvalPayload},
		{Owner: "agent.approval_store", Ref: fixture.Decision.DecisionReceiptID, Digest: decisionPayload},
		{Owner: fixture.ToolPin.Owner, Ref: fixture.ToolPin.Ref, Digest: toolPinPayload},
	}
	for _, item := range required {
		count := 0
		for _, actual := range want {
			if actual == item {
				count++
			}
		}
		if count != 1 {
			return false
		}
	}
	return true
}

func applyCrossObjectMutationV1(fixture *crossObjectFixtureV1, mutation string) {
	switch mutation {
	case "context.schema.unknown":
		fixture.TurnContext.Canonical.SchemaVersion = "session_turn_context.v2"
	case "receipt.schema.unknown":
		fixture.ParentReceipt.BaseSnapshot.SchemaVersion = "tool_receipt.v2"
	case "context.parent_receipt_ref.mismatch":
		value := "tool-receipt:019f4500-0000-7000-8000-000000000799@v1"
		fixture.TurnContext.Canonical.ParentToolReceiptRef = &value
	case "context.parent_receipt_digest.mismatch":
		value := strings.Repeat("1", 64)
		fixture.TurnContext.Canonical.ParentToolReceiptDigest = &value
	case "context.parent_request_digest.mismatch":
		value := strings.Repeat("2", 64)
		fixture.TurnContext.Canonical.ParentRequestSemanticDigest = &value
	case "receipt.approval_id.mismatch":
		approvalID := "019f4500-0000-7000-8000-000000000899"
		mutateApprovalContinuationResultApprovalRefV1(&fixture.ParentReceipt.Result, func(approval *approvalContinuationApprovalRefV1) {
			approval.ApprovalID = approvalID
		})
		fixture.ParentReceipt.SetupSlots[0].AuthorityRef.AuthorityID = approvalID
	case "receipt.approval_version.mismatch":
		var approvalVersion int64
		mutateApprovalContinuationResultApprovalRefV1(&fixture.ParentReceipt.Result, func(approval *approvalContinuationApprovalRefV1) {
			approval.ApprovalVersion++
			approvalVersion = approval.ApprovalVersion
		})
		fixture.ParentReceipt.SetupSlots[0].AuthorityRef.AuthorityVersion = approvalVersion
	case "receipt.approval_digest.mismatch":
		approvalDigest := "sha256:" + strings.Repeat("3", 64)
		mutateApprovalContinuationResultApprovalRefV1(&fixture.ParentReceipt.Result, func(approval *approvalContinuationApprovalRefV1) {
			approval.ApprovalDigest = approvalDigest
		})
		fixture.ParentReceipt.SetupSlots[0].AuthorityRef.AuthoritySemanticDigest = approvalDigest
	case "receipt.result.failed_without_approval":
		fixture.ParentReceipt.SetupSlots = []approvalContinuationResolvedSlotV1{}
		fixture.ParentReceipt.ResultRefSlots = []string{}
		fixture.ParentReceipt.Result = json.RawMessage(fmt.Sprintf(`{
			"schema_version":"graph_tool_result.v1",
			"status":"failed",
			"result_code":"DEPENDENCY_UNAVAILABLE",
			"summary":"依赖暂时不可用，且尚未发生副作用。",
			"resource_refs":[],
			"receipt_ref":{"receipt_id":%q},
			"warnings":[],
			"retryable":true
		}`, fixture.ParentReceipt.BaseSnapshot.ReceiptID))
	case "approval.origin_turn_id.mismatch":
		fixture.Approval.OriginalTurnID = "019f4500-0000-7000-8000-000000000798"
	case "approval.creation_state.approved":
		fixture.Approval.CreationState = "approved"
		fixture.StoredApprovalDigest, _ = digestValueV1(approvalDigestDomainV1, fixture.Approval)
	case "decision.approval_id.mismatch":
		fixture.Decision.ApprovalID = "019f4500-0000-7000-8000-000000000898"
		fixture.StoredDecisionDigest, _ = digestValueV1(decisionDigestDomainV1, fixture.Decision)
	case "decision.digest.mismatch":
		fixture.StoredDecisionDigest = "sha256:" + strings.Repeat("4", 64)
	case "decision.continuation_source.mismatch":
		fixture.Decision.ContinuationSourceID += ":other"
		fixture.StoredDecisionDigest, _ = digestValueV1(decisionDigestDomainV1, fixture.Decision)
	case "context.approval_ref.mismatch":
		value := "approval:019f4500-0000-7000-8000-000000000898@v1"
		fixture.TurnContext.Canonical.ApprovalRef = &value
	case "context.tool_pin_ref.mismatch":
		value := "tool-pin:plan_creation_spec@v2"
		fixture.TurnContext.Canonical.PinnedToolRef = &value
	case "tool_pin.definition_version.drift":
		fixture.ToolPin.DefinitionVersion = "plan_creation_spec.v1alpha2"
		fixture.StoredToolPinDigest, _ = digestValueV1(toolPinDigestDomainV1, fixture.ToolPin)
	case "tool_pin.tool_key.drift":
		fixture.ToolPin.ToolKey = "plan_storyboard"
		fixture.StoredToolPinDigest, _ = digestValueV1(toolPinDigestDomainV1, fixture.ToolPin)
	case "tool_pin.intent_schema_version.drift":
		fixture.ToolPin.IntentSchemaVersion = "plan_creation_spec_intent.v2"
		fixture.StoredToolPinDigest, _ = digestValueV1(toolPinDigestDomainV1, fixture.ToolPin)
	case "tool_pin.result_schema_version.drift":
		fixture.ToolPin.ResultSchemaVersion = "graph_tool_result.v2"
		fixture.StoredToolPinDigest, _ = digestValueV1(toolPinDigestDomainV1, fixture.ToolPin)
	case "tool_pin.graph_key.drift":
		fixture.ToolPin.GraphKey = "plan_creation_spec_graph_v2"
		fixture.StoredToolPinDigest, _ = digestValueV1(toolPinDigestDomainV1, fixture.ToolPin)
	case "context.resolved_refs.extra":
		fixture.TurnContext.ResolvedRefs = append(fixture.TurnContext.ResolvedRefs, turnContextResolvedRefV1{Owner: "agent.tool_registry", Ref: "tool-pin:extra@v1", Digest: strings.Repeat("5", 64)})
	case "context.stored_digest.mismatch":
		fixture.TurnContext.StoredContextDigest = strings.Repeat("6", 64)
	case "context.authority.legacy_substitution":
		fixture.TurnContext.Canonical.AuthorityOwner = "agent.legacy_authority"
		fixture.TurnContext.Canonical.AuthorityRefType = "legacy_ensure_receipt_attestation"
		fixture.TurnContext.InputBinding.AuthorityOwner = "agent.legacy_authority"
		fixture.TurnContext.InputBinding.AuthorityRefType = "legacy_ensure_receipt_attestation"
	case "context.authority_id.mismatch":
		fixture.TurnContext.Canonical.AuthorityRefID = "019f4500-0000-7000-8000-000000000497"
	case "receipt.session_id.mismatch":
		fixture.ParentReceipt.BaseSnapshot.SessionID = "019f4500-0000-7000-8000-000000000997"
	default:
		panic("未知 cross-object mutation: " + mutation)
	}
}

func invalidCrossObjectV1(reason string) crossObjectEvaluationV1 {
	return crossObjectEvaluationV1{Decision: "invalid", ReasonCodes: []string{reason}}
}

func digestValueV1(domain string, value any) (string, error) {
	raw, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	return semanticDigest(domain, raw), nil
}

func assertStoredDigestV1(t *testing.T, domain string, value any, stored string) {
	t.Helper()
	got, err := digestValueV1(domain, value)
	if err != nil || got != stored {
		t.Fatalf("domain=%s digest=%s want=%s err=%v", domain, got, stored, err)
	}
}

func strictSHA256PayloadV1(value string) (string, bool) {
	if !digestPattern.MatchString(value) || !strings.HasPrefix(value, "sha256:") {
		return "", false
	}
	return value[len("sha256:"):], true
}

func stripSHA256V1(t *testing.T, value string) string {
	t.Helper()
	payload, ok := strictSHA256PayloadV1(value)
	if !ok {
		t.Fatalf("非法 sha256 digest=%q", value)
	}
	return payload
}

func cloneCrossObjectFixtureV1(fixture crossObjectFixtureV1) crossObjectFixtureV1 {
	raw, err := canonicalJSON(fixture)
	if err != nil {
		panic(err)
	}
	var cloned crossObjectFixtureV1
	if err := messageSetStrictDecodeV1(raw, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func loadCrossObjectCorpusV1(t *testing.T) crossObjectCorpusV1 {
	t.Helper()
	raw, err := os.ReadFile(crossObjectCorpusPathV1)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("R03 corpus JSON 非法: %v", err)
	}
	var corpus crossObjectCorpusV1
	if err := messageSetStrictDecodeV1(raw, &corpus); err != nil {
		t.Fatalf("解析 R03 corpus: %v", err)
	}
	if corpus.SchemaVersion != "approval_continuation_cross_object_evidence_v1_corpus.v1" || len(corpus.Fixtures) != 1 || len(corpus.Cases) != crossObjectVectorCountV1 {
		t.Fatalf("R03 corpus 版本或数量不符 version=%s fixtures=%d cases=%d", corpus.SchemaVersion, len(corpus.Fixtures), len(corpus.Cases))
	}
	for index := range corpus.Fixtures {
		hydrateCrossObjectTurnContextV1(t, &corpus.Fixtures[index])
	}
	return corpus
}

func hydrateCrossObjectTurnContextV1(t *testing.T, fixture *crossObjectFixtureV1) {
	t.Helper()
	baseCorpus := loadTurnContextCorpusV1(t)
	var base turnContextFixtureV1
	found := false
	for _, candidate := range baseCorpus.Fixtures {
		if candidate.FixtureID == fixture.BaseTurnContextFixtureID {
			base = cloneTurnContextFixtureV1(candidate)
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("R03 base Turn Context fixture 不存在=%s", fixture.BaseTurnContextFixtureID)
	}
	storedContextDigest := fixture.TurnContext.StoredContextDigest
	canonical := &base.Canonical
	canonical.InputID = fixture.Decision.ContinuationInputID
	canonical.InputSourceDigest = stripSHA256V1(t, fixture.StoredDecisionDigest)
	canonical.AuthorityOwner = "agent.approval_store"
	canonical.AuthorityRefType = "approval_decision"
	canonical.AuthorityRefID = fixture.Decision.DecisionReceiptID
	canonical.AuthorityRefDigest = stripSHA256V1(t, fixture.StoredDecisionDigest)
	receiptRef := fixture.ParentReceiptOwnerRecord.Ref
	receiptDigest := stripSHA256V1(t, fixture.StoredReceiptOwnerDigest)
	parentRequestDigest := stripSHA256V1(t, fixture.ParentReceipt.BaseSnapshot.RequestSemanticDigest)
	approvalRef := "approval:" + fixture.Approval.ApprovalID + "@v1"
	approvalDigest := stripSHA256V1(t, fixture.StoredApprovalDigest)
	toolPinRef := fixture.ToolPin.Ref
	toolPinDigest := stripSHA256V1(t, fixture.StoredToolPinDigest)
	canonical.ParentToolReceiptRef = &receiptRef
	canonical.ParentToolReceiptDigest = &receiptDigest
	canonical.ParentRequestSemanticDigest = &parentRequestDigest
	canonical.ApprovalRef = &approvalRef
	canonical.ApprovalDigest = &approvalDigest
	canonical.PinnedToolRef = &toolPinRef
	canonical.PinnedToolDigest = &toolPinDigest
	base.InputBinding.InputID = canonical.InputID
	base.InputBinding.InputSourceDigest = canonical.InputSourceDigest
	base.InputBinding.AuthorityOwner = canonical.AuthorityOwner
	base.InputBinding.AuthorityRefType = canonical.AuthorityRefType
	base.InputBinding.AuthorityRefID = canonical.AuthorityRefID
	base.InputBinding.AuthorityRefDigest = canonical.AuthorityRefDigest
	triplets, reason := turnContextFrozenTripletsV1(*canonical)
	if reason != "" {
		t.Fatalf("R03 Context frozen triplets=%s", reason)
	}
	base.ResolvedRefs = triplets
	base.StoredContextDigest = storedContextDigest
	fixture.TurnContext = base
}

func loadCrossObjectManifestV1(t *testing.T) crossObjectManifestV1 {
	t.Helper()
	raw, err := os.ReadFile(crossObjectManifestPathV1)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("R03 manifest JSON 非法: %v", err)
	}
	var manifest crossObjectManifestV1
	if err := messageSetStrictDecodeV1(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func onlyCrossObjectFixtureV1(t *testing.T) crossObjectFixtureV1 {
	t.Helper()
	corpus := loadCrossObjectCorpusV1(t)
	return corpus.Fixtures[0]
}

func crossObjectVectorIDsV1() []string {
	return []string{
		"COE-CAN-001-approval-continuation-golden", "COE-CAN-002-replay-stable", "COE-CAN-003-operational-metadata-excluded",
		"COE-N01-parent-receipt-ref-mismatch", "COE-N02-parent-receipt-digest-mismatch", "COE-N03-parent-request-digest-mismatch",
		"COE-N04-receipt-approval-id-mismatch", "COE-N05-receipt-approval-version-mismatch", "COE-N06-receipt-approval-digest-mismatch",
		"COE-N07-approval-origin-identity-mismatch", "COE-N08-approval-binding-state-not-pending", "COE-N09-decision-authority-id-mismatch",
		"COE-N10-decision-authority-digest-mismatch", "COE-N11-continuation-source-mismatch", "COE-N12-context-approval-ref-mismatch",
		"COE-N13-tool-pin-ref-digest-mismatch", "COE-N14-tool-pin-field-drift", "COE-N15-resolved-ref-exact-set-mismatch",
		"COE-N16-legacy-authority-substitution", "COE-N17-multi-error-priority",
	}
}

func crossObjectTargetTestNamesV1(t *testing.T) []string {
	t.Helper()
	directory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, "approval_continuation_cross_object_evidence_v1_corpus_test.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && strings.HasPrefix(function.Name.Name, "Test") {
			names = append(names, function.Name.Name)
		}
	}
	sort.Strings(names)
	return names
}
