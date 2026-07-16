// Package contract_test 只承载未 Approved 的 CreationSpec Candidate Decision 公共契约候选语料，
// 不提供 Business RPC、IDL、Repository、Migration、Runtime 或任何生产实现。
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
	creationSpecDecisionCorpusPathV1      = "testdata/w2_r04_creation_spec_decision/creation_spec_candidate_decision_v1.json"
	creationSpecDecisionManifestPathV1    = "testdata/w2_r04_creation_spec_decision/manifest.json"
	creationSpecDecisionCommandDomainV1   = "dora.creation_spec_candidate_decision_command.v1"
	creationSpecDecisionAuthorityDomainV1 = "dora.creation_spec_candidate_decision_authority.v1"
	creationSpecDecisionVectorCountV1     = 70
)

type creationSpecDecisionManifestV1 struct {
	SchemaVersion    string                     `json:"schema_version"`
	Files            []messageSetManifestFileV1 `json:"files"`
	FixtureIDs       []string                   `json:"fixture_ids"`
	VectorIDs        []string                   `json:"vector_ids"`
	TotalVectorCount int                        `json:"total_vector_count"`
	TargetTests      []string                   `json:"target_tests"`
}

type creationSpecDecisionCorpusV1 struct {
	SchemaVersion string                          `json:"schema_version"`
	Status        creationSpecDecisionStatusV1    `json:"status"`
	ExactSets     creationSpecDecisionExactSetsV1 `json:"exact_sets"`
	Fixture       creationSpecDecisionFixtureV1   `json:"fixture"`
	Cases         []creationSpecDecisionCaseV1    `json:"cases"`
}

type creationSpecDecisionStatusV1 struct {
	ApprovalState string `json:"approval_state"`
	ExecutionUse  string `json:"execution_use"`
	OwnerGate     string `json:"owner_gate"`
}

type creationSpecDecisionExactSetsV1 struct {
	Actions             []string `json:"actions"`
	QueryStates         []string `json:"query_states"`
	AuthorityOutcomes   []string `json:"authority_outcomes"`
	NotCommittedReasons []string `json:"not_committed_reasons"`
	CaseKinds           []string `json:"case_kinds"`
	EvaluationOutcomes  []string `json:"evaluation_outcomes"`
}

type creationSpecDecisionFixtureV1 struct {
	FixtureID                         string                                   `json:"fixture_id"`
	ApprovePreparedSlotAuthority      creationSpecDecisionPreparedSlotV1       `json:"approve_prepared_slot_authority"`
	RejectPreparedSlotAuthority       creationSpecDecisionPreparedSlotV1       `json:"reject_prepared_slot_authority"`
	AuthenticatedPrincipal            creationSpecDecisionPrincipalV1          `json:"authenticated_principal"`
	CandidateAuthority                creationSpecDecisionCandidateBeforeV1    `json:"candidate_authority"`
	AuthenticatedConsumptionAuthority creationSpecDecisionConsumptionBindingV1 `json:"authenticated_consumption_authority"`
	ApproveCommand                    creationSpecDecisionCommandV1            `json:"approve_command"`
	RejectCommand                     creationSpecDecisionCommandV1            `json:"reject_command"`
	ApproveRequestDigest              string                                   `json:"approve_request_digest"`
	RejectRequestDigest               string                                   `json:"reject_request_digest"`
	ApproveQuery                      creationSpecDecisionQueryV1              `json:"approve_query"`
	RejectQuery                       creationSpecDecisionQueryV1              `json:"reject_query"`
	ApproveCommitted                  creationSpecDecisionQueryResultV1        `json:"approve_committed"`
	RejectCommitted                   creationSpecDecisionQueryResultV1        `json:"reject_committed"`
	ApproveNotCommitted               creationSpecDecisionQueryResultV1        `json:"approve_not_committed"`
	ApproveNotFound                   creationSpecDecisionQueryResultV1        `json:"approve_not_found"`
	ApproveConflict                   creationSpecDecisionQueryResultV1        `json:"approve_conflict"`
}

// creationSpecDecisionPreparedSlotV1 是 Agent child ToolReceipt 的受信、非 wire prepared slot authority。
// child_tool_receipt_id 不得被复制进 Business command semantic fields。
type creationSpecDecisionPreparedSlotV1 struct {
	ChildToolReceiptID string `json:"child_tool_receipt_id"`
	RefSlot            string `json:"ref_slot"`
	Action             string `json:"action"`
	RequestDigest      string `json:"request_digest"`
	IdempotencyKey     string `json:"idempotency_key"`
	QueryContract      string `json:"query_contract"`
}

type creationSpecDecisionPrincipalV1 struct {
	UserID    string `json:"user_id"`
	ProjectID string `json:"project_id"`
}

type creationSpecDecisionDecisionV1 struct {
	DecisionReceiptID        string `json:"decision_receipt_id"`
	DecisionID               string `json:"decision_id"`
	DecisionDigest           string `json:"decision_digest"`
	ApprovalID               string `json:"approval_id"`
	PresentedApprovalVersion int64  `json:"presented_approval_version"`
	ResultingApprovalVersion int64  `json:"resulting_approval_version"`
	Action                   string `json:"action"`
	ActorUserID              string `json:"actor_user_id"`
	ActorProjectID           string `json:"actor_project_id"`
	CardID                   string `json:"card_id"`
	CardRevision             int64  `json:"card_revision"`
}

type creationSpecDecisionCandidateV1 struct {
	ResourceType             string `json:"resource_type"`
	CandidateID              string `json:"candidate_id"`
	ExpectedCandidateVersion int64  `json:"expected_candidate_version"`
	ExpectedCandidateDigest  string `json:"expected_candidate_digest"`
	TargetExactSetDigest     string `json:"target_exact_set_digest"`
}

type creationSpecDecisionToolBindingV1 struct {
	ToolKey             string `json:"tool_key"`
	DefinitionVersion   string `json:"definition_version"`
	IntentSchemaVersion string `json:"intent_schema_version"`
	ResultSchemaVersion string `json:"result_schema_version"`
	GraphKey            string `json:"graph_key"`
	ToolPinOwner        string `json:"tool_pin_owner"`
	ToolPinRef          string `json:"tool_pin_ref"`
	ToolPinDigest       string `json:"tool_pin_digest"`
	IntentDigest        string `json:"intent_digest"`
}

// creationSpecDecisionConsumptionBindingV1 是测试候选中的“已认证 authority reference”绑定。
// 它不决定生产环境最终采用 DB authority query 还是签名 envelope。
type creationSpecDecisionConsumptionBindingV1 struct {
	Owner             string `json:"owner"`
	AuthorityRef      string `json:"authority_ref"`
	SchemaVersion     string `json:"schema_version"`
	AuthorityDigest   string `json:"authority_digest"`
	ReceiptID         string `json:"receipt_id"`
	ReceiptVersion    int64  `json:"receipt_version"`
	ConsumptionKey    string `json:"consumption_key"`
	ConsumptionDigest string `json:"consumption_digest"`
	EffectKind        string `json:"effect_kind"`
}

// creationSpecDecisionCommandV1 用指针只表达严格的 action 联合类型：approve 必须有，reject 必须无。
type creationSpecDecisionCommandV1 struct {
	SchemaVersion               string                                    `json:"schema_version"`
	MethodKey                   string                                    `json:"method_key"`
	QueryContract               string                                    `json:"query_contract"`
	IdempotencyKey              string                                    `json:"idempotency_key"`
	Action                      string                                    `json:"action"`
	Principal                   creationSpecDecisionPrincipalV1           `json:"principal"`
	Decision                    creationSpecDecisionDecisionV1            `json:"decision"`
	Candidate                   creationSpecDecisionCandidateV1           `json:"candidate"`
	ToolBinding                 creationSpecDecisionToolBindingV1         `json:"tool_binding"`
	ConsumptionAuthorityBinding *creationSpecDecisionConsumptionBindingV1 `json:"consumption_authority_binding,omitempty"`
}

