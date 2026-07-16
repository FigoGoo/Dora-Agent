// Package w2adrballotpreparationguard_test 从语义 validator 包外固定 ADR ballot 准备请求的失败关闭边界。
package w2adrballotpreparationguard_test

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
	mainPathV1           = "agent/tests/contract/w2adrballotpreparation/adr_ballot_preparation_v1_test.go"
	guardPathV1          = "agent/tests/contract/w2adrballotpreparationguard/adr_ballot_preparation_guard_v1_test.go"
	closureSHA256V1      = "sha256:29a39b727ba9089a8575b9807e090e97df5dcc6ae190b3f67dcae5b05ca01f8f"
	gateManifestSHA256V1 = "sha256:a98059cfa4971f0123565d63ad56ab4d202ad354a0971bbecf99a0711bee616e"
	blockV1              = "本 CPR 只固定 W2-ADR-001/002/003/004/005/008/010/011 的准备态、P4 冲突、跨 Gate 联合裁决引用和 ADR-005→ADR-011 硬前置；它不是 decision request 或 ballot，不提供 accept/reject、选择、批准、Reviewer/Actor、candidate evidence、状态迁移或 implementation unlock。八项 ADR 的 canonical ballot 均未创建；R02-D19 只联合裁决 ADR-001/002/008/010 的 A1 scope，不代表 ADR semantic acceptance，也不得自动传播任何选择。W2-R00～R04 继续 expansion_frozen 且 freeze/reopen_exception 为 null，所有生产 Gate 继续失败关闭。"
)

type (
	artifactRefV1 struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	linkedRequestV1 struct {
		RequestID string `json:"request_id"`
		Path      string `json:"path"`
		SHA256    string `json:"sha256"`
	}
	itemV1 struct {
		ADRID                        string   `json:"adr_id"`
		SemanticRecommendationStatus string   `json:"semantic_recommendation_status"`
		CanonicalBallotStatus        string   `json:"canonical_ballot_status"`
		ReadinessStatus              string   `json:"readiness_status"`
		P4ConflictRefs               []string `json:"p4_conflict_refs"`
		JointDispositionRefs         []string `json:"joint_disposition_refs"`
		UnmetEvidence                []string `json:"unmet_evidence"`
		BlockCode                    string   `json:"block_code"`
	}
	prerequisiteEdgeV1 struct {
		PrerequisiteADRID string `json:"prerequisite_adr_id"`
		DependentADRID    string `json:"dependent_adr_id"`
	}
	requestV1 struct {
		SchemaVersion          string               `json:"schema_version"`
		RequestID              string               `json:"request_id"`
		RequestKind            string               `json:"request_kind"`
		Scope                  string               `json:"scope"`
		Status                 string               `json:"status"`
		RegistrationStatus     string               `json:"registration_status"`
		DecisionRequestStatus  string               `json:"decision_request_status"`
		ApprovalStatus         string               `json:"approval_status"`
		ImplementationStatus   string               `json:"implementation_status"`
		EvidenceStatus         string               `json:"evidence_status"`
		ImplementationUnlocked bool                 `json:"implementation_unlocked"`
		BallotEnabled          bool                 `json:"ballot_enabled"`
		CanonicalBallotStatus  string               `json:"canonical_ballot_status"`
		OwnerRoleSetStatus     string               `json:"owner_role_set_status"`
		DecisionDocument       artifactRefV1        `json:"decision_document"`
		GateManifest           artifactRefV1        `json:"gate_manifest"`
		LinkedRequests         []linkedRequestV1    `json:"linked_requests"`
		ValidatorSources       []artifactRefV1      `json:"validator_sources"`
		RequiredADRIDs         []string             `json:"required_adr_ids"`
		Items                  []itemV1             `json:"items"`
		HardPrerequisiteEdges  []prerequisiteEdgeV1 `json:"hard_prerequisite_edges"`
		BlockedProductionGates []string             `json:"blocked_production_gates"`
		ForbiddenCapabilities  []string             `json:"forbidden_capabilities"`
		BlockStatement         string               `json:"block_statement"`
	}
	itemWantV1   struct{ id, readiness, p4, joint, unmet, block string }
	sourceSpecV1 struct {
		Path, PackageName string
		Imports, Tests    []string
	}
)

