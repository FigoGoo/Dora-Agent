package reviewfreeze_test

import (
	"bytes"
	"encoding/json"
	pathpkg "path"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func reviewFreezeCompileAttestationFixtureEmptyOtherBuildInputsV1() reviewFreezeGoListOtherBuildInputsV1 {
	return reviewFreezeGoListOtherBuildInputsV1{
		CgoFiles:     []string{},
		CFiles:       []string{},
		CXXFiles:     []string{},
		MFiles:       []string{},
		HFiles:       []string{},
		FFiles:       []string{},
		SFiles:       []string{},
		SysoFiles:    []string{},
		SwigFiles:    []string{},
		SwigCXXFiles: []string{},
	}
}

func reviewFreezeCompileAttestationFixtureTargetPackageV1() reviewFreezeGoListCanonicalPackageV1 {
	return reviewFreezeGoListCanonicalPackageV1{
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
		TestImports:      []string{"testing"},
		XTestImports:     []string{},
		EmbedFiles:       []string{},
		TestEmbedFiles:   []string{},
		XTestEmbedFiles:  []string{},
		OtherBuildInputs: reviewFreezeCompileAttestationFixtureEmptyOtherBuildInputsV1(),
	}
}

func reviewFreezeCompileAttestationFixtureExternalPackageV1(module reviewFreezeCompileAttestationModuleV1, externalPackage reviewFreezeCompileAttestationExternalPackageV1) reviewFreezeGoListCanonicalPackageV1 {
	sources := make([]string, 0, len(externalPackage.Sources))
	for _, source := range externalPackage.Sources {
		sources = append(sources, source.Path)
	}
	return reviewFreezeGoListCanonicalPackageV1{
		ImportPath:  externalPackage.ImportPath,
		PackageName: pathpkg.Base(externalPackage.ImportPath),
		PackageKind: "external",
		Module: reviewFreezeGoListCanonicalModuleV1{
			Kind:      "external",
			Path:      module.ModulePath,
			Version:   module.Version,
			ModuleSum: module.ModuleSum,
			GoModSum:  module.GoModSum,
		},
		GoFiles:          append([]string{}, sources...),
		CompiledGoFiles:  append([]string{}, sources...),
		TestGoFiles:      []string{},
		XTestGoFiles:     []string{},
		Imports:          append([]string{}, externalPackage.Imports...),
		TestImports:      []string{},
		XTestImports:     []string{},
		EmbedFiles:       []string{},
		TestEmbedFiles:   []string{},
		XTestEmbedFiles:  []string{},
		OtherBuildInputs: reviewFreezeCompileAttestationFixtureEmptyOtherBuildInputsV1(),
	}
}

// reviewFreezeCompileAttestationSchemaFixtureProjectionV1 仅构造确定、闭合的 Schema
// 对抗测试 fixture：它刻意用 bytes 连到其余 stdlib package 来覆盖图规则，不是真实
// Go 1.26.3 go-list 投影，禁止作为 material、builder 或 Review Freeze evidence。
func reviewFreezeCompileAttestationSchemaFixtureProjectionV1(module reviewFreezeCompileAttestationModuleV1, goArchiveSHA256 string) reviewFreezeGoListCanonicalProjectionV1 {
	standardImports := reviewFreezeValidateCompileAttestationGoListExpectedStandardImportsV1()
	packages := make([]reviewFreezeGoListCanonicalPackageV1, 0, len(standardImports)+len(module.Packages)+1)
	for _, importPath := range standardImports {
		imports := []string{}
		if importPath == "bytes" {
			for _, dependency := range standardImports {
				if dependency != importPath {
					imports = append(imports, dependency)
				}
			}
		}
		packages = append(packages, reviewFreezeGoListCanonicalPackageV1{
			ImportPath:       importPath,
			PackageName:      "fixture",
			PackageKind:      "stdlib",
			Module:           reviewFreezeGoListCanonicalModuleV1{Kind: "stdlib", GoArchiveSHA256: goArchiveSHA256},
			GoFiles:          []string{"src/" + importPath + "/fixture.go"},
			CompiledGoFiles:  []string{},
			TestGoFiles:      []string{},
			XTestGoFiles:     []string{},
			Imports:          imports,
			TestImports:      []string{},
			XTestImports:     []string{},
			EmbedFiles:       []string{},
			TestEmbedFiles:   []string{},
			XTestEmbedFiles:  []string{},
			OtherBuildInputs: reviewFreezeCompileAttestationFixtureEmptyOtherBuildInputsV1(),
		})
	}
	packages = append(packages, reviewFreezeCompileAttestationFixtureTargetPackageV1())
	for _, externalPackage := range module.Packages {
		packages = append(packages, reviewFreezeCompileAttestationFixtureExternalPackageV1(module, externalPackage))
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })
	return reviewFreezeGoListCanonicalProjectionV1{
		SchemaVersion:             reviewFreezeGoListProjectionSchemaV1,
		EntrypointID:              reviewFreezeCompileAttestationEntrypointV1,
		ModuleRoot:                "agent",
		PackagePattern:            reviewFreezeCompileAttestationPackagePatternV1,
		TargetImportPath:          reviewFreezeCompileAttestationTargetImportV1,
		TargetTestVariantObserved: true,
		SyntheticTestMainObserved: true,
		Packages:                  packages,
	}
}

