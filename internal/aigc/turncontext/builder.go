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

type ConfirmedSpecSource interface {
	GetConfirmedBySession(ctx context.Context, sessionID string) (spec.FinalVideoSpec, error)
}

type StoryboardSource interface {
	GetLatestBySession(ctx context.Context, sessionID string) (storyboard.Storyboard, error)
}

type DynamicStoryboardSource interface {
	GetAggregateBySession(ctx context.Context, sessionID string) (storyboard.StoryboardAggregate, error)
}

type Config struct {
	Skills      SkillSource
	Specs       SpecSource
	Storyboards StoryboardSource
	// DynamicStoryboards is the production source. Storyboards remains only as
	// a compatibility projection for old sessions during migration.
	DynamicStoryboards DynamicStoryboardSource
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
		if confirmedSource, ok := b.cfg.Specs.(ConfirmedSpecSource); ok {
			confirmed, confirmedErr := confirmedSource.GetConfirmedBySession(ctx, record.ID)
			if confirmedErr != nil && !errors.Is(confirmedErr, spec.ErrNotFound) {
				return "", confirmedErr
			}
			if confirmedErr == nil {
				appendSpec(&out, confirmed)
			}
			if err == nil && (confirmedErr != nil || currentSpec.Version != confirmed.Version) {
				out.WriteString("LatestSpecCandidate: ")
				appendSpec(&out, currentSpec)
			}
		} else if err == nil {
			appendSpec(&out, currentSpec)
		}
	}
	if b.cfg.DynamicStoryboards != nil {
		board, err := b.cfg.DynamicStoryboards.GetAggregateBySession(ctx, record.ID)
		if err != nil && !errors.Is(err, storyboard.ErrAggregateNotFound) {
			return "", err
		}
		if err == nil {
			appendDynamicStoryboard(&out, board)
		}
	} else if b.cfg.Storyboards != nil {
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

func appendDynamicStoryboard(out *strings.Builder, board storyboard.StoryboardAggregate) {
	out.WriteString(fmt.Sprintf(
		"DynamicStoryboard: id=%s version=%d plan_revision=%d status=%s active_revision=%s pending_revision=%s\n",
		board.ID,
		board.Version,
		board.PlanRevision,
		board.Status,
		board.ActiveRevisionID,
		board.PendingRevisionID,
	))
	appendRevision := func(label string, revision *storyboard.StoryboardRevision) {
		if revision == nil {
			return
		}
		out.WriteString(fmt.Sprintf("%s: id=%s status=%s scenario=%s modules=%d\n", label, revision.ID, revision.Status, revision.Scenario, len(revision.Modules)))
		for _, module := range revision.Modules {
			out.WriteString(fmt.Sprintf("Module %s key=%s type=%s title=%s count=%d required=%t\n", module.ID, module.Key, module.SemanticType, module.Title, module.PlannedCount, module.Required))
			for _, element := range module.Elements {
				out.WriteString(fmt.Sprintf("Target %s key=%s type=%s title=%s revision=%d review=%s\n", element.ID, element.Key, element.SemanticType, element.Title, element.Revision, element.ReviewState))
				for _, prompt := range element.PromptSlots {
					out.WriteString(fmt.Sprintf("Prompt target=%s purpose=%s revision=%d status=%s locked=%t\n", element.ID, prompt.Purpose, prompt.Revision, prompt.Status, prompt.LockedByUser))
				}
				for _, slot := range element.AssetSlots {
					out.WriteString(fmt.Sprintf("AssetSlot target=%s key=%s kind=%s status=%s required=%t epoch=%d active=%s candidates=%s\n", element.ID, slot.Key, slot.MediaKind, slot.Status, slot.Required, slot.GenerationEpoch, slot.ActiveBindingID, strings.Join(slot.CandidateIDs, ",")))
				}
			}
		}
	}
	if active, err := board.ActiveRevision(); err == nil {
		appendRevision("ActiveRevision", active)
	}
	if pending, err := board.PendingRevision(); err == nil {
		appendRevision("PendingRevision", pending)
	}
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
