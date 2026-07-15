package reviewfreeze_test

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	reviewFreezeManifestPathV1          = "docs/design/agent/approvals/w2-review-freeze-manifest.json"
	reviewFreezeSchemaV1                = "w2_review_freeze_manifest.v1"
	reviewFreezeOwnerApprovalSchemaV1   = "w2_review_freeze_owner_approval.v1"
	reviewFreezeExceptionSchemaV1       = "w2_corpus_freeze_exception.v1"
	reviewFreezeApprovalSummarySchemaV2 = "w2_review_freeze_approval_summary.v2"
)

// reviewFreezeManifestV1 描述由 Integration Owner 串行维护的 W2 契约评审冻结总表。
type reviewFreezeManifestV1 struct {
	// SchemaVersion 固定治理清单的严格 JSON 版本。
	SchemaVersion string `json:"schema_version"`
	// GovernanceOwnerRole 固定唯一有权写入治理状态的角色。
	GovernanceOwnerRole string `json:"governance_owner_role"`
	// ImplementationOwnerRoles 列出不得为自身实现写入批准事实的角色。
	ImplementationOwnerRoles []string `json:"implementation_owner_roles"`
	// Gates 按 W2-R00 到 W2-R08 顺序固定全部契约门禁。
	Gates []reviewFreezeGateV1 `json:"gates"`
}

// reviewFreezeGateV1 描述单个 Gate 的候选证据、正式冻结和受控重开状态。
type reviewFreezeGateV1 struct {
	// Gate 是 W2-R00 到 W2-R08 的稳定标识。
	Gate string `json:"gate"`
	// Status 区分扩展冻结、待评审、正式冻结、批准与重开。
	Status string `json:"status"`
	// RequiredOwnerRoles 声明正式冻结和 CFE 必须签字的受影响 Owner exact-set；进入正式 authority 后不可变。
	RequiredOwnerRoles []string `json:"required_owner_roles"`
	// CandidateEvidence 只记录可复核候选，不产生批准或实现授权。
	CandidateEvidence []reviewFreezeCandidateEvidenceV1 `json:"candidate_evidence"`
	// Freeze 只在正式状态下记录不可变冻结事实。
	Freeze *reviewFreezeRecordV1 `json:"freeze"`
	// ReopenException 在受控重开或重开后重新批准时绑定 CFE。
	ReopenException *reviewFreezeExceptionRefV1 `json:"reopen_exception"`
	// Blockers 明确当前状态仍未关闭的阻断原因。
	Blockers []reviewFreezeBlockerV1 `json:"blockers"`
}

// reviewFreezeCandidateEvidenceV1 描述尚未产生审批效力的可执行候选 Corpus。
type reviewFreezeCandidateEvidenceV1 struct {
	// Scope 说明候选证据覆盖的契约子域。
	Scope string `json:"scope"`
	// Coverage 区分完整 Gate 候选与局部候选。
	Coverage string `json:"coverage"`
	// ContractManifestPath 指向仓库内既有 Corpus manifest。
	ContractManifestPath string `json:"contract_manifest_path"`
	// ContractManifestSHA256 固定 manifest 原始字节摘要。
	ContractManifestSHA256 string `json:"contract_manifest_sha256"`
	// VectorIDs 固定排序后的向量 exact-set。
	VectorIDs []string `json:"vector_ids"`
	// TargetTests 固定排序后的目标测试 exact-set。
	TargetTests []string `json:"target_tests"`
}

// reviewFreezeBlockerV1 描述未关闭门禁的稳定机器码和中文原因。
type reviewFreezeBlockerV1 struct {
	// Code 是稳定且唯一的阻断码。
	Code string `json:"code"`
	// Statement 说明缺失事实及其禁止推进的边界。
	Statement string `json:"statement"`
}

// reviewFreezeRecordV1 描述完成 Owner 复核后不可变的正式冻结事实。
type reviewFreezeRecordV1 struct {
	// FreezeID 是 CF-W2-Rnn-vn 格式的稳定冻结标识。
	FreezeID string `json:"freeze_id"`
	// SupersedesFreezeID 在 CFE 重新批准时指向旧冻结标识，首次冻结为空。
	SupersedesFreezeID string `json:"supersedes_freeze_id"`
	// ContractManifestPath 指向该 Gate 的 canonical contract manifest。
	ContractManifestPath string `json:"contract_manifest_path"`
	// ContractManifestSHA256 固定 canonical manifest 原始字节摘要。
	ContractManifestSHA256 string `json:"contract_manifest_sha256"`
	// VectorIDs 固定排序后的正式向量 exact-set。
	VectorIDs []string `json:"vector_ids"`
	// TargetTests 固定排序后的正式目标测试 exact-set。
	TargetTests []string `json:"target_tests"`
	// FrozenAt 是 UTC RFC3339 冻结时间。
	FrozenAt string `json:"frozen_at"`
	// OwnerApprovalRef 绑定受影响 Owner 的不可变联合审批清单。
	OwnerApprovalRef reviewFreezeOwnerApprovalRefV1 `json:"owner_approval_ref"`
}

// reviewFreezeOwnerApprovalRefV1 绑定外部审批清单及其冻结摘要。
type reviewFreezeOwnerApprovalRefV1 struct {
	// Path 指向 Integration Owner 维护的审批清单。
	Path string `json:"path"`
	// OwnerApprovalManifestSHA256 固定审批清单原始字节摘要。
	OwnerApprovalManifestSHA256 string `json:"owner_approval_manifest_sha256"`
	// ApprovalSummarySHA256 固定 Gate、Freeze、Corpus 和 CFE 的规范化摘要。
	ApprovalSummarySHA256 string `json:"approval_summary_sha256"`
}

// reviewFreezeExceptionRefV1 绑定允许重开单个 Gate 的 Corpus Freeze Exception。
type reviewFreezeExceptionRefV1 struct {
	// Path 指向 Integration Owner 维护的 CFE 清单。
	Path string `json:"path"`
	// ExceptionManifestSHA256 固定 CFE 清单原始字节摘要。
	ExceptionManifestSHA256 string `json:"exception_manifest_sha256"`
	// ExceptionID 是稳定 CFE 标识。
	ExceptionID string `json:"exception_id"`
	// ParentFreezeID 指向被重新打开的冻结基线。
	ParentFreezeID string `json:"parent_freeze_id"`
}

// reviewFreezeOwnerApprovalManifestV1 描述受影响 Owner 对同一冻结摘要的联合签字事实。
type reviewFreezeOwnerApprovalManifestV1 struct {
	// SchemaVersion 固定审批清单版本。
	SchemaVersion string `json:"schema_version"`
	// ApprovalID 是不可变审批记录标识。
	ApprovalID string `json:"approval_id"`
	// Gate 绑定唯一 W2 Gate。
	Gate string `json:"gate"`
	// FreezeID 绑定唯一冻结基线。
	FreezeID string `json:"freeze_id"`
	// ContractManifestSHA256 复述被审批的 canonical manifest 摘要。
	ContractManifestSHA256 string `json:"contract_manifest_sha256"`
	// ApprovalSummarySHA256 复述规范化审批摘要并与治理清单比对。
	ApprovalSummarySHA256 string `json:"approval_summary_sha256"`
	// ReapprovalExceptionID 在 CFE 后重新批准时绑定该 CFE，首次批准为空。
	ReapprovalExceptionID string `json:"reapproval_exception_id"`
	// RecordedByRole 必须是 Integration Owner，实施角色不得写入自身批准事实。
	RecordedByRole string `json:"recorded_by_role"`
	// OwnerApprovals 固定受影响 Owner 的外部评审引用。
	OwnerApprovals []reviewFreezeOwnerSignatureV1 `json:"owner_approvals"`
}

// reviewFreezeOwnerSignatureV1 描述单个 Owner 的外部不可变评审引用。
type reviewFreezeOwnerSignatureV1 struct {
	// OwnerRole 是被代表的领域 Owner。
	OwnerRole string `json:"owner_role"`
	// ApproverRole 是实际给出结论的审核角色，不能是实施角色。
	ApproverRole string `json:"approver_role"`
	// ReviewURL 指向不可变评审记录。
	ReviewURL string `json:"review_url"`
	// CommitSHA 固定评审所针对的仓库提交。
	CommitSHA string `json:"commit_sha"`
	// ApprovedAt 是 UTC RFC3339 审批时间。
	ApprovedAt string `json:"approved_at"`
}