func TestW2ADRBallotPreparationGuardV1(t *testing.T) {
	t.Parallel()
	root := repoRootV1(t)
	verifySingleFileV1(t, root, requestPathV1)
	raw := readFileV1(t, root, requestPathV1)
	request, err := decodeRequestV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	verifyRootKeysV1(t, raw)
	if err := semanticErrorV1(request); err != nil {
		t.Fatal(err)
	}
	verifyForbiddenAuthorityV1(t, raw)
	verifyArtifactV1(t, root, request.DecisionDocument, "docs/design/cross-module/w2-owner-decision-closure-v1.md", closureSHA256V1)
	verifyArtifactV1(t, root, request.GateManifest, "docs/design/agent/approvals/w2-review-freeze-manifest.json", gateManifestSHA256V1)
	verifyGateManifestV1(t, readFileV1(t, root, request.GateManifest.Path))
	linked := strings.Split("CPR-W2-R00-v1|docs/design/agent/approvals/w2-r00-candidate-preparation-requests/CPR-W2-R00-v1.json|sha256:6d9cd4a033d19c127fcfec04e975abdcb047247dcaa56c9fd381068b6977c836,DR-W2-R01-v1|docs/design/agent/approvals/w2-r01-owner-decision-requests/DR-W2-R01-v1.json|sha256:676c4f83a1e7570c5ac41e3d0ffc8556fb936b0b363b93a6c7b79b2da7552018,DR-W2-R02-v1|docs/design/agent/approvals/w2-r02-owner-decision-requests/DR-W2-R02-v1.json|sha256:4b6356f9d6b4da7adf348c2207135e2cebd8c972349f84c67ade274f6d274fe9,DR-W2-R03-v1|docs/design/agent/approvals/w2-r03-owner-decision-requests/DR-W2-R03-v1.json|sha256:d0e229c8b2fbaaee21b67a87155d6f9607f08e581d36419ae3833ae65b2d7c6d,DR-W2-R04-v1|docs/design/agent/approvals/w2-r04-owner-decision-requests/DR-W2-R04-v1.json|sha256:d8806af1289aff1b8a790bdbf861c97a7c348f70aa83213760bdc28b318cd0e7", ",")
	for index, encoded := range linked {
		want := strings.Split(encoded, "|")
		if request.LinkedRequests[index].RequestID != want[0] || request.LinkedRequests[index].SHA256 != want[2] {
			t.Fatalf("linked_requests[%d]=%q want=%q", index, request.LinkedRequests[index].RequestID, want[0])
		}
		verifyArtifactV1(t, root, artifactRefV1{Path: request.LinkedRequests[index].Path, SHA256: request.LinkedRequests[index].SHA256}, want[1], want[2])
	}
}

