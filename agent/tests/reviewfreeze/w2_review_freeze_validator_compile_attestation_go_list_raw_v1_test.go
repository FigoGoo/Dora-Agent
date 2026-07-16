package reviewfreeze_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	reviewFreezeCompileAttestationGoListRawPackageCountV1 = 129
	// reviewFreezeCompileAttestationGoListRawGoldenSHA256V1 绑定 Go 1.26.3、
	// linux/amd64、CGO=0 下真实 raw stream 规范化后的完整 source/import 图。
	reviewFreezeCompileAttestationGoListRawGoldenSHA256V1 = "sha256:52bd7a314b5ce513feba0eca5d3af43e45cc6ffbf5d84b4fd333221902d5f844"
)

type reviewFreezeCompileAttestationGoListRawContextV1 struct {
	GoRoot          string
	ModuleRoot      string
	ModuleCacheRoot string
	GoCacheRoot     string
	GoArchiveSHA256 string
	ExternalModule  reviewFreezeCompileAttestationModuleV1
}

// reviewFreezeCompileAttestationGoListRawPackageV1 只接受 Go 1.26.3 `go list
// -deps -test -compiled -json` 的固定字段集合。未来 Go 新增字段时必须升级 raw schema，
// 不能在 verifier 中静默忽略。
type reviewFreezeCompileAttestationGoListRawPackageV1 struct {
	Dir                string                                           `json:"Dir"`
	ImportPath         string                                           `json:"ImportPath"`
	ImportComment      string                                           `json:"ImportComment"`
	Name               string                                           `json:"Name"`
	Doc                string                                           `json:"Doc"`
	Target             string                                           `json:"Target"`
	Shlib              string                                           `json:"Shlib"`
	Root               string                                           `json:"Root"`
	ConflictDir        string                                           `json:"ConflictDir"`
	ForTest            string                                           `json:"ForTest"`
	Export             string                                           `json:"Export"`
	BuildID            string                                           `json:"BuildID"`
	Module             *reviewFreezeCompileAttestationGoListRawModuleV1 `json:"Module"`
	Match              []string                                         `json:"Match"`
	Goroot             bool                                             `json:"Goroot"`
	Standard           bool                                             `json:"Standard"`
	DepOnly            bool                                             `json:"DepOnly"`
	BinaryOnly         bool                                             `json:"BinaryOnly"`
	Incomplete         bool                                             `json:"Incomplete"`
	Stale              bool                                             `json:"Stale"`
	StaleReason        string                                           `json:"StaleReason"`
	GoFiles            []string                                         `json:"GoFiles"`
	CgoFiles           []string                                         `json:"CgoFiles"`
	CompiledGoFiles    []string                                         `json:"CompiledGoFiles"`
	IgnoredGoFiles     []string                                         `json:"IgnoredGoFiles"`
	IgnoredOtherFiles  []string                                         `json:"IgnoredOtherFiles"`
	CFiles             []string                                         `json:"CFiles"`
	CXXFiles           []string                                         `json:"CXXFiles"`
	MFiles             []string                                         `json:"MFiles"`
	HFiles             []string                                         `json:"HFiles"`
	FFiles             []string                                         `json:"FFiles"`
	SFiles             []string                                         `json:"SFiles"`
	SwigFiles          []string                                         `json:"SwigFiles"`
	SwigCXXFiles       []string                                         `json:"SwigCXXFiles"`
	SysoFiles          []string                                         `json:"SysoFiles"`
	EmbedPatterns      []string                                         `json:"EmbedPatterns"`
	EmbedFiles         []string                                         `json:"EmbedFiles"`
	CgoCFLAGS          []string                                         `json:"CgoCFLAGS"`
	CgoCPPFLAGS        []string                                         `json:"CgoCPPFLAGS"`
	CgoCXXFLAGS        []string                                         `json:"CgoCXXFLAGS"`
	CgoFFLAGS          []string                                         `json:"CgoFFLAGS"`
	CgoLDFLAGS         []string                                         `json:"CgoLDFLAGS"`
	CgoPkgConfig       []string                                         `json:"CgoPkgConfig"`
	Imports            []string                                         `json:"Imports"`
	ImportMap          map[string]string                                `json:"ImportMap"`
	Deps               []string                                         `json:"Deps"`
	Error              *reviewFreezeCompileAttestationGoListRawErrorV1  `json:"Error"`
	DepsErrors         []reviewFreezeCompileAttestationGoListRawErrorV1 `json:"DepsErrors"`
	TestGoFiles        []string                                         `json:"TestGoFiles"`
	TestImports        []string                                         `json:"TestImports"`
	TestEmbedPatterns  []string                                         `json:"TestEmbedPatterns"`
	TestEmbedFiles     []string                                         `json:"TestEmbedFiles"`
	XTestGoFiles       []string                                         `json:"XTestGoFiles"`
	XTestImports       []string                                         `json:"XTestImports"`
	XTestEmbedPatterns []string                                         `json:"XTestEmbedPatterns"`
	XTestEmbedFiles    []string                                         `json:"XTestEmbedFiles"`
}

type reviewFreezeCompileAttestationGoListRawModuleV1 struct {
	Path      string                                                `json:"Path"`
	Version   string                                                `json:"Version"`
	Time      string                                                `json:"Time"`
	Main      bool                                                  `json:"Main"`
	Indirect  bool                                                  `json:"Indirect"`
	Dir       string                                                `json:"Dir"`
	GoMod     string                                                `json:"GoMod"`
	GoVersion string                                                `json:"GoVersion"`
	Sum       string                                                `json:"Sum"`
	GoModSum  string                                                `json:"GoModSum"`
	Replace   *reviewFreezeCompileAttestationGoListRawModuleV1      `json:"Replace"`
	Error     *reviewFreezeCompileAttestationGoListRawModuleErrorV1 `json:"Error"`
}

type reviewFreezeCompileAttestationGoListRawModuleErrorV1 struct {
	Err string `json:"Err"`
}

type reviewFreezeCompileAttestationGoListRawErrorV1 struct {
	ImportStack []string `json:"ImportStack"`
	Pos         string   `json:"Pos"`
	Err         string   `json:"Err"`
}

func reviewFreezeParseCompileAttestationGoListRawStreamV1(raw []byte) ([]reviewFreezeCompileAttestationGoListRawPackageV1, error) {
	if len(raw) == 0 || len(raw) > reviewFreezeCompileAttestationGoListRawMaxBytesV1 {
		return nil, fmt.Errorf("go list raw stream size=%d limit=%d", len(raw), reviewFreezeCompileAttestationGoListRawMaxBytesV1)
	}
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("go list raw stream 不是合法 UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	packages := make([]reviewFreezeCompileAttestationGoListRawPackageV1, 0, reviewFreezeCompileAttestationGoListRawPackageCountV1)
	for {
		var objectRaw json.RawMessage
		if err := decoder.Decode(&objectRaw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("decode go list raw stream: %w", err)
		}
		if len(objectRaw) == 0 || objectRaw[0] != '{' {
			return nil, fmt.Errorf("go list raw stream 元素必须是 object")
		}
		if len(packages) >= reviewFreezeCompileAttestationGoListRawPackageCountV1 {
			return nil, fmt.Errorf("go list raw package count 超出=%d limit=%d", len(packages)+1, reviewFreezeCompileAttestationGoListRawPackageCountV1)
		}
		if err := reviewFreezeInspectCompileAttestationJSONV1(objectRaw); err != nil {
			return nil, fmt.Errorf("inspect go list raw object: %w", err)
		}
		var generic any
		genericDecoder := json.NewDecoder(bytes.NewReader(objectRaw))
		genericDecoder.UseNumber()
		if err := genericDecoder.Decode(&generic); err != nil {
			return nil, fmt.Errorf("decode generic go list raw object: %w", err)
		}
		if err := reviewFreezeRejectCompileAttestationNullV1(generic, "$go_list_raw"); err != nil {
			return nil, err
		}
		if err := reviewFreezeValidateCompileAttestationGoListRawExactKeysV1(objectRaw, reflect.TypeOf(reviewFreezeCompileAttestationGoListRawPackageV1{}), "$go_list_raw"); err != nil {
			return nil, err
		}
		strictDecoder := json.NewDecoder(bytes.NewReader(objectRaw))
		strictDecoder.DisallowUnknownFields()
		var rawPackage reviewFreezeCompileAttestationGoListRawPackageV1
		if err := strictDecoder.Decode(&rawPackage); err != nil {
			return nil, fmt.Errorf("strict decode go list raw object: %w", err)
		}
		var trailing any
		if err := strictDecoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("go list raw object trailing JSON")
		}
		packages = append(packages, rawPackage)
	}
	if len(packages) != reviewFreezeCompileAttestationGoListRawPackageCountV1 {
		return nil, fmt.Errorf("go list raw package exact-set count=%d want=%d", len(packages), reviewFreezeCompileAttestationGoListRawPackageCountV1)
	}
	return packages, nil
}

