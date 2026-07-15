package reviewfreeze_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/token"
	pathpkg "path"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const reviewFreezeValidateCompileAttestationGoListStandardPackageCountV1 = 124

// reviewFreezeValidateCompileAttestationGoListV1 只校验声明的 canonical go-list 投影结构；
// Batch 3C 必须从每个 builder 的 raw JSON 独立重算。含宿主路径/cache 字段的 raw 不能直接比较。
func reviewFreezeValidateCompileAttestationGoListV1(goList reviewFreezeCompileAttestationGoListV1, goArchiveSHA256 string, externalModule reviewFreezeCompileAttestationModuleV1) error {
	if goList.RawToProjectionPolicy != reviewFreezeGoListRawToProjectionPolicyV1 {
		return fmt.Errorf("go list raw-to-projection policy=%q", goList.RawToProjectionPolicy)
	}
	if !reviewFreezePrefixedSHA256V1.MatchString(goArchiveSHA256) {
		return fmt.Errorf("go list toolchain archive digest 非法=%q", goArchiveSHA256)
	}
	projection := goList.Projection
	if projection.SchemaVersion != reviewFreezeGoListProjectionSchemaV1 ||
		projection.EntrypointID != reviewFreezeCompileAttestationEntrypointV1 ||
		projection.ModuleRoot != "agent" ||
		projection.PackagePattern != reviewFreezeCompileAttestationPackagePatternV1 ||
		projection.TargetImportPath != reviewFreezeCompileAttestationTargetImportV1 {
		return fmt.Errorf("go list projection identity 漂移=%+v", projection)
	}
	if !projection.TargetTestVariantObserved || !projection.SyntheticTestMainObserved {
		return fmt.Errorf("go list 未观测完整 target test variant/synthetic test main")
	}
	if projection.Packages == nil {
		return fmt.Errorf("go list packages 必须显式声明数组")
	}

	projectionRaw, err := json.Marshal(projection)
	if err != nil {
		return fmt.Errorf("编码 go list canonical projection: %w", err)
	}
	projectionDigest := sha256.Sum256(projectionRaw)
	wantProjectionSHA256 := "sha256:" + hex.EncodeToString(projectionDigest[:])
	if goList.ProjectionSHA256 != wantProjectionSHA256 {
		return fmt.Errorf("go list projection digest=%q want=%q", goList.ProjectionSHA256, wantProjectionSHA256)
	}

	externalPackages := make(map[string]reviewFreezeCompileAttestationExternalPackageV1, len(externalModule.Packages))
	for _, externalPackage := range externalModule.Packages {
		if _, duplicate := externalPackages[externalPackage.ImportPath]; duplicate {
			return fmt.Errorf("go list external module package 重复=%q", externalPackage.ImportPath)
		}
		externalPackages[externalPackage.ImportPath] = externalPackage
	}

	packages := make(map[string]reviewFreezeGoListCanonicalPackageV1, len(projection.Packages))
	standardImports := make([]string, 0, len(projection.Packages))
	lastImportPath := ""
	targetCount := 0
	externalCount := 0
	for _, selectedPackage := range projection.Packages {
		if selectedPackage.ImportPath <= lastImportPath || !reviewFreezeExternalImportPathV1.MatchString(selectedPackage.ImportPath) {
			return fmt.Errorf("go list packages 未排序、重复或 import_path 非法=%q", selectedPackage.ImportPath)
		}
		if err := reviewFreezeValidateSafePathV1(selectedPackage.ImportPath, ""); err != nil {
			return fmt.Errorf("go list package import_path: %w", err)
		}
		if strings.Contains(selectedPackage.ImportPath, " [") || strings.HasSuffix(selectedPackage.ImportPath, ".test") {
			return fmt.Errorf("go list canonical projection 禁止 synthetic package=%q", selectedPackage.ImportPath)
		}
		if selectedPackage.PackageName == "_" || !token.IsIdentifier(selectedPackage.PackageName) {
			return fmt.Errorf("go list package_name 非法=%q", selectedPackage.PackageName)
		}
		if err := reviewFreezeValidateCompileAttestationGoListImportSetV1(selectedPackage.Imports, selectedPackage.ImportPath+" imports", true); err != nil {
			return err
		}
		if _, duplicate := packages[selectedPackage.ImportPath]; duplicate {
			return fmt.Errorf("go list package 重复=%q", selectedPackage.ImportPath)
		}

		switch {
		case selectedPackage.ImportPath == reviewFreezeCompileAttestationTargetImportV1:
			targetCount++
			if err := reviewFreezeValidateCompileAttestationGoListTargetPackageV1(selectedPackage); err != nil {
				return err
			}
		case externalPackages[selectedPackage.ImportPath].ImportPath != "":
			externalCount++
			if err := reviewFreezeValidateCompileAttestationGoListExternalPackageV1(selectedPackage, externalModule, externalPackages[selectedPackage.ImportPath]); err != nil {
				return err
			}
		default:
			if !reviewFreezeIsStandardLibraryImportV1(selectedPackage.ImportPath) {
				return fmt.Errorf("go list 出现未锁定 external package=%q", selectedPackage.ImportPath)
			}
			if err := reviewFreezeValidateCompileAttestationGoListStandardPackageV1(selectedPackage, goArchiveSHA256); err != nil {
				return err
			}
			standardImports = append(standardImports, selectedPackage.ImportPath)
		}

		packages[selectedPackage.ImportPath] = selectedPackage
		lastImportPath = selectedPackage.ImportPath
	}
	if targetCount != 1 || externalCount != len(externalPackages) {
		return fmt.Errorf("go list target/external exact-set 数量=%d/%d want=1/%d", targetCount, externalCount, len(externalPackages))
	}
	if want := reviewFreezeValidateCompileAttestationGoListExpectedStandardImportsV1(); !reflect.DeepEqual(standardImports, want) {
		return fmt.Errorf("go list stdlib package exact-set=%v want=%v", standardImports, want)
	}
	if err := reviewFreezeValidateCompileAttestationGoListClosureV1(packages); err != nil {
		return err
	}
	return nil
}