func TestW2ADRBallotPreparationGuardV1AuthorityMutations(t *testing.T) {
	t.Parallel()
	raw := readFileV1(t, repoRootV1(t), requestPathV1)
	for name, field := range map[string]string{
		"selected": `"selected_option_id":"accept_recommendation",`, "approval": `"approval":{},`,
		"reviewer": `"reviewer_id":"someone",`, "actor": `"actor_id":"someone",`,
	} {
		t.Run(name, func(t *testing.T) {
			mutated := bytes.Replace(raw, []byte(`"request_kind":`), []byte(field+`"request_kind":`), 1)
			if _, err := decodeRequestV1(mutated); err == nil {
				t.Fatalf("必须拒绝 authority root mutation %s", name)
			}
		})
	}
	for name, field := range map[string]string{
		"options": `"allowed_option_ids":["accept"],`, "owner": `"canonical_ballot_owner":"owner",`,
	} {
		t.Run(name, func(t *testing.T) {
			mutated := bytes.Replace(raw, []byte(`"readiness_status":`), []byte(field+`"readiness_status":`), 1)
			if _, err := decodeRequestV1(mutated); err == nil {
				t.Fatalf("必须拒绝 authority item mutation %s", name)
			}
		})
	}
	mutations := map[string][]byte{
		"candidate_ready": bytes.Replace(raw, []byte(`"readiness_status": "candidate_incomplete_not_ballot_ready"`), []byte(`"readiness_status": "awaiting_owner_decision"`), 1),
		"008_delegated":   bytes.Replace(raw, []byte(`"readiness_status": "joint_disposition_pending_not_ballot_ready"`), []byte(`"readiness_status": "delegated_decision"`), 1),
		"owner_final":     bytes.Replace(raw, []byte(`"owner_role_set_status": "not_derived"`), []byte(`"owner_role_set_status": "final"`), 1),
		"unlock":          bytes.Replace(raw, []byte(`"implementation_unlocked": false`), []byte(`"implementation_unlocked": true`), 1),
	}
	for name, mutated := range mutations {
		t.Run(name, func(t *testing.T) {
			request, err := decodeRequestV1(mutated)
			if err == nil {
				err = semanticErrorV1(request)
			}
			if err == nil {
				t.Fatalf("必须拒绝 semantic authority mutation %s", name)
			}
		})
	}
}

func TestW2ADRBallotPreparationGuardV1StrictAndSourceBoundaries(t *testing.T) {
	t.Parallel()
	invalidUTF8 := append([]byte(`{"x":"`), 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	for name, raw := range map[string][]byte{
		"duplicate": []byte(`{"x":"a","x":"b"}`), "null": []byte(`{"x":null}`),
		"number": []byte(`{"x":1}`), "trailing": []byte(`{"x":"a"}{}`), "utf8": invalidUTF8,
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateJSONV1(raw); err == nil {
				t.Fatalf("必须拒绝 %s JSON", name)
			}
		})
	}
	if _, err := decodeRequestV1([]byte(`{"schema_version":"x","future":true}`)); err == nil {
		t.Fatal("typed projection 必须拒绝未知字段")
	}
	root := repoRootV1(t)
	request, err := decodeRequestV1(readFileV1(t, root, requestPathV1))
	if err != nil {
		t.Fatal(err)
	}
	for index, spec := range sourceSpecsV1() {
		if index >= len(request.ValidatorSources) {
			t.Fatalf("validator_sources 缺少 %s", spec.Path)
		}
		verifyArtifactV1(t, root, request.ValidatorSources[index], spec.Path, request.ValidatorSources[index].SHA256)
		verifySourceV1(t, root, spec)
	}
	if len(request.ValidatorSources) != len(sourceSpecsV1()) {
		t.Fatalf("validator_sources=%v", request.ValidatorSources)
	}
}

