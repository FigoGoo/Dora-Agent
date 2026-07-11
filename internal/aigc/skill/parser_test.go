package skill

import (
	"os"
	"strings"
	"testing"
)

func TestParseSkill(t *testing.T) {
	doc := `
<name>
武侠短片创作
</name>
<description>
生成武侠短片。
</description>
<planner>
1. 分析上传素材。 -> ** multimodal_analyze_tool **
   depends_on: []
   pause_after: true
2. 编写 Final_Video_Spec.md。 -> ** text_editor **
   depends_on: [1]
   pause_after: true
3. 生成故事板。 -> ** storyboard_designer **
   depends_on: [1,2]
   pause_after: false
</planner>`

	plan, err := ParseSkill(doc)
	if err != nil {
		t.Fatalf("ParseSkill() error = %v", err)
	}
	if plan.Name != "武侠短片创作" {
		t.Fatalf("unexpected name: %s", plan.Name)
	}
	if len(plan.Stages) != 3 {
		t.Fatalf("unexpected stage count: %d", len(plan.Stages))
	}
	if got := plan.Stages[0].ToolKeys[0]; got != "multimodal_analyze_tool" {
		t.Fatalf("unexpected tool key: %s", got)
	}
	if !plan.Stages[0].PauseAfter {
		t.Fatalf("stage 1 pause_after should be true")
	}
	if got := plan.Stages[2].DependsOn; len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Fatalf("unexpected dependencies: %#v", got)
	}
}

func TestProductVideoSkillUsesAuthoritativeApprovalContract(t *testing.T) {
	content, err := os.ReadFile("../../../商品宣传短片_v2.Skill.md")
	if err != nil {
		t.Fatalf("read product video skill: %v", err)
	}
	plan, err := ParseSkill(string(content))
	if err != nil {
		t.Fatalf("ParseSkill(product video) error = %v", err)
	}
	allowedTools := map[string]bool{
		"analyze_materials":  true,
		"plan_creation_spec": true,
		"plan_storyboard":    true,
		"generate_media":     true,
		"assemble_output":    true,
	}
	for _, stage := range plan.Stages {
		for _, toolKey := range stage.ToolKeys {
			if !allowedTools[toolKey] {
				t.Fatalf("stage %s exposes non-capability tool %q", stage.Key, toolKey)
			}
		}
	}
	for _, required := range []string{
		"携带 approval_id",
		"“确认/拒绝”单选项和“提交”控件",
		"普通聊天文字“确认”只是 UserMessage",
		"左侧故事板统一确认",
		"不要逐素材、逐元素或逐镜头发送 chat A2UI 审核卡",
	} {
		if !strings.Contains(string(content), required) {
			t.Fatalf("product video skill lost approval rule %q", required)
		}
	}
	for _, forbidden := range []string{
		"单镜头流水线",
		"用户确认中断与恢复",
		"询问用户确认当前镜头",
		"<multimodal_analyze_tool>",
		"resource_prepare_and_analyze",
		"<storyboard_designer>",
		"<media_generator>",
		"<write_the_prompt>",
		"<video_assembler>",
	} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("product video skill still contains legacy approval rule %q", forbidden)
		}
	}
}

func TestMemoryStageLedger(t *testing.T) {
	plan := &SkillPlan{Stages: []SkillStage{
		{Key: "1", ToolKeys: []string{"a"}, PauseAfter: true},
		{Key: "2", ToolKeys: []string{"b"}, DependsOn: []string{"1"}},
	}}

	ledger := NewMemoryStageLedger()
	ledger.LoadPlan("session-1", "skill-1", plan)

	if ok, missing := ledger.CanRun("2"); ok || len(missing) != 1 || missing[0] != "1" {
		t.Fatalf("expected stage 2 to be blocked by stage 1, ok=%v missing=%v", ok, missing)
	}
	if err := ledger.Complete("1", "checkpoint-1", []string{"spec-1"}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	run, ok := ledger.Get("1")
	if !ok || run.Status != StageWaitingUser {
		t.Fatalf("stage 1 should wait for user, got %#v", run)
	}
	if err := ledger.Confirm("1"); err != nil {
		t.Fatalf("Confirm() error = %v", err)
	}
	if ok, missing := ledger.CanRun("2"); !ok || len(missing) != 0 {
		t.Fatalf("stage 2 should be runnable, ok=%v missing=%v", ok, missing)
	}
}
