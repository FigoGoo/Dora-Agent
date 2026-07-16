package w2r01_test

import (
	"fmt"
	"strings"
)

// ApprovalContinuationParentReceiptProjectionV1 是 R03 校验器从 R01 父 Tool Receipt 中获取的最小只读投影。
// 该投影只在 R01 完成严格解码、执行引用固结和重算快照摘要后生成，不允许 R03 绕过 R01 不变量组装内部 DTO。
type ApprovalContinuationParentReceiptProjectionV1 struct {
	// ReceiptID 是父 Tool Receipt 的 UUIDv7 标识。
	ReceiptID string
	// SessionID 是父 Tool Receipt 归属的 Session UUIDv7。
	SessionID string
	// TurnID 是父 Tool Receipt 归属的原始 Turn UUIDv7。
	TurnID string
	// RunID 是父 Tool Receipt 归属的原始 Run UUIDv7。
	RunID string
	// ToolCallID 是产生父 Tool Receipt 的 Tool Call UUIDv7。
	ToolCallID string
	// ToolKey 是创建父 Tool Receipt 时固结的 Tool Key。
	ToolKey string
	// DefinitionVersion 是创建父 Tool Receipt 时固结的 Tool 定义版本。
	DefinitionVersion string
	// IntentSchemaVersion 是创建父 Tool Receipt 时固结的 Intent Schema 版本。
	IntentSchemaVersion string
	// ResultSchemaVersion 是父 Tool Receipt 结果使用的 Schema 版本。
	ResultSchemaVersion string
	// RequestSemanticDigest 是父 Tool Request 的语义摘要。
	RequestSemanticDigest string
	// WriteState 是重建后父 Tool Receipt 的写入状态，成功时必须为 frozen。
	WriteState string
	// ReceiptVersion 是执行引用预留、解析和结果固结后的 Receipt 版本。
	ReceiptVersion int64
	// SnapshotDigest 是 R01 对最终 canonical Receipt 快照实时重算的语义摘要。
	SnapshotDigest string
	// ApprovalID 在 Result 含 approval_ref 时返回其 Approval UUIDv7，否则为空。
	ApprovalID string
	// ApprovalVersion 在 Result 含 approval_ref 时返回其 Approval 版本，否则为零。
	ApprovalVersion int64
	// ApprovalDigest 在 Result 含 approval_ref 时返回其语义摘要，否则为空。
	ApprovalDigest string
	// ApprovalCardID 在 Result 含 approval_ref 时返回其 Card UUIDv7，否则为空。
	ApprovalCardID string
}

// ApprovalContinuationParentReceiptResultV1 表示 R01 父回执重建结果。
// 成功时 Projection 非 nil 且 ErrorCode 为空；任一契约校验失败时 Projection 为 nil 且 ErrorCode 为稳定错误码。
type ApprovalContinuationParentReceiptResultV1 struct {
	// Projection 是通过 R01 全部不变量后生成的父回执投影。
	Projection *ApprovalContinuationParentReceiptProjectionV1
	// ErrorCode 是失败关闭时的 R01 稳定错误码。
	ErrorCode string
}

