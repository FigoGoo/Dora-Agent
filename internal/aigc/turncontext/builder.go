package turncontext

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/session"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/spec"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

type SkillSource interface {
	Get(ctx context.Context, skillID string) (skill.SkillRecord, error)
}

type SpecSource interface {
	GetLatestBySession(ctx context.Context, sessionID string) (spec.FinalVideoSpec, error)
}

type StoryboardSource interface {
	GetLatestBySession(ctx context.Context, sessionID string) (storyboard.Storyboard, error)
}

type Config struct {
	Skills      SkillSource
	Specs       SpecSource
	Storyboards StoryboardSource
}

type Builder struct {
	cfg Config
}

func NewBuilder(cfg Config) *Builder {
	return &Builder{cfg: cfg}
}

func (b *Builder) Build(ctx context.Context, record session.SessionRecord) (string, error) {
	if b == nil {
		return "", nil
	}

	var out strings.Builder
	out.WriteString("AIGC 创作会话上下文\n")
	out.WriteString(fmt.Sprintf("Session: id=%s title=%s skill_id=%s\n", record.ID, record.Title, record.SkillID))
	out.WriteString("请基于以下最新状态继续多轮创作；如果用户要求修改，请优先修改已有规范、故事板或素材状态，而不是从零开始。\n")

	if b.cfg.Skills != nil && strings.TrimSpace(record.SkillID) != "" {
		skillRecord, err := b.cfg.Skills.Get(ctx, record.SkillID)
		if err != nil && !errors.Is(err, skill.ErrSkillNotFound) {
			return "", err
		}
		if err == nil {
			appendSkill(&out, skillRecord)
		}
	}
	if b.cfg.Specs != nil {
		currentSpec, err := b.cfg.Specs.GetLatestBySession(ctx, record.ID)
		if err != nil && !errors.Is(err, spec.ErrNotFound) {
			return "", err
		}
		if err == nil {
			appendSpec(&out, currentSpec)
		}
	}
	if b.cfg.Storyboards != nil {
		board, err := b.cfg.Storyboards.GetLatestBySession(ctx, record.ID)
		if err != nil && !errors.Is(err, storyboard.ErrNotFound) {
			return "", err
		}
		if err == nil {
			appendStoryboard(&out, board)
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func appendSkill(out *strings.Builder, record skill.SkillRecord) {
	plan, err := skill.ParseSkill(record.Content)
	if err != nil {
		out.WriteString(fmt.Sprintf("Skill: id=%s name=%s description=%s\n", record.ID, record.Name, record.Description))
		return
	}
	out.WriteString(fmt.Sprintf("Skill: %s id=%s description=%s\n", plan.Name, record.ID, plan.Description))
	for _, stage := range plan.Stages {
		out.WriteString(fmt.Sprintf(
			"Stage %s: %s tools=%s depends_on=%s pause_after=%t\n",
			stage.Key,
			stage.Title,
			strings.Join(stage.ToolKeys, ","),
			strings.Join(stage.DependsOn, ","),
			stage.PauseAfter,
		))
	}
}

func appendSpec(out *strings.Builder, value spec.FinalVideoSpec) {
	out.WriteString(fmt.Sprintf(
		"FinalVideoSpec: id=%s version=%d status=%s title=%s type=%s duration=%ds aspect_ratio=%s\n",
		value.ID,
		value.Version,
		value.Status,
		value.Title,
		value.VideoType,
		value.DurationSeconds,
		value.AspectRatio,
	))
	if value.VisualStyle != "" {
		out.WriteString("VisualStyle: " + value.VisualStyle + "\n")
	}
	if value.SoundStyle != "" {
		out.WriteString("SoundStyle: " + value.SoundStyle + "\n")
	}
	if value.ModelPreference != "" {
		out.WriteString("ModelPreference: " + value.ModelPreference + "\n")
	}
}

func appendStoryboard(out *strings.Builder, board storyboard.Storyboard) {
	out.WriteString(fmt.Sprintf(
		"Storyboard: id=%s version=%d status=%s spec_id=%s key_elements=%d shots=%d audio_layers=%d\n",
		board.ID,
		board.Version,
		board.Status,
		board.SpecID,
		len(board.KeyElements),
		len(board.Shots),
		len(board.AudioLayers),
	))
	for _, element := range board.KeyElements {
		out.WriteString(fmt.Sprintf(
			"Element %s(%s): %s status=%s assets=%s\n",
			element.Key,
			element.Type,
			element.Name,
			element.Status,
			strings.Join(element.AssetIDs, ","),
		))
	}
	for _, shot := range board.Shots {
		out.WriteString(fmt.Sprintf(
			"Shot %d/%s: %s status=%s refs=%s keyframe=%s video=%s\n",
			shot.Index,
			shot.ShotID,
			shot.SceneDescription,
			shot.Status,
			strings.Join(shot.ReferenceElements, ","),
			shot.KeyframeAssetID,
			shot.VideoAssetID,
		))
	}
	for _, layer := range board.AudioLayers {
		out.WriteString(fmt.Sprintf(
			"Audio %s(%s): %s status=%s asset=%s\n",
			layer.LayerID,
			layer.Type,
			layer.Description,
			layer.Status,
			layer.AssetID,
		))
	}
}
