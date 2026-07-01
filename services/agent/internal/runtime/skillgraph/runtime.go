package skillgraph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr2"
)

type Clock func() time.Time

type Runtime struct {
	clock Clock
}

type Input struct {
	RunID                string
	ProjectID            string
	SessionID            string
	SpaceID              string
	ActorUserID          string
	TraceID              string
	Prompt               string
	SkillID              string
	SkillVersion         string
	SkillSource          string
	SkillSpecJSON        string
	OutputElements       []OutputElement
	RouterDecisionDigest string
}

type OutputElement struct {
	ElementType  string
	ElementName  string
	Required     bool
	UseDraft     bool
	UseFinal     bool
	Editable     bool
	Referable    bool
	DisplayOrder int32
	DisplaySlot  string
	SchemaJSON   string
}

type Result struct {
	SkillSpecDigest string
	GraphTemplate   pr2.GraphTemplate
	GraphPlan       pr2.GraphPlan
	Board           pr2.CreativeBoard
	Elements        []pr2.CreativeElement
	Snapshot        pr2.BoardSnapshot
	Events          []pr1.AGUIEnvelope
}

type runtimeSpec struct {
	SchemaVersion string         `json:"schema_version"`
	SkillID       string         `json:"skill_id"`
	Version       string         `json:"version"`
	Status        string         `json:"status"`
	Level         string         `json:"level"`
	Scope         string         `json:"scope"`
	Stages        []string       `json:"stages"`
	GraphTemplate graphSpec      `json:"graph_template"`
	Raw           map[string]any `json:"-"`
}

type graphSpec struct {
	EntryNode     string     `json:"entry_node"`
	TerminalNodes []string   `json:"terminal_nodes"`
	Nodes         []nodeSpec `json:"nodes"`
	Edges         []edgeSpec `json:"edges"`
}

type nodeSpec struct {
	NodeKey     string         `json:"node_key"`
	NodeType    string         `json:"node_type"`
	DisplayName string         `json:"display_name"`
	Config      map[string]any `json:"config,omitempty"`
}

type edgeSpec struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func New(clock Clock) Runtime {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return Runtime{clock: clock}
}

func (r Runtime) Execute(ctx context.Context, input Input) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if err := validateInput(input); err != nil {
		return Result{}, err
	}
	spec, err := parseRuntimeSpec(input)
	if err != nil {
		return Result{}, err
	}
	specDigest, err := pr1.CanonicalDigest(spec.Raw)
	if err != nil {
		return Result{}, err
	}
	now := r.clock().UTC()
	template, err := buildTemplate(input, spec, specDigest, now)
	if err != nil {
		return Result{}, err
	}
	boardID := "board_" + shortDigest(input.RunID+":skill:"+input.SkillID+":board")
	plan, err := buildPlan(input, spec, template, specDigest, boardID, now)
	if err != nil {
		return Result{}, err
	}
	elements, err := buildElements(input, boardID, now)
	if err != nil {
		return Result{}, err
	}
	graphPlanID := plan.GraphPlanID
	board := pr2.CreativeBoard{
		SchemaVersion:   pr2.SchemaVersionCreativeBoard,
		BoardID:         boardID,
		ProjectID:       input.ProjectID,
		SessionID:       input.SessionID,
		RunID:           input.RunID,
		GraphPlanID:     &graphPlanID,
		Title:           boardTitle(input),
		Status:          "ready",
		Version:         1,
		ElementsCount:   len(elements),
		ToolPlanAllowed: false,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	board.BoardDigest, err = pr1.CanonicalDigest(map[string]any{
		"board_id":          board.BoardID,
		"version":           board.Version,
		"status":            board.Status,
		"skill_id":          input.SkillID,
		"skill_version":     input.SkillVersion,
		"skill_spec_digest": specDigest,
		"elements":          elements,
	})
	if err != nil {
		return Result{}, err
	}
	snapshot := pr2.BoardSnapshot{
		SchemaVersion: pr2.SchemaVersionBoardSnapshot,
		BoardID:       board.BoardID,
		Version:       board.Version,
		Status:        board.Status,
		Elements:      elements,
		BoardDigest:   board.BoardDigest,
		CreatedAt:     now,
	}
	events, err := buildEvents(input, plan, board, elements, now)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		SkillSpecDigest: specDigest,
		GraphTemplate:   template,
		GraphPlan:       plan,
		Board:           board,
		Elements:        elements,
		Snapshot:        snapshot,
		Events:          events,
	}
	if err := validateResult(result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func validateInput(input Input) error {
	if strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.SessionID) == "" {
		return errors.New("run_id, project_id and session_id are required")
	}
	if strings.TrimSpace(input.SkillID) == "" || strings.TrimSpace(input.SkillVersion) == "" || strings.TrimSpace(input.SkillSpecJSON) == "" {
		return errors.New("skill_id, skill_version and skill_spec_json are required")
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return errors.New("prompt is required")
	}
	if strings.TrimSpace(input.RouterDecisionDigest) != "" {
		if err := pr1.ValidateDigest(input.RouterDecisionDigest); err != nil {
			return fmt.Errorf("router_decision_digest: %w", err)
		}
	}
	return nil
}

