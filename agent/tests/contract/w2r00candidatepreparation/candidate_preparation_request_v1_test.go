// Package w2r00candidatepreparation_test 验证 R00 候选准备请求只登记缺失输入，不产生候选、批准或实现解锁能力。
package w2r00candidatepreparation_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const (
	requestPathV1            = "docs/design/agent/approvals/w2-r00-candidate-preparation-requests/CPR-W2-R00-v1.json"
	matrixPathV1             = "docs/design/agent/w2-r00-owner-decision-matrix-v1.md"
	sourceContractPathV1     = "docs/design/cross-module/graph-execution-billing-contract-v1.md"
	gateManifestPathV1       = "docs/design/agent/approvals/w2-review-freeze-manifest.json"
	validatorPathV1          = "agent/tests/contract/w2r00candidatepreparation/candidate_preparation_request_v1_test.go"
	guardPathV1              = "agent/tests/contract/w2r00candidatepreparationguard/candidate_preparation_request_guard_v1_test.go"
	itemRequirementsSHA256V1 = "sha256:34c85a8d0f9c72df0eb1a52dee54a150175084a48b395f561a6049787e9f6f2b"
	blockStatementV1         = "本 CPR 只固定 R00-D01～D14 的 readiness 基线，以及 R00-D05/D07/D08/D09/D11/D13 仍缺少的版本化候选输入与关闭证据；它不提交任何 Policy 数值、Price/Model mapping、Provider 能力事实、ModelReceipt 字段、时钟阈值、slot ordinal、Owner 角色或 ballot 选项。R00 继续为 expansion_frozen、candidate_evidence 为空、freeze/reopen_exception 为空，禁止生成 DR-W2-R00-v1、canonical/IDL/vector/test manifest、Owner approval、Formal Freeze、W2-B0a/W2-B1 或生产实现。"
)

type requestV1 struct {
	SchemaVersion          string              `json:"schema_version"`
	RequestID              string              `json:"request_id"`
	RequestKind            string              `json:"request_kind"`
	Gate                   string              `json:"gate"`
	Status                 string              `json:"status"`
	ApprovalStatus         string              `json:"approval_status"`
	ImplementationStatus   string              `json:"implementation_status"`
	EvidenceStatus         string              `json:"evidence_status"`
	ImplementationUnlocked bool                `json:"implementation_unlocked"`
	BallotEnabled          bool                `json:"ballot_enabled"`
	OwnerRoleSetStatus     string              `json:"owner_role_set_status"`
	DecisionDocument       artifactRefV1       `json:"decision_document"`
	SourceContract         artifactRefV1       `json:"source_contract"`
	GateManifest           artifactRefV1       `json:"gate_manifest"`
	ValidatorSources       []artifactRefV1     `json:"validator_sources"`
	ReadinessBaseline      readinessBaselineV1 `json:"readiness_baseline"`
	Items                  []itemV1            `json:"items"`
	UnmetEvidence          []string            `json:"unmet_evidence"`
	BlockedProductionGates []string            `json:"blocked_production_gates"`
	ForbiddenCapabilities  []string            `json:"forbidden_capabilities"`
	BlockStatement         string              `json:"block_statement"`
}

type artifactRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type readinessBaselineV1 struct {
	ScopeDerivationPending            []string `json:"scope_derivation_pending"`
	AwaitingOwnerDecision             []string `json:"awaiting_owner_decision"`
	CandidateIncompleteNotBallotReady []string `json:"candidate_incomplete_not_ballot_ready"`
}

type itemV1 struct {
	DecisionID                string               `json:"decision_id"`
	MatrixRowID               string               `json:"matrix_row_id"`
	SourceOpenItemIDs         []string             `json:"source_open_item_ids"`
	ReadinessStatus           string               `json:"readiness_status"`
	CandidateSubmissionStatus string               `json:"candidate_submission_status"`
	PrerequisiteRefs          []string             `json:"prerequisite_refs"`
	CrossGateAlignmentRefs    []string             `json:"cross_gate_alignment_refs"`
	RequiredInputs            []requiredInputV1    `json:"required_inputs"`
	RequiredEvidence          []requiredEvidenceV1 `json:"required_evidence"`
	BlockCode                 string               `json:"block_code"`
}

type requiredInputV1 struct {
	InputID            string   `json:"input_id"`
	Kind               string   `json:"kind"`
	RequiredDimensions []string `json:"required_dimensions"`
	Status             string   `json:"status"`
}

type requiredEvidenceV1 struct {
	EvidenceID         string   `json:"evidence_id"`
	Kind               string   `json:"kind"`
	RequiredAssertions []string `json:"required_assertions"`
	Status             string   `json:"status"`
}

