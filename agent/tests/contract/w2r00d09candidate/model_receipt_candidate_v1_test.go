// Package w2r00d09candidate_test 验证 R00-D09 terminal ModelReceipt 候选的字段、摘要、状态与失败关闭边界。
package w2r00d09candidate_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	candidatePathV1  = "docs/design/agent/approvals/w2-r00-candidate-inputs/R00-D09-v1/candidate-input.json"
	vectorsPathV1    = "docs/design/agent/approvals/w2-r00-candidate-inputs/R00-D09-v1/terminal-model-receipt-v1-vectors.json"
	gatePathV1       = "docs/design/agent/approvals/w2-review-freeze-manifest.json"
	digestDomainV1   = "dora.agent.model_receipt.terminal.v1"
	maxSafeIntegerV1 = int64(9007199254740991)
)

var (
	digestPatternV1     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	uuidV7PatternV1     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	errorPatternV1      = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)
	safeStringPatternV1 = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)
)

type vectorCorpusV1 struct {
	SchemaVersion                string                     `json:"schema_version"`
	ArtifactID                   string                     `json:"artifact_id"`
	CandidateInputID             string                     `json:"candidate_input_id"`
	TerminalReceiptSchemaVersion string                     `json:"terminal_receipt_schema_version"`
	DigestDomain                 string                     `json:"digest_domain"`
	ValidCases                   []validCaseV1              `json:"valid_cases"`
	InvalidCases                 []invalidCaseV1            `json:"invalid_cases"`
	TerminalUniquenessCases      []terminalUniquenessCaseV1 `json:"terminal_uniqueness_cases"`
	FinalizeGuardCases           []finalizeGuardCaseV1      `json:"finalize_guard_cases"`
}

type validCaseV1 struct {
	CaseID         string          `json:"case_id"`
	ExpectedDigest string          `json:"expected_digest"`
	Receipt        json.RawMessage `json:"receipt"`
}

type invalidCaseV1 struct {
	CaseID        string `json:"case_id"`
	BaseCaseID    string `json:"base_case_id"`
	Operation     string `json:"operation"`
	Path          string `json:"path"`
	ValueJSON     string `json:"value_json"`
	ExpectedError string `json:"expected_error"`
}

type terminalUniquenessCaseV1 struct {
	CaseID              string `json:"case_id"`
	StoredIdentity      string `json:"stored_identity"`
	StoredDigest        string `json:"stored_digest"`
	IncomingIdentity    string `json:"incoming_identity"`
	IncomingDigest      string `json:"incoming_digest"`
	ExpectedDisposition string `json:"expected_disposition"`
}

type finalizeGuardCaseV1 struct {
	CaseID                       string `json:"case_id"`
	ReceiptState                 string `json:"receipt_state"`
	ModelOutcome                 string `json:"model_outcome"`
	ReceiptDigestMatches         bool   `json:"receipt_digest_matches"`
	PrepareBindingMatches        bool   `json:"prepare_binding_matches"`
	RequestContainsMoneyMutation bool   `json:"request_contains_money_mutation"`
	ExpectedDisposition          string `json:"expected_disposition"`
}

type terminalModelReceiptV1 struct {
	SchemaVersion          string                  `json:"schema_version"`
	ModelReceiptID         string                  `json:"model_receipt_id"`
	ReceiptVersion         int64                   `json:"receipt_version"`
	CausalIdentity         causalIdentityV1        `json:"causal_identity"`
	ExecutionIdentity      executionIdentityV1     `json:"execution_identity"`
	ToolDefinitionPin      toolDefinitionPinV1     `json:"tool_definition_pin"`
	NodeKey                string                  `json:"node_key"`
	PromptPin              promptPinV1             `json:"prompt_pin"`
	ModelConfigPin         modelConfigPinV1        `json:"model_config_pin"`
	ModelRequestDigest     string                  `json:"model_request_digest"`
	BudgetSnapshotDigest   string                  `json:"budget_snapshot_digest"`
	BillingPrepareBinding  billingPrepareBindingV1 `json:"billing_prepare_binding"`
	ProviderExecution      providerExecutionV1     `json:"provider_execution"`
	ModelOutcome           string                  `json:"model_outcome"`
	Output                 *modelOutputRefV1       `json:"output,omitempty"`
	Usage                  modelUsageV1            `json:"usage"`
	FinishReasonCode       string                  `json:"finish_reason_code"`
	FinalErrorCode         *string                 `json:"final_error_code,omitempty"`
	ModelStartedAtUnixMS   int64                   `json:"model_started_at_unix_ms"`
	ModelCompletedAtUnixMS int64                   `json:"model_completed_at_unix_ms"`
	TerminalEvidenceDigest string                  `json:"terminal_evidence_digest"`
	ModelReceiptDigest     string                  `json:"model_receipt_digest,omitempty"`
}

type causalIdentityV1 struct {
	UserID        string `json:"user_id"`
	ProjectID     string `json:"project_id"`
	SessionID     string `json:"session_id"`
	InputID       string `json:"input_id"`
	TurnID        string `json:"turn_id"`
	RunID         string `json:"run_id"`
	ToolCallID    string `json:"tool_call_id"`
	ToolReceiptID string `json:"tool_receipt_id"`
	GraphRunID    string `json:"graph_run_id"`
}

type executionIdentityV1 struct {
	LogicalToolExecutionID string `json:"logical_tool_execution_id"`
	ModelCallOrdinal       int32  `json:"model_call_ordinal"`
}

type toolDefinitionPinV1 struct {
	ToolKey           string `json:"tool_key"`
	DefinitionVersion int64  `json:"definition_version"`
	DefinitionDigest  string `json:"definition_digest"`
}