// reviewFreezeValidateCompileAttestationGoListTargetPackageV1 固定 R01 独立 package 的六个源码、
// 两个测试入口和直接 import；这层不能接受同 Module 的额外传递源码。
func reviewFreezeValidateCompileAttestationGoListTargetPackageV1(selectedPackage reviewFreezeGoListCanonicalPackageV1) error {
	want := reviewFreezeGoListCanonicalPackageV1{
		ImportPath:  reviewFreezeCompileAttestationTargetImportV1,
		PackageName: "w2r01_test",
		PackageKind: "target",
		Module: reviewFreezeGoListCanonicalModuleV1{
			Kind:      "main",
			Path:      reviewFreezeCompileAttestationModulePathV1,
			GoVersion: "1.26",
		},
		GoFiles: []string{
			"tests/contract/w2r01/approval_continuation_parent_receipt_v1.go",
			"tests/contract/w2r01/graph_tool_result_v1.go",
			"tests/contract/w2r01/tool_receipt_v1.go",
			"tests/contract/w2r01/validator_support_v1.go",
		},
		CompiledGoFiles: []string{
			"tests/contract/w2r01/approval_continuation_parent_receipt_v1.go",
			"tests/contract/w2r01/graph_tool_result_v1.go",
			"tests/contract/w2r01/tool_receipt_v1.go",
			"tests/contract/w2r01/validator_support_v1.go",
		},
		TestGoFiles: []string{
			"tests/contract/w2r01/graph_tool_result_v1_corpus_test.go",
			"tests/contract/w2r01/tool_receipt_v1_corpus_test.go",
		},
		XTestGoFiles: []string{},
		Imports: []string{
			"bytes",
			"crypto/sha256",
			"encoding/hex",
			"encoding/json",
			"errors",
			"fmt",
			"go/ast",
			"go/parser",
			"go/token",
			"golang.org/x/text/unicode/norm",
			"io",
			"os",
			"path",
			"path/filepath",
			"reflect",
			"regexp",
			"sort",
			"strings",
			"testing",
			"unicode",
			"unicode/utf8",
		},
		TestImports:     []string{"testing"},
		XTestImports:    []string{},
		EmbedFiles:      []string{},
		TestEmbedFiles:  []string{},
		XTestEmbedFiles: []string{},
		OtherBuildInputs: reviewFreezeGoListOtherBuildInputsV1{
			CgoFiles:     []string{},
			CFiles:       []string{},
			CXXFiles:     []string{},
			MFiles:       []string{},
			FFiles:       []string{},
			SFiles:       []string{},
			SysoFiles:    []string{},
			SwigFiles:    []string{},
			SwigCXXFiles: []string{},
		},
	}
	if !reflect.DeepEqual(selectedPackage, want) {
		return fmt.Errorf("go list target package projection 漂移=%+v", selectedPackage)
	}
	return nil
}

