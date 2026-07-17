package contract_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	foundationv1 "github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
)

const (
	bizPreview004OwnerIDLPath = "business/api/thrift/foundation/v1/foundation.thrift"
	bizPreview004Schema       = "asset_analysis_inputs.preview.rpc.v1"
)

// bizPreview004EnumValue 描述 Thrift enum 的名称、顺序和稳定数值。
type bizPreview004EnumValue struct {
	Name  string
	Value int64
}

// bizPreview004ThriftField 描述 Thrift struct 中不可复用的字段号和必要性。
type bizPreview004ThriftField struct {
	ID          int
	Requirement string
	Type        string
	Name        string
}

// bizPreview004GoField 描述生成 Go DTO 必须保留的字段类型与 Thrift tag。
type bizPreview004GoField struct {
	Name      string
	Type      string
	ThriftTag string
}

// TestBIZPreview004OwnerThriftContract 冻结 Business Owner IDL 的 schema、枚举、DTO 字段号和服务方法。
func TestBIZPreview004OwnerThriftContract(t *testing.T) {
	root := bizPreview004RepoRoot(t)
	source := string(bizPreview004ReadFile(t, filepath.Join(root, bizPreview004OwnerIDLPath)))

	schemaPattern := regexp.MustCompile(`(?m)^\s*const\s+string\s+ASSET_ANALYSIS_INPUTS_PREVIEW_RPC_SCHEMA_VERSION\s*=\s*"` + regexp.QuoteMeta(bizPreview004Schema) + `"\s*$`)
	if !schemaPattern.MatchString(source) {
		t.Fatalf("Owner IDL 缺少冻结 schema 常量 %q", bizPreview004Schema)
	}

	enums := map[string][]bizPreview004EnumValue{
		"AssetAnalysisPreviewMediaTypeV1": {
			{Name: "TEXT", Value: 1},
			{Name: "IMAGE", Value: 2},
		},
		"AssetAnalysisPreviewEvidenceKindV1": {
			{Name: "TEXT_SEGMENT", Value: 1},
			{Name: "VISUAL_DESCRIPTION", Value: 2},
			{Name: "SAFETY_LABEL", Value: 3},
		},
		"AssetAnalysisPreviewAvailabilityV1": {
			{Name: "READY", Value: 1},
			{Name: "MISSING", Value: 2},
			{Name: "FAILED", Value: 3},
			{Name: "REDACTED", Value: 4},
			{Name: "UNSUPPORTED", Value: 5},
		},
		"AssetAnalysisPreviewLocatorKindV1": {
			{Name: "TEXT_RANGE", Value: 1},
			{Name: "IMAGE_WHOLE", Value: 2},
			{Name: "IMAGE_REGION", Value: 3},
		},
	}
	for name, expected := range enums {
		actual := bizPreview004ParseEnum(t, source, name)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("Owner IDL enum %s=%v want=%v", name, actual, expected)
		}
	}

	structs := map[string][]bizPreview004ThriftField{
		"AssetAnalysisPreviewTargetV1": {
			{1, "required", "string", "asset_id"},
			{2, "optional", "i64", "expected_asset_version"},
		},
		"AssetAnalysisPreviewLocatorV1": {
			{1, "required", "AssetAnalysisPreviewLocatorKindV1", "kind"},
			{2, "optional", "i64", "text_start"},
			{3, "optional", "i64", "text_end"},
			{4, "optional", "i64", "text_source_length"},
			{5, "optional", "i32", "image_x"},
			{6, "optional", "i32", "image_y"},
			{7, "optional", "i32", "image_width"},
			{8, "optional", "i32", "image_height"},
		},
		"AssetAnalysisPreviewEvidenceV1": {
			{1, "required", "string", "evidence_id"},
			{2, "required", "string", "asset_id"},
			{3, "required", "i64", "asset_version"},
			{4, "required", "AssetAnalysisPreviewMediaTypeV1", "media_type"},
			{5, "required", "AssetAnalysisPreviewEvidenceKindV1", "evidence_kind"},
			{6, "required", "AssetAnalysisPreviewAvailabilityV1", "availability"},
			{7, "optional", "string", "reason_code"},
			{8, "optional", "string", "content_digest"},
			{9, "optional", "string", "extractor_schema_version"},
			{10, "optional", "string", "extractor_version"},
			{11, "optional", "AssetAnalysisPreviewLocatorV1", "locator"},
			{12, "optional", "string", "content"},
		},
		"AssetAnalysisPreviewAssetV1": {
			{1, "required", "string", "asset_id"},
			{2, "required", "i64", "asset_version"},
			{3, "required", "AssetAnalysisPreviewMediaTypeV1", "media_type"},
			{4, "required", "list<AssetAnalysisPreviewEvidenceV1>", "evidence"},
		},
		"BatchGetAssetAnalysisInputsPreviewRequestV1": {
			{1, "required", "string", "schema_version"},
			{2, "required", "string", "request_id"},
			{3, "required", "string", "user_id"},
			{4, "required", "string", "project_id"},
			{5, "required", "list<AssetAnalysisPreviewTargetV1>", "targets"},
		},
		"BatchGetAssetAnalysisInputsPreviewResponseV1": {
			{1, "required", "string", "schema_version"},
			{2, "required", "string", "request_id"},
			{3, "required", "string", "snapshot_token"},
			{4, "required", "bool", "response_complete"},
			{5, "required", "list<AssetAnalysisPreviewAssetV1>", "assets"},
		},
	}
	for name, expected := range structs {
		actual := bizPreview004ParseStruct(t, source, name)
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("Owner IDL struct %s=%v want=%v", name, actual, expected)
		}
	}

	service := bizPreview004ThriftBlock(t, source, "service", "BusinessFoundationServiceV1")
	methodPattern := regexp.MustCompile(`(?s)\bBatchGetAssetAnalysisInputsPreviewResponseV1\s+BatchGetAssetAnalysisInputsPreviewV1\s*\(\s*1:\s*BatchGetAssetAnalysisInputsPreviewRequestV1\s+request\s*\)\s*throws\s*\(\s*1:\s*FoundationServiceExceptionV1\s+service_error\s*\)`)
	if !methodPattern.MatchString(service) {
		t.Fatal("Owner IDL 缺少冻结的 BatchGetAssetAnalysisInputsPreviewV1 方法签名")
	}
	for _, method := range []string{
		"Probe",
		"GetCreationSpecContextPreviewV1",
		"SaveCreationSpecDraftPreviewV1",
		"QueryCreationSpecDraftCommandPreviewV1",
		"BatchGetAssetAnalysisInputsPreviewV1",
	} {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(method) + `\s*\(`).MatchString(service) {
			t.Fatalf("BusinessFoundationServiceV1 缺少既有或冻结方法 %s", method)
		}
	}
}

