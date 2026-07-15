// Package contract_test 验证 R02 Owner 待决请求只承载候选输入，不产生批准或实现解锁能力。
package contract_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const r02OwnerDecisionRequestPathV1 = "docs/design/agent/approvals/w2-r02-owner-decision-requests/DR-W2-R02-v1.json"

// r02OwnerDecisionRequestV1 描述 R02 语义 Owner 尚未选择前的严格请求形状。
// 它故意不包含选择、批准、Review 身份或提交绑定字段，避免仓库文件自报 authority。
type r02OwnerDecisionRequestV1 struct {
	SchemaVersion          string                          `json:"schema_version"`
	RequestID              string                          `json:"request_id"`
	Gate                   string                          `json:"gate"`
	Status                 string                          `json:"status"`
	ImplementationUnlocked bool                            `json:"implementation_unlocked"`
	OwnerRoleSetStatus     string                          `json:"owner_role_set_status"`
	DecisionDocument       r02OwnerDecisionArtifactRefV1   `json:"decision_document"`
	GateManifest           r02OwnerDecisionArtifactRefV1   `json:"gate_manifest"`
	ValidatorSource        r02OwnerDecisionArtifactRefV1   `json:"validator_source"`
	Items                  []r02OwnerDecisionRequestItemV1 `json:"items"`
	UnmetEvidence          []string                        `json:"unmet_evidence"`
	BlockedProductionGates []string                        `json:"blocked_production_gates"`
	BlockStatement         string                          `json:"block_statement"`
}

// r02OwnerDecisionArtifactRefV1 绑定请求依赖的仓库原始字节，防止候选矩阵或 Gate 状态静默漂移。
type r02OwnerDecisionArtifactRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// r02OwnerDecisionRequestItemV1 描述一个稳定决策 ID 的候选选项和临时候选 Owner 集合。
// CandidateOwnerRoles 不是正式 required_owner_roles，最终集合必须在语义裁决后重新推导。
type r02OwnerDecisionRequestItemV1 struct {
	DecisionID          string   `json:"decision_id"`
	Title               string   `json:"title"`
	Status              string   `json:"status"`
	AllowedOptionIDs    []string `json:"allowed_option_ids"`
	RecommendedOptionID string   `json:"recommended_option_id"`
	CandidateOwnerRoles []string `json:"candidate_owner_roles"`
	MatrixAnchor        string   `json:"matrix_anchor"`
	BlockCode           string   `json:"block_code"`
}

// r02ReviewFreezeManifestProjectionV1 只读取当前 R02 Gate 的失败关闭事实；完整 manifest 由 reviewfreeze 包验证。
type r02ReviewFreezeManifestProjectionV1 struct {
	Gates []struct {
		Gate              string            `json:"gate"`
		Status            string            `json:"status"`
		Freeze            json.RawMessage   `json:"freeze"`
		CandidateEvidence []json.RawMessage `json:"candidate_evidence"`
		Blockers          []struct {
			Code string `json:"code"`
		} `json:"blockers"`
	} `json:"gates"`
}

var r02OwnerRolePatternV1 = regexp.MustCompile(`^[a-z][a-z0-9_]*_owner$`)