func decodeRequestV1(raw []byte) (requestV1, error) {
	if err := validateJSONV1(raw); err != nil {
		return requestV1{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request requestV1
	if err := decoder.Decode(&request); err != nil {
		return requestV1{}, err
	}
	return request, eofV1(decoder)
}

func semanticErrorV1(request requestV1) error {
	if request.SchemaVersion != "w2_adr_ballot_preparation_request.v1" || request.RequestID != "CPR-W2-ADR-v1" || request.RequestKind != "adr_readiness_and_dependency_closure_only" || request.Scope != "W2-P4-ADR-001-002-003-004-005-008-010-011" {
		return fmt.Errorf("request identity 漂移")
	}
	if request.Status != "prepared_unregistered" || request.RegistrationStatus != "not_registered" || request.DecisionRequestStatus != "not_created" || request.ApprovalStatus != "not_requested" || request.ImplementationStatus != "prohibited" || request.EvidenceStatus != "candidate_only" || request.ImplementationUnlocked || request.BallotEnabled || request.CanonicalBallotStatus != "not_created" || request.OwnerRoleSetStatus != "not_derived" || request.BlockStatement != blockV1 {
		return fmt.Errorf("request fail-closed status 漂移")
	}
	wants := itemWantsV1()
	if len(request.Items) != len(wants) {
		return fmt.Errorf("items=%d want=%d", len(request.Items), len(wants))
	}
	for index, want := range wants {
		item := request.Items[index]
		if item.ADRID != want.id || item.SemanticRecommendationStatus != "recommendation_present_in_closure" || item.CanonicalBallotStatus != "not_created" || item.ReadinessStatus != want.readiness || strings.Join(item.P4ConflictRefs, ",") != want.p4 || strings.Join(item.JointDispositionRefs, ",") != want.joint || strings.Join(item.UnmetEvidence, ",") != want.unmet || item.BlockCode != want.block {
			return fmt.Errorf("item[%d] 漂移: %+v", index, item)
		}
	}
	if !reflect.DeepEqual(request.RequiredADRIDs, []string{"W2-ADR-001", "W2-ADR-002", "W2-ADR-003", "W2-ADR-004", "W2-ADR-005", "W2-ADR-008", "W2-ADR-010", "W2-ADR-011"}) || !reflect.DeepEqual(request.HardPrerequisiteEdges, []prerequisiteEdgeV1{{"W2-ADR-005", "W2-ADR-011"}}) || !reflect.DeepEqual(request.BlockedProductionGates, []string{"W2-A1", "W2-A2", "W2-B0a", "W2-B0b", "W2-B1"}) {
		return fmt.Errorf("ADR exact-set/edge/blocked gates 漂移")
	}
	wantForbidden := strings.Split("accept_recommendation,authorize_production,create_approval_summary,create_ballot,create_canonical_contract_manifest,create_decision_request,create_idl_manifest,create_test_manifest,create_vector_manifest,derive_final_owner_exact_set,enable_ballot,formal_freeze,record_actor,record_owner_approval,record_platform_review,record_reviewer,record_selected_choice,register_candidate_evidence,reject_keep_blocked,reject_recommendation,select_owner_option,transition_gate,unlock_implementation", ",")
	if !reflect.DeepEqual(request.ForbiddenCapabilities, wantForbidden) || len(request.LinkedRequests) != 5 {
		return fmt.Errorf("forbidden capabilities/linked requests 漂移")
	}
	return nil
}

func itemWantsV1() []itemWantV1 {
	return []itemWantV1{
		{"W2-ADR-001", "candidate_incomplete_not_ballot_ready", "P4-C01,P4-C02,P4-C07", "R00-D14,R01-D02,R01-D05,R02-D09,R02-D19,R04-D10,R04-D13,R04-D14,R04-D19", "AUTHORITY_ENVELOPE_EXACT_SCHEMA_MISSING,PRODUCTION_TABLE_STATE_MAPPING_MISSING,R02_AGGREGATE_BUILD_TRUST_CLOSURE_MISSING,SLOT_OBSERVATION_IDENTITY_MISSING", "W2_ADR_001_CANDIDATE_INCOMPLETE"},
		{"W2-ADR-002", "candidate_incomplete_not_ballot_ready", "P4-C03,P4-C04,P4-C07", "R01-D01,R02-D19", "DIGEST_OLD_TO_NEW_FIELD_MAPPING_MISSING,DIGEST_VERSION_POLICY_MISSING,DIGEST_GOLDEN_VECTORS_MISSING,R02_AGGREGATE_BUILD_TRUST_CLOSURE_MISSING", "W2_ADR_002_CANDIDATE_INCOMPLETE"},
		{"W2-ADR-003", "candidate_incomplete_not_ballot_ready", "P4-C13", "R01-D05,R03-D04,R03-D05,R03-D06,R03-D08,R03-D11,R04-D16,R04-D18,R04-D19", "ACTIVATION_COMMAND_IDENTITY_EXACT_SCHEMA_MISSING,BUSINESS_COMMAND_RECEIPT_IDL_MISSING,UNKNOWN_OUTCOME_QUERY_VECTORS_MISSING", "W2_ADR_003_CANDIDATE_INCOMPLETE"},
		{"W2-ADR-004", "candidate_incomplete_not_ballot_ready", "P4-C14", "R01-D02,R02-D18,R03-D03,R03-D10,R03-D11,R04-D17,R04-D18,R04-D19", "AUTHENTICATED_QUERY_EXACT_SCHEMA_MISSING,BIDIRECTIONAL_RPC_SECURITY_BOUNDARY_MISSING,SIGNATURE_KEY_ROTATION_DISPOSITION_MISSING", "W2_ADR_004_CANDIDATE_INCOMPLETE"},
		{"W2-ADR-005", "candidate_incomplete_not_ballot_ready", "P4-C11", "R00-D01,R00-D02,R00-D03,R00-D04,R00-D05,R00-D06,R00-D07,R00-D08,R00-D09,R00-D10,R00-D11,R00-D12,R00-D13,R00-D14,R03-D01,R04-D01,R04-D08,R04-D09,R04-D10,R04-D11,R04-D12,R04-D13,R04-D14,R04-D15", "BILLING_POLICY_EXACT_VALUES_MISSING,PROVIDER_CAPABILITY_MATRIX_MISSING,R00_LIVE_CANDIDATE_SUBMISSION_MISSING", "W2_ADR_005_CANDIDATE_INCOMPLETE"},
		{"W2-ADR-008", "joint_disposition_pending_not_ballot_ready", "P4-C07,P4-C08", "R02-D19", "R02_AGGREGATE_BUILD_TRUST_CLOSURE_MISSING,R02_D19_OWNER_DECISION_PENDING", "W2_ADR_008_JOINT_DISPOSITION_PENDING"},
		{"W2-ADR-010", "joint_disposition_pending_not_ballot_ready", "P4-C07,P4-C08", "R02-D19", "R02_AGGREGATE_BUILD_TRUST_CLOSURE_MISSING,R02_D19_OWNER_DECISION_PENDING", "W2_ADR_010_JOINT_DISPOSITION_PENDING"},
		{"W2-ADR-011", "candidate_incomplete_not_ballot_ready", "", "R01-D05,R03-D01,R03-D02,R03-D08,R03-D09,R03-D10,R03-D11,R04-D16,R04-D17,R04-D18,R04-D19", "ACTIVATION_FIELD_CROSS_GATE_MAPPING_MISSING,R01_D05_SLOT_SCOPE_PENDING,SLOT_ORDINAL_UNIQUENESS_DOMAIN_MISSING", "W2_ADR_011_CANDIDATE_INCOMPLETE"},
	}
}

func verifyRootKeysV1(t *testing.T, raw []byte) {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	want := strings.Split("approval_status,ballot_enabled,block_statement,blocked_production_gates,canonical_ballot_status,decision_document,decision_request_status,evidence_status,forbidden_capabilities,gate_manifest,hard_prerequisite_edges,implementation_status,implementation_unlocked,items,linked_requests,owner_role_set_status,registration_status,request_id,request_kind,required_adr_ids,schema_version,scope,status,validator_sources", ",")
	if len(root) != len(want) {
		t.Fatalf("root key count=%d want=%d", len(root), len(want))
	}
	for _, key := range want {
		if _, exists := root[key]; !exists {
			t.Fatalf("root 缺少 key %q", key)
		}
	}
	for _, field := range []string{"decision_document", "gate_manifest"} {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(root[field], &object); err != nil {
			t.Fatal(err)
		}
		verifyObjectKeysV1(t, field, object, []string{"path", "sha256"})
	}
	for field, keys := range map[string][]string{
		"linked_requests":         {"path", "request_id", "sha256"},
		"validator_sources":       {"path", "sha256"},
		"items":                   {"adr_id", "block_code", "canonical_ballot_status", "joint_disposition_refs", "p4_conflict_refs", "readiness_status", "semantic_recommendation_status", "unmet_evidence"},
		"hard_prerequisite_edges": {"dependent_adr_id", "prerequisite_adr_id"},
	} {
		var objects []map[string]json.RawMessage
		if err := json.Unmarshal(root[field], &objects); err != nil {
			t.Fatal(err)
		}
		for index, object := range objects {
			verifyObjectKeysV1(t, fmt.Sprintf("%s[%d]", field, index), object, keys)
		}
	}
}

func verifyObjectKeysV1(t *testing.T, label string, object map[string]json.RawMessage, want []string) {
	t.Helper()
	got := make([]string, 0, len(object))
	for key := range object {
		got = append(got, key)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s keys=%v want=%v", label, got, want)
	}
}

func verifyForbiddenAuthorityV1(t *testing.T, raw []byte) {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	forbidden := strings.Split("accepted,accepted_option_id,actor_id,allowed_option_ids,approval,approval_refs,approved,approved_at,approver_role,candidate_evidence,candidate_owner_roles,canonical_ballot_owner,delegated_decision_ref,freeze,owner_approvals,recommended_option_id,reopen_exception,required_owner_roles,review_id,review_state,review_url,reviewer_id,selected_option,selected_option_id", ",")
	walkAuthorityV1(t, "$", value, forbidden)
}

func walkAuthorityV1(t *testing.T, path string, value any, forbidden []string) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if sort.SearchStrings(forbidden, key) < len(forbidden) && forbidden[sort.SearchStrings(forbidden, key)] == key {
				t.Fatalf("发现禁止 authority 字段 %s.%s", path, key)
			}
			walkAuthorityV1(t, path+"."+key, child, forbidden)
		}
	case []any:
		for index, child := range typed {
			walkAuthorityV1(t, fmt.Sprintf("%s[%d]", path, index), child, forbidden)
		}
	}
}