type promptPinV1 struct {
	PromptKey     string `json:"prompt_key"`
	PromptVersion string `json:"prompt_version"`
	PromptDigest  string `json:"prompt_digest"`
}

type modelConfigPinV1 struct {
	ModelConfigRef     string `json:"model_config_ref"`
	ModelConfigVersion int64  `json:"model_config_version"`
	ModelConfigDigest  string `json:"model_config_digest"`
	ProviderRouteRef   string `json:"provider_route_ref"`
}

type billingPrepareBindingV1 struct {
	BillingExecutionID   string `json:"billing_execution_id"`
	PrepareCommandID     string `json:"prepare_command_id"`
	PrepareRequestDigest string `json:"prepare_request_digest"`
	PrepareReceiptDigest string `json:"prepare_receipt_digest"`
	ChargeReceiptID      string `json:"charge_receipt_id"`
}

type providerExecutionV1 struct {
	ProviderRouteRef           string  `json:"provider_route_ref"`
	ProviderRequestKey         string  `json:"provider_request_key"`
	DispatchDisposition        string  `json:"dispatch_disposition"`
	DispatchMarkerDigest       *string `json:"dispatch_marker_digest,omitempty"`
	ProviderRequestID          *string `json:"provider_request_id,omitempty"`
	AttemptCount               int64   `json:"attempt_count"`
	AttemptsDigest             string  `json:"attempts_digest"`
	TerminalAuthorityKind      string  `json:"terminal_authority_kind"`
	ProviderAuthorityRefDigest *string `json:"provider_authority_ref_digest,omitempty"`
}

type modelOutputRefV1 struct {
	OutputSchemaVersion string `json:"output_schema_version"`
	OutputRef           string `json:"output_ref"`
	OutputDigest        string `json:"output_digest"`
}

type modelUsageV1 struct {
	UsageStatus  string `json:"usage_status"`
	InputTokens  *int64 `json:"input_tokens,omitempty"`
	OutputTokens *int64 `json:"output_tokens,omitempty"`
	TotalTokens  *int64 `json:"total_tokens,omitempty"`
}

// TestW2R00D09CandidateInputV1 固定未注册候选身份、完整 wire schema 与 live Gate 失败关闭状态。
func TestW2R00D09CandidateInputV1(t *testing.T) {
	t.Parallel()

	root := repoRootV1(t)
	raw := readFileV1(t, root, candidatePathV1)
	if err := validateJSONShapeV1(raw); err != nil {
		t.Fatal(err)
	}
	var candidate map[string]json.RawMessage
	if err := json.Unmarshal(raw, &candidate); err != nil {
		t.Fatal(err)
	}
	requireExactKeysV1(t, "candidate", candidate, []string{
		"approval_status", "artifact_id", "artifact_kind", "ballot_enabled", "block_statement",
		"contract_candidate", "cross_gate_alignment_refs", "decision_id", "evidence_status", "forbidden_claims",
		"gate", "implementation_status", "implementation_unlocked", "owner_request_status", "prerequisite_refs",
		"registration_status", "required_evidence_projection", "schema_version", "source_artifacts",
		"source_open_item_ids", "status", "unmet_registration_evidence",
		"validator_sources",
	})
	verifyCandidateIdentityV1(t, candidate)
	verifySourceArtifactsV1(t, root, candidate["source_artifacts"])
	verifyValidatorArtifactsV1(t, root, candidate["validator_sources"])
	verifyWireContractsV1(t, candidate["contract_candidate"])
	verifyFailureClosedClaimsV1(t, candidate)
	verifyLiveGateV1(t, root)
}

func verifyValidatorArtifactsV1(t *testing.T, repoRoot string, raw json.RawMessage) {
	t.Helper()
	var refs []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &refs); err != nil || len(refs) != 2 {
		t.Fatalf("validator artifacts=%d err=%v", len(refs), err)
	}
	wantPaths := []string{
		"agent/tests/contract/w2r00d09candidate/model_receipt_candidate_v1_test.go",
		"agent/tests/contract/w2r00d09candidateguard/model_receipt_candidate_guard_v1_test.go",
	}
	for index, ref := range refs {
		if ref.Path != wantPaths[index] || !digestPatternV1.MatchString(ref.SHA256) {
			t.Fatalf("validator ref[%d] 非法: %+v", index, ref)
		}
		verifyRegularFileV1(t, repoRoot, ref.Path)
		content := readFileV1(t, repoRoot, ref.Path)
		digest := sha256.Sum256(content)
		if got := "sha256:" + hex.EncodeToString(digest[:]); got != ref.SHA256 {
			t.Fatalf("validator %s digest=%s want=%s", ref.Path, got, ref.SHA256)
		}
	}
}