// reviewFreezeValidateCompileAttestationGoListExternalPackageV1 把声明的外部 package
// selection 与 statement 固定的 archive/source/import claim 逐项交叉绑定。
func reviewFreezeValidateCompileAttestationGoListExternalPackageV1(selectedPackage reviewFreezeGoListCanonicalPackageV1, module reviewFreezeCompileAttestationModuleV1, externalPackage reviewFreezeCompileAttestationExternalPackageV1) error {
	wantModule := reviewFreezeGoListCanonicalModuleV1{
		Kind:      "external",
		Path:      module.ModulePath,
		Version:   module.Version,
		ModuleSum: module.ModuleSum,
		GoModSum:  module.GoModSum,
	}
	if selectedPackage.PackageKind != "external" || !reflect.DeepEqual(selectedPackage.Module, wantModule) {
		return fmt.Errorf("go list external package module union 漂移=%s %+v", selectedPackage.ImportPath, selectedPackage.Module)
	}
	wantPackageName := pathpkg.Base(externalPackage.ImportPath)
	if selectedPackage.PackageName != wantPackageName {
		return fmt.Errorf("go list external package_name=%q want=%q", selectedPackage.PackageName, wantPackageName)
	}
	wantSources := make([]string, 0, len(externalPackage.Sources))
	wantDirectory := strings.TrimPrefix(externalPackage.ImportPath, module.ModulePath)
	wantDirectory = strings.TrimPrefix(wantDirectory, "/")
	for _, source := range externalPackage.Sources {
		if err := reviewFreezeValidateSafePathV1(source.Path, wantDirectory+"/"); err != nil {
			return fmt.Errorf("go list external source path: %w", err)
		}
		if pathpkg.Dir(source.Path) != wantDirectory || pathpkg.Ext(source.Path) != ".go" || !reviewFreezePrefixedSHA256V1.MatchString(source.SHA256) {
			return fmt.Errorf("go list external source identity 非法=%+v", source)
		}
		wantSources = append(wantSources, source.Path)
	}
	if !reflect.DeepEqual(selectedPackage.GoFiles, wantSources) || !reflect.DeepEqual(selectedPackage.CompiledGoFiles, wantSources) || !reflect.DeepEqual(selectedPackage.Imports, externalPackage.Imports) {
		return fmt.Errorf("go list external selected source/import exact-set 漂移=%s", selectedPackage.ImportPath)
	}
	if err := reviewFreezeValidateCompileAttestationGoListPathSetV1(selectedPackage.GoFiles, selectedPackage.ImportPath+" go_files", wantDirectory+"/", false); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileAttestationGoListPathSetV1(selectedPackage.CompiledGoFiles, selectedPackage.ImportPath+" compiled_go_files", wantDirectory+"/", false); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileAttestationGoListEmptyPackageTestAndEmbedV1(selectedPackage, selectedPackage.ImportPath); err != nil {
		return err
	}
	return reviewFreezeValidateCompileAttestationGoListEmptyOtherBuildInputsV1(selectedPackage.OtherBuildInputs, selectedPackage.ImportPath)
}

