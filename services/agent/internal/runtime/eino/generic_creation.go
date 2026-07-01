package eino

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr1"
	"github.com/FigoGoo/Dora-Agent/internal/contracts/pr2"
	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/creation"
	"github.com/cloudwego/eino/compose"
)

const (
	GenericCreationGraphName = "generic_creation_graph"

	genericCreationNodeBriefParser         = "brief_parser"
	genericCreationNodeClarifier           = "clarifier"
	genericCreationNodeCreativeDirection   = "creative_direction"
	genericCreationNodeBoardWriter         = "board_writer"
	genericCreationNodeSkillRecommendation = "skill_recommendation"
)

var genericCreationNodeOrder = []string{
	genericCreationNodeBriefParser,
	genericCreationNodeClarifier,
	genericCreationNodeCreativeDirection,
	genericCreationNodeBoardWriter,
	genericCreationNodeSkillRecommendation,
}

type GenericCreationGraphRunner struct {
	runnable compose.Runnable[creation.GenericCreationInput, creation.GenericCreationResult]
}

type genericCreationStage struct {
	Input                    creation.GenericCreationInput
	BriefSummary             string
	ClarificationRequired    bool
	CreativeDirectionSummary string
}

func NewGenericCreationGraphRunner(ctx context.Context, clock creation.Clock) (*GenericCreationGraphRunner, error) {
	runtime := creation.New(clock)
	runnable, err := compileGenericCreationGraph(ctx, runtime)
	if err != nil {
		return nil, err
	}
	return &GenericCreationGraphRunner{runnable: runnable}, nil
}

func GenericCreationGraphNodeIDs() []string {
	ids := make([]string, len(genericCreationNodeOrder))
	copy(ids, genericCreationNodeOrder)
	return ids
}

func (r *GenericCreationGraphRunner) Execute(ctx context.Context, input creation.GenericCreationInput) (creation.GenericCreationResult, error) {
	if r == nil || r.runnable == nil {
		return creation.GenericCreationResult{}, errors.New("generic creation graph runner is not initialized")
	}
	result, err := r.runnable.Invoke(ctx, input, compose.WithRuntimeMaxSteps(len(genericCreationNodeOrder)+2))
	if err != nil {
		return creation.GenericCreationResult{}, err
	}
	if err := validateGenericCreationOutput(result); err != nil {
		return creation.GenericCreationResult{}, err
	}
	return result, nil
}

func compileGenericCreationGraph(ctx context.Context, runtime creation.Runtime) (compose.Runnable[creation.GenericCreationInput, creation.GenericCreationResult], error) {
	graph := compose.NewGraph[creation.GenericCreationInput, creation.GenericCreationResult]()
	nodes := []struct {
		id     string
		lambda *compose.Lambda
	}{
		{id: genericCreationNodeBriefParser, lambda: compose.InvokableLambda(parseGenericBrief, compose.WithLambdaType(genericCreationNodeBriefParser))},
		{id: genericCreationNodeClarifier, lambda: compose.InvokableLambda(clarifyGenericBrief, compose.WithLambdaType(genericCreationNodeClarifier))},
		{id: genericCreationNodeCreativeDirection, lambda: compose.InvokableLambda(planGenericCreativeDirection, compose.WithLambdaType(genericCreationNodeCreativeDirection))},
		{id: genericCreationNodeBoardWriter, lambda: compose.InvokableLambda(writeGenericBoard(runtime), compose.WithLambdaType(genericCreationNodeBoardWriter))},
		{id: genericCreationNodeSkillRecommendation, lambda: compose.InvokableLambda(recommendGenericSkills, compose.WithLambdaType(genericCreationNodeSkillRecommendation))},
	}
	for _, node := range nodes {
		if err := graph.AddLambdaNode(node.id, node.lambda, compose.WithNodeName(node.id)); err != nil {
			return nil, fmt.Errorf("add graph node %s: %w", node.id, err)
		}
	}
	edges := [][2]string{
		{compose.START, genericCreationNodeBriefParser},
		{genericCreationNodeBriefParser, genericCreationNodeClarifier},
		{genericCreationNodeClarifier, genericCreationNodeCreativeDirection},
		{genericCreationNodeCreativeDirection, genericCreationNodeBoardWriter},
		{genericCreationNodeBoardWriter, genericCreationNodeSkillRecommendation},
		{genericCreationNodeSkillRecommendation, compose.END},
	}
	for _, edge := range edges {
		if err := graph.AddEdge(edge[0], edge[1]); err != nil {
			return nil, fmt.Errorf("add graph edge %s -> %s: %w", edge[0], edge[1], err)
		}
	}
	return graph.Compile(ctx, compose.WithGraphName(GenericCreationGraphName), compose.WithMaxRunSteps(len(genericCreationNodeOrder)+2))
}