func reviewFreezeCompileAttestationFixtureModuleV1() reviewFreezeCompileAttestationModuleV1 {
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

func reviewFreezeCompileAttestationFixtureContentRefV1(artifactKind, contentSchema, mediaType, label string, sizeBytes int64) reviewFreezeAttestationContentRefV1 {
	return reviewFreezeAttestationContentRefV1{
		RefSchemaVersion:     reviewFreezeAttestationContentRefSchemaV1,
		ArtifactKind:         artifactKind,
		ContentSchemaVersion: contentSchema,
		MediaType:            mediaType,
		SHA256:               reviewFreezeSHA256V1([]byte(label)),
		SizeBytes:            sizeBytes,
	}
}

func reviewFreezeCompileAttestationFixtureRefreshProjectionDigestV1(t *testing.T, statement *reviewFreezeValidatorCompileAttestationV1) {
	t.Helper()
	raw, err := json.Marshal(statement.GoList.Projection)
	if err != nil {
		t.Fatalf("marshal canonical projection: %v", err)
	}
	statement.GoList.ProjectionSHA256 = reviewFreezeSHA256V1(raw)
	statement.BuilderRun.GoListProjectionSHA = statement.GoList.ProjectionSHA256
}

func reviewFreezeCompileAttestationFixtureStatementV1(t *testing.T) reviewFreezeValidatorCompileAttestationV1 {
	t.Helper()
	module := reviewFreezeCompileAttestationFixtureModuleV1()
	builderRuns, _, toolchain := reviewFreezeValidateCompileAttestationBuilderFixtureV1()
	toolchain.InstallRoot = "/opt/go"
	toolchain.GoArchiveFile = "/artifacts/go1.26.3.linux-amd64.tar.gz"
	toolchain.GoArchiveSHA256 = reviewFreezeSHA256V1([]byte("go1.26.3 linux-amd64 archive"))
	toolchain.GoArchiveSize = 74298117
	toolchain.GOROOTTreeProjectionSHA256 = reviewFreezeSHA256V1([]byte("go1.26.3 GOROOT tree projection"))
	toolchain.GoBinarySHA256 = reviewFreezeSHA256V1([]byte("go1.26.3 go binary"))
	statement := reviewFreezeValidatorCompileAttestationV1{
		SchemaVersion: reviewFreezeValidatorCompileAttestationSchemaV1,
		EvaluationRequest: reviewFreezeCompileAttestationEvaluationRequestV1{
			EvaluationID:           "019f62c8-9eb6-7882-8936-e8e6bbf1b95b",
			PairingChallengeSHA256: reviewFreezeSHA256V1([]byte("pairing challenge")),
			PairPolicySHA256:       reviewFreezeSHA256V1([]byte("pair policy")),
			BuilderSlot:            "builder_a",
		},
		Subject: reviewFreezeCompileAttestationSubjectV1{
			RepositoryID:           "123456789",
			BaseCommitSHA:          strings.Repeat("a", 40),
			BaseTreeSHA:            strings.Repeat("b", 40),
			EntrypointID:           reviewFreezeCompileAttestationEntrypointV1,
			PackagePath:            reviewFreezeCompileAttestationPackagePathV1,
			PackagePattern:         reviewFreezeCompileAttestationPackagePatternV1,
			ContractManifestPath:   "agent/tests/contract/testdata/w2_r01/manifest.json",
			ContractManifestSHA256: reviewFreezeSHA256V1([]byte("R01 contract manifest")),
			BuildClosureProjectionRef: reviewFreezeCompileAttestationFixtureContentRefV1(
				reviewFreezeCompileAttestationBuildClosureArtifactKindV1,
				reviewFreezeCompileAttestationBuildClosureContentSchemaV1,
				reviewFreezeCompileAttestationBuildClosureMediaTypeV1,
				"R01 build closure projection",
				4096,
			),
			ValidatorSourceTreeSHA256: reviewFreezeSHA256V1([]byte("R01 validator source tree")),
			GoModSHA256:               reviewFreezeSHA256V1([]byte("agent go.mod")),
			GoSumSHA256:               reviewFreezeSHA256V1([]byte("agent go.sum")),
		},
		ExternalModules: []reviewFreezeCompileAttestationModuleV1{module},
		Environment:     reviewFreezeExpectedCompileAttestationEnvironmentV1(),
		Toolchain:       toolchain,
		BuilderRun:      builderRuns[0],
	}
	statement.GoList.RawToProjectionPolicy = reviewFreezeGoListRawToProjectionPolicyV1
	statement.GoList.Projection = reviewFreezeCompileAttestationSchemaFixtureProjectionV1(module, toolchain.GoArchiveSHA256)
	reviewFreezeCompileAttestationFixtureRefreshProjectionDigestV1(t, &statement)
	return statement
}

func reviewFreezeCompileAttestationFixtureMarshalV1(t *testing.T, statement reviewFreezeValidatorCompileAttestationV1) []byte {
	t.Helper()
	raw, err := json.Marshal(statement)
	if err != nil {
		t.Fatalf("marshal compile attestation fixture: %v", err)
	}
	return raw
}

func reviewFreezeCompileAttestationFixtureDeepCopyV1(t *testing.T, statement reviewFreezeValidatorCompileAttestationV1) reviewFreezeValidatorCompileAttestationV1 {
	t.Helper()
	raw := reviewFreezeCompileAttestationFixtureMarshalV1(t, statement)
	var clone reviewFreezeValidatorCompileAttestationV1
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatalf("deep-copy compile attestation fixture: %v", err)
	}
	return clone
}

