package smokegovernance_test

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	smokeGovernanceManifestPathV1 = "docs/design/testing/approvals/w2-s0-g0/approval-manifest.json"
	smokeGovernanceWorkflowPathV1 = ".github/workflows/w2-smoke-governance.yml"
	smokeGovernanceTrustRootV1    = "agent/tests/smokegovernance/"

	smokeGovernanceManifestSchemaV1  = "w2_s0_g0_approval_manifest.v1"
	smokeGovernanceBaselineSchemaV1  = "w2_s0_g0_smk_004a_shadow_baseline.v1"
	smokeGovernanceGateIDV1          = "W2-S0-G0"
	smokeGovernanceAwaitingV1        = "awaiting_owner_approval"
	smokeGovernanceTrustRootStatusV1 = "candidate_unactivated"
	smokeGovernanceShadowModeV1      = "shadow_parity"
	smokeGovernanceBlockCodeV1       = "W2_S0_G0_AWAITING_OWNER_APPROVAL"
	smokeGovernanceBlockStatementV1  = "W2-S0-G0 尚未取得七方 Owner 联合批准；禁止创建 smoke/**、test-adapters/** 或 deploy/local-smoke/**。"

	smokeGovernanceADRPathV1      = "docs/design/cross-module/w2-adr-009-structured-smoke-harness-v1.md"
	smokeGovernanceContractPathV1 = "docs/design/testing/w2-smoke-context-registry-contract-v1.md"
	smokeGovernanceBaselinePathV1 = "docs/design/testing/approvals/w2-s0-g0/smk-004a-shadow-baseline-v1.json"

	smokeGovernanceAPISourcePathV1 = "scripts/smoke-w0-transport.sh"
	smokeGovernanceUISourcePathV1  = "frontend/e2e/w0-transport.spec.js"
)

// smokeGovernanceManifestV1 描述 W2-S0-G0 在 Owner 联合批准前唯一允许的机器状态。
type smokeGovernanceManifestV1 struct {
	SchemaVersion          string                         `json:"schema_version"`
	GateID                 string                         `json:"gate_id"`
	Status                 string                         `json:"status"`
	ImplementationUnlocked bool                           `json:"implementation_unlocked"`
	TrustRootStatus        string                         `json:"trust_root_status"`
	ActivationBlockers     []string                       `json:"activation_blockers"`
	RequiredOwnerRoles     []string                       `json:"required_owner_roles"`
	ArtifactRefs           []smokeGovernanceArtifactRefV1 `json:"artifact_refs"`
	ForbiddenPaths         []string                       `json:"forbidden_paths"`
	BlockCode              string                         `json:"block_code"`
	BlockStatement         string                         `json:"block_statement"`
}

// smokeGovernanceArtifactRefV1 将待审 ADR、契约和 shadow baseline 绑定到原始字节摘要。
type smokeGovernanceArtifactRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind"`
}

// smokeGovernanceShadowBaselineV1 冻结 SMK-004A 的 API/UI shadow parity 边界及 canonical-only 排除项。
type smokeGovernanceShadowBaselineV1 struct {
	SchemaVersion             string                           `json:"schema_version"`
	SliceID                   string                           `json:"slice_id"`
	CanonicalSmokeID          string                           `json:"canonical_smoke_id"`
	ScenarioKind              string                           `json:"scenario_kind"`
	Mode                      string                           `json:"mode"`
	ContributesToStatus       bool                             `json:"contributes_to_status"`
	SourceRefs                []smokeGovernanceSourceRefV1     `json:"source_refs"`
	Profiles                  []smokeGovernanceShadowProfileV1 `json:"profiles"`
	CanonicalOnlyAssertionIDs []string                         `json:"canonical_only_assertion_ids"`
}

// smokeGovernanceSourceRefV1 固定既有 Shell/Playwright 权威路径、Git mode、行数和原始摘要。
type smokeGovernanceSourceRefV1 struct {
	Profile   string `json:"profile"`
	Path      string `json:"path"`
	GitMode   string `json:"git_mode"`
	LineCount int    `json:"line_count"`
	SHA256    string `json:"sha256"`
}

// smokeGovernanceShadowProfileV1 固定单个 profile 的非贡献模式和 assertion exact-set。
type smokeGovernanceShadowProfileV1 struct {
	Profile             string   `json:"profile"`
	Mode                string   `json:"mode"`
	ContributesToStatus bool     `json:"contributes_to_status"`
	AssertionIDs        []string `json:"assertion_ids"`
}

