package turncontext

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestPlanStoryboardRuntimeContextRoundTrip 验证 Runtime Context 使用私有类型 Key 且保持值语义。
func TestPlanStoryboardRuntimeContextRoundTrip(t *testing.T) {
	value := PlanStoryboardRuntime{
		Owner: "owner-a", FenceToken: 7, IntentJSON: `{"schema_version":"plan_storyboard.preview.intent.v1"}`,
		Context: PlanStoryboardTurnContext{
			SchemaVersion:     PlanStoryboardTurnContextSchemaVersion,
			Profile:           PlanStoryboardRuntimeProfile,
			RequestID:         "019b95d3-e600-7000-8000-000000000000",
			BusinessCommandID: "019b95d3-e600-7000-8000-000000000001",
			CreationSpecID:    "019b95d3-e600-7000-8000-000000000002",
		},
	}
	ctx := WithPlanStoryboardRuntime(context.Background(), value)
	got, ok := PlanStoryboardRuntimeFrom(ctx)
	if !ok || got.Owner != value.Owner || got.FenceToken != value.FenceToken ||
		got.Context.SchemaVersion != PlanStoryboardTurnContextSchemaVersion ||
		got.Context.BusinessCommandID != value.Context.BusinessCommandID {
		t.Fatalf("Storyboard Runtime Context 往返异常: got=%+v ok=%v", got, ok)
	}
	if _, ok := PlanStoryboardRuntimeFrom(context.Background()); ok {
		t.Fatal("空 Context 不应返回 Storyboard Runtime")
	}
	encoded, err := json.Marshal(got.Context)
	if err != nil {
		t.Fatalf("编码 Storyboard Turn Context 失败: %v", err)
	}
	if !strings.Contains(string(encoded), `"request_id"`) || strings.Contains(string(encoded), `"RequestID"`) {
		t.Fatalf("Storyboard Turn Context JSON key 漂移: %s", encoded)
	}
}

// TestPlanStoryboardPreviewContextRoundTrip 验证 Tool Core 只能读取显式注入的最小可信值。
func TestPlanStoryboardPreviewContextRoundTrip(t *testing.T) {
	value := PlanStoryboardPreview{
		Owner: "owner-b", RequestID: "019b95d3-e600-7000-8000-000000000003",
		BusinessCommandID: "019b95d3-e600-7000-8000-000000000004", FenceToken: 11,
		CreationSpecID: "019b95d3-e600-7000-8000-000000000005", CreationSpecVersion: 1,
		CreationSpecContentDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PromptVersion:             "prompt.v1", ValidatorVersion: "validator.v1", DAGValidatorVersion: "dag.v1",
	}
	ctx := WithPlanStoryboardPreview(context.Background(), value)
	got, ok := PlanStoryboardPreviewFrom(ctx)
	if !ok || got.Owner != value.Owner || got.FenceToken != value.FenceToken ||
		got.CreationSpecID != value.CreationSpecID || got.DAGValidatorVersion != value.DAGValidatorVersion {
		t.Fatalf("Storyboard Preview Context 往返异常: got=%+v ok=%v", got, ok)
	}
	if _, ok := PlanStoryboardPreviewFrom(context.Background()); ok {
		t.Fatal("空 Context 不应返回 Storyboard Preview")
	}
}