type gateManifestProjectionV1 struct {
	Gates []struct {
		Gate               string            `json:"gate"`
		Status             string            `json:"status"`
		RequiredOwnerRoles []string          `json:"required_owner_roles"`
		CandidateEvidence  []json.RawMessage `json:"candidate_evidence"`
		Freeze             json.RawMessage   `json:"freeze"`
		ReopenException    json.RawMessage   `json:"reopen_exception"`
		Blockers           []struct {
			Code string `json:"code"`
		} `json:"blockers"`
	} `json:"gates"`
}

type itemSpecV1 struct {
	DecisionID          string
	OpenItemID          string
	Prerequisites       []string
	CrossGateAlignments []string
	InputIDs            []string
	EvidenceIDs         []string
}

type sourceSpecV1 struct {
	Path        string
	PackageName string
	Imports     []string
}

var (
	shaPatternV1      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	decisionPatternV1 = regexp.MustCompile(`^R00-D(05|07|08|09|11|13)$`)
)

// TestW2R00CandidatePreparationRequestV1 固定六项 incomplete candidate 的输入要求，并证明 live Gate 仍失败关闭。
func TestW2R00CandidatePreparationRequestV1(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootV1(t)
	raw := readFileV1(t, repoRoot, requestPathV1)
	request, err := strictDecodeRequestV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifyRequestShapeV1(t, raw)
	verifyItemRequirementsDigestV1(t, raw)
	verifyIdentityAndStatusesV1(t, request)
	verifyArtifactRefV1(t, repoRoot, request.DecisionDocument, matrixPathV1)
	verifyArtifactRefV1(t, repoRoot, request.SourceContract, sourceContractPathV1)
	verifyArtifactRefV1(t, repoRoot, request.GateManifest, gateManifestPathV1)
	verifyValidatorPackagesV1(t, repoRoot, request.ValidatorSources)
	verifyReadinessBaselineV1(t, request.ReadinessBaseline)
	verifyItemsV1(t, request.Items)
	verifyMatrixAndSourceRowsV1(t, repoRoot, request.Items)
	verifyLiveGateV1(t, repoRoot)
	verifyEvidenceAndCapabilitiesV1(t, request)
	verifyForbiddenClaimsV1(t, raw)
}

// TestW2R00CandidatePreparationRequestV1RequirementMutation 固定 kind、dimension 与 assertion 的完整语义，禁止只保留 ID 的空壳。
func TestW2R00CandidatePreparationRequestV1RequirementMutation(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootV1(t)
	raw := readFileV1(t, repoRoot, requestPathV1)
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	mutations := map[string][]byte{
		"kind":      bytes.Replace(root["items"], []byte(`"typed_candidate_input"`), []byte(`"accepted_candidate_value"`), 1),
		"dimension": bytes.Replace(root["items"], []byte(`"currency_code"`), []byte(`"currency_code=dora_point"`), 1),
		"assertion": bytes.Replace(root["items"], []byte(`"cap_boundary_accept_reject"`), []byte(`"cap_is_approved"`), 1),
	}
	for name, mutated := range mutations {
		t.Run(name, func(t *testing.T) {
			if itemRequirementsDigestMatchesV1(mutated) {
				t.Fatalf("requirements digest 必须拒绝 %s mutation", name)
			}
		})
	}
}

