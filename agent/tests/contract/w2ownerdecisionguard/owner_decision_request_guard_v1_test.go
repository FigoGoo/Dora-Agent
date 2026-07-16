// Package w2ownerdecisionguard_test 从语义 validator 包外部固定 R03/R04 待决请求验证器的源码选择边界。
package w2ownerdecisionguard_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const (
	r03RequestPathV1 = "docs/design/agent/approvals/w2-r03-owner-decision-requests/DR-W2-R03-v1.json"
	r04RequestPathV1 = "docs/design/agent/approvals/w2-r04-owner-decision-requests/DR-W2-R04-v1.json"
	validatorPathV1  = "agent/tests/contract/w2ownerdecision/owner_decision_request_v1_test.go"
	guardPathV1      = "agent/tests/contract/w2ownerdecisionguard/owner_decision_request_guard_v1_test.go"
)

type artifactRefV1 struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type requestProjectionV1 struct {
	ValidatorSources []artifactRefV1 `json:"validator_sources"`
}

type sourceSpecV1 struct {
	Path        string
	PackageName string
	Imports     []string
}

func TestW2R03R04OwnerDecisionRequestValidatorSourceGuardV1(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootV1(t)
	specs := sourceSpecsV1()
	wantPaths := []string{validatorPathV1, guardPathV1}

	var baseline []artifactRefV1
	for _, requestPath := range []string{r03RequestPathV1, r04RequestPathV1} {
		raw := readFileV1(t, repoRoot, requestPath)
		var request requestProjectionV1
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Fatal(err)
		}
		if len(request.ValidatorSources) != len(wantPaths) {
			t.Fatalf("%s validator_sources=%v", requestPath, request.ValidatorSources)
		}
		for index, wantPath := range wantPaths {
			verifyArtifactRefV1(t, repoRoot, request.ValidatorSources[index], wantPath)
		}
		if baseline == nil {
			baseline = append([]artifactRefV1(nil), request.ValidatorSources...)
		} else if !reflect.DeepEqual(request.ValidatorSources, baseline) {
			t.Fatalf("R03/R04 validator_sources 漂移: got=%v want=%v", request.ValidatorSources, baseline)
		}
	}

	for _, spec := range specs {
		verifySourcePackageV1(t, repoRoot, spec)
	}
}

func repoRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 validator guard 源文件")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "..", ".."))
}

func readFileV1(t *testing.T, repoRoot, repoPath string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(repoPath)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func verifyArtifactRefV1(t *testing.T, repoRoot string, ref artifactRefV1, wantPath string) {
	t.Helper()
	if ref.Path != wantPath {
		t.Fatalf("validator source path=%q want=%q", ref.Path, wantPath)
	}
	raw := readFileV1(t, repoRoot, ref.Path)
	digest := sha256.Sum256(raw)
	wantHash := "sha256:" + hex.EncodeToString(digest[:])
	if ref.SHA256 != wantHash {
		t.Fatalf("%s raw SHA-256=%s want=%s", ref.Path, wantHash, ref.SHA256)
	}
}

func verifySourcePackageV1(t *testing.T, repoRoot string, spec sourceSpecV1) {
	t.Helper()
	sourcePath := filepath.Join(repoRoot, filepath.FromSlash(spec.Path))
	entries, err := os.ReadDir(filepath.Dir(sourcePath))
	if err != nil {
		t.Fatal(err)
	}
	var goSources []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			t.Fatalf("validator source 不得为 symlink: %s", entry.Name())
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			t.Fatal(infoErr)
		}
		if !info.Mode().IsRegular() {
			t.Fatalf("validator source 必须是 regular file: %s", entry.Name())
		}
		goSources = append(goSources, entry.Name())
	}
	if want := []string{filepath.Base(sourcePath)}; !reflect.DeepEqual(goSources, want) {
		t.Fatalf("%s Go source exact-set=%v want=%v", spec.PackageName, goSources, want)
	}

	raw := readFileV1(t, repoRoot, spec.Path)
	if bytes.Contains(raw, []byte("//"+"go:build")) || bytes.Contains(raw, []byte("// "+"+build")) {
		t.Fatalf("%s 禁止 build constraint", spec.PackageName)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), sourcePath, raw, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Name.Name != spec.PackageName {
		t.Fatalf("validator package=%q want=%q", parsed.Name.Name, spec.PackageName)
	}
	imports := make([]string, 0, len(parsed.Imports))
	for _, importSpec := range parsed.Imports {
		if importSpec.Name != nil {
			t.Fatalf("validator import 禁止 alias/dot/blank: %s", importSpec.Path.Value)
		}
		imports = append(imports, strings.Trim(importSpec.Path.Value, `"`))
	}
	if !reflect.DeepEqual(imports, spec.Imports) {
		t.Fatalf("%s stdlib import exact-set=%v want=%v", spec.PackageName, imports, spec.Imports)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && (function.Name.Name == "init" || function.Name.Name == "TestMain") {
			t.Fatalf("%s 禁止 %s", spec.PackageName, function.Name.Name)
		}
	}
}

func sourceSpecsV1() []sourceSpecV1 {
	return []sourceSpecV1{
		{
			Path:        validatorPathV1,
			PackageName: "w2ownerdecision_test",
			Imports: []string{
				"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "fmt", "go/ast", "go/parser", "go/token",
				"io", "os", "path", "path/filepath", "reflect", "regexp", "runtime", "sort", "strings", "testing",
			},
		},
		{
			Path:        guardPathV1,
			PackageName: "w2ownerdecisionguard_test",
			Imports: []string{
				"bytes", "crypto/sha256", "encoding/hex", "encoding/json", "go/ast", "go/parser", "go/token",
				"os", "path/filepath", "reflect", "runtime", "strings", "testing",
			},
		},
	}
}
