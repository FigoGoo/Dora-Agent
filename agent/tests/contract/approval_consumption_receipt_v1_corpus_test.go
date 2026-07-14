// Package contract_test 只承载未 Approved 的 Approval Consumption Receipt Core 候选语料，
// 不提供生产 Approval Store、Migration、Repository、Runner、child Receipt 或 Business RPC。
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
	"time"
)

const (
	approvalConsumptionCorpusPathV1   = "testdata/w2_r04_approval_consumption/approval_consumption_receipt_core_v1.json"
	approvalConsumptionManifestPathV1 = "testdata/w2_r04_approval_consumption/manifest.json"
	approvalConsumptionVectorCountV1  = 111
	consumptionIntentDigestDomainV1   = "dora.approval_consumption_intent_binding.v1"
	consumptionCoreDigestDomainV1     = "dora.approval_consumption_receipt_core.v1"
	trustedConsumptionPrincipalV1     = "agent.approval_consumption_service"
)

// approvalConsumptionManifestV1 固定 R04 文件摘要、fixture/vector exact-set 和目标测试集合。
type approvalConsumptionManifestV1 struct {
	SchemaVersion    string                     `json:"schema_version"`
	Files            []messageSetManifestFileV1 `json:"files"`
	FixtureIDs       []string                   `json:"fixture_ids"`
	VectorIDs        []string                   `json:"vector_ids"`
	TotalVectorCount int                        `json:"total_vector_count"`
	TargetTests      []string                   `json:"target_tests"`
}

// approvalConsumptionCorpusV1 是 unsigned core 的测试专用机器真源。
type approvalConsumptionCorpusV1 struct {
	SchemaVersion string                                   `json:"schema_version"`
	ExactSets     approvalConsumptionExactSetsV1           `json:"exact_sets"`
	Fixtures      []approvalConsumptionFixtureDefinitionV1 `json:"fixtures"`
	GoldenDigests []approvalConsumptionGoldenDigestV1      `json:"golden_digests"`
	Cases         []approvalConsumptionCaseV1              `json:"cases"`
}

type approvalConsumptionExactSetsV1 struct {
	Decisions           []string `json:"decisions"`
	ReasonCodes         []string `json:"reason_codes"`
	Commands            []string `json:"commands"`
	ApprovalStates      []string `json:"approval_states"`
	Scopes              []string `json:"scopes"`
	Actions             []string `json:"actions"`
	Effects             []string `json:"effects"`
	ResourceTypes       []string `json:"resource_types"`
	ConsumptionPolicies []string `json:"consumption_policies"`
	EvidenceKinds       []string `json:"evidence_kinds"`
}

// approvalConsumptionFixtureDefinitionV1 允许由唯一完整 golden 派生重放、漂移与拒绝 fixture。
type approvalConsumptionFixtureDefinitionV1 struct {
	FixtureID      string                        `json:"fixture_id"`
	BaseFixtureID  string                        `json:"base_fixture_id"`
	SetupMutations []string                      `json:"setup_mutations"`
	Value          *approvalConsumptionFixtureV1 `json:"value,omitempty"`
}

type approvalConsumptionGoldenDigestV1 struct {
	FixtureID     string `json:"fixture_id"`
	RequestDigest string `json:"request_digest"`
	CoreDigest    string `json:"core_digest"`
}

// approvalConsumptionFixtureV1 聚合纯状态机所需的受信 authority、命令、当前事实和存量 core。
type approvalConsumptionFixtureV1 struct {
	SchemaVersion          string                                    `json:"schema_version"`
	DecisionAuthority      approvalConsumptionDecisionAuthorityV1    `json:"decision_authority"`
	RecordCommand          approvalConsumptionRecordCommandV1        `json:"record_command"`
	FirstWriteMaterial     approvalConsumptionFirstWriteMaterialV1   `json:"first_write_material"`
	QueryCommand           approvalConsumptionQueryCommandV1         `json:"query_command"`
	QueryExpectedBinding   approvalConsumptionQueryExpectedBindingV1 `json:"query_expected_binding"`
	CurrentEligibility     approvalConsumptionCurrentEligibilityV1   `json:"current_eligibility"`
	StoredConsumptionIndex []approvalConsumptionStoredIndexV1        `json:"stored_consumption_index"`
	StoredCores            []approvalConsumptionStoredCoreV1         `json:"stored_cores"`
	OperationalMetadata    approvalConsumptionOperationalMetadataV1  `json:"operational_metadata"`
}

type approvalConsumptionIdentityV1 struct {
	SchemaVersion  string `json:"schema_version"`
	AgentPrincipal string `json:"agent_principal"`
	UserID         string `json:"user_id"`
	ProjectID      string `json:"project_id"`
	SessionID      string `json:"session_id"`
}

type approvalConsumptionDecisionAuthorityV1 struct {
	SchemaVersion string                             `json:"schema_version"`
	Owner         string                             `json:"owner"`
	ApprovalState string                             `json:"approval_state"`
	FrozenIntent  approvalConsumptionIntentBindingV1 `json:"frozen_intent"`
}

// approvalConsumptionIntentBindingV1 是 immutable intent；实时 observation 不属于该摘要。
type approvalConsumptionIntentBindingV1 struct {
	SchemaVersion                string                           `json:"schema_version"`
	ApprovalID                   string                           `json:"approval_id"`
	PresentedApprovalVersion     int64                            `json:"presented_approval_version"`
	ResultingApprovalVersion     int64                            `json:"resulting_approval_version"`
	ApprovalExpiresAt            string                           `json:"approval_expires_at"`
	DecisionReceiptID            string                           `json:"decision_receipt_id"`
	DecisionID                   string                           `json:"decision_id"`
	DecisionDigest               string                           `json:"decision_digest"`
	ContinuationSourceID         string                           `json:"continuation_source_id"`
	DecisionAction               string                           `json:"decision_action"`
	ResultingState               string                           `json:"resulting_state"`
	UserID                       string                           `json:"user_id"`
	ProjectID                    string                           `json:"project_id"`
	SessionID                    string                           `json:"session_id"`
	RootToolReceiptID            string                           `json:"root_tool_receipt_id"`
	RootToolReceiptVersion       int64                            `json:"root_tool_receipt_version"`
	RootTurnID                   string                           `json:"root_turn_id"`
	RootRunID                    string                           `json:"root_run_id"`
	RootRequestSemanticDigest    string                           `json:"root_request_semantic_digest"`
	ParentToolReceiptID          string                           `json:"parent_tool_receipt_id"`
	ParentToolReceiptVersion     int64                            `json:"parent_tool_receipt_version"`
	ParentTurnID                 string                           `json:"parent_turn_id"`
	ParentRunID                  string                           `json:"parent_run_id"`
	ParentRequestSemanticDigest  string                           `json:"parent_request_semantic_digest"`
	ParentResultStatus           string                           `json:"parent_result_status"`
	ParentResultDigest           string                           `json:"parent_result_digest"`
	OriginalToolCallID           string                           `json:"original_tool_call_id"`
	ParentToolReceiptOwnerRef    string                           `json:"parent_tool_receipt_owner_ref"`
	ParentToolReceiptOwnerDigest string                           `json:"parent_tool_receipt_owner_digest"`
	ContinuationInputID          string                           `json:"continuation_input_id"`
	ContinuationTurnID           string                           `json:"continuation_turn_id"`
	ContinuationRunID            string                           `json:"continuation_run_id"`
	ChildToolReceiptID           string                           `json:"child_tool_receipt_id"`
	Scope                        string                           `json:"scope"`
	Action                       string                           `json:"action"`
	ConsumptionPolicy            string                           `json:"consumption_policy"`
	EffectKind                   string                           `json:"effect_kind"`
	ConsumptionKey               string                           `json:"consumption_key"`
	ToolPinOwner                 string                           `json:"tool_pin_owner"`
	ToolPinRef                   string                           `json:"tool_pin_ref"`
	ToolPinDigest                string                           `json:"tool_pin_digest"`
	ToolKey                      string                           `json:"tool_key"`
	DefinitionVersion            string                           `json:"definition_version"`
	IntentSchemaVersion          string                           `json:"intent_schema_version"`
	ResultSchemaVersion          string                           `json:"result_schema_version"`
	GraphKey                     string                           `json:"graph_key"`
	IntentDigest                 string                           `json:"intent_digest"`
	ResourceType                 string                           `json:"resource_type"`
	ResourceID                   string                           `json:"resource_id"`
	ResourceVersion              int64                            `json:"resource_version"`
	ResourceDigest               string                           `json:"resource_digest"`
	TargetExactSetDigest         string                           `json:"target_exact_set_digest"`
	ExpectedAuthorizationScope   approvalConsumptionEvidenceRefV1 `json:"expected_authorization_scope"`
	ExpectedResourceSnapshot     approvalConsumptionEvidenceRefV1 `json:"expected_resource_snapshot"`
	ExpectedPolicySnapshot       approvalConsumptionEvidenceRefV1 `json:"expected_policy_snapshot"`
}

type approvalConsumptionEvidenceRefV1 struct {
	Kind    string `json:"kind"`
	Owner   string `json:"owner"`
	Ref     string `json:"ref"`
	Version int64  `json:"version"`
	Digest  string `json:"digest"`
}

// approvalConsumptionRevalidationObservationV1 只在首写时冻结并进入 core digest。
type approvalConsumptionRevalidationObservationV1 struct {
	SchemaVersion string                             `json:"schema_version"`
	RecordedAt    string                             `json:"recorded_at"`
	Evidence      []approvalConsumptionEvidenceRefV1 `json:"evidence"`
}

type approvalConsumptionRecordCommandV1 struct {
	SchemaVersion         string                             `json:"schema_version"`
	Command               string                             `json:"command"`
	AuthenticatedIdentity approvalConsumptionIdentityV1      `json:"authenticated_identity"`
	IntentBinding         approvalConsumptionIntentBindingV1 `json:"intent_binding"`
	RequestDigest         string                             `json:"request_digest"`
}

// approvalConsumptionFirstWriteMaterialV1 是服务端生成输入，不属于 Record command JSON。
type approvalConsumptionFirstWriteMaterialV1 struct {
	ReceiptID               string                                       `json:"receipt_id"`
	RevalidationObservation approvalConsumptionRevalidationObservationV1 `json:"revalidation_observation"`
}

type approvalConsumptionQueryCommandV1 struct {
	SchemaVersion         string                        `json:"schema_version"`
	Command               string                        `json:"command"`
	AuthenticatedIdentity approvalConsumptionIdentityV1 `json:"authenticated_identity"`
	ApprovalID            string                        `json:"approval_id"`
	ConsumptionKey        string                        `json:"consumption_key"`
}

// approvalConsumptionQueryExpectedBindingV1 是 Query 返回后的本地纯校验输入，不属于 Query command。
type approvalConsumptionQueryExpectedBindingV1 struct {
	SchemaVersion      string `json:"schema_version"`
	RequestDigest      string `json:"request_digest"`
	UserID             string `json:"user_id"`
	ProjectID          string `json:"project_id"`
	SessionID          string `json:"session_id"`
	ApprovalID         string `json:"approval_id"`
	EffectKind         string `json:"effect_kind"`
	ChildToolReceiptID string `json:"child_tool_receipt_id"`
}

type approvalConsumptionCurrentEligibilityV1 struct {
	SchemaVersion        string                           `json:"schema_version"`
	AuthorizationScope   approvalConsumptionEvidenceRefV1 `json:"authorization_scope"`
	ResourceSnapshot     approvalConsumptionEvidenceRefV1 `json:"resource_snapshot"`
	PolicySnapshot       approvalConsumptionEvidenceRefV1 `json:"policy_snapshot"`
	ResourceType         string                           `json:"resource_type"`
	ResourceID           string                           `json:"resource_id"`
	ResourceVersion      int64                            `json:"resource_version"`
	ResourceDigest       string                           `json:"resource_digest"`
	TargetExactSetDigest string                           `json:"target_exact_set_digest"`
}