// TestBIZPreview004GeneratedContractParity 校验两端生成文件同源，并锁定 Agent 侧可编译 API 形状。
func TestBIZPreview004GeneratedContractParity(t *testing.T) {
	root := bizPreview004RepoRoot(t)
	for _, relative := range []string{
		"kitex_gen/foundationv1/foundation.go",
		"kitex_gen/foundationv1/k-foundation.go",
	} {
		agentGenerated := bizPreview004ReadFile(t, filepath.Join(root, "agent", relative))
		businessGenerated := bizPreview004ReadFile(t, filepath.Join(root, "business", relative))
		bizPreview004AssertBytesEqual(t, relative, agentGenerated, businessGenerated)
	}

	for _, relative := range []string{
		"kitex_gen/foundationv1/businessfoundationservicev1/businessfoundationservicev1.go",
		"kitex_gen/foundationv1/businessfoundationservicev1/client.go",
	} {
		agentGenerated := bizPreview004NormalizeModulePath(bizPreview004ReadFile(t, filepath.Join(root, "agent", relative)))
		businessGenerated := bizPreview004NormalizeModulePath(bizPreview004ReadFile(t, filepath.Join(root, "business", relative)))
		bizPreview004AssertBytesEqual(t, relative+" normalized", agentGenerated, businessGenerated)
	}

	if foundationv1.ASSET_ANALYSIS_INPUTS_PREVIEW_RPC_SCHEMA_VERSION != bizPreview004Schema {
		t.Fatalf("generated schema=%q want=%q", foundationv1.ASSET_ANALYSIS_INPUTS_PREVIEW_RPC_SCHEMA_VERSION, bizPreview004Schema)
	}
	bizPreview004AssertGeneratedEnums(t)
	bizPreview004AssertGeneratedStruct(t, reflect.TypeOf(foundationv1.AssetAnalysisPreviewTargetV1{}), []bizPreview004GoField{
		{"AssetId", "string", "asset_id,1,required"},
		{"ExpectedAssetVersion", "*int64", "expected_asset_version,2,optional"},
	})
	bizPreview004AssertGeneratedStruct(t, reflect.TypeOf(foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1{}), []bizPreview004GoField{
		{"SchemaVersion", "string", "schema_version,1,required"},
		{"RequestId", "string", "request_id,2,required"},
		{"UserId", "string", "user_id,3,required"},
		{"ProjectId", "string", "project_id,4,required"},
		{"Targets", "[]*foundationv1.AssetAnalysisPreviewTargetV1", "targets,5,required"},
	})
	bizPreview004AssertGeneratedStruct(t, reflect.TypeOf(foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1{}), []bizPreview004GoField{
		{"SchemaVersion", "string", "schema_version,1,required"},
		{"RequestId", "string", "request_id,2,required"},
		{"SnapshotToken", "string", "snapshot_token,3,required"},
		{"ResponseComplete", "bool", "response_complete,4,required"},
		{"Assets", "[]*foundationv1.AssetAnalysisPreviewAssetV1", "assets,5,required"},
	})

	target := foundationv1.NewAssetAnalysisPreviewTargetV1()
	if target.IsSetExpectedAssetVersion() {
		t.Fatal("optional expected_asset_version 默认必须保持 absent")
	}
	version := int64(7)
	target.SetExpectedAssetVersion(&version)
	if !target.IsSetExpectedAssetVersion() || target.GetExpectedAssetVersion() != version {
		t.Fatal("optional expected_asset_version 生成 API 未保留 presence 语义")
	}

	serviceType := reflect.TypeOf((*foundationv1.BusinessFoundationServiceV1)(nil)).Elem()
	methods := []struct {
		name     string
		request  reflect.Type
		response reflect.Type
	}{
		{"Probe", reflect.TypeOf((*foundationv1.FoundationProbeRequestV1)(nil)), reflect.TypeOf((*foundationv1.FoundationProbeResponseV1)(nil))},
		{"GetCreationSpecContextPreviewV1", reflect.TypeOf((*foundationv1.GetCreationSpecContextPreviewRequestV1)(nil)), reflect.TypeOf((*foundationv1.GetCreationSpecContextPreviewResponseV1)(nil))},
		{"SaveCreationSpecDraftPreviewV1", reflect.TypeOf((*foundationv1.SaveCreationSpecDraftPreviewRequestV1)(nil)), reflect.TypeOf((*foundationv1.SaveCreationSpecDraftPreviewResponseV1)(nil))},
		{"QueryCreationSpecDraftCommandPreviewV1", reflect.TypeOf((*foundationv1.QueryCreationSpecDraftCommandPreviewRequestV1)(nil)), reflect.TypeOf((*foundationv1.QueryCreationSpecDraftCommandPreviewResponseV1)(nil))},
		{"BatchGetAssetAnalysisInputsPreviewV1", reflect.TypeOf((*foundationv1.BatchGetAssetAnalysisInputsPreviewRequestV1)(nil)), reflect.TypeOf((*foundationv1.BatchGetAssetAnalysisInputsPreviewResponseV1)(nil))},
	}
	for _, expected := range methods {
		bizPreview004AssertServiceMethod(t, serviceType, expected.name, expected.request, expected.response)
	}
}

