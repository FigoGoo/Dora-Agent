package reviewfreeze_test

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// reviewFreezeValidateGateTransitionV1 校验单个 Gate 跨提交迁移，阻止正式 Freeze 被降级、覆盖或跳过 CFE 重批流程。
func reviewFreezeValidateGateTransitionV1(base, head reviewFreezeGateV1) error {
	if base.Gate != head.Gate {
		return fmt.Errorf("gate identity changed from %q to %q", base.Gate, head.Gate)
	}

	switch base.Status {
	case "expansion_frozen":
		if head.Status != "expansion_frozen" && head.Status != "awaiting_review" {
			return fmt.Errorf("%s 非法迁移 %s -> %s", base.Gate, base.Status, head.Status)
		}
		return nil
	case "awaiting_review":
		switch head.Status {
		case "expansion_frozen", "awaiting_review":
			return nil
		case "review_frozen":
			return reviewFreezeValidateInitialFormalTransitionV1(base, head)
		default:
			return fmt.Errorf("%s 非法迁移 %s -> %s", base.Gate, base.Status, head.Status)
		}
	case "review_frozen":
		switch head.Status {
		case "review_frozen":
			return reviewFreezeRequireFormalGateUnchangedV1(base, head)
		case "approved":
			return reviewFreezeValidateApprovalTransitionV1(base, head)
		case "reopened":
			return reviewFreezeValidateReopenTransitionV1(base, head)
		default:
			return fmt.Errorf("%s 正式 Freeze 禁止降级 %s -> %s", base.Gate, base.Status, head.Status)
		}
	case "approved":
		switch head.Status {
		case "approved":
			// 已批准 Gate 的同态只允许语义完全不变；重批必须先形成一个可见的 reopened 基线，不能在单个 PR 原子跨越。
			return reviewFreezeRequireFormalGateUnchangedV1(base, head)
		case "reopened":
			return reviewFreezeValidateReopenTransitionV1(base, head)
		default:
			return fmt.Errorf("%s Approved 禁止降级 %s -> %s", base.Gate, base.Status, head.Status)
		}
	case "reopened":
		switch head.Status {
		case "reopened":
			return reviewFreezeRequireFormalGateUnchangedV1(base, head)
		case "approved":
			return reviewFreezeValidateReapprovalTransitionV1(base, head)
		default:
			return fmt.Errorf("%s Reopened 禁止降级 %s -> %s", base.Gate, base.Status, head.Status)
		}
	default:
		return fmt.Errorf("%s base status 非法=%q", base.Gate, base.Status)
	}
}

// reviewFreezeValidateInitialFormalTransitionV1 要求首个正式 Freeze 从完整候选生成 v1，且不得携带 CFE 或 supersedes。
func reviewFreezeValidateInitialFormalTransitionV1(base, head reviewFreezeGateV1) error {
	if head.Freeze == nil || head.ReopenException != nil {
		return fmt.Errorf("%s 初次 Review Freeze 必须有 freeze 且不得携带 CFE", base.Gate)
	}
	version, err := reviewFreezeParseVersionV1(head.Gate, head.Freeze.FreezeID)
	if err != nil || version != 1 || head.Freeze.SupersedesFreezeID != "" {
		return fmt.Errorf("%s 初次 Review Freeze 必须是 v1 且无 supersedes", base.Gate)
	}
	if !reflect.DeepEqual(base.CandidateEvidence, head.CandidateEvidence) {
		return fmt.Errorf("%s 初次 Review Freeze 不得同时替换 candidate evidence", base.Gate)
	}
	if len(base.CandidateEvidence) != 1 || base.CandidateEvidence[0].Coverage != "gate_candidate" {
		return fmt.Errorf("%s 初次 Review Freeze 缺唯一完整 candidate", base.Gate)
	}
	candidate := base.CandidateEvidence[0]
	if head.Freeze.ContractManifestPath != candidate.ContractManifestPath ||
		head.Freeze.ContractManifestSHA256 != candidate.ContractManifestSHA256 ||
		!reflect.DeepEqual(head.Freeze.VectorIDs, candidate.VectorIDs) ||
		!reflect.DeepEqual(head.Freeze.TargetTests, candidate.TargetTests) {
		return fmt.Errorf("%s 初次 Freeze 未采用 awaiting_review candidate exact-set", base.Gate)
	}
	return nil
}

