package server

import (
	"context"
	"log/slog"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/a2ui"
	"github.com/FigoGoo/Dora-Agent/internal/aigc/skill"
)

// listEnabledSkillOptions 取所有 Enabled 的 Skill，解析出名称/描述组成候选。
// 解析失败的记录跳过（用其原始 Name/Description 兜底）。
func (cfg Config) listEnabledSkillOptions(ctx context.Context) []skill.SkillOption {
	records, err := cfg.Skills.ListEnabled(ctx)
	if err != nil {
		slog.Error("skill router: list enabled skills", "error", err)
		return nil
	}
	options := make([]skill.SkillOption, 0, len(records))
	for _, r := range records {
		name, desc := r.Name, r.Description
		if plan, err := skill.ParseSkill(r.Content); err == nil {
			if plan.Name != "" {
				name = plan.Name
			}
			if plan.Description != "" {
				desc = plan.Description
			}
		}
		options = append(options, skill.SkillOption{ID: r.ID, Name: name, Description: desc})
	}
	return options
}

// resolveSkillSelection 拥有 0/1 候选与出错的全部兜底策略。调用前应保证 len(options) > 0。
func (cfg Config) resolveSkillSelection(ctx context.Context, brief string, options []skill.SkillOption) skill.SkillSelection {
	if len(options) == 1 {
		return skill.SkillSelection{SkillID: options[0].ID, Reason: "库中唯一 Skill"}
	}
	sel, err := cfg.SkillSelector.Select(ctx, brief, options)
	if err != nil {
		slog.Warn("skill router: selector error, fallback to default", "error", err)
		fallbackID := cfg.DefaultSkillID
		if fallbackID == "" {
			fallbackID = options[0].ID
		}
		return skill.SkillSelection{SkillID: fallbackID, Reason: "未能匹配，回落默认", Fallback: true}
	}
	return sel
}

// emitSkillSelected 通过 a2ui broker 广播 skill.selected（Publisher 为 nil 时静默跳过）。
func (cfg Config) emitSkillSelected(ctx context.Context, sessionID, runID string, sel skill.SkillSelection, options []skill.SkillOption) {
	if cfg.Publisher == nil {
		return
	}
	name := sel.SkillID
	for _, o := range options {
		if o.ID == sel.SkillID {
			name = o.Name
			break
		}
	}
	event := a2ui.SSEEvent{
		ID:        cfg.NewID(),
		SessionID: sessionID,
		RunID:     runID,
		Event:     a2ui.EventSkillSelected,
		Payload: a2ui.SkillSelectedPayload{
			SkillID:   sel.SkillID,
			SkillName: name,
			Reason:    sel.Reason,
			Fallback:  sel.Fallback,
		},
		CreatedAt: cfg.Now(),
	}
	if err := cfg.Publisher.Publish(ctx, event); err != nil {
		slog.Error("skill router: publish skill.selected", "session_id", sessionID, "skill_id", sel.SkillID, "error", err)
	}
}
