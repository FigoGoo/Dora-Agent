// Package w2r01ownerdecision_test 验证 R01 Owner 待决请求只绑定当前 partial candidate，不产生批准、构建证明或实现解锁能力。
package w2r01ownerdecision_test

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
	requestPathV1           = "docs/design/agent/approvals/w2-r01-owner-decision-requests/DR-W2-R01-v1.json"
	matrixPathV1            = "docs/design/agent/w2-r01-owner-decision-matrix-v1.md"
	reviewFreezePathV1      = "docs/design/agent/approvals/w2-review-freeze-manifest.json"
	candidateManifestPathV1 = "agent/tests/contract/testdata/w2_r01/manifest.json"
	validatorPathV1         = "agent/tests/contract/w2r01ownerdecision/owner_decision_request_v1_test.go"
	guardPathV1             = "agent/tests/contract/w2r01ownerdecisionguard/owner_decision_request_guard_v1_test.go"
)

type requestV1 struct {
	SchemaVersion             string          `json:"schema_version"`
	RequestID                 string          `json:"request_id"`
	Gate                      string          `json:"gate"`
	Status                    string          `json:"status"`
	ImplementationUnlocked    bool            `json:"implementation_unlocked"`
	OwnerRoleSetStatus        string          `json:"owner_role_set_status"`
	DecisionDocument          artifactRefV1   `json:"decision_document"`
	GateManifest              artifactRefV1   `json:"gate_manifest"`
	CandidateContractManifest artifactRefV1   `json:"candidate_contract_manifest"`
	ValidatorSources          []artifactRefV1 `json:"validator_sources"`
	Items                     []itemV1        `json:"items"`
	UnmetEvidence             []string        `json:"unmet_evidence"`
	BlockedProductionGates    []string        `json:"blocked_production_gates"`
	BlockStatement            string          `json:"block_statement"`
}

type artifactRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type itemV1 struct {
	DecisionID          string   `json:"decision_id"`
	Title               string   `json:"title"`
	Status              string   `json:"status"`
	AllowedOptionIDs    []string `json:"allowed_option_ids"`
	RecommendedOptionID string   `json:"recommended_option_id"`
	CandidateOwnerRoles []string `json:"candidate_owner_roles"`
	MatrixRowID         string   `json:"matrix_row_id"`
	BlockCode           string   `json:"block_code"`
}

type gateManifestProjectionV1 struct {
	Gates []struct {
		Gate               string          `json:"gate"`
		Status             string          `json:"status"`
		RequiredOwnerRoles []string        `json:"required_owner_roles"`
		Freeze             json.RawMessage `json:"freeze"`
		ReopenException    json.RawMessage `json:"reopen_exception"`
		CandidateEvidence  []struct {
			Scope                  string   `json:"scope"`
			Coverage               string   `json:"coverage"`
			ContractManifestPath   string   `json:"contract_manifest_path"`
			ContractManifestSHA256 string   `json:"contract_manifest_sha256"`
			VectorIDs              []string `json:"vector_ids"`
			TargetTests            []string `json:"target_tests"`
		} `json:"candidate_evidence"`
		Blockers []struct {
			Code string `json:"code"`
		} `json:"blockers"`
	} `json:"gates"`
}

type candidateManifestV1 struct {
	SchemaVersion string `json:"schema_version"`
	Files         []struct {
		File        string `json:"file"`
		SHA256      string `json:"sha256"`
		VectorCount int    `json:"vector_count"`
	} `json:"files"`
	DesignSources         []artifactRefV1 `json:"design_sources"`
	ValidatorSources      []artifactRefV1 `json:"validator_sources"`
	ValidatorBuildSources []artifactRefV1 `json:"validator_build_sources"`
	FixtureIDs            []string        `json:"fixture_ids"`
	VectorIDs             []string        `json:"vector_ids"`
	TotalVectorCount      int             `json:"total_vector_count"`
	TargetTests           []string        `json:"target_tests"`
}