func reviewFreezeCompileAttestationFixtureMutateJSONObjectV1(t *testing.T, raw []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("decode compile attestation JSON fixture: %v", err)
	}
	mutate(object)
	mutated, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("encode mutated compile attestation JSON fixture: %v", err)
	}
	return mutated
}

func reviewFreezeTestCompileAttestationRejectsJSONV1(t *testing.T, raw []byte, want string) {
	t.Helper()
	err := reviewFreezeValidateCompileAttestationStatementJSONV1(raw)
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("validation error=%v want contains %q", err, want)
	}
}

func reviewFreezeTestCompileAttestationRejectsV1(t *testing.T, statement reviewFreezeValidatorCompileAttestationV1, want string) {
	t.Helper()
	reviewFreezeTestCompileAttestationRejectsJSONV1(t, reviewFreezeCompileAttestationFixtureMarshalV1(t, statement), want)
}

func reviewFreezeTestCompileAttestationPackageIndexV1(t *testing.T, statement reviewFreezeValidatorCompileAttestationV1, importPath string) int {
	t.Helper()
	for index, selectedPackage := range statement.GoList.Projection.Packages {
		if selectedPackage.ImportPath == importPath {
			return index
		}
	}
	t.Fatalf("package %q missing from fixture", importPath)
	return -1
}