// reviewFreezeValidateCompileAttestationGoListStandardPackageV1 校验 stdlib package 只绑定锁定
// Go archive，并要求所有源码和辅助输入使用 GOROOT/src 相对路径，排除绝对宿主路径。
func reviewFreezeValidateCompileAttestationGoListStandardPackageV1(selectedPackage reviewFreezeGoListCanonicalPackageV1, goArchiveSHA256 string) error {
	wantModule := reviewFreezeGoListCanonicalModuleV1{Kind: "stdlib", GoArchiveSHA256: goArchiveSHA256}
	if selectedPackage.PackageKind != "stdlib" || !reflect.DeepEqual(selectedPackage.Module, wantModule) {
		return fmt.Errorf("go list stdlib module union 漂移=%s %+v", selectedPackage.ImportPath, selectedPackage.Module)
	}
	prefix := "src/" + selectedPackage.ImportPath + "/"
	if err := reviewFreezeValidateCompileAttestationGoListPathSetV1(selectedPackage.GoFiles, selectedPackage.ImportPath+" go_files", prefix, false); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileAttestationGoListPathSetV1(selectedPackage.CompiledGoFiles, selectedPackage.ImportPath+" compiled_go_files", prefix, true); err != nil {
		return err
	}
	if err := reviewFreezeValidateCompileAttestationGoListEmptyPackageTestAndEmbedV1(selectedPackage, selectedPackage.ImportPath); err != nil {
		return err
	}
	for _, input := range []struct {
		name   string
		values []string
	}{
		{name: "cgo_files", values: selectedPackage.OtherBuildInputs.CgoFiles},
		{name: "c_files", values: selectedPackage.OtherBuildInputs.CFiles},
		{name: "cxx_files", values: selectedPackage.OtherBuildInputs.CXXFiles},
		{name: "m_files", values: selectedPackage.OtherBuildInputs.MFiles},
		{name: "f_files", values: selectedPackage.OtherBuildInputs.FFiles},
		{name: "s_files", values: selectedPackage.OtherBuildInputs.SFiles},
		{name: "syso_files", values: selectedPackage.OtherBuildInputs.SysoFiles},
		{name: "swig_files", values: selectedPackage.OtherBuildInputs.SwigFiles},
		{name: "swig_cxx_files", values: selectedPackage.OtherBuildInputs.SwigCXXFiles},
	} {
		if err := reviewFreezeValidateCompileAttestationGoListPathSetV1(input.values, selectedPackage.ImportPath+" "+input.name, prefix, true); err != nil {
			return err
		}
	}
	return nil
}

// reviewFreezeValidateCompileAttestationGoListEmptyPackageTestAndEmbedV1 拒绝依赖 package
// 的测试元数据与当前闭包不存在的 embed 输入；只有 target 自身测试会进入 test binary。
func reviewFreezeValidateCompileAttestationGoListEmptyPackageTestAndEmbedV1(selectedPackage reviewFreezeGoListCanonicalPackageV1, field string) error {
	for _, values := range []struct {
		name   string
		values []string
	}{
		{name: "test_go_files", values: selectedPackage.TestGoFiles},
		{name: "x_test_go_files", values: selectedPackage.XTestGoFiles},
		{name: "test_imports", values: selectedPackage.TestImports},
		{name: "x_test_imports", values: selectedPackage.XTestImports},
		{name: "embed_files", values: selectedPackage.EmbedFiles},
		{name: "test_embed_files", values: selectedPackage.TestEmbedFiles},
		{name: "x_test_embed_files", values: selectedPackage.XTestEmbedFiles},
	} {
		if values.values == nil || len(values.values) != 0 {
			return fmt.Errorf("go list %s %s 必须是显式空 exact-set=%v", field, values.name, values.values)
		}
	}
	return nil
}

