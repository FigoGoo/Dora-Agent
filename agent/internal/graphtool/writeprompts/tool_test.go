package writeprompts

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestToolInfoUsesStrictMinimalIntentSchema(t *testing.T) {
	t.Parallel()
	info, err := (&Tool{}).Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	jsonSchema, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(jsonSchema)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if document["additionalProperties"] != false {
		t.Fatalf("Tool Schema 非 strict: %s", encoded)
	}
	properties, ok := document["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Tool Schema properties 类型错误: %s", encoded)
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"output_language", "schema_version", "writing_instruction"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("Tool Schema properties=%v want=%v", keys, want)
	}
	for _, forbidden := range []string{
		"storyboard_preview_id", "storyboard_preview_ref", "target_local_key", "targets", "positive_prompt",
		"negative_constraints", "prompt", "user_id", "project_id", "session_id", "tool_call_id",
		"business_command_id", "fence_token", "owner", "provider", "asset_id",
	} {
		if strings.Contains(string(encoded), `"`+forbidden+`"`) {
			t.Fatalf("Tool Schema 暴露禁止字段 %q: %s", forbidden, encoded)
		}
	}
}

func TestToolCompletedJSONDoesNotLeakCardOrPromptBody(t *testing.T) {
	t.Parallel()
	contextValue := previewTestGenerationContext()
	modelValue := &previewTestModel{content: previewTestCandidateJSON(t, func(*Candidate) {})}
	store := &previewTestStore{}
	journal := &previewTestJournal{}
	graph := previewTestCompile(t, modelValue, contextValue, store, journal)
	trusted := previewTestTrustedContext(contextValue, 8)
	toolValue, err := NewTool(graph, func(context.Context) (TrustedContext, bool) { return trusted, true })
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := toolValue.InvokableRun(context.Background(), `{"schema_version":"write_prompts.preview.intent.v1","writing_instruction":"为全部目标编写清晰的生成提示词"}`)
	if err != nil {
		t.Fatalf("InvokableRun() error=%v", err)
	}
	for _, forbidden := range []string{
		`"card"`, `"prompts"`, `"positive_prompt"`, `"negative_constraints"`,
		"明亮夏日海边的品牌主视觉", "温暖自然的中文品牌旁白", "避免阴暗色调",
	} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("Tool completed JSON 泄漏内部 Card 或 Prompt 正文 %q: %s", forbidden, encoded)
		}
	}
	var result struct {
		SchemaVersion string       `json:"schema_version"`
		Status        string       `json:"status"`
		ResultCode    string       `json:"result_code"`
		Ref           *ResourceRef `json:"prompt_preview_ref"`
		TargetCount   int          `json:"target_count"`
	}
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != ResultSchemaVersion || result.Status != "completed" ||
		result.ResultCode != ResultCodeCompleted || result.Ref == nil || result.TargetCount != 3 {
		t.Fatalf("result=%+v", result)
	}
}

func TestToolInvalidIntentReturnsStableFailedResult(t *testing.T) {
	t.Parallel()
	contextValue := previewTestGenerationContext()
	modelValue := &previewTestModel{content: previewTestCandidateJSON(t, func(*Candidate) {})}
	store := &previewTestStore{}
	journal := &previewTestJournal{}
	graph := previewTestCompile(t, modelValue, contextValue, store, journal)
	trusted := previewTestTrustedContext(contextValue, 8)
	toolValue, err := NewTool(graph, func(context.Context) (TrustedContext, bool) { return trusted, true })
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := toolValue.InvokableRun(context.Background(), `{"schema_version":"write_prompts.preview.intent.v1","writing_instruction":"合法","target_local_key":"slot_1"}`)
	if err != nil {
		t.Fatalf("InvokableRun() error=%v", err)
	}
	var result struct {
		Status     string `json:"status"`
		ResultCode string `json:"result_code"`
	}
	if err := json.Unmarshal([]byte(encoded), &result); err != nil {
		t.Fatal(err)
	}
	saveCalls, queryCalls := store.counts()
	if result.Status != "failed" || result.ResultCode != ResultCodeInvalidArgument ||
		modelValue.callCount() != 0 || saveCalls != 0 || queryCalls != 0 || journal.calls() != 0 ||
		strings.Contains(encoded, `"card"`) {
		t.Fatalf("result=%+v encoded=%s model=%d save=%d query=%d journal=%d", result, encoded,
			modelValue.callCount(), saveCalls, queryCalls, journal.calls())
	}
}

func TestToolPropagatesUnclassifiedGraphContractError(t *testing.T) {
	t.Parallel()
	contextValue := previewTestGenerationContext()
	modelValue := &previewTestModel{content: previewTestCandidateJSON(t, func(*Candidate) {})}
	store := &previewTestStore{}
	journal := &previewTestJournal{}
	sentinel := errors.New("graph contract broken")
	graph, err := Compile(
		context.Background(), modelValue, &previewTestReader{value: contextValue, err: sentinel}, store, journal,
		previewTestClock{now: time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)},
	)
	if err != nil {
		t.Fatal(err)
	}
	trusted := previewTestTrustedContext(contextValue, 8)
	toolValue, err := NewTool(graph, func(context.Context) (TrustedContext, bool) { return trusted, true })
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := toolValue.InvokableRun(context.Background(), `{"schema_version":"write_prompts.preview.intent.v1","writing_instruction":"为全部目标编写"}`)
	if !errors.Is(err, sentinel) || encoded != "" {
		t.Fatalf("encoded=%q error=%v want sentinel", encoded, err)
	}
}