// reviewFreezeValidateApprovalTransitionV1 要求 Review Frozen 到 Approved 只改变状态绑定和审批引用，冻结核心保持不变。
func reviewFreezeValidateApprovalTransitionV1(base, head reviewFreezeGateV1) error {
	if base.Freeze == nil || head.Freeze == nil || head.ReopenException != nil {
		return fmt.Errorf("%s Review Frozen -> Approved 的 freeze/CFE 形状非法", base.Gate)
	}
	if !reviewFreezeEqualFreezeCoreV1(base.Freeze, head.Freeze) {
		return fmt.Errorf("%s Review Frozen -> Approved 不得替换 freeze core", base.Gate)
	}
	if !reflect.DeepEqual(base.CandidateEvidence, head.CandidateEvidence) {
		return fmt.Errorf("%s Review Frozen -> Approved 不得替换 candidate evidence", base.Gate)
	}
	if reflect.DeepEqual(base.Freeze.OwnerApprovalRef, head.Freeze.OwnerApprovalRef) {
		return fmt.Errorf("%s Approved 必须使用绑定 approved status 的新 approval ref", base.Gate)
	}
	return nil
}

// reviewFreezeValidateReopenTransitionV1 要求重开保留原 Freeze 核心，并用一个新 CFE 精确指向 parent Freeze。
func reviewFreezeValidateReopenTransitionV1(base, head reviewFreezeGateV1) error {
	if base.Freeze == nil || head.Freeze == nil || head.ReopenException == nil {
		return fmt.Errorf("%s reopen 缺 parent freeze 或 CFE", base.Gate)
	}
	if !reviewFreezeEqualFreezeCoreV1(base.Freeze, head.Freeze) {
		return fmt.Errorf("%s reopen 不得替换 parent freeze core", base.Gate)
	}
	if !reflect.DeepEqual(base.CandidateEvidence, head.CandidateEvidence) {
		return fmt.Errorf("%s reopen 不得替换 candidate evidence", base.Gate)
	}
	if head.ReopenException.ParentFreezeID != base.Freeze.FreezeID {
		return fmt.Errorf("%s CFE parent=%q want=%q", base.Gate, head.ReopenException.ParentFreezeID, base.Freeze.FreezeID)
	}
	if base.ReopenException != nil && reflect.DeepEqual(base.ReopenException, head.ReopenException) {
		return fmt.Errorf("%s 再次 reopen 必须使用新的 CFE", base.Gate)
	}
	if reflect.DeepEqual(base.Freeze.OwnerApprovalRef, head.Freeze.OwnerApprovalRef) {
		return fmt.Errorf("%s reopened 必须使用绑定 CFE 的新 approval ref", base.Gate)
	}
	return nil
}

// reviewFreezeValidateReapprovalTransitionV1 要求 reopened 经同一 CFE 形成严格递增一个版本的新 Freeze。
func reviewFreezeValidateReapprovalTransitionV1(base, head reviewFreezeGateV1) error {
	if base.Freeze == nil || head.Freeze == nil || base.ReopenException == nil || head.ReopenException == nil {
		return fmt.Errorf("%s reapproval 缺 parent/new freeze 或 CFE", base.Gate)
	}
	if !reflect.DeepEqual(base.ReopenException, head.ReopenException) {
		return fmt.Errorf("%s reapproval 必须沿用 reopened 状态的同一 CFE", base.Gate)
	}
	if !reflect.DeepEqual(base.CandidateEvidence, head.CandidateEvidence) {
		return fmt.Errorf("%s reapproval 不得替换 candidate evidence", base.Gate)
	}
	baseVersion, err := reviewFreezeParseVersionV1(base.Gate, base.Freeze.FreezeID)
	if err != nil {
		return err
	}
	headVersion, err := reviewFreezeParseVersionV1(head.Gate, head.Freeze.FreezeID)
	if err != nil {
		return err
	}
	if headVersion != baseVersion+1 || head.Freeze.SupersedesFreezeID != base.Freeze.FreezeID {
		return fmt.Errorf("%s reapproval 版本必须 v%d -> v%d 且 supersedes parent", base.Gate, baseVersion, baseVersion+1)
	}
	baseFrozenAt, err := time.Parse(time.RFC3339, base.Freeze.FrozenAt)
	if err != nil {
		return fmt.Errorf("%s base frozen_at 非法: %w", base.Gate, err)
	}
	headFrozenAt, err := time.Parse(time.RFC3339, head.Freeze.FrozenAt)
	if err != nil || !headFrozenAt.After(baseFrozenAt) {
		return fmt.Errorf("%s reapproval frozen_at 必须晚于 parent", base.Gate)
	}
	if reflect.DeepEqual(base.Freeze.OwnerApprovalRef, head.Freeze.OwnerApprovalRef) {
		return fmt.Errorf("%s reapproval 必须使用新的 approval ref", base.Gate)
	}
	return nil
}