// reviewFreezeExceptionManifestV1 描述 Review Freeze 后受控补充最小回归的 CFE。
type reviewFreezeExceptionManifestV1 struct {
	// SchemaVersion 固定 CFE 严格 JSON 版本。
	SchemaVersion string `json:"schema_version"`
	// ExceptionID 是稳定 CFE 标识。
	ExceptionID string `json:"exception_id"`
	// ParentFreezeID 绑定被重新打开的冻结基线。
	ParentFreezeID string `json:"parent_freeze_id"`
	// ImpactedADROrGate 固定最小受影响 ADR/Gate exact-set。
	ImpactedADROrGate []string `json:"impacted_adr_or_gate"`
	// Blocker 说明符合白名单的安全、一致性、迁移或真实回归原因。
	Blocker string `json:"blocker"`
	// AllowedFiles 固定本次例外允许修改的仓库文件 exact-set。
	AllowedFiles []string `json:"allowed_files"`
	// VectorIDs 固定本次允许新增或替换的向量 exact-set。
	VectorIDs []string `json:"vector_ids"`
	// Compatibility 说明 Schema、Digest 或已发布 Receipt 兼容性。
	Compatibility string `json:"compatibility"`
	// ProductionConsumer 指向将消费该回归的生产组件。
	ProductionConsumer string `json:"production_consumer"`
	// RecordedByRole 必须是 Integration Owner。
	RecordedByRole string `json:"recorded_by_role"`
	// OwnerApprovals 固定受影响 Owner 的重新批准引用。
	OwnerApprovals []reviewFreezeOwnerSignatureV1 `json:"owner_approvals"`
	// Manifest 固定例外更新后的 manifest、向量和目标测试集合。
	Manifest reviewFreezeExceptionBaselineV1 `json:"manifest"`
}

// reviewFreezeExceptionBaselineV1 描述 CFE 允许形成的新候选基线。
type reviewFreezeExceptionBaselineV1 struct {
	// ContractManifestSHA256 固定更新后 canonical manifest 摘要。
	ContractManifestSHA256 string `json:"contract_manifest_sha256"`
	// VectorIDs 固定更新后完整向量 exact-set。
	VectorIDs []string `json:"vector_ids"`
	// TargetTests 固定更新后完整目标测试 exact-set。
	TargetTests []string `json:"target_tests"`
}

// reviewFreezeCorpusManifestV1 提取既有 Corpus manifest 的可冻结字段。
type reviewFreezeCorpusManifestV1 struct {
	// SchemaVersion 标识被引用 Corpus manifest 的版本。
	SchemaVersion string `json:"schema_version"`
	// Files 固定 Corpus 文件、原始字节摘要和各文件向量数。
	Files []reviewFreezeCorpusFileV1 `json:"files"`
	// FixtureIDs 固定 Corpus 使用的初始状态夹具 exact-set。
	FixtureIDs []string `json:"fixture_ids"`
	// ValidatorSources 必填绑定实际解释 Corpus 的 Go 契约测试源文件及原始字节摘要。
	ValidatorSources []reviewFreezeValidatorSourceV1 `json:"validator_sources"`
	// ValidatorBuildSources 必填绑定 Validator 所属 Go Module 的 go.mod/go.sum 原始字节摘要。
	ValidatorBuildSources []reviewFreezeValidatorSourceV1 `json:"validator_build_sources"`
	// ValidatorBuildClosure 可选承载独立 Validator entrypoint 的完整构建闭包候选；旧 v1 manifest 缺失时保持兼容，但不获得 formal Freeze 效力。
	ValidatorBuildClosure *reviewFreezeValidatorBuildClosureV1 `json:"validator_build_closure,omitempty"`
	// DesignSources 可选绑定产生该 Corpus 的设计源文件及原始字节摘要。
	DesignSources []reviewFreezeDesignSourceV1 `json:"design_sources,omitempty"`
	// VectorIDs 是现有 manifest 声明的向量集合。
	VectorIDs []string `json:"vector_ids"`
	// TotalVectorCount 是现有 manifest 声明的向量总数。
	TotalVectorCount int `json:"total_vector_count"`
	// TargetTests 是现有 manifest 声明的目标测试集合。
	TargetTests []string `json:"target_tests"`
}

// reviewFreezeCorpusFileV1 描述 contract manifest 直接拥有的单个 Corpus 文件。
type reviewFreezeCorpusFileV1 struct {
	// File 是相对 contract manifest 所在目录的安全文件路径。
	File string `json:"file"`
	// SHA256 固定文件原始字节摘要。
	SHA256 string `json:"sha256"`
	// VectorCount 是该文件实际承载的向量数。
	VectorCount int `json:"vector_count"`
}

// reviewFreezeValidatorSourceV1 描述解释 Corpus 的仓库 Go 契约测试源文件。
type reviewFreezeValidatorSourceV1 struct {
	// Path 是仓库相对契约测试 Go 源文件路径。
	Path string `json:"path"`
	// SHA256 固定 Validator 源文件原始字节摘要。
	SHA256 string `json:"sha256"`
}

// reviewFreezeDesignSourceV1 描述可选的仓库设计源绑定。
type reviewFreezeDesignSourceV1 struct {
	// Path 是仓库相对设计文件路径。
	Path string `json:"path"`
	// SHA256 固定设计文件原始字节摘要。
	SHA256 string `json:"sha256"`
}

// reviewFreezeArtifactLoaderV1 以仓库相对路径读取不可变治理输入。
type reviewFreezeArtifactLoaderV1 func(relative string) ([]byte, error)

// TestW2ReviewFreezeManifestV1CurrentBaseline 验证当前 R00-R08 exact-set、候选摘要和未批准边界。
func TestW2ReviewFreezeManifestV1CurrentBaseline(t *testing.T) {
	manifest, raw := reviewFreezeLoadCurrentV1(t)
	if err := reviewFreezeValidateManifestV1(manifest, reviewFreezeRepositoryLoaderV1(reviewFreezeRepoRootV1(t))); err != nil {
		t.Fatalf("Review Freeze manifest 失败关闭: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("Review Freeze manifest 不能为空")
	}
	// 状态值只由 Integration Owner 的清单维护；测试只校验状态对应的证据是否完整，不复制可漂移的批准结论。
}

// TestW2ReviewFreezeManifestV1StrictJSON 验证未知字段、重复键和尾随值全部失败关闭。
func TestW2ReviewFreezeManifestV1StrictJSON(t *testing.T) {
	_, raw := reviewFreezeLoadCurrentV1(t)
	cases := map[string][]byte{
		"unknown field": []byte(strings.Replace(string(raw), "{", `{"future":true,`, 1)),
		"duplicate field": []byte(strings.Replace(
			string(raw),
			`"schema_version": "w2_review_freeze_manifest.v1",`,
			`"schema_version": "w2_review_freeze_manifest.v1", "schema_version": "duplicate",`,
			1,
		)),
		"trailing value": append(append([]byte(nil), raw...), []byte(`{}`)...),
	}
	for name, candidate := range cases {
		t.Run(name, func(t *testing.T) {
			var manifest reviewFreezeManifestV1
			if err := messageSetStrictDecodeV1(candidate, &manifest); err == nil {
				t.Fatalf("strict decoder accepted %s", name)
			}
		})
	}
}

// TestW2ReviewFreezeManifestV1FormalStates 验证正式冻结、批准、重开和重新批准的合法路径。
func TestW2ReviewFreezeManifestV1FormalStates(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		reapproved bool
	}{
		{name: "review frozen", status: "review_frozen"},
		{name: "initial approved", status: "approved"},
		{name: "reopened with CFE", status: "reopened"},
		{name: "reapproved after CFE", status: "approved", reapproved: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifest, overlay := reviewFreezeSyntheticFormalV1(t, tc.status, tc.reapproved)
			loader := reviewFreezeOverlayLoaderV1(overlay, reviewFreezeRepositoryLoaderV1(reviewFreezeRepoRootV1(t)))
			if err := reviewFreezeValidateManifestV1(manifest, loader); err != nil {
				t.Fatalf("valid formal transition rejected: %v", err)
			}
		})
	}
}

// TestW2ReviewFreezeManifestV1ReopenReapprovedFreeze 验证已经由 CFE 重批的高版本 Freeze 仍可保留完整谱系后再次受控重开。
func TestW2ReviewFreezeManifestV1ReopenReapprovedFreeze(t *testing.T) {
	manifest, overlay := reviewFreezeSyntheticFormalV1(t, "approved", true)
	gate := &manifest.Gates[0]
	gate.Status = "reopened"
	gate.Blockers = []reviewFreezeBlockerV1{{Code: "W2_R00_CFE_OPEN", Statement: "第二个 CFE 尚未重新冻结和批准，生产实现继续阻断。"}}

	oldException := reviewFreezeDecodeExceptionV1(t, overlay[gate.ReopenException.Path])
	newException := oldException
	newException.ExceptionID = "CFE-W2-R00-SECURITY-002"
	newException.ParentFreezeID = gate.Freeze.FreezeID
	newException.Blocker = "重批后的 v2 Freeze 发现新的 Unknown Outcome 回归。"
	newException.VectorIDs = []string{"V-003"}
	newException.Manifest.ContractManifestSHA256 = reviewFreezeSHA256V1([]byte("synthetic-v3-contract"))
	newException.Manifest.VectorIDs = []string{"V-001", "V-002", "V-003"}
	newExceptionRaw := reviewFreezeMarshalV1(t, newException)
	newExceptionPath := "docs/design/agent/approvals/w2-review-freeze-exceptions/cfe-w2-r00-security-002.json"
	overlay[newExceptionPath] = newExceptionRaw
	gate.ReopenException = &reviewFreezeExceptionRefV1{
		Path: newExceptionPath, ExceptionManifestSHA256: reviewFreezeSHA256V1(newExceptionRaw),
		ExceptionID: newException.ExceptionID, ParentFreezeID: gate.Freeze.FreezeID,
	}

	approval := reviewFreezeDecodeOwnerApprovalV1(t, overlay[gate.Freeze.OwnerApprovalRef.Path])
	approval.ApprovalID = "APR-W2-R00-REOPEN-002"
	approval.ApprovalSummarySHA256 = reviewFreezeApprovalSummaryV1(*gate)
	reviewFreezeReplaceOwnerApprovalV1(t, gate, approval, overlay)
	gate.Freeze.OwnerApprovalRef.ApprovalSummarySHA256 = approval.ApprovalSummarySHA256

	loader := reviewFreezeOverlayLoaderV1(overlay, reviewFreezeRepositoryLoaderV1(reviewFreezeRepoRootV1(t)))
	if err := reviewFreezeValidateManifestV1(manifest, loader); err != nil {
		t.Fatalf("reopen reapproved freeze rejected: %v", err)
	}
}