func TestW2ReviewFreezeCompileAttestationStatementValidV1(t *testing.T) {
	statement := reviewFreezeCompileAttestationFixtureStatementV1(t)
	statement = reviewFreezeCompileAttestationFixtureDeepCopyV1(t, statement)
	if err := reviewFreezeValidateCompileAttestationStatementJSONV1(reviewFreezeCompileAttestationFixtureMarshalV1(t, statement)); err != nil {
		t.Fatalf("valid compile attestation rejected: %v", err)
	}
}

func TestW2ReviewFreezeCompileAttestationStatementStrictJSONAdversarialV1(t *testing.T) {
	valid := reviewFreezeCompileAttestationFixtureMarshalV1(t, reviewFreezeCompileAttestationFixtureStatementV1(t))
	illegalUTF8 := append([]byte{}, valid...)
	marker := bytes.Index(illegalUTF8, []byte("123456789"))
	if marker < 0 {
		t.Fatal("repository ID marker missing from fixture")
	}
	illegalUTF8[marker] = 0xff
	overDepth := append(bytes.Repeat([]byte("["), reviewFreezeCompileAttestationMaxJSONDepthV1+2), []byte("0")...)
	overDepth = append(overDepth, bytes.Repeat([]byte("]"), reviewFreezeCompileAttestationMaxJSONDepthV1+2)...)
	overArray := []byte("[" + strings.Repeat("0,", reviewFreezeCompileAttestationMaxJSONArrayElementsV1) + "0]")
	overString := []byte(`"` + strings.Repeat("a", reviewFreezeCompileAttestationMaxJSONStringBytesV1+1) + `"`)
	overNumber := []byte(strings.Repeat("1", reviewFreezeCompileAttestationMaxJSONNumberBytesV1+1))
	var overObject strings.Builder
	overObject.WriteByte('{')
	for index := 0; index <= reviewFreezeCompileAttestationMaxJSONObjectFieldsV1; index++ {
		if index != 0 {
			overObject.WriteByte(',')
		}
		overObject.WriteString(`"field_`)
		overObject.WriteString(strconv.Itoa(index))
		overObject.WriteString(`":0`)
	}
	overObject.WriteByte('}')

	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{name: "illegal UTF-8", raw: illegalUTF8, want: "UTF-8"},
		{name: "unknown field", raw: append([]byte(`{"future":true,`), valid[1:]...), want: "unknown field"},
		{name: "case-insensitive top-level alias", raw: append([]byte(`{"SCHEMA_VERSION":"shadow",`), valid[1:]...), want: "unknown field"},
		{name: "case-insensitive nested alias", raw: reviewFreezeCompileAttestationFixtureMutateJSONObjectV1(t, valid, func(object map[string]any) {
			subject := object["subject"].(map[string]any)
			subject["BASE_COMMIT_SHA"] = strings.Repeat("c", 40)
		}), want: "unknown field"},
		{name: "duplicate field", raw: append([]byte(`{"schema_version":"duplicate",`), valid[1:]...), want: "duplicate field"},
		{name: "null", raw: reviewFreezeCompileAttestationFixtureMutateJSONObjectV1(t, valid, func(object map[string]any) { object["subject"] = nil }), want: "禁止 null"},
		{name: "nested null", raw: reviewFreezeCompileAttestationFixtureMutateJSONObjectV1(t, valid, func(object map[string]any) {
			builderRun := object["builder_run"].(map[string]any)
			invocation := builderRun["go_list_invocation"].(map[string]any)
			invocation["stderr_sha256"] = nil
		}), want: "禁止 null"},
		{name: "missing", raw: reviewFreezeCompileAttestationFixtureMutateJSONObjectV1(t, valid, func(object map[string]any) { delete(object, "subject") }), want: "缺必填字段"},
		{name: "missing nested zero value", raw: reviewFreezeCompileAttestationFixtureMutateJSONObjectV1(t, valid, func(object map[string]any) {
			builderRun := object["builder_run"].(map[string]any)
			invocation := builderRun["go_list_invocation"].(map[string]any)
			delete(invocation, "stderr_size_bytes")
		}), want: "stderr_size_bytes"},
		{name: "trailing JSON", raw: append(append([]byte{}, valid...), []byte("\n{}")...), want: "trailing JSON"},
		{name: "size limit", raw: bytes.Repeat([]byte(" "), reviewFreezeCompileAttestationMaxJSONBytesV1+1), want: "statement size"},
		{name: "depth limit", raw: overDepth, want: "JSON depth"},
		{name: "array cardinality limit", raw: overArray, want: "array elements"},
		{name: "object cardinality limit", raw: []byte(overObject.String()), want: "object fields"},
		{name: "string size limit", raw: overString, want: "string bytes"},
		{name: "number size limit", raw: overNumber, want: "number bytes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reviewFreezeTestCompileAttestationRejectsJSONV1(t, test.raw, test.want)
		})
	}

	for _, field := range []string{
		"status",
		"candidate_unactivated",
		"active",
		"trusted",
		"authoritative",
		"result",
		"issuer",
		"issuer_app_id",
		"issuer_key_id",
		"signature",
		"record_signature",
	} {
		t.Run("injected "+field, func(t *testing.T) {
			raw := append([]byte(`{"`+field+`":true,`), valid[1:]...)
			reviewFreezeTestCompileAttestationRejectsJSONV1(t, raw, "unknown field")
		})
	}
}