// TestW2R00D09TerminalReceiptVectorsV1 固定四个 terminal canonical golden 与十五个负向 schema/binding 向量。
func TestW2R00D09TerminalReceiptVectorsV1(t *testing.T) {
	t.Parallel()

	corpus := loadCorpusV1(t)
	if corpus.SchemaVersion != "w2_r00_d09_terminal_model_receipt_vectors.v1" || corpus.ArtifactID != "VEC-W2-R00-D09-v1" || corpus.CandidateInputID != "CI-W2-R00-D09-v1" || corpus.TerminalReceiptSchemaVersion != "dora.agent.terminal_model_receipt.v1" || corpus.DigestDomain != digestDomainV1 {
		t.Fatalf("D09 vector identity 漂移: %+v", corpus)
	}
	if len(corpus.ValidCases) != 4 || len(corpus.InvalidCases) != 20 {
		t.Fatalf("D09 vector count valid=%d invalid=%d", len(corpus.ValidCases), len(corpus.InvalidCases))
	}
	validByID := make(map[string]json.RawMessage, len(corpus.ValidCases))
	for _, item := range corpus.ValidCases {
		item := item
		t.Run(item.CaseID, func(t *testing.T) {
			receipt, code := validateTerminalReceiptV1(item.Receipt)
			if code != "" {
				t.Fatalf("valid receipt rejected: %s", code)
			}
			verifyReceiptFieldOrderV1(t, item.Receipt)
			if got := terminalReceiptDigestV1(receipt); got != item.ExpectedDigest || got != receipt.ModelReceiptDigest {
				t.Fatalf("digest=%s expected=%s claimed=%s", got, item.ExpectedDigest, receipt.ModelReceiptDigest)
			}
		})
		if _, exists := validByID[item.CaseID]; exists {
			t.Fatalf("duplicate valid case id %s", item.CaseID)
		}
		validByID[item.CaseID] = item.Receipt
	}
	for _, item := range corpus.InvalidCases {
		item := item
		t.Run(item.CaseID, func(t *testing.T) {
			base, ok := validByID[item.BaseCaseID]
			if !ok {
				t.Fatalf("unknown base case %s", item.BaseCaseID)
			}
			mutated := applyMutationV1(t, base, item)
			if _, code := validateTerminalReceiptV1(mutated); code != item.ExpectedError {
				t.Fatalf("error=%s want=%s json=%s", code, item.ExpectedError, mutated)
			}
		})
	}
}

// TestW2R00D09TerminalReceiptErrorPrecedenceV1 固定多故障时 schema、outcome 与 provider binding 的拒绝优先级。
func TestW2R00D09TerminalReceiptErrorPrecedenceV1(t *testing.T) {
	t.Parallel()

	base := loadCorpusV1(t).ValidCases[0].Receipt
	unknownAndVersion := applyMutationV1(t, base, invalidCaseV1{Operation: "add", Path: "/future_field", ValueJSON: `"x"`})
	unknownAndVersion = applyMutationV1(t, unknownAndVersion, invalidCaseV1{Operation: "replace", Path: "/schema_version", ValueJSON: `"dora.agent.terminal_model_receipt.v2"`})
	if _, code := validateTerminalReceiptV1(unknownAndVersion); code != "MODEL_RECEIPT_SCHEMA_INVALID" {
		t.Fatalf("unknown+version precedence=%s", code)
	}

	unknownOutcomeAndMissingOutput := applyMutationV1(t, base, invalidCaseV1{Operation: "replace", Path: "/model_outcome", ValueJSON: `"UNKNOWN_OUTCOME"`})
	unknownOutcomeAndMissingOutput = applyMutationV1(t, unknownOutcomeAndMissingOutput, invalidCaseV1{Operation: "remove", Path: "/output"})
	if _, code := validateTerminalReceiptV1(unknownOutcomeAndMissingOutput); code != "MODEL_RECEIPT_OUTCOME_INVALID" {
		t.Fatalf("outcome+presence precedence=%s", code)
	}

	providerMismatchAndMissingMarker := applyMutationV1(t, base, invalidCaseV1{Operation: "replace", Path: "/provider_execution/provider_route_ref", ValueJSON: `"provider-route.test-only.mismatch.v1"`})
	providerMismatchAndMissingMarker = applyMutationV1(t, providerMismatchAndMissingMarker, invalidCaseV1{Operation: "remove", Path: "/provider_execution/dispatch_marker_digest"})
	if _, code := validateTerminalReceiptV1(providerMismatchAndMissingMarker); code != "MODEL_RECEIPT_PROVIDER_BINDING_MISMATCH" {
		t.Fatalf("provider binding+evidence precedence=%s", code)
	}
}

// TestW2R00D09TerminalUniquenessAndFinalizeV1 固定 first-write-wins 与 Finalize 零财务改写纯状态模型。
func TestW2R00D09TerminalUniquenessAndFinalizeV1(t *testing.T) {
	t.Parallel()

	corpus := loadCorpusV1(t)
	if len(corpus.TerminalUniquenessCases) != 4 || len(corpus.FinalizeGuardCases) != 5 {
		t.Fatalf("state vector count uniqueness=%d finalize=%d", len(corpus.TerminalUniquenessCases), len(corpus.FinalizeGuardCases))
	}
	for _, item := range corpus.TerminalUniquenessCases {
		if got := terminalDispositionV1(item); got != item.ExpectedDisposition {
			t.Errorf("%s disposition=%s want=%s", item.CaseID, got, item.ExpectedDisposition)
		}
	}
	for _, item := range corpus.FinalizeGuardCases {
		if got := finalizeDispositionV1(item); got != item.ExpectedDisposition {
			t.Errorf("%s disposition=%s want=%s", item.CaseID, got, item.ExpectedDisposition)
		}
	}
}

// TestW2R00D09CandidateArtifactsStrictJSONV1 固定重复键、null、浮点、指数与尾随值失败关闭。
func TestW2R00D09CandidateArtifactsStrictJSONV1(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string][]byte{
		"duplicate":    []byte(`{"x":"a","x":"b"}`),
		"null":         []byte(`{"x":null}`),
		"float":        []byte(`{"x":1.2}`),
		"exponent":     []byte(`{"x":1e2}`),
		"invalid_utf8": {'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'},
		"trailing":     []byte(`{"x":"a"}{}`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateJSONShapeV1(raw); err == nil {
				t.Fatalf("strict JSON 必须拒绝 %s", name)
			}
		})
	}
}