// TestW2ReviewFreezeManifestV1FailClosedGuards 验证缺基线、自批、摘要漂移、无 CFE 重开与路径越界均被拒绝。
func TestW2ReviewFreezeManifestV1FailClosedGuards(t *testing.T) {
	tests := []struct {
		name string
		make func(*testing.T) (reviewFreezeManifestV1, map[string][]byte)
		want string
	}{
		{
			name: "gate exact set missing",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, _ := reviewFreezeLoadCurrentV1(t)
				manifest.Gates = manifest.Gates[:8]
				return manifest, nil
			},
			want: "gate exact-set",
		},
		{
			name: "required owner roles invalid exact set",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, _ := reviewFreezeLoadCurrentV1(t)
				manifest.Gates[0].RequiredOwnerRoles = []string{"security_owner", "agent_owner"}
				return manifest, nil
			},
			want: "required_owner_roles",
		},
		{
			name: "blocker belongs to another gate",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, _ := reviewFreezeLoadCurrentV1(t)
				manifest.Gates[0].Blockers[0].Code = "W2_R01_BILLING_REVIEW_PENDING"
				return manifest, nil
			},
			want: "不属于当前 Gate",
		},
		{
			name: "unknown status",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, _ := reviewFreezeLoadCurrentV1(t)
				manifest.Gates[0].Status = "implemented"
				return manifest, nil
			},
			want: "非法 status",
		},
		{
			name: "formal status missing freeze",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, _ := reviewFreezeLoadCurrentV1(t)
				manifest.Gates[0].Status = "review_frozen"
				manifest.Gates[0].Blockers = nil
				return manifest, nil
			},
			want: "必须提供 freeze",
		},
		{
			name: "implementation owner self approval",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, overlay := reviewFreezeSyntheticFormalV1(t, "approved", false)
				gate := &manifest.Gates[0]
				approval := reviewFreezeDecodeOwnerApprovalV1(t, overlay[gate.Freeze.OwnerApprovalRef.Path])
				approval.RecordedByRole = manifest.ImplementationOwnerRoles[0]
				reviewFreezeReplaceOwnerApprovalV1(t, gate, approval, overlay)
				return manifest, overlay
			},
			want: "实施角色不得写入批准",
		},
		{
			name: "implementation owner signs own approval",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, overlay := reviewFreezeSyntheticFormalV1(t, "approved", false)
				gate := &manifest.Gates[0]
				approval := reviewFreezeDecodeOwnerApprovalV1(t, overlay[gate.Freeze.OwnerApprovalRef.Path])
				approval.OwnerApprovals[0].ApproverRole = manifest.ImplementationOwnerRoles[0]
				reviewFreezeReplaceOwnerApprovalV1(t, gate, approval, overlay)
				return manifest, overlay
			},
			want: "不得自批",
		},
		{
			name: "approval summary mismatch",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, overlay := reviewFreezeSyntheticFormalV1(t, "approved", false)
				gate := &manifest.Gates[0]
				approval := reviewFreezeDecodeOwnerApprovalV1(t, overlay[gate.Freeze.OwnerApprovalRef.Path])
				approval.ApprovalSummarySHA256 = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				reviewFreezeReplaceOwnerApprovalV1(t, gate, approval, overlay)
				return manifest, overlay
			},
			want: "approval summary 不一致",
		},
		{
			name: "status flip needs a new approval summary",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, overlay := reviewFreezeSyntheticFormalV1(t, "review_frozen", false)
				manifest.Gates[0].Status = "approved"
				return manifest, overlay
			},
			want: "approval summary 不一致",
		},
		{
			name: "owner approval missing required owner",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, overlay := reviewFreezeSyntheticFormalV1(t, "approved", false)
				gate := &manifest.Gates[0]
				approval := reviewFreezeDecodeOwnerApprovalV1(t, overlay[gate.Freeze.OwnerApprovalRef.Path])
				approval.OwnerApprovals = approval.OwnerApprovals[:len(approval.OwnerApprovals)-1]
				reviewFreezeReplaceOwnerApprovalV1(t, gate, approval, overlay)
				return manifest, overlay
			},
			want: "owner approval roles exact-set",
		},
		{
			name: "owner approval adds unrequired owner",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, overlay := reviewFreezeSyntheticFormalV1(t, "approved", false)
				gate := &manifest.Gates[0]
				approval := reviewFreezeDecodeOwnerApprovalV1(t, overlay[gate.Freeze.OwnerApprovalRef.Path])
				approval.OwnerApprovals = append(approval.OwnerApprovals, reviewFreezeOwnerSignatureV1{
					OwnerRole: "zz_unrequired_owner", ApproverRole: "zz_unrequired_reviewer", ReviewURL: "https://review.example/r00/zz",
					CommitSHA: "4444444444444444444444444444444444444444", ApprovedAt: "2026-07-15T00:00:00Z",
				})
				reviewFreezeReplaceOwnerApprovalV1(t, gate, approval, overlay)
				return manifest, overlay
			},
			want: "owner approval roles exact-set",
		},
		{
			name: "reopened without CFE",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, overlay := reviewFreezeSyntheticFormalV1(t, "reopened", false)
				manifest.Gates[0].ReopenException = nil
				return manifest, overlay
			},
			want: "必须绑定 CFE",
		},
		{
			name: "reapproved without reapproval binding",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, overlay := reviewFreezeSyntheticFormalV1(t, "approved", true)
				gate := &manifest.Gates[0]
				approval := reviewFreezeDecodeOwnerApprovalV1(t, overlay[gate.Freeze.OwnerApprovalRef.Path])
				approval.ReapprovalExceptionID = ""
				reviewFreezeReplaceOwnerApprovalV1(t, gate, approval, overlay)
				return manifest, overlay
			},
			want: "未绑定重新审批 CFE",
		},
		{
			name: "CFE cannot reopen another gate",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, overlay := reviewFreezeSyntheticFormalV1(t, "reopened", false)
				gate := &manifest.Gates[0]
				exception := reviewFreezeDecodeExceptionV1(t, overlay[gate.ReopenException.Path])
				exception.ImpactedADROrGate = []string{"W2-R00", "W2-R01"}
				reviewFreezeReplaceExceptionV1(t, gate, exception, overlay)
				return manifest, overlay
			},
			want: "额外 Gate",
		},
		{
			name: "reopened approval summary binds exact CFE",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, overlay := reviewFreezeSyntheticFormalV1(t, "reopened", false)
				gate := &manifest.Gates[0]
				exception := reviewFreezeDecodeExceptionV1(t, overlay[gate.ReopenException.Path])
				exception.Blocker = "同一 CFE 引用的阻断事实被替换。"
				reviewFreezeReplaceExceptionV1(t, gate, exception, overlay)
				return manifest, overlay
			},
			want: "approval summary 不一致",
		},
		{
			name: "candidate path escapes repository",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, _ := reviewFreezeLoadCurrentV1(t)
				manifest.Gates[1].CandidateEvidence[0].ContractManifestPath = "../outside.json"
				return manifest, nil
			},
			want: "不安全路径",
		},
		{
			name: "candidate corpus file digest drift",
			make: func(t *testing.T) (reviewFreezeManifestV1, map[string][]byte) {
				manifest, _ := reviewFreezeLoadCurrentV1(t)
				return manifest, map[string][]byte{
					"agent/tests/contract/testdata/w2_r01/graph_tool_result_v1.json": []byte(`{"tampered":true}`),
				}
			},
			want: "sha256=",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest, overlay := tc.make(t)
			loader := reviewFreezeOverlayLoaderV1(overlay, reviewFreezeRepositoryLoaderV1(reviewFreezeRepoRootV1(t)))
			err := reviewFreezeValidateManifestV1(manifest, loader)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want substring=%q", err, tc.want)
			}
		})
	}
}

