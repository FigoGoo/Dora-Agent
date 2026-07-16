package contract_test

import (
	"reflect"
	"testing"
)

// failedAfterReceiptCorpusV1 固定副作用后永久失败与 Business 明确未提交权威的测试专用语料。
type failedAfterReceiptCorpusV1 struct {
	SchemaVersion             string                     `json:"schema_version"`
	FixtureID                 string                     `json:"fixture_id"`
	SlotPolicies              []executionSlotPolicyV1    `json:"slot_policies"`
	BaseSnapshot              toolReceiptSnapshotV1      `json:"base_snapshot"`
	ConsumptionSlot           executionSlotV1            `json:"consumption_slot"`
	ConsumptionAuthority      authorityRefV1             `json:"consumption_authority"`
	BusinessSlot              executionSlotV1            `json:"business_slot"`
	BusinessOnlySlot          executionSlotV1            `json:"business_only_slot"`
	BusinessNegativeAuthority authorityRefV1             `json:"business_negative_authority"`
	Results                   failedAfterResultsV1       `json:"results"`
	Cases                     []failedAfterReceiptCaseV1 `json:"cases"`
}

// failedAfterResultsV1 保存三种 pinned ResultCode 对应的合法 GraphToolResult 候选。
type failedAfterResultsV1 struct {
	PostConsumption     graphToolResultV1 `json:"post_consumption"`
	ApproveNotCommitted graphToolResultV1 `json:"approve_not_committed"`
	RejectNotCommitted  graphToolResultV1 `json:"reject_not_committed"`
}

// failedAfterReceiptCaseV1 描述固定账本形态、结果投影和预期失败关闭原因。
type failedAfterReceiptCaseV1 struct {
	ID                     string   `json:"id"`
	Setup                  string   `json:"setup"`
	Result                 string   `json:"result"`
	ResultRefSlots         []string `json:"result_ref_slots"`
	Expect                 string   `json:"expect"`
	ErrorCode              string   `json:"error_code"`
	ExpectedSnapshotDigest string   `json:"expected_snapshot_digest"`
}

func TestToolReceiptFailedAfterV1Corpus(t *testing.T) {
	corpus := loadFailedAfterReceiptCorpusV1(t)
	resultCorpus := loadResultCorpus(t)
	policies := receiptPolicySetV1{Result: buildResultPolicies(t, resultCorpus), Slots: buildSlotPolicies(t, corpus.SlotPolicies)}
	if err := validateToolReceiptSnapshotV1(corpus.BaseSnapshot, policies); err != nil {
		t.Fatalf("failed-after 基础 Receipt 非法: %v", err)
	}
	wantIDs := []string{
		"TR-FA-P01-post-consumption", "TR-FA-N01-post-consumption-missing-ref",
		"TR-FA-P02-approve-not-committed", "TR-FA-N02-approve-missing-negative-ref",
		"TR-FA-N03-negative-only-cannot-use-failed-after", "TR-FA-P03-reject-not-committed",
		"TR-FA-N04-reject-not-committed-missing-ref", "TR-FA-N05-business-unknown-unresolved",
		"TR-FA-N06-business-outcome-missing", "TR-FA-N07-business-outcome-invalid",
		"TR-FA-N08-non-business-outcome-forbidden",
	}
	gotIDs := make([]string, 0, len(corpus.Cases))
	for _, testCase := range corpus.Cases {
		testCase := testCase
		gotIDs = append(gotIDs, testCase.ID)
		t.Run(testCase.ID, func(t *testing.T) {
			after, err := evaluateFailedAfterReceiptCaseV1(corpus, testCase, policies)
			if testCase.Expect == "reject" {
				if err == nil || errorCode(err) != testCase.ErrorCode || testCase.ExpectedSnapshotDigest != "" {
					t.Fatalf("failed-after 拒绝结果 err=%v code=%s want=%s", err, errorCode(err), testCase.ErrorCode)
				}
				return
			}
			if testCase.Expect != "accept" || testCase.ErrorCode != "" || err != nil || testCase.ExpectedSnapshotDigest == "" {
				t.Fatalf("failed-after 合法向量元数据或执行错误: expect=%q code=%q err=%v", testCase.Expect, testCase.ErrorCode, err)
			}
			canonical, encodeErr := canonicalJSON(after)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			if digest := semanticDigest(toolReceiptSnapshotDigestDomainV1, canonical); digest != testCase.ExpectedSnapshotDigest {
				t.Errorf("failed-after snapshot digest=%s want=%s\ncanonical=%s", digest, testCase.ExpectedSnapshotDigest, canonical)
			}
		})
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("failed-after case exact-set=%v want=%v", gotIDs, wantIDs)
	}
}

