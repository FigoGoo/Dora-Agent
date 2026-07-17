//go:build localsmoke

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/business/internal/assetanalysis"
)

const (
	testProjectID = "019b0000-0000-7000-8000-000000000001"
	testOwnerID   = "019b0000-0000-7000-8000-000000000002"
)

// fixtureRepositoryStub 是不访问外部服务的命令层测试替身。
type fixtureRepositoryStub struct {
	// authority 是 seed/authority 返回的安全权威结果。
	authority fixtureAuthority
	// err 是注入的 Repository 失败。
	err error
	// ensureCalls 记录 seed 分支调用次数。
	ensureCalls int
	// authorityCalls 记录 authority 分支调用次数。
	authorityCalls int
}

// Ensure 记录 seed 调用并返回注入结果。
func (stub *fixtureRepositoryStub) Ensure(_ context.Context, _ fixtureSeed) (fixtureAuthority, error) {
	stub.ensureCalls++
	return stub.authority, stub.err
}

// Authority 记录只读调用并返回注入结果。
func (stub *fixtureRepositoryStub) Authority(_ context.Context, _ fixtureSeed) (fixtureAuthority, error) {
	stub.authorityCalls++
	return stub.authority, stub.err
}

// TestFixtureConfigFromEnvironmentValidatesParameters 验证闭集模式、必填 UUIDv7 和显式标识均失败关闭。
func TestFixtureConfigFromEnvironmentValidatesParameters(t *testing.T) {
	environment := map[string]string{
		"DORA_SMOKE_ANALYZE_MATERIALS_MODE": fixtureModeSeed,
		"DORA_SMOKE_PROJECT_ID":             testProjectID,
		"DORA_SMOKE_OWNER_USER_ID":          testOwnerID,
	}
	getenv := func(key string) string { return environment[key] }
	config, err := fixtureConfigFromEnvironment(getenv)
	if err != nil {
		t.Fatalf("fixtureConfigFromEnvironment() error = %v", err)
	}
	if !assetanalysis.CanonicalUUIDv7(config.AssetID) || !assetanalysis.CanonicalUUIDv7(config.EvidenceID) ||
		config.AssetID == config.EvidenceID {
		t.Fatalf("derived IDs are invalid: %+v", config)
	}
	again, err := fixtureConfigFromEnvironment(getenv)
	if err != nil || again.AssetID != config.AssetID || again.EvidenceID != config.EvidenceID {
		t.Fatalf("derived IDs are not deterministic: again=%+v err=%v", again, err)
	}
	if _, err := fixtureConfigFromEnvironment(nil); !errors.Is(err, errInvalidFixtureInput) {
		t.Fatalf("nil getenv error = %v, want invalid input", err)
	}

	for name, mutate := range map[string]func(){
		"unknown mode":       func() { environment["DORA_SMOKE_ANALYZE_MATERIALS_MODE"] = "write" },
		"non UUIDv7 project": func() { environment["DORA_SMOKE_PROJECT_ID"] = "00000000-0000-4000-8000-000000000001" },
		"non UUIDv7 owner":   func() { environment["DORA_SMOKE_OWNER_USER_ID"] = "owner" },
		"same explicit IDs": func() {
			environment["DORA_SMOKE_ANALYZE_MATERIALS_ASSET_ID"] = testProjectID
			environment["DORA_SMOKE_ANALYZE_MATERIALS_EVIDENCE_ID"] = testProjectID
		},
	} {
		t.Run(name, func(t *testing.T) {
			original := make(map[string]string, len(environment))
			for key, value := range environment {
				original[key] = value
			}
			mutate()
			if _, err := fixtureConfigFromEnvironment(getenv); !errors.Is(err, errInvalidFixtureInput) {
				t.Fatalf("error = %v, want invalid input", err)
			}
			environment = original
			getenv = func(key string) string { return environment[key] }
		})
	}
}

// TestExecuteWritesExactSafeJSON 验证 stdout exact-set、嵌套字段闭集和敏感输入不回显。
func TestExecuteWritesExactSafeJSON(t *testing.T) {
	config, seed, authority := validFixtureTestData()
	stub := &fixtureRepositoryStub{authority: authority}
	var output bytes.Buffer
	if err := execute(context.Background(), config, stub, &output); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if stub.ensureCalls != 1 || stub.authorityCalls != 0 {
		t.Fatalf("repository calls ensure=%d authority=%d", stub.ensureCalls, stub.authorityCalls)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	assertExactJSONKeys(t, decoded, "schema_version", "mode", "asset_id", "evidence_id", "asset_version", "counts", "digests")
	var counts map[string]json.RawMessage
	if err := json.Unmarshal(decoded["counts"], &counts); err != nil {
		t.Fatalf("decode counts: %v", err)
	}
	assertExactJSONKeys(t, counts, "asset_count", "evidence_count", "creation_spec_count", "creation_spec_receipt_count")
	var digests map[string]json.RawMessage
	if err := json.Unmarshal(decoded["digests"], &digests); err != nil {
		t.Fatalf("decode digests: %v", err)
	}
	assertExactJSONKeys(t, digests, "content_sha256", "authority_sha256")
	var contract fixtureOutput
	if err := json.Unmarshal(output.Bytes(), &contract); err != nil {
		t.Fatalf("decode typed output: %v", err)
	}
	if contract.SchemaVersion != fixtureSchemaVersion || contract.Mode != fixtureModeSeed ||
		contract.AssetID != authority.AssetID || contract.EvidenceID != authority.EvidenceID ||
		contract.AssetVersion != authority.AssetVersion || contract.Counts != authority.Counts ||
		contract.Digests.ContentSHA256 != authority.ContentDigest || len(contract.Digests.AuthoritySHA256) != 64 {
		t.Fatalf("output contract mismatch: %+v", contract)
	}
	for _, forbidden := range []string{seed.ProjectID, seed.OwnerUserID, seed.Content, "postgres://", "BUSINESS_DATABASE_URL", "password"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("stdout leaked forbidden value %q: %s", forbidden, output.String())
		}
	}
}

