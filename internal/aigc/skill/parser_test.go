package skill

import "testing"

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
