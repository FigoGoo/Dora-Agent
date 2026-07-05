package skill

import (
	"context"
	"testing"
)

func TestEinoBackendListsAndLoadsEnabledSkills(t *testing.T) {
	source := fakeSkillRecordSource{
		records: []SkillRecord{
			{
				ID:          "video",
				Name:        "短视频创作",
				Description: "生成故事板与素材",
				Content:     "<name>短视频创作</name><description>生成故事板与素材</description><planner>1. 分析 **resource_prepare_and_analyze**</planner>",
				Enabled:     true,
			},
		},
	}
	backend := NewEinoBackend(source)

	matters, err := backend.List(context.Background())
	if err != nil {
		t.Fatalf("list skills: %v", err)
	}
	if len(matters) != 1 {
		t.Fatalf("front matter count = %d", len(matters))
	}
	if matters[0].Name != "短视频创作" || matters[0].Description != "生成故事板与素材" {
		t.Fatalf("front matter = %#v", matters[0])
	}

	got, err := backend.Get(context.Background(), "短视频创作")
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	if got.Name != "短视频创作" {
		t.Fatalf("skill name = %q", got.Name)
	}
	if got.BaseDirectory != "db://aigc_skills/video" {
		t.Fatalf("base directory = %q", got.BaseDirectory)
	}
	if got.Content == "" {
		t.Fatal("skill content is empty")
	}
}

type fakeSkillRecordSource struct {
	records []SkillRecord
}

func (s fakeSkillRecordSource) ListEnabled(context.Context) ([]SkillRecord, error) {
	return append([]SkillRecord(nil), s.records...), nil
}

func (s fakeSkillRecordSource) GetEnabledByName(_ context.Context, name string) (SkillRecord, error) {
	for _, record := range s.records {
		if record.Name == name || record.ID == name {
			return record, nil
		}
	}
	return SkillRecord{}, ErrSkillNotFound
}