// bizPreview004AssertGeneratedEnums 防止生成代码把 Owner enum 数值重排或静默插入零值。
func bizPreview004AssertGeneratedEnums(t *testing.T) {
	t.Helper()
	actual := map[string]int64{
		"media.TEXT":               int64(foundationv1.AssetAnalysisPreviewMediaTypeV1_TEXT),
		"media.IMAGE":              int64(foundationv1.AssetAnalysisPreviewMediaTypeV1_IMAGE),
		"kind.TEXT_SEGMENT":        int64(foundationv1.AssetAnalysisPreviewEvidenceKindV1_TEXT_SEGMENT),
		"kind.VISUAL_DESCRIPTION":  int64(foundationv1.AssetAnalysisPreviewEvidenceKindV1_VISUAL_DESCRIPTION),
		"kind.SAFETY_LABEL":        int64(foundationv1.AssetAnalysisPreviewEvidenceKindV1_SAFETY_LABEL),
		"availability.READY":       int64(foundationv1.AssetAnalysisPreviewAvailabilityV1_READY),
		"availability.MISSING":     int64(foundationv1.AssetAnalysisPreviewAvailabilityV1_MISSING),
		"availability.FAILED":      int64(foundationv1.AssetAnalysisPreviewAvailabilityV1_FAILED),
		"availability.REDACTED":    int64(foundationv1.AssetAnalysisPreviewAvailabilityV1_REDACTED),
		"availability.UNSUPPORTED": int64(foundationv1.AssetAnalysisPreviewAvailabilityV1_UNSUPPORTED),
		"locator.TEXT_RANGE":       int64(foundationv1.AssetAnalysisPreviewLocatorKindV1_TEXT_RANGE),
		"locator.IMAGE_WHOLE":      int64(foundationv1.AssetAnalysisPreviewLocatorKindV1_IMAGE_WHOLE),
		"locator.IMAGE_REGION":     int64(foundationv1.AssetAnalysisPreviewLocatorKindV1_IMAGE_REGION),
	}
	expected := map[string]int64{
		"media.TEXT": 1, "media.IMAGE": 2,
		"kind.TEXT_SEGMENT": 1, "kind.VISUAL_DESCRIPTION": 2, "kind.SAFETY_LABEL": 3,
		"availability.READY": 1, "availability.MISSING": 2, "availability.FAILED": 3,
		"availability.REDACTED": 4, "availability.UNSUPPORTED": 5,
		"locator.TEXT_RANGE": 1, "locator.IMAGE_WHOLE": 2, "locator.IMAGE_REGION": 3,
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("generated enum values=%v want=%v", actual, expected)
	}
}