// smokeGovernanceArtifactV1 是从 worktree 或 Git tree 读取的原始治理输入及其 Git mode。
type smokeGovernanceArtifactV1 struct {
	Raw  []byte
	Mode string
}

// smokeGovernanceArtifactLoaderV1 只按仓库相对路径读取被 manifest 直接绑定的原始字节。
type smokeGovernanceArtifactLoaderV1 func(relative string) (smokeGovernanceArtifactV1, error)

// smokeGovernanceExpectedOwnerRolesV1 返回七方 Owner 的排序 exact-set，调用方不得修改共享全局切片。
func smokeGovernanceExpectedOwnerRolesV1() []string {
	return []string{
		"agent_owner",
		"business_owner",
		"frontend_owner",
		"operations_owner",
		"security_owner",
		"test_owner",
		"worker_owner",
	}
}

// smokeGovernanceExpectedActivationBlockersV1 固定 workflow/Ruleset 激活前必须显式关闭的排序 exact-set。
func smokeGovernanceExpectedActivationBlockersV1() []string {
	return []string{
		"BASE_OWNED_WORKFLOW_NOT_ACTIVE",
		"FORK_CANARY_NOT_PASSED",
		"OWNER_AUTHORITY_NOT_ACTIVE",
		"RULESET_SOURCE_AND_NO_BYPASS_NOT_PROVEN",
		"SAME_REPO_CANARY_NOT_PASSED",
		"SEMANTIC_PATH_POLICY_NOT_ACTIVE",
		"TRUST_ROOT_REKEY_HANDOFF_NOT_FROZEN",
		"TRUST_ROOT_RELEASE_NOT_INSTALLED",
		"VALIDATOR_BUILD_CLOSURE_NOT_FROZEN",
		"WORKFLOW_DIGEST_AND_ACTION_SHA_NOT_FROZEN",
	}
}

// smokeGovernanceExpectedArtifactRefsV1 返回 approval manifest 必须逐项绑定的三个待审产物。
func smokeGovernanceExpectedArtifactRefsV1() []smokeGovernanceArtifactRefV1 {
	return []smokeGovernanceArtifactRefV1{
		{Path: smokeGovernanceADRPathV1, Kind: "architecture_decision"},
		{Path: smokeGovernanceBaselinePathV1, Kind: "shadow_baseline"},
		{Path: smokeGovernanceContractPathV1, Kind: "context_registry_contract"},
	}
}

// smokeGovernanceExpectedForbiddenPathsV1 返回未批准状态禁止出现的 canonical root exact-set。
func smokeGovernanceExpectedForbiddenPathsV1() []string {
	return []string{"deploy/local-smoke/**", "smoke/**", "test-adapters/**"}
}

// smokeGovernanceExpectedAPIAssertionsV1 返回 API shadow profile 的 14 项排序 exact-set。
func smokeGovernanceExpectedAPIAssertionsV1() []string {
	return []string{
		"agent_direct_access_denied",
		"agent_restart_hit",
		"events_cross_owner_not_found",
		"retention_old_events_pruned",
		"retention_server_cursor_expired_reset",
		"retention_window_advanced",
		"snapshot_after_restart",
		"sse_after_restart",
		"sse_cursor_reset",
		"sse_replay_and_ready",
		"workspace_cross_owner_not_found",
		"workspace_empty_arrays",
		"workspace_owner_safe_not_found",
		"workspace_snapshot",
	}
}

// smokeGovernanceExpectedUIAssertionsV1 返回 UI shadow profile 的 12 项排序 exact-set。
func smokeGovernanceExpectedUIAssertionsV1() []string {
	return []string{
		"browser_controlled_disconnect",
		"browser_cross_owner_agent_blocked",
		"browser_cross_owner_not_found",
		"browser_resource_facts_not_disclosed",
		"browser_retention_no_stale_event_replayed",
		"browser_retention_reset_received",
		"browser_retention_reset_without_id",
		"browser_retention_same_session_recovery",
		"browser_retention_snapshot_reloaded",
		"browser_retention_snapshot_retained",
		"browser_same_session_recovery",
		"browser_ui",
	}
}

// smokeGovernanceExpectedCanonicalOnlyAssertionsV1 返回只能留在既有 canonical Evidence、不得被首切 shadow 冒充的八项断言。
func smokeGovernanceExpectedCanonicalOnlyAssertionsV1() []string {
	return []string{
		"agent_unique_facts",
		"blank_negative_side_effects",
		"business_prompt_cleared",
		"concurrent_requests",
		"idempotency_conflict",
		"idempotent_replay",
		"logout_revoked",
		"logout_workspace_denied",
	}
}