// TestW2R02OwnerDecisionRequestV1 验证待决请求绑定当前矩阵与 Gate，并拒绝任何批准能力字段。
func TestW2R02OwnerDecisionRequestV1(t *testing.T) {
	t.Parallel()

	repoRoot := r02OwnerDecisionRepoRootV1(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(r02OwnerDecisionRequestPathV1)))
	if err != nil {
		t.Fatal(err)
	}
	request, err := r02OwnerDecisionStrictDecodeV1(raw)
	if err != nil {
		t.Fatal(err)
	}

	if request.SchemaVersion != "w2_r02_owner_decision_request.v1" || request.RequestID != "DR-W2-R02-v1" || request.Gate != "W2-R02" {
		t.Fatalf("R02 Owner request identity 非法: %+v", request)
	}
	if request.Status != "awaiting_owner_decision" || request.ImplementationUnlocked || request.OwnerRoleSetStatus != "provisional_candidates_not_final" {
		t.Fatalf("R02 Owner request 不得提升状态或冻结 Owner: %+v", request)
	}
	if !strings.Contains(request.BlockStatement, "禁止") || !strings.Contains(request.BlockStatement, "expansion_frozen") {
		t.Fatalf("R02 Owner request 缺少失败关闭声明: %q", request.BlockStatement)
	}

	r02OwnerDecisionVerifyArtifactRefV1(t, repoRoot, request.DecisionDocument, "docs/design/agent/w2-r02-owner-decision-matrix-v1.md")
	r02OwnerDecisionVerifyArtifactRefV1(t, repoRoot, request.GateManifest, "docs/design/agent/approvals/w2-review-freeze-manifest.json")
	r02OwnerDecisionVerifyArtifactRefV1(t, repoRoot, request.ValidatorSource, "agent/tests/contract/w2_r02_owner_decision_request_v1_test.go")
	r02OwnerDecisionVerifyLiveGateV1(t, repoRoot, request.GateManifest.Path)

	wantUnmet := []string{
		"ADR_DISPOSITIONS_PENDING",
		"AGGREGATE_MANIFEST_MISSING",
		"BUILD_TRUST_CLOSURE_MISSING",
		"FINAL_OWNER_ROLE_EXACT_SET_MISSING",
		"GOVERNANCE_AUTHORITY_NOT_ACTIVE",
		"OWNER_DECISIONS_PENDING",
		"UPGRADE_EXACT_SET_PENDING",
	}
	if !reflect.DeepEqual(request.UnmetEvidence, wantUnmet) {
		t.Fatalf("R02 unmet evidence=%v want=%v", request.UnmetEvidence, wantUnmet)
	}
	if want := []string{"W2-A1", "W2-A2"}; !reflect.DeepEqual(request.BlockedProductionGates, want) {
		t.Fatalf("R02 blocked production gates=%v want=%v", request.BlockedProductionGates, want)
	}

	wantRoles := r02OwnerDecisionCandidateRolesV1()
	if len(request.Items) != 19 || len(wantRoles) != 19 {
		t.Fatalf("R02 decision items=%d roles=%d want=19", len(request.Items), len(wantRoles))
	}
	for index, item := range request.Items {
		wantID := fmt.Sprintf("R02-D%02d", index+1)
		if item.DecisionID != wantID || item.Status != "awaiting_owner_decision" || strings.TrimSpace(item.Title) == "" {
			t.Fatalf("R02 decision item[%d] identity/status 非法: %+v", index, item)
		}
		if want := []string{"accept_recommendation", "reject_keep_blocked"}; !reflect.DeepEqual(item.AllowedOptionIDs, want) || item.RecommendedOptionID != want[0] {
			t.Fatalf("%s option exact-set 非法: %+v", item.DecisionID, item)
		}
		if item.MatrixAnchor != fmt.Sprintf("r02-d%02d", index+1) || item.BlockCode != fmt.Sprintf("W2_R02_D%02d_OWNER_DECISION_PENDING", index+1) {
			t.Fatalf("%s anchor/block code 非法: %+v", item.DecisionID, item)
		}
		if !reflect.DeepEqual(item.CandidateOwnerRoles, wantRoles[index]) {
			t.Fatalf("%s candidate owner roles=%v want=%v", item.DecisionID, item.CandidateOwnerRoles, wantRoles[index])
		}
		if err := r02OwnerDecisionValidateSortedRolesV1(item.CandidateOwnerRoles); err != nil {
			t.Fatalf("%s candidate owner roles 非法: %v", item.DecisionID, err)
		}
	}

	for _, forbiddenKey := range []string{
		"selected_option", "accepted", "approved", "owner_approvals", "approval_refs",
		"review_id", "actor_id", "review_url", "approver_role", "approved_at", "commit_sha",
	} {
		if bytes.Contains(raw, []byte(`"`+forbiddenKey+`"`)) {
			t.Fatalf("R02 Owner request 禁止字段 %q", forbiddenKey)
		}
	}
}

