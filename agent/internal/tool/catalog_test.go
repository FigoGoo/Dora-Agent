package tool

import (
	"reflect"
	"testing"
)

// TestCatalogProviderReturnsExactIndependentCopies 验证六项目录的集合、顺序、名称和不可用原因，并验证返回值相互隔离。
func TestCatalogProviderReturnsExactIndependentCopies(t *testing.T) {
	provider := NewCatalogProvider()
	want := []Definition{
		{ToolKey: "plan_creation_spec", DisplayName: "流程规划", Order: 1, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
		{ToolKey: "analyze_materials", DisplayName: "素材分析", Order: 2, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
		{ToolKey: "plan_storyboard", DisplayName: "故事板设计", Order: 3, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
		{ToolKey: "generate_media", DisplayName: "媒体生成", Order: 4, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
		{ToolKey: "write_prompts", DisplayName: "提示词写法", Order: 5, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
		{ToolKey: "assemble_output", DisplayName: "视频剪辑", Order: 6, Availability: "unavailable", ReasonCode: "DESIGN_REVIEW_PENDING"},
	}
	first := provider.ListDefinitions()
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("Tool catalog drifted: got=%+v want=%+v", first, want)
	}
	first[0].ToolKey = "mutated"
	first = append(first, Definition{ToolKey: "unexpected"})
	second := provider.ListDefinitions()
	if !reflect.DeepEqual(second, want) {
		t.Fatalf("caller mutation polluted Tool catalog: got=%+v want=%+v", second, want)
	}
}