func parseGenericBrief(ctx context.Context, input creation.GenericCreationInput) (genericCreationStage, error) {
	if err := ctx.Err(); err != nil {
		return genericCreationStage{}, err
	}
	input.RunID = strings.TrimSpace(input.RunID)
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.SessionID = strings.TrimSpace(input.SessionID)
	input.SpaceID = strings.TrimSpace(input.SpaceID)
	input.ActorUserID = strings.TrimSpace(input.ActorUserID)
	input.TraceID = strings.TrimSpace(input.TraceID)
	input.Prompt = strings.TrimSpace(input.Prompt)
	input.RouterDecisionDigest = strings.TrimSpace(input.RouterDecisionDigest)
	if input.RunID == "" || input.ProjectID == "" || input.SessionID == "" {
		return genericCreationStage{}, errors.New("run_id, project_id and session_id are required")
	}
	if input.Prompt == "" {
		return genericCreationStage{}, errors.New("prompt is required")
	}
	if input.RouterDecisionDigest != "" {
		if err := pr1.ValidateDigest(input.RouterDecisionDigest); err != nil {
			return genericCreationStage{}, fmt.Errorf("router_decision_digest: %w", err)
		}
	}
	return genericCreationStage{
		Input:        input,
		BriefSummary: summarizeGenericPrompt(input.Prompt),
	}, nil
}

func clarifyGenericBrief(ctx context.Context, stage genericCreationStage) (genericCreationStage, error) {
	if err := ctx.Err(); err != nil {
		return genericCreationStage{}, err
	}
	stage.ClarificationRequired = false
	return stage, nil
}

func planGenericCreativeDirection(ctx context.Context, stage genericCreationStage) (genericCreationStage, error) {
	if err := ctx.Err(); err != nil {
		return genericCreationStage{}, err
	}
	stage.CreativeDirectionSummary = "storyboard:" + stage.BriefSummary
	return stage, nil
}

func writeGenericBoard(runtime creation.Runtime) func(context.Context, genericCreationStage) (creation.GenericCreationResult, error) {
	return func(ctx context.Context, stage genericCreationStage) (creation.GenericCreationResult, error) {
		if err := ctx.Err(); err != nil {
			return creation.GenericCreationResult{}, err
		}
		return runtime.ExecuteGenericCreation(ctx, stage.Input)
	}
}

func recommendGenericSkills(ctx context.Context, result creation.GenericCreationResult) (creation.GenericCreationResult, error) {
	if err := ctx.Err(); err != nil {
		return creation.GenericCreationResult{}, err
	}
	if result.GenericGraph.MarketplaceListingID != nil || result.GenericGraph.UsageFee != 0 {
		return creation.GenericCreationResult{}, errors.New("generic L0 graph must not bind marketplace listing or charge usage fee")
	}
	return result, nil
}

func validateGenericCreationOutput(result creation.GenericCreationResult) error {
	if err := pr2.ValidateGenericGraphFixture(result.GenericGraph, result.GraphTemplate, result.GraphPlan); err != nil {
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

func summarizeGenericPrompt(prompt string) string {
	prompt = strings.Join(strings.Fields(strings.TrimSpace(prompt)), " ")
	if len([]rune(prompt)) <= 80 {
		return prompt
	}
	runes := []rune(prompt)
	return string(runes[:80])
}