type itemSpecV1 struct {
	ID                string
	Title             string
	Status            string
	AllowedOptionIDs  []string
	RecommendedOption string
	Roles             []string
	BlockCode         string
}

type sourceSpecV1 struct {
	Path        string
	PackageName string
	Imports     []string
}

var ownerRolePatternV1 = regexp.MustCompile(`^[a-z][a-z0-9_]*_owner$`)

func TestW2R01OwnerDecisionRequestV1(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootV1(t)
	raw := readFileV1(t, repoRoot, requestPathV1)
	request, err := strictDecodeRequestV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifyRequestShapeV1(t, raw)

	if request.SchemaVersion != "w2_r01_owner_decision_request.v1" || request.RequestID != "DR-W2-R01-v1" || request.Gate != "W2-R01" {
		t.Fatalf("R01 Owner request identity 非法: %+v", request)
	}
	if request.Status != "awaiting_owner_decision" || request.ImplementationUnlocked || request.OwnerRoleSetStatus != "provisional_candidates_not_final" {
		t.Fatalf("R01 Owner request 不得提升状态或冻结 Owner: %+v", request)
	}
	for _, fragment := range []string{"expansion_frozen", "partial_candidate", "87", "禁止"} {
		if !strings.Contains(request.BlockStatement, fragment) {
			t.Fatalf("R01 block statement 缺少 %q: %s", fragment, request.BlockStatement)
		}
	}

	verifyArtifactRefV1(t, repoRoot, request.DecisionDocument, matrixPathV1)
	verifyMatrixV1(t, repoRoot, request.DecisionDocument.Path)
	verifyArtifactRefV1(t, repoRoot, request.GateManifest, reviewFreezePathV1)
	verifyArtifactRefV1(t, repoRoot, request.CandidateContractManifest, candidateManifestPathV1)
	verifyValidatorPackagesV1(t, repoRoot, request.ValidatorSources)
	verifyItemsV1(t, request.Items)

	wantUnmet := []string{
		"ADR_DISPOSITIONS_PENDING",
		"AUTHORITY_OUTCOME_FAILED_AFTER_BASELINE_MISSING",
		"BUILD_TRUST_CLOSURE_MISSING",
		"CARD_AND_REGISTRY_SCOPE_PENDING",
		"FINAL_OWNER_ROLE_EXACT_SET_MISSING",
		"FULL_GATE_BASELINE_MISSING",
		"GOVERNANCE_AUTHORITY_NOT_ACTIVE",
		"GRAPH_TOOL_RESULT_RECEIPT_CANDIDATE_PARTIAL_ONLY",
		"OWNER_DECISIONS_PENDING",
		"SLOT_REGISTRY_ORDINAL_PENDING",
		"VERSION_POLICY_PENDING",
	}
	if !reflect.DeepEqual(request.UnmetEvidence, wantUnmet) {
		t.Fatalf("R01 unmet evidence=%v want=%v", request.UnmetEvidence, wantUnmet)
	}
	if want := []string{"W2-A2", "W2-B1"}; !reflect.DeepEqual(request.BlockedProductionGates, want) {
		t.Fatalf("R01 blocked production gates=%v want=%v", request.BlockedProductionGates, want)
	}

	manifest := verifyCandidateManifestV1(t, repoRoot, request.CandidateContractManifest.Path)
	verifyLiveGateV1(t, repoRoot, request, manifest)
	verifyForbiddenAuthorityFieldsV1(t, raw)
}