func TestW2ReviewFreezeCompileAttestationStatementInputAdversarialV1(t *testing.T) {
	base := reviewFreezeCompileAttestationFixtureStatementV1(t)
	tests := []struct {
		name   string
		mutate func(*reviewFreezeValidatorCompileAttestationV1)
		want   string
	}{
		{name: "evaluation ID drift", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.EvaluationRequest.EvaluationID = "not-a-uuidv7"
		}, want: "evaluation_id"},
		{name: "evaluation slot drift", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.EvaluationRequest.BuilderSlot = "builder_c"
		}, want: "builder_slot"},
		{name: "evaluation challenge drift", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.EvaluationRequest.PairingChallengeSHA256 = reviewFreezeSHA256V1(nil)
		}, want: "pairing_challenge"},
		{name: "subject base identity", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) { statement.Subject.BaseCommitSHA = "main" }, want: "repository/base identity"},
		{name: "subject zero base SHA", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.Subject.BaseCommitSHA = strings.Repeat("0", 40)
		}, want: "repository/base identity"},
		{name: "subject manifest digest", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.Subject.ContractManifestSHA256 = "invalid"
		}, want: "contract_manifest digest"},
		{name: "subject empty SHA disguise", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.Subject.ContractManifestSHA256 = reviewFreezeSHA256V1(nil)
		}, want: "contract_manifest digest"},
		{name: "subject build closure digest", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.Subject.BuildClosureProjectionRef.SHA256 = "invalid"
		}, want: "build closure projection content ref digest"},
		{name: "subject validator source digest", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.Subject.ValidatorSourceTreeSHA256 = "invalid"
		}, want: "validator_sources digest"},
		{name: "subject go.mod digest", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) { statement.Subject.GoModSHA256 = "invalid" }, want: "go.mod digest"},
		{name: "subject go.sum digest", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) { statement.Subject.GoSumSHA256 = "invalid" }, want: "go.sum digest"},
		{name: "absolute contract path", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.Subject.ContractManifestPath = "/workspace/agent/manifest.json"
		}, want: "subject identity"},
		{name: "module zip raw digest", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.ExternalModules[0].ZipSHA256 = reviewFreezeSHA256V1([]byte("other zip"))
		}, want: "zip raw identity"},
		{name: "module zip raw size", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.ExternalModules[0].ZipSizeBytes++
		}, want: "zip raw identity"},
		{name: "module h1", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.ExternalModules[0].ModuleSum = "h1:forged"
		}, want: "module/go.mod identity"},
		{name: "module go.mod raw digest", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.ExternalModules[0].GoModSHA256 = reviewFreezeSHA256V1([]byte("other go.mod"))
		}, want: "module/go.mod identity"},
		{name: "module zip-root go.mod drift", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.ExternalModules[0].ZipRootGoModSHA256 = reviewFreezeSHA256V1([]byte("other zip-root go.mod"))
		}, want: "module/go.mod identity"},
		{name: "module source digest", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.ExternalModules[0].Packages[0].Sources[0].SHA256 = reviewFreezeSHA256V1([]byte("other source"))
		}, want: "selected package/source/import"},
		{name: "environment", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) { statement.Environment.GOOS = "darwin" }, want: "environment="},
		{name: "toolchain empty SHA disguise", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.Toolchain.GoBinarySHA256 = reviewFreezeSHA256V1(nil)
		}, want: "go binary digest"},
		{name: "toolchain archive oversize", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.Toolchain.GoArchiveSize = reviewFreezeCompileAttestationMaxGoArchiveV1 + 1
		}, want: "toolchain path/size"},
		{name: "go list digest", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.GoList.ProjectionSHA256 = reviewFreezeSHA256V1([]byte("forged projection"))
		}, want: "projection digest"},
		{name: "go list raw-to-projection policy", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.GoList.RawToProjectionPolicy = "w2_go_list_raw_to_projection.v2"
		}, want: "raw-to-projection policy"},
		{name: "go list package order", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.GoList.Projection.Packages[0], statement.GoList.Projection.Packages[1] = statement.GoList.Projection.Packages[1], statement.GoList.Projection.Packages[0]
			reviewFreezeCompileAttestationFixtureRefreshProjectionDigestV1(t, statement)
		}, want: "未排序"},
		{name: "go list absolute source path", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			index := reviewFreezeTestCompileAttestationPackageIndexV1(t, *statement, "bytes")
			statement.GoList.Projection.Packages[index].GoFiles[0] = "/opt/go/src/bytes/fixture.go"
			reviewFreezeCompileAttestationFixtureRefreshProjectionDigestV1(t, statement)
		}, want: "不安全路径"},
		{name: "go list missing target test variant", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.GoList.Projection.TargetTestVariantObserved = false
			reviewFreezeCompileAttestationFixtureRefreshProjectionDigestV1(t, statement)
		}, want: "test variant"},
		{name: "go list missing target tests", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			index := reviewFreezeTestCompileAttestationPackageIndexV1(t, *statement, reviewFreezeCompileAttestationTargetImportV1)
			statement.GoList.Projection.Packages[index].TestGoFiles = []string{}
			reviewFreezeCompileAttestationFixtureRefreshProjectionDigestV1(t, statement)
		}, want: "target package projection"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement := reviewFreezeCompileAttestationFixtureDeepCopyV1(t, base)
			test.mutate(&statement)
			reviewFreezeTestCompileAttestationRejectsV1(t, statement, test.want)
		})
	}
}