// EvaluateApprovalContinuationParentReceiptV1 使用 R01 权威校验器重建 Approval Continuation 的父 Tool Receipt。
// 调用方必须传入完整 parent_receipt fixture 原始 JSON 和已审核的 Graph Tool Result corpus 原始 JSON；函数无副作用，并在任一未知字段、非法状态或证据不一致时失败关闭。
// R01 内在合法但不含 approval_ref 的 Result 仍返回 Receipt 投影，由 R03 在更高层稳定归类为 Approval binding 错误。
func EvaluateApprovalContinuationParentReceiptV1(parentReceiptJSON, graphToolResultCorpusJSON []byte) ApprovalContinuationParentReceiptResultV1 {
	var parent receiptEvidenceCaseV1
	if err := decodeApprovalContinuationInputV1(parentReceiptJSON, &parent); err != nil {
		return approvalContinuationParentReceiptFailureV1(err)
	}

	var resultCorpus resultCorpusV1
	if err := decodeApprovalContinuationInputV1(graphToolResultCorpusJSON, &resultCorpus); err != nil {
		return approvalContinuationParentReceiptFailureV1(err)
	}
	if resultCorpus.SchemaVersion != "graph_tool_result_v1_corpus.v1" {
		return approvalContinuationParentReceiptFailureV1(reject("UNKNOWN_VERSION", "graph tool result corpus"))
	}
	resultPolicies, err := buildApprovalContinuationResultPoliciesV1(resultCorpus)
	if err != nil {
		return approvalContinuationParentReceiptFailureV1(err)
	}

	slotPolicy := executionSlotPolicyV1{
		ToolKey: "plan_creation_spec", DefinitionVersion: "plan_creation_spec.v1alpha1",
		RefSlot: "approval.primary", SlotOrdinal: 1, RefType: "approval", RefSchemaVersion: "approval_ref.v1",
		AuthorityOwner: "agent", QueryContract: "agent.approval.query.v1", EffectClass: "side_effect",
	}
	policies := receiptPolicySetV1{
		Result: resultPolicies,
		Slots: map[string]executionSlotPolicyV1{
			slotPolicyKey(slotPolicy.ToolKey, slotPolicy.DefinitionVersion, slotPolicy.RefSlot): slotPolicy,
		},
	}
	current, err := prepareEvidenceSnapshotV1(parent.BaseSnapshot, parent.SetupSlots, policies)
	if err != nil {
		return approvalContinuationParentReceiptFailureV1(err)
	}
	current, err = applyReceiptCommandV1(current, receiptCommandV1{
		Kind: "freeze", PresentedRequestDigest: current.RequestSemanticDigest,
		ExpectedReceiptVersion: current.ReceiptVersion, OwnerFence: current.OwnerFence,
		Result: parent.Result, ResultRefSlots: parent.ResultRefSlots,
	}, policies)
	if err != nil {
		return approvalContinuationParentReceiptFailureV1(err)
	}
	canonical, err := canonicalJSON(current)
	if err != nil {
		return approvalContinuationParentReceiptFailureV1(reject("CANONICAL_JSON_INVALID", "tool receipt snapshot"))
	}
	projection := &ApprovalContinuationParentReceiptProjectionV1{
		ReceiptID: current.ReceiptID, SessionID: current.SessionID, TurnID: current.TurnID,
		RunID: current.RunID, ToolCallID: current.ToolCallID, ToolKey: current.ToolKey,
		DefinitionVersion: current.DefinitionVersion, IntentSchemaVersion: current.IntentSchemaVersion,
		ResultSchemaVersion: current.ResultSchemaVersion, RequestSemanticDigest: current.RequestSemanticDigest,
		WriteState: current.WriteState, ReceiptVersion: current.ReceiptVersion,
		SnapshotDigest: semanticDigest(toolReceiptSnapshotDigestDomainV1, canonical),
	}
	if current.Result != nil && current.Result.ApprovalRef != nil {
		approval := current.Result.ApprovalRef
		projection.ApprovalID = approval.ApprovalID
		projection.ApprovalVersion = approval.ApprovalVersion
		projection.ApprovalDigest = approval.ApprovalDigest
		projection.ApprovalCardID = approval.CardID
	}
	return ApprovalContinuationParentReceiptResultV1{Projection: projection}
}

func decodeApprovalContinuationInputV1(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > 1024*1024 {
		return reject("LIMIT_EXCEEDED", "approval continuation input")
	}
	if err := inspectJSON(raw); err != nil {
		return err
	}
	if err := strictDecode(raw, target); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return reject("UNKNOWN_FIELD", "approval continuation input")
		}
		if strings.Contains(err.Error(), "cannot unmarshal") {
			return reject("TYPE_MISMATCH", "approval continuation input")
		}
		return reject("INVALID_JSON", "approval continuation input")
	}
	return nil
}