// TestExecuteAuthorityUsesReadOnlyBranch 验证 authority 模式只调用只读 Repository 边界。
func TestExecuteAuthorityUsesReadOnlyBranch(t *testing.T) {
	config, _, authority := validFixtureTestData()
	config.Mode = fixtureModeAuthority
	stub := &fixtureRepositoryStub{authority: authority}
	if err := execute(context.Background(), config, stub, &bytes.Buffer{}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if stub.ensureCalls != 0 || stub.authorityCalls != 1 {
		t.Fatalf("repository calls ensure=%d authority=%d", stub.ensureCalls, stub.authorityCalls)
	}
}

// TestExecuteRejectsAuthorityDrift 验证额外素材、CreationSpec 副作用与 Evidence 漂移均不能生成成功 JSON。
func TestExecuteRejectsAuthorityDrift(t *testing.T) {
	config, _, valid := validFixtureTestData()
	tests := map[string]func(*fixtureAuthority){
		"extra asset":               func(authority *fixtureAuthority) { authority.Counts.Assets = 2 },
		"creation spec side effect": func(authority *fixtureAuthority) { authority.Counts.CreationSpecs = 1 },
		"receipt side effect":       func(authority *fixtureAuthority) { authority.Counts.CreationSpecCommandReceipts = 1 },
		"evidence mismatch":         func(authority *fixtureAuthority) { authority.EvidenceValid = false },
		"content digest drift":      func(authority *fixtureAuthority) { authority.ContentDigest = strings.Repeat("0", 64) },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			authority := valid
			mutate(&authority)
			var output bytes.Buffer
			err := execute(context.Background(), config, &fixtureRepositoryStub{authority: authority}, &output)
			if !errors.Is(err, errFixtureConflict) || output.Len() != 0 {
				t.Fatalf("error=%v output=%q", err, output.String())
			}
		})
	}
}

// validFixtureTestData 创建不依赖外部服务且满足全部权威不变量的测试夹具。
func validFixtureTestData() (fixtureConfig, fixtureSeed, fixtureAuthority) {
	config := fixtureConfig{
		Mode: fixtureModeSeed, ProjectID: testProjectID, OwnerUserID: testOwnerID,
		AssetID:    deterministicFixtureUUIDv7("analyze-materials-asset", testProjectID, testOwnerID),
		EvidenceID: deterministicFixtureUUIDv7("analyze-materials-evidence", testProjectID, testOwnerID),
	}
	contentDigest := sha256Text(fixtureEvidenceContent)
	seed := fixtureSeed{
		ProjectID: config.ProjectID, OwnerUserID: config.OwnerUserID,
		AssetID: config.AssetID, EvidenceID: config.EvidenceID, AssetVersion: fixtureAssetVersion,
		Content: fixtureEvidenceContent, ContentDigest: contentDigest,
	}
	authority := fixtureAuthority{
		AssetID: seed.AssetID, EvidenceID: seed.EvidenceID, AssetVersion: seed.AssetVersion,
		ContentDigest: seed.ContentDigest,
		Counts:        fixtureCounts{Assets: 1, Evidence: 1},
		ProjectValid:  true, AssetValid: true, EvidenceValid: true,
	}
	return config, seed, authority
}

// sha256Text 返回测试字符串 UTF-8 字节的小写 SHA-256。
func sha256Text(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// assertExactJSONKeys 验证 JSON object 只包含期望键集合，不接受未知安全字段扩张。
func assertExactJSONKeys(t *testing.T, actual map[string]json.RawMessage, expected ...string) {
	t.Helper()
	actualKeys := make([]string, 0, len(actual))
	for key := range actual {
		actualKeys = append(actualKeys, key)
	}
	sort.Strings(actualKeys)
	sort.Strings(expected)
	if !reflect.DeepEqual(actualKeys, expected) {
		t.Fatalf("JSON keys=%v want=%v", actualKeys, expected)
	}
}