// smokeGovernanceValidateManifestV1 校验 pre-approval manifest、三个摘要绑定产物和 SMK-004A baseline。
func smokeGovernanceValidateManifestV1(manifest smokeGovernanceManifestV1, loader smokeGovernanceArtifactLoaderV1) error {
	if manifest.SchemaVersion != smokeGovernanceManifestSchemaV1 || manifest.GateID != smokeGovernanceGateIDV1 {
		return fmt.Errorf("manifest identity 非法 schema=%q gate=%q", manifest.SchemaVersion, manifest.GateID)
	}
	// v1 信任根只表达未批准状态；批准/解锁必须由后续经过审核的版本化治理迁移承接，不能自报完成。
	if manifest.Status != smokeGovernanceAwaitingV1 || manifest.ImplementationUnlocked {
		return fmt.Errorf("manifest 必须保持 awaiting/locked status=%q unlocked=%t", manifest.Status, manifest.ImplementationUnlocked)
	}
	if manifest.TrustRootStatus != smokeGovernanceTrustRootStatusV1 || !reflect.DeepEqual(manifest.ActivationBlockers, smokeGovernanceExpectedActivationBlockersV1()) {
		return fmt.Errorf("manifest trust root 必须保持未激活且 blockers 为 exact-set status=%q blockers=%v", manifest.TrustRootStatus, manifest.ActivationBlockers)
	}
	if !reflect.DeepEqual(manifest.RequiredOwnerRoles, smokeGovernanceExpectedOwnerRolesV1()) {
		return fmt.Errorf("required_owner_roles 非七方排序 exact-set: %v", manifest.RequiredOwnerRoles)
	}
	if !reflect.DeepEqual(manifest.ForbiddenPaths, smokeGovernanceExpectedForbiddenPathsV1()) {
		return fmt.Errorf("forbidden_paths 不能弱化或扩写: %v", manifest.ForbiddenPaths)
	}
	if manifest.BlockCode != smokeGovernanceBlockCodeV1 || manifest.BlockStatement != smokeGovernanceBlockStatementV1 {
		return fmt.Errorf("approval block 发生漂移 code=%q statement=%q", manifest.BlockCode, manifest.BlockStatement)
	}

	expectedRefs := smokeGovernanceExpectedArtifactRefsV1()
	if len(manifest.ArtifactRefs) != len(expectedRefs) {
		return fmt.Errorf("artifact_refs=%d want=%d", len(manifest.ArtifactRefs), len(expectedRefs))
	}
	var baselineRaw []byte
	for index, expected := range expectedRefs {
		actual := manifest.ArtifactRefs[index]
		if actual.Path != expected.Path || actual.Kind != expected.Kind {
			return fmt.Errorf("artifact_refs[%d] shape=%+v want path=%q kind=%q", index, actual, expected.Path, expected.Kind)
		}
		if err := smokeGovernanceValidateCanonicalGitPathV1(actual.Path); err != nil {
			return fmt.Errorf("artifact_refs[%d] path: %w", index, err)
		}
		artifact, err := loader(actual.Path)
		if err != nil {
			return fmt.Errorf("读取 artifact %q: %w", actual.Path, err)
		}
		if artifact.Mode != "100644" {
			return fmt.Errorf("artifact %q 必须是 100644 blob，mode=%q", actual.Path, artifact.Mode)
		}
		if len(bytes.TrimSpace(artifact.Raw)) == 0 {
			return fmt.Errorf("artifact %q 不能为空", actual.Path)
		}
		if err := smokeGovernanceValidateSHA256V1(actual.SHA256, artifact.Raw); err != nil {
			return fmt.Errorf("artifact %q: %w", actual.Path, err)
		}
		if actual.Path == smokeGovernanceBaselinePathV1 {
			baselineRaw = artifact.Raw
		}
	}
	var baseline smokeGovernanceShadowBaselineV1
	if err := smokeGovernanceStrictDecodeV1(baselineRaw, &baseline); err != nil {
		return fmt.Errorf("shadow baseline strict JSON: %w", err)
	}
	return smokeGovernanceValidateShadowBaselineV1(baseline, loader)
}