func TestW2ReviewFreezeCompileAttestationStatementContentRefAdversarialV1(t *testing.T) {
	base := reviewFreezeCompileAttestationFixtureStatementV1(t)
	refs := []struct {
		name      string
		selectRef func(*reviewFreezeValidatorCompileAttestationV1) *reviewFreezeAttestationContentRefV1
		maxSize   int64
	}{
		{name: "build closure", selectRef: func(statement *reviewFreezeValidatorCompileAttestationV1) *reviewFreezeAttestationContentRefV1 {
			return &statement.Subject.BuildClosureProjectionRef
		}, maxSize: reviewFreezeCompileAttestationBuildClosureMaxBytesV1},
		{name: "input before", selectRef: func(statement *reviewFreezeValidatorCompileAttestationV1) *reviewFreezeAttestationContentRefV1 {
			return &statement.BuilderRun.InputSnapshotBeforeRef
		}, maxSize: reviewFreezeCompileAttestationInputSnapshotMaxBytesV1},
		{name: "input after", selectRef: func(statement *reviewFreezeValidatorCompileAttestationV1) *reviewFreezeAttestationContentRefV1 {
			return &statement.BuilderRun.InputSnapshotAfterRef
		}, maxSize: reviewFreezeCompileAttestationInputSnapshotMaxBytesV1},
		{name: "go list raw", selectRef: func(statement *reviewFreezeValidatorCompileAttestationV1) *reviewFreezeAttestationContentRefV1 {
			return &statement.BuilderRun.GoListRawRef
		}, maxSize: reviewFreezeCompileAttestationGoListRawMaxBytesV1},
		{name: "compile artifact", selectRef: func(statement *reviewFreezeValidatorCompileAttestationV1) *reviewFreezeAttestationContentRefV1 {
			return &statement.BuilderRun.Compile.ArtifactRef
		}, maxSize: reviewFreezeCompileAttestationArtifactMaxBytesV1},
		{name: "build info raw", selectRef: func(statement *reviewFreezeValidatorCompileAttestationV1) *reviewFreezeAttestationContentRefV1 {
			return &statement.BuilderRun.Compile.BuildInfoRawRef
		}, maxSize: reviewFreezeCompileAttestationBuildInfoRawMaxBytesV1},
		{name: "SBOM generator binary", selectRef: func(statement *reviewFreezeValidatorCompileAttestationV1) *reviewFreezeAttestationContentRefV1 {
			return &statement.BuilderRun.SBOM.GeneratorBinaryRef
		}, maxSize: reviewFreezeCompileAttestationSBOMGeneratorMaxBytesV1},
		{name: "SBOM raw", selectRef: func(statement *reviewFreezeValidatorCompileAttestationV1) *reviewFreezeAttestationContentRefV1 {
			return &statement.BuilderRun.SBOM.RawRef
		}, maxSize: reviewFreezeCompileAttestationSBOMRawMaxBytesV1},
	}
	mutations := []struct {
		name   string
		mutate func(*reviewFreezeAttestationContentRefV1, int64)
		want   string
	}{
		{name: "ref schema drift", mutate: func(ref *reviewFreezeAttestationContentRefV1, _ int64) {
			ref.RefSchemaVersion = "w2_attestation_content_ref.v2"
		}, want: "content ref identity"},
		{name: "kind drift", mutate: func(ref *reviewFreezeAttestationContentRefV1, _ int64) { ref.ArtifactKind = "other_artifact" }, want: "content ref identity"},
		{name: "content schema drift", mutate: func(ref *reviewFreezeAttestationContentRefV1, _ int64) { ref.ContentSchemaVersion = "other_content.v1" }, want: "content ref identity"},
		{name: "media drift", mutate: func(ref *reviewFreezeAttestationContentRefV1, _ int64) { ref.MediaType = "application/octet-stream" }, want: "content ref identity"},
		{name: "empty SHA disguise", mutate: func(ref *reviewFreezeAttestationContentRefV1, _ int64) { ref.SHA256 = reviewFreezeSHA256V1(nil) }, want: "content ref digest"},
		{name: "zero size", mutate: func(ref *reviewFreezeAttestationContentRefV1, _ int64) { ref.SizeBytes = 0 }, want: "content ref size"},
		{name: "oversize", mutate: func(ref *reviewFreezeAttestationContentRefV1, maxSize int64) { ref.SizeBytes = maxSize + 1 }, want: "content ref size"},
	}
	for _, refCase := range refs {
		for _, mutation := range mutations {
			t.Run(refCase.name+"/"+mutation.name, func(t *testing.T) {
				statement := reviewFreezeCompileAttestationFixtureDeepCopyV1(t, base)
				mutation.mutate(refCase.selectRef(&statement), refCase.maxSize)
				reviewFreezeTestCompileAttestationRejectsV1(t, statement, mutation.want)
			})
		}
	}
}