// loadFailedAfterReceiptCorpusV1 严格加载共享 raw Corpus，避免测试代码静默补默认字段。
func loadFailedAfterReceiptCorpusV1(t *testing.T) failedAfterReceiptCorpusV1 {
	t.Helper()
	raw, err := w2R01CorpusFS.ReadFile(failedAfterReceiptCorpusPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := inspectJSON(raw); err != nil {
		t.Fatalf("failed-after corpus JSON 非法: %v", err)
	}
	var corpus failedAfterReceiptCorpusV1
	if err := strictDecode(raw, &corpus); err != nil {
		t.Fatalf("解析 failed-after corpus: %v", err)
	}
	if corpus.SchemaVersion != "tool_receipt_failed_after_v1_corpus.v1" || corpus.FixtureID == "" || len(corpus.SlotPolicies) != 3 || len(corpus.Cases) != 11 {
		t.Fatalf("failed-after corpus 版本或覆盖不足: version=%q fixture=%q policies=%d cases=%d", corpus.SchemaVersion, corpus.FixtureID, len(corpus.SlotPolicies), len(corpus.Cases))
	}
	return corpus
}

// evaluateFailedAfterReceiptCaseV1 只模拟已评审的 reserve/resolve/freeze 纯状态机，不实现生产事务。
func evaluateFailedAfterReceiptCaseV1(corpus failedAfterReceiptCorpusV1, testCase failedAfterReceiptCaseV1, policies receiptPolicySetV1) (toolReceiptSnapshotV1, error) {
	current := corpus.BaseSnapshot
	var err error
	switch testCase.Setup {
	case "consumption_resolved":
		current, err = prepareEvidenceSnapshotV1(current, []resolvedSlotFixtureV1{{Slot: corpus.ConsumptionSlot, AuthorityRef: corpus.ConsumptionAuthority}}, policies)
	case "consumption_and_business_negative_resolved":
		current, err = prepareEvidenceSnapshotV1(current, []resolvedSlotFixtureV1{
			{Slot: corpus.ConsumptionSlot, AuthorityRef: corpus.ConsumptionAuthority},
			{Slot: corpus.BusinessSlot, AuthorityRef: corpus.BusinessNegativeAuthority},
		}, policies)
	case "business_negative_resolved":
		current, err = prepareEvidenceSnapshotV1(current, []resolvedSlotFixtureV1{{Slot: corpus.BusinessOnlySlot, AuthorityRef: corpus.BusinessNegativeAuthority}}, policies)
	case "consumption_resolved_business_prepared":
		current, err = prepareEvidenceSnapshotV1(current, []resolvedSlotFixtureV1{{Slot: corpus.ConsumptionSlot, AuthorityRef: corpus.ConsumptionAuthority}}, policies)
		if err == nil {
			current, err = applyReceiptCommandV1(current, receiptCommandV1{
				Kind: "reserve_slot", PresentedRequestDigest: current.RequestSemanticDigest,
				ExpectedReceiptVersion: current.ReceiptVersion, OwnerFence: current.OwnerFence, Slot: &corpus.BusinessSlot,
			}, policies)
		}
	case "business_outcome_missing":
		missing := corpus.BusinessNegativeAuthority
		missing.AuthorityOutcome = nil
		current, err = prepareEvidenceSnapshotV1(current, []resolvedSlotFixtureV1{{Slot: corpus.BusinessOnlySlot, AuthorityRef: missing}}, policies)
	case "business_outcome_invalid":
		invalid := corpus.BusinessNegativeAuthority
		unknown := "unknown"
		invalid.AuthorityOutcome = &unknown
		current, err = prepareEvidenceSnapshotV1(current, []resolvedSlotFixtureV1{{Slot: corpus.BusinessOnlySlot, AuthorityRef: invalid}}, policies)
	case "non_business_outcome_forbidden":
		invalid := corpus.ConsumptionAuthority
		committed := "committed"
		invalid.AuthorityOutcome = &committed
		current, err = prepareEvidenceSnapshotV1(current, []resolvedSlotFixtureV1{{Slot: corpus.ConsumptionSlot, AuthorityRef: invalid}}, policies)
	default:
		return toolReceiptSnapshotV1{}, reject("INVALID_TOOL_RECEIPT", "failed-after setup")
	}
	if err != nil {
		return toolReceiptSnapshotV1{}, err
	}
	result, ok := failedAfterResultByKeyV1(corpus.Results, testCase.Result)
	if !ok {
		return toolReceiptSnapshotV1{}, reject("INVALID_TOOL_RECEIPT", "failed-after result")
	}
	raw, err := canonicalJSON(result)
	if err != nil {
		return toolReceiptSnapshotV1{}, err
	}
	return applyReceiptCommandV1(current, receiptCommandV1{
		Kind: "freeze", PresentedRequestDigest: current.RequestSemanticDigest,
		ExpectedReceiptVersion: current.ReceiptVersion, OwnerFence: current.OwnerFence,
		Result: raw, ResultRefSlots: testCase.ResultRefSlots,
	}, policies)
}

// failedAfterResultByKeyV1 只接受 Corpus 冻结的三种结果模板，未知 key 必须失败关闭。
func failedAfterResultByKeyV1(results failedAfterResultsV1, key string) (graphToolResultV1, bool) {
	switch key {
	case "post_consumption":
		return results.PostConsumption, true
	case "approve_not_committed":
		return results.ApproveNotCommitted, true
	case "reject_not_committed":
		return results.RejectNotCommitted, true
	default:
		return graphToolResultV1{}, false
	}
}
