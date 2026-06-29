package skillcatalog

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/services/business/internal/application/accountspace"
)

func TestSaveSkillCompilesMarkdownContract(t *testing.T) {
	app := newSkillTestApp(t)
	auth := accountspace.AuthContext{UserID: "usr_markdown", LoginIdentityType: "personal"}

	markdown := `# 完整视频生成 <名称>

## 说明 <说明>

当用户需要根据剧本、图片或文本生成完整视频时触发。

## 输入 <输入>

用户可以先用自然语言描述目标。
如果缺少剧本或素材，向用户请求上传图片、PDF 或文本文件。
如果缺少风格偏好，提供写实、动画、电影感三个选项让用户选择。
如果目标不清楚，使用多行文本框让用户补充创作目标。
允许用户在看到故事板后继续修改风格、镜头和素材。

## 计划 <计划>

1. 获取或生成剧本。
2. 生成故事板并等待用户确认。
3. 生成资产并根据用户修改重新执行。

## 工具引用 <工具引用>

<tool id="web_fetch:browser">Web Fetch</tool>
<tool id="image_generate:model_generation">图片生成</tool>

## AG-UI 元素引用 <AG-UI元素引用>

对话框内：
<agui id="confirm_card">确认卡片</agui>

对话框外：
<agui id="storyboard_panel">故事板面板</agui>
<agui id="asset_panel">资产面板</agui>

## 生成偏好 <生成偏好>

优先生成 16:9 视频，默认保持角色一致性。

## 提示词写法 <提示词写法>

视频提示词按摄像机、主体、空间、音频顺序书写。

## 结果输出 <结果输出>

Agent 可以先输出故事板供用户审阅。
生成图片资产后更新资产面板。
生成视频资产后展示预览，并允许用户继续修改。
当用户满意后输出最终结果。`

	detail, err := app.SaveSkill(t.Context(), SaveSkillInput{
		Auth: auth, SkillName: "完整视频生成", SkillScope: "public", Version: "0.1.0",
		SkillMarkdown: markdown, SkillTags: []string{"视频", "故事板"},
	})
	if err != nil {
		t.Fatalf("save markdown skill: %v", err)
	}
	if detail.SkillKey == "" {
		t.Fatal("skill_key should be derived when omitted")
	}
	if detail.RouteHints["invocation_rule"] != "当用户需要根据剧本、图片或文本生成完整视频时触发。" {
		t.Fatalf("route hint not derived from 说明: %#v", detail.RouteHints)
	}

	versionID := latestVersionID(t, app, detail.SkillID)
	spec, inputSchema, outputSchema, memoryPolicy := loadSkillVersionJSON(t, app, versionID)
	if spec["source_format"] != "markdown" || spec["markdown"] != markdown {
		t.Fatalf("compiled spec should preserve source markdown: %#v", spec)
	}
	if got := stringSlice(spec["skill_tags"]); len(got) != 2 || got[0] != "视频" || got[1] != "故事板" {
		t.Fatalf("skill tags missing from compiled spec: %#v", spec["skill_tags"])
	}
	if sections, ok := spec["sections"].(map[string]any); !ok || sections["inputs"] == "" || sections["plan"] == "" || sections["tool_refs"] == "" {
		t.Fatalf("compiled sections missing: %#v", spec["sections"])
	}
	if refs := stringSlice(spec["tool_refs"]); len(refs) != 2 || refs[0] != "web_fetch:browser" || refs[1] != "image_generate:model_generation" {
		t.Fatalf("tool refs missing from compiled spec: %#v", spec["tool_refs"])
	}
	agui := spec["agui_refs"].(map[string]any)
	if inside := stringSlice(agui["inside_dialog"]); len(inside) != 1 || inside[0] != "confirm_card" {
		t.Fatalf("inside dialog ag-ui refs missing: %#v", agui)
	}
	if outside := stringSlice(agui["outside_dialog"]); len(outside) != 2 || outside[0] != "storyboard_panel" || outside[1] != "asset_panel" {
		t.Fatalf("outside dialog ag-ui refs missing: %#v", agui)
	}
	if inputSchema["mode"] != "agent_requested_inputs" {
		t.Fatalf("input schema should describe agent requested inputs: %#v", inputSchema)
	}
	intents, _ := inputSchema["input_intents"].([]any)
	if len(intents) < 3 {
		t.Fatalf("input intents should be derived from natural language input section: %#v", inputSchema)
	}
	rawInputSchema, _ := json.Marshal(inputSchema)
	for _, expected := range []string{"asset_upload", "chips", "textarea"} {
		if !strings.Contains(string(rawInputSchema), expected) {
			t.Fatalf("input schema missing preferred ag-ui %s: %s", expected, string(rawInputSchema))
		}
	}
	if outputSchema["mode"] != "agent_generated_outputs" {
		t.Fatalf("output schema should describe agent generated outputs: %#v", outputSchema)
	}
	outputIntents, _ := outputSchema["output_intents"].([]any)
	if len(outputIntents) < 3 {
		t.Fatalf("output intents should be derived from natural language result section: %#v", outputSchema)
	}
	rawOutputSchema, _ := json.Marshal(outputSchema)
	for _, expected := range []string{"storyboard_panel", "asset_panel", "video_preview"} {
		if !strings.Contains(string(rawOutputSchema), expected) {
			t.Fatalf("output schema missing preferred ag-ui %s: %s", expected, string(rawOutputSchema))
		}
	}
	if memoryPolicy["enabled"] != true {
		t.Fatalf("memory policy should default enabled: %#v", memoryPolicy)
	}

	refs, err := app.toolRefs(t.Context(), detail.SkillID, versionID)
	if err != nil {
		t.Fatalf("load tool refs: %v", err)
	}
	if len(refs) != 2 || refs[0] != "image_generate:model_generation" || refs[1] != "web_fetch:browser" {
		t.Fatalf("tool bindings not persisted: %#v", refs)
	}
}