func verifyCandidateIdentityV1(t *testing.T, root map[string]json.RawMessage) {
	t.Helper()
	wantStrings := map[string]string{
		"schema_version":        "w2_r00_d09_candidate_input.v1",
		"artifact_id":           "CI-W2-R00-D09-v1",
		"artifact_kind":         "versioned_candidate_input_only",
		"gate":                  "W2-R00",
		"decision_id":           "R00-D09",
		"status":                "prepared_unregistered",
		"registration_status":   "not_registered",
		"owner_request_status":  "not_created",
		"approval_status":       "not_requested",
		"implementation_status": "prohibited",
		"evidence_status":       "candidate_only",
	}
	for key, want := range wantStrings {
		var got string
		if err := json.Unmarshal(root[key], &got); err != nil || got != want {
			t.Fatalf("candidate %s=%q want=%q err=%v", key, got, want, err)
		}
	}
	for _, key := range []string{"implementation_unlocked", "ballot_enabled"} {
		var got bool
		if err := json.Unmarshal(root[key], &got); err != nil || got {
			t.Fatalf("candidate %s 必须为 false: %v", key, err)
		}
	}
	verifyStringListV1(t, root["source_open_item_ids"], []string{"BILL-OPEN-007"})
	verifyStringListV1(t, root["prerequisite_refs"], []string{})
	verifyStringListV1(t, root["cross_gate_alignment_refs"], []string{"R01-D02", "R02-D09", "R04-D14"})
}

func verifySourceArtifactsV1(t *testing.T, repoRoot string, raw json.RawMessage) {
	t.Helper()
	var refs []struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &refs); err != nil || len(refs) != 10 {
		t.Fatalf("source artifacts=%d err=%v", len(refs), err)
	}
	wantPaths := []string{
		"docs/design/agent/w2-r00-owner-decision-matrix-v1.md",
		"docs/design/agent/approvals/w2-r00-candidate-preparation-requests/CPR-W2-R00-v1.json",
		"docs/design/cross-module/graph-execution-billing-contract-v1.md",
		"docs/design/agent/runner-session-lane-review-v1.md",
		"docs/design/agent/w2-r01-owner-decision-matrix-v1.md",
		"docs/design/agent/w2-r02-owner-decision-matrix-v1.md",
		"docs/design/agent/w2-r04-owner-decision-matrix-v1.md",
		"docs/design/agent/graph-tool-result-receipt-contract-v1.md",
		"docs/design/agent/immutable-turn-context-design-v1.md",
		"docs/design/agent/graphtool/plan_creation_spec-w2-r04-gap-review.md",
	}
	for index, ref := range refs {
		if ref.Path != wantPaths[index] || !digestPatternV1.MatchString(ref.SHA256) {
			t.Fatalf("source ref 非法: %+v", ref)
		}
		verifyRegularFileV1(t, repoRoot, ref.Path)
		content := readFileV1(t, repoRoot, ref.Path)
		digest := sha256.Sum256(content)
		if got := "sha256:" + hex.EncodeToString(digest[:]); got != ref.SHA256 {
			t.Fatalf("source %s digest=%s want=%s", ref.Path, got, ref.SHA256)
		}
	}
}