// approvalConsumptionReceiptCoreV1 是 unsigned semantic core；不含 signature 或 self digest。
type approvalConsumptionReceiptCoreV1 struct {
	SchemaVersion           string                                       `json:"schema_version"`
	ReceiptID               string                                       `json:"receipt_id"`
	ReceiptVersion          int64                                        `json:"receipt_version"`
	WriteState              string                                       `json:"write_state"`
	IntentBinding           approvalConsumptionIntentBindingV1           `json:"intent_binding"`
	RequestDigest           string                                       `json:"request_digest"`
	RevalidationObservation approvalConsumptionRevalidationObservationV1 `json:"revalidation_observation"`
}

type approvalConsumptionStoredCoreV1 struct {
	Core       approvalConsumptionReceiptCoreV1 `json:"core"`
	CoreDigest string                           `json:"core_digest"`
}

// approvalConsumptionStoredIndexV1 模拟独立唯一索引；other-key guard 不伪造损坏 core。
type approvalConsumptionStoredIndexV1 struct {
	SchemaVersion  string `json:"schema_version"`
	Owner          string `json:"owner"`
	ApprovalID     string `json:"approval_id"`
	ConsumptionKey string `json:"consumption_key"`
	RequestDigest  string `json:"request_digest"`
	UserID         string `json:"user_id"`
	ProjectID      string `json:"project_id"`
	SessionID      string `json:"session_id"`
	EffectKind     string `json:"effect_kind"`
	CoreDigest     string `json:"core_digest"`
}

type approvalConsumptionOperationalMetadataV1 struct {
	SchemaVersion        string `json:"schema_version"`
	TraceID              string `json:"trace_id"`
	Attempt              int64  `json:"attempt"`
	ProcessorInstance    string `json:"processor_instance"`
	ReadAt               string `json:"read_at"`
	ChildWriteState      string `json:"child_write_state"`
	CurrentFence         int64  `json:"current_fence"`
	ExpectedChildVersion int64  `json:"expected_child_version"`
}

type approvalConsumptionCaseV1 struct {
	ID                      string                        `json:"id"`
	Command                 string                        `json:"command"`
	FromFixture             string                        `json:"from_fixture"`
	Mutations               []string                      `json:"mutations"`
	RawFixtureJSON          string                        `json:"raw_fixture_json"`
	RawFixtureMutation      string                        `json:"raw_fixture_mutation"`
	ValidateExpectedBinding bool                          `json:"validate_expected_binding"`
	Expected                approvalConsumptionExpectedV1 `json:"expected"`
}

type approvalConsumptionExpectedV1 struct {
	Decision    string   `json:"decision"`
	ReasonCodes []string `json:"reason_codes"`
}

type approvalConsumptionEvaluationV1 struct {
	Decision      string
	ReasonCodes   []string
	RequestDigest string
	CoreDigest    string
	Core          *approvalConsumptionReceiptCoreV1
}

func TestW2R04ApprovalConsumptionManifest(t *testing.T) {
	manifest := loadApprovalConsumptionManifestV1(t)
	wantFixtures := []string{
		"acr.creation_spec_activation.approved_recorded",
		"acr.creation_spec_activation.approved_recorded_current_drift",
		"acr.creation_spec_activation.approved_unconsumed",
		"acr.creation_spec_activation.rejected_unconsumed",
	}
	wantTests := []string{
		"TestW2R04ApprovalConsumptionManifest",
		"TestApprovalConsumptionReceiptCoreV1Corpus",
		"TestApprovalConsumptionReceiptCoreV1GoldenDigests",
		"TestApprovalConsumptionReceiptCoreV1ExactSets",
		"TestApprovalConsumptionReceiptCoreV1FirstWrite",
		"TestApprovalConsumptionReceiptCoreV1ReplayAndQuery",
		"TestApprovalConsumptionReceiptCoreV1SingleUseConflicts",
		"TestApprovalConsumptionReceiptCoreV1EligibilityAndRevalidation",
		"TestApprovalConsumptionReceiptCoreV1ReasonPriority",
		"TestApprovalConsumptionReceiptCoreV1StrictJSON",
		"TestApprovalConsumptionReceiptCoreV1OperationalMetadataExcluded",
	}
	if manifest.SchemaVersion != "w2_r04_approval_consumption_manifest.v1" || manifest.TotalVectorCount != approvalConsumptionVectorCountV1 ||
		!reflect.DeepEqual(manifest.FixtureIDs, wantFixtures) || !reflect.DeepEqual(manifest.VectorIDs, approvalConsumptionVectorIDsV1()) ||
		!reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("R04 manifest exact-set 不符: %+v", manifest)
	}
	actualTests := contractManifestTargetTestNamesV1(t, []string{"approval_consumption_receipt_v1_corpus_test.go"})
	sortedWant := append([]string(nil), wantTests...)
	sort.Strings(sortedWant)
	if !reflect.DeepEqual(actualTests, sortedWant) {
		t.Fatalf("R04 manifest target tests 未绑定 AST actual=%v want=%v", actualTests, sortedWant)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].File != "approval_consumption_receipt_core_v1.json" || manifest.Files[0].VectorCount != approvalConsumptionVectorCountV1 {
		t.Fatalf("R04 manifest files=%+v", manifest.Files)
	}
	raw, err := os.ReadFile(filepath.Join("testdata/w2_r04_approval_consumption", manifest.Files[0].File))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := "sha256:" + hex.EncodeToString(sum[:]); got != manifest.Files[0].SHA256 {
		t.Fatalf("R04 corpus sha=%s want=%s", got, manifest.Files[0].SHA256)
	}
	corpus, fixtures := loadApprovalConsumptionCorpusV1(t)
	fixtureIDs := make([]string, 0, len(fixtures))
	for id := range fixtures {
		fixtureIDs = append(fixtureIDs, id)
	}
	sort.Strings(fixtureIDs)
	caseIDs := make([]string, 0, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		caseIDs = append(caseIDs, testCase.ID)
	}
	if !reflect.DeepEqual(fixtureIDs, manifest.FixtureIDs) || !reflect.DeepEqual(caseIDs, manifest.VectorIDs) {
		t.Fatalf("R04 manifest 未绑定 corpus fixtures=%v cases=%v", fixtureIDs, caseIDs)
	}
}

func TestApprovalConsumptionReceiptCoreV1Corpus(t *testing.T) {
	corpus, fixtures := loadApprovalConsumptionCorpusV1(t)
	seen := map[string]struct{}{}
	for _, testCase := range corpus.Cases {
		testCase := testCase
		t.Run(testCase.ID, func(t *testing.T) {
			if _, duplicate := seen[testCase.ID]; duplicate {
				t.Fatalf("重复 case=%s", testCase.ID)
			}
			seen[testCase.ID] = struct{}{}
			got := evaluateApprovalConsumptionCaseV1(t, fixtures, testCase)
			if got.Decision != testCase.Expected.Decision || !reflect.DeepEqual(got.ReasonCodes, testCase.Expected.ReasonCodes) {
				t.Fatalf("evaluation=%+v want=%+v", got, testCase.Expected)
			}
		})
	}
	if len(seen) != approvalConsumptionVectorCountV1 {
		t.Fatalf("case count=%d", len(seen))
	}
}

func TestApprovalConsumptionReceiptCoreV1GoldenDigests(t *testing.T) {
	corpus, fixtures := loadApprovalConsumptionCorpusV1(t)
	if len(corpus.GoldenDigests) != len(fixtures) {
		t.Fatalf("golden count=%d fixtures=%d", len(corpus.GoldenDigests), len(fixtures))
	}
	for _, golden := range corpus.GoldenDigests {
		fixture, exists := fixtures[golden.FixtureID]
		if !exists {
			t.Fatalf("未知 golden fixture=%s", golden.FixtureID)
		}
		requestDigest, err := digestValueV1(consumptionIntentDigestDomainV1, fixture.RecordCommand.IntentBinding)
		if err != nil || requestDigest != golden.RequestDigest || fixture.RecordCommand.RequestDigest != golden.RequestDigest {
			t.Fatalf("fixture=%s request=%s command=%s want=%s err=%v", golden.FixtureID, requestDigest, fixture.RecordCommand.RequestDigest, golden.RequestDigest, err)
		}
		if golden.CoreDigest == "" {
			continue
		}
		core := buildApprovalConsumptionCoreV1(fixture.RecordCommand, fixture.FirstWriteMaterial)
		if len(fixture.StoredCores) != 0 {
			core = fixture.StoredCores[0].Core
		}
		coreDigest, digestErr := digestValueV1(consumptionCoreDigestDomainV1, core)
		if digestErr != nil || coreDigest != golden.CoreDigest {
			t.Fatalf("fixture=%s core=%s want=%s err=%v", golden.FixtureID, coreDigest, golden.CoreDigest, digestErr)
		}
	}
}

func TestApprovalConsumptionReceiptCoreV1ExactSets(t *testing.T) {
	corpus, _ := loadApprovalConsumptionCorpusV1(t)
	want := approvalConsumptionExactSetsV1{
		Decisions:   []string{"conflict", "found", "invalid", "not_found", "recorded", "replayed"},
		ReasonCodes: []string{"APPROVAL_EFFECT_NOT_ELIGIBLE", "APPROVAL_EXPIRED", "AUTHENTICATED_IDENTITY_MISMATCH", "COMMAND_INVALID", "CONSUMPTION_DIGEST_CONFLICT", "CONSUMPTION_QUERY_BINDING_CONFLICT", "IMMUTABLE_CAUSAL_BINDING_MISMATCH", "KEY_DERIVATION_MISMATCH", "NOT_FOUND", "REQUEST_DIGEST_MISMATCH", "REVALIDATE_POLICY_MISMATCH", "REVALIDATE_RESOURCE_MISMATCH", "REVALIDATE_SCOPE_MISMATCH", "REVALIDATE_TARGET_SET_MISMATCH", "SCHEMA_INVALID", "SINGLE_USE_ALREADY_CONSUMED", "STORED_CONSUMPTION_CORE_INVALID", "TOOL_PIN_INTENT_MISMATCH"},
		Commands:    []string{"query", "record"}, ApprovalStates: []string{"approved", "cancelled", "expired", "rejected"},
		Scopes: []string{"creation_spec_activation"}, Actions: []string{"activate"}, Effects: []string{"creation_spec_activation"}, ResourceTypes: []string{"creation_spec_candidate"}, ConsumptionPolicies: []string{"single_use"},
		EvidenceKinds: []string{"authorization_scope", "policy_snapshot", "resource_snapshot"},
	}
	if !reflect.DeepEqual(corpus.ExactSets, want) {
		t.Fatalf("exact sets=%+v want=%+v", corpus.ExactSets, want)
	}
}

func TestApprovalConsumptionReceiptCoreV1FirstWrite(t *testing.T) {
	_, fixtures := loadApprovalConsumptionCorpusV1(t)
	got := evaluateApprovalConsumptionRecordV1(fixtures["acr.creation_spec_activation.approved_unconsumed"])
	metadata := fixtures["acr.creation_spec_activation.approved_unconsumed"].OperationalMetadata
	if got.Decision != "recorded" || got.Core == nil || got.Core.ReceiptVersion != 1 || got.Core.WriteState != "recorded" || got.Core.IntentBinding.EffectKind != "creation_spec_activation" ||
		metadata.ChildWriteState != "open" || !safePositiveIntegerV1(metadata.CurrentFence) || !safePositiveIntegerV1(metadata.ExpectedChildVersion) {
		t.Fatalf("first write=%+v", got)
	}
}

func TestApprovalConsumptionReceiptCoreV1ReplayAndQuery(t *testing.T) {
	_, fixtures := loadApprovalConsumptionCorpusV1(t)
	for _, id := range []string{"acr.creation_spec_activation.approved_recorded", "acr.creation_spec_activation.approved_recorded_current_drift"} {
		fixture := fixtures[id]
		record := evaluateApprovalConsumptionRecordV1(fixture)
		query := evaluateApprovalConsumptionQueryV1(fixture)
		if record.Decision != "replayed" || query.Decision != "found" || record.CoreDigest != query.CoreDigest || record.Core == nil || query.Core == nil || record.Core.ReceiptID != query.Core.ReceiptID ||
			validateApprovalConsumptionQueryResultV1(fixture.QueryExpectedBinding, *query.Core) != "" {
			t.Fatalf("fixture=%s record=%+v query=%+v", id, record, query)
		}
	}
	fixture := fixtures["acr.creation_spec_activation.approved_recorded"]
	query := evaluateApprovalConsumptionQueryV1(fixture)
	tampered := *query.Core
	tampered.IntentBinding.ChildToolReceiptID = "019f4500-0000-7000-8000-000000000799"
	if reason := validateApprovalConsumptionQueryResultV1(fixture.QueryExpectedBinding, tampered); reason != "CONSUMPTION_QUERY_BINDING_CONFLICT" {
		t.Fatalf("query-result pure evaluator reason=%s", reason)
	}
}