// reviewFreezeValidateCompileAttestationGoListEmptyOtherBuildInputsV1 要求 target/external
// 不含 cgo、汇编、syso 或 swig 等未由其源码摘要覆盖的额外构建输入。
func reviewFreezeValidateCompileAttestationGoListEmptyOtherBuildInputsV1(inputs reviewFreezeGoListOtherBuildInputsV1, field string) error {
	for _, values := range []struct {
		name   string
		values []string
	}{
		{name: "cgo_files", values: inputs.CgoFiles},
		{name: "c_files", values: inputs.CFiles},
		{name: "cxx_files", values: inputs.CXXFiles},
		{name: "m_files", values: inputs.MFiles},
		{name: "f_files", values: inputs.FFiles},
		{name: "s_files", values: inputs.SFiles},
		{name: "syso_files", values: inputs.SysoFiles},
		{name: "swig_files", values: inputs.SwigFiles},
		{name: "swig_cxx_files", values: inputs.SwigCXXFiles},
	} {
		if values.values == nil || len(values.values) != 0 {
			return fmt.Errorf("go list %s %s 必须是显式空 exact-set=%v", field, values.name, values.values)
		}
	}
	return nil
}

// reviewFreezeValidateCompileAttestationGoListPathSetV1 校验 canonical path 数组显式、
// 按字节序唯一且位于预期 module/GOROOT 相对目录中。
func reviewFreezeValidateCompileAttestationGoListPathSetV1(values []string, field, requiredPrefix string, allowEmpty bool) error {
	if values == nil {
		return fmt.Errorf("go list %s 必须显式声明数组", field)
	}
	if len(values) == 0 && !allowEmpty {
		return fmt.Errorf("go list %s exact-set 不能为空", field)
	}
	last := ""
	for _, value := range values {
		if value <= last {
			return fmt.Errorf("go list %s 未排序或重复=%q", field, value)
		}
		if err := reviewFreezeValidateSafePathV1(value, requiredPrefix); err != nil {
			return fmt.Errorf("go list %s: %w", field, err)
		}
		last = value
	}
	return nil
}

// reviewFreezeValidateCompileAttestationGoListImportSetV1 校验 import exact-set 显式、
// 按字节序唯一且不含 synthetic、绝对路径或父目录逃逸形式。
func reviewFreezeValidateCompileAttestationGoListImportSetV1(values []string, field string, allowEmpty bool) error {
	if values == nil {
		return fmt.Errorf("go list %s 必须显式声明数组", field)
	}
	if len(values) == 0 && !allowEmpty {
		return fmt.Errorf("go list %s exact-set 不能为空", field)
	}
	last := ""
	for _, value := range values {
		if value <= last || !reviewFreezeExternalImportPathV1.MatchString(value) {
			return fmt.Errorf("go list %s 未排序、重复或非法=%q", field, value)
		}
		if err := reviewFreezeValidateSafePathV1(value, ""); err != nil {
			return fmt.Errorf("go list %s: %w", field, err)
		}
		last = value
	}
	return nil
}

