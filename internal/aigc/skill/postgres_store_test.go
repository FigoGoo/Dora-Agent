package skill

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
	aigcstorage "github.com/FigoGoo/Dora-Agent/internal/aigc/storage"
)

func TestPostgresSkillStorePersistsAndParsesSkill(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := aigcstorage.OpenAgentPostgres(ctx, aigcconfig.LoadFromEnv())
	if err != nil {
		t.Skipf("local postgres is not available: %v", err)
	}

	store := NewPostgresSkillStore(db)
	if err := store.AutoMigrate(ctx); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	skillID := fmt.Sprintf("skill-%d", time.Now().UnixNano())
	content := `<name>武侠短片</name>
<description>生成故事驱动型武侠短片</description>
<planner>
1. 编写 Final_Video_Spec.md -> **text_editor**
   pause_after: true
2. 生成故事板 -> **storyboard_designer**
   depends_on: [1]
   pause_after: true
</planner>`

	if err := store.Save(ctx, SkillRecord{
		ID:      skillID,
		Name:    "武侠短片",
		Version: "v1",
		Content: content,
		Enabled: true,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	plan, err := store.LoadPlan(ctx, skillID)
	if err != nil {
		t.Fatalf("LoadPlan() error = %v", err)
	}
	if plan.SkillID != skillID || plan.Name != "武侠短片" {
		t.Fatalf("unexpected plan metadata: %#v", plan)
	}
	if len(plan.Stages) != 2 || plan.Stages[0].ToolKeys[0] != "text_editor" || plan.Stages[1].DependsOn[0] != "1" {
		t.Fatalf("unexpected parsed stages: %#v", plan.Stages)
	}

	if _, err := store.Get(ctx, fmt.Sprintf("missing-%d", time.Now().UnixNano())); !errors.Is(err, ErrSkillNotFound) {
		t.Fatalf("Get(missing) error = %v, want ErrSkillNotFound", err)
	}
}