// TestW2R02OwnerDecisionRequestV1StrictJSON 验证未知字段、重复键和尾随 JSON 均失败关闭。
func TestW2R02OwnerDecisionRequestV1StrictJSON(t *testing.T) {
	t.Parallel()

	if _, err := r02OwnerDecisionStrictDecodeV1([]byte(`{"schema_version":"x","future":true}`)); err == nil {
		t.Fatal("R02 Owner request 必须拒绝未知字段")
	}
	if _, err := r02OwnerDecisionStrictDecodeV1([]byte(`{"schema_version":"x","schema_version":"y"}`)); err == nil {
		t.Fatal("R02 Owner request 必须拒绝重复键")
	}
	if _, err := r02OwnerDecisionStrictDecodeV1([]byte(`{"schema_version":"x"}{}`)); err == nil {
		t.Fatal("R02 Owner request 必须拒绝尾随 JSON")
	}
}

// r02OwnerDecisionStrictDecodeV1 严格解码待决请求，避免宽松 JSON 把未审字段带入治理输入。
func r02OwnerDecisionStrictDecodeV1(raw []byte) (r02OwnerDecisionRequestV1, error) {
	if err := r02OwnerDecisionValidateJSONShapeV1(raw); err != nil {
		return r02OwnerDecisionRequestV1{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var request r02OwnerDecisionRequestV1
	if err := decoder.Decode(&request); err != nil {
		return r02OwnerDecisionRequestV1{}, err
	}
	if err := r02OwnerDecisionRequireEOFV1(decoder); err != nil {
		return r02OwnerDecisionRequestV1{}, err
	}
	return request, nil
}

// r02OwnerDecisionValidateJSONShapeV1 递归检查对象键唯一和单一顶层 JSON 值。
func r02OwnerDecisionValidateJSONShapeV1(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := r02OwnerDecisionReadJSONValueV1(decoder); err != nil {
		return err
	}
	return r02OwnerDecisionRequireEOFV1(decoder)
}

// r02OwnerDecisionReadJSONValueV1 读取一个完整 JSON 值，并在每个对象层拒绝重复键。
func r02OwnerDecisionReadJSONValueV1(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
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
			if valueErr := r02OwnerDecisionReadJSONValueV1(decoder); valueErr != nil {
				return valueErr
			}
		}
	case '[':
		for decoder.More() {
			if valueErr := r02OwnerDecisionReadJSONValueV1(decoder); valueErr != nil {
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

// r02OwnerDecisionRequireEOFV1 要求解码器在一个 JSON 值后立即结束。
func r02OwnerDecisionRequireEOFV1(decoder *json.Decoder) error {
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("存在尾随 JSON token %v", token)
	}
	return nil
}

// r02OwnerDecisionRepoRootV1 从当前测试源文件稳定定位多 Module 仓库根目录。
func r02OwnerDecisionRepoRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 R02 Owner request 测试源文件")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

// r02OwnerDecisionVerifyArtifactRefV1 校验仓库相对路径和原始字节摘要，防止请求引用漂移。
func r02OwnerDecisionVerifyArtifactRefV1(t *testing.T, repoRoot string, ref r02OwnerDecisionArtifactRefV1, wantPath string) {
	t.Helper()
	if ref.Path != wantPath || path.Clean(ref.Path) != ref.Path || path.IsAbs(ref.Path) || strings.HasPrefix(ref.Path, "../") {
		t.Fatalf("R02 Owner request artifact path=%q want=%q", ref.Path, wantPath)
	}
	if !strings.HasPrefix(ref.SHA256, "sha256:") || len(ref.SHA256) != len("sha256:")+sha256.Size*2 {
		t.Fatalf("%s SHA-256 格式非法: %q", ref.Path, ref.SHA256)
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(ref.Path)))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	got := "sha256:" + hex.EncodeToString(digest[:])
	if got != ref.SHA256 {
		t.Fatalf("%s raw SHA-256=%s want=%s", ref.Path, got, ref.SHA256)
	}
}

// r02OwnerDecisionVerifyLiveGateV1 校验待决请求没有绕过当前 R02 expansion freeze 和 blocker exact-set。
func r02OwnerDecisionVerifyLiveGateV1(t *testing.T, repoRoot string, manifestPath string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(manifestPath)))
	if err != nil {
		t.Fatal(err)
	}
	var manifest r02ReviewFreezeManifestProjectionV1
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, gate := range manifest.Gates {
		if gate.Gate != "W2-R02" {
			continue
		}
		if gate.Status != "expansion_frozen" || string(gate.Freeze) != "null" || len(gate.CandidateEvidence) != 0 {
			t.Fatalf("R02 live Gate 不再是失败关闭状态: %+v", gate)
		}
		codes := make([]string, len(gate.Blockers))
		for index, blocker := range gate.Blockers {
			codes[index] = blocker.Code
		}
		want := []string{"W2_R02_AGGREGATE_MANIFEST_MISSING", "W2_R02_OWNER_APPROVAL_MISSING"}
		if !reflect.DeepEqual(codes, want) {
			t.Fatalf("R02 live blocker codes=%v want=%v", codes, want)
		}
		return
	}
	t.Fatal("Review Freeze manifest 缺少 W2-R02")
}