func TestW2R01OwnerDecisionRequestV1StrictJSON(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string][]byte{
		"unknown":   []byte(`{"schema_version":"x","future":true}`),
		"duplicate": []byte(`{"schema_version":"x","schema_version":"y"}`),
		"trailing":  []byte(`{"schema_version":"x"}{}`),
		"null":      []byte(`{"implementation_unlocked":null}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := strictDecodeRequestV1(raw); err == nil {
				t.Fatalf("R01 Owner request 必须拒绝 %s JSON", name)
			}
		})
	}
}

func verifyRequestShapeV1(t *testing.T, raw []byte) {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	requireExactKeysV1(t, "request", root, []string{
		"block_statement", "blocked_production_gates", "candidate_contract_manifest", "decision_document",
		"gate", "gate_manifest", "implementation_unlocked", "items", "owner_role_set_status", "request_id",
		"schema_version", "status", "unmet_evidence", "validator_sources",
	})
	for _, key := range []string{"decision_document", "gate_manifest", "candidate_contract_manifest"} {
		verifyArtifactShapeV1(t, key, root[key])
	}
	var validatorSources []json.RawMessage
	if err := json.Unmarshal(root["validator_sources"], &validatorSources); err != nil || validatorSources == nil || len(validatorSources) != 2 {
		t.Fatalf("validator_sources 必须是 two-source non-null array: %v", err)
	}
	for index, source := range validatorSources {
		verifyArtifactShapeV1(t, fmt.Sprintf("validator_sources[%d]", index), source)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(root["items"], &items); err != nil || items == nil || len(items) != 12 {
		t.Fatalf("items 必须是 12-item non-null array: %v", err)
	}
	for index, itemRaw := range items {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(itemRaw, &item); err != nil {
			t.Fatal(err)
		}
		requireExactKeysV1(t, fmt.Sprintf("items[%d]", index), item, []string{
			"allowed_option_ids", "block_code", "candidate_owner_roles", "decision_id", "matrix_row_id",
			"recommended_option_id", "status", "title",
		})
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

func verifyItemsV1(t *testing.T, items []itemV1) {
	t.Helper()
	specs := itemSpecsV1()
	if len(items) != len(specs) {
		t.Fatalf("R01 decision items=%d want=%d", len(items), len(specs))
	}
	for index, spec := range specs {
		item := items[index]
		if item.DecisionID != spec.ID || item.Title != spec.Title || item.Status != spec.Status {
			t.Fatalf("R01 item[%d] identity/status=%+v want=%+v", index, item, spec)
		}
		if item.MatrixRowID != spec.ID || item.BlockCode != spec.BlockCode {
			t.Fatalf("%s matrix row/block code 非法: %+v", item.DecisionID, item)
		}
		if !reflect.DeepEqual(item.AllowedOptionIDs, spec.AllowedOptionIDs) || item.RecommendedOptionID != spec.RecommendedOption {
			t.Fatalf("%s option exact-set 非法: %+v", item.DecisionID, item)
		}
		if !reflect.DeepEqual(item.CandidateOwnerRoles, spec.Roles) {
			t.Fatalf("%s candidate owner roles=%v want=%v", item.DecisionID, item.CandidateOwnerRoles, spec.Roles)
		}
		validateSortedRolesV1(t, item.DecisionID, item.CandidateOwnerRoles)
	}
}

func itemSpecsV1() []itemSpecV1 {
	ballotOptions := []string{"accept_recommendation", "reject_keep_blocked"}
	return []itemSpecV1{
		{"R01-D01", "GraphToolResultV1 exact-version 与未知字段策略", "awaiting_owner_decision", ballotOptions, "accept_recommendation", []string{"agent_owner", "business_owner", "security_owner", "test_owner"}, "W2_R01_D01_OWNER_DECISION_PENDING"},
		{"R01-D02", "authority_outcome、failed-after 与 Billing authority projection 范围", "awaiting_owner_decision", ballotOptions, "accept_recommendation", []string{"agent_owner", "business_owner", "finance_owner", "operations_owner", "security_owner"}, "W2_R01_D02_OWNER_DECISION_PENDING"},
		{"R01-D03", "waiting_user.approval_ref.card_id 的 R01/R08 边界", "awaiting_owner_decision", ballotOptions, "accept_recommendation", []string{"agent_owner", "frontend_owner", "product_owner", "security_owner"}, "W2_R01_D03_OWNER_DECISION_PENDING"},
		{"R01-D04", "Tool Registry 机制与六 Tool Key exact-set", "awaiting_owner_decision", ballotOptions, "accept_recommendation", []string{"agent_owner", "business_owner", "finance_owner", "operations_owner", "security_owner", "test_owner"}, "W2_R01_D04_OWNER_DECISION_PENDING"},
		{"R01-D05", "Approval Consumption slot identity、Receipt/segment scope 与 ordinal", "candidate_incomplete_not_ballot_ready", []string{"submit_versioned_candidate", "keep_blocked"}, "submit_versioned_candidate", []string{"agent_owner", "business_owner", "security_owner", "test_owner"}, "W2_R01_D05_CANDIDATE_INCOMPLETE"},
		{"R01-D06", "R01 required_owner_roles exact-set", "scope_derivation_pending", []string{"derive_after_scope_closure", "keep_blocked"}, "derive_after_scope_closure", []string{"agent_owner", "business_owner", "finance_owner", "frontend_owner", "integration_owner", "operations_owner", "product_owner", "security_owner", "test_owner"}, "W2_R01_D06_SCOPE_DERIVATION_PENDING"},
		{"GOV-D01", "Owner role 到平台 actor/team authority", "awaiting_owner_decision", ballotOptions, "accept_recommendation", []string{"integration_owner", "security_owner"}, "W2_GOV_D01_OWNER_DECISION_PENDING"},
		{"GOV-D02", "Approval 与 current PR head freshness", "awaiting_owner_decision", ballotOptions, "accept_recommendation", []string{"integration_owner", "security_owner", "test_owner"}, "W2_GOV_D02_OWNER_DECISION_PENDING"},
		{"GOV-D03", "Distinct actor per required role", "awaiting_owner_decision", ballotOptions, "accept_recommendation", []string{"integration_owner", "security_owner", "test_owner"}, "W2_GOV_D03_OWNER_DECISION_PENDING"},
		{"GOV-D04", "Required check、Ruleset 与 no-bypass", "awaiting_owner_decision", ballotOptions, "accept_recommendation", []string{"integration_owner", "operations_owner", "security_owner", "test_owner"}, "W2_GOV_D04_OWNER_DECISION_PENDING"},
		{"GOV-D05", "Base-owned trust root 与 verifier source 保护", "awaiting_owner_decision", ballotOptions, "accept_recommendation", []string{"integration_owner", "operations_owner", "security_owner", "test_owner"}, "W2_GOV_D05_OWNER_DECISION_PENDING"},
		{"GOV-D06", "Validator transitive build-input closure", "awaiting_owner_decision", ballotOptions, "accept_recommendation", []string{"agent_owner", "integration_owner", "operations_owner", "security_owner", "test_owner"}, "W2_GOV_D06_OWNER_DECISION_PENDING"},
	}
}

func validateSortedRolesV1(t *testing.T, decisionID string, roles []string) {
	t.Helper()
	if len(roles) == 0 || !sort.StringsAreSorted(roles) {
		t.Fatalf("%s role 集必须非空且排序: %v", decisionID, roles)
	}
	for index, role := range roles {
		if !ownerRolePatternV1.MatchString(role) || index > 0 && roles[index-1] == role {
			t.Fatalf("%s role key 非法或重复: %v", decisionID, roles)
		}
	}
}

func verifyMatrixV1(t *testing.T, repoRoot, matrixPath string) {
	t.Helper()
	raw := string(readFileV1(t, repoRoot, matrixPath))
	for _, spec := range itemSpecsV1() {
		if !strings.Contains(raw, "`"+spec.ID+"`") {
			t.Fatalf("R01 matrix 缺少 %s", spec.ID)
		}
	}
	for _, fragment := range []string{
		"R01-D01`～`R01-D06", "GOV-D01`～`GOV-D06", "expansion_frozen / partial_candidate",
		"validator_build_closure", "GOV-D06", "formal Freeze blocker",
	} {
		if !strings.Contains(raw, fragment) {
			t.Fatalf("R01 matrix 缺少失败关闭片段 %q", fragment)
		}
	}
	verifyDecisionIDExactSetV1(t, raw, `R01-D[0-9]{2}`, []string{"R01-D01", "R01-D02", "R01-D03", "R01-D04", "R01-D05", "R01-D06"})
	verifyDecisionIDExactSetV1(t, raw, `GOV-D[0-9]{2}`, []string{"GOV-D01", "GOV-D02", "GOV-D03", "GOV-D04", "GOV-D05", "GOV-D06"})
}

func verifyDecisionIDExactSetV1(t *testing.T, document, pattern string, want []string) {
	t.Helper()
	seen := make(map[string]struct{})
	for _, decisionID := range regexp.MustCompile(pattern).FindAllString(document, -1) {
		seen[decisionID] = struct{}{}
	}
	actual := make([]string, 0, len(seen))
	for decisionID := range seen {
		actual = append(actual, decisionID)
	}
	sort.Strings(actual)
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("decision exact-set=%v want=%v", actual, want)
	}
}

func verifyLiveGateV1(t *testing.T, repoRoot string, request requestV1, candidate candidateManifestV1) {
	t.Helper()
	raw := readFileV1(t, repoRoot, request.GateManifest.Path)
	var manifest gateManifestProjectionV1
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, gate := range manifest.Gates {
		if gate.Gate != "W2-R01" {
			continue
		}
		if gate.Status != "expansion_frozen" || string(gate.Freeze) != "null" || string(gate.ReopenException) != "null" {
			t.Fatalf("R01 live Gate 不再失败关闭: %+v", gate)
		}
		if want := []string{"agent_owner", "business_owner", "finance_owner", "operations_owner", "security_owner"}; !reflect.DeepEqual(gate.RequiredOwnerRoles, want) {
			t.Fatalf("R01 live provisional roles=%v want=%v", gate.RequiredOwnerRoles, want)
		}
		wantBlockers := []string{
			"W2_R01_ADR_REVIEW_PENDING", "W2_R01_CARD_AND_REGISTRY_SCOPE_PENDING", "W2_R01_CORPUS_SCOPE_INCOMPLETE",
			"W2_R01_OWNER_APPROVAL_MISSING", "W2_R01_OWNER_SCOPE_PENDING", "W2_R01_SLOT_REGISTRY_ORDINAL_PENDING",
			"W2_R01_VALIDATOR_BUILD_CLOSURE_PENDING", "W2_R01_VERSION_POLICY_PENDING",
		}
		actualBlockers := make([]string, len(gate.Blockers))
		for index, blocker := range gate.Blockers {
			actualBlockers[index] = blocker.Code
		}
		if !reflect.DeepEqual(actualBlockers, wantBlockers) {
			t.Fatalf("R01 live blockers=%v want=%v", actualBlockers, wantBlockers)
		}
		if len(gate.CandidateEvidence) != 1 {
			t.Fatalf("R01 candidate evidence=%d want=1", len(gate.CandidateEvidence))
		}
		live := gate.CandidateEvidence[0]
		if live.Scope != "graph_tool_result_and_tool_receipt_current_candidate" || live.Coverage != "partial_candidate" {
			t.Fatalf("R01 candidate scope/coverage 非法: %+v", live)
		}
		if live.ContractManifestPath != request.CandidateContractManifest.Path || live.ContractManifestSHA256 != request.CandidateContractManifest.SHA256 {
			t.Fatalf("R01 live/request candidate 绑定漂移: live=%+v request=%+v", live, request.CandidateContractManifest)
		}
		verifyUniqueSetEqualV1(t, "R01 candidate vectors", live.VectorIDs, candidate.VectorIDs, 87)
		verifyUniqueSetEqualV1(t, "R01 candidate target tests", live.TargetTests, candidate.TargetTests, 4)
		return
	}
	t.Fatal("Review Freeze manifest 缺少 W2-R01")
}

func verifyCandidateManifestV1(t *testing.T, repoRoot, manifestPath string) candidateManifestV1 {
	t.Helper()
	raw := readFileV1(t, repoRoot, manifestPath)
	if err := validateJSONShapeV1(raw); err != nil {
		t.Fatalf("R01 candidate manifest strict JSON: %v", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	requireExactKeysV1(t, "R01 candidate manifest", root, []string{
		"design_sources", "files", "fixture_ids", "schema_version", "target_tests", "total_vector_count",
		"validator_build_sources", "validator_sources", "vector_ids",
	})
	var manifest candidateManifestV1
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != "w2_r01_contract_corpus_manifest.v1" || manifest.TotalVectorCount != 87 {
		t.Fatalf("R01 candidate identity/count 非法: %+v", manifest)
	}
	if !reflect.DeepEqual(manifest.FixtureIDs, []string{"open.empty"}) {
		t.Fatalf("R01 candidate fixture IDs=%v", manifest.FixtureIDs)
	}
	wantTargetTests := []string{"TestW2R01CorpusManifest", "TestGraphToolResultV1Corpus", "TestWarningIntegerPolicySafeBoundaryV1", "TestToolReceiptV1Corpus"}
	if !reflect.DeepEqual(manifest.TargetTests, wantTargetTests) {
		t.Fatalf("R01 manifest target tests=%v want=%v", manifest.TargetTests, wantTargetTests)
	}
	verifyUniqueSetEqualV1(t, "R01 manifest target tests", manifest.TargetTests, wantTargetTests, 4)

	wantFiles := []struct {
		name  string
		hash  string
		count int
	}{
		{"graph_tool_result_v1.json", "sha256:0217f80689927017541fa07e7f78fd40eede77ff82bed94c6f8c30172ec5a63a", 48},
		{"tool_receipt_v1.json", "sha256:abe0ebd3c99e11bf3d7d0e9d59a68ceca0db0f087893a76eb177099b22ff0d89", 39},
	}
	if len(manifest.Files) != len(wantFiles) {
		t.Fatalf("R01 corpus files=%d want=%d", len(manifest.Files), len(wantFiles))
	}
	manifestDir := filepath.Dir(filepath.Join(repoRoot, filepath.FromSlash(manifestPath)))
	for index, want := range wantFiles {
		file := manifest.Files[index]
		if file.File != want.name || file.SHA256 != want.hash || file.VectorCount != want.count {
			t.Fatalf("R01 corpus file[%d]=%+v want=%+v", index, file, want)
		}
		verifyRawHashV1(t, filepath.Join(manifestDir, file.File), file.SHA256)
	}
	if manifest.Files[0].VectorCount+manifest.Files[1].VectorCount != manifest.TotalVectorCount {
		t.Fatal("R01 corpus file vector count 小计不等于 total_vector_count")
	}
	derivedVectorIDs := deriveCorpusVectorIDsV1(t, manifestDir, manifest.Files[0].File, manifest.Files[1].File)
	if !reflect.DeepEqual(manifest.VectorIDs, derivedVectorIDs) {
		t.Fatal("R01 manifest vector_ids 未按 GraphToolResult cases + ToolReceipt transitions/evidence 从 corpus 派生")
	}
	verifyUniqueSetEqualV1(t, "R01 manifest vectors", manifest.VectorIDs, derivedVectorIDs, 87)

	verifyArtifactSetV1(t, repoRoot, "design_sources", manifest.DesignSources, []string{
		"docs/design/agent/graph-tool-result-receipt-contract-v1.md",
		"docs/design/agent/runner-session-lane-review-v1.md",
		"docs/design/cross-module/aigc-contract-catalog.md",
	})
	verifyArtifactSetV1(t, repoRoot, "validator_sources", manifest.ValidatorSources, []string{
		"agent/tests/contract/w2r01/approval_continuation_parent_receipt_v1.go",
		"agent/tests/contract/w2r01/graph_tool_result_v1.go",
		"agent/tests/contract/w2r01/graph_tool_result_v1_corpus_test.go",
		"agent/tests/contract/w2r01/tool_receipt_v1.go",
		"agent/tests/contract/w2r01/tool_receipt_v1_corpus_test.go",
		"agent/tests/contract/w2r01/validator_support_v1.go",
	})
	verifyArtifactSetV1(t, repoRoot, "validator_build_sources", manifest.ValidatorBuildSources, []string{"agent/go.mod", "agent/go.sum"})
	verifyExternalBuildGapV1(t, repoRoot, manifest.ValidatorSources)
	return manifest
}

func deriveCorpusVectorIDsV1(t *testing.T, manifestDir, resultFile, receiptFile string) []string {
	t.Helper()
	var resultCorpus struct {
		Cases []struct {
			ID string `json:"id"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(readRawFileV1(t, filepath.Join(manifestDir, resultFile)), &resultCorpus); err != nil {
		t.Fatal(err)
	}
	var receiptCorpus struct {
		TransitionCases []struct {
			ID string `json:"id"`
		} `json:"transition_cases"`
		EvidenceCases []struct {
			ID string `json:"id"`
		} `json:"evidence_cases"`
	}
	if err := json.Unmarshal(readRawFileV1(t, filepath.Join(manifestDir, receiptFile)), &receiptCorpus); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(resultCorpus.Cases)+len(receiptCorpus.TransitionCases)+len(receiptCorpus.EvidenceCases))
	for _, corpusCase := range resultCorpus.Cases {
		ids = append(ids, corpusCase.ID)
	}
	for _, corpusCase := range receiptCorpus.TransitionCases {
		ids = append(ids, corpusCase.ID)
	}
	for _, corpusCase := range receiptCorpus.EvidenceCases {
		ids = append(ids, corpusCase.ID)
	}
	return ids
}

