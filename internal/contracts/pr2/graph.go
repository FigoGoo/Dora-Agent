package pr2

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
)

const (
	SchemaVersionGenericCreationGraph = "generic_creation_graph.v1"
	SchemaVersionGraphTemplate        = "graph_template.v1"
	SchemaVersionGraphPlan            = "graph_plan.v1"
	SchemaVersionGraphCheckpoint      = "graph_checkpoint.v1"
)

const (
	GenericCreationGraphID = "generic_creation_graph"
	SkillLevelL0           = "L0"
	SkillLevelL1           = "L1"
	SkillLevelL2           = "L2"
	SkillLevelL3           = "L3"
)

const (
	GraphTypeGenericCreation  = "generic_creation"
	GraphTypeSystemSkill      = "system_skill"
	GraphTypeMarketplaceSkill = "marketplace_skill"
)

const (
	GraphNodeTypeBriefParser    = "brief_parser"
	GraphNodeTypeClarifier      = "clarifier"
	GraphNodeTypeLLM            = "llm"
	GraphNodeTypeBoardWriter    = "board_writer"
	GraphNodeTypeRouter         = "router"
	GraphNodeTypeGate           = "gate"
	GraphNodeTypeRecommendation = "recommendation"
	GraphNodeTypeSummarizer     = "summarizer"
	GraphNodeTypeInterrupt      = "interrupt"
)

const (
	GraphPlanNodeStatusPending             = "pending"
	GraphPlanNodeStatusRunning             = "running"
	GraphPlanNodeStatusWaitingInput        = "waiting_input"
	GraphPlanNodeStatusWaitingConfirmation = "waiting_confirmation"
	GraphPlanNodeStatusCompleted           = "completed"
	GraphPlanNodeStatusFailed              = "failed"
	GraphPlanNodeStatusSkipped             = "skipped"
)

const (
	ValueDeliveredStageBoardReady      = "board_ready"
	ValueDeliveredStageStoryboardReady = "storyboard_ready"
	ValueDeliveredStageAssetReady      = "asset_ready"
)

const (
	CheckpointTypeInterrupt     = "interrupt"
	CheckpointTypeResume        = "resume"
	CheckpointTypeNodeCompleted = "node_completed"
	CheckpointTypeErrorBoundary = "error_boundary"
)

const (
	CheckpointStatusOpen    = "open"
	CheckpointStatusResumed = "resumed"
	CheckpointStatusExpired = "expired"
	CheckpointStatusClosed  = "closed"
)

type GenericCreationGraph struct {
	SchemaVersion        string   `json:"schema_version"`
	GenericGraphID       string   `json:"generic_graph_id"`
	SkillLevel           string   `json:"skill_level"`
	MarketplaceListingID *string  `json:"marketplace_listing_id"`
	PricingPolicy        string   `json:"pricing_policy"`
	UsageFee             int      `json:"usage_fee"`
	VersionStrategy      string   `json:"version_strategy"`
	DefaultNodes         []string `json:"default_nodes"`
	AllowedOutputs       []string `json:"allowed_outputs"`
	GraphTemplateDigest  string   `json:"graph_template_digest"`
}

type GraphTemplate struct {
	SchemaVersion   string      `json:"schema_version"`
	GraphTemplateID string      `json:"graph_template_id"`
	Name            string      `json:"name"`
	Version         string      `json:"version"`
	GraphType       string      `json:"graph_type"`
	SkillLevel      string      `json:"skill_level"`
	EntryNode       string      `json:"entry_node"`
	TerminalNodes   []string    `json:"terminal_nodes"`
	Nodes           []GraphNode `json:"nodes"`
	Edges           []GraphEdge `json:"edges"`
	TemplateDigest  string      `json:"template_digest"`
	CreatedAt       time.Time   `json:"created_at"`
}

type GraphNode struct {
	NodeID       string  `json:"node_id"`
	NodeType     string  `json:"node_type"`
	DisplayName  string  `json:"display_name"`
	ConfigDigest *string `json:"config_digest,omitempty"`
}

type GraphEdge struct {
	From      string  `json:"from"`
	To        string  `json:"to"`
	Condition *string `json:"condition"`
}

