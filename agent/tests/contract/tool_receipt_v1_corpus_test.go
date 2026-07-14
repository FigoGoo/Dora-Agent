package contract_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"
)

const (
	toolReceiptSnapshotSchemaVersionV1 = "tool_receipt_snapshot.v1"
	toolRequestCanonicalVersionV1      = "dora.tool_request.v1"
	toolExecutionRefDigestDomainV1     = "dora.tool_execution_ref.v1"
	toolReceiptSnapshotDigestDomainV1  = "dora.tool_receipt_snapshot.v1"
	receiptCorpusPath                  = "testdata/w2_r01/tool_receipt_v1.json"
)

var (
	refSlotPattern      = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	dottedSchemaPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,95}$`)
	idempotencyPattern  = regexp.MustCompile(`^[a-z0-9._:-]{1,160}$`)
)

type receiptCorpusV1 struct {
	SchemaVersion   string                  `json:"schema_version"`
	InitialState    receiptStateFixtureV1   `json:"initial_state"`
	SlotPolicies    []executionSlotPolicyV1 `json:"slot_policies"`
	TransitionCases []receiptTransitionV1   `json:"transition_cases"`
	EvidenceCases   []receiptEvidenceCaseV1 `json:"evidence_cases"`
}

type executionSlotPolicyV1 struct {
	ToolKey           string `json:"tool_key"`
	DefinitionVersion string `json:"definition_version"`
	RefSlot           string `json:"ref_slot"`
	SlotOrdinal       int64  `json:"slot_ordinal"`
	RefType           string `json:"ref_type"`
	RefSchemaVersion  string `json:"ref_schema_version"`
	AuthorityOwner    string `json:"authority_owner"`
	QueryContract     string `json:"query_contract"`
	EffectClass       string `json:"effect_class"`
}

type receiptStateFixtureV1 struct {
	StateID  string                `json:"state_id"`
	Snapshot toolReceiptSnapshotV1 `json:"snapshot"`
}

type receiptTransitionV1 struct {
	ID                     string           `json:"id"`
	FromState              string           `json:"from_state"`
	SaveAsState            string           `json:"save_as_state"`
	Expect                 string           `json:"expect"`
	ErrorCode              string           `json:"error_code"`
	ExpectedSnapshotDigest string           `json:"expected_snapshot_digest"`
	Command                receiptCommandV1 `json:"command"`
}

type receiptEvidenceCaseV1 struct {
	ID                     string                  `json:"id"`
	BaseSnapshot           toolReceiptSnapshotV1   `json:"base_snapshot"`
	SetupSlots             []resolvedSlotFixtureV1 `json:"setup_slots"`
	Result                 json.RawMessage         `json:"result"`
	ResultRefSlots         []string                `json:"result_ref_slots"`
	Expect                 string                  `json:"expect"`
	ErrorCode              string                  `json:"error_code"`
	ExpectedSnapshotDigest string                  `json:"expected_snapshot_digest"`
}

type resolvedSlotFixtureV1 struct {
	Slot         executionSlotV1 `json:"slot"`
	AuthorityRef authorityRefV1  `json:"authority_ref"`
}

type toolReceiptSnapshotV1 struct {
	SchemaVersion                  string             `json:"schema_version"`
	ReceiptID                      string             `json:"receipt_id"`
	SessionID                      string             `json:"session_id"`
	TurnID                         string             `json:"turn_id"`
	RunID                          string             `json:"run_id"`
	ToolCallID                     string             `json:"tool_call_id"`
	ToolKey                        string             `json:"tool_key"`
	DefinitionVersion              string             `json:"definition_version"`
	IntentSchemaVersion            string             `json:"intent_schema_version"`
	ResultSchemaVersion            string             `json:"result_schema_version"`
	RequestCanonicalizationVersion string             `json:"request_canonicalization_version"`
	RequestSemanticDigest          string             `json:"request_semantic_digest"`
	WriteState                     string             `json:"write_state"`
	ReceiptVersion                 int64              `json:"receipt_version"`
	OwnerFence                     int64              `json:"owner_fence"`
	ExecutionSlots                 []executionSlotV1  `json:"execution_slots"`
	Result                         *graphToolResultV1 `json:"result,omitempty"`
	ResultDigest                   *string            `json:"result_digest,omitempty"`
	ResultRefs                     []resultRefV1      `json:"result_refs"`
}

type executionSlotV1 struct {
	RefSlot           string          `json:"ref_slot"`
	SlotOrdinal       int64           `json:"slot_ordinal"`
	RefType           string          `json:"ref_type"`
	RefSchemaVersion  string          `json:"ref_schema_version"`
	AuthorityOwner    string          `json:"authority_owner"`
	IdempotencyKey    string          `json:"idempotency_key"`
	RequestDigest     string          `json:"request_digest"`
	QueryContract     string          `json:"query_contract"`
	ResolutionState   string          `json:"resolution_state"`
	AuthorityRef      *authorityRefV1 `json:"authority_ref,omitempty"`
	ResolvedRefDigest *string         `json:"resolved_ref_digest,omitempty"`
}

type authorityRefV1 struct {
	AuthorityID             string `json:"authority_id"`
	AuthorityVersion        int64  `json:"authority_version"`
	AuthoritySemanticDigest string `json:"authority_semantic_digest"`
	ResourceType            string `json:"resource_type,omitempty"`
	ProjectionID            string `json:"projection_id,omitempty"`
}

type resultRefV1 struct {
	RefSlot   string `json:"ref_slot"`
	RefDigest string `json:"ref_digest"`
}

type receiptCommandV1 struct {
	Kind                   string           `json:"kind"`
	PresentedRequestDigest string           `json:"presented_request_digest"`
	ExpectedReceiptVersion int64            `json:"expected_receipt_version"`
	OwnerFence             int64            `json:"owner_fence"`
	Slot                   *executionSlotV1 `json:"slot,omitempty"`
	RefSlot                string           `json:"ref_slot,omitempty"`
	AuthorityRef           *authorityRefV1  `json:"authority_ref,omitempty"`
	Result                 json.RawMessage  `json:"result,omitempty"`
	ClaimedResultDigest    string           `json:"claimed_result_digest,omitempty"`
	ResultRefSlots         []string         `json:"result_ref_slots,omitempty"`
}

type executionRefDigestProjectionV1 struct {
	CanonicalizationVersion string         `json:"canonicalization_version"`
	ReceiptID               string         `json:"receipt_id"`
	ParentRequestDigest     string         `json:"parent_request_digest"`
	ToolKey                 string         `json:"tool_key"`
	DefinitionVersion       string         `json:"definition_version"`
	IntentSchemaVersion     string         `json:"intent_schema_version"`
	RefSlot                 string         `json:"ref_slot"`
	SlotOrdinal             int64          `json:"slot_ordinal"`
	RefType                 string         `json:"ref_type"`
	RefSchemaVersion        string         `json:"ref_schema_version"`
	AuthorityOwner          string         `json:"authority_owner"`
	IdempotencyKey          string         `json:"idempotency_key"`
	RequestDigest           string         `json:"request_digest"`
	QueryContract           string         `json:"query_contract"`
	AuthorityRef            authorityRefV1 `json:"authority_ref"`
}

type receiptPolicySetV1 struct {
	Result resultPolicySetV1
	Slots  map[string]executionSlotPolicyV1
}

func TestToolReceiptV1Corpus(t *testing.T) {
	resultCorpus := loadResultCorpus(t)
	corpus := loadReceiptCorpus(t)
	policies := receiptPolicySetV1{Result: buildResultPolicies(t, resultCorpus), Slots: buildSlotPolicies(t, corpus.SlotPolicies)}
	if err := validateToolReceiptSnapshotV1(corpus.InitialState.Snapshot, policies); err != nil {
		t.Fatalf("初始 Receipt 非法: %v", err)
	}
	states := map[string]toolReceiptSnapshotV1{corpus.InitialState.StateID: corpus.InitialState.Snapshot}
	seenCases := make(map[string]struct{}, len(corpus.TransitionCases))

	for _, fixture := range corpus.TransitionCases {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			if _, exists := seenCases[fixture.ID]; exists {
				t.Fatalf("重复 transition id %q", fixture.ID)
			}
			seenCases[fixture.ID] = struct{}{}
			before, exists := states[fixture.FromState]
			if !exists {
				t.Fatalf("未知 from_state %q", fixture.FromState)
			}
			after, err := applyReceiptCommandV1(before, fixture.Command, policies)
			if fixture.Expect == "reject" {
				if err == nil {
					t.Fatalf("期望拒绝，实际状态=%+v", after)
				}
				if got := errorCode(err); got != fixture.ErrorCode {
					t.Fatalf("拒绝码=%s want=%s err=%v", got, fixture.ErrorCode, err)
				}
				if fixture.SaveAsState != "" || fixture.ExpectedSnapshotDigest != "" {
					t.Fatal("拒绝向量不得保存状态或声明摘要")
				}
				return
			}
			if fixture.Expect != "accept" || fixture.ErrorCode != "" || err != nil || fixture.SaveAsState == "" {
				t.Fatalf("合法 transition 元数据或结果错误: expect=%q code=%q save=%q err=%v", fixture.Expect, fixture.ErrorCode, fixture.SaveAsState, err)
			}
			canonical, encodeErr := canonicalJSON(after)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			digest := semanticDigest(toolReceiptSnapshotDigestDomainV1, canonical)
			states[fixture.SaveAsState] = after
			if digest != fixture.ExpectedSnapshotDigest {
				t.Errorf("snapshot digest=%s want=%s\ncanonical=%s", digest, fixture.ExpectedSnapshotDigest, canonical)
			}
		})
	}

	required := []string{"TR-P01-reserve-operation", "TR-P02-resolve-operation", "TR-P03-reserve-batch", "TR-P04-resolve-batch", "TR-P05-reserve-dispatch", "TR-P06-resolve-dispatch", "TR-P07-freeze-accepted", "TR-P08-replay-frozen", "TR-P09-replay-open", "TR-P10-replay-reserve-slot", "TR-P11-replay-resolve-slot", "TR-N01-request-conflict", "TR-N02-slot-conflict", "TR-N03-ref-conflict", "TR-N04-unresolved-freeze", "TR-N05-frozen-append", "TR-N06-result-digest-mismatch", "TR-N07-stale-fence", "TR-N08-version-conflict", "TR-N09-result-ref-mismatch", "TR-N10-illegal-result", "TR-N11-forged-higher-fence", "TR-N12-stale-freeze-version", "TR-N13-unpinned-slot"}
	if len(seenCases) != len(required) {
		t.Fatalf("transition exact-set 数量=%d want=%d", len(seenCases), len(required))
	}
	for _, id := range required {
		if _, exists := seenCases[id]; !exists {
			t.Fatalf("缺少固定 transition %s", id)
		}
	}

	seenEvidence := make(map[string]struct{}, len(corpus.EvidenceCases))
	for _, fixture := range corpus.EvidenceCases {
		fixture := fixture
		t.Run(fixture.ID, func(t *testing.T) {
			if _, exists := seenEvidence[fixture.ID]; exists {
				t.Fatalf("重复 evidence id %q", fixture.ID)
			}
			seenEvidence[fixture.ID] = struct{}{}
			before, err := prepareEvidenceSnapshotV1(fixture.BaseSnapshot, fixture.SetupSlots, policies)
			if err != nil {
				t.Fatalf("准备 evidence snapshot: %v", err)
			}
			command := receiptCommandV1{
				Kind: "freeze", PresentedRequestDigest: before.RequestSemanticDigest,
				ExpectedReceiptVersion: before.ReceiptVersion, OwnerFence: before.OwnerFence,
				Result: fixture.Result, ResultRefSlots: fixture.ResultRefSlots,
			}
			after, err := applyReceiptCommandV1(before, command, policies)
			if fixture.Expect == "reject" {
				if err == nil || errorCode(err) != fixture.ErrorCode {
					t.Fatalf("evidence 拒绝 err=%v code=%s want=%s", err, errorCode(err), fixture.ErrorCode)
				}
				return
			}
			if fixture.Expect != "accept" || fixture.ErrorCode != "" || err != nil {
				t.Fatalf("evidence 合法向量失败: expect=%s code=%s err=%v", fixture.Expect, fixture.ErrorCode, err)
			}
			canonical, encodeErr := canonicalJSON(after)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if digest := semanticDigest(toolReceiptSnapshotDigestDomainV1, canonical); digest != fixture.ExpectedSnapshotDigest {
				t.Errorf("evidence snapshot digest=%s want=%s\ncanonical=%s", digest, fixture.ExpectedSnapshotDigest, canonical)
			}
		})
	}
	wantEvidence := []string{"TR-E01-waiting-user", "TR-E02-waiting-user-missing-approval", "TR-E03-completed", "TR-E04-completed-missing-resource", "TR-E05-partial", "TR-E06-partial-missing-resource", "TR-E07-retryable-failed-after-side-effect", "TR-E08-card-projection-mismatch", "TR-E09-cancelled-after-side-effects", "TR-E10-failed-before-side-effect", "TR-E11-failed-hidden-policy-side-effect", "TR-E12-cancelled-after-policy-side-effect", "TR-E13-cancelled-after-missing-policy-side-effect"}
	if len(seenEvidence) != len(wantEvidence) {
		t.Fatalf("evidence exact-set 数量=%d want=%d", len(seenEvidence), len(wantEvidence))
	}
	for _, id := range wantEvidence {
		if _, exists := seenEvidence[id]; !exists {
			t.Fatalf("缺少 evidence case %s", id)
		}
	}
}

func prepareEvidenceSnapshotV1(base toolReceiptSnapshotV1, setup []resolvedSlotFixtureV1, policies receiptPolicySetV1) (toolReceiptSnapshotV1, error) {
	current := base
	for _, item := range setup {
		reserved, err := applyReceiptCommandV1(current, receiptCommandV1{
			Kind: "reserve_slot", PresentedRequestDigest: current.RequestSemanticDigest,
			ExpectedReceiptVersion: current.ReceiptVersion, OwnerFence: current.OwnerFence, Slot: &item.Slot,
		}, policies)
		if err != nil {
			return toolReceiptSnapshotV1{}, err
		}
		current, err = applyReceiptCommandV1(reserved, receiptCommandV1{
			Kind: "resolve_slot", PresentedRequestDigest: reserved.RequestSemanticDigest,
			ExpectedReceiptVersion: reserved.ReceiptVersion, OwnerFence: reserved.OwnerFence,
			RefSlot: item.Slot.RefSlot, AuthorityRef: &item.AuthorityRef,
		}, policies)
		if err != nil {
			return toolReceiptSnapshotV1{}, err
		}
	}
	return current, nil
}

func loadReceiptCorpus(t *testing.T) receiptCorpusV1 {
	t.Helper()
	raw, err := w2R01CorpusFS.ReadFile(receiptCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("receipt corpus JSON 非法: %v", err)
	}
	var corpus receiptCorpusV1
	if err := strictDecode(raw, &corpus); err != nil {
		t.Fatalf("解析 receipt corpus: %v", err)
	}
	if corpus.SchemaVersion != "tool_receipt_v1_corpus.v1" || corpus.InitialState.StateID == "" || len(corpus.SlotPolicies) != 7 || len(corpus.TransitionCases) != 24 || len(corpus.EvidenceCases) != 13 {
		t.Fatalf("receipt corpus 版本或覆盖不足: version=%q slots=%d transitions=%d evidence=%d", corpus.SchemaVersion, len(corpus.SlotPolicies), len(corpus.TransitionCases), len(corpus.EvidenceCases))
	}
	return corpus
}

func buildSlotPolicies(t *testing.T, fixtures []executionSlotPolicyV1) map[string]executionSlotPolicyV1 {
	t.Helper()
	policies := make(map[string]executionSlotPolicyV1, len(fixtures))
	lastKey := ""
	for _, policy := range fixtures {
		if !snakeKeyPattern.MatchString(policy.ToolKey) || !dottedSchemaPattern.MatchString(policy.DefinitionVersion) ||
			!refSlotPattern.MatchString(policy.RefSlot) || !safePositiveIntegerV1(policy.SlotOrdinal) || !snakeKeyPattern.MatchString(policy.RefType) ||
			!dottedSchemaPattern.MatchString(policy.RefSchemaVersion) || !dottedSchemaPattern.MatchString(policy.QueryContract) ||
			(policy.AuthorityOwner != "agent" && policy.AuthorityOwner != "business" && policy.AuthorityOwner != "worker") ||
			(policy.EffectClass != "side_effect" && policy.EffectClass != "evidence_only") {
			t.Fatalf("非法 slot policy: %+v", policy)
		}
		key := slotPolicyKey(policy.ToolKey, policy.DefinitionVersion, policy.RefSlot)
		if key <= lastKey {
			t.Fatalf("slot policy 非规范顺序或重复: %s", key)
		}
		lastKey = key
		policies[key] = policy
	}
	return policies
}

func applyReceiptCommandV1(before toolReceiptSnapshotV1, command receiptCommandV1, policies receiptPolicySetV1) (toolReceiptSnapshotV1, error) {
	if err := validateToolReceiptSnapshotV1(before, policies); err != nil {
		return toolReceiptSnapshotV1{}, err
	}
	if command.PresentedRequestDigest != before.RequestSemanticDigest {
		return toolReceiptSnapshotV1{}, reject("TOOL_RECEIPT_CONFLICT", "request_semantic_digest")
	}
	if command.Kind == "replay" {
		if command.ExpectedReceiptVersion != 0 || command.OwnerFence != 0 || command.Slot != nil || command.RefSlot != "" || command.AuthorityRef != nil || len(command.Result) != 0 || command.ClaimedResultDigest != "" || command.ResultRefSlots != nil {
			return toolReceiptSnapshotV1{}, reject("INVALID_TOOL_RECEIPT", "replay command")
		}
		return before, nil
	}
	if before.WriteState == "frozen" {
		return toolReceiptSnapshotV1{}, reject("TOOL_RECEIPT_FROZEN", "write_state")
	}
	if command.OwnerFence != before.OwnerFence {
		return toolReceiptSnapshotV1{}, reject("STALE_FENCE", "owner_fence")
	}
	if replayed, handled, err := replayExistingSlotV1(before, command); handled {
		return replayed, err
	}
	if command.ExpectedReceiptVersion != before.ReceiptVersion {
		return toolReceiptSnapshotV1{}, reject("RECEIPT_VERSION_CONFLICT", "receipt_version")
	}
	if before.ReceiptVersion == maxSafeIntegerV1 {
		return toolReceiptSnapshotV1{}, reject("LIMIT_EXCEEDED", "receipt_version")
	}

	switch command.Kind {
	case "reserve_slot":
		return reserveExecutionSlot(before, command, policies)
	case "resolve_slot":
		return resolveExecutionSlot(before, command, policies)
	case "freeze":
		return freezeToolReceipt(before, command, policies)
	default:
		return toolReceiptSnapshotV1{}, reject("INVALID_TOOL_RECEIPT", "command kind")
	}
}

func replayExistingSlotV1(before toolReceiptSnapshotV1, command receiptCommandV1) (toolReceiptSnapshotV1, bool, error) {
	switch command.Kind {
	case "reserve_slot":
		if command.Slot == nil {
			return toolReceiptSnapshotV1{}, false, nil
		}
		for _, existing := range before.ExecutionSlots {
			if existing.RefSlot != command.Slot.RefSlot {
				continue
			}
			if equalJSON(existing, *command.Slot) {
				return before, true, nil
			}
			return toolReceiptSnapshotV1{}, true, reject("TOOL_EXECUTION_REF_CONFLICT", "ref_slot")
		}
	case "resolve_slot":
		if command.RefSlot == "" || command.AuthorityRef == nil {
			return toolReceiptSnapshotV1{}, false, nil
		}
		for _, existing := range before.ExecutionSlots {
			if existing.RefSlot != command.RefSlot || existing.ResolutionState != "resolved" {
				continue
			}
			if equalJSON(*existing.AuthorityRef, *command.AuthorityRef) {
				return before, true, nil
			}
			return toolReceiptSnapshotV1{}, true, reject("TOOL_EXECUTION_REF_CONFLICT", "authority_ref")
		}
	}
	return toolReceiptSnapshotV1{}, false, nil
}

func reserveExecutionSlot(before toolReceiptSnapshotV1, command receiptCommandV1, policies receiptPolicySetV1) (toolReceiptSnapshotV1, error) {
	if command.Slot == nil || command.RefSlot != "" || command.AuthorityRef != nil || len(command.Result) != 0 || command.ClaimedResultDigest != "" || command.ResultRefSlots != nil {
		return toolReceiptSnapshotV1{}, reject("INVALID_TOOL_RECEIPT", "reserve command")
	}
	slot := *command.Slot
	if err := validateExecutionSlotV1(slot); err != nil || slot.ResolutionState != "prepared" {
		return toolReceiptSnapshotV1{}, reject("INVALID_TOOL_RECEIPT", "prepared slot")
	}
	for _, existing := range before.ExecutionSlots {
		if existing.RefSlot != slot.RefSlot {
			continue
		}
		if equalJSON(existing, slot) {
			return before, nil
		}
		return toolReceiptSnapshotV1{}, reject("TOOL_EXECUTION_REF_CONFLICT", "ref_slot")
	}
	if slot.SlotOrdinal != int64(len(before.ExecutionSlots)+1) {
		return toolReceiptSnapshotV1{}, reject("INVALID_TOOL_RECEIPT", "slot_ordinal")
	}
	after := cloneReceiptSnapshot(before)
	after.ExecutionSlots = append(after.ExecutionSlots, slot)
	after.ReceiptVersion++
	after.OwnerFence = command.OwnerFence
	return after, validateToolReceiptSnapshotStructureV1(after, policies)
}

func resolveExecutionSlot(before toolReceiptSnapshotV1, command receiptCommandV1, policies receiptPolicySetV1) (toolReceiptSnapshotV1, error) {
	if command.RefSlot == "" || command.AuthorityRef == nil || command.Slot != nil || len(command.Result) != 0 || command.ClaimedResultDigest != "" || command.ResultRefSlots != nil {
		return toolReceiptSnapshotV1{}, reject("INVALID_TOOL_RECEIPT", "resolve command")
	}
	after := cloneReceiptSnapshot(before)
	for index := range after.ExecutionSlots {
		slot := &after.ExecutionSlots[index]
		if slot.RefSlot != command.RefSlot {
			continue
		}
		if err := validateAuthorityRefV1(*command.AuthorityRef, *slot, policies.Result); err != nil {
			return toolReceiptSnapshotV1{}, err
		}
		if slot.ResolutionState == "resolved" {
			if equalJSON(*slot.AuthorityRef, *command.AuthorityRef) {
				return before, nil
			}
			return toolReceiptSnapshotV1{}, reject("TOOL_EXECUTION_REF_CONFLICT", "authority_ref")
		}
		digest, err := executionRefDigestV1(before, *slot, *command.AuthorityRef)
		if err != nil {
			return toolReceiptSnapshotV1{}, err
		}
		slot.ResolutionState = "resolved"
		slot.AuthorityRef = command.AuthorityRef
		slot.ResolvedRefDigest = &digest
		after.ReceiptVersion++
		after.OwnerFence = command.OwnerFence
		return after, validateToolReceiptSnapshotStructureV1(after, policies)
	}
	return toolReceiptSnapshotV1{}, reject("INVALID_TOOL_RECEIPT", "unknown ref_slot")
}

func freezeToolReceipt(before toolReceiptSnapshotV1, command receiptCommandV1, policies receiptPolicySetV1) (toolReceiptSnapshotV1, error) {
	if len(command.Result) == 0 || command.Slot != nil || command.RefSlot != "" || command.AuthorityRef != nil || command.ResultRefSlots == nil {
		return toolReceiptSnapshotV1{}, reject("INVALID_TOOL_RECEIPT", "freeze command")
	}
	for _, slot := range before.ExecutionSlots {
		if slot.ResolutionState != "resolved" {
			return toolReceiptSnapshotV1{}, reject("TOOL_EXECUTION_SLOT_UNRESOLVED", slot.RefSlot)
		}
	}
	result, _, resultDigest, err := decodeGraphToolResultV1(command.Result, policies.Result)
	if err != nil {
		return toolReceiptSnapshotV1{}, err
	}
	if command.ClaimedResultDigest != "" && command.ClaimedResultDigest != resultDigest {
		return toolReceiptSnapshotV1{}, reject("RESULT_DIGEST_MISMATCH", "claimed_result_digest")
	}
	refs, err := projectResultRefs(before.ExecutionSlots, command.ResultRefSlots)
	if err != nil {
		return toolReceiptSnapshotV1{}, err
	}
	if err := validateResultEvidence(result, before, refs, policies.Slots); err != nil {
		return toolReceiptSnapshotV1{}, err
	}
	after := cloneReceiptSnapshot(before)
	after.WriteState = "frozen"
	after.Result = &result
	after.ResultDigest = &resultDigest
	after.ResultRefs = refs
	after.ReceiptVersion++
	after.OwnerFence = command.OwnerFence
	if err := validateToolReceiptSnapshotV1(after, policies); err != nil {
		return toolReceiptSnapshotV1{}, err
	}
	return after, nil
}

func validateToolReceiptSnapshotV1(snapshot toolReceiptSnapshotV1, policies receiptPolicySetV1) error {
	if err := validateToolReceiptSnapshotStructureV1(snapshot, policies); err != nil {
		return err
	}
	if snapshot.WriteState == "open" {
		if snapshot.Result != nil || snapshot.ResultDigest != nil || len(snapshot.ResultRefs) != 0 {
			return reject("INVALID_TOOL_RECEIPT", "open result fields")
		}
		return nil
	}
	if snapshot.Result == nil || snapshot.ResultDigest == nil {
		return reject("INVALID_TOOL_RECEIPT", "frozen result fields")
	}
	for _, slot := range snapshot.ExecutionSlots {
		if slot.ResolutionState != "resolved" {
			return reject("TOOL_EXECUTION_SLOT_UNRESOLVED", slot.RefSlot)
		}
	}
	if err := validateGraphToolResultV1(*snapshot.Result, policies.Result); err != nil {
		return err
	}
	canonicalResult, err := canonicalJSON(*snapshot.Result)
	if err != nil || semanticDigest(graphToolResultDigestDomainV1, canonicalResult) != *snapshot.ResultDigest {
		return reject("RESULT_DIGEST_MISMATCH", "stored result")
	}
	if snapshot.Result.ReceiptRef.ReceiptID != snapshot.ReceiptID {
		return reject("INVALID_TOOL_RECEIPT", "self receipt_ref")
	}
	if err := validateStoredResultRefs(snapshot.ExecutionSlots, snapshot.ResultRefs); err != nil {
		return err
	}
	return validateResultEvidence(*snapshot.Result, snapshot, snapshot.ResultRefs, policies.Slots)
}

func validateToolReceiptSnapshotStructureV1(snapshot toolReceiptSnapshotV1, policies receiptPolicySetV1) error {
	if snapshot.SchemaVersion != toolReceiptSnapshotSchemaVersionV1 || snapshot.RequestCanonicalizationVersion != toolRequestCanonicalVersionV1 || snapshot.ResultSchemaVersion != graphToolResultSchemaVersionV1 {
		return reject("UNKNOWN_VERSION", "tool receipt versions")
	}
	ids := []string{snapshot.ReceiptID, snapshot.SessionID, snapshot.TurnID, snapshot.RunID, snapshot.ToolCallID}
	for _, id := range ids {
		if !canonicalUUIDv7(id) {
			return reject("INVALID_TOOL_RECEIPT", "identity")
		}
	}
	if !snakeKeyPattern.MatchString(snapshot.ToolKey) || !dottedSchemaPattern.MatchString(snapshot.DefinitionVersion) || !dottedSchemaPattern.MatchString(snapshot.IntentSchemaVersion) || !digestPattern.MatchString(snapshot.RequestSemanticDigest) || !safePositiveIntegerV1(snapshot.ReceiptVersion) || !safePositiveIntegerV1(snapshot.OwnerFence) || snapshot.ExecutionSlots == nil || snapshot.ResultRefs == nil {
		return reject("INVALID_TOOL_RECEIPT", "base fields")
	}
	if snapshot.WriteState != "open" && snapshot.WriteState != "frozen" {
		return reject("UNKNOWN_ENUM", "write_state")
	}
	lastOrdinal := int64(0)
	seenSlots := make(map[string]struct{}, len(snapshot.ExecutionSlots))
	for _, slot := range snapshot.ExecutionSlots {
		if err := validateExecutionSlotV1(slot); err != nil {
			return err
		}
		policy, exists := policies.Slots[slotPolicyKey(snapshot.ToolKey, snapshot.DefinitionVersion, slot.RefSlot)]
		if !exists || !slotMatchesPolicy(slot, policy) {
			return reject("INVALID_TOOL_RECEIPT", "unpinned execution slot")
		}
		if slot.IdempotencyKey != fmt.Sprintf("tr:%s:%s:v1", snapshot.ReceiptID, slot.RefSlot) {
			return reject("INVALID_TOOL_RECEIPT", "idempotency_key derivation")
		}
		if slot.ResolutionState == "resolved" {
			if err := validateAuthorityRefV1(*slot.AuthorityRef, slot, policies.Result); err != nil {
				return err
			}
			expected, err := executionRefDigestV1(snapshot, slot, *slot.AuthorityRef)
			if err != nil || expected != *slot.ResolvedRefDigest {
				return reject("TOOL_EXECUTION_REF_CONFLICT", "resolved_ref_digest")
			}
		}
		if slot.SlotOrdinal <= lastOrdinal {
			return reject("NON_CANONICAL_ORDER", "execution_slots")
		}
		if _, exists := seenSlots[slot.RefSlot]; exists {
			return reject("TOOL_EXECUTION_REF_CONFLICT", slot.RefSlot)
		}
		seenSlots[slot.RefSlot] = struct{}{}
		lastOrdinal = slot.SlotOrdinal
	}
	return nil
}

func validateExecutionSlotV1(slot executionSlotV1) error {
	if !refSlotPattern.MatchString(slot.RefSlot) || !safePositiveIntegerV1(slot.SlotOrdinal) || !snakeKeyPattern.MatchString(slot.RefType) || !dottedSchemaPattern.MatchString(slot.RefSchemaVersion) || !idempotencyPattern.MatchString(slot.IdempotencyKey) || !digestPattern.MatchString(slot.RequestDigest) || !dottedSchemaPattern.MatchString(slot.QueryContract) {
		return reject("INVALID_TOOL_RECEIPT", "execution slot")
	}
	if slot.AuthorityOwner != "agent" && slot.AuthorityOwner != "business" && slot.AuthorityOwner != "worker" {
		return reject("UNKNOWN_ENUM", "authority_owner")
	}
	switch slot.ResolutionState {
	case "prepared":
		if slot.AuthorityRef != nil || slot.ResolvedRefDigest != nil {
			return reject("INVALID_TOOL_RECEIPT", "prepared slot fields")
		}
	case "resolved":
		if slot.AuthorityRef == nil || slot.ResolvedRefDigest == nil || !digestPattern.MatchString(*slot.ResolvedRefDigest) {
			return reject("INVALID_TOOL_RECEIPT", "resolved slot fields")
		}
	default:
		return reject("UNKNOWN_ENUM", "resolution_state")
	}
	return nil
}

func validateAuthorityRefV1(ref authorityRefV1, slot executionSlotV1, resultPolicies resultPolicySetV1) error {
	if !canonicalUUIDv7(ref.AuthorityID) || !safePositiveIntegerV1(ref.AuthorityVersion) || !digestPattern.MatchString(ref.AuthoritySemanticDigest) {
		return reject("INVALID_TOOL_RECEIPT", "authority_ref")
	}
	switch slot.RefType {
	case "resource":
		if _, exists := resultPolicies.resourceTypes[ref.ResourceType]; !exists || ref.ProjectionID != "" {
			return reject("INVALID_TOOL_RECEIPT", "resource authority_ref")
		}
	case "approval":
		if ref.ResourceType != "" || !canonicalUUIDv7(ref.ProjectionID) {
			return reject("INVALID_TOOL_RECEIPT", "approval authority_ref")
		}
	default:
		if ref.ResourceType != "" || ref.ProjectionID != "" {
			return reject("INVALID_TOOL_RECEIPT", "authority_ref extensions")
		}
	}
	return nil
}

func executionRefDigestV1(snapshot toolReceiptSnapshotV1, slot executionSlotV1, ref authorityRefV1) (string, error) {
	projection := executionRefDigestProjectionV1{
		CanonicalizationVersion: "dora.tool_execution_ref.v1", ReceiptID: snapshot.ReceiptID,
		ParentRequestDigest: snapshot.RequestSemanticDigest, ToolKey: snapshot.ToolKey,
		DefinitionVersion: snapshot.DefinitionVersion, IntentSchemaVersion: snapshot.IntentSchemaVersion, RefSlot: slot.RefSlot,
		SlotOrdinal: slot.SlotOrdinal, RefType: slot.RefType, RefSchemaVersion: slot.RefSchemaVersion,
		AuthorityOwner: slot.AuthorityOwner, IdempotencyKey: slot.IdempotencyKey,
		RequestDigest: slot.RequestDigest, QueryContract: slot.QueryContract, AuthorityRef: ref,
	}
	canonical, err := canonicalJSON(projection)
	if err != nil {
		return "", reject("INVALID_TOOL_RECEIPT", "execution ref canonical")
	}
	return semanticDigest(toolExecutionRefDigestDomainV1, canonical), nil
}

func slotPolicyKey(toolKey, definitionVersion, refSlot string) string {
	return toolKey + "\x00" + definitionVersion + "\x00" + refSlot
}

func slotMatchesPolicy(slot executionSlotV1, policy executionSlotPolicyV1) bool {
	return slot.RefSlot == policy.RefSlot && slot.SlotOrdinal == policy.SlotOrdinal && slot.RefType == policy.RefType &&
		slot.RefSchemaVersion == policy.RefSchemaVersion && slot.AuthorityOwner == policy.AuthorityOwner && slot.QueryContract == policy.QueryContract
}

func projectResultRefs(slots []executionSlotV1, requested []string) ([]resultRefV1, error) {
	byName := make(map[string]executionSlotV1, len(slots))
	for _, slot := range slots {
		byName[slot.RefSlot] = slot
	}
	refs := make([]resultRefV1, len(requested))
	lastOrdinal := int64(0)
	for index, name := range requested {
		slot, exists := byName[name]
		if !exists || slot.ResolutionState != "resolved" || slot.ResolvedRefDigest == nil || slot.SlotOrdinal <= lastOrdinal {
			return nil, reject("RESULT_REF_MISMATCH", name)
		}
		refs[index] = resultRefV1{RefSlot: name, RefDigest: *slot.ResolvedRefDigest}
		lastOrdinal = slot.SlotOrdinal
	}
	return refs, nil
}

func validateStoredResultRefs(slots []executionSlotV1, refs []resultRefV1) error {
	requested := make([]string, len(refs))
	for index, ref := range refs {
		requested[index] = ref.RefSlot
	}
	expected, err := projectResultRefs(slots, requested)
	if err != nil || !equalJSON(expected, refs) {
		return reject("RESULT_REF_MISMATCH", "stored result_refs")
	}
	return nil
}

func validateResultEvidence(result graphToolResultV1, snapshot toolReceiptSnapshotV1, refs []resultRefV1, slotPolicies map[string]executionSlotPolicyV1) error {
	if result.ReceiptRef.ReceiptID != snapshot.ReceiptID {
		return reject("INVALID_TOOL_RECEIPT", "self receipt_ref")
	}
	selected := make([]executionSlotV1, 0, len(refs))
	for _, ref := range refs {
		for _, slot := range snapshot.ExecutionSlots {
			if slot.RefSlot == ref.RefSlot && slot.ResolvedRefDigest != nil && *slot.ResolvedRefDigest == ref.RefDigest {
				selected = append(selected, slot)
			}
		}
	}
	if len(selected) != len(refs) {
		return reject("RESULT_REF_MISMATCH", "unresolved selected evidence")
	}
	switch result.Status {
	case "accepted":
		if result.OperationRef == nil || result.ReceiptRef.DispatchReceiptID == nil {
			return reject("RESULT_REF_MISMATCH", "accepted refs")
		}
		operation, operationOK := singleSlotByType(selected, "operation")
		batch, batchOK := singleSlotByType(selected, "batch")
		dispatch, dispatchOK := singleSlotByType(selected, "dispatch_receipt")
		if !operationOK || !batchOK || !dispatchOK || len(refs) != 3 ||
			!authorityMatches(operation, result.OperationRef.OperationID, result.OperationRef.OperationVersion, result.OperationRef.OperationDigest) ||
			!authorityMatches(batch, result.OperationRef.BatchID, result.OperationRef.BatchVersion, result.OperationRef.BatchDigest) ||
			!authorityMatches(dispatch, *result.ReceiptRef.DispatchReceiptID, *result.ReceiptRef.DispatchReceiptVersion, *result.ReceiptRef.DispatchReceiptDigest) {
			return reject("RESULT_REF_MISMATCH", "accepted authority evidence")
		}
		if ledgerContainsType(snapshot.ExecutionSlots, "approval", "resource") {
			return reject("RESULT_REF_MISMATCH", "accepted incompatible ledger")
		}
	case "waiting_user":
		approval, ok := singleSlotByType(selected, "approval")
		if !ok || result.ApprovalRef == nil || len(refs) != len(result.ResourceRefs)+1 ||
			!authorityMatches(approval, result.ApprovalRef.ApprovalID, result.ApprovalRef.ApprovalVersion, result.ApprovalRef.ApprovalDigest) ||
			approval.AuthorityRef.ProjectionID != result.ApprovalRef.CardID || !resourceEvidenceMatches(result.ResourceRefs, selected) ||
			ledgerContainsType(snapshot.ExecutionSlots, "operation", "batch", "dispatch_receipt") {
			return reject("RESULT_REF_MISMATCH", "waiting_user authority evidence")
		}
	case "completed", "partial":
		if len(refs) != len(result.ResourceRefs) || !resourceEvidenceMatches(result.ResourceRefs, selected) ||
			ledgerContainsType(snapshot.ExecutionSlots, "approval", "operation", "batch", "dispatch_receipt") {
			return reject("RESULT_REF_MISMATCH", "resource authority evidence")
		}
	case "failed":
		if len(refs) != 0 || ledgerContainsSideEffect(snapshot, slotPolicies) {
			return reject("RESULT_REF_MISMATCH", "failed side-effect evidence")
		}
	case "cancelled":
		if result.CancellationStage == nil {
			return reject("RESULT_REF_MISMATCH", "cancelled stage evidence")
		}
		if *result.CancellationStage == "before_side_effect" {
			if len(refs) != 0 || ledgerContainsSideEffect(snapshot, slotPolicies) {
				return reject("RESULT_REF_MISMATCH", "cancelled-before side-effect evidence")
			}
		} else if !exactSideEffectEvidence(snapshot, selected, slotPolicies) {
			return reject("RESULT_REF_MISMATCH", "cancelled-after resolved evidence")
		}
	}
	return nil
}

func exactSideEffectEvidence(snapshot toolReceiptSnapshotV1, selected []executionSlotV1, slotPolicies map[string]executionSlotPolicyV1) bool {
	selectedBySlot := make(map[string]struct{}, len(selected))
	for _, slot := range selected {
		if !slotIsSideEffect(snapshot, slot, slotPolicies) {
			return false
		}
		selectedBySlot[slot.RefSlot] = struct{}{}
	}
	count := 0
	for _, slot := range snapshot.ExecutionSlots {
		if !slotIsSideEffect(snapshot, slot, slotPolicies) {
			continue
		}
		count++
		if _, exists := selectedBySlot[slot.RefSlot]; !exists {
			return false
		}
	}
	return count > 0 && len(selected) == count
}

func ledgerContainsSideEffect(snapshot toolReceiptSnapshotV1, slotPolicies map[string]executionSlotPolicyV1) bool {
	for _, slot := range snapshot.ExecutionSlots {
		if slotIsSideEffect(snapshot, slot, slotPolicies) {
			return true
		}
	}
	return false
}

func slotIsSideEffect(snapshot toolReceiptSnapshotV1, slot executionSlotV1, slotPolicies map[string]executionSlotPolicyV1) bool {
	policy, exists := slotPolicies[slotPolicyKey(snapshot.ToolKey, snapshot.DefinitionVersion, slot.RefSlot)]
	return exists && policy.EffectClass == "side_effect"
}

func singleSlotByType(slots []executionSlotV1, refType string) (executionSlotV1, bool) {
	var matched executionSlotV1
	count := 0
	for _, slot := range slots {
		if slot.RefType == refType {
			matched = slot
			count++
		}
	}
	return matched, count == 1
}

func resourceEvidenceMatches(refs []resourceRefV1, slots []executionSlotV1) bool {
	resources := make([]executionSlotV1, 0, len(slots))
	for _, slot := range slots {
		if slot.RefType == "resource" {
			resources = append(resources, slot)
		}
	}
	if len(resources) != len(refs) {
		return false
	}
	for _, ref := range refs {
		matched := false
		for _, slot := range resources {
			if authorityMatches(slot, ref.ResourceID, ref.ResourceVersion, ref.ContentDigest) && slot.AuthorityRef.ResourceType == ref.ResourceType {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func ledgerContainsType(slots []executionSlotV1, refTypes ...string) bool {
	for _, slot := range slots {
		for _, refType := range refTypes {
			if slot.RefType == refType {
				return true
			}
		}
	}
	return false
}

func authorityMatches(slot executionSlotV1, id string, version int64, digest string) bool {
	return slot.AuthorityRef != nil && slot.AuthorityRef.AuthorityID == id && slot.AuthorityRef.AuthorityVersion == version && slot.AuthorityRef.AuthoritySemanticDigest == digest
}

func cloneReceiptSnapshot(input toolReceiptSnapshotV1) toolReceiptSnapshotV1 {
	raw, err := json.Marshal(input)
	if err != nil {
		panic(err)
	}
	var output toolReceiptSnapshotV1
	if err := json.Unmarshal(raw, &output); err != nil {
		panic(err)
	}
	return output
}

func equalJSON(left, right any) bool {
	leftJSON, leftErr := canonicalJSON(left)
	rightJSON, rightErr := canonicalJSON(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