func TestSaveSkillAutoKeyKeepsChineseNamesDistinct(t *testing.T) {
	app := newSkillTestApp(t)
	auth := accountspace.AuthContext{UserID: "usr_markdown_distinct", LoginIdentityType: "personal"}

	first, err := app.SaveSkill(t.Context(), SaveSkillInput{
		Auth: auth, SkillName: "E2E 审核通过 Skill 20260629_continue_mqyjlrz9", SkillScope: "personal",
		SkillMarkdown: "# E2E 审核通过 Skill 20260629_continue_mqyjlrz9 <名称>\n\n## 说明 <说明>\n\n用于审核通过场景。",
	})
	if err != nil {
		t.Fatalf("save first skill: %v", err)
	}
	second, err := app.SaveSkill(t.Context(), SaveSkillInput{
		Auth: auth, SkillName: "E2E 审核拒绝 Skill 20260629_continue_mqyjlrz9", SkillScope: "personal",
		SkillMarkdown: "# E2E 审核拒绝 Skill 20260629_continue_mqyjlrz9 <名称>\n\n## 说明 <说明>\n\n用于审核拒绝场景。",
	})
	if err != nil {
		t.Fatalf("save second skill: %v", err)
	}
	if first.SkillKey == second.SkillKey {
		t.Fatalf("auto skill keys should be distinct: %q", first.SkillKey)
	}
	if !strings.HasPrefix(first.SkillKey, "e2e_skill_20260629_continue_mqyjlrz9_") {
		t.Fatalf("unexpected first key: %q", first.SkillKey)
	}
}

func TestCompileSkillMarkdownUsesFallbackNameForPlaceholderTitle(t *testing.T) {
	compiled := compileSkillMarkdown("# 未命名 Skill <名称>\n\n## 说明 <说明>\n\n用于测试。", "视频生成", nil)

	var spec map[string]any
	if err := json.Unmarshal([]byte(compiled.SkillSpecJSON), &spec); err != nil {
		t.Fatalf("unmarshal compiled spec: %v", err)
	}
	if spec["name"] != "视频生成" {
		t.Fatalf("placeholder title should fall back to form name: %#v", spec["name"])
	}
}

func loadSkillVersionJSON(t *testing.T, app *App, versionID string) (map[string]any, map[string]any, map[string]any, map[string]any) {
	t.Helper()
	var row struct {
		SkillSpecJSON    []byte `gorm:"column:skill_spec_json"`
		InputSchemaJSON  []byte `gorm:"column:input_schema_json"`
		OutputSchemaJSON []byte `gorm:"column:output_schema_json"`
		MemoryPolicyJSON []byte `gorm:"column:memory_policy_json"`
	}
	if err := app.repo.DB().Raw("SELECT skill_spec_json, input_schema_json, output_schema_json, memory_policy_json FROM skill_versions WHERE id = ?", versionID).Scan(&row).Error; err != nil {
		t.Fatalf("load skill version json: %v", err)
	}
	return mustJSONMap(t, row.SkillSpecJSON), mustJSONMap(t, row.InputSchemaJSON), mustJSONMap(t, row.OutputSchemaJSON), mustJSONMap(t, row.MemoryPolicyJSON)
}

func mustJSONMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal json %s: %v", string(raw), err)
	}
	return out
}

func stringSlice(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}
