// Package w2adrballotpreparation_test 固定 W2 八项 ADR 的准备态与跨 Gate 联合裁决边界。
package w2adrballotpreparation_test

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
	"runtime"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	requestPathV1        = "docs/design/agent/approvals/w2-adr-ballot-preparation-requests/CPR-W2-ADR-v1.json"
	closurePathV1        = "docs/design/cross-module/w2-owner-decision-closure-v1.md"
	closureSHA256V1      = "sha256:29a39b727ba9089a8575b9807e090e97df5dcc6ae190b3f67dcae5b05ca01f8f"
	gateManifestPathV1   = "docs/design/agent/approvals/w2-review-freeze-manifest.json"
	gateManifestSHA256V1 = "sha256:a98059cfa4971f0123565d63ad56ab4d202ad354a0971bbecf99a0711bee616e"
	validatorPathV1      = "agent/tests/contract/w2adrballotpreparation/adr_ballot_preparation_v1_test.go"
	guardPathV1          = "agent/tests/contract/w2adrballotpreparationguard/adr_ballot_preparation_guard_v1_test.go"
	blockStatementV1     = "本 CPR 只固定 W2-ADR-001/002/003/004/005/008/010/011 的准备态、P4 冲突、跨 Gate 联合裁决引用和 ADR-005→ADR-011 硬前置；它不是 decision request 或 ballot，不提供 accept/reject、选择、批准、Reviewer/Actor、candidate evidence、状态迁移或 implementation unlock。八项 ADR 的 canonical ballot 均未创建；R02-D19 只联合裁决 ADR-001/002/008/010 的 A1 scope，不代表 ADR semantic acceptance，也不得自动传播任何选择。W2-R00～R04 继续 expansion_frozen 且 freeze/reopen_exception 为 null，所有生产 Gate 继续失败关闭。"
)

type requestV1 struct {
	SchemaVersion          string          `json:"schema_version"`
	RequestID              string          `json:"request_id"`
	RequestKind            string          `json:"request_kind"`
	Scope                  string          `json:"scope"`
	Status                 string          `json:"status"`
	RegistrationStatus     string          `json:"registration_status"`
	DecisionRequestStatus  string          `json:"decision_request_status"`
	ApprovalStatus         string          `json:"approval_status"`
	ImplementationStatus   string          `json:"implementation_status"`
	EvidenceStatus         string          `json:"evidence_status"`
	ImplementationUnlocked bool            `json:"implementation_unlocked"`
	BallotEnabled          bool            `json:"ballot_enabled"`
	CanonicalBallotStatus  string          `json:"canonical_ballot_status"`
	OwnerRoleSetStatus     string          `json:"owner_role_set_status"`
	DecisionDocument       artifactRefV1   `json:"decision_document"`
	GateManifest           artifactRefV1   `json:"gate_manifest"`
	LinkedRequests         []linkedRefV1   `json:"linked_requests"`
	ValidatorSources       []artifactRefV1 `json:"validator_sources"`
	RequiredADRIDs         []string        `json:"required_adr_ids"`
	Items                  []itemV1        `json:"items"`
	HardPrerequisiteEdges  []edgeV1        `json:"hard_prerequisite_edges"`
	BlockedProductionGates []string        `json:"blocked_production_gates"`
	ForbiddenCapabilities  []string        `json:"forbidden_capabilities"`
	BlockStatement         string          `json:"block_statement"`
}

type artifactRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type linkedRefV1 struct {
	RequestID string `json:"request_id"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
}

type itemV1 struct {
	ADRID                        string   `json:"adr_id"`
	SemanticRecommendationStatus string   `json:"semantic_recommendation_status"`
	CanonicalBallotStatus        string   `json:"canonical_ballot_status"`
	ReadinessStatus              string   `json:"readiness_status"`
	P4ConflictRefs               []string `json:"p4_conflict_refs"`
	JointDispositionRefs         []string `json:"joint_disposition_refs"`
	UnmetEvidence                []string `json:"unmet_evidence"`
	BlockCode                    string   `json:"block_code"`
}

type edgeV1 struct {
	PrerequisiteADRID string `json:"prerequisite_adr_id"`
	DependentADRID    string `json:"dependent_adr_id"`
}

type itemSpecV1 struct {
	ADRID     string
	Readiness string
	P4Refs    []string
	JointRefs []string
	Evidence  []string
	BlockCode string
}

type sourceSpecV1 struct {
	Path        string
	PackageName string
	Tests       []string
	Imports     []string
}

var shaPatternV1 = regexpSHA256V1()

// TestW2ADRBallotPreparationV1 固定八项 ADR 的未投票状态、联合裁决关系和 live Gate 失败关闭事实。
func TestW2ADRBallotPreparationV1(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootV1(t)
	verifySingleFileDirectoryV1(t, filepath.Join(repoRoot, filepath.Dir(requestPathV1)), filepath.Base(requestPathV1))
	raw := readRegularFileV1(t, repoRoot, requestPathV1)
	request, err := validateRequestV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifyArtifactRefV1(t, repoRoot, request.DecisionDocument, closurePathV1, closureSHA256V1)
	verifyArtifactRefV1(t, repoRoot, request.GateManifest, gateManifestPathV1, gateManifestSHA256V1)
	verifyLinkedRequestsV1(t, repoRoot, request.LinkedRequests)
	verifyValidatorSourcesV1(t, repoRoot, request.ValidatorSources)
	verifyLiveGatesV1(t, repoRoot)
}

// TestW2ADRBallotPreparationV1AuthorityMutations 证明准备清单拒绝第二投票、批准、角色和依赖闭合注入。
func TestW2ADRBallotPreparationV1AuthorityMutations(t *testing.T) {
	t.Parallel()
	raw := readRegularFileV1(t, repoRootV1(t), requestPathV1)
	mutations := map[string][]byte{
		"awaiting_owner_decision": bytes.Replace(raw, []byte(`"prepared_unregistered"`), []byte(`"awaiting_owner_decision"`), 1),
		"registered":              bytes.Replace(raw, []byte(`"not_registered"`), []byte(`"registered"`), 1),
		"request_created":         bytes.Replace(raw, []byte(`"not_created"`), []byte(`"created"`), 1),
		"approved":                bytes.Replace(raw, []byte(`"not_requested"`), []byte(`"approved"`), 1),
		"implementation_allowed":  bytes.Replace(raw, []byte(`"prohibited"`), []byte(`"allowed"`), 1),
		"implementation_unlocked": bytes.Replace(raw, []byte(`"implementation_unlocked": false`), []byte(`"implementation_unlocked": true`), 1),
		"ballot_enabled":          bytes.Replace(raw, []byte(`"ballot_enabled": false`), []byte(`"ballot_enabled": true`), 1),
		"ballot_created":          bytes.Replace(raw, []byte(`"canonical_ballot_status": "not_created"`), []byte(`"canonical_ballot_status": "created"`), 1),
		"final_roles":             bytes.Replace(raw, []byte(`"owner_role_set_status": "not_derived"`), []byte(`"owner_role_set_status": "final"`), 1),
		"ballot_ready":            bytes.Replace(raw, []byte(`"candidate_incomplete_not_ballot_ready"`), []byte(`"ballot_ready"`), 1),
		"semantic_accepted":       bytes.Replace(raw, []byte(`"recommendation_present_in_closure"`), []byte(`"accepted"`), 1),
		"adr_009":                 bytes.Replace(raw, []byte(`"W2-ADR-008"`), []byte(`"W2-ADR-009"`), 1),
		"adr_duplicate":           bytes.Replace(raw, []byte(`"W2-ADR-008"`), []byte(`"W2-ADR-005"`), 1),
		"omit_p4_ref":             bytes.Replace(raw, []byte("        \"P4-C01\",\n"), nil, 1),
		"omit_joint_ref":          bytes.Replace(raw, []byte("        \"R00-D14\",\n"), nil, 1),
		"omit_unmet_evidence":     bytes.Replace(raw, []byte("        \"AUTHORITY_ENVELOPE_EXACT_SCHEMA_MISSING\",\n"), nil, 1),
		"reverse_hard_edge":       bytes.Replace(raw, []byte(`"prerequisite_adr_id": "W2-ADR-005"`), []byte(`"prerequisite_adr_id": "W2-ADR-011"`), 1),
		"source_sha":              bytes.Replace(raw, []byte(closureSHA256V1), []byte("sha256:"+strings.Repeat("0", 64)), 1),
		"allowed_options":         bytes.Replace(raw, []byte(`"adr_id": "W2-ADR-001",`), []byte(`"adr_id": "W2-ADR-001","allowed_option_ids":["accept_recommendation"],`), 1),
		"recommended_option":      bytes.Replace(raw, []byte(`"adr_id": "W2-ADR-001",`), []byte(`"adr_id": "W2-ADR-001","recommended_option_id":"accept_recommendation",`), 1),
		"approval_actor":          bytes.Replace(raw, []byte(`"adr_id": "W2-ADR-001",`), []byte(`"adr_id": "W2-ADR-001","approver_role":"agent_owner","actor_id":"1",`), 1),
		"authority_owner":         bytes.Replace(raw, []byte(`"adr_id": "W2-ADR-001",`), []byte(`"adr_id": "W2-ADR-001","canonical_ballot_owner":"R02-D19",`), 1),
		"candidate_evidence":      bytes.Replace(raw, []byte(`"adr_id": "W2-ADR-001",`), []byte(`"adr_id": "W2-ADR-001","candidate_evidence":[],`), 1),
		"freeze":                  bytes.Replace(raw, []byte(`"adr_id": "W2-ADR-001",`), []byte(`"adr_id": "W2-ADR-001","freeze":{},`), 1),
	}
	for name, mutated := range mutations {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(mutated, raw) {
				t.Fatal("mutation 未命中")
			}
			if _, err := validateRequestV1(mutated); err == nil {
				t.Fatalf("准备契约必须拒绝 %s mutation", name)
			}
		})
	}
	gateRaw := readRegularFileV1(t, repoRootV1(t), gateManifestPathV1)
	gateMutations := map[string][]byte{
		"duplicate_status":   bytes.Replace(gateRaw, []byte(`"status": "expansion_frozen",`), []byte(`"status":"approved","status":"expansion_frozen",`), 1),
		"null_evidence":      bytes.Replace(gateRaw, []byte(`"candidate_evidence": []`), []byte(`"candidate_evidence": null`), 1),
		"duplicate_coverage": bytes.Replace(gateRaw, []byte(`"coverage": "partial_candidate",`), []byte(`"coverage":"full_candidate","coverage":"partial_candidate",`), 1),
		"live_p4_gate":       bytes.Replace(gateRaw, []byte(`"gate": "W2-R05"`), []byte(`"gate": "W2-P4-ADR"`), 1),
	}
	for name, mutated := range gateMutations {
		t.Run("gate_"+name, func(t *testing.T) {
			if bytes.Equal(mutated, gateRaw) {
				t.Fatal("Gate mutation 未命中")
			}
			if err := validateGateManifestRawV1(mutated); err == nil {
				t.Fatalf("Gate validator 必须拒绝 %s", name)
			}
		})
	}
}

// TestW2ADRBallotPreparationV1StrictJSON 固定未知字段、重复键、null、数字、尾随值和非法 UTF-8 均失败关闭。
func TestW2ADRBallotPreparationV1StrictJSON(t *testing.T) {
	t.Parallel()
	raw := readRegularFileV1(t, repoRootV1(t), requestPathV1)
	invalidUTF8 := bytes.Replace(raw, []byte(`prepared_unregistered`), []byte{'p', 0xff}, 1)
	mutations := map[string][]byte{
		"unknown":    bytes.Replace(raw, []byte(`"request_kind":`), []byte(`"future":true,"request_kind":`), 1),
		"duplicate":  bytes.Replace(raw, []byte(`"request_id": "CPR-W2-ADR-v1",`), []byte(`"request_id":"x","request_id":"CPR-W2-ADR-v1",`), 1),
		"nested_dup": bytes.Replace(raw, []byte(`"adr_id": "W2-ADR-001",`), []byte(`"adr_id":"x","adr_id":"W2-ADR-001",`), 1),
		"null":       bytes.Replace(raw, []byte(`"status": "prepared_unregistered"`), []byte(`"status": null`), 1),
		"number":     bytes.Replace(raw, []byte(`"scope":`), []byte(`"ordinal":1,"scope":`), 1),
		"trailing":   append(append([]byte{}, raw...), []byte(`{}`)...),
		"utf8":       invalidUTF8,
	}
	for name, mutated := range mutations {
		t.Run(name, func(t *testing.T) {
			if _, err := strictDecodeRequestV1(mutated); err == nil {
				t.Fatalf("严格 JSON 必须拒绝 %s", name)
			}
		})
	}
}

func validateRequestV1(raw []byte) (requestV1, error) {
	request, err := strictDecodeRequestV1(raw)
	if err != nil {
		return requestV1{}, err
	}
	if err := verifyExactJSONKeysV1(raw); err != nil {
		return requestV1{}, err
	}
	if request.SchemaVersion != "w2_adr_ballot_preparation_request.v1" || request.RequestID != "CPR-W2-ADR-v1" || request.RequestKind != "adr_readiness_and_dependency_closure_only" || request.Scope != "W2-P4-ADR-001-002-003-004-005-008-010-011" {
		return requestV1{}, fmt.Errorf("准备契约身份漂移")
	}
	if request.Status != "prepared_unregistered" || request.RegistrationStatus != "not_registered" || request.DecisionRequestStatus != "not_created" || request.ApprovalStatus != "not_requested" || request.ImplementationStatus != "prohibited" || request.EvidenceStatus != "candidate_only" || request.ImplementationUnlocked || request.BallotEnabled || request.CanonicalBallotStatus != "not_created" || request.OwnerRoleSetStatus != "not_derived" {
		return requestV1{}, fmt.Errorf("准备契约失败关闭状态漂移")
	}
	if request.DecisionDocument != (artifactRefV1{Path: closurePathV1, SHA256: closureSHA256V1}) || request.GateManifest != (artifactRefV1{Path: gateManifestPathV1, SHA256: gateManifestSHA256V1}) || !reflect.DeepEqual(request.LinkedRequests, linkedRefsV1()) {
		return requestV1{}, fmt.Errorf("准备契约 source ref 漂移")
	}
	wantValidatorPaths := []string{validatorPathV1, guardPathV1}
	if len(request.ValidatorSources) != len(wantValidatorPaths) {
		return requestV1{}, fmt.Errorf("validator source count 漂移")
	}
	for index, ref := range request.ValidatorSources {
		if ref.Path != wantValidatorPaths[index] || !shaPatternV1(ref.SHA256) || ref.SHA256 == "sha256:"+strings.Repeat("0", 64) {
			return requestV1{}, fmt.Errorf("validator source[%d] 漂移", index)
		}
	}
	wantIDs := []string{"W2-ADR-001", "W2-ADR-002", "W2-ADR-003", "W2-ADR-004", "W2-ADR-005", "W2-ADR-008", "W2-ADR-010", "W2-ADR-011"}
	if !reflect.DeepEqual(request.RequiredADRIDs, wantIDs) {
		return requestV1{}, fmt.Errorf("ADR exact-set=%v", request.RequiredADRIDs)
	}
	if err := verifyItemsV1(request.Items); err != nil {
		return requestV1{}, err
	}
	if err := verifyHardEdgesV1(request.RequiredADRIDs, request.HardPrerequisiteEdges); err != nil {
		return requestV1{}, err
	}
	wantGates := []string{"W2-A1", "W2-A2", "W2-B0a", "W2-B0b", "W2-B1"}
	if !reflect.DeepEqual(request.BlockedProductionGates, wantGates) {
		return requestV1{}, fmt.Errorf("blocked gates=%v", request.BlockedProductionGates)
	}
	wantForbidden := []string{
		"accept_recommendation", "authorize_production", "create_approval_summary", "create_ballot", "create_canonical_contract_manifest",
		"create_decision_request", "create_idl_manifest", "create_test_manifest", "create_vector_manifest", "derive_final_owner_exact_set",
		"enable_ballot", "formal_freeze", "record_actor", "record_owner_approval", "record_platform_review", "record_reviewer",
		"record_selected_choice", "register_candidate_evidence", "reject_keep_blocked", "reject_recommendation", "select_owner_option",
		"transition_gate", "unlock_implementation",
	}
	if !reflect.DeepEqual(request.ForbiddenCapabilities, wantForbidden) || request.BlockStatement != blockStatementV1 {
		return requestV1{}, fmt.Errorf("禁止能力或阻断声明漂移")
	}
	if err := verifyForbiddenKeysV1(raw); err != nil {
		return requestV1{}, err
	}
	return request, nil
}

func verifyItemsV1(items []itemV1) error {
	specs := itemSpecsV1()
	if len(items) != len(specs) {
		return fmt.Errorf("items=%d want=%d", len(items), len(specs))
	}
	for index, spec := range specs {
		item := items[index]
		if item.ADRID != spec.ADRID || item.SemanticRecommendationStatus != "recommendation_present_in_closure" || item.CanonicalBallotStatus != "not_created" || item.ReadinessStatus != spec.Readiness || !reflect.DeepEqual(item.P4ConflictRefs, spec.P4Refs) || !reflect.DeepEqual(item.JointDispositionRefs, spec.JointRefs) || !reflect.DeepEqual(item.UnmetEvidence, spec.Evidence) || item.BlockCode != spec.BlockCode {
			return fmt.Errorf("item[%d] 漂移: %+v", index, item)
		}
	}
	return nil
}

func itemSpecsV1() []itemSpecV1 {
	return []itemSpecV1{
		{ADRID: "W2-ADR-001", Readiness: "candidate_incomplete_not_ballot_ready", P4Refs: []string{"P4-C01", "P4-C02", "P4-C07"}, JointRefs: []string{"R00-D14", "R01-D02", "R01-D05", "R02-D09", "R02-D19", "R04-D10", "R04-D13", "R04-D14", "R04-D19"}, Evidence: []string{"AUTHORITY_ENVELOPE_EXACT_SCHEMA_MISSING", "PRODUCTION_TABLE_STATE_MAPPING_MISSING", "R02_AGGREGATE_BUILD_TRUST_CLOSURE_MISSING", "SLOT_OBSERVATION_IDENTITY_MISSING"}, BlockCode: "W2_ADR_001_CANDIDATE_INCOMPLETE"},
		{ADRID: "W2-ADR-002", Readiness: "candidate_incomplete_not_ballot_ready", P4Refs: []string{"P4-C03", "P4-C04", "P4-C07"}, JointRefs: []string{"R01-D01", "R02-D19"}, Evidence: []string{"DIGEST_OLD_TO_NEW_FIELD_MAPPING_MISSING", "DIGEST_VERSION_POLICY_MISSING", "DIGEST_GOLDEN_VECTORS_MISSING", "R02_AGGREGATE_BUILD_TRUST_CLOSURE_MISSING"}, BlockCode: "W2_ADR_002_CANDIDATE_INCOMPLETE"},
		{ADRID: "W2-ADR-003", Readiness: "candidate_incomplete_not_ballot_ready", P4Refs: []string{"P4-C13"}, JointRefs: []string{"R01-D05", "R03-D04", "R03-D05", "R03-D06", "R03-D08", "R03-D11", "R04-D16", "R04-D18", "R04-D19"}, Evidence: []string{"ACTIVATION_COMMAND_IDENTITY_EXACT_SCHEMA_MISSING", "BUSINESS_COMMAND_RECEIPT_IDL_MISSING", "UNKNOWN_OUTCOME_QUERY_VECTORS_MISSING"}, BlockCode: "W2_ADR_003_CANDIDATE_INCOMPLETE"},
		{ADRID: "W2-ADR-004", Readiness: "candidate_incomplete_not_ballot_ready", P4Refs: []string{"P4-C14"}, JointRefs: []string{"R01-D02", "R02-D18", "R03-D03", "R03-D10", "R03-D11", "R04-D17", "R04-D18", "R04-D19"}, Evidence: []string{"AUTHENTICATED_QUERY_EXACT_SCHEMA_MISSING", "BIDIRECTIONAL_RPC_SECURITY_BOUNDARY_MISSING", "SIGNATURE_KEY_ROTATION_DISPOSITION_MISSING"}, BlockCode: "W2_ADR_004_CANDIDATE_INCOMPLETE"},
		{ADRID: "W2-ADR-005", Readiness: "candidate_incomplete_not_ballot_ready", P4Refs: []string{"P4-C11"}, JointRefs: append(sequenceV1("R00-D", 1, 14), "R03-D01", "R04-D01", "R04-D08", "R04-D09", "R04-D10", "R04-D11", "R04-D12", "R04-D13", "R04-D14", "R04-D15"), Evidence: []string{"BILLING_POLICY_EXACT_VALUES_MISSING", "PROVIDER_CAPABILITY_MATRIX_MISSING", "R00_LIVE_CANDIDATE_SUBMISSION_MISSING"}, BlockCode: "W2_ADR_005_CANDIDATE_INCOMPLETE"},
		{ADRID: "W2-ADR-008", Readiness: "joint_disposition_pending_not_ballot_ready", P4Refs: []string{"P4-C07", "P4-C08"}, JointRefs: []string{"R02-D19"}, Evidence: []string{"R02_AGGREGATE_BUILD_TRUST_CLOSURE_MISSING", "R02_D19_OWNER_DECISION_PENDING"}, BlockCode: "W2_ADR_008_JOINT_DISPOSITION_PENDING"},
		{ADRID: "W2-ADR-010", Readiness: "joint_disposition_pending_not_ballot_ready", P4Refs: []string{"P4-C07", "P4-C08"}, JointRefs: []string{"R02-D19"}, Evidence: []string{"R02_AGGREGATE_BUILD_TRUST_CLOSURE_MISSING", "R02_D19_OWNER_DECISION_PENDING"}, BlockCode: "W2_ADR_010_JOINT_DISPOSITION_PENDING"},
		{ADRID: "W2-ADR-011", Readiness: "candidate_incomplete_not_ballot_ready", P4Refs: []string{}, JointRefs: []string{"R01-D05", "R03-D01", "R03-D02", "R03-D08", "R03-D09", "R03-D10", "R03-D11", "R04-D16", "R04-D17", "R04-D18", "R04-D19"}, Evidence: []string{"ACTIVATION_FIELD_CROSS_GATE_MAPPING_MISSING", "R01_D05_SLOT_SCOPE_PENDING", "SLOT_ORDINAL_UNIQUENESS_DOMAIN_MISSING"}, BlockCode: "W2_ADR_011_CANDIDATE_INCOMPLETE"},
	}
}

func verifyHardEdgesV1(adrIDs []string, edges []edgeV1) error {
	want := []edgeV1{{PrerequisiteADRID: "W2-ADR-005", DependentADRID: "W2-ADR-011"}}
	if !reflect.DeepEqual(edges, want) {
		return fmt.Errorf("hard prerequisite edges=%v", edges)
	}
	nodes := make(map[string]struct{}, len(adrIDs))
	for _, id := range adrIDs {
		nodes[id] = struct{}{}
	}
	adjacency := make(map[string][]string)
	indegree := make(map[string]int, len(nodes))
	seen := make(map[string]struct{})
	for _, edge := range edges {
		if _, ok := nodes[edge.PrerequisiteADRID]; !ok {
			return fmt.Errorf("unknown prerequisite %s", edge.PrerequisiteADRID)
		}
		if _, ok := nodes[edge.DependentADRID]; !ok || edge.PrerequisiteADRID == edge.DependentADRID {
			return fmt.Errorf("invalid dependent %s", edge.DependentADRID)
		}
		key := edge.PrerequisiteADRID + "->" + edge.DependentADRID
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate edge %s", key)
		}
		seen[key] = struct{}{}
		adjacency[edge.PrerequisiteADRID] = append(adjacency[edge.PrerequisiteADRID], edge.DependentADRID)
		indegree[edge.DependentADRID]++
	}
	queue := make([]string, 0, len(nodes))
	for id := range nodes {
		if indegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adjacency[id] {
			indegree[next]--
			if indegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if visited != len(nodes) {
		return fmt.Errorf("hard prerequisite graph contains cycle")
	}
	return nil
}

func verifyLinkedRequestsV1(t *testing.T, repoRoot string, refs []linkedRefV1) {
	t.Helper()
	want := linkedRefsV1()
	if len(refs) != len(want) {
		t.Fatalf("linked requests=%d", len(refs))
	}
	for index, ref := range refs {
		if ref != want[index] {
			t.Fatalf("linked request[%d]=%+v", index, ref)
		}
		verifyRawSHA256V1(t, repoRoot, artifactRefV1{Path: ref.Path, SHA256: ref.SHA256})
		var identity struct {
			RequestID string `json:"request_id"`
		}
		if err := json.Unmarshal(readRegularFileV1(t, repoRoot, ref.Path), &identity); err != nil || identity.RequestID != ref.RequestID {
			t.Fatalf("linked request identity %s: %v", ref.Path, err)
		}
	}
}

func linkedRefsV1() []linkedRefV1 {
	return []linkedRefV1{
		{RequestID: "CPR-W2-R00-v1", Path: "docs/design/agent/approvals/w2-r00-candidate-preparation-requests/CPR-W2-R00-v1.json", SHA256: "sha256:6d9cd4a033d19c127fcfec04e975abdcb047247dcaa56c9fd381068b6977c836"},
		{RequestID: "DR-W2-R01-v1", Path: "docs/design/agent/approvals/w2-r01-owner-decision-requests/DR-W2-R01-v1.json", SHA256: "sha256:676c4f83a1e7570c5ac41e3d0ffc8556fb936b0b363b93a6c7b79b2da7552018"},
		{RequestID: "DR-W2-R02-v1", Path: "docs/design/agent/approvals/w2-r02-owner-decision-requests/DR-W2-R02-v1.json", SHA256: "sha256:4b6356f9d6b4da7adf348c2207135e2cebd8c972349f84c67ade274f6d274fe9"},
		{RequestID: "DR-W2-R03-v1", Path: "docs/design/agent/approvals/w2-r03-owner-decision-requests/DR-W2-R03-v1.json", SHA256: "sha256:d0e229c8b2fbaaee21b67a87155d6f9607f08e581d36419ae3833ae65b2d7c6d"},
		{RequestID: "DR-W2-R04-v1", Path: "docs/design/agent/approvals/w2-r04-owner-decision-requests/DR-W2-R04-v1.json", SHA256: "sha256:d8806af1289aff1b8a790bdbf861c97a7c348f70aa83213760bdc28b318cd0e7"},
	}
}

func verifyValidatorSourcesV1(t *testing.T, repoRoot string, refs []artifactRefV1) {
	t.Helper()
	specs := []sourceSpecV1{
		{Path: validatorPathV1, PackageName: "w2adrballotpreparation_test", Tests: []string{"TestW2ADRBallotPreparationV1", "TestW2ADRBallotPreparationV1AuthorityMutations", "TestW2ADRBallotPreparationV1StrictJSON"}, Imports: []string{"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "fmt", "go/ast", "go/parser", "go/token", "io", "os", "path/filepath", "reflect", "runtime", "sort", "strings", "testing", "unicode/utf8"}},
		{Path: guardPathV1, PackageName: "w2adrballotpreparationguard_test", Tests: []string{"TestW2ADRBallotPreparationGuardV1", "TestW2ADRBallotPreparationGuardV1AuthorityMutations", "TestW2ADRBallotPreparationGuardV1StrictAndSourceBoundaries"}, Imports: []string{"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "fmt", "go/ast", "go/parser", "go/token", "io", "os", "path/filepath", "reflect", "runtime", "sort", "strings", "testing", "unicode/utf8"}},
	}
	if len(refs) != len(specs) {
		t.Fatalf("validator sources=%d", len(refs))
	}
	for index, spec := range specs {
		if refs[index].Path != spec.Path {
			t.Fatalf("validator[%d]=%s", index, refs[index].Path)
		}
		verifyRawSHA256V1(t, repoRoot, refs[index])
		verifySourceV1(t, repoRoot, spec)
	}
}

func verifyLiveGatesV1(t *testing.T, repoRoot string) {
	t.Helper()
	if err := validateGateManifestRawV1(readRegularFileV1(t, repoRoot, gateManifestPathV1)); err != nil {
		t.Fatal(err)
	}
}

func validateGateManifestRawV1(raw []byte) error {
	if err := validateGateJSONShapeV1(raw); err != nil {
		return err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return err
	}
	gatesRaw, exists := root["gates"]
	if !exists || isJSONNullV1(gatesRaw) {
		return fmt.Errorf("Gate manifest gates 缺失或为 null")
	}
	var gateObjects []json.RawMessage
	if err := json.Unmarshal(gatesRaw, &gateObjects); err != nil {
		return err
	}
	var manifest struct {
		Gates []struct {
			Gate              string            `json:"gate"`
			Status            string            `json:"status"`
			CandidateEvidence []json.RawMessage `json:"candidate_evidence"`
			Freeze            json.RawMessage   `json:"freeze"`
			ReopenException   json.RawMessage   `json:"reopen_exception"`
		} `json:"gates"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	if len(gateObjects) != len(manifest.Gates) {
		return fmt.Errorf("Gate manifest raw/typed count 漂移")
	}
	wantCandidates := map[string]int{"W2-R00": 0, "W2-R01": 1, "W2-R02": 0, "W2-R03": 0, "W2-R04": 1}
	seen := make(map[string]bool, len(wantCandidates))
	for index, gate := range manifest.Gates {
		var gateObject map[string]json.RawMessage
		if err := json.Unmarshal(gateObjects[index], &gateObject); err != nil {
			return err
		}
		if err := requireKeysV1(fmt.Sprintf("gate[%d]", index), gateObject, []string{"blockers", "candidate_evidence", "freeze", "gate", "reopen_exception", "required_owner_roles", "status"}); err != nil {
			return err
		}
		if isJSONNullV1(gateObject["candidate_evidence"]) {
			return fmt.Errorf("gate %s candidate_evidence 不得为 null", gate.Gate)
		}
		candidateCount, tracked := wantCandidates[gate.Gate]
		if !tracked {
			if gate.Gate == "W2-P4-ADR" {
				return fmt.Errorf("准备契约不得创建 live P4 ADR Gate")
			}
			continue
		}
		if seen[gate.Gate] || gate.Status != "expansion_frozen" || len(gate.CandidateEvidence) != candidateCount || !isJSONNullV1(gate.Freeze) || !isJSONNullV1(gate.ReopenException) {
			return fmt.Errorf("live gate[%d] 漂移: %+v", index, gate)
		}
		seen[gate.Gate] = true
		for _, candidate := range gate.CandidateEvidence {
			var candidateObject map[string]json.RawMessage
			if err := json.Unmarshal(candidate, &candidateObject); err != nil {
				return err
			}
			if err := requireKeysV1("candidate", candidateObject, []string{"contract_manifest_path", "contract_manifest_sha256", "coverage", "scope", "target_tests", "vector_ids"}); err != nil {
				return err
			}
			var projection struct {
				Coverage               string   `json:"coverage"`
				Scope                  string   `json:"scope"`
				ContractManifestPath   string   `json:"contract_manifest_path"`
				ContractManifestSHA256 string   `json:"contract_manifest_sha256"`
				VectorIDs              []string `json:"vector_ids"`
				TargetTests            []string `json:"target_tests"`
			}
			if err := json.Unmarshal(candidate, &projection); err != nil {
				return err
			}
			if projection.Coverage != "partial_candidate" || projection.Scope == "" || projection.ContractManifestPath == "" || !shaPatternV1(projection.ContractManifestSHA256) || len(projection.VectorIDs) == 0 || len(projection.TargetTests) == 0 {
				return fmt.Errorf("live candidate 不得为空壳或冒充 full: %s", candidate)
			}
		}
	}
	for gate := range wantCandidates {
		if !seen[gate] {
			return fmt.Errorf("live Gate 缺少 %s", gate)
		}
	}
	return nil
}

