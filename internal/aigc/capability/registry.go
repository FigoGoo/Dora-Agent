package capability

import (
	"context"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"

	aigctools "github.com/FigoGoo/Dora-Agent/internal/aigc/tools"
)

// NewAgentRegistry returns the target Agent-facing registry. It intentionally
// contains exactly five high-level capabilities: no prompt preparation,
// provider, billing, entitlement, asset CRUD, or other business-system tools.
func NewAgentRegistry(set ToolSet) (*aigctools.Registry, error) {
	registry := aigctools.NewRegistry()
	if err := RegisterAgentTools(registry, set); err != nil {
		return nil, err
	}
	return registry, nil
}

func RegisterAgentTools(registry *aigctools.Registry, set ToolSet) error {
	if registry == nil {
		return fmt.Errorf("tool registry is required")
	}
	entries := []struct {
		key  string
		tool einotool.BaseTool
		meta aigctools.ToolMeta
	}{
		{
			key: AnalyzeMaterialsToolKey, tool: set.AnalyzeMaterials,
			meta: aigctools.ToolMeta{Category: "capability", StageHints: []string{"material_analysis"}, OutputKinds: []string{"material_analysis_revision", "artifact_ref"}, Provider: "eino_graph"},
		},
		{
			key: PlanCreationSpecToolKey, tool: set.PlanCreationSpec,
			meta: aigctools.ToolMeta{Category: "capability", StageHints: []string{"creation_spec", "spec_review"}, OutputKinds: []string{"creation_spec_revision", "approval"}, Provider: "eino_graph"},
		},
		{
			key: PlanStoryboardToolKey, tool: set.PlanStoryboard,
			meta: aigctools.ToolMeta{Category: "capability", StageHints: []string{"storyboard_planning", "storyboard_review"}, OutputKinds: []string{"storyboard_revision", "approval"}, Provider: "eino_graph"},
		},
		{
			key: GenerateMediaToolKey, tool: set.GenerateMedia,
			meta: aigctools.ToolMeta{Category: "capability", StageHints: []string{"media_generation"}, OutputKinds: []string{"operation", "batch", "job_plan"}, Provider: "eino_graph"},
		},
		{
			key: AssembleOutputToolKey, tool: set.AssembleOutput,
			meta: aigctools.ToolMeta{Category: "capability", StageHints: []string{"assembly", "export"}, OutputKinds: []string{"assembly_revision", "operation"}, Provider: "eino_graph"},
		},
	}
	for _, entry := range entries {
		if entry.tool == nil {
			return fmt.Errorf("%s capability tool is required", entry.key)
		}
		info, err := entry.tool.Info(context.Background())
		if err != nil {
			return fmt.Errorf("load %s capability info: %w", entry.key, err)
		}
		if info == nil || info.Name != entry.key {
			return fmt.Errorf("capability key/name mismatch: key=%s", entry.key)
		}
	}
	for _, entry := range entries {
		if err := registry.Register(entry.key, entry.tool, entry.meta); err != nil {
			return err
		}
	}
	return nil
}