type GraphPlan struct {
	SchemaVersion        string          `json:"schema_version"`
	GraphPlanID          string          `json:"graph_plan_id"`
	GraphTemplateID      string          `json:"graph_template_id"`
	GraphTemplateVersion string          `json:"graph_template_version"`
	RunID                string          `json:"run_id"`
	BoardID              string          `json:"board_id"`
	Status               string          `json:"status"`
	CurrentNode          *string         `json:"current_node"`
	ValueDeliveredStage  string          `json:"value_delivered_stage"`
	Nodes                []GraphPlanNode `json:"nodes"`
	Edges                []GraphPlanEdge `json:"edges"`
	GraphPlanDigest      string          `json:"graph_plan_digest"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type GraphPlanNode struct {
	NodeID       string  `json:"node_id"`
	NodeType     string  `json:"node_type"`
	Status       string  `json:"status"`
	InputDigest  *string `json:"input_digest,omitempty"`
	OutputDigest *string `json:"output_digest,omitempty"`
}

type GraphPlanEdge struct {
	From            string  `json:"from"`
	To              string  `json:"to"`
	ConditionResult *string `json:"condition_result"`
}

type GraphCheckpoint struct {
	SchemaVersion  string     `json:"schema_version"`
	CheckpointID   string     `json:"checkpoint_id"`
	GraphPlanID    string     `json:"graph_plan_id"`
	RunID          string     `json:"run_id"`
	NodeID         string     `json:"node_id"`
	CheckpointType string     `json:"checkpoint_type"`
	Status         string     `json:"status"`
	StateDigest    string     `json:"state_digest"`
	Resumable      bool       `json:"resumable"`
	ExpiresAt      *time.Time `json:"expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

func ValidateGenericCreationGraph(graph GenericCreationGraph) error {
	if graph.SchemaVersion != SchemaVersionGenericCreationGraph {
		return fmt.Errorf("schema_version must be %s", SchemaVersionGenericCreationGraph)
	}
	if graph.GenericGraphID != GenericCreationGraphID {
		return fmt.Errorf("generic_graph_id must be %s", GenericCreationGraphID)
	}
	if graph.SkillLevel != SkillLevelL0 {
		return errors.New("generic graph must be L0")
	}
	if graph.MarketplaceListingID != nil {
		return errors.New("generic graph must not have marketplace_listing_id")
	}
	if graph.PricingPolicy != "free" || graph.UsageFee != 0 {
		return errors.New("generic graph must be free")
	}
	if graph.VersionStrategy != "platform_builtin" {
		return errors.New("generic graph version_strategy must be platform_builtin")
	}
	if len(graph.DefaultNodes) < 4 {
		return errors.New("generic graph default_nodes must contain at least 4 nodes")
	}
	for _, required := range []string{
		GraphNodeTypeBriefParser,
		GraphNodeTypeClarifier,
		"creative_direction",
		GraphNodeTypeBoardWriter,
		"skill_recommendation",
	} {
		if !contains(graph.DefaultNodes, required) {
			return fmt.Errorf("generic graph default_nodes missing %q", required)
		}
	}
	if len(graph.AllowedOutputs) == 0 {
		return errors.New("generic graph allowed_outputs are required")
	}
	for _, output := range graph.AllowedOutputs {
		if !isAllowed(output, []string{
			"brief_summary",
			"clarifying_questions",
			"creative_direction",
			"prompt_draft",
			"storyboard",
			"skill_recommendations",
		}) {
			return fmt.Errorf("invalid generic graph output %q", output)
		}
	}
	if err := pr1.ValidateDigest(graph.GraphTemplateDigest); err != nil {
		return fmt.Errorf("graph_template_digest: %w", err)
	}
	return nil
}

func ValidateGraphTemplate(template GraphTemplate) error {
	if template.SchemaVersion != SchemaVersionGraphTemplate {
		return fmt.Errorf("schema_version must be %s", SchemaVersionGraphTemplate)
	}
	if err := validatePrefixID(template.GraphTemplateID, "gtemplate_"); err != nil {
		return fmt.Errorf("graph_template_id: %w", err)
	}
	if strings.TrimSpace(template.Name) == "" {
		return errors.New("name is required")
	}
	if !strings.HasPrefix(template.Version, "v") {
		return fmt.Errorf("invalid version %q", template.Version)
	}
	if !isAllowed(template.GraphType, []string{GraphTypeGenericCreation, GraphTypeSystemSkill, GraphTypeMarketplaceSkill}) {
		return fmt.Errorf("invalid graph_type %q", template.GraphType)
	}
	if !isAllowed(template.SkillLevel, []string{SkillLevelL0, SkillLevelL1, SkillLevelL2, SkillLevelL3}) {
		return fmt.Errorf("invalid skill_level %q", template.SkillLevel)
	}
	if len(template.Nodes) == 0 || len(template.TerminalNodes) == 0 {
		return errors.New("nodes and terminal_nodes are required")
	}
	nodeSet := make(map[string]struct{}, len(template.Nodes))
	for index, node := range template.Nodes {
		if strings.TrimSpace(node.NodeID) == "" {
			return fmt.Errorf("node %d node_id is required", index+1)
		}
		if !isAllowed(node.NodeType, []string{
			GraphNodeTypeBriefParser,
			GraphNodeTypeClarifier,
			GraphNodeTypeLLM,
			GraphNodeTypeBoardWriter,
			GraphNodeTypeRouter,
			GraphNodeTypeGate,
			GraphNodeTypeRecommendation,
			GraphNodeTypeSummarizer,
			GraphNodeTypeInterrupt,
		}) {
			return fmt.Errorf("node %d invalid node_type %q", index+1, node.NodeType)
		}
		if strings.TrimSpace(node.DisplayName) == "" {
			return fmt.Errorf("node %d display_name is required", index+1)
		}
		if node.ConfigDigest != nil {
			if err := pr1.ValidateDigest(*node.ConfigDigest); err != nil {
				return fmt.Errorf("node %d config_digest: %w", index+1, err)
			}
		}
		nodeSet[node.NodeID] = struct{}{}
	}
	if _, ok := nodeSet[template.EntryNode]; !ok {
		return fmt.Errorf("entry_node %q is not defined", template.EntryNode)
	}
	for _, terminalNode := range template.TerminalNodes {
		if strings.TrimSpace(terminalNode) == "" {
			return errors.New("terminal_nodes cannot contain empty values")
		}
	}
	for index, edge := range template.Edges {
		if _, ok := nodeSet[edge.From]; !ok {
			return fmt.Errorf("edge %d from node %q is not defined", index+1, edge.From)
		}
		if _, ok := nodeSet[edge.To]; !ok {
			return fmt.Errorf("edge %d to node %q is not defined", index+1, edge.To)
		}
	}
	if err := pr1.ValidateDigest(template.TemplateDigest); err != nil {
		return fmt.Errorf("template_digest: %w", err)
	}
	if template.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	return nil
}

func ValidateGraphPlan(plan GraphPlan) error {
	if plan.SchemaVersion != SchemaVersionGraphPlan {
		return fmt.Errorf("schema_version must be %s", SchemaVersionGraphPlan)
	}
	if err := validatePrefixID(plan.GraphPlanID, "gplan_"); err != nil {
		return fmt.Errorf("graph_plan_id: %w", err)
	}
	if strings.TrimSpace(plan.GraphTemplateID) == "" || strings.TrimSpace(plan.GraphTemplateVersion) == "" || strings.TrimSpace(plan.RunID) == "" {
		return errors.New("graph_template_id, graph_template_version and run_id are required")
	}
	if err := validatePrefixID(plan.BoardID, "board_"); err != nil {
		return fmt.Errorf("board_id: %w", err)
	}
	if !pr1.IsValidState(pr1.StateGraphPlanStatus, plan.Status) {
		return fmt.Errorf("invalid graph plan status %q", plan.Status)
	}
	if !isAllowed(plan.ValueDeliveredStage, []string{
		ValueDeliveredStageBoardReady,
		ValueDeliveredStageStoryboardReady,
		ValueDeliveredStageAssetReady,
	}) {
		return fmt.Errorf("invalid value_delivered_stage %q", plan.ValueDeliveredStage)
	}
	if len(plan.Nodes) == 0 {
		return errors.New("nodes are required")
	}
	nodeSet := make(map[string]struct{}, len(plan.Nodes))
	for index, node := range plan.Nodes {
		if strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.NodeType) == "" {
			return fmt.Errorf("node %d node_id and node_type are required", index+1)
		}
		if !isAllowed(node.Status, []string{
			GraphPlanNodeStatusPending,
			GraphPlanNodeStatusRunning,
			GraphPlanNodeStatusWaitingInput,
			GraphPlanNodeStatusWaitingConfirmation,
			GraphPlanNodeStatusCompleted,
			GraphPlanNodeStatusFailed,
			GraphPlanNodeStatusSkipped,
		}) {
			return fmt.Errorf("node %d invalid status %q", index+1, node.Status)
		}
		if node.InputDigest != nil {
			if err := pr1.ValidateDigest(*node.InputDigest); err != nil {
				return fmt.Errorf("node %d input_digest: %w", index+1, err)
			}
		}
		if node.OutputDigest != nil {
			if err := pr1.ValidateDigest(*node.OutputDigest); err != nil {
				return fmt.Errorf("node %d output_digest: %w", index+1, err)
			}
		}
		nodeSet[node.NodeID] = struct{}{}
	}
	if plan.CurrentNode != nil {
		if _, ok := nodeSet[*plan.CurrentNode]; !ok {
			return fmt.Errorf("current_node %q is not defined", *plan.CurrentNode)
		}
	}
	for index, edge := range plan.Edges {
		if _, ok := nodeSet[edge.From]; !ok {
			return fmt.Errorf("edge %d from node %q is not defined", index+1, edge.From)
		}
		if _, ok := nodeSet[edge.To]; !ok {
			return fmt.Errorf("edge %d to node %q is not defined", index+1, edge.To)
		}
	}
	if err := pr1.ValidateDigest(plan.GraphPlanDigest); err != nil {
		return fmt.Errorf("graph_plan_digest: %w", err)
	}
	if plan.CreatedAt.IsZero() || plan.UpdatedAt.IsZero() {
		return errors.New("created_at and updated_at are required")
	}
	if plan.UpdatedAt.Before(plan.CreatedAt) {
		return errors.New("updated_at must not be before created_at")
	}
	return nil
}