// reviewFreezeValidateCompileAttestationGoListRawExactKeysV1 补足 encoding/json
// 对 struct 字段名大小写不敏感的兼容行为。字段仍可选，但出现的每个 struct key
// 必须与固定 Go 1.26.3 schema 的 json tag 完全一致；Module.Replace、Error 和
// DepsErrors 等嵌套 struct 也递归执行同一约束。
func reviewFreezeValidateCompileAttestationGoListRawExactKeysV1(raw json.RawMessage, targetType reflect.Type, path string) error {
	for targetType.Kind() == reflect.Pointer {
		targetType = targetType.Elem()
	}
	switch targetType.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return fmt.Errorf("go list raw exact-case object %s: %w", path, err)
		}
		allowed := make(map[string]reflect.Type, targetType.NumField())
		for index := 0; index < targetType.NumField(); index++ {
			field := targetType.Field(index)
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "" {
				name = field.Name
			}
			if name != "-" {
				allowed[name] = field.Type
			}
		}
		for name, value := range object {
			fieldType, ok := allowed[name]
			if !ok {
				return fmt.Errorf("go list raw exact-case unknown field %s.%s", path, name)
			}
			if err := reviewFreezeValidateCompileAttestationGoListRawExactKeysV1(value, fieldType, path+"."+name); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		elementType := targetType.Elem()
		for elementType.Kind() == reflect.Pointer {
			elementType = elementType.Elem()
		}
		if elementType.Kind() != reflect.Struct {
			return nil
		}
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return fmt.Errorf("go list raw exact-case array %s: %w", path, err)
		}
		for index, value := range values {
			if err := reviewFreezeValidateCompileAttestationGoListRawExactKeysV1(value, targetType.Elem(), fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func reviewFreezeCanonicalizeCompileAttestationGoListRawV1(raw []byte, rawContext reviewFreezeCompileAttestationGoListRawContextV1) (reviewFreezeGoListCanonicalProjectionV1, error) {
	if err := reviewFreezeValidateCompileAttestationGoListRawContextV1(rawContext); err != nil {
		return reviewFreezeGoListCanonicalProjectionV1{}, err
	}
	rawPackages, err := reviewFreezeParseCompileAttestationGoListRawStreamV1(raw)
	if err != nil {
		return reviewFreezeGoListCanonicalProjectionV1{}, err
	}

	seen := make(map[string]struct{}, len(rawPackages))
	mainModules := make(map[string]struct{})
	canonicalPackages := make([]reviewFreezeGoListCanonicalPackageV1, 0, len(rawPackages)-2)
	regularRawPackages := make(map[string]reviewFreezeCompileAttestationGoListRawPackageV1, len(rawPackages)-2)
	var targetPackage *reviewFreezeCompileAttestationGoListRawPackageV1
	var targetTestVariant *reviewFreezeCompileAttestationGoListRawPackageV1
	var syntheticTestMain *reviewFreezeCompileAttestationGoListRawPackageV1
	targetTestVariantImport := reviewFreezeCompileAttestationTargetImportV1 + " [" + reviewFreezeCompileAttestationTargetImportV1 + ".test]"
	targetTestMainImport := reviewFreezeCompileAttestationTargetImportV1 + ".test"

	for index := range rawPackages {
		rawPackage := &rawPackages[index]
		if err := reviewFreezeValidateCompileAttestationGoListRawPackageCommonV1(*rawPackage); err != nil {
			return reviewFreezeGoListCanonicalProjectionV1{}, err
		}
		if _, duplicate := seen[rawPackage.ImportPath]; duplicate {
			return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("go list raw package 重复=%q", rawPackage.ImportPath)
		}
		seen[rawPackage.ImportPath] = struct{}{}
		if rawPackage.Module != nil && rawPackage.Module.Main {
			if rawPackage.Module.Path != reviewFreezeCompileAttestationModulePathV1 || rawPackage.Module.Dir != rawContext.ModuleRoot {
				return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("go list raw workspace/multiple main module 漂移=%q/%q", rawPackage.Module.Path, rawPackage.Module.Dir)
			}
			mainModules[rawPackage.Module.Path+"\x00"+rawPackage.Module.Dir] = struct{}{}
		}

		switch rawPackage.ImportPath {
		case reviewFreezeCompileAttestationTargetImportV1:
			if rawPackage.ForTest != "" {
				return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("go list raw target regular ForTest 非法=%q", rawPackage.ForTest)
			}
			targetPackage = rawPackage
		case targetTestVariantImport:
			targetTestVariant = rawPackage
			continue
		case targetTestMainImport:
			syntheticTestMain = rawPackage
			continue
		default:
			if rawPackage.ForTest != "" || strings.Contains(rawPackage.ImportPath, " [") || strings.HasSuffix(rawPackage.ImportPath, ".test") {
				return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("go list raw 未知 target test variant/synthetic main=%q ForTest=%q", rawPackage.ImportPath, rawPackage.ForTest)
			}
		}

		canonicalPackage, canonicalErr := reviewFreezeCanonicalizeCompileAttestationGoListRawRegularPackageV1(*rawPackage, rawContext)
		if canonicalErr != nil {
			return reviewFreezeGoListCanonicalProjectionV1{}, canonicalErr
		}
		canonicalPackages = append(canonicalPackages, canonicalPackage)
		regularRawPackages[rawPackage.ImportPath] = *rawPackage
	}

	if len(mainModules) != 1 {
		return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("go list raw workspace/multiple main modules=%v", mainModules)
	}
	wantMainModuleKey := reviewFreezeCompileAttestationModulePathV1 + "\x00" + filepath.Clean(rawContext.ModuleRoot)
	if _, ok := mainModules[wantMainModuleKey]; !ok {
		return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("go list raw workspace main module 漂移=%v", mainModules)
	}
	if targetPackage == nil || targetTestVariant == nil || syntheticTestMain == nil {
		return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("go list raw target/test variant/synthetic main 缺失")
	}
	if err := reviewFreezeValidateCompileAttestationGoListRawDependencyClosuresV1(regularRawPackages); err != nil {
		return reviewFreezeGoListCanonicalProjectionV1{}, err
	}
	if err := reviewFreezeValidateCompileAttestationGoListRawTargetVariantV1(*targetPackage, *targetTestVariant, rawContext); err != nil {
		return reviewFreezeGoListCanonicalProjectionV1{}, err
	}
	wantSyntheticDeps := make([]string, 0, len(canonicalPackages))
	for _, canonicalPackage := range canonicalPackages {
		if canonicalPackage.ImportPath != reviewFreezeCompileAttestationTargetImportV1 {
			wantSyntheticDeps = append(wantSyntheticDeps, canonicalPackage.ImportPath)
		}
	}
	wantSyntheticDeps = append(wantSyntheticDeps, targetTestVariantImport)
	sort.Strings(wantSyntheticDeps)
	if err := reviewFreezeValidateCompileAttestationGoListRawSyntheticMainV1(*syntheticTestMain, rawContext, wantSyntheticDeps); err != nil {
		return reviewFreezeGoListCanonicalProjectionV1{}, err
	}

	sort.Slice(canonicalPackages, func(i, j int) bool {
		return canonicalPackages[i].ImportPath < canonicalPackages[j].ImportPath
	})
	projection := reviewFreezeGoListCanonicalProjectionV1{
		SchemaVersion:             reviewFreezeGoListProjectionSchemaV1,
		EntrypointID:              reviewFreezeCompileAttestationEntrypointV1,
		ModuleRoot:                "agent",
		PackagePattern:            reviewFreezeCompileAttestationPackagePatternV1,
		TargetImportPath:          reviewFreezeCompileAttestationTargetImportV1,
		TargetTestVariantObserved: true,
		SyntheticTestMainObserved: true,
		Packages:                  canonicalPackages,
	}
	projectionSHA, digestErr := reviewFreezeCompileAttestationGoListRawProjectionSHA256V1(projection)
	if digestErr != nil {
		return reviewFreezeGoListCanonicalProjectionV1{}, digestErr
	}
	goList := reviewFreezeCompileAttestationGoListV1{
		RawToProjectionPolicy: reviewFreezeGoListRawToProjectionPolicyV1,
		Projection:            projection,
		ProjectionSHA256:      projectionSHA,
	}
	if err := reviewFreezeValidateCompileAttestationGoListV1(goList, rawContext.GoArchiveSHA256, rawContext.ExternalModule); err != nil {
		return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("validate canonicalized go list projection: %w", err)
	}
	return projection, nil
}

func reviewFreezeValidateCompileAttestationGoListRawContextV1(rawContext reviewFreezeCompileAttestationGoListRawContextV1) error {
	for _, root := range []struct {
		name  string
		value string
	}{
		{name: "GOROOT", value: rawContext.GoRoot},
		{name: "module root", value: rawContext.ModuleRoot},
		{name: "module cache", value: rawContext.ModuleCacheRoot},
		{name: "go cache", value: rawContext.GoCacheRoot},
	} {
		if !filepath.IsAbs(root.value) || filepath.Clean(root.value) != root.value || strings.IndexByte(root.value, 0) >= 0 {
			return fmt.Errorf("go list raw context %s 非法=%q", root.name, root.value)
		}
	}
	if !reviewFreezePrefixedSHA256V1.MatchString(rawContext.GoArchiveSHA256) || rawContext.GoArchiveSHA256 == reviewFreezeSHA256V1(nil) {
		return fmt.Errorf("go list raw context Go archive digest 非法=%q", rawContext.GoArchiveSHA256)
	}
	if err := reviewFreezeValidateCompileAttestationModuleV1(rawContext.ExternalModule); err != nil {
		return fmt.Errorf("go list raw context external module: %w", err)
	}
	return nil
}

func reviewFreezeValidateCompileAttestationGoListRawPackageCommonV1(rawPackage reviewFreezeCompileAttestationGoListRawPackageV1) error {
	if rawPackage.ImportPath == "" || !utf8.ValidString(rawPackage.ImportPath) || strings.IndexByte(rawPackage.ImportPath, 0) >= 0 {
		return fmt.Errorf("go list raw import path 非法=%q", rawPackage.ImportPath)
	}
	if rawPackage.Incomplete || rawPackage.Error != nil || len(rawPackage.DepsErrors) != 0 {
		return fmt.Errorf("go list raw package incomplete/error/deps_errors=%q incomplete=%v error=%+v deps=%+v", rawPackage.ImportPath, rawPackage.Incomplete, rawPackage.Error, rawPackage.DepsErrors)
	}
	if rawPackage.Module != nil {
		if rawPackage.Module.Replace != nil {
			return fmt.Errorf("go list raw Module.Replace 禁止=%q", rawPackage.ImportPath)
		}
		if rawPackage.Module.Error != nil {
			return fmt.Errorf("go list raw module error=%q %+v", rawPackage.ImportPath, rawPackage.Module.Error)
		}
	}
	if rawPackage.Target != "" || rawPackage.Shlib != "" || rawPackage.ConflictDir != "" || rawPackage.Export != "" || rawPackage.BuildID != "" || rawPackage.BinaryOnly {
		return fmt.Errorf("go list raw 禁止 host/build artifact 字段=%q", rawPackage.ImportPath)
	}
	for _, hostPath := range []string{rawPackage.Dir, rawPackage.Root} {
		if !filepath.IsAbs(hostPath) || filepath.Clean(hostPath) != hostPath || strings.IndexByte(hostPath, 0) >= 0 {
			return fmt.Errorf("go list raw host path 非法=%q package=%q", hostPath, rawPackage.ImportPath)
		}
		if reviewFreezeCompileAttestationGoListRawContainsVendorV1(hostPath) {
			return fmt.Errorf("go list raw vendor path 禁止=%q", hostPath)
		}
	}
	if reviewFreezeCompileAttestationGoListRawContainsVendorV1(rawPackage.ImportPath) {
		return fmt.Errorf("go list raw vendor import 禁止=%q", rawPackage.ImportPath)
	}
	if rawPackage.Module != nil {
		for _, modulePath := range []string{rawPackage.Module.Dir, rawPackage.Module.GoMod} {
			if !filepath.IsAbs(modulePath) || filepath.Clean(modulePath) != modulePath || reviewFreezeCompileAttestationGoListRawContainsVendorV1(modulePath) {
				return fmt.Errorf("go list raw module path/vendor 非法=%q", modulePath)
			}
		}
	}
	return nil
}

func reviewFreezeCanonicalizeCompileAttestationGoListRawRegularPackageV1(rawPackage reviewFreezeCompileAttestationGoListRawPackageV1, rawContext reviewFreezeCompileAttestationGoListRawContextV1) (reviewFreezeGoListCanonicalPackageV1, error) {
	if rawPackage.ForTest != "" {
		return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw regular package ForTest 非法=%q", rawPackage.ImportPath)
	}
	if len(rawPackage.ImportMap) != 0 {
		return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw regular package ImportMap 禁止=%q", rawPackage.ImportPath)
	}
	if err := reviewFreezeValidateCompileAttestationGoListRawImportListV1(rawPackage.Imports, rawPackage.ImportPath+" imports"); err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	if err := reviewFreezeValidateCompileAttestationGoListRawImportListV1(rawPackage.Deps, rawPackage.ImportPath+" deps"); err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	if err := reviewFreezeValidateCompileAttestationGoListRawImportListV1(rawPackage.TestImports, rawPackage.ImportPath+" test imports"); err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	if err := reviewFreezeValidateCompileAttestationGoListRawImportListV1(rawPackage.XTestImports, rawPackage.ImportPath+" x_test imports"); err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}

	var packageKind string
	var canonicalDirectory string
	var canonicalModule reviewFreezeGoListCanonicalModuleV1
	switch {
	case rawPackage.Module == nil:
		packageKind = "stdlib"
		if !rawPackage.Goroot || !rawPackage.Standard || !rawPackage.DepOnly {
			return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw stdlib identity 漂移=%q", rawPackage.ImportPath)
		}
		if err := reviewFreezeValidateSafePathV1(rawPackage.ImportPath, ""); err != nil {
			return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw stdlib import: %w", err)
		}
		wantDirectory := filepath.Join(rawContext.GoRoot, "src", filepath.FromSlash(rawPackage.ImportPath))
		if rawPackage.Root != rawContext.GoRoot || rawPackage.Dir != wantDirectory || len(rawPackage.Match) != 0 {
			return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw stdlib root/dir/match 漂移=%q", rawPackage.ImportPath)
		}
		canonicalDirectory = "src/" + rawPackage.ImportPath
		canonicalModule = reviewFreezeGoListCanonicalModuleV1{Kind: "stdlib", GoArchiveSHA256: rawContext.GoArchiveSHA256}
	case rawPackage.Module.Main:
		packageKind = "target"
		if rawPackage.ImportPath != reviewFreezeCompileAttestationTargetImportV1 {
			return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw 出现未知 main-module package=%q", rawPackage.ImportPath)
		}
		if err := reviewFreezeValidateCompileAttestationGoListRawMainModuleV1(*rawPackage.Module, rawContext); err != nil {
			return reviewFreezeGoListCanonicalPackageV1{}, err
		}
		wantDirectory := filepath.Join(rawContext.ModuleRoot, filepath.FromSlash(strings.TrimPrefix(reviewFreezeCompileAttestationPackagePathV1, "agent/")))
		if rawPackage.Goroot || rawPackage.Standard || rawPackage.DepOnly || rawPackage.Root != rawContext.ModuleRoot || rawPackage.Dir != wantDirectory || !reflect.DeepEqual(rawPackage.Match, []string{reviewFreezeCompileAttestationPackagePatternV1}) {
			return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw target root/dir/match identity 漂移")
		}
		canonicalDirectory = strings.TrimPrefix(reviewFreezeCompileAttestationPackagePathV1, "agent/")
		canonicalModule = reviewFreezeGoListCanonicalModuleV1{Kind: "main", Path: reviewFreezeCompileAttestationModulePathV1, GoVersion: "1.26"}
	case rawPackage.Module.Path == rawContext.ExternalModule.ModulePath:
		packageKind = "external"
		if err := reviewFreezeValidateCompileAttestationGoListRawExternalModuleV1(*rawPackage.Module, rawContext); err != nil {
			return reviewFreezeGoListCanonicalPackageV1{}, err
		}
		relativeImport := strings.TrimPrefix(rawPackage.ImportPath, rawContext.ExternalModule.ModulePath)
		if relativeImport == rawPackage.ImportPath || !strings.HasPrefix(relativeImport, "/") {
			return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw external package 不属于锁定module=%q", rawPackage.ImportPath)
		}
		canonicalDirectory = strings.TrimPrefix(relativeImport, "/")
		wantRoot := filepath.Join(rawContext.ModuleCacheRoot, filepath.FromSlash(rawContext.ExternalModule.ModulePath)+"@"+rawContext.ExternalModule.Version)
		wantDirectory := filepath.Join(wantRoot, filepath.FromSlash(canonicalDirectory))
		if rawPackage.Goroot || rawPackage.Standard || !rawPackage.DepOnly || rawPackage.Root != wantRoot || rawPackage.Dir != wantDirectory || len(rawPackage.Match) != 0 {
			return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw external root/dir/match identity 漂移=%q", rawPackage.ImportPath)
		}
		canonicalModule = reviewFreezeGoListCanonicalModuleV1{
			Kind:      "external",
			Path:      rawContext.ExternalModule.ModulePath,
			Version:   rawContext.ExternalModule.Version,
			ModuleSum: rawContext.ExternalModule.ModuleSum,
			GoModSum:  rawContext.ExternalModule.GoModSum,
		}
	default:
		return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw 出现未锁定 external package/module=%q/%q", rawPackage.ImportPath, rawPackage.Module.Path)
	}

	if packageKind != "stdlib" && len(rawPackage.HFiles) != 0 {
		return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw target/external HFiles 禁止=%q", rawPackage.ImportPath)
	}
	if len(rawPackage.CgoCFLAGS)+len(rawPackage.CgoCPPFLAGS)+len(rawPackage.CgoCXXFLAGS)+len(rawPackage.CgoFFLAGS)+len(rawPackage.CgoLDFLAGS)+len(rawPackage.CgoPkgConfig) != 0 {
		return reviewFreezeGoListCanonicalPackageV1{}, fmt.Errorf("go list raw cgo flags 禁止=%q", rawPackage.ImportPath)
	}
	for _, field := range []struct {
		name   string
		values []string
	}{
		{name: "ignored_go_files", values: rawPackage.IgnoredGoFiles},
		{name: "ignored_other_files", values: rawPackage.IgnoredOtherFiles},
		{name: "h_files", values: rawPackage.HFiles},
		{name: "embed_patterns", values: rawPackage.EmbedPatterns},
		{name: "test_embed_patterns", values: rawPackage.TestEmbedPatterns},
		{name: "x_test_embed_patterns", values: rawPackage.XTestEmbedPatterns},
	} {
		if err := reviewFreezeValidateCompileAttestationGoListRawRelativeSetV1(field.values, rawPackage.ImportPath+" "+field.name, true); err != nil {
			return reviewFreezeGoListCanonicalPackageV1{}, err
		}
	}

	goFiles, err := reviewFreezeCanonicalizeCompileAttestationGoListRawFileSetV1(rawPackage.GoFiles, rawPackage.Dir, canonicalDirectory, rawPackage.ImportPath+" go_files", false)
	if err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	compiledGoFiles, err := reviewFreezeCanonicalizeCompileAttestationGoListRawFileSetV1(rawPackage.CompiledGoFiles, rawPackage.Dir, canonicalDirectory, rawPackage.ImportPath+" compiled_go_files", false)
	if err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	testGoFiles, err := reviewFreezeCanonicalizeCompileAttestationGoListRawFileSetV1(rawPackage.TestGoFiles, rawPackage.Dir, canonicalDirectory, rawPackage.ImportPath+" test_go_files", false)
	if err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	xTestGoFiles, err := reviewFreezeCanonicalizeCompileAttestationGoListRawFileSetV1(rawPackage.XTestGoFiles, rawPackage.Dir, canonicalDirectory, rawPackage.ImportPath+" x_test_go_files", false)
	if err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	embedFiles, err := reviewFreezeCanonicalizeCompileAttestationGoListRawFileSetV1(rawPackage.EmbedFiles, rawPackage.Dir, canonicalDirectory, rawPackage.ImportPath+" embed_files", true)
	if err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	testEmbedFiles, err := reviewFreezeCanonicalizeCompileAttestationGoListRawFileSetV1(rawPackage.TestEmbedFiles, rawPackage.Dir, canonicalDirectory, rawPackage.ImportPath+" test_embed_files", true)
	if err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	xTestEmbedFiles, err := reviewFreezeCanonicalizeCompileAttestationGoListRawFileSetV1(rawPackage.XTestEmbedFiles, rawPackage.Dir, canonicalDirectory, rawPackage.ImportPath+" x_test_embed_files", true)
	if err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	otherBuildInputs, err := reviewFreezeCanonicalizeCompileAttestationGoListRawOtherInputsV1(rawPackage, canonicalDirectory)
	if err != nil {
		return reviewFreezeGoListCanonicalPackageV1{}, err
	}
	canonicalTestGoFiles := []string{}
	canonicalXTestGoFiles := []string{}
	canonicalTestImports := []string{}
	canonicalXTestImports := []string{}
	canonicalTestEmbedFiles := []string{}
	canonicalXTestEmbedFiles := []string{}
	if packageKind == "target" {
		canonicalTestGoFiles = testGoFiles
		canonicalXTestGoFiles = xTestGoFiles
		canonicalTestImports = reviewFreezeCompileAttestationGoListRawExplicitStringsV1(rawPackage.TestImports)
		canonicalXTestImports = reviewFreezeCompileAttestationGoListRawExplicitStringsV1(rawPackage.XTestImports)
		canonicalTestEmbedFiles = testEmbedFiles
		canonicalXTestEmbedFiles = xTestEmbedFiles
	}

	return reviewFreezeGoListCanonicalPackageV1{
		ImportPath:       rawPackage.ImportPath,
		PackageName:      rawPackage.Name,
		PackageKind:      packageKind,
		Module:           canonicalModule,
		GoFiles:          goFiles,
		CompiledGoFiles:  compiledGoFiles,
		TestGoFiles:      canonicalTestGoFiles,
		XTestGoFiles:     canonicalXTestGoFiles,
		Imports:          reviewFreezeCompileAttestationGoListRawExplicitStringsV1(rawPackage.Imports),
		TestImports:      canonicalTestImports,
		XTestImports:     canonicalXTestImports,
		EmbedFiles:       embedFiles,
		TestEmbedFiles:   canonicalTestEmbedFiles,
		XTestEmbedFiles:  canonicalXTestEmbedFiles,
		OtherBuildInputs: otherBuildInputs,
	}, nil
}

func reviewFreezeValidateCompileAttestationGoListRawMainModuleV1(module reviewFreezeCompileAttestationGoListRawModuleV1, rawContext reviewFreezeCompileAttestationGoListRawContextV1) error {
	wantGoMod := filepath.Join(rawContext.ModuleRoot, "go.mod")
	if module.Path != reviewFreezeCompileAttestationModulePathV1 || !module.Main || module.Version != "" || module.Time != "" || module.Indirect || module.Dir != rawContext.ModuleRoot || module.GoMod != wantGoMod || module.GoVersion != "1.26" || module.Sum != "" || module.GoModSum != "" || module.Replace != nil || module.Error != nil {
		return fmt.Errorf("go list raw main module identity 漂移=%+v", module)
	}
	return nil
}

func reviewFreezeValidateCompileAttestationGoListRawExternalModuleV1(module reviewFreezeCompileAttestationGoListRawModuleV1, rawContext reviewFreezeCompileAttestationGoListRawContextV1) error {
	wantRoot := filepath.Join(rawContext.ModuleCacheRoot, filepath.FromSlash(rawContext.ExternalModule.ModulePath)+"@"+rawContext.ExternalModule.Version)
	wantGoMod := filepath.Join(rawContext.ModuleCacheRoot, "cache", "download", filepath.FromSlash(rawContext.ExternalModule.ModulePath), "@v", rawContext.ExternalModule.Version+".mod")
	// Time 仅在 cache 中存在 acquisition `.info` 时由 go list 输出；删除 `.info` 不影响
	// go list/go test -c，因此空值与锁定的 provenance 时间应规范化为同一 build-selection
	// projection。ModuleSum 与 `.ziphash` 文件的摘要绑定由 snapshot/material 层负责。
	if module.Path != rawContext.ExternalModule.ModulePath || module.Version != rawContext.ExternalModule.Version ||
		(module.Time != "" && module.Time != reviewFreezeCompileInputSnapshotModuleInfoTimeV1) ||
		module.Main || module.Indirect || module.Dir != wantRoot || module.GoMod != wantGoMod || module.GoVersion != "1.24.0" || module.Sum != rawContext.ExternalModule.ModuleSum || module.GoModSum != rawContext.ExternalModule.GoModSum || module.Replace != nil || module.Error != nil {
		return fmt.Errorf("go list raw external module identity 漂移=%+v", module)
	}
	return nil
}

func reviewFreezeCanonicalizeCompileAttestationGoListRawOtherInputsV1(rawPackage reviewFreezeCompileAttestationGoListRawPackageV1, canonicalDirectory string) (reviewFreezeGoListOtherBuildInputsV1, error) {
	inputs := reviewFreezeGoListOtherBuildInputsV1{}
	fields := []struct {
		name   string
		raw    []string
		target *[]string
	}{
		{name: "cgo_files", raw: rawPackage.CgoFiles, target: &inputs.CgoFiles},
		{name: "c_files", raw: rawPackage.CFiles, target: &inputs.CFiles},
		{name: "cxx_files", raw: rawPackage.CXXFiles, target: &inputs.CXXFiles},
		{name: "m_files", raw: rawPackage.MFiles, target: &inputs.MFiles},
		{name: "h_files", raw: rawPackage.HFiles, target: &inputs.HFiles},
		{name: "f_files", raw: rawPackage.FFiles, target: &inputs.FFiles},
		{name: "s_files", raw: rawPackage.SFiles, target: &inputs.SFiles},
		{name: "syso_files", raw: rawPackage.SysoFiles, target: &inputs.SysoFiles},
		{name: "swig_files", raw: rawPackage.SwigFiles, target: &inputs.SwigFiles},
		{name: "swig_cxx_files", raw: rawPackage.SwigCXXFiles, target: &inputs.SwigCXXFiles},
	}
	for _, field := range fields {
		canonical, err := reviewFreezeCanonicalizeCompileAttestationGoListRawFileSetV1(field.raw, rawPackage.Dir, canonicalDirectory, rawPackage.ImportPath+" "+field.name, false)
		if err != nil {
			return reviewFreezeGoListOtherBuildInputsV1{}, err
		}
		*field.target = canonical
	}
	return inputs, nil
}

func reviewFreezeCanonicalizeCompileAttestationGoListRawFileSetV1(values []string, rawDirectory, canonicalDirectory, field string, allowNested bool) ([]string, error) {
	if err := reviewFreezeValidateCompileAttestationGoListRawRelativeSetV1(values, field, allowNested); err != nil {
		return nil, err
	}
	canonical := make([]string, 0, len(values))
	for _, value := range values {
		rawPath := filepath.Join(rawDirectory, filepath.FromSlash(value))
		if !reviewFreezeCompileAttestationGoListRawPathWithinV1(rawDirectory, rawPath) {
			return nil, fmt.Errorf("go list raw %s 路径逃逸=%q", field, value)
		}
		canonical = append(canonical, pathpkg.Join(canonicalDirectory, value))
	}
	return canonical, nil
}

func reviewFreezeValidateCompileAttestationGoListRawRelativeSetV1(values []string, field string, allowNested bool) error {
	last := ""
	for _, value := range values {
		if value == "" || !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 || strings.Contains(value, "\\") || pathpkg.IsAbs(value) || pathpkg.Clean(value) != value || value == ".." || strings.HasPrefix(value, "../") {
			return fmt.Errorf("go list raw %s 不安全路径=%q", field, value)
		}
		if !allowNested && strings.Contains(value, "/") {
			return fmt.Errorf("go list raw %s 禁止嵌套路径=%q", field, value)
		}
		if value <= last {
			return fmt.Errorf("go list raw %s 未排序或重复=%q", field, value)
		}
		last = value
	}
	return nil
}

func reviewFreezeValidateCompileAttestationGoListRawImportListV1(values []string, field string) error {
	last := ""
	for _, value := range values {
		if value == "" || strings.Contains(value, " [") || strings.HasSuffix(value, ".test") {
			return fmt.Errorf("go list raw %s 非法 synthetic import=%q", field, value)
		}
		if err := reviewFreezeValidateSafePathV1(value, ""); err != nil {
			return fmt.Errorf("go list raw %s: %w", field, err)
		}
		if value <= last {
			return fmt.Errorf("go list raw %s 未排序或重复=%q", field, value)
		}
		last = value
	}
	return nil
}

func reviewFreezeValidateCompileAttestationGoListRawDependencyClosuresV1(packages map[string]reviewFreezeCompileAttestationGoListRawPackageV1) error {
	importPaths := make([]string, 0, len(packages))
	for importPath := range packages {
		importPaths = append(importPaths, importPath)
	}
	sort.Strings(importPaths)
	for _, rootImportPath := range importPaths {
		closure := make(map[string]struct{}, len(packages)-1)
		state := make(map[string]uint8, len(packages))
		var visit func(string) error
		visit = func(importPath string) error {
			switch state[importPath] {
			case 1:
				return fmt.Errorf("go list raw regular import graph cycle=%q", importPath)
			case 2:
				return nil
			}
			selectedPackage, ok := packages[importPath]
			if !ok {
				return fmt.Errorf("go list raw regular import 不在 129 exact-set=%q", importPath)
			}
			state[importPath] = 1
			for _, dependency := range selectedPackage.Imports {
				if _, ok := packages[dependency]; !ok {
					return fmt.Errorf("go list raw regular import 不在 129 exact-set=%q from=%q", dependency, importPath)
				}
				closure[dependency] = struct{}{}
				if err := visit(dependency); err != nil {
					return err
				}
			}
			state[importPath] = 2
			return nil
		}
		if err := visit(rootImportPath); err != nil {
			return err
		}
		wantDeps := make([]string, 0, len(closure))
		for dependency := range closure {
			wantDeps = append(wantDeps, dependency)
		}
		sort.Strings(wantDeps)
		gotDeps := reviewFreezeCompileAttestationGoListRawExplicitStringsV1(packages[rootImportPath].Deps)
		if !reflect.DeepEqual(gotDeps, wantDeps) {
			return fmt.Errorf("go list raw regular Deps exact-set 漂移=%q got=%v want=%v", rootImportPath, gotDeps, wantDeps)
		}
	}
	return nil
}

func reviewFreezeValidateCompileAttestationGoListRawTargetVariantV1(target, variant reviewFreezeCompileAttestationGoListRawPackageV1, rawContext reviewFreezeCompileAttestationGoListRawContextV1) error {
	wantImportPath := reviewFreezeCompileAttestationTargetImportV1 + " [" + reviewFreezeCompileAttestationTargetImportV1 + ".test]"
	if variant.ImportPath != wantImportPath || variant.ForTest != reviewFreezeCompileAttestationTargetImportV1 || variant.Name != target.Name || variant.Dir != target.Dir || variant.Root != target.Root || variant.Goroot || variant.Standard || variant.DepOnly || !reflect.DeepEqual(variant.Match, target.Match) || len(variant.ImportMap) != 0 {
		return fmt.Errorf("go list raw target test variant identity 漂移=%+v", variant)
	}
	if variant.Module == nil {
		return fmt.Errorf("go list raw target test variant module 缺失")
	}
	if err := reviewFreezeValidateCompileAttestationGoListRawMainModuleV1(*variant.Module, rawContext); err != nil {
		return err
	}
	wantGoFiles := append(append([]string{}, target.GoFiles...), target.TestGoFiles...)
	wantImports := append([]string{"testing"}, target.Imports...)
	if !reflect.DeepEqual(variant.GoFiles, wantGoFiles) || !reflect.DeepEqual(variant.CompiledGoFiles, wantGoFiles) || !reflect.DeepEqual(variant.TestGoFiles, target.TestGoFiles) || !reflect.DeepEqual(variant.XTestGoFiles, target.XTestGoFiles) || !reflect.DeepEqual(variant.Imports, wantImports) || !reflect.DeepEqual(variant.Deps, target.Deps) || !reflect.DeepEqual(variant.TestImports, target.TestImports) || !reflect.DeepEqual(variant.XTestImports, target.XTestImports) || !reflect.DeepEqual(variant.IgnoredGoFiles, target.IgnoredGoFiles) || !reflect.DeepEqual(variant.IgnoredOtherFiles, target.IgnoredOtherFiles) {
		return fmt.Errorf("go list raw target test variant source/import 结构漂移")
	}
	if reviewFreezeCompileAttestationGoListRawHasSelectedAuxiliaryInputsV1(variant) {
		return fmt.Errorf("go list raw target test variant 含未知 selected input")
	}
	return nil
}

func reviewFreezeValidateCompileAttestationGoListRawSyntheticMainV1(synthetic reviewFreezeCompileAttestationGoListRawPackageV1, rawContext reviewFreezeCompileAttestationGoListRawContextV1, wantDeps []string) error {
	wantVariant := reviewFreezeCompileAttestationTargetImportV1 + " [" + reviewFreezeCompileAttestationTargetImportV1 + ".test]"
	wantImports := []string{wantVariant, "os", "reflect", "testing", "testing/internal/testdeps"}
	wantImportMap := map[string]string{reviewFreezeCompileAttestationTargetImportV1: wantVariant}
	wantDirectory := filepath.Join(rawContext.ModuleRoot, filepath.FromSlash(strings.TrimPrefix(reviewFreezeCompileAttestationPackagePathV1, "agent/")))
	if synthetic.ImportPath != reviewFreezeCompileAttestationTargetImportV1+".test" || synthetic.ForTest != "" || synthetic.Name != "main" || synthetic.Dir != wantDirectory || synthetic.Root != rawContext.ModuleRoot || synthetic.Goroot || synthetic.Standard || synthetic.DepOnly || len(synthetic.Match) != 0 || !reflect.DeepEqual(synthetic.Imports, wantImports) || !reflect.DeepEqual(synthetic.ImportMap, wantImportMap) || !reflect.DeepEqual(synthetic.Deps, wantDeps) {
		return fmt.Errorf("go list raw synthetic test main identity/import map 漂移=%+v", synthetic)
	}
	if synthetic.Module == nil {
		return fmt.Errorf("go list raw synthetic test main module 缺失")
	}
	if err := reviewFreezeValidateCompileAttestationGoListRawMainModuleV1(*synthetic.Module, rawContext); err != nil {
		return err
	}
	if len(synthetic.GoFiles) != 1 || !reviewFreezeValidateCompileAttestationGoListRawGeneratedPathV1(rawContext.GoCacheRoot, synthetic.GoFiles[0]) {
		return fmt.Errorf("go list raw synthetic test main generated path 非法=%v", synthetic.GoFiles)
	}
	if len(synthetic.CompiledGoFiles) != 0 || len(synthetic.TestGoFiles) != 0 || len(synthetic.XTestGoFiles) != 0 || len(synthetic.TestImports) != 0 || len(synthetic.XTestImports) != 0 || reviewFreezeCompileAttestationGoListRawHasSelectedAuxiliaryInputsV1(synthetic) {
		return fmt.Errorf("go list raw synthetic test main 含未知结构/input")
	}
	return nil
}

func reviewFreezeCompileAttestationGoListRawHasSelectedAuxiliaryInputsV1(rawPackage reviewFreezeCompileAttestationGoListRawPackageV1) bool {
	return len(rawPackage.CgoFiles)+len(rawPackage.CFiles)+len(rawPackage.CXXFiles)+len(rawPackage.MFiles)+len(rawPackage.HFiles)+len(rawPackage.FFiles)+len(rawPackage.SFiles)+len(rawPackage.SwigFiles)+len(rawPackage.SwigCXXFiles)+len(rawPackage.SysoFiles)+len(rawPackage.EmbedPatterns)+len(rawPackage.EmbedFiles)+len(rawPackage.TestEmbedPatterns)+len(rawPackage.TestEmbedFiles)+len(rawPackage.XTestEmbedPatterns)+len(rawPackage.XTestEmbedFiles)+len(rawPackage.CgoCFLAGS)+len(rawPackage.CgoCPPFLAGS)+len(rawPackage.CgoCXXFLAGS)+len(rawPackage.CgoFFLAGS)+len(rawPackage.CgoLDFLAGS)+len(rawPackage.CgoPkgConfig) != 0
}

func reviewFreezeCompileAttestationGoListRawExplicitStringsV1(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string{}, values...)
}