func verifyArtifactRefV1(t *testing.T, repoRoot string, ref artifactRefV1, wantPath, wantSHA256 string) {
	t.Helper()
	if ref.Path != wantPath {
		t.Fatalf("artifact path=%s want=%s", ref.Path, wantPath)
	}
	if ref.SHA256 != wantSHA256 {
		t.Fatalf("artifact SHA=%s want=%s", ref.SHA256, wantSHA256)
	}
	verifyRawSHA256V1(t, repoRoot, ref)
}

func verifyRawSHA256V1(t *testing.T, repoRoot string, ref artifactRefV1) {
	t.Helper()
	if !shaPatternV1(ref.SHA256) || ref.SHA256 == "sha256:"+strings.Repeat("0", 64) {
		t.Fatalf("非法 SHA: %s", ref.SHA256)
	}
	raw := readRegularFileV1(t, repoRoot, ref.Path)
	digest := sha256.Sum256(raw)
	if got := "sha256:" + hex.EncodeToString(digest[:]); got != ref.SHA256 {
		t.Fatalf("%s raw SHA=%s want=%s", ref.Path, got, ref.SHA256)
	}
}

func verifySourceV1(t *testing.T, repoRoot string, spec sourceSpecV1) {
	t.Helper()
	directory := filepath.Join(repoRoot, filepath.Dir(spec.Path))
	verifySingleFileDirectoryV1(t, directory, filepath.Base(spec.Path))
	raw := readRegularFileV1(t, repoRoot, spec.Path)
	if bytes.Contains(raw, []byte("//"+"go:build")) || bytes.Contains(raw, []byte("// "+"+build")) || bytes.Contains(raw, []byte("go:"+"embed")) {
		t.Fatalf("validator source 不得包含 build tag/embed: %s", spec.Path)
	}
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, spec.Path), raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	if file.Name.Name != spec.PackageName {
		t.Fatalf("package=%s want=%s", file.Name.Name, spec.PackageName)
	}
	imports := make([]string, 0, len(file.Imports))
	for _, imported := range file.Imports {
		if imported.Name != nil {
			t.Fatalf("validator import 不得使用 alias: %s", spec.Path)
		}
		path := strings.Trim(imported.Path.Value, `"`)
		if strings.Contains(path, ".") || strings.Contains(path, "agent/internal") {
			t.Fatalf("validator 只能使用 stdlib: %s", path)
		}
		imports = append(imports, path)
	}
	sort.Strings(imports)
	if spec.Imports != nil && !reflect.DeepEqual(imports, spec.Imports) {
		t.Fatalf("%s imports=%v want=%v", spec.Path, imports, spec.Imports)
	}
	tests := make([]string, 0)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if function.Name.Name == "init" || function.Name.Name == "TestMain" || strings.HasPrefix(function.Name.Name, "Fuzz") || strings.HasPrefix(function.Name.Name, "Benchmark") || strings.HasPrefix(function.Name.Name, "Example") {
			t.Fatalf("validator source 含禁止入口: %s", function.Name.Name)
		}
		if strings.HasPrefix(function.Name.Name, "Test") {
			tests = append(tests, function.Name.Name)
		}
	}
	sort.Strings(tests)
	wantTests := append([]string{}, spec.Tests...)
	sort.Strings(wantTests)
	if !reflect.DeepEqual(tests, wantTests) {
		t.Fatalf("%s tests=%v want=%v", spec.Path, tests, wantTests)
	}
}

