package capability

import (
	"fmt"
	"slices"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/aigc/storyboard"
)

const (
	AnalyzeMaterialsToolKey = "analyze_materials"
	PlanCreationSpecToolKey = "plan_creation_spec"
	PlanStoryboardToolKey   = "plan_storyboard"
	GenerateMediaToolKey    = "generate_media"
	AssembleOutputToolKey   = "assemble_output"
)

var AgentToolKeys = []string{
	AnalyzeMaterialsToolKey,
	PlanCreationSpecToolKey,
	PlanStoryboardToolKey,
	GenerateMediaToolKey,
	AssembleOutputToolKey,
}

type AnalyzeMaterialsIntent struct {
	AssetIDs    []string `json:"asset_ids"`
	Goal        string   `json:"goal"`
	Instruction string   `json:"instruction,omitempty"`
}

type AnalyzeMaterialsData struct {
	AnalysisID       string   `json:"analysis_id"`
	AnalysisVersion  int      `json:"analysis_version"`
	Summary          string   `json:"summary"`
	ReusableAssetIDs []string `json:"reusable_asset_ids,omitempty"`
	MissingInputs    []string `json:"missing_inputs,omitempty"`
}

type PlanCreationSpecIntent struct {
	Mode        string `json:"mode"`
	Background  string `json:"background,omitempty"`
	Goal        string `json:"goal,omitempty"`
	Instruction string `json:"instruction,omitempty"`
}

type PlanCreationSpecData struct {
	SpecID                   string         `json:"spec_id"`
	SpecVersion              int            `json:"spec_version"`
	Status                   string         `json:"status"`
	Candidate                map[string]any `json:"candidate,omitempty"`
	ApprovalID               string         `json:"approval_id,omitempty"`
	StoryboardReplanRequired bool           `json:"storyboard_replan_required"`
}

type PlanStoryboardIntent struct {
	Mode                   string `json:"mode"`
	Instruction            string `json:"instruction,omitempty"`
	PreserveApprovedAssets bool   `json:"preserve_approved_assets,omitempty"`
}

type PlanStoryboardData struct {
	Revision   storyboard.StoryboardRevision `json:"revision"`
	Diff       storyboard.RevisionDiff       `json:"diff"`
	ApprovalID string                        `json:"approval_id,omitempty"`
}

const (
	MediaTargetStoryboard         = "storyboard"
	MediaTargetSessionDeliverable = "session_deliverable"
)

type GenerateMediaIntent struct {
	Phase  string `json:"phase,omitempty"`
	Policy string `json:"policy,omitempty"`
	// 轻直出（session_deliverable 目标）字段；target 为空 = storyboard，
	// 行为与旧版完全一致。
	Target      string `json:"target,omitempty"`
	MediaKind   string `json:"media_kind,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	Count       int    `json:"count,omitempty"`
	AspectRatio string `json:"aspect_ratio,omitempty"`
}

func (intent GenerateMediaIntent) NormalizedTarget() string {
	target := strings.TrimSpace(intent.Target)
	if target == "" {
		return MediaTargetStoryboard
	}
	return target
}

func (intent GenerateMediaIntent) NormalizedCount() int {
	if intent.Count == 0 {
		return 1
	}
	return intent.Count
}

type GenerateMediaData struct {
	SelectedTargets []string `json:"selected_targets,omitempty"`
	JobCount        int      `json:"job_count"`
	NoOp            bool     `json:"no_op,omitempty"`
	Reason          string   `json:"reason,omitempty"`
}

type AssembleOutputIntent struct {
	Mode        string `json:"mode"`
	OutputType  string `json:"output_type,omitempty"`
	Instruction string `json:"instruction,omitempty"`
}

type AssembleOutputData struct {
	AssemblyRevisionID  string         `json:"assembly_revision_id,omitempty"`
	Manifest            map[string]any `json:"manifest,omitempty"`
	MissingDependencies []string       `json:"missing_dependencies,omitempty"`
}

func (intent AnalyzeMaterialsIntent) Validate() error {
	if len(compactStrings(intent.AssetIDs)) == 0 && strings.TrimSpace(intent.Goal) == "" {
		return fmt.Errorf("analyze_materials requires asset_ids or goal")
	}
	return nil
}

func (intent PlanCreationSpecIntent) Validate() error {
	if !slices.Contains([]string{"create", "revise"}, strings.TrimSpace(intent.Mode)) {
		return fmt.Errorf("plan_creation_spec mode must be create or revise")
	}
	if strings.TrimSpace(intent.Background) == "" && strings.TrimSpace(intent.Goal) == "" && strings.TrimSpace(intent.Instruction) == "" {
		return fmt.Errorf("plan_creation_spec requires background, goal, or instruction")
	}
	return nil
}

func (intent PlanStoryboardIntent) Validate() error {
	if !slices.Contains([]string{"create", "replan"}, strings.TrimSpace(intent.Mode)) {
		return fmt.Errorf("plan_storyboard mode must be create or replan")
	}
	return nil
}

func (intent GenerateMediaIntent) Validate() error {
	switch intent.NormalizedTarget() {
	case MediaTargetStoryboard:
		if !slices.Contains([]string{"auto_next", "element_images", "keyframes", "videos", "audio"}, strings.TrimSpace(intent.Phase)) {
			return fmt.Errorf("generate_media phase is invalid")
		}
		if !slices.Contains([]string{"single_next", "all_eligible"}, strings.TrimSpace(intent.Policy)) {
			return fmt.Errorf("generate_media policy is invalid")
		}
		return nil
	case MediaTargetSessionDeliverable:
		if !slices.Contains([]string{"image", "video", "music", "audio"}, strings.TrimSpace(intent.MediaKind)) {
			return fmt.Errorf("generate_media media_kind must be image|video|music|audio for session_deliverable")
		}
		if strings.TrimSpace(intent.Prompt) == "" {
			return fmt.Errorf("generate_media prompt is required for session_deliverable")
		}
		if count := intent.NormalizedCount(); count < 1 || count > 4 {
			return fmt.Errorf("generate_media count must be between 1 and 4")
		}
		return nil
	default:
		return fmt.Errorf("generate_media target %q is not supported", intent.Target)
	}
}

func (intent AssembleOutputIntent) Validate() error {
	if !slices.Contains([]string{"validate", "plan", "preview", "export"}, strings.TrimSpace(intent.Mode)) {
		return fmt.Errorf("assemble_output mode is invalid")
	}
	return nil
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
