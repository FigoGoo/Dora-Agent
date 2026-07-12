package orchestration

import (
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/capability"
)

func TestApprovalContinuationRulesAreValid(t *testing.T) {
	if err := ValidateApprovalContinuationRules(); err != nil {
		t.Fatalf("shipped rules must be valid: %v", err)
	}
}

func TestDecideApprovalContinuation(t *testing.T) {
	tests := []struct {
		name         string
		artifactType string
		guards       map[string]bool
		wantTool     string
		wantArgs     string
		wantInstr    string // 判包含（逐字全文由 server 黄金测试锁定）
	}{
		{
			name: "spec v1", artifactType: "creation_spec_revision",
			guards:   map[string]bool{},
			wantTool: capability.PlanStoryboardToolKey, wantArgs: `{"mode":"create"}`,
			wantInstr: "必须调用 plan_storyboard；禁止再次调用 plan_creation_spec",
		},
		{
			name: "spec revision", artifactType: "creation_spec_revision",
			guards:   map[string]bool{GuardArtifactVersionGt1: true},
			wantTool: capability.PlanStoryboardToolKey, wantArgs: `{"mode":"replan","preserve_approved_assets":true}`,
			wantInstr: "必须调用 plan_storyboard；禁止再次调用 plan_creation_spec",
		},
		{
			name: "storyboard not complete", artifactType: "storyboard_revision",
			guards:   map[string]bool{},
			wantTool: capability.GenerateMediaToolKey, wantArgs: `{"phase":"auto_next","policy":"all_eligible"}`,
			wantInstr: "禁止再次调用 plan_storyboard",
		},
		{
			name: "storyboard complete", artifactType: "storyboard_revision",
			guards:   map[string]bool{GuardProductionComplete: true},
			wantTool: capability.AssembleOutputToolKey, wantArgs: `{"mode":"preview","output_type":"video"}`,
			wantInstr: "全部生产槽位已激活",
		},
		{
			name: "candidate not complete", artifactType: "candidate_asset",
			guards:   map[string]bool{},
			wantTool: capability.GenerateMediaToolKey, wantArgs: `{"phase":"auto_next","policy":"all_eligible"}`,
			wantInstr: "必须继续调用 generate_media",
		},
		{
			name: "candidate complete", artifactType: "candidate_asset",
			guards:   map[string]bool{GuardProductionComplete: true},
			wantTool: capability.AssembleOutputToolKey, wantArgs: `{"mode":"preview","output_type":"video"}`,
			wantInstr: "全部生产槽位已激活",
		},
		{
			name: "unknown type", artifactType: "unknown",
			guards:    map[string]bool{GuardProductionComplete: true},
			wantInstr: "没有定义确定性下一阶段",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := DecideApprovalContinuation(ApprovalContinuationInput{
				ArtifactType: test.artifactType, Guards: test.guards,
			})
			if !strings.Contains(decision.Instruction, test.wantInstr) {
				t.Fatalf("instruction %q must contain %q", decision.Instruction, test.wantInstr)
			}
			if test.wantTool == "" {
				if decision.Node != nil {
					t.Fatalf("unexpected node %+v", decision.Node)
				}
				return
			}
			if decision.Node == nil {
				t.Fatal("expected a next node")
			}
			if decision.Node.Kind != NodeKindCapability || decision.Node.ToolKey != test.wantTool || string(decision.Node.Arguments) != test.wantArgs {
				t.Fatalf("node = %+v", decision.Node)
			}
		})
	}
}
