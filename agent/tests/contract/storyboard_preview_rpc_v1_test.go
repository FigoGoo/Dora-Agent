package contract_test

import (
	"context"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	foundationv1 "github.com/FigoGoo/Dora-Agent/agent/kitex_gen/foundationv1"
)

const storyboardPreviewRPCSchema = "storyboard.preview.rpc.v1"

// TestStoryboardPreviewOwnerIDLContract 冻结 Storyboard Preview 的 Owner IDL、DTO 与三条收敛 RPC。
func TestStoryboardPreviewOwnerIDLContract(t *testing.T) {
	root := bizPreview004RepoRoot(t)
	source := string(bizPreview004ReadFile(t, filepath.Join(root, bizPreview004OwnerIDLPath)))
	if !regexp.MustCompile(`(?m)^const string STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION = "` + regexp.QuoteMeta(storyboardPreviewRPCSchema) + `"$`).MatchString(source) {
		t.Fatalf("Owner IDL 缺少 Storyboard Preview schema %q", storyboardPreviewRPCSchema)
	}

	service := bizPreview004ThriftBlock(t, source, "service", "BusinessFoundationServiceV1")
	for _, signature := range []string{
		`GetStoryboardPlanningContextPreviewResponseV1\s+GetStoryboardPlanningContextPreviewV1\s*\(\s*1:\s*GetStoryboardPlanningContextPreviewRequestV1\s+request\s*\)`,
		`SaveStoryboardDraftPreviewResponseV1\s+SaveStoryboardDraftPreviewV1\s*\(\s*1:\s*SaveStoryboardDraftPreviewRequestV1\s+request\s*\)`,
		`QueryStoryboardDraftCommandPreviewResponseV1\s+QueryStoryboardDraftCommandPreviewV1\s*\(\s*1:\s*QueryStoryboardDraftCommandPreviewRequestV1\s+request\s*\)`,
	} {
		if !regexp.MustCompile(`(?s)` + signature + `\s*throws\s*\(\s*1:\s*FoundationServiceExceptionV1\s+service_error\s*\)`).MatchString(service) {
			t.Fatalf("Owner IDL 缺少冻结 Storyboard Preview 方法：%s", signature)
		}
	}

	if foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION != storyboardPreviewRPCSchema {
		t.Fatalf("generated schema=%q want=%q", foundationv1.STORYBOARD_PREVIEW_RPC_SCHEMA_VERSION, storyboardPreviewRPCSchema)
	}
	if int64(foundationv1.StoryboardPreviewElementTypeV1_SCENE) != 1 ||
		int64(foundationv1.StoryboardPreviewElementTypeV1_AUDIO) != 5 ||
		int64(foundationv1.StoryboardPreviewSlotTypeV1_IMAGE) != 1 ||
		int64(foundationv1.StoryboardPreviewSlotTypeV1_CAPTION) != 5 {
		t.Fatal("generated Storyboard Preview enum 数值漂移")
	}

	contentType := reflect.TypeOf(foundationv1.StoryboardPreviewContentV1{})
	if contentType.NumField() != 5 {
		t.Fatalf("generated StoryboardPreviewContentV1 fields=%d want=5", contentType.NumField())
	}
	for index, expected := range []struct {
		name string
		tag  string
	}{
		{"Title", "title,1,required"}, {"Summary", "summary,2,required"},
		{"Sections", "sections,3,required"}, {"Elements", "elements,4,required"}, {"Slots", "slots,5,required"},
	} {
		field := contentType.Field(index)
		if field.Name != expected.name || field.Tag.Get("thrift") != expected.tag {
			t.Fatalf("generated content field[%d]=%s/%s want=%s/%s", index, field.Name, field.Tag.Get("thrift"), expected.name, expected.tag)
		}
	}

	serviceType := reflect.TypeOf((*foundationv1.BusinessFoundationServiceV1)(nil)).Elem()
	for _, method := range []struct {
		name     string
		request  reflect.Type
		response reflect.Type
	}{
		{"GetStoryboardPlanningContextPreviewV1", reflect.TypeOf((*foundationv1.GetStoryboardPlanningContextPreviewRequestV1)(nil)), reflect.TypeOf((*foundationv1.GetStoryboardPlanningContextPreviewResponseV1)(nil))},
		{"SaveStoryboardDraftPreviewV1", reflect.TypeOf((*foundationv1.SaveStoryboardDraftPreviewRequestV1)(nil)), reflect.TypeOf((*foundationv1.SaveStoryboardDraftPreviewResponseV1)(nil))},
		{"QueryStoryboardDraftCommandPreviewV1", reflect.TypeOf((*foundationv1.QueryStoryboardDraftCommandPreviewRequestV1)(nil)), reflect.TypeOf((*foundationv1.QueryStoryboardDraftCommandPreviewResponseV1)(nil))},
	} {
		assertStoryboardPreviewServiceMethod(t, serviceType, method.name, method.request, method.response)
	}
}

// TestStoryboardPreviewGeneratedParity 防止 Business 与 Agent 使用不同的生成 DTO。
func TestStoryboardPreviewGeneratedParity(t *testing.T) {
	root := bizPreview004RepoRoot(t)
	for _, relative := range []string{"kitex_gen/foundationv1/foundation.go", "kitex_gen/foundationv1/k-foundation.go"} {
		bizPreview004AssertBytesEqual(t, relative,
			bizPreview004ReadFile(t, filepath.Join(root, "agent", relative)),
			bizPreview004ReadFile(t, filepath.Join(root, "business", relative)))
	}
}

// assertStoryboardPreviewServiceMethod 校验生成接口仍使用 context、请求指针、响应指针和 error。
func assertStoryboardPreviewServiceMethod(t *testing.T, service reflect.Type, name string, request reflect.Type, response reflect.Type) {
	t.Helper()
	method, ok := service.MethodByName(name)
	if !ok {
		t.Fatalf("generated service missing %s", name)
	}
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if method.Type.NumIn() != 2 || method.Type.In(0) != contextType || method.Type.In(1) != request ||
		method.Type.NumOut() != 2 || method.Type.Out(0) != response || method.Type.Out(1) != errorType {
		t.Fatalf("generated method %s type=%s", name, method.Type)
	}
}