type creationSpecDecisionQueryV1 struct {
	SchemaVersion  string                          `json:"schema_version"`
	QueryContract  string                          `json:"query_contract"`
	Principal      creationSpecDecisionPrincipalV1 `json:"principal"`
	IdempotencyKey string                          `json:"idempotency_key"`
	RequestDigest  string                          `json:"request_digest"`
	Action         string                          `json:"action"`
	DecisionID     string                          `json:"decision_id"`
	CandidateID    string                          `json:"candidate_id"`
}

type creationSpecDecisionAuthorityV1 struct {
	SchemaVersion   string                              `json:"schema_version"`
	AuthorityDigest string                              `json:"authority_digest"`
	Core            creationSpecDecisionAuthorityCoreV1 `json:"core"`
}

type creationSpecDecisionAuthorityCoreV1 struct {
	AuthorityID          string                                  `json:"authority_id"`
	AuthorityVersion     int64                                   `json:"authority_version"`
	TransactionReceiptID string                                  `json:"transaction_receipt_id"`
	AuditedAt            string                                  `json:"audited_at"`
	IdempotencyKey       string                                  `json:"idempotency_key"`
	QueryContract        string                                  `json:"query_contract"`
	RequestDigest        string                                  `json:"request_digest"`
	Action               string                                  `json:"action"`
	Decision             creationSpecDecisionAuthorityDecisionV1 `json:"decision"`
	CandidateBefore      creationSpecDecisionCandidateBeforeV1   `json:"candidate_before"`
	Outcome              string                                  `json:"outcome"`
	Committed            *creationSpecDecisionCommittedV1        `json:"committed,omitempty"`
	NotCommitted         *creationSpecDecisionNotCommittedV1     `json:"not_committed,omitempty"`
}

type creationSpecDecisionAuthorityDecisionV1 struct {
	DecisionID               string `json:"decision_id"`
	DecisionDigest           string `json:"decision_digest"`
	ApprovalID               string `json:"approval_id"`
	PresentedApprovalVersion int64  `json:"presented_approval_version"`
	ResultingApprovalVersion int64  `json:"resulting_approval_version"`
}

type creationSpecDecisionCandidateBeforeV1 struct {
	ResourceType string `json:"resource_type"`
	CandidateID  string `json:"candidate_id"`
	State        string `json:"state"`
	Version      int64  `json:"version"`
	Digest       string `json:"digest"`
}

type creationSpecDecisionCandidateAfterV1 struct {
	ResourceType string `json:"resource_type"`
	CandidateID  string `json:"candidate_id"`
	State        string `json:"state"`
	Version      int64  `json:"version"`
	Digest       string `json:"digest"`
}

type creationSpecDecisionCommittedV1 struct {
	CandidateAfter              creationSpecDecisionCandidateAfterV1      `json:"candidate_after"`
	ConsumptionAuthorityBinding *creationSpecDecisionConsumptionBindingV1 `json:"consumption_authority_binding,omitempty"`
}

type creationSpecDecisionNotCommittedV1 struct {
	Reason string `json:"reason"`
}

type creationSpecDecisionQueryResultV1 struct {
	SchemaVersion  string                           `json:"schema_version"`
	QueryContract  string                           `json:"query_contract"`
	QueryState     string                           `json:"query_state"`
	IdempotencyKey string                           `json:"idempotency_key"`
	RequestDigest  string                           `json:"request_digest"`
	Authority      *creationSpecDecisionAuthorityV1 `json:"authority,omitempty"`
}

type creationSpecDecisionCaseV1 struct {
	ID        string                         `json:"id"`
	Kind      string                         `json:"kind"`
	Base      string                         `json:"base"`
	Mutations []string                       `json:"mutations"`
	Expected  creationSpecDecisionExpectedV1 `json:"expected"`
}

type creationSpecDecisionExpectedV1 struct {
	Outcome                  string `json:"outcome"`
	Code                     string `json:"code"`
	QueryState               string `json:"query_state"`
	QueryCount               int    `json:"query_count"`
	ResendAllowed            bool   `json:"resend_allowed"`
	TransportUnknownObserved bool   `json:"transport_unknown_observed"`
}

type creationSpecDecisionEvaluationV1 struct {
	Outcome                  string
	Code                     string
	QueryState               string
	QueryCount               int
	ResendAllowed            bool
	TransportUnknownObserved bool
}

type creationSpecDecisionScenarioV1 struct {
	Command                    creationSpecDecisionCommandV1
	ClaimedDigest              string
	OriginalDigest             string
	PreparedSlotAuthority      creationSpecDecisionPreparedSlotV1
	AuthenticatedPrincipal     creationSpecDecisionPrincipalV1
	CandidateAuthority         creationSpecDecisionCandidateBeforeV1
	ToolAuthority              creationSpecDecisionToolBindingV1
	ConsumptionAuthority       creationSpecDecisionConsumptionBindingV1
	Query                      *creationSpecDecisionQueryV1
	QueryResult                *creationSpecDecisionQueryResultV1
	StoredRequestDigest        string
	CommandRawKind             string
	QueryResultRawKind         string
	QueryTimedOut              bool
	ConcurrentTransportUnknown bool
	QueryCount                 int
	RehashAuthority            bool
	CorruptAuthorityDigest     bool
	DigestProbe                bool
}

func TestW2R04CreationSpecCandidateDecisionManifest(t *testing.T) {
	manifest := loadCreationSpecDecisionManifestV1(t)
	wantTests := []string{
		"TestW2R04CreationSpecCandidateDecisionManifest",
		"TestCreationSpecCandidateDecisionV1Corpus",
		"TestCreationSpecCandidateDecisionV1GoldenDigestAndIdempotency",
		"TestCreationSpecCandidateDecisionV1ApproveRejectUnion",
		"TestCreationSpecCandidateDecisionV1AuthorityOutcomes",
		"TestCreationSpecCandidateDecisionV1QueryUnknownOutcome",
		"TestCreationSpecCandidateDecisionV1ReasonPriority",
		"TestCreationSpecCandidateDecisionV1StrictJSON",
	}
	if manifest.SchemaVersion != "w2_r04_creation_spec_decision_manifest.v1" || manifest.TotalVectorCount != creationSpecDecisionVectorCountV1 || !reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("creation spec decision manifest 版本、数量或目标测试漂移: %+v", manifest)
	}
	actualTests := contractManifestTargetTestNamesV1(t, []string{"creation_spec_candidate_decision_v1_corpus_test.go"})
	sortedWant := append([]string(nil), wantTests...)
	sort.Strings(sortedWant)
	if !reflect.DeepEqual(actualTests, sortedWant) {
		t.Fatalf("creation spec decision target tests 未绑定 AST actual=%v want=%v", actualTests, sortedWant)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].File != filepath.Base(creationSpecDecisionCorpusPathV1) || manifest.Files[0].VectorCount != creationSpecDecisionVectorCountV1 {
		t.Fatalf("creation spec decision manifest files 非 exact-set: %+v", manifest.Files)
	}
	raw, err := os.ReadFile(creationSpecDecisionCorpusPathV1)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if got := "sha256:" + hex.EncodeToString(digest[:]); got != manifest.Files[0].SHA256 {
		t.Fatalf("creation spec decision corpus hash 漂移 got=%s want=%s", got, manifest.Files[0].SHA256)
	}
	corpus := loadCreationSpecDecisionCorpusV1(t)
	caseIDs := make([]string, 0, len(corpus.Cases))
	seen := make(map[string]struct{}, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		if _, duplicate := seen[testCase.ID]; duplicate {
			t.Fatalf("duplicate vector id=%s", testCase.ID)
		}
		seen[testCase.ID] = struct{}{}
		caseIDs = append(caseIDs, testCase.ID)
	}
	if !reflect.DeepEqual(manifest.FixtureIDs, []string{corpus.Fixture.FixtureID}) || !reflect.DeepEqual(manifest.VectorIDs, caseIDs) || len(caseIDs) != creationSpecDecisionVectorCountV1 {
		t.Fatalf("manifest 未 exact-set 绑定 fixture/vector ids")
	}
}

