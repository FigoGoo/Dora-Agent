package writepromptsruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/agent/internal/graphtool/writeprompts"
	"github.com/cloudwego/eino/schema"
)

func TestFakePromptModelProducesValidatorCompatibleCandidate(t *testing.T) {
	t.Parallel()
	targets := []writeprompts.PromptTarget{{
		TargetLocalKey: "slot_1", ElementLocalKey: "element_1",
		ElementTitle: "开场", NarrativePurpose: "建立氛围",
		SlotType: "image", MediaKind: "image", Purpose: "核心主视觉", Required: true,
	}}
	targetsJSON, err := json.Marshal(targets)
	if err != nil {
		t.Fatal(err)
	}
	messages := []*schema.Message{
		schema.SystemMessage("只输出严格候选 JSON"),
		schema.UserMessage("prompt_version=test\nexact_targets_json=" + string(targetsJSON) + "\n为每个 exact target 生成提示词"),
	}
	response, err := NewFakePromptModel().Generate(context.Background(), messages)
	if err != nil {
		t.Fatalf("FakePromptModel.Generate() error=%v", err)
	}
	candidate, err := writeprompts.DecodeAndValidateCandidate([]byte(response.Content))
	if err != nil {
		t.Fatalf("DecodeAndValidateCandidate() error=%v content=%s", err, response.Content)
	}
	content, err := writeprompts.ValidateExactTargetSet(candidate, targets, "zh-CN", writeprompts.StoryboardPreviewRef{
		ID: "019f0000-0000-7000-8000-000000000001", Version: 1, ContentDigest: strings.Repeat("a", 64),
	})
	if err != nil {
		t.Fatalf("ValidateExactTargetSet() error=%v", err)
	}
	if candidate.Prompts[0].NegativeConstraints == nil || content.Prompts[0].NegativeConstraints == nil {
		t.Fatal("negative_constraints 必须跨 Fake、候选 Validator 与 Content Validator 保持非 null 空数组")
	}
	if !strings.Contains(response.Content, `"negative_constraints":[]`) {
		t.Fatalf("Fake JSON 空数组退化: %s", response.Content)
	}
}
