// Package w2r02legacyupgradev2_test 校验仅完成准备、尚未登记的 R02 legacy-upgrade v2 child manifest。
package w2r02legacyupgradev2_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	manifestPathV2        = "agent/tests/contract/testdata/w2_r02_upgrade_v2/manifest.json"
	legacyManifestPathV1  = "agent/tests/contract/testdata/w2_r02_upgrade/manifest.json"
	authorityCorpusPathV1 = "agent/tests/contract/testdata/w2_r02_upgrade/legacy_authority_attestation_v1.json"
	blockerCorpusPathV1   = "agent/tests/contract/testdata/w2_r02_upgrade/session_lane_upgrade_blocker_v1.json"
	validatorPathV2       = "agent/tests/contract/w2r02legacyupgradev2/legacy_upgrade_child_manifest_v2_test.go"
	guardPathV2           = "agent/tests/contract/w2r02legacyupgradev2guard/legacy_upgrade_child_manifest_v2_guard_test.go"
	blockStatementV2      = "本 manifest 只把既有 R02 legacy-upgrade v1 两份 corpus 的 3 个 fixture、107 条 vector 与 14 个 target test 机械发布为 prepared_unregistered child exact-set；它不修改 v1 字节、不登记 live candidate_evidence、不生成 aggregate、不记录 Owner 选择或批准，也不解锁 W2-A1/W2-A2 或任何生产实现。"
)

type manifestV2 struct {
	SchemaVersion          string          `json:"schema_version"`
	ManifestID             string          `json:"manifest_id"`
	Gate                   string          `json:"gate"`
	Scope                  string          `json:"scope"`
	Status                 string          `json:"status"`
	ApprovalStatus         string          `json:"approval_status"`
	ImplementationStatus   string          `json:"implementation_status"`
	EvidenceStatus         string          `json:"evidence_status"`
	RegistrationStatus     string          `json:"registration_status"`
	AggregateStatus        string          `json:"aggregate_status"`
	OwnerRoleSetStatus     string          `json:"owner_role_set_status"`
	ImplementationUnlocked bool            `json:"implementation_unlocked"`
	BallotEnabled          bool            `json:"ballot_enabled"`
	SourceManifest         artifactRefV2   `json:"source_manifest"`
	CorpusFiles            []corpusFileV2  `json:"corpus_files"`
	FixtureIDs             []string        `json:"fixture_ids"`
	VectorIDs              []string        `json:"vector_ids"`
	TotalVectorCount       int             `json:"total_vector_count"`
	TargetTests            []string        `json:"target_tests"`
	ValidatorSources       []artifactRefV2 `json:"validator_sources"`
	ForbiddenCapabilities  []string        `json:"forbidden_capabilities"`
	BlockStatement         string          `json:"block_statement"`
}

type artifactRefV2 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type corpusFileV2 struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	VectorCount int    `json:"vector_count"`
}

type legacyManifestV1 struct {
	SchemaVersion    string         `json:"schema_version"`
	Files            []legacyFileV1 `json:"files"`
	TotalVectorCount int            `json:"total_vector_count"`
	TargetTests      []string       `json:"target_tests"`
}

type legacyFileV1 struct {
	File        string `json:"file"`
	SHA256      string `json:"sha256"`
	VectorCount int    `json:"vector_count"`
}

type sourceSpecV2 struct {
	Path        string
	PackageName string
	Imports     []string
	Tests       []string
}

// TestW2R02LegacyUpgradeChildManifestV2 将 v2 manifest 绑定到未改动的 v1 corpus 原始字节与完整 ID 集。
func TestW2R02LegacyUpgradeChildManifestV2(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootV2(t)
	manifest, err := validateManifestV2(repoRoot, readFileV2(t, repoRoot, manifestPathV2))
	if err != nil {
		t.Fatal(err)
	}
	verifyPreparedBoundaryV2(t, manifest)
	verifySourceManifestV2(t, repoRoot, manifest.SourceManifest)
	verifyCorpusBindingsV2(t, repoRoot, manifest)
	verifyValidatorSourcesV2(t, repoRoot, manifest.ValidatorSources)
	verifyManifestDirectoryV2(t, repoRoot)
}