func TestApprovalConsumptionReceiptCoreV1SingleUseConflicts(t *testing.T) {
	_, fixtures := loadApprovalConsumptionCorpusV1(t)
	for _, testCase := range []approvalConsumptionCaseV1{
		{Command: "record", FromFixture: "acr.creation_spec_activation.approved_recorded", Mutations: []string{"record.intent.target_digest.rehash"}},
		{Command: "record", FromFixture: "acr.creation_spec_activation.approved_recorded", Mutations: []string{"stored.key.different"}},
	} {
		got := evaluateApprovalConsumptionCaseV1(t, fixtures, testCase)
		if got.Decision != "conflict" || got.Core != nil || got.CoreDigest != "" {
			t.Fatalf("mutations=%v got=%+v", testCase.Mutations, got)
		}
	}
}

func TestApprovalConsumptionReceiptCoreV1EligibilityAndRevalidation(t *testing.T) {
	_, fixtures := loadApprovalConsumptionCorpusV1(t)
	for _, mutation := range []string{"state.rejected", "state.expired", "state.cancelled", "recorded_at.after_expiry", "current.scope.drift", "current.resource_version.drift", "current.resource_digest.drift", "current.target.drift", "current.policy.drift"} {
		fixture := cloneApprovalConsumptionFixtureV1(fixtures["acr.creation_spec_activation.approved_unconsumed"])
		applyApprovalConsumptionMutationV1(t, &fixture, mutation)
		if got := evaluateApprovalConsumptionRecordV1(fixture); got.Decision == "recorded" {
			t.Fatalf("mutation=%s 未失败关闭", mutation)
		}
	}
}

func TestApprovalConsumptionReceiptCoreV1ReasonPriority(t *testing.T) {
	_, fixtures := loadApprovalConsumptionCorpusV1(t)
	tests := []struct {
		mutations []string
		want      string
	}{
		{[]string{"record.command.invalid", "record.key.invalid"}, "COMMAND_INVALID"},
		{[]string{"record.key.invalid", "identity.user.mismatch"}, "KEY_DERIVATION_MISMATCH"},
		{[]string{"identity.user.mismatch", "record.causal.approval_id.mismatch"}, "AUTHENTICATED_IDENTITY_MISMATCH"},
		{[]string{"record.causal.source.mismatch", "record.request_digest.invalid"}, "IMMUTABLE_CAUSAL_BINDING_MISMATCH"},
		{[]string{"record.request_digest.invalid", "stored.key.different"}, "REQUEST_DIGEST_MISMATCH"},
		{[]string{"state.expired", "tool.intent_digest.drift"}, "APPROVAL_EFFECT_NOT_ELIGIBLE"},
		{[]string{"tool.intent_digest.drift", "current.scope.drift"}, "TOOL_PIN_INTENT_MISMATCH"},
		{[]string{"current.scope.drift", "current.resource_version.drift"}, "REVALIDATE_SCOPE_MISMATCH"},
		{[]string{"current.resource_version.drift", "current.target.drift"}, "REVALIDATE_RESOURCE_MISMATCH"},
		{[]string{"current.target.drift", "current.policy.drift"}, "REVALIDATE_TARGET_SET_MISMATCH"},
	}
	for _, testCase := range tests {
		fixture := cloneApprovalConsumptionFixtureV1(fixtures["acr.creation_spec_activation.approved_unconsumed"])
		for _, mutation := range testCase.mutations {
			applyApprovalConsumptionMutationV1(t, &fixture, mutation)
		}
		if got := evaluateApprovalConsumptionRecordV1(fixture); !reflect.DeepEqual(got.ReasonCodes, []string{testCase.want}) {
			t.Fatalf("mutations=%v reasons=%v want=%s", testCase.mutations, got.ReasonCodes, testCase.want)
		}
	}
}

