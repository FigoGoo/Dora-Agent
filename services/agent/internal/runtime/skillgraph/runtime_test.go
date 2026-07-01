package skillgraph

import (
	"context"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/boardgraph"
)

func TestExecutePublishedSkillBuildsStableGraphBoardAndEvents(t *testing.T) {
	runtime := New(func() time.Time { return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) })
	input := skillGraphTestInput()

	first, err := runtime.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute first: %v", err)
	}
	second, err := runtime.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute second: %v", err)
	}
	if first.SkillSpecDigest == "" || first.SkillSpecDigest != second.SkillSpecDigest {
		t.Fatalf("skill spec digest should be stable, first=%q second=%q", first.SkillSpecDigest, second.SkillSpecDigest)
	}
	if first.GraphPlan.GraphTemplateID == "gtemplate_generic_creation" || first.GraphTemplate.GraphType != boardgraph.GraphTypeSystemSkill {
		t.Fatalf("expected system Skill graph, template=%#v", first.GraphTemplate)
	}
	if first.GraphPlan.GraphPlanDigest != second.GraphPlan.GraphPlanDigest || first.Board.BoardDigest != second.Board.BoardDigest {
		t.Fatalf("graph or board digest should be stable, first=%#v second=%#v", first.GraphPlan, second.GraphPlan)
	}
	if len(first.Events) != 2 || first.Events[0].EventType != boardgraph.EventTypeGraphPlanCreated || first.Events[1].EventType != boardgraph.EventTypeBoardSnapshotUpdated {
		t.Fatalf("unexpected events: %#v", first.Events)
	}
}

func TestExecuteRejectsNonPublishedSkillSpec(t *testing.T) {
	runtime := New(func() time.Time { return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) })
	input := skillGraphTestInput()
	input.SkillSpecJSON = `{
		"schema_version":"skill_runtime_spec.v1",
		"skill_id":"skill_city_tourism_video",
		"version":"1.0.0",
		"status":"draft",
		"level":"L3",
		"scope":"system_default",
		"graph_template":{"entry_node":"brief_builder","nodes":[{"node_key":"brief_builder","node_type":"llm"}]}
	}`
	if _, err := runtime.Execute(context.Background(), input); err == nil {
		t.Fatal("expected non-published skill spec to be rejected")
	}
}

func TestExecuteRejectsBrokenGraphSpec(t *testing.T) {
	runtime := New(func() time.Time { return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) })
	input := skillGraphTestInput()
	input.SkillSpecJSON = `{
		"schema_version":"skill_runtime_spec.v1",
		"skill_id":"skill_city_tourism_video",
		"version":"1.0.0",
		"status":"published",
		"level":"L3",
		"scope":"system_default",
		"graph_template":{
			"entry_node":"brief_builder",
			"nodes":[{"node_key":"brief_builder","node_type":"llm"}],
			"edges":[{"from":"brief_builder","to":"missing_gate"}]
		}
	}`
	if _, err := runtime.Execute(context.Background(), input); err == nil {
		t.Fatal("expected graph spec with undeclared edge node to be rejected")
	}
}

func skillGraphTestInput() Input {
	return Input{
		RunID:                "run_skill_graph_001",
		ProjectID:            "prj_active_1001",
		SessionID:            "sess_skill_graph_001",
		SpaceID:              "sp_personal_1001",
		ActorUserID:          "usr_1001",
		TraceID:              "trace-skill-graph",
		Prompt:               "帮我做一个杭州夏季文旅宣传视频，现代国风，30 秒",
		SkillID:              "skill_city_tourism_video",
		SkillVersion:         "1.0.0",
		SkillSource:          "system_default",
		RouterDecisionDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		SkillSpecJSON: `{
			"schema_version":"skill_runtime_spec.v1",
			"skill_id":"skill_city_tourism_video",
			"version":"1.0.0",
			"status":"published",
			"level":"L3",
			"scope":"system_default",
			"stages":["brief","storyboard","board_review"],
			"graph_template":{
				"entry_node":"brief_builder",
				"nodes":[
					{"node_key":"brief_builder","node_type":"llm","display_name":"生成 brief"},
					{"node_key":"storyboard_planner","node_type":"llm","display_name":"生成分镜"},
					{"node_key":"board_review_gate","node_type":"user_gate","display_name":"Board 审核"}
				],
				"edges":[
					{"from":"brief_builder","to":"storyboard_planner"},
					{"from":"storyboard_planner","to":"board_review_gate"}
				],
				"terminal_nodes":["board_review_gate"]
			}
		}`,
		OutputElements: []OutputElement{{
			ElementType:  boardgraph.BoardElementTypeStoryboardFrame,
			ElementName:  "分镜",
			Required:     true,
			UseDraft:     true,
			UseFinal:     true,
			Editable:     true,
			Referable:    true,
			DisplayOrder: 1,
			DisplaySlot:  "board",
		}},
	}
}