// smokeGovernanceValidateShadowBaselineV1 固定首切只做 shadow parity，不贡献 canonical 状态。
func smokeGovernanceValidateShadowBaselineV1(baseline smokeGovernanceShadowBaselineV1, loader smokeGovernanceArtifactLoaderV1) error {
	if baseline.SchemaVersion != smokeGovernanceBaselineSchemaV1 || baseline.SliceID != "SMK-004A" || baseline.CanonicalSmokeID != "SMK-004" || baseline.ScenarioKind != "derived_slice" {
		return fmt.Errorf("shadow baseline identity 非法 schema=%q slice=%q canonical=%q kind=%q", baseline.SchemaVersion, baseline.SliceID, baseline.CanonicalSmokeID, baseline.ScenarioKind)
	}
	if baseline.Mode != smokeGovernanceShadowModeV1 || baseline.ContributesToStatus {
		return fmt.Errorf("shadow baseline 不得贡献 canonical status mode=%q contributes=%t", baseline.Mode, baseline.ContributesToStatus)
	}
	expectedSources := []smokeGovernanceSourceRefV1{
		{Profile: "api", Path: smokeGovernanceAPISourcePathV1, GitMode: "100755", LineCount: 4457},
		{Profile: "ui", Path: smokeGovernanceUISourcePathV1, GitMode: "100644", LineCount: 567},
	}
	if len(baseline.SourceRefs) != len(expectedSources) {
		return fmt.Errorf("source_refs=%d want=%d", len(baseline.SourceRefs), len(expectedSources))
	}
	for index, expected := range expectedSources {
		actual := baseline.SourceRefs[index]
		if actual.Profile != expected.Profile || actual.Path != expected.Path || actual.GitMode != expected.GitMode || actual.LineCount != expected.LineCount {
			return fmt.Errorf("source_refs[%d] shape=%+v want=%+v", index, actual, expected)
		}
		if err := smokeGovernanceValidateCanonicalGitPathV1(actual.Path); err != nil {
			return fmt.Errorf("source_refs[%d] path: %w", index, err)
		}
		artifact, err := loader(actual.Path)
		if err != nil {
			return fmt.Errorf("读取 shadow source %q: %w", actual.Path, err)
		}
		if artifact.Mode != actual.GitMode {
			return fmt.Errorf("shadow source %q mode=%q want=%q", actual.Path, artifact.Mode, actual.GitMode)
		}
		if smokeGovernanceLineCountV1(artifact.Raw) != actual.LineCount {
			return fmt.Errorf("shadow source %q lines=%d want=%d", actual.Path, smokeGovernanceLineCountV1(artifact.Raw), actual.LineCount)
		}
		if err := smokeGovernanceValidateSHA256V1(actual.SHA256, artifact.Raw); err != nil {
			return fmt.Errorf("shadow source %q: %w", actual.Path, err)
		}
	}

	expectedProfiles := []smokeGovernanceShadowProfileV1{
		{Profile: "api", Mode: smokeGovernanceShadowModeV1, AssertionIDs: smokeGovernanceExpectedAPIAssertionsV1()},
		{Profile: "ui", Mode: smokeGovernanceShadowModeV1, AssertionIDs: smokeGovernanceExpectedUIAssertionsV1()},
	}
	if len(baseline.Profiles) != len(expectedProfiles) {
		return fmt.Errorf("profiles=%d want=%d", len(baseline.Profiles), len(expectedProfiles))
	}
	shadowAssertions := make(map[string]struct{})
	for index, expected := range expectedProfiles {
		actual := baseline.Profiles[index]
		if actual.Profile != expected.Profile || actual.Mode != expected.Mode || actual.ContributesToStatus || !reflect.DeepEqual(actual.AssertionIDs, expected.AssertionIDs) {
			return fmt.Errorf("profiles[%d] 非 shadow exact-set: %+v", index, actual)
		}
		for _, assertionID := range actual.AssertionIDs {
			if _, exists := shadowAssertions[assertionID]; exists {
				return fmt.Errorf("shadow assertion 重复=%q", assertionID)
			}
			shadowAssertions[assertionID] = struct{}{}
		}
	}
	if len(shadowAssertions) != 26 {
		return fmt.Errorf("shadow assertion count=%d want=26", len(shadowAssertions))
	}
	if !reflect.DeepEqual(baseline.CanonicalOnlyAssertionIDs, smokeGovernanceExpectedCanonicalOnlyAssertionsV1()) {
		return fmt.Errorf("canonical_only_assertion_ids 非八项 exact-set: %v", baseline.CanonicalOnlyAssertionIDs)
	}
	for _, assertionID := range baseline.CanonicalOnlyAssertionIDs {
		if _, exists := shadowAssertions[assertionID]; exists {
			return fmt.Errorf("canonical-only assertion 被 shadow 冒充=%q", assertionID)
		}
	}
	return nil
}

