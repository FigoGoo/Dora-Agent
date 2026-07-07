package skill

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// fakeChatModel 用固定文本回应 Generate，模拟 LLM 分类输出。
type fakeChatModel struct {
	reply string
	err   error
	calls int
}

func (f *fakeChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return schema.AssistantMessage(f.reply, nil), nil
}

func (f *fakeChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("not implemented")
}

func testOptions() []SkillOption {
	return []SkillOption{
		{ID: "sk_product", Name: "商品宣传短片", Description: "电商带货、卖点驱动的短片"},
		{ID: "sk_travel", Name: "人文纪录短片", Description: "城市/文旅/纪实风格"},
	}
}

func TestLLMSkillSelectorPicksCandidate(t *testing.T) {
	fm := &fakeChatModel{reply: `{"skill_id":"sk_travel","reason":"文旅题材"}`}
	sel := NewLLMSkillSelector(fm)

	got, err := sel.Select(context.Background(), "帮我做北京平谷文旅宣传片", testOptions())
	if err != nil {
		t.Fatalf("Select 出错: %v", err)
	}
	if got.SkillID != "sk_travel" {
		t.Fatalf("SkillID = %q, 期望 sk_travel", got.SkillID)
	}
	if got.Reason != "文旅题材" {
		t.Fatalf("Reason = %q", got.Reason)
	}
	if got.Fallback {
		t.Fatalf("正常命中不应 Fallback")
	}
	if fm.calls != 1 {
		t.Fatalf("应调用 LLM 一次, 实际 %d", fm.calls)
	}
}

func TestLLMSkillSelectorRejectsUnknownID(t *testing.T) {
	fm := &fakeChatModel{reply: `{"skill_id":"sk_nope","reason":"x"}`}
	sel := NewLLMSkillSelector(fm)

	_, err := sel.Select(context.Background(), "brief", testOptions())
	if err == nil {
		t.Fatalf("越界 skill_id 应返回 error")
	}
	if !errors.Is(err, ErrUnknownSkillID) {
		t.Fatalf("越界 skill_id 应包裹 ErrUnknownSkillID, 实际 %v", err)
	}
}

func TestLLMSkillSelectorRejectsBadJSON(t *testing.T) {
	fm := &fakeChatModel{reply: "这不是 JSON"}
	sel := NewLLMSkillSelector(fm)

	_, err := sel.Select(context.Background(), "brief", testOptions())
	if err == nil {
		t.Fatalf("非法 JSON 应返回 error")
	}
	if !errors.Is(err, ErrSkillSelectionParse) {
		t.Fatalf("非法 JSON 应包裹 ErrSkillSelectionParse, 实际 %v", err)
	}
}

func TestLLMSkillSelectorExtractsJSONFromProse(t *testing.T) {
	fm := &fakeChatModel{reply: `好的，结果是 {"skill_id":"sk_travel","reason":"文旅"} 供参考`}
	sel := NewLLMSkillSelector(fm)

	got, err := sel.Select(context.Background(), "brief", testOptions())
	if err != nil {
		t.Fatalf("Select 出错: %v", err)
	}
	if got.SkillID != "sk_travel" {
		t.Fatalf("SkillID = %q, 期望 sk_travel", got.SkillID)
	}
	if got.Reason != "文旅" {
		t.Fatalf("Reason = %q, 期望 文旅", got.Reason)
	}
}