func parseRuntimeSpec(input Input) (runtimeSpec, error) {
	raw := map[string]any{}
	if err := json.Unmarshal([]byte(input.SkillSpecJSON), &raw); err != nil {
		return runtimeSpec{}, fmt.Errorf("skill spec json: %w", err)
	}
	var spec runtimeSpec
	if err := json.Unmarshal([]byte(input.SkillSpecJSON), &spec); err != nil {
		return runtimeSpec{}, fmt.Errorf("skill spec: %w", err)
	}
	spec.Raw = raw
	if spec.SchemaVersion != "skill_runtime_spec.v1" {
		return runtimeSpec{}, errors.New("skill runtime spec schema_version must be skill_runtime_spec.v1")
	}
	if spec.SkillID != input.SkillID || spec.Version != input.SkillVersion {
		return runtimeSpec{}, errors.New("skill runtime spec identity does not match selected skill")
	}
	if spec.Status != "published" {
		return runtimeSpec{}, errors.New("only published skill runtime spec can run")
	}
	if !isAllowed(spec.Level, []string{pr2.SkillLevelL0, pr2.SkillLevelL1, pr2.SkillLevelL2, pr2.SkillLevelL3}) {
		return runtimeSpec{}, fmt.Errorf("invalid skill level %q", spec.Level)
	}
	if strings.TrimSpace(spec.Scope) == "" {
		return runtimeSpec{}, errors.New("scope is required")
	}
	if len(spec.GraphTemplate.Nodes) == 0 || strings.TrimSpace(spec.GraphTemplate.EntryNode) == "" {
		return runtimeSpec{}, errors.New("graph_template entry_node and nodes are required")
	}
	if err := validateGraphSpec(spec.GraphTemplate); err != nil {
		return runtimeSpec{}, err
	}
	return spec, nil
}

func validateGraphSpec(graph graphSpec) error {
	declared := make(map[string]struct{}, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodeKey := strings.TrimSpace(node.NodeKey)
		if nodeKey == "" {
			return errors.New("graph_template node_key is required")
		}
		if _, exists := declared[nodeKey]; exists {
			return fmt.Errorf("graph_template duplicate node_key %q", nodeKey)
		}
		declared[nodeKey] = struct{}{}
	}
	if _, exists := declared[graph.EntryNode]; !exists {
		return fmt.Errorf("graph_template entry_node %q is not declared", graph.EntryNode)
	}
	for _, terminal := range graph.TerminalNodes {
		if _, exists := declared[terminal]; !exists {
			return fmt.Errorf("graph_template terminal_node %q is not declared", terminal)
		}
	}
	for _, edge := range graph.Edges {
		if _, exists := declared[edge.From]; !exists {
			return fmt.Errorf("graph_template edge from %q is not declared", edge.From)
		}
		if _, exists := declared[edge.To]; !exists {
			return fmt.Errorf("graph_template edge to %q is not declared", edge.To)
		}
	}
	return nil
}