func ValidateGraphCheckpoint(checkpoint GraphCheckpoint) error {
	if checkpoint.SchemaVersion != SchemaVersionGraphCheckpoint {
		return fmt.Errorf("schema_version must be %s", SchemaVersionGraphCheckpoint)
	}
	if err := validatePrefixID(checkpoint.CheckpointID, "gchk_"); err != nil {
		return fmt.Errorf("checkpoint_id: %w", err)
	}
	if err := validatePrefixID(checkpoint.GraphPlanID, "gplan_"); err != nil {
		return fmt.Errorf("graph_plan_id: %w", err)
	}
	if strings.TrimSpace(checkpoint.RunID) == "" || strings.TrimSpace(checkpoint.NodeID) == "" {
		return errors.New("run_id and node_id are required")
	}
	if !isAllowed(checkpoint.CheckpointType, []string{
		CheckpointTypeInterrupt,
		CheckpointTypeResume,
		CheckpointTypeNodeCompleted,
		CheckpointTypeErrorBoundary,
	}) {
		return fmt.Errorf("invalid checkpoint_type %q", checkpoint.CheckpointType)
	}
	if !isAllowed(checkpoint.Status, []string{
		CheckpointStatusOpen,
		CheckpointStatusResumed,
		CheckpointStatusExpired,
		CheckpointStatusClosed,
	}) {
		return fmt.Errorf("invalid checkpoint status %q", checkpoint.Status)
	}
	if err := pr1.ValidateDigest(checkpoint.StateDigest); err != nil {
		return fmt.Errorf("state_digest: %w", err)
	}
	if checkpoint.Status == CheckpointStatusOpen && checkpoint.Resumable && checkpoint.ExpiresAt == nil {
		return errors.New("open resumable checkpoint requires expires_at")
	}
	if checkpoint.CreatedAt.IsZero() {
		return errors.New("created_at is required")
	}
	return nil
}