// TestW2ReviewFreezeManifestV1ValidatorSources 覆盖 Validator 源必填、路径白名单、排序和逐文件摘要的失败关闭语义。
func TestW2ReviewFreezeManifestV1ValidatorSources(t *testing.T) {
	firstPath := "agent/tests/contract/a_validator_test.go"
	secondPath := "agent/tests/contract/b_validator_test.go"
	firstRaw := []byte("package contract_test\n")
	secondRaw := []byte("package contract_test\n\nfunc TestB() {}\n")
	values := map[string][]byte{firstPath: firstRaw, secondPath: secondRaw}
	loader := func(path string) ([]byte, error) {
		raw, ok := values[path]
		if !ok {
			return nil, os.ErrNotExist
		}
		return raw, nil
	}
	valid := []reviewFreezeValidatorSourceV1{
		{Path: firstPath, SHA256: reviewFreezeSHA256V1(firstRaw)},
		{Path: secondPath, SHA256: reviewFreezeSHA256V1(secondRaw)},
	}
	if err := reviewFreezeValidateValidatorSourcesV1(valid, loader); err != nil {
		t.Fatalf("valid validator sources rejected: %v", err)
	}

	tests := []struct {
		name    string
		sources []reviewFreezeValidatorSourceV1
		want    string
	}{
		{name: "missing", sources: nil, want: "不能为空"},
		{name: "outside contract tests", sources: []reviewFreezeValidatorSourceV1{{Path: "agent/internal/validator.go", SHA256: reviewFreezeSHA256V1(firstRaw)}}, want: "contract test Go"},
		{name: "not go", sources: []reviewFreezeValidatorSourceV1{{Path: "agent/tests/contract/validator.json", SHA256: reviewFreezeSHA256V1(firstRaw)}}, want: "contract test Go"},
		{name: "unsorted", sources: []reviewFreezeValidatorSourceV1{valid[1], valid[0]}, want: "未排序或重复"},
		{name: "duplicate", sources: []reviewFreezeValidatorSourceV1{valid[0], valid[0]}, want: "未排序或重复"},
		{name: "digest drift", sources: []reviewFreezeValidatorSourceV1{{Path: firstPath, SHA256: reviewFreezeSHA256V1(secondRaw)}}, want: "sha256="},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := reviewFreezeValidateValidatorSourcesV1(tc.sources, loader)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v want substring=%q", err, tc.want)
			}
		})
	}
}

// reviewFreezeValidateManifestV1 对治理状态、候选证据和正式审批执行统一失败关闭校验。
func reviewFreezeValidateManifestV1(manifest reviewFreezeManifestV1, loader reviewFreezeArtifactLoaderV1) error {
	if manifest.SchemaVersion != reviewFreezeSchemaV1 {
		return fmt.Errorf("schema_version=%q", manifest.SchemaVersion)
	}
	if manifest.GovernanceOwnerRole != "integration_owner" {
		return fmt.Errorf("governance_owner_role=%q", manifest.GovernanceOwnerRole)
	}
	wantImplementationOwners := []string{
		"agent_runtime_implementation_owner",
		"business_domain_implementation_owner",
		"frontend_smoke_implementation_owner",
		"worker_implementation_owner",
	}
	if !reflect.DeepEqual(manifest.ImplementationOwnerRoles, wantImplementationOwners) {
		return fmt.Errorf("implementation_owner_roles=%v", manifest.ImplementationOwnerRoles)
	}
	wantGates := []string{"W2-R00", "W2-R01", "W2-R02", "W2-R03", "W2-R04", "W2-R05", "W2-R06", "W2-R07", "W2-R08"}
	if len(manifest.Gates) != len(wantGates) {
		return fmt.Errorf("gate exact-set 长度=%d want=%d", len(manifest.Gates), len(wantGates))
	}
	for index := range manifest.Gates {
		gate := manifest.Gates[index]
		if gate.Gate != wantGates[index] {
			return fmt.Errorf("gate exact-set[%d]=%q want=%q", index, gate.Gate, wantGates[index])
		}
		if err := reviewFreezeValidateGateV1(manifest, gate, loader); err != nil {
			return fmt.Errorf("%s: %w", gate.Gate, err)
		}
	}
	return nil
}

// reviewFreezeValidateGateV1 强制每个状态只携带该阶段允许的字段和证据。
func reviewFreezeValidateGateV1(manifest reviewFreezeManifestV1, gate reviewFreezeGateV1, loader reviewFreezeArtifactLoaderV1) error {
	allowedStatuses := map[string]struct{}{
		"expansion_frozen": {}, "awaiting_review": {}, "review_frozen": {}, "approved": {}, "reopened": {},
	}
	if _, ok := allowedStatuses[gate.Status]; !ok {
		return fmt.Errorf("非法 status=%q", gate.Status)
	}
	if err := reviewFreezeValidateOwnerRoleSetV1(gate.RequiredOwnerRoles); err != nil {
		return err
	}
	if err := reviewFreezeValidateBlockersV1(gate.Gate, gate.Blockers); err != nil {
		return err
	}
	completeCandidateCount := 0
	seenScopes := make(map[string]struct{}, len(gate.CandidateEvidence))
	for _, candidate := range gate.CandidateEvidence {
		if _, exists := seenScopes[candidate.Scope]; exists || strings.TrimSpace(candidate.Scope) == "" {
			return fmt.Errorf("candidate scope 重复或为空=%q", candidate.Scope)
		}
		seenScopes[candidate.Scope] = struct{}{}
		if candidate.Coverage != "gate_candidate" && candidate.Coverage != "partial_candidate" {
			return fmt.Errorf("candidate coverage=%q", candidate.Coverage)
		}
		if candidate.Coverage == "gate_candidate" {
			completeCandidateCount++
		}
		if err := reviewFreezeValidateContractRefV1(candidate.ContractManifestPath, candidate.ContractManifestSHA256, candidate.VectorIDs, candidate.TargetTests, loader); err != nil {
			return fmt.Errorf("candidate %s: %w", candidate.Scope, err)
		}
	}
	if err := reviewFreezeValidateGateBuildClosurePolicyV1(gate, loader); err != nil {
		return err
	}

	switch gate.Status {
	case "expansion_frozen":
		if gate.Freeze != nil || gate.ReopenException != nil {
			return fmt.Errorf("expansion_frozen 不得携带 Freeze/CFE")
		}
		if completeCandidateCount != 0 {
			return fmt.Errorf("expansion_frozen 不得声明完整 gate candidate")
		}
		if len(gate.Blockers) == 0 {
			return fmt.Errorf("expansion_frozen 必须列出 blockers")
		}
		return nil
	case "awaiting_review":
		if gate.Freeze != nil || gate.ReopenException != nil {
			return fmt.Errorf("awaiting_review 不得携带正式 Freeze/CFE")
		}
		if completeCandidateCount != 1 || len(gate.Blockers) == 0 {
			return fmt.Errorf("awaiting_review 必须有且仅有一个完整候选并列出 blockers")
		}
		return nil
	case "review_frozen", "approved", "reopened":
		if gate.Freeze == nil {
			return fmt.Errorf("%s 必须提供 freeze", gate.Status)
		}
	default:
		return fmt.Errorf("不可达 status=%q", gate.Status)
	}

	if gate.Status == "reopened" {
		if gate.ReopenException == nil {
			return fmt.Errorf("reopened 必须绑定 CFE")
		}
		if len(gate.Blockers) == 0 {
			return fmt.Errorf("reopened 必须列出阻断实现的 blocker")
		}
	} else if len(gate.Blockers) != 0 {
		return fmt.Errorf("%s 不得保留 blocker", gate.Status)
	}

	exception, err := reviewFreezeValidateExceptionV1(manifest, gate, loader)
	if err != nil {
		return err
	}
	if err := reviewFreezeValidateFormalRecordV1(manifest, gate, exception, loader); err != nil {
		return err
	}
	return nil
}

// reviewFreezeValidateOwnerRoleSetV1 要求每个 Gate 在任何状态下都具有非空、排序、唯一的 Owner 角色集合。
func reviewFreezeValidateOwnerRoleSetV1(ownerRoles []string) error {
	if err := reviewFreezeValidateSortedExactSetV1(ownerRoles, "required_owner_roles"); err != nil {
		return err
	}
	for _, ownerRole := range ownerRoles {
		if !regexp.MustCompile(`^[a-z][a-z0-9_]*_owner$`).MatchString(ownerRole) {
			return fmt.Errorf("required_owner_role 非法=%q", ownerRole)
		}
	}
	return nil
}

// reviewFreezeValidateBlockersV1 确保阻断码属于当前 Gate、排序唯一且每项都有可执行中文说明。
func reviewFreezeValidateBlockersV1(gate string, blockers []reviewFreezeBlockerV1) error {
	last := ""
	wantPrefix := strings.ReplaceAll(gate, "-", "_") + "_"
	for _, blocker := range blockers {
		if !regexp.MustCompile(`^W2_R[0-9]{2}_[A-Z0-9_]+$`).MatchString(blocker.Code) {
			return fmt.Errorf("blocker code=%q", blocker.Code)
		}
		if !strings.HasPrefix(blocker.Code, wantPrefix) {
			return fmt.Errorf("blocker code=%q 不属于当前 Gate=%q", blocker.Code, gate)
		}
		if blocker.Code <= last {
			return fmt.Errorf("blockers 未排序或重复=%q", blocker.Code)
		}
		if strings.TrimSpace(blocker.Statement) == "" {
			return fmt.Errorf("blocker %s 缺 statement", blocker.Code)
		}
		last = blocker.Code
	}
	return nil
}