// smokeGovernanceValidateCanonicalGitPathV1 只接受 Git 使用 `/` 表达的规范化 UTF-8 仓库相对路径。
func smokeGovernanceValidateCanonicalGitPathV1(relative string) error {
	if relative == "" || !utf8.ValidString(relative) || strings.ContainsRune(relative, '\x00') || strings.Contains(relative, "\\") {
		return fmt.Errorf("非 canonical Git path=%q", relative)
	}
	if strings.HasPrefix(relative, "/") || strings.HasSuffix(relative, "/") || path.Clean(relative) != relative {
		return fmt.Errorf("非 canonical Git path=%q", relative)
	}
	for _, component := range strings.Split(relative, "/") {
		if component == "" || component == "." || component == ".." || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") || strings.Contains(component, ":") || strings.IndexFunc(component, func(r rune) bool {
			return r < 0x20 || r == 0x7f || r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069)
		}) >= 0 {
			return fmt.Errorf("非 canonical Git path component=%q", component)
		}
	}
	return nil
}

// smokeGovernanceValidateSHA256V1 使用固定时比较 manifest 摘要与原始字节。
func smokeGovernanceValidateSHA256V1(encoded string, raw []byte) error {
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(encoded) {
		return fmt.Errorf("sha256 格式非法=%q", encoded)
	}
	want, err := hex.DecodeString(strings.TrimPrefix(encoded, "sha256:"))
	if err != nil {
		return err
	}
	actual := sha256.Sum256(raw)
	if subtle.ConstantTimeCompare(want, actual[:]) != 1 {
		return fmt.Errorf("sha256 不匹配")
	}
	return nil
}

// smokeGovernanceSHA256V1 返回治理清单统一使用的带算法前缀摘要。
func smokeGovernanceSHA256V1(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// smokeGovernanceLineCountV1 使用逻辑文本行计数；空文件为零，末行无换行时仍计一行。
func smokeGovernanceLineCountV1(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	count := bytes.Count(raw, []byte{'\n'})
	if raw[len(raw)-1] != '\n' {
		count++
	}
	return count
}

// smokeGovernanceSyntheticBundleV1 是测试内自包含的待审清单、baseline 与绑定文件集合。
type smokeGovernanceSyntheticBundleV1 struct {
	Manifest  smokeGovernanceManifestV1
	Baseline  smokeGovernanceShadowBaselineV1
	Artifacts map[string]smokeGovernanceArtifactV1
}

// smokeGovernanceNewSyntheticBundleV1 构造不依赖尚未落库 docs/manifest 的有效候选基线。
func smokeGovernanceNewSyntheticBundleV1(t *testing.T) smokeGovernanceSyntheticBundleV1 {
	t.Helper()
	apiRaw := bytes.Repeat([]byte("api-source-line\n"), 4457)
	uiRaw := bytes.Repeat([]byte("ui-source-line\n"), 567)
	baseline := smokeGovernanceShadowBaselineV1{
		SchemaVersion:       smokeGovernanceBaselineSchemaV1,
		SliceID:             "SMK-004A",
		CanonicalSmokeID:    "SMK-004",
		ScenarioKind:        "derived_slice",
		Mode:                smokeGovernanceShadowModeV1,
		ContributesToStatus: false,
		SourceRefs: []smokeGovernanceSourceRefV1{
			{Profile: "api", Path: smokeGovernanceAPISourcePathV1, GitMode: "100755", LineCount: 4457, SHA256: smokeGovernanceSHA256V1(apiRaw)},
			{Profile: "ui", Path: smokeGovernanceUISourcePathV1, GitMode: "100644", LineCount: 567, SHA256: smokeGovernanceSHA256V1(uiRaw)},
		},
		Profiles: []smokeGovernanceShadowProfileV1{
			{Profile: "api", Mode: smokeGovernanceShadowModeV1, ContributesToStatus: false, AssertionIDs: smokeGovernanceExpectedAPIAssertionsV1()},
			{Profile: "ui", Mode: smokeGovernanceShadowModeV1, ContributesToStatus: false, AssertionIDs: smokeGovernanceExpectedUIAssertionsV1()},
		},
		CanonicalOnlyAssertionIDs: smokeGovernanceExpectedCanonicalOnlyAssertionsV1(),
	}
	baselineRaw, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		t.Fatalf("marshal synthetic baseline: %v", err)
	}
	baselineRaw = append(baselineRaw, '\n')
	artifacts := map[string]smokeGovernanceArtifactV1{
		smokeGovernanceADRPathV1:       {Raw: []byte("# W2-ADR-009 structured smoke harness candidate\n"), Mode: "100644"},
		smokeGovernanceContractPathV1:  {Raw: []byte("# W2 smoke context and registry contract candidate\n"), Mode: "100644"},
		smokeGovernanceBaselinePathV1:  {Raw: baselineRaw, Mode: "100644"},
		smokeGovernanceAPISourcePathV1: {Raw: apiRaw, Mode: "100755"},
		smokeGovernanceUISourcePathV1:  {Raw: uiRaw, Mode: "100644"},
	}
	manifest := smokeGovernanceManifestV1{
		SchemaVersion:          smokeGovernanceManifestSchemaV1,
		GateID:                 smokeGovernanceGateIDV1,
		Status:                 smokeGovernanceAwaitingV1,
		ImplementationUnlocked: false,
		TrustRootStatus:        smokeGovernanceTrustRootStatusV1,
		ActivationBlockers:     smokeGovernanceExpectedActivationBlockersV1(),
		RequiredOwnerRoles:     smokeGovernanceExpectedOwnerRolesV1(),
		ArtifactRefs: []smokeGovernanceArtifactRefV1{
			{Path: smokeGovernanceADRPathV1, SHA256: smokeGovernanceSHA256V1(artifacts[smokeGovernanceADRPathV1].Raw), Kind: "architecture_decision"},
			{Path: smokeGovernanceBaselinePathV1, SHA256: smokeGovernanceSHA256V1(baselineRaw), Kind: "shadow_baseline"},
			{Path: smokeGovernanceContractPathV1, SHA256: smokeGovernanceSHA256V1(artifacts[smokeGovernanceContractPathV1].Raw), Kind: "context_registry_contract"},
		},
		ForbiddenPaths: smokeGovernanceExpectedForbiddenPathsV1(),
		BlockCode:      smokeGovernanceBlockCodeV1,
		BlockStatement: smokeGovernanceBlockStatementV1,
	}
	return smokeGovernanceSyntheticBundleV1{Manifest: manifest, Baseline: baseline, Artifacts: artifacts}
}