func verifyExternalBuildGapV1(t *testing.T, repoRoot string, sources []artifactRefV1) {
	t.Helper()
	foundNorm := false
	for _, source := range sources {
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, filepath.FromSlash(source.Path)), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, importSpec := range parsed.Imports {
			if strings.Trim(importSpec.Path.Value, `"`) == "golang.org/x/text/unicode/norm" {
				foundNorm = true
			}
		}
	}
	if !foundNorm {
		t.Fatal("R01 validator 必须继续显式暴露 x/text build-closure 缺口")
	}
	goMod := string(readFileV1(t, repoRoot, "agent/go.mod"))
	if !strings.Contains(goMod, "golang.org/x/text") {
		t.Fatal("agent/go.mod 缺少 x/text 依赖")
	}
}

func verifyArtifactSetV1(t *testing.T, repoRoot, label string, refs []artifactRefV1, wantPaths []string) {
	t.Helper()
	if len(refs) != len(wantPaths) {
		t.Fatalf("%s=%d want=%d", label, len(refs), len(wantPaths))
	}
	for index, wantPath := range wantPaths {
		verifyArtifactRefV1(t, repoRoot, refs[index], wantPath)
	}
}

func verifyUniqueSetEqualV1(t *testing.T, label string, actual, want []string, wantCount int) {
	t.Helper()
	if len(actual) != wantCount || len(want) != wantCount {
		t.Fatalf("%s count actual=%d want=%d expected=%d", label, len(actual), len(want), wantCount)
	}
	actualSorted := append([]string(nil), actual...)
	wantSorted := append([]string(nil), want...)
	sort.Strings(actualSorted)
	sort.Strings(wantSorted)
	for index := 1; index < len(actualSorted); index++ {
		if actualSorted[index-1] == actualSorted[index] {
			t.Fatalf("%s duplicate=%s", label, actualSorted[index])
		}
	}
	if !reflect.DeepEqual(actualSorted, wantSorted) {
		t.Fatalf("%s exact-set 漂移", label)
	}
}