func validateJSONV1(raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("JSON 非 UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := readValueV1(decoder, false); err != nil {
		return err
	}
	return eofV1(decoder)
}

func validateGateJSONV1(raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("Gate JSON 非 UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := readValueV1(decoder, true); err != nil {
		return err
	}
	return eofV1(decoder)
}

func readValueV1(decoder *json.Decoder, allowNull bool) error {
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
	if _, ok := tokenValue.(json.Number); ok {
		return fmt.Errorf("JSON number 被禁止")
	}
	delimiter, ok := tokenValue.(json.Delim)
	if !ok {
		return nil
	}
	if delimiter != '{' && delimiter != '[' {
		return fmt.Errorf("JSON delimiter 非法")
	}
	seen := map[string]struct{}{}
	for decoder.More() {
		if delimiter == '{' {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return keyErr
			}
			key, valid := keyToken.(string)
			if !valid {
				return fmt.Errorf("JSON key 非字符串")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("JSON 重复键 %q", key)
			}
			seen[key] = struct{}{}
		}
		if err := readValueV1(decoder, allowNull); err != nil {
			return err
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return err
	}
	want := json.Delim('}')
	if delimiter == '[' {
		want = ']'
	}
	if closing != want {
		return fmt.Errorf("JSON closing=%v want=%v", closing, want)
	}
	return nil
}