// TestW2R02LegacyUpgradeChildManifestV2StrictJSON 证明重复键、空值、未知字段与尾随 JSON 均按失败关闭处理。
func TestW2R02LegacyUpgradeChildManifestV2StrictJSON(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootV2(t)
	raw := readFileV2(t, repoRoot, manifestPathV2)
	tests := map[string][]byte{
		"duplicate": bytes.Replace(raw, []byte(`"manifest_id":`), []byte(`"schema_version":"duplicate","manifest_id":`), 1),
		"null":      bytes.Replace(raw, []byte(`"implementation_unlocked": false`), []byte(`"implementation_unlocked": null`), 1),
		"unknown":   bytes.Replace(raw, []byte(`"manifest_id":`), []byte(`"approved":true,"manifest_id":`), 1),
		"trailing":  append(append([]byte(nil), raw...), []byte(`{}`)...),
	}
	for name, mutated := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := validateManifestV2(repoRoot, mutated); err == nil {
				t.Fatalf("v2 child manifest must reject %s JSON", name)
			}
		})
	}
}

// TestW2R02LegacyUpgradeChildManifestV2FailClosed 冻结排序、计数、原始引用与非权威状态边界。
func TestW2R02LegacyUpgradeChildManifestV2FailClosed(t *testing.T) {
	t.Parallel()

	repoRoot := repoRootV2(t)
	raw := readFileV2(t, repoRoot, manifestPathV2)
	mutations := map[string]func(*manifestV2){
		"fixture duplicate": func(manifest *manifestV2) { manifest.FixtureIDs[1] = manifest.FixtureIDs[0] },
		"fixture order": func(manifest *manifestV2) {
			manifest.FixtureIDs[0], manifest.FixtureIDs[1] = manifest.FixtureIDs[1], manifest.FixtureIDs[0]
		},
		"vector duplicate": func(manifest *manifestV2) { manifest.VectorIDs[1] = manifest.VectorIDs[0] },
		"vector order": func(manifest *manifestV2) {
			manifest.VectorIDs[0], manifest.VectorIDs[1] = manifest.VectorIDs[1], manifest.VectorIDs[0]
		},
		"count":         func(manifest *manifestV2) { manifest.TotalVectorCount++ },
		"corpus digest": func(manifest *manifestV2) { manifest.CorpusFiles[0].SHA256 = strings.Repeat("0", 64) },
		"registered":    func(manifest *manifestV2) { manifest.RegistrationStatus = "registered" },
		"approved":      func(manifest *manifestV2) { manifest.ApprovalStatus = "approved" },
		"aggregate":     func(manifest *manifestV2) { manifest.AggregateStatus = "created" },
		"unlocked":      func(manifest *manifestV2) { manifest.ImplementationUnlocked = true },
		"ballot":        func(manifest *manifestV2) { manifest.BallotEnabled = true },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			manifest, err := decodeManifestV2(raw)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&manifest)
			mutated, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := validateManifestV2(repoRoot, mutated); err == nil {
				t.Fatalf("v2 child manifest must reject %s drift", name)
			}
		})
	}
}