// reviewFreezeValidateCompileAttestationGoListClosureV1 要求每条 regular/test import 都在
// 投影中闭合，并拒绝无法从 target 与 Go 1.26.3 synthetic test main 到达的额外 package。
func reviewFreezeValidateCompileAttestationGoListClosureV1(packages map[string]reviewFreezeGoListCanonicalPackageV1) error {
	packagePaths := reviewFreezeValidateCompileAttestationGoListSortedPackagePathsV1(packages)
	for _, importPath := range packagePaths {
		selectedPackage := packages[importPath]
		imports := selectedPackage.Imports
		if importPath == reviewFreezeCompileAttestationTargetImportV1 {
			imports = append(append([]string{}, imports...), selectedPackage.TestImports...)
			imports = append(imports, selectedPackage.XTestImports...)
		}
		for _, dependency := range imports {
			dependencyPackage, exists := packages[dependency]
			if !exists {
				return fmt.Errorf("go list import graph 未闭合 %s -> %s", importPath, dependency)
			}
			if selectedPackage.PackageKind == "stdlib" && dependencyPackage.PackageKind != "stdlib" {
				return fmt.Errorf("go list stdlib 禁止反向依赖非 stdlib %s -> %s", importPath, dependency)
			}
			if selectedPackage.PackageKind == "external" && dependencyPackage.PackageKind == "target" {
				return fmt.Errorf("go list external 禁止反向依赖 target %s -> %s", importPath, dependency)
			}
		}
	}

	// synthetic test main 不进入 canonical package 列表，但其固定四条根依赖必须参与可达性。
	queue := []string{
		reviewFreezeCompileAttestationTargetImportV1,
		"os",
		"reflect",
		"testing",
		"testing/internal/testdeps",
	}
	visited := make(map[string]struct{}, len(packages))
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		if _, exists := visited[current]; exists {
			continue
		}
		selectedPackage, exists := packages[current]
		if !exists {
			return fmt.Errorf("go list synthetic/target root 缺失=%q", current)
		}
		visited[current] = struct{}{}
		queue = append(queue, selectedPackage.Imports...)
		if current == reviewFreezeCompileAttestationTargetImportV1 {
			queue = append(queue, selectedPackage.TestImports...)
			queue = append(queue, selectedPackage.XTestImports...)
		}
	}
	if len(visited) != len(packages) {
		unreachable := make([]string, 0, len(packages)-len(visited))
		for _, importPath := range packagePaths {
			if _, exists := visited[importPath]; !exists {
				unreachable = append(unreachable, importPath)
			}
		}
		return fmt.Errorf("go list 含不可达额外 package=%v", unreachable)
	}
	return nil
}

// reviewFreezeValidateCompileAttestationGoListSortedPackagePathsV1 为闭包错误选择提供稳定顺序，
// 避免 Go map 迭代顺序改变同一非法 statement 的首个失败原因。
func reviewFreezeValidateCompileAttestationGoListSortedPackagePathsV1(packages map[string]reviewFreezeGoListCanonicalPackageV1) []string {
	paths := make([]string, 0, len(packages))
	for importPath := range packages {
		paths = append(paths, importPath)
	}
	sort.Strings(paths)
	return paths
}

// reviewFreezeValidateCompileAttestationGoListExpectedStandardImportsV1 固定 Go 1.26.3、
// linux/amd64、CGO=0 下 R01 test binary 的 124 项 stdlib package exact-set。
func reviewFreezeValidateCompileAttestationGoListExpectedStandardImportsV1() []string {
	return []string{
		"bufio",
		"bytes",
		"cmp",
		"compress/flate",
		"compress/gzip",
		"context",
		"crypto",
		"crypto/cipher",
		"crypto/fips140",
		"crypto/internal/boring",
		"crypto/internal/boring/sig",
		"crypto/internal/constanttime",
		"crypto/internal/entropy/v1.0.0",
		"crypto/internal/fips140",
		"crypto/internal/fips140/aes",
		"crypto/internal/fips140/aes/gcm",
		"crypto/internal/fips140/alias",
		"crypto/internal/fips140/check",
		"crypto/internal/fips140/drbg",
		"crypto/internal/fips140/hmac",
		"crypto/internal/fips140/sha256",
		"crypto/internal/fips140/sha3",
		"crypto/internal/fips140/sha512",
		"crypto/internal/fips140/subtle",
		"crypto/internal/fips140deps/byteorder",
		"crypto/internal/fips140deps/cpu",
		"crypto/internal/fips140deps/godebug",
		"crypto/internal/fips140deps/time",
		"crypto/internal/fips140only",
		"crypto/internal/impl",
		"crypto/internal/sysrand",
		"crypto/sha256",
		"crypto/subtle",
		"encoding",
		"encoding/base64",
		"encoding/binary",
		"encoding/hex",
		"encoding/json",
		"errors",
		"flag",
		"fmt",
		"go/ast",
		"go/build/constraint",
		"go/internal/scannerhooks",
		"go/parser",
		"go/scanner",
		"go/token",
		"hash",
		"hash/crc32",
		"internal/abi",
		"internal/asan",
		"internal/bisect",
		"internal/bytealg",
		"internal/byteorder",
		"internal/chacha8rand",
		"internal/coverage/rtcov",
		"internal/cpu",
		"internal/filepathlite",
		"internal/fmtsort",
		"internal/fuzz",
		"internal/goarch",
		"internal/godebug",
		"internal/godebugs",
		"internal/goexperiment",
		"internal/goos",
		"internal/msan",
		"internal/oserror",
		"internal/poll",
		"internal/profilerecord",
		"internal/race",
		"internal/reflectlite",
		"internal/runtime/atomic",
		"internal/runtime/cgroup",
		"internal/runtime/exithook",
		"internal/runtime/gc",
		"internal/runtime/gc/scan",
		"internal/runtime/maps",
		"internal/runtime/math",
		"internal/runtime/pprof/label",
		"internal/runtime/sys",
		"internal/runtime/syscall/linux",
		"internal/strconv",
		"internal/stringslite",
		"internal/sync",
		"internal/synctest",
		"internal/syscall/execenv",
		"internal/syscall/unix",
		"internal/sysinfo",
		"internal/testlog",
		"internal/trace/tracev2",
		"internal/unsafeheader",
		"io",
		"io/fs",
		"iter",
		"math",
		"math/bits",
		"math/rand",
		"os",
		"os/exec",
		"os/signal",
		"path",
		"path/filepath",
		"reflect",
		"regexp",
		"regexp/syntax",
		"runtime",
		"runtime/debug",
		"runtime/pprof",
		"runtime/trace",
		"slices",
		"sort",
		"strconv",
		"strings",
		"sync",
		"sync/atomic",
		"syscall",
		"testing",
		"testing/internal/testdeps",
		"text/tabwriter",
		"time",
		"unicode",
		"unicode/utf16",
		"unicode/utf8",
		"unsafe",
	}
}