func reviewFreezeCompileAttestationGoListRawContainsVendorV1(value string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(value), "/") {
		if segment == "vendor" {
			return true
		}
	}
	return false
}

func reviewFreezeCompileAttestationGoListRawPathWithinV1(root, candidate string) bool {
	if !filepath.IsAbs(root) || !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
		return false
	}
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func reviewFreezeValidateCompileAttestationGoListRawGeneratedPathV1(goCacheRoot, generatedPath string) bool {
	if !reviewFreezeCompileAttestationGoListRawPathWithinV1(goCacheRoot, generatedPath) {
		return false
	}
	relative, err := filepath.Rel(goCacheRoot, generatedPath)
	if err != nil {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	if len(parts) != 2 || len(parts[0]) != 2 || !strings.HasSuffix(parts[1], "-d") {
		return false
	}
	digest := strings.TrimSuffix(parts[1], "-d")
	if len(digest) != 64 || parts[0] != digest[:2] {
		return false
	}
	for _, character := range parts[0] + digest {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func reviewFreezeCompileAttestationGoListRawProjectionSHA256V1(projection reviewFreezeGoListCanonicalProjectionV1) (string, error) {
	raw, err := json.Marshal(projection)
	if err != nil {
		return "", fmt.Errorf("marshal go list canonical projection: %w", err)
	}
	return reviewFreezeSHA256V1(raw), nil
}

// reviewFreezeVerifyCompileAttestationGoListRawPairV1 独立解析两份 raw，再比较规范化结果；
// 调用方不能把任一 statement 自报的 projection SHA 当作另一份 raw 的替代证据。
func reviewFreezeVerifyCompileAttestationGoListRawPairV1(firstRaw []byte, firstContext reviewFreezeCompileAttestationGoListRawContextV1, secondRaw []byte, secondContext reviewFreezeCompileAttestationGoListRawContextV1) (reviewFreezeGoListCanonicalProjectionV1, error) {
	firstProjection, err := reviewFreezeCanonicalizeCompileAttestationGoListRawV1(firstRaw, firstContext)
	if err != nil {
		return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("first raw: %w", err)
	}
	secondProjection, err := reviewFreezeCanonicalizeCompileAttestationGoListRawV1(secondRaw, secondContext)
	if err != nil {
		return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("second raw: %w", err)
	}
	if !reflect.DeepEqual(firstProjection, secondProjection) {
		firstSHA, _ := reviewFreezeCompileAttestationGoListRawProjectionSHA256V1(firstProjection)
		secondSHA, _ := reviewFreezeCompileAttestationGoListRawProjectionSHA256V1(secondProjection)
		return reviewFreezeGoListCanonicalProjectionV1{}, fmt.Errorf("go list raw pair canonical projection mismatch=%q/%q", firstSHA, secondSHA)
	}
	return firstProjection, nil
}

func reviewFreezeTestCompileAttestationGoListRawExternalModuleV1() reviewFreezeCompileAttestationModuleV1 {
	return reviewFreezeCompileAttestationModuleV1{
		ModulePath:            reviewFreezeXTextModulePathV1,
		Version:               reviewFreezeXTextModuleVersionV1,
		ZipSHA256:             "sha256:67a6cab352a4f313d56671618dfff2f82d908a17151dc4ca9b7fbbfd40828134",
		ZipSizeBytes:          7015063,
		ZipEntryCount:         488,
		ZipUncompressedBytes:  29567429,
		ModuleSum:             "h1:oL/Qq0Kdaqxa1KbNeMKwQq0reLCCaFtqu2eNuSeNHbk=",
		GoModSHA256:           "sha256:43b8c43b254e9c1895aca686f562dc9029ec3051f5fc568c56eff7c282084dcf",
		GoModSizeBytes:        190,
		GoModSum:              "h1:homfLqTYRFyVYemLBFl5GgL/DWEiH5wcsQ5gSh1yziA=",
		ZipRootGoModSHA256:    "sha256:43b8c43b254e9c1895aca686f562dc9029ec3051f5fc568c56eff7c282084dcf",
		ZipRootGoModSizeBytes: 190,
		HashDirBefore:         "h1:oL/Qq0Kdaqxa1KbNeMKwQq0reLCCaFtqu2eNuSeNHbk=",
		HashDirAfter:          "h1:oL/Qq0Kdaqxa1KbNeMKwQq0reLCCaFtqu2eNuSeNHbk=",
		Packages:              reviewFreezeExpectedCompileAttestationExternalPackagesV1(),
	}
}

func reviewFreezeTestCompileAttestationGoListRawModuleRootV1(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve go list raw test source path")
	}
	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	if _, err := os.Stat(filepath.Join(moduleRoot, "go.mod")); err != nil {
		t.Fatalf("resolve agent module root %q: %v", moduleRoot, err)
	}
	return moduleRoot
}

func reviewFreezeTestCompileAttestationGoListRawModuleCacheV1(t *testing.T, goBinary, moduleRoot string) string {
	t.Helper()
	// 预置的全局 GOMODCACHE 只为 real-raw golden 提供离线执行输入，不构成
	// 15-file snapshot/material 完整性证明；该证明由独立 snapshot/material 校验层完成。
	home := os.Getenv("HOME")
	if home == "" {
		t.Fatal("HOME is required only to resolve the pre-provisioned offline module cache")
	}
	commandContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, goBinary, "env", "GOMODCACHE")
	command.Dir = moduleRoot
	command.Env = []string{
		"HOME=" + home,
		"GOROOT=" + runtime.GOROOT(),
		"GOENV=off",
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"GOTELEMETRY=off",
	}
	output, err := command.Output()
	if err != nil {
		t.Fatalf("resolve offline GOMODCACHE: %v", err)
	}
	moduleCache := filepath.Clean(strings.TrimSpace(string(output)))
	if !filepath.IsAbs(moduleCache) {
		t.Fatalf("offline GOMODCACHE must be absolute: %q", moduleCache)
	}
	moduleRootPath := filepath.Join(moduleCache, "golang.org", "x", "text@v0.34.0")
	goModPath := filepath.Join(moduleCache, "cache", "download", "golang.org", "x", "text", "@v", "v0.34.0.mod")
	for _, required := range []string{moduleRootPath, goModPath} {
		if _, statErr := os.Stat(required); statErr != nil {
			t.Fatalf("offline go list material missing %q: %v; provision agent modules before governance tests", required, statErr)
		}
	}
	return moduleCache
}

func reviewFreezeTestCompileAttestationRunGoListRawV1(t *testing.T) ([]byte, reviewFreezeCompileAttestationGoListRawContextV1) {
	t.Helper()
	if runtime.Version() != "go1.26.3" {
		t.Fatalf("go list raw golden requires go1.26.3, got %q", runtime.Version())
	}
	goRoot := filepath.Clean(runtime.GOROOT())
	goBinary := filepath.Join(goRoot, "bin", "go")
	moduleRoot := reviewFreezeTestCompileAttestationGoListRawModuleRootV1(t)
	moduleCache := reviewFreezeTestCompileAttestationGoListRawModuleCacheV1(t, goBinary, moduleRoot)
	goCache := filepath.Clean(t.TempDir())
	goTmp := filepath.Clean(t.TempDir())

	commandContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(
		commandContext,
		goBinary,
		"list", "-deps", "-test", "-compiled", "-json", "-mod=readonly", "-trimpath", "-p=1",
		reviewFreezeCompileAttestationPackagePatternV1,
	)
	command.Dir = moduleRoot
	command.Env = []string{
		"HOME=/nonexistent",
		"TMPDIR=" + goTmp,
		"TZ=UTC",
		"LANG=C",
		"LC_ALL=C",
		"GOROOT=" + goRoot,
		"GOMODCACHE=" + moduleCache,
		"GOCACHE=" + goCache,
		"GOTMPDIR=" + goTmp,
		"GOPATH=/nonexistent/go",
		"GOENV=off",
		"GOWORK=off",
		"GOTOOLCHAIN=local",
		"GOOS=linux",
		"GOARCH=amd64",
		"GOAMD64=v1",
		"CGO_ENABLED=0",
		"GOEXPERIMENT=",
		"GOFIPS140=off",
		"GOFLAGS=",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOVCS=off",
		"GOAUTH=off",
		"GOTELEMETRY=off",
		"GODEBUG=",
		"GOMAXPROCS=1",
		"GOMEMLIMIT=off",
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	raw, err := command.Output()
	if err != nil {
		t.Fatalf("run offline go list: %v stderr=%s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("offline go list wrote stderr: %s", stderr.String())
	}
	if len(raw) == 0 || len(raw) > reviewFreezeCompileAttestationGoListRawMaxBytesV1 {
		t.Fatalf("offline go list raw size=%d", len(raw))
	}
	return raw, reviewFreezeCompileAttestationGoListRawContextV1{
		GoRoot:          goRoot,
		ModuleRoot:      moduleRoot,
		ModuleCacheRoot: moduleCache,
		GoCacheRoot:     goCache,
		// 这里只为验证 raw canonicalizer 的 test-only toolchain union 提供非空占位。
		// 它不是实际 Go archive 摘要，不能作为 toolchain provenance 或 authority。
		GoArchiveSHA256: reviewFreezeSHA256V1([]byte("go1.26.3 linux-amd64 archive")),
		ExternalModule:  reviewFreezeTestCompileAttestationGoListRawExternalModuleV1(),
	}
}

func reviewFreezeTestCompileAttestationGoListRawMessagesV1(t *testing.T, raw []byte) []json.RawMessage {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	messages := make([]json.RawMessage, 0, reviewFreezeCompileAttestationGoListRawPackageCountV1)
	for {
		var message json.RawMessage
		if err := decoder.Decode(&message); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode raw mutation fixture: %v", err)
		}
		messages = append(messages, append(json.RawMessage{}, message...))
	}
	return messages
}

func reviewFreezeTestCompileAttestationGoListRawJoinV1(messages []json.RawMessage) []byte {
	var output bytes.Buffer
	for _, message := range messages {
		output.Write(message)
		output.WriteByte('\n')
	}
	return output.Bytes()
}

func reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t *testing.T, raw []byte, importPath string, mutate func(map[string]any)) []byte {
	t.Helper()
	messages := reviewFreezeTestCompileAttestationGoListRawMessagesV1(t, raw)
	found := false
	for index, message := range messages {
		var object map[string]any
		if err := json.Unmarshal(message, &object); err != nil {
			t.Fatalf("decode raw package mutation: %v", err)
		}
		if object["ImportPath"] != importPath {
			continue
		}
		mutate(object)
		mutated, err := json.Marshal(object)
		if err != nil {
			t.Fatalf("encode raw package mutation: %v", err)
		}
		messages[index] = mutated
		found = true
		break
	}
	if !found {
		t.Fatalf("raw package %q not found", importPath)
	}
	return reviewFreezeTestCompileAttestationGoListRawJoinV1(messages)
}

func reviewFreezeTestCompileAttestationGoListRawDuplicateFieldV1(t *testing.T, raw []byte) []byte {
	t.Helper()
	messages := reviewFreezeTestCompileAttestationGoListRawMessagesV1(t, raw)
	if len(messages) == 0 || len(messages[0]) < 2 || messages[0][0] != '{' {
		t.Fatal("raw stream first object missing")
	}
	messages[0] = append([]byte(`{"ImportPath":"duplicate",`), messages[0][1:]...)
	return reviewFreezeTestCompileAttestationGoListRawJoinV1(messages)
}

func TestW2ReviewFreezeCompileAttestationGoListRawGoldenV1(t *testing.T) {
	firstRaw, firstContext := reviewFreezeTestCompileAttestationRunGoListRawV1(t)
	secondRaw, secondContext := reviewFreezeTestCompileAttestationRunGoListRawV1(t)
	if bytes.Equal(firstRaw, secondRaw) {
		t.Fatal("fresh GOCACHE raw streams unexpectedly identical; synthetic main host path was not exercised")
	}
	projection, err := reviewFreezeVerifyCompileAttestationGoListRawPairV1(firstRaw, firstContext, secondRaw, secondContext)
	if err != nil {
		t.Fatalf("verify two real go list raw streams: %v", err)
	}
	projectionSHA, err := reviewFreezeCompileAttestationGoListRawProjectionSHA256V1(projection)
	if err != nil {
		t.Fatal(err)
	}
	if projectionSHA != reviewFreezeCompileAttestationGoListRawGoldenSHA256V1 {
		t.Fatalf("real go list canonical projection SHA=%q want=%q", projectionSHA, reviewFreezeCompileAttestationGoListRawGoldenSHA256V1)
	}
	if len(projection.Packages) != reviewFreezeValidateCompileAttestationGoListStandardPackageCountV1+len(firstContext.ExternalModule.Packages)+1 {
		t.Fatalf("canonical package count=%d", len(projection.Packages))
	}
	withoutInfoTime := reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, firstRaw, "golang.org/x/text/transform", func(object map[string]any) {
		delete(object["Module"].(map[string]any), "Time")
	})
	withoutInfoTime = reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, withoutInfoTime, "golang.org/x/text/unicode/norm", func(object map[string]any) {
		delete(object["Module"].(map[string]any), "Time")
	})
	withoutInfoProjection, err := reviewFreezeCanonicalizeCompileAttestationGoListRawV1(withoutInfoTime, firstContext)
	if err != nil || !reflect.DeepEqual(withoutInfoProjection, projection) {
		t.Fatalf("missing acquisition .info Time must preserve build projection: projection_equal=%v error=%v", reflect.DeepEqual(withoutInfoProjection, projection), err)
	}

	driftedSecondRaw := reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, secondRaw, "bytes", func(object map[string]any) {
		files := object["GoFiles"].([]any)
		object["GoFiles"] = append(files, "zz_pair_drift.go")
	})
	if _, err := reviewFreezeVerifyCompileAttestationGoListRawPairV1(firstRaw, firstContext, driftedSecondRaw, secondContext); err == nil || !strings.Contains(err.Error(), "canonical projection mismatch") {
		t.Fatalf("pair drift error=%v", err)
	}
	driftedHeaderRaw := reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, secondRaw, "runtime", func(object map[string]any) {
		headers := append(object["HFiles"].([]any), "zz_forged.h")
		object["HFiles"] = headers
	})
	if _, err := reviewFreezeVerifyCompileAttestationGoListRawPairV1(firstRaw, firstContext, driftedHeaderRaw, secondContext); err == nil || !strings.Contains(err.Error(), "canonical projection mismatch") {
		t.Fatalf("pair HFiles drift error=%v", err)
	}
}