// r02OwnerDecisionValidateSortedRolesV1 校验候选 Owner role 使用排序、唯一且稳定的 role key。
func r02OwnerDecisionValidateSortedRolesV1(roles []string) error {
	if len(roles) == 0 || !sort.StringsAreSorted(roles) {
		return fmt.Errorf("role 集必须非空且排序: %v", roles)
	}
	for index, role := range roles {
		if !r02OwnerRolePatternV1.MatchString(role) {
			return fmt.Errorf("role key 非法: %q", role)
		}
		if index > 0 && roles[index-1] == role {
			return fmt.Errorf("role key 重复: %q", role)
		}
	}
	return nil
}

// r02OwnerDecisionCandidateRolesV1 返回矩阵中逐项 Owner 候选的排序 exact-set。
// 这些集合仅用于发起语义评审，不能替代批准范围反推的最终 required_owner_roles。
func r02OwnerDecisionCandidateRolesV1() [][]string {
	return [][]string{
		{"agent_owner", "data_owner", "security_owner", "test_owner"},
		{"agent_owner", "data_owner", "operations_owner", "security_owner", "test_owner"},
		{"agent_owner", "business_owner", "product_owner", "security_owner", "test_owner"},
		{"agent_owner", "data_owner", "operations_owner", "security_owner"},
		{"agent_owner", "data_owner", "operations_owner", "test_owner"},
		{"agent_owner", "data_owner", "operations_owner", "security_owner", "test_owner"},
		{"agent_owner", "data_owner", "operations_owner", "security_owner", "test_owner"},
		{"agent_owner", "data_owner", "operations_owner", "security_owner", "test_owner"},
		{"agent_owner", "data_owner", "operations_owner", "security_owner", "test_owner"},
		{"agent_owner", "data_owner", "operations_owner", "security_owner", "test_owner"},
		{"agent_owner", "operations_owner", "security_owner", "test_owner"},
		{"agent_owner", "frontend_owner", "operations_owner", "security_owner", "test_owner"},
		{"agent_owner", "business_owner", "frontend_owner", "security_owner", "test_owner"},
		{"agent_owner", "frontend_owner", "operations_owner", "security_owner", "test_owner"},
		{"agent_owner", "operations_owner", "security_owner", "test_owner", "worker_owner"},
		{"agent_owner", "data_owner", "operations_owner", "security_owner", "test_owner"},
		{"agent_owner", "data_owner", "product_owner", "security_owner", "test_owner"},
		{"agent_owner", "business_owner", "finance_owner", "operations_owner", "product_owner", "security_owner", "test_owner"},
		{"agent_owner", "business_owner", "data_owner", "finance_owner", "operations_owner", "product_owner", "security_owner", "test_owner"},
	}
}