func TestCreationSpecCandidateDecisionV1Corpus(t *testing.T) {
	corpus := loadCreationSpecDecisionCorpusV1(t)
	validateCreationSpecDecisionCorpusHeaderV1(t, corpus)
	for _, testCase := range corpus.Cases {
		t.Run(testCase.ID, func(t *testing.T) {
			scenario := buildCreationSpecDecisionScenarioV1(t, corpus.Fixture, testCase.Base)
			for _, mutation := range testCase.Mutations {
				applyCreationSpecDecisionMutationV1(t, &scenario, corpus.Fixture, mutation)
			}
			finalizeCreationSpecDecisionScenarioV1(t, &scenario)
			actual := evaluateCreationSpecDecisionScenarioV1(t, scenario)
			want := creationSpecDecisionEvaluationV1{
				Outcome: testCase.Expected.Outcome, Code: testCase.Expected.Code,
				QueryState: testCase.Expected.QueryState, QueryCount: testCase.Expected.QueryCount,
				ResendAllowed:            testCase.Expected.ResendAllowed,
				TransportUnknownObserved: testCase.Expected.TransportUnknownObserved,
			}
			if actual != want {
				t.Fatalf("evaluation=%+v want=%+v", actual, want)
			}
		})
	}
}

func TestCreationSpecCandidateDecisionV1GoldenDigestAndIdempotency(t *testing.T) {
	fixture := loadCreationSpecDecisionCorpusV1(t).Fixture
	approveDigest := creationSpecDecisionCommandDigestV1(t, fixture.ApproveCommand)
	rejectDigest := creationSpecDecisionCommandDigestV1(t, fixture.RejectCommand)
	if approveDigest != fixture.ApproveRequestDigest || rejectDigest != fixture.RejectRequestDigest || approveDigest == rejectDigest {
		t.Fatalf("command golden digest 漂移 approve=%s reject=%s", approveDigest, rejectDigest)
	}
	approvePrepared := fixture.ApprovePreparedSlotAuthority
	rejectPrepared := fixture.RejectPreparedSlotAuthority
	if approvePrepared.ChildToolReceiptID == rejectPrepared.ChildToolReceiptID || approvePrepared.IdempotencyKey == rejectPrepared.IdempotencyKey {
		t.Fatal("approve/reject 必须来自不同 child receipt/prepared key")
	}
	if fixture.ApproveCommand.IdempotencyKey != creationSpecDecisionIdempotencyKeyV1(approvePrepared) ||
		fixture.RejectCommand.IdempotencyKey != creationSpecDecisionIdempotencyKeyV1(rejectPrepared) ||
		fixture.ApproveCommand.QueryContract != approvePrepared.QueryContract || fixture.RejectCommand.QueryContract != rejectPrepared.QueryContract ||
		approvePrepared.Action != "approve" || rejectPrepared.Action != "reject" ||
		approvePrepared.RequestDigest != approveDigest || rejectPrepared.RequestDigest != rejectDigest {
		t.Fatal("command 未逐值复用各自 child prepared business_decide slot authority")
	}
	approveRaw, _ := canonicalJSON(fixture.ApproveCommand)
	rejectRaw, _ := canonicalJSON(fixture.RejectCommand)
	approveObject, _ := creationSpecDecisionObjectV1(approveRaw)
	rejectObject, _ := creationSpecDecisionObjectV1(rejectRaw)
	if _, exists := approveObject["child_tool_receipt_id"]; exists {
		t.Fatal("Business command 不得携带 child_tool_receipt_id")
	}
	if _, exists := rejectObject["child_tool_receipt_id"]; exists {
		t.Fatal("Business command 不得携带 child_tool_receipt_id")
	}
	if strings.Contains(creationSpecDecisionCorpusPathV1, "business/") {
		t.Fatal("test-only corpus 不得落入 Business 生产目录")
	}
}

func TestCreationSpecCandidateDecisionV1ApproveRejectUnion(t *testing.T) {
	fixture := loadCreationSpecDecisionCorpusV1(t).Fixture
	approveRaw, _ := canonicalJSON(fixture.ApproveCommand)
	rejectRaw, _ := canonicalJSON(fixture.RejectCommand)
	if err := strictCreationSpecDecisionCommandV1(approveRaw); err != nil || fixture.ApproveCommand.ConsumptionAuthorityBinding == nil {
		t.Fatalf("approve 联合类型必须携带 consumption authority: %v", err)
	}
	if err := strictCreationSpecDecisionCommandV1(rejectRaw); err != nil || fixture.RejectCommand.ConsumptionAuthorityBinding != nil {
		t.Fatalf("reject 联合类型必须禁止 consumption authority: %v", err)
	}
	if fixture.ApproveCommand.Decision.ResultingApprovalVersion != fixture.ApproveCommand.Decision.PresentedApprovalVersion+1 {
		t.Fatal("Decision 必须绑定 presented/resulting approval version")
	}
}

func TestCreationSpecCandidateDecisionV1AuthorityOutcomes(t *testing.T) {
	fixture := loadCreationSpecDecisionCorpusV1(t).Fixture
	for name, result := range map[string]creationSpecDecisionQueryResultV1{
		"approve_committed":     fixture.ApproveCommitted,
		"reject_committed":      fixture.RejectCommitted,
		"approve_not_committed": fixture.ApproveNotCommitted,
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := canonicalJSON(result)
			if err := strictCreationSpecDecisionQueryResultV1(raw); err != nil {
				t.Fatal(err)
			}
			if result.Authority == nil || creationSpecDecisionAuthorityDigestV1(t, result.Authority.Core) != result.Authority.AuthorityDigest {
				t.Fatal("authority terminal envelope digest 漂移")
			}
		})
	}
}

func TestCreationSpecCandidateDecisionV1QueryUnknownOutcome(t *testing.T) {
	fixture := loadCreationSpecDecisionCorpusV1(t).Fixture
	for name, result := range map[string]creationSpecDecisionQueryResultV1{"not_found": fixture.ApproveNotFound, "conflict": fixture.ApproveConflict} {
		t.Run(name, func(t *testing.T) {
			if result.Authority != nil {
				t.Fatal("not_found/conflict 不得伪造 authority")
			}
			if name == "not_found" && result.QueryState != "not_found" {
				t.Fatal("not_found 漂移")
			}
		})
	}
	scenario := buildCreationSpecDecisionScenarioV1(t, fixture, "approve_not_found")
	actual := evaluateCreationSpecDecisionScenarioV1(t, scenario)
	if actual.Outcome != "unknown" || actual.ResendAllowed {
		t.Fatalf("not_found 必须保持 unknown 且禁止重发: %+v", actual)
	}
}

func TestCreationSpecCandidateDecisionV1ReasonPriority(t *testing.T) {
	corpus := loadCreationSpecDecisionCorpusV1(t)
	want := []string{"BSD-Q01-schema-before-identity", "BSD-Q02-identity-before-action", "BSD-Q03-action-before-candidate", "BSD-Q04-candidate-before-consumption", "BSD-Q05-consumption-before-digest", "BSD-Q06-authority-integrity-before-transport", "BSD-Q07-terminal-authority-before-transport", "BSD-Q08-not-found-with-transport-unknown"}
	actual := make([]string, 0, len(want))
	for _, testCase := range corpus.Cases {
		if testCase.Kind == "priority" {
			actual = append(actual, testCase.ID)
		}
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("stable priority vectors=%v want=%v", actual, want)
	}
}