// reviewFreezeRequireFormalGateUnchangedV1 禁止同一正式状态下静默替换任何 Freeze、CFE、候选或审批引用。
func reviewFreezeRequireFormalGateUnchangedV1(base, head reviewFreezeGateV1) error {
	if !reflect.DeepEqual(base, head) {
		return fmt.Errorf("%s formal same-state 必须保持语义不可变", base.Gate)
	}
	return nil
}

// reviewFreezeEqualFreezeCoreV1 比较不含状态专属 OwnerApprovalRef 的不可变 Freeze 核心。
func reviewFreezeEqualFreezeCoreV1(left, right *reviewFreezeRecordV1) bool {
	if left == nil || right == nil {
		return left == right
	}
	leftCopy := *left
	rightCopy := *right
	leftCopy.OwnerApprovalRef = reviewFreezeOwnerApprovalRefV1{}
	rightCopy.OwnerApprovalRef = reviewFreezeOwnerApprovalRefV1{}
	return reflect.DeepEqual(leftCopy, rightCopy)
}

// reviewFreezeParseVersionV1 从 Gate 绑定的 FreezeID 提取正整数版本。
func reviewFreezeParseVersionV1(gate, freezeID string) (int, error) {
	pattern := regexp.MustCompile(`^CF-` + regexp.QuoteMeta(gate) + `-v([1-9][0-9]*)$`)
	matched := pattern.FindStringSubmatch(freezeID)
	if len(matched) != 2 {
		return 0, fmt.Errorf("%s freeze_id 非法=%q", gate, freezeID)
	}
	version, err := strconv.Atoi(matched[1])
	if err != nil {
		return 0, fmt.Errorf("%s freeze version 非法: %w", gate, err)
	}
	return version, nil
}

