package analyzematerials

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
)

const (
	nodeValidateIntent         = "validate_intent"
	nodeLoadAssetInputs        = "load_asset_inputs"
	nodeNormalizeEvidence      = "normalize_evidence"
	nodeSelectPromptEvidence   = "select_prompt_evidence"
	nodeEvaluateEvidenceGate   = "evaluate_evidence_gate"
	nodeBuildPrimaryPrompt     = "build_primary_prompt"
	nodeCallModelPrimary       = "call_model_primary"
	nodeValidateAnalysis       = "validate_analysis_primary"
	nodeEmitCompletedOrPartial = "emit_completed_or_partial"
	nodeEmitDependencyFailed   = "emit_dependency_failed"
	nodeEmitCandidateFailed    = "emit_candidate_failed"
)

// GraphEdge 是设计与拓扑测试共同使用的稳定直接边；Branch 目标不重复计入本清单。
type GraphEdge struct {
	From string
	To   string
}

var graphEdges = []GraphEdge{
	{compose.START, nodeValidateIntent},
	{nodeValidateIntent, nodeLoadAssetInputs},
	{nodeLoadAssetInputs, nodeNormalizeEvidence},
	{nodeNormalizeEvidence, nodeSelectPromptEvidence},
	{nodeSelectPromptEvidence, nodeEvaluateEvidenceGate},
	{nodeBuildPrimaryPrompt, nodeCallModelPrimary},
	{nodeCallModelPrimary, nodeValidateAnalysis},
	{nodeEmitCompletedOrPartial, compose.END},
	{nodeEmitDependencyFailed, compose.END},
	{nodeEmitCandidateFailed, compose.END},
}

const (
	branchRouteEvidenceGate        = "route_evidence_gate"
	branchRouteCandidateValidation = "route_candidate_validation"
)

// NodeKeys 返回设计评审冻结的 11 个 Graph Node exact-set。
func NodeKeys() []string {
	return []string{
		nodeValidateIntent,
		nodeLoadAssetInputs,
		nodeNormalizeEvidence,
		nodeSelectPromptEvidence,
		nodeEvaluateEvidenceGate,
		nodeBuildPrimaryPrompt,
		nodeCallModelPrimary,
		nodeValidateAnalysis,
		nodeEmitCompletedOrPartial,
		nodeEmitDependencyFailed,
		nodeEmitCandidateFailed,
	}
}

// BranchKeys 返回设计评审冻结的两个 Branch exact-set。
func BranchKeys() []string {
	return []string{branchRouteEvidenceGate, branchRouteCandidateValidation}
}

// GraphEdges 返回直接 Edge exact-set 的副本，防止测试或调用方修改启动拓扑。
func GraphEdges() []GraphEdge {
	return append([]GraphEdge(nil), graphEdges...)
}

// CompiledGraph 是启动阶段编译后可并发复用的 analyze_materials V2 Preview DAG。
type CompiledGraph struct {
	runnable compose.Runnable[GraphInput, Outcome]
}

// Invoke 执行已编译 Graph。该能力当前只能由默认不注册的 Tool Core 包装器调用。
func (g *CompiledGraph) Invoke(ctx context.Context, input GraphInput) (Outcome, error) {
	if g == nil || g.runnable == nil {
		return Outcome{}, fmt.Errorf("invoke %s: graph is not compiled", GraphName)
	}
	return g.runnable.Invoke(ctx, input)
}

// graphBuilder 仅持有启动时注入的只读依赖和冻结 Prompt 模板。
type graphBuilder struct {
	loader         EvidenceLoader
	promptTemplate primaryPromptTemplate
}

// Compile 只装配获准的 11 Node、2 Branch、无环 AllPredecessor DAG。
func Compile(ctx context.Context, chatModel model.BaseChatModel, loader EvidenceLoader) (*CompiledGraph, error) {
	if chatModel == nil || loader == nil {
		return nil, fmt.Errorf("compile %s: dependency is nil", GraphName)
	}
	builder := &graphBuilder{loader: loader, promptTemplate: newPrimaryPromptTemplate()}
	graph := compose.NewGraph[GraphInput, Outcome](compose.WithGenLocalState(func(context.Context) *State {
		return &State{}
	}))

	for _, node := range []struct {
		key    string
		lambda *compose.Lambda
	}{
		{nodeValidateIntent, compose.InvokableLambda(builder.validateIntent)},
		{nodeLoadAssetInputs, compose.InvokableLambda(builder.loadAssetInputs)},
		{nodeNormalizeEvidence, compose.InvokableLambda(builder.normalizeEvidence)},
		{nodeSelectPromptEvidence, compose.InvokableLambda(builder.selectPromptEvidence)},
		{nodeEvaluateEvidenceGate, compose.InvokableLambda(builder.evaluateEvidenceGate)},
		{nodeBuildPrimaryPrompt, compose.InvokableLambda(builder.buildPrimaryPrompt)},
		{nodeValidateAnalysis, compose.InvokableLambda(builder.validateAnalysisPrimary)},
		{nodeEmitCompletedOrPartial, compose.InvokableLambda(builder.emitCompletedOrPartial)},
		{nodeEmitDependencyFailed, compose.InvokableLambda(builder.emitDependencyFailed)},
		{nodeEmitCandidateFailed, compose.InvokableLambda(builder.emitCandidateFailed)},
	} {
		if err := graph.AddLambdaNode(node.key, node.lambda); err != nil {
			return nil, fmt.Errorf("add %s node: %w", node.key, err)
		}
	}
	if err := graph.AddChatModelNode(
		nodeCallModelPrimary,
		classifyModelErrors(chatModel),
		compose.WithStatePostHandler(builder.captureModelMessage),
	); err != nil {
		return nil, fmt.Errorf("add %s node: %w", nodeCallModelPrimary, err)
	}

	for _, edge := range graphEdges {
		if err := graph.AddEdge(edge.From, edge.To); err != nil {
			return nil, fmt.Errorf("add graph edge %s -> %s: %w", edge.From, edge.To, err)
		}
	}

	if err := graph.AddBranch(nodeEvaluateEvidenceGate, compose.NewGraphBranch(
		builder.routeEvidenceGate,
		map[string]bool{nodeBuildPrimaryPrompt: true, nodeEmitDependencyFailed: true},
	)); err != nil {
		return nil, fmt.Errorf("add %s branch: %w", branchRouteEvidenceGate, err)
	}
	if err := graph.AddBranch(nodeValidateAnalysis, compose.NewGraphBranch(
		builder.routeCandidateValidation,
		map[string]bool{nodeEmitCompletedOrPartial: true, nodeEmitCandidateFailed: true},
	)); err != nil {
		return nil, fmt.Errorf("add %s branch: %w", branchRouteCandidateValidation, err)
	}

	runnable, err := graph.Compile(ctx,
		compose.WithGraphName(GraphName),
		compose.WithNodeTriggerMode(compose.AllPredecessor),
	)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", GraphName, err)
	}
	return &CompiledGraph{runnable: runnable}, nil
}