func TestApprovalConsumptionReceiptCoreV1StrictJSON(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":"approval_consumption_fixture.v1"`,
		`{"schema_version":"approval_consumption_fixture.v1","schema_version":"approval_consumption_fixture.v1"}`,
		`{} {}`,
		`{"schema_version":"approval_consumption_fixture.v1","billing_key":"forbidden"}`,
		`{"schema_version":"approval_consumption_fixture.v1","amount":1}`,
	} {
		var fixture approvalConsumptionFixtureV1
		if err := inspectJSON([]byte(raw)); err == nil {
			if err = strictDecode([]byte(raw), &fixture); err == nil {
				t.Fatalf("strict JSON 未拒绝 %s", raw)
			}
		}
	}
	for _, field := range []string{"billable_execution", "quote", "amount", "currency", "unit", "charge", "billing_key", "model", "prompt", "execution_digest", "primary", "correction"} {
		raw := []byte(fmt.Sprintf(`{"schema_version":"approval_consumption_receipt_core.v1","%s":"forbidden"}`, field))
		var core approvalConsumptionReceiptCoreV1
		if err := strictDecode(raw, &core); err == nil {
			t.Fatalf("billing-only 字段未被拒绝 field=%s", field)
		}
	}
	_, fixtures := loadApprovalConsumptionCorpusV1(t)
	fixture := fixtures["acr.creation_spec_activation.approved_unconsumed"]
	recordRaw, err := canonicalJSON(fixture.RecordCommand)
	if err != nil || validateApprovalConsumptionRecordCommandJSONV1(recordRaw) != nil {
		t.Fatalf("合法 record command strict 校验失败 err=%v", err)
	}
	materialRaw, err := canonicalJSON(fixture.FirstWriteMaterial)
	if err != nil || validateApprovalConsumptionFirstWriteMaterialJSONV1(materialRaw) != nil {
		t.Fatalf("合法 first_write_material strict 校验失败 err=%v", err)
	}
	for name, raw := range map[string][]byte{
		"record-missing-intent": []byte(`{"schema_version":"approval_consumption_record_command.v1","command":"record","authenticated_identity":{},"request_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
		"record-null-intent":    []byte(`{"schema_version":"approval_consumption_record_command.v1","command":"record","authenticated_identity":{},"intent_binding":null,"request_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
		"record-signature":      []byte(`{"schema_version":"approval_consumption_record_command.v1","command":"record","authenticated_identity":{},"intent_binding":{},"request_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","signature":"forbidden"}`),
		"record-nested-unknown": []byte(`{"schema_version":"approval_consumption_record_command.v1","command":"record","authenticated_identity":{"future":true},"intent_binding":{},"request_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`),
	} {
		if err := validateApprovalConsumptionRecordCommandJSONV1(raw); err == nil {
			t.Fatalf("record command strict 未拒绝 %s", name)
		}
	}
	for name, raw := range map[string][]byte{
		"material-missing-observation": []byte(`{"receipt_id":"019f4500-0000-7000-8000-000000000501"}`),
		"material-null-observation":    []byte(`{"receipt_id":"019f4500-0000-7000-8000-000000000501","revalidation_observation":null}`),
		"material-signature":           []byte(`{"receipt_id":"019f4500-0000-7000-8000-000000000501","revalidation_observation":{},"signature":"forbidden"}`),
		"material-nested-unknown":      []byte(`{"receipt_id":"019f4500-0000-7000-8000-000000000501","revalidation_observation":{"future":true}}`),
	} {
		if err := validateApprovalConsumptionFirstWriteMaterialJSONV1(raw); err == nil {
			t.Fatalf("first_write_material strict 未拒绝 %s", name)
		}
	}
}

func validateApprovalConsumptionRecordCommandJSONV1(raw []byte) error {
	var command approvalConsumptionRecordCommandV1
	return strictApprovalConsumptionRequiredObjectV1(raw, &command, []string{"schema_version", "command", "authenticated_identity", "intent_binding", "request_digest"})
}

func validateApprovalConsumptionFirstWriteMaterialJSONV1(raw []byte) error {
	var material approvalConsumptionFirstWriteMaterialV1
	return strictApprovalConsumptionRequiredObjectV1(raw, &material, []string{"receipt_id", "revalidation_observation"})
}

func strictApprovalConsumptionRequiredObjectV1(raw []byte, target any, required []string) error {
	if err := inspectJSON(raw); err != nil {
		return err
	}
	if err := strictDecode(raw, target); err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if len(fields) != len(required) {
		return fmt.Errorf("field count=%d want=%d", len(fields), len(required))
	}
	for _, field := range required {
		value, exists := fields[field]
		if !exists || string(value) == "null" {
			return fmt.Errorf("required field=%s", field)
		}
	}
	return nil
}

func TestApprovalConsumptionReceiptCoreV1OperationalMetadataExcluded(t *testing.T) {
	_, fixtures := loadApprovalConsumptionCorpusV1(t)
	fixture := fixtures["acr.creation_spec_activation.approved_unconsumed"]
	before := evaluateApprovalConsumptionRecordV1(fixture)
	fixture.OperationalMetadata = approvalConsumptionOperationalMetadataV1{
		SchemaVersion: "approval_consumption_operational_metadata.v1", TraceID: "trace-retry", Attempt: 99,
		ProcessorInstance: "processor-z", ReadAt: "2026-07-16T09:10:11Z", ChildWriteState: "open", CurrentFence: 999, ExpectedChildVersion: 88,
	}
	after := evaluateApprovalConsumptionRecordV1(fixture)
	if before.RequestDigest != after.RequestDigest || before.CoreDigest != after.CoreDigest {
		t.Fatalf("operational metadata 改变摘要 before=%+v after=%+v", before, after)
	}
}

func evaluateApprovalConsumptionCaseV1(t *testing.T, fixtures map[string]approvalConsumptionFixtureV1, testCase approvalConsumptionCaseV1) approvalConsumptionEvaluationV1 {
	t.Helper()
	if testCase.RawFixtureMutation != "" {
		fixture, exists := fixtures[testCase.FromFixture]
		if !exists {
			t.Fatalf("raw mutation=%s 缺少完整基线 fixture=%s", testCase.RawFixtureMutation, testCase.FromFixture)
		}
		raw := buildApprovalConsumptionRawMutationV1(t, fixture, testCase.RawFixtureMutation)
		if err := validateApprovalConsumptionFixtureJSONV1(raw); err == nil {
			t.Fatalf("raw mutation=%s 未被目标 strict validator 拒绝", testCase.RawFixtureMutation)
		}
		return invalidApprovalConsumptionV1("SCHEMA_INVALID")
	}
	if testCase.RawFixtureJSON != "" {
		var fixture approvalConsumptionFixtureV1
		raw := []byte(testCase.RawFixtureJSON)
		if err := inspectJSON(raw); err != nil {
			return invalidApprovalConsumptionV1("SCHEMA_INVALID")
		}
		if err := strictDecode(raw, &fixture); err != nil || fixture.SchemaVersion != "approval_consumption_fixture.v1" {
			return invalidApprovalConsumptionV1("SCHEMA_INVALID")
		}
		if testCase.Command == "query" {
			return evaluateApprovalConsumptionQueryV1(fixture)
		}
		return evaluateApprovalConsumptionRecordV1(fixture)
	}
	fixture, exists := fixtures[testCase.FromFixture]
	if !exists {
		return invalidApprovalConsumptionV1("SCHEMA_INVALID")
	}
	fixture = cloneApprovalConsumptionFixtureV1(fixture)
	for _, mutation := range testCase.Mutations {
		applyApprovalConsumptionMutationV1(t, &fixture, mutation)
	}
	if testCase.Command == "query" {
		result := evaluateApprovalConsumptionQueryV1(fixture)
		if testCase.ValidateExpectedBinding && result.Decision == "found" {
			if reason := validateApprovalConsumptionQueryResultV1(fixture.QueryExpectedBinding, *result.Core); reason != "" {
				return invalidApprovalConsumptionConflictV1(reason)
			}
		}
		return result
	}
	return evaluateApprovalConsumptionRecordV1(fixture)
}

// buildApprovalConsumptionRawMutationV1 从完整合法 fixture 制造单点 JSON 破坏，避免稀疏非法对象让 strict 向量因无关缺字段而自证通过。
func buildApprovalConsumptionRawMutationV1(t *testing.T, fixture approvalConsumptionFixtureV1, mutation string) []byte {
	t.Helper()
	raw, err := canonicalJSON(fixture)
	if err != nil {
		t.Fatal(err)
	}
	switch mutation {
	case "strict.malformed":
		return raw[:len(raw)-1]
	case "strict.duplicate":
		return append([]byte(`{"schema_version":"approval_consumption_fixture.v1",`), raw[1:]...)
	case "strict.trailing":
		return append(raw, []byte(` {}`)...)
	case "strict.root_unknown":
		return append([]byte(`{"future":true,`), raw[1:]...)
	case "strict.root_billing":
		return append([]byte(`{"quote":"forbidden",`), raw[1:]...)
	}

	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	record, recordOK := root["record_command"].(map[string]any)
	material, materialOK := root["first_write_material"].(map[string]any)
	switch mutation {
	case "strict.record_nested_unknown":
		if !recordOK {
			t.Fatal("完整 fixture 缺 record_command")
		}
		record["future"] = true
	case "strict.record_intent_null":
		if !recordOK {
			t.Fatal("完整 fixture 缺 record_command")
		}
		record["intent_binding"] = nil
	case "strict.first_write_null":
		root["first_write_material"] = nil
	case "strict.record_signature":
		if !recordOK {
			t.Fatal("完整 fixture 缺 record_command")
		}
		record["signature"] = "forbidden"
	case "strict.first_write_signature":
		if !materialOK {
			t.Fatal("完整 fixture 缺 first_write_material")
		}
		material["signature"] = "forbidden"
	default:
		t.Fatalf("未知 raw mutation=%s", mutation)
	}
	mutated, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return mutated
}

// validateApprovalConsumptionFixtureJSONV1 对完整 fixture 与两个安全敏感嵌套对象执行 exact-field/non-null strict 校验。
func validateApprovalConsumptionFixtureJSONV1(raw []byte) error {
	var fixture approvalConsumptionFixtureV1
	required := []string{"schema_version", "decision_authority", "record_command", "first_write_material", "query_command", "query_expected_binding", "current_eligibility", "stored_consumption_index", "stored_cores", "operational_metadata"}
	if err := strictApprovalConsumptionRequiredObjectV1(raw, &fixture, required); err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if err := validateApprovalConsumptionRecordCommandJSONV1(fields["record_command"]); err != nil {
		return err
	}
	return validateApprovalConsumptionFirstWriteMaterialJSONV1(fields["first_write_material"])
}

// evaluateApprovalConsumptionRecordV1 按设计冻结的优先级模拟 first-write-wins；它不是生产事务实现。
func evaluateApprovalConsumptionRecordV1(fixture approvalConsumptionFixtureV1) approvalConsumptionEvaluationV1 {
	command := fixture.RecordCommand
	if reason := validateApprovalConsumptionRecordSchemaV1(fixture); reason != "" {
		return invalidApprovalConsumptionV1(reason)
	}
	if command.Command != "record" {
		return invalidApprovalConsumptionV1("COMMAND_INVALID")
	}
	intent := command.IntentBinding
	if !approvalConsumptionFixedActivationV1(intent) || intent.ConsumptionKey != derivedApprovalConsumptionKeyV1(intent) {
		return invalidApprovalConsumptionV1("KEY_DERIVATION_MISMATCH")
	}
	if !approvalConsumptionIdentityMatchesIntentV1(command.AuthenticatedIdentity, intent) {
		return invalidApprovalConsumptionV1("AUTHENTICATED_IDENTITY_MISMATCH")
	}
	if intent.ResultingApprovalVersion != intent.PresentedApprovalVersion+1 ||
		intent.ContinuationSourceID != "approval-decision:"+intent.ApprovalID+":"+intent.DecisionID || intent.ParentResultStatus != "waiting_user" ||
		intent.ParentToolReceiptOwnerRef != "tool-receipt:"+intent.ParentToolReceiptID+"@v1" {
		return invalidApprovalConsumptionV1("IMMUTABLE_CAUSAL_BINDING_MISMATCH")
	}
	requestDigest, err := digestValueV1(consumptionIntentDigestDomainV1, intent)
	if err != nil || requestDigest != command.RequestDigest {
		return invalidApprovalConsumptionV1("REQUEST_DIGEST_MISMATCH")
	}
	exact, backstop, indexReason := selectApprovalConsumptionIndexesV1(fixture.StoredConsumptionIndex, intent.ApprovalID, intent.ConsumptionKey)
	if indexReason != "" {
		return invalidApprovalConsumptionV1(indexReason)
	}
	if exact != nil {
		if reason := validateApprovalConsumptionStoredIndexV1(*exact); reason != "" {
			return invalidApprovalConsumptionV1(reason)
		}
		stored, coreUnique := selectApprovalConsumptionCoreV1(fixture.StoredCores, exact.CoreDigest)
		if !coreUnique || validateApprovalConsumptionStoredCoreV1(*exact, *stored) != "" {
			return invalidApprovalConsumptionV1("STORED_CONSUMPTION_CORE_INVALID")
		}
		storedIntent := stored.Core.IntentBinding
		if !approvalConsumptionIdentityMatchesIntentV1(command.AuthenticatedIdentity, storedIntent) || exact.EffectKind != intent.EffectKind {
			return invalidApprovalConsumptionV1("AUTHENTICATED_IDENTITY_MISMATCH")
		}
		if exact.RequestDigest != requestDigest {
			return invalidApprovalConsumptionConflictV1("CONSUMPTION_DIGEST_CONFLICT")
		}
		core := stored.Core
		return approvalConsumptionEvaluationV1{Decision: "replayed", ReasonCodes: []string{}, RequestDigest: requestDigest, CoreDigest: stored.CoreDigest, Core: &core}
	}
	if backstop != nil {
		if reason := validateApprovalConsumptionStoredIndexV1(*backstop); reason != "" {
			return invalidApprovalConsumptionV1(reason)
		}
		if command.AuthenticatedIdentity.UserID != backstop.UserID || command.AuthenticatedIdentity.ProjectID != backstop.ProjectID || command.AuthenticatedIdentity.SessionID != backstop.SessionID {
			return invalidApprovalConsumptionV1("AUTHENTICATED_IDENTITY_MISMATCH")
		}
		return invalidApprovalConsumptionConflictV1("SINGLE_USE_ALREADY_CONSUMED")
	}
	if reason := validateApprovalConsumptionFreshRecordMaterialV1(fixture); reason != "" {
		return invalidApprovalConsumptionV1(reason)
	}
	if fixture.DecisionAuthority.SchemaVersion != "approval_decision_authority.v1" {
		return invalidApprovalConsumptionV1("SCHEMA_INVALID")
	}
	frozen := fixture.DecisionAuthority.FrozenIntent
	if fixture.DecisionAuthority.Owner != "agent.approval_store" || !approvalConsumptionIdentityMatchesIntentV1(command.AuthenticatedIdentity, frozen) ||
		!approvalConsumptionCausalEqualV1(intent, frozen) {
		return invalidApprovalConsumptionV1("IMMUTABLE_CAUSAL_BINDING_MISMATCH")
	}
	if !approvalConsumptionAuthorizationEvidenceFixedV1(intent) || intent.ExpectedAuthorizationScope != frozen.ExpectedAuthorizationScope {
		return invalidApprovalConsumptionV1("REVALIDATE_SCOPE_MISMATCH")
	}
	if !approvalConsumptionResourceEvidenceFixedV1(intent) || intent.ExpectedResourceSnapshot != frozen.ExpectedResourceSnapshot ||
		!approvalConsumptionTargetEqualV1(intent, frozen, false) {
		return invalidApprovalConsumptionV1("REVALIDATE_RESOURCE_MISMATCH")
	}
	if !approvalConsumptionTargetEqualV1(intent, frozen, true) {
		return invalidApprovalConsumptionV1("REVALIDATE_TARGET_SET_MISMATCH")
	}
	if !approvalConsumptionPolicyEvidenceFixedV1(intent) || intent.ExpectedPolicySnapshot != frozen.ExpectedPolicySnapshot {
		return invalidApprovalConsumptionV1("REVALIDATE_POLICY_MISMATCH")
	}
	if intent.DecisionAction != "approve" || intent.ResultingState != "approved" || fixture.DecisionAuthority.ApprovalState != "approved" {
		return invalidApprovalConsumptionV1("APPROVAL_EFFECT_NOT_ELIGIBLE")
	}
	recordedAt, _ := time.Parse(time.RFC3339Nano, fixture.FirstWriteMaterial.RevalidationObservation.RecordedAt)
	expiresAt, _ := time.Parse(time.RFC3339Nano, intent.ApprovalExpiresAt)
	if !recordedAt.Before(expiresAt) {
		return invalidApprovalConsumptionV1("APPROVAL_EXPIRED")
	}
	if !approvalConsumptionToolIntentEqualV1(intent, frozen) {
		return invalidApprovalConsumptionV1("TOOL_PIN_INTENT_MISMATCH")
	}
	observation := fixture.FirstWriteMaterial.RevalidationObservation.Evidence
	if len(observation) != 3 || observation[0].Kind != "authorization_scope" || observation[1].Kind != "resource_snapshot" || observation[2].Kind != "policy_snapshot" ||
		observation[0] != intent.ExpectedAuthorizationScope || observation[0] != fixture.CurrentEligibility.AuthorizationScope {
		return invalidApprovalConsumptionV1("REVALIDATE_SCOPE_MISMATCH")
	}
	if observation[1] != intent.ExpectedResourceSnapshot || observation[1] != fixture.CurrentEligibility.ResourceSnapshot ||
		intent.ResourceType != fixture.CurrentEligibility.ResourceType || intent.ResourceID != fixture.CurrentEligibility.ResourceID ||
		intent.ResourceVersion != fixture.CurrentEligibility.ResourceVersion || intent.ResourceDigest != fixture.CurrentEligibility.ResourceDigest {
		return invalidApprovalConsumptionV1("REVALIDATE_RESOURCE_MISMATCH")
	}
	if intent.TargetExactSetDigest != fixture.CurrentEligibility.TargetExactSetDigest {
		return invalidApprovalConsumptionV1("REVALIDATE_TARGET_SET_MISMATCH")
	}
	if observation[2] != intent.ExpectedPolicySnapshot || observation[2] != fixture.CurrentEligibility.PolicySnapshot {
		return invalidApprovalConsumptionV1("REVALIDATE_POLICY_MISMATCH")
	}
	core := buildApprovalConsumptionCoreV1(command, fixture.FirstWriteMaterial)
	coreDigest, digestErr := digestValueV1(consumptionCoreDigestDomainV1, core)
	if digestErr != nil {
		return invalidApprovalConsumptionV1("SCHEMA_INVALID")
	}
	return approvalConsumptionEvaluationV1{Decision: "recorded", ReasonCodes: []string{}, RequestDigest: requestDigest, CoreDigest: coreDigest, Core: &core}
}

// evaluateApprovalConsumptionQueryV1 只校验 trusted identity 与原 key；不读取 intent、request digest 或 current drift。
func evaluateApprovalConsumptionQueryV1(fixture approvalConsumptionFixtureV1) approvalConsumptionEvaluationV1 {
	command := fixture.QueryCommand
	if reason := validateApprovalConsumptionQuerySchemaV1(command); reason != "" {
		return invalidApprovalConsumptionV1(reason)
	}
	if command.Command != "query" {
		return invalidApprovalConsumptionV1("COMMAND_INVALID")
	}
	if command.AuthenticatedIdentity.AgentPrincipal != trustedConsumptionPrincipalV1 {
		return invalidApprovalConsumptionV1("AUTHENTICATED_IDENTITY_MISMATCH")
	}
	if !validApprovalConsumptionQueryKeyV1(command.ConsumptionKey) {
		return invalidApprovalConsumptionV1("KEY_DERIVATION_MISMATCH")
	}
	index, _, indexReason := selectApprovalConsumptionIndexesV1(fixture.StoredConsumptionIndex, command.ApprovalID, command.ConsumptionKey)
	if indexReason != "" {
		return invalidApprovalConsumptionV1(indexReason)
	}
	if index == nil {
		return approvalConsumptionEvaluationV1{Decision: "not_found", ReasonCodes: []string{"NOT_FOUND"}}
	}
	if reason := validateApprovalConsumptionStoredIndexV1(*index); reason != "" {
		return invalidApprovalConsumptionV1(reason)
	}
	stored, coreUnique := selectApprovalConsumptionCoreV1(fixture.StoredCores, index.CoreDigest)
	if !coreUnique || validateApprovalConsumptionStoredCoreV1(*index, *stored) != "" {
		return invalidApprovalConsumptionV1("STORED_CONSUMPTION_CORE_INVALID")
	}
	if !approvalConsumptionIdentityMatchesIntentV1(command.AuthenticatedIdentity, stored.Core.IntentBinding) {
		return invalidApprovalConsumptionV1("AUTHENTICATED_IDENTITY_MISMATCH")
	}
	core := stored.Core
	return approvalConsumptionEvaluationV1{Decision: "found", ReasonCodes: []string{}, RequestDigest: core.RequestDigest, CoreDigest: stored.CoreDigest, Core: &core}
}

func validateApprovalConsumptionRecordSchemaV1(fixture approvalConsumptionFixtureV1) string {
	command := fixture.RecordCommand
	if command.SchemaVersion != "approval_consumption_record_command.v1" || command.AuthenticatedIdentity.SchemaVersion != "approval_consumption_authenticated_identity.v1" ||
		command.IntentBinding.SchemaVersion != "approval_consumption_intent_binding.v1" {
		return "SCHEMA_INVALID"
	}
	intent := command.IntentBinding
	ids := []string{intent.ApprovalID, intent.DecisionReceiptID, intent.DecisionID, intent.UserID, intent.ProjectID, intent.SessionID,
		intent.RootToolReceiptID, intent.RootTurnID, intent.RootRunID, intent.ParentToolReceiptID, intent.ParentTurnID, intent.ParentRunID, intent.OriginalToolCallID, intent.ContinuationInputID,
		intent.ContinuationTurnID, intent.ContinuationRunID, intent.ChildToolReceiptID, intent.ResourceID}
	for _, id := range ids {
		if !canonicalUUIDv7(id) {
			return "SCHEMA_INVALID"
		}
	}
	if !safePositiveIntegerV1(intent.PresentedApprovalVersion) || !safePositiveIntegerV1(intent.ResultingApprovalVersion) || !safePositiveIntegerV1(intent.RootToolReceiptVersion) ||
		!safePositiveIntegerV1(intent.ParentToolReceiptVersion) || !safePositiveIntegerV1(intent.ResourceVersion) || !digestPattern.MatchString(command.RequestDigest) ||
		!approvalConsumptionIntentDigestsValidV1(intent) || !canonicalUTCRFC3339NanoV1(intent.ApprovalExpiresAt) ||
		!approvalConsumptionIdentityShapeValidV1(command.AuthenticatedIdentity) || !approvalConsumptionIntentRequiredTextV1(intent) {
		return "SCHEMA_INVALID"
	}
	return ""
}

func validateApprovalConsumptionQuerySchemaV1(command approvalConsumptionQueryCommandV1) string {
	if command.SchemaVersion != "approval_consumption_query_command.v1" || command.AuthenticatedIdentity.SchemaVersion != "approval_consumption_authenticated_identity.v1" ||
		!canonicalUUIDv7(command.ApprovalID) || !approvalConsumptionIdentityShapeValidV1(command.AuthenticatedIdentity) {
		return "SCHEMA_INVALID"
	}
	return ""
}

func approvalConsumptionIntentDigestsValidV1(intent approvalConsumptionIntentBindingV1) bool {
	for _, digest := range []string{intent.DecisionDigest, intent.RootRequestSemanticDigest, intent.ParentRequestSemanticDigest, intent.ParentResultDigest, intent.ParentToolReceiptOwnerDigest, intent.ToolPinDigest, intent.IntentDigest, intent.ResourceDigest, intent.TargetExactSetDigest} {
		if !digestPattern.MatchString(digest) {
			return false
		}
	}
	return approvalConsumptionEvidenceValidV1(intent.ExpectedAuthorizationScope) && approvalConsumptionEvidenceValidV1(intent.ExpectedResourceSnapshot) && approvalConsumptionEvidenceValidV1(intent.ExpectedPolicySnapshot)
}

func approvalConsumptionIntentRequiredTextV1(intent approvalConsumptionIntentBindingV1) bool {
	values := []string{
		intent.ContinuationSourceID, intent.DecisionAction, intent.ResultingState, intent.ParentResultStatus,
		intent.ParentToolReceiptOwnerRef, intent.Scope, intent.Action, intent.ConsumptionPolicy, intent.EffectKind, intent.ConsumptionKey,
		intent.ToolPinOwner, intent.ToolPinRef, intent.ToolKey, intent.DefinitionVersion, intent.IntentSchemaVersion, intent.ResultSchemaVersion,
		intent.GraphKey, intent.ResourceType,
	}
	for _, value := range values {
		if value == "" || value != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

func approvalConsumptionExpectedEvidenceKindsV1(intent approvalConsumptionIntentBindingV1) bool {
	return intent.ExpectedAuthorizationScope.Kind == "authorization_scope" && intent.ExpectedResourceSnapshot.Kind == "resource_snapshot" &&
		intent.ExpectedPolicySnapshot.Kind == "policy_snapshot"
}

func approvalConsumptionAuthorizationEvidenceFixedV1(intent approvalConsumptionIntentBindingV1) bool {
	value := intent.ExpectedAuthorizationScope
	return value.Kind == "authorization_scope" && value.Owner == "business.authorization" &&
		value.Ref == "project-scope:"+intent.ProjectID+":user:"+intent.UserID
}

func approvalConsumptionResourceEvidenceFixedV1(intent approvalConsumptionIntentBindingV1) bool {
	value := intent.ExpectedResourceSnapshot
	return value.Kind == "resource_snapshot" && value.Owner == "business.creation_spec" && value.Version == intent.ResourceVersion &&
		value.Ref == fmt.Sprintf("creation-spec-candidate:%s@v%d", intent.ResourceID, intent.ResourceVersion)
}

func approvalConsumptionPolicyEvidenceFixedV1(intent approvalConsumptionIntentBindingV1) bool {
	value := intent.ExpectedPolicySnapshot
	return value.Kind == "policy_snapshot" && value.Owner == "business.creation_spec_policy" &&
		value.Ref == fmt.Sprintf("creation-spec-activation-policy:%s@v%d", intent.ProjectID, value.Version)
}

func approvalConsumptionEvidenceValidV1(value approvalConsumptionEvidenceRefV1) bool {
	return value.Kind != "" && value.Owner != "" && value.Ref != "" && safePositiveIntegerV1(value.Version) && digestPattern.MatchString(value.Digest)
}

func approvalConsumptionCausalEqualV1(actual, expected approvalConsumptionIntentBindingV1) bool {
	a := actual
	e := expected
	// Tool/Intent、target 与 eligibility 由其各自较低优先级阶段验证；root/direct parent/child 始终属于 causal。
	a.ToolPinOwner, e.ToolPinOwner = "", ""
	a.ToolPinRef, e.ToolPinRef = "", ""
	a.ToolPinDigest, e.ToolPinDigest = "", ""
	a.ToolKey, e.ToolKey = "", ""
	a.DefinitionVersion, e.DefinitionVersion = "", ""
	a.IntentSchemaVersion, e.IntentSchemaVersion = "", ""
	a.ResultSchemaVersion, e.ResultSchemaVersion = "", ""
	a.GraphKey, e.GraphKey = "", ""
	a.IntentDigest, e.IntentDigest = "", ""
	a.ResourceType, e.ResourceType = "", ""
	a.ResourceID, e.ResourceID = "", ""
	a.ResourceVersion, e.ResourceVersion = 0, 0
	a.ResourceDigest, e.ResourceDigest = "", ""
	a.TargetExactSetDigest, e.TargetExactSetDigest = "", ""
	a.ExpectedAuthorizationScope, e.ExpectedAuthorizationScope = approvalConsumptionEvidenceRefV1{}, approvalConsumptionEvidenceRefV1{}
	a.ExpectedResourceSnapshot, e.ExpectedResourceSnapshot = approvalConsumptionEvidenceRefV1{}, approvalConsumptionEvidenceRefV1{}
	a.ExpectedPolicySnapshot, e.ExpectedPolicySnapshot = approvalConsumptionEvidenceRefV1{}, approvalConsumptionEvidenceRefV1{}
	return reflect.DeepEqual(a, e)
}

func approvalConsumptionToolIntentEqualV1(actual, expected approvalConsumptionIntentBindingV1) bool {
	return actual.ToolPinOwner == "agent.tool_registry" && actual.ToolPinRef == "tool-pin:plan_creation_spec.v1alpha1@v1" &&
		actual.ToolKey == "plan_creation_spec" && actual.DefinitionVersion == "plan_creation_spec.v1alpha1" &&
		actual.IntentSchemaVersion == "plan_creation_spec_intent.v1" && actual.ResultSchemaVersion == "graph_tool_result.v1" && actual.GraphKey == "plan_creation_spec_graph_v1" &&
		actual.ToolPinOwner == expected.ToolPinOwner && actual.ToolPinRef == expected.ToolPinRef &&
		actual.ToolPinDigest == expected.ToolPinDigest && actual.ToolKey == expected.ToolKey && actual.DefinitionVersion == expected.DefinitionVersion &&
		actual.IntentSchemaVersion == expected.IntentSchemaVersion && actual.ResultSchemaVersion == expected.ResultSchemaVersion && actual.GraphKey == expected.GraphKey &&
		actual.IntentDigest == expected.IntentDigest
}

func derivedApprovalConsumptionKeyV1(intent approvalConsumptionIntentBindingV1) string {
	return fmt.Sprintf("approval-consumption:v1:%d:%s", intent.ResultingApprovalVersion, intent.EffectKind)
}

func buildApprovalConsumptionCoreV1(command approvalConsumptionRecordCommandV1, material approvalConsumptionFirstWriteMaterialV1) approvalConsumptionReceiptCoreV1 {
	return approvalConsumptionReceiptCoreV1{
		SchemaVersion: "approval_consumption_receipt_core.v1", ReceiptID: material.ReceiptID, ReceiptVersion: 1, WriteState: "recorded",
		IntentBinding: command.IntentBinding, RequestDigest: command.RequestDigest, RevalidationObservation: material.RevalidationObservation,
	}
}

func approvalConsumptionStoredIndexFromCoreV1(core approvalConsumptionReceiptCoreV1, coreDigest string) approvalConsumptionStoredIndexV1 {
	intent := core.IntentBinding
	return approvalConsumptionStoredIndexV1{
		SchemaVersion: "approval_consumption_stored_index.v1", Owner: "agent.approval_store", ApprovalID: intent.ApprovalID, ConsumptionKey: intent.ConsumptionKey,
		RequestDigest: core.RequestDigest, UserID: intent.UserID, ProjectID: intent.ProjectID, SessionID: intent.SessionID,
		EffectKind: intent.EffectKind, CoreDigest: coreDigest,
	}
}

func invalidApprovalConsumptionV1(reason string) approvalConsumptionEvaluationV1 {
	return approvalConsumptionEvaluationV1{Decision: "invalid", ReasonCodes: []string{reason}}
}

func invalidApprovalConsumptionConflictV1(reason string) approvalConsumptionEvaluationV1 {
	return approvalConsumptionEvaluationV1{Decision: "conflict", ReasonCodes: []string{reason}}
}

func validateApprovalConsumptionFreshRecordMaterialV1(fixture approvalConsumptionFixtureV1) string {
	material := fixture.FirstWriteMaterial
	if fixture.CurrentEligibility.SchemaVersion != "approval_consumption_current_eligibility.v1" ||
		fixture.OperationalMetadata.SchemaVersion != "approval_consumption_operational_metadata.v1" ||
		!canonicalUUIDv7(material.ReceiptID) || material.RevalidationObservation.SchemaVersion != "approval_consumption_revalidation_observation.v1" ||
		!canonicalUTCRFC3339NanoV1(material.RevalidationObservation.RecordedAt) {
		return "SCHEMA_INVALID"
	}
	evidenceSet := material.RevalidationObservation.Evidence
	if len(evidenceSet) != 3 || evidenceSet[0].Kind != "authorization_scope" || evidenceSet[1].Kind != "resource_snapshot" || evidenceSet[2].Kind != "policy_snapshot" {
		return "SCHEMA_INVALID"
	}
	for _, evidence := range evidenceSet {
		if !approvalConsumptionEvidenceValidV1(evidence) {
			return "SCHEMA_INVALID"
		}
	}
	if fixture.OperationalMetadata.ChildWriteState != "open" || !safePositiveIntegerV1(fixture.OperationalMetadata.CurrentFence) || !safePositiveIntegerV1(fixture.OperationalMetadata.ExpectedChildVersion) {
		return "COMMAND_INVALID"
	}
	return ""
}

func approvalConsumptionFixedActivationV1(intent approvalConsumptionIntentBindingV1) bool {
	return intent.Scope == "creation_spec_activation" && intent.Action == "activate" && intent.ConsumptionPolicy == "single_use" &&
		intent.EffectKind == "creation_spec_activation" && intent.ResourceType == "creation_spec_candidate"
}

func approvalConsumptionIdentityShapeValidV1(identity approvalConsumptionIdentityV1) bool {
	return identity.AgentPrincipal != "" && canonicalUUIDv7(identity.UserID) && canonicalUUIDv7(identity.ProjectID) && canonicalUUIDv7(identity.SessionID)
}

func approvalConsumptionIdentityMatchesIntentV1(identity approvalConsumptionIdentityV1, intent approvalConsumptionIntentBindingV1) bool {
	return identity.AgentPrincipal == trustedConsumptionPrincipalV1 && identity.UserID == intent.UserID && identity.ProjectID == intent.ProjectID && identity.SessionID == intent.SessionID
}

func canonicalUTCRFC3339NanoV1(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && strings.HasSuffix(value, "Z") && parsed.Location() == time.UTC && parsed.Format(time.RFC3339Nano) == value
}

func validApprovalConsumptionQueryKeyV1(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 4 || parts[0] != "approval-consumption" || parts[1] != "v1" || parts[3] != "creation_spec_activation" {
		return false
	}
	version := int64(0)
	for _, character := range parts[2] {
		if character < '0' || character > '9' {
			return false
		}
		version = version*10 + int64(character-'0')
		if version > maxSafeIntegerV1 {
			return false
		}
	}
	return safePositiveIntegerV1(version)
}

func approvalConsumptionTargetEqualV1(actual, expected approvalConsumptionIntentBindingV1, includeTarget bool) bool {
	if actual.ResourceType != expected.ResourceType || actual.ResourceID != expected.ResourceID || actual.ResourceVersion != expected.ResourceVersion || actual.ResourceDigest != expected.ResourceDigest {
		return false
	}
	return !includeTarget || actual.TargetExactSetDigest == expected.TargetExactSetDigest
}

// selectApprovalConsumptionIndexesV1 扫描当前 Approval 的完整索引集合；重复 exact 或多个 single-use key 都视为存量损坏，结果不依赖数组顺序。
func selectApprovalConsumptionIndexesV1(indexes []approvalConsumptionStoredIndexV1, approvalID, key string) (exact, backstop *approvalConsumptionStoredIndexV1, reason string) {
	approvalCount := 0
	for index := range indexes {
		if indexes[index].ApprovalID != approvalID {
			continue
		}
		approvalCount++
		if indexes[index].ConsumptionKey == key {
			exact = &indexes[index]
		} else {
			backstop = &indexes[index]
		}
	}
	if approvalCount > 1 {
		return nil, nil, "STORED_CONSUMPTION_CORE_INVALID"
	}
	return exact, backstop, ""
}

// selectApprovalConsumptionCoreV1 要求目标摘要只对应一份 core；缺失或重复都不能由“取第一个”掩盖存储损坏。
func selectApprovalConsumptionCoreV1(cores []approvalConsumptionStoredCoreV1, digest string) (*approvalConsumptionStoredCoreV1, bool) {
	var matched *approvalConsumptionStoredCoreV1
	count := 0
	for index := range cores {
		if cores[index].CoreDigest != digest {
			continue
		}
		matched = &cores[index]
		count++
	}
	return matched, count == 1
}

func validateApprovalConsumptionStoredIndexV1(index approvalConsumptionStoredIndexV1) string {
	if index.SchemaVersion != "approval_consumption_stored_index.v1" || index.Owner != "agent.approval_store" || !canonicalUUIDv7(index.ApprovalID) || !validApprovalConsumptionQueryKeyV1(index.ConsumptionKey) ||
		!digestPattern.MatchString(index.RequestDigest) || !canonicalUUIDv7(index.UserID) || !canonicalUUIDv7(index.ProjectID) || !canonicalUUIDv7(index.SessionID) ||
		index.EffectKind != "creation_spec_activation" || !digestPattern.MatchString(index.CoreDigest) {
		return "STORED_CONSUMPTION_CORE_INVALID"
	}
	return ""
}

func validateApprovalConsumptionStoredCoreV1(index approvalConsumptionStoredIndexV1, stored approvalConsumptionStoredCoreV1) string {
	core := stored.Core
	intent := core.IntentBinding
	ids := []string{intent.ApprovalID, intent.DecisionReceiptID, intent.DecisionID, intent.UserID, intent.ProjectID, intent.SessionID,
		intent.RootToolReceiptID, intent.RootTurnID, intent.RootRunID, intent.ParentToolReceiptID, intent.ParentTurnID, intent.ParentRunID,
		intent.OriginalToolCallID, intent.ContinuationInputID, intent.ContinuationTurnID, intent.ContinuationRunID, intent.ChildToolReceiptID, intent.ResourceID}
	for _, id := range ids {
		if !canonicalUUIDv7(id) {
			return "STORED_CONSUMPTION_CORE_INVALID"
		}
	}
	if core.SchemaVersion != "approval_consumption_receipt_core.v1" || core.ReceiptVersion != 1 || core.WriteState != "recorded" || !canonicalUUIDv7(core.ReceiptID) ||
		intent.SchemaVersion != "approval_consumption_intent_binding.v1" || !approvalConsumptionFixedActivationV1(intent) || intent.ConsumptionKey != derivedApprovalConsumptionKeyV1(intent) ||
		intent.ResultingApprovalVersion != intent.PresentedApprovalVersion+1 || intent.ContinuationSourceID != "approval-decision:"+intent.ApprovalID+":"+intent.DecisionID ||
		intent.DecisionAction != "approve" || intent.ResultingState != "approved" || intent.ParentResultStatus != "waiting_user" ||
		!safePositiveIntegerV1(intent.PresentedApprovalVersion) || !safePositiveIntegerV1(intent.ResultingApprovalVersion) ||
		!safePositiveIntegerV1(intent.RootToolReceiptVersion) || !safePositiveIntegerV1(intent.ParentToolReceiptVersion) || !safePositiveIntegerV1(intent.ResourceVersion) ||
		!canonicalUTCRFC3339NanoV1(intent.ApprovalExpiresAt) || !approvalConsumptionIntentDigestsValidV1(intent) || !approvalConsumptionIntentRequiredTextV1(intent) ||
		!approvalConsumptionExpectedEvidenceKindsV1(intent) || !approvalConsumptionToolIntentEqualV1(intent, intent) ||
		intent.ParentToolReceiptOwnerRef != "tool-receipt:"+intent.ParentToolReceiptID+"@v1" ||
		!approvalConsumptionAuthorizationEvidenceFixedV1(intent) || !approvalConsumptionResourceEvidenceFixedV1(intent) || !approvalConsumptionPolicyEvidenceFixedV1(intent) ||
		core.RevalidationObservation.SchemaVersion != "approval_consumption_revalidation_observation.v1" || !canonicalUTCRFC3339NanoV1(core.RevalidationObservation.RecordedAt) ||
		len(core.RevalidationObservation.Evidence) != 3 || core.RevalidationObservation.Evidence[0] != intent.ExpectedAuthorizationScope ||
		core.RevalidationObservation.Evidence[1] != intent.ExpectedResourceSnapshot || core.RevalidationObservation.Evidence[2] != intent.ExpectedPolicySnapshot {
		return "STORED_CONSUMPTION_CORE_INVALID"
	}
	for _, evidence := range core.RevalidationObservation.Evidence {
		if !approvalConsumptionEvidenceValidV1(evidence) {
			return "STORED_CONSUMPTION_CORE_INVALID"
		}
	}
	recordedAt, _ := time.Parse(time.RFC3339Nano, core.RevalidationObservation.RecordedAt)
	expiresAt, _ := time.Parse(time.RFC3339Nano, intent.ApprovalExpiresAt)
	requestDigest, requestErr := digestValueV1(consumptionIntentDigestDomainV1, intent)
	coreDigest, coreErr := digestValueV1(consumptionCoreDigestDomainV1, core)
	if !recordedAt.Before(expiresAt) || requestErr != nil || requestDigest != core.RequestDigest || requestDigest != index.RequestDigest ||
		coreErr != nil || coreDigest != stored.CoreDigest || coreDigest != index.CoreDigest || intent.ApprovalID != index.ApprovalID || intent.ConsumptionKey != index.ConsumptionKey ||
		intent.UserID != index.UserID || intent.ProjectID != index.ProjectID || intent.SessionID != index.SessionID || intent.EffectKind != index.EffectKind {
		return "STORED_CONSUMPTION_CORE_INVALID"
	}
	return ""
}

func validateApprovalConsumptionQueryResultV1(expected approvalConsumptionQueryExpectedBindingV1, core approvalConsumptionReceiptCoreV1) string {
	if expected.SchemaVersion != "approval_consumption_query_expected_binding.v1" || !digestPattern.MatchString(expected.RequestDigest) ||
		!canonicalUUIDv7(expected.UserID) || !canonicalUUIDv7(expected.ProjectID) || !canonicalUUIDv7(expected.SessionID) || !canonicalUUIDv7(expected.ApprovalID) ||
		expected.EffectKind != "creation_spec_activation" || !canonicalUUIDv7(expected.ChildToolReceiptID) {
		return "CONSUMPTION_QUERY_BINDING_CONFLICT"
	}
	intent := core.IntentBinding
	if core.SchemaVersion != "approval_consumption_receipt_core.v1" || core.ReceiptVersion != 1 || core.WriteState != "recorded" || !canonicalUUIDv7(core.ReceiptID) ||
		intent.SchemaVersion != "approval_consumption_intent_binding.v1" || !digestPattern.MatchString(core.RequestDigest) {
		return "CONSUMPTION_QUERY_BINDING_CONFLICT"
	}
	if expected.RequestDigest != core.RequestDigest || expected.UserID != intent.UserID || expected.ProjectID != intent.ProjectID || expected.SessionID != intent.SessionID ||
		expected.ApprovalID != intent.ApprovalID || expected.EffectKind != intent.EffectKind || expected.ChildToolReceiptID != intent.ChildToolReceiptID {
		return "CONSUMPTION_QUERY_BINDING_CONFLICT"
	}
	return ""
}

func loadApprovalConsumptionManifestV1(t *testing.T) approvalConsumptionManifestV1 {
	t.Helper()
	raw, err := os.ReadFile(approvalConsumptionManifestPathV1)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("R04 manifest JSON 非法: %v", err)
	}
	var manifest approvalConsumptionManifestV1
	if err := strictDecode(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func loadApprovalConsumptionCorpusV1(t *testing.T) (approvalConsumptionCorpusV1, map[string]approvalConsumptionFixtureV1) {
	t.Helper()
	raw, err := os.ReadFile(approvalConsumptionCorpusPathV1)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("R04 corpus JSON 非法: %v", err)
	}
	var corpus approvalConsumptionCorpusV1
	if err := strictDecode(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.SchemaVersion != "approval_consumption_receipt_core_v1_corpus.v1" || len(corpus.Fixtures) != 4 || len(corpus.Cases) != approvalConsumptionVectorCountV1 {
		t.Fatalf("R04 corpus 版本或数量错误 version=%s fixtures=%d cases=%d", corpus.SchemaVersion, len(corpus.Fixtures), len(corpus.Cases))
	}
	fixtures := map[string]approvalConsumptionFixtureV1{}
	for _, definition := range corpus.Fixtures {
		var fixture approvalConsumptionFixtureV1
		switch {
		case definition.Value != nil && definition.BaseFixtureID == "":
			fixture = cloneApprovalConsumptionFixtureV1(*definition.Value)
		case definition.Value == nil && definition.BaseFixtureID != "":
			base, exists := fixtures[definition.BaseFixtureID]
			if !exists {
				t.Fatalf("fixture=%s base=%s 未先定义", definition.FixtureID, definition.BaseFixtureID)
			}
			fixture = cloneApprovalConsumptionFixtureV1(base)
		default:
			t.Fatalf("fixture=%s 必须且只能选择 value/base", definition.FixtureID)
		}
		for _, mutation := range definition.SetupMutations {
			applyApprovalConsumptionMutationV1(t, &fixture, mutation)
		}
		if _, duplicate := fixtures[definition.FixtureID]; duplicate {
			t.Fatalf("重复 fixture=%s", definition.FixtureID)
		}
		fixtures[definition.FixtureID] = fixture
	}
	return corpus, fixtures
}

func cloneApprovalConsumptionFixtureV1(fixture approvalConsumptionFixtureV1) approvalConsumptionFixtureV1 {
	raw, err := canonicalJSON(fixture)
	if err != nil {
		panic(err)
	}
	var cloned approvalConsumptionFixtureV1
	if err := strictDecode(raw, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func applyApprovalConsumptionMutationV1(t *testing.T, fixture *approvalConsumptionFixtureV1, mutation string) {
	t.Helper()
	intent := &fixture.RecordCommand.IntentBinding
	switch mutation {
	case "existing.recorded":
		core := buildApprovalConsumptionCoreV1(fixture.RecordCommand, fixture.FirstWriteMaterial)
		digest, err := digestValueV1(consumptionCoreDigestDomainV1, core)
		if err != nil {
			t.Fatal(err)
		}
		fixture.StoredCores = []approvalConsumptionStoredCoreV1{{Core: core, CoreDigest: digest}}
		fixture.StoredConsumptionIndex = []approvalConsumptionStoredIndexV1{approvalConsumptionStoredIndexFromCoreV1(core, digest)}
	case "current.all.drift":
		fixture.CurrentEligibility.AuthorizationScope.Digest = "sha256:" + strings.Repeat("4", 64)
		fixture.CurrentEligibility.ResourceVersion++
		fixture.CurrentEligibility.ResourceDigest = "sha256:" + strings.Repeat("5", 64)
		fixture.CurrentEligibility.TargetExactSetDigest = "sha256:" + strings.Repeat("6", 64)
		fixture.CurrentEligibility.PolicySnapshot.Digest = "sha256:" + strings.Repeat("7", 64)
		fixture.FirstWriteMaterial.RevalidationObservation.RecordedAt = "2026-07-17T08:00:00Z"
	case "state.rejected":
		fixture.DecisionAuthority.ApprovalState = "rejected"
		fixture.DecisionAuthority.FrozenIntent.DecisionAction = "reject"
		fixture.DecisionAuthority.FrozenIntent.ResultingState = "rejected"
		intent.DecisionAction = "reject"
		intent.ResultingState = "rejected"
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "state.expired":
		fixture.DecisionAuthority.ApprovalState = "expired"
		intent.DecisionAction = fixture.DecisionAuthority.FrozenIntent.DecisionAction
		intent.ResultingState = fixture.DecisionAuthority.FrozenIntent.ResultingState
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "state.cancelled":
		fixture.DecisionAuthority.ApprovalState = "cancelled"
		intent.DecisionAction = fixture.DecisionAuthority.FrozenIntent.DecisionAction
		intent.ResultingState = fixture.DecisionAuthority.FrozenIntent.ResultingState
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "recorded_at.after_expiry":
		fixture.FirstWriteMaterial.RevalidationObservation.RecordedAt = "2026-07-16T08:00:00Z"
	case "record.command.invalid":
		fixture.RecordCommand.Command = "delete"
	case "query.command.invalid":
		fixture.QueryCommand.Command = "record"
	case "record.key.invalid":
		intent.ConsumptionKey += ":other"
	case "query.key.invalid":
		fixture.QueryCommand.ConsumptionKey += ":other"
	case "identity.user.mismatch":
		fixture.RecordCommand.AuthenticatedIdentity.UserID = "019f4500-0000-7000-8000-000000000999"
	case "identity.project.mismatch":
		fixture.RecordCommand.AuthenticatedIdentity.ProjectID = "019f4500-0000-7000-8000-000000000998"
	case "identity.session.mismatch":
		fixture.RecordCommand.AuthenticatedIdentity.SessionID = "019f4500-0000-7000-8000-000000000997"
	case "identity.principal.mismatch":
		fixture.RecordCommand.AuthenticatedIdentity.AgentPrincipal = "agent.untrusted"
	case "query.identity.user.mismatch":
		fixture.QueryCommand.AuthenticatedIdentity.UserID = "019f4500-0000-7000-8000-000000000999"
	case "query.identity.session.mismatch":
		fixture.QueryCommand.AuthenticatedIdentity.SessionID = "019f4500-0000-7000-8000-000000000997"
	case "query.identity.principal.mismatch":
		fixture.QueryCommand.AuthenticatedIdentity.AgentPrincipal = "agent.untrusted"
	case "record.causal.approval_id.mismatch":
		intent.ApprovalID = "019f4500-0000-7000-8000-000000000899"
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "record.causal.decision_digest.mismatch":
		intent.DecisionDigest = "sha256:" + strings.Repeat("8", 64)
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "record.causal.source.mismatch":
		intent.ContinuationSourceID += ":other"
	case "record.causal.root_receipt.mismatch":
		intent.RootToolReceiptID = "019f4500-0000-7000-8000-000000000799"
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "record.causal.root_version.mismatch":
		intent.RootToolReceiptVersion++
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "record.causal.parent_result.mismatch":
		intent.ParentResultDigest = "sha256:" + strings.Repeat("8", 64)
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "record.causal.child_receipt.mismatch":
		intent.ChildToolReceiptID = "019f4500-0000-7000-8000-000000000795"
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "record.causal.source_formula.invalid":
		intent.ContinuationSourceID += ":invalid"
		fixture.DecisionAuthority.FrozenIntent.ContinuationSourceID = intent.ContinuationSourceID
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "record.causal.version_formula.invalid":
		intent.ResultingApprovalVersion = 3
		intent.ConsumptionKey = "approval-consumption:v1:3:creation_spec_activation"
		fixture.DecisionAuthority.FrozenIntent.ResultingApprovalVersion = 3
		fixture.DecisionAuthority.FrozenIntent.ConsumptionKey = intent.ConsumptionKey
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "record.expiry.noncanonical":
		intent.ApprovalExpiresAt = "2026-07-16T08:00:00+00:00"
		fixture.DecisionAuthority.FrozenIntent.ApprovalExpiresAt = intent.ApprovalExpiresAt
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "record.request_digest.invalid":
		fixture.RecordCommand.RequestDigest = "sha256:" + strings.Repeat("9", 64)
	case "record.intent.target_digest.rehash":
		intent.TargetExactSetDigest = "sha256:" + strings.Repeat("8", 64)
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "stored.key.different":
		fixture.StoredCores = nil
		fixture.StoredConsumptionIndex = []approvalConsumptionStoredIndexV1{{
			SchemaVersion: "approval_consumption_stored_index.v1", Owner: "agent.approval_store", ApprovalID: intent.ApprovalID,
			ConsumptionKey: "approval-consumption:v1:1:creation_spec_activation", RequestDigest: "sha256:" + strings.Repeat("1", 64),
			UserID: intent.UserID, ProjectID: intent.ProjectID, SessionID: intent.SessionID, EffectKind: intent.EffectKind,
			CoreDigest: "sha256:" + strings.Repeat("2", 64),
		}}
	case "stored.index.other.prepend":
		fixture.StoredConsumptionIndex = append([]approvalConsumptionStoredIndexV1{{
			SchemaVersion: "approval_consumption_stored_index.v1", Owner: "agent.approval_store", ApprovalID: "019f4500-0000-7000-8000-000000000123",
			ConsumptionKey: "approval-consumption:v1:1:creation_spec_activation", RequestDigest: "sha256:" + strings.Repeat("1", 64),
			UserID: intent.UserID, ProjectID: intent.ProjectID, SessionID: intent.SessionID, EffectKind: intent.EffectKind,
			CoreDigest: "sha256:" + strings.Repeat("2", 64),
		}}, fixture.StoredConsumptionIndex...)
	case "stored.index.exact.duplicate":
		fixture.StoredConsumptionIndex = append(fixture.StoredConsumptionIndex, fixture.StoredConsumptionIndex[0])
	case "stored.index.same_approval.other_key.append":
		fixture.StoredConsumptionIndex = append(fixture.StoredConsumptionIndex, approvalConsumptionStoredIndexV1{
			SchemaVersion: "approval_consumption_stored_index.v1", Owner: "agent.approval_store", ApprovalID: intent.ApprovalID,
			ConsumptionKey: "approval-consumption:v1:1:creation_spec_activation", RequestDigest: "sha256:" + strings.Repeat("1", 64),
			UserID: intent.UserID, ProjectID: intent.ProjectID, SessionID: intent.SessionID, EffectKind: intent.EffectKind,
			CoreDigest: "sha256:" + strings.Repeat("2", 64),
		})
	case "stored.core.exact.duplicate":
		fixture.StoredCores = append(fixture.StoredCores, fixture.StoredCores[0])
	case "observation.evidence.missing":
		fixture.FirstWriteMaterial.RevalidationObservation.Evidence = fixture.FirstWriteMaterial.RevalidationObservation.Evidence[:2]
	case "observation.evidence.extra":
		fixture.FirstWriteMaterial.RevalidationObservation.Evidence = append(fixture.FirstWriteMaterial.RevalidationObservation.Evidence, fixture.FirstWriteMaterial.RevalidationObservation.Evidence[2])
	case "observation.evidence.duplicate":
		fixture.FirstWriteMaterial.RevalidationObservation.Evidence[1] = fixture.FirstWriteMaterial.RevalidationObservation.Evidence[0]
	case "backstop.index.schema.invalid":
		fixture.StoredConsumptionIndex[0].SchemaVersion = "approval_consumption_stored_index.v2"
	case "backstop.scope.other":
		fixture.StoredConsumptionIndex[0].SessionID = "019f4500-0000-7000-8000-000000000997"
	case "stored.request_digest.tamper":
		fixture.StoredCores[0].Core.RequestDigest = "sha256:" + strings.Repeat("8", 64)
		fixture.StoredConsumptionIndex[0].RequestDigest = fixture.StoredCores[0].Core.RequestDigest
		rehashApprovalConsumptionStoredOuterV1(t, fixture)
	case "stored.observation.order.tamper":
		observation := fixture.StoredCores[0].Core.RevalidationObservation.Evidence
		observation[0], observation[1] = observation[1], observation[0]
		rehashApprovalConsumptionStoredOuterV1(t, fixture)
	case "stored.recorded_at.noncanonical":
		fixture.StoredCores[0].Core.RevalidationObservation.RecordedAt = "2026-07-15T08:00:00+00:00"
		rehashApprovalConsumptionStoredOuterV1(t, fixture)
	case "stored.core_digest.tamper":
		fixture.StoredCores[0].CoreDigest = "sha256:" + strings.Repeat("8", 64)
		fixture.StoredConsumptionIndex[0].CoreDigest = fixture.StoredCores[0].CoreDigest
	case "stored.intent.policy.invalid":
		fixture.StoredCores[0].Core.IntentBinding.ConsumptionPolicy = "multi_use"
		rehashApprovalConsumptionStoredSemanticV1(t, fixture)
	case "stored.scope.other":
		storedIntent := &fixture.StoredCores[0].Core.IntentBinding
		storedIntent.UserID = "019f4500-0000-7000-8000-000000000999"
		storedIntent.ExpectedAuthorizationScope.Ref = "project-scope:" + storedIntent.ProjectID + ":user:" + storedIntent.UserID
		fixture.StoredCores[0].Core.RevalidationObservation.Evidence[0].Ref = storedIntent.ExpectedAuthorizationScope.Ref
		rehashApprovalConsumptionStoredSemanticV1(t, fixture)
	case "stored.parent_owner_ref.tamper":
		fixture.StoredCores[0].Core.IntentBinding.ParentToolReceiptOwnerRef += ":other"
		rehashApprovalConsumptionStoredSemanticV1(t, fixture)
	case "stored.authorization_ref.tamper":
		fixture.StoredCores[0].Core.IntentBinding.ExpectedAuthorizationScope.Ref += ":other"
		fixture.StoredCores[0].Core.RevalidationObservation.Evidence[0].Ref += ":other"
		rehashApprovalConsumptionStoredSemanticV1(t, fixture)
	case "stored.resource_ref.tamper":
		fixture.StoredCores[0].Core.IntentBinding.ExpectedResourceSnapshot.Ref += ":other"
		fixture.StoredCores[0].Core.RevalidationObservation.Evidence[1].Ref += ":other"
		rehashApprovalConsumptionStoredSemanticV1(t, fixture)
	case "stored.policy_ref.tamper":
		fixture.StoredCores[0].Core.IntentBinding.ExpectedPolicySnapshot.Ref += ":other"
		fixture.StoredCores[0].Core.RevalidationObservation.Evidence[2].Ref += ":other"
		rehashApprovalConsumptionStoredSemanticV1(t, fixture)
	case "stored.tool_owner.tamper":
		fixture.StoredCores[0].Core.IntentBinding.ToolPinOwner = "agent.other_registry"
		rehashApprovalConsumptionStoredSemanticV1(t, fixture)
	case "stored.source_formula.tamper":
		fixture.StoredCores[0].Core.IntentBinding.ContinuationSourceID += ":other"
		rehashApprovalConsumptionStoredSemanticV1(t, fixture)
	case "stored.resulting_version.tamper":
		fixture.StoredCores[0].Core.IntentBinding.ResultingApprovalVersion = 3
		rehashApprovalConsumptionStoredSemanticV1(t, fixture)
	case "tool.pin_ref.drift":
		intent.ToolPinRef += ":other"
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "tool.intent_digest.drift":
		intent.IntentDigest = "sha256:" + strings.Repeat("a", 64)
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "tool.tuple.invalid":
		intent.ToolKey = "other_tool"
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "record.action.invalid":
		intent.Action = "prepare"
	case "record.resource_type.invalid":
		intent.ResourceType = "other_candidate"
	case "intent.expected_scope.rehash":
		digest := "sha256:" + strings.Repeat("4", 64)
		intent.ExpectedAuthorizationScope.Digest = digest
		fixture.FirstWriteMaterial.RevalidationObservation.Evidence[0].Digest = digest
		fixture.CurrentEligibility.AuthorizationScope.Digest = digest
		syncApprovalConsumptionRequestDigestV1(t, fixture)
	case "current.scope.drift":
		fixture.CurrentEligibility.AuthorizationScope.Digest = "sha256:" + strings.Repeat("4", 64)
	case "current.resource_version.drift":
		fixture.CurrentEligibility.ResourceVersion++
	case "current.resource_digest.drift":
		fixture.CurrentEligibility.ResourceDigest = "sha256:" + strings.Repeat("5", 64)
	case "current.target.drift":
		fixture.CurrentEligibility.TargetExactSetDigest = "sha256:" + strings.Repeat("6", 64)
	case "current.policy.drift":
		fixture.CurrentEligibility.PolicySnapshot.Digest = "sha256:" + strings.Repeat("7", 64)
	case "observation.order.invalid":
		fixture.FirstWriteMaterial.RevalidationObservation.Evidence[0], fixture.FirstWriteMaterial.RevalidationObservation.Evidence[1] = fixture.FirstWriteMaterial.RevalidationObservation.Evidence[1], fixture.FirstWriteMaterial.RevalidationObservation.Evidence[0]
	case "recorded_at.noncanonical":
		fixture.FirstWriteMaterial.RevalidationObservation.RecordedAt = "2026-07-15T08:00:00+00:00"
	case "first_write.receipt.invalid":
		fixture.FirstWriteMaterial.ReceiptID = "not-a-uuid"
	case "first_write.observation.invalid":
		fixture.FirstWriteMaterial.RevalidationObservation.SchemaVersion = "approval_consumption_revalidation_observation.v2"
	case "current.schema.invalid":
		fixture.CurrentEligibility.SchemaVersion = "approval_consumption_current_eligibility.v2"
	case "operational.schema.invalid":
		fixture.OperationalMetadata.SchemaVersion = "approval_consumption_operational_metadata.v2"
	case "authority.schema.invalid":
		fixture.DecisionAuthority.SchemaVersion = "approval_decision_authority.v2"
	case "authority.frozen.drift":
		fixture.DecisionAuthority.FrozenIntent.ParentResultDigest = "sha256:" + strings.Repeat("8", 64)
	case "query.expected.request.mismatch":
		fixture.QueryExpectedBinding.RequestDigest = "sha256:" + strings.Repeat("8", 64)
	case "query.expected.session.mismatch":
		fixture.QueryExpectedBinding.SessionID = "019f4500-0000-7000-8000-000000000997"
	case "query.expected.effect.mismatch":
		fixture.QueryExpectedBinding.EffectKind = "other_effect"
	case "operational.metadata.change":
		fixture.OperationalMetadata.TraceID = "trace-retry"
		fixture.OperationalMetadata.Attempt = 99
		fixture.OperationalMetadata.ProcessorInstance = "processor-z"
		fixture.OperationalMetadata.ReadAt = "2026-07-16T09:10:11Z"
		fixture.OperationalMetadata.ChildWriteState = "open"
		fixture.OperationalMetadata.CurrentFence = 999
		fixture.OperationalMetadata.ExpectedChildVersion = 88
	case "schema.unknown":
		fixture.SchemaVersion = "approval_consumption_fixture.v2"
	default:
		panic("未知 approval consumption mutation: " + mutation)
	}
}

func syncApprovalConsumptionRequestDigestV1(t *testing.T, fixture *approvalConsumptionFixtureV1) {
	t.Helper()
	digest, err := digestValueV1(consumptionIntentDigestDomainV1, fixture.RecordCommand.IntentBinding)
	if err != nil {
		t.Fatal(err)
	}
	fixture.RecordCommand.RequestDigest = digest
}

func rehashApprovalConsumptionStoredOuterV1(t *testing.T, fixture *approvalConsumptionFixtureV1) {
	t.Helper()
	digest, err := digestValueV1(consumptionCoreDigestDomainV1, fixture.StoredCores[0].Core)
	if err != nil {
		t.Fatal(err)
	}
	fixture.StoredCores[0].CoreDigest = digest
	fixture.StoredConsumptionIndex[0].CoreDigest = digest
}

func rehashApprovalConsumptionStoredSemanticV1(t *testing.T, fixture *approvalConsumptionFixtureV1) {
	t.Helper()
	core := &fixture.StoredCores[0].Core
	requestDigest, err := digestValueV1(consumptionIntentDigestDomainV1, core.IntentBinding)
	if err != nil {
		t.Fatal(err)
	}
	core.RequestDigest = requestDigest
	fixture.StoredConsumptionIndex[0] = approvalConsumptionStoredIndexFromCoreV1(*core, fixture.StoredCores[0].CoreDigest)
	rehashApprovalConsumptionStoredOuterV1(t, fixture)
}

func approvalConsumptionVectorIDsV1() []string {
	return []string{
		"ACR-N01-strict-malformed", "ACR-N02-strict-duplicate", "ACR-N03-strict-trailing", "ACR-N04-strict-unknown-field", "ACR-N05-strict-billing-field",
		"ACR-N06-record-command-invalid", "ACR-N07-query-command-invalid", "ACR-N08-key-derivation", "ACR-N09-identity-user", "ACR-N10-identity-project",
		"ACR-N11-causal-approval", "ACR-N12-causal-decision-digest", "ACR-N13-causal-source", "ACR-N14-request-digest", "ACR-N15-consumption-digest-conflict",
		"ACR-N16-single-use-different-key", "ACR-N17-rejected", "ACR-N18-expired-state", "ACR-N19-cancelled-state", "ACR-N20-recorded-at-expiry",
		"ACR-N21-tool-pin", "ACR-N22-intent-digest", "ACR-N23-scope-drift", "ACR-N24-resource-version-drift", "ACR-N25-resource-digest-drift",
		"ACR-N26-target-drift", "ACR-N27-policy-drift", "ACR-N28-observation-order", "ACR-N29-priority-command-key", "ACR-N30-priority-key-identity",
		"ACR-N31-priority-identity-causal", "ACR-N32-priority-causal-request", "ACR-N33-priority-request-existing", "ACR-N34-priority-existing-eligibility", "ACR-N35-priority-eligibility-tool",
		"ACR-N36-priority-tool-scope", "ACR-N37-priority-scope-resource", "ACR-N38-priority-resource-target", "ACR-N39-priority-target-policy", "ACR-N40-query-priority-identity-key",
		"ACR-N41-strict-nested-unknown", "ACR-N42-strict-intent-null", "ACR-N43-strict-first-write-null", "ACR-N44-strict-record-signature", "ACR-N45-strict-first-write-signature",
		"ACR-N46-record-principal", "ACR-N47-record-session", "ACR-N48-query-principal", "ACR-N49-causal-root-receipt", "ACR-N50-causal-root-version",
		"ACR-N51-causal-parent-result", "ACR-N52-causal-child-receipt", "ACR-N53-causal-source-formula", "ACR-N54-causal-version-formula", "ACR-N55-expiry-noncanonical",
		"ACR-N56-recorded-at-noncanonical", "ACR-N57-fixed-action", "ACR-N58-fixed-resource-type", "ACR-N59-fixed-tool-tuple", "ACR-N60-frozen-scope-before-current",
		"ACR-N61-stored-request-digest", "ACR-N62-stored-observation-order", "ACR-N63-stored-recorded-time", "ACR-N64-stored-core-digest", "ACR-N65-stored-policy-enum",
		"ACR-N66-stored-parent-owner-ref", "ACR-N67-stored-authorization-ref", "ACR-N68-stored-resource-ref", "ACR-N69-stored-policy-ref", "ACR-N70-stored-tool-owner",
		"ACR-N71-query-stored-before-scope", "ACR-N72-query-stored-scope", "ACR-N73-query-key-before-stored-scope", "ACR-N74-query-expected-request", "ACR-N75-query-expected-session",
		"ACR-N76-query-expected-effect", "ACR-N77-query-stored-validation", "ACR-N78-replay-ignores-receipt", "ACR-N79-replay-ignores-observation", "ACR-N80-replay-ignores-current-schema",
		"ACR-N81-replay-ignores-operational-schema", "ACR-N82-exact-lookup-order-independent", "ACR-N83-first-write-receipt-invalid", "ACR-N84-first-write-observation-invalid", "ACR-N85-record-stored-scope",
		"ACR-N86-record-stored-validation-before-scope", "ACR-N87-query-ignores-current-authority", "ACR-N88-query-binding-valid", "ACR-N89-backstop-index-malformed", "ACR-N90-backstop-cross-scope",
		"ACR-N91-backstop-before-current-drift",
		"ACR-N92-backstop-ignores-current-authority", "ACR-N93-replay-ignores-authority-schema", "ACR-N94-replay-ignores-authority-frozen-drift",
		"ACR-N95-stored-source-formula", "ACR-N96-stored-resulting-version", "ACR-N97-record-duplicate-exact-index", "ACR-N98-query-duplicate-exact-index",
		"ACR-N99-record-multiple-single-use-index", "ACR-N100-query-multiple-single-use-index", "ACR-N101-record-duplicate-core", "ACR-N102-query-duplicate-core",
		"ACR-N103-observation-evidence-missing", "ACR-N104-observation-evidence-extra", "ACR-N105-observation-evidence-duplicate",
		"ACR-P01-first-write", "ACR-P02-record-replay", "ACR-P03-query-found", "ACR-P04-replay-current-drift", "ACR-P05-query-not-found", "ACR-P06-operational-metadata-excluded",
	}
}