// TestW2R00CandidatePreparationRequestV1StrictJSON 固定重复键、null、数字、未知字段和尾随值都失败关闭。
func TestW2R00CandidatePreparationRequestV1StrictJSON(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string][]byte{
		"unknown":   []byte(`{"schema_version":"x","future":true}`),
		"duplicate": []byte(`{"schema_version":"x","schema_version":"y"}`),
		"null":      []byte(`{"schema_version":null}`),
		"number":    []byte(`{"schema_version":"x","ordinal":1}`),
		"trailing":  []byte(`{"schema_version":"x"}{}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := strictDecodeRequestV1(raw); err == nil {
				t.Fatalf("R00 candidate preparation 必须拒绝 %s JSON", name)
			}
		})
	}
}

func verifyIdentityAndStatusesV1(t *testing.T, request requestV1) {
	t.Helper()
	if request.SchemaVersion != "w2_r00_candidate_preparation_request.v1" || request.RequestID != "CPR-W2-R00-v1" || request.RequestKind != "candidate_input_requirements_only" || request.Gate != "W2-R00" {
		t.Fatalf("R00 candidate preparation identity 非法: %+v", request)
	}
	if request.Status != "readiness_inventory_only" || request.ApprovalStatus != "not_requested" || request.ImplementationStatus != "prohibited" || request.EvidenceStatus != "candidate_only" {
		t.Fatalf("R00 candidate preparation 状态非法: %+v", request)
	}
	if request.ImplementationUnlocked || request.BallotEnabled || request.OwnerRoleSetStatus != "scope_not_derived" {
		t.Fatalf("R00 candidate preparation 不得启用 ballot、实现或最终 Owner: %+v", request)
	}
	if request.BlockStatement != blockStatementV1 {
		t.Fatalf("block_statement 漂移: %s", request.BlockStatement)
	}
}

func verifyReadinessBaselineV1(t *testing.T, baseline readinessBaselineV1) {
	t.Helper()
	if want := []string{"R00-D01"}; !reflect.DeepEqual(baseline.ScopeDerivationPending, want) {
		t.Fatalf("scope readiness=%v want=%v", baseline.ScopeDerivationPending, want)
	}
	if want := []string{"R00-D02", "R00-D03", "R00-D04", "R00-D06", "R00-D10", "R00-D12", "R00-D14"}; !reflect.DeepEqual(baseline.AwaitingOwnerDecision, want) {
		t.Fatalf("awaiting readiness=%v want=%v", baseline.AwaitingOwnerDecision, want)
	}
	if want := []string{"R00-D05", "R00-D07", "R00-D08", "R00-D09", "R00-D11", "R00-D13"}; !reflect.DeepEqual(baseline.CandidateIncompleteNotBallotReady, want) {
		t.Fatalf("incomplete readiness=%v want=%v", baseline.CandidateIncompleteNotBallotReady, want)
	}
	all := append([]string{}, baseline.ScopeDerivationPending...)
	all = append(all, baseline.AwaitingOwnerDecision...)
	all = append(all, baseline.CandidateIncompleteNotBallotReady...)
	sort.Strings(all)
	wantAll := make([]string, 14)
	for index := range wantAll {
		wantAll[index] = fmt.Sprintf("R00-D%02d", index+1)
	}
	if !reflect.DeepEqual(all, wantAll) {
		t.Fatalf("R00 readiness baseline 未覆盖 14 项 exact-set: %v", all)
	}
}

func verifyItemsV1(t *testing.T, items []itemV1) {
	t.Helper()
	specs := itemSpecsV1()
	if len(items) != len(specs) {
		t.Fatalf("R00 incomplete items=%d want=%d", len(items), len(specs))
	}
	for index, spec := range specs {
		item := items[index]
		if item.DecisionID != spec.DecisionID || item.MatrixRowID != spec.DecisionID || !decisionPatternV1.MatchString(item.DecisionID) {
			t.Fatalf("item[%d] decision/matrix identity 非法: %+v", index, item)
		}
		if !reflect.DeepEqual(item.SourceOpenItemIDs, []string{spec.OpenItemID}) || !reflect.DeepEqual(item.PrerequisiteRefs, spec.Prerequisites) || !reflect.DeepEqual(item.CrossGateAlignmentRefs, spec.CrossGateAlignments) {
			t.Fatalf("%s source/dependency 非法: %+v", item.DecisionID, item)
		}
		if item.ReadinessStatus != "candidate_incomplete_not_ballot_ready" || item.CandidateSubmissionStatus != "missing_not_submitted" || item.BlockCode != fmt.Sprintf("W2_R00_D%s_CANDIDATE_INPUTS_MISSING", strings.TrimPrefix(item.DecisionID, "R00-D")) {
			t.Fatalf("%s readiness/block 非法: %+v", item.DecisionID, item)
		}
		if got := inputIDsV1(item.RequiredInputs); !reflect.DeepEqual(got, spec.InputIDs) {
			t.Fatalf("%s input ids=%v want=%v", item.DecisionID, got, spec.InputIDs)
		}
		if got := evidenceIDsV1(item.RequiredEvidence); !reflect.DeepEqual(got, spec.EvidenceIDs) {
			t.Fatalf("%s evidence ids=%v want=%v", item.DecisionID, got, spec.EvidenceIDs)
		}
		for _, input := range item.RequiredInputs {
			if input.Kind == "" || input.Status != "missing_not_submitted" {
				t.Fatalf("%s input status/kind 非法: %+v", item.DecisionID, input)
			}
			validateSortedUniqueStringsV1(t, input.InputID+" dimensions", input.RequiredDimensions, true)
		}
		for _, evidence := range item.RequiredEvidence {
			if evidence.Kind == "" || evidence.Status != "missing_not_submitted" {
				t.Fatalf("%s evidence status/kind 非法: %+v", item.DecisionID, evidence)
			}
			validateSortedUniqueStringsV1(t, evidence.EvidenceID+" assertions", evidence.RequiredAssertions, true)
		}
		validateSortedUniqueStringsV1(t, item.DecisionID+" prerequisites", item.PrerequisiteRefs, false)
		validateSortedUniqueStringsV1(t, item.DecisionID+" cross-gate alignments", item.CrossGateAlignmentRefs, false)
	}
}

func inputIDsV1(inputs []requiredInputV1) []string {
	ids := make([]string, 0, len(inputs))
	for _, input := range inputs {
		ids = append(ids, input.InputID)
	}
	return ids
}

func evidenceIDsV1(evidence []requiredEvidenceV1) []string {
	ids := make([]string, 0, len(evidence))
	for _, item := range evidence {
		ids = append(ids, item.EvidenceID)
	}
	return ids
}

func verifyMatrixAndSourceRowsV1(t *testing.T, repoRoot string, items []itemV1) {
	t.Helper()
	matrix := string(readFileV1(t, repoRoot, matrixPathV1))
	source := string(readFileV1(t, repoRoot, sourceContractPathV1))
	for _, item := range items {
		rowFragment := fmt.Sprintf("| `%s` | `%s` | `candidate_incomplete_not_ballot_ready`", item.DecisionID, item.SourceOpenItemIDs[0])
		if item.DecisionID == "R00-D07" {
			rowFragment = "| `R00-D07` | `BILL-OPEN-005` | `candidate_incomplete_not_ballot_ready`"
		}
		if !strings.Contains(matrix, rowFragment) {
			t.Fatalf("matrix 缺少 readiness row %q", rowFragment)
		}
		if !strings.Contains(source, "| `"+item.SourceOpenItemIDs[0]+"` |") {
			t.Fatalf("Billing source 缺少 %s", item.SourceOpenItemIDs[0])
		}
	}
	for _, fragment := range []string{
		"`R00-D01` | P4-C11 / Gate | `scope_derivation_pending`",
		"`R00-D06` | `BILL-OPEN-005` / P4-C11 | `awaiting_owner_decision`",
		"源契约的 Owner 在本决定被接受前继续保持“未登记、不得预填”",
		"当前不得生成 `DR-W2-R00-v1`",
	} {
		if !strings.Contains(matrix, fragment) {
			t.Fatalf("matrix 缺少失败关闭边界 %q", fragment)
		}
	}
}

func verifyLiveGateV1(t *testing.T, repoRoot string) {
	t.Helper()
	raw := readFileV1(t, repoRoot, gateManifestPathV1)
	var manifest gateManifestProjectionV1
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, gate := range manifest.Gates {
		if gate.Gate != "W2-R00" {
			continue
		}
		if gate.Status != "expansion_frozen" || !isJSONNullV1(gate.Freeze) || !isJSONNullV1(gate.ReopenException) || len(gate.CandidateEvidence) != 0 {
			t.Fatalf("R00 live Gate 不得前移或登记 candidate: %+v", gate)
		}
		wantRoles := []string{"agent_owner", "business_owner", "finance_owner", "product_owner", "security_owner"}
		if !reflect.DeepEqual(gate.RequiredOwnerRoles, wantRoles) {
			t.Fatalf("R00 live provisional roles=%v want=%v", gate.RequiredOwnerRoles, wantRoles)
		}
		var blockerCodes []string
		for _, blocker := range gate.Blockers {
			blockerCodes = append(blockerCodes, blocker.Code)
		}
		wantBlockers := []string{"W2_R00_BILLING_REVIEW_PENDING", "W2_R00_OWNER_APPROVAL_MISSING"}
		if !reflect.DeepEqual(blockerCodes, wantBlockers) {
			t.Fatalf("R00 live blockers=%v want=%v", blockerCodes, wantBlockers)
		}
		return
	}
	t.Fatal("review freeze manifest 缺少 W2-R00")
}

func verifyEvidenceAndCapabilitiesV1(t *testing.T, request requestV1) {
	t.Helper()
	wantEvidence := []string{
		"BILLING_SLOT_ORDINALS_AND_SCOPE_MISSING",
		"CLOCK_TOLERANCE_AND_RECOVERY_RULES_MISSING",
		"FINAL_OWNER_ROLE_EXACT_SET_MISSING",
		"MODEL_RECEIPT_SCHEMA_AND_DIGEST_MISSING",
		"OWNER_DECISIONS_PENDING",
		"POLICY_EXACT_VALUES_AND_LIFECYCLE_MISSING",
		"PRICE_MODEL_MAPPING_EXACT_SET_MISSING",
		"PROVIDER_CAPABILITY_MATRIX_MISSING",
		"R00_CANONICAL_CANDIDATE_MISSING",
	}
	if !reflect.DeepEqual(request.UnmetEvidence, wantEvidence) {
		t.Fatalf("unmet evidence=%v want=%v", request.UnmetEvidence, wantEvidence)
	}
	if want := []string{"W2-B0a", "W2-B1"}; !reflect.DeepEqual(request.BlockedProductionGates, want) {
		t.Fatalf("blocked production gates=%v want=%v", request.BlockedProductionGates, want)
	}
	wantCapabilities := []string{
		"accept_recommendation",
		"authorize_production",
		"create_canonical_manifest",
		"create_idl_manifest",
		"create_vector_test_exact_set",
		"derive_final_owner_exact_set",
		"freeze_review",
		"record_owner_approval",
		"record_platform_review",
		"register_candidate_evidence",
		"reject_recommendation",
		"select_owner_option",
		"transition_gate",
		"unlock_implementation",
	}
	if !reflect.DeepEqual(request.ForbiddenCapabilities, wantCapabilities) {
		t.Fatalf("forbidden capabilities=%v want=%v", request.ForbiddenCapabilities, wantCapabilities)
	}
}

func verifyRequestShapeV1(t *testing.T, raw []byte) {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	requireExactKeysV1(t, "request", root, []string{
		"approval_status", "ballot_enabled", "block_statement", "blocked_production_gates", "decision_document",
		"evidence_status", "forbidden_capabilities", "gate", "gate_manifest", "implementation_status",
		"implementation_unlocked", "items", "owner_role_set_status", "readiness_baseline", "request_id",
		"request_kind", "schema_version", "source_contract", "status", "unmet_evidence", "validator_sources",
	})
	for _, key := range []string{"decision_document", "source_contract", "gate_manifest"} {
		verifyArtifactShapeV1(t, key, root[key])
	}
	var sources []json.RawMessage
	if err := json.Unmarshal(root["validator_sources"], &sources); err != nil || len(sources) != 2 {
		t.Fatalf("validator_sources 必须是 two-source array: %v", err)
	}
	for index, source := range sources {
		verifyArtifactShapeV1(t, fmt.Sprintf("validator_sources[%d]", index), source)
	}
	var baseline map[string]json.RawMessage
	if err := json.Unmarshal(root["readiness_baseline"], &baseline); err != nil {
		t.Fatal(err)
	}
	requireExactKeysV1(t, "readiness_baseline", baseline, []string{"awaiting_owner_decision", "candidate_incomplete_not_ballot_ready", "scope_derivation_pending"})
	var items []json.RawMessage
	if err := json.Unmarshal(root["items"], &items); err != nil || len(items) != 6 {
		t.Fatalf("items 必须是 six-item array: %v", err)
	}
	for index, itemRaw := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(itemRaw, &item); err != nil {
			t.Fatal(err)
		}
		requireExactKeysV1(t, fmt.Sprintf("items[%d]", index), item, []string{
			"block_code", "candidate_submission_status", "cross_gate_alignment_refs", "decision_id", "matrix_row_id",
			"prerequisite_refs", "readiness_status", "required_evidence", "required_inputs", "source_open_item_ids",
		})
		verifyNestedRequirementShapesV1(t, fmt.Sprintf("items[%d]", index), item)
	}
}

func verifyNestedRequirementShapesV1(t *testing.T, label string, item map[string]json.RawMessage) {
	t.Helper()
	var inputs []map[string]json.RawMessage
	if err := json.Unmarshal(item["required_inputs"], &inputs); err != nil || len(inputs) == 0 {
		t.Fatalf("%s required_inputs 非法: %v", label, err)
	}
	for index, input := range inputs {
		requireExactKeysV1(t, fmt.Sprintf("%s.required_inputs[%d]", label, index), input, []string{"input_id", "kind", "required_dimensions", "status"})
	}
	var evidence []map[string]json.RawMessage
	if err := json.Unmarshal(item["required_evidence"], &evidence); err != nil || len(evidence) == 0 {
		t.Fatalf("%s required_evidence 非法: %v", label, err)
	}
	for index, item := range evidence {
		requireExactKeysV1(t, fmt.Sprintf("%s.required_evidence[%d]", label, index), item, []string{"evidence_id", "kind", "required_assertions", "status"})
	}
}

func verifyArtifactShapeV1(t *testing.T, label string, raw json.RawMessage) {
	t.Helper()
	var artifact map[string]json.RawMessage
	if err := json.Unmarshal(raw, &artifact); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
	requireExactKeysV1(t, label, artifact, []string{"path", "sha256"})
}

func requireExactKeysV1(t *testing.T, label string, object map[string]json.RawMessage, want []string) {
	t.Helper()
	actual := make([]string, 0, len(object))
	for key := range object {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	want = append([]string(nil), want...)
	sort.Strings(want)
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("%s keys=%v want=%v", label, actual, want)
	}
}

func verifyItemRequirementsDigestV1(t *testing.T, raw []byte) {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if !itemRequirementsDigestMatchesV1(root["items"]) {
		compact := compactJSONV1(t, root["items"])
		digest := sha256.Sum256(compact)
		t.Fatalf("items semantic SHA-256=sha256:%s want=%s", hex.EncodeToString(digest[:]), itemRequirementsSHA256V1)
	}
}

func itemRequirementsDigestMatchesV1(raw json.RawMessage) bool {
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return false
	}
	digest := sha256.Sum256(compact.Bytes())
	return "sha256:"+hex.EncodeToString(digest[:]) == itemRequirementsSHA256V1
}

func compactJSONV1(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatal(err)
	}
	return compact.Bytes()
}

func verifyForbiddenClaimsV1(t *testing.T, raw []byte) {
	t.Helper()
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	forbiddenKeys := map[string]struct{}{}
	for _, key := range []string{
		"accepted", "accepted_option_id", "actor_id", "allowed_option_ids", "approval_refs", "approved", "approved_at", "approver_role",
		"candidate_contract_manifest", "candidate_evidence", "candidate_owner_roles", "canonical_contract_manifest", "commit_sha",
		"compile_attestation", "contract_manifest_sha256", "exact_value", "freeze", "head_sha", "idl_manifest",
		"owner_approvals", "recommended_option_id", "reopen_exception", "required_owner_roles", "review_commit_sha",
		"review_id", "review_state", "review_url", "reviewer_id", "selected_option", "selected_option_id", "target_tests",
		"validator_build_closure", "value", "vector_exact_set",
	} {
		forbiddenKeys[key] = struct{}{}
	}
	forbiddenValues := map[string]struct{}{
		"accept_recommendation": {},
		"reject_recommendation": {},
		"reject_keep_blocked":   {},
		"awaiting_review":       {},
		"approved":              {},
		"frozen":                {},
	}
	walkJSONV1(t, "$", value, forbiddenKeys, forbiddenValues)
}

func walkJSONV1(t *testing.T, path string, value any, forbiddenKeys, forbiddenValues map[string]struct{}) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, forbidden := forbiddenKeys[key]; forbidden {
				t.Fatalf("candidate preparation 禁止字段 %s.%s", path, key)
			}
			walkJSONV1(t, path+"."+key, child, forbiddenKeys, forbiddenValues)
		}
	case []any:
		for index, child := range typed {
			walkJSONV1(t, fmt.Sprintf("%s[%d]", path, index), child, forbiddenKeys, forbiddenValues)
		}
	case string:
		_, capabilityDeclaration := forbiddenValues[typed]
		if capabilityDeclaration && !strings.HasPrefix(path, "$.forbidden_capabilities[") {
			t.Fatalf("candidate preparation 禁止权威值 %s=%q", path, typed)
		}
	case json.Number, float64:
		t.Fatalf("candidate preparation 不得提交数值 %s=%v", path, typed)
	case nil:
		t.Fatalf("candidate preparation 不得提交 null %s", path)
	}
}

func strictDecodeRequestV1(raw []byte) (requestV1, error) {
	if err := validateJSONShapeV1(raw); err != nil {
		return requestV1{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request requestV1
	if err := decoder.Decode(&request); err != nil {
		return requestV1{}, err
	}
	if err := requireEOFV1(decoder); err != nil {
		return requestV1{}, err
	}
	return request, nil
}

func validateJSONShapeV1(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := readJSONValueV1(decoder); err != nil {
		return err
	}
	return requireEOFV1(decoder)
}

func readJSONValueV1(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("JSON null 被禁止")
	}
	if _, number := token.(json.Number); number {
		return fmt.Errorf("JSON number 被禁止")
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key 非字符串")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("JSON object 重复键 %q", key)
			}
			seen[key] = struct{}{}
			if valueErr := readJSONValueV1(decoder); valueErr != nil {
				return valueErr
			}
		}
	case '[':
		for decoder.More() {
			if valueErr := readJSONValueV1(decoder); valueErr != nil {
				return valueErr
			}
		}
	default:
		return fmt.Errorf("JSON delimiter 非法: %q", delimiter)
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	wantClosing := json.Delim('}')
	if delimiter == '[' {
		wantClosing = ']'
	}
	if closing != wantClosing {
		return fmt.Errorf("JSON closing delimiter=%v want=%v", closing, wantClosing)
	}
	return nil
}

func requireEOFV1(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON 含尾随值")
		}
		return err
	}
	return nil
}

func verifyArtifactRefV1(t *testing.T, repoRoot string, ref artifactRefV1, wantPath string) {
	t.Helper()
	if ref.Path != wantPath || !shaPatternV1.MatchString(ref.SHA256) {
		t.Fatalf("artifact ref 非法: %+v want path=%s", ref, wantPath)
	}
	raw := readFileV1(t, repoRoot, ref.Path)
	digest := sha256.Sum256(raw)
	wantHash := "sha256:" + hex.EncodeToString(digest[:])
	if ref.SHA256 != wantHash {
		t.Fatalf("%s raw SHA-256=%s want=%s", ref.Path, wantHash, ref.SHA256)
	}
}

func verifyValidatorPackagesV1(t *testing.T, repoRoot string, refs []artifactRefV1) {
	t.Helper()
	specs := sourceSpecsV1()
	if len(refs) != len(specs) {
		t.Fatalf("validator_sources=%v", refs)
	}
	for index, spec := range specs {
		verifyArtifactRefV1(t, repoRoot, refs[index], spec.Path)
		verifySourcePackageV1(t, repoRoot, spec)
	}
}

func verifySourcePackageV1(t *testing.T, repoRoot string, spec sourceSpecV1) {
	t.Helper()
	sourcePath := filepath.Join(repoRoot, filepath.FromSlash(spec.Path))
	entries, err := os.ReadDir(filepath.Dir(sourcePath))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(sourcePath) {
		t.Fatalf("%s source directory exact-set=%v want=%s", spec.PackageName, entries, filepath.Base(sourcePath))
	}
	entry := entries[0]
	if entry.Type()&os.ModeSymlink != 0 {
		t.Fatalf("validator source 不得为 symlink: %s", entry.Name())
	}
	info, infoErr := entry.Info()
	if infoErr != nil {
		t.Fatal(infoErr)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("validator source 必须是 mode 0644 regular file: %s mode=%s", entry.Name(), info.Mode())
	}
	raw := readFileV1(t, repoRoot, spec.Path)
	if bytes.Contains(raw, []byte("//"+"go:build")) || bytes.Contains(raw, []byte("// "+"+build")) || bytes.Contains(raw, []byte("//"+"go:embed")) {
		t.Fatalf("%s 禁止 build constraint/embed", spec.PackageName)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), sourcePath, raw, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name.Name != spec.PackageName {
		t.Fatalf("validator package=%q want=%q", parsed.Name.Name, spec.PackageName)
	}
	var imports []string
	for _, importSpec := range parsed.Imports {
		if importSpec.Name != nil {
			t.Fatalf("validator import 禁止 alias/dot/blank: %s", importSpec.Path.Value)
		}
		imports = append(imports, strings.Trim(importSpec.Path.Value, `"`))
	}
	if !reflect.DeepEqual(imports, spec.Imports) {
		t.Fatalf("%s stdlib import exact-set=%v want=%v", spec.PackageName, imports, spec.Imports)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && (function.Name.Name == "init" || function.Name.Name == "TestMain") {
			t.Fatalf("%s 禁止 %s", spec.PackageName, function.Name.Name)
		}
	}
}