// bizPreview004AssertGeneratedStruct 校验生成 DTO 字段顺序、指针可选性和 Thrift tag。
func bizPreview004AssertGeneratedStruct(t *testing.T, generated reflect.Type, expected []bizPreview004GoField) {
	t.Helper()
	if generated.NumField() != len(expected) {
		t.Fatalf("generated %s fields=%d want=%d", generated.Name(), generated.NumField(), len(expected))
	}
	for index, want := range expected {
		field := generated.Field(index)
		got := bizPreview004GoField{Name: field.Name, Type: field.Type.String(), ThriftTag: field.Tag.Get("thrift")}
		if got != want {
			t.Fatalf("generated %s field[%d]=%+v want=%+v", generated.Name(), index, got, want)
		}
	}
}

// bizPreview004AssertServiceMethod 校验生成 Service interface 的请求、响应和 error 形状。
func bizPreview004AssertServiceMethod(t *testing.T, service reflect.Type, name string, request reflect.Type, response reflect.Type) {
	t.Helper()
	method, exists := service.MethodByName(name)
	if !exists {
		t.Fatalf("generated BusinessFoundationServiceV1 缺少方法 %s", name)
	}
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if method.Type.NumIn() != 2 || method.Type.In(0) != contextType || method.Type.In(1) != request ||
		method.Type.NumOut() != 2 || method.Type.Out(0) != response || method.Type.Out(1) != errorType {
		t.Fatalf("generated method %s type=%s", name, method.Type)
	}
}