func eofV1(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("JSON 含尾随值")
		}
		return err
	}
	return nil
}

func verifyArtifactV1(t *testing.T, root string, ref artifactRefV1, wantPath, wantSHA256 string) {
	t.Helper()
	if ref.Path != wantPath {
		t.Fatalf("artifact path=%q want=%q", ref.Path, wantPath)
	}
	if ref.SHA256 != wantSHA256 {
		t.Fatalf("artifact SHA=%s want=%s", ref.SHA256, wantSHA256)
	}
	verifyRegularFileV1(t, root, ref.Path)
	raw := readFileV1(t, root, ref.Path)
	digest := sha256.Sum256(raw)
	actual := "sha256:" + hex.EncodeToString(digest[:])
	if ref.SHA256 != actual {
		t.Fatalf("%s raw SHA=%s want=%s", ref.Path, actual, ref.SHA256)
	}
}

func verifyGateManifestV1(t *testing.T, raw []byte) {
	t.Helper()
	if err := validateGateJSONV1(raw); err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	gatesRaw, exists := root["gates"]
	if !exists || string(bytes.TrimSpace(gatesRaw)) == "null" {
		t.Fatal("Gate manifest gates 缺失或为 null")
	}
	var gateObjects []json.RawMessage
	if err := json.Unmarshal(gatesRaw, &gateObjects); err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Gates []struct {
			Gate              string            `json:"gate"`
			Status            string            `json:"status"`
			Freeze            json.RawMessage   `json:"freeze"`
			ReopenException   json.RawMessage   `json:"reopen_exception"`
			CandidateEvidence []json.RawMessage `json:"candidate_evidence"`
		} `json:"gates"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(gateObjects) != len(manifest.Gates) {
		t.Fatal("Gate manifest raw/typed count 漂移")
	}
	want := map[string]bool{"W2-R00": false, "W2-R01": false, "W2-R02": false, "W2-R03": false, "W2-R04": false}
	wantCandidates := map[string]int{"W2-R00": 0, "W2-R01": 1, "W2-R02": 0, "W2-R03": 0, "W2-R04": 1}
	for index, gate := range manifest.Gates {
		var gateObject map[string]json.RawMessage
		if err := json.Unmarshal(gateObjects[index], &gateObject); err != nil {
			t.Fatal(err)
		}
		verifyObjectKeysV1(t, fmt.Sprintf("gate[%d]", index), gateObject, []string{"blockers", "candidate_evidence", "freeze", "gate", "reopen_exception", "required_owner_roles", "status"})
		if string(bytes.TrimSpace(gateObject["candidate_evidence"])) == "null" {
			t.Fatalf("gate %s candidate_evidence 不得为 null", gate.Gate)
		}
		if gate.Gate == "W2-P4-ADR" {
			t.Fatal("准备契约不得创建 live P4 ADR Gate")
		}
		if _, tracked := want[gate.Gate]; !tracked {
			continue
		}
		if want[gate.Gate] || gate.Status != "expansion_frozen" || len(gate.CandidateEvidence) != wantCandidates[gate.Gate] || string(bytes.TrimSpace(gate.Freeze)) != "null" || string(bytes.TrimSpace(gate.ReopenException)) != "null" {
			t.Fatalf("gate %s frozen/null boundary 漂移", gate.Gate)
		}
		for _, rawCandidate := range gate.CandidateEvidence {
			var candidateObject map[string]json.RawMessage
			if err := json.Unmarshal(rawCandidate, &candidateObject); err != nil {
				t.Fatal(err)
			}
			verifyObjectKeysV1(t, "candidate", candidateObject, []string{"contract_manifest_path", "contract_manifest_sha256", "coverage", "scope", "target_tests", "vector_ids"})
			var candidate struct {
				Coverage               string   `json:"coverage"`
				Scope                  string   `json:"scope"`
				ContractManifestPath   string   `json:"contract_manifest_path"`
				ContractManifestSHA256 string   `json:"contract_manifest_sha256"`
				VectorIDs              []string `json:"vector_ids"`
				TargetTests            []string `json:"target_tests"`
			}
			if err := json.Unmarshal(rawCandidate, &candidate); err != nil || candidate.Coverage != "partial_candidate" || candidate.Scope == "" || candidate.ContractManifestPath == "" || len(candidate.ContractManifestSHA256) != len("sha256:")+64 || len(candidate.VectorIDs) == 0 || len(candidate.TargetTests) == 0 {
				t.Fatalf("gate %s candidate 不得冒充 full", gate.Gate)
			}
		}
		want[gate.Gate] = true
	}
	for gate, found := range want {
		if !found {
			t.Fatalf("gate manifest 缺少 %s", gate)
		}
	}
}

func verifySingleFileV1(t *testing.T, root, repoPath string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(repoPath))
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("directory exact-set=%v want=%s", entries, filepath.Base(path))
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("%s 必须是 no-symlink mode 0644 regular file: %s", repoPath, info.Mode())
	}
}

func verifySourceV1(t *testing.T, root string, spec sourceSpecV1) {
	t.Helper()
	verifySingleFileV1(t, root, spec.Path)
	path := filepath.Join(root, filepath.FromSlash(spec.Path))
	raw := readFileV1(t, root, spec.Path)
	for _, marker := range []string{"//" + "go:" + "build", "// " + "+build", "//" + "go:" + "embed", "/" + "internal" + "/"} {
		if bytes.Contains(raw, []byte(marker)) {
			t.Fatalf("%s 禁止 %s", spec.Path, marker)
		}
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), path, raw, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name.Name != spec.PackageName {
		t.Fatalf("package=%q want=%q", parsed.Name.Name, spec.PackageName)
	}
	var imports, tests []string
	for _, imported := range parsed.Imports {
		if imported.Name != nil {
			t.Fatalf("%s 禁止 import alias/dot/blank", spec.Path)
		}
		imports = append(imports, strings.Trim(imported.Path.Value, `"`))
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if function.Name.Name == "init" || function.Name.Name == "TestMain" || strings.HasPrefix(function.Name.Name, "Fuzz") || strings.HasPrefix(function.Name.Name, "Benchmark") || strings.HasPrefix(function.Name.Name, "Example") {
			t.Fatalf("%s 禁止 %s", spec.Path, function.Name.Name)
		}
		if strings.HasPrefix(function.Name.Name, "Test") {
			tests = append(tests, function.Name.Name)
		}
	}
	if !reflect.DeepEqual(imports, spec.Imports) || !reflect.DeepEqual(tests, spec.Tests) {
		t.Fatalf("%s import/tests exact-set=%v/%v want=%v/%v", spec.Path, imports, tests, spec.Imports, spec.Tests)
	}
}

func sourceSpecsV1() []sourceSpecV1 {
	common := []string{"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "fmt", "go/ast", "go/parser", "go/token", "io", "os", "path/filepath", "reflect", "runtime", "sort", "strings", "testing", "unicode/utf8"}
	return []sourceSpecV1{
		{mainPathV1, "w2adrballotpreparation_test", common, []string{"TestW2ADRBallotPreparationV1", "TestW2ADRBallotPreparationV1AuthorityMutations", "TestW2ADRBallotPreparationV1StrictJSON"}},
		{guardPathV1, "w2adrballotpreparationguard_test", common, []string{"TestW2ADRBallotPreparationGuardV1", "TestW2ADRBallotPreparationGuardV1AuthorityMutations", "TestW2ADRBallotPreparationGuardV1StrictAndSourceBoundaries"}},
	}
}

func repoRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 ADR ballot preparation guard")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
}

func readFileV1(t *testing.T, root, repoPath string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(repoPath)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verifyRegularFileV1(t *testing.T, root, repoPath string) {
	t.Helper()
	info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(repoPath)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("%s 必须是 no-symlink mode 0644 regular file: %s", repoPath, info.Mode())
	}
}