func buildTemplate(input Input, spec runtimeSpec, specDigest string, now time.Time) (pr2.GraphTemplate, error) {
	nodes := make([]pr2.GraphNode, 0, len(spec.GraphTemplate.Nodes))
	for _, node := range spec.GraphTemplate.Nodes {
		mappedType, err := mapNodeType(node.NodeType)
		if err != nil {
			return pr2.GraphTemplate{}, err
		}
		displayName := strings.TrimSpace(node.DisplayName)
		if displayName == "" {
			displayName = node.NodeKey
		}
		configDigest := specDigest
		if len(node.Config) > 0 {
			var err error
			configDigest, err = pr1.CanonicalDigest(node.Config)
			if err != nil {
				return pr2.GraphTemplate{}, err
			}
		}
		nodes = append(nodes, pr2.GraphNode{
			NodeID:       node.NodeKey,
			NodeType:     mappedType,
			DisplayName:  displayName,
			ConfigDigest: &configDigest,
		})
	}
	edges := make([]pr2.GraphEdge, 0, len(spec.GraphTemplate.Edges))
	for _, edge := range spec.GraphTemplate.Edges {
		edges = append(edges, pr2.GraphEdge{From: edge.From, To: edge.To})
	}
	terminalNodes := append([]string{}, spec.GraphTemplate.TerminalNodes...)
	if len(terminalNodes) == 0 {
		terminalNodes = []string{nodes[len(nodes)-1].NodeID}
	}
	graphType := pr2.GraphTypeSystemSkill
	if strings.TrimSpace(input.SkillSource) == pr1.SkillSourceMarketplace {
		graphType = pr2.GraphTypeMarketplaceSkill
	}
	templateDigest, err := pr1.CanonicalDigest(map[string]any{
		"skill_id":          input.SkillID,
		"skill_version":     input.SkillVersion,
		"skill_spec_digest": specDigest,
		"entry_node":        spec.GraphTemplate.EntryNode,
		"terminal_nodes":    terminalNodes,
		"nodes":             nodes,
		"edges":             edges,
	})
	if err != nil {
		return pr2.GraphTemplate{}, err
	}
	template := pr2.GraphTemplate{
		SchemaVersion:   pr2.SchemaVersionGraphTemplate,
		GraphTemplateID: "gtemplate_" + shortDigest(input.SkillID+":"+input.SkillVersion+":"+specDigest),
		Name:            input.SkillID + " Skill Graph",
		Version:         "v1",
		GraphType:       graphType,
		SkillLevel:      spec.Level,
		EntryNode:       spec.GraphTemplate.EntryNode,
		TerminalNodes:   terminalNodes,
		Nodes:           nodes,
		Edges:           edges,
		TemplateDigest:  templateDigest,
		CreatedAt:       now,
	}
	return template, pr2.ValidateGraphTemplate(template)
}