// smokeGovernanceSyntheticLoaderV1 将测试 bundle 暴露为只读 artifact loader。
func smokeGovernanceSyntheticLoaderV1(bundle smokeGovernanceSyntheticBundleV1) smokeGovernanceArtifactLoaderV1 {
	return func(relative string) (smokeGovernanceArtifactV1, error) {
		artifact, ok := bundle.Artifacts[relative]
		if !ok {
			return smokeGovernanceArtifactV1{}, fmt.Errorf("artifact not found=%q", relative)
		}
		return artifact, nil
	}
}

// smokeGovernanceRepositoryRootV1 从独立 Agent Module 测试目录向上定位仓库根，不依赖根 go.work 参与构建。
func smokeGovernanceRepositoryRootV1(t *testing.T) string {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if info, statErr := os.Stat(filepath.Join(current, "agent", "go.mod")); statErr == nil && info.Mode().IsRegular() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("无法定位 Dora 仓库根")
		}
		current = parent
	}
}

// smokeGovernanceWorktreeLoaderV1 读取当前候选原始字节，并把普通/可执行文件投影为 Git blob mode。
func smokeGovernanceWorktreeLoaderV1(repositoryRoot string) smokeGovernanceArtifactLoaderV1 {
	return func(relative string) (smokeGovernanceArtifactV1, error) {
		if err := smokeGovernanceValidateCanonicalGitPathV1(relative); err != nil {
			return smokeGovernanceArtifactV1{}, err
		}
		absolute := filepath.Join(repositoryRoot, filepath.FromSlash(relative))
		info, err := os.Lstat(absolute)
		if err != nil {
			return smokeGovernanceArtifactV1{}, err
		}
		if !info.Mode().IsRegular() {
			return smokeGovernanceArtifactV1{}, fmt.Errorf("artifact 不是普通文件=%q mode=%s", relative, info.Mode())
		}
		raw, err := os.ReadFile(absolute)
		if err != nil {
			return smokeGovernanceArtifactV1{}, err
		}
		mode := "100644"
		if info.Mode().Perm()&0o111 != 0 {
			mode = "100755"
		}
		return smokeGovernanceArtifactV1{Raw: raw, Mode: mode}, nil
	}
}