// reviewFreezeValidateContractRefV1 校验 manifest 摘要并与其向量和目标测试 exact-set 对齐。
func reviewFreezeValidateContractRefV1(path, expectedSHA string, vectorIDs, targetTests []string, loader reviewFreezeArtifactLoaderV1) error {
	if err := reviewFreezeValidateContractManifestPathV1(path); err != nil {
		return err
	}
	if err := reviewFreezeValidateSortedExactSetV1(vectorIDs, "vector_ids"); err != nil {
		return err
	}
	if err := reviewFreezeValidateSortedExactSetV1(targetTests, "target_tests"); err != nil {
		return err
	}
	raw, err := loader(path)
	if err != nil {
		return fmt.Errorf("读取 contract manifest %s: %w", path, err)
	}
	if err := reviewFreezeValidateValidatorBuildClosureJSONV1(raw); err != nil {
		return fmt.Errorf("contract manifest %s: %w", path, err)
	}
	if err := reviewFreezeCheckSHA256V1(raw, expectedSHA); err != nil {
		return fmt.Errorf("contract manifest %s: %w", path, err)
	}
	var source reviewFreezeCorpusManifestV1
	if err := messageSetStrictDecodeV1(raw, &source); err != nil {
		return fmt.Errorf("严格解析 contract manifest %s: %w", path, err)
	}
	if source.SchemaVersion == "" || source.TotalVectorCount != len(source.VectorIDs) || len(source.Files) == 0 {
		return fmt.Errorf("contract manifest %s 版本或向量总数非法", path)
	}
	if err := reviewFreezeValidateCorpusFilesV1(path, source, loader); err != nil {
		return err
	}
	if err := reviewFreezeValidateValidatorSourcesV1(source.ValidatorSources, loader); err != nil {
		return err
	}
	if err := reviewFreezeValidateValidatorBuildSourcesV1(source.ValidatorSources, source.ValidatorBuildSources, loader); err != nil {
		return err
	}
	if err := reviewFreezeValidateValidatorBuildClosureV1(source, loader, nil); err != nil {
		return err
	}
	if err := reviewFreezeValidateOptionalDesignSourcesV1(source.DesignSources, loader); err != nil {
		return err
	}
	sourceVectors := append([]string(nil), source.VectorIDs...)
	sourceTests := append([]string(nil), source.TargetTests...)
	sort.Strings(sourceVectors)
	sort.Strings(sourceTests)
	if !reflect.DeepEqual(vectorIDs, sourceVectors) || !reflect.DeepEqual(targetTests, sourceTests) {
		return fmt.Errorf("contract manifest %s 的 vector/target-test exact-set 漂移", path)
	}
	return nil
}

// reviewFreezeValidateValidatorSourcesV1 校验非空、排序唯一的 Validator 源 exact-set，并通过当前 Git/worktree loader 固定逐文件原始摘要。
func reviewFreezeValidateValidatorSourcesV1(sources []reviewFreezeValidatorSourceV1, loader reviewFreezeArtifactLoaderV1) error {
	if len(sources) == 0 {
		return fmt.Errorf("validator_sources exact-set 不能为空")
	}
	lastPath := ""
	for _, source := range sources {
		if err := reviewFreezeValidateValidatorSourcePathV1(source.Path); err != nil {
			return fmt.Errorf("validator source: %w", err)
		}
		if source.Path <= lastPath {
			return fmt.Errorf("validator_sources 未排序或重复=%q", source.Path)
		}
		raw, err := loader(source.Path)
		if err != nil {
			return fmt.Errorf("读取 validator source %s: %w", source.Path, err)
		}
		if err := reviewFreezeCheckSHA256V1(raw, source.SHA256); err != nil {
			return fmt.Errorf("validator source %s: %w", source.Path, err)
		}
		lastPath = source.Path
	}
	return nil
}

// reviewFreezeValidateValidatorSourcePathV1 将 Validator authority 限定到既有 contract test 目录中的 Go 源文件。
func reviewFreezeValidateValidatorSourcePathV1(path string) error {
	if err := reviewFreezeValidateSafePathV1(path, ""); err != nil {
		return err
	}
	allowedPrefixes := []string{
		"agent/tests/contract/",
		"business/tests/contract/",
		"frontend/tests/contract/",
		"smoke/contracts/",
		"worker/tests/contract/",
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(path, prefix) && strings.HasSuffix(path, ".go") {
			return nil
		}
	}
	return fmt.Errorf("validator source 路径不是允许的 contract test Go 文件=%q", path)
}

// reviewFreezeValidateCorpusFilesV1 校验 Corpus 文件 exact-set、逐文件摘要及总向量数，防止只固定顶层 manifest 而替换底层向量。
func reviewFreezeValidateCorpusFilesV1(manifestPath string, source reviewFreezeCorpusManifestV1, loader reviewFreezeArtifactLoaderV1) error {
	lastFile := ""
	totalVectors := 0
	manifestDir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(manifestPath)))
	for _, corpusFile := range source.Files {
		if err := reviewFreezeValidateSafePathV1(corpusFile.File, ""); err != nil {
			return fmt.Errorf("contract corpus file: %w", err)
		}
		if corpusFile.File <= lastFile || corpusFile.VectorCount <= 0 {
			return fmt.Errorf("contract corpus files 未排序、重复或 vector_count 非正=%q", corpusFile.File)
		}
		joined := filepath.ToSlash(filepath.Join(filepath.FromSlash(manifestDir), filepath.FromSlash(corpusFile.File)))
		raw, err := loader(joined)
		if err != nil {
			return fmt.Errorf("读取 contract corpus file %s: %w", joined, err)
		}
		if err := reviewFreezeCheckSHA256V1(raw, corpusFile.SHA256); err != nil {
			return fmt.Errorf("contract corpus file %s: %w", joined, err)
		}
		totalVectors += corpusFile.VectorCount
		lastFile = corpusFile.File
	}
	if totalVectors != source.TotalVectorCount {
		return fmt.Errorf("contract corpus files vector_count=%d want=%d", totalVectors, source.TotalVectorCount)
	}
	if err := reviewFreezeValidateSortedExactSetV1(source.FixtureIDs, "fixture_ids"); err != nil {
		return err
	}
	return nil
}

// reviewFreezeValidateOptionalDesignSourcesV1 在 manifest 声明设计源时逐项固定仓库路径和原始摘要。
func reviewFreezeValidateOptionalDesignSourcesV1(sources []reviewFreezeDesignSourceV1, loader reviewFreezeArtifactLoaderV1) error {
	lastPath := ""
	for _, source := range sources {
		if err := reviewFreezeValidateSafePathV1(source.Path, "docs/"); err != nil {
			return fmt.Errorf("design source: %w", err)
		}
		if source.Path <= lastPath {
			return fmt.Errorf("design_sources 未排序或重复=%q", source.Path)
		}
		raw, err := loader(source.Path)
		if err != nil {
			return fmt.Errorf("读取 design source %s: %w", source.Path, err)
		}
		if err := reviewFreezeCheckSHA256V1(raw, source.SHA256); err != nil {
			return fmt.Errorf("design source %s: %w", source.Path, err)
		}
		lastPath = source.Path
	}
	return nil
}

// reviewFreezeValidateContractManifestPathV1 允许各受影响 Module、前端和 Smoke 保存各自契约 manifest，同时拒绝生产源码与任意文档冒充机器基线。
func reviewFreezeValidateContractManifestPathV1(path string) error {
	if err := reviewFreezeValidateSafePathV1(path, ""); err != nil {
		return err
	}
	allowedPrefixes := []string{
		"agent/tests/contract/",
		"business/tests/contract/",
		"frontend/tests/contract/",
		"smoke/contracts/",
		"worker/tests/contract/",
	}
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(path, prefix) && strings.HasSuffix(path, ".json") && strings.Contains(filepath.Base(path), "manifest") {
			return nil
		}
	}
	return fmt.Errorf("contract manifest 路径不在契约测试白名单=%q", path)
}

// reviewFreezeValidateFormalRecordV1 校验正式 Freeze、审批清单摘要和重新批准绑定。
func reviewFreezeValidateFormalRecordV1(manifest reviewFreezeManifestV1, gate reviewFreezeGateV1, exception *reviewFreezeExceptionManifestV1, loader reviewFreezeArtifactLoaderV1) error {
	freeze := gate.Freeze
	exceptionID := ""
	if exception != nil {
		exceptionID = exception.ExceptionID
	}
	wantFreezeID := regexp.MustCompile(`^CF-` + regexp.QuoteMeta(gate.Gate) + `-v[1-9][0-9]*$`)
	if !wantFreezeID.MatchString(freeze.FreezeID) {
		return fmt.Errorf("freeze_id=%q", freeze.FreezeID)
	}
	if err := reviewFreezeValidateContractRefV1(freeze.ContractManifestPath, freeze.ContractManifestSHA256, freeze.VectorIDs, freeze.TargetTests, loader); err != nil {
		return fmt.Errorf("freeze contract: %w", err)
	}
	if err := reviewFreezeValidateUTCTimeV1(freeze.FrozenAt); err != nil {
		return fmt.Errorf("frozen_at: %w", err)
	}
	if gate.Status != "reopened" && exceptionID == "" && freeze.SupersedesFreezeID != "" {
		return fmt.Errorf("supersedes freeze 缺 CFE")
	}
	if exceptionID != "" && gate.Status == "approved" && freeze.SupersedesFreezeID == "" {
		return fmt.Errorf("CFE 后 approved 缺 supersedes_freeze_id")
	}
	if gate.Status == "reopened" && gate.ReopenException.ParentFreezeID != freeze.FreezeID {
		return fmt.Errorf("reopened parent freeze 不一致")
	}
	if gate.Status == "approved" && freeze.SupersedesFreezeID != "" && gate.ReopenException.ParentFreezeID != freeze.SupersedesFreezeID {
		return fmt.Errorf("reapproved supersedes 与 CFE parent 不一致")
	}
	if gate.Status == "approved" && freeze.SupersedesFreezeID != "" {
		if exception == nil || freeze.ContractManifestSHA256 != exception.Manifest.ContractManifestSHA256 ||
			!reflect.DeepEqual(freeze.VectorIDs, exception.Manifest.VectorIDs) || !reflect.DeepEqual(freeze.TargetTests, exception.Manifest.TargetTests) {
			return fmt.Errorf("reapproved freeze 未采用 CFE 更新后的 manifest/vector/target-test exact-set")
		}
	}
	return reviewFreezeValidateOwnerApprovalV1(manifest, gate, exceptionID, loader)
}