func TestW2ReviewFreezeCompileAttestationStatementExecutionAdversarialV1(t *testing.T) {
	base := reviewFreezeCompileAttestationFixtureStatementV1(t)
	tests := []struct {
		name   string
		mutate func(*reviewFreezeValidatorCompileAttestationV1)
		want   string
	}{
		{name: "compile uses go test run", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.BuilderRun.Compile.Invocation.Argv = []string{"/opt/go/bin/go", "test", "-run", "TestGraphToolResultV1Corpus", reviewFreezeCompileAttestationPackagePatternV1}
		}, want: "go test -c argv"},
		{name: "test invokes go again", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.BuilderRun.Test.Invocation.Argv = []string{"/opt/go/bin/go", "test", "-run", "TestGraphToolResultV1Corpus", reviewFreezeCompileAttestationPackagePatternV1}
		}, want: "compiled test binary argv"},
		{name: "nonzero compile exit", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.BuilderRun.Compile.Invocation.ExitCode = 1
		}, want: "exit_code"},
		{name: "go list raw ref stdout mismatch", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.BuilderRun.GoListRawRef.SHA256 = reviewFreezeSHA256V1([]byte("different go list stdout"))
		}, want: "go list stdout/stderr"},
		{name: "input snapshot before-after mismatch", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.BuilderRun.InputSnapshotAfterRef.SHA256 = reviewFreezeSHA256V1([]byte("different input snapshot"))
		}, want: "input snapshot pre/post"},
		{name: "build info projection digest", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.BuilderRun.Compile.BuildInfoProjectionSHA256 = reviewFreezeSHA256V1([]byte("different build info projection"))
		}, want: "build info projection digest"},
		{name: "build info settings order", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			settings := statement.BuilderRun.Compile.BuildInfoProjection.Settings
			settings[0], settings[1] = settings[1], settings[0]
			reviewFreezeRefreshCompileAttestationBuildInfoProjectionDigestV1(t, &statement.BuilderRun)
		}, want: "build info projection 漂移"},
		{name: "build info setting value", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.BuilderRun.Compile.BuildInfoProjection.Settings[0].Value = "pie"
			reviewFreezeRefreshCompileAttestationBuildInfoProjectionDigestV1(t, &statement.BuilderRun)
		}, want: "build info projection 漂移"},
		{name: "artifact pre-post mismatch", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.BuilderRun.Test.PostExecutionArtifactSHA256 = reviewFreezeSHA256V1([]byte("changed artifact"))
		}, want: "pre/post artifact"},
		{name: "SBOM non-Dora generator", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.BuilderRun.SBOM.GeneratorName = "syft"
			statement.BuilderRun.SBOM.GeneratorVersion = "1.30.0"
		}, want: "SBOM generator identity"},
		{name: "SBOM invokes second build", mutate: func(statement *reviewFreezeValidatorCompileAttestationV1) {
			statement.BuilderRun.SBOM.Invocation.Argv = []string{"/opt/go/bin/go", "test", "-c", "-o", reviewFreezeCompileAttestationBinaryPathV1, reviewFreezeCompileAttestationPackagePatternV1}
		}, want: "SBOM deterministic generator argv"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement := reviewFreezeCompileAttestationFixtureDeepCopyV1(t, base)
			test.mutate(&statement)
			reviewFreezeTestCompileAttestationRejectsV1(t, statement, test.want)
		})
	}
}