func validateManifestV2(repoRoot string, raw []byte) (manifestV2, error) {
	manifest, err := decodeManifestV2(raw)
	if err != nil {
		return manifestV2{}, err
	}
	if manifest.SchemaVersion != "w2_r02_legacy_upgrade_child_manifest.v2" || manifest.ManifestID != "W2-R02-LEGACY-UPGRADE-CHILD-v2" || manifest.Gate != "W2-R02" || manifest.Scope != "legacy_upgrade_existing_v1_corpus_exact_set" {
		return manifestV2{}, fmt.Errorf("v2 child manifest identity drift")
	}
	if manifest.Status != "prepared_unregistered" || manifest.ApprovalStatus != "not_requested" || manifest.ImplementationStatus != "prohibited" || manifest.EvidenceStatus != "candidate_only" || manifest.RegistrationStatus != "not_registered" || manifest.AggregateStatus != "not_created" || manifest.OwnerRoleSetStatus != "not_derived" || manifest.ImplementationUnlocked || manifest.BallotEnabled {
		return manifestV2{}, fmt.Errorf("v2 child manifest authority boundary drift")
	}
	if manifest.BlockStatement != blockStatementV2 {
		return manifestV2{}, fmt.Errorf("v2 child manifest block statement drift")
	}
	if err := requireSortedUniqueV2(manifest.FixtureIDs, "fixture_ids", 3); err != nil {
		return manifestV2{}, err
	}
	if err := requireSortedUniqueV2(manifest.VectorIDs, "vector_ids", 107); err != nil {
		return manifestV2{}, err
	}
	if err := requireSortedUniqueV2(manifest.TargetTests, "target_tests", 14); err != nil {
		return manifestV2{}, err
	}
	if manifest.TotalVectorCount != len(manifest.VectorIDs) || manifest.TotalVectorCount != 107 {
		return manifestV2{}, fmt.Errorf("total_vector_count=%d", manifest.TotalVectorCount)
	}
	wantForbidden := []string{"aggregate_manifest", "formal_freeze", "gate_candidate_evidence_registration", "implementation_unlock", "owner_approval", "production_w2_a1", "production_w2_a2"}
	if !reflect.DeepEqual(manifest.ForbiddenCapabilities, wantForbidden) {
		return manifestV2{}, fmt.Errorf("forbidden_capabilities drift")
	}
	if len(manifest.CorpusFiles) != 2 || manifest.CorpusFiles[0].Path >= manifest.CorpusFiles[1].Path || len(manifest.ValidatorSources) != 2 || manifest.ValidatorSources[0].Path >= manifest.ValidatorSources[1].Path {
		return manifestV2{}, fmt.Errorf("artifact refs exact-set/order drift")
	}
	wantCorpus := []corpusFileV2{{Path: authorityCorpusPathV1, VectorCount: 17}, {Path: blockerCorpusPathV1, VectorCount: 90}}
	for index, want := range wantCorpus {
		if manifest.CorpusFiles[index].Path != want.Path || manifest.CorpusFiles[index].VectorCount != want.VectorCount {
			return manifestV2{}, fmt.Errorf("corpus_files[%d] identity/count drift", index)
		}
		if err := verifyArtifactRefErrorV2(repoRoot, artifactRefV2{Path: manifest.CorpusFiles[index].Path, SHA256: manifest.CorpusFiles[index].SHA256}, want.Path); err != nil {
			return manifestV2{}, err
		}
	}
	wantSources := []string{validatorPathV2, guardPathV2}
	for index, wantPath := range wantSources {
		if err := verifyArtifactRefErrorV2(repoRoot, manifest.ValidatorSources[index], wantPath); err != nil {
			return manifestV2{}, err
		}
	}
	if err := verifyArtifactRefErrorV2(repoRoot, manifest.SourceManifest, legacyManifestPathV1); err != nil {
		return manifestV2{}, err
	}
	return manifest, nil
}