// reviewFreezeValidateOwnerApprovalV1 校验审批文件不可变摘要、记录者、自批隔离和 CFE 重新批准。
func reviewFreezeValidateOwnerApprovalV1(manifest reviewFreezeManifestV1, gate reviewFreezeGateV1, exceptionID string, loader reviewFreezeArtifactLoaderV1) error {
	ref := gate.Freeze.OwnerApprovalRef
	if err := reviewFreezeValidateSafePathV1(ref.Path, "docs/design/agent/approvals/w2-review-freeze-owner-approvals/"); err != nil {
		return fmt.Errorf("owner approval: %w", err)
	}
	raw, err := loader(ref.Path)
	if err != nil {
		return fmt.Errorf("读取 owner approval: %w", err)
	}
	if err := reviewFreezeCheckSHA256V1(raw, ref.OwnerApprovalManifestSHA256); err != nil {
		return fmt.Errorf("owner approval manifest: %w", err)
	}
	var approval reviewFreezeOwnerApprovalManifestV1
	if err := messageSetStrictDecodeV1(raw, &approval); err != nil {
		return fmt.Errorf("owner approval 严格 JSON: %w", err)
	}
	if approval.SchemaVersion != reviewFreezeOwnerApprovalSchemaV1 || approval.ApprovalID == "" {
		return fmt.Errorf("owner approval 版本或 ID 非法")
	}
	if approval.Gate != gate.Gate || approval.FreezeID != gate.Freeze.FreezeID || approval.ContractManifestSHA256 != gate.Freeze.ContractManifestSHA256 {
		return fmt.Errorf("owner approval 未绑定同一 gate/freeze/manifest")
	}
	if approval.RecordedByRole != manifest.GovernanceOwnerRole || reviewFreezeContainsV1(manifest.ImplementationOwnerRoles, approval.RecordedByRole) {
		return fmt.Errorf("实施角色不得写入批准: recorded_by_role=%q", approval.RecordedByRole)
	}
	if err := reviewFreezeValidateOwnerSignaturesV1(manifest, gate.RequiredOwnerRoles, approval.OwnerApprovals); err != nil {
		return err
	}
	wantSummary := reviewFreezeApprovalSummaryV1(gate)
	if ref.ApprovalSummarySHA256 != wantSummary || approval.ApprovalSummarySHA256 != wantSummary {
		return fmt.Errorf("approval summary 不一致 ref=%q approval=%q want=%q", ref.ApprovalSummarySHA256, approval.ApprovalSummarySHA256, wantSummary)
	}
	if gate.Status == "approved" && gate.Freeze.SupersedesFreezeID != "" && approval.ReapprovalExceptionID != exceptionID {
		return fmt.Errorf("approved 未绑定重新审批 CFE=%q", exceptionID)
	}
	if gate.Freeze.SupersedesFreezeID == "" && approval.ReapprovalExceptionID != "" {
		return fmt.Errorf("首次冻结不得声明 reapproval_exception_id")
	}
	return nil
}

// reviewFreezeValidateOwnerSignaturesV1 确保 Owner 引用排序唯一、不可由实施角色代签且时间有效。
func reviewFreezeValidateOwnerSignaturesV1(manifest reviewFreezeManifestV1, requiredOwnerRoles []string, approvals []reviewFreezeOwnerSignatureV1) error {
	if len(approvals) == 0 {
		return fmt.Errorf("owner approvals 不能为空")
	}
	last := ""
	for _, approval := range approvals {
		if approval.OwnerRole == "" || approval.OwnerRole <= last {
			return fmt.Errorf("owner approvals 未排序、重复或为空=%q", approval.OwnerRole)
		}
		if approval.ApproverRole == "" || reviewFreezeContainsV1(manifest.ImplementationOwnerRoles, approval.ApproverRole) {
			return fmt.Errorf("Implementation Owner 不得自批: approver_role=%q", approval.ApproverRole)
		}
		if !strings.HasPrefix(approval.ReviewURL, "https://") || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(approval.CommitSHA) {
			return fmt.Errorf("owner %s review_url/commit_sha 非法", approval.OwnerRole)
		}
		if err := reviewFreezeValidateUTCTimeV1(approval.ApprovedAt); err != nil {
			return fmt.Errorf("owner %s approved_at: %w", approval.OwnerRole, err)
		}
		last = approval.OwnerRole
	}
	actualOwnerRoles := make([]string, len(approvals))
	for index, approval := range approvals {
		actualOwnerRoles[index] = approval.OwnerRole
	}
	if !reflect.DeepEqual(actualOwnerRoles, requiredOwnerRoles) {
		return fmt.Errorf("owner approval roles exact-set=%v want=%v", actualOwnerRoles, requiredOwnerRoles)
	}
	return nil
}