func verifyForbiddenKeysV1(raw []byte) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	forbidden := map[string]struct{}{}
	for _, key := range []string{"accepted", "accepted_option_id", "actor_id", "allowed_option_ids", "approval_refs", "approver_role", "authority_record", "ballot_authority", "candidate_evidence", "candidate_owner_roles", "canonical_ballot_owner", "commit_sha", "decision_status", "disposition", "freeze", "head_sha", "owner_approvals", "recommended_option_id", "reopen_exception", "required_owner_roles", "review_id", "review_state", "review_url", "reviewer_id", "selected_option", "selected_option_id", "team_id", "vote", "voter_id", "votes"} {
		forbidden[key] = struct{}{}
	}
	return walkForbiddenV1("$", value, forbidden)
}

func walkForbiddenV1(path string, value any, forbidden map[string]struct{}) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, denied := forbidden[key]; denied {
				return fmt.Errorf("发现禁止字段 %s.%s", path, key)
			}
			if err := walkForbiddenV1(path+"."+key, child, forbidden); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range typed {
			if err := walkForbiddenV1(fmt.Sprintf("%s[%d]", path, index), child, forbidden); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyExactJSONKeysV1(raw []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return err
	}
	wantRoot := []string{"approval_status", "ballot_enabled", "block_statement", "blocked_production_gates", "canonical_ballot_status", "decision_document", "decision_request_status", "evidence_status", "forbidden_capabilities", "gate_manifest", "hard_prerequisite_edges", "implementation_status", "implementation_unlocked", "items", "linked_requests", "owner_role_set_status", "registration_status", "request_id", "request_kind", "required_adr_ids", "schema_version", "scope", "status", "validator_sources"}
	if err := requireKeysV1("root", root, wantRoot); err != nil {
		return err
	}
	for _, field := range []string{"decision_document", "gate_manifest"} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(root[field], &object); err != nil {
			return err
		}
		if err := requireKeysV1(field, object, []string{"path", "sha256"}); err != nil {
			return err
		}
	}
	var linked []map[string]json.RawMessage
	if err := json.Unmarshal(root["linked_requests"], &linked); err != nil {
		return err
	}
	for index, object := range linked {
		if err := requireKeysV1(fmt.Sprintf("linked[%d]", index), object, []string{"path", "request_id", "sha256"}); err != nil {
			return err
		}
	}
	var validators []map[string]json.RawMessage
	if err := json.Unmarshal(root["validator_sources"], &validators); err != nil {
		return err
	}
	for index, object := range validators {
		if err := requireKeysV1(fmt.Sprintf("validator[%d]", index), object, []string{"path", "sha256"}); err != nil {
			return err
		}
	}
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(root["items"], &items); err != nil {
		return err
	}
	wantItem := []string{"adr_id", "block_code", "canonical_ballot_status", "joint_disposition_refs", "p4_conflict_refs", "readiness_status", "semantic_recommendation_status", "unmet_evidence"}
	for index, object := range items {
		if err := requireKeysV1(fmt.Sprintf("item[%d]", index), object, wantItem); err != nil {
			return err
		}
	}
	var edges []map[string]json.RawMessage
	if err := json.Unmarshal(root["hard_prerequisite_edges"], &edges); err != nil {
		return err
	}
	for index, object := range edges {
		if err := requireKeysV1(fmt.Sprintf("edge[%d]", index), object, []string{"dependent_adr_id", "prerequisite_adr_id"}); err != nil {
			return err
		}
	}
	return nil
}

