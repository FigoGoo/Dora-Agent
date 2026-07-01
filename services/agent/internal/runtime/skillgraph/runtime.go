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

	"github.com/FigoGoo/Dora-Agent/internal/contracts/boardgraph"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/foundation"
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
	GraphTemplate   boardgraph.GraphTemplate
	GraphPlan       boardgraph.GraphPlan
	Board           boardgraph.CreativeBoard
	Elements        []boardgraph.CreativeElement
	Snapshot        boardgraph.BoardSnapshot
	Events          []foundation.AGUIEnvelope
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
	specDigest, err := foundation.CanonicalDigest(spec.Raw)
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
	board := boardgraph.CreativeBoard{
		SchemaVersion:   boardgraph.SchemaVersionCreativeBoard,
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
	board.BoardDigest, err = foundation.CanonicalDigest(map[string]any{
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
	snapshot := boardgraph.BoardSnapshot{
		SchemaVersion: boardgraph.SchemaVersionBoardSnapshot,
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
		if err := foundation.ValidateDigest(input.RouterDecisionDigest); err != nil {
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
	if !isAllowed(spec.Level, []string{boardgraph.SkillLevelL0, boardgraph.SkillLevelL1, boardgraph.SkillLevelL2, boardgraph.SkillLevelL3}) {
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

func buildTemplate(input Input, spec runtimeSpec, specDigest string, now time.Time) (boardgraph.GraphTemplate, error) {
	nodes := make([]boardgraph.GraphNode, 0, len(spec.GraphTemplate.Nodes))
	for _, node := range spec.GraphTemplate.Nodes {
		mappedType, err := mapNodeType(node.NodeType)
		if err != nil {
			return boardgraph.GraphTemplate{}, err
		}
		displayName := strings.TrimSpace(node.DisplayName)
		if displayName == "" {
			displayName = node.NodeKey
		}
		configDigest := specDigest
		if len(node.Config) > 0 {
			var err error
			configDigest, err = foundation.CanonicalDigest(node.Config)
			if err != nil {
				return boardgraph.GraphTemplate{}, err
			}
		}
		nodes = append(nodes, boardgraph.GraphNode{
			NodeID:       node.NodeKey,
			NodeType:     mappedType,
			DisplayName:  displayName,
			ConfigDigest: &configDigest,
		})
	}
	edges := make([]boardgraph.GraphEdge, 0, len(spec.GraphTemplate.Edges))
	for _, edge := range spec.GraphTemplate.Edges {
		edges = append(edges, boardgraph.GraphEdge{From: edge.From, To: edge.To})
	}
	terminalNodes := append([]string{}, spec.GraphTemplate.TerminalNodes...)
	if len(terminalNodes) == 0 {
		terminalNodes = []string{nodes[len(nodes)-1].NodeID}
	}
	graphType := boardgraph.GraphTypeSystemSkill
	if strings.TrimSpace(input.SkillSource) == foundation.SkillSourceMarketplace {
		graphType = boardgraph.GraphTypeMarketplaceSkill
	}
	templateDigest, err := foundation.CanonicalDigest(map[string]any{
		"skill_id":          input.SkillID,
		"skill_version":     input.SkillVersion,
		"skill_spec_digest": specDigest,
		"entry_node":        spec.GraphTemplate.EntryNode,
		"terminal_nodes":    terminalNodes,
		"nodes":             nodes,
		"edges":             edges,
	})
	if err != nil {
		return boardgraph.GraphTemplate{}, err
	}
	template := boardgraph.GraphTemplate{
		SchemaVersion:   boardgraph.SchemaVersionGraphTemplate,
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
	return template, boardgraph.ValidateGraphTemplate(template)
}

func buildPlan(input Input, spec runtimeSpec, template boardgraph.GraphTemplate, specDigest string, boardID string, now time.Time) (boardgraph.GraphPlan, error) {
	nodes := make([]boardgraph.GraphPlanNode, 0, len(template.Nodes))
	currentNode := template.TerminalNodes[len(template.TerminalNodes)-1]
	for _, node := range template.Nodes {
		status := boardgraph.GraphPlanNodeStatusCompleted
		if node.NodeID == currentNode && node.NodeType == boardgraph.GraphNodeTypeInterrupt {
			status = boardgraph.GraphPlanNodeStatusWaitingConfirmation
		}
		inputDigest, err := foundation.CanonicalDigest(map[string]any{
			"prompt":                 input.Prompt,
			"router_decision_digest": input.RouterDecisionDigest,
			"skill_spec_digest":      specDigest,
			"node_id":                node.NodeID,
		})
		if err != nil {
			return boardgraph.GraphPlan{}, err
		}
		outputDigest, err := foundation.CanonicalDigest(map[string]any{
			"node_id":   node.NodeID,
			"status":    status,
			"skill_id":  input.SkillID,
			"stage_set": spec.Stages,
		})
		if err != nil {
			return boardgraph.GraphPlan{}, err
		}
		nodes = append(nodes, boardgraph.GraphPlanNode{
			NodeID:       node.NodeID,
			NodeType:     node.NodeType,
			Status:       status,
			InputDigest:  &inputDigest,
			OutputDigest: &outputDigest,
		})
	}
	edges := make([]boardgraph.GraphPlanEdge, 0, len(template.Edges))
	for _, edge := range template.Edges {
		edges = append(edges, boardgraph.GraphPlanEdge{From: edge.From, To: edge.To})
	}
	graphPlanID := "gplan_" + shortDigest(input.RunID+":skill:"+input.SkillID+":graph")
	plan := boardgraph.GraphPlan{
		SchemaVersion:        boardgraph.SchemaVersionGraphPlan,
		GraphPlanID:          graphPlanID,
		GraphTemplateID:      template.GraphTemplateID,
		GraphTemplateVersion: template.Version,
		RunID:                input.RunID,
		BoardID:              boardID,
		Status:               "compiled",
		CurrentNode:          &currentNode,
		ValueDeliveredStage:  boardgraph.ValueDeliveredStageStoryboardReady,
		Nodes:                nodes,
		Edges:                edges,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	digest, err := foundation.CanonicalDigest(map[string]any{
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
		return boardgraph.GraphPlan{}, err
	}
	plan.GraphPlanDigest = digest
	return plan, boardgraph.ValidateGraphPlan(plan)
}

func buildElements(input Input, boardID string, now time.Time) ([]boardgraph.CreativeElement, error) {
	outputs := append([]OutputElement{}, input.OutputElements...)
	if len(outputs) == 0 {
		outputs = []OutputElement{{ElementType: boardgraph.BoardElementTypePromptBlock, ElementName: "Prompt 草稿", DisplayOrder: 1}}
	}
	elements := make([]boardgraph.CreativeElement, 0, len(outputs))
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
		contentDigest, err := foundation.CanonicalDigest(content)
		if err != nil {
			return nil, err
		}
		order := int(output.DisplayOrder)
		if order <= 0 {
			order = index + 1
		}
		elements = append(elements, boardgraph.CreativeElement{
			SchemaVersion:  boardgraph.SchemaVersionCreativeElement,
			ElementID:      "elem_" + shortDigest(boardID+":"+output.ElementType+":"+fmt.Sprint(order)),
			BoardID:        boardID,
			ElementType:    elementType,
			Source:         boardgraph.BoardElementSourceGraph,
			Status:         boardgraph.BoardElementStatusReady,
			Position:       boardgraph.ElementPosition{X: 0, Y: float64(order * 240), Width: 640, Height: 220, Order: order},
			Content:        content,
			LinkedAssetIDs: []string{},
			ContentDigest:  contentDigest,
			CreatedAt:      now,
			UpdatedAt:      now,
		})
	}
	return elements, nil
}

func buildEvents(input Input, plan boardgraph.GraphPlan, board boardgraph.CreativeBoard, elements []boardgraph.CreativeElement, now time.Time) ([]foundation.AGUIEnvelope, error) {
	graphPayload := map[string]any{
		"graph_plan_id":         plan.GraphPlanID,
		"graph_template_id":     plan.GraphTemplateID,
		"graph_plan_status":     plan.Status,
		"graph_plan_digest":     plan.GraphPlanDigest,
		"board_id":              plan.BoardID,
		"value_delivered_stage": plan.ValueDeliveredStage,
	}
	if err := boardgraph.ValidateGraphPlanCreatedPayload(boardgraph.GraphPlanCreatedPayload{
		GraphPlanID:         plan.GraphPlanID,
		GraphTemplateID:     plan.GraphTemplateID,
		GraphPlanStatus:     plan.Status,
		GraphPlanDigest:     plan.GraphPlanDigest,
		BoardID:             plan.BoardID,
		ValueDeliveredStage: plan.ValueDeliveredStage,
	}); err != nil {
		return nil, err
	}
	graphDigest, err := foundation.CanonicalDigest(graphPayload)
	if err != nil {
		return nil, err
	}
	first, err := foundation.BuildAGUIEnvelope(foundation.AGUIInput{
		EventID:       "evt_" + shortDigest(input.RunID+":skill.graph.plan.created"),
		EventType:     boardgraph.EventTypeGraphPlanCreated,
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
	boardDigest, err := foundation.CanonicalDigest(boardPayload)
	if err != nil {
		return nil, err
	}
	second, err := foundation.BuildAGUIEnvelope(foundation.AGUIInput{
		EventID:       "evt_" + shortDigest(input.RunID+":skill.board.snapshot.updated"),
		EventType:     boardgraph.EventTypeBoardSnapshotUpdated,
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
	return []foundation.AGUIEnvelope{first, second}, nil
}

func validateResult(result Result) error {
	if err := boardgraph.ValidateGraphTemplate(result.GraphTemplate); err != nil {
		return err
	}
	if err := boardgraph.ValidateGraphPlan(result.GraphPlan); err != nil {
		return err
	}
	if err := boardgraph.ValidateBoardCreation(result.Board, result.Elements); err != nil {
		return err
	}
	if err := boardgraph.ValidateBoardSnapshot(result.Snapshot); err != nil {
		return err
	}
	return foundation.ValidateAGUISequence(result.Events)
}

func mapNodeType(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "llm":
		return boardgraph.GraphNodeTypeLLM, nil
	case "control", "gate":
		return boardgraph.GraphNodeTypeGate, nil
	case "user_gate", "interrupt":
		return boardgraph.GraphNodeTypeInterrupt, nil
	case "state", "board_writer":
		return boardgraph.GraphNodeTypeBoardWriter, nil
	case boardgraph.GraphNodeTypeBriefParser, boardgraph.GraphNodeTypeClarifier, boardgraph.GraphNodeTypeRouter, boardgraph.GraphNodeTypeRecommendation, boardgraph.GraphNodeTypeSummarizer:
		return value, nil
	default:
		return "", fmt.Errorf("unsupported graph node_type %q", value)
	}
}

func mapElementType(value string) string {
	switch strings.TrimSpace(value) {
	case boardgraph.BoardElementTypeStoryScene, boardgraph.BoardElementTypeStoryboardFrame, boardgraph.BoardElementTypePromptBlock,
		boardgraph.BoardElementTypeReferenceAsset, boardgraph.BoardElementTypeTextNote, boardgraph.BoardElementTypeToolSlot, boardgraph.BoardElementTypeSkillRecommendation:
		return value
	case "storyboard", "shot", "video":
		return boardgraph.BoardElementTypeStoryboardFrame
	case "prompt":
		return boardgraph.BoardElementTypePromptBlock
	case "asset_ref", "image_ref":
		return boardgraph.BoardElementTypeReferenceAsset
	default:
		return boardgraph.BoardElementTypeTextNote
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

func elementIDs(elements []boardgraph.CreativeElement) []string {
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
