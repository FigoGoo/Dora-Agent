package eino

import (
	"context"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/boardgraph"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/skillgraph"
)

func TestSkillGraphRunnerExecutesPublishedSkillSpec(t *testing.T) {
	runner, err := NewSkillGraphRunner(t.Context(), func() time.Time { return time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatalf("new skill graph runner: %v", err)
	}

	result, err := runner.Execute(context.Background(), skillgraph.Input{
		RunID:                "run_skill_graph_eino_001",
		ProjectID:            "prj_active_1001",
		SessionID:            "sess_skill_graph_eino_001",
		SpaceID:              "sp_personal_1001",
		ActorUserID:          "usr_1001",
		TraceID:              "trace-skill-graph-eino",
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
		OutputElements: []skillgraph.OutputElement{{
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
	})
	if err != nil {
		t.Fatalf("execute skill graph runner: %v", err)
	}
	if result.GraphTemplate.GraphType != boardgraph.GraphTypeSystemSkill || result.GraphPlan.GraphTemplateID == "gtemplate_generic_creation" {
		t.Fatalf("expected published skill graph, got %#v", result.GraphTemplate)
	}
	if len(result.Events) != 2 || result.Events[0].EventType != boardgraph.EventTypeGraphPlanCreated || result.Events[1].EventType != boardgraph.EventTypeBoardSnapshotUpdated {
		t.Fatalf("unexpected events: %#v", result.Events)
	}
}