func TestW2ReviewFreezeCompileAttestationGoListRawAdversarialV1(t *testing.T) {
	raw, rawContext := reviewFreezeTestCompileAttestationRunGoListRawV1(t)
	targetVariant := reviewFreezeCompileAttestationTargetImportV1 + " [" + reviewFreezeCompileAttestationTargetImportV1 + ".test]"
	targetSynthetic := reviewFreezeCompileAttestationTargetImportV1 + ".test"
	tests := []struct {
		name   string
		mutate func() []byte
		want   string
	}{
		{name: "duplicate field", mutate: func() []byte { return reviewFreezeTestCompileAttestationGoListRawDuplicateFieldV1(t, raw) }, want: "duplicate field"},
		{name: "unknown field", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "bytes", func(object map[string]any) { object["FutureField"] = true })
		}, want: "unknown field"},
		{name: "top-level case alias", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "bytes", func(object map[string]any) {
				object["DIR"] = object["Dir"]
				delete(object, "Dir")
			})
		}, want: "exact-case unknown field"},
		{name: "nested Module case alias", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "golang.org/x/text/transform", func(object map[string]any) {
				module := object["Module"].(map[string]any)
				module["PATH"] = module["Path"]
				delete(module, "Path")
			})
		}, want: "exact-case unknown field"},
		{name: "null", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "bytes", func(object map[string]any) { object["Dir"] = nil })
		}, want: "禁止 null"},
		{name: "trailing", mutate: func() []byte { return append(append([]byte{}, raw...), '!') }, want: "decode go list raw stream"},
		{name: "budget", mutate: func() []byte { return make([]byte, reviewFreezeCompileAttestationGoListRawMaxBytesV1+1) }, want: "raw stream size"},
		{name: "package count early stop", mutate: func() []byte {
			return bytes.Repeat([]byte("{}\n"), reviewFreezeCompileAttestationGoListRawPackageCountV1+1)
		}, want: "package count 超出"},
		{name: "Incomplete", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "bytes", func(object map[string]any) { object["Incomplete"] = true })
		}, want: "incomplete/error/deps_errors"},
		{name: "Error", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "bytes", func(object map[string]any) { object["Error"] = map[string]any{"Err": "forged"} })
		}, want: "incomplete/error/deps_errors"},
		{name: "DepsErrors", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "bytes", func(object map[string]any) { object["DepsErrors"] = []any{map[string]any{"Err": "forged"}} })
		}, want: "incomplete/error/deps_errors"},
		{name: "regular Deps closure drift", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "bytes", func(object map[string]any) {
				deps := append(object["Deps"].([]any), "crypto/sha256")
				sort.Slice(deps, func(i, j int) bool { return deps[i].(string) < deps[j].(string) })
				object["Deps"] = deps
			})
		}, want: "regular Deps exact-set"},
		{name: "regular Imports unknown", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "bytes", func(object map[string]any) {
				imports := append(object["Imports"].([]any), "example.invalid/unknown")
				sort.Slice(imports, func(i, j int) bool { return imports[i].(string) < imports[j].(string) })
				object["Imports"] = imports
			})
		}, want: "regular import 不在 129 exact-set"},
		{name: "regular Imports self cycle", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "bytes", func(object map[string]any) {
				imports := append(object["Imports"].([]any), "bytes")
				sort.Slice(imports, func(i, j int) bool { return imports[i].(string) < imports[j].(string) })
				object["Imports"] = imports
			})
		}, want: "regular import graph cycle"},
		{name: "Module.Replace", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "golang.org/x/text/transform", func(object map[string]any) {
				module := object["Module"].(map[string]any)
				module["Replace"] = map[string]any{"Path": "example.invalid/replacement", "Version": "v1.0.0", "Dir": rawContext.ModuleRoot, "GoMod": filepath.Join(rawContext.ModuleRoot, "go.mod")}
			})
		}, want: "Module.Replace"},
		{name: "Module.Time drift", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "golang.org/x/text/transform", func(object map[string]any) {
				object["Module"].(map[string]any)["Time"] = "2026-02-09T16:14:30Z"
			})
		}, want: "external module identity"},
		{name: "vendor", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "golang.org/x/text/transform", func(object map[string]any) {
				object["Dir"] = filepath.Join(rawContext.ModuleRoot, "vendor", "golang.org", "x", "text", "transform")
			})
		}, want: "vendor"},
		{name: "workspace multiple main", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "golang.org/x/text/transform", func(object map[string]any) {
				module := object["Module"].(map[string]any)
				module["Main"] = true
			})
		}, want: "workspace/multiple main"},
		{name: "unknown package", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, "golang.org/x/text/transform", func(object map[string]any) { object["ImportPath"] = "example.invalid/evil" })
		}, want: "不属于锁定module"},
		{name: "path escape", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, reviewFreezeCompileAttestationTargetImportV1, func(object map[string]any) {
				files := object["GoFiles"].([]any)
				files[0] = "../escape.go"
			})
		}, want: "不安全路径"},
		{name: "absolute selected path", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, reviewFreezeCompileAttestationTargetImportV1, func(object map[string]any) {
				files := object["GoFiles"].([]any)
				files[0] = filepath.Join(rawContext.ModuleRoot, "escape.go")
			})
		}, want: "不安全路径"},
		{name: "unknown target variant", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, targetVariant, func(object map[string]any) { object["ForTest"] = "example.invalid/other" })
		}, want: "target test variant"},
		{name: "target variant structure", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, targetVariant, func(object map[string]any) { object["Name"] = "other" })
		}, want: "test variant identity"},
		{name: "synthetic main structure", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, targetSynthetic, func(object map[string]any) {
				imports := object["Imports"].([]any)
				object["Imports"] = imports[:len(imports)-1]
			})
		}, want: "synthetic test main identity"},
		{name: "synthetic main generated path", mutate: func() []byte {
			return reviewFreezeTestCompileAttestationGoListRawMutatePackageV1(t, raw, targetSynthetic, func(object map[string]any) { object["GoFiles"] = []any{"/tmp/outside-cache/generated-d"} })
		}, want: "generated path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := reviewFreezeCanonicalizeCompileAttestationGoListRawV1(test.mutate(), rawContext)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v want contains %q", err, test.want)
			}
		})
	}
}