// TestW2ReviewFreezeTransitionPolicyV1StateMatrix 覆盖 pre-formal、正式冻结、重开和重批的允许与拒绝迁移。
func TestW2ReviewFreezeTransitionPolicyV1StateMatrix(t *testing.T) {
	tests := []struct {
		name    string
		base    reviewFreezeGateV1
		head    reviewFreezeGateV1
		wantErr bool
	}{
		{name: "expansion stays", base: reviewFreezePolicyPreFormalGateV1("expansion_frozen", false), head: reviewFreezePolicyPreFormalGateV1("expansion_frozen", false)},
		{name: "expansion to awaiting", base: reviewFreezePolicyPreFormalGateV1("expansion_frozen", false), head: reviewFreezePolicyPreFormalGateV1("awaiting_review", true)},
		{name: "awaiting back to expansion", base: reviewFreezePolicyPreFormalGateV1("awaiting_review", true), head: reviewFreezePolicyPreFormalGateV1("expansion_frozen", false)},
		{name: "awaiting to review frozen", base: reviewFreezePolicyPreFormalGateV1("awaiting_review", true), head: reviewFreezePolicyFormalGateV1("review_frozen", 1, "", false)},
		{name: "expansion skips review", base: reviewFreezePolicyPreFormalGateV1("expansion_frozen", false), head: reviewFreezePolicyFormalGateV1("review_frozen", 1, "", false), wantErr: true},
		{name: "awaiting skips to approved", base: reviewFreezePolicyPreFormalGateV1("awaiting_review", true), head: reviewFreezePolicyFormalGateV1("approved", 1, "", false), wantErr: true},
		{name: "formal downgrade", base: reviewFreezePolicyFormalGateV1("approved", 1, "", false), head: reviewFreezePolicyPreFormalGateV1("awaiting_review", true), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := reviewFreezeValidateGateTransitionV1(tc.base, tc.head)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// TestW2ReviewFreezeTransitionPolicyV1FormalLineage 覆盖正式状态不可变、CFE parent 和连续版本规则。
func TestW2ReviewFreezeTransitionPolicyV1FormalLineage(t *testing.T) {
	reviewFrozen := reviewFreezePolicyFormalGateV1("review_frozen", 1, "", false)
	approved := reviewFreezePolicyFormalGateV1("approved", 1, "", false)
	reopened := reviewFreezePolicyFormalGateV1("reopened", 1, "", true)
	reapproved := reviewFreezePolicyFormalGateV1("approved", 2, "CF-W2-R00-v1", true)

	tests := []struct {
		name    string
		base    reviewFreezeGateV1
		head    reviewFreezeGateV1
		mutate  func(*reviewFreezeGateV1)
		wantErr bool
	}{
		{name: "review frozen unchanged", base: reviewFrozen, head: reviewFrozen},
		{name: "review frozen approved", base: reviewFrozen, head: approved},
		{name: "approved reopened", base: approved, head: reopened},
		{name: "reopened reapproved", base: reopened, head: reapproved},
		{name: "same approved core mutation", base: approved, head: approved, mutate: func(g *reviewFreezeGateV1) {
			g.Freeze.ContractManifestSHA256 = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}, wantErr: true},
		{name: "direct approved atomic reapproval", base: approved, head: reapproved, wantErr: true},
		{name: "reopen replaces parent core", base: approved, head: reopened, mutate: func(g *reviewFreezeGateV1) { g.Freeze.FrozenAt = "2026-07-16T00:00:00Z" }, wantErr: true},
		{name: "reapproval skips version", base: reopened, head: reapproved, mutate: func(g *reviewFreezeGateV1) { g.Freeze.FreezeID = "CF-W2-R00-v3" }, wantErr: true},
		{name: "reapproval swaps CFE", base: reopened, head: reapproved, mutate: func(g *reviewFreezeGateV1) { g.ReopenException.ExceptionID = "CFE-W2-R00-SECURITY-999" }, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			head := reviewFreezePolicyCloneGateV1(tc.head)
			if tc.mutate != nil {
				tc.mutate(&head)
			}
			err := reviewFreezeValidateGateTransitionV1(tc.base, head)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// reviewFreezePolicyPreFormalGateV1 构造纯迁移策略测试使用的 pre-formal Gate。
func reviewFreezePolicyPreFormalGateV1(status string, completeCandidate bool) reviewFreezeGateV1 {
	gate := reviewFreezeGateV1{Gate: "W2-R00", Status: status}
	if completeCandidate {
		gate.CandidateEvidence = []reviewFreezeCandidateEvidenceV1{reviewFreezePolicyCandidateV1()}
	}
	return gate
}

// reviewFreezePolicyFormalGateV1 构造纯迁移策略测试使用的正式 Gate，字段只用于 lineage 比较而不替代 shape validator。
func reviewFreezePolicyFormalGateV1(status string, version int, supersedes string, withCFE bool) reviewFreezeGateV1 {
	gate := reviewFreezeGateV1{
		Gate: "W2-R00", Status: status,
		CandidateEvidence: []reviewFreezeCandidateEvidenceV1{reviewFreezePolicyCandidateV1()},
		Freeze: &reviewFreezeRecordV1{
			FreezeID: fmt.Sprintf("CF-W2-R00-v%d", version), SupersedesFreezeID: supersedes,
			ContractManifestPath:   "agent/tests/contract/testdata/synthetic/manifest.json",
			ContractManifestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			VectorIDs:              []string{"V-001"}, TargetTests: []string{"TestSynthetic"},
			FrozenAt: fmt.Sprintf("2026-07-%02dT00:00:00Z", 14+version),
			OwnerApprovalRef: reviewFreezeOwnerApprovalRefV1{
				Path:                        fmt.Sprintf("docs/design/agent/approvals/w2-review-freeze-owner-approvals/w2-r00-v%d-%s.json", version, status),
				OwnerApprovalManifestSHA256: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				ApprovalSummarySHA256:       "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			},
		},
	}
	if withCFE {
		gate.ReopenException = &reviewFreezeExceptionRefV1{
			Path:                    "docs/design/agent/approvals/w2-review-freeze-exceptions/cfe-w2-r00-security-001.json",
			ExceptionManifestSHA256: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			ExceptionID:             "CFE-W2-R00-SECURITY-001", ParentFreezeID: "CF-W2-R00-v1",
		}
	}
	return gate
}

// reviewFreezePolicyCandidateV1 返回策略测试使用的稳定完整候选。
func reviewFreezePolicyCandidateV1() reviewFreezeCandidateEvidenceV1 {
	return reviewFreezeCandidateEvidenceV1{
		Scope: "synthetic", Coverage: "gate_candidate",
		ContractManifestPath:   "agent/tests/contract/testdata/synthetic/manifest.json",
		ContractManifestSHA256: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		VectorIDs:              []string{"V-001"}, TargetTests: []string{"TestSynthetic"},
	}
}

// reviewFreezePolicyCloneGateV1 深拷贝策略测试 Gate，避免负向用例共享 Slice/Pointer。
func reviewFreezePolicyCloneGateV1(source reviewFreezeGateV1) reviewFreezeGateV1 {
	clone := source
	clone.CandidateEvidence = append([]reviewFreezeCandidateEvidenceV1(nil), source.CandidateEvidence...)
	if source.Freeze != nil {
		freeze := *source.Freeze
		freeze.VectorIDs = append([]string(nil), source.Freeze.VectorIDs...)
		freeze.TargetTests = append([]string(nil), source.Freeze.TargetTests...)
		clone.Freeze = &freeze
	}
	if source.ReopenException != nil {
		reopen := *source.ReopenException
		clone.ReopenException = &reopen
	}
	return clone
}