// TestW2ReviewFreezeCompileAttestationGoListStandardPackageCountV1 防止锁定工具链的
// stdlib exact-set 在无人审计时被静默增删。
func TestW2ReviewFreezeCompileAttestationGoListStandardPackageCountV1(t *testing.T) {
	if got := len(reviewFreezeValidateCompileAttestationGoListExpectedStandardImportsV1()); got != reviewFreezeValidateCompileAttestationGoListStandardPackageCountV1 {
		t.Fatalf("stdlib package exact-set count=%d want=%d", got, reviewFreezeValidateCompileAttestationGoListStandardPackageCountV1)
	}
}

// TestW2ReviewFreezeCompileAttestationGoListClosureAdversarialV1 覆盖缺失依赖、不可达
// package 与 synthetic test package，确保 exact-set 不能只校验名称而忽略依赖图语义。
func TestW2ReviewFreezeCompileAttestationGoListClosureAdversarialV1(t *testing.T) {
	base := reviewFreezeCompileAttestationFixtureStatementV1(t)
	tests := []struct {
		name   string
		mutate func(*testing.T, *reviewFreezeValidatorCompileAttestationV1)
		want   string
	}{
		{
			name: "import references missing package",
			mutate: func(t *testing.T, statement *reviewFreezeValidatorCompileAttestationV1) {
				index := reviewFreezeTestCompileAttestationPackageIndexV1(t, *statement, "bytes")
				statement.GoList.Projection.Packages[index].Imports = append(statement.GoList.Projection.Packages[index].Imports, "missing")
				sort.Strings(statement.GoList.Projection.Packages[index].Imports)
			},
			want: "import graph 未闭合",
		},
		{
			name: "locked stdlib package becomes unreachable",
			mutate: func(t *testing.T, statement *reviewFreezeValidatorCompileAttestationV1) {
				index := reviewFreezeTestCompileAttestationPackageIndexV1(t, *statement, "bytes")
				imports := statement.GoList.Projection.Packages[index].Imports
				for importIndex, importPath := range imports {
					if importPath == "compress/flate" {
						statement.GoList.Projection.Packages[index].Imports = append(imports[:importIndex], imports[importIndex+1:]...)
						return
					}
				}
				t.Fatal("bytes fixture missing compress/flate reachability edge")
			},
			want: "不可达额外 package",
		},
		{
			name: "synthetic test package",
			mutate: func(t *testing.T, statement *reviewFreezeValidatorCompileAttestationV1) {
				index := reviewFreezeTestCompileAttestationPackageIndexV1(t, *statement, "unsafe")
				statement.GoList.Projection.Packages[index].ImportPath = "unsafe.test"
			},
			want: "禁止 synthetic package",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement := reviewFreezeCompileAttestationFixtureDeepCopyV1(t, base)
			test.mutate(t, &statement)
			reviewFreezeCompileAttestationFixtureRefreshProjectionDigestV1(t, &statement)
			reviewFreezeTestCompileAttestationRejectsV1(t, statement, test.want)
		})
	}
}