func verifyValidatorPackagesV1(t *testing.T, repoRoot string, refs []artifactRefV1) {
	t.Helper()
	wantPaths := []string{validatorPathV1, guardPathV1}
	verifyArtifactSetV1(t, repoRoot, "request validator_sources", refs, wantPaths)
	for _, spec := range sourceSpecsV1() {
		verifySourcePackageV1(t, repoRoot, spec)
	}
}

func sourceSpecsV1() []sourceSpecV1 {
	return []sourceSpecV1{
		{
			Path: validatorPathV1, PackageName: "w2r01ownerdecision_test",
			Imports: []string{"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "fmt", "go/ast", "go/parser", "go/token", "io", "os", "path/filepath", "reflect", "regexp", "runtime", "sort", "strings", "testing"},
		},
		{
			Path: guardPathV1, PackageName: "w2r01ownerdecisionguard_test",
			Imports: []string{"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "go/ast", "go/parser", "go/token", "os", "path/filepath", "reflect", "runtime", "strings", "testing"},
		},
	}
}

func verifySourcePackageV1(t *testing.T, repoRoot string, spec sourceSpecV1) {
	t.Helper()
	sourcePath := filepath.Join(repoRoot, filepath.FromSlash(spec.Path))
	entries, err := os.ReadDir(filepath.Dir(sourcePath))
	if err != nil {
		t.Fatal(err)
	}
	var goSources []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			t.Fatalf("validator source 不得为 symlink: %s", entry.Name())
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("validator source 必须是 regular file: %s: %v", entry.Name(), infoErr)
		}
		goSources = append(goSources, entry.Name())
	}
	if want := []string{filepath.Base(sourcePath)}; !reflect.DeepEqual(goSources, want) {
		t.Fatalf("%s Go source exact-set=%v want=%v", spec.PackageName, goSources, want)
	}
	raw := readFileV1(t, repoRoot, spec.Path)
	if bytes.Contains(raw, []byte("//"+"go:build")) || bytes.Contains(raw, []byte("// "+"+build")) {
		t.Fatalf("%s 禁止 build constraint", spec.PackageName)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), sourcePath, raw, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name.Name != spec.PackageName {
		t.Fatalf("validator package=%q want=%q", parsed.Name.Name, spec.PackageName)
	}
	imports := make([]string, 0, len(parsed.Imports))
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

func verifyForbiddenAuthorityFieldsV1(t *testing.T, raw []byte) {
	t.Helper()
	for _, forbiddenKey := range []string{
		"selected_option", "accepted", "approved", "owner_approvals", "approval_refs", "review_id", "actor_id",
		"review_url", "approver_role", "approved_at", "commit_sha", "freeze", "required_owner_roles",
		"validator_build_closure", "compile_attestation", "candidate_unactivated",
	} {
		if bytes.Contains(raw, []byte(`"`+forbiddenKey+`"`)) {
			t.Fatalf("R01 Owner request 禁止字段 %q", forbiddenKey)
		}
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
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("存在尾随 JSON token %v", token)
	}
	return nil
}

func verifyArtifactRefV1(t *testing.T, repoRoot string, ref artifactRefV1, wantPath string) {
	t.Helper()
	if ref.Path != wantPath {
		t.Fatalf("artifact path=%q want=%q", ref.Path, wantPath)
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(ref.SHA256) {
		t.Fatalf("artifact SHA-256 格式非法: %q", ref.SHA256)
	}
	verifyRawHashV1(t, filepath.Join(repoRoot, filepath.FromSlash(ref.Path)), ref.SHA256)
}

func verifyRawHashV1(t *testing.T, filePath, wantHash string) {
	t.Helper()
	raw := readRawFileV1(t, filePath)
	digest := sha256.Sum256(raw)
	actual := "sha256:" + hex.EncodeToString(digest[:])
	if actual != wantHash {
		t.Fatalf("%s raw SHA-256=%s want=%s", filePath, actual, wantHash)
	}
}

func readRawFileV1(t *testing.T, filePath string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func repoRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 R01 validator 源文件")
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