func TestCreationSpecCandidateDecisionV1StrictJSON(t *testing.T) {
	corpus := loadCreationSpecDecisionCorpusV1(t)
	for _, command := range []creationSpecDecisionCommandV1{corpus.Fixture.ApproveCommand, corpus.Fixture.RejectCommand} {
		raw, _ := canonicalJSON(command)
		if err := strictCreationSpecDecisionCommandV1(raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []creationSpecDecisionQueryV1{corpus.Fixture.ApproveQuery, corpus.Fixture.RejectQuery} {
		raw, _ := canonicalJSON(query)
		if err := strictCreationSpecDecisionQueryV1(raw); err != nil {
			t.Fatal(err)
		}
	}
}

func validateCreationSpecDecisionCorpusHeaderV1(t *testing.T, corpus creationSpecDecisionCorpusV1) {
	t.Helper()
	if corpus.SchemaVersion != "creation_spec_candidate_decision_v1_corpus.v1" || corpus.Status.ApprovalState != "draft_awaiting_agent_business_security_owner" || corpus.Status.ExecutionUse != "test_only" || corpus.Status.OwnerGate != "production_forbidden_until_approved" {
		t.Fatalf("corpus status 非 Draft/test-only owner gate: %+v", corpus.Status)
	}
	want := creationSpecDecisionExactSetsV1{
		Actions: []string{"approve", "reject"}, QueryStates: []string{"conflict", "found", "not_found"},
		AuthorityOutcomes:   []string{"committed", "not_committed"},
		NotCommittedReasons: []string{"approval_invalid", "idempotency_conflict", "permission_denied", "version_conflict"},
		CaseKinds:           []string{"binding", "digest_sensitivity", "flow", "priority", "strict_json"},
		EvaluationOutcomes:  []string{"accepted", "digest_changed", "quarantined", "rejected", "resolved", "resolved_not_committed", "unknown"},
	}
	if !reflect.DeepEqual(corpus.ExactSets, want) || len(corpus.Cases) != creationSpecDecisionVectorCountV1 {
		t.Fatalf("corpus exact-sets/count 漂移: %+v count=%d", corpus.ExactSets, len(corpus.Cases))
	}
}

func buildCreationSpecDecisionScenarioV1(t *testing.T, fixture creationSpecDecisionFixtureV1, base string) creationSpecDecisionScenarioV1 {
	t.Helper()
	approve := strings.HasPrefix(base, "approve")
	command := fixture.RejectCommand
	claimed := fixture.RejectRequestDigest
	query := fixture.RejectQuery
	if approve {
		command = fixture.ApproveCommand
		claimed = fixture.ApproveRequestDigest
		query = fixture.ApproveQuery
	}
	prepared := fixture.RejectPreparedSlotAuthority
	if approve {
		prepared = fixture.ApprovePreparedSlotAuthority
	}
	scenario := creationSpecDecisionScenarioV1{
		Command:       cloneCreationSpecDecisionValueV1[creationSpecDecisionCommandV1](t, command),
		ClaimedDigest: claimed, OriginalDigest: claimed,
		PreparedSlotAuthority:  prepared,
		AuthenticatedPrincipal: fixture.AuthenticatedPrincipal,
		CandidateAuthority:     fixture.CandidateAuthority,
		ToolAuthority:          fixture.ApproveCommand.ToolBinding,
		ConsumptionAuthority:   fixture.AuthenticatedConsumptionAuthority,
	}
	switch base {
	case "approve_command", "reject_command":
	case "approve_found_committed":
		scenario.Query = pointerCreationSpecDecisionValueV1(t, query)
		scenario.QueryResult = pointerCreationSpecDecisionValueV1(t, fixture.ApproveCommitted)
		scenario.QueryCount = 1
	case "reject_found_committed":
		scenario.Query = pointerCreationSpecDecisionValueV1(t, query)
		scenario.QueryResult = pointerCreationSpecDecisionValueV1(t, fixture.RejectCommitted)
		scenario.QueryCount = 1
	case "approve_found_not_committed":
		scenario.Query = pointerCreationSpecDecisionValueV1(t, query)
		scenario.QueryResult = pointerCreationSpecDecisionValueV1(t, fixture.ApproveNotCommitted)
		scenario.QueryCount = 1
	case "approve_not_found":
		scenario.Query = pointerCreationSpecDecisionValueV1(t, query)
		scenario.QueryResult = pointerCreationSpecDecisionValueV1(t, fixture.ApproveNotFound)
		scenario.QueryCount = 1
	case "approve_conflict":
		scenario.Query = pointerCreationSpecDecisionValueV1(t, query)
		scenario.QueryResult = pointerCreationSpecDecisionValueV1(t, fixture.ApproveConflict)
		scenario.QueryCount = 1
	default:
		t.Fatalf("unknown base=%s", base)
	}
	return scenario
}

func applyCreationSpecDecisionMutationV1(t *testing.T, scenario *creationSpecDecisionScenarioV1, fixture creationSpecDecisionFixtureV1, mutation string) {
	t.Helper()
	switch mutation {
	case "raw:malformed", "raw:unknown", "raw:duplicate", "raw:null", "raw:trailing", "raw:missing":
		scenario.CommandRawKind = strings.TrimPrefix(mutation, "raw:")
	case "raw:query_unknown":
		scenario.QueryResultRawKind = "query_unknown"
	case "raw:authority_unknown":
		scenario.QueryResultRawKind = "authority_unknown"
	case "identity:user":
		scenario.Command.Principal.UserID = "019f4700-0000-7000-8000-000000009999"
	case "identity:project":
		scenario.Command.Principal.ProjectID = "019f4700-0000-7000-8000-000000009998"
	case "action:invalid":
		scenario.Command.Action = "activate"
	case "decision:action":
		scenario.Command.Decision.Action = oppositeCreationSpecDecisionActionV1(scenario.Command.Action)
	case "decision:id":
		scenario.Command.Decision.DecisionID = "not-a-uuid"
	case "decision:digest":
		scenario.Command.Decision.DecisionDigest = "sha256:short"
	case "approval:version":
		scenario.Command.Decision.ResultingApprovalVersion = scenario.Command.Decision.PresentedApprovalVersion
	case "candidate:id":
		scenario.Command.Candidate.CandidateID = "019f4700-0000-7000-8000-000000009997"
	case "candidate:version":
		scenario.Command.Candidate.ExpectedCandidateVersion++
	case "candidate:digest":
		scenario.Command.Candidate.ExpectedCandidateDigest = creationSpecDecisionRepeatedDigestV1("9")
	case "tool:pin":
		scenario.Command.ToolBinding.ToolPinDigest = creationSpecDecisionRepeatedDigestV1("8")
	case "approve:missing_consumption":
		scenario.Command.ConsumptionAuthorityBinding = nil
	case "reject:has_consumption":
		binding := fixture.AuthenticatedConsumptionAuthority
		scenario.Command.ConsumptionAuthorityBinding = &binding
	case "consumption:authority":
		if scenario.Command.ConsumptionAuthorityBinding != nil {
			scenario.Command.ConsumptionAuthorityBinding.AuthorityDigest = creationSpecDecisionRepeatedDigestV1("7")
		}
	case "claimed:digest":
		scenario.ClaimedDigest = creationSpecDecisionRepeatedDigestV1("6")
	case "idempotency:formula":
		scenario.Command.IdempotencyKey += ":second-key"
	case "idempotency:stored_conflict":
		scenario.StoredRequestDigest = creationSpecDecisionRepeatedDigestV1("5")
	case "prepared:action":
		scenario.PreparedSlotAuthority.Action = oppositeCreationSpecDecisionActionV1(scenario.PreparedSlotAuthority.Action)
	case "prepared:request_digest":
		scenario.PreparedSlotAuthority.RequestDigest = creationSpecDecisionRepeatedDigestV1("5")
	case "prepared:query_contract":
		scenario.PreparedSlotAuthority.QueryContract = "business.creation_spec_candidate_decision.query.v2"
	case "command:schema_switch":
		scenario.Command.SchemaVersion = "creation_spec_candidate_decision_command.v2"
	case "command:query_switch":
		scenario.Command.QueryContract = "business.creation_spec_candidate_decision.query.v2"
	case "sensitivity:identity":
		scenario.Command.Principal.UserID = "019f4700-0000-7000-8000-000000009991"
		scenario.Command.Decision.ActorUserID = scenario.Command.Principal.UserID
		scenario.AuthenticatedPrincipal = scenario.Command.Principal
		scenario.DigestProbe = true
	case "sensitivity:decision":
		scenario.Command.Decision.DecisionDigest = creationSpecDecisionRepeatedDigestV1("4")
		scenario.DigestProbe = true
	case "sensitivity:candidate":
		scenario.Command.Candidate.ExpectedCandidateVersion++
		scenario.CandidateAuthority.Version++
		scenario.DigestProbe = true
	case "sensitivity:tool":
		scenario.Command.ToolBinding.IntentDigest = creationSpecDecisionRepeatedDigestV1("3")
		scenario.ToolAuthority = scenario.Command.ToolBinding
		scenario.DigestProbe = true
	case "sensitivity:consumption":
		if scenario.Command.ConsumptionAuthorityBinding != nil {
			scenario.Command.ConsumptionAuthorityBinding.ConsumptionDigest = creationSpecDecisionRepeatedDigestV1("2")
			scenario.ConsumptionAuthority = *scenario.Command.ConsumptionAuthorityBinding
		}
		scenario.DigestProbe = true
	case "authority:digest":
		scenario.CorruptAuthorityDigest = true
	case "authority:request_digest":
		creationSpecDecisionMutationAuthorityV1(t, scenario).Core.RequestDigest = creationSpecDecisionRepeatedDigestV1("1")
		scenario.RehashAuthority = true
	case "authority:idempotency":
		creationSpecDecisionMutationAuthorityV1(t, scenario).Core.IdempotencyKey += ":other"
		scenario.RehashAuthority = true
	case "authority:action":
		creationSpecDecisionMutationAuthorityV1(t, scenario).Core.Action = oppositeCreationSpecDecisionActionV1(scenario.Command.Action)
		scenario.RehashAuthority = true
	case "authority:decision":
		creationSpecDecisionMutationAuthorityV1(t, scenario).Core.Decision.DecisionID = "019f4700-0000-7000-8000-000000009990"
		scenario.RehashAuthority = true
	case "authority:candidate_before":
		creationSpecDecisionMutationAuthorityV1(t, scenario).Core.CandidateBefore.Version++
		scenario.RehashAuthority = true
	case "authority:committed_state":
		creationSpecDecisionMutationCommittedV1(t, scenario).CandidateAfter.State = "reviewing"
		scenario.RehashAuthority = true
	case "authority:missing_consumption":
		creationSpecDecisionMutationCommittedV1(t, scenario).ConsumptionAuthorityBinding = nil
		scenario.RehashAuthority = true
	case "authority:reject_has_consumption":
		binding := fixture.AuthenticatedConsumptionAuthority
		creationSpecDecisionMutationCommittedV1(t, scenario).ConsumptionAuthorityBinding = &binding
		scenario.RehashAuthority = true
	case "authority:not_committed_reason":
		creationSpecDecisionMutationNotCommittedV1(t, scenario).Reason = "transport_timeout"
		scenario.RehashAuthority = true
	case "query_result:missing":
		scenario.QueryResult = nil
	case "query:found_missing_authority":
		if scenario.QueryResult != nil {
			scenario.QueryResult.Authority = nil
		}
	case "query:not_found_has_authority":
		if scenario.QueryResult != nil {
			scenario.QueryResult.Authority = pointerCreationSpecDecisionValueV1(t, *fixture.ApproveCommitted.Authority)
		}
	case "authority:committed_missing":
		creationSpecDecisionMutationAuthorityV1(t, scenario).Core.Committed = nil
		scenario.RehashAuthority = true
	case "authority:not_committed_missing":
		creationSpecDecisionMutationAuthorityV1(t, scenario).Core.NotCommitted = nil
		scenario.RehashAuthority = true
	case "authority:committed_has_not_committed":
		authority := creationSpecDecisionMutationAuthorityV1(t, scenario)
		authority.Core.NotCommitted = &creationSpecDecisionNotCommittedV1{Reason: "version_conflict"}
		scenario.RehashAuthority = true
	case "query:late_found":
		scenario.QueryResult = pointerCreationSpecDecisionValueV1(t, fixture.ApproveCommitted)
		scenario.QueryCount = 2
	case "query:timeout":
		scenario.QueryTimedOut = true
	case "authority:replay":
		scenario.QueryCount = 2
		scenario.ConcurrentTransportUnknown = true
	case "transport:unknown_observed":
		scenario.ConcurrentTransportUnknown = true
	default:
		t.Fatalf("unknown mutation=%s", mutation)
	}
}

func creationSpecDecisionMutationAuthorityV1(t *testing.T, scenario *creationSpecDecisionScenarioV1) *creationSpecDecisionAuthorityV1 {
	t.Helper()
	if scenario.QueryResult == nil || scenario.QueryResult.Authority == nil {
		t.Fatalf("mutation requires found authority")
	}
	return scenario.QueryResult.Authority
}

func creationSpecDecisionMutationCommittedV1(t *testing.T, scenario *creationSpecDecisionScenarioV1) *creationSpecDecisionCommittedV1 {
	t.Helper()
	authority := creationSpecDecisionMutationAuthorityV1(t, scenario)
	if authority.Core.Committed == nil {
		t.Fatalf("mutation requires committed authority")
	}
	return authority.Core.Committed
}

func creationSpecDecisionMutationNotCommittedV1(t *testing.T, scenario *creationSpecDecisionScenarioV1) *creationSpecDecisionNotCommittedV1 {
	t.Helper()
	authority := creationSpecDecisionMutationAuthorityV1(t, scenario)
	if authority.Core.NotCommitted == nil {
		t.Fatalf("mutation requires not_committed authority")
	}
	return authority.Core.NotCommitted
}

func finalizeCreationSpecDecisionScenarioV1(t *testing.T, scenario *creationSpecDecisionScenarioV1) {
	t.Helper()
	if scenario.DigestProbe {
		digest := creationSpecDecisionCommandDigestV1(t, scenario.Command)
		scenario.ClaimedDigest = digest
		scenario.PreparedSlotAuthority.RequestDigest = digest
	}
	if scenario.QueryResult == nil || scenario.QueryResult.Authority == nil {
		return
	}
	if scenario.RehashAuthority {
		scenario.QueryResult.Authority.AuthorityDigest = creationSpecDecisionAuthorityDigestV1(t, scenario.QueryResult.Authority.Core)
	}
	if scenario.CorruptAuthorityDigest {
		scenario.QueryResult.Authority.AuthorityDigest = creationSpecDecisionRepeatedDigestV1("0")
	}
}

func evaluateCreationSpecDecisionScenarioV1(t *testing.T, scenario creationSpecDecisionScenarioV1) creationSpecDecisionEvaluationV1 {
	t.Helper()
	evaluation := func(outcome, code, queryState string, queryCount int) creationSpecDecisionEvaluationV1 {
		return creationSpecDecisionEvaluationResultV1(outcome, code, queryState, queryCount, scenario.ConcurrentTransportUnknown)
	}
	commandRaw := creationSpecDecisionCommandRawV1(t, scenario.Command, scenario.CommandRawKind)
	if err := strictCreationSpecDecisionCommandV1(commandRaw); err != nil {
		return evaluation("rejected", "BUSINESS_DECIDE_SCHEMA_INVALID", "", 0)
	}
	if code := validateCreationSpecDecisionCommandV1(scenario); code != "" {
		return evaluation("rejected", code, "", 0)
	}
	digest := creationSpecDecisionCommandDigestV1(t, scenario.Command)
	if digest != scenario.ClaimedDigest {
		return evaluation("rejected", "BUSINESS_DECIDE_REQUEST_DIGEST_MISMATCH", "", 0)
	}
	if scenario.PreparedSlotAuthority.RequestDigest != digest {
		return evaluation("rejected", "BUSINESS_DECIDE_IDEMPOTENCY_CONFLICT", "", 0)
	}
	if scenario.StoredRequestDigest != "" && scenario.StoredRequestDigest != digest {
		return evaluation("rejected", "BUSINESS_DECIDE_IDEMPOTENCY_CONFLICT", "", 0)
	}
	if scenario.DigestProbe {
		if digest == scenario.OriginalDigest {
			return evaluation("rejected", "BUSINESS_DECIDE_DIGEST_NOT_SENSITIVE", "", 0)
		}
		return evaluation("digest_changed", "", "", 0)
	}
	if scenario.Query == nil {
		return evaluation("accepted", "", "", 0)
	}
	queryRaw, _ := canonicalJSON(scenario.Query)
	if err := strictCreationSpecDecisionQueryV1(queryRaw); err != nil || validateCreationSpecDecisionQueryV1(*scenario.Query, scenario.Command, digest) != "" {
		return evaluation("rejected", "BUSINESS_DECIDE_QUERY_INVALID", "", scenario.QueryCount)
	}
	if scenario.QueryTimedOut {
		return evaluation("unknown", "BUSINESS_DECIDE_UNKNOWN_OUTCOME", "", scenario.QueryCount)
	}
	if scenario.QueryResult == nil {
		return evaluation("rejected", "BUSINESS_DECIDE_QUERY_INVALID", "", scenario.QueryCount)
	}
	if code := validateCreationSpecDecisionQueryResultShapeV1(scenario.QueryResult); code != "" {
		return evaluation("rejected", code, scenario.QueryResult.QueryState, scenario.QueryCount)
	}
	resultRaw := creationSpecDecisionQueryResultRawV1(t, *scenario.QueryResult, scenario.QueryResultRawKind)
	if err := strictCreationSpecDecisionQueryResultV1(resultRaw); err != nil {
		return evaluation("rejected", "BUSINESS_DECIDE_SCHEMA_INVALID", "", scenario.QueryCount)
	}
	result := scenario.QueryResult
	if result.SchemaVersion != "creation_spec_candidate_decision_query_result.v1" || result.QueryContract != scenario.Query.QueryContract || result.IdempotencyKey != scenario.Query.IdempotencyKey || result.RequestDigest != digest {
		return evaluation("rejected", "BUSINESS_DECIDE_QUERY_INVALID", "", scenario.QueryCount)
	}
	switch result.QueryState {
	case "not_found":
		return evaluation("unknown", "BUSINESS_DECIDE_UNKNOWN_OUTCOME", "not_found", scenario.QueryCount)
	case "conflict":
		return evaluation("quarantined", "BUSINESS_DECIDE_QUERY_CONFLICT", "conflict", scenario.QueryCount)
	case "found":
		if code := validateCreationSpecDecisionAuthorityV1(t, *result.Authority, scenario.Command, scenario.PreparedSlotAuthority, scenario.CandidateAuthority, scenario.ConsumptionAuthority, digest); code != "" {
			return evaluation("rejected", code, "found", scenario.QueryCount)
		}
		if result.Authority.Core.Outcome == "not_committed" {
			return evaluation("resolved_not_committed", "", "found", scenario.QueryCount)
		}
		return evaluation("resolved", "", "found", scenario.QueryCount)
	default:
		return evaluation("rejected", "BUSINESS_DECIDE_QUERY_INVALID", "", scenario.QueryCount)
	}
}

func creationSpecDecisionEvaluationResultV1(outcome, code, queryState string, queryCount int, transportUnknownObserved bool) creationSpecDecisionEvaluationV1 {
	return creationSpecDecisionEvaluationV1{
		Outcome:                  outcome,
		Code:                     code,
		QueryState:               queryState,
		QueryCount:               queryCount,
		ResendAllowed:            creationSpecDecisionResendAllowedV1(queryState, outcome),
		TransportUnknownObserved: transportUnknownObserved,
	}
}

func creationSpecDecisionResendAllowedV1(queryState, outcome string) bool {
	// Candidate decision 没有任何允许 blind write replay 的 query state。未查询、timeout、
	// found、not_found 和 conflict 都只能接受首写、查询或人工恢复，不能生成第二次 write。
	switch queryState {
	case "", "found", "not_found", "conflict":
		return false
	default:
		return false
	}
}

func validateCreationSpecDecisionQueryResultShapeV1(result *creationSpecDecisionQueryResultV1) string {
	if result == nil {
		return "BUSINESS_DECIDE_QUERY_INVALID"
	}
	switch result.QueryState {
	case "found":
		if result.Authority == nil {
			return "BUSINESS_DECIDE_AUTHORITY_INVALID"
		}
		core := result.Authority.Core
		switch core.Outcome {
		case "committed":
			if core.Committed == nil || core.NotCommitted != nil {
				return "BUSINESS_DECIDE_AUTHORITY_INVALID"
			}
		case "not_committed":
			if core.NotCommitted == nil || core.Committed != nil {
				return "BUSINESS_DECIDE_AUTHORITY_INVALID"
			}
		}
	case "not_found", "conflict":
		if result.Authority != nil {
			return "BUSINESS_DECIDE_QUERY_INVALID"
		}
	}
	return ""
}

func validateCreationSpecDecisionCommandV1(scenario creationSpecDecisionScenarioV1) string {
	command := scenario.Command
	prepared := scenario.PreparedSlotAuthority
	if command.SchemaVersion != "creation_spec_candidate_decision_command.v1" || command.MethodKey != "decide_creation_spec_candidate.v1" || command.QueryContract != "business.creation_spec_candidate_decision.query.v1" {
		return "BUSINESS_DECIDE_SCHEMA_INVALID"
	}
	if command.Principal != scenario.AuthenticatedPrincipal || !canonicalUUIDv7(command.Principal.UserID) || !canonicalUUIDv7(command.Principal.ProjectID) {
		return "BUSINESS_DECIDE_IDENTITY_INVALID"
	}
	if command.Action != "approve" && command.Action != "reject" {
		return "BUSINESS_DECIDE_ACTION_INVALID"
	}
	decision := command.Decision
	if decision.Action != command.Action || !canonicalUUIDv7(decision.DecisionReceiptID) || !canonicalUUIDv7(decision.DecisionID) || !creationSpecDecisionSHA256V1(decision.DecisionDigest) || !canonicalUUIDv7(decision.ApprovalID) || !canonicalUUIDv7(decision.CardID) || !safePositiveIntegerV1(decision.CardRevision) || !safePositiveIntegerV1(decision.PresentedApprovalVersion) || decision.ResultingApprovalVersion != decision.PresentedApprovalVersion+1 || decision.ActorUserID != command.Principal.UserID || decision.ActorProjectID != command.Principal.ProjectID {
		return "BUSINESS_DECIDE_DECISION_INVALID"
	}
	candidate := command.Candidate
	if candidate.ResourceType != "creation_spec_candidate" || candidate.CandidateID != scenario.CandidateAuthority.CandidateID || candidate.ExpectedCandidateVersion != scenario.CandidateAuthority.Version || candidate.ExpectedCandidateDigest != scenario.CandidateAuthority.Digest || scenario.CandidateAuthority.State != "reviewing" || !creationSpecDecisionSHA256V1(candidate.TargetExactSetDigest) {
		return "BUSINESS_DECIDE_CANDIDATE_CONFLICT"
	}
	tool := command.ToolBinding
	if !reflect.DeepEqual(tool, scenario.ToolAuthority) || tool.ToolKey != "plan_creation_spec" || tool.DefinitionVersion != "plan_creation_spec.v1alpha1" || tool.IntentSchemaVersion != "plan_creation_spec_intent.v1" || tool.ResultSchemaVersion != "graph_tool_result.v1" || tool.GraphKey != "plan_creation_spec_graph_v1" || tool.ToolPinOwner != "agent.tool_registry" || tool.ToolPinRef == "" || !creationSpecDecisionSHA256V1(tool.ToolPinDigest) || !creationSpecDecisionSHA256V1(tool.IntentDigest) {
		return "BUSINESS_DECIDE_TOOL_BINDING_INVALID"
	}
	if command.Action == "approve" {
		if command.ConsumptionAuthorityBinding == nil {
			return "BUSINESS_DECIDE_ACTION_UNION_INVALID"
		}
		if !reflect.DeepEqual(*command.ConsumptionAuthorityBinding, scenario.ConsumptionAuthority) || !validateCreationSpecDecisionConsumptionBindingV1(*command.ConsumptionAuthorityBinding) {
			return "BUSINESS_DECIDE_CONSUMPTION_AUTHORITY_INVALID"
		}
	} else if command.ConsumptionAuthorityBinding != nil {
		return "BUSINESS_DECIDE_ACTION_UNION_INVALID"
	}
	if !canonicalUUIDv7(prepared.ChildToolReceiptID) || prepared.RefSlot != "business_decide" ||
		(prepared.Action != "approve" && prepared.Action != "reject") || !creationSpecDecisionSHA256V1(prepared.RequestDigest) ||
		prepared.QueryContract != "business.creation_spec_candidate_decision.query.v1" || prepared.IdempotencyKey != creationSpecDecisionIdempotencyKeyV1(prepared) ||
		prepared.Action != command.Action || prepared.QueryContract != command.QueryContract {
		return "BUSINESS_DECIDE_PREPARED_SLOT_INVALID"
	}
	if command.IdempotencyKey != prepared.IdempotencyKey {
		return "BUSINESS_DECIDE_IDEMPOTENCY_INVALID"
	}
	return ""
}

func validateCreationSpecDecisionQueryV1(query creationSpecDecisionQueryV1, command creationSpecDecisionCommandV1, digest string) string {
	if query.SchemaVersion != "creation_spec_candidate_decision_query.v1" || query.QueryContract != command.QueryContract || query.Principal != command.Principal || query.IdempotencyKey != command.IdempotencyKey || query.RequestDigest != digest || query.Action != command.Action || query.DecisionID != command.Decision.DecisionID || query.CandidateID != command.Candidate.CandidateID {
		return "BUSINESS_DECIDE_QUERY_INVALID"
	}
	return ""
}

func validateCreationSpecDecisionAuthorityV1(t *testing.T, authority creationSpecDecisionAuthorityV1, command creationSpecDecisionCommandV1, prepared creationSpecDecisionPreparedSlotV1, candidateAuthority creationSpecDecisionCandidateBeforeV1, consumptionAuthority creationSpecDecisionConsumptionBindingV1, digest string) string {
	t.Helper()
	if authority.SchemaVersion != "creation_spec_candidate_decision_authority.v1" || authority.AuthorityDigest != creationSpecDecisionAuthorityDigestV1(t, authority.Core) {
		return "BUSINESS_DECIDE_AUTHORITY_INVALID"
	}
	core := authority.Core
	if !canonicalUUIDv7(core.AuthorityID) || !safePositiveIntegerV1(core.AuthorityVersion) || !canonicalUUIDv7(core.TransactionReceiptID) || !canonicalUTCRFC3339NanoV1(core.AuditedAt) || core.IdempotencyKey != prepared.IdempotencyKey || core.IdempotencyKey != command.IdempotencyKey || core.QueryContract != prepared.QueryContract || core.QueryContract != command.QueryContract || core.RequestDigest != digest || core.Action != command.Action {
		return "BUSINESS_DECIDE_AUTHORITY_INVALID"
	}
	if core.Decision.DecisionID != command.Decision.DecisionID || core.Decision.DecisionDigest != command.Decision.DecisionDigest || core.Decision.ApprovalID != command.Decision.ApprovalID || core.Decision.PresentedApprovalVersion != command.Decision.PresentedApprovalVersion || core.Decision.ResultingApprovalVersion != command.Decision.ResultingApprovalVersion || core.CandidateBefore != candidateAuthority {
		return "BUSINESS_DECIDE_AUTHORITY_INVALID"
	}
	switch core.Outcome {
	case "committed":
		if core.Committed == nil || core.NotCommitted != nil {
			return "BUSINESS_DECIDE_AUTHORITY_INVALID"
		}
		after := core.Committed.CandidateAfter
		wantState := "rejected"
		if command.Action == "approve" {
			wantState = "active"
		}
		if after.ResourceType != "creation_spec_candidate" || after.CandidateID != command.Candidate.CandidateID || after.State != wantState || after.Version != candidateAuthority.Version+1 || !creationSpecDecisionSHA256V1(after.Digest) {
			return "BUSINESS_DECIDE_AUTHORITY_INVALID"
		}
		if command.Action == "approve" {
			if core.Committed.ConsumptionAuthorityBinding == nil || !reflect.DeepEqual(*core.Committed.ConsumptionAuthorityBinding, consumptionAuthority) {
				return "BUSINESS_DECIDE_AUTHORITY_INVALID"
			}
		} else if core.Committed.ConsumptionAuthorityBinding != nil {
			return "BUSINESS_DECIDE_AUTHORITY_INVALID"
		}
	case "not_committed":
		if core.NotCommitted == nil || core.Committed != nil || !creationSpecDecisionContainsV1([]string{"approval_invalid", "idempotency_conflict", "permission_denied", "version_conflict"}, core.NotCommitted.Reason) {
			return "BUSINESS_DECIDE_AUTHORITY_INVALID"
		}
	default:
		return "BUSINESS_DECIDE_AUTHORITY_INVALID"
	}
	return ""
}

func validateCreationSpecDecisionConsumptionBindingV1(binding creationSpecDecisionConsumptionBindingV1) bool {
	return binding.Owner == "agent.approval_store" && strings.HasPrefix(binding.AuthorityRef, "approval-consumption-authority:v1:") && binding.SchemaVersion == "approval_consumption_receipt_core.v1" && creationSpecDecisionSHA256V1(binding.AuthorityDigest) && canonicalUUIDv7(binding.ReceiptID) && safePositiveIntegerV1(binding.ReceiptVersion) && strings.HasPrefix(binding.ConsumptionKey, "approval-consumption:v1:") && creationSpecDecisionSHA256V1(binding.ConsumptionDigest) && binding.EffectKind == "creation_spec_activation"
}

func strictCreationSpecDecisionCommandV1(raw []byte) error {
	if err := inspectJSON(raw); err != nil {
		return err
	}
	var command creationSpecDecisionCommandV1
	if err := strictDecode(raw, &command); err != nil {
		return err
	}
	top, err := creationSpecDecisionObjectV1(raw)
	if err != nil {
		return err
	}
	fields := []string{"schema_version", "method_key", "query_contract", "idempotency_key", "action", "principal", "decision", "candidate", "tool_binding"}
	if _, exists := top["consumption_authority_binding"]; exists {
		fields = append(fields, "consumption_authority_binding")
	}
	if err := creationSpecDecisionExactFieldsV1(top, fields); err != nil {
		return err
	}
	if err := creationSpecDecisionNestedExactV1(top, "principal", []string{"user_id", "project_id"}); err != nil {
		return err
	}
	if err := creationSpecDecisionNestedExactV1(top, "decision", []string{"decision_receipt_id", "decision_id", "decision_digest", "approval_id", "presented_approval_version", "resulting_approval_version", "action", "actor_user_id", "actor_project_id", "card_id", "card_revision"}); err != nil {
		return err
	}
	if err := creationSpecDecisionNestedExactV1(top, "candidate", []string{"resource_type", "candidate_id", "expected_candidate_version", "expected_candidate_digest", "target_exact_set_digest"}); err != nil {
		return err
	}
	if err := creationSpecDecisionNestedExactV1(top, "tool_binding", []string{"tool_key", "definition_version", "intent_schema_version", "result_schema_version", "graph_key", "tool_pin_owner", "tool_pin_ref", "tool_pin_digest", "intent_digest"}); err != nil {
		return err
	}
	if _, exists := top["consumption_authority_binding"]; exists {
		return creationSpecDecisionNestedExactV1(top, "consumption_authority_binding", creationSpecDecisionConsumptionFieldsV1())
	}
	return nil
}

func strictCreationSpecDecisionQueryV1(raw []byte) error {
	if err := inspectJSON(raw); err != nil {
		return err
	}
	var query creationSpecDecisionQueryV1
	if err := strictDecode(raw, &query); err != nil {
		return err
	}
	top, err := creationSpecDecisionObjectV1(raw)
	if err != nil {
		return err
	}
	if err := creationSpecDecisionExactFieldsV1(top, []string{"schema_version", "query_contract", "principal", "idempotency_key", "request_digest", "action", "decision_id", "candidate_id"}); err != nil {
		return err
	}
	return creationSpecDecisionNestedExactV1(top, "principal", []string{"user_id", "project_id"})
}

func strictCreationSpecDecisionQueryResultV1(raw []byte) error {
	if err := inspectJSON(raw); err != nil {
		return err
	}
	var result creationSpecDecisionQueryResultV1
	if err := strictDecode(raw, &result); err != nil {
		return err
	}
	top, err := creationSpecDecisionObjectV1(raw)
	if err != nil {
		return err
	}
	fields := []string{"schema_version", "query_contract", "query_state", "idempotency_key", "request_digest"}
	if result.QueryState == "found" {
		fields = append(fields, "authority")
	}
	if err := creationSpecDecisionExactFieldsV1(top, fields); err != nil {
		return err
	}
	if result.QueryState == "found" {
		return strictCreationSpecDecisionAuthorityV1(top["authority"])
	}
	return nil
}

func strictCreationSpecDecisionAuthorityV1(raw []byte) error {
	var authority creationSpecDecisionAuthorityV1
	if err := strictDecode(raw, &authority); err != nil {
		return err
	}
	top, err := creationSpecDecisionObjectV1(raw)
	if err != nil {
		return err
	}
	if err := creationSpecDecisionExactFieldsV1(top, []string{"schema_version", "authority_digest", "core"}); err != nil {
		return err
	}
	core, err := creationSpecDecisionObjectV1(top["core"])
	if err != nil {
		return err
	}
	coreFields := []string{"authority_id", "authority_version", "transaction_receipt_id", "audited_at", "idempotency_key", "query_contract", "request_digest", "action", "decision", "candidate_before", "outcome"}
	if authority.Core.Outcome == "committed" {
		coreFields = append(coreFields, "committed")
	}
	if authority.Core.Outcome == "not_committed" {
		coreFields = append(coreFields, "not_committed")
	}
	if err := creationSpecDecisionExactFieldsV1(core, coreFields); err != nil {
		return err
	}
	if err := creationSpecDecisionNestedExactV1(core, "decision", []string{"decision_id", "decision_digest", "approval_id", "presented_approval_version", "resulting_approval_version"}); err != nil {
		return err
	}
	if err := creationSpecDecisionNestedExactV1(core, "candidate_before", []string{"resource_type", "candidate_id", "state", "version", "digest"}); err != nil {
		return err
	}
	if authority.Core.Outcome == "committed" {
		committed, err := creationSpecDecisionObjectV1(core["committed"])
		if err != nil {
			return err
		}
		fields := []string{"candidate_after"}
		if authority.Core.Committed != nil && authority.Core.Committed.ConsumptionAuthorityBinding != nil {
			fields = append(fields, "consumption_authority_binding")
		}
		if err := creationSpecDecisionExactFieldsV1(committed, fields); err != nil {
			return err
		}
		if err := creationSpecDecisionNestedExactV1(committed, "candidate_after", []string{"resource_type", "candidate_id", "state", "version", "digest"}); err != nil {
			return err
		}
		if _, exists := committed["consumption_authority_binding"]; exists {
			return creationSpecDecisionNestedExactV1(committed, "consumption_authority_binding", creationSpecDecisionConsumptionFieldsV1())
		}
	}
	if authority.Core.Outcome == "not_committed" {
		return creationSpecDecisionNestedExactV1(core, "not_committed", []string{"reason"})
	}
	return nil
}

func creationSpecDecisionCommandRawV1(t *testing.T, command creationSpecDecisionCommandV1, kind string) []byte {
	t.Helper()
	raw, err := canonicalJSON(command)
	if err != nil {
		t.Fatal(err)
	}
	switch kind {
	case "":
		return raw
	case "malformed":
		return []byte(`{"schema_version":`)
	case "unknown":
		return append(raw[:len(raw)-1], []byte(`,"unknown_field":"forbidden"}`)...)
	case "duplicate":
		return append(raw[:len(raw)-1], []byte(`,"action":"approve"}`)...)
	case "null":
		return []byte(strings.Replace(string(raw), `"method_key":"decide_creation_spec_candidate.v1"`, `"method_key":null`, 1))
	case "trailing":
		return append(raw, []byte(` {}`)...)
	case "missing":
		var value map[string]any
		_ = json.Unmarshal(raw, &value)
		delete(value, "method_key")
		mutated, _ := canonicalJSON(value)
		return mutated
	default:
		t.Fatalf("unknown raw command kind=%s", kind)
		return nil
	}
}

func creationSpecDecisionQueryResultRawV1(t *testing.T, result creationSpecDecisionQueryResultV1, kind string) []byte {
	t.Helper()
	raw, err := canonicalJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	if kind == "" {
		return raw
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	switch kind {
	case "query_unknown":
		value["unknown_field"] = "forbidden"
	case "authority_unknown":
		authority, ok := value["authority"].(map[string]any)
		if !ok {
			t.Fatal("authority_unknown requires object authority")
		}
		authority["unknown_field"] = "forbidden"
	default:
		t.Fatalf("unknown query raw kind=%s", kind)
	}
	mutated, _ := canonicalJSON(value)
	return mutated
}

func creationSpecDecisionAuthorityDigestV1(t *testing.T, core creationSpecDecisionAuthorityCoreV1) string {
	t.Helper()
	raw, err := canonicalJSON(core)
	if err != nil {
		t.Fatal(err)
	}
	return semanticDigest(creationSpecDecisionAuthorityDomainV1, raw)
}

func creationSpecDecisionCommandDigestV1(t *testing.T, command creationSpecDecisionCommandV1) string {
	t.Helper()
	raw, err := canonicalJSON(command)
	if err != nil {
		t.Fatal(err)
	}
	return semanticDigest(creationSpecDecisionCommandDomainV1, raw)
}

func creationSpecDecisionIdempotencyKeyV1(prepared creationSpecDecisionPreparedSlotV1) string {
	return "tr:" + prepared.ChildToolReceiptID + ":business_decide:v1"
}

func creationSpecDecisionConsumptionFieldsV1() []string {
	return []string{"owner", "authority_ref", "schema_version", "authority_digest", "receipt_id", "receipt_version", "consumption_key", "consumption_digest", "effect_kind"}
}

func creationSpecDecisionObjectV1(raw []byte) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return value, nil
}

func creationSpecDecisionNestedExactV1(parent map[string]json.RawMessage, field string, fields []string) error {
	raw, exists := parent[field]
	if !exists || string(raw) == "null" {
		return fmt.Errorf("required object=%s", field)
	}
	nested, err := creationSpecDecisionObjectV1(raw)
	if err != nil {
		return err
	}
	if err := creationSpecDecisionExactFieldsV1(nested, fields); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
}

func creationSpecDecisionExactFieldsV1(value map[string]json.RawMessage, fields []string) error {
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

func loadCreationSpecDecisionCorpusV1(t *testing.T) creationSpecDecisionCorpusV1 {
	t.Helper()
	raw, err := os.ReadFile(creationSpecDecisionCorpusPathV1)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatal(err)
	}
	var corpus creationSpecDecisionCorpusV1
	if err := strictDecode(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func loadCreationSpecDecisionManifestV1(t *testing.T) creationSpecDecisionManifestV1 {
	t.Helper()
	raw, err := os.ReadFile(creationSpecDecisionManifestPathV1)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatal(err)
	}
	var manifest creationSpecDecisionManifestV1
	if err := strictDecode(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func cloneCreationSpecDecisionValueV1[T any](t *testing.T, value T) T {
	t.Helper()
	raw, err := canonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone T
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func pointerCreationSpecDecisionValueV1[T any](t *testing.T, value T) *T {
	clone := cloneCreationSpecDecisionValueV1(t, value)
	return &clone
}

func creationSpecDecisionSHA256V1(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && strings.ToLower(value) == value
}

func creationSpecDecisionRepeatedDigestV1(value string) string {
	return "sha256:" + strings.Repeat(value, 64)
}

func creationSpecDecisionContainsV1(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func oppositeCreationSpecDecisionActionV1(action string) string {
	if action == "approve" {
		return "reject"
	}
	return "approve"
}