func buildPlan(input Input, spec runtimeSpec, template pr2.GraphTemplate, specDigest string, boardID string, now time.Time) (pr2.GraphPlan, error) {
	nodes := make([]pr2.GraphPlanNode, 0, len(template.Nodes))
	currentNode := template.TerminalNodes[len(template.TerminalNodes)-1]
	for _, node := range template.Nodes {
		status := pr2.GraphPlanNodeStatusCompleted
		if node.NodeID == currentNode && node.NodeType == pr2.GraphNodeTypeInterrupt {
			status = pr2.GraphPlanNodeStatusWaitingConfirmation
		}
		inputDigest, err := pr1.CanonicalDigest(map[string]any{
			"prompt":                 input.Prompt,
			"router_decision_digest": input.RouterDecisionDigest,
			"skill_spec_digest":      specDigest,
			"node_id":                node.NodeID,
		})
		if err != nil {
			return pr2.GraphPlan{}, err
		}
		outputDigest, err := pr1.CanonicalDigest(map[string]any{
			"node_id":   node.NodeID,
			"status":    status,
			"skill_id":  input.SkillID,
			"stage_set": spec.Stages,
		})
		if err != nil {
			return pr2.GraphPlan{}, err
		}
		nodes = append(nodes, pr2.GraphPlanNode{
			NodeID:       node.NodeID,
			NodeType:     node.NodeType,
			Status:       status,
			InputDigest:  &inputDigest,
			OutputDigest: &outputDigest,
		})
	}
	edges := make([]pr2.GraphPlanEdge, 0, len(template.Edges))
	for _, edge := range template.Edges {
		edges = append(edges, pr2.GraphPlanEdge{From: edge.From, To: edge.To})
	}
	graphPlanID := "gplan_" + shortDigest(input.RunID+":skill:"+input.SkillID+":graph")
	plan := pr2.GraphPlan{
		SchemaVersion:        pr2.SchemaVersionGraphPlan,
		GraphPlanID:          graphPlanID,
		GraphTemplateID:      template.GraphTemplateID,
		GraphTemplateVersion: template.Version,
		RunID:                input.RunID,
		BoardID:              boardID,
		Status:               "compiled",
		CurrentNode:          &currentNode,
		ValueDeliveredStage:  pr2.ValueDeliveredStageStoryboardReady,
		Nodes:                nodes,
		Edges:                edges,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	digest, err := pr1.CanonicalDigest(map[string]any{
		"graph_plan_id":         plan.GraphPlanID,
		"template_digest":       template.TemplateDigest,
		"skill_id":              input.SkillID,
		"skill_version":         input.SkillVersion,
		"skill_spec_digest":     specDigest,
		"board_id":              boardID,
		"nodes":                 plan.Nodes,
		"edges":                 plan.Edges,
		"value_delivered_stage": plan.ValueDeliveredStage,
	})
	if err != nil {
		return pr2.GraphPlan{}, err
	}
	plan.GraphPlanDigest = digest
	return plan, pr2.ValidateGraphPlan(plan)
}

func buildElements(input Input, boardID string, now time.Time) ([]pr2.CreativeElement, error) {
	outputs := append([]OutputElement{}, input.OutputElements...)
	if len(outputs) == 0 {
		outputs = []OutputElement{{ElementType: pr2.BoardElementTypePromptBlock, ElementName: "Prompt 草稿", DisplayOrder: 1}}
	}
	elements := make([]pr2.CreativeElement, 0, len(outputs))
	for index, output := range outputs {
		elementType := mapElementType(output.ElementType)
		content := map[string]any{
			"title":               firstNonEmpty(output.ElementName, output.ElementType, "Skill 输出元素"),
			"skill_id":            input.SkillID,
			"skill_version":       input.SkillVersion,
			"source_element_type": output.ElementType,
			"prompt_summary":      summarize(input.Prompt),
			"editable":            output.Editable,
			"referable":           output.Referable,
		}
		contentDigest, err := pr1.CanonicalDigest(content)
		if err != nil {
			return nil, err
		}
		order := int(output.DisplayOrder)
		if order <= 0 {
			order = index + 1
		}
		elements = append(elements, pr2.CreativeElement{
			SchemaVersion:  pr2.SchemaVersionCreativeElement,
			ElementID:      "elem_" + shortDigest(boardID+":"+output.ElementType+":"+fmt.Sprint(order)),
			BoardID:        boardID,
			ElementType:    elementType,
			Source:         pr2.BoardElementSourceGraph,
			Status:         pr2.BoardElementStatusReady,
			Position:       pr2.ElementPosition{X: 0, Y: float64(order * 240), Width: 640, Height: 220, Order: order},
			Content:        content,
			LinkedAssetIDs: []string{},
			ContentDigest:  contentDigest,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}
	return elements, nil
}

func buildEvents(input Input, plan pr2.GraphPlan, board pr2.CreativeBoard, elements []pr2.CreativeElement, now time.Time) ([]pr1.AGUIEnvelope, error) {
	graphPayload := map[string]any{
		"graph_plan_id":         plan.GraphPlanID,
		"graph_template_id":     plan.GraphTemplateID,
		"graph_plan_status":     plan.Status,
		"graph_plan_digest":     plan.GraphPlanDigest,
		"board_id":              plan.BoardID,
		"value_delivered_stage": plan.ValueDeliveredStage,
	}
	if err := pr2.ValidateGraphPlanCreatedPayload(pr2.GraphPlanCreatedPayload{
		GraphPlanID:         plan.GraphPlanID,
		GraphTemplateID:     plan.GraphTemplateID,
		GraphPlanStatus:     plan.Status,
		GraphPlanDigest:     plan.GraphPlanDigest,
		BoardID:             plan.BoardID,
		ValueDeliveredStage: plan.ValueDeliveredStage,
	}); err != nil {
		return nil, err
	}
	graphDigest, err := pr1.CanonicalDigest(graphPayload)
	if err != nil {
		return nil, err
	}
	first, err := pr1.BuildAGUIEnvelope(pr1.AGUIInput{
		EventID:       "evt_" + shortDigest(input.RunID+":skill.graph.plan.created"),
		EventType:     pr2.EventTypeGraphPlanCreated,
		ProjectID:     input.ProjectID,
		SpaceID:       input.SpaceID,
		ActorUserID:   input.ActorUserID,
		SessionID:     input.SessionID,
		RunID:         input.RunID,
		Seq:           1,
		CreatedAt:     now,
		PayloadDigest: graphDigest,
		TraceID:       input.TraceID,
		Payload:       graphPayload,
	})
	if err != nil {
		return nil, err
	}
	boardPayload := map[string]any{
		"board_id":            board.BoardID,
		"board_version":       board.Version,
		"board_status":        board.Status,
		"board_digest":        board.BoardDigest,
		"changed_element_ids": elementIDs(elements),
		"snapshot_required":   true,
	}
	boardDigest, err := pr1.CanonicalDigest(boardPayload)
	if err != nil {
		return nil, err
	}
	second, err := pr1.BuildAGUIEnvelope(pr1.AGUIInput{
		EventID:       "evt_" + shortDigest(input.RunID+":skill.board.snapshot.updated"),
		EventType:     pr2.EventTypeBoardSnapshotUpdated,
		ProjectID:     input.ProjectID,
		SpaceID:       input.SpaceID,
		ActorUserID:   input.ActorUserID,
		SessionID:     input.SessionID,
		RunID:         input.RunID,
		Seq:           2,
		CreatedAt:     now,
		PayloadDigest: boardDigest,
		TraceID:       input.TraceID,
		Payload:       boardPayload,
	})
	if err != nil {
		return nil, err
	}
	return []pr1.AGUIEnvelope{first, second}, nil
}

func validateResult(result Result) error {
	if err := pr2.ValidateGraphTemplate(result.GraphTemplate); err != nil {
		return err
	}
	if err := pr2.ValidateGraphPlan(result.GraphPlan); err != nil {
		return err
	}
	if err := pr2.ValidateBoardCreation(result.Board, result.Elements); err != nil {
		return err
	}
	if err := pr2.ValidateBoardSnapshot(result.Snapshot); err != nil {
		return err
	}
	return pr1.ValidateAGUISequence(result.Events)
}

func mapNodeType(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "llm":
		return pr2.GraphNodeTypeLLM, nil
	case "control", "gate":
		return pr2.GraphNodeTypeGate, nil
	case "user_gate", "interrupt":
		return pr2.GraphNodeTypeInterrupt, nil
	case "state", "board_writer":
		return pr2.GraphNodeTypeBoardWriter, nil
	case pr2.GraphNodeTypeBriefParser, pr2.GraphNodeTypeClarifier, pr2.GraphNodeTypeRouter, pr2.GraphNodeTypeRecommendation, pr2.GraphNodeTypeSummarizer:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported graph node_type %q", value)
	}
}

func mapElementType(value string) string {
	switch strings.TrimSpace(value) {
	case pr2.BoardElementTypeStoryScene, pr2.BoardElementTypeStoryboardFrame, pr2.BoardElementTypePromptBlock,
		pr2.BoardElementTypeReferenceAsset, pr2.BoardElementTypeTextNote, pr2.BoardElementTypeToolSlot, pr2.BoardElementTypeSkillRecommendation:
		return value
	case "storyboard", "shot", "video":
		return pr2.BoardElementTypeStoryboardFrame
	case "prompt":
		return pr2.BoardElementTypePromptBlock
	case "asset_ref", "image_ref":
		return pr2.BoardElementTypeReferenceAsset
	default:
		return pr2.BoardElementTypeTextNote
	}
}

func boardTitle(input Input) string {
	if strings.Contains(input.Prompt, "文旅") || strings.Contains(input.Prompt, "城市") {
		return "城市文旅 Skill Storyboard"
	}
	return input.SkillID + " Board"
}

func summarize(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	runes := []rune(text)
	if len(runes) <= 80 {
		return text
	}
	return string(runes[:80])
}

func elementIDs(elements []pr2.CreativeElement) []string {
	ids := make([]string, 0, len(elements))
	for _, element := range elements {
		ids = append(ids, element.ElementID)
	}
	return ids
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isAllowed(value string, allowed []string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func shortDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}