// TestW2S0G0ManifestV1CurrentRepository 绑定真实 ADR、契约、baseline 与既有 Shell/Playwright 权威源，而非只验证 synthetic fixture。
func TestW2S0G0ManifestV1CurrentRepository(t *testing.T) {
	loader := smokeGovernanceWorktreeLoaderV1(smokeGovernanceRepositoryRootV1(t))
	manifestArtifact, err := loader(smokeGovernanceManifestPathV1)
	if err != nil {
		t.Fatalf("读取真实 W2-S0-G0 manifest: %v", err)
	}
	if manifestArtifact.Mode != "100644" {
		t.Fatalf("真实 W2-S0-G0 manifest mode=%s want=100644", manifestArtifact.Mode)
	}
	var manifest smokeGovernanceManifestV1
	if err := smokeGovernanceStrictDecodeV1(manifestArtifact.Raw, &manifest); err != nil {
		t.Fatalf("真实 W2-S0-G0 manifest strict JSON: %v", err)
	}
	if err := smokeGovernanceValidateManifestV1(manifest, loader); err != nil {
		t.Fatalf("真实 W2-S0-G0 candidate 非法: %v", err)
	}
}

// TestW2S0G0ManifestV1Candidate 验证自包含候选严格保持 awaiting、locked、七角色和 SMK-004A shadow 边界。
func TestW2S0G0ManifestV1Candidate(t *testing.T) {
	bundle := smokeGovernanceNewSyntheticBundleV1(t)
	if err := smokeGovernanceValidateManifestV1(bundle.Manifest, smokeGovernanceSyntheticLoaderV1(bundle)); err != nil {
		t.Fatalf("valid W2-S0-G0 candidate rejected: %v", err)
	}
}

// TestW2S0G0ManifestV1RejectsGateWeakening 覆盖状态自报、Owner 集合、路径阻断和摘要绑定的失败关闭。
func TestW2S0G0ManifestV1RejectsGateWeakening(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*smokeGovernanceSyntheticBundleV1)
	}{
		{name: "approved self report", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) { bundle.Manifest.Status = "approved" }},
		{name: "implementation unlocked", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) { bundle.Manifest.ImplementationUnlocked = true }},
		{name: "trust root self activated", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) { bundle.Manifest.TrustRootStatus = "active" }},
		{name: "activation blocker removed", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Manifest.ActivationBlockers = bundle.Manifest.ActivationBlockers[1:]
		}},
		{name: "owner missing", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Manifest.RequiredOwnerRoles = bundle.Manifest.RequiredOwnerRoles[1:]
		}},
		{name: "owner extra", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Manifest.RequiredOwnerRoles = append(bundle.Manifest.RequiredOwnerRoles, "product_owner")
		}},
		{name: "owner reordered", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Manifest.RequiredOwnerRoles[0], bundle.Manifest.RequiredOwnerRoles[1] = bundle.Manifest.RequiredOwnerRoles[1], bundle.Manifest.RequiredOwnerRoles[0]
		}},
		{name: "forbidden path removed", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Manifest.ForbiddenPaths = bundle.Manifest.ForbiddenPaths[1:]
		}},
		{name: "block weakened", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) { bundle.Manifest.BlockStatement = "等待审批" }},
		{name: "artifact removed", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Manifest.ArtifactRefs = bundle.Manifest.ArtifactRefs[1:]
		}},
		{name: "artifact reordered", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Manifest.ArtifactRefs[0], bundle.Manifest.ArtifactRefs[1] = bundle.Manifest.ArtifactRefs[1], bundle.Manifest.ArtifactRefs[0]
		}},
		{name: "artifact digest drift", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Manifest.ArtifactRefs[0].SHA256 = "sha256:" + strings.Repeat("0", 64)
		}},
		{name: "artifact executable", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			artifact := bundle.Artifacts[smokeGovernanceADRPathV1]
			artifact.Mode = "100755"
			bundle.Artifacts[smokeGovernanceADRPathV1] = artifact
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := smokeGovernanceNewSyntheticBundleV1(t)
			tc.mutate(&bundle)
			if err := smokeGovernanceValidateManifestV1(bundle.Manifest, smokeGovernanceSyntheticLoaderV1(bundle)); err == nil {
				t.Fatal("weakened governance manifest was accepted")
			}
		})
	}
}