func buildApprovalContinuationResultPoliciesV1(corpus resultCorpusV1) (resultPolicySetV1, error) {
	policies := resultPolicySetV1{
		resultCodes:   make(map[string]resultCodePolicyV1, len(corpus.ResultPolicies)),
		warningCodes:  make(map[string]warningCodePolicyV1, len(corpus.WarningPolicies)),
		resourceTypes: make(map[string]struct{}, len(corpus.ResourceTypes)),
	}
	lastResultCode := ""
	for index, policy := range corpus.ResultPolicies {
		if !upperCodePattern.MatchString(policy.ResultCode) || !validResultStatus(policy.Status) || !validResultEffectPolicy(policy) || isForbiddenInternalOutcome(policy.ResultCode) ||
			(index > 0 && policy.ResultCode <= lastResultCode) {
			return resultPolicySetV1{}, reject("INVALID_RESULT_POLICY", fmt.Sprintf("result_code_policies[%d]", index))
		}
		lastResultCode = policy.ResultCode
		if _, exists := policies.resultCodes[policy.ResultCode]; exists {
			return resultPolicySetV1{}, reject("INVALID_RESULT_POLICY", "duplicate result code")
		}
		policies.resultCodes[policy.ResultCode] = policy
	}
	lastWarningCode := ""
	for index, policy := range corpus.WarningPolicies {
		if !upperCodePattern.MatchString(policy.Code) || (policy.EffectClass != "informational" && policy.EffectClass != "failed_target") ||
			(index > 0 && policy.Code <= lastWarningCode) || len(policy.AllowedStatuses) == 0 {
			return resultPolicySetV1{}, reject("INVALID_WARNING_POLICY", fmt.Sprintf("warning_code_policies[%d]", index))
		}
		lastWarningCode = policy.Code
		if _, exists := policies.warningCodes[policy.Code]; exists {
			return resultPolicySetV1{}, reject("INVALID_WARNING_POLICY", "duplicate warning code")
		}
		seenParams := make(map[string]struct{}, len(policy.Params))
		lastParamKey := ""
		for paramIndex, param := range policy.Params {
			if !snakeKeyPattern.MatchString(param.Key) || !validWarningParamPolicyV1(param) ||
				(paramIndex > 0 && param.Key <= lastParamKey) {
				return resultPolicySetV1{}, reject("INVALID_WARNING_POLICY", fmt.Sprintf("warning_code_policies[%d].params[%d]", index, paramIndex))
			}
			lastParamKey = param.Key
			if _, exists := seenParams[param.Key]; exists {
				return resultPolicySetV1{}, reject("INVALID_WARNING_POLICY", "duplicate warning param")
			}
			seenParams[param.Key] = struct{}{}
		}
		lastStatus := ""
		for statusIndex, status := range policy.AllowedStatuses {
			if !validResultStatus(status) || (statusIndex > 0 && status <= lastStatus) {
				return resultPolicySetV1{}, reject("INVALID_WARNING_POLICY", "allowed statuses")
			}
			lastStatus = status
		}
		policies.warningCodes[policy.Code] = policy
	}
	lastResourceType := ""
	for index, resourceType := range corpus.ResourceTypes {
		if !snakeKeyPattern.MatchString(resourceType) || (index > 0 && resourceType <= lastResourceType) {
			return resultPolicySetV1{}, reject("INVALID_RESOURCE_TYPE_POLICY", fmt.Sprintf("resource_types[%d]", index))
		}
		lastResourceType = resourceType
		if _, exists := policies.resourceTypes[resourceType]; exists {
			return resultPolicySetV1{}, reject("INVALID_RESOURCE_TYPE_POLICY", "duplicate resource type")
		}
		policies.resourceTypes[resourceType] = struct{}{}
	}
	return policies, nil
}

func approvalContinuationParentReceiptFailureV1(err error) ApprovalContinuationParentReceiptResultV1 {
	code := errorCode(err)
	if code == "INTERNAL_TEST_ERROR" {
		code = "INVALID_TOOL_RECEIPT"
	}
	return ApprovalContinuationParentReceiptResultV1{ErrorCode: code}
}
