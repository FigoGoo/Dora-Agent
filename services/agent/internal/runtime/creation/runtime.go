package creation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr2"
)

const genericTemplateID = "gtemplate_generic_creation"

type Clock func() time.Time

type Runtime struct {
	clock Clock
}

type GenericCreationInput struct {
	RunID                string
	ProjectID            string
	SessionID            string
	SpaceID              string
	ActorUserID          string
	TraceID              string
	Prompt               string
	RouterDecisionDigest string
}

type GenericCreationResult struct {
	GenericGraph  pr2.GenericCreationGraph
	GraphTemplate pr2.GraphTemplate
	GraphPlan     pr2.GraphPlan
	Board         pr2.CreativeBoard
	Elements      []pr2.CreativeElement
	Snapshot      pr2.BoardSnapshot
	Events        []pr1.AGUIEnvelope
}

type ApproveBoardInput struct {
	Board          pr2.CreativeBoard
	ActorUserID    string
	IdempotencyKey string
	ApprovedAt     time.Time
}

type ApproveBoardResult struct {
	Patch pr2.BoardPatch
	Board pr2.CreativeBoard
}

func New(clock Clock) Runtime {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return Runtime{clock: clock}
}

func (r Runtime) ExecuteGenericCreation(ctx context.Context, input GenericCreationInput) (GenericCreationResult, error) {
	if err := ctx.Err(); err != nil {
		return GenericCreationResult{}, err
	}
	if err := validateGenericCreationInput(input); err != nil {
		return GenericCreationResult{}, err
	}
	now := r.clock().UTC()
	prompt := strings.TrimSpace(input.Prompt)

	boardID := "board_" + shortDigest(input.RunID+":board")
	graphPlanID := "gplan_" + shortDigest(input.RunID+":generic_graph")
	template := buildGenericTemplate(now)
	generic := buildGenericGraph(template.TemplateDigest)

	briefDigest, err := pr1.CanonicalDigest(map[string]any{
		"prompt":                 prompt,
		"router_decision_digest": input.RouterDecisionDigest,
	})
	if err != nil {
		return GenericCreationResult{}, err
	}
	briefOutputDigest, err := pr1.CanonicalDigest(map[string]any{
		"brief_summary": summarizePrompt(prompt),
		"graph":         pr2.GenericCreationGraphID,
	})
	if err != nil {
		return GenericCreationResult{}, err
	}
	plan := buildGraphPlan(input, graphPlanID, boardID, briefDigest, briefOutputDigest, now)
	if plan.GraphPlanDigest, err = pr1.CanonicalDigest(map[string]any{
		"graph_plan_id":          plan.GraphPlanID,
		"graph_template_id":      plan.GraphTemplateID,
		"graph_template_version": plan.GraphTemplateVersion,
		"run_id":                 plan.RunID,
		"board_id":               plan.BoardID,
		"nodes":                  plan.Nodes,
		"edges":                  plan.Edges,
		"value_delivered_stage":  plan.ValueDeliveredStage,
	}); err != nil {
		return GenericCreationResult{}, err
	}

	elements, err := buildBoardElements(boardID, prompt, now)
	if err != nil {
		return GenericCreationResult{}, err
	}
	graphPlanIDPtr := plan.GraphPlanID
	board := pr2.CreativeBoard{
		SchemaVersion:   pr2.SchemaVersionCreativeBoard,
		BoardID:         boardID,
		ProjectID:       input.ProjectID,
		SessionID:       input.SessionID,
		RunID:           input.RunID,
		GraphPlanID:     &graphPlanIDPtr,
		Title:           boardTitle(prompt),
		Status:          "ready",
		Version:         1,
		ElementsCount:   len(elements),
		ToolPlanAllowed: false,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if board.BoardDigest, err = pr1.CanonicalDigest(map[string]any{
		"board_id": board.BoardID,
		"version":  board.Version,
		"status":   board.Status,
		"elements": elements,
	}); err != nil {
		return GenericCreationResult{}, err
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
		return GenericCreationResult{}, err
	}

	result := GenericCreationResult{
		GenericGraph:  generic,
		GraphTemplate: template,
		GraphPlan:     plan,
		Board:         board,
		Elements:      elements,
		Snapshot:      snapshot,
		Events:        events,
	}
	if err := validateGenericCreationResult(result); err != nil {
		return GenericCreationResult{}, err
	}
	return result, nil
}

func (r Runtime) ApproveBoard(ctx context.Context, input ApproveBoardInput) (ApproveBoardResult, error) {
	if err := ctx.Err(); err != nil {
		return ApproveBoardResult{}, err
	}
	if err := pr2.ValidateCreativeBoard(input.Board); err != nil {
		return ApproveBoardResult{}, fmt.Errorf("board: %w", err)
	}
	if strings.TrimSpace(input.ActorUserID) == "" {
		return ApproveBoardResult{}, errors.New("actor_user_id is required")
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return ApproveBoardResult{}, errors.New("idempotency_key is required")
	}
	approvedAt := input.ApprovedAt.UTC()
	if approvedAt.IsZero() {
		approvedAt = r.clock().UTC()
	}
	payload := map[string]any{
		"approved_by": input.ActorUserID,
		"approved_at": approvedAt.Format(time.RFC3339),
	}
	patchDigest, err := pr1.CanonicalDigest(map[string]any{
		"board_id":        input.Board.BoardID,
		"base_version":    input.Board.Version,
		"target_version":  input.Board.Version + 1,
		"operation":       pr2.BoardPatchOperationApproveBoard,
		"actor":           pr2.BoardPatchActorUser,
		"idempotency_key": input.IdempotencyKey,
		"payload":         payload,
	})
	if err != nil {
		return ApproveBoardResult{}, err
	}
	patch := pr2.BoardPatch{
		SchemaVersion:  pr2.SchemaVersionBoardPatch,
		PatchID:        "patch_" + shortDigest(input.Board.BoardID+":approve:"+fmt.Sprint(input.Board.Version)),
		BoardID:        input.Board.BoardID,
		BaseVersion:    input.Board.Version,
		TargetVersion:  input.Board.Version + 1,
		Operation:      pr2.BoardPatchOperationApproveBoard,
		Actor:          pr2.BoardPatchActorUser,
		IdempotencyKey: input.IdempotencyKey,
		Payload:        payload,
		PatchDigest:    patchDigest,
		CreatedAt:      approvedAt,
	}
	after := input.Board
	after.Status = "approved"
	after.Version = patch.TargetVersion
	after.ApprovedAt = &approvedAt
	approvedBy := strings.TrimSpace(input.ActorUserID)
	after.ApprovedBy = &approvedBy
	after.ToolPlanAllowed = true
	after.UpdatedAt = approvedAt
	after.BoardDigest, err = pr1.CanonicalDigest(map[string]any{
		"board_id":        after.BoardID,
		"version":         after.Version,
		"status":          after.Status,
		"previous_digest": input.Board.BoardDigest,
		"patch_digest":    patch.PatchDigest,
	})
	if err != nil {
		return ApproveBoardResult{}, err
	}
	if err := pr2.ValidateBoardApproval(input.Board, patch, after); err != nil {
		return ApproveBoardResult{}, err
	}
	return ApproveBoardResult{Patch: patch, Board: after}, nil
}

func validateGenericCreationInput(input GenericCreationInput) error {
	if strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.SessionID) == "" {
		return errors.New("run_id, project_id and session_id are required")
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

func validateGenericCreationResult(result GenericCreationResult) error {
	if err := pr2.ValidateGenericGraphFixture(result.GenericGraph, result.GraphTemplate, result.GraphPlan); err != nil {
		return err
	}
	if err := pr2.ValidateBoardCreation(result.Board, result.Elements); err != nil {
		return err
	}
	if err := pr2.ValidateBoardSnapshot(result.Snapshot); err != nil {
		return err
	}
	if err := pr1.ValidateAGUISequence(result.Events); err != nil {
		return err
	}
	return nil
}

func buildGenericGraph(templateDigest string) pr2.GenericCreationGraph {
	return pr2.GenericCreationGraph{
		SchemaVersion:        pr2.SchemaVersionGenericCreationGraph,
		GenericGraphID:       pr2.GenericCreationGraphID,
		SkillLevel:           pr2.SkillLevelL0,
		PricingPolicy:        "free",
		UsageFee:             0,
		VersionStrategy:      "platform_builtin",
		DefaultNodes:         []string{"brief_parser", "clarifier", "creative_direction", "board_writer", "skill_recommendation"},
		AllowedOutputs:       []string{"brief_summary", "clarifying_questions", "creative_direction", "prompt_draft", "storyboard", "skill_recommendations"},
		GraphTemplateDigest:  templateDigest,
		MarketplaceListingID: nil,
	}
}

func buildGenericTemplate(now time.Time) pr2.GraphTemplate {
	nodes := []pr2.GraphNode{
		{NodeID: "brief_parser", NodeType: pr2.GraphNodeTypeBriefParser, DisplayName: "解析创作意图"},
		{NodeID: "clarifier", NodeType: pr2.GraphNodeTypeClarifier, DisplayName: "补齐关键信息"},
		{NodeID: "creative_direction", NodeType: pr2.GraphNodeTypeLLM, DisplayName: "生成创意方向"},
		{NodeID: "board_writer", NodeType: pr2.GraphNodeTypeBoardWriter, DisplayName: "生成 Board 草稿"},
		{NodeID: "skill_recommendation", NodeType: pr2.GraphNodeTypeRecommendation, DisplayName: "推荐可安装 Skill"},
	}
	edges := []pr2.GraphEdge{
		{From: "brief_parser", To: "clarifier"},
		{From: "clarifier", To: "creative_direction"},
		{From: "creative_direction", To: "board_writer"},
		{From: "board_writer", To: "skill_recommendation"},
	}
	digest, _ := pr1.CanonicalDigest(map[string]any{
		"graph_template_id": genericTemplateID,
		"version":           "v1",
		"graph_type":        pr2.GraphTypeGenericCreation,
		"skill_level":       pr2.SkillLevelL0,
		"entry_node":        "brief_parser",
		"terminal_nodes":    []string{"board_writer", "skill_recommendation"},
		"nodes":             nodes,
		"edges":             edges,
	})
	return pr2.GraphTemplate{
		SchemaVersion:   pr2.SchemaVersionGraphTemplate,
		GraphTemplateID: genericTemplateID,
		Name:            "Generic Creation Graph",
		Version:         "v1",
		GraphType:       pr2.GraphTypeGenericCreation,
		SkillLevel:      pr2.SkillLevelL0,
		EntryNode:       "brief_parser",
		TerminalNodes:   []string{"board_writer", "skill_recommendation"},
		Nodes:           nodes,
		Edges:           edges,
		TemplateDigest:  digest,
		CreatedAt:       now,
	}
}

func buildGraphPlan(input GenericCreationInput, graphPlanID, boardID, inputDigest, outputDigest string, now time.Time) pr2.GraphPlan {
	currentNode := "board_writer"
	return pr2.GraphPlan{
		SchemaVersion:        pr2.SchemaVersionGraphPlan,
		GraphPlanID:          graphPlanID,
		GraphTemplateID:      genericTemplateID,
		GraphTemplateVersion: "v1",
		RunID:                input.RunID,
		BoardID:              boardID,
		Status:               "compiled",
		CurrentNode:          &currentNode,
		ValueDeliveredStage:  pr2.ValueDeliveredStageStoryboardReady,
		Nodes: []pr2.GraphPlanNode{
			{NodeID: "brief_parser", NodeType: pr2.GraphNodeTypeBriefParser, Status: pr2.GraphPlanNodeStatusCompleted, InputDigest: &inputDigest, OutputDigest: &outputDigest},
			{NodeID: "clarifier", NodeType: pr2.GraphNodeTypeClarifier, Status: pr2.GraphPlanNodeStatusSkipped},
			{NodeID: "creative_direction", NodeType: pr2.GraphNodeTypeLLM, Status: pr2.GraphPlanNodeStatusCompleted, InputDigest: &outputDigest, OutputDigest: &outputDigest},
			{NodeID: "board_writer", NodeType: pr2.GraphNodeTypeBoardWriter, Status: pr2.GraphPlanNodeStatusCompleted, InputDigest: &outputDigest},
			{NodeID: "skill_recommendation", NodeType: pr2.GraphNodeTypeRecommendation, Status: pr2.GraphPlanNodeStatusPending},
		},
		Edges: []pr2.GraphPlanEdge{
			{From: "brief_parser", To: "clarifier"},
			{From: "clarifier", To: "creative_direction"},
			{From: "creative_direction", To: "board_writer"},
			{From: "board_writer", To: "skill_recommendation"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func buildBoardElements(boardID, prompt string, now time.Time) ([]pr2.CreativeElement, error) {
	sceneContent := map[string]any{
		"scene_title": "核心开场镜头",
		"shot_prompt": "围绕用户 brief 建立真实、有节奏的开场镜头：" + summarizePrompt(prompt),
	}
	promptContent := map[string]any{
		"prompt": "基于 brief 生成 Storyboard 草稿：" + prompt,
	}
	sceneDigest, err := pr1.CanonicalDigest(sceneContent)
	if err != nil {
		return nil, err
	}
	promptDigest, err := pr1.CanonicalDigest(promptContent)
	if err != nil {
		return nil, err
	}
	return []pr2.CreativeElement{
		{
			SchemaVersion:  pr2.SchemaVersionCreativeElement,
			ElementID:      "elem_" + shortDigest(boardID+":scene:1"),
			BoardID:        boardID,
			ElementType:    pr2.BoardElementTypeStoryScene,
			Source:         pr2.BoardElementSourceGraph,
			Status:         pr2.BoardElementStatusReady,
			Position:       pr2.ElementPosition{X: 0, Y: 0, Width: 640, Height: 360, Order: 1},
			Content:        sceneContent,
			LinkedAssetIDs: []string{},
			ContentDigest:  sceneDigest,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		{
			SchemaVersion:  pr2.SchemaVersionCreativeElement,
			ElementID:      "elem_" + shortDigest(boardID+":prompt:1"),
			BoardID:        boardID,
			ElementType:    pr2.BoardElementTypePromptBlock,
			Source:         pr2.BoardElementSourceGraph,
			Status:         pr2.BoardElementStatusReady,
			Position:       pr2.ElementPosition{X: 660, Y: 0, Width: 480, Height: 260, Order: 2},
			Content:        promptContent,
			LinkedAssetIDs: []string{},
			ContentDigest:  promptDigest,
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}, nil
}

func buildEvents(input GenericCreationInput, plan pr2.GraphPlan, board pr2.CreativeBoard, elements []pr2.CreativeElement, now time.Time) ([]pr1.AGUIEnvelope, error) {
	graphPayload := pr2.GraphPlanCreatedPayload{
		GraphPlanID:         plan.GraphPlanID,
		GraphTemplateID:     plan.GraphTemplateID,
		GraphPlanStatus:     plan.Status,
		GraphPlanDigest:     plan.GraphPlanDigest,
		BoardID:             plan.BoardID,
		ValueDeliveredStage: plan.ValueDeliveredStage,
	}
	if err := pr2.ValidateGraphPlanCreatedPayload(graphPayload); err != nil {
		return nil, err
	}
	graphPayloadMap := map[string]any{
		"graph_plan_id":         graphPayload.GraphPlanID,
		"graph_template_id":     graphPayload.GraphTemplateID,
		"graph_plan_status":     graphPayload.GraphPlanStatus,
		"graph_plan_digest":     graphPayload.GraphPlanDigest,
		"board_id":              graphPayload.BoardID,
		"value_delivered_stage": graphPayload.ValueDeliveredStage,
	}
	graphPayloadDigest, err := pr1.CanonicalDigest(graphPayloadMap)
	if err != nil {
		return nil, err
	}
	boardPayload := pr2.BoardSnapshotUpdatedPayload{
		BoardID:           board.BoardID,
		BoardVersion:      board.Version,
		BoardStatus:       board.Status,
		BoardDigest:       board.BoardDigest,
		ChangedElementIDs: elementIDs(elements),
		SnapshotRequired:  true,
	}
	if err := pr2.ValidateBoardSnapshotUpdatedPayload(boardPayload); err != nil {
		return nil, err
	}
	boardPayloadMap := map[string]any{
		"board_id":            boardPayload.BoardID,
		"board_version":       boardPayload.BoardVersion,
		"board_status":        boardPayload.BoardStatus,
		"board_digest":        boardPayload.BoardDigest,
		"changed_element_ids": boardPayload.ChangedElementIDs,
		"snapshot_required":   boardPayload.SnapshotRequired,
	}
	boardPayloadDigest, err := pr1.CanonicalDigest(boardPayloadMap)
	if err != nil {
		return nil, err
	}
	first, err := pr1.BuildAGUIEnvelope(pr1.AGUIInput{
		EventID:       "evt_" + shortDigest(input.RunID+":graph.plan.created"),
		EventType:     pr2.EventTypeGraphPlanCreated,
		ProjectID:     input.ProjectID,
		SpaceID:       input.SpaceID,
		ActorUserID:   input.ActorUserID,
		SessionID:     input.SessionID,
		RunID:         input.RunID,
		Seq:           1,
		CreatedAt:     now,
		PayloadDigest: graphPayloadDigest,
		TraceID:       input.TraceID,
		Payload:       graphPayloadMap,
	})
	if err != nil {
		return nil, err
	}
	second, err := pr1.BuildAGUIEnvelope(pr1.AGUIInput{
		EventID:       "evt_" + shortDigest(input.RunID+":board.snapshot.updated"),
		EventType:     pr2.EventTypeBoardSnapshotUpdated,
		ProjectID:     input.ProjectID,
		SpaceID:       input.SpaceID,
		ActorUserID:   input.ActorUserID,
		SessionID:     input.SessionID,
		RunID:         input.RunID,
		Seq:           2,
		CreatedAt:     now,
		PayloadDigest: boardPayloadDigest,
		TraceID:       input.TraceID,
		Payload:       boardPayloadMap,
	})
	if err != nil {
		return nil, err
	}
	return []pr1.AGUIEnvelope{first, second}, nil
}

func boardTitle(prompt string) string {
	if strings.Contains(prompt, "城市") || strings.Contains(strings.ToLower(prompt), "city") {
		return "城市文旅短片 Storyboard"
	}
	return "创作 Storyboard 草稿"
}

func summarizePrompt(prompt string) string {
	prompt = strings.Join(strings.Fields(strings.TrimSpace(prompt)), " ")
	if len([]rune(prompt)) <= 80 {
		return prompt
	}
	runes := []rune(prompt)
	return string(runes[:80])
}

func elementIDs(elements []pr2.CreativeElement) []string {
	ids := make([]string, 0, len(elements))
	for _, element := range elements {
		ids = append(ids, element.ElementID)
	}
	return ids
}

func shortDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:16]
}