// TestW2S0G0ShadowBaselineV1RejectsParityInflation 防止 shadow profile 冒充 canonical 通过或漂移既有权威来源。
func TestW2S0G0ShadowBaselineV1RejectsParityInflation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*smokeGovernanceSyntheticBundleV1)
	}{
		{name: "root contributes", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) { bundle.Baseline.ContributesToStatus = true }},
		{name: "profile contributes", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) { bundle.Baseline.Profiles[0].ContributesToStatus = true }},
		{name: "profile mode promoted", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) { bundle.Baseline.Profiles[1].Mode = "canonical" }},
		{name: "api assertion removed", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Baseline.Profiles[0].AssertionIDs = bundle.Baseline.Profiles[0].AssertionIDs[1:]
		}},
		{name: "ui assertion added", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Baseline.Profiles[1].AssertionIDs = append(bundle.Baseline.Profiles[1].AssertionIDs, "canonical_inflation")
		}},
		{name: "canonical only removed", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			bundle.Baseline.CanonicalOnlyAssertionIDs = bundle.Baseline.CanonicalOnlyAssertionIDs[1:]
		}},
		{name: "source line drift", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) { bundle.Baseline.SourceRefs[0].LineCount-- }},
		{name: "source mode drift", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) { bundle.Baseline.SourceRefs[0].GitMode = "100644" }},
		{name: "source content drift", mutate: func(bundle *smokeGovernanceSyntheticBundleV1) {
			artifact := bundle.Artifacts[smokeGovernanceAPISourcePathV1]
			artifact.Raw = append(artifact.Raw, []byte("drift\n")...)
			bundle.Artifacts[smokeGovernanceAPISourcePathV1] = artifact
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bundle := smokeGovernanceNewSyntheticBundleV1(t)
			tc.mutate(&bundle)
			if err := smokeGovernanceValidateShadowBaselineV1(bundle.Baseline, smokeGovernanceSyntheticLoaderV1(bundle)); err == nil {
				t.Fatal("inflated or drifted shadow baseline was accepted")
			}
		})
	}
}

// TestW2S0G0ManifestV1StrictJSON 固定 manifest 与 baseline 都拒绝重复、未知和尾随字段。
func TestW2S0G0ManifestV1StrictJSON(t *testing.T) {
	bundle := smokeGovernanceNewSyntheticBundleV1(t)
	manifestRaw, err := json.Marshal(bundle.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	baselineRaw := bundle.Artifacts[smokeGovernanceBaselinePathV1].Raw
	cases := map[string]struct {
		raw      []byte
		baseline bool
	}{
		"manifest unknown":         {raw: []byte(strings.Replace(string(manifestRaw), "{", `{"future":true,`, 1))},
		"manifest duplicate":       {raw: []byte(strings.Replace(string(manifestRaw), `"gate_id":"W2-S0-G0"`, `"gate_id":"W2-S0-G0","gate_id":"other"`, 1))},
		"manifest trailing":        {raw: append(append([]byte(nil), manifestRaw...), []byte(`{}`)...)},
		"manifest missing boolean": {raw: []byte(strings.Replace(string(manifestRaw), `"implementation_unlocked":false,`, "", 1))},
		"manifest null boolean":    {raw: []byte(strings.Replace(string(manifestRaw), `"implementation_unlocked":false`, `"implementation_unlocked":null`, 1))},
		"manifest self approvals":  {raw: []byte(strings.Replace(string(manifestRaw), `"forbidden_paths":`, `"owner_approval_refs":[],"forbidden_paths":`, 1))},
		"baseline unknown":         {raw: []byte(strings.Replace(string(baselineRaw), "{", `{"future":true,`, 1)), baseline: true},
		"baseline duplicate":       {raw: []byte(strings.Replace(string(baselineRaw), `"slice_id": "SMK-004A"`, `"slice_id": "SMK-004A", "slice_id": "other"`, 1)), baseline: true},
		"baseline trailing":        {raw: append(append([]byte(nil), baselineRaw...), []byte(`{}`)...), baseline: true},
		"baseline missing boolean": {raw: []byte(strings.Replace(string(baselineRaw), `  "contributes_to_status": false,`+"\n", "", 1)), baseline: true},
		"baseline null boolean":    {raw: []byte(strings.Replace(string(baselineRaw), `"contributes_to_status": false`, `"contributes_to_status": null`, 1)), baseline: true},
		"profile null boolean":     {raw: []byte(strings.Replace(string(baselineRaw), `      "contributes_to_status": false,`, `      "contributes_to_status": null,`, 1)), baseline: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if tc.baseline {
				var target smokeGovernanceShadowBaselineV1
				if err := smokeGovernanceStrictDecodeV1(tc.raw, &target); err == nil {
					t.Fatal("non-canonical baseline JSON was accepted")
				}
				return
			}
			var target smokeGovernanceManifestV1
			if err := smokeGovernanceStrictDecodeV1(tc.raw, &target); err == nil {
				t.Fatal("non-canonical manifest JSON was accepted")
			}
		})
	}
}