func verifyWireContractsV1(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var contract map[string]json.RawMessage
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	requireExactKeysV1(t, "contract_candidate", contract, []string{
		"attempt_evidence_digest_domain", "attempt_evidence_schema_version", "canonicalization_rules", "cross_gate_boundaries",
		"finalize_projection", "profile", "terminal_receipt_digest_domain", "terminal_receipt_schema_version", "terminal_rules", "wire_contracts",
	})
	var wire []struct {
		ContractName string   `json:"contract_name"`
		FieldOrder   []string `json:"field_order"`
		Fields       []struct {
			Name        string   `json:"name"`
			WireType    string   `json:"wire_type"`
			Presence    string   `json:"presence"`
			Constraints []string `json:"constraints"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(contract["wire_contracts"], &wire); err != nil {
		t.Fatal(err)
	}
	wantNames := []string{
		"TerminalModelReceiptV1", "CausalIdentityV1", "ModelExecutionIdentityV1", "ToolDefinitionPinV1", "PromptPinV1",
		"ModelConfigPinV1", "BillingPrepareBindingV1", "ProviderExecutionV1", "ModelOutputRefV1", "ModelUsageV1", "ProviderAttemptEvidenceV1",
	}
	if len(wire) != len(wantNames) {
		t.Fatalf("wire contracts=%d want=%d", len(wire), len(wantNames))
	}
	for index, item := range wire {
		if item.ContractName != wantNames[index] || len(item.FieldOrder) != len(item.Fields) {
			t.Fatalf("wire[%d] identity/order mismatch: %+v", index, item)
		}
		for fieldIndex, field := range item.Fields {
			if field.Name != item.FieldOrder[fieldIndex] || field.WireType == "" || field.Presence == "" {
				t.Fatalf("%s field[%d] mismatch: %+v", item.ContractName, fieldIndex, field)
			}
			verifyUniqueStringsV1(t, item.ContractName+"."+field.Name+" constraints", field.Constraints)
		}
		verifyUniqueStringsV1(t, item.ContractName+" field order", item.FieldOrder)
	}
	if !reflect.DeepEqual(wire[0].FieldOrder, terminalFieldOrderV1()) {
		t.Fatalf("terminal schema field order=%v", wire[0].FieldOrder)
	}
	var canonical []string
	if err := json.Unmarshal(contract["canonicalization_rules"], &canonical); err != nil {
		t.Fatal(err)
	}
	wantCanonical := []string{
		"canonical_json_is_compact_ascii_safe_string_subset_of_utf8_nfc", "condition_not_applicable_means_field_absent", "explicit_null_is_rejected",
		"field_order_follows_wire_contract_field_order", "floating_point_and_exponent_numbers_are_rejected", "integers_are_base10_json_safe_integers",
		"object_unknown_fields_are_rejected", "recursive_duplicate_keys_are_rejected", "sha256_is_domain_utf8_then_zero_byte_then_canonical_json",
		"terminal_receipt_digest_excludes_only_model_receipt_digest", "trailing_json_values_are_rejected",
	}
	if !reflect.DeepEqual(canonical, wantCanonical) {
		t.Fatalf("canonical rules=%v", canonical)
	}
}

func verifyFailureClosedClaimsV1(t *testing.T, root map[string]json.RawMessage) {
	t.Helper()
	var claims []string
	if err := json.Unmarshal(root["forbidden_claims"], &claims); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"awaiting_owner_decision", "ballot_ready", "candidate_evidence_registered", "cross_gate_decisions_closed", "dr_w2_r00_created",
		"implementation_unlocked", "owner_approval_recorded", "owner_request_created", "production_model_receipt_implemented",
		"provider_capability_verified", "r00_canonical_manifest_created", "review_frozen_or_approved", "w2_b0a_or_w2_b1_unlocked",
	}
	if !reflect.DeepEqual(claims, want) {
		t.Fatalf("forbidden claims=%v", claims)
	}
	var block string
	if err := json.Unmarshal(root["block_statement"], &block); err != nil || !strings.Contains(block, "未登记到 live W2-R00 candidate_evidence") || !strings.Contains(block, "不授权生产") {
		t.Fatalf("block statement 非法: %q err=%v", block, err)
	}
	var evidence []struct {
		EvidenceID         string   `json:"evidence_id"`
		CandidateCoverage  []string `json:"candidate_coverage"`
		RegistrationStatus string   `json:"registration_status"`
	}
	if err := json.Unmarshal(root["required_evidence_projection"], &evidence); err != nil || len(evidence) != 4 {
		t.Fatalf("evidence projection=%d err=%v", len(evidence), err)
	}
	wantIDs := []string{"R00-D09-EV-CANONICAL-GOLDEN", "R00-D09-EV-SCHEMA-NEGATIVE", "R00-D09-EV-TERMINAL-UNIQUENESS", "R00-D09-EV-FINALIZE-GUARD"}
	for index, item := range evidence {
		if item.EvidenceID != wantIDs[index] || item.RegistrationStatus == "registered" || len(item.CandidateCoverage) == 0 {
			t.Fatalf("evidence[%d] 非法: %+v", index, item)
		}
	}
}

func verifyLiveGateV1(t *testing.T, repoRoot string) {
	t.Helper()
	var manifest struct {
		Gates []struct {
			Gate              string            `json:"gate"`
			Status            string            `json:"status"`
			CandidateEvidence []json.RawMessage `json:"candidate_evidence"`
			Freeze            json.RawMessage   `json:"freeze"`
			ReopenException   json.RawMessage   `json:"reopen_exception"`
		} `json:"gates"`
	}
	if err := json.Unmarshal(readFileV1(t, repoRoot, gatePathV1), &manifest); err != nil {
		t.Fatal(err)
	}
	for _, gate := range manifest.Gates {
		if gate.Gate == "W2-R00" {
			if gate.Status != "expansion_frozen" || len(gate.CandidateEvidence) != 0 || string(gate.Freeze) != "null" || string(gate.ReopenException) != "null" {
				t.Fatalf("live R00 Gate 不得前移: %+v", gate)
			}
			return
		}
	}
	t.Fatal("live manifest 缺少 W2-R00")
}

func loadCorpusV1(t *testing.T) vectorCorpusV1 {
	t.Helper()
	raw := readFileV1(t, repoRootV1(t), vectorsPathV1)
	if err := validateJSONShapeV1(raw); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var corpus vectorCorpusV1
	if err := decoder.Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if err := requireEOFV1(decoder); err != nil {
		t.Fatal(err)
	}
	return corpus
}

func validateTerminalReceiptV1(raw []byte) (terminalModelReceiptV1, string) {
	if err := validateJSONShapeV1(raw); err != nil {
		if errors.Is(err, errNullV1) {
			return terminalModelReceiptV1{}, "MODEL_RECEIPT_NULL_NOT_ALLOWED"
		}
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_SCHEMA_INVALID"
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var receipt terminalModelReceiptV1
	if err := decoder.Decode(&receipt); err != nil {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_SCHEMA_INVALID"
	}
	if err := requireEOFV1(decoder); err != nil {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_SCHEMA_INVALID"
	}
	if receipt.SchemaVersion != "dora.agent.terminal_model_receipt.v1" || receipt.ReceiptVersion != 1 {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_SCHEMA_UNSUPPORTED"
	}
	if !safeStringsV1(reflect.ValueOf(receipt)) {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_STRING_INVALID"
	}
	if receipt.ExecutionIdentity.ModelCallOrdinal != 0 || !uuidV7PatternV1.MatchString(receipt.ExecutionIdentity.LogicalToolExecutionID) || receipt.ToolDefinitionPin.ToolKey != "plan_creation_spec" || receipt.ToolDefinitionPin.DefinitionVersion < 1 || receipt.NodeKey != "call_model_primary" {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_EXECUTION_IDENTITY_INVALID"
	}
	if !validReceiptIDsAndDigestsV1(receipt) {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_SCHEMA_INVALID"
	}
	wantCommand := "tr:" + receipt.CausalIdentity.ToolReceiptID + ":charge.primary:v1"
	if receipt.BillingPrepareBinding.PrepareCommandID != wantCommand {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_BILLING_BINDING_MISMATCH"
	}
	if receipt.ProviderExecution.ProviderRouteRef != receipt.ModelConfigPin.ProviderRouteRef {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_PROVIDER_BINDING_MISMATCH"
	}
	if code := validateProviderEvidenceV1(receipt.ProviderExecution); code != "" {
		return terminalModelReceiptV1{}, code
	}
	if !containsV1([]string{"SUCCEEDED", "FAILED", "CANCELLED"}, receipt.ModelOutcome) {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_OUTCOME_INVALID"
	}
	if (receipt.ModelOutcome == "SUCCEEDED" && (receipt.Output == nil || receipt.FinalErrorCode != nil)) || (receipt.ModelOutcome != "SUCCEEDED" && (receipt.Output != nil || receipt.FinalErrorCode == nil || !errorPatternV1.MatchString(*receipt.FinalErrorCode))) {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_PRESENCE_INVALID"
	}
	if receipt.ModelOutcome == "CANCELLED" && receipt.FinishReasonCode != "CANCELLED" {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_PRESENCE_INVALID"
	}
	if receipt.ModelOutcome == "FAILED" && receipt.FinishReasonCode != "ERROR" && receipt.FinishReasonCode != "CONTENT_FILTER" {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_PRESENCE_INVALID"
	}
	if !containsV1([]string{"STOP", "LENGTH_LIMIT", "TOOL_CALLS", "CONTENT_FILTER", "ERROR", "CANCELLED"}, receipt.FinishReasonCode) {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_PRESENCE_INVALID"
	}
	if code := validateUsageV1(receipt.Usage); code != "" {
		return terminalModelReceiptV1{}, code
	}
	if receipt.ModelStartedAtUnixMS < 0 || receipt.ModelCompletedAtUnixMS < receipt.ModelStartedAtUnixMS {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_TIME_INVALID"
	}
	if receipt.ModelReceiptDigest == "" || terminalReceiptDigestV1(receipt) != receipt.ModelReceiptDigest {
		return terminalModelReceiptV1{}, "MODEL_RECEIPT_DIGEST_MISMATCH"
	}
	return receipt, ""
}

func validReceiptIDsAndDigestsV1(receipt terminalModelReceiptV1) bool {
	ids := []string{
		receipt.ModelReceiptID, receipt.CausalIdentity.UserID, receipt.CausalIdentity.ProjectID, receipt.CausalIdentity.SessionID,
		receipt.CausalIdentity.InputID, receipt.CausalIdentity.TurnID, receipt.CausalIdentity.RunID, receipt.CausalIdentity.ToolCallID,
		receipt.CausalIdentity.ToolReceiptID, receipt.CausalIdentity.GraphRunID, receipt.ExecutionIdentity.LogicalToolExecutionID,
		receipt.BillingPrepareBinding.BillingExecutionID, receipt.BillingPrepareBinding.ChargeReceiptID,
	}
	for _, id := range ids {
		if !uuidV7PatternV1.MatchString(id) {
			return false
		}
	}
	digests := []string{
		receipt.ToolDefinitionPin.DefinitionDigest, receipt.PromptPin.PromptDigest, receipt.ModelConfigPin.ModelConfigDigest,
		receipt.ModelRequestDigest, receipt.BudgetSnapshotDigest, receipt.BillingPrepareBinding.PrepareRequestDigest,
		receipt.BillingPrepareBinding.PrepareReceiptDigest, receipt.ProviderExecution.AttemptsDigest, receipt.TerminalEvidenceDigest,
	}
	if receipt.ProviderExecution.DispatchMarkerDigest != nil {
		digests = append(digests, *receipt.ProviderExecution.DispatchMarkerDigest)
	}
	if receipt.ProviderExecution.ProviderAuthorityRefDigest != nil {
		digests = append(digests, *receipt.ProviderExecution.ProviderAuthorityRefDigest)
	}
	if receipt.Output != nil {
		digests = append(digests, receipt.Output.OutputDigest)
		if receipt.Output.OutputSchemaVersion == "" || !strings.HasPrefix(receipt.Output.OutputRef, "test-only://") {
			return false
		}
	}
	for _, digest := range digests {
		if !digestPatternV1.MatchString(digest) {
			return false
		}
	}
	return receipt.PromptPin.PromptKey != "" && receipt.PromptPin.PromptVersion != "" && receipt.ModelConfigPin.ModelConfigRef != "" && receipt.ModelConfigPin.ModelConfigVersion > 0 && receipt.ModelConfigPin.ProviderRouteRef != "" && receipt.ProviderExecution.ProviderRequestKey != ""
}

func validateProviderEvidenceV1(provider providerExecutionV1) string {
	if !containsV1([]string{"NOT_DISPATCHED", "DISPATCHED_DIRECT_TERMINAL", "DISPATCHED_QUERY_TERMINAL"}, provider.DispatchDisposition) || !containsV1([]string{"AGENT_NO_DISPATCH_PROOF", "PROVIDER_RESPONSE", "PROVIDER_QUERY", "CONTROLLED_LOCAL_FAKE"}, provider.TerminalAuthorityKind) {
		return "MODEL_RECEIPT_PROVIDER_EVIDENCE_INVALID"
	}
	if provider.DispatchDisposition == "NOT_DISPATCHED" {
		if provider.AttemptCount != 0 || provider.TerminalAuthorityKind != "AGENT_NO_DISPATCH_PROOF" || provider.DispatchMarkerDigest != nil || provider.ProviderRequestID != nil || provider.ProviderAuthorityRefDigest != nil {
			return "MODEL_RECEIPT_PROVIDER_EVIDENCE_INVALID"
		}
		return ""
	}
	if provider.AttemptCount < 1 || provider.DispatchMarkerDigest == nil || provider.ProviderAuthorityRefDigest == nil || provider.TerminalAuthorityKind == "AGENT_NO_DISPATCH_PROOF" {
		return "MODEL_RECEIPT_PROVIDER_EVIDENCE_INVALID"
	}
	if provider.DispatchDisposition == "DISPATCHED_DIRECT_TERMINAL" && provider.TerminalAuthorityKind != "PROVIDER_RESPONSE" && provider.TerminalAuthorityKind != "CONTROLLED_LOCAL_FAKE" {
		return "MODEL_RECEIPT_PROVIDER_EVIDENCE_INVALID"
	}
	if provider.DispatchDisposition == "DISPATCHED_QUERY_TERMINAL" && provider.TerminalAuthorityKind != "PROVIDER_QUERY" {
		return "MODEL_RECEIPT_PROVIDER_EVIDENCE_INVALID"
	}
	return ""
}

func validateUsageV1(usage modelUsageV1) string {
	if usage.UsageStatus == "UNAVAILABLE" {
		if usage.InputTokens != nil || usage.OutputTokens != nil || usage.TotalTokens != nil {
			return "MODEL_RECEIPT_USAGE_INVALID"
		}
		return ""
	}
	if usage.UsageStatus != "REPORTED" || usage.InputTokens == nil || usage.OutputTokens == nil || usage.TotalTokens == nil || *usage.InputTokens < 0 || *usage.OutputTokens < 0 || *usage.TotalTokens < 0 || *usage.InputTokens > maxSafeIntegerV1-*usage.OutputTokens || *usage.TotalTokens != *usage.InputTokens+*usage.OutputTokens {
		return "MODEL_RECEIPT_USAGE_INVALID"
	}
	return ""
}

func terminalReceiptDigestV1(receipt terminalModelReceiptV1) string {
	receipt.ModelReceiptDigest = ""
	payload, err := json.Marshal(receipt)
	if err != nil {
		panic(err)
	}
	hash := sha256.New()
	hash.Write([]byte(digestDomainV1))
	hash.Write([]byte{0})
	hash.Write(payload)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func verifyReceiptFieldOrderV1(t *testing.T, raw json.RawMessage) {
	t.Helper()
	verifyObjectOrderV1(t, "terminal", raw, terminalFieldOrderForRawV1(raw))
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	wantNested := map[string][]string{
		"causal_identity":         {"user_id", "project_id", "session_id", "input_id", "turn_id", "run_id", "tool_call_id", "tool_receipt_id", "graph_run_id"},
		"execution_identity":      {"logical_tool_execution_id", "model_call_ordinal"},
		"tool_definition_pin":     {"tool_key", "definition_version", "definition_digest"},
		"prompt_pin":              {"prompt_key", "prompt_version", "prompt_digest"},
		"model_config_pin":        {"model_config_ref", "model_config_version", "model_config_digest", "provider_route_ref"},
		"billing_prepare_binding": {"billing_execution_id", "prepare_command_id", "prepare_request_digest", "prepare_receipt_digest", "charge_receipt_id"},
	}
	for field, want := range wantNested {
		verifyObjectOrderV1(t, field, root[field], want)
	}
	providerWant := []string{"provider_route_ref", "provider_request_key", "dispatch_disposition"}
	var provider map[string]json.RawMessage
	if err := json.Unmarshal(root["provider_execution"], &provider); err != nil {
		t.Fatal(err)
	}
	for _, optional := range []string{"dispatch_marker_digest", "provider_request_id"} {
		if _, ok := provider[optional]; ok {
			providerWant = append(providerWant, optional)
		}
	}
	providerWant = append(providerWant, "attempt_count", "attempts_digest", "terminal_authority_kind")
	if _, ok := provider["provider_authority_ref_digest"]; ok {
		providerWant = append(providerWant, "provider_authority_ref_digest")
	}
	verifyObjectOrderV1(t, "provider_execution", root["provider_execution"], providerWant)
	if output, ok := root["output"]; ok {
		verifyObjectOrderV1(t, "output", output, []string{"output_schema_version", "output_ref", "output_digest"})
	}
	var usage map[string]json.RawMessage
	if err := json.Unmarshal(root["usage"], &usage); err != nil {
		t.Fatal(err)
	}
	usageWant := []string{"usage_status"}
	if _, ok := usage["input_tokens"]; ok {
		usageWant = append(usageWant, "input_tokens", "output_tokens", "total_tokens")
	}
	verifyObjectOrderV1(t, "usage", root["usage"], usageWant)
}

func terminalFieldOrderV1() []string {
	return []string{
		"schema_version", "model_receipt_id", "receipt_version", "causal_identity", "execution_identity", "tool_definition_pin",
		"node_key", "prompt_pin", "model_config_pin", "model_request_digest", "budget_snapshot_digest", "billing_prepare_binding",
		"provider_execution", "model_outcome", "output", "usage", "finish_reason_code", "final_error_code",
		"model_started_at_unix_ms", "model_completed_at_unix_ms", "terminal_evidence_digest", "model_receipt_digest",
	}
}

func terminalFieldOrderForRawV1(raw json.RawMessage) []string {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil
	}
	want := make([]string, 0, len(root))
	for _, field := range terminalFieldOrderV1() {
		if _, ok := root[field]; ok {
			want = append(want, field)
		}
	}
	return want
}

func applyMutationV1(t *testing.T, base json.RawMessage, mutation invalidCaseV1) []byte {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(base, &root); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimPrefix(mutation.Path, "/"), "/")
	if len(parts) == 0 || len(parts) > 2 {
		t.Fatalf("unsupported mutation path %s", mutation.Path)
	}
	target := root
	if len(parts) == 2 {
		nested, ok := root[parts[0]].(map[string]any)
		if !ok {
			t.Fatalf("mutation target %s is not object", parts[0])
		}
		target = nested
	}
	field := parts[len(parts)-1]
	switch mutation.Operation {
	case "remove":
		delete(target, field)
	case "add", "replace":
		var value any
		if err := json.Unmarshal([]byte(mutation.ValueJSON), &value); err != nil {
			t.Fatal(err)
		}
		target[field] = value
	default:
		t.Fatalf("unknown mutation operation %s", mutation.Operation)
	}
	mutated, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return mutated
}

func terminalDispositionV1(item terminalUniquenessCaseV1) string {
	if item.StoredIdentity == "absent" {
		return "STORED"
	}
	if item.StoredIdentity != item.IncomingIdentity {
		return "INDEPENDENT"
	}
	if item.StoredDigest == item.IncomingDigest {
		return "REPLAYED"
	}
	return "CONFLICT"
}

func finalizeDispositionV1(item finalizeGuardCaseV1) string {
	if item.ReceiptState != "TERMINAL" || !containsV1([]string{"SUCCEEDED", "FAILED", "CANCELLED"}, item.ModelOutcome) {
		return "REJECTED_NONTERMINAL"
	}
	if !item.ReceiptDigestMatches {
		return "REJECTED_RECEIPT_BINDING"
	}
	if !item.PrepareBindingMatches {
		return "REJECTED_PREPARE_BINDING"
	}
	if item.RequestContainsMoneyMutation {
		return "REJECTED_FINANCIAL_MUTATION"
	}
	return "ALLOWED_NO_FINANCIAL_MUTATION"
}

var errNullV1 = errors.New("null not allowed")

func validateJSONShapeV1(raw []byte) error {
	if !utf8.Valid(raw) {
		return errors.New("invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := walkJSONValueV1(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func walkJSONValueV1(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch value := token.(type) {
	case nil:
		return errNullV1
	case json.Number:
		if strings.ContainsAny(value.String(), ".eE") {
			return errors.New("non-integer number")
		}
		number, err := strconv.ParseInt(value.String(), 10, 64)
		if err != nil {
			return err
		}
		if number < -maxSafeIntegerV1 || number > maxSafeIntegerV1 {
			return errors.New("integer outside JSON safe range")
		}
	case json.Delim:
		switch value {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				keyToken, keyErr := decoder.Token()
				if keyErr != nil {
					return keyErr
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not string")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate key %s", key)
				}
				seen[key] = struct{}{}
				if err := walkJSONValueV1(decoder); err != nil {
					return err
				}
			}
			end, endErr := decoder.Token()
			if endErr != nil || end != json.Delim('}') {
				return errors.New("invalid object end")
			}
		case '[':
			for decoder.More() {
				if err := walkJSONValueV1(decoder); err != nil {
					return err
				}
			}
			end, endErr := decoder.Token()
			if endErr != nil || end != json.Delim(']') {
				return errors.New("invalid array end")
			}
		default:
			return errors.New("unexpected closing delimiter")
		}
	}
	return nil
}

func verifyObjectOrderV1(t *testing.T, label string, raw json.RawMessage, want []string) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		t.Fatalf("%s is not object: %v", label, err)
	}
	var got []string
	for decoder.More() {
		key, keyErr := decoder.Token()
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		got = append(got, key.(string))
		var discard json.RawMessage
		if err := decoder.Decode(&discard); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s field order=%v want=%v", label, got, want)
	}
}

func requireExactKeysV1(t *testing.T, label string, value map[string]json.RawMessage, want []string) {
	t.Helper()
	got := make([]string, 0, len(value))
	for key := range value {
		got = append(got, key)
	}
	sort.Strings(got)
	wantCopy := append([]string(nil), want...)
	sort.Strings(wantCopy)
	if !reflect.DeepEqual(got, wantCopy) {
		t.Fatalf("%s keys=%v want=%v", label, got, wantCopy)
	}
}

func verifyStringListV1(t *testing.T, raw json.RawMessage, want []string) {
	t.Helper()
	var got []string
	if err := json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("string list=%v want=%v err=%v", got, want, err)
	}
}

func verifyUniqueStringsV1(t *testing.T, label string, values []string) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, value := range values {
		if value == "" {
			t.Fatalf("%s contains empty value", label)
		}
		if _, ok := seen[value]; ok {
			t.Fatalf("%s contains duplicate %s", label, value)
		}
		seen[value] = struct{}{}
	}
}

func containsV1(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func safeStringsV1(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	switch value.Kind() {
	case reflect.Pointer, reflect.Interface:
		return value.IsNil() || safeStringsV1(value.Elem())
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if !safeStringsV1(value.Field(index)) {
				return false
			}
		}
	case reflect.String:
		return safeStringPatternV1.MatchString(value.String())
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if !safeStringsV1(value.Index(index)) {
				return false
			}
		}
	}
	return true
}

func requireEOFV1(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func readFileV1(t *testing.T, repoRoot, relativePath string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verifyRegularFileV1(t *testing.T, repoRoot, relativePath string) {
	t.Helper()
	info, err := os.Lstat(filepath.Join(repoRoot, filepath.FromSlash(relativePath)))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("artifact %s mode=%v want regular 0644", relativePath, info.Mode())
	}
}

func repoRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../../"))
	if _, err := os.Stat(filepath.Join(root, "agent", "go.mod")); err != nil {
		t.Fatalf("repo root invalid: %v", err)
	}
	return root
}