func decodeManifestV2(raw []byte) (manifestV2, error) {
	if err := validateJSONShapeV2(raw); err != nil {
		return manifestV2{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest manifestV2
	if err := decoder.Decode(&manifest); err != nil {
		return manifestV2{}, err
	}
	if err := requireEOFV2(decoder); err != nil {
		return manifestV2{}, err
	}
	return manifest, nil
}

func verifyPreparedBoundaryV2(t *testing.T, manifest manifestV2) {
	t.Helper()
	if manifest.Status != "prepared_unregistered" || manifest.RegistrationStatus != "not_registered" || manifest.AggregateStatus != "not_created" || manifest.ImplementationUnlocked || manifest.BallotEnabled {
		t.Fatalf("prepared-only boundary drift: %+v", manifest)
	}
}

func verifySourceManifestV2(t *testing.T, repoRoot string, ref artifactRefV2) {
	t.Helper()
	if err := verifyArtifactRefErrorV2(repoRoot, ref, legacyManifestPathV1); err != nil {
		t.Fatal(err)
	}
	raw := readFileV2(t, repoRoot, legacyManifestPathV1)
	if err := validateJSONShapeV2(raw); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest legacyManifestV1
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if err := requireEOFV2(decoder); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != "w2_r02_legacy_upgrade_manifest.v1" || manifest.TotalVectorCount != 107 || len(manifest.Files) != 2 || len(manifest.TargetTests) != 14 {
		t.Fatalf("legacy v1 source manifest drift: %+v", manifest)
	}
}

func verifyCorpusBindingsV2(t *testing.T, repoRoot string, manifest manifestV2) {
	t.Helper()
	wantCorpus := []corpusFileV2{
		{Path: authorityCorpusPathV1, VectorCount: 17},
		{Path: blockerCorpusPathV1, VectorCount: 90},
	}
	for index := range wantCorpus {
		if manifest.CorpusFiles[index].Path != wantCorpus[index].Path || manifest.CorpusFiles[index].VectorCount != wantCorpus[index].VectorCount {
			t.Fatalf("corpus_files[%d] drift: %+v", index, manifest.CorpusFiles[index])
		}
		verifyArtifactRefV2(t, repoRoot, artifactRefV2{Path: manifest.CorpusFiles[index].Path, SHA256: manifest.CorpusFiles[index].SHA256}, wantCorpus[index].Path)
	}
	authorityRaw := readFileV2(t, repoRoot, authorityCorpusPathV1)
	blockerRaw := readFileV2(t, repoRoot, blockerCorpusPathV1)
	wantFixtures := extractIDsV2(t, authorityRaw, []string{"cases", "exact_sets", "fixtures", "schema_version"}, "fixtures", "fixture_id")
	wantVectors := append(extractIDsV2(t, authorityRaw, []string{"cases", "exact_sets", "fixtures", "schema_version"}, "cases", "id"), extractIDsV2(t, blockerRaw, []string{"cases", "exact_sets", "schema_version"}, "cases", "id")...)
	sort.Strings(wantVectors)
	if !reflect.DeepEqual(manifest.FixtureIDs, wantFixtures) || !reflect.DeepEqual(manifest.VectorIDs, wantVectors) {
		t.Fatalf("fixture/vector exact-set drift")
	}
	legacyRaw := readFileV2(t, repoRoot, legacyManifestPathV1)
	legacy, err := decodeLegacyManifestV1(legacyRaw)
	if err != nil {
		t.Fatal(err)
	}
	wantTests := append([]string(nil), legacy.TargetTests...)
	sort.Strings(wantTests)
	if !reflect.DeepEqual(manifest.TargetTests, wantTests) {
		t.Fatalf("target_tests=%v want=%v", manifest.TargetTests, wantTests)
	}
}

func decodeLegacyManifestV1(raw []byte) (legacyManifestV1, error) {
	if err := validateJSONShapeV2(raw); err != nil {
		return legacyManifestV1{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest legacyManifestV1
	if err := decoder.Decode(&manifest); err != nil {
		return legacyManifestV1{}, err
	}
	if err := requireEOFV2(decoder); err != nil {
		return legacyManifestV1{}, err
	}
	return manifest, nil
}

func extractIDsV2(t *testing.T, raw []byte, wantRootKeys []string, arrayKey, idKey string) []string {
	t.Helper()
	if err := validateJSONShapeV2(raw); err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	keys := sortedMapKeysV2(root)
	if !reflect.DeepEqual(keys, wantRootKeys) {
		t.Fatalf("corpus root keys=%v want=%v", keys, wantRootKeys)
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(root[arrayKey], &entries); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		var id string
		if err := json.Unmarshal(entry[idKey], &id); err != nil || id == "" {
			t.Fatalf("corpus %s id invalid: %v", arrayKey, err)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if err := requireSortedUniqueV2(ids, arrayKey, len(ids)); err != nil {
		t.Fatal(err)
	}
	return ids
}

func verifyValidatorSourcesV2(t *testing.T, repoRoot string, refs []artifactRefV2) {
	t.Helper()
	specs := sourceSpecsV2()
	if len(refs) != len(specs) {
		t.Fatalf("validator_sources=%d want=%d", len(refs), len(specs))
	}
	for index, spec := range specs {
		verifyArtifactRefV2(t, repoRoot, refs[index], spec.Path)
		verifySourceV2(t, repoRoot, spec)
	}
}

func sourceSpecsV2() []sourceSpecV2 {
	commonImports := []string{"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "errors", "fmt", "go/ast", "go/parser", "go/token", "io", "os", "path/filepath", "reflect", "runtime", "sort", "strings", "testing", "unicode/utf8"}
	return []sourceSpecV2{
		{Path: validatorPathV2, PackageName: "w2r02legacyupgradev2_test", Imports: commonImports, Tests: []string{"TestW2R02LegacyUpgradeChildManifestV2", "TestW2R02LegacyUpgradeChildManifestV2FailClosed", "TestW2R02LegacyUpgradeChildManifestV2StrictJSON"}},
		{Path: guardPathV2, PackageName: "w2r02legacyupgradev2guard_test", Imports: commonImports, Tests: []string{"TestW2R02LegacyUpgradeChildManifestGuardV2", "TestW2R02LegacyUpgradeChildManifestGuardV2SourceAndAuthorityMutations", "TestW2R02LegacyUpgradeChildManifestGuardV2StrictJSON"}},
	}
}

func verifySourceV2(t *testing.T, repoRoot string, spec sourceSpecV2) {
	t.Helper()
	fullPath := filepath.Join(repoRoot, filepath.FromSlash(spec.Path))
	info, err := os.Lstat(fullPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o644 {
		t.Fatalf("validator source must be regular 0644: %s mode=%s", spec.Path, info.Mode())
	}
	entries, err := os.ReadDir(filepath.Dir(fullPath))
	if err != nil {
		t.Fatal(err)
	}
	var goFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			goFiles = append(goFiles, entry.Name())
		}
	}
	if !reflect.DeepEqual(goFiles, []string{filepath.Base(spec.Path)}) {
		t.Fatalf("validator package source exact-set=%v", goFiles)
	}
	raw := readFileV2(t, repoRoot, spec.Path)
	if bytes.Contains(raw, []byte("go:"+"embed")) {
		t.Fatalf("validator source must not use embed: %s", spec.Path)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), spec.Path, raw, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name.Name != spec.PackageName {
		t.Fatalf("package=%s want=%s", parsed.Name.Name, spec.PackageName)
	}
	var imports []string
	for _, item := range parsed.Imports {
		path := strings.Trim(item.Path.Value, `"`)
		if item.Name != nil || strings.Contains(strings.Split(path, "/")[0], ".") || strings.Contains(path, "/internal/") {
			t.Fatalf("validator import is not independent stdlib-only: %s", path)
		}
		imports = append(imports, path)
	}
	sort.Strings(imports)
	wantImports := append([]string(nil), spec.Imports...)
	sort.Strings(wantImports)
	if !reflect.DeepEqual(imports, wantImports) {
		t.Fatalf("imports=%v want=%v", imports, wantImports)
	}
	var tests []string
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv != nil {
			continue
		}
		if function.Name.Name == "init" || function.Name.Name == "TestMain" || strings.HasPrefix(function.Name.Name, "Fuzz") || strings.HasPrefix(function.Name.Name, "Benchmark") || strings.HasPrefix(function.Name.Name, "Example") {
			t.Fatalf("hidden test entrypoint=%s", function.Name.Name)
		}
		if strings.HasPrefix(function.Name.Name, "Test") {
			tests = append(tests, function.Name.Name)
		}
	}
	sort.Strings(tests)
	if !reflect.DeepEqual(tests, spec.Tests) {
		t.Fatalf("tests=%v want=%v", tests, spec.Tests)
	}
}

func verifyManifestDirectoryV2(t *testing.T, repoRoot string) {
	t.Helper()
	directory := filepath.Join(repoRoot, filepath.FromSlash(filepath.Dir(manifestPathV2)))
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(manifestPathV2) {
		t.Fatalf("v2 manifest directory exact-set drift: %v", entries)
	}
	info, err := os.Lstat(filepath.Join(directory, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o644 {
		t.Fatalf("v2 manifest must be regular 0644: %s", info.Mode())
	}
}

func verifyArtifactRefV2(t *testing.T, repoRoot string, ref artifactRefV2, wantPath string) {
	t.Helper()
	if err := verifyArtifactRefErrorV2(repoRoot, ref, wantPath); err != nil {
		t.Fatal(err)
	}
}

func verifyArtifactRefErrorV2(repoRoot string, ref artifactRefV2, wantPath string) error {
	if ref.Path != wantPath || !strings.HasPrefix(ref.SHA256, "sha256:") || len(ref.SHA256) != len("sha256:")+sha256.Size*2 {
		return fmt.Errorf("artifact ref invalid path=%s", ref.Path)
	}
	fullPath := filepath.Join(repoRoot, filepath.FromSlash(ref.Path))
	info, err := os.Lstat(fullPath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o644 {
		return fmt.Errorf("artifact ref must be regular 0644 path=%s mode=%s", ref.Path, info.Mode())
	}
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	want := "sha256:" + hex.EncodeToString(digest[:])
	if ref.SHA256 != want {
		return fmt.Errorf("artifact digest=%s want=%s path=%s", ref.SHA256, want, ref.Path)
	}
	return nil
}

func requireSortedUniqueV2(values []string, name string, wantLen int) error {
	if values == nil || len(values) != wantLen {
		return fmt.Errorf("%s length=%d want=%d", name, len(values), wantLen)
	}
	last := ""
	for _, value := range values {
		if value == "" || value <= last {
			return fmt.Errorf("%s not sorted or duplicate=%q", name, value)
		}
		last = value
	}
	return nil
}

func validateJSONShapeV2(raw []byte) error {
	if !utf8.Valid(raw) || len(raw) == 0 {
		return fmt.Errorf("invalid UTF-8 or empty JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := scanJSONValueV2(decoder, "$"); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON token=%v err=%v", token, err)
	}
	return nil
}

func scanJSONValueV2(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return fmt.Errorf("null forbidden at %s", path)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not string at %s", path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate key %s.%s", path, key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValueV2(decoder, path+"."+key); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return fmt.Errorf("object close at %s: %v", path, err)
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := scanJSONValueV2(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
			index++
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return fmt.Errorf("array close at %s: %v", path, err)
		}
	default:
		return fmt.Errorf("unexpected delimiter at %s", path)
	}
	return nil
}

func requireEOFV2(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func sortedMapKeysV2(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func repoRootV2(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve validator source path")
	}
	directory := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(directory, "agent", "go.mod")); err == nil {
			if _, docsErr := os.Stat(filepath.Join(directory, "docs", "requirements", "project-development-plan.md")); docsErr == nil {
				return directory
			}
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("repository root not found")
		}
		directory = parent
	}
}

func readFileV2(t *testing.T, repoRoot, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