// reviewFreezeValidateExceptionV1 校验 reopened 或 CFE 后重新批准所绑定的最小受控例外。
func reviewFreezeValidateExceptionV1(manifest reviewFreezeManifestV1, gate reviewFreezeGateV1, loader reviewFreezeArtifactLoaderV1) (*reviewFreezeExceptionManifestV1, error) {
	ref := gate.ReopenException
	if ref == nil {
		return nil, nil
	}
	if gate.Status != "reopened" && !(gate.Status == "approved" && gate.Freeze.SupersedesFreezeID != "") {
		return nil, fmt.Errorf("只有 reopened 或 CFE 后 approved 可携带 reopen_exception")
	}
	if err := reviewFreezeValidateSafePathV1(ref.Path, "docs/design/agent/approvals/w2-review-freeze-exceptions/"); err != nil {
		return nil, fmt.Errorf("CFE: %w", err)
	}
	raw, err := loader(ref.Path)
	if err != nil {
		return nil, fmt.Errorf("读取 CFE: %w", err)
	}
	if err := reviewFreezeCheckSHA256V1(raw, ref.ExceptionManifestSHA256); err != nil {
		return nil, fmt.Errorf("CFE manifest: %w", err)
	}
	var exception reviewFreezeExceptionManifestV1
	if err := messageSetStrictDecodeV1(raw, &exception); err != nil {
		return nil, fmt.Errorf("CFE 严格 JSON: %w", err)
	}
	if exception.SchemaVersion != reviewFreezeExceptionSchemaV1 || !regexp.MustCompile(`^CFE-W2-R[0-9]{2}-[A-Z0-9-]+$`).MatchString(exception.ExceptionID) {
		return nil, fmt.Errorf("CFE 版本或 exception_id 非法")
	}
	if exception.ExceptionID != ref.ExceptionID || exception.ParentFreezeID != ref.ParentFreezeID {
		return nil, fmt.Errorf("CFE ref 与 manifest 不一致")
	}
	if exception.RecordedByRole != manifest.GovernanceOwnerRole || reviewFreezeContainsV1(manifest.ImplementationOwnerRoles, exception.RecordedByRole) {
		return nil, fmt.Errorf("CFE 必须由 Integration Owner 记录")
	}
	impactedGateCount := 0
	for _, impacted := range exception.ImpactedADROrGate {
		switch {
		case regexp.MustCompile(`^W2-R[0-9]{2}$`).MatchString(impacted):
			impactedGateCount++
			if impacted != gate.Gate {
				return nil, fmt.Errorf("CFE 不得包含额外 Gate=%q", impacted)
			}
		case regexp.MustCompile(`^W2-ADR-[0-9]{3}$`).MatchString(impacted):
		default:
			return nil, fmt.Errorf("CFE impacted_adr_or_gate 非法=%q", impacted)
		}
	}
	if impactedGateCount != 1 || reviewFreezeContainsPrefixV1(exception.ImpactedADROrGate, "W2-R09") {
		return nil, fmt.Errorf("CFE impacted_adr_or_gate 必须且只能绑定当前 Gate")
	}
	if err := reviewFreezeValidateSortedExactSetV1(exception.ImpactedADROrGate, "impacted_adr_or_gate"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(exception.Blocker) == "" || strings.TrimSpace(exception.Compatibility) == "" || strings.TrimSpace(exception.ProductionConsumer) == "" {
		return nil, fmt.Errorf("CFE blocker/compatibility/production_consumer 不能为空")
	}
	if err := reviewFreezeValidateSortedExactSetV1(exception.AllowedFiles, "allowed_files"); err != nil {
		return nil, err
	}
	for _, path := range exception.AllowedFiles {
		if err := reviewFreezeValidateSafePathV1(path, ""); err != nil {
			return nil, fmt.Errorf("CFE allowed_files: %w", err)
		}
	}
	if err := reviewFreezeValidateSortedExactSetV1(exception.VectorIDs, "CFE vector_ids"); err != nil {
		return nil, err
	}
	if err := reviewFreezeValidateSortedExactSetV1(exception.Manifest.VectorIDs, "CFE manifest vector_ids"); err != nil {
		return nil, err
	}
	for _, vectorID := range exception.VectorIDs {
		if !reviewFreezeContainsV1(exception.Manifest.VectorIDs, vectorID) {
			return nil, fmt.Errorf("CFE delta vector %q 未进入更新后 manifest exact-set", vectorID)
		}
	}
	if err := reviewFreezeValidateSortedExactSetV1(exception.Manifest.TargetTests, "CFE manifest target_tests"); err != nil {
		return nil, err
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(exception.Manifest.ContractManifestSHA256) {
		return nil, fmt.Errorf("CFE manifest contract sha 非法")
	}
	if err := reviewFreezeValidateOwnerSignaturesV1(manifest, gate.RequiredOwnerRoles, exception.OwnerApprovals); err != nil {
		return nil, fmt.Errorf("CFE owner approvals: %w", err)
	}
	return &exception, nil
}

// reviewFreezeApprovalSummaryV1 对正式冻结和可选 CFE 引用做无歧义摘要，防止审批引用被移花接木。
func reviewFreezeApprovalSummaryV1(gate reviewFreezeGateV1) string {
	freeze := gate.Freeze
	reopenPath := ""
	reopenManifestSHA := ""
	reopenExceptionID := ""
	reopenParentFreezeID := ""
	if gate.ReopenException != nil {
		reopenPath = gate.ReopenException.Path
		reopenManifestSHA = gate.ReopenException.ExceptionManifestSHA256
		reopenExceptionID = gate.ReopenException.ExceptionID
		reopenParentFreezeID = gate.ReopenException.ParentFreezeID
	}
	canonical := strings.Join([]string{
		"schema=" + reviewFreezeApprovalSummarySchemaV2,
		"gate=" + gate.Gate,
		"status=" + gate.Status,
		"required_owner_roles=" + strings.Join(gate.RequiredOwnerRoles, "\x1f"),
		"freeze_id=" + freeze.FreezeID,
		"supersedes_freeze_id=" + freeze.SupersedesFreezeID,
		"owner_approval_path=" + freeze.OwnerApprovalRef.Path,
		"contract_manifest_path=" + freeze.ContractManifestPath,
		"contract_manifest_sha256=" + freeze.ContractManifestSHA256,
		"vector_ids=" + strings.Join(freeze.VectorIDs, "\x1f"),
		"target_tests=" + strings.Join(freeze.TargetTests, "\x1f"),
		"frozen_at=" + freeze.FrozenAt,
		"reopen_exception_path=" + reopenPath,
		"reopen_exception_manifest_sha256=" + reopenManifestSHA,
		"reopen_exception_id=" + reopenExceptionID,
		"reopen_parent_freeze_id=" + reopenParentFreezeID,
	}, "\n")
	return reviewFreezeSHA256V1([]byte(canonical))
}

// reviewFreezeValidateSortedExactSetV1 要求集合非空、按字节序排序且不存在重复值。
func reviewFreezeValidateSortedExactSetV1(values []string, field string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s exact-set 不能为空", field)
	}
	last := ""
	for _, value := range values {
		if strings.TrimSpace(value) == "" || value <= last {
			return fmt.Errorf("%s 未排序、重复或为空=%q", field, value)
		}
		last = value
	}
	return nil
}

// reviewFreezeValidateSafePathV1 拒绝绝对路径、清理后漂移、父目录逃逸和错误目录前缀。
func reviewFreezeValidateSafePathV1(relative, requiredPrefix string) error {
	if relative == "" || strings.ContainsAny(relative, "\x00\r\n\t\\") || filepath.IsAbs(relative) || filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))) != relative || relative == ".." || strings.HasPrefix(relative, "../") {
		return fmt.Errorf("不安全路径=%q", relative)
	}
	if requiredPrefix != "" && !strings.HasPrefix(relative, requiredPrefix) {
		return fmt.Errorf("路径 %q 不在允许前缀 %q", relative, requiredPrefix)
	}
	return nil
}

// reviewFreezeValidateUTCTimeV1 要求时间为带 Z 的规范 UTC RFC3339。
func reviewFreezeValidateUTCTimeV1(value string) error {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || !strings.HasSuffix(value, "Z") || parsed.Location() != time.UTC {
		return fmt.Errorf("必须是 UTC RFC3339=%q", value)
	}
	return nil
}

// reviewFreezeCheckSHA256V1 使用常量时间比较校验 sha256: 小写摘要。
func reviewFreezeCheckSHA256V1(raw []byte, expected string) error {
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(expected) {
		return fmt.Errorf("sha256 格式非法=%q", expected)
	}
	actual := reviewFreezeSHA256V1(raw)
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return fmt.Errorf("sha256=%s want=%s", actual, expected)
	}
	return nil
}

// reviewFreezeSHA256V1 返回带算法前缀的小写 SHA-256。
func reviewFreezeSHA256V1(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// reviewFreezeRepositoryLoaderV1 创建拒绝符号链接逃逸的仓库只读加载器。
func reviewFreezeRepositoryLoaderV1(root string) reviewFreezeArtifactLoaderV1 {
	return func(relative string) ([]byte, error) {
		if err := reviewFreezeValidateSafePathV1(relative, ""); err != nil {
			return nil, err
		}
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, err
		}
		inside, err := filepath.Rel(realRoot, realPath)
		if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("路径逃逸仓库=%q", relative)
		}
		info, err := os.Stat(realPath)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("不是普通文件=%q err=%v", relative, err)
		}
		return os.ReadFile(realPath)
	}
}

// reviewFreezeOverlayLoaderV1 让负向状态机测试覆盖虚拟审批文件，同时回退读取真实候选基线。
func reviewFreezeOverlayLoaderV1(overlay map[string][]byte, fallback reviewFreezeArtifactLoaderV1) reviewFreezeArtifactLoaderV1 {
	return func(relative string) ([]byte, error) {
		if raw, ok := overlay[relative]; ok {
			return append([]byte(nil), raw...), nil
		}
		return fallback(relative)
	}
}

// reviewFreezeLoadCurrentV1 严格读取当前 Integration Owner 治理清单。
func reviewFreezeLoadCurrentV1(t *testing.T) (reviewFreezeManifestV1, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(reviewFreezeRepoRootV1(t), filepath.FromSlash(reviewFreezeManifestPathV1)))
	if err != nil {
		t.Fatal(err)
	}
	var manifest reviewFreezeManifestV1
	if err := messageSetStrictDecodeV1(raw, &manifest); err != nil {
		t.Fatalf("strict decode %s: %v", reviewFreezeManifestPathV1, err)
	}
	return manifest, raw
}

// reviewFreezeRepoRootV1 从 Agent contract test 工作目录解析多 Module 仓库根。
func reviewFreezeRepoRootV1(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		t.Fatalf("resolve repository root from %s: %v", cwd, err)
	}
	return root
}