// bizPreview004ParseEnum 从 Owner IDL 读取一个 enum，并拒绝额外或重排的成员。
func bizPreview004ParseEnum(t *testing.T, source string, name string) []bizPreview004EnumValue {
	t.Helper()
	block := bizPreview004ThriftBlock(t, source, "enum", name)
	linePattern := regexp.MustCompile(`(?m)^\s*([A-Z][A-Z0-9_]*)\s*=\s*(-?[0-9]+)\s*,?\s*$`)
	matches := linePattern.FindAllStringSubmatch(block, -1)
	values := make([]bizPreview004EnumValue, 0, len(matches))
	for _, match := range matches {
		value, err := strconv.ParseInt(match[2], 10, 64)
		if err != nil {
			t.Fatalf("parse enum %s.%s: %v", name, match[1], err)
		}
		values = append(values, bizPreview004EnumValue{Name: match[1], Value: value})
	}
	return values
}

// bizPreview004ParseStruct 从 Owner IDL 读取 struct 的完整字段序列。
func bizPreview004ParseStruct(t *testing.T, source string, name string) []bizPreview004ThriftField {
	t.Helper()
	block := bizPreview004ThriftBlock(t, source, "struct", name)
	linePattern := regexp.MustCompile(`(?m)^\s*([0-9]+):\s+(required|optional)\s+([A-Za-z0-9_<>,]+)\s+([a-z][a-z0-9_]*)\s*$`)
	matches := linePattern.FindAllStringSubmatch(block, -1)
	fields := make([]bizPreview004ThriftField, 0, len(matches))
	for _, match := range matches {
		fieldID, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse struct %s field id %q: %v", name, match[1], err)
		}
		fields = append(fields, bizPreview004ThriftField{fieldID, match[2], match[3], match[4]})
	}
	return fields
}

// bizPreview004ThriftBlock 提取无嵌套花括号的 Thrift enum、struct 或 service 块。
func bizPreview004ThriftBlock(t *testing.T, source string, kind string, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?s)\b` + regexp.QuoteMeta(kind) + `\s+` + regexp.QuoteMeta(name) + `\s*\{(.*?)\}`)
	match := pattern.FindStringSubmatch(source)
	if len(match) != 2 {
		t.Fatalf("Owner IDL 缺少 %s %s", kind, name)
	}
	return match[1]
}

// bizPreview004RepoRoot 以当前测试源文件定位多 Module 仓库根，避免依赖调用方工作目录。
func bizPreview004RepoRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位 BIZ-PREVIEW-004 契约测试源文件")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, bizPreview004OwnerIDLPath)); err != nil {
		t.Fatalf("定位仓库根失败: %v", err)
	}
	return root
}

// bizPreview004ReadFile 读取仓库内契约文件，错误仅包含路径而不接触网络或数据库。
func bizPreview004ReadFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取契约文件 %s: %v", path, err)
	}
	return content
}

// bizPreview004NormalizeModulePath 只消除生成代码中必然不同的 Module import path。
func bizPreview004NormalizeModulePath(content []byte) []byte {
	normalized := strings.ReplaceAll(string(content), "github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1", "github.com/FigoGoo/Dora-Agent/{module}/kitex_gen/foundationv1")
	normalized = strings.ReplaceAll(normalized, "github.com/FigoGoo/Dora-Agent/business/kitex_gen/foundationv1", "github.com/FigoGoo/Dora-Agent/{module}/kitex_gen/foundationv1")
	return []byte(normalized)
}

// bizPreview004AssertBytesEqual 用 SHA-256 提供两端生成文件不一致时的最小诊断。
func bizPreview004AssertBytesEqual(t *testing.T, name string, agentGenerated []byte, businessGenerated []byte) {
	t.Helper()
	if bytes.Equal(agentGenerated, businessGenerated) {
		return
	}
	agentDigest := sha256.Sum256(agentGenerated)
	businessDigest := sha256.Sum256(businessGenerated)
	t.Fatalf("generated parity %s: agent=%s business=%s", name, hex.EncodeToString(agentDigest[:]), hex.EncodeToString(businessDigest[:]))
}