func sourceSpecsV1() []sourceSpecV1 {
	return []sourceSpecV1{
		{
			Path:        validatorPathV1,
			PackageName: "w2r00candidatepreparation_test",
			Imports: []string{
				"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "fmt", "go/ast", "go/parser", "go/token",
				"io", "os", "path/filepath", "reflect", "regexp", "runtime", "sort", "strings", "testing",
			},
		},
		{
			Path:        guardPathV1,
			PackageName: "w2r00candidatepreparationguard_test",
			Imports: []string{
				"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "fmt", "go/ast", "go/parser", "go/token",
				"io", "os", "path/filepath", "reflect", "runtime", "sort", "strings", "testing",
			},
		},
	}
}

func itemSpecsV1() []itemSpecV1 {
	return []itemSpecV1{
		{
			DecisionID:          "R00-D05",
			OpenItemID:          "BILL-OPEN-004",
			Prerequisites:       []string{"R00-D03", "R00-D04", "W2-ADR-005"},
			CrossGateAlignments: []string{},
			InputIDs:            []string{"R00-D05-IN-POLICY-CAP-SCOPE", "R00-D05-IN-POLICY-LIFECYCLE", "R00-D05-IN-EMERGENCY-PAUSE"},
			EvidenceIDs:         []string{"R00-D05-EV-BOUNDARY-VECTORS", "R00-D05-EV-LIFECYCLE-VECTORS", "R00-D05-EV-PAUSE-RACE", "R00-D05-EV-VERSION-PIN"},
		},
		{
			DecisionID:          "R00-D07",
			OpenItemID:          "BILL-OPEN-005",
			Prerequisites:       []string{"R00-D06"},
			CrossGateAlignments: []string{},
			InputIDs:            []string{"R00-D07-IN-MAPPING-IDENTITY", "R00-D07-IN-MAPPING-LIFECYCLE", "R00-D07-IN-MAPPING-AUTHORITY"},
			EvidenceIDs:         []string{"R00-D07-EV-RPC-GOLDEN", "R00-D07-EV-PIN-MISMATCH", "R00-D07-EV-LIFECYCLE", "R00-D07-EV-TAMPER"},
		},
		{
			DecisionID:          "R00-D08",
			OpenItemID:          "BILL-OPEN-006",
			Prerequisites:       []string{},
			CrossGateAlignments: []string{"R04-D14"},
			InputIDs:            []string{"R00-D08-IN-PROVIDER-CAPABILITY", "R00-D08-IN-UNKNOWN-SEMANTICS", "R00-D08-IN-PROVIDER-SECURITY"},
			EvidenceIDs:         []string{"R00-D08-EV-SANDBOX-CAPABILITY", "R00-D08-EV-RESPONSE-LOST", "R00-D08-EV-LATE-RESULT", "R00-D08-EV-REDACTION"},
		},
		{
			DecisionID:          "R00-D09",
			OpenItemID:          "BILL-OPEN-007",
			Prerequisites:       []string{},
			CrossGateAlignments: []string{"R01-D02", "R02-D09", "R04-D14"},
			InputIDs:            []string{"R00-D09-IN-MODEL-RECEIPT-SCHEMA", "R00-D09-IN-MODEL-RECEIPT-CANONICAL", "R00-D09-IN-FINALIZE-BINDING"},
			EvidenceIDs:         []string{"R00-D09-EV-CANONICAL-GOLDEN", "R00-D09-EV-SCHEMA-NEGATIVE", "R00-D09-EV-TERMINAL-UNIQUENESS", "R00-D09-EV-FINALIZE-GUARD"},
		},
		{
			DecisionID:          "R00-D11",
			OpenItemID:          "BILL-OPEN-009",
			Prerequisites:       []string{},
			CrossGateAlignments: []string{"R02-D06", "R04-D13", "R04-D14"},
			InputIDs:            []string{"R00-D11-IN-CLOCK-SOURCES", "R00-D11-IN-SKEW-POLICY", "R00-D11-IN-CLOCK-RECOVERY"},
			EvidenceIDs:         []string{"R00-D11-EV-SKEW-INJECTION", "R00-D11-EV-ALERT-RECOVERY", "R00-D11-EV-CLOCK-REGRESSION", "R00-D11-EV-AUTHORITY-INDEPENDENCE"},
		},
		{
			DecisionID:          "R00-D13",
			OpenItemID:          "BILL-OPEN-011",
			Prerequisites:       []string{"R01-D05", "R04-D10"},
			CrossGateAlignments: []string{"W2-ADR-011"},
			InputIDs:            []string{"R00-D13-IN-REGISTRY-SCOPE", "R00-D13-IN-SLOT-ORDINALS", "R00-D13-IN-REF-AUTHORITY"},
			EvidenceIDs:         []string{"R00-D13-EV-REGISTRY-DIGEST", "R00-D13-EV-ORDINAL-NEGATIVE", "R00-D13-EV-ORDINAL-DOMAIN", "R00-D13-EV-COMMAND-KEY"},
		},
	}
}

func validateSortedUniqueStringsV1(t *testing.T, label string, values []string, requireNonEmpty bool) {
	t.Helper()
	if requireNonEmpty && len(values) == 0 {
		t.Fatalf("%s 不得为空", label)
	}
	if len(values) == 0 {
		return
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s 含空值", label)
		}
		if _, duplicate := seen[value]; duplicate {
			t.Fatalf("%s 含重复值 %q", label, value)
		}
		seen[value] = struct{}{}
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(values, sorted) {
		t.Fatalf("%s 必须排序: %v", label, values)
	}
}

func isJSONNullV1(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func repoRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 R00 candidate preparation validator")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
}

func readFileV1(t *testing.T, repoRoot, repoPath string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(repoPath)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
