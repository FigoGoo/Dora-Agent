package contract_test

import (
	"encoding/json"
	"os"
	"testing"

	w2r01 "github.com/FigoGoo/Dora-Agent/agent/tests/contract/w2r01"
)

const approvalContinuationGraphToolResultCorpusPathV1 = "testdata/w2_r01/graph_tool_result_v1.json"

// approvalContinuationParentReceiptFixtureV1 是 R03→R01 边界的序列化父 Receipt 输入；
// R03 只拥有该 fixture 投影，所有状态迁移与不变量仍由 w2r01 负责。
type approvalContinuationParentReceiptFixtureV1 struct {
	ID                     string                                `json:"id"`
	BaseSnapshot           approvalContinuationReceiptSnapshotV1 `json:"base_snapshot"`
	SetupSlots             []approvalContinuationResolvedSlotV1  `json:"setup_slots"`
	Result                 json.RawMessage                       `json:"result"`
	ResultRefSlots         []string                              `json:"result_ref_slots"`
	Expect                 string                                `json:"expect"`
	ErrorCode              string                                `json:"error_code"`
	ExpectedSnapshotDigest string                                `json:"expected_snapshot_digest"`
}

// approvalContinuationReceiptSnapshotV1 是 R03 corpus 中父 Receipt 的输入快照 DTO，
// 仅用于无损序列化后交给 R01 严格解码，不在 R03 内解释状态机语义。
type approvalContinuationReceiptSnapshotV1 struct {
	SchemaVersion                  string                                `json:"schema_version"`
	ReceiptID                      string                                `json:"receipt_id"`
	SessionID                      string                                `json:"session_id"`
	TurnID                         string                                `json:"turn_id"`
	RunID                          string                                `json:"run_id"`
	ToolCallID                     string                                `json:"tool_call_id"`
	ToolKey                        string                                `json:"tool_key"`
	DefinitionVersion              string                                `json:"definition_version"`
	IntentSchemaVersion            string                                `json:"intent_schema_version"`
	ResultSchemaVersion            string                                `json:"result_schema_version"`
	RequestCanonicalizationVersion string                                `json:"request_canonicalization_version"`
	RequestSemanticDigest          string                                `json:"request_semantic_digest"`
	WriteState                     string                                `json:"write_state"`
	ReceiptVersion                 int64                                 `json:"receipt_version"`
	OwnerFence                     int64                                 `json:"owner_fence"`
	ExecutionSlots                 []approvalContinuationExecutionSlotV1 `json:"execution_slots"`
	Result                         json.RawMessage                       `json:"result,omitempty"`
	ResultDigest                   *string                               `json:"result_digest,omitempty"`
	ResultRefs                     []approvalContinuationResultRefV1     `json:"result_refs"`
}

// approvalContinuationResolvedSlotV1 固定父 Receipt 预解析 slot 与 authority ref 的成对输入。
type approvalContinuationResolvedSlotV1 struct {
	Slot         approvalContinuationExecutionSlotV1 `json:"slot"`
	AuthorityRef approvalContinuationAuthorityRefV1  `json:"authority_ref"`
}

// approvalContinuationExecutionSlotV1 镜像 R01 slot 的序列化字段，禁止 R03 自行派生或放宽字段。
type approvalContinuationExecutionSlotV1 struct {
	RefSlot           string                              `json:"ref_slot"`
	SlotOrdinal       int64                               `json:"slot_ordinal"`
	RefType           string                              `json:"ref_type"`
	RefSchemaVersion  string                              `json:"ref_schema_version"`
	AuthorityOwner    string                              `json:"authority_owner"`
	IdempotencyKey    string                              `json:"idempotency_key"`
	RequestDigest     string                              `json:"request_digest"`
	QueryContract     string                              `json:"query_contract"`
	ResolutionState   string                              `json:"resolution_state"`
	AuthorityRef      *approvalContinuationAuthorityRefV1 `json:"authority_ref,omitempty"`
	ResolvedRefDigest *string                             `json:"resolved_ref_digest,omitempty"`
}

// approvalContinuationAuthorityRefV1 表示 R01 slot 所消费的候选 authority 引用。
type approvalContinuationAuthorityRefV1 struct {
	AuthorityID             string `json:"authority_id"`
	AuthorityVersion        int64  `json:"authority_version"`
	AuthoritySemanticDigest string `json:"authority_semantic_digest"`
	ResourceType            string `json:"resource_type,omitempty"`
	ProjectionID            string `json:"projection_id,omitempty"`
}

// approvalContinuationResultRefV1 表示冻结结果引用的 slot/digest 对。
type approvalContinuationResultRefV1 struct {
	RefSlot   string `json:"ref_slot"`
	RefDigest string `json:"ref_digest"`
}