// TestW2ReviewFreezeCompileAttestationGoListModuleAndBuildInputsAdversarialV1 覆盖 module
// tagged union 与 target/external/stdlib 辅助输入边界，避免汇编输入绕过各自内容信任根。
func TestW2ReviewFreezeCompileAttestationGoListModuleAndBuildInputsAdversarialV1(t *testing.T) {
	base := reviewFreezeCompileAttestationFixtureStatementV1(t)
	tests := []struct {
		name   string
		mutate func(*testing.T, *reviewFreezeValidatorCompileAttestationV1)
		want   string
	}{
		{
			name: "external module union drift",
			mutate: func(t *testing.T, statement *reviewFreezeValidatorCompileAttestationV1) {
				index := reviewFreezeTestCompileAttestationPackageIndexV1(t, *statement, reviewFreezeXTextModulePathV1+"/transform")
				statement.GoList.Projection.Packages[index].Module.GoVersion = "1.24.0"
			},
			want: "external package module union",
		},
		{
			name: "target assembly input",
			mutate: func(t *testing.T, statement *reviewFreezeValidatorCompileAttestationV1) {
				index := reviewFreezeTestCompileAttestationPackageIndexV1(t, *statement, reviewFreezeCompileAttestationTargetImportV1)
				statement.GoList.Projection.Packages[index].OtherBuildInputs.SFiles = []string{"tests/contract/w2r01/extra.s"}
			},
			want: "target package projection",
		},
		{
			name: "external assembly input",
			mutate: func(t *testing.T, statement *reviewFreezeValidatorCompileAttestationV1) {
				index := reviewFreezeTestCompileAttestationPackageIndexV1(t, *statement, reviewFreezeXTextModulePathV1+"/transform")
				statement.GoList.Projection.Packages[index].OtherBuildInputs.SFiles = []string{"transform/extra.s"}
			},
			want: "s_files 必须是显式空",
		},
		{
			name: "stdlib absolute assembly input",
			mutate: func(t *testing.T, statement *reviewFreezeValidatorCompileAttestationV1) {
				index := reviewFreezeTestCompileAttestationPackageIndexV1(t, *statement, "bytes")
				statement.GoList.Projection.Packages[index].OtherBuildInputs.SFiles = []string{"/opt/go/src/bytes/fixture.s"}
			},
			want: "不安全路径",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement := reviewFreezeCompileAttestationFixtureDeepCopyV1(t, base)
			test.mutate(t, &statement)
			reviewFreezeCompileAttestationFixtureRefreshProjectionDigestV1(t, &statement)
			reviewFreezeTestCompileAttestationRejectsV1(t, statement, test.want)
		})
	}

	t.Run("stdlib safe assembly input", func(t *testing.T) {
		statement := reviewFreezeCompileAttestationFixtureDeepCopyV1(t, base)
		index := reviewFreezeTestCompileAttestationPackageIndexV1(t, statement, "bytes")
		statement.GoList.Projection.Packages[index].OtherBuildInputs.SFiles = []string{"src/bytes/fixture.s"}
		reviewFreezeCompileAttestationFixtureRefreshProjectionDigestV1(t, &statement)
		raw := reviewFreezeCompileAttestationFixtureMarshalV1(t, statement)
		if err := reviewFreezeValidateCompileAttestationStatementJSONV1(raw); err != nil {
			t.Fatalf("安全 stdlib SFiles 被拒绝: %v", err)
		}
	})
}
