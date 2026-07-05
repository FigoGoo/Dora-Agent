package turncontext

import (
	"context"
	"strings"
	"testing"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

func TestBuilderSummarizesCurrentCreativeState(t *testing.T) {
	builder := NewBuilder(Config{
		Skills: fakeSkillSource{record: skill.SkillRecord{
			ID:      "skill-video",
			Content: sampleSkillContent(),
			Enabled: true,
		}},
		Specs: fakeSpecSource{value: spec.FinalVideoSpec{
			ID:              "spec-1",
			SessionID:       "s1",
			Version:         2,
			Status:          spec.StatusReviewing,
			Title:           "归隐·藏锋",
			DurationSeconds: 120,
			AspectRatio:     "16:9",
			VisualStyle:     "真人电影实拍风格，冷郁竹林光影",
		}},
		Storyboards: fakeStoryboardSource{value: storyboard.Storyboard{
			ID:        "storyboard-1",
			SessionID: "s1",
			Version:   3,
			Status:    storyboard.StatusReviewing,
			KeyElements: []storyboard.KeyElement{
				{Key: "suji", Type: "character", Name: "苏寂", Status: "planned"},
			},
			Shots: []storyboard.Shot{
				{ShotID: "shot-1", Index: 1, SceneDescription: "竹林归隐", Status: "planned"},
			},
			AudioLayers: []storyboard.AudioLayer{
				{LayerID: "music-1", Type: "music", Description: "悲凉沉郁"},
			},
		}},
	})

	out, err := builder.Build(context.Background(), session.SessionRecord{
		ID:      "s1",
		SkillID: "skill-video",
		Title:   "武侠短片",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	for _, want := range []string{
		"AIGC 创作会话上下文",
		"Skill: 武侠短片创作",
		"Stage 1: 编写 Final_Video_Spec.md。",
		"FinalVideoSpec: id=spec-1 version=2 status=reviewing title=归隐·藏锋",
		"Storyboard: id=storyboard-1 version=3 status=reviewing",
		"Element suji(character): 苏寂 status=planned",
		"Shot 1/shot-1: 竹林归隐 status=planned",
		"Audio music-1(music): 悲凉沉郁",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("context missing %q in:\n%s", want, out)
		}
	}
}

func sampleSkillContent() string {
	return `<name>
武侠短片创作
</name>
<description>
生成武侠短片。
</description>
<planner>
1. 编写 Final_Video_Spec.md。 -> ** text_editor **
   depends_on: []
   pause_after: true
2. 生成故事板。 -> ** storyboard_designer **
   depends_on: [1]
   pause_after: true
</planner>`
}

type fakeSkillSource struct {
	record skill.SkillRecord
	err    error
}

func (s fakeSkillSource) Get(_ context.Context, _ string) (skill.SkillRecord, error) {
	if s.err != nil {
		return skill.SkillRecord{}, s.err
	}
	return s.record, nil
}

type fakeSpecSource struct {
	value spec.FinalVideoSpec
	err   error
}

func (s fakeSpecSource) GetLatestBySession(_ context.Context, _ string) (spec.FinalVideoSpec, error) {
	if s.err != nil {
		return spec.FinalVideoSpec{}, s.err
	}
	return s.value, nil
}

type fakeStoryboardSource struct {
	value storyboard.Storyboard
	err   error
}

func (s fakeStoryboardSource) GetLatestBySession(_ context.Context, _ string) (storyboard.Storyboard, error) {
	if s.err != nil {
		return storyboard.Storyboard{}, s.err
	}
	return s.value, nil
}