func ValidateGenericGraphFixture(generic GenericCreationGraph, template GraphTemplate, plan GraphPlan) error {
	if err := ValidateGenericCreationGraph(generic); err != nil {
		return fmt.Errorf("generic_creation_graph: %w", err)
	}
	if err := ValidateGraphTemplate(template); err != nil {
		return fmt.Errorf("graph_template: %w", err)
	}
	if err := ValidateGraphPlan(plan); err != nil {
		return fmt.Errorf("graph_plan: %w", err)
	}
	if template.GraphType != GraphTypeGenericCreation || template.SkillLevel != SkillLevelL0 {
		return errors.New("generic graph fixture template must be generic_creation L0")
	}
	if plan.GraphTemplateID != template.GraphTemplateID || plan.GraphTemplateVersion != template.Version {
		return errors.New("graph plan must reference template id and version")
	}
	if plan.ValueDeliveredStage != ValueDeliveredStageStoryboardReady {
		return errors.New("generic graph fixture must deliver storyboard_ready")
	}
	return nil
}

func ResumeCheckpoint(checkpoint GraphCheckpoint) (GraphCheckpoint, error) {
	if err := ValidateGraphCheckpoint(checkpoint); err != nil {
		return GraphCheckpoint{}, err
	}
	if checkpoint.Status != CheckpointStatusOpen || !checkpoint.Resumable {
		return GraphCheckpoint{}, errors.New("checkpoint is not resumable")
	}
	checkpoint.Status = CheckpointStatusResumed
	return checkpoint, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
