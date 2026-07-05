package skill

import (
	"context"
	"fmt"
	"strings"

	adkskill "github.com/cloudwego/eino/adk/middlewares/skill"
)

type RecordSource interface {
	ListEnabled(ctx context.Context) ([]SkillRecord, error)
	GetEnabledByName(ctx context.Context, name string) (SkillRecord, error)
}

type EinoBackend struct {
	source RecordSource
}

func NewEinoBackend(source RecordSource) *EinoBackend {
	return &EinoBackend{source: source}
}

func (b *EinoBackend) List(ctx context.Context) ([]adkskill.FrontMatter, error) {
	if b == nil || b.source == nil {
		return nil, fmt.Errorf("skill record source is required")
	}
	records, err := b.source.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}

	matters := make([]adkskill.FrontMatter, 0, len(records))
	for _, record := range records {
		matter, err := recordFrontMatter(record)
		if err != nil {
			return nil, err
		}
		matters = append(matters, matter)
	}
	return matters, nil
}

func (b *EinoBackend) Get(ctx context.Context, name string) (adkskill.Skill, error) {
	if b == nil || b.source == nil {
		return adkskill.Skill{}, fmt.Errorf("skill record source is required")
	}
	record, err := b.source.GetEnabledByName(ctx, name)
	if err != nil {
		return adkskill.Skill{}, err
	}
	matter, err := recordFrontMatter(record)
	if err != nil {
		return adkskill.Skill{}, err
	}
	return adkskill.Skill{
		FrontMatter:   matter,
		Content:       strings.TrimSpace(record.Content),
		BaseDirectory: "db://aigc_skills/" + record.ID,
	}, nil
}

func recordFrontMatter(record SkillRecord) (adkskill.FrontMatter, error) {
	name := strings.TrimSpace(record.Name)
	description := strings.TrimSpace(record.Description)
	if name == "" || description == "" {
		plan, err := ParseSkill(record.Content)
		if err != nil {
			return adkskill.FrontMatter{}, fmt.Errorf("parse skill %s: %w", record.ID, err)
		}
		if name == "" {
			name = plan.Name
		}
		if description == "" {
			description = plan.Description
		}
	}
	return adkskill.FrontMatter{
		Name:        name,
		Description: description,
	}, nil
}