func requireKeysV1(label string, object map[string]json.RawMessage, want []string) error {
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("%s keys=%v want=%v", label, got, want)
	}
	return nil
}

func strictDecodeRequestV1(raw []byte) (requestV1, error) {
	if !utf8.Valid(raw) {
		return requestV1{}, fmt.Errorf("JSON UTF-8 非法")
	}
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
	if err := readJSONValueV1(decoder, false); err != nil {
		return err
	}
	return requireEOFV1(decoder)
}

func validateGateJSONShapeV1(raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("Gate JSON UTF-8 非法")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := readJSONValueV1(decoder, true); err != nil {
		return err
	}
	return requireEOFV1(decoder)
}

func readJSONValueV1(decoder *json.Decoder, allowNull bool) error {
	tokenValue, err := decoder.Token()
	if err != nil {
		return err
	}
	if tokenValue == nil {
		if allowNull {
			return nil
		}
		return fmt.Errorf("JSON null 被禁止")
	}
	if _, number := tokenValue.(json.Number); number {
		return fmt.Errorf("JSON number 被禁止")
	}
	delimiter, container := tokenValue.(json.Delim)
	if !container {
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
				return fmt.Errorf("JSON duplicate key %s", key)
			}
			seen[key] = struct{}{}
			if err := readJSONValueV1(decoder, allowNull); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim('}') {
			return fmt.Errorf("JSON object 未闭合: %v", endErr)
		}
	case '[':
		for decoder.More() {
			if err := readJSONValueV1(decoder, allowNull); err != nil {
				return err
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim(']') {
			return fmt.Errorf("JSON array 未闭合: %v", endErr)
		}
	default:
		return fmt.Errorf("unexpected delimiter %v", delimiter)
	}
	return nil
}

func requireEOFV1(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON 存在尾随值")
		}
		return err
	}
	return nil
}

func readRegularFileV1(t *testing.T, repoRoot, relative string) []byte {
	t.Helper()
	path := filepath.Join(repoRoot, filepath.FromSlash(relative))
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("%s 必须是 mode 0644 regular file: %s", relative, info.Mode())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verifySingleFileDirectoryV1(t *testing.T, directory, want string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if !reflect.DeepEqual(names, []string{want}) {
		t.Fatalf("directory exact-set=%v want=%s", names, want)
	}
}

func sequenceV1(prefix string, first, last int) []string {
	result := make([]string, 0, last-first+1)
	for value := first; value <= last; value++ {
		result = append(result, fmt.Sprintf("%s%02d", prefix, value))
	}
	return result
}

func isJSONNullV1(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func regexpSHA256V1() func(string) bool {
	return func(value string) bool {
		if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
			return false
		}
		_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
		return err == nil
	}
}

func repoRootV1(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", "..", ".."))
}