// reviewFreezeSyntheticFormalV1 构造只用于门禁负向测试的正式 Freeze、审批和可选 CFE。
func reviewFreezeSyntheticFormalV1(t *testing.T, status string, reapproved bool) (reviewFreezeManifestV1, map[string][]byte) {
	t.Helper()
	manifest, _ := reviewFreezeLoadCurrentV1(t)
	overlay := make(map[string][]byte)
	contractPath := "agent/tests/contract/testdata/w2_review_freeze_synthetic/manifest.json"
	contractVectors := []string{"V-001"}
	if reapproved {
		contractVectors = []string{"V-001", "V-002"}
	}
	contractFixtureName := "vectors.json"
	contractFixtureRaw := reviewFreezeMarshalV1(t, contractVectors)
	validatorPath := "worker/tests/contract/w2_review_freeze_synthetic_validator_test.go"
	validatorRaw := []byte("package contract_test\n\nfunc TestSynthetic() {}\n")
	validatorSources := []reviewFreezeValidatorSourceV1{{Path: validatorPath, SHA256: reviewFreezeSHA256V1(validatorRaw)}}
	validatorGoModRaw := []byte("module example.invalid/review-freeze-synthetic\n\ngo 1.26\n")
	validatorGoSumRaw := []byte("example.invalid/dependency v1.0.0 h1:synthetic\n")
	validatorBuildSources := []reviewFreezeValidatorSourceV1{
		{Path: "worker/go.mod", SHA256: reviewFreezeSHA256V1(validatorGoModRaw)},
		{Path: "worker/go.sum", SHA256: reviewFreezeSHA256V1(validatorGoSumRaw)},
	}
	contractRaw := reviewFreezeMarshalV1(t, reviewFreezeCorpusManifestV1{
		SchemaVersion: "synthetic_contract_manifest.v1",
		Files: []reviewFreezeCorpusFileV1{{
			File: contractFixtureName, SHA256: reviewFreezeSHA256V1(contractFixtureRaw), VectorCount: len(contractVectors),
		}},
		FixtureIDs: []string{"synthetic.open"}, ValidatorSources: validatorSources, ValidatorBuildSources: validatorBuildSources, VectorIDs: contractVectors,
		TotalVectorCount: len(contractVectors), TargetTests: []string{"TestSynthetic"},
	})
	cfeVectors := []string{"V-001", "V-002"}
	cfeFixtureRaw := reviewFreezeMarshalV1(t, cfeVectors)
	cfeContractRaw := reviewFreezeMarshalV1(t, reviewFreezeCorpusManifestV1{
		SchemaVersion: "synthetic_contract_manifest.v1",
		Files: []reviewFreezeCorpusFileV1{{
			File: contractFixtureName, SHA256: reviewFreezeSHA256V1(cfeFixtureRaw), VectorCount: len(cfeVectors),
		}},
		FixtureIDs: []string{"synthetic.open"}, ValidatorSources: validatorSources, ValidatorBuildSources: validatorBuildSources, VectorIDs: cfeVectors,
		TotalVectorCount: len(cfeVectors), TargetTests: []string{"TestSynthetic"},
	})
	overlay[contractPath] = contractRaw
	overlay[filepath.ToSlash(filepath.Join(filepath.Dir(contractPath), contractFixtureName))] = contractFixtureRaw
	overlay[validatorPath] = validatorRaw
	overlay["worker/go.mod"] = validatorGoModRaw
	overlay["worker/go.sum"] = validatorGoSumRaw

	freezeID := "CF-W2-R00-v1"
	supersedes := ""
	if reapproved {
		freezeID = "CF-W2-R00-v2"
		supersedes = "CF-W2-R00-v1"
	}
	gate := reviewFreezeGateV1{
		Gate:               "W2-R00",
		Status:             status,
		RequiredOwnerRoles: []string{"agent_owner", "business_owner", "finance_owner", "product_owner", "security_owner"},
		CandidateEvidence:  []reviewFreezeCandidateEvidenceV1{},
		Blockers:           []reviewFreezeBlockerV1{},
		Freeze: &reviewFreezeRecordV1{
			FreezeID:               freezeID,
			SupersedesFreezeID:     supersedes,
			ContractManifestPath:   contractPath,
			ContractManifestSHA256: reviewFreezeSHA256V1(contractRaw),
			VectorIDs:              contractVectors,
			TargetTests:            []string{"TestSynthetic"},
			FrozenAt:               "2026-07-15T00:00:00Z",
		},
	}

	exceptionID := ""
	if status == "reopened" || reapproved {
		exceptionID = "CFE-W2-R00-SECURITY-001"
		exceptionPath := "docs/design/agent/approvals/w2-review-freeze-exceptions/cfe-w2-r00-security-001.json"
		exception := reviewFreezeExceptionManifestV1{
			SchemaVersion:      reviewFreezeExceptionSchemaV1,
			ExceptionID:        exceptionID,
			ParentFreezeID:     "CF-W2-R00-v1",
			ImpactedADROrGate:  []string{"W2-R00"},
			Blocker:            "发现 Unknown Outcome 可能重复扣费。",
			AllowedFiles:       []string{"agent/tests/contract/testdata/w2_review_freeze_synthetic/manifest.json"},
			VectorIDs:          []string{"V-002"},
			Compatibility:      "不破坏既有 Digest；仅增加失败关闭反例。",
			ProductionConsumer: "business billing canonical validator",
			RecordedByRole:     manifest.GovernanceOwnerRole,
			OwnerApprovals:     reviewFreezeSyntheticOwnerApprovalsV1(gate.RequiredOwnerRoles, "cfe-r00", "1"),
			Manifest: reviewFreezeExceptionBaselineV1{
				ContractManifestSHA256: reviewFreezeSHA256V1(cfeContractRaw),
				VectorIDs:              []string{"V-001", "V-002"},
				TargetTests:            []string{"TestSynthetic"},
			},
		}
		exceptionRaw := reviewFreezeMarshalV1(t, exception)
		overlay[exceptionPath] = exceptionRaw
		gate.ReopenException = &reviewFreezeExceptionRefV1{
			Path: exceptionPath, ExceptionManifestSHA256: reviewFreezeSHA256V1(exceptionRaw),
			ExceptionID: exceptionID, ParentFreezeID: "CF-W2-R00-v1",
		}
	}
	if status == "reopened" {
		gate.Blockers = []reviewFreezeBlockerV1{{Code: "W2_R00_CFE_OPEN", Statement: "CFE 尚未重新冻结和批准，生产实现继续阻断。"}}
	}

	approvalPath := "docs/design/agent/approvals/w2-review-freeze-owner-approvals/w2-r00.json"
	gate.Freeze.OwnerApprovalRef.Path = approvalPath
	approvalSummary := reviewFreezeApprovalSummaryV1(gate)
	approval := reviewFreezeOwnerApprovalManifestV1{
		SchemaVersion:          reviewFreezeOwnerApprovalSchemaV1,
		ApprovalID:             "APR-W2-R00-001",
		Gate:                   gate.Gate,
		FreezeID:               gate.Freeze.FreezeID,
		ContractManifestSHA256: gate.Freeze.ContractManifestSHA256,
		ApprovalSummarySHA256:  approvalSummary,
		ReapprovalExceptionID:  "",
		RecordedByRole:         manifest.GovernanceOwnerRole,
		OwnerApprovals:         reviewFreezeSyntheticOwnerApprovalsV1(gate.RequiredOwnerRoles, "approval-r00", "2"),
	}
	if reapproved {
		approval.ReapprovalExceptionID = exceptionID
	}
	approvalRaw := reviewFreezeMarshalV1(t, approval)
	overlay[approvalPath] = approvalRaw
	gate.Freeze.OwnerApprovalRef = reviewFreezeOwnerApprovalRefV1{
		Path: approvalPath, OwnerApprovalManifestSHA256: reviewFreezeSHA256V1(approvalRaw), ApprovalSummarySHA256: approvalSummary,
	}
	manifest.Gates[0] = gate
	return manifest, overlay
}

// reviewFreezeSyntheticOwnerApprovalsV1 为状态机测试按 Gate Owner exact-set 生成排序的不可变引用。
func reviewFreezeSyntheticOwnerApprovalsV1(ownerRoles []string, reviewGroup, commitDigit string) []reviewFreezeOwnerSignatureV1 {
	approvals := make([]reviewFreezeOwnerSignatureV1, len(ownerRoles))
	for index, ownerRole := range ownerRoles {
		approvals[index] = reviewFreezeOwnerSignatureV1{
			OwnerRole: ownerRole, ApproverRole: strings.TrimSuffix(ownerRole, "_owner") + "_reviewer",
			ReviewURL: "https://review.example/" + reviewGroup + "/" + ownerRole,
			CommitSHA: strings.Repeat(commitDigit, 40), ApprovedAt: "2026-07-15T00:00:00Z",
		}
	}
	return approvals
}

// reviewFreezeDecodeOwnerApprovalV1 严格解码负向测试中的虚拟审批清单。
func reviewFreezeDecodeOwnerApprovalV1(t *testing.T, raw []byte) reviewFreezeOwnerApprovalManifestV1 {
	t.Helper()
	var approval reviewFreezeOwnerApprovalManifestV1
	if err := messageSetStrictDecodeV1(raw, &approval); err != nil {
		t.Fatal(err)
	}
	return approval
}

// reviewFreezeDecodeExceptionV1 严格解码负向测试中的虚拟 CFE 清单。
func reviewFreezeDecodeExceptionV1(t *testing.T, raw []byte) reviewFreezeExceptionManifestV1 {
	t.Helper()
	var exception reviewFreezeExceptionManifestV1
	if err := messageSetStrictDecodeV1(raw, &exception); err != nil {
		t.Fatal(err)
	}
	return exception
}

// reviewFreezeReplaceOwnerApprovalV1 替换虚拟审批并同步文件摘要，确保负向测试命中目标规则。
func reviewFreezeReplaceOwnerApprovalV1(t *testing.T, gate *reviewFreezeGateV1, approval reviewFreezeOwnerApprovalManifestV1, overlay map[string][]byte) {
	t.Helper()
	path := gate.Freeze.OwnerApprovalRef.Path
	raw := reviewFreezeMarshalV1(t, approval)
	overlay[path] = raw
	gate.Freeze.OwnerApprovalRef.OwnerApprovalManifestSHA256 = reviewFreezeSHA256V1(raw)
}

// reviewFreezeReplaceExceptionV1 替换虚拟 CFE 并同步文件摘要，确保负向测试命中跨 Gate 规则。
func reviewFreezeReplaceExceptionV1(t *testing.T, gate *reviewFreezeGateV1, exception reviewFreezeExceptionManifestV1, overlay map[string][]byte) {
	t.Helper()
	path := gate.ReopenException.Path
	raw := reviewFreezeMarshalV1(t, exception)
	overlay[path] = raw
	gate.ReopenException.ExceptionManifestSHA256 = reviewFreezeSHA256V1(raw)
}

// reviewFreezeMarshalV1 为虚拟治理文件生成确定性 JSON 字节。
func reviewFreezeMarshalV1(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// reviewFreezeContainsV1 判断 exact-set 是否包含指定值。
func reviewFreezeContainsV1(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

// reviewFreezeContainsPrefixV1 判断 exact-set 是否错误包含指定前缀。
func reviewFreezeContainsPrefixV1(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