// approvalContinuationApprovalRefV1 是 R01 成功投影中 R03 实际消费的最小 Approval 引用。
type approvalContinuationApprovalRefV1 struct {
	ApprovalID      string `json:"approval_id"`
	ApprovalVersion int64  `json:"approval_version"`
	ApprovalDigest  string `json:"approval_digest"`
	CardID          string `json:"card_id"`
}

// approvalContinuationReceiptResultV1 保留旧 R03 断言所需的 result→approval_ref 只读形状。
type approvalContinuationReceiptResultV1 struct {
	ApprovalRef *approvalContinuationApprovalRefV1
}

// approvalContinuationParentReceiptProjectionV1 是 R03 可见的父 Receipt 最小只读投影；
// SnapshotDigest 必须由 R01 对冻结后完整快照重算，R03 不得自行构造。
type approvalContinuationParentReceiptProjectionV1 struct {
	ReceiptID             string
	SessionID             string
	TurnID                string
	RunID                 string
	ToolCallID            string
	ToolKey               string
	DefinitionVersion     string
	IntentSchemaVersion   string
	ResultSchemaVersion   string
	RequestSemanticDigest string
	WriteState            string
	ReceiptVersion        int64
	SnapshotDigest        string
	Result                *approvalContinuationReceiptResultV1
}

// evaluateApprovalContinuationParentReceiptV1 将本地 fixture canonical 化后交给 R01 纯 evaluator，
// 并只把已通过 R01 不变量的窄投影映射回 R03。
func evaluateApprovalContinuationParentReceiptV1(t *testing.T, fixture approvalContinuationParentReceiptFixtureV1) (approvalContinuationParentReceiptProjectionV1, string) {
	t.Helper()
	parentReceiptJSON, err := canonicalJSON(fixture)
	if err != nil {
		return approvalContinuationParentReceiptProjectionV1{}, "CANONICAL_JSON_INVALID"
	}
	graphToolResultCorpusJSON, err := os.ReadFile(approvalContinuationGraphToolResultCorpusPathV1)
	if err != nil {
		t.Fatal(err)
	}
	evaluation := w2r01.EvaluateApprovalContinuationParentReceiptV1(parentReceiptJSON, graphToolResultCorpusJSON)
	if evaluation.ErrorCode != "" || evaluation.Projection == nil {
		if evaluation.ErrorCode == "" {
			return approvalContinuationParentReceiptProjectionV1{}, "INTERNAL_TEST_ERROR"
		}
		return approvalContinuationParentReceiptProjectionV1{}, evaluation.ErrorCode
	}
	projection := evaluation.Projection
	result := &approvalContinuationReceiptResultV1{}
	if projection.ApprovalID != "" || projection.ApprovalVersion != 0 || projection.ApprovalDigest != "" || projection.ApprovalCardID != "" {
		result.ApprovalRef = &approvalContinuationApprovalRefV1{
			ApprovalID:      projection.ApprovalID,
			ApprovalVersion: projection.ApprovalVersion,
			ApprovalDigest:  projection.ApprovalDigest,
			CardID:          projection.ApprovalCardID,
		}
	}
	return approvalContinuationParentReceiptProjectionV1{
		ReceiptID:             projection.ReceiptID,
		SessionID:             projection.SessionID,
		TurnID:                projection.TurnID,
		RunID:                 projection.RunID,
		ToolCallID:            projection.ToolCallID,
		ToolKey:               projection.ToolKey,
		DefinitionVersion:     projection.DefinitionVersion,
		IntentSchemaVersion:   projection.IntentSchemaVersion,
		ResultSchemaVersion:   projection.ResultSchemaVersion,
		RequestSemanticDigest: projection.RequestSemanticDigest,
		WriteState:            projection.WriteState,
		ReceiptVersion:        projection.ReceiptVersion,
		SnapshotDigest:        projection.SnapshotDigest,
		Result:                result,
	}, ""
}

// mutateApprovalContinuationResultApprovalRefV1 只服务于 R03 失败向量，保留完整 Result raw 字段，
// 且仅替换 approval_ref 后重新 canonical 编码。
func mutateApprovalContinuationResultApprovalRefV1(raw *json.RawMessage, mutate func(*approvalContinuationApprovalRefV1)) {
	var object map[string]json.RawMessage
	if err := strictDecode(*raw, &object); err != nil {
		panic(err)
	}
	approvalRaw, exists := object["approval_ref"]
	if !exists {
		panic("R03 parent result 缺少 approval_ref")
	}
	var approval approvalContinuationApprovalRefV1
	if err := strictDecode(approvalRaw, &approval); err != nil {
		panic(err)
	}
	mutate(&approval)
	encodedApproval, err := canonicalJSON(approval)
	if err != nil {
		panic(err)
	}
	object["approval_ref"] = encodedApproval
	encodedResult, err := canonicalJSON(object)
	if err != nil {
		panic(err)
	}
	*raw = encodedResult
}
