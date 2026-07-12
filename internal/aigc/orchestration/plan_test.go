package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/vocabulary"
)

// staticTool 是校验/调度测试共用的可配置假工具。
type staticTool struct {
	key    string
	inputs map[string]vocabulary.ParamSpec
	run    func(call vocabulary.Call) (vocabulary.Result, error)
}

func (t staticTool) Descriptor() vocabulary.Descriptor {
	return vocabulary.Descriptor{Key: t.key, Name: t.key, Description: "test", Category: "cognition", Inputs: t.inputs}
}

func (t staticTool) Run(_ context.Context, call vocabulary.Call) (vocabulary.Result, error) {
	if t.run != nil {
		return t.run(call)
	}
	return vocabulary.Result{Outputs: map[string]any{"ok": true}}, nil
}

func testVocabulary(t *testing.T) *vocabulary.Registry {
	t.Helper()
	registry := vocabulary.NewRegistry()
	for _, tool := range []vocabulary.Tool{
		staticTool{key: "write_media_prompt", inputs: map[string]vocabulary.ParamSpec{
			"target_desc": {Type: "string", Required: true},
		}},
		staticTool{key: "dispatch_generation", inputs: map[string]vocabulary.ParamSpec{
			"targets": {Type: "array", Required: true},
		}},
		staticTool{key: "request_confirmation", inputs: map[string]vocabulary.ParamSpec{
			"question": {Type: "string", Required: true},
		}},
	} {
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
	}
	return registry
}

func validPlan() ExecutionPlan {
	return ExecutionPlan{
		PlanID: "plan-1", Source: "dynamic", Summary: "一张图", Direction: "image",
		Steps: []PlanStep{
			{ID: "prompt", Tool: "write_media_prompt", Params: map[string]any{"target_desc": "雨中柴犬"}, Required: true},
			{ID: "confirm", Tool: "request_confirmation", Params: map[string]any{"question": "提示词可以吗"}, DependsOn: []string{"prompt"}, Required: true},
			{ID: "generate", Tool: "dispatch_generation", Params: map[string]any{"targets": []any{map[string]any{"prompt": "$prompt.prompt"}}}, DependsOn: []string{"confirm"}, Required: true},
		},
		EstimatedJobs: 1,
	}
}

func TestPlanValidate(t *testing.T) {
	registry := testVocabulary(t)
	if err := validPlan().Validate(registry, 20); err != nil {
		t.Fatalf("valid plan: %v", err)
	}

	cyclic := validPlan()
	cyclic.Steps[0].DependsOn = []string{"generate"} // prompt→generate→confirm→prompt 成环
	if err := cyclic.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("cycle must be rejected: %v", err)
	}

	unknownTool := validPlan()
	unknownTool.Steps[0].Tool = "no_such_tool"
	if err := unknownTool.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("unknown tool must be rejected: %v", err)
	}

	danglingDep := validPlan()
	danglingDep.Steps[1].DependsOn = []string{"ghost"}
	if err := danglingDep.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "unknown step") {
		t.Fatalf("dangling dependency must be rejected: %v", err)
	}

	missingRequired := validPlan()
	delete(missingRequired.Steps[0].Params, "target_desc")
	if err := missingRequired.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "required param") {
		t.Fatalf("missing required param must be rejected: %v", err)
	}

	rawSlot := validPlan()
	rawSlot.Steps[0].Params["target_desc"] = "<PROMPT>"
	if err := rawSlot.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "slot") {
		t.Fatalf("uninstantiated slot must be rejected: %v", err)
	}

	expand := validPlan()
	expand.Steps[2].Expand = &ExpandSpec{Over: "targets"}
	if err := expand.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "expand") {
		t.Fatalf("expand is reserved and must be rejected in v1: %v", err)
	}

	dupID := validPlan()
	dupID.Steps[1].ID = "prompt"
	if err := dupID.Validate(registry, 20); err == nil {
		t.Fatal("duplicate step id must be rejected")
	}

	refUnknown := validPlan()
	refUnknown.Steps[2].Params["targets"] = "$ghost.prompt"
	if err := refUnknown.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "reference") {
		t.Fatalf("reference to unknown step must be rejected: %v", err)
	}

	// 畸形引用（非"指向未知 step"，而是引用语法本身不合法）：单段无点、首段空、尾段空。
	malformedRef := validPlan()
	for _, ref := range []string{"$prompt", "$.", "$prompt."} {
		malformedRef.Steps[2].Params["targets"] = ref
		if err := malformedRef.Validate(registry, 20); err == nil || !strings.Contains(err.Error(), "malformed reference") {
			t.Fatalf("malformed reference %q must be rejected: %v", ref, err)
		}
	}

	// 所有校验错误必须可被 errors.Is(err, ErrPlanInvalid) 识别（修复循环的结构化错误约定）。
	if err := cyclic.Validate(registry, 20); !errors.Is(err, ErrPlanInvalid) {
		t.Fatalf("validation errors must wrap ErrPlanInvalid: %v", err)
	}
}

func TestPlanBudgetCheck(t *testing.T) {
	registry := testVocabulary(t)
	plan := validPlan()
	plan.EstimatedJobs = 25
	// 预算超限不是校验错误——由 Submit 转预览，Validate 仅回报超限事实。
	if !plan.ExceedsJobBudget(20) {
		t.Fatal("estimated 25 must exceed budget 20")
	}
	if err := plan.Validate(registry, 20); err != nil {
		t.Fatalf("over-budget plan is still structurally valid: %v", err)
	}
}
